#!/usr/bin/env bash
# F20.5 编辑器的 clipboard = "unnamedplus"（specs/m20-redis-ui/task.md F20.5；tech-design §11「寄存器」、§14 clipboard）
# 缺省（同 vim 的 clipboard=）：不带寄存器的 yank / 删除 / 修改不写系统剪贴板，p 贴无名寄存器。
# unnamedplus（同 nvim）：不带寄存器的 y d c x 同时写系统剪贴板，p / P 读系统剪贴板，读不到（工具失败或空）退回无名寄存器；
# 没有本地剪贴板工具时不等 OSC 52 的回应，直接用无名寄存器；WHERE 里同样；别的值照未知键报。
# 系统剪贴板是 lib.sh 放在 PATH 最前面的假 pbcopy / pbpaste（clip 读它、set_clip 写它），不碰用户真实的剪贴板。只读，用共用库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
printf 'clipboard = "unnamedplus"\n' >"$D/plus.toml"
printf 'clipboard = ""\n' >"$D/empty.toml"
printf 'clipboard = "bogus"\n' >"$D/bogus.toml"

paste() { t set-buffer -b e2e -- "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; key 0; }   # query 的内容换成 SQL，光标回到第 1 行行首
line() { e2e_text 104 159 $((1 + $1)) | sed 's/ *$//'; }                         # 第 N 行的文字（② query 从 x 111 起）
set_clip() { printf '%s' "$1" >"$E2E_TMP/clipboard"; }
clip_is() { local want=$1; wait_for 3 eval '[[ "$(clip; echo .)" == "$want." ]]' || { echo "  clip: [$(clip)], want [$want]"; false; }; }   # TEXT：剪贴板里是 TEXT
line_is() { [[ $(line "$1") == "$2" ]] || { echo "  line $1: [$(line "$1")], want [$2]"; false; }; }
SQL=$'select id, status\nfrom t_order\nwhere id < 5'

# ---- 缺省（没写 clipboard）：yank 不写系统剪贴板
start; key C-l; setsql "$SQL"
rm -f "$E2E_TMP/clipboard"
key y y; sleep 0.5
check "缺省：yy 不写系统剪贴板" eval '[[ ! -s "$E2E_TMP/clipboard" ]]'
key j; key p
check "缺省：p 贴无名寄存器（刚 yy 的第 1 行）" eval 'line_is 3 "select id, status" && line_is 2 "from t_order"'
key u
set_clip "CLIPTEXT"; key g g; key 0; key p; sleep 0.5
check "缺省：系统剪贴板里有别的内容，p 贴的还是无名寄存器（不读剪贴板）" eval 'line_is 2 "select id, status" && [[ $(line 1) != *CLIPTEXT* ]]'
key u

# ---- clipboard = "" 同缺省
start -c "$D/empty.toml"; key C-l; setsql "$SQL"
rm -f "$E2E_TMP/clipboard"; key d d; sleep 0.5
check "clipboard = \"\"：dd 不写系统剪贴板" eval '[[ ! -s "$E2E_TMP/clipboard" ]]'

# ---- unnamedplus：y d c x 写系统剪贴板
start -c "$D/plus.toml"; key C-l; setsql "$SQL"
check "（准备）unnamedplus 的配置没有 toast" eval '! screen_has "bogus"'
key y y; sleep 0.5
check "unnamedplus：yy 写系统剪贴板（这一行，带换行）" clip_is "select id, status"$'\n'
key j; key 0; key y i w; sleep 0.5
check "unnamedplus：yiw 写系统剪贴板（from）" clip_is from
key d d; sleep 0.5
check "unnamedplus：dd 写系统剪贴板（删掉的行）；行没了" eval 'clip_is "from t_order"$'"'\n'"' && line_is 2 "where id < 5"'
key u; key g g; key 0; key x; sleep 0.5
check "unnamedplus：x 写系统剪贴板（s）" eval 'clip_is s && line_is 1 "elect id, status"'
key u; key g g; key 0; key c i w; e2e_type ID; key Escape; sleep 0.5
check "unnamedplus：ciw 写系统剪贴板（被改掉的词 select）；文字改了" eval 'clip_is select && line_is 1 "ID id, status"'
key u

# ---- unnamedplus：p / P 读系统剪贴板
set_clip "XYZ"; key g g; key 0; key p; sleep 0.5
check "unnamedplus：p 贴出系统剪贴板的内容（XYZ），不是无名寄存器（那里是 select）" eval 'line_is 1 "sXYZelect id, status"'
key u; key 0; key P; sleep 0.5
check "unnamedplus：P 贴在光标前" eval 'line_is 1 "XYZselect id, status"'
key u
set_clip $'whole line\n'; key g g; key p; sleep 0.5
check "unnamedplus：剪贴板里是整行（带换行）：p 贴成下一行" eval 'line_is 2 "whole line" && line_is 3 "from t_order"'
key u

# ---- 读不到：退回无名寄存器
key g g; key y y; sleep 0.5; rm -f "$E2E_TMP/clipboard"; key j; key p; sleep 0.5
check "unnamedplus：剪贴板工具读失败（没有内容）：p 贴无名寄存器（刚 yy 的第 1 行）" eval 'line_is 3 "select id, status" && line_is 2 "from t_order"'
key u; : >"$E2E_TMP/clipboard"; key g g; key j; key p; sleep 0.5
check "unnamedplus：剪贴板是空的：p 贴无名寄存器" eval 'line_is 3 "select id, status"'
key u

# ---- 具名寄存器照旧：整条命令不执行；"+p 读剪贴板，空就什么都不贴
set_clip "KEEP"; key g g; e2e_type '"ayy'; sleep 0.5
check "unnamedplus：\"ayy 不执行，剪贴板不变" eval '[[ "$(clip)" == KEEP ]]'
: >"$E2E_TMP/clipboard"; key g g; key 0; e2e_type '"+p'; sleep 0.5
check "unnamedplus：剪贴板空时 \"+p 什么都不贴（F3.38）" eval 'line_is 1 "select id, status"'

# ---- WHERE 里同样
key Escape; key C-h; open_table t_order; wait_for 8 settled
key /; e2e_type "abc def"; key Escape; key 0; key y i w; sleep 0.5
check "unnamedplus：WHERE 里 yiw 写系统剪贴板（abc）" clip_is abc
set_clip "ZZ"; key '$'; key p; sleep 0.5
check "unnamedplus：WHERE 里 p 贴系统剪贴板的内容（ZZ）" eval '[[ $(where_in) == "abc defZZ"* ]] || { echo "  WHERE: [$(where_in)]"; false; }'
key Escape; key Escape

# ---- 没有本地剪贴板工具（-P /bin）：不等 OSC 52，p 直接用无名寄存器
start -P /bin -c "$D/plus.toml"; key C-l; setsql "$SQL"
key y y; key j; key p; sleep 0.8
check "没有剪贴板工具：yy 后 p 马上贴出无名寄存器里的那一行（不等 OSC 52 的回应）" eval 'line_is 3 "select id, status" && line_is 2 "from t_order"'
key u

# ---- 别的值：照未知键报，当缺省用
start -c "$D/bogus.toml"; sleep 0.8
check "clipboard = \"bogus\"：启动照常，toast 是「config.toml: 不认识的键 clipboard = \"bogus\"」" eval 'running && screen_has "config.toml: 不认识的键 clipboard = \"bogus\""' || e2e_plain | head -4
key C-l; setsql "$SQL"
rm -f "$E2E_TMP/clipboard"; key y y; sleep 0.5
check "clipboard = \"bogus\"：当缺省，yy 不写系统剪贴板" eval '[[ ! -s "$E2E_TMP/clipboard" ]]'

e2e_done
