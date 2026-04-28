"""
download_raw.py — fetch raw data files needed by transform.py

Downloads:
  1. Papers with Code links dump  — from Hugging Face (paperswithcode/papers-with-code)
  2. ArXiv metadata snapshot      — from Kaggle (Cornell-University/arxiv)
                                    requires KAGGLE_USERNAME + KAGGLE_KEY env vars

Run:
    python download_raw.py              # both datasets
    python download_raw.py --pwc-only   # skip Kaggle (ArXiv already on disk)
    python download_raw.py --arxiv-only # skip HuggingFace

Output directory: raw_data/   (created if absent)
"""

import argparse
import os
from pathlib import Path

RAW_DIR = Path("raw_data")

# Hugging Face dataset repo for Papers with Code
# https://huggingface.co/datasets/pwc-archive/links-between-paper-and-code
PWC_HF_REPO      = "pwc-archive/links-between-paper-and-code"
PWC_HF_FILENAME  = "data/train-00000-of-00001.parquet"   # actual file in the HF repo
PWC_LOCAL_NAME   = "links-between-paper-and-code.parquet" # saved locally as this

# Kaggle dataset slug
ARXIV_KAGGLE_DATASET = "Cornell-University/arxiv"
ARXIV_FILENAME       = "arxiv-metadata-oai-snapshot.json"


def download_pwc():
    from datasets import load_dataset

    dest = RAW_DIR / PWC_LOCAL_NAME
    if dest.exists():
        print(f"[pwc] already exists: {dest} — skipping")
        return

    print(f"[pwc] downloading {PWC_HF_REPO} via datasets ...")
    print(f"[pwc] if this fails with 401, run: huggingface-cli login")
    ds = load_dataset(PWC_HF_REPO, split="train")
    ds.to_parquet(str(dest))
    print(f"[pwc] saved to {dest} ({len(ds):,} rows)")


def download_arxiv():
    # kaggle API requires KAGGLE_USERNAME and KAGGLE_KEY env vars,
    # or a ~/.kaggle/kaggle.json credentials file.
    try:
        import kaggle  # noqa: F401
    except ImportError:
        print("[arxiv] ERROR: kaggle package not installed. Run: pip install kaggle")
        return

    dest = RAW_DIR / ARXIV_FILENAME
    if dest.exists():
        print(f"[arxiv] already exists: {dest} — skipping")
        return

    print(f"[arxiv] downloading {ARXIV_KAGGLE_DATASET} (~3.5 GB, this will take a while) ...")
    import kaggle
    kaggle.api.authenticate()
    kaggle.api.dataset_download_files(
        ARXIV_KAGGLE_DATASET,
        path=str(RAW_DIR),
        unzip=True,
        quiet=False,
    )
    print(f"[arxiv] saved to {RAW_DIR / ARXIV_FILENAME}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--pwc-only",   action="store_true")
    parser.add_argument("--arxiv-only", action="store_true")
    args = parser.parse_args()

    RAW_DIR.mkdir(exist_ok=True)

    if not args.arxiv_only:
        download_pwc()
    if not args.pwc_only:
        download_arxiv()


if __name__ == "__main__":
    main()
