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
		"{AppDataFolder}\\x",
		"{AppDataFolder}",
		"pre{LocalAppDataFolder}\\post",
	}
	for _, s := range trues {
		if !HasKnownFolderMarker(s) {
			t.Errorf("HasKnownFolderMarker(%q) = false, want true", s)
		}
	}
	falses := []string{
		"C:\\plain\\path",
		"",               // empty
		"{AppData}",      // unknown name -> not a known marker
		"$AppDataFolder", // dollar form is not a {marker}
	}
	for _, s := range falses {
		if HasKnownFolderMarker(s) {
			t.Errorf("HasKnownFolderMarker(%q) = true, want false", s)
		}
	}
}

func TestValidateKnownFolderMarkers(t *testing.T) {
	if err := ValidateKnownFolderMarkers("{AppDataFolder}\\MyApp"); err != nil {
		t.Errorf("known marker returned error: %v", err)
	}
	if err := ValidateKnownFolderMarkers("C:\\plain"); err != nil {
		t.Errorf("plain path returned error: %v", err)
	}
	if err := ValidateKnownFolderMarkers("{AppData}"); err == nil {
		t.Error("unknown {AppData} should return an error")
	}
	if err := ValidateKnownFolderMarkers("{AppDataFolder}{BogusFolder}"); err == nil {
		t.Error("mixed known+unknown should return an error naming BogusFolder")
	} else if !strings.Contains(err.Error(), "BogusFolder") {
		t.Errorf("error should name the unknown variable, got: %v", err)
	}
}

func TestKnownFolderMarkersAndDollarVars(t *testing.T) {
	got := KnownFolderMarkers("{LocalAppDataFolder}\\x{AppDataFolder}\\y{LocalAppDataFolder}{Unknown}")
	want := []string{"AppDataFolder", "LocalAppDataFolder"} // deduped + sorted, unknown excluded
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("KnownFolderMarkers = %v, want %v", got, want)
	}

	gotD := KnownFolderDollarVars("$AppDataFolder and $LocalAppDataFolder again $AppDataFolder and $Nope")
	wantD := []string{"AppDataFolder", "LocalAppDataFolder"}
	if len(gotD) != 2 || gotD[0] != wantD[0] || gotD[1] != wantD[1] {
		t.Errorf("KnownFolderDollarVars = %v, want %v", gotD, wantD)
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
	// AppDataFolder appears once (deduped).
	if strings.Count(out, "$AppDataFolder =") != 1 {
		t.Errorf("AppDataFolder def not deduped, got:\n%s", out)
	}
	if EmitKnownFolderDefs(nil) != "" {
		t.Error("EmitKnownFolderDefs(nil) should be empty")
	}
}

func TestEmitPathExpr(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\plain\path`, `'C:\plain\path'`},                                                            // bare -> single-quoted literal
		{`{AppDataFolder}\MyApp\data`, `$AppDataFolder + '\MyApp\data'`},                                // marker at start
		{`{AppDataFolder}`, `$AppDataFolder`},                                                           // marker only
		{`pre\{LocalAppDataFolder}\post`, `'pre\' + $LocalAppDataFolder + '\post'`},                     // marker in middle
		{`{AppDataFolder}\{LocalAppDataFolder}\x`, `$AppDataFolder + '\' + $LocalAppDataFolder + '\x'`}, // multiple
	}
	for _, c := range cases {
		got := EmitPathExpr(c.in)
		if got != c.want {
			t.Errorf("EmitPathExpr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
