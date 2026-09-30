#!/usr/bin/env bash
# F3.36 表格的 {N}G 跳到第 N 行（specs/m3-console/task.md F3.36；tech-design §7.6）
# 计数不知道时翻到哪一页、转置视图的第 N 个字段由单测覆盖；这里在真实 PG 上按键，看光标到哪一行、翻到哪一页。
# 自建库：最后一段要锁住 t_order，让 ] 的请求卡在路上。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"

row_y() { echo $(( $(grid_y) + $1 )); }
rowno() { local c; c=$(e2e_find ┼ "$(grid_y)"); e2e_text 35 $((${c%% *} - 1)) "$1" | tr -d ' '; }
idat() { e2e_text 35 60 "$(row_y "$1")" | awk -F'│' '{ gsub(/ /, "", $2); print $2 }'; }   # N：第 N 个显示的行的 id

start -C "$D/own"; open_table t_order; wait_for 8 settled

# ---- 同 nvim：{N}G、{N}gg 跳到第 N 行（task.md 验收）
key 1 5 G
check "15G：光标到第 15 行（id 15）" eval 'pos_is 15,1 && [[ $(idat 15) == 15 ]]'
key 3 g g
check "3gg：到第 3 行" pos_is 3,1
key G
check "不带次数的 G：照旧是本页的最后一行" pos_is 100,1
key g g
check "不带次数的 gg：本页第一行" pos_is 1,1

# ---- 不在当前页时翻到它所在的页；超出总行数时到最后一行
key g l; e2e_type 10; sleep 0.3; key Enter; wait_for 8 settled
key 1 5 G; wait_for 8 settled
check "LIMIT 10 时 15G：翻到第 2 页，光标在第 5 行（行号 15，id 15）" eval 'qb_has "PAGE 2/600" && pos_is 15,1 && [[ $(rowno $(row_y 5)) == 15 ]]'
key 9 9 9 9 9 G; wait_for 8 settled
check "99999G：超出总行数，到最后一页的最后一行（6000）" eval 'qb_has "PAGE 600/600" && pos_is 6000,1'
key 1 G; wait_for 8 settled
check "1G：回到第 1 页第 1 行" eval 'qb_has "PAGE 1/600" && pos_is 1,1'

# ---- ] 的请求还在路上时按 3G：最后停在第 3 行（23dca1d）
start -C "$D/own"; open_table t_order; wait_for 8 settled
e2e_lock t_order; key ']'; sleep 0.5; key 3 G; e2e_unlock; wait_for 8 settled; sleep 0.5
check "] 在等锁时按 3G：放锁后停在第 1 页第 3 行，不是第 2 页" eval 'qb_has "PAGE 1/60" && pos_is 3,1 && [[ $(rowno $(row_y 3)) == 3 ]]'

e2e_done
