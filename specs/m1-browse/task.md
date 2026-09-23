# M1 浏览（PostgreSQL，只读）· 任务清单

- **状态**：draft。开工前由 worker 核对并补充开发清单，经决策者确认后改为 todo。
- **目标**：连接 PostgreSQL，浏览 schema 和表数据，全程只读。M1 完成后，就有一个可以日常用来查数据的版本。
- **范围**：
  - PRD：D-01~D-04、Q-01~Q-06（保存除外）、G-01、G-04~G-06、T-01~T-03、S-01（只含单个 session）
  - tech-design：§7.6、§8、§9.6、§9.7（WHERE 部分）
- **依赖**：M0 全部 passed。
- **完成标准**：
  - F1.1–F1.6 全部 passed；
  - 集成测试在 docker 的 PG 上全部通过；
  - M0 和 M1 的 e2e 回归全部通过；
  - 用户验收通过后打 tag `m1`。

任务文件的格式和状态约定见 [`../plan.md`](../plan.md)。

---

## F1.1 测试数据库与连接 · 状态：draft

- **依赖**：M0
- **涉及**：`docker-compose.yml`、`testdata/seed/pg.sql`、`internal/db`（Conn、Worker、postgres）、`internal/config`（connections.toml）

**开发**
- [ ] `docker-compose.yml`：
  - 包含 postgres:17 与 mysql:8.4（MySQL 在 M5 用）；
  - 端口避开本机默认端口，PG 用 55432、MySQL 用 53306。
- [ ] `testdata/seed/pg.sql`：建两个 schema（`public`、`agentable`），表要覆盖以下情况：
  - 单列主键、复合主键、只有非空唯一索引、无主键；
  - enum、boolean、timestamptz、json / jsonb 类型；
  - 可空列、有默认值的列；
  - 至少一张 5000 行以上的表（用于测分页和计数）；
  - 一个视图。
- [ ] 读取 `connections.toml`（§14）：
  - 字段：`name`、`engine`、`dsn`、`password_cmd`、`password_env`、`read_only`；`~/.pgpass` 由 pgconn 自动读取；
  - 文件中写有明文 `password`，且对同组或其他用户可读时，给出警告。
- [ ] PG 版 `db.Conn`（§8.1）：
  - `Exec` 走简单协议，结果为文本；
  - `Query` 走扩展协议，参数按文本传、OID 传 0，结果为文本；
  - `Cancel`、`Close`；
  - 值用 `Val{S, Null}` 表示。
- [ ] `db.Worker`（§8.2）：
  - 每条连接由一个 goroutine 独占，请求串行执行；
  - 每个 session 有 Main 和 Meta 两条连接；
  - 建连超时 10s；
  - 建连后执行 `SET DateStyle = ISO, YMD`，并记录原始 `search_path`。
- [ ] 启动方式：`sqlmux <连接名>`，未指定时使用第一个连接；连接失败时显示数据库返回的错误。
- [ ] 状态栏改为显示真实的 session 名和地址。

**验收**
- [ ] 集成测试覆盖：
  - 建连；
  - 各类型的文本值，包括 NULL、enum、json，以及 ISO 格式的 timestamptz；
  - 用 `pg_sleep` 触发 `Cancel`，确认能取消。
- [ ] `password_cmd`、`password_env`、`~/.pgpass` 三种方式都能连上。
- [ ] 密码错误时显示数据库返回的错误，不 panic。
- [ ] `connections.toml` 中写有明文密码且权限为 0644 时，出现警告。

## F1.2 catalog 与 schema 树 · 状态：draft

- **依赖**：F1.1
- **涉及**：`internal/db`（catalog）、`internal/ui`（tree、`match.go`）

**开发**
- [ ] catalog 查询（§8.4）：
  - 表列表及行数估计、列属性、主键、非空唯一索引、枚举值；
  - 按 session 缓存，提供刷新命令。
- [ ] `match.go`（§9.7）：封装 fzf 的 `src/algo`，支持 smartcase 和扩展语法的常用部分，附单元测试。
- [ ] schema 树（§7.8 的样式）：
  - 显示表列表、行数量级（如 `2.1m`、`48k`），高亮当前打开的表；
  - 按键：`j` / `k` 移动；`/` 过滤并高亮匹配字符；`↵` 在当前 tab 打开，`t` 在新 tab 打开；
  - 鼠标：单击在当前 tab 打开，中键在新 tab 打开；支持滚轮和悬停高亮。
- [ ] 切换树的 schema：在树里按 `gs`，或点击树的第一行，打开 schema 下拉框。这个下拉组件以后 console 的 schema 选择（§8.6）会复用。

**验收**
- [ ] 集成测试：catalog 返回的主键、唯一索引、枚举、可空、默认值都与 seed 一致。
- [ ] e2e：
  - 树列出 seed 中 `public` 下的表，行数量级格式正确；
  - `/` 过滤后高亮匹配字符；
  - 用 `↵`、`t` 和鼠标都能打开表（data pane 标题显示表名）；
  - 用 `gs` 切换到 `agentable` 后，列出该 schema 下的表。
- [ ] `match.go` 的单测覆盖：排序结果、高亮位置、扩展语法。

## F1.3 data pane 表格（只读） · 状态：draft

- **依赖**：F1.2
- **涉及**：`internal/ui`（grid）、`internal/app`（DataTab）、`internal/db`

**开发**
- [ ] 取数：
  - 查询语句为 `SELECT * FROM "schema"."table" ORDER BY <行标识列> LIMIT n OFFSET m`；
  - 走 Meta 连接和扩展协议；用 seq 丢弃过期的响应；
  - 行标识列按 §10.1 判定，没有行标识列时不排序。
- [ ] 表格渲染（§7.6）：
  - 复用 F0.9 的 grid 组件和网格样式，把占位数据换成真实数据；
  - 只渲染可见区域（虚拟滚动），列宽按列宽算法计算；
  - 数值右对齐；NULL 显示为暗色的 `<null>`；过长内容截断为 `…`；
  - 主键列显示图标；显示行号，当前行的行号为绿色；
  - 单元格光标在 pane 聚焦时为蓝底，失焦时变暗；当前行有底色。
- [ ] 移动：
  - 键盘：`hjkl`、`gg`、`G`、`0`、`$`，支持次数前缀，支持 `[map.grid.*]` 映射；
  - 滚轮：纵向滚轮纵向滚动，每格 3 行（§7.4）；Shift+滚轮和横向滚轮横向滚动。
- [ ] 转置 `T`（G-05）。
- [ ] 状态栏显示光标的行号和列号（行,列）。
- [ ] 查询执行中显示忙碌提示；按 `C-c` 或点击提示可取消查询。

**验收**
- [ ] e2e：
  - 打开 seed 中的表后，显示第一页数据；
  - 各种类型的值显示正确；
  - 光标移动后，状态栏的行,列与光标一致；
  - 转置后光标位置保持不变；
  - 配置 `[map.grid.normal] L = "5l"` 后，按 `L` 右移 5 列。
- [ ] 长查询（例如 WHERE 中带 `pg_sleep(3) is not null`）执行时显示忙碌提示；按 `C-c` 后查询被取消，并有提示。
- [ ] 表格渲染的 golden 测试通过（160×45，固定数据）。

## F1.4 查询条 · 状态：draft

- **依赖**：F1.3
- **涉及**：`internal/ui`（querybar、dropdown）、`internal/app`

**开发**
- [ ] WHERE 输入框（§9.6）：
  - 在表格中按 `/` 聚焦输入框，进入 INSERT 模式；`↵` 执行，`esc` 回到表格；
  - 输入是完整的条件表达式，拼接时外层加括号；
  - 只允许一条语句，依靠扩展协议拒绝多语句。
- [ ] 单行输入：WHERE 输入框和 F0.4 的 `:` 命令行共用同一套输入处理。
  - 退格和光标移动都按字素簇处理，与 F0.4 的命令行一致；
  - 显示光标；内容超出可用宽度时，保持光标所在的位置可见（M0 的命令行看不到光标，超出宽度时末尾会被截掉，tester 在 F0.5 中提出，放到这里一起做）。
  - 据 reviewer 了解，bubbles 的 textinput 按 rune 处理退格。用它之前先写测试确认；不满足就在 F0.4 命令行的输入处理上扩展，同时从 tech-design §4 的依赖里去掉 bubbles。
- [ ] 四个 chip：
  - ORDER（`go`）：选择排序列和方向；
  - LIMIT（`gl`）；
  - PAGE（`gp`）：显示为 `当前页/总页数`，`]` / `[` 翻页；
  - COLS（`gc`）：打开下拉框，按 Q-04 实现过滤、`space` 勾选、`a` / `A` 全选与全不选、esc 分两步（先清空过滤，再关闭）。
- [ ] 计数（§8.5）：
  - 在 Meta 连接上异步执行，超过 3 秒显示 `?`；
  - 没有 WHERE 且估计行数超过 100 万时，显示估计值 `~n`。
- [ ] 查询条右侧（Q-06）：
  - 显示事务模式（M1 固定为 auto）、返回行数、耗时；
  - `R` 刷新；转置按钮可以使用，保存按钮在 M2 之前先占位。

**验收**
- [ ] e2e：
  - WHERE 能正确过滤数据；表达式写错时，显示数据库返回的错误；
  - 输入 `1=1; drop table x` 会被拒绝，执行后表仍然存在。
- [ ] WHERE 输入框和命令行：输入 é（e+U+0301）或 👍🏽 后按一次退格，整个字删掉；光标可见；输入超出宽度后，正在输入的位置仍然可见。
- [ ] ORDER / LIMIT / PAGE / COLS 的按键和点击行为都正确；翻页后数据正确；COLS 的过滤高亮、全选、全不选都正确。
- [ ] 大表的计数显示为 `~n`；计数超时显示 `?`。
- [ ] 查询条的 golden 测试通过。

## F1.5 WHERE 补全与历史 / 收藏 · 状态：draft

- **依赖**：F1.4
- **涉及**：`internal/sqlkit`（补全上下文的 WHERE 部分）、`internal/ui`（补全列表、WHERE 下拉）、`internal/config`（state.json）

**开发**
- [ ] WHERE 补全（§9.7）：
  - 候选：列名（附类型）和关键字；在 `列 =`、`列 <>`、`列 in (` 之后，给出枚举列和布尔列的可选值；
  - 输入时自动弹出；`C-n` / `C-p` 或 `↑` / `↓` 移动；`Tab` 接受；
  - `↵` 只在明确选中过某个候选时才接受；esc 分两步；支持鼠标。
- [ ] `C-r` 打开历史 / 收藏下拉框（Q-02）：
  - 分成「收藏」和「历史」两组，按当前输入模糊过滤；
  - `C-n` / `C-p` 移动，`↵` 应用，`C-f` 收藏或取消收藏，esc 关闭；
  - 每一项右侧显示附带的排序 / limit，或者时间。
- [ ] `state.json`（§14）：保存每张表的 WHERE 历史和收藏；写入是原子的；文件权限为 0600。

**验收**
- [ ] e2e：
  - 输入 `sta` 时出现 `status` 候选，匹配字符高亮，按 `Tab` 接受；
  - 输入 `status = ` 后，出现该枚举列的可选值；
  - 没有选中候选时，`↵` 直接执行查询。
- [ ] 执行过的 WHERE 会进入历史；收藏的条件在重启后仍然存在；`C-r` 下拉的模糊过滤正确。
- [ ] `state.json` 的权限为 0600。

## F1.6 data pane 的 tab · 状态：draft

- **依赖**：F1.3，可以与 F1.4、F1.5 交错开发
- **涉及**：`internal/app`（tab）、`internal/ui`（tabbar）

**开发**
- [ ] 每个 tab 对应一张表，各自保留 WHERE、ORDER、LIMIT、PAGE、COLS、光标位置和转置状态（T-01~T-03）。
- [ ] `gt` / `gT` 切换 tab；`x` 关闭 tab（M1 中没有修改，直接关闭）；点击 tab 切换；点击 `+` 时聚焦 schema 树的过滤框，选中的表在新 tab 中打开。
- [ ] tab 栏：当前 tab 标 `*`，上一个 tab 标 `-`；右侧显示键位提示（T-03）。

**验收**
- [ ] e2e：
  - 打开两张表各一个 tab，两者的状态互不影响；
  - `gt` / `gT`、`x`、点击都能正确切换或关闭 tab；
  - 点击 `+` 后聚焦树的过滤框，选中的表在新 tab 中打开。
