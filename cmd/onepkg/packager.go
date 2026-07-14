package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type FileEntry struct {
	RelPath string
	AbsPath string
	SHA1    string
	Mode    fs.FileMode
	Size    int64
}

// ResolvedItem represents an item path after platform resolution
type ResolvedItem struct {
	Original string // Original item from config (e.g., "bin/myapp.{platform}")
	Resolved string // Resolved path (e.g., "bin/myapp.linux-arm64")
	PackPath string // Path to use in package (e.g., "bin/myapp" - marker stripped)
}

// ResolveItems resolves {platform} markers in items for a specific platform
// Returns error if a platform-specific file does not exist (no fallback)
func ResolveItems(configDir string, items []string, platform string) ([]ResolvedItem, error) {
	var resolved []ResolvedItem
	for _, item := range items {
		ri := ResolvedItem{
			Original: item,
		}

		if HasPlatformMarker(item) {
			ri.Resolved = ResolvePlatformItem(item, platform)
			ri.PackPath = StripPlatformMarker(item)

			// Verify the platform-specific file exists - NO FALLBACK
			fullPath := filepath.Join(configDir, ri.Resolved)
			if _, err := os.Stat(fullPath); err != nil {
				return nil, fmt.Errorf("item %q (resolved to %q) does not exist for platform %s", item, ri.Resolved, platform)
			}
		} else {
			// Literal item - no resolution needed
			ri.Resolved = item
			ri.PackPath = item
		}

		resolved = append(resolved, ri)
	}
	return resolved, nil
}

// execTargetPaths returns the set of config-form exec references that name
// bundled items: every non-empty commands[].exec plus the single service exec
// (cfg.Services[0].Exec). The keys are identical to an item's stripped pack path
// (e.g. "bin/onepkg"), so applyExeExtension can match items to execs directly.
func execTargetPaths(cfg *Config) map[string]bool {
	targets := make(map[string]bool)
	for _, c := range cfg.Commands {
		if c.Exec != "" {
			targets[c.Exec] = true
		}
	}
	if len(cfg.Services) > 0 && cfg.Services[0].Exec != "" {
		targets[cfg.Services[0].Exec] = true
	}
	return targets
}

// applyExeExtension appends ".exe" to the pack path of any resolved item that is
// both an exec target and extensionless. It never touches items that already have
// a file extension or that are not referenced by an exec. This is the Windows-only
// pass that lets an extensionless {platform} binary (packed as "bin/onepkg") be
// launched by Windows (as "bin/onepkg.exe"). The match uses the pre-extension
// PackPath, which equals the config-form exec string that itemMatches validates.
func applyExeExtension(resolved []ResolvedItem, execTargets map[string]bool) []ResolvedItem {
	for i := range resolved {
		ri := &resolved[i]
		if execTargets[ri.PackPath] && filepath.Ext(ri.PackPath) == "" {
			ri.PackPath += ".exe"
		}
	}
	return resolved
}

// resolveWindowsItemPath maps a config reference (a command/service exec, or an
// overlay item) to its actual packed path. It returns ref unchanged when an entry
// matches exactly (already-extensioned or non-extended refs); otherwise ref+".exe"
// when that matches (the Windows auto-extension); otherwise ref unchanged as a
// defensive fallback. With a valid config the fallback is unreachable for exec and
// overlay refs.
func resolveWindowsItemPath(entries []FileEntry, ref string) string {
	if ref == "" {
		return ""
	}
	for _, e := range entries {
		if e.RelPath == ref {
			return ref
		}
	}
	withExe := ref + ".exe"
	for _, e := range entries {
		if e.RelPath == withExe {
			return withExe
		}
	}
	return ref
}

// ResolveItemsForPlatform is the single entry point for the per-platform item
// pipeline: it resolves {platform} markers, applies the Windows .exe extension to
// executed extensionless items, and collects the file entries. main()'s build loop
// and the build tests both use it so the .exe wiring lives in exactly one place.
func ResolveItemsForPlatform(configDir string, cfg *Config, platform string) ([]FileEntry, error) {
	resolved, err := ResolveItems(configDir, cfg.Items, platform)
	if err != nil {
		return nil, err
	}
	goos := strings.SplitN(platform, "-", 2)[0]
	if goos == "windows" {
		resolved = applyExeExtension(resolved, execTargetPaths(cfg))
	}
	return CollectFilesResolved(configDir, resolved, cfg.Exclude)
}

func CollectFiles(configDir string, items []string) ([]FileEntry, error) {
	return CollectFilesWithExclude(configDir, items, nil)
}

// CollectFilesWithExclude collects files from items, excluding paths matching patterns.
// Patterns:
//   - "tests/" - exclude directory and all contents (prefix match)
//   - "*.md" - exclude files matching glob pattern at basename level
//   - ".git*" - exclude files/dirs matching prefix glob
func CollectFilesWithExclude(configDir string, items []string, exclude []string) ([]FileEntry, error) {
	var entries []FileEntry
	for _, item := range items {
		cleanItem := strings.TrimRight(item, "/")
		fullPath := filepath.Join(configDir, cleanItem)

		info, err := os.Stat(fullPath)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", item, err)
		}

		if info.IsDir() {
			err = filepath.WalkDir(fullPath, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					return nil
				}
				relPath, err := filepath.Rel(configDir, path)
				if err != nil {
					return err
				}
				// Check exclude patterns
				if IsExcluded(relPath, exclude) {
					return nil // skip this file
				}
				entry, err := makeFileEntry(configDir, relPath)
				if err != nil {
					return err
				}
				entries = append(entries, entry)
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("walk %s: %w", item, err)
			}
		} else {
			// Check exclude for single file
			if IsExcluded(cleanItem, exclude) {
				continue // skip this item
			}
			entry, err := makeFileEntry(configDir, cleanItem)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// IsExcluded checks if relPath matches any exclude pattern
func IsExcluded(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if MatchesExcludePattern(relPath, pattern) {
			return true
		}
	}
	return false
}

// MatchesExcludePattern checks if relPath matches a single exclude pattern.
// Pattern types:
//   - "dir/" - matches any path starting with "dir/" (directory exclusion)
//   - "*.ext" - matches basename against glob at ROOT LEVEL only (not subdirs)
//   - "prefix*" - matches if relPath starts with prefix
//   - "exact" - exact match
//   - "dir/*.ext" - glob in subdirectory (matches files in dir/ with .ext)
func MatchesExcludePattern(relPath, pattern string) bool {
	// Directory pattern: "tests/" matches "tests/file.go" and "tests/sub/file.go"
	if strings.HasSuffix(pattern, "/") {
		prefix := strings.TrimSuffix(pattern, "/")
		return strings.HasPrefix(relPath, prefix+"/") || relPath == prefix
	}

	// Path glob pattern: "docs/*.md" matches "docs/README.md"
	if strings.Contains(pattern, "/") {
		// Use filepath.Match for path-aware globbing
		matched, err := filepath.Match(pattern, relPath)
		if err == nil && matched {
			return true
		}
		// Also check if pattern is a directory prefix + glob
		// e.g., "docs/*.md" should match files in docs/ with .md extension
		patternDir, patternGlob := filepath.Dir(pattern), filepath.Base(pattern)
		relDir, relBase := filepath.Dir(relPath), filepath.Base(relPath)
		// Normalize "." to empty string for root
		if patternDir == "." {
			patternDir = ""
		}
		if relDir == "." {
			relDir = ""
		}
		if relDir == patternDir {
			matched, err := filepath.Match(patternGlob, relBase)
			if err == nil && matched {
				return true
			}
		}
		return false
	}

	// Simple glob at ROOT LEVEL only: "*.md" matches "README.md" but not "docs/README.md"
	basename := filepath.Base(relPath)
	matched, err := filepath.Match(pattern, basename)
	if err == nil && matched {
		// Only match if file is at root level (no directory in path)
		relDir := filepath.Dir(relPath)
		if relDir == "." || relDir == "" {
			return true
		}
	}

	// Prefix glob: ".git*" matches ".gitignore" and ".git/config"
	// Check if relPath starts with the prefix (before *)
	if strings.Contains(pattern, "*") {
		prefix := strings.SplitN(pattern, "*", 2)[0]
		if prefix != "" && strings.HasPrefix(relPath, prefix) {
			return true
		}
	}

	// Exact match
	return relPath == pattern
}

// CollectFilesResolved collects files from resolved items.
// It reads files from Resolved paths but packs them with PackPath (marker stripped).
// Exclude patterns are applied to the PackPath (the final path in the package).
func CollectFilesResolved(configDir string, items []ResolvedItem, exclude []string) ([]FileEntry, error) {
	var entries []FileEntry
	for _, item := range items {
		cleanResolved := strings.TrimRight(item.Resolved, "/")
		cleanPackPath := strings.TrimRight(item.PackPath, "/")
		fullPath := filepath.Join(configDir, cleanResolved)

		info, err := os.Stat(fullPath)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", item.Resolved, err)
		}

		if info.IsDir() {
			err = filepath.WalkDir(fullPath, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					return nil
				}
				// Calculate relative path from the resolved directory
				relToResolved, err := filepath.Rel(fullPath, path)
				if err != nil {
					return err
				}
				// Combine with PackPath for the final RelPath
				packRelPath := filepath.Join(cleanPackPath, relToResolved)
				// Check exclude patterns against PackPath
				if IsExcluded(packRelPath, exclude) {
					return nil // skip this file
				}
				entry, err := makeFileEntryWithPackPath(configDir, path, packRelPath)
				if err != nil {
					return err
				}
				entries = append(entries, entry)
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("walk %s: %w", item.Resolved, err)
			}
		} else {
			// Check exclude for single file (against PackPath)
			if IsExcluded(cleanPackPath, exclude) {
				continue // skip this item
			}
			entry, err := makeFileEntryWithPackPath(configDir, fullPath, cleanPackPath)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// makeFileEntryWithPackPath creates a FileEntry reading from absPath but using packPath as RelPath.
func makeFileEntryWithPackPath(configDir, absPath, packPath string) (FileEntry, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("read %s: %w", absPath, err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("stat %s: %w", absPath, err)
	}
	hash := sha1.Sum(data)
	return FileEntry{
		RelPath: packPath,
		AbsPath: absPath,
		SHA1:    hex.EncodeToString(hash[:]),
		Mode:    info.Mode(),
		Size:    info.Size(),
	}, nil
}

func makeFileEntry(configDir, relPath string) (FileEntry, error) {
	absPath := filepath.Join(configDir, relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("read %s: %w", relPath, err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("stat %s: %w", relPath, err)
	}
	hash := sha1.Sum(data)
	return FileEntry{
		RelPath: relPath,
		AbsPath: absPath,
		SHA1:    hex.EncodeToString(hash[:]),
		Mode:    info.Mode(),
		Size:    info.Size(),
	}, nil
}

func BuildTarGZ(entries []FileEntry, preservePermissions bool) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, entry := range entries {
		data, err := os.ReadFile(entry.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.RelPath, err)
		}

		// Determine mode based on preservePermissions setting
		mode := entry.Mode
		if !preservePermissions {
			// Normalize permissions: executables -> 0755, others -> 0644
			mode = normalizedMode(entry.Mode)
		}

		hdr := &tar.Header{
			Name: entry.RelPath,
			Mode: int64(mode),
			Size: entry.Size,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("tar header %s: %w", entry.RelPath, err)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, fmt.Errorf("tar write %s: %w", entry.RelPath, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("close gzip: %w", err)
	}

	return buf.Bytes(), nil
}

// normalizedMode returns standard permissions: 0755 for executables, 0644 for others
func normalizedMode(mode fs.FileMode) fs.FileMode {
	if mode&0111 != 0 { // has any execute bit
		return 0755
	}
	return 0644
}

// VirtualFile is an in-memory file to add to an archive (e.g., generated VERSION file)
type VirtualFile struct {
	RelPath string
	Content []byte
	Mode    fs.FileMode
}

// BuildZip creates a ZIP archive from file entries (for Windows installers)
func BuildZip(entries []FileEntry, preservePermissions bool) ([]byte, error) {
	return BuildZipWithExtras(entries, preservePermissions, nil)
}

// BuildZipWithExtras creates a ZIP archive from file entries plus in-memory virtual files
func BuildZipWithExtras(entries []FileEntry, preservePermissions bool, extras []VirtualFile) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, entry := range entries {
		data, err := os.ReadFile(entry.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.RelPath, err)
		}

		mode := entry.Mode
		if !preservePermissions {
			mode = normalizedMode(entry.Mode)
		}

		hdr := &zip.FileHeader{
			Name:   entry.RelPath,
			Method: zip.Deflate,
		}
		hdr.SetMode(mode)

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, fmt.Errorf("zip header %s: %w", entry.RelPath, err)
		}
		if _, err := w.Write(data); err != nil {
			return nil, fmt.Errorf("zip write %s: %w", entry.RelPath, err)
		}
	}

	for _, extra := range extras {
		hdr := &zip.FileHeader{
			Name:   extra.RelPath,
			Method: zip.Deflate,
		}
		hdr.SetMode(extra.Mode)

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, fmt.Errorf("zip header %s: %w", extra.RelPath, err)
		}
		if _, err := w.Write(extra.Content); err != nil {
			return nil, fmt.Errorf("zip write %s: %w", extra.RelPath, err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}

	return buf.Bytes(), nil
}
