package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
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

	tarData, err := BuildTarGZ(entries, true) // preservePermissions=true (default behavior)
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

func TestResolveItems(t *testing.T) {
	dir := t.TempDir()

	// Create platform-specific binaries
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "myapp.linux-arm64"), []byte("binary-linux-arm64"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "myapp.linux-amd64"), []byte("binary-linux-amd64"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "helper.linux-arm64"), []byte("helper-linux"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create non-platform-specific files
	etcDir := filepath.Join(dir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "config.conf"), []byte("config-data"), 0644); err != nil {
		t.Fatal(err)
	}

	items := []string{
		"bin/myapp.{platform}",
		"bin/helper.{platform}",
		"etc/config.conf",
	}

	resolved, err := ResolveItems(dir, items, "linux-arm64")
	if err != nil {
		t.Fatalf("ResolveItems returned error: %v", err)
	}

	// Check resolved items have correct paths
	expectedResolved := map[string]string{
		"bin/myapp.{platform}":  "bin/myapp.linux-arm64",
		"bin/helper.{platform}": "bin/helper.linux-arm64",
		"etc/config.conf":       "etc/config.conf",
	}

	for original, wantResolved := range expectedResolved {
		found := false
		for _, r := range resolved {
			if r.Original == original && r.Resolved == wantResolved {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing resolved item for %q: want resolved=%q", original, wantResolved)
		}
	}

	// Check that files exist
	for _, r := range resolved {
		fullPath := filepath.Join(dir, r.Resolved)
		if _, err := os.Stat(fullPath); err != nil {
			t.Errorf("resolved file %q does not exist at %s", r.Resolved, fullPath)
		}
	}
}

func TestResolveItemsMissingPlatformFile(t *testing.T) {
	dir := t.TempDir()

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Only create linux-amd64, not linux-arm64
	if err := os.WriteFile(filepath.Join(binDir, "myapp.linux-amd64"), []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}

	items := []string{"bin/myapp.{platform}"}

	_, err := ResolveItems(dir, items, "linux-arm64")
	if err == nil {
		t.Fatal("expected error for missing platform file")
	}
	if !strings.Contains(err.Error(), "bin/myapp.linux-arm64") {
		t.Errorf("error should mention missing file, got: %v", err)
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should say 'does not exist', got: %v", err)
	}
}

func TestResolveItemsNoFallback(t *testing.T) {
	dir := t.TempDir()

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create base file but NOT platform-specific file
	if err := os.WriteFile(filepath.Join(binDir, "myapp"), []byte("base-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	items := []string{"bin/myapp.{platform}"}

	_, err := ResolveItems(dir, items, "linux-arm64")
	if err == nil {
		t.Fatal("expected error - should NOT fall back to base file")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should mention missing platform file, got: %v", err)
	}
}

func TestCollectFilesWithResolvedItems(t *testing.T) {
	dir := t.TempDir()

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	etcDir := filepath.Join(dir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Platform-specific binary
	if err := os.WriteFile(filepath.Join(binDir, "myapp.linux-arm64"), []byte("binary-linux"), 0755); err != nil {
		t.Fatal(err)
	}
	// Literal config file
	if err := os.WriteFile(filepath.Join(etcDir, "config.conf"), []byte("config-data"), 0644); err != nil {
		t.Fatal(err)
	}

	resolved := []ResolvedItem{
		{Original: "bin/myapp.{platform}", Resolved: "bin/myapp.linux-arm64", PackPath: "bin/myapp"},
		{Original: "etc/config.conf", Resolved: "etc/config.conf", PackPath: "etc/config.conf"},
	}

	entries, err := CollectFilesResolved(dir, resolved, nil) // no exclude patterns
	if err != nil {
		t.Fatalf("CollectFilesResolved returned error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	// Check that RelPath uses PackPath (marker stripped)
	pathMap := make(map[string]*FileEntry)
	for i := range entries {
		pathMap[entries[i].RelPath] = &entries[i]
	}

	// Should be "bin/myapp" (stripped), NOT "bin/myapp.linux-arm64"
	if _, ok := pathMap["bin/myapp"]; !ok {
		t.Error("missing bin/myapp (should have PackPath, not resolved path)")
	}
	if _, ok := pathMap["etc/config.conf"]; !ok {
		t.Error("missing etc/config.conf")
	}

	// Should NOT have the platform-suffixed path in RelPath
	if _, ok := pathMap["bin/myapp.linux-arm64"]; ok {
		t.Error("should not have bin/myapp.linux-arm64 in RelPath - marker should be stripped")
	}
}

func TestBuildTarGZPreservePermissionsFalse(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create files with various permissions
	// Executable (0755)
	if err := os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("hello"), 0755); err != nil {
		t.Fatal(err)
	}
	// Read-only executable (0500) - should become 0755 when normalized
	if err := os.WriteFile(filepath.Join(dir, "bin", "run"), []byte("runner"), 0500); err != nil {
		t.Fatal(err)
	}
	// Config file (0644)
	if err := os.WriteFile(filepath.Join(dir, "etc", "app.conf"), []byte("key=val"), 0644); err != nil {
		t.Fatal(err)
	}
	// Read-only config (0600) - should become 0644 when normalized
	if err := os.WriteFile(filepath.Join(dir, "etc", "secret.conf"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	entries, err := CollectFiles(dir, []string{"bin/app", "bin/run", "etc/app.conf", "etc/secret.conf"})
	if err != nil {
		t.Fatal(err)
	}

	tarData, err := BuildTarGZ(entries, false) // preservePermissions=false
	if err != nil {
		t.Fatalf("BuildTarGZ returned error: %v", err)
	}

	gzReader, err := gzip.NewReader(bytes.NewReader(tarData))
	if err != nil {
		t.Fatalf("gzip reader error: %v", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read error: %v", err)
		}

		// When preservePermissions=false, all files should have normalized permissions
		switch hdr.Name {
		case "bin/app", "bin/run":
			// Executables should be 0755
			if hdr.Mode != 0755 {
				t.Errorf("%s mode = %o, want 0755 (normalized executable)", hdr.Name, hdr.Mode)
			}
		case "etc/app.conf", "etc/secret.conf":
			// Non-executables should be 0644
			if hdr.Mode != 0644 {
				t.Errorf("%s mode = %o, want 0644 (normalized file)", hdr.Name, hdr.Mode)
			}
		}
	}
}

func TestBuildTarGZPreservePermissionsTrue(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create files with specific permissions that should be preserved
	if err := os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("hello"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "app.conf"), []byte("key=val"), 0600); err != nil {
		t.Fatal(err)
	}

	entries, err := CollectFiles(dir, []string{"bin/app", "etc/app.conf"})
	if err != nil {
		t.Fatal(err)
	}

	tarData, err := BuildTarGZ(entries, true) // preservePermissions=true
	if err != nil {
		t.Fatalf("BuildTarGZ returned error: %v", err)
	}

	gzReader, err := gzip.NewReader(bytes.NewReader(tarData))
	if err != nil {
		t.Fatalf("gzip reader error: %v", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read error: %v", err)
		}

		// When preservePermissions=true, original permissions should be preserved
		switch hdr.Name {
		case "bin/app":
			if hdr.Mode != 0755 {
				t.Errorf("bin/app mode = %o, want 0755 (original preserved)", hdr.Mode)
			}
		case "etc/app.conf":
			if hdr.Mode != 0600 {
				t.Errorf("etc/app.conf mode = %o, want 0600 (original preserved)", hdr.Mode)
			}
		}
	}
}

func TestMatchesExcludePattern(t *testing.T) {
	tests := []struct {
		relPath  string
		pattern  string
		expected bool
	}{
		// Directory patterns (trailing /)
		{"tests/test1.go", "tests/", true},
		{"tests/subdir/test2.go", "tests/", true},
		{"src/main.go", "tests/", false},
		{"libtests/main.go", "tests/", false}, // prefix match only

		// Glob patterns (*)
		{"README.md", "*.md", true},
		{"docs/README.md", "*.md", false},    // glob only matches basename
		{"CHANGELOG.md", "*.md", true},
		{"main.go", "*.md", false},

		// Prefix patterns (*suffix)
		{".gitignore", ".git*", true},
		{".git/config", ".git*", true},
		{".github/workflows.yml", ".git*", true},
		{"gitignore", ".git*", false}, // missing leading dot

		// Exact match
		{"config.local", "config.local", true},
		{"config.local.json", "config.local", false},

		// Combined patterns
		{"docs/README.md", "docs/*.md", true},
		{"docs/test.go", "docs/*.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.relPath+"_"+tt.pattern, func(t *testing.T) {
			result := MatchesExcludePattern(tt.relPath, tt.pattern)
			if result != tt.expected {
				t.Errorf("MatchesExcludePattern(%q, %q) = %v, want %v", tt.relPath, tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestCollectFilesWithExclude(t *testing.T) {
	dir := t.TempDir()

	// Create directory structure
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "tests"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create files
	if err := os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tests", "test.go"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create subdirectory first, then file
	if err := os.MkdirAll(filepath.Join(dir, "tests", "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tests", "sub", "test2.go"), []byte("test2"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "README.md"), []byte("doc"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("gitignore"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("config"), 0644); err != nil {
		t.Fatal(err)
	}

	// Collect all items with exclude patterns
	exclude := []string{"tests/", "*.md", ".git*"}
	entries, err := CollectFilesWithExclude(dir, []string{"bin/", "tests/", "docs/", "README.md", ".gitignore", ".git/"}, exclude)
	if err != nil {
		t.Fatal(err)
	}

	// Check excluded files are not in entries
	for _, e := range entries {
		// tests/ should be excluded
		if strings.HasPrefix(e.RelPath, "tests/") {
			t.Errorf("tests/ should be excluded, but found: %s", e.RelPath)
		}
		// *.md should exclude README.md (but not docs/README.md since glob matches basename only)
		if e.RelPath == "README.md" {
			t.Errorf("README.md should be excluded by *.md, but found")
		}
		// docs/*.md is NOT excluded by "*.md" - glob only matches basename at root level
		if strings.Contains(e.RelPath, ".git") {
			t.Errorf(".git* files should be excluded, but found: %s", e.RelPath)
		}
	}

	// Check included files are present
	foundBinApp := false
	for _, e := range entries {
		if e.RelPath == "bin/app" {
			foundBinApp = true
		}
	}
	if !foundBinApp {
		t.Error("bin/app should be included")
	}
}

func TestCollectFilesExcludeEmpty(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}

	// Empty exclude list - should collect everything
	entries, err := CollectFilesWithExclude(dir, []string{"bin/app"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Errorf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].RelPath != "bin/app" {
		t.Errorf("entries[0].RelPath = %q, want %q", entries[0].RelPath, "bin/app")
	}
}

func TestBuildZip(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	os.MkdirAll(filepath.Join(dir, "etc"), 0755)
	os.WriteFile(filepath.Join(dir, "bin", "app.exe"), []byte("hello"), 0755)
	os.WriteFile(filepath.Join(dir, "etc", "app.conf"), []byte("key=val"), 0644)

	entries, err := CollectFiles(dir, []string{"bin/app.exe", "etc/app.conf"})
	if err != nil {
		t.Fatal(err)
	}

	zipData, err := BuildZip(entries, true)
	if err != nil {
		t.Fatalf("BuildZip returned error: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip.NewReader error: %v", err)
	}

	found := map[string]bool{}
	for _, f := range zr.File {
		found[f.Name] = true
	}
	if !found["bin/app.exe"] {
		t.Error("zip missing bin/app.exe")
	}
	if !found["etc/app.conf"] {
		t.Error("zip missing etc/app.conf")
	}
}

func TestBuildZipWithExtras(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	os.WriteFile(filepath.Join(dir, "bin", "app.exe"), []byte("hello"), 0755)

	entries, err := CollectFiles(dir, []string{"bin/app.exe"})
	if err != nil {
		t.Fatal(err)
	}

	versionContent := "name = \"myapp\"\nversion = \"v1.0.0\"\n"
	zipData, err := BuildZipWithExtras(entries, true, []VirtualFile{
		{RelPath: "VERSION", Content: []byte(versionContent), Mode: 0644},
	})
	if err != nil {
		t.Fatalf("BuildZipWithExtras returned error: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip.NewReader error: %v", err)
	}

	found := map[string]bool{}
	for _, f := range zr.File {
		found[f.Name] = true
	}
	if !found["bin/app.exe"] {
		t.Error("zip missing bin/app.exe")
	}
	if !found["VERSION"] {
		t.Error("zip missing VERSION (virtual file)")
	}

	// Verify VERSION content
	for _, f := range zr.File {
		if f.Name == "VERSION" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open VERSION: %v", err)
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			if string(data) != versionContent {
				t.Errorf("VERSION content = %q, want %q", string(data), versionContent)
			}
		}
	}
}

func TestExecTargetPaths(t *testing.T) {
	cfg := &Config{
		Commands: []CommandConfig{
			{Exec: "bin/onepkg"},
			{Shell: "Write-Host hi"}, // no exec -> ignored
			{Exec: "bin/helper"},
		},
		Services: []ServiceConfig{{Exec: "bin/svc"}},
	}
	got := execTargetPaths(cfg)
	want := map[string]bool{"bin/onepkg": true, "bin/helper": true, "bin/svc": true}
	if len(got) != len(want) {
		t.Fatalf("len(targets) = %d, want %d; got %v", len(got), len(want), got)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("missing exec target %q; got %v", k, got)
		}
	}
}

func TestExecTargetPathsEmpty(t *testing.T) {
	if got := execTargetPaths(&Config{}); len(got) != 0 {
		t.Errorf("expected empty target set for empty config; got %v", got)
	}
}

func TestApplyExeExtension(t *testing.T) {
	targets := map[string]bool{"bin/onepkg": true, "bin/tool": true, "bin/myapp.exe": true}
	resolved := []ResolvedItem{
		{Original: "bin/onepkg.{platform}", Resolved: "bin/onepkg.windows-amd64", PackPath: "bin/onepkg"},       // exec target, extensionless -> .exe
		{Original: "bin/tool", Resolved: "bin/tool", PackPath: "bin/tool"},                                        // literal exec target, extensionless -> .exe
		{Original: "data/README", Resolved: "data/README", PackPath: "data/README"},                                // not an exec target -> unchanged
		{Original: "bin/myapp.{platform}.exe", Resolved: "bin/myapp.windows-amd64.exe", PackPath: "bin/myapp.exe"}, // exec target, already has extension -> unchanged (idempotent)
		{Original: "scripts/setup.ps1", Resolved: "scripts/setup.ps1", PackPath: "scripts/setup.ps1"},             // other extension -> unchanged
	}
	got := applyExeExtension(resolved, targets)
	want := []string{"bin/onepkg.exe", "bin/tool.exe", "data/README", "bin/myapp.exe", "scripts/setup.ps1"}
	for i, w := range want {
		if got[i].PackPath != w {
			t.Errorf("item %d PackPath = %q, want %q", i, got[i].PackPath, w)
		}
	}
}

func TestResolveWindowsItemPath(t *testing.T) {
	entries := []FileEntry{
		{RelPath: "bin/onepkg.exe"},
		{RelPath: "etc/config.conf"},
	}
	tests := []struct{ ref, want string }{
		{"bin/onepkg", "bin/onepkg.exe"},     // only the .exe form is packed -> resolve to it
		{"etc/config.conf", "etc/config.conf"}, // exact match
		{"bin/missing", "bin/missing"},        // neither form present -> unchanged (fallback)
		{"", ""},                              // empty ref -> empty
	}
	for _, tt := range tests {
		if got := resolveWindowsItemPath(entries, tt.ref); got != tt.want {
			t.Errorf("resolveWindowsItemPath(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}
