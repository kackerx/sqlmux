# sqlmux 技术方案（索引）

> 对应 [PRD - DB TUI v0.0.2](https://claude.ai/design/p/e6118404-bf7a-4420-abfd-e31198df2ba8?file=PRD+-+DB+TUI+v0.0.2.dc.html) 与 [设计稿 DB TUI v0.0.2](https://claude.ai/design/p/e6118404-bf7a-4420-abfd-e31198df2ba8?file=DB+TUI+v0.0.2.dc.html)；2026-10-04 起不再跟进设计稿，界面按现有实现的风格做（§18）。文中的需求编号（S-01、G-03、F-05 等）指 PRD 条目。
> 产品名：**sqlmux**。GitHub 上只有一个 2014 年后就没再更新的同名 Erlang 仓库，Homebrew 里没有同名软件。

设计与决策以这份方案为准：要改设计，先改这里，再写代码。方案按子系统分章，每章一个文件放在 `design/`，章节编号（§5、§7.8）在代码注释、task.md、e2e 和交接里到处引用，拆文件后编号不变，找一节用 `grep -rn '^### 7\.8' specs/design`。

**写法规则**（2026-10-10 起）：每一节只写**现在的行为**和它的理由；「原来是什么、哪一版改的、用户怎么说的」写在那个 feature 的 task.md 里（「细节（已定）」和「审查时定」），git log 也有。已有的改版说明逐步清掉，新的不再往里加。里程碑和推进规则见 [`plan.md`](plan.md)，协作流程见 [`../AGENTS.md`](../AGENTS.md)。

| 章 | 文件 | 小节 |
|---|---|---|
| §0 结论速览 | [`design/00-overview.md`](design/00-overview.md) | — |
| §1 范围 | [`design/00-overview.md`](design/00-overview.md) | — |
| §2 技术选型 | [`design/00-overview.md`](design/00-overview.md) | 2.1 选型记录：Go + Bubble Tea v2（已确认）；2.2 limoni 评估：不采用 |
| §3 总体架构 | [`design/00-overview.md`](design/00-overview.md) | — |
| §4 目录结构 | [`design/00-overview.md`](design/00-overview.md) | — |
| §5 核心数据模型 | [`design/05-model.md`](design/05-model.md) | — |
| §6 输入系统 | [`design/06-input.md`](design/06-input.md) | 6.1 Action；6.2 终端兼容：默认键位只用处处可用的键；6.3 键位记法；6.4 作用域与解析；6.5 按键序列、leader 与 which-key；6.6 次数前缀与用户映射；6.7 配置、冲突、提示、导出（PRD 6.3）；6.8 默认键位；6.9 非 vim 模式（M6 F6.6）；6.10 flash 式跳转（M10 F10.8） |
| §7 渲染与鼠标 | [`design/07-render.md`](design/07-render.md) | 7.1 Frame；7.2 Block；7.3 主题；7.4 命中表与鼠标（PRD 第 7 章）；7.5 半透明遮罩；7.6 表格；7.7 图标；7.8 默认尺寸与样式（取自设计稿）；7.9 文本输入：删词与自动配对（M2/M3 验收时定） |
| §8 数据访问 | [`design/08-data.md`](design/08-data.md) | 8.1 接口；8.2 连接模型；8.3 取消、超时、过期响应；8.4 元数据；8.5 分页与计数；8.6 PG 的 schema 与 search_path；8.7 键值引擎（Redis，M16）；8.8 外部执行器；8.9 ClickHouse；8.10 Trino |
| §9 SQL 处理（`internal/sqlkit`） | [`design/09-sqlkit.md`](design/09-sqlkit.md) | 9.1 词法扫描器；9.2 分句、可执行标记与执行前高亮（C-01、C-02）；9.3 读写判定；9.4 自动 LIMIT（F-05）；9.5 格式化（C-03 gq）；9.6 WHERE 条件（Q-01）；9.7 模糊匹配与补全（query、WHERE）；9.8 Redis 控制台的词法与补全（M16） |
| §10 单元格编辑与提交（G-02、G-03） | [`design/10-edit.md`](design/10-edit.md) | 10.1 进入与记录；10.2 按列属性提供选项；10.3 保存（C-s / `:w`）；10.4 刷新（R）；10.5 未保存修改的确认框；10.6 新增与删除行（M2/M3 验收时定）；10.7 字段的前置校验（M2/M3 验收时定） |
| §11 Query 与 result | [`design/11-query.md`](design/11-query.md) | — |
| §12 命令面板与快速 SQL（K、F） | [`design/12-palette.md`](design/12-palette.md) | — |
| §13 安全 | [`design/13-security.md`](design/13-security.md) | — |
| §14 配置与持久化 | [`design/14-config.md`](design/14-config.md) | — |
| §15 测试 | [`design/15-testing.md`](design/15-testing.md) | — |
| §17 待定问题 | [`design/16-prd.md`](design/16-prd.md) | — |
| §18 需要同步到 PRD / 设计稿的改动 | [`design/16-prd.md`](design/16-prd.md) | — |
