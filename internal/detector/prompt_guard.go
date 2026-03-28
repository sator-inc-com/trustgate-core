//go:build !nollm

package detector

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/shota3506/onnxruntime-purego/onnxruntime"
	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	"github.com/trustgate/trustgate/internal/config"
)

// PromptGuardDetector implements LLMDetector using Meta's Prompt Guard 2.
//
// Architecture:
//   - Model: Prompt Guard 2 86M (mDeBERTa-base, 86M params)
//   - Format: ONNX quantized (from gravitee-io conversion)
//   - Runtime: ONNX Runtime shared library via purego (no CGO)
//   - Memory: ~200MB (model + runtime)
//   - Latency: 1-5ms per inference (CPU only, no GPU needed)
//   - Classification: benign / malicious (2-class binary)
type PromptGuardDetector struct {
	mu        sync.RWMutex
	cfg       config.LLMDetectorConfig
	ready     bool
	runtime   *ort.Runtime
	env       *ort.Env
	session   *ort.Session
	tokenizer *tokenizer.Tokenizer
	labels    []string // ["benign", "malicious"]
	maxLen    int
}

// PromptGuardResult represents the classification output.
type PromptGuardResult struct {
	Label      string     // "benign" or "malicious"
	Confidence float64    // 0.0-1.0
	Scores     [2]float64 // [benign, malicious]
}

// NewPromptGuardDetector creates a new Prompt Guard 2 detector.
func NewPromptGuardDetector(cfg config.LLMDetectorConfig) *PromptGuardDetector {
	return &PromptGuardDetector{
		cfg:    cfg,
		labels: []string{"benign", "malicious"},
		maxLen: 512,
	}
}

func (d *PromptGuardDetector) Name() string { return "prompt_guard" }

// Ready returns true if the model is loaded.
func (d *PromptGuardDetector) Ready() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.ready
}

// LoadModel loads the ONNX model and tokenizer.
func (d *PromptGuardDetector) LoadModel() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Try embedded model first
	if hasEmbeddedModel() {
		return d.loadFromEmbedded()
	}

	// Load from disk
	modelDir := d.resolveModelDir()
	modelPath := filepath.Join(modelDir, "model.quant.onnx")
	tokenizerPath := filepath.Join(modelDir, "tokenizer.json")

	// Also check for non-quantized model.onnx as fallback
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		fallback := filepath.Join(modelDir, "model.onnx")
		if _, err2 := os.Stat(fallback); err2 == nil {
			modelPath = fallback
		} else {
			return fmt.Errorf("model not found at %s — run 'aigw model download %s' first", modelPath, d.modelName())
		}
	}
	if _, err := os.Stat(tokenizerPath); os.IsNotExist(err) {
		return fmt.Errorf("tokenizer not found at %s — run 'aigw model download %s' first", tokenizerPath, d.modelName())
	}

	return d.loadFromFiles(modelPath, tokenizerPath)
}

func (d *PromptGuardDetector) loadFromFiles(modelPath, tokenizerPath string) error {
	// Initialize ONNX Runtime
	libPath := findONNXRuntimeLib()
	rt, err := ort.NewRuntime(libPath, 23)
	if err != nil {
		return fmt.Errorf("init ONNX Runtime from %s: %w", libPath, err)
	}
	d.runtime = rt

	// Create environment
	env, err := rt.NewEnv("trustgate", ort.LoggingLevelWarning)
	if err != nil {
		return fmt.Errorf("create env: %w", err)
	}
	d.env = env

	// Create session with optimized settings
	sessOpts := &ort.SessionOptions{
		IntraOpNumThreads: 4, // balance between speed and CPU usage
	}
	session, err := rt.NewSession(env, modelPath, sessOpts)
	if err != nil {
		return fmt.Errorf("load model %s: %w", modelPath, err)
	}
	d.session = session

	// Load tokenizer
	tk, err := pretrained.FromFile(tokenizerPath)
	if err != nil {
		return fmt.Errorf("load tokenizer %s: %w", tokenizerPath, err)
	}
	d.tokenizer = tk

	d.ready = true
	return nil
}

func (d *PromptGuardDetector) loadFromEmbedded() error {
	tmpDir, err := os.MkdirTemp("", "trustgate-model-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}

	modelPath := filepath.Join(tmpDir, "model.onnx")
	tokenizerPath := filepath.Join(tmpDir, "tokenizer.json")

	if err := os.WriteFile(modelPath, getEmbeddedModel(), 0644); err != nil {
		return fmt.Errorf("write embedded model: %w", err)
	}
	if err := os.WriteFile(tokenizerPath, getEmbeddedTokenizer(), 0644); err != nil {
		return fmt.Errorf("write embedded tokenizer: %w", err)
	}

	return d.loadFromFiles(modelPath, tokenizerPath)
}

// Classify performs prompt injection/jailbreak classification.
func (d *PromptGuardDetector) Classify(input string) ([]Finding, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if !d.ready {
		return nil, fmt.Errorf("model not loaded")
	}

	result, err := d.infer(input)
	if err != nil {
		return nil, fmt.Errorf("inference error: %w", err)
	}

	var findings []Finding

	if result.Label == "malicious" && result.Confidence > 0.5 {
		findings = append(findings, Finding{
			Detector:    "llm_injection",
			Category:    "injection", // map "malicious" to "injection" for Stage 1 compatibility
			Severity:    classifySeverity(result.Confidence),
			Confidence:  result.Confidence,
			Description: fmt.Sprintf("Prompt Guard 2: malicious input detected (%.1f%%)", result.Confidence*100),
			Matched:     truncate(input, 100),
			Position:    0,
			Length:      len(input),
		})
	}

	return findings, nil
}

// Close releases model resources.
func (d *PromptGuardDetector) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ready = false
	if d.session != nil {
		d.session.Close()
		d.session = nil
	}
	if d.env != nil {
		d.env.Close()
		d.env = nil
	}
	if d.runtime != nil {
		d.runtime.Close()
		d.runtime = nil
	}
	d.tokenizer = nil
	return nil
}

// infer runs the full pipeline: tokenize → tensors → ONNX → softmax → classify
func (d *PromptGuardDetector) infer(input string) (*PromptGuardResult, error) {
	// 1. Tokenize
	encoding, err := d.tokenizer.EncodeSingle(input, true)
	if err != nil {
		return nil, fmt.Errorf("tokenize: %w", err)
	}

	ids := encoding.GetIds()
	mask := encoding.GetAttentionMask()

	// Strip padding: the tokenizer may pad to max_length (e.g., 512),
	// but ONNX inference cost scales with sequence length.
	// Only keep tokens where attention_mask == 1.
	realLen := 0
	for _, m := range mask {
		if m == 1 {
			realLen++
		}
	}
	if realLen == 0 {
		realLen = len(ids) // fallback: no mask info, use all tokens
	}

	// Truncate to max length
	if realLen > d.maxLen {
		realLen = d.maxLen
	}

	seqLen := int64(realLen)

	// 2. Create input tensors (int64) — unpadded
	inputIDs := make([]int64, seqLen)
	attentionMask := make([]int64, seqLen)
	for i := 0; i < realLen; i++ {
		inputIDs[i] = int64(ids[i])
		attentionMask[i] = 1 // all real tokens have mask=1
	}

	inputShape := []int64{1, seqLen}

	inputIDsTensor, err := ort.NewTensorValue(d.runtime, inputIDs, inputShape)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer inputIDsTensor.Close()

	attMaskTensor, err := ort.NewTensorValue(d.runtime, attentionMask, inputShape)
	if err != nil {
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer attMaskTensor.Close()

	// 3. Run inference
	inputs := map[string]*ort.Value{
		"input_ids":      inputIDsTensor,
		"attention_mask": attMaskTensor,
	}

	outputs, err := d.session.Run(context.Background(), inputs)
	if err != nil {
		return nil, fmt.Errorf("run inference: %w", err)
	}

	// 4. Get logits from first output
	var logits []float32
	for _, v := range outputs {
		logits, _, err = ort.GetTensorData[float32](v)
		v.Close()
		if err != nil {
			return nil, fmt.Errorf("get output tensor data: %w", err)
		}
		break // only need first output
	}

	if len(logits) < 2 {
		return nil, fmt.Errorf("unexpected output size: %d (expected 2)", len(logits))
	}

	// 5. Softmax → probabilities
	scores := softmax(logits)

	// 6. Find best label
	bestIdx := 0
	bestScore := scores[0]
	for i := 1; i < len(scores); i++ {
		if scores[i] > bestScore {
			bestScore = scores[i]
			bestIdx = i
		}
	}

	return &PromptGuardResult{
		Label:      d.labels[bestIdx],
		Confidence: bestScore,
		Scores:     scores,
	}, nil
}

// softmax converts logits to probabilities (2-class: benign/malicious).
func softmax(logits []float32) [2]float64 {
	var result [2]float64
	n := len(logits)
	if n > 2 {
		n = 2
	}

	maxVal := float64(logits[0])
	for i := 1; i < n; i++ {
		if float64(logits[i]) > maxVal {
			maxVal = float64(logits[i])
		}
	}

	var sum float64
	for i := 0; i < n; i++ {
		result[i] = math.Exp(float64(logits[i]) - maxVal)
		sum += result[i]
	}
	for i := 0; i < n; i++ {
		result[i] /= sum
	}

	return result
}

func (d *PromptGuardDetector) resolveModelDir() string {
	if d.cfg.ModelDir != "" {
		return d.cfg.ModelDir
	}
	return DefaultModelDir(d.modelName())
}

func (d *PromptGuardDetector) modelName() string {
	model := d.cfg.Model
	if model == "" {
		model = "prompt-guard-2-22m"
	}
	return model
}

// findONNXRuntimeLib searches for the ONNX Runtime shared library.
func findONNXRuntimeLib() string {
	candidates := onnxRuntimeLibPaths()
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return onnxRuntimeLibName()
}

func classifySeverity(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "critical"
	case confidence >= 0.7:
		return "high"
	case confidence >= 0.5:
		return "medium"
	default:
		return "low"
	}
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

