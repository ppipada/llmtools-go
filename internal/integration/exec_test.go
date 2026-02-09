package integration

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/flexigpt/llmtools-go/exectool"
	"github.com/flexigpt/llmtools-go/fstool"
	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

// Shell sessions + env persistence + runscript.

func TestE2E_Exec_ShellCommand_SessionEnvWorkdir(t *testing.T) {
	base := t.TempDir()
	h := newHarness(t, base)

	var shell exectool.ShellName
	var cmds1, cmds2, cmds3 []string
	var expectEnv1, expectEnv2 string

	if runtime.GOOS == toolutil.GOOSWindows {
		shell = exectool.ShellNamePowershell
		cmds1 = []string{
			`Set-Content -NoNewline -Path "shell.txt" -Value "from-shell"`,
			`Write-Output $env:MYVAR`,
		}
		cmds2 = []string{
			`Write-Output $env:MYVAR`,
		}
		cmds3 = []string{
			`Write-Output $env:MYVAR`,
		}
		expectEnv1 = "hello"
		expectEnv2 = "bye"
	} else {
		shell = exectool.ShellNameSh
		cmds1 = []string{
			`printf "%s" "from-shell" > shell.txt`,
			`echo "$MYVAR"`,
		}
		cmds2 = []string{`echo "$MYVAR"`}
		cmds3 = []string{`echo "$MYVAR"`}
		expectEnv1 = "hello"
		expectEnv2 = "bye"
	}

	// 1) First call: create session + set env.
	resp1 := callJSON[exectool.ShellCommandResponse](t, h.r, "shellcommand", exectool.ShellCommandArgs{
		Shell:     shell,
		Commands:  cmds1,
		Env:       map[string]string{"MYVAR": "hello"},
		SessionID: "",
	})
	if strings.TrimSpace(resp1.SessionID) == "" {
		t.Fatalf("expected sessionID, got: %s", debugJSON(t, resp1))
	}
	if len(resp1.Results) < 2 {
		t.Fatalf("expected >=2 results, got: %s", debugJSON(t, resp1))
	}
	if !strings.Contains(resp1.Results[len(resp1.Results)-1].Stdout, expectEnv1) {
		t.Fatalf("expected stdout to contain %q, got: %s", expectEnv1, debugJSON(t, resp1.Results))
	}

	// Verify file created via shell by reading it with readfile.
	out := callRaw(t, h.r, "readfile", fstool.ReadFileArgs{Path: "shell.txt", Encoding: "text"})
	got := requireSingleTextOutput(t, out)
	if got != "from-shell" {
		t.Fatalf("shell.txt content mismatch: got=%q want=%q", got, "from-shell")
	}

	// 2) Second call: same session, no env provided => session env should persist.
	resp2 := callJSON[exectool.ShellCommandResponse](t, h.r, "shellcommand", exectool.ShellCommandArgs{
		Shell:     shell,
		Commands:  cmds2,
		SessionID: resp1.SessionID,
	})
	if !strings.Contains(resp2.Results[0].Stdout, expectEnv1) {
		t.Fatalf("expected persisted env stdout to contain %q, got: %s", expectEnv1, debugJSON(t, resp2))
	}

	// 3) Third call: override env, and that override becomes part of session state.
	resp3 := callJSON[exectool.ShellCommandResponse](t, h.r, "shellcommand", exectool.ShellCommandArgs{
		Shell:     shell,
		Commands:  cmds2,
		Env:       map[string]string{"MYVAR": "bye"},
		SessionID: resp1.SessionID,
	})
	if !strings.Contains(resp3.Results[0].Stdout, expectEnv2) {
		t.Fatalf("expected overridden env stdout to contain %q, got: %s", expectEnv2, debugJSON(t, resp3))
	}

	// 4) Fourth call: no env passed => should still be "bye".
	resp4 := callJSON[exectool.ShellCommandResponse](t, h.r, "shellcommand", exectool.ShellCommandArgs{
		Shell:     shell,
		Commands:  cmds3,
		SessionID: resp1.SessionID,
	})
	if !strings.Contains(resp4.Results[0].Stdout, expectEnv2) {
		t.Fatalf("expected session env stdout to contain %q, got: %s", expectEnv2, debugJSON(t, resp4))
	}
}

func TestE2E_Exec_RunScript(t *testing.T) {
	base := t.TempDir()

	// Make runscript robust on Windows by forcing .ps1 to run with ExecutionPolicy Bypass.
	var execOpts []exectool.ExecToolOption
	if runtime.GOOS == toolutil.GOOSWindows {
		pol := exectool.DefaultRunScriptPolicy()
		if pol.InterpreterByExtension == nil {
			pol.InterpreterByExtension = map[string]exectool.RunScriptInterpreter{}
		}
		pol.InterpreterByExtension[".ps1"] = exectool.RunScriptInterpreter{
			Shell:   exectool.ShellNamePwsh,
			Mode:    exectool.RunScriptModeInterpreter,
			Command: "powershell",
			Args:    []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File"},
		}
		execOpts = append(execOpts, exectool.WithRunScriptPolicy(pol))
	}

	h := newHarness(t, base, execOpts...)

	scriptsDirRel := "scripts"

	if runtime.GOOS == toolutil.GOOSWindows {
		scriptRel := filepath.Join(scriptsDirRel, "hello.ps1")
		content := "" +
			"param([string]$Name)\n" +
			"Write-Output \"NAME=$Name\"\n" +
			"Write-Output \"ENV=$env:MYVAR\"\n"

		_ = callJSON[fstool.WriteFileOut](t, h.r, "writefile", fstool.WriteFileArgs{
			Path:          scriptRel,
			Encoding:      "text",
			Content:       content,
			CreateParents: true,
		})

		res := callJSON[exectool.RunScriptResult](t, h.r, "runscript", exectool.RunScriptArgs{
			Path:    "hello.ps1",
			Args:    []string{"world"},
			Env:     map[string]string{"MYVAR": "hello"},
			Workdir: scriptsDirRel,
		})
		if res.ExitCode != 0 {
			t.Fatalf("runscript exit != 0: %s", debugJSON(t, res))
		}
		if !strings.Contains(res.Stdout, "NAME=world") || !strings.Contains(res.Stdout, "ENV=hello") {
			t.Fatalf("unexpected stdout: %s", debugJSON(t, res))
		}
	} else {
		scriptRel := filepath.Join(scriptsDirRel, "hello.sh")
		content := "" +
			"echo \"NAME=$1\"\n" +
			"echo \"ENV=$MYVAR\"\n"

		_ = callJSON[fstool.WriteFileOut](t, h.r, "writefile", fstool.WriteFileArgs{
			Path:          scriptRel,
			Encoding:      "text",
			Content:       content,
			CreateParents: true,
		})

		res := callJSON[exectool.RunScriptResult](t, h.r, "runscript", exectool.RunScriptArgs{
			Path:    "hello.sh",
			Args:    []string{"world"},
			Env:     map[string]string{"MYVAR": "hello"},
			Workdir: scriptsDirRel,
		})
		if res.ExitCode != 0 {
			t.Fatalf("runscript exit != 0: %s", debugJSON(t, res))
		}
		if !strings.Contains(res.Stdout, "NAME=world") || !strings.Contains(res.Stdout, "ENV=hello") {
			t.Fatalf("unexpected stdout: %s", debugJSON(t, res))
		}
	}
}
