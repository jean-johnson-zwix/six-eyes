"""
train.py — train and log baseline models for the six-eyes hype predictor

Models
------
1. Logistic Regression (baseline)     req 4.1
2. XGBoost classifier                 req 4.2

Each model is logged as a separate MLflow run under the experiment
"six-eyes-v1". Metrics, params, feature list, and the serialised model
artifact are all recorded so runs are fully reproducible.

Usage
-----
    python train.py                              # both models
    python train.py --model lr                   # baseline only
    python train.py --model xgb                  # XGBoost only
    python train.py --parquet path/to/file.parquet

Env vars
--------
    PARQUET               path to input parquet  (default: seed/papers_seed.parquet)
    MLFLOW_TRACKING_URI   MLflow server URL       (default: mlruns  — local)
"""

from __future__ import annotations

import argparse
import os

import mlflow
import mlflow.sklearn
import mlflow.xgboost
from mlflow.models import infer_signature
import numpy as np
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import (
    average_precision_score,
    classification_report,
    f1_score,
    roc_auc_score,
)
from sklearn.model_selection import train_test_split
from sklearn.pipeline import Pipeline
from sklearn.preprocessing import StandardScaler
from xgboost import XGBClassifier

from features import FEATURE_COLS, class_balance_report, load_parquet

# ── Config ───────────────────────────────────────────────────────────────────

PARQUET     = os.getenv("PARQUET", "seed/papers_seed.parquet")
EXPERIMENT  = "six-eyes-v1"
RANDOM_SEED = 42

# ── Data split ───────────────────────────────────────────────────────────────

def split_data(X, y):
    """Stratified 80 / 10 / 10 train / val / test split."""
    X_train, X_tmp, y_train, y_tmp = train_test_split(
        X, y, test_size=0.20, stratify=y, random_state=RANDOM_SEED
    )
    X_val, X_test, y_val, y_test = train_test_split(
        X_tmp, y_tmp, test_size=0.50, stratify=y_tmp, random_state=RANDOM_SEED
    )
    return X_train, X_val, X_test, y_train, y_val, y_test


# ── Metrics ──────────────────────────────────────────────────────────────────

def compute_metrics(model, X, y, prefix: str) -> dict:
    """
    Compute ROC-AUC, PR-AUC (average precision), and F1 at 0.5 threshold.
    PR-AUC is the primary metric — accuracy is misleading with 17.7% positives.
    """
    proba = model.predict_proba(X)[:, 1]
    pred  = (proba >= 0.5).astype(int)
    return {
        f"{prefix}_roc_auc": round(roc_auc_score(y, proba), 4),
        f"{prefix}_pr_auc":  round(average_precision_score(y, proba), 4),
        f"{prefix}_f1":      round(f1_score(y, pred), 4),
    }


# ── Logistic Regression ──────────────────────────────────────────────────────

def train_lr(X_train, X_val, X_test, y_train, y_val, y_test):
    scale_pos = int((y_train == 0).sum() / (y_train == 1).sum())

    params = {
        "model":         "logistic_regression",
        "feature_version": "v1",
        "C":             1.0,
        "max_iter":      1000,
        "class_weight":  "balanced",
        "solver":        "lbfgs",
        "random_state":  RANDOM_SEED,
    }

    # StandardScaler is required for LR — tree models don't need it.
    # Fit scaler on train only; apply to val/test (req 4.5).
    pipeline = Pipeline([
        ("scaler", StandardScaler()),
        ("clf",    LogisticRegression(
            C=params["C"],
            max_iter=params["max_iter"],
            class_weight=params["class_weight"],
            solver=params["solver"],
            random_state=params["random_state"],
        )),
    ])

    with mlflow.start_run(run_name="lr-baseline"):
        pipeline.fit(X_train, y_train)

        metrics = {}
        metrics.update(compute_metrics(pipeline, X_val,  y_val,  "val"))
        metrics.update(compute_metrics(pipeline, X_test, y_test, "test"))

        mlflow.log_params(params)
        mlflow.log_metrics(metrics)
        mlflow.log_param("n_features",   len(FEATURE_COLS))
        mlflow.log_param("feature_cols", str(FEATURE_COLS))
        mlflow.log_param("train_size",   len(X_train))
        mlflow.log_param("val_size",     len(X_val))
        mlflow.log_param("test_size",    len(X_test))
        signature = infer_signature(X_train, pipeline.predict_proba(X_train)[:, 1])
        model_info = mlflow.sklearn.log_model(pipeline, name="model",
                                              registered_model_name="six-eyes-lr",
                                              signature=signature)
        mlflow.set_tag("model_type", "logistic_regression")
        mlflow.set_tag("feature_version", "v1")
        mlflow.set_tag("label_source", "pwc_proxy")
        client = mlflow.MlflowClient()
        client.update_registered_model(
            "six-eyes-lr",
            description="Logistic Regression baseline (V1 features: metadata + title buzzwords). "
                        "Label is PwC code-link presence proxy — to be replaced with github_stars_t60 > 100.",
        )
        client.set_registered_model_tag("six-eyes-lr", "feature_version", "v1")
        client.set_registered_model_tag("six-eyes-lr", "label", "pwc_proxy")

        print(f"\n[LR baseline]")
        print(f"  val  PR-AUC={metrics['val_pr_auc']}  ROC-AUC={metrics['val_roc_auc']}  F1={metrics['val_f1']}")
        print(f"  test PR-AUC={metrics['test_pr_auc']}  ROC-AUC={metrics['test_roc_auc']}  F1={metrics['test_f1']}")
        print(classification_report(y_test, pipeline.predict(X_test),
                                    target_names=["not-hype", "hype"]))


# ── XGBoost ──────────────────────────────────────────────────────────────────

def train_xgb(X_train, X_val, X_test, y_train, y_val, y_test):
    # scale_pos_weight compensates for 17.7% positive rate (~4.6x imbalance).
    scale_pos_weight = round((y_train == 0).sum() / (y_train == 1).sum(), 2)

    params = {
        "model":             "xgboost",
        "feature_version":   "v1",
        "n_estimators":      300,
        "max_depth":         6,
        "learning_rate":     0.05,
        "subsample":         0.8,
        "colsample_bytree":  0.8,
        "scale_pos_weight":  scale_pos_weight,
        "eval_metric":       "aucpr",
        "random_state":      RANDOM_SEED,
    }

    clf = XGBClassifier(
        n_estimators=params["n_estimators"],
        max_depth=params["max_depth"],
        learning_rate=params["learning_rate"],
        subsample=params["subsample"],
        colsample_bytree=params["colsample_bytree"],
        scale_pos_weight=params["scale_pos_weight"],
        eval_metric=params["eval_metric"],
        random_state=params["random_state"],
        verbosity=0,
    )

    with mlflow.start_run(run_name="xgb-v1"):
        clf.fit(
            X_train, y_train,
            eval_set=[(X_val, y_val)],
            verbose=False,
        )

        metrics = {}
        metrics.update(compute_metrics(clf, X_val,  y_val,  "val"))
        metrics.update(compute_metrics(clf, X_test, y_test, "test"))

        mlflow.log_params(params)
        mlflow.log_metrics(metrics)
        mlflow.log_param("n_features",   len(FEATURE_COLS))
        mlflow.log_param("feature_cols", str(FEATURE_COLS))
        mlflow.log_param("train_size",   len(X_train))
        mlflow.log_param("val_size",     len(X_val))
        mlflow.log_param("test_size",    len(X_test))
        signature = infer_signature(X_train, clf.predict_proba(X_train)[:, 1])
        mlflow.xgboost.log_model(clf, name="model",
                                 registered_model_name="six-eyes-xgb",
                                 signature=signature)
        mlflow.set_tag("model_type", "xgboost")
        mlflow.set_tag("feature_version", "v1")
        mlflow.set_tag("label_source", "pwc_proxy")
        client = mlflow.MlflowClient()
        client.update_registered_model(
            "six-eyes-xgb",
            description="XGBoost classifier (V1 features: metadata + title buzzwords). "
                        "Label is PwC code-link presence proxy — to be replaced with github_stars_t60 > 100.",
        )
        client.set_registered_model_tag("six-eyes-xgb", "feature_version", "v1")
        client.set_registered_model_tag("six-eyes-xgb", "label", "pwc_proxy")

        # Feature importance — logged as a param for quick inspection in UI.
        importances = dict(zip(
            FEATURE_COLS,
            clf.feature_importances_.round(4).tolist()
        ))
        top5 = sorted(importances.items(), key=lambda x: x[1], reverse=True)[:5]
        mlflow.log_param("top5_features", str(top5))

        print(f"\n[XGBoost v1]")
        print(f"  val  PR-AUC={metrics['val_pr_auc']}  ROC-AUC={metrics['val_roc_auc']}  F1={metrics['val_f1']}")
        print(f"  test PR-AUC={metrics['test_pr_auc']}  ROC-AUC={metrics['test_roc_auc']}  F1={metrics['test_f1']}")
        print(f"  top-5 features: {top5}")
        print(classification_report(y_test, clf.predict(X_test),
                                    target_names=["not-hype", "hype"]))


# ── Main ─────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model",   choices=["lr", "xgb", "both"], default="both")
    parser.add_argument("--parquet", default=PARQUET)
    args = parser.parse_args()

    print(f"Loading {args.parquet} ...")
    X, y = load_parquet(args.parquet)
    print(f"  {len(X):,} rows × {len(X.columns)} features")
    class_balance_report(y)

    X_train, X_val, X_test, y_train, y_val, y_test = split_data(X, y)
    print(f"\n  train={len(X_train):,}  val={len(X_val):,}  test={len(X_test):,}")

    mlflow.set_tracking_uri(os.getenv("MLFLOW_TRACKING_URI", "sqlite:///mlflow.db"))
    mlflow.set_experiment(EXPERIMENT)

    if args.model in ("lr", "both"):
        train_lr(X_train, X_val, X_test, y_train, y_val, y_test)

    if args.model in ("xgb", "both"):
        train_xgb(X_train, X_val, X_test, y_train, y_val, y_test)


if __name__ == "__main__":
    main()
