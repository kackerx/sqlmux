package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// Connection is one [[connection]] in connections.toml (§14).
type Connection struct {
	Name        string `toml:"name"`
	Group       string `toml:"group,omitempty"` // the tree and the manager divide the connections by it (F10.6)
	Engine      string `toml:"engine"`
	DSN         string `toml:"dsn"`
	PasswordCmd string `toml:"password_cmd,omitempty"`
	PasswordEnv string `toml:"password_env,omitempty"`
	Password    string `toml:"password,omitempty"`
	ReadOnly    bool   `toml:"read_only,omitempty"`  // §13, from F5.3; a production connection that does not say it is read only (decodeConnections)
	Production  bool   `toml:"production,omitempty"` // every write on it is asked about, and it is marked so (§13, F17.9)
	SSH         string `toml:"ssh,omitempty"`        // "user@host": the server is reached through that machine's ssh (§8.4, F15.2)
	Delimiter   string `toml:"delimiter,omitempty"`  // redis only: what the tree splits a key's name by, over [redis] delimiter (§7.8, F16.2)
	// Dialect and Command are an exec connection's (§8.8, F17.7): what the command
	// runs, postgres, and the command, {sql} where the statement goes in an argument.
	Dialect string   `toml:"dialect,omitempty"`
	Command []string `toml:"command,omitempty"`
	// Databases and Schemas are what the tree lists and loads of the server when they
	// are not all of it (F17.13): the databases (PG), and the schemas, `db.schema`
	// for PG and a name for MySQL; none: all. The DSN's database is always there.
	Databases []string `toml:"databases,omitempty"`
	Schemas   []string `toml:"schemas,omitempty"`
	// Temp is a connection of the command line, a file or a URL: written nowhere,
	// not in the manager (F14.1).
	Temp bool `toml:"-"`
	// Database is the database a PG connection is dialed to instead of the one
	// the DSN names: the child of a connection in the tree (F15.1), written
	// nowhere.
	Database string `toml:"-"`
}

// Kind is the engine whose catalog and commands c speaks: its Engine, or an exec
// connection's dialect (§8.8).
func (c Connection) Kind() string {
	if c.Engine == "exec" {
		return c.Dialect
	}
	return c.Engine
}

// UnknownKeys are the lines that say which keys of config.toml, and of each
// connection of connections.toml, nothing reads: `readonly` for `read_only`
// reads a connection that is writable, with no word. The files are the ones
// the program read; what does not read is its error's to say (F13.5).
func UnknownKeys(c *Config) (lines []string) {
	if len(c.Unknown) > 0 {
		lines = append(lines, unknownKeys("config.toml", c.Unknown))
	}
	if note := UnknownThemeKeys(c.ThemeName, c.ThemeUnknown); note != "" {
		lines = append(lines, note)
	}
	data, err := os.ReadFile(connectionsPath())
	if err != nil {
		return lines
	}
	var file struct {
		Connection []map[string]any `toml:"connection"`
	}
	md, err := toml.Decode(string(data), &file)
	if err != nil {
		return lines
	}
	var outside []string // what is not in a [[connection]]: `default = "prod"`, a block of another name
	for _, k := range md.Undecoded() {
		if top := k[0]; !slices.Contains(outside, top) {
			outside = append(outside, top)
		}
	}
	if slices.Sort(outside); len(outside) > 0 {
		lines = append(lines, unknownKeys("connections.toml", outside))
	}
	known, t := map[string]bool{}, reflect.TypeFor[Connection]()
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("toml"), ",")
		known[strings.ToLower(name)] = true // the decoder matches a key whatever its case
	}
	for _, m := range file.Connection {
		var unknown []string
		name := ""
		for k, v := range m {
			if strings.EqualFold(k, "name") {
				name, _ = v.(string)
			}
			if !known[strings.ToLower(k)] {
				unknown = append(unknown, k)
			}
		}
		if slices.Sort(unknown); len(unknown) > 0 {
			lines = append(lines, i18n.T("connections.toml: 不认识的键 %s（connection %q）", strings.Join(unknown, ", "), name))
		}
	}
	return lines
}

// connectionsPath is connections.toml (§14).
func connectionsPath() string { return filepath.Join(Dir(), "connections.toml") }

// noConnections is connections.toml having none: no file, or none in it.
// The program starts all the same, with the form open, unless a connection
// was asked for (§14「启动时找不到连接」).
type noConnections string

func (e noConnections) Error() string { return string(e) }

// NoConnections reports whether err is that.
func NoConnections(err error) bool {
	var e noConnections
	return errors.As(err, &e)
}

// LoadConnections reads every connection in connections.toml, and which
// of them is connection name, or the first one when name is "" (§5).
func LoadConnections(name string) (conns []Connection, start int, err error) {
	path := connectionsPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, noConnections(fmt.Sprintf(i18n.T("找不到 %s"), path))
	}
	if err != nil {
		return nil, 0, err
	}
	if conns, err = parseConnections(path, string(data)); err != nil {
		return nil, 0, err
	}
	var names []string
	for _, c := range conns {
		names = append(names, c.Name)
	}
	if name != "" {
		if start = slices.Index(names, name); start < 0 {
			return nil, 0, fmt.Errorf(i18n.T("%s: 没有名为 %q 的连接，已有：%s"), path, name, strings.Join(names, ", "))
		}
	}
	return conns, start, nil
}

// parseConnections is the connections text, the file at path, holds, each
// checked.
func parseConnections(path, text string) ([]Connection, error) {
	conns, err := decodeConnections(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	file := struct{ Connection []Connection }{conns}
	if len(file.Connection) == 0 {
		return nil, noConnections(fmt.Sprintf(i18n.T("%s: 里面没有 [[connection]]"), path))
	}
	var names []string
	for i, c := range file.Connection {
		if _, p := c.Check(names); p != "" {
			return nil, fmt.Errorf(i18n.T("%s: 第 %d 个连接：%s"), path, i+1, p)
		}
		names = append(names, c.Name)
	}
	return file.Connection, nil
}

// decodeConnections reads the [[connection]]s of text. A production one that does
// not write read_only is read only: read_only = false has to be written (§13).
func decodeConnections(text string) ([]Connection, error) {
	var file struct {
		Connection []Connection `toml:"connection"`
	}
	if _, err := toml.Decode(text, &file); err != nil {
		return nil, err
	}
	var raw struct {
		Connection []struct {
			ReadOnly *bool `toml:"read_only"`
		} `toml:"connection"`
	}
	if _, err := toml.Decode(text, &raw); err == nil && len(raw.Connection) == len(file.Connection) {
		for i, c := range file.Connection {
			if c.Production && raw.Connection[i].ReadOnly == nil {
				file.Connection[i].ReadOnly = true
			}
		}
	}
	for i, c := range file.Connection {
		if c.Engine == "exec" { // read only is all a command is (§8.8)
			file.Connection[i].ReadOnly = true
		}
	}
	return file.Connection, nil
}

// Check is what makes c unusable beside the connections names, and the
// field at fault: "", "" when nothing does (§14).
func (c Connection) Check(names []string) (field, problem string) {
	for _, f := range [][2]string{{"name", c.Name}, {"engine", c.Engine}, {"dsn", c.DSN}} {
		if f[1] == "" && !(f[0] == "dsn" && c.Engine == "exec") { // a command is no address
			return f[0], i18n.T("缺少 %s", f[0])
		}
	}
	// the name keys state.json's tables and tree node IDs (§14)
	if checkFileName(c.Name) != nil {
		return "name", fmt.Sprintf(i18n.T(`name = %q：不能含 / 或 \，也不能以 . 开头`), c.Name)
	}
	if slices.Contains(names, c.Name) { // the tree's roots go by it too (§5)
		return "name", fmt.Sprintf(i18n.T("name = %q 重复了"), c.Name)
	}
	if c.Group != "" && checkFileName(c.Group) != nil { // the characters of a name (F10.6)
		return "group", fmt.Sprintf(i18n.T(`group = %q：不能含 / 或 \，也不能以 . 开头`), c.Group)
	}
	if !slices.Contains([]string{"postgres", "mysql", "sqlite", "redis", "exec"}, c.Engine) {
		return "engine", fmt.Sprintf(i18n.T("engine = %q：只能是 postgres、mysql、sqlite、redis 或 exec"), c.Engine)
	}
	if c.Engine == "exec" {
		place := map[string]string{"postgres": "{sql}", "redis": "{cmd}"}[c.Dialect]
		switch {
		case place == "":
			return "dialect", fmt.Sprintf(i18n.T("dialect = %q：exec 只认 postgres 和 redis"), c.Dialect)
		case !slices.ContainsFunc(c.Command, func(a string) bool { return strings.Contains(a, place) }):
			return "command", fmt.Sprintf(i18n.T("exec 的 command 是 argv 数组，里面要有 %s"), place)
		case c.Dialect == "redis" && !slices.Contains(c.Command, "{args...}"): // as an item of its own: the words go in as many items; else they are dropped
			return "command", i18n.T("redis 的 command 要有单独一项 {args...}")
		}
	} else if c.Dialect != "" || len(c.Command) > 0 {
		return "command", i18n.T("dialect 和 command 只对 exec 连接有用")
	}
	if (len(c.Databases) > 0 || len(c.Schemas) > 0) && (c.Kind() == "redis" || c.Kind() == "sqlite") {
		return "schemas", i18n.T("databases 和 schemas 只对 postgres 和 mysql 连接有用")
	}
	if len(c.Databases) > 0 && c.Kind() != "postgres" {
		return "databases", i18n.T("databases 只对 postgres 连接有用")
	}
	if c.Delimiter != "" && c.Engine != "redis" {
		return "delimiter", i18n.T("delimiter 只对 redis 连接有用")
	}
	if strings.HasPrefix(c.SSH, "-") || strings.ContainsAny(c.SSH, " \t") { // ssh would take it for an option, or for two words
		return "ssh", fmt.Sprintf(i18n.T("ssh = %q：写成 user@host，不能以 - 开头，也不能含空格"), c.SSH)
	}
	return "", ""
}

// appendText is the text AppendConnection writes for c, a blank line
// before it, a newline first if the file lacks one. What the file would be
// is parsed first: one it would break (its connections an inline array, a
// name taken since it was read) is the error.
func appendText(c Connection) (string, error) {
	var b strings.Builder
	enc := toml.NewEncoder(&b)
	enc.Indent = ""
	if err := enc.Encode(struct {
		Connection []Connection `toml:"connection"`
	}{[]Connection{c}}); err != nil {
		return "", err
	}
	old, err := os.ReadFile(connectionsPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	text := b.String()
	if c.Production && !c.ReadOnly { // the default is read only: writable is said
		text += "read_only = false\n"
	}
	if len(old) > 0 {
		if old[len(old)-1] != '\n' {
			text = "\n" + text
		}
		text = "\n" + text
	}
	if _, err := parseConnections(connectionsPath(), string(old)+text); err != nil {
		return "", err
	}
	return text, nil
}

// CheckAppend is the error AppendConnection would give for c, nothing
// written, a file that cannot be appended to included: asked before a
// password goes into the keychain for it (F7.3, F8.16).
func CheckAppend(c Connection) error {
	if f, err := os.OpenFile(connectionsPath(), os.O_WRONLY|os.O_APPEND, 0); err == nil {
		_ = f.Close()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	} else if err := probeDir(Dir()); err != nil { // none yet: it is made there
		return err
	}
	_, err := appendText(c)
	return err
}

// AppendConnection adds c at the end of connections.toml, made 0600 if
// there is none; the permissions of one there is are the user's (§13, §14):
// its text written after what is there, never the file encoded again, so the
// user's comments stay.
// ponytail: called in Update, the file read and written as the UI waits; a
// Cmd if a slow disk ever shows it
func AppendConnection(c Connection) error {
	text, err := appendText(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(connectionsPath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(text)
	return err
}

// envRef is a ${NAME} in dsn or password.
var envRef = regexp.MustCompile(`\$\{([^}]+)\}`)

// Expanded is c with every ${NAME} in dsn and password replaced by the
// environment variable's value (§13), password only when it is the source
// of the password. A variable not set, or empty, is an error naming it, the
// rest of c as it is.
// ponytail: only ${NAME}; $NAME is left alone (a password or DSN may hold
// a $), and there is no ${NAME:-default}
func (c Connection) Expanded() (Connection, error) {
	var err error
	expand := func(s string) string {
		return envRef.ReplaceAllStringFunc(s, func(ref string) string {
			v := os.Getenv(ref[2 : len(ref)-1])
			if v == "" && err == nil {
				err = fmt.Errorf(i18n.T("%s：这个环境变量没有设置"), ref)
			}
			return v
		})
	}
	c.DSN = expand(c.DSN)
	if c.PasswordCmd == "" && c.PasswordEnv == "" { // else password is not the source: Secret() never reads it
		c.Password = expand(c.Password)
	}
	return c, err
}

// Secret is the password from the first source set: password_cmd,
// password_env, password (§13). "" leaves it to the DSN and ~/.pgpass.
func (c Connection) Secret() (string, error) {
	switch {
	case c.PasswordCmd != "":
		out, err := ShellCommand(context.Background(), c.PasswordCmd).Output()
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
		return "", fmt.Errorf(i18n.T("password_env = %q：这个环境变量没有设置"), c.PasswordEnv)
	}
	return c.Password, nil
}
