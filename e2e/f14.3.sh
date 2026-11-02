#!/usr/bin/env bash
# F14.3 树上的对象（PG）（specs/m14-sqlite/task.md F14.3；tech-design §7.8「对象节点」、§8.4）
# 表下面默认收起的 Indexes / Keys，schema 下面 Functions / Sequences；索引 ↵ 开 create index 的 DDL 浮窗，外键 ↵ 跳到目标表（目标在另一个 schema 就先展开它），
# 函数 ↵ 在新 query 里放定义，PRIMARY 和序列 ↵ 无事；树过滤时这些节点不显示；R 刷新。自建库：短名字，侧栏只有 30 列。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q <<'SQL' >/dev/null
create table cust (id int primary key, nm text);
create table ord (id int primary key, cid int, code text, constraint f1 foreign key (cid) references cust (id));
create unique index ux_code on ord (code);
create index ix_cid on ord (cid, code);
create schema other;
create table other.ref (id int primary key, v text);
create table ord2 (id int primary key, rid int, constraint f2 foreign key (rid) references other.ref (id));
create function fnx(a int) returns int language sql as $$ select a + 1 $$;
create function fnx(a int, b text) returns text language sql as $$ select b || a $$;
create sequence sq1;
create view vw as select id from cust;
create materialized view mv as select id, nm from cust;
create unique index mv_i on mv (id);
SQL
mkdir -p "$D/own"; printf '[tree]\nwidth = 70\nfollow = true\n' >"$D/own/config.toml"; mkdir -p "$D/own2"; printf '[tree]\nwidth = 70\n' >"$D/own2/config.toml"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"; cp -p "$D/own/connections.toml" "$D/own2/connections.toml"
tree() { e2e_plain | python3 -c "import sys; [print(l[:72].rstrip()) for l in list(sys.stdin)[:${1:-40}]]"; }
trow() { e2e_plain | python3 -c "
import sys
for i,l in enumerate(sys.stdin, 1):
    if sys.argv[1] in l[:72]: print(i); break" "$1"; }
tclick() { local y x; y=$(trow "$1"); [[ -n $y ]] || { echo "  no tree row '$1'"; return 1; }; x=$(e2e_plain | sed -n "${y}p" | python3 -c "import sys; l=sys.stdin.read(); print(max(l.find('▸'), l.find('▾')) + 1)"); e2e_click "$x" "$y"; sleep 0.5; }   # the arrow of the row that has TEXT
tleaf() { local y; y=$(trow "$1"); e2e_click 12 "$y"; sleep 0.6; }                                                                                    # the row's text: a leaf's ↵ is the click
thas() { tree 60 | grep -qF -- "$1"; }

title1() { e2e_plain | sed -n 1p | python3 -c "import sys; l = sys.stdin.read(); i = l.find('┌─ ①'); print(l[i:i + 40].strip() if i >= 0 else '')"; }
start -C "$D/own"; solo
check "schema 下面 Tables / Views 之后是 Functions (n)、Sequences (n)（三个引擎里 PG 两个都有）" eval 'thas "Functions (" && thas "Sequences ("'
tclick Functions; tclick Sequences
check "Functions：函数名加参数签名（fnx(a integer)、fnx(a integer, b text)，重载各一条）" eval 'thas "fnx(a integer)" && thas "fnx(a integer, b text)" && thas "fn_order_total(order_id bigint)"'
check "Sequences：invoice_no、sq1（叶子）" eval 'thas "invoice_no" && thas "sq1"'
tclick "ord  "; sleep 0.3
check "表展开：先列、再两个分组 Indexes (3)、Keys (2)，默认收起（只有分组行）" eval 'thas "Indexes (3)" && thas "Keys (2)" && ! thas "ux_code"'
tclick Indexes; tclick Keys
check "Indexes：ix_cid (cid, code)、ord_pkey (id) UNIQUE、ux_code (code) UNIQUE" eval 'thas "ix_cid (cid, code)" && thas "ord_pkey (id) UNIQUE" && thas "ux_code (code) UNIQUE"'
check "Keys：主键 ord_pkey (id)、外键 f1 (cid) → public.cust (id)" eval 'thas "ord_pkey (id)" && thas "f1 (cid) → public.cust (id)"'
tleaf "ux_code"; sleep 0.8
check "索引 ↵：DDL 浮窗是那条 create index（CREATE UNIQUE INDEX ux_code ON public.ord USING btree (code)）" eval 'screen_has "CREATE UNIQUE INDEX ux_code ON public.ord USING btree (code)"'
key Escape; sleep 0.3
check "（准备）浮窗 esc 关了" eval '! screen_has "CREATE UNIQUE INDEX"'
tleaf "f1 ("; sleep 1; wait_for 5 settled
check "外键 ↵：打开目标表 cust（①的标题是 cust）" eval '[[ $(title1) == *" cust "* ]]' || title1
tclick "ord  "; sleep 0.3; tclick ord2; sleep 0.3; tclick Indexes; tclick Keys
check "（准备）ord2 的 Keys 里有 f2 (rid) → other.ref (id)" eval 'thas "f2 (rid) → other.ref (id)"'
tleaf "f2 ("; sleep 1.2; wait_for 5 settled
check "外键指到另一个 schema（other）：展开它、打开 ref（标题 ref，树里 other 展开了、有 ref）" eval '[[ $(title1) == *" ref "* ]] && e2e_plain | grep -qE "▾ .*other" && thas "ref"' || { title1; tree 30; }
before=$(e2e_plain)
tleaf "sq1"; sleep 0.6
check "Sequence ↵：无事（屏幕不变）" eval '[[ $(e2e_plain) == "$before" ]]'
tleaf "ord2_pkey (id)"; sleep 0.6
check "PRIMARY / 主键那条 ↵：无事（没有开新 tab，标题还是 ref）" eval '[[ $(title1) == *" ref "* ]]'
tclick ord2; sleep 0.3; tleaf "fnx(a integer)"; sleep 1.2; wait_for 5 settled
check "函数 ↵：新 query 里放定义（CREATE OR REPLACE FUNCTION public.fnx(a integer) …）" eval 'screen_has "CREATE OR REPLACE FUNCTION public.fnx(a integer)"' || e2e_plain | sed -n 1,8p | cut -c1-160
check "函数的 query 在它所在的 schema（public）：查询条是 doraemon · …public" eval 'e2e_plain | grep -qE "doraemon · .*public"' || true
key C-h; sleep 0.3; key /; e2e_type "ord"; sleep 0.6
check "树过滤 ord：只有表和视图参与，Functions / Sequences 分组不显示，ord 的表还在" eval '! thas "Functions (" && ! thas "Sequences (" && thas "ord2"'
key Escape; sleep 0.5
check "清掉过滤：Functions / Sequences 回来" eval 'thas "Functions (" && thas "Sequences ("'
psql "$E2E_DB" -q -c "create index ix_new on ord (code)" >/dev/null
tclick "ord  "; sleep 0.4
check "（准备）重新展开 ord：Indexes (3) 还是旧的（惰性取过了，库里已经有第 4 个）" eval 'thas "Indexes (3)"'
key R; sleep 1.5; wait_for 5 settled; sleep 0.3
check "R 刷新：库里新建的 ix_new 出现在 Indexes 里（(4)）" eval 'thas "Indexes (4)" && thas "ix_new (code)"' || tree 30

# ---- 普通视图不画索引和键分组，物化视图只有 Indexes
kids() { e2e_plain | python3 -c "
import sys
rows = [l[1:72].rstrip() for l in sys.stdin]
for i, l in enumerate(rows):
    if sys.argv[1] in l and ('▾' in l or '▸' in l):
        ind = len(l) - len(l.lstrip(' '))
        for m in rows[i + 1:]:
            if not m.strip() or len(m) - len(m.lstrip(' ')) <= ind: break
            print(m.strip())
        break" "$1"; }
tclick "ord2"; tclick "ord  "
tclick "Views (4)"; sleep 0.4; tclick "vw  "; sleep 0.4; tclick " mv  "; sleep 0.4
check "普通视图 vw：展开只有列，没有 Indexes / Keys 分组" eval '[[ -n $(kids "vw ") ]] && ! kids "vw " | grep -qE "Indexes|Keys"' || { kids "vw "; tree 40; }
check "物化视图 mv：列之后只有 Indexes (1)（没有 Keys）" eval 'kids "mv " | grep -qF "Indexes (1)" && ! kids "mv " | grep -qF "Keys"' || { kids "mv "; tree 44; }

# ---- 默认（没写 follow）：外键 ↵ 照样打开目标表，树不展开 other（F20.4：openRef 也跟 [tree] follow）
start -C "$D/own2"; solo
tclick "ord2"; sleep 0.3; tclick Keys; tleaf "f2 ("; sleep 1.2; wait_for 5 settled
check "默认：外键 ↵ 指到另一个 schema：打开了 ref（标题 ref），树上 other 还是折着的、没有 ref 节点" eval '[[ $(title1) == *" ref "* ]] && ! e2e_plain | grep -qE "▾ .*other" && ! tree 60 | grep -qE "\bref\b.* [0-9]" ' || { title1; tree 30; }
e2e_done
