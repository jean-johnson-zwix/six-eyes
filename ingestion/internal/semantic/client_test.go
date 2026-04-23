package semantic

// White-box tests (same package) so we can construct Client directly with a
// test server URL and no rate limiting, without needing exported constructors.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/jeanjohnson/six-eyes/ingestion/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// newTestClient builds a Client pointed at the given base URL with no retries
// and no rate limiting — suitable for unit tests.
func newTestClient(baseURL string) *Client {
	return &Client{
		rc:      resty.New().SetBaseURL(baseURL).SetRetryCount(0),
		limiter: rate.NewLimiter(rate.Inf, 1),
	}
}

func newPaper(arxivID string) *models.Paper {
	return &models.Paper{ArxivID: arxivID, SubmittedAt: time.Now()}
}

// ---- TestEnrichAll_PaperFound ----
// Happy path: paper and both authors are found. Verifies CitationCount,
// MaxHIndex (max across authors), and TotalPriorPapers (sum of paperCounts).

func TestEnrichAll_PaperFound(t *testing.T) {
	authorData := map[string]ssAuthorResponse{
		"auth-1": {HIndex: 15, PaperCount: 30},
		"auth-2": {HIndex: 8, PaperCount: 12},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/paper/batch":
			fmt.Fprint(w, `[{"paperId":"ss-paper-001","citationCount":42,"authors":[{"authorId":"auth-1"},{"authorId":"auth-2"}]}]`)
		case "/author/batch":
			var req ssBatchAuthorRequest
			json.NewDecoder(r.Body).Decode(&req)
			resps := make([]ssAuthorResponse, len(req.IDs))
			for i, id := range req.IDs {
				resps[i] = authorData[id]
			}
			json.NewEncoder(w).Encode(resps)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := newPaper("2401.12345")
	err := newTestClient(srv.URL).EnrichAll(context.Background(), []*models.Paper{p})

	require.NoError(t, err)
	assert.Equal(t, "ss-paper-001", p.SSPaperID)
	require.NotNil(t, p.CitationCount)
	assert.Equal(t, 42, *p.CitationCount)
	require.NotNil(t, p.MaxHIndex)
	assert.Equal(t, 15, *p.MaxHIndex, "should be the max h-index across all authors")
	require.NotNil(t, p.TotalPriorPapers)
	assert.Equal(t, 42, *p.TotalPriorPapers, "should be the sum of all authors' paper counts")
}

// ---- TestEnrichAll_PaperNotFound ----
// A null entry in the batch response means the paper is not yet indexed in
// Semantic Scholar — should be a no-op (nil error), not a failure.

func TestEnrichAll_PaperNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/paper/batch" {
			fmt.Fprint(w, `[null]`)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := newPaper("2401.99999")
	err := newTestClient(srv.URL).EnrichAll(context.Background(), []*models.Paper{p})

	assert.NoError(t, err, "null entry (paper not in SS) should be a no-op, not an error")
	assert.Nil(t, p.CitationCount, "enrichment fields should remain nil")
	assert.Nil(t, p.MaxHIndex)
	assert.Empty(t, p.SSPaperID)
}

// ---- TestEnrichAll_ServerError ----
// A 5xx response on the paper batch is an infrastructure failure and should be
// returned as an error so main.go can log it and decide what to do.

func TestEnrichAll_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := newPaper("2401.12345")
	err := newTestClient(srv.URL).EnrichAll(context.Background(), []*models.Paper{p})

	assert.Error(t, err)
}

// ---- TestEnrichAll_AuthorFetchFails ----
// When the paper is found but the author batch returns null entries, CitationCount
// is still set and MaxHIndex defaults to 0. The error is non-fatal.

func TestEnrichAll_AuthorFetchFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/paper/batch":
			fmt.Fprint(w, `[{"paperId":"ss-paper-002","citationCount":10,"authors":[{"authorId":"auth-bad"}]}]`)
		case "/author/batch":
			// Simulate author not found: null entry (SS batch API returns null per missing item)
			fmt.Fprint(w, `[null]`)
		}
	}))
	defer srv.Close()

	p := newPaper("2401.12345")
	err := newTestClient(srv.URL).EnrichAll(context.Background(), []*models.Paper{p})

	require.NoError(t, err, "null author entry should not fail the whole enrichment")
	require.NotNil(t, p.CitationCount)
	assert.Equal(t, 10, *p.CitationCount, "citation count from paper response should be set")
	require.NotNil(t, p.MaxHIndex)
	assert.Equal(t, 0, *p.MaxHIndex, "h-index defaults to 0 when all author entries are null")
}

// ---- TestEnrichAll_SkipsAuthorWithEmptyID ----
// Authors with an empty authorId should be excluded from the author batch
// request entirely — no /author/batch call should be made.

func TestEnrichAll_SkipsAuthorWithEmptyID(t *testing.T) {
	authorCallCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/paper/batch":
			fmt.Fprint(w, `[{"paperId":"ss-paper-003","citationCount":5,"authors":[{"authorId":""}]}]`)
		case "/author/batch":
			authorCallCount++
			fmt.Fprint(w, `[{"hIndex":99,"paperCount":99}]`)
		}
	}))
	defer srv.Close()

	p := newPaper("2401.12345")
	require.NoError(t, newTestClient(srv.URL).EnrichAll(context.Background(), []*models.Paper{p}))
	assert.Equal(t, 0, authorCallCount, "should not call author batch for empty authorId")
}
