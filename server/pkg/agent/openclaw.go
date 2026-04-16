package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// openclawBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Overriding these would break
// the daemon↔OpenClaw communication protocol.
var openclawBlockedArgs = map[string]blockedArgMode{
	"--local":      blockedStandalone, // forces non-interactive embedded agent
	"--json":       blockedStandalone, // enables claude-stream-json on stdout
	"--session-id": blockedWithValue,  // managed by daemon for session resumption
	"--message":    blockedWithValue,  // prompt is set by daemon
	"--model":      blockedWithValue,  // backend/model selection is set by daemon
}

// openclawBackend implements Backend by spawning `openclaw agent --local --json`
// with a Multica-managed config (set via OPENCLAW_CONFIG_PATH in daemon env)
// and parsing the resulting claude-stream-json envelope from stdout.
type openclawBackend struct {
	cfg Config
}

func (b *openclawBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "openclaw"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("openclaw executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	sessionID := opts.ResumeSessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("multica-%d", time.Now().UnixNano())
	}

	args := []string{
		"agent",
		"--local",
		"--json",
		"--session-id", sessionID,
		"--model", openclawModelString(opts.Model),
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, openclawBlockedArgs, b.cfg.Logger)...)
	args = append(args, "--message", prompt)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	b.cfg.Logger.Debug("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("openclaw stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[openclaw:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start openclaw: %w", err)
	}

	b.cfg.Logger.Info("openclaw started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when context is cancelled so the parser unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		state := processOpenclawOutput(stdout, msgCh, b.cfg.Logger)
		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			state.status = "timeout"
			state.errMsg = fmt.Sprintf("openclaw timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			state.status = "aborted"
			state.errMsg = "execution cancelled"
		} else if exitErr != nil && state.status == "completed" {
			state.status = "failed"
			state.errMsg = fmt.Sprintf("openclaw exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("openclaw finished",
			"pid", cmd.Process.Pid,
			"status", state.status,
			"duration", duration.Round(time.Millisecond).String())

		var usage map[string]TokenUsage
		u := state.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "multica-claude/sonnet"
			}
			usage = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     state.status,
			Output:     state.output.String(),
			Error:      state.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  state.sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// openclawModelString resolves the daemon's MULTICA_OPENCLAW_MODEL value into
// the <backend-id>/<model-name> format OpenClaw expects.
//
//	""              → "multica-claude/sonnet"          (zero-config default)
//	"opus"          → "multica-claude/opus"            (model alias under default backend)
//	"claude-cli/4"  → "claude-cli/4"                   (operator passed full backend/model)
func openclawModelString(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "multica-claude/sonnet"
	}
	if strings.Contains(model, "/") {
		return model
	}
	return "multica-claude/" + model
}
