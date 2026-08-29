//go:build darwin

package localstate

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mattwalters/dipstick/internal/cliexec"
)

type darwinKeychainReader struct {
	runner *cliexec.Runner
}

// NewDarwinKeychainReader returns a KeychainReader implementation backed by macOS /usr/bin/security.
func NewDarwinKeychainReader() KeychainReader {
	return &darwinKeychainReader{
		runner: cliexec.New(
			cliexec.WithTimeout(5*time.Second),
			cliexec.WithScrubSecrets(true),
		),
	}
}

// NewPlatformKeychainReader returns the platform-specific KeychainReader for macOS.
func NewPlatformKeychainReader() KeychainReader {
	return NewDarwinKeychainReader()
}

func (r *darwinKeychainReader) GetGenericPassword(ctx context.Context, service string, account string) ([]byte, error) {
	if strings.TrimSpace(service) == "" {
		return nil, fmt.Errorf("keychain service cannot be empty")
	}

	args := []string{"find-generic-password", "-s", service}
	if account != "" {
		args = append(args, "-a", account)
	}
	args = append(args, "-w") // Output only password bytes

	res, err := r.runner.Run(ctx, "/usr/bin/security", args...)
	if err != nil {
		if res != nil {
			// Exit code 44 is errSecItemNotFound in macOS Security framework.
			if res.ExitCode == 44 || strings.Contains(res.StderrString(), "The specified item could not be found") {
				return nil, ErrKeychainItemNotFound
			}
			if strings.Contains(res.StderrString(), "User canceled") || strings.Contains(res.StderrString(), "interaction not allowed") {
				return nil, ErrKeychainAccessDenied
			}
		}
		return nil, fmt.Errorf("reading keychain for service %q: %w", service, err)
	}

	out := bytes.TrimRight(res.Stdout, "\r\n")
	if len(out) == 0 {
		return nil, ErrKeychainItemNotFound
	}

	return out, nil
}
