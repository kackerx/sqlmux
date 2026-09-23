// Package config reads config.toml from the XDG config dir (tech-design §14).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Icons      string // nerd | ascii
	Timeoutlen int    // ms to wait on an ambiguous key sequence

	// Leader and Bindings are the raw [keys] and [map.*] entries; the keymap
	// package gives them meaning.
	Leader   string
	Bindings []Binding
}

// Binding is one `key = "value"` line under [keys.<scope>] or [map.<...>].
type Binding struct {
	Table string // "keys.normal", "map.console.normal"
	Key   string // as written, e.g. "<C-p>"
	Value string // action for keys.*, key sequence for map.*; "" unbinds
}

func Default() *Config { return &Config{Icons: "nerd", Timeoutlen: 1000} }

// Dir is $XDG_CONFIG_HOME/sqlmux, falling back to ~/.config/sqlmux on every
// Unix, macOS included (§14).
func Dir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "sqlmux")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sqlmux")
}

// Load reads config.toml; a missing file means all defaults.
func Load() (*Config, error) {
	path := filepath.Join(Dir(), "config.toml")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	c, err := Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse reads config TOML over the defaults. Binding order follows the file,
// so "first binding" questions have a stable answer.
func Parse(data string) (*Config, error) {
	c := Default()
	raw := struct {
		Icons      *string        `toml:"icons"`
		Timeoutlen *int           `toml:"timeoutlen"`
		Keys       map[string]any `toml:"keys"`
		Map        map[string]any `toml:"map"`
	}{}
	md, err := toml.Decode(data, &raw)
	if err != nil {
		return nil, err
	}
	if raw.Icons != nil {
		if *raw.Icons != "nerd" && *raw.Icons != "ascii" {
			return nil, fmt.Errorf(`icons = %q：只能是 "nerd" 或 "ascii"`, *raw.Icons)
		}
		c.Icons = *raw.Icons
	}
	if raw.Timeoutlen != nil {
		if *raw.Timeoutlen <= 0 {
			return nil, fmt.Errorf("timeoutlen = %d：必须大于 0", *raw.Timeoutlen)
		}
		c.Timeoutlen = *raw.Timeoutlen
	}
	tables := map[string]map[string]any{"keys": raw.Keys, "map": raw.Map}
	for _, k := range md.Keys() {
		if tables[k[0]] == nil || md.Type(k...) == "Hash" {
			continue
		}
		v, ok := lookup(tables[k[0]], k[1:]).(string)
		if !ok {
			return nil, fmt.Errorf("%s：值必须是字符串", k)
		}
		if k.String() == "keys.leader" {
			c.Leader = v
			continue
		}
		if len(k) < 3 {
			return nil, fmt.Errorf("%s：键位要写在 [keys.normal]、[map.normal] 这样的表里", k)
		}
		c.Bindings = append(c.Bindings, Binding{strings.Join(k[:len(k)-1], "."), k[len(k)-1], v})
	}
	return c, nil
}

func lookup(m map[string]any, path []string) any {
	var v any = m
	for _, p := range path {
		v = v.(map[string]any)[p]
	}
	return v
}
