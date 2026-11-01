package lint

import (
	"strings"
	"testing"

	"github.com/lonedevel/sysdtui/internal/catalog"
	"github.com/lonedevel/sysdtui/internal/unitfile"
)

var cat = func() *catalog.Catalog {
	c, err := catalog.Embedded()
	if err != nil {
		panic(err)
	}
	return c
}()

func messages(in Input) []string {
	var out []string
	for _, is := range Check(cat, in) {
		out = append(out, is.Severity.String()+": "+is.Msg)
	}
	return out
}

func expect(t *testing.T, in Input, want ...string) {
	t.Helper()
	got := messages(in)
	for _, w := range want {
		found := false
		for _, g := range got {
			if strings.Contains(g, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("no issue containing %q in %q", w, got)
		}
	}
	if len(want) == 0 && len(got) > 0 {
		t.Errorf("unexpected issues: %q", got)
	}
}

func unit(src, typ string) Input {
	return Input{File: unitfile.Parse(src), Type: typ}
}

func TestCleanService(t *testing.T) {
	expect(t, unit(`[Unit]
Description=ok
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=/usr/bin/foo --flag
Restart=on-failure
RestartSec=5s
ProtectSystem=true
ProtectHome=read-only
PrivateTmp=yes
StandardOutput=file:/var/log/foo.log
Environment=A=1 B=2
X-Custom=whatever

[Install]
WantedBy=multi-user.target
`, "service"))
}

func TestValues(t *testing.T) {
	expect(t, unit("[Service]\nExecStart=/bin/x\nRestart=sometimes\nPrivateTmp=maybe\n", "service"),
		"error: Restart= must be one of", "error: PrivateTmp= takes a boolean", "info: no [Install]")
	expect(t, unit("[Service]\nExecStart=/bin/x\nRestart=%i\n[Install]\nWantedBy=a.target\n", "service"))
}

func TestStructure(t *testing.T) {
	expect(t, unit("Description=x\n[Service]\nExecStart=/a\nExecStart=/b\nFrobnicate=1\n[Timer]\nOnCalendar=daily\n[Install]\nWantedBy=a.target\n", "service"),
		"error: Description= is outside any section",
		"error: only Type=oneshot services may have several ExecStart=",
		"warning: unknown directive Frobnicate=",
		"warning: [Timer] is not a section of .service units")
	expect(t, unit("[Service]\nType=simple\n", "service"), "error: Type=simple needs ExecStart=")
	expect(t, unit("[Service]\nType=oneshot\nExecStart=/a\nExecStart=/b\n[Install]\nWantedBy=a.target\n", "service"))
	expect(t, unit("[Timer]\nPersistent=true\n", "timer"), "error: a timer needs OnCalendar=")
	expect(t, unit("[Socket]\nAccept=yes\n", "socket"), "error: a socket needs a Listen")
	expect(t, unit("[Path]\nDirectoryNotEmpty=/var/spool/x\n[Install]\nWantedBy=paths.target\n", "path"))
	expect(t, unit("[Service]\nExecStart=/a\nMemoryLimit=1G\n[Install]\nWantedBy=a.target\n", "service"), "info: MemoryLimit= is deprecated")
}

func TestDropIn(t *testing.T) {
	base := unitfile.Parse("[Service]\nType=simple\nExecStart=/usr/bin/foo\nEnvironment=A=1\n[Unit]\nAfter=a.target\n")
	in := Input{File: unitfile.Parse("[Service]\nExecStart=/usr/bin/foo --debug\nEnvironment=B=2\n[Unit]\nAfter=b.target\n"),
		Type: "service", Base: base, DropIn: true}
	expect(t, in, "error: a second ExecStart= makes the service fail to load", "warning: this adds to the unit's Environment= list")
	for _, m := range messages(in) {
		if strings.Contains(m, "After=") {
			t.Errorf("After= cannot be reset, so adding to it is fine: %s", m)
		}
	}
	// Reset first: clean. Drop-ins need no [Install] or ExecStart= of their own.
	in.File = unitfile.Parse("[Service]\nExecStart=\nExecStart=/usr/bin/foo --debug\n")
	expect(t, in)
}
