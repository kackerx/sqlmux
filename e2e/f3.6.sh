#!/usr/bin/env bash
# F3.6 console tab（specs/m3-console/task.md F3.6；tech-design §5 默认布局、§11 编辑器 / 文件、§7.3 高亮、§6.8、§14）
# 高亮、gutter、语句范围、选区、命令行的画法由 golden 覆盖；这里测真实终端里的按键、鼠标、粘贴、光标形状和文件。
# 默认布局 ⓪ | ① | ② console_1，② 在 x 105..160：x 106 是 ▶，107..109 行号，111 起是文字；第 y 行是第 y−1 行文字。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); DD=$D/data; CF=$DD/sqlmux/consoles/doraemon/console_1.sql
trap 'e2e_stop; rm -rf "$D"' EXIT
KEYWORD=#bb9af7 NUMBER=#ff9e64 STR=#9ece6a COMMENT=#565f89 FUNC=#7aa2f7 FG=#c0caf5 DIM=#565f89 FOCUS=#9ece6a ROW=#292e42 PANE_BG=#24283b
line() { e2e_text 111 159 $(($1 + 1)) | sed 's/ *$//'; }                         # 第 N 行的文字
cur() { echo "$(( $(e2e_flag cursor_y) )),$(( $(e2e_flag cursor_x) - 109 ))"; }     # 光标在第几行、第几个字符（都从 1 起）
cur_is() { [[ $(cur) == "$1" ]] || { echo "  cursor $(cur), want $1"; false; }; }
mode() { bar | awk '{ print $NF }'; }
info() { bar | grep -oE '[0-9]+ (字符|行)( × [0-9]+ 列)? · ↵ run'; }            # VISUAL 的附加信息
pending() { bar | grep -oE '[^ ]+ +[^ ]+ [^ ]+@.*$' | awk '{ print $1 }'; }   # 状态栏连接图标左边那一块（没有待输入时是 ·）
typ() { e2e_type "$1"; sleep 0.3; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }   # bracketed paste，-r：原样发出，不把 LF 换成 CR

start -D "$DD"
check "默认布局：⓪ | ① | ② console_1，焦点在 ①" eval '[[ $(e2e_panes | awk "{ print \$1 }" | tr "\n" " ") == "0 1 2 " && $(focused) == 1 && $(e2e_text 105 130 1) == *"console_1"* ]]'
key C-l
check "C-l 到 ②：NORMAL，不显示 行,列" eval '[[ $(focused) == 2 && $(mode) == NORMAL && -z $(pos) ]]'
check "NORMAL 下终端光标是块" flag_is cursor_shape block
key i
check "i：INSERT，终端光标是竖线" eval '[[ $(mode) == INSERT ]] && flag_is cursor_shape bar'
typ "select count(*), 'a' from t where id > 10"; e2e_keys '\;'; typ " -- n"; key Enter
typ "select 2 as b"; key Escape

# ---- 高亮、▶、语句范围（§7.3、§9.2）
check "关键字 keyword、函数 func、字符串 sql_string、数字 number、注释 comment、其余 fg" eval 'style_has 111 2 fg=$KEYWORD && style_has 118 2 fg=$FUNC && style_has 128 2 fg=$STR && style_has 150 2 fg=$NUMBER && style_has 154 2 fg=$COMMENT && style_has 137 2 fg=$FG'
check "每条语句的第一行有 focus 色的 ▶，别的行没有" eval '[[ $(e2e_text 106 106 2) == ▶ && $(e2e_text 106 106 3) == ▶ && $(e2e_text 106 106 4) == " " ]] && style_has 106 2 fg=$FOCUS'
check "行号 dim，光标行 fg" eval 'style_has 109 2 fg=$DIM && style_has 109 3 fg=$FG'
check "光标所在的语句（第 2 行）是 row 底，另一条不是" eval 'style_has 115 3 bg=$ROW && style_has 115 2 bg=$PANE_BG'
key k
check "光标移到第 1 行：row 底跟着换到第 1 条" eval 'style_has 115 2 bg=$ROW && style_has 115 3 bg=$PANE_BG'

# ---- 按键：f<Space> 给编辑器，; 重复，: 是编辑器的命令行（§6.4、§6.8）
key 0; key f
check "f 等后续按键：待输入一块显示 f" eval '[[ $(pending) == f ]] || { echo "  pending: $(pending)"; false; }'
e2e_keys Space; sleep 0.3; key x
check "f<Space>x：同 nvim 删掉 f 跳到的空格，pane 没有关" eval '[[ $(line 1) == "selectcount(*), '"'a'"' from t where id > 10; -- n" && $(e2e_panes | awk "{ print \$1 }" | tr "\n" " ") == "0 1 2 " ]]'
key '\;'
check "; 重复 f<Space>：到下一个空格（第 1 行第 16 个字符）" cur_is 1,16
key 2; key d
check "2d：待输入一块显示 2d（showcmd）" eval '[[ $(pending) == 2d ]] || { echo "  pending: $(pending)"; false; }'
key Escape
key G; key o; typ "select 3"; key Escape
key gg; key 0; key :
check ":：COMMAND，命令行在内容区最后一行（tab 栏上方）" eval '[[ $(mode) == COMMAND && $(e2e_text 105 160 42) == "│:"* ]]'
typ 3; key Enter
check ":3 跳到第 3 行，回到 NORMAL" eval 'cur_is 3,1 && [[ $(mode) == NORMAL ]]'
key R
check "R：REPLACE，终端光标是下划线" eval '[[ $(mode) == REPLACE ]] && flag_is cursor_shape underline'
key Escape

# ---- 状态栏的模式与 VISUAL 附加信息（键位文字从 keymap 读）
key gg; key j; key 0; key v; key e
check "第 2 行 v e：VISUAL，附加信息 6 字符 · ↵ run" eval '[[ $(mode) == VISUAL && $(info) == "6 字符 · ↵ run" ]] || { echo "  $(bar)"; false; }'
key Escape; key V; key j
check "V j：V-LINE，2 行 · ↵ run" eval '[[ $(mode) == V-LINE && $(info) == "2 行 · ↵ run" ]] || { echo "  $(bar)"; false; }'
key Escape; key gg; key 0; key C-v; key j; key j; key l; key l; key l
check "C-v jj lll：V-BLOCK，3 行 × 4 列 · ↵ run" eval '[[ $(mode) == V-BLOCK && $(info) == "3 行 × 4 列 · ↵ run" ]] || { echo "  $(bar)"; false; }'
key Escape

# ---- 鼠标（§11、F3.6）
e2e_click 115 3; sleep 0.3
check "单击文字：光标到那里（第 2 行第 5 个字符），仍是 NORMAL" eval 'cur_is 2,5 && [[ $(mode) == NORMAL ]]'
key i; e2e_click 113 2; sleep 0.3
check "INSERT 下单击：光标移过去，仍是 INSERT" eval 'cur_is 1,3 && [[ $(mode) == INSERT ]]'
key Escape; key v; key l; e2e_click 112 4; sleep 0.3
check "VISUAL 下单击：回到 NORMAL" eval 'cur_is 3,2 && [[ $(mode) == NORMAL ]]'
e2e_drag 111 2 116 2; sleep 0.3
check "在文字上拖动：从按下处进入字符 VISUAL，选中 6 个字符" eval '[[ $(mode) == VISUAL && $(info) == "6 字符 · ↵ run" ]] || { echo "  $(bar)"; false; }'
key Escape

# ---- 粘贴（§11「粘贴」，照 nvim 的 vim.paste）
key gg; key 0
paste $'dd\r\nx'
check "NORMAL 下粘贴 dd↵x：当文字贴在光标后面（CRLF 换成 LF），不当命令执行，仍是 NORMAL" eval '[[ $(line 1) == sdd && $(line 2) == "xelectcount(*), '"'a'"' from t where id > 10; -- n" && $(mode) == NORMAL ]]'
check "贴完光标停在最后一个贴进去的字符上" cur_is 2,1
key u
check "整段粘贴是一个撤销步：u 一次恢复原样" eval '[[ $(line 1) == "selectcount(*), '"'a'"' from t where id > 10; -- n" && $(line 2) == "select 2 as b" ]]'

# ---- 文件（§11「文件」、§13）
sleep 1.5
check "改动 1s 后自动保存到 XDG_DATA_HOME/sqlmux/consoles/doraemon/console_1.sql，内容与缓冲区一致" eval '[[ -f $CF && $(cat "$CF") == "$(printf "selectcount(*), '"'a'"' from t where id > 10; -- n\nselect 2 as b\nselect 3")" ]] || { echo "  file: $(cat "$CF" 2>&1)"; false; }'
check "文件 0600，目录 0700" eval '[[ $(stat -f %Lp "$CF") == 600 && $(stat -f %Lp "$(dirname "$CF")") == 700 && $(stat -f %Lp "$DD/sqlmux/consoles") == 700 ]]'
key G; key dd; key C-s; sleep 0.3
check "G dd 后 C-s 立刻写盘（不等 1s）" eval '[[ $(cat "$CF") == "$(printf "selectcount(*), '"'a'"' from t where id > 10; -- n\nselect 2 as b")" ]] || { echo "  file: $(cat "$CF")"; false; }'
start -D "$DD"
check "重启：console_1 还是上次的内容" eval '[[ $(line 1) == "selectcount(*), '"'a'"' from t where id > 10; -- n" && $(line 2) == "select 2 as b" && -z $(line 3) ]]'

# ---- ex 命令（§11「命令行」）：编辑器不认识的交给 app 按面板的 ex 别名执行
key C-l; key :; typ foo; key Enter
check ":foo：toast「不支持的命令：foo」" toast_is "不支持的命令：foo"
key x; key :; typ w; key Enter
check ":w 立刻写盘" eval '[[ $(head -1 "$CF") == "electcount(*), '"'a'"' from t where id > 10; -- n" ]] || { echo "  file: $(head -1 "$CF")"; false; }'
key u; key C-s; sleep 0.3
chmod 500 "$(dirname "$CF")"; key x; key :; typ q; key Enter
check "写不进文件时 :q 不关 tab，toast「保存失败：…」" eval 'e2e_text 1 160 44 | grep -q "保存失败：.*console_1.sql.*permission denied" && [[ $(e2e_text 105 160 1) == *" console_1 "* ]]'   # 长 toast 盖住了下边框，e2e_panes 认不出 pane，看标题行
key :; typ qa; key Enter
check "写不进文件时 :qa 也不退出" eval 'running && e2e_text 1 160 44 | grep -q "保存失败："'
key Space; e2e_type x; sleep 0.3
check "写不进文件时 SPC x 也不关 pane" eval '[[ $(e2e_text 105 160 1) == *" console_1 "* ]]'
chmod 700 "$(dirname "$CF")"; key u
key :; typ q; key Enter
check ":q 关掉 console_1：不确认，② 随最后一个 tab 关掉，文件还在" eval '[[ $(e2e_text 1 160 1) != *console_1* && $(e2e_find ┐ 1) == "32 160" && -f $CF ]] && ! screen_has 丢弃'

# ---- [map.console.normal] / [map.console.visual]（§6.6）：只对 console，覆盖同名的 vim 键
mkdir -p "$D/map"; printf '[map.console.normal]\nL = "5l"\nJ = "j"\n[map.console.visual]\nL = "3l"\n' >"$D/map/config.toml"
start -C "$D/map" -D "$DD"; key C-l; key g g; key 0
key L
check "[map.console.normal] L = \"5l\"：右移 5 个字符" cur_is 1,6
key J
check "J = \"j\" 覆盖合并行：只是下移一行，没有合并" eval 'cur_is 2,6 && [[ $(line 1) == "selectcount(*), '"'a'"' from t where id > 10; -- n" ]]'
key 0; key v; key L
check "[map.console.visual] L = \"3l\"：选区扩到 4 个字符" eval '[[ $(info) == "4 字符 · ↵ run" ]] || { echo "  $(bar)"; false; }'
key Escape

# ---- 在 $EDITOR 中编辑（console.external，默认不绑键）：先写盘，外部编辑器改完回来重新载入，算一个撤销步
printf '#!/bin/sh\nprintf "select 42\\n" >>"$1"\n' >"$D/ed"; chmod +x "$D/ed"
e2e_start -D "$DD" "VISUAL=$D/ed $E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3
key C-l; key C-p; typ '>console.external'; key Enter; sleep 1
check "面板里执行「在 \$EDITOR 中编辑」：\$VISUAL 改过的文件重新载入，界面恢复" eval 'running && flag_is alternate_on 1 && [[ $(line 2) == "select 2 as b" && $(line 3) == "select 42" ]] || { echo "  line 3: $(line 3)"; false; }'
key u
check "u 一次撤掉外部编辑器的改动" eval '[[ $(line 2) == "select 2 as b" && -z $(line 3) ]]'

# ---- console 文件读不出来（cd4729f）：照常启动，这个 console 不打开，toast「读取失败：…」，不拿空内容覆盖它
mkdir -p "$D/bad-data/sqlmux/consoles/doraemon/console_1.sql"                # 是个目录
start -D "$D/bad-data"
check "console_1.sql 是个目录：照常启动，toast「读取失败：…is a directory」，② 没有 console（显示引导页）" eval 'running && e2e_text 1 160 44 | grep -q "读取失败：.*console_1.sql.*is a directory" && [[ $(e2e_text 105 160 1) != *console_1* ]] && e2e_plain | cut -c105-160 | grep -q 新建'
check "那个目录原样留着" test -d "$D/bad-data/sqlmux/consoles/doraemon/console_1.sql"

# ---- 用户映射展开的按键（§6.6）：展开出来的空格和 x 不能凑成 SPC x 关掉 pane（审查时的真 bug）
printf '[map.console.normal]\nQ = "iselect x<Esc>"\n' >"$D/map/config.toml"
start -C "$D/map"; key C-l; key Q
check "映射 Q = \"iselect x<Esc>\"：插入 select x，回到 NORMAL，pane 都在" eval '[[ $(line 1) == "select x" && $(mode) == NORMAL && $(e2e_panes | awk "{ print \$1 }" | tr "\n" " ") == "0 1 2 " ]]'

# ---- 粘贴的其余几种（vim.paste）
key 0; key i; paste XY
check "INSERT 下粘贴：插在光标处，仍是 INSERT" eval '[[ $(line 1) == "XYselect x" && $(mode) == INSERT ]]'
key Escape; key 0; key v; key l; paste Z
check "VISUAL 下粘贴：替换选区（XY → Z）" eval '[[ $(line 1) == "Zselect x" ]]'
key Escape; key 0; key v; key l; t send-keys -t t -l $'\e[200~\e[201~'; sleep 0.3
check "VISUAL 下空的粘贴：也删掉选区" eval '[[ $(line 1) == "elect x" ]]'
key Escape; key 0; key 3; key i; paste ab; key Escape
check "3i 粘贴 ab 再 esc：插入重复三遍" eval '[[ $(line 1) == "abababelect x" ]]'

# ---- 鼠标与撤销步、操作符（§11，审查时定）
key g g; key d G; key i; paste "$(seq -f 'select %g;' 60)"; key Escape; key g g; key 0
top_line() { e2e_text 107 109 2 | tr -d ' '; }
key i; typ Q; e2e_wheel 130 20 down; sleep 0.3; typ W; key Escape; key u; key g g
check "INSERT 下滚轮把光标带走：之前输入的 Q 和之后的 W 是两个撤销步（u 只撤掉 W）" eval '[[ $(line 1) == "Qselect 1;" && $(e2e_plain | cut -c111-160 | grep -c W) == 0 ]] || { echo "  line 1: $(line 1)"; false; }'
key g g; key d; e2e_wheel 130 20 down; sleep 0.3
check "按了 d 再滚轮：操作符取消，视图不滚" eval '[[ $(top_line) == 1 && $(pending) == · ]] || { echo "  top $(top_line), pending $(pending)"; false; }'
key j
check "之后的 j 只是移动（不是 dj）" eval '[[ $(line 1) == "Qselect 1;" && $(line 2) == "select 2;" && $(e2e_flag cursor_y) == 2 ]]'
key i; typ A; e2e_click $(( $(e2e_flag cursor_x) + 1 )) $(( $(e2e_flag cursor_y) + 1 )); sleep 0.3; typ B; key Escape; key u
check "INSERT 下点在光标原处：不断开撤销步（u 一次撤掉 A 和 B）" eval '[[ $(line 2) == "select 2;" ]] || { echo "  line 2: $(line 2)"; false; }'

# ---- 连接名做目录名：不能含 / 或 \，不能以 . 开头（§14）
mkdir -p "$D/bad"; printf '[[connection]]\nname = "a/b"\nengine = "postgres"\ndsn = "%s"\n' "$SQLMUX_TEST_PG" >"$D/bad/connections.toml"; chmod 600 "$D/bad/connections.toml"
e2e_start -C "$D/bad" "$E2E_BIN"; wait_for 3 screen_has '[e2e-exit 1]'
check "连接名 a/b：启动报错退出，错误里指出这个名字" eval 'screen_has "[e2e-exit 1]" && e2e_plain | tr -d "\n" | grep -q "name = \"a/b\"：不能含 / 或"'

e2e_done
