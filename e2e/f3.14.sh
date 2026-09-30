#!/usr/bin/env bash
# F3.14 输入框删词（specs/m3-console/task.md F3.14；tech-design §7.9「删词」）
# 切词规则由单测覆盖；这里在真实终端里按 C-w、M-BS（ESC DEL）、C-u：WHERE、面板、单元格编辑、console 的 INSERT 和 : 命令行。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

typ() { e2e_type "$1"; sleep 0.3; }
mbs() { e2e_keys M-BSpace; sleep 0.3; }                                        # Option / Alt + 退格
line() { e2e_text 111 159 $(($1 + 1)) | sed 's/ *$//'; }
cmdline() { e2e_text 106 159 42 | sed 's/ *$//'; }
mode() { bar | awk '{ print $NF }'; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
cell() { local x; x=$(col_x note); e2e_text "$x" $((x + 20)) $(( $(grid_y) + 10 )) | sed 's/ *▾.*//; s/ *│.*//; s/ *$//'; }   # 第 10 行 note 格（编辑中右边有选项浮层的 ▾）

# ---- WHERE（SOLO）
SOLO=1 start; open_table t_order
key /; typ "status = 'done' and id"
key C-w
check "WHERE 里 C-w：删掉前一个词 id" eval '[[ $(where_in) == "status = '"'done'"' and" ]] || { echo "  $(where_in)"; false; }'
mbs
check "WHERE 里 M-BS：同 C-w，再删掉 and" eval '[[ $(where_in) == "status = '"'done'"'" ]] || { echo "  $(where_in)"; false; }'
key C-u
check "WHERE 里 C-u：删到行首" eval '[[ -z $(where_in) ]]'
key Escape; key Escape                                                         # F3.39：INSERT → WHERE 的 NORMAL → 表格

# ---- COLS 的过滤框、PAGE 输入框（§7.9：所有单行输入框）
key g; key c; key /; typ "user id"; key C-w
check "COLS 过滤框里 C-w：删掉 id" eval 'e2e_plain | grep -qE " user +1/10 │" && ! e2e_plain | grep -qE " user id +1/10 │"'
key Escape; key Escape; key Escape
key g; key p; typ 12; key C-w
check "PAGE 输入框里 C-w：删掉整串数字" qb_has "PAGE []/"
key Escape

# ---- 面板
pal "t_order foo"
key C-w
check "面板里 C-w：删掉 foo" input_is "t_order"
mbs
check "面板里 M-BS：删掉 t_order" input_is ""
typ "abc def"; key C-u
check "面板里 C-u：删到行首" input_is ""
key Escape

# ---- 单元格编辑（第 10 行的 note 是 "note 10"）
key g g; key 9 j; key 7 l                                                  # id 列往右 7 列是 note
key Enter; key C-w
check "单元格刚进入编辑、文字还是全选：C-w 同退格，清空全部" eval '[[ -z $(cell) ]] || { echo "  $(cell)"; false; }'
typ "hello world"; key C-w
check "单元格里 C-w：删掉 world" eval '[[ $(cell) == hello ]] || { echo "  $(cell)"; false; }'
mbs
check "单元格里 M-BS：删掉 hello" eval '[[ -z $(cell) ]] || { echo "  $(cell)"; false; }'
key Escape; key Escape

# ---- console 的 INSERT 和 : 命令行
start; key C-l; key i; typ "select foo.bar"
key C-w
check "console INSERT 里 C-w：同 vim，删掉 bar，留下 foo." eval '[[ $(line 1) == "select foo." ]] || { echo "  $(line 1)"; false; }'
mbs
check "console INSERT 里 M-BS：同 C-w，删掉 .（一串非关键字字符）" eval '[[ $(line 1) == "select foo" ]] || { echo "  $(line 1)"; false; }'
key Escape; mbs
check "NORMAL 下 M-BS 不做事" eval '[[ $(line 1) == "select foo" && $(mode) == NORMAL ]]'
key u
check "M-BS 和 C-w 一样先断开撤销步：u 只恢复删掉的那一截" eval '[[ $(line 1) == "select foo." ]] || { echo "  $(line 1)"; false; }'
key /; typ "foo bar"; key C-w
check "/ 搜索行里 C-w：删掉 bar" eval '[[ $(cmdline) == "/foo" ]] || { echo "  $(cmdline)"; false; }'
key Escape
key :; typ "s/foo bar"; key C-w
check ": 命令行里 C-w：删掉 bar" eval '[[ $(cmdline) == ":s/foo" ]] || { echo "  $(cmdline)"; false; }'
mbs
check ": 命令行里 M-BS：删掉 foo" eval '[[ $(cmdline) == ":s/" ]] || { echo "  $(cmdline)"; false; }'
key Escape

e2e_done
