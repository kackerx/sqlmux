#!/usr/bin/env bash
# F1.5 WHERE 补全与历史 / 收藏（specs/m1-browse/task.md F1.5；tech-design §9.7「WHERE 补全的细节」「C-r 历史 / 收藏下拉」、§14 state.json、§7.8）
# 只读，用共用库的 t_order；state.json 放在脚本自己的临时目录（-S），重启时沿用。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); ST=$D/state
trap 'e2e_stop; rm -rf "$D"' EXIT
SJ=$ST/sqlmux/state.json
. "$(dirname "$0")/palette.sh"
SELECT=#364a82
typ() { e2e_type "$1"; sleep 0.4; }
edit() { key /; clear_in; }                                   # 进入 WHERE 输入并清空
run() { edit; typ "$1"; key Enter; wait_for 8 settled; sleep 0.2; }   # 执行一条 WHERE
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }       # 浮层（补全列表 / 历史下拉）的 X Y W H
# items：浮层里每一行「文字|选中 0/1」；补全列表没有过滤框，从第 2 行起就是候选
items() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do
    printf '%s|%s\n' "$(e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | tr -s ' ' | sed 's/^ //; s/ $//')" "$(style_has $((g[0] + g[2] - 2)) $y bg=$SELECT >/dev/null && echo 1 || echo 0)"; done; }   # 选中看行尾的留白
names() { items | cut -d'|' -f1 | awk '{ print $1 }' | tr '\n' ' '; }
picked() { items | awk -F'|' '$2 == 1 { print $1 }'; }
picked_i() { items | awk -F'|' '$2 == 1 { print NR }'; }
item_y() { local g y; g=($(pop)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do [[ $(e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y) == "$1"* ]] && { echo $y; return; }; done; }

start -S "$ST"; open_table t_order

# ---- 补全：列名 / 关键字模式（§9.7）
edit; typ sta
check "输入 sta：输入框下方从前缀所在列弹出列表，status 在列，右侧注释是类型；仍是 INSERT" eval 'g=($(pop)); [[ ${g[0]} == 42 && ${g[1]} == 3 && $(items | head -1) == "status order_status|1" ]] && mode_is INSERT || { echo "  [$(pop)] $(items | tr "\n" ,)"; false; }'
key Enter
check "↵ 接受已选中的第一项（F1.14）：输入框是 status（不补空格），列表关闭" eval '[[ $(where_in) == status && -z $(pop) ]] && flag_is cursor_x 47'
clear_in; typ "id > 0 and "
check "前缀为空（刚敲了空格）：不弹" eval '[[ -z $(pop) ]]'
typ n
check "列名一组在前（note），关键字在后（not、null），注释「关键字」；首字符必须是 n（F1.14：没有 amount、is null）" eval 'i=$(items | cut -d"|" -f1); [[ $(tr "\n" , <<<"$i") == "note text,not 关键字,null 关键字," ]] || { echo "  $(tr "\n" , <<<"$i")"; false; }'
clear_in; typ stat; key Escape; key Enter; wait_for 8 settled
check "esc 关掉列表之后 ↵ 直接执行查询（stat 不是列：报错）" eval 'mode_is NORMAL && [[ $(e2e_text 35 159 4) == *"stat"*"does not exist"* ]]'

# ---- 补全：取值模式（枚举、布尔；§9.7）
edit; typ "status = "
check "status = ：直接列出枚举值，按 enumsortorder（pending queued running done failed），注释「值」" eval '[[ $(names) == "pending queued running done failed " && $(items | head -1) == "pending 值|1" ]] || { echo "  $(items | tr "\n" ,)"; false; }'
key C-n
check "弹出时 pending 已选中，第一次 C-n 就移到 queued（F1.14）" eval '[[ $(picked) == "queued 值" ]] || { echo "  selected: $(picked)"; false; }'
i0=$(picked_i); key Down; i1=$(picked_i); key Up; i2=$(picked_i); key C-n; i3=$(picked_i); key C-p; i4=$(picked_i)
check "↓ / ↑ / C-n / C-p 各移一项" eval '(( i1 == i0 + 1 && i2 == i0 && i3 == i0 + 1 && i4 == i0 )) || { echo "  $i0 $i1 $i2 $i3 $i4"; false; }'
key C-p
key Enter
check "明确选中之后 ↵ 是接受：插入带引号的 'pending'，仍在输入" eval '[[ $(where_in) == "status = '"'pending'"'" ]] && mode_is INSERT || { echo "  $(where_in)"; false; }'
clear_in; typ "status = "; key C-p
check "第一项上 C-p：绕到最后一项 failed（F1.14）" eval '[[ $(picked) == "failed 值" ]] || { echo "  selected: $(picked)"; false; }'
clear_in; typ "status = "; key Enter
check "status = 直接 ↵：接受已选中的 pending" eval '[[ $(where_in) == "status = '"'pending'"'" ]] || { echo "  $(where_in)"; false; }'
clear_in; typ "status = 'd"; key Tab; key Enter
check "status = 'd 再 Tab ↵：替换半截值成 'done'" eval '[[ $(where_in) == "status = '"'done'"'" ]] || { echo "  $(where_in)"; false; }'
clear_in; typ "status in ('done', "
check "in 列表里逗号之后也弹枚举值" eval '[[ $(names) == "pending queued running done failed " ]]'
clear_in; typ "paid = "
check "paid = ：列出 true / false" eval '[[ $(names) == "true false " ]] || { echo "  $(names)"; false; }'
key Escape
check "esc 第一次：只关列表，仍在输入" eval '[[ -z $(pop) ]] && mode_is INSERT && [[ $(where_in) == "paid =" ]]'
key Escape
check "esc 第二次：退出输入" mode_is NORMAL
edit; typ "status = "
px() { local g; g=($(pop)); echo $((g[0] + 3)); }
e2e_move $(px) $(item_y done); sleep 0.3
check "鼠标悬停就选中" eval '[[ $(picked) == "done 值" ]]'
e2e_click $(px) $(item_y running); sleep 0.3
check "点击就接受" eval '[[ $(where_in) == "status = '"'running'"'" && -z $(pop) ]] || { echo "  $(where_in)"; false; }'
e2e_move 100 30
clear_in; typ "note = 'sta"; np1=$(pop)
clear_in; typ '"sta'; np2=$(pop)
clear_in; typ "id > 0 -- sta"; np3=$(pop)
check "字符串里、带引号的标识符里、注释里都不弹" eval '[[ -z $np1 && -z $np2 && -z $np3 ]]'
clear_in; typ "$(printf 'st\xe3\x80\x80a')"; typ "$(printf ' \xc2\xa0 \xef\xbc\x91')"; typ x
check "全角空格、不换行空格、全角数字：不卡死，照常输入" eval 'running && [[ $(where_in) == "$(printf "st\xe3\x80\x80a \xc2\xa0 \xef\xbc\x91x")" ]] || { echo "  $(where_in)"; false; }'
key Escape; key Escape

# ---- 历史（§9.7）：↵ 执行的非空 WHERE 连同 ORDER / LIMIT 记一条；相同的去重挪到最前
run ""; run "id < 50"
key gl; key C-n; key Enter; wait_for 8 settled      # LIMIT 500
run "status = 'done'"
key go; typ amount; key Enter; wait_for 8 settled   # ORDER amount ↑
run "id < 50"
edit; key C-r
hist() { items | cut -d'|' -f1 | sed -E 's/ [0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}$//' | tr '\n' ,; }   # 去掉右侧的时间
check "C-r：打开历史下拉，状态栏 COMMAND；只有「历史」一组，新的在前（id < 50、status = 'done'、id < 50，最早的是出错的 stat）" eval 'mode_is COMMAND && [[ $(hist) == "历史,id < 50,status = '"'done'"',id < 50,stat," ]] || { echo "  $(items | tr "\n" ,)"; false; }'
check "历史项右侧是时间 MM-DD HH:MM" eval 'items | sed -n 2p | grep -qE " [0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}\|"'
check "空的 WHERE 不记" eval '! items | grep -q "^ *[0-9][0-9]-"'
check "打开时选中第一项" eval '[[ $(picked) == "id < 50 "* ]]'
key C-f
check "C-f：收藏这一项，出现「收藏」组，右侧是和默认不同的 ORDER / LIMIT（amount ↑ · 500）" eval '[[ $(items | head -2 | cut -d"|" -f1 | tr "\n" ,) == "收藏,id < 50 amount ↑ · 500," ]] || { echo "  $(items | tr "\n" ,)"; false; }'
key Escape
check "esc 关闭，输入框的文字保留（空）" eval '[[ -z $(pop) && -z $(where_in) ]] && mode_is INSERT'
typ sta; key C-r
check "用输入框的文字过滤：sta 只剩 status = 'done' 和 stat" eval '[[ $(hist) == "历史,status = '"'done'"',stat," ]] || { echo "  $(items | tr "\n" ,)"; false; }'
key Enter; wait_for 8 settled
check "↵ 应用：WHERE、ORDER、LIMIT 都换成这一项的（status = 'done'、id ↑、500），从第 1 页开始" eval '[[ $(where_in) == "status = '"'done'"'" ]] && qb_has "ORDER id $ASC" && qb_has "LIMIT 500" && qb_has "PAGE 1/3" && mode_is NORMAL'
run "id < 50"; run "status = 'done'"   # 后一条和刚应用的那项内容相同（status = 'done'、id ↑、500）
check "内容相同的去重、挪到最前：status = 'done' 在第一条，它只有一条" eval 'n=$(python3 -c "import json,sys; h=json.load(open(sys.argv[1]))[\"tables\"][\"doraemon/public.t_order\"][\"history\"]; print(len(h), h[0][\"where\"], sum(1 for e in h if e[\"where\"] == \"status = '"'done'"'\" and e.get(\"limit\") == 500))" "$SJ"); [[ $n == *" status = '"'done'"' 1" ]] || { echo "  $n"; false; }'
key Escape
e2e_click $(e2e_find ▾ 2 | cut -d" " -f1) 2; sleep 0.4
check "NORMAL 下点击 WHERE 行右端的 ▾：先进入输入，再打开下拉" eval 'mode_is COMMAND && [[ -n $(pop) && $(bar) == *"-- editing WHERE --"* ]]'
clear_in; sleep 0.3
e2e_click 60 $(item_y "status = "); sleep 0.5; wait_for 8 settled
check "点击一行就应用这一项" eval '[[ $(where_in) == "status = '"'done'"'" && -z $(pop) ]] && qb_has "LIMIT 500"'

# ---- state.json（§14）：0600、按「连接名/schema.表」、面板的最近使用；重启后收藏和最近使用都在
check "state.json 在 \$XDG_STATE_HOME/sqlmux/，权限 0600，目录里没有留下临时文件" eval '[[ $(stat -f %Lp "$SJ") == 600 && $(ls "$ST/sqlmux") == state.json ]]'
check "按 doraemon/public.t_order 存；最近使用记着表 public.t_order" eval 'python3 -c "import json,sys; s=json.load(open(sys.argv[1])); assert \"doraemon/public.t_order\" in s[\"tables\"]; assert {\"kind\": \"table\", \"id\": \"public.t_order\"} in s[\"recent\"]" "$SJ"'
start -S "$ST"
pal ""
check "重启后命令面板的最近使用仍在最前（t_order）" eval '[[ $(list | head -1 | cut -d" " -f1) == t_order ]] || { echo "  $(list | head -3 | tr "\n" ,)"; cat "$SJ"; false; }'
key Enter; wait_for 8 settled
edit; key C-r
check "重启后收藏仍在" eval '[[ $(items | head -2 | cut -d"|" -f1 | tr "\n" ,) == "收藏,id < 50 amount ↑ · 500," ]] || { echo "  $(items | tr "\n" ,)"; false; }'
key C-f
check "C-f 取消收藏：「收藏」组消失" eval '[[ $(items | head -1) != 收藏* ]]'
key Escape; key Escape

# 历史每张表最多 50 条：先放 50 条，再执行 1 条新的
python3 - "$SJ" <<'PY'
import json, sys
p = sys.argv[1]; s = json.load(open(p))
s["tables"]["doraemon/public.t_order"]["history"] = [{"where": f"id <> {i}", "at": "2026-09-01T00:00:00+08:00"} for i in range(50)]
json.dump(s, open(p, "w"))
PY
chmod 600 "$SJ"
start -S "$ST"; open_table t_order; run "id > 5990"
check "历史满 50 条后再执行一条：仍是 50 条，新的在最前，最旧的被挤掉" eval 'r=$(python3 -c "import json,sys; h=json.load(open(sys.argv[1]))[\"tables\"][\"doraemon/public.t_order\"][\"history\"]; print(len(h), h[0][\"where\"], h[-1][\"where\"])" "$SJ"); [[ $r == "50 id > 5990 id <> 48" ]] || { echo "  $r"; false; }'

# 写坏的 state.json：改名为 state.json.broken，toast 报错，以空状态照常运行
printf '{' >"$SJ"
start -S "$ST"
check "写坏的 state.json：启动有 toast 报错，程序照常运行" eval 'running && e2e_text 60 160 $(( $(H) - 1 )) | grep -q "state.json"'
check "原文件改名为 state.json.broken（内容不动）" eval '[[ -f $ST/sqlmux/state.json.broken && $(cat "$ST/sqlmux/state.json.broken") == "{" ]]'
open_table t_order; run "id = 1"
check "之后照常记录：新的 state.json 只有这一条历史，权限 0600" eval 'r=$(python3 -c "import json,sys; print(len(json.load(open(sys.argv[1]))[\"tables\"][\"doraemon/public.t_order\"][\"history\"]))" "$SJ"); [[ $r == 1 && $(stat -f %Lp "$SJ") == 600 ]]'

e2e_done
