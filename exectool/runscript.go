package exectool

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flexigpt/llmtools-go/internal/executil"
	"github.com/flexigpt/llmtools-go/internal/fileutil"
	"github.com/flexigpt/llmtools-go/spec"
)

const runScriptFuncID spec.FuncID = "github.com/flexigpt/llmtools-go/exectool/runscript.RunScript"

var runScriptToolSpec = spec.Tool{
	SchemaVersion: spec.SchemaVersion,
	ID:            "01j0b303-0d6a-7a9f-b52c-2a7ac3f3b7f4",
	Slug:          "runscript",
	Version:       "v1.0.0",
	DisplayName:   "Run Script",
	Description:   "Run a pre-existing script from disk.",
	Tags:          []string{"exec", "script"},

	ArgSchema: spec.JSONSchema(`{
"$schema": "http://json-schema.org/draft-07/schema#",
"type": "object",
"properties": {
	"path": {
		"type": "string",
		"description": "Path to the script. Can be absolute or relative. Relative paths try to resolve against input workdir if provided, else resolves against the tool workBaseDir."
	},
	"args": {
		"type": "array",
		"items": { "type": "string" },
		"description": "Arguments passed to the script."
	},
	"env": {
		"type": "object",
		"additionalProperties": { "type": "string" },
		"description": "Environment variable overrides (merged into the process env)."
	},
	"workdir": {
		"type": "string",
		"description": "Working directory. Can be absolute or relative to workBaseDir."
	}
},
"required": ["path"],
"additionalProperties": false
}`),
	GoImpl: spec.GoToolImpl{FuncID: runScriptFuncID},

	CreatedAt:  spec.SchemaStartTime,
	ModifiedAt: spec.SchemaStartTime,
}

type RunScriptArgs struct {
	Path    string            `json:"path"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Workdir string            `json:"workdir,omitempty"`
}

type RunScriptResult struct {
	Path       string `json:"path"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`

	StdoutTruncated bool `json:"stdout_truncated,omitempty"`
	StderrTruncated bool `json:"stderr_truncated,omitempty"`
}

type RunScriptPolicy struct {
	// RequireUnderDir, if non-empty, requires args.Path to be under this relative directory (default: "scripts").
	RequireUnderDir string

	// On Windows, a ".sh" script may still work if "sh" exists (e.g. Git Bash), but extension mapping above is
	// conservative.
	// If needed, host apps can expand RunScriptPolicy.AllowedExtensions and add more interpreter mappings later.

	// AllowedExtensions is a lowercase allowlist (e.g. [".sh", ".ps1"]).
	// If empty/nil, extension is not restricted (but interpreter inference may still fail).
	AllowedExtensions []string

	// ExecutionPolicy overrides the ExecTool-wide defaults for runscript.
	// If left zero-valued, ExecTool.execPolicy is used.
	ExecutionPolicy ExecutionPolicy

	// Arg limits (defense-in-depth).
	MaxArgs     int
	MaxArgBytes int
}

var DefaultRunScriptPolicy = RunScriptPolicy{
	AllowedExtensions: []string{".sh", ".bash", ".zsh", ".ksh", ".dash", ".ps1"},
	ExecutionPolicy:   ExecutionPolicy{}, // inherit from ExecTool by default
	MaxArgs:           256,
	MaxArgBytes:       16 * 1024,
}

func runScript(
	ctx context.Context,
	args RunScriptArgs,
	workBaseDir string,
	allowedRoots []string,
	defaultExecPol ExecutionPolicy,
	blocked map[string]struct{},
	pol RunScriptPolicy,
) (*RunScriptResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	reqPath := strings.TrimSpace(args.Path)
	if reqPath == "" {
		return nil, errors.New("path is required")
	}
	// Resolve script path:
	// - relative => workBaseDir
	// - absolute => must still be within allowedRoots (if configured).
	scriptAbs, err := fileutil.ResolvePath(workBaseDir, allowedRoots, reqPath, "")
	if err != nil {
		return nil, err
	}

	// Require existing, regular, non-symlink file and refuse symlink traversal in parents.
	if _, err := fileutil.RequireExistingRegularFileNoSymlink(scriptAbs); err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(scriptAbs))
	if len(pol.AllowedExtensions) != 0 && !extAllowed(ext, pol.AllowedExtensions) {
		return nil, fmt.Errorf("script extension %q is not allowed", ext)
	}

	// Workdir: absolute or relative; default to workBaseDir.
	workdirAbs, err := fileutil.ResolvePath(workBaseDir, allowedRoots, args.Workdir, workBaseDir)
	if err != nil {
		return nil, err
	}
	workdirAbs, err = fileutil.GetEffectiveWorkDir(workdirAbs, allowedRoots)
	if err != nil {
		return nil, err
	}
	if err := fileutil.VerifyDirNoSymlink(workdirAbs); err != nil {
		return nil, err
	}

	// Validate env + args.
	if err := executil.ValidateEnvMap(args.Env); err != nil {
		return nil, err
	}
	if pol.MaxArgs > 0 && len(args.Args) > pol.MaxArgs {
		return nil, fmt.Errorf("too many args: %d (max %d)", len(args.Args), pol.MaxArgs)
	}
	maxArgBytes := pol.MaxArgBytes
	if maxArgBytes <= 0 {
		maxArgBytes = 16 * 1024
	}
	for i, a := range args.Args {
		if strings.ContainsRune(a, '\x00') {
			return nil, fmt.Errorf("args[%d] contains NUL byte", i)
		}
		if len(a) > maxArgBytes {
			return nil, fmt.Errorf("args[%d] too long", i)
		}
	}

	// Choose shell based on extension and build a safely-quoted command using executil.CommandFromArgv.
	requestedShell, err := shellForScriptExt(ext)
	if err != nil {
		return nil, err
	}
	sel, err := selectShell(requestedShell)
	if err != nil {
		return nil, err
	}
	cmdStr, err := buildScriptCommand(sel, scriptAbs, args.Args)
	if err != nil {
		return nil, err
	}

	// Effective execution policy: runscript overrides or inherit ExecTool default.
	execPol := pol.ExecutionPolicy
	if execPol.Timeout == 0 && execPol.MaxOutputBytes == 0 && execPol.MaxCommands == 0 &&
		execPol.MaxCommandLength == 0 &&
		!execPol.AllowDangerous {
		execPol = defaultExecPol
	}

	timeout := effectiveTimeout(execPol)
	maxOut := effectiveMaxOutputBytes(execPol)

	// Merge env like shellcommand does (process env + overrides), but no session.
	env, err := executil.EffectiveEnv(args.Env)
	if err != nil {
		return nil, err
	}

	// Apply the same outer-command checks (blocklist always, heuristics optional).
	if err := executil.RejectDangerousCommand(
		cmdStr,
		sel.Path,
		sel.Name,
		blocked,
		!execPol.AllowDangerous,
	); err != nil {
		return nil, err
	}

	res, runErr := executil.RunOneShellCommand(ctx, sel, cmdStr, workdirAbs, env, timeout, maxOut)
	if runErr != nil {
		return &RunScriptResult{ //nolint:nilerr // For shell exec, we return a exit code on err.
			Path:     scriptAbs,
			ExitCode: 127,
			Stderr:   runErr.Error(),
		}, nil
	}

	return &RunScriptResult{
		Path:       scriptAbs,
		ExitCode:   res.ExitCode,
		Stdout:     res.Stdout,
		Stderr:     res.Stderr,
		TimedOut:   res.TimedOut,
		DurationMS: res.DurationMS,

		StdoutTruncated: res.StdoutTruncated,
		StderrTruncated: res.StderrTruncated,
	}, nil
}

func extAllowed(ext string, allowed []string) bool {
	if ext == "" {
		return false
	}
	if len(allowed) == 0 {
		return false
	}
	x := strings.ToLower(ext)
	for _, a := range allowed {
		if strings.ToLower(strings.TrimSpace(a)) == x {
			return true
		}
	}
	return false
}

func shellForScriptExt(ext string) (ShellName, error) {
	switch strings.ToLower(ext) {
	case ".ps1":
		// Request "powershell" so resolveShell prefers pwsh but can fall back to Windows PowerShell.
		return ShellNamePowershell, nil
	case ".sh", ".bash", ".zsh", ".ksh", ".dash":
		return ShellNameSh, nil
	default:
		return "", fmt.Errorf("no shell mapping for script extension %q", ext)
	}
}

func buildScriptCommand(sel executil.SelectedShell, scriptAbs string, scriptArgs []string) (string, error) {
	switch sel.Name {
	case ShellNamePwsh, ShellNamePowershell:
		// In PowerShell, run the script via call operator.
		argv := append([]string{scriptAbs}, scriptArgs...)
		return executil.CommandFromArgv(sel.Name, argv)
	default:
		// In sh-like shells, invoke the selected shell binary as the interpreter.
		// This avoids requiring the script file to have executable bits.
		// (Yes, this spawns an extra shell layer; it's deliberate for consistent behavior.)
		argv := append([]string{sel.Path, scriptAbs}, scriptArgs...)
		return executil.CommandFromArgv(sel.Name, argv)
	}
}
