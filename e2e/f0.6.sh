#!/usr/bin/env bash
# F0.6 which-key（specs/m0-skeleton/task.md F0.6；tech-design §6.5、§6.8）
# 点击浮层里的项属于 F0.8。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

CFG=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$CFG"' EXIT
start() { e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
start_with() { printf "$1" >| "$CFG/config.toml"; start -c "$CFG/config.toml"; }
H() { e2e_flag pane_height; }
W() { e2e_flag pane_width; }
# 浮层上边框所在行（一次截屏，计时类断言要快）
top_row() { e2e_plain | python3 -c 'import sys; print(next((i + 1 for i, l in enumerate(sys.stdin) if l.startswith("┌─ " + sys.argv[1])), ""))' "$1"; }
shown()  { [[ -n $(top_row "$1 ") ]]; }
hidden() { ! shown "$1" || { echo "  which-key '$1' is showing"; false; }; }
pending_is() { local c; c=$(e2e_find "C-p" "$(H)"); text_is $((c + 7)) $((c + 6 + $(strwidth "$1"))) "$(H)" "$1"; }
cmdline_open() { [[ $(e2e_text 1 "$(W)" "$(H)") == *" COMMAND " ]]; }
running() { flag_is alternate_on 1 && ! screen_has '[e2e-exit'; }
# 浮层里的键，按列读（先竖后横）：每一格是「键 → 标题」
items() {
  local t b; t=$(top_row "$1 "); b=$(($(H) - 2))
  e2e_plain | sed -n "$((t + 1)),${b}p" | python3 -c '
import re, sys
rows = [re.findall(r"(?:^|\s)(\S) → ", l[1:]) for l in sys.stdin]   # 每行从左到右的键
cells = [[r[k] for r in rows if len(r) > k] for k in range(max(map(len, rows), default=0))]
print(" ".join(k for col in cells for k in col))'
}
titles_cjk() { e2e_plain | sed -n "$(($(top_row "$1 ") + 1)),$(($(H) - 2))p" | python3 -c '
import re, sys
ts = [t.strip() for l in sys.stdin for t in re.findall(r"→ (.+?)(?=\s{2,}\S →|\s*│$)", l)]
sys.exit(0 if ts and all(re.search(r"[一-鿿]|ORDER|LIMIT|PAGE|COLS", t) for t in ts) else 1)'; }

SPC_KEYS='s c n p l % " z x q b'   # §6.8 里以 SPC 开头的默认键（F0.11 精简后），按 default.toml 的顺序

# ---- 出现：停在纯前缀节点 400ms 后
start
e2e_keys Space; sleep 0.2
check "SPC 后 0.2 秒：还没有浮层" hidden SPC
sleep 0.4
check "SPC 后约 0.6 秒：出现 which-key" shown SPC
check "浮层紧贴状态栏上方、全宽" eval '[[ $(e2e_text 1 1 $(($(H) - 1))) == └ && $(e2e_text $(W) $(W) $(($(H) - 1))) == ┘ && $(e2e_text 1 1 $(top_row "SPC ")) == ┌ ]]'
check "上边框标题 SPC：warn 色粗体" eval 'style_has 4 $(top_row "SPC ") fg=#e0af68 && style_has 4 $(top_row "SPC ") bold'
check "内容与 §6.8 的 SPC 键一致、按列排列" eval '[[ $(items SPC) == "$SPC_KEYS" ]] || { echo "  got: $(items SPC)"; false; }'
check "每项是「键 → 标题」，标题取自注册表（中文）" titles_cjk SPC
check "状态栏仍在、待输入显示 SPC" eval 'pending_is SPC && [[ $(e2e_text 1 "$(W)" "$(H)") == *" NORMAL " ]]'

# ---- esc 取消
e2e_keys Escape; sleep 0.2
check "esc：浮层关闭、待输入回到 ·" eval 'hidden SPC && pending_is "·"'

# ---- 按下一个键：效果与直接按相同
e2e_keys Space; sleep 0.6; e2e_type s; sleep 0.2
check "浮层里按 s（完成 SPC s）：浮层关闭、待输入清空" eval 'hidden SPC && pending_is "·"'
e2e_keys Space; sleep 0.05; e2e_type s; sleep 0.6
check "SPC 后立即按 s：不出现浮层" eval 'hidden SPC && pending_is "·"'

# ---- 过期的 Tick 要忽略：第一次 SPC 的 400ms 定时器不能让第二次 SPC 提前弹出
e2e_keys Space; sleep 0.3; e2e_type s; e2e_keys Space; sleep 0.2
check "第一次 SPC 的定时器过期后，第二次 SPC 不提前弹出" hidden SPC
sleep 0.4
check "第二次 SPC 满 400ms 后弹出" shown SPC
e2e_keys Escape; sleep 0.2

# ---- 浮层里的 C-c 等同 esc（§6.8），不计入连按两次退出
e2e_keys Space; sleep 0.6; e2e_keys C-c; sleep 0.3
check "浮层里 C-c：关闭浮层、清空待输入、不弹退出提示" eval 'hidden SPC && pending_is "·" && ! screen_has 再按一次'
e2e_keys C-c; sleep 0.3
check "随后的 C-c 算第一次：不退出" eval 'running && screen_has "再按一次 C-c 退出"'

# ---- grid 里的 g：先 grid 的键，再 normal 的键
start
e2e_type g; sleep 0.6
check "grid 里按 g：浮层列出 g o l p c t T" eval '[[ $(items g) == "g o l p c t T" ]] || { echo "  got: $(items g)"; false; }'
e2e_keys Escape; sleep 0.2

# ---- 高度不够：只截掉放不下的行，不压状态栏
for s in "80 24" "80 12" "40 6"; do
  set -- $s; start -x $1 -y $2; e2e_keys Space; sleep 0.6
  check "${1}x$2：浮层在状态栏之上、状态栏完整" eval 'shown SPC && [[ $(e2e_text 1 1 $(($(H) - 1))) == └ && $(e2e_text 1 "$(W)" "$(H)") == *" NORMAL " ]]'
done

# ---- 在浮层里按键，效果与直接按相同：用 <Leader>: 打开命令行来观察
start_with '[keys.normal]\n"<Leader>:" = "cmdline.open"\n'
e2e_keys Space; sleep 0.6
check "配置的 <Leader>: 出现在浮层里" eval '[[ $(items SPC) == *" :"* || $(items SPC) == ":"* ]]'
e2e_type ':'; sleep 0.3
check "浮层里按 :：打开命令行、浮层关闭" eval 'cmdline_open && hidden SPC'
e2e_keys Escape; sleep 0.2
e2e_keys Space; sleep 0.05; e2e_type ':'; sleep 0.3
check "不等浮层直接按 SPC :：同样打开命令行" cmdline_open
e2e_keys Escape; sleep 0.2

# ---- 浮层内容跟随 keymap
start_with '[keys.normal]\n"<Leader>n" = ""\n'
e2e_keys Space; sleep 0.6
check "解绑 <Leader>n：浮层里没有 n" eval '[[ " $(items SPC) " != *" n "* && -n $(items SPC) ]]'
start_with '[keys]\nleader = "<C-a>"\n'
e2e_keys C-a; sleep 0.6
check "leader = <C-a>：浮层标题为 C-a，内容不变" eval 'shown C-a && [[ $(items C-a) == "$SPC_KEYS" ]]'

# ---- which-key 不是作用域（§6.4）：[keys.whichkey] 是未知的表
printf '[keys.whichkey]\n"a" = "b"\n' >| "$CFG/config.toml"; mkdir -p "$CFG/x/sqlmux"; cp "$CFG/config.toml" "$CFG/x/sqlmux/"
err=$(XDG_CONFIG_HOME="$CFG/x" "$E2E_BIN" keys --check 2>&1); rc=$?
check "[keys.whichkey]：--check 报「没有这个表」、退出码 1" eval '[[ $rc == 1 && $err == *"[keys.whichkey] a: 没有这个表"* ]] || { echo "  rc=$rc: $err"; false; }'

e2e_done
