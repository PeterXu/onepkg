# Cross-Platform `{dst.*}` Markers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expand the cross-platform `{dst.*}` set from `{dst.HOME}` alone to four markers — `HOME`, `TMPDIR`, `USER`, `HOSTNAME` — each resolving on Linux, macOS, and Windows at install/run time.

**Architecture:** A single token table in `installer/knownfolders.go` maps each cross-platform name to a unix (bash) token and a windows (PowerShell) token. The unix builders bake the bash token into the generated `.sh` via a new `installer.ReplaceCrossPlatformDst(s, goos)`; the Windows render functions (`EmitPathExpr`, `ReplaceMarkers`) emit the PowerShell token inline. `{env.VAR}` and the Windows-only known-folder vocabulary are unchanged.

**Tech Stack:** Go 1.21, module `onepkg`, dependency `github.com/BurntSushi/toml`. No new dependencies.

## Global Constraints

- **TDD:** failing test first, then minimal implementation, every function gets a test.
- **Package layout:** `cmd/onepkg/*.go` is `package main`; `cmd/onepkg/installer/` is `package installer`.
- **Stubs required:** Windows `//go:embed` stubs must already exist — run `make win-wrapper` (or `make build`) once before building/testing. They exist in this repo; if a Windows test fails to build, run `make build` first.
- **Generated scripts:** install `.sh` uses `set -euo pipefail`; run `.sh` uses `set -uo pipefail`. New bash tokens must be safe under `set -u` (use `${VAR:-fallback}`).
- **No regression:** unmarked configs and `{dst.HOME}`-only configs produce byte-identical output.
- **Windows-only `{dst.*}` vocabulary unchanged** (`AppDataFolder`, …); still rejected on Linux/macOS by the existing build guard.
- **`{env.VAR}` unchanged** (build-time, build host, every string field).

---

### Task 1: Cross-platform token table, predicates, and unix helper

Add the token table that is the single source of truth, update the name predicates so the four names are recognized, and add `ReplaceCrossPlatformDst` for the unix builders. This task is the data layer; Task 2 wires the Windows render functions to the same table.

**Files:**
- Modify: `cmd/onepkg/installer/knownfolders.go` (replace the `crossPlatformFolders` map and the two predicates; add `crossPlatformDst`, `crossPlatformDstToken`, `ReplaceCrossPlatformDst`)
- Test: `cmd/onepkg/installer/knownfolders_test.go`

**Interfaces:**
- Produces: `crossPlatformDst` (map), `crossPlatformDstToken(name, goos string) (string, bool)`, `ReplaceCrossPlatformDst(s, goos string) string`. Task 2 consumes `crossPlatformDstToken`/`crossPlatformDst`; Task 3 consumes `ReplaceCrossPlatformDst`.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/installer/knownfolders_test.go`:

```go
func TestCrossPlatformDstMarkersKnown(t *testing.T) {
	// All four cross-platform markers are recognized as known and are not Windows-only.
	for _, name := range []string{"HOME", "TMPDIR", "USER", "HOSTNAME"} {
		if err := ValidateKnownFolderMarkers("{dst." + name + "}/x"); err != nil {
			t.Errorf("{dst.%s} should validate: %v", name, err)
		}
		if !HasKnownFolderMarker("{dst." + name + "}/x") {
			t.Errorf("HasKnownFolderMarker should detect {dst.%s}", name)
		}
		if isWindowsOnlyFolderName(name) {
			t.Errorf("%s must not be Windows-only", name)
		}
	}
}

func TestReplaceCrossPlatformDst(t *testing.T) {
	// Unix tokens (identical for linux and darwin).
	for _, goos := range []string{"linux", "darwin"} {
		cases := []struct{ in, want string }{
			{"{dst.HOME}/x", "$HOME/x"},
			{"{dst.TMPDIR}/x", "${TMPDIR:-/tmp}/x"},
			{"{dst.USER}", "${USER:-$(id -un)}"},
			{"{dst.HOSTNAME}-host", "${HOSTNAME:-$(hostname)}-host"},
			{"pre {dst.TMPDIR} {dst.USER} post", "pre ${TMPDIR:-/tmp} ${USER:-$(id -un)} post"},
		}
		for _, c := range cases {
			if got := ReplaceCrossPlatformDst(c.in, goos); got != c.want {
				t.Errorf("ReplaceCrossPlatformDst(%q, %s) = %q, want %q", c.in, goos, got, c.want)
			}
		}
	}
	// Windows tokens.
	wcases := []struct{ in, want string }{
		{"{dst.HOME}", "$HOME"},
		{"{dst.TMPDIR}\\x", "$env:TEMP\\x"},
		{"{dst.USER}", "$env:USERNAME"},
		{"{dst.HOSTNAME}", "$env:COMPUTERNAME"},
	}
	for _, c := range wcases {
		if got := ReplaceCrossPlatformDst(c.in, "windows"); got != c.want {
			t.Errorf("ReplaceCrossPlatformDst(%q, windows) = %q, want %q", c.in, got, c.want)
		}
	}
	// Unknown marker left intact (validation rejects it earlier).
	if got := ReplaceCrossPlatformDst("{dst.Bogus}/x", "linux"); got != "{dst.Bogus}/x" {
		t.Errorf("unknown marker should be left intact, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test ./installer/ -run 'TestCrossPlatformDst|TestReplaceCrossPlatformDst' -v`
Expected: FAIL — `ReplaceCrossPlatformDst` undefined; TMPDIR/USER/HOSTNAME fail validation (unknown names).

- [ ] **Step 3: Replace the cross-platform map and predicates**

In `cmd/onepkg/installer/knownfolders.go`, replace this block (lines ~30–47):

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

with:

```go
// crossPlatformDst maps a cross-platform {dst.NAME} marker to its resolution
// token on unix (bash; identical for linux and darwin) and windows (PowerShell).
// Each is an env/automatic variable on the target, so no runtime definition line
// is emitted for them (unlike the Windows-only KnownFolders, which define
// $<Name> = [Environment]::GetFolderPath(...)).
var crossPlatformDst = map[string]struct{ unix, windows string }{
	"HOME":     {"$HOME", "$HOME"},
	"TMPDIR":   {"${TMPDIR:-/tmp}", "$env:TEMP"},
	"USER":     {"${USER:-$(id -un)}", "$env:USERNAME"},
	"HOSTNAME": {"${HOSTNAME:-$(hostname)}", "$env:COMPUTERNAME"},
}

// crossPlatformDstToken returns the target-side resolution token for a
// cross-platform {dst.NAME} on goos, and whether name is one. linux and darwin
// share the unix token.
func crossPlatformDstToken(name, goos string) (string, bool) {
	e, ok := crossPlatformDst[name]
	if !ok {
		return "", false
	}
	if goos == "windows" {
		return e.windows, true
	}
	return e.unix, true
}

// isKnownFolderName reports whether name is a resolvable {dst.NAME} marker name
// (a Windows-only known folder, or a cross-platform marker like HOME/TMPDIR/...).
func isKnownFolderName(name string) bool {
	if _, ok := KnownFolders[name]; ok {
		return true
	}
	_, ok := crossPlatformDst[name]
	return ok
}

// isWindowsOnlyFolderName reports whether name resolves only on Windows.
func isWindowsOnlyFolderName(name string) bool {
	_, ok := KnownFolders[name]
	return ok
}
```

- [ ] **Step 4: Add `ReplaceCrossPlatformDst`**

Append to `cmd/onepkg/installer/knownfolders.go` (after `ReplaceMarkers`):

```go
// ReplaceCrossPlatformDst rewrites every cross-platform {dst.NAME} in s to its
// resolution token on goos (linux/darwin -> bash, windows -> PowerShell). The
// unix builders bake the resulting token into the generated .sh (the shell
// expands it at runtime); Windows renders via EmitPathExpr/ReplaceMarkers
// instead. Unknown {dst.X} are left in place; validation rejects them earlier.
func ReplaceCrossPlatformDst(s, goos string) string {
	return markerRe.ReplaceAllStringFunc(s, func(tok string) string {
		m := markerRe.FindStringSubmatch(tok)
		if replacement, ok := crossPlatformDstToken(m[1], goos); ok {
			return replacement
		}
		return tok
	})
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test ./installer/ -run 'TestCrossPlatformDst|TestReplaceCrossPlatformDst' -v`
Expected: PASS.

- [ ] **Step 6: Run the full installer suite to confirm no regression**

Run: `cd cmd/onepkg && go test ./installer/ -v`
Expected: PASS (existing HOME tests still green; `isKnownFolderName` still recognizes HOME via the new map).

- [ ] **Step 7: Commit**

```bash
git add cmd/onepkg/installer/knownfolders.go cmd/onepkg/installer/knownfolders_test.go
git commit -m "feat(installer): cross-platform {dst.*} token table + unix helper

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: Windows render functions handle the cross-platform markers

`EmitPathExpr` (install paths/overlays and run args) and `ReplaceMarkers` (run shell) currently special-case only `HOME`. Generalize them to render all four cross-platform markers via the token table, and make `EmitKnownFolderDefs` skip all four (no `[Environment]::GetFolderPath` definition — they are env/automatic vars).

**Files:**
- Modify: `cmd/onepkg/installer/knownfolders.go` (`ReplaceMarkers`, `EmitKnownFolderDefs`, `EmitPathExpr`)
- Test: `cmd/onepkg/installer/knownfolders_test.go`

**Interfaces:**
- Consumes: `crossPlatformDstToken(name, goos)` from Task 1.
- Produces: extended `EmitPathExpr`, `ReplaceMarkers`, `EmitKnownFolderDefs` (signatures unchanged).

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/installer/knownfolders_test.go`:

```go
func TestCrossPlatformDstWindowsRender(t *testing.T) {
	// EmitPathExpr renders cross-platform markers inline as PowerShell tokens
	// (no $<Name> definition, no concatenation def needed).
	peCases := []struct{ in, want string }{
		{`{dst.TMPDIR}\myapp`, `$env:TEMP + '\myapp'`},
		{`{dst.USER}\cfg`, `$env:USERNAME + '\cfg'`},
		{`{dst.HOSTNAME}`, `$env:COMPUTERNAME`},
		{`{dst.HOME}\x`, `$HOME + '\x'`}, // regression
	}
	for _, c := range peCases {
		if got := EmitPathExpr(c.in); got != c.want {
			t.Errorf("EmitPathExpr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// ReplaceMarkers (run shell/args inline).
	if got := ReplaceMarkers("echo {dst.TMPDIR} {dst.USER}"); got != "echo $env:TEMP $env:USERNAME" {
		t.Errorf("ReplaceMarkers = %q, want echo $env:TEMP $env:USERNAME", got)
	}
	// EmitKnownFolderDefs emits NO definition for any cross-platform marker.
	for _, name := range []string{"HOME", "TMPDIR", "USER", "HOSTNAME"} {
		if out := EmitKnownFolderDefs([]string{name}); out != "" {
			t.Errorf("EmitKnownFolderDefs(%q) should be empty, got %q", name, out)
		}
	}
	// Mixed: still emits defs for Windows-only folders, skipping cross-platform ones.
	out := EmitKnownFolderDefs([]string{"TMPDIR", "AppDataFolder"})
	if !strings.Contains(out, "$AppDataFolder = [Environment]::GetFolderPath") {
		t.Errorf("AppDataFolder def missing; got:\n%s", out)
	}
	if strings.Contains(out, "TMPDIR") {
		t.Errorf("cross-platform name must not get a def; got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test ./installer/ -run TestCrossPlatformDstWindowsRender -v`
Expected: FAIL — new markers not rendered (left as literals / unknown).

- [ ] **Step 3: Generalize `ReplaceMarkers`**

In `cmd/onepkg/installer/knownfolders.go`, replace the `ReplaceMarkers` function:

```go
// ReplaceMarkers replaces each known {dst.Name} token in s with the PowerShell
// variable reference $Name, for inline use inside run-mode shell expressions.
// Cross-platform markers (HOME/TMPDIR/USER/HOSTNAME) become their PowerShell
// env/automatic variable; unknown {dst.X} tokens are left in place (validation
// rejects them earlier).
func ReplaceMarkers(s string) string {
	return markerRe.ReplaceAllStringFunc(s, func(tok string) string {
		m := markerRe.FindStringSubmatch(tok)
		if replacement, ok := crossPlatformDstToken(m[1], "windows"); ok {
			return replacement
		}
		if _, ok := KnownFolders[m[1]]; ok {
			return "$" + m[1]
		}
		return tok
	})
}
```

- [ ] **Step 4: Generalize `EmitKnownFolderDefs`**

In `cmd/onepkg/installer/knownfolders.go`, replace the `EmitKnownFolderDefs` function:

```go
// EmitKnownFolderDefs emits a "$<Name> = [Environment]::GetFolderPath(...)" line
// for each Windows-only name in names (deduped, sorted). Cross-platform markers
// (HOME/TMPDIR/USER/HOSTNAME) are env/automatic variables and get no definition.
// Empty input produces empty output.
func EmitKnownFolderDefs(names []string) string {
	var b strings.Builder
	for _, n := range dedupeSorted(names) {
		if _, ok := crossPlatformDst[n]; ok {
			continue // cross-platform markers need no runtime definition
		}
		fmt.Fprintf(&b, "$%s = [Environment]::GetFolderPath([Environment+SpecialFolder]::%s)\n", n, KnownFolders[n])
	}
	return b.String()
}
```

- [ ] **Step 5: Generalize `EmitPathExpr`**

In `cmd/onepkg/installer/knownfolders.go`, replace the marker-resolution branch inside `EmitPathExpr` (the `if name == "HOME"` / `else if KnownFolders` block) so it reads:

```go
		name := s[nameStart:nameEnd]
		if tok, ok := crossPlatformDstToken(name, "windows"); ok {
			parts = append(parts, tok)
		} else if _, ok := KnownFolders[name]; ok {
			parts = append(parts, "$"+name)
		} else {
			parts = append(parts, psQuote(s[start:end]))
		}
```

(The full `EmitPathExpr` after the change:)

```go
func EmitPathExpr(s string) string {
	if !markerRe.MatchString(s) {
		return psQuote(s)
	}
	var parts []string
	idx := 0
	for _, m := range markerRe.FindAllStringSubmatchIndex(s, -1) {
		start, end := m[0], m[1]
		nameStart, nameEnd := m[2], m[3]
		if start > idx {
			parts = append(parts, psQuote(s[idx:start]))
		}
		name := s[nameStart:nameEnd]
		if tok, ok := crossPlatformDstToken(name, "windows"); ok {
			parts = append(parts, tok)
		} else if _, ok := KnownFolders[name]; ok {
			parts = append(parts, "$"+name)
		} else {
			parts = append(parts, psQuote(s[start:end]))
		}
		idx = end
	}
	if idx < len(s) {
		parts = append(parts, psQuote(s[idx:]))
	}
	return strings.Join(parts, " + ")
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test ./installer/ -run 'TestCrossPlatformDstWindowsRender|TestHomeMarkerRender|TestEmitPathExpr|TestKnownFolderMarkersAndReplaceMarkers|TestEmitKnownFolderDefs' -v`
Expected: PASS (new test green; existing HOME/known-folder regression tests still green).

- [ ] **Step 7: Run the full installer suite**

Run: `cd cmd/onepkg && go test ./installer/ -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/onepkg/installer/knownfolders.go cmd/onepkg/installer/knownfolders_test.go
git commit -m "feat(installer): render cross-platform {dst.*} in PowerShell generators

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Wire unix substitution in `main.go` (install + run)

Replace the HOME-only `substituteHomeUnix`/`substituteHomeUnixArgs` helpers with calls to `installer.ReplaceCrossPlatformDst`, so install-mode `dirs[].path`/`overlay.system` and run-mode `exec`/`args`/`shell` get all four cross-platform markers substituted to their bash token on Linux/macOS.

**Files:**
- Modify: `cmd/onepkg/main.go` (the non-Windows substitution loop in `BuildForPlatform`; the command loop in `BuildForUnixRun`; replace the two `substituteHome*` helpers)
- Test: `cmd/onepkg/main_test.go`

**Interfaces:**
- Consumes: `installer.ReplaceCrossPlatformDst(s, goos string) string` from Task 1.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/main_test.go`:

```go
func TestBuildForPlatformAllowsCrossPlatformDstOnLinux(t *testing.T) {
	out := t.TempDir()
	cfg := &Config{
		Name: "x", Version: "v1", Mode: "install",
		Dirs: []DirConfig{{Path: "{dst.TMPDIR}/x/{dst.USER}", Mode: "0755"}},
	}
	// Cross-platform markers must NOT be rejected on linux (unlike Windows-only).
	if err := BuildForPlatform(cfg, "linux-amd64", nil, nil, nil, out); err != nil {
		t.Fatalf("cross-platform markers should be allowed on linux: %v", err)
	}
	script, err := os.ReadFile(filepath.Join(out, "x_v1_linux_amd64.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(script)
	want := `${TMPDIR:-/tmp}/x/${USER:-$(id -un)}`
	if !strings.Contains(s, want) {
		t.Errorf("linux install .sh missing unix tokens %q; got:\n%s", want, s)
	}
	if strings.Contains(s, "{dst.") {
		t.Errorf("raw {dst. marker must not survive into the .sh; got:\n%s", s)
	}
}

func TestBuildForUnixRunLinuxCrossPlatformDst(t *testing.T) {
	projectDir := t.TempDir()
	configContent := `
name = "diag"
version = "v1.0.0"
mode = "run"

[[commands]]
shell = "echo hi"
shell_linux = "mkdir -p {dst.TMPDIR}/diag && echo {dst.USER}@{dst.HOSTNAME}"
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
	want := `mkdir -p ${TMPDIR:-/tmp}/diag && echo ${USER:-$(id -un)}@${HOSTNAME:-$(hostname)}`
	if !strings.Contains(s, want) {
		t.Errorf("linux run .sh missing unix-token-substituted shell; got:\n%s", s)
	}
	if strings.Contains(s, "{dst.") {
		t.Errorf("raw {dst. marker must not survive into run .sh; got:\n%s", s)
	}
}

func TestBuildForPlatformWindowsCrossPlatformDstDirs(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "myapp.exe"), []byte("fake-exe"), 0755)

	content := `
name = "myapp"
version = "v1.0.0"
items = ["bin/myapp.exe"]

[[dirs]]
path = "{dst.TMPDIR}\\myapp"
mode = "0755"
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(content), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	resolved, err := ResolveItems(projectDir, cfg.Items, "windows-amd64")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := CollectFilesResolved(projectDir, resolved, cfg.Exclude)
	if err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)

	if err := BuildForPlatform(cfg, "windows-amd64", entries, nil, nil, outputDir); err != nil {
		t.Fatalf("BuildForPlatform windows should accept {dst.TMPDIR} dirs: %v", err)
	}
	exe, err := os.ReadFile(filepath.Join(outputDir, "myapp_v1.0.0_windows_amd64.exe"))
	if err != nil {
		t.Fatal(err)
	}
	// Cross-platform marker renders inline as $env:TEMP; no $TMPDIR definition.
	if !bytes.Contains(exe, []byte("$DIR_0_PATH = $env:TEMP + '\\myapp'")) {
		t.Errorf("embedded install.ps1 missing $env:TEMP path expr; got (tail):\n%s", exe[len(exe)-400:])
	}
	if bytes.Contains(exe, []byte("$TMPDIR =")) {
		t.Error("cross-platform markers must not emit a $TMPDIR definition")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run (stubs must exist — run `make build` once first if this fails to compile): `cd cmd/onepkg && go test . -run 'TestBuildForPlatformAllowsCrossPlatformDstOnLinux|TestBuildForUnixRunLinuxCrossPlatformDst|TestBuildForPlatformWindowsCrossPlatformDstDirs' -v`
Expected: FAIL — the linux tests see raw `{dst.TMPDIR}` (HOME-only substitution); the windows test sees no `$env:TEMP` (EmitPathExpr already fixed in Task 2, so this one may pass already — that is fine; the linux two drive this task).

- [ ] **Step 3: Replace the install-mode unix substitution loop**

In `cmd/onepkg/main.go`, inside `BuildForPlatform`, replace this block (the `// Substitute the cross-platform {dst.HOME} marker -> $HOME on Unix targets` loop):

```go
		// Substitute the cross-platform {dst.HOME} marker -> $HOME on Unix targets
		// (install-mode paths here; run-mode commands are substituted in
		// BuildForUnixRun). The shell expands $HOME at runtime.
		for i := range cfg.Dirs {
			cfg.Dirs[i].Path = substituteHomeUnix(cfg.Dirs[i].Path)
		}
		for i := range cfg.SystemOverlay {
			cfg.SystemOverlay[i].System = substituteHomeUnix(cfg.SystemOverlay[i].System)
		}
```

with:

```go
		// Substitute cross-platform {dst.*} markers -> bash tokens on Unix targets
		// (install-mode paths here; run-mode commands are substituted in
		// BuildForUnixRun). The shell expands them at runtime. Windows-only
		// markers were rejected above, so only the cross-platform set remains.
		for i := range cfg.Dirs {
			cfg.Dirs[i].Path = installer.ReplaceCrossPlatformDst(cfg.Dirs[i].Path, goos)
		}
		for i := range cfg.SystemOverlay {
			cfg.SystemOverlay[i].System = installer.ReplaceCrossPlatformDst(cfg.SystemOverlay[i].System, goos)
		}
```

- [ ] **Step 4: Replace the run-mode command substitution**

In `cmd/onepkg/main.go`, inside `BuildForUnixRun`, replace the command-building loop body:

```go
	for _, c := range cfg.Commands {
		// cfg is already resolved for goos; collapse to a CommandSpec and
		// substitute {dst.HOME} -> $HOME in the effective strings.
		commands = append(commands, installer.CommandSpec{
			Exec:  substituteHomeUnix(c.Exec),
			Args:  substituteHomeUnixArgs(c.Args),
			Shell: substituteHomeUnix(c.Shell),
		})
	}
```

with:

```go
	for _, c := range cfg.Commands {
		// cfg is already resolved for goos; collapse to a CommandSpec and
		// substitute cross-platform {dst.*} markers -> bash tokens. (Exec is a
		// defensive no-op: the stray-field check forbids {dst.*} in exec.)
		commands = append(commands, installer.CommandSpec{
			Exec:  installer.ReplaceCrossPlatformDst(c.Exec, goos),
			Args:  replaceCrossPlatformDstArgs(c.Args, goos),
			Shell: installer.ReplaceCrossPlatformDst(c.Shell, goos),
		})
	}
```

- [ ] **Step 5: Replace the `substituteHome*` helper definitions**

In `cmd/onepkg/main.go`, replace the two helpers (`substituteHomeUnix` and `substituteHomeUnixArgs`):

```go
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

with the single args helper:

```go
// replaceCrossPlatformDstArgs applies installer.ReplaceCrossPlatformDst to each
// argument (run-mode commands). Exec/Shell are substituted directly at the
// call site; only the slice needs a helper.
func replaceCrossPlatformDstArgs(args []string, goos string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = installer.ReplaceCrossPlatformDst(a, goos)
	}
	return out
}
```

- [ ] **Step 6: Confirm `strings` is still imported and no dead references remain**

Run: `cd cmd/onepkg && grep -n "substituteHome" main.go`
Expected: no matches (both helpers removed). If `strings` becomes unused, the compiler will tell you; it is still used elsewhere in `main.go` (e.g. `strings.SplitN`), so it stays.

- [ ] **Step 7: Run the new tests to verify they pass**

Run: `cd cmd/onepkg && go test . -run 'TestBuildForPlatformAllowsCrossPlatformDstOnLinux|TestBuildForUnixRunLinuxCrossPlatformDst|TestBuildForPlatformWindowsCrossPlatformDstDirs' -v`
Expected: PASS.

- [ ] **Step 8: Run the full `package main` suite (regression)**

Run: `cd cmd/onepkg && go test . -v`
Expected: PASS — in particular `TestBuildForPlatformAllowsHomeOnLinux`, `TestBuildForUnixRunLinuxShellOnly`, and the Windows-only-rejection tests stay green.

- [ ] **Step 9: Commit**

```bash
git add cmd/onepkg/main.go cmd/onepkg/main_test.go
git commit -m "feat: substitute cross-platform {dst.*} markers in unix install/run .sh

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: Documentation — example config and CLAUDE.md

Document the four cross-platform markers and their per-OS resolution so authors can discover them.

**Files:**
- Modify: `docs/config-example.toml`
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: the final marker behavior from Tasks 1–3.

- [ ] **Step 1: Read the current marker example in `docs/config-example.toml`**

Run: `grep -n "dst\.\|env\." docs/config-example.toml`
to find the existing `{dst.*}` / `{env.*}` examples to extend consistently.

- [ ] **Step 2: Add a cross-platform `{dst.*}` example**

In `docs/config-example.toml`, add (near the existing `{dst.*}` example, as a run-mode or dirs example — match the surrounding section style):

```toml
# Cross-platform {dst.*} markers resolve on the TARGET at install/run time,
# identically spelled on Linux, macOS, and Windows:
#   {dst.HOME}      -> $HOME            (Linux/macOS)  /  $HOME            (Windows)
#   {dst.TMPDIR}    -> ${TMPDIR:-/tmp}  (Linux/macOS)  /  $env:TEMP        (Windows)
#   {dst.USER}      -> ${USER:-$(id -un)}               /  $env:USERNAME
#   {dst.HOSTNAME}  -> ${HOSTNAME:-$(hostname)}         /  $env:COMPUTERNAME
# Valid in dirs[].path, system_overlay[].system, and run commands[].args/.shell.
```

Place it as a commented block, or on an existing run/`dirs` example's `path`/`shell` line if one fits. Keep it consistent with how the existing `{dst.AppDataFolder}` example is presented.

- [ ] **Step 3: Update the CLAUDE.md marker-namespaces section**

In `CLAUDE.md`, find the `**Config marker namespaces:**` bullet that documents `{dst.NAME}`. Update the `{dst.*}` description to list the four cross-platform markers and note the Windows-only vocabulary is unchanged. Concretely, extend the existing `{dst.NAME}` bullet so it reads (preserve the surrounding text):

```
  - `{dst.NAME}` — destination variables, resolved on the TARGET at install/run time. Two classes:
    - **Cross-platform** (`HOME`, `TMPDIR`, `USER`, `HOSTNAME`): resolve on Linux, macOS, and Windows. `HOME`→`$HOME`; `TMPDIR`→`${TMPDIR:-/tmp}` / `$env:TEMP`; `USER`→`${USER:-$(id -un)}` / `$env:USERNAME`; `HOSTNAME`→`${HOSTNAME:-$(hostname)}` / `$env:COMPUTERNAME`.
    - **Windows-only** (the `KnownFolders` set: `AppDataFolder`, `ProgramFiles64Folder`, …): resolve via `[Environment]::GetFolderPath`; rejected on Linux/macOS by the build guard.
    Valid only in `dirs[].path`, `system_overlay[].system`, and run-mode `commands[].args`/`commands[].shell` (and their `_<os>` variants); anywhere else is a validation error. Curated names in `installer.crossPlatformDst` + `installer.KnownFolders`.
```

Also update the trailing note that currently singles out `{dst.HOME}` as "the one cross-platform `{dst.*}` marker" — change it to state there are now four cross-platform markers (`HOME`, `TMPDIR`, `USER`, `HOSTNAME`).

- [ ] **Step 4: Verify docs build/test unaffected**

Run: `cd cmd/onepkg && go build ./...`
Expected: succeeds (docs-only change; confirms no accidental code edit).

- [ ] **Step 5: Commit**

```bash
git add docs/config-example.toml CLAUDE.md
git commit -m "docs: cross-platform {dst.*} markers (HOME/TMPDIR/USER/HOSTNAME)

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Final Verification

- [ ] **Run the entire test suite the way CI does**

Run: `make test`
Expected: PASS (generates stubs, runs `package main` + `package installer` suites). All new tests green; all regression tests green.

- [ ] **Build the binary**

Run: `make build`
Expected: succeeds.

- [ ] **Sanity-check a generated package** (optional, manual)

Build a small run-mode package for linux using `{dst.TMPDIR}`/`{dst.USER}` in a `shell_linux` command, inspect the generated `.sh`, and confirm the tokens appear (e.g. `${TMPDIR:-/tmp}`) and no raw `{dst.` remains.

---

## Self-Review

**Spec coverage:**
- Four cross-platform markers + per-OS table → Task 1.
- Windows inline render (`EmitPathExpr`/`ReplaceMarkers`), `EmitKnownFolderDefs` skip → Task 2.
- Unix `.sh` substitution (install dirs/overlay + run exec/args/shell) → Task 3.
- Build guard unchanged (cross-platform names pass via not-Windows-only) → covered by Task 3 regression tests (AppDataFolder still rejected, HOME/TMPDIR allowed).
- Unknown `{dst.X}` still errors → Task 1 `TestCrossPlatformDstMarkersKnown` + existing `TestValidateKnownFolderMarkers`.
- Field allow-list unchanged → no task needed (existing stray-marker tests cover `{dst.*}` generically).
- `{env.VAR}` unchanged → no task needed.
- Docs → Task 4.
- No regression → Task 1/2/3 regression steps + Final Verification.

**Placeholder scan:** none — every code step shows the exact code; every test step shows the exact test.

**Type consistency:** `ReplaceCrossPlatformDst(s, goos string) string` and `crossPlatformDstToken(name, goos string) (string, bool)` are used identically across Tasks 1–3. `replaceCrossPlatformDstArgs(args []string, goos string) []string` matches its single call site in Task 3.

**Scope:** single focused feature; one plan is appropriate.
