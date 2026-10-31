# M20 Redis 键的模糊匹配与界面一致性 · 任务清单

- **状态**：**todo**（2026-10-31 决策者起草；用户 2026-10-30 / 31 提，v0.8.0 发版后用户说直接进 M20）。
- **目标**：Redis 键名能像 SQL 表名一样模糊补全、模糊搜到、看到高亮；打开树不再乱动光标；编辑器能直接复制到系统剪贴板；间隙和边框的颜色一致、可配；用户的 ristretto 成为内置主题。发 v0.9.0。
- **范围**：§9.8「键名的模糊补全」、§7.8「Redis 的树」（过滤高亮）、§12「Redis 键」、§7.8「`tree.toggle`」和 §6 的 `SPC b`（树光标）、§11「寄存器」（`clipboard`）、§7.3 主题 token `border` / `border_focus` / `gap` 和内置主题、§7.8「整体布局」、§14（`index_keys`、`[tree] follow`、`clipboard`）。都已写进各章（2026-10-31）。
- **依赖**：M19 完成（v0.8.0）。
- **完成标准**：四个节点全过；完整回归绿；用户体验验收后 worker 打 `m20` 和 `v0.9.0`，照 AGENTS.md 两步推送。
- **审查节点和分工**（AGENTS.md「并行开发」）：1 = F20.1；2 = F20.2 + F20.3；5 = F20.8（用户 2026-10-31 加的增量索引和后台匹配，排在节点 2 之后）—— 这三个节点是 worker 的，在 `main` 上做（三条都是 Redis，F20.3 依赖 F20.1 的索引）。3 = F20.4 + F20.5；4 = F20.6 + F20.7 —— **这两个节点由 `sqlmux-fullstack-dev-2` 在 `/Users/ctw/proj/sqlmux-dev2` 的分支 `f20-ui` 上做**，从决策者起这份清单的那个 commit 拉；节点 3 过了就由 worker `merge --no-ff` 进 `main`，节点 4 同样，dev-2 接着在合过之后的 `main` 上 `merge main` 继续。边界：`internal/sqlkit` 的匹配器、`internal/db/redis`、Redis 的树和补全、面板归 worker；`internal/editor`、主题（`internal/ui/theme.go` 和内置主题文件）、布局的间隙、树光标归 dev-2。两边都会往 config 的结构里加字段（`[redis] index_keys` 是 worker 的，`[tree] follow`、`clipboard` 是 dev-2 的），各加各的、不改对方的；golden 两边都会动（F20.6 会改几乎所有 golden），合入照 AGENTS.md 重新生成。
- **不做（留候选）**：Redis 树过滤改成模糊（仍是服务端子串，树要看全库）；索引按键类型分图标（要对每个键发 `TYPE`）；exec 连接的索引；`border_focus` 以外的按状态配边框样式（聚焦用粗线、未聚焦用细线）；Windows 上的剪贴板读（照 M18 的工具表，不另做）。M21 的命令行两条和 issue #4 见 plan.md。

---

## F20.1 Redis 键名的模糊补全 · 状态：todo

- **依赖**：M16 F16.5（Redis 补全）、M12 F12.6（匹配和排序）
- **涉及**：`internal/app`（Redis 补全、键名缓存）、`internal/sqlkit`（词首规则）、`internal/config`（`index_keys`）、`internal/i18n`
- 用户 2026-10-30（截图）：`smembers i18n` 只出 `i18n:` 开头的，`i18n_agent:expert:…` 补不出，输 `expert` 也补不出；SQL 的 `tsk` 能出 `mt_task` 并高亮。

**开发**（§9.8「键名的模糊补全」）
- [ ] a. 每个 db 一份键名索引：第一次补全键名时在 `Meta` 上分批 SCAN 建，`index_keys` 封顶；树完整扫过的 db 直接用树的键。建好前照旧前缀 SCAN。失效规则沿用现有缓存的那一处。
- [ ] b. 补全在索引里走 §9.7 的匹配器和高亮；词首加 `:`（改在匹配器那一处）。
- [ ] c. 截断时补全框底部「已索引 N / 共 M 键」（i18n）。exec 连接不建索引。

**验收**
- [ ] 单测：建索引分批、封顶、用树的键不再扫；`i18n_`、`expert`、`fntfp` 这类输入在索引里的命中和高亮位置；`:` 是词首；写命令和 `R` 后重建；exec 不建；mutation。
- [ ] 集成测试（Redis 56379）：灌 2 万个键，建索引的 SCAN 批数和总数对；`index_keys = 5000` 时截断提示。
- [ ] e2e：`smembers expert` 补出 `i18n_agent:expert:…` 且 `expert` 是 `match` 色；`smembers i18n_` 补出带下划线的。

## F20.2 Redis 树过滤的高亮 · 状态：todo

- **依赖**：M18 F18.11
- **涉及**：`internal/app`（Redis 树的行）、`internal/ui`（树行的高亮，复用 SQL 树过滤那一处）
- 用户 2026-10-30（截图）：`kaixuan` 过滤出 8 个键，命中的字没有颜色。

**开发**（§7.8「Redis 的树」）
- [ ] a. 每个节点在自己那段名字里找输入文字，`match` 色；输入带 glob 字符不画；跨分隔符的不画。用 SQL 树过滤画高亮的同一个函数。

**验收**
- [ ] 单测：命名空间节点和键节点的高亮位置；`*` 输入不画；区分大小写；mutation。
- [ ] golden：过滤 `kaixuan` 的树。
- [ ] e2e：`/` 输入 `employees`，命名空间 `employees` 的字是 `match` 色。

## F20.3 命令面板的 `@` 列出 Redis 键 · 状态：todo

- **依赖**：F20.1
- **涉及**：`internal/app`（palette）、`internal/i18n`
- 用户 2026-10-30（截图）：`@p:employees` 没有结果。

**开发**（§12「Redis 键」）
- [ ] a. `@` 和「所有」的候选加上已建索引的 db 的键（加树已扫到的），和表、query 混排；标签「Key」、位置 `<连接>/db<n>`、图标按已知类型；`↵` 走树上开键 tab 的同一个函数；预览空。
- [ ] b. 面板不触发建索引。

**验收**
- [ ] 单测：建过索引的 db 的键出现在 `@`、没建过的不出现；`↵` 开键 tab / 聚焦已开的；输入时没有 SCAN 发出；10 万个键时一次过滤的耗时（基准，超过 30ms 在送审里说明）；mutation。
- [ ] e2e：先在 query 里补全一次键名（建索引），再 `@p:employees` 出键、`↵` 开键 tab。

## F20.4 打开树时光标不跳：`[tree] follow` · 负责：dev-2 · 状态：passed（832fb7c，分支 f20-ui；e2e e817cdf）

- **依赖**：M19 F19.5
- **涉及**：`internal/app`（tree.toggle 进树、点 `»`）、`internal/config`
- 用户 2026-10-30：焦点在表格时打开树会自动定位到那张表，打乱树的光标；只在手动上下时才动。用户 10-30 定：做成配置，默认不跳。

**开发**（§7.8「`tree.toggle`」、§6 `SPC b`、§14 `[tree] follow`）
- [ ] a. 进树时光标默认不动；`follow = true` 时照 F3.29 落到当前 tab 的节点。改在 F3.29 那一处，加一个判断。

**验收**
- [ ] 单测：`follow` 两种取值下，从表格 `SPC b` 进树、点 `»` 展开后光标的位置；mutation。
- [ ] e2e：树光标停在别的表上，聚焦一个表 tab 再 `SPC b`，光标还在原来那行；`follow = true` 时落在当前表上。

## F20.5 编辑器的 `clipboard = "unnamedplus"` · 负责：dev-2 · 状态：passed（832fb7c，分支 f20-ui；e2e e817cdf）

- **依赖**：M3 F3.38（寄存器）
- **涉及**：`internal/editor`（寄存器）、`internal/app`（读剪贴板的路）、`internal/config`
- issue #2（2026-10-31）：用户在 query 里 `yy` 后贴不到外面，自己发现 `"+yy`；用户定加 nvim 的 `unnamedplus`，缺省不变。

**开发**（§11「寄存器」、§14 `clipboard`）
- [ ] a. 不带寄存器的 yank / 删除 / 修改在 `unnamedplus` 下同时写系统剪贴板（走现在 `"+` 写的那一处）；不带寄存器的 `p` / `P` 走 `"+p` 读剪贴板的那条路，读不到退回无名寄存器。所有 vim 编辑器一起生效（编辑器包的一个开关，不在各处判断）。
- [ ] b. 配置值只认 `""` 和 `"unnamedplus"`，别的值同未知键的 toast。

**验收**
- [ ] **nvim 对照用例**（用户 2026-10-31：要保证符合 neovim 用户的直觉）：`cases.txt` 加一组 `:set clipboard=unnamedplus` 的用例，生成脚本里给 nvim 一个假的 `g:clipboard`（copy / paste 存取一个变量，`cache_enabled = 1`，就是 provider 那份带 regtype 的缓存），期望结果由 nvim 跑出来，不手写；Go 侧把这条 ex 当成打开 `UnnamedPlus` 加假剪贴板（同 `set noma` 的做法）。至少覆盖：`yy` / `yiw` / 块 `<C-v>…y` 后 `p` 和 `P`（块还是块）、`dd` / `x` / `cw` 之后 `p`（删掉的进剪贴板）、`"_dd` 不动剪贴板、`"+p` 显式、带 count 的 `3p`、外面写进剪贴板的文字（和缓存不同）按行 / 按字符贴。nvim 和编辑器不一致的，先报决策者，不改用例迁就实现。
- [ ] 单测：两种取值下 `yy` / `dd` / `x` / `p` 走的寄存器（用假的剪贴板工具）；读失败退回无名寄存器；没有本地工具时不发 OSC 52 查询；WHERE 和值编辑器也生效；mutation。
- [ ] e2e：`clipboard = "unnamedplus"` 时 query 里 `yy`，假 `pbpaste` 读到这一行；缺省时读不到。

## F20.6 间隙跟 `pane_bg`、`border_focus`、默认边框调亮 · 负责：dev-2 · 状态：todo

- **依赖**：M8 F8.10（边框）
- **涉及**：`internal/ui`（theme、frame、block 的边框色）、`internal/app`（画间隙的那一处）
- 用户 2026-10-30（截图）：间隙是一条更深的竖条，lazygit 是整片一个底色；聚焦 / 未聚焦边框要能在主题里分开配。决策者用空配置截图，内置主题的间隙和未聚焦边框也有同样的问题。

**开发**（§7.3 token、§7.8「整体布局」）
- [ ] a. 主题 token `gap`（缺省 = `pane_bg`）画 pane 之间、侧栏和 pane 之间的空白；`border_focus`（缺省 = `focus`）画聚焦的 pane 和浮层的边框，`zoom` 照旧盖过它。缺省值的写法同 `zoom` 跟 `warn`。
- [ ] b. 内置 tokyonight-storm 的 `border` 调亮，在 `#565f89` 附近截图对比定值，送审时附截图。

**验收**
- [ ] 单测：没写 `gap` / `border_focus` 时取 `pane_bg` / `focus`；写了用写的；mutation。
- [ ] golden：默认布局（间隙和边框颜色）。
- [ ] e2e：主题里 `bg` 和 `pane_bg` 不同时，间隙那一列的底色等于 `pane_bg`；`border_focus` 设了别的颜色，聚焦 pane 的边框是它、NORMAL 块还是 `focus`。

## F20.7 内置第二个主题 `ristretto` · 负责：dev-2 · 状态：todo

- **依赖**：F20.6（`bg` 要留深色给 tab 栏，间隙靠 `gap`）
- **涉及**：`internal/ui`（内置主题）
- 用户 2026-10-30：把现在用的 ristretto 做成内置，别人 `theme = "ristretto"` 就能用。

**开发**（§7.3）
- [ ] a. 内容取 `specs/m20-redis-ui/ristretto.toml`（决策者 2026-10-31 从用户的主题文件拷的，`bg` 已还原成 `#312b2b`，`border` 是试过的 `#7a7272`）：颜色和 `[icon]` 原样，注释改成面向所有人的英文（去掉「your nvim screenshot」「the color you chose」这类话）。放在内置主题的那一处，主题下拉里列出。
- [ ] b. 用户目录里同名文件优先（§7.3 现有的查找顺序，不改）。

**验收**
- [ ] 单测：`theme = "ristretto"` 没有用户文件时用内置的，有时用户文件优先；主题下拉里两个；mutation。
- [ ] golden：ristretto 的默认布局。
- [ ] 发版前 oss 在 README / `docs/config.md` 的主题一节补上它和一张截图。

## F20.8 键名索引的增量更新与后台匹配 · 状态：todo

- **依赖**：F20.1、F20.3
- **涉及**：`internal/app`（Redis 补全、面板、写命令之后）、`internal/db/redis`（`EXISTS`）
- 用户 2026-10-31：参考 DataGrip / JetBrains 的丝滑；写命令后每次全量重建怕有性能问题，能不能增量。

**开发**（§9.8「索引的增量更新与后台匹配」）
- [ ] a. 写命令成功后按 DOCS 的键位置取键、一条 `EXISTS` 增量改索引；`FLUSHDB` / `FLUSHALL` 清空；认不出键的才整份重建。替掉 F20.1 里「写命令后丢掉重扫」那一处，不另加第二条路。
- [ ] b. 整份重建在后台、旧索引照用、建好一次换上（树上 `R` 也走它）。
- [ ] c. 补全和面板 `@` 的匹配放进 Cmd，对不可变快照算，带序号丢过期结果，算的时候列表保留上一次的。

**验收**
- [ ] 单测：`SET` 新键、`DEL`、`RENAME a b`、`SET k v NX`（已存在）、`EXPIRE k 0`、`FLUSHDB` 后索引的内容和发出的 `EXISTS`；`EVAL` 认不出时重建且旧索引在重建期间可用；匹配 Cmd 的过期结果被丢；快照不被增量更新改到；mutation。
- [ ] 基准：10 万键时 Update 处理一个按键的耗时（匹配不在里面了），送审附数字。
- [ ] e2e：控制台 `SET m20:new 1` 后马上补全 `m20:n` 出它、没有整库 SCAN（slowproxy 日志里只有一条 EXISTS）；`DEL` 后补不出；树上 `R` 期间补全照常有结果。
