# Run-Mode Results Dialog — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Windows `mode = "run"` packages run all commands, then show a native `MessageBox` summarizing each command's result and a tally, exiting `0`/`1` by overall result.

**Architecture:** All logic is in the run-mode generator `installer/powershell_runner.go`. `runnerCommandBlock` stops fail-fast and instead records each command's `[OK]`/`[FAIL]` result into a `$results` array (bumping `$failures`). After the loop, `GeneratePowerShellRunner` emits a `System.Windows.Forms.MessageBox` summary (body = results + tally, icon = Information/Warning by `$failures`) and a conditional `exit 0`/`exit 1`, replacing the old `Read-Host 'Press Enter to exit'`. Newlines use `[Environment]::NewLine` to avoid PowerShell backtick escaping in Go source.

**Tech Stack:** Go 1.21, module `onepkg`, package `installer` in `cmd/onepkg/installer/`. Generated target language: Windows PowerShell 5.1 (the stub invokes `powershell`). No new Go dependencies.

## Global Constraints

Copied verbatim from the approved spec (`docs/superpowers/specs/2026-07-14-run-mode-results-dialog-design.md`):

- Run mode **only**. Install mode (`mode = "install"`) is entirely untouched.
- **Run all** commands (no fail-fast); record each; exit `1` if any failed, else `0`.
- Per-command result uses **ASCII** markers `[OK]`/`[FAIL]` (two spaces after `[OK]`, one after `[FAIL]`), **not** glyphs. The `MessageBox` title uses an ASCII hyphen `-`, not an em-dash. Reason: `run.ps1` is embedded BOM-less UTF-8 and Windows PowerShell 5.1 reads it as the system ANSI codepage, which would misread non-ASCII bytes.
- Dialog via `[System.Windows.Forms.MessageBox]::Show(...)` after `Add-Type -AssemblyName System.Windows.Forms` (`System.Windows.Forms` is always present in Windows PowerShell 5.1). Icon = `Information` if `$failures -eq 0`, else `Warning`. One `OK` button.
- **Remove** the per-failure and final `Read-Host 'Press Enter to exit'`; the modal dialog is the single acknowledgment.
- **Preserve** the per-command `Write-Host 'Running:' …` console lines and the commands' own stdout/stderr (real-time detail). Preserve `require_admin`/`CheckAdmin`, `CheckArch`, `Expand-Archive`, `$PayloadDir`, and `{dst.NAME}` resolution.
- Generated `.ps1` cannot run on macOS → tests assert generated **text** (`installer/powershell_runner_test.go`), the existing pattern.
- Do not modify `build-package/` (symlink to another project).
- TDD: update tests to the new behavior first, confirm they fail, implement, confirm they pass, commit.
- Windows stubs (`cmd/onepkg/installer/stubs/*.exe`) are gitignored and required by `//go:embed`; run `make build` once before tests, or `make test` (regenerates stubs then runs both suites).

---

## File Structure

| File | Responsibility | Touched by |
|------|----------------|------------|
| `cmd/onepkg/installer/powershell_runner.go` | Restructure `runnerCommandBlock` (record results, no fail-fast); add `commandLabel`; in `GeneratePowerShellRunner`, init `$results`/`$failures`, emit MessageBox summary + conditional `exit`, remove final `Read-Host`. | Task 1 |
| `cmd/onepkg/installer/powershell_runner_test.go` | Update `TestGeneratePowerShellRunner` assertions to the run-all/dialog behavior. | Task 1 |
| `docs/config-example.toml` | Update the `mode = "run"` comment block. | Task 2 |
| `CLAUDE.md` | Update the run-mode note. | Task 2 |

---

## Task 1: Run-all + MessageBox results in `powershell_runner.go`

**Files:**
- Modify: `cmd/onepkg/installer/powershell_runner.go` — rewrite `runnerCommandBlock` (currently lines 102–130), add `commandLabel`, and rewrite the tail of `GeneratePowerShellRunner` (currently the `Write-Host 'Running …'` banner through the final `Read-Host`, lines 77–87).
- Test: `cmd/onepkg/installer/powershell_runner_test.go` — update `TestGeneratePowerShellRunner` (lines 8–51).

**Interfaces:**
- Consumes: existing `CommandSpec` (`Exec`, `Args`, `Shell`), `PowerShellRunnerConfig` (`Name`, `Version`, `Commands`, `RequireAdmin`, `HasPayload`, `ExpectedArch`, `Platform`), and helpers `psQuote`, `argPS`, `ReplaceMarkers`, `EmitKnownFolderDefs`, `KnownFolderMarkers`, `powershellArchVars`, `powershellCheckAdmin`, `powershellCheckArch`. All already defined in package `installer`.
- Produces: new helper `func commandLabel(cmd CommandSpec) string`. No signature changes to existing exported functions.

- [ ] **Step 1: Update the failing test** in `cmd/onepkg/installer/powershell_runner_test.go`

Replace the entire body of `TestGeneratePowerShellRunner` (lines 8–51) with:

```go
func TestGeneratePowerShellRunner(t *testing.T) {
	cfg := PowerShellRunnerConfig{
		Name:         "cleanup",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		RequireAdmin: true,
		HasPayload:   true,
		Commands: []CommandSpec{
			{Exec: "bin/cleanup", Args: []string{"--all", "--force"}},
			{Shell: "netsh advfirewall set allprofiles state off"},
		},
	}

	out, err := GeneratePowerShellRunner(cfg)
	if err != nil {
		t.Fatalf("GeneratePowerShellRunner: %v", err)
	}

	mustContain := []string{
		"param(",
		"[string]$PayloadDir",
		"[string]$InstallDir", // declared for stub parity, ignored
		"[string]$Action",     // declared for stub parity, ignored
		"$EXPECTED_ARCH = 'AMD64'",
		"$ALLOWED_ARCHS = @('AMD64', 'ARM64')",
		"function CheckArch",
		"function CheckAdmin", // RequireAdmin = true
		"Expand-Archive",      // HasPayload = true
		"& (Join-Path $PayloadDir 'bin/cleanup')", // exec invocation preserved
		"'--all'",
		"'--force'",
		"Invoke-Expression 'netsh advfirewall set allprofiles state off'", // shell invocation preserved
		"$LASTEXITCODE = 0",                   // still reset before each command
		"(-not $?) -or ($LASTEXITCODE -ne 0)", // condition reused to record result
		"$results = @()",                      // run-all result accumulator
		"$failures = 0",
		"$results += '[OK]  '",  // success recorded (ASCII, two trailing spaces)
		"$results += '[FAIL] '", // failure recorded (ASCII, one trailing space)
		"$failures++",
		"Add-Type -AssemblyName System.Windows.Forms",
		"[System.Windows.Forms.MessageBox]::Show(",
		"'cleanup v1.0.0 - run results'",                 // title (ASCII hyphen, name+version)
		"'{0} of {1} commands succeeded.'",               // tally
		"[System.Windows.Forms.MessageBoxIcon]::Information", // icon when all ok
		"[System.Windows.Forms.MessageBoxIcon]::Warning",     // icon when any failed
		"if ($failures -gt 0) { exit 1 } else { exit 0 }", // exit code by overall result
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q", s)
		}
	}

	mustNotContain := []string{
		"Read-Host 'Press Enter to exit'", // removed; the modal dialog is the acknowledgment
		"Write-Host 'Completed ",          // replaced by the MessageBox tally
	}
	for _, s := range mustNotContain {
		if strings.Contains(out, s) {
			t.Errorf("output should not contain %q", s)
		}
	}
}
```

Leave the other test functions (`TestGeneratePowerShellRunnerConditionalBlocks`, `TestGeneratePowerShellRunnerIsLean`, `TestGeneratePowerShellRunnerKnownFolders`, `TestGeneratePowerShellRunnerNoKnownFolderDefsWhenUnused`) unchanged — they assert invocation forms and conditional blocks that this change preserves.

- [ ] **Step 2: Run the test to verify it fails**

First ensure stubs exist: `make build` (from repo root, once).
Run: `cd cmd/onepkg/installer && go test -run TestGeneratePowerShellRunner -v`
Expected: FAIL — missing `Add-Type -AssemblyName System.Windows.Forms`, `$results = @()`, `[System.Windows.Forms.MessageBox]::Show(`, etc.; and `Read-Host 'Press Enter to exit'` / `Write-Host 'Completed ` still present (the mustNotContain assertions fire).

- [ ] **Step 3: Add `commandLabel` and rewrite `runnerCommandBlock`**

In `cmd/onepkg/installer/powershell_runner.go`, replace the entire current `runnerCommandBlock` function (the version whose exec branch ends with `… Read-Host 'Press Enter to exit'; exit 1 }\n"` and whose shell branch ends similarly) with:

```go
// commandLabel returns the human-readable form of a command for the results
// dialog: exec plus its args space-joined, or the shell expression.
func commandLabel(cmd CommandSpec) string {
	if cmd.Exec != "" {
		parts := append([]string{cmd.Exec}, cmd.Args...)
		return strings.Join(parts, " ")
	}
	return cmd.Shell
}

// runnerCommandBlock renders one command's real-time log line, invocation, and
// result recording. Execution is run-all: a failure is recorded into $results
// and bumps $failures instead of aborting.
func runnerCommandBlock(cmd CommandSpec) string {
	var b strings.Builder
	label := psQuote(commandLabel(cmd))
	if cmd.Exec != "" {
		// Log: Write-Host 'Running:' '<exec>' '<arg>' ...  (all single-quoted via psQuote)
		b.WriteString("    Write-Host 'Running:'")
		b.WriteString(" " + psQuote(cmd.Exec))
		for _, a := range cmd.Args {
			b.WriteString(" " + argPS(a))
		}
		b.WriteString("\n")

		b.WriteString("    $LASTEXITCODE = 0\n")
		b.WriteString("    & (Join-Path $PayloadDir " + psQuote(cmd.Exec) + ")")
		for _, a := range cmd.Args {
			b.WriteString(" " + argPS(a))
		}
		b.WriteString("\n")
	} else {
		b.WriteString("    Write-Host 'Running:' " + psQuote(ReplaceMarkers(cmd.Shell)) + "\n")
		b.WriteString("    $LASTEXITCODE = 0\n")
		b.WriteString("    Invoke-Expression " + psQuote(ReplaceMarkers(cmd.Shell)) + "\n")
	}
	// Record result (run-all, no fail-fast). Exit code is shown only when non-zero,
	// so a failed cmdlet (which leaves $LASTEXITCODE at 0) does not read "(exit 0)".
	b.WriteString("    if ((-not $?) -or ($LASTEXITCODE -ne 0)) {\n")
	b.WriteString("        $results += '[FAIL] ' + " + label + " + $(if ($LASTEXITCODE -ne 0) { ' (exit ' + $LASTEXITCODE + ')' })\n")
	b.WriteString("        $failures++\n")
	b.WriteString("    } else {\n")
	b.WriteString("        $results += '[OK]  ' + " + label + "\n")
	b.WriteString("    }\n")
	return b.String()
}
```

- [ ] **Step 4: Rewrite the tail of `GeneratePowerShellRunner`**

In `cmd/onepkg/installer/powershell_runner.go`, find this block (currently near the end of `GeneratePowerShellRunner`, after the `try {` / `Expand-Archive` lines):

```go
	fmt.Fprintf(&b, "    Write-Host 'Running %d command(s) for %s %s'\n", len(cfg.Commands), cfg.Name, cfg.Version)
	for _, cmd := range cfg.Commands {
		b.WriteString(runnerCommandBlock(cmd))
	}
	fmt.Fprintf(&b, "    Write-Host 'Completed %d command(s)'\n", len(cfg.Commands))
	b.WriteString("} finally {\n")
	b.WriteString("    Pop-Location\n")
	b.WriteString("}\n")
	b.WriteString("Read-Host 'Press Enter to exit'\n")

	return b.String(), nil
```

Replace it with:

```go
	b.WriteString("    $results = @()\n")
	b.WriteString("    $failures = 0\n")
	fmt.Fprintf(&b, "    Write-Host 'Running %d command(s) for %s %s'\n", len(cfg.Commands), cfg.Name, cfg.Version)
	for _, cmd := range cfg.Commands {
		b.WriteString(runnerCommandBlock(cmd))
	}
	// Results dialog: one line per command + tally. ASCII markers and an ASCII
	// hyphen in the title — run.ps1 is BOM-less UTF-8 and PowerShell 5.1 would
	// misread non-ASCII bytes. [Environment]::NewLine avoids backtick escaping.
	b.WriteString("    Add-Type -AssemblyName System.Windows.Forms\n")
	b.WriteString("    $nl = [Environment]::NewLine\n")
	b.WriteString("    $tally = ('{0} of {1} commands succeeded.' -f ($results.Count - $failures), $results.Count)\n")
	b.WriteString("    $message = ($results -join $nl) + $nl + $nl + $tally\n")
	title := psQuote(cfg.Name + " " + cfg.Version + " - run results")
	b.WriteString("    if ($failures -eq 0) {\n")
	b.WriteString("        $icon = [System.Windows.Forms.MessageBoxIcon]::Information\n")
	b.WriteString("    } else {\n")
	b.WriteString("        $icon = [System.Windows.Forms.MessageBoxIcon]::Warning\n")
	b.WriteString("    }\n")
	b.WriteString("    [System.Windows.Forms.MessageBox]::Show($message, " + title + ", [System.Windows.Forms.MessageBoxButtons]::OK, $icon) | Out-Null\n")
	b.WriteString("    if ($failures -gt 0) { exit 1 } else { exit 0 }\n")
	b.WriteString("} finally {\n")
	b.WriteString("    Pop-Location\n")
	b.WriteString("}\n")

	return b.String(), nil
```

The `$results = @()` / `$failures = 0` init must land inside the `try {` block, before the command loop, so the per-command blocks can append to `$results`. (The `try {` and `Push-Location`/`Expand-Archive` lines above are unchanged.)

- [ ] **Step 5: Run the focused test to verify it passes**

Run: `cd cmd/onepkg/installer && go test -run TestGeneratePowerShellRunner -v`
Expected: PASS.

- [ ] **Step 6: Run the full suite to confirm no regressions**

Run: `make test` (from repo root; regenerates stubs, runs `onepkg` + `installer` + `stub` suites).
Expected: PASS — the other `powershell_runner_test.go` tests (`ConditionalBlocks`, `IsLean`, `KnownFolders`, `NoKnownFolderDefsWhenUnused`) still pass (they assert invocation/conditional forms that this change preserves), and all unrelated tests are green.

- [ ] **Step 7: Commit**

```bash
git add cmd/onepkg/installer/powershell_runner.go cmd/onepkg/installer/powershell_runner_test.go
git commit -m "feat(run): show MessageBox results summary, run all commands

Run-mode packages run every command (no fail-fast), record each result,
then show a Windows MessageBox summarizing [OK]/[FAIL] + exit code and a
tally, exiting 0/1 by overall result. Replaces the Read-Host console prompt."
```

---

## Task 2: Document the run-mode results dialog

**Files:**
- Modify: `docs/config-example.toml` — the `mode = "run"` comment block (currently around lines 139–145).
- Modify: `CLAUDE.md` — the run-mode bullet in "Key Conventions".

**Interfaces:** None (documentation only).

- [ ] **Step 1: Update the `mode = "run"` comment in `docs/config-example.toml`**

Find this block (currently around lines 139–145):

```toml
# --- mode = "run": one-shot command/script package (Windows-only) ---
# Double-clicking the .exe runs the commands once in order, then exits.
# No install/uninstall, no service, no permanent files. Items are optional
# (omit them for shell-only packages).
# NOTE: run packages are interactive-only today - they wait on 'Read-Host' before exiting,
# so they are not suited to scheduled/CI/unattended use (a future 'quiet'/'no_pause' option may change this).
```

Replace it with:

```toml
# --- mode = "run": one-shot command/script package (Windows-only) ---
# Double-clicking the .exe runs ALL commands (continuing past failures), then
# shows a Windows MessageBox summarizing each command's result ([OK]/[FAIL] +
# exit code) and a tally, and exits 0 (all ok) or 1 (any failed).
# No install/uninstall, no service, no permanent files. Items are optional
# (omit them for shell-only packages).
# NOTE: run packages are interactive-only - the results MessageBox is modal, so
# they are not suited to scheduled/CI/unattended use (a future 'quiet' option may change this).
```

- [ ] **Step 2: Update the run-mode bullet in `CLAUDE.md`**

Find the run-mode bullet in the "Key Conventions" section (it begins `- **Run mode (`mode = "run"`):**`). Replace the sentence `[[commands]]` entries (each exactly one of `exec`+`args` for a bundled file, or `shell` for arbitrary PowerShell) run once in order; no install/service/`VERSION`.` with:

```
`[[commands]]` entries (each exactly one of `exec`+`args` for a bundled file, or `shell` for arbitrary PowerShell) run all (continuing past failures), then a MessageBox summarizes each command's [OK]/[FAIL] result and the package exits 0/1; no install/service/`VERSION`.
```

Leave the rest of that bullet unchanged (`require_admin` gates; `items` optional; install mode rejects `[[commands]]`; run mode rejects `services`/`dirs`/`system_overlay`; run packages reuse the unchanged exe stub).

- [ ] **Step 3: Commit**

```bash
git add docs/config-example.toml CLAUDE.md
git commit -m "docs: document run-mode results MessageBox and run-all behavior"
```

---

## Self-Review

**Spec coverage:** Every spec requirement maps to a task step.
- "Run all commands" → Task 1 Step 3 (result-recording block, no `exit` mid-loop). ✓
- "Per-command result captured `[OK]`/`[FAIL]` + exit code" → Task 1 Step 3 (`$results += ...`) and `commandLabel`. ✓
- "Summary MessageBox: title/body/tally/icon/OK button" → Task 1 Step 4 (`Add-Type`, `$message`, `$tally`, `[System.Windows.Forms.MessageBox]::Show(...)`, Information/Warning icon). ✓
- "Exit code 0/1 by overall result" → Task 1 Step 4 (`if ($failures -gt 0) { exit 1 } else { exit 0 }`). ✓
- "Console keeps real-time detail" → Task 1 Step 3 preserves `Write-Host 'Running:' …` and the `&`/`Invoke-Expression` invocations. ✓
- "Dialog is the acknowledgment; remove Read-Host" → Task 1 Step 4 removes the final `Read-Host`; Step 3 removes the per-failure ones; test asserts absence (Step 1 `mustNotContain`). ✓
- ASCII markers / ASCII hyphen / no glyphs → Task 1 Step 3 (`[OK]  `/`[FAIL] `) and Step 4 (` - run results`), with the constraint documented inline. ✓
- Docs → Task 2. ✓

**Placeholder scan:** No TBD/TODO/“add error handling”/“similar to Task N”. Every code step contains full code (old → new). ✓

**Type consistency:** `commandLabel(cmd CommandSpec) string` matches its use in `runnerCommandBlock`. `$results`/`$failures` are initialized in Step 4 (`$results = @()`, `$failures = 0`) before the loop emits the Step-3 blocks that append to them — the init lands inside `try {` above the loop, and the per-command blocks reference the same names. `psQuote`, `argPS`, `ReplaceMarkers`, `cfg.Name`/`cfg.Version`/`cfg.Commands` match the existing generator. The test's `mustContain` strings (`[OK]  ` two spaces, `[FAIL] ` one space, `'cleanup v1.0.0 - run results'`) match the code Step 3/4 emit exactly. ✓
