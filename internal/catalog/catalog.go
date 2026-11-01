// Package catalog knows the directives systemd accepts in each section of
// a unit file: their documentation, the kind of value they take and, for
// enumerations, the allowed values. It reads the systemd.*(5) man pages
// installed on the machine and falls back to a copy embedded at build time.
package catalog

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Kind is the type of value a directive takes.
type Kind string

const (
	KindString  Kind = "string"
	KindBool    Kind = "bool"
	KindEnum    Kind = "enum"
	KindTime    Kind = "time"
	KindCommand Kind = "command"
	KindUnits   Kind = "units" // a space-separated list of unit names
	KindPath    Kind = "path"
	KindSize    Kind = "size"
)

// Directive describes one setting, such as Restart= in [Service].
type Directive struct {
	Name     string   `json:"name"`
	Sections []string `json:"sections"`
	Page     string   `json:"page"`
	Kind     Kind     `json:"kind"`
	Values   []string `json:"values,omitempty"`
	// Multi is set when the directive may be given more than once and its
	// values accumulate into a list.
	Multi bool `json:"multi,omitempty"`
	// Resets is set when assigning the empty string clears the list, which
	// a drop-in must do before replacing a list from the unit it overrides.
	Resets bool   `json:"resets,omitempty"`
	Doc    []Para `json:"doc"`
}

// Summary is the first sentence of the documentation.
func (d *Directive) Summary() string {
	if len(d.Doc) == 0 {
		return ""
	}
	s := d.Doc[0].Text
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '.' && s[i+1] == ' ' && !abbrev(s[:i]) {
			return s[:i+1]
		}
	}
	return s
}

func abbrev(s string) bool {
	for _, a := range []string{"e.g", "i.e", "etc", "vs", "cf"} {
		if strings.HasSuffix(s, a) {
			return true
		}
	}
	return false
}

// Catalog indexes directives by section, then by name.
type Catalog struct {
	// Source says where the documentation came from.
	Source     string                           `json:"source"`
	Directives []*Directive                     `json:"directives"`
	index      map[string]map[string]*Directive // section → name → directive
}

// Pages lists the man pages read, and the sections their directives
// belong to. systemd.unit is split by heading in pageSections.
var Pages = map[string][]string{
	"systemd.unit":             nil,
	"systemd.service":          {"Service"},
	"systemd.socket":           {"Socket"},
	"systemd.timer":            {"Timer"},
	"systemd.path":             {"Path"},
	"systemd.mount":            {"Mount"},
	"systemd.automount":        {"Automount"},
	"systemd.swap":             {"Swap"},
	"systemd.slice":            {"Slice"},
	"systemd.scope":            {"Scope"},
	"systemd.exec":             {"Service", "Socket", "Mount", "Swap"},
	"systemd.kill":             {"Service", "Socket", "Mount", "Swap", "Scope"},
	"systemd.resource-control": {"Service", "Socket", "Mount", "Swap", "Slice", "Scope"},
}

// Sections lists the sections each unit type may have, in file order.
var Sections = map[string][]string{
	"service":   {"Unit", "Service", "Install"},
	"socket":    {"Unit", "Socket", "Install"},
	"timer":     {"Unit", "Timer", "Install"},
	"path":      {"Unit", "Path", "Install"},
	"mount":     {"Unit", "Mount", "Install"},
	"automount": {"Unit", "Automount", "Install"},
	"swap":      {"Unit", "Swap", "Install"},
	"target":    {"Unit", "Install"},
	"device":    {"Unit", "Install"},
	"slice":     {"Unit", "Slice", "Install"},
	"scope":     {"Unit", "Scope"},
}

// skipHeadings hold examples or reference tables, not settings.
var skipHeadings = map[string]bool{
	"EXAMPLES": true, "SEE ALSO": true, "NOTES": true, "HISTORY": true,
	"ENVIRONMENT VARIABLES IN SPAWNED PROCESSES": true,
	"PROCESS EXIT CODES":                         true,
}

func pageSections(page, heading string) []string {
	if page != "systemd.unit" {
		if skipHeadings[heading] {
			return nil
		}
		return Pages[page]
	}
	switch {
	case strings.Contains(heading, "[UNIT]"):
		return []string{"Unit"}
	case strings.Contains(heading, "[INSTALL]"):
		return []string{"Install"}
	}
	return nil
}

// Parse adds the directives documented by a page's troff source.
func (c *Catalog) Parse(page, src string) {
	for _, e := range scanEntries(src) {
		secs := pageSections(page, e.heading)
		if secs == nil {
			continue
		}
		doc := render(e.body)
		for _, n := range e.names {
			d := &Directive{Name: n, Sections: secs, Page: page, Doc: doc}
			infer(d, e.body)
			c.add(d)
		}
	}
}

func (c *Catalog) add(d *Directive) {
	if c.index == nil {
		c.index = map[string]map[string]*Directive{}
	}
	// A name documented twice for the same sections (some pages repeat a
	// tag with an argument) keeps the first, fuller entry.
	for _, s := range d.Sections {
		if c.index[s][d.Name] != nil {
			return
		}
	}
	c.Directives = append(c.Directives, d)
	for _, s := range d.Sections {
		if c.index[s] == nil {
			c.index[s] = map[string]*Directive{}
		}
		c.index[s][d.Name] = d
	}
}

func (c *Catalog) reindex() {
	ds := c.Directives
	c.Directives, c.index = nil, nil
	for _, d := range ds {
		c.add(d)
	}
}

// Lookup returns the directive name in section, or nil.
func (c *Catalog) Lookup(section, name string) *Directive {
	if c == nil {
		return nil
	}
	return c.index[section][name]
}

// InSection returns a section's directives sorted by name.
func (c *Catalog) InSection(section string) []*Directive {
	var out []*Directive
	for _, d := range c.index[section] {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Len is the number of directives.
func (c *Catalog) Len() int { return len(c.Directives) }

//go:embed systemd.json.gz
var embedded []byte

// Embedded returns the catalog generated at build time (see
// cmd/gencatalog), for machines without systemd's man pages.
func Embedded() (*Catalog, error) {
	zr, err := gzip.NewReader(bytes.NewReader(embedded))
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err := json.NewDecoder(zr).Decode(&c); err != nil {
		return nil, err
	}
	c.reindex()
	return &c, nil
}

// FromDir reads the pages from a man5 directory holding files like
// systemd.service.5 or systemd.service.5.gz.
func FromDir(dir string) (*Catalog, error) {
	c := &Catalog{Source: dir}
	found := 0
	for page := range Pages {
		for _, name := range []string{page + ".5.gz", page + ".5"} {
			src, err := readPage(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			c.Parse(page, src)
			found++
			break
		}
	}
	if found == 0 {
		return nil, fmt.Errorf("no systemd man pages in %s", dir)
	}
	return c, nil
}

// FromMan finds the pages with man -w, so it follows MANPATH.
func FromMan() (*Catalog, error) {
	out, err := exec.Command("man", "-w", "systemd.service").Output()
	if err != nil {
		return nil, fmt.Errorf("systemd.service(5) is not installed")
	}
	return FromDir(filepath.Dir(strings.TrimSpace(string(out))))
}

func readPage(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		zr, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		r = zr
	}
	b, err := io.ReadAll(r)
	return string(b), err
}

// Load returns the catalog from the installed man pages, which match the
// installed systemd, or else the embedded one.
func Load() *Catalog {
	if os.Getenv("SYSDTUI_EMBEDDED_DOCS") == "" {
		if c, err := FromMan(); err == nil && c.Len() > 100 {
			return c
		}
	}
	c, err := Embedded()
	if err != nil {
		return &Catalog{Source: "none"}
	}
	return c
}

// Marshal writes the catalog as gzipped JSON, the embedded form.
func (c *Catalog) Marshal(w io.Writer) error {
	sort.SliceStable(c.Directives, func(i, j int) bool {
		a, b := c.Directives[i], c.Directives[j]
		if a.Page != b.Page {
			return a.Page < b.Page
		}
		return a.Name < b.Name
	})
	zw, _ := gzip.NewWriterLevel(w, gzip.BestCompression)
	enc := json.NewEncoder(zw)
	if err := enc.Encode(c); err != nil {
		return err
	}
	return zw.Close()
}

var (
	boldWord  = regexp.MustCompile(`\\fB([a-z0-9][a-z0-9+_.:\\-]*)\\fR(\(?)`)
	firstStop = regexp.MustCompile(`(?m)^\.(sp|PP|RS)`)
	// boolOrList matches "takes a boolean argument, or a space-separated
	// list of…" within one sentence.
	boolOrList = regexp.MustCompile(`boolean[^.]*\bor (a|an|one or more)\b[^.]*\blist\b`)
)

// infer sets Kind, Values, Multi and Resets from the documentation's
// wording, then applies the corrections in overrides.go.
func infer(d *Directive, body []string) {
	src := strings.Join(body, "\n")
	// The first paragraph is where systemd's pages state the value type.
	first := src
	if loc := firstStop.FindStringIndex(src); loc != nil {
		first = src[:loc[0]]
	}
	text := strings.ToLower(inline(strings.ReplaceAll(first, "\n", " ")))
	all := strings.ToLower(inline(strings.ReplaceAll(src, "\n", " ")))

	switch {
	case strings.HasPrefix(d.Name, "Exec"):
		d.Kind = KindCommand
	case strings.Contains(text, "takes a boolean") || strings.Contains(text, "takes boolean") ||
		strings.Contains(text, "boolean argument") || strings.Contains(text, "a boolean value"):
		d.Kind = KindBool
		d.Values = []string{"yes", "no"}
		if strings.Contains(text, "special value") || strings.Contains(text, "or one of") {
			d.Values = append(d.Values, words(first, d.Values)...)
		}
		if boolOrList.MatchString(text) {
			// "a boolean or a list of…": the values are only suggestions.
			d.Kind = KindString
		}
	case strings.Contains(text, "one of") || strings.Contains(text, "takes either") ||
		strings.Contains(text, "may be set to") || strings.Contains(text, "set to either"):
		if v := words(first, nil); len(v) >= 2 {
			d.Kind = KindEnum
			d.Values = v
		}
	}
	if d.Kind == "" {
		switch {
		case strings.Contains(text, "time span") || strings.Contains(text, "timespan") ||
			strings.HasSuffix(d.Name, "Sec") || strings.HasSuffix(d.Name, "USec"):
			d.Kind = KindTime
		case strings.Contains(text, "absolute path") || strings.Contains(text, "takes a path") ||
			strings.Contains(text, "takes a file system path") || strings.HasSuffix(d.Name, "Directory"):
			d.Kind = KindPath
		case strings.Contains(text, "size in bytes") || strings.Contains(text, "suffixed with k, m, g"):
			d.Kind = KindSize
		default:
			d.Kind = KindString
		}
	}
	d.Multi = strings.Contains(all, "more than once") || strings.Contains(all, "multiple times") ||
		strings.Contains(all, "space-separated list") || strings.Contains(all, "whitespace-separated list")
	d.Resets = strings.Contains(all, "empty string is assigned") || strings.Contains(all, "assigning the empty string") ||
		strings.Contains(all, "empty string assignment") || strings.Contains(all, "assigned the empty string")
	applyOverride(d)
}

// words collects the bold lowercase tokens in troff source: the way the
// pages set enumerated values.
func words(src string, have []string) []string {
	seen := map[string]bool{}
	for _, h := range have {
		seen[h] = true
	}
	var out []string
	for _, m := range boldWord.FindAllStringSubmatch(src, -1) {
		w := inline(m[1])
		if m[2] != "" || strings.Contains(w, ".") || seen[w] {
			continue // units like foo.service, man page refs
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}
