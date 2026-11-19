#!/usr/bin/env bash
# F16.11c Redis 的右键菜单和补全候选的图标（specs/m16-redis/task.md F16.11；tech-design §7.7、§7.8、§9.7、§9.8）
# 树上 db / 命名空间 / 键的右键菜单每项左边一个图标（刷新 refresh、删除 trash、打开 open、改名 rename、设 TTL auto_refresh）；
# 控制台的补全：命令 command（色 func）、子命令和 token 是 keyword、键名 key、HASH 的 field column；ascii 下菜单没有图标、候选前面是一个字符。
# 每个脚本自己起 redis-server（redis.sh）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/cellops.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1; redis_seed
mkdir -p "$D/c" "$D/a"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/c/connections.toml"; cp "$D/c/connections.toml" "$D/a/connections.toml")
printf 'icons = "ascii"\n' >"$D/a/config.toml"
BOX() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
rows() { local g y; g=($(BOX)); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y; done | python3 -c '
import re, sys
for l in sys.stdin.read().split("\n"):
    l = l.strip()
    if not l: continue
    if "\ue000" <= l[0] <= "\uf8ff" or "\U000f0000" <= l[0] <= "\U000ffffd": print("U+%04x %s" % (ord(l[0]), re.sub(r"\s{2,}\S+$", "", l[1:].strip())))
    else: print("- " + re.sub(r"\s{2,}\S+$", "", l))'; }   # the key at the right dropped (F22.3)
icon_fg() { local g; g=($(BOX)); e2e_style $((g[0] + 2)) $((g[1] + $1)); }
rows_are() { local got; got=$(rows); [[ $got == "$1" ]] || { echo "  rows:"; echo "$got" | sed 's/^/    /'; false; }; }
has_row() { rows | grep -qE "$1" || { echo "  no row /$1/ in:"; rows | sed 's/^/    /'; false; }; }
tree_row() { e2e_plain | python3 -c '
import re, sys
for i, l in enumerate(sys.stdin.read().split("\n"), 1):
    if re.search(sys.argv[1], re.sub("[-] ?", "", l[:34])): print(i); break' "$1"; }
newq() { key Escape; key Escape; e2e_type ggdG; sleep 0.3; key i; e2e_type "$1"; sleep 1.5; }

e2e_start -C "$D/c" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep 2
y=$(tree_row 'db0 +29'); rclick 10 "$y"; sleep 0.6
check "db 节点右键：刷新，前面是 refresh 图标（U+f021）" rows_are "U+f021 刷新"
key Escape; sleep 0.3
y=$(tree_row '▸ +str +7'); rclick 10 "$y"; sleep 0.6
check "命名空间右键：删除，前面是 trash 图标（U+f1f8）" rows_are "U+f1f8 删除…"
key Escape; sleep 0.3
e2e_click 10 "$(tree_row '▸ +str +7')"; sleep 0.8
y=$(tree_row 'plain'); rclick 12 "$y"; sleep 0.6
check "键右键：打开 open、删除 trash、改名 rename、设 TTL auto_refresh（U+f017）" rows_are "U+f115 打开
U+f1f8 删除…
U+f246 改名…
U+f017 设 TTL…"
check "选中行的图标和文字同色，其余是 dim" eval '[[ $(icon_fg 1) == "fg=#c0caf5 "* && $(icon_fg 2) == "fg=#565f89 "* ]]' || { icon_fg 1; icon_fg 2; }
key Escape; sleep 0.3

# ---- 控制台的补全
key C-l; key C-l; key i
newq "GE"
check "命令：GET 等前面是 command 图标（U+f0e7），色 func（#7aa2f7）" eval 'has_row "^U\+f0e7 GET " && [[ $(icon_fg 1) == "fg=#7aa2f7 "* ]]' || { rows | head -3; icon_fg 1; }
newq "CONFIG G"
check "子命令：CONFIG GET 的 GET 是 keyword 图标（U+f030b），色 keyword（#bb9af7）" eval 'has_row "^U\+f030b GET " && [[ $(icon_fg 1) == "fg=#bb9af7 "* ]]' || { rows | head -3; icon_fg 1; }
newq "GET str:p"
check "键名：str:plain 前面是 key 图标（U+f084）" eval 'has_row "^U\+f084 str:plain"' || rows
newq "HGET hash:user a"
check "HASH 的 field：age 前面是 column 图标（U+eb5f），dim" eval 'has_row "^U\+eb5f age" && [[ $(icon_fg 1) == "fg=#565f89 "* ]]' || { rows | head -3; icon_fg 1; }
newq "SET zzz 1 N"
check "参数 token：NX 是 keyword 图标（U+f030b）" eval 'has_row "^U\+f030b NX"' || rows
newq "GET str:p"; key Tab; key Enter; sleep 0.4
check "接受键名候选：写进去的是 str:plain，没有图标字符" eval 'e2e_plain | python3 -c "
import re, sys
m = re.search(r\"GET (\\S+)\", sys.stdin.read())
sys.exit(0 if m and m.group(1) == \"str:plain\" else 1)"' || e2e_plain | grep -a "GET"
_e2e_kill

# ---- icons = "ascii"
e2e_start -C "$D/a" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep 2
y=$(tree_row 'db0 +29'); rclick 10 "$y"; sleep 0.6
check "ascii：db 节点的菜单没有图标，行首就是文字" rows_are "- 刷新"
key Escape; sleep 0.3
e2e_click 10 "$(tree_row 'str +7')"; sleep 0.8
y=$(tree_row 'plain'); rclick 12 "$y"; sleep 0.6
check "ascii：键的菜单四项都没有图标" rows_are "- 打开
- 删除…
- 改名…
- 设 TTL…"
key Escape; sleep 0.3
key C-l; key C-l; key i
newq "GE"
check "ascii：命令候选前面是 :（command 的 ascii 图标）" eval 'has_row "^- : GET "' || rows
newq "CONFIG G"
check "ascii：子命令前面是 k" eval 'has_row "^- k GET "' || rows
newq "GET str:p"
check "ascii：键名候选前面是一个字符（key 的 ascii 图标）" eval 'has_row "^- [^ ] str:plain"' || rows
e2e_done
