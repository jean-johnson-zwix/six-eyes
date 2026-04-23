package arxiv

// White-box tests (same package) so we can exercise unexported helpers
// extractArxivID and parseAtom directly, in line with the principle of testing
// pure logic separately from I/O.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// newTestClient builds a Client pointed at the given URL with no retries and
// no rate limiting — suitable for unit tests.
func newTestClient(queryURL string) *Client {
	return &Client{
		queryURL: queryURL,
		rc:       resty.New().SetRetryCount(0),
		limiter:  rate.NewLimiter(rate.Inf, 0),
	}
}

// ---- Pure logic: extractArxivID ----

func TestExtractArxivID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"v1 suffix stripped",           "http://arxiv.org/abs/2401.12345v1",  "2401.12345"},
		{"v2 suffix stripped",           "http://arxiv.org/abs/2401.12345v2",  "2401.12345"},
		{"v10 double-digit stripped",    "http://arxiv.org/abs/2401.12345v10", "2401.12345"},
		{"old-style cs/ ID",             "http://arxiv.org/abs/cs/0601001v1",  "cs/0601001"},
		{"no version suffix",            "http://arxiv.org/abs/2401.12345",    "2401.12345"},
		{"no /abs/ segment → empty",     "http://arxiv.org/pdf/2401.12345",    ""},
		{"empty string → empty",         "",                                    ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractArxivID(tt.input))
		})
	}
}

// ---- Pure logic: parseAtom ----

// fixtureAtom is a minimal but representative Arxiv Atom response containing
// two entries: one recent and one older. The opensearch namespace is included
// so the TotalResults field is correctly populated.
const fixtureAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"
      xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">
  <opensearch:totalResults>2</opensearch:totalResults>
  <entry>
    <id>http://arxiv.org/abs/2401.12345v1</id>
    <title>  Attention Is All You Need: A Survey  </title>
    <summary>  This paper surveys attention mechanisms.  </summary>
    <published>2024-01-20T00:00:00Z</published>
    <updated>2024-01-21T00:00:00Z</updated>
    <author><name>Alice Smith</name></author>
    <author><name>Bob Jones</name></author>
    <category term="cs.LG" scheme="http://arxiv.org/schemas/atom"/>
    <category term="cs.AI" scheme="http://arxiv.org/schemas/atom"/>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2401.11111v1</id>
    <title>Older Paper</title>
    <summary>An older abstract.</summary>
    <published>2024-01-18T00:00:00Z</published>
    <updated>2024-01-18T00:00:00Z</updated>
    <author><name>Carol White</name></author>
    <category term="cs.CV" scheme="http://arxiv.org/schemas/atom"/>
  </entry>
</feed>`

func TestParseAtom_ParsesAllFields(t *testing.T) {
	papers, total, err := parseAtom([]byte(fixtureAtom))
	require.NoError(t, err)

	assert.Equal(t, 2, total, "TotalResults should be parsed from opensearch namespace")
	assert.Len(t, papers, 2)

	p := papers[0]
	assert.Equal(t, "2401.12345", p.ArxivID)
	assert.Equal(t, "Attention Is All You Need: A Survey", p.Title, "leading/trailing whitespace should be trimmed")
	assert.Equal(t, "This paper surveys attention mechanisms.", p.Abstract)
	assert.Equal(t, []string{"cs.LG", "cs.AI"}, p.Categories)
	require.Len(t, p.Authors, 2)
	assert.Equal(t, "Alice Smith", p.Authors[0].Name)
	assert.Equal(t, "Bob Jones", p.Authors[1].Name)
	assert.Equal(t, time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC), p.SubmittedAt)
	assert.Equal(t, time.Date(2024, 1, 21, 0, 0, 0, 0, time.UTC), p.UpdatedAtAPI)
}

func TestParseAtom_SkipsEntryWithInvalidID(t *testing.T) {
	// An entry whose <id> doesn't contain "/abs/" should be silently skipped.
	badFeed := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"
      xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">
  <opensearch:totalResults>1</opensearch:totalResults>
  <entry>
    <id>http://arxiv.org/pdf/2401.12345v1</id>
    <title>Bad Entry</title>
    <summary>No /abs/ in URL.</summary>
    <published>2024-01-20T00:00:00Z</published>
    <updated>2024-01-20T00:00:00Z</updated>
    <author><name>Nobody</name></author>
    <category term="cs.LG" scheme="http://arxiv.org/schemas/atom"/>
  </entry>
</feed>`

	papers, _, err := parseAtom([]byte(badFeed))
	require.NoError(t, err)
	assert.Empty(t, papers, "entry with invalid ID should be skipped")
}

func TestParseAtom_ReturnsErrorOnMalformedXML(t *testing.T) {
	_, _, err := parseAtom([]byte(`<feed><entry><broken`))
	assert.Error(t, err)
}

// ---- HTTP boundary: FetchSince ----

func TestFetchSince_ReturnsOnlyPapersAfterCutoff(t *testing.T) {
	// The fixture has paper1 (Jan 20) and paper2 (Jan 18).
	// With since=Jan 19, only paper1 should be returned.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, fixtureAtom)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	since := time.Date(2024, 1, 19, 0, 0, 0, 0, time.UTC)

	papers, err := c.FetchSince(context.Background(), "cs.LG", since)
	require.NoError(t, err)
	require.Len(t, papers, 1)
	assert.Equal(t, "2401.12345", papers[0].ArxivID)
}

func TestFetchSince_ReturnsAllPapersWhenAllAreRecent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, fixtureAtom)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	// since = Jan 1 → both papers pass the cutoff
	since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	papers, err := c.FetchSince(context.Background(), "cs.LG", since)
	require.NoError(t, err)
	assert.Len(t, papers, 2)
}

func TestFetchSince_ReturnsErrorOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FetchSince(context.Background(), "cs.LG", time.Now().Add(-24*time.Hour))
	assert.Error(t, err)
}

func TestFetchSince_ReturnsEmptySliceWhenNoRecentPapers(t *testing.T) {
	// since = tomorrow → all papers are older than the cutoff
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, fixtureAtom)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	since := time.Date(2024, 1, 22, 0, 0, 0, 0, time.UTC) // after both fixture papers

	papers, err := c.FetchSince(context.Background(), "cs.LG", since)
	require.NoError(t, err)
	assert.Empty(t, papers)
}
