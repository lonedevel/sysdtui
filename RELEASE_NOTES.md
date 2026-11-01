# Release notes

## v0.1.0

First release.

- Browse system and user units with their state, origin, drop-ins and shadowed files.
- Edit units with each directive's documentation beside it, from the installed systemd man pages or an embedded copy of systemd 255's.
- Cycle and complete enum, boolean and unit-name values; add directives from a picker of those valid in each section.
- Override packaged units through drop-ins, with the unit's settings shown and list resets added for you; or edit a full copy.
- Lint as you type, and run `systemd-analyze verify` on the edited text.
- Create units from templates for services, timers, sockets, paths, mounts, targets and slices.
- Save with a diff, backups and sudo when needed, then reload, restart or enable.
- `--lint`, `--list` and `--dump-catalog` for scripts.
