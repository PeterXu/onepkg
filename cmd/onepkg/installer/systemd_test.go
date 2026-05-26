package installer

import (
	"strings"
	"testing"
)

func TestGenerateSystemdUnit(t *testing.T) {
	cfg := SystemdConfig{
		Name:       "autotest",
		Exec:       "/usr/local/autotest/bin/test1",
		Restart:    "on-failure",
		User:       "autotest",
		Type:       "simple",
		After:      []string{"network.target"},
		RestartSec: 5,
	}

	output := GenerateSystemdUnit(cfg)

	tests := []struct{ substr string }{
		{`[Unit]`},
		{`Description=autotest`},
		{`After=network.target`},
		{`[Service]`},
		{`Type=simple`},
		{`ExecStart=/usr/local/autotest/bin/test1`},
		{`Restart=on-failure`},
		{`RestartSec=5`},
		{`User=autotest`},
		{`[Install]`},
		{`WantedBy=multi-user.target`},
	}

	for _, tt := range tests {
		if !strings.Contains(output, tt.substr) {
			t.Errorf("output missing %q\nfull output:\n%s", tt.substr, output)
		}
	}
}

func TestGenerateSystemdUnitMinimal(t *testing.T) {
	cfg := SystemdConfig{
		Name:    "myapp",
		Exec:    "/usr/local/myapp/bin/myapp",
		Restart: "always",
		User:    "root",
	}

	output := GenerateSystemdUnit(cfg)

	if strings.Contains(output, "After=") {
		t.Error("minimal config should not contain After=")
	}
	if strings.Contains(output, "RestartSec=") {
		t.Error("minimal config should not contain RestartSec=")
	}
	if !strings.Contains(output, "Restart=always") {
		t.Error("missing Restart=always")
	}
	if !strings.Contains(output, "User=root") {
		t.Error("missing User=root")
	}
}
