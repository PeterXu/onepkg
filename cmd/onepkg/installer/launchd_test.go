package installer

import (
	"strings"
	"testing"
)

func TestGenerateLaunchdPlist(t *testing.T) {
	cfg := LaunchdConfig{
		Name:      "autotest",
		Exec:      "/opt/autotest/bin/test1",
		User:      "autotest",
		RunAtLoad: true,
		KeepAlive: true,
	}

	output := GenerateLaunchdPlist(cfg)

	tests := []struct{ substr string }{
		{`<?xml version="1.0" encoding="UTF-8"?>`},
		{`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"`},
		{`<plist version="1.0">`},
		{`<dict>`},
		{`<key>Label</key>`},
		{`<string>com.autotest</string>`},
		{`<key>Program</key>`},
		{`<string>/opt/autotest/bin/test1</string>`},
		{`<key>RunAtLoad</key>`},
		{`<true/>`},
		{`<key>KeepAlive</key>`},
		{`<key>UserName</key>`},
		{`<string>autotest</string>`},
		{`</dict>`},
		{`</plist>`},
	}

	for _, tt := range tests {
		if !strings.Contains(output, tt.substr) {
			t.Errorf("output missing %q\nfull output:\n%s", tt.substr, output)
		}
	}
}

func TestGenerateLaunchdPlistMinimal(t *testing.T) {
	cfg := LaunchdConfig{
		Name: "myapp",
		Exec: "/opt/myapp/bin/myapp",
		User: "root",
	}

	output := GenerateLaunchdPlist(cfg)

	if strings.Contains(output, "RunAtLoad") {
		t.Error("minimal config should not contain RunAtLoad")
	}
	if strings.Contains(output, "KeepAlive") {
		t.Error("minimal config should not contain KeepAlive")
	}
	if !strings.Contains(output, "<string>root</string>") {
		t.Error("missing UserName root")
	}
}

func TestGenerateLaunchdPlistWithArgs(t *testing.T) {
	cfg := LaunchdConfig{
		Name:      "myapp",
		Exec:      "/opt/myapp/bin/myapp",
		Args:      []string{"--config", "etc/myapp.conf", "--log-level", "info"},
		User:      "root",
		RunAtLoad: true,
		KeepAlive: true,
	}

	output := GenerateLaunchdPlist(cfg)

	// With args, should use ProgramArguments instead of Program
	if strings.Contains(output, "<key>Program</key>") {
		t.Error("with args should use ProgramArguments, not Program")
	}
	if !strings.Contains(output, "<key>ProgramArguments</key>") {
		t.Error("missing ProgramArguments key")
	}
	// ProgramArguments should contain binary as first element, then args
	if !strings.Contains(output, "<string>/opt/myapp/bin/myapp</string>") {
		t.Error("missing binary path in ProgramArguments")
	}
	if !strings.Contains(output, "<string>--config</string>") {
		t.Error("missing --config argument")
	}
	if !strings.Contains(output, "<string>etc/myapp.conf</string>") {
		t.Error("missing config file argument")
	}
}

func TestGenerateLaunchdPlistEmptyArgs(t *testing.T) {
	cfg := LaunchdConfig{
		Name: "myapp",
		Exec: "/opt/myapp/bin/myapp",
		Args: []string{},
		User: "root",
	}

	output := GenerateLaunchdPlist(cfg)

	// Empty args should use Program (simpler form)
	if !strings.Contains(output, "<key>Program</key>") {
		t.Error("empty args should use Program key")
	}
	if strings.Contains(output, "ProgramArguments") {
		t.Error("empty args should not use ProgramArguments")
	}
}

func TestGenerateLaunchdPlistWithWorkingDir(t *testing.T) {
	cfg := LaunchdConfig{
		Name:       "myapp",
		Exec:       "/opt/myapp/bin/myapp",
		User:       "root",
		WorkingDir: "/opt/myapp/data",
	}

	output := GenerateLaunchdPlist(cfg)

	if !strings.Contains(output, "<key>WorkingDirectory</key>") {
		t.Errorf("output missing WorkingDirectory key\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "<string>/opt/myapp/data</string>") {
		t.Errorf("output missing WorkingDirectory value\nfull output:\n%s", output)
	}
}

func TestGenerateLaunchdPlistWithoutWorkingDir(t *testing.T) {
	cfg := LaunchdConfig{
		Name: "myapp",
		Exec: "/opt/myapp/bin/myapp",
		User: "root",
	}

	output := GenerateLaunchdPlist(cfg)

	if strings.Contains(output, "WorkingDirectory") {
		t.Error("should not contain WorkingDirectory when not set")
	}
}
