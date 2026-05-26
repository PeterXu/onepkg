package main

import (
	"archive/tar"
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

func CollectFiles(configDir string, items []string) ([]FileEntry, error) {
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
			entry, err := makeFileEntry(configDir, cleanItem)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// CollectFilesResolved collects files from resolved items.
// It reads files from Resolved paths but packs them with PackPath (marker stripped).
func CollectFilesResolved(configDir string, items []ResolvedItem) ([]FileEntry, error) {
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
