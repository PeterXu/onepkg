package installer

import (
	"strings"
	"testing"
)

func TestGeneratePowerShellInstaller(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "myapp",
		ServiceConfig: WindowsServiceConfig{
			StartType:       "auto",
			RecoveryDelayMs: 5000,
			Exec:            `bin/myapp.exe`,
			Args:            []string{"--config", "etc/myapp.conf"},
		},
	}

	output, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatalf("GeneratePowerShellInstaller returned error: %v", err)
	}

	tests := []struct{ substr string }{
		{`param(`},
		{`$PKG_NAME = 'myapp'`},
		{`$PKG_VERSION = 'v1.0.0'`},
		{`$PKG_PLATFORM = 'windows-amd64'`},
		{`$EXPECTED_ARCH = 'AMD64'`},
		{`$ALLOWED_ARCHS = @('AMD64', 'ARM64')`},
		{`$DEFAULT_DIR = 'C:\Program Files\myapp'`},
		{`$SERVICE_NAME = 'myapp'`},
		{`$SERVICE_START_TYPE = 'auto'`},
		{`$SERVICE_RECOVERY_DELAY_MS = 5000`},
		{`$SERVICE_EXEC = 'bin/myapp.exe'`},
		{`Join-Path $InstallDir $SERVICE_EXEC`},
		{`function CheckAdmin`},
		{`function CheckArch`},
		{`$ALLOWED_ARCHS -notcontains $arch`},
		{`function InstallService`},
		{`function UninstallService`},
		{`function CreateDirs`},
		{`function ApplyOverlays`},
		{`function Install`},
		{`function Uninstall`},
		{`New-Service -Name $SERVICE_NAME -BinaryPathName $binPath`},
		{`Get-Service -Name $SERVICE_NAME -ErrorAction SilentlyContinue`},
		{`(Get-Date) -lt $deadline`},
		{`-StartupType`},
		{`actions= restart/$SERVICE_RECOVERY_DELAY_MS/restart/$SERVICE_RECOVERY_DELAY_MS/restart/$SERVICE_RECOVERY_DELAY_MS`},
		{`reset= 86400`},
		{`sc.exe delete`},
		{`Expand-Archive`},
		{`-Action`},
		{`switch ($Action)`},
	}

	for _, tt := range tests {
		if !strings.Contains(output, tt.substr) {
			t.Errorf("output missing %q", tt.substr)
		}
	}
}

func TestGeneratePowerShellInstallerNoService(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-arm64",
		ExpectedArch: "ARM64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "",
	}

	output, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, `$SERVICE_NAME = ''`) {
		t.Error("should set empty SERVICE_NAME when no service")
	}
	// An arm64 package may only run on arm64 (x64 cannot emulate arm64).
	if !strings.Contains(output, "$ALLOWED_ARCHS = @('ARM64')") {
		t.Error("arm64 package should allow only ARM64")
	}
	// Should not contain service install logic when no service
	if strings.Contains(output, "New-Service") {
		t.Error("should not contain New-Service when no service")
	}
}

func TestGeneratePowerShellInstallerWithDirs(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "",
		Dirs: []DirSpec{
			{Path: `C:\var\log\myapp`, Owner: "myapp", Mode: "0755"},
			{Path: "data", Owner: "myapp", Mode: "0750"},
		},
	}

	output, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, "$DIRS_COUNT = 2") {
		t.Error("output missing DIRS_COUNT = 2")
	}
	if !strings.Contains(output, "$DIR_0_PATH = 'C:\\var\\log\\myapp'") {
		t.Error("output missing DIR_0_PATH")
	}
	if !strings.Contains(output, "$DIR_1_PATH = 'data'") {
		t.Error("output missing DIR_1_PATH")
	}
}

func TestGeneratePowerShellInstallerWithOverlays(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "",
		Overlays: []OverlaySpec{
			{System: `C:\etc\agent\config.txt`, Item: "etc/config.txt"},
		},
	}

	output, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, "$OVERLAY_COUNT = 1") {
		t.Error("output missing OVERLAY_COUNT = 1")
	}
	if !strings.Contains(output, "$OVERLAY_0_SYSTEM = 'C:\\etc\\agent\\config.txt'") {
		t.Error("output missing OVERLAY_0_SYSTEM")
	}
}

func TestGeneratePowerShellInstallerArgsQuoting(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "myapp",
		ServiceConfig: WindowsServiceConfig{
			StartType:       "auto",
			RecoveryDelayMs: 5000,
			Exec:            "bin/myapp.exe",
			// An arg value containing a space must not split the service binPath.
			Args: []string{"--config", `C:\Program Files\myapp\etc\app.conf`},
		},
	}

	output, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Per-arg quoting must be present so an arg with a space is passed as one token.
	if !strings.Contains(output, "$SERVICE_ARGS | ForEach-Object") {
		t.Error("output missing per-arg quoting (ForEach-Object over $SERVICE_ARGS)")
	}
	// The naive join that splits on spaces must be gone.
	if strings.Contains(output, "$SERVICE_ARGS -join") {
		t.Error("output still uses naive $SERVICE_ARGS -join (would split args with spaces)")
	}
}
