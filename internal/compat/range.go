package compat

import (
	"fmt"
	"regexp"
	"strings"
)

// Status represents the compatibility status of an observed version against a range.
type Status string

const (
	StatusInRange           Status = "in_range"
	StatusNewerThanVerified Status = "newer_than_verified"
	StatusOlderThanFloor    Status = "older_than_floor"
	StatusUnknown           Status = "unknown"
)

type opType string

const (
	opEq  opType = "="
	opNeq opType = "!="
	opGt  opType = ">"
	opGte opType = ">="
	opLt  opType = "<"
	opLte opType = "<="
)

type constraint struct {
	op      opType
	version SemVer
}

func (c constraint) Matches(v SemVer) bool {
	cmp := v.Compare(c.version)
	switch c.op {
	case opEq:
		return cmp == 0
	case opNeq:
		return cmp != 0
	case opGt:
		return cmp > 0
	case opGte:
		return cmp >= 0
	case opLt:
		return cmp < 0
	case opLte:
		return cmp <= 0
	default:
		return false
	}
}

// Range represents a set of parsed SemVer constraints.
type Range struct {
	raw         string
	constraints []constraint
	floor       *SemVer
	ceiling     *SemVer
}

var (
	comparatorRegex = regexp.MustCompile(`^(>=|<=|>|<|=|!=|\^|~)?\s*v?([0-9A-Za-z.*+-]+)$`)
)

// ParseRange parses a version range expression (e.g. ">=2.1.0 <2.2.0", "^0.148.0", "v0.2.x").
func ParseRange(s string) (*Range, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || strings.EqualFold(trimmed, "none") || strings.EqualFold(trimmed, "n/a") || trimmed == "*" {
		return &Range{raw: s}, nil
	}

	// Handle hyphen range: "v0.2.x - v0.3.x" or "0.2.0 - 0.3.0"
	if parts := strings.Split(trimmed, " - "); len(parts) == 2 {
		return parseHyphenRange(parts[0], parts[1], s)
	}
	if parts := strings.Split(trimmed, " – "); len(parts) == 2 { // en-dash
		return parseHyphenRange(parts[0], parts[1], s)
	}

	// Tokenize on whitespace or comma
	normalized := strings.ReplaceAll(trimmed, ",", " ")
	// Normalize things like ">= 2.1.0" to ">=2.1.0"
	normalized = regexp.MustCompile(`(>=|<=|>|<|=|!=|\^|~)\s+`).ReplaceAllString(normalized, "$1")

	tokens := strings.Fields(normalized)
	var constraints []constraint

	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" || tok == "*" {
			continue
		}

		parsedConstraints, err := parseTokenConstraints(tok)
		if err != nil {
			return nil, fmt.Errorf("compat: parsing range token %q in %q: %w", tok, s, err)
		}
		constraints = append(constraints, parsedConstraints...)
	}

	r := &Range{
		raw:         s,
		constraints: constraints,
	}
	r.computeBounds()
	return r, nil
}

// MustParseRange parses a range expression and panics on error.
func MustParseRange(s string) *Range {
	r, err := ParseRange(s)
	if err != nil {
		panic(err)
	}
	return r
}

func parseHyphenRange(fromStr, toStr, raw string) (*Range, error) {
	fromTok := strings.TrimSpace(fromStr)
	toTok := strings.TrimSpace(toStr)

	vFrom, err := parseWildcardLower(fromTok)
	if err != nil {
		return nil, fmt.Errorf("compat: invalid range lower bound %q: %w", fromTok, err)
	}

	vTo, isWildcard, err := parseWildcardUpper(toTok)
	if err != nil {
		return nil, fmt.Errorf("compat: invalid range upper bound %q: %w", toTok, err)
	}

	var constraints []constraint
	constraints = append(constraints, constraint{op: opGte, version: vFrom})
	if isWildcard {
		constraints = append(constraints, constraint{op: opLt, version: vTo})
	} else {
		constraints = append(constraints, constraint{op: opLte, version: vTo})
	}

	r := &Range{
		raw:         raw,
		constraints: constraints,
	}
	r.computeBounds()
	return r, nil
}

func parseTokenConstraints(tok string) ([]constraint, error) {
	tok = strings.TrimSuffix(tok, "+") // e.g. "v1.18.x+" -> "v1.18.x"

	m := comparatorRegex.FindStringSubmatch(tok)
	if m == nil {
		return nil, fmt.Errorf("invalid comparator expression %q", tok)
	}

	prefix := m[1]
	verStr := m[2]

	switch prefix {
	case "^":
		v, err := Parse(verStr)
		if err != nil {
			return nil, err
		}
		var upper SemVer
		if v.Major > 0 {
			upper = SemVer{Major: v.Major + 1, Minor: 0, Patch: 0}
		} else if v.Minor > 0 {
			upper = SemVer{Major: 0, Minor: v.Minor + 1, Patch: 0}
		} else {
			upper = SemVer{Major: 0, Minor: 0, Patch: v.Patch + 1}
		}
		return []constraint{
			{op: opGte, version: v},
			{op: opLt, version: upper},
		}, nil

	case "~":
		v, err := Parse(verStr)
		if err != nil {
			return nil, err
		}
		upper := SemVer{Major: v.Major, Minor: v.Minor + 1, Patch: 0}
		return []constraint{
			{op: opGte, version: v},
			{op: opLt, version: upper},
		}, nil

	case ">=", ">", "<=", "<", "!=", "=":
		op := opType(prefix)
		// Check for wildcard in version string like ">=1.18.x", ">1.18.x", "<=1.18.x", "<1.18.x", "=1.18.x"
		if strings.ContainsAny(verStr, "xX*") {
			switch op {
			case opGte:
				vLower, err := parseWildcardLower(verStr)
				if err != nil {
					return nil, err
				}
				return []constraint{{op: opGte, version: vLower}}, nil
			case opGt:
				vUpper, _, err := parseWildcardUpper(verStr)
				if err != nil {
					return nil, err
				}
				return []constraint{{op: opGte, version: vUpper}}, nil
			case opLte:
				vUpper, _, err := parseWildcardUpper(verStr)
				if err != nil {
					return nil, err
				}
				return []constraint{{op: opLt, version: vUpper}}, nil
			case opLt:
				vLower, err := parseWildcardLower(verStr)
				if err != nil {
					return nil, err
				}
				return []constraint{{op: opLt, version: vLower}}, nil
			case opEq:
				vLower, err := parseWildcardLower(verStr)
				if err != nil {
					return nil, err
				}
				vUpper, _, err := parseWildcardUpper(verStr)
				if err != nil {
					return nil, err
				}
				return []constraint{
					{op: opGte, version: vLower},
					{op: opLt, version: vUpper},
				}, nil
			default:
				v, err := Parse(verStr)
				if err != nil {
					return nil, err
				}
				return []constraint{{op: op, version: v}}, nil
			}
		}
		v, err := Parse(verStr)
		if err != nil {
			return nil, err
		}
		return []constraint{{op: op, version: v}}, nil

	default:
		// No operator prefix. Check if wildcard.
		if strings.ContainsAny(verStr, "xX*") {
			vLower, err := parseWildcardLower(verStr)
			if err != nil {
				return nil, err
			}
			vUpper, _, err := parseWildcardUpper(verStr)
			if err != nil {
				return nil, err
			}
			return []constraint{
				{op: opGte, version: vLower},
				{op: opLt, version: vUpper},
			}, nil
		}
		v, err := Parse(verStr)
		if err != nil {
			return nil, err
		}
		return []constraint{{op: opEq, version: v}}, nil
	}
}

func parseWildcardLower(s string) (SemVer, error) {
	clean := strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	parts := strings.Split(clean, ".")
	maj := 0
	min := 0
	patch := 0

	var err error
	if len(parts) > 0 && !isWildcardToken(parts[0]) {
		maj, err = parseNum(parts[0])
		if err != nil {
			return SemVer{}, err
		}
	}
	if len(parts) > 1 && !isWildcardToken(parts[1]) {
		min, err = parseNum(parts[1])
		if err != nil {
			return SemVer{}, err
		}
	}
	if len(parts) > 2 && !isWildcardToken(parts[2]) {
		patch, err = parseNum(parts[2])
		if err != nil {
			return SemVer{}, err
		}
	}

	return SemVer{Major: maj, Minor: min, Patch: patch}, nil
}

func parseWildcardUpper(s string) (SemVer, bool, error) {
	clean := strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	parts := strings.Split(clean, ".")

	if len(parts) == 0 {
		return SemVer{}, false, fmt.Errorf("empty version")
	}

	if isWildcardToken(parts[0]) {
		return SemVer{}, true, nil
	}

	maj, err := parseNum(parts[0])
	if err != nil {
		return SemVer{}, false, err
	}

	if len(parts) == 1 || isWildcardToken(parts[1]) {
		return SemVer{Major: maj + 1, Minor: 0, Patch: 0}, true, nil
	}

	min, err := parseNum(parts[1])
	if err != nil {
		return SemVer{}, false, err
	}

	if len(parts) == 2 || isWildcardToken(parts[2]) {
		return SemVer{Major: maj, Minor: min + 1, Patch: 0}, true, nil
	}

	patch, err := parseNum(parts[2])
	if err != nil {
		return SemVer{}, false, err
	}

	return SemVer{Major: maj, Minor: min, Patch: patch}, false, nil
}

func isWildcardToken(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	return t == "x" || t == "*" || t == ""
}

func parseNum(s string) (int, error) {
	s = strings.TrimSpace(s)
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", s)
	}
	return n, nil
}

func (r *Range) computeBounds() {
	var floor *SemVer
	var ceiling *SemVer

	for _, c := range r.constraints {
		switch c.op {
		case opGte, opGt:
			if floor == nil || c.version.Compare(*floor) > 0 {
				v := c.version
				floor = &v
			}
		case opLte, opLt:
			if ceiling == nil || c.version.Compare(*ceiling) < 0 {
				v := c.version
				ceiling = &v
			}
		case opEq:
			v := c.version
			if floor == nil || v.Compare(*floor) > 0 {
				floor = &v
			}
			if ceiling == nil || v.Compare(*ceiling) < 0 {
				ceiling = &v
			}
		}
	}

	r.floor = floor
	r.ceiling = ceiling
}

// Floor returns the lower bound SemVer of this range, if specified.
func (r *Range) Floor() *SemVer {
	return r.floor
}

// Ceiling returns the upper bound SemVer of this range, if specified.
func (r *Range) Ceiling() *SemVer {
	return r.ceiling
}

// Contains returns true if v satisfies every constraint in this range.
func (r *Range) Contains(v SemVer) bool {
	if len(r.constraints) == 0 {
		return true
	}
	for _, c := range r.constraints {
		if !c.Matches(v) {
			return false
		}
	}
	return true
}

// Evaluate evaluates a SemVer against the range and returns one of:
// - StatusInRange
// - StatusOlderThanFloor
// - StatusNewerThanVerified
func (r *Range) Evaluate(v SemVer) Status {
	if len(r.constraints) == 0 {
		return StatusInRange
	}

	if r.Contains(v) {
		return StatusInRange
	}

	// Check if older than floor
	if r.floor != nil {
		for _, c := range r.constraints {
			if (c.op == opGte && v.Compare(c.version) < 0) || (c.op == opGt && v.Compare(c.version) <= 0) {
				return StatusOlderThanFloor
			}
			if c.op == opEq && v.Compare(c.version) < 0 {
				return StatusOlderThanFloor
			}
		}
	}

	// Check if newer than ceiling
	if r.ceiling != nil {
		for _, c := range r.constraints {
			if (c.op == opLt && v.Compare(c.version) >= 0) || (c.op == opLte && v.Compare(c.version) > 0) {
				return StatusNewerThanVerified
			}
			if c.op == opEq && v.Compare(c.version) > 0 {
				return StatusNewerThanVerified
			}
		}
	}

	// Default classification when not contained
	if r.floor != nil && v.Compare(*r.floor) < 0 {
		return StatusOlderThanFloor
	}
	return StatusNewerThanVerified
}

// String returns the raw range string.
func (r *Range) String() string {
	if r == nil {
		return ""
	}
	return r.raw
}
