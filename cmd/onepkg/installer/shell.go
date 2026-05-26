package installer

import (
	"fmt"
	"strings"
)

type DirSpec struct {
	Path  string
	Owner string
	Group string
	Mode  string
}

type ShellConfig struct {
	Name           string
	Version        string
	Platform       string
	ExpectedArch   string
	DefaultDir     string
	PayloadBase64  string
	VersionContent string
	ServiceContent string
	ServiceType    string
	ServiceName    string
	Dirs           []DirSpec
}

func GenerateShellInstaller(cfg ShellConfig) (string, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "#!/bin/bash\n")
	fmt.Fprintf(&b, "# %s %s self-extracting installer for %s\n", cfg.Name, cfg.Version, cfg.Platform)
	fmt.Fprintf(&b, "set -euo pipefail\n\n")

	fmt.Fprintf(&b, "PKG_NAME=%q\n", cfg.Name)
	fmt.Fprintf(&b, "PKG_VERSION=%q\n", cfg.Version)
	fmt.Fprintf(&b, "PKG_PLATFORM=%q\n", cfg.Platform)
	fmt.Fprintf(&b, "EXPECTED_ARCH=%q\n", cfg.ExpectedArch)
	fmt.Fprintf(&b, "DEFAULT_DIR=%q\n", cfg.DefaultDir)
	fmt.Fprintf(&b, "SERVICE_TYPE=%q\n", cfg.ServiceType)
	fmt.Fprintf(&b, "SERVICE_NAME=%q\n", cfg.ServiceName)
	fmt.Fprintf(&b, "DIRS_COUNT=%d\n", len(cfg.Dirs))
	for i, d := range cfg.Dirs {
		fmt.Fprintf(&b, "DIR_%d_PATH=%q\n", i, d.Path)
		fmt.Fprintf(&b, "DIR_%d_OWNER=%q\n", i, d.Owner)
		fmt.Fprintf(&b, "DIR_%d_GROUP=%q\n", i, d.Group)
		fmt.Fprintf(&b, "DIR_%d_MODE=%q\n", i, d.Mode)
	}
	b.WriteString("\n")

	b.WriteString("PAYLOAD='")
	b.WriteString(cfg.PayloadBase64)
	b.WriteString("'\n")
	b.WriteString("VERSION_B64='")
	b.WriteString(cfg.VersionContent)
	b.WriteString("'\n")
	b.WriteString("SERVICE_B64='")
	b.WriteString(cfg.ServiceContent)
	b.WriteString("'\n\n")

	b.WriteString(`compute_sha1() {
    if command -v sha1sum &>/dev/null; then
        sha1sum "$1" | cut -d' ' -f1
    else
        shasum -a 1 "$1" | cut -d' ' -f1
    fi
}

check_arch() {
    local arch=$(uname -m)
    if [ "$arch" != "$EXPECTED_ARCH" ]; then
        echo "Error: This package is for $EXPECTED_ARCH, current system is $arch"
        exit 1
    fi
}

check_root() {
    if [ "$(id -u)" != "0" ]; then
        echo "Error: This operation requires root privileges (use sudo)"
        exit 1
    fi
}

install_service() {
    local install_dir="$1"
    if [ "$SERVICE_TYPE" = "systemd" ]; then
        echo "$SERVICE_B64" | base64 -d > "/etc/systemd/system/$SERVICE_NAME"
        # Update paths from DEFAULT_DIR to actual install location
        sed -i "s|$DEFAULT_DIR|$install_dir|g" "/etc/systemd/system/$SERVICE_NAME"
        systemctl daemon-reload
        systemctl enable "$SERVICE_NAME"
        systemctl start "$SERVICE_NAME"
    elif [ "$SERVICE_TYPE" = "launchd" ]; then
        echo "$SERVICE_B64" | base64 -d > "/Library/LaunchDaemons/$SERVICE_NAME"
        # Update paths from DEFAULT_DIR to actual install location
        sed -i "s|$DEFAULT_DIR|$install_dir|g" "/Library/LaunchDaemons/$SERVICE_NAME"
        launchctl load "/Library/LaunchDaemons/$SERVICE_NAME"
    fi
}

uninstall_service() {
    if [ "$SERVICE_TYPE" = "systemd" ]; then
        systemctl stop "$SERVICE_NAME" 2>/dev/null || true
        systemctl disable "$SERVICE_NAME" 2>/dev/null || true
        rm -f "/etc/systemd/system/$SERVICE_NAME"
        systemctl daemon-reload
    elif [ "$SERVICE_TYPE" = "launchd" ]; then
        launchctl unload "/Library/LaunchDaemons/$SERVICE_NAME" 2>/dev/null || true
        rm -f "/Library/LaunchDaemons/$SERVICE_NAME"
    fi
}

create_dirs() {
    local install_dir="$1"

    if [ "$DIRS_COUNT" -eq 0 ] || [ -z "$DIRS_COUNT" ]; then
        return
    fi

    for i in $(seq 0 $((DIRS_COUNT - 1))); do
        eval "dir_path=\"\$DIR_${i}_PATH\""
        eval "dir_owner=\"\$DIR_${i}_OWNER\""
        eval "dir_group=\"\$DIR_${i}_GROUP\""
        eval "dir_mode=\"\$DIR_${i}_MODE\""

        # Resolve relative paths
        if [ "${dir_path:0:1}" != "/" ]; then
            dir_path="$install_dir/$dir_path"
        fi

        mkdir -p "$dir_path"

        if [ -n "$dir_owner" ]; then
            chown "${dir_owner}:${dir_group:-$dir_owner}" "$dir_path"
        fi

        if [ -n "$dir_mode" ]; then
            chmod "$dir_mode" "$dir_path"
        fi
    done
}

`)
	b.WriteString(`cmd_install() {
    check_arch
    check_root

    local base_dir="$DEFAULT_DIR"

    while [ $# -gt 0 ]; do
        case "$1" in
            --dir) base_dir="$2"; shift 2 ;;
            *) shift ;;
        esac
    done

    # If custom --dir specified, append package name as subdirectory
    local install_dir="$base_dir"
    if [ "$base_dir" != "$DEFAULT_DIR" ]; then
        install_dir="$base_dir/$PKG_NAME"
    fi

    if [ -d "$install_dir" ] && [ "$(ls -A "$install_dir" 2>/dev/null)" ]; then
        echo "Warning: $install_dir exists and is not empty, replacing files..."
    fi

    mkdir -p "$install_dir"

    echo "Installing $PKG_NAME $PKG_VERSION to $install_dir..."
    echo "$PAYLOAD" | base64 -d | tar xzf - -C "$install_dir"

    echo "$VERSION_B64" | base64 -d > "$install_dir/VERSION"

    # Create directories if configured
    if [ "$DIRS_COUNT" -gt 0 ]; then
        echo "Creating directories..."
        create_dirs "$install_dir"
    fi

    if [ "$SERVICE_TYPE" != "none" ] && [ -n "$SERVICE_B64" ]; then
        echo "Installing service..."
        install_service "$install_dir"
    fi

    echo ""
    echo "Installation complete: $install_dir"
    if [ "$SERVICE_TYPE" != "none" ]; then
        echo "Service: $SERVICE_NAME"
    fi
}

cmd_uninstall() {
    check_root

    local base_dir="$DEFAULT_DIR"

    while [ $# -gt 0 ]; do
        case "$1" in
            --dir) base_dir="$2"; shift 2 ;;
            *) shift ;;
        esac
    done

    # If custom --dir specified, append package name as subdirectory
    local install_dir="$base_dir"
    if [ "$base_dir" != "$DEFAULT_DIR" ]; then
        install_dir="$base_dir/$PKG_NAME"
    fi

    echo "Uninstalling $PKG_NAME..."

    uninstall_service

    rm -rf "$install_dir"

    echo "Uninstallation complete."
}

cmd_version() {
    local base_dir="$DEFAULT_DIR"

    while [ $# -gt 0 ]; do
        case "$1" in
            --dir) base_dir="$2"; shift 2 ;;
            *) shift ;;
        esac
    done

    # If custom --dir specified, append package name as subdirectory
    local install_dir="$base_dir"
    if [ "$base_dir" != "$DEFAULT_DIR" ]; then
        install_dir="$base_dir/$PKG_NAME"
    fi

    if [ -f "$install_dir/VERSION" ]; then
        cat "$install_dir/VERSION"
    else
        echo "$PKG_NAME is not installed (VERSION not found at $install_dir/VERSION)"
        exit 1
    fi
}

cmd_list() {
    local base_dir="$DEFAULT_DIR"

    while [ $# -gt 0 ]; do
        case "$1" in
            --dir) base_dir="$2"; shift 2 ;;
            *) shift ;;
        esac
    done

    # If custom --dir specified, append package name as subdirectory
    local install_dir="$base_dir"
    if [ "$base_dir" != "$DEFAULT_DIR" ]; then
        install_dir="$base_dir/$PKG_NAME"
    fi

    if [ ! -f "$install_dir/VERSION" ]; then
        echo "$PKG_NAME is not installed"
        exit 1
    fi

    echo "Installed files in $install_dir:"
    echo ""
    grep -A9999 '^\[sha\]' "$install_dir/VERSION" | while IFS= read -r line; do
        case "$line" in
            \"*\")
                temp="${line#\"}"
                file="${temp%%\" *}"
                temp="${line##*= \"}"
                sha="${temp%\"}"
                printf "  %-40s %s\n" "$file" "$sha"
                ;;
        esac
    done || true
}

cmd_verify() {
    local base_dir="$DEFAULT_DIR"

    while [ $# -gt 0 ]; do
        case "$1" in
            --dir) base_dir="$2"; shift 2 ;;
            *) shift ;;
        esac
    done

    # If custom --dir specified, append package name as subdirectory
    local install_dir="$base_dir"
    if [ "$base_dir" != "$DEFAULT_DIR" ]; then
        install_dir="$base_dir/$PKG_NAME"
    fi

    if [ ! -f "$install_dir/VERSION" ]; then
        echo "$PKG_NAME is not installed"
        exit 1
    fi

    echo "Verifying $PKG_NAME $PKG_VERSION..."
    echo ""

    local fail=0
    grep -A9999 '^\[sha\]' "$install_dir/VERSION" | while IFS= read -r line; do
        case "$line" in
            \"*\")
                temp="${line#\"}"
                file="${temp%%\" *}"
                temp="${line##*= \"}"
                expected="${temp%\"}"
                if [ ! -f "$install_dir/$file" ]; then
                    echo "MISSING: $file"
                    exit 1
                fi
                actual=$(compute_sha1 "$install_dir/$file")
                if [ "$expected" = "$actual" ]; then
                    echo "OK: $file"
                else
                    echo "FAIL: $file (expected $expected, got $actual)"
                    exit 1
                fi
                ;;
        esac
    done || fail=1

    if [ "$fail" -ne 0 ]; then
        echo ""
        echo "Verification FAILED"
        exit 1
    fi

    echo ""
    echo "Verification complete."
}

`)

	b.WriteString(`COMMAND="${1:-}"
shift 2>/dev/null || true

case "$COMMAND" in
    install)   cmd_install "$@" ;;
    uninstall) cmd_uninstall "$@" ;;
    version)   cmd_version "$@" ;;
    list)      cmd_list "$@" ;;
    verify)    cmd_verify "$@" ;;
    *)
        echo "$PKG_NAME $PKG_VERSION ($PKG_PLATFORM)"
        echo ""
        echo "Usage: $0 <command> [options]"
        echo ""
        echo "Commands:"
        echo "  install [--dir <path>]  Install to default or custom directory (requires root)"
        echo "  uninstall [--dir <path>] Remove installation and service (requires root)"
        echo "  version [--dir <path>] Show installed version"
        echo "  list [--dir <path>]    List installed files"
        echo "  verify [--dir <path>]  Verify file checksums"
        exit 1
        ;;
esac
`)

	return b.String(), nil
}
