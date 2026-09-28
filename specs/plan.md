# sqlmux 开发计划（索引）

范围和设计以 [`tech-design.md`](tech-design.md) 为准。每个里程碑一个目录，目录里的 `task.md` 是这个里程碑的任务清单。协作流程见 [`../AGENTS.md`](../AGENTS.md)。

| 里程碑 | 任务清单 | 目标 | 状态 |
|---|---|---|---|
| M0 骨架 | [m0-skeleton/task.md](m0-skeleton/task.md) | 界面骨架、按键系统、鼠标、命令面板、主题；不连数据库 | 完成（tag `m0`，5b272d4） |
| M1 浏览 | [m1-browse/task.md](m1-browse/task.md) | 连接 PG，浏览 schema 与表数据，命令面板的快速 SQL；只读 | 完成（tag `m1`，c113b29） |
| M2 编辑 | [m2-edit/task.md](m2-edit/task.md) | 编辑单元格、选项浮层、保存与刷新 | 起草开发清单 |
| M3 console | [m3-console/task.md](m3-console/task.md) | vim 编辑器、执行、底部结果区、格式化、补全、schema 下拉 | draft |
| M4 命令面板 | [m4-palette/task.md](m4-palette/task.md) | 统一搜索入口、DDL 预览、快速 SQL | draft |
| M5 工作现场 | [m5-workspace/task.md](m5-workspace/task.md) | 多 session / window、只读与事务模式、MySQL | draft |
| M6 配置 | [m6-config/task.md](m6-config/task.md) | 键位配置收尾、主题、持久化 | draft |

## 任务文件的格式

每个 `task.md` 开头写清五项：目标、范围（PRD 编号与 tech-design 章节）、依赖、完成标准、**审查节点**。之后每个 feature 一节：

- **标题行**：`## <ID> <名称> · 状态：<状态>`。状态为 passed 时，在后面注明 commit sha 和 e2e sha。
- **依赖**：必须先完成的 feature。
- **涉及**：会改动的包或文件，用来判断改动范围、避免冲突。
- **开发**：worker 按顺序实现的清单。
- **验收**：tester 的测试项，每项都要能用命令、golden 或 e2e 脚本验证。写着「e2e」的验收项里，属于布局、颜色、位置的，可以由渲染 golden 验证（AGENTS.md「tester 的规则」）。

**审查节点**：worker 按 feature 提交，reviewer 不逐个审，只在节点上整批审，通过后整批提测。节点由决策者按工作量定：小的里程碑只在最后一个 feature 之后设一个；大的里程碑在中间合适的位置多设几个，让一批的改动量保持在一次能审完的范围内。

## 状态

| 状态 | 含义 |
|---|---|
| `draft` | 决策者起草，开发清单等 worker 补充 |
| `todo` | 决策者已确认，可以开工 |
| `passed（sha；e2e sha）` | 所在批次审查、测试都已通过，由决策者填写 |

不再有 doing、reviewing、testing、failed 这些中间状态：按批审查之后，它们只增加记账，不提供信息。

## 按步推进的规则

1. **按依赖顺序开发，不等审查。** worker 做完一个 feature 就提交，接着做下一个。
2. **每一步都保持测试通过。** 每个 commit 都由 worker 自己保证：vet、单元测试、golden、gofmt、`go mod tidy`、涉及数据库时的集成测试，以及和改动相关的 e2e 脚本。全量 e2e 每个审查节点跑一次，和送审同时进行；reviewer 等它跑完、没有清单外的失败，才提测（AGENTS.md「worker 的规则」）。
3. **到了审查节点：先整批审查，再整批测试。**
   1. worker 把 `<上一个节点>..<sha>` 发给 reviewer，同时告诉 tester，tester 开始准备，spec 问题在审查期间就提；
   2. reviewer 审查，有待定的 spec 问题先问决策者，再把必须改的问题一次退回；通过后整批提测给 tester；
   3. 审查或测试发现问题时，worker 修复后提交新的 commit，同样先交给 reviewer 复审，再交给 tester 复测；
   4. tester 全部通过后，把批次结论发给决策者和 worker。
   5. 修复 commit 排在下一批的 feature 之后时，照常在修复的 sha 上审、测，只看本批的验收项；后续 feature 造成的 e2e 失败由 worker 列出，不算本批的问题（AGENTS.md「固定的工作流」第 5 条）。
4. **谁改什么。**
   - `specs/` 和 `AGENTS.md` 只由决策者修改，并由决策者自己提交：`git commit -m "docs: …" -- specs AGENTS.md`。worker、reviewer、tester、pruner 都不改。
   - 收到 tester 的批次结论后，决策者把这批 feature 的状态填为 `passed`，注明 sha，并勾选开发清单和验收项。
   - worker 觉得清单不合理时，提给决策者。
5. **里程碑完成的流程。** 用户按里程碑验收，不逐个验收 feature。
   1. 最后一个审查节点通过，全部 feature 为 `passed`。
   2. tester 在最后一个 commit 上跑一遍完整的 e2e 回归。
   3. **暂停，由用户深度体验并验收。**
      - worker 停止写代码，只可以起草下一个里程碑的开发清单。
      - 决策者整理出体验路径：怎么运行、依次试哪些操作、应该看到什么。用户照着完整体验一遍，提出问题和改进意见。
   4. 决策者把用户的反馈整理成任务。
      - 属于当前里程碑的，追加成改进 feature，编号接着往后排，标好审查节点，照常按批走完。
      - 其余的排进后续里程碑。
   5. 修剪不是必经步骤。用户需要时自己启动 `sqlmux-pruner`（AGENTS.md「pruner 的规则」）。
   6. 用户确认验收通过后，worker 打 tag（`m0`、`m1` ……），决策者把本表的状态改为「完成」，然后进入下一个里程碑。
6. **下一个里程碑开工前**，worker 起草该里程碑的开发清单发给决策者，决策者写进 task.md、定好审查节点，状态从 `draft` 改为 `todo`。
7. **消息和会话**：只发 spec 问题、退回、批次提测、批次结论四类消息；上下文到 700–800k token 时在原会话里 `/handoff` + `/clear`，名字不变（AGENTS.md「消息规则」「会话与交接」）。
