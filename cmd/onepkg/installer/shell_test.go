package installer

import (
	"strings"
	"testing"
)

func TestGenerateShellInstaller(t *testing.T) {
	cfg := ShellConfig{
		Name:           "autotest",
		Version:        "v1.0.0",
		Platform:       "linux-arm64",
		ExpectedArch:   "aarch64",
		DefaultDir:     "/usr/local/autotest",
		PayloadBase64:  "cGF5bG9hZA==",
		VersionContent: "dmVyc2lvbg==",
		ServiceContent: "c2VydmljZQ==",
		ServiceType:    "systemd",
		ServiceName:    "autotest.service",
	}

	output, err := GenerateShellInstaller(cfg)
	if err != nil {
		t.Fatalf("GenerateShellInstaller returned error: %v", err)
	}

	tests := []struct{ substr string }{
		{`#!/bin/bash`},
		{`PKG_NAME="autotest"`},
		{`PKG_VERSION="v1.0.0"`},
		{`EXPECTED_ARCH="aarch64"`},
		{`DEFAULT_DIR="/usr/local/autotest"`},
		{`SERVICE_TYPE="systemd"`},
		{`SERVICE_NAME="autotest.service"`},
		{`PAYLOAD='cGF5bG9hZA=='`},
		{`cmd_install()`},
		{`cmd_uninstall()`},
		{`cmd_version()`},
		{`cmd_list()`},
		{`cmd_verify()`},
		{`check_arch`},
		{`check_root`},
		{`base64 -d`},
		{`tar xzf`},
		{`--dir)`},
	}

	for _, tt := range tests {
		if !strings.Contains(output, tt.substr) {
			t.Errorf("output missing %q", tt.substr)
		}
	}

	if !strings.HasPrefix(output, "#!/bin/bash") {
		t.Error("script must start with shebang")
	}
}

func TestGenerateShellInstallerNoService(t *testing.T) {
	cfg := ShellConfig{
		Name:           "myapp",
		Version:        "v0.1.0",
		Platform:       "linux-amd64",
		ExpectedArch:   "x86_64",
		DefaultDir:     "/usr/local/myapp",
		PayloadBase64:  "cGF5bG9hZA==",
		VersionContent: "dmVyc2lvbg==",
		ServiceContent: "",
		ServiceType:    "none",
		ServiceName:    "",
	}

	output, err := GenerateShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, `SERVICE_TYPE="none"`) {
		t.Error("should set SERVICE_TYPE to none")
	}
	if !strings.Contains(output, `SERVICE_NAME=""`) {
		t.Error("should set empty SERVICE_NAME")
	}
}

func TestGenerateShellInstallerDarwin(t *testing.T) {
	cfg := ShellConfig{
		Name:           "autotest",
		Version:        "v1.0.0",
		Platform:       "darwin-arm64",
		ExpectedArch:   "arm64",
		DefaultDir:     "/opt/autotest",
		PayloadBase64:  "cGF5bG9hZA==",
		VersionContent: "dmVyc2lvbg==",
		ServiceContent: "c2VydmljZQ==",
		ServiceType:    "launchd",
		ServiceName:    "com.autotest.plist",
	}

	output, err := GenerateShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, `DEFAULT_DIR="/opt/autotest"`) {
		t.Error("darwin should use /opt default")
	}
	if !strings.Contains(output, `EXPECTED_ARCH="arm64"`) {
		t.Error("darwin arm64 should expect arm64")
	}
}

func TestGenerateShellInstallerWithDirs(t *testing.T) {
	cfg := ShellConfig{
		Name:           "myapp",
		Version:        "v1.0.0",
		Platform:       "linux-arm64",
		ExpectedArch:   "aarch64",
		DefaultDir:     "/usr/local/myapp",
		PayloadBase64:  "dGVzdA==",
		VersionContent: "dGVzdA==",
		ServiceType:    "none",
		ServiceName:    "",
		Dirs: []DirSpec{
			{Path: "/var/log/myapp", Owner: "myapp", Group: "myapp", Mode: "0755"},
			{Path: "data", Owner: "myapp", Group: "myapp", Mode: "0750"},
		},
	}

	output, err := GenerateShellInstaller(cfg)
	if err != nil {
		t.Fatalf("GenerateShellInstaller returned error: %v", err)
	}

	// Check DIRS_COUNT
	if !strings.Contains(output, "DIRS_COUNT=2") {
		t.Error("output missing DIRS_COUNT=2")
	}

	// Check DIR_0 variables
	if !strings.Contains(output, "DIR_0_PATH=\"/var/log/myapp\"") {
		t.Error("output missing DIR_0_PATH")
	}
	if !strings.Contains(output, "DIR_0_OWNER=\"myapp\"") {
		t.Error("output missing DIR_0_OWNER")
	}
	if !strings.Contains(output, "DIR_0_GROUP=\"myapp\"") {
		t.Error("output missing DIR_0_GROUP")
	}
	if !strings.Contains(output, "DIR_0_MODE=\"0755\"") {
		t.Error("output missing DIR_0_MODE")
	}

	// Check DIR_1 variables (relative path)
	if !strings.Contains(output, "DIR_1_PATH=\"data\"") {
		t.Error("output missing DIR_1_PATH")
	}

	// Check create_dirs function exists
	if !strings.Contains(output, "create_dirs()") {
		t.Error("output missing create_dirs function")
	}
}

func TestGenerateShellInstallerNoDirs(t *testing.T) {
	cfg := ShellConfig{
		Name:           "myapp",
		Version:        "v1.0.0",
		Platform:       "linux-arm64",
		ExpectedArch:   "aarch64",
		DefaultDir:     "/usr/local/myapp",
		PayloadBase64:  "dGVzdA==",
		VersionContent: "dGVzdA==",
		ServiceType:    "none",
		ServiceName:    "",
		Dirs:           []DirSpec{}, // Empty
	}

	output, err := GenerateShellInstaller(cfg)
	if err != nil {
		t.Fatalf("GenerateShellInstaller returned error: %v", err)
	}

	// Should have DIRS_COUNT=0
	if !strings.Contains(output, "DIRS_COUNT=0") {
		t.Error("output should have DIRS_COUNT=0 for empty dirs")
	}
}
