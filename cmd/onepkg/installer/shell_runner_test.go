package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateShellRunnerLinux(t *testing.T) {
	cfg := ShellRunnerConfig{
		Name:         "cleanup",
		Version:      "v1.0.0",
		Platform:     "linux-arm64",
		ExpectedArch: "aarch64",
		Goos:         "linux",
		HasPayload:   true,
		Commands: []CommandSpec{
			{Exec: "bin/cleanup", Args: []string{"--all", "--force"}},
			{Shell: "mkdir -p \"$HOME/.local/share/cleanup\""},
		},
	}
	out, err := GenerateShellRunner(cfg)
	if err != nil {
		t.Fatalf("GenerateShellRunner: %v", err)
	}
	mustContain := []string{
		"#!/bin/bash",
		"set -uo pipefail", // run-all: no -e
		`PKG_NAME="cleanup"`,
		`EXPECTED_ARCH="aarch64"`,
		"check_arch()",
		"trap 'rm -rf \"$PKG_TMP\"' EXIT",
		"base64 -d | tar xzf",   // HasPayload = true
		`echo Running: "bin/cleanup" "--all" "--force"`,
		`"$PKG_TMP/bin/cleanup" "--all" "--force"; __rc=$?`,
		`results+=("[OK]  $__label")`,
		`results+=("[FAIL] $__label (exit $__rc)")`,
		`failures=$((failures+1))`,
		`eval "mkdir -p \"$HOME/.local/share/cleanup\""; __rc=$?`,
		`$(( ${#results[@]} - failures )) of ${#results[@]} commands succeeded.`,
		`if [ "$failures" -gt 0 ]; then`,
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q", s)
		}
	}
	// Linux: no dialog.
	for _, bad := range []string{"osascript"} {
		if strings.Contains(out, bad) {
			t.Errorf("linux output should not contain %q", bad)
		}
	}
	// Must not carry install-only machinery.
	for _, bad := range []string{"cmd_install", "cmd_uninstall", "cmd_verify", "check_root", "install_service"} {
		if strings.Contains(out, bad) {
			t.Errorf("runner output should not contain install-only %q", bad)
		}
	}
}

func TestGenerateShellRunnerLinuxNoPayload(t *testing.T) {
	cfg := ShellRunnerConfig{
		Name: "shellonly", Version: "v1", Platform: "linux-amd64",
		ExpectedArch: "x86_64", Goos: "linux", HasPayload: false,
		Commands: []CommandSpec{{Shell: "echo hi"}},
	}
	out, err := GenerateShellRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "base64 -d | tar xzf") {
		t.Error("shell-only runner should not emit payload extraction")
	}
}

func TestGenerateShellRunnerDarwinDialog(t *testing.T) {
	cfg := ShellRunnerConfig{
		Name: "cleanup", Version: "v1.0.0", Platform: "darwin-arm64",
		ExpectedArch: "arm64", Goos: "darwin", HasPayload: false,
		Commands: []CommandSpec{{Shell: "echo hi"}},
	}
	out, err := GenerateShellRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mustContain := []string{
		"osascript",                       // dialog invocation
		"display dialog",
		"ONEPKG_DIALOG_MSG",               // message passed via env var (never embedded in quotes)
		"ONEPKG_DIALOG_TITLE",             // title also passed via env var
		`with title _t`,                   // title referenced as an AppleScript variable, not a literal
		`ONEPKG_DIALOG_TITLE='cleanup v1.0.0'`, // title bash-single-quote-escaped
		`buttons {"OK"} default button "OK" with icon note`,
		`|| true`,                         // dialog failure must not fail the script
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("darwin output missing %q", s)
		}
	}
	// The title must NOT be embedded raw inside the single-quoted -e argument
	// (an apostrophe in a name would break bash parsing).
	if strings.Contains(out, `with title "cleanup v1.0.0"`) {
		t.Error("title must not be embedded as a raw literal in the osascript -e arg")
	}
}

func TestGenerateShellRunnerDarwinApostropheNameSyntax(t *testing.T) {
	// A package name with an apostrophe must not break the generated script:
	// the title is bash-single-quote-escaped and passed via env var, so the
	// whole .sh still parses.
	cfg := ShellRunnerConfig{
		Name: "Peter's Tool", Version: "v1.0.0", Platform: "darwin-arm64",
		ExpectedArch: "arm64", Goos: "darwin", HasPayload: false,
		Commands: []CommandSpec{{Shell: "echo hi"}},
	}
	out, err := GenerateShellRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `ONEPKG_DIALOG_TITLE='Peter'\''s Tool v1.0.0'`) {
		t.Errorf("title not bash-single-quote-escaped for apostrophe name; got:\n%s", out)
	}
	tmp := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(tmp, []byte(out), 0644); err != nil {
		t.Fatal(err)
	}
	// bash -n parses without executing; a quoting bug exits non-zero.
	if combined, err := exec.Command("bash", "-n", tmp).CombinedOutput(); err != nil {
		t.Fatalf("darwin runner with apostrophe name must parse (bash -n): %v\n%s", err, combined)
	}
}

func TestGenerateShellRunnerLinuxExecutes(t *testing.T) {
	// The spec requires executing the linux run .sh (no GUI). Run-all: a
	// failing command is recorded but does not abort; exit reflects failures.
	hostArch, err := exec.Command("uname", "-m").Output()
	if err != nil {
		t.Skipf("uname -m unavailable: %v", err)
	}
	cfg := ShellRunnerConfig{
		Name: "diag", Version: "v1", Platform: "linux-amd64",
		ExpectedArch: strings.TrimSpace(string(hostArch)), Goos: "linux", HasPayload: false,
		Commands: []CommandSpec{
			{Shell: "echo first"},
			{Shell: "false"}, // fails
			{Shell: "echo third"},
		},
	}
	out, err := GenerateShellRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(tmp, []byte(out), 0755); err != nil {
		t.Fatal(err)
	}
	combined, err := exec.Command("bash", tmp).CombinedOutput()
	s := string(combined)
	if err == nil {
		t.Fatalf("expected non-zero exit (one command fails); got 0. output:\n%s", s)
	}
	for _, want := range []string{"[OK]", "[FAIL]", "2 of 3 commands succeeded."} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q; got:\n%s", want, s)
		}
	}
}

func TestGenerateShellRunnerDarwinNotOnLinux(t *testing.T) {
	linux, _ := GenerateShellRunner(ShellRunnerConfig{
		Name: "x", Version: "v1", Platform: "linux-amd64",
		ExpectedArch: "x86_64", Goos: "linux", Commands: []CommandSpec{{Shell: "echo"}},
	})
	if strings.Contains(linux, "osascript") {
		t.Error("linux runner must not contain osascript")
	}
}
