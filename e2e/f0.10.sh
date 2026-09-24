#!/usr/bin/env bash
# F0.10 字符宽度统一按字素簇（specs/m0-skeleton/task.md F0.10；tech-design §7.1「宽度」）
# 渲染器切换宽度算法这条借用的路径由单测 TestRendererUsesGraphemeWidths 守住；这里只看 tmux 里的效果。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

OUT=$(mktemp "${TMPDIR:-/tmp}/sqlmux-e2e-out.XXXXXX")
trap 'e2e_stop; rm -f "$OUT"' EXIT
bar_end_is() { local got; got=$(e2e_text 150 160 45); [[ $got == *" $1 " ]] || { echo "  status tail: '$got'"; false; }; }
# F0.13 起「未知命令」toast 没了，改在命令面板的输入行里放这些字形：输入行的右边框要还在原位
input_row_ok() {
  local t; t=$(e2e_plain | python3 -c 'import sys; print(next(i + 1 for i, l in enumerate(sys.stdin) if "┌─ 命令面板" in l))')
  local l; l=$(e2e_find "┌─ 命令面板" "$t" | cut -d' ' -f1)
  [[ $(e2e_text $((l + 79)) $((l + 79)) $((t + 1))) == │ && $(e2e_text $((l + 2)) $((l + 20)) $((t + 1))) == ">x$1"* ]] ||
    { echo "  input row: '$(e2e_text $l $((l + 79)) $((t + 1)))'"; false; }
}
# bash 3.2 没有 \u：用 UTF-8 字节写
declare -a NAMES=("👍🏽（肤色修饰）" "❤️（VS16）" "👨‍👩‍👧（ZWJ）" "1️⃣（keycap）")
declare -a EMOJI=("$(printf '\xf0\x9f\x91\x8d\xf0\x9f\x8f\xbd')" "$(printf '\xe2\x9d\xa4\xef\xb8\x8f')" \
  "$(printf '\xf0\x9f\x91\xa8\xe2\x80\x8d\xf0\x9f\x91\xa9\xe2\x80\x8d\xf0\x9f\x91\xa7')" "$(printf '1\xef\xb8\x8f\xe2\x83\xa3')")

start
for i in 0 1 2 3; do
  e=${EMOJI[$i]}
  e2e_type ":x${e}"; sleep 0.3
  check "${NAMES[$i]}：面板打开时，状态栏行尾的 COMMAND 完整" bar_end_is COMMAND
  check "${NAMES[$i]}：面板输入行完整，右边框在原位" input_row_ok "${e}"
  e2e_keys Escape; sleep 0.3
  check "${NAMES[$i]}：之后 NORMAL 状态栏完整" bar_end_is NORMAL
done

# 渲染器切到字素簇时写 CSI ? 2027 h，退出时写 CSI ? 2027 l 还原
e2e_start "sleep 0.5; $E2E_BIN"; e2e_record "$OUT"; wait_for 5 flag_is alternate_on 1; sleep 0.3
e2e_type ':qa'; e2e_keys Enter; wait_for 3 screen_has '[e2e-exit 0]'; sleep 0.2
check "启动时开启 mode 2027、退出时关闭（关闭在开启之后）" python3 -c '
import sys; b = open(sys.argv[1], "rb").read()
on, off = b.find(b"\x1b[?2027h"), b.rfind(b"\x1b[?2027l")
sys.exit(0 if 0 <= on < off else f"  2027h at {on}, 2027l at {off}")' "$OUT"
e2e_type "printf 'a$(printf '\xf0\x9f\x91\x8d\xf0\x9f\x8f\xbd')b|'"; e2e_keys Enter; sleep 0.3
check "退出后 shell 里 emoji 不错位" eval 'e2e_plain | grep -q "^a$(printf "\xf0\x9f\x91\x8d\xf0\x9f\x8f\xbd")b|"'

e2e_done
