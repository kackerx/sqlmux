#!/usr/bin/env bash
# F22.14 看得出连接连没连上，断开有反馈（specs/m22-menus-keys/task.md F22.14；tech-design §7.8「连接节点」「断开连接」，§7.7 图标 connected）
# 已连上的连接节点名字后面 1 列空白加 focus 色（#9ece6a）的标记（nerd U+F444，ascii *），收起着也看得到；没连上的（懒连接）没有；
# 右键「断开连接」→ toast「已断开 ca」、标记消失；在它的 tab 上 R 重连后标记回来；只读连接的标记在 lock 之前；ascii 图标下是 *。自建库，state 放临时目录。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table f22_14(id int); insert into f22_14 select generate_series(1, 3)"
mkdir -p "$D/c" "$D/a"
(umask 077; printf '[[connection]]\nname = "ca"\nengine = "postgres"\ndsn = "%s"\n[[connection]]\nname = "cb"\nengine = "postgres"\ndsn = "%s"\nread_only = true\n' "$E2E_DB" "$E2E_DB" >"$D/c/connections.toml")
cp "$D/c/connections.toml" "$D/a/"; printf 'icons = "ascii"\n' >"$D/a/config.toml"
DOT=$(printf '\xef\x91\x84')   # U+F444 nf-oct-dot_fill
FOCUS=#9ece6a
srow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") =~ [▸▾v\>]\ +([^ ]+\ +)?$1( |$) ]] && { echo "$y"; return; }; done; }
rtext() { e2e_text 1 32 "$(srow "$1")" | sed 's/ *│ *$//'; }
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
toasted() { [[ $(e2e_text 1 200 $(($(e2e_flag pane_height) - 1))) == *"$1"* || $(bar) == *"$1"* ]]; }
# has_dot NAME: the row of NAME has " <DOT>" right after the name, the dot in focus color
has_dot() { local y x; y=$(srow "$1"); [[ -n $y ]] || return 1; x=$(e2e_find "$1 $DOT" "$y" | cut -d' ' -f1); [[ -n $x ]] && style_has $((x + ${#1} + 1)) "$y" "fg=$FOCUS"; }
no_dot() { [[ -n $(srow "$1") && $(rtext "$1") != *"$DOT"* ]] || { echo "  row: '$(rtext "$1")'"; false; }; }

SOLO=1 start -x 200 -C "$D/c" -S "$D/st"; sleep 1
check "（准备）ca 连上了（默认展开），cb（懒连接）还没连" eval 'wait_for 8 eval "[[ \$(rtext ca) == *▾* ]]" && [[ $(rtext cb) == *▸* ]]'
check "已连上的 ca：名字后面 1 列空白加 focus 色的连接标记" eval 'has_dot ca || { echo "  row: $(rtext ca)"; false; }'
check "没连上的 cb：没有标记" no_dot cb
y=$(srow ca); e2e_click 10 "$y"; sleep 0.3; key C-h; key h; sleep 0.5
check "收起 ca 的节点（▸）：标记还在" eval '[[ $(rtext ca) == *▸* ]] && has_dot ca || { echo "  row: $(rtext ca)"; false; }'

# 只读连接：标记在 lock 之前
y=$(srow cb); e2e_click 10 "$y"; sleep 0.3; key l; wait_for 8 eval '[[ $(rtext cb) == *▾* ]]'; sleep 0.5
check "只读的 cb 连上后：名字后面先是标记、再是 lock" eval 'r=$(rtext cb); [[ $r == *"cb $DOT "* ]] && has_dot cb && x=$(e2e_find "$DOT" $(srow cb) | cut -d" " -f1) && [[ -n $(e2e_text $((x + 1)) 32 $(srow cb) | tr -d " │") ]] || { echo "  row: $(rtext cb)"; false; }'

# 断开：toast、标记消失。ca 和 cb 是同一个库，@f22_14 有两条：挑 ca 的那条
. "$(dirname "$0")/palette.sh"
pal "@f22_14"; sleep 0.8; n=$(list | cut -d'|' -f1 | grep -n "ca.public" | cut -d: -f1)
for ((i = 1; i < n; i++)); do e2e_keys Down; sleep 0.15; done; key Enter; sleep 1.2
check "（准备）f22_14 是 ca 的（状态栏的地址块是 ca）" eval '[[ $(bar) == *" ca · "* ]] || { bar; false; }'
y=$(srow ca); rclick 12 "$y"; key j; key j; key Enter
check "右键 ca →「断开连接」：toast「已断开 ca」" eval 'wait_for 3 toasted "已断开 ca"'
check "断开后 ca 的标记消失，cb 的还在" eval 'no_dot ca && has_dot cb'
check "f22_14 的 tab 还在" eval 'e2e_plain | grep -q "f22_14"'
key C-l; sleep 0.3; key R; wait_for 10 eval 'has_dot ca'
check "在它的 tab 上 R：重连上，标记回来" eval 'has_dot ca || { echo "  row: $(rtext ca)"; false; }'
_e2e_kill

# ascii 图标：标记是 *
SOLO=1 start -x 200 -C "$D/a" -S "$D/st2"; sleep 1
DOT='*'
check "icons = \"ascii\"：已连上的 ca 名字后面是 focus 色的 *" eval 'wait_for 8 has_dot ca || { echo "  row: $(rtext ca)"; false; }'
check "icons = \"ascii\"：没连上的 cb 没有 *" no_dot cb
e2e_done
