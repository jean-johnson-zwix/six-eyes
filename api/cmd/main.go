// six-eyes API — Go GraphQL server for the Arxiv hype predictor.
//
// Loads the champion XGBoost model at startup (fetched from S3 if not already
// present locally), connects to Supabase PostgreSQL, and serves a GraphQL
// endpoint at /graphql.
//
// Env vars:
//
//	SUPABASE_DB_URL   Postgres connection string (required)
//	MODEL_BASE_URL    Public base URL for model artifacts, e.g.
//	                  https://huggingface.co/<user>/<repo>/resolve/main/
//	                  If unset, loads from MODEL_DIR directly (local/dev).
//	MODEL_DIR         Local cache dir for model files (default: /tmp/model)
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
	"github.com/jeanjohnson/six-eyes/api/internal/modelstore"
)

func main() {
	// Load .env for local development (ignored if vars already set)
	_ = godotenv.Load()

	ctx := context.Background()

	// --- Model ---
	modelDir := getenv("MODEL_DIR", "/tmp/model")
	if baseURL := os.Getenv("MODEL_BASE_URL"); baseURL != "" {
		if err := modelstore.EnsureLocal(modelDir, baseURL); err != nil {
			log.Fatalf("Failed to fetch model artifacts: %v", err)
		}
	}
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

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Println("WARNING: API_KEY not set — GraphQL endpoint is unauthenticated")
	}

	mux := http.NewServeMux()
	mux.Handle("/graphql", corsMiddleware(authMiddleware(apiKey, &relay.Handler{Schema: schema})))
	// Health check for Render — intentionally unauthenticated
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	port := getenv("PORT", "8080")
	log.Printf("Listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

// authMiddleware rejects requests whose Authorization header doesn't match
// "Bearer <apiKey>". If apiKey is empty the middleware is a no-op.
func authMiddleware(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey != "" {
			got := r.Header.Get("Authorization")
			if got != "Bearer "+apiKey {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
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
