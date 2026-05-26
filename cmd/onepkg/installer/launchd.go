package installer

import (
	"fmt"
	"strings"
)

type LaunchdConfig struct {
	Name      string
	Exec      string
	User      string
	RunAtLoad bool
	KeepAlive bool
}

func GenerateLaunchdPlist(cfg LaunchdConfig) string {
	var b strings.Builder

	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString("\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`)
	b.WriteString("\n")
	b.WriteString(`<plist version="1.0">`)
	b.WriteString("\n<dict>\n")

	fmt.Fprintf(&b, "\t<key>Label</key>\n")
	fmt.Fprintf(&b, "\t<string>com.%s</string>\n", cfg.Name)

	fmt.Fprintf(&b, "\t<key>Program</key>\n")
	fmt.Fprintf(&b, "\t<string>%s</string>\n", cfg.Exec)

	if cfg.RunAtLoad {
		fmt.Fprintf(&b, "\t<key>RunAtLoad</key>\n")
		fmt.Fprintf(&b, "\t<true/>\n")
	}

	if cfg.KeepAlive {
		fmt.Fprintf(&b, "\t<key>KeepAlive</key>\n")
		fmt.Fprintf(&b, "\t<true/>\n")
	}

	fmt.Fprintf(&b, "\t<key>UserName</key>\n")
	fmt.Fprintf(&b, "\t<string>%s</string>\n", cfg.User)

	b.WriteString("</dict>\n")
	b.WriteString("</plist>\n")

	return b.String()
}
