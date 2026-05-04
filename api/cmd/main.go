// six-eyes API — Go GraphQL server for the Arxiv hype predictor.
//
// Loads the champion XGBoost model from api/model/ at startup, connects to
// Supabase PostgreSQL, and serves a GraphQL endpoint at /graphql.
//
// Env vars (all required in production):
//
//	SUPABASE_DB_URL   Postgres connection string (postgresql://...)
//	MODEL_DIR         Path to xgb_model.json + model_meta.json (default: ./model)
//	PORT              HTTP port (default: 8080)
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"github.com/joho/godotenv"

	"github.com/jeanjohnson/six-eyes/api/graph"
	"github.com/jeanjohnson/six-eyes/api/internal/db"
	"github.com/jeanjohnson/six-eyes/api/internal/inference"
)

func main() {
	// Load .env for local development (ignored if vars already set)
	_ = godotenv.Load()

	ctx := context.Background()

	// --- Model ---
	modelDir := getenv("MODEL_DIR", "./model")
	log.Printf("Loading model from %s ...", modelDir)
	model, err := inference.Load(modelDir)
	if err != nil {
		log.Fatalf("Failed to load model: %v", err)
	}
	log.Printf("Model loaded: %s v%s  threshold=%.4f  features=%d",
		model.Meta.ModelName, model.Meta.Version,
		model.Meta.Threshold, model.Meta.NumFeatures,
	)

	// --- Database ---
	connStr := getenv("SUPABASE_DB_URL", "")
	if connStr == "" {
		log.Fatal("SUPABASE_DB_URL is required")
	}
	store, err := db.New(ctx, connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer store.Close()
	log.Println("Database connected")

	// --- GraphQL ---
	schema := graphql.MustParseSchema(
		graph.Schema,
		&graph.Resolver{DB: store, Model: model},
		graphql.UseFieldResolvers(),
	)

	mux := http.NewServeMux()
	mux.Handle("/graphql", corsMiddleware(&relay.Handler{Schema: schema}))
	// Health check for Render
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	port := getenv("PORT", "8080")
	log.Printf("Listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

// corsMiddleware adds permissive CORS headers so the Next.js dashboard can
// call this API from Vercel (different origin).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
