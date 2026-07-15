package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"onepkg/installer"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	// Subcommand dispatch
	if os.Args[1] == "toml-update" {
		var addMode bool
		rest := os.Args[2:]
		if len(rest) > 0 && rest[0] == "--add" {
			addMode = true
			rest = rest[1:]
		}
		var err error
		if addMode {
			err = runTomlUpdateAdd(rest)
		} else {
			err = runTomlUpdate(rest)
		}
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if os.Args[1] == "json-update" {
		var addMode bool
		rest := os.Args[2:]
		if len(rest) > 0 && rest[0] == "--add" {
			addMode = true
			rest = rest[1:]
		}
		var err error
		if addMode {
			err = runJsonUpdateAdd(rest)
		} else {
			err = runJsonUpdate(rest)
		}
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var configPath string
	var platformInput string
	var outputOverride string

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-c", "--config":
			if i+1 < len(os.Args) {
				configPath = os.Args[i+1]
				i++
			}
		case "--platform":
			if i+1 < len(os.Args) {
				platformInput = os.Args[i+1]
				i++
			}
		case "--output":
			if i+1 < len(os.Args) {
				outputOverride = os.Args[i+1]
				i++
			}
		case "help", "-h", "--help":
			printHelp()
			os.Exit(0)
		}
	}

	if configPath == "" {
		fmt.Println("Error: -c <config.toml> is required")
		os.Exit(1)
	}

	if platformInput == "" {
		fmt.Println("Error: --platform <platforms> is required")
		fmt.Println("Example: --platform linux-arm64")
		fmt.Println("         --platform linux-arm64,linux-amd64")
		os.Exit(1)
	}

	platforms, err := ParsePlatforms(platformInput)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	if outputOverride != "" {
		cfg.OutputDir = outputOverride
	}

	configDir := filepath.Dir(configPath)
	if err := cfg.Validate(configDir); err != nil {
		fmt.Printf("Error validating config: %v\n", err)
		os.Exit(1)
	}

	outputDir := filepath.Join(configDir, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Printf("Error creating output dir: %v\n", err)
		os.Exit(1)
	}

	for _, platform := range platforms {
		entries, err := ResolveItemsForPlatform(configDir, cfg, platform)
		if err != nil {
			fmt.Printf("Error resolving items for %s: %v\n", platform, err)
			os.Exit(1)
		}

		goos := strings.SplitN(platform, "-", 2)[0]
		var tarData []byte
		if goos != "windows" {
			tarData, err = BuildTarGZ(entries, *cfg.PreservePermissions)
			if err != nil {
				fmt.Printf("Error building archive for %s: %v\n", platform, err)
				os.Exit(1)
			}
		}

		shaMap := make(map[string]string)
		for _, e := range entries {
			shaMap[e.RelPath] = e.SHA1
		}

		if err := BuildForPlatform(cfg, platform, entries, tarData, shaMap, outputDir); err != nil {
			fmt.Printf("Error building %s: %v\n", platform, err)
			os.Exit(1)
		}
		fmt.Printf("Created: %s\n", platformOutputPath(cfg, platform, outputDir))
	}

	fmt.Printf("\nBuilt %d platform(s)\n", len(platforms))
}

// loadConfig parses config.toml, expands {env.NAME} host variables, and applies
// defaults. It is the testable seam for the {env.NAME} wiring (main() otherwise
// orchestrates inline). Validate is called separately by main() so its error
// message and the output-override step are preserved.
func loadConfig(configPath string) (*Config, error) {
	cfg, err := ParseConfig(configPath)
	if err != nil {
		return nil, err
	}
	if err := cfg.ExpandEnv(); err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

// unameArch maps GOARCH to the value uname -m returns on the target system.
func unameArch(goos, arch string) string {
	switch {
	case goos == "windows" && arch == "amd64":
		return "AMD64"
	case goos == "windows" && arch == "arm64":
		return "ARM64"
	case goos == "linux" && arch == "arm64":
		return "aarch64"
	case arch == "amd64":
		return "x86_64"
	case arch == "386":
		return "i686"
	case arch == "loong64":
		return "loongarch64"
	case arch == "mipsle":
		return "mipsel"
	case arch == "mips64le":
		return "mips64el"
	default:
		return arch
	}
}

func BuildForPlatform(cfg *Config, platform string, entries []FileEntry, tarData []byte, shaMap map[string]string, outputDir string) error {
	parts := strings.SplitN(platform, "-", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid platform format %q, expected {os}-{arch}", platform)
	}
	goos := parts[0]
	arch := parts[1]

	// Collapse per-OS overrides to the effective values for this target. Operate
	// on a local copy so main()'s shared *Config is untouched across platforms.
	resolved := cfg.resolveFor(goos)
	cfg = &resolved

	// Windows-only {dst.*} markers cannot resolve on Linux/macOS targets. Check
	// the RESOLVED path for this OS (so a marker in path_windows does not affect
	// a linux build); {dst.HOME} is cross-platform and allowed.
	if goos != "windows" {
		for _, d := range cfg.Dirs {
			if installer.HasWindowsOnlyKnownFolderMarker(d.Path) {
				return fmt.Errorf("Windows known-folder variable in dirs[].path %q is not supported on %s; build for windows or remove it", d.Path, goos)
			}
		}
		for _, o := range cfg.SystemOverlay {
			if installer.HasWindowsOnlyKnownFolderMarker(o.System) {
				return fmt.Errorf("Windows known-folder variable in system_overlay[].system %q is not supported on %s; build for windows or remove it", o.System, goos)
			}
		}
		// Substitute the cross-platform {dst.HOME} marker -> $HOME on Unix targets
		// (install-mode paths here; run-mode commands are substituted in
		// BuildForUnixRun). The shell expands $HOME at runtime.
		for i := range cfg.Dirs {
			cfg.Dirs[i].Path = substituteHomeUnix(cfg.Dirs[i].Path)
		}
		for i := range cfg.SystemOverlay {
			cfg.SystemOverlay[i].System = substituteHomeUnix(cfg.SystemOverlay[i].System)
		}
	}

	// Route unix run mode to the dedicated builder (before install-only VERSION generation)
	if cfg.Mode == "run" && (goos == "linux" || goos == "darwin") {
		if err := validateResolvedCommands(cfg, goos); err != nil {
			return err
		}
		return BuildForUnixRun(cfg, platform, entries, outputDir)
	}

	versionContent, err := GenerateVersion(VersionInfo{
		Name:     cfg.Name,
		Version:  cfg.Version,
		Platform: platform,
		Created:  time.Now().UTC(),
		Files:    len(entries),
		SHA:      shaMap,
	})
	if err != nil {
		return fmt.Errorf("generate version: %w", err)
	}

	var serviceContent string
	var serviceType string
	var serviceName string
	var defaultDir string

	switch goos {
	case "linux":
		defaultDir = "/usr/local/" + cfg.Name
		if len(cfg.Services) > 0 {
			svc := cfg.Services[0]
			serviceType = "systemd"
			serviceName = svc.Name + ".service"
			workingDir := svc.Linux.WorkingDir
			if workingDir == "" {
				workingDir = defaultDir // default to install directory
			} else if !filepath.IsAbs(workingDir) {
				workingDir = filepath.Join(defaultDir, workingDir)
			}
			serviceContent = installer.GenerateSystemdUnit(installer.SystemdConfig{
				Name:        svc.Name,
				Exec:        filepath.Join(defaultDir, svc.Exec),
				Args:        svc.Args,
				Restart:     svc.Restart,
				User:        svc.User,
				Type:        svc.Linux.Type,
				After:       svc.Linux.After,
				RestartSec:  svc.Linux.RestartSec,
				WorkingDir:  workingDir,
				Environment: svc.Linux.Environment,
				LimitNOFILE: svc.Linux.LimitNOFILE,
			})
		}
	case "darwin":
		defaultDir = "/opt/" + cfg.Name
		if len(cfg.Services) > 0 {
			svc := cfg.Services[0]
			serviceType = "launchd"
			serviceName = "com." + svc.Name + ".plist"
			workingDir := svc.MacOS.WorkingDir
			if workingDir == "" {
				workingDir = defaultDir // default to install directory
			} else if !filepath.IsAbs(workingDir) {
				workingDir = filepath.Join(defaultDir, workingDir)
			}
			serviceContent = installer.GenerateLaunchdPlist(installer.LaunchdConfig{
				Name:        svc.Name,
				Exec:        filepath.Join(defaultDir, svc.Exec),
				Args:        svc.Args,
				User:        svc.User,
				RunAtLoad:   svc.MacOS.RunAtLoad,
				KeepAlive:   svc.MacOS.KeepAlive,
				WorkingDir:  workingDir,
				Environment: svc.MacOS.Environment,
				LimitNOFILE: svc.MacOS.LimitNOFILE,
			})
		}
	case "windows":
		return BuildForWindows(cfg, platform, entries, shaMap, outputDir)
	default:
		defaultDir = "/usr/local/" + cfg.Name
	}

	if serviceType == "" {
		serviceType = "none"
	}

	expectedArch := unameArch(goos, arch)

	// Convert DirConfig to DirSpec
	var dirs []installer.DirSpec
	for _, dir := range cfg.Dirs {
		dirs = append(dirs, installer.DirSpec{
			Path:  dir.Path,
			Owner: dir.Owner,
			Group: dir.Group,
			Mode:  dir.Mode,
		})
	}

	// Convert OverlayConfig to OverlaySpec
	var overlays []installer.OverlaySpec
	for _, o := range cfg.SystemOverlay {
		overlays = append(overlays, installer.OverlaySpec{
			System: o.System,
			Item:   o.Item,
		})
	}

	shellCfg := installer.ShellConfig{
		Name:           cfg.Name,
		Version:        cfg.Version,
		Platform:       platform,
		ExpectedArch:   expectedArch,
		DefaultDir:     defaultDir,
		PayloadBase64:  base64.StdEncoding.EncodeToString(tarData),
		VersionContent: base64.StdEncoding.EncodeToString([]byte(versionContent)),
		ServiceContent: base64.StdEncoding.EncodeToString([]byte(serviceContent)),
		ServiceType:    serviceType,
		ServiceName:    serviceName,
		Dirs:           dirs,
		Overlays:       overlays,
	}

	script, err := installer.GenerateShellInstaller(shellCfg)
	if err != nil {
		return fmt.Errorf("generate installer: %w", err)
	}

	outputPath := platformOutputPath(cfg, platform, outputDir)
	return os.WriteFile(outputPath, []byte(script), 0755)
}

func BuildForWindows(cfg *Config, platform string, entries []FileEntry, shaMap map[string]string, outputDir string) error {
	if cfg.Mode == "run" {
		return BuildForWindowsRun(cfg, platform, entries, outputDir)
	}
	parts := strings.SplitN(platform, "-", 2)
	arch := parts[1]

	defaultDir := filepath.Join("C:", "Program Files", cfg.Name)

	versionContent, err := GenerateVersion(VersionInfo{
		Name:     cfg.Name,
		Version:  cfg.Version,
		Platform: platform,
		Created:  time.Now().UTC(),
		Files:    len(entries),
		SHA:      shaMap,
	})
	if err != nil {
		return fmt.Errorf("generate version: %w", err)
	}

	// Build ZIP payload with VERSION file included
	payloadZip, err := BuildZipWithExtras(entries, *cfg.PreservePermissions, []VirtualFile{
		{RelPath: "VERSION", Content: []byte(versionContent), Mode: 0644},
	})
	if err != nil {
		return fmt.Errorf("build zip: %w", err)
	}

	// Build Windows service config (if service defined)
	var serviceName string
	var serviceConfig installer.WindowsServiceConfig
	if len(cfg.Services) > 0 {
		svc := cfg.Services[0]
		serviceName = svc.Name
		serviceConfig = installer.WindowsServiceConfig{
			StartType:       svc.Windows.StartType,
			RecoveryDelayMs: *svc.Windows.RecoveryDelayMs,
			// Exec is kept relative; install.ps1 resolves it against $InstallDir at
			// install time so a custom -InstallDir is reflected in the service binPath
			// (matching how the .sh installer rewrites DEFAULT_DIR -> install_dir).
			// resolveWindowsItemPath maps the config exec to its packed path, so an
			// extensionless {platform} item packed as .exe on Windows is addressed.
			Exec: resolveWindowsItemPath(entries, svc.Exec),
			Args: svc.Args,
		}
	}

	// Convert DirConfig to DirSpec
	var dirs []installer.DirSpec
	for _, d := range cfg.Dirs {
		dirs = append(dirs, installer.DirSpec{
			Path:  d.Path,
			Owner: d.Owner,
			Group: d.Group,
			Mode:  d.Mode,
		})
	}

	// Convert OverlayConfig to OverlaySpec
	var overlays []installer.OverlaySpec
	for _, o := range cfg.SystemOverlay {
		overlays = append(overlays, installer.OverlaySpec{
			System: o.System,
			Item:   resolveWindowsItemPath(entries, o.Item),
		})
	}

	// Generate install.ps1
	ps1Content, err := installer.GeneratePowerShellInstaller(installer.PowerShellConfig{
		Name:          cfg.Name,
		Version:       cfg.Version,
		Platform:      platform,
		ExpectedArch:  unameArch("windows", arch),
		DefaultDir:    defaultDir,
		ServiceName:   serviceName,
		ServiceConfig: serviceConfig,
		Dirs:          dirs,
		Overlays:      overlays,
	})
	if err != nil {
		return fmt.Errorf("generate powershell: %w", err)
	}

	// Generate self-extracting exe
	exeData, err := installer.GenerateExeWrapper(installer.ExeWrapperConfig{
		Name:          cfg.Name,
		Version:       cfg.Version,
		Platform:      platform,
		InstallScript: ps1Content,
		PayloadZip:    payloadZip,
	})
	if err != nil {
		return fmt.Errorf("generate exe wrapper: %w", err)
	}

	outputPath := platformOutputPath(cfg, platform, outputDir)
	return os.WriteFile(outputPath, exeData, 0755)
}

// BuildForWindowsRun builds a mode="run" self-extracting Windows exe: a payload
// zip (no VERSION file), a lean run.ps1, wrapped via the existing exe-wrapper.
// The stub is unchanged; run.ps1 tolerates the -InstallDir/-Action it forwards.
func BuildForWindowsRun(cfg *Config, platform string, entries []FileEntry, outputDir string) error {
	parts := strings.SplitN(platform, "-", 2)
	arch := parts[1]

	if err := validateResolvedCommands(cfg, "windows"); err != nil {
		return err
	}

	// No VERSION file: run mode has no verify action. Empty zip when no entries.
	payloadZip, err := BuildZipWithExtras(entries, *cfg.PreservePermissions, nil)
	if err != nil {
		return fmt.Errorf("build zip: %w", err)
	}

	commands := make([]installer.CommandSpec, 0, len(cfg.Commands))
	for _, c := range cfg.Commands {
		commands = append(commands, installer.CommandSpec{
			Exec:  resolveWindowsItemPath(entries, c.Exec),
			Args:  c.Args,
			Shell: c.Shell,
		})
	}

	ps1Content, err := installer.GeneratePowerShellRunner(installer.PowerShellRunnerConfig{
		Name:         cfg.Name,
		Version:      cfg.Version,
		Platform:     platform,
		ExpectedArch: unameArch("windows", arch),
		RequireAdmin: cfg.RequireAdmin,
		HasPayload:   len(entries) > 0,
		Commands:     commands,
	})
	if err != nil {
		return fmt.Errorf("generate powershell runner: %w", err)
	}

	exeData, err := installer.GenerateExeWrapper(installer.ExeWrapperConfig{
		Name:          cfg.Name,
		Version:       cfg.Version,
		Platform:      platform,
		InstallScript: ps1Content,
		PayloadZip:    payloadZip,
	})
	if err != nil {
		return fmt.Errorf("generate exe wrapper: %w", err)
	}

	outputPath := platformOutputPath(cfg, platform, outputDir)
	return os.WriteFile(outputPath, exeData, 0755)
}

// validateResolvedCommands checks the per-platform run-mode command rules that
// Validate() cannot (it has no target OS): each resolved command has exactly
// one of exec/shell (guaranteed by base-type-fixed resolution) and, if exec,
// the resolved exec names a bundled item. cfg must already be resolveFor(goos).
func validateResolvedCommands(cfg *Config, goos string) error {
	for i, c := range cfg.Commands {
		if c.Exec != "" && !itemMatches(cfg.Items, c.Exec) {
			return fmt.Errorf("command %d exec %q not found in items for %s", i, c.Exec, goos)
		}
	}
	return nil
}

// BuildForUnixRun builds a mode="run" self-extracting .sh for Linux/macOS:
// a tar.gz payload (no VERSION), a lean run .sh, written with the .sh extension.
// {dst.HOME} is substituted to the literal $HOME token (the shell expands it).
func BuildForUnixRun(cfg *Config, platform string, entries []FileEntry, outputDir string) error {
	parts := strings.SplitN(platform, "-", 2)
	goos, arch := parts[0], parts[1]

	// No VERSION file: run mode has no verify action. Empty/nil when no entries.
	var tarData []byte
	var err error
	if len(entries) > 0 {
		tarData, err = BuildTarGZ(entries, *cfg.PreservePermissions)
		if err != nil {
			return fmt.Errorf("build tar.gz: %w", err)
		}
	}

	commands := make([]installer.CommandSpec, 0, len(cfg.Commands))
	for _, c := range cfg.Commands {
		// cfg is already resolved for goos; collapse to a CommandSpec and
		// substitute {dst.HOME} -> $HOME in the effective strings.
		commands = append(commands, installer.CommandSpec{
			Exec:  substituteHomeUnix(c.Exec),
			Args:  substituteHomeUnixArgs(c.Args),
			Shell: substituteHomeUnix(c.Shell),
		})
	}

	script, err := installer.GenerateShellRunner(installer.ShellRunnerConfig{
		Name:          cfg.Name,
		Version:       cfg.Version,
		Platform:      platform,
		ExpectedArch:  unameArch(goos, arch),
		Goos:          goos,
		HasPayload:    len(entries) > 0,
		PayloadBase64: base64.StdEncoding.EncodeToString(tarData),
		Commands:      commands,
	})
	if err != nil {
		return fmt.Errorf("generate run script: %w", err)
	}

	outputPath := platformOutputPath(cfg, platform, outputDir)
	return os.WriteFile(outputPath, []byte(script), 0755)
}

// substituteHomeUnix replaces the cross-platform {dst.HOME} marker with the
// literal $HOME token for Unix targets (the shell expands $HOME at runtime).
// Other {dst.*} markers are Windows-only and rejected by the build guard, so
// they never reach here on a unix build.
func substituteHomeUnix(s string) string {
	return strings.ReplaceAll(s, "{dst.HOME}", "$HOME")
}

func substituteHomeUnixArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = substituteHomeUnix(a)
	}
	return out
}

func platformOutputPath(cfg *Config, platform string, outputDir string) string {
	platformFile := strings.ReplaceAll(platform, "-", "_")
	goos := strings.SplitN(platform, "-", 2)[0]
	ext := ".sh"
	if goos == "windows" {
		ext = ".exe"
	}
	return filepath.Join(outputDir, fmt.Sprintf("%s_%s_%s%s", cfg.Name, cfg.Version, platformFile, ext))
}

func printHelp() {
	fmt.Println(`onepkg - General-purpose self-extracting installer generator

Usage:
  onepkg -c config.toml --platform <platforms> [options]
  onepkg toml-update [--add] <key> <value> <file>
  onepkg json-update [--add] <key> <value> <file>

Subcommands:
  toml-update           Update a key/value in a TOML file
    --add               Upsert: add if key doesn't exist, update if it does
  json-update           Update a key/value in a JSON file
    --add               Upsert: add if key doesn't exist, update if it does

Options:
  -c, --config <path>      Path to config.toml (required)
      --platform <p>       Target platforms, comma-separated (required)
                           e.g., --platform linux-arm64
                           e.g., --platform linux-arm64,linux-amd64
      --output <dir>       Override output directory
  -h, --help               Show this help

Supported platforms:
  linux-amd64, linux-arm64
  darwin-amd64, darwin-arm64
  windows-amd64, windows-arm64

Item syntax:
  Items can use {platform} marker for platform-specific files:
    "bin/myapp.{platform}"  -> resolves to bin/myapp.linux-arm64, packed as bin/myapp
  Literal items are unchanged:
    "etc/config.conf"       -> packed as etc/config.conf

Examples:
  onepkg -c config.toml --platform linux-arm64
  onepkg -c config.toml --platform linux-arm64,linux-amd64
  onepkg -c config.toml --platform darwin-arm64 --output ./dist
  onepkg -c config.toml --platform windows-amd64
  onepkg toml-update version "1.0.1" config.toml
  onepkg toml-update --add newkey "value" config.toml
  onepkg json-update version "1.0.1" package.json
  onepkg json-update --add newField "value" package.json`)
}
