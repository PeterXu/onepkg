package main

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Name      string          `toml:"name"`
	Version   string          `toml:"version"`
	Platforms []string        `toml:"platforms"`
	OutputDir string          `toml:"output_dir"`
	Items     []string        `toml:"items"`
	Services  []ServiceConfig `toml:"services"`
}

type ServiceConfig struct {
	Name    string       `toml:"name"`
	Exec    string       `toml:"exec"`
	Restart string       `toml:"restart"`
	User    string       `toml:"user"`
	Linux   LinuxService `toml:"linux"`
	MacOS   MacOSService `toml:"macos"`
}

type LinuxService struct {
	Type       string   `toml:"type"`
	After      []string `toml:"after"`
	RestartSec int      `toml:"restart_sec"`
}

type MacOSService struct {
	RunAtLoad bool `toml:"run_at_load"`
	KeepAlive bool `toml:"keep_alive"`
}

func ParseConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}
