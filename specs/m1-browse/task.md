# M1 浏览（PostgreSQL，只读）· 任务清单

- **状态**：F1.1–F1.7 全部 passed，完整回归在 1fab6bc 上全绿（748 项 e2e）。用户验收的改进项 F1.8–F1.12 开发中。开发清单已按 worker 的核对意见补充，决策者已确认（2026-09-23）。
- **目标**：连接 PostgreSQL，浏览 schema 和表数据，全程只读。M1 完成后，就有一个可以日常用来查数据的版本。
- **范围**：
  - PRD：D-01~D-04、Q-01~Q-06（保存除外）、G-01、G-04~G-06、T-01~T-03、S-01（只含单个 session）、F-01~F-03（快速 SQL 的只读部分）
  - tech-design：§7.6、§8、§9.6、§9.7（WHERE 部分）
- **依赖**：M0 全部 passed。
- **完成标准**：
  - F1.1–F1.12 全部 passed；
  - 集成测试在 docker 的 PG 上全部通过；
  - M0 和 M1 的 e2e 回归全部通过；
  - 用户验收通过后打 tag `m1`。
- **审查节点**：
  1. F1.7 之后，F1.6–F1.7 一批（已通过）。F1.1–F1.5 按旧流程逐个审查、测试过（2026-09-24 改为按批）；
  2. F1.11 之后，改进项 F1.8–F1.11 一批，都是小改动；
  3. F1.13 之后，F1.12–F1.13 一批：层级目录树，加上随后的键位导出。

任务文件的格式和状态约定见 [`../plan.md`](../plan.md)。

---

## F1.1 测试数据库与连接 · 状态：passed（1157e3d；e2e 017f052）

- **依赖**：M0
- **涉及**：`docker-compose.yml`、`testdata/seed/pg.sql`、`internal/db`（Conn、Worker、postgres）、`internal/config`（connections.toml）

**开发**
- [x] `docker-compose.yml`：
  - 包含 postgres:17 与 mysql:8.4（MySQL 在 M5 用）；
  - 端口避开本机默认端口，PG 用 55432、MySQL 用 53306。
- [x] `testdata/seed/pg.sql`：建两个 schema（`public`、`agentable`），表要覆盖以下情况：
  - 单列主键、复合主键、只有非空唯一索引、无主键；
  - enum、boolean、timestamptz、json / jsonb 类型；
  - 可空列、有默认值的列；
  - 至少一张 5000 行以上的表（用于测分页和计数）；
  - 一个视图、一张物化视图、一张分区表（含两个分区），用来测 catalog 的列表规则（§8.4）；
  - 一行含换行、Tab 和 ESC 字符的文本，用来测单元格的控制字符清理（§7.6）。
- [x] 读取 `connections.toml`（§14）：
  - 字段：`name`、`engine`、`dsn`、`password_cmd`、`password_env`、`password`；前三个必填，`engine` 只接受 `postgres`；`~/.pgpass` 由 pgconn 自动读取。`read_only` 到 M5 做 S-04 时再加，M1 没有代码读它（reviewer 的意见）；
  - 密码来源的优先级、`password_cmd` 的执行方式按 §13「凭据」；
  - 文件中写有明文 `password`，且对同组或其他用户可读时，进入界面后用 toast 警告（§13）。
- [x] PG 版 `db.Conn`（§8.1）：
  - `Exec` 走简单协议，结果为文本；
  - `Query` 走扩展协议，参数按文本传、OID 传 0，结果为文本；
  - `Close`；取消走 ctx，不单设 `Cancel` 方法（§8.1）；
  - 值用 `Val{S, Null}` 表示；`Col.Type` 用 pgtype 的 OID 表转成类型名，认不出的留空（§8.1）；
  - 连接参数用 `pgconn.ParseConfig` 解析，环境变量和 `~/.pgpass` 交给它；没写 `application_name` 时补成 `sqlmux`，没写连接超时时补成 10s（§8.1）；
  - ContextWatcherHandler 改用 `CancelRequestContextWatcherHandler`（`DeadlineDelay` 5s）：pgconn 默认的 handler 在 context 取消时会断开连接（§8.3）。
- [x] `db.Worker`（§8.2）：
  - 每条连接一个 Worker，用互斥锁串行执行请求；`Cancel()` 取消当前请求的 ctx；
  - 每个 session 有 Main 和 Meta 两条连接；
  - 建连超时 10s；`main` 里同步建连，先 Main 后 Meta，都连上才进界面（§8.2）；
  - 建连时通过 RuntimeParams 设置 `DateStyle = ISO, YMD`（§8.1），建连后记录原始 `search_path`；
  - `Meta` 的 RuntimeParams 另加 `default_transaction_read_only = on`，设为只读（§8.2）。
- [x] 启动方式：`sqlmux <连接名>`，未指定时使用第一个连接。找不到连接或者连接失败时，在终端打印错误后退出，退出码为 1，不进入界面（§14「启动时找不到连接」）。
- [x] 默认 window 名为 `data`，只有侧栏和一个占满其余宽度的空 data pane（§5）。去掉 M0 的假 session、假 console、第二个 window `report`，以及 data pane 的假 WHERE 行和假表格；假的表列表留到 F1.2 换成 catalog。从面板打开表仍只建 tab，取数在 F1.3。初始焦点仍在 data pane，经由 `Window.focus()` 设置（M0 审查留下的建议）。`PaneKind` console 及其标题提示保留，M3 要用，现有测试自己构造 console pane 覆盖，不算死代码。M0 的 e2e 里依赖这些假数据的用例（如命令面板里的 `%report`），提测时告诉 tester 一起调整。
  - M0 修剪时留下的观察：按 ID 找 pane 的写法已经有三份（`focused()`、`scrollPane`、`openTable`），换成真实 session 和 tab 后如果再多出来，就提一个 `pane(id)` 辅助函数。
- [x] 集成测试用环境变量 `SQLMUX_TEST_PG` 指定连接串；没有设置时，`-tags integration` 的测试直接 skip。
- [x] 状态栏改为显示真实的 session 名（`connections.toml` 里的 `name`）和地址 `<用户>@<host>:<port>`（§7.8）。

**验收**
- [x] 集成测试覆盖：
  - 建连；
  - 各类型的文本值，包括 NULL、enum、json，以及 ISO 格式的 timestamptz；
  - 用 `pg_sleep` 触发 `Worker.Cancel()`，确认能取消，并且取消之后同一条连接还能继续查询；
  - `Meta` 是只读的：在 `Meta` 上执行会写数据的语句（例如 `select nextval('<序列>')`），返回只读事务的错误。
- [x] `password_cmd`、`password_env`、`~/.pgpass` 三种方式都能连上。
- [x] 密码错误时，终端打印数据库返回的错误，退出码为 1，不 panic。
- [x] 没有 `connections.toml` 时，错误里写明配置文件的路径；`sqlmux <名字>` 的名字不存在时，列出已有的连接名。
- [x] `connections.toml` 中写有明文密码且权限为 0644 时，出现警告。

## F1.2 catalog 与 schema 树 · 状态：passed（f0a7db2；e2e a65a8cf）

- **依赖**：F1.1
- **涉及**：`internal/db`（catalog）、`internal/ui`（tree）

**开发**
- [x] catalog 查询（§8.4）：
  - 表列表及行数估计、列属性、主键、非空唯一索引、枚举值；
  - 加载时机按 §8.4：Init 时只查 schema 列表和表列表；列属性等按表在第一次打开时查并缓存。F1.2 先把按表查的那条查询和集成测试写好，F1.3 取数时用；
  - 按 session 缓存；`R`（Action `tree.refresh`）重新加载表列表并清掉列缓存；
  - PG 按 §8.4 表下列出的细节实现：分区的子表不列出、`reltuples < 0` 显示 `?`、列属性查 `pg_attribute`、主键按 `conkey` 的顺序、唯一索引的判定条件、枚举按 `enumsortorder` 排序。
- [x] 树的过滤直接复用 F0.13 的 `match.go`。
- [x] schema 树（§7.8 的样式）：
  - 显示表列表、行数量级（格式见 §7.8），光标行与当前打开的表分开画（§7.8）；
  - 按键：`j` / `k` 移动（支持次数前缀）；`/` 过滤（nvim-tree 式 live filter，§7.8）并高亮匹配字符；`↵` 在当前 tab 打开，`t` 在新 tab 打开；
  - 绘制放在 `ui.Tree`（过滤行、列表、提示行），app 只提供数据和状态；
  - 鼠标：单击在当前 tab 打开，中键在新 tab 打开；支持滚轮（每格 3 行，最多滚到最后一项贴着底边，§7.4）和悬停高亮。
- [x] 侧栏底部的提示行（`j/k move  ↵ open …`）复用 ui 的 `hintRow`。M0 里它是在 view.go 手写的，和 pane 标题提示、tab 栏 / 面板底栏已经是三种画法，不要再写第四份（M0 修剪时留下的观察）。
- [x] 切换树的 schema：在树里按 `gs`，或点击侧栏标题上的 schema（F0.15 已经画出这个按钮），打开 schema 下拉框（交互、位置、键位见 §8.6）。这个下拉组件以后 console 的 schema 选择会复用。
  - 侧栏标题显示树当前所在的 schema，切换后跟着变，不再写死为 `public`。
- [x] 命令面板的「表」范围改为列出 catalog 里所有非系统 schema 的表，所在位置显示真实的 `session.schema`，顺序见 §12，不再用 M0 的假表。tab 名 F1.2 只写表名，F1.3 改成带 schema 的 DataTab。
- [x] 从树里打开表之后，焦点移到 data pane（§7.8，已定）。

**验收**
- [x] 集成测试：catalog 返回的主键、唯一索引、枚举、可空、默认值都与 seed 一致；分区表只列父表，物化视图在列表里。
- [x] e2e：
  - 树列出 seed 中 `public` 下的表，行数量级格式正确；
  - `/` 过滤后高亮匹配字符；
  - 用 `↵`、`t` 和鼠标都能打开表（data pane 标题显示表名）；
  - 用 `gs` 切换到 `agentable` 后，列出该 schema 下的表。

## F1.3 data pane 表格（只读） · 状态：passed（febd031；e2e 766323c）

- **依赖**：F1.2
- **涉及**：`internal/ui`（grid）、`internal/app`（DataTab）、`internal/db`

**开发**
- [x] 取数：
  - 查询语句为 `SELECT * FROM "schema"."table" ORDER BY <行标识列> LIMIT n OFFSET m`；
  - 走 Meta 连接和扩展协议；用 seq 丢弃过期的响应；
  - 打开表时先查列缓存，没有就在 Meta 上查 TableColumns 并缓存（§8.4），再算行标识列（§10.1）拼查询；没有行标识列时不加 ORDER BY；两次往返放在同一个 Cmd 里；建列缓存时，F1.2 的 `R` 要一并清掉它；
  - 拼 SQL 的函数放在 `db/postgres`，标识符用 `pgx.Identifier{}.Sanitize()`，F1.4 往里加 WHERE、ORDER；F1.3 只取第 0 页；
  - 每页默认 100 行，多取一行来判断还有没有下一页（§8.5）；
  - 从命令面板打开表（`↵` / `C-t`）和从树里打开走同一条路径，真正取数，取代 M0 里只改 tab 名的做法。
- [x] 表格渲染（§7.6）：
  - 复用 F0.9 的 grid 组件和网格样式，把占位数据换成真实数据；
  - 只渲染可见区域（虚拟滚动），列宽按列宽算法计算；
  - 按列的数据库类型映射到 F0.12 的类型分类（对照表在 §7.6），分别着色；数值右对齐；NULL 显示为暗色的 `<null>`；过长内容截断为 `…`，截断按字素簇；
  - 数据库报错在 pane 内容区第一行用 error 色显示，不画表格（§7.6）；
  - `Pane.Tabs` 改为 `[]Tab`，`ui.Grid` 的行用 `[][]db.Val`（§7.6「数据结构」）；
  - 清理控制字符：换行显示为暗色的 `↵`，Tab 显示为空格，其他控制字符（包括 ESC）去掉（§7.6）；
  - 主键列显示图标；显示行号，当前行的行号为绿色；
  - 单元格光标在 pane 聚焦时为蓝底，失焦时变暗；当前行有底色。
- [x] 移动：
  - 键盘：`hjkl`、`gg`、`G`、`0`、`$`，支持次数前缀，支持 `[map.grid.*]` 映射；光标移出可视区域时，表格跟着纵向或横向滚动；
  - 滚轮：纵向滚轮纵向滚动，每格 3 行，最多滚到最后一行贴着底边（§7.4）；Shift+滚轮和横向滚轮横向滚动，每格 1 列；滚动后光标夹回视图内；
  - 点击单元格移动光标：命中区 Kind `cell`，等于 `pane.focus` + `grid.goto r c`（§7.4）。
- [x] 转置 `T`（G-05）。
- [x] 状态栏显示光标的行号和列号（行,列），只在焦点 data pane 有已加载的表时显示（§7.8）。
- [x] 查询执行中状态栏显示忙碌提示；按 `C-c` 或点击提示可取消查询，取消后 toast 提示（§8.3）。

**验收**
- [x] e2e：
  - 打开 seed 中的表后，显示第一页数据；
  - 各种类型的值显示正确；
  - 光标移动后，状态栏的行,列与光标一致；
  - 转置后光标位置保持不变；
  - 配置 `[map.grid.normal] L = "5l"` 后，按 `L` 右移 5 列；
  - 按 `l` 或 `$` 把光标移到可视区域右边以外时，表格横向滚动，光标所在的列完整可见；
  - seed 里含换行、Tab、ESC 的那一行显示为 `↵` 和空格，屏幕上不出现 ESC 引起的错乱。
- [x] 补回 M0 里因 F1.1 去掉假表格而删掉的 23 项 e2e（tester 在 F1.1 结论里列出）：f0.9 整个脚本（`┼`、对齐、留白、sep 色、表头 func 色、主键钥匙图标、斑马纹、cursor / cursor_blur、数值右对齐、滚动后表头固定、80 宽裁列、ascii 下显示 `* id`，17 项）；f0.8 表格滚轮每格 3 行、滚到顶停住；f0.12 列按类型着色及 row / cursor / number·string·time token 生效（4 项）。
- [x] 长查询（例如 WHERE 中带 `pg_sleep(3) is not null`）执行时显示忙碌提示；按 `C-c` 后查询被取消，并有提示。
- [x] 表格渲染的 golden 测试通过（160×45，固定数据：一个打开了 t_order 的 data tab，含 NULL、枚举、json、timestamptz、带换行的文本各一列；外加一张转置视图）。

## F1.4 查询条 · 状态：passed（d8c2447；e2e 2207c8b）

- **依赖**：F1.3
- **涉及**：`internal/ui`（querybar、dropdown）、`internal/app`

**开发**
- [x] WHERE 输入框（§9.6）：
  - 在表格中按 `/` 聚焦输入框，进入 INSERT 模式；`↵` 执行，`esc` 回到表格；
  - 输入是完整的条件表达式，拼接为 `WHERE (\n<输入>\n)`（§9.6）；
  - 只允许一条语句，依靠扩展协议拒绝多语句。
- [x] WHERE 输入框复用 F0.13 的单行输入组件：按字素簇处理，显示光标，超宽时保持可见。不用 bubbles 的 textinput，它按 rune 处理退格（worker 核对时实测，textinput.go:606）。
- [x] 编辑 WHERE 时，状态栏的模式附加信息显示 `-- editing WHERE --`（§7.8），用上 M0 为它保留的 StatusLine.Info。
- [x] 查询条布局按 §7.8「查询条」：两行；第二行是四个 chip、三个图标按钮（图标名进 §7.7）和右侧的 `auto · <计数> 行 · <耗时>`。
- [x] 四个 chip（行为细节见 §7.8「查询条」）：
  - ORDER（`go`）：通用下拉框选排序列，同列 `↵` 翻转方向；单列；行标识列作 tiebreaker；
  - LIMIT（`gl`）：默认 100，可选 100 / 500 / 1000（§8.5），换了回第 1 页；
  - PAGE（`gp`）：显示 `当前页/总页数`，chip 原地变页码输入框；`]` / `[` 翻页，边界上不起作用；翻页后光标夹在新页内；
  - COLS（`gc`）：打开时焦点在列表上，`/` 进过滤框，`space` 勾选，`a` / `A` 全选与全不选，esc 分两步；隐藏列按 tab 记住。
- [x] F1.2 的 `[keys.schema]` / `schema.*` 改名为 `[keys.dropdown]` / `dropdown.*`，schema、ORDER、LIMIT 三种下拉框共用；COLS 用 `[keys.cols]`（§6.8）。
- [x] 计数（§8.5、§8.3）：
  - 在 Meta 连接上用 `tea.Sequence` 排在取数之后异步执行，ctx 超时 3 秒，超时显示 `?`；不算 busy；
  - 没有 WHERE 且估计行数超过 100 万时，显示估计值 `~n`。
- [x] 查询条右侧（Q-06）：
  - 显示事务模式（M1 固定为 auto）、计数结果（`…` / `?` / `~n` / n）、耗时；
  - `R` 刷新：按当前 WHERE / ORDER / LIMIT / PAGE 重取当前页并重新计数；转置按钮可以使用，保存按钮在 M2 之前先占位。
- [x] WHERE 的 `esc` 恢复成当前生效的条件；报错显示在 pane 内容区第一行，查询条仍可编辑（§7.8）。
- [x] 补 F1.3 漏掉的两条 PRD 要求（§7.6）：悬停行 `row` 底、不移动光标（G-06）；点击行号把光标移到该行、列不变，命中区 Kind `rowno`（G-04）。

**验收**
- [x] e2e：
  - WHERE 能正确过滤数据；表达式写错时，显示数据库返回的错误；
  - 输入 `1=1; drop table x` 会被拒绝，执行后表仍然存在；
  - 末尾带 `--` 注释的条件（如 `status = 'done' -- 备注`）能正常执行，排序和分页不受影响。
- [x] WHERE 输入框：输入 é（e+U+0301）或 👍🏽 后按一次退格，整个字删掉；光标可见；输入超出宽度后，正在输入的位置仍然可见。
- [x] ORDER / LIMIT / PAGE / COLS 的按键和点击行为都正确；翻页后数据正确；COLS 的过滤高亮、全选、全不选都正确。
- [x] 大表的计数显示为 `~n`；计数超时显示 `?`（造法：WHERE 写 `pg_sleep(0.001) is not null`，页面只取 101 行很快回来，计数要扫全表约 6 秒而超时。锁表造不出来：取数先被锁住，排在后面的计数不会开始）。
- [x] 悬停行有底色且光标不动；点击行号后光标移到该行、列号不变。
- [x] 查询条的 golden 测试通过。

## F1.5 WHERE 补全与历史 / 收藏 · 状态：passed（233aa6d；e2e 2207c8b）

- **依赖**：F1.4
- **涉及**：`internal/sqlkit`（补全上下文的 WHERE 部分）、`internal/ui`（补全列表、WHERE 下拉）、`internal/config`（state.json）

**开发**
- [x] WHERE 补全（§9.7「WHERE 补全的细节」）：
  - 上下文判断 `sqlkit.WhereContext(text, pos)`，只做词法判断，先看 lazysql 的 sql_lexer.go / sql_context.go；
  - 候选：列名（附类型）和关键字；在 `列 =`、`列 <>`、`列 in (` 之后，给出枚举列和布尔列的可选值；弹出时机、接受规则按 §9.7；
  - 列表组件 `ui.Complete`，键位 `[keys.complete]`：`C-n` / `C-p` / `↑` / `↓` 移动，`Tab` 接受；
  - `↵` 只在明确选中过某个候选时才接受；esc 分两步；支持鼠标。
- [x] `C-r` 或 WHERE 行右端的 `▾` 打开历史 / 收藏下拉框（Q-02，§9.7）：
  - 分成「收藏」和「历史」两组，用 WHERE 输入框的文字模糊过滤；
  - `[keys.where]`：`C-n` / `C-p` 移动，`↵` 应用（WHERE / ORDER / LIMIT 一起换，回第 1 页），`C-f` 收藏或取消收藏，esc 关闭；
  - 每一项右侧显示附带的排序 / limit（与默认不同时），或者时间；
  - 历史每表最多 50 条，去重后挪到最前。
- [x] `state.json`（§14）：保存每张表的 WHERE 历史和收藏，以及命令面板的最近使用（按「种类 + id」记录，§12）；结构、写入方式、损坏文件的处理按 §14；文件权限为 0600。

**验收**
- [x] e2e：
  - 输入 `sta` 时出现 `status` 候选，匹配字符高亮，按 `Tab` 接受；
  - 输入 `status = ` 后，出现该枚举列的可选值；
  - 没有选中候选时，`↵` 直接执行查询。
- [x] 执行过的 WHERE 会进入历史；收藏的条件在重启后仍然存在；`C-r` 下拉的模糊过滤正确。
- [x] `state.json` 的权限为 0600；把它写坏后启动，有 toast 报错、原文件被改名为 `state.json.broken`、程序照常可用。

## F1.6 data pane 的 tab · 状态：passed（1fab6bc；e2e 380fa9d）

- **依赖**：F1.3，可以与 F1.4、F1.5 交错开发
- **涉及**：`internal/app`（tab）、`internal/ui`（tabbar）

**开发**
- [x] 每个 tab 对应一张表，各自保留 WHERE、ORDER、LIMIT、PAGE、COLS、光标位置和转置状态（T-01~T-03）。
- [x] `gt` / `gT` 切换 tab；`x` 关闭 tab（M1 中没有修改，直接关闭）；点击 tab 切换；点击 `+` 时聚焦 schema 树的过滤框，选中的表在新 tab 中打开。
  - 新增 Action `tab.new`，没有默认键；`+` 的命中区改为执行它，不再用 `KindTab I:-1`（M0 审查留下的建议）。
- [x] 打开表时，在当前 window 的所有 data pane 里找这张表的 tab（§7.8「打开已有的表」）：没有就打开到目标 pane；只有一个就切过去；有多个就在命令面板里列出来选（显示位置和条件摘要），从树按 `↵` 也一样。`C-t` 始终新开，同一张表可以在多个 pane、多个 tab 里分别用不同的 WHERE。
- [x] tab 栏：当前 tab 标 `*`，上一个 tab 标 `-`；右侧显示键位提示（T-03）。
- [x] 切换、打开已有的表、`+` 的细节按 §7.8「tab 栏」（`{N}gt` / `{N}gT`、多个同表 tab 时的选择列表、`+` 的一次性标记）。
- [x] 顺带修 F1.2 遗留：树里的表名放不下时加 `…`（§7.8），窄侧栏（24 列）下 `mv_order_by_status` 目前被直接截掉。截短与匹配高亮的处理挪成 ui 里的小函数，树和面板共用。

**验收**
- [x] e2e：
  - 打开两张表各一个 tab，两者的状态互不影响；
  - `gt` / `gT`、`x`、点击都能正确切换或关闭 tab；
  - 点击 `+` 后聚焦树的过滤框，选中的表在新 tab 中打开；
  - 同一张表在两个 pane 里各开一个 tab 后，从面板和树按 `↵` 都出现选择列表，选中后焦点落到对应的 pane 和 tab；只开了一个时直接切过去。

## F1.7 命令面板：快速 SQL（只读） · 状态：passed（1fab6bc；e2e 380fa9d）

- **依赖**：F1.3（表格）、F1.5（补全列表、state.json）。用户确认 M0 时要求提前，原计划在 M4。
- **涉及**：`internal/app`（palette）、`internal/ui`（palette 的结果区）、`internal/db`

**开发**
- [x] 面板加上「SQL ;」范围标签；输入以 `;` 开头时，输入内容就是 SQL（§12「快速 SQL」）。
- [x] 执行：`↵` 在 `Meta` 上以 `BEGIN READ ONLY` 执行，结束后一律 ROLLBACK；可以用 `C-c` 取消。
  - 最多显示 100 行，超过时显示 `100+`；
  - 不能让服务端算出整个结果集，也不能靠改写语句加 LIMIT（还没有 sqlkit）。按 §12 定的做法：首词为 select / values / table / with 的走 `DECLARE … CURSOR` + `FETCH FORWARD 101`，其余直接执行；同一事务里 `SET LOCAL search_path` 为树当前的 schema；
  - 执行中 `C-c` 取消、面板不关（§12）。
- [x] 结果区：显示在面板的下半部分，复用 F1.3 的表格组件和网格样式；标题行显示行数、耗时和「只读」；数据库报错时显示错误。SQL 范围下面板向下扩展，结果区至少 8 行。
- [x] 补全：输入时按 catalog 的表名、列名和 SQL 关键字做模糊补全，复用 F1.5 的补全列表。能看懂别名、CTE、子查询的补全要等 M3 F3.7。
- [x] 已修改提示（F-03）：输入和上次执行的语句不同时，底栏提示「已修改，↵ 重新执行」。
- [x] 历史：SQL 范围在输入为空时列出最近 50 条，去重，存进 state.json。
- [x] `C-y`：把结果用 `encoding/csv` 生成 CSV，通过 OSC 52 复制。`C-t`（送到结果区）、`C-e`（在 console 中打开）要等 M3。

**验收**
- [x] 集成测试：
  - 对 5000 行以上的表执行 `select *`，只取回 101 行，服务端没有算完整个结果集（`select i, 1/(i-5000) from generate_series(1, 10000) i` 拿回 100 行和截断标记，而不是除零错误）；
  - `show search_path`、`explain select …` 能执行；树停在 `agentable` 时 `select * from agent` 能找到表；
  - 写语句（如 `delete from …`）被只读事务拒绝，表里的数据不变（在自建库上跑）；`select 1; delete from …` 这类多语句被拒绝。
- [x] e2e：
  - 输入 `;select status, count(*) from t_order group by 1`，按 `↵` 后，结果以表格样式显示在面板下半部分，标题行显示行数和耗时；
  - 结果超过 100 行时显示 `100+`；语法错误时显示数据库返回的错误；
  - 输入表名、列名的前几个字母时出现补全，匹配到的字符高亮；
  - 改动输入后，出现「已修改」提示；执行过的语句出现在历史里，重启后仍在；
  - 按 `C-y` 后剪贴板里是 CSV。

---

以下为 M1 用户验收的改进项（2026-09-28）。

## F1.8 图标间距、查询条按钮与 ORDER 方向 · 状态：todo

- **依赖**：F1.7
- **涉及**：`internal/ui`（各处绘制图标的地方、querybar）、`internal/app`（grid.order.toggle）、`internal/keymap`（图标名）

**开发**
- [ ] 所有图标后面留 1 个空格再接文字：树、面板的搜索行与候选、pane 标题、侧栏标题、状态栏、查询条（§7.7「图标后面留空格」）。宽度计算照旧按 1 列。
- [ ] 查询条的三个按钮画成 ` <图标> `，按钮之间隔 1 列；悬停时整个按钮 `select` 底，命中区覆盖整个按钮；图标默认 `info` 色（§7.8「查询条」、§7.7）。
- [ ] ORDER chip 的方向改为 `sort_asc` / `sort_desc` 图标（默认 `warn` 色）。点击方向图标切换升降序，新增 Action `grid.order.toggle`（标题「切换排序方向」，没有默认键）；点击 chip 其余部分仍打开下拉框；没有行标识列、chip 显示 `—` 时不画方向图标。
- [ ] 新图标名 `sort_asc`、`sort_desc`、`view`、`column` 加进可覆盖的列表（§7.7）；`view`、`column` 在 F1.12 用。

**验收**
- [ ] golden：查询条（含一个悬停中的按钮）、树、面板搜索行，图标后面都有 1 列空白。
- [ ] e2e：点击方向图标后发出的 SQL 方向翻转，chip 的图标跟着变；默认排序时点击得到行标识列降序；点击 chip 的列名部分打开下拉框。
- [ ] 主题的 `[icon]` 给 `save`、`refresh`、`transpose`、`sort_asc` 写 `fg` 后生效。

## F1.9 补全列表的按键 · 状态：todo

- **依赖**：F1.7
- **涉及**：`internal/ui`（complete）、`internal/app`（WHERE 输入框、面板）、`internal/keymap/default.toml`

**开发**
- [ ] 按 §9.7「交互」改：弹出时第一项弱高亮；第一次 `Tab` / `C-n` / `↓` 选中它，之后移到下一项；`S-Tab` / `C-p` / `↑` 上一项；`↵` 只在选过时接受，否则照常执行；`esc` 两步。WHERE 与快速 SQL 共用。
- [ ] 快速 SQL 的面板里，`Tab` / `S-Tab` 在列表开着时移动候选，关着时照常切换范围。
- [ ] `default.toml` 的 `[keys.complete]` 跟着改（§6.8）。

**验收**
- [ ] e2e：
  - WHERE 输入 `sta` 后 `status` 弱高亮，`↵` 直接执行查询；按一次 `Tab` 后 `status` 强高亮，`↵` 接受；再按 `Tab` 移到下一项、`S-Tab` 回来；
  - 快速 SQL 输入 `select 42 as x` 后 `↵` 直接执行，语句不变；
  - `esc` 关掉列表后 `↵` 执行查询；
  - 快速 SQL 里列表开着时 `Tab` 移动候选，关着时 `Tab` 切换范围。

## F1.10 `;` 直接打开快速 SQL · 状态：todo

- **依赖**：F1.7
- **涉及**：`internal/keymap/default.toml`、`internal/app`（palette）

**开发**
- [ ] `[keys.normal]` 绑 `;`：打开面板，输入框预填 `;`，即 SQL 范围（§12、§6.8）。

**验收**
- [ ] e2e：焦点在表格、树、空 pane 时按 `;`，面板打开在 SQL 范围，输入 `select 1` 后 `↵` 能执行。

## F1.11 LIMIT 自定义每页行数 · 状态：todo

- **依赖**：F1.7
- **涉及**：`internal/app`（LIMIT 下拉）

**开发**
- [ ] LIMIT 下拉框的过滤框里输入正整数时，候选第一项就是这个数，`↵` 应用、回到第 1 页；超过 10000 按 10000（§7.8「查询条」）。

**验收**
- [ ] e2e：输入 `250` 后 `↵`，chip 显示 `LIMIT 250`，发出的 SQL 为 `LIMIT 251`；输入 `99999` 得到 10000。

## F1.12 层级目录树 · 状态：todo

- **依赖**：F1.8（图标间距）
- **涉及**：`internal/ui`（tree）、`internal/app`（树的状态与按键、工作区节点）、`internal/db`（列查询已有，复用）

**开发**
- [ ] 按 §7.8「schema 侧栏」重写树：session → schema → Tables / Views → 表 → 列；工作区 → window → pane → tab。参考 sqmeow.nvim 的层级（用户给的截图）。
- [ ] `ui.Tree` 改为通用的节点树：每个节点有层级、是否可展开、图标、文字、右侧注释；app 只提供节点和展开状态。
- [ ] 列节点在展开表时才取，与打开表共用列缓存（§8.4）；主键列用 `key` 图标。
- [ ] 按键 `h` / `l` / `↵` / `t` / `/` / `R`，鼠标单击、中键（§7.8、§6.8）。树里去掉 `gs` 和 schema 下拉框，侧栏标题改为 session 名。
- [ ] 过滤只匹配表和视图，匹配项的上级展开，其余隐藏。
- [ ] 「树当前的 schema」按 §7.8 的定义，快速 SQL 的 search_path、面板表范围的排序、补全的表名都改用它。
- [ ] 工作区节点随 tab 的打开、关闭、切换更新；焦点所在的 tab 节点用 `focus` 色。

**验收**
- [ ] golden：默认展开状态；展开一张表后显示列（主键带 key 图标，右侧是类型）；过滤后的树。
- [ ] e2e：
  - `l` / `h` 展开、折叠、跳到父节点；分组节点 `↵` 展开折叠；
  - `agentable` 下的表不切 schema 就能展开、打开；
  - 列节点 `↵` 打开表，光标落在这一列；
  - 工作区里单击或 `↵` 一个 tab 节点，焦点落到对应的 pane 和 tab；打开、关闭 tab 后工作区跟着变；
  - 过滤能跨 schema 匹配；
  - 光标移到 `agentable` 下后，快速 SQL 的 `select * from agent` 能找到表。

## F1.13 键位配置的完整导出 · 状态：todo

- **依赖**：F1.12（树的按键定下来之后再导出）
- **涉及**：`cmd/sqlmux`（keys 子命令）、`internal/keymap`

**开发**
- [ ] `sqlmux keys --format toml` 按 §6.7「导出」输出完整的配置片段：分节注释、每行的标题注释、没有绑定键的 Action 的注释行、`[map.*]` 示例。
- [ ] 同一个键在不同作用域各绑一个 Action 的情况留一个单测锁住：用户配置里 `[keys.grid]` 和 `[keys.tree]` 把同一个键绑到不同 Action，`--check` 通过，两边各自生效；`[map.grid.normal]` 与 `[map.tree.normal]` 同理。

**验收**
- [ ] 把 `sqlmux keys --format toml` 的输出原样放进隔离的 `config.toml` 后启动，`sqlmux keys` 的输出与没有配置时完全一致。
- [ ] 输出里每个有标题的 Action 都恰好出现一次，要么是绑定行，要么是注释行。
