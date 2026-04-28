package semantic

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/jeanjohnson/six-eyes/ingestion/internal/models"
	"golang.org/x/time/rate"
)

const baseURL = "https://api.semanticscholar.org/graph/v1"

const (
	paperBatchSize  = 500
	authorBatchSize = 1000
)

// Client enriches papers with author h-index and citation count from
// Semantic Scholar. Rate limit: 100 req/min (no API key required).
type Client struct {
	rc      *resty.Client
	limiter *rate.Limiter
}

func NewClient() *Client {
	rc := resty.New().
		SetBaseURL(baseURL).
		SetRetryCount(5).
		SetRetryWaitTime(10 * time.Second).
		SetRetryMaxWaitTime(60 * time.Second).
		AddRetryCondition(func(r *resty.Response, err error) bool {
			return err != nil || r.StatusCode() == 429
		}).
		SetRetryAfter(func(_ *resty.Client, _ *resty.Response) (time.Duration, error) {
			log.Printf("[ss] retry: sleeping ~10s–60s before next attempt")
			return 0, nil // use resty's default exponential backoff
		})

	return &Client{
		rc: rc,
		// 100 req/min ≈ 1.667 req/sec; burst=1 to avoid bursting into 429s
		limiter: rate.NewLimiter(rate.Limit(100.0/60.0), 1),
	}
}

// reserveAndWait blocks until the rate limiter grants a token.
// Logs the sleep duration when non-zero so callers can see the delay upfront.
func reserveAndWait(ctx context.Context, limiter *rate.Limiter, tag string) error {
	r := limiter.Reserve()
	if !r.OK() {
		return fmt.Errorf("rate limiter: burst exceeded")
	}
	if d := r.Delay(); d > 0 {
		log.Printf("%s rate limiter: sleeping %s", tag, d.Round(time.Millisecond))
		select {
		case <-time.After(d):
			return nil
		case <-ctx.Done():
			r.Cancel()
			return ctx.Err()
		}
	}
	return nil
}

type ssBatchPaperRequest struct {
	IDs []string `json:"ids"`
}

type ssBatchAuthorRequest struct {
	IDs []string `json:"ids"`
}

type ssPaperResponse struct {
	PaperID       string        `json:"paperId"`
	CitationCount int           `json:"citationCount"`
	Authors       []ssAuthorRef `json:"authors"`
}

type ssAuthorRef struct {
	AuthorID string `json:"authorId"`
}

type ssAuthorResponse struct {
	HIndex     int `json:"hIndex"`
	PaperCount int `json:"paperCount"`
}

// EnrichAll enriches all papers using SS batch endpoints.
// 263 papers → 1 paper batch call + a few author batch calls (vs 1000+ sequential calls).
func (c *Client) EnrichAll(ctx context.Context, papers []*models.Paper) error {
	if len(papers) == 0 {
		return nil
	}

	// Build ID list and lookup index.
	ids := make([]string, len(papers))
	paperByKey := make(map[string]*models.Paper, len(papers))
	for i, p := range papers {
		key := "arXiv:" + p.ArxivID
		ids[i] = key
		paperByKey[key] = p
	}

	// authorIDsForPaper[arxivID] = ordered SS author IDs for that paper.
	authorIDsForPaper := make(map[string][]string, len(papers))

	// --- Phase 1: batch paper lookup ---
	for i := 0; i < len(ids); i += paperBatchSize {
		chunk := ids[i:min(i+paperBatchSize, len(ids))]

		if err := reserveAndWait(ctx, c.limiter, "[ss]"); err != nil {
			return fmt.Errorf("rate limiter: %w", err)
		}
		log.Printf("[ss] pre-batch pause 10s")
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}

		log.Printf("[ss] POST /paper/batch n=%d", len(chunk))
		t0 := time.Now()
		var paperResps []*ssPaperResponse
		resp, err := c.rc.R().
			SetContext(ctx).
			SetBody(ssBatchPaperRequest{IDs: chunk}).
			SetResult(&paperResps).
			SetQueryParam("fields", "authors,citationCount").
			Post("/paper/batch")
		log.Printf("[ss] POST /paper/batch n=%d → %d (%s)", len(chunk), resp.StatusCode(), time.Since(t0).Round(time.Millisecond))

		if err != nil {
			return fmt.Errorf("ss paper batch: %w", err)
		}
		if resp.StatusCode() != 200 {
			return fmt.Errorf("ss paper batch unexpected status %d", resp.StatusCode())
		}

		for j, pr := range paperResps {
			if pr == nil {
				continue // paper not indexed in SS
			}
			p := paperByKey[chunk[j]]
			p.SSPaperID = pr.PaperID
			citCount := pr.CitationCount
			p.CitationCount = &citCount

			aIDs := make([]string, 0, len(pr.Authors))
			for _, a := range pr.Authors {
				if a.AuthorID != "" {
					aIDs = append(aIDs, a.AuthorID)
				}
			}
			if len(aIDs) > 0 {
				authorIDsForPaper[p.ArxivID] = aIDs
			}
		}
	}

	// --- Phase 2: collect unique author IDs across all papers ---
	authorIDSet := make(map[string]struct{})
	var allAuthorIDs []string
	for _, aIDs := range authorIDsForPaper {
		for _, aID := range aIDs {
			if _, seen := authorIDSet[aID]; !seen {
				authorIDSet[aID] = struct{}{}
				allAuthorIDs = append(allAuthorIDs, aID)
			}
		}
	}

	if len(allAuthorIDs) == 0 {
		return nil
	}

	// --- Phase 3: batch author lookup ---
	authorHIndex := make(map[string]int, len(allAuthorIDs))
	authorPaperCount := make(map[string]int, len(allAuthorIDs))

	for i := 0; i < len(allAuthorIDs); i += authorBatchSize {
		chunk := allAuthorIDs[i:min(i+authorBatchSize, len(allAuthorIDs))]

		if err := reserveAndWait(ctx, c.limiter, "[ss]"); err != nil {
			return fmt.Errorf("rate limiter: %w", err)
		}
		log.Printf("[ss] pre-batch pause 10s")
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}

		log.Printf("[ss] POST /author/batch n=%d", len(chunk))
		t0 := time.Now()
		var authorResps []*ssAuthorResponse
		resp, err := c.rc.R().
			SetContext(ctx).
			SetBody(ssBatchAuthorRequest{IDs: chunk}).
			SetResult(&authorResps).
			SetQueryParam("fields", "hIndex,paperCount").
			Post("/author/batch")
		log.Printf("[ss] POST /author/batch n=%d → %d (%s)", len(chunk), resp.StatusCode(), time.Since(t0).Round(time.Millisecond))

		if err != nil {
			return fmt.Errorf("ss author batch: %w", err)
		}
		if resp.StatusCode() != 200 {
			return fmt.Errorf("ss author batch unexpected status %d", resp.StatusCode())
		}

		for j, ar := range authorResps {
			if ar == nil || j >= len(chunk) {
				continue
			}
			authorHIndex[chunk[j]] = ar.HIndex
			authorPaperCount[chunk[j]] = ar.PaperCount
		}
	}

	// --- Phase 4: assign per-paper author aggregates ---
	for arxivID, aIDs := range authorIDsForPaper {
		p := paperByKey["arXiv:"+arxivID]
		maxH := 0
		totalPapers := 0
		for _, aID := range aIDs {
			if h := authorHIndex[aID]; h > maxH {
				maxH = h
			}
			totalPapers += authorPaperCount[aID]
		}
		p.MaxHIndex = &maxH
		p.TotalPriorPapers = &totalPapers
	}

	return nil
}
