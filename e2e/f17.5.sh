#!/usr/bin/env bash
# F17.5 发布元数据（specs/m17-polish-exec/task.md F17.5）
# a. formula 和 npm 壳包的描述写四种引擎；b. --version：没有 ldflags 时用 Go 记下的 vcs.revision / vcs.time，都没有时只打版本、不打 (none, unknown)；
# d. formula 的 ldflags 带 -s -w。（c. npm 五个包都带 LICENSE 在 f11.3 里，它要 goreleaser 出包。）
# 这里用真的 go build：在一份本地 clone 里编（vcs 信息只有 git 检出里才有），带 / 不带 -buildvcs=false、带 / 不带 ldflags 各一份。
. "$(dirname "$0")/lib.sh"
cd "$E2E_ROOT" || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'rm -rf "$D"' EXIT
git clone -q --local "$E2E_ROOT" "$D/cl" || { echo "e2e: clone failed" >&2; exit 1; }
REV=$(git -C "$D/cl" rev-parse HEAD | cut -c1-7)
TIME=$(TZ=UTC git -C "$D/cl" log -1 --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ)
gb() { (cd "$D/cl" && go build "$@" ./cmd/sqlmux); }   # gb -o OUT [flags]

gb -o "$D/vcs"
check "git 检出里直接 go build（没有 ldflags）：--version 带 vcs 的 (<revision 前 7 位>, <提交时间>)，版本是 dev 或 Go 记的模块版本，不是 (none, unknown)" eval '[[ $("$D/vcs" --version) =~ ^sqlmux\ (dev|v[^\ ]+)\ \($REV,\ $TIME\)$ ]] || { "$D/vcs" --version; echo "  want (REV, TIME) = ($REV, $TIME)"; false; }'
gb -buildvcs=false -o "$D/novcs"
check "go build -buildvcs=false：只打版本 sqlmux dev（没有括号、没有 none / unknown）" eval '[[ $("$D/novcs" --version) == "sqlmux dev" ]] || { "$D/novcs" --version; false; }'
gb -buildvcs=false -ldflags "-X main.version=v1.2.3" -o "$D/v"
check "只注入版本（-X main.version=v1.2.3，没有 vcs）：sqlmux v1.2.3" eval '[[ $("$D/v" --version) == "sqlmux v1.2.3" ]] || { "$D/v" --version; false; }'
gb -ldflags "-X main.version=v1.2.3 -X main.commit=abc1234 -X main.date=2026-01-01T00:00:00Z" -o "$D/all"
check "ldflags 注入了版本、commit、日期（GoReleaser / brew 的做法）：用它们的，不用 vcs 的" eval '[[ $("$D/all" --version) == "sqlmux v1.2.3 (abc1234, 2026-01-01T00:00:00Z)" ]] || { "$D/all" --version; false; }'
gb -ldflags "-X main.version=v1.2.3 -X main.commit=abc1234" -buildvcs=false -o "$D/part"
check "只注入了 commit（没有日期）：括号里只有 commit，不打 unknown" eval '[[ $("$D/part" --version) == "sqlmux v1.2.3 (abc1234)" ]] || { "$D/part" --version; false; }'

# ---- a. 描述写五种引擎（F17.5 四种，F19.6 加了 SQL Server）
want="PostgreSQL, MySQL, SQLite, SQL Server and Redis"   # F19.6 added SQL Server
check "npm 壳包的 description：…for $want" eval 'python3 -c "import json,sys; d=json.load(open(\"npm/sqlmux/package.json\"))[\"description\"]; sys.exit(0 if d.endswith(\"$want\") else 1)"' || python3 -c 'import json; print(json.load(open("npm/sqlmux/package.json"))["description"])'
.github/scripts/tap-formula.sh v0.5.1 0000000000000000000000000000000000000000000000000000000000000000 abc1234 2026-10-18T00:00:00Z >"$D/formula.rb"
check "formula 的 desc：…for $want" eval 'grep -q "^  desc \".*for $want\"" "$D/formula.rb"' || grep desc "$D/formula.rb"
check "formula 的 ldflags 带 -s -w 和版本、commit、日期（brew 的 go build 自己不加 -s -w）" eval 'grep -q "ldflags = \"-s -w -X main.version=v#{version} -X main.commit=abc1234 -X main.date=2026-10-18T00:00:00Z\"" "$D/formula.rb"' || grep ldflags "$D/formula.rb"
check "formula 里没有「std_go_args adds -s -w」那句错的注释" eval '! grep -q "std_go_args adds" "$D/formula.rb"'
e2e_done
