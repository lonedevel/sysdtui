package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lonedevel/sysdtui/internal/catalog"
)

var testCat = func() *catalog.Catalog {
	c, err := catalog.Embedded()
	if err != nil {
		panic(err)
	}
	return c
}()

// setup makes a unit search path with an admin and a vendor directory.
func setup(t *testing.T) (etc, vendor string) {
	t.Helper()
	root := t.TempDir()
	etc, vendor = filepath.Join(root, "etc"), filepath.Join(root, "vendor")
	for _, d := range []string{etc, vendor} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SYSDTUI_UNIT_PATH", etc+":"+vendor)
	return
}

func start(t *testing.T, o Options) *Model {
	t.Helper()
	m := New(testCat, o)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(m.Init()()) // load units synchronously
	return m
}

// press sends keys: named keys ("enter", "esc", "ctrl+s"…) or text.
func press(m *Model, keys ...string) {
	named := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab, "up": tea.KeyUp,
		"down": tea.KeyDown, "ctrl+s": tea.KeyCtrlS, "space": tea.KeySpace, "backspace": tea.KeyBackspace,
	}
	for _, k := range keys {
		var cmd tea.Cmd
		if t, ok := named[k]; ok {
			_, cmd = m.Update(tea.KeyMsg{Type: t})
		} else {
			_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		}
		// Run commands that load units, so the model sees fresh state.
		if cmd != nil {
			if msg, ok := safeRun(cmd).(unitsMsg); ok {
				m.Update(msg)
			}
		}
	}
}

// safeRun runs a command unless it is one that takes over the terminal.
func safeRun(cmd tea.Cmd) (msg tea.Msg) {
	defer func() { _ = recover() }()
	return cmd()
}

func (m *Model) focus(t *testing.T, section, key string) {
	t.Helper()
	for i, r := range m.ed.rows {
		if r.section == section && ((r.line != nil && r.kind == rowEntry && r.line.Name == key) || r.key == key) {
			m.ed.cur = i
			return
		}
	}
	t.Fatalf("no row %s= in [%s]", key, section)
}

func TestNewUnitAndSave(t *testing.T) {
	etc, _ := setup(t)
	m := start(t, Options{})
	press(m, "n", "backup", "enter")
	if m.mode != modeEdit || m.ed.name != "backup.service" {
		t.Fatalf("mode %v, editor %+v", m.mode, m.ed)
	}
	// The cursor starts on the empty ExecStart=, the first problem.
	if r := m.ed.rows[m.ed.cur]; r.line == nil || r.line.Name != "ExecStart" {
		t.Fatalf("cursor on %+v", r)
	}
	press(m, "enter", "/usr/local/bin/backup --all", "enter")

	// Add a directive through the picker; Tab picks a value.
	press(m, "a", "ProtectSyst", "enter", "tab", "tab", "enter")
	if v, _ := m.ed.file.Get("Service", "ProtectSystem"); v != "yes" {
		t.Errorf("ProtectSystem = %q", v)
	}
	// Space cycles an enum.
	m.focus(t, "Service", "Restart")
	press(m, "space")
	if v, _ := m.ed.file.Get("Service", "Restart"); v != "on-abnormal" {
		t.Errorf("Restart after space = %q", v)
	}
	press(m, "u")
	if v, _ := m.ed.file.Get("Service", "Restart"); v != "on-failure" {
		t.Errorf("undo left Restart = %q", v)
	}
	for _, is := range m.ed.issues {
		if is.Severity > 0 {
			t.Errorf("unexpected issue: %s", is.Msg)
		}
	}
	press(m, "ctrl+s")
	if m.mode != modeSave {
		t.Fatal("ctrl+s did not open the save dialog")
	}
	press(m, "down", "enter") // "Save only"
	b, err := os.ReadFile(filepath.Join(etc, "backup.service"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ExecStart=/usr/local/bin/backup --all\nProtectSystem=yes\n", "Restart=on-failure\n", "WantedBy=multi-user.target\n"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("saved file lacks %q:\n%s", want, b)
		}
	}
	if m.ed.dirty() {
		t.Error("editor still dirty after saving")
	}
	if m.unitNamed("backup.service") == nil {
		t.Error("the unit list was not refreshed")
	}
}

func TestOverrideVendorUnit(t *testing.T) {
	etc, vendor := setup(t)
	if err := os.WriteFile(filepath.Join(vendor, "web.service"), []byte(
		"[Unit]\nDescription=Web\n\n[Service]\nExecStart=/usr/bin/web\nEnvironment=PORT=80\n\n[Install]\nWantedBy=multi-user.target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := start(t, Options{Open: "web.service"})
	if m.ed == nil || !m.ed.dropIn {
		t.Fatalf("a vendor unit should open as a drop-in: %+v", m.ed)
	}
	if want := filepath.Join(etc, "web.service.d", "override.conf"); m.ed.path != want {
		t.Errorf("path = %s", m.ed.path)
	}
	// Enter on the inherited ExecStart= adds a reset line, then the value.
	m.focus(t, "Service", "ExecStart")
	press(m, "enter", " --debug", "enter")
	if got := m.ed.text(); got != "[Service]\nExecStart=\nExecStart=/usr/bin/web --debug\n" {
		t.Errorf("drop-in = %q", got)
	}
	for _, is := range m.ed.issues {
		t.Errorf("unexpected issue: %s", is.Msg)
	}
	// Esc on a freshly added override removes it again.
	m.focus(t, "Unit", "Description")
	press(m, "enter", "esc")
	if strings.Contains(m.ed.text(), "Description") {
		t.Errorf("cancelled override left a line: %q", m.ed.text())
	}
	// Leaving with changes asks first.
	press(m, "esc")
	if m.ed == nil || m.ed.confirm != "leave" {
		t.Fatal("esc with changes did not ask")
	}
	press(m, "y")
	if m.ed != nil || m.mode != modeBrowse {
		t.Error("did not leave the editor")
	}
}

func TestOpenFile(t *testing.T) {
	setup(t)
	p := filepath.Join(t.TempDir(), "x.timer")
	if err := os.WriteFile(p, []byte("[Timer]\nOnCalendar=daily\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := start(t, Options{Open: p})
	if m.ed == nil || m.ed.typ != "timer" || m.ed.dirty() {
		t.Fatalf("editor = %+v", m.ed)
	}
	if v := m.View(); !strings.Contains(v, "OnCalendar=daily") {
		t.Errorf("view lacks the entry:\n%s", v)
	}
	// Deleting and re-adding a section.
	m.ed.cur = 0
	press(m, "d")
	if strings.TrimSpace(m.ed.text()) != "" {
		t.Errorf("section delete left %q", m.ed.text())
	}
	press(m, "S")
	if m.mode != modePick || !m.pick.sections {
		t.Fatal("S did not open the section picker")
	}
}

func TestViewsRender(t *testing.T) {
	_, vendor := setup(t)
	if err := os.WriteFile(filepath.Join(vendor, "a.service"), []byte("[Service]\nExecStart=/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, w := range []int{60, 90, 140} {
		m := New(testCat, Options{})
		m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
		m.Update(m.Init()())
		check := func(what string) {
			t.Helper()
			v := m.View()
			lines := strings.Split(v, "\n")
			if len(lines) != 24 {
				t.Errorf("%s at width %d: %d lines", what, w, len(lines))
			}
		}
		check("browser")
		press(m, "enter")
		check("editor")
		press(m, "a")
		check("picker")
		press(m, "esc", "p")
		check("preview")
		press(m, "esc", "ctrl+s")
		check("save")
	}
}
