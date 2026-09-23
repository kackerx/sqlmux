# M3 console 与结果区 · 任务清单

- **状态**：draft。开工前由 worker 补充开发清单，经决策者确认后改为 todo。
- **目标**：
  - vim 编辑器写 SQL；
  - 执行语句，结果显示在底部结果区；
  - 支持格式化、补全和 schema 选择。
- **范围**：
  - PRD：C-01~C-07
  - tech-design：§8.6、§9.1–§9.5、§9.7、§11
- **依赖**：
  - F3.1–F3.3 只依赖 M0，可以提前开发，但要排在当前里程碑的 feature 之后，并且不能跨过里程碑结束后的暂停；
  - 其余 feature 依赖 M1。
- **完成标准**：
  - F3.1–F3.8 全部 passed；
  - 编辑器的差分测试与 nvim 的结果完全一致；
  - 用户验收通过后，打 tag `m3`。

任务文件的格式和状态约定见 [`../plan.md`](../plan.md)。

---

## F3.1 编辑器核心与 nvim 差分测试 · 状态：draft

- **依赖**：M0
- **涉及**：`internal/editor`

**内容**
- 实现 §11「支持的操作」表中的全部内容，块选择除外。
- 按 vim 的语法组织：`[次数] 操作符 [次数] 移动或文本对象`。
- 撤销以「一条 NORMAL 命令」或「一次完整的 INSERT」为一步；`U` 撤销整行的修改。
- 无名寄存器与系统剪贴板同步。
- 搜索使用 RE2 语法，采用 smartcase。
- 支持 `:s` 和 `:{n}`。
- 差分测试框架：
  - 用例写在 `testdata/cases.txt`；
  - `go generate` 调用 `nvim --headless --clean` 执行用例（按键用 `silent!` 执行），生成 golden 文件；
  - 生成的 golden 文件提交进仓库。

**验收**
- [ ] 差分用例至少 200 条，覆盖每一类操作，结果与 nvim 全部一致。
- [ ] 运行 `go test ./internal/editor` 不需要安装 nvim。

## F3.2 块选择（VISUAL BLOCK） · 状态：draft

- **依赖**：F3.1
- **涉及**：`internal/editor`

**内容**

实现 §11 表格中「VISUAL BLOCK」一行及其补充说明的全部内容：

- `I`、`A`、`$A`、`c`：先在第一行输入，按 esc 后复制到其余各行；
- `d`、`x`、`y`；`y` 复制时，寄存器类型为块；
- `p`、`P`：按块粘贴；
- `r`、`~`、`u`、`U`、`>`、`<`、`gc`；
- `o`、`O`：切换选区的对角。

**验收**
- [ ] 差分用例覆盖上面列出的全部操作，结果与 nvim 一致，其中包括块边界落在 Tab 上或宽字符中间的情况。

## F3.3 sqlkit：词法分析与分句 · 状态：draft

- **依赖**：M0
- **涉及**：`internal/sqlkit`

**内容**

§9.1–§9.4 的全部内容：

- PG 和 MySQL 两种方言的词法分析；
- 分句，并找出光标所在的语句；
- 判断语句是读还是写；
- 自动添加 LIMIT；
- 检查语句顶层是否有 `;`，这个检查接入到 WHERE 的校验中。

写实现之前，先参考 AGENTS.md 表格中列出的 lazysql 的对应文件。

**验收**
- [ ] 表驱动的单元测试覆盖以下情况：
  - `$tag$…$tag$`、`E'…'`、可嵌套的 `/* */` 注释；
  - MySQL 的反引号、`#` 注释、`-- `（后面必须跟空格才算注释）；
  - 包含写语句的 CTE；
  - `EXPLAIN ANALYZE` 后面跟写语句；
  - 语句已有 LIMIT 或 FETCH 时，不再追加 LIMIT。

## F3.4 console pane · 状态：draft

- **依赖**：F3.1、F3.3、M1
- **涉及**：`internal/ui`（console）、`internal/app`（ConsoleTab）

**内容**
- **渲染**：行号、gutter 中的 ▶、语法高亮；执行前高亮将要执行的语句范围（§9.2）。
- **鼠标**：点击定位光标；拖动选择文本并进入 VISUAL；滚轮滚动。
- **文件**：
  - 自动保存（1s）；`:w`、`C-s` 立即写入；
  - 保存路径在 `$XDG_DATA_HOME/sqlmux/consoles/` 下。
- **其他**：
  - 支持 `[map.console.*]` 映射；
  - 提供「Edit in $EDITOR」命令；
  - 粘贴使用 bracketed paste；
  - 编辑器的模式同步到状态栏。

**验收**
- [ ] e2e：
  - 输入 SQL 后，高亮颜色正确；每条语句的第一行显示 ▶；光标所在语句的范围被高亮；
  - 拖动能选中文本；
  - 文件保存到隔离后的 `XDG_DATA_HOME` 下；
  - 在 NORMAL 下粘贴，内容作为文本插入。

## F3.5 执行与结果区 · 状态：draft

- **依赖**：F3.4
- **涉及**：`internal/app`（结果区、日志）、`internal/ui`（结果 tab、工具行）、`internal/db`

**内容**
- **执行方式**：按 `↵` 执行光标所在语句或选区；也可以点击 ▶ 或 run 按钮。
- **执行中**：显示忙碌状态，`C-c` 取消。
- **结果区**：实现 §11「结果区」的全部内容：
  - 位于底部，占满宽度；
  - 日志 tab；
  - 每个 console 有自己的结果 tab，再次执行时复用，序号递增；
  - 多个结果集分别显示；非查询语句只写进日志；
  - 执行期间显示占位；
  - `P` 固定，`q` 关闭，`gt` / `gT` 切换；
  - 执行后焦点留在 console；
  - 工具行上有按钮；
  - 结果表格只读，复用 grid 组件。
- **截断**：超过 `max_rows` 时截断，并给出提示。
- **出错**：切换到日志 tab，console 里出错语句的 ▶ 变为红色。
- **配置**：支持 `result_split = console`。

**验收**
- [ ] e2e：
  - 第一次执行时，在底部出现结果区；
  - 再次执行时替换原有的结果 tab，序号加 1；
  - 按 `P` 后再执行，会新开一个 tab；
  - 选中多条语句执行时，产生多个结果 tab；
  - 执行 DDL / DML 只写进日志；
  - 执行出错时，切到日志 tab，并出现红色标记；
  - 返回 1001 行的查询被截断，并有提示。

## F3.6 格式化 · 状态：draft

- **依赖**：F3.4
- **涉及**：`internal/sqlkit`（format，embed sql-formatter）

**内容**

§9.5 的全部内容：

- 通过 goja 运行 sql-formatter，打包文件用 embed 嵌入；
- 加载前替换 Unicode 属性名；
- 第一次使用时才初始化，调用时加互斥锁；
- 支持 `keyword_case`、`tab_width`、`formatprg` 配置；
- 格式化失败时给出 toast 提示，缓冲区保持不变。

**验收**
- [ ] 格式化的 golden 测试通过，PG 和 MySQL 各准备若干条语句。
- [ ] 第一次执行 `gq` 的耗时小于 300ms。
- [ ] 配置 `formatprg` 后，改用外部命令格式化。
- [ ] 格式化失败时，缓冲区内容不变。

## F3.7 SQL 补全（console） · 状态：draft

- **依赖**：F3.3、F3.4、F1.2
- **涉及**：`internal/sqlkit`（补全上下文）、`internal/ui`（补全列表）

**内容**

§9.7 中 console 的部分：

- 识别补全的上下文：FROM / JOIN / UPDATE / INTO / TABLE 之后、别名、CTE、子查询层级、`schema.` 前缀；
- 按候选组和 fzf 得分排序；
- 交互方式与 WHERE 的补全一致。

**验收**
- [ ] 上下文判断有单元测试，可以参考 lazysql 的 `sql_context_test.go`。
- [ ] e2e：
  - 输入 `select * from t_o` 时，候选中出现 `t_order`；
  - 输入别名加 `.` 后，列出这张表的列；
  - CTE 的名字会出现在候选中。

## F3.8 schema 下拉框（PG） · 状态：draft

- **依赖**：F3.4、F3.5
- **涉及**：`internal/app`（ConsoleTab.Schema）、`internal/ui`（复用 F1.2 的下拉组件）

**内容**

§8.6 的全部内容：

- console 标题右侧显示 `db.schema ▾`，按 `gs` 打开下拉框，下拉框中可以过滤；
- 执行前，如果连接当前的 search_path 与 console 选择的 schema 不一致，先执行 `SET search_path`；
- 新 console 默认使用 schema 树当前的 schema；
- MySQL 的 console 不显示这个下拉框。

**验收**
- [ ] 集成测试与 e2e：
  - console 选择 `agentable` 后，`select * from <agentable 中的表>` 能执行成功；
  - 两个 console 分别选择不同的 schema，交替执行，结果都正确。
