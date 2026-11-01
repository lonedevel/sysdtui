package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lonedevel/sysdtui/internal/catalog"
	"github.com/lonedevel/sysdtui/internal/lint"
	"github.com/lonedevel/sysdtui/internal/systemd"
	"github.com/lonedevel/sysdtui/internal/unitfile"
)

func (m *Model) editHeight() int {
	h := m.height - 4
	if !m.sideDocs() {
		h -= m.bottomDocsHeight()
	}
	h -= len(m.verifyBlock())
	return max(3, h)
}

func (m *Model) updateEdit(k tea.KeyMsg) tea.Cmd {
	e := m.ed
	key := k.String()
	if e.confirm != "" {
		q := e.confirm
		e.confirm = ""
		if key == "y" || key == "Y" {
			switch q {
			case "quit":
				return tea.Quit
			case "leave":
				m.leaveEditor()
			}
		}
		return nil
	}
	if e.editing != nil {
		return m.updateField(k)
	}
	if e.preview {
		switch key {
		case "esc", "q", "p":
			e.preview = false
		case "down", "j":
			e.prevOff++
		case "up", "k":
			e.prevOff = max(0, e.prevOff-1)
		case "pgdown", "ctrl+d", " ":
			e.prevOff += m.height / 2
		case "pgup", "ctrl+u":
			e.prevOff = max(0, e.prevOff-m.height/2)
		case "ctrl+s", "w":
			e.preview = false
			m.openSave()
		}
		return nil
	}
	var r row
	if e.cur < len(e.rows) {
		r = e.rows[e.cur]
	}
	switch key {
	case "esc", "q":
		if e.dirty() && !(e.cleanFile() && !e.exists) {
			e.confirm = "leave"
			return nil
		}
		m.leaveEditor()
	case "up", "k", "ctrl+p":
		if e.cur > 0 {
			e.cur--
			e.docOff = 0
		}
	case "down", "j", "ctrl+n":
		if e.cur < len(e.rows)-1 {
			e.cur++
			e.docOff = 0
		}
	case "pgup", "ctrl+u":
		e.cur = max(0, e.cur-m.editHeight())
		e.docOff = 0
	case "pgdown", "ctrl+d":
		e.cur = min(len(e.rows)-1, e.cur+m.editHeight())
		e.docOff = 0
	case "home", "g":
		e.cur, e.docOff = 0, 0
	case "end", "G":
		e.cur, e.docOff = max(0, len(e.rows)-1), 0
	case "]", "shift+down":
		e.docOff += 5
	case "[", "shift+up":
		e.docOff = max(0, e.docOff-5)
	case "enter", "e":
		return m.activate(r)
	case " ":
		if r.kind == rowEntry && r.line.Kind == unitfile.Entry {
			if d := m.cat.Lookup(r.section, r.line.Name); d != nil && len(d.Values) > 0 &&
				(d.Kind == catalog.KindBool || d.Kind == catalog.KindEnum) {
				e.snapshot()
				r.line.Set(nextValue(d, r.line.Value))
				e.rebuild(m.cat)
				return nil
			}
		}
		return m.activate(r)
	case "a", "+":
		if r.section == "" {
			m.openSectionPicker()
		} else {
			m.openDirectivePicker(r.section)
		}
	case "A", "S":
		m.openSectionPicker()
	case "d", "x", "delete", "backspace":
		m.deleteRow(r)
	case "K", "alt+up":
		m.moveEntry(r, -1)
	case "J", "alt+down":
		m.moveEntry(r, +1)
	case "u", "ctrl+z":
		if e.doUndo() {
			e.rebuild(m.cat)
			m.setStatus(false, "undone")
		} else {
			m.setStatus(false, "nothing to undo")
		}
	case "n", "!":
		if !m.nextIssue() {
			m.setStatus(false, "no problems found")
		}
	case "p":
		e.preview, e.prevOff = true, 0
	case "v":
		e.verifying = true
		m.setStatus(false, "running systemd-analyze verify…")
		return m.verifyCmd()
	case "V":
		e.showVerify = !e.showVerify
	case "ctrl+s", "w":
		m.openSave()
	}
	return nil
}

// nextIssue moves the cursor to the next row with a warning or error,
// wrapping around. It reports whether there was one.
func (m *Model) nextIssue() bool {
	e := m.ed
	for k := 1; k <= len(e.rows); k++ {
		i := (e.cur + k) % len(e.rows)
		r := e.rows[i]
		if r.kind != rowEntry && r.kind != rowSection {
			continue
		}
		for _, is := range e.issuesFor(r.line) {
			if is.Severity >= lint.Warning {
				e.cur, e.docOff = i, 0
				return true
			}
		}
	}
	return false
}

func (m *Model) leaveEditor() {
	m.ed = nil
	m.mode = modeBrowse
}

// nextValue cycles a bool/enum to its next value.
func nextValue(d *catalog.Directive, v string) string {
	vals := d.Values
	if d.Kind == catalog.KindBool {
		switch strings.ToLower(v) {
		case "1", "y", "true", "t", "on":
			v = "yes"
		case "0", "n", "false", "f", "off":
			v = "no"
		}
	}
	i := indexOf(vals, v)
	return vals[(i+1)%len(vals)]
}

// activate is Enter on a row: edit an entry, override an inherited value,
// or add to a section.
func (m *Model) activate(r row) tea.Cmd {
	e := m.ed
	switch r.kind {
	case rowEntry:
		if r.line.Kind == unitfile.Invalid {
			m.setStatus(true, "this line is not key=value: delete it with d")
			return nil
		}
		e.snapshot()
		e.startEdit(r.line, m.cat.Lookup(r.section, r.line.Name), m.unitNames(), false)
		return textinput.Blink
	case rowInherited:
		d := m.cat.Lookup(r.section, r.key)
		e.snapshot()
		val := r.values[len(r.values)-1]
		switch {
		case d != nil && d.Multi && d.Resets:
			// Replace the list rather than add to it.
			e.file.Add(r.section, r.key, "", e.sectionOrder(), nil)
			m.setStatus(false, "added an empty %s= first, so this replaces the unit's list", r.key)
		case d != nil && d.Multi:
			val = ""
			m.setStatus(false, "%s= adds to the unit's values rather than replacing them", r.key)
		}
		l := e.file.Add(r.section, r.key, val, e.sectionOrder(), nil)
		e.rebuild(m.cat)
		m.focusLine(l)
		e.startEdit(l, d, m.unitNames(), true)
		return textinput.Blink
	case rowSection, rowAdd:
		m.openDirectivePicker(r.section)
	}
	return nil
}

func (m *Model) focusLine(l *unitfile.Line) {
	for i, r := range m.ed.rows {
		if r.line == l && r.kind == rowEntry {
			m.ed.cur = i
			return
		}
	}
}

func (m *Model) deleteRow(r row) {
	e := m.ed
	switch r.kind {
	case rowEntry:
		e.snapshot()
		e.file.Remove(r.line)
		e.rebuild(m.cat)
		m.setStatus(false, "deleted %s= (u to undo)", r.line.Name)
	case rowSection:
		if r.line == nil {
			return
		}
		e.snapshot()
		i := e.file.Index(r.line)
		j := i + 1
		for j < len(e.file.Lines) && e.file.Lines[j].Kind != unitfile.Section {
			j++
		}
		e.file.Lines = append(e.file.Lines[:i], e.file.Lines[j:]...)
		e.rebuild(m.cat)
		m.setStatus(false, "deleted [%s] (u to undo)", r.section)
	case rowInherited:
		m.setStatus(true, "inherited from the unit: override it with Enter, or edit the full unit with F from the list")
	}
}

// moveEntry swaps an entry with the next or previous entry in its section.
func (m *Model) moveEntry(r row, dir int) {
	e := m.ed
	if r.kind != rowEntry {
		return
	}
	i := e.file.Index(r.line)
	for j := i + dir; j >= 0 && j < len(e.file.Lines); j += dir {
		l := e.file.Lines[j]
		if l.Kind == unitfile.Section {
			return
		}
		if l.Kind == unitfile.Entry {
			e.snapshot()
			e.file.Lines[i], e.file.Lines[j] = e.file.Lines[j], e.file.Lines[i]
			e.rebuild(m.cat)
			m.focusLine(r.line)
			return
		}
	}
}

func (m *Model) updateField(k tea.KeyMsg) tea.Cmd {
	e := m.ed
	l := e.editing
	d := m.cat.Lookup(e.file.SectionOf(e.file.Index(l)), l.Name)
	switch k.String() {
	case "enter":
		v := strings.TrimSpace(e.input.Value())
		if v != l.Value || l.Modified() {
			l.Set(v)
		}
		e.editing = nil
		e.input.Blur()
		e.rebuild(m.cat)
		if n := len(e.undo); n > 0 && e.text() == e.undo[n-1] {
			e.undo = e.undo[:len(e.undo)-1] // nothing changed
		}
		return nil
	case "esc":
		e.editing = nil
		e.input.Blur()
		if e.editNew {
			e.doUndo()
		} else if len(e.undo) > 0 && e.undo[len(e.undo)-1] == e.text() {
			e.undo = e.undo[:len(e.undo)-1]
		}
		e.rebuild(m.cat)
		return nil
	case "tab", "down":
		e.complete(d, +1)
		return nil
	case "shift+tab", "up":
		e.complete(d, -1)
		return nil
	}
	var cmd tea.Cmd
	e.input, cmd = e.input.Update(k)
	return cmd
}

func (m *Model) verifyCmd() tea.Cmd {
	e := m.ed
	text, base, name, scope, dropIn := e.text(), e.baseTx, e.name, e.scope, e.dropIn
	if strings.Contains(name, "@.") {
		name = strings.Replace(name, "@.", "@sysdtui-verify.", 1) // verify an instance of a template
	}
	return func() tea.Msg {
		out, err := systemd.Verify(scope, name, []byte(base), []byte(text), dropIn)
		return verifyMsg{text: text, out: out, err: err}
	}
}
