# Cross-Platform Run Mode & Per-Platform Field Overrides — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `mode = "run"` to Linux and macOS, add per-platform `<field>_<os>` overrides so one config targets many platforms, and add a cross-platform `{dst.HOME}` marker.

**Architecture:** Per-platform resolution collapses `<field>_<os>` overrides into effective values once per target OS inside `BuildForPlatform` (via a new `Config.resolveFor`), so all existing generators stay single-platform and unchanged. A new `installer.GenerateShellRunner` emits a lean run-mode `.sh` (Linux: stdout only; macOS: plus an `osascript` results dialog). `{dst.HOME}` substitutes to the literal `$HOME` token at build time on Unix and via the known-folder renderer on Windows.

**Tech Stack:** Go 1.26, module `onepkg`, TOML lib `github.com/pelletier/go-toml` v1.9.5 (struct tags like `toml:"exec_linux"` map directly to flat keys). No new dependencies.

## Global Constraints

- **TDD:** write the failing test first, run it, implement, run it, commit — every task.
- **No new external dependencies.** Only the stdlib and `github.com/pelletier/go-toml`.
- **Package layout:** `cmd/onepkg/*.go` is `package main`; `cmd/onepkg/installer/*.go` is `package installer`.
- **Stubs prerequisite:** Windows build embeds precompiled stubs. Before running any `go test`/`go build`, run `make win-wrapper` once (or `make build`) so `installer/stubs/*.exe` exist (the `//go:embed` directives fail otherwise). Linux/macOS-only test runs still need this because the `installer` package embeds stubs unconditionally.
- **Resolution rule (normative):** for target OS family `g` (`linux`, `darwin`, `windows`), a field's effective value is `<field>_<g>` if non-empty, else `<field>`. `_<os>` is keyed by OS family, never arch.
- **Command type is fixed by the base field.** A command's base `exec` xor base `shell` must be set (existing `validateRun` rule, unchanged). `_<os>` overrides *refine the same-typed value* per platform (e.g. `exec_windows`, `shell_linux`); they do not flip a command from exec-type to shell-type per platform. This keeps every existing run-mode validation test passing.
- **`{dst.HOME}` is the only cross-platform `{dst.*}` marker.** All other `{dst.NAME}` remain Windows-only and are rejected on non-Windows resolved paths.
- **`$HOME`-under-root caveat:** install runs as root, so `$HOME=/root`. Per-user paths belong in run mode. Documented, not gated.
- **Commit message style:** lowercase conventional prefix, e.g. `feat(run): ...`. End commit bodies with `Co-Authored-By: Claude <noreply@anthropic.com>`.
- **Generated shell uses Go `%q` for bash double-quoted string literals** (matches `shell.go`'s existing convention, e.g. `PKG_NAME=%q`). Run-mode `shell` commands should be one-liners (`&&`/`;` joined); multi-line shell commands are out of scope.

---

## Task 1: CommandConfig per-platform override fields + `resolveFor`

**Files:**
- Modify: `cmd/onepkg/config.go:30-36` (the `CommandConfig` struct)
- Test: `cmd/onepkg/config_test.go` (append a new test)

**Interfaces:**
- Produces: `func (c CommandConfig) resolveFor(goos string) CommandConfig` — returns a command whose `Exec`, `Args`, `Shell` hold the effective values for `goos` (override if set, else base), and whose `ExecLinux/.../ShellWindows` override fields are zeroed. Later tasks (`Config.resolveFor` in Task 2; `BuildForUnixRun` in Task 8) call this.

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/config_test.go`:

```go
func TestCommandConfigResolveFor(t *testing.T) {
	cases := []struct {
		name string
		goos string
		in   CommandConfig
		want CommandConfig
	}{
		{
			name: "linux uses override exec/args, ignores windows",
			goos: "linux",
			in: CommandConfig{
				Exec: "bin/cleanup", Args: []string{"--all"},
				ExecLinux: "bin/cleanup-linux", ArgsLinux: []string{"--linux"},
				ExecWindows: "bin/cleanup.exe", ArgsWindows: []string{"/all"},
			},
			want: CommandConfig{Exec: "bin/cleanup-linux", Args: []string{"--linux"}},
		},
		{
			name: "windows uses override exec/args",
			goos: "windows",
			in: CommandConfig{
				Exec: "bin/cleanup", Args: []string{"--all"},
				ExecWindows: "bin/cleanup.exe", ArgsWindows: []string{"/all"},
			},
			want: CommandConfig{Exec: "bin/cleanup.exe", Args: []string{"/all"}},
		},
		{
			name: "macos falls back to base when no override",
			goos: "darwin",
			in: CommandConfig{
				Shell:       "echo base",
				ShellLinux:  "echo linux",
				ShellMacos:  "", // unset -> base
			},
			want: CommandConfig{Shell: "echo base"},
		},
		{
			name: "empty override slice falls back to base args",
			goos: "linux",
			in: CommandConfig{
				Exec: "bin/x", Args: []string{"--base"},
				ArgsLinux: []string{}, // empty -> base
			},
			want: CommandConfig{Exec: "bin/x", Args: []string{"--base"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.resolveFor(tc.goos)
			if got.Exec != tc.want.Exec {
				t.Errorf("Exec = %q, want %q", got.Exec, tc.want.Exec)
			}
			if got.Shell != tc.want.Shell {
				t.Errorf("Shell = %q, want %q", got.Shell, tc.want.Shell)
			}
			if !equalArgs(got.Args, tc.want.Args) {
				t.Errorf("Args = %v, want %v", got.Args, tc.want.Args)
			}
			// Override fields must be zeroed on the resolved value.
			if got.ExecLinux != "" || got.ExecMacos != "" || got.ExecWindows != "" {
				t.Errorf("exec override fields not zeroed: %+v", got)
			}
			if got.ShellLinux != "" || got.ShellMacos != "" || got.ShellWindows != "" {
				t.Errorf("shell override fields not zeroed: %+v", got)
			}
			if len(got.ArgsLinux) != 0 || len(got.ArgsMacos) != 0 || len(got.ArgsWindows) != 0 {
				t.Errorf("args override fields not zeroed: %+v", got)
			}
		})
	}
}

func equalArgs(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestCommandConfigResolveFor -v`
Expected: FAIL / compile error (`c.resolveFor undefined`, unknown fields).

- [ ] **Step 3: Add the override fields and resolver**

Replace the `CommandConfig` struct in `cmd/onepkg/config.go` (lines 32-36) with:

```go
// CommandConfig is one entry in [[commands]] for mode = "run" packages.
// Exactly one of the base Exec or Shell must be set. Per-OS override fields
// (<field>_<os>) refine the same-typed value for that OS family when set;
// otherwise the base value is used. See resolveFor.
type CommandConfig struct {
	Exec  string   `toml:"exec"`  // bundled file path, resolved against the temp extraction dir; empty if Shell set
	Args  []string `toml:"args"`  // args for Exec
	Shell string   `toml:"shell"` // arbitrary shell expression; empty if Exec set
	// Per-OS overrides (effective value = override if set, else base).
	ExecLinux    string   `toml:"exec_linux"`
	ExecMacos    string   `toml:"exec_macos"`
	ExecWindows  string   `toml:"exec_windows"`
	ArgsLinux    []string `toml:"args_linux"`
	ArgsMacos    []string `toml:"args_macos"`
	ArgsWindows  []string `toml:"args_windows"`
	ShellLinux   string   `toml:"shell_linux"`
	ShellMacos   string   `toml:"shell_macos"`
	ShellWindows string   `toml:"shell_windows"`
}

// resolveFor returns a CommandConfig with the effective Exec/Args/Shell for the
// given target OS family (override if set, else base), and all override fields
// zeroed. A command's type (exec vs shell) is fixed by its base field.
func (c CommandConfig) resolveFor(goos string) CommandConfig {
	pickStr := func(def, linux, macos, windows string) string {
		switch goos {
		case "linux":
			if linux != "" {
				return linux
			}
		case "darwin":
			if macos != "" {
				return macos
			}
		case "windows":
			if windows != "" {
				return windows
			}
		}
		return def
	}
	pickArgs := func(def, linux, macos, windows []string) []string {
		switch goos {
		case "linux":
			if len(linux) > 0 {
				return linux
			}
		case "darwin":
			if len(macos) > 0 {
				return macos
			}
		case "windows":
			if len(windows) > 0 {
				return windows
			}
		}
		return def
	}
	return CommandConfig{
		Exec:  pickStr(c.Exec, c.ExecLinux, c.ExecMacos, c.ExecWindows),
		Args:  pickArgs(c.Args, c.ArgsLinux, c.ArgsMacos, c.ArgsWindows),
		Shell: pickStr(c.Shell, c.ShellLinux, c.ShellMacos, c.ShellWindows),
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestCommandConfigResolveFor -v`
Expected: PASS.

- [ ] **Step 5: Run the full config test suite to confirm no regression**

Run: `cd cmd/onepkg && go test -run 'TestParseConfigRunMode|TestValidateRunMode|TestValidate' -v`
Expected: PASS (override fields are inert until wired up).

- [ ] **Step 6: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "$(cat <<'EOF'
feat(config): add per-OS command override fields + resolveFor

Adds exec_linux/macos/windows, args_*, shell_* override fields to
CommandConfig and a resolveFor(goos) that collapses them to effective
values (override-if-set-else-base), zeroing the override fields on the
result. Command type stays fixed by the base field.

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: DirConfig + OverlayConfig overrides + `Config.resolveFor`

**Files:**
- Modify: `cmd/onepkg/config.go` (`DirConfig` ~line 71, `OverlayConfig` ~line 78)
- Test: `cmd/onepkg/config_test.go`

**Interfaces:**
- Produces: `(d DirConfig) resolveFor(goos) DirConfig`, `(o OverlayConfig) resolveFor(goos) OverlayConfig`, and `(c Config) resolveFor(goos) Config` — the last returns a copy with `Commands`, `Dirs`, `SystemOverlay` each resolved for `goos` (all other fields copied; the caller's `*Config` is NOT mutated). Task 8 calls `cfg.resolveFor(goos)` at the top of `BuildForPlatform`.

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/config_test.go`:

```go
func TestDirConfigResolveFor(t *testing.T) {
	d := DirConfig{
		Path: "/var/lib/x", Owner: "root", Group: "root", Mode: "0755",
		PathMacos: "/Library/Application Support/x",
	}
	got := d.resolveFor("darwin")
	if got.Path != "/Library/Application Support/x" {
		t.Errorf("darwin Path = %q", got.Path)
	}
	if got.Owner != "root" || got.Mode != "0755" {
		t.Errorf("non-path fields must be preserved: %+v", got)
	}
	if got.PathMacos != "" || got.PathLinux != "" {
		t.Errorf("path override fields not zeroed: %+v", got)
	}
	if d.resolveFor("linux").Path != "/var/lib/x" {
		t.Error("linux should fall back to base path")
	}
}

func TestOverlayConfigResolveFor(t *testing.T) {
	o := OverlayConfig{
		System: "/etc/x", Item: "data/x",
		SystemWindows: "{dst.CommonAppDataFolder}\\x",
	}
	got := o.resolveFor("windows")
	if got.System != `{dst.CommonAppDataFolder}\x` {
		t.Errorf("windows System = %q", got.System)
	}
	if got.Item != "data/x" {
		t.Errorf("Item must be preserved: %q", got.Item)
	}
	if o.resolveFor("linux").System != "/etc/x" {
		t.Error("linux should fall back to base system path")
	}
}

func TestConfigResolveForDoesNotMutateOriginal(t *testing.T) {
	cfg := Config{
		Name: "x", Version: "v1",
		Commands: []CommandConfig{{Exec: "bin/a", ExecLinux: "bin/a-linux"}},
		Dirs:     []DirConfig{{Path: "/p", PathMacos: "/pmac"}},
		SystemOverlay: []OverlayConfig{{System: "/s", SystemWindows: "\\swin"}},
	}
	resolved := cfg.resolveFor("darwin")
	// Resolved values for darwin.
	if resolved.Commands[0].Exec != "bin/a" {
		t.Errorf("darwin command Exec = %q, want bin/a", resolved.Commands[0].Exec)
	}
	if resolved.Dirs[0].Path != "/pmac" {
		t.Errorf("darwin dir Path = %q, want /pmac", resolved.Dirs[0].Path)
	}
	if resolved.SystemOverlay[0].System != "/s" {
		t.Errorf("darwin overlay System = %q, want /s", resolved.SystemOverlay[0].System)
	}
	// Original must be untouched.
	if cfg.Commands[0].ExecLinux != "bin/a-linux" {
		t.Error("resolveFor mutated the original cfg.Commands")
	}
	if cfg.Dirs[0].PathMacos != "/pmac" {
		t.Error("resolveFor mutated the original cfg.Dirs")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run 'TestDirConfigResolveFor|TestOverlayConfigResolveFor|TestConfigResolveFor' -v`
Expected: FAIL / compile error (unknown fields and methods).

- [ ] **Step 3: Add the fields and resolvers**

Update `DirConfig` in `cmd/onepkg/config.go`:

```go
type DirConfig struct {
	Path  string `toml:"path"`  // Required
	Owner string `toml:"owner"` // Optional, default: service user or root
	Group string `toml:"group"` // Optional, default: same as owner
	Mode  string `toml:"mode"`  // Optional, default: "0755"
	// Per-OS path overrides (effective = override if set, else Path).
	PathLinux   string `toml:"path_linux"`
	PathMacos   string `toml:"path_macos"`
	PathWindows string `toml:"path_windows"`
}

func (d DirConfig) resolveFor(goos string) DirConfig {
	out := d
	switch goos {
	case "linux":
		if d.PathLinux != "" {
			out.Path = d.PathLinux
		}
	case "darwin":
		if d.PathMacos != "" {
			out.Path = d.PathMacos
		}
	case "windows":
		if d.PathWindows != "" {
			out.Path = d.PathWindows
		}
	}
	out.PathLinux, out.PathMacos, out.PathWindows = "", "", ""
	return out
}
```

Update `OverlayConfig`:

```go
type OverlayConfig struct {
	System string `toml:"system"`
	Item   string `toml:"item"`
	// Per-OS system-path overrides (effective = override if set, else System).
	SystemLinux   string `toml:"system_linux"`
	SystemMacos   string `toml:"system_macos"`
	SystemWindows string `toml:"system_windows"`
}

func (o OverlayConfig) resolveFor(goos string) OverlayConfig {
	out := o
	switch goos {
	case "linux":
		if o.SystemLinux != "" {
			out.System = o.SystemLinux
		}
	case "darwin":
		if o.SystemMacos != "" {
			out.System = o.SystemMacos
		}
	case "windows":
		if o.SystemWindows != "" {
			out.System = o.SystemWindows
		}
	}
	out.SystemLinux, out.SystemMacos, out.SystemWindows = "", "", ""
	return out
}
```

Add `Config.resolveFor` (place it right after the `Config` struct definition, near the other `Config` methods):

```go
// resolveFor returns a copy of c with every per-platform override field
// (commands, dirs, system_overlay) collapsed to its effective value for the
// given target OS family. Base values are used where no override is set. The
// receiver is NOT mutated, so main()'s shared *Config is safe across platforms.
func (c Config) resolveFor(goos string) Config {
	out := c
	out.Commands = make([]CommandConfig, len(c.Commands))
	for i, cmd := range c.Commands {
		out.Commands[i] = cmd.resolveFor(goos)
	}
	out.Dirs = make([]DirConfig, len(c.Dirs))
	for i, d := range c.Dirs {
		out.Dirs[i] = d.resolveFor(goos)
	}
	out.SystemOverlay = make([]OverlayConfig, len(c.SystemOverlay))
	for i, o := range c.SystemOverlay {
		out.SystemOverlay[i] = o.resolveFor(goos)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run 'TestDirConfigResolveFor|TestOverlayConfigResolveFor|TestConfigResolveFor' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "$(cat <<'EOF'
feat(config): add per-OS path overrides + Config.resolveFor

DirConfig gains path_linux/macos/windows, OverlayConfig gains
system_*. Config.resolveFor(goos) collapses all override fields
(commands/dirs/system_overlay) for one target OS without mutating the
receiver, ready for BuildForPlatform to call per platform.

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `{dst.HOME}` cross-platform marker (`knownfolders.go`)

`{dst.HOME}` resolves to the literal token `$HOME` on every platform (`$HOME` is valid in both bash and PowerShell — PowerShell's automatic `$HOME` is the user profile dir). It is distinct from the Windows-only `KnownFolders` map; no definition line is emitted for it.

**Files:**
- Modify: `cmd/onepkg/installer/knownfolders.go`
- Test: `cmd/onepkg/installer/knownfolders_test.go`

**Interfaces:**
- Produces: a new `HasWindowsOnlyKnownFolderMarker(s string) bool` (true only for Windows-only `{dst.NAME}`, excluding `HOME`) — Task 8's build guard uses this so `{dst.HOME}` is permitted on non-Windows.
- Modifies: `ValidateKnownFolderMarkers`, `HasKnownFolderMarker`, `KnownFolderMarkers`, `EmitKnownFolderDefs`, `EmitPathExpr`, `ReplaceMarkers` to treat `HOME` as a known, renderable-as-`$HOME` marker.

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/installer/knownfolders_test.go`:

```go
func TestHomeMarkerIsKnown(t *testing.T) {
	if err := ValidateKnownFolderMarkers("{dst.HOME}/x"); err != nil {
		t.Errorf("{dst.HOME} should be valid: %v", err)
	}
	if !HasKnownFolderMarker("{dst.HOME}/x") {
		t.Error("HasKnownFolderMarker should detect {dst.HOME}")
	}
}

func TestHomeMarkerRender(t *testing.T) {
	// Path expression: {dst.HOME} -> $HOME (no concatenation def needed).
	if got := EmitPathExpr("{dst.HOME}/.config/x"); got != "$HOME + '/.config/x'" {
		t.Errorf("EmitPathExpr(HOME) = %q, want $HOME + '/.config/x'", got)
	}
	if got := EmitPathExpr("{dst.HOME}"); got != "$HOME" {
		t.Errorf("EmitPathExpr(HOME only) = %q, want $HOME", got)
	}
	// Inline replace for run shell.
	if got := ReplaceMarkers("mkdir -p {dst.HOME}/x"); got != "mkdir -p $HOME/x" {
		t.Errorf("ReplaceMarkers(HOME) = %q, want mkdir -p $HOME/x", got)
	}
	// No definition line emitted for HOME.
	if out := EmitKnownFolderDefs([]string{"HOME", "AppDataFolder"}); strings.Contains(out, "$HOME =") {
		t.Errorf("EmitKnownFolderDefs must not define $HOME; got:\n%s", out)
	}
	if !strings.Contains(out, "$AppDataFolder = [Environment]::GetFolderPath") {
		t.Errorf("AppDataFolder def still emitted; got:\n%s", out)
	}
}

func TestKnownFolderMarkersIncludesHome(t *testing.T) {
	got := KnownFolderMarkers("{dst.HOME}/x{dst.AppDataFolder}/y")
	want := []string{"AppDataFolder", "HOME"} // deduped + sorted
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("KnownFolderMarkers = %v, want %v", got, want)
	}
}

func TestHasWindowsOnlyKnownFolderMarker(t *testing.T) {
	// Windows-only marker present -> true.
	if !HasWindowsOnlyKnownFolderMarker("{dst.AppDataFolder}\\x") {
		t.Error("Windows-only marker should be detected")
	}
	// HOME-only -> false (HOME is cross-platform, allowed on non-Windows).
	if HasWindowsOnlyKnownFolderMarker("{dst.HOME}/x") {
		t.Error("HOME is not Windows-only")
	}
	// Mixed HOME + Windows-only -> true (a Windows-only marker is present).
	if !HasWindowsOnlyKnownFolderMarker("{dst.HOME}/x/{dst.AppDataFolder}") {
		t.Error("mixed string has a Windows-only marker -> true")
	}
	// Plain -> false.
	if HasWindowsOnlyKnownFolderMarker("/etc/x") {
		t.Error("plain path has no marker")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg/installer && go test -run 'TestHomeMarker|TestHasWindowsOnly|TestKnownFolderMarkersIncludesHome' -v`
Expected: FAIL (`{dst.HOME}` rejected as unknown; `HasWindowsOnlyKnownFolderMarker` undefined).

- [ ] **Step 3: Implement HOME support**

In `cmd/onepkg/installer/knownfolders.go`, add a cross-platform set and name-check helper near the `KnownFolders` var:

```go
// crossPlatformFolders are {dst.NAME} markers that resolve on every target OS,
// not just Windows. HOME resolves to the target user's home directory via the
// $HOME variable (bash env var / PowerShell automatic variable), so no runtime
// definition line is emitted for it.
var crossPlatformFolders = map[string]bool{"HOME": true}

// isKnownFolderName reports whether name is a resolvable {dst.NAME} marker name
// (a Windows-only known folder, or a cross-platform marker like HOME).
func isKnownFolderName(name string) bool {
	_, ok := KnownFolders[name]
	return ok || crossPlatformFolders[name]
}

// isWindowsOnlyFolderName reports whether name resolves only on Windows.
func isWindowsOnlyFolderName(name string) bool {
	_, ok := KnownFolders[name]
	return ok
}
```

Update `ValidateKnownFolderMarkers` (change the membership test):

```go
func ValidateKnownFolderMarkers(s string) error {
	for _, m := range markerRe.FindAllStringSubmatch(s, -1) {
		if !isKnownFolderName(m[1]) {
			return fmt.Errorf("unknown Windows known-folder variable {dst.%s}; supported: %s", m[1], knownFolderNameList())
		}
	}
	return nil
}
```

Update `KnownFolderMarkers` (accept HOME):

```go
func KnownFolderMarkers(s string) []string {
	var names []string
	for _, m := range markerRe.FindAllStringSubmatch(s, -1) {
		if isKnownFolderName(m[1]) {
			names = append(names, m[1])
		}
	}
	return dedupeSorted(names)
}
```

Update `EmitKnownFolderDefs` to skip HOME:

```go
func EmitKnownFolderDefs(names []string) string {
	var b strings.Builder
	for _, n := range dedupeSorted(names) {
		if n == "HOME" {
			continue // $HOME is a shell/PowerShell built-in; no definition needed.
		}
		fmt.Fprintf(&b, "$%s = [Environment]::GetFolderPath([Environment+SpecialFolder]::%s)\n", n, KnownFolders[n])
	}
	return b.String()
}
```

Update `EmitPathExpr` so HOME renders as `$HOME` (the unknown-name branch must not catch it). In the loop body, replace the known-folder check:

```go
		name := s[nameStart:nameEnd]
		if name == "HOME" {
			parts = append(parts, "$HOME")
		} else if _, ok := KnownFolders[name]; ok {
			parts = append(parts, "$"+name)
		} else {
			parts = append(parts, psQuote(s[start:end]))
		}
```

Update `ReplaceMarkers` similarly:

```go
func ReplaceMarkers(s string) string {
	return markerRe.ReplaceAllStringFunc(s, func(tok string) string {
		m := markerRe.FindStringSubmatch(tok)
		if m[1] == "HOME" {
			return "$HOME"
		}
		if _, ok := KnownFolders[m[1]]; ok {
			return "$" + m[1]
		}
		return tok
	})
}
```

Add the new guard helper:

```go
// HasWindowsOnlyKnownFolderMarker reports whether s contains any {dst.NAME}
// marker that resolves only on Windows (i.e. a KnownFolders entry, excluding
// cross-platform markers like HOME). The non-Windows build guard uses this so
// {dst.HOME} is permitted on Linux/macOS while {dst.AppDataFolder} is not.
func HasWindowsOnlyKnownFolderMarker(s string) bool {
	for _, m := range markerRe.FindAllStringSubmatch(s, -1) {
		if isWindowsOnlyFolderName(m[1]) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the marker tests to verify they pass**

Run: `cd cmd/onepkg/installer && go test -run 'TestHomeMarker|TestHasWindowsOnly|TestKnownFolderMarkersIncludesHome|TestHasKnownFolderMarker|TestValidateKnownFolderMarkers|TestEmitPathExpr|TestEmitKnownFolderDefs|TestKnownFolderMarkersAndReplaceMarkers' -v`
Expected: PASS (new tests pass; existing marker tests still pass — `{dst.AppData}` unknown still rejected; `{dst.HOME}` now accepted).

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/installer/knownfolders.go cmd/onepkg/installer/knownfolders_test.go
git commit -m "$(cat <<'EOF'
feat(markers): add cross-platform {dst.HOME} marker

{dst.HOME} resolves to the $HOME token on all platforms (bash env var /
PowerShell automatic var), so no definition line is emitted. Other
{dst.NAME} stay Windows-only. Adds HasWindowsOnlyKnownFolderMarker for
the build guard to permit HOME on non-Windows.

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Stray-marker allow-list covers `_<os>` fields (`markers.go`)

`{dst.*}` markers are only valid in `dirs[].path`, `system_overlay[].system`, run `commands[].args`, run `commands[].shell` — and now their `_<os>` override variants. The reflection walker in `markers.go` reports field paths like `Commands[0].ShellLinux`, `Dirs[0].PathMacos`, `Commands[0].ArgsWindows[0]`. The allow-list regex must match these.

**Files:**
- Modify: `cmd/onepkg/markers.go:107-118` (`dstSupportedFieldRe`, `isDstSupportedField`)
- Test: `cmd/onepkg/config_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/config_test.go`:

```go
func TestValidateAllowsDstMarkerInOverrideFields(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "cleanup"), []byte("x"), 0755)

	full := `
name = "x"
version = "v1"
mode = "run"
items = ["bin/cleanup"]

[[commands]]
exec = "bin/cleanup"
shell_linux = "mkdir -p {dst.HOME}/.config/x"
args_windows = ["{dst.AppDataFolder}\\cfg"]
`
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(full), 0644)
	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(dir); err != nil {
		t.Fatalf("{dst.*} in override fields should be allowed: %v", err)
	}
}

func TestValidateRejectsDstMarkerInOverrideExec(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "cleanup"), []byte("x"), 0755)

	full := `
name = "x"
version = "v1"
mode = "run"
items = ["bin/cleanup"]

[[commands]]
exec = "bin/cleanup"
exec_windows = "{dst.AppDataFolder}\\cleanup.exe"
`
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(full), 0644)
	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(dir); err == nil {
		t.Fatal("{dst.*} in exec_windows should be rejected (exec is not a supported field)")
	}
}

func TestValidateAllowsDstMarkerInDirPathOverride(t *testing.T) {
	dir := t.TempDir()
	full := `
name = "x"
version = "v1"
items = ["bin/x"]

[[dirs]]
path = "/var/lib/x"
path_macos = "{dst.HOME}/.config/x"
mode = "0755"
`
	// bin/x must exist for install-mode item validation.
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "x"), []byte("x"), 0755)
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(full), 0644)
	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(dir); err != nil {
		t.Fatalf("{dst.HOME} in path_macos should be allowed: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run 'TestValidateAllowsDstMarkerInOverrideFields|TestValidateRejectsDstMarkerInOverrideExec|TestValidateAllowsDstMarkerInDirPathOverride' -v`
Expected: FAIL (override fields with markers are rejected as stray, because the regex doesn't match `ShellLinux`/`PathMacos`/etc.).

- [ ] **Step 3: Update the allow-list regex**

In `cmd/onepkg/markers.go`, replace the `dstSupportedFieldRe` definition and its comment (lines 111-118) with:

```go
// dstSupportedFieldRe matches the dotted field paths of the field types that
// resolve {dst.NAME} at install/run time: dirs[].path, system_overlay[].system,
// and run-mode commands[].args / commands[].shell — including their per-OS
// override variants (PathLinux, ShellMacos, ArgsWindows[i], ...). The reflection
// walker reports slice elements as Name[i], so args variants are Args<OS>[i].
var dstSupportedFieldRe = regexp.MustCompile(`^(Dirs\[\d+\]\.(Path|PathLinux|PathMacos|PathWindows)|SystemOverlay\[\d+\]\.(System|SystemLinux|SystemMacos|SystemWindows)|Commands\[\d+\]\.(Args|ArgsLinux|ArgsMacos|ArgsWindows)\[\d+\]|Commands\[\d+\]\.(Shell|ShellLinux|ShellMacos|ShellWindows))$`)

func isDstSupportedField(path string) bool {
	return dstSupportedFieldRe.MatchString(path)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run 'TestValidateAllowsDstMarkerInOverrideFields|TestValidateRejectsDstMarkerInOverrideExec|TestValidateAllowsDstMarkerInDirPathOverride' -v`
Expected: PASS.

- [ ] **Step 5: Run the full stray-marker + run-mode suites (no regression)**

Run: `cd cmd/onepkg && go test -run 'Stray|RunMode|Validate' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/onepkg/markers.go cmd/onepkg/config_test.go
git commit -m "$(cat <<'EOF'
feat(markers): allow {dst.*} in per-OS override fields

Extends the stray-marker allow-list regex to the _<os> variants of
dirs.path, system_overlay.system, and run commands.args/shell, so
{dst.HOME} and Windows known-folder markers resolve there. exec
overrides stay rejected.

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `execTargetPaths` is aware of windows override execs

The Windows `.exe` auto-extension pass (`applyExeExtension`) keys off the exec strings returned by `execTargetPaths`. With `exec_windows` overrides, a command whose base `exec` is empty but `exec_windows` is set must still get `.exe` on a Windows build.

**Files:**
- Modify: `cmd/onepkg/packager.go:65-76` (`execTargetPaths`)
- Test: `cmd/onepkg/packager_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/packager_test.go`:

```go
func TestExecTargetPathsIncludesWindowsOverride(t *testing.T) {
	cfg := &Config{
		Commands: []CommandConfig{
			{Exec: "bin/a", ExecWindows: "bin/a-win"},
			{Shell: "echo hi"}, // not an exec target
			{ExecLinux: "bin/b-linux", ExecWindows: "bin/b-win"}, // base exec empty
		},
	}
	got := execTargetPaths(cfg)
	want := map[string]bool{"bin/a-win": true, "bin/b-win": true}
	for k := range want {
		if !got[k] {
			t.Errorf("execTargetPaths missing windows override %q; got %v", k, got)
		}
	}
	// Base-only exec (no windows override) still included via fallback.
	cfg2 := &Config{Commands: []CommandConfig{{Exec: "bin/c"}}}
	if !execTargetPaths(cfg2)["bin/c"] {
		t.Error("base exec with no windows override should be included")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestExecTargetPathsIncludesWindowsOverride -v`
Expected: FAIL (`bin/a-win` / `bin/b-win` not collected — current code only reads `c.Exec`).

- [ ] **Step 3: Update `execTargetPaths`**

Replace `execTargetPaths` in `cmd/onepkg/packager.go` (lines 65-76) with:

```go
// execTargetPaths returns the set of config-form exec references that name
// bundled items: every non-empty commands[].exec (resolved for Windows, since
// the .exe pass is Windows-only) plus the single service exec. The keys are
// identical to an item's stripped pack path (e.g. "bin/onepkg"), so
// applyExeExtension can match items to execs directly.
func execTargetPaths(cfg *Config) map[string]bool {
	targets := make(map[string]bool)
	for _, c := range cfg.Commands {
		// The .exe extension pass only runs on Windows builds, so resolve the
		// exec for Windows: exec_windows if set, else the base exec.
		exec := c.ExecWindows
		if exec == "" {
			exec = c.Exec
		}
		if exec != "" {
			targets[exec] = true
		}
	}
	if len(cfg.Services) > 0 && cfg.Services[0].Exec != "" {
		targets[cfg.Services[0].Exec] = true
	}
	return targets
}
```

- [ ] **Step 4: Run test to verify it passes, and the existing exe-extension suite for regression**

Run: `cd cmd/onepkg && go test -run 'TestExecTargetPathsIncludesWindowsOverride|TestWindowsRunAutoExeExtension|TestWindowsRunNonExecItemUnchanged|TestWindowsInstallAutoExeExtension' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/packager.go cmd/onepkg/packager_test.go
git commit -m "$(cat <<'EOF'
fix(packager): include exec_windows overrides in exe-extension targets

execTargetPaths now resolves each command's exec for Windows (exec_windows
if set, else base) so an extensionless {platform} binary referenced only via
exec_windows still gets .exe appended on Windows builds.

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `GenerateShellRunner` core (Linux run-mode `.sh`, no dialog) + factor `shellCheckArch`

New lean run-mode `.sh` generator. First the Linux form (stdout results only, no dialog). Also extract the shared `check_arch` snippet so the install `.sh` and run `.sh` cannot drift.

**Files:**
- Create: `cmd/onepkg/installer/shell_runner.go`
- Create: `cmd/onepkg/installer/shell_runner_test.go`
- Modify: `cmd/onepkg/installer/shell.go` (factor `check_arch` into `shellCheckArch()`; install output must stay byte-identical)

**Interfaces:**
- Produces: `type ShellRunnerConfig struct {...}` and `func GenerateShellRunner(cfg ShellRunnerConfig) (string, error)`. `ShellRunnerConfig` reuses the existing `CommandSpec` (already in `powershell_runner.go`). Field `Goos string` controls the macOS dialog (added in Task 7; for now only `linux` is exercised). Task 8's `BuildForUnixRun` calls this.
- Produces: `func shellCheckArch() string` (shared bash snippet), used by `GenerateShellInstaller` and `GenerateShellRunner`.

- [ ] **Step 1: Factor `check_arch` out of `shell.go`**

In `cmd/onepkg/installer/shell.go`, add a new exported-from-package-internal helper near the top of the file (after the `OverlaySpec` struct, before `GenerateShellInstaller`):

```go
// shellCheckArch returns the bash check_arch function used by both the install
// and run self-extracting scripts. EXPECTED_ARCH is set by the caller before
// the function is invoked.
func shellCheckArch() string {
	return `check_arch() {
    local arch=$(uname -m)
    if [ "$arch" != "$EXPECTED_ARCH" ]; then
        echo "Error: This package is for $EXPECTED_ARCH, current system is $arch"
        exit 1
    fi
}

`
}
```

Then in the large raw-string blob passed to `b.WriteString(\`...\`)` (the block starting with `compute_sha1() {` around line 73), remove the `check_arch() { ... }` block (lines 81-87 in the blob) so the blob contains `compute_sha1() {...}` immediately followed by `check_root() {...}`. Insert `b.WriteString(shellCheckArch())` between the `compute_sha1` blob and the `check_root` blob. Concretely, the code currently is one `b.WriteString(\`compute_sha1()...check_arch()...check_root()...install_service()...\`)`; split it into:

```go
	b.WriteString(`compute_sha1() {
    if command -v sha1sum &>/dev/null; then
        sha1sum "$1" | cut -d' ' -f1
    else
        shasum -a 1 "$1" | cut -d' ' -f1
    fi
}

`)
	b.WriteString(shellCheckArch())
	b.WriteString(`check_root() {
    if [ "$(id -u)" != "0" ]; then
        echo "Error: This operation requires root privileges (use sudo)"
        exit 1
    fi
}
...rest unchanged...
`)
```

The `check_arch` text emitted is byte-identical to before, so `TestGenerateShellInstaller` (which checks the `check_arch` substring) still passes and install `.sh` behavior is unchanged.

- [ ] **Step 2: Write the failing test for the Linux runner**

Create `cmd/onepkg/installer/shell_runner_test.go`:

```go
package installer

import (
	"strings"
	"testing"
)

func TestGenerateShellRunnerLinux(t *testing.T) {
	cfg := ShellRunnerConfig{
		Name:         "cleanup",
		Version:      "v1.0.0",
		Platform:     "linux-arm64",
		ExpectedArch: "aarch64",
		Goos:         "linux",
		HasPayload:   true,
		Commands: []CommandSpec{
			{Exec: "bin/cleanup", Args: []string{"--all", "--force"}},
			{Shell: "mkdir -p \"$HOME/.local/share/cleanup\""},
		},
	}
	out, err := GenerateShellRunner(cfg)
	if err != nil {
		t.Fatalf("GenerateShellRunner: %v", err)
	}
	mustContain := []string{
		"#!/bin/bash",
		"set -uo pipefail", // run-all: no -e
		`PKG_NAME="cleanup"`,
		`EXPECTED_ARCH="aarch64"`,
		"check_arch()",
		"trap 'rm -rf \"$PKG_TMP\"' EXIT",
		"base64 -d | tar xzf",   // HasPayload = true
		`echo Running: "bin/cleanup" "--all" "--force"`,
		`"$PKG_TMP/bin/cleanup" "--all" "--force"; __rc=$?`,
		`results+=("[OK]  $__label")`,
		`results+=("[FAIL] $__label (exit $__rc)")`,
		`failures=$((failures+1))`,
		`eval "mkdir -p \"$HOME/.local/share/cleanup\""; __rc=$?`,
		`$(( ${#results[@]} - failures )) of ${#results[@]} commands succeeded.`,
		`if [ "$failures" -gt 0 ]; then`,
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q", s)
		}
	}
	// Linux: no dialog.
	for _, bad := range []string{"osascript"} {
		if strings.Contains(out, bad) {
			t.Errorf("linux output should not contain %q", bad)
		}
	}
	// Must not carry install-only machinery.
	for _, bad := range []string{"cmd_install", "cmd_uninstall", "cmd_verify", "check_root", "install_service"} {
		if strings.Contains(out, bad) {
			t.Errorf("runner output should not contain install-only %q", bad)
		}
	}
}

func TestGenerateShellRunnerLinuxNoPayload(t *testing.T) {
	cfg := ShellRunnerConfig{
		Name: "shellonly", Version: "v1", Platform: "linux-amd64",
		ExpectedArch: "x86_64", Goos: "linux", HasPayload: false,
		Commands: []CommandSpec{{Shell: "echo hi"}},
	}
	out, err := GenerateShellRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "base64 -d | tar xzf") {
		t.Error("shell-only runner should not emit payload extraction")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd cmd/onepkg/installer && go test -run 'TestGenerateShellRunnerLinux' -v`
Expected: FAIL / compile error (`ShellRunnerConfig` and `GenerateShellRunner` undefined).

- [ ] **Step 4: Implement `GenerateShellRunner` (Linux form)**

Create `cmd/onepkg/installer/shell_runner.go`:

```go
package installer

import (
	"fmt"
	"strings"
)

// ShellRunnerConfig configures the generated run-mode .sh script (Linux/macOS).
type ShellRunnerConfig struct {
	Name          string
	Version       string
	Platform      string
	ExpectedArch  string
	Goos          string // "linux" or "darwin" — selects the macOS results dialog
	HasPayload    bool   // emit payload extraction iff true
	PayloadBase64 string // base64 tar.gz payload (empty for shell-only packages)
	Commands      []CommandSpec
}

// GenerateShellRunner produces a lean run-mode .sh: extract payload to a temp
// dir (optional), run the commands once in order (run-all, continue past
// failures), print [OK]/[FAIL] per command, and on macOS pop an osascript
// results dialog. It reuses the shared check_arch snippet.
func GenerateShellRunner(cfg ShellRunnerConfig) (string, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "#!/bin/bash\n")
	fmt.Fprintf(&b, "# %s %s run-commands package for %s\n", cfg.Name, cfg.Version, cfg.Platform)
	b.WriteString("# Generated by onepkg\n")
	b.WriteString("set -uo pipefail\n\n") // no -e: run-all continues past failures

	fmt.Fprintf(&b, "PKG_NAME=%q\n", cfg.Name)
	fmt.Fprintf(&b, "PKG_VERSION=%q\n", cfg.Version)
	fmt.Fprintf(&b, "PKG_PLATFORM=%q\n", cfg.Platform)
	fmt.Fprintf(&b, "EXPECTED_ARCH=%q\n", cfg.ExpectedArch)
	b.WriteString("\n")

	b.WriteString(shellCheckArch())

	b.WriteString("PAYLOAD='")
	b.WriteString(cfg.PayloadBase64)
	b.WriteString("'\n\n")

	b.WriteString("PKG_TMP=$(mktemp -d)\n")
	b.WriteString("trap 'rm -rf \"$PKG_TMP\"' EXIT\n\n")
	b.WriteString("check_arch\n")
	if cfg.HasPayload {
		b.WriteString("if [ -n \"$PAYLOAD\" ]; then\n")
		b.WriteString("    printf '%s' \"$PAYLOAD\" | base64 -d | tar xzf - -C \"$PKG_TMP\"\n")
		b.WriteString("fi\n")
	}
	b.WriteString("cd \"$PKG_TMP\" || exit 1\n\n")

	b.WriteString("failures=0\n")
	b.WriteString("results=()\n\n")
	for _, cmd := range cfg.Commands {
		b.WriteString(runnerShellCommand(cmd))
	}

	b.WriteString("echo \"\"\n")
	b.WriteString("for r in \"${results[@]}\"; do\n")
	b.WriteString("    echo \"$r\"\n")
	b.WriteString("done\n")
	b.WriteString("echo \"$(( ${#results[@]} - failures )) of ${#results[@]} commands succeeded.\"\n")

	if cfg.Goos == "darwin" {
		b.WriteString(shellResultsDialog(cfg.Name, cfg.Version))
	}

	b.WriteString("\nif [ \"$failures\" -gt 0 ]; then\n    exit 1\nfi\nexit 0\n")
	return b.String(), nil
}

// runnerShellCommand renders one command's log line, invocation, and result
// recording. Execution is run-all: a failure is recorded and bumps failures
// instead of aborting. The exit code is captured explicitly (set -e is off).
func runnerShellCommand(cmd CommandSpec) string {
	var b strings.Builder
	if cmd.Exec != "" {
		label := cmd.Exec
		for _, a := range cmd.Args {
			label += " " + a
		}
		// Log line quotes each piece (mirrors the invocation's token quoting).
		fmt.Fprintf(&b, "echo Running: %q", cmd.Exec)
		for _, a := range cmd.Args {
			fmt.Fprintf(&b, " %q", a)
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "\"$PKG_TMP/%s\"", cmd.Exec)
		for _, a := range cmd.Args {
			fmt.Fprintf(&b, " %q", a)
		}
		b.WriteString("; __rc=$?\n")
		fmt.Fprintf(&b, "__label=%q\n", label)
	} else {
		fmt.Fprintf(&b, "echo Running: %q\n", cmd.Shell)
		fmt.Fprintf(&b, "eval %q; __rc=$?\n", cmd.Shell)
		fmt.Fprintf(&b, "__label=%q\n", cmd.Shell)
	}
	b.WriteString("if [ \"$__rc\" -eq 0 ]; then\n")
	b.WriteString("    results+=(\"[OK]  $__label\")\n")
	b.WriteString("else\n")
	b.WriteString("    results+=(\"[FAIL] $__label (exit $__rc)\")\n")
	b.WriteString("    failures=$((failures+1))\n")
	b.WriteString("fi\n\n")
	return b.String()
}

// shellResultsDialog emits the macOS osascript results dialog (Task 7). Placeholder
// until Task 7 replaces it; linux builds never reach this branch.
func shellResultsDialog(name, version string) string {
	_ = name
	_ = version
	return ""
}
```

- [ ] **Step 5: Run the runner test to verify it passes, and the install test for byte-identity**

Run: `cd cmd/onepkg/installer && go test -run 'TestGenerateShellRunnerLinux|TestGenerateShellInstaller' -v`
Expected: PASS (runner emits the expected linux script; installer still emits `check_arch`).

- [ ] **Step 6: Commit**

```bash
git add cmd/onepkg/installer/shell_runner.go cmd/onepkg/installer/shell_runner_test.go cmd/onepkg/installer/shell.go
git commit -m "$(cat <<'EOF'
feat(installer): add run-mode .sh generator (linux) + factor check_arch

GenerateShellRunner emits a lean run-mode .sh: temp-dir extract, run-all
commands with [OK]/[FAIL] recording, exit by overall result. Linux form
has no dialog. check_arch is extracted into shellCheckArch() shared by
the install and run scripts.

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: macOS results dialog (`osascript`) in `GenerateShellRunner`

Add the `osascript` `display dialog` block, emitted only when `Goos == "darwin"`. Results are joined with AppleScript `& return &` so each command is one line.

**Files:**
- Modify: `cmd/onepkg/installer/shell_runner.go` (`shellResultsDialog`)
- Test: `cmd/onepkg/installer/shell_runner_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/installer/shell_runner_test.go`:

```go
func TestGenerateShellRunnerDarwinDialog(t *testing.T) {
	cfg := ShellRunnerConfig{
		Name: "cleanup", Version: "v1.0.0", Platform: "darwin-arm64",
		ExpectedArch: "arm64", Goos: "darwin", HasPayload: false,
		Commands: []CommandSpec{{Shell: "echo hi"}},
	}
	out, err := GenerateShellRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mustContain := []string{
		"osascript",                      // dialog invocation
		"display dialog",
		"ONEPKG_DIALOG_MSG",              // message passed via env var (never embedded in quotes)
		`with title "cleanup v1.0.0"`,    // title
		`buttons {"OK"} default button "OK" with icon note`,
		`|| true`,                        // dialog failure must not fail the script
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("darwin output missing %q", s)
		}
	}
}

func TestGenerateShellRunnerDarwinNotOnLinux(t *testing.T) {
	linux, _ := GenerateShellRunner(ShellRunnerConfig{
		Name: "x", Version: "v1", Platform: "linux-amd64",
		ExpectedArch: "x86_64", Goos: "linux", Commands: []CommandSpec{{Shell: "echo"}},
	})
	if strings.Contains(linux, "osascript") {
		t.Error("linux runner must not contain osascript")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg/installer && go test -run 'TestGenerateShellRunnerDarwin' -v`
Expected: FAIL (`shellResultsDialog` returns `""`, so no `osascript`).

- [ ] **Step 3: Implement the dialog**

Replace the placeholder `shellResultsDialog` in `cmd/onepkg/installer/shell_runner.go` with the env-var approach below. Passing the message via an environment variable means its contents (spaces, quotes, `$`, newlines) never have to be embedded inside a bash- or AppleScript-quoted string — only the safe package title is interpolated.

```go
// shellResultsDialog emits a macOS osascript results dialog. The results array
// is flattened to a newline-joined message and handed to osascript via an
// environment variable, which osascript reads back with do shell script — this
// avoids embedding arbitrary message text inside bash/AppleScript string
// literals. "|| true" keeps a dismissed/failed dialog from changing the exit
// code (decided by $failures).
func shellResultsDialog(name, version string) string {
	title := fmt.Sprintf("%q", name+" "+version) // bash double-quoted title
	return `
# Show a results dialog (macOS). Message goes through an env var so its
# contents never need embedding in a bash/AppleScript quoted string.
__msg=$(printf "%s\n" "${results[@]}")
ONEPKG_DIALOG_MSG="$__msg" osascript \
  -e 'set _m to do shell script "printf \"%s\" \"$ONEPKG_DIALOG_MSG\""' \
  -e 'display dialog _m with title ` + title + ` buttons {"OK"} default button "OK" with icon note' >/dev/null 2>&1 || true
`
}
```

(The raw string keeps every backslash and quote literal; only `title` is interpolated. `printf "%s\n"` is intentional — bash `printf` expands `\n`, joining each result with a real newline.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg/installer && go test -run 'TestGenerateShellRunnerDarwin|TestGenerateShellRunnerLinux' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/installer/shell_runner.go cmd/onepkg/installer/shell_runner_test.go
git commit -m "$(cat <<'EOF'
feat(installer): add osascript results dialog for macOS run-mode

darwin run packages end with an osascript display-dialog summarizing
per-command results (joined with AppleScript return); linux omits it.
The dialog is best-effort (|| true) so it never changes the exit code.

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: `BuildForUnixRun`, routing, guard refinement, HOME substitution (`main.go`)

Wire run-mode into Linux/macOS: lift the Windows-only guard, resolve the config per OS, substitute `{dst.HOME}` → `$HOME` for Unix, route to `BuildForUnixRun`, and refine the known-folder build guard to the resolved path while permitting `{dst.HOME}`.

**Files:**
- Modify: `cmd/onepkg/main.go` (`BuildForPlatform` ~line 200, the guard block ~208-225, add `BuildForUnixRun`)
- Test: `cmd/onepkg/main_test.go`

**Interfaces:**
- Consumes: `Config.resolveFor` (Task 2), `installer.GenerateShellRunner` + `ShellRunnerConfig` (Tasks 6-7), `installer.HasWindowsOnlyKnownFolderMarker` (Task 3), `installer.CommandSpec` (existing).
- Produces: `func BuildForUnixRun(cfg *Config, platform string, entries []FileEntry, outputDir string) error`.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/main_test.go`:

```go
func TestBuildForUnixRunLinuxShellOnly(t *testing.T) {
	projectDir := t.TempDir()
	configContent := `
name = "diag"
version = "v1.0.0"
mode = "run"

[[commands]]
shell = "echo hello"
shell_linux = "mkdir -p {dst.HOME}/.local/share/diag"
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)

	if err := BuildForPlatform(cfg, "linux-amd64", nil, nil, nil, outputDir); err != nil {
		t.Fatalf("BuildForPlatform linux run: %v", err)
	}
	script, err := os.ReadFile(filepath.Join(outputDir, "diag_v1.0.0_linux_amd64.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(script)
	if !strings.HasPrefix(s, "#!/bin/bash") {
		t.Error("run .sh must start with #!/bin/bash")
	}
	// Resolved linux shell + HOME substitution applied.
	if !strings.Contains(s, `eval "mkdir -p $HOME/.local/share/diag"`) {
		t.Errorf("linux run .sh missing resolved/HOME-substituted shell; got:\n%s", s)
	}
	// No macOS dialog on linux.
	if strings.Contains(s, "osascript") {
		t.Error("linux run .sh must not contain osascript")
	}
	// No install machinery.
	if strings.Contains(s, "cmd_install") {
		t.Error("run .sh must not contain install machinery")
	}
}

func TestBuildForUnixRunDarwinHasDialog(t *testing.T) {
	projectDir := t.TempDir()
	configContent := `
name = "diag"
version = "v1.0.0"
mode = "run"

[[commands]]
shell = "echo hi"
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)

	if err := BuildForPlatform(cfg, "darwin-arm64", nil, nil, nil, outputDir); err != nil {
		t.Fatalf("BuildForPlatform darwin run: %v", err)
	}
	s, err := os.ReadFile(filepath.Join(outputDir, "diag_v1.0.0_darwin_arm64.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(s), "osascript") {
		t.Error("darwin run .sh must contain osascript dialog")
	}
}

func TestBuildForPlatformAllowsHomeOnLinux(t *testing.T) {
	out := t.TempDir()
	cfg := &Config{
		Name: "x", Version: "v1", Mode: "install",
		Dirs: []DirConfig{{Path: "{dst.HOME}/.config/x", Mode: "0755"}},
	}
	// {dst.HOME} must NOT be rejected on linux (unlike Windows-only markers)...
	if err := BuildForPlatform(cfg, "linux-amd64", nil, nil, nil, out); err != nil {
		t.Fatalf("{dst.HOME} should be allowed on linux: %v", err)
	}
	// ...and it must be substituted to $HOME in the install .sh.
	script, err := os.ReadFile(filepath.Join(out, "x_v1_linux_amd64.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(script); !strings.Contains(s, `$HOME/.config/x`) || strings.Contains(s, "{dst.HOME}") {
		t.Errorf("{dst.HOME} should be substituted to $HOME; got:\n%s", s)
	}
}

func TestBuildForPlatformStillRejectsWindowsOnlyMarkerOnLinuxResolvedPath(t *testing.T) {
	// path_macos carries a Windows-only marker; building for macos uses it as the
	// resolved path, so it must be rejected.
	cfg := &Config{
		Name: "x", Version: "v1", Mode: "install",
		Dirs: []DirConfig{{Path: "/var/lib/x", PathMacos: "{dst.AppDataFolder}\\x", Mode: "0755"}},
	}
	err := BuildForPlatform(cfg, "darwin-arm64", nil, nil, nil, "")
	if err == nil {
		t.Fatal("should reject Windows-only marker in resolved macos path")
	}
	if !strings.Contains(err.Error(), "not supported on darwin") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildForPlatformIgnoresPathWindowsOnLinux(t *testing.T) {
	out := t.TempDir()
	// path_windows has a Windows-only marker, but a linux build does not use it,
	// so the build must succeed.
	cfg := &Config{
		Name: "x", Version: "v1", Mode: "install",
		Dirs: []DirConfig{{Path: "/var/lib/x", PathWindows: "{dst.AppDataFolder}\\x", Mode: "0755"}},
	}
	if err := BuildForPlatform(cfg, "linux-amd64", nil, nil, nil, out); err != nil {
		t.Fatalf("path_windows marker must not affect a linux build: %v", err)
	}
}
```

Also update the existing `TestBuildForWindowsRunRejectsNonWindows` (it currently asserts `mode=run` is Windows-only — that is no longer true). Change its name and body to assert the opposite for unix (run is now allowed), or delete it. Replace it with:

```go
func TestBuildForPlatformRunAllowedOnUnix(t *testing.T) {
	cfg := &Config{Name: "x", Version: "v1", Mode: "run", Commands: []CommandConfig{{Shell: "echo hi"}}}
	// mode=run is no longer Windows-only; linux/darwin must reach the run builder
	// (they may fail later for other reasons, but NOT with "Windows-only").
	for _, p := range []string{"linux-amd64", "darwin-arm64"} {
		err := BuildForPlatform(cfg, p, nil, nil, nil, "")
		if err != nil && strings.Contains(err.Error(), "Windows-only") {
			t.Errorf("run mode must not be Windows-only on %s: %v", p, err)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run 'TestBuildForUnixRun|TestBuildForPlatformAllowsHome|TestBuildForPlatformStillRejects|TestBuildForPlatformIgnoresPathWindows|TestBuildForPlatformRunAllowedOnUnix' -v`
Expected: FAIL (run mode still rejected as Windows-only; `BuildForUnixRun` undefined; HOME rejected on linux).

- [ ] **Step 3: Lift the guard and add per-OS resolution at the top of `BuildForPlatform`**

In `cmd/onepkg/main.go`, in `BuildForPlatform` (starts ~line 200), replace the guard block (lines ~208-225):

```go
	if cfg.Mode == "run" && goos != "windows" {
		return fmt.Errorf("mode=\"run\" is Windows-only; got platform %q", platform)
	}

	// Windows known-folder variables in install path fields are not resolvable on
	// Linux/macOS targets; fail fast rather than emit a meaningless literal.
	if goos != "windows" {
		for _, d := range cfg.Dirs {
			if installer.HasKnownFolderMarker(d.Path) {
				return fmt.Errorf("Windows known-folder variable in dirs[].path %q is not supported on %s; build for windows or remove it", d.Path, goos)
			}
		}
		for _, o := range cfg.SystemOverlay {
			if installer.HasKnownFolderMarker(o.System) {
				return fmt.Errorf("Windows known-folder variable in system_overlay[].system %q is not supported on %s; build for windows or remove it", o.System, goos)
			}
		}
	}
```

with:

```go
	// Collapse per-OS overrides to the effective values for this target. Operate
	// on a local copy so main()'s shared *Config is untouched across platforms.
	resolved := cfg.resolveFor(goos)
	cfg = &resolved

	// Windows-only {dst.*} markers cannot resolve on Linux/macOS targets. Check
	// the RESOLVED path for this OS (so a marker in path_windows does not affect
	// a linux build); {dst.HOME} is cross-platform and allowed.
	if goos != "windows" {
		for _, d := range cfg.Dirs {
			if installer.HasWindowsOnlyKnownFolderMarker(d.Path) {
				return fmt.Errorf("Windows known-folder variable in dirs[].path %q is not supported on %s; build for windows or remove it", d.Path, goos)
			}
		}
		for _, o := range cfg.SystemOverlay {
			if installer.HasWindowsOnlyKnownFolderMarker(o.System) {
				return fmt.Errorf("Windows known-folder variable in system_overlay[].system %q is not supported on %s; build for windows or remove it", o.System, goos)
			}
		}
		// Substitute the cross-platform {dst.HOME} marker -> $HOME on Unix targets
		// (install-mode paths here; run-mode commands are substituted in
		// BuildForUnixRun). The shell expands $HOME at runtime.
		for i := range cfg.Dirs {
			cfg.Dirs[i].Path = substituteHomeUnix(cfg.Dirs[i].Path)
		}
		for i := range cfg.SystemOverlay {
			cfg.SystemOverlay[i].System = substituteHomeUnix(cfg.SystemOverlay[i].System)
		}
	}
```

- [ ] **Step 4: Route unix run mode and add `BuildForUnixRun`**

The `linux`/`darwin` branches of `BuildForPlatform`'s `switch goos` currently always build the install `.sh`. Short-circuit run mode first: insert this branch immediately after the guard block from Step 3 (so `cfg` is already the resolved copy and before the install-only `GenerateVersion`/service generation):

```go
	if cfg.Mode == "run" && (goos == "linux" || goos == "darwin") {
		return BuildForUnixRun(cfg, platform, entries, outputDir)
	}
```

Then add the new function at the end of `main.go` (after `BuildForWindowsRun`):

```go
// BuildForUnixRun builds a mode="run" self-extracting .sh for Linux/macOS:
// a tar.gz payload (no VERSION), a lean run .sh, written with the .sh extension.
// {dst.HOME} is substituted to the literal $HOME token (the shell expands it).
func BuildForUnixRun(cfg *Config, platform string, entries []FileEntry, outputDir string) error {
	parts := strings.SplitN(platform, "-", 2)
	goos, arch := parts[0], parts[1]

	// No VERSION file: run mode has no verify action. Empty/nil when no entries.
	var tarData []byte
	var err error
	if len(entries) > 0 {
		tarData, err = BuildTarGZ(entries, *cfg.PreservePermissions)
		if err != nil {
			return fmt.Errorf("build tar.gz: %w", err)
		}
	}

	commands := make([]installer.CommandSpec, 0, len(cfg.Commands))
	for _, c := range cfg.Commands {
		// cfg is already resolved for goos; collapse to a CommandSpec and
		// substitute {dst.HOME} -> $HOME in the effective strings.
		commands = append(commands, installer.CommandSpec{
			Exec:  substituteHomeUnix(c.Exec),
			Args:  substituteHomeUnixArgs(c.Args),
			Shell: substituteHomeUnix(c.Shell),
		})
	}

	script, err := installer.GenerateShellRunner(installer.ShellRunnerConfig{
		Name:          cfg.Name,
		Version:       cfg.Version,
		Platform:      platform,
		ExpectedArch:  unameArch(goos, arch),
		Goos:          goos,
		HasPayload:    len(entries) > 0,
		PayloadBase64: base64.StdEncoding.EncodeToString(tarData),
		Commands:      commands,
	})
	if err != nil {
		return fmt.Errorf("generate run script: %w", err)
	}

	outputPath := platformOutputPath(cfg, platform, outputDir)
	return os.WriteFile(outputPath, []byte(script), 0755)
}

// substituteHomeUnix replaces the cross-platform {dst.HOME} marker with the
// literal $HOME token for Unix targets (the shell expands $HOME at runtime).
// Other {dst.*} markers are Windows-only and rejected by the build guard, so
// they never reach here on a unix build.
func substituteHomeUnix(s string) string {
	return strings.ReplaceAll(s, "{dst.HOME}", "$HOME")
}

func substituteHomeUnixArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = substituteHomeUnix(a)
	}
	return out
}
```

- [ ] **Step 5: Run the new tests to verify they pass, plus the guard regression tests**

Run: `cd cmd/onepkg && go test -run 'TestBuildForUnixRun|TestBuildForPlatformAllowsHome|TestBuildForPlatformStillRejects|TestBuildForPlatformIgnoresPathWindows|TestBuildForPlatformRunAllowedOnUnix|TestBuildForPlatformRejectsKnownFolderOnLinux|TestBuildForPlatformRejectsKnownFolderOverlayOnDarwin|TestBuildForWindowsRunWithItems' -v`
Expected: PASS (the two existing "rejects known folder" tests still pass — `{dst.AppDataFolder}` in a base path is still a Windows-only marker caught by `HasWindowsOnlyKnownFolderMarker`).

- [ ] **Step 6: Run the whole suite to confirm no regression**

Run: `make test`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/onepkg/main.go cmd/onepkg/main_test.go
git commit -m "$(cat <<'EOF'
feat(run): support run-mode on linux/macos with per-OS resolution

Lifts the Windows-only run-mode guard; BuildForPlatform now resolves
per-OS overrides per target (local copy, caller untouched), routes
linux/darwin run mode to BuildForUnixRun (tar.gz payload + run .sh),
substitutes {dst.HOME} -> $HOME, and refines the known-folder guard to
the resolved path while allowing {dst.HOME} on non-Windows.

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Build-time per-platform exec-match validation

`validateRun` checks the *base* exec against items (kept). But a `exec_windows`/`exec_linux`/`exec_macos` override may name a different file than the base. Fail fast at build time: the resolved exec (per target OS) must match a bundled item.

**Files:**
- Modify: `cmd/onepkg/main.go` (`BuildForPlatform`, run-mode branch)
- Test: `cmd/onepkg/main_test.go`

**Interfaces:**
- Consumes: `itemMatches` (existing, `config.go`), `cfg.Items`, resolved `cfg.Commands`.

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/main_test.go`:

```go
func TestBuildForUnixRunRejectsUnmatchedOverrideExec(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "cleanup"), []byte("x"), 0755)

	configContent := `
name = "x"
version = "v1"
mode = "run"
items = ["bin/cleanup"]

[[commands]]
exec = "bin/cleanup"
exec_linux = "bin/does-not-exist"
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate (base exec matches): %v", err)
	}
	// linux build uses exec_linux, which is not an item -> build-time error.
	err = BuildForPlatform(cfg, "linux-amd64", nil, nil, nil, "")
	if err == nil {
		t.Fatal("build should reject exec_linux that matches no item")
	}
	if !strings.Contains(err.Error(), "not found in items") {
		t.Errorf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestBuildForUnixRunRejectsUnmatchedOverrideExec -v`
Expected: FAIL (build currently succeeds because resolved-exec item match is not checked).

- [ ] **Step 3: Add the build-time check**

In `cmd/onepkg/main.go`, in the run-mode routing block added in Task 8 (the `if cfg.Mode == "run" && (goos == ...) { return BuildForUnixRun(...) }` line), insert a validation loop *before* calling `BuildForUnixRun`. Replace that block with:

```go
	if cfg.Mode == "run" && (goos == "linux" || goos == "darwin") {
		if err := validateResolvedCommands(cfg, goos); err != nil {
			return err
		}
		return BuildForUnixRun(cfg, platform, entries, outputDir)
	}
```

And add the helper near `BuildForUnixRun`:

```go
// validateResolvedCommands checks the per-platform run-mode command rules that
// Validate() cannot (it has no target OS): each resolved command has exactly
// one of exec/shell (guaranteed by base-type-fixed resolution) and, if exec,
// the resolved exec names a bundled item. cfg must already be resolveFor(goos).
func validateResolvedCommands(cfg *Config, goos string) error {
	for i, c := range cfg.Commands {
		_ = goos
		if c.Exec != "" && !itemMatches(cfg.Items, c.Exec) {
			return fmt.Errorf("command %d exec %q not found in items for %s", i, c.Exec, goos)
		}
	}
	return nil
}
```

Also apply the same check to the Windows run path: in `BuildForWindowsRun`, before generating, add an equivalent guard. At the top of `BuildForWindowsRun` (after `arch := parts[1]`), add:

```go
	if err := validateResolvedCommands(cfg, "windows"); err != nil {
		return err
	}
```

(`BuildForWindowsRun` receives the already-resolved cfg because `BuildForPlatform` resolved it in Task 8 Step 3 before calling `BuildForWindows` → `BuildForWindowsRun`.)

- [ ] **Step 4: Run test to verify it passes, plus existing windows run tests**

Run: `cd cmd/onepkg && go test -run 'TestBuildForUnixRunRejectsUnmatchedOverrideExec|TestBuildForWindowsRunWithItems|TestBuildForWindowsRunShellOnly' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/main.go cmd/onepkg/main_test.go
git commit -m "$(cat <<'EOF'
feat(run): validate resolved exec matches an item at build time

Per-platform exec overrides may name a different file than the base
exec; check the resolved exec for the target OS against items at build
time (unix and windows run paths), failing fast instead of emitting a
script that references a missing file.

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Docs, example config, and spec amendment

**Files:**
- Modify: `docs/superpowers/specs/2026-07-15-cross-platform-run-mode-and-overrides-design.md` (one accuracy fix)
- Modify: `docs/config-example.toml` (add example)
- Modify: `CLAUDE.md` (document the new conventions)

- [ ] **Step 1: Amend the spec's over-claim about mixed exec/shell per platform**

In `docs/superpowers/specs/2026-07-15-cross-platform-run-mode-and-overrides-design.md`, find the sentence in the "Config struct additions" section:

> `... so a command can be `exec` on Linux but `shell` on Windows — exactly-one-of rule is checked **per resolved platform** at build time).`

Replace that parenthetical with:

> `... command type (exec vs shell) is fixed by the base field; `_<os>` overrides refine the same-typed value per platform. The base exactly-one-of rule stays in `validateRun`; build-time checks verify the resolved exec names an item).`

- [ ] **Step 2: Add a cross-platform run-mode example to the example config**

Append to `docs/config-example.toml` (read it first to match style):

```toml

# ── Cross-platform run-mode example ──────────────────────────────────────────
# mode = "run" builds a one-shot package. One [[commands]] list serves all
# platforms: each field's effective value is the <field>_<os> override if set,
# else the base field. {dst.HOME} resolves to $HOME on every platform.
#
# name = "diag"
# version = "v1.0.0"
# mode = "run"
# items = ["bin/diag.{platform}"]
#
# [[commands]]
# exec          = "bin/diag"            # default (linux/macos)
# exec_windows  = "bin/diag.exe"        # windows override
# args          = ["--check"]
# args_windows  = ["/check"]
#
# [[commands]]
# shell         = "mkdir -p {dst.HOME}/.config/diag"   # base (linux/macos)
# shell_windows = "New-Item -ItemType Directory -Force \"$env:APPDATA\\diag\""
```

- [ ] **Step 3: Document the new conventions in CLAUDE.md**

In `CLAUDE.md`, in the **Key Conventions** section, add a bullet:

```
- **Per-platform field overrides:** `commands[].exec|args|shell`, `dirs[].path`, and `system_overlay[].system` each accept a base value plus optional `_<os>` variants (`exec_windows`, `path_macos`, `system_linux`, ...). Effective value for a target OS = override if set, else base. Command type is fixed by the base field; overrides refine the value. `_<os>` is keyed by OS family (linux/darwin/windows), never arch. One config builds many platforms.
```

And update the **Run mode** bullet to note Linux/macOS:

```
- **Run mode (`mode = "run"`):** Windows `.exe` (MessageBox), Linux `.sh` (stdout `[OK]`/`[FAIL]`, no dialog), macOS `.sh` (stdout + `osascript` dialog). Run-all; no install/service/`VERSION`.
```

And add a marker note under the config marker namespaces bullet:

```
  - `{dst.HOME}` is the one cross-platform `{dst.*}` marker — resolves to `$HOME` on Linux, macOS, and Windows. All other `{dst.NAME}` remain Windows-only.
```

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-07-15-cross-platform-run-mode-and-overrides-design.md docs/config-example.toml CLAUDE.md
git commit -m "$(cat <<'EOF'
docs: document cross-platform run mode, overrides, {dst.HOME}

Amends the spec's mixed exec/shell claim, adds a cross-platform run-mode
example to config-example.toml, and records the per-platform override
convention, run-mode platform behavior, and {dst.HOME} marker in CLAUDE.md.

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

---

## Done criteria

- `make test` passes (main suite + stub-module suite).
- A `mode = "run"` config builds for `linux-arm64`, `darwin-arm64`, and `windows-amd64` from one file using `_<os>` overrides; `{dst.HOME}` appears as `$HOME` in the unix scripts.
- Linux run `.sh` has no `osascript`; macOS run `.sh` has it; Windows run `.exe` is unchanged in shape.
- `{dst.AppDataFolder}` in a resolved linux/macos path fails fast; in `path_windows` it does not affect a linux build.
