package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattwalters/dipstick/internal/cliexec"
	"github.com/mattwalters/dipstick/internal/types"
)

// DefaultAppServerTimeout is the default execution timeout for codex app-server stdio operations.
const DefaultAppServerTimeout = 5 * time.Second

// AppServerRunner is responsible for starting a codex app-server stdio session.
type AppServerRunner interface {
	Start(ctx context.Context) (io.ReadWriteCloser, error)
}

// AppServerRunnerFunc is an adapter to allow using ordinary functions as AppServerRunner.
type AppServerRunnerFunc func(ctx context.Context) (io.ReadWriteCloser, error)

// Start calls f(ctx).
func (f AppServerRunnerFunc) Start(ctx context.Context) (io.ReadWriteCloser, error) {
	return f(ctx)
}

type processTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	mu     sync.Mutex
	closed bool
}

func (p *processTransport) Read(b []byte) (int, error) {
	return p.stdout.Read(b)
}

func (p *processTransport) Write(b []byte) (int, error) {
	return p.stdin.Write(b)
}

func (p *processTransport) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true

	var closeErr error
	if p.stdin != nil {
		closeErr = p.stdin.Close()
	}
	if p.stdout != nil {
		_ = p.stdout.Close()
	}

	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case <-time.After(500 * time.Millisecond):
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-done
	case <-done:
	}

	return closeErr
}

func defaultAppServerRunner(binaryName string) AppServerRunner {
	return AppServerRunnerFunc(func(ctx context.Context) (io.ReadWriteCloser, error) {
		resolvedPath, err := cliexec.ResolveBinary(binaryName)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", types.ErrNotInstalled, err)
		}

		cmd := exec.CommandContext(ctx, resolvedPath, "app-server", "--stdio")
		cmd.Env = cliexec.ScrubEnv(os.Environ())
		cmd.Cancel = func() error {
			if cmd.Process != nil {
				return cmd.Process.Kill()
			}
			return nil
		}
		cmd.WaitDelay = 500 * time.Millisecond

		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("creating stdin pipe: %w", err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			_ = stdin.Close()
			return nil, fmt.Errorf("creating stdout pipe: %w", err)
		}

		if err := cmd.Start(); err != nil {
			_ = stdin.Close()
			_ = stdout.Close()
			return nil, fmt.Errorf("starting codex app-server: %w", err)
		}

		return &processTransport{
			cmd:    cmd,
			stdin:  stdin,
			stdout: stdout,
		}, nil
	})
}

// JSON-RPC 2.0 structures

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *jsonRPCError) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// Protocol Models

type initializeParams struct {
	ClientInfo   clientInfo     `json:"clientInfo"`
	Capabilities map[string]any `json:"capabilities"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOs     string `json:"platformOs"`
}

type rateLimitsReadResult struct {
	RateLimits            *rateLimitsSnapshot           `json:"rateLimits"`
	RateLimitsByLimitID   map[string]rateLimitsSnapshot `json:"rateLimitsByLimitId"`
	RateLimitResetCredits *rateLimitResetCredits        `json:"rateLimitResetCredits"`
}

type rateLimitsSnapshot struct {
	LimitID              string           `json:"limitId"`
	LimitName            *string          `json:"limitName"`
	Primary              *rateLimitWindow `json:"primary"`
	Secondary            *rateLimitWindow `json:"secondary"`
	Credits              *creditsInfo     `json:"credits"`
	IndividualLimit      any              `json:"individualLimit"`
	SpendControlReached  bool             `json:"spendControlReached"`
	PlanType             string           `json:"planType"`
	RateLimitReachedType *string          `json:"rateLimitReachedType"`
}

type rateLimitWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins int64   `json:"windowDurationMins"`
	ResetsAt           int64   `json:"resetsAt"`
}

type creditsInfo struct {
	HasCredits bool   `json:"hasCredits"`
	Unlimited  bool   `json:"unlimited"`
	Balance    string `json:"balance"`
}

type rateLimitResetCredits struct {
	AvailableCount int               `json:"availableCount"`
	Credits        []rateLimitCredit `json:"credits"`
}

type rateLimitCredit struct {
	ID          string `json:"id"`
	ResetType   string `json:"resetType"`
	Status      string `json:"status"`
	GrantedAt   int64  `json:"grantedAt"`
	ExpiresAt   int64  `json:"expiresAt"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type usageReadResult struct {
	Summary           *usageSummary      `json:"summary"`
	DailyUsageBuckets []dailyUsageBucket `json:"dailyUsageBuckets"`
	ThreadUsage       any                `json:"threadUsage"`
}

type usageSummary struct {
	LifetimeTokens        int64 `json:"lifetimeTokens"`
	PeakDailyTokens       int64 `json:"peakDailyTokens"`
	LongestRunningTurnSec int64 `json:"longestRunningTurnSec"`
	CurrentStreakDays     int   `json:"currentStreakDays"`
	LongestStreakDays     int   `json:"longestStreakDays"`
}

type dailyUsageBucket struct {
	StartDate string `json:"startDate"`
	Tokens    int64  `json:"tokens"`
}

type accountReadResult struct {
	Account struct {
		Type     string `json:"type"`
		Email    string `json:"email"`
		PlanType string `json:"planType"`
	} `json:"account"`
	RequiresOpenaiAuth bool `json:"requiresOpenaiAuth"`
}

// appServerClient manages JSON-RPC 2.0 communication over a stdio transport.
type appServerClient struct {
	transport io.ReadWriteCloser
	reader    *bufio.Reader
	mu        sync.Mutex
	nextID    int64
}

func newAppServerClient(transport io.ReadWriteCloser) *appServerClient {
	return &appServerClient{
		transport: transport,
		reader:    bufio.NewReader(transport),
	}
}

func (c *appServerClient) call(ctx context.Context, method string, params any, result any) error {
	id := atomic.AddInt64(&c.nextID, 1)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	reqBytes = append(reqBytes, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()

	type writeResponse struct {
		err error
	}
	writeCh := make(chan writeResponse, 1)
	go func() {
		_, wErr := c.transport.Write(reqBytes)
		writeCh <- writeResponse{err: wErr}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-writeCh:
		if res.err != nil {
			return fmt.Errorf("writing to app-server: %w", res.err)
		}
	}

	// Read responses until matching ID is found
	type readResponse struct {
		line []byte
		err  error
	}

	for {
		readCh := make(chan readResponse, 1)
		go func() {
			line, rErr := c.reader.ReadBytes('\n')
			readCh <- readResponse{line: line, err: rErr}
		}()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case res := <-readCh:
			if res.err != nil {
				return fmt.Errorf("reading from app-server: %w", res.err)
			}

			trimmed := bytes.TrimSpace(res.line)
			if len(trimmed) == 0 {
				continue
			}

			var resp jsonRPCResponse
			if err := json.Unmarshal(trimmed, &resp); err != nil {
				return fmt.Errorf("%w: unmarshaling app-server response: %v", types.ErrParseFailed, err)
			}

			// If notification or different request ID, continue reading
			if resp.ID != id {
				continue
			}

			if resp.Error != nil {
				return resp.Error
			}

			if result != nil && len(resp.Result) > 0 {
				if err := json.Unmarshal(resp.Result, result); err != nil {
					return fmt.Errorf("%w: unmarshaling result payload: %v", types.ErrParseFailed, err)
				}
			}
			return nil
		}
	}
}

func (c *appServerClient) Initialize(ctx context.Context) (*initializeResult, error) {
	params := initializeParams{
		ClientInfo: clientInfo{
			Name:    "dipstick",
			Version: "0.1.0",
		},
		Capabilities: map[string]any{},
	}
	var res initializeResult
	if err := c.call(ctx, "initialize", params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *appServerClient) ReadRateLimits(ctx context.Context) (*rateLimitsReadResult, error) {
	var res rateLimitsReadResult
	if err := c.call(ctx, "account/rateLimits/read", nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *appServerClient) ReadUsage(ctx context.Context) (*usageReadResult, error) {
	var res usageReadResult
	if err := c.call(ctx, "account/usage/read", nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *appServerClient) ReadAccount(ctx context.Context) (*accountReadResult, error) {
	var res accountReadResult
	if err := c.call(ctx, "account/read", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
