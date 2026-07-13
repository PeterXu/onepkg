package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnameArch(t *testing.T) {
	tests := []struct {
		goos, arch, want string
	}{
		{"linux", "amd64", "x86_64"},
		{"linux", "arm64", "aarch64"},
		{"windows", "amd64", "AMD64"},
		{"windows", "arm64", "ARM64"},
		{"linux", "386", "i686"},
		{"linux", "loong64", "loongarch64"},
		{"linux", "mipsle", "mipsel"},
		{"linux", "mips64le", "mips64el"},
		{"darwin", "amd64", "x86_64"},
		{"darwin", "arm64", "arm64"},
		{"linux", "riscv64", "riscv64"},
		{"linux", "s390x", "s390x"},
		{"linux", "ppc64le", "ppc64le"},
		{"linux", "mips64", "mips64"},
		{"linux", "arm", "arm"},
	}

	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.arch, func(t *testing.T) {
			got := unameArch(tt.goos, tt.arch)
			if got != tt.want {
				t.Errorf("unameArch(%q, %q) = %q, want %q", tt.goos, tt.arch, got, tt.want)
			}
		})
	}
}

func TestBuildPackage(t *testing.T) {
	projectDir := t.TempDir()

	binDir := filepath.Join(projectDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	etcDir := filepath.Join(projectDir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(binDir, "app"), []byte("#!/bin/sh\necho hello"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "app.conf"), []byte("key=value"), 0644); err != nil {
		t.Fatal(err)
	}

	configContent := `
name = "testapp"
version = "v1.0.0"
output_dir = "packages"
platforms = ["linux-amd64"]

items = [
  "bin/app",
  "etc/app.conf",
]

[[services]]
name = "testapp"
exec = "bin/app"
restart = "on-failure"
user = "root"

[services.linux]
type = "simple"
after = ["network.target"]
`
	configPath := filepath.Join(projectDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	entries, err := CollectFiles(projectDir, cfg.Items)
	if err != nil {
		t.Fatalf("CollectFiles: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	tarData, err := BuildTarGZ(entries, true)
	if err != nil {
		t.Fatalf("BuildTarGZ: %v", err)
	}

	shaMap := make(map[string]string)
	for _, e := range entries {
		shaMap[e.RelPath] = e.SHA1
	}

	platform := cfg.Platforms[0]
	versionContent, err := GenerateVersion(VersionInfo{
		Name:     cfg.Name,
		Version:  cfg.Version,
		Platform: platform,
		Files:    len(entries),
		SHA:      shaMap,
	})
	if err != nil {
		t.Fatalf("GenerateVersion: %v", err)
	}

	if !strings.Contains(versionContent, `name = "testapp"`) {
		t.Errorf("VERSION missing name, got:\n%s", versionContent)
	}
	if !strings.Contains(versionContent, `"bin/app"`) {
		t.Errorf("VERSION missing bin/app sha, got:\n%s", versionContent)
	}

	outputDir := filepath.Join(projectDir, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := BuildForPlatform(cfg, platform, entries, tarData, shaMap, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	expectedFile := filepath.Join(outputDir, "testapp_v1.0.0_linux_amd64.sh")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Fatalf("expected output file %s does not exist", expectedFile)
	}

	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)

	if !strings.HasPrefix(script, "#!/bin/bash") {
		t.Error("installer must start with #!/bin/bash")
	}
	if !strings.Contains(script, `PKG_NAME="testapp"`) {
		t.Error("installer missing PKG_NAME")
	}
	if !strings.Contains(script, "cmd_install") {
		t.Error("installer missing cmd_install function")
	}
	if !strings.Contains(script, "cmd_verify") {
		t.Error("installer missing cmd_verify function")
	}

	if err := os.Chmod(expectedFile, 0755); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPackageWithOverlay(t *testing.T) {
	projectDir := t.TempDir()

	binDir := filepath.Join(projectDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(projectDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "app"), []byte("#!/bin/sh\necho hello"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.txt"), []byte("key=value"), 0644); err != nil {
		t.Fatal(err)
	}

	configContent := `
name = "testapp"
version = "v1.0.0"
output_dir = "packages"

items = ["bin/app", "data/config.txt"]

[[system_overlay]]
system = "/etc/testapp/config.txt"
item = "data/config.txt"
`
	configPath := filepath.Join(projectDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	resolved, err := ResolveItems(projectDir, cfg.Items, "linux-amd64")
	if err != nil {
		t.Fatalf("ResolveItems: %v", err)
	}
	entries, err := CollectFilesResolved(projectDir, resolved, cfg.Exclude)
	if err != nil {
		t.Fatalf("CollectFilesResolved: %v", err)
	}

	tarData, err := BuildTarGZ(entries, true)
	if err != nil {
		t.Fatalf("BuildTarGZ: %v", err)
	}

	shaMap := make(map[string]string)
	for _, e := range entries {
		shaMap[e.RelPath] = e.SHA1
	}

	outputDir := filepath.Join(projectDir, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := BuildForPlatform(cfg, "linux-amd64", entries, tarData, shaMap, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	scriptBytes, err := os.ReadFile(filepath.Join(outputDir, "testapp_v1.0.0_linux_amd64.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	if !strings.Contains(script, "OVERLAY_COUNT=1") {
		t.Error("installer should have OVERLAY_COUNT=1")
	}
	if !strings.Contains(script, `OVERLAY_0_SYSTEM="/etc/testapp/config.txt"`) {
		t.Error("installer should have OVERLAY_0_SYSTEM")
	}
	if !strings.Contains(script, `OVERLAY_0_ITEM="data/config.txt"`) {
		t.Error("installer should have OVERLAY_0_ITEM")
	}
	if !strings.Contains(script, "apply_overlays") {
		t.Error("installer should have apply_overlays function")
	}
}

func TestOverlayEndToEnd(t *testing.T) {
	projectDir := t.TempDir()

	// Create project files
	binDir := filepath.Join(projectDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "myapp"), []byte("#!/bin/sh\necho hello"), 0755); err != nil {
		t.Fatal(err)
	}

	configContent := `
name = "e2etest"
version = "v1.0.0"
output_dir = "packages"

items = ["bin/myapp"]

[[system_overlay]]
system = "/etc/e2etest/config.txt"
item = "bin/myapp"
`
	configPath := filepath.Join(projectDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	resolved, err := ResolveItems(projectDir, cfg.Items, "linux-amd64")
	if err != nil {
		t.Fatalf("ResolveItems: %v", err)
	}
	entries, err := CollectFilesResolved(projectDir, resolved, cfg.Exclude)
	if err != nil {
		t.Fatalf("CollectFilesResolved: %v", err)
	}
	tarData, err := BuildTarGZ(entries, true)
	if err != nil {
		t.Fatalf("BuildTarGZ: %v", err)
	}

	shaMap := make(map[string]string)
	for _, e := range entries {
		shaMap[e.RelPath] = e.SHA1
	}

	outputDir := filepath.Join(projectDir, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := BuildForPlatform(cfg, "linux-amd64", entries, tarData, shaMap, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	scriptBytes, err := os.ReadFile(filepath.Join(outputDir, "e2etest_v1.0.0_linux_amd64.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	// Verify the apply_overlays function exists
	if !strings.Contains(script, "apply_overlays()") {
		t.Fatal("installer missing apply_overlays function")
	}

	// Verify the overlay function checks for existence before copying
	if !strings.Contains(script, `[ -e "$sys_path" ]`) {
		t.Error("apply_overlays should check if system path exists before copying")
	}

	// Verify directory copy uses recursive flag
	if !strings.Contains(script, `cp -r "$sys_path/."`) {
		t.Error("apply_overlays should use cp -r for directories")
	}

	// Verify file copy (non-directory)
	if !strings.Contains(script, `cp "$sys_path" "$install_dir/$item_path"`) {
		t.Error("apply_overlays should use cp for files")
	}

	// Verify the function is called during install
	if !strings.Contains(script, `apply_overlays "$install_dir"`) {
		t.Error("cmd_install should call apply_overlays")
	}

	// Verify overlay variables
	if !strings.Contains(script, "OVERLAY_COUNT=1") {
		t.Error("installer should have OVERLAY_COUNT=1")
	}
	if !strings.Contains(script, `OVERLAY_0_SYSTEM="/etc/e2etest/config.txt"`) {
		t.Error("installer should have correct OVERLAY_0_SYSTEM")
	}
	if !strings.Contains(script, `OVERLAY_0_ITEM="bin/myapp"`) {
		t.Error("installer should have correct OVERLAY_0_ITEM")
	}
}

func TestBuildForWindows(t *testing.T) {
	projectDir := t.TempDir()

	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "myapp.exe"), []byte("fake-exe-content"), 0755)

	configContent := `
name = "myapp"
version = "v1.0.0"

items = ["bin/myapp.exe"]

[[services]]
name = "myapp"
exec = "bin/myapp.exe"

[services.windows]
start_type = "auto"
recovery_delay_ms = 5000
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()

	platform := "windows-amd64"
	resolved, err := ResolveItems(projectDir, cfg.Items, platform)
	if err != nil {
		t.Fatalf("ResolveItems: %v", err)
	}

	entries, err := CollectFilesResolved(projectDir, resolved, cfg.Exclude)
	if err != nil {
		t.Fatalf("CollectFilesResolved: %v", err)
	}

	shaMap := make(map[string]string)
	for _, e := range entries {
		shaMap[e.RelPath] = e.SHA1
	}

	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)

	// For Windows, tarData is nil (built internally)
	if err := BuildForPlatform(cfg, platform, entries, nil, shaMap, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	// Verify output file exists with .exe extension
	expectedFile := filepath.Join(outputDir, "myapp_v1.0.0_windows_amd64.exe")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Fatalf("expected output file %s does not exist", expectedFile)
	}

	// Verify exe ends with magic marker
	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("ONEPKG")) {
		t.Error("exe does not end with ONEPKG magic marker")
	}
	// Verify exe starts with MZ (PE header)
	if data[0] != 'M' || data[1] != 'Z' {
		t.Error("exe does not start with MZ (Windows PE header)")
	}
}

func TestPlatformOutputPathWindows(t *testing.T) {
	cfg := &Config{Name: "myapp", Version: "v1.0.0"}

	tests := []struct {
		platform string
		want     string
	}{
		{"linux-amd64", "myapp_v1.0.0_linux_amd64.sh"},
		{"darwin-arm64", "myapp_v1.0.0_darwin_arm64.sh"},
		{"windows-amd64", "myapp_v1.0.0_windows_amd64.exe"},
		{"windows-arm64", "myapp_v1.0.0_windows_arm64.exe"},
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			got := filepath.Base(platformOutputPath(cfg, tt.platform, ""))
			if got != tt.want {
				t.Errorf("platformOutputPath(%q) = %q, want %q", tt.platform, got, tt.want)
			}
		})
	}
}
