package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProviderCompat represents the compatibility declaration for a provider.
type ProviderCompat struct {
	Vendor          string
	ProviderID      string
	VerifiedVersion string
	SupportedTiers  string
	StatusNotes     string
}

// DefaultMatrix contains the declared compatibility metadata across providers.
var DefaultMatrix = []ProviderCompat{
	{
		Vendor:          "**Claude Code** (Anthropic)",
		ProviderID:      "`claude`",
		VerifiedVersion: "`v0.2.x` – `v0.3.x`",
		SupportedTiers:  "Tier 1 (`oauth_api`), Tier 2 (`local_state`), Tier 4 (`transcripts`)",
		StatusNotes:     "Supported",
	},
	{
		Vendor:          "**OpenAI Codex**",
		ProviderID:      "`codex`",
		VerifiedVersion: "`v0.1.x` – `v0.2.x`",
		SupportedTiers:  "Tier 1 (`oauth_api`), Tier 3 (`local_rpc`), Tier 4 (`transcripts`)",
		StatusNotes:     "Supported",
	},
	{
		Vendor:          "**OpenCode** (`anomalyco/opencode`)",
		ProviderID:      "`opencode`",
		VerifiedVersion: "`v1.18.x`+",
		SupportedTiers:  "Tier 2 (`local_state`), Tier 3 (`local_rpc`), Tier 5 (`cli_stdout`)",
		StatusNotes:     "Supported via local SQLite (`opencode.db`)",
	},
	{
		Vendor:          "**Google Antigravity**",
		ProviderID:      "`antigravity`",
		VerifiedVersion: "None (`N/A`)",
		SupportedTiers:  "None (`ReasonNotSupported`)",
		StatusNotes:     "Exposes no token usage API; cookie extraction prohibited",
	},
}

const (
	matrixStartMarker = "<!-- BEGIN SUPPORT MATRIX -->"
	matrixEndMarker   = "<!-- END SUPPORT MATRIX -->"
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
func GenerateFullSection(matrix []ProviderCompat) string {
	table := GenerateMatrixTable(matrix)
	return fmt.Sprintf("%s\n%s%s", matrixStartMarker, table, matrixEndMarker)
}

// UpdateReadmeContent replaces the support matrix section in readme content.
func UpdateReadmeContent(content string, matrix []ProviderCompat) (string, error) {
	startIdx := strings.Index(content, matrixStartMarker)
	if startIdx == -1 {
		return "", fmt.Errorf("missing start marker %q", matrixStartMarker)
	}

	endIdx := strings.Index(content, matrixEndMarker)
	if endIdx == -1 {
		return "", fmt.Errorf("missing end marker %q", matrixEndMarker)
	}
	endIdx += len(matrixEndMarker)

	if startIdx >= endIdx {
		return "", fmt.Errorf("invalid marker positions: start %d >= end %d", startIdx, endIdx)
	}

	replacement := GenerateFullSection(matrix)
	newContent := content[:startIdx] + replacement + content[endIdx:]
	return newContent, nil
}

func main() {
	readmePath := flag.String("readme", "README.md", "Path to README.md")
	checkOnly := flag.Bool("check", false, "Verify that README.md is up to date without modifying")
	stdoutOnly := flag.Bool("stdout", false, "Print generated table to stdout")
	flag.Parse()

	if *stdoutOnly {
		fmt.Print(GenerateFullSection(DefaultMatrix))
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
