// Package modelstore downloads model artifacts from a public URL (e.g. HuggingFace Hub)
// at startup if they are not already present locally.
package modelstore

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

var modelFiles = []string{"xgb_model.json", "model_meta.json"}

// EnsureLocal checks whether the model files exist in dir.
// If either is missing, downloads both from baseURL+filename.
//
// For HuggingFace Hub set baseURL to:
//
//	https://huggingface.co/<user>/<repo>/resolve/main/
func EnsureLocal(dir, baseURL string) error {
	if allPresent(dir) {
		log.Printf("Model artifacts present in %s — skipping download", dir)
		return nil
	}

	baseURL = strings.TrimRight(baseURL, "/") + "/"
	log.Printf("Downloading model artifacts from %s ...", baseURL)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create model dir: %w", err)
	}

	for _, f := range modelFiles {
		url := baseURL + f
		dest := dir + "/" + f
		if err := downloadFile(url, dest); err != nil {
			return err
		}
		log.Printf("  %s → %s", url, dest)
	}
	return nil
}

func allPresent(dir string) bool {
	for _, f := range modelFiles {
		if _, err := os.Stat(dir + "/" + f); os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url) //nolint:gosec // URL is operator-supplied config
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	return nil
}
