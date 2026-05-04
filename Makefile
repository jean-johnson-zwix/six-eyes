# ── six-eyes Makefile ─────────────────────────────────────────────────────────
# Targets: test, lint, ingest, train, serve, migrate
#
# Prerequisites:
#   Go 1.22+     (ingestion/, api/)
#   Python 3.11+ (training/)
#   psql         (migrate target)
#   ruff         (lint-python: pip install ruff)
#   .env files   (see ingestion/.env.example, api/.env.example)

.PHONY: test test-go test-python lint lint-go lint-python \
        ingest train serve migrate

# ── Test ───────────────────────────────────────────────────────────────────────

test: test-go test-python

test-go:
	@echo "==> Go tests (ingestion)..."
	cd ingestion && go test ./... -v
	@echo "==> Go tests (api)..."
	cd api && go test ./... -v

test-python:
	@echo "==> Python tests (training)..."
	cd training && python -m pytest tests/ -v

# ── Lint ──────────────────────────────────────────────────────────────────────

lint: lint-go lint-python

lint-go:
	@echo "==> gofmt check (ingestion)..."
	@test -z "$$(gofmt -l ingestion/)" || (gofmt -d ingestion/ && exit 1)
	@echo "==> gofmt check (api)..."
	@test -z "$$(gofmt -l api/)" || (gofmt -d api/ && exit 1)

lint-python:
	@echo "==> ruff check (training)..."
	ruff check training/

# ── Ingest ────────────────────────────────────────────────────────────────────

ingest:
	@echo "==> Building ingestion binary..."
	cd ingestion && go build -o ../bin/ingest ./cmd/main.go
	@echo "==> Running ingestion..."
	./bin/ingest

# ── Train ─────────────────────────────────────────────────────────────────────

train:
	@echo "==> Running training pipeline..."
	cd training && python train.py

# ── Serve ─────────────────────────────────────────────────────────────────────

serve:
	@echo "==> Building API binary..."
	cd api && go build -o ../bin/api ./cmd/main.go
	@echo "==> Starting API server (ctrl-C to stop)..."
	./bin/api

# ── Migrate ───────────────────────────────────────────────────────────────────

migrate:
	@if [ -z "$$SUPABASE_DB_URL" ]; then \
		echo "ERROR: SUPABASE_DB_URL is not set"; exit 1; \
	fi
	@echo "==> Applying migrations..."
	@for f in migrations/*.sql; do \
		echo "  $$f ..."; \
		psql "$$SUPABASE_DB_URL" -f "$$f"; \
	done
	@echo "==> Done."
