package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mattwalters/dipstick/internal/localstate"
	"github.com/mattwalters/dipstick/internal/types"
)

var _ types.Source = (*TranscriptSource)(nil)

const (
	// DefaultMaxLineBufferSize is the maximum line length accepted during streaming scanning (10MB).
	DefaultMaxLineBufferSize = 10 * 1024 * 1024
)

// TranscriptStats captures scan diagnostics for testing and drift monitoring.
type TranscriptStats struct {
	FilesScanned int64 `json:"files_scanned"`
	LinesScanned int64 `json:"lines_scanned"`
	TurnsCounted int64 `json:"turns_counted"`
	LinesSkipped int64 `json:"lines_skipped"`
}

// TranscriptOption configures a TranscriptSource instance.
type TranscriptOption func(*TranscriptSource)

// WithProjectsDir overrides the base directory scanned for transcripts.
func WithProjectsDir(dir string) TranscriptOption {
	return func(s *TranscriptSource) {
		s.projectsDir = dir
	}
}

// WithSince sets the time filter threshold for transcript scanning.
func WithSince(t time.Time) TranscriptOption {
	return func(s *TranscriptSource) {
		s.since = t
	}
}

// WithResolver sets the localstate.Resolver used to discover Claude paths.
func WithResolver(r *localstate.Resolver) TranscriptOption {
	return func(s *TranscriptSource) {
		s.resolver = r
	}
}

// WithTranscriptNow sets the time provider function.
func WithTranscriptNow(fn func() time.Time) TranscriptOption {
	return func(s *TranscriptSource) {
		if fn != nil {
			s.now = fn
		}
	}
}

// WithMaxLineBuffer sets the maximum line buffer size in bytes.
func WithMaxLineBuffer(size int) TranscriptOption {
	return func(s *TranscriptSource) {
		if size > 0 {
			s.maxLineBufferSize = size
		}
	}
}

// TranscriptSource implements Tier 4 usage accounting via local transcript JSONL log scanning.
type TranscriptSource struct {
	projectsDir       string
	since             time.Time
	resolver          *localstate.Resolver
	now               func() time.Time
	maxLineBufferSize int

	mu        sync.RWMutex
	lastStats TranscriptStats
}

// NewTranscriptSource creates a new Tier 4 TranscriptSource.
func NewTranscriptSource(opts ...TranscriptOption) *TranscriptSource {
	s := &TranscriptSource{
		resolver:          localstate.New(),
		now:               time.Now,
		maxLineBufferSize: DefaultMaxLineBufferSize,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// ID returns the source identifier for Tier 4 Transcripts.
func (s *TranscriptSource) ID() types.SourceID {
	return types.SourceTranscript
}

// Tier returns the source robustness tier (Tier 4: Transcripts).
func (s *TranscriptSource) Tier() types.SourceTier {
	return types.TierTranscripts
}

// resolveProjectsDir returns the effective projects directory path.
func (s *TranscriptSource) resolveProjectsDir() string {
	if s.projectsDir != "" {
		return s.projectsDir
	}
	resolver := s.resolver
	if resolver == nil {
		resolver = localstate.New()
	}
	paths, err := resolver.ClaudePaths()
	if err != nil || paths == nil {
		return ""
	}
	return paths.ProjectsDir
}

// Available checks whether the Claude projects directory exists and is accessible.
func (s *TranscriptSource) Available(ctx context.Context) bool {
	dir := s.resolveProjectsDir()
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// Stats returns the latest scan statistics.
func (s *TranscriptSource) Stats() TranscriptStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastStats
}

// SkippedLines returns the number of unparseable/malformed lines skipped during the last scan.
func (s *TranscriptSource) SkippedLines() int64 {
	return s.Stats().LinesSkipped
}

type transcriptEntry struct {
	Type       string           `json:"type"`
	SessionID  string           `json:"sessionId"`
	Timestamp  string           `json:"timestamp"`
	RequestID  string           `json:"requestId"`
	UUID       string           `json:"uuid"`
	ParentUUID string           `json:"parentUuid"`
	AgentID    string           `json:"agentId"`
	Message    *transcriptMsg   `json:"message"`
	Usage      *transcriptUsage `json:"usage"`
}

type transcriptMsg struct {
	ID    string           `json:"id"`
	Model string           `json:"model"`
	Type  string           `json:"type"`
	Role  string           `json:"role"`
	Usage *transcriptUsage `json:"usage"`
}

type transcriptUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
}

type tokenTotals struct {
	input      int64
	output     int64
	cacheWrite int64
	cacheRead  int64
}

// Fetch scans all .jsonl transcript files in the projects directory and aggregates token usage.
func (s *TranscriptSource) Fetch(ctx context.Context) (*types.ProviderReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dir := s.resolveProjectsDir()
	if dir == "" {
		return nil, fmt.Errorf("%w: unable to resolve claude projects directory", types.ErrNotInstalled)
	}

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: claude projects directory %q does not exist", types.ErrNotInstalled, dir)
		}
		return nil, fmt.Errorf("%w: accessing claude projects directory %q: %v", types.ErrUpstreamError, dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: claude projects path %q is not a directory", types.ErrNotInstalled, dir)
	}

	var stats TranscriptStats
	var totals tokenTotals
	seenTurns := make(map[string]bool)

	usageNeedle := []byte(`"usage"`)

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip directories/files that cannot be read
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		// File modification time check if since filter is set
		if !s.since.IsZero() {
			if fInfo, infoErr := d.Info(); infoErr == nil {
				if fInfo.ModTime().Before(s.since) {
					return nil
				}
			}
		}

		stats.FilesScanned++
		return s.processFile(ctx, path, usageNeedle, seenTurns, &stats, &totals)
	})

	if walkErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: walking transcript directory: %v", types.ErrUpstreamError, walkErr)
	}

	s.mu.Lock()
	s.lastStats = stats
	s.mu.Unlock()

	now := time.Now()
	if s.now != nil {
		now = s.now()
	}

	totalTokens := totals.input + totals.output + totals.cacheWrite + totals.cacheRead
	tokenUsage := &types.TokenUsage{
		InputTokens:      types.Ptr(totals.input),
		OutputTokens:     types.Ptr(totals.output),
		CacheReadTokens:  types.Ptr(totals.cacheRead),
		CacheWriteTokens: types.Ptr(totals.cacheWrite),
		TotalTokens:      types.Ptr(totalTokens),
	}

	return &types.ProviderReport{
		Provider:   types.ProviderClaude,
		Source:     types.SourceTranscript,
		Confidence: types.ConfidenceDerived,
		Tokens:     tokenUsage,
		Windows:    nil,
		ObservedAt: now.UTC(),
	}, nil
}

func (s *TranscriptSource) processFile(
	ctx context.Context,
	path string,
	usageNeedle []byte,
	seenTurns map[string]bool,
	stats *TranscriptStats,
	totals *tokenTotals,
) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() {
		_ = f.Close()
	}()

	reader := bufio.NewReaderSize(f, 64*1024)
	var lineBuf bytes.Buffer
	lineTooLong := false

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		lineChunk, isPrefix, err := reader.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil
		}

		if lineTooLong {
			if !isPrefix {
				lineTooLong = false
			}
			continue
		}

		if lineBuf.Len()+len(lineChunk) > s.maxLineBufferSize {
			stats.LinesSkipped++
			lineBuf.Reset()
			if isPrefix {
				lineTooLong = true
			}
			continue
		}

		lineBuf.Write(lineChunk)

		if isPrefix {
			continue
		}

		rawLine := bytes.TrimSpace(lineBuf.Bytes())
		lineBuf.Reset()

		if len(rawLine) == 0 {
			continue
		}
		stats.LinesScanned++

		if bytes.Contains(rawLine, usageNeedle) {
			var entry transcriptEntry
			if err := json.Unmarshal(rawLine, &entry); err != nil {
				stats.LinesSkipped++
				continue
			}

			// Apply since filter
			if !s.since.IsZero() {
				if entry.Timestamp == "" {
					stats.LinesSkipped++
					continue
				}
				t, ok := parseTimestamp(entry.Timestamp)
				if !ok {
					stats.LinesSkipped++
					continue
				}
				if t.Before(s.since) {
					continue
				}
			}

			usage := entry.Usage
			if entry.Message != nil && entry.Message.Usage != nil {
				usage = entry.Message.Usage
			}

			if usage != nil {
				var in, out, cw, cr int64
				if usage.InputTokens != nil {
					in = *usage.InputTokens
				}
				if usage.OutputTokens != nil {
					out = *usage.OutputTokens
				}
				if usage.CacheCreationInputTokens != nil {
					cw = *usage.CacheCreationInputTokens
				}
				if usage.CacheReadInputTokens != nil {
					cr = *usage.CacheReadInputTokens
				}

				hasTokens := in > 0 || out > 0 || cw > 0 || cr > 0

				if hasTokens {
					sessID := entry.SessionID
					if sessID == "" {
						sessID = path
					}
					var dedupKey string
					if entry.Message != nil && entry.Message.ID != "" {
						dedupKey = sessID + ":" + entry.Message.ID
					} else if entry.RequestID != "" {
						dedupKey = sessID + ":" + entry.RequestID
					} else if entry.ParentUUID != "" {
						dedupKey = fmt.Sprintf("%s:%s:%d:%d:%d:%d", sessID, entry.ParentUUID, in, out, cw, cr)
					} else if entry.Timestamp != "" {
						dedupKey = fmt.Sprintf("%s:%s:%d:%d:%d:%d", sessID, entry.Timestamp, in, out, cw, cr)
					} else {
						dedupKey = fmt.Sprintf("%s:%d:%d:%d:%d", sessID, in, out, cw, cr)
					}

					if !seenTurns[dedupKey] {
						seenTurns[dedupKey] = true
						stats.TurnsCounted++

						totals.input += in
						totals.output += out
						totals.cacheWrite += cw
						totals.cacheRead += cr
					}
				}
			}
		} else {
			if !json.Valid(rawLine) {
				stats.LinesSkipped++
			}
		}
	}

	return nil
}

func parseTimestamp(ts string) (time.Time, bool) {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z07:00", ts); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse("2006-01-02T15:04:05", ts); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}
