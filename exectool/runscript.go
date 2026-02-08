package exectool

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/flexigpt/llmtools-go/internal/executil"
	"github.com/flexigpt/llmtools-go/internal/fileutil"
	"github.com/flexigpt/llmtools-go/internal/toolutil"
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
		"description": "Path to the script. Can be absolute or relative. If relative and workdir is provided, resolves against workdir; otherwise resolves against the tool workBaseDir."
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

type RunScriptMode string

const (
	// RunScriptModeDirect executes the script path directly (as the "command").
	// This is appropriate for PowerShell scripts executed via "& 'script.ps1' ...".
	RunScriptModeDirect RunScriptMode = "direct"

	// RunScriptModeShell executes the script by invoking the selected wrapper shell as an interpreter:
	//   <shellPath> <script> <args...>
	// This avoids requiring execute bits on Unix shell scripts.
	RunScriptModeShell RunScriptMode = "shell"

	// RunScriptModeInterpreter executes the script via an explicit interpreter command:
	//   <command> <commandArgs...> <script> <args...>
	RunScriptModeInterpreter RunScriptMode = "interpreter"
)

type RunScriptInterpreter struct {
	// Shell selects the wrapper shell used to run the *constructed command string*.
	// This affects quoting dialect and which binary is used for "shell -c" / "-Command".
	Shell ShellName

	Mode RunScriptMode

	// Command is required for ModeInterpreter.
	// Examples: "python3", "python", "node", "ruby".
	Command string
	Args    []string
}

type RunScriptPolicy struct {
	// AllowedExtensions is an optional lowercase allowlist (e.g. [".sh", ".ps1", ".py"]).
	// If empty/nil, extension is allowed iff InterpreterByExtension has a match (or a "" fallback is configured).
	AllowedExtensions []string

	// InterpreterByExtension controls how scripts are executed based on extension.
	// Keys should be lowercase and include the leading dot (".sh", ".ps1", ".py").
	//
	// If no mapping exists for the script extension:
	//   - if a mapping for "" exists, it is used as a fallback
	//   - otherwise runscript fails with "no interpreter mapping for extension"
	InterpreterByExtension map[string]RunScriptInterpreter

	// ExecutionPolicy overrides the ExecTool-wide defaults for runscript.
	// If left zero-valued, ExecTool.execPolicy is used.
	ExecutionPolicy ExecutionPolicy

	// Arg limits (defense-in-depth).
	MaxArgs     int
	MaxArgBytes int
}

var DefaultRunScriptPolicy = func() RunScriptPolicy {
	pyCmd := "python3"
	pyShell := ShellNameSh
	if runtime.GOOS == toolutil.GOOSWindows {
		pyCmd = "python"
		pyShell = ShellNamePowershell
	}
	return RunScriptPolicy{
		AllowedExtensions: []string{".sh", ".bash", ".zsh", ".ksh", ".dash", ".ps1", ".py"},
		InterpreterByExtension: map[string]RunScriptInterpreter{
			// Shell scripts: run via the wrapper shell path as interpreter.
			".sh":   {Shell: ShellNameSh, Mode: RunScriptModeShell},
			".bash": {Shell: ShellNameBash, Mode: RunScriptModeShell},
			".zsh":  {Shell: ShellNameZsh, Mode: RunScriptModeShell},
			".ksh":  {Shell: ShellNameKsh, Mode: RunScriptModeShell},
			".dash": {Shell: ShellNameDash, Mode: RunScriptModeShell},

			// PowerShell: execute the script directly via PowerShell dialect ("& 'script.ps1' ...").
			".ps1": {Shell: ShellNamePowershell, Mode: RunScriptModeDirect},

			// Python: interpreter-based.
			".py": {Shell: pyShell, Mode: RunScriptModeInterpreter, Command: pyCmd},
		},
		ExecutionPolicy: ExecutionPolicy{}, // inherit from ExecTool by default
		MaxArgs:         256,
		MaxArgBytes:     16 * 1024,
	}
}()

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

	// Resolve script path:
	// - relative => workBaseDir
	// - absolute => must still be within allowedRoots (if configured).
	// If relative and workdir provided, resolve relative to workdir.
	baseForScript := workBaseDir
	if strings.TrimSpace(args.Workdir) != "" && !filepath.IsAbs(reqPath) {
		baseForScript = workdirAbs
	}
	scriptAbs, err := fileutil.ResolvePath(baseForScript, allowedRoots, reqPath, "")
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

	interp, ok := lookupInterpreter(pol, ext)
	if !ok {
		return nil, fmt.Errorf("no interpreter mapping for extension %q", ext)
	}

	// Select wrapper shell (concrete shell needed for quoting + execution).
	sel, err := selectShell(interp.Shell)
	if err != nil {
		return nil, err
	}

	// Build argv based on mode.
	var argv []string
	switch interp.Mode {
	case RunScriptModeDirect:
		// Execute script path directly.
		argv = append([]string{scriptAbs}, args.Args...)
	case RunScriptModeShell:
		// Use the wrapper shell binary as the interpreter.
		argv = append([]string{sel.Path, scriptAbs}, args.Args...)
	case RunScriptModeInterpreter:
		cmd := strings.TrimSpace(interp.Command)
		if cmd == "" {
			return nil, errors.New("invalid interpreter mapping: empty command")
		}
		argv = append(argv, cmd)
		argv = append(argv, interp.Args...)
		argv = append(argv, scriptAbs)
		argv = append(argv, args.Args...)
	default:
		return nil, fmt.Errorf("invalid interpreter mode: %q", interp.Mode)
	}

	// Convert argv into a safely-quoted command string for the selected wrapper shell.
	cmdStr, err := executil.CommandFromArgv(sel.Name, argv)
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

func lookupInterpreter(pol RunScriptPolicy, ext string) (RunScriptInterpreter, bool) {
	if pol.InterpreterByExtension == nil {
		return RunScriptInterpreter{}, false
	}
	// Exact match.
	if v, ok := pol.InterpreterByExtension[strings.ToLower(ext)]; ok {
		return v, true
	}
	// Optional fallback for extension-less scripts / shebang-style files.
	if v, ok := pol.InterpreterByExtension[""]; ok {
		return v, true
	}
	return RunScriptInterpreter{}, false
}
