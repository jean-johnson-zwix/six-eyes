"""pytest unit tests for training/features.py — req 8.1"""
import pandas as pd
import pytest
from features import (
    BUZZWORDS,
    CATEGORIES,
    FEATURE_COLS,
    build_features,
    get_label,
    split_data,
)


# ── Helpers ───────────────────────────────────────────────────────────────────

def _make_df(**kwargs):
    """Single-row DataFrame with sensible defaults; override via kwargs."""
    defaults = {
        "authors":      [["Alice", "Bob"]],
        "abstract":     ["This is a test abstract."],
        "title":        ["Test Paper"],
        "categories":   [["cs.LG", "cs.AI"]],
        "submitted_at": ["2024-01-08T12:00:00Z"],  # Monday
        "hype_label":   [1],
    }
    defaults.update(kwargs)
    return pd.DataFrame(defaults)


# ── build_features ────────────────────────────────────────────────────────────

class TestBuildFeatures:
    def test_output_has_exactly_feature_cols(self):
        X = build_features(_make_df())
        assert list(X.columns) == FEATURE_COLS

    def test_num_authors(self):
        df = _make_df(authors=[["Alice", "Bob", "Carol"]])
        assert build_features(df)["num_authors"].iloc[0] == 3

    def test_num_authors_none(self):
        df = _make_df(authors=[None])
        assert build_features(df)["num_authors"].iloc[0] == 0

    def test_abstract_length(self):
        df = _make_df(abstract=["hello"])
        assert build_features(df)["abstract_length"].iloc[0] == 5

    def test_title_length(self):
        df = _make_df(title=["Hi"])
        assert build_features(df)["title_length"].iloc[0] == 2

    def test_num_categories(self):
        df = _make_df(categories=[["cs.LG", "cs.CV"]])
        assert build_features(df)["num_categories"].iloc[0] == 2

    def test_category_multi_hot_single(self):
        df = _make_df(categories=[["cs.LG"]])
        X = build_features(df)
        assert X["cat_cs_LG"].iloc[0] == 1
        assert X["cat_cs_AI"].iloc[0] == 0
        assert X["cat_cs_CV"].iloc[0] == 0
        assert X["cat_cs_CL"].iloc[0] == 0

    def test_category_multi_hot_multiple(self):
        df = _make_df(categories=[["cs.LG", "cs.CL"]])
        X = build_features(df)
        assert X["cat_cs_LG"].iloc[0] == 1
        assert X["cat_cs_CL"].iloc[0] == 1
        assert X["cat_cs_AI"].iloc[0] == 0

    def test_category_none_gives_zeros(self):
        df = _make_df(categories=[None])
        X = build_features(df)
        for cat in CATEGORIES:
            assert X[f"cat_{cat.replace('.', '_')}"].iloc[0] == 0

    def test_buzzword_flag_transformer(self):
        df = _make_df(title=["A Transformer for Vision"])
        assert build_features(df)["buzz_transformer"].iloc[0] == 1

    def test_buzzword_flag_case_insensitive(self):
        df = _make_df(title=["DIFFUSION Models Are Great"])
        assert build_features(df)["buzz_diffusion"].iloc[0] == 1

    def test_no_buzzword_flags_plain_title(self):
        df = _make_df(title=["Plain Title Without Keywords"])
        X = build_features(df)
        for word in BUZZWORDS:
            assert X[f"buzz_{word}"].iloc[0] == 0, f"Expected buzz_{word}=0"

    def test_day_of_week_monday(self):
        # 2024-01-08 is a Monday → Python dayofweek=0
        df = _make_df(submitted_at=["2024-01-08T00:00:00Z"])
        assert build_features(df)["day_of_week"].iloc[0] == 0

    def test_day_of_week_friday(self):
        # 2024-01-12 is a Friday → Python dayofweek=4
        df = _make_df(submitted_at=["2024-01-12T00:00:00Z"])
        assert build_features(df)["day_of_week"].iloc[0] == 4

    def test_month(self):
        df = _make_df(submitted_at=["2024-03-15T00:00:00Z"])
        assert build_features(df)["month"].iloc[0] == 3

    def test_v2_author_signals_present(self):
        df = _make_df()
        df["max_h_index"] = 20
        df["total_prior_papers"] = 50
        df["has_author_enrichment"] = True
        X = build_features(df)
        assert X["max_h_index"].iloc[0] == 20
        assert X["total_prior_papers"].iloc[0] == 50
        assert X["has_author_enrichment"].iloc[0] == 1

    def test_v2_author_signals_absent_gives_zeros(self):
        df = _make_df()  # no max_h_index column
        X = build_features(df)
        assert X["max_h_index"].iloc[0] == 0
        assert X["total_prior_papers"].iloc[0] == 0
        assert X["has_author_enrichment"].iloc[0] == 0

    def test_no_nulls_in_output(self):
        X = build_features(_make_df())
        assert X.isnull().sum().sum() == 0


# ── get_label ─────────────────────────────────────────────────────────────────

class TestGetLabel:
    def test_returns_integer_series(self):
        y = get_label(_make_df(hype_label=[1]))
        assert y.dtype in (int, "int64", "int32")

    def test_label_zero(self):
        y = get_label(_make_df(hype_label=[0]))
        assert y.iloc[0] == 0

    def test_label_one(self):
        y = get_label(_make_df(hype_label=[1]))
        assert y.iloc[0] == 1


# ── split_data ────────────────────────────────────────────────────────────────

class TestSplitData:
    def _make_xy(self, n=100):
        X = pd.DataFrame({"a": range(n)})
        # 20% positive to ensure stratification works
        y = pd.Series([1 if i % 5 == 0 else 0 for i in range(n)], dtype=int)
        return X, y

    def test_split_sizes_sum_to_total(self):
        X, y = self._make_xy(100)
        X_train, X_val, X_test, y_train, y_val, y_test = split_data(X, y)
        assert len(X_train) + len(X_val) + len(X_test) == 100

    def test_split_80_10_10(self):
        X, y = self._make_xy(100)
        X_train, X_val, X_test, *_ = split_data(X, y)
        assert len(X_train) == 80
        assert len(X_val) == 10
        assert len(X_test) == 10

    def test_split_is_deterministic(self):
        X, y = self._make_xy(100)
        X_train1, *_ = split_data(X, y, random_state=42)
        X_train2, *_ = split_data(X, y, random_state=42)
        assert list(X_train1.index) == list(X_train2.index)

    def test_label_arrays_align_with_feature_arrays(self):
        X, y = self._make_xy(100)
        X_train, _, _, y_train, _, _ = split_data(X, y)
        assert len(X_train) == len(y_train)
