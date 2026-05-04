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
    print(f"  Reference: {len(df):,} rows total")
    # Use only SS-enriched rows as reference — live papers are all enriched via
    # the ingestion service, so comparing against unenriched seed rows inflates drift.
    if "max_h_index" in df.columns:
        enriched = df[df["max_h_index"].notna() & (df["max_h_index"] > 0)]
        print(f"  Filtering to SS-enriched rows: {len(enriched):,}")
        df = enriched
    sample_n = min(n_rows, len(df))
    print(f"  Sampling {sample_n:,}")
    if "hype_label" in df.columns and df["hype_label"].nunique() > 1:
        return df.groupby("hype_label", group_keys=False).apply(
            lambda g: g.sample(frac=sample_n / len(df), random_state=random_state)
        ).reset_index(drop=True)
    return df.sample(n=sample_n, random_state=random_state).reset_index(drop=True)


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
                 report_date: str) -> tuple[Path, dict]:
    """Run Evidently DataDrift + DataQuality and save as HTML.

    Returns (html_path, report_dict) — report_dict is used for Grafana push.
    """
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
    return out_path, report.as_dict()


def update_index(report_dir: Path) -> None:
    """Regenerate index.html linking all drift-*.html reports, newest first."""
    reports = sorted(report_dir.glob("drift-*.html"), reverse=True)
    rows = "\n".join(
        f'    <li><a href="{r.name}">{r.stem.replace("drift-", "")}</a></li>'
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
    index_path = report_dir / "index.html"
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
    _, report_dict = build_report(reference, current, report_date)

    # Update index
    print("\n[3/3] Updating index")
    update_index(REPORT_DIR)

    print(f"\nDone. View at: https://jean-johnson-zwix.github.io/six-eyes/reports/drift-{report_date}.html")

    # Push metrics to Grafana Cloud (optional — skipped if env vars not set)
    _push_to_grafana(report_dict, current)


# ── Grafana metrics push ──────────────────────────────────────────────────────


def _compute_mean_hype_score(current: pd.DataFrame) -> float | None:
    """Download model from HuggingFace and compute mean predicted hype score."""
    model_base = os.getenv("MODEL_BASE_URL", "").rstrip("/") + "/"
    if not model_base.startswith("http"):
        return None
    try:
        import json
        import urllib.request
        import xgboost as xgb

        for fname in ("xgb_model.json", "model_meta.json"):
            urllib.request.urlretrieve(model_base + fname, f"/tmp/{fname}")

        with open("/tmp/model_meta.json") as f:
            meta = json.load(f)

        booster = xgb.Booster()
        booster.load_model("/tmp/xgb_model.json")

        X = build_features(current)
        dm = xgb.DMatrix(X.values, feature_names=meta["feature_cols"])
        scores = booster.predict(dm)
        mean_score = float(scores.mean())
        print(f"  Mean hype score: {mean_score:.4f}  (n={len(scores)})")
        return mean_score
    except Exception as exc:
        print(f"  WARN: could not compute hype scores: {exc}")
        return None


def _extract_evidently_metrics(report_dict: dict, n_current: int) -> list[tuple[dict, float]]:
    """Parse Evidently as_dict() output into (labels, value) pairs for Grafana."""
    metrics: list[tuple[dict, float]] = []
    metrics.append(({'__name__': 'six_eyes_current_papers_count'}, float(n_current)))

    for m in report_dict.get("metrics", []):
        name = m.get("metric", "")
        res  = m.get("result", {})

        if name == "DatasetDriftMetric":
            share = res.get("share_of_drifted_columns") or res.get("drift_share") or 0.0
            detected = res.get("dataset_drift", False)
            metrics.append(({'__name__': 'six_eyes_drift_share'},    float(share)))
            metrics.append(({'__name__': 'six_eyes_drift_detected'}, 1.0 if detected else 0.0))

        elif name == "DataDriftTable":
            for col, col_data in res.get("drift_by_columns", {}).items():
                score = col_data.get("drift_score") or 0.0
                metrics.append(({
                    '__name__': 'six_eyes_feature_drift_score',
                    'feature':  col,
                }, float(score)))

    return metrics


def _push_to_grafana(report_dict: dict, current: pd.DataFrame) -> None:
    """Push all Grafana metrics. No-op if GRAFANA_REMOTE_WRITE_URL is not set."""
    if not os.getenv("GRAFANA_REMOTE_WRITE_URL"):
        print("\n[Grafana] GRAFANA_REMOTE_WRITE_URL not set — skipping push.")
        return

    print("\n[4/4] Pushing metrics to Grafana Cloud")
    try:
        from push_metrics import push

        metrics = _extract_evidently_metrics(report_dict, len(current))

        mean_score = _compute_mean_hype_score(current)
        if mean_score is not None:
            metrics.append(({'__name__': 'six_eyes_mean_hype_score'}, mean_score))

        push(metrics)
    except Exception as exc:
        # Don't fail the whole job over a metrics push error
        print(f"  WARN: Grafana push failed: {exc}")


if __name__ == "__main__":
    main()
