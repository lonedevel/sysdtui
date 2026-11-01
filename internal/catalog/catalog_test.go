package catalog

import (
	"strings"
	"testing"
)

func TestTagNames(t *testing.T) {
	cases := map[string]string{
		`\fIType=\fR`:                 "Type",
		`\fIBefore=\fR, \fIAfter=\fR`: "Before After",
		`\fICPUWeight=\fR\fI\fIweight\fR\fR, \fIStartupCPUWeight=\fR\fI\fIweight\fR\fR`:                                             "CPUWeight StartupCPUWeight",
		`\fIDevicePolicy=auto|closed|strict\fR`:                                                                                     "DevicePolicy",
		`\fIIPAddressAllow=\fR\fI\fIADDRESS[/PREFIXLENGTH]\&...\fR\fR, \fIIPAddressDeny=\fR\fI\fIADDRESS[/PREFIXLENGTH]\&...\fR\fR`: "IPAddressAllow IPAddressDeny",
		`Configures the mechanism`: "",
		`\fBsystemctl\fR`:          "",
	}
	for in, want := range cases {
		if got := strings.Join(tagNames(in), " "); got != want {
			t.Errorf("tagNames(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInline(t *testing.T) {
	cases := map[string]string{
		`\fBsystemd.time\fR(7)\&.`:                    "systemd.time(7).",
		`time\-sync\&.target`:                         "time-sync.target",
		`the service\*(Aqs binary`:                    "the service's binary",
		`\h'-04'\(bu\h'+03'`:                          "• ",
		`see \m[blue]\fBdocs\fR\m[]\&\s-2\u[1]\d\s+2`: "see docs",
	}
	for in, want := range cases {
		if got := inline(in); got != want {
			t.Errorf("inline(%q) = %q, want %q", in, got, want)
		}
	}
}

func fromTestdata(t *testing.T) *Catalog {
	t.Helper()
	c, err := FromDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPages(t *testing.T) {
	c := fromTestdata(t)
	for _, tc := range []struct {
		section string
		min     int
		names   []string
	}{
		{"Unit", 80, []string{"Description", "After", "Before", "Requires", "Wants", "ConditionPathExists", "StartLimitIntervalSec", "OnFailure"}},
		{"Install", 5, []string{"WantedBy", "RequiredBy", "Alias", "Also", "DefaultInstance"}},
		{"Timer", 15, []string{"OnCalendar", "OnBootSec", "OnUnitActiveSec", "Persistent", "RandomizedDelaySec", "Unit", "AccuracySec"}},
	} {
		got := c.InSection(tc.section)
		if len(got) < tc.min {
			t.Errorf("[%s]: %d directives, want at least %d", tc.section, len(got), tc.min)
		}
		for _, n := range tc.names {
			if c.Lookup(tc.section, n) == nil {
				t.Errorf("[%s] lacks %s=", tc.section, n)
			}
		}
	}
	if c.Lookup("Unit", "WantedBy") != nil {
		t.Error("WantedBy= was put in [Unit]")
	}
}

func TestInfer(t *testing.T) {
	c := fromTestdata(t)
	check := func(sec, name string, kind Kind, multi, resets bool) {
		t.Helper()
		d := c.Lookup(sec, name)
		if d == nil {
			t.Fatalf("no %s=", name)
		}
		if d.Kind != kind || d.Multi != multi || d.Resets != resets {
			t.Errorf("%s= kind=%s multi=%v resets=%v, want %s %v %v", name, d.Kind, d.Multi, d.Resets, kind, multi, resets)
		}
	}
	check("Timer", "Persistent", KindBool, false, false)
	check("Timer", "OnCalendar", KindString, true, true)
	check("Timer", "RandomizedDelaySec", KindTime, false, false)
	check("Unit", "After", KindUnits, true, false)
	check("Install", "WantedBy", KindUnits, true, false)
	check("Timer", "Unit", KindUnits, true, false)
}

func TestDocs(t *testing.T) {
	c := fromTestdata(t)
	d := c.Lookup("Timer", "OnCalendar")
	if !strings.HasPrefix(d.Summary(), "Defines realtime (i.e. wallclock) timers") {
		t.Errorf("summary = %q", d.Summary())
	}
	for _, p := range d.Doc {
		if strings.Contains(p.Text, `\f`) || strings.Contains(p.Text, `\&`) || strings.Contains(p.Text, `\-`) {
			t.Errorf("troff escape left in %q", p.Text)
		}
	}
	// The [Unit] page's bullet lists come out as "• text".
	bullets := 0
	for _, d := range c.Directives {
		for _, p := range d.Doc {
			if strings.HasPrefix(p.Text, "• ") {
				bullets++
			}
			if strings.HasPrefix(p.Text, "•") && !strings.HasPrefix(p.Text, "• ") {
				t.Errorf("%s: bullet without a space: %q", d.Name, p.Text)
			}
		}
	}
	if bullets == 0 {
		t.Error("no bulleted paragraphs found")
	}
}

func TestEmbedded(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if c.Len() < 400 {
		t.Errorf("embedded catalog has %d directives", c.Len())
	}
	typ := c.Lookup("Service", "Type")
	if typ == nil || typ.Kind != KindEnum || !strings.Contains(strings.Join(typ.Values, " "), "notify-reload") {
		t.Errorf("[Service] Type= = %+v", typ)
	}
	if mt := c.Lookup("Mount", "Type"); mt == nil || mt.Kind != KindString {
		t.Errorf("[Mount] Type= should be a free string: %+v", mt)
	}
	for _, sec := range []string{"Service", "Socket", "Mount", "Swap"} {
		if c.Lookup(sec, "User") == nil || c.Lookup(sec, "KillMode") == nil {
			t.Errorf("[%s] lacks the systemd.exec/kill settings", sec)
		}
	}
	if c.Lookup("Slice", "MemoryMax") == nil {
		t.Error("[Slice] lacks MemoryMax=")
	}
	if c.Lookup("Timer", "User") != nil {
		t.Error("[Timer] has exec settings")
	}
	if ip := c.Lookup("Service", "IPAddressDeny"); ip == nil {
		t.Error("IPAddressDeny= (a tag with an argument) is missing")
	}
	if rn := c.Lookup("Service", "RestrictNamespaces"); rn == nil || rn.Kind != KindString || len(rn.Values) < 2 {
		t.Errorf("RestrictNamespaces= (boolean or list) = %+v", rn)
	}
}
