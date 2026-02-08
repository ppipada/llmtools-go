package executil

import (
	"errors"
	"strings"
)

// CommandFromArgv builds a safely-quoted command string for ShellTool (which executes via shell "-c"/"-Command").
//
// Why this exists:
// ShellTool accepts command strings; without a canonical helper, wrappers duplicate quoting and can
// accidentally introduce injection bugs. This keeps quoting logic centralized and consistent.
//
// Supported dialects:
//   - sh-like shells (bash/zsh/sh/dash/ksh/fish): POSIX single-quote strategy
//   - PowerShell (pwsh/powershell): single-quote strategy + "&" call operator prefix
//
// cmd.exe quoting is intentionally not supported here (too error-prone); pass a raw command string instead.
func CommandFromArgv(selectedShell ShellName, argv []string) (string, error) {
	if len(argv) == 0 || strings.TrimSpace(string(selectedShell)) == "" || selectedShell == ShellNameAuto {
		// We require a already selected concrete shell (not auto).
		return "", errors.New("invalid args to CommandFromArgv")
	}

	switch dialectForShell(selectedShell) {
	case dialectCmd:
		return "", errors.New("CommandFromArgv does not support cmd.exe quoting; pass a raw command string")
	case dialectPowerShell:
		parts := make([]string, 0, 1+len(argv))
		parts = append(parts, "&")
		for _, a := range argv {
			q, err := quotePowerShell(a)
			if err != nil {
				return "", err
			}
			parts = append(parts, q)
		}
		return strings.Join(parts, " "), nil
	default: // sh-like
		parts := make([]string, 0, len(argv))
		for _, a := range argv {
			q, err := quotePOSIX(a)
			if err != nil {
				return "", err
			}
			parts = append(parts, q)
		}
		return strings.Join(parts, " "), nil
	}
}

func quotePowerShell(s string) (string, error) {
	if strings.ContainsRune(s, '\x00') {
		return "", errors.New("arg contains NUL byte")
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
}

func quotePOSIX(s string) (string, error) {
	if strings.ContainsRune(s, '\x00') {
		return "", errors.New("arg contains NUL byte")
	}
	if s == "" {
		return "''", nil
	}
	if !strings.ContainsRune(s, '\'') {
		return "'" + s + "'", nil
	}
	// 'foo'"'"'bar'.
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'", nil
}
