package graph

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/jeanjohnson/six-eyes/api/internal/db"
	"github.com/jeanjohnson/six-eyes/api/internal/inference"
)

// Resolver is the root GraphQL resolver. graph-gophers/graphql-go calls its
// methods by matching CamelCase field names from the schema.
type Resolver struct {
	DB    *db.Store
	Model *inference.Model
}

// --- Query resolvers ---

// Papers resolves Query.papers.
func (r *Resolver) Papers(ctx context.Context, args struct {
	Days  *int32
	Limit *int32
	Tier  *string
}) ([]*PaperResolver, error) {
	days := 30
	if args.Days != nil && *args.Days > 0 {
		days = int(*args.Days)
	}
	limit := 50
	if args.Limit != nil && *args.Limit > 0 {
		limit = int(*args.Limit)
		if limit > 200 {
			limit = 200
		}
	}

	papers, err := r.DB.ListRecent(ctx, days, limit)
	if err != nil {
		return nil, err
	}

	var resolvers []*PaperResolver
	for i := range papers {
		pr := r.scorePaper(&papers[i])
		if args.Tier != nil && *args.Tier != "" && pr.hypeTier != *args.Tier {
			continue
		}
		resolvers = append(resolvers, pr)
	}

	// Sort by hype score descending
	sort.Slice(resolvers, func(i, j int) bool {
		return resolvers[i].hypeScore > resolvers[j].hypeScore
	})
	return resolvers, nil
}

// Paper resolves Query.paper.
func (r *Resolver) Paper(ctx context.Context, args struct{ ArxivId string }) (*PaperResolver, error) {
	p, err := r.DB.GetByArxivID(ctx, args.ArxivId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r.scorePaper(p), nil
}

func (r *Resolver) scorePaper(p *db.Paper) *PaperResolver {
	input := inference.PaperInput{
		Authors:          p.Authors,
		Abstract:         p.Abstract,
		Title:            p.Title,
		Categories:       p.Categories,
		SubmittedAt:      p.SubmittedAt,
		MaxHIndex:        p.MaxHIndex,
		TotalPriorPapers: p.TotalPriorPapers,
	}
	score := r.Model.Predict(input)
	return &PaperResolver{
		paper:     p,
		hypeScore: score,
		hypeTier:  r.Model.Tier(score),
	}
}

// --- PaperResolver ---

// PaperResolver holds a paper and its computed prediction.
// Methods are called by graph-gophers/graphql-go to resolve each field.
type PaperResolver struct {
	paper     *db.Paper
	hypeScore float64
	hypeTier  string
}

func (pr *PaperResolver) ArxivId() string    { return pr.paper.ArxivID }
func (pr *PaperResolver) Title() string      { return pr.paper.Title }
func (pr *PaperResolver) Abstract() string   { return pr.paper.Abstract }
func (pr *PaperResolver) Categories() []string { return pr.paper.Categories }
func (pr *PaperResolver) Authors() []string  { return pr.paper.Authors }
func (pr *PaperResolver) SubmittedAt() string {
	return pr.paper.SubmittedAt.Format("2006-01-02T15:04:05Z")
}
func (pr *PaperResolver) HasCode() bool      { return pr.paper.HasCode }
func (pr *PaperResolver) HypeScore() float64 { return pr.hypeScore }
func (pr *PaperResolver) HypeTier() string   { return pr.hypeTier }
func (pr *PaperResolver) MaxHIndex() *int32 {
	if pr.paper.MaxHIndex == nil {
		return nil
	}
	v := int32(*pr.paper.MaxHIndex)
	return &v
}
func (pr *PaperResolver) TotalPriorPapers() *int32 {
	if pr.paper.TotalPriorPapers == nil {
		return nil
	}
	v := int32(*pr.paper.TotalPriorPapers)
	return &v
}
func (pr *PaperResolver) HfUpvotes() *int32 {
	if pr.paper.HFUpvotes == nil {
		return nil
	}
	v := int32(*pr.paper.HFUpvotes)
	return &v
}
