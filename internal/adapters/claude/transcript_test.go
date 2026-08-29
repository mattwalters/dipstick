package claude_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattwalters/dipstick"
	"github.com/mattwalters/dipstick/internal/adapters/claude"
	"github.com/mattwalters/dipstick/internal/localstate"
)

func TestTranscriptSource_Interface(t *testing.T) {
	src := claude.NewTranscriptSource()
	if src == nil {
		t.Fatalf("expected non-nil TranscriptSource")
	}

	if src.ID() != dipstick.SourceTranscript {
		t.Errorf("expected ID %q, got %q", dipstick.SourceTranscript, src.ID())
	}

	if src.Tier() != dipstick.TierTranscripts {
		t.Errorf("expected Tier %v, got %v", dipstick.TierTranscripts, src.Tier())
	}
}

func TestTranscriptSource_Available(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	t.Run("returns true when projects directory exists", func(t *testing.T) {
		src := claude.NewTranscriptSource(
			claude.WithProjectsDir(tmpDir),
		)
		if !src.Available(ctx) {
			t.Errorf("expected Available() = true for existing directory")
		}
	})

	t.Run("returns false when projects directory does not exist", func(t *testing.T) {
		nonExistent := filepath.Join(tmpDir, "does-not-exist")
		src := claude.NewTranscriptSource(
			claude.WithProjectsDir(nonExistent),
		)
		if src.Available(ctx) {
			t.Errorf("expected Available() = false for missing directory")
		}
	})

	t.Run("returns false when path is a file not directory", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "file.txt")
		if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
			t.Fatalf("failed creating file: %v", err)
		}
		src := claude.NewTranscriptSource(
			claude.WithProjectsDir(filePath),
		)
		if src.Available(ctx) {
			t.Errorf("expected Available() = false for file path")
		}
	})

	t.Run("uses resolver when projects dir not explicitly set", func(t *testing.T) {
		claudeDir := filepath.Join(tmpDir, ".claude")
		projDir := filepath.Join(claudeDir, "projects")
		if err := os.MkdirAll(projDir, 0o755); err != nil {
			t.Fatalf("failed creating projects dir: %v", err)
		}

		resolver := localstate.New(localstate.WithHomeDir(tmpDir))
		src := claude.NewTranscriptSource(
			claude.WithResolver(resolver),
		)
		if !src.Available(ctx) {
			t.Errorf("expected Available() = true via resolver")
		}
	})
}

func TestTranscriptSource_Fetch_ExactAccounting(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	fixturesDir := filepath.Join("testdata", "transcripts")

	src := claude.NewTranscriptSource(
		claude.WithProjectsDir(fixturesDir),
		claude.WithTranscriptNow(func() time.Time { return now }),
	)

	report, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if report == nil {
		t.Fatalf("expected non-nil ProviderReport")
	}

	if report.Provider != dipstick.ProviderClaude {
		t.Errorf("expected Provider %q, got %q", dipstick.ProviderClaude, report.Provider)
	}

	if report.Source != dipstick.SourceTranscript {
		t.Errorf("expected Source %q, got %q", dipstick.SourceTranscript, report.Source)
	}

	if report.Confidence != dipstick.ConfidenceDerived {
		t.Errorf("expected Confidence %q, got %q", dipstick.ConfidenceDerived, report.Confidence)
	}

	if len(report.Windows) != 0 {
		t.Errorf("expected Windows to be empty, got %d windows", len(report.Windows))
	}

	if report.Tokens == nil {
		t.Fatalf("expected Tokens to be non-nil")
	}

	// Known totals from testdata/transcripts:
	// session-1.jsonl:
	//   turn 1: in 100, out 50, cw 20, cr 30
	//   turn 2 (deduped): in 200, out 80, cw 40, cr 60
	//   turn 3: in 500, out 100, cw 0, cr 0
	// agent-sub1.jsonl:
	//   turn 1: in 300, out 70, cw 10, cr 40
	// session-2.jsonl:
	//   turn 1: in 150, out 25, cw 5, cr 15
	//
	// Sums:
	// Input:       100 + 200 + 500 + 300 + 150 = 1250
	// Output:      50 + 80 + 100 + 70 + 25 = 325
	// CacheWrite:  20 + 40 + 0 + 10 + 5 = 75
	// CacheRead:   30 + 60 + 0 + 40 + 15 = 145
	// Total:       1250 + 325 + 75 + 145 = 1795

	expectedIn := int64(1250)
	expectedOut := int64(325)
	expectedCW := int64(75)
	expectedCR := int64(145)
	expectedTotal := int64(1795)

	if report.Tokens.InputTokens == nil || *report.Tokens.InputTokens != expectedIn {
		t.Errorf("InputTokens: want %d, got %v", expectedIn, report.Tokens.InputTokens)
	}
	if report.Tokens.OutputTokens == nil || *report.Tokens.OutputTokens != expectedOut {
		t.Errorf("OutputTokens: want %d, got %v", expectedOut, report.Tokens.OutputTokens)
	}
	if report.Tokens.CacheWriteTokens == nil || *report.Tokens.CacheWriteTokens != expectedCW {
		t.Errorf("CacheWriteTokens: want %d, got %v", expectedCW, report.Tokens.CacheWriteTokens)
	}
	if report.Tokens.CacheReadTokens == nil || *report.Tokens.CacheReadTokens != expectedCR {
		t.Errorf("CacheReadTokens: want %d, got %v", expectedCR, report.Tokens.CacheReadTokens)
	}
	if report.Tokens.TotalTokens == nil || *report.Tokens.TotalTokens != expectedTotal {
		t.Errorf("TotalTokens: want %d, got %v", expectedTotal, report.Tokens.TotalTokens)
	}

	stats := src.Stats()
	if stats.FilesScanned != 3 {
		t.Errorf("FilesScanned: want 3, got %d", stats.FilesScanned)
	}
	if stats.TurnsCounted != 5 {
		t.Errorf("TurnsCounted: want 5, got %d", stats.TurnsCounted)
	}
	if stats.LinesSkipped != 2 {
		t.Errorf("LinesSkipped: want 2, got %d", stats.LinesSkipped)
	}
	if src.SkippedLines() != 2 {
		t.Errorf("SkippedLines(): want 2, got %d", src.SkippedLines())
	}
}

func TestTranscriptSource_Fetch_TimeFiltering(t *testing.T) {
	ctx := context.Background()
	fixturesDir := filepath.Join("testdata", "transcripts")

	// Filter since 2026-08-15 -> excludes turn 3 from session-1 (timestamp 2026-08-10)
	since := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	src := claude.NewTranscriptSource(
		claude.WithProjectsDir(fixturesDir),
		claude.WithSince(since),
	)

	report, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	expectedIn := int64(750)
	expectedOut := int64(225)
	expectedCW := int64(75)
	expectedCR := int64(145)
	expectedTotal := int64(1195)

	if report.Tokens.InputTokens == nil || *report.Tokens.InputTokens != expectedIn {
		t.Errorf("InputTokens with since: want %d, got %v", expectedIn, report.Tokens.InputTokens)
	}
	if report.Tokens.OutputTokens == nil || *report.Tokens.OutputTokens != expectedOut {
		t.Errorf("OutputTokens with since: want %d, got %v", expectedOut, report.Tokens.OutputTokens)
	}
	if report.Tokens.CacheWriteTokens == nil || *report.Tokens.CacheWriteTokens != expectedCW {
		t.Errorf("CacheWriteTokens with since: want %d, got %v", expectedCW, report.Tokens.CacheWriteTokens)
	}
	if report.Tokens.CacheReadTokens == nil || *report.Tokens.CacheReadTokens != expectedCR {
		t.Errorf("CacheReadTokens with since: want %d, got %v", expectedCR, report.Tokens.CacheReadTokens)
	}
	if report.Tokens.TotalTokens == nil || *report.Tokens.TotalTokens != expectedTotal {
		t.Errorf("TotalTokens with since: want %d, got %v", expectedTotal, report.Tokens.TotalTokens)
	}
}

func TestTranscriptSource_Fetch_FileModTimeSkipping(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	oldFile := filepath.Join(tmpDir, "old-session.jsonl")
	content := `{"type":"assistant","timestamp":"2026-08-01T10:00:00Z","sessionId":"old","message":{"id":"m1","usage":{"input_tokens":500,"output_tokens":100}}}` + "\n"
	if err := os.WriteFile(oldFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed writing old file: %v", err)
	}

	// Set file modification time to past
	pastTime := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldFile, pastTime, pastTime); err != nil {
		t.Fatalf("failed setting file mtime: %v", err)
	}

	since := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	src := claude.NewTranscriptSource(
		claude.WithProjectsDir(tmpDir),
		claude.WithSince(since),
	)

	report, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if report.Tokens == nil || *report.Tokens.TotalTokens != 0 {
		t.Errorf("expected 0 tokens when file mtime before since, got %v", report.Tokens)
	}

	if src.Stats().FilesScanned != 0 {
		t.Errorf("expected 0 files scanned when mtime filtered out, got %d", src.Stats().FilesScanned)
	}
}

func TestTranscriptSource_Fetch_EmptyDirectory(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	src := claude.NewTranscriptSource(
		claude.WithProjectsDir(tmpDir),
	)

	report, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if report.Tokens == nil {
		t.Fatalf("expected non-nil Tokens for empty dir")
	}

	if *report.Tokens.TotalTokens != 0 {
		t.Errorf("expected TotalTokens = 0, got %d", *report.Tokens.TotalTokens)
	}
	if *report.Tokens.InputTokens != 0 {
		t.Errorf("expected InputTokens = 0, got %d", *report.Tokens.InputTokens)
	}
}

func TestTranscriptSource_Fetch_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	src := claude.NewTranscriptSource(
		claude.WithProjectsDir(filepath.Join("testdata", "transcripts")),
	)

	_, err := src.Fetch(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

func BenchmarkTranscriptScan(b *testing.B) {
	tmpDir := b.TempDir()

	// Generate realistic synthetic transcript tree:
	// 50 project directories, each with 3 session files, each with 20 turns
	for p := 0; p < 50; p++ {
		pDir := filepath.Join(tmpDir, fmt.Sprintf("project-%03d", p))
		if err := os.MkdirAll(pDir, 0o755); err != nil {
			b.Fatalf("failed creating dir: %v", err)
		}
		for s := 0; s < 3; s++ {
			sessFile := filepath.Join(pDir, fmt.Sprintf("session-%03d.jsonl", s))
			var content string
			for turn := 0; turn < 20; turn++ {
				// Each turn has thinking block and tool_use block
				msgID := fmt.Sprintf("msg-p%d-s%d-t%d", p, s, turn)
				content += fmt.Sprintf(`{"type":"assistant","timestamp":"2026-08-20T10:00:00Z","sessionId":"sess-%d-%d","message":{"id":%q,"model":"claude-3-7-sonnet","role":"assistant","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":10,"cache_read_input_tokens":20}}}`+"\n", p, s, msgID)
				content += fmt.Sprintf(`{"type":"assistant","timestamp":"2026-08-20T10:00:01Z","sessionId":"sess-%d-%d","message":{"id":%q,"model":"claude-3-7-sonnet","role":"assistant","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":10,"cache_read_input_tokens":20}}}`+"\n", p, s, msgID)
			}
			if err := os.WriteFile(sessFile, []byte(content), 0o644); err != nil {
				b.Fatalf("failed writing session file: %v", err)
			}
		}
	}

	src := claude.NewTranscriptSource(
		claude.WithProjectsDir(tmpDir),
	)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report, err := src.Fetch(ctx)
		if err != nil {
			b.Fatalf("Fetch failed in benchmark: %v", err)
		}
		if report == nil || report.Tokens == nil {
			b.Fatalf("invalid report in benchmark")
		}
	}
}
