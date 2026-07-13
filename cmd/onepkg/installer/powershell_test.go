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
			Exec:            `C:\Program Files\myapp\bin\myapp.exe`,
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
		{`$DEFAULT_DIR = 'C:\Program Files\myapp'`},
		{`$SERVICE_NAME = 'myapp'`},
		{`$SERVICE_START_TYPE = 'auto'`},
		{`$SERVICE_RECOVERY_DELAY_MS = 5000`},
		{`$SERVICE_EXEC = 'C:\Program Files\myapp\bin\myapp.exe'`},
		{`function CheckAdmin`},
		{`function CheckArch`},
		{`function InstallService`},
		{`function UninstallService`},
		{`function CreateDirs`},
		{`function ApplyOverlays`},
		{`function Install`},
		{`function Uninstall`},
		{`sc.exe create`},
		{`sc.exe failure`},
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
	// Should not contain service install logic when no service
	if strings.Contains(output, "sc.exe create") {
		t.Error("should not contain sc.exe create when no service")
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
