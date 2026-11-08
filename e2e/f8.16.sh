#!/usr/bin/env bash
# F8.16 连接管理：修改和删除连接（specs/m8-feedback/task.md F8.16；tech-design §7.8「连接管理」、§13「凭据」、§14）
# connections.toml 的局部改写（只动改过的行，注释和别的块一个字节不动）、钥匙串的搬 / 删、state.json 的改名有单测；这里在真实终端和 PG 上测：
# 浮层的列表和表单、改 read_only 保存后文件只变一行、删除（确认、树里消失、正在用的先问再关它的 tab，F18.12）、新建追加、改名连带 state.json 和钥匙串、
# 文件只读时在存钥匙串之前就拒绝、esc 按进入方式、列表上 C-s 是保存表单（不是全局保存）。钥匙串一律用 lib.sh 的假 security / secret-tool。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'chmod -R u+w "$D" 2>/dev/null; e2e_stop; rm -rf "$D"' EXIT
SRC=$D/c/connections.toml; ST=$D/state
ct() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }   # 程序读写的是 start -C 拷出来的那份
write_conns() { mkdir -p "$D/c"; chmod u+w "$SRC" 2>/dev/null; (umask 077; cat >|"$SRC" <<EOT
# my conns
[[connection]]
name = "doraemon"
engine = "postgres"
dsn = "$SQLMUX_TEST_PG"

# seed one
[[connection]]
name = "seed"
engine = "postgres"
dsn = "$SQLMUX_TEST_PG"
read_only = true

# ---- legacy ----
# [[connection]]
# name = "old"
EOT
); cp "$SRC" "$D/orig.toml"; }
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
mgr_open() { e2e_plain | grep -q "┌─ 连接"; }
lrows() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + 26)) "$y" | noicon | sed 's/^ *//; s/ *$//'; done; }   # 左边的列表
frows() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 28)) $((g[0] + g[2] - 2)) "$y" | noicon | sed 's/^ *//; s/ *$//'; done; }   # 右边的表单
lsel() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do style_has $((g[0] + 20)) "$y" bg=#364a82 >/dev/null && { e2e_text $((g[0] + 2)) $((g[0] + 26)) "$y" | noicon | sed 's/^ *//; s/ *$//'; return; }; done; }   # 选中的那一项
fval() { frows | awk -v k="$1" '$1 == k { if (NF == 1) print ""; else { sub(/^[^ ]+ /, ""); print }; exit }'; }       # 表单里字段 K 的值
cmdp() { pal ">$1"; key Enter; sleep 0.7; }
toast_text() { e2e_text 1 "$(e2e_flag pane_width)" $(( $(H) - 1 )); }
nrow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && { echo "$y"; return; }; done; }   # 侧栏里 NAME 的行
# 文件里除了 K 开头的那一行，别处和原文一样？
only_changed() { diff "$D/orig.toml" "$(ct)" | grep -c '^[<>]'; }

write_conns
SOLO= start -C "$D/c" -S "$ST"; sleep 0.5
cmdp conn.manage
check "conn.manage：居中浮层「连接」，左边列表是 connections.toml 的顺序：doraemon、seed、新建" eval 'mgr_open && [[ $(lrows | head -3 | tr "\n" ,) == "doraemon,seed,新建," ]]'
check "选中第一项 doraemon，右边表单填着它的值（name doraemon，dsn 写法的连接：host 等从 dsn 拆出来，高级展开显示 dsn）" eval '[[ $(lsel) == doraemon && $(fval name) == doraemon && $(fval host) == localhost && $(fval port) == 55432 ]] && frows | grep -q "^dsn "'
key j; sleep 0.3
check "j：选中 seed，表单换成 seed 的值，read_only 勾着 [x]" eval '[[ $(lsel) == seed && $(fval name) == seed && $(fval read_only) == "[x]" ]] || { echo "  sel $(lsel) / $(frows | head -9 | tr "\n" ";")"; false; }'
key k; sleep 0.3
check "k：回到 doraemon" eval '[[ $(lsel) == doraemon ]]'
key Down; key Down; sleep 0.3
check "↓ ↓：选中最后一项「新建」，表单是空白的" eval '[[ $(lsel) == 新建 && -z $(fval name) ]] || { echo "  sel [$(lsel)] name [$(fval name)]"; frows | head -4; false; }'
key Up; sleep 0.2

# ---- esc：从列表进表单，esc 回列表，再 esc 关闭
key Tab; sleep 0.3
check "Tab：进表单（光标在 name 上）" eval '[[ $(e2e_flag cursor_flag) == 1 ]] && [[ $(( $(e2e_flag cursor_y) + 1 )) == $(( $(box | cut -d" " -f2) + 1 )) ]]'
key Escape; sleep 0.3
check "表单里 esc：回到列表（浮层还在）" eval 'mgr_open && [[ $(e2e_flag cursor_flag) == 0 ]]'
key Escape; sleep 0.3
check "再 esc：关闭浮层" eval '! mgr_open'

# ---- 改 seed 的 read_only：保存后文件只变一行
cmdp conn.manage; key j; key Tab; key Tab; key Tab; key Tab; key Tab; sleep 0.3
check "（准备）Tab 到 read_only（seed 的 dsn 非空，host / port / user / database 灰着，跳过）" eval '[[ $(fval read_only) == "[x]" ]]'
e2e_keys Space; sleep 0.3
check "空格翻开关：read_only 变成 [ ]" eval '[[ $(fval read_only) == "[ ]" ]]'
key C-s; sleep 1
check "C-s 保存：浮层还开着，connections.toml 里只有 read_only 这一行变了（删掉或改成 false），注释、空行、别的块一个字节不动" eval 'n=$(only_changed); [[ $n == 1 || $n == 2 ]] && ! grep -q "^read_only = true" "$(ct)" && grep -q "^# my conns" "$(ct)" && grep -q "^# seed one" "$(ct)" && grep -q "^# ---- legacy ----" "$(ct)" && grep -q "^# name = \"old\"" "$(ct)" || { diff "$D/orig.toml" "$(ct)"; false; }'
check "权限没变（仍是 0600）" eval '[[ $(fmode "$(ct)") == 600 ]]'
# 已连上的连接改了：toast 下次连接时生效
key Escape; key Escape; sleep 0.3
cmdp conn.manage; key Tab; key Tab; key Tab; key Tab; key Tab; e2e_keys Space; key C-s; sleep 1
check "改的是已连上的 doraemon（read_only 勾上）：toast「下次连接时生效」，连接没重连、表还能编辑" eval 'toast_text | grep -q "下次连接时生效"'
key Escape; key Escape; sleep 0.3
write_conns   # 还原，下面从原文开始

# ---- 删除：没在用的 seed
SOLO= start -C "$D/c" -S "$ST"; sleep 0.5
check "（准备）树里有 doraemon 和 seed" eval '[[ -n $(nrow doraemon) && -n $(nrow seed) ]]'
cmdp conn.manage; key j; key x; sleep 0.5
check "x：确认框「删除连接 seed？」" eval 'e2e_plain | grep -q "删除连接 seed？"'
key n; sleep 0.3
check "n：取消，seed 还在（文件没变）" eval 'cmp -s "$(ct)" "$D/orig.toml" && [[ $(lsel) == seed ]]'
key x; sleep 0.4; key y; sleep 1
check "y：seed 删掉了——文件里它的整块和它上面紧挨着的注释「# seed one」都没了，doraemon 块和后面的 legacy 注释原样" eval '! grep -q "seed" "$(ct)" && grep -q "^# my conns" "$(ct)" && grep -q "^name = \"doraemon\"" "$(ct)" && grep -q "^# ---- legacy ----" "$(ct)" && grep -q "^# name = \"old\"" "$(ct)" || { cat "$(ct)"; false; }'
check "列表里没有 seed 了" eval '[[ $(lrows | head -2 | tr "\n" ,) == "doraemon,新建," ]]'
key Escape; sleep 0.4
check "关掉浮层之后树里 seed 消失" eval '[[ -z $(nrow seed) ]]'
write_conns

# ---- 正在用的：doraemon（启动连接，有 tab）不再拒绝（F18.12）：确认框写它开着几个 tab，n 取消什么都不动（y 之后的全过程在 f18.12.sh）
SOLO= start -C "$D/c" -S "$ST"; open_table t_user; wait_for 8 settled
cmdp conn.manage; key x; sleep 0.5
check "删正在用的 doraemon（t_user 和 query_1 两个 tab）：确认框「删除连接 doraemon？它开着的 2 个 tab 会一起关掉」，没有「先关掉用它的 tab」的 toast" eval 'e2e_plain | grep -q "删除连接 doraemon？它开着的 2 个 tab 会一起关掉" && ! toast_text | grep -q "先关掉用它的 tab"'
key n; sleep 0.4
check "n：取消，文件没变、浮层还在" eval 'cmp -s "$(ct)" "$D/orig.toml" && mgr_open && ! e2e_plain | grep -q "删除连接"'
key Escape; sleep 0.3

# ---- 新建：从列表走到「新建」进表单，esc 回列表；conn.new 直接进的，esc 直接关
cmdp conn.manage; key Down; key Down; key Tab; sleep 0.3
key Escape; sleep 0.3
check "从列表走到「新建」进表单：esc 回到列表（浮层还在）" eval 'mgr_open && [[ $(lsel) == 新建 ]]'
key Escape; sleep 0.3
cmdp conn.new
check "conn.new：直接进表单，选中「新建」，光标在 name 上" eval 'mgr_open && [[ -z $(fval name) && $(lrows | sed "/^$/d" | tail -1) == 新建 && $(e2e_flag cursor_flag) == 1 && $(( $(e2e_flag cursor_y) + 1 )) == $(( $(box | cut -d" " -f2) + 1 )) ]] || { echo "  name [$(fval name)] cursor $(e2e_flag cursor_flag)"; false; }'
key Escape; sleep 0.3
check "conn.new 进的表单：esc 直接关闭浮层" eval '! mgr_open'

# ---- 新建一个（带密码）：追加在文件末尾，前面的字节不动；密码进钥匙串，文件里没有明文
read -r PG_USER PG_PW PG_HOST PG_PORT PG_DB < <(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p.username, p.password, p.hostname, p.port, p.path[1:])' "$SQLMUX_TEST_PG")
clr() { local i; for ((i = 0; i < 40; i++)); do e2e_keys BSpace; done; sleep 0.2; }
fill() { e2e_type "$1"; key Tab; key Tab; key Tab; e2e_type "$2"; key Tab; clr; e2e_type "$3"; key Tab; e2e_type "$4"; key Tab; e2e_type "$5"; key Tab; e2e_type "$6"; sleep 0.3; }
write_conns
SOLO= start -C "$D/c" -S "$ST"; sleep 0.5
cmdp conn.new; fill kc "$PG_HOST" "$PG_PORT" "$PG_USER" "$PG_PW" "$PG_DB"; key Enter; sleep 1.2
check "新建 kc（带密码）：connections.toml 前面的内容一个字节不动（原文是新文件的前缀），后面追加了 kc 这一块" eval 'cmp -s "$D/orig.toml" <(head -c "$(wc -c <"$D/orig.toml")" "$(ct)") && tail -n +"$(( $(wc -l <"$D/orig.toml") + 1 ))" "$(ct)" | grep -q "^name = \"kc\""'
check "密码进了钥匙串（假 security 的记录 sqlmux-kc），文件里没有明文密码，写的是 password_cmd" eval '[[ $(cat "$E2E_TMP/keychain/sqlmux-kc") == "$PG_PW" ]] && ! tail -n 6 "$(ct)" | grep -qE "^password = |:sqlmux@" && tail -n 8 "$(ct)" | grep -qE "^password_cmd = .*(sqlmux-kc|sqlmux '"'"'kc'"'"')"'
key Escape; sleep 0.4
check "树里多了 kc" eval '[[ -n $(nrow kc) ]]'

# ---- 改名 kc → kc2：钥匙串里的项跟着搬，password_cmd 改写
cmdp conn.manage; key j; key j; key Tab; sleep 0.3
check "（准备）选中 kc（第 3 项），光标在 name 上" eval '[[ $(fval name) == kc ]] || { echo "  sel [$(lsel)] name [$(fval name)] list: $(lrows | head -4 | tr "\n" ,)"; false; }'
clr; e2e_type kc2; key C-s; sleep 1.5
check "改名 kc2：文件里 name = \"kc2\"，没有 name = \"kc\"，password_cmd 改写成 sqlmux-kc2" eval 'grep -q "^name = \"kc2\"" "$(ct)" && ! grep -q "^name = \"kc\"$" "$(ct)" && grep -qE "password_cmd = .*(sqlmux-kc2|sqlmux '"'"'kc2'"'"')" "$(ct)" && ! grep -qE "sqlmux-kc.[ ]*-w|sqlmux '"'"'kc'"'"'" "$(ct)" || { tail -12 "$(ct)"; false; }'
check "钥匙串里的项搬到了新名字（sqlmux-kc2 有，sqlmux-kc 没有了，密码没丢）" eval '[[ $(cat "$E2E_TMP/keychain/sqlmux-kc2") == "$PG_PW" && ! -e "$E2E_TMP/keychain/sqlmux-kc" ]]'
check "doraemon 和 seed 的块（以及注释）仍是原文：文件开头 14 行没变" eval 'cmp -s <(head -n "$(wc -l <"$D/orig.toml")" "$D/orig.toml") <(head -n "$(wc -l <"$D/orig.toml")" "$(ct)")'

# ---- 删除 kc2：钥匙串里的项一并删
key Escape; key Escape; sleep 0.3; cmdp conn.manage; key j; key j; key x; sleep 0.5
check "x：确认「删除连接 kc2？」" eval 'e2e_plain | grep -q "删除连接 kc2？"'
key y; sleep 1.5
check "y：文件里没有 kc2 了（别的原样），钥匙串里的 sqlmux-kc2 也没了" eval '! grep -q "kc2" "$(ct)" && [[ ! -e "$E2E_TMP/keychain/sqlmux-kc2" ]] && cmp -s <(head -n "$(wc -l <"$D/orig.toml")" "$D/orig.toml") <(head -n "$(wc -l <"$D/orig.toml")" "$(ct)")'
key Escape; key Escape; sleep 0.3

# ---- 文件只读：先拒绝，钥匙串不碰
chmod 444 "$(ct)"; cp "$(ct)" "$D/before-ro.toml"
cmdp conn.new; fill ro1 "$PG_HOST" "$PG_PORT" "$PG_USER" "pw-ro" "$PG_DB"; key Enter; sleep 1
e2e_skip_if_root "connections.toml 只读时保存：被拒绝（浮层不关、有错误），文件没变" || check "connections.toml 只读时保存：被拒绝（浮层不关、有错误），文件没变" eval 'mgr_open && cmp -s "$D/before-ro.toml" "$(ct)"'
e2e_skip_if_root "…而且是在存钥匙串之前拒绝的：没有 sqlmux-ro1（task.md F8.16 审查时定的顺序：先预检文件，再存钥匙串，再写文件）" || check "…而且是在存钥匙串之前拒绝的：没有 sqlmux-ro1（task.md F8.16 审查时定的顺序：先预检文件，再存钥匙串，再写文件）" eval '[[ ! -e "$E2E_TMP/keychain/sqlmux-ro1" ]]'
chmod 644 "$(ct)"; key Escape; key Escape; sleep 0.3

# ---- 改名连带 state.json 里按连接名记的东西（recent、tables 的键）
write_conns
SOLO= start -C "$D/c" -S "$ST"; sleep 0.5
key C-h; sleep 0.3; for i in 1 2 3 4 5 6 7 8 9 10 11 12; do key j; done; sleep 0.2
pal "@t_user seed"; sleep 0.5
key Escape; sleep 0.2
cmdp conn.manage; key j; key Tab; sleep 0.3
clr; e2e_type seed2; key C-s; sleep 1.5
check "seed 改名 seed2：文件里是 seed2；state.json 里按连接名记的键（recent / tables / queries）没有 seed 了" eval 'grep -q "^name = \"seed2\"" "$(ct)" && ! grep -q "\"seed/" "$ST/sqlmux/state.json" 2>/dev/null || { cat "$ST/sqlmux/state.json" 2>/dev/null | head -c 600; false; }'
key Escape; key Escape; sleep 0.3

# ---- 列表上 C-s 是保存表单，不是全局保存（背后表格的修改不会被存进库）：自建库
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -v ON_ERROR_STOP=1 -c "create table t_cs (id serial primary key, v text)" -c "insert into t_cs (v) values ('a')"
mkdir -p "$D/own"; (umask 077; printf '[[connection]]\nname = "d"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml")
SOLO= start -C "$D/own"; open_table t_cs; wait_for 8 settled
key o; sleep 0.3
check "（准备）t_cs 里新增了一行（行号栏 +，没保存）" eval 'e2e_plain | grep -qE "│ +\+ │"'
cmdp conn.manage
e2e_keys C-s; sleep 1.2
check "管理面板的列表上按 C-s：保存的是表单（没有改动），不是背后的表格：库里还是 1 行" eval '[[ $(psql "$E2E_DB" -At -c "select count(*) from t_cs") == 1 ]]'
key Escape; sleep 0.4
check "关掉浮层之后表格里新增的行还在，没保存" eval 'e2e_plain | grep -qE "│ +\+ │"'

e2e_done
