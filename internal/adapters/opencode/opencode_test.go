package opencode_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mattwalters/dipstick/internal/adapters/opencode"
	"github.com/mattwalters/dipstick/internal/cliexec"
	"github.com/mattwalters/dipstick/internal/localstate"
)

func createTestDB(t *testing.T, dir string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer func() { _ = db.Close() }()

	schema := `
	CREATE TABLE session (
		id text PRIMARY KEY,
		project_id text NOT NULL,
		workspace_id text,
		parent_id text,
		slug text NOT NULL,
		directory text NOT NULL,
		path text,
		title text NOT NULL,
		version text NOT NULL,
		share_url text,
		summary_additions integer,
		summary_deletions integer,
		summary_files integer,
		summary_diffs text,
		metadata text,
		cost real DEFAULT 0 NOT NULL,
		tokens_input integer DEFAULT 0 NOT NULL,
		tokens_output integer DEFAULT 0 NOT NULL,
		tokens_reasoning integer DEFAULT 0 NOT NULL,
		tokens_cache_read integer DEFAULT 0 NOT NULL,
		tokens_cache_write integer DEFAULT 0 NOT NULL,
		revert text,
		permission text,
		agent text,
		model text,
		time_created integer NOT NULL,
		time_updated integer NOT NULL,
		time_compacting integer,
		time_archived integer
	);`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create session schema: %v", err)
	}

	insertQuery := `
	INSERT INTO session (
		id, project_id, slug, directory, title, version,
		tokens_input, tokens_output, tokens_reasoning, tokens_cache_read, tokens_cache_write,
		time_created, time_updated
	) VALUES 
	('ses_1', 'prj_1', 'slug-1', '/dir', 'Turn 1', '1.0.0', 1000, 250, 50, 5000, 800, 1724961200000, 1724961234000),
	('ses_2', 'prj_1', 'slug-2', '/dir', 'Turn 2', '1.0.0', 2000, 500, 100, 10000, 1200, 1724961250000, 1724961300000);`

	if _, err := db.Exec(insertQuery); err != nil {
		t.Fatalf("failed to insert test session rows: %v", err)
	}

	return dbPath
}

func TestAdapter_Name(t *testing.T) {
	a := opencode.New()
	if a == nil {
		t.Fatalf("expected non-nil adapter")
	}
	if a.Name() != "opencode" {
		t.Errorf("expected name 'opencode', got %q", a.Name())
	}
}

func TestAdapter_Detect(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	t.Run("DB present and auth file present", func(t *testing.T) {
		shareDir := filepath.Join(tmpDir, "share", "opencode")
		if err := os.MkdirAll(shareDir, 0o755); err != nil {
			t.Fatal(err)
		}
		createTestDB(t, shareDir)
		authFile := filepath.Join(shareDir, "auth.json")
		if err := os.WriteFile(authFile, []byte(`{"anthropic": {"apiKey": "sk-ant-test"}}`), 0o600); err != nil {
			t.Fatal(err)
		}

		resolver := localstate.New(
			localstate.WithEnvMap(map[string]string{
				"XDG_DATA_HOME": filepath.Join(tmpDir, "share"),
			}),
		)

		a := opencode.New(
			opencode.WithResolver(resolver),
		)

		det, err := a.Detect(ctx)
		if err != nil {
			t.Fatalf("Detect failed: %v", err)
		}

		if !det.Installed {
			t.Errorf("expected Installed == true when DB exists")
		}
		if !det.Authenticated {
			t.Errorf("expected Authenticated == true when auth.json exists")
		}
	})

	t.Run("Nothing present", func(t *testing.T) {
		emptyDir := filepath.Join(tmpDir, "empty")
		resolver := localstate.New(
			localstate.WithHomeDir(emptyDir),
			localstate.WithEnvMap(map[string]string{}),
		)

		a := opencode.New(
			opencode.WithResolver(resolver),
		)

		det, err := a.Detect(ctx)
		if err != nil {
			t.Fatalf("Detect failed: %v", err)
		}

		// When no DB and no auth in custom empty resolver
		if det.Authenticated {
			t.Errorf("expected Authenticated == false in isolated empty dir")
		}
	})
}

func TestAdapter_Tier2_LocalState(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	shareDir := filepath.Join(tmpDir, "share", "opencode")
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatal(err)
	}

	createTestDB(t, shareDir)

	resolver := localstate.New(
		localstate.WithEnvMap(map[string]string{
			"XDG_DATA_HOME": filepath.Join(tmpDir, "share"),
		}),
	)

	nowTime := time.Date(2026, 8, 29, 15, 0, 0, 0, time.UTC)
	a := opencode.New(
		opencode.WithResolver(resolver),
		opencode.WithNow(func() time.Time { return nowTime }),
	)

	if !a.AvailableLocalState(ctx) {
		t.Fatalf("expected AvailableLocalState == true")
	}

	usage, err := a.FetchLocalState(ctx)
	if err != nil {
		t.Fatalf("FetchLocalState failed: %v", err)
	}

	if usage == nil {
		t.Fatal("expected non-nil TokenUsage")
	}

	// 1000 + 2000 = 3000
	if usage.InputTokens != 3000 {
		t.Errorf("InputTokens: expected 3000, got %d", usage.InputTokens)
	}
	// 250 + 500 = 750
	if usage.OutputTokens != 750 {
		t.Errorf("OutputTokens: expected 750, got %d", usage.OutputTokens)
	}
	// 50 + 100 = 150
	if usage.ReasoningTokens != 150 {
		t.Errorf("ReasoningTokens: expected 150, got %d", usage.ReasoningTokens)
	}
	// 5000 + 10000 = 15000
	if usage.CacheReadTokens != 15000 {
		t.Errorf("CacheReadTokens: expected 15000, got %d", usage.CacheReadTokens)
	}
	// 800 + 1200 = 2000
	if usage.CacheWriteTokens != 2000 {
		t.Errorf("CacheWriteTokens: expected 2000, got %d", usage.CacheWriteTokens)
	}
	// Total = 3000 + 750 + 150 + 15000 + 2000 = 20900
	if usage.TotalTokens != 20900 {
		t.Errorf("TotalTokens: expected 20900, got %d", usage.TotalTokens)
	}

	expectedObserved := time.UnixMilli(1724961300000).UTC()
	if !usage.ObservedAt.Equal(expectedObserved) {
		t.Errorf("ObservedAt: expected %v, got %v", expectedObserved, usage.ObservedAt)
	}
}

func TestAdapter_Tier2_EmptyDB(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	shareDir := filepath.Join(tmpDir, "share", "opencode")
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(shareDir, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE session (
		id text PRIMARY KEY,
		tokens_input integer DEFAULT 0 NOT NULL,
		tokens_output integer DEFAULT 0 NOT NULL,
		tokens_reasoning integer DEFAULT 0 NOT NULL,
		tokens_cache_read integer DEFAULT 0 NOT NULL,
		tokens_cache_write integer DEFAULT 0 NOT NULL,
		time_updated integer NOT NULL
	);`)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}

	resolver := localstate.New(
		localstate.WithEnvMap(map[string]string{
			"XDG_DATA_HOME": filepath.Join(tmpDir, "share"),
		}),
	)

	nowTime := time.Date(2026, 8, 29, 15, 0, 0, 0, time.UTC)
	a := opencode.New(
		opencode.WithResolver(resolver),
		opencode.WithNow(func() time.Time { return nowTime }),
	)

	usage, err := a.FetchLocalState(ctx)
	if err != nil {
		t.Fatalf("FetchLocalState failed on empty table: %v", err)
	}

	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.TotalTokens != 0 {
		t.Errorf("expected 0 tokens on empty DB, got total %d", usage.TotalTokens)
	}
}

func TestAdapter_Tier2_MissingDB(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	resolver := localstate.New(
		localstate.WithHomeDir(tmpDir),
		localstate.WithEnvMap(map[string]string{}),
	)

	a := opencode.New(
		opencode.WithResolver(resolver),
	)

	if a.AvailableLocalState(ctx) {
		t.Errorf("expected AvailableLocalState == false when DB file does not exist")
	}
}

func TestAdapter_Tier3_RPC(t *testing.T) {
	ctx := context.Background()

	t.Run("Valid nested session response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/session" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"id": "ses_rpc_1",
					"tokens": {
						"input": 500,
						"output": 120,
						"reasoning": 30,
						"cache": {
							"read": 1000,
							"write": 200
						}
					},
					"time": {
						"updated": 1724961400000
					}
				},
				{
					"id": "ses_rpc_2",
					"tokens": {
						"input": 1500,
						"output": 380,
						"reasoning": 70,
						"cache": {
							"read": 2000,
							"write": 400
						}
					},
					"time": {
						"updated": 1724961500000
					}
				}
			]`))
		}))
		defer server.Close()

		a := opencode.New(
			opencode.WithServerURL(server.URL),
			opencode.WithHTTPClient(server.Client()),
		)

		if !a.AvailableRPC(ctx) {
			t.Fatalf("expected AvailableRPC == true")
		}

		usage, err := a.FetchRPC(ctx)
		if err != nil {
			t.Fatalf("FetchRPC failed: %v", err)
		}

		if usage.InputTokens != 2000 {
			t.Errorf("InputTokens: expected 2000, got %d", usage.InputTokens)
		}
		if usage.OutputTokens != 500 {
			t.Errorf("OutputTokens: expected 500, got %d", usage.OutputTokens)
		}
		if usage.ReasoningTokens != 100 {
			t.Errorf("ReasoningTokens: expected 100, got %d", usage.ReasoningTokens)
		}
		if usage.CacheReadTokens != 3000 {
			t.Errorf("CacheReadTokens: expected 3000, got %d", usage.CacheReadTokens)
		}
		if usage.CacheWriteTokens != 600 {
			t.Errorf("CacheWriteTokens: expected 600, got %d", usage.CacheWriteTokens)
		}
		if usage.TotalTokens != 6200 {
			t.Errorf("TotalTokens: expected 6200, got %d", usage.TotalTokens)
		}
	})

	t.Run("Valid flat session response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"id": "ses_flat",
					"tokens_input": 100,
					"tokens_output": 50,
					"tokens_reasoning": 10,
					"tokens_cache_read": 300,
					"tokens_cache_write": 40,
					"time_updated": 1724961600000
				}
			]`))
		}))
		defer server.Close()

		a := opencode.New(
			opencode.WithServerURL(server.URL),
			opencode.WithHTTPClient(server.Client()),
		)

		usage, err := a.FetchRPC(ctx)
		if err != nil {
			t.Fatalf("FetchRPC flat format failed: %v", err)
		}

		if usage.TotalTokens != 500 {
			t.Errorf("TotalTokens: expected 500, got %d", usage.TotalTokens)
		}
	})

	t.Run("Server returns 500 error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}))
		defer server.Close()

		a := opencode.New(
			opencode.WithServerURL(server.URL),
			opencode.WithHTTPClient(server.Client()),
		)

		if a.AvailableRPC(ctx) {
			t.Errorf("expected AvailableRPC == false on 500")
		}

		_, err := a.FetchRPC(ctx)
		if err == nil {
			t.Fatalf("expected error from FetchRPC on 500 status")
		}
		if !errors.Is(err, opencode.ErrUpstreamError) {
			t.Errorf("expected ErrUpstreamError, got %v", err)
		}
	})

	t.Run("Server returns malformed JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{ not valid json }`))
		}))
		defer server.Close()

		a := opencode.New(
			opencode.WithServerURL(server.URL),
			opencode.WithHTTPClient(server.Client()),
		)

		_, err := a.FetchRPC(ctx)
		if err == nil {
			t.Fatalf("expected error from FetchRPC on malformed JSON")
		}
		if !errors.Is(err, opencode.ErrParseFailed) {
			t.Errorf("expected ErrParseFailed, got %v", err)
		}
	})

	t.Run("Server unreachable", func(t *testing.T) {
		a := opencode.New(
			opencode.WithServerURL("http://127.0.0.1:59999"),
			opencode.WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}),
		)

		if a.AvailableRPC(ctx) {
			t.Errorf("expected AvailableRPC == false when server unreachable")
		}

		_, err := a.FetchRPC(ctx)
		if err == nil {
			t.Fatalf("expected error when server unreachable")
		}
	})
}

func TestAdapter_Tier5_CLI(t *testing.T) {
	ctx := context.Background()

	t.Run("Valid JSON array output from CLI", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create a fake opencode binary script
		fakeScript := filepath.Join(tmpDir, "opencode")
		scriptContent := `#!/bin/sh
if [ "$1" = "--version" ]; then
	echo "opencode v1.18.20"
	exit 0
fi
if [ "$1" = "db" ]; then
	echo '[{"input_tokens": 1200, "output_tokens": 300, "reasoning_tokens": 50, "cache_read_tokens": 4000, "cache_write_tokens": 500, "time_updated": 1724961700000}]'
	exit 0
fi
exit 1
`
		if err := os.WriteFile(fakeScript, []byte(scriptContent), 0o755); err != nil {
			t.Fatal(err)
		}

		origPath := os.Getenv("PATH")
		t.Setenv("PATH", fmt.Sprintf("%s%c%s", tmpDir, filepath.ListSeparator, origPath))

		runner := cliexec.New(
			cliexec.WithStrictArgv(false),
		)

		a := opencode.New(
			opencode.WithRunner(runner),
		)

		if !a.AvailableCLI(ctx) {
			t.Fatalf("expected AvailableCLI == true")
		}

		usage, err := a.FetchCLI(ctx)
		if err != nil {
			t.Fatalf("FetchCLI failed: %v", err)
		}

		if usage.InputTokens != 1200 {
			t.Errorf("InputTokens: expected 1200, got %d", usage.InputTokens)
		}
		if usage.OutputTokens != 300 {
			t.Errorf("OutputTokens: expected 300, got %d", usage.OutputTokens)
		}
		if usage.ReasoningTokens != 50 {
			t.Errorf("ReasoningTokens: expected 50, got %d", usage.ReasoningTokens)
		}
		if usage.CacheReadTokens != 4000 {
			t.Errorf("CacheReadTokens: expected 4000, got %d", usage.CacheReadTokens)
		}
		if usage.CacheWriteTokens != 500 {
			t.Errorf("CacheWriteTokens: expected 500, got %d", usage.CacheWriteTokens)
		}
		if usage.TotalTokens != 6050 {
			t.Errorf("TotalTokens: expected 6050, got %d", usage.TotalTokens)
		}
	})

	t.Run("Malformed JSON output from CLI", func(t *testing.T) {
		tmpDir := t.TempDir()
		fakeScript := filepath.Join(tmpDir, "opencode")
		scriptContent := `#!/bin/sh
echo 'invalid json output'
exit 0
`
		if err := os.WriteFile(fakeScript, []byte(scriptContent), 0o755); err != nil {
			t.Fatal(err)
		}

		origPath := os.Getenv("PATH")
		t.Setenv("PATH", fmt.Sprintf("%s%c%s", tmpDir, filepath.ListSeparator, origPath))

		runner := cliexec.New(
			cliexec.WithStrictArgv(false),
		)

		a := opencode.New(
			opencode.WithRunner(runner),
		)

		_, err := a.FetchCLI(ctx)
		if err == nil {
			t.Fatalf("expected error on malformed CLI JSON")
		}
		if !errors.Is(err, opencode.ErrParseFailed) {
			t.Errorf("expected ErrParseFailed, got %v", err)
		}
	})
}

func TestAdapter_OptionsAndEdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("nil option arguments do not cause panics", func(t *testing.T) {
		a := opencode.New(
			opencode.WithResolver(nil),
			opencode.WithRunner(nil),
			opencode.WithHTTPClient(nil),
			opencode.WithServerURL(""),
			opencode.WithNow(nil),
		)

		det, err := a.Detect(ctx)
		if err != nil {
			t.Fatalf("Detect failed: %v", err)
		}
		_ = det

		_ = a.AvailableLocalState(ctx)
		_ = a.AvailableRPC(ctx)
		_ = a.AvailableCLI(ctx)
	})

	t.Run("server URL with trailing slash works correctly", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/session" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"tokens_input": 100, "tokens_output": 50, "time_updated": 1000000}]`))
		}))
		defer server.Close()

		a := opencode.New(
			opencode.WithServerURL(server.URL+"/"),
			opencode.WithHTTPClient(server.Client()),
		)

		if !a.AvailableRPC(ctx) {
			t.Errorf("expected AvailableRPC to be true with trailing slash URL")
		}

		usage, err := a.FetchRPC(ctx)
		if err != nil {
			t.Fatalf("FetchRPC failed with trailing slash URL: %v", err)
		}
		if usage.InputTokens != 100 || usage.OutputTokens != 50 {
			t.Errorf("unexpected tokens: %+v", usage)
		}
	})
}
