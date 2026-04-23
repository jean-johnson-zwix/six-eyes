package hf

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/jeanjohnson/six-eyes/ingestion/internal/models"
)

const (
	baseURL             = "https://huggingface.co/api"
	minDelay            = 3 * time.Second
	maxDelay            = 5 * time.Second
	CheckedNone         = "checked_none"     // sentinel: looked up, not found on HF
	CheckedNoneMaxAge   = 7 * 24 * time.Hour // re-check papers newer than this
)

// Client enriches papers with HuggingFace metadata: upvote count and linked
// GitHub repository. Public unauthenticated API; no key required.
type Client struct {
	rc      *resty.Client
	sleepFn func(ctx context.Context) error // injectable for tests
}

func NewClient() *Client {
	return &Client{
		rc: resty.New().
			SetBaseURL(baseURL).
			SetRetryCount(3).
			SetRetryWaitTime(5 * time.Second).
			SetRetryAfter(func(_ *resty.Client, _ *resty.Response) (time.Duration, error) {
				log.Printf("[hf] retry: sleeping ~5s before next attempt")
				return 0, nil // use resty's default backoff
			}),
		sleepFn: jitterSleep,
	}
}

// jitterSleep sleeps for a random duration in [minDelay, maxDelay] and logs it.
func jitterSleep(ctx context.Context) error {
	d := minDelay + time.Duration(rand.Int63n(int64(maxDelay-minDelay)))
	log.Printf("[hf] sleeping %s before next request", d.Round(time.Millisecond))
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type hfPaperResponse struct {
	ID         string `json:"id"`
	Upvotes    int    `json:"upvotes"`
	GithubRepo string `json:"githubRepo"`
}

// DailyPaper is a paper returned by the HF daily papers endpoint.
type DailyPaper struct {
	ArxivID    string
	Upvotes    int
	GithubRepo string
}

type hfDailyEntry struct {
	Paper struct {
		ID         string `json:"id"`
		Upvotes    int    `json:"upvotes"`
		GithubRepo string `json:"githubRepo"`
	} `json:"paper"`
}

// FetchDailyPapers returns the papers featured on HuggingFace for the given
// date (format: YYYY-MM-DD). Typically ~50 papers per day. Single HTTP call —
// no jitter needed.
func (c *Client) FetchDailyPapers(ctx context.Context, date time.Time) ([]DailyPaper, error) {
	dateStr := date.Format("2006-01-02")
	log.Printf("[hf] GET /daily_papers?date=%s", dateStr)
	t0 := time.Now()
	var entries []hfDailyEntry
	resp, err := c.rc.R().
		SetContext(ctx).
		SetResult(&entries).
		SetQueryParam("date", dateStr).
		Get("/daily_papers")
	log.Printf("[hf] GET /daily_papers?date=%s → %d n=%d (%s)", dateStr, resp.StatusCode(), len(entries), time.Since(t0).Round(time.Millisecond))

	if err != nil {
		return nil, fmt.Errorf("hf daily papers request: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("hf daily papers unexpected status %d", resp.StatusCode())
	}

	papers := make([]DailyPaper, 0, len(entries))
	for _, e := range entries {
		if e.Paper.ID == "" {
			continue
		}
		papers = append(papers, DailyPaper{
			ArxivID:    e.Paper.ID,
			Upvotes:    e.Paper.Upvotes,
			GithubRepo: e.Paper.GithubRepo,
		})
	}
	return papers, nil
}

// Enrich populates the HuggingFace fields on p. Sleeps before the request to
// respect rate limits. Sets HFPaperID to CheckedNone when the paper is not on
// HF — stored in the DB so future runs can skip the lookup.
func (c *Client) Enrich(ctx context.Context, p *models.Paper) error {
	if err := c.sleepFn(ctx); err != nil {
		return ctx.Err()
	}

	endpoint := fmt.Sprintf("/papers/%s", p.ArxivID)
	log.Printf("[hf] GET %s", endpoint)
	t0 := time.Now()
	var paperResp hfPaperResponse
	resp, err := c.rc.R().
		SetContext(ctx).
		SetResult(&paperResp).
		Get(endpoint)
	log.Printf("[hf] GET %s → %d (%s)", endpoint, resp.StatusCode(), time.Since(t0).Round(time.Millisecond))

	if err != nil {
		return fmt.Errorf("hf paper request: %w", err)
	}
	if resp.StatusCode() == 404 {
		p.HFPaperID = CheckedNone // mark so future runs skip this paper
		return nil
	}
	if resp.StatusCode() != 200 {
		return fmt.Errorf("hf paper unexpected status %d for %s", resp.StatusCode(), p.ArxivID)
	}

	p.HFPaperID = p.ArxivID // confirmed present on HF
	p.HFUpvotes = &paperResp.Upvotes
	p.HFGithubRepo = paperResp.GithubRepo
	p.HasCode = paperResp.GithubRepo != ""
	return nil
}
