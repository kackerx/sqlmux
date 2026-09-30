#!/usr/bin/env bash
# F4.2 DDL 预览（specs/m4-palette/task.md F4.2；tech-design §12「预览」、§8.3 超时）
# 带预览的面板由 golden 覆盖、高度分配由单测覆盖；这里测真实 PG 上的 DDL、停下来才取、锁表时放弃与取消、缓存与 R、resize 后的高度。
# 自建库：要建表、删表、锁表。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f42-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
. "$(dirname "$0")/palette.sh"
ERROR=#f7768e SQL_TABLE=#2ac3de
psql_n() { psql "$E2E_DB" -At -c "$1"; }
# 自引用外键用的是普通的唯一索引；约束触发器；没有列的表；启动后就删掉的表
psql "$E2E_DB" -q <<'SQL'
create table t_tree (id int, code text, parent text);
create unique index t_tree_code on t_tree (code);
alter table t_tree add constraint t_tree_parent_fk foreign key (parent) references t_tree (code);
create function e2e_trg() returns trigger language plpgsql as $$ begin return null; end $$;
create constraint trigger t_tree_check after insert on t_tree for each row execute function e2e_trg();
create table t_empty ();
create table t_gone (i int);
SQL
shows() { wait_for 3 eval '[[ -n $(preview) ]]'; }                                  # 预览出来了
rowof() { list | cut -d'|' -f1 | grep -n "^$1  " | cut -d: -f1; }                      # NAME：它在列表的第几行
downs() { local k=(); for ((i = 0; i < $1; i++)); do k+=(Down); done; e2e_keys "${k[@]}"; }   # N 个 Down 一次发出去（间隔远小于 150ms）
ddl_waits() { [[ $(e2e_waiting "$APP") == *"quote_ident(a.attname)"* ]]; }            # 取 DDL 的查询在等锁（PG 16 起 pg_get_expr 要拿表锁）

start -C "$D/own"
psql_n "drop table t_gone" >/dev/null

# ---- 选中表停下来，预览出现在列表下方（§12「预览」）；内容对照 PG
pal "@t_sku"
check "@t_sku：停下来后列表下方出现 DDL 预览，正好 12 行" eval 'shows && [[ $(preview | wc -l) -eq 12 && $(preview | head -1) == "create table public.t_sku (" ]] || { preview; false; }'
idx=$(psql_n "select pg_get_indexdef(indexrelid) || ';' from pg_index where indrelid = 't_sku'::regclass order by indexrelid::regclass::text")
check "t_sku 的六个唯一索引各一行，和 pg_get_indexdef 一致（按索引名排，放不下的截短）" eval 'bad=; i=7; while IFS= read -r want; do got=$(preview | sed -n ${i}p); [[ $want == "$got"* && ${#got} -gt 40 ]] || bad+=" [$got]"; ((i++)); done <<<"$idx"; [[ -z $bad && $i == 13 ]] || { echo "  $bad"; false; }'
clear_input; e2e_type "@t_tree"; sleep 0.3
check "自引用外键：它用到的唯一索引 t_tree_code 照样列出；约束触发器不在 create table 里" eval 'shows && preview | grep -qx "  constraint t_tree_parent_fk FOREIGN KEY (parent) REFERENCES t_tree(code)" && preview | grep -qx "CREATE UNIQUE INDEX t_tree_code ON public.t_tree USING btree (code);" && ! preview | grep -qi trigger || { preview; false; }'
clear_input; e2e_type "@t_empty"; sleep 0.3
check "没有列的表：只有一行 create table public.t_empty ();" eval 'shows && [[ $(preview) == "create table public.t_empty ();" ]] || { preview; false; }'
clear_input; e2e_type "@t_order_item"; sleep 0.3; shows
y=$(( $(read s1 s2 <<<"$(seps)"; echo $s2) + $(preview | grep -n REFERENCES | cut -d: -f1) )); x=$(e2e_find "t_order(id)" "$y" | cut -d' ' -f1)
check "REFERENCES t_order(id) 里的 t_order 用表名色，不是函数色" style_has "$x" "$y" fg=$SQL_TABLE
clear_input; e2e_type "@v_paid_order"; sleep 0.3
want=$(printf 'create view public.v_paid_order as\n%s' "$(psql_n "select pg_get_viewdef('v_paid_order'::regclass, true)")")
check "视图：create view public.v_paid_order as 接 pg_get_viewdef(oid, true)" eval 'shows && [[ $(preview) == "$want" ]] || { preview; false; }'
clear_input; e2e_type "@mv_order"; sleep 0.3
check "物化视图：create materialized view public.mv_order_by_status as" eval 'shows && [[ $(preview | head -1) == "create materialized view public.mv_order_by_status as" ]]'
clear_input; e2e_type "t_sku"; sleep 0.3
check "「所有」范围选中表也有预览" shows
clear_input; e2e_type "%"; sleep 0.8
check "窗口·Pane 范围没有预览" eval '[[ -z $(preview) ]]'

# ---- 出错：error 色的原文，缓存到 R；R 同时清掉 DDL 缓存（§12、F4.2）
clear_input; e2e_type "@t_gone"; sleep 0.3
check "启动后被删掉的 t_gone：预览是 error 色的数据库原文" eval 'shows && [[ $(preview) == "ERROR: relation \"public.t_gone\" does not exist (SQLSTATE 42P01)" ]] && style_has $(( $(left) + 2 )) $(( $(read s1 s2 <<<"$(seps)"; echo $s2) + 1 )) fg=$ERROR || { preview; false; }'
e2e_keys Escape; sleep 0.3
psql "$E2E_DB" -q -c "create table t_gone (j int)" -c "alter table t_tree add column note text"
pal "@t_gone"; sleep 0.8
check "表又建回来了：还是缓存着的错误（数据库报的错缓存到 R）" eval '[[ $(preview) == ERROR:* ]]'
clear_input; e2e_type "@t_tree"; sleep 0.8
check "t_tree 加了列：预览还是缓存着的旧 DDL" eval '! preview | grep -q "note text"'
e2e_keys Escape; sleep 0.3; key C-h; key R; sleep 1
pal "@t_gone"
check "树上 R 之后：t_gone 的预览是新的 create table" eval 'shows && [[ $(preview | tr "\n" /) == "create table public.t_gone (/  j integer/);/" ]] || { preview; false; }'
clear_input; e2e_type "@t_tree"; sleep 0.3
check "t_tree 的预览有了新加的 note 列" eval 'shows && preview | grep -qx "  note text,"'

# ---- 停下来才取，锁着的表 3 秒后放弃，关掉面板就取消（§12、§8.3）
e2e_keys Escape; sleep 0.3
e2e_lock t_order
pal "@t_"; sleep 0.8
o=$(rowof t_order) s=$(( $(rowof t_order) + 1 ))
check "@t_ 列出 t_order，它后面还有别的表" eval '[[ -n $o ]] && (( s <= $(nrows) ))'
downs $((s - 1)); sleep 1
check "一口气移过锁着的 t_order、停在后面那张：t_order 的 DDL 没去取（没有在等锁的查询），停下的那张出了预览" eval '[[ -z $(e2e_waiting "$APP") ]] && [[ $(selected) != t_order\ * ]] && [[ $(preview | head -1) == *".$(selected | cut -d" " -f1)"* ]] || { echo "  waiting: $(e2e_waiting "$APP")"; false; }'
e2e_keys Up; sleep 0.8
check "停在 t_order 上：去取了，取 DDL 的查询在等锁；预览区还没出现" eval 'ddl_waits && [[ -z $(preview) ]]'
check "3 秒后放弃：不再等锁，预览区照旧不出现，面板照常能用" eval 'wait_for 4 eval "! ddl_waits" && sleep 0.3 && [[ -z $(preview) ]] && is_open'
e2e_keys Down; sleep 0.5; e2e_keys Up; sleep 0.8
check "超时的不缓存：移开再停回来，又去取" ddl_waits
e2e_keys Escape
check "取的时候关掉面板：请求取消，服务端不再等锁" wait_for 2 eval '! ddl_waits'
pal "@t_order"; sleep 0.8; ddl_waits; e2e_keys Enter; sleep 0.8
check "停一下再 ↵ 打开 t_order：DDL 请求取消，等锁的是打开它的取数（只有一条，不是取 DDL 的）" eval 'w=$(psql_n "select count(*) from pg_stat_activity where application_name = '"'$APP'"' and wait_event_type = '"'Lock'"'"); [[ $w == 1 ]] && ! ddl_waits || { echo "  $w waiting: $(e2e_waiting "$APP")"; false; }'
key C-c
check "C-c 取消的是取数：不再有等锁的查询" wait_for 3 eval '[[ -z $(e2e_waiting "$APP") ]]'
e2e_unlock

# ---- 鼠标悬停换了选中项也算移动
pal "@t_"; sleep 0.8; n=$(rowof t_user)
e2e_move $(( $(left) + 10 )) "$(row_y "$n")"
check "鼠标移到 t_user 那一行：选中它，停下后预览换成 t_user 的" eval 'wait_for 3 eval "[[ \$(preview | head -1) == \"create table public.t_user (\" ]]" || { preview | head -1; false; }'
e2e_keys Escape; sleep 0.3

# ---- resize：列表和预览各最多 12 行，先压预览到 3 行，再压列表到 3 行，还放不下就不显示；DDL 只有一两行的照原样（2db2d60、295de25）
e2e_resize 80 24; sleep 0.5
pal "@"; sleep 1
check "80×24、候选多：列表 9 行 + 预览 3 行" eval 'shows && [[ $(nrows) == 9 && $(preview | wc -l) -eq 3 ]] || { echo "  $(nrows) + $(preview | wc -l)"; false; }'
e2e_resize 80 18; sleep 0.5
check "80×18：预览还是 3 行，列表让到 4 行" eval '[[ $(nrows) == 4 && $(preview | wc -l) -eq 3 ]] || { echo "  $(nrows) + $(preview | wc -l)"; false; }'
e2e_resize 80 16; sleep 0.5
check "80×16：列表 3 行 + 预览 3 行也放不下，不显示预览，列表拿回空间（至少 3 行），底栏还在" eval '[[ -z $(preview) ]] && (( $(nrows) >= 3 )) && [[ $(footer) == *"↵ 打开 " ]] || { echo "  $(nrows) + $(preview | wc -l)"; false; }'
clear_input; e2e_type "@t_empty"; sleep 0.8
check "80×16 选中 t_empty：一行的 DDL 照它自己的高度显示" eval '[[ $(preview) == "create table public.t_empty ();" ]] || { preview; false; }'

e2e_done
