# Windows Support Design for onepkg

Self-extracting installer generator for Windows platforms using pre-compiled stub executables with embedded payload.

## Scope

- **Architectures**: windows-amd64, windows-arm64
- **Target systems**: Windows 8, 10, 11
- **Service level**: Standard (basic start/stop/restart + recovery options)

## Output

- **Format**: `package_windows_amd64.exe` (self-extracting executable)
- **Installation**: Double-click supported
- **Default path**: `C:\Program Files\{name}`

## Config Schema

### Windows Service Options

```toml
[[services]]
name = "myapp"
exec = "bin/myapp.exe"
args = ["--config", "etc/myapp.conf"]
restart = "on-failure"
user = "myapp"

[services.windows]
start_type = "auto"        # auto | manual | disabled (default: auto)
recovery_delay_ms = 5000   # Delay before restart on failure (default: 5000)
```

### Defaults

| Field | Default |
|-------|---------|
| `start_type` | `auto` |
| `recovery_delay_ms` | `5000` |

### Validation

- `start_type` must be one of: `auto`, `manual`, `disabled`
- `recovery_delay_ms` must be >= 0 (0 = no recovery)

## Architecture

### Overview

```
package_windows_amd64.exe
├── Pre-compiled stub exe        (reads itself, extracts payload)
├── install.ps1                  (installer script)
├── payload.zip                  (app files)
├── Manifest JSON                (metadata)
├── Manifest size (4 bytes)
└── "ONEPKG" magic marker
```

### Packaging Flow

```
1. Parse config.toml
2. Collect files, compute SHA1
3. Build ZIP payload (not tar.gz - Windows 8 compatibility)
4. Generate install.ps1 from template
5. Read pre-compiled stub from embedded
6. Append: payload + manifest + size + magic marker
7. Write: {name}_{version}_windows_{arch}.exe
```

### Installation Flow

```
1. User double-clicks exe
2. Stub reads own exe file, finds appended payload
3. Extracts install.ps1 + payload.zip to %TEMP%
4. Runs PowerShell -File install.ps1
5. install.ps1:
   - Check admin privileges
   - Check architecture
   - Extract payload.zip to C:\Program Files\{name}
   - Create directories (if configured)
   - Apply overlays (if configured)
   - Create/start Windows Service (sc.exe)
6. Cleanup temp (optional)
```

## Payload Format

### ZIP (not tar.gz)

Windows 8/10/11 all support `Expand-Archive` natively. Using ZIP ensures:
- Works on all target Windows versions
- No external dependencies
- PowerShell native extraction

### Contents

```
payload.zip
├── bin/
│   └── myapp.exe
├── etc/
│   └── myapp.conf
├── data/
└── VERSION
```

## Service Management

### sc.exe (not PowerShell cmdlets)

Using `sc.exe` for service management because:
- Works on all Windows versions (Windows 8+)
- `Remove-Service` requires PowerShell 7+ (not installed by default)
- Recovery configuration via `sc.exe failure` is straightforward

### Service Creation

```powershell
# Create service
sc.exe create $SERVICE_NAME binPath= "`"$SERVICE_EXEC`" $SERVICE_ARGS" start= $start_type

# Configure recovery (restart on failure)
sc.exe failure $SERVICE_NAME actions= restart/$SERVICE_RECOVERY_DELAY_MS reset= 0
sc.exe failureflag $SERVICE_NAME 1  # Trigger on non-zero exit

# Start service (if auto)
sc.exe start $SERVICE_NAME
```

### Service Types

| Config value | sc.exe value | Description |
|--------------|--------------|-------------|
| `auto` | `auto` | Starts at boot |
| `manual` | `demand` | Starts manually |
| `disabled` | `disabled` | Cannot start |

### Service Removal

```powershell
sc.exe stop $SERVICE_NAME
sc.exe delete $SERVICE_NAME
```

## Admin Privilege Check

PowerShell security principal API (idiomatic PowerShell):

```powershell
function CheckAdmin {
    $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]$currentUser
    $isAdmin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

    if (-not $isAdmin) {
        Write-Error "This script requires administrator privileges."
        exit 1
    }
}
```

## Architecture Check

Windows uses `$env:PROCESSOR_ARCHITECTURE`:

```powershell
function CheckArch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($arch -ne $EXPECTED_ARCH) {
        Write-Error "Error: This package is for $EXPECTED_ARCH, current system is $arch"
        exit 1
    }
}
```

### Architecture Mapping

| Platform | Expected value |
|----------|----------------|
| `windows-amd64` | `AMD64` |
| `windows-arm64` | `ARM64` |

## Pre-compiled Stub

### Stub Architecture

The stub is a minimal Go executable that:
1. Reads its own exe file
2. Finds appended payload via magic marker
3. Extracts install.ps1 and payload.zip to temp
4. Runs PowerShell to execute install.ps1

### Stub Source Location

```
cmd/onepkg/installer/
├── stub-src/
│   ├── wrapper.go     # Stub source code
│   └── go.mod         # module stub
└── stubs/
    ├── wrapper-stub-amd64.exe
    └── wrapper-stub-arm64.exe
```

### Building Stubs

```bash
cd cmd/onepkg/installer/stub-src

# Build AMD64 stub
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -ldflags="-H windowsgui" \
  -o ../stubs/wrapper-stub-amd64.exe wrapper.go

# Build ARM64 stub
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 \
  go build -ldflags="-H windowsgui" \
  -o ../stubs/wrapper-stub-arm64.exe wrapper.go
```

**Note**: `-H windowsgui` hides the console window for cleaner double-click experience.

### Embedding Stubs

```go
//go:embed stubs/wrapper-stub-amd64.exe
var stubAmd64 []byte

//go:embed stubs/wrapper-stub-arm64.exe
var stubArm64 []byte
```

## File Structure

```
cmd/onepkg/
├── main.go              # Add BuildForWindows function
├── config.go            # Add WindowsService struct
├── platform.go          # Add "windows" to validOS
├── packager.go          # Add BuildZip function
└── installer/
    ├── shell.go         # Linux/macOS .sh generator (existing)
    ├── powershell.go    # Windows .ps1 generator (NEW)
    ├── exewrapper.go    # Append payload to stub (NEW)
    ├── systemd.go       # (existing)
    ├── launchd.go       # (existing)
    ├── stubs/           # Pre-compiled stubs (NEW)
    │   ├── wrapper-stub-amd64.exe
    │   └── wrapper-stub-arm64.exe
    └── stub-src/        # Stub source (NEW)
        ├── wrapper.go
        └── go.mod
```

## Implementation Tasks

### Task 1: Config Changes
- Add `WindowsService` struct in config.go
- Add defaults for start_type and recovery_delay_ms
- Add validation for Windows service options

### Task 2: Platform Validation
- Add "windows" to validOS map
- Add architecture mapping for Windows (AMD64, ARM64)

### Task 3: ZIP Building
- Add `BuildZip` function in packager.go
- Use `archive/zip` package

### Task 4: PowerShell Installer Generator
- Create `installer/powershell.go`
- Generate install.ps1 with:
  - CheckAdmin function
  - CheckArch function
  - InstallService function (sc.exe)
  - UninstallService function
  - CreateDirs function
  - ApplyOverlays function
  - Main dispatch (install/uninstall/version/list/verify)

### Task 5: EXE Wrapper Generator
- Create `installer/exewrapper.go`
- Embed pre-compiled stubs
- Append payload + manifest + magic marker

### Task 6: Pre-compiled Stubs
- Create `installer/stub-src/wrapper.go`
- Build stubs for AMD64 and ARM64
- Embed stubs in installer package

### Task 7: Main Orchestration
- Add `BuildForWindows` function in main.go
- Add Windows case in `BuildForPlatform`
- Update `platformOutputPath` for .exe extension

### Task 8: Tests
- Test config parsing with Windows service options
- Test ZIP building
- Test PowerShell installer generation
- Test exe wrapper generation
- Integration test for Windows platform

## install.ps1 Commands

| Command | Description |
|---------|-------------|
| `install` | Install to default or custom directory |
| `uninstall` | Remove installation and service |
| `version` | Show installed version |
| `list` | List installed files |
| `verify` | Verify file checksums |

## Differences from Linux/macOS

| Aspect | Linux/macOS | Windows |
|--------|-------------|---------|
| Output format | `.sh` | `.exe` |
| Payload format | tar.gz (base64) | ZIP (embedded) |
| Service tool | systemd/launchd | sc.exe |
| Admin check | `id -u` | PowerShell security principal |
| Arch check | `uname -m` | `$env:PROCESSOR_ARCHITECTURE` |
| Default path | `/usr/local/{name}`, `/opt/{name}` | `C:\Program Files\{name}` |

## Post-MVP Considerations

- Multiple services support
- Additional Windows service options (recovery actions, reset period)
- Windows-specific ACL for directories
- Windows Event Log integration
- MSI generation (enterprise deployment)