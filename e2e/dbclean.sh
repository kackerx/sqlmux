#!/usr/bin/env bash
# F21.1 just db-clean（specs/m21-cli-outer/task.md；tech-design §15）
# 在 PG 和 MySQL 上各建：死 pid 的 sqlmux_e2e_<pid>、死 pid 的 sqlmux_worker_<pid>_x、活 pid（本脚本）的 sqlmux_worker_<pid>_x、
# 不匹配的 sqlmux_keep_$$；跑一遍后只有活的和不匹配的还在。
. "$(dirname "$0")/lib.sh"
cd "$E2E_ROOT" || exit 1

dead=$((999999 - $$ % 1000)); while kill -0 "$dead" 2>/dev/null; do dead=$((dead - 1)); done
gone=("sqlmux_e2e_$dead" "sqlmux_worker_${dead}_x") kept=("sqlmux_worker_$$_x" "sqlmux_keep_$$")
pg_has() { [[ $(psql "$SQLMUX_TEST_PG" -qAt -c "select count(*) from pg_database where datname = '$1'") == 1 ]]; }
my_has() { [[ -n $(echo "show databases like '$1'" | e2e_mysql) ]]; }
cleanup() { for n in "${gone[@]}" "${kept[@]}"; do
  psql "$SQLMUX_TEST_PG" -qAt -c "drop database if exists $n with (force)" >/dev/null 2>&1; echo "drop database if exists $n" | e2e_mysql; done; }
trap 'cleanup; e2e_stop' EXIT
for n in "${gone[@]}" "${kept[@]}"; do
  psql "$SQLMUX_TEST_PG" -qAt -c "create database $n" >/dev/null && echo "create database $n" | e2e_mysql || { echo "e2e: cannot create $n" >&2; exit 1; }
done

out=$(just db-clean 2>&1); rc=$?
echo "$out" | sed 's/^/  | /'
check "just db-clean 退出码 0" test $rc -eq 0
for n in "${gone[@]}"; do
  check "PG：死 pid 的 $n 被删" eval '! pg_has "$n"'
  check "MySQL：死 pid 的 $n 被删" eval '! my_has "$n"'
  check "输出里写了删掉 $n" eval '[[ $out == *"dropped $n"* ]]'
done
for n in "${kept[@]}"; do
  check "PG：$n 还在" pg_has "$n"
  check "MySQL：$n 还在" my_has "$n"
done
check "输出里写了活 pid 的库留着" eval '[[ $out == *"keep sqlmux_worker_$$_x"* ]]'
check "共享的 sqlmux 库还在" pg_has sqlmux
e2e_done
