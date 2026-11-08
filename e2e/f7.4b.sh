#!/usr/bin/env bash
# F7.3 / F7.4 的审查补充（specs/m7-quality/task.md F7.3、F7.4；tech-design §7.8、§13「凭据」、§14「启动时找不到连接」）：非 ASCII 密码走 xxd -r -p、
# 明文回退改成 0600、密码和 password_cmd 都填报错、密码按字素画 *、port 规范化、单击分段、高级收起时 dsn 不生效、高级里的 dsn 让 host 等变灰且 Tab 跳过、
# 重名时钥匙串不碰、零连接的几种文件和命令、80×24 和 ascii 图标下的表单、password 里的 ${VAR}。钥匙串用 lib.sh 的假工具。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
ERROR=#f7768e
read -r PG_USER PG_PW PG_HOST PG_PORT PG_DB < <(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p.username, p.password, p.hostname, p.port, p.path[1:])' "$SQLMUX_TEST_PG")
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
frows() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 28)) $((g[0] + g[2] - 2)) "$y" | noicon | sed 's/^ *//; s/ *$//'; done; }   # 每行前面有字段的图标，noicon 去掉
form_open() { e2e_plain | grep -q "┌─ 连接"; }
field_y() { local g; g=($(box)); echo $(( g[1] + 1 + $1 )); }              # N：0 name 1 group 2 engine 3 host 4 port 5 user 6 password 7 database 8 read_only 9 高级（展开后 10 dsn 11 password_cmd）
# on_field N：焦点在第 N 个字段上：输入框有终端光标；engine（1）、高级（8）是开关，光标隐藏；read_only（7）的值是 select 底
ro_x() { e2e_find "[" "$(field_y 8)" | cut -d" " -f1; }                       # read_only 的 [ ] 开关的列（标签列的宽度会随字段名变）
on_field() { local g; g=($(box)); case $1 in
  2|9) [[ $(e2e_flag cursor_flag) == 0 ]] || { echo "  cursor visible on a switch ($1)"; false; } ;;
  8) style_has "$(ro_x)" "$(field_y 8)" bg=#364a82 ;;
  *) [[ $(( $(e2e_flag cursor_y) + 1 )) == $(field_y "$1") ]] || { echo "  cursor on row $(( $(e2e_flag cursor_y) + 1 )), field $1 is on $(field_y "$1")"; false; } ;;
  esac; }
fsep() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3]; y++)); do [[ $(e2e_text $((g[0] + 28)) $((g[0] + 30)) $y) == "───" ]] && { echo "$y"; return; }; done; }
ferr() { local g; g=($(box)); e2e_text $((g[0] + 28)) $((g[0] + g[2] - 2)) $(( $(fsep) - 1 )) | sed 's/^ *//; s/ *$//'; }   # 分隔线上面那一行（F8.16：在右半边，前面空一格）：校验的原因
ferr_is() { local g; g=($(box)); [[ $(ferr) == "$1" ]] && style_has $((g[0] + 29)) $(( $(fsep) - 1 )) fg=$ERROR || { echo "  error row: '$(ferr)', want '$1'"; false; }; }
clr() { local i; for ((i = 0; i < 40; i++)); do e2e_keys BSpace; done; sleep 0.2; }
# fill NAME HOST PORT USER PW DB：焦点在 name 上，一路 Tab 填下去（port 先清掉预填的）
fill() { e2e_type "$1"; key Tab; key Tab; key Tab; e2e_type "$2"; key Tab; clr; e2e_type "$3"; key Tab; e2e_type "$4"; key Tab; e2e_type "$5"; key Tab; e2e_type "$6"; sleep 0.3; }
CT() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }
nrow() { local y; for y in $(seq 3 30); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && { echo "$y"; return; }; done; }   # 侧栏里 NAME 的行
toast_row() { e2e_text 1 "$(e2e_flag pane_width)" $(( $(H) - 1 )); }

# ---- 没有连接时启动：界面出来、空树一行提示、表单自动打开
E2E_NOCONN=1 e2e_start "$E2E_BIN"; wait_for 6 flag_is alternate_on 1; sleep 1.5
frows_raw() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 28)) $((g[0] + g[2] - 2)) "$y" | sed 's/^ *//; s/ *$//'; done; }   # 图标不去掉（ascii 模式看替代字符）

newconn() { mkdir -p "$D/$1"; (umask 077; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$SQLMUX_TEST_PG" >"$D/$1/connections.toml"); }
toast_text() { toast_row | sed 's/^[^┘]*└*[─ ]*//; s/ *┘.*//; s/^ *//'; }

# ---- 非 ASCII 密码：password_cmd 末尾接 xxd -r -p，真 security 对这种密码输出十六进制；假工具照这个规则
newconn a; start -C "$D/a"; pal ">conn.new"; key Enter
fill uni "$PG_HOST" "$PG_PORT" "$PG_USER" "pässwörd" "$PG_DB"; key Enter; sleep 1.5
check "非 ASCII 密码（pässwörd）：保存成功，macOS 上 password_cmd 末尾接 xxd -r -p，connections.toml 里没有明文" eval '! form_open && { [[ $(uname) != Darwin ]] || grep -q "password_cmd = .*xxd -r -p" "$(CT)"; } && ! grep -q "pässwörd" "$(CT)"'
check "真 security 会输出十六进制：假工具的记录里是原密码；password_cmd 在假 PATH 下执行，还原出 pässwörd" eval 'cmd=$(sed -n "s/^password_cmd = \"\(.*\)\"$/\1/p" "$(CT)" | tail -1); cmd=${cmd//\\\"/\"}; [[ $(cat "$E2E_TMP/keychain/sqlmux-uni") == "pässwörd" && $(PATH="$E2E_TMP/bin:$PATH" E2E_DIR="$E2E_TMP" sh -c "$cmd") == "pässwörd" ]] || { echo "  cmd: $cmd"; false; }'

# ---- 密码按字素画 *：中文、emoji、e + 组合音标各一个
pal ">conn.new"; key Enter; e2e_type g; key Tab; key Tab; key Tab; key Tab; key Tab; key Tab; e2e_type $'中文e\xcc\x81\xf0\x9f\x98\x80'; sleep 0.3
check "密码「中文é😀」（é 是 e + 组合音标）：画 4 个 *，按字素" eval 'r=$(frows | sed -n 7p); [[ $r == *"****"* && $r != *"*****"* && $r != *中* ]] || { echo "  [$r]"; false; }'
key Escape; sleep 0.3

# ---- 密码和 password_cmd 都填了：报错；重名时钥匙串不碰
pal ">conn.new"; key Enter; fill both "$PG_HOST" "$PG_PORT" "$PG_USER" "pw" "$PG_DB"
for i in 1 2 3; do key Tab; done; key Space; key Tab; key Tab; e2e_type "echo x"; key Enter; sleep 0.4
check "password 和 password_cmd 都填了：报错，表单不关，没有存钥匙串" eval 'form_open && [[ -n $(ferr) && ! -e "$E2E_TMP/keychain/sqlmux-both" ]]'
key Escape; sleep 0.3
ls "$E2E_TMP/keychain" >"$D/ks1"
pal ">conn.new"; key Enter; fill doraemon "$PG_HOST" "$PG_PORT" "$PG_USER" "pw2" "$PG_DB"; key Enter; sleep 0.5
check "名字重复（doraemon）：先报错，钥匙串不碰（没有 sqlmux-doraemon）" eval 'form_open && ferr_is "name = \"doraemon\" 重复了" && [[ ! -e "$E2E_TMP/keychain/sqlmux-doraemon" ]]'
key Escape; sleep 0.3

# ---- port 规范化；engine 分段单击；port 为空时切换跟着换
pal ">conn.new"; key Enter; fill p5 "$PG_HOST" "05432" "$PG_USER" "" "$PG_DB"; key Enter; sleep 0.8
check "port 写成 05432：存下来的 dsn 里是 5432" eval '! form_open && grep -A3 "name = \"p5\"" "$(CT)" | grep -q "dsn = \"postgres://$PG_USER@$PG_HOST:5432/"'
pal ">conn.new"; key Enter; fill p6 "$PG_HOST" "+5433" "$PG_USER" "" "$PG_DB"; key Enter; sleep 0.8
check "port 写成 +5433：存下来的是 5433" eval '! form_open && grep -A3 "name = \"p6\"" "$(CT)" | grep -q ":5433/"'
pal ">conn.new"; key Enter; g=($(box)); mx=$(e2e_find mysql "$(field_y 2)" | cut -d' ' -f1)
e2e_click "$mx" "$(field_y 2)"; sleep 0.3
check "单击 mysql 段：选中 mysql，port 跟着变成 3306" eval '[[ $(frows | sed -n 5p) == "port 3306" ]]'
e2e_click "$mx" "$(field_y 2)"; sleep 0.3
check "再点已经选中的 mysql 段：什么都不做（port 还是 3306）" eval '[[ $(frows | sed -n 5p) == "port 3306" ]]'
px=$(e2e_find postgres "$(field_y 2)" | tr " " "\n" | awk '$1 > 34 { print; exit }'); e2e_click "$px" "$(field_y 2)"; sleep 0.3   # x right of the sidebar: the tree has a postgres database node on that row too
check "单击 postgres 段：切回，port 回到 5432" eval '[[ $(frows | sed -n 5p) == "port 5432" ]]'
key Tab; key Tab; key Tab; clr; key BTab; key Space; sleep 0.3
check "port 清空后切 engine：跟着换成 3306" eval '[[ $(frows | sed -n 5p) == "port 3306" ]] || { frows | sed -n 5p; true; }'
key Escape; sleep 0.3

# ---- 高级：展开填了 dsn、password_cmd 后收起：不生效；dsn 非空时 host / port / user / database 变灰、Tab 跳过
pal ">conn.new"; key Enter; fill hid "$PG_HOST" "$PG_PORT" "$PG_USER" "" "$PG_DB"
for i in 1 2 3; do key Tab; done; key Space; key Tab; e2e_type "postgres://other@elsewhere:1/zzz"; key BTab; key Space; sleep 0.2   # 三下 Tab 到「高级」（read_only、production、高级；F17.9 前是两下）
check "收起「高级」：dsn 那一行不见了" eval '[[ $(frows | sed -n 11p) == "高级 ▸" ]]'
key Enter; sleep 0.8
check "收起时里面填的 dsn 不生效：存下来的 dsn 是 host / port / user / database 拼的，不是 elsewhere" eval '! form_open && grep -A3 "name = \"hid\"" "$(CT)" | grep -q "dsn = \"postgres://$PG_USER@$PG_HOST:$PG_PORT/$PG_DB" && ! grep -q elsewhere "$(CT)"'
pal ">conn.new"; key Enter; e2e_type grey
for i in 1 2 3 4 5 6 7 8 9 10; do key Tab; done; key Space; key Tab; e2e_type "postgres://u@h:1/d"; sleep 0.3
key BTab; key BTab; key BTab; key BTab; sleep 0.2
check "高级里 dsn 非空：S-Tab 从 dsn 往回，经过 高级、production、read_only，跳过变灰的 database / user / port / host，落在 password（password 不变灰）" on_field 6
key Escape; sleep 0.3

# ---- 没有钥匙串工具时明文回退：F7.5 起不 chmod 已有的文件（权限不变），新建的文件仍是 0600
newconn n644; chmod 644 "$D/n644/connections.toml"
start -P /bin -C "$D/n644"; pal ">conn.new"; key Enter; fill plain2 "$PG_HOST" "$PG_PORT" "$PG_USER" "$PG_PW" "$PG_DB"; key Enter; sleep 1.5
check "没有钥匙串工具：写明文，原来 0644 的文件权限不变（程序不 chmod）" eval '! form_open && [[ $(fmode "$(CT)") == 644 ]] && grep -q "password = \"$PG_PW\"" "$(CT)"'
e2e_stop

# ---- 零连接：没有文件、空文件、只有注释、connection = []：都进界面、空树一行提示、表单自动打开；esc 之后能用
for variant in none empty comment emptyarray; do
  mkdir -p "$D/z-$variant"; case $variant in none) ;; empty) : >"$D/z-$variant/connections.toml" ;; comment) printf '# nothing yet\n' >"$D/z-$variant/connections.toml" ;; emptyarray) printf 'connection = []\n' >"$D/z-$variant/connections.toml" ;; esac
  chmod 600 "$D/z-$variant/connections.toml" 2>/dev/null
  E2E_NOCONN=1 e2e_start -C "$D/z-$variant" "$E2E_BIN"; wait_for 6 flag_is alternate_on 1; sleep 1.2
  check "零连接（$variant）：进界面，表单自动打开，树里有「还没有连接」" eval 'running && form_open && e2e_plain | sed -n 1,6p | grep -q "还没有连接"'
  key Escape; e2e_keys C-p; sleep 0.4
  check "零连接（$variant）：esc 关掉表单后界面能用（面板打得开）" eval 'palette_open'
  key Escape
  e2e_stop
done

# ---- 零连接：sqlmux <名字> 和 --layout 照旧报错退出；last.toml 被忽略；需要连接的命令 toast
mkdir -p "$D/z-cli"
e2e_start -C "$D/z-cli" "E2E_NOCONN=1 $E2E_BIN nosuch"; sleep 1.2
check "零连接 sqlmux nosuch：照旧报错退出（找不到这个名字）" eval 'screen_has "[e2e-exit 1]"'
e2e_stop
mkdir -p "$D/z-lay/layouts"; printf 'version = 1\n' >"$D/z-lay/layouts/x.toml"
E2E_NOCONN=1 e2e_start -C "$D/z-lay" "$E2E_BIN --layout x"; sleep 1.2
check "零连接 --layout x：照旧报错退出" eval 'screen_has "[e2e-exit 1]"'
e2e_stop
mkdir -p "$D/z-last/state/sqlmux"; printf 'version = 1\n[[session]]\n  name = "ghost"\n  connection = "gone"\n' >"$D/z-last/state/sqlmux/last.toml"
E2E_NOCONN=1 e2e_start -S "$D/z-last/state" "$E2E_BIN"; wait_for 6 flag_is alternate_on 1; sleep 1.2
check "零连接：last.toml 被忽略（不弹它的错、不建 ghost session），表单照常打开" eval 'running && form_open && ! screen_has "ghost" && [[ -f "$D/z-last/state/sqlmux/last.toml" ]]'
key Escape; e2e_keys C-p; sleep 0.3; e2e_type ">新建 query"; sleep 0.5; key Enter; sleep 0.6
check "零连接下需要连接的命令（新建 query）：toast「还没有连接，先新建一个」，不崩" eval 'running && toast_row | grep -q "还没有连接，先新建一个"'
e2e_keys C-p; sleep 0.3; e2e_type ">打开布局"; sleep 0.5; key Enter; sleep 0.6
check "layout.open 也一样" eval 'running && toast_row | grep -q "还没有连接，先新建一个"'
e2e_stop

# ---- 80×24 下的表单；ascii 图标
E2E_NOCONN=1 e2e_start -x 80 -y 24 "$E2E_BIN"; sleep 1.5
g=($(box))
check "80×24：表单整个在屏幕里（上下左右都不出界），按钮行和键位提示都看得见" eval '[[ -n ${g[0]} ]] && (( g[0] >= 0 && g[1] >= 0 && g[0] + g[2] <= 80 && g[1] + g[3] <= 24 )) && frows | tail -1 | grep -q "保存" && frows | tail -1 | grep -q "esc"'
e2e_stop
mkdir -p "$D/ascii"; printf 'icons = "ascii"\n' >"$D/ascii/config.toml"
E2E_NOCONN=1 e2e_start -C "$D/ascii" "$E2E_BIN"; sleep 1.5
check "ascii 图标：每行字段名前面是替代字符（name # / group % / host @ / port : / user ~ / database = / 高级 +），没有 Nerd Font 字形" eval 'r=$(frows_raw); [[ $(grep -c "^# *name" <<<"$r") == 1 && $(grep -c "^@ *host" <<<"$r") == 1 && $(grep -c "^: *port" <<<"$r") == 1 && $(grep -c "^~ *user" <<<"$r") == 1 && $(grep -c "^= *database" <<<"$r") == 1 && $(grep -c "^+ *高级" <<<"$r") == 1 && $(grep -c "^% *group" <<<"$r") == 1 ]] || { echo "$r"; false; }'
e2e_stop

e2e_done
