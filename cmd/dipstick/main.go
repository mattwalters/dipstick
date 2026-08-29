package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattwalters/dipstick"
)

var Version = "dev"

func main() {
	var (
		providerList  string
		timeout       time.Duration
		sourceTimeout time.Duration
		policy        string
		sourceFlag    string
		showVersion   bool
	)

	flag.StringVar(&providerList, "providers", "", "Comma-separated list of providers to collect (default: all)")
	flag.StringVar(&providerList, "p", "", "Comma-separated list of providers to collect (shorthand)")
	flag.DurationVar(&timeout, "timeout", 0, "Overall timeout for collection run (e.g. 5s, 1m)")
	flag.DurationVar(&sourceTimeout, "source-timeout", 0, "Per-source timeout for ladder resolution (e.g. 2s, 500ms)")
	flag.DurationVar(&sourceTimeout, "st", 0, "Per-source timeout for ladder resolution (shorthand)")
	flag.StringVar(&policy, "policy", string(dipstick.SourcePolicyDefault), "Source policy: default, local, remote, api, cli, all")
	flag.StringVar(&sourceFlag, "source", "", "Source policy / tier pin (alias for --policy): api, local, rpc, transcripts, cli")
	flag.BoolVar(&showVersion, "version", false, "Print version information and exit")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: dipstick [options]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("dipstick %s\n", Version)
		return
	}

	var opts []dipstick.Option

	if providerList != "" {
		parts := strings.Split(providerList, ",")
		var providers []dipstick.ProviderID
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				providers = append(providers, dipstick.ProviderID(trimmed))
			}
		}
		if len(providers) > 0 {
			opts = append(opts, dipstick.WithProviders(providers...))
		}
	}

	if timeout != 0 {
		opts = append(opts, dipstick.WithTimeout(timeout))
	}

	// != 0 rather than > 0, matching -timeout above: a negative value has to
	// reach Collect for Collect's validation of it to mean anything.
	if sourceTimeout != 0 {
		opts = append(opts, dipstick.WithSourceTimeout(sourceTimeout))
	}

	activePolicy := policy
	if sourceFlag != "" {
		activePolicy = sourceFlag
	}
	if activePolicy != "" {
		opts = append(opts, dipstick.WithSourcePolicy(dipstick.SourcePolicy(activePolicy)))
	}

	ctx := context.Background()
	report, err := dipstick.Collect(ctx, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "error encoding report: %v\n", err)
		os.Exit(1)
	}
}
