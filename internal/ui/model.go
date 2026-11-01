// Package ui is sysdtui's Bubble Tea interface: a unit browser, an editor
// with the directive documentation beside it, and the save/apply flow.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lonedevel/sysdtui/internal/catalog"
	"github.com/lonedevel/sysdtui/internal/systemd"
)

type mode int

const (
	modeBrowse mode = iota
	modeEdit
	modePick
	modeNew
	modeSave
)

// Options configure the program at start.
type Options struct {
	Scope systemd.Scope
	// Open names a unit (foo.service) or a file path to edit at start.
	Open string
	// Override opens Open's override drop-in rather than the unit.
	Override bool
	// New starts a new unit with this name.
	New string
}

// Model is the whole application state.
type Model struct {
	cat   *catalog.Catalog
	scope systemd.Scope
	opts  Options

	units    []*systemd.Unit
	shown    []*systemd.Unit // after the filter
	cur, off int
	filter   textinput.Model
	filterOn bool
	typeIdx  int // into typeFilters
	loading  bool
	descs    map[string]string // unit path → Description=

	mode   mode
	ed     *editor
	pick   *picker
	newu   *newUnit
	save   *saveDialog
	width  int
	height int

	status    string
	statusErr bool
}

var typeFilters = []string{"", "service", "timer", "socket", "path", "mount", "target", "slice"}

// New builds the model.
func New(cat *catalog.Catalog, o Options) *Model {
	f := textinput.New()
	f.Prompt = "/"
	f.Placeholder = "filter units"
	return &Model{cat: cat, scope: o.Scope, opts: o, filter: f, loading: true, descs: map[string]string{}, width: 100, height: 30}
}

type unitsMsg struct {
	scope systemd.Scope
	units []*systemd.Unit
}

type verifyMsg struct {
	text string
	out  string
	err  error
}

type execDoneMsg struct {
	err   error
	saved string // the text written, when the command saved the file
	what  string
}

func loadUnits(s systemd.Scope) tea.Cmd {
	return func() tea.Msg {
		u := systemd.Discover(s)
		systemd.FillStates(s, u)
		return unitsMsg{s, u}
	}
}

// Init starts loading units.
func (m *Model) Init() tea.Cmd {
	if m.opts.New != "" {
		m.startNewUnit(m.opts.New)
	}
	return loadUnits(m.scope)
}

func (m *Model) setStatus(err bool, f string, a ...any) {
	m.status, m.statusErr = fmt.Sprintf(f, a...), err
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case unitsMsg:
		if msg.scope != m.scope {
			return m, nil
		}
		m.units, m.loading = msg.units, false
		m.applyFilter()
		if m.opts.Open != "" {
			open := m.opts.Open
			m.opts.Open = ""
			return m, m.openArg(open, m.opts.Override)
		}
		return m, nil
	case verifyMsg:
		if m.ed != nil && m.ed.verifying {
			m.ed.verifying = false
			switch {
			case msg.err != nil:
				m.setStatus(true, "verify: %v", msg.err)
			case msg.out == "":
				m.ed.verify = ""
				m.setStatus(false, "systemd-analyze verify: no problems found")
			default:
				m.ed.verify, m.ed.showVerify = msg.out, true
				m.setStatus(true, "systemd-analyze verify reported problems (V to hide)")
			}
		}
		return m, nil
	case execDoneMsg:
		return m, m.execDone(msg)
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.ed != nil && m.ed.dirty() && m.mode != modeBrowse {
				if m.ed.confirm == "quit" {
					return m, tea.Quit
				}
				m.ed.confirm = "quit"
				m.mode = modeEdit
				return m, nil
			}
			return m, tea.Quit
		}
		m.status = ""
		switch m.mode {
		case modeEdit:
			return m, m.updateEdit(msg)
		case modePick:
			return m, m.updatePick(msg)
		case modeNew:
			return m, m.updateNew(msg)
		case modeSave:
			return m, m.updateSave(msg)
		}
		return m, m.updateBrowse(msg)
	}
	return m, nil
}

func (m *Model) applyFilter() {
	q := strings.ToLower(m.filter.Value())
	t := typeFilters[m.typeIdx]
	m.shown = m.shown[:0]
	for _, u := range m.units {
		if t != "" && u.Type() != t {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(u.Name), q) {
			continue
		}
		m.shown = append(m.shown, u)
	}
	if m.cur >= len(m.shown) {
		m.cur = max(0, len(m.shown)-1)
	}
}

func (m *Model) selected() *systemd.Unit {
	if m.cur < len(m.shown) {
		return m.shown[m.cur]
	}
	return nil
}

func (m *Model) unitNamed(name string) *systemd.Unit {
	for _, u := range m.units {
		if u.Name == name {
			return u
		}
	}
	return nil
}

func (m *Model) unitNames() []string {
	out := make([]string, len(m.units))
	for i, u := range m.units {
		out[i] = u.Name
	}
	return out
}

func (m *Model) listHeight() int { return max(3, m.height-6) }

func (m *Model) updateBrowse(k tea.KeyMsg) tea.Cmd {
	if m.filterOn {
		switch k.String() {
		case "esc":
			m.filterOn = false
			m.filter.Blur()
			m.filter.SetValue("")
			m.applyFilter()
			return nil
		case "enter":
			m.filterOn = false
			m.filter.Blur()
			return nil
		case "up", "down", "ctrl+n", "ctrl+p":
		default:
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(k)
			m.cur = 0
			m.applyFilter()
			return cmd
		}
	}
	u := m.selected()
	switch k.String() {
	case "q", "esc":
		if k.String() == "esc" && m.filter.Value() != "" {
			m.filter.SetValue("")
			m.applyFilter()
			return nil
		}
		return tea.Quit
	case "up", "k", "ctrl+p":
		if m.cur > 0 {
			m.cur--
		}
	case "down", "j", "ctrl+n":
		if m.cur < len(m.shown)-1 {
			m.cur++
		}
	case "pgup", "ctrl+u":
		m.cur = max(0, m.cur-m.listHeight())
	case "pgdown", "ctrl+d":
		m.cur = min(max(0, len(m.shown)-1), m.cur+m.listHeight())
	case "home", "g":
		m.cur = 0
	case "end", "G":
		m.cur = max(0, len(m.shown)-1)
	case "/":
		m.filterOn = true
		return m.filter.Focus()
	case "t":
		m.typeIdx = (m.typeIdx + 1) % len(typeFilters)
		m.cur = 0
		m.applyFilter()
	case "T":
		m.typeIdx = (m.typeIdx + len(typeFilters) - 1) % len(typeFilters)
		m.cur = 0
		m.applyFilter()
	case "u":
		m.scope = 1 - m.scope
		m.loading, m.units, m.shown, m.cur = true, nil, nil, 0
		return loadUnits(m.scope)
	case "r":
		m.loading = true
		return loadUnits(m.scope)
	case "n":
		m.startNewUnit("")
	case "enter", "e":
		if u != nil {
			return m.openUnit(u, false, false)
		}
	case "o":
		if u != nil {
			return m.openUnit(u, true, false)
		}
	case "F":
		if u != nil {
			return m.openUnit(u, false, true)
		}
	case "s":
		if u != nil {
			return m.runCommands("status", []systemd.Command{systemd.Systemctl(m.scope, "status", "--no-pager", "--full", u.Name)}, nil, "")
		}
	case "l":
		if u != nil {
			c := systemd.Journal(m.scope, u.Name)
			return tea.ExecProcess(exec.Command(c[0], c[1:]...), func(err error) tea.Msg { return execDoneMsg{err: err, what: "journal"} })
		}
	}
	return nil
}

// openArg opens what was named on the command line: a unit or a file.
func (m *Model) openArg(arg string, override bool) tea.Cmd {
	if u := m.unitNamed(arg); u != nil {
		return m.openUnit(u, override, false)
	}
	if strings.Contains(arg, "/") || fileExists(arg) {
		p, _ := filepath.Abs(arg)
		text, ok := readFile(p)
		name := filepath.Base(p)
		if strings.HasSuffix(name, ".conf") && strings.HasSuffix(filepath.Dir(p), ".d") {
			name = strings.TrimSuffix(filepath.Base(filepath.Dir(p)), ".d")
		}
		m.ed = newEditor(name, m.scope, p, text, ok)
		m.ed.dropIn = strings.HasSuffix(p, ".conf")
		m.ed.rebuild(m.cat)
		m.mode = modeEdit
		return nil
	}
	if systemd.ValidName(arg) {
		m.startNewUnit(arg)
		m.newu.name.SetValue(arg)
		m.setStatus(false, "%s does not exist yet: create it?", arg)
		return nil
	}
	m.setStatus(true, "no unit or file named %q", arg)
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// openUnit starts editing a unit. Units owned by packages are edited
// through a drop-in unless full is set, which copies the whole unit to
// the admin directory, as `systemctl edit --full` does.
func (m *Model) openUnit(u *systemd.Unit, override, full bool) tea.Cmd {
	if u.Masked {
		m.setStatus(true, "%s is masked (linked to /dev/null); unmask it first", u.Name)
		return nil
	}
	if u.AliasOf != "" {
		if t := m.unitNamed(u.AliasOf); t != nil {
			m.setStatus(false, "%s is an alias of %s", u.Name, t.Name)
			u = t
		}
	}
	real, err := filepath.EvalSymlinks(u.Path)
	if err != nil {
		real = u.Path
	}
	writable := u.Origin == systemd.Admin || u.Origin == systemd.Home
	if !writable && !full {
		override = true
	}
	switch {
	case override:
		p := systemd.OverridePath(m.scope, u.Name)
		text, ok := readFile(p)
		e := newEditor(u.Name, m.scope, p, text, ok)
		e.dropIn = true
		e.base, e.baseTx = mergedBase(real, u.DropIns, p)
		m.ed = e
		if !ok && !writable && u.Origin != systemd.Runtime {
			m.setStatus(false, "%s belongs to a package: changes go to an override drop-in (F edits a full copy)", u.Name)
		}
	case full && !writable:
		text, _ := readFile(real)
		p := filepath.Join(systemd.WriteDir(m.scope), u.Name)
		m.ed = newEditor(u.Name, m.scope, p, text, false)
		m.setStatus(false, "editing a full copy: it will be saved to %s and replace the packaged unit", p)
	default:
		text, ok := readFile(real)
		m.ed = newEditor(u.Name, m.scope, real, text, ok)
	}
	m.ed.rebuild(m.cat)
	m.mode = modeEdit
	return nil
}

// runCommands hands the terminal to a script that runs cmds, then
// returns to the editor. saved is the text being written, if any.
func (m *Model) runCommands(what string, cmds []systemd.Command, cleanup []string, saved string) tea.Cmd {
	c := exec.Command("sh", "-c", systemd.Script(cmds, cleanup))
	return tea.ExecProcess(c, func(err error) tea.Msg { return execDoneMsg{err: err, saved: saved, what: what} })
}

func (m *Model) execDone(msg execDoneMsg) tea.Cmd {
	if msg.what == "save" && m.ed != nil {
		if msg.err == nil || fileHas(m.ed.path, msg.saved) {
			m.ed.orig, m.ed.exists = msg.saved, true
			m.ed.undo = nil
		}
	}
	switch {
	case msg.err != nil && msg.what == "save":
		if m.ed != nil && fileHas(m.ed.path, msg.saved) {
			m.setStatus(true, "saved %s, but a follow-up command failed", m.ed.path)
		} else {
			m.setStatus(true, "not saved: the install command failed")
		}
	case msg.err != nil && msg.what != "journal" && msg.what != "status":
		m.setStatus(true, "%s failed", msg.what)
	case msg.what == "save":
		m.setStatus(false, "saved %s", m.ed.path)
	}
	return loadUnits(m.scope)
}

func fileHas(p, text string) bool {
	t, ok := readFile(p)
	return ok && t == text
}

// View renders the current screen.
func (m *Model) View() string {
	switch m.mode {
	case modeEdit:
		return m.viewEdit()
	case modePick:
		return m.viewPick()
	case modeNew:
		return m.viewNew()
	case modeSave:
		return m.viewSave()
	}
	return m.viewBrowse()
}
