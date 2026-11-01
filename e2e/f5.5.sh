#!/usr/bin/env bash
# F5.5 工具按钮共用一条底色、竖线分隔（specs/m5-workspace/task.md F5.5；tech-design §7.8「工具按钮」）
# 底色、竖线的颜色、让位由 golden 覆盖（TestGoldenToolButtons）；这里在真终端里点 │ 两侧紧挨着的那一列，看落到哪个按钮上，
# 以及 │ 本身没有命中区。按钮：+ − │ 保存 刷新 │ 自动刷新 停止 │ 转置。自建库：要删一行再保存、锁表。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f55-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
BORDER=#565f89 SELECT=#364a82
psql_n() { psql "$E2E_DB" -At -c "$1"; }
# bars：查询条第二行上那几条 border 色的 │（组之间的分隔）；按钮会变宽（保存带修改数），每次现找
bars() { local x; for x in $(e2e_find "│" 3); do ((x > 34 && x < 159)) && style_has "$x" 3 "fg=$BORDER" >/dev/null && echo "$x"; done | tr '\n' ' '; }
bar_x() { bars | cut -d' ' -f"$1"; }                                            # N：第 N 条 │ 的列
lit() { local x; for x in $(seq 35 158); do style_has "$x" 3 "bg=$SELECT" >/dev/null && echo "$x"; done | tr '\n' ' '; }   # 查询条上亮着的格
dd() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }                    # 下拉框
header() { e2e_text 35 159 "$(hy)"; }
mark() { T0=$(psql_n "select clock_timestamp()"); }
fetched() { [[ -n $(psql_n "select 1 from pg_stat_activity where application_name = '$APP' and query_start > '$T0' and query like '%t_sku%'") ]]; }

start -C "$D/own"; open_table t_sku; wait_for 8 settled
read b1 b2 b3 _ <<<"$(bars)"
check "按钮条上有三条 │（四组之间），各隔开一个三格的按钮" eval '[[ -n $b3 ]] && (( b2 - b1 == 7 && b3 - b2 == 7 )) || { echo "  bars: $(bars)"; false; }'

# ---- │ 本身没有命中区：悬停不亮、点了不做事
e2e_move "$b1" 3; sleep 0.3
check "指针停在 │ 上：哪个按钮都不亮" eval '[[ -z $(lit) ]] || { echo "  lit: $(lit)"; false; }'
e2e_move $((b1 - 1)) 3; sleep 0.3
check "停在 │ 左边一列：只亮删除行那一个按钮（三格）" eval '[[ $(lit) == "$((b1 - 3)) $((b1 - 2)) $((b1 - 1)) " ]] || { echo "  lit: $(lit)"; false; }'
e2e_move $((b1 + 1)) 3; sleep 0.3
check "停在 │ 右边一列：只亮保存那一个按钮" eval '[[ $(lit) == "$((b1 + 1)) $((b1 + 2)) $((b1 + 3)) " ]] || { echo "  lit: $(lit)"; false; }'
mark
for b in "$b1" "$b2" "$b3"; do e2e_click "$b" 3; sleep 0.4; done
check "依次点三条 │：什么都不做（没有删行、没有下拉框、没转置、没取数）" eval '[[ $(bars) == "$b1 $b2 $b3 " && -z $(dd) && $(header) != *"│ 1 "* ]] && ! fetched && ! screen_has 查询已取消'

# ---- │ 两侧紧挨着的一列各落在对的按钮上
e2e_click $((b1 - 1)) 3; sleep 0.4
check "点第一条 │ 左边一列：删除行（光标所在的 sku_0 标成删除，保存按钮带上修改数，│ 跟着右移）" eval '(( $(bar_x 2) > b2 )) || { echo "  bars: $(bars)"; false; }'
e2e_click $(( $(bar_x 1) + 1 )) 3; wait_for 8 settled; sleep 0.3
check "点第一条 │ 右边一列：保存，库里少了 sku_0" eval '[[ $(psql_n "select count(*) from t_sku") == 19 && -z $(psql_n "select 1 from t_sku where code = '"'sku_0'"'") ]]'
read b1 b2 b3 _ <<<"$(bars)"
mark; e2e_click $((b2 - 1)) 3; wait_for 8 settled
check "点第二条 │ 左边一列：刷新，重新取了 t_sku" fetched
e2e_click $((b2 + 1)) 3; sleep 0.4
check "点第二条 │ 右边一列：自动刷新，下拉框开在这个按钮下面（左边框对着按钮的第一列）" eval 'g=($(dd)); [[ ${g[0]} == $((b2 + 1)) ]] || { echo "  dropdown at [$(dd)], │ at $b2"; false; }'
key Escape
e2e_lock t_sku; key R
check "锁住 t_sku 再 R：取数在等锁" wait_for 3 eval '[[ -n $(e2e_waiting "$APP") ]]'
e2e_click $((b3 - 1)) 3; sleep 0.4
check "点第三条 │ 左边一列：停止，取数取消，toast「查询已取消」" eval 'toast_is "查询已取消" && wait_for 3 eval "[[ -z \$(e2e_waiting \"\$APP\") ]]"'
e2e_unlock; sleep 0.3
e2e_click $((b3 + 1)) 3; sleep 0.4
check "点第三条 │ 右边一列：转置（表头变成记录号）" eval '[[ $(header) == *"│ 1 "*"│ 2 "* ]] || { echo "  $(header)"; false; }'

e2e_done
