"""
train_flow.py — Prefect orchestration for the six-eyes training pipeline  (Module 03)

Tasks
-----
  load_data          load parquet → (X, y)
  prepare_splits     stratified 80/10/10 split + scale_pos_weight
  run_optuna_study   50-trial Optuna search; each trial is a nested MLflow run
  train_and_register retrain best params on train+val; evaluate test; register model

Run locally
-----------
    # from project root:
    python flows/train_flow.py
    python flows/train_flow.py --n-trials 20
    python flows/train_flow.py --parquet training/seed/papers_seed.parquet

Deploy to Prefect Cloud
-----------------------
    prefect deploy flows/train_flow.py:train_flow --name six-eyes-train --pool <pool>

Env vars
--------
    PARQUET               override default parquet path
    MLFLOW_TRACKING_URI   override default MLflow URI
"""

from __future__ import annotations

import argparse
import os
import sys

# Allow imports from training/ regardless of working directory.
_FLOWS_DIR   = os.path.dirname(os.path.abspath(__file__))
_PROJECT_ROOT = os.path.dirname(_FLOWS_DIR)
sys.path.insert(0, os.path.join(_PROJECT_ROOT, "training"))

import mlflow
import mlflow.xgboost
import optuna
import pandas as pd
from mlflow.models import infer_signature
from prefect import flow, task, get_run_logger
from prefect.blocks.system import Secret
from sklearn.metrics import average_precision_score, f1_score, precision_recall_curve, roc_auc_score
from xgboost import XGBClassifier

from features import FEATURE_COLS, class_balance_report, load_parquet, split_data
from tune import make_objective

# ── Constants ─────────────────────────────────────────────────────────────────

EXPERIMENT  = "six-eyes-v1"
RANDOM_SEED = 42

_DEFAULT_PARQUET = os.getenv(
    "PARQUET",
    os.path.join(_PROJECT_ROOT, "training", "seed", "papers_seed.parquet"),
)
_DEFAULT_MLFLOW_URI = os.getenv(
    "MLFLOW_TRACKING_URI",
    f"sqlite:///{os.path.join(_PROJECT_ROOT, 'training', 'mlflow.db')}",
)

optuna.logging.set_verbosity(optuna.logging.WARNING)


# ── Tasks ─────────────────────────────────────────────────────────────────────

@task(name="load-data", log_prints=True)
def load_data(parquet_path: str):
    logger = get_run_logger()
    logger.info(f"Loading {parquet_path}")
    X, y = load_parquet(parquet_path)
    logger.info(f"{len(X):,} rows × {len(X.columns)} features")
    class_balance_report(y)
    return X, y


@task(name="prepare-splits", log_prints=True)
def prepare_splits(X, y):
    logger = get_run_logger()
    X_train, X_val, X_test, y_train, y_val, y_test = split_data(X, y, RANDOM_SEED)
    scale_pos_weight = round((y_train == 0).sum() / (y_train == 1).sum(), 2)
    logger.info(
        f"train={len(X_train):,}  val={len(X_val):,}  test={len(X_test):,}"
        f"  scale_pos_weight={scale_pos_weight}"
    )
    return X_train, X_val, X_test, y_train, y_val, y_test, scale_pos_weight


@task(name="run-optuna-study", log_prints=True)
def run_optuna_study(
    X_train, X_val, y_train, y_val,
    scale_pos_weight: float,
    n_trials: int,
):
    logger = get_run_logger()
    logger.info(f"Starting Optuna study — {n_trials} trials")

    with mlflow.start_run(run_name="optuna-xgb-tuning"):
        mlflow.log_param("n_trials",        n_trials)
        mlflow.log_param("feature_version", "v1")
        mlflow.log_param("n_features",      len(FEATURE_COLS))
        mlflow.set_tag("job", "hyperparameter_tuning")

        study = optuna.create_study(
            direction="maximize",
            sampler=optuna.samplers.TPESampler(seed=RANDOM_SEED),
        )
        study.optimize(
            make_objective(X_train, X_val, y_train, y_val, scale_pos_weight),
            n_trials=n_trials,
            show_progress_bar=False,
        )

        best = study.best_trial
        mlflow.log_metric("best_val_pr_auc", round(best.value, 4))
        mlflow.log_param("best_trial",       best.number)
        mlflow.log_params({f"best_{k}": v for k, v in best.params.items()})

    logger.info(f"Best trial #{best.number} — val PR-AUC={best.value:.4f}")
    logger.info(f"Best params: {best.params}")
    return best.params


def _find_threshold(proba, y, min_precision: float = 0.30) -> float:
    """Return the lowest threshold where precision >= min_precision. Falls back to 0.5."""
    prec, rec, thresholds = precision_recall_curve(y, proba)
    for p, r, t in zip(prec, rec, thresholds):
        if p >= min_precision:
            return float(round(t, 4))
    return 0.5


@task(name="train-and-register", log_prints=True)
def train_and_register(
    best_params: dict,
    X_train, X_val, X_test,
    y_train, y_val, y_test,
    scale_pos_weight: float,
):
    logger = get_run_logger()

    # Find threshold on val using probe fit on train only (val must stay held-out).
    probe = XGBClassifier(
        **best_params,
        scale_pos_weight=scale_pos_weight,
        eval_metric="aucpr",
        random_state=RANDOM_SEED,
        verbosity=0,
    )
    probe.fit(X_train, y_train, verbose=False)
    threshold = _find_threshold(probe.predict_proba(X_val)[:, 1], y_val)
    logger.info(f"Optimal threshold={threshold}  (min_precision=0.30)")

    X_trainval = pd.concat([X_train, X_val])
    y_trainval = pd.concat([y_train, y_val])

    clf = XGBClassifier(
        **best_params,
        scale_pos_weight=scale_pos_weight,
        eval_metric="aucpr",
        random_state=RANDOM_SEED,
        verbosity=0,
    )
    clf.fit(X_trainval, y_trainval, verbose=False)

    proba   = clf.predict_proba(X_test)[:, 1]
    pred    = (proba >= threshold).astype(int)
    pr_auc  = round(average_precision_score(y_test, proba), 4)
    roc_auc = round(roc_auc_score(y_test, proba), 4)
    f1      = round(f1_score(y_test, pred), 4)

    logger.info(f"Test  PR-AUC={pr_auc}  ROC-AUC={roc_auc}  F1={f1}")

    with mlflow.start_run(run_name="xgb-best"):
        mlflow.log_params(best_params)
        mlflow.log_param("scale_pos_weight",  scale_pos_weight)
        mlflow.log_param("trained_on",        "train+val")
        mlflow.log_param("optimal_threshold", threshold)
        mlflow.log_metric("test_pr_auc",      pr_auc)
        mlflow.log_metric("test_roc_auc",     roc_auc)
        mlflow.log_metric("test_f1",          f1)
        mlflow.set_tag("feature_version",     "v1")
        mlflow.set_tag("label_source",        "github_stars_t60")

        signature = infer_signature(X_trainval, clf.predict_proba(X_trainval)[:, 1])
        mlflow.xgboost.log_model(
            clf, name="model",
            registered_model_name="six-eyes-xgb",
            signature=signature,
        )

    return {"test_pr_auc": pr_auc, "test_roc_auc": roc_auc, "test_f1": f1, "optimal_threshold": threshold}


# ── Flow ──────────────────────────────────────────────────────────────────────

@flow(name="six-eyes-train", log_prints=True)
def train_flow(
    parquet_path: str = _DEFAULT_PARQUET,
    n_trials: int = 50,
    mlflow_tracking_uri: str = _DEFAULT_MLFLOW_URI,
):
    """End-to-end training flow: load → split → tune → register."""
    try:
        token = Secret.load("dagshub-token").get()
        os.environ["MLFLOW_TRACKING_PASSWORD"] = token
    except Exception:
        pass  # local runs use MLFLOW_TRACKING_PASSWORD from env / .env

    mlflow.set_tracking_uri(mlflow_tracking_uri)
    mlflow.set_experiment(EXPERIMENT)

    X, y                                                          = load_data(parquet_path)
    X_train, X_val, X_test, y_train, y_val, y_test, spw          = prepare_splits(X, y)
    best_params                                                    = run_optuna_study(X_train, X_val, y_train, y_val, spw, n_trials)
    metrics                                                        = train_and_register(best_params, X_train, X_val, X_test, y_train, y_val, y_test, spw)

    return metrics


# ── CLI ───────────────────────────────────────────────────────────────────────

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Run the six-eyes training flow locally.")
    parser.add_argument("--parquet",   default=_DEFAULT_PARQUET,    help="Path to input parquet")
    parser.add_argument("--n-trials",  type=int, default=50,         help="Optuna trial count")
    parser.add_argument("--mlflow-uri",default=_DEFAULT_MLFLOW_URI,  help="MLflow tracking URI")
    args = parser.parse_args()

    result = train_flow(
        parquet_path=args.parquet,
        n_trials=args.n_trials,
        mlflow_tracking_uri=args.mlflow_uri,
    )
    print(f"\nFlow complete — {result}")
