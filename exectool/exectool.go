package exectool

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/flexigpt/llmtools-go/internal/executil"
	"github.com/flexigpt/llmtools-go/internal/fileutil"
	"github.com/flexigpt/llmtools-go/internal/toolutil"
	"github.com/flexigpt/llmtools-go/spec"
)

// ExecTool is an instance-owned execution tool runner (modeled after fstool.FSTool).
// It centralizes:
//   - workBaseDir: base for resolving relative paths
//   - allowedRoots: optional sandbox roots; if empty, allow all
//   - execution policy (timeouts/output/limits)
//   - session store for shellcommand
//   - runscript policy (optional tool)
type ExecTool struct {
	mu sync.RWMutex

	allowedRoots []string
	// "workBaseDir" is the base for resolving relative paths and the default working directory.
	// If allowedRoots is set and workBaseDir is empty, InitPathPolicy will default workBaseDir to the first allowed
	// root.
	workBaseDir     string
	blockedCommands map[string]struct{} // includes executil.HardBlockedCommands

	execPolicy      ExecutionPolicy
	runScriptPolicy RunScriptPolicy

	sessions *executil.SessionStore
}

type ExecToolOption func(*ExecTool) error

func WithAllowedRoots(roots []string) ExecToolOption {
	return func(et *ExecTool) error {
		et.allowedRoots = roots
		return nil
	}
}

func WithWorkBaseDir(base string) ExecToolOption {
	return func(et *ExecTool) error {
		et.workBaseDir = base
		return nil
	}
}

func WithExecutionPolicy(p ExecutionPolicy) ExecToolOption {
	return func(et *ExecTool) error {
		et.execPolicy = p
		return nil
	}
}

// WithBlockedCommands adds additional commands to the instance blocklist.
// Entries must be single command names (not full command lines).
func WithBlockedCommands(cmds []string) ExecToolOption {
	return func(et *ExecTool) error {
		for _, c := range cmds {
			n, err := executil.NormalizeBlockedCommand(c)
			if err != nil {
				return err
			}
			if n == "" {
				continue
			}
			et.blockedCommands[n] = struct{}{}
			if runtime.GOOS == toolutil.GOOSWindows {
				ext := strings.ToLower(filepath.Ext(n))
				switch ext {
				case ".exe", ".com", ".bat", ".cmd":
					et.blockedCommands[strings.TrimSuffix(n, ext)] = struct{}{}
				}
			}
		}
		return nil
	}
}

func WithSessionTTL(ttl time.Duration) ExecToolOption {
	return func(et *ExecTool) error {
		et.sessions.SetTTL(ttl)
		return nil
	}
}

func WithMaxSessions(maxSessions int) ExecToolOption {
	return func(et *ExecTool) error {
		et.sessions.SetMaxSessions(maxSessions)
		return nil
	}
}

func WithRunScriptPolicy(p RunScriptPolicy) ExecToolOption {
	return func(et *ExecTool) error {
		et.runScriptPolicy = p
		return nil
	}
}

func NewExecTool(opts ...ExecToolOption) (*ExecTool, error) {
	et := &ExecTool{
		allowedRoots:    nil,
		workBaseDir:     "",
		blockedCommands: maps.Clone(executil.HardBlockedCommands),

		execPolicy:      DefaultExecutionPolicy,
		runScriptPolicy: DefaultRunScriptPolicy,

		sessions: executil.NewSessionStore(),
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(et); err != nil {
			return nil, err
		}
	}

	eff, roots, err := fileutil.InitPathPolicy(et.workBaseDir, et.allowedRoots)
	if err != nil {
		return nil, err
	}
	et.workBaseDir = eff
	et.allowedRoots = roots
	return et, nil
}

func (et *ExecTool) WorkBaseDir() string {
	et.mu.RLock()
	defer et.mu.RUnlock()
	return et.workBaseDir
}

func (et *ExecTool) AllowedRoots() []string {
	et.mu.RLock()
	defer et.mu.RUnlock()
	return slices.Clone(et.allowedRoots)
}

// SetAllowedRoots updates allowed roots at runtime (best-effort).
// If the current workBaseDir is not within the new roots, this returns an error and leaves state unchanged.
func (et *ExecTool) SetAllowedRoots(roots []string) error {
	canon, err := fileutil.CanonicalizeAllowedRoots(roots)
	if err != nil {
		return err
	}
	et.mu.Lock()
	defer et.mu.Unlock()
	if _, err := fileutil.GetEffectiveWorkDir(et.workBaseDir, canon); err != nil {
		return err
	}
	et.allowedRoots = canon
	return nil
}

// SetWorkBaseDir updates the work base directory at runtime (best-effort).
func (et *ExecTool) SetWorkBaseDir(base string) error {
	et.mu.RLock()
	roots := slices.Clone(et.allowedRoots)
	et.mu.RUnlock()

	b := strings.TrimSpace(base)
	if b == "" {
		// Mirror InitPathPolicy behavior:
		// if a sandbox is configured, default base to the first allowed root;
		// otherwise default to process CWD.
		if len(roots) > 0 {
			b = roots[0]
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			b = cwd
		}
	}
	eff, err := fileutil.GetEffectiveWorkDir(b, roots)
	if err != nil {
		return err
	}

	et.mu.Lock()
	et.workBaseDir = eff
	et.mu.Unlock()
	return nil
}

func (et *ExecTool) Tools() []spec.Tool {
	return []spec.Tool{et.ShellCommandTool(), et.RunScriptTool()}
}

func (et *ExecTool) RunScriptTool() spec.Tool { return toolutil.CloneTool(runScriptToolSpec) }

func (et *ExecTool) RunScript(ctx context.Context, args RunScriptArgs) (*RunScriptResult, error) {
	return toolutil.WithRecoveryResp(func() (*RunScriptResult, error) {
		base, roots, execPol, blocked, rsPol := et.snapshot()
		return runScript(ctx, args, base, roots, execPol, blocked, rsPol)
	})
}

func (et *ExecTool) ShellCommandTool() spec.Tool { return toolutil.CloneTool(shellCommandToolSpec) }

func (et *ExecTool) ShellCommand(ctx context.Context, args ShellCommandArgs) (*ShellCommandResponse, error) {
	return toolutil.WithRecoveryResp(func() (*ShellCommandResponse, error) {
		base, roots, policy, blocked, _ := et.snapshot()
		return shellCommand(ctx, args, base, roots, policy, blocked, et.sessions)
	})
}

func (et *ExecTool) snapshot() (base string, roots []string, pol ExecutionPolicy, blocked map[string]struct{}, rsPol RunScriptPolicy) {
	et.mu.RLock()
	base = et.workBaseDir
	roots = slices.Clone(et.allowedRoots)
	pol = et.execPolicy
	blocked = et.blockedCommands // treated as immutable after construction/options
	rsPol = et.runScriptPolicy
	et.mu.RUnlock()

	return base, roots, pol, blocked, rsPol
}
