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

func BuildTarGZ(entries []FileEntry) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, entry := range entries {
		data, err := os.ReadFile(entry.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.RelPath, err)
		}

		hdr := &tar.Header{
			Name: entry.RelPath,
			Mode: int64(entry.Mode),
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
