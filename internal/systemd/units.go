// Package systemd finds unit files, works out where edits should be
// written, and builds the systemctl commands that apply them.
package systemd

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Scope is the systemd instance: the system manager or the user's.
type Scope int

const (
	System Scope = iota
	User
)

func (s Scope) String() string {
	if s == User {
		return "user"
	}
	return "system"
}

// Flag is the systemctl/systemd-analyze flag selecting the scope.
func (s Scope) Flag() string {
	if s == User {
		return "--user"
	}
	return "--system"
}

// Origin says what kind of directory a unit file lives in.
type Origin string

const (
	Admin   Origin = "admin"   // /etc: the administrator's own files
	Runtime Origin = "runtime" // /run: lost at reboot
	Vendor  Origin = "vendor"  // /usr/lib, /lib: owned by packages
	Home    Origin = "home"    // the user's ~/.config
)

// Dir is a unit search path entry.
type Dir struct {
	Path   string
	Origin Origin
}

// Paths returns the unit search path for a scope, highest priority first.
// SYSDTUI_UNIT_PATH (colon-separated) replaces it, for tests.
func Paths(s Scope) []Dir {
	if p := os.Getenv("SYSDTUI_UNIT_PATH"); p != "" {
		var out []Dir
		for i, d := range filepath.SplitList(p) {
			o := Vendor
			if i == 0 {
				o = Admin
			}
			out = append(out, Dir{d, o})
		}
		return out
	}
	if s == User {
		home, _ := os.UserHomeDir()
		cfg := os.Getenv("XDG_CONFIG_HOME")
		if cfg == "" {
			cfg = filepath.Join(home, ".config")
		}
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		return []Dir{
			{filepath.Join(cfg, "systemd", "user"), Home},
			{"/etc/systemd/user", Admin},
			{filepath.Join(data, "systemd", "user"), Home},
			{"/usr/local/lib/systemd/user", Vendor},
			{"/usr/lib/systemd/user", Vendor},
		}
	}
	return []Dir{
		{"/etc/systemd/system", Admin},
		{"/run/systemd/system", Runtime},
		{"/usr/local/lib/systemd/system", Vendor},
		{"/usr/lib/systemd/system", Vendor},
		{"/lib/systemd/system", Vendor},
	}
}

// WriteDir is where new units and overrides go for a scope.
func WriteDir(s Scope) string { return Paths(s)[0].Path }

// Unit is a unit file found on the search path.
type Unit struct {
	Name     string // foo.service
	Path     string // the file that defines it (the highest-priority one)
	Origin   Origin
	Masked   bool     // linked to /dev/null
	AliasOf  string   // set when Path is a symlink to another unit name
	DropIns  []string // *.conf files from every foo.service.d directory
	State    string   // from systemctl list-unit-files: enabled, disabled, static…
	Shadowed []string // lower-priority files with the same name
}

// Type is the unit type: service, timer…
func (u *Unit) Type() string { return TypeOf(u.Name) }

// TypeOf returns the suffix of a unit name without the dot.
func TypeOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return ""
}

var unitSuffixes = map[string]bool{
	"service": true, "socket": true, "timer": true, "path": true, "mount": true,
	"automount": true, "swap": true, "target": true, "slice": true, "scope": true, "device": true,
}

// ValidName reports whether name looks like a unit name.
func ValidName(name string) bool {
	if strings.ContainsAny(name, "/ \t") || !unitSuffixes[TypeOf(name)] {
		return false
	}
	return len(name) > len(TypeOf(name))+1
}

// Discover lists the units on the search path, sorted by name.
func Discover(s Scope) []*Unit {
	dirs := Paths(s)
	byName := map[string]*Unit{}
	seenDir := map[string]bool{}
	for _, d := range dirs {
		real, err := filepath.EvalSymlinks(d.Path)
		if err != nil || seenDir[real] {
			continue // /lib is often a link to /usr/lib
		}
		seenDir[real] = true
		ents, err := os.ReadDir(d.Path)
		if err != nil {
			continue
		}
		for _, e := range ents {
			name := e.Name()
			if !ValidName(name) {
				continue
			}
			p := filepath.Join(d.Path, name)
			if u := byName[name]; u != nil {
				u.Shadowed = append(u.Shadowed, p)
				continue
			}
			u := &Unit{Name: name, Path: p, Origin: d.Origin}
			if e.Type()&os.ModeSymlink != 0 {
				if t, err := os.Readlink(p); err == nil {
					if t == "/dev/null" {
						u.Masked = true
					} else if b := filepath.Base(t); b != name && ValidName(b) {
						u.AliasOf = b
					}
				}
			} else if e.IsDir() {
				continue
			}
			byName[name] = u
		}
	}
	units := make([]*Unit, 0, len(byName))
	for _, u := range byName {
		u.DropIns = DropIns(dirs, u.Name)
		units = append(units, u)
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Name < units[j].Name })
	return units
}

// DropIns returns the drop-in files that apply to a unit, in the order
// systemd applies them (by file name; a higher-priority directory's file
// replaces a same-named one). It includes the template's (foo@.service.d)
// and the type's (service.d) directories.
func DropIns(dirs []Dir, name string) []string {
	var names []string
	names = append(names, TypeOf(name)) // service.d applies to every service
	if at := strings.IndexByte(name, '@'); at >= 0 && !strings.HasPrefix(name[at:], "@.") {
		names = append(names, name[:at+1]+"."+TypeOf(name))
	}
	names = append(names, name)
	chosen := map[string]string{} // file name → path
	for _, n := range names {
		for i := len(dirs) - 1; i >= 0; i-- { // lower priority first, overwritten by higher
			matches, _ := filepath.Glob(filepath.Join(dirs[i].Path, n+".d", "*.conf"))
			for _, m := range matches {
				chosen[filepath.Base(m)] = m
			}
		}
	}
	keys := make([]string, 0, len(chosen))
	for k := range chosen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = chosen[k]
	}
	return out
}

// OverridePath is where `systemctl edit` puts a unit's override.
func OverridePath(s Scope, name string) string {
	return filepath.Join(WriteDir(s), name+".d", "override.conf")
}

// FillStates sets each unit's State from systemctl list-unit-files. It
// leaves them empty when systemctl is missing or fails.
func FillStates(s Scope, units []*Unit) {
	if os.Getenv("SYSDTUI_UNIT_PATH") != "" {
		return
	}
	out, err := exec.Command("systemctl", s.Flag(), "list-unit-files", "--no-legend", "--no-pager", "--full").Output()
	if err != nil {
		return
	}
	states := ParseStates(out)
	for _, u := range units {
		u.State = states[u.Name]
		if u.Masked {
			u.State = "masked"
		}
	}
}

// ParseStates reads `systemctl list-unit-files --no-legend` output.
func ParseStates(out []byte) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 {
			m[f[0]] = f[1]
		}
	}
	return m
}
