#!/usr/bin/env bash
# F21.2 默认连接 default_connection（specs/m21-cli-outer/task.md F21.2；tech-design §14「启动」）
# 三个连接 alpha / beta / gamma，beta 指向自建库（只有那里有 only_beta 这张表）：config.toml 写 default_connection = "beta"，
# 无 last.toml、不带参数：启动在 beta 上；--table only_beta 不给连接也开得出来；写了不存在的名字：toast、第一个连接（alpha）；没写：照旧第一个。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table only_beta (id int primary key)" -c "insert into only_beta values (1)"
mkdir -p "$D/c"
(umask 077; for n in alpha beta gamma; do
  dsn=$SQLMUX_TEST_PG; [[ $n == beta ]] && dsn=$E2E_DB
  printf '[[connection]]\nname = "%s"\nengine = "postgres"\ndsn = "%s"\n\n' "$n" "$dsn"; done >"$D/c/connections.toml")
conf() { printf 'language = "en"\n%s\n' "$1" >"$D/c/config.toml"; }
tstart() { e2e_start -C "$D/c" "$E2E_BIN $1"; wait_for 6 flag_is alternate_on 1; wait_for 15 settled; sleep 1; }
toast_text() { e2e_text 1 "$(e2e_flag pane_width)" $(( $(H) - 1 )); }

conf 'default_connection = "beta"'
tstart ""
check "default_connection = \"beta\"，不带参数（无 last.toml）：状态栏的连接是 beta，树里 beta 是当前连接" eval 'bar | grep -q "beta · " && ! bar | grep -q "alpha · " || { bar; false; }'
tstart "--table only_beta"
check "--table only_beta（不给连接，只有 beta 有这张表）：开出 only_beta tab" eval 'tabs_are 1 "1:only_beta*" && bar | grep -q "beta · " || { tabbar 1; bar; false; }'
tstart "gamma"
check "位置参数给了连接就用它：gamma 压过 default_connection" eval 'bar | grep -q "gamma · " || { bar; false; }'

conf 'default_connection = "nope"'
tstart ""
check "default_connection 指向不存在的名字：toast「default_connection: no connection nope」" eval 'toast_text | grep -q "default_connection: no connection nope" || { toast_text; false; }'
check "…退回第一个连接 alpha，程序照常启动" eval 'bar | grep -q "alpha · " && running || { bar; false; }'

conf ''
tstart ""
check "没写 default_connection：第一个连接 alpha，没有 toast" eval 'bar | grep -q "alpha · " && ! toast_text | grep -q "default_connection" || { bar; toast_text; false; }'
e2e_done
