// cmd/label writes T+60 GitHub star counts and ground-truth hype labels for
// papers that are at least 60 days old and have a linked GitHub repo.
// Run this on a schedule (e.g. weekly) to keep training labels up to date.
// It is intentionally separate from the daily pipeline (cmd/main.go).
//
// Usage:
//
//	go run ./cmd/label
//
// Environment variables:
//
//	SUPABASE_DB_URL   — required
//	GITHUB_TOKEN      — strongly recommended; without it the unauthenticated
//	                    rate limit (60 req/hr) makes large runs very slow
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/jeanjohnson/six-eyes/ingestion/internal/db"
	gh "github.com/jeanjohnson/six-eyes/ingestion/internal/github"
	"github.com/joho/godotenv"
)

const labelLagDays = 60

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, reading from environment")
	}

	dbURL := os.Getenv("SUPABASE_DB_URL")
	if dbURL == "" {
		log.Fatal("SUPABASE_DB_URL is required")
	}
	ghToken := os.Getenv("GITHUB_TOKEN")
	if ghToken == "" {
		log.Println("WARN: GITHUB_TOKEN not set — unauthenticated rate limit (60 req/hr) applies; large runs will be slow")
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, dbURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	store := db.NewStore(pool)
	ghClient := gh.NewClient(ghToken)

	before := time.Now().UTC().Add(-labelLagDays * 24 * time.Hour)
	papers, err := store.LoadUnlabeledPapers(ctx, before)
	if err != nil {
		log.Fatalf("load unlabeled papers: %v", err)
	}
	log.Printf("Found %d papers to label (submitted before %s)", len(papers), before.Format("2006-01-02"))

	done, failed := 0, 0
	for _, p := range papers {
		stars, err := ghClient.FetchStars(ctx, p.HFGithubRepo)
		if err != nil {
			log.Printf("WARN: github fetch failed for %s (%s): %v", p.ArxivID, p.HFGithubRepo, err)
			failed++
			continue
		}
		p.GitHubStarsT60 = &stars
		hype := stars > 100
		p.HypeLabel = &hype
		if err := store.UpdateLabel(ctx, p); err != nil {
			log.Printf("ERROR: update label failed for %s: %v", p.ArxivID, err)
			failed++
			continue
		}
		done++
	}
	log.Printf("Done — labeled=%d failed=%d", done, failed)
}
