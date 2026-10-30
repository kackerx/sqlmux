#!/usr/bin/env bash
# F19.1 命令行直接开一张表：--table / --where（specs/m19-polish/task.md F19.1；tech-design §14「启动」）
# sqlmux play --table t_order --where "id < 11"：默认布局、t_order 开成 data pane 里聚焦的 tab、WHERE 已执行、10 行；参数在位置参数前后都认；
# 不给连接用 connections.toml 的第一个；schema.表 能写；没有的表 toast 并留在默认布局；Redis 的 --table 是键名、开键 tab；
# 三种 usage 错误在读配置之前退出；退出不写 last.toml。PG 用共用库（只读），Redis 每个脚本起私有的 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1; redis_seed
mkdir -p "$D/c" "$D/xc/sqlmux" "$D/st"
(umask 077; printf '[[connection]]\nname = "play"\nengine = "postgres"\ndsn = "%s"\n\n[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SQLMUX_TEST_PG" "$RPORT" >"$D/c/connections.toml")
cp "$D/c/connections.toml" "$D/xc/sqlmux/connections.toml"
quit_app() { e2e_type ':qa'; e2e_keys Enter; wait_for 5 screen_has '[e2e-exit 0]'; }
lastf() { echo "$D/st/sqlmux/last.toml"; }
toast_text() { e2e_text 1 "$(e2e_flag pane_width)" $(( $(H) - 1 )); }
tstart() { e2e_start -C "$D/c" -S "$D/st" "$E2E_BIN $1"; wait_for 6 flag_is alternate_on 1; wait_for 15 settled; sleep 1; }

# ---- 先正常开一次再退出，留下 last.toml（后面几次都不许改它）
tstart "play"; quit_app; e2e_stop
check "（准备）正常退出写了 last.toml" eval '[[ -f "$(lastf)" ]]'
cp "$(lastf)" "$D/last.orig"; M0=$(stat -f %m "$(lastf)"); sleep 1.2

# ---- 参数在连接名后面
tstart "play --table t_order --where 'id < 11'"
check "play --table t_order --where 'id < 11'：默认布局（侧栏、①、② query_1），① 聚焦，tab 条上是 t_order" eval 'tabs_are 1 "1:t_order*" && [[ $(focused) == 1 ]] && tabs_are 2 "1:query_1*" || { tabbar 1; focused; false; }'
check "WHERE 输入框里是 id < 11，已执行：只有一页，第一行 id 1、最后一行 id 10，没有 11" eval 'where_in | grep -q "id < 11" && qb | grep -q "PAGE 1/1" && e2e_plain | grep -qE "│ +1 │ +1 │" && e2e_plain | grep -qE "│ +10 │ +10 │" && ! e2e_plain | grep -qE "│ +11 │ +11 │" || { where_in; qb; false; }'
check "工作区树里 data 下有 t_order（开的是 tab，不是别的）" eval 'e2e_plain | grep -q "t_order"'
quit_app; e2e_stop
check "退出没有写 last.toml：内容和 mtime 都和之前一样" eval 'cmp -s "$(lastf)" "$D/last.orig" && [[ $(stat -f %m "$(lastf)") == $M0 ]]'

# ---- 参数在连接名前面、不给 where、schema.表
tstart "--table t_order --where 'id < 4' play"
check "参数在连接名前面也认：t_order、WHERE id < 4、只有 id 1 2 3 三行" eval 'tabs_are 1 "1:t_order*" && where_in | grep -q "id < 4" && e2e_plain | grep -qE "│ +3 │ +3 │" && ! e2e_plain | grep -qE "│ +4 │ +4 │" || { where_in; false; }'
e2e_stop
tstart "--table public.t_user play"
check "--table public.t_user：schema 写出来也行，开出 t_user，WHERE 是空的，一页" eval 'tabs_are 1 "1:t_user*" && ! where_in | grep -q "=" && qb | grep -q "PAGE 1/1" || { tabbar 1; qb; false; }'
e2e_stop

# ---- 不给连接：connections.toml 的第一个（play）
tstart "--table t_sku"
check "sqlmux --table t_sku（不给连接）：用第一个连接 play，开出 t_sku，状态栏连接是 play" eval 'tabs_are 1 "1:t_sku*" && bar | grep -q "play · "'
e2e_stop

# ---- 没有这张表：toast，默认布局
tstart "play --table nope_table"
check "没有的表：toast「play：没有表 nope_table」" eval 'toast_text | grep -q "play：没有表 nope_table" || { toast_text; false; }'
check "留在默认布局：① 是引导页（没有表 tab），② 有 query_1，程序没崩" eval 'tabs_are 2 "1:query_1*" && e2e_plain | grep -q "打开表" && running'
e2e_stop

# ---- Redis：--table 是键名，开键 tab
tstart "rd --table hash:user"
check "rd --table hash:user：开出键 tab hash:user（标题和 tab 条里有它）" eval 'tabbar 1 | grep -q "hash:user" || { tabbar 1; e2e_plain | head -3 | cut -c1-120; false; }'
e2e_stop
check "（对照）last.toml 到现在还是原样" eval 'cmp -s "$(lastf)" "$D/last.orig"'

# ---- usage 错误：读配置之前（或刚读完连接）退出，英文、非零
ux() { XDG_CONFIG_HOME="$D/xc" XDG_STATE_HOME="$D/st2" "$E2E_BIN" "$@" 2>&1 </dev/null; echo "rc=$?"; }
check "--where 没有 --table：英文报错「--where needs --table」、退出码非零" eval 'o=$(ux --where "id < 3" play); echo "$o" | grep -q -- "--where needs --table" && ! echo "$o" | grep -q "rc=0" || { echo "$o"; false; }'
check "--table 和 --layout 同时给：usage 错误" eval 'o=$(ux --table t_order --layout x play); echo "$o" | grep -qiE "usage|layout" && ! echo "$o" | grep -q "rc=0" || { echo "$o"; false; }'
check "Redis 连接加 --where：usage 错误" eval 'o=$(ux rd --table hash:user --where "x"); echo "$o" | grep -qiE "usage|where|redis" && ! echo "$o" | grep -q "rc=0" || { echo "$o"; false; }'
check "两个位置参数：usage 错误（位置参数仍然最多一个）" eval 'o=$(ux play rd --table t_order); echo "$o" | grep -qi "usage" && ! echo "$o" | grep -q "rc=0" || { echo "$o"; false; }'
check "usage 行里列出 --table 和 --where（两个位置参数时打印的那一行）" eval 'ux play rd | grep -q -- "usage:.*--table.*--where"'
e2e_done
