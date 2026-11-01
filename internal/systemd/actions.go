package systemd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Command is one program run, shown to the user before it runs.
type Command []string

func (c Command) String() string {
	q := make([]string, len(c))
	for i, a := range c {
		q[i] = Quote(a)
	}
	return strings.Join(q, " ")
}

// Quote quotes a word for a POSIX shell when it needs it.
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@%+=:,./-_", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// NeedsRoot reports whether changing system units needs sudo.
func NeedsRoot(s Scope) bool { return s == System && os.Geteuid() != 0 }

// Systemctl builds a systemctl command for the scope, with sudo when
// changing system state as a regular user.
func Systemctl(s Scope, args ...string) Command {
	c := Command{"systemctl"}
	if s == User {
		c = append(c, "--user")
	}
	c = append(c, args...)
	if NeedsRoot(s) && mutates(args) {
		c = append(Command{"sudo"}, c...)
	}
	return c
}

func mutates(args []string) bool {
	for _, a := range args {
		switch a {
		case "status", "cat", "show", "list-unit-files", "list-units", "is-enabled", "is-active", "list-dependencies":
			return false
		}
	}
	return true
}

// Journal shows a unit's log.
func Journal(s Scope, unit string) Command {
	c := Command{"journalctl"}
	if s == User {
		c = append(c, "--user")
	}
	return append(c, "-u", unit, "-e", "--no-hostname")
}

// ErrNeedsRoot is returned by Write when the file can only be written
// with elevated rights; the caller installs it with InstallCommand.
var ErrNeedsRoot = errors.New("permission denied")

// Write saves content to path, creating its directory. It keeps a backup
// of the previous version as path~ the first time the file is changed.
func Write(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return ErrNeedsRoot
		}
		return err
	}
	if old, err := os.ReadFile(path); err == nil && string(old) != string(content) {
		if _, err := os.Stat(path + "~"); errors.Is(err, fs.ErrNotExist) {
			_ = os.WriteFile(path+"~", old, 0o644)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return ErrNeedsRoot
		}
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return ErrNeedsRoot
		}
		return err
	}
	return nil
}

// Stage writes content to a temporary file for InstallCommand.
func Stage(content []byte) (string, error) {
	f, err := os.CreateTemp("", "sysdtui-*.unit")
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.Write(content)
	return f.Name(), err
}

// InstallCommand copies a staged file into place as root, creating the
// directory and keeping a backup of the old file.
func InstallCommand(staged, path string) Command {
	script := `set -e; mkdir -p "$(dirname "$2")"; if [ -f "$2" ] && [ ! -e "$2~" ]; then cp -p "$2" "$2~"; fi; install -m 0644 "$1" "$2"`
	return Command{"sudo", "sh", "-c", script, "sh", staged, path}
}

// Script builds a shell script that shows and runs each command, stops at
// the first failure and waits for Enter, so the user can read the output
// before the editor comes back.
func Script(cmds []Command, cleanup []string) string {
	var b strings.Builder
	b.WriteString("run() { printf '\\n\\033[1m$ %s\\033[0m\\n' \"$*\"; \"$@\"; }\n")
	b.WriteString("ok=1\n")
	for _, c := range cmds {
		fmt.Fprintf(&b, "[ $ok = 1 ] && { run %s || ok=0; }\n", c)
	}
	for _, f := range cleanup {
		fmt.Fprintf(&b, "rm -f %s\n", Quote(f))
	}
	b.WriteString("[ $ok = 1 ] || printf '\\n\\033[31mA command failed.\\033[0m\\n'\n")
	b.WriteString("printf '\\nPress Enter to return to sysdtui… '; read _\n")
	b.WriteString("[ $ok = 1 ]\n")
	return b.String()
}

// Verify runs systemd-analyze verify on content as the unit name. For a
// drop-in, base is the unit's own text, and content the drop-in. It
// returns systemd-analyze's messages; an empty string means it is clean.
func Verify(s Scope, name string, base, content []byte, dropIn bool) (string, error) {
	if _, err := exec.LookPath("systemd-analyze"); err != nil {
		return "", fmt.Errorf("systemd-analyze is not installed")
	}
	dir, err := os.MkdirTemp("", "sysdtui-verify-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	unit := content
	if dropIn {
		unit = base
		if err := os.Mkdir(filepath.Join(dir, name+".d"), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, name+".d", "override.conf"), content, 0o644); err != nil {
			return "", err
		}
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, unit, 0o644); err != nil {
		return "", err
	}
	args := []string{"verify", "--no-pager"}
	if s == User {
		args = append(args, "--user")
	}
	args = append(args, p)
	cmd := exec.Command("systemd-analyze", args...)
	cmd.Env = append(os.Environ(), "SYSTEMD_LOG_COLOR=0")
	out, _ := cmd.CombinedOutput()
	return strings.TrimSpace(strings.ReplaceAll(string(out), dir+"/", "")), nil
}
