package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type VersionInfo struct {
	Name     string
	Version  string
	Platform string
	Created  time.Time
	Files    int
	SHA      map[string]string
}

func GenerateVersion(info VersionInfo) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "name = %q\n", info.Name)
	fmt.Fprintf(&b, "version = %q\n", info.Version)
	fmt.Fprintf(&b, "platform = %q\n", info.Platform)
	fmt.Fprintf(&b, "created = %q\n", info.Created.Format(time.RFC3339))
	fmt.Fprintf(&b, "files = %d\n", info.Files)
	b.WriteString("\n[sha]\n")

	paths := make([]string, 0, len(info.SHA))
	for p := range info.SHA {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		fmt.Fprintf(&b, "%q = %q\n", p, info.SHA[p])
	}

	return b.String(), nil
}
