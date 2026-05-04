package graph

// Unit tests for the graph package — req 8.3
//
// Note: the GraphQL resolvers are implemented in Go (graph-gophers/graphql-go),
//
// Covered here: PaperResolver field accessor methods (no DB or model required).

import (
	"testing"
	"time"

	"github.com/jeanjohnson/six-eyes/api/internal/db"
)

func makePaper() *db.Paper {
	maxH := 42
	totalPP := 100
	hfUp := 5
	return &db.Paper{
		ArxivID:          "2403.12345",
		Title:            "Attention Is All You Need",
		Abstract:         "We propose a new architecture.",
		Categories:       []string{"cs.LG", "cs.AI"},
		Authors:          []string{"Alice", "Bob"},
		SubmittedAt:      time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC),
		HasCode:          true,
		MaxHIndex:        &maxH,
		TotalPriorPapers: &totalPP,
		HFUpvotes:        &hfUp,
	}
}

func TestPaperResolver_BasicFields(t *testing.T) {
	pr := &PaperResolver{paper: makePaper(), hypeScore: 0.75, hypeTier: "hype"}

	if got := pr.ArxivId(); got != "2403.12345" {
		t.Errorf("ArxivId = %q, want %q", got, "2403.12345")
	}
	if got := pr.Title(); got != "Attention Is All You Need" {
		t.Errorf("Title = %q, want %q", got, "Attention Is All You Need")
	}
	if got := pr.Abstract(); got != "We propose a new architecture." {
		t.Errorf("Abstract mismatch: %q", got)
	}
	if got := pr.HypeScore(); got != 0.75 {
		t.Errorf("HypeScore = %v, want 0.75", got)
	}
	if got := pr.HypeTier(); got != "hype" {
		t.Errorf("HypeTier = %q, want \"hype\"", got)
	}
	if !pr.HasCode() {
		t.Error("HasCode = false, want true")
	}
}

func TestPaperResolver_SubmittedAt_ISO8601(t *testing.T) {
	pr := &PaperResolver{paper: makePaper(), hypeScore: 0, hypeTier: "low"}
	want := "2024-03-15T12:00:00Z"
	if got := pr.SubmittedAt(); got != want {
		t.Errorf("SubmittedAt = %q, want %q", got, want)
	}
}

func TestPaperResolver_Categories(t *testing.T) {
	pr := &PaperResolver{paper: makePaper(), hypeScore: 0, hypeTier: "low"}
	cats := pr.Categories()
	if len(cats) != 2 || cats[0] != "cs.LG" || cats[1] != "cs.AI" {
		t.Errorf("Categories = %v, want [cs.LG cs.AI]", cats)
	}
}

func TestPaperResolver_Authors(t *testing.T) {
	pr := &PaperResolver{paper: makePaper(), hypeScore: 0, hypeTier: "low"}
	authors := pr.Authors()
	if len(authors) != 2 || authors[0] != "Alice" || authors[1] != "Bob" {
		t.Errorf("Authors = %v, want [Alice Bob]", authors)
	}
}

func TestPaperResolver_NullableFields_Present(t *testing.T) {
	pr := &PaperResolver{paper: makePaper(), hypeScore: 0, hypeTier: "low"}

	if v := pr.MaxHIndex(); v == nil || *v != 42 {
		t.Errorf("MaxHIndex = %v, want 42", v)
	}
	if v := pr.TotalPriorPapers(); v == nil || *v != 100 {
		t.Errorf("TotalPriorPapers = %v, want 100", v)
	}
	if v := pr.HfUpvotes(); v == nil || *v != 5 {
		t.Errorf("HfUpvotes = %v, want 5", v)
	}
}

func TestPaperResolver_NullableFields_Nil(t *testing.T) {
	paper := &db.Paper{
		ArxivID:     "test-nil",
		SubmittedAt: time.Now(),
		// MaxHIndex, TotalPriorPapers, HFUpvotes all nil
	}
	pr := &PaperResolver{paper: paper, hypeScore: 0.1, hypeTier: "low"}

	if v := pr.MaxHIndex(); v != nil {
		t.Errorf("MaxHIndex should be nil, got %v", *v)
	}
	if v := pr.TotalPriorPapers(); v != nil {
		t.Errorf("TotalPriorPapers should be nil, got %v", *v)
	}
	if v := pr.HfUpvotes(); v != nil {
		t.Errorf("HfUpvotes should be nil, got %v", *v)
	}
}

func TestPaperResolver_Tiers(t *testing.T) {
	cases := []struct {
		score float64
		tier  string
	}{
		{0.9, "hype"},
		{0.5, "likely"},
		{0.05, "low"},
	}
	paper := &db.Paper{ArxivID: "x", SubmittedAt: time.Now()}
	for _, tc := range cases {
		pr := &PaperResolver{paper: paper, hypeScore: tc.score, hypeTier: tc.tier}
		if got := pr.HypeTier(); got != tc.tier {
			t.Errorf("HypeTier score=%v: got %q, want %q", tc.score, got, tc.tier)
		}
		if got := pr.HypeScore(); got != tc.score {
			t.Errorf("HypeScore: got %v, want %v", got, tc.score)
		}
	}
}
