// Command sysdtui is a terminal editor for systemd unit files.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lonedevel/sysdtui/internal/catalog"
	"github.com/lonedevel/sysdtui/internal/lint"
	"github.com/lonedevel/sysdtui/internal/systemd"
	"github.com/lonedevel/sysdtui/internal/ui"
	"github.com/lonedevel/sysdtui/internal/unitfile"
)

var version = "0.1.0"

const usage = `sysdtui: edit systemd units with their documentation beside you

Usage:
  sysdtui [--user] [unit | file]     browse units, or open one
  sysdtui --override <unit>          edit the unit's override drop-in
  sysdtui --new <name>               create a unit (backup.service, /mnt/data…)
  sysdtui --lint <file>...           check unit files and exit
  sysdtui --list                     print the units found and exit
  sysdtui --dump-catalog [section]   print the directive catalog as JSON

Flags:
`

func main() {
	fs := flag.NewFlagSet("sysdtui", flag.ExitOnError)
	user := fs.Bool("user", false, "work on the user's units (systemctl --user)")
	override := fs.Bool("override", false, "edit the unit's override drop-in")
	newName := fs.String("new", "", "create a new unit with this name")
	doLint := fs.Bool("lint", false, "check the unit files named and exit")
	list := fs.Bool("list", false, "print the units found and exit")
	dump := fs.Bool("dump-catalog", false, "print the directive catalog as JSON and exit")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fs.PrintDefaults()
	}
	_ = fs.Parse(reorder(os.Args[1:]))

	scope := systemd.System
	if *user {
		scope = systemd.User
	}
	switch {
	case *showVersion:
		fmt.Println("sysdtui", version)
		return
	case *dump:
		os.Exit(dumpCatalog(fs.Args()))
	case *list:
		listUnits(scope)
		return
	case *doLint:
		os.Exit(lintFiles(fs.Args()))
	}
	if fs.NArg() > 1 {
		fs.Usage()
		os.Exit(2)
	}
	opts := ui.Options{Scope: scope, Override: *override, New: *newName}
	if fs.NArg() == 1 {
		opts.Open = fs.Arg(0)
	}
	if *override && opts.Open == "" {
		fmt.Fprintln(os.Stderr, "sysdtui: --override needs a unit name")
		os.Exit(2)
	}
	p := tea.NewProgram(ui.New(catalog.Load(), opts), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sysdtui:", err)
		os.Exit(1)
	}
}

// reorder moves flags ahead of arguments, so `sysdtui foo.service --user`
// works like `sysdtui --user foo.service`.
func reorder(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			rest = append(rest, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "-") && len(a) > 1:
			flags = append(flags, a)
			if (a == "--new" || a == "-new") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		default:
			rest = append(rest, a)
		}
	}
	return append(flags, rest...)
}

func dumpCatalog(args []string) int {
	c := catalog.Load()
	var ds []*catalog.Directive
	if len(args) == 0 {
		ds = c.Directives
	} else {
		for _, s := range args {
			ds = append(ds, c.InSection(strings.Trim(s, "[]"))...)
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"source": c.Source, "directives": ds}); err != nil {
		fmt.Fprintln(os.Stderr, "sysdtui:", err)
		return 1
	}
	return 0
}

func listUnits(s systemd.Scope) {
	units := systemd.Discover(s)
	systemd.FillStates(s, units)
	for _, u := range units {
		state := u.State
		if state == "" {
			state = "-"
		}
		extra := ""
		if len(u.DropIns) > 0 {
			extra = fmt.Sprintf(" (+%d drop-ins)", len(u.DropIns))
		}
		fmt.Printf("%-50s %-10s %-8s %s%s\n", u.Name, state, u.Origin, u.Path, extra)
	}
}

// lintFiles checks files and prints issues like a compiler does. Drop-ins
// (*.conf in a foo.service.d directory) are checked as drop-ins of that
// unit type. It returns 1 if any file has an error.
func lintFiles(paths []string) int {
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "sysdtui: --lint needs at least one file")
		return 2
	}
	c := catalog.Load()
	status := 0
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "sysdtui:", err)
			status = 1
			continue
		}
		if strings.TrimSpace(string(b)) == "" {
			continue // a masked unit (a link to /dev/null)
		}
		f := unitfile.Parse(string(b))
		name := filepath.Base(p)
		in := lint.Input{File: f, Type: systemd.TypeOf(name)}
		if dir := filepath.Base(filepath.Dir(p)); strings.HasSuffix(name, ".conf") && strings.HasSuffix(dir, ".d") {
			in.Type, in.DropIn = systemd.TypeOf(strings.TrimSuffix(dir, ".d")), true
		}
		for _, is := range lint.Check(c, in) {
			line := 0
			if is.Line != nil {
				line = lineNumber(f, is.Line)
			}
			fmt.Printf("%s:%d: %s: %s\n", p, line, is.Severity, is.Msg)
			if is.Severity == lint.Error {
				status = 1
			}
		}
	}
	return status
}

// lineNumber is the 1-based physical line where l starts.
func lineNumber(f *unitfile.File, l *unitfile.Line) int {
	n := 1
	for _, x := range f.Lines {
		if x == l {
			return n
		}
		n += strings.Count(x.Raw(), "\n") + 1
	}
	return 0
}
