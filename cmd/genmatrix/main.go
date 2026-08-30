package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattwalters/dipstick/internal/adapters/antigravity"
	"github.com/mattwalters/dipstick/internal/adapters/claude"
	"github.com/mattwalters/dipstick/internal/adapters/codex"
	"github.com/mattwalters/dipstick/internal/adapters/opencode"
)

// ProviderCompat represents the compatibility declaration for a provider.
type ProviderCompat struct {
	Vendor          string
	ProviderID      string
	VerifiedVersion string
	SupportedTiers  string
	StatusNotes     string
}

// BuildMatrix constructs the compatibility table from adapter declarations.
func BuildMatrix() []ProviderCompat {
	claudeAdp := claude.New()
	codexAdp := codex.New()
	openCodeAdp := opencode.New()
	antigravityAdp := antigravity.New()

	return []ProviderCompat{
		{
			Vendor:          "**Claude Code** (Anthropic)",
			ProviderID:      "`claude`",
			VerifiedVersion: formatVerifiedVersion(claudeAdp.Compat().VerifiedRange),
			SupportedTiers:  "Tier 1 (`oauth_api`)",
			StatusNotes:     claudeAdp.Compat().Notes,
		},
		{
			Vendor:          "**OpenAI Codex**",
			ProviderID:      "`codex`",
			VerifiedVersion: formatVerifiedVersion(codexAdp.Compat().VerifiedRange),
			SupportedTiers:  "Tier 2 (`local_state`)",
			StatusNotes:     codexAdp.Compat().Notes,
		},
		{
			Vendor:          "**OpenCode** (`anomalyco/opencode`)",
			ProviderID:      "`opencode`",
			VerifiedVersion: formatVerifiedVersion(openCodeAdp.Compat().VerifiedRange),
			SupportedTiers:  "Tier 2 (`local_state`), Tier 3 (`local_rpc`), Tier 5 (`cli_stdout`)",
			StatusNotes:     openCodeAdp.Compat().Notes,
		},
		{
			Vendor:          "**Google Antigravity**",
			ProviderID:      "`antigravity`",
			VerifiedVersion: formatVerifiedVersion(antigravityAdp.Compat().VerifiedRange),
			SupportedTiers:  "None (`ReasonNotSupported`)",
			StatusNotes:     antigravityAdp.Compat().Notes,
		},
	}
}

func formatVerifiedVersion(v string) string {
	if v == "" || v == "None" || v == "N/A" {
		return "None (`N/A`)"
	}
	return fmt.Sprintf("`%s`", v)
}

// DefaultMatrix contains the declared compatibility metadata across providers.
var DefaultMatrix = BuildMatrix()

const (
	matrixStartMarkerLegacy = "<!-- BEGIN SUPPORT MATRIX -->"
	matrixEndMarkerLegacy   = "<!-- END SUPPORT MATRIX -->"
	matrixStartMarkerCompat = "<!-- COMPAT_MATRIX_START -->"
	matrixEndMarkerCompat   = "<!-- COMPAT_MATRIX_END -->"
)

// GenerateMatrixTable renders the markdown table from provider declarations.
func GenerateMatrixTable(matrix []ProviderCompat) string {
	var buf bytes.Buffer
	buf.WriteString("| Vendor | Provider ID | Verified Versions | Supported Sources / Tiers | Status & Notes |\n")
	buf.WriteString("| :--- | :--- | :--- | :--- | :--- |\n")
	for _, p := range matrix {
		buf.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
			p.Vendor, p.ProviderID, p.VerifiedVersion, p.SupportedTiers, p.StatusNotes))
	}
	return buf.String()
}

// GenerateFullSection renders the matrix enclosed by marker tags.
func GenerateFullSection(matrix []ProviderCompat, startMarker, endMarker string) string {
	table := GenerateMatrixTable(matrix)
	return fmt.Sprintf("%s\n%s%s", startMarker, table, endMarker)
}

// UpdateReadmeContent replaces the support matrix section in readme content.
func UpdateReadmeContent(content string, matrix []ProviderCompat) (string, error) {
	startMarker := matrixStartMarkerLegacy
	endMarker := matrixEndMarkerLegacy

	if strings.Contains(content, matrixStartMarkerCompat) {
		startMarker = matrixStartMarkerCompat
		endMarker = matrixEndMarkerCompat
	}

	startIdx := strings.Index(content, startMarker)
	if startIdx == -1 {
		return "", fmt.Errorf("missing start marker %q", startMarker)
	}

	endIdx := strings.Index(content, endMarker)
	if endIdx == -1 {
		return "", fmt.Errorf("missing end marker %q", endMarker)
	}
	endIdx += len(endMarker)

	if startIdx >= endIdx {
		return "", fmt.Errorf("invalid marker positions: start %d >= end %d", startIdx, endIdx)
	}

	replacement := GenerateFullSection(matrix, startMarker, endMarker)
	newContent := content[:startIdx] + replacement + content[endIdx:]
	return newContent, nil
}

func main() {
	readmePath := flag.String("readme", "README.md", "Path to README.md")
	checkOnly := flag.Bool("check", false, "Verify that README.md is up to date without modifying")
	stdoutOnly := flag.Bool("stdout", false, "Print generated table to stdout")
	flag.Parse()

	if *stdoutOnly {
		fmt.Print(GenerateFullSection(DefaultMatrix, matrixStartMarkerLegacy, matrixEndMarkerLegacy))
		return
	}

	path := *readmePath
	if !filepath.IsAbs(path) {
		// If running from cmd/genmatrix, resolve relative to repo root if necessary
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if _, err := os.Stat(filepath.Join("..", "..", path)); err == nil {
				path = filepath.Join("..", "..", path)
			}
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", path, err)
		os.Exit(1)
	}

	content := string(data)
	updated, err := UpdateReadmeContent(content, DefaultMatrix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error updating matrix in %s: %v\n", path, err)
		os.Exit(1)
	}

	if *checkOnly {
		if content != updated {
			fmt.Fprintf(os.Stderr, "error: %s support matrix is out of sync with provider definitions. Run 'make matrix' to update.\n", path)
			os.Exit(1)
		}
		fmt.Printf("%s support matrix is synchronized.\n", path)
		return
	}

	if content == updated {
		fmt.Printf("%s is already up to date.\n", path)
		return
	}

	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Printf("Updated support matrix in %s successfully.\n", path)
}
