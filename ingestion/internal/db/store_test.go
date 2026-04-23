package db

// This file contains two test suites:
//
//  1. Unit tests for pure helper logic (nullString) — always run.
//  2. Integration tests for UpsertPaper against a real Postgres connection.
//     These are skipped automatically when SUPABASE_DB_URL is not set,
//     so they never block a local `go test ./...` run without a database.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jeanjohnson/six-eyes/ingestion/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- Unit tests: pure logic ----

func TestNullString_ReturnsNilForEmptyString(t *testing.T) {
	assert.Nil(t, nullString(""), "empty string should become nil (SQL NULL)")
}

func TestNullString_ReturnsPointerForNonEmpty(t *testing.T) {
	result := nullString("hello")
	require.NotNil(t, result)
	assert.Equal(t, "hello", *result)
}

// ---- Integration tests: UpsertPaper ----
// Skipped when SUPABASE_DB_URL is not set.

func requireDBURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("SUPABASE_DB_URL")
	if url == "" {
		t.Skip("SUPABASE_DB_URL not set — skipping DB integration tests")
	}
	return url
}

// testPaper returns a Paper with all fields populated, using a unique arxiv_id
// so tests can be run multiple times without conflicts.
func testPaper(arxivID string) *models.Paper {
	citCount := 42
	maxH := 15
	totalPapers := 30
	upvotes := 87
	now := time.Now().UTC().Truncate(time.Millisecond)

	return &models.Paper{
		ArxivID:     arxivID,
		Title:       "Test Paper: " + arxivID,
		Abstract:    "A test abstract.",
		Categories:  []string{"cs.LG", "cs.AI"},
		Authors:     []models.Author{{Name: "Alice"}, {Name: "Bob"}},
		SubmittedAt: now.Add(-24 * time.Hour),
		UpdatedAtAPI: now.Add(-23 * time.Hour),

		SSPaperID:        "ss-" + arxivID,
		CitationCount:    &citCount,
		MaxHIndex:        &maxH,
		TotalPriorPapers: &totalPapers,

		HasCode:      true,
		HFPaperID:    arxivID,
		HFUpvotes:    &upvotes,
		HFGithubRepo: "https://github.com/user/" + arxivID,

		EnrichedAt: &now,
	}
}

func TestUpsertPaper_InsertsNewRecord(t *testing.T) {
	ctx := context.Background()
	pool, err := NewPool(ctx, requireDBURL(t))
	require.NoError(t, err)
	defer pool.Close()

	store := NewStore(pool)
	arxivID := "test-insert-" + time.Now().Format("20060102150405")

	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM papers WHERE arxiv_id = $1", arxivID)
	})

	p := testPaper(arxivID)
	require.NoError(t, store.UpsertPaper(ctx, p))

	// Read it back and verify the core fields were persisted.
	var (
		gotTitle         string
		gotCitationCount *int
		gotHasCode       bool
	)
	err = pool.QueryRow(ctx,
		"SELECT title, citation_count, has_code FROM papers WHERE arxiv_id = $1",
		arxivID,
	).Scan(&gotTitle, &gotCitationCount, &gotHasCode)

	require.NoError(t, err)
	assert.Equal(t, p.Title, gotTitle)
	require.NotNil(t, gotCitationCount)
	assert.Equal(t, 42, *gotCitationCount)
	assert.True(t, gotHasCode)
}

func TestUpsertPaper_CoalescePreservesEnrichmentOnRerun(t *testing.T) {
	// Simulates a scenario where the first run succeeds with full enrichment,
	// then a second run (e.g. after an SS/PWC outage) writes nil enrichment
	// fields. The COALESCE in the upsert SQL should preserve the original values.
	ctx := context.Background()
	pool, err := NewPool(ctx, requireDBURL(t))
	require.NoError(t, err)
	defer pool.Close()

	store := NewStore(pool)
	arxivID := "test-coalesce-" + time.Now().Format("20060102150405")

	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM papers WHERE arxiv_id = $1", arxivID)
	})

	// First upsert: full enrichment.
	p1 := testPaper(arxivID)
	require.NoError(t, store.UpsertPaper(ctx, p1))

	// Second upsert: enrichment failed — all enrichment fields are nil.
	p2 := &models.Paper{
		ArxivID:     arxivID,
		Title:       "Updated Title",
		Abstract:    "Updated abstract.",
		Categories:  []string{"cs.LG"},
		Authors:     []models.Author{{Name: "Alice"}},
		SubmittedAt: p1.SubmittedAt,
		// All enrichment fields intentionally nil / zero
	}
	require.NoError(t, store.UpsertPaper(ctx, p2))

	// Read back: title should be updated, but enrichment fields preserved.
	var (
		gotTitle         string
		gotCitationCount *int
		gotSSPaperID     *string
		gotEnrichedAt    *time.Time
	)
	err = pool.QueryRow(ctx,
		"SELECT title, citation_count, ss_paper_id, enriched_at FROM papers WHERE arxiv_id = $1",
		arxivID,
	).Scan(&gotTitle, &gotCitationCount, &gotSSPaperID, &gotEnrichedAt)

	require.NoError(t, err)
	assert.Equal(t, "Updated Title", gotTitle, "title should be overwritten (Arxiv data is authoritative)")
	require.NotNil(t, gotCitationCount, "citation_count should be preserved by COALESCE")
	assert.Equal(t, 42, *gotCitationCount)
	require.NotNil(t, gotSSPaperID)
	assert.Equal(t, "ss-"+arxivID, *gotSSPaperID)
	assert.NotNil(t, gotEnrichedAt, "enriched_at should be preserved by COALESCE")
}
