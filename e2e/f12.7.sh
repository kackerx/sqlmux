#!/usr/bin/env bash
# F12.7 表头上的排序标记（specs/m12-grid/task.md F12.7；tech-design §7.6「按列排序」）
# 排序列表里的列，表头列名后面画方向图标（sort_asc U+F062 / sort_desc U+F063，warn 色）；多列时图标前加序号 1 2；默认排序不画；转置视图下不画。自建库 t_f。
. "$(dirname "$0")/lib.sh"
SOLO=1
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/ordering.sh"
. "$(dirname "$0")/cellops.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table t_f (id int primary key, name text, qty int, tag text)" -c "insert into t_f values (1,'ann',30,'x'),(2,'bob',10,null),(3,'o''neil',20,'x')" >/dev/null
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
WARN=#e0af68
hrow() { e2e_text 35 159 "$(hy)" | sed 's/  */ /g'; }
marks() { hrow | python3 -c 'import sys; print("".join(c for c in sys.stdin.read() if c in ""))'; }   # 表头里的方向图标

start -C "$D/own"; open_table t_f; wait_for 8 settled
check "默认排序（只按 id）：表头里没有方向图标（不然每张表的 id 都带箭头）" eval '[[ -z $(marks) ]] && qb_has "ORDER id"'
x=$(colx qty)
e2e_click $((x + 1)) "$(hy)"; wait_for 8 settled; sleep 0.5
check "点 qty 表头一次：qty 后面是升序图标 ↑（U+F062），别的列没有" eval '[[ $(hrow) == *"qty "$(printf "\\xef\\x81\\xa2")* && $(marks | python3 -c "import sys; print(len(sys.stdin.read().strip()))") == 1 ]] || { echo "  $(hrow)"; false; }'
check "图标是 warn 色（$WARN）" eval 'c=$(e2e_find "$(printf "\\xef\\x81\\xa2")" "$(hy)" | cut -d" " -f1); style_has "$c" "$(hy)" fg=$WARN'
e2e_click $((x + 1)) "$(hy)"; wait_for 8 settled; sleep 0.5
check "再点：降序图标 ↓（U+F063）" eval '[[ $(hrow) == *"qty "$(printf "\\xef\\x81\\xa3")* ]]'
e2e_click $((x + 1)) "$(hy)"; wait_for 8 settled; sleep 0.5
check "第三次点回默认：图标消失" eval '[[ -z $(marks) ]]'
# ---- 两列：序号加图标
key g g; key 0; key l; key l; key g s; wait_for 8 settled; sleep 0.5
key h; key g S; wait_for 8 settled; sleep 0.5
check "gs qty、gS name：qty 1↑、name 2↑（序号在图标前，同下拉里的 1↑ 2↓ 标法）" eval '[[ $(hrow) == *"name 2"$(printf "\\xef\\x81\\xa2")* && $(hrow) == *"qty 1"$(printf "\\xef\\x81\\xa2")* ]] || { echo "  $(hrow)"; false; }'
check "chip 是 qty ↑, name ↑" eval 'qb_has "ORDER qty ↑, name ↑"'
# ---- 转置视图下不画
key T; sleep 0.5
check "转置视图：不画方向图标（表头是记录号）" eval '[[ -z $(for y in $(seq "$(hy)" 30); do e2e_text 35 159 "$y"; done | python3 -c "import sys; print(\"\".join(c for c in sys.stdin.read() if c in \"\\uf062\\uf063\"))") ]]'
key T; sleep 0.4
check "转回竖直：图标又在" eval '[[ -n $(marks) ]]'
e2e_done
