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
