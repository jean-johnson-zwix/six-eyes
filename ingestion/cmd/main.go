package main

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/jeanjohnson/six-eyes/ingestion/internal/arxiv"
	"github.com/jeanjohnson/six-eyes/ingestion/internal/db"
	"github.com/jeanjohnson/six-eyes/ingestion/internal/hf"
	"github.com/jeanjohnson/six-eyes/ingestion/internal/models"
	"github.com/jeanjohnson/six-eyes/ingestion/internal/semantic"
	"github.com/joho/godotenv"
)

var categories = []string{"cs.LG", "cs.CV", "cs.AI", "cs.CL"}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, reading from environment")
	}

	dbURL := os.Getenv("SUPABASE_DB_URL")
	if dbURL == "" {
		log.Fatal("SUPABASE_DB_URL is required")
	}

	ctx := context.Background()

	pool, err := db.NewPool(ctx, dbURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	store := db.NewStore(pool)
	arxivClient := arxiv.NewClient()
	ssClient := semantic.NewClient()
	hfClient := hf.NewClient()

	now := time.Now().UTC()
	yesterday := now.Truncate(24 * time.Hour).Add(-24 * time.Hour)

	// =========================================================
	// PHASE 1 — Fast track (synchronous, ~10s total)
	// =========================================================

	// Step 1: Fetch from Arxiv (deduplicate across categories).
	log.Printf("Fetching papers submitted since %s", yesterday.Format(time.RFC3339))
	seen := make(map[string]*models.Paper)
	for _, cat := range categories {
		papers, err := arxivClient.FetchSince(ctx, cat, yesterday)
		if err != nil {
			log.Printf("WARN: arxiv fetch failed for %s: %v", cat, err)
			continue
		}
		for i := range papers {
			if _, dup := seen[papers[i].ArxivID]; !dup {
				seen[papers[i].ArxivID] = &papers[i]
			}
		}
		log.Printf("  %s: %d papers", cat, len(papers))
	}
	log.Printf("Total unique papers fetched: %d", len(seen))

	papers := make([]*models.Paper, 0, len(seen))
	for _, p := range seen {
		papers = append(papers, p)
	}

	// Step 2: Semantic Scholar batch enrich (1 paper call + 1–3 author calls).
	if err := ssClient.EnrichAll(ctx, papers); err != nil {
		log.Printf("WARN: SS batch enrich failed: %v", err)
	}

	// Step 3: HF daily papers snipe — one call, hydrates the ~50 trending papers.
	dailyPapers, err := hfClient.FetchDailyPapers(ctx, yesterday)
	if err != nil {
		log.Printf("WARN: HF daily papers fetch failed: %v", err)
	} else {
		dailyByID := make(map[string]hf.DailyPaper, len(dailyPapers))
		for _, dp := range dailyPapers {
			dailyByID[dp.ArxivID] = dp
		}
		hydrated := 0
		for _, p := range papers {
			dp, ok := dailyByID[p.ArxivID]
			if !ok {
				continue
			}
			p.HFPaperID = p.ArxivID
			upvotes := dp.Upvotes
			p.HFUpvotes = &upvotes
			p.HFGithubRepo = dp.GithubRepo
			p.HasCode = dp.GithubRepo != ""
			hype := upvotes > 5
			p.HypeLabel = &hype
			hydrated++
		}
		log.Printf("HF daily snipe: %d/%d papers hydrated from daily feed", hydrated, len(papers))
	}

	// Step 4: Upsert all papers. Daily-hydrated papers carry HF data; the rest
	// have NULL hf_paper_id and will be picked up by the slow track.
	var upserted, upsertFailed int
	for _, p := range papers {
		enrichedAt := time.Now().UTC()
		p.EnrichedAt = &enrichedAt

		if err := store.UpsertPaper(ctx, p); err != nil {
			log.Printf("ERROR: upsert failed for %s: %v", p.ArxivID, err)
			upsertFailed++
			continue
		}
		upserted++
	}
	log.Printf("Phase 1 done — fetched=%d upserted=%d upsertFailed=%d", len(seen), upserted, upsertFailed)

	// =========================================================
	// PHASE 2 — Slow track (background goroutine)
	// Backfills HF data for papers not in the daily feed.
	// Queries the DB directly so it only processes the rows
	// that Phase 1 left with hf_paper_id IS NULL.
	// =========================================================
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		since := now.Add(-hf.CheckedNoneMaxAge)
		unhydrated, err := store.LoadUnhydratedPapers(ctx, since)
		if err != nil {
			log.Printf("WARN: slow track: failed to load unhydrated papers: %v", err)
			return
		}
		log.Printf("Slow track: %d papers to hydrate via per-paper HF lookups", len(unhydrated))

		done, failed := 0, 0
		for _, p := range unhydrated {
			if err := hfClient.Enrich(ctx, p); err != nil {
				log.Printf("WARN: slow track: HF enrich failed for %s: %v", p.ArxivID, err)
				failed++
				continue
			}
			if p.HFUpvotes != nil {
				hype := *p.HFUpvotes > 5
				p.HypeLabel = &hype
			}
			if err := store.UpdateHFFields(ctx, p); err != nil {
				log.Printf("ERROR: slow track: DB update failed for %s: %v", p.ArxivID, err)
				failed++
				continue
			}
			done++
		}
		log.Printf("Slow track done — hydrated=%d failed=%d", done, failed)
	}()

	wg.Wait()
}
