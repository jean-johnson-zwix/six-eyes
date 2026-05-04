"""
monitor.py — weekly Evidently drift report for the six-eyes hype predictor  (req 6.1 / 6.5)

Compares the feature distribution of papers ingested in the last 30 days (current window)
against a 5,000-row sample of the seed training data (reference).

Outputs
-------
    reports/drift-YYYY-MM-DD.html   self-contained Evidently report
    reports/index.html              updated index linking all historical reports

Usage
-----
    python training/monitor.py                    # full run
    python training/monitor.py --days 7           # narrow current window
    python training/monitor.py --ref-rows 1000    # smaller reference sample (faster)

Env vars
--------
    SUPABASE_DB_URL           Postgres connection string for live papers
    REFERENCE_PARQUET_S3      s3://bucket/key for seed parquet  (default: s3://six-eyes/papers_seed.parquet)
    REPORT_DIR                output directory  (default: reports/)
"""

from __future__ import annotations

import argparse
import os
import sys
from datetime import date, datetime, timedelta, timezone
from pathlib import Path

import pandas as pd
from dotenv import load_dotenv

load_dotenv(Path(__file__).parent / ".env")

# Allow `from features import ...` regardless of CWD
sys.path.insert(0, str(Path(__file__).parent))
from features import build_features  # noqa: E402

# ── Config ────────────────────────────────────────────────────────────────────

REFERENCE_S3    = os.getenv("REFERENCE_PARQUET_S3", "s3://six-eyes/papers_seed.parquet")
REPORT_DIR      = Path(os.getenv("REPORT_DIR", "reports"))
MIN_CURRENT_ROWS = 30   # skip report if live window is too sparse to be meaningful


# ── Data loaders ─────────────────────────────────────────────────────────────

def load_reference(s3_uri: str, n_rows: int, random_state: int = 42) -> pd.DataFrame:
    """Download seed parquet from public S3 URL and return a stratified sample."""
    # Convert s3://bucket/key → public HTTPS URL (bucket is public-read)
    url = s3_uri.replace("s3://", "https://").replace(
        "six-eyes/", "six-eyes.s3.us-east-1.amazonaws.com/"
    ) if s3_uri.startswith("s3://") else s3_uri
    print(f"  Downloading reference parquet from {url} ...")
    df = pd.read_parquet(url)
    print(f"  Reference: {len(df):,} rows — sampling {n_rows:,}")
    # Stratified sample to preserve hype rate
    if "hype_label" in df.columns:
        return df.groupby("hype_label", group_keys=False).apply(
            lambda g: g.sample(frac=n_rows / len(df), random_state=random_state)
        ).reset_index(drop=True)
    return df.sample(n=min(n_rows, len(df)), random_state=random_state).reset_index(drop=True)


def load_current(db_url: str, days: int) -> pd.DataFrame:
    """Fetch papers submitted in the last `days` days from Supabase."""
    import psycopg2
    since = (datetime.now(timezone.utc) - timedelta(days=days)).isoformat()
    query = f"""
        SELECT arxiv_id, title, abstract, authors, categories, submitted_at,
               max_h_index, total_prior_papers,
               (max_h_index IS NOT NULL) AS has_author_enrichment
        FROM papers
        WHERE submitted_at >= '{since}'
        ORDER BY submitted_at DESC
    """
    print(f"  Querying Supabase for papers since {since[:10]} ...")
    conn = psycopg2.connect(db_url)
    try:
        df = pd.read_sql(query, conn)
    finally:
        conn.close()
    print(f"  Current window: {len(df):,} rows")
    return df


# ── Report builder ────────────────────────────────────────────────────────────

def build_report(reference: pd.DataFrame, current: pd.DataFrame,
                 report_date: str) -> Path:
    """Run Evidently DataDrift + DataQuality and save as HTML."""
    from evidently.report import Report
    from evidently.metric_preset import DataDriftPreset, DataQualityPreset

    print("  Building feature matrices ...")
    X_ref = build_features(reference)
    X_cur = build_features(current)

    print("  Running Evidently report ...")
    report = Report(metrics=[
        DataQualityPreset(),
        DataDriftPreset(),
    ])
    report.run(reference_data=X_ref, current_data=X_cur)

    REPORT_DIR.mkdir(parents=True, exist_ok=True)
    out_path = REPORT_DIR / f"drift-{report_date}.html"
    report.save_html(str(out_path))
    print(f"  Report saved → {out_path}")
    return out_path


def update_index(report_dir: Path) -> None:
    """Regenerate index.html linking all drift-*.html reports, newest first."""
    reports = sorted(report_dir.glob("drift-*.html"), reverse=True)
    rows = "\n".join(
        f'    <li><a href="reports/{r.name}">{r.stem.replace("drift-", "")}</a></li>'
        for r in reports
    )
    html = f"""<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>six-eyes drift reports</title>
<style>body{{font-family:sans-serif;max-width:600px;margin:40px auto}}
a{{color:#0066cc}}</style></head>
<body>
<h1>six-eyes — drift reports</h1>
<p>Weekly Evidently feature drift: live Supabase papers vs seed training distribution.</p>
<ul>
{rows}
</ul>
</body>
</html>
"""
    # index lives one level above reports/ so it's the gh-pages root
    index_path = report_dir.parent / "index.html"
    index_path.write_text(html)
    print(f"  Index updated → {index_path}")


# ── Main ──────────────────────────────────────────────────────────────────────

def main() -> None:
    parser = argparse.ArgumentParser(description="Generate Evidently drift report")
    parser.add_argument("--days",     type=int, default=30,
                        help="Current window in days (default: 30)")
    parser.add_argument("--ref-rows", type=int, default=5000,
                        help="Reference sample size (default: 5000)")
    args = parser.parse_args()

    report_date = date.today().isoformat()
    print(f"\n── six-eyes drift monitor — {report_date} ─────────────────────")

    db_url = os.getenv("SUPABASE_DB_URL", "")
    if not db_url:
        print("ERROR: SUPABASE_DB_URL not set")
        sys.exit(1)

    # Load data
    print("\n[1/3] Loading data")
    reference = load_reference(REFERENCE_S3, args.ref_rows)
    current   = load_current(db_url, args.days)

    if len(current) < MIN_CURRENT_ROWS:
        print(f"\nWARN: only {len(current)} rows in current window (< {MIN_CURRENT_ROWS}).")
        print("  Report skipped — not enough live data for meaningful drift analysis.")
        print("  Hint: widen --days or wait until more papers have been ingested.")
        # Still create the report dir so the GHA deploy step doesn't fail
        REPORT_DIR.mkdir(parents=True, exist_ok=True)
        (REPORT_DIR / ".gitkeep").touch()
        sys.exit(0)

    # Build report
    print("\n[2/3] Building Evidently report")
    build_report(reference, current, report_date)

    # Update index
    print("\n[3/3] Updating index")
    update_index(REPORT_DIR)

    print(f"\nDone. View at: https://jean-johnson-zwix.github.io/six-eyes/reports/drift-{report_date}.html")


if __name__ == "__main__":
    main()
