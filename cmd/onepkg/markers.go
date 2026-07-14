package main

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// envMarkerRe matches a {env.NAME} token. NAME is an OS environment variable
// name: a letter or underscore, then word characters.
var envMarkerRe = regexp.MustCompile(`\{env\.([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv replaces every {env.NAME} token in the config's string fields with the
// build host's value of os.Getenv(NAME). A referenced variable that is unset or
// empty is a configuration error: ExpandEnv collects all offenders and returns a
// single error listing them (deduped, sorted). {platform} and {dst.NAME} are left
// untouched (different prefixes), so they can coexist with {env.NAME} in one field.
func (c *Config) ExpandEnv() error {
	var missing []string
	seen := map[string]bool{}
	walkStrings(reflect.ValueOf(c), func(_ string, s *string) {
		expanded, miss := expandEnvString(*s)
		*s = expanded
		for _, name := range miss {
			if !seen[name] {
				seen[name] = true
				missing = append(missing, name)
			}
		}
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("environment variables referenced via {env.NAME} are unset or empty: %s", strings.Join(missing, ", "))
	}
	return nil
}

// expandEnvString substitutes {env.NAME} in s. Returns the substituted string and
// the list of referenced variable names that were unset or empty (the marker is
// left in place in the returned string; the caller errors out before use).
func expandEnvString(s string) (string, []string) {
	var missing []string
	out := envMarkerRe.ReplaceAllStringFunc(s, func(tok string) string {
		name := envMarkerRe.FindStringSubmatch(tok)[1]
		if val := os.Getenv(name); val != "" {
			return val
		}
		missing = append(missing, name)
		return tok
	})
	return out, missing
}

// walkStrings visits every settable string field reachable from v (a *Config),
// invoking fn with a dotted field path (e.g. "Dirs[0].Path", "Services[0].Linux.Environment[KEY]")
// and a pointer to that field's value. It descends structs, pointers, slices, and
// maps; map keys and non-string scalars are skipped. Map values are substituted via
// copy-out/copy-in (map values are not addressable). Config has no map-of-struct, so
// the String branch is only reached through settable struct/slice paths.
//
// It is the shared traversal for {env.NAME} substitution (fn ignores the path) and
// stray {dst.NAME} validation (fn uses the path to allow-list supported fields).
func walkStrings(v reflect.Value, fn func(path string, s *string)) {
	walkStringsInto(v, "", fn)
}

func walkStringsInto(v reflect.Value, path string, fn func(path string, s *string)) {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		walkStringsInto(v.Elem(), path, fn)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			child := t.Field(i).Name
			if path != "" {
				child = path + "." + child
			}
			walkStringsInto(v.Field(i), child, fn)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkStringsInto(v.Index(i), fmt.Sprintf("%s[%d]", path, i), fn)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			child := v.MapIndex(k)
			if child.Kind() != reflect.String {
				continue
			}
			cur := child.String()
			fn(fmt.Sprintf("%s[%s]", path, k.String()), &cur)
			v.SetMapIndex(k, reflect.ValueOf(cur))
		}
	case reflect.String:
		cur := v.String()
		fn(path, &cur)
		v.SetString(cur)
	}
}

// dstMarkerRe matches any {dst. token (known or unknown name). Used by the stray
// check to catch {dst.NAME} used in fields that do not resolve destination markers.
var dstMarkerRe = regexp.MustCompile(`\{dst\.`)

// dstSupportedFieldRe matches the dotted field paths of the four field types that
// resolve {dst.NAME} at install/run time: dirs[].path, system_overlay[].system,
// and run-mode commands[].args / commands[].shell.
var dstSupportedFieldRe = regexp.MustCompile(`^(Dirs\[\d+\]\.Path|SystemOverlay\[\d+\]\.System|Commands\[\d+\]\.Args\[\d+\]|Commands\[\d+\]\.Shell)$`)

func isDstSupportedField(path string) bool {
	return dstSupportedFieldRe.MatchString(path)
}

// validateNoStrayDstMarkers errors if any {dst.NAME} marker appears in a string
// field that is not one of the four supported types. A {dst.NAME} elsewhere would
// survive literally into the package, so fail fast.
func (c *Config) validateNoStrayDstMarkers() error {
	var offenders []string
	walkStrings(reflect.ValueOf(c), func(path string, s *string) {
		if dstMarkerRe.MatchString(*s) && !isDstSupportedField(path) {
			offenders = append(offenders, path)
		}
	})
	if len(offenders) > 0 {
		sort.Strings(offenders)
		return fmt.Errorf("{dst.NAME} markers are only valid in dirs[].path, system_overlay[].system, and run commands[].args/shell; found in: %s", strings.Join(offenders, ", "))
	}
	return nil
}
