# sysdtui

A terminal editor for systemd unit files that keeps the documentation beside you.

Each directive is described next to the field you're editing, taken straight from systemd's own man pages. The editor knows which directives belong in which section and which values they accept. It also flags mistakes before systemd sees them, and when you save it can reload, restart or enable the unit.

```
sysdtui › web.service drop-in /etc/systemd/system/web.service.d/override.conf  ● modified
  [Unit] from the unit                         │ ExecStart=  [Service] · systemd.service(5)
    Description=Web frontend                   │ command line; prefixes: - (ignore failure) @ + ! !!
    After=network-online.target                │ may be repeated · empty value resets
                                               │ the unit sets:
● [Service]                                    │   /usr/bin/web --port 80
●   ExecStart=(empty: resets)                  │
●   ExecStart=/usr/bin/web --port 80 --debug   │ Commands that are executed when this service is
    Environment=PORT=80                        │ started. The value is split into zero or more command
    Restart=on-failure                         │ lines according to the rules described in the section
```

## Features

- **Browse units** on the system or user search path, with their state (enabled, static, masked…), where they come from (admin, vendor, runtime), drop-ins and the files they shadow.
- **Edit with documentation.** The docs pane shows the focused directive's text from `systemd.service(5)`, `systemd.exec(5)` and the other pages, along with the kind of value it takes: boolean, enum choices, time span, unit list or command line.
- **Choose instead of typing.** `space` cycles a boolean or enum, and `tab` completes enum values and unit names (`After=`, `WantedBy=`…). The `a` picker lists every directive valid in the section, with a summary.
- **Override packaged units the right way.** Opening a vendor unit edits a drop-in (`/etc/systemd/system/foo.service.d/override.conf`), as `systemctl edit` does, with the unit's own settings shown underneath. Overriding a list such as `ExecStart=` adds the empty reset line for you. `F` copies the whole unit instead, like `systemctl edit --full`.
- **Catch mistakes as you type**: unknown directives, values outside a directive's choices, a second `ExecStart=` on a non-oneshot service, timers with nothing to trigger them, and drop-ins that add to a list when they meant to replace it. `v` also runs `systemd-analyze verify` on the edited text before you save.
- **New units from templates** for services, timers, sockets, paths, mounts (named from the mount point), targets and slices.
- **Save safely.** Unchanged lines and comments are written back exactly as they were. The previous version is kept as `file~`. Writing to a root-owned directory goes through `sudo`, and sysdtui shows you every command before it runs. After saving you can reload, restart, or enable and start the unit.
- **`--lint`** checks unit files from scripts or CI and prints `file:line: severity: message`.

## Install

```sh
go install github.com/lonedevel/sysdtui/cmd/sysdtui@latest
```

or build from a checkout with `make build` (the binary lands in `bin/sysdtui`). sysdtui is for Linux, where systemd runs.

## Usage

```sh
sysdtui                        # browse system units
sysdtui --user                 # browse your user units (~/.config/systemd/user)
sysdtui nginx.service          # open a unit: a drop-in for packaged units
sysdtui --override sshd.service
sysdtui --new backup.timer     # start a new unit from a template
sysdtui ./myapp.service        # edit any unit file by path
sysdtui --lint units/*.service # check files and exit 1 on errors
sysdtui --list                 # print the units found
sysdtui --dump-catalog Service # the directive catalog as JSON
```

Run sysdtui as yourself. When a save needs root it uses `sudo`, and you'll see the password prompt and the exact commands.

### Keys

**Unit list:** `enter` edit · `o` override drop-in · `F` edit a full copy · `n` new unit · `/` filter · `t`/`T` filter by type · `u` switch between system and user · `s` status · `l` journal · `r` refresh · `q` quit

**Editor:** `enter` edit value · `space` cycle a boolean or enum · `a` add a directive · `S` add a section · `d` delete · `J`/`K` move a line · `n` next problem · `u` undo · `p` diff · `v` verify · `V` show/hide verify output · `[`/`]` scroll the docs · `ctrl+s` save · `esc` back

**Editing a value:** `tab`/`↓` next choice · `shift+tab`/`↑` previous · `enter` done · `esc` cancel

## Where the documentation comes from

sysdtui reads the `systemd.*(5)` man pages installed on the machine (located with `man -w`), so the docs match your systemd version. Minimal systems and containers often ship without man pages, so the binary also embeds a catalog generated from systemd 255's pages. Selecting a section header in the editor shows which one is in use. Set `SYSDTUI_EMBEDDED_DOCS=1` to always use the embedded copy.

Directives in the catalog: [Unit] and [Install] from `systemd.unit(5)`, plus each type's page (`systemd.service(5)`, `.socket`, `.timer`, `.path`, `.mount`, `.automount`, `.swap`, `.slice`, `.scope`), with `systemd.exec(5)`, `systemd.kill(5)` and `systemd.resource-control(5)` added to the sections they apply to.

## Development

```sh
make build      # bin/sysdtui
make test
make lint       # gofmt + go vet
make catalog    # regenerate the embedded catalog from /usr/share/man/man5
```

`SYSDTUI_UNIT_PATH=dir1:dir2` replaces the unit search path (the first directory is where edits go), which is handy for trying sysdtui without touching `/etc`.

## License

Apache 2.0, see [LICENSE](LICENSE). The embedded directive catalog and the test man pages come from systemd's documentation, which is LGPL-2.1-or-later; see [NOTICE](NOTICE).
