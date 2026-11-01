package catalog

// unitLists take space-separated unit names, so the editor completes unit
// names for them.
var unitLists = map[string]bool{
	"Wants": true, "Requires": true, "Requisite": true, "BindsTo": true, "PartOf": true,
	"Upholds": true, "Conflicts": true, "Before": true, "After": true, "OnFailure": true,
	"OnSuccess": true, "PropagatesReloadTo": true, "ReloadPropagatedFrom": true,
	"PropagatesStopTo": true, "StopPropagatedFrom": true, "JoinsNamespaceOf": true,
	"WantedBy": true, "RequiredBy": true, "UpheldBy": true, "Also": true,
	"Unit": true, "Sockets": true, "Slice": true,
}

// override corrects what infer reads from the prose, for directives that
// people set most and whose wording is irregular.
type override struct {
	kind   Kind
	values []string
	multi  *bool
}

var yes, no = true, false

var overrides = map[string]override{
	"Type":                {kind: KindEnum, values: []string{"simple", "exec", "forking", "oneshot", "dbus", "notify", "notify-reload", "idle"}},
	"Restart":             {kind: KindEnum, values: []string{"no", "on-success", "on-failure", "on-abnormal", "on-watchdog", "on-abort", "always"}},
	"KillMode":            {kind: KindEnum, values: []string{"control-group", "mixed", "process", "none"}},
	"NotifyAccess":        {kind: KindEnum, values: []string{"none", "main", "exec", "all"}},
	"StandardInput":       {kind: KindEnum, values: []string{"null", "tty", "tty-force", "tty-fail", "data", "file:", "socket", "fd:"}},
	"StandardOutput":      {kind: KindEnum, values: []string{"inherit", "null", "tty", "journal", "kmsg", "journal+console", "kmsg+console", "file:", "append:", "truncate:", "socket", "fd:"}},
	"StandardError":       {kind: KindEnum, values: []string{"inherit", "null", "tty", "journal", "kmsg", "journal+console", "kmsg+console", "file:", "append:", "truncate:", "socket", "fd:"}},
	"ProtectSystem":       {kind: KindEnum, values: []string{"no", "yes", "full", "strict"}},
	"ProtectHome":         {kind: KindEnum, values: []string{"no", "yes", "read-only", "tmpfs"}},
	"ProtectProc":         {kind: KindEnum, values: []string{"default", "noaccess", "invisible", "ptraceable"}},
	"ProcSubset":          {kind: KindEnum, values: []string{"all", "pid"}},
	"DevicePolicy":        {kind: KindEnum, values: []string{"auto", "closed", "strict"}},
	"CPUSchedulingPolicy": {kind: KindEnum, values: []string{"other", "batch", "idle", "fifo", "rr"}},
	"IOSchedulingClass":   {kind: KindEnum, values: []string{"realtime", "best-effort", "idle"}},
	"ExitType":            {kind: KindEnum, values: []string{"main", "cgroup"}},
	"OOMPolicy":           {kind: KindEnum, values: []string{"continue", "stop", "kill"}},
	"CollectMode":         {kind: KindEnum, values: []string{"inactive", "inactive-or-failed"}},
	"KeyringMode":         {kind: KindEnum, values: []string{"inherit", "private", "shared"}},
	"UtmpMode":            {kind: KindEnum, values: []string{"init", "login", "user"}},
	"BindIPv6Only":        {kind: KindEnum, values: []string{"default", "both", "ipv6-only"}},
	"Timestamping":        {kind: KindEnum, values: []string{"off", "us", "ns"}},
	"SyslogLevel":         {kind: KindEnum, values: []string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"}},
	"LogLevelMax":         {kind: KindEnum, values: []string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"}},
	"JobMode":             {kind: KindEnum, values: []string{"fail", "replace", "replace-irreversibly", "isolate", "flush", "ignore-dependencies", "ignore-requirements"}},
	"OnFailureJobMode":    {kind: KindEnum, values: []string{"fail", "replace", "replace-irreversibly", "isolate", "flush", "ignore-dependencies", "ignore-requirements"}},
	"OnSuccessJobMode":    {kind: KindEnum, values: []string{"fail", "replace", "replace-irreversibly", "isolate", "flush", "ignore-dependencies", "ignore-requirements"}},
	"FailureAction":       {kind: KindEnum, values: []string{"none", "reboot", "reboot-force", "reboot-immediate", "poweroff", "poweroff-force", "poweroff-immediate", "exit", "exit-force", "soft-reboot", "soft-reboot-force", "kexec", "kexec-force", "halt", "halt-force", "halt-immediate"}},
	"SuccessAction":       {kind: KindEnum, values: []string{"none", "reboot", "reboot-force", "reboot-immediate", "poweroff", "poweroff-force", "poweroff-immediate", "exit", "exit-force", "soft-reboot", "soft-reboot-force", "kexec", "kexec-force", "halt", "halt-force", "halt-immediate"}},
	"StartLimitAction":    {kind: KindEnum, values: []string{"none", "reboot", "reboot-force", "reboot-immediate", "poweroff", "poweroff-force", "poweroff-immediate", "exit", "exit-force", "soft-reboot", "soft-reboot-force", "kexec", "kexec-force", "halt", "halt-force", "halt-immediate"}},
	"OnCalendar":          {kind: KindString, multi: &yes},
	"Environment":         {kind: KindString, multi: &yes},
	"EnvironmentFile":     {kind: KindPath, multi: &yes},
	"Description":         {kind: KindString, multi: &no},
	"Documentation":       {kind: KindString, multi: &yes},
	"User":                {kind: KindString},
	"Group":               {kind: KindString},
	"WorkingDirectory":    {kind: KindPath},
	"ListenStream":        {kind: KindString, multi: &yes},
	"ListenDatagram":      {kind: KindString, multi: &yes},
	"What":                {kind: KindString},
	"Where":               {kind: KindPath},
	"Options":             {kind: KindString},
	"MemoryMax":           {kind: KindSize},
	"MemoryHigh":          {kind: KindSize},
	"MemoryLow":           {kind: KindSize},
	"MemoryMin":           {kind: KindSize},
	"MemorySwapMax":       {kind: KindSize},
	"CPUQuota":            {kind: KindString},
}

func applyOverride(d *Directive) {
	if unitLists[d.Name] {
		d.Kind, d.Values, d.Multi = KindUnits, nil, true
	}
	o, ok := overrides[d.Name]
	if !ok {
		return
	}
	// Type= in [Mount] and [Swap]-ish pages is a file system type, not the
	// service type.
	if d.Name == "Type" && d.Page != "systemd.service" {
		d.Kind, d.Values = KindString, nil
		return
	}
	if o.kind != "" {
		d.Kind = o.kind
		d.Values = o.values
	}
	if o.multi != nil {
		d.Multi = *o.multi
	}
}
