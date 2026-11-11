#!/usr/bin/env bash
# F22.6 关闭一批对 query tab、窄 pane 里的固定 tab、ascii 的固定标记（specs/m22-menus-keys/task.md F22.6；tech-design §7.8「tab 栏」，0b36e59）
# 一个 pane 里：未保存的表、关了会丢事务的 query、正在执行的 query 在 SPC wo 时留下，toast 数对，不弹确认；
# 80 列、6 个 tab 要滚动的 pane 里固定一个：当前 tab 和它的图钉画全，点看得见的每个 tab 都切到它；ascii 下固定标记是 ^。自建库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
mkdir -p "$D/rw" "$D/a"
(umask 077; printf '[[connection]]\nname = "rw"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/rw/connections.toml"; cp "$D/rw/connections.toml" "$D/a/")
printf 'icons = "ascii"\n' >"$D/a/config.toml"
PIN=$(printf '\xef\x90\xb5')
tabraw() { local g; g=($(geom "${1:-1}")); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + g[3] - 2)); }
tabbar() { tabraw "$1" | noicon | sed 's/  */ /g'; }
title() { local g; g=($(geom "${1:-1}")); e2e_text "${g[0]}" $((g[0] + g[2] - 1)) "${g[1]}" | noicon; }
toast_has() { e2e_cap | grep -qF -- "$1"; }
paste() { t set-buffer -b e2e -- "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; key 0; }
newq() { pal ">新建 query"; sleep 0.4; key Enter; sleep 0.8; }
active() { psql "$E2E_DB" -At -c "select count(*) from pg_stat_activity where datname = current_database() and query like '%$1%' and state <> 'idle' and pid <> pg_backend_pid()"; }

# ---- 一个 pane 里：表、query 混在一起（§10.5：事务归连接，关掉的是这个连接上最后一个 query 时才算丢事务、才会问）
idle_tx() { psql "$E2E_DB" -At -c "select count(*) from pg_stat_activity where datname = current_database() and state = 'idle in transaction'"; }
SOLO=1 start -C "$D/rw"; open_table t_user; wait_for 8 settled; key l; key Enter; e2e_type X; sleep 0.2; key Enter; sleep 0.4
newq; setsql "begin; update t_order set note = note where id = 1"; key g g; key V; key G; key Enter; sleep 1.5
open_table t_log; wait_for 8 settled; open_table t_order; wait_for 8 settled
check "（准备）① 里 t_user（改了没存）、query_1（这个连接唯一的 query，开着事务）、t_log、t_order" eval '[[ $(tabbar) == *t_user*query_1*t_log*t_order* && $(idle_tx) == 1 ]]' || { tabbar; idle_tx; }
key Space; key w; key o; sleep 1
check "t_order 上 SPC wo：t_log 关了；t_user（未保存）、query_1（会丢事务）留下" eval '[[ $(tabbar) == *t_user*query_1*t_order* && $(tabbar) != *t_log* ]]' || tabbar
check "一条 toast「2 个 tab 没关：有未保存的修改、未提交的事务或正在执行」，没有弹确认，事务还在" eval 'toast_has "2 个 tab 没关：有未保存的修改、未提交的事务或正在执行" && ! e2e_plain | grep -qE "回滚|丢弃" && [[ $(idle_tx) == 1 ]]' || e2e_plain | tail -4
_e2e_kill

SOLO=1 start -C "$D/rw"; open_table t_user; wait_for 8 settled
newq; setsql "select pg_sleep(30) as f22sleep"; key Enter; sleep 1
open_table t_order; wait_for 8 settled
check "（准备）query_1 在执行 pg_sleep" eval '[[ $(active f22sleep) -ge 1 && $(tabbar) == *t_user*query_1*t_order* ]]' || tabbar
key Space; key w; key o; sleep 1
check "t_order 上 SPC wo：t_user 关了，正在执行的 query_1 留下，toast「1 个 tab 没关：…」，pg_sleep 还在跑" eval '[[ $(tabbar) == *query_1*t_order* && $(tabbar) != *t_user* && $(active f22sleep) -ge 1 ]] && toast_has "1 个 tab 没关：有未保存的修改、未提交的事务或正在执行"' || { tabbar; e2e_plain | tail -3; }
_e2e_kill

# ---- 80 列、tab 栏要滚动
SOLO=1 start -x 80 -C "$D/rw"
for tb in t_user t_order t_sku t_log t_event t_order_item; do open_table "$tb"; wait_for 8 settled; done
open_table t_log; wait_for 8 settled; key Space; key w; key P; sleep 0.6
check "80 列：t_log 固定后，tab 栏里它在第一个（1:t_log）、图钉画全" eval '[[ $(tabraw) == *"1:"*"t_log $PIN"* ]]' || tabraw
bad=; for tb in t_user t_order t_sku t_event t_order_item t_log; do
  y=$(( $(e2e_flag pane_height) - 2 )); x=$(e2e_find "$tb" "$y" | tr ' ' '\n' | awk '$1 > 34 { print; exit }')
  [[ -z $x ]] && continue
  e2e_click $((x + 1)) "$y"; sleep 0.5; [[ $(title) == *" $tb "* || $(title) == *" $tb"* ]] || bad+="$tb->$(title | tr -s ' ─') "
done
check "点 tab 栏上看得见的每个 tab（图钉算进了宽度）：pane 标题都切到点的那个" eval '[[ -z $bad ]] || { echo "  $bad"; tabraw; false; }'
_e2e_kill

# ---- ascii
SOLO=1 start -C "$D/a"; open_table t_user; wait_for 8 settled; open_table t_order; wait_for 8 settled
open_table t_user; wait_for 8 settled; key Space; key w; key P; sleep 0.6
check "ascii：固定的 t_user 名字后面是 ^（不是 * 也不是图钉），其后才是当前 tab 的 *" eval '[[ $(tabraw) == *"1:"*"t_user ^"* && $(tabraw) != *"$PIN"* ]]' || tabraw
e2e_done
