// Package inference loads a trained XGBoost model from the JSON format written
// by model.save_model("xgb_model.json") and scores Arxiv papers entirely
// within Go — no Python runtime, no CGO, no external ML dependencies.
package inference

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// ── XGBoost JSON format ───────────────────────────────────────────────────────

type xgbModelJSON struct {
	Learner struct {
		LearnerModelParam struct {
			// Stored as a JSON string (e.g. "5.000205E-1"), not a number
			BaseScore string `json:"base_score"`
		} `json:"learner_model_param"`
		GradientBooster struct {
			Model struct {
				Trees []xgbTreeJSON `json:"trees"`
			} `json:"model"`
		} `json:"gradient_booster"`
	} `json:"learner"`
}

type xgbTreeJSON struct {
	LeftChildren    []int32   `json:"left_children"`
	RightChildren   []int32   `json:"right_children"`
	SplitIndices    []int32   `json:"split_indices"`
	SplitConditions []float32 `json:"split_conditions"`
	BaseWeights     []float32 `json:"base_weights"`
}

// ── Model metadata ────────────────────────────────────────────────────────────

// Meta mirrors the model_meta.json written by training/export_model.py.
type Meta struct {
	ModelName      string   `json:"model_name"`
	Alias          string   `json:"alias"`
	Version        string   `json:"version"`
	RunID          string   `json:"run_id"`
	FeatureCols    []string `json:"feature_cols"`
	NumFeatures    int      `json:"num_features"`
	Threshold      float64  `json:"threshold"`
	FeatureVersion string   `json:"feature_version"`
}

// ── Model ─────────────────────────────────────────────────────────────────────

// Model holds the parsed XGBoost trees and their metadata.
type Model struct {
	trees     []xgbTreeJSON
	baseLogit float64 // logit(base_score), added to raw margin before sigmoid
	Meta      Meta
}

// Load reads xgb_model.json and model_meta.json from dir.
func Load(dir string) (*Model, error) {
	// --- Metadata ---
	metaData, err := os.ReadFile(dir + "/model_meta.json")
	if err != nil {
		return nil, fmt.Errorf("read model_meta.json: %w", err)
	}
	var meta Meta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return nil, fmt.Errorf("parse model_meta.json: %w", err)
	}

	// --- XGBoost model ---
	modelData, err := os.ReadFile(dir + "/xgb_model.json")
	if err != nil {
		return nil, fmt.Errorf("read xgb_model.json: %w", err)
	}
	var xgb xgbModelJSON
	if err := json.Unmarshal(modelData, &xgb); err != nil {
		return nil, fmt.Errorf("parse xgb_model.json: %w", err)
	}

	bsStr := strings.Trim(xgb.Learner.LearnerModelParam.BaseScore, "[] \t")
	if bsStr == "" {
		return nil, fmt.Errorf("xgb_model.json: missing base_score")
	}
	bs, err := strconv.ParseFloat(bsStr, 64)
	if err != nil {
		return nil, fmt.Errorf("xgb_model.json: invalid base_score %q: %w", bsStr, err)
	}
	// base_score is stored as probability; convert to logit for raw margin sum.
	// For default 0.5, logit ≈ 0 and has negligible effect.
	baseLogit := math.Log(bs / (1.0 - bs))

	return &Model{
		trees:     xgb.Learner.GradientBooster.Model.Trees,
		baseLogit: baseLogit,
		Meta:      meta,
	}, nil
}

// ── Prediction ────────────────────────────────────────────────────────────────

// PaperInput holds the raw paper fields needed to compute the feature vector.
type PaperInput struct {
	Authors          []string
	Abstract         string
	Title            string
	Categories       []string
	SubmittedAt      time.Time
	MaxHIndex        *int
	TotalPriorPapers *int
}

// Predict returns the hype probability for a paper (0–1).
func (m *Model) Predict(p PaperInput) float64 {
	fvals := buildFeatures(p)
	raw := m.baseLogit
	for i := range m.trees {
		raw += walkTree(&m.trees[i], fvals)
	}
	return sigmoid(raw)
}

// Tier returns the display tier for a given score.
//   - "hype"   — score >= model threshold (predicted positive)
//   - "likely" — score >= threshold/2
//   - "low"    — below that
func (m *Model) Tier(score float64) string {
	t := m.Meta.Threshold
	switch {
	case score >= t:
		return "hype"
	case score >= t/2:
		return "likely"
	default:
		return "low"
	}
}

// walkTree descends one tree from root to leaf and returns the leaf weight.
func walkTree(tree *xgbTreeJSON, fvals []float64) float64 {
	node := int32(0)
	for tree.LeftChildren[node] != -1 {
		feat := int(tree.SplitIndices[node])
		threshold := float64(tree.SplitConditions[node])
		if feat < len(fvals) && fvals[feat] < threshold {
			node = tree.LeftChildren[node]
		} else {
			node = tree.RightChildren[node]
		}
	}
	return float64(tree.BaseWeights[node])
}

func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}

// ── Feature engineering ───────────────────────────────────────────────────────
// Must exactly match FEATURE_COLS order in training/features.py.
// V1 metadata (6) + category flags (4) + buzzword flags (14) + author signals (3) = 27

var featureCategories = []string{"cs.LG", "cs.AI", "cs.CV", "cs.CL"}

var featureBuzzwords = []string{
	"transformer", "diffusion", "agent", "multimodal", "rlhf",
	"llm", "mamba", "vision", "reasoning", "inference",
	"quantization", "scaling", "foundation", "eval",
}

func buildFeatures(p PaperInput) []float64 {
	f := make([]float64, 27)

	// --- V1 metadata (indices 0–5) ---
	f[0] = float64(len(p.Authors))
	f[1] = float64(len(p.Abstract))
	f[2] = float64(len(p.Title))
	// Python dayofweek: Mon=0…Sun=6. Go Weekday: Sun=0, Mon=1…Sat=6.
	f[3] = float64((int(p.SubmittedAt.Weekday()) + 6) % 7)
	f[4] = float64(p.SubmittedAt.Month())
	f[5] = float64(len(p.Categories))

	// --- Category multi-hot (indices 6–9) ---
	catSet := make(map[string]bool, len(p.Categories))
	for _, c := range p.Categories {
		catSet[c] = true
	}
	for i, cat := range featureCategories {
		if catSet[cat] {
			f[6+i] = 1
		}
	}

	// --- Title buzzword flags (indices 10–23) ---
	titleLower := strings.ToLower(p.Title)
	for i, word := range featureBuzzwords {
		if strings.Contains(titleLower, word) {
			f[10+i] = 1
		}
	}

	// --- V2 author signals (indices 24–26) ---
	if p.MaxHIndex != nil {
		f[24] = float64(*p.MaxHIndex)
		f[26] = 1 // has_author_enrichment
	}
	if p.TotalPriorPapers != nil {
		f[25] = float64(*p.TotalPriorPapers)
	}

	return f
}
