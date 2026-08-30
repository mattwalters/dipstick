package compat

import (
	"fmt"
	"strings"
)

// Check evaluates an observed version string against a verified range expression.
func Check(verifiedRange string, observedVersion string) (Status, error) {
	trimmedRange := strings.TrimSpace(verifiedRange)
	if trimmedRange == "" || strings.EqualFold(trimmedRange, "none") || strings.EqualFold(trimmedRange, "n/a") {
		return StatusInRange, nil
	}

	trimmedVer := strings.TrimSpace(observedVersion)
	if trimmedVer == "" {
		return StatusUnknown, fmt.Errorf("compat: empty observed version")
	}

	r, err := ParseRange(trimmedRange)
	if err != nil {
		return StatusUnknown, err
	}

	v, err := Extract(trimmedVer)
	if err != nil {
		// If extraction fails, try strict parse
		v, err = Parse(trimmedVer)
		if err != nil {
			return StatusUnknown, err
		}
	}

	return r.Evaluate(v), nil
}

// FormatWarning constructs a human-readable warning message for drift.
func FormatWarning(observedVer, verifiedRange, lastCheck string) string {
	if lastCheck != "" {
		return fmt.Sprintf("installed version %s is newer than verified range %s (last verified %s)", observedVer, verifiedRange, lastCheck)
	}
	return fmt.Sprintf("installed version %s is newer than verified range %s", observedVer, verifiedRange)
}
