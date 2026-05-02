"""
backfill_labels.py — fetch current GitHub star counts for seed papers and write
real hype labels back to papers_seed.parquet.

The proxy label (has_code from PwC) is replaced with github_stars_t60 > 100.

NOTE: Fetches CURRENT star counts, not historical T+60 counts. All seed papers
are from 2024 (>60 days old), so current counts are a reasonable bootstrap proxy.
Stars accumulate over time, so this slightly inflates the positive rate — acceptable
for an initial real-label training run; proper T+60 labeling happens going forward
via ingestion/cmd/label against live Supabase records.

Usage
-----
    python backfill_labels.py                        # full run
    python backfill_labels.py --limit 200            # smoke test
    python backfill_labels.py --parquet /path/to/file.parquet

Env vars
--------
    GITHUB_TOKEN    strongly recommended (5 000 req/hr vs 60/hr unauthenticated)
    PARQUET         override default parquet path

Progress
--------
A checkpoint parquet is written every CHECKPOINT_EVERY rows so a crash can be
resumed. Re-running the script skips rows that already have github_stars_t60 set.

After running
-------------
    Upload the updated parquet to replace the training artifact:
        aws s3 cp papers_seed.parquet s3://six-eyes/papers_seed.parquet
    Then trigger a new Prefect training run.
"""

from __future__ import annotations

import argparse
import os
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

import pandas as pd
import requests
from dotenv import load_dotenv

# Load .env from training/ (parent of seed/) regardless of CWD.
load_dotenv(Path(__file__).parent.parent / ".env")

# ── Config ────────────────────────────────────────────────────────────────────

PARQUET_DEFAULT  = "papers_seed.parquet"
HYPE_THRESHOLD   = 100        # stars > 100 at collection time = "hype"
CHECKPOINT_EVERY = 500        # write parquet after every N fetches
GITHUB_API_BASE  = "https://api.github.com"

# Authenticated: 80/min (safely under 5 000/hr primary + secondary rate limits).
# Unauthenticated: 1/min (safely under 60/hr).
AUTH_RATE_PER_MIN   = 80
UNAUTH_RATE_PER_MIN = 1


# ── GitHub helpers ────────────────────────────────────────────────────────────

def parse_github_url(raw_url: str) -> tuple[str, str] | tuple[None, None]:
    """Extract (owner, repo) from a https://github.com/owner/repo URL."""
    url = raw_url.strip().rstrip("/")
    if url.endswith(".git"):
        url = url[:-4]
    prefix = "https://github.com/"
    if not url.startswith(prefix):
        return None, None
    parts = url[len(prefix):].split("/", 2)
    if len(parts) < 2 or not parts[0] or not parts[1]:
        return None, None
    return parts[0], parts[1]


def _sleep_until_reset(resp: requests.Response, fallback_secs: int = 60) -> None:
    """Sleep until the X-RateLimit-Reset timestamp (+ 5s buffer), or fallback_secs."""
    reset_header = resp.headers.get("X-RateLimit-Reset")
    if reset_header:
        reset_ts = int(reset_header)
        wait = datetime.fromtimestamp(reset_ts, tz=timezone.utc) - datetime.now(timezone.utc)
        secs = max(wait.total_seconds() + 5, 5)
    else:
        retry_after = resp.headers.get("Retry-After")
        secs = int(retry_after) + 5 if retry_after else fallback_secs
    print(f"  [gh] rate limited — sleeping {secs:.0f}s", flush=True)
    time.sleep(secs)


def fetch_stars(
    session: requests.Session,
    owner: str,
    repo: str,
    min_sleep: float,
) -> int | None:
    """
    Return star count for owner/repo, or None on permanent failure.
    Handles 404 (deleted/renamed → 0), rate-limit 403/429, and network errors.
    min_sleep enforces the per-request rate limit before the API call.
    """
    time.sleep(min_sleep)
    url = f"{GITHUB_API_BASE}/repos/{owner}/{repo}"

    for attempt in range(4):  # initial attempt + 3 retries
        try:
            resp = session.get(url, timeout=15)
        except requests.RequestException as exc:
            if attempt == 3:
                print(f"  [gh] network error after 3 retries for {owner}/{repo}: {exc}")
                return None
            time.sleep(30)
            continue

        if resp.status_code == 200:
            return resp.json()["stargazers_count"]

        if resp.status_code == 404:
            return 0  # repo deleted or renamed; treat as 0 stars

        if resp.status_code in (403, 429):
            remaining = resp.headers.get("X-RateLimit-Remaining", "1")
            if remaining == "0":
                _sleep_until_reset(resp)
                continue
            # 403 with remaining > 0 is a permission/auth error — don't retry
            print(f"  [gh] 403 (auth/permission) for {owner}/{repo} — skipping")
            return None

        # Any other status: log and skip
        print(f"  [gh] unexpected status {resp.status_code} for {owner}/{repo} — skipping")
        return None

    print(f"  [gh] max retries exceeded for {owner}/{repo}")
    return None


# ── Main ──────────────────────────────────────────────────────────────────────

def main() -> None:

    parser = argparse.ArgumentParser(description="Backfill github_stars_t60 in seed parquet")
    parser.add_argument("--parquet", default=os.getenv("PARQUET", PARQUET_DEFAULT))
    parser.add_argument("--limit",   type=int, default=None,
                        help="Process at most N rows (for smoke testing)")
    args = parser.parse_args()

    token = os.getenv("GITHUB_TOKEN", "")
    if not token:
        print("WARN: GITHUB_TOKEN not set — unauthenticated rate limit (60 req/hr) applies.")
        print("      Full backfill will take ~28 days. Set GITHUB_TOKEN for a ~8.5hr run.")
        rate_per_min = UNAUTH_RATE_PER_MIN
    else:
        print("GITHUB_TOKEN set — authenticated rate limit (5 000 req/hr).")
        rate_per_min = AUTH_RATE_PER_MIN
    min_sleep = 60.0 / rate_per_min

    # ── Load parquet ──────────────────────────────────────────────────────────
    print(f"\nLoading {args.parquet} ...")
    df = pd.read_parquet(args.parquet)
    print(f"  {len(df):,} rows × {len(df.columns)} columns")

    # Add github_stars_t60 column if this is the first run
    if "github_stars_t60" not in df.columns:
        df["github_stars_t60"] = pd.NA

    # Rows to process: has_code=True and not yet labeled
    to_label = df[df["has_code"] == True]["github_stars_t60"].isna()  # noqa: E712
    pending = df[df["has_code"] == True][to_label.values].index.tolist()

    already_done = int((df["github_stars_t60"].notna() & (df["has_code"] == True)).sum())
    print(f"  Already labeled : {already_done:,}")
    print(f"  Pending         : {len(pending):,}")
    print(f"  No GitHub repo  : {int((df['has_code'] == False).sum()):,}  (skipped)")

    if args.limit:
        pending = pending[: args.limit]
        print(f"  --limit {args.limit}: processing {len(pending):,} rows")

    if not pending:
        print("\nNothing to do — all has_code rows already labeled.")
        sys.exit(0)

    eta_hrs = len(pending) / rate_per_min / 60
    print(f"\nEstimated time at {rate_per_min} req/min: {eta_hrs:.1f} hours")
    print("Starting in 5 seconds ... Ctrl-C to abort.\n")
    time.sleep(5)

    # ── Set up requests session ───────────────────────────────────────────────
    session = requests.Session()
    session.headers.update({
        "Accept":               "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
    })
    if token:
        session.headers["Authorization"] = f"Bearer {token}"

    # ── Fetch loop ────────────────────────────────────────────────────────────
    done = failed = skipped = 0
    checkpoint_path = args.parquet.replace(".parquet", ".checkpoint.parquet")

    for i, idx in enumerate(pending):
        repo_url = df.at[idx, "hf_github_repo"]

        if not isinstance(repo_url, str) or not repo_url.strip():
            skipped += 1
            continue

        owner, repo = parse_github_url(repo_url)
        if owner is None:
            print(f"  [skip] unparseable URL: {repo_url!r}")
            skipped += 1
            continue

        stars = fetch_stars(session, owner, repo, min_sleep)

        if stars is None:
            failed += 1
        else:
            df.at[idx, "github_stars_t60"] = stars
            done += 1

        # Progress log
        if (i + 1) % 100 == 0 or (i + 1) == len(pending):
            pct = 100 * (i + 1) / len(pending)
            print(
                f"  [{i+1:>6}/{len(pending)}  {pct:5.1f}%]  "
                f"done={done}  failed={failed}  skipped={skipped}",
                flush=True,
            )

        # Checkpoint
        if done > 0 and done % CHECKPOINT_EVERY == 0:
            df.to_parquet(checkpoint_path, index=False)
            print(f"  checkpoint written → {checkpoint_path}", flush=True)

    # ── Rewrite hype_label from new star counts ───────────────────────────────
    labeled_mask = df["github_stars_t60"].notna()
    df.loc[labeled_mask, "hype_label"] = df.loc[labeled_mask, "github_stars_t60"] > HYPE_THRESHOLD

    n_labeled  = int(labeled_mask.sum())
    n_positive = int(df.loc[labeled_mask, "hype_label"].sum())
    n_negative = n_labeled - n_positive
    print(f"\n── Label summary ────────────────────────────────────────")
    print(f"  Rows with github_stars_t60 : {n_labeled:,}")
    print(f"  Hype (stars > {HYPE_THRESHOLD})           : {n_positive:,}  ({100*n_positive/n_labeled:.1f}%)")
    print(f"  Not hype                   : {n_negative:,}  ({100*n_negative/n_labeled:.1f}%)")
    print(f"  Rows still proxy-labeled   : {int((~labeled_mask & df['has_code']).sum()):,}")

    # ── Save ──────────────────────────────────────────────────────────────────
    df.to_parquet(args.parquet, index=False)
    print(f"\nSaved → {args.parquet}")
    print("\nNext steps:")
    print("  1. Upload to S3:")
    print(f"       aws s3 cp {args.parquet} s3://six-eyes/papers_seed.parquet")
    print("  2. Trigger Prefect training run:")
    print("       prefect deployment run 'six-eyes-train/six-eyes-train'")


if __name__ == "__main__":
    main()
