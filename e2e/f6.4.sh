#!/usr/bin/env bash
# F6.4 界面多语言（specs/m6-config/task.md F6.4；tech-design §14「界面语言」）
# 翻译表的覆盖、「含汉字的字面量都走 T」有单测扫描；en 的画面有 golden。这里在真实终端里测：language = "en" 启动后引导页、状态栏、面板、
# toast、日志、命令标题都是英文（数据库返回的原文不翻译），命令面板按英文标题和 action id 都搜得到，sqlmux keys 的输出跟着语言；
# language 写错时启动报错（带路径、退出码 1，英文）；F7.6 起默认是英文，language = "zh" 才是中文（lib.sh 默认补 zh，这里用 E2E_NOLANG=1 关掉）。只读，用共用库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
# palette.sh 用中文标题「命令面板」找浮层，en 下是 Command palette
top()  { e2e_plain | python3 -c 'import sys; print(next((i + 1 for i, l in enumerate(sys.stdin) if "┌─ Command palette" in l), ""))'; }
left() { local t; t=$(top); [[ -n $t ]] && e2e_find "┌─ Command palette" "$t" | cut -d' ' -f1; }

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
mkdir -p "$D/en" "$D/fr" "$D/zh" "$D/none"
printf 'language = "en"\n' >"$D/en/config.toml"; printf 'language = "fr"\n' >"$D/fr/config.toml"; printf 'language = "zh"\n' >"$D/zh/config.toml"
paste() { t set-buffer -b e2e -- "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
toast_text() { e2e_text 1 "$(e2e_flag pane_width)" $(( $(H) - 1 )); }

# ---- 默认英文（F7.6）；language = "zh" 才是中文
E2E_NOLANG=1 start -C "$D/none"
check "不写 language：默认英文（引导页 Open a table，没有汉字「打开表」）" eval 'screen_has "Open a table" && ! screen_has "打开表"'
key C-c
check "不写 language：toast 英文（Press C-c again to quit），整屏没有汉字" eval 'screen_has "Press C-c again to quit" && e2e_plain | python3 -c "import sys,re; sys.exit(1 if re.search(r\"[\\u4e00-\\u9fff]\", sys.stdin.read()) else 0)"'
e2e_keys C-p; sleep 0.6
check "不写 language：命令面板标题 Command palette、范围标签英文" eval 'p=$(e2e_plain); grep -q "Command palette" <<<"$p" && grep -q "Table @" <<<"$p"'
e2e_stop
E2E_NOLANG=1 start -C "$D/zh"
check "language = \"zh\"：中文（引导页「打开表」）" eval 'screen_has "打开表" && ! screen_has "Open a table"'
e2e_stop

# ---- language 写错：启动报错，带路径，退出码 1
start -C "$D/fr"; sleep 1
check "language = \"fr\"：退出码 1，错误带 config.toml 的路径，说明只能是 en 或 zh（英文，读 config 之前）" eval 'screen_has "[e2e-exit 1]" && screen_has "config.toml" && screen_has "language = \"fr\": must be \"en\" or \"zh\"" && e2e_plain | python3 -c "import sys,re; sys.exit(1 if re.search(r\"[\\u4e00-\\u9fff]\", sys.stdin.read()) else 0)"'
e2e_stop

# ---- en：界面都是英文
start -C "$D/en"; sleep 0.5
check "引导页：Open a table / New query，没有中文" eval 'screen_has "Open a table" && screen_has "New query" && ! e2e_plain | grep -qE "打开表|新建 query"'
check "query 标题栏：Tx: Auto（不是 自动）" eval 'screen_has "Tx: Auto" && ! screen_has "自动"'
key C-c
check "toast：Press C-c again to quit（键位文字照旧从 keymap 读）" eval 'screen_has "Press C-c again to quit"'
key C-l; key i; paste "select 1 as x; select nosuchcol"; key Escape; key V; key G; key Enter; sleep 2
check "日志：tab 叫 Log，行里是 1 rows · 1ms，数据库的错误原文（ERROR: column \"nosuchcol\" does not exist）不翻译" eval 'screen_has "Log*" && screen_has "1 rows" && screen_has "ERROR: column \"nosuchcol\" does not exist"'
check "状态栏和结果区底部的工具行里没有汉字" eval 'e2e_plain | tail -3 | python3 -c "import sys,re; sys.exit(1 if re.search(r\"[\u4e00-\u9fff]\", sys.stdin.read()) else 0)"'
e2e_keys C-p; sleep 0.6
check "命令面板：标题 Command palette，范围标签 All / Session / Win·Pane / Win / Tab / Table / Key / Cmd / SQL，底栏 Move / Scope / Close" eval 'p=$(e2e_plain); grep -q "Command palette" <<<"$p" && grep -q "All   Session \\$   Win·Pane %   Win &   Tab #   Table @   Key :   Cmd >   SQL ;" <<<"$p" && grep -q "Move" <<<"$p" && grep -q "Close" <<<"$p"'
e2e_type ">switch theme"; sleep 0.8
check "命令标题是英文：Switch theme…（theme.switch），能按英文标题搜到" eval 'list | head -1 | grep -q "^Switch theme…  theme.switch"'
clear_input; e2e_type ">tree.toggle"; sleep 0.8
check "action id 照旧可以搜：tree.toggle 的标题是英文" eval 'list | head -1 | grep -q "tree.toggle"'
key Escape
e2e_keys C-p; sleep 0.4; e2e_type ">switch theme"; sleep 0.6; key Enter; sleep 0.5; key Down; key Enter; sleep 0.5
check "切主题的 toast 是英文：Only a theme written in config.toml is kept" eval 'toast_text | grep -q "Only a theme written in config.toml is kept"'
e2e_stop

# ---- sqlmux keys 的输出跟着语言
mkdir -p "$D/xdg-en/sqlmux" "$D/xdg-zh/sqlmux" "$D/xdg-none/sqlmux"; cp "$D/en/config.toml" "$D/xdg-en/sqlmux/config.toml"; cp "$D/zh/config.toml" "$D/xdg-zh/sqlmux/config.toml"
keys() { XDG_CONFIG_HOME=$D/$1 XDG_STATE_HOME=$D/st XDG_DATA_HOME=$D/st "$E2E_BIN" keys --format toml; }
check "sqlmux keys（language = en）：Action 标题是英文（Switch theme… 之类），没有汉字标题" eval 'k=$(keys xdg-en); grep -q "Switch theme" <<<"$k" && ! grep -q "切换主题" <<<"$k"'
check "sqlmux keys（不写 language）：默认英文标题" eval 'k=$(keys xdg-none); grep -q "Switch theme" <<<"$k" && ! grep -q "切换主题" <<<"$k"'
check "sqlmux keys（language = zh）：标题是中文" eval 'k=$(keys xdg-zh); grep -q "切换主题" <<<"$k" && ! grep -q "Switch theme" <<<"$k"'

e2e_done
