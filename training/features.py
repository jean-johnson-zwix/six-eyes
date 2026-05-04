"""
features.py — feature engineering for the six-eyes hype predictor

Feature set (V1: Metadata + Title)
-----------------------------------
2.1  Paper metadata : num_authors, abstract_length, day_of_week, month,
                      num_categories, category multi-hot (cs.LG/CV/AI/CL)
2.3  Title signals  : title_length, buzz_* binary flags (14 buzzwords)

Feature set (V2: + Author signals, added after SS backfill)
------------------------------------------------------------
2.2  Author signals : max_h_index, total_prior_papers, has_author_enrichment

Deferred:
    has_code   — in seed data has_code == hype_label (PwC leakage)
    hf_upvotes — not present in seed parquet (HF enrichment on live Supabase only)

All transforms are vectorised — safe for 229K rows.
"""

from __future__ import annotations

import pandas as pd

# ── Constants ────────────────────────────────────────────────────────────────

CATEGORIES: list[str] = ["cs.LG", "cs.AI", "cs.CV", "cs.CL"]

BUZZWORDS: list[str] = [
    "transformer", "diffusion", "agent", "multimodal", "rlhf",
    "llm", "mamba", "vision", "reasoning", "inference",
    "quantization", "scaling", "foundation", "eval",
]

V1_FEATURE_COLS: list[str] = (
    ["num_authors", "abstract_length", "title_length",
     "day_of_week", "month", "num_categories"]
    + [f"cat_{c.replace('.', '_')}" for c in CATEGORIES]
    + [f"buzz_{w}" for w in BUZZWORDS]
)

V2_AUTHOR_COLS: list[str] = [
    "max_h_index", "total_prior_papers", "has_author_enrichment",
]

# Active feature set — switch to V1_FEATURE_COLS + V2_AUTHOR_COLS after SS backfill.
FEATURE_COLS: list[str] = V1_FEATURE_COLS + V2_AUTHOR_COLS


# ── Feature builder ──────────────────────────────────────────────────────────

def build_features(df: pd.DataFrame) -> pd.DataFrame:
    """
    Transform raw paper records into a V1 feature matrix.
    Returns a DataFrame with exactly the columns in FEATURE_COLS.
    """
    feat = pd.DataFrame(index=df.index)

    # Paper metadata
    feat["num_authors"]     = df["authors"].apply(lambda a: len(a) if a is not None else 0)
    feat["abstract_length"] = df["abstract"].str.len().fillna(0).astype(int)
    feat["title_length"]    = df["title"].str.len().fillna(0).astype(int)
    feat["num_categories"]  = df["categories"].apply(lambda c: len(c) if c is not None else 0)

    submitted = pd.to_datetime(df["submitted_at"], utc=True, errors="coerce")
    feat["day_of_week"] = submitted.dt.dayofweek  # 0=Mon … 6=Sun
    feat["month"]       = submitted.dt.month       # 1–12

    # Category multi-hot (a paper can belong to multiple categories)
    for cat in CATEGORIES:
        feat[f"cat_{cat.replace('.', '_')}"] = df["categories"].apply(
            lambda c, _cat=cat: int(_cat in c) if c is not None else 0
        )

    # Title buzzword flags
    title_lower = df["title"].str.lower().fillna("")
    for word in BUZZWORDS:
        feat[f"buzz_{word}"] = title_lower.str.contains(word, regex=False).astype(int)

    # V2 author signals (present after SS backfill; zero-filled if absent)
    if "max_h_index" in df.columns:
        feat["max_h_index"]           = pd.to_numeric(df["max_h_index"],        errors="coerce").fillna(0).astype(int)
        feat["total_prior_papers"]    = pd.to_numeric(df["total_prior_papers"],  errors="coerce").fillna(0).astype(int)
        feat["has_author_enrichment"] = df["has_author_enrichment"].fillna(False).astype(int)
    else:
        feat["max_h_index"]           = 0
        feat["total_prior_papers"]    = 0
        feat["has_author_enrichment"] = 0

    return feat[FEATURE_COLS]


# ── Label ────────────────────────────────────────────────────────────────────

def get_label(df: pd.DataFrame) -> pd.Series:
    """Returns the binary hype_label column as int (0/1)."""
    return df["hype_label"].astype(int)


# ── Data loader ──────────────────────────────────────────────────────────────

def load_parquet(path: str) -> tuple[pd.DataFrame, pd.Series]:
    """
    Load a papers parquet file and return (X, y).
    Drops rows where hype_label is null.
    Uses DuckDB for fast columnar reads on large files.
    """
    import duckdb
    df = duckdb.connect().execute(f"SELECT * FROM read_parquet('{path}')").df()
    df = df.dropna(subset=["hype_label"]).reset_index(drop=True)
    return build_features(df), get_label(df)


# ── Split ────────────────────────────────────────────────────────────────────

def split_data(X, y, random_state: int = 42):
    """Stratified 80 / 10 / 10 train / val / test split."""
    from sklearn.model_selection import train_test_split
    X_train, X_tmp, y_train, y_tmp = train_test_split(
        X, y, test_size=0.20, stratify=y, random_state=random_state
    )
    X_val, X_test, y_val, y_test = train_test_split(
        X_tmp, y_tmp, test_size=0.50, stratify=y_tmp, random_state=random_state
    )
    return X_train, X_val, X_test, y_train, y_val, y_test


# ── Diagnostics ──────────────────────────────────────────────────────────────

def class_balance_report(y: pd.Series) -> None:
    """Print class balance. Call before training to catch imbalance early."""
    total = len(y)
    n_pos = int(y.sum())
    n_neg = total - n_pos
    print(f"  Total labelled : {total:,}")
    print(f"  Positive (hype): {n_pos:,}  ({100 * n_pos / total:.1f}%)")
    print(f"  Negative       : {n_neg:,}  ({100 * n_neg / total:.1f}%)")
    if n_pos / total < 0.10:
        print("  WARNING: <10% positive — set scale_pos_weight in XGBoost "
              "or class_weight='balanced' in LogisticRegression.")


# ── Smoke test ───────────────────────────────────────────────────────────────

if __name__ == "__main__":
    import sys
    path = sys.argv[1] if len(sys.argv) > 1 else "seed/papers_seed.parquet"

    print(f"Loading {path} ...")
    X, y = load_parquet(path)

    print(f"  Feature matrix : {X.shape}")
    print(f"  Features       : {X.columns.tolist()}")
    nulls = X.isnull().sum()
    if nulls.any():
        print(f"  Nulls in X     :\n{nulls[nulls > 0]}")
    else:
        print("  Nulls in X     : none")
    print()
    class_balance_report(y)
