package compat_test

import (
	"testing"

	"github.com/mattwalters/dipstick/internal/compat"
)

func TestSemVer_Parse(t *testing.T) {
	tests := []struct {
		input       string
		wantMajor   int
		wantMinor   int
		wantPatch   int
		wantPrerel  string
		wantBuild   string
		expectError bool
	}{
		{"2.1.0", 2, 1, 0, "", "", false},
		{"v2.1.0", 2, 1, 0, "", "", false},
		{"V0.148.0", 0, 148, 0, "", "", false},
		{"1.18", 1, 18, 0, "", "", false},
		{"1.0.0-alpha", 1, 0, 0, "alpha", "", false},
		{"1.0.0-alpha.1", 1, 0, 0, "alpha.1", "", false},
		{"1.0.0-0.3.7", 1, 0, 0, "0.3.7", "", false},
		{"1.0.0-x.7.z.92", 1, 0, 0, "x.7.z.92", "", false},
		{"1.0.0+20130313144700", 1, 0, 0, "", "20130313144700", false},
		{"1.0.0-beta+exp.sha.5114f85", 1, 0, 0, "beta", "exp.sha.5114f85", false},
		{"", 0, 0, 0, "", "", true},
		{"invalid", 0, 0, 0, "", "", true},
		{"a.b.c", 0, 0, 0, "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := compat.Parse(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if v.Major != tt.wantMajor || v.Minor != tt.wantMinor || v.Patch != tt.wantPatch {
				t.Errorf("got version %d.%d.%d, want %d.%d.%d", v.Major, v.Minor, v.Patch, tt.wantMajor, tt.wantMinor, tt.wantPatch)
			}
			if v.Prerelease != tt.wantPrerel {
				t.Errorf("got prerelease %q, want %q", v.Prerelease, tt.wantPrerel)
			}
			if v.Build != tt.wantBuild {
				t.Errorf("got build %q, want %q", v.Build, tt.wantBuild)
			}
		})
	}
}

func TestSemVer_Extract(t *testing.T) {
	tests := []struct {
		input       string
		wantString  string
		expectError bool
	}{
		{"claude 2.1.4 (build 123)", "2.1.4", false},
		{"codex version 0.149.0 (2026-08-29)", "0.149.0", false},
		{"opencode/1.18.2 darwin-arm64", "1.18.2", false},
		{"v2.1.0", "2.1.0", false},
		{"Agent version v0.148.5-beta.1", "0.148.5-beta.1", false},
		{"nothing here", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := compat.Extract(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if v.String() != tt.wantString {
				t.Errorf("got %q, want %q", v.String(), tt.wantString)
			}
		})
	}
}

func TestSemVer_Compare(t *testing.T) {
	tests := []struct {
		v1   string
		v2   string
		want int // -1 for v1 < v2, 0 for equal, 1 for v1 > v2
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.1.0", "1.2.0", -1},
		{"1.2.0", "1.1.0", 1},
		{"1.0.1", "1.0.2", -1},
		{"1.0.2", "1.0.1", 1},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-alpha.beta", "1.0.0-beta", -1},
		{"1.0.0-beta", "1.0.0-beta.2", -1},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1},
		{"1.0.0-beta.11", "1.0.0-rc.1", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0+build1", "1.0.0+build2", 0}, // build metadata ignored in precedence
	}

	for _, tt := range tests {
		t.Run(tt.v1+"_vs_"+tt.v2, func(t *testing.T) {
			v1 := compat.MustParse(tt.v1)
			v2 := compat.MustParse(tt.v2)
			got := v1.Compare(v2)
			if got != tt.want {
				t.Errorf("%s vs %s: got %d, want %d", tt.v1, tt.v2, got, tt.want)
			}
		})
	}
}

func TestRange_Evaluate(t *testing.T) {
	tests := []struct {
		rangeExpr string
		version   string
		want      compat.Status
	}{
		// Standard Claude Range
		{">=2.1.0 <2.2.0", "2.0.9", compat.StatusOlderThanFloor},
		{">=2.1.0 <2.2.0", "2.1.0", compat.StatusInRange},
		{">=2.1.0 <2.2.0", "2.1.4", compat.StatusInRange},
		{">=2.1.0 <2.2.0", "2.1.99", compat.StatusInRange},
		{">=2.1.0 <2.2.0", "2.2.0", compat.StatusNewerThanVerified},
		{">=2.1.0 <2.2.0", "3.0.0", compat.StatusNewerThanVerified},

		// Standard Codex Range
		{">=0.148.0 <0.150.0", "0.147.9", compat.StatusOlderThanFloor},
		{">=0.148.0 <0.150.0", "0.148.0", compat.StatusInRange},
		{">=0.148.0 <0.150.0", "0.149.5", compat.StatusInRange},
		{">=0.148.0 <0.150.0", "0.150.0", compat.StatusNewerThanVerified},

		// OpenCode Floor Range
		{">=1.18.0", "1.17.9", compat.StatusOlderThanFloor},
		{">=1.18.0", "1.18.0", compat.StatusInRange},
		{">=1.18.0", "2.0.0", compat.StatusInRange},

		// Caret Range
		{"^2.1.0", "2.0.0", compat.StatusOlderThanFloor},
		{"^2.1.0", "2.5.0", compat.StatusInRange},
		{"^2.1.0", "3.0.0", compat.StatusNewerThanVerified},
		{"^0.14.0", "0.13.9", compat.StatusOlderThanFloor},
		{"^0.14.0", "0.14.5", compat.StatusInRange},
		{"^0.14.0", "0.15.0", compat.StatusNewerThanVerified},

		// Tilde Range
		{"~2.1.0", "2.0.9", compat.StatusOlderThanFloor},
		{"~2.1.0", "2.1.5", compat.StatusInRange},
		{"~2.1.0", "2.2.0", compat.StatusNewerThanVerified},

		// Wildcards
		{"v0.2.x", "0.1.9", compat.StatusOlderThanFloor},
		{"v0.2.x", "0.2.5", compat.StatusInRange},
		{"v0.2.x", "0.3.0", compat.StatusNewerThanVerified},
		{">=1.18.x", "1.17.9", compat.StatusOlderThanFloor},
		{">=1.18.x", "1.18.2", compat.StatusInRange},
		{">1.18.x", "1.18.9", compat.StatusOlderThanFloor},
		{">1.18.x", "1.19.0", compat.StatusInRange},
		{"<1.18.x", "1.18.0", compat.StatusNewerThanVerified},
		{"<1.18.x", "1.17.9", compat.StatusInRange},
		{"<=1.18.x", "1.18.9", compat.StatusInRange},
		{"<=1.18.x", "1.19.0", compat.StatusNewerThanVerified},
		{"=1.18.x", "1.17.9", compat.StatusOlderThanFloor},
		{"=1.18.x", "1.18.5", compat.StatusInRange},
		{"=1.18.x", "1.19.0", compat.StatusNewerThanVerified},

		// None / N/A
		{"None", "1.0.0", compat.StatusInRange},
		{"N/A", "1.0.0", compat.StatusInRange},
		{"", "1.0.0", compat.StatusInRange},
	}

	for _, tt := range tests {
		t.Run(tt.rangeExpr+"_check_"+tt.version, func(t *testing.T) {
			status, err := compat.Check(tt.rangeExpr, tt.version)
			if err != nil {
				t.Fatalf("unexpected error for range %q and version %q: %v", tt.rangeExpr, tt.version, err)
			}
			if status != tt.want {
				t.Errorf("range %q vs %q: got status %q, want %q", tt.rangeExpr, tt.version, status, tt.want)
			}
		})
	}
}

func TestCheck_MessyCLIOutput(t *testing.T) {
	rangeExpr := ">=2.1.0 <2.2.0"

	status, err := compat.Check(rangeExpr, "claude 2.1.5 (commit 8ef32a)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != compat.StatusInRange {
		t.Errorf("expected StatusInRange, got %s", status)
	}

	status, err = compat.Check(rangeExpr, "claude version 2.3.0 (built 2026-09-01)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != compat.StatusNewerThanVerified {
		t.Errorf("expected StatusNewerThanVerified, got %s", status)
	}

	status, err = compat.Check(rangeExpr, "claude 1.9.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != compat.StatusOlderThanFloor {
		t.Errorf("expected StatusOlderThanFloor, got %s", status)
	}
}
