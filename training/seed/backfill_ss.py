"""
backfill_ss.py — fetch Semantic Scholar author signals for labeled seed papers
and write max_h_index, total_prior_papers, has_author_enrichment back to parquet.

Only processes rows where github_stars_t60 is not null (the 40,639 labeled rows).
Re-running the script skips rows that already have max_h_index set.

API shape (mirrors ingestion/internal/semantic/client.go):
    POST /paper/batch?fields=authors,citationCount  — chunk size 500
    POST /author/batch?fields=hIndex,paperCount     — chunk size 1000

Rate limit: 100 req/min unauthenticated. No API key required.
At ~160 total requests, this completes in ~2 minutes.

Usage
-----
    python backfill_ss.py                        # full run
    python backfill_ss.py --limit 5              # smoke test (5 paper-batch chunks)
    python backfill_ss.py --parquet /path/to/file.parquet

After running
-------------
    Upload the updated parquet:
        aws s3 cp papers_seed.parquet s3://six-eyes/papers_seed.parquet
    Enable V2 features in features.py, then trigger a new Prefect training run.
"""

from __future__ import annotations

import argparse
import os
import time
from pathlib import Path

import pandas as pd
import requests
from dotenv import load_dotenv

load_dotenv(Path(__file__).parent.parent / ".env")

# ── Config ────────────────────────────────────────────────────────────────────

PARQUET_DEFAULT  = "papers_seed.parquet"
SS_BASE          = "https://api.semanticscholar.org/graph/v1"
PAPER_BATCH_SIZE = 500
AUTHOR_BATCH_SIZE = 1000
RATE_PER_MIN     = 90          # conservative; documented limit is 100/min
MIN_SLEEP        = 60.0 / RATE_PER_MIN   # ~0.67s between requests
CHECKPOINT_EVERY = 10          # write parquet after every N paper-batch chunks


# ── SS API helpers ────────────────────────────────────────────────────────────

def _post_batch(session: requests.Session, endpoint: str, ids: list[str],
                fields: str) -> list[dict | None]:
    """
    POST a batch request to SS. Returns a list parallel to ids, with None
    for papers/authors not found. Retries up to 4 times on 429/5xx.
    """
    url = f"{SS_BASE}/{endpoint}"
    for attempt in range(5):
        time.sleep(MIN_SLEEP)
        try:
            resp = session.post(
                url,
                json={"ids": ids},
                params={"fields": fields},
                timeout=30,
            )
        except requests.RequestException as exc:
            if attempt == 4:
                print(f"  [ss] network error after 5 attempts: {exc}")
                return [None] * len(ids)
            time.sleep(30)
            continue

        if resp.status_code == 200:
            return resp.json()

        if resp.status_code == 429:
            retry_after = int(resp.headers.get("Retry-After", 60))
            print(f"  [ss] 429 rate limited — sleeping {retry_after + 5}s")
            time.sleep(retry_after + 5)
            continue

        if resp.status_code >= 500:
            wait = 30 * (attempt + 1)
            print(f"  [ss] {resp.status_code} server error — retrying in {wait}s")
            time.sleep(wait)
            continue

        print(f"  [ss] unexpected {resp.status_code} for {endpoint} — skipping chunk")
        return [None] * len(ids)

    print(f"  [ss] max retries exceeded for {endpoint}")
    return [None] * len(ids)


# ── Main ──────────────────────────────────────────────────────────────────────

def main() -> None:
    parser = argparse.ArgumentParser(description="Backfill SS author signals into seed parquet")
    parser.add_argument("--parquet", default=os.getenv("PARQUET", PARQUET_DEFAULT))
    parser.add_argument("--limit",   type=int, default=None,
                        help="Process at most N paper-batch chunks (smoke test)")
    args = parser.parse_args()

    # ── Load parquet ──────────────────────────────────────────────────────────
    print(f"Loading {args.parquet} ...")
    df = pd.read_parquet(args.parquet)
    print(f"  {len(df):,} rows × {len(df.columns)} columns")

    # Add columns if first run
    for col in ("max_h_index", "total_prior_papers", "has_author_enrichment"):
        if col not in df.columns:
            df[col] = pd.NA

    # Only process labeled rows not yet enriched
    labeled = df["github_stars_t60"].notna()
    pending_mask = labeled & df["max_h_index"].isna()
    pending_idx = df[pending_mask].index.tolist()

    print(f"  Labeled rows          : {int(labeled.sum()):,}")
    print(f"  Already SS-enriched   : {int((labeled & df['max_h_index'].notna()).sum()):,}")
    print(f"  Pending               : {len(pending_idx):,}")

    if not pending_idx:
        print("\nNothing to do — all labeled rows already SS-enriched.")
        return

    # Chunk pending rows for paper batches
    chunks = [
        pending_idx[i: i + PAPER_BATCH_SIZE]
        for i in range(0, len(pending_idx), PAPER_BATCH_SIZE)
    ]
    if args.limit:
        chunks = chunks[: args.limit]
        print(f"  --limit {args.limit}: processing {len(chunks)} paper-batch chunks "
              f"({len(chunks) * PAPER_BATCH_SIZE} rows max)")

    n_chunks = len(chunks)
    eta_min = (n_chunks + n_chunks) * MIN_SLEEP / 60  # rough: paper + author calls
    print(f"\n  ~{n_chunks} paper-batch calls + author-batch calls")
    print(f"  Estimated time: <{eta_min:.0f} min at {RATE_PER_MIN} req/min")
    print("Starting ...\n")

    session = requests.Session()
    session.headers["User-Agent"] = "six-eyes-backfill/1.0"

    checkpoint_path = args.parquet.replace(".parquet", ".ss_checkpoint.parquet")
    papers_enriched = authors_fetched = 0

    # ── Phase 1: paper batch → collect author IDs ─────────────────────────────
    # author_ids_for_idx[df_index] = [ss_author_id, ...]
    author_ids_for_idx: dict[int, list[str]] = {}
    # citation counts while we're at it
    citation_counts: dict[int, int] = {}

    for chunk_num, idx_chunk in enumerate(chunks):
        ss_ids = ["arXiv:" + str(df.at[i, "arxiv_id"]) for i in idx_chunk]

        results = _post_batch(session, "paper/batch", ss_ids, "authors,citationCount")

        for df_idx, result in zip(idx_chunk, results):
            if result is None:
                continue
            if result.get("citationCount") is not None:
                citation_counts[df_idx] = result["citationCount"]
            a_ids = [a["authorId"] for a in result.get("authors", []) if a.get("authorId")]
            if a_ids:
                author_ids_for_idx[df_idx] = a_ids
                papers_enriched += 1

        if (chunk_num + 1) % 10 == 0 or (chunk_num + 1) == n_chunks:
            print(
                f"  paper-batch [{chunk_num + 1}/{n_chunks}]  "
                f"papers_with_authors={papers_enriched}",
                flush=True,
            )

        if (chunk_num + 1) % CHECKPOINT_EVERY == 0:
            df.to_parquet(checkpoint_path, index=False)
            print(f"  checkpoint → {checkpoint_path}", flush=True)

    # ── Phase 2: deduplicate author IDs ───────────────────────────────────────
    all_author_ids: list[str] = []
    seen: set[str] = set()
    for a_ids in author_ids_for_idx.values():
        for a_id in a_ids:
            if a_id not in seen:
                seen.add(a_id)
                all_author_ids.append(a_id)

    print(f"\n  Unique SS author IDs : {len(all_author_ids):,}")

    # ── Phase 3: author batch → h-index, paper count ─────────────────────────
    h_index_map:    dict[str, int] = {}
    paper_count_map: dict[str, int] = {}

    author_chunks = [
        all_author_ids[i: i + AUTHOR_BATCH_SIZE]
        for i in range(0, len(all_author_ids), AUTHOR_BATCH_SIZE)
    ]
    for chunk_num, chunk in enumerate(author_chunks):
        results = _post_batch(session, "author/batch", chunk, "hIndex,paperCount")
        for a_id, result in zip(chunk, results):
            if result is None:
                continue
            h_index_map[a_id]     = result.get("hIndex", 0) or 0
            paper_count_map[a_id] = result.get("paperCount", 0) or 0
            authors_fetched += 1

        if (chunk_num + 1) % 10 == 0 or (chunk_num + 1) == len(author_chunks):
            print(
                f"  author-batch [{chunk_num + 1}/{len(author_chunks)}]  "
                f"authors_fetched={authors_fetched}",
                flush=True,
            )

    # ── Phase 4: assign per-paper aggregates ─────────────────────────────────
    print("\n  Writing aggregates back to DataFrame ...")
    for df_idx, a_ids in author_ids_for_idx.items():
        max_h         = max((h_index_map.get(a, 0) for a in a_ids), default=0)
        total_papers  = sum(paper_count_map.get(a, 0) for a in a_ids)
        df.at[df_idx, "max_h_index"]        = max_h
        df.at[df_idx, "total_prior_papers"] = total_papers
        df.at[df_idx, "has_author_enrichment"] = True

    # Papers that were returned by SS but had no author IDs: mark enriched but 0
    for df_idx in citation_counts:
        if df_idx not in author_ids_for_idx:
            df.at[df_idx, "max_h_index"]           = 0
            df.at[df_idx, "total_prior_papers"]    = 0
            df.at[df_idx, "has_author_enrichment"] = True

    # ── Summary ───────────────────────────────────────────────────────────────
    enriched = df["has_author_enrichment"] == True   # noqa: E712
    total_labeled = int(labeled.sum())
    print(f"\n── SS enrichment summary ───────────────────────────────────")
    print(f"  Rows enriched         : {int(enriched.sum()):,}")
    print(f"  Rows not in SS        : {total_labeled - int(enriched.sum()):,}")
    if int(enriched.sum()) > 0:
        print(f"  max_h_index  p50/p90  : "
              f"{df.loc[enriched, 'max_h_index'].median():.0f} / "
              f"{df.loc[enriched, 'max_h_index'].quantile(0.9):.0f}")
        print(f"  total_prior p50/p90   : "
              f"{df.loc[enriched, 'total_prior_papers'].median():.0f} / "
              f"{df.loc[enriched, 'total_prior_papers'].quantile(0.9):.0f}")

    # ── Save ──────────────────────────────────────────────────────────────────
    df.to_parquet(args.parquet, index=False)
    print(f"\nSaved → {args.parquet}")
    print("\nNext steps:")
    print("  1. Enable V2 features in training/features.py")
    print("  2. Upload to S3:")
    print(f"       aws s3 cp {args.parquet} s3://six-eyes/papers_seed.parquet")
    print("  3. Trigger Prefect training run:")
    print("       prefect deployment run 'six-eyes-train/six-eyes-train'")


if __name__ == "__main__":
    main()
