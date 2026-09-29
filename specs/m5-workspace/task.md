# M5 工作现场与 MySQL · 任务清单

- **状态**：draft。开工前由 worker 补充开发清单，经决策者确认后改为 todo。
- **目标**：
  - 支持多个 session、多个 window；
  - 支持只读 session 和两种事务模式；
  - 支持 MySQL。
- **范围**：
  - PRD：S-01~S-04、W-01~W-02、第 9 章中的 MySQL
  - tech-design：§8（MySQL 部分）、§13
- **依赖**：M1–M4 全部 passed。
- **开工前要先改设计**（M2/M3 验收时用户提出，2026-09-29）：
  - **一个 session 里可以有多个连接**，像 DataGrip 的多个数据源（用户给的截图：`D 2/5`、`D`、`D@PROD`）。现在 §5 是「一个 session 对应一个连接」，M5 开工前要重写 §5 的数据模型：目录树的根变成各个连接，每个连接各自有 `Main` / `Meta` 两条连接；console 标题右上角除了 schema，还要能选用哪个连接；表 tab 属于它来自的那个连接。session 本身变成「工作区」的概念。
  - **console 的事务模式与提交 / 回滚**放在 F5.3，界面照 DataGrip：console 标题上有 `Tx: 自动 ▾` 下拉（自动 / 手动），手动时旁边出现提交 ✓、回滚 ↺ 两个按钮（用户给的截图）。
- **完成标准**：
  - F5.1–F5.4 全部 passed；
  - 前面各阶段的集成测试在 MySQL 上也全部通过；
  - 用户验收通过后，打 tag `m5`。
- **审查节点**：
  1. F5.3 之后：F5.1–F5.3，多 session、多 window、只读与事务模式；
  2. F5.4 之后：MySQL 单独一批。

任务文件的格式和状态约定见 [`../plan.md`](../plan.md)。

---

## F5.1 多 session · 状态：draft

- **session 列表浮层**：支持 S-02、S-03 规定的全部按键（`j/k`、`l/h`、`↵`、`n`、`x`、`$`）。
- **新建连接**：通过表单新建，结果写入 `connections.toml`。
- **管理 session**：attach、关闭、重命名；点击状态栏的 session 块也能打开列表。
- **命令面板的会话范围 `$`**（从 M4 挪来，§12）：列出 session，`↵` attach。

**验收**
- [ ] e2e：
  - 新建连接后，`connections.toml` 中出现这条连接，且不会破坏文件中已有的内容；
  - 在两个 session 之间切换后，各自的界面状态保持不变。

## F5.2 多 window · 状态：draft

- **按键**：`SPC n` / `SPC p` / `SPC l` 切到下一个 / 上一个 / 上次用的 window，`SPC c` 新建；重命名、关闭默认不绑键，在命令面板里执行（§6.8）。不提供按编号切换 window。
- **鼠标**：点击状态栏上的 window 列表可切换。
- **独立性**：每个 window 有自己的布局和结果区。

**验收**
- [ ] e2e：
  - 新建、切换、重命名、关闭 window 都正确；
  - 各 window 的布局互不影响。
- [ ] 补回 M0 里因 F1.1 去掉第二个 window 而删掉的 3 项 e2e（tester 在 F1.1 结论里列出，2026-09-24）：f0.5 其余 window 为 dim 字、无底色；窄宽度下省略非当前 window（含省略顺序单调性检查里的这一环）；f0.14 window 范围 `↵`（`%report`）。

## F5.3 只读 session 与事务模式 · 状态：draft

- **只读**（S-04）：应用层拦截与数据库层设置两层同时生效；SET 语句按 §13 的规则区分放行和拦截。
- **只读 session 禁止编辑单元格**（§10.1）：M2 没做，随 `connections.toml` 的 `read_only` 一起加（M1 F1.1 时去掉了这个字段）。
- **事务模式**：在 auto 和 manual 之间切换；提供 Commit 和 Rollback 命令；manual 模式下保存修改不自动提交。有未提交的事务时，退出前需要确认。
- **console 标题上的事务控件**（M2/M3 验收时用户要求，照 DataGrip）：`Tx: 自动 ▾` 下拉切换自动 / 手动；手动时旁边显示提交 ✓、回滚 ↺ 按钮，点击即执行，也有对应的命令。M3 已知上限「在 console 里自己 begin 之后不提交」在这里一并处理：状态栏显示事务中。
- **状态显示**：状态栏和查询条显示当前事务模式。

**验收**
- [ ] 集成测试：
  - 只读 session 中，写语句被拦截；
  - 解除只读的 SET 被拦截，`SET search_path` 等可以执行。
- [ ] e2e：manual 模式下保存后不提交；执行 Rollback 后修改被撤销。

## F5.4 MySQL · 状态：draft

- **驱动**：对应 §8.1–§8.3 中 MySQL 的部分：
  - 走文本协议，开启 `interpolateParams`；
  - 取消执行用 `KILL QUERY`。
- **catalog**：取 §8.4 表格中 MySQL 那一列的数据：
  - `TABLE_ROWS`、`STATISTICS`；
  - 从 `COLUMN_TYPE` 解析出 enum 的可选值；
  - DDL 用 `SHOW CREATE TABLE` 获取。
- **方言**：
  - 标识符用反引号；
  - 空值安全比较用 `<=>`；
  - 布尔值写入 1 / 0。
- **种子数据**：`testdata/seed/mysql.sql`，覆盖的情况与 PG 的种子数据相同。
- **扫描器**：MySQL 会执行的注释 `/*! … */`（如 `SELECT 5 /*! +1 */` 返回 6，dump 开头的 `/*!40101 SET NAMES utf8 */;`）M3 的扫描器当作注释，分句和读写判定会错；M5 接 MySQL 时处理（M3 审查时发现）。

**验收**
- [ ] M1–M4 的集成测试在 MySQL 上全部通过。
- [ ] 用 `KILL QUERY` 取消长查询有效。
