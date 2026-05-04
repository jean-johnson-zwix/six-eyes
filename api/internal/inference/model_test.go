package inference

import (
	"math"
	"testing"
	"time"
)

// ── sigmoid ───────────────────────────────────────────────────────────────────

func TestSigmoid_ZeroIsHalf(t *testing.T) {
	if got := sigmoid(0); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("sigmoid(0) = %v, want 0.5", got)
	}
}

func TestSigmoid_LargePositiveApproachesOne(t *testing.T) {
	if got := sigmoid(100); got <= 0.999 {
		t.Errorf("sigmoid(100) = %v, want ~1.0", got)
	}
}

func TestSigmoid_LargeNegativeApproachesZero(t *testing.T) {
	if got := sigmoid(-100); got >= 0.001 {
		t.Errorf("sigmoid(-100) = %v, want ~0.0", got)
	}
}

// ── Tier ─────────────────────────────────────────────────────────────────────

func TestTier_AboveThresholdIsHype(t *testing.T) {
	m := &Model{Meta: Meta{Threshold: 0.5}}
	if got := m.Tier(0.8); got != "hype" {
		t.Errorf("Tier(0.8) with threshold 0.5 = %q, want \"hype\"", got)
	}
}

func TestTier_AtThresholdIsHype(t *testing.T) {
	m := &Model{Meta: Meta{Threshold: 0.5}}
	if got := m.Tier(0.5); got != "hype" {
		t.Errorf("Tier(0.5) with threshold 0.5 = %q, want \"hype\"", got)
	}
}

func TestTier_BetweenHalfThresholdAndThresholdIsLikely(t *testing.T) {
	m := &Model{Meta: Meta{Threshold: 0.8}}
	if got := m.Tier(0.5); got != "likely" {
		t.Errorf("Tier(0.5) with threshold 0.8 = %q, want \"likely\"", got)
	}
}

func TestTier_BelowHalfThresholdIsLow(t *testing.T) {
	m := &Model{Meta: Meta{Threshold: 0.8}}
	if got := m.Tier(0.1); got != "low" {
		t.Errorf("Tier(0.1) with threshold 0.8 = %q, want \"low\"", got)
	}
}

// ── buildFeatures ─────────────────────────────────────────────────────────────

func TestBuildFeatures_Length(t *testing.T) {
	p := PaperInput{
		Authors:     []string{"Alice"},
		Abstract:    "test abstract",
		Title:       "Test Paper",
		Categories:  []string{"cs.LG"},
		SubmittedAt: time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC),
	}
	fvals := buildFeatures(p)
	if len(fvals) != 27 {
		t.Errorf("buildFeatures returned %d features, want 27", len(fvals))
	}
}

func TestBuildFeatures_NumAuthors(t *testing.T) {
	p := PaperInput{Authors: []string{"A", "B", "C"}}
	fvals := buildFeatures(p)
	if fvals[0] != 3 {
		t.Errorf("f[0] (num_authors) = %v, want 3", fvals[0])
	}
}

func TestBuildFeatures_AbstractLength(t *testing.T) {
	p := PaperInput{Abstract: "hello"}
	fvals := buildFeatures(p)
	if fvals[1] != 5 {
		t.Errorf("f[1] (abstract_length) = %v, want 5", fvals[1])
	}
}

func TestBuildFeatures_CategoryFlags(t *testing.T) {
	p := PaperInput{Categories: []string{"cs.LG", "cs.CL"}}
	fvals := buildFeatures(p)
	// featureCategories order: cs.LG(6), cs.AI(7), cs.CV(8), cs.CL(9)
	if fvals[6] != 1 {
		t.Errorf("f[6] (cs.LG) = %v, want 1", fvals[6])
	}
	if fvals[7] != 0 {
		t.Errorf("f[7] (cs.AI) = %v, want 0", fvals[7])
	}
	if fvals[8] != 0 {
		t.Errorf("f[8] (cs.CV) = %v, want 0", fvals[8])
	}
	if fvals[9] != 1 {
		t.Errorf("f[9] (cs.CL) = %v, want 1", fvals[9])
	}
}

func TestBuildFeatures_BuzzwordTransformer(t *testing.T) {
	// "transformer" is featureBuzzwords[0] → feature index 10
	p := PaperInput{Title: "A Transformer Model"}
	fvals := buildFeatures(p)
	if fvals[10] != 1 {
		t.Errorf("f[10] (buzz_transformer) = %v, want 1", fvals[10])
	}
}

func TestBuildFeatures_BuzzwordCaseInsensitive(t *testing.T) {
	p := PaperInput{Title: "DIFFUSION Models"}
	fvals := buildFeatures(p)
	// "diffusion" is featureBuzzwords[1] → feature index 11
	if fvals[11] != 1 {
		t.Errorf("f[11] (buzz_diffusion) = %v, want 1", fvals[11])
	}
}

func TestBuildFeatures_DayOfWeek_Monday(t *testing.T) {
	// 2024-01-08 is a Monday; Python dayofweek=0
	p := PaperInput{SubmittedAt: time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)}
	fvals := buildFeatures(p)
	if fvals[3] != 0 {
		t.Errorf("f[3] (day_of_week Monday) = %v, want 0", fvals[3])
	}
}

func TestBuildFeatures_DayOfWeek_Sunday(t *testing.T) {
	// 2024-01-14 is a Sunday; Python dayofweek=6
	p := PaperInput{SubmittedAt: time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC)}
	fvals := buildFeatures(p)
	if fvals[3] != 6 {
		t.Errorf("f[3] (day_of_week Sunday) = %v, want 6", fvals[3])
	}
}

func TestBuildFeatures_AuthorSignals_Present(t *testing.T) {
	h := 30
	pp := 100
	p := PaperInput{MaxHIndex: &h, TotalPriorPapers: &pp}
	fvals := buildFeatures(p)
	if fvals[24] != 30 {
		t.Errorf("f[24] (max_h_index) = %v, want 30", fvals[24])
	}
	if fvals[25] != 100 {
		t.Errorf("f[25] (total_prior_papers) = %v, want 100", fvals[25])
	}
	if fvals[26] != 1 {
		t.Errorf("f[26] (has_author_enrichment) = %v, want 1", fvals[26])
	}
}

func TestBuildFeatures_AuthorSignals_Absent(t *testing.T) {
	p := PaperInput{} // nil MaxHIndex and TotalPriorPapers
	fvals := buildFeatures(p)
	if fvals[24] != 0 || fvals[25] != 0 || fvals[26] != 0 {
		t.Errorf("author signals without enrichment should all be 0, got [%v, %v, %v]",
			fvals[24], fvals[25], fvals[26])
	}
}

// ── walkTree ──────────────────────────────────────────────────────────────────

func TestWalkTree_SingleLeafNode(t *testing.T) {
	// A tree whose root is already a leaf (LeftChildren[0] == -1)
	tree := &xgbTreeJSON{
		LeftChildren:    []int32{-1},
		RightChildren:   []int32{-1},
		SplitIndices:    []int32{0},
		SplitConditions: []float32{0},
		BaseWeights:     []float32{0.5},
	}
	if got := walkTree(tree, []float64{}); math.Abs(got-0.5) > 1e-6 {
		t.Errorf("walkTree single leaf = %v, want 0.5", got)
	}
}

func TestWalkTree_LeftBranch(t *testing.T) {
	// Root splits feature 0 at 5.0; left leaf weight=0.1, right=0.9
	tree := &xgbTreeJSON{
		LeftChildren:    []int32{1, -1, -1},
		RightChildren:   []int32{2, -1, -1},
		SplitIndices:    []int32{0, 0, 0},
		SplitConditions: []float32{5.0, 0, 0},
		BaseWeights:     []float32{0, 0.1, 0.9},
	}
	// fvals[0]=3 < 5.0 → takes left branch → weight 0.1
	if got := walkTree(tree, []float64{3.0}); math.Abs(got-0.1) > 1e-6 {
		t.Errorf("walkTree left branch = %v, want 0.1", got)
	}
}

func TestWalkTree_RightBranch(t *testing.T) {
	tree := &xgbTreeJSON{
		LeftChildren:    []int32{1, -1, -1},
		RightChildren:   []int32{2, -1, -1},
		SplitIndices:    []int32{0, 0, 0},
		SplitConditions: []float32{5.0, 0, 0},
		BaseWeights:     []float32{0, 0.1, 0.9},
	}
	// fvals[0]=7 >= 5.0 → takes right branch → weight 0.9
	if got := walkTree(tree, []float64{7.0}); math.Abs(got-0.9) > 1e-6 {
		t.Errorf("walkTree right branch = %v, want 0.9", got)
	}
}
