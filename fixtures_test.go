package dipstick_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/mattwalters/dipstick"
)

type manifestDoc struct {
	Provider      string   `json:"provider"`
	VendorVersion string   `json:"vendor_version"`
	CapturedAt    string   `json:"captured_at"`
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	Sources       []string `json:"sources"`
}

func TestFixtures_ManifestIntegrity(t *testing.T) {
	fixtureRoot := filepath.Join("testdata", "fixtures")
	manifestCount := 0

	err := filepath.Walk(fixtureRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Base(path) != "manifest.json" {
			return nil
		}

		manifestCount++
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("failed reading %s: %v", path, err)
			return nil
		}

		var m manifestDoc
		if err := json.Unmarshal(data, &m); err != nil {
			t.Errorf("malformed JSON in %s: %v", path, err)
			return nil
		}

		if m.Provider == "" {
			t.Errorf("%s: empty provider", path)
		}
		if m.VendorVersion == "" {
			t.Errorf("%s: empty vendor_version", path)
		}
		if m.CapturedAt == "" {
			t.Errorf("%s: empty captured_at", path)
		} else {
			if _, err := time.Parse(time.RFC3339, m.CapturedAt); err != nil {
				t.Errorf("%s: invalid RFC3339 timestamp %q: %v", path, m.CapturedAt, err)
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("walking fixtures directory: %v", err)
	}

	if manifestCount == 0 {
		t.Fatalf("no manifest.json files found in %s", fixtureRoot)
	}
}

func TestFixtures_GoldenReportsSchemaConformance(t *testing.T) {
	schemaPath := filepath.Join("schema", "dipstick.v1.json")
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		t.Fatalf("failed compiling schema %s: %v", schemaPath, err)
	}

	fixtureRoot := filepath.Join("testdata", "fixtures")
	goldenCount := 0

	err = filepath.Walk(fixtureRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Base(path) != "golden_report.json" {
			return nil
		}

		goldenCount++
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("failed reading golden report %s: %v", path, err)
			return nil
		}

		var rawProv any
		if err := json.Unmarshal(data, &rawProv); err != nil {
			t.Errorf("failed unmarshaling golden report %s: %v", path, err)
			return nil
		}

		// Also unmarshal into typed ProviderReport to ensure Go struct decodability
		var provReport dipstick.ProviderReport
		if err := json.Unmarshal(data, &provReport); err != nil {
			t.Errorf("failed decoding typed provider report %s: %v", path, err)
			return nil
		}

		// Wrap raw payload into complete top-level Report map for strict schema validation
		rawTopReport := map[string]any{
			"schema_version": dipstick.SchemaVersion,
			"generated_at":   time.Now().UTC().Format(time.RFC3339),
			"providers":      []any{rawProv},
		}

		if err := schema.Validate(rawTopReport); err != nil {
			t.Errorf("golden report %s failed dipstick.v1 schema validation: %v\nJSON:\n%s", path, err, string(data))
		}
		return nil
	})

	if err != nil {
		t.Fatalf("walking fixtures directory: %v", err)
	}

	if goldenCount == 0 {
		t.Fatalf("no golden_report.json files found in %s", fixtureRoot)
	}
}
