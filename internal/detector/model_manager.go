package detector

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

// ModelInfo describes a downloadable model.
type ModelInfo struct {
	Name        string // e.g., "prompt-guard-2-86m"
	Description string
	Size        string // human-readable size
	URL         string // HuggingFace model page
	Files       []ModelFile
}

// ModelFile describes a single file to download.
type ModelFile struct {
	Name string // e.g., "model.onnx"
	URL  string // direct download URL
	Size int64  // bytes
}

// AvailableModels lists the supported models for local LLM detection.
var AvailableModels = map[string]ModelInfo{
	"prompt-guard-2-86m": {
		Name:        "prompt-guard-2-86m",
		Description: "Meta Prompt Guard 2 (86M) — prompt injection & jailbreak classifier. mDeBERTa-base, multilingual.",
		Size:        "~350MB",
		URL:         "https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-86M",
		Files: []ModelFile{
			{Name: "model.onnx", URL: "https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-86M/resolve/main/onnx/model.onnx"},
			{Name: "tokenizer.json", URL: "https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-86M/resolve/main/tokenizer.json"},
			{Name: "config.json", URL: "https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-86M/resolve/main/config.json"},
		},
	},
	"prompt-guard-2-22m": {
		Name:        "prompt-guard-2-22m",
		Description: "Meta Prompt Guard 2 (22M) — lightweight variant. Lower accuracy, faster inference.",
		Size:        "~100MB",
		URL:         "https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-22M",
		Files: []ModelFile{
			{Name: "model.onnx", URL: "https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-22M/resolve/main/onnx/model.onnx"},
			{Name: "tokenizer.json", URL: "https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-22M/resolve/main/tokenizer.json"},
			{Name: "config.json", URL: "https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-22M/resolve/main/config.json"},
		},
	},
}

// DefaultModelDir returns the default directory for model storage.
func DefaultModelDir(modelName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("LOCALAPPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(appData, "TrustGate", "models", modelName)
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "TrustGate", "models", modelName)
	default: // linux
		dataDir := os.Getenv("XDG_DATA_HOME")
		if dataDir == "" {
			dataDir = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(dataDir, "trustgate", "models", modelName)
	}
}

// ModelExists checks if all model files are present.
func ModelExists(modelName string) (bool, string) {
	info, ok := AvailableModels[modelName]
	if !ok {
		return false, ""
	}

	dir := DefaultModelDir(modelName)
	for _, f := range info.Files {
		path := filepath.Join(dir, f.Name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return false, dir
		}
	}
	return true, dir
}

// ModelStatus returns a human-readable status of the model.
func ModelStatus(modelName string) string {
	exists, dir := ModelExists(modelName)
	if exists {
		return fmt.Sprintf("✓ %s installed at %s", modelName, dir)
	}
	info, ok := AvailableModels[modelName]
	if !ok {
		return fmt.Sprintf("✗ unknown model: %s", modelName)
	}
	return fmt.Sprintf("✗ %s not installed (%s) — run: aigw model download %s", modelName, info.Size, modelName)
}

// DownloadModel downloads model files from HuggingFace.
// Supports HF_TOKEN environment variable for gated models (e.g., Meta Llama).
// Downloads to a temp file first, then renames for crash safety.
func DownloadModel(modelName string, progressFn func(file string, pct int)) error {
	info, ok := AvailableModels[modelName]
	if !ok {
		return fmt.Errorf("unknown model: %s (available: prompt-guard-2-86m, prompt-guard-2-22m)", modelName)
	}

	dir := DefaultModelDir(modelName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create model dir: %w", err)
	}

	for _, f := range info.Files {
		destPath := filepath.Join(dir, f.Name)
		if _, err := os.Stat(destPath); err == nil {
			if progressFn != nil {
				progressFn(f.Name, 100)
			}
			continue // already exists
		}

		if err := downloadFile(f.URL, destPath, f.Name, progressFn); err != nil {
			return fmt.Errorf("download %s: %w", f.Name, err)
		}
	}

	return nil
}

// downloadFile downloads a single file from URL to destPath via temp file.
func downloadFile(url, destPath, displayName string, progressFn func(file string, pct int)) error {
	// Create HTTP request with optional HuggingFace token
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// Support HF_TOKEN for gated models
	if token := os.Getenv("HF_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("access denied (HTTP %d) — set HF_TOKEN environment variable. " +
			"Get token at https://huggingface.co/settings/tokens and accept model license at %s",
			resp.StatusCode, url)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	totalSize, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)

	// Download to temp file first (crash safety)
	tmpPath := destPath + ".download"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		tmpFile.Close()
		// Clean up temp file on error
		if _, err := os.Stat(tmpPath); err == nil {
			os.Remove(tmpPath)
		}
	}()

	// Stream download with progress tracking
	var downloaded int64
	buf := make([]byte, 32*1024) // 32KB buffer
	lastPct := -1

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := tmpFile.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("write: %w", writeErr)
			}
			downloaded += int64(n)

			if progressFn != nil && totalSize > 0 {
				pct := int(downloaded * 100 / totalSize)
				if pct != lastPct {
					progressFn(displayName, pct)
					lastPct = pct
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read: %w", readErr)
		}
	}

	tmpFile.Close()

	// Atomic rename: temp → final
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	if progressFn != nil {
		progressFn(displayName, 100)
	}

	return nil
}
