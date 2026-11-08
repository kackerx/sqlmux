#!/usr/bin/env bash
# F8.21 query 文件：编号递增、树里的 Queries 节点、↵ 打开、dd 删除（specs/m8-feedback/task.md F8.21；tech-design §11、§7.8「Queries 节点」）
# 关掉的 query 文件留着、树里 Queries 下还有它，↵ 重新打开内容还在；新建的编号跳过有文件的和开着的；dd 删文件（确认）之后再新建得到空的
# 同号 query；开着的 dd 也能删（F10.10，关掉它的 tab）；别的节点上 dd 无事；改名后树跟着变。节点的画法由 golden 覆盖。只读，用共用库（query 只写自己的文件）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

CD=$E2E_TMP; D=
files() { ls "$E2E_TMP/data/sqlmux/queries" 2>/dev/null | sed 's/\.sql$//' | tr '\n' ' '; }
cat_f() { cat "$E2E_TMP/data/sqlmux/queries/$1.sql" 2>/dev/null; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
# 侧栏：「工作区」下面的 query 名（窗口里开着的，排序后）和「Queries」下面的 名字:连接名，各一行
sections() { local y; for y in $(seq 3 43); do e2e_text 2 31 "$y"; done | python3 -c '
import re, sys
rows = [re.sub(r"^[⓪①②③④⑤⑥⑦⑧⑨]\s+", "", re.sub(r"^[\s▾▸-]+|\s+$", "", l)) for l in re.sub(r"[\ue000-\uf8ff]", "", sys.stdin.read()).split("\n")]
def part(a, b):
    if a not in rows: return []
    r = rows[rows.index(a) + 1:]
    r = r[:next((i for i, x in enumerate(r) if x.startswith("──") or (b and x == b)), len(r))]
    return [x for x in r if x]
ws = [x for x in part("工作区", "Queries") if x.startswith("query_") or not x.startswith(("data", "pane"))]
co = [re.sub(r"\s+", ":", x) for x in part("Queries", None)]
print("WS=" + " ".join(sorted(x for x in ws if not x.startswith("data") and not x.startswith("pane"))) + " | CO=" + " ".join(co))'; }
ws_is() { [[ $(sections) == "WS=$1 | CO="* ]] || { echo "  sections: [$(sections)], want WS=$1"; false; }; }
co_is() { [[ $(sections) == *"| CO=$1" ]] || { echo "  sections: [$(sections)], want CO=$1"; false; }; }
tsel() { local y; for y in $(seq 3 43); do [[ $(e2e_style 4 "$y") == *bg=#364a82* ]] && { e2e_text 1 32 "$y"; return; }; done; }
tree_focus() { local i; for ((i = 0; i < 6; i++)); do [[ $(focused) == 0 ]] && return 0; key C-h; done; }
tree_to() { local i; tree_focus; key G; for ((i = 0; i < 60; i++)); do [[ $(tsel) == *" $1 "* ]] && return 0; key k; done; return 1; }
newcon() { pal ">query.new"; key Enter; sleep 0.8; }
type_in() { key i; e2e_type "$1"; key Escape; sleep 1.5; }                   # 写一句，等 1 秒自动保存
box() { e2e_plain | grep -oE '删除 [^？]*？' | head -1; }
CO3="query_1:doraemon query_2:doraemon query_3:doraemon"

SOLO= start; key C-l
check "启动：Queries 节点下有 query_1（开着，文件还没有，因为没打过字），右端是连接名 doraemon" eval 'co_is "query_1:doraemon" && ws_is "query_1"'
type_in "one"; newcon; type_in "two"; newcon; type_in "three"
check "三个 query：文件 query_1、_2、_3 都落了盘，Queries 下三行，右端都是 doraemon" eval '[[ $(files) == "query_1 query_2 query_3 " ]] && co_is "$CO3" && ws_is "query_1 query_2 query_3"'

# ---- 关掉 query_3：文件还在，树里 Queries 下还有它，工作区下没了；↵ 重新打开，内容还在
e2e_type ':q'; key Enter; sleep 0.8
check "关掉 query_3 的 tab：工作区下没有它，Queries 下还在，文件还在" eval 'ws_is "query_1 query_2" && co_is "$CO3" && [[ $(cat_f query_3) == three ]]'
tree_to query_3; key Enter; sleep 1
check "Queries 下选 query_3 ↵：重新打开，工作区下又有了，内容是 three" eval 'ws_is "query_1 query_2 query_3" && e2e_plain | grep -q "▶ *1 three" || { sections; false; }'
check "（没有另开重复的 query_3 文件，也没多出别的）" eval '[[ $(files) == "query_1 query_2 query_3 " ]]'
e2e_type ':q'; key Enter; sleep 0.8

# ---- 新建的编号跳过有文件的：query_1、_2、_3 都有文件，新建得 query_4
newcon
check "query_3 关着（文件在）时新建：得 query_4，不是 query_3" eval 'ws_is "query_1 query_2 query_4" && co_is "query_1:doraemon query_2:doraemon query_3:doraemon query_4:doraemon"'
check "query_4 没打过字：没有文件，树里 Queries 下仍列出它（占着编号）" eval '[[ $(files) == "query_1 query_2 query_3 " ]]'
type_in "four"
check "写了字之后落盘：query_4.sql 是 four" eval '[[ $(cat_f query_4) == four ]]'
e2e_type ':q'; key Enter; sleep 0.8

# ---- dd：删文件（确认）；开着的拒绝；别的节点上无事
tree_to query_3; key d; key d; sleep 0.5
check "关着的 query_3 上 dd：弹确认「删除 query_3？」" eval '[[ $(box) == "删除 query_3？" ]] || { echo "  box: [$(box)]"; false; }'
key n; sleep 0.4
check "n：取消，文件和树里的项都在" eval '[[ -z $(box) && $(cat_f query_3) == three ]] && co_is "query_1:doraemon query_2:doraemon query_3:doraemon query_4:doraemon"'
key d; key d; sleep 0.4; key y; sleep 0.8
check "y：文件删了，Queries 下没有 query_3 了" eval '[[ $(files) == "query_1 query_2 query_4 " ]] && co_is "query_1:doraemon query_2:doraemon query_4:doraemon"'
newcon
check "再新建：得到全新的 query_3（空内容，没有文件）" eval 'ws_is "query_1 query_2 query_3" && [[ $(files) == "query_1 query_2 query_4 " ]] && l1=$(g=($(geom $(focused))); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + 1)) | tr -d " ▶"); [[ $l1 == 1 ]] || { echo "  line 1: [$l1]"; false; }'
tree_to query_1; key d; key d; sleep 0.6
check "开着的 query_1 上 dd（F10.10 起不再拒绝）：照样弹确认「删除 query_1？」" eval '[[ $(box) == "删除 query_1？" ]] || { echo "  box: [$(box)]"; false; }'
key y; sleep 0.8
check "y：文件删了，它的 tab 关掉了，工作区和 Queries 下都没有 query_1，没有「先关掉」的 toast" eval '[[ $(files) == "query_2 query_4 " ]] && ws_is "query_2 query_3" && co_is "query_2:doraemon query_3:doraemon query_4:doraemon" && ! screen_has "先关掉"'
tree_to public; key d; key d; sleep 0.5
check "schema 节点（public）上 dd：什么都不发生（没有确认框；F8.23 起表节点上 dd 是删表，见 f8.23）" eval '[[ -z $(box) ]] && ! e2e_plain | grep -q "不能撤销"'

# ---- 改名：树里跟着变
key C-l; key C-l 2>/dev/null
check "（准备）焦点在 query_3 上" eval '[[ $(tabbar $(focused)) == *query_3* ]]'
pal ">query.rename"; key Enter; sleep 0.4; for i in $(seq 12); do e2e_keys BSpace; done; e2e_type "mine"; key Enter; sleep 1
check "query.rename 改成 mine：工作区和 Queries 下都是 mine，query_N 的排在前面，改过名的排在后面（query_1 已在上面删掉）" eval 'ws_is "query_2 mine" || ws_is "mine query_2"; co_is "query_2:doraemon query_4:doraemon mine:doraemon" || { sections; false; }'
# ---- 读不了的文件：树里 ↵ 打开它，toast 读取失败，不开 tab（F8.21 起 C-e 不再读旧文件，读失败只剩这一条路）
key Escape; key Escape
printf 'secret' >"$E2E_TMP/data/sqlmux/queries/query_9.sql"; chmod 000 "$E2E_TMP/data/sqlmux/queries/query_9.sql"; sleep 0.5
nt=$(tabbar $(focused))
if tree_to query_9; then key Enter; sleep 1; ok=1; else ok=; fi
check "（准备）外面新放进 queries/ 的 query_9.sql 在树里（目录 mtime 变了，列表跟着变）" eval '[[ -n $ok ]]'
e2e_skip_if_root "读不了的 query_9.sql（权限 000）：树里 ↵ 打开，toast 说读取失败，不多开 tab" || check "读不了的 query_9.sql（权限 000）：树里 ↵ 打开，toast 说读取失败，不多开 tab" eval '[[ $(tabbar $(focused)) != *query_9* ]] && { screen_has "读取失败" || screen_has "denied" || screen_has "permission"; } || { echo "  tab=[$(tabbar $(focused))]"; false; }'
chmod 644 "$E2E_TMP/data/sqlmux/queries/query_9.sql"
e2e_done
