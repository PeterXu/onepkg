package installer

import (
	"fmt"
	"strings"
)

type SystemdConfig struct {
	Name       string
	Exec       string
	Restart    string
	User       string
	Type       string
	After      []string
	RestartSec int
}

func GenerateSystemdUnit(cfg SystemdConfig) string {
	var b strings.Builder

	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", cfg.Name)
	if len(cfg.After) > 0 {
		fmt.Fprintf(&b, "After=%s\n", strings.Join(cfg.After, " "))
	}
	b.WriteString("\n")

	b.WriteString("[Service]\n")
	if cfg.Type != "" {
		fmt.Fprintf(&b, "Type=%s\n", cfg.Type)
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", cfg.Exec)
	fmt.Fprintf(&b, "Restart=%s\n", cfg.Restart)
	if cfg.RestartSec > 0 {
		fmt.Fprintf(&b, "RestartSec=%d\n", cfg.RestartSec)
	}
	fmt.Fprintf(&b, "User=%s\n", cfg.User)
	b.WriteString("\n")

	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return b.String()
}
