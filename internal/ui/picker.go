package ui

import (
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lonedevel/sysdtui/internal/catalog"
	"github.com/lonedevel/sysdtui/internal/systemd"
	"github.com/lonedevel/sysdtui/internal/unitfile"
)

type pickItem struct {
	name   string
	detail string
	d      *catalog.Directive
	set    bool // already in the file
}

// picker chooses a directive to add to a section, or a section to add.
type picker struct {
	sections bool   // choosing a section rather than a directive
	section  string // for directives: where they go
	items    []pickItem
	shown    []int
	cur, off int
	input    textinput.Model
}

var keyName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

func newPicker(placeholder string) *picker {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = placeholder
	in.Focus()
	return &picker{input: in}
}

func (m *Model) openDirectivePicker(section string) {
	p := newPicker("type to filter, or a custom X-… name")
	p.section = section
	set := map[string]bool{}
	for _, l := range m.ed.file.Entries(section) {
		set[l.Name] = true
	}
	for _, d := range m.cat.InSection(section) {
		p.items = append(p.items, pickItem{name: d.Name, detail: d.Summary(), d: d, set: set[d.Name]})
	}
	p.filter()
	m.pick, m.mode = p, modePick
}

func (m *Model) openSectionPicker() {
	p := newPicker("section name")
	p.sections = true
	have := map[string]bool{}
	for _, s := range m.ed.file.Sections() {
		have[s] = true
	}
	for _, s := range m.ed.sectionOrder() {
		if !have[s] {
			n := len(m.cat.InSection(s))
			p.items = append(p.items, pickItem{name: s, detail: sectionBlurb(s, n)})
		}
	}
	p.filter()
	m.pick, m.mode = p, modePick
}

func sectionBlurb(s string, n int) string {
	switch s {
	case "Unit":
		return "description, dependencies and ordering, conditions"
	case "Install":
		return "what `systemctl enable` hooks the unit into (WantedBy=…)"
	}
	if n > 0 {
		return strings.ToLower(s) + "-specific settings"
	}
	return ""
}

// filter ranks items: names starting with the query, then names
// containing it, then summaries mentioning it.
func (p *picker) filter() {
	q := strings.ToLower(strings.TrimSpace(p.input.Value()))
	type scored struct{ i, score int }
	var s []scored
	for i, it := range p.items {
		n := strings.ToLower(it.name)
		switch {
		case q == "":
			s = append(s, scored{i, 0})
		case strings.HasPrefix(n, q):
			s = append(s, scored{i, 0})
		case strings.Contains(n, q):
			s = append(s, scored{i, 1})
		case len(q) > 2 && strings.Contains(strings.ToLower(it.detail), q):
			s = append(s, scored{i, 2})
		}
	}
	sort.SliceStable(s, func(a, b int) bool { return s[a].score < s[b].score })
	p.shown = p.shown[:0]
	for _, x := range s {
		p.shown = append(p.shown, x.i)
	}
	p.cur, p.off = 0, 0
}

func (p *picker) current() *pickItem {
	if p.cur < len(p.shown) {
		return &p.items[p.shown[p.cur]]
	}
	return nil
}

func (m *Model) updatePick(k tea.KeyMsg) tea.Cmd {
	p := m.pick
	switch k.String() {
	case "esc":
		m.pick, m.mode = nil, modeEdit
		return nil
	case "up", "ctrl+p":
		if p.cur > 0 {
			p.cur--
		}
		return nil
	case "down", "ctrl+n", "tab":
		if p.cur < len(p.shown)-1 {
			p.cur++
		}
		return nil
	case "pgup":
		p.cur = max(0, p.cur-10)
		return nil
	case "pgdown":
		p.cur = min(max(0, len(p.shown)-1), p.cur+10)
		return nil
	case "enter":
		name := ""
		if it := p.current(); it != nil {
			name = it.name
		} else if v := strings.TrimSpace(p.input.Value()); keyName.MatchString(v) {
			name = v // a custom key, such as X-Foo
		}
		if name == "" {
			return nil
		}
		m.pick, m.mode = nil, modeEdit
		if p.sections {
			return m.addSection(name)
		}
		return m.addDirective(p.section, name)
	}
	old := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(k)
	if p.input.Value() != old {
		p.filter()
	}
	return cmd
}

func (m *Model) addSection(name string) tea.Cmd {
	e := m.ed
	e.snapshot()
	l := e.file.AddSection(name, e.sectionOrder())
	e.rebuild(m.cat)
	for i, r := range e.rows {
		if r.kind == rowSection && r.line == l {
			e.cur = i
		}
	}
	if len(m.cat.InSection(name)) > 0 {
		m.openDirectivePicker(name)
	}
	return nil
}

// addDirective inserts name= after the focused entry when it is in the
// same section (so related settings stay together), else at the end of
// the section, and starts editing its value.
func (m *Model) addDirective(section, name string) tea.Cmd {
	e := m.ed
	var after *unitfile.Line
	if e.cur < len(e.rows) {
		if r := e.rows[e.cur]; r.kind == rowEntry && r.section == section {
			after = r.line
		}
	}
	d := m.cat.Lookup(section, name)
	e.snapshot()
	if d != nil && !d.Multi {
		// Setting a single-valued directive twice is legal but confusing:
		// edit the existing line instead.
		for _, l := range e.file.Entries(section) {
			if l.Name == name {
				e.rebuild(m.cat)
				m.focusLine(l)
				e.startEdit(l, d, m.unitNames(), false)
				m.setStatus(false, "%s= is already set; editing it", name)
				return textinput.Blink
			}
		}
	}
	val := ""
	if e.base != nil {
		// In a drop-in, start from the unit's value so it can be adjusted.
		if v, ok := e.base.Get(section, name); ok && (d == nil || !d.Multi) {
			val = v
		}
		if d != nil && d.Multi && d.Resets && len(e.base.Values(section, name)) > 0 {
			after = e.file.Add(section, name, "", e.sectionOrder(), after)
			m.setStatus(false, "added an empty %s= first, so this replaces the unit's list", name)
		}
	}
	l := e.file.Add(section, name, val, e.sectionOrder(), after)
	e.rebuild(m.cat)
	m.focusLine(l)
	e.startEdit(l, d, m.unitNames(), true)
	return textinput.Blink
}

// newUnit is the dialog that names a new unit.
type newUnit struct {
	name    textinput.Model
	typeIdx int
	err     string
}

var newTypes = []string{"service", "timer", "socket", "path", "mount", "target", "slice"}

func (m *Model) startNewUnit(name string) {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "name, e.g. backup (or /mnt/data for a mount)"
	in.Focus()
	n := &newUnit{name: in}
	if t := systemd.TypeOf(name); indexOf(newTypes, t) >= 0 {
		n.typeIdx = indexOf(newTypes, t)
	}
	in.SetValue(name)
	n.name = in
	m.newu, m.mode = n, modeNew
}

// unitName is the full name the dialog would create.
func (n *newUnit) unitName() string {
	v := strings.TrimSpace(n.name.Value())
	t := newTypes[n.typeIdx]
	if (t == "mount" || t == "automount") && strings.HasPrefix(v, "/") {
		return systemd.EscapePath(v) + "." + t
	}
	if systemd.ValidName(v) {
		return v
	}
	if v == "" {
		return ""
	}
	return v + "." + t
}

func (m *Model) updateNew(k tea.KeyMsg) tea.Cmd {
	n := m.newu
	switch k.String() {
	case "esc":
		m.newu, m.mode = nil, modeBrowse
		if m.ed != nil {
			m.mode = modeEdit
		}
		return nil
	case "tab", "right":
		if k.String() == "right" && n.name.Position() < len(n.name.Value()) {
			break
		}
		n.typeIdx = (n.typeIdx + 1) % len(newTypes)
		n.syncSuffix()
		return nil
	case "shift+tab", "left":
		if k.String() == "left" && n.name.Position() > 0 {
			break
		}
		n.typeIdx = (n.typeIdx + len(newTypes) - 1) % len(newTypes)
		n.syncSuffix()
		return nil
	case "enter":
		name := n.unitName()
		switch {
		case name == "":
			n.err = "type a name"
			return nil
		case !systemd.ValidName(name):
			n.err = name + " is not a valid unit name"
			return nil
		case m.unitNamed(name) != nil:
			n.err = name + " already exists: open it from the list"
			return nil
		}
		path := systemdWritePath(m.scope, name)
		m.ed = newEditor(name, m.scope, path, "", false)
		m.ed.file = unitfileParse(systemd.Template(name, m.scope))
		m.ed.rebuild(m.cat)
		m.ed.cur = len(m.ed.rows) - 1
		m.nextIssue()
		m.newu, m.mode = nil, modeEdit
		m.setStatus(false, "new unit: fix the marked settings (n jumps to the next), then ctrl+s to save")
		if systemd.TypeOf(name) == "timer" {
			svc := strings.TrimSuffix(name, ".timer") + ".service"
			if m.unitNamed(svc) == nil {
				m.setStatus(false, "a timer starts %s, which does not exist yet: create it next with n", svc)
			}
		}
		return nil
	}
	var cmd tea.Cmd
	n.name, cmd = n.name.Update(k)
	n.err = ""
	if t := systemd.TypeOf(n.name.Value()); indexOf(newTypes, t) >= 0 {
		n.typeIdx = indexOf(newTypes, t)
	}
	return cmd
}

// syncSuffix keeps a typed suffix in step with the chosen type.
func (n *newUnit) syncSuffix() {
	v := n.name.Value()
	if t := systemd.TypeOf(v); indexOf(newTypes, t) >= 0 {
		n.name.SetValue(strings.TrimSuffix(v, t) + newTypes[n.typeIdx])
		n.name.CursorEnd()
	}
}
