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
