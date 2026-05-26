package installer

import (
	"fmt"
	"strings"
)

type SystemdConfig struct {
	Name        string
	Exec        string
	Args        []string
	Restart     string
	User        string
	Type        string
	After       []string
	RestartSec  int
	WorkingDir  string
	Environment map[string]string
	LimitNOFILE int
}

// systemdEscapeValue escapes characters that have special meaning in systemd Environment values.
// Double quotes and backslashes must be escaped within quoted values.
func systemdEscapeValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
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
	// ExecStart includes binary and arguments
	execStart := cfg.Exec
	if len(cfg.Args) > 0 {
		execStart = execStart + " " + strings.Join(cfg.Args, " ")
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", execStart)
	fmt.Fprintf(&b, "Restart=%s\n", cfg.Restart)
	if cfg.RestartSec > 0 {
		fmt.Fprintf(&b, "RestartSec=%d\n", cfg.RestartSec)
	}
	fmt.Fprintf(&b, "User=%s\n", cfg.User)
	if cfg.WorkingDir != "" {
		fmt.Fprintf(&b, "WorkingDirectory=%s\n", cfg.WorkingDir)
	}
	for key, value := range cfg.Environment {
		fmt.Fprintf(&b, `Environment="%s=%s"`+"\n", systemdEscapeValue(key), systemdEscapeValue(value))
	}
	if cfg.LimitNOFILE > 0 {
		fmt.Fprintf(&b, "LimitNOFILE=%d\n", cfg.LimitNOFILE)
	}
	b.WriteString("\n")

	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return b.String()
}
