#!/usr/bin/env bash
# F3.13 自动配对括号与引号（specs/m3-console/task.md F3.13；tech-design §7.9「自动配对」）
# 配对规则由单测覆盖；这里在真实终端里按键：WHERE、快速 SQL、console 的 INSERT，撤销，配置 autopairs = false。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
typ() { e2e_type "$1"; sleep 0.3; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
wcur() { echo $(( $(e2e_flag cursor_x) - 41 )); }                              # WHERE 输入框里光标前有几个字符（SOLO 布局，文字从 x 42 起；cursor_x 从 0 起）
where_is() { [[ $(where_in) == "$1" && $(wcur) == "$2" ]] || { echo "  WHERE '$(where_in)' cursor $(wcur), want '$1' cursor $2"; false; }; }
line() { e2e_text 111 159 $(($1 + 1)) | sed 's/ *$//'; }                       # console 第 N 行（默认布局）
ccur() { echo $(( $(e2e_flag cursor_x) - 110 )); }                              # console 里光标前有几个字符
mode() { bar | awk '{ print $NF }'; }

# ---- WHERE（SOLO：① 独占右侧）
SOLO=1 start; open_table t_order
key /; typ "status = '"
check "WHERE 输入 status = '：补上右引号，光标在两个引号中间" where_is "status = ''" 10
key Escape; key Escape; key /; clear_in; typ "status = 'done'"   # F3.39：两次 esc 才回表格
check "接着输入 done'：右引号只是跳过去，得到 status = 'done'，光标在最后" where_is "status = 'done'" 15
clear_in; typ "don't"
check "don't 里的引号前面是字母：不配对" where_is "don't" 5
clear_in; typ "id in ("; key BSpace
check "空的一对括号中间按退格：两边一起删" where_is "id in" 6   # where_in 去掉了行尾空格
clear_in; typ "status = '"; typ d; sleep 0.5; key Enter
check "status = '█' 里接受补全 'done'：连光标后的引号一起换掉，得到 status = 'done'█" where_is "status = 'done'" 15
clear_in; paste "id in (1,"
check "粘贴 id in (1,：原样插入，不配对（7ed8bda）" where_is "id in (1," 9
clear_in; typ "f("; paste ")"
check "光标后正好是 ) 时粘贴 )：也不跳过，照样插入" where_is "f())" 3
key Escape

# ---- 快速 SQL
key C-p; typ ";select count("
check "快速 SQL 输入 count(：补上 )" eval 'e2e_plain | grep -q "; *select count()"'
key Escape; key Escape

# ---- console 的 INSERT：一起撤销；带次数的重放照样配对
start; key C-l; key i; typ "select count("
check "console 输入 count(：得到 count(█)" eval '[[ $(line 1) == "select count()" && $(ccur) == 13 ]] || { echo "  $(line 1) / $(ccur)"; false; }'
typ "*) from t where note = 'a"
check "接着输入 *) 跳过右括号，引号照样配对" eval '[[ $(line 1) == "select count(*) from t where note = '"'a'"'" ]] || { echo "  $(line 1)"; false; }'
key Escape; key u
check "一次 u 撤掉这次 INSERT 的全部内容（配对补上的也算在内）" eval '[[ -z $(line 1) ]] || { echo "  $(line 1)"; false; }'
key 3; key i; typ "("; key Escape
check "3i( 再 esc：重放时照样配对，得到 ((()))" eval '[[ $(line 1) == "((()))" ]] || { echo "  $(line 1)"; false; }'
key u; key R; typ "("; key Escape
check "REPLACE 模式不配对" eval '[[ $(line 1) == "(" ]] || { echo "  $(line 1)"; false; }'
key u; key g g; key d G; key 3; key o; typ "("; key Escape
check "3o( 再 esc：每一行各配一对" eval '[[ $(line 2) == "()" && $(line 3) == "()" && $(line 4) == "()" ]] || { echo "  $(line 2) / $(line 3) / $(line 4)"; false; }'
key :; typ "s/a/("
check ": 命令行不配对" eval '[[ $(e2e_text 105 160 42) == "│:s/a/("*  && $(e2e_text 105 160 42) != *"()"* ]]'
key Escape

# ---- autopairs = false：哪里都不配对
mkdir -p "$D/off"; printf 'autopairs = false\n' >"$D/off/config.toml"
start -C "$D/off"; key C-l; key i; typ "count('"
check "autopairs = false：console 里不配对" eval '[[ $(line 1) == "count('"'"'" ]] || { echo "  $(line 1)"; false; }'
key Escape
SOLO=1 start -C "$D/off"; open_table t_order; key /; typ "status = '"
check "autopairs = false：WHERE 里不配对" where_is "status = '" 10
key Escape
mkdir -p "$D/bad"; printf 'autopairs = "yes"\n' >"$D/bad/config.toml"
e2e_start -C "$D/bad" "$E2E_BIN"; wait_for 3 screen_has '[e2e-exit 1]'
check "autopairs 写成字符串：启动报错退出，指出这一项" eval 'screen_has "[e2e-exit 1]" && e2e_plain | tr -d "\n" | grep -q autopairs'

e2e_done
