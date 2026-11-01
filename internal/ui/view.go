package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/lonedevel/sysdtui/internal/catalog"
	"github.com/lonedevel/sysdtui/internal/lint"
	"github.com/lonedevel/sysdtui/internal/systemd"
	"github.com/lonedevel/sysdtui/internal/unitfile"
)

// sideDocs is whether the documentation sits beside the list (wide
// terminals) rather than below it.
func (m *Model) sideDocs() bool { return m.width >= 110 }

func (m *Model) docsWidth() int {
	if m.sideDocs() {
		return max(44, m.width*9/20)
	}
	return m.width
}

func (m *Model) bottomDocsHeight() int { return max(6, m.height*2/5) }

// screen joins header, body and footer into exactly the window height.
func (m *Model) screen(header string, body []string, footer ...string) string {
	h := m.height - len(footer)
	lines := append([]string{header}, body...)
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = fit(lines[i], m.width)
	}
	return strings.Join(append(lines, footer...), "\n")
}

func (m *Model) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return sErr.Render(m.status)
	}
	return sOK.Render(m.status)
}

func scrollTo(cur, off, h int) int {
	if cur < off {
		return cur
	}
	if cur >= off+h {
		return cur - h + 1
	}
	return off
}

// helpFit renders as many key hints as fit the width, in order.
func (m *Model) helpFit(pairs ...string) string {
	out := ""
	for n := 2; n <= len(pairs); n += 2 {
		h := help(pairs[:n]...)
		if ansi.StringWidth(h) > m.width {
			break
		}
		out = h
	}
	return out
}

// verifyBlock is systemd-analyze's output under the editor, at most 8 lines.
func (m *Model) verifyBlock() []string {
	e := m.ed
	if e == nil || !e.showVerify || e.verify == "" {
		return nil
	}
	out := []string{sFaint.Render(strings.Repeat("─", m.width)), sBoxTitle.Render("systemd-analyze verify") + sDim.Render("  (V hides)")}
	for _, l := range strings.Split(e.verify, "\n") {
		for _, w := range wrapWords(l, m.width-2, 2, "") {
			out = append(out, sWarn.Render(w))
		}
	}
	if len(out) > 8 {
		out = append(out[:7], sFaint.Render("…"))
	}
	return out
}

// ---- browser ----

func (m *Model) viewBrowse() string {
	t := typeFilters[m.typeIdx]
	if t == "" {
		t = "all types"
	}
	header := sTitle.Render("sysdtui") + sDim.Render(fmt.Sprintf("  %s units · %s · %d shown", m.scope, t, len(m.shown)))
	if m.loading {
		header += sDim.Render("  loading…")
	}
	listW := m.width
	if m.width >= 100 {
		listW = m.width * 11 / 20
	}
	h := m.listHeight()
	m.off = scrollTo(m.cur, m.off, h)
	var list []string
	if m.filterOn || m.filter.Value() != "" {
		list = append(list, m.filter.View())
		h--
	} else {
		list = append(list, "")
	}
	nameW := max(20, listW-24)
	for i := m.off; i < len(m.shown) && i < m.off+h; i++ {
		u := m.shown[i]
		name := sText.Render(u.Name)
		if u.Masked {
			name = sErr.Render(u.Name)
		}
		extra := ""
		if n := len(u.DropIns); n > 0 {
			extra = sChanged.Render(fmt.Sprintf(" +%d", n))
		}
		line := fit(name+extra, nameW) + " " + fit(stateStyle(u.State).Render(u.State), 10) + " " + sFaint.Render(string(u.Origin))
		line = fit(line, listW)
		if i == m.cur {
			line = sSel.Render(ansi.Strip(line))
		}
		list = append(list, line)
	}
	if !m.loading && len(m.shown) == 0 {
		list = append(list, sDim.Render("  no units match"))
	}
	body := list
	if m.width >= 100 {
		detail := m.unitDetail(m.selected(), m.width-listW-3)
		body = joinColumns(list, detail, listW, m.width-listW-3, h+1)
	}
	foot := m.helpFit("enter", "edit", "n", "new", "/", "filter", "o", "override", "F", "full copy", "s", "status", "l", "logs",
		"t", "type", "u", "user/system", "q", "quit")
	return m.screen(header, body, m.statusLine(), foot)
}

// joinColumns lays two columns of lines side by side with a separator.
func joinColumns(left, right []string, lw, rw, h int) []string {
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, fit(l, lw)+sFaint.Render(" │ ")+fit(r, rw))
	}
	return out
}

func (m *Model) description(path string) string {
	if d, ok := m.descs[path]; ok {
		return d
	}
	t, _ := readFile(path)
	d, _ := unitfile.Parse(t).Get("Unit", "Description")
	m.descs[path] = d
	return d
}

func (m *Model) unitDetail(u *systemd.Unit, w int) []string {
	if u == nil {
		return nil
	}
	var out []string
	out = append(out, sTitle.Render(u.Name))
	if d := m.description(u.Path); d != "" {
		out = append(out, wrapWords(d, w, 0, "")...)
	}
	out = append(out, "")
	kv := func(k, v string) {
		out = append(out, sDim.Render(fit(k, 9))+v)
	}
	if u.State != "" {
		kv("state", stateStyle(u.State).Render(u.State))
	}
	for i, l := range wrapWords(u.Path, w-9, 0, "") {
		if i == 0 {
			kv("file", l)
		} else {
			out = append(out, strings.Repeat(" ", 9)+l)
		}
	}
	kv("origin", originBlurb(u.Origin))
	if u.AliasOf != "" {
		kv("alias of", sUnit.Render(u.AliasOf))
	}
	if len(u.DropIns) > 0 {
		out = append(out, "", sDim.Render("drop-ins"))
		for _, d := range u.DropIns {
			out = append(out, "  "+sChanged.Render(d))
		}
	}
	if len(u.Shadowed) > 0 {
		out = append(out, "", sDim.Render("overrides"))
		for _, d := range u.Shadowed {
			out = append(out, "  "+sFaint.Render(d))
		}
	}
	out = append(out, "")
	switch {
	case u.Masked:
		out = append(out, sErr.Render("masked: systemctl unmask "+u.Name))
	case u.Origin == systemd.Vendor || u.Origin == systemd.Runtime:
		out = append(out, sDim.Render("enter adds an override drop-in; F copies the whole unit to "+systemd.WriteDir(m.scope)))
	}
	return out
}

func originBlurb(o systemd.Origin) string {
	switch o {
	case systemd.Admin:
		return "admin (yours to edit)"
	case systemd.Home:
		return "your home directory"
	case systemd.Runtime:
		return "runtime (lost at reboot)"
	}
	return "vendor (owned by a package)"
}

// ---- editor ----

func (m *Model) editHeader() string {
	e := m.ed
	kind := "unit"
	if e.dropIn {
		kind = "drop-in"
	}
	h := sTitle.Render("sysdtui") + sDim.Render(" › ") + sText.Bold(true).Render(e.name) + sDim.Render(" "+kind+" ") + sFaint.Render(e.path)
	switch {
	case !e.exists:
		h += sChanged.Render("  new")
	case e.dirty():
		h += sChanged.Render("  ● modified")
	}
	errs, warns := 0, 0
	for _, is := range e.issues {
		switch is.Severity {
		case lint.Error:
			errs++
		case lint.Warning:
			warns++
		}
	}
	if errs > 0 {
		h += sErr.Render(fmt.Sprintf("  ✖ %d", errs))
	}
	if warns > 0 {
		h += sWarn.Render(fmt.Sprintf("  ▲ %d", warns))
	}
	return h
}

func (m *Model) viewEdit() string {
	e := m.ed
	if e.preview {
		return m.viewPreview()
	}
	listW := m.width
	if m.sideDocs() {
		listW = m.width - m.docsWidth() - 3
	}
	h := m.editHeight()
	list := m.editRows(listW, h)
	verify := m.verifyBlock()
	var body []string
	if m.sideDocs() {
		docs := m.docPane(m.docsWidth(), h)
		body = joinColumns(list, docs, listW, m.docsWidth(), h)
	} else {
		for len(list) < h {
			list = append(list, "")
		}
		body = append(list, sFaint.Render(strings.Repeat("─", m.width)))
		body = append(body, m.docPane(m.width, m.height-3-len(body)-len(verify))...)
	}
	body = append(body, verify...)
	status := m.statusLine()
	if e.confirm != "" {
		q := map[string]string{"leave": "Discard your changes to " + e.name + "?", "quit": "Quit without saving?"}[e.confirm]
		status = sWarn.Render(q) + " " + help("y", "yes", "n", "no")
	}
	var foot string
	switch {
	case e.editing != nil:
		foot = m.helpFit("enter", "done", "esc", "cancel", "tab/↑↓", "choose value")
	default:
		foot = m.helpFit("enter", "edit", "a", "add", "d", "delete", "ctrl+s", "save", "esc", "back",
			"n", "next problem", "u", "undo", "space", "cycle", "v", "verify", "p", "diff", "S", "section", "J/K", "move")
	}
	return m.screen(m.editHeader(), body, status, foot)
}

func (m *Model) editRows(w, h int) []string {
	e := m.ed
	// Rows can take two lines when the value choices are shown under the
	// field being edited; keep it simple by reserving one line for them.
	avail := h
	if e.editing != nil && len(e.choices) > 0 {
		avail--
	}
	e.off = scrollTo(e.cur, e.off, avail)
	var out []string
	for i := e.off; i < len(e.rows) && len(out) < h; i++ {
		r := e.rows[i]
		sel := i == e.cur
		out = append(out, m.renderRow(r, sel, w))
		if sel && e.editing != nil && r.line == e.editing && len(e.choices) > 0 {
			out = append(out, m.choiceLine(w))
		}
	}
	return out
}

func gutter(issues []lint.Issue, changed bool) string {
	sev := -1
	for _, is := range issues {
		sev = max(sev, int(is.Severity))
	}
	switch {
	case sev == int(lint.Error):
		return sErr.Render("✖ ")
	case sev == int(lint.Warning):
		return sWarn.Render("▲ ")
	case changed:
		return sChanged.Render("● ")
	case sev == int(lint.Info):
		return sDim.Render("· ")
	}
	return "  "
}

func (m *Model) renderRow(r row, sel bool, w int) string {
	e := m.ed
	var s string
	switch r.kind {
	case rowSection:
		name := "[" + r.section + "]"
		if r.line == nil {
			s = "  " + sFaint.Render(name+" from the unit")
		} else {
			s = gutter(e.issuesFor(r.line), r.line.Modified()) + sSection.Render(name)
		}
	case rowEntry:
		l := r.line
		g := gutter(e.issuesFor(l), l.Modified() && e.exists)
		if l.Kind == unitfile.Invalid {
			s = g + sErr.Render(l.Raw())
			break
		}
		d := m.cat.Lookup(r.section, l.Name)
		key := sKey.Render("  " + l.Name)
		if d == nil {
			key = sWarn.Render("  " + l.Name)
		}
		if e.editing == l {
			e.input.Width = max(10, w-ansi.StringWidth(l.Name)-6)
			s = g + key + sDim.Render("=") + e.input.View()
			return fit(s, w)
		}
		val := l.Value
		vs := sValue
		if d != nil {
			vs = kindStyle(d.Kind)
		}
		if val == "" {
			val, vs = "(empty: resets)", sFaint
			if !e.dropIn && d != nil && !d.Resets {
				val = "(empty)"
			}
		}
		s = g + key + sDim.Render("=") + vs.Render(val)
	case rowInherited:
		s = "  " + sFaint.Render("  "+r.key+"="+strings.Join(r.values, " · "))
	case rowAdd:
		if !sel {
			return ""
		}
		s = "    " + sDim.Render("+ add a directive to ["+r.section+"]")
	}
	s = fit(s, w)
	if sel {
		s = sSel.Render(ansi.Strip(s))
	}
	return s
}

// choiceLine shows the values Tab cycles through, current one marked.
func (m *Model) choiceLine(w int) string {
	e := m.ed
	vals := e.choices
	if len(e.compMatches) > 0 {
		vals = e.compMatches
	}
	cur := strings.TrimPrefix(e.input.Value(), e.compPrefix)
	var parts []string
	for _, v := range vals {
		if v == cur {
			parts = append(parts, sKeyHint.Render(v))
		} else {
			parts = append(parts, sDim.Render(v))
		}
		if len(parts) > 40 {
			parts = append(parts, sFaint.Render(fmt.Sprintf("…%d more", len(vals)-41)))
			break
		}
	}
	return fit("      "+strings.Join(parts, sFaint.Render(" · ")), w)
}

// docPane documents the focused row.
func (m *Model) docPane(w, h int) []string {
	e := m.ed
	var out []string
	r := row{}
	if e.cur < len(e.rows) {
		r = e.rows[e.cur]
	}
	var d *catalog.Directive
	var line *unitfile.Line
	switch r.kind {
	case rowEntry:
		line = r.line
		if line.Kind == unitfile.Entry {
			d = m.cat.Lookup(r.section, line.Name)
			if d == nil {
				out = append(out, sWarn.Render(line.Name+"="), sDim.Render("not a directive systemd documents for ["+r.section+"]"))
			}
		}
	case rowInherited:
		d = m.cat.Lookup(r.section, r.key)
	case rowSection, rowAdd:
		return m.sectionPane(r, w, h)
	}
	if d != nil {
		out = append(out, directiveHeader(d, r.section, w)...)
	}
	for _, is := range e.issuesFor(line) {
		out = append(out, issueLine(is, w)...)
	}
	if e.base != nil {
		key := r.key
		if line != nil {
			key = line.Name
		}
		if v := e.base.Values(r.section, key); len(v) > 0 {
			out = append(out, sDim.Render("the unit sets:"))
			for _, x := range v {
				for _, l := range wrapWords(x, w-2, 2, "  ") {
					out = append(out, sValue.Render(l))
				}
			}
			if r.kind == rowInherited {
				out = append(out, sDim.Render("enter overrides it in this drop-in"))
			}
		}
	}
	if d != nil {
		out = append(out, "")
		doc := wrapDoc(d.Doc, w)
		maxOff := max(0, len(doc)-(h-len(out)))
		e.docOff = min(e.docOff, maxOff)
		doc = doc[e.docOff:]
		out = append(out, doc...)
		if maxOff > 0 && len(out) > h {
			out = out[:h-1]
			out = append(out, sFaint.Render(fmt.Sprintf("[ ] scroll · %d%%", 100*e.docOff/maxOff)))
		}
	}
	return out
}

func directiveHeader(d *catalog.Directive, section string, w int) []string {
	out := []string{sKey.Bold(true).Render(d.Name+"=") + sDim.Render("  ["+section+"] · "+d.Page+"(5)")}
	var kind string
	switch d.Kind {
	case catalog.KindBool:
		kind = "boolean"
		if len(d.Values) > 2 {
			kind += ", or " + strings.Join(d.Values[2:], ", ")
		}
	case catalog.KindEnum:
		kind = "one of: " + strings.Join(d.Values, " · ")
	case catalog.KindTime:
		kind = "time span, e.g. 30s, 5min, 1h 30min"
	case catalog.KindCommand:
		kind = "command line; prefixes: - (ignore failure) @ + ! !!"
	case catalog.KindUnits:
		kind = "unit names, space-separated (tab completes)"
	case catalog.KindPath:
		kind = "path"
	case catalog.KindSize:
		kind = "size in bytes, K/M/G/T suffixes, or a percentage"
	}
	var notes []string
	if d.Multi {
		notes = append(notes, "may be repeated")
	}
	if d.Resets {
		notes = append(notes, "empty value resets")
	}
	if kind != "" {
		out = append(out, wrapWords(sValue.Render(kind), w, 2, "")...)
	}
	if len(notes) > 0 {
		out = append(out, sDim.Render(strings.Join(notes, " · ")))
	}
	return out
}

func issueLine(is lint.Issue, w int) []string {
	st, mark := sDim, "· "
	switch is.Severity {
	case lint.Error:
		st, mark = sErr, "✖ "
	case lint.Warning:
		st, mark = sWarn, "▲ "
	}
	lines := wrapWords(is.Msg, w-2, 0, "")
	for i := range lines {
		if i == 0 {
			lines[i] = st.Render(mark + lines[i])
		} else {
			lines[i] = st.Render("  " + lines[i])
		}
	}
	return lines
}

func (m *Model) sectionPane(r row, w, h int) []string {
	e := m.ed
	out := []string{sSection.Render("[" + r.section + "]")}
	if n := len(m.cat.InSection(r.section)); n > 0 {
		out = append(out, sDim.Render(fmt.Sprintf("%d directives documented · enter or a to add one", n)))
	}
	if b := sectionBlurb(r.section, 1); b != "" {
		out = append(out, wrapWords(b, w, 0, "")...)
	}
	if fi := e.fileIssues(); len(fi) > 0 {
		out = append(out, "", sDim.Render("whole file"))
		for _, is := range fi {
			out = append(out, issueLine(is, w)...)
		}
	}
	if r.line != nil {
		for _, is := range e.issuesFor(r.line) {
			out = append(out, issueLine(is, w)...)
		}
	}
	out = append(out, "", sFaint.Render("docs: "+m.cat.Source))
	if len(out) > h {
		out = out[:h]
	}
	return out
}

func (m *Model) viewPreview() string {
	e := m.ed
	ops := lineDiff(e.orig, e.text())
	add, del := diffStat(ops)
	header := m.editHeader() + sDim.Render(fmt.Sprintf("  diff: +%d −%d", add, del))
	h := m.height - 3
	e.prevOff = min(e.prevOff, max(0, len(ops)-h))
	var body []string
	for _, o := range ops[e.prevOff:] {
		body = append(body, diffLine(o))
	}
	return m.screen(header, body, "", help("j/k", "scroll", "ctrl+s", "save", "esc", "back"))
}

func diffLine(o diffOp) string {
	switch o.op {
	case '+':
		return sOK.Render("+ " + o.text)
	case '-':
		return sErr.Render("- " + o.text)
	}
	return sDim.Render("  " + o.text)
}

// ---- picker, new unit, save ----

func (m *Model) viewPick() string {
	p := m.pick
	title := "Add a directive to [" + p.section + "]"
	if p.sections {
		title = "Add a section"
	}
	header := m.editHeader()
	listW := m.width
	if m.sideDocs() {
		listW = m.width - m.docsWidth() - 3
	}
	h := m.height - 5
	if !m.sideDocs() {
		h -= m.bottomDocsHeight()
	}
	h = max(3, h)
	p.off = scrollTo(p.cur, p.off, h)
	list := []string{sBoxTitle.Render(title), p.input.View()}
	nameW := 26
	for i := p.off; i < len(p.shown) && i < p.off+h; i++ {
		it := p.items[p.shown[i]]
		mark := "  "
		if it.set {
			mark = sOK.Render("✓ ")
		}
		line := mark + fit(sKey.Render(it.name), nameW) + " " + sDim.Render(it.detail)
		line = fit(line, listW)
		if i == p.cur {
			line = sSel.Render(ansi.Strip(line))
		}
		list = append(list, line)
	}
	if len(p.shown) == 0 {
		if keyName.MatchString(strings.TrimSpace(p.input.Value())) {
			list = append(list, sDim.Render("  enter adds "+strings.TrimSpace(p.input.Value())+" (not documented by systemd)"))
		} else {
			list = append(list, sDim.Render("  no match"))
		}
	}
	var docs []string
	if it := p.current(); it != nil && it.d != nil {
		docs = directiveHeader(it.d, p.section, m.docsWidth())
		docs = append(docs, "")
		docs = append(docs, wrapDoc(it.d.Doc, m.docsWidth())...)
	}
	var body []string
	if m.sideDocs() {
		body = joinColumns(list, docs, listW, m.docsWidth(), h+2)
	} else {
		body = append(list, sFaint.Render(strings.Repeat("─", m.width)))
		body = append(body, docs...)
	}
	return m.screen(header, body, m.statusLine(), help("↑↓", "choose", "enter", "add", "esc", "cancel"))
}

func (m *Model) viewNew() string {
	n := m.newu
	var types []string
	for i, t := range newTypes {
		if i == n.typeIdx {
			types = append(types, sSel.Render(" "+t+" "))
		} else {
			types = append(types, sDim.Render(" "+t+" "))
		}
	}
	body := []string{
		"",
		sBoxTitle.Render("New " + m.scope.String() + " unit"),
		"",
		"  " + n.name.View(),
		"",
		"  " + strings.Join(types, ""),
		"",
	}
	if name := n.unitName(); name != "" {
		body = append(body, "  "+sDim.Render("creates ")+sText.Render(systemdWritePath(m.scope, name)))
	}
	if n.err != "" {
		body = append(body, "", "  "+sErr.Render(n.err))
	}
	return m.screen(sTitle.Render("sysdtui"), body, m.statusLine(), help("tab", "type", "enter", "create", "esc", "cancel"))
}

func (m *Model) viewSave() string {
	s := m.save
	e := m.ed
	add, del := diffStat(s.diff)
	body := []string{"", sBoxTitle.Render("Save "+e.path) + sDim.Render(fmt.Sprintf("   +%d −%d lines", add, del))}
	if systemd.NeedsRoot(e.scope) {
		body = append(body, sDim.Render("needs root: sysdtui will run the commands below with sudo"))
	}
	body = append(body, "")
	for i, a := range s.actions {
		mark := "( ) "
		st := sText
		if i == s.cur {
			mark, st = "(•) ", sKeyHint
		}
		body = append(body, "  "+st.Render(mark+a.label))
	}
	body = append(body, "")
	if cmds := s.actions[s.cur].cmds; len(cmds) > 0 {
		body = append(body, sDim.Render("  then runs:"))
		for _, c := range cmds {
			body = append(body, "    "+sText.Render("$ "+c.String()))
		}
		body = append(body, "")
	}
	if errs := countSeverity(e.issues, lint.Error); errs > 0 {
		body = append(body, "  "+sErr.Render(fmt.Sprintf("✖ the file has %d error(s); systemd may refuse to load it", errs)), "")
	}
	body = append(body, sFaint.Render(strings.Repeat("─", m.width)))
	var changed []diffOp
	for i, o := range s.diff {
		// Show changes with a line of context.
		if o.op != ' ' || (i > 0 && s.diff[i-1].op != ' ') || (i+1 < len(s.diff) && s.diff[i+1].op != ' ') {
			changed = append(changed, o)
		}
	}
	s.off = min(s.off, max(0, len(changed)-1))
	for _, o := range changed[s.off:] {
		body = append(body, diffLine(o))
	}
	return m.screen(m.editHeader(), body, "", help("↑↓", "choose", "enter", "save", "J/K", "scroll diff", "esc", "back"))
}

func countSeverity(is []lint.Issue, s lint.Severity) int {
	n := 0
	for _, i := range is {
		if i.Severity == s {
			n++
		}
	}
	return n
}
