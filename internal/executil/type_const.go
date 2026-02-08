package executil

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
