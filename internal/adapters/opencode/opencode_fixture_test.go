package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type fixtureManifest struct {
	Provider      string   `json:"provider"`
	VendorVersion string   `json:"vendor_version"`
	CapturedAt    string   `json:"captured_at"`
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	Sources       []string `json:"sources"`
}

func TestOpenCodeFixtures_Manifests(t *testing.T) {
	fixturesRoot := filepath.Join("..", "..", "..", "testdata", "fixtures", "opencode")
	entries, err := os.ReadDir(fixturesRoot)
	if err != nil {
		t.Fatalf("reading opencode fixtures root: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "malformed" {
			continue
		}

		manifestPath := filepath.Join(fixturesRoot, entry.Name(), "manifest.json")
		manifestData, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("reading manifest %s: %v", manifestPath, err)
		}

		var manifest fixtureManifest
		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			t.Fatalf("unmarshaling manifest %s: %v", manifestPath, err)
		}

		if manifest.Provider != "opencode" {
			t.Errorf("manifest provider: got %s, want opencode", manifest.Provider)
		}
	}
}

func FuzzOpenCodeParser(f *testing.F) {
	seeds := []string{
		filepath.Join("..", "..", "..", "testdata", "fixtures", "opencode", "malformed", "corrupted.json"),
	}

	for _, p := range seeds {
		if data, err := os.ReadFile(p); err == nil {
			f.Add(data)
		}
	}

	f.Add([]byte(`{"status": "ok"}`))
	f.Add([]byte(`{"tokens": {"input": 100, "output": 50}}`))
	f.Add([]byte(``))

	adapter := New()
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = adapter.Name()
		var v any
		_ = json.Unmarshal(data, &v)
	})
}
