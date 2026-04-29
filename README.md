# six-eyes
An MLOps pipeline for an AI research digest — ingests papers, trains a model, and serves predictions.

---

## Tech Stack

| Layer | Tech |
|---|---|
| Ingestion service | Go (`pgx`, `go-resty`, `godotenv`) |
| GraphQL API | Go (`gqlgen`) |
| ML training & monitoring | Python (XGBoost, scikit-learn, MLflow, Evidently) |
| Orchestration | Prefect Cloud |
| Frontend | Next.js (TypeScript) |
| Database | Supabase PostgreSQL |
| Artifact storage | Supabase Storage (MLflow artifacts) |
| Serving | Render (Go services), Vercel (frontend) |
| Monitoring | Grafana Cloud |
| CI/CD | GitHub Actions + Docker + pre-commit |

---

## Data Ingestion

A Go binary (`ingestion/cmd/main.go`) runs daily and populates Supabase PostgreSQL with enriched Arxiv paper records.


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
| HuggingFace (daily) | Single bulk `GET /daily_papers?date=...` — hydrates ~50 featured papers | No key required |
| HuggingFace (per-paper) | Sequential `GET /papers/{id}` with 3–5s random jitter | No key required |

### Two-phase design

**Phase 1 — Fast track (~10s, synchronous):** Arxiv fetch → SS batch enrichment → HF daily snipe (one bulk call covering ~50 trending papers) → upsert all papers. Trending papers get full HF metadata immediately; the rest are upserted with `hf_paper_id IS NULL`.

**Phase 2 — Slow track (background goroutine):** Queries the DB for papers where `hf_paper_id IS NULL`, then looks each up individually via the HF per-paper API. Uses the DB as a synchronisation point — Phase 2 only touches rows that Phase 1 left unhydrated.

**`checked_none` sentinel:** When a paper is not found on HuggingFace, `hf_paper_id` is set to `checked_none` rather than NULL. Papers with this sentinel and `submitted_at` older than 7 days are excluded from Phase 2 entirely — HF rarely indexes a paper more than a week after submission.

### Preliminary hype label

During HF enrichment, a preliminary `hype_label` (`*bool`) is computed as `hf_upvotes > 5` and written to the DB. This proxy signal is overwritten by the T+60 label job once ground-truth GitHub star counts are available.

### Schema

See [`docs/entity-relationship.md`](docs/entity-relationship.md) for the full ER diagram.

---

## Historical Seed (`training/seed/`)

A one-time DuckDB pipeline that builds the training dataset from two static dumps.

### Sources

| Dataset | Source | Size |
|---|---|---|
| ArXiv metadata | Kaggle (`Cornell-University/arxiv`) | ~3.5 GB NDJSON, 1.7M papers |
| Papers with Code links | HuggingFace (`pwc-archive/links-between-paper-and-code`) | 300K rows |

### Pipeline

```
download_raw.py   →   raw_data/arxiv-metadata-oai-snapshot.json
                  →   raw_data/links-between-paper-and-code.parquet
                            │
transform.py      ──────────┘
  DuckDB:
    filter ArXiv → cs.LG / cs.AI / cs.CV / cs.CL, 2024+
    join PwC links → has_code, hf_github_repo
    label proxy → hype_label = is_official PwC match
                            │
                            ▼
                  papers_seed.parquet  (229,948 rows — DVC tracked)
                            │
                            ▼
                  train.py reads directly (Supabase load skipped;
                  229K rows exceeds free-tier 10K budget)
```

### Label strategy

| Condition | `has_code` | `hype_label` |
|---|---|---|
| Paper has official repo in PwC | `true` | `true` (proxy) |
| Paper absent from PwC | `false` | `false` |

Bootstrap proxy only — `cmd/label` overwrites `hype_label` with `github_stars_t60 > 100` ground truth once T+60 days have elapsed.

### Run

```bash
cd training/seed
pip install -r requirements.txt
huggingface-cli login            # required for PwC dataset
python download_raw.py           # downloads both sources
python transform.py              # ~60s → papers_seed.parquet
```

Raw files are DVC-tracked (Google Drive remote). To restore without re-downloading:
```bash
dvc pull
```

---

## Model Training (`training/`)

### Feature engineering (`features.py`)

V1 feature set — 24 features, no nulls, fully vectorised:

| Group | Features |
|---|---|
| Paper metadata | `num_authors`, `abstract_length`, `title_length`, `day_of_week`, `month`, `num_categories` |
| Category multi-hot | `cat_cs_LG`, `cat_cs_AI`, `cat_cs_CV`, `cat_cs_CL` |
| Title buzzwords | `buzz_transformer`, `buzz_diffusion`, `buzz_agent`, … (14 flags) |

Features excluded from V1 (deferred):
- `has_code` — equals `hype_label` in seed data (both derived from PwC presence — leakage)
- `max_h_index`, `total_prior_papers` — NULL for all seed rows; added in V2 with `has_author_enrichment` flag
- `hf_upvotes` — excluded while label is PwC proxy; re-enabled in V3 with ground-truth label

### Results (V1 baseline — PwC proxy label)

Stratified 80/10/10 split · 183,958 train · 22,995 val · 22,995 test

| Model | Val PR-AUC | Val ROC-AUC | Val F1 | Test PR-AUC |
|---|---|---|---|---|
| Logistic Regression | 0.2315 | 0.5983 | 0.3151 | 0.2344 |
| XGBoost (default) | 0.2739 | 0.6468 | 0.3511 | 0.2770 |
| **XGBoost (Optuna-tuned, 50 trials)** | **0.2778** | — | — | **0.2780** |

Random baseline PR-AUC = 0.177 (positive class rate). Tuned XGBoost is +57% over random on title/metadata features alone.

Top-5 XGBoost features: `cat_cs_CV` (0.127), `cat_cs_CL` (0.110), `month` (0.089), `cat_cs_LG` (0.083), `buzz_mamba` (0.054)

> **Label caveat:** `hype_label` is currently a PwC code-link presence proxy (17.7% positive rate vs ~3% in the wild). The model will over-predict hype on live inference until `cmd/label` overwrites labels with `github_stars_t60 > 100` ground truth. Track Precision-Recall, not accuracy.

### Run

```bash
cd training
pip install mlflow xgboost scikit-learn sqlalchemy optuna
python train.py                   # trains both LR and XGBoost
python train.py --model xgb       # XGBoost only
python tune.py --n-trials 50      # Optuna tuning; logs 50 nested MLflow runs
python -m mlflow ui --backend-store-uri sqlite:///mlflow.db
```

Both models are registered in the MLflow model registry (`six-eyes-lr`, `six-eyes-xgb`).

---

### Ingestion metrics for 263 papers

| | First run (single-phase) | Second run (two-phase) |
|---|---|---|
| Arxiv fetch | ~4s | — |
| Semantic Scholar (batch) | ~4s | — |
| Phase 1 total | — | ~29s |
| HF enrichment (sequential) | 28m 55s | 18m 51s |
| Supabase upsert | ~2s | — |
| **Total** | **~29m** | **~19m (~35% faster)** |

