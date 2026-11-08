#!/usr/bin/env bash
# F10.6 连接分组（specs/m10-feedback3/task.md F10.6；tech-design §7.8「连接节点」「新建连接表单」「连接管理」、§14 group）
# connections.toml 里 [[connection]] 的 group：树的根先是没分组的连接，再每个组一段（段前一行 ── 组名 ──、最后一条没字的线），分割线不是节点
# （j / k 跳过、点不动、过滤时不画）；表单有 group 字段（补全已有的组名），保存写 group = "…"；连接管理的列表画同样的线。画法由 golden 覆盖。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
read -r PG_USER PG_PW PG_HOST PG_PORT PG_DB < <(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p.username, p.password, p.hostname, p.port, p.path[1:])' "$SQLMUX_TEST_PG")
mkdir -p "$D/c"
{ for spec in "p1 prod" "plain -" "s1 stage" "p2 prod"; do set -- $spec
    printf '[[connection]]\nname = "%s"\nengine = "postgres"\ndsn = "%s"\n' "$1" "$SQLMUX_TEST_PG"; [[ $2 != - ]] && printf 'group = "%s"\n' "$2"; printf '\n'; done; } >"$D/c/connections.toml"
chmod 600 "$D/c/connections.toml"
SELECT=#364a82
# roots：侧栏里连接这一层（缩进 1 格的 ▸ / ▾ 行）和分割线，逗号连起来：plain,── prod,p1,…；分割线写成「── 组名」，没字的写成 ──
roots() { local y; for y in $(seq 4 41); do e2e_text 1 33 "$y"; done | python3 -c '
import re, sys
out = []
for l in re.sub(r"[\ue000-\uf8ff]", "", sys.stdin.read()).split("\n"):
    if "工作区" in l: break
    body = re.sub(r"^│|│\s*$", "", l).strip()
    m = re.match(r"^│ [▸▾]\s+(\S+)", l)
    if body.startswith("──"):
        name = body.strip("─ ")
        out.append("── " + name if name else "──")
    elif m:
        out.append(m[1])
print(",".join(out))'; }
roots_is() { [[ $(roots) == "$1" ]] || { echo "  roots: '$(roots)'"; echo "  want:  '$1'"; false; }; }
tsel() { local y; for y in $(seq 3 43); do [[ $(e2e_style 4 "$y") == *bg=$SELECT* ]] && { e2e_text 1 32 "$y" | python3 -c "import re, sys; print(re.sub(r'^[│ ▸▾\ue000-\uf8ff]+', '', sys.stdin.read().strip()).rstrip('│ '))"; return; }; done; }   # 选中行的文字（去掉边框、展开符、图标）
CT() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
frows() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 28)) $((g[0] + g[2] - 2)) "$y" | noicon | sed 's/^ *//; s/ *$//'; done; }
fval() { frows | awk -v k="$1" '$1 == k { if (NF == 1) print ""; else { sub(/^[^ ]+ /, ""); print }; exit }'; }
clr() { local i; for ((i = 0; i < 40; i++)); do e2e_keys BSpace; done; sleep 0.2; }

SOLO= start -C "$D/c"
check "树的根：没分组的 plain 最前面，然后 prod 段（p1、p2，组内按文件顺序）、stage 段（s1），最后一条没字的线" roots_is "plain,── prod,p1,p2,── stage,s1,──"
check "分割线是 sep 色（不是节点的 fg），组名同色线上" eval 'y=$(e2e_plain | awk "/── prod/ { print NR; exit }"); style_has 4 $y fg=#2f3549'

# ---- 分割线不是节点：j / k 跳过，不能点
key C-h; key g g
check "gg：光标在第一个连接 plain" eval '[[ $(tsel) == plain* ]]'
key j
check "j：跳过「── prod」，落在 p1" eval '[[ $(tsel) == p1* ]]'
key k
check "k：回到 plain（不停在分割线上）" eval '[[ $(tsel) == plain* ]]'
key j; key h; key j
check "（p1 折起来后）j：p2" eval '[[ $(tsel) == p2* ]]'
key j
check "j：跳过「── stage」，落在 s1" eval '[[ $(tsel) == s1* ]]'
key j
check "s1 之后 j：跳过最后那条没字的线，落在 工作区" eval '[[ $(tsel) == 工作区* ]]'
key G
check "G：最后一个节点（不是分割线）" eval '[[ -n $(tsel) && $(tsel) != ─* ]]'
y=$(e2e_plain | awk '/── prod/ { print NR; exit }'); sel0=$(tsel)
e2e_click 10 "$y"; sleep 0.4
check "点击「── prod」那一行：什么都没发生（选中、展开都不变）" eval '[[ $(tsel) == "$sel0" ]] && roots_is "plain,── prod,p1,p2,── stage,s1,──"'

# ---- 过滤时不画分割线
key g g; key /; e2e_type p; sleep 0.5
check "过滤（/ p）：树里没有分割线（只剩匹配的表和它们的上级）" eval '[[ $(roots) != *──* && $(roots) == p1 ]] || { echo "  roots: $(roots)"; false; }'
key Escape; sleep 0.3

# ---- 表单：group 在 name 和 engine 之间，补全已有的组名，保存写 group
pal ">conn.new"; key Enter; sleep 0.6
check "表单第二行是 group（name 下一行），值为空" eval '[[ $(frows | sed -n 2p) == group && $(frows | sed -n 3p) == engine* ]]'
e2e_type neo; key Tab; e2e_type st; sleep 0.5
check "group 里输入 st：补全列表里是 stage（前缀匹配，没有 prod、plain）" eval 'e2e_plain | grep -q "│ stage *│" && ! e2e_plain | grep -q "│ prod *│"'
key Tab; sleep 0.3
check "Tab 接受：group 是 stage" eval '[[ $(fval group) == stage ]]'
key Tab; key Tab; e2e_type "$PG_HOST"; key Tab; clr; e2e_type "$PG_PORT"; key Tab; e2e_type "$PG_USER"; key Tab; e2e_type "$PG_PW"; key Tab; e2e_type "$PG_DB"; key Enter; sleep 1.2
check "保存：connections.toml 里 neo 那一块有 group = \"stage\"，原来的块一个字节不动" eval 'f=$(CT); grep -A6 "name = \"neo\"" "$f" | grep -q "^group = \"stage\"" && [[ $(grep -c "^group" "$f") == 4 ]] && head -c $(fsize "$D/c/connections.toml") "$f" | cmp -s - "$D/c/connections.toml"'
check "树里 neo 排在 stage 段 s1 后面，仍在最后那条线之前" roots_is "plain,── prod,p1,p2,── stage,s1,neo,──"

# ---- 没填 group：不写 group 这一行，排在没分组的那段
pal ">conn.new"; key Enter; sleep 0.6; e2e_type bare; key Tab; key Tab; key Tab; e2e_type "$PG_HOST"; key Tab; clr; e2e_type "$PG_PORT"; key Tab; e2e_type "$PG_USER"; key Tab; e2e_type "$PG_PW"; key Tab; e2e_type "$PG_DB"; key Enter; sleep 1.2
check "没填 group：文件里 bare 那一块没有 group 行，树里排在没分组的 plain 后面" eval '! grep -A5 "name = \"bare\"" "$(CT)" | head -5 | grep -q "^group" && roots_is "plain,bare,── prod,p1,p2,── stage,s1,neo,──"'

# ---- 连接管理：列表画同样的线，选中带分组的连接，表单里 group 是它的组
pal ">conn.manage"; key Enter; sleep 0.7
check "连接管理的列表里有「── prod」「── stage」两条线" eval 'e2e_plain | grep -q "│ ── prod ─" && e2e_plain | grep -q "│ ── stage ─"'
e2e_done
