package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateMatrixTable(t *testing.T) {
	table := GenerateMatrixTable(DefaultMatrix)
	if table == "" {
		t.Fatalf("expected non-empty table output")
	}

	for _, p := range DefaultMatrix {
		if !strings.Contains(table, p.ProviderID) {
			t.Errorf("expected table to contain provider ID %s", p.ProviderID)
		}
		if !strings.Contains(table, p.Vendor) {
			t.Errorf("expected table to contain vendor %s", p.Vendor)
		}
	}
}

func TestUpdateReadmeContent(t *testing.T) {
	sample := `Header
<!-- BEGIN SUPPORT MATRIX -->
old content
<!-- END SUPPORT MATRIX -->
Footer`

	updated, err := UpdateReadmeContent(sample, DefaultMatrix)
	if err != nil {
		t.Fatalf("unexpected error updating readme content: %v", err)
	}

	if !strings.Contains(updated, "Header\n<!-- BEGIN SUPPORT MATRIX -->\n") {
		t.Errorf("expected header before marker, got %s", updated)
	}
	if !strings.Contains(updated, "\n<!-- END SUPPORT MATRIX -->\nFooter") {
		t.Errorf("expected footer after marker, got %s", updated)
	}
	if strings.Contains(updated, "old content") {
		t.Errorf("expected old content to be replaced, got %s", updated)
	}
}

func TestReadmeSynchronization(t *testing.T) {
	// Look for README.md in current dir, or repo root (two levels up)
	readmePath := "README.md"
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		readmePath = filepath.Join("..", "..", "README.md")
	}

	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("failed reading %s: %v", readmePath, err)
	}

	content := string(data)
	updated, err := UpdateReadmeContent(content, DefaultMatrix)
	if err != nil {
		t.Fatalf("failed checking %s: %v", readmePath, err)
	}

	if content != updated {
		t.Errorf("%s support matrix is out of sync with cmd/genmatrix. Run 'make matrix' or 'go run ./cmd/genmatrix' to synchronize.", readmePath)
	}
}
