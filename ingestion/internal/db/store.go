package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jeanjohnson/six-eyes/ingestion/internal/models"
)

const upsertSQL = `
INSERT INTO papers (
    arxiv_id, title, abstract, categories, authors,
    submitted_at, updated_at_api,
    ss_paper_id, citation_count, max_h_index, total_prior_papers,
    has_code, hf_paper_id, hf_upvotes, hf_github_repo,
    github_stars_t60, hype_label,
    enriched_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7,
    $8, $9, $10, $11,
    $12, $13, $14, $15,
    $16, $17,
    $18
)
ON CONFLICT (arxiv_id) DO UPDATE SET
    title                = EXCLUDED.title,
    abstract             = EXCLUDED.abstract,
    categories           = EXCLUDED.categories,
    authors              = EXCLUDED.authors,
    updated_at_api       = EXCLUDED.updated_at_api,
    ss_paper_id          = COALESCE(EXCLUDED.ss_paper_id,          papers.ss_paper_id),
    citation_count       = COALESCE(EXCLUDED.citation_count,       papers.citation_count),
    max_h_index          = COALESCE(EXCLUDED.max_h_index,          papers.max_h_index),
    total_prior_papers   = COALESCE(EXCLUDED.total_prior_papers,   papers.total_prior_papers),
    has_code             = COALESCE(EXCLUDED.has_code,             papers.has_code),
    hf_paper_id          = COALESCE(EXCLUDED.hf_paper_id,          papers.hf_paper_id),
    hf_upvotes           = COALESCE(EXCLUDED.hf_upvotes,           papers.hf_upvotes),
    hf_github_repo       = COALESCE(EXCLUDED.hf_github_repo,       papers.hf_github_repo),
    github_stars_t60     = COALESCE(papers.github_stars_t60,       EXCLUDED.github_stars_t60),
    hype_label           = COALESCE(EXCLUDED.hype_label,           papers.hype_label),
    enriched_at          = COALESCE(EXCLUDED.enriched_at,          papers.enriched_at)
`

// Store writes papers to Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// NewPool opens a pgx connection pool. MaxConns is capped for Supabase free tier.
func NewPool(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}
	cfg.MaxConns = 5
	return pgxpool.NewWithConfig(ctx, cfg)
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// UpsertPaper inserts or updates a paper record. Enrichment fields use COALESCE
// so a re-run that fails enrichment does not overwrite previously stored values.
func (s *Store) UpsertPaper(ctx context.Context, p *models.Paper) error {
	authorsJSON, err := json.Marshal(p.Authors)
	if err != nil {
		return fmt.Errorf("marshal authors: %w", err)
	}

	// nullString/nullInt convert zero-value Go types to nil (SQL NULL) so the
	// COALESCE logic in the upsert correctly preserves existing non-null values.
	ssID := nullString(p.SSPaperID)
	hfID := nullString(p.HFPaperID)
	hfRepo := nullString(p.HFGithubRepo)

	_, err = s.pool.Exec(ctx, upsertSQL,
		p.ArxivID,          // $1
		p.Title,            // $2
		p.Abstract,         // $3
		p.Categories,       // $4  pgx handles []string → text[]
		authorsJSON,        // $5  JSONB
		p.SubmittedAt,      // $6
		p.UpdatedAtAPI,     // $7
		ssID,               // $8  NULL when enrichment failed
		p.CitationCount,    // $9  *int, nil → NULL
		p.MaxHIndex,        // $10 *int, nil → NULL
		p.TotalPriorPapers, // $11 *int, nil → NULL
		p.HasCode,          // $12
		hfID,               // $13 NULL when not yet checked
		p.HFUpvotes,        // $14 *int, nil → NULL
		hfRepo,             // $15 NULL when no repo linked
		p.GitHubStarsT60,   // $16 *int, nil → NULL (written by label job)
		p.HypeLabel,        // $17 *bool, nil → NULL (written by label job)
		p.EnrichedAt,       // $18 *time.Time, nil → NULL
	)
	if err != nil {
		return fmt.Errorf("upsert paper %s: %w", p.ArxivID, err)
	}
	return nil
}

// LoadHFPaperIDs populates p.HFPaperID from the DB for any papers that already
// exist. This lets the HF client skip papers previously looked up and not found.
func (s *Store) LoadHFPaperIDs(ctx context.Context, papers []*models.Paper) error {
	arxivIDs := make([]string, len(papers))
	index := make(map[string]*models.Paper, len(papers))
	for i, p := range papers {
		arxivIDs[i] = p.ArxivID
		index[p.ArxivID] = p
	}

	rows, err := s.pool.Query(ctx,
		`SELECT arxiv_id, hf_paper_id FROM papers WHERE arxiv_id = ANY($1) AND hf_paper_id IS NOT NULL`,
		arxivIDs,
	)
	if err != nil {
		return fmt.Errorf("load hf paper ids: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var arxivID, hfPaperID string
		if err := rows.Scan(&arxivID, &hfPaperID); err != nil {
			return fmt.Errorf("scan hf paper id: %w", err)
		}
		if p, ok := index[arxivID]; ok {
			p.HFPaperID = hfPaperID
		}
	}
	return rows.Err()
}

// LoadUnhydratedPapers returns papers where hf_paper_id IS NULL and
// submitted_at > since, ordered newest first. Used by the slow-track goroutine.
func (s *Store) LoadUnhydratedPapers(ctx context.Context, since time.Time) ([]*models.Paper, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT arxiv_id, submitted_at FROM papers
		 WHERE hf_paper_id IS NULL AND submitted_at > $1
		 ORDER BY submitted_at DESC`,
		since,
	)
	if err != nil {
		return nil, fmt.Errorf("load unhydrated papers: %w", err)
	}
	defer rows.Close()

	var papers []*models.Paper
	for rows.Next() {
		p := &models.Paper{}
		if err := rows.Scan(&p.ArxivID, &p.SubmittedAt); err != nil {
			return nil, fmt.Errorf("scan unhydrated paper: %w", err)
		}
		papers = append(papers, p)
	}
	return papers, rows.Err()
}

// UpdateHFFields writes the HuggingFace enrichment columns for a paper that
// already exists in the DB. Used by the slow-track goroutine to avoid a full
// upsert on rows that are already committed. hype_label is included so the
// preliminary upvote-derived signal is persisted alongside the HF data.
func (s *Store) UpdateHFFields(ctx context.Context, p *models.Paper) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE papers
		 SET hf_paper_id = $1, hf_upvotes = $2, hf_github_repo = $3, has_code = $4, hype_label = $5
		 WHERE arxiv_id = $6`,
		nullString(p.HFPaperID),
		p.HFUpvotes,
		nullString(p.HFGithubRepo),
		p.HasCode,
		p.HypeLabel,
		p.ArxivID,
	)
	if err != nil {
		return fmt.Errorf("update hf fields %s: %w", p.ArxivID, err)
	}
	return nil
}

// LoadUnlabeledPapers returns papers that have a linked GitHub repo but no T+60
// label yet, submitted before `before`. Used by the label job to find papers
// that are at least 60 days old and ready to be labeled.
func (s *Store) LoadUnlabeledPapers(ctx context.Context, before time.Time) ([]*models.Paper, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT arxiv_id, hf_github_repo FROM papers
		 WHERE has_code = true AND github_stars_t60 IS NULL AND submitted_at < $1
		 ORDER BY submitted_at DESC`,
		before,
	)
	if err != nil {
		return nil, fmt.Errorf("load unlabeled papers: %w", err)
	}
	defer rows.Close()

	var papers []*models.Paper
	for rows.Next() {
		p := &models.Paper{}
		if err := rows.Scan(&p.ArxivID, &p.HFGithubRepo); err != nil {
			return nil, fmt.Errorf("scan unlabeled paper: %w", err)
		}
		papers = append(papers, p)
	}
	return papers, rows.Err()
}

// UpdateLabel writes the T+60 target label columns for a paper. This is the
// ground-truth signal used for ML training and must not be called by the daily
// ingestion pipeline — only by the label job.
func (s *Store) UpdateLabel(ctx context.Context, p *models.Paper) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE papers SET github_stars_t60 = $1, hype_label = $2 WHERE arxiv_id = $3`,
		p.GitHubStarsT60,
		p.HypeLabel,
		p.ArxivID,
	)
	if err != nil {
		return fmt.Errorf("update label %s: %w", p.ArxivID, err)
	}
	return nil
}

// nullString returns nil for empty strings so COALESCE treats them as SQL NULL.
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
