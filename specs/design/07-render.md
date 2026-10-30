# sqlmux 技术方案 · §7 渲染与鼠标

> 这是技术方案的一章，各章的索引和阅读规则在 [`../tech-design.md`](../tech-design.md)；章节编号（§N.M）全仓库通用，不随文件改。

## 7. 渲染与鼠标

### 7.1 Frame

```go
type Frame struct {
    Buf    uv.ScreenBuffer       // Ultraviolet 的 cell 缓冲区（lipgloss.Canvas 只是在它外面包了一层，还不暴露 FillArea，所以直接用它）
    Hits   []Hit                 // 按绘制顺序追加；查找时倒序，后画的在上层
    Mouse  uv.Position           // 当前指针位置，用于悬停样式
    Theme  *Theme
    Cursor *uv.Position          // 输入框获得焦点时，终端光标的位置（§12）
}
```

- **键位提示**：Frame 不持有 keymap。键位文字由 app 通过 `keymap.Hint` 取好，再传给各个组件。
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
- 主题文件只需写要改的 token，没写的沿用 `tokyonight-storm`。颜色写成 `#rrggbb`。颜色写错、或者找不到这个主题时，启动报错并指出是哪一项；不认识的 token 名或 `[icon]` 名 M14 F14.7 起同 config.toml 的未知键：忽略它、启动后 toast「themes/<名>.toml: 不认识的键 …」，不阻止启动（原来也是启动错误，用户 2026-10-12 用旧版本读带新键的主题直接进不去）。
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

终端不支持真彩时，Bubble Tea 的渲染器会按终端能力（colorprofile）自动降级。

| token | 值 | 用途 |
|---|---|---|
| `bg` / `pane_bg` | #1f2335 / #24283b | 背景 |
| `fg` / `fg_muted` / `dim` | #c0caf5 / #a9b1d6 / #565f89 | 文字 |
| `border` | #3b4261 | 未聚焦的边框 |
| `focus` | #9ece6a | 焦点、NORMAL、执行 |
| `warn` | #e0af68 | 待提交修改、键位、INSERT |
| `zoom` | = `warn` | 缩放中的 pane 的边框和标题里的缩放图标（M9 F9.6；没写就跟 `warn`） |
| `match` | #ff9e64 | 模糊匹配到的字符：只改前景色，不加底色，行的底色照旧（M1 用户反馈：黄底太重，参考 fzf / telescope 的效果）。补全、命令面板、树的过滤、COLS、WHERE 历史都用它 |
| `cursor` / `cursor_blur` | #3d59a1 / #2f3549 | 单元格光标（pane 聚焦 / 失焦） |
| `select` / `row` | #364a82 / #292e42 | 选中项 / 当前行 |
| `visual` | #2d3f76 | query 的 VISUAL / V-LINE / V-BLOCK 选区，以及单元格编辑、输入框的「全选」（M2/M3 验收时从 `select` 拆出，用户觉得选区不明显）；表格里正在选的范围（§7.6「选择」） |
| `selected` | 黄色系（worker 定，和 `warn` 同色相、压暗） | 表格里已确认选中的记录的序号格（M12 F12.12）；主题没写时退回 `visual` |
| `search` | 橙色系底、`bg` 字（worker 定，照 nvim 默认配色的 `Search`） | 编辑器里搜索的其他匹配（M15 F15.9，§11「搜索」）；主题没写时退回 `visual` 底、命中文字保留原前景（深底深字看不清），退回规则同 `selected`；内置主题自带 |
| `search_current` | 同 `yank`（`warn` 底、`bg` 字，照 nvim 的 `CurSearch`） | 编辑器里搜索的当前匹配（M15 F15.9）；主题没写时退回 `yank` |
| `error_bg` | #3b2230 | 错误栏的底色（§7.8「错误栏」） |
| `row_alt` | #1f2335 | 表格斑马纹的偶数行（比 pane 底色深） |
| `edited_bg` | #2d2a24 | 已修改单元格的底色 |
| `keyword` | #bb9af7 | SQL 关键字、VISUAL |
| `sql_string` / `comment` | #9ece6a / 同 `dim` | query 里 SQL 的字符串字面量 / 注释（M3）。`string` 是表格里字符串值的颜色，默认不着色，所以另设一个 |
| `sql_table` / `sql_column` / `sql_operator` | #2ac3de / #73daca / #89ddff | query 和 WHERE 输入框里的表名 / 列名 / 运算符（F3.27）。按词法加 catalog 缓存判断，别名、CTE 名不着色。`sql_operator` 也用于标点 `;` `,` `(` `)`（M5 F5.14，用户验收时要求 `*`、`;` 这类符号同一个颜色）；`.` 不着色，`schema.table` 里的点着了色反而碎 |
| `yank` | #e0af68 底、`bg` 字 | 复制时闪一下被复制的范围，150ms，同 nvim 的 `vim.hl.on_yank`（F3.32） |
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
    Target Target   // {Kind, Pane, I, Action}。M0 的 Kind：pane、title、tab、hint、button、item、number、border、backdrop、row、treeedge；cell、rowno、chip、segment 等随后面的功能加入
}
```

**键位提示即按钮**：界面上画出的每一条键位提示都登记为命中区，点击就执行对应的 Action。覆盖的位置包括 pane 标题右侧、tab 栏右侧、浮层底栏、which-key 浮层、命令面板右列、状态栏。因此键盘不方便的操作，总能在界面上找到可以点的地方。

| 鼠标操作 | 实现 |
|---|---|
| 单击 | 查到目标后转成 Action。例如点击单元格，等于依次执行 `pane.focus` 和 `grid.goto r c`，与键盘走同一条路径 |
| 双击 | 400ms 内第二次点中同一目标，触发它的双击操作：单元格进入编辑，pane 标题切换缩放 |
| 悬停 | 绘制时用 `f.Region(rect, target)` 判断是否悬停并据此选择样式。命令面板里悬停即触发 `palette.select i`（K-03） |
| 滚轮 | 作用于指针所在的 pane，而不是焦点 pane。纵向每格滚动 3 行，与 nvim 的默认值（`mousescroll=ver:3`）一致，表格、树、query 都一样。表格和树最多滚到最后一行贴着底边，内容一屏放得下时滚轮不起作用；query 的滚动范围与 nvim 一致（M3）。模态浮层（命令面板、COLS、session 列表、表单、确认框、菜单、值编辑器这些）开着时滚轮不滚底下的 pane（M13 F13.7；有自己滚动的列表以后再让它滚自己）。横向滚动支持 Shift+滚轮，也支持触控板的横向滑动（终端上报为滚轮按钮 6/7） |
| 拖动 | 在 pane 边界上按下即开始拖动，实时修改对应节点的 `Ratio`。拖动柄：左右相邻的 pane 之间是那 1 列间隔；上下相邻的 pane 之间没有间隔，用上面那个 pane 的下边框这一行，因为下面那个 pane 的上边框放着标题和提示，点击、双击另有用途。在 query 里拖选文本会进入 VISUAL |
| 点击浮层外部 | 浮层先登记一个全屏的 `backdrop` 命中区，点中它就关闭浮层。正在编辑的单元格失焦时，保留修改 |

终端兼容：

- 悬停依赖终端支持全量移动上报（1003 模式）。点击、滚轮、拖动在主流终端上都能用。
- 在 tmux 里使用鼠标，需要 tmux 开启 `mouse` 选项。这是 tmux 自身的行为，只写进 README。

### 7.5 半透明遮罩

终端没有 alpha 通道。遮罩的做法是：画命令面板之前，遍历缓冲区里已经画好的所有 cell，把前景色和背景色都向 `bg` 混合 60%，然后再画面板。toast 画在面板之后，不被压暗：面板开着时也会出现 toast（如快速 SQL 的「查询已取消」），压暗后几乎看不出来。状态栏照旧被压暗。

### 7.6 表格

- **虚拟滚动**：只渲染视口内的行；横向按列偏移滚动。
- **列宽**：
  1. 期望宽度取表头宽度与本页最宽的值中的较大值，上限 40（M8 F8.5 起；原来是 p90，为的是少数长值不撑宽列，现在宽列一律按比例收，p90 反而把只有一行非空的时间列压到表头宽、显示成 `2026-09-0…`）。
  2. 总宽度超出时按比例收缩，但不小于表头宽度（G-06）。M8 F8.5 起（用户：`<null>`、`pending` 被缩成 `<nu…` 看不出是什么）：期望宽度不超过 `[grid] full_width`（默认 22，放得下 `2026-09-01 00:01:00+00`）的列不收缩，只在比它宽的列之间按比例收，收到 `full_width` 为止，还放不下就横向滚动。转置时不收缩，直接横向滚动（见下文「转置时列宽不按比例收缩」）。
  3. **手动列宽**（M17 F17.14，用户 2026-10-19 验收时提：一条记录差一点点看不全，终端拖不了列宽，DataGrip 是拖单列）：`>` / `<`（`grid.col.wider` / `grid.col.narrower`，带次数前缀，一次 1 列；用户 2026-10-19 定，原来是照 vim 窗口键的 `C-w >` / `C-w <`，表格里 `<` `>` 本来空着、按起来顺手）把光标所在列定为当前显示宽度 ± N，从此这一列按定宽画：不参与上面的比例收缩、不随本页内容变、不受 40 上限；最窄 1 列，最宽不限（放不下横向滚动）。`=`（`grid.col.fit`）清掉这个 tab 全部定宽、回到自动。定宽按列名记在 tab 上，和 `order_cols` 同一处存法（布局文件的 `col_widths`，§14），COLS 隐藏再恢复不丢；结果 tab 同样。**转置视图**（M17 F17.15，用户 2026-10-20 发版后提：竖置里也需要；2026-10-21 试过统一宽度后定：照 DataGrip，竖置拉的也是当前那一列）：`>` / `<` 只调**光标所在那条记录的列**（当前宽度 ± N），别的记录列不动；按本页的记录号记在 `col_widths` 旁的 `transpose_widths = {记录号 = 宽}`（§14，翻页后同一个号的位置沿用），`=` 清掉这个 tab 的 `col_widths` 和 `transpose_widths`；表头（记录号那一行）记录 N 右边的分隔符拖的是记录 N。左边的字段名列不调（已知上限）。决策者先定过「全部记录列统一宽度」，用户否了。**鼠标**：在表头行按住两列之间的分隔符左右拖，松开时左边那列定宽为拖到的宽度（同 pane 边界的拖动，§7.4；拖到 1 列为止），行号列和它右边的分隔符不能拖。chip 不变。
  3. 还放不下就横向滚动。
- **宽度计算**：按字素簇计算（§7.1「宽度」），CJK 字符和 emoji 都能正确处理。
- **单元格显示**：
  - 按列的类型分类着色（§7.3），分类按 catalog 的 `format_type`：
    - `number`（右对齐；转置视图里一律左对齐，因为一列里混着各种类型，M5 F5.7 用户要求）：smallint、integer、bigint、numeric(…)、real、double precision、oid；
    - `time`：date、time…、timestamp…（含 with time zone）、interval；
    - `bool`：boolean；
    - `json`：json、jsonb；
    - `string`：text、character varying(…)、character(…)、"char"、name、citext；
    - 其余用 `fg`：枚举（不算字符串类型）、uuid、数组、bytea 等。
  - NULL 显示为 `dim` 色的 `<null>`；超长内容用 `…` 截断；主键列的表头带钥匙图标。
  - 显示前清理控制字符：换行显示为 `dim` 色的 `↵`，Tab 显示为一个空格，其余 C0、DEL、C1 控制字符直接去掉，避免把终端控制序列画到屏幕上。替换之后再按字素簇截断。完整的值在单元格编辑（M2）里看。已知上限：数据里本来就有的 `↵` 字符也会画成 dim 色，不作区分。
  - 空串显示为空白。
  - **数据库报错**（权限不足、WHERE 写错等）：显示在 pane 底部的错误栏里（§7.8「错误栏」），表格照旧画着上一次的数据；第一次打开就出错时表格为空。出错之后，ORDER、LIMIT、PAGE 退回到画着的这份数据用的值，chip 上的操作（方向箭头、下拉框的当前项、`]` / `[`）都按它来；失败的 WHERE 留在输入框和请求里，改完再 `↵`，或直接 `R` 重试（M3 F3.20 reviewer 实测：原来 chip 显示 `id ↑`，点箭头发出去的却是失败的那列）。M3 验收前是画在内容区第一行、不画表格。
  - **取数中**：pane 里保留上一次的数据，不画「加载中」占位；忙碌提示和取消见 §8.3。
  - **点击单元格**：命中区 Kind 为 `cell`，点击等于依次执行 `pane.focus` 和 `grid.goto r c`（§7.4），M1 F1.3 就做。
  - 已修改的单元格用 `warn` 色文字、`edited_bg` 底色、点状下划线（SGR 4:4）。终端不支持点状下划线时，退化为普通下划线。
- **转置（G-05）只影响渲染**：`GridState` 始终保存数据坐标（游标 Row / Col 和视图偏移 Top / Left 都是），按键时把屏幕方向换算成数据方向，所以光标位置、视图和修改标记在两种视图之间自然保持。转置后：表头行是记录的行号（当前记录的行号用绿色），第一列是字段名（`func` 色，主键带钥匙图标），每一行按字段的类型着色；`j` / `k` 在字段间移动，`h` / `l` 在记录间移动，`0` / `$` 到第一条 / 最后一条记录，`gg` / `G` 到第一个 / 最后一个字段。
- **JSON 视图**（M14 F14.5，用户 2026-10-11：参考 DataGrip 的「查看方式」，只要 表 和 JSON 两种，不做树）：`gj`（`grid.view.json`「以 JSON 查看」；M15 F15.9 起，用户验收：原来是 `gJ`；表格里 `gj` 没绑，JSON 视图的编辑器不折行、vim 的 `gj` 就是 `j`，让给它）在表 tab 和结果 tab 上切换：内容区换成只读的编辑器（同值编辑器的只读规则，JSON 着色、vim 移动、`/` 搜索、`y` 复制），文字是当前页的 JSON 数组，每个对象同 `Y`（键按显示顺序、值按类型），不截断；查询条照旧，chip 行末多一段 `JSON`（让位顺序同 ORDER 那几个 chip）；作用域 `jsonview`（§6.4）只有 `esc` / `q` 回表格、`s` flash 式跳转（§6.10，M16 F16.8，用户验收时提）、`Y` 把整个视图的 JSON 文本复制到系统剪贴板（`json.yank`「复制整个 JSON」，当前页的那份数组，toast 同表格的 `Y`；只在 NORMAL 下，VISUAL 的 `Y` 仍是 vim 的按行复制选区，同 query 的 `s`（§6.10），F16.8 审查时定：选了一段再按 Y 不能悄悄丢掉选区；M16 F16.9，用户验收时提）（`keys.normal` 的 `esc` 绑了 `pane.error.close`，所以单设；搜索高亮亮着时第一下 `esc` 只清高亮，M15 F15.9，§11「搜索」）和 `gj`（再按一次也回表格）；翻页和刷新沿用表格的键（M15 F15.9，用户验收：表格的翻页改成了 `<` / `>`，JSON 视图得跟着：`[keys.grid]` / `[keys.result]` 里绑到 `grid.page.*` / `result.page.*` / `grid.refresh` 的键在 JSON 视图里照样生效，表格的别的键不进来；原来 `jsonview` 自己绑 `]` / `[` / `R` 到 `json.page.*` / `json.refresh`，这三个 Action 去掉）；光标行不同步；翻页、刷新、改 WHERE / ORDER 在 JSON 视图里照常，内容跟着换（结果 tab 上 `R` 重跑新建的结果 tab 沿用 JSON 视图，转置一并沿用，审查时定）；pane 底部的提示行换成 `]/[ 翻页 · esc 回表格`，键位从表格的作用域和 `jsonview` 读，同值编辑器的提示行；和转置互斥（开一个关另一个）；按 tab 记、不进布局文件；右键菜单和命令面板里都有。M16 Redis 的「以 JSON 查看」复用它。
- **选择（M10 F10.7，照 yazi）**：选择的单位是记录（竖直视图一行，转置视图一列），选中集按 tab 记。`m`（`grid.select.toggle`，标记）切换光标所在的记录，不进 VISUAL（yazi 用 `space`，这里 `space` 是 leader：单按要等 `timeoutlen`，`space j` 又是用户的 `SPC j`，所以换成 vim 标记的 `m`；worker 开工时提出）；`v`（`grid.select.visual`）进 VISUAL，锚点是当前记录，之后的移动（`j` / `k`，转置下 `h` / `l`，以及 `gg` / `G`、`0` / `$`）改变范围，范围内的记录和 `m` 标记的合并算选中，模式块显示 VISUAL；`esc` 或再按 `v` 退出 VISUAL，范围并入选中集。`V`（`grid.select.unset`，M12 F12.12，同 yazi 的 `visual_mode --unset`；原来和 `v` 一样）进 unset 变体：模式块显示 UNSET，移动同 VISUAL，`esc` 或再按 `V` 时范围内的记录从选中集里去掉，本来没选中的不受影响；VISUAL 里按 `V`、UNSET 里按 `v` 都只是结束当前模式、按当前模式结算，不换模式（审查时定，vim 式换模式保留锚点不做）；`d` / `y` 在 UNSET 里按去掉之后的选中集算。有选中或在 VISUAL 时 `select` 作用域生效（§6.4）：`d`（`grid.select.delete`）对选中的记录做 `dd` 的标记删除，全部已标记时全部取消，`C-s` 才真删；`y`（`grid.select.yank`）复制为 JSON 数组，按显示顺序，每条的格式同 `Y`，复制后序号格闪一下 `yank` 色；两者之后清空选中、退出 VISUAL；NORMAL 下有选中时 `esc`（`grid.select.clear`）清空。VISUAL 仍是 NORMAL 上下文，`normal` 层和用户映射照常（§6.4）。有选中或在 VISUAL 时自动刷新暂停，同有未保存修改时（刷新会清掉选中）。结果 tab 同样能选：`y` 照常，`d` 不做事、选中和 VISUAL 都保留（结果只读）；结果 tab 没有 chip 行，选中数不显示，靠序号格的高亮和模式块。高亮（M12 F12.12 起分两种，用户验收：一种灰色分不清哪些是定了的；同 yazi）：已确认选中的记录的序号格（竖直视图是左侧行号，转置视图是顶部记录号）用 `selected` 底（§7.7，黄色系），VISUAL / UNSET 里正在选的范围用 `visual` 底；chip 行多一段 `3 selected`（M12 F12.12 起文字用 `warn` 色，和 `selected` 底同色相，用户验收时定；原来同其他 chip 文字），独立于行数统计，让位顺序见 §7.8「第二行放不下时谁让位」。换页、刷新、改 WHERE / ORDER / LIMIT 取回新数据后清空选中，转置不清。不做鼠标选择（VISUAL 里单击一条记录只是光标移过去，范围跟着扩到那条），不做 `o` 换锚点。
- **按单元值过滤**（M12 F12.3，DataGrip 的「按单元值进行本地筛选」）：`*`（`grid.filter.cell`「按这个值过滤」）把光标所在格拼成条件追加进 WHERE 并重新取数：`col = <字面量>`，字面量按列类型：数字、布尔直接写；字符串、时间、uuid、jsonb 当字符串字面量（单引号、`'` 翻倍；MySQL 的反斜杠也翻倍，`NO_BACKSLASH_ESCAPES` 模式不管，`ponytail:` 标出）；bytea / blob、binary / varbinary、数组、PG 的 json 类型（没有 `=` 运算符）和 MySQL 的 json 列（和字符串字面量比较永远不相等）、xml、几何类型、real / float4 / float（文本不是精确值）、MySQL 的 BIT 不做，toast「这一列的类型不支持按值过滤」（worker 开工时定、审查时补齐）。比较用数据库自己的 `=`，和 DataGrip、手写条件一致，所以 MySQL 不分大小写的排序规则、PAD SPACE 的 CHAR、PG 的 interval 会多匹配几行，不另做精确比较；NULL 是 `col IS NULL`；PG 的 bool 写成 true / false，MySQL 的 tinyint(1) 当数字，numeric 的 NaN / Infinity 当字符串字面量，判断只看列类型不看值。WHERE 已有条件时用 ` AND ` 接在后面：已有条件里有顶层的 `or`（MySQL 的 `xor`、`||` 同样，AND 结合更紧；括号、引号、注释之外）就先把它括起来；末尾带 `-- 注释` 时新条件插在注释前。完全相同的条件已经在 WHERE 里就不重复加、不取数，toast「条件已在 WHERE 里」：比较时把已有条件按顶层 AND 拆段，去首尾空白、空白压成一个、关键字和标识符不分大小写、字符串字面量分。转置视图下 `*` 同样作用于光标所在格。进 WHERE 历史同手打的条件。结果 tab 没有 WHERE，不做。（细节 worker 开工时定）
- **表格的 `f` 组**（M12 F12.11，用户验收）：`fv` = `*`（按这个值过滤）、`fc` = `grid.col.hide`「隐藏这一列」（同 COLS 里取消勾选光标所在列，光标挪到最近的可见列，转置下作用于光标所在字段；结果 tab 没有 COLS、不做事，同 `fv`）、`fe` = `ge`（导出），和 `fh` / `fH` 一组；`O` 和 `e` 一样打开值编辑器。
- **按列排序**（M12 F12.3）：`gs`（`grid.order.cycle`「按这一列排序」）对光标所在列做表头单击的那一轮：只按这一列 → 翻转 → 默认，和表头单击同一个函数；`gS`（`grid.order.add`「加入排序」）同下拉框的 `space`。表头点击落在列名那一格，隐藏的列没有表头。**表头上的排序标记**（M12 F12.7，用户验收：只在 chip 和树里看得到不直观）：在排序列表里的列，列名后面画方向图标（`sort_asc` / `sort_desc`，`warn` 色，同 chip），多于一列时图标前加序号 `1` `2`；默认排序不画（不然每张表的 id 都带箭头）；列宽不够时列名先截断、图标仍在末尾；转置视图下不画。
- **Windows 的剪贴板**（M18 F18.3）：工具表在 Windows 上多一行，两条都是显式 UTF-8 的 PowerShell、直接走 argv 不经 cmd（reviewer 2026-10-24：`clip` 按控制台代码页读 stdin、PowerShell 默认按 OEM 代码页输出，中文都成乱码；`Get-Clipboard` 经 host 输出末尾多一个 CRLF，归一后 `PutClip` 会把复制的一个词当成整行贴）：写 `powershell -NoProfile -Command [Console]::InputEncoding=[Text.Encoding]::UTF8;Set-Clipboard -Value ([Console]::In.ReadToEnd())`，读 `powershell -NoProfile -Command [Console]::OutputEncoding=[Text.Encoding]::UTF8;[Console]::Out.Write((Get-Clipboard -Raw))`（读回来的 `\r\n` 归一成 `\n`，同 nvim 的 `:h clipboard-wsl`）；写本来就有 OSC 52 兜底，Windows Terminal 支持。
- **右键菜单**（M12 F12.3，同树的右键菜单 §7.8，作用域 `menu`，画法和键一样）：在格上右键，光标先移到那一格，菜单项：在值编辑器中打开（§10）、按这个值过滤、按这一列排序 ↑ / ↓、加入排序、隐藏这一列（`grid.col.hide`，M12 F12.11 起有键 `fc`，同 COLS 里取消勾选）、复制这一格、复制行为 JSON、导出…；表头上右键：按这一列排序 ↑ / ↓、加入排序、隐藏这一列。「排序 ↑ / ↓」是只按这一列、方向定死（替换整个列表，同下拉框在别的列上 `↵`）；「加入排序」= `gS`；「复制这一格」= 单元格的 `y`，「复制行为 JSON」= `Y`；「导出…」随 F12.4 加进来。结果 tab 的格只有值编辑器、复制这一格（F12.4 起加导出）。转置视图下的格同样能开。每项等于对应的 Action，只能从右键打开，菜单项不显示键位（审查时定，树的菜单也是）。每项左边一个图标（M16 F16.11，用户 2026-10-17；名字在 §7.7：值编辑器 `edit`、按值过滤 `filter`、排序 `sort_asc` / `sort_desc`、加入排序 `sort_asc`、隐藏列 `hide`、复制格 / 行 `copy`、以 JSON 查看 `json`、导出 `export`；树的菜单：打开 `open`、新 tab 打开 `open`、删表 / 删键 / 删 query `trash`、加列 `column`、加索引 `index`、改名 `rename`、删列 `hide`、删索引 `trash`、设 TTL `auto_refresh`、刷新 `refresh`），ascii 下不画图标。
- **导出**（M12 F12.4，用户要表上也有导出按钮，先 CSV 和 JSON）：查询条的导出按钮、`ge`、右键菜单的「导出…」都打开一个下拉（通用下拉框）：`CSV · 当前页`、`CSV · 全部`、`JSON · 当前页`、`JSON · 全部`。当前页就是内存里这一页；全部是同一条取数语句不带 LIMIT / OFFSET 再跑一遍（WHERE、ORDER 照旧；`select *` 读全部列、写的时候只取显示的列、按显示顺序，隐藏列多读了不写，`ponytail:` 标上限），从驱动流式读（db 层的 `Streamer`：PG 走 pgconn 的 ResultReader、MySQL 走 `database/sql` 的 Rows，没实现的假库退回 Query 逐行）、边读边写文件，不进表格也不占内存；连接同取数（表 tab 用 Meta，手动事务或事务中用 Main；结果 tab 的「全部」在 Main 上、复用那次运行设 schema 的那段），导出期间这条连接忙、别的取数排队、状态栏照旧显示忙碌，同一个 tab 导出还在跑时再开 toast「正在导出」。`C-c` 取消：关文件、删掉半截文件、toast「导出已取消」；中途出错同样删文件、toast 错误原文。当前页不含新增行和未保存的修改，「全部」导出的是数据库里的；当前页里有二进制占位符（§8.1 非 UTF-8 的值内存里只留 `<binary N bytes>`）时，按这一页的 LIMIT / OFFSET 走同一条流式读重读这一页再写，格子不变（审查时定）。结果 tab 的「全部」和翻页一样会把这条 SELECT 再跑一遍，有副作用的（`select nextval('s')`）会再推进一次；写语句都不重跑。文件写进程当前目录 `<表名>.csv` / `.json`（不带 schema；结果 tab 是 `query_1-42.csv`，同 §11；键 tab 用键名），名字里 Windows 不允许的字符（`<` `>` `:` `"` `/` `\` `|` `?` `*` 和控制字符）在所有平台都换成 `_`（M18 F18.2 e：Redis 的键名常带 `:`，Windows 上冒号后面是 ADS 的流名、macOS 的 Finder 把 `:` 显示成 `/`；一条规则不分平台，win-worker 2026-10-24 问），已存在就加 `-2`、`-3`（`O_EXCL` 创建，不覆盖），权限按 umask 的普通文件（导出本来就是要给别人的）；完成 toast `已导出 <绝对路径> · N 行`。下拉的锚点同自动刷新的下拉；结果 tab 的锚在标题按钮处。CSV 规则同 §11（表头一行，NULL 为空）；JSON 是对象数组，每个对象同 `Y`（键按显示顺序、值按列类型、2 空格缩进），空结果是 `[]`。二进制值（MySQL 的 binary / varbinary / blob 族，或非 UTF-8 的值）在 CSV 和 JSON 里都写 `0x` 加十六进制，和 PG bytea 的 `\x` 十六进制对应；格子里的 `<binary N bytes>` 是占位不是数据，不写进文件。不做剪贴板（OSC 52 装不下），不做 TSV、INSERT、XML。结果 tab 的「全部」：能翻页的语句（§11：我们自动加了 LIMIT 的）去掉自动 LIMIT 重跑；其余（自己写了 LIMIT / OFFSET 的、快速 SQL 的、跟在 SET / RESET / DISCARD / USE 之后的）「全部」就是当前页，规则和翻页一样；下拉里仍列出这一项，不灰不藏，免得出现第二种下拉。（细节 worker 开工时定）
- **转置时列宽不按比例收缩**：一页 100 条记录横排时，收缩下去每列只剩行号那么宽，什么都看不见；所以转置视图各列取期望宽度，放不下就横向滚动。
- **悬停行**（G-06）：指针所在的行用 `row` 底，不移动光标，与树一致。**点击行号**（G-04）：光标移到那一行，列不变，等于 `grid.goto r <当前列>`；命中区 Kind 为 `rowno`。
- **视图跟随光标**：光标移出视图时视图跟着滚，保证光标所在的列完整可见；这个计算在 `ui.Grid` 里，app 每次移动后调用并把结果存下来，这样上移时视图不会跳回顶部（与 vim 相同）。滚轮只滚视图：纵向每格 3 行，最多滚到最后一行贴底；Shift + 滚轮和横向滚轮每格 1 列；两种滚动都把光标夹回视图内（与树一致）。
- **数据结构**：`Pane.Tabs` 是 `[]Tab`，`Tab{Name; Data *dataTab}`，M3 再加 `Query *queryTab`，不用接口。`ui.Grid` 的行直接用 `[][]db.Val`，`ui` 只为这个类型 import `db`，`db` 不 import `ui`，不成环；这样每帧不用拷贝整页数据。
- **网格样式**：按用户要求，行和列都要有清晰的分隔（参考 DataGrip 的表格）。
  - **列**：
    - 相邻两列之间用 `│`（`sep` 色）分隔；
    - 行号列和第一列数据之间也用 `│` 分隔；
    - 每个单元格左右各留 1 列空白。
  - **表头**：
    - 表头是命中目标（`head`，M12 F12.3）：单击按这一列排序，再点翻转，第三次回到默认（只按行标识列）；`S-click`（SGR 的 shift 位）把这一列在排序列表里循环，同下拉框的 `space`；右键打开表头的菜单（下面「右键菜单」）。转置视图下表头是记录号，不响应。
    - 表头下面画一条横线，与竖线交叉的位置用 `┼`；
    - 列名用 `func` 色，主键列带钥匙图标；
    - 横向滚动时，表头始终固定在最上方。
  - **行**：
    - 用斑马纹区分：偶数行使用 `row_alt` 底色，比 pane 底色更深一些；
    - 当前行使用 `row` 底色（比 pane 底色更亮），当前单元格使用 `cursor` 底色；
    - 行与行之间不画横线，因为终端里每条横线都要占掉一整行，一屏能显示的行数会减半。
- 转置视图用同样的样式：竖线分隔的是各条记录，横线画在第一行（行号行）的下方。

### 7.7 图标

默认使用 Nerd Font 字形。设置 `icons = "ascii"` 后改用 ASCII 替代字符，用于没有安装 Nerd Font 的环境。query 用带方框的终端图标 `nf-oct-terminal`（U+F489）。

**有图标就不再配文字**（M0 用户反馈）：Nerd Font 图标本身就能说明含义，所以图标旁边不再重复写说明文字，包括 pane 标题里的类型名（`data`、`query`）、状态栏里命令面板入口的 `C-p`、侧栏过滤行的 `/`。ASCII 字符表达不了含义，所以 `icons = "ascii"` 时这些文字照常显示。命令面板里 pane 那一行的名称仍然带类型名，因为那是用来搜索的文字。

主题文件里可以逐个覆盖图标（M0 用户反馈，写法参考 yazi 的 `theme.toml`）：

```toml
[icon]
query = { text = "\uf489", fg = "#78dce8" }   # 换字形，也换颜色
table   = { fg = "#a9dc76" }                     # 只换颜色
```

- `text` 换字形，`fg` 换颜色，两者都可以只写一个。没写的沿用 `icons` 选的那一套（nerd / ascii），`icons = "ascii"` 时覆盖照样生效。
- 写了 `fg` 的图标在任何位置都用这个颜色；没写时跟随所在位置的颜色，比如标题聚焦时是 `focus` 色。
- **图标后面留 1 个空格再接文字**（M1 用户反馈）：树、面板的搜索行与候选、pane 标题、侧栏标题、状态栏、查询条都一样。Nerd Font 图标按 1 列计算（§7.1），但不少终端和字体把它画得比 1 列宽，溢出到后一格，紧挨着文字时看起来贴在一起，悬停底色也只盖住半个。留一格空白正好接住溢出的部分。
- 默认颜色：`save`、`refresh`、`transpose` 用 `info` 色，`sort_asc` / `sort_desc` 和 WHERE 历史下拉里的 `star` 用 `warn` 色（收藏和历史只靠图标区分，黄星一眼能认出）；都可以在 `[icon]` 里用 `fg` 覆盖。
- 可以覆盖的图标：`schema`、`table`、`data`、`query`、`filter`、`search`、`keys`、`conn`、`key`、`postgres`、`redis` 和 Redis 键的类型 `str` / `hash` / `list` / `set` / `zset` / `stream` / `json`（M16 F16.2；颜色依次用现有的 `sql_string` / `keyword` / `number` / `func` / `warn` / `info` / `sql_operator`，不加主题键），`pane`（M10 F10.5 起树里不再画 pane 节点、没有地方用它，键仍然接受，写了它的主题不报错），以及命令面板用的 `command`（nf-fa-bolt，U+F0E7，ascii 为 `:`）和 `window`（nf-fa-window_restore，U+F2D2，ascii 为 `[]`）、缩放中 pane 的 `zoom`（nf-fa-expand，U+F065，ascii 为 `[Z]`，M9 F9.6）；pane 按钮的 `split_right`（nf-cod-split_horizontal，U+EB56，ascii `|`）、`split_below`（nf-cod-split_vertical，U+EB57，ascii `=`）、`unzoom`（nf-fa-compress，U+F066，ascii `[z]`），缩放和关闭按钮用 `zoom`、`close`（M19 F19.3；两个分割图标 worker 在终端里对照字形确认方向，不对就换成同一套 codicon 里方向对的那个）；查询条的 `save`（U+F0C7，ascii `[S]`）、`refresh`（U+F021，ascii `[R]`）、`transpose`（U+F0EC，ascii `[T]`）、`sort_asc`（nf-fa-sort_amount_asc，U+F160，ascii `↑`）、`sort_desc`（nf-fa-sort_amount_desc，U+F161，ascii `↓`）；树的 `view`（nf-fa-eye，U+F06E，ascii `v`）、`column`（nf-cod-symbol_field，U+EB5F，ascii `-`）、只读连接的 `lock`（nf-fa-lock，U+F023，ascii `RO`，M5 F5.3）；查询条新增的 `row_add`（nf-fa-plus，U+F067，ascii `+`）、`row_delete`（nf-fa-minus，U+F068，ascii `-`）、`auto_refresh`（nf-fa-clock_o，U+F017，ascii `@`）、`stop`（nf-fa-stop，U+F04D，ascii `#`）；结果区的 `result`（nf-fa-list_alt，U+F022，ascii `=`）、`pin`（nf-oct-pin，U+F435，ascii `*`）、`export`（nf-fa-download，U+F019，ascii `>`）、`close`（nf-fa-times，U+F00D，ascii `x`）、日志 tab 的 `log`（nf-fa-file_text_o，U+F0F6，ascii `L`）；WHERE 历史下拉的 `star`（nf-fa-star，U+F005，ascii `*`）、`history`（nf-fa-history，U+F1DA，ascii `h`）；树的工作区里 pane 节点的 `pane`（nf-fa-columns，U+F0DB，ascii `|`，M5 F5.15）；新建连接表单的 `tag`（U+F02B，ascii `#`）、`host`（U+F233，ascii `@`）、`port`（U+F1E6，ascii `:`）、`user`（U+F007，ascii `~`）、`database`（U+F1C0，ascii `=`）、`advanced`（U+F013，ascii `+`）、`link`（U+F0C1，ascii `&`）（M7 F7.4；ascii 都是一个字符，不和 key / pin / star 的 `*` 撞）；分组字段的 `group`（nf-fa-tags，U+F02C，ascii `%`，M10 F10.6；不用 `#`，和上一行 name 的 `tag` 撞）。M3 起去掉 `data`：表 tab 在 pane 标题、tab 栏、树的工作区、面板的 pane 行里一律用 `table` 图标，query tab 用 `query`，引导 tab 不画图标。`mysql`（nf-dev-mysql，U+E704，ascii `my`，M5 F5.4）。补全候选的种类图标（M16 F16.11，用户 2026-10-17：照 nvim-cmp 的 lspkind）：`keyword`（U+F030B，nf-md 的钥匙，ascii `k`，色 `keyword`）、`value`（U+F03A0，nf-md-numeric，ascii `v`，色 `string`；`now()` / `current_timestamp` 也算值，和枚举值、true / false 一组）、`snippet`（nf-cod-symbol_snippet，U+EB66，ascii `s`，色 `info`；keyword 和 value 的字形照用户 nvim 里 blink.cmp 的 `kind_icons`，和他编辑器里看到的一致，用户 2026-10-17 定），表 / 视图 / 列 / Redis 键 / 命令沿用 `table` / `view` / `column` / `key` / `command`（色同树里的），Redis 的 field 用 `column`、子命令和 token 用 `keyword`；没有函数和 schema 候选，所以不定义它们的图标（F16.11 审查时定，有候选时再加）；右键菜单项的图标（同一个 feature）：`open`（nf-fa-folder_open_o，U+F115，ascii `o`）、`edit`（nf-fa-pencil，U+F040，ascii `e`）、`copy`（nf-fa-clipboard，U+F0EA，ascii `y`）、`trash`（nf-fa-trash，U+F1F8，ascii `x`）、`rename`（nf-fa-i_cursor，U+F246，ascii `r`）、`hide`（nf-fa-eye_slash，U+F070，ascii `-`）、`index`（nf-fa-list_ol，U+F0CB，ascii `i`），其余项用已有的 `filter` / `sort_asc` / `sort_desc` / `column` / `export` / `json`（Redis 键类型的那个，同一个 JSON 图标，不另起名：v0.5.0 的主题已经在用 `json`，改名会让用户主题里的键悄悄指到别处，F16.11 审查时定） / `auto_refresh`（设 TTL） / `row_add`；菜单项的图标 `dim` 色、选中行跟文字色。以后新增的图标也按名字加入。名字写错时启动报错。

### 7.8 默认尺寸与样式（取自设计稿）

设计稿按 13px 等宽字体绘制，一列约 7.8px。下面的数值由此折算成终端的行和列；设计稿里没有标注的，由实现自行取整。

- **整体布局**：
  - 最外层不留边距。
  - 横向相邻的 pane 之间留 1 列空白，纵向相邻的不留。
  - data 与 query 的宽度比为 1 : 1（M8 F8.9 起；设计稿是 5 : 4）。
- **schema 侧栏**：
  - 宽度默认 32 列（含边框；设计稿为 250px），config.toml 的 `[tree] width` 可改（M8 F8.22，用户要 300px 左右，即 38 列；范围 16 到窗口宽度的一半，超出只夹、不报错，0 和负数也夹到 16；布局文件里记的 `sidebar_width` 优先，拖过的宽度照旧记在 window 上）。窗口宽度小于 100 列时，缩到 24 列。
  - 可以用鼠标拖动侧栏和右边 pane 之间的那 1 列间隔来调宽度，最窄 16 列，最宽为窗口宽度的一半；拖过的宽度记在 window 上。折叠时不能拖。
  - 标题为 `⟨0⟩ <conn 图标> <session 名>`，例如 `⓪  seed`，右侧是收起图标 `«`（`dim` 色，可点击：收起侧栏，焦点在树上时回到进树之前的 pane，M19 F19.5 起不再等于 `tree.toggle`；M10 F10.9 起，原来是键位文字 `SPC b`，用户觉得难看）；放不下时先去掉图标，再截短 session 名。点击标题的其余部分只聚焦树。schema 不再放在标题上、也不再用下拉框切换，改成树里的节点（M1 用户反馈：用下拉框切 schema，看不出还有别的 schema；参考 sqmeow.nvim 的层级树）。
  - 折叠后是 3 列宽的细栏（设计稿为 24px），左右各 1 列边框、中间 1 列内容：只在顶部显示 `»`（可点击：展开并聚焦树，M19 F19.5），下面不画任何文字（M10 F10.9 起，原来竖排 `schema · SPC b`，用户：不好看，能点、能映射就够了）。
  - **`tree.toggle`**（默认 `SPC b`，用户映射成 `Cmd+B`；M19 F19.5 起照 DataGrip 的工具窗口，用户 2026-10-30：原来只开关侧栏、不动焦点，不直觉）：侧栏开着、焦点在树上 → 收起，焦点回到进树之前的那个 pane（它已经关了就回这个 window 的当前 pane）；侧栏开着、焦点不在树上 → 只把焦点移到树，不收起；侧栏收起 → 展开并聚焦树。有 pane 在缩放时侧栏被遮住，按它先退出缩放，再照上面三种情况走（这时焦点在 pane 上，侧栏开着就是聚焦树，收着就是展开并聚焦树；tester 2026-10-30 指出原来「按收起那一条处理」的措辞不对）。「进树之前的 pane」是焦点从某个 pane 进到树时记下的那一个，不论是按键、点击还是 `tree.toggle` 进来的。命令面板和右键里的标题改成「打开 / 聚焦 / 收起侧栏」，状态栏轮播提示的那条跟着改。
  - 侧栏内部从上到下依次是：
    - 第一行：过滤图标（`info` 色）、`160 tables`（`dim` 色，已连上的各个连接所有 schema 的表和视图总数）；ascii 图标下，图标后面还有 `/`（`fg` 色）（§7.7）。按 `/` 后这一行变成输入框（INSERT），照 nvim-tree 的 live filter：边输入边过滤，只匹配表和视图的名字，匹配项的上级节点全部展开显示，其余节点隐藏（包括工作区整块，以及已展开的表下面的列），顺序不变，匹配到的字符高亮；清空过滤后回到原来的展开状态；`↵` 退出输入框、保留过滤、光标放到第一个匹配上；`esc`（及等同 esc 的 `C-c`）清空过滤并退出。有过滤条件时这一行显示 `<图标> <过滤文字>`，右侧 dim 色显示 `3/14`；
    - 一条分隔线（`sep` 色）；
    - 层级树（M1 F1.12）：

      ```
      ▾ <postgres> play
        ▾ <schema> public
          ▾ <table> Tables (8)
            ▸ <table> t_order                6.0k
            ▾ <table> t_user                   50
                <key> id                     int8
                <column> name                text
          ▸ <view> Views (1)
        ▸ <schema> agentable
      ▸ <postgres> seed <lock>
      ▾ <window> 工作区
        ▾ <window> data
          ▾ ① <data>
              <table> t_order  status = 'done'
              <table> t_order
      ```

      - 每层缩进 2 列。有子节点的节点前面是 `▸`（折叠）/ `▾`（展开），`dim` 色；叶子节点在同一位置留空。
      - **连接节点**（M5 F5.10 起，原来是 session 节点）：connections.toml 里的每个连接一个，顺序见下面「分组」；引擎图标加连接名，只读连接（F5.3）名字后面加 `lock` 图标。四种状态：未连接时折叠、没有子节点；连接中名字后面跟 `dim` 色的 `…`；失败时名字用 `error` 色，错误进 toast（`<连接名>：<驱动的错误>`）；已连接时下面是 schema。在未连接或失败的节点上展开（`l`、`↵`、单击、`R`）就去连，失败的就是重试，连接中再展开不做事；两个连接上开着同名表时，tab 名和 pane 标题照旧只写表名，焦点所在的连接看状态栏的地址块（已知上限，要区分时再给 tab 加 `[play]` 这样的后缀）；`password_cmd` 也在 Cmd 里跑；连上后加载 catalog，并照「默认展开」展开它。
      - **数据库节点**（M15 F15.1，只有 PG）：连接节点下先是数据库一层（图标 `database`，DSN 里的库排第一、默认展开，其余按名字），schema 挂在数据库下；别的库点开或 `l` 时才拨号，连不上同连接节点的失败状态；节点 id 多一段 `/db:<name>`；`R` 刷新的是光标所在库；右键菜单、表 tab 的标题（`db.schema.table` 只在不是默认库时带库名）、state.json 的表键、query 标题的 `DB` 字段、状态栏的地址都带库名。query 切库：不另开下拉，`query.schema` 的下拉（标题 `db.schema ▾`）第一级列库、第二级列 schema（worker 起草时提出、决策者同意）；库只有一个（或列表为空、读失败）时没有第一级、直接列 schema，和 MySQL / SQLite 一致，标题照旧（审查时定）。表 tab 标题、右键菜单、删表确认框里非默认库都带库名（`db.schema.table`），免得在别的库里看着像默认库的那张。树过滤只搜表和视图，库和 schema 不参与，同以前。MySQL、SQLite 没有这一层。**exec 连接**（M17 F17.12，用户 2026-10-18 验收：层级要和别的 PG 连接一样）：数据库一层只有一个节点，名字是连接时 `select current_database()` 的结果，它就是默认库、走连接本身的 Conn，不另拨号；命令能到别的库时是另一条连接（§8.8）。
      - **库 / schema 的勾选**（M17 F17.13，用户 2026-10-18 验收时提：同 DataGrip 数据源的「架构」勾选，在连接和库上选显示和加载哪些）：连接节点（PG）的右键菜单和命令面板里「选择库…」，数据库节点（PG）和连接节点（MySQL）的「选择 schema…」，打开一个 COLS 式的列表浮层（同一个浮层、同 `[keys.cols]` 的键：`j` / `k`、`space` 勾选、`a` / `A`、`/` 过滤、`esc` 关闭；标题「库」/「schema」，每行 `[x] 名字`，schema 行的尾注是已加载的表数）。关闭时和打开时不同才生效：写回 connections.toml 的 `databases` / `schemas`（§14）并重载这个库的 catalog（同 `R`）。没勾的库不在树上列、不拨号；没勾的 schema 不在树上列、**catalog 不加载**（Tables / Views / Functions / Sequences 的查询都只查勾了的，§8.3），query 的 schema 下拉、补全、面板的 `@` 范围都只认加载的，手写带前缀的 SQL 照常能跑。两个字段都没写 = 全部（和以前一样）；DSN 里的库（exec 是 `current_database()` 那个）固定勾选、不能取消；schema 浮层里同样固定一个：MySQL 是 DSN 里的库，PG 是 `Schemas` 返回的默认 schema（不带前缀的表名落到的那个，search_path 里没有就是 `public`）——两个浮层都不会关在 0 个上，列表为空就不会写出来（tester 2026-10-19 问的，定的是和库浮层一致）。节点名后面的尾注：勾了的 / 总数（`2/7`，`dim`），只在收窄了时显示；侧栏放不下就整个不画，同 Redis 的尾注。Redis 的 db、SQLite 不做；系统 schema（`pg_catalog`、`information_schema`、`pg_toast`）不列入、不能勾（已知上限，要看系统表去 query 写）。
      - **Redis 的树**（M16 F16.2）：连接 → db（只列 `INFO keyspace` 里非空的，加 DSN 指定的那个，按号从小到大排、DSN 的那个默认展开（M16 F16.10，用户验收时提：原来 DSN 的排最前面，`db2, db0, db1, db3`）；节点名 `db0`，尾注 `DBSIZE`；每个 db 是一个子连接，同 PG 的数据库节点）→ 命名空间（按分隔符切键名，默认 `:`，`[redis] delimiter` 可改、连接里 `delimiter` 覆盖；尾注是它下面扫到的键数）→ 键（前面是类型图标，§7.7；不按类型分文件夹，用户 2026-10-11 定：真实项目的键按前缀组织，类型做标记就够）。扫描预算（`[redis] scan_keys`，默认 10000）用完时，这个 db 的键列表末尾多一行 `dim` 的 `已扫描 10000 / 12k，用 / 过滤`（db 节点的尾注仍是 `DBSIZE`：侧栏 32 列，长尾注放不下会整个不画，F16.2 时 worker 定）；侧栏的 `/` 过滤在 Redis 连接上改成服务端 `SCAN MATCH <模式>`、同一预算，模式不含 `*` 时两头补 `*`。过滤的 SCAN 用 `COUNT 10000`（MATCH 由服务端做，COUNT 只定往返次数；展开 db 的扫描仍是 1000，边扫边有键可画）；扫描在途时连接和 db 节点照画不隐藏，db 节点的尾注换成 `dim` 的「搜索中…」，扫完换回 `DBSIZE`、什么都没找到才按空处理；过滤文字一变就取消在途的扫描（ctx），不排队（M18 F18.11，用户 2026-10-26 验收时提：6 万键、往返 57ms 的库一次过滤要 7–14 秒，期间侧栏全空、计数 `0/61072`，以为搜不到）。右键：db 节点「刷新」；命名空间节点「删除 N 个键…」（确认后分批 `UNLINK`，走 `confirm_dangerous`，§13）；键节点「打开」「删除…」（确认「删除键 <名>？」，`dd` 同样走确认，§13）「改名…」（`RENAME`，目标存在时再确认覆盖）「设 TTL…」。不做 RedisInsight 的内存占比（要全量扫）。SCAN 和写回的规则在 §8.7。
      - **分组**（M10 F10.6，用户要按项目或环境分、不要文件夹多一层）：connections.toml 里可选的 `group = "prod"`（§14）。根的顺序：没分组的连接按文件顺序排最前面、不画标题；然后每个组一段，组按文件里第一次出现的顺序，组内按文件顺序，段前一行分割线 `── prod ──────`（`sep` 色的线、`dim` 色的组名，宽到侧栏右边）；最后一个连接和 Workspace 之间一条没字的分割线。分割线不是节点：`j` / `k` / `gg` / `G` 跳过，不能点，过滤（`/`）时不画。组的顺序只能改文件。连接管理的列表画同样的分割线，最后一个连接和「＋ 新建」之间也是一条没字的。一个连接都没分组时一条分割线都不画（树和连接管理都是，含 Workspace 前和「＋ 新建」前那两条）：没用分组的界面和 M9 一样。
      - **schema 节点**：非系统 schema，按名字排。
      - **分组节点**：`Tables (n)`、`Views (n)`，n 为这个 schema 下的个数；n 为 0 时照样显示，没有 `▸`。Tables 含普通表、分区表的父表、外部表；Views 含视图和物化视图。函数、序列、角色等不列：目前对它们没有可做的操作，有用处时再加。
      - **表 / 视图节点**：图标（`func` 色）+ 名字 + 右对齐的行数量级（`border` 色）；普通视图没有行数，不显示。
      - **列节点**：展开表的时候才从 `Meta` 取列，与打开表共用列缓存（§8.4），取回之前不显示子节点，也不显示「加载中」。每行是图标 + 列名 + 右侧 `dim` 色的类型（`format_type`）；主键列用 `key` 图标（`pk` 色），其余用 `column` 图标。
      - **右键菜单**（M8 F8.23，用户要在树里删 query 和表）：在树的节点上右键（SGR 按钮 2）弹出一个小菜单，左边贴指针的列、框从指针的下一行开始（放不下就翻到指针的上一行结束，被点的节点始终看得见）、`select` 底高亮、可点击，菜单开着时再右键先关掉再按新位置处理，作用域 `menu`（`j` / `k` / `↑` / `↓` 移动，`↵` 执行，`esc` 或点别处关闭），键位文字从 keymap 读。菜单项按节点：连接（PG）→ 选择库…、连接（MySQL）/ 数据库（PG）→ 选择 schema…（M17 F17.13，上面「库 / schema 的勾选」；Redis 的 db 节点照旧只有「刷新」）；连接（每种引擎）另有「编辑…」（打开连接管理并选中它）和「删除…」（同连接管理的删除，F18.12 的确认和关 tab；M18 F18.14，用户 2026-10-26 验收时提：树里连接右键删不了、只能走管理连接）；exec 连接不在连接管理里（F17.7），只有「删除…」；命令行开的临时连接不在 connections.toml 里，两项都没有、`dd` 无事（reviewer 2026-10-26 问）；`tree.delete`（默认 `dd`，不加 `D`：用户 2026-10-26 定）在连接节点上也是这个删除，在 query 文件、表 / 视图、Redis 键 / 命名空间上照旧；query 文件 → 打开、重命名…、删除…；表 / 视图 → 打开、在新 tab 打开、删除表…（视图是「删除视图…」）、**结构修改的模板**（M14 F14.4，命令面板里同样有）：「加列…」「加索引…」「改名…」「删除列…」「删除索引…」，每个把一条按引擎拼好的模板 SQL 开进当前连接的新 query（PG `alter table "s"."t" add column <name> <type>`，MySQL 反引号；SQLite 不支持的不列），占位符是字面的 `<name>` / `<type>` / `<column>` / `<index>` / `<new_name>`，是 snippet 的 tabstop（M15 F15.8，§11「snippet」：第一个在 Select 模式下选中、打字即替换，`Tab` / `S-Tab` 跳到下一个 / 上一个；原来只 VISUAL 选中第一个、要按 `c` 才能替换，用户验收：要和 nvim 的 snippet 一样；`vim = false` 时同样选中、打字即替换、`Tab` 跳），光标所在的列 / 索引直接写进模板；命令面板里「改名…」写成「改名表…」/ `Rename table…`，和 query、session 的重命名区分，右键菜单保持「改名…」；删除类直接写、不写成注释，`C-e` 走危险确认；不做表单；其他节点不弹。「删除表…」走危险操作的确认框（§13，同没带 WHERE 的 delete：红色标题，正文是要执行的 `DROP TABLE "public"."t_log"`，`y` / `↵` 确认），在那个连接的 `Main` 上执行，成功后关掉这张表的全部 tab（所有 session，未保存的修改随 tab 丢弃，不另提示：表都删了存不进去）、重载 catalog、toast；`confirm_dangerous = false` 也照样问（删表没有不问的理由）。手动事务模式、或自动模式下用户自己 `begin` 了的事务里（`c.Manual || c.inTx()`）拒绝删表（toast「手动事务模式下不能删表：先提交或回滚，或切回自动」/「事务中不能删表：先提交或回滚」；审查时在真库实测：MySQL 的 DROP 会把事务里的 insert 隐式提交、rollback 撤不回，PG 的 DROP 进了用户的事务、tab 关了 catalog 里表还在；要在事务里删表去 query 写）。按对象类型用 `DROP TABLE` / `DROP VIEW` / `DROP MATERIALIZED VIEW` / `DROP FOREIGN TABLE`，不带 CASCADE。键盘上同样的事：表 / 视图节点上 `dd`（`tree.delete`，F8.21 的同一个 Action，按节点类型决定删文件还是删表）。只读连接上拒绝、toast。
      - **对象节点**（M14 F14.3，细节 worker 开工时定）：表展开后先列、再两个默认收起的分组 `Indexes (n)`、`Keys (n)`（和列一起取、`R` 一起丢）；schema 下面在 Tables / Views 之后 `Functions (n)`、`Sequences (n)`（随 catalog 一次读，每种一条查询、所有 schema；只在有对象时才画，空 schema 不显示空分组）；引擎没有的不画（MySQL 没有 Sequences，SQLite 没有 Functions / Sequences）；树过滤时这些节点不显示，只有表和视图参与过滤。叶子文字：索引 `name (a, b) UNIQUE`（含主键那条：PG 的 pkey、MySQL 的 PRIMARY；SQLite 的 autoindex 不含），键 `PRIMARY (id)`、`fk_name (a) → schema.table (b)`（SQLite 的外键没名字，写 `FOREIGN KEY (a) → table (b)`），函数 `name(identity 参数)`（PG `pg_get_function_identity_arguments`，MySQL 用 `information_schema.parameters` 拼）。`↵`：索引 → 表 DDL 浮窗换成那条 `create index`（PG `pg_indexes.indexdef`，SQLite 用 `sqlite_schema.sql`，MySQL 自己拼）；外键 → 打开目标表（另一个 schema 就先展开它的 Tables）；函数 → 新 query 里放定义（PG `pg_get_functiondef`，MySQL `SHOW CREATE FUNCTION`），schema 是函数所在的；PRIMARY 和 Sequence 无事。只做函数：PG `prokind = 'f'`，不含扩展自带的（`pg_depend` deptype `e`）和 `pg_catalog` / `information_schema`；聚合、过程、触发器（`CREATE TRIGGER` 对象）不做，返回 `trigger` / `event_trigger` 的函数照列（它们就是函数，`prokind` 也是 `f`，审查时定）。普通视图不取也不画 Indexes / Keys，物化视图保留 Indexes；SQLite 表达式索引的列显示成字面的 `expr`，PG / MySQL 显示表达式原文（审查时定）。
      - **Queries 节点**（M8 F8.21，同 DataGrip 的「文件」列表）：工作区节点下面再一个根节点，列出 `queries/*.sql` 的全部文件加上开着但还没落盘的 query（没打过字的没有文件但占着编号；按名字，`query_<n>` 按 n 排，改过名的按字母排在后面；目录列表按目录的 mtime 缓存：每画一帧 stat 一次目录，mtime 变了才重读，外部的改动也能看到；F8.20 沿用上一帧时连 stat 也不做；同一时钟刻度里的两次变化看不到，`ponytail:` 标出），每项 `query` 图标 + 名字，右端 `dim` 色的连接名（state.json 里记的最后一次连接，没有就空）；正在某个 session 里开着的名字用 `focus` 色。`↵` / 单击：已经开着就聚焦过去（别的 session 开着就 attach 过去，同 `tab.reopen` 的规则），没开着就在当前 data pane 里新开一个 tab，连接按 §11（记下的连接，不在了用当前连接）。`dd`（`tree.delete`「删除 query 文件」，只对这个节点下的项有效）：确认框「删除 query_3？」后删文件和 state.json 里的记录；正在开着的也删（M10 F10.10 起，同 DataGrip；原来拒绝、toast「先关掉它的 tab」）：关掉它在所有 session 里的 tab，没落盘的内容随文件丢弃，不另提示，确认框已经问过了（同 F8.23 删表关 tab）。两个例外走关 tab 的常规检查：有语句正在执行的拒绝（toast「正在执行，结束后再关闭」）；它是所在连接最后一个 query、连接又在事务里时，再问一次（同关 tab）。已知上限：名字按 F8.11 的不分大小写比较，大小写敏感的文件系统上外部放进来的 `Foo` 会随 `foo` 一起关。右键菜单的「删除…」同样。新建、关闭、改名、删除后列表跟着变。
      - **工作区节点**：当前 session 的 window → tab，最小粒度是 tab（M1 用户反馈）。M10 F10.5 起没有 pane 这一层（用户：`pane-x` 价值不高、层级交错不好读）：window 节点下面直接是 tab 节点，按 pane 编号分组、组内按 tab 顺序，每行 `<带圈编号> <图标> 名字  摘要`，编号同 pane 标题（`① ②`，ascii 图标下 `⟨1⟩`）、`dim` 色；结果区的 tab 也这样列，「日志」tab 画图标 `log`（F3.30）；没有 tab 的 pane 不出现。tab 节点上 `↵` 或单击聚焦过去；tab 节点显示表名加 `dim` 色的条件摘要（WHERE、非默认的 ORDER）。打开、关闭、切换 tab 后跟着变。M5 F5.15 到 M9 是 window → pane → tab，pane 节点 `<pane 图标> pane-<n>` 可以展开 / 折叠。
      - **默认展开**：启动时的连接、它 `current_schema()` 所在的 schema 和那个 schema 的 Tables、工作区及其下所有节点；其余折叠，其他连接也折叠着、不连。之后连上的连接同样展开它的 `current_schema()` 和 Tables。展开状态记在 session 上（节点按连接区分），只在内存里。
      - 行数量级：n < 1000 原样显示；1000 以上用 k / m / b 后缀，一律向下取整，首位只有一位数字时保留一位小数（`8.1k`、`2.1m`），否则取整（`48k`、`410k`）；`reltuples < 0` 显示 `?`。
      - 右侧注释（行数、列的类型）放不下时整个不显示，先保名字；下拉和补全列表的行同样（M13 F13.9，审查时定）；侧栏只有 24 列时列节点的名字只剩十来列，可以拖宽侧栏。
      - 名字放不下时按字素簇截短并加 `…`，与 pane 标题、面板、表格一致；直接截掉会让 `mv_order_by_stat` 看起来像另一张表的全名（tester 在 F1.3 提出）。
      - 光标行和「当前打开的表」分开画：光标行在树聚焦时 `select` 底，失焦时 `row` 底，与表格的 cursor / cursor_blur 同理；悬停行 `row` 底，不移动光标。「当前打开的表」指 `↵` 会打开到的那个 data pane（§12 的规则）当前 tab 里的表，它的表节点图标和名字用 `focus` 色；工作区里，同一个 pane 的当前 tab 节点也用 `focus` 色。两处高亮说的是同一件事，焦点在树上时也有提示。
      - 光标移出可视区域时列表跟着滚；滚轮只滚视图，光标被夹回视图内（与 nvim 相同）。
    - 一条分隔线；
    - 提示行，复用 tab 栏 / 面板底栏的 `hintRow`：`j/k move · ↵ open`（F3.37 起去掉 `t tab`），全部 `dim` 色，` · ` 分隔，每一项都能点击（§7.4），放不下的项整项不显示。
  - **按键**：`j` / `k` / `gg` / `G` 在所有可见节点间移动，支持次数前缀。`l` 展开，已展开时移到第一个子节点；`h` 折叠，已折叠或是叶子时移到父节点（nvim-tree、neo-tree 的习惯）。`↵`：表 / 视图节点打开表（§7.8「打开已有的表」）；列节点打开所属的表，并把光标移到这一列；工作区的 tab 节点切过去并聚焦（在别的 window 里时先切 window），window 节点切到这个 window（F5.2；`l` / `h` 照常展开、折叠）；连接节点见上；其余节点展开或折叠。列节点打开已有的 tab 时，光标移到这一列；这一列被 COLS 隐藏了的话，光标不动，也不取消隐藏。叶子节点上按 `l` 不做事。`t`：表 / 视图节点上和 `↵` 相同（F3.37 起），其他节点上无效；Action `tree.open.tab`、`palette.open.tab` 的标题都叫「打开」。`/` 过滤，`R` 刷新。树里不再用 `gs`。
  - **鼠标**：单击 `▸` / `▾` 或分组、schema、连接节点，展开或折叠（未连接的连接节点同时去连）；单击表 / 视图 / 列节点与 `↵` 相同，中键与 `t` 相同；单击工作区的 tab 节点切过去，单击 window 节点切到这个 window（F5.2）；和点树上别的节点一样，焦点到树上，不会跳到那个 pane。
  - **每个连接的 Schema**（M5 起，原来是「树当前的 schema」；快速 SQL 的 search_path、新 query 的默认 schema、面板表范围的排序、快速 SQL 补全的表名，都用当前连接的）：光标停在某个连接的 schema 或它下面的节点上时，这个连接的 Schema 跟着变成那个 schema；光标在连接节点或工作区里时不变；初始为 `current_schema()`，为 NULL 时取第一个 schema。
  - **从树里打开表**（`↵` / `t` / 单击 / 中键）后，焦点移到 data pane，与命令面板一致（§12「打开表就是为了接着看数据」），也与 nvim-tree 的 `<CR>` 一致。要连续浏览几张表时按 `C-h` 回到树。
  - **连接节点下是空的直到 catalog 加载完成**，不显示「加载中」；加载出错用 toast 显示数据库的错误。`R` 重新加载当前连接（焦点在树上时是光标所在的连接）的 schema 和表列表，并清掉它缓存的列信息（Action `tree.refresh`）；展开状态保留。
- **pane 标题**：
  - 格式为 `⟨n⟩ <图标> 对象名`，ascii 图标下为 `⟨n⟩ <图标> 类型 · 对象名`（§7.7）。从左上角往右 1 列开始，两侧各留 1 个空格。
  - 编号 `⟨n⟩` 在 Nerd Font 图标下显示为带圈数字 `⓪ ① ② … ⑳`，只占 1 列（M0 用户反馈）；超过 20 时仍写成 `⟨21⟩`。ascii 图标下照旧是 `⟨n⟩`。标题、侧栏标题、命令面板里 pane 那一行都照此显示。`SPC q` 在 pane 中央显示的编号仍是普通数字，因为那是要按的键。
  - 带圈数字按 1 列计算，已知上限见 §7.1「宽度」。
  - 右侧提示离右上角 1 列。
  - **pane 按钮**（M19 F19.3，用户 2026-10-30，参考 Warp 的 pane 右上角）：每个 pane 标题的最右边四个按钮，从左到右：往右分割 `split_right`（`pane.split.right`）、往下分割 `split_below`（`pane.split.below`）、缩放 / 还原（`pane.zoom`；没缩放时画 `zoom`，缩放中画 `unzoom`）、关闭 `close`（`pane.close`）。画出来是 `<图标> <图标> <图标> <图标> `，整组和左边的提示隔 1 列；每个按钮占「图标加它后面那 1 列」，命中区和悬停的 `select` 底都盖这 2 列（ascii 同理，如 `[Z] `）：Nerd 字形常溢出到后一格，只亮 1 列就是「悬停只盖住半个」（§7.7），四个挨着也不好点；最后一个按钮后面那列就是「离右上角 1 列」的空隙，悬停时一起亮（reviewer 2026-10-30 问）。只这四个按钮这样，标题上别的单个图标提示（`«`、锁）不变；`dim` 色；点击先聚焦这个 pane 再执行对应的 Action，行为和按键完全相同（关最后一个 pane 时照 `pane.close` 的规则）。只画在 window 的 pane 上，侧栏和结果区不画。缩放中编号后面的 `zoom` 状态图标照旧。
  - query 标题的右侧从左到右是：事务控件 `Tx: 自动 ▾`（F5.3，手动时后面跟着提交 ✓、回滚 ↺）、连接和 schema 的下拉框 `play · sqlmux_play.public ▾`（F5.11 起加上连接；原来只有 `doraemon.public ▾`）、`▶ run`（`focus` 底、`bg` 色字、粗体）加上 `↵`（`dim` 色）。
    - 连接名和 `<库>.<schema>` 各是一个下拉框，点哪一段开哪一个：连接名用 `info` 色（同状态栏的地址块），`<库>.<schema> ▾` 照旧 `pk` 色，中间的 ` · ` 不能点，悬停只亮那一段；两个下拉框是同一个组件，位置、按键同 §8.6；`gs` 照旧打开 schema 下拉，命令「切换连接…」（`query.conn`）打开连接下拉。连接下拉列出全部连接（名字；每项的图标——只读的 `lock`、MySQL 的引擎图标——等 F5.3 / F5.4 给下拉框加上每项一个图标时一起画），当前的用 `pk` 色标出；选了未连接的就关掉下拉去连，连上之后才换过去（schema 取那个连接的 Schema），连接中又选了别的以最后一次为准，连不上 toast `<连接名>：<错误>`、留在原来的连接。只读连接在标题的连接名后面也画 `lock`，和连接名同一个命中区（`play <lock> · db.public ▾`）。所在连接有未提交的事务时不能换（§5）；这个 query 的执行还没回来时也不能换，toast「正在执行，结束后再换连接」（执行里可能有 begin，回来后原连接停在事务里、上面却没有 query 了，reviewer 在 F5.11 复现）。MySQL 的第二段是 `<database> ▾`（§8.6）。
    - `Tx: 自动 ▾` / `Tx: 手动 ▾` 点开是自动、手动两项的下拉，显示的是所在连接的模式（§5）。✓ 为 `focus` 色、↺ 为 `info` 色，悬停 `select` 底；所在连接不在事务里时两个都是 `dim` 色、不能点（同 DataGrip，也同查询条的停止按钮）。`Tx: …` 用 `fg` 色，下拉里当前那项 `pk` 色；✓ ↺ ▾ 不是 Nerd Font 字形，ascii 图标下照样用。
    - 提交、回滚（✓ ↺、`tx.commit` / `tx.rollback`）在当前连接的 `Main` 上用不可取消、带超时的 ctx 执行（同 §10.3 的 COMMIT），不在事务里时不做事。成功不弹 toast：状态栏的「事务中」消失、✓ ↺ 变暗就是反馈；失败 toast 错误。在出错的事务里提交，PG 实际做的是回滚，toast「事务已出错，提交改为了回滚」，免得以为已经提交了。提交、回滚之后（包括确认框里的回滚）重新取这个连接上打开着的表格，计数也重算：手动模式下表格读的是 `Main`，不重取的话屏幕上还是已经撤掉的值；同时清掉列缓存、重新加载这个连接的 catalog：手动模式下 DDL 执行时还没提交，在 `Meta` 上重载看不到它。
  - **空间不够时，提示优先于标题的对象名**，因为提示都是可点击的按钮（§7.4）。按下面的顺序分配空间：
    1. 先给最短的标题 `⟨n⟩ <图标>`（ascii 下是 `⟨n⟩ <图标> 类型`）留出位置。
    2. 按优先级从高到低依次摆放右侧的提示：放得下就保留；放不下就跳过这一个，继续试下一个。每个 pane 的提示按重要性排序，query 的顺序是 `▶ run` > 连接和 schema 的下拉框（两段算一项）> 事务控件（`Tx ▾` 和 ✓ ↺ 算一项）> 键位文字 `↵`；四个 pane 按钮算一项，排在每种 pane 的最后（M19 F19.3），放不下整组不画。所以宽度较窄时，可能显示 `▶ run ↵`，但没有下拉框。
       键位文字依附于它所属的按钮：按钮没显示，它的键位文字也不显示。例如 `↵` 只能跟着 `▶ run` 一起出现，不会单独出现。
    3. 剩下的空间再给标题里的对象名；放不下的部分截掉，末尾加 `…`，如 `⟨2⟩ query · cons…`。
    4. 连最短的标题都放不下时，只保留 `⟨n⟩`。
  - **空 pane**（没有 tab）：标题栏只显示 `⟨n⟩`（M3 F3.7 起，ascii 下也一样），右侧只有四个 pane 按钮（M19 F19.3：它们作用于 pane，不需要 tab），内容区显示引导页。这些提示都作用于当前 tab：`▶ run` 执行 tab 里的内容，schema 下拉框是 tab 的属性（§8.6）；没有 tab，它们就没有作用对象。新建 tab 用 tab 栏里的 `+`。
- **查询条**（Q-01～Q-06，data pane 的表格上方，两行；设计稿只给了大致布局，细节在 M1 F1.4 定）：
  - 第一行：`keyword` 色的 `WHERE`，后面是输入框，占满整行。输入框为空且没有聚焦时只显示 `WHERE`，不加占位提示。点击这一行就开始输入（Q-01 的鼠标一栏）。行的右端是历史 / 收藏入口 `▾`（F1.5）：点击时若还没开始输入，先进入 WHERE 输入，再打开下拉。
  - 第二行从左到右：
    - 四个 chip：` ORDER id <方向图标> `（多列时 ` ORDER source ↓, id ↑ `，M12 F12.1）、` LIMIT 100 `、` PAGE 1/60 `、` COLS 10/10 `。标签 `dim` 色、值 `fg` 色、`sep` 底，悬停 `select` 底，点击等于按对应的键（`go` `gl` `gp` `gc`）。ORDER 的方向用 `sort_asc` / `sort_desc` 图标（§7.7），它本身是一个按钮，见下面的 ORDER；
    - 图标按钮（M2/M3 验收时重新设计，见下面「工具按钮」；这一段是原来的三个按钮）：保存（M2 之前只占位，点击不做事）、刷新（`grid.refresh`）、转置（`grid.transpose`），图标见 §7.7。每个按钮画成 ` <图标> `，左右各留 1 列，按钮之间隔 1 列；悬停时整个按钮 `select` 底，命中区覆盖整个按钮（M1 用户反馈：图标紧贴、悬停只亮半个，见 §7.7「图标后面留空格」）。保存在 M2 之前没有命中区，悬停不亮，免得让人以为能点；M2 起有命中区，点击执行 `save`，有修改时画成 ` <save 图标> 3 `，3 是这个 tab 所有页的修改格数（按格计），没有修改时只有图标（Q-05）；
    - 最右边：`auto · 6000 行 · 12ms`。「行」是计数结果而不是本页行数（本页固定 100 行没有信息量）：计数完成前 `…`，超时 `?`，用估计值时 `~1.2m`（量级格式同侧栏）。
    - **不满一页时常驻**（M19 F19.4，用户 2026-10-30，参考 DataGrip 右下角的「157 行」：结果比 LIMIT 少时窄 pane 里这一段第一个让位，就看不到有几条）：第 1 页的行数少于 LIMIT 时，总数就是这一页的行数：第一页回来时直接记成计数结果、算作计数完成，不再发 COUNT（PAGE 立刻是 `1/1`，exec 连接也省一次子进程；只看第 1 页，后面的页不按这条推总数；reviewer 2026-10-30 问）；这时这一段放不下全文就缩成 `N 行`（i18n，同全文里的「行」），并且排到让位顺序的最后（ORDER 之后才让）。转置视图同一条查询条，一样。满一页的照旧。
  - 查询条和表格之间不画分隔线。
  - **工具按钮**（M2/M3 验收时定，取代上面的三个按钮；F3.26 重新分组：用户嫌每个图标一块底色难看）：分四组 `+ −` ｜ 保存 刷新 ｜ 自动刷新 停止 ｜ 转置 导出（导出 M12 F12.4）。M5 F5.5 起（用户嫌一组一块底色有割裂感，参考 DataGrip 的工具栏）：整排按钮共用一条 `sep` 底，组之间用 `border` 色的 `│` 分隔，转置也在这条底里；按钮各占 ` <图标> `；悬停时只把那一个按钮画成 `select` 底。下面的按钮说明按原来的三组写，颜色与行为不变：
    - 数据：新增行 `row_add`（`focus` 色，同 `o`）、删除行 `row_delete`（`error` 色，同 `dd`）、保存 `save`（`info` 色，有修改时带数字）；
    - 查询：刷新 `refresh`（`info`）、自动刷新 `auto_refresh`（`info`，开着时 `warn` 色并带间隔，如 ` <图标> 5s `）、停止 `stop`（这个 tab 有请求在跑时 `error` 色、可点，否则 `dim`、不可点，同 `C-c`）；
    - 视图：转置 `transpose`（`info`）。
    - 导出 `export`（`info`，同结果区的那个图标）：点开导出下拉（§7.6「导出」），Action `grid.export`「导出…」，默认键 `ge`；
    - 颜色都可以在主题的 `[icon]` 里用 `fg` 覆盖（§7.7）；有状态的按钮，覆盖的是「亮着」时的颜色（停止有请求在跑、自动刷新开着），停止空闲时照旧 `dim`，自动刷新关着时用 `info`。放不下时整组舍去，顺序为转置 / 导出 → 自动刷新 / 停止 → `+ −` → 保存 / 刷新（F3.26）。
    - 「有请求在跑」指这个 tab 的取数、计数或保存还没回来。
  - **自动刷新**：点按钮打开下拉框（关 / 2s / 5s / 10s / 30s / 60s；Action `grid.refresh.auto`，没有默认键；按钮让位了、从面板打开时，下拉框同 schema 下拉框锚在这个 pane 查询条的右端，右边对齐 pane 的右边框），按间隔重取当前页和计数，不清掉保存结果那行提示。只在这个 tab 是某个可见 pane 的当前 tab、没有未保存的修改、没有在编辑单元格、没有请求在跑、没有选中的记录也不在 VISUAL（M10 F10.7）时才触发，不满足就跳过这一轮；有修改或有选中时自动暂停，免得修改、选中被刷掉。间隔按 tab 记，不持久化。
  - **第二行放不下时谁让位**（M3 默认布局里 ① 只有 70 列左右）：行数统计 `auto · 6000 行 · 12ms` 最先让位，放不下就不显示（不满一页时例外，见上面「不满一页时常驻」）；然后依次是转置、自动刷新 / 停止、`+ −` 三组（F3.26），再是 COLS，再是选中数 `3 selected`（M10 F10.7，独立的一段，不跟着行数统计一起没：默认布局 63 列下行数统计和三组按钮都已经让位、保存 / 刷新后面只剩 4 列，选中数要看得见就得排在 COLS 之后；选中时 COLS 让位，`c` 和面板照样能开），再是 PAGE、LIMIT，然后才是保存 / 刷新那一组（M8 F8.9 审查时改：它带着未保存的修改数，比 LIMIT 100 要紧；原来排在 COLS 之前，1:1 的默认布局在 160 列下会把四组按钮全让掉），最后 ORDER。保存结果的优先级高于 chip 和按钮，放不下时照上面的顺序隐藏，直到放得下，再放不下按 §10.3 中间截短。保存结果那行文字消失后，被隐藏的 chip 和按钮照常回来。
  - **ORDER**（`go`）：排序是有序的列表（M12 F12.1，用户：只能一列一个方向是严重缺陷；原来只支持单列，Q-03）。M12 F12.9 起是 COLS 式的列表浮层（用户验收时定：打开就 `j` / `k` 选、`/` 搜，和 COLS 一样；原来是通用下拉框，打字直接进过滤框）：列出所有列，每行 `1↓ 列名  类型` 加主键标记（不在排序里的前面空着），没有「默认」项；打开时焦点在列表，`j` / `k` 移动，`/` 进过滤框（`↵` / `esc` 回列表，过滤文字保留），`esc` 先清空过滤再关闭；作用域 `order`，键在 `[keys.order]`；底部提示行 `j/k 移动 · space 方向 · a 移出 · A 清空 · ↵ 只按这一列 · / 搜索`，键位从 keymap 读。框的宽度按内容：最长的 `列名  类型` 行加序号标记、边框和内边距，最少 30、最多 pane 宽，同 COLS；提示行放不下时折成两行，再放不下按值编辑器提示行的规则丢末尾的项，不把框撑宽（M15 F15.6，用户验收：ORDER 的六项提示把框撑到整个 pane 宽；COLS 是同一个框，一起改）。`↵`（`order.only`）：在唯一的排序列上翻转方向，在别的列上换成只按这一列、方向按 `[grid] order`（默认 asc；M8 F8.8，用户常倒序看最新数据，配成 desc 后新打开的表和新选的列都先降序；布局文件里记的方向优先），并关闭，和原来单列时一样。`space`（`order.toggle`）：不在排序里 → 追加到末尾、方向按 `[grid] order`；在 → 翻转；不再去掉（原来第三次按是去掉，用户验收时改）。`a`（`order.remove`）把光标所在列移出排序，`A`（`order.clear`）清空、回到默认排序。这三个都不关浮层，每次改动立刻重取，连按几次就能排好多列。默认排序时在行标识列上按 `space` 得到显式的 `1↑`（方向按 `[grid] order`），行和 SQL 同默认，只是列表里标了序号，和 `↵` 的「选行标识列得到降序」不同（审查时定，按 `space` 的字面）。chip 显示整个列表 `source ↓, id ↑`（文字箭头），末尾仍画第一列方向的图标按钮（点它翻转第一列，同单列时）；列表文字超过剩余宽度时只显示前两列加 `…`（审查时定，不用 §10.3 的中间截短，免得把 LIMIT / PAGE / COLS 挤掉）。默认排序时 chip 上显示的那一列（行标识列；复合键时是它的第一列）就算当前排序列：chip 显示 `id ↑` 时选 `id` 得到 `id ↓`，chip 显示 `occurred_at,id ↑` 时选 `occurred_at` 得到 `occurred_at ↓`，不能只是从「默认」变成显式的 `id ↑`，否则用户看着 chip 会以为按了没反应（tester 在 F1.4 提出）。SQL 为 `ORDER BY <列1> <方向1>, <列2> <方向2>, …, <行标识列>`，行标识列作 tiebreaker 保证分页稳定，列表里已有的行标识列不重复。换了排序后回到第 1 页（和 LIMIT 一样）。chip 默认显示行标识列 `id` 加升序图标，没有行标识列时显示 `—`、不画方向图标。点击方向图标切换升降序（Action `grid.order.toggle`，标题「切换排序方向」，没有默认键；多列时翻转第一列，图标画在 chip 末尾），规则同上：默认排序时点击得到行标识列降序；行标识列是复合键时（如 `occurred_at, id`），得到按第一列降序，其余键列照旧作 tiebreaker，与下拉框里选第一列的规则一致；点击 chip 的其余部分仍打开 ORDER 浮层（设计稿如此，M1 用户反馈）。
  - **LIMIT**（`gl`）：同一个下拉框，100 / 500 / 1000，换了之后回到第 1 页。过滤框里可以直接输入正整数作为自定义的每页行数（M1 用户反馈）：输入是正整数时，候选第一项就是这个数，`↵` 应用，后面照旧列出匹配到的 100 / 500 / 1000；超过 10000 按 10000，因为一页数据全在内存里，再大会卡。`0`、负数、小数不当数字，照常过滤。
  - **PAGE**（`gp`）：chip 原地变成页码输入框 `PAGE [3_]/60`（INSERT），`↵` 跳页（超出范围夹到边界），`esc` 放弃。总页数在计数未知时是 `?`，估计值加 `~`。`]` / `[` 翻页：已是最后一页（没有下一页）时 `]` 不起作用，第 1 页时 `[` 不起作用。翻页后光标的行、列保留，夹在新页范围内。
  - **COLS**（`gc`）：打开时焦点在列表上：`j` / `k` 移动、`space` 勾选、`a` / `A` 全选 / 全不选、`esc` 先清空过滤再关闭；`/` 进过滤框（模式仍是 COMMAND），`↵` 或 `esc` 回列表，过滤文字保留。每行 `[x] 列名  类型` 加主键标记，顶部显示匹配数。`J` / `K` 把这一列下移 / 上移一位（M10 F10.3）：和相邻项换位，表格的列顺序立刻跟着变，光标仍在原来那一列上；过滤框非空时不动；底部提示 `J/K move`。顺序和隐藏的列都按 tab 记住、同一处存法（布局文件的 `order_cols`，§14），`yy`、`yi`、`Y`、转置视图都按显示顺序；隐藏的正好是光标所在列时，光标挪到最近的可见列。chip 显示 `可见列数/总列数`。键位在 `[keys.cols]`。
  - **WHERE**（F3.39 起是一行的 vim 编辑器，同 query：INSERT 下 `esc` 到 NORMAL，NORMAL 下能用 `dd`、`ciw` 这类操作，`/` 打开历史 / 收藏下拉；NORMAL 下再 `esc` 才回表格。F6.6 关掉 vim 模式时是普通输入框）：`↵` 应用输入、回到第 1 页、重新取数和计数、焦点回表格；回表格时输入框恢复成当前生效的那条，所以输入框显示的总是正在生效的条件；执行出错时例外，失败的条件留在输入框里。报错显示在 pane 底部的错误栏（§7.8「错误栏」），查询条仍在上面，方便改了再执行。
  - **`R`**（`grid.refresh`）：按当前 WHERE / ORDER / LIMIT / PAGE 重新取当前页并重新计数。
- **tab 栏**：
  - 位于 pane 内容区的最后一行，底色为 `bg`（比 pane 底色深），不画分隔线。
  - **放不下时横向滚动**（M17 F17.1，用户 2026-10-16：开到七八个 tab 后面的看不见）：当前 tab 始终可见，从它往两边摆，两端画 dim 的 `‹N` / `N›`（N 是那一边看不见的 tab 数，可点击、各滚一屏）；`gt` / `gT` / `{N}gt` / 点击切换后自动滚到当前 tab；`+` 和右侧键位提示照旧；滚动位置按 pane 记、不进布局文件；不换行、不另开翻页态，命令面板的 `#` 范围列全部 tab。
  - 每个 tab 显示为 ` 序号:名称标记 `，tab 之间用 `│`（`sep` 色）分隔。
  - 当前 tab 用 `pane_bg` 底色、`focus` 色字；其他 tab 用 `dim` 色字。
  - tab 之后是可点击的 `+`；最右端是 `dim` 色的键位提示，如 `hjkl · ↵ edit · T 转置 · gt/gT`，文字从 keymap 读取。
  - **切换**（M1 F1.6）：`gt` / `gT` 照 vim，下一个 / 上一个，到头绕回；`{N}gt` 跳到第 N 个（超出不动），`{N}gT` 往回 N 个。作用于焦点 pane，树和空 pane 上无效。点击 tab 切过去并聚焦该 pane。切换时原来的当前 tab 成为「上一个」（`-`）；旧 tab 正在编辑 WHERE / PAGE 时先退出输入，输入框恢复成生效的条件。中键关闭、拖动排序不做。
  - **打开已有的表**（F3.37 用户定，取代 M1 起的「在整个 window 里找、多个时列出选择」）：同一个 pane 里一张表只开一个 tab，不同的 pane 可以各开一个，用来对比同一张表的不同条件。从树或命令面板打开一张表时，只在目标 pane（§12「表」）里找这张表的 tab（按 schema + 表名比较）：
    - 有：切过去，焦点移到这个 pane，不重新取数，状态保留；它就是当前 tab 时只移焦点；是从这个 pane 的引导 tab 发起的（引导页的 `t`、`+` 之后从树或面板选），引导 tab 一起关掉，不留空 tab；
    - 没有：在目标 pane 新开一个 tab，不替换当前的表 tab 或 query tab；只有引导 tab 会被替换，它只是占位。别的 pane 里开着这张表不影响。
    - `C-t`、树里的 `t`、`+` 的标记同样遵守这条规则，目标 pane 里已有就切过去。「选择 tab」列表不再使用。
  - **`+`**：命中区执行 Action `tab.new`（标题「新建 tab」，没有默认键，面板能搜到）：在 `+` 所在的 pane 新开一个引导 tab 并聚焦它（M3 F3.7 起，取代 M1 的「展开树、进过滤框、记下下一次打开进新 tab 的标记」，那个标记已经没有了）。
- **转圈**（M19 F19.2，用户 2026-10-30，参考 lazygit 推送时的 `正在推送 •●•`）：在途的请求画一个三点动画，帧依次 `●••` `•●•` `••●` `•●•`，每帧 150ms；ascii 图标下 `o..` `.o.` `..o` `.o.`；颜色跟它所在那一段的字色。画在四处，都替掉原来的静态文字或跟在后面：状态栏的 busy 块（` busy •●• · C-c 取消 `，§8.3）、结果 tab 的执行中占位（§11）、查询条行数统计里计数完成前的 `…`、树上连接中的连接节点名后面的 `…`。整个程序一个 tick 驱动所有转圈，只在屏幕上有转圈时 tick，全都停了就不再发（空闲时不重画，CPU 和 e2e 截屏都靠这个）；帧号从 0 开始，golden 里是第一帧。
- **状态栏**：
  - 底色为 `bar`（#292e42）。各段是扁平色块，不用 powerline 箭头，每段左右各留 1 列内边距。
  - 左侧依次为：
    - ` doraemon ▾ `（session 名）：`focus` 底、`bg` 色字、粗体。M5 起 session 不再对应一个连接，不画引擎图标；点击打开 session 列表（F5.1）；
    - window 列表，如 ` 0: data* `：当前 window 用 #3b4261 底、`fg` 色字，其余 window 用 `dim` 色字、无底色。
  - 右侧右对齐，依次为：
    - 模式附加信息：`dim` 色，过长时用省略号。内容取自设计稿，随当前模式变化：
      - NORMAL：不显示；
      - INSERT：正在编辑的对象，如 `-- editing WHERE --`；
      - VISUAL：选区大小和执行键，如 `4 lines · ↵ run`，键位文字从 keymap 读取；
      - COMMAND：不显示，匹配的命令在命令面板里。
    - **特性提示**（M15 F15.4，用户 2026-10-11：特性多、新用户不会细看文档）：命令面板入口的左边一段 `dim` 色的 `Tip: table pane 里 yl 复制当前单元格`，是状态栏最先让位的一段（上面的保留顺序不变，它排在全部之后），模式附加信息、待输入序列、toast 不受它影响。清单在代码里，每条是 Action ID 加一句话（`i18n.T`，中英文都有；键位用 `{key}` 占位、从 keymap 读，用户改过键跟着变，解绑了就跳过这条；每条对应一个真实存在的 Action，加特性时顺手加提示，单测遍历清单对 Action 表）。启动时随机一条，之后每 5 分钟换一条（一个 `tea.Tick`），不重复直到轮完；单击换下一条；`tips.next`「下一条提示」、`tips.show`「所有提示…」（就是命令面板的 `?` 前缀，§12：能搜索，`↵` 执行那个 Action）；`[ui] tips = false` 关掉（§14）。候选但没定：只在闲置时才换、前几次启动更频繁。
    - ` <搜索图标> `：`info` 色，点击打开命令面板；ascii 图标下显示为 ` ~ C-p `（§7.7）；
    - ` <键盘图标> 待输入序列 `：序列用 `warn` 色粗体。没有待输入的键时，显示 `dim` 色的 `·`；这一块始终占着位置，序列部分至少 3 列宽（放得下 `SPC`），内容靠左，这样按键时右侧各块不会左右跳动，序列超过 3 列时才变宽；
    - ` 行,列 `：`fg_muted` 色。只在焦点所在的 data pane 或结果区有已加载的表时显示（M3 起结果区也算）：行号是绝对行号（页偏移 + 1），列号从 1 起；树聚焦、空 pane、空表都不显示这一块（M0 的 `1,1` 占位就此去掉）；
    - ` <图标> play · sqlmux@localhost:5432 `：`info` 色，`sep` 底。内容是当前连接（§5）的 `<连接名> · <用户>@<host>:<port>`（M5 起加上连接名：两个连接常常只差库名，比如 play 和 seed），`@` 前面放数据库用户名；unix socket 时 host 就是目录，照样显示，如 `ctw@/tmp:5432`。设计稿里的 `pg@` 按此理解；
    - ` 事务中 `（`warn` 色）/ ` 事务出错 `（`error` 色）：当前连接的 `Main` 在事务里 / 处在出错的事务里时显示（M5 F5.3），自动模式下用户自己 `begin` 的也算；在 busy 提示（§8.3）的左边，和它一样不让位；
    - ` NORMAL `：模式色底、`bg` 色字、粗体。
  - 窗口太窄、放不下时，按下面的顺序依次省略：
    1. 模式附加信息；
    2. 连接地址；
    3. 不是当前的 window，从右往左去掉；
    4. 光标位置；
    5. 截短 session 名。

    始终保留的是：session 块（名称可以被截短）、当前 window、`C-p` 入口、待输入序列、模式块。
- **COMMAND 模式**：命令面板打开时，模式块显示 COMMAND；接管按键的浮层（schema / LIMIT 下拉、COLS 列表、ORDER 浮层、WHERE 历史 / 收藏下拉）打开时也是 COMMAND，不论焦点在列表还是过滤框，因为它们和面板一样把 normal 作用域的键（leader、`C-hjkl`）挡在浮层外，不另设模式名；keymap 里 COMMAND 和 INSERT 一样算「在打字」，没绑定的可打印字符照样进过滤框。which-key 不算：它不是作用域（§6.5），弹出时序列还没按完，剩下的键仍在 normal 里解析，所以照旧显示 NORMAL。补全列表也不算：它不接管输入，只是挂在输入框下面，显示 INSERT。状态栏里不再有命令行：M0 用户体验后改由命令面板取代（§12），`:` 打开面板（F5.8 起是「所有」范围）。
- **JUMP 模式**（M10 F10.8）：flash 式跳转进行中显示 JUMP，`info` 色（§6.10）；跳转的遮罩不盖状态栏。表格的选择（M10 F10.7）在扩选时显示 VISUAL，同 query。
- **错误栏**（M2/M3 验收时定，参考 DataGrip 在编辑区下方的报错区）：数据库的报错显示在出错那个 pane 的内容区底部、tab 栏上方，不再挤在查询条右侧或者占掉表格。
  - 第一行是 `error` 色的 `[SQLSTATE] <Message>`，下面依次是 DETAIL、HINT、位置（PG 返回时才有），一项一行，DETAIL 自带的换行照拆，`error_bg` 底；每行放不下时右边截短加 `…`，不折行；最多 6 行，超出时第 6 行画成 `…`；右端是可以点击的 `×`。不是数据库返回的错误（如「WHERE 里不能有 ;」、保存时行数据已变化）没有 `[SQLSTATE]`，只有文字。
  - 位置：query 的报错换算成 query 文件里的「位置：第 3 行第 12 列」；表格取数的 SQL 是拼出来的，不显示位置。
  - 错误栏占内容区最下面的几行，表格 / query 的可见高度相应变矮，不盖住内容。一个 tab 同时只有一条，后来的替换先前的；错误栏属于 tab，切走不显示，切回来还在。
  - 关闭：点 `×`，或者在这个 pane 的 NORMAL 下按 `esc`（Action `pane.error.close`）。同一个 tab 下一次同类操作成功时自动消失：取数成功清掉取数的报错，保存成功清掉保存的报错，query 每次开始执行就清掉上一次的；编辑单元格不清。`esc` 绑在 `[keys.normal]`，query 的 VISUAL 下和编辑器有待定键时照旧交给 vim。
  - 用在：表格的取数与 WHERE 报错、保存失败（「id = 42：…，已回滚」这一类，§10.3）、query 的执行报错（日志 tab 照旧记，出错语句的 ▶ 照旧变红；结果区 `R` 重跑出错时显示在来源 query 的 tab 上，来源已关就只记日志）。保存被取消的「已取消，已回滚」不是错误，仍在查询条右侧。
- **toast**：显示在状态栏上方一行的右侧，默认 3 秒后消失；「再按一次 C-c 退出」这一条显示 2 秒，正好是连按的窗口（§6.8）。样式为 `warn` 色字、#292e42 底、左右各留 1 列。toast 只有一行：文字里的换行换成「；」、Tab 换成空格，在显示 toast 的那一处统一做（驱动的错误常是多行的，比如 localhost 解析出两个地址各报一行；F5.10 审查时发现）。设计稿里没有 toast，这个样式是后定的。加底色是因为那一行正好是 pane 的下边框，不加底色，文字会和边框混在一起。data pane 中保存 / 刷新的结果按 Q-06 的要求显示在查询条的右侧，不通过 toast 显示。
- **session 列表**（M5 F5.1，S-02、S-03；`SPC s` 或点击状态栏的 session 块）：框和行的样式、位置、宽度照命令面板，标题「session」。每行是 session 名和 `dim` 色的 window 数（如 `3 windows`），当前 session 用 `focus` 色；`l` 展开后下面一级是它的 window（`0: data`），当前 window 同样标出。`$` 把这一行原地变成输入框（INSERT，`↵` 确定、`esc` 放弃）；`x` 关闭 session：有未保存的修改或未提交的事务时先确认（§10.5），关的是最后一个 session 等于退出。单击一行等于移过去再按 `↵`。每个 session 下面都标出它自己的当前 window（同 tmux）。键位见 §6.8 的浮层表。列表和新建连接表单打开时按 `C-p`，面板盖在上面并接管按键（同 COLS 列表）；表单里的 `C-s` 是保存表单，不去保存背后表格的修改。切换 session（包括 `n` 建连回来后自动 attach）时关掉所有浮层，确认框也关。关掉有查询在执行的 session 不另确认，查询随连接关掉，也不弹「查询已取消」。
- **新建 session**（`n`，命令 `session.new`）：先在状态栏单行输入里要名字（同 `session.rename`；M10 F10.2，用户要先起名；window 不要，序号就行），预填 tmux 式最小的没用过的数字（`1`、`2`……），`↵` 用输入的名字（trim 后为空就用预填的），重名 toast 同重命名、输入框留着，`esc` 什么都不建；一个 window、一个空 pane（引导页），不建 query（M8 F8.18，用户：新建 window / session 时总是两个 pane；原来是默认布局带一个新 query_n）；连上当前连接并照「默认展开」展开它，其余连接不连。新 session 有自己的一套连接，按 `n` 后先在后台给它连上当前连接（toast「正在连接 <名字>…」），连上之后才建出 session 并 attach 过去，连不上就 toast `<名字>：<错误>`、不建，所以「当前连接一定是连上的」照样成立。
- **把 tab 移到相邻 pane**（M12 F12.5，用户 2026-10-11 要 `SPC wn` / `SPC wp`）：`tab.move.next` / `tab.move.prev`「把 tab 移到下一个 / 上一个 pane」（默认 `<Leader>wn` / `<Leader>wp`）把当前 tab 挪到这个 window 里顺时针的下一个 / 上一个普通 pane（顺序同 `pane.focus.next`），放在那个 pane 的 tab 列表末尾并成为当前 tab（目标 pane 当前是引导 tab 时替换它，同打开表的惯例），焦点跟过去；来源 pane 空了留引导页；结果区不是目标，只有一个普通 pane 时无事；引导 tab 不能移；目标 pane 已有同一张表的 tab（F3.37）时不移、toast「目标 pane 里已有这张表」，不合并免得丢未保存的修改；缩放中无事（屏幕上只有一个 pane，同 `pane.focus.next`）。
- **新建连接表单**（M5 F5.1，命令 `conn.new`「新建连接…」；M7 F7.4 按用户要求重做成分字段、居中、好看）：居中的框，宽 60 列（窗口不够时取窗口宽 − 4），单线 `focus` 色边框，标题「新建连接」。字段从上到下：name、group（分组，M10 F10.6：单行输入，留空就是没分组；有已有组名时提供补全，复用 `complete` 列表、前缀匹配、`Tab` / `↵` 接受，和 WHERE、query 的补全一样打了字、有匹配才弹出，焦点刚进字段不弹：不然 `C-p` 被列表截走、列表盖住下面的 engine / host 行；组名的字符规则同连接名；图标 `group`，nf-fa-tags，U+F02C）、engine（分段开关 `postgres │ mysql │ sqlite`，M14 F14.1 起选了 sqlite 只剩 name / group / engine / file / read_only：host、port、user、password 隐藏并跳过，database 字段改叫 file、就是路径，高级里的 dsn 照旧，测试用 `sqlite_version()`；选中段 `select` 底，`space` / `←` `→` 切换，单击选中点到的那一段、点已选中的不变，同单选按钮）、host、port（engine 切换时没改过的 port 跟着换默认 5432 / 3306）、user、password（输入画 `*`，F7.3）、database、read_only（`[x]` 开关）、最下面一行「高级」折叠：dsn 和 password_cmd（展开后直接填 dsn 时上面的 host / port / user / database 变灰、不参与）。每行是「图标 + 字段名 + 输入框」（用户定）：图标用 Nerd Font，后面留 1 格（§7.7），字段名右对齐、`dim` 色，输入框占满右侧、`sep` 底；当前字段的输入框换 `select` 底、图标和字段名 `fg` 色。图标：name `tag`（nf-fa-tag，U+F02B）、engine 用现有的 `postgres` / `mysql`（跟着选中的段变）、host `host`（nf-fa-server，U+F233）、port `port`（nf-fa-plug，U+F1E6）、user `user`（nf-fa-user，U+F007）、password 用现有的 `key`、database `database`（nf-fa-database，U+F1C0）、read_only 用现有的 `lock`（树里只读连接也是它）、「高级」`advanced`（nf-fa-cog，U+F013）、dsn `link`（nf-fa-link，U+F0C1）、password_cmd 用现有的 `query`；校验不通过的字段名 `error` 色，原因写在底栏上方一行。底部一行三个按钮「保存」「测试」「取消」（`focus` 色边、可点击）加键位提示（`Tab 下一项 · ↵ 保存 · esc 取消`，从 keymap 读；`C-t` 不进提示行，F7.4 / F8.16 的宽度验收塞不下，按钮本身和 `?` 都能看到它；编辑已有连接时英文四个按钮占满一行，160 列下提示行挪到按钮下一行，宽度验收英文的下限 75）。**测试**（M12 F12.5，同 DataGrip 的「测试连接」；Action `form.test`，`C-t`）：先做和保存一样的校验，通过后用表单当前的值拼 dsn（密码取字段里的，或跑 password_cmd），在 Cmd 里真连一次（连接超时同 §8，10s），期间按钮处写 `测试中…`；结果写在底栏上方那一行（和校验错误同一行）：成功 `已连接 · PostgreSQL 17.7 · 120ms`（版本来自 `version()` / `@@version` 的第一段，`focus` 色，主题没有 ok 色），失败是驱动的错误原文（`error` 色；原来一行放不下按 §10.3 中间截短，M18 F18.13 起不截：按表单宽度折行，占几行就几行、框随之变高，窗口放不下时框取窗口高度只画得下的行；校验错误那一行同样折行；用户 2026-10-26 验收时提，Windows 上的 dial 错误一行放不下看不到原因）；测试完就断开，不保存、不进树；测试中按钮文字不变，`测试中…` 写在结果行（按钮变宽会把「删除」挤掉）；改任何字段、再按 `C-t`、关表单都清掉结果行，晚到的旧结果丢弃；连接管理里用表单当前的值，已有连接的密码取 password_cmd / 钥匙串里原来的；版本文字 PG 取 `version()` 在「 on 」前、再在「 (」前截一刀（`PostgreSQL 17.11 (Debian …)` → `PostgreSQL 17.11`），MySQL 取 `@@version` 前缀「MySQL 」。dsn 字段空着时显示 `dim` 色的示例（按 engine：`postgres://user:pass@host:5432/db?sslmode=prefer`、`mysql://user:pass@host:3306/db`；用户 2026-10-11 怕新用户看不懂格式；只有这个字段有示例，别的字段照旧不预填）。单行输入框是 `ui.Input`（同面板，不是 vim）。`↵` 保存：校验同启动时读 connections.toml（name 必填、名字的字符规则、不能重名；host / user / database 必填，port 是数字，或者填了 dsn），不通过就焦点移到出错的字段；通过就拼成 `dsn`（`postgres://user@host:port/db?sslmode=prefer` / `mysql://user@host:port/db`，密码不进 dsn、按 F7.3 存钥匙串）追加到 connections.toml（§14，文件的字段不变，仍是 name / group / engine / dsn / password_* / read_only），所有 session 的树里多出这个连接，状态是未连接。**从「没有连接」的启动状态保存第一个连接时**，保存后直接连上并展开，并像正常启动那样在 ② 建 query_1 替掉引导页，和 `sqlmux <连接名>` 启动的结果一致；零连接期间 ①② 都是引导页、不建 query 文件（F7.4）。「高级」里 dsn 非空时 host / port / user / database 变灰、Tab 跳过、不校验，password 不灰（dsn 里不带密码是常态，密码照存钥匙串）；host 不预填（必填），port 预填 5432 / 3306（用户改过之前跟着 engine 换），其余空；dsn 用 `net/url` 拼，user / database 转义，IPv6 用 `JoinHostPort`；不预填占位提示字。
- **连接管理**（M8 F8.16，用户要能改、能删，参考 DataGrip 的数据源面板）：命令 `conn.manage`「管理连接…」打开居中浮层，左边是按「分组」排的连接列表（图标 + 名字，只读的带 `lock`，选中项 `select` 底，最后一项 `＋ 新建`），右边就是上面这张表单，填着选中连接的值（`dsn` 写法的连接把 host 等从 dsn 拆出来只读显示，改的是 `dsn` 那一栏——不做改字段写回 dsn 的对应段，`ponytail:` 标上限；`${VAR}`、`password_cmd` 原样显示在高级里）；`j` / `k` / 单击切换，`Tab` 进表单，`↵` / `C-s` 保存，`esc` 关闭，列表上 `x` 或表单下的「删除」按钮删除（确认框）。有 session 正在用它（有它的 tab、结果 tab 跑在它上面、或它是启动连接）时不再拒绝（M8 的「先关掉用它的 tab」toast 去掉，M18 F18.12，用户 2026-10-26 验收时提：不该阻塞，二次确认就够）：确认框写「删除连接 D？它开着的 N 个 tab 会一起关掉」，有未保存的表格修改再加一句「，其中 M 个有没保存的修改，会丢」，它或它各库的子连接在事务里再加关 session / 退出时 `unlessLost` 的那句「D 有未提交的事务，会回滚」（reviewer 2026-10-26 问）；正在跑的查询同关 session：直接取消，不拦。`y` 后的顺序（worker 定，reviewer 认可：落盘或写文件失败时 tab 不白关）：先把它的 query 文字照 §11 落盘、删文件块和钥匙串，失败就 toast、什么都不关；成功了再在每个 session 里关它的 tab（表格的修改丢弃、不进「重开关掉的 tab」列表）、空了的 pane 一起收，启动连接是它时换成剩下的第一个，最后关掉它和各库子连接已连上的 `Main` / `Meta`（不关的话服务端会话留到退出、未提交的事务一直拿着锁；关法同 `closeSession` 的 `closeWorkers`，不在 Update 里同步等）。按钮和键位提示放不下同一行时提示挪到按钮下面一行、右对齐（80 列下右半边只有 45 列），列表固定 26 列。`conn.new` 和侧栏的 `＋` 都打开它并选中 `＋ 新建`，不再另有一个单独的表单。文件改写规则见 §14「修改和删除连接」；改了已连上的连接不重连，toast「下次连接时生效」；删除时钥匙串里的密码一并删。
- **新建连接的入口**（F7.4）：侧栏第一行（过滤图标和 `N tables` 那一行）右端一个 `＋` 按钮（图标 `row_add`，`info` 色，悬停 `select` 底，可在 `[icon]` 里覆盖），单击打开表单；命令面板的「新建连接…」照旧。没有任何连接时，树的正文只有一行可点击的「还没有连接，按 ＋ 新建」（`dim` 色），单击等于按 `＋`。

### 7.9 文本输入：删词与自动配对（M2/M3 验收时定）

- **删词**：所有单行输入框（面板、树的过滤框、COLS、PAGE、单元格编辑、编辑器的 `:` / `/` 命令行；WHERE 在 F3.39 起同 query 的 INSERT，`C-u` / `C-w` 照 nvim 先删到这次 INSERT 的起点，再按一次才删到行首）都支持 `C-w` 删掉光标前的一个词、`C-u` 删到行首；词的划分同 vim INSERT 下的 `C-w`（先跳过空白，再删一串关键字字符或一串其他非空白字符）。query 的 INSERT 本来就有。
- **`C-h` 在所有单行输入框里当退格**，同命令行和 query 的 INSERT（§6.2 的终端差异；M13 F13.11，原来面板、过滤、COLS、PAGE、单元格、表单、下拉的输入框不认）；flash 跳转（§6.10）的输入同样。
- **`M-BS`（Option / Alt + 退格）当作 `C-w`**：在上面这些地方和 query 的 INSERT 里都一样，用户在 nvim 里就是这样映射的，也是 macOS 文本框的习惯。它是编辑文字的内建行为，不写进 `default.toml`，所以「默认键位不用 Alt」（§6.2）的检查不受影响；终端不发这个键时，`C-w` 照样能用。query 的 NORMAL / VISUAL 下 `M-BS` 不做事。
- **自动配对**：在 query 的 INSERT、WHERE 输入框、快速 SQL 里，输入 `(`、`[`、`{`、`'`、`"`、`` ` `` 时补上另一半，光标停在中间，如 `order = '█'`。
  - 只在光标后面是行尾、空白或右括号时才配对；引号还要求光标前面不是字母数字或同一个引号，免得 `don't`、`''` 这类被弄乱。
  - 光标后面正好是同一个右括号或引号时，输入它只是跳过去。接受补全也一样：插入的内容以引号结尾、光标后面正好是同一个引号，就一起替换掉，WHERE 里 `status = '█'` 接受 `'done'` 得到 `'done'█`。
  - 光标在一对空的配对中间时按退格，两边一起删。
  - 粘贴原样插入，不配对，也不跳过光标后面的同一个字符（同 VS Code、nvim-autopairs）。
  - 在 query 里这些都算这次 INSERT 的一部分，一起撤销。单元格编辑、树的过滤框、COLS、面板的其他范围不配对。
  - 配置 `autopairs = true | false`，默认开。编辑器的 nvim 差分测试里关掉，nvim 本体没有这个行为。
- **有选区时输入开括号或引号**（非 vim 模式才有「选着再输入」，F6.6）：把选区包起来，不替换，同 VS Code 的 autoSurround。
