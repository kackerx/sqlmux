# sqlmux 开发计划（索引）

范围和设计以 [`tech-design.md`](tech-design.md) 为准。每个里程碑一个目录，目录里的 `task.md` 是这个里程碑的任务清单。协作流程见 [`../AGENTS.md`](../AGENTS.md)。

| 里程碑 | 任务清单 | 目标 | 状态 |
|---|---|---|---|
| M0 骨架 | [m0-skeleton/task.md](m0-skeleton/task.md) | 界面骨架、按键系统、鼠标、命令面板、主题；不连数据库 | 完成（tag `m0`，5b272d4） |
| M1 浏览 | [m1-browse/task.md](m1-browse/task.md) | 连接 PG，浏览 schema 与表数据，命令面板的快速 SQL；只读 | 进行中 |
| M2 编辑 | [m2-edit/task.md](m2-edit/task.md) | 编辑单元格、选项浮层、保存与刷新 | draft |
| M3 console | [m3-console/task.md](m3-console/task.md) | vim 编辑器、执行、底部结果区、格式化、补全、schema 下拉 | draft |
| M4 命令面板 | [m4-palette/task.md](m4-palette/task.md) | 统一搜索入口、DDL 预览、快速 SQL | draft |
| M5 工作现场 | [m5-workspace/task.md](m5-workspace/task.md) | 多 session / window、只读与事务模式、MySQL | draft |
| M6 配置 | [m6-config/task.md](m6-config/task.md) | 键位配置收尾、主题、持久化 | draft |

## 任务文件的格式

每个 `task.md` 开头写清四项：目标、范围（PRD 编号与 tech-design 章节）、依赖、完成标准。之后每个 feature 一节：

- **标题行**：`## <ID> <名称> · 状态：<状态>`。状态为 passed 时，在后面注明 commit sha。
- **依赖**：必须先完成的 feature。
- **涉及**：会改动的包或文件，用来判断改动范围、避免冲突。
- **开发**：worker 逐项勾选的清单，按顺序做。
- **验收**：tester 的测试项，每项都要能用命令或 e2e 脚本验证。

## 状态

| 状态 | 含义 |
|---|---|
| `draft` | 决策者起草，开发清单等 worker 补充 |
| `todo` | 决策者已确认，可以开工 |
| `doing` | 开发中 |
| `reviewing` | 已提交，等待 reviewer 审查 |
| `testing` | 审查通过，已提测，等待 tester 测试 |
| `passed` | 测试通过 |
| `failed` | 审查或测试发现问题；修复后回到 `reviewing` |

## 按步推进的规则

1. **按依赖顺序开发。** 依赖的 feature 只要已经提交（状态为 `reviewing` 或之后），就可以开工；如果依赖在审查或测试中发现问题，先修依赖。
2. **每一步都保持全绿。** 每个 commit 都要让已有测试全部通过：单元测试、golden 测试，以及 worker 合入 `e2e` 分支后的 e2e 回归。
3. **一个 feature 完成后：先审查，再测试。**
   1. 开发清单全部勾选、单元测试通过后提交，把 commit sha 发给 reviewer；
   2. reviewer 审查通过后，提测给 tester；
   3. 审查或测试发现问题时，worker 修复后提交新的 commit。新 commit 同样要先交给 reviewer，再交给 tester。
4. **谁改什么。**
   - 开发清单由 worker 勾选。
   - 验收项由 tester 测试。tester 报告通过后，worker 勾选验收项，把状态改为 `passed`，并注明 commit sha。
   - reviewer 和 tester 都不改 `task.md`。
   - 范围和验收标准只由决策者修改。worker 觉得清单不合理时，提给决策者。
5. **里程碑完成的流程。**用户按里程碑验收，不逐个验收 feature。
   1. 全部 feature 为 `passed`。
   2. tester 在最后一个 commit 上跑一遍完整的 e2e 回归。
   3. **暂停，由用户深度体验并验收。**
      - worker 停止写代码，只可以起草下一个里程碑的开发清单。
      - 决策者整理出体验路径：怎么运行、依次试哪些操作、应该看到什么。用户照着完整体验一遍，提出问题和改进意见。
   4. 决策者把用户的反馈整理成任务。
      - 属于当前里程碑的，追加成改进 feature，编号接着往后排，照常走「审查 → 测试」。
      - 其余的排进后续里程碑。
   5. 改进项全部完成后，**修剪一轮**：新开 `sqlmux-pruner` 会话，按 AGENTS.md「pruner 的规则」对整个仓库做一次不改变行为的精简。每个 commit 都要经过 reviewer，全部做完后 tester 在最后一个 commit 上跑完整回归。
   6. 用户确认验收通过后，worker 打 tag（`m0`、`m1` ……），决策者把本表的状态改为「完成」，然后进入下一个里程碑。
6. **下一个里程碑开工前**，worker 补充该里程碑的开发清单，发给决策者确认后，状态从 `draft` 改为 `todo`。
7. **specs 的提交。** feature commit 不包含 `specs/` 和 `AGENTS.md` 的改动，但 worker 自己改的任务状态可以一起提交。决策者改完文档后会通知 worker，由 worker 单独提交一个 `docs:` commit。
