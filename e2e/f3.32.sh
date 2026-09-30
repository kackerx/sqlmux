#!/usr/bin/env bash
# F3.32 整行复制粘贴与复制高亮（specs/m3-console/task.md F3.32；tech-design §10.6、§7.3 yank、§11「寄存器」）
# 系统剪贴板是 lib.sh 放在 PATH 最前面的假 pbcopy（clip 读它，AGENTS.md「隔离用户数据」）。复制高亮只闪 150ms：
# 用 pipe-pane 把程序写到终端的字节录下来，找 yank 底色（#e0af68，48;2;224;175;104）。自建库：yyp 之后要保存。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -v ON_ERROR_STOP=1 -c "create table t_ident (id serial primary key, seq int generated always as identity, v text)" -c "insert into t_ident (v) values ('abc')" \
  -c "create table t_gen (id serial primary key, v text, g int generated always as (length(v)) stored)" -c "insert into t_gen (v) values ('abc')"
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
psql_n() { psql "$E2E_DB" -At -c "$1"; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
rowno() { local c; c=$(e2e_find ┼ "$(grid_y)"); e2e_text 35 $((${c%% *} - 2)) "$(row_y "$1")" | tr -d ' '; }
cell() { local x; x=$(col_x "$1"); e2e_text "$x" $((x + 8)) "$(row_y "$2")" | sed 's/ *│.*//; s/ *$//; s/^ *//'; }
flashed() { local n; for n in $(seq 20); do LC_ALL=C grep -q '48;2;224;175;104' "$REC" 2>/dev/null && return 0; sleep 0.05; done; false; }   # 录下的输出里出现过 yank 底色
rec() { REC=$D/rec.$1; : >"$REC"; e2e_record "$REC"; }                           # NAME：从现在起录一段
unrec() { t pipe-pane -t t; }
# want_tsv SQL：一行值按 encoding/csv 的 Tab 分隔写法（含 Tab、换行、" 的加引号，" 写两遍；NULL 为空）
want_tsv() { psql "$E2E_DB" -At -F $'\x1f' -c "$1" | python3 -c 'import csv, sys; csv.writer(sys.stdout, delimiter="\t", lineterminator="").writerow(sys.stdin.read().rstrip("\n").split("\x1f"))'; }
COLS="id, user_id, status, amount, paid, meta, raw, note, created_at, deleted_at"

start -C "$D/own"; open_table t_order; wait_for 8 settled

# ---- yy 复制整行：剪贴板里是可见列的 TSV；这一行闪一下 yank 色（task.md 验收）
rec yy; key y y; flashed; f=$?; unrec; wait_for 3 eval '[[ -n $(clip) ]]'
check "表格 yy：剪贴板里是整行的 TSV（可见列、按显示顺序，NULL 为空，带 \" 的加引号）" eval '[[ "$(clip)" == "$(want_tsv "select $COLS from t_order where id = 1")" ]] || { echo "  clip: $(clip | cat -v)"; echo "  want: $(want_tsv "select $COLS from t_order where id = 1" | cat -v)"; false; }'
check "yy 时这一行闪一下 yank 色（黄底）" eval '(( f == 0 ))'
key 3 l; rec yl; key y l; flashed; f=$?; unrec
check "yl 复制单元格：剪贴板里是 amount 的 1.99，也闪一下" eval '[[ "$(clip)" == 1.99 ]] && (( f == 0 ))'
key 0

# ---- p 在光标下面粘贴成一个新行：自增主键是 <default>，其余照抄；C-s 存进去（task.md 验收）
key p
check "p：第 1 行下面多出一个新行（行号 +），id 是 <default>，其余和第 1 行相同" eval '[[ $(e2e_text 35 60 $(row_y 2) | tr -s " ") == " + │ <de"* && $(cell user_id 2) == 2 && $(cell status 2) == runni* && $(cell amount 2) == 1.99 && $(cell note 2) == "<nul"* && $(rowno 3) == 2 ]] || { e2e_text 35 159 $(row_y 2); false; }'
key C-s; wait_for 8 eval '[[ $(qb) == *已保存* ]]'
rows() { psql_n "select concat_ws('|', id, user_id, status, amount, paid, meta, raw, coalesce(note, 'NULL'), created_at, coalesce(deleted_at::text, 'NULL')) from t_order where id in (1, 6001) order by id"; }
check "C-s：存进去，新行的 id 是序列给的 6001，其余各列和 id 1 相同" eval 'r=($(rows | cut -d"|" -f2- | tr " " "_")); [[ $(psql_n "select count(*) from t_order where id > 6000") == 1 && ${#r[@]} == 2 && ${r[0]} == "${r[1]}" ]] || rows | sed "s/^/  /"'

# ---- yl 不动给 p 用的那一行；在别的表里 p 不做事；没复制过行时 p 不做事
key g g; key 2 j; key 3 l; key y l; key 0; key p                               # 在第 3 行（amount 3.99）上 yl，再 p
check "yl 之后 p：粘的还是 yy 复制的第 1 行（yl 不动它），剪贴板里是 3.99" eval '[[ $(rowno 4) == + && $(cell user_id 4) == 2 && $(cell amount 4) == 1.99 && "$(clip)" == 3.99 ]]'
key r
open_table t_user; wait_for 8 settled; key p
check "在别的表（t_user）里 p：不做事" eval '[[ $(rowno 2) == 2 ]] && ! screen_has "只读"'
start -C "$D/own"; open_table t_order; wait_for 8 settled; key p
check "重新启动、没复制过行时 p：不做事" eval '[[ $(rowno 2) == 2 ]]'

# ---- 不在行标识里的 generated always as identity 列：粘成 <default>，C-s 存得进去（5514975，原来报 428C9）
open_table t_ident; wait_for 8 settled; key y y; key p
check "t_ident 的 yyp：id（serial 主键）和 seq（identity）都是 <default>，v 照抄 abc" eval '[[ $(e2e_text 35 70 $(row_y 2) | tr -s " ") == " + │ <de"*"│ <de"*"│ abc"* ]] || { e2e_text 35 70 $(row_y 2); false; }'
key C-s; wait_for 8 eval '[[ $(qb) == *已保存* || -n $(errbar 1) ]]'
ident_rows() { psql_n "select string_agg(concat_ws(':', id, seq, v), ' ' order by id) from t_ident"; }
check "C-s：存进去，seq 由 identity 给出 2" eval '[[ $(ident_rows) == "1:1:abc 2:2:abc" ]] || { errbar 1; ident_rows; false; }'
# generated（stored）列照抄，保存时报错：已知上限（ponytail）
open_table t_gen; wait_for 8 settled; key y y; key p; key C-s; wait_for 8 eval '[[ -n $(errbar 1) || $(qb) == *已保存* ]]'
check "t_gen 的 yyp 再 C-s：generated 列 g 照抄了，报 428C9（已知上限），库里没变" eval '[[ $(errbar 1 | head -1) == "[428C9] "* && $(psql_n "select count(*) from t_gen") == 1 ]] || { errbar 1; false; }'

# ---- console 里 yy 同样闪一下；结果区只读：p 不做事，yy / yl 照常
SOLO= start -C "$D/own"; key C-l; key i; e2e_type "select 7 as a, 'x' as b"; key Escape
rec cy; key y y; flashed; f=$?; unrec
check "console 里 yy：这一行闪一下 yank 色" eval '(( f == 0 ))'
check "console 的无名寄存器不写系统剪贴板（F3.38：只有 \"+ 写）" eval '[[ "$(clip)" != "select 7"* ]]'
key Enter; wait_for 8 eval '[[ $(bar) != *busy* && -n $(geom 3) ]]'; sleep 0.3
key C-j; key y y; sleep 0.3
check "结果区 yy：剪贴板里是这一行的 TSV" eval '[[ "$(clip)" == "$(printf "7\tx")" ]] || { clip | od -c | head -2; false; }'
key p; sleep 0.3
check "结果区 p：不做事（只读）" eval '[[ $(e2e_plain | grep -c " 7 │ x") == 1 ]] && ! screen_has "只读"'

e2e_done
