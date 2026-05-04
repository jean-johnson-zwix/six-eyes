"""
export_model.py — Export the champion XGBoost model to Go-compatible artifacts.

Outputs written to api/model/ (relative to repo root):
  xgb_model.json   — XGBoost native JSON, read directly by the Go API
  model_meta.json  — feature list, optimal threshold, version metadata

Pass --upload-hf to push both files to a public HuggingFace Hub model repo.
The Go API then downloads them at startup via MODEL_BASE_URL (no credentials).

Usage:
    cd training
    python export_model.py
    python export_model.py --upload-hf --hf-repo <user>/<repo>

Env vars:
    MLFLOW_TRACKING_URI       DagShub MLflow URL (required)
    MLFLOW_TRACKING_USERNAME  DagShub username
    MLFLOW_TRACKING_PASSWORD  DagShub token
    HF_TOKEN                  HuggingFace write token (required with --upload-hf)
    HF_REPO                   HuggingFace repo id, e.g. jajoh151/six-eyes-model
"""

from __future__ import annotations

import argparse
import json
import os
import sys

try:
    from dotenv import load_dotenv
    load_dotenv()
except ImportError:
    pass  # dotenv not installed; rely on env vars being set in the shell

import mlflow
import mlflow.xgboost
from mlflow import MlflowClient

# Allow importing features.py from this directory
sys.path.insert(0, os.path.dirname(__file__))
from features import FEATURE_COLS

MODEL_NAME = "six-eyes-xgb"
DEFAULT_OUT = os.path.join(os.path.dirname(__file__), "..", "api", "model")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--alias",     default="champion",                   help="MLflow model alias to export")
    parser.add_argument("--out-dir",   default=DEFAULT_OUT,                  help="Output directory for artifacts")
    parser.add_argument("--upload-hf", action="store_true",                  help="Upload artifacts to HuggingFace Hub")
    parser.add_argument("--hf-repo",   default=os.getenv("HF_REPO", ""),     help="HF repo id, e.g. jajoh151/six-eyes-model")
    args = parser.parse_args()

    tracking_uri = os.getenv("MLFLOW_TRACKING_URI")
    if not tracking_uri:
        sys.exit("MLFLOW_TRACKING_URI is required")

    # DagShub auth — set MLFLOW_TRACKING_USERNAME + MLFLOW_TRACKING_PASSWORD in .env
    username = os.getenv("MLFLOW_TRACKING_USERNAME")
    password = os.getenv("MLFLOW_TRACKING_PASSWORD")
    if username:
        os.environ["MLFLOW_TRACKING_USERNAME"] = username
    if password:
        os.environ["MLFLOW_TRACKING_PASSWORD"] = password

    mlflow.set_tracking_uri(tracking_uri)
    client = MlflowClient()

    print(f"Loading {MODEL_NAME}@{args.alias} ...")
    model_uri = f"models:/{MODEL_NAME}@{args.alias}"
    booster = mlflow.xgboost.load_model(model_uri)

    os.makedirs(args.out_dir, exist_ok=True)

    # --- Export XGBoost model to JSON (leaves-compatible) ---
    model_path = os.path.join(args.out_dir, "xgb_model.json")
    booster.save_model(model_path)
    print(f"  model  → {model_path}")

    # --- Pull threshold + version from MLflow run ---
    mv = client.get_model_version_by_alias(MODEL_NAME, args.alias)
    run = client.get_run(mv.run_id)
    threshold = float(run.data.params.get("optimal_threshold", 0.8837))

    meta = {
        "model_name":      MODEL_NAME,
        "alias":           args.alias,
        "version":         mv.version,
        "run_id":          mv.run_id,
        "feature_cols":    FEATURE_COLS,
        "num_features":    len(FEATURE_COLS),
        "threshold":       threshold,
        "feature_version": run.data.tags.get("feature_version", "v3"),
    }

    meta_path = os.path.join(args.out_dir, "model_meta.json")
    with open(meta_path, "w") as f:
        json.dump(meta, f, indent=2)
    print(f"  meta   → {meta_path}")
    print(f"  threshold={threshold}  features={len(FEATURE_COLS)}")

    # --- Log artifacts back to MLflow for traceability ---
    mlflow.set_experiment("six-eyes-v1")
    with mlflow.start_run(run_name="model-export-go"):
        mlflow.log_artifact(model_path, artifact_path="go_artifacts")
        mlflow.log_artifact(meta_path,  artifact_path="go_artifacts")
        mlflow.log_param("exported_model",   f"{MODEL_NAME}@{args.alias}")
        mlflow.log_param("exported_version", mv.version)
        mlflow.set_tag("job", "model_export_for_go")
    print("  artifacts logged to MLflow ✓")

    # --- Upload to HuggingFace Hub (optional) ---
    if args.upload_hf:
        _upload_to_hf(model_path, meta_path, args.hf_repo)
    else:
        print("\nTip: re-run with --upload-hf --hf-repo <user>/<repo> to push to HuggingFace Hub.")


def _upload_to_hf(model_path: str, meta_path: str, hf_repo: str) -> None:
    try:
        from huggingface_hub import HfApi
    except ImportError:
        print("huggingface_hub not installed — run: pip install huggingface_hub")
        return

    token = os.getenv("HF_TOKEN")
    if not token:
        print("HF_TOKEN not set — skipping HuggingFace upload")
        return
    if not hf_repo:
        print("--hf-repo not specified — skipping HuggingFace upload")
        return

    api = HfApi(token=token)

    # Create the repo if it doesn't exist yet
    api.create_repo(repo_id=hf_repo, repo_type="model", exist_ok=True, private=False)

    for path in (model_path, meta_path):
        filename = os.path.basename(path)
        api.upload_file(
            path_or_fileobj=path,
            path_in_repo=filename,
            repo_id=hf_repo,
            repo_type="model",
        )
        print(f"  uploaded → hf://{hf_repo}/{filename}")

    base_url = f"https://huggingface.co/{hf_repo}/resolve/main/"
    print(f"\nSet this env var on Render:")
    print(f"  MODEL_BASE_URL={base_url}")


if __name__ == "__main__":
    main()
