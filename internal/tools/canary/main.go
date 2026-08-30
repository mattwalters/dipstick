package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	outPath := flag.String("out", "", "Path to write Markdown drift report")
	jsonPath := flag.String("json", "", "Path to write JSON drift report")
	mockDrift := flag.Bool("mock-drift", false, "Simulate vendor drift for testing issue creation")
	failOnDrift := flag.Bool("fail-on-drift", false, "Exit with non-zero code if drift is detected")
	verbose := flag.Bool("v", false, "Verbose output")
	stdout := flag.Bool("stdout", false, "Print Markdown report to stdout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := Config{
		MockDrift: *mockDrift,
		Verbose:   *verbose,
	}

	report, err := Run(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running canary probes: %v\n", err)
		os.Exit(1)
	}

	mdContent := GenerateMarkdownReport(report)
	jsonContent, err := GenerateJSONReport(report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating JSON report: %v\n", err)
		os.Exit(1)
	}

	if *outPath != "" {
		if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil && filepath.Dir(*outPath) != "." {
			fmt.Fprintf(os.Stderr, "Error creating directory for %s: %v\n", *outPath, err)
			os.Exit(1)
		}
		if err := os.WriteFile(*outPath, []byte(mdContent), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing markdown report to %s: %v\n", *outPath, err)
			os.Exit(1)
		}
		if *verbose {
			fmt.Printf("Wrote Markdown report to %s\n", *outPath)
		}
	}

	if *jsonPath != "" {
		if err := os.MkdirAll(filepath.Dir(*jsonPath), 0o755); err != nil && filepath.Dir(*jsonPath) != "." {
			fmt.Fprintf(os.Stderr, "Error creating directory for %s: %v\n", *jsonPath, err)
			os.Exit(1)
		}
		if err := os.WriteFile(*jsonPath, append(jsonContent, '\n'), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing JSON report to %s: %v\n", *jsonPath, err)
			os.Exit(1)
		}
		if *verbose {
			fmt.Printf("Wrote JSON report to %s\n", *jsonPath)
		}
	}

	if *stdout || (*outPath == "" && *jsonPath == "") {
		fmt.Print(mdContent)
	}

	if *failOnDrift && report.DriftDetected {
		fmt.Fprintf(os.Stderr, "Drift detected across vendor CLIs: %s\n", report.Summary)
		os.Exit(1)
	}
}
