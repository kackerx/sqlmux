# M0 骨架 · 任务清单

- **目标**：把界面骨架、按键系统和鼠标交互搭起来。不连接数据库，界面上的数据全部是假数据。
- **范围**：PRD B-01~B-03、第 6 章、第 7 章的基础部分；对应 tech-design 的 §3、§6、§7。
- **依赖**：无。
- **完成标准**：F0.1–F0.15 全部 `passed`；在 M0 最后一个 commit 上，tester 跑一遍全部 e2e 脚本，结果全绿；用户验收通过后打 tag `m0`。

任务文件的格式和状态约定见 [`../plan.md`](../plan.md)。

---

## F0.1 工程初始化、启动与退出 · 状态：passed（a98b991；e2e fb74c03）

- **依赖**：无
- **涉及**：`cmd/sqlmux`、`internal/app`

**开发**
- [x] `git init`；`.handoff/` 写入 `.git/info/exclude`；`.gitignore` 忽略 `bin/`
- [x] `go mod init sqlmux`，按 tech-design §4 建立目录骨架
- [x] Bubble Tea v2 程序：AltScreen、`MouseModeAllMotion`、bracketed paste；终端支持时顺带开启键盘增强
- [x] `:qa` 退出；空闲时按 `C-c` 不退出，弹出 toast「输入 :qa 退出」

**验收**
- [x] `go build ./... && go vet ./... && go test ./...` 全部通过
- [x] 在 tmux 中以 160×45 启动，进入全屏；`:qa` 退出。检查 `tmux display -p '#{alternate_on} #{mouse_all_flag} #{mouse_sgr_flag} #{bracket_paste_flag} #{cursor_flag}'`：运行时前四项为 1；退出后前四项为 0，`cursor_flag` 为 1
- [x] 按 `C-c` 不退出，出现提示
- [x] 窗口调到 100×30 再调回原尺寸，重绘正确，没有残影，不 panic

## F0.2 Frame、Block、主题与静态布局 · 状态：passed（b43d1d1；e2e 2dc37fb）

- **依赖**：F0.1
- **涉及**：`internal/ui`（frame、block、theme、icons）、`internal/app`（组装 View）

**开发**
- [x] 主题：把 §7.3 的 token 定义成 Go 结构，内置 tokyonight-storm
- [x] 图标：做 nerd / ascii 两套映射。F0.2 先用代码参数切换，F0.3 再接入配置文件
- [x] Frame：包含 Canvas、命中表（这一步只记录区域，不处理鼠标事件）、主题、指针位置；提供绘制文字、登记区域的基础 API
- [x] Block：单线边框；上边框左侧放标题、右侧放提示；聚焦时用焦点色；标题过长时截断
- [x] 静态布局：侧栏宽度、5 : 4 的宽度比例、1 列间隔都按 §7.8；pane 里放占位内容（含占位的 tab 栏）
- [x] golden 测试框架，覆盖 160×45 和 80×24 两个尺寸

**验收**
- [x] 160×45 与 80×24 的 golden 测试通过
- [x] 聚焦 pane 的边框和标题为 `focus` 色（#9ece6a），其余 pane 为暗色
- [x] `icons = "ascii"` 时，画面中不出现私有区码点（U+E000–U+F8FF）
- [x] 80×24 下不 panic、不越界。标题栏空间不够时，按 §7.8 的顺序退让：先截短标题中的对象名，再从优先级最低的提示开始丢；160×45 下，console 标题栏的 `doraemon.public ▾` 和 `▶ run ↵` 都能完整显示
- [x] 布局尺寸符合 §7.8：侧栏 32 列（窗口宽度小于 100 列时为 24 列）、data 与 console 为 5 : 4、横向间隔 1 列

## F0.3 keymap 引擎与配置 · 状态：passed（32c49a1；e2e cad10a2）

- **依赖**：F0.1。这一项是纯逻辑加命令行，不依赖 F0.2 的界面，可以和 F0.2 交错进行。
- **涉及**：`internal/keymap`、`internal/config`、`cmd/sqlmux`（`keys` 子命令）

**开发**
- [x] 键位记法的解析与规范化：`<C-p>`、`<Space>`、`<S-Tab>`、`<CR>`、`<Esc>`，以及普通字符序列
- [x] 作用域 trie，并按当前上下文合并优先级（§6.4）
- [x] 按键序列：纯前缀节点一直等待；歧义节点等待 `timeoutlen` 后执行
- [x] leader：`<Space>` 只在 NORMAL 下生效；配置成 Ctrl 组合时全局生效
- [x] 次数前缀：通过 `Args.Count` 传给 Action；没有输入次数时，`0` 作为普通按键
- [x] 用户映射 `[map.<mode>]`、`[map.<pane>.<mode>]`，语义同 noremap，优先级为 pane 类型 > 通用 > 默认
- [x] 冲突检测；`keymap.Hint`
- [x] `default.toml`（embed）写入 §6.8 的全部默认键位
- [x] 配置加载：读取 `$XDG_CONFIG_HOME/sqlmux/config.toml`，未设置时读 `~/.config/sqlmux/config.toml`；把 `icons` 接进来
- [x] 命令行：`sqlmux keys`（输出 markdown 表）、`--format toml`、`--check`

**验收**
- [x] 单测覆盖以下几项：
  - 纯前缀节点一直等待；歧义节点超时后执行；
  - 次数能传给 Action；
  - 映射优先级；
  - leader 配成 `<C-a>` 后，在 INSERT 下也生效。
- [x] 默认键位兼容性单测：`default.toml` 中没有 Alt 组合，也没有只能在 kitty 协议下用的键
- [x] `sqlmux keys` 输出当前生效的键位表；`--format toml` 的输出能被重新加载
- [x] 在配置中写入重复绑定后，`sqlmux keys --check` 以非零状态码退出，并指出冲突的作用域和键
- [x] 把 `XDG_CONFIG_HOME` 指向临时目录后，写入的配置能生效（例如 `icons = "ascii"`）

## F0.4 Action 注册表与命令行 · 状态：passed（0e26160；e2e 6967c6a）

- **依赖**：F0.3
- **涉及**：`internal/app`（action、mode、cmdline）

**开发**
- [x] Action 注册表（§6.1），keymap 解析出的结果分发到对应的 Action
- [x] 模式由状态推导（§3 原则 3）：NORMAL / INSERT / VISUAL / COMMAND
- [x] `:` 命令行：显示在状态栏左侧（§7.8）；按 esc，或用退格删到空时关闭；支持 `:q`、`:qa`；输入未知命令时弹出 toast「未知命令: xxx」
- [x] `:q` 关闭当前 pane 的当前 tab（M0 中是占位 tab）；关掉最后一个 tab 时，连同 pane 一起关闭（侧栏除外）；如果它是 window 里唯一的 pane，就保留为空 pane，不关闭
- [x] 空闲时连按两次 `C-c` 退出（§6.8）：第一次按下弹出 toast「再按一次 C-c 退出」，2 秒内再按一次就退出，超过 2 秒则重新计时。这条规则取代 F0.1 中「按 C-c 不退出」的行为
- [x] 空 pane 的标题栏右侧不显示提示（§7.8「pane 标题」中的「空 pane」）
- [x] 「再按一次 C-c 退出」的 toast 只显示 2 秒；toast 还在时再按才退出，消失后再按重新算第一次（§6.8、§7.8「toast」）

**验收**
- [x] `:q` 关闭当前 tab；关掉最后一个 tab 后，pane 也被关闭
- [x] 窗口里只剩一个 pane 时，用 `:q` 关掉它的最后一个 tab 后，这个 pane 保留下来，变成空 pane：
  - 内容区为空，不显示任何占位内容；
  - 标题栏只显示 `⟨n⟩ <图标> 类型`，右侧没有 `▶ run`、schema 下拉框等提示；
  - tab 栏里只剩一个可以点击的 `+`；
  - `:q` 不会让程序退出。
- [x] 命令行打开时按 `C-c`，只关闭命令行，不计入「连按两次退出」
- [x] `:qa` 退出；输入未知命令时出现提示
- [x] 连按两次 `C-c` 退出，退出后终端恢复正常（按 F0.1 的 tmux 标志位检查）；只按一次时不退出，并出现提示，提示 2 秒后消失；两次之间隔超过 2 秒时不退出，而是重新出现提示
- [x] 按 esc 或把命令行删空，命令行关闭，回到 NORMAL
- [x] 所有键位最终都经由 Action 执行：单测里能直接调用 Action，结果与按键一致

## F0.5 状态栏 · 状态：passed（0883129；e2e 6967c6a）

- **依赖**：F0.2、F0.3、F0.4
- **涉及**：`internal/ui`（statusline）

**开发**
- [x] 左侧：session 块、window 列表；右侧：模式附加信息、`C-p` 入口、待输入序列、光标位置、连接地址、模式块。颜色、间距、顺序都按 §7.8
- [x] 模式附加信息按 §7.8 显示：M0 中只有 NORMAL（不显示）和 COMMAND（显示匹配的命令，如 `:q | :qa`）两种情况
- [x] 待输入序列为空时，显示 `dim` 色的 `·`，占位保持不变
- [x] 窗口窄时，按 §7.8 的顺序依次省略
- [x] 恢复 Session 结构（名称、引擎、连接地址、window 列表），假数据用两个 window：`0: data*` 和 `1: report`
- [x] 待输入序列取自 keymap 的当前状态，模式取自 F0.4 的推导结果
- [x] 待输入这一块的序列部分至少 3 列宽、内容靠左（§7.8「状态栏」），按 `SPC`、`g`、数字时右侧各块不移动

**验收**
- [x] golden 测试通过
- [x] 模式块颜色：NORMAL 为绿色；按 `:` 进入 COMMAND 后为青色
- [x] 按下 `SPC`、`g` 或数字后，状态栏显示待输入序列；序列完成或按 esc 后清空
- [x] 在 COMMAND 模式下，命令行至少占状态栏宽度的一半（§7.8）：
  - 宽度 160 时，显示匹配的命令；
  - 宽度 80 时，按省略顺序先去掉匹配的命令，再去掉连接地址，模式块仍然可见；
  - 输入的命令变长时，命令行随之变宽，右侧各块依次让出空间。
- [x] 窗口继续变窄时，各段按 §7.8 的顺序省略，session 块、当前 window、`C-p` 入口、待输入序列和模式块始终都在
- [x] 待输入序列在 3 列以内时（如 `SPC`、`g`、`5`），`C-p` 入口的位置与没有待输入时相同

## F0.6 which-key · 状态：passed（80898e4；e2e 6967c6a）

- **依赖**：F0.3、F0.4、F0.5
- **涉及**：`internal/ui`（whichkey）、`internal/app`

**开发**
- [x] 按键序列停在纯前缀节点上 400ms 后，弹出浮层。定时器用带序号的 `tea.Tick`，过期的 Tick 直接忽略
- [x] 浮层紧贴在状态栏上方，左对齐，多列排列；每一项显示为 `键 → Action 标题`，标题取自注册表
- [x] 按 esc 取消：关闭浮层，并清空待输入序列
- [x] keymap 的表清单去掉 `keys.whichkey`：which-key 浮层不是作用域（§6.4、§6.5）

**验收**
- [x] 按 `SPC` 并等待约 0.5 秒后出现浮层，内容与 §6.8 中以 `SPC` 开头的默认键一致
- [x] 按 `SPC` 后立即按下一个键，不出现浮层
- [x] 在浮层中按键，效果与不打开浮层时直接按键相同
- [x] 按 esc 后浮层关闭，状态栏的待输入序列清空

## F0.7 布局树与 pane 操作（键盘） · 状态：passed（7be424c；e2e 96cb31e）

- **依赖**：F0.2、F0.4
- **涉及**：`internal/app`（layout、workspace）

**开发**
- [x] `layout.go`：二叉分割树的分割、关闭、调整大小、按方向切焦点。全部写成纯函数，配单元测试
- [x] 按键：
  - `C-h/j/k/l`、`SPC h/j/k/l`：切换焦点
  - `SPC %` / `SPC "`：左右分割 / 上下分割
  - `SPC x`：关闭当前 pane
  - `SPC z`：缩放 / 还原
  - `SPC H/J/K/L`：调整大小
  - `SPC q`：显示编号，再按数字跳转
  - `SPC b`：折叠 / 展开侧栏
- [x] pane 编号 ⟨n⟩ 按树的遍历顺序计算，侧栏固定为 ⟨0⟩
- [x] 按方向切焦点有多个候选时，选最近获得过焦点的 pane，不再选重叠最长的（§5「按方向切焦点」）

**验收**
- [x] layout 的单元测试通过
- [x] tmux e2e 测试：
  - 分割后出现同类型的空 pane，并获得焦点，编号正确；
  - 按方向切焦点的结果与几何位置一致；到了边上再按，焦点不动；
  - 关闭 pane 后，兄弟 pane 占满空间；
  - 缩放后 pane 占满状态栏以上的整个区域（侧栏也被盖住），能还原；缩放状态下分割、关闭（包括 `:q` 关掉最后一个 tab）、按编号跳转，都会退出缩放，画面正常；
  - `SPC H/J/K/L` 每次移动 5%，支持次数前缀，比例停在 10%–90%；方向与 tmux 的 resize-pane 相同（§5）；
  - `SPC q` 按数字跳转正确；按非数字键只关闭编号，这个键不执行别的操作；
  - 折叠后显示 3 列宽的细栏，再按一次恢复
- [x] 按方向切焦点有多个候选时，回到最近用过的那个：把 data 上下分割，在下面那个 pane 里按 C-l 到 console，再按 C-h，回到下面那个；在上面那个 pane 里做同样的操作，回到上面那个

## F0.8 命中表与鼠标 · 状态：passed（f91026e；e2e 31fa267）

- **依赖**：F0.5、F0.6、F0.7
- **涉及**：`internal/ui`（hit）、`internal/app`（鼠标事件的分发）

**开发**
- [x] 鼠标事件经由命中表转成 Action（§7.4）；查找时倒序，后画的在上层
- [x] 点击 pane 使其获得焦点；双击 pane 标题切换缩放；拖动 pane 之间的边界调整大小；点击细栏展开侧栏
- [x] 键位提示即按钮：pane 标题右侧的提示、which-key 中的每一项、状态栏的 `C-p` 入口
- [x] 悬停高亮；识别双击（400ms 内点中同一个目标）；滚轮作用于指针下方的 pane；点击浮层外部关闭浮层
- [x] `SPC q` 显示编号时：点击某个 pane 就跳到它，点击其他地方只关闭编号（§5「按编号跳转」）

**验收**

在 tmux e2e 中注入 SGR 鼠标序列，检查以下各项：
- [x] 点击 pane 后获得焦点；双击标题会缩放，再双击还原
- [x] 拖动边界后比例改变：左右分割拖中间那 1 列间隔，上下分割拖上面那个 pane 的下边框（§7.4）；点击细栏后侧栏展开
- [x] 点击提示或 which-key 中的一项，都会触发对应的 Action
- [x] 悬停时样式有变化（用 `capture-pane -e` 检查颜色）
- [x] 滚轮作用于指针所在的 pane，每格滚动 3 行（用占位内容的滚动偏移验证）
- [x] 点击浮层外部后，浮层关闭
- [x] `SPC q` 显示编号时，点击某个 pane（包括侧栏），焦点移到这个 pane，编号关闭；点击状态栏，编号关闭，之后的按键照常生效，不会被吞掉

## F0.9 表格网格样式 · 状态：passed（b8186b8；e2e 96cb31e）

- **依赖**：F0.2。用户在验证 F0.2 后提出。可以在 F0.5 之后的任意时间插进来做。
- **涉及**：`internal/ui`（grid 的绘制，以后 M1 F1.3 的真实表格会直接复用这部分）、主题（新增 token `row_alt`）

**开发**
- [x] 按 tech-design §7.6「网格样式」一节绘制：
  - 列之间、行号列之后用 `│` 分隔；
  - 表头下方画横线，与竖线交叉处用 `┼`；
  - 偶数行使用 `row_alt` 斑马纹；
  - 当前行使用 `row` 底色，当前单元格使用 `cursor` 底色。
- [x] 绘制代码写成 grid 组件，数据由调用方传入。M0 先传占位用的假数据，M1 F1.3 再换成真实数据。

**验收**
- [x] golden 测试（160×45、80×24）通过。
- [x] 竖线、表头横线和 `┼` 的位置与列宽一致；斑马纹隔行出现；当前行和当前单元格的底色符合 §7.3。
- [x] 窄宽度下列宽被压缩后，竖线仍然与表头对齐，不越界。

## F0.10 字符宽度统一按字素簇 · 状态：passed（261fb17；e2e 816a0bc）

- **依赖**：F0.2。tester 测 F0.4 时发现，可以在任意时间插进来做。
- **涉及**：`internal/app`（启动）

**开发**
- [x] 启动时把 Bubble Tea 渲染器的宽度算法切换为字素簇，与 Frame 一致（§7.1「宽度」）。在代码注释里写明为什么这样切换，以及升级 Bubble Tea 时要检查什么
- [x] 一个单测，守住这个借用的行为：在终端不回报 2027 的情况下启动程序，检查渲染器已经切换到字素簇。例如用 `tea.WithOutput` 把输出接到缓冲区，检查里面有 `ansi.SetModeUnicodeCore`，渲染器切换宽度算法时会写出这个序列。Bubble Tea 升级后这条路径一旦变了，这个测试就会失败，不用靠人记着去查。e2e 替代不了它：哪天 tmux 支持了 2027，e2e 照样通过，但 Terminal.app 上已经坏了

**验收**
- [x] 在 tmux 中执行 `:x👍🏽`，出现 toast「未知命令: x👍🏽」时，这一行完整：toast 右侧的内边距和 pane 右下角的 `┘` 都在
- [x] 命令行里输入 `x👍🏽` 时，状态栏行尾的模式块完整显示为 `COMMAND`
- [x] 把 👍🏽 换成 ❤️、👨‍👩‍👧、1️⃣，结果相同
- [x] 已有的 golden 测试和 e2e 回归不受影响

---

以下 F0.11–F0.15 是用户体验 M0 之后提出的改进（2026-09-23），按编号顺序做。

## F0.11 精简默认键位 · 状态：passed（d6af259；e2e 264b62f）

- **依赖**：F0.10
- **涉及**：`internal/keymap`（default.toml）、`internal/app`（Action 注册表）

**开发**
- [x] `default.toml` 的 NORMAL 键位按 §6.8 精简：
  - 保留：`C-h/j/k/l`、`SPC s`、`SPC c`、`SPC %`、`SPC "`、`SPC z`、`SPC x`、`SPC q`、`SPC b`、`gt` / `gT`、`:`；
  - 新增：`SPC n` / `SPC p` / `SPC l` → `window.next` / `window.prev` / `window.last`，M5 之前什么都不做；
  - 去掉：`SPC 0-9`、`SPC h/j/k/l`、`SPC H/J/K/L`、`SPC ,`、`SPC &`，以及原来 `SPC n` 的新建连接。对应的 Action 都保留，F0.13 之后能在命令面板里执行，用户也可以在 `config.toml` 里自己绑定。
- [x] `window.select` 去掉标题：不再出现在 which-key 和命令面板里，只供点击状态栏上的 window 名时使用。

**验收**
- [x] 按 `SPC` 停半秒，which-key 只列出上面保留和新增的键。
- [x] `sqlmux keys` 的输出里没有 `SPC 0-9`、`SPC h/j/k/l`、`SPC H/J/K/L`、`SPC ,`、`SPC &`。
- [x] 在 `config.toml` 里写 `[keys.normal] "<Leader>h" = "pane.focus.left"` 后，`SPC h` 能切焦点，which-key 里也出现这一项。
- [x] 默认键位的兼容性单测照常通过。

## F0.12 主题文件：颜色、按类型配色、图标 · 状态：todo

- **依赖**：F0.9
- **涉及**：`internal/ui`（theme、grid、icons）、`internal/config`、`internal/app`

**开发**
- [ ] 主题文件（§7.3）：`config.toml` 的 `theme = "<名字>"` 先找 `$XDG_CONFIG_HOME/sqlmux/themes/<名字>.toml`，再找内置主题。文件里只写要改的 token，其余沿用 tokyonight-storm。token 名写错、颜色不是 `#rrggbb`、找不到这个主题时，启动报错，指出文件和出错的那一项。
- [ ] 新增 token：
  - `bar`：状态栏和 toast 的底色，从原来共用的 `row` 拆出来；
  - `string`、`time`、`bool`、`json`：默认都与 `fg` 相同。
- [ ] grid 按列类型着色（§7.6）：每列带一个类型分类（number / string / time / bool / json / 其他）。M0 的假数据给每列标上分类：id 是 number，biz_type、status 是 string，created_at 是 time；M1 再按数据库类型映射。
- [ ] console 图标改为 `nf-oct-terminal`（U+F489，带方框的终端图标）；ascii 下仍是 `>`。
- [ ] 主题文件的 `[icon]` 表（§7.7，写法参考 yazi）：`名字 = { text = "…", fg = "#…" }`。
  - `text` 换字形，`fg` 换颜色，两者都可以只写一个；没写的沿用 `icons`（nerd / ascii）那一套；
  - 写了 `fg` 的图标在任何位置都用这个颜色，没写时跟随所在位置的颜色；
  - 可以写的名字：`schema`、`table`、`data`、`console`、`filter`、`search`、`keys`、`conn`、`key`、`postgres`。

**验收**
- [ ] 不写 `theme` 时，除了 console 图标，画面与现在完全一样：160×45（nerd）的 golden 只有 console 标题里的图标这一个字符不同，80×24（ascii）的 golden 不变。拆出 `bar` 不应带来任何变化。
- [ ] 在临时的 `XDG_CONFIG_HOME` 里放一个主题文件，只写 `pane_bg`、`row`、`cursor`、`number`、`string`、`time`，`theme` 选它：
  - 这几项生效，其余 token 不变；
  - 状态栏的底色不受 `row` 影响。
- [ ] 主题文件里 token 名写错、颜色写错，或者 `theme` 指向不存在的名字：启动报错并指出是哪一项，退出码为 1。
- [ ] data 表格里 id、biz_type、created_at 三列分别用 `number`、`string`、`time` 的颜色。
- [ ] nerd 图标下，console 标题里的图标是 U+F489。
- [ ] 主题文件里写 `[icon] console = { text = "C", fg = "#ff0000" }`：console 标题的图标变成红色的 `C`。只写 `fg` 时字形不变、只改颜色。`icons = "ascii"` 时覆盖照样生效。
- [ ] `[icon]` 里写了不存在的名字，或者颜色写错：启动报错并指出是哪一项。

## F0.13 命令面板：框架与命令范围 · 状态：todo

- **依赖**：F0.11
- **涉及**：`internal/ui`（palette、遮罩、`match.go`、单行输入）、`internal/app`、`internal/keymap`

**开发**
- [ ] `match.go`（§9.7）：封装 fzf 的 `src/algo`，固定版本；smartcase；支持扩展语法的常用部分；附单元测试。原计划在 M1 F1.2，提前到这里。
- [ ] 单行输入组件，原计划在 M1 F1.4，提前到这里：
  - 退格和光标移动都按字素簇处理；
  - 显示光标；内容超出可用宽度时，保持光标所在的位置可见；
  - 命令面板用它，M1 的 WHERE 输入框复用。
- [ ] 命令面板（§12）：
  - `C-p` 打开；NORMAL 下按 `:` 打开并直接进入命令范围；esc 或 `C-c` 关闭；
  - 打开时背景变暗（§7.5）；
  - 居中的浮层：顶部是输入行，下面是候选列表，底栏说明按 ↵ 会做什么；
  - 候选是注册表里所有带标题的 Action。右列显示它的键位（通过 Hint 获取，可以点击）；开关类命令显示 ON / OFF；
  - 模糊匹配，匹配到的字符用 `warn` 底色高亮；输入为空时按最近使用排序，M0 只记在内存里；
  - ex 别名：`q` 是关闭 tab，`qa` 是退出，`w` 是保存。在命令范围里，输入与别名完全相同时，这条排第一，所以 `:q↵`、`:qa↵` 的用法不变；
  - 按键按 §6.8 的浮层键位表，这一步写进 `default.toml`：`↑` / `↓` 或 `C-n` / `C-p` 移动，`↵` 执行，esc 关闭；
  - 鼠标：悬停即选中，点击即执行，点击面板外面关闭；
  - 面板打开时，模式为 COMMAND。
- [ ] 去掉 F0.4 在状态栏里的命令行和「未知命令」toast，以及 F0.5 里 COMMAND 模式下的状态栏布局（命令行至少占一半宽度、匹配命令的附加信息）。

**验收**
- [ ] 按 `C-p` 打开面板，背景变暗；按 esc 关闭，画面恢复。
- [ ] 输入 `split`，能找到左右分割和上下分割，右列分别显示 `SPC %`、`SPC "`；按 ↵ 执行分割。
- [ ] 输入 `resize`，能找到调整大小的命令。这些命令默认没有绑键，右列为空；按 ↵ 能执行。
- [ ] `:` 打开时已经在命令范围；`:q↵` 关闭当前 tab，`:qa↵` 退出。
- [ ] 匹配到的字符高亮；smartcase：输入全小写时不区分大小写。
- [ ] 悬停时选中项跟着移动，点击执行，点击面板外面关闭。
- [ ] 输入 é（e+U+0301）或 👍🏽 后按一次退格，整个字删掉；光标可见；输入超出宽度后，正在输入的位置仍然可见。
- [ ] 状态栏里不再出现命令行；面板打开时，模式块显示 COMMAND。
- [ ] `match.go` 的单测覆盖：排序结果、高亮位置、扩展语法。

## F0.14 命令面板：表、pane、window 范围 · 状态：todo

- **依赖**：F0.13
- **涉及**：`internal/app`、`internal/ui`（palette）

**开发**
- [ ] 范围标签：全部 · 命令 · 表 · pane · window，`Tab` / `S-Tab` 循环。输入前缀直接限定范围：`>` 命令、`@` 表、`#` pane、`%` window（§12）。
- [ ] 表：M0 用侧栏的假表列表。`↵` 在当前 tab 打开，M0 里只是把 tab 名换成这张表；`C-t` 在新 tab 打开。
- [ ] pane：每项显示为 `⟨n⟩ <图标> 类型 · 对象名`，`↵` 聚焦。
- [ ] window：每项显示为 `序号: 名称`。M5 之前只列出，选中后不切换，和其他尚未实现的操作一样什么都不做。
- [ ] 「全部」范围里，每项的左侧用图标标出种类。

**验收**
- [ ] `Tab` / `S-Tab` 在范围之间循环；输入 `@ord` 只在表里找，`#con` 只在 pane 里找。
- [ ] 在表范围选中 `t_user`，按 ↵ 后当前 tab 变成 t_user；按 `C-t` 则新开一个 tab。
- [ ] 在 pane 范围选中 console，按 ↵ 后焦点移到 console。
- [ ] 「全部」范围里同时有命令、表、pane 和 window。

## F0.15 侧栏：拖动调宽、标题显示当前 schema · 状态：todo

- **依赖**：F0.8
- **涉及**：`internal/app`（layout）、`internal/ui`

**开发**
- [ ] 侧栏和右边 pane 之间的那 1 列间隔也是拖动柄（§7.4、§7.8）：
  - 拖动时实时改变侧栏宽度，最窄 16 列，最宽为窗口宽度的一半；
  - 宽度记在 window 上；
  - 侧栏折叠时不能拖。
- [ ] 侧栏标题改为 `⟨0⟩ <schema 图标> public ▾`（M0 的假 schema）。整段登记为按钮，点击执行 `tree.schema`：M1 F1.2 实现下拉框，M0 里什么都不做。

**验收**
- [ ] 拖动侧栏右边的间隔，侧栏宽度跟着变，停在 16 列和半宽；松开后不再变化。
- [ ] 拖过之后再分割、缩放、折叠后展开，侧栏宽度保持拖动后的值。
- [ ] 侧栏标题显示 `public ▾`；窗口变窄时按 §7.8 的规则截短。
- [ ] golden 测试随之更新并通过。
