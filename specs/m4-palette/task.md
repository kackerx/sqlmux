# M4 命令面板与快速 SQL · 任务清单

- **状态**：todo。M3 第二轮改进（F3.25–F3.38）通过后直接开工，不等验收（用户 2026-09-30 决定）；M4 完成后 M2、M3、M4 一起验收。
- **目标**：补齐命令面板剩下的部分：快速 SQL 接上 M3 的读写判定、结果区和 console，表的 DDL 预览，以及窗口·Pane 范围里的 tab 与层级（用户 2026-09-30 提出）。
- **调整**：
  - 面板本身，以及命令、表、pane、window 四个范围，已在 M0 的 F0.13、F0.14 做完；快速 SQL 的执行、历史、`C-y`、已修改提示在 M1 F1.7；补全在 M3 F3.10 起改用 `CompletionContext`。
  - 会话范围 `$` 挪到 M5：M4 只有一个 session，`↵` attach 没有可切换的对象，和 M5 的多 session 一起做更自然。M4 的面板保持 5 个范围标签。
- **范围**：
  - PRD：K-04、K-05、F-01~F-05
  - tech-design：§12
- **依赖**：M3 全部 passed（F3.5 sqlkit、F3.8 执行与结果区）。
- **完成标准**：
  - F4.1–F4.3 全部 passed；
  - e2e 回归全部通过；
  - 用户与 M2、M3 一起验收通过后，打 tag `m2`、`m3`、`m4`。
- **审查节点**：F4.3 之后，F4.1–F4.3 一批。

任务文件的格式和状态约定见 [`../plan.md`](../plan.md)。

---

## F4.1 快速 SQL 接上 M3 · 状态：draft

- **依赖**：F3.5、F3.8
- **涉及**：`internal/app`（quick、palette、结果区）、`internal/keymap/default.toml`

**开发**
- [ ] 读写判定换掉 `SelectLike`（M1 F1.7 用 `ponytail:` 标着）：`sqlkit.IsRead` 为假就不执行，结果区显示 `warn` 色的「写语句不在这里执行 · C-e 在 console 中打开」，键位文字从 keymap 读（§12「写语句」）。
- [ ] 限行：判为读、且首词是 select / with / table / values 的仍走游标 + `FETCH FORWARD 101`，不改用自动 LIMIT（§12「不让服务端算完整个结果集」）。
- [ ] `C-t`（SQL 范围、已有结果时）：结果作为固定 tab `quick #n` 进结果区，日志记一行，`R` 在 `Meta` 的只读事务里重跑，导出名 `quick-42.csv`，面板不关（§12「C-t 送到结果区」）。
- [ ] `C-e`（Action `quicksql.edit`，`[keys.palette]` 里已经绑了）：在目标 pane 按 `console.new` 的规则开 console，已有内容时追加到末尾、前面空一行，光标落在 SQL 第一行，关面板、聚焦 console（§12「C-e 在 console 中打开」）。

**验收**
- [ ] 集成测试：
  - 写语句不会被执行，数据不变；
  - `select` 最多返回 100 行，超出时显示 `100+`；
  - 在只读事务中，调用会写数据的函数时报错。
- [ ] e2e：
  - 输入 `;delete from t_log` 按 `↵`，出现黄色提示，按 `C-e` 后这条 SQL 出现在 console 里；
  - `with x as (delete from t_log returning *) select * from x` 也被判为写、不执行；
  - 查询后按 `C-t`，结果区出现固定的 `quick #n`，面板没关；在它上面按 `R` 能重跑；
  - `C-e` 打开的 console 已有内容时，SQL 追加在末尾。

## F4.2 DDL 预览 · 状态：draft

- **依赖**：F4.1
- **涉及**：`internal/app`（palette）、`internal/ui`（预览区）、`internal/db/postgres`（DDL）

**开发**
- [ ] 选中项在表或视图上停留 150ms 后从 `Meta` 取 DDL，按表缓存；`R`（`tree.refresh`）时和列缓存一起清掉。每次移动选中项就重新计时，只取停下来的那一项；取回时选中项已经变了，只放进缓存、不显示。
- [ ] PG 的 DDL 自己拼，先看 pgtui 的 `internal/db/metadata/`，提交说明里写明：
  - 表：`create table s.t (` 列（`format_type`、not null、default），约束用 `pg_get_constraintdef`，`);`，每个索引一行 `pg_get_indexdef`（主键和唯一约束已经带出来的索引跳过）；
  - 视图、物化视图：`pg_get_viewdef`。
  - MySQL 的 `SHOW CREATE TABLE` 在 M5。
- [ ] 画法：列表下方的预览区，用 console 的配色高亮；出错时显示 `error` 色的原文。高度取剩余空间、最多 12 行；窗口不够高时先压缩预览区，列表至少留 3 行，底栏始终显示（§12「预览」）。

**验收**
- [ ] 集成测试：t_order、复合主键的 t_event、只有唯一索引的 t_sku、视图、物化视图的 DDL 各对照一次，索引不重复。
- [ ] e2e：
  - 选中一张表停留片刻后，出现 DDL 预览；快速上下移动时不会取中间经过的每一项；
  - 窗口较小时，预览区被压缩，列表仍至少有 3 行。
- [ ] golden：带预览区的面板。

## F4.3 窗口·Pane 范围列出 tab 并显示层级 · 状态：todo

用户 2026-09-30 提出：用面板找打开着的表或 console 时，要能看出它在哪个 session 的哪个 window、哪个 pane、哪个 tab，和树里的工作区一样。

- **依赖**：F0.14、F3.19
- **涉及**：`internal/app`（palette 的窗口·Pane 范围）

**开发**
- [ ] `%` 范围除了 window、pane，每个打开着的 tab 也是一项（表、console、结果 tab），图标照 tab 的类型，名称是 tab 名，所在位置是完整的层级 `play › 0: data › pane-1`，和工作区节点的叫法一致（`pane-<n>`）；`↵` 切到这个 tab 并聚焦它的 pane（§12）。
- [ ] pane 项的所在位置也改成 `play › 0: data`，window 项是 `play`；排序、类型标签（tab 的标签叫「Tab」）由 worker 开工时提议。

**验收**
- [ ] e2e：① 开着 t_order、② 开着 console_1 时，`%t_or` 列出 t_order，所在位置是 `play › 0: data › pane-1`，`↵` 切过去。

