"""pytest unit tests for model inference utilities — req 8.2

Tests compute_metrics() and find_threshold() from train.py using stub models
so no MLflow server or parquet file is required.
"""
import numpy as np
import pandas as pd
import pytest
from train import compute_metrics, find_threshold


# ── Stub model ────────────────────────────────────────────────────────────────

class _ConstantModel:
    """Returns a fixed probability for every sample."""
    def __init__(self, proba: float):
        self._p = proba

    def predict_proba(self, X):
        n = len(X)
        return np.column_stack([np.full(n, 1 - self._p), np.full(n, self._p)])


class _PerfectModel:
    """Returns proba=1 for the first n_pos rows, 0 for the rest."""
    def __init__(self, n_pos: int):
        self._n = n_pos

    def predict_proba(self, X):
        p = np.zeros(len(X))
        p[: self._n] = 1.0
        return np.column_stack([1 - p, p])


# ── Fixtures ──────────────────────────────────────────────────────────────────

@pytest.fixture
def imbalanced_data():
    n_pos, n_neg = 10, 90
    y = pd.Series([1] * n_pos + [0] * n_neg)
    X = pd.DataFrame({"f": range(len(y))})
    return X, y, n_pos


# ── compute_metrics ───────────────────────────────────────────────────────────

class TestComputeMetrics:
    def test_returns_correct_keys(self, imbalanced_data):
        X, y, _ = imbalanced_data
        m = compute_metrics(_ConstantModel(0.5), X, y, "val")
        assert set(m.keys()) == {"val_roc_auc", "val_pr_auc", "val_f1"}

    def test_perfect_model_roc_auc_is_one(self, imbalanced_data):
        X, y, n_pos = imbalanced_data
        m = compute_metrics(_PerfectModel(n_pos), X, y, "test")
        assert m["test_roc_auc"] == 1.0

    def test_perfect_model_pr_auc_is_one(self, imbalanced_data):
        X, y, n_pos = imbalanced_data
        m = compute_metrics(_PerfectModel(n_pos), X, y, "test")
        assert m["test_pr_auc"] == 1.0

    def test_values_rounded_to_4_decimal_places(self, imbalanced_data):
        X, y, _ = imbalanced_data
        m = compute_metrics(_ConstantModel(0.5), X, y, "val")
        for v in m.values():
            assert v == round(v, 4), f"{v} is not rounded to 4 dp"

    def test_high_threshold_gives_zero_f1(self, imbalanced_data):
        # Model always predicts 0.7; at threshold=0.8 nothing is predicted positive → F1=0
        X, y, _ = imbalanced_data
        m = compute_metrics(_ConstantModel(0.7), X, y, "v", threshold=0.8)
        assert m["v_f1"] == 0.0

    def test_prefix_applied_to_all_keys(self, imbalanced_data):
        X, y, _ = imbalanced_data
        m = compute_metrics(_ConstantModel(0.5), X, y, "train")
        assert all(k.startswith("train_") for k in m)


# ── find_threshold ────────────────────────────────────────────────────────────

class TestFindThreshold:
    def _val_data(self):
        y = pd.Series([1, 0, 1, 0, 0, 1, 0, 0, 0, 0])
        X = pd.DataFrame({"f": range(len(y))})
        return X, y

    def test_returns_float_in_unit_interval(self):
        X, y = self._val_data()

        class _AlternatingModel:
            def predict_proba(self, X):
                p = np.array([0.8 if i % 2 == 0 else 0.2 for i in range(len(X))])
                return np.column_stack([1 - p, p])

        t = find_threshold(_AlternatingModel(), X, y)
        assert 0.0 <= t <= 1.0

    def test_is_deterministic(self):
        X, y = self._val_data()
        model = _ConstantModel(0.6)
        assert find_threshold(model, X, y) == find_threshold(model, X, y)

    def test_returns_rounded_float(self):
        X, y = self._val_data()
        t = find_threshold(_ConstantModel(0.5), X, y)
        assert t == round(t, 4)
