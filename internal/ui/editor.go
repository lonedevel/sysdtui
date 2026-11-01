package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/lonedevel/sysdtui/internal/catalog"
	"github.com/lonedevel/sysdtui/internal/lint"
	"github.com/lonedevel/sysdtui/internal/systemd"
	"github.com/lonedevel/sysdtui/internal/unitfile"
)

type rowKind int

const (
	rowSection rowKind = iota
	rowEntry
	rowInherited // a value the drop-in's unit sets, shown for context
	rowAdd       // "add a directive" at the end of a section
)

type row struct {
	kind    rowKind
	section string
	line    *unitfile.Line // rowSection (nil when the section is only inherited), rowEntry
	key     string         // rowInherited
	values  []string       // rowInherited: the effective values
}

// editor holds one file being edited.
type editor struct {
	name   string // the unit: foo.service
	typ    string
	scope  systemd.Scope
	path   string // where it will be written
	dropIn bool
	base   *unitfile.File // for a drop-in: the unit plus its other drop-ins
	baseTx string
	file   *unitfile.File
	orig   string // the file's text on disk, "" for a new file
	exists bool
	undo   []string

	rows     []row
	cur, off int
	docOff   int

	input       textinput.Model
	editing     *unitfile.Line
	editNew     bool     // the line was added for this edit; Esc removes it
	choices     []string // completions for the field being edited
	choice      int      // index into compMatches, -1 before the first Tab
	compMatches []string // the values Tab cycles through
	compPrefix  string   // text before the word being completed

	issues     []lint.Issue
	verify     string // systemd-analyze output
	verifying  bool
	showVerify bool
	preview    bool
	prevOff    int
	confirm    string // a pending y/n question
}

// sectionOrder is the order sections appear in for the unit's type.
func (e *editor) sectionOrder() []string {
	if o := catalog.Sections[e.typ]; o != nil {
		return o
	}
	return []string{"Unit", "Install"}
}

func newEditor(name string, scope systemd.Scope, path, text string, exists bool) *editor {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 0
	return &editor{
		name: name, typ: systemd.TypeOf(name), scope: scope, path: path,
		file: unitfile.Parse(text), orig: text, exists: exists, input: in,
	}
}

func (e *editor) text() string    { return e.file.String() }
func (e *editor) dirty() bool     { return e.text() != e.orig || !e.exists }
func (e *editor) cleanFile() bool { return strings.TrimSpace(e.text()) == "" }

// snapshot records the text for undo before a change.
func (e *editor) snapshot() {
	t := e.text()
	if n := len(e.undo); n > 0 && e.undo[n-1] == t {
		return
	}
	e.undo = append(e.undo, t)
	if len(e.undo) > 200 {
		e.undo = e.undo[1:]
	}
}

func (e *editor) doUndo() bool {
	if len(e.undo) == 0 {
		return false
	}
	t := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.file = unitfile.Parse(t)
	return true
}

// rebuild recomputes rows and lint issues after a change, keeping the
// cursor on the same line where possible.
func (e *editor) rebuild(cat *catalog.Catalog) {
	var keep *unitfile.Line
	var keepKey, keepSec string
	if e.cur < len(e.rows) {
		r := e.rows[e.cur]
		keep, keepKey, keepSec = r.line, r.key, r.section
	}
	e.rows = e.buildRows()
	e.issues = lint.Check(cat, lint.Input{File: e.file, Type: e.typ, Base: e.base, DropIn: e.dropIn})
	for i, r := range e.rows {
		if (keep != nil && r.line == keep && r.kind != rowAdd) || (keep == nil && keepKey != "" && r.key == keepKey && r.section == keepSec) {
			e.cur = i
			return
		}
	}
	if e.cur >= len(e.rows) {
		e.cur = len(e.rows) - 1
	}
	if e.cur < 0 {
		e.cur = 0
	}
}

func (e *editor) buildRows() []row {
	var rows []row
	// Section order: the file's own, then (for drop-ins) the unit's
	// sections the drop-in does not have yet.
	secs := e.file.Sections()
	have := map[string]bool{}
	for _, s := range secs {
		have[s] = true
	}
	if e.base != nil {
		for _, s := range e.base.Sections() {
			if !have[s] {
				secs = append(secs, s)
				have[s] = true
			}
		}
		order := e.sectionOrder()
		rank := func(s string) int {
			for i, o := range order {
				if o == s {
					return i
				}
			}
			return len(order)
		}
		sort.SliceStable(secs, func(i, j int) bool { return rank(secs[i]) < rank(secs[j]) })
	}
	headers := map[string]*unitfile.Line{}
	for _, l := range e.file.Lines {
		if l.Kind == unitfile.Section && headers[l.Name] == nil {
			headers[l.Name] = l
		}
	}
	for _, s := range secs {
		rows = append(rows, row{kind: rowSection, section: s, line: headers[s]})
		set := map[string]bool{}
		for _, l := range e.file.Entries(s) {
			rows = append(rows, row{kind: rowEntry, section: s, line: l})
			set[l.Name] = true
		}
		if e.base != nil {
			var keys []string
			seen := map[string]bool{}
			for _, l := range e.base.Entries(s) {
				if !seen[l.Name] && !set[l.Name] {
					seen[l.Name] = true
					keys = append(keys, l.Name)
				}
			}
			for _, k := range keys {
				if v := e.base.Values(s, k); len(v) > 0 {
					rows = append(rows, row{kind: rowInherited, section: s, key: k, values: v})
				}
			}
		}
		rows = append(rows, row{kind: rowAdd, section: s})
	}
	// Lines that are not in any section (invalid entries before the first
	// header) are shown first so they can be fixed or deleted.
	var stray []row
	for i, l := range e.file.Lines {
		if (l.Kind == unitfile.Entry || l.Kind == unitfile.Invalid) && e.file.SectionOf(i) == "" {
			stray = append(stray, row{kind: rowEntry, line: l})
		}
	}
	return append(stray, rows...)
}

// issuesFor returns the lint issues anchored on a line.
func (e *editor) issuesFor(l *unitfile.Line) []lint.Issue {
	var out []lint.Issue
	for _, is := range e.issues {
		if l != nil && is.Line == l {
			out = append(out, is)
		}
	}
	return out
}

// fileIssues are the issues not tied to a line.
func (e *editor) fileIssues() []lint.Issue {
	var out []lint.Issue
	for _, is := range e.issues {
		if is.Line == nil || e.file.Index(is.Line) < 0 {
			out = append(out, is)
		}
	}
	return out
}

// directive returns the catalog entry for the focused row, if any.
func (e *editor) directive(cat *catalog.Catalog) *catalog.Directive {
	if e.cur >= len(e.rows) {
		return nil
	}
	r := e.rows[e.cur]
	switch r.kind {
	case rowEntry:
		return cat.Lookup(r.section, r.line.Name)
	case rowInherited:
		return cat.Lookup(r.section, r.key)
	}
	return nil
}

// startEdit opens the input on an entry.
func (e *editor) startEdit(l *unitfile.Line, d *catalog.Directive, units []string, isNew bool) {
	e.editing, e.editNew = l, isNew
	e.input.SetValue(l.Value)
	e.input.CursorEnd()
	e.input.Focus()
	e.choices, e.choice, e.compMatches = nil, -1, nil
	if d != nil {
		switch d.Kind {
		case catalog.KindUnits:
			e.choices = units
		default:
			e.choices = d.Values
		}
	}
}

// complete cycles the input through completions (dir is +1 or -1). For
// unit lists it completes the last word. Repeated presses cycle through
// the matches for what was typed before the first press.
func (e *editor) complete(d *catalog.Directive, dir int) {
	if len(e.choices) == 0 {
		return
	}
	v := e.input.Value()
	cycling := e.choice >= 0 && e.choice < len(e.compMatches) && v == e.compPrefix+e.compMatches[e.choice]
	if !cycling {
		prefix, word := "", v
		if d != nil && d.Kind == catalog.KindUnits {
			if i := strings.LastIndexByte(v, ' '); i >= 0 {
				prefix, word = v[:i+1], v[i+1:]
			}
		}
		var matches []string
		for _, c := range e.choices {
			if strings.HasPrefix(c, word) {
				matches = append(matches, c)
			}
		}
		closed := d != nil && (d.Kind == catalog.KindEnum || d.Kind == catalog.KindBool)
		if closed && len(matches) <= 1 {
			matches, prefix = e.choices, "" // a closed set of values always cycles
		}
		if len(matches) == 0 {
			return
		}
		e.compMatches, e.compPrefix = matches, prefix
		e.choice = indexOf(matches, word)
		if e.choice < 0 {
			e.choice = -dir
			if dir < 0 {
				e.choice = len(matches)
			}
		}
	}
	e.choice = (e.choice + dir + len(e.compMatches)) % len(e.compMatches)
	e.input.SetValue(e.compPrefix + e.compMatches[e.choice])
	e.input.CursorEnd()
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

// readFile returns a file's text, "" if missing.
func readFile(p string) (string, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// mergedBase reads a unit and its drop-ins, except skip, into one file
// as systemd would apply them.
func mergedBase(unitPath string, dropIns []string, skip string) (*unitfile.File, string) {
	var b strings.Builder
	if t, ok := readFile(unitPath); ok {
		b.WriteString(t)
		if !strings.HasSuffix(t, "\n") {
			b.WriteByte('\n')
		}
	}
	for _, d := range dropIns {
		if filepath.Clean(d) == filepath.Clean(skip) {
			continue
		}
		if t, ok := readFile(d); ok {
			b.WriteString("\n# " + d + "\n")
			b.WriteString(t)
			if !strings.HasSuffix(t, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	return unitfile.Parse(b.String()), b.String()
}
