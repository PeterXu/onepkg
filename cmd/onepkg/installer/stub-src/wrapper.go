package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const magicMarker = "ONEPKG"

type manifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	PS1Size int64  `json:"ps1_size"`
	ZipSize int64  `json:"zip_size"`
}

func main() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting exe path: %v\n", err)
		os.Exit(1)
	}

	exeData, err := os.ReadFile(exePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading exe: %v\n", err)
		os.Exit(1)
	}

	magicLen := len(magicMarker)
	if len(exeData) < magicLen+4 {
		fmt.Fprintf(os.Stderr, "Invalid exe format: too short\n")
		os.Exit(1)
	}

	magic := string(exeData[len(exeData)-magicLen:])
	if magic != magicMarker {
		fmt.Fprintf(os.Stderr, "Invalid exe format: no magic marker\n")
		os.Exit(1)
	}

	// Read manifest size (4 bytes before magic marker)
	manifestSize := binary.LittleEndian.Uint32(exeData[len(exeData)-magicLen-4:])
	manifestStart := len(exeData) - magicLen - 4 - int(manifestSize)
	if manifestStart < 0 {
		fmt.Fprintf(os.Stderr, "Invalid exe format: manifest size %d exceeds file size\n", manifestSize)
		os.Exit(1)
	}

	var m manifest
	if err := json.Unmarshal(exeData[manifestStart:manifestStart+int(manifestSize)], &m); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing manifest: %v\n", err)
		os.Exit(1)
	}

	// Read payload (ps1 + zip) before manifest
	payloadStart := manifestStart - int(m.PS1Size+m.ZipSize)
	if payloadStart < 0 {
		fmt.Fprintf(os.Stderr, "Invalid exe format: payload sizes exceed available data\n")
		os.Exit(1)
	}
	ps1Data := exeData[payloadStart : payloadStart+int(m.PS1Size)]
	zipData := exeData[payloadStart+int(m.PS1Size) : manifestStart]

	// Create temp directory
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%s-install", m.Name, m.Version))
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	// Write install.ps1
	ps1Path := filepath.Join(tmpDir, "install.ps1")
	if err := os.WriteFile(ps1Path, ps1Data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing install.ps1: %v\n", err)
		os.Exit(1)
	}

	// Write payload.zip
	zipPath := filepath.Join(tmpDir, "payload.zip")
	if err := os.WriteFile(zipPath, zipData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing payload.zip: %v\n", err)
		os.Exit(1)
	}

	// Run PowerShell installer
	defaultDir := filepath.Join("C:", "Program Files", m.Name)
	cmd := exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", ps1Path,
		"-PayloadDir", tmpDir,
		"-InstallDir", defaultDir,
		"-Action", "install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Install failed: %v\n", err)
		os.Exit(1)
	}
}
