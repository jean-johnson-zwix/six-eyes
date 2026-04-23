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

### Ingestion metrics for 263 papers

| | First run (single-phase) | Second run (two-phase) |
|---|---|---|
| Arxiv fetch | ~4s | — |
| Semantic Scholar (batch) | ~4s | — |
| Phase 1 total | — | ~29s |
| HF enrichment (sequential) | 28m 55s | 18m 51s |
| Supabase upsert | ~2s | — |
| **Total** | **~29m** | **~19m (~35% faster)** |

