package installer

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// KnownFolders maps a config variable name (the MSI/WiX folder property name)
// to its .NET SpecialFolder enum value, resolved on the Windows target at
// install/run time via [Environment]::GetFolderPath. This map is the single
// source of truth for valid variable names; config validation and the
// build-time guard both consult it.
var KnownFolders = map[string]string{
	"AppDataFolder":         "ApplicationData",
	"LocalAppDataFolder":    "LocalApplicationData",
	"CommonAppDataFolder":   "CommonApplicationData",
	"ProgramFilesFolder":    "ProgramFilesX86",
	"ProgramFiles64Folder":  "ProgramFiles",
	"WindowsFolder":         "Windows",
	"SystemFolder":          "System",
	"DesktopFolder":         "DesktopDirectory",
	"ProgramMenuFolder":     "Programs",
	"StartupFolder":         "Startup",
	"PersonalFolder":        "MyDocuments",
	"CommonDocumentsFolder": "CommonDocuments",
}

// crossPlatformDst maps a cross-platform {dst.NAME} marker to its resolution
// token on unix (bash; identical for linux and darwin) and windows (PowerShell).
// Each is an env/automatic variable on the target, so no runtime definition line
// is emitted for them (unlike the Windows-only KnownFolders, which define
// $<Name> = [Environment]::GetFolderPath(...)).
var crossPlatformDst = map[string]struct{ unix, windows string }{
	"HOME":     {"$HOME", "$HOME"},
	"TMPDIR":   {"${TMPDIR:-/tmp}", "$env:TEMP"},
	"USER":     {"${USER:-$(id -un)}", "$env:USERNAME"},
	"HOSTNAME": {"${HOSTNAME:-$(hostname)}", "$env:COMPUTERNAME"},
}

// crossPlatformDstToken returns the target-side resolution token for a
// cross-platform {dst.NAME} on goos, and whether name is one. linux and darwin
// share the unix token.
func crossPlatformDstToken(name, goos string) (string, bool) {
	e, ok := crossPlatformDst[name]
	if !ok {
		return "", false
	}
	if goos == "windows" {
		return e.windows, true
	}
	return e.unix, true
}

// isKnownFolderName reports whether name is a resolvable {dst.NAME} marker name
// (a Windows-only known folder, or a cross-platform marker like HOME/TMPDIR/...).
func isKnownFolderName(name string) bool {
	if _, ok := KnownFolders[name]; ok {
		return true
	}
	_, ok := crossPlatformDst[name]
	return ok
}

// isWindowsOnlyFolderName reports whether name resolves only on Windows.
func isWindowsOnlyFolderName(name string) bool {
	_, ok := KnownFolders[name]
	return ok
}

// markerRe matches a {dst.Name} token in path-string config fields. The captured
// group is the bare folder name (e.g. "AppDataFolder"), which keys KnownFolders.
var markerRe = regexp.MustCompile(`\{dst\.(\w+)\}`)

// HasKnownFolderMarker reports whether s contains at least one {KnownName} token.
// Unknown braces such as {Foo} do not count; rejecting those is
// ValidateKnownFolderMarkers's job.
func HasKnownFolderMarker(s string) bool {
	return len(KnownFolderMarkers(s)) > 0
}

// ValidateKnownFolderMarkers returns an error if s contains any {dst.X} token whose
// name X is not a known folder. Known markers are allowed.
func ValidateKnownFolderMarkers(s string) error {
	for _, m := range markerRe.FindAllStringSubmatch(s, -1) {
		if !isKnownFolderName(m[1]) {
			return fmt.Errorf("unknown Windows known-folder variable {dst.%s}; supported: %s", m[1], knownFolderNameList())
		}
	}
	return nil
}

// KnownFolderMarkers returns the deduped, sorted known-folder names that appear
// as {Name} tokens in s.
func KnownFolderMarkers(s string) []string {
	var names []string
	for _, m := range markerRe.FindAllStringSubmatch(s, -1) {
		if isKnownFolderName(m[1]) {
			names = append(names, m[1])
		}
	}
	return dedupeSorted(names)
}

// ReplaceMarkers replaces each known {dst.Name} token in s with the PowerShell
// variable reference $Name, for inline use inside run-mode shell expressions.
// Cross-platform markers (HOME/TMPDIR/USER/HOSTNAME) become their PowerShell
// env/automatic variable; unknown {dst.X} tokens are left in place (validation
// rejects them earlier).
func ReplaceMarkers(s string) string {
	return markerRe.ReplaceAllStringFunc(s, func(tok string) string {
		m := markerRe.FindStringSubmatch(tok)
		if replacement, ok := crossPlatformDstToken(m[1], "windows"); ok {
			return replacement
		}
		if _, ok := KnownFolders[m[1]]; ok {
			return "$" + m[1]
		}
		return tok
	})
}

// ReplaceCrossPlatformDst rewrites every cross-platform {dst.NAME} in s to its
// resolution token on goos (linux/darwin -> bash, windows -> PowerShell). The
// unix builders bake the resulting token into the generated .sh (the shell
// expands it at runtime); Windows renders via EmitPathExpr/ReplaceMarkers
// instead. Unknown {dst.X} are left in place; validation rejects them earlier.
func ReplaceCrossPlatformDst(s, goos string) string {
	return markerRe.ReplaceAllStringFunc(s, func(tok string) string {
		m := markerRe.FindStringSubmatch(tok)
		if replacement, ok := crossPlatformDstToken(m[1], goos); ok {
			return replacement
		}
		return tok
	})
}

// EmitKnownFolderDefs emits a "$<Name> = [Environment]::GetFolderPath(...)" line
// for each Windows-only name in names (deduped, sorted). Cross-platform markers
// (HOME/TMPDIR/USER/HOSTNAME) are env/automatic variables and get no definition.
// Empty input produces empty output.
func EmitKnownFolderDefs(names []string) string {
	var b strings.Builder
	for _, n := range dedupeSorted(names) {
		if _, ok := crossPlatformDst[n]; ok {
			continue // cross-platform markers need no runtime definition
		}
		fmt.Fprintf(&b, "$%s = [Environment]::GetFolderPath([Environment+SpecialFolder]::%s)\n", n, KnownFolders[n])
	}
	return b.String()
}

// EmitPathExpr renders a config path value as a PowerShell expression.
// A value without markers is a single-quoted literal (identical to psQuote).
// A value with {dst.Name} markers is a concatenation referencing the
// runtime-resolved $<Name> variable, e.g. "$AppDataFolder + '\MyApp'".
// Unknown {dst.X} tokens are left as single-quoted literals (safety; validation
// rejects them earlier).
func EmitPathExpr(s string) string {
	if !markerRe.MatchString(s) {
		return psQuote(s)
	}
	var parts []string
	idx := 0
	for _, m := range markerRe.FindAllStringSubmatchIndex(s, -1) {
		start, end := m[0], m[1]
		nameStart, nameEnd := m[2], m[3]
		if start > idx {
			parts = append(parts, psQuote(s[idx:start]))
		}
		name := s[nameStart:nameEnd]
		if tok, ok := crossPlatformDstToken(name, "windows"); ok {
			parts = append(parts, tok)
		} else if _, ok := KnownFolders[name]; ok {
			parts = append(parts, "$"+name)
		} else {
			parts = append(parts, psQuote(s[start:end]))
		}
		idx = end
	}
	if idx < len(s) {
		parts = append(parts, psQuote(s[idx:]))
	}
	return strings.Join(parts, " + ")
}

func dedupeSorted(names []string) []string {
	seen := make(map[string]bool, len(names))
	var out []string
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func knownFolderNameList() string {
	names := make([]string, 0, len(KnownFolders))
	for n := range KnownFolders {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// HasWindowsOnlyKnownFolderMarker reports whether s contains any {dst.NAME}
// marker that resolves only on Windows (i.e. a KnownFolders entry, excluding
// cross-platform markers like HOME). The non-Windows build guard uses this so
// {dst.HOME} is permitted on Linux/macOS while {dst.AppDataFolder} is not.
func HasWindowsOnlyKnownFolderMarker(s string) bool {
	for _, m := range markerRe.FindAllStringSubmatch(s, -1) {
		if isWindowsOnlyFolderName(m[1]) {
			return true
		}
	}
	return false
}
