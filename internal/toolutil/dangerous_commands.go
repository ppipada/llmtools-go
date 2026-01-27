package toolutil

import (
	"errors"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unicode"
)

const (
	goosWindows  = "windows"
	shellNameCmd = "cmd"
)

func RejectDangerousCommand(cmd, shellName, shellPath string) error {
	c := strings.TrimSpace(cmd)
	if c == "" {
		return nil
	}

	// Cheap whole-input checks first.
	if looksLikeForkBomb(c) {
		return errors.New("blocked dangerous command pattern (fork bomb)")
	}
	if hasBackgroundAmpersand(c) {
		return errors.New("blocked backgrounding with '&' (leaks processes)")
	}

	// Scan each "command segment" separated by ";, &&, ||, |, &, newline, (, )".
	return forEachSegment(c, func(seg string) error {
		toks := shellFields(seg, runtime.GOOS == goosWindows)
		if len(toks) == 0 {
			return nil
		}

		name, args := unwrapCommand(toks)
		if name == "" {
			return nil
		}

		// UNIX / cross-platform.
		switch name {
		case "sudo", "su":
			return errors.New("blocked privileged escalation (sudo/su)")

		case "rm":
			if rmIsDangerous(args) {
				return errors.New("blocked destructive rm on root-like path")
			}

		case "mkfs":
			return errors.New("blocked filesystem formatting command (mkfs)")

		case "shutdown", "reboot", "halt", "poweroff":
			return errors.New("blocked shutdown/reboot/poweroff command")

		case "vim", "vi", "nano", "emacs", "less", "more", "top", "htop":
			return errors.New("blocked interactive TUI/editor command (non-interactive tool)")
		}
		if strings.HasPrefix(name, "mkfs.") {
			return errors.New("blocked filesystem formatting command (mkfs)")
		}

		// Windows-only.
		if runtime.GOOS == goosWindows {
			switch name {
			case "diskpart", "diskpart.exe":
				return errors.New("blocked destructive disk operation (diskpart)")
			case "format.com":
				return errors.New("blocked destructive disk operation (format.com)")
			}
			if shellName == shellNameCmd && (name == "format" || name == "format.exe") {
				return errors.New("blocked destructive disk operation (format)")
			}
		}

		return nil
	})
}

func forEachSegment(s string, fn func(seg string) error) error {
	inS, inD := false, false
	esc := false
	start := 0

	emit := func(end int) error {
		seg := strings.TrimSpace(s[start:end])
		start = end
		if seg == "" {
			return nil
		}
		return fn(seg)
	}

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if esc {
			esc = false
			continue
		}
		if !inS && ch == '\\' {
			esc = true
			continue
		}
		if !inD && ch == '\'' {
			inS = !inS
			continue
		}
		if !inS && ch == '"' {
			inD = !inD
			continue
		}
		if inS || inD {
			continue
		}

		// Comment start: " #...".
		if ch == '#' && (i == 0 || unicode.IsSpace(rune(s[i-1]))) {
			if err := emit(i); err != nil {
				return err
			}
			return nil
		}

		// Separators.
		if ch == ';' || ch == '\n' || ch == '(' || ch == ')' {
			if err := emit(i); err != nil {
				return err
			}
			start = i + 1
			continue
		}

		// "|| and |".
		if ch == '|' {
			if err := emit(i); err != nil {
				return err
			}
			if i+1 < len(s) && s[i+1] == '|' { //nolint:gocritic // No switch here.
				start = i + 2
				i++
			} else if i+1 < len(s) && s[i+1] == '&' { // |&
				start = i + 2
				i++
			} else {
				start = i + 1
			}
			continue
		}

		// "&&"" (single & is handled by hasBackgroundAmpersand before we get here).
		if ch == '&' && i+1 < len(s) && s[i+1] == '&' {
			if err := emit(i); err != nil {
				return err
			}
			start = i + 2
			i++
			continue
		}
	}

	return emit(len(s))
}

func shellFields(s string, windowsMode bool) []string {
	var out []string
	var b strings.Builder
	inS, inD := false, false
	esc := false

	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, b.String())
		b.Reset()
	}

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if esc {
			b.WriteByte(ch)
			esc = false
			continue
		}
		if !windowsMode && !inS && ch == '\\' {
			esc = true
			continue
		}
		if !inD && ch == '\'' {
			inS = !inS
			continue
		}
		if !inS && ch == '"' {
			inD = !inD
			continue
		}

		if !inS && !inD && unicode.IsSpace(rune(ch)) {
			flush()
			continue
		}
		b.WriteByte(ch)
	}
	flush()
	return out
}

func unwrapCommand(tokens []string) (name string, args []string) {
	i := 0
	for i < len(tokens) && isEnvAssignment(tokens[i]) {
		i++
	}
	if i >= len(tokens) {
		return "", nil
	}

	cmd := canonicalCmd(tokens[i])
	rest := tokens[i+1:]

	for {
		switch cmd {
		case "env":
			j := 0
			for j < len(rest) {
				a := rest[j]
				if a == "-i" || a == "--ignore-environment" || isEnvAssignment(a) || strings.HasPrefix(a, "-") {
					j++
					continue
				}
				break
			}
			if j >= len(rest) {
				return cmd, rest
			}
			cmd = canonicalCmd(rest[j])
			rest = rest[j+1:]
			continue

		case "command", "builtin":
			j := 0
			for j < len(rest) && strings.HasPrefix(rest[j], "-") {
				j++
			}
			if j >= len(rest) {
				return cmd, rest
			}
			cmd = canonicalCmd(rest[j])
			rest = rest[j+1:]
			continue
		}
		break
	}

	return cmd, rest
}

func canonicalCmd(tok string) string {
	return strings.ToLower(filepath.Base(strings.TrimSpace(tok)))
}

func isEnvAssignment(tok string) bool {
	eq := strings.IndexByte(tok, '=')
	if eq <= 0 {
		return false
	}
	for i := range eq {
		c := tok[i]
		if c != '_' &&
			(c < 'a' || c > 'z') &&
			(c < 'A' || c > 'Z') &&
			(i <= 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func looksLikeForkBomb(s string) bool {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if !unicode.IsSpace(rune(s[i])) {
			b = append(b, s[i])
		}
	}
	return strings.Contains(string(b), ":(){:|:&};:")
}

// Only reject actual backgrounding/chaining '&'. Avoid "2>&1, &>file, |&".
func hasBackgroundAmpersand(s string) bool {
	inS, inD := false, false
	esc := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if esc {
			esc = false
			continue
		}
		if !inS && ch == '\\' {
			esc = true
			continue
		}
		if !inD && ch == '\'' {
			inS = !inS
			continue
		}
		if !inS && ch == '"' {
			inD = !inD
			continue
		}
		if inS || inD {
			continue
		}

		if ch != '&' {
			continue
		}
		// Allow "&&".
		if i+1 < len(s) && s[i+1] == '&' {
			i++
			continue
		}
		// Allow redirections: "&>file, 2>&1, |&".
		if (i+1 < len(s) && s[i+1] == '>') || (i > 0 && (s[i-1] == '>' || s[i-1] == '<' || s[i-1] == '|')) {
			continue
		}
		return true
	}
	return false
}

// Paranoid simple rm rule: block any recursive rm targeting "/" or "/*".
func rmIsDangerous(args []string) bool {
	recursive := false
	inOpts := true
	var targets []string

	for _, a := range args {
		if inOpts && a == "--" {
			inOpts = false
			continue
		}
		if inOpts && strings.HasPrefix(a, "-") && a != "-" {
			if a == "--recursive" {
				recursive = true
			} else if !strings.HasPrefix(a, "--") && strings.ContainsAny(a, "rR") {
				recursive = true // -r, -R, -fr, -rf, etc
			}
			continue
		}
		inOpts = false
		targets = append(targets, a)
	}

	if !recursive {
		return false
	}
	return slices.ContainsFunc(targets, isUnixRootLike)
}

func isUnixRootLike(arg string) bool {
	if arg == "" {
		return false
	}
	if strings.HasPrefix(arg, "/*") {
		return true
	}
	if strings.HasPrefix(arg, "/") && path.Clean(arg) == "/" {
		return true
	}
	return arg == "/"
}
