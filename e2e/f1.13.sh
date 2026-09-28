#!/usr/bin/env bash
# F1.13 键位配置的完整导出（specs/m1-browse/task.md F1.13；tech-design §6.7「导出」）
# sqlmux keys 是命令行，不需要终端；「有标题的 Action」取自命令面板的命令范围（它列的就是带标题的 Action，
# 只在浮层 / 输入态里用的除外，§12），和导出结果对照。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-keys.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
keys() { local c=$1; shift; mkdir -p "$D/$c/sqlmux" "$D/st"; XDG_CONFIG_HOME=$D/$c XDG_STATE_HOME=$D/st XDG_DATA_HOME=$D/st "$E2E_BIN" keys "$@"; }   # CONFIG_DIR ARGS…

# ---- 导出的 toml 原样放进 config.toml：sqlmux keys 的输出与没有配置时完全一致
keys none >"$D/default.md"; keys none --format toml >"$D/export.toml"
mkdir -p "$D/rt/sqlmux"; cp "$D/export.toml" "$D/rt/sqlmux/config.toml"
check "默认键位导出 → 放进 config.toml：--check 通过" keys rt --check
check "… sqlmux keys 的输出与没有配置时完全一致" eval 'keys rt | cmp -s - "$D/default.md" || { keys rt | diff "$D/default.md" - | head; false; }'
# 用户改过键（改绑、解绑、[map]）时同样：导出的是当前生效的键位
mkdir -p "$D/mine/sqlmux"; printf '[keys.grid]\n"x" = ""\n"X" = "tab.close"\n[keys.tree]\n"o" = "tree.open"\n' >"$D/mine/sqlmux/config.toml"
keys mine >"$D/mine.md"; mkdir -p "$D/rt2/sqlmux"; keys mine --format toml >"$D/rt2/sqlmux/config.toml"
check "改绑、解绑之后导出 → 再放进 config.toml：键位与原配置一致（x 仍是解绑的）" eval 'keys rt2 | cmp -s - "$D/mine.md" && ! grep -q "| grid | \`x\` |" "$D/mine.md" || { keys rt2 | diff "$D/mine.md" - | head; false; }'

# ---- 片段的格式：每个作用域一节，节前一行注释；末尾是注释掉的 [map.*] 示例
check "每个 [keys.<作用域>] 前一行都是注释" eval 'awk "/^\[keys\./ { if (prev !~ /^# /) bad = bad \$0 \" \" } { prev = \$0 } END { if (bad) { print \"  no comment above: \" bad; exit 1 } }" "$D/export.toml"'
check "末尾附一段注释掉的 [map.<上下文>.<模式>] 示例" eval 'tail -5 "$D/export.toml" | grep -qE "^# \[map\.[a-z]+\.[a-z]+\]"'

# ---- 有标题的 Action 都在：有绑定的出现在绑定行里，没有绑定的恰好一行注释
bound=$(grep -E '^"([^"\\]|\\.)+" = "[a-z_.]+"' "$D/export.toml" | sed -E 's/^"([^"\\]|\\.)+" = "([a-z_.]+)".*/\2/' | sort -u)   # 键可能带转义的引号，如 "<Leader>\""
unbound=$(grep -E '^# "" = "[a-z_.]+"' "$D/export.toml" | sed -E 's/^# "" = "([a-z_.]+)".*/\1/' | sort)
check "没有绑定的 Action 各恰好一行注释，也不出现在任何绑定行里" eval '[[ -n $unbound && -z $(uniq -d <<<"$unbound") && -z $(comm -12 <(echo "$bound") <(echo "$unbound")) ]]'
check "一个 Action 绑了多个键时有多行（palette.up：↑ 和 C-p）" eval '(( $(grep -c "= \"palette.up\"" "$D/export.toml") == 2 ))'
start; pal ">"; sleep 0.3
ids=; last=; same=0
for ((i = 0; i < 150 && same < 3; i++)); do          # 从第一项往下走，读选中行的「所在位置」（action id）
  id=$(selected | awk -F'  ' '{ print $2 }')
  [[ $id == "$last" ]] && same=$((same + 1)) || { same=0; ids="$ids$id"$'\n'; }
  last=$id; e2e_keys Down; sleep 0.12
done
cmds=$(sort -u <<<"$ids" | grep .)
check "命令面板的每个命令（带标题的 Action，共 $(grep -c . <<<"$cmds") 个）都在导出里：绑定行或注释行" eval 'miss=$(comm -23 <(echo "$cmds") <(printf "%s\n%s\n" "$bound" "$unbound" | sort -u)); [[ $(grep -c . <<<"$cmds") -gt 20 && -z $miss ]] || { echo "  missing: $miss"; false; }'

e2e_done
