package executil

import "time"

// Fixed, package-wide hard limits (single source of truth).
const (
	HardMaxTimeout             = 10 * time.Minute
	HardMaxOutputBytes   int64 = 4 * 1024 * 1024 // per stream
	HardMaxCommands            = 64
	HardMaxCommandLength       = 64 * 1024 // bytes
	MinOutputBytes       int64 = 1024

	DefaultTimeout                = 60 * time.Second
	DefaultMaxOutputBytes   int64 = 256 * 1024
	DefaultMaxCommands            = 64
	DefaultMaxCommandLength       = 64 * 1024

	defaultSessionTTL  = 30 * time.Minute
	defaultMaxSessions = 256

	hardMaxEnvVars       = 256
	hardMaxEnvKeyBytes   = 256
	hardMaxEnvValueBytes = 32 * 1024
	hardMaxEnvTotalBytes = 256 * 1024
)

type ShellName string

const (
	ShellNameAuto       ShellName = "auto"
	ShellNameBash       ShellName = "bash"
	ShellNameZsh        ShellName = "zsh"
	ShellNameSh         ShellName = "sh"
	ShellNameDash       ShellName = "dash"
	ShellNameKsh        ShellName = "ksh"
	ShellNameFish       ShellName = "fish"
	ShellNamePwsh       ShellName = "pwsh"
	ShellNamePowershell ShellName = "powershell"
	ShellNameCmd        ShellName = "cmd"
)

type shellDialect int

const (
	dialectSh shellDialect = iota
	dialectPowerShell
	dialectCmd
)

type SelectedShell struct {
	Name ShellName
	Path string
}

type ShellCommandExecResult struct {
	Command   string    `json:"command"`
	WorkDir   string    `json:"workDir"`
	Shell     ShellName `json:"shell"`
	ShellPath string    `json:"shellPath"`

	ExitCode   int   `json:"exitCode"`
	TimedOut   bool  `json:"timedOut"`
	DurationMS int64 `json:"durationMS"`

	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`

	StdoutTruncated bool `json:"stdoutTruncated"`
	StderrTruncated bool `json:"stderrTruncated"`
}

var HardBlockedCommands = func() map[string]struct{} {
	// Non-overridable baseline. These are blocked regardless of AllowDangerous.
	hard := []string{
		// Privilege escalation / destructive.
		"sudo", "su",
		"rm",
		"mkfs",
		"shutdown", "reboot", "halt", "poweroff",

		// Interactive/TUI editors (not useful in non-interactive tool).
		"vim", "vi", "nano", "emacs", "less", "more", "top", "htop",

		// Network/communication tools.
		"curl", "wget",
		"nc", "netcat", "ncat", "socat",
		"ssh", "scp", "sftp",
		"ftp", "tftp", "telnet",

		// PowerShell network cmdlets/aliases (relevant when shell is pwsh/powershell).
		"invoke-webrequest", "iwr",
		"invoke-restmethod", "irm",

		// Windows destructive / deletion equivalents (also harmless to block on unix).
		"diskpart",
		"format.com",
		"del", "erase", "rmdir", "rd",
		"remove-item", "ri",
	}

	m := make(map[string]struct{}, len(hard))
	for _, c := range hard {
		m[c] = struct{}{}
	}
	return m
}()
