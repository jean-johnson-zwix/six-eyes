// Package db queries the Supabase PostgreSQL papers table via pgx.
package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Paper is the row shape returned from the papers table.
type Paper struct {
	ArxivID          string
	Title            string
	Abstract         string
	Categories       []string
	Authors          []string // just names, unpacked from JSONB [{name:"..."}]
	SubmittedAt      time.Time
	HasCode          bool
	MaxHIndex        *int
	TotalPriorPapers *int
	HFUpvotes        *int
}

// Store wraps a pgx connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// New creates a Store from a Postgres connection URL.
func New(ctx context.Context, connStr string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}
	// Use simple protocol to avoid prepared-statement cache conflicts
	// across pool connections (common with PgBouncer / Supabase pooler).
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the connection pool.
func (s *Store) Close() { s.pool.Close() }

// ListRecent returns the most recent papers from the last `days` days.
// Results are ordered by submitted_at descending.
func (s *Store) ListRecent(ctx context.Context, days, limit int) ([]Paper, error) {
	const q = `
		SELECT
			arxiv_id, title, abstract, categories, authors,
			submitted_at, has_code, max_h_index, total_prior_papers, hf_upvotes
		FROM papers
		WHERE submitted_at >= NOW() - ($1 * INTERVAL '1 day')
		ORDER BY submitted_at DESC
		LIMIT $2
	`
	rows, err := s.pool.Query(ctx, q, days, limit)
	if err != nil {
		return nil, fmt.Errorf("query papers: %w", err)
	}
	defer rows.Close()

	var papers []Paper
	for rows.Next() {
		var p Paper
		var authorsJSON []byte
		err := rows.Scan(
			&p.ArxivID, &p.Title, &p.Abstract, &p.Categories, &authorsJSON,
			&p.SubmittedAt, &p.HasCode, &p.MaxHIndex, &p.TotalPriorPapers, &p.HFUpvotes,
		)
		if err != nil {
			return nil, fmt.Errorf("scan paper: %w", err)
		}
		p.Authors = unpackAuthors(authorsJSON)
		papers = append(papers, p)
	}
	return papers, rows.Err()
}

// GetByArxivID returns a single paper by its Arxiv ID, or nil if not found.
func (s *Store) GetByArxivID(ctx context.Context, arxivID string) (*Paper, error) {
	const q = `
		SELECT
			arxiv_id, title, abstract, categories, authors,
			submitted_at, has_code, max_h_index, total_prior_papers, hf_upvotes
		FROM papers
		WHERE arxiv_id = $1
		LIMIT 1
	`
	var p Paper
	var authorsJSON []byte
	err := s.pool.QueryRow(ctx, q, arxivID).Scan(
		&p.ArxivID, &p.Title, &p.Abstract, &p.Categories, &authorsJSON,
		&p.SubmittedAt, &p.HasCode, &p.MaxHIndex, &p.TotalPriorPapers, &p.HFUpvotes,
	)
	if err != nil {
		// pgx returns pgx.ErrNoRows for not-found; caller can type-check if needed
		return nil, err
	}
	p.Authors = unpackAuthors(authorsJSON)
	return &p, nil
}

// authorJSON is the shape stored in the JSONB authors column.
type authorJSON struct {
	Name string `json:"name"`
}

func unpackAuthors(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var authors []authorJSON
	if err := json.Unmarshal(raw, &authors); err != nil {
		return nil
	}
	names := make([]string, len(authors))
	for i, a := range authors {
		names[i] = a.Name
	}
	return names
}
