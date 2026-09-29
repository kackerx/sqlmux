#!/usr/bin/env bash
# F3.11 console 的 schema 下拉框（specs/m3-console/task.md F3.11；tech-design §8.6）
# 标题栏的退让在 f0.2；这里测下拉框的打开与选择，以及选的 schema 在真实 PG 上怎么生效（search_path 的比较与重设）。
# 自建库（库名 sqlmux_e2e_<pid>）；默认布局，② console_1 的文字从 x 111 起。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f311-$$ DB=sqlmux_e2e_$$ PK=#73daca
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
psql "$E2E_DB" -q -c "create table public.agent (who text)" -c "insert into public.agent values ('public')"   # 同名表：查错 schema 会读到这张
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }
done_run() { wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.3; }
run() { key Enter; done_run; }
title() { local g; g=($(geom "$1")); e2e_text "${g[0]}" $((g[0] + g[2] - 1)) "${g[1]}"; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
rrow() { local g; g=($(geom 3)); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + $1)) | sed 's/^ //; s/ *$//'; }
logs() { local g y; g=($(geom 3)); for ((y = g[1] + 1; y < g[1] + g[3] - 2; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ //; s/ *$//'; done | sed '/^$/d'; }
psql_n() { psql "$E2E_DB" -At -c "$1"; }

start -C "$D/own"
check "② 标题：<库名>.<schema> ▾，默认是树当前的 public" eval '[[ $(title 2) == *" $DB.public ▾  ▶ run  ↵ ─┐" ]] || { echo "  $(title 2)"; false; }'
key C-l; key g; key s
check "gs：打开下拉框，输入直接进过滤框（COMMAND）" eval '[[ -n $(pop) && $(bar) == *" COMMAND " ]]'
g=($(pop)); x=$(e2e_find "$DB.public" 1 | cut -d" " -f1)
check "下拉框在按钮下方、与按钮左对齐，右边框和 ② 的右边框对齐" eval '[[ ${g[0]} == $x && ${g[1]} == 2 && $(( g[0] + g[2] - 1 )) == 160 ]] || { echo "  box ${g[*]}, button x $x"; false; }'
check "当前的 public 用 pk 色标出；系统 schema 不列" eval 'y=$(for y in $(seq 3 12); do [[ $(e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y) == *" public "* ]] && echo $y; done | head -1); [[ -n $y ]] && c=$(e2e_find public $y | tr " " "\n" | awk -v l=${g[0]} "\$1 > l { print; exit }") && style_has $c $y fg=$PK && ! screen_has pg_catalog && ! screen_has information_schema'
e2e_type agen; sleep 0.3; key Enter
check "过滤出 agentable、↵：标题变成 $DB.agentable ▾" eval '[[ -z $(pop) && $(title 2) == *" $DB.agentable ▾  ▶ run  ↵ ─┐" ]]'
setsql "select count(*) as n from agent"
run
check "选了 agentable：不带 schema 的 agent 查的是 agentable.agent" eval '[[ $(rrow 3) == *" $(psql_n "select count(*) from agentable.agent")"* ]] || { echo "  $(rrow 3)"; false; }'
check "SET search_path 不进日志、不占结果 tab" eval '! logs | grep -qi "search_path" && [[ $(tabbar 3) != *"#"*"#"* ]]'
setsql "show search_path"; run
check "SET 的是所选 schema，后面接着建连时的原始 search_path" eval '[[ $(rrow 3) == *agentable*public* ]] || { echo "  $(rrow 3)"; false; }'

# ---- 两个 console 各选各的，交替执行（§8.6「执行方式」：只在不一致时 SET）
key C-h; key c; setsql "select who from agent"
check "新 console 默认用树当前的 schema（public），和 console_1 互不影响" eval '[[ $(title 1) == *" $DB.public ▾"* && $(title 2) == *" $DB.agentable ▾"* ]]'
run
check "console_2（public）：读到 public.agent" eval '[[ $(rrow 3) == *public* ]] || { echo "  $(rrow 3)"; false; }'
key C-l; setsql "select count(*) as n from agent"; run
check "回到 console_1（agentable）：又读 agentable.agent" eval '[[ $(rrow 3) == *" $(psql_n "select count(*) from agentable.agent")"* ]] || { echo "  $(rrow 3)"; false; }'
key C-h; run
check "再回 console_2：public.agent" eval '[[ $(rrow 3) == *public* ]]'

# ---- console 里自己 set search_path：只到这次执行结束，下一次照下拉框重新 SET
key C-l; setsql $'set search_path to public;\nselect count(*) as n from agent;'
key V; key G; run
check "同一次执行里 set search_path to public 之后：这次读的是 public.agent（1 行）" eval '[[ $(rrow 3) == *" 1"* ]] || { echo "  $(rrow 3)"; false; }'
setsql "select count(*) as n from agent"; run
check "下一次执行：照下拉框重新 SET 成 agentable" eval '[[ $(rrow 3) == *" $(psql_n "select count(*) from agentable.agent")"* ]] || { echo "  $(rrow 3)"; false; }'

# ---- 只在事务外记住 search_path（§8.6，92df28b）
# 事务里做的 SET 会被 rollback 撤掉：这时不能当它还在。console_2（public）先跑一次，Main 记下 public
N=$(psql_n "select count(*) from agentable.agent")
key C-h; setsql "select who from agent"; run
key C-l; setsql $'begin;\nselect count(*) as n from agent;'; key V; key G; run      # 事务里 SET 成 agentable
check "事务里执行：读的是 agentable.agent" eval '[[ $(rrow 3) == *" $N"* ]] || { echo "  $(rrow 3)"; false; }'
setsql "rollback"; run
setsql "select count(*) as n from agent"; run
check "rollback 撤掉了事务里的 SET：下一次照样重新 SET，仍读 agentable.agent（不是 public 的 1 行）" eval '[[ $(rrow 3) == *" $N"* ]] || { echo "  $(rrow 3)"; false; }'
# 出错的事务里（TxStatus E）跳过 SET 直接执行，所以 rollback 能执行到
setsql $'begin;\nselect 1/0;'; key V; key G; run
key C-h; setsql "select who from agent"; run                                     # console_2（public）要 SET，但事务已中止
check "中止的事务里另一个 schema 的 console 执行：不先 SET，语句自己报「事务已中止」" eval '[[ $(logs | tail -1) == *"select who from agent  ERROR: current transaction is aborted"* ]] && ! logs | tail -2 | grep -qi "search_path" || { logs | tail -2; false; }'
key C-l; setsql "rollback"; run
check "回到 console_1 执行 rollback：执行到了，Main 恢复" eval '[[ $(logs | tail -1) == *"rollback  ROLLBACK"* ]] || { logs | tail -2; false; }'
setsql "select count(*) as n from agent"; run
check "之后照常按下拉框 SET：读 agentable.agent" eval '[[ $(rrow 3) == *" $N"* ]] || { echo "  $(rrow 3)"; false; }'

# ---- 选的 schema 被删掉：catalog 重新加载后，console 退回树当前的 schema（§8.6）
setsql "create schema gone"; run                                               # 在 console 里建：DDL 成功后重新加载 catalog
key g; key s; e2e_type gone; sleep 0.3; key Enter
check "选了新建的 schema gone" eval '[[ $(title 2) == *" $DB.gone ▾"* ]] || { echo "  $(title 2)"; false; }'
setsql "drop schema gone"; run                                                 # DDL 成功后重新加载 catalog
check "drop schema gone 之后：console 退回树当前的 public，不弹提示" eval '[[ $(title 2) == *" $DB.public ▾"* ]] || { echo "  $(title 2)"; false; }'

# ---- 按钮让掉时（100 宽）gs 照样打开，框的右边对齐本 pane 的右边框，不伸进别的 pane
start -x 100 -C "$D/own"; key C-l; key g; key s
check "100 宽：标题里没有下拉按钮，gs 打开的框右边对齐 ② 的右边框" eval 'g=($(pop)); [[ $(title 2) != *"$DB."* && -n ${g[0]} && $(( g[0] + g[2] - 1 )) == 100 && ${g[0]} -ge $(geom 2 | cut -d" " -f1) ]] || { echo "  box ${g[*]} / ② $(geom 2)"; false; }'
key Escape

e2e_done
