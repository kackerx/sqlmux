#!/bin/sh
# usage: tap-formula.sh <tag> <sha256 of the tag's source tarball> [commit [date]]
# (commit and date are what --version says; the tarball has no .git to read them from)
# Prints Formula/sqlmux.rb for kackerx/homebrew-tap (release.yml, tech-design
# §15「发布与分发」): built from the tag's source with Go, macOS and Linux alike.
set -eu
tag=$1 sha=$2 commit=${3:-none} date=${4:-unknown}
cat <<RUBY
class Sqlmux < Formula
  desc "Tmux-style terminal database client for PostgreSQL, MySQL, SQLite, SQL Server and Redis"
  homepage "https://github.com/kackerx/sqlmux"
  url "https://github.com/kackerx/sqlmux/archive/refs/tags/${tag}.tar.gz"
  sha256 "${sha}"
  license "MIT"

  depends_on "go" => :build

  def install
    ldflags = "-s -w -X main.version=v#{version} -X main.commit=${commit} -X main.date=${date}" # std_go_args(ldflags:) adds nothing of its own
    system "go", "build", *std_go_args(ldflags:), "./cmd/sqlmux"
  end

  test do
    assert_match "sqlmux v#{version}", shell_output("#{bin}/sqlmux --version")
  end
end
RUBY
