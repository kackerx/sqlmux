#!/usr/bin/env bash
# F11.1 --version 和 GoReleaser（specs/m11-distribution/task.md F11.1；tech-design §15「发布与分发」）
# 不碰界面：--version 的输出（ldflags 注入、没注入、不读配置不建文件）、just build 的注入、just release-check（snapshot 出八个二进制）、
# 在打了 tag 的干净 clone 里 goreleaser release --skip=publish（六个 tar.gz、两个 zip、校验和、Linux 链接方式、版本和 changelog）、release.yml 的结构。
# 真 tag 的 workflow 要推送后由决策者确认。需要 goreleaser（GORELEASER=路径）、just；时间在 goreleaser 的编译上（约 1 分钟）。
. "$(dirname "$0")/lib.sh"
D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-rel.XXXXXX"); trap 'rm -rf "$D"' EXIT
. "$(dirname "$0")/release.sh"
rel_clone || exit 1
cd "$SRC"
SHORT=$(git rev-parse --short HEAD)
DATE_UTC=$(TZ=UTC git log -1 --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ)

# ---- --version
go build -ldflags "-X main.version=v1.2.3 -X main.commit=abc1234 -X main.date=2026-10-11" -o "$D/inj" ./cmd/sqlmux
check "注入的版本：--version 是 'sqlmux v1.2.3 (abc1234, 2026-10-11)'" eval '[[ $("$D/inj" --version) == "sqlmux v1.2.3 (abc1234, 2026-10-11)" ]]'
check "-version 同样；一行、退出码 0、stderr 空" eval '[[ $("$D/inj" -version) == "sqlmux v1.2.3 (abc1234, 2026-10-11)" && $("$D/inj" --version | wc -l) -eq 1 ]] && "$D/inj" --version >/dev/null 2>"$D/err"; [[ $? -eq 0 && ! -s $D/err ]]'
go build -buildvcs=false -o "$D/dev0" ./cmd/sqlmux
check "没注入、没有 vcs 信息（-buildvcs=false）：sqlmux dev，不打 (none, unknown)（F17.5）" eval '[[ $("$D/dev0" --version) == "sqlmux dev" ]] || { "$D/dev0" --version; false; }'
go build -o "$D/dev" ./cmd/sqlmux
check "没注入、在 git 检出里 go build：版本是 Go 记下的伪版本（v…-<时间>-<12 位 sha>），括号里是 vcs 的 revision 前 7 位和提交时间（F17.5），没有 none / unknown" eval 'got=$("$D/dev" --version); r12=$(git rev-parse --short=12 HEAD); r7=$(git rev-parse HEAD | cut -c1-7); [[ $got =~ ^sqlmux\ v[^\ ]+-$r12\ \($r7,\ [0-9T:-]+Z\)$ ]] || { echo "  $got"; false; }'
check "参数多了：退出码 1，usage 里带 [--version]" eval '"$D/inj" a b >/dev/null 2>"$D/err"; [[ $? -eq 1 ]] && grep -qF "usage: sqlmux [--version] [--layout name-or-path] [--schema schema] [--table [schema.]table [--where condition] | --sql statement] [connection | file | url]" "$D/err"'
check "README：Releases 页和 checksums.txt（oss 重写 README 后不再写压缩包名）" eval 'grep -q "releases" README.md && grep -q "checksums.txt" README.md'
mkdir -p "$D/h" "$D/c" "$D/s" "$D/d"
check "--version 不读配置、不建文件：空的 HOME / XDG 目录里跑完仍是空的（没有 connections.toml 也照常输出）" eval 'HOME=$D/h XDG_CONFIG_HOME=$D/c XDG_STATE_HOME=$D/s XDG_DATA_HOME=$D/d "$D/inj" --version >/dev/null && [[ -z $(find "$D/h" "$D/c" "$D/s" "$D/d" -mindepth 1) ]]'

# ---- just build 注入 git describe
just build >/dev/null 2>&1
check "just build：版本是 git describe（${TAG}，m99 在上一个 commit 上也不用它）、commit 是 HEAD 的短 sha、日期是提交时间（UTC）" eval 'got=$(bin/sqlmux --version); want="sqlmux $TAG ($SHORT, $DATE_UTC)"; [[ $got == "$want" ]] || { echo "  $got, want $want"; false; }'
rm -rf bin

# ---- just release-check：goreleaser check + build --snapshot
PATH=$(dirname "$(command -v "$GR")"):$PATH just release-check >"$D/rc.log" 2>&1; rc=$?
check "just release-check 通过（goreleaser check 和 build --snapshot）" eval '[[ $rc -eq 0 ]] || { tail -15 "$D/rc.log"; false; }'
snapshot_binaries() (
  local build arch os
  local binaries=(dist/sqlmux_*/sqlmux dist/sqlmux_windows_*/sqlmux.exe dist/sqlmux-duckdb_*/sqlmux)
  [[ ${#binaries[@]} == 8 ]] || { ls dist; return 1; }
  for arch in amd64 arm64; do
    for os in darwin linux; do
      build=(dist/sqlmux_${os}_${arch}_*/sqlmux)
      [[ -x ${build[0]} ]] || return 1
    done
    build=(dist/sqlmux_windows_${arch}_*/sqlmux.exe)
    [[ -f ${build[0]} ]] || return 1
    build=(dist/sqlmux-duckdb_linux_${arch}_*/sqlmux)
    [[ -x ${build[0]} ]] || return 1
  done
)
check "snapshot 在 dist/ 出八个二进制：darwin / linux / windows 各两个，Linux DuckDB 版两个" snapshot_binaries
check "本机的 snapshot 二进制 --version：sqlmux v<版本>-SNAPSHOT-<短 sha> (短 sha, 日期)" eval 'got=$($(rel_native) --version); [[ $got == "sqlmux v"*"-SNAPSHOT-$SHORT ($SHORT, "* ]] || { echo "  $got"; false; }'

# ---- 打了 tag 的 release（不发布）
rel_run; rr=$?
check "goreleaser release --clean --skip=publish 成功" eval '[[ $rr -eq 0 ]]'
v=${TAG#v}
archives=(sqlmux_"${v}"_{darwin,linux}_{amd64,arm64}.tar.gz
          sqlmux_"${v}"_linux_{amd64,arm64}_duckdb.tar.gz
          sqlmux_"${v}"_windows_{amd64,arm64}.zip)
archive_names() { printf '%s\n' "$@" | LC_ALL=C sort; }
check "八个压缩包的文件名正确，包括两个 Linux _duckdb 包" eval '(cd dist && diff -u <(archive_names "${archives[@]}") <(archive_names *.tar.gz *.zip))'
windows_archive_binaries() {
  local arch
  for arch in amd64 arm64; do
    unzip -Z1 "dist/sqlmux_${v}_windows_$arch.zip" | grep -qx sqlmux.exe || return 1
  done
}
check "两个 Windows zip 都含有 sqlmux.exe（F18.4）" windows_archive_binaries
check "checksums.txt 列出了全部八个压缩包，且校验全部通过" eval '(cd dist && diff -u <(archive_names "${archives[@]}") <(awk "{print \$2}" checksums.txt | LC_ALL=C sort) && shasum -a 256 -c checksums.txt)'
check "每个 tar.gz 里有可执行的 sqlmux" eval '(for f in dist/*.tar.gz; do tar -tzvf "$f" | awk "\$NF == \"sqlmux\" && \$1 ~ /x/ {ok=1} END {exit !ok}" || { echo "  $f"; tar -tzvf "$f"; exit 1; }; done)'
archive_binary_is() {
  local dir="$D/x/$1" description
  mkdir -p "$dir" && tar -xzf "dist/sqlmux_${v}_$1.tar.gz" -C "$dir" sqlmux || return 1
  description=$(file "$dir/sqlmux") || return 1
  printf '%s\n' "$description"
  [[ $description =~ $2 ]]
}
check "Darwin arm64 / amd64 包里的二进制是对应平台的 Mach-O" eval 'archive_binary_is darwin_arm64 "Mach-O.*arm64" && archive_binary_is darwin_amd64 "Mach-O.*x86_64"'
check "普通 Linux amd64 / arm64 包里的 ELF 是静态链接的" eval 'archive_binary_is linux_amd64 "ELF.*x86-64.*statically linked" && archive_binary_is linux_arm64 "ELF.*(ARM )?aarch64.*statically linked"'
check "Linux _duckdb amd64 / arm64 包里的 ELF 是动态链接的" eval 'archive_binary_is linux_amd64_duckdb "ELF.*x86-64.*dynamically linked" && archive_binary_is linux_arm64_duckdb "ELF.*(ARM )?aarch64.*dynamically linked"'
check "本机的二进制 --version：sqlmux ${TAG} ($SHORT, 提交时间)" eval 'got=$($(rel_native) --version); [[ $got == "sqlmux $TAG ($SHORT, $DATE_UTC)" ]] || { echo "  $got"; false; }'
check "压缩包里的本机二进制同样（-s -w 去符号后还能跑）" eval 'os=$(uname -s | tr A-Z a-z); cpu=$(uname -m | sed "s/x86_64/amd64/"); [[ $("$D/x/${os}_${cpu}/sqlmux" -version) == "sqlmux $TAG ($SHORT, $DATE_UTC)" ]]'
chlog=dist/CHANGELOG.md
check "changelog：m99 的 tag 不算发布，从上一个 v* 起算：有 Features 组，F98.1（m99 之前）和 F99.1（之后）都列着" eval 'grep -q "^### Features" $chlog && grep -q "F98.1: before m99" $chlog && grep -q "F99.1: after m99" $chlog'
check "changelog：没有 docs: / e2e: / prune: / Merge 的提交" eval '! grep -Eq "^\* [0-9a-f]+ (docs:|e2e:|prune:|Merge )" $chlog || { grep -E "^\* [0-9a-f]+ (docs:|e2e:|prune:|Merge )" $chlog; false; }'

# ---- release.yml 的结构（workflow 本身要推送后才能跑）
yml() { ruby -ryaml -e 'y = YAML.load_file(ARGV[0]); y["on"] ||= y[true]; puts eval(ARGV[1])' ".github/workflows/$1.yml" "$2" 2>/dev/null; }
check "release.yml 只在 v* tag 上触发" eval '[[ $(yml release "y[\"on\"][\"push\"][\"tags\"].inspect") == "[\"v*\"]" && $(yml release "y[\"on\"].keys.inspect") == "[\"push\"]" ]]'
check "release.yml：ci → release → tap / npm 的依赖链；ci 复用 ci.yml；ci.yml 有 workflow_call" eval '[[ $(yml release "y[\"jobs\"][\"ci\"][\"uses\"]") == ./.github/workflows/ci.yml && $(yml release "y[\"jobs\"][\"release\"][\"needs\"]") == ci && $(yml release "y[\"jobs\"][\"tap\"][\"needs\"]") == release && $(yml release "y[\"jobs\"][\"npm\"][\"needs\"]") == release && $(yml ci "y[\"on\"].key?(\"workflow_call\")") == true ]]'
check "release.yml：release job 有 contents: write，顶层只读；用 goreleaser release --clean，fetch-depth 0" eval '[[ $(yml release "y[\"jobs\"][\"release\"][\"permissions\"][\"contents\"]") == write && $(yml release "y[\"permissions\"][\"contents\"]") == read ]] && grep -q "args: release --clean" .github/workflows/release.yml && grep -q "fetch-depth: 0" .github/workflows/release.yml'
check "release.yml：密钥只用 TAP_GITHUB_TOKEN / NPM_TOKEN / GITHUB_TOKEN" eval '[[ $(grep -o "secrets\.[A-Z_]*" .github/workflows/release.yml | sort -u | tr "\n" " ") == "secrets.GITHUB_TOKEN secrets.NPM_TOKEN secrets.TAP_GITHUB_TOKEN " ]]'
check ".goreleaser.yaml：goreleaser check 通过" eval '"$GR" check >/dev/null 2>"$D/chk"; [[ $? -eq 0 ]] || cat "$D/chk"'
e2e_done
