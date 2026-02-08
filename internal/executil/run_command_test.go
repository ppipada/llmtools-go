package executil

import (
	"os/exec"
	"runtime"
	"testing"

	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

func TestUnixSpecific_ProcessGroupAndExitCodeHelpers(t *testing.T) {
	if runtime.GOOS == toolutil.GOOSWindows {
		t.Skip("unix-specific")
	}

	// "configureProcessGroup" should set Setpgid=true.
	cmd := exec.CommandContext(t.Context(), "sh", "-c", "exit 0")
	configureProcessGroup(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatalf("expected SysProcAttr set")
	}

	// "exitCodeFromProcessState" should reflect normal exit and signaled exit.
	c := exec.CommandContext(t.Context(), "sh", "-c", "exit 9")
	if err := c.Run(); err == nil {
		t.Fatalf("expected non-zero exit")
	}
	if got := exitCodeFromProcessState(c.ProcessState); got != 9 {
		t.Fatalf("expected exit code 9, got %d", got)
	}

	c = exec.CommandContext(t.Context(), "sh", "-c", "kill -9 $$")
	_ = c.Run() // expect error
	if got := exitCodeFromProcessState(c.ProcessState); got != 137 {
		t.Fatalf("expected signaled exit code 137, got %d", got)
	}

	// "killProcessGroup" should be safe on nils.
	killProcessGroup(nil)
	killProcessGroup(&exec.Cmd{})
}
