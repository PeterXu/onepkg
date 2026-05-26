package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectFiles(t *testing.T) {
	dir := t.TempDir()

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	etcDir := filepath.Join(dir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}

	bin1 := filepath.Join(binDir, "test1")
	if err := os.WriteFile(bin1, []byte("binary1"), 0755); err != nil {
		t.Fatal(err)
	}
	bin2 := filepath.Join(binDir, "test2")
	if err := os.WriteFile(bin2, []byte("binary2"), 0755); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(etcDir, "gate.conf")
	if err := os.WriteFile(conf, []byte("config-data"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := CollectFiles(dir, []string{"bin/test1", "bin/test2", "etc/gate.conf"})
	if err != nil {
		t.Fatalf("CollectFiles returned error: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}

	pathMap := make(map[string]*FileEntry)
	for i := range entries {
		pathMap[entries[i].RelPath] = &entries[i]
	}

	if _, ok := pathMap["bin/test1"]; !ok {
		t.Error("missing bin/test1")
	}
	if _, ok := pathMap["bin/test2"]; !ok {
		t.Error("missing bin/test2")
	}
	if _, ok := pathMap["etc/gate.conf"]; !ok {
		t.Error("missing etc/gate.conf")
	}

	for _, e := range entries {
		if e.SHA1 == "" {
			t.Errorf("SHA1 empty for %s", e.RelPath)
		}
		if e.Mode == 0 {
			t.Errorf("Mode not set for %s", e.RelPath)
		}
	}
}

func TestCollectFilesDirectory(t *testing.T) {
	dir := t.TempDir()

	resDir := filepath.Join(dir, "res")
	if err := os.MkdirAll(resDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "a.png"), []byte("img-a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "b.png"), []byte("img-b"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := CollectFiles(dir, []string{"res/"})
	if err != nil {
		t.Fatalf("CollectFiles returned error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	pathMap := make(map[string]string)
	for _, e := range entries {
		pathMap[e.RelPath] = e.SHA1
	}

	if _, ok := pathMap["res/a.png"]; !ok {
		t.Error("missing res/a.png")
	}
	if _, ok := pathMap["res/b.png"]; !ok {
		t.Error("missing res/b.png")
	}
}

func TestCollectFilesPreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "run"), []byte("exec"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "data"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := CollectFiles(dir, []string{"bin/run", "bin/data"})
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if e.RelPath == "bin/run" {
			if e.Mode&0111 == 0 {
				t.Errorf("bin/run mode = %o, expected execute bit set", e.Mode)
			}
		}
		if e.RelPath == "bin/data" {
			if e.Mode&0111 != 0 {
				t.Errorf("bin/data mode = %o, expected no execute bit", e.Mode)
			}
		}
	}
}

func TestBuildTarGZ(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("hello"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "app.conf"), []byte("key=val"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := CollectFiles(dir, []string{"bin/app", "etc/app.conf"})
	if err != nil {
		t.Fatal(err)
	}

	tarData, err := BuildTarGZ(entries)
	if err != nil {
		t.Fatalf("BuildTarGZ returned error: %v", err)
	}

	gzReader, err := gzip.NewReader(bytes.NewReader(tarData))
	if err != nil {
		t.Fatalf("gzip reader error: %v", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	found := map[string]bool{}
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read error: %v", err)
		}
		found[hdr.Name] = true

		if hdr.Name == "bin/app" {
			if hdr.Mode&0111 == 0 {
				t.Errorf("bin/app mode in tar = %o, expected execute bit", hdr.Mode)
			}
		}
	}

	if !found["bin/app"] {
		t.Error("tar missing bin/app")
	}
	if !found["etc/app.conf"] {
		t.Error("tar missing etc/app.conf")
	}
}
