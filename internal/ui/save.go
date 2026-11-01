package ui

import (
	"errors"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lonedevel/sysdtui/internal/systemd"
	"github.com/lonedevel/sysdtui/internal/unitfile"
)

func unitfileParse(s string) *unitfile.File { return unitfile.Parse(s) }

func systemdWritePath(s systemd.Scope, name string) string {
	return filepath.Join(systemd.WriteDir(s), name)
}

// saveAction is one choice of what to do after writing the file.
type saveAction struct {
	label string
	cmds  []systemd.Command
}

type saveDialog struct {
	actions []saveAction
	cur     int
	diff    []diffOp
	off     int
}

func (m *Model) openSave() {
	e := m.ed
	if e.editing != nil {
		return
	}
	s := &saveDialog{diff: lineDiff(e.orig, e.text())}
	reload := systemd.Systemctl(e.scope, "daemon-reload")
	s.actions = []saveAction{
		{label: "Save and reload systemd", cmds: []systemd.Command{reload}},
		{label: "Save only", cmds: nil},
	}
	canStart := false
	switch e.typ {
	case "service", "socket", "timer", "path", "mount", "swap", "automount", "target":
		canStart = !containsAt(e.name)
	}
	if canStart {
		s.actions = append(s.actions, saveAction{
			label: "Save, reload and restart " + e.name,
			cmds:  []systemd.Command{reload, systemd.Systemctl(e.scope, "restart", e.name)},
		})
		hasInstall := len(e.file.Entries("Install")) > 0 || (e.base != nil && len(e.base.Entries("Install")) > 0)
		if hasInstall {
			s.actions = append(s.actions, saveAction{
				label: "Save, reload, enable and start " + e.name,
				cmds:  []systemd.Command{reload, systemd.Systemctl(e.scope, "enable", "--now", e.name)},
			})
		}
	}
	m.save, m.mode = s, modeSave
}

func containsAt(name string) bool {
	for i := 0; i+1 < len(name); i++ {
		if name[i] == '@' && name[i+1] == '.' {
			return true // a template has no instance to start
		}
	}
	return false
}

func (m *Model) updateSave(k tea.KeyMsg) tea.Cmd {
	s := m.save
	switch k.String() {
	case "esc", "q":
		m.save, m.mode = nil, modeEdit
	case "up", "k":
		if s.cur > 0 {
			s.cur--
		}
	case "down", "j", "tab":
		if s.cur < len(s.actions)-1 {
			s.cur++
		}
	case "pgdown", "ctrl+d", "J":
		s.off += 5
	case "pgup", "ctrl+u", "K":
		s.off = max(0, s.off-5)
	case "enter", "ctrl+s", "y":
		return m.doSave(s.actions[s.cur])
	}
	return nil
}

// doSave writes the file directly when it can, or through sudo when the
// directory belongs to root, then runs the chosen systemctl commands.
func (m *Model) doSave(a saveAction) tea.Cmd {
	e := m.ed
	text := e.text()
	m.save, m.mode = nil, modeEdit
	err := systemd.Write(e.path, []byte(text))
	switch {
	case err == nil:
		e.orig, e.exists, e.undo = text, true, nil
		if len(a.cmds) == 0 {
			m.setStatus(false, "saved %s", e.path)
			return loadUnits(m.scope)
		}
		return m.runCommands("apply", a.cmds, nil, "")
	case errors.Is(err, systemd.ErrNeedsRoot):
		staged, serr := systemd.Stage([]byte(text))
		if serr != nil {
			m.setStatus(true, "cannot stage the file: %v", serr)
			return nil
		}
		cmds := append([]systemd.Command{systemd.InstallCommand(staged, e.path)}, a.cmds...)
		return m.runCommands("save", cmds, []string{staged}, text)
	default:
		m.setStatus(true, "save failed: %v", err)
		return nil
	}
}
