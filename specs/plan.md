# sqlmux 开发计划

范围和设计以 [`tech-design.md`](tech-design.md) 为准。本文件列出每个 feature 的内容、验收标准和状态。

- **分工**：决策者负责增删 feature、修改验收标准；worker 负责更新状态。协作流程见 [`AGENTS.md`](../AGENTS.md)。
- **状态**：`todo`（未开始）→ `doing`（开发中）→ `testing`（已提交，等待测试）→ `passed`（测试通过）；测出问题时为 `failed`，修复后回到 `testing`。

## M0 骨架（不连数据库，界面数据都是假数据）

### F0.1 工程初始化、启动与退出 · 状态：testing

**内容**
- 初始化仓库：`git init`，`.handoff/` 写入 `.git/info/exclude`，`.gitignore` 忽略 `bin/`。
- Go module 名为 `sqlmux`，发布前再改成正式的仓库路径。目录结构按 tech-design §4，入口为 `cmd/sqlmux/main.go`。
- Bubble Tea v2 程序：开启 AltScreen、`MouseModeAllMotion`、bracketed paste；终端支持时顺带开启键盘增强协议。
- `:qa` 退出。空闲时按 `C-c` 不退出，只弹出 toast 提示「输入 :qa 退出」。

**验收**
- `go build ./... && go vet ./... && go test ./...` 全部通过。
- 在 tmux 中以 160×45 启动，进入全屏界面；执行 `:qa` 后程序退出，终端恢复正常（shell 提示符正常、光标可见）。
  - 用来判断的客观数据：`tmux display -p '#{alternate_on} #{mouse_all_flag} #{mouse_sgr_flag} #{bracket_paste_flag} #{cursor_flag}'`。运行时前四项应为 1；退出后前四项回到 0，`cursor_flag` 回到 1。
- 按 `C-c` 程序不退出，并出现提示。
- 窗口调到 100×30 再调回原尺寸，界面都能正确重绘，没有残影，也不 panic。

### F0.2 Frame、Block、主题与静态布局 · 状态：todo

**内容**
- `internal/ui`：
  - Frame：Canvas、命中表、主题、指针位置。
  - Block：单线边框；上边框左侧嵌标题、右侧嵌提示；聚焦时用焦点色。
- 主题使用 tokyonight-storm 的 token（tech-design §7.3）；图标支持 `icons = "nerd" | "ascii"` 两种。
- 默认 window 的静态布局：⟨0⟩ schema 侧栏、⟨1⟩ data、⟨2⟩ console，内容都是占位用的假数据。

**验收**
- 有 160×45 渲染的 golden 测试。
- 聚焦的 pane，边框和标题为 `focus` 色（#9ece6a）；其余 pane 为暗色。
- `icons = "ascii"` 时，画面中不出现 Nerd Font 字形（私有区码点 U+E000–U+F8FF）。
- 在 80×24 下不 panic、不越界；标题过长时截断。

### F0.3 状态栏 · 状态：todo

**内容**

单行状态栏（PRD B-01~B-03），数据为假数据：

- 左侧：session（引擎图标 + 名称），然后是 window 列表（`序号:名称`，当前 window 带 `*`）。
- 右侧依次为：模式附加信息、`C-p` 命令面板入口、待输入序列、光标位置、连接地址、模式块。

**验收**
- 有 golden 测试。
- 模式块颜色：NORMAL 绿、INSERT 黄、VISUAL 紫、COMMAND 青。按 `:` 进入 COMMAND 模式即可验证。
- 按下 `SPC`、`g` 或数字后，状态栏显示待输入的序列；序列完成或按 esc 后清空。
- 宽度为 80 时，省略模式附加信息，模式块仍然可见。

### F0.4 keymap 引擎与配置 · 状态：todo

**内容**（tech-design §6.2–§6.8）
- keymap 本身：
  - vim 记法解析；
  - 作用域与优先级合并；
  - 按键序列与 `timeoutlen`；
  - leader：`<Space>` 只在 NORMAL 下生效，配成 Ctrl 组合时全局生效；
  - 次数前缀；
  - 用户映射 `[map.<mode>]`、`[map.<pane>.<mode>]`，语义同 noremap；
  - 冲突检测；
  - `keymap.Hint`。
- 默认键位：`default.toml`（embed），写入 §6.8 的全部默认键位。
- 配置文件：读取 `$XDG_CONFIG_HOME/sqlmux/config.toml`，未设置时读 `~/.config/sqlmux/config.toml`。
- 命令行：`sqlmux keys`（输出 markdown 表）、`sqlmux keys --format toml`、`sqlmux keys --check`。

**验收**
- 单元测试覆盖上面的每一项，至少包括：
  - 纯前缀节点一直等待；歧义节点超时后执行；
  - 次数能传给 Action；
  - 映射优先级为「pane 类型 > 通用 > 默认」；
  - leader 配成 `<C-a>` 后，在 INSERT 模式下也生效。
- 默认键位兼容性单测：`default.toml` 中没有 Alt 组合键，也没有只能在 kitty 协议下使用的键。
- `sqlmux keys` 能输出当前生效的键位表。在配置里写入重复绑定时，`sqlmux keys --check` 以非零状态码退出，并指出冲突所在的作用域和键。

### F0.5 Action 注册表、命令行与 which-key · 状态：todo

**内容**
- Action 注册表（§6.1）。
- `:` 命令行（COMMAND 模式）：支持 `:q`、`:qa`，未知命令给出提示。
- which-key 浮层（§6.5）：
  - 按键序列停在纯前缀节点上 400ms 后出现；
  - 列出下一步可按的键和对应 Action 的标题，每一项都可以点击；
  - esc 取消。

**验收**
- 按 `SPC` 后等待约 0.5 秒，出现 which-key，内容与 §6.8 中以 `SPC` 开头的默认键一致。
- 按 `SPC` 后立即按下一个键，不出现浮层。
- 点击 which-key 中的某一项，效果与按对应的键相同。
- 按 esc 取消后，浮层关闭，状态栏的待输入序列清空。

### F0.6 布局树与 pane 操作 · 状态：todo

**内容**
- 布局树（`internal/app/layout.go`）：二叉分割树的分割、关闭、调整大小、按方向切换焦点。全部写成纯函数，配单元测试。
- 键位：

  | 键 | 操作 |
  |---|---|
  | `C-h/j/k/l`、`SPC h/j/k/l` | 切换焦点 |
  | `SPC %` / `SPC "` | 左右分割 / 上下分割 |
  | `SPC x` | 关闭当前 pane |
  | `SPC z` | 缩放 / 还原 |
  | `SPC H/J/K/L` | 调整大小 |
  | `SPC q` | 显示 pane 编号，再按数字跳转 |
  | `SPC b` | 折叠 / 展开 schema 侧栏 |

- 鼠标：
  - 点击 pane 使其获得焦点；
  - 双击 pane 标题，切换缩放；
  - 拖动 pane 之间的边界，调整大小；
  - 点击折叠后的细栏，展开侧栏。
- pane 编号 ⟨n⟩ 按树的遍历顺序计算；侧栏固定为 ⟨0⟩。

**验收**
- 布局树有单元测试。
- tmux e2e 测试：
  - 分割后出现新 pane，编号正确；
  - 按方向切换焦点的结果与各 pane 的几何位置一致；
  - 关闭 pane 后，兄弟 pane 占满腾出的空间；
  - 缩放后能还原；
  - 拖动边界后，比例随之改变；
  - 折叠侧栏后显示细栏，点击细栏可以展开。

### F0.7 命中表与鼠标 · 状态：todo

**内容**（§7.4）
- 键位提示即按钮：pane 标题右侧的提示、which-key 中的每一项、状态栏的 `C-p` 入口，点击后都执行对应的 Action。
- 悬停时高亮。
- 识别双击：400ms 内点中同一个目标。
- 滚轮作用于指针下方的 pane。
- 点击浮层外部，关闭浮层。

**验收**

在 tmux e2e 测试中注入 SGR 鼠标序列，检查以下几点：
- 点击提示，触发对应的 Action。
- 悬停时样式发生变化，用 `capture-pane -e` 检查颜色。
- 滚轮作用在指针所在的 pane 上。M0 阶段用占位内容的滚动偏移来验证。
- 点击浮层外部后，浮层关闭。

## M1–M6

各阶段的范围见 tech-design §16。每个阶段开工前：

1. worker 按 M0 的格式，把这个阶段拆成若干 feature 写进本文件，状态标为 `draft`；
2. 发给决策者确认；
3. 确认后再开工。

编辑器（M3 的 `internal/editor`，包括 nvim 差分测试框架）是纯逻辑包，worker 可以利用 M0 做完后等待测试反馈的空档提前开发。但它的优先级仍然低于 M0 和 M1。
