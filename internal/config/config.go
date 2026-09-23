// Package config reads config.toml from the XDG config dir (tech-design §14).
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Icons string `toml:"icons"` // nerd | ascii
}

func Default() *Config { return &Config{Icons: "nerd"} }

// Dir is $XDG_CONFIG_HOME/sqlmux, falling back to ~/.config/sqlmux.
func Dir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "sqlmux")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sqlmux")
}

// Load reads config.toml over the defaults; a missing file is not an error.
func Load() (*Config, error) {
	c := Default()
	_, err := toml.DecodeFile(filepath.Join(Dir(), "config.toml"), c)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	return c, err
}
