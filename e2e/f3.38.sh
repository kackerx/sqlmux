#!/usr/bin/env bash
# F3.38 console 的系统剪贴板寄存器（specs/m3-console/task.md F3.38；tech-design §11「寄存器」）
# 系统剪贴板是 lib.sh 放在 PATH 最前面的假 pbcopy / pbpaste（clip 读它、set_clip 写它），测试不碰用户真实的剪贴板
# （AGENTS.md「隔离用户数据」）。最后一段 -P /bin：PATH 里没有这类工具，读写走 OSC 52 和 tmux 的 paste buffer（快速 SQL 的 C-y 在 f1.7.sh）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

paste() { t set-buffer -b e2e -- "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; key 0; }   # console 的内容换成 SQL，光标回到第 1 行行首
line() { e2e_text 111 159 $((1 + $1)) | sed 's/ *$//'; }                         # 第 N 行的文字（② console 从 x 111 起）
set_clip() { printf '%s' "$1" >"$E2E_TMP/clipboard"; }
clip_is() { local want=$1; wait_for 3 eval '[[ "$(clip; echo .)" == "$want." ]]' || { echo "  clip: [$(clip)], want [$want]"; false; }; }   # TEXT：剪贴板里是 TEXT（pbcopy 负载高时晚一点才写完）

start; key C-l
setsql $'select id, status\nfrom t_order\nwhere id < 5'

# ---- VISUAL 选中后 "+y：剪贴板里是选中的文字（task.md 验收）
key j; key 0; key v; key e; e2e_type '"+y'; sleep 0.5
check "VISUAL 选中 from、\"+y：剪贴板里是 from" clip_is from
key g g; e2e_type '"+yy'; sleep 0.5
check "\"+yy：剪贴板里是这一行（带换行，行类型）" clip_is "select id, status"$'\n'
key j; key 0; e2e_type '"*yiw'; sleep 0.5
check "\"* 等同 \"+：\"*yiw 复制 from" clip_is from
key y y; sleep 0.5
check "无名寄存器的 yy：不写系统剪贴板（同 vim 的 clipboard=）" eval '[[ "$(clip)" == from ]]'

# ---- "+p / "+P：粘出剪贴板的内容（字符类型放在光标后 / 前）
set_clip "XYZ"; key g g; key 0; e2e_type '"+p'; sleep 0.5
check "\"+p：剪贴板里的 XYZ 粘在光标后面" eval '[[ $(line 1) == "sXYZelect id, status" ]] || { echo "  $(line 1)"; false; }'
key u; key 0; e2e_type '"+P'; sleep 0.5
check "\"+P：粘在光标前面" eval '[[ $(line 1) == "XYZselect id, status" ]] || { echo "  $(line 1)"; false; }'
key u; key '$'; e2e_type '"+x'; sleep 0.5
check "\"+x：删掉的字（s）进剪贴板" eval 'clip_is s && [[ $(line 1) == "select id, statu" ]]'
key u; e2e_type '"ayy'; sleep 0.5
check "其他具名寄存器（\"a）：整条命令不执行，剪贴板和文字都不变" eval '[[ "$(clip)" == s && $(line 1) == "select id, status" ]]'

e2e_type '"add'; sleep 0.5
check "\"add：整条命令不执行，不删行" eval '[[ $(line 1) == "select id, status" && $(line 2) == "from t_order" ]]'
e2e_type '"'; key Escape; key 0; key x
check "\" 之后 esc：取消，接着的 x 照常只删一个字，剪贴板不变" eval '[[ $(line 1) == "elect id, status" && "$(clip)" == s ]]'
key u

# ---- "+yy 之后剪贴板换了别的内容："+p、再 p 贴出来的都是剪贴板的新内容（同 nvim，无名寄存器指着 "+）
key g g; e2e_type '"+yy'; sleep 0.5; set_clip "NEW"; key '$'; e2e_type '"+p'; sleep 0.5
check "\"+yy 后剪贴板改成 NEW，\"+p：贴出 NEW" eval '[[ $(line 1) == "select id, statusNEW" ]] || { echo "  $(line 1)"; false; }'
key p; sleep 0.5
check "再 p：贴出来的也是 NEW" eval '[[ $(line 1) == "select id, statusNEWNEW" ]] || { echo "  $(line 1)"; false; }'
key u; key u

# ---- 剪贴板为空时 "+p 什么都不做：出错语句的红 ▶ 还在，不触发自动保存
setsql "select nosuch"; key Enter; wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 1.5
mtime() { python3 -c 'import glob, os, sys; print(os.stat(glob.glob(sys.argv[1] + "/sqlmux/consoles/*/console_1.sql")[0]).st_mtime_ns)' "$E2E_TMP/data"; }
rm -f "$E2E_TMP/clipboard"; m0=$(mtime); e2e_type '"+p'; sleep 1.5
check "剪贴板为空时 \"+p：文字不变，红 ▶ 还在，console 文件没有重写" eval '[[ $(line 1) == "select nosuch" && $(mtime) == "$m0" ]] && style_has 106 2 fg=#f7768e'

# ---- "+p 的内容晚到、编辑器已经离开 NORMAL 时不贴（pbpaste 慢 1.5 秒）
printf '#!/bin/sh\nsleep 1.5\ncat "%s/clipboard"\n' "$E2E_TMP" >"$E2E_TMP/bin/pbpaste"; set_clip "LATE"
key '$'; e2e_type '"+p'; key a; sleep 2.5
check "\"+p 之后马上 a 进 INSERT：剪贴板的内容到了也不贴" eval '[[ $(line 1) == "select nosuch" ]] && mode_is INSERT || { echo "  $(line 1)"; false; }'
key Escape

# ---- WHERE 里 "add 也不清空条件（默认布局，① 里开 t_order）
key C-h; key C-p; e2e_type "@t_order"; sleep 0.3; key Enter; wait_for 8 settled
key /; e2e_type "id < 5"; key Enter; wait_for 8 settled
key /; key Escape; e2e_type '"add'; sleep 0.3
wq() { e2e_text 42 101 2 | sed 's/ *▾.*//; s/ *$//'; }                                    # ① 的 WHERE 输入框（默认布局，① 70 列）
check "WHERE 的 NORMAL 下 \"add：不清空条件，还在 WHERE 里" eval '[[ $(wq) == "id < 5" && $(bar) == *"-- editing WHERE --"* ]] || { echo "  [$(wq)]"; false; }'
key Escape

# ---- PATH 里没有 pbcopy 这类工具：走 OSC 52 写、OSC 52 查询读（tmux set-clipboard on 时回最新的 paste buffer）
start -P /bin; key C-l
t set -g set-clipboard on
setsql "select 42"; t delete-buffer -b e2e                                      # 只留 OSC 52 放进来的 buffer
e2e_type '"+yy'; sleep 0.8
check "没有剪贴板工具时 \"+yy：经 OSC 52 进了 tmux 的 buffer" eval '[[ "$(t show-buffer 2>/dev/null; echo .)" == "select 42"$'"'\n'"'. ]] || { t show-buffer 2>&1 | od -c | head -2; false; }'
t set-buffer -- "from-tmux"; key '$'; e2e_type '"+p'; sleep 0.8
check "\"+p：OSC 52 查询读到 tmux 的 buffer，粘进来" eval '[[ $(line 1) == "select 42from-tmux" ]] || { echo "  $(line 1)"; false; }'

e2e_done
