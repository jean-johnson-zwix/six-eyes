package models

import "time"

// Author is a paper author as stored in the JSONB authors column.
type Author struct {
	Name string `json:"name"`
}

// Paper is the central data structure populated by the three API clients and
// written to Postgres by the DB store. Zero values for enrichment fields
// (0, false, "") indicate unknown — distinguished from real data by EnrichedAt
// being nil when enrichment never ran.
type Paper struct {
	// Arxiv fields
	ArxivID      string
	Title        string
	Abstract     string
	Categories   []string
	Authors      []Author
	SubmittedAt  time.Time
	UpdatedAtAPI time.Time

	// Semantic Scholar enrichment (nil/zero when enrichment failed or not yet run)
	SSPaperID        string
	CitationCount    *int
	MaxHIndex        *int
	TotalPriorPapers *int

	// HuggingFace enrichment
	HFPaperID    string // arxiv ID if found, "checked_none" if looked up and absent
	HFUpvotes    *int   // upvote count at most recent enrichment run
	HFGithubRepo string // linked GitHub repo URL, empty if none
	HasCode      bool   // true when HFGithubRepo is non-empty

	// Target labels — populated by the T+60 label job, never by ingestion
	GitHubStarsT60 *int
	HypeLabel      *bool // true = stars > 100 at T+60

	// Set to non-nil when at least one enrichment source succeeded
	EnrichedAt *time.Time
}
