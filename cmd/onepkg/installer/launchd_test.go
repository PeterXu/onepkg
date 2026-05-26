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
