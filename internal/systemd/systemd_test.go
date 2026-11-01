package systemd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	etc, vendor := filepath.Join(root, "etc"), filepath.Join(root, "vendor")
	t.Setenv("SYSDTUI_UNIT_PATH", etc+":"+vendor)

	write(t, filepath.Join(vendor, "a.service"), "[Service]\nExecStart=/a\n")
	write(t, filepath.Join(etc, "a.service"), "[Service]\nExecStart=/a2\n") // shadows the vendor file
	write(t, filepath.Join(vendor, "b.service"), "[Service]\nExecStart=/b\n")
	write(t, filepath.Join(vendor, "b.service.d", "10-x.conf"), "")
	write(t, filepath.Join(etc, "b.service.d", "20-y.conf"), "")
	write(t, filepath.Join(vendor, "b.service.d", "20-y.conf"), "") // replaced by /etc's
	write(t, filepath.Join(vendor, "service.d", "05-all.conf"), "")
	write(t, filepath.Join(vendor, "t@.service"), "")
	write(t, filepath.Join(vendor, "README"), "")
	if err := os.Symlink("/dev/null", filepath.Join(etc, "m.service")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(vendor, "b.service"), filepath.Join(etc, "alias.service")); err != nil {
		t.Fatal(err)
	}

	units := Discover(System)
	byName := map[string]*Unit{}
	var names []string
	for _, u := range units {
		byName[u.Name] = u
		names = append(names, u.Name)
	}
	if got := strings.Join(names, " "); got != "a.service alias.service b.service m.service t@.service" {
		t.Fatalf("units = %s", got)
	}
	a := byName["a.service"]
	if a.Origin != Admin || len(a.Shadowed) != 1 || !strings.HasPrefix(a.Path, etc) {
		t.Errorf("a.service = %+v", a)
	}
	b := byName["b.service"]
	var drops []string
	for _, d := range b.DropIns {
		drops = append(drops, strings.TrimPrefix(d, root+"/"))
	}
	if got := strings.Join(drops, " "); got != "vendor/service.d/05-all.conf vendor/b.service.d/10-x.conf etc/b.service.d/20-y.conf" {
		t.Errorf("drop-ins = %s", got)
	}
	if !byName["m.service"].Masked {
		t.Error("m.service is not masked")
	}
	if byName["alias.service"].AliasOf != "b.service" {
		t.Errorf("alias.service AliasOf = %q", byName["alias.service"].AliasOf)
	}
	if WriteDir(System) != etc || OverridePath(System, "b.service") != filepath.Join(etc, "b.service.d", "override.conf") {
		t.Error("write paths do not use the first directory")
	}
}

func TestValidName(t *testing.T) {
	for n, want := range map[string]bool{
		"foo.service": true, "foo@.service": true, "a-b.mount": true, ".service": false,
		"foo": false, "foo.conf": false, "a b.service": false, "x/y.service": false,
	} {
		if ValidName(n) != want {
			t.Errorf("ValidName(%q) = %v", n, !want)
		}
	}
}

func TestEscapePath(t *testing.T) {
	for path, want := range map[string]string{
		"/":              "-",
		"/mnt/data":      "mnt-data",
		"/srv/my-files/": `srv-my\x2dfiles`,
		"/home/a b":      `home-a\x20b`,
	} {
		got := EscapePath(path)
		if got != want {
			t.Errorf("EscapePath(%q) = %q, want %q", path, got, want)
		}
		if back := UnescapePath(got); back != strings.TrimSuffix(path, "/") && path != "/" {
			t.Errorf("UnescapePath(%q) = %q", got, back)
		}
	}
	if out, err := exec.Command("systemd-escape", "--path", "/srv/my-files").Output(); err == nil {
		if got := strings.TrimSpace(string(out)); got != EscapePath("/srv/my-files") {
			t.Errorf("systemd-escape says %q, EscapePath %q", got, EscapePath("/srv/my-files"))
		}
	}
}

func TestTemplates(t *testing.T) {
	for _, n := range []string{"a.service", "a.timer", "a.socket", "a.path", "mnt-data.mount", "a.target", "a.slice"} {
		tpl := Template(n, System)
		if !strings.HasPrefix(tpl, "[Unit]\nDescription=") {
			t.Errorf("%s template: %q", n, tpl)
		}
	}
	if !strings.Contains(Template("mnt-data.mount", System), "Where=/mnt/data\n") {
		t.Error("mount template does not derive Where= from the name")
	}
	if !strings.Contains(Template("a.service", User), "WantedBy=default.target") {
		t.Error("user services should be wanted by default.target")
	}
}

func TestCommands(t *testing.T) {
	if got := Quote("it's"); got != `'it'\''s'` {
		t.Errorf("Quote = %s", got)
	}
	if got := Systemctl(User, "restart", "x.service").String(); got != "systemctl --user restart x.service" {
		t.Errorf("Systemctl = %s", got)
	}
	if got := Systemctl(System, "status", "x.service"); got[0] == "sudo" {
		t.Error("status should not need sudo")
	}

	// The install script really copies the file and keeps a backup.
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged")
	target := filepath.Join(dir, "sub", "x.service")
	write(t, staged, "new\n")
	write(t, target, "old\n")
	c := InstallCommand(staged, target)[1:] // without sudo
	if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if b, _ := os.ReadFile(target); string(b) != "new\n" {
		t.Errorf("target = %q", b)
	}
	if b, _ := os.ReadFile(target + "~"); string(b) != "old\n" {
		t.Errorf("backup = %q", b)
	}

	script := Script([]Command{{"true"}, {"false"}, {"touch", filepath.Join(dir, "never")}}, []string{staged})
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdin = strings.NewReader("\n")
	if err := cmd.Run(); err == nil {
		t.Error("script should fail when a command fails")
	}
	if _, err := os.Stat(filepath.Join(dir, "never")); err == nil {
		t.Error("script kept going after a failure")
	}
	if _, err := os.Stat(staged); err == nil {
		t.Error("script did not clean up")
	}
}

func TestWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.service.d", "override.conf")
	if err := Write(p, []byte("one\n")); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, []byte("two\n")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "two\n" {
		t.Errorf("file = %q", b)
	}
	if b, _ := os.ReadFile(p + "~"); string(b) != "one\n" {
		t.Errorf("backup = %q", b)
	}
}

func TestParseStates(t *testing.T) {
	m := ParseStates([]byte("a.service enabled enabled\nb.timer   disabled enabled\n\n"))
	if m["a.service"] != "enabled" || m["b.timer"] != "disabled" {
		t.Errorf("states = %v", m)
	}
}
