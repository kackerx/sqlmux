#!/usr/bin/env bash
# F0.3 keymap 引擎与配置（specs/m0-skeleton/task.md F0.3；tech-design §6.2–§6.8、§14）
# 解析、trie、超时、次数、映射优先级由单测覆盖；这里测 `sqlmux keys` CLI、配置加载和界面上的键位文字。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

CFG=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); mkdir -p "$CFG/sqlmux"
trap 'e2e_stop; rm -rf "$CFG"' EXIT
conf() { printf "$1" >| "$CFG/sqlmux/config.toml"; }   # printf 格式串：\n 换行
keys() { XDG_CONFIG_HOME=$CFG "$E2E_BIN" keys "$@"; }
# run ARGS... — 跑 `sqlmux keys ARGS`，结果放进 OUT / ERR / RC
run() { OUT=$(keys "$@" 2>"$CFG/err"); RC=$?; ERR=$(<"$CFG/err"); }
rc_is() { [[ $RC == "$1" ]] || { echo "  rc=$RC, want $1; stderr: $ERR"; false; }; }
err_has() { [[ $ERR == *"$1"* ]] || { echo "  stderr lacks '$1': $ERR"; false; }; }
out_has() { [[ $OUT == *"$1"* ]] || { echo "  stdout lacks '$1'"; false; }; }
out_lacks() { [[ $OUT != *"$1"* ]] || { echo "  stdout has '$1'"; false; }; }
count_rows() { grep -cF -- "$1" <<<"$OUT"; }

# ---- 默认键位
conf ''
run
check "keys：退出码 0、stderr 为空" eval 'rc_is 0 && [[ -z $ERR ]]'
check "keys：markdown 表头" out_has $'| 作用域 | 键 | 操作 |\n|---|---|---|'
check "keys：行数与 default.toml 的绑定数一致" eval '(( $(grep -c "^| [a-z]" <<<"$OUT") == $(grep -cE "^[\"'"'"']" "$E2E_ROOT/internal/keymap/default.toml") ))'
check "keys：§6.8 抽查（C-p / SPC s / SPC \" / 表格 ↵ / console gq / result P）" eval 'out_has "| global | \`C-p\` | palette.open |" && out_has "| normal | \`SPC s\` | session.list |" && out_has "| normal | \`SPC \"\` | pane.split.below |" && out_has "| grid | \`↵\` | grid.edit |" && out_has "| console | \`gq\` | console.format |" && out_has "| result | \`P\` | result.pin |"'
check "默认键位只用 §6.2 允许的键（无 Alt、C-S-、C-数字、C-i/m/[、F 键、Home/End/PgUp/PgDn）" eval '! grep -E "\`[^\`]*(M-|A-|C-S-|C-[0-9]|C-i|C-m|C-\[|F[0-9]|Home|End|PageUp|PageDown)[^\`]*\`" <<<"$OUT"'
check "C-h 只出现在 NORMAL" eval '[[ $(grep -F "\`C-h\`" <<<"$OUT" | cut -d"|" -f2 | tr -d " " | sort -u) == normal ]]'
run --check; check "--check：默认配置退出码 0" rc_is 0

# ---- --format toml 能重新加载（§6.7 导出）
roundtrip() {
  conf "$1"; run; local md1=$OUT; run --format toml; local t1=$OUT
  printf '%s\n' "$t1" >| "$CFG/sqlmux/config.toml"
  run --format toml; [[ $OUT == "$t1" ]] || { echo "  toml 两次输出不同"; diff <(echo "$t1") <(echo "$OUT") | head; return 1; }
  run; [[ $OUT == "$md1" ]] || { echo "  重新加载后键位表变了"; diff <(echo "$md1") <(echo "$OUT") | head; return 1; }
  run --check; rc_is 0
}
check "toml 往返：默认" roundtrip ''
check "toml 往返：leader = <C-a>" roundtrip '[keys]\nleader = "<C-a>"\n'
check "toml 往返：改键 + 解绑" roundtrip '[keys.normal]\n"<Space>s" = ""\n"<C-b>" = "tree.toggle"\n"<Leader>b" = ""\n[keys.global]\n"<C-c>" = ""\n'
check "toml 往返：映射（通用 + 按 pane）" roundtrip '[map.normal]\nJ = "5j"\n[map.console.normal]\nL = "5l"\nQ = "<Leader>b"\n[map.grid.normal]\nH = "0"\n'
check "toml 往返：特殊字符 | < \\" roundtrip '[keys.grid]\n"<Bar>" = "p"\n"<lt>" = "q"\n"\\\\" = "r"\n'
check "toml 往返：自定义序列 <C-w>v" roundtrip '[keys.normal]\n"<C-w>v" = "pane.split.right"\n'

# ---- 冲突检测（§6.7）：非零退出，指出作用域和键
conf '[keys.normal]\n"<C-x>" = "a"\n"<c-x>" = "b"\n'; run --check
check "规范化后相同的键 → 退出码 1，[keys.normal] <c-x>" eval 'rc_is 1 && err_has "[keys.normal] <c-x>: 与 \"<C-x>\" 是同一个键"'
conf '[keys.normal]\n"<Leader>s" = "a"\n"<Space>s" = "b"\n'; run --check
check "<Leader>s 与 <Space>s 是同一个键 → 退出码 1" eval 'rc_is 1 && err_has "[keys.normal] <Space>s: 与 \"<Leader>s\" 是同一个键"'
conf '[keys.normal]\n"<C-x>" = "a"\n"<C-x>" = "b"\n'; run --check
check "字面重复的键 → 退出码 1，指出 keys.normal.<C-x>" eval 'rc_is 1 && err_has "keys.normal.\"<C-x>\""'
conf '[keys.normal]\n"g" = "x"\n'; run --check
check "单键是已有序列的前缀 → 退出码 1，给出警告" eval 'rc_is 1 && err_has "[keys.normal] g: 是 gt 的前缀"'
conf '[keys.foo]\n"a" = "b"\n'; run --check; check "未知的表 → 退出码 1" eval 'rc_is 1 && err_has "[keys.foo] a: 没有这个表"'
conf '[keys.normal]\n"<C-foo>" = "b"\n'; run --check; check "写错的键名 → 退出码 1" eval 'rc_is 1 && err_has "[keys.normal] <C-foo>: 键位写法不对"'

# ---- 上一轮修过的三处（reviewer 要求回归）
conf '[keys.normal]\n"<Space>s" = "x.y"\n'; run
check "\"<Space>s\" 改绑默认 <Leader>s：只剩一行、不报冲突" eval '(( $(count_rows "| normal | \`SPC s\`") == 1 )) && out_has "| normal | \`SPC s\` | x.y |" && { run --check; rc_is 0; }'
conf '[keys.normal]\n"<Space>s" = ""\n'; run
check "\"<Space>s\" = \"\" 解绑默认 <Leader>s" eval 'out_lacks "\`SPC s\`" && out_lacks "session.list"'
conf '[keys]\nleader = " "\n'; run --check; local_rc=$RC; run --format toml
check "leader = \" \"（字面空格）等同 <Space>" eval '[[ $local_rc == 0 ]] && out_has "leader = \"<Space>\"" && { run; out_has "| normal | \`SPC b\` | tree.toggle |"; }'
conf '[map.normal]\nQ = "<Leader>b"\n'; run
check "映射右侧写 <Leader>：接受、不报错" eval 'out_has "| map.normal | \`Q\` | → <Leader>b |" && { run --check; rc_is 0; }'

# ---- 配置文件位置（§14）：XDG_CONFIG_HOME，未设置时 ~/.config
H=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-home.XXXXXX"); mkdir -p "$H/.config/sqlmux"
printf '[keys]\nleader = "<C-a>"\n' > "$H/.config/sqlmux/config.toml"
check "未设置 XDG_CONFIG_HOME 时读 ~/.config/sqlmux/config.toml" eval '[[ $(env -u XDG_CONFIG_HOME HOME="$H" "$E2E_BIN" keys --format toml) == *"leader = \"<C-a>\""* ]]'
check "设置了 XDG_CONFIG_HOME 时不读 ~/.config" eval '[[ $(HOME="$H" XDG_CONFIG_HOME="$CFG/none" "$E2E_BIN" keys --format toml) == *"leader = \"<Space>\""* ]]'
rm -rf "$H"

# ---- 界面：配置生效、键位文字取自 keymap（§6.7）
tui() { conf "$1"; e2e_start -c "$CFG/sqlmux/config.toml" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
pua() { e2e_plain | python3 -c 'import sys; sys.exit(0 if any(0xE000 <= ord(c) <= 0xF8FF for c in sys.stdin.read()) else 1)'; }
tui ''; check "默认 nerd：画面有私有区码点（对照）" pua
tui 'icons = "ascii"\n'; check "icons = \"ascii\"：画面没有私有区码点" eval '! pua'
tui '[keys.normal]\n"<Space>b" = ""\n"<C-b>" = "tree.toggle"\n'; check "改绑 tree.toggle 为 C-b：侧栏标题显示 C-b" text_ends 1 32 1 " C-b ─┐"
tui '[keys]\nleader = "<C-a>"\n'; check "leader = <C-a>：侧栏标题显示 C-a b" text_ends 1 32 1 " C-a b ─┐"
tui '[keys.normal]\n"<Leader>b" = ""\n'; check "解绑 tree.toggle：侧栏标题不显示提示" eval '[[ $(e2e_text 1 32 1) == *"public ▾ ───"*"─┐" && $(e2e_text 1 32 1) != *SPC* ]]'
tui '[keys.console]\n"<CR>" = ""\n"R" = "console.run"\n'; check "改绑 console.run 为 R：标题显示 ▶ run R" text_ends 105 160 1 "▶ run  R ─┐"
tui '[keys.grid]\n"T" = ""\n'; check "解绑 grid.transpose：tab 栏不显示「转置」" eval '[[ $(e2e_text 34 103 43) != *转置* && $(e2e_text 34 103 43) == *"↵ edit"* ]]'
# 侧栏提示行（§6.7、§7.8）：未绑定的整项不显示；放不下时整项省略
side_hints() { local c; c=$(e2e_find ┐ 1); text_is 1 "${c%% *}" 43 "$1"; }
tui ''; check "侧栏提示行（默认）" side_hints "│ j/k move  ↵ open  t tab      │"
tui '[keys.tree]\n"<CR>" = ""\n'; check "解绑 tree.open：整项不显示" side_hints "│ j/k move  t tab              │"
tui '[keys.tree]\n"j" = ""\n'; check "解绑 tree.down：j/k move 整项不显示" side_hints "│ ↵ open  t tab                │"
conf ''; e2e_start -x 80 "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3
check "80 宽：放不下的 t tab 整项省略" side_hints "│ j/k move  ↵ open     │"

# ---- 配置写错：启动直接报错退出，指出文件
tui 'icons = "emoji"\n'
check "icons = \"emoji\"：启动报错、退出码 1" eval 'wait_for 3 screen_has "[e2e-exit 1]" && screen_has "config.toml: icons = \"emoji\"" && flag_is alternate_on 0'
tui 'timeoutlen = 0\n'; check "timeoutlen = 0：启动报错" eval 'wait_for 3 screen_has "[e2e-exit 1]" && screen_has "timeoutlen = 0"'
tui 'not toml\n'; check "TOML 语法错误：启动报错并给出行号" eval 'wait_for 3 screen_has "[e2e-exit 1]" && screen_has "line 1"'

e2e_done
