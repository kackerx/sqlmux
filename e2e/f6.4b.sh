#!/usr/bin/env bash
# F6.4 的审查补充（specs/m6-config/task.md F6.4；tech-design §14「界面语言」）：language = "en" 下 80 列的命令面板范围行放不下时横向滚动、当前范围看得见（M22 F22.5 e）、
# 确认框（危险语句、未提交的事务）是英文、整个画面里没有汉字和全角标点，运行中 theme.switch 选到坏主题的 toast 是英文。
# 数据库返回的原文和 config.toml / keymaps.toml 的解析错误不在此列。写库的部分在自己建的库里做。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
top()  { e2e_plain | python3 -c 'import sys; print(next((i + 1 for i, l in enumerate(sys.stdin) if "┌─ Command palette" in l), ""))'; }
left() { local t; t=$(top); [[ -n $t ]] && e2e_find "┌─ Command palette" "$t" | cut -d' ' -f1; }

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
mkdir -p "$D/c/themes"
printf 'language = "en"\nconfirm_dangerous = true\n' >"$D/c/config.toml"
(umask 077; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/c/connections.toml")
printf 'pane_bgg = "#101010"\n' >"$D/c/themes/bad.toml"
paste() { t set-buffer -b e2e -- "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }
# 画面里的汉字和全角标点（数据库的原文不会有；表名、数据都是 ASCII）
cjk() { e2e_plain | python3 -c 'import sys,re; t=sys.stdin.read(); m=re.findall(r"[　-〿一-鿿＀-￯]", t); print("".join(m)); sys.exit(1 if m else 0)'; }
dbox() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
boxtext() { local g y; g=($(dbox)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1]; y < g[1] + g[3]; y++)); do e2e_text "${g[0]}" $((g[0] + g[2] - 1)) $y; done; }

start -x 80 -y 35 -C "$D/c"; sleep 0.5
check "引导页和主界面里没有汉字和全角标点" cjk
e2e_keys C-p; sleep 0.6
# 80 列放不下整行范围标签：横向滚动，当前范围始终看得见，被截掉的那头是 ‹N / N›（§12，M22 F22.5 e）
srow() { e2e_text "$(left)" $(( $(right) - 1 )) "$(tabs_y)"; }
check "80 列的命令面板：当前范围 All 看得见，后面是 Session \$ Win·Pane % …，右头截掉的画 N›" eval '[[ $(srow) == *"All   Session \$   Win·Pane %"* && $(srow) =~ [0-9]› ]] || { echo "  [$(srow)]"; false; }'
for i in 1 2 3 4 5 6 7 8; do e2e_keys Tab; sleep 0.2; done
check "Tab 走 8 次到 SQL：输入是 ;，「SQL ;」看得见，左头截掉的画 ‹N" eval 'input_is ";" && [[ $(srow) == *"Cmd >   SQL ;"* && $(srow) =~ ‹[0-9] ]] || { echo "  [$(srow)]"; false; }'
e2e_keys BTab; sleep 0.2
check "S-Tab 回到 Cmd：「Cmd >」看得见" eval '[[ $(srow) == *"Cmd >"* ]] || { echo "  [$(srow)]"; false; }'
check "命令面板里没有汉字" cjk
key Escape
key C-c
check "C-c 的 toast：Press C-c again to quit（没有全角标点）" eval 'screen_has "Press C-c again to quit" && cjk'
key C-l; key i
setsql "delete from t_log"; key Enter; sleep 0.8
check "危险语句确认框是英文：「1 dangerous statement(s) in this run:」，按钮 y Run / n Cancel，没有汉字" eval 'bt=$(boxtext); grep -q "1 dangerous statement(s) in this run:" <<<"$bt" && grep -q "y Run   n Cancel" <<<"$bt" && cjk'
key n; sleep 0.4
# ---- 未提交的事务：:qa 的确认框是两句话，各占一行，英文
setsql "begin"; key Enter; sleep 1
e2e_type ":"; sleep 0.3; e2e_type qa; key Enter; sleep 0.8
check "未提交的事务：:qa 弹确认框，英文，「Uncommitted transaction in doraemon」，没有汉字" eval 'bt=$(boxtext); grep -q "Uncommitted transaction in doraemon" <<<"$bt" && cjk || { echo "$bt"; false; }'
check "确认框放得下 80 列" eval 'g=($(dbox)); (( g[2] <= 80 ))'
key n; sleep 0.4

# ---- 运行中 theme.switch 选到坏主题：toast 是英文（启动时的主题错误才是中文）
e2e_keys C-p; sleep 0.3; e2e_type ">switch theme"; sleep 0.5; key Enter; sleep 0.5
g=($(dbox)); ib=; isel=
for ((i = 3; i < g[3] - 1; i++)); do y=$((g[1] + i)); [[ $(e2e_text $((g[0] + 2)) $((g[0] + g[2] - 3)) $y | sed "s/ *$//") == bad ]] && ib=$i; [[ $(e2e_style $((g[0] + 3)) $y) == *bg=#364a82* ]] && isel=$i; done
for ((j = isel; j < ib; j++)); do key Down; done; for ((j = isel; j > ib; j--)); do key Up; done
key Enter; sleep 0.8
check "选到 bad 主题（有错别字的 token）：toast 是英文，没有汉字" eval 'tt=$(e2e_text 1 80 $(( $(H) - 1 ))); cjk && [[ $tt == *[A-Za-z]* ]] && ! screen_has "主题"' || { e2e_text 1 80 $(( $(H) - 1 )); false; }
echo "  toast: $(e2e_text 1 80 $(( $(H) - 1 )))"

e2e_done
