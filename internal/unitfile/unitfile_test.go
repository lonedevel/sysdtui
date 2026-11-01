package unitfile

import (
	"strings"
	"testing"
)

const sample = `# Managed by hand
[Unit]
Description=Example
After=network.target
; old style comment

[Service]
ExecStart=/usr/bin/foo \
    --verbose \
# a comment inside the continuation
    --port 80
Environment=A=1
Environment=
Environment=B=2
garbage line

[Install]
WantedBy=multi-user.target`

func TestRoundTrip(t *testing.T) {
	for _, src := range []string{sample, sample + "\n", "", "\n\n", "[A]\nk=v\n"} {
		if got := Parse(src).String(); got != src {
			t.Errorf("round trip changed the file:\n%q\n%q", src, got)
		}
	}
}

func TestParse(t *testing.T) {
	f := Parse(sample)
	if got := f.Sections(); strings.Join(got, ",") != "Unit,Service,Install" {
		t.Fatalf("sections = %v", got)
	}
	if v, _ := f.Get("Unit", "Description"); v != "Example" {
		t.Errorf("Description = %q (trailing space should be trimmed)", v)
	}
	if v, _ := f.Get("Service", "ExecStart"); v != "/usr/bin/foo --verbose --port 80" {
		t.Errorf("continued ExecStart = %q", v)
	}
	if v := f.Values("Service", "Environment"); len(v) != 1 || v[0] != "B=2" {
		t.Errorf("Environment after reset = %q", v)
	}
	invalid := 0
	for _, l := range f.Lines {
		if l.Kind == Invalid {
			invalid++
		}
	}
	if invalid != 1 {
		t.Errorf("invalid lines = %d, want 1", invalid)
	}
	if _, ok := f.Get("Service", "Nope"); ok {
		t.Error("Get found a missing key")
	}
}

func TestEditsKeepOtherLines(t *testing.T) {
	f := Parse(sample)
	for _, l := range f.Entries("Unit") {
		if l.Name == "After" {
			l.Set("network-online.target")
		}
	}
	got := f.String()
	want := strings.Replace(sample, "After=network.target", "After=network-online.target", 1)
	if got != want {
		t.Errorf("Set changed more than one line:\n%s", got)
	}
}

func TestAdd(t *testing.T) {
	order := []string{"Unit", "Service", "Install"}
	f := Parse("[Unit]\nDescription=x\n\n[Install]\nWantedBy=a.target\n")
	f.Add("Service", "ExecStart", "/bin/true", order, nil)
	f.Add("Unit", "After", "b.target", order, nil)
	want := "[Unit]\nDescription=x\nAfter=b.target\n\n[Service]\nExecStart=/bin/true\n\n[Install]\nWantedBy=a.target\n"
	if got := f.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	// After a given line.
	f = Parse("[Service]\nA=1\nB=2\n")
	a := f.Entries("Service")[0]
	f.Add("Service", "C", "3", order, a)
	if got := f.String(); got != "[Service]\nA=1\nC=3\nB=2\n" {
		t.Errorf("insert after: %q", got)
	}

	// Into an empty file: no leading blank line.
	f = Parse("")
	f.Add("Unit", "Description", "x", order, nil)
	if got := f.String(); got != "[Unit]\nDescription=x\n" {
		t.Errorf("empty file: %q", got)
	}
}

func TestRemoveAndClone(t *testing.T) {
	f := Parse(sample)
	c := f.Clone()
	f.Remove(f.Entries("Unit")[0])
	if _, ok := f.Get("Unit", "Description"); ok {
		t.Error("Remove left the entry")
	}
	if _, ok := c.Get("Unit", "Description"); !ok {
		t.Error("Clone shares lines with the original")
	}
	if c.String() != sample {
		t.Error("Clone does not round-trip")
	}
}
