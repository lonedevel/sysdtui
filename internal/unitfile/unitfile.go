// Package unitfile reads and writes systemd unit files. It keeps every
// line it does not change exactly as it was, comments and layout
// included, so saving an edited file produces a minimal diff.
package unitfile

import (
	"strings"
)

// Kind classifies a line.
type Kind int

const (
	Blank Kind = iota
	Comment
	Section
	Entry
	Invalid // text that is neither, which systemd warns about and ignores
)

// Line is one logical line: a continued entry spans several physical lines.
type Line struct {
	Kind  Kind
	Name  string // section name for Section, key for Entry
	Value string // for Entry, with continuations joined by spaces
	raw   []string
}

// Raw is the line's text as it will be written.
func (l *Line) Raw() string {
	if l.raw != nil {
		return strings.Join(l.raw, "\n")
	}
	switch l.Kind {
	case Section:
		return "[" + l.Name + "]"
	case Entry:
		return l.Name + "=" + l.Value
	}
	return ""
}

// Modified reports whether the line was changed since it was parsed.
func (l *Line) Modified() bool { return l.raw == nil }

// File is a parsed unit file: a flat list of lines.
type File struct {
	Lines []*Line
	// NoFinalNewline is set when the source did not end in a newline.
	NoFinalNewline bool
}

// Parse reads a unit file. It never fails: lines systemd would reject are
// kept as Invalid.
func Parse(src string) *File {
	f := &File{}
	if src == "" {
		return f
	}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	f.NoFinalNewline = !strings.HasSuffix(src, "\n")
	phys := strings.Split(strings.TrimSuffix(src, "\n"), "\n")
	for i := 0; i < len(phys); i++ {
		p := phys[i]
		t := strings.TrimSpace(p)
		l := &Line{raw: []string{p}}
		switch {
		case t == "":
			l.Kind = Blank
		case t[0] == '#' || t[0] == ';':
			l.Kind = Comment
		case t[0] == '[' && strings.HasSuffix(t, "]"):
			l.Kind, l.Name = Section, t[1:len(t)-1]
		case strings.Contains(t, "="):
			l.Kind = Entry
			k, v, _ := strings.Cut(t, "=")
			l.Name = strings.TrimSpace(k)
			parts := []string{}
			for {
				v = strings.TrimSpace(v)
				if !strings.HasSuffix(v, `\`) || i+1 >= len(phys) {
					parts = append(parts, v)
					break
				}
				parts = append(parts, strings.TrimSpace(strings.TrimSuffix(v, `\`)))
				// Comment lines inside a continuation are skipped by systemd.
				for i+1 < len(phys) {
					i++
					l.raw = append(l.raw, phys[i])
					n := strings.TrimSpace(phys[i])
					if n == "" || (n[0] != '#' && n[0] != ';') {
						v = phys[i]
						break
					}
					v = ""
				}
			}
			l.Value = strings.TrimSpace(strings.Join(nonEmpty(parts), " "))
		default:
			l.Kind = Invalid
		}
		f.Lines = append(f.Lines, l)
	}
	return f
}

func nonEmpty(s []string) []string {
	out := s[:0:0]
	for _, x := range s {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

// String renders the file.
func (f *File) String() string {
	var b strings.Builder
	for i, l := range f.Lines {
		b.WriteString(l.Raw())
		if i < len(f.Lines)-1 || !f.NoFinalNewline {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Clone returns a deep copy.
func (f *File) Clone() *File {
	c := &File{NoFinalNewline: f.NoFinalNewline}
	for _, l := range f.Lines {
		n := *l
		if l.raw != nil {
			n.raw = append([]string(nil), l.raw...)
		}
		c.Lines = append(c.Lines, &n)
	}
	return c
}

// SectionOf returns the section line i belongs to, or "".
func (f *File) SectionOf(i int) string {
	for ; i >= 0; i-- {
		if f.Lines[i].Kind == Section {
			return f.Lines[i].Name
		}
	}
	return ""
}

// Sections lists the file's section names in order, without repeats.
func (f *File) Sections() []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range f.Lines {
		if l.Kind == Section && !seen[l.Name] {
			seen[l.Name] = true
			out = append(out, l.Name)
		}
	}
	return out
}

// Entries returns the entries in a section (all of its occurrences).
func (f *File) Entries(section string) []*Line {
	var out []*Line
	cur := ""
	for _, l := range f.Lines {
		switch l.Kind {
		case Section:
			cur = l.Name
		case Entry:
			if cur == section {
				out = append(out, l)
			}
		}
	}
	return out
}

// Values returns what a key resolves to in a section, as systemd applies
// assignments in order: an empty assignment resets the list.
func (f *File) Values(section, key string) []string {
	var out []string
	for _, l := range f.Entries(section) {
		if l.Name != key {
			continue
		}
		if l.Value == "" {
			out = nil
			continue
		}
		out = append(out, l.Value)
	}
	return out
}

// Get returns the last value assigned to key, which is the one that counts
// for single-valued settings.
func (f *File) Get(section, key string) (string, bool) {
	v := f.Values(section, key)
	if len(v) == 0 {
		return "", false
	}
	return v[len(v)-1], true
}

// Index returns the position of l in f.Lines, or -1.
func (f *File) Index(l *Line) int {
	for i, x := range f.Lines {
		if x == l {
			return i
		}
	}
	return -1
}

// Set changes an entry's value.
func (l *Line) Set(value string) {
	l.Value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	l.raw = nil
}

// Remove deletes a line.
func (f *File) Remove(l *Line) {
	if i := f.Index(l); i >= 0 {
		f.Lines = append(f.Lines[:i], f.Lines[i+1:]...)
	}
}

// AddSection appends a section header if the file lacks it, placed by
// order (for example Unit, Service, Install), and returns its line.
func (f *File) AddSection(name string, order []string) *Line {
	for _, l := range f.Lines {
		if l.Kind == Section && l.Name == name {
			return l
		}
	}
	rank := func(s string) int {
		for i, o := range order {
			if o == s {
				return i
			}
		}
		return len(order)
	}
	// Insert before the first section that sorts after this one.
	at := len(f.Lines)
	for i, l := range f.Lines {
		if l.Kind == Section && rank(l.Name) > rank(name) {
			at = i
			break
		}
	}
	hdr := &Line{Kind: Section, Name: name}
	ins := []*Line{hdr, {Kind: Blank}}
	if at > 0 && f.Lines[at-1].Kind != Blank {
		ins = append([]*Line{{Kind: Blank}}, ins...)
	}
	if at == len(f.Lines) {
		ins = ins[:len(ins)-1] // no trailing blank at the end of the file
	}
	f.Lines = append(f.Lines[:at], append(ins, f.Lines[at:]...)...)
	return hdr
}

// Add appends an entry to a section, after its last entry, creating the
// section if needed. after, if non-nil, places it right after that line.
func (f *File) Add(section, key, value string, order []string, after *Line) *Line {
	hdr := f.AddSection(section, order)
	l := &Line{Kind: Entry, Name: key}
	l.Set(value)
	at := -1
	if after != nil {
		at = f.Index(after)
	}
	if at < 0 {
		h := f.Index(hdr)
		at = h
		for i := h + 1; i < len(f.Lines) && f.Lines[i].Kind != Section; i++ {
			if f.Lines[i].Kind == Entry {
				at = i
			}
		}
	}
	f.Lines = append(f.Lines[:at+1], append([]*Line{l}, f.Lines[at+1:]...)...)
	return l
}
