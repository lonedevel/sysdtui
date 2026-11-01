package systemd

import (
	"fmt"
	"strings"
)

// Template returns the starting text for a new unit of the given type.
func Template(name string, s Scope) string {
	base := strings.TrimSuffix(name, "."+TypeOf(name))
	wanted := "multi-user.target"
	if s == User {
		wanted = "default.target"
	}
	switch TypeOf(name) {
	case "service":
		return fmt.Sprintf(`[Unit]
Description=%s
After=network.target

[Service]
Type=exec
ExecStart=
Restart=on-failure

[Install]
WantedBy=%s
`, base, wanted)
	case "timer":
		return fmt.Sprintf(`[Unit]
Description=Run %s.service on a schedule

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
`, base)
	case "socket":
		return fmt.Sprintf(`[Unit]
Description=%s socket

[Socket]
ListenStream=

[Install]
WantedBy=sockets.target
`, base)
	case "path":
		return fmt.Sprintf(`[Unit]
Description=Watch for %s

[Path]
PathChanged=

[Install]
WantedBy=paths.target
`, base)
	case "mount":
		return fmt.Sprintf(`[Unit]
Description=Mount %s

[Mount]
What=
Where=%s
Type=
Options=defaults

[Install]
WantedBy=%s
`, base, UnescapePath(base), wanted)
	case "automount":
		return fmt.Sprintf(`[Unit]
Description=Automount %s

[Automount]
Where=%s

[Install]
WantedBy=%s
`, base, UnescapePath(base), wanted)
	case "target":
		return fmt.Sprintf(`[Unit]
Description=%s
`, base)
	case "slice":
		return fmt.Sprintf(`[Unit]
Description=%s slice

[Slice]
`, base)
	}
	return fmt.Sprintf("[Unit]\nDescription=%s\n", base)
}

// UnescapePath turns a mount unit's name (without suffix) back into the
// path it mounts, as `systemd-escape --unescape --path` does.
func UnescapePath(s string) string {
	if s == "-" {
		return "/"
	}
	var b strings.Builder
	b.WriteByte('/')
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '-':
			b.WriteByte('/')
		case s[i] == '\\' && i+3 < len(s) && s[i+1] == 'x':
			var c byte
			if _, err := fmt.Sscanf(s[i+2:i+4], "%02x", &c); err == nil {
				b.WriteByte(c)
				i += 3
				continue
			}
			b.WriteByte(s[i])
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// EscapePath turns a path into the unit name prefix systemd expects for
// a mount at that path, as `systemd-escape --path` does.
func EscapePath(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return "-"
	}
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '/':
			b.WriteByte('-')
		case c == '.' && i == 0, !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == ':'):
			fmt.Fprintf(&b, `\x%02x`, c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
