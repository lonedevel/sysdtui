// Package lint checks a unit file against the directive catalog as it is
// edited: unknown keys, values outside a directive's choices, and the
// mistakes systemd only reports when the unit is loaded.
package lint

import (
	"fmt"
	"strings"

	"github.com/lonedevel/sysdtui/internal/catalog"
	"github.com/lonedevel/sysdtui/internal/unitfile"
)

// Severity orders issues.
type Severity int

const (
	Info Severity = iota
	Warning
	Error
)

func (s Severity) String() string {
	return [...]string{"info", "warning", "error"}[s]
}

// Issue is one finding. Line is nil for problems with the file as a whole.
type Issue struct {
	Line     *unitfile.Line
	Severity Severity
	Msg      string
}

// Input is what Check looks at.
type Input struct {
	File *unitfile.File
	Type string // service, timer…
	// Base is the unit a drop-in overrides; nil when File is a whole unit.
	Base   *unitfile.File
	DropIn bool
}

var booleans = map[string]bool{
	"1": true, "yes": true, "y": true, "true": true, "t": true, "on": true,
	"0": true, "no": true, "n": true, "false": true, "f": true, "off": true,
}

// Check returns the issues found, most severe first per line.
func Check(c *catalog.Catalog, in Input) []Issue {
	var out []Issue
	add := func(l *unitfile.Line, s Severity, f string, a ...any) {
		out = append(out, Issue{l, s, fmt.Sprintf(f, a...)})
	}
	allowed := map[string]bool{}
	for _, s := range catalog.Sections[in.Type] {
		allowed[s] = true
	}
	section := ""
	for _, l := range in.File.Lines {
		switch l.Kind {
		case unitfile.Section:
			section = l.Name
			if len(allowed) > 0 && !allowed[section] && !strings.HasPrefix(section, "X-") {
				add(l, Warning, "[%s] is not a section of .%s units", section, in.Type)
			}
		case unitfile.Invalid:
			add(l, Error, "not a key=value line; systemd ignores it")
		case unitfile.Entry:
			if section == "" {
				add(l, Error, "%s= is outside any section", l.Name)
				continue
			}
			if strings.HasPrefix(l.Name, "X-") || strings.HasPrefix(section, "X-") {
				continue
			}
			if dep, ok := deprecated[l.Name]; ok && c.Lookup(section, l.Name) == nil {
				add(l, Info, "%s", dep)
				continue
			}
			d := c.Lookup(section, l.Name)
			if d == nil {
				if len(c.InSection(section)) > 0 {
					add(l, Warning, "unknown directive %s= in [%s]", l.Name, section)
				}
				continue
			}
			checkValue(c, d, l, add)
		}
	}
	if in.DropIn {
		out = append(out, dropInChecks(c, in)...)
	} else {
		out = append(out, unitChecks(in)...)
	}
	return out
}

func checkValue(c *catalog.Catalog, d *catalog.Directive, l *unitfile.Line, add func(*unitfile.Line, Severity, string, ...any)) {
	// Docs for another systemd version may lack values this one accepts.
	sev, note := Error, ""
	if !c.Exact {
		sev = Warning
		if c.Version != "" {
			note = " (per systemd " + c.Version + "'s docs)"
		}
	}
	v := l.Value
	if v == "" || strings.Contains(v, "%") || strings.Contains(v, "$") {
		return // empty resets; specifiers and variables expand at load time
	}
	// Conditions and asserts take "|" (trigger) and "!" (negate) prefixes,
	// and the ones with named values also accept a boolean.
	if strings.HasPrefix(d.Name, "Condition") || strings.HasPrefix(d.Name, "Assert") {
		v = strings.TrimLeft(v, "|!")
		if booleans[strings.ToLower(v)] {
			return
		}
	}
	// Some values carry an argument after a colon: ProtectHostname=private:host.
	if i := strings.IndexByte(v, ':'); i > 0 && (d.Kind == catalog.KindBool || d.Kind == catalog.KindEnum) {
		if x := v[:i]; booleans[strings.ToLower(x)] || indexOf(d.Values, x) >= 0 {
			return
		}
	}
	switch d.Kind {
	case catalog.KindBool:
		if booleans[strings.ToLower(v)] {
			return
		}
		for _, x := range d.Values {
			if v == x {
				return
			}
		}
		if len(d.Values) > 2 {
			add(l, sev, "%s= takes a boolean or one of: %s%s", d.Name, strings.Join(d.Values[2:], ", "), note)
		} else {
			add(l, sev, "%s= takes a boolean (yes/no)%s", d.Name, note)
		}
	case catalog.KindEnum:
		word := v
		if d.Multi {
			return
		}
		if booleans[strings.ToLower(v)] && indexOf(d.Values, "yes") >= 0 {
			return // enums like ProtectSystem= also take any boolean spelling
		}
		for _, x := range d.Values {
			if word == x || (strings.HasSuffix(x, ":") && strings.HasPrefix(word, x)) {
				return
			}
		}
		add(l, sev, "%s= must be one of: %s%s", d.Name, strings.Join(d.Values, ", "), note)
	case catalog.KindPath:
		for _, p := range strings.Fields(v) {
			p = strings.TrimLeft(p, "-+!:~|@")
			if p != "" && !strings.HasPrefix(p, "/") && d.Name != "WorkingDirectory" &&
				!strings.HasSuffix(d.Name, "Directory") {
				add(l, Warning, "%s= expects an absolute path", d.Name)
				return
			}
		}
	}
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

// deprecated names that systemd still accepts but no longer documents.
var deprecated = map[string]string{
	"ReadWriteDirectories":    "ReadWriteDirectories= is deprecated; use ReadWritePaths=",
	"ReadOnlyDirectories":     "ReadOnlyDirectories= is deprecated; use ReadOnlyPaths=",
	"InaccessibleDirectories": "InaccessibleDirectories= is deprecated; use InaccessiblePaths=",
	"PermissionsStartOnly":    "PermissionsStartOnly= is deprecated; prefix commands with + instead",
	"MemoryLimit":             "MemoryLimit= is deprecated; use MemoryMax=",
	"CPUAccounting":           "CPUAccounting= is no longer documented: recent systemd always accounts CPU use",
	"CPUShares":               "CPUShares= is deprecated; use CPUWeight=",
	"StartupCPUShares":        "StartupCPUShares= is deprecated; use StartupCPUWeight=",
	"BlockIOWeight":           "BlockIOWeight= is deprecated; use IOWeight=",
	"BlockIOAccounting":       "BlockIOAccounting= is deprecated; use IOAccounting=",
	"StartLimitInterval":      "StartLimitInterval= is deprecated; use StartLimitIntervalSec= in [Unit]",
	"StartLimitBurst":         "StartLimitBurst= belongs in [Unit] now",
	"StartLimitAction":        "StartLimitAction= belongs in [Unit] now",
	"FailureAction":           "FailureAction= belongs in [Unit] now",
	"SuccessAction":           "SuccessAction= belongs in [Unit] now",
	"RebootArgument":          "RebootArgument= belongs in [Unit] now",
}

// unitChecks covers a whole unit file.
func unitChecks(in Input) []Issue {
	var out []Issue
	f := in.File
	switch in.Type {
	case "service":
		typ, _ := f.Get("Service", "Type")
		starts := f.Values("Service", "ExecStart")
		// Without ExecStart= the type defaults to oneshot, which systemd
		// accepts if something else (ExecStop=, SuccessAction=) is set.
		switch {
		case len(starts) == 0 && typ != "" && typ != "oneshot":
			out = append(out, Issue{entry(f, "Service", "ExecStart"), Error, fmt.Sprintf("Type=%s needs ExecStart=", typ)})
		case len(starts) == 0 && len(f.Values("Service", "ExecStop")) == 0 && len(f.Values("Unit", "SuccessAction")) == 0:
			out = append(out, Issue{entry(f, "Service", "ExecStart"), Error, "a service needs ExecStart="})
		case len(starts) > 1 && typ != "oneshot":
			out = append(out, Issue{entry(f, "Service", "ExecStart"), Error, "only Type=oneshot services may have several ExecStart= lines"})
		}
	case "timer":
		any := false
		for _, k := range []string{"OnCalendar", "OnActiveSec", "OnBootSec", "OnStartupSec", "OnUnitActiveSec", "OnUnitInactiveSec", "OnClockChange", "OnTimezoneChange"} {
			if len(f.Values("Timer", k)) > 0 {
				any = true
			}
		}
		if !any {
			out = append(out, Issue{nil, Error, "a timer needs OnCalendar= or a monotonic setting like OnBootSec="})
		}
	case "socket":
		any := false
		for _, l := range f.Entries("Socket") {
			if strings.HasPrefix(l.Name, "Listen") && l.Value != "" {
				any = true
			}
		}
		if !any {
			out = append(out, Issue{nil, Error, "a socket needs a Listen…= setting, such as ListenStream="})
		}
	case "path":
		any := false
		for _, l := range f.Entries("Path") {
			if (strings.HasPrefix(l.Name, "Path") || l.Name == "DirectoryNotEmpty") && l.Value != "" {
				any = true
			}
		}
		if !any {
			out = append(out, Issue{nil, Error, "a path unit needs PathExists=, PathChanged= or another Path…= setting"})
		}
	case "mount":
		if v, _ := f.Get("Mount", "What"); v == "" {
			out = append(out, Issue{entry(f, "Mount", "What"), Error, "a mount needs What="})
		}
		if v, _ := f.Get("Mount", "Where"); v == "" {
			out = append(out, Issue{entry(f, "Mount", "Where"), Error, "a mount needs Where="})
		}
	}
	switch in.Type {
	case "service", "timer", "socket", "path", "mount":
		if len(f.Entries("Install")) == 0 {
			out = append(out, Issue{nil, Info, "no [Install] section: the unit can be started, but not enabled"})
		}
	}
	return out
}

// entry returns the last entry for key, to anchor an issue on it.
func entry(f *unitfile.File, section, key string) *unitfile.Line {
	var found *unitfile.Line
	for _, l := range f.Entries(section) {
		if l.Name == key {
			found = l
		}
	}
	return found
}

// dropInChecks catches the classic override mistake: assigning a list
// setting such as ExecStart= in a drop-in appends to the unit's list
// unless an empty assignment resets it first.
func dropInChecks(c *catalog.Catalog, in Input) []Issue {
	var out []Issue
	if in.Base == nil {
		return nil
	}
	for _, sec := range in.File.Sections() {
		reset := map[string]bool{}
		for _, l := range in.File.Entries(sec) {
			if l.Value == "" {
				reset[l.Name] = true
				continue
			}
			d := c.Lookup(sec, l.Name)
			if d == nil || !d.Multi || !d.Resets || reset[l.Name] || len(in.Base.Values(sec, l.Name)) == 0 {
				continue
			}
			reset[l.Name] = true // one finding per key
			sev := Warning
			msg := fmt.Sprintf("this adds to the unit's %s= list; put an empty %s= line first to replace it", l.Name, l.Name)
			if l.Name == "ExecStart" {
				if typ, _ := in.Base.Get("Service", "Type"); typ != "oneshot" {
					sev = Error
					msg = "a second ExecStart= makes the service fail to load; add an empty ExecStart= line first"
				}
			}
			out = append(out, Issue{l, sev, msg})
		}
	}
	return out
}
