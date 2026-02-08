package exectool

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/flexigpt/llmtools-go/internal/executil"
	"github.com/flexigpt/llmtools-go/internal/fileutil"
	"github.com/flexigpt/llmtools-go/internal/toolutil"
	"github.com/flexigpt/llmtools-go/spec"
)

const shellCommandFuncID spec.FuncID = "github.com/flexigpt/llmtools-go/exectool/shell.ShellCommand"

var shellToolSpec = spec.Tool{
	SchemaVersion: spec.SchemaVersion,
	ID:            "019bfeda-33f2-7315-9007-de55935d2302",
	Slug:          "shell",
	Version:       "v1.0.0",
	DisplayName:   "Shell",
	Description:   "Execute local shell commands (cross-platform) with session-like persistence for workdir/env.",
	Tags:          []string{"shell", "exec"},

	ArgSchema: spec.JSONSchema(`{
"$schema": "http://json-schema.org/draft-07/schema#",
"type": "object",
"properties": {
	"commands": {
		"type": "array",
		"items": { "type": "string" },
		"description": "List of commands to execute sequentially. Prefer setting workdir rather than using 'cd'."
	},
	"workdir": {
		"type": "string",
		"description": "Working directory to execute in. If omitted and sessionID is used, uses the session workdir; otherwise uses current process directory."
	},
	"env": {
		"type": "object",
		"additionalProperties": { "type": "string" },
		"description": "Environment variable overrides (merged into the process env)."
	},
	"shell": {
		"type": "string",
		"enum": ["auto", "bash", "zsh", "sh", "dash", "ksh", "fish", "pwsh", "powershell", "cmd"],
		"default": "auto",
		"description": "Which shell to run. 'auto' chooses a safe default per OS."
	},
	"executeParallel": {
		"type": "boolean",
		"default": false,
		"description": "If true, treat commands as independent and parallel executable (do not stop on error)."
	},
	"sessionID": {
		"type": "string",
		"default": "",
		"description": "Optional session identifier. If omitted/empty, a new session is created and returned. Sessions persist workdir and env across calls (not a persistent shell process)."
	}
},
"additionalProperties": false
}`),
	GoImpl: spec.GoToolImpl{FuncID: shellCommandFuncID},

	CreatedAt:  spec.SchemaStartTime,
	ModifiedAt: spec.SchemaStartTime,
}

type ShellName = executil.ShellName

const (
	ShellNameAuto       ShellName = executil.ShellNameAuto
	ShellNameBash       ShellName = executil.ShellNameBash
	ShellNameZsh        ShellName = executil.ShellNameZsh
	ShellNameSh         ShellName = executil.ShellNameSh
	ShellNameDash       ShellName = executil.ShellNameDash
	ShellNameKsh        ShellName = executil.ShellNameKsh
	ShellNameFish       ShellName = executil.ShellNameFish
	ShellNamePwsh       ShellName = executil.ShellNamePwsh
	ShellNamePowershell ShellName = executil.ShellNamePowershell
	ShellNameCmd        ShellName = executil.ShellNameCmd
)

type ShellCommandArgs struct {
	Commands        []string          `json:"commands,omitempty"`
	Workdir         string            `json:"workdir,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	Shell           ShellName         `json:"shell,omitempty"`
	ExecuteParallel bool              `json:"executeParallel,omitempty"`
	SessionID       string            `json:"sessionID,omitempty"`
}

type ShellCommandExecResult = executil.ShellCommandExecResult

type ShellCommandResponse struct {
	SessionID string                   `json:"sessionID,omitempty"`
	Workdir   string                   `json:"workdir,omitempty"`
	Results   []ShellCommandExecResult `json:"results,omitempty"`
}

// ShellCommandPolicy provides policy / hardening knobs (package-level, so host app can tune).
type ShellCommandPolicy struct {
	// If true, skip heuristic checks (fork-bomb/backgrounding). NOTE: hard-blocked commands are ALWAYS blocked.
	AllowDangerous bool

	// Policy limits (clamped to package hard limits).
	Timeout          time.Duration
	MaxOutputBytes   int64
	MaxCommands      int
	MaxCommandLength int
}

var DefaultShellCommandPolicy = ShellCommandPolicy{
	AllowDangerous:   false,
	Timeout:          executil.DefaultTimeout,
	MaxOutputBytes:   executil.DefaultMaxOutputBytes,
	MaxCommands:      executil.DefaultMaxCommands,
	MaxCommandLength: executil.DefaultMaxCommandLength,
}

// ShellTool is an instance-owned shell tool runner.
// It owns sessions, policy, and environment inheritance settings.
type ShellTool struct {
	mu                  sync.RWMutex
	policy              ShellCommandPolicy
	allowedWorkdirRoots []string            // optional; if empty, allow any
	workdirBase         string              // optional; if set, relative workdir is resolved against this base
	blockedCommands     map[string]struct{} // instance-owned blocklist (includes non-overridable hard defaults)
	sessions            *executil.SessionStore
}

type ShellToolOption func(*ShellTool) error

func WithShellCommandPolicy(p ShellCommandPolicy) ShellToolOption {
	return func(st *ShellTool) error {
		st.policy = p
		return nil
	}
}

// WithShellBlockedCommands adds additional commands to the instance blocklist.
// These are enforced before execution and cannot override/remove the hard default blocklist.
// Entries must be command names (e.g. "git", "python", "curl"), not full command lines.
func WithShellBlockedCommands(cmds []string) ShellToolOption {
	return func(st *ShellTool) error {
		for _, c := range cmds {
			n, err := executil.NormalizeBlockedCommand(c)
			if err != nil {
				return err
			}
			if n == "" {
				continue
			}
			st.blockedCommands[n] = struct{}{}
			// On Windows, also add the no-extension variant if it looks like an executable name.
			if runtime.GOOS == toolutil.GOOSWindows {
				ext := strings.ToLower(filepath.Ext(n))
				switch ext {
				case ".exe", ".com", ".bat", ".cmd":
					st.blockedCommands[strings.TrimSuffix(n, ext)] = struct{}{}
				}
			}
		}
		return nil
	}
}

// WithShellAllowedWorkdirRoots restricts workdir to be within one of the provided roots.
// Roots are canonicalized (clean+abs) and must exist as directories.
func WithShellAllowedWorkdirRoots(roots []string) ShellToolOption {
	return func(st *ShellTool) error {
		canon, err := fileutil.CanonicalizeAllowedRoots(roots)
		if err != nil {
			return err
		}
		st.allowedWorkdirRoots = canon
		return nil
	}
}

// WithShellWorkdirBase sets a base directory used to resolve relative ShellCommandArgs.Workdir.
// If unset, relative workdir is resolved against the current process working directory (existing behavior).
//
// This is useful when callers want workdir values to be interpreted relative to a "sandbox root"
// without re-implementing path joining logic in every wrapper.
func WithShellWorkdirBase(base string) ShellToolOption {
	return func(st *ShellTool) error {
		if strings.TrimSpace(base) == "" {
			st.workdirBase = ""
			return nil
		}
		p, err := fileutil.GetEffectiveWorkDir(base, nil)
		if err != nil {
			return err
		}
		st.workdirBase = p
		return nil
	}
}

// WithShellSessionTTL enables TTL eviction for sessions.
// "ttl<=0" disables TTL eviction (LRU max may still evict).
func WithShellSessionTTL(ttl time.Duration) ShellToolOption {
	return func(st *ShellTool) error {
		st.sessions.SetTTL(ttl)
		return nil
	}
}

// WithShellMaxSessions sets an upper bound on concurrent sessions (LRU eviction).
// "max<=0" disables max-session eviction (TTL may still evict).
func WithShellMaxSessions(maxSessions int) ShellToolOption {
	return func(st *ShellTool) error {
		st.sessions.SetMaxSessions(maxSessions)
		return nil
	}
}

func NewShellTool(opts ...ShellToolOption) (*ShellTool, error) {
	st := &ShellTool{
		policy:              DefaultShellCommandPolicy,
		allowedWorkdirRoots: nil,
		workdirBase:         "",

		blockedCommands: maps.Clone(executil.HardBlockedCommands),
		sessions:        executil.NewSessionStore(),
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(st); err != nil {
			return nil, err
		}
	}
	return st, nil
}

func (st *ShellTool) Tool() spec.Tool { return toolutil.CloneTool(shellToolSpec) }

// SetAllowedWorkdirRoots allows changing workdir roots at runtime (best-effort).
// Existing sessions whose workdir falls outside the new roots will fail when used.
func (st *ShellTool) SetAllowedWorkdirRoots(roots []string) error {
	canon, err := fileutil.CanonicalizeAllowedRoots(roots)
	if err != nil {
		return err
	}
	st.mu.Lock()
	st.allowedWorkdirRoots = canon
	st.mu.Unlock()
	return nil
}

func (st *ShellTool) Run(ctx context.Context, args ShellCommandArgs) (out *ShellCommandResponse, err error) {
	return toolutil.WithRecoveryResp(func() (out *ShellCommandResponse, err error) {
		return st.run(ctx, args)
	})
}

func (st *ShellTool) run(ctx context.Context, args ShellCommandArgs) (out *ShellCommandResponse, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	args.SessionID = strings.TrimSpace(args.SessionID)

	st.mu.RLock()
	policy := st.policy
	roots := append([]string(nil), st.allowedWorkdirRoots...)
	blocked := st.blockedCommands
	st.mu.RUnlock()

	// Determine commands early (so we don't create sessions for invalid requests).
	cmds := normalizedCommandList(args)
	if len(cmds) == 0 {
		return nil, errors.New("commands is required")
	}
	maxCmds := effectiveMaxCommands(policy)
	if maxCmds > 0 && len(cmds) > maxCmds {
		return nil, fmt.Errorf("too many commands: %d (max %d)", len(cmds), maxCmds)
	}

	createdSessionID := ""
	defer func() {
		// If we created a session but the call failed, do not leak it.
		if err != nil && createdSessionID != "" {
			st.sessions.Delete(createdSessionID)
		}
	}()

	// Handle session lifecycle first.
	var sess *executil.ShellSession
	// Session semantics:
	// - if sessionID provided: use it
	// - otherwise: create a new session and return it.
	if args.SessionID != "" {
		var ok bool
		sess, ok = st.sessions.Get(args.SessionID)
		if !ok {
			return nil, fmt.Errorf("unknown sessionID: %s", args.SessionID)
		}
	} else {
		sess = st.sessions.NewSession()
		args.SessionID = sess.GetID()
		createdSessionID = sess.GetID()
	}

	// Determine effective settings (policy-only).
	timeout := effectiveTimeout(policy)
	maxOut := effectiveMaxOutputBytes(policy)
	maxCmdLen := effectiveMaxCommandLength(policy)

	// "executeParallel=true" => treat commands as independent => do not stop on error.
	stopOnError := !args.ExecuteParallel

	// If base is set and arg is relative, resolve against base.
	inputWorkDir := args.Workdir
	if strings.TrimSpace(st.workdirBase) != "" && !filepath.IsAbs(args.Workdir) {
		inputWorkDir = filepath.Join(st.workdirBase, args.Workdir)
	}

	// Determine effective workdir (args > session > current).
	workdir, err := sess.GetEffectiveWorkdir(inputWorkDir, roots)
	if err != nil {
		return nil, err
	}

	// Determine effective env (process env + session env + args env).
	// Validate env early so we don't:
	//  1) store invalid env into sessions
	//  2) fail later at exec.Start with confusing errors
	if err := executil.ValidateEnvMap(args.Env); err != nil {
		return nil, err
	}

	// Determine effective env (process env + session env + args env).
	env, err := sess.GetEffectiveEnv(args.Env)
	if err != nil {
		return nil, err
	}

	// Choose shell + path.
	sel, err := selectShell(args.Shell)
	if err != nil {
		return nil, err
	}

	// Persist session defaults if session is used and caller provided values.
	if sess != nil {
		if strings.TrimSpace(args.Workdir) != "" {
			sess.SetWorkDir(workdir)
		}
		err := sess.AddToEnv(args.Env)
		if err != nil {
			return nil, err
		}
	}

	results := make([]ShellCommandExecResult, 0, len(cmds))
	for _, one := range cmds {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		command := strings.TrimSpace(one)
		if command == "" {
			continue
		}
		if maxCmdLen > 0 && len(command) > maxCmdLen {
			return nil, fmt.Errorf(
				"command too long (%d bytes; max %d)",
				len(command), maxCmdLen,
			)
		}
		if strings.ContainsRune(command, '\x00') {
			return nil, errors.New("command contains NUL byte")
		}

		// Always enforce command blocklist. Heuristic checks are optional.
		if err := executil.RejectDangerousCommand(
			command,
			sel.Path,
			sel.Name,
			blocked,
			!policy.AllowDangerous,
		); err != nil {
			return nil, err
		}

		res, err := executil.RunOneShellCommand(ctx, sel, command, workdir, env, timeout, maxOut)
		if err != nil {
			// We still return structured output when possible.
			// If it's an exec-start failure, include it in stderr-ish form.
			res = ShellCommandExecResult{
				Command:   command,
				Workdir:   workdir,
				Shell:     sel.Name,
				ShellPath: sel.Path,

				ExitCode:   127,
				TimedOut:   false,
				DurationMS: 0,
				Stdout:     "",
				Stderr:     err.Error(),
			}
		}
		results = append(results, res)
		if stopOnError && (res.TimedOut || res.ExitCode != 0) {
			break
		}
	}

	resp := ShellCommandResponse{
		SessionID: args.SessionID,
		Workdir:   workdir,
		Results:   results,
	}

	return &resp, err
}

func normalizedCommandList(args ShellCommandArgs) []string {
	if len(args.Commands) != 0 {
		out := make([]string, 0, len(args.Commands))
		for _, c := range args.Commands {
			if strings.TrimSpace(c) != "" {
				out = append(out, c)
			}
		}
		return out
	}
	return nil
}

func selectShell(requested ShellName) (executil.SelectedShell, error) {
	r := strings.ToLower(strings.TrimSpace(string(requested)))
	if r == "" {
		r = "auto"
	}

	if r != "auto" {
		return resolveShell(r)
	}

	// Auto.
	if runtime.GOOS == toolutil.GOOSWindows {
		// Prefer pwsh, then Windows PowerShell, then cmd.
		if p, _ := exec.LookPath("pwsh"); p != "" {
			return executil.SelectedShell{Name: ShellNamePwsh, Path: p}, nil
		}
		if p, _ := exec.LookPath("powershell"); p != "" {
			return executil.SelectedShell{Name: ShellNamePowershell, Path: p}, nil
		}
		if p, _ := exec.LookPath("cmd"); p != "" {
			return executil.SelectedShell{Name: ShellNameCmd, Path: p}, nil
		}
		return executil.SelectedShell{}, errors.New("no suitable shell found on windows (pwsh/powershell/cmd)")
	}

	// Unix-ish: prefer $SHELL if present, else bash/zsh/sh.
	if sh := os.Getenv("SHELL"); sh != "" {
		if p, err := exec.LookPath(sh); err == nil && p != "" {
			// Best-effort: infer by basename.
			base := ShellName(strings.ToLower(filepath.Base(p)))
			switch base {
			case ShellNameBash, ShellNameZsh, ShellNameSh, ShellNameDash, ShellNameKsh, ShellNameFish:
				return executil.SelectedShell{Name: base, Path: p}, nil
			default:
				// Need to explicitly try what we support.
			}
		}
	}

	if p, _ := exec.LookPath(string(ShellNameBash)); p != "" {
		return executil.SelectedShell{Name: ShellNameBash, Path: p}, nil
	}
	if p, _ := exec.LookPath(string(ShellNameZsh)); p != "" {
		return executil.SelectedShell{Name: ShellNameZsh, Path: p}, nil
	}
	if p, _ := exec.LookPath(string(ShellNameSh)); p != "" {
		return executil.SelectedShell{Name: ShellNameSh, Path: p}, nil
	}
	if p, _ := exec.LookPath(string(ShellNameDash)); p != "" {
		return executil.SelectedShell{Name: ShellNameDash, Path: p}, nil
	}
	if p, _ := exec.LookPath(string(ShellNameKsh)); p != "" {
		return executil.SelectedShell{Name: ShellNameKsh, Path: p}, nil
	}
	if p, _ := exec.LookPath(string(ShellNameFish)); p != "" {
		return executil.SelectedShell{Name: ShellNameFish, Path: p}, nil
	}
	return executil.SelectedShell{}, errors.New("no suitable shell found (bash/zsh/sh)")
}

func resolveShell(name string) (executil.SelectedShell, error) {
	shellName := ShellName(name)
	switch shellName {
	case ShellNameBash, ShellNameZsh, ShellNameSh, ShellNameDash, ShellNameKsh, ShellNameFish:
		p, err := exec.LookPath(name)
		if err != nil {
			return executil.SelectedShell{}, fmt.Errorf("shell not found: %s", name)
		}
		return executil.SelectedShell{Name: shellName, Path: p}, nil
	case ShellNamePwsh:
		p, err := exec.LookPath("pwsh")
		if err != nil {
			return executil.SelectedShell{}, errors.New("pwsh requested but not found")
		}
		return executil.SelectedShell{Name: ShellNamePwsh, Path: p}, nil
	case ShellNamePowershell:
		// Accept pwsh or powershell as the resolved path.
		if p, _ := exec.LookPath("pwsh"); p != "" {
			return executil.SelectedShell{Name: ShellNamePwsh, Path: p}, nil
		}
		p, err := exec.LookPath("powershell")
		if err != nil {
			return executil.SelectedShell{}, errors.New("powershell requested but neither pwsh nor powershell found")
		}
		return executil.SelectedShell{Name: ShellNamePowershell, Path: p}, nil
	case ShellNameCmd:
		p, err := exec.LookPath("cmd")
		if err != nil {
			return executil.SelectedShell{}, errors.New("cmd requested but not found")
		}
		return executil.SelectedShell{Name: ShellNameCmd, Path: p}, nil
	default:
		return executil.SelectedShell{}, fmt.Errorf("invalid shell: %q", name)
	}
}

func effectiveTimeout(policy ShellCommandPolicy) time.Duration {
	d := policy.Timeout
	if d <= 0 {
		d = executil.DefaultTimeout
	}
	if d > executil.HardMaxTimeout {
		d = executil.HardMaxTimeout
	}
	return d
}

func effectiveMaxOutputBytes(policy ShellCommandPolicy) int64 {
	v := policy.MaxOutputBytes
	if v <= 0 {
		v = executil.DefaultMaxOutputBytes
	}
	v = max(v, executil.MinOutputBytes)
	v = min(v, executil.HardMaxOutputBytes)
	// Also prevent int overflow in cappedWriter allocation.
	v = min(v, int64(math.MaxInt))
	return v
}

func effectiveMaxCommands(policy ShellCommandPolicy) int {
	v := policy.MaxCommands
	if v <= 0 {
		v = executil.DefaultMaxCommands
	}
	v = max(v, 1)
	v = min(v, executil.HardMaxCommands)
	return v
}

func effectiveMaxCommandLength(policy ShellCommandPolicy) int {
	v := policy.MaxCommandLength
	if v <= 0 {
		v = executil.DefaultMaxCommandLength
	}
	v = max(v, 1)
	v = min(v, executil.HardMaxCommandLength)
	return v
}
