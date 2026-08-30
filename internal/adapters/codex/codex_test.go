package codex_test

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	_ "modernc.org/sqlite"

	"github.com/mattwalters/dipstick"
	"github.com/mattwalters/dipstick/internal/adapters/codex"
	"github.com/mattwalters/dipstick/internal/localstate"
)

func makeTestJWT(header map[string]any, payload map[string]any) string {
	hBytes, _ := json.Marshal(header)
	pBytes, _ := json.Marshal(payload)
	hB64 := base64.RawURLEncoding.EncodeToString(hBytes)
	pB64 := base64.RawURLEncoding.EncodeToString(pBytes)
	sigB64 := base64.RawURLEncoding.EncodeToString([]byte("signature-bytes"))
	return fmt.Sprintf("%s.%s.%s", hB64, pB64, sigB64)
}

type mockPipeTransport struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (m *mockPipeTransport) Read(p []byte) (int, error) {
	return m.reader.Read(p)
}

func (m *mockPipeTransport) Write(p []byte) (int, error) {
	return m.writer.Write(p)
}

func (m *mockPipeTransport) Close() error {
	_ = m.reader.Close()
	return m.writer.Close()
}

type mockStderrTransport struct {
	*mockPipeTransport
	stderrText string
}

func (m *mockStderrTransport) Stderr() string {
	return m.stderrText
}

func createMockAppServerRunner(handler func(reqMethod string, reqID int64, params json.RawMessage) (any, *mockRPCError)) codex.AppServerRunner {
	return codex.AppServerRunnerFunc(func(ctx context.Context) (io.ReadWriteCloser, error) {
		clientReader, serverWriter := io.Pipe()
		serverReader, clientWriter := io.Pipe()

		go func() {
			defer func() {
				_ = serverReader.Close()
				_ = serverWriter.Close()
			}()

			scanner := bufio.NewScanner(serverReader)
			for scanner.Scan() {
				line := scanner.Bytes()
				if len(line) == 0 {
					continue
				}

				var req struct {
					JSONRPC string          `json:"jsonrpc"`
					ID      int64           `json:"id"`
					Method  string          `json:"method"`
					Params  json.RawMessage `json:"params"`
				}

				if err := json.Unmarshal(line, &req); err != nil {
					continue
				}

				result, rpcErr := handler(req.Method, req.ID, req.Params)
				var resp map[string]any
				if rpcErr != nil {
					resp = map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"error": map[string]any{
							"code":    rpcErr.Code,
							"message": rpcErr.Message,
						},
					}
				} else {
					resp = map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"result":  result,
					}
				}

				respBytes, err := json.Marshal(resp)
				if err != nil {
					return
				}
				respBytes = append(respBytes, '\n')
				if _, err := serverWriter.Write(respBytes); err != nil {
					return
				}
			}
		}()

		return &mockPipeTransport{
			reader: clientReader,
			writer: clientWriter,
		}, nil
	})
}

type mockRPCError struct {
	Code    int
	Message string
}

func createTestStateDB(t *testing.T, dbPath string, tokens []int64) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open test sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	_, err = db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		rollout_path TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		source TEXT NOT NULL,
		model_provider TEXT NOT NULL,
		cwd TEXT NOT NULL,
		title TEXT NOT NULL,
		sandbox_policy TEXT NOT NULL,
		approval_mode TEXT NOT NULL,
		tokens_used INTEGER NOT NULL DEFAULT 0
	);`)
	if err != nil {
		t.Fatalf("failed creating schema: %v", err)
	}

	for i, tok := range tokens {
		_, err := db.Exec(`INSERT INTO threads (id, rollout_path, created_at, updated_at, source, model_provider, cwd, title, sandbox_policy, approval_mode, tokens_used)
			VALUES (?, 'path', 1000, 1000, 'src', 'codex', '/dir', 'title', 'auto', 'auto', ?);`,
			fmt.Sprintf("thread-%d", i), tok)
		if err != nil {
			t.Fatalf("failed inserting row: %v", err)
		}
	}
}

func TestAdapter_IDAndName(t *testing.T) {
	a := codex.New()
	if a == nil {
		t.Fatalf("expected non-nil adapter")
	}
	if a.ID() != dipstick.ProviderCodex {
		t.Errorf("expected ID %q, got %q", dipstick.ProviderCodex, a.ID())
	}
	if a.Name() != "codex" {
		t.Errorf("expected name %q, got %q", "codex", a.Name())
	}
	sources := a.Sources()
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if sources[0].ID() != dipstick.SourceAppServer {
		t.Errorf("expected source 0 ID %q, got %q", dipstick.SourceAppServer, sources[0].ID())
	}
	if sources[0].Tier() != dipstick.TierLocalRPC {
		t.Errorf("expected source 0 tier %v, got %v", dipstick.TierLocalRPC, sources[0].Tier())
	}
	if sources[1].ID() != dipstick.SourceLocalState {
		t.Errorf("expected source 1 ID %q, got %q", dipstick.SourceLocalState, sources[1].ID())
	}
	if sources[1].Tier() != dipstick.TierLocalState {
		t.Errorf("expected source 1 tier %v, got %v", dipstick.TierLocalState, sources[1].Tier())
	}
}

func TestAppServerSource_FullSuccess(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	runner := createMockAppServerRunner(func(reqMethod string, reqID int64, params json.RawMessage) (any, *mockRPCError) {
		switch reqMethod {
		case "initialize":
			return map[string]any{
				"userAgent":      "dipstick/0.148.0",
				"codexHome":      "/Users/test/.codex",
				"platformFamily": "unix",
				"platformOs":     "macos",
			}, nil
		case "account/rateLimits/read":
			return map[string]any{
				"rateLimits": map[string]any{
					"limitId": "codex",
					"primary": map[string]any{
						"usedPercent":        15.5,
						"windowDurationMins": 300,
						"resetsAt":           now.Add(4 * time.Hour).Unix(),
					},
					"secondary": map[string]any{
						"usedPercent":        2.0,
						"windowDurationMins": 10080,
						"resetsAt":           now.Add(6 * 24 * time.Hour).Unix(),
					},
					"credits": map[string]any{
						"hasCredits": false,
						"unlimited":  false,
						"balance":    "0",
					},
					"planType": "plus",
				},
			}, nil
		case "account/usage/read":
			return map[string]any{
				"summary": map[string]any{
					"lifetimeTokens":        44535901,
					"peakDailyTokens":       19503139,
					"longestRunningTurnSec": 622,
					"currentStreakDays":     0,
					"longestStreakDays":     3,
				},
				"dailyUsageBuckets": []map[string]any{
					{"startDate": "2026-08-24", "tokens": 19503139},
				},
			}, nil
		case "account/read":
			return map[string]any{
				"account": map[string]any{
					"type":     "chatgpt",
					"email":    "developer@example.com",
					"planType": "plus",
				},
				"requiresOpenaiAuth": true,
			}, nil
		default:
			return nil, &mockRPCError{Code: -32601, Message: "method not found"}
		}
	})

	adapter := codex.New(
		codex.WithAppServerRunner(runner),
		codex.WithNow(func() time.Time { return now }),
	)

	src := adapter.Sources()[0]
	if !src.Available(context.Background()) {
		t.Fatalf("expected appServerSource to be available with injected runner")
	}

	report, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if report.Provider != dipstick.ProviderCodex {
		t.Errorf("expected provider codex, got %s", report.Provider)
	}
	if report.Source != dipstick.SourceAppServer {
		t.Errorf("expected source app_server, got %s", report.Source)
	}
	if report.Tier != dipstick.TierLocalRPC {
		t.Errorf("expected tier local_rpc, got %v", report.Tier)
	}
	if report.Confidence != dipstick.ConfidenceExact {
		t.Errorf("expected confidence exact, got %s", report.Confidence)
	}

	if report.Identity == nil {
		t.Fatalf("expected non-nil Identity")
	}
	if report.Identity.Email != "developer@example.com" {
		t.Errorf("expected email developer@example.com, got %q", report.Identity.Email)
	}
	if report.Identity.Plan != "plus" {
		t.Errorf("expected plan plus, got %q", report.Identity.Plan)
	}

	if report.Tokens == nil || report.Tokens.TotalTokens == nil {
		t.Fatalf("expected non-nil TotalTokens")
	}
	if *report.Tokens.TotalTokens != 44535901 {
		t.Errorf("expected 44535901 total tokens, got %d", *report.Tokens.TotalTokens)
	}

	if len(report.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(report.Windows))
	}

	// Window 0: Primary 5h
	w0 := report.Windows[0]
	if w0.Label != "primary" {
		t.Errorf("window 0: expected label primary, got %q", w0.Label)
	}
	if w0.UsedPercent == nil || *w0.UsedPercent != 15.5 {
		t.Errorf("window 0: expected used percent 15.5, got %v", w0.UsedPercent)
	}
	if w0.WindowDurationSeconds == nil || *w0.WindowDurationSeconds != 18000 {
		t.Errorf("window 0: expected duration 18000s, got %v", w0.WindowDurationSeconds)
	}
	expectedReset0 := now.Add(4 * time.Hour)
	if w0.ResetsAt == nil || !w0.ResetsAt.Equal(expectedReset0) {
		t.Errorf("window 0: expected resetsAt %v, got %v", expectedReset0, w0.ResetsAt)
	}

	// Window 1: Secondary weekly
	w1 := report.Windows[1]
	if w1.Label != "secondary" {
		t.Errorf("window 1: expected label secondary, got %q", w1.Label)
	}
	if w1.UsedPercent == nil || *w1.UsedPercent != 2.0 {
		t.Errorf("window 1: expected used percent 2.0, got %v", w1.UsedPercent)
	}
	if w1.WindowDurationSeconds == nil || *w1.WindowDurationSeconds != 604800 {
		t.Errorf("window 1: expected duration 604800s, got %v", w1.WindowDurationSeconds)
	}
}

func TestAppServerSource_ErrorHandling(t *testing.T) {
	t.Run("handshake initialize error", func(t *testing.T) {
		runner := createMockAppServerRunner(func(reqMethod string, reqID int64, params json.RawMessage) (any, *mockRPCError) {
			return nil, &mockRPCError{Code: -32000, Message: "daemon initialization failed"}
		})

		adapter := codex.New(codex.WithAppServerRunner(runner))
		src := adapter.Sources()[0]

		_, err := src.Fetch(context.Background())
		if err == nil {
			t.Fatalf("expected error from failed initialize")
		}
		if !errors.Is(err, dipstick.ErrUpstreamError) {
			t.Errorf("expected ErrUpstreamError, got %v", err)
		}
	})

	t.Run("start error", func(t *testing.T) {
		runner := codex.AppServerRunnerFunc(func(ctx context.Context) (io.ReadWriteCloser, error) {
			return nil, fmt.Errorf("process spawn failed")
		})

		adapter := codex.New(codex.WithAppServerRunner(runner))
		src := adapter.Sources()[0]

		_, err := src.Fetch(context.Background())
		if err == nil {
			t.Fatalf("expected error on start failure")
		}
		if !errors.Is(err, dipstick.ErrUpstreamError) {
			t.Errorf("expected ErrUpstreamError, got %v", err)
		}
	})

	t.Run("timeout on query", func(t *testing.T) {
		runner := codex.AppServerRunnerFunc(func(ctx context.Context) (io.ReadWriteCloser, error) {
			clientReader, _ := io.Pipe()
			serverReader, clientWriter := io.Pipe()
			go func() {
				_, _ = io.Copy(io.Discard, serverReader)
			}()
			return &mockPipeTransport{
				reader: clientReader,
				writer: clientWriter,
			}, nil
		})

		adapter := codex.New(
			codex.WithAppServerRunner(runner),
			codex.WithAppServerTimeout(20*time.Millisecond),
		)
		src := adapter.Sources()[0]

		_, err := src.Fetch(context.Background())
		if err == nil {
			t.Fatalf("expected error on timeout")
		}
		if !errors.Is(err, dipstick.ErrSourceTimeout) {
			t.Errorf("expected ErrSourceTimeout, got %v", err)
		}
	})

	t.Run("error with stderr diagnostics", func(t *testing.T) {
		runner := codex.AppServerRunnerFunc(func(ctx context.Context) (io.ReadWriteCloser, error) {
			clientReader, serverWriter := io.Pipe()
			serverReader, clientWriter := io.Pipe()
			go func() {
				_ = serverReader.Close()
				_ = serverWriter.Close()
			}()
			return &mockStderrTransport{
				mockPipeTransport: &mockPipeTransport{
					reader: clientReader,
					writer: clientWriter,
				},
				stderrText: "fatal: failed to open database /path/state_5.sqlite: permission denied",
			}, nil
		})

		adapter := codex.New(codex.WithAppServerRunner(runner))
		src := adapter.Sources()[0]

		_, err := src.Fetch(context.Background())
		if err == nil {
			t.Fatalf("expected error from closed transport")
		}
		if !errors.Is(err, dipstick.ErrUpstreamError) {
			t.Errorf("expected ErrUpstreamError, got %v", err)
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("expected stderr diagnostics in error message, got: %v", err)
		}
	})

	t.Run("plan populated when email omitted", func(t *testing.T) {
		runner := createMockAppServerRunner(func(reqMethod string, reqID int64, params json.RawMessage) (any, *mockRPCError) {
			switch reqMethod {
			case "initialize":
				return map[string]any{"userAgent": "dipstick/test"}, nil
			case "account/rateLimits/read":
				return map[string]any{"rateLimits": map[string]any{"limitId": "codex"}}, nil
			case "account/usage/read":
				return map[string]any{"summary": map[string]any{"lifetimeTokens": 100}}, nil
			case "account/read":
				return map[string]any{
					"account": map[string]any{
						"type":     "chatgpt",
						"email":    "",
						"planType": "team",
					},
				}, nil
			default:
				return nil, &mockRPCError{Code: -32601, Message: "not found"}
			}
		})

		adapter := codex.New(codex.WithAppServerRunner(runner))
		src := adapter.Sources()[0]

		report, err := src.Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if report.Identity == nil {
			t.Fatalf("expected non-nil Identity when planType is present")
		}
		if report.Identity.Plan != "team" {
			t.Errorf("expected plan team, got %q", report.Identity.Plan)
		}
		if report.Identity.Email != "" {
			t.Errorf("expected empty email, got %q", report.Identity.Email)
		}
	})

	t.Run("empty identity omitted as nil", func(t *testing.T) {
		runner := createMockAppServerRunner(func(reqMethod string, reqID int64, params json.RawMessage) (any, *mockRPCError) {
			switch reqMethod {
			case "initialize":
				return map[string]any{"userAgent": "dipstick/test"}, nil
			case "account/rateLimits/read":
				return map[string]any{"rateLimits": map[string]any{"limitId": "codex"}}, nil
			case "account/usage/read":
				return map[string]any{"summary": map[string]any{"lifetimeTokens": 100}}, nil
			case "account/read":
				return map[string]any{
					"account": map[string]any{
						"type":     "chatgpt",
						"email":    "",
						"planType": "",
					},
				}, nil
			default:
				return nil, &mockRPCError{Code: -32601, Message: "not found"}
			}
		})

		adapter := codex.New(codex.WithAppServerRunner(runner))
		src := adapter.Sources()[0]

		report, err := src.Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if report.Identity != nil {
			t.Errorf("expected nil Identity when all identity fields are empty, got %+v", report.Identity)
		}
	})
}

func TestLocalStateSource_SQLiteTokenAccounting(t *testing.T) {
	fixturePath := filepath.Join("testdata", "auth_subscription.json")
	authData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	t.Run("valid sqlite database calculates cumulative tokens", func(t *testing.T) {
		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		if err := os.MkdirAll(codexDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), authData, 0o600); err != nil {
			t.Fatal(err)
		}

		dbPath := filepath.Join(codexDir, "state_5.sqlite")
		createTestStateDB(t, dbPath, []int64{1000, 2500, 500, 10000})

		resolver := localstate.New(
			localstate.WithHomeDir(tmpDir),
			localstate.WithEnvMap(map[string]string{}),
		)
		adapter := codex.New(codex.WithResolver(resolver))

		localSrc := adapter.Sources()[1]
		report, err := localSrc.Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}

		if report.Tokens == nil || report.Tokens.TotalTokens == nil {
			t.Fatalf("expected non-nil TotalTokens from sqlite")
		}
		// 1000 + 2500 + 500 + 10000 = 14000
		if *report.Tokens.TotalTokens != 14000 {
			t.Errorf("expected 14000 total tokens, got %d", *report.Tokens.TotalTokens)
		}
		if report.Confidence != dipstick.ConfidenceDerived {
			t.Errorf("expected confidence derived, got %s", report.Confidence)
		}
	})

	t.Run("missing sqlite database returns nil tokens gracefully", func(t *testing.T) {
		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		if err := os.MkdirAll(codexDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), authData, 0o600); err != nil {
			t.Fatal(err)
		}

		resolver := localstate.New(
			localstate.WithHomeDir(tmpDir),
			localstate.WithEnvMap(map[string]string{}),
		)
		adapter := codex.New(codex.WithResolver(resolver))

		localSrc := adapter.Sources()[1]
		report, err := localSrc.Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}

		if report.Tokens != nil {
			t.Errorf("expected nil Tokens when sqlite file is absent, got %+v", report.Tokens)
		}
		if report.Identity.Email != "developer@example.com" {
			t.Errorf("expected valid identity, got %s", report.Identity.Email)
		}
	})

	t.Run("corrupt sqlite file handled gracefully without panic", func(t *testing.T) {
		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		if err := os.MkdirAll(codexDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), authData, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codexDir, "state_5.sqlite"), []byte("not a real sqlite database"), 0o600); err != nil {
			t.Fatal(err)
		}

		resolver := localstate.New(
			localstate.WithHomeDir(tmpDir),
			localstate.WithEnvMap(map[string]string{}),
		)
		adapter := codex.New(codex.WithResolver(resolver))

		localSrc := adapter.Sources()[1]
		report, err := localSrc.Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed on corrupt sqlite: %v", err)
		}

		if report.Tokens != nil {
			t.Errorf("expected nil Tokens when sqlite file is corrupt, got %+v", report.Tokens)
		}
	})
}

func TestLocalStateSource_SubscriptionFixture(t *testing.T) {
	fixturePath := filepath.Join("testdata", "auth_subscription.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("creating test dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600); err != nil {
		t.Fatalf("writing auth.json: %v", err)
	}

	resolver := localstate.New(
		localstate.WithHomeDir(tmpDir),
		localstate.WithEnvMap(map[string]string{}),
	)
	adapter := codex.New(codex.WithResolver(resolver))

	sources := adapter.Sources()
	src := sources[1] // LocalStateSource

	ctx := context.Background()
	if !src.Available(ctx) {
		t.Fatalf("expected source to be available with auth.json present")
	}

	report, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if report.Provider != dipstick.ProviderCodex {
		t.Errorf("expected provider %s, got %s", dipstick.ProviderCodex, report.Provider)
	}
	if report.Source != dipstick.SourceLocalState {
		t.Errorf("expected source %s, got %s", dipstick.SourceLocalState, report.Source)
	}
	if report.Confidence != dipstick.ConfidenceDerived {
		t.Errorf("expected confidence %s, got %s", dipstick.ConfidenceDerived, report.Confidence)
	}
	if report.Identity == nil {
		t.Fatalf("expected non-nil Identity")
	}
	if report.Identity.Email != "developer@example.com" {
		t.Errorf("expected email 'developer@example.com', got %q", report.Identity.Email)
	}
	if report.Identity.AccountID != "acc-chatgpt-12345" {
		t.Errorf("expected account_id 'acc-chatgpt-12345', got %q", report.Identity.AccountID)
	}
	if report.Identity.Plan != "pro" {
		t.Errorf("expected plan 'pro', got %q", report.Identity.Plan)
	}
	if report.Windows != nil {
		t.Errorf("expected Windows to be nil from Tier 2 source, got %+v", report.Windows)
	}
	if report.Tokens != nil {
		t.Errorf("expected Tokens to be nil from Tier 2 source without sqlite, got %+v", report.Tokens)
	}
}

func TestLocalStateSource_ApiKeyFixture(t *testing.T) {
	fixturePath := filepath.Join("testdata", "auth_api_key.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("creating test dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600); err != nil {
		t.Fatalf("writing auth.json: %v", err)
	}

	resolver := localstate.New(
		localstate.WithHomeDir(tmpDir),
		localstate.WithEnvMap(map[string]string{}),
	)
	adapter := codex.New(codex.WithResolver(resolver))

	src := adapter.Sources()[1]

	ctx := context.Background()
	if !src.Available(ctx) {
		t.Fatalf("expected source to be available")
	}

	report, err := src.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if report.Identity == nil {
		t.Fatalf("expected non-nil Identity")
	}
	if report.Identity.Plan != "api_key" {
		t.Errorf("expected plan 'api_key', got %q", report.Identity.Plan)
	}
	if report.Windows != nil {
		t.Errorf("expected Windows to be nil for API key mode, got %+v", report.Windows)
	}
	if report.Tokens != nil {
		t.Errorf("expected Tokens to be nil for API key mode, got %+v", report.Tokens)
	}
}

func TestLocalStateSource_PaddedJwtFixture(t *testing.T) {
	fixturePath := filepath.Join("testdata", "auth_padded_jwt.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("creating test dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600); err != nil {
		t.Fatalf("writing auth.json: %v", err)
	}

	resolver := localstate.New(
		localstate.WithHomeDir(tmpDir),
		localstate.WithEnvMap(map[string]string{}),
	)
	adapter := codex.New(codex.WithResolver(resolver))
	src := adapter.Sources()[1]

	report, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed on padded JWT fixture: %v", err)
	}

	if report.Identity == nil {
		t.Fatalf("expected non-nil Identity")
	}
	if report.Identity.Email != "padded@example.com" {
		t.Errorf("expected email 'padded@example.com', got %q", report.Identity.Email)
	}
	if report.Identity.AccountID != "acc-padded-999" {
		t.Errorf("expected account_id 'acc-padded-999', got %q", report.Identity.AccountID)
	}
	if report.Identity.Plan != "plus" {
		t.Errorf("expected plan 'plus', got %q", report.Identity.Plan)
	}
}

func TestLocalStateSource_MalformedAndCorruptFixtures(t *testing.T) {
	tests := []struct {
		name        string
		fixtureFile string
	}{
		{"malformed_jwt", "auth_malformed_jwt.json"},
		{"corrupt_json", "auth_corrupt.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixturePath := filepath.Join("testdata", tt.fixtureFile)
			data, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}

			tmpDir := t.TempDir()
			codexDir := filepath.Join(tmpDir, ".codex")
			if err := os.MkdirAll(codexDir, 0o700); err != nil {
				t.Fatalf("creating test dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600); err != nil {
				t.Fatalf("writing auth.json: %v", err)
			}

			resolver := localstate.New(
				localstate.WithHomeDir(tmpDir),
				localstate.WithEnvMap(map[string]string{}),
			)
			adapter := codex.New(codex.WithResolver(resolver))
			src := adapter.Sources()[1]

			report, err := src.Fetch(context.Background())
			if err == nil {
				t.Fatalf("expected error from %s, got report: %+v", tt.name, report)
			}
			if !errors.Is(err, dipstick.ErrParseFailed) {
				t.Errorf("expected ErrParseFailed, got: %v", err)
			}
		})
	}
}

func TestLocalStateSource_MissingAndEmptyAuthFile(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		resolver := localstate.New(
			localstate.WithHomeDir(tmpDir),
			localstate.WithEnvMap(map[string]string{}),
		)
		adapter := codex.New(codex.WithResolver(resolver))
		src := adapter.Sources()[1]

		if src.Available(context.Background()) {
			t.Errorf("expected Available to be false for missing auth.json")
		}

		_, err := src.Fetch(context.Background())
		if err == nil {
			t.Fatalf("expected error for missing auth.json, got nil")
		}
		if !errors.Is(err, dipstick.ErrNotInstalled) {
			t.Errorf("expected ErrNotInstalled for missing file, got %v", err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		_ = os.MkdirAll(codexDir, 0o700)
		_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte("   \n"), 0o600)

		resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
		adapter := codex.New(codex.WithResolver(resolver))
		src := adapter.Sources()[1]

		_, err := src.Fetch(context.Background())
		if err == nil || !errors.Is(err, dipstick.ErrParseFailed) {
			t.Errorf("expected ErrParseFailed for empty file, got: %v", err)
		}
	})

	t.Run("empty json object", func(t *testing.T) {
		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		_ = os.MkdirAll(codexDir, 0o700)
		_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte("{}"), 0o600)

		resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
		adapter := codex.New(codex.WithResolver(resolver))
		src := adapter.Sources()[1]

		_, err := src.Fetch(context.Background())
		if err == nil || !errors.Is(err, dipstick.ErrParseFailed) {
			t.Errorf("expected ErrParseFailed for empty JSON object, got: %v", err)
		}
	})
}

func TestLocalStateSource_EnvironmentOverrides(t *testing.T) {
	jwt := makeTestJWT(
		map[string]any{"alg": "RS256", "typ": "JWT"},
		map[string]any{
			"email": "envtest@example.com",
			"https://api.openai.com/auth": map[string]any{
				"chatgpt_account_id": "acc-env-123",
				"chatgpt_plan_type":  "team",
			},
		},
	)
	authData := fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":%q}}`, jwt)

	t.Run("CODEX_HOME override", func(t *testing.T) {
		customHome := t.TempDir()
		if err := os.WriteFile(filepath.Join(customHome, "auth.json"), []byte(authData), 0o600); err != nil {
			t.Fatal(err)
		}

		resolver := localstate.New(
			localstate.WithHomeDir("/other/home"),
			localstate.WithEnvMap(map[string]string{
				"CODEX_HOME": customHome,
			}),
		)
		adapter := codex.New(codex.WithResolver(resolver))
		src := adapter.Sources()[1]

		if !src.Available(context.Background()) {
			t.Fatalf("expected source available via CODEX_HOME")
		}

		rep, err := src.Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if rep.Identity.Email != "envtest@example.com" {
			t.Errorf("expected email envtest@example.com, got %s", rep.Identity.Email)
		}
		if rep.Identity.Plan != "team" {
			t.Errorf("expected plan team, got %s", rep.Identity.Plan)
		}
	})

	t.Run("CODEX_CONFIG_DIR fallback override", func(t *testing.T) {
		customConfig := t.TempDir()
		if err := os.WriteFile(filepath.Join(customConfig, "auth.json"), []byte(authData), 0o600); err != nil {
			t.Fatal(err)
		}

		resolver := localstate.New(
			localstate.WithHomeDir("/other/home"),
			localstate.WithEnvMap(map[string]string{
				"CODEX_CONFIG_DIR": customConfig,
			}),
		)
		adapter := codex.New(codex.WithResolver(resolver))
		src := adapter.Sources()[1]

		if !src.Available(context.Background()) {
			t.Fatalf("expected source available via CODEX_CONFIG_DIR")
		}

		rep, err := src.Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if rep.Identity.AccountID != "acc-env-123" {
			t.Errorf("expected account_id acc-env-123, got %s", rep.Identity.AccountID)
		}
	})
}

func TestLocalStateSource_JWTVariousPlansAndFallbacks(t *testing.T) {
	plans := []string{"pro", "plus", "free", "team", "enterprise"}

	for _, plan := range plans {
		t.Run("plan_"+plan, func(t *testing.T) {
			jwt := makeTestJWT(
				map[string]any{"alg": "RS256", "typ": "JWT"},
				map[string]any{
					"email": "user@example.com",
					"https://api.openai.com/auth": map[string]any{
						"chatgpt_account_id": "acc-plan-" + plan,
						"chatgpt_plan_type":  plan,
					},
				},
			)
			authData := fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":%q}}`, jwt)

			tmpDir := t.TempDir()
			codexDir := filepath.Join(tmpDir, ".codex")
			_ = os.MkdirAll(codexDir, 0o700)
			_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authData), 0o600)

			resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
			adapter := codex.New(codex.WithResolver(resolver))

			rep, err := adapter.Sources()[1].Fetch(context.Background())
			if err != nil {
				t.Fatalf("Fetch failed for plan %s: %v", plan, err)
			}
			if rep.Identity.Plan != plan {
				t.Errorf("expected plan %s, got %s", plan, rep.Identity.Plan)
			}
		})
	}

	t.Run("top-level claims fallback", func(t *testing.T) {
		jwt := makeTestJWT(
			map[string]any{"alg": "RS256", "typ": "JWT"},
			map[string]any{
				"email":              "fallback@example.com",
				"chatgpt_account_id": "acc-fallback",
				"chatgpt_plan_type":  "pro",
			},
		)
		authData := fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":%q}}`, jwt)

		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		_ = os.MkdirAll(codexDir, 0o700)
		_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authData), 0o600)

		resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
		adapter := codex.New(codex.WithResolver(resolver))

		rep, err := adapter.Sources()[1].Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if rep.Identity.Email != "fallback@example.com" {
			t.Errorf("expected fallback email, got %s", rep.Identity.Email)
		}
		if rep.Identity.AccountID != "acc-fallback" {
			t.Errorf("expected fallback account ID, got %s", rep.Identity.AccountID)
		}
		if rep.Identity.Plan != "pro" {
			t.Errorf("expected fallback plan pro, got %s", rep.Identity.Plan)
		}
	})

	t.Run("tokens.account_id fallback when omitted in JWT", func(t *testing.T) {
		jwt := makeTestJWT(
			map[string]any{"alg": "RS256", "typ": "JWT"},
			map[string]any{
				"email": "noacc@example.com",
				"https://api.openai.com/auth": map[string]any{
					"chatgpt_plan_type": "plus",
				},
			},
		)
		authData := fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":%q,"account_id":"acc-from-tokens"}}`, jwt)

		tmpDir := t.TempDir()
		codexDir := filepath.Join(tmpDir, ".codex")
		_ = os.MkdirAll(codexDir, 0o700)
		_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authData), 0o600)

		resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
		adapter := codex.New(codex.WithResolver(resolver))

		rep, err := adapter.Sources()[1].Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if rep.Identity.AccountID != "acc-from-tokens" {
			t.Errorf("expected account ID fallback to tokens.account_id, got %s", rep.Identity.AccountID)
		}
	})
}

func TestLocalStateSource_Detect(t *testing.T) {
	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	_ = os.MkdirAll(codexDir, 0o700)

	authData := `{"auth_mode":"api_key","OPENAI_API_KEY":"sk-test-12345"}`
	_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authData), 0o600)

	resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
	adapter := codex.New(codex.WithResolver(resolver))

	det, err := adapter.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if !det.Authenticated {
		t.Errorf("expected Authenticated to be true when valid auth.json exists")
	}
}

func TestAdapter_LadderResolutionFallback(t *testing.T) {
	fixturePath := filepath.Join("testdata", "auth_subscription.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	_ = os.MkdirAll(codexDir, 0o700)
	_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600)

	createTestStateDB(t, filepath.Join(codexDir, "state_5.sqlite"), []int64{5000})

	// AppServer fails with error
	failingRunner := codex.AppServerRunnerFunc(func(ctx context.Context) (io.ReadWriteCloser, error) {
		return nil, fmt.Errorf("app-server binary crashed")
	})

	resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
	adapter := codex.New(
		codex.WithResolver(resolver),
		codex.WithAppServerRunner(failingRunner),
	)

	report, err := dipstick.Collect(context.Background(),
		dipstick.WithProviders(dipstick.ProviderCodex),
		dipstick.WithAdapter(adapter),
	)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(report.Providers) != 1 {
		t.Fatalf("expected 1 provider report, got %d (errors: %+v)", len(report.Providers), report.Errors)
	}
	pr := report.Providers[0]
	if pr.Source != dipstick.SourceLocalState {
		t.Errorf("expected fallback to SourceLocalState, got %s", pr.Source)
	}
	if pr.Confidence != dipstick.ConfidenceDerived {
		t.Errorf("expected ConfidenceDerived, got %s", pr.Confidence)
	}
	if pr.Identity.Email != "developer@example.com" {
		t.Errorf("expected email developer@example.com, got %s", pr.Identity.Email)
	}
	if pr.Tokens == nil || *report.Providers[0].Tokens.TotalTokens != 5000 {
		t.Errorf("expected 5000 tokens from sqlite fallback, got %+v", pr.Tokens)
	}
	if pr.Windows != nil {
		t.Errorf("expected Windows to be nil on fallback, got %+v", pr.Windows)
	}
}

func TestAdapter_CollectWholeRunAndSchemaValidation(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	runner := createMockAppServerRunner(func(reqMethod string, reqID int64, params json.RawMessage) (any, *mockRPCError) {
		switch reqMethod {
		case "initialize":
			return map[string]any{"userAgent": "dipstick/0.1.0"}, nil
		case "account/rateLimits/read":
			return map[string]any{
				"rateLimits": map[string]any{
					"limitId": "codex",
					"primary": map[string]any{
						"usedPercent":        10.0,
						"windowDurationMins": 300,
						"resetsAt":           now.Add(5 * time.Hour).Unix(),
					},
					"secondary": map[string]any{
						"usedPercent":        5.0,
						"windowDurationMins": 10080,
						"resetsAt":           now.Add(7 * 24 * time.Hour).Unix(),
					},
					"planType": "pro",
				},
			}, nil
		case "account/usage/read":
			return map[string]any{
				"summary": map[string]any{
					"lifetimeTokens": 1000000,
				},
			}, nil
		case "account/read":
			return map[string]any{
				"account": map[string]any{
					"type":     "chatgpt",
					"email":    "schema-user@example.com",
					"planType": "pro",
				},
			}, nil
		default:
			return nil, &mockRPCError{Code: -32601, Message: "not found"}
		}
	})

	adapter := codex.New(
		codex.WithAppServerRunner(runner),
		codex.WithNow(func() time.Time { return now }),
	)

	report, err := dipstick.Collect(context.Background(),
		dipstick.WithProviders(dipstick.ProviderCodex),
		dipstick.WithAdapter(adapter),
	)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(report.Providers) != 1 {
		t.Fatalf("expected 1 provider report, got %d (errors: %+v)", len(report.Providers), report.Errors)
	}
	pr := report.Providers[0]
	if pr.Provider != dipstick.ProviderCodex {
		t.Errorf("expected provider codex, got %s", pr.Provider)
	}
	if pr.Source != dipstick.SourceAppServer {
		t.Errorf("expected source app_server, got %s", pr.Source)
	}
	if pr.Confidence != dipstick.ConfidenceExact {
		t.Errorf("expected confidence exact, got %s", pr.Confidence)
	}

	// Validate JSON schema
	schemaPath := filepath.Join("..", "..", "..", "schema", "dipstick.v1.json")
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		t.Fatalf("failed compiling schema %s: %v", schemaPath, err)
	}

	reportBytes, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshalling report: %v", err)
	}

	var unmarshaled any
	if err := json.Unmarshal(reportBytes, &unmarshaled); err != nil {
		t.Fatalf("unmarshalling report JSON: %v", err)
	}

	if err := schema.Validate(unmarshaled); err != nil {
		t.Errorf("report failed dipstick.v1 schema validation: %v\nJSON: %s", err, string(reportBytes))
	}
}

func TestLocalStateSource_ZeroLeakageInvariant(t *testing.T) {
	secretKey := "sk-super-secret-openai-api-key-9999"
	secretToken := "secret-jwt-token-string-xyz"
	authData := fmt.Sprintf(`{
		"auth_mode": "api_key",
		"OPENAI_API_KEY": %q,
		"tokens": {
			"id_token": %q,
			"access_token": "secret-access-token",
			"refresh_token": "secret-refresh-token",
			"account_id": "acc-leak-test"
		}
	}`, secretKey, secretToken)

	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	_ = os.MkdirAll(codexDir, 0o700)
	_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authData), 0o600)

	resolver := localstate.New(localstate.WithHomeDir(tmpDir), localstate.WithEnvMap(map[string]string{}))
	adapter := codex.New(codex.WithResolver(resolver))

	report, err := adapter.Sources()[1].Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	reportBytes, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshaling report: %v", err)
	}
	reportStr := string(reportBytes)

	if strings.Contains(reportStr, secretKey) {
		t.Errorf("Report JSON leaked secret API key: %s", reportStr)
	}
	if strings.Contains(reportStr, secretToken) {
		t.Errorf("Report JSON leaked secret token: %s", reportStr)
	}
	if strings.Contains(reportStr, "secret-access-token") {
		t.Errorf("Report JSON leaked access token: %s", reportStr)
	}
	if strings.Contains(reportStr, "secret-refresh-token") {
		t.Errorf("Report JSON leaked refresh token: %s", reportStr)
	}
}
