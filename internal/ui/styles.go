package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lonedevel/sysdtui/internal/catalog"
)

// Palette: bright accents on dark terminals, deeper ones on light.
var (
	cAccent  = lipgloss.AdaptiveColor{Light: "#0F766E", Dark: "#2DD4BF"} // teal: sections, title
	cKey     = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#C4B5FD"} // directive names
	cValue   = lipgloss.AdaptiveColor{Light: "#A16207", Dark: "#FDE68A"}
	cUnit    = lipgloss.AdaptiveColor{Light: "#1D4ED8", Dark: "#93C5FD"}
	cOK      = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#86EFAC"}
	cWarn    = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FCD34D"}
	cErr     = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#FCA5A5"}
	cChanged = lipgloss.AdaptiveColor{Light: "#C2410C", Dark: "#FDBA74"}
	cText    = lipgloss.AdaptiveColor{Light: "#1F2937", Dark: "#E5E7EB"}
	cDim     = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	cFaint   = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#4B5563"}
	cSelBg   = lipgloss.AdaptiveColor{Light: "#CCFBF1", Dark: "#134E4A"}
)

var (
	sTitle    = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sSection  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sKey      = lipgloss.NewStyle().Foreground(cKey)
	sValue    = lipgloss.NewStyle().Foreground(cValue)
	sUnit     = lipgloss.NewStyle().Foreground(cUnit)
	sText     = lipgloss.NewStyle().Foreground(cText)
	sDim      = lipgloss.NewStyle().Foreground(cDim)
	sFaint    = lipgloss.NewStyle().Foreground(cFaint)
	sOK       = lipgloss.NewStyle().Foreground(cOK)
	sWarn     = lipgloss.NewStyle().Foreground(cWarn)
	sErr      = lipgloss.NewStyle().Foreground(cErr)
	sChanged  = lipgloss.NewStyle().Foreground(cChanged)
	sSel      = lipgloss.NewStyle().Background(cSelBg)
	sKeyHint  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sBoxTitle = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
)

// stateStyle colors a unit-file state from systemctl.
func stateStyle(state string) lipgloss.Style {
	switch state {
	case "enabled", "enabled-runtime", "linked", "linked-runtime", "alias":
		return sOK
	case "masked", "masked-runtime", "bad":
		return sErr
	case "static", "indirect", "generated", "transient":
		return sDim
	}
	return sText
}

// kindStyle colors a value by the directive's kind.
func kindStyle(k catalog.Kind) lipgloss.Style {
	switch k {
	case catalog.KindUnits:
		return sUnit
	case catalog.KindCommand:
		return sText
	}
	return sValue
}

// help renders key hints: pairs of key, description.
func help(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, sKeyHint.Render(pairs[i])+" "+sDim.Render(pairs[i+1]))
	}
	return strings.Join(parts, sFaint.Render("  "))
}

// fit truncates or pads s (which may hold ANSI codes) to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		return ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", w-ansi.StringWidth(s))
}

// wrapDoc lays out documentation paragraphs at width w.
func wrapDoc(paras []catalog.Para, w int) []string {
	var out []string
	prevPre := false
	for i, p := range paras {
		if i > 0 && !(p.Pre && prevPre) {
			out = append(out, "")
		}
		prevPre = p.Pre
		ind := p.Indent
		if ind > w/3 {
			ind = w / 3
		}
		pad := strings.Repeat(" ", ind)
		if p.Pre {
			for _, l := range hardWrap(p.Text, w-ind) {
				out = append(out, pad+sDim.Render(l))
			}
			continue
		}
		hang := 0
		if strings.HasPrefix(p.Text, "• ") {
			hang = 2
		}
		out = append(out, wrapWords(p.Text, w-ind, hang, pad)...)
	}
	return out
}

// wrapWords word-wraps text with a prefix on every line and a hanging
// indent after the first.
func wrapWords(text string, w, hang int, pad string) []string {
	if w < 10 {
		w = 10
	}
	var out []string
	line := ""
	limit := w
	for _, word := range strings.Fields(text) {
		ww := ansi.StringWidth(word)
		switch {
		case line == "":
			line = word
		case ansi.StringWidth(line)+1+ww <= limit:
			line += " " + word
		default:
			out = append(out, line)
			line = strings.Repeat(" ", hang) + word
			limit = w
		}
		for ansi.StringWidth(line) > limit { // a single word longer than the line
			out = append(out, ansi.Truncate(line, limit, ""))
			line = strings.Repeat(" ", hang) + ansi.TruncateLeft(line, limit, "")
		}
	}
	if line != "" {
		out = append(out, line)
	}
	for i := range out {
		out[i] = pad + out[i]
	}
	return out
}

func hardWrap(s string, w int) []string {
	if w < 10 {
		w = 10
	}
	var out []string
	for ansi.StringWidth(s) > w {
		out = append(out, ansi.Truncate(s, w, ""))
		s = "  " + ansi.TruncateLeft(s, w, "")
	}
	return append(out, s)
}
