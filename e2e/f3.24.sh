#!/usr/bin/env bash
# F3.24 新增与删除行（specs/m3-console/task.md F3.24；tech-design §10.6、§10.1、§10.3）
# INSERT / DELETE 的生成、DEFAULT VALUES、混在一次保存里的回滚由集成测试覆盖；新行、删除行的画法（含转置）由 golden 覆盖。
# 这里在真实 PG 上走：o / dd / r 和查询条的 + / −、保存进库、失败的文字（按新行的显示顺序数）、保存途中新增和删除、
# LIMIT 改小之后新行挂到哪、删除线（SGR 9 由 tmux 转发）。自建库：清空 t_order_item（它引用 t_order 的 1–200，挡住删除），
# t_def 有默认值，t_trig 的触发器不让插入，t_txt 的主键有空串。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -v ON_ERROR_STOP=1 -c "delete from t_order_item" -c "create table t_def (id serial primary key, v text default 'dv', n int)" -c "insert into t_def (v, n) values ('x', 1)" \
  -c "create table t_trig (id serial primary key, v text)" -c "insert into t_trig (v) values ('x')" \
  -c 'create function skip_row() returns trigger language plpgsql as $$ begin return null; end $$' \
  -c "create trigger skip before insert on t_trig for each row execute function skip_row()" \
  -c "create table t_txt (k text primary key, v int check (v < 100))" -c "insert into t_txt values ('', 1), ('a', 2)"
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
. "$(dirname "$0")/palette.sh"
WARN=#e0af68 ERROR=#f7768e DIM=#565f89
SAVE=$(printf '\xef\x83\x87') ADD=$(printf '\xef\x81\xa7') DEL=$(printf '\xef\x81\xa8')   # save、row_add U+F067、row_delete U+F068
psql_n() { psql "$E2E_DB" -At -c "$1"; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
cell() { local x; x=$(col_x "$1"); e2e_text "$x" $((x + 8)) "$(row_y "$2")" | sed 's/ *│.*//; s/ *$//; s/^ *//'; }   # NAME N：第 N 个显示的行
saves() { local q; q=$(qb); q=${q#*"$SAVE"}; q=${q%%[!\ 0-9]*}; echo $q; }
result() { qb | sed 's/.*   //; s/^ *//; s/ *$//'; }
rno_x() { local c; c=$(e2e_find ┼ "$(grid_y)"); echo $((${c%% *} - 2)); }          # 行号个位（或 + / −）所在的列
rowno() { e2e_text 35 "$(rno_x)" "$(row_y "$1")" | tr -d ' '; }                    # N：第 N 个显示的行的行号
rowno_fg() { e2e_style "$(rno_x)" "$(row_y "$1")" | grep -oE 'fg=[^ ]+' | cut -c4-; }
rawpos() { bar | grep -oE ' [0-9+−-]+,[0-9]+ ' | tr -d ' '; }                      # 状态栏的 行,列（新行上是 +,列）
struck() { e2e_cap -e | sed -n "$(row_y "$1")p" | grep -q $'\e\\[9m'; }            # N：这一行画了删除线（SGR 9）
set_cell() { key Enter; e2e_type "$1"; sleep 0.2; key Enter; }
saved() { wait_for 8 eval '[[ $(result) == 已保存* ]]'; sleep 0.3; }
where() { key /; clear_in; e2e_type "$1"; sleep 0.2; key Enter; wait_for 8 settled; sleep 0.2; }
lock_row() { psql "$E2E_DB" -q -c "begin" -c "select 1 from t_order where id = $1 for update" -c "select pg_sleep(60)" -c "commit" >/dev/null 2>&1 & LOCKER=$!
  wait_for 5 eval '[[ $(psql_n "select count(*) from pg_locks l join pg_class c on c.oid = l.relation where c.relname = '"'t_order'"' and l.mode = '"'RowShareLock'"' and l.granted") -ge 1 ]]'; }
unlock_row() { psql_n "select pg_terminate_backend(pid) from pg_stat_activity where datname = current_database() and query like '%pg_sleep(60)%' and pid <> pg_backend_pid()" >/dev/null; wait $LOCKER 2>/dev/null; }

start -C "$D/own"; open_table t_order; wait_for 8 settled

# ---- o：新增一行、填两格、C-s 保存进库（task.md F3.24 验收）
key j; key o
check "o：第 2 行下面插入新行：行号 warn 色的 +，每格 dim 色的 <default>，光标在它的第一列（状态栏 +,1），计数 1" eval '[[ $(rowno 3) == + && $(rowno_fg 3) == $WARN && $(cell user_id 3) == "<defau"* && $(rawpos) == +,1 && $(saves) == 1 ]] && style_has $(col_x user_id) $(row_y 3) fg=$DIM || { echo "  rowno $(rowno 3) pos $(rawpos) saves $(saves)"; false; }'
key j
check "新行下面那一行：行号和状态栏照旧是 3（不多算一）" eval '[[ $(rowno 4) == 3 ]] && pos_is 3,1'
key k; key l; set_cell 3; key 2 l; set_cell 5.5
check "填两格（user_id 3、amount 5.5）：这两格显示修改后的值，其余仍是 <default>，计数仍是 1（新增的行算一处）" eval '[[ $(cell user_id 3) == 3 && $(cell amount 3) == 5.5 && $(cell status 3) == "<defa"* && $(saves) == 1 ]]'
key C-s; saved
check "C-s：「已保存 1 行」；库里多了一行，没填的列取默认值（status pending）" eval '[[ $(result) == "已保存 1 行"* && $(psql_n "select user_id, amount, status from t_order where id > 6000") == "3|5.50|pending" ]]'
check "重新加载：新行不再是 +，第 3 个显示的行回到 id 3" eval '[[ $(rowno 3) == 3 && $(cell id 3) == 3 && -z $(saves) ]]'
where "id > 6000"
check "按条件看得到这一行" eval '[[ $(cell user_id 1) == 3 && $(cell amount 1) == 5.50 ]]'
where ""

# ---- dd 标删除、再 dd 取消；r 也取消；标删除后保存，这一行没了（task.md F3.24 验收）
key g g; key 4 j; key d d
check "dd：第 5 行整行 dim 色加删除线（SGR 9），行号是 error 色的 −，计数 1" eval '[[ $(rowno 5) == − && $(rowno_fg 5) == $ERROR && $(saves) == 1 ]] && struck 5 && style_has $(col_x status) $(row_y 5) fg=$DIM'
key d d
check "再 dd：取消删除，行号、样式恢复，没有计数" eval '[[ $(rowno 5) == 5 && -z $(saves) ]] && ! struck 5'
key d d; key r
check "r 也取消删除" eval '[[ $(rowno 5) == 5 && -z $(saves) ]] && ! struck 5'
e2e_click "$(e2e_find "$DEL" 3 | cut -d' ' -f1)" 3; sleep 0.3
check "点查询条的 −：同 dd" eval '[[ $(rowno 5) == − ]] && struck 5'
key C-s; saved
check "标删除后保存：「已保存 1 行」，id 5 从库里和表格里都没了" eval '[[ $(result) == "已保存 1 行"* && $(psql_n "select count(*) from t_order where id = 5") == 0 && $(cell id 5) == 6 ]]'

# ---- 标了删除的行：不能进编辑，也不提示；上面原有的修改留着，取消删除后恢复（§10.6，reviewer 18）
key 0; key 3 l; set_cell 7.77; key d d
key Enter; key i; t set-buffer -b e2e -- 8.88; t paste-buffer -p -b e2e -t t; sleep 0.3
check "标删除的行上 ↵ / i / 粘贴：不进编辑，不 toast" eval 'mode_is NORMAL && ! screen_has 只读 && [[ -z $(e2e_text 60 159 $(( $(H) - 1 )) | tr -d " ─┘") ]]'
key h; key C-p; e2e_type ">设为 DEFAULT"; sleep 0.3; key Enter; sleep 0.3; key 3 l; key C-p; e2e_type ">设为 NULL"; sleep 0.3; key Enter; sleep 0.3
check "面板里对 status「设为 DEFAULT」、对 meta「设为 NULL」：也不做事，都还是原值" eval '[[ $(cell status 5) == done && $(cell meta 5) == "{\"n\""* ]] && ! palette_open'
key 2 h; key d d
check "取消删除：原来那处修改（7.77）回来了" eval '[[ $(cell amount 5) == 7.77 && $(rowno 5) == 5 ]] && ! struck 5'
key r

# ---- 保存途中：o 照样新增、dd 照样标删除，但新增的行不能编辑；保存完这两样都还在。r / dd 撤销它们时清掉「已保存」（reviewer 17、18）
key g g; key 0; key 3 l; set_cell 5; lock_row 1
key C-s; sleep 0.5
key 2 j; key o; key Enter; sleep 0.3
check "保存进行中 o：新增了一行；在它上面 ↵ 不进编辑" eval '[[ $(bar) == *busy* && $(rowno 4) == + ]] && mode_is NORMAL'
key 3 j; key d d
unlock_row; saved
check "保存完成：「已保存 1 行」，保存途中新增的行、删除标记都还在" eval '[[ $(result) == "已保存 1 行"* && $(rowno 4) == + && $(rowno 7) == − && $(psql_n "select amount from t_order where id = 1") == 5.00 ]] || { echo "  $(result) | $(rowno 4) $(rowno 7)"; false; }'
key 3 k; key r
check "新增行上按 r：去掉这一行，「已保存 …」提示清掉" eval '[[ $(rowno 4) != + && $(result) != 已保存* && $(rowno 6) == − ]]'
key g g; key 0; key 3 l; set_cell 6; lock_row 1; key C-s; sleep 0.5; key 4 j; key d d; unlock_row; saved   # 再来一次（这次把上一轮标的 id 7 也删了）：保存途中标删除 id 6
check "第二次保存：id 7 删掉了，保存途中标的 id 6 还标着" eval '[[ $(psql_n "select count(*) from t_order where id = 7") == 0 && $(rowno 5) == − ]]'
key d d
check "标删除的行上 dd 取消：「已保存 …」提示也清掉" eval '[[ $(result) != 已保存* ]] && ! struck 5'

# ---- LIMIT 改小之后，新行的位置超出本页的行数：挂到最后一页的末尾，不会看不见（reviewer 14）
key gl; e2e_type 500; sleep 0.3; key Enter; wait_for 8 settled
key g g; key 2 9 9 j; key o
check "LIMIT 500 的第 1 页，第 300 行下面新增一行" eval '[[ $(rawpos) == +,1 ]]'
key gl; e2e_type 100; sleep 0.3; key Enter; wait_for 8 settled
key G
check "改回 LIMIT 100：第 1 页的最后一行是第 100 行，新行不在这里" eval 'pos_is 100,1'
key gp; key BSpace; e2e_type 999; key Enter; wait_for 8 settled; key G
check "最后一页的末尾：新行在这里（+），计数 1" eval '[[ $(rawpos) == +,1 && $(saves) == 1 ]] && qb_has "PAGE 60/60"'
key r; key gp; key BSpace; key BSpace; e2e_type 1; key Enter; wait_for 8 settled

# ---- 失败的文字：按新行的显示顺序数，和 INSERT 的顺序一致（reviewer 15）
key g g; key 2 j; key o; key l; set_cell 999; key 2 l; set_cell 1   # A：第 3 行下面，user_id 999（外键不通过）
key g g; key o; key l; set_cell 3; key 2 l; set_cell 1              # B：第 1 行下面；显示的顺序是 1、B、2、3、A
key C-s; wait_for 8 eval '[[ -n $(errbar 1) ]]'
check "B、A 两个新行，只有 A 失败：「[23503] 新增的第 2 行：…，已回滚」（A 是显示的第 2 个新行）" eval '[[ $(errbar 1 | head -1) == "[23503] 新增的第 2 行：insert or update on table"*"，已回滚 ×" ]] || { errbar 1; false; }'
check "整体回滚：B 也没插进去" eval '[[ $(psql_n "select count(*) from t_order where id > 6000") == 1 ]]'
key 3 j; key 2 h; set_cell 3; key 3 k; set_cell 999                # A 改好，B 改坏
key C-s; wait_for 8 eval '[[ $(errbar 1 | head -1) == *"新增的第 1 行"* ]]'
check "只有 B 失败：「新增的第 1 行：…」" eval '[[ $(errbar 1 | head -1) == "[23503] 新增的第 1 行："* ]]'
key 3 j; set_cell 999; key Escape                                    # 两个都坏：先插的那个先报错（先关掉旧的错误栏）
key C-s; wait_for 8 eval '[[ -n $(errbar 1) ]]'
check "两个都失败：报的是「新增的第 1 行」，INSERT 按显示的顺序（先 B 后 A）" eval '[[ $(errbar 1 | head -1) == "[23503] 新增的第 1 行："* ]]'
key r; key g g; key j; key r; key Escape

# ---- DELETE 失败：别的连接先删了；外键挡住
where "id = 8"; psql_n "delete from t_order where id = 8" >/dev/null
key d d; key C-s; wait_for 8 eval '[[ -n $(errbar 1) ]]'
check "别的连接已经删了 id 8：「id = 8 的行不存在，已回滚」（没有 SQLSTATE）" eval 'errbar_is 1 "id = 8 的行不存在，已回滚 ×"'
key R; key y; wait_for 8 settled; where ""
open_table t_user; wait_for 8 settled
key j; key d d; key C-s; wait_for 8 eval '[[ -n $(errbar 1) ]]'
check "删被 t_order 引用的 t_user id 2：「[23503] id = 2：<PG 的 Message>，已回滚」，库里还在" eval '[[ $(errbar 1 | head -1) == "[23503] id = 2：update or delete on table \"t_user\""*"，已回滚 ×" && $(psql_n "select count(*) from t_user where id = 2") == 1 ]] || { errbar 1; false; }'

# ---- 一格都不填：INSERT DEFAULT VALUES；BEFORE 触发器返回 NULL（INSERT 0 0）：「新增的第 1 行没有插入，已回滚」（reviewer 19）
open_table t_def; wait_for 8 settled
e2e_click "$(e2e_find "$ADD" 3 | cut -d' ' -f1)" 3; sleep 0.3
check "点查询条的 +：同 o，在光标所在行下面新增" eval '[[ $(rowno 2) == + ]]'
key C-s; saved
check "一格都不填就保存：插进去一行，全是默认值（id 2、v dv、n NULL）" eval '[[ $(psql_n "select id, v, n is null from t_def order by id desc limit 1") == "2|dv|t" ]]'
open_table t_trig; wait_for 8 settled
key o; key l; set_cell y; key C-s; wait_for 8 eval '[[ -n $(errbar 1) ]]'
check "触发器不让插入：「新增的第 1 行没有插入，已回滚」" eval 'errbar_is 1 "新增的第 1 行没有插入，已回滚 ×" && [[ $(psql_n "select count(*) from t_trig") == 1 ]]'

# ---- 文本主键是空串的行：一样标黄、标失败、画删除（reviewer 11）
open_table t_txt; wait_for 8 settled
check "t_txt 第 1 行的主键是空串" eval '[[ $(e2e_text 35 50 $(row_y 1) | tr -s " ") == " 1 │ │ 1"* && $(e2e_text 35 50 $(row_y 2) | tr -s " ") == " 2 │ a │ 2"* ]]'
key l; set_cell 5
check "改了空串主键那一行：行号 warn 色" eval '[[ $(rowno_fg 1) == $WARN ]]'
key Enter; clear_in; e2e_type 500; key Enter; key C-s; wait_for 8 eval '[[ -n $(errbar 1) ]]'
check "保存失败（check 约束）：「[23514] k = ：…，已回滚」，这一行的行号 error 色" eval '[[ $(errbar 1 | head -1) == "[23514] k = ：new row for relation \"t_txt\""*"，已回滚 ×" && $(rowno_fg 1) == $ERROR ]] || { errbar 1; false; }'
key r; key d d
check "标删除：行号 error 色的 −，删除线" eval '[[ $(rowno 1) == − && $(rowno_fg 1) == $ERROR ]] && struck 1'
key C-s; saved
txt_keys() { psql_n "select string_agg(k, ',' order by k) from t_txt"; }
check "保存：空串主键那一行删掉了" eval '[[ $(txt_keys) == a ]]'

# ---- 没有行标识列的表：新增、删除都不行，toast 说明原因
open_table t_log; wait_for 8 settled
key o
check "t_log 上 o：不新增，toast「…只读」" eval '[[ $(rowno 2) != + ]] && e2e_text 40 160 $(( $(H) - 1 )) | grep -q "t_log 没有主键，也没有全部列都非空的唯一索引，只读"'
sleep 3.2; key d d
check "t_log 上 dd：不标删除，同样 toast" eval '! struck 1 && e2e_text 40 160 $(( $(H) - 1 )) | grep -q "只读"'

e2e_done
