package compat

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// SemVer represents a parsed Semantic Version 2.0.0 structure.
type SemVer struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	Build      string
	raw        string
}

var (
	// semverStrictRegex matches full or partial semver strings with optional 'v' or 'V' prefix.
	semverStrictRegex  = regexp.MustCompile(`^[vV]?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-([0-9A-Za-z.-]+))?(?:\+([0-9A-Za-z.-]+))?$`)
	semverExtractRegex = regexp.MustCompile(`[vV]?(\d+)\.(\d+)(?:\.(\d+))?(?:-([0-9A-Za-z.-]+))?(?:\+([0-9A-Za-z.-]+))?`)
)

// Parse parses a strict SemVer string (e.g. "2.1.0", "v2.1.0", "1.0.0-alpha.1+build123").
func Parse(s string) (SemVer, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return SemVer{}, fmt.Errorf("compat: empty version string")
	}

	matches := semverStrictRegex.FindStringSubmatch(trimmed)
	if matches == nil {
		return SemVer{}, fmt.Errorf("compat: invalid semver format: %q", s)
	}

	return parseMatches(matches, trimmed)
}

// MustParse parses a SemVer string and panics if invalid.
func MustParse(s string) SemVer {
	v, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

// Extract extracts and parses the first SemVer token from a messy probe string (e.g. "claude 2.1.0 (commit abc)").
func Extract(s string) (SemVer, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return SemVer{}, fmt.Errorf("compat: empty version string")
	}

	matches := semverExtractRegex.FindStringSubmatch(trimmed)
	if matches == nil {
		return SemVer{}, fmt.Errorf("compat: no semver found in: %q", s)
	}

	return parseMatches(matches, matches[0])
}

func parseMatches(matches []string, raw string) (SemVer, error) {
	maj, err := strconv.Atoi(matches[1])
	if err != nil {
		return SemVer{}, fmt.Errorf("compat: invalid major version: %q", matches[1])
	}

	min := 0
	if len(matches) > 2 && matches[2] != "" {
		m, err := strconv.Atoi(matches[2])
		if err != nil {
			return SemVer{}, fmt.Errorf("compat: invalid minor version: %q", matches[2])
		}
		min = m
	}

	patch := 0
	if len(matches) > 3 && matches[3] != "" {
		p, err := strconv.Atoi(matches[3])
		if err != nil {
			return SemVer{}, fmt.Errorf("compat: invalid patch version: %q", matches[3])
		}
		patch = p
	}

	var prerelease, build string
	if len(matches) > 4 {
		prerelease = matches[4]
	}
	if len(matches) > 5 {
		build = matches[5]
	}

	return SemVer{
		Major:      maj,
		Minor:      min,
		Patch:      patch,
		Prerelease: prerelease,
		Build:      build,
		raw:        raw,
	}, nil
}

// String returns the canonical SemVer representation.
func (v SemVer) String() string {
	res := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease != "" {
		res += "-" + v.Prerelease
	}
	if v.Build != "" {
		res += "+" + v.Build
	}
	return res
}

// Compare returns -1 if v < other, 0 if v == other, and 1 if v > other.
// Precedence is calculated according to Semantic Versioning 2.0.0.
func (v SemVer) Compare(other SemVer) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}

	// When major, minor, and patch are equal, a pre-release version has lower precedence
	// than a normal version.
	if v.Prerelease == "" && other.Prerelease != "" {
		return 1
	}
	if v.Prerelease != "" && other.Prerelease == "" {
		return -1
	}
	if v.Prerelease != "" && other.Prerelease != "" {
		return comparePrereleases(v.Prerelease, other.Prerelease)
	}

	return 0
}

// comparePrereleases compares two dot-separated prerelease identifier strings.
func comparePrereleases(p1, p2 string) int {
	parts1 := strings.Split(p1, ".")
	parts2 := strings.Split(p2, ".")

	n := len(parts1)
	if len(parts2) < n {
		n = len(parts2)
	}

	for i := 0; i < n; i++ {
		id1 := parts1[i]
		id2 := parts2[i]

		if id1 == id2 {
			continue
		}

		num1, err1 := strconv.Atoi(id1)
		num2, err2 := strconv.Atoi(id2)

		// Identifiers consisting of only digits are compared numerically.
		if err1 == nil && err2 == nil {
			if num1 != num2 {
				if num1 < num2 {
					return -1
				}
				return 1
			}
			continue
		}

		// Numeric identifiers always have lower precedence than non-numeric identifiers.
		if err1 == nil && err2 != nil {
			return -1
		}
		if err1 != nil && err2 == nil {
			return 1
		}

		// Identifiers with letters or hyphens are compared lexically in ASCII sort order.
		if id1 < id2 {
			return -1
		}
		return 1
	}

	// A larger set of pre-release fields has a higher precedence than a smaller set,
	// if all of the preceding identifiers are equal.
	if len(parts1) < len(parts2) {
		return -1
	}
	if len(parts1) > len(parts2) {
		return 1
	}

	return 0
}
