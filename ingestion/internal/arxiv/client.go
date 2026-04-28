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
	defaultQueryURL = "https://export.arxiv.org/api/query"
	pageSize        = 100
	maxRetries      = 5
	// userAgent identifies this client to Arxiv. Per Arxiv's API guidelines,
	// callers should use a descriptive User-Agent so Arxiv staff can contact
	// maintainers if a client misbehaves — the default Go UA triggers stricter
	// filtering on some Arxiv edge nodes.
	userAgent = "six-eyes-ingestion/1.0 (github.com/jeanjohnson/six-eyes; polite-crawler)"
)

// Client fetches papers from the Arxiv API.
// Rate limit: 1 req/5s (Arxiv guideline is 3s; we use 5s for bulk backfills).
type Client struct {
	queryURL string // full query endpoint; overridable in tests
	rc       *resty.Client
	limiter  *rate.Limiter
}

func NewClient() *Client {
	return &Client{
		queryURL: defaultQueryURL,
		rc: resty.New().
			SetHeader("User-Agent", userAgent).
			SetRetryCount(maxRetries).
			SetRetryWaitTime(30 * time.Second).
			SetRetryMaxWaitTime(5 * time.Minute).
			AddRetryCondition(func(r *resty.Response, err error) bool {
				return err != nil || r.StatusCode() == 429 || r.StatusCode() == 503
			}).
			SetRetryAfter(func(_ *resty.Client, r *resty.Response) (time.Duration, error) {
				log.Printf("[arxiv] retry: status=%d sleeping ~30s before next attempt", r.StatusCode())
				return 0, nil // use resty's exponential backoff
			}),
		limiter: rate.NewLimiter(rate.Limit(1.0/5.0), 1), // 1 req/5s — conservative; Arxiv guideline is 3s but bans happen fast
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
	query := fmt.Sprintf("cat:%s", category)
	var all []models.Paper
	start := 0

	for {
		page, total, err := c.fetchPage(ctx, query, start)
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

// FetchRange returns all papers submitted between `from` and `to` (inclusive)
// for the given Arxiv category. Uses a date-range search query so it does not
// need to page through papers outside the window — efficient for backfills.
func (c *Client) FetchRange(ctx context.Context, category string, from, to time.Time) ([]models.Paper, error) {
	query := fmt.Sprintf("cat:%s AND submittedDate:[%s TO %s]",
		category,
		from.UTC().Format("20060102"),
		to.UTC().Format("20060102"),
	)
	var all []models.Paper
	start := 0

	for {
		page, total, err := c.fetchPage(ctx, query, start)
		if err != nil {
			return all, fmt.Errorf("arxiv fetch page start=%d: %w", start, err)
		}
		all = append(all, page...)
		start += len(page)
		if len(page) == 0 || start >= total {
			break
		}
	}

	return all, nil
}

// fetchPage retrieves one page of results for the given searchQuery.
// Returns (papers, totalResults, error).
func (c *Client) fetchPage(ctx context.Context, searchQuery string, start int) ([]models.Paper, int, error) {
	if err := reserveAndWait(ctx, c.limiter, "[arxiv]"); err != nil {
		return nil, 0, fmt.Errorf("rate limiter: %w", err)
	}

	log.Printf("[arxiv] GET query=%q start=%d max=%d", searchQuery, start, pageSize)
	t0 := time.Now()
	resp, err := c.rc.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"search_query": searchQuery,
			"sortBy":       "submittedDate",
			"sortOrder":    "descending",
			"start":        fmt.Sprintf("%d", start),
			"max_results":  fmt.Sprintf("%d", pageSize),
		}).
		Get(c.queryURL)
	log.Printf("[arxiv] GET query=%q start=%d → %d (%s)", searchQuery, start, resp.StatusCode(), time.Since(t0).Round(time.Millisecond))

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
