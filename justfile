# The everyday commands (tech-design §15). `just` lists them.

# the version CI pins too (.github/workflows/ci.yml)
lint_version := "2.14.0"

# the databases docker-compose.yml starts; set them to test against others
export SQLMUX_TEST_PG := env("SQLMUX_TEST_PG", "postgres://sqlmux:sqlmux@localhost:55432/sqlmux?sslmode=disable")
export SQLMUX_TEST_MYSQL := env("SQLMUX_TEST_MYSQL", "sqlmux:sqlmux@tcp(localhost:53306)/sqlmux")
export SQLMUX_TEST_REDIS := env("SQLMUX_TEST_REDIS", "redis://localhost:56379/0")

default:
    @just --list

# GoReleaser's config is valid and builds the four binaries into dist/ (needs goreleaser)
release-check:
    goreleaser check
    goreleaser build --snapshot --clean

# the npm packages of a snapshot build are made, installed side by side and run (needs goreleaser, node, npm)
npm-check:
    #!/usr/bin/env bash
    set -eu
    goreleaser release --snapshot --clean --skip=publish
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    node npm/build.mjs v0.0.0 dist "$tmp/out"
    os=$(uname -s | tr A-Z a-z); cpu=$(uname -m | sed 's/x86_64/x64/;s/aarch64/arm64/')
    mkdir "$tmp/tgz" "$tmp/use"
    for d in "$tmp/out/sqlmux" "$tmp/out/sqlmux-$os-$cpu"; do (cd "$d" && npm pack --pack-destination "$tmp/tgz" >/dev/null); done
    cd "$tmp/use" && npm init -y >/dev/null && npm i --offline "$tmp"/tgz/*.tgz >/dev/null
    ./node_modules/.bin/sqlmux --version

# build bin/sqlmux
build:
    go build -ldflags "-X main.version=$(git describe --tags --match 'v*' --always --dirty) -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(TZ=UTC git log -1 --date=iso-strict-local --format=%cd)" -o bin/sqlmux ./cmd/sqlmux

# run sqlmux from source
run *args:
    go run ./cmd/sqlmux {{args}}

# unit tests, golden files included
test:
    go test ./...

# the tests that need the databases of `just db-up` (in the main workspace only: AGENTS.md)
test-integration:
    go test -tags integration ./... || { echo "if the databases are not up: just db-up" >&2; exit 1; }

# end-to-end scripts in tmux, all of them or the named ones (just e2e f0.1 f0.2); one build for all
e2e *scripts:
    #!/usr/bin/env bash
    set -u
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    go build -o "$tmp/sqlmux" ./cmd/sqlmux || exit 1
    export E2E_BIN="$tmp/sqlmux" E2E_NOBUILD=1
    list="{{scripts}}"
    [ -n "$list" ] || list=$(cd e2e && ls *.sh | sed 's/\.sh$//' | grep -vx -e lib -e palette)
    failed=()
    for name in $list; do
        bash "e2e/$name.sh" || failed+=("$name")
    done
    [ ${#failed[@]} -eq 0 ] || { echo "e2e failed: ${failed[*]}"; exit 1; }

# sqlmux over the seed database with pprof on addr, and the commands that take a profile of it (§15「性能」)
profile addr="localhost:6060":
    #!/usr/bin/env bash
    set -eu
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    mkdir -p "$tmp/config/sqlmux" "$tmp/state" "$tmp/data"
    (umask 077; printf '[[connection]]\nname = "seed"\nengine = "postgres"\ndsn = "%s"\n' "$SQLMUX_TEST_PG" >"$tmp/config/sqlmux/connections.toml")
    echo "CPU:  go tool pprof -top -cum 'http://{{addr}}/debug/pprof/profile?seconds=10'"
    echo "heap: go tool pprof -sample_index=alloc_space -top http://{{addr}}/debug/pprof/heap"
    XDG_CONFIG_HOME=$tmp/config XDG_STATE_HOME=$tmp/state XDG_DATA_HOME=$tmp/data SQLMUX_PPROF={{addr}} go run ./cmd/sqlmux

# golangci-lint at the pinned version, with .golangci.yml
lint:
    @golangci-lint --version | grep -q "version {{lint_version}} " || { echo "golangci-lint {{lint_version}} wanted: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v{{lint_version}}" >&2; exit 1; }
    golangci-lint run ./...

# gofmt every file
fmt:
    gofmt -w .

# go mod tidy
tidy:
    go mod tidy

# what every commit passes: vet, tests with the databases, lint, gofmt, go mod tidy
check:
    go vet ./...
    go vet -tags integration ./...
    go test ./...
    go test -run '^$' -bench . -benchtime 1x ./internal/app/
    just test-integration
    just lint
    @test -z "$(gofmt -l .)" || { gofmt -l . >&2; echo "gofmt: just fmt" >&2; exit 1; }
    go mod tidy
    @git diff --quiet go.mod go.sum || { echo "go mod tidy changed go.mod or go.sum" >&2; exit 1; }

# rewrite the golden files (default: every package that has some) with a throwaway home and clipboard
golden *pkgs:
    #!/usr/bin/env bash
    set -eu
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    mkdir "$tmp/bin"
    printf '#!/bin/sh\ncat >"%s/clipboard"\n' "$tmp" >"$tmp/bin/pbcopy"
    printf '#!/bin/sh\ncat "%s/clipboard" 2>/dev/null\n' "$tmp" >"$tmp/bin/pbpaste"
    chmod +x "$tmp/bin/pbcopy" "$tmp/bin/pbpaste"
    export PATH="$tmp/bin:$PATH" XDG_CONFIG_HOME="$tmp/config" XDG_STATE_HOME="$tmp/state" XDG_DATA_HOME="$tmp/data"
    pkgs="{{pkgs}}"
    go test ${pkgs:-./internal/app ./internal/sqlkit ./internal/ui} -update

# rewrite the editor's expected results with nvim (internal/editor/testdata)
generate:
    go generate ./internal/editor

# start PG 17 and MySQL 8.4 of docker-compose.yml, seeded on their first start;
# Redis is not here (an image that cannot be pulled would fail this): `docker compose up -d redis redis-seed`,
# or a redis-server --port 56379 given testdata/seed/redis.txt with redis-cli
db-up:
    docker compose up -d --wait postgres mysql

# stop them, the data kept
db-down:
    docker compose down

# start them over: a changed testdata/seed/*.sql is loaded only into an empty volume
db-reset:
    docker compose down -v
    docker compose up -d --wait postgres mysql

# the keys table or toml of `sqlmux keys` (--format md|toml, --check)
keys *args:
    go run ./cmd/sqlmux keys {{args}}

# remove bin/
clean:
    rm -rf bin
