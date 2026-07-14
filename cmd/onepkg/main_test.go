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

func TestBuildForWindowsRunRejectsNonWindows(t *testing.T) {
	cfg := &Config{Name: "x", Version: "v1", Mode: "run"}
	err := BuildForPlatform(cfg, "linux-amd64", nil, nil, nil, "")
	if err == nil {
		t.Fatal("BuildForPlatform should reject mode=run on non-windows platform")
	}
	if !strings.Contains(err.Error(), "Windows-only") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildForWindowsRunWithItems(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "runapp.exe"), []byte("fake-exe"), 0755)

	configContent := `
name = "runapp"
version = "v1.0.0"
mode = "run"
items = ["bin/runapp.exe"]

[[commands]]
exec = "bin/runapp.exe"
args = ["--do-stuff"]

[[commands]]
shell = "Write-Host shell-cmd"
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	platform := "windows-amd64"
	resolved, err := ResolveItems(projectDir, cfg.Items, platform)
	if err != nil {
		t.Fatalf("ResolveItems: %v", err)
	}
	entries, err := CollectFilesResolved(projectDir, resolved, cfg.Exclude)
	if err != nil {
		t.Fatalf("CollectFilesResolved: %v", err)
	}

	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)

	if err := BuildForPlatform(cfg, platform, entries, nil, nil, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	exeFile := filepath.Join(outputDir, "runapp_v1.0.0_windows_amd64.exe")
	data, err := os.ReadFile(exeFile)
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	if data[0] != 'M' || data[1] != 'Z' {
		t.Error("exe does not start with MZ")
	}
	if !bytes.HasSuffix(data, []byte("ONEPKG")) {
		t.Error("exe does not end with ONEPKG magic")
	}
	// run.ps1 text is embedded verbatim in the exe bytes.
	if !bytes.Contains(data, []byte("& (Join-Path $PayloadDir 'bin/runapp.exe')")) {
		t.Error("exe missing exec command in embedded run.ps1")
	}
	if !bytes.Contains(data, []byte("Invoke-Expression 'Write-Host shell-cmd'")) {
		t.Error("exe missing shell command in embedded run.ps1")
	}
	if bytes.Contains(data, []byte("New-Service")) {
		t.Error("run exe should not contain service code")
	}
}

func TestBuildForWindowsRunShellOnly(t *testing.T) {
	projectDir := t.TempDir()
	configContent := `
name = "shellonly"
version = "v1.0.0"
mode = "run"

[[commands]]
shell = "Write-Host hello"
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)

	// Shell-only: no items -> empty entries.
	if err := BuildForPlatform(cfg, "windows-amd64", nil, nil, nil, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "shellonly_v1.0.0_windows_amd64.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("ONEPKG")) {
		t.Error("exe does not end with ONEPKG magic")
	}
	if bytes.Contains(data, []byte("Expand-Archive")) {
		t.Error("shell-only run exe should not contain Expand-Archive")
	}
}

func TestBuildForPlatformRejectsKnownFolderOnLinux(t *testing.T) {
	cfg := &Config{
		Name:    "kf",
		Version: "v1",
		Mode:    "install",
		Dirs:    []DirConfig{{Path: `{dst.AppDataFolder}\MyApp`, Mode: "0755"}},
	}
	err := BuildForPlatform(cfg, "linux-amd64", nil, nil, nil, "")
	if err == nil {
		t.Fatal("BuildForPlatform should reject a marked dirs path on linux")
	}
	if !strings.Contains(err.Error(), "not supported on linux") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildForPlatformRejectsKnownFolderOverlayOnDarwin(t *testing.T) {
	cfg := &Config{
		Name:          "kf",
		Version:       "v1",
		Mode:          "install",
		SystemOverlay: []OverlayConfig{{System: `{dst.ProgramFiles64Folder}\OtherApp\cfg`, Item: "etc/cfg"}},
	}
	err := BuildForPlatform(cfg, "darwin-arm64", nil, nil, nil, "")
	if err == nil {
		t.Fatal("BuildForPlatform should reject a marked overlay system on darwin")
	}
	if !strings.Contains(err.Error(), "not supported on darwin") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildForPlatformWindowsAllowsKnownFolderDirs(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "myapp.exe"), []byte("fake-exe"), 0755)

	content := `
name = "myapp"
version = "v1.0.0"
items = ["bin/myapp.exe"]

[[dirs]]
path = "{dst.AppDataFolder}\\MyApp\\data"
mode = "0755"
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(content), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	resolved, err := ResolveItems(projectDir, cfg.Items, "windows-amd64")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := CollectFilesResolved(projectDir, resolved, cfg.Exclude)
	if err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)

	if err := BuildForPlatform(cfg, "windows-amd64", entries, nil, nil, outputDir); err != nil {
		t.Fatalf("BuildForPlatform on windows should accept marked dirs: %v", err)
	}
	exe, err := os.ReadFile(filepath.Join(outputDir, "myapp_v1.0.0_windows_amd64.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(exe, []byte("$AppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)")) {
		t.Error("embedded install.ps1 missing the $AppDataFolder definition")
	}
}

func TestWindowsRunAutoExeExtension(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	// Source artifact is extensionless, matching {platform} resolution.
	os.WriteFile(filepath.Join(binDir, "onepkg.windows-amd64"), []byte("fake-exe"), 0755)

	configContent := `
name = "rustdesk-config"
version = "v1.0.0"
mode = "run"
items = ["bin/onepkg.{platform}"]

[[commands]]
exec = "bin/onepkg"
args = ["--version"]
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	platform := "windows-amd64"
	entries, err := ResolveItemsForPlatform(projectDir, cfg, platform)
	if err != nil {
		t.Fatalf("ResolveItemsForPlatform: %v", err)
	}

	// The executed item must be packed as bin/onepkg.exe, never bin/onepkg.
	var sawExe bool
	for _, e := range entries {
		if e.RelPath == "bin/onepkg.exe" {
			sawExe = true
		}
		if e.RelPath == "bin/onepkg" {
			t.Error("extensionless bin/onepkg must not be packed; expected bin/onepkg.exe")
		}
	}
	if !sawExe {
		t.Fatalf("entries missing bin/onepkg.exe; RelPaths: %v", relPathsOf(entries))
	}

	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)
	if err := BuildForPlatform(cfg, platform, entries, nil, nil, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "rustdesk-config_v1.0.0_windows_amd64.exe"))
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	// run.ps1 must invoke the .exe path, never the extensionless one.
	if !bytes.Contains(data, []byte("Join-Path $PayloadDir 'bin/onepkg.exe'")) {
		t.Error("embedded run.ps1 missing 'Join-Path $PayloadDir 'bin/onepkg.exe''")
	}
	if bytes.Contains(data, []byte("Join-Path $PayloadDir 'bin/onepkg'")) {
		t.Error("embedded run.ps1 references extensionless bin/onepkg")
	}
}

func TestWindowsRunNonExecItemUnchanged(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	dataDir := filepath.Join(projectDir, "data")
	os.MkdirAll(binDir, 0755)
	os.MkdirAll(dataDir, 0755)
	os.WriteFile(filepath.Join(binDir, "onepkg.windows-amd64"), []byte("fake-exe"), 0755)
	os.WriteFile(filepath.Join(dataDir, "README"), []byte("readme"), 0644)

	cfg := &Config{
		Name:     "x",
		Version:  "v1",
		Mode:     "run",
		Items:    []string{"bin/onepkg.{platform}", "data/README"},
		Commands: []CommandConfig{{Exec: "bin/onepkg"}},
	}
	entries, err := ResolveItemsForPlatform(projectDir, cfg, "windows-amd64")
	if err != nil {
		t.Fatalf("ResolveItemsForPlatform: %v", err)
	}
	// bin/onepkg is an exec target -> .exe; data/README is not -> unchanged.
	for _, e := range entries {
		if e.RelPath == "data/README.exe" {
			t.Errorf("non-exec item must not gain .exe; got %q", e.RelPath)
		}
	}
}

func TestResolveItemsForPlatformLinuxNoExe(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "onepkg.linux-arm64"), []byte("x"), 0755)

	cfg := &Config{
		Name:     "x",
		Version:  "v1",
		Items:    []string{"bin/onepkg.{platform}"},
		Commands: []CommandConfig{{Exec: "bin/onepkg"}}, // exec target, but Linux -> no .exe
	}
	entries, err := ResolveItemsForPlatform(projectDir, cfg, "linux-arm64")
	if err != nil {
		t.Fatalf("ResolveItemsForPlatform: %v", err)
	}
	for _, e := range entries {
		if e.RelPath == "bin/onepkg.exe" {
			t.Errorf("Linux build must not add .exe; got %q", e.RelPath)
		}
		if e.RelPath != "bin/onepkg" {
			t.Errorf("Linux pack path = %q, want bin/onepkg", e.RelPath)
		}
	}
}

func TestWindowsInstallAutoExeExtension(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "onepkg.windows-amd64"), []byte("fake-exe"), 0755)

	// Service exec and an overlay both reference the extensionless {platform} item.
	// The overlay system path uses a {dst.} known folder so it is not Go-validated
	// for absoluteness on the macOS build host (only literal paths are).
	configContent := `
name = "svcapp"
version = "v1.0.0"
items = ["bin/onepkg.{platform}"]

[[services]]
name = "svcapp"
exec = "bin/onepkg"

[services.windows]
start_type = "auto"

[[system_overlay]]
system = "{dst.ProgramFiles64Folder}/OtherApp/conf"
item = "bin/onepkg"
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	platform := "windows-amd64"
	entries, err := ResolveItemsForPlatform(projectDir, cfg, platform)
	if err != nil {
		t.Fatalf("ResolveItemsForPlatform: %v", err)
	}
	var sawExe bool
	for _, e := range entries {
		if e.RelPath == "bin/onepkg.exe" {
			sawExe = true
		}
	}
	if !sawExe {
		t.Fatalf("entries missing bin/onepkg.exe; RelPaths: %v", relPathsOf(entries))
	}

	shaMap := make(map[string]string)
	for _, e := range entries {
		shaMap[e.RelPath] = e.SHA1
	}
	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)
	if err := BuildForPlatform(cfg, platform, entries, nil, shaMap, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "svcapp_v1.0.0_windows_amd64.exe"))
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	// Service exec and overlay item must both reference the .exe path.
	if !bytes.Contains(data, []byte("$SERVICE_EXEC = 'bin/onepkg.exe'")) {
		t.Error("install.ps1 missing $SERVICE_EXEC = 'bin/onepkg.exe'")
	}
	if !bytes.Contains(data, []byte("$OVERLAY_0_ITEM = 'bin/onepkg.exe'")) {
		t.Error("install.ps1 missing $OVERLAY_0_ITEM = 'bin/onepkg.exe'")
	}
}

// relPathsOf returns the RelPath values of entries, for assertion failure messages.
func relPathsOf(entries []FileEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.RelPath
	}
	return out
}
