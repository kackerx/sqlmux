#!/usr/bin/env bash
# F22.13 外键跳转（issue #11；specs/m22-menus-keys/task.md F22.13；tech-design §7.6「外键跳转」）
# 外键格子上 gd 或右键「转到被引用的行」：开被引用的表、WHERE = 被引用列 = 这一行的值（多列用 and、引号转义、跨 schema），只剩那一行；一路 gd 跳多层；
# 被引用的表已开着：切过去并替换 WHERE；NULL 的格子 toast「外键为空…」，非外键列 toast「这一列不是外键」，非外键列的右键菜单没有这一项。
# 自建库：t_order_item → t_order → t_user（seed 的外键）；c13 的 (pa, pb) → f13.p (a, b)（多列、跨 schema、值里有引号）。state 放临时目录。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -v ON_ERROR_STOP=1 <<'SQL' || exit 1
create schema f13; create table f13.p (a int, b text, primary key (a, b));
insert into f13.p values (1, 'it''s'), (1, 'x'), (2, 'y');
create table c13 (id int primary key, pa int, pb text, constraint c13_fk foreign key (pa, pb) references f13.p (a, b));
insert into c13 values (1, 1, 'it''s'), (2, null, null), (3, 2, 'y'), (4, 1, null);
SQL
mkdir -p "$D/c"; (umask 077; printf '[[connection]]\nname = "rw"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/c/connections.toml")
wh() { e2e_text 36 150 2 | sed 's/ *C-r ▾ *$//; s/^ *WHERE *//; s/ *$//'; }
wh_is() { local w; w=$(wh); [[ $w == "$1" ]] || { echo "  WHERE '$w', want '$1'"; false; }; }
nrows() { e2e_plain | grep -cE "│ +[0-9]+ │ +[^ ]"; }
title() { e2e_text 34 198 1 | noicon | sed -E 's/^┌─ ① +//; s/ ─+.*//'; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
mitems() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/^[^ ]* //; s/ *$//'; done | tr '\n' '|'; }
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
toasted() { [[ $(e2e_text 1 200 $(($(e2e_flag pane_height) - 1))) == *"$1"* ]]; }
gd() { e2e_keys g; sleep 0.1; e2e_keys d; sleep 0.4; }

SOLO=1 start -x 200 -C "$D/c" -S "$D/st"; sleep 1
open_table c13
check "（准备）c13 打开了，光标在 id 列" eval '[[ $(pos) == 1,1 ]]'
gd
check "非外键列（id）：gd toast「这一列不是外键」，没有跳" eval 'wait_for 3 toasted "这一列不是外键" && [[ $(title) == c13 ]]'
key l
check "（准备）光标在 pa 列（外键的第一列）" eval '[[ $(pos) == 1,2 ]]'
gd
check "gd：开了被引用的表 p（f13 里的），WHERE 是 \"a\" = 1 and \"b\" = 'it''s'（多列 and、引号转义、跨 schema）" eval 'wait_for 5 eval "[[ \$(title) == p ]]" && wh_is "\"a\" = 1 and \"b\" = '"'"'it'"''"'s'"'"'"'
check "只剩被引用的那一行（1 行，b 是 it's）" eval '[[ $(nrows) == 1 ]] && e2e_plain | grep -qE "│ +1 │ +1 +│ +it.s"' 
check "c13 的 tab 还在（pane 里两个 tab）" eval 'tabs_are 1 "1:c13- │ 2:p*"'

key g; key T; sleep 0.6
check "（准备）gT 回到 c13" eval '[[ $(title) == c13 ]]'
key j
check "（准备）光标在第 2 行 pa（值是 NULL）" eval '[[ $(pos) == 2,2 ]]'
gd
check "NULL 的外键格子：toast「外键为空，没有被引用的行」，没有跳" eval 'wait_for 3 toasted "外键为空，没有被引用的行" && [[ $(title) == c13 ]]'
key j; key j; sleep 0.3
check "（准备）第 4 行：pa = 1、pb = NULL" eval '[[ $(pos) == 4,2 ]]'
gd
check "多列外键里任何一列 NULL（pa 有值、pb 是 NULL）：同样 toast，没有跳" eval 'wait_for 3 toasted "外键为空，没有被引用的行" && [[ $(title) == c13 ]]'
key k
check "（准备）第 3 行 pa = 2" eval '[[ $(pos) == 3,2 ]]'
gd
check "被引用的表 p 已开着：切到它的 tab、WHERE 被替换成 \"a\" = 2 and \"b\" = 'y'，仍是两个 tab" eval 'wait_for 5 eval "[[ \$(title) == p ]]" && wh_is "\"a\" = 2 and \"b\" = '"'"'y'"'"'" && tabs_are 1 "1:c13- │ 2:p*" && [[ $(nrows) == 1 ]]'
key /; sleep 0.3; key C-r; sleep 0.6
check "p 的 WHERE 历史（C-r）里有这两次跳转写的条件" eval 'screen_has "\"a\" = 1 and \"b\" = '"'"'it'"''"'s'"'"'" && screen_has "\"a\" = 2 and \"b\" = '"'"'y'"'"'"'
key Escape; key Escape; sleep 0.3

# 右键菜单：外键列的格子有「转到被引用的行 gd」，非外键列没有；点它也跳
key g; key T; sleep 0.6; key g; key g; sleep 0.3
hx=$(e2e_find "pa" 4 | cut -d' ' -f1); ix=$(e2e_find "id" 4 | cut -d' ' -f1); gy=$(grid_y)
rclick $((hx + 1)) $((gy + 1))
check "右键 pa 列的格子：菜单里有「转到被引用的行」，右边是 gd" eval '[[ $(mitems) =~ 转到被引用的行\ +gd ]] || { echo "  [$(mitems)]"; false; }'
key Escape; sleep 0.3
rclick $((ix + 1)) $((gy + 1))
check "右键 id 列的格子：菜单里没有「转到被引用的行」" eval '[[ -n $(mitems) && $(mitems) != *转到被引用的行* ]] || { echo "  [$(mitems)]"; false; }'
key Escape; sleep 0.3
rclick $((hx + 1)) $((gy + 1))
g=($(pop)); my=$(for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do [[ $(e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y) == *转到被引用的行* ]] && echo $y; done | head -1)
e2e_click $((g[0] + 4)) "$my"; sleep 1.5
check "点菜单里的「转到被引用的行」：同 gd，p 的 WHERE 是 \"a\" = 1 and \"b\" = 'it''s'" eval '[[ $(title) == p ]] && wh_is "\"a\" = 1 and \"b\" = '"'"'it'"''"'s'"'"'"'
_e2e_kill

# ---- seed 的外键链：t_order_item.order_id → t_order.id，t_order.user_id → t_user.id（一路 gd 跳两层）
SOLO=1 start -x 200 -C "$D/c" -S "$D/st2"; sleep 1
open_table t_order_item; key j; key j
check "（准备）t_order_item 的第 3 行，光标在 order_id 列（第 1 列）" eval '[[ $(pos) == 3,1 ]]'
OID=$(psql "$E2E_DB" -At -c "select order_id from t_order_item order by order_id, line_no offset 2 limit 1")
UID_=$(psql "$E2E_DB" -At -c "select user_id from t_order where id = $OID")
gd
check "gd：t_order 打开，WHERE \"id\" = $OID，只剩一行" eval 'wait_for 5 eval "[[ \$(title) == t_order ]]" && wh_is "\"id\" = $OID" && [[ $(nrows) == 1 ]]'
key l
check "（准备）光标移到 t_order 的 user_id 列" eval '[[ $(pos) == 1,2 ]]'
gd
check "再 gd：t_user 打开，WHERE \"id\" = $UID_，只剩一行（跳了两层）" eval 'wait_for 5 eval "[[ \$(title) == t_user ]]" && wh_is "\"id\" = $UID_" && [[ $(nrows) == 1 ]]'
e2e_done
