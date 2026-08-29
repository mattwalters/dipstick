package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattwalters/dipstick"
)

var Version = "dev"

var collectFn = dipstick.Collect

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "version":
			_, _ = fmt.Fprintf(stdout, "dipstick %s\n", Version)
			return 0
		case "doctor":
			return runDoctor(args[1:], stdout, stderr)
		}
	}

	fs := flag.NewFlagSet("dipstick", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		jsonFlag       bool
		providerFlag   string
		providersFlag  string
		pFlag          string
		policyFlag     string
		sourceFlag     string
		strictFlag     bool
		timeoutFlag    time.Duration = dipstick.DefaultTimeout
		sourceTimeout1 time.Duration
		sourceTimeout2 time.Duration
		versionFlag    bool
		vFlag          bool
	)

	fs.BoolVar(&jsonFlag, "json", false, "Output dipstick.v1 report to stdout as JSON")
	fs.StringVar(&providerFlag, "provider", "", "Comma-separated list of providers to collect (alias for --providers)")
	fs.StringVar(&providersFlag, "providers", "", "Comma-separated list of providers to collect (default: all)")
	fs.StringVar(&pFlag, "p", "", "Comma-separated list of providers to collect (shorthand)")
	fs.StringVar(&policyFlag, "policy", "", "Source policy: default, local, remote, api, cli, all")
	fs.StringVar(&sourceFlag, "source", "", "Source policy / tier pin (alias for --policy): api, local, rpc, transcripts, cli")
	fs.BoolVar(&strictFlag, "strict", false, "Treat drift warnings as failures")
	fs.DurationVar(&timeoutFlag, "timeout", dipstick.DefaultTimeout, "Overall timeout for collection run (e.g. 5s, 1m)")
	fs.DurationVar(&sourceTimeout1, "source-timeout", 0, "Per-source timeout for ladder resolution (e.g. 2s, 500ms)")
	fs.DurationVar(&sourceTimeout2, "st", 0, "Per-source timeout for ladder resolution (shorthand)")
	fs.BoolVar(&versionFlag, "version", false, "Print version information and exit")
	fs.BoolVar(&vFlag, "v", false, "Print version information and exit (shorthand)")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: dipstick [options] [command]\n\n")
		_, _ = fmt.Fprintf(stderr, "Commands:\n")
		_, _ = fmt.Fprintf(stderr, "  doctor      Diagnose provider installations and source ladders\n")
		_, _ = fmt.Fprintf(stderr, "  version     Print version information\n\n")
		_, _ = fmt.Fprintf(stderr, "Options:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if versionFlag || vFlag {
		_, _ = fmt.Fprintf(stdout, "dipstick %s\n", Version)
		return 0
	}

	remaining := fs.Args()
	if len(remaining) > 0 {
		switch remaining[0] {
		case "version":
			_, _ = fmt.Fprintf(stdout, "dipstick %s\n", Version)
			return 0
		case "doctor":
			return runDoctor(remaining[1:], stdout, stderr)
		default:
			_, _ = fmt.Fprintf(stderr, "error: unexpected argument %q\n", remaining[0])
			return 2
		}
	}

	if timeoutFlag < 0 {
		_, _ = fmt.Fprintf(stderr, "error: invalid timeout: %v\n", timeoutFlag)
		return 2
	}

	sourceTimeout := sourceTimeout1
	if sourceTimeout2 != 0 {
		sourceTimeout = sourceTimeout2
	}
	if sourceTimeout < 0 {
		_, _ = fmt.Fprintf(stderr, "error: invalid source timeout: %v\n", sourceTimeout)
		return 2
	}

	var opts []dipstick.Option

	var rawProviders []string
	for _, p := range []string{providerFlag, providersFlag, pFlag} {
		if p != "" {
			rawProviders = append(rawProviders, p)
		}
	}

	if len(rawProviders) > 0 {
		var providers []dipstick.ProviderID
		for _, raw := range rawProviders {
			parts := strings.Split(raw, ",")
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					providers = append(providers, dipstick.ProviderID(trimmed))
				}
			}
		}
		if len(providers) > 0 {
			opts = append(opts, dipstick.WithProviders(providers...))
		}
	}

	if timeoutFlag != 0 {
		opts = append(opts, dipstick.WithTimeout(timeoutFlag))
	}

	if sourceTimeout != 0 {
		opts = append(opts, dipstick.WithSourceTimeout(sourceTimeout))
	}

	activePolicy := policyFlag
	if sourceFlag != "" {
		activePolicy = sourceFlag
	}
	if activePolicy != "" {
		opts = append(opts, dipstick.WithSourcePolicy(dipstick.SourcePolicy(activePolicy)))
	}

	if strictFlag {
		opts = append(opts, dipstick.WithStrict(true))
	}

	ctx := context.Background()
	report, err := collectFn(ctx, opts...)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		_, _ = fmt.Fprintf(stderr, "error encoding report: %v\n", err)
		return 2
	}

	if len(report.Providers) == 0 {
		return 1
	}

	return 0
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	_, _ = fmt.Fprintf(stderr, "dipstick doctor is not yet implemented\n")
	return 0
}
