package github

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"golang.org/x/time/rate"
)

const apiBase = "https://api.github.com"

// Client fetches repository metadata from the GitHub REST API.
// Authenticated requests (GITHUB_TOKEN set) allow 5 000 req/hr; unauthenticated
// requests are capped at 60 req/hr, so a token is strongly recommended.
type Client struct {
	rc      *resty.Client
	limiter *rate.Limiter
}

func NewClient(token string) *Client {
	rc := resty.New().
		SetBaseURL(apiBase).
		SetHeader("Accept", "application/vnd.github+json").
		SetRetryCount(3).
		SetRetryWaitTime(10 * time.Second).
		AddRetryCondition(func(r *resty.Response, err error) bool {
			return err != nil || r.StatusCode() == 429 || r.StatusCode() == 403
		}).
		SetRetryAfter(func(_ *resty.Client, _ *resty.Response) (time.Duration, error) {
			log.Printf("[gh] retry: sleeping before next attempt")
			return 0, nil
		})

	var limit rate.Limit
	if token != "" {
		rc.SetAuthToken(token)
		limit = rate.Limit(80.0 / 60.0) // ~80 req/min (safely under 5 000/hr)
	} else {
		limit = rate.Limit(1.0 / 60.0) // 1 req/min (safely under 60/hr)
	}

	return &Client{
		rc:      rc,
		limiter: rate.NewLimiter(limit, 1),
	}
}

type repoResponse struct {
	StargazersCount int `json:"stargazers_count"`
}

// FetchStars returns the current star count for a GitHub repository.
// repoURL must be in the form "https://github.com/owner/repo".
// Returns 0 (no error) when the repo is not found (deleted / renamed).
func (c *Client) FetchStars(ctx context.Context, repoURL string) (int, error) {
	owner, repo, err := parseGitHubURL(repoURL)
	if err != nil {
		return 0, fmt.Errorf("parse repo url %q: %w", repoURL, err)
	}

	if err := reserveAndWait(ctx, c.limiter, "[gh]"); err != nil {
		return 0, err
	}

	endpoint := fmt.Sprintf("/repos/%s/%s", owner, repo)
	log.Printf("[gh] GET %s", endpoint)
	t0 := time.Now()
	var result repoResponse
	resp, err := c.rc.R().
		SetContext(ctx).
		SetResult(&result).
		Get(endpoint)
	log.Printf("[gh] GET %s → %d (%s)", endpoint, resp.StatusCode(), time.Since(t0).Round(time.Millisecond))

	if err != nil {
		return 0, fmt.Errorf("github api: %w", err)
	}
	if resp.StatusCode() == 404 {
		return 0, nil // repo deleted or renamed; treat as 0 stars
	}
	if resp.StatusCode() != 200 {
		return 0, fmt.Errorf("github api unexpected status %d for %s", resp.StatusCode(), repoURL)
	}
	return result.StargazersCount, nil
}

// parseGitHubURL extracts the owner and repo name from a GitHub URL.
// Handles trailing slashes, .git suffixes, and subdirectory paths.
func parseGitHubURL(rawURL string) (owner, repo string, err error) {
	rawURL = strings.TrimRight(rawURL, "/")
	rawURL = strings.TrimSuffix(rawURL, ".git")

	const prefix = "https://github.com/"
	if !strings.HasPrefix(rawURL, prefix) {
		return "", "", fmt.Errorf("not a github.com URL: %s", rawURL)
	}
	parts := strings.SplitN(strings.TrimPrefix(rawURL, prefix), "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot parse owner/repo from: %s", rawURL)
	}
	return parts[0], parts[1], nil
}

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
