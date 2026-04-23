package arxiv

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/jeanjohnson/six-eyes/ingestion/internal/models"
	"golang.org/x/time/rate"
)

const (
	defaultQueryURL = "http://export.arxiv.org/api/query"
	pageSize        = 100
	maxRetries      = 3
)

// Client fetches papers from the Arxiv API.
// Rate limit: 3 requests/sec (no API key required).
type Client struct {
	queryURL string // full query endpoint; overridable in tests
	rc       *resty.Client
	limiter  *rate.Limiter
}

func NewClient() *Client {
	return &Client{
		queryURL: defaultQueryURL,
		rc: resty.New().
			SetRetryCount(maxRetries).
			SetRetryWaitTime(2 * time.Second).
			SetRetryAfter(func(_ *resty.Client, _ *resty.Response) (time.Duration, error) {
				log.Printf("[arxiv] retry: sleeping ~2s before next attempt")
				return 0, nil // use resty's default backoff
			}),
		limiter: rate.NewLimiter(3, 1),
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

// FetchSince returns all papers submitted on or after `since` for the given
// Arxiv category. Stops paginating once it encounters a paper older than `since`.
func (c *Client) FetchSince(ctx context.Context, category string, since time.Time) ([]models.Paper, error) {
	var all []models.Paper
	start := 0

	for {
		page, total, err := c.fetchPage(ctx, category, start)
		if err != nil {
			return all, fmt.Errorf("arxiv fetch page start=%d: %w", start, err)
		}

		done := false
		for _, p := range page {
			if p.SubmittedAt.Before(since) {
				done = true
				break
			}
			all = append(all, p)
		}

		start += len(page)
		if done || len(page) == 0 || start >= total {
			break
		}
	}

	return all, nil
}

// fetchPage retrieves one page of results. Returns (papers, totalResults, error).
func (c *Client) fetchPage(ctx context.Context, category string, start int) ([]models.Paper, int, error) {
	if err := reserveAndWait(ctx, c.limiter, "[arxiv]"); err != nil {
		return nil, 0, fmt.Errorf("rate limiter: %w", err)
	}

	log.Printf("[arxiv] GET query cat=%s start=%d max=%d", category, start, pageSize)
	t0 := time.Now()
	resp, err := c.rc.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"search_query": fmt.Sprintf("cat:%s", category),
			"sortBy":       "submittedDate",
			"sortOrder":    "descending",
			"start":        fmt.Sprintf("%d", start),
			"max_results":  fmt.Sprintf("%d", pageSize),
		}).
		Get(c.queryURL)
	log.Printf("[arxiv] GET query cat=%s start=%d → %d (%s)", category, start, resp.StatusCode(), time.Since(t0).Round(time.Millisecond))

	if err != nil {
		return nil, 0, fmt.Errorf("http get: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, 0, fmt.Errorf("unexpected status %d", resp.StatusCode())
	}

	return parseAtom(resp.Body())
}

// ---- Atom XML parsing ----

type atomFeed struct {
	XMLName      xml.Name    `xml:"feed"`
	TotalResults int         `xml:"http://a9.com/-/spec/opensearch/1.1/ totalResults"`
	Entries      []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID         string       `xml:"id"`
	Title      string       `xml:"title"`
	Summary    string       `xml:"summary"`
	Published  time.Time    `xml:"published"`
	Updated    time.Time    `xml:"updated"`
	Authors    []atomAuthor `xml:"author"`
	Categories []atomCat    `xml:"category"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomCat struct {
	Term string `xml:"term,attr"`
}

func parseAtom(body []byte) ([]models.Paper, int, error) {
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, 0, fmt.Errorf("xml unmarshal: %w", err)
	}

	papers := make([]models.Paper, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		arxivID := extractArxivID(e.ID)
		if arxivID == "" {
			continue
		}

		authors := make([]models.Author, len(e.Authors))
		for i, a := range e.Authors {
			authors[i] = models.Author{Name: strings.TrimSpace(a.Name)}
		}

		cats := make([]string, len(e.Categories))
		for i, c := range e.Categories {
			cats[i] = c.Term
		}

		papers = append(papers, models.Paper{
			ArxivID:      arxivID,
			Title:        strings.TrimSpace(e.Title),
			Abstract:     strings.TrimSpace(e.Summary),
			Categories:   cats,
			Authors:      authors,
			SubmittedAt:  e.Published.UTC(),
			UpdatedAtAPI: e.Updated.UTC(),
		})
	}

	return papers, feed.TotalResults, nil
}

// extractArxivID converts "http://arxiv.org/abs/2401.12345v1" → "2401.12345".
func extractArxivID(rawID string) string {
	parts := strings.Split(rawID, "/abs/")
	if len(parts) != 2 {
		return ""
	}
	// Strip version suffix (v1, v2, ...)
	id := parts[1]
	if idx := strings.LastIndex(id, "v"); idx != -1 {
		// Only strip if what follows "v" is all digits
		suffix := id[idx+1:]
		allDigits := len(suffix) > 0
		for _, ch := range suffix {
			if ch < '0' || ch > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			id = id[:idx]
		}
	}
	return strings.TrimSpace(id)
}
