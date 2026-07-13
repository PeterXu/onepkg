package installer

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

func TestAppendPayload(t *testing.T) {
	fakeStub := []byte("FAKE_STUB_CONTENT")
	cfg := ExeWrapperConfig{
		Name:          "myapp",
		Version:       "v1.0.0",
		Platform:      "windows-amd64",
		InstallScript: "Write-Host 'install'",
		PayloadZip:    []byte("ZIP_DATA_BYTES"),
	}

	result, err := appendPayload(fakeStub, cfg)
	if err != nil {
		t.Fatalf("appendPayload returned error: %v", err)
	}

	// Result must start with the stub
	if !bytes.HasPrefix(result, fakeStub) {
		t.Error("result does not start with stub bytes")
	}

	// Result must end with magic marker
	if !bytes.HasSuffix(result, []byte(magicMarker)) {
		t.Error("result does not end with magic marker")
	}

	// Result must be: stub + ps1 + zip + manifest + manifestSize(4) + magic
	expectedLen := len(fakeStub) + len(cfg.InstallScript) + len(cfg.PayloadZip)
	// manifestSize(4) + magic(6) = 10 minimum trailer
	if len(result) <= expectedLen+10 {
		t.Errorf("result length = %d, expected > %d", len(result), expectedLen+10)
	}
}

func TestAppendPayloadManifest(t *testing.T) {
	fakeStub := []byte("STUB")
	cfg := ExeWrapperConfig{
		Name:          "testapp",
		Version:       "v2.0.0",
		Platform:      "windows-arm64",
		InstallScript: "ps1-content",
		PayloadZip:    []byte("zip-content"),
	}

	result, err := appendPayload(fakeStub, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Parse back: magic marker at end
	magicLen := len(magicMarker)
	manifestSize := binary.LittleEndian.Uint32(result[len(result)-magicLen-4:])
	manifestStart := len(result) - magicLen - 4 - int(manifestSize)

	var m exeManifest
	if err := json.Unmarshal(result[manifestStart:manifestStart+int(manifestSize)], &m); err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	if m.Name != "testapp" {
		t.Errorf("manifest.Name = %q, want %q", m.Name, "testapp")
	}
	if m.Version != "v2.0.0" {
		t.Errorf("manifest.Version = %q, want %q", m.Version, "v2.0.0")
	}
	if m.PS1Size != int64(len("ps1-content")) {
		t.Errorf("manifest.PS1Size = %d, want %d", m.PS1Size, len("ps1-content"))
	}
	if m.ZipSize != int64(len("zip-content")) {
		t.Errorf("manifest.ZipSize = %d, want %d", m.ZipSize, len("zip-content"))
	}
}

func TestGenerateExeWrapper(t *testing.T) {
	cfg := ExeWrapperConfig{
		Name:          "myapp",
		Version:       "v1.0.0",
		Platform:      "windows-amd64",
		InstallScript: "Write-Host 'hello'",
		PayloadZip:    []byte("zipdata"),
	}

	result, err := GenerateExeWrapper(cfg)
	if err != nil {
		t.Fatalf("GenerateExeWrapper returned error: %v", err)
	}

	// Must end with magic marker
	if !bytes.HasSuffix(result, []byte(magicMarker)) {
		t.Error("exe does not end with magic marker")
	}

	// Must be larger than the stub alone (stubs are ~2MB+)
	if len(result) < 1000000 {
		t.Errorf("exe size = %d, expected > 1MB (stub + payload)", len(result))
	}

	// Must start with MZ (Windows PE header)
	if result[0] != 'M' || result[1] != 'Z' {
		t.Error("exe does not start with MZ (Windows PE header)")
	}
}

func TestGenerateExeWrapperArm64(t *testing.T) {
	cfg := ExeWrapperConfig{
		Name:          "myapp",
		Version:       "v1.0.0",
		Platform:      "windows-arm64",
		InstallScript: "Write-Host 'hello'",
		PayloadZip:    []byte("zipdata"),
	}

	result, err := GenerateExeWrapper(cfg)
	if err != nil {
		t.Fatalf("GenerateExeWrapper returned error: %v", err)
	}

	if !bytes.HasSuffix(result, []byte(magicMarker)) {
		t.Error("arm64 exe does not end with magic marker")
	}
}

func TestGenerateExeWrapperUnsupportedPlatform(t *testing.T) {
	cfg := ExeWrapperConfig{
		Name:          "myapp",
		Version:       "v1.0.0",
		Platform:      "linux-amd64", // not windows
		InstallScript: "script",
		PayloadZip:    []byte("data"),
	}

	_, err := GenerateExeWrapper(cfg)
	if err == nil {
		t.Error("expected error for non-windows platform")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error = %v, want error containing 'unsupported'", err)
	}
}

// TestExeWrapperRoundTrip pins the on-disk trailer layout that the writer
// (appendPayload) and the Windows stub reader (stub-src/wrapper.go) must agree
// on. The two live in separate Go modules with no compile-time linkage, so this
// test is the guard against silent drift: layout is
// stub + ps1 + zip + manifest(json) + manifestSize(uint32 LE) + magic("ONEPKG").
// If you change appendPayload, update the stub's parsing to match (and vice versa).
func TestExeWrapperRoundTrip(t *testing.T) {
	cfg := ExeWrapperConfig{
		Name:          "roundapp",
		Version:       "v9.9.9",
		Platform:      "windows-amd64",
		InstallScript: "Write-Host 'round trip'\n# install.ps1 body",
		PayloadZip:    []byte("FAKE-ZIP-CONTENTS-12345"),
	}

	exe, err := GenerateExeWrapper(cfg)
	if err != nil {
		t.Fatalf("GenerateExeWrapper: %v", err)
	}

	// Parse using the SAME offset math as stub-src/wrapper.go main().
	magicLen := len(magicMarker)
	if !bytes.HasSuffix(exe, []byte(magicMarker)) {
		t.Fatalf("exe does not end with magic marker")
	}
	manifestSize := binary.LittleEndian.Uint32(exe[len(exe)-magicLen-4:])
	manifestStart := len(exe) - magicLen - 4 - int(manifestSize)
	if manifestStart < 0 {
		t.Fatalf("manifestStart < 0 (manifestSize=%d)", manifestSize)
	}
	var m exeManifest
	if err := json.Unmarshal(exe[manifestStart:manifestStart+int(manifestSize)], &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	payloadStart := manifestStart - int(m.PS1Size+m.ZipSize)
	if payloadStart < 0 {
		t.Fatalf("payloadStart < 0 (ps1=%d zip=%d)", m.PS1Size, m.ZipSize)
	}
	ps1Data := exe[payloadStart : payloadStart+int(m.PS1Size)]
	zipData := exe[payloadStart+int(m.PS1Size) : manifestStart]

	if string(ps1Data) != cfg.InstallScript {
		t.Errorf("recovered ps1 = %q, want %q", string(ps1Data), cfg.InstallScript)
	}
	if string(zipData) != string(cfg.PayloadZip) {
		t.Errorf("recovered zip = %q, want %q", string(zipData), string(cfg.PayloadZip))
	}
	if m.Name != cfg.Name || m.Version != cfg.Version {
		t.Errorf("manifest name/version = %q/%q, want %q/%q", m.Name, m.Version, cfg.Name, cfg.Version)
	}
	if m.PS1Size != int64(len(cfg.InstallScript)) {
		t.Errorf("manifest PS1Size = %d, want %d", m.PS1Size, len(cfg.InstallScript))
	}
	if m.ZipSize != int64(len(cfg.PayloadZip)) {
		t.Errorf("manifest ZipSize = %d, want %d", m.ZipSize, len(cfg.PayloadZip))
	}
}
