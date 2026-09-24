# DB TUI 技术方案

> 状态：草案，待评审 · 2026-09-23（第七版）
> 对应 [PRD - DB TUI v0.0.2](https://claude.ai/design/p/e6118404-bf7a-4420-abfd-e31198df2ba8?file=PRD+-+DB+TUI+v0.0.2.dc.html) 与 [设计稿 DB TUI v0.0.2](https://claude.ai/design/p/e6118404-bf7a-4420-abfd-e31198df2ba8?file=DB+TUI+v0.0.2.dc.html)
> 产品名：**sqlmux**。GitHub 上只有一个 2014 年后就没再更新的同名 Erlang 仓库，Homebrew 里没有同名软件。
> 文中的需求编号（S-01、G-03、F-05 等）指 PRD 条目。对 PRD 的改动建议汇总在 §18。

## 0. 结论速览

| 项 | 决定 |
|---|---|
| 语言 / 框架 | Go 1.26 + Bubble Tea v2 + Lip Gloss v2 + Ultraviolet（已确认） |
| 渲染 | 立即模式：每帧把画面画进一块 cell 缓冲区，同时生成鼠标命中表 |
| 输入 | 键盘、鼠标、命令面板统一转成 Action。默认键位只用所有终端都能区分的键，不依赖 kitty 键盘协议，也不假设用户在用 tmux |
| 键位模型 | 不做 tmux 式的全局前缀键。NORMAL 模式下用空格（`SPC`）作 leader，按下后弹出可点击的键位提示（已确认） |
| 数据库 | pgx/pgconn、go-sql-driver/mysql；每个 session 两条连接；所有值按文本处理 |
| SQL 处理 | 自研词法扫描器，负责分句、高亮、读写判定、自动 LIMIT |
| SQL 格式化 | 通过 goja 在进程内运行 sql-formatter（JS 库），已做可行性验证 |
| SQL 编辑器 | 自研 vim 编辑器，覆盖 nvim 的基础操作：文本对象、操作符加移动、大小写转换、注释、撤销与重做、`:s` 替换。不做宏和 `.` 重复。键位映射（如 J → 5j）可以按 pane 类型分别配置。行为用 nvim 做差分测试来校验 |
| 补全与模糊匹配 | console 和 WHERE 输入时提供 fzf 式的模糊补全。全项目的模糊匹配都直接使用 fzf 的匹配算法 |
| 单元格编辑 | 按列属性提供选项：时间分段调整、NULL、DEFAULT、布尔、枚举 |
| WHERE | 不做简写语法，直接输入完整的 SQL 条件表达式 |
| 结果展示 | 每个 window 底部一个全宽结果区，内部用 tab 区分：日志、各 console 的结果、已固定的结果（已确认） |
| 范围 | PostgreSQL + MySQL。Redis 暂缓，只在 session 层留出位置 |

## 1. 范围

- 覆盖 PRD 第 5 章全部功能需求（Redis 相关除外），以及第 6、7、8 章的键位、鼠标和视觉规范。键位的调整见 §6.8 和 §18。
- 在 PRD 之外新增：console 内的 SQL 模糊补全（§9.7）。PRD 原本只要求 WHERE 输入框补全字段名。
- 不做：PRD 1.4 列出的非目标；Redis 的 key 浏览（布局未定）；表格多选和批量编辑（PRD 已标为后续版本）。

## 2. 技术选型

### 2.1 选型记录：Go + Bubble Tea v2（已确认）

选型依据：

- **团队熟悉 Go。**
- **框架能力足够。** Bubble Tea v2 和 Lip Gloss v2 原生支持：
  - 按 cell 读写的缓冲区；
  - 带 z 序的图层和点击命中测试；
  - 带坐标的鼠标事件，包括悬停；
  - 约束布局（`ultraviolet/layout`，与 ratatui 的 Layout 同构）。

  版本线为 bubbletea v2.0.9、lipgloss v2.0.6。
- **性能不是瓶颈。** 数据库 TUI 只在有输入或数据回来时才重绘，画一帧 200×60 的界面远不到 1ms。真正影响体验的是数据量，靠两点解决：表格只渲染可见行（§7.6），结果集设行数上限（§11）。

Go 生态相对 Rust 缺三样东西，对策如下：

| 缺口 | 对策 |
|---|---|
| vim 编辑器组件 | 自研，覆盖 nvim 的基础操作（§11）。用 nvim headless 生成期望结果，做差分测试（§15） |
| SQL 词法分析 | 自研扫描器，约 300 行，配表驱动测试（§9.1） |
| SQL 格式化 | 在进程内运行成熟的 JS 库 sql-formatter，已验证可行（§9.5） |

### 2.2 limoni 评估：不采用

[thebanri/limoni](https://github.com/thebanri/limoni) 主打零分配渲染、3D、图表，以及可被 AI 驱动的语义树，和本项目的需求重合不多。关键问题是还不成熟：

- 仓库创建于 2026-08-06，写本文时约 7 周，55 个 star，205 次提交里 194 次来自同一位作者，版本 v0.8.1。
- 未关闭或刚修复的问题，恰好落在本项目依赖的交互上：
  - #48：指针移动被当成 `MousePressMsg`，悬停无法实现；
  - #49：`MouseWheelMsg` 不带坐标；
  - #19：没有可拖动分隔线的 SplitPane；
  - #18：没有 StatusBar；
  - #47：点击事件不会路由到控件，2026-09-21 才修复；
  - #53：输入事件循环还没在真实 PTY 上测试过。

## 3. 总体架构

```
 KeyPressMsg ─► keymap 解析 ──┐
 Mouse*Msg ──► 命中表查找 ────┼─► Action ─► Update（单线程）─► 状态树
 命令面板选中 ────────────────┘                   │   ▲
                                                 ▼   │ resultMsg{tab, seq}
                                             tea.Cmd ─► db.Worker（每条连接一个 goroutine）

 View(): 状态树 ─► Frame（cell 缓冲区 + 命中表）─► Bubble Tea 差分输出
```

四条原则：

1. **只有一棵状态树，只在 Update 里修改。** IO 放在 `tea.Cmd` 里执行，结果以 Msg 的形式回到 Update。
2. **所有操作都是 Action。** 键位、鼠标点击、命令面板最终都落到同一个 Action 上，满足 PRD 第 2、7 章「鼠标和键盘不存在两套逻辑」的要求。
3. **模式由状态推导，不单独存储。** 编辑单元格或输入框获得焦点即为 INSERT；console 的模式就是编辑器自身的模式；命令面板打开即为 COMMAND（B-02）。
4. **视图是状态的纯函数。** 命中表是渲染的副产品，每帧重建。

## 4. 目录结构

```
sqlmux/
├─ cmd/sqlmux/main.go       参数解析、子命令（keys）、启动
├─ internal/app/            Model/Update/View、Action 注册表、工作现场模型、布局树
├─ internal/ui/             Frame、Block、主题、命中表、模糊匹配（fzf 算法的封装）、各组件绘制（grid/tree/tabs/statusline/palette/popup/cell editor/补全列表）
├─ internal/keymap/         键位记法解析、作用域 trie、次数前缀、用户映射（按 pane 类型）、冲突检测、提示查询、default.toml（embed）
├─ internal/editor/         vim 编辑器：缓冲区、移动/文本对象/操作符、撤销、绘制；testdata/ 存放 nvim 差分用例
├─ internal/sqlkit/         词法扫描、分句、读写判定、自动 LIMIT、WHERE 拼接校验、补全上下文判断、格式化（内嵌 sql-formatter）
├─ internal/db/             Conn/Engine 接口、Worker、postgres/、mysql/、catalog 查询
├─ internal/config/         config / connections / state 的读写与 XDG 路径
└─ docker-compose.yml       集成测试用的 postgres 与 mysql
```

依赖：

- 界面：`charm.land/bubbletea/v2`、`charm.land/lipgloss/v2`、`github.com/charmbracelet/ultraviolet`。不用 bubbles 的 textinput：它按 rune 删字，退格会把 é、👍🏽 这类字素簇拆开（M1 核对时实测）。单行输入框在 F0.4 命令行的输入处理上扩展。
- 数据库驱动：`github.com/jackc/pgx/v5`、`github.com/go-sql-driver/mysql`
- 格式化：`github.com/dop251/goja`
- 模糊匹配：`github.com/junegunn/fzf/src/algo`
- 其他：`github.com/BurntSushi/toml`
- 命令行参数：用标准库 `flag`。目前只有 `keys` 一个子命令，加上 `--debug`、`--format`、`--check` 几个参数，不需要引入 kong 这类 CLI 库；等子命令多起来、或者需要 shell 补全时再换。

## 5. 核心数据模型

对应 PRD 第 3 章的 session / window / pane / tab 四级结构。

```go
type Workspace struct {
    Sessions []*Session
    Active   int
}

type Session struct {           // 1 session = 1 连接（host + 用户 + 库）
    Name      string            // 默认 host/db
    Spec      ConnSpec          // 来自 connections.toml
    Main      *db.Worker        // 主连接：console、保存修改；持有事务状态
    Meta      *db.Worker        // 辅连接：元数据、计数、DDL 预览、快速 SQL
    ReadOnly  bool
    Tx        TxMode            // auto | manual
    Schema    string            // schema 树当前所在的 PG schema；console 各自另选（§8.6）
    Catalog   *Catalog          // 表、列属性、主键、行数估计的缓存
    Windows   []*Window
    ActiveWin int
    RunSeq    int               // result 标题里的执行序号，如 #42
}

type Window struct {
    Name     string
    TreeOpen bool               // ⟨0⟩ schema 树是固定侧栏，不参与分割（D-04）
    Root     *Node              // 其余区域的分割树
    Focus    PaneID
    Zoom     PaneID             // 0 = 未缩放（P-03）
}

type Node struct {              // 二叉分割树
    Split Dir                   // None 表示叶子
    Ratio float64               // A 所占比例
    A, B  *Node
    Pane  *Pane
}

type Pane struct {
    ID        PaneID            // 内部稳定 ID；显示的 ⟨n⟩ 按树的遍历顺序计算
    Kind      PaneKind          // data | console | result
    Tabs      []Tab
    Cur, Prev int               // tab 栏中的 * 和 -（T-01）
}
```

Tab 的种类：

- `DataTab`：表引用、`Query{Where, Order, Limit, Page}`、隐藏列集合、`GridState{Row, Col, Scroll, Transpose}`、当前页数据、`Edits map[CellKey]Edit`（修改后的值为文本、NULL 或 DEFAULT）、总行数（-1 表示未知）、请求序号 `seq`。
- `ConsoleTab`：文件路径、`*editor.Buffer`、所选的 schema（仅 PG，§8.6）、它在结果区里的结果 tab。
- `ResultTab`：来源 console、执行序号、`*db.Result`、`GridState`、是否固定。

默认 window 的布局：

```
Window
├─ 侧栏 ⟨0⟩ schema（可折叠）
└─ Node(纵向, 0.6)              ← 第一次执行 SQL 时，把原来的根节点包进一个新的纵向节点
   ├─ Node(横向, 5:4)          ← data 与 console 的宽度比，取自设计稿（§7.8）
   │  ├─ Pane ⟨1⟩ data
   │  └─ Pane ⟨2⟩ console
   └─ Pane ⟨3⟩ result           底部全宽结果区（§11）
```

布局树的操作写在 `internal/app/layout.go` 里，都是纯函数，可以单测：

- **分割**：把叶子替换成 `Node{A: 原叶子, B: 新叶子}`。新 pane 是与原 pane 同类型的空 pane（没有 tab，样式见 §7.8），焦点移到新 pane。
- **关闭**：用兄弟节点替换父节点。
- **调整大小**：修改该方向上最近的祖先分割节点的 `Ratio`。
  - 每按一次移动 5%，乘以次数前缀（`3 SPC L` 移动 15%）；比例限制在 10%–90%。
  - 方向与 tmux 的 resize-pane 相同：`H` 把分割线往左移，与当前 pane 在分割线的哪一侧无关。
- **缩放**：pane 占满状态栏以上的整个 window，侧栏的位置也被占用（P-03「缩放至整个 window」）。分割、关闭、按编号跳转都会退出缩放。从命令面板聚焦 pane、或者把表打开到某个 pane 时也一样：目标 pane 被缩放遮住时，先退出缩放再聚焦；目标就是正在缩放的 pane 时，保持缩放。
- **按方向切焦点**：用上一帧的 pane 矩形，在该方向上找相邻、且有重叠的 pane。
  - 有多个候选时，选最近获得过焦点的那个；都没获得过焦点时，选遍历顺序靠前的（上 / 左）。这与 tmux 的 `window_pane_choose_best`（取 `active_point` 最大者）一致。
  - 选它而不选「重叠最长」，是因为这样 C-l 之后再 C-h 总能回到原处。更重要的是默认布局：从底部全宽的结果区按 C-k，会回到刚才执行查询的 console，而不是更宽的 data。
  - 与 tmux 不同的一点：到了边上不绕到另一侧。侧栏固定在最左边，vim 的 `<C-w>h` 也不绕回。
- **按编号跳转**（`SPC q`）：编号一直显示到下一个按键，不自动消失（相当于 tmux 的 `display-panes -d 0`）。按数字跳到对应的 pane；按其他键只关闭编号，这个键不再执行别的操作。
  - 鼠标：编号也是键位提示，按 §7.4 可以点击。点击某个 pane（含侧栏 ⟨0⟩），等于按下它的编号；点击 pane 以外的地方（状态栏、间隔）只关闭编号。

默认 window 名为 `data`。各个 pane 随里程碑逐步出现：M1 只有侧栏和 data，data 占满其余宽度；console 在 M3 加入默认布局，结果区在第一次执行 SQL 时出现。

所有持久化字段都是普通值类型，运行时句柄（连接、编辑器实例）不进入序列化结构。所以以后如果要支持工作现场恢复（PRD 10.2 #6），只需要补上读写逻辑。

## 6. 输入系统

### 6.1 Action

```go
type Action struct {
    ID    string                        // "pane.split.right"
    Title string                        // 命令面板和 which-key 显示的名字
    Scope string                        // 可用范围，用于提示与面板过滤
    Run   func(*App, Args) tea.Cmd      // Args 含参数与次数 Count
    State func(*App) *bool              // 开关类命令返回当前值（K-04 的 ON/OFF）
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
- **兜底**：键盘不方便的操作，都能用鼠标（§7.4）或命令面板完成。

### 6.3 键位记法

配置里用 vim 记法：`<C-p>`、`<Space>`、`<Esc>`、`<S-Tab>`、`<CR>`；字符序列直接写，如 `gt`、`gq`、`%`。

界面显示成 PRD 的风格：`C-p`、`SPC`、`esc`、`↵`。

### 6.4 作用域与解析

| 作用域 | 何时生效 |
|---|---|
| `palette` / `where` / `cols` / `schema` / `sessions` / `complete` | 对应的浮层打开时，取最上层的一个。which-key 浮层不是作用域（§6.5） |
| `cell` | 正在编辑单元格（INSERT） |
| `input` | 任意单行输入框获得焦点（INSERT） |
| `result` | result pane 获得焦点时生效，优先级在 `grid` 之上（如 `P`、`q`）；其余按键落到 `grid` |
| `grid` / `tree` / `console` | 对应控件获得焦点。`console` 只含应用层的键（如 ↵ 执行、gq），其余按键交给 vim 引擎 |
| `normal` | 所有 NORMAL 上下文共用：pane 焦点、leader、gt/gT、`:` |
| `global` | 始终生效，只有少数 Ctrl 组合：C-p、C-s、C-c |

解析顺序：最上层浮层 → 用户映射（仅在 NORMAL 上下文，§6.6）→ 焦点控件 → `normal` → `global`，先匹配到的生效。

- 实现上，把当前上下文涉及的各作用域 trie 按上述优先级合并，合并结果缓存起来复用。
- INSERT 下没有匹配到的可打印字符交给输入控件。
- console 中没有匹配到的按键序列（包括已经缓冲的前缀，如 `g` 之后的 `g`）整体交给 vim 引擎。

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

which-key 浮层只是把待输入序列画出来，本身不是作用域：浮层打开时，按键仍按原来的上下文解析，效果与没有浮层时相同；esc 清空待输入序列，浮层随之关闭。所以配置里没有 `[keys.whichkey]` 这张表。

**序列规则**：

- 只是前缀的节点（如 `SPC`、`g`）一直等待，直到按下一个键或 Esc。
- 既是完整绑定、又是更长序列前缀的节点，等待 `timeoutlen`（默认 1000ms）后执行已有的绑定。默认键位不会产生这种歧义节点。
- 状态栏显示正在等待的序列（B-03），例如 `SPC`、`g`、`5`。

### 6.6 次数前缀与用户映射

**次数前缀**：NORMAL 下按数字即输入次数。只有在尚未输入次数时，单独按 `0` 才是「移到行首 / 首列」。次数通过 `Args.Count` 传给 Action，表格、树、编辑器的移动都支持，比如 `5j`。

**用户映射**：定义「键 → 键序列」，语义同 vim 的 `noremap`，右侧不会再次展开。映射可以对所有 pane 通用，也可以按 pane 类型单独配置：

- `[map.<模式>]`：所有上下文通用，模式为 `normal` 或 `visual`。
- `[map.<上下文>.<模式>]`：只对某类 pane 生效。上下文有三种：`console`、`grid`（data pane 与 result pane 的表格共用）、`tree`。

```toml
[map.normal]              # 所有 pane 通用
J = "5j"
K = "5k"

[map.console.normal]      # 只在 console：左右各移动 5 个字符
L = "5l"
H = "5h"

[map.grid.normal]         # 只在表格：L 跳到右侧第 5 列，H 回到首列
L = "5l"
H = "0"
```

- 优先级：pane 类型的映射 > 通用映射 > 默认键位，以用户意图为准。
- 右侧按所在 pane 的含义执行：`l` 在 console 里是移动一个字符，在表格里是移动一列。
- 表格和树的默认键位没有占用 H、J、K、L。console 里这几个键本来有 vim 的含义（如 `J` 合并行），用户映射会覆盖它们，与 nvim 的 `nnoremap` 一致。
- 如果要把键绑定到某个操作，而不是一串按键，用 `[keys.<作用域>]`（§6.7）。

### 6.7 配置、冲突、提示、导出（PRD 6.3）

- **加载**：先加载内置的 `default.toml`，再用用户配置覆盖。值写成空字符串表示解绑。
- **冲突**：用户重新绑定默认键属于正常改键，不报冲突。只有以下两种情况会报：
  - 同一作用域内，两个键规范化后相同（如 `<C-p>` 和 `<c-p>`），或者同一个键绑定了两个 Action。
  - 单键和以它开头的序列同时存在（如 `g` 和 `gt`），会产生超时歧义，给出警告。

  启动时用浮层列出冲突；`sqlmux keys --check` 发现冲突时以非零状态码退出。
- **提示**：`keymap.Hint(actionID, scope)` 返回当前生效的第一个键位的显示形式。界面上所有的键位文字都通过它读取，不写死；未绑定的 Action 不显示提示。
- **导出**：`sqlmux keys` 输出当前生效的键位表（markdown），加 `--format toml` 输出可以分享的配置片段。

### 6.8 默认键位

| 范围 | 默认键 | 操作 | PRD 原键 |
|---|---|---|---|
| 全局 | `C-p` | 命令面板 | C-k / C-p |
| 全局 | `C-s` | 保存：表格中提交修改，console 中写入文件 | C-s |
| 全局 | `C-c` | 有查询在执行时，取消查询。空闲时连按两次退出：第一次弹出 toast「再按一次 C-c 退出」，显示 2 秒；toast 还在时再按一次即退出，消失后再按，重新算第一次（与 Claude Code、node REPL 的习惯一致）。`:qa` 也可以退出。处在命令行、输入框、浮层、单元格编辑或编辑器 INSERT 这类输入状态时，`C-c` 的作用等同 esc（与 vim 一致），不算作连按两次退出中的一次 | 新增 |
| NORMAL | `C-h` `C-j` `C-k` `C-l` | 切换 pane 焦点 | C-a hjkl |
| NORMAL | `SPC s` | session 列表 | C-a s |
| NORMAL | `SPC c` · `SPC n` · `SPC p` · `SPC l` | 新建 · 下一个 · 上一个 · 上次用的 window | C-a c / n / p / l |
| NORMAL | `SPC %` · `SPC "` · `SPC z` · `SPC x` | 左右分割 · 上下分割 · 缩放 · 关闭 pane | C-a % / " / z / x |
| NORMAL | `SPC q` | 按编号跳转 pane | C-a q |
| NORMAL | `SPC b` | 折叠 schema 树 | C-a b |
| NORMAL | `gt` · `gT` | 下一个 · 上一个 tab | 同 |
| NORMAL | `:` | 打开命令面板并直接进入命令范围；`:q`、`:qa`、`:w` 照常可用（§12） | 同 |
| 表格 | `hjkl` `gg` `G` `0` `$` | 移动，支持次数前缀 | hjkl |
| 表格 | `↵` · `i` | 编辑单元格 | 同 |
| 表格 | `R` · `T` · `x` | 刷新 · 转置 · 关闭 tab（有未保存修改时需确认） | 同 |
| 表格 | `/` | 聚焦 WHERE 输入框 | 未定义 |
| 表格 | `go` `gl` `gp` `gc` | 打开 ORDER / LIMIT / PAGE / COLS 下拉 | 未定义（Q-03） |
| 表格 | `]` · `[` | 下一页 · 上一页 | 未定义 |
| 表格 | `yy` · `yi` | 复制单元格 · 把行复制为 INSERT 语句 | y / yi |
| result | `P` · `q` | 固定当前结果 tab · 关闭当前结果 tab | P / q（原为固定 / 关闭 pane） |
| schema 树 | `j` `k` `gg` `G` | 移动，支持次数前缀，与表格一致 | j/k |
| schema 树 | `↵` · `t` · `/` · `gs` | 在当前 tab 打开 · 在新 tab 打开 · 过滤 · 切换 schema | ↵ / C-↵ / / ；gs 为新增 |
| console | `↵`（NORMAL / VISUAL） | 执行光标所在语句 · 执行选区 | ⌥↵ |
| console | `gq` | 格式化当前语句或选区 | 同 |
| console | `gs` | 打开 schema 下拉框（仅 PG，§8.6） | 新增 |
| 命令面板 | `C-t` | 表在新 tab 打开；快速 SQL 的结果送到 result pane | C-↵ |
| 快速 SQL | `C-y` · `C-e` | 复制为 CSV · 在 console 中打开（写语句也走这条路） | C-y / C-e；取消 C-S-↵ |
| 单元格编辑 | 见 §10.2 | 时间分段调整、选项选择 | 新增 |
| 各浮层 | 见下表 | 各浮层内的移动与选择 | 同 PRD |

**默认键位只放常用的**（M0 用户反馈）。其余操作都能在命令面板里搜到并执行；想要快捷键的，在 `config.toml` 里自己绑定，例如：

```toml
[keys.normal]
"<Leader>h" = "pane.focus.left"   # 用 SPC h 切焦点
"<Leader>L" = "pane.resize.right"  # 用 SPC L 调整大小
"<Leader>," = "window.rename"
```

不提供「切换到第 n 个 window」这类按编号的命令：window 用 `SPC n` / `SPC p` / `SPC l` 切换，或者在命令面板的 window 范围里搜，也可以点击状态栏上的 window 名。

console 里的 `x` 是 vim 的删除字符，所以 console 的 tab 用 `:q` 关闭。

各浮层内的键位如下，全部取自 PRD。等到相应的浮层实现时，再加进 `default.toml`。

| 浮层 | 键 | 操作 | PRD |
|---|---|---|---|
| WHERE 历史 / 收藏下拉（`where`） | `C-r` · `C-n` / `C-p` · `↵` · `C-f` · `esc` | 打开 · 上下选择 · 应用 · 收藏或取消收藏 · 关闭 | Q-02 |
| COLS 下拉（`cols`） | `/` · `j` / `k` · `space` · `a` · `A` · `esc` | 聚焦过滤框 · 移动 · 勾选 · 全选 · 全不选 · 先清空过滤，再按一次关闭 | Q-04 |
| session 列表（`sessions`） | `j` / `k` · `l` / `h` · `↵` · `n` · `x` · `$` · `esc` | 移动 · 展开 / 收起 · attach · 新建连接 · 关闭 · 重命名 · 关闭列表 | S-03 |
| 命令面板（`palette`） | `↑` / `↓` 或 `C-n` / `C-p` · `Tab` / `S-Tab` · `↵` · `C-t` · `C-y` · `C-e` · `esc` | 移动 · 切换范围 · 执行 · 在新 tab 打开 / 把结果送到结果区 · 复制 CSV · 在 console 中打开 · 关闭 | K-01~K-04、F-04 |
| schema 下拉（`schema`） | `C-n` / `C-p` 或 `j` / `k` · `↵` · `esc` | 移动 · 选中 · 关闭 | §8.6 |
| 补全列表（`complete`） | `C-n` / `C-p` 或 `↑` / `↓` · `Tab` · `↵` · `esc` | 移动 · 接受 · 只有明确选中过才接受 · 第一次按关闭列表 | §9.7 |

## 7. 渲染与鼠标

### 7.1 Frame

```go
type Frame struct {
    Buf    *uv.ScreenBuffer      // Ultraviolet 的 cell 缓冲区（lipgloss.Canvas 只是在它外面包了一层，还不暴露 FillArea，所以直接用它）
    Hits   []Hit                 // 按绘制顺序追加；查找时倒序，后画的在上层
    Mouse  uv.Position           // 当前指针位置，用于悬停样式
    Theme  *Theme
    Keys   *keymap.Map           // 查询键位提示
}
```

- **组件接口**：每个组件实现 `Draw(f *Frame, area uv.Rectangle)`。区域由 `ultraviolet/layout` 切分，文字用 `uv.NewStyledString(s).Draw(buf, rect)` 画进去。
- **View()**：依次画 pane、状态栏、浮层，把 `Hits` 存进 model 的缓存指针，然后返回：

  ```go
  tea.View{Content: buf.Render(), AltScreen: true, MouseMode: tea.MouseModeAllMotion, ...}
  ```
- **宽度**：Frame 和 Bubble Tea 的渲染器必须用同一种宽度算法。两者不一致时，渲染器会按自己的算法重排这一行，行尾的边框会被挤掉。统一按字素簇计算（`ansi.GraphemeWidth`）：
  - 实测 tmux 3.7b 按字素簇显示：👍🏽、❤️、👨‍👩‍👧、1️⃣、☺️、⚠️ 都占 2 列；按 wcwidth 算，分别是 4、1、6、1、1、1 列。
  - Bubble Tea v2 默认用 wcwidth，只有终端回报支持 mode 2027 时才改用字素簇。tmux 不回报，Terminal.app 连查询都不发，所以要在启动时主动切换。
  - Bubble Tea v2.0.9 没有公开的设置项。做法是在 `Init` 里返回一个 `tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet}`，它走的是终端回报 2027 时的同一条路径（`tea.go` 的 eventLoop）。这借用的是未公开的行为，所以配一个单测：终端不回报时，启动后渲染器已经切换（它会输出 `ansi.SetModeUnicodeCore`）。升级 Bubble Tea 后这条路径变了，测试就会失败；升级时也顺便看看是否有了正式的设置项。
  - 代价：只按 wcwidth 显示的老终端，遇到这类字符会错位。有人遇到时再加配置项。
  - 已知上限：East Asian Width 为 Ambiguous 的字符一律按 1 列计算，与 tmux 默认一致。这类字符包括 Nerd Font 图标所在的私用区、`·`、截断用的 `…`、带圈数字 ①–⑳。终端把 Ambiguous 字符设成双宽时（iTerm2 等有这个选项，一些 CJK 用户会打开），它们会占 2 列，边框随之错位；⓪ 属于 Neutral，不受影响，所以还会和 ①–⑳ 表现不一致。有人遇到时，再加 `ambiguous_width = 2` 之类的配置项。

### 7.2 Block

`ui.Block` 画单线边框：

- 上边框左侧嵌入 `⟨n⟩ 图标 对象名`（ascii 图标下为 `⟨n⟩ 图标 类型 · 对象名`，§7.7），右侧嵌入操作提示（如 `SPC b`、`▶ run ↵`）。
- 获得焦点时，边框和标题用 `focus` 色；未聚焦时，边框用 `border` 色，标题用 `dim` 色。
- tab 栏是内容区的最后一行，不画在边框上，与设计稿一致。

### 7.3 主题

颜色从设计稿提取，定义为语义 token。内置主题 `tokyonight-storm`，也可以自己写主题文件（PRD 第 8 章）：

- `config.toml` 里写 `theme = "<名字>"` 选择主题：先找 `~/.config/sqlmux/themes/<名字>.toml`，没有再找内置主题。
- 主题文件只需写要改的 token，没写的沿用 `tokyonight-storm`。颜色写成 `#rrggbb`。token 名或颜色写错、或者找不到这个主题时，启动报错并指出是哪一项。
- 主题文件里还可以用 `[icon]` 表自定义图标（§7.7）。

```toml
# ~/.config/sqlmux/themes/ristretto.toml
pane_bg = "#393333"
row     = "#6c6a6d"   # 当前行
cursor  = "#81817e"   # 当前单元格
number  = "#ab9df2"
string  = "#ffd866"
time    = "#fc9867"
```

终端不支持真彩时，lipgloss 的 colorprofile 会自动降级。

| token | 值 | 用途 |
|---|---|---|
| `bg` / `pane_bg` | #1f2335 / #24283b | 背景 |
| `fg` / `fg_muted` / `dim` | #c0caf5 / #a9b1d6 / #565f89 | 文字 |
| `border` | #3b4261 | 未聚焦的边框 |
| `focus` | #9ece6a | 焦点、NORMAL、执行 |
| `warn` | #e0af68 | 待提交修改、匹配高亮、键位、INSERT |
| `cursor` / `cursor_blur` | #3d59a1 / #2f3549 | 单元格光标（pane 聚焦 / 失焦） |
| `select` / `row` | #364a82 / #292e42 | 选中项 / 当前行 |
| `row_alt` | #1f2335 | 表格斑马纹的偶数行（比 pane 底色深） |
| `edited_bg` | #2d2a24 | 已修改单元格的底色 |
| `keyword` | #bb9af7 | SQL 关键字、VISUAL |
| `info` | #7dcfff | 连接信息、COMMAND |
| `error` | #f7768e | 错误、Redis 标识 |
| `number` / `pk` / `func` | #ff9e64 / #73daca / #7aa2f7 | 数值 / 主键与 schema 值 / 函数名 |
| `string` / `time` / `bool` / `json` | 同 `fg` | 表格里对应类型的值（§7.6）。默认不着色，主题可以改 |
| `bar` | #292e42 | 状态栏和 toast 的底色。原来与 `row` 共用一个值，拆开后主题改当前行的颜色不会连带改状态栏 |
| `sep` | #2f3549 | 分隔线：侧栏内的分隔线、tab 之间的 `│`、连接地址块的底色 |

### 7.4 命中表与鼠标（PRD 第 7 章）

```go
type Hit struct {
    Rect   uv.Rectangle
    Target Target   // {Kind: cell|rowno|tab|chip|pane|title|border|item|hint|segment|backdrop, Pane, I, J, Action}
}
```

**键位提示即按钮**：界面上画出的每一条键位提示都登记为命中区，点击就执行对应的 Action。覆盖的位置包括 pane 标题右侧、tab 栏右侧、浮层底栏、which-key 浮层、命令面板右列、状态栏。因此键盘不方便的操作，总能在界面上找到可以点的地方。

| 鼠标操作 | 实现 |
|---|---|
| 单击 | 查到目标后转成 Action。例如点击单元格，等于依次执行 `pane.focus` 和 `grid.goto r c`，与键盘走同一条路径 |
| 双击 | 400ms 内第二次点中同一目标，触发它的双击操作：单元格进入编辑，pane 标题切换缩放 |
| 悬停 | 绘制时用 `f.Region(rect, target)` 判断是否悬停并据此选择样式。命令面板里悬停即触发 `palette.select i`（K-03） |
| 滚轮 | 作用于指针所在的 pane，而不是焦点 pane。纵向每格滚动 3 行，与 nvim 的默认值（`mousescroll=ver:3`）一致，表格、树、console 都一样。表格和树最多滚到最后一行贴着底边，内容一屏放得下时滚轮不起作用；console 的滚动范围与 nvim 一致（M3）。横向滚动支持 Shift+滚轮，也支持触控板的横向滑动（终端上报为滚轮按钮 6/7） |
| 拖动 | 在 pane 边界上按下即开始拖动，实时修改对应节点的 `Ratio`。拖动柄：左右相邻的 pane 之间是那 1 列间隔；上下相邻的 pane 之间没有间隔，用上面那个 pane 的下边框这一行，因为下面那个 pane 的上边框放着标题和提示，点击、双击另有用途。在 console 里拖选文本会进入 VISUAL |
| 点击浮层外部 | 浮层先登记一个全屏的 `backdrop` 命中区，点中它就关闭浮层。正在编辑的单元格失焦时，保留修改 |

终端兼容：

- 悬停依赖终端支持全量移动上报（1003 模式）。点击、滚轮、拖动在主流终端上都能用。
- 在 tmux 里使用鼠标，需要 tmux 开启 `mouse` 选项。这是 tmux 自身的行为，只写进 README。

### 7.5 半透明遮罩

终端没有 alpha 通道。遮罩的做法是：画命令面板之前，遍历缓冲区里已经画好的所有 cell，把前景色和背景色都向 `bg` 混合 60%，然后再画面板。

### 7.6 表格

- **虚拟滚动**：只渲染视口内的行；横向按列偏移滚动。
- **列宽**：
  1. 期望宽度取表头宽度与当前页数据宽度 p90 中的较大值，上限 40。
  2. 总宽度超出时按比例收缩，但不小于表头宽度（G-06）。
  3. 还放不下就横向滚动。
- **宽度计算**：按字素簇计算（§7.1「宽度」），CJK 字符和 emoji 都能正确处理。
- **单元格显示**：
  - 按列的类型分类着色（§7.3）：数值（int、numeric、float 等）用 `number` 色，并且右对齐；字符串用 `string`；时间（date、time、timestamp、timestamptz、interval）用 `time`；布尔用 `bool`；json / jsonb 用 `json`；其余类型用 `fg`。
  - NULL 显示为 `dim` 色的 `<null>`；超长内容用 `…` 截断；主键列的表头带钥匙图标。
  - 显示前清理控制字符：换行显示为 `dim` 色的 `↵`，Tab 显示为一个空格，其余控制字符（包括 ESC）直接去掉，避免把终端控制序列画到屏幕上。截断按字素簇进行。完整的值在单元格编辑（M2）里看。
  - 已修改的单元格用 `warn` 色文字、`edited_bg` 底色、点状下划线（SGR 4:4）。终端不支持点状下划线时，退化为普通下划线。
- **转置（G-05）只影响渲染**：`GridState` 始终保存数据坐标，按键时把屏幕方向换算成数据方向，所以光标位置和修改标记在两种视图之间自然保持。
- **网格样式**：按用户要求，行和列都要有清晰的分隔（参考 DataGrip 的表格）。
  - **列**：
    - 相邻两列之间用 `│`（`sep` 色）分隔；
    - 行号列和第一列数据之间也用 `│` 分隔；
    - 每个单元格左右各留 1 列空白。
  - **表头**：
    - 表头下面画一条横线，与竖线交叉的位置用 `┼`；
    - 列名用 `func` 色，主键列带钥匙图标；
    - 横向滚动时，表头始终固定在最上方。
  - **行**：
    - 用斑马纹区分：偶数行使用 `row_alt` 底色，比 pane 底色更深一些；
    - 当前行使用 `row` 底色（比 pane 底色更亮），当前单元格使用 `cursor` 底色；
    - 行与行之间不画横线，因为终端里每条横线都要占掉一整行，一屏能显示的行数会减半。
- 转置视图用同样的样式：竖线分隔的是各条记录，横线画在第一行（行号行）的下方。

### 7.7 图标

默认使用 Nerd Font 字形。设置 `icons = "ascii"` 后改用 ASCII 替代字符，用于没有安装 Nerd Font 的环境。console 用带方框的终端图标 `nf-oct-terminal`（U+F489）。

**有图标就不再配文字**（M0 用户反馈）：Nerd Font 图标本身就能说明含义，所以图标旁边不再重复写说明文字，包括 pane 标题里的类型名（`data`、`console`）、状态栏里命令面板入口的 `C-p`、侧栏过滤行的 `/`。ASCII 字符表达不了含义，所以 `icons = "ascii"` 时这些文字照常显示。命令面板里 pane 那一行的名称仍然带类型名，因为那是用来搜索的文字。

主题文件里可以逐个覆盖图标（M0 用户反馈，写法参考 yazi 的 `theme.toml`）：

```toml
[icon]
console = { text = "\uf489", fg = "#78dce8" }   # 换字形，也换颜色
table   = { fg = "#a9dc76" }                     # 只换颜色
```

- `text` 换字形，`fg` 换颜色，两者都可以只写一个。没写的沿用 `icons` 选的那一套（nerd / ascii），`icons = "ascii"` 时覆盖照样生效。
- 写了 `fg` 的图标在任何位置都用这个颜色；没写时跟随所在位置的颜色，比如标题聚焦时是 `focus` 色。
- 可以覆盖的图标：`schema`、`table`、`data`、`console`、`filter`、`search`、`keys`、`conn`、`key`、`postgres`，以及命令面板用的 `command`（nf-fa-bolt，U+F0E7，ascii 为 `:`）和 `window`（nf-fa-window_restore，U+F2D2，ascii 为 `[]`）。以后新增的图标（如 `mysql`、视图）也按名字加入。名字写错时启动报错。

### 7.8 默认尺寸与样式（取自设计稿）

设计稿按 13px 等宽字体绘制，一列约 7.8px。下面的数值由此折算成终端的行和列；设计稿里没有标注的，由实现自行取整。

- **整体布局**：
  - 最外层不留边距。
  - 横向相邻的 pane 之间留 1 列空白，纵向相邻的不留。
  - data 与 console 的宽度比为 5 : 4（设计稿中分别是 flex 5 和 flex 4）。
- **schema 侧栏**：
  - 宽度默认 32 列（含边框；设计稿为 250px）。窗口宽度小于 100 列时，缩到 24 列。
  - 可以用鼠标拖动侧栏和右边 pane 之间的那 1 列间隔来调宽度，最窄 16 列，最宽为窗口宽度的一半；拖过的宽度记在 window 上。折叠时不能拖。
  - 标题为 `⟨0⟩ <schema 图标> <当前 schema> ▾`，例如 `⟨0⟩ public ▾`。点击它，或者在树里按 `gs`，打开 schema 下拉框（与 §8.6 是同一个组件）。右侧提示仍是 `SPC b`。
    - 空间分配与 pane 标题不同：schema 名本身就是切换 schema 的按钮，又是树里唯一能看出当前 schema 的地方，所以优先级高于右侧的 `SPC b`（折叠也可以用键盘、命令面板完成）。依次为：
      1. 先留 `⟨0⟩ <图标>`；
      2. 再放 schema 名和 `▾`，放不下时截短，例如 `pub… ▾`，`▾` 始终跟在名字后面，一个字都放不下时连 `▾` 一起去掉；
      3. 剩下的空间放得下 `SPC b` 才显示。
      只有连 `⟨0⟩ <图标>` 都放不下时，才只剩 `⟨0⟩`。
  - 折叠后是 3 列宽的细栏（设计稿为 24px），左右各 1 列边框、中间 1 列内容：顶部显示 `»`，下面竖排 `schema · SPC b`，每行一个字符。
  - 侧栏内部从上到下依次是：
    - 第一行：过滤图标（`info` 色）、`160 tables`（`dim` 色）；ascii 图标下，图标后面还有 `/`（`fg` 色）（§7.7）；
    - 一条分隔线（`sep` 色）；
    - 表列表：每项是「图标（`func` 色，当前表用 `focus` 色）+ 表名 + 右对齐的行数量级（`border` 色）」，当前表整行用 `select` 底色；
    - 一条分隔线；
    - 提示行，如 `j/k move  ↵ open tab`，键名用 `focus` 色粗体，说明文字用 `dim` 色。
- **pane 标题**：
  - 格式为 `⟨n⟩ <图标> 对象名`，ascii 图标下为 `⟨n⟩ <图标> 类型 · 对象名`（§7.7）。从左上角往右 1 列开始，两侧各留 1 个空格。
  - 编号 `⟨n⟩` 在 Nerd Font 图标下显示为带圈数字 `⓪ ① ② … ⑳`，只占 1 列（M0 用户反馈）；超过 20 时仍写成 `⟨21⟩`。ascii 图标下照旧是 `⟨n⟩`。标题、侧栏标题、命令面板里 pane 那一行都照此显示。`SPC q` 在 pane 中央显示的编号仍是普通数字，因为那是要按的键。
  - 带圈数字按 1 列计算，已知上限见 §7.1「宽度」。
  - 右侧提示离右上角 1 列。
  - console 标题的右侧是 `▶ run`（`focus` 底、`bg` 色字、粗体）加上 `↵`（`dim` 色）；schema 下拉框 `doraemon.public ▾` 放在 run 的左边。
  - **空间不够时，提示优先于标题的对象名**，因为提示都是可点击的按钮（§7.4）。按下面的顺序分配空间：
    1. 先给最短的标题 `⟨n⟩ <图标>`（ascii 下是 `⟨n⟩ <图标> 类型`）留出位置。
    2. 按优先级从高到低依次摆放右侧的提示：放得下就保留；放不下就跳过这一个，继续试下一个。每个 pane 的提示按重要性排序，console 的顺序是 `▶ run` > schema 下拉框 > 键位文字 `↵`。所以宽度较窄时，可能显示 `▶ run ↵`，但没有下拉框。
       键位文字依附于它所属的按钮：按钮没显示，它的键位文字也不显示。例如 `↵` 只能跟着 `▶ run` 一起出现，不会单独出现。
    3. 剩下的空间再给标题里的对象名；放不下的部分截掉，末尾加 `…`，如 `⟨2⟩ console · cons…`。
    4. 连最短的标题都放不下时，只保留 `⟨n⟩`。
  - **空 pane**（没有 tab）：标题栏只显示 `⟨n⟩ <图标>`（ascii 下是 `⟨n⟩ <图标> 类型`），右侧不显示任何提示。这些提示都作用于当前 tab：`▶ run` 执行 tab 里的内容，schema 下拉框是 tab 的属性（§8.6）；没有 tab，它们就没有作用对象。新建 tab 用 tab 栏里的 `+`。
- **tab 栏**：
  - 位于 pane 内容区的最后一行，底色为 `bg`（比 pane 底色深），不画分隔线。
  - 每个 tab 显示为 ` 序号:名称标记 `，tab 之间用 `│`（`sep` 色）分隔。
  - 当前 tab 用 `pane_bg` 底色、`focus` 色字；其他 tab 用 `dim` 色字。
  - tab 之后是可点击的 `+`；最右端是 `dim` 色的键位提示，如 `hjkl · ↵ edit · T 转置 · gt/gT`，文字从 keymap 读取。
- **状态栏**：
  - 底色为 `bar`（#292e42）。各段是扁平色块，不用 powerline 箭头，每段左右各留 1 列内边距。
  - 左侧依次为：
    - ` <引擎图标> doraemon ▾ `：`focus` 底、`bg` 色字、粗体；
    - window 列表，如 ` 0: data* `：当前 window 用 #3b4261 底、`fg` 色字，其余 window 用 `dim` 色字、无底色。
  - 右侧右对齐，依次为：
    - 模式附加信息：`dim` 色，过长时用省略号。内容取自设计稿，随当前模式变化：
      - NORMAL：不显示；
      - INSERT：正在编辑的对象，如 `-- editing WHERE --`；
      - VISUAL：选区大小和执行键，如 `4 lines · ↵ run`，键位文字从 keymap 读取；
      - COMMAND：不显示，匹配的命令在命令面板里。
    - ` <搜索图标> `：`info` 色，点击打开命令面板；ascii 图标下显示为 ` ~ C-p `（§7.7）；
    - ` <键盘图标> 待输入序列 `：序列用 `warn` 色粗体。没有待输入的键时，显示 `dim` 色的 `·`；这一块始终占着位置，序列部分至少 3 列宽（放得下 `SPC`），内容靠左，这样按键时右侧各块不会左右跳动，序列超过 3 列时才变宽；
    - ` 行,列 `：`fg_muted` 色；
    - ` <图标> pg@localhost:5432 `：`info` 色，`sep` 底；
    - ` NORMAL `：模式色底、`bg` 色字、粗体。
  - 窗口太窄、放不下时，按下面的顺序依次省略：
    1. 模式附加信息；
    2. 连接地址；
    3. 不是当前的 window，从右往左去掉；
    4. 光标位置；
    5. 截短 session 名。

    始终保留的是：session 块（名称可以被截短）、当前 window、`C-p` 入口、待输入序列、模式块。
- **COMMAND 模式**：命令面板打开时，模式块显示 COMMAND。状态栏里不再有命令行：M0 用户体验后改由命令面板取代（§12），`:` 打开面板的命令范围。
- **toast**：显示在状态栏上方一行的右侧，默认 3 秒后消失；「再按一次 C-c 退出」这一条显示 2 秒，正好是连按的窗口（§6.8）。样式为 `warn` 色字、#292e42 底、左右各留 1 列。设计稿里没有 toast，这个样式是后定的。加底色是因为那一行正好是 pane 的下边框，不加底色，文字会和边框混在一起。data pane 中保存 / 刷新的结果按 Q-06 的要求显示在查询条的右侧，不通过 toast 显示。

## 8. 数据访问

### 8.1 接口

```go
type Conn interface {
    Exec(ctx context.Context, sql string, maxRows int) ([]Result, error)  // 多语句，走简单协议
    Query(ctx context.Context, sql string, args ...Val) (Result, error)   // 单语句，参数按文本或 NULL 传
    Cancel(ctx context.Context) error
    Close() error
}

type Val struct{ S string; Null bool }

type Result struct {
    Cols      []Col             // Name, Type（数据库类型名）, Numeric
    Rows      [][]Val
    Tag       string            // 例如 "UPDATE 3"
    Truncated bool
    Took      time.Duration
}
```

- `postgres`、`mysql` 各实现一份 `Conn`，外加各自的 catalog 查询和方言函数（`QuoteIdent`、`Placeholder(n)`）。
- `Session` 只依赖这些接口。以后接入非 SQL 引擎（如 Redis）时，由引擎决定 window 里能创建哪些 pane。

**值一律按文本处理**：客户端只负责显示和回写，按文本处理可以绕开各类类型的解码问题（枚举、数组、range、geometry 等）。

- **PG**：
  - `pgconn.Exec`（简单协议）本身就返回文本；`ExecParams` 把结果格式指定为文本，参数 OID 传 0，由服务端推断类型。
  - 列类型取自 FieldDescription 里的 OID。
  - 建连时通过 RuntimeParams（startup 消息）设置 `DateStyle = ISO, YMD`，保证时间文本的格式可以解析（§10.2）。每条连接都会带上，不会漏设，也不需要额外的往返。
  - 连接参数交给 `pgconn.ParseConfig(dsn)` 解析，环境变量（`PGHOST` 等）和 `~/.pgpass` 都由它处理，不自己实现。没写 `application_name` 时补成 `sqlmux`，没写连接超时时补成 10s。
- **MySQL**：DSN 加上 `interpolateParams=true`，全程走文本协议；列类型从 `ColumnTypes()` 获取。
- **已知上限**：MySQL 中非 UTF-8 的 blob 显示为 `<binary n bytes>`；PG 的 bytea 按服务端返回的 `\x…` 文本显示。

### 8.2 连接模型

每个 session 两条连接。每条连接由一个 `db.Worker` goroutine 独占，请求通过 channel 串行执行，因为连接对象不能并发使用。

| 连接 | 用途 |
|---|---|
| `Main` | console 执行、保存修改、manual 事务模式下的表格读取；持有事务状态 |
| `Meta` | schema 树、列属性、DDL 预览、计数、快速 SQL；auto 事务模式下的表格读取 |

- 拆成两条的原因：console 里跑长查询时，树、命令面板和表格浏览不会跟着卡住。
- manual 事务模式下表格读取改走 `Main`，这样能看到自己还没提交的修改。
- `Meta` 建连后设为只读：PG 设 `default_transaction_read_only = on`（和 DateStyle 一样放在 RuntimeParams 里，效果等同 `SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY`），MySQL 执行 `SET SESSION TRANSACTION READ ONLY`。`Meta` 上只有读操作，写入只走 `Main`，所以 WHERE 里就算调用了会写数据的函数，也改不了数据。和 §13 一样，这是为了防误操作，不是权限边界。

### 8.3 取消、超时、过期响应

- **取消**：`Main` 忙碌时，状态栏显示 `busy · C-c 取消`，这段文字可以点击。
  - PG 调用 `PgConn.CancelRequest`。pgconn 默认的 `DeadlineContextWatcherHandler` 会在 context 取消时给连接设 deadline，连接随之断开。每个 session 只有两条长连接，所以改用 `CancelRequestContextWatcherHandler`（设 `DeadlineDelay`），取消之后连接还能继续用。
  - MySQL 另开一条临时连接执行 `KILL QUERY <connection_id>`，id 在建连时记录。只取消 context 的话，驱动会关掉连接，但服务端上的查询会继续跑。
- **超时**：建连 10s。计数查询 3s：PG 用 `SET LOCAL statement_timeout`，MySQL 用 `MAX_EXECUTION_TIME` hint。
- **过期响应**：每个 tab 维护一个递增的 `seq`，请求时带上。结果回来时 `seq` 已经不是最新的就丢弃，避免快速翻页时旧结果覆盖新结果。

### 8.4 元数据

| 内容 | PostgreSQL | MySQL |
|---|---|---|
| 表列表、行数量级（D-01） | `pg_class.reltuples` + `pg_namespace` | `information_schema.TABLES.TABLE_ROWS` |
| 列属性：类型、可空、默认值、主键 | `pg_attribute`（`format_type`、`attnotnull`、`pg_get_expr(adbin, adrelid)`）+ `pg_constraint` | `information_schema.COLUMNS` + `KEY_COLUMN_USAGE` |
| 所有列都不可空的唯一索引（用于定位行，见 §10.1） | `pg_index`，条件见表下 | `information_schema.STATISTICS` 中 `NON_UNIQUE = 0`，并要求每列的 `IS_NULLABLE = 'NO'` |
| 枚举值（§10.2） | `pg_type.typtype = 'e'` 时查 `pg_enum` | 解析 `COLUMN_TYPE`，如 `enum('a','b')` |
| DDL 预览（K-05） | 用 `format_type`、`pg_get_constraintdef`、`pg_get_indexdef` 拼出，不要求与 pg_dump 一致 | `SHOW CREATE TABLE` |

PG 的几处细节（M1 核对时对照 lazysql、pgtui 确认，两者各有问题）：

- **表列表**：`relkind IN ('r','p','v','m','f')` 且 `NOT relispartition`，分区的子表不列出，只列父表。系统 schema 用 `nspname !~ '^pg_'` 排除；`NOT LIKE 'pg_%'` 里的 `_` 是通配符，会误伤 `pgx_data` 这类 schema。
- **行数量级**：`reltuples < 0`（从没 analyze 过，或者是分区表的父表）时显示为 `?`。
- **列属性**：不用 `information_schema.columns`，它会漏掉没有权限的列，数组和枚举的类型也只显示成 `ARRAY`、`USER-DEFINED`。
- **主键**：列按 `unnest(conkey) WITH ORDINALITY` 的顺序排列。
- **可用于定位行的唯一索引**：`indisunique AND indisvalid AND indpred IS NULL AND 0 <> ALL(indkey)`；只看前 `indnkeyatts` 个键列（INCLUDE 列不算），并要求这些列都是 `attnotnull`。
- **枚举值**：`array_agg(enumlabel ORDER BY enumsortorder)`，按列的 `atttypid` 关联。

catalog 按 session 缓存。console 执行 DDL 后（由 §9.3 的判定得知）或用户执行刷新命令时，重新加载。

### 8.5 分页与计数

**取数**：`SELECT * FROM t WHERE … ORDER BY … LIMIT n OFFSET m`。

- 每页默认 100 行，LIMIT chip 可选 100 / 500 / 1000。100 与 PRD 快速 SQL 的 limit 100 一致。
- 每次多取一行（`LIMIT n+1`），用来判断还有没有下一页。这样计数还没回来或者超时的时候，翻页也能正确停在最后一页。

- 默认按「行标识列」排序（§10.1：主键，没有主键时用所有列都不可空的唯一索引），保证分页结果稳定。
  - 两者都没有时，不加默认排序，用户可以通过 ORDER 指定排序列。
- 总是取全部列，列的显示与隐藏只在客户端控制，所以切换 COLS 不需要重新查询。已知上限：列很多或含大 JSON 字段的表会多传数据，以后可以改成只查可见列。

**计数**：在 `Meta` 上异步执行 `count(*)`。

- 超时则显示 `1/?`。
- 没有 WHERE 条件、且估计行数超过 100 万时，不做精确计数，用估计值显示 `~n`。

**已知上限**：OFFSET 翻到很深的页会变慢，以后可以改成 keyset 分页。

### 8.6 PG 的 schema 与 search_path

**背景**：

- PG 的一个数据库里可以有多个 schema，例如 `doraemon.public`、`doraemon.agentable`。
- SQL 里不带 schema 前缀的表名，会按连接的 `search_path` 设置依次查找，默认是 `"$user", public`。
- 所以，如果树里正在看的是 `agentable`，而 console 里写的是 `select * from tbl_xxx`，PG 默认只会在 `public` 里找这张表，结果报「表不存在」；更糟的情况是 `public` 里恰好有同名表，查到的就是另一张表。

**方案**：与 DataGrip 一致，每个 console 可以单独选择 schema。

- **入口**：console pane 标题的右侧显示 `doraemon.public ▾`，位于 `▶ run ↵` 旁边。
  - 点击它，或在 console 的 NORMAL 模式下按 `gs`，打开 schema 下拉框。
  - 下拉框列出当前库的 schema，系统 schema（`pg_catalog`、`information_schema` 等）不列出。顶部有过滤输入框，支持模糊匹配（§9.7）。
  - 下拉框里 `C-n` / `C-p` 或 `j` / `k` 移动，`↵` 选中，`esc` 关闭，也可以用鼠标点选。
  - 命令面板里也有对应的「Switch schema…」命令。
- **默认值**：新建的 console，默认使用 schema 树当前所在的 schema；之后两者互不影响。
- **执行方式**：同一个 session 的所有 console 共用 `Main` 连接，所以每次执行前比较一下：如果连接当前的 `search_path` 与这个 console 选择的 schema 不一致，先执行 `SET search_path TO <所选 schema>, <建连时的原始 search_path>`。
  - 原始路径在建连时用 `SHOW search_path` 读取，接在后面，这样装在 `public` 等 schema 里的扩展函数仍然能找到。
- **影响范围**：console 的补全以它自己选择的 schema 为准。表格查询始终带 schema 前缀，不受影响。

MySQL 的 schema 就是 database，按 PRD，切换 database 会新建 session，所以 MySQL 的 console 不显示这个下拉框。

## 9. SQL 处理（`internal/sqlkit`）

### 9.1 词法扫描器

扫描一遍即完成，不构建 AST，按方言处理：

- **PG**：`'…'`（`''` 转义）、`E'…'`、`$tag$…$tag$`、`"标识符"`、`--` 注释、可嵌套的 `/* */`。
- **MySQL**：`'…'` 和 `"…"`（反斜杠转义）、`` `标识符` ``、`#` 注释、`-- `（后面必须带空格）、不可嵌套的 `/* */`。
- **输出**：token 的类型（关键字、标识符、字符串、数字、运算符、标点、注释、空白）、字节偏移、行号。

风险控制：

- 只做词法分析，代码量小，已知的边界情况都用表驱动测试覆盖。
- 执行前，console 会高亮即将执行的语句范围（§9.2），用户能看到实际要跑的是哪一段。
- 读写判定只用于显示提示，真正的保护在数据库层（§13）。

### 9.2 分句、可执行标记与执行前高亮（C-01、C-02）

- **分句**：在括号深度为 0 的 `;` 处切分，得到每条语句的范围。
- **光标所在语句**：取范围包含光标的那条；光标停在两条语句之间的空行上时，取前一条。
- **显示**：
  - 每条语句的起始行，在 gutter 显示 ▶ 标记，可以点击执行（C-02）。
  - console 处于 NORMAL 时，光标所在语句的范围用 `row` 底色标出；处于 VISUAL 时显示选区。这就是按 ↵ 会执行的内容。

### 9.3 读写判定

1. 第一个有效关键字是 SELECT、SHOW、EXPLAIN、TABLE、VALUES、DESC 之一时，判为读。
2. 以 WITH 开头的，继续在顶层扫描：出现 INSERT、UPDATE、DELETE、MERGE 就判为写。
3. `EXPLAIN ANALYZE` 后面跟写语句时判为写，因为它会真正执行。
4. 其余一律判为写。

这个判定只用于提示（F-05 的黄色提示、只读 session 的拦截说明），保护靠数据库层（§13）。

### 9.4 自动 LIMIT（F-05）

- 对 SELECT、WITH…SELECT、TABLE、VALUES 这类读语句：顶层没有 LIMIT 或 FETCH 时，在末尾追加 ` LIMIT 101`。多取一行是为了判断是否还有更多，界面上显示 `100+`。
- 不用子查询包一层来加 LIMIT，因为 MySQL 可能丢掉派生表里的 ORDER BY。
- 其他语句由 `Exec` 的 `maxRows` 截断。

### 9.5 格式化（C-03 gq）

**做法**：用 goja（纯 Go 实现的 JS 引擎）在进程内运行 [sql-formatter](https://github.com/sql-formatter-org/sql-formatter) 的独立打包文件。

- sql-formatter 使用 MIT 许可，支持 postgresql、mysql 等方言，在 JS 生态中广泛使用。
- 打包文件 `sql-formatter.min.js` 固定版本，放在 `internal/sqlkit/` 下，用 `go:embed` 嵌入。

**可行性验证**（2026-09-23 spike，版本 sql-formatter 15.8.2，打包文件 312KB，darwin/arm64）：

| 项 | 结果 |
|---|---|
| 初始化（解析打包文件） | 113ms |
| 单次格式化 | 首次 55ms，之后 13–21ms |
| 内存 | 进程 RSS 峰值约 30MB |
| 二进制体积 | 增加约 8MB（spike 9.6MB，空程序 1.6MB） |
| 正确性 | PG 的 `$$…$$`、`E'…'`、`::` 转换、`filter (where …)`、`$1` 参数、注释，MySQL 的反引号、`#` 注释、`limit 5, 10` 均输出正确 |

**兼容处理**：goja 的正则引擎不认识长 Unicode 属性名。加载前把 `\p{Alphabetic}`、`\p{Mark}`、`\p{Decimal_Number}` 替换为 `\p{L}`、`\p{M}`、`\p{Nd}`，共三处；对 SQL 标识符而言，二者的差别可以忽略。

**运行方式**：

- 第一次按 `gq` 时才初始化 VM，之后一直复用。goja 不是线程安全的，所以用一把互斥锁保护。
- 语言按 session 的引擎选择；`keyword_case`、`tab_width` 原样传给 sql-formatter。
- 格式化失败时给出 toast 提示，缓冲区保持不变。
- 配置了 `formatprg`（例如 `"pg_format -"`）时，改为调用外部命令：从 stdin 输入，读取 stdout 输出，与 vim 的 formatprg 一致。

### 9.6 WHERE 条件（Q-01）

不做简写语法。输入框里直接写完整的 SQL 条件表达式，例如 `deleted_at is null and status <> 'cancelled'`。

- **拼接方式**：输入原样放进 `SELECT * FROM t WHERE (\n<输入>\n) ORDER BY … LIMIT … OFFSET …`；计数查询用同一个表达式。
  - 外面加一层括号，是为了防止表达式里的 `OR` 和后面拼上的子句发生优先级错乱。
  - 括号内侧各加一个换行，这样输入末尾的 `--` 注释不会把右括号和后面的 ORDER / LIMIT 一起注释掉（lazysql 有这个问题）。输入为空时不加 WHERE。
- **只允许一条语句**：
  - PG 的表格查询走扩展协议，协议本身就拒绝多条语句；MySQL 不开启 `multiStatements`。
  - 发送之前，扫描器如果在顶层发现 `;`，直接提示错误，不发送。这样可以防止类似 `1=1; drop table t` 的输入连带执行其他语句。
- **历史 / 收藏**：保存输入原文。
- **补全**：见 §9.7。

### 9.7 模糊匹配与补全（console、WHERE）

**匹配算法直接用 fzf 的**：`github.com/junegunn/fzf/src/algo`（MIT 许可），用的就是 fzf 的 `FuzzyMatchV2`，得分和高亮位置都与 fzf 一致。

- **统一使用**：项目里所有的模糊匹配（命令面板、COLS 过滤、树过滤、WHERE 历史、补全）都用它，不再引入 sahilm/fuzzy。
- **已验证**（spike）：
  - 单独引入这个包，只会带进 fzf 的 `util` 以及 uniseg、go-isatty、go-shellwords、x/sys，测试程序只有 1.7MB。
  - 输入 `tord` 时，`t_order` 排在第一位。
- **封装**：这个包并不是 fzf 承诺稳定的库 API。所以固定版本，并封装在 `internal/ui/match.go` 一个文件里，以后升级只需要改这一处。
- **大小写**：smartcase，与 fzf 相同。
- **扩展语法**：列表类的过滤支持 fzf 扩展语法的常用部分：空格分隔多个词（AND）、`'精确`、`^前缀`、`后缀$`、`!排除`。

**补全的上下文判断**：根据光标前的 token 决定给哪类候选，只做词法判断，不做语法分析（`sqlkit.CompletionContext`）。

| 光标所在位置 | 候选 |
|---|---|
| FROM / JOIN / UPDATE / INTO / TABLE 之后 | 表名（含视图）；输入 `schema.` 后，只列该 schema 下的表 |
| `别名.` 或 `表名.` 之后 | 这张表的列。别名从当前语句的 FROM / JOIN 子句中解析，例如 `from t_order o` 中 `o` 指 `t_order` |
| console 的其他位置 | 依次为：当前语句引用到的表的列、其余表名、SQL 关键字 |
| WHERE 输入框 | 当前表的列名（附带类型）和常用关键字（`and` `or` `is null` `in` `like` `between` 等）。在 `列 =`、`列 <>`、`列 in (` 之后，枚举列和布尔列给出可选值 |

- **排序**：先按上表中候选组的顺序，再按 fzf 得分。
- **数据来源**：候选全部来自 session 的 catalog 缓存，在本地匹配，不发请求。某张表的列第一次用到时，才从 `Meta` 连接获取。

**交互**（与 fzf 以及常见编辑器的补全一致）：

- 输入标识符字符时自动弹出候选列表，随输入实时重排，匹配到的字符用黄底高亮（与命令面板相同）。
- 每一项右侧标注类别：表、视图、列的类型、关键字。列还会标出所属的表。
- `C-n` / `C-p` 或 `↑` / `↓` 移动选择，`Tab` 接受。
- `↵` 只有在用上述按键明确选中过候选时才接受；否则照常执行原来的功能：console 里换行，WHERE 里执行查询。
- `esc` 第一次关闭候选列表，第二次才退出 INSERT，与 COLS 下拉「先清空，再关闭」的规则一致。
- 鼠标：悬停即移动选择，点击即接受。

**WHERE 的 `C-r` 历史 / 收藏下拉**：也按当前输入模糊过滤，体验与 shell 里 fzf 的 CTRL-R 相同。

## 10. 单元格编辑与提交（G-02、G-03）

### 10.1 进入与记录

- **行标识列**：保存修改时，需要用它唯一定位到要改的那一行。依次取：
  1. 主键；
  2. 没有主键时，取一个所有列都不可空的唯一索引，效果与主键相同；
  3. 两者都没有，这张表只读，在 toast 中说明原因。
- **进入编辑**：表有行标识列、且 session 不是只读时才能进入编辑，否则用 toast 说明原因。result pane 里的表格只读，因为无法可靠地确定来源表和主键。
- **粘贴**：在表格 NORMAL 下粘贴（bracketed paste），会以粘贴的内容开始编辑当前单元格。
- **修改的存储**：`Edits` 以（原行标识值, 列）为 key，修改后的值可以是文本、NULL 或 DEFAULT（§10.2）。
  - 翻页后修改仍然保留，保存按钮上显示所有修改的总数。
  - 改回原值时，这条修改被删除，标记自动消失。

### 10.2 按列属性提供选项

编辑时，如果该列有可用的选项，单元格下方会出现一个选项浮层。浮层不抢输入框的焦点：键盘照常输入文字，也可以直接用鼠标点选。

| 列属性（来自 catalog） | 选项 |
|---|---|
| 可空 | `∅ NULL` |
| 有默认值（`column_default` 不为空） | `DEFAULT`：写入时使用 SQL 关键字 `DEFAULT`，而不是字符串。做法参考 lazysql 的 `set_value_list` |
| 本格有待提交的修改 | `↺ 原值` |
| 布尔（PG boolean；MySQL tinyint(1)） | `true` / `false`（MySQL 写入 1 / 0） |
| 枚举（PG enum；MySQL ENUM） | 全部枚举值，随输入内容模糊过滤 |
| 时间（date / time / timestamp / timestamptz / datetime） | 分段调整，加上 `◷ 现在` |

时间列的浮层（对应 DataGrip 的日期选择器，改成适合终端的分段形式）：

```
 2026-09-20 02:49:23.305808+08 ▾          行内输入框（INSERT）
┌──────────────────────────────────┐
│  ▴      ▴     ▴     ▴    ▴    ▴  │
│ 2026 - [09] - 20    02 : 49 : 23 │      分段调整，当前段高亮
│  ▾      ▾     ▾     ▾    ▾    ▾  │
│ ◷ 现在    ∅ NULL    ↺ 原值        │      选项
└──────────────────────────────────┘
```

**键盘**（`cell` 作用域）：

- 直接输入：编辑文字。
- `Tab` / `S-Tab`：切换当前段。
- `↑` / `↓`：当前段加一 / 减一。
- `C-n` / `C-p`：在选项之间移动，与其他浮层一致。布尔和枚举列的 `↑` / `↓` 也用来移动选项。
- `↵`：有选中的选项时应用该选项，否则提交文字。
- `esc`：提交并退出编辑，保留修改（G-02）。

**鼠标**：

- 点击 ▴ / ▾，或在某一段上滚动滚轮：该段加减。
- 点击某一段：选中该段。
- 点击选项：立即应用。
- 点击浮层外部：提交并退出编辑。
- 点击输入框右端的 `▾`：收起或展开浮层。

**文字与分段的同步**：

- 文字按列的类型格式解析：PG 在 ISO DateStyle 下为 `YYYY-MM-DD HH:MM:SS[.ffffff][±TZ]`，MySQL 格式相同但没有时区。
- 文字解析失败时，分段区变暗，但仍然可以直接编辑文字。
- 调整某一段时，只改写文字中对应的那部分，小数秒和时区后缀保持不变。
- 各段在自身取值范围内循环，不向上一级进位（与 macOS、DataGrip 的行为一致）。改动年或月后，日会被限制在该月的天数之内。

**「现在」**：取客户端本地时间，按列的类型格式化，timestamptz 带上本地时区偏移。

**NULL 与 DEFAULT**：

- 只能通过对应的选项设置。直接输入文字 `<null>` 或 `DEFAULT`，得到的仍然是字符串。
- 修改后的值因此有三种：文本、NULL、DEFAULT。表格里分别显示为原文、`<null>`、`<default>`，都带「已修改」的样式。
- 不可空的列不显示 NULL 选项；没有默认值的列不显示 DEFAULT 选项。
- 命令面板另有「Set NULL」「Set DEFAULT」两条命令，默认不绑定键位。

### 10.3 保存（C-s / `:w`）

按行分组，每行生成一条 UPDATE，WHERE 中带上被修改列的旧值，作为乐观校验：

```sql
UPDATE t SET c1 = $1, c2 = DEFAULT
WHERE pk = $2 AND c1 IS NOT DISTINCT FROM $3 AND c2 IS NOT DISTINCT FROM $4
-- pk 为行标识列（§10.1），有多列时逐列比较；MySQL 用 <=>；DEFAULT 直接写成关键字，不作为参数
```

- **auto 事务模式**：所有 UPDATE 放在同一个事务里，每条都必须恰好影响 1 行。否则整体回滚，保留修改标记，并在出问题的行上提示「数据已变化或行不存在」，以免悄悄覆盖别人的修改。
- **manual 事务模式**：UPDATE 直接在用户当前的事务中执行，不自动提交。
- **保存成功后**：重新加载当前页，拿到数据库规整过的值，例如数值格式、触发器写入的字段。

### 10.4 刷新（R）

丢弃全部修改。有未保存修改时是否需要二次确认，取决于 PRD 10.2 #1 的结论；实现上两种都支持。

## 11. Console 与 result

**编辑器**（`internal/editor`）：覆盖 nvim 的基础编辑操作，行为以 nvim 为准。不做宏和 `.` 重复。预计 2000–3000 行加测试，其中块选择约 300–500 行；这是整个项目里最大的一块自研代码。

**支持的操作**（都支持次数前缀）：

| 类别 | 内容 |
|---|---|
| 模式 | NORMAL、INSERT、REPLACE（`R`）、VISUAL（`v`）、VISUAL LINE（`V`）、VISUAL BLOCK（`C-v`） |
| 移动 | `h j k l`、`w b e ge`、`W B E gE`、`0 ^ $`、`gg G {n}G`、`f F t T ; ,`、`%`、`{ }`、`H M L`、`C-d C-u C-f C-b`、`zz zt zb`、`* #` |
| 搜索 | `/ ?` 回车，`n N` 跳转。正则用 Go 的 RE2 语法（接近 vim 的 `\v`），smartcase |
| 进入 INSERT | `i a I A o O`、`s S` |
| 操作符 | `d c y`、`> <`、`gu gU g~`（大小写）、`gc`（切换 `--` 注释，同 nvim 0.10 起内置的行为）。操作符可以接任意移动或文本对象，例如 `dw`、`c$`、`y2j`、`gUiw` |
| 简写 | `dd cc yy`、`D C Y`（`Y` 等同 `y$`，与 nvim 默认一致）、`x X`、`r{字符}`、`~`、`J`、`p P`、`>> <<`、`gcc` |
| 文本对象 | `iw aw`、`iW aW`、`i( a(`（`ib ab`）、`i[ a[`、`i{ a{`（`iB aB`）、`i' a'`、`i" a"`、`` i` a` ``、`ip ap` |
| VISUAL | 可以用移动和文本对象扩展选区（如 `viw`、`vi(`）；`o` 切换选区的两端，`gv` 重新选中上次的选区；选区上可用 `d c y x > < u U ~ J gc` |
| VISUAL BLOCK | 按显示列选出一个矩形；`$` 把选区延伸到每行行尾；`o` / `O` 切换对角。可用的操作：`I` / `A` 在每行的块首 / 块尾插入（输入先显示在第一行，按 esc 后复制到其余各行；`A` 会给较短的行补空格）；`c` 修改整块；`d` / `x` 删除整块；`y` 复制整块，寄存器记为块类型；`p` / `P` 按块粘贴；`r{字符}`；`~ u U`；`> <`；`gc` |
| 撤销 | `u` 撤销、`C-r` 重做、`U` 撤销当前行上的全部修改，与 nvim 一致。想把 `U` 当成重做（Helix 的习惯），映射一行 `U = "<C-r>"` 即可 |
| 命令行 | `:{n}` 跳到第 n 行；`:s` 与 `:%s` 替换，支持选区范围 `'<,'>` 和标志 `g i`，替换串中的 `\1`、`&` 按 vim 的写法；`:w`、`:q` |
| INSERT 下 | Backspace、`C-w`、`C-u`、方向键；`↵` 换行并保持上一行的缩进；Tab 按 `tab_width` 插入空格。模糊补全见 §9.7：输入时自动弹出，也可以用 `C-n` / `C-p` 手动唤起 |
| 寄存器 | 只有无名寄存器，并与系统剪贴板同步（OSC 52，`tea.SetClipboard`） |

**不做**：宏（`q` `@`）、`.` 重复、具名寄存器（`"a`–`"z`）、标记与跳转列表、折叠、句子与标签类文本对象（`is` `as` `it` `at`）、`:g` 等其他 ex 命令。

**块选择的补充说明**：

- 块的边界落在 Tab 或宽字符中间时，按 nvim 的规则处理，由差分测试兜住（§15）。
- `C-v` 在 macOS 和 Linux 的终端里都能收到。Windows Terminal 默认把它当作粘贴，需要在终端设置里释放这个键，或者把块选择改绑到 `<C-q>`（与 Windows 版 vim 的做法相同）。

**实现结构**：按 vim 的语法组织，即「[次数] 操作符 [次数] 移动或文本对象」，由一个小状态机解析等待中的按键。

- 移动和文本对象都是「位置 → 范围」的函数，范围分为按字符（含端点或不含端点）和按行两种。
- 操作符作用在范围上。
- 因此每新增一个移动或文本对象，都能自动和所有操作符组合，不用逐个组合去写。

**其他要求**：

- **撤销的粒度**：一条 NORMAL 命令，或一次完整的 INSERT，算一个撤销步骤。实现上保存整份快照；缓冲区是 `[]string`，几 MB 以上的文件会变慢，SQL 文件一般遇不到。
- **粘贴**：支持 bracketed paste，粘贴内容一律作为文本插入。否则在 NORMAL 下粘贴，会把文本当作命令执行，可能误删内容。
- **键位映射**：按 §6.6 配置。`[map.console.normal]` 和 `[map.console.visual]` 只对 console 生效，例如 `L = "5l"`。用户映射会覆盖编辑器内置的同名 vim 键（如 `J` 合并行），与 nvim 的 `nnoremap` 行为一致。

**完整 vim 的出口**：命令面板中的「Edit in $EDITOR」命令，通过 `tea.ExecProcess` 暂停 TUI，用外部编辑器打开当前文件，退出后重新载入。默认不绑定键位。

**文件**：

- 每个 console tab 对应 `~/.local/share/sqlmux/consoles/<session>/<name>.sql`。
- 内容变更后 1s 内自动保存；`:w` 或 `C-s` 立即写入。

**执行（C-02）**：

- 在 NORMAL 或 VISUAL 下按 `↵`、点击 gutter 的 ▶、点击标题栏的 `▶ run` 都可以执行。有选区时执行选区，否则执行光标所在的语句。
- 每次最多取 `console.max_rows` 行（默认 1000），超出部分截断，并提示已截断。

**结果区（C-04 至 C-07，已确认）**

参考 DataGrip、DBeaver 的做法：所有结果集中显示在底部一个全宽的区域里，内部用 tab 区分。这样做有两个原因：

- 表格最需要的是宽度。PRD 原方案是在 console 旁边分割，结果只能占到屏幕的一半，向右分时甚至只有四分之一。
- 结果多了只是 tab 变多，pane 数量不变，布局不会越用越乱，这正是 C-05 想要解决的问题。

```
┌⟨0⟩ schema─┐┌⟨1⟩ data · t_order ─────────┐┌⟨2⟩ console · console_1 ─── ▶ run ↵┐
│ t_order   ││ WHERE deleted_at is null    ││ select * from mt_task              │
│ t_user    ││ …                           ││ where status = 'running';          │
│ …         │└─────────────────────────────┘└────────────────────────────────────┘
│           │┌⟨3⟩ result · console_1 #42 ─── 3 rows · 8ms  [重跑][转置][固定][导出][×]┐
│           ││ id    biz_type   status    created_at                                 │
│           ││ 689   goal       running   09-21 10:02                                │
│           ││ …                                                                     │
│           ││ 1:日志  2:console_1 #42*  3:console_1 #40（已固定）                    │
└───────────┘└───────────────────────────────────────────────────────────────────────┘
```

**出现与位置**：

- 某个 window 里第一次执行 SQL 时，在主区域（schema 树右侧）的底部新建一个全宽的 result pane，默认占 40% 的高度。
- 拖动边界或用 `SPC J` / `SPC K` 调整高度后，按 window 记住。
- 它是一个普通 pane，可以缩放（`SPC z`）、关闭（`SPC x`）；关闭后，下次执行时会重新出现。
- 每个 window 只有一个结果区，这个 window 里的所有 console 共用。

**tab 的组织**：

- **日志 tab**：第 1 个 tab 固定为「日志」，不能关闭。
  - 每执行一条语句就追加一行，内容是：时间、来源 console、语句的第一行、结果（行数、影响行数或错误）、耗时。
  - 执行出错时，自动切到日志 tab，显示完整的错误信息；同时，console 里出错语句的 ▶ 标记变成红色。
- **console 的结果 tab**：每个 console tab 在结果区里有自己的结果 tab，标题形如 `console_1 #42`。
  - 同一个 console 再次执行时，替换它原有的结果 tab，序号加一。C-05 的复用从 pane 级别改到了 tab 级别。
  - 一次执行多条语句时，每个结果集各占一个 tab（`#42`、`#42·2`……），下次执行时整组替换。
  - 非查询语句（如 `UPDATE 3`）只写进日志，不单独开 tab。
- **执行中**：结果 tab 先显示占位内容（执行中、已用时、`C-c` 取消），完成后再替换成结果。这一做法参考了 pgtui 的 `result_tabs`。
- **固定与关闭**：
  - `P` 固定当前结果 tab：它不会再被替换，下次执行另开新 tab（C-06）。
  - `q` 关闭当前结果 tab。
  - 快速 SQL 里按 `C-t`，结果会作为一个已固定的 tab 放进结果区。
- **切换**：`gt` / `gT`，或者直接点击 tab（T-02）。

**焦点**：执行之后，焦点留在 console，方便接着写；结果区只切换到对应的 tab。按 `C-j` 进入结果区。

**工具行**：

- pane 标题的右侧显示来源与序号、行数、耗时（C-07），以及可以点击的按钮：重跑、转置、固定、导出 CSV、关闭 tab。
- 表格的交互与 data pane 相同（G-01、G-05、G-06），但只读。

**配置**：`result_split = bottom | console`。

- `bottom`（默认）：即上面描述的底部结果区。
- `console`：保留 PRD 的原方案，在 console 旁边分割；宽度大于高度的 2 倍时向右分，否则向下分。

## 12. 命令面板与快速 SQL（K、F）

命令面板是执行命令的主要入口，取代了 M0 最初的 `:` 命令行（M0 用户反馈）。M0 先做面板本身，包括命令、表、pane、window 四个范围（F0.13、F0.14）；表在 M0 里用侧栏的假数据，M1 换成 catalog。session、SQL 范围和 DDL 预览在后面的里程碑。

- **打开**：`C-p`（`palette.open`）打开，范围为「所有」；NORMAL 下按 `:`（`palette.command`）打开并直接进入命令范围，相当于输入了 `>`。
- **ex 别名**：命令可以带别名，如 `q`（关闭 tab）、`qa`（退出）、`w`（保存）。在命令范围里，输入与某个别名完全相同时，这条命令排第一，所以 `:q↵`、`:qa↵` 的用法不变。
- **数据来源**：session、window、pane 取自工作现场；表取自 catalog；命令取自 Action 注册表（带标题的 Action）；SQL 取自快速查询的历史。
  - 只在浮层或输入状态里用的 Action 不作为候选：离开那个上下文，执行它们没有意义，标题单独看也看不懂。包括：
    - 浮层作用域里的，比如面板自己的 `palette.up` / `palette.down` / `palette.run` / `palette.close`，以及以后的 `where.*`、`cols.*`；
    - `cell`、`input` 作用域里的，比如 `cell.accept`「确定」、`cell.up`「加一」。
  - 判断方法是「只在这些作用域里有绑定」。
- **范围与前缀**：按 PRD K-02，范围标签为 所有 / 会话 / 窗口·Pane / 表 / 命令 / SQL，M0 先有 所有、窗口·Pane、表、命令。`Tab` / `S-Tab` 切换范围标签；输入前缀直接限定范围：`>` 命令、`@` 表、`%` 窗口·Pane、`$` 会话、`;` SQL。
  - 范围只由输入里的前缀决定，不另存状态：`Tab` / `S-Tab` 就是改写前缀（「命令」→ `>`、「表」→ `@`、「窗口·Pane」→ `%`，「所有」去掉前缀），输入的其余内容保留；范围标签按当前前缀高亮。`:` 相当于输入了 `>`，也是同一套机制。
- **每一行**（K-03）：图标、名称、所在位置（`dim` 色）、右侧的键位或 ON / OFF、类型标签（命令 / 表 / Pane / 窗口）。图标都放在最左列对齐。
  - pane 的名称是 `⟨1⟩ data · t_order`，所在位置是它所在的 window，如 `0: data`；
  - window 的名称是 `0: data`，所在位置是 session 名。
  - 命令的名称是中文标题，所在位置显示它的 action id，如 `左右分割  pane.split.right`。匹配的对象是「名称 + 空格 + 所在位置」这一整串，两部分都能高亮，所以输入 `split` 也能找到；action id 也正是在 config.toml 里绑键时要写的名字。
  - 表的所在位置是 `session.schema`，如 `doraemon.public`。
- **布局**：宽度 min(80, 窗口宽 − 4)，水平居中；上边缘固定在状态栏以上区域高度的 1/4 处，即 (H−1)/4，列表变长变短时输入行不动。单线边框、`focus` 色，标题「命令面板」。从上到下：
  - 输入行，`>` 等前缀照常显示在输入里；
  - 范围标签；
  - 分隔线、列表（最多 10 行，放不下时滚动）、分隔线；
  - 底栏：左边是移动和关闭的键位提示（从 keymap 读取），右边是 `↵ <按回车的效果>`。
- **光标**：用终端自己的光标，放在输入位置上，输入法的候选框也会跟着它。
- **开关类命令**（M0）：`tree.toggle`（侧栏展开时为 ON）、`pane.zoom`（缩放中为 ON）。
- **匹配**：使用 fzf 的算法，并支持它的扩展语法（§9.7），匹配到的字符用黄底高亮。
  - 输入为空时，先列最近用过的（不分种类）；其余按范围标签的顺序：window、pane（按 ⟨n⟩）→ 表（按侧栏顺序）→ 命令（按 action id）。
  - 输入不为空时，各种类混在一起按 fzf 的分数排。
  - 最近使用 M0 只记在内存里，M1 有了 state.json 之后持久化。
- **执行**：按 K-04。底栏右侧显示当前项按回车会做什么。
  - 命令：执行；开关类命令只切换状态，不关闭面板。
  - 表：打开到焦点所在的 data pane；焦点不在 data pane 上时，用这个 window 里的第一个 data pane；一个都没有时什么都不做。打开后焦点移到那个 data pane，因为打开表就是为了接着看数据。`↵` 在当前 tab 打开，`C-t` 在那个 pane 新开一个 tab 并切过去；底栏显示 `↵ 打开 · C-t 新 tab`。选中的不是表时，`C-t` 不起作用。
  - pane：聚焦。
  - window：切换。M5 之前只列出，`↵` 只关闭面板。
- **预览（K-05）**：光标在某张表上停留 150ms 后，从 `Meta` 获取 DDL，获取后缓存。窗口高度不够时，列表至少保留 3 行，底部提示始终显示，先压缩预览区。
- **快速 SQL**：
  - **执行**：在 `Meta` 上执行，PG 用 `BEGIN READ ONLY`，MySQL 用 `START TRANSACTION READ ONLY`，执行完一律 ROLLBACK。执行时加上 §9.4 的自动 LIMIT。
  - **写语句（F-05）**：判为写的语句不执行，显示黄色提示「`C-e` 在 console 中打开后执行」。在 console 里由用户自己按下执行，这一步就是确认，不再需要 C-S-↵。
  - **错误**：语法错误显示红色提示。
  - **已修改提示（F-03）**：当前输入与上次执行的语句不同时，提示「已修改，↵ 重新执行」。
  - **后续操作（F-04）**：`C-t` 把结果送到一个新的、已固定的 result pane；`C-y` 用 `encoding/csv` 生成 CSV，再通过 OSC 52 复制；`C-e` 在 console 中打开。结果区标题栏上的按钮都可以点击。
  - **历史**：保留最近 50 条，去重。

## 13. 安全

- **只读 session（S-04）**：
  - **应用层**：
    - 判为写的语句一律拦截，同时禁止进入单元格编辑。
    - SET 语句中，会解除只读的一律拦截：`SET TRANSACTION`、`SET SESSION CHARACTERISTICS`、`default_transaction_read_only`、`transaction_read_only`。
    - 其他不影响只读的 SET（如 `search_path`、`statement_timeout`）放行。
  - **数据库层**：连接建立后执行 PG 的 `SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY`，或 MySQL 的 `SET SESSION TRANSACTION READ ONLY`，兜住判定漏掉的情况（例如会写数据的函数）。
  - 只读是为了防止误操作，不是权限边界。真正需要限制权限时，应使用只读的数据库账号。这一点要写进用户文档。
- **快速 SQL**：只读由数据库事务保证，即使一条会写数据的 select 被判为读，也写不进去。
- **凭据**：
  - `connections.toml` 支持 `password_cmd`（例如 macOS 的 `security` 命令、`pass`）、`password_env`；PG 还会被 pgconn 自动读取 `~/.pgpass`。
  - 也允许直接写明文 `password`。但如果此时文件对同组或其他用户可读，启动时会给出警告。
- **注入**：值一律参数化，标识符一律按方言加引号。
- **日志与文件**：
  - 只有带 `--debug` 启动时才写日志，DSN 中的密码会被脱敏。
  - 历史记录里可能含有敏感的字面量，所以 state 文件和 console 文件的权限都设为 0600。

## 14. 配置与持久化

所有 Unix 平台都遵循 XDG 规范：优先读 `$XDG_CONFIG_HOME`、`$XDG_STATE_HOME`、`$XDG_DATA_HOME`，未设置时分别用 `~/.config`、`~/.local/state`、`~/.local/share`。macOS 上也用 `~/.config`，不用 `~/Library/Application Support`，这是终端工具的惯例。

**文件格式**：

- **配置文件用 TOML**。配置是用户手写的，需要能写注释；键位的分层表（如 `[keys.grid]`、`[map.console.normal]`）写成 TOML 的表最自然。lazysql、Helix、Alacritty 用的也是 TOML。
  - 不选 YAML：缩进容易写错，而且有隐式类型转换，比如 `no` 会被当成 false。
  - 不选 JSON：不能写注释。
- **state 文件用 JSON**。它只由程序读写，不需要用户手工编辑。

| 文件 | 写入方 | 内容 |
|---|---|---|
| `~/.config/sqlmux/config.toml` | 只有用户写 | 常规选项、主题、键位、映射 |
| `~/.config/sqlmux/connections.toml` | 由应用写入（S-03 新建连接） | 连接定义 |
| `~/.config/sqlmux/themes/*.toml` | 只有用户写 | 自定义主题 |
| `~/.local/state/sqlmux/state.json` | 由应用写入 | 最近使用、每张表的 WHERE 历史与收藏、快速 SQL 历史；以后还有工作现场 |
| `~/.local/share/sqlmux/consoles/` | 由应用写入 | console 的 SQL 文件 |

- 连接定义单独放一个文件，是因为应用改写 TOML 时会丢掉注释，所以不能去改用户手写的 config.toml。
- **启动时找不到连接**：没有 `connections.toml`、文件里没有连接，或者 `sqlmux <名字>` 找不到这个名字时，在终端打印一行错误就退出（退出码 1），不进入界面。错误里写明配置文件的路径；名字找不到时列出已有的连接名。连接失败（比如密码错误）也一样，打印数据库返回的错误后退出。在界面里新建连接（S-03）要到 M5。
- state 文件先写到临时文件，再 rename 过去，保证原子性。

```toml
# config.toml
theme        = "tokyonight-storm"
icons        = "nerd"            # nerd | ascii
timeoutlen   = 1000
result_split = "bottom"          # bottom | console（§11）
result_height = 0.4              # 底部结果区默认所占的高度比例
formatprg    = ""                # 例如 "pg_format -"；为空时使用内置的 sql-formatter
keyword_case = "lower"
tab_width    = 2                 # console 缩进与格式化共用

[console]
max_rows = 1000

[keys]
leader = "<Space>"               # 改成 "<C-a>" 即为 tmux 式的全局前缀

[keys.normal]
"<C-w>v" = "pane.split.right"    # 想要 vim 风格的窗口键，可以自己添加
"<C-w>s" = "pane.split.below"

[keys.grid]
"x" = ""                         # 解绑

[map.normal]                     # 键 → 键序列，语义同 vim 的 noremap，所有 pane 通用
J = "5j"
K = "5k"

[map.console.normal]             # 只在 console
L = "5l"
H = "5h"

[map.grid.normal]                # 只在表格（data / result）
L = "5l"
H = "0"
```

```toml
# connections.toml
[[connection]]
name         = "doraemon"
engine       = "postgres"        # postgres | mysql
dsn          = "postgres://ctw@localhost:5432/doraemon?sslmode=disable"
password_cmd = "security find-generic-password -s sqlmux-doraemon -w"
read_only    = false
```

## 15. 测试

- **单元测试**（表驱动）：
  - `sqlkit`：扫描、分句、读写判定、自动 LIMIT、WHERE 拼接与多语句拦截，以及补全上下文判断（光标位置 → 候选类别、表的别名、CTE 和子查询的层级）。用例可以参考 lazysql 的 `sql_context_test.go`。
  - `keymap`：解析、trie 合并、次数前缀、映射展开（含按 pane 类型的优先级）、冲突检测、提示。
  - 模糊匹配：对 fzf 算法的封装，以及扩展语法的解析。
  - 布局树：分割、关闭、调整大小、按方向切焦点。
  - 保存时生成的 UPDATE 语句。
  - 时间分段的解析与调整。
- **编辑器与 nvim 的差分测试**：
  - `internal/editor/testdata/cases.txt` 中每条用例包含：初始文本、光标位置、按键序列。
  - `go generate` 调用 `nvim --headless --clean`，逐条执行 `normal!`，记录结果的文本、光标位置和无名寄存器，作为 golden 文件提交。
  - `go test` 把自研编辑器的结果和 golden 文件逐条比对。CI 上不需要安装 nvim；只有新增用例或升级 nvim 版本时，才需要重新生成。
  - 已验证可行：在 nvim 0.12.4 上跑了 `ciw`、`daw`、`di(`、`gUiw`、`D`、`J`、`caw`、`dd`、`C`、`gcc`，以及块选择的 `I`、`$A`、`d`、`c`、`y` + `p` 等用例，都能拿到结果文本、光标位置和寄存器类型。
  - 生成期望结果时，用 `silent!` 执行按键，避免 nvim 的提示消息混进输出。
- **默认键位兼容性测试**：断言 `default.toml` 中没有只能在 kitty 协议下使用的键，也没有 Alt 组合键。
- **格式化 golden 测试**：几条典型的 PG / MySQL 语句，格式化结果与 golden 文件比对。升级 sql-formatter 时，输出如有变化，这个测试会失败。
- **渲染 golden 测试**：在固定尺寸 160×45 下渲染 Frame，与 golden 文件比对（`charmbracelet/x/exp/golden` 已是 bubbletea 的依赖）。场景对应设计稿的几种状态：WHERE 下拉、COLS 下拉、命令面板的 SQL 模式、转置、result 在右侧或下方、时间选项浮层、which-key。
- **集成测试**：用 `docker compose` 启动 postgres:17 和 mysql:8.4，运行 `go test -tags integration ./internal/db/...`。覆盖以下内容：catalog、文本值、取消、只读拦截、保存时的乐观校验、枚举和可空属性、用唯一索引作为行标识、无主键表只读、按 console 切换 `search_path`。
- **手工测试矩阵**：Terminal.app、iTerm2、Ghostty、WezTerm、Linux 下的 GNOME Terminal。每个都分别在 tmux 内外测试，重点检查默认键位和鼠标。

## 16. 里程碑

| 阶段 | 内容 | PRD 条目 |
|---|---|---|
| M0 骨架 | Bubble Tea 程序、Frame、Block、主题与主题文件、状态栏；keymap（序列、leader、which-key、次数、映射、提示）；Action 注册表；命中表与键位提示按钮；命令面板（命令、表、pane、window 范围） | B-01~03、K-01~04 的面板部分、第 6 章、第 7 章基础 |
| M1 浏览 | PG 连接、schema 树、data pane 只读（WHERE 条件及其补全、历史/收藏、ORDER/LIMIT/PAGE/COLS、转置）、滚轮 | D-01~04、Q-01~06、G-01、G-04~06、T-01~03 |
| M2 编辑 | 单元格编辑与按列属性的选项、待提交标记、保存与刷新 | G-02、G-03 |
| M3 console | vim 编辑器（用 nvim 差分测试校验）、SQL 模糊补全、高亮、分句与执行前高亮、执行与取消、sql-formatter 格式化、底部结果区（日志 tab、结果 tab 的复用与固定）。编辑器（含块选择）是工作量最大的一项，可以从 M0 起并行开发 | C-01~07 |
| M4 命令面板 | 面板的 session 与 SQL 范围、DDL 预览、快速 SQL（面板本身已在 M0 完成） | K-02、K-05、F-01~05 |
| M5 工作现场 | 多 session 与多 window、pane 的分割/缩放/关闭/拖拽、session 列表、只读、事务模式、MySQL | S-01~04、W-01~02、P-01~04 |
| M6 配置 | 键位覆盖/冲突/导出、持久化（主题文件已在 M0 完成） | 第 6 章 |

M1 完成后，就有一个能日常查数据的只读版本，可以尽早给用户试用。

## 17. 待定问题

已确认：

- 技术栈 Go；leader 用 `SPC`；产品名 sqlmux。
- 表格的 chip 与翻页键：`go` `gl` `gp` `gc`、`]` `[`、`/`。
- WHERE 输入完整的条件表达式，不做简写。
- 编辑器支持块选择（`C-v`）。
- 结果展示采用底部全宽结果区（§11）。
- PG 的 schema 选择与 DataGrip 一致：console 标题右侧有 schema 下拉框，每个 console 单独选择（§8.6）。
- 行标识列依次取主键、所有列都不可空的唯一索引；两者都没有的表只读，也不加默认排序，不做额外提示（§10.1、§8.5）。
- 边界情况暂不处理，出现了再说。

暂不讨论，按文中做法实现：

- 「按行提交」：一个事务，每行一条 UPDATE，全部成功才提交（§10.3）。

目前没有待定问题。实现过程中出现的新问题，由 worker 或 tester 提给决策者，决策后更新本文。

PRD 10.2 中影响实现的几条：

- #1 刷新时是否确认：两种实现都支持。
- #2 分页后行号是否连续：只是一个参数。
- #4 which-key：已采纳，见 §6.5。
- #6 工作现场是否恢复：数据模型已经可以序列化。

## 18. 需要同步到 PRD / 设计稿的改动

| # | 改动 | 涉及 |
|---|---|---|
| 1 | 前缀 `C-a` 改为 leader `SPC`，只在 NORMAL 模式下生效；pane 焦点新增直达键 `C-h/j/k/l` | 第 2 章「tmux 心智模型」、5.1–5.5、6.2 |
| 2 | 命令面板只用 `C-p` 打开，`C-k` 改为 pane 焦点向上 | K-01、6.2 |
| 3 | console 执行由 `⌥↵` 改为 NORMAL / VISUAL 下的 `↵` | C-02、6.2 |
| 4 | `C-↵` 改为：schema 树中用 `t`，命令面板中用 `C-t` | D-03、K-04、F-04 |
| 5 | 快速 SQL 中写语句的确认，由 `C-S-↵` 改为 `C-e` 在 console 打开后执行 | F-05 |
| 6 | 单元格编辑增加按列属性的选项：时间分段、NULL、DEFAULT、布尔、枚举、原值 | G-02、10.2 #5 相关 |
| 7 | 采纳 which-key 提示层，且各项可点击 | 10.2 #4 |
| 8 | 补充 chip 键位 `go`/`gl`/`gp`/`gc`，翻页 `]`/`[`，WHERE 聚焦 `/`（已确认） | Q-03 |
| 9 | 补充规则：界面上的每条键位提示都是可点击的按钮 | 第 7 章 |
| 10 | console 的 tab 用 `:q` 关闭，因为 `x` 是 vim 的删除字符 | T-02 |
| 11 | 默认键位只使用任何终端都能区分的键 | 6.3 |
| 12 | 新增 console 的 SQL 模糊补全；WHERE 的字段补全和 `C-r` 历史改为 fzf 式模糊匹配 | C-01、Q-01、Q-02 |
| 13 | vim 映射可以按 pane 类型分别配置 | 6.3 |
| 14 | WHERE 不做简写语法，直接输入完整的 SQL 条件表达式 | Q-01 |
| 15 | 结果改为在底部全宽结果区中显示：第 1 个 tab 为日志，每个 console 复用自己的结果 tab，固定的结果单独保留。原来在 console 旁边分割的方式保留为可选配置 | C-04~C-07、第 4 章 |
| 16 | console 编辑器支持块选择 `C-v` | C-01、6.1 |
| 17 | console 标题右侧增加 schema 下拉框（仅 PG，与 DataGrip 一致），每个 console 单独选择 schema | 5.7、3 章 Session 行 |
| 18 | 取消 `SPC 0-9`（原 C-a 0-9）按编号切换 window，改为 `SPC n` / `SPC p` / `SPC l` 和命令面板的 window 范围；默认键位只保留常用的，其余在命令面板里执行，或者自己绑定 | W-02、6.2 |
| 19 | `:` 命令行由命令面板取代：`:` 打开面板的命令范围，`:q` 等用法不变 | B-02、K-01 |
| 20 | 侧栏标题显示当前 schema，可以点击切换；侧栏宽度可以拖动 | D-01~D-04、第 4 章 |
| 21 | 主题可以写成 `~/.config/sqlmux/themes/` 下的文件；表格按列的类型着色 | 第 8 章 |

设计稿里写死的键位文字（如 `C-a b`、`run ⌥↵`、`C-↵ 送到 result pane`）会按新的默认键位显示；实现中这些文字都从 keymap 读取，不写死。
