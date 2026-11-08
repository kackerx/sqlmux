#!/usr/bin/env bash
# F14.1 SQLite 与命令行 DSN（specs/m14-sqlite/task.md F14.1）
# sqlmux shop.db：一个临时连接，树是 文件 → Tables / Views → 列（没有 schema 层），浏览、翻页、排序、WHERE、编辑（有主键 / 无主键用 rowid / WITHOUT ROWID）
# 存回文件，只读文件上保存报错；sqlmux postgres://… / mysql://… / DATABASE_URL 同样是临时连接；退出后 connections.toml 不变、没有存布局、钥匙串没动。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/ordering.sh"
. "$(dirname "$0")/paging.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
DB=$D/shop.db
sqlite3 "$DB" <"$E2E_ROOT/testdata/seed/sqlite.sql"
lite() { sqlite3 "$DB" "$1"; }
lstart() { e2e_start "$@"; wait_for 5 flag_is alternate_on 1 || echo "e2e: sqlmux did not start: $(e2e_plain | grep -m1 'sqlmux:')"; sleep 1; [[ -n $NOSOLO ]] || solo; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
cell() { local x; x=$(col_x "$1"); e2e_text "$x" $((x + ${3:-10})) "$(row_y "$2")" | sed 's/ *│.*//; s/ *$//; s/^ *//'; }   # NAME N [W]
edit() { key Enter; e2e_type "$1"; sleep 0.3; }

quit_app() { e2e_type ':qa'; e2e_keys Enter; wait_for 3 screen_has '[e2e-exit 0]'; }
conns_file() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }   # the running start's (e2e_start makes a new E2E_TMP each time)
untouched() { grep -q doraemon "$(conns_file)" && ! grep -qE "shop|sqlmux_e2e|ok\.db" "$(conns_file)" && [[ -z $(find "$E2E_TMP/state" "$E2E_TMP/data" "$E2E_TMP/config" -name "last.toml*" 2>/dev/null) && -z $(ls "$E2E_TMP/keychain") ]]; }

# ---- 1. sqlmux shop.db：一个临时连接，树是 文件 → Tables / Views，没有 schema 层，没有行数
lstart "$E2E_BIN $DB"
check "（准备）树里只有这一个连接 shop（没有配置里的 doraemon）" eval 'e2e_plain | grep -qE "▾ .*shop" && ! screen_has "doraemon"'
check "树：shop → Tables (N)、Views (1)，没有 schema 那一层（没有 main / public）" eval 'e2e_plain | grep -qE "Tables \([0-9]+\)" && screen_has "Views (1)" && ! e2e_plain | grep -qE "[▾▸] .*(main|public)"'
check "表后面没有行数（SQLite 没有估计值）：t_order 那一行的树部分没有数字" eval '[[ $(e2e_plain | python3 -c "
import sys
for l in sys.stdin:
    t = l[1:31]
    if \"t_order \" in t + \" \" and \"▸\" in t and \"t_order_item\" not in t:
        print(t.strip())
        break") =~ ^[^0-9]*t_order$ ]]' || e2e_plain | sed -n 4,12p | cut -c1-40
check "状态栏的地址块是这个文件的路径" eval '[[ $(bar) == *"shop.db"* ]]'
open_table t_order; wait_for 8 settled
check "t_order：6000 行，PAGE 1/60，列都出来了（id … raw、created_at），blob 画成 <binary 6 bytes>，REAL 是 1.5" eval '[[ $(cnt) == 6000 ]] && qb | grep -qF "PAGE 1/60" && e2e_plain | grep -qF "<binary 6 bytes>" && cell amount 1 | grep -qx 1.5'
key "]"; sleep 0.8; wait_for 5 settled
check "翻页：] 到第 2 页，第一行是 id 101" eval 'qb | grep -qF "PAGE 2/60" && [[ $(ids 2) == "101,102," ]]'
key "["; sleep 0.8; wait_for 5 settled
order_by amount; sleep 0.8; order_by amount; sleep 0.8; wait_for 5 settled
check "按 amount 排序两次（翻成降序）：第一行是 id 6000、5999" eval '[[ $(ids 2) == "6000,5999," ]]' || ids 3
key /; e2e_type "status = 'done'"; key Enter; wait_for 5 settled; sleep 0.5
check "WHERE status = 'done'：1200 行" eval '[[ $(cnt) == 1200 ]]'

# ---- 2. 编辑存回文件：有主键 / 组合主键 / rowid（多一列只读 rowid）/ WITHOUT ROWID；删除；视图只读
save() { key C-s; sleep 1.2; wait_for 5 settled; }
open_table t_user; wait_for 8 settled
key l; edit zed; key Enter; sleep 0.3; save
check "有主键（t_user，id 是 rowid 的别名）：改 name 存回文件，只改了第 1 行" eval '[[ $(lite "select group_concat(name) from t_user order by id") == "zed,bob,carol" ]]' || lite "select * from t_user"
key j j; key d d; sleep 0.3; save
check "dd 标删第 3 行、C-s：库里 2 行，carol 没了" eval '[[ $(lite "select count(*) from t_user") == 2 && $(lite "select count(*) from t_user where name = '"'"'carol'"'"'") == 0 ]]'
open_table t_log; wait_for 8 settled
check "没有主键的表（t_log）：最前面多一列 rowid，值是 1 2 3" eval 'e2e_plain | grep -qE "rowid +│ at" && [[ $(cell rowid 3) == 3 ]]'
key Enter; sleep 0.4
check "rowid 那一列只读：↵ 进不了编辑（还是 NORMAL）" eval 'mode_is NORMAL'
key Escape; key j j; key l l; edit STOPPED2; key Enter; sleep 0.3; save
check "按 rowid 改第 3 行：1、2 行（内容相同的 started）没动，只有第 3 行变了" eval '[[ $(lite "select group_concat(line) from (select line from t_log order by rowid)") == "started,started,STOPPED2" ]]' || lite "select rowid, * from t_log"
open_table t_kv; wait_for 8 settled
key l; edit A1; key Enter; sleep 0.3; save
check "WITHOUT ROWID（t_kv，主键 k）：改 a 的 v 存回，b 没动" eval '[[ $(lite "select group_concat(k || '"'"'='"'"' || coalesce(v, '"'"'null'"'"'), '"'"','"'"') from t_kv order by k") == "a=A1,b=null" ]]' || lite "select * from t_kv"
open_table t_order_item; wait_for 8 settled
key j; key l l l; edit 9; key Enter; sleep 0.3; save
check "组合主键（t_order_item）：改第 2 行 line_no 之后的 qty（1 → 9），只动了 (1,2)" eval '[[ $(lite "select group_concat(qty) from (select qty from t_order_item order by order_id, line_no)") == "2,9,5" ]]' || lite "select * from t_order_item"
open_table v_order_total; wait_for 8 settled
key l; key Enter; sleep 0.4
check "视图（v_order_total）：单元格进不了编辑（只读）" eval 'mode_is NORMAL'
_e2e_kill

# ---- 3. query：结果、格式化（sqlite 方言）、补全、取消长查询
qhas() { e2e_plain | python3 -c 'import sys; print(*[l.rstrip("\n")[62:] for l in sys.stdin], sep="\n")' | grep -qF -- "$1"; }
NOSOLO=1 lstart "$E2E_BIN $DB"; key C-l
check "query 的连接是 shop，schema 是 main" eval 'qhas "shop · main"'
setsql "select count(*) as n, sum(amount) as total from t_order"; key Enter; sleep 1.5
check "query 里 select count(*)、sum(amount)：6000 行的合计 27004500.0（REAL 按驱动的文本）" eval 'e2e_plain | grep -qE "│ 6000 +│ 27004500.0"'
setsql "select id,name from t_user where id=1 and name='a' order by id"; key g q q; sleep 1.5
check "gqq 用 sqlite 方言格式化：select / from / where / order by 各占一行（看 query 文件）" eval '[[ $(cat "$E2E_TMP/data/sqlmux/queries/query_1.sql" | tr "\n" "|") == "select|  id,|  name|from|  t_user|where|  id = 1|  and name = '"'"'a'"'"'|order by|  id|" ]]' || cat "$E2E_TMP/data/sqlmux/queries/query_1.sql"
key g g; key d G; key i; e2e_type "select * from t_us"; sleep 1.2
check "补全：t_us 补出表 t_user（列表里有 t_user · 表）" eval 'qhas "t_user"' 
key Escape; key Escape; key g g; key d G
setsql "with recursive n(i) as (select 1 union all select i+1 from n) select count(*) from n"; key Enter; sleep 2
check "（准备）一条停不下来的递归查询在跑（状态栏 busy）" eval '[[ $(bar) == *busy* ]]'
key C-c; wait_for 8 eval '[[ $(bar) != *busy* ]]'
check "C-c：取消（日志里「已取消」），状态栏不 busy" eval 'e2e_plain | grep -qF "已取消" && [[ $(bar) != *busy* ]]'
setsql "select 5 as five"; key Enter; sleep 1.5
check "取消之后接着跑 select 5：结果出来（连接还能用）" eval 'e2e_plain | grep -qE "│ 5 +│" || e2e_plain | grep -qE "^ *1 │ 5"'
_e2e_kill

# ---- 4. 只读文件、路径不存在、不认识的 URL
cp "$DB" "$D/ro.db"
lstart "$E2E_BIN '$D/ro.db?mode=ro'"
open_table t_user; wait_for 8 settled
key l; edit RO; key Enter; sleep 0.3; save
check "?mode=ro：改一格、C-s：错误栏报 readonly，库里没变" eval 'e2e_plain | grep -qi "readonly" && [[ $(sqlite3 "$D/ro.db" "select name from t_user where id = 1") == zed ]]' || e2e_plain | sed -n 1,30p | cut -c30-170
_e2e_kill
chmod 444 "$D/ro.db"
lstart "$E2E_BIN $D/ro.db"
open_table t_user; wait_for 8 settled
key l; edit RO2; key Enter; sleep 0.3; save
e2e_skip_if_root "文件权限只读（chmod 444）：同样报 readonly" || check "文件权限只读（chmod 444）：同样报 readonly，库里没变" eval 'e2e_plain | grep -qi "readonly" && [[ $(sqlite3 "$D/ro.db" "select name from t_user where id = 1") == zed ]]'
_e2e_kill

e2e_start "$E2E_BIN $D/nope.db"; wait_for 5 flag_is alternate_on 1; sleep 1; key C-h; key Enter; sleep 2
check "路径不存在：连接报错「no such file or directory」（带路径），不新建空文件" eval 'screen_has "nope.db" && screen_has "no such file or directory" && [[ ! -e $D/nope.db ]]' || e2e_plain | tail -4
_e2e_kill
e2e_start "$E2E_BIN '$D/made.db?mode=rwc'"; wait_for 5 flag_is alternate_on 1; sleep 1; key C-h; key Enter; sleep 2
check "要新建写成 x.db?mode=rwc：文件建出来了，连上了（空库，Tables 下没有表）" eval '[[ -e $D/made.db ]] && ! screen_has "unable to open"' || e2e_plain | tail -4
_e2e_kill
e2e_start "$E2E_BIN foo://bar"; wait_for 5 screen_has '[e2e-exit'; sleep 0.3
check "不认识的 URL（foo://）：报错退出，说只有 postgres://、mysql:// 和 SQLite 的文件" eval 'screen_has "[e2e-exit 1]" && screen_has "foo://"' || e2e_plain | head -6
_e2e_kill

# ---- 5. 临时连接什么都不留：connections.toml 不变、没有 last.toml、钥匙串里没东西；（对照：普通启动退出会存 last.toml）
lstart "$E2E_BIN $DB"; open_table t_user; wait_for 8 settled; quit_app
check "sqlmux shop.db 退出后：connections.toml 里没有它、没有 last.toml、钥匙串（假的）里没东西" eval 'untouched' || { cat "$(conns_file)"; find "$E2E_TMP" -name "last.toml*"; ls "$E2E_TMP/keychain"; }
_e2e_kill
start; quit_app
check "（对照）普通启动退出：存了 last.toml（上面那条不是找错了地方）" eval '[[ -n $(find "$E2E_TMP" -name "last.toml" 2>/dev/null) ]]'
_e2e_kill

# ---- 6. sqlmux postgres://… / mysql://…：库名@主机的临时连接，能改能存，密码不进钥匙串
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
e2e_own_mydb || { echo "e2e: could not create the private MySQL database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table t_u (id int primary key, v text)" -c "insert into t_u values (1,'one'),(2,'two')" >/dev/null
e2e_mysql "sqlmux_e2e_$$" <<<"create table t_u (id int primary key, v varchar(20)); insert into t_u values (1,'one'),(2,'two')"
lstart "$E2E_BIN '$E2E_DB'"
check "postgres://：树里是 <库>@localhost 一个连接" eval 'e2e_plain | grep -qE "▾ .*sqlmux_e2e_$$@localhost"'
open_table t_u; wait_for 8 settled
key l; edit PGX; key Enter; sleep 0.3; save
check "postgres://：改一格存回 PG（id 1 的 v 是 PGX）" eval '[[ $(psql "$E2E_DB" -At -c "select v from t_u where id = 1") == PGX ]]'
quit_app
check "postgres:// 退出后：connections.toml 没有它、没有 last.toml、密码没进钥匙串" eval 'untouched'
_e2e_kill
lstart "$E2E_BIN $E2E_MYDB"
check "mysql://：树里是 <库>@localhost" eval 'e2e_plain | grep -qE "▾ .*sqlmux_e2e_$$@localhost"'
open_table t_u; wait_for 8 settled
key l; edit MYX; key Enter; sleep 0.3; save
check "mysql://：改一格存回 MySQL（id 1 的 v 是 MYX）" eval '[[ $(e2e_mysql "sqlmux_e2e_$$" <<<"select v from t_u where id = 1") == MYX ]]'
quit_app
check "mysql:// 退出后：同样什么都没留" eval 'untouched'
_e2e_kill

# ---- 7. DATABASE_URL：只在没有配置的连接、又没给参数时才认；参数优先；已有连接名（带点）不当文件
NAME_PG=sqlmux_e2e_$$@localhost
E2E_NOCONN=1 e2e_start "DATABASE_URL='$E2E_DB' $E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 1.5
check "没有 connections.toml、DATABASE_URL 给了 postgres://：开它（树里是 $NAME_PG）" eval 'e2e_plain | grep -qF "$NAME_PG"'
_e2e_kill
e2e_start "DATABASE_URL='$E2E_DB' $E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 1.5
check "配置里有连接（doraemon）：DATABASE_URL 不劫持，还是 doraemon，树里没有 $NAME_PG" eval 'e2e_plain | grep -qE "▾ .*doraemon" && ! screen_has "$NAME_PG"'
_e2e_kill
E2E_NOCONN=1 e2e_start "DATABASE_URL='postgres://nobody@127.0.0.1:1/none' $E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1.5
check "DATABASE_URL 和位置参数同时给：以参数为准（开的是 shop.db，不是 none@127.0.0.1）" eval 'e2e_plain | grep -qE "▾ .*shop" && ! screen_has "none@"'
_e2e_kill

# ---- 8. connections.toml 里 engine = "sqlite"：路径 ~ 展开、名字带点的连接名不当文件、表单新建
mkdir -p "$D/home" "$D/c"
cp "$DB" "$D/home/h.db"
printf '[[connection]]\nname = "lite.home"\nengine = "sqlite"\ndsn = "~/h.db"\n' >"$D/c/connections.toml"; chmod 600 "$D/c/connections.toml"
e2e_start -C "$D/c" "HOME=$D/home $E2E_BIN lite.home"; wait_for 5 flag_is alternate_on 1; sleep 1.5; solo
open_table t_user; wait_for 8 settled
check "sqlmux lite.home：「lite.home」是配置里的连接名，不当文件；dsn 的 ~ 展开成 HOME，打开 h.db 的 t_user（有 zed）" eval 'e2e_plain | grep -qE "▾ .*lite.home" && e2e_plain | grep -qF zed'
_e2e_kill
start; pal ">新建连接"; key Enter; sleep 0.6
e2e_type litef; key Tab; key Tab; key Right; key Right; key Tab; e2e_type "$DB"; sleep 0.3
check "表单选了 sqlite：只剩 name / group / engine / file / read_only（没有 host、port、user、password、database）" eval 'e2e_plain | grep -qE " file " && ! e2e_plain | grep -qE " (host|port|user|password|database) "'
key C-t; sleep 2
check "测试：已连接 · SQLite 版本（sqlite_version()）" eval 'e2e_plain | grep -qE "已连接 · SQLite [0-9]"' || e2e_plain | grep -a '连接\|SQLite' | head -5
key Enter; sleep 1
check "保存：connections.toml 里写了 name = litef、engine = sqlite、dsn = 文件路径" eval 'grep -q "name = \"litef\"" "$(conns_file)" && grep -q "engine = \"sqlite\"" "$(conns_file)" && grep -qF "$DB" "$(conns_file)"' || cat "$(conns_file)"

# ---- 9. 临时连接和用户自己的东西：同名保留扩展名、用新编号的 query（不动用户的 query_1）、不进连接管理器
mkdir -p "$D/c9" "$D/data9/sqlmux/queries"
printf '[[connection]]\nname = "shop"\nengine = "postgres"\ndsn = "%s"\n' "$SQLMUX_TEST_PG" >"$D/c9/connections.toml"; chmod 600 "$D/c9/connections.toml"
echo "select 'mine'" >"$D/data9/sqlmux/queries/query_1.sql"
e2e_start -C "$D/c9" -D "$D/data9" "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1.5
check "配置里已有连接 shop：临时连接的名字保留扩展名，叫 shop.db（不是 shop）" eval 'e2e_plain | grep -qE "▾ .*shop\.db" && ! e2e_plain | grep -qE "[▾▸] +shop +│"'
key C-l; key i; e2e_type "select 42"; key Escape; sleep 1.5
check "用户自己的 query_1.sql 没被动（还是 select 'mine'），临时连接的 query 是新编号的 query_2" eval '[[ $(cat "$D/data9/sqlmux/queries/query_1.sql") == "select '"'"'mine'"'"'" && $(cat "$D/data9/sqlmux/queries/query_2.sql") == "select 42" ]]' || ls "$D/data9/sqlmux/queries"
pal ">管理连接"; sleep 0.4; key Enter; sleep 0.6
check "连接管理器里没有临时连接（shop.db / shop 都不在，只有「新建」）" eval 'e2e_plain | grep -qF "新建" && ! e2e_plain | grep -E "连接 ─" -A6 | grep -qE "shop"'
e2e_done
