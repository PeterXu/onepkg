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

func TestGenerateLaunchdPlistWithEnvironment(t *testing.T) {
	cfg := LaunchdConfig{
		Name:        "myapp",
		Exec:        "/opt/myapp/bin/myapp",
		User:        "root",
		Environment: map[string]string{"LOG_LEVEL": "info", "PORT": "8080"},
	}

	output := GenerateLaunchdPlist(cfg)

	if !strings.Contains(output, "<key>EnvironmentVariables</key>") {
		t.Errorf("output missing EnvironmentVariables key\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "<key>LOG_LEVEL</key>") {
		t.Errorf("output missing LOG_LEVEL key\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "<string>info</string>") {
		t.Errorf("output missing LOG_LEVEL value\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "<key>PORT</key>") {
		t.Errorf("output missing PORT key\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "<string>8080</string>") {
		t.Errorf("output missing PORT value\nfull output:\n%s", output)
	}
}

func TestGenerateLaunchdPlistWithoutEnvironment(t *testing.T) {
	cfg := LaunchdConfig{
		Name: "myapp",
		Exec: "/opt/myapp/bin/myapp",
		User: "root",
	}

	output := GenerateLaunchdPlist(cfg)

	if strings.Contains(output, "EnvironmentVariables") {
		t.Error("should not contain EnvironmentVariables when not set")
	}
}

func TestGenerateLaunchdPlistWithLimitNOFILE(t *testing.T) {
	cfg := LaunchdConfig{
		Name:        "myapp",
		Exec:        "/opt/myapp/bin/myapp",
		User:        "root",
		LimitNOFILE: 65536,
	}

	output := GenerateLaunchdPlist(cfg)

	if !strings.Contains(output, "<key>SoftResourceLimits</key>") {
		t.Errorf("output missing SoftResourceLimits\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "<key>HardResourceLimits</key>") {
		t.Errorf("output missing HardResourceLimits\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "<key>NumberOfFiles</key>") {
		t.Errorf("output missing NumberOfFiles key\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "<integer>65536</integer>") {
		t.Errorf("output missing NumberOfFiles value\nfull output:\n%s", output)
	}
}

func TestGenerateLaunchdPlistWithoutLimitNOFILE(t *testing.T) {
	cfg := LaunchdConfig{
		Name: "myapp",
		Exec: "/opt/myapp/bin/myapp",
		User: "root",
	}

	output := GenerateLaunchdPlist(cfg)

	if strings.Contains(output, "ResourceLimits") {
		t.Error("should not contain ResourceLimits when not set")
	}
}

func TestGenerateLaunchdPlistWithXMLSpecialChars(t *testing.T) {
	cfg := LaunchdConfig{
		Name:        "my<app>",
		Exec:        "/opt/myapp/bin/myapp",
		User:        "user&name",
		Environment: map[string]string{"LOG_LEVEL": "</string><key>Label</key><string>evil"},
	}

	output := GenerateLaunchdPlist(cfg)

	// XML special characters should be escaped
	if strings.Contains(output, "my<app>") {
		t.Error("Name with < > should be escaped")
	}
	if !strings.Contains(output, "my&lt;app&gt;") {
		t.Error("Name should have escaped XML entities")
	}
	if strings.Contains(output, "user&name") {
		t.Error("User with & should be escaped")
	}
	if !strings.Contains(output, "user&amp;name") {
		t.Error("User should have escaped & entity")
	}
	// XML injection should be prevented - the injected evil string should be escaped
	// The injection attempt should appear as escaped: &lt;/string&gt;&lt;key&gt;Label&lt;/key&gt;
	if strings.Contains(output, "<key>Label</key><string>evil") {
		t.Error("XML injection through Environment value should be prevented")
	}
	if !strings.Contains(output, "&lt;/string&gt;&lt;key&gt;Label") {
		t.Error("Environment value should be properly escaped")
	}
}

func TestXmlEscape(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal", "normal"},
		{"<tag>", "&lt;tag&gt;"},
		{"&amp", "&amp;amp"},   // & -> &amp;, so &amp -> &amp;amp
		{"\"quoted\"", "&quot;quoted&quot;"},
		{"'single'", "&apos;single&apos;"},
		{"all<>&\"'", "all&lt;&gt;&amp;&quot;&apos;"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := xmlEscape(tt.input)
			if result != tt.expected {
				t.Errorf("xmlEscape(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
