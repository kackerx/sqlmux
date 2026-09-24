package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Connection is one [[connection]] in connections.toml (§14).
type Connection struct {
	Name        string `toml:"name"`
	Engine      string `toml:"engine"`
	DSN         string `toml:"dsn"`
	PasswordCmd string `toml:"password_cmd"`
	PasswordEnv string `toml:"password_env"`
	Password    string `toml:"password"`
}

// LoadConnection finds connection name in connections.toml, or the first
// one when name is "". warning is set when the file holds a plain password
// that group or others can read (§13).
func LoadConnection(name string) (Connection, string, error) {
	var none Connection
	path := filepath.Join(Dir(), "connections.toml")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return none, "", fmt.Errorf("找不到 %s", path)
	}
	if err != nil {
		return none, "", err
	}
	var file struct {
		Connection []Connection `toml:"connection"`
	}
	if _, err := toml.Decode(string(data), &file); err != nil {
		return none, "", fmt.Errorf("%s: %w", path, err)
	}
	conns := file.Connection
	if len(conns) == 0 {
		return none, "", fmt.Errorf("%s: 里面没有 [[connection]]", path)
	}
	var names []string
	for i, c := range conns {
		if p := c.problem(); p != "" {
			return none, "", fmt.Errorf("%s: 第 %d 个连接：%s", path, i+1, p)
		}
		names = append(names, c.Name)
	}
	i := 0
	if name != "" {
		if i = slices.Index(names, name); i < 0 {
			return none, "", fmt.Errorf("%s: 没有名为 %q 的连接，已有：%s", path, name, strings.Join(names, ", "))
		}
	}
	warning := ""
	plain := slices.ContainsFunc(conns, func(c Connection) bool { return c.Password != "" })
	if info, err := os.Stat(path); err == nil && plain && info.Mode().Perm()&0o044 != 0 {
		warning = "connections.toml 里有明文密码，且其他用户可读，建议 chmod 600"
	}
	return conns[i], warning, nil
}

// problem is what makes c unusable, or "".
func (c Connection) problem() string {
	for _, f := range [][2]string{{"name", c.Name}, {"engine", c.Engine}, {"dsn", c.DSN}} {
		if f[1] == "" {
			return "缺少 " + f[0]
		}
	}
	if c.Engine != "postgres" {
		return fmt.Sprintf("engine = %q，目前只支持 postgres", c.Engine)
	}
	return ""
}

// Secret is the password from the first source set: password_cmd,
// password_env, password (§13). "" leaves it to the DSN and ~/.pgpass.
func (c Connection) Secret() (string, error) {
	switch {
	case c.PasswordCmd != "":
		out, err := exec.Command("sh", "-c", c.PasswordCmd).Output()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("password_cmd: %w: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		if err != nil {
			return "", fmt.Errorf("password_cmd: %w", err)
		}
		return strings.TrimSuffix(string(out), "\n"), nil
	case c.PasswordEnv != "":
		if s := os.Getenv(c.PasswordEnv); s != "" {
			return s, nil
		}
		return "", fmt.Errorf("password_env = %q：这个环境变量没有设置", c.PasswordEnv)
	}
	return c.Password, nil
}
