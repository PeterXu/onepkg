package installer

import (
	"strings"
	"testing"
)

func TestKnownFoldersMap(t *testing.T) {
	if len(KnownFolders) == 0 {
		t.Fatal("KnownFolders map is empty")
	}
	cases := map[string]string{
		"AppDataFolder":        "ApplicationData",
		"LocalAppDataFolder":   "LocalApplicationData",
		"CommonAppDataFolder":  "CommonApplicationData",
		"ProgramFilesFolder":   "ProgramFilesX86",
		"ProgramFiles64Folder": "ProgramFiles",
		"WindowsFolder":        "Windows",
		"SystemFolder":         "System",
	}
	for name, enum := range cases {
		if got, ok := KnownFolders[name]; !ok {
			t.Errorf("KnownFolders missing %q", name)
		} else if got != enum {
			t.Errorf("KnownFolders[%q] = %q, want %q", name, got, enum)
		}
	}
}

func TestHasKnownFolderMarker(t *testing.T) {
	trues := []string{
		"{dst.AppDataFolder}\\x",
		"{dst.AppDataFolder}",
		"pre{dst.LocalAppDataFolder}\\post",
	}
	for _, s := range trues {
		if !HasKnownFolderMarker(s) {
			t.Errorf("HasKnownFolderMarker(%q) = false, want true", s)
		}
	}
	falses := []string{
		"C:\\plain\\path",
		"",                  // empty
		"{AppDataFolder}",   // bare form: no dst. prefix -> not matched
		"{dst.AppData}",     // dst. prefix but unknown name -> not a known marker
		"$AppDataFolder",    // dollar form is not a {dst.} marker
	}
	for _, s := range falses {
		if HasKnownFolderMarker(s) {
			t.Errorf("HasKnownFolderMarker(%q) = true, want false", s)
		}
	}
}

func TestValidateKnownFolderMarkers(t *testing.T) {
	if err := ValidateKnownFolderMarkers("{dst.AppDataFolder}\\MyApp"); err != nil {
		t.Errorf("known marker returned error: %v", err)
	}
	if err := ValidateKnownFolderMarkers("C:\\plain"); err != nil {
		t.Errorf("plain path returned error: %v", err)
	}
	if err := ValidateKnownFolderMarkers("{dst.AppData}"); err == nil {
		t.Error("unknown {dst.AppData} should return an error")
	} else if !strings.Contains(err.Error(), "AppData") {
		t.Errorf("error should name the unknown variable, got: %v", err)
	}
	if err := ValidateKnownFolderMarkers("{dst.AppDataFolder}{dst.BogusFolder}"); err == nil {
		t.Error("mixed known+unknown should return an error naming BogusFolder")
	} else if !strings.Contains(err.Error(), "BogusFolder") {
		t.Errorf("error should name the unknown variable, got: %v", err)
	}
}

func TestKnownFolderMarkersAndReplaceMarkers(t *testing.T) {
	got := KnownFolderMarkers("{dst.LocalAppDataFolder}\\x{dst.AppDataFolder}\\y{dst.LocalAppDataFolder}{dst.Unknown}")
	want := []string{"AppDataFolder", "LocalAppDataFolder"} // deduped + sorted, unknown excluded
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("KnownFolderMarkers = %v, want %v", got, want)
	}

	gotR := ReplaceMarkers(`pre{dst.AppDataFolder}\x{dst.LocalAppDataFolder}\y{dst.Unknown}`)
	wantR := `pre$AppDataFolder\x$LocalAppDataFolder\y{dst.Unknown}` // known -> $Name; unknown left intact
	if gotR != wantR {
		t.Errorf("ReplaceMarkers = %q, want %q", gotR, wantR)
	}
	if ReplaceMarkers("no markers here") != "no markers here" {
		t.Error("ReplaceMarkers should be a no-op on marker-free strings")
	}
}

func TestEmitKnownFolderDefs(t *testing.T) {
	out := EmitKnownFolderDefs([]string{"LocalAppDataFolder", "AppDataFolder", "AppDataFolder"})
	if !strings.Contains(out, "$AppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)\n") {
		t.Errorf("missing/sorted AppDataFolder def, got:\n%s", out)
	}
	if !strings.Contains(out, "$LocalAppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)\n") {
		t.Errorf("missing LocalAppDataFolder def, got:\n%s", out)
	}
	if strings.Count(out, "$AppDataFolder =") != 1 {
		t.Errorf("AppDataFolder def not deduped, got:\n%s", out)
	}
	if EmitKnownFolderDefs(nil) != "" {
		t.Error("EmitKnownFolderDefs(nil) should be empty")
	}
}

func TestEmitPathExpr(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\plain\path`, `'C:\plain\path'`},                                                              // bare -> single-quoted literal
		{`{dst.AppDataFolder}\MyApp\data`, `$AppDataFolder + '\MyApp\data'`},                              // marker at start
		{`{dst.AppDataFolder}`, `$AppDataFolder`},                                                         // marker only
		{`pre\{dst.LocalAppDataFolder}\post`, `'pre\' + $LocalAppDataFolder + '\post'`},                   // marker in middle
		{`{dst.AppDataFolder}\{dst.LocalAppDataFolder}\x`, `$AppDataFolder + '\' + $LocalAppDataFolder + '\x'`}, // multiple
	}
	for _, c := range cases {
		got := EmitPathExpr(c.in)
		if got != c.want {
			t.Errorf("EmitPathExpr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

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
	out := EmitKnownFolderDefs([]string{"HOME", "AppDataFolder"})
	if strings.Contains(out, "$HOME =") {
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
