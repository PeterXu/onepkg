package installer

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

//go:embed stubs/wrapper-stub-amd64.exe
var stubAmd64 []byte

//go:embed stubs/wrapper-stub-arm64.exe
var stubArm64 []byte

const magicMarker = "ONEPKG"

// ExeWrapperConfig configures the generated self-extracting exe
type ExeWrapperConfig struct {
	Name          string
	Version       string
	Platform      string // e.g., windows-amd64
	InstallScript string // install.ps1 content
	PayloadZip    []byte // payload.zip bytes
}

type exeManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	PS1Size int64  `json:"ps1_size"`
	ZipSize int64  `json:"zip_size"`
}

// GenerateExeWrapper produces a self-extracting Windows exe by appending
// payload data to a pre-compiled stub. The stub reads its own file at runtime
// to find the appended payload.
func GenerateExeWrapper(cfg ExeWrapperConfig) ([]byte, error) {
	var stub []byte
	if cfg.Platform == "windows-amd64" {
		stub = stubAmd64
	} else if cfg.Platform == "windows-arm64" {
		stub = stubArm64
	} else {
		return nil, fmt.Errorf("unsupported windows platform: %s (expected windows-amd64 or windows-arm64)", cfg.Platform)
	}
	return appendPayload(stub, cfg)
}

// appendPayload appends install.ps1 + payload.zip + manifest + size + magic
// to the given stub bytes. This is the testable core of GenerateExeWrapper.
func appendPayload(stub []byte, cfg ExeWrapperConfig) ([]byte, error) {
	manifest := exeManifest{
		Name:    cfg.Name,
		Version: cfg.Version,
		PS1Size: int64(len(cfg.InstallScript)),
		ZipSize: int64(len(cfg.PayloadZip)),
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	var buf bytes.Buffer
	buf.Write(stub)
	buf.WriteString(cfg.InstallScript)
	buf.Write(cfg.PayloadZip)
	buf.Write(manifestData)

	manifestSizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(manifestSizeBytes, uint32(len(manifestData)))
	buf.Write(manifestSizeBytes)

	buf.WriteString(magicMarker)

	return buf.Bytes(), nil
}
