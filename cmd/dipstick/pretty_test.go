package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattwalters/dipstick"
)

func loadReportFixture(t *testing.T, relPath string) *dipstick.Report {
	t.Helper()
	rootRel := filepath.Join("..", "..", relPath)
	data, err := os.ReadFile(rootRel)
	if err != nil {
		t.Fatalf("failed reading fixture %s: %v", rootRel, err)
	}
	var rep dipstick.Report
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("failed unmarshaling fixture %s: %v", rootRel, err)
	}
	return &rep
}

func readGoldenFile(t *testing.T, relPath string) string {
	t.Helper()
	rootRel := filepath.Join("..", "..", relPath)
	data, err := os.ReadFile(rootRel)
	if err != nil {
		t.Fatalf("failed reading golden file %s: %v", rootRel, err)
	}
	return string(data)
}

func TestRenderPretty_GoldenFiles(t *testing.T) {
	fullRep := loadReportFixture(t, filepath.Join("testdata", "report_full.golden.json"))
	// Fixed reference time matching report_full.golden.json generated_at (2026-08-29T12:00:00Z)
	fixedTime := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	t.Run("pretty_full", func(t *testing.T) {
		var buf bytes.Buffer
		colorFalse := false
		unicodeTrue := true
		err := RenderPretty(&buf, fullRep, RenderOptions{
			Width:         80,
			Color:         &colorFalse,
			Unicode:       &unicodeTrue,
			ReferenceTime: fixedTime,
		})
		if err != nil {
			t.Fatalf("RenderPretty failed: %v", err)
		}

		expected := readGoldenFile(t, filepath.Join("testdata", "pretty_full.golden.txt"))
		if buf.String() != expected {
			t.Errorf("mismatch for pretty_full\nGot:\n%s\nWant:\n%s", buf.String(), expected)
		}
	})

	t.Run("pretty_no_color", func(t *testing.T) {
		var buf bytes.Buffer
		colorFalse := false
		unicodeTrue := true
		err := RenderPretty(&buf, fullRep, RenderOptions{
			Width:         80,
			Color:         &colorFalse,
			Unicode:       &unicodeTrue,
			ReferenceTime: fixedTime,
		})
		if err != nil {
			t.Fatalf("RenderPretty failed: %v", err)
		}

		expected := readGoldenFile(t, filepath.Join("testdata", "pretty_no_color.golden.txt"))
		if buf.String() != expected {
			t.Errorf("mismatch for pretty_no_color\nGot:\n%s\nWant:\n%s", buf.String(), expected)
		}
	})

	t.Run("pretty_ascii", func(t *testing.T) {
		var buf bytes.Buffer
		colorFalse := false
		unicodeFalse := false
		err := RenderPretty(&buf, fullRep, RenderOptions{
			Width:         80,
			Color:         &colorFalse,
			Unicode:       &unicodeFalse,
			ReferenceTime: fixedTime,
		})
		if err != nil {
			t.Fatalf("RenderPretty failed: %v", err)
		}

		expected := readGoldenFile(t, filepath.Join("testdata", "pretty_ascii.golden.txt"))
		if buf.String() != expected {
			t.Errorf("mismatch for pretty_ascii\nGot:\n%s\nWant:\n%s", buf.String(), expected)
		}
	})

	t.Run("pretty_narrow", func(t *testing.T) {
		var buf bytes.Buffer
		colorFalse := false
		unicodeTrue := true
		err := RenderPretty(&buf, fullRep, RenderOptions{
			Width:         45,
			Color:         &colorFalse,
			Unicode:       &unicodeTrue,
			ReferenceTime: fixedTime,
		})
		if err != nil {
			t.Fatalf("RenderPretty failed: %v", err)
		}

		expected := readGoldenFile(t, filepath.Join("testdata", "pretty_narrow.golden.txt"))
		if buf.String() != expected {
			t.Errorf("mismatch for pretty_narrow\nGot:\n%s\nWant:\n%s", buf.String(), expected)
		}
	})

	t.Run("pretty_all_missing", func(t *testing.T) {
		missingRep := &dipstick.Report{
			SchemaVersion: dipstick.SchemaVersion,
			GeneratedAt:   fixedTime,
			Providers: []dipstick.ProviderReport{
				{
					Provider:   dipstick.ProviderClaude,
					Source:     dipstick.SourceOAuthAPI,
					Confidence: dipstick.ConfidenceDerived,
					Windows: []dipstick.RateWindow{
						{
							Label:       "session",
							UsedPercent: nil,
						},
					},
				},
				{
					Provider:   dipstick.ProviderCodex,
					Source:     dipstick.SourceLocalState,
					Confidence: dipstick.ConfidenceDerived,
					Windows: []dipstick.RateWindow{
						{
							Label:       "weekly",
							UsedPercent: nil,
						},
					},
				},
			},
			Errors: []dipstick.ProviderError{
				{
					Provider: dipstick.ProviderAntigravity,
					Reason:   dipstick.ReasonNotSupported,
					Detail:   "antigravity exposes no usage or quota surface",
				},
				{
					Provider: dipstick.ProviderOpenCode,
					Reason:   dipstick.ReasonUpstreamError,
					Detail:   "opencode connection failed",
				},
			},
		}

		var buf bytes.Buffer
		colorFalse := false
		unicodeTrue := true
		err := RenderPretty(&buf, missingRep, RenderOptions{
			Width:         80,
			Color:         &colorFalse,
			Unicode:       &unicodeTrue,
			ReferenceTime: fixedTime,
		})
		if err != nil {
			t.Fatalf("RenderPretty failed: %v", err)
		}

		expected := readGoldenFile(t, filepath.Join("testdata", "pretty_all_missing.golden.txt"))
		if buf.String() != expected {
			t.Errorf("mismatch for pretty_all_missing\nGot:\n%s\nWant:\n%s", buf.String(), expected)
		}
	})
}

func TestRenderPretty_PointerVsZeroSemantics(t *testing.T) {
	zeroPct := 0.0
	rep := &dipstick.Report{
		SchemaVersion: dipstick.SchemaVersion,
		GeneratedAt:   time.Now(),
		Providers: []dipstick.ProviderReport{
			{
				Provider: dipstick.ProviderClaude,
				Windows: []dipstick.RateWindow{
					{
						Label:       "zero-window",
						UsedPercent: &zeroPct,
					},
					{
						Label:       "nil-window",
						UsedPercent: nil,
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	colorFalse := false
	unicodeTrue := true
	err := RenderPretty(&buf, rep, RenderOptions{
		Width:   80,
		Color:   &colorFalse,
		Unicode: &unicodeTrue,
	})
	if err != nil {
		t.Fatalf("RenderPretty error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "░░░░░░░░░░    0%") {
		t.Errorf("expected legitimate 0%% bar for non-nil UsedPercent 0, got:\n%s", out)
	}
	if !strings.Contains(out, "— usage unavailable") {
		t.Errorf("expected explicit dash with unavailable reason for nil UsedPercent, got:\n%s", out)
	}
}

func TestRenderPretty_ColorThresholds(t *testing.T) {
	low := 15.0
	med := 75.0
	high := 95.0
	rep := &dipstick.Report{
		SchemaVersion: dipstick.SchemaVersion,
		GeneratedAt:   time.Now(),
		Providers: []dipstick.ProviderReport{
			{
				Provider: dipstick.ProviderClaude,
				Windows: []dipstick.RateWindow{
					{Label: "low", UsedPercent: &low},
					{Label: "med", UsedPercent: &med},
					{Label: "high", UsedPercent: &high},
				},
			},
		},
	}

	var buf bytes.Buffer
	colorTrue := true
	unicodeTrue := true
	err := RenderPretty(&buf, rep, RenderOptions{
		Width:   80,
		Color:   &colorTrue,
		Unicode: &unicodeTrue,
	})
	if err != nil {
		t.Fatalf("RenderPretty error: %v", err)
	}

	out := buf.String()
	// Check that ANSI color escapes are generated
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected ANSI color escapes in colored output, got:\n%s", out)
	}
}

func TestDetectRenderOptions_EnvPrecedence(t *testing.T) {
	origNoColor := os.Getenv("NO_COLOR")
	origCliColor := os.Getenv("CLICOLOR")
	origCliColorForce := os.Getenv("CLICOLOR_FORCE")
	origTerm := os.Getenv("TERM")
	origCI := os.Getenv("CI")
	origLang := os.Getenv("LANG")
	origLCAll := os.Getenv("LC_ALL")
	defer func() {
		_ = os.Setenv("NO_COLOR", origNoColor)
		_ = os.Setenv("CLICOLOR", origCliColor)
		_ = os.Setenv("CLICOLOR_FORCE", origCliColorForce)
		_ = os.Setenv("TERM", origTerm)
		_ = os.Setenv("CI", origCI)
		_ = os.Setenv("LANG", origLang)
		_ = os.Setenv("LC_ALL", origLCAll)
	}()

	t.Run("NO_COLOR disables color", func(t *testing.T) {
		_ = os.Setenv("NO_COLOR", "1")
		_ = os.Setenv("CLICOLOR_FORCE", "1")
		_ = os.Setenv("TERM", "xterm-256color")
		opts := detectRenderOptions(&bytes.Buffer{})
		if opts.Color == nil || *opts.Color != false {
			t.Errorf("expected Color to be false under NO_COLOR")
		}
	})

	t.Run("CLICOLOR=0 disables color", func(t *testing.T) {
		_ = os.Unsetenv("NO_COLOR")
		_ = os.Unsetenv("CLICOLOR_FORCE")
		_ = os.Setenv("CLICOLOR", "0")
		_ = os.Setenv("TERM", "xterm-256color")
		opts := detectRenderOptions(&bytes.Buffer{})
		if opts.Color == nil || *opts.Color != false {
			t.Errorf("expected Color to be false under CLICOLOR=0")
		}
	})

	t.Run("TERM=dumb disables color and unicode", func(t *testing.T) {
		_ = os.Unsetenv("NO_COLOR")
		_ = os.Unsetenv("CLICOLOR_FORCE")
		_ = os.Unsetenv("CLICOLOR")
		_ = os.Setenv("TERM", "dumb")
		opts := detectRenderOptions(&bytes.Buffer{})
		if opts.Color == nil || *opts.Color != false {
			t.Errorf("expected Color to be false under TERM=dumb")
		}
		if opts.Unicode == nil || *opts.Unicode != false {
			t.Errorf("expected Unicode to be false under TERM=dumb")
		}
	})

	t.Run("CI disables color unless forced", func(t *testing.T) {
		_ = os.Unsetenv("NO_COLOR")
		_ = os.Unsetenv("CLICOLOR_FORCE")
		_ = os.Unsetenv("CLICOLOR")
		_ = os.Setenv("TERM", "xterm-256color")
		_ = os.Setenv("CI", "true")
		opts := detectRenderOptions(&bytes.Buffer{})
		if opts.Color == nil || *opts.Color != false {
			t.Errorf("expected Color to be false under CI")
		}
	})

	t.Run("CI with CLICOLOR_FORCE=0 disables color", func(t *testing.T) {
		_ = os.Unsetenv("NO_COLOR")
		_ = os.Setenv("CLICOLOR_FORCE", "0")
		_ = os.Unsetenv("CLICOLOR")
		_ = os.Setenv("TERM", "xterm-256color")
		_ = os.Setenv("CI", "true")
		opts := detectRenderOptions(&bytes.Buffer{})
		if opts.Color == nil || *opts.Color != false {
			t.Errorf("expected Color to be false under CI with CLICOLOR_FORCE=0")
		}
	})

	t.Run("CLICOLOR_FORCE enables color in CI", func(t *testing.T) {
		_ = os.Unsetenv("NO_COLOR")
		_ = os.Setenv("CLICOLOR_FORCE", "1")
		_ = os.Setenv("TERM", "xterm-256color")
		_ = os.Setenv("CI", "true")
		opts := detectRenderOptions(&bytes.Buffer{})
		if opts.Color == nil || *opts.Color != true {
			t.Errorf("expected Color to be true under CLICOLOR_FORCE in CI")
		}
	})

	t.Run("LANG=C disables unicode", func(t *testing.T) {
		_ = os.Unsetenv("NO_COLOR")
		_ = os.Unsetenv("CLICOLOR_FORCE")
		_ = os.Setenv("TERM", "xterm-256color")
		_ = os.Unsetenv("LC_ALL")
		_ = os.Unsetenv("LC_CTYPE")
		_ = os.Setenv("LANG", "C")
		opts := detectRenderOptions(&bytes.Buffer{})
		if opts.Unicode == nil || *opts.Unicode != false {
			t.Errorf("expected Unicode to be false under LANG=C")
		}
	})

	t.Run("LANG=POSIX disables unicode", func(t *testing.T) {
		_ = os.Unsetenv("NO_COLOR")
		_ = os.Unsetenv("CLICOLOR_FORCE")
		_ = os.Setenv("TERM", "xterm-256color")
		_ = os.Unsetenv("LC_ALL")
		_ = os.Unsetenv("LC_CTYPE")
		_ = os.Setenv("LANG", "POSIX")
		opts := detectRenderOptions(&bytes.Buffer{})
		if opts.Unicode == nil || *opts.Unicode != false {
			t.Errorf("expected Unicode to be false under LANG=POSIX")
		}
	})

	t.Run("LC_CTYPE=C disables unicode", func(t *testing.T) {
		_ = os.Unsetenv("NO_COLOR")
		_ = os.Unsetenv("CLICOLOR_FORCE")
		_ = os.Setenv("TERM", "xterm-256color")
		_ = os.Unsetenv("LC_ALL")
		_ = os.Setenv("LC_CTYPE", "C")
		_ = os.Unsetenv("LANG")
		opts := detectRenderOptions(&bytes.Buffer{})
		if opts.Unicode == nil || *opts.Unicode != false {
			t.Errorf("expected Unicode to be false under LC_CTYPE=C")
		}
	})
}

func TestRun_PrettyFlag(t *testing.T) {
	origCollect := collectFn
	defer func() { collectFn = origCollect }()

	goldenRep := loadReportFixture(t, filepath.Join("testdata", "report_full.golden.json"))
	collectFn = func(ctx context.Context, opts ...dipstick.Option) (*dipstick.Report, error) {
		return goldenRep, nil
	}

	t.Run("--pretty outputs styled text", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--pretty"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
		}
		out := stdout.String()
		if !strings.Contains(out, "claude") || !strings.Contains(out, "session") {
			t.Errorf("expected pretty output on stdout, got:\n%s", out)
		}
		// Stderr should be empty
		if stderr.Len() > 0 {
			t.Errorf("expected empty stderr, got: %s", stderr.String())
		}
	})

	t.Run("both --json and --pretty is an error", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--json", "--pretty"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 on conflicting flags, got %d", code)
		}
		if !strings.Contains(stderr.String(), "cannot specify both --json and --pretty") {
			t.Errorf("expected conflicting flag error on stderr, got: %s", stderr.String())
		}
	})
}

func TestLibraryDependencyIsolation(t *testing.T) {
	// Ensure root dipstick package does not import lipgloss or term
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, "../..", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("failed parsing root dipstick package: %v", err)
	}

	for pkgName, pkg := range pkgs {
		if strings.HasSuffix(pkgName, "_test") {
			continue
		}
		for filePath, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if strings.Contains(path, "lipgloss") || strings.Contains(path, "golang.org/x/term") {
					t.Errorf("root library file %s imports restricted dependency %q", filePath, path)
				}
			}
		}
	}
}

func TestRenderPretty_EmptyReport(t *testing.T) {
	rep := &dipstick.Report{
		SchemaVersion: dipstick.SchemaVersion,
		GeneratedAt:   time.Now(),
		Providers:     nil,
		Errors:        nil,
	}

	var buf bytes.Buffer
	err := RenderPretty(&buf, rep, RenderOptions{})
	if err != nil {
		t.Fatalf("RenderPretty failed: %v", err)
	}
	if !strings.Contains(buf.String(), "No providers reported usage.") {
		t.Errorf("expected empty provider message, got:\n%s", buf.String())
	}
}

func TestRenderPretty_NilReport(t *testing.T) {
	var buf bytes.Buffer
	err := RenderPretty(&buf, nil, RenderOptions{})
	if err != nil {
		t.Fatalf("expected nil error on nil report, got: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output on nil report, got: %s", buf.String())
	}
}

func TestFormatReset(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		resetsAt *time.Time
		expected string
	}{
		{
			name:     "nil resetsAt",
			resetsAt: nil,
			expected: "",
		},
		{
			name:     "past resetsAt",
			resetsAt: dipstick.Ptr(now.Add(-10 * time.Minute)),
			expected: "resets now",
		},
		{
			name:     "exact now resetsAt",
			resetsAt: dipstick.Ptr(now),
			expected: "resets now",
		},
		{
			name:     "seconds away",
			resetsAt: dipstick.Ptr(now.Add(45 * time.Second)),
			expected: "resets in 45s",
		},
		{
			name:     "minutes away",
			resetsAt: dipstick.Ptr(now.Add(15 * time.Minute)),
			expected: "resets in 15m",
		},
		{
			name:     "hours and minutes away",
			resetsAt: dipstick.Ptr(now.Add(2*time.Hour + 14*time.Minute)),
			expected: "resets in 2h 14m",
		},
		{
			name:     "exact hours away",
			resetsAt: dipstick.Ptr(now.Add(5 * time.Hour)),
			expected: "resets in 5h",
		},
		{
			name:     "more than 24 hours away",
			resetsAt: dipstick.Ptr(time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)),
			expected: "resets Tue 09:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatReset(tt.resetsAt, now)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestFormatTokensAndCount(t *testing.T) {
	t.Run("formatTokens", func(t *testing.T) {
		if formatTokens(nil) != "" {
			t.Errorf("expected empty string for nil tokens")
		}

		tok := &dipstick.TokenUsage{
			InputTokens:  dipstick.Ptr(int64(125000)),
			OutputTokens: dipstick.Ptr(int64(34000)),
			TotalTokens:  dipstick.Ptr(int64(159000)),
		}
		got := formatTokens(tok)
		expected := "159k total (125k in / 34k out)"
		if got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}

		tokOnlyTotal := &dipstick.TokenUsage{
			TotalTokens: dipstick.Ptr(int64(2500000)),
		}
		gotTotal := formatTokens(tokOnlyTotal)
		if gotTotal != "2.5M total" {
			t.Errorf("expected 2.5M total, got %q", gotTotal)
		}

		tokInOut := &dipstick.TokenUsage{
			InputTokens:  dipstick.Ptr(int64(500)),
			OutputTokens: dipstick.Ptr(int64(200)),
		}
		gotInOut := formatTokens(tokInOut)
		if gotInOut != "500 in / 200 out" {
			t.Errorf("expected '500 in / 200 out', got %q", gotInOut)
		}

		tokOnlyIn := &dipstick.TokenUsage{
			InputTokens: dipstick.Ptr(int64(80000)),
		}
		gotOnlyIn := formatTokens(tokOnlyIn)
		if gotOnlyIn != "80k in" {
			t.Errorf("expected '80k in', got %q", gotOnlyIn)
		}

		tokTotalAndIn := &dipstick.TokenUsage{
			TotalTokens: dipstick.Ptr(int64(100000)),
			InputTokens: dipstick.Ptr(int64(80000)),
		}
		gotTotalAndIn := formatTokens(tokTotalAndIn)
		if gotTotalAndIn != "100k total (80k in)" {
			t.Errorf("expected '100k total (80k in)', got %q", gotTotalAndIn)
		}
	})

	t.Run("formatCount", func(t *testing.T) {
		if formatCount(500) != "500" {
			t.Errorf("expected 500, got %s", formatCount(500))
		}
		if formatCount(15000) != "15k" {
			t.Errorf("expected 15k, got %s", formatCount(15000))
		}
		if formatCount(1200000) != "1.2M" {
			t.Errorf("expected 1.2M, got %s", formatCount(1200000))
		}
	})
}
