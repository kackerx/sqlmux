# sqlmux 技术方案 · §6 输入系统

> 这是技术方案的一章，各章的索引和阅读规则在 [`../tech-design.md`](../tech-design.md)；章节编号（§N.M）全仓库通用，不随文件改。

## 6. 输入系统

### 6.1 Action

```go
type Action struct {                // 注册表是 map[id]Action，id 如 "pane.split.right"
    Title string                    // which-key 和命令面板显示的名字；只在浮层、cell、input 里用的 Action 没有标题（§12）
    Run   func(*App, Args) tea.Cmd  // Args 含参数与次数 Count；还没实现的 Action 只有标题，没有 Run
    On    func(*App) bool           // 开关类命令的当前状态（K-04 的 ON/OFF）
}
```

命令面板列出所有带 Title 的 Action，所以没有绑定键位的命令也能执行（PRD 6.3）。Action 可以带参数，配置里写作 `"window.select 3"`。

### 6.2 终端兼容：默认键位只用处处可用的键

不假设用户使用 tmux 或 kitty。默认键位只用下面这些键，它们在 macOS Terminal.app、iTerm2、Linux 各种终端、SSH、tmux/screen 里都能区分：

- **可以用的**：
  - 可打印字符、Esc、Tab、S-Tab、↵、Backspace、方向键。
  - Ctrl+字母，但排除 C-i、C-m、C-[：在传统编码下它们分别等同于 Tab、↵、Esc。
  - C-h 在少数终端里等同于 Backspace，所以只在 NORMAL 模式下使用，那里 Backspace 没有用途。
- **不用的**：
  - Alt/Option 组合：macOS 的终端默认不把 Option 当作 Meta。
  - Ctrl+Shift 组合。
  - C-↵ 等带修饰键的回车。
  - Ctrl+数字：传统编码无法发送。
  - Home、End、PgUp、PgDn：macOS 的 Terminal.app 默认拿它们来滚动回滚缓冲区，按键不会传给程序；而且笔记本键盘上没有这几个键。
  - F1–F12：经常被操作系统占用，在 Mac 笔记本上还要同时按住 fn。
- **kitty 键盘协议只顺带开启**：终端支持时会打开（Bubble Tea v2 里只是一个字段），用户可以在配置里自己绑定 C-↵ 这类键。终端不支持时，这类绑定不生效，对应的提示也不显示。
  - 默认键位不依赖这个协议。有一条单测保证 `default.toml` 里不出现只能在这个协议下使用的键。
- **终端翻译过来的键**（M5 F5.17，用户要用 Cmd、Ctrl+Shift 组合）：这类键用传统编码送不进来，用户可以让终端把它翻成 CSI u 编码的键（如 `<C-S-w>`）再送进来，做法同用户 kitty + tmux 里给 nvim 的那套：kitty 把键翻成 tmux 前缀加一个键，tmux 看 pane 里跑的是什么再决定发什么。这类键绑在 `[keys.global]` 里，在任何模式下都生效，不会当成文字输进去；终端或 tmux 没有协商 kitty 协议时也要能解析。默认键位照旧不用它们。
  - **送法**（F5.17 在 tmux 3.7b 上实测）：tmux 的分支用 `send-keys -H` 写原始字节，格式 `ESC [ <码点十进制> ; <1+修饰> u`（shift 1、alt 2、ctrl 4、super 8），如 `<C-S-w>` 是 `send-keys -H 1b 5b 31 31 39 3b 36 75`。Bubble Tea 不管协商过没有都按 CSI u 解析；在 query 的 NORMAL / INSERT、表格、WHERE 的 NORMAL / INSERT、面板打开时都执行了绑定的命令，没有字符漏进输入框。tmux 自己的 `send-keys C-S-w` 不行：不论 extended-keys 怎么设，只有请求了 modifyOtherKeys 的 pane 才收到带 shift 的编码，Bubble Tea 只请求 kitty 协议，收到的是普通的 C-w。
  - **Super（Cmd）键**：带 Super、不带 Ctrl / Alt 的键记作 `<D-x>`（vim 的记法），能绑在 `[keys.global]` 里，没绑时也不会当成字符输入（F5.17 实测时发现：kitty 开着协议、Cmd+x 没被 kitty 截住时，原来会当成 `x` 删掉一个字符）。
- **兜底**：键盘不方便的操作，都能用鼠标（§7.4）或命令面板完成。

### 6.3 键位记法

配置里用 vim 记法：`<C-p>`、`<Space>`、`<Esc>`、`<S-Tab>`、`<CR>`；字符序列直接写，如 `gt`、`gq`、`%`。

界面显示成 PRD 的风格：`C-p`、`SPC`、`esc`、`↵`。

### 6.4 作用域与解析

| 作用域 | 何时生效 |
|---|---|
| `palette` / `where` / `cols` / `order` / `dropdown` / `sessions` / `form` / `complete` / `options` / `segments` / `confirm` / `keyhelp` / `menu` / `conns` / `ddl` | 对应的浮层打开时，取最上层的一个。最上层由一个固定顺序决定（M13 F13.7 的 `topOverlay`，绘制顺序是它的倒序）；两个方向都能开到对方之上的只有 session 列表和连接管理 / 新建连接表单，定为表单在上（表单里有正在填的内容，审查时定）。确认框开着时只放行 `confirm.*` 和取消，全局键（`C-p`、`C-s`、Ctrl leader、`?`）不生效（审查时定；原来能在确认框底下再开浮层）。`menu` 是树和表格的右键菜单（M8 F8.23；表格 M12 F12.3）、`order` 是 ORDER 浮层（M12 F12.9，COLS 式的列表，§7.6）、`conns` 是连接管理的列表（M8 F8.16）、`ddl` 是表 DDL 浮窗（M9 F9.8：`j` / `↓`、`k` / `↑` 一行，`C-d` / `C-u` 半页，`gg` / `G`，`esc` / `C-c` 关；滚轮也滚它，点浮窗外关）。`options` 是单元格的选项浮层，`segments` 是时间列的分段浮层，都只在展开时生效（§10.2）；`keyhelp` 是 `?` 的键位帮助（§6.5）；`sessions` 是 session 列表，`form` 是新建连接的表单（M5 F5.1，§7.8）。which-key 浮层不是作用域（§6.5） |
| `value` | 值编辑器浮窗打开时（M12 F12.2，§10「值编辑器」）：只有应用层的键（`C-s` 写回并关闭），其余按键交给 vim 引擎，同 `query`；`[map.normal]` / `[map.visual]` 照常，`[map.query.*]` 不用 |
| `wherenormal` | WHERE 输入框在 NORMAL 下（F3.39）：`↵` 执行、`/` 历史 / 收藏、`esc` 回表格，其余交给编辑器；`normal` 照常在它下面 |
| `newrow` | 编辑还没保存的新行的格时生效（F3.33），叠在 `options` / `segments` 之上：浮层层级里它在最上，没绑的键再落到下面那层浮层。默认只有 `Tab` / `S-Tab`（`cell.field.next` / `cell.field.prev`） |
| `cell` | 正在编辑单元格（INSERT） |
| `input` | 任意单行输入框获得焦点（INSERT） |
| `result` | result pane 获得焦点时生效，优先级在 `grid` 之上（如 `P`、`q`）；其余按键落到 `grid` |
| `select` | 表格有选中的记录、或在 VISUAL 扩选时生效，叠在 `grid` / `result` 之上（M10 F10.7，§7.6「选择」）：`d` / `y` 对选中的记录生效、`esc` 清空；它们是完整绑定，上层优先，不因为下层的 `dd` / `yy` 等 `timeoutlen`：解析器合并 trie 时，`select` 是「完整绑定优先」的作用域，挂在它已绑节点下面的下层更长序列不并进来（Context 里指定，只对 `select` 生效；别的作用域照旧先到先得、更长序列仍挂在下面）。表格的 VISUAL 仍是 NORMAL 上下文，`normal` 层和用户映射照常叠在下面（`C-h` / `C-j` / `C-k` / `C-l`、`gt` / `gT`、`?`、leader 都能用）；「VISUAL 下不叠 `normal`」只是 query 的 vim VISUAL |
| `jump` | flash 式跳转进行中（M10 F10.8，§6.10）：`esc`、`backspace`、`↵`；其余可打印字符进模式，不经过别的作用域 |
| `jsonview` | 表格的 JSON 视图开着时（M14 F14.5，§7.6）：`esc` / `q` = `json.leave`，`gj` 再按一次也回表格；`s` = `jump.start`（NORMAL 和 VISUAL）、`Y` = `json.yank`（只 NORMAL，VISUAL 的 `Y` 是 vim 的）（M16 F16.8 / F16.9）；翻页和刷新用表格自己的键（M15 F15.9：`[keys.result]` / `[keys.grid]` 里绑到 `grid.page.next` / `grid.page.prev` / `result.page.*` / `grid.refresh` 的键，用户改成 `<` / `>` 也生效；表格的其他键不进 JSON 视图）；其余键交给只读编辑器 |
| `queryinsert` | query 在 INSERT 下（M14 F14.2）：默认只绑 `C-r` = `query.history`「查询历史…」；vim NORMAL 的 `C-r` 仍是 redo，INSERT 里 vim 的 `C-r` 是插寄存器、这里让给历史；`vim = false` 时同样 |
| `grid` / `tree` / `query` / `landing` | 对应控件获得焦点：按 pane 当前 tab 的类型，表格 → grid，query → query，引导页和没有 tab 的 pane → landing（M3 F3.7）。`query` 只含应用层的键（如 ↵ 执行），其余按键交给 vim 引擎；`gq` 是编辑器的操作符（§9.5） |
| `normal` | 所有 NORMAL 上下文共用：pane 焦点、leader、gt/gT。`:`、`;` M3 起在 grid / tree / landing 里（§6.8） |
| `global` | 始终生效，只有少数 Ctrl 组合：C-p、C-s、C-c |

解析顺序：最上层浮层 → 用户映射（仅在 NORMAL 上下文，§6.6）→ 焦点控件 → `normal` → `global`，先匹配到的生效。

- 实现上，把当前上下文涉及的各作用域 trie 按上述优先级合并，合并结果缓存起来复用。
- INSERT 下没有匹配到的可打印字符交给输入控件。
- query 中没有匹配到的按键序列（包括已经缓冲的前缀，如 `g` 之后的 `g`）整体交给 vim 引擎。
- **编辑器在等后续按键时，按键不经过 keymap**（M3 定）：有待输入的操作符、字符参数或文本对象前缀时（`d`、`c`、`y`、`f`、`t`、`r`、`i` / `a` 等），按键直接交给引擎，用户映射也不展开，与 nvim 在 operator-pending 下不用 normal 映射一致；只有 `C-c` 仍等同 esc。否则 `f<Space>x` 会被当成 leader 序列 `SPC x`，把 pane 关掉。

### 6.5 按键序列、leader 与 which-key

**不做 tmux 式的全局前缀键**：

- 实现上它并不复杂：前缀键和 `gt`、`gg`、`dd` 走的是同一套按键序列机制。
- 真正的问题有两个：全局 Ctrl 前缀会占掉输入框里常用的 Ctrl 键（C-a 行首、C-w 删词）；而且会和 tmux 用户自己设置的前缀冲突。

**也不能只用 Ctrl 组合键**：

- 传统终端里能用的 Ctrl+字母只有二十个左右，大半已被 vim/readline 的习惯占用；Ctrl+数字又无法发送。
- PRD 里约 30 个工作现场操作（切 window 0–9、分割、缩放、调整大小……）装不下。

**方案（已确认）**：

- NORMAL 模式下用 `SPC` 作 leader，后面接 PRD 原来跟在 C-a 后面的那些键：`SPC s` 打开 session 列表、`SPC %` 左右分割，以此类推。与 PRD 基本是机械替换关系，默认只保留常用的几个（§6.8）。
- 最常用的 pane 焦点切换另配直达键：C-h/j/k/l。
- leader 可以配置。配成 Ctrl 组合（如 `<C-a>`）时，它在所有模式下生效，也就恢复了 tmux 式前缀。

**which-key**：按键序列停在「只是前缀」的位置超过 400ms，就弹出提示浮层，列出下一步能按的键和对应操作名。每一项都可以点击。这解决了 PRD 10.2 #4。

**键位帮助 `?`**（M2/M3 验收时用户要求，同 nvim which-key 的 `:WhichKey`）：在表格、树、引导页、结果区按 `?`，打开当前上下文的全部键位，也就是按 §6.4 的顺序合并后生效的键，含用户映射，每项带操作名；前缀显示成一组，画法和 which-key 相同（`g → …`）；每一层的键按来源的配置表分组，组标题是表名（`[keys.grid]`、`[map.grid.normal]`、`[keys.normal]` 等），顺序同上面的解析顺序，只列生效的绑定，让人看出这个键属于哪个作用域、改它该改哪一节（用户验收时要求，which-key 浮层同样分组）；按下前缀往下一层、`<BS>` 回上一层，按到绑定就关掉帮助并照原来的上下文执行；`esc` 关闭。query 里只列 keymap 的绑定，vim 本身的键不列。query 的 NORMAL 下 `?` 也打开它（M3 F3.25 用户定：不用 vim 的反向搜索，`/` 照旧；绑在 `[keys.normal]`，所以 query 的 VISUAL 下 `?` 仍是 vim 的），`<leader>?` 在哪里都能用。这个浮层是作用域 `keyhelp`。按 leader 等前缀停住时弹出的 which-key 本来就按层级列出，比如 `SPC w` 之后列出 `SPC w…` 下的键。

which-key 浮层只是把待输入序列画出来，本身不是作用域：浮层打开时，按键仍按原来的上下文解析，效果与没有浮层时相同；esc 清空待输入序列，浮层随之关闭。所以配置里没有 `[keys.whichkey]` 这张表。

**序列规则**：

- 只是前缀的节点（如 `SPC`、`g`）一直等待，直到按下一个键或 Esc。
- 既是完整绑定、又是更长序列前缀的节点，等待 `timeoutlen`（默认 1000ms）后执行已有的绑定；下一个键接不上任何更长的序列时，立刻执行已有的绑定，再把这个键当成新序列的开头（nvim 的做法，M8 F8.15）。默认键位不会产生这种歧义节点，用户的会：引导页的 `t` 加自己的 `tc`。
- 状态栏显示正在等待的序列（B-03），例如 `SPC`、`g`、`5`。

### 6.6 次数前缀与用户映射

**次数前缀**：NORMAL 下按数字即输入次数。只有在尚未输入次数时，单独按 `0` 才是「移到行首 / 首列」。次数通过 `Args.Count` 传给 Action，表格、树、编辑器的移动都支持，比如 `5j`。

**用户映射**：定义「键 → 键序列」，语义同 vim 的 `noremap`，右侧不会再次展开。映射可以对所有 pane 通用，也可以按 pane 类型单独配置：

- `[map.<模式>]`：所有上下文通用，模式为 `normal` 或 `visual`。
- `[map.<上下文>.<模式>]`：只对某类 pane 生效。上下文有三种：`query`、`grid`（data pane 与 result pane 的表格共用）、`tree`。

```toml
[map.normal]              # 所有 pane 通用
J = "5j"
K = "5k"

[map.query.normal]      # 只在 query：左右各移动 5 个字符
L = "5l"
H = "5h"

[map.grid.normal]         # 只在表格：L 跳到右侧第 5 列，H 回到首列
L = "5l"
H = "0"
```

- 优先级：pane 类型的映射 > 通用映射 > 默认键位，以用户意图为准。
- 右侧按所在 pane 的含义执行：`l` 在 query 里是移动一个字符，在表格里是移动一列。
- 表格和树的默认键位没有占用 H、J、K、L。query 里这几个键本来有 vim 的含义（如 `J` 合并行），用户映射会覆盖它们，与 nvim 的 `nnoremap` 一致。
- 如果要把键绑定到某个操作，而不是一串按键，用 `[keys.<作用域>]`（§6.7）。

### 6.7 配置、冲突、提示、导出（PRD 6.3）

- **加载**：先加载内置的 `default.toml`，再用用户配置覆盖。值写成空字符串表示解绑。
- **冲突**：用户重新绑定默认键属于正常改键，不报冲突。只有一种情况会报（M8 F8.15 起；原来「单键和以它开头的序列同时存在」也警告，F8.15 之后那是 vim 的正常用法，`t` 和 `tc` 并存只有单独按 `t` 才等超时，不再报、`keys --check` 也不算）：
  - 同一作用域内，两个键规范化后相同（如 `<C-p>` 和 `<c-p>`），或者同一个键绑定了两个 Action。

  启动时用浮层列出冲突；`sqlmux keys --check` 发现冲突时以非零状态码退出。
- **标题**：每个有默认键的 Action 都要有标题（含输入框、浮层里的，如 `where.history`、`keyhelp.close`），键位帮助、面板、导出都用它；单测检查 `default.toml` 里每个绑定的 Action 都有标题（M3 F3.17 reviewer 发现）。
- **提示**：`keymap.Hint(actionID, scope)` 返回当前生效的第一个键位的显示形式。界面上所有的键位文字都通过它读取，不写死；未绑定的 Action 不显示提示。
- **导出**：`sqlmux keys` 输出当前生效的键位表（markdown），加 `--format toml` 输出可以分享的配置片段。
  - toml 输出就是一份完整的 `keymaps.toml`（M5 F5.9 起键位和映射单独放在 `~/.config/sqlmux/keymaps.toml`，M1 用户反馈要完整的键位参考）：按作用域分节，每节前一行注释写明这个作用域什么时候生效（§6.4）；**和默认相同的绑定都写成注释行**，值就是默认键，后面注释 Action 的标题，用户去掉 `#` 再改右边才生效——这样新版本改了默认键，不会被一份旧导出盖住（M3 验收期间两次出现）；用户在 keymaps.toml 里改过的（改键、解绑、leader、`[map.*]`）照旧写成生效的行，放在对应默认行的位置，所以升级后重新导出一次，既拿到新版本的完整参考，又不丢自己的改动（用户自己写的注释会丢）；没有默认键的 Action 写成 `# "" = "<action>"  # <标题>`，作用域按 id 前缀对应（`grid.` → grid，`tree.` → tree，`query.` → query，`result.` → result，其余 → normal）；`leader` 和 `[map.<上下文>.<模式>]` 的示例同样注释着放在里面（§6.6）。
  - 同一个键可以在不同作用域各绑一个 Action，按焦点所在的 pane 类型生效，例如 `R` 在表格里是刷新数据、在树里是刷新表列表（§6.4 的解析顺序本来就支持）。

### 6.8 默认键位

| 范围 | 默认键 | 操作 | PRD 原键 |
|---|---|---|---|
| 全局 | `C-p` | 命令面板 | C-k / C-p |
| 全局 | `C-s` | 保存：表格中提交修改，query 中写入文件 | C-s |
| 全局 | `C-c` | 有查询在执行时，取消查询。空闲时连按两次退出：第一次弹出 toast「再按一次 C-c 退出」，显示 2 秒；toast 还在时再按一次即退出，消失后再按，重新算第一次（与 Claude Code、node REPL 的习惯一致）。`:qa` 也可以退出。处在命令行、输入框、浮层、单元格编辑、编辑器的 INSERT / VISUAL，或者编辑器还在等后续按键（`d`、`f` 这类）时，`C-c` 的作用等同 esc（与 vim 一致），不取消查询，也不算作连按两次退出中的一次；要取消查询，在 NORMAL 下按 | 新增 |
| NORMAL | `C-h` `C-j` `C-k` `C-l` | 切换 pane 焦点 | C-a hjkl |
| NORMAL | `SPC s` | session 列表 | C-a s |
| NORMAL | `SPC c` · `SPC n` · `SPC p` · `SPC l` | 新建 · 下一个 · 上一个 · 上次用的 window | C-a c / n / p / l |
| NORMAL | `SPC %` · `SPC "` · `SPC z` · `SPC x` | 左右分割 · 上下分割 · 缩放 · 关闭 pane | C-a % / " / z / x |
| NORMAL | `SPC q` | 按编号跳转 pane | C-a q |
| NORMAL | `SPC b` | 打开 / 聚焦 / 收起侧栏（M19 F19.5）；光标默认留在上次的位置，`[tree] follow = true` 时落在当前 tab 对应的节点（F3.29 原来的行为，M20 F20.4 起变成可选） | C-a b |
| NORMAL | `SPC >` · `SPC <` | 侧栏加宽 · 收窄 4 列（`tree.wider` / `tree.narrower`，带 count 乘倍数；用户 2026-11-09）；侧栏收起时先展开；树里单按 `>` · `<`，`=` 是 `tree.fit`（宽到放下最长的名字，已经是那么宽就回到默认宽度） | 新增 |
| NORMAL | `SPC r` | 显示 / 隐藏结果区（`result.toggle`，F3.28） | 新增 |
| NORMAL | `gt` · `gT` | 下一个 · 上一个 tab | 同 |
| 表格 / 树 / 引导页 | `:` | 打开命令面板的「所有」范围（M5 F5.8 起，同 `C-p`；原来进命令范围，想要原来的就把 `:` 绑到 `palette.command`）；`:q`、`:qa`、`:w` 照常可用（§12）。M3 起绑在 `[keys.grid]`、`[keys.tree]`、`[keys.landing]`，不在 normal：query 里的 `:` 要交给编辑器自己的命令行（§11） | 同 |
| 表格 / 树 / 引导页 | `;` | 打开命令面板并直接进入 SQL 范围（§12）。绑法同 `:`，query 里 `;` 是 vim 的「重复 f/t」 | 新增（M1 用户反馈） |
| 引导页（`landing`） | `t` · `c` | 打开表（`tab.table`）· 新建 query（`query.new`） | 新增（M3） |
| 表格 | `hjkl` `gg` `G` `0` `$` | 移动，支持次数前缀；`{N}G` / `{N}gg` 跳到第 N 行（F3.36） | hjkl |
| 表格 | `↵` · `i` | 编辑单元格 | 同 |
| 表格 | `R` · `T` · `x` · `Tab` / `S-Tab` | 刷新 · 转置 · 关闭 tab（有未保存修改时需确认）· 右移 / 左移一格（M8 F8.6，所有行；新增行的编辑中 `Tab` 照旧提交并右移） | 同 |
| 表格 | `/` | 聚焦 WHERE 输入框 | 未定义 |
| 表格 | `go` `gl` `gp` `gc` | 打开 ORDER / LIMIT / PAGE / COLS 下拉 | 未定义（Q-03） |
| 表格 | `>` `<` `=` | 光标所在列加宽 / 收窄 N 列、全部列回自动宽度（M17 F17.14，§7.8「列宽」；用户定的键，原来是 `C-w >` / `C-w <` / `C-w =`） | `C-w >` / `C-w <` / `C-w =` 调窗口宽度 |
| 表格 | `]` · `[` | 下一页 · 上一页 | 未定义 |
| 表格 | `yy` · `yl` · `p` · `yi` · `Y` | 复制整行 · 复制单元格 · 把复制的行粘贴成新行 · 把行复制为 INSERT 语句（F3.32 起 `yy` 从单元格改为整行）· 把行复制为 JSON（M9 F9.5） | y / yi |
| 表格 | `0` / `^` · `$` | 首列（`^` 是 M9 F9.3 加的，用户要映射 `_`）· 末列 | 0 / $ |
| 表格 | `r` · `o` · `dd` | 撤回这一格的修改 · 新增一行 · 标记 / 取消删除这一行（§10.1、§10.6） | 新增（M2/M3 验收） |
| 表格 | `e` · `*` · `gs` / `gS` · `ge` | 在值编辑器中打开这一格（§10）· 按这个值过滤 · 按这一列排序 / 加入排序 · 导出下拉（§7.6）（M12 F12.2–F12.4；`e`、`ge` result 同） | 新增（M12） |
| ORDER 浮层 | `space` · `a` · `A` | 加进或翻转 · 移出 · 清空（M12 F12.1 是下拉里 `space` 循环三态，F12.9 改成列表） | 新增（M12） |
| 表单 | `C-t` | 测试连接（§7.8，M12 F12.5） | 新增（M12） |
| NORMAL | `SPC wn` · `SPC wp` | 把当前 tab 移到下一个 · 上一个 pane（§7.8，M12 F12.5） | 新增（M12） |
| 表格 | `m` · `v` / `V` · `s` | 标记 / 取消标记光标所在的记录 · 进 VISUAL 扩选（§7.6「选择」）· flash 式跳转（§6.10）（M10 F10.7、F10.8；result 同） | 新增（M10） |
| NORMAL | `?` | 键位帮助（§6.5），所有 pane 通用，绑在 `[keys.normal]`（F3.25 起；原来在 grid / tree / landing 各绑一个） | 新增（M2/M3 验收） |
| NORMAL | `<leader>?` | 键位帮助，哪里都能用 | 新增（M2/M3 验收） |
| result | `P` · `q` | 固定当前结果 tab · 关闭当前结果 tab | P / q（原为固定 / 关闭 pane） |
| result | `]` · `[` | 结果的下一页 · 上一页，只对被自动 LIMIT 的语句有效（M10 F10.4，§11） | 新增（M10） |
| schema 树 | `j` `k` `gg` `G` | 移动，支持次数前缀，与表格一致 | j/k |
| schema 树 | `↵` · `t` · `/` · `R` · `l` · `h` | 打开（表）或展开 / 折叠（其他节点）· 打开（F3.37 起和 `↵` 相同）· 过滤 · 刷新 · 展开 · 折叠或到父节点（§7.8） | ↵ / C-↵ / / ；R、l、h 为新增 |
| schema 树 | `s` | flash 式跳转（M10 F10.8，§6.10） | 新增（M10） |
| query | `↵`（NORMAL / VISUAL） | 执行光标所在语句 · 执行选区 | ⌥↵ |
| query | `gq{移动}` · `gqq` | 格式化移动碰到的语句 · 当前语句；VISUAL 下格式化选区。`gq` 是编辑器的操作符，不在 `[keys.query]` 里 | 同 |
| query | `gs` | 打开 schema 下拉框（仅 PG，§8.6） | 新增 |
| query | `s` | flash 式跳转（M10 F10.8，§6.10）；覆盖 vim 的 `s`，和 flash.nvim 一样，`cl` 照旧；按键只在 NORMAL 下进跳转，VISUAL 下的 `s` 仍是 vim 的 | 新增（M10） |
| 命令面板 | `C-t` | 打开表（F3.37 起和 `↵` 相同）；快速 SQL 的结果送到 result pane | C-↵ |
| 快速 SQL | `C-y` · `C-e` | 复制为 CSV · 在 query 中打开（写语句也走这条路） | C-y / C-e；取消 C-S-↵ |
| 单元格编辑 | 见 §10.2 | 时间分段调整、选项选择 | 新增 |
| 各浮层 | 见下表 | 各浮层内的移动与选择 | 同 PRD |

**默认键位只放常用的**（M0 用户反馈）。其余操作都能在命令面板里搜到并执行；想要快捷键的，在 `keymaps.toml` 里自己绑定（M5 F5.9 起，原来在 `config.toml`），例如：

```toml
[keys.normal]
"<Leader>h" = "pane.focus.left"   # 用 SPC h 切焦点
"<Leader>L" = "pane.resize.right"  # 用 SPC L 调整大小
"<Leader>," = "window.rename"
```

不提供「切换到第 n 个 window」这类按编号的命令：window 用 `SPC n` / `SPC p` / `SPC l` 切换，或者在命令面板的 window 范围里搜，也可以点击状态栏上的 window 名。

M5 新增的命令都不带默认键，在命令面板里执行：`conn.new`「新建连接…」、`query.conn`「切换连接…」、`tx.mode`「手动提交事务」（开关类，手动时为 ON）、`tx.commit`「提交」、`tx.rollback`「回滚」。

M5 验收后加的命令同样不带默认键（F5.16，用户在 keymaps.toml 里自己绑）：

- `pane.focus.next`「下一个 pane」：按屏幕上的位置顺时针切到下一个 pane（用户定）：以整个界面（含状态栏）的中心为圆心，各 pane（含树和结果区）按自己中心点的角度、从 12 点方向起顺时针排，从当前焦点所在的那个往下一个，到最后一个再回到第一个；角度完全相同时按 tmux 的编号顺序（从上到下、从左到右）。树收起时、结果区隐藏或还没出现时不算在内。要满足的例子：默认布局「树 · data · query，下面结果区」是 树 → data → query → 结果区 → 树；一排满高的 `[树|1|2|3]` 是 树 → 1 → 2 → 3 → 树（F5.16 审查时定：圆心含状态栏，满高的 pane 中心比圆心高半行，不再同角度）。
- `query.format`「格式化整个 query」：对整个 query 做格式化，规则同 `gq`（§9.5），算一次修改、`u` 一次撤销；光标留在原来的行号和列号（超出时夹到末尾；M8 F8.3，原来移到语句开头，用户不要光标动）。VISUAL 下格式化选区照旧用 `gq`。
- M8 F8.11–F8.13 新增、都不带默认键：`session.rename`「重命名 session…」、`query.rename`「重命名 query…」（状态栏单行输入，同 `window.rename`；三种名字各自不能重复）、`session.close`「关闭 session」（同 session 列表里的 `x`）、`palette.table`「命令面板：表」（打开面板并输入 `@`）、`palette.session`「命令面板：session」（输入 `$`，M9 F9.9）、`session.next` / `session.prev` / `session.last`（M9 F9.10，默认 `SPC )` / `SPC (` / `SPC L`，照 tmux；按列表顺序绕回，last 是上一次 attach 的）、`conn.manage`「管理连接…」（§7.8「连接管理」，F8.16）。
- `tab.quit`「关闭 tab，空了连 pane / window 一起关」：先照 `tab.close` 关掉当前 tab（有未保存的修改照样确认）；pane 里没有 tab 了、window 里还有别的 pane，就关掉这个 pane；window 里只剩这一个空 pane（结果区不算），就关掉这个 window，结果区和里面的结果 tab（含固定的）一起关、不另外确认，同 `window.close`；是 session 里最后一个 window 时不关，留下引导页，和 `tab.close` 一样。
- `tab.reopen`「重开关掉的 tab」：每个 session 在内存里记最近关掉的 20 个表和 query tab（引导页不记，重启后不留），每次重开最近的一个，再按往前一个。开回原来的 pane，那个 pane 已经没了就开在当前 pane；表恢复关掉时的 WHERE、ORDER、LIMIT，query 打开原来的文件、用原来的连接和 schema；这个文件已经在别处开着时切过去，在别的 session 里就 attach 那个 session 再切过去（F5.16 审查时定）。这张表在那个 pane 里已经开着时（F3.37 一张表在一个 pane 里只有一个 tab），只切过去。关掉时放弃了的未保存修改不恢复。
- `session.new`「新建 session」：和 session 列表里的 `n` 相同，在哪里都能执行。
- `palette.window`、`palette.tab`（F5.19）：打开面板的「窗口」/「Tab」范围（§12）。

query 里的 `x` 是 vim 的删除字符，所以 query 的 tab 用 `:q` 关闭。

各浮层内的键位如下，全部取自 PRD。等到相应的浮层实现时，再加进 `default.toml`。

| 浮层 | 键 | 操作 | PRD |
|---|---|---|---|
| WHERE 历史 / 收藏下拉（`where`） | `C-r` · `C-n` / `Tab` / `↓` · `C-p` / `S-Tab` / `↑` · `↵` · `C-e` · `C-f` · `esc` | 打开（表格上按也先进 WHERE 再打开，M9 F9.4）· 下一项 · 上一项（到头绕回）· 应用 · 填入输入框不执行（M9 F9.1）· 收藏或取消收藏（输入框里没开下拉时也能按，收藏当前条件并 toast，M9 F9.2）· 关闭 | Q-02 |
| COLS 下拉（`cols`） | `/` · `j` / `k` · `space` · `a` · `A` · `J` / `K` · `esc` | 聚焦过滤框 · 移动 · 勾选 · 全选 · 全不选 · 下移 / 上移这一列（M10 F10.3）· 先清空过滤，再按一次关闭。打开时焦点在列表上，PRD 写的「打开时焦点在过滤框」与这些键冲突，按这里做（§18） | Q-04 |
| session 列表（`sessions`） | `j` / `k` · `l` / `h` · `↵` · `n` · `x` · `$` · `esc` | 移动 · 展开 / 收起（session 下面是它的 window）· attach（停在 window 上时还切到这个 window）· 新建 session（M10 F10.2 起先要名字）· 关闭 · 重命名 · 关闭列表。`n` 原为新建连接：M5 起 session 是工作区，新建连接改为命令 `conn.new`（§18） | S-03 |
| 命令面板（`palette`） | `↑` / `↓` 或 `C-n` / `C-p` · `Tab` / `S-Tab` · `↵` · `C-t` · `C-y` · `C-e` · `C-f` / `C-b` · `esc` | 移动 · 切换范围 · 执行 · 打开表（同 `↵`）/ 把结果送到结果区 · 复制 CSV · 在 query 中打开 · DDL 预览翻半页（M9 F9.7；`C-u` 仍是输入框的清行）· 关闭 | K-01~K-04、F-04 |
| 通用下拉（`dropdown`）：schema、LIMIT 等（ORDER M12 F12.9 起不是） | `C-n` / `C-p` / `↑` / `↓` · `↵` · `esc` | 移动 · 选中 · 关闭。输入直接进过滤框，所以不用 j/k。Action 为 `dropdown.up/down/select/close` | §8.6 |
| JSON 视图（`jsonview`，M14 F14.5） | `esc` / `q` · `s` · `Y`；翻页、刷新是表格的键 | 回表格（`json.leave`）· flash 式跳转（§6.10，M16 F16.8）· 复制整个 JSON 到系统剪贴板（`json.yank`，M16 F16.9）；翻页、刷新走表格的 `grid.page.*` / `result.page.*` / `grid.refresh`（M15 F15.9 起，原来是 `jsonview` 自己的 `json.page.*` / `json.refresh`）；进入和退出都是表格的 `gj`（`grid.view.json`，原来 `gJ`） | §7.6 |
| ORDER 浮层（`order`，M12 F12.9） | `/` · `j` / `k` · `space` · `a` · `A` · `↵` · `esc` | 聚焦过滤框 · 移动 · 加入或翻转方向 · 移出 · 清空 · 只按这一列并关闭 · 先清空过滤，再按一次关闭。Action 为 `order.filter/down/up/toggle/remove/clear/only/close` | §7.6 |
| 补全列表（`complete`） | `Tab` / `C-n` / `↓` · `S-Tab` / `C-p` / `↑` | 下一项 · 上一项，到头绕回；弹出时第一项已选中；列表滚动照 nvim 的补全弹窗，选中项在视图里就不动、出了视图才滚（M13 F13.10，原来有的列表把选中项钉在最底行）。`↵`（接受，文字不变时照常执行）和 `esc`（第一次关列表）由输入框处理，不在这个作用域里 | §9.7 |
| 确认框（`confirm`） | `y` / `↵` · `n` / `esc` | 确认 · 取消；`C-c` 等同 `esc`；两个按钮都能点击 | §10.5 |
| 跳转（`jump`） | `esc` · `backspace` · `↵` | 退出 · 删一个字符 · 跳到当前匹配；标签字母由跳转自己处理（M10 F10.8，§6.10） | 新增（M10） |
| 新建连接表单（`form`） | `Tab` / `S-Tab` · `↵` / `C-s` · `esc` | 下一个 · 上一个字段（到头绕回）· 保存 · 取消；`C-c` 等同 `esc`。开关字段（engine、read_only）上的 `space` 由表单自己处理、不进作用域，文字字段里照常输入空格 | 新增（M5 F5.1） |

### 6.9 非 vim 模式（M6 F6.6）

用户只用 vim 模式，但要照顾不用 vim 的人（2026-09-30）。`config.toml` 里 `vim = true | false`，默认 `true`。关掉之后改的只有两处，其余界面、命令、鼠标都不变：

**1. query 和 WHERE 是没有模式的普通编辑器**（F6.6a，`internal/editor` 里的另一层，和 vim 层共用缓冲区、撤销栈和绘制）：

- 一直可以输入；方向键移动；`S-←/→/↑/↓` 和鼠标拖动选择（query 和 WHERE 都能拖），选区用 `visual` 底；再输入就替换选区，输入自动配对的开括号或引号时把选区包起来（同 VS Code 的 autoSurround、DataGrip，F6.6 审查时定，§7.9）。`C-a` 行首、`C-e` 行尾（query 里 `C-e` 是执行，行尾没有默认键，想要的自己绑），`C-w` 删到词首（同 §7.9），`Backspace` / `Delete` 删一个字素或删掉选区，行首缩进里 `Backspace` 一次退一个制表位；`Tab` 补到下一个制表位（`tab_width` 一档），有选区时 `Tab` / `S-Tab` 是缩进 / 反缩进这几行（走编辑器的 `>` / `<`）。`C-z` 撤销、`C-y` 重做，撤销步在按方向键、`↵`、粘贴、格式化处断开，连续打字算一步；重做后光标放在重做内容的末尾、不恢复选区（同 VS Code / DataGrip；vim 模式照 vim，光标回这一步开始的地方），撤销后的光标两边一致。`C-c` 取消选区、关补全列表，没有可取消的时照旧算退出的第一次。Home / End、Alt+方向键这类终端分不清的键不作默认（§6.2），用 `C-a` / `C-e`。
- query 里 `↵` 换行，执行用 `C-e`（`query.run`，F6.6b 的键位层改绑；标题上的 `▶ run` 按钮照旧可以点）；有选区时执行选区，否则执行光标所在语句。`:` 只是字符：关 tab 用 tab 栏的 `×` 或面板的「关闭 tab」；格式化用面板的「格式化整个 query」，有选区时格式化选区。
- WHERE 里 `↵` 执行，`C-r` 历史 / 收藏下拉照旧；`esc` 回表格。
- 补全：弹出、`Tab` / `S-Tab` 移动、`↵` 接受都和 vim 模式相同；`esc` 只关列表，没有模式可退。
- 复制、粘贴不另配键：终端自己的选择和 `Cmd-V` 就是非 vim 用户的习惯；寄存器只在 vim 模式下有。
- 编辑器里没有 NORMAL 上下文：leader、`gt` / `gT`、`?`、`[map.*]` 在编辑器里都不生效（`SPC` 是要打的字）。能「到」编辑器的只有两类绑定：Ctrl 组合，和 `[keys.global]`；`Tab`、退格、`↵`、方向键、`esc`、`F5` 这类非 Ctrl 的绑定在这里不生效，免得编辑键被 NORMAL 的默认绑定截走。用户绑的 Ctrl 键优先于编辑器自己的 `C-a` / `C-w` / `C-z`。`C-h` 例外：§6.2 说它在一些终端里就是退格，plain 编辑器面向的正是这类用户，所以在编辑器里当退格、不切焦点，`C-j` / `C-k` / `C-l` 照旧。表格、树、引导页上这些照常。状态栏不显示模式块。界面上的键位提示在 `vim = false` 时优先取 novim 层的键（`↓`、`PgDn`、`Del`），plain query 的 tab 栏不画 `gt` / `gT`。
- 编辑器行为不再对照 nvim，nvim 差分测试不适用，用普通单测。

**2. 一套叠加的非 vim 默认键位**（F6.6b，`default-novim.toml`，`vim = false` 时叠在 `default.toml` 上，同一套解析、冲突检查和导出；`sqlmux keys` 按配置导出叠加后的结果）。M16 F16.6 起表格和树的方向键（`←↓↑→` = `grid.left/down/up/right`、`tree.up/down/expand/collapse`）挪进 `default.toml`，两种模式都有（nvim 的 NORMAL 本来就认方向键；用户 2026-10-14：vim 模式下方向键不动光标，不用 vim 的人也会碰到）；`PgUp` / `PgDn` / `Delete` 仍只在非 vim 层：

| 作用域 | 加的键 | 操作 |
|---|---|---|
| grid / tree | `←` `→` `↑` `↓` | 移动（hjkl 照旧也在：表格和树里字母不会被当成输入，留着不碍事） |
| grid | `PgUp` `PgDn` | 上一页 / 下一页（`[` `]` 照旧；Terminal.app 吃掉这两个键的人用 `[` `]`） |
| grid | `Delete` | 标记 / 取消删除这一行（`dd` 照旧） |
| query | `<CR>` 解绑、`<C-e>` | 换行交给编辑器；`C-e` 执行（所以 plain query 里 `C-e` 不是行尾） |

这一层允许用 PgUp / PgDn / Delete：它们正是非 vim 用户要的键，§6.2 的「默认键位不用」只管 `default.toml`；检查「只用处处可用的键」的单测对这一层放开这三个键，其余限制照旧。

两个键的对应 `yy` / `dd` / `gg` / `go` 这些照旧可用：它们在表格和树里不会打进输入框，which-key 也会提示。所有有标题的 Action 本来就都在命令面板里，不用再为「只有 vim 键能触发的操作」另加命令；编辑器里的 `gq`、`"+y` 对应 `query.format` 和终端自己的复制。

### 6.10 flash 式跳转（M10 F10.8）

照 [flash.nvim](https://github.com/folke/flash.nvim) 的 jump 模式做，用户要的是「`s` 跳到屏幕上任意一个光标在 NORMAL 下能停的字符」。实现上复用命中表（§7.4）：标签就是一次代替鼠标的点击。

- **进入**：表格、结果区、树、query、JSON 视图（M16 F16.8）的 NORMAL 下按 `s`（`jump.start`）；JSON 视图在 VISUAL 下按 `s` 也跳（只读视图里 vim 的 `s` 没用，flash 的可视模式 `s` 本来就是跳转；F16.8 审查时定）。作用域 `jump`（§6.4），模式块显示 JUMP（`info` 色）。`esc` 退出、光标不动；`backspace` 删掉模式的最后一个字符，删到空仍在 JUMP；`↵` 跳到当前匹配；其余可打印字符追加到模式，粘贴也追加到模式（表格和 query 一样，不写进 query）。只有一个匹配也不自动跳（flash 的 `jump.autojump = false`）。模式打到一个匹配都没有时直接退出 JUMP、光标不动、这个字符丢掉，不留在空的遮罩里（M15 F15.7，用户验收时对照 nvim 的 flash.nvim 定：原来全黑、删字符才回来、还看不见已打的前缀）。从命令面板执行 `jump.start` 同样进 JUMP，query 在 VISUAL 时也进（flash 在可视模式下的 `s` 本来就是跳转），不交给 vim 的 `s`；「VISUAL 下 `s` 仍是 vim 的」只对按键生效。
- **匹配**：模式非空后，在整个屏幕的可跳区域里找精确子串（flash 的 `search.mode = "exact"`、`multi_window = true`），smartcase：模式全小写不分大小写，含大写就区分。可跳区域是命中表里光标能停的目标：单元格（`cell`，含结果区的表格）、query 和 JSON 视图的文字、树的节点行；chip、标题、按钮、输入框不算。同一个目标只取第一处匹配。当前匹配是焦点 pane 里离光标最近的那个。
- **标签**：字母表 `asdfghjklqwertyuiopzxcvbnm`，去掉任一匹配后面紧跟的那个字符，这样继续打字不会被当成标签；小写用完用大写（`label.uppercase = true`）。焦点 pane 里的匹配按离光标的距离先分配（`label.distance`），其余按屏幕顺序；分不到标签的匹配只高亮，继续打字收窄。标签画在匹配之后一格、覆盖那个字符（`label.after = true`），样式同 `yank`（`warn` 底、`bg` 字）；匹配的字符 `match` 色；pane 区域里其余内容的前景降成 `dim`（flash 的 backdrop）。遮罩只盖 pane 区域，不盖状态栏，模式块的 JUMP 要看得清。
- **跳**：按标签字母就执行那个位置的点击动作并退出 JUMP：单元格是聚焦 pane 并 `grid.goto`；树是光标到那个节点，不展开、不打开；query 是聚焦并把光标放到那个字符，和单击一样。query 里只是移动，不做 operator-pending（`ds…`）和远程操作，也不做 treesitter 模式。
