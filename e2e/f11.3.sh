#!/usr/bin/env bash
# F11.3 npm：壳包加平台子包（specs/m11-distribution/task.md F11.3；tech-design §15「发布与分发」）
# 在打了 tag 的干净 clone 里 goreleaser 出压缩包，npm/build.mjs 做七个包（和 release.yml 的 npm job 同一步），
# 然后检查包的内容、用 npm（本地、-g、--ignore-scripts）和 pnpm 装本机平台的两个包，跑 sqlmux：--version、退出码和输出透传、
# 缺子包时的提示、TUI 能开能退能缩放。真发布后的 `npx sqlmux@<tag>` / `pnpm dlx sqlmux` 要 npm 上有包，由决策者在 tag 上确认。
# 需要 goreleaser（GORELEASER=路径）、node、npm、pnpm；时间在 goreleaser 的编译上（约 1 分钟）。
. "$(dirname "$0")/lib.sh"
D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-npm.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
. "$(dirname "$0")/release.sh"
rel_clone || exit 1
rel_run || exit 1
cd "$SRC"
v=${TAG#v}
os=$(uname -s | tr A-Z a-z); cpu=$(uname -m | sed 's/x86_64/x64/;s/aarch64/arm64/')
RAW=$(rel_native)
js() { node -e "const p = JSON.parse(require('fs').readFileSync(process.argv[1], 'utf8')); process.stdout.write(String(eval(process.argv[2])))" "$1" "$2"; }

# ---- build.mjs
check "build.mjs 没有参数：退出码 2 和用法" eval 'node npm/build.mjs 2>"$D/err"; [[ $? -eq 2 ]] && grep -q usage "$D/err"'
mkdir -p "$D/few"; cp dist/sqlmux_${v}_darwin_arm64.tar.gz "$D/few/"
check "build.mjs 缺一个平台的压缩包：失败并说是哪个平台" eval 'node npm/build.mjs $TAG "$D/few" "$D/few-out" 2>"$D/few-err"; [[ $? -ne 0 ]] && grep -q "darwin_amd64\|linux_" "$D/few-err"'
check "build.mjs 在路径带空格的目录里也能出包" eval 'mkdir -p "$D/with space" && cp -R npm "$D/with space/npm" && cp LICENSE "$D/with space/LICENSE" && node "$D/with space/npm/build.mjs" $TAG dist "$D/sp out" && [[ -x "$D/sp out/sqlmux-$os-$cpu/bin/sqlmux" && -f "$D/sp out/sqlmux/bin/sqlmux.js" ]]'
node npm/build.mjs "$TAG" dist "$D/out" || exit 1
check "做出七个包：sqlmux 和六个 sqlmux-<os>-<cpu>（F18.4：加 win32-arm64、win32-x64）" eval '[[ $(ls "$D/out" | tr "\n" " ") == "sqlmux sqlmux-darwin-arm64 sqlmux-darwin-x64 sqlmux-linux-arm64 sqlmux-linux-x64 sqlmux-win32-arm64 sqlmux-win32-x64 " ]]'
check "tag 带不带 v 结果一样（version 都是 ${v}）" eval 'node npm/build.mjs "${v}" dist "$D/out2" && [[ $(js "$D/out2/sqlmux/package.json" p.version) == ${v} && $(js "$D/out/sqlmux/package.json" p.version) == ${v} ]]'

# ---- 包的内容
S=$D/out/sqlmux
check "壳包：name sqlmux、version ${v}、bin → bin/sqlmux.js、engines node >=18、license MIT" eval '[[ $(js $S/package.json "[p.name, p.version, p.bin.sqlmux, p.engines.node, p.license].join(\" \")") == "sqlmux ${v} bin/sqlmux.js >=18 MIT" ]]'
check "壳包：optionalDependencies 正好是六个 @kackerx/sqlmux-<os>-<cpu>，版本都是 ${v}，没有 dependencies" eval '[[ $(js $S/package.json "Object.entries(p.optionalDependencies).map(([k, x]) => k + \"@\" + x).sort().join(\" \")") == "@kackerx/sqlmux-darwin-arm64@${v} @kackerx/sqlmux-darwin-x64@${v} @kackerx/sqlmux-linux-arm64@${v} @kackerx/sqlmux-linux-x64@${v} @kackerx/sqlmux-win32-arm64@${v} @kackerx/sqlmux-win32-x64@${v}" && $(js $S/package.json "p.dependencies === undefined") == true ]]'
check "壳包：没有 scripts（不用 postinstall 下载，pnpm 10 / --ignore-scripts 才能用）" eval '[[ $(js $S/package.json "p.scripts === undefined") == true ]]'
check "壳包：bin/sqlmux.js 带 node shebang、可执行" eval '[[ -x $S/bin/sqlmux.js && $(head -1 $S/bin/sqlmux.js) == "#!/usr/bin/env node" ]]'
for pl in darwin-arm64 darwin-x64 linux-arm64 linux-x64; do
  P=$D/out/sqlmux-${pl}
  check "子包 ${pl}：name、version、os / cpu 只限这一个平台、没有 scripts" eval '[[ $(js $P/package.json "[p.name, p.version, p.os.join(), p.cpu.join(), p.scripts === undefined].join(\" \")") == "@kackerx/sqlmux-${pl} ${v} ${pl%%-*} ${pl#*-} true" ]]'
  check "子包 ${pl}：bin/sqlmux 可执行、是这个平台的二进制" eval 'f=$(file $P/bin/sqlmux); [[ -x $P/bin/sqlmux ]] && case ${pl} in darwin-arm64) [[ $f == *"Mach-O"*arm64* ]];; darwin-x64) [[ $f == *"Mach-O"*x86_64* ]];; linux-arm64) [[ $f == *"ELF"*aarch64* ]];; linux-x64) [[ $f == *"ELF"*x86-64* ]];; esac'
done
packfiles() { (cd "$1" && npm pack --dry-run --json 2>/dev/null) | node -e 'const p = JSON.parse(require("fs").readFileSync(0, "utf8")); console.log(p[0].files.map((f) => f.path).sort().join(" "))'; }
check "npm pack：壳包只有 LICENSE、package.json 和 bin/sqlmux.js（F17.5）" eval '[[ $(packfiles $S) == "LICENSE bin/sqlmux.js package.json" ]] || { packfiles $S; false; }'
check "npm pack：子包只有 LICENSE、package.json 和 bin/sqlmux（F17.5）" eval '[[ $(packfiles $D/out/sqlmux-$os-$cpu) == "LICENSE bin/sqlmux package.json" ]]'
for pl in win32-arm64 win32-x64; do
  P=$D/out/sqlmux-${pl}
  check "子包 ${pl}：name、version、os win32 / cpu 只限这一个平台、没有 scripts（F18.4）" eval '[[ $(js $P/package.json "[p.name, p.version, p.os.join(), p.cpu.join(), p.scripts === undefined].join(\" \")") == "@kackerx/sqlmux-${pl} ${v} win32 ${pl#*-} true" ]]'
  check "子包 ${pl}：bin/sqlmux.exe 是 Windows 的 PE 可执行文件、架构对，npm pack 里只有 LICENSE、package.json、bin/sqlmux.exe" eval 'f=$(file $P/bin/sqlmux.exe); [[ $f == *"PE32+"* ]] && case ${pl} in win32-arm64) [[ $f == *"Aarch64"* || $f == *"ARM64"* ]];; win32-x64) [[ $f == *"x86-64"* ]];; esac && [[ $(packfiles $P) == "LICENSE bin/sqlmux.exe package.json" ]] || { echo "  $f"; packfiles $P; false; }'
done
check "壳包的 bin/sqlmux.js 在 win32 上找 sqlmux.exe，别的平台找 sqlmux（F18.4）" eval 'grep -q "win32\" ? \"sqlmux.exe\" : \"sqlmux\"" $S/bin/sqlmux.js'
check "七个包里都有 LICENSE，和仓库根的一样（MIT 要附副本，F17.5）" eval 'for d in sqlmux sqlmux-darwin-arm64 sqlmux-darwin-x64 sqlmux-linux-arm64 sqlmux-linux-x64 sqlmux-win32-arm64 sqlmux-win32-x64; do cmp -s LICENSE "$D/out/$d/LICENSE" || { echo "  $d: LICENSE missing or different"; exit 1; }; done'
check "壳包的 package.json：files 有 LICENSE，description 写五种引擎（F17.5，F19.6 加了 SQL Server）" eval '[[ $(js $S/package.json "p.files.join()") == "bin,LICENSE" && $(js $S/package.json "p.description") == *"PostgreSQL, MySQL, SQLite, SQL Server and Redis" ]]'

# ---- 装、跑
mkdir -p "$D/tgz"; for d in "$S" "$D/out/sqlmux-$os-$cpu"; do (cd "$d" && npm pack --pack-destination "$D/tgz" >/dev/null 2>&1); done
SH=$(ls "$D/tgz"/sqlmux-${v}.tgz) SUB=$(ls "$D/tgz"/kackerx-sqlmux-$os-$cpu-${v}.tgz)
want="sqlmux $TAG ($(git rev-parse --short HEAD), $(TZ=UTC git log -1 --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ))"
mkdir "$D/n1" && (cd "$D/n1" && npm init -y >/dev/null 2>&1 && npm i --offline --no-audit --no-fund "$SH" "$SUB" >"$D/n1.log" 2>&1)
NB=$D/n1/node_modules/.bin/sqlmux
check "npm i（离线、本机平台两个包）：node_modules/.bin/sqlmux --version 是 '$want'" eval '[[ $($NB --version) == "$want" ]] || { cat "$D/n1.log"; false; }'
check "npm exec -- sqlmux --version（npx 同一条路径）" eval '[[ $(cd "$D/n1" && npm exec --offline -- sqlmux --version) == "$want" ]]'
check "输出和退出码透传：未知参数时 stdout / stderr / 退出码和直接跑二进制一样" eval '"$RAW" --bogus >"$D/ro1.txt" 2>"$D/re1.txt"; r1=$?; "$NB" --bogus >"$D/ro2.txt" 2>"$D/re2.txt"; r2=$?; [[ $r1 -ne 0 && $r1 == $r2 ]] && cmp -s "$D/ro1.txt" "$D/ro2.txt" && cmp -s "$D/re1.txt" "$D/re2.txt" || { echo "  raw $r1 / npm $r2"; head -3 "$D/re2.txt"; false; }'
check "子命令原样传过去：sqlmux keys 的输出和直接跑一样" eval '"$RAW" keys >"$D/k1" 2>&1; "$NB" keys >"$D/k2" 2>&1; [[ -s $D/k1 ]] && cmp -s "$D/k1" "$D/k2"'
mkdir "$D/n2" && (cd "$D/n2" && npm init -y >/dev/null 2>&1 && npm i --offline --no-audit --no-fund --ignore-scripts "$SH" "$SUB" >"$D/n2.log" 2>&1)
check "npm i --ignore-scripts：照样能跑" eval '[[ $("$D/n2/node_modules/.bin/sqlmux" --version) == "$want" ]]'
mkdir "$D/g"; npm i -g --offline --no-audit --no-fund --prefix "$D/g" "$SH" "$SUB" >"$D/g.log" 2>&1
check "npm i -g：<prefix>/bin/sqlmux --version（符号链接的 bin）" eval '[[ $("$D/g/bin/sqlmux" --version) == "$want" ]] || { cat "$D/g.log"; false; }'
mkdir "$D/n3" && (cd "$D/n3" && npm init -y >/dev/null 2>&1 && npm i --offline --no-audit --no-fund --omit=optional "$SH" >"$D/n3.log" 2>&1)
"$D/n3/node_modules/.bin/sqlmux" --version >"$D/o3" 2>"$D/e3"; r3=$?
check "缺子包（--omit=optional）：退出码 1，stdout 空，stderr 说没有这个平台的二进制并给出办法" eval '[[ $r3 -eq 1 && ! -s $D/o3 ]] && grep -q "no binary for $os-$cpu" "$D/e3" && grep -q "@kackerx/sqlmux-$os-$cpu" "$D/e3" && grep -q "omit=optional" "$D/e3" && grep -q "go install" "$D/e3" || { echo "  exit $r3: $(cat $D/e3)"; false; }'
mkdir "$D/p1" && (cd "$D/p1" && pnpm init >/dev/null 2>&1 && pnpm add --offline "$SH" "$SUB" >"$D/p1.log" 2>&1)
check "pnpm add（默认不跑脚本）：node_modules/.bin/sqlmux --version" eval '[[ $("$D/p1/node_modules/.bin/sqlmux" --version) == "$want" ]] || { tail -8 "$D/p1.log"; false; }'
check "pnpm exec sqlmux --version" eval '[[ $(cd "$D/p1" && pnpm exec sqlmux --version) == "$want" ]]'

# ---- TUI：从 npm 装出来的 sqlmux 能开、能缩放、能退（退出码 0）
. "$(dirname "$0")/palette.sh"
quit_app() { e2e_type ":"; sleep 0.4; e2e_type qa; e2e_keys Enter; wait_for 8 exited; sleep 0.5; }
for how in npm pnpm; do
  [[ $how == npm ]] && E2E_BIN=$NB || E2E_BIN=$D/p1/node_modules/.bin/sqlmux
  start; sleep 1
  check "$how 装的：TUI 打开（alternate screen、侧栏里有 doraemon）" eval 'flag_is alternate_on 1 && e2e_plain | grep -q doraemon'
  e2e_resize 100 30; sleep 0.8
  check "$how 装的：窗口缩到 100x30 后界面跟着重排（每行 100 列）" eval '[[ $(e2e_widths | sed "/^0$/d" | sort -u | tr "\n" " ") == "100 " ]]'
  quit_app
  check "$how 装的：:qa 退出，退出码 0，退出后回到普通屏幕" eval 'screen_has "[e2e-exit 0]" && flag_is alternate_on 0'
done
e2e_done
