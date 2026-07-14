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

// markerRe matches a {Name} token in path-string config fields.
var markerRe = regexp.MustCompile(`\{(\w+)\}`)

// dollarRe matches a $Name token as written in run-mode shell expressions.
var dollarRe = regexp.MustCompile(`\$([A-Za-z_]\w*)`)

// HasKnownFolderMarker reports whether s contains at least one {KnownName} token.
// Unknown braces such as {Foo} do not count; rejecting those is
// ValidateKnownFolderMarkers's job.
func HasKnownFolderMarker(s string) bool {
	return len(KnownFolderMarkers(s)) > 0
}

// ValidateKnownFolderMarkers returns an error if s contains any {X} token whose
// name X is not a known folder. Known markers are allowed.
func ValidateKnownFolderMarkers(s string) error {
	for _, m := range markerRe.FindAllStringSubmatch(s, -1) {
		if _, ok := KnownFolders[m[1]]; !ok {
			return fmt.Errorf("unknown Windows known-folder variable {%s}; supported: %s", m[1], knownFolderNameList())
		}
	}
	return nil
}

// KnownFolderMarkers returns the deduped, sorted known-folder names that appear
// as {Name} tokens in s.
func KnownFolderMarkers(s string) []string {
	var names []string
	for _, m := range markerRe.FindAllStringSubmatch(s, -1) {
		if _, ok := KnownFolders[m[1]]; ok {
			names = append(names, m[1])
		}
	}
	return dedupeSorted(names)
}

// KnownFolderDollarVars returns the deduped, sorted known-folder names that
// appear as $Name tokens in s (used to detect variables referenced in run shell).
func KnownFolderDollarVars(s string) []string {
	var names []string
	for _, m := range dollarRe.FindAllStringSubmatch(s, -1) {
		if _, ok := KnownFolders[m[1]]; ok {
			names = append(names, m[1])
		}
	}
	return dedupeSorted(names)
}

// EmitKnownFolderDefs emits a "$<Name> = [Environment]::GetFolderPath(...)" line
// for each name in names (deduped, sorted). Empty input produces empty output.
func EmitKnownFolderDefs(names []string) string {
	var b strings.Builder
	for _, n := range dedupeSorted(names) {
		fmt.Fprintf(&b, "$%s = [Environment]::GetFolderPath([Environment+SpecialFolder]::%s)\n", n, KnownFolders[n])
	}
	return b.String()
}

// EmitPathExpr renders a config path value as a PowerShell expression.
// A value without markers is a single-quoted literal (identical to psQuote).
// A value with {KnownName} markers is a concatenation referencing the
// runtime-resolved $<Name> variable, e.g. "$AppDataFolder + '\MyApp'".
// Unknown {X} tokens are left as single-quoted literals (safety; validation
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
		if _, ok := KnownFolders[name]; ok {
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
