# Run-Mode Results Dialog — Design Spec

**Date:** 2026-07-14
**Status:** Approved
**Scope:** Windows `mode = "run"` packages. After running all commands, show a native Windows `MessageBox` summarizing each command's result (success/failure + exit code) and a tally, and set the package exit code to reflect overall pass/fail. Replaces today's fail-fast, console-only, scrollback-buried feedback.

## Motivation

Today a run-mode `.exe` (the stub spawns `powershell.exe`) prints feedback only to the transient console (`installer/powershell_runner.go` → generated `run.ps1`):

- `Running N command(s) for <name> <version>`
- per command: `Running: <exec> <args…>` (or the shell string)
- on the **first** failure: `Command failed with exit code <N>` (exec) / `Command failed` (shell), then `Read-Host 'Press Enter to exit'`, then `exit 1`
- on full success: `Completed N command(s)`, then `Read-Host 'Press Enter to exit'`

Three problems:

1. **No per-command result.** There is no `[OK] cmd 1` marker; you infer command *N* succeeded only because command *N+1*'s `Running:` line appears. Blink and you miss it.
2. **Fail-fast hides later commands.** Execution stops at the first failure, so later commands' status is never shown — you cannot see "each command and its result."
3. **Overall verdict is buried.** Success/failure is conveyed only by the presence/absence of a `Command failed…` line deep in the console scrollback; there is no consolidated, at-a-glance summary.

A results dialog addresses all three: run every command, mark each, and surface a clear summary popup.

## Requirements

- **Run all commands.** Execution continues past a failing command; every command runs and is recorded. (Replaces fail-fast.)
- **Per-command result, captured.** Each command records a result line: `[OK] <display>` or `[FAIL] <display> (exit <N>)`, where `<display>` is the exec+args or the shell string shown on the existing `Running:` line.
- **Summary MessageBox at the end.** After all commands, show a native `[System.Windows.Forms.MessageBox]::Show(...)` with:
  - title: `<name> <version> — run results`
  - body: the per-command result lines, then a blank line, then `N of M commands succeeded.`
  - a single **OK** button
  - icon: `Information` if all succeeded, `Warning` if any failed
- **Exit code reflects overall result.** The `.exe`/`run.ps1` exits `0` if all commands succeeded, `1` if any failed — machine-detectable in addition to the dialog.
- **Console keeps real-time detail.** The existing per-command `Write-Host 'Running: …'` lines and the commands' own stdout/stderr still go to the console. The dialog is the consolidated summary.
- **Dialog is the acknowledgment.** The old per-failure and final `Read-Host 'Press Enter to exit'` prompts are removed; the modal dialog is the single user acknowledgment.

## Non-Goals

- **Capturing command stdout/stderr into the dialog.** Command output stays in the console; the dialog shows status + exit code only. (Avoids per-command output redirection/truncation complexity.)
- **A `quiet` / `no_dialog` flag for unattended/CI use.** A mandatory modal dialog is *more* blocking than today's `Read-Host`, so unattended use still requires a future quiet option. The "interactive-only" stance is unchanged; only the docs note is updated.
- **Per-package opt-out** of the dialog (no `show_dialog` config flag).
- **A sortable GUI table** (Out-GridView) — rejected during design: availability varies (absent on Server Core; needs a module in PowerShell 7) and it must be closed manually. MessageBox (`System.Windows.Forms`) is always present in Windows PowerShell 5.1, which the stub's `powershell` invocation uses.
- **Changes to install mode.** Install mode (`mode = "install"`) is unaffected.

## Design

### Encoding: ASCII markers, not glyphs

The dialog uses ASCII `[OK]` / `[FAIL]` markers, **not** ✓/✗. The generated `run.ps1` is embedded as UTF-8 *without* a byte-order mark (the stub writes the payload bytes verbatim via `os.WriteFile`), and Windows PowerShell 5.1 (the `powershell` the stub invokes) interprets a BOM-less file as the system ANSI codepage — so non-ASCII bytes (✓/✗) would be misread. ASCII avoids the pitfall entirely with no encoding changes to the generator. (This is why the approved mockup's ✓/✗ become `[OK]`/`[FAIL]` in the implementation.)

### PowerShell mechanism (`installer/powershell_runner.go`)

**Per-command block** (`runnerCommandBlock`), restructured from fail-fast to record-result:

```powershell
Write-Host 'Running:' '<display tokens>'   # unchanged real-time progress
$LASTEXITCODE = 0
& (Join-Path $PayloadDir '<exec>') <args>   # or: Invoke-Expression '<shell>'
$rc = $LASTEXITCODE
if ((-not $?) -or ($rc -ne 0)) {
    $results += '[FAIL] <display> (exit ' + $rc + ')'
    $failures++
} else {
    $results += '[OK]  <display>'
}
```

`<display>` is the same label used for the `Running:` line: exec followed by its args (space-joined), or the shell string. The `exit 1` and the mid-loop `Read-Host 'Press Enter to exit'` are removed.

**After the command loop** (`GeneratePowerShellRunner`), before what is currently the final `Write-Host 'Completed …'` + `Read-Host`:

```powershell
Add-Type -AssemblyName System.Windows.Forms
$body = ($results -join "`r`n")
$tally = ('{0} of {1} commands succeeded.' -f (($results.Count) - $failures), $results.Count)
$message = $body + "`r`n`r`n" + $tally
if ($failures -eq 0) {
    $icon = [System.Windows.Forms.MessageBoxIcon]::Information
} else {
    $icon = [System.Windows.Forms.MessageBoxIcon]::Warning
}
[System.Windows.Forms.MessageBox]::Show($message, '<name> <version> — run results', [System.Windows.Forms.MessageBoxButtons]::OK, $icon) | Out-Null
if ($failures -gt 0) { exit 1 } else { exit 0 }
```

The final `Read-Host 'Press Enter to exit'` is removed (the modal dialog is the acknowledgment). `$results` and `$failures` are initialized to `@()` and `0` before the loop.

`Add-Type -AssemblyName System.Windows.Forms` is emitted unconditionally (the dialog is always shown). `System.Windows.Forms` is always present in Windows PowerShell 5.1, which the stub's `powershell -ExecutionPolicy Bypass -File …` invocation targets.

### Console vs. dialog

The console window (allocated by Windows for the stub-spawned `powershell.exe`) still shows each `Running: …` line and the commands' own stdout/stderr in real time — useful for debugging. The MessageBox appears at the end, on top, as the consolidated result. After the user clicks OK, `run.ps1` exits and the console closes.

### Exit code

`run.ps1` exits `1` if `$failures -gt 0`, else `0`. The stub's `cmd.Run()` surfaces PowerShell's exit code, so the self-extracting `.exe`'s exit code reflects overall pass/fail — detectable by callers/scripts in addition to the human-facing dialog.

### Generator structure

`GeneratePowerShellRunner` initializes `$results = @()` and `$failures = 0` before the command loop, emits the restructured per-command blocks (results-recording, no fail-fast), then emits the `System.Windows.Forms` summary block and the conditional `exit`. `runnerCommandBlock` returns the restructured block. The `$results`/`$failures` variable names are internal to the generated script.

## Backward Compatibility

- **Intentionally breaking (run mode only, local/unreleased):** the generated `run.ps1` changes shape — fail-fast becomes run-all, and a modal dialog is always shown. Existing `powershell_runner_test.go` assertions that hard-code the old `Read-Host 'Press Enter to exit'; exit 1` form are updated as part of this change.
- **Unmarked/other behavior unchanged:** the per-command `Running:` line, `require_admin`/`CheckAdmin`, `CheckArch`, the `$PayloadDir`/`Expand-Archive` payload handling, and known-folder (`{dst.NAME}`) resolution are untouched. Install mode is entirely untouched.
- **No config changes.** Authors write `mode = "run"` and `[[commands]]` exactly as today; the dialog is automatic.

## Testing Strategy

Generated `.ps1` cannot run on macOS, so tests verify generated text in `installer/powershell_runner_test.go` (the existing pattern for this generator):

- **Run-all structure:** a config with multiple commands produces a `$results`/`$failures` init, per-command blocks that record `[OK]`/`[FAIL]` and bump `$failures` **without** a mid-loop `exit` or `Read-Host`.
- **Dialog emission:** the generated script contains `Add-Type -AssemblyName System.Windows.Forms`, a `[System.Windows.Forms.MessageBox]::Show(` call whose arguments include the `<name> <version> — run results` title and the `… of … commands succeeded.` tally, the `OK` button, and the `Information`/`Warning` icon selection on `$failures`.
- **Exit code:** the script ends with `if ($failures -gt 0) { exit 1 } else { exit 0 }` and **no** `Read-Host 'Press Enter to exit'`.
- **Per-command invocations preserved:** exec commands still render `& (Join-Path $PayloadDir '<exec>') <args>`; shell commands still render `Invoke-Expression '<shell>'` (existing assertions retained/updated).
- **Display label:** the `[OK]`/`[FAIL]` line uses the same exec+args or shell tokens as the `Running:` line.
- Existing tests that asserted the old fail-fast `Read-Host … exit 1` form are rewritten to the results-recording form.

## Files to Create / Modify

| File | Change |
|------|--------|
| `cmd/onepkg/installer/powershell_runner.go` | Restructure `runnerCommandBlock` to record results (no fail-fast); in `GeneratePowerShellRunner`, init `$results`/`$failures`, emit the `System.Windows.Forms` MessageBox summary block and conditional `exit`, remove the final `Read-Host`. |
| `cmd/onepkg/installer/powershell_runner_test.go` | Update existing fail-fast assertions; add run-all structure, dialog-emission, exit-code, and removal-of-Read-Host assertions. |
| `docs/config-example.toml` | Update the `mode = "run"` comment: results shown via a MessageBox dialog (not a `Read-Host` console prompt); still interactive-only / not for unattended use. |
| `CLAUDE.md` | Update the run-mode note to reflect the results dialog and run-all behavior. |

## Open Questions

None. Display (MessageBox), failure mode (run-all, mark each), markers (ASCII `[OK]`/`[FAIL]`), and acknowledgment (dialog replaces `Read-Host`) are settled by this design.
