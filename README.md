# six-eyes

An end-to-end MLOps pipeline that predicts which Arxiv ML papers will gain traction (GitHub stars > 100 at T+60 days) and serves predictions through a personal research digest API.

Built as a capstone covering all 6 modules of the MLOps Zoomcamp.

---

## Architecture

```
Arxiv / Semantic Scholar / HuggingFace
              │
              ▼
    Go ingestion service  ──► Supabase PostgreSQL
    (daily GitHub Action)
              │
              ▼
    Python training pipeline  ──► MLflow on DagShub
    (monthly Prefect Cloud)         (experiment tracking)
              │
              ▼
    export_model.py  ──► HuggingFace Hub
                          (xgb_model.json + model_meta.json)
              │
              ▼
    Go GraphQL API  ──► Render
    (fetches model at startup,
     scores papers at query time)
```

---

## Tech Stack

| Layer | Tech |
|---|---|
| Ingestion service | Go (`pgx`, `go-resty`, `godotenv`) |
| GraphQL API | Go (`graph-gophers/graphql-go`, `pgx`) |
| ML training & monitoring | Python (XGBoost, scikit-learn, MLflow, Evidently, Optuna) |
| Orchestration | Prefect Cloud (monthly retrain) + GitHub Actions (daily ingest, weekly drift) |
| Frontend | Next.js (TypeScript) — in progress |
| Database | Supabase PostgreSQL |
| Model registry | MLflow on DagShub |
| Model serving artifacts | HuggingFace Hub |
| Serving | Render (Go API), Vercel (frontend — in progress) |
| Monitoring | Evidently drift reports → GitHub Pages |
| CI/CD | GitHub Actions + Docker |

---

## Data Ingestion

A Go binary (`ingestion/cmd/main.go`) runs daily via GitHub Actions and populates Supabase PostgreSQL with enriched Arxiv paper records.

### Pipeline

```
Arxiv API  ──────────────────────────────────────────────────► papers (deduplicated)
                                                                        │
Semantic Scholar API  ◄── POST /paper/batch (all IDs, 1 call) ─────────┤
                      ◄── POST /author/batch (all authors, 1–3 calls)   │
                                                                        │
HuggingFace API       ◄── GET /daily_papers?date=... (1 call,  ─────────┤  PHASE 1 (~10s)
                          hydrates ~50 trending papers)                  │
                                                                        ▼
                                                               Supabase upsert (all papers)
                                                                        │
                                                            ┌───────────┘
                                                            │ PHASE 2 (background goroutine)
HuggingFace API       ◄── GET /papers/{arxiv_id}  ──────────┤ per-paper lookup for papers
                          (3–5s jitter, per-paper)          │ not in daily feed
                                                            ▼
                                                   Supabase UPDATE (HF fields only)
```

### External APIs

| API | Strategy | Rate limit |
|---|---|---|
| Arxiv | Paginated fetch (100/page) | 3 req/sec, no key |
| Semantic Scholar | Batch POST — 1 paper call + 1–3 author calls total | 100 req/min, no key |
| HuggingFace (daily) | Single bulk `GET /daily_papers?date=...` | No key required |
| HuggingFace (per-paper) | Sequential `GET /papers/{id}` with 3–5s random jitter | No key required |

### Two-phase design

**Phase 1 — Fast track (~10s, synchronous):** Arxiv fetch → SS batch enrichment → HF daily snipe → upsert all papers.

**Phase 2 — Slow track (background goroutine):** Per-paper HF lookup for rows that Phase 1 left unhydrated. Uses the DB as a synchronisation point.

**`checked_none` sentinel:** When a paper is not found on HuggingFace, `hf_paper_id` is set to `checked_none`. Papers with this sentinel older than 7 days are excluded from Phase 2.

---

## Historical Seed (`training/seed/`)

A one-time DuckDB pipeline that builds the training dataset from two static dumps.

| Dataset | Source | Size |
|---|---|---|
| ArXiv metadata | Kaggle (`Cornell-University/arxiv`) | ~3.5 GB NDJSON, 1.7M papers |
| Papers with Code links | HuggingFace (`pwc-archive/links-between-paper-and-code`) | 300K rows |

**Output:** `papers_seed.parquet` — 229,948 rows (DVC-tracked, Google Drive remote).

```bash
cd training/seed
python download_raw.py   # downloads both sources
python transform.py      # ~60s → papers_seed.parquet
# or restore from DVC:
dvc pull
```

`backfill_labels.py` then fetched GitHub stars for all 40,813 rows with a repo link and overwrote `hype_label` with `github_stars_t60 > 100`. Final positive rate: 12.1% among repos (2.2% across all 229,948 rows).

---

## Model Training (`training/`)

### Feature engineering (`features.py`)

27 features across three groups:

| Group | Features |
|---|---|
| Paper metadata | `num_authors`, `abstract_length`, `title_length`, `day_of_week`, `month`, `num_categories` |
| Category multi-hot | `cat_cs_LG`, `cat_cs_AI`, `cat_cs_CV`, `cat_cs_CL` |
| Title buzzwords | `buzz_transformer`, `buzz_diffusion`, `buzz_agent`, … (14 flags) |
| Author signals (V2) | `max_h_index`, `total_prior_papers`, `has_author_enrichment` |

### Results

Stratified 80/10/10 split · 183,958 train · 22,995 val · 22,995 test

**V3 — Real labels + Semantic Scholar author signals (active model)**

| Model | Val PR-AUC | Test PR-AUC | Test ROC-AUC | Test F1 | Threshold |
|---|---|---|---|---|---|
| XGBoost (Optuna, 50 trials) | 0.3357 | 0.3363 | 0.9424 | 0.3862 | 0.8837 |

Random baseline PR-AUC = 0.022. V3 is **15x better than random**. Author h-index is the dominant feature — adding `max_h_index` and `total_prior_papers` drove a +330% PR-AUC improvement over V2 (real labels only).

<details>
<summary>Earlier model versions</summary>

**V1 — PwC proxy label (17.7% positive rate)**

| Model | Val PR-AUC | Val ROC-AUC | Val F1 |
|---|---|---|---|
| Logistic Regression | 0.2315 | 0.5983 | 0.3151 |
| XGBoost (default) | 0.2739 | 0.6468 | 0.3511 |
| XGBoost (Optuna-tuned) | 0.2778 | — | — |

**V2 — Real `github_stars_t60` labels (2.2% positive rate)**

| Model | Test PR-AUC | Test ROC-AUC | Test F1 | Threshold |
|---|---|---|---|---|
| XGBoost (Optuna-tuned) | 0.0876 | 0.7652 | 0.1477 | 0.7242 |

</details>

### Run

```bash
# Prefect flow (recommended) — from project root
python flows/train_flow.py                     # 50 Optuna trials
python flows/train_flow.py --n-trials 2        # quick smoke test

# Individual scripts
cd training
python train.py --model xgb
python tune.py --n-trials 50
```

Experiments are tracked in MLflow on DagShub. Models are registered as `six-eyes-xgb` (champion alias points to the production model).

### Model export

After training, export the champion model to Go-compatible artifacts and upload to HuggingFace Hub:

```bash
cd training
python export_model.py --upload-hf --hf-repo <user>/<repo>
# prints MODEL_BASE_URL to set on Render
```

---

## Orchestration

| Schedule | Trigger | What it does |
|---|---|---|
| Daily `0 7 * * *` | `.github/workflows/ingest.yml` | Builds Go binary, ingests new Arxiv papers into Supabase |
| Weekly `0 9 * * 1` | `.github/workflows/monitor.yml` | Runs Evidently drift report, publishes to GitHub Pages |
| Monthly `0 8 1 * *` | Prefect Cloud (`prefect.yaml`) | Retrains XGBoost on latest data, registers new model version |

---

## Drift Monitoring

Evidently `DataDriftPreset` + `DataQualityPreset` reports comparing the current week's ingested papers against the training distribution. Reports are published to GitHub Pages on every Monday run.

---

## GraphQL API (`api/`)

Go service deployed on Render. Loads the XGBoost model from HuggingFace Hub at startup and scores papers at query time — no Python runtime, no CGO.

### Queries

```graphql
# Ranked paper feed
papers(days: Int, limit: Int, tier: String): [Paper!]!

# Single paper lookup
paper(arxivId: String!): Paper
```

`tier` filter: `"hype"` (score ≥ threshold), `"likely"`, or `"low"`.

### Auth

All requests require `Authorization: Bearer <API_KEY>`. The `/health` endpoint is unauthenticated (used by Render's health check).

### Local dev

```bash
cd api
cp ../training/.env .env   # needs SUPABASE_DB_URL
go run ./cmd/main.go

curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <key>" \
  -d '{"query":"{ papers(limit:5) { arxivId title hypeScore hypeTier } }"}'
```

### Env vars

| Var | Description |
|---|---|
| `SUPABASE_DB_URL` | Postgres connection string |
| `MODEL_BASE_URL` | HuggingFace Hub base URL for model artifacts |
| `API_KEY` | Bearer token for GraphQL auth |
| `MODEL_DIR` | Local model cache dir (default: `/tmp/model`) |
| `PORT` | HTTP port (default: `8080`, set automatically by Render) |

---

## Project Status

| Module | Component | Status |
|---|---|---|
| 01 — ML Pipeline | `training/` | Done |
| 02 — Experiment Tracking | MLflow on DagShub | Done |
| 03 — Orchestration | GitHub Actions + Prefect Cloud | Done |
| 04 — Deployment | Go GraphQL API on Render | Done (dashboard in progress) |
| 05 — Monitoring | Evidently → GitHub Pages | Done (Grafana dashboards pending) |
| 06 — Best Practices | CI, pre-commit, tests, Makefile | Pending |
