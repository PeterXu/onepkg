package installer

import (
	"strings"
	"testing"
)

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
		"& (Join-Path $PayloadDir 'bin/cleanup')",
		"'--all'",
		"'--force'",
		"Invoke-Expression 'netsh advfirewall set allprofiles state off'",
		"$LASTEXITCODE = 0",                   // shell reset before Invoke-Expression
		"(-not $?) -or ($LASTEXITCODE -ne 0)", // fail-fast condition for both exec and shell
		"exit 1",                              // fail-fast present
		"Read-Host 'Press Enter to exit'",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q", s)
		}
	}
}

func TestGeneratePowerShellRunnerConditionalBlocks(t *testing.T) {
	// Shell-only package: no payload, no admin.
	cfg := PowerShellRunnerConfig{
		Name:         "shellonly",
		Version:      "v1.0.0",
		Platform:     "windows-arm64",
		ExpectedArch: "ARM64",
		RequireAdmin: false,
		HasPayload:   false,
		Commands:     []CommandSpec{{Shell: "Write-Host hi"}},
	}
	out, err := GeneratePowerShellRunner(cfg)
	if err != nil {
		t.Fatalf("GeneratePowerShellRunner: %v", err)
	}

	if strings.Contains(out, "function CheckAdmin") {
		t.Error("CheckAdmin should be omitted when RequireAdmin is false")
	}
	if strings.Contains(out, "Expand-Archive") {
		t.Error("Expand-Archive should be omitted when HasPayload is false")
	}
	if !strings.Contains(out, "$ALLOWED_ARCHS = @('ARM64')") {
		t.Error("arm64 package should allow only ARM64")
	}
}

func TestGeneratePowerShellRunnerIsLean(t *testing.T) {
	cfg := PowerShellRunnerConfig{
		Name: "x", Version: "v1", Platform: "windows-amd64", ExpectedArch: "AMD64",
		Commands: []CommandSpec{{Shell: "Write-Host hi"}},
	}
	out, _ := GeneratePowerShellRunner(cfg)
	// Must not carry install-only machinery.
	for _, bad := range []string{"New-Service", "switch ($Action)", "function Install", "function Uninstall"} {
		if strings.Contains(out, bad) {
			t.Errorf("runner output should not contain install-only %q", bad)
		}
	}
}

func TestGeneratePowerShellRunnerKnownFolders(t *testing.T) {
	cfg := PowerShellRunnerConfig{
		Name:         "kf",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		Commands: []CommandSpec{
			{Exec: "bin/app.exe", Args: []string{"--config", `{LocalAppDataFolder}\MyApp\cfg.json`}},
			{Shell: `Copy-Item profile.json "$AppDataFolder\MyApp\"`},
		},
	}
	out, err := GeneratePowerShellRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "$AppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)") {
		t.Error("missing $AppDataFolder def (referenced via $Var in shell)")
	}
	if !strings.Contains(out, "$LocalAppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)") {
		t.Error("missing $LocalAppDataFolder def (referenced via {Var} in args)")
	}
	// Marked arg rendered as a parenthesized concatenation in the invocation.
	if !strings.Contains(out, "($LocalAppDataFolder + '\\MyApp\\cfg.json')") {
		t.Errorf("missing marked arg expression in invocation; got:\n%s", out)
	}
}

func TestGeneratePowerShellRunnerNoKnownFolderDefsWhenUnused(t *testing.T) {
	cfg := PowerShellRunnerConfig{
		Name: "x", Version: "v1", Platform: "windows-amd64", ExpectedArch: "AMD64",
		Commands: []CommandSpec{{Shell: "Write-Host hi"}},
	}
	out, err := GeneratePowerShellRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[Environment]::GetFolderPath") {
		t.Error("runner with no variable references should emit no known-folder definitions")
	}
}
