package hf

// White-box tests (same package) so we can construct Client directly.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/jeanjohnson/six-eyes/ingestion/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(baseURL string) *Client {
	return &Client{
		rc:      resty.New().SetBaseURL(baseURL).SetRetryCount(0),
		sleepFn: func(_ context.Context) error { return nil }, // no-op: keep tests fast
	}
}

func newPaper(arxivID string) *models.Paper {
	return &models.Paper{ArxivID: arxivID, SubmittedAt: time.Now()}
}

// ---- TestEnrich_PaperOnHF ----
// Happy path: paper found on HF with upvotes and a linked GitHub repo.

func TestEnrich_PaperOnHF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"2401.12345","upvotes":42,"githubRepo":"https://github.com/user/repo"}`)
	}))
	defer srv.Close()

	p := newPaper("2401.12345")
	err := newTestClient(srv.URL).Enrich(context.Background(), p)

	require.NoError(t, err)
	assert.Equal(t, "2401.12345", p.HFPaperID)
	require.NotNil(t, p.HFUpvotes)
	assert.Equal(t, 42, *p.HFUpvotes)
	assert.Equal(t, "https://github.com/user/repo", p.HFGithubRepo)
	assert.True(t, p.HasCode)
}

// ---- TestEnrich_PaperOnHFNoRepo ----
// Paper is on HF but has no linked GitHub repo yet. HasCode should be false.

func TestEnrich_PaperOnHFNoRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"2401.12345","upvotes":10,"githubRepo":""}`)
	}))
	defer srv.Close()

	p := newPaper("2401.12345")
	err := newTestClient(srv.URL).Enrich(context.Background(), p)

	require.NoError(t, err)
	assert.Equal(t, "2401.12345", p.HFPaperID)
	assert.False(t, p.HasCode)
	assert.Empty(t, p.HFGithubRepo)
}

// ---- TestEnrich_PaperNotOnHF ----
// 404 means the paper has not been submitted to / featured on HF. Not an error,
// but HFPaperID should be set to CheckedNone so future runs skip the lookup.

func TestEnrich_PaperNotOnHF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := newPaper("2401.99999")
	err := newTestClient(srv.URL).Enrich(context.Background(), p)

	assert.NoError(t, err, "404 should be a no-op, not an error")
	assert.Equal(t, CheckedNone, p.HFPaperID, "should mark as checked so future runs skip it")
	assert.False(t, p.HasCode)
}

// ---- TestEnrich_ServerError ----
// A 5xx is an infrastructure failure and should propagate as an error.

func TestEnrich_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := newPaper("2401.12345")
	err := newTestClient(srv.URL).Enrich(context.Background(), p)

	assert.Error(t, err)
}

// ---- TestHFSkipPredicate ----
// The skip predicate used in main.go. Three cases:
//   real HFPaperID        → skip (already on HF, upvotes only go up)
//   checked_none + > 7d   → skip (not featured after a week, won't be)
//   checked_none + ≤ 7d   → re-check (may appear in first few days)
//   empty                 → check (never looked up)

func TestHFSkipPredicate(t *testing.T) {
	skip := func(p *models.Paper) bool {
		alreadyFound := p.HFPaperID != "" && p.HFPaperID != CheckedNone
		tooOld := p.HFPaperID == CheckedNone && time.Since(p.SubmittedAt) > CheckedNoneMaxAge
		return alreadyFound || tooOld
	}

	featured := &models.Paper{HFPaperID: "2401.12345", SubmittedAt: time.Now()}
	assert.True(t, skip(featured), "paper already on HF should be skipped")

	oldAbsent := &models.Paper{HFPaperID: CheckedNone, SubmittedAt: time.Now().Add(-8 * 24 * time.Hour)}
	assert.True(t, skip(oldAbsent), "8-day-old checked_none paper should be skipped")

	recentAbsent := &models.Paper{HFPaperID: CheckedNone, SubmittedAt: time.Now().Add(-3 * 24 * time.Hour)}
	assert.False(t, skip(recentAbsent), "3-day-old checked_none paper should be re-checked")

	neverChecked := &models.Paper{HFPaperID: "", SubmittedAt: time.Now().Add(-30 * 24 * time.Hour)}
	assert.False(t, skip(neverChecked), "paper never checked should not be skipped")
}
