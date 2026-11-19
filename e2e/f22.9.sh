#!/usr/bin/env bash
# F22.9 命令行的 sqlserver:// / mssql:// URL（issue #19；specs/m22-menus-keys/task.md F22.9；tech-design §14「启动」）
# 这两种开头不再报「不认识的 URL」：开一个临时 SQL Server 连接，名字是 <库>@<主机>（没有 ?database= 就只有主机），连不上时是连接失败的报错、不是不认识；
# 真不认识的开头（bogus://）仍然退出 1，说明里列着 sqlserver://。SQL Server 的容器没起（清单外环境），用连不上的 127.0.0.1:1，所以只测到「认得」这一步。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
trap 'e2e_stop' EXIT
node() { e2e_plain | sed -n 4p | noicon | grep -oE "[▸▾] +[^ ]+" | sed 's/^[▸▾] *//'; }
node_is() { local n; n=$(node); [[ $n == "$1" ]] || { echo "  tree node '$n', want '$1'"; false; }; }
open_url() { e2e_start "$E2E_BIN '$1'"; wait_for 5 flag_is alternate_on 1; sleep 1.5; }

open_url 'mssql://sa:x@127.0.0.1:1/?database=foo'
check "mssql://…?database=foo：程序开着（没有退出）、树上的连接叫 foo@127.0.0.1" eval 'running && node_is foo@127.0.0.1'
check "屏幕上没有「不认识的 URL」" eval '! screen_has "不认识"'
key C-h; key l
check "展开它：报的是连接失败（地址 127.0.0.1:1 的拨号错误），名字是 foo@127.0.0.1" eval 'wait_for 25 eval "screen_has \"foo@127.0.0.1：\" && e2e_plain | grep -q \"127.0.0.1:1\""' || e2e_plain | tail -4 | cut -c1-200
check "报错之后程序还在、还是没有「不认识」" eval 'running && ! screen_has "不认识"'

open_url 'sqlserver://sa:x@127.0.0.1:1/?database=bar'
check "sqlserver://…?database=bar：连接叫 bar@127.0.0.1" eval 'running && node_is bar@127.0.0.1'

open_url 'sqlserver://sa:x@127.0.0.1:1/?Database=baz'
check "?Database=baz（大写 D，驱动也认）：名字照样是 baz@127.0.0.1" eval 'running && node_is baz@127.0.0.1'

open_url 'sqlserver://sa:x@127.0.0.1:1'
check "sqlserver:// 没有 database：名字只是主机 127.0.0.1" eval 'running && node_is 127.0.0.1'

e2e_start "$E2E_BIN 'bogus://x'"; wait_for 5 exited; sleep 0.5
check "bogus://：退出 1，说明里列着 sqlserver://（和 postgres://、mysql://、redis://）" eval 'screen_has "不认识的 URL bogus://" && screen_has "sqlserver://" && screen_has "[e2e-exit 1]"' || e2e_plain | head -5
e2e_done
