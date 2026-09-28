#!/usr/bin/env bash
# F2.4 时间分段（specs/m2-edit/task.md F2.4；tech-design §10.2「分段」「文字与分段的同步」「现在」「鼠标」）
# 解析、循环、按天数夹取由单测覆盖（timepick_test）；这里在真实终端里按键、点 ▴、滚滚轮，看输入框的文字怎么变，
# 以及「◷ 现在」写进库里的值。在自建库里做。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
SELECT=#364a82
psql_n() { psql "$E2E_DB" -At -c "$1"; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
goto() { key g g; key 0; [[ $2 -gt 1 ]] && key $(($2 - 1)) j; local n; n=$(e2e_text 35 $(( $(col_x "$1") - 1 )) $(hy) | tr -cd '│' | wc -m); n=$((n - 1)); [[ $n -gt 0 ]] && key "$n" l; true; }
tbox() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }        # 时间浮层的 X Y W H
segs() { local g; g=($(tbox)); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + 2)) | tr -s ' ' | sed 's/^ //; s/ $//'; }   # 分段行的文字
up_x() { local g; g=($(tbox)); e2e_find ▴ $((g[1] + 1)) | tr ' ' '\n' | awk -v l=${g[0]} '$1 > l' | sed -n "${1}p"; }   # N：第 N 段（年月日时分秒）上方 ▴ 的列
cur_seg() { local g x; g=($(tbox)); for x in $(seq $((g[0] + 1)) $((g[0] + g[2] - 2))); do style_has $x $((g[1] + 2)) bg=$SELECT >/dev/null && printf '%s' "$(e2e_text $x $x $((g[1] + 2)))"; done; }   # 当前段（select 底）的文字
input() { e2e_text $(col_x created_at) $(( $(col_x created_at) + 24 )) $(row_y 1) | sed 's/ *▾.*//; s/ *$//'; }   # 第 1 行 created_at 输入框的文字
orig=$(psql_n "select created_at from t_order where id = 1")

start -C "$D/own"; open_table t_order; wait_for 8 settled
goto created_at 1; key Enter
check "timestamptz 列进入编辑：出现分段（年 月 日 时 分 秒，和文字一致），当前段是年" eval '[[ $(segs) == "2026 - 09 - 01 00 : 01 : 00" && $(input) == "$orig" && $(cur_seg) == 2026 ]] || { echo "  [$(segs)] [$(input)] [$(cur_seg)] want $orig"; false; }'

# ---- Tab / S-Tab 切换当前段，↑ / ↓ 加减；只改写文字里对应的那一段；各段在自己的范围内循环、不进位
key Tab
check "Tab：当前段到月" eval '[[ $(cur_seg) == 09 ]]'
key Up
check "在月份上 ↑：月 09 → 10，文字同步（时区后缀 +00 不变）" eval '[[ $(segs) == "2026 - 10 - 01 00 : 01 : 00" && $(input) == "2026-10-01 00:01:00+00" ]] || echo "  $(input)"'
key Up; key Up; key Up
check "12 月再 ↑：绕回 01，年不进位" eval '[[ $(input) == "2026-01-01 00:01:00+00" ]] || { echo "  $(input)"; false; }'
key BTab
check "S-Tab：回到年" eval '[[ $(cur_seg) == 2026 ]]'
key BTab
check "年上再 S-Tab：绕到最后一段秒" eval '[[ $(cur_seg) == 00 && $(input) == "2026-01-01 00:01:00+00" ]] && key Down && [[ $(input) == "2026-01-01 00:01:59+00" ]] || { echo "  $(input)"; false; }'

# ---- 鼠标：点 ▴ / ▾ 加减，在某一段上滚滚轮加减，点击某一段选中它
g=($(tbox)); dx=$(up_x 3)                                                       # 日是第 3 段
e2e_click "$dx" $((g[1] + 1)); sleep 0.3
check "点日上方的 ▴：日 01 → 02" eval '[[ $(input) == "2026-01-02 00:01:59+00" ]] || { echo "  $(input)"; false; }'
e2e_click "$dx" $((g[1] + 3)); sleep 0.3
check "点 ▾：日 02 → 01" eval '[[ $(input) == "2026-01-01 00:01:59+00" ]] || { echo "  $(input)"; false; }'
hx=$(up_x 4)                                                                    # 时是第 4 段
e2e_wheel "$hx" $((g[1] + 2)) down; sleep 0.3
check "在时上滚轮向下：时 00 → 23（循环，日不变）；不提交、表格不滚" eval '[[ $(input) == "2026-01-01 23:01:59+00" ]] && mode_is INSERT && [[ $(e2e_text 35 45 $(row_y 1)) == *" 1 │"* ]] || { echo "  $(input)"; false; }'
e2e_wheel "$hx" $((g[1] + 2)) up; sleep 0.3
check "滚轮向上：23 → 00" eval '[[ $(input) == "2026-01-01 00:01:59+00" ]] || { echo "  $(input)"; false; }'
e2e_click "$hx" $((g[1] + 2)); sleep 0.3
check "点击时这一段：选中它" eval '[[ $(cur_seg) == 00 ]] && key Up && [[ $(input) == "2026-01-01 01:01:59+00" ]]'

# ---- 点击浮层外部：提交并退出
e2e_click 60 30; sleep 0.4
check "点击浮层外部：提交并退出编辑（NORMAL），文字记成修改" eval 'mode_is NORMAL && e2e_text $(col_x created_at) $(( $(col_x created_at) + 18 )) $(row_y 1) | grep -q "2026-01-01 01:01"'

# ---- 「◷ 现在」：填进本地当前时间，精确到秒、带本地偏移，写法和 PG 的 ISO 输出一致；不结束编辑
goto created_at 2; key Enter; key C-n; key Enter
now_re='^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}[+-][0-9]{2}(:[0-9]{2})?$'
v=$(e2e_text $(col_x created_at) $(( $(col_x created_at) + 24 )) $(row_y 2) | sed 's/ *▾.*//; s/ *$//')
off=$(date +%z | sed -E 's/^([+-][0-9]{2})00$/\1/; s/^([+-][0-9]{2})([0-9]{2})$/\1:\2/')
check "C-n 选 ◷ 现在、↵：输入框是本地现在（YYYY-MM-DD HH:MM:SS${off}，无小数秒），仍在编辑" eval '[[ $v =~ $now_re && $v == *"$off" && $v == "$(date "+%Y-%m-%d %H:")"* ]] && mode_is INSERT || { echo "  [$v] offset $off"; false; }'
key Enter; key C-s; wait_for 8 eval '[[ $(qb) == *已保存* ]]'
check "保存后：库里 id 2 的 created_at 就是这个时刻（PG 按这个写法解析出同一个时间点）" eval '[[ $(psql_n "select extract(epoch from created_at)::bigint from t_order where id = 2") == $(psql_n "select extract(epoch from '"'$v'"'::timestamptz)::bigint") ]] || { echo "  DB $(psql_n "select created_at from t_order where id = 2") want $v"; false; }'

# ---- 解析不了的值（NULL 进来是空文字）：分段区变暗，↑ / ↓ 不起作用
goto deleted_ 1; key Enter                                                     # 表头截成了 deleted_
check "deleted_at 是 NULL：分段区显示 ----，↑ 不起作用（文字仍为空）" eval '[[ $(segs) == "---- - -- - -- -- : -- : --" ]] && key Up && [[ $(segs) == "---- - -- - -- -- : -- : --" && -z $(e2e_text $(col_x deleted_) $(( $(col_x deleted_) + 6 )) $(row_y 1) | sed "s/ *▾.*//; s/ *│.*//; s/ *$//") ]]'
key Escape

# ---- 矮终端：放不下整个时间浮层时整个不画，键盘照样能加减
start -y 11 -C "$D/own"; open_table t_order; wait_for 8 settled                 # 上下都放不下 6 行高的浮层
cx=$(col_x created_at); goto created_at 1; key Enter
check "11 行高、编辑第 1 行的 created_at：上下都放不下，时间浮层不画" eval 'mode_is INSERT && [[ -z $(tbox) ]]'
key Tab; key Up
check "键盘照样加减：Tab 到月、↑，文字变成 10 月" eval 'e2e_text $cx $((cx + 24)) $(row_y 1) | grep -q "2026-10-01 00:01:00+00" || e2e_text $cx $((cx + 24)) $(row_y 1)'
key Escape

e2e_done
