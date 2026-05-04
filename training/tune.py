"""
tune.py — Optuna hyperparameter tuning for XGBoost  (req 4.3)

Each Optuna trial is one nested MLflow run under a parent "optuna-xgb" run,
so the MLflow UI shows every trial's params and PR-AUC in one place.

After all trials complete, the best model is trained on train+val, evaluated
on the held-out test set, and registered as a new version in the model registry.

Usage
-----
    python tune.py                   # 50 trials (default)
    python tune.py --n-trials 100
    python tune.py --parquet path/to/file.parquet

Env vars
--------
    PARQUET               input parquet  (default: seed/papers_seed.parquet)
    MLFLOW_TRACKING_URI   MLflow server  (default: sqlite:///mlflow.db)
"""

from __future__ import annotations

import argparse
import os

import mlflow
import mlflow.xgboost
from mlflow.models import infer_signature
import numpy as np
import optuna
from sklearn.metrics import average_precision_score, classification_report, precision_recall_curve, roc_auc_score, f1_score
from xgboost import XGBClassifier

from features import FEATURE_COLS, class_balance_report, load_parquet, split_data

PARQUET    = os.getenv("PARQUET", "seed/papers_seed.parquet")
EXPERIMENT = "six-eyes-v1"
RANDOM_SEED = 42

optuna.logging.set_verbosity(optuna.logging.WARNING)


def make_objective(X_train, X_val, y_train, y_val, scale_pos_weight: float):
    """Return an Optuna objective that logs each trial as a nested MLflow run."""

    def objective(trial: optuna.Trial) -> float:
        params = {
            "n_estimators":      trial.suggest_int("n_estimators", 100, 600),
            "max_depth":         trial.suggest_int("max_depth", 3, 9),
            "learning_rate":     trial.suggest_float("learning_rate", 0.01, 0.3, log=True),
            "subsample":         trial.suggest_float("subsample", 0.6, 1.0),
            "colsample_bytree":  trial.suggest_float("colsample_bytree", 0.6, 1.0),
            "min_child_weight":  trial.suggest_int("min_child_weight", 1, 10),
            "gamma":             trial.suggest_float("gamma", 0.0, 5.0),
            "reg_alpha":         trial.suggest_float("reg_alpha", 0.0, 5.0),
            "reg_lambda":        trial.suggest_float("reg_lambda", 0.0, 5.0),
        }

        with mlflow.start_run(run_name=f"trial-{trial.number:03d}", nested=True):
            clf = XGBClassifier(
                **params,
                scale_pos_weight=scale_pos_weight,
                eval_metric="aucpr",
                random_state=RANDOM_SEED,
                verbosity=0,
            )
            clf.fit(X_train, y_train, eval_set=[(X_val, y_val)], verbose=False)

            proba   = clf.predict_proba(X_val)[:, 1]
            pr_auc  = average_precision_score(y_val, proba)
            roc_auc = roc_auc_score(y_val, proba)

            mlflow.log_params(params)
            mlflow.log_param("scale_pos_weight", scale_pos_weight)
            mlflow.log_metric("val_pr_auc",  round(pr_auc,  4))
            mlflow.log_metric("val_roc_auc", round(roc_auc, 4))
            mlflow.set_tag("trial_number", trial.number)

        return pr_auc

    return objective


def find_threshold(proba, y) -> float:
    """Find the threshold that maximises F1 on the val set."""
    prec, rec, thresholds = precision_recall_curve(y, proba)
    f1s = 2 * prec[:-1] * rec[:-1] / (prec[:-1] + rec[:-1] + 1e-9)
    return float(round(float(thresholds[f1s.argmax()]), 4))


def train_best(params, X_train, X_val, X_test, y_train, y_val, y_test,
               scale_pos_weight: float):
    """Retrain with best params on train+val, evaluate on test, register model."""
    import pandas as pd

    # Find threshold on val using a probe fit on train only (val must stay held-out).
    probe = XGBClassifier(
        **params,
        scale_pos_weight=scale_pos_weight,
        eval_metric="aucpr",
        random_state=RANDOM_SEED,
        verbosity=0,
    )
    probe.fit(X_train, y_train, verbose=False)
    threshold = find_threshold(probe.predict_proba(X_val)[:, 1], y_val)


    X_trainval = pd.concat([X_train, X_val])
    y_trainval = pd.concat([y_train, y_val])

    clf = XGBClassifier(
        **params,
        scale_pos_weight=scale_pos_weight,
        eval_metric="aucpr",
        random_state=RANDOM_SEED,
        verbosity=0,
    )
    clf.fit(X_trainval, y_trainval, verbose=False)

    proba   = clf.predict_proba(X_test)[:, 1]
    pred    = (proba >= threshold).astype(int)
    pr_auc  = average_precision_score(y_test, proba)
    roc_auc = roc_auc_score(y_test, proba)
    f1      = f1_score(y_test, pred)

    print(f"\n[Best model — test set]")
    print(f"  threshold={threshold}  (F1-maximising on val)")
    print(f"  PR-AUC={pr_auc:.4f}  ROC-AUC={roc_auc:.4f}  F1={f1:.4f}")
    print(classification_report(y_test, pred, target_names=["not-hype", "hype"]))

    with mlflow.start_run(run_name="xgb-best", nested=True):
        mlflow.log_params(params)
        mlflow.log_param("scale_pos_weight",  scale_pos_weight)
        mlflow.log_param("trained_on",        "train+val")
        mlflow.log_param("optimal_threshold", threshold)
        mlflow.log_metric("test_pr_auc",  round(pr_auc,  4))
        mlflow.log_metric("test_roc_auc", round(roc_auc, 4))
        mlflow.log_metric("test_f1",      round(f1,      4))
        mlflow.set_tag("feature_version", "v1")
        mlflow.set_tag("label_source",    "github_stars_t60")

        signature = infer_signature(X_trainval, clf.predict_proba(X_trainval)[:, 1])
        mlflow.xgboost.log_model(clf, name="model",
                                 registered_model_name="six-eyes-xgb",
                                 signature=signature)

        client = mlflow.MlflowClient()
        client.set_registered_model_tag("six-eyes-xgb", "feature_version", "v1")
        client.set_registered_model_tag("six-eyes-xgb", "tuned", "optuna")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--n-trials", type=int, default=50)
    parser.add_argument("--parquet",  default=PARQUET)
    args = parser.parse_args()

    print(f"Loading {args.parquet} ...")
    X, y = load_parquet(args.parquet)
    print(f"  {len(X):,} rows × {len(X.columns)} features")
    class_balance_report(y)

    X_train, X_val, X_test, y_train, y_val, y_test = split_data(X, y, RANDOM_SEED)
    scale_pos_weight = round((y_train == 0).sum() / (y_train == 1).sum(), 2)
    print(f"\n  train={len(X_train):,}  val={len(X_val):,}  test={len(X_test):,}")
    print(f"  scale_pos_weight={scale_pos_weight}")

    mlflow.set_tracking_uri(os.getenv("MLFLOW_TRACKING_URI", "sqlite:///mlflow.db"))
    mlflow.set_experiment(EXPERIMENT)

    print(f"\nRunning {args.n_trials} Optuna trials ...")
    with mlflow.start_run(run_name="optuna-xgb-tuning"):
        mlflow.log_param("n_trials",       args.n_trials)
        mlflow.log_param("feature_version", "v1")
        mlflow.log_param("n_features",     len(FEATURE_COLS))
        mlflow.set_tag("job", "hyperparameter_tuning")

        study = optuna.create_study(
            direction="maximize",
            sampler=optuna.samplers.TPESampler(seed=RANDOM_SEED),
        )
        study.optimize(
            make_objective(X_train, X_val, y_train, y_val, scale_pos_weight),
            n_trials=args.n_trials,
            show_progress_bar=True,
        )

        best = study.best_trial
        print(f"\nBest trial #{best.number} — val PR-AUC={best.value:.4f}")
        print(f"  params: {best.params}")

        mlflow.log_metric("best_val_pr_auc", round(best.value, 4))
        mlflow.log_param("best_trial",       best.number)
        mlflow.log_params({f"best_{k}": v for k, v in best.params.items()})

        train_best(best.params, X_train, X_val, X_test,
                   y_train, y_val, y_test, scale_pos_weight)


if __name__ == "__main__":
    main()
