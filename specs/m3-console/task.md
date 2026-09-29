# M3 console 与结果区 · 任务清单

- **状态**：todo。开发清单按 worker 的起草确认（2026-09-28）。M2 还没验收，用户决定与 M3 一起验收。
- **目标**：
  - vim 编辑器写 SQL；
  - 执行语句，结果显示在底部结果区；
  - 支持格式化、补全和 schema 选择；
  - 表格和 console 可以放在同一个 pane 的不同 tab 里，新 tab 显示引导页（M1 用户验收时加）。
- **范围**：
  - PRD：C-01~C-07
  - tech-design：§5、§8.6、§9.1–§9.5、§9.7、§11、§15
- **依赖**：M1、M2 全部 passed。
- **不在 M3 做**：
  - 快速 SQL 依赖 M3 的部分（写语句的判定与提示、`C-t` 送到结果区、`C-e` 在 console 中打开、首词判断换成读写判定与自动 LIMIT）：M4 F4.3。M3 只在 sqlkit 里提供这些函数，不动快速 SQL。
  - `result_split = console`：不做（§11）。
  - MySQL 的连接和 console：M5。M3 只做 sqlkit 这一层的方言（词法、格式化的 golden）。
  - 用户在 console 里 `begin` 之后不提交：M5 的 manual 事务模式时处理（§11 已知上限）。
- **完成标准**：
  - F3.1–F3.11 全部 passed；
  - 编辑器的差分测试与 nvim 的结果完全一致；
  - 用户验收通过后，打 tag `m3`（`m2` 同时打）。
- **审查节点**：
  1. F3.3 之后：F3.1–F3.3，编辑器（除块选择），纯逻辑，有 nvim 差分兜底（已通过）；
  2. F3.5 之后：F3.4–F3.5，块选择与 sqlkit（已通过）；
  3. F3.8 之后：F3.6–F3.8，console tab、混放与引导页、执行与结果区；
  4. F3.11 之后：F3.9–F3.11，格式化、补全、schema 下拉。
- **M1 / M2 的 e2e**：默认布局加入 console 后，⟨1⟩ 的宽度从占满变成 5/9，M1 / M2 脚本里依赖 data pane 宽度、`C-l` 焦点的地方可能失效。tester 在 `lib.sh` 里加一个开头先关掉 ⟨2⟩ 的辅助函数，不逐条改断言。

任务文件的格式和状态约定见 [`../plan.md`](../plan.md)。

---

## F3.1 编辑器核心与差分测试框架 · 状态：passed（f551bac；e2e 428c35d）

- **依赖**：M0
- **涉及**：`internal/editor`

**开发**
- [x] 接口：`New(text)`、`Feed(key)`（按键用 keymap 的记法字符串，editor 不 import keymap）、`Lines()`、`Cursor()`、`Mode()`、`Pending()`、`Selection()`、`SetHeight(n)`、`Top()`，另返回这一步的效果：寄存器变了、要执行的 ex 命令、内容变了。editor 不 import ui 和 bubbletea（§4）。
- [x] 位置为行加字节列，与 nvim 的 `getpos` 相同；显示列按 `ansi.GraphemeWidth`，Tab 按 `tab_width` 对齐。
- [x] 缓冲区、§11 表里的全部移动、INSERT / REPLACE、次数前缀；不折行，横向跟着光标（§11「不做」）。
- [x] INSERT：Tab 补到下一个 `tab_width` 的倍数（等同 `expandtab ts=sw=tab_width sts=0`），`↵` 继承缩进，带次数的插入（`3ix<Esc>`），方向键照 nvim 断开撤销步。
- [x] 撤销：每步保存整份 `[]string` 快照和光标，没改过的行共享字符串；不设上限，用 `ponytail:` 标出大文件会变慢。
- [x] 差分框架（§15）：`cases.txt` 每条是「`## 标题`、按键、初始文本（█ 标光标）」；golden 记录结果文本（█ 标光标）、无名寄存器的内容与类型、topline；生成器用 `feedkeys(keys, 'xt')` 和 §15 列的选项，文件头记 nvim 版本。

**验收**
- [x] 差分用例覆盖移动、INSERT / REPLACE、次数、撤销，结果与 nvim 全部一致。
- [x] 运行 `go test ./internal/editor` 不需要安装 nvim。

## F3.2 操作符、文本对象、VISUAL 与寄存器 · 状态：passed（f551bac；e2e 428c35d）

- **依赖**：F3.1
- **涉及**：`internal/editor`

**开发**
- [x] §11 表里的操作符、文本对象、VISUAL / VISUAL LINE、简写（`dd x J ~ r p gc` 等）。操作符的双写形式（`dd cc yy >> << gcc guu gUU g~~`）走「同一个操作符再按一次 = 作用于当前行」的通用规则，不逐个写。
- [x] 无名寄存器带类型；yank 或删除后产生「写剪贴板」的效果，`p` 只读内部寄存器（§11「寄存器」）。

**验收**
- [x] 差分用例覆盖每一个操作符与文本对象的组合类别，结果与 nvim 全部一致。

## F3.3 搜索与命令行 · 状态：passed（f551bac；e2e 428c35d）

- **依赖**：F3.2
- **涉及**：`internal/editor`

**开发**
- [x] `/ ? n N * #`，RE2、smartcase；找不到时产生「找不到：<pat>」的效果（app 用 toast 显示）；`n` / `N` 绕回时不提示。
- [x] `:` 命令行：`:{n}`、`:s`（范围只做当前行、`%`、`'<,'>`；分隔符可以是任意非字母数字字符；标志 `g i`；`\1`、`&`）；其余命令作为 ex 效果交给 app（§11「命令行」）。
- [x] 命令行的状态放在 editor 里，差分用例能直接覆盖 `/foo<CR>`、`:%s/a/b/g<CR>`。

**验收**
- [x] F3.1–F3.3 的差分用例合计至少 200 条，覆盖每一类操作，结果与 nvim 全部一致。
- [x] RE2 与 vim 语法不同的写法（`+`、`?`、`|`、`()`）有单测。

## F3.4 块选择（VISUAL BLOCK） · 状态：passed（f551bac；e2e 428c35d）

- **依赖**：F3.2
- **涉及**：`internal/editor`

**开发**

实现 §11 表格中「VISUAL BLOCK」一行及其补充说明的全部内容：

- [x] `I`、`A`、`$A`、`c`：先在第一行输入，按 esc 后复制到其余各行；
- [x] `d`、`x`、`y`；`y` 复制时，寄存器类型为块；`p`、`P` 按块粘贴；
- [x] `r`、`~`、`u`、`U`、`>`、`<`、`gc`；`o`、`O` 切换选区的对角。
- [x] 模式 VisualBlock，状态栏显示 V-BLOCK；`<C-q>` 也当作 `<C-v>`（nvim 的 `nv_visual` 就是这样处理的），所以 §11 说的 Windows 改绑 `C-q` 不用额外配置；v、V、C-v 互相切换同 nvim。
- [x] 给绘制的接口：`Selection()` 语义不变，另加 `Block()` 返回上下行和左右显示列，`$` 延伸时右边为 `math.MaxInt`。
- [x] 寄存器的块类型记为 `"\x16{宽度}"`（`getregtype` 的写法），内容是各行用 `\n` 连起来的文字，同 `getreg`。
- [x] 块下按 `:` 照其他 VISUAL 预填 `'<,'>`，按行执行。清单外的块操作不做：`D C S R X Y s J`、块里的文本对象、块下的 `p`。

**验收**
- [x] 差分用例覆盖上面列出的全部操作，结果与 nvim 一致，包括短行上的 `I` / `A` / `c`、块边界落在 Tab 上或宽字符中间的情况。

## F3.5 sqlkit：词法、分句、读写判定、自动 LIMIT · 状态：passed（f551bac；e2e 428c35d）

- **依赖**：M1
- **涉及**：`internal/sqlkit`

**开发**
- [x] 扫描器：在现有的 `Tokens` 上扩展成 `Scan(s, dialect)`，输出 token 的类型、字节偏移和行号；方言为 `sqlkit.PG`、`sqlkit.MySQL`；`WhereContext`、`SelectLike` 改用它。先参考 lazysql 的 `sql_lexer.go`，提交说明里写明。
- [x] token 类型加 Keyword，按单独一个文件里的关键字表判断；补全用的关键字从这张表里取常用的部分。
- [x] 分句（§9.2）：语句从第一个不是空白、也不是注释的 token 开始，到 `;` 之前为止；光标在第一条语句之前时取第一条。PG 14 的 `BEGIN ATOMIC … END` 里的 `;` 会被切开，用 `ponytail:` 标出。先看 usql 的 `stmt/`。
- [x] 读写判定（§9.3，含 `SELECT … INTO`、括号写法的 `EXPLAIN (ANALYZE …)`）。先看 lazysql 的 `validation.go`。
- [x] 自动 LIMIT `AutoLimit(stmt, n)`（§9.4）。
- [x] WHERE 的 `;` 检查接进 WHERE 的执行（§9.6）。

**验收**
- [x] 表驱动的单元测试覆盖以下情况：
  - `$tag$…$tag$`、`E'…'`、可嵌套的 `/* */` 注释；
  - MySQL 的反引号、`#` 注释、`-- `（后面必须跟空格才算注释）；
  - 包含写语句的 CTE；`EXPLAIN ANALYZE` 与 `EXPLAIN (ANALYZE, BUFFERS)` 后面跟写语句；`SELECT … INTO`；
  - 语句已有 LIMIT 或 FETCH 时，不再追加 LIMIT；末尾带 `--` 注释时追加的 LIMIT 仍然生效。
- [x] e2e：WHERE 输入 `1=1; drop table t_log` 后执行，pane 第一行显示「WHERE 里不能有 ;」，没有发出查询。

## F3.6 console tab · 状态：todo

- **依赖**：F3.3、F3.5
- **涉及**：`internal/ui`（console.go）、`internal/app`（consoleTab）、`internal/config`

**开发**
- [ ] 默认布局（§5）：⟨1⟩ 是空 pane（F3.7 起显示引导页），⟨2⟩ 是 console_1，宽度 5:4，初始焦点在 ⟨1⟩。
- [ ] 文件、自动保存、`:w` / `C-s` / `:q` / `:wq`（§11「文件」）。自动保存的去抖用 `tea.Tick` 加序号；写入失败 toast「保存失败：<err>」。配置加 `tab_width`。
- [ ] 连接名用作目录名，config 里校验连接名：不允许 `/`、`\\`、以 `.` 开头（§14）。
- [ ] 横向滚动照 nvim 默认的 `sidescroll=1`、`sidescrolloff=0`；编辑器加 `SetWidth(n)`、`Left()`；差分结果在 leftcol 不为 0 时加一行 `left: N`，补几条长行的用例。
- [ ] 绘制（`ui/console.go`）：每行 `▶`（1 列，`focus` 色；上次执行出错的语句为 `error` 色，缓冲区一有改动就清掉）+ 行号（宽 max(3, 位数)，右对齐，`dim` 色，光标行用 `fg`）+ 1 个空格 + 文本。NORMAL 下光标所在语句的范围用 `row` 底，VISUAL 选区用 `select` 底。
- [ ] 高亮：关键字 `keyword`，数字 `number`，字符串 `sql_string`，注释 `comment`，标识符后面紧跟 `(` 的用 `func`，其余 `fg`（§7.3）。
- [ ] 光标用终端光标：NORMAL / VISUAL 为块，INSERT 为竖线，REPLACE 为下划线。
- [ ] 状态栏：模式块显示 `NORMAL`、`INSERT`、`VISUAL`、`V-LINE`、`V-BLOCK`、`REPLACE`、`COMMAND`（V-LINE、V-BLOCK 用 VISUAL 的颜色，REPLACE 用 INSERT 的颜色）；VISUAL 的附加信息为 `4 行 · ↵ run` 或 `12 字符 · ↵ run`，V-BLOCK 为 `3 行 × 4 列 · ↵ run`，键位文字从 keymap 读；待输入序列这一块也显示引擎正在等的键（`2d`、`f`），相当于 showcmd；console 聚焦时不显示 `行,列`。
- [ ] `/`、`?`、`:` 的输入行在内容区最后一行（§11「命令行」）。`:`、`;` 从 `[keys.normal]` 挪到 `[keys.grid]`、`[keys.tree]`（`[keys.landing]` 在 F3.7），console 里交给编辑器（§6.8）。
- [ ] 编辑器在等后续按键时跳过 keymap（§6.4）。
- [ ] 鼠标：单击定位光标（INSERT 下仍是 INSERT，VISUAL 下回到 NORMAL）；在文本区拖动从按下处进入字符 VISUAL；滚轮每格 3 行，最多滚到最后一行在顶部，光标夹回视图内；单击 ▶ 执行那一条语句（`console.run <行>`，F3.8 接上）。
- [ ] 粘贴照 nvim 的 `vim.paste`：NORMAL 下贴在光标后面（同 `p`），贴完光标停在最后一个贴进去的字符上；INSERT 下插在光标处；VISUAL 下用粘贴内容替换选区；模式不变；整段算一个撤销步；CRLF 换成 LF；粘贴不弹补全。
- [ ] 「在 $EDITOR 中编辑」：Action `console.external`，默认不绑键；依次取 `$VISUAL`、`$EDITOR`、`vi`，用 `sh -c` 执行；先写盘，再用 `tea.ExecProcess` 打开，回来后重新载入，算一个撤销步。
- [ ] 支持 `[map.console.normal]`、`[map.console.visual]`。
- [ ] 代码里写死的 `doraemon.public ▾` 去掉，F3.11 再画真实的按钮。

**验收**
- [ ] e2e：
  - 输入 SQL 后，高亮颜色正确；每条语句的第一行显示 ▶；光标所在语句的范围被高亮；
  - 拖动能选中文本；`f<Space>x` 删掉的是空格本身（`f<Space>` 跳到空格上），不会关掉 pane；
  - console 里 `;` 重复 f/t，`:` 打开编辑器的命令行，`:3` 跳到第 3 行；
  - 文件保存到隔离后的 `XDG_DATA_HOME` 下，权限 0600；重启后 console_1 还是上次的内容；
  - 在 NORMAL 下粘贴，内容作为文本贴在光标后面，不会被当作命令执行；
  - 状态栏的模式、VISUAL 附加信息、showcmd 正确；
  - 连接名里带 `/` 时启动报错。
- [ ] golden：console 的高亮、gutter、语句范围、VISUAL 选区、命令行。
- [ ] 补回 M0 里因 F1.1 去掉假 console 而删掉的 e2e（tester 在 F1.1 结论里列出），和 schema 下拉框无关的部分：
  - f0.2：data:console = 5:4；console 未聚焦的边框 / 标题色；`▶ run ↵` 的样式与各宽度下的退让；no_wasted_room；
  - f0.3：改绑 `console.run` 为 `R` 后标题显示 `▶ run R`；
  - f0.8：点击 `▶ run` 先让 console 获得焦点；悬停 `▶ run` 为 warn 底、移开恢复；指针在 console 上滚动只滚 console、焦点不变；
  - f0.12：nerd 下 console 图标 U+F489，ascii 下为 `>`。

## F3.7 pane 混放 tab 与引导页 · 状态：todo

- **依赖**：F3.6
- **涉及**：`internal/app`（Pane、Tab、openTarget、作用域）、`internal/ui`（tab 栏、引导页）

**开发**
- [ ] 去掉 PaneKind（§5「表格和 console 可以放在同一个 pane」）：侧栏就是 `win.Tree`，结果区用 `win.Result`（F3.8 加上），其余都是普通 pane；`Tab` 加 `Console *consoleTab`，`Data` 和 `Console` 都是 nil 的是引导 tab；按 PaneKind 下标的并行表删掉。
- [ ] 作用域按当前 tab 的类型（§6.4）。
- [ ] 图标：去掉 `data`，表 tab 一律用 `table`，console tab 用 `console`，引导 tab 不画图标（§7.7）。tab 栏每项写成 ` 1:<图标> t_order* `，引导 tab 写成 ` 2:新 tab `。
- [ ] 引导页：内容区中间两行按钮 ` <table 图标> 打开表 ` 和 ` <console 图标> 新建 console `，后面跟 `dim` 色的键位，悬停 `select` 底。`[keys.landing]`：`t` → `tab.table`「打开表」，`c` → `console.new`「新建 console」，另加 `:`、`;`（§6.8）。
- [ ] `+` 新开一个引导 tab 并切过去；M1 的 `newTabIn` 标记删掉。
- [ ] 「打开表」：打开面板的表范围，选中的表总是开在这个引导 tab 里，替换它，不去找已经打开的同一张表。从树或面板正常打开表时，目标 pane 的当前 tab 是引导 tab，也替换它。
- [ ] 「新建 console」：目标 pane 的当前 tab 是引导 tab 时就地换成 console（`c`、点击、面板都一样）；否则在焦点所在的普通 pane 新开 console tab，焦点在树上时用 openTarget 选出的 pane。打开表同理：当前 tab 是引导 tab 时，`↵`、`t`、`C-t` 都替换它（§5）。
- [ ] 查询条第二行放不下时的让位顺序（§7.8「查询条」）：统计最先让位，保存结果和错误优先于 chip 和按钮。
- [ ] openTarget 的「data pane」改成「普通 pane」（不含结果区）；焦点不在普通 pane 上时优先取最近聚焦过、当前 tab 不是 console 的 pane；当前 tab 是 console 时新开 tab（§5）。
- [ ] 引导 tab 名为「新 tab」，不画图标，ascii 下也不写类型词；没有 tab 的 pane 标题只有 `⟨n⟩`；引导页和空 pane 的 tab 栏不显示键位提示。`x` / `:q` 关引导 tab 不确认。
- [ ] `:wq` 在表 tab 上先保存、成功后才关闭（§11「文件」）。

**验收**
- [ ] e2e：
  - 同一个 pane 里开一个表 tab 和一个 console tab，来回切换时标题图标、`▶ run`、按键作用域跟着变；
  - 当前 tab 是 console 时从树按 `↵` 打开表，console 还在，表在新 tab 里；
  - 点 `+` 出现引导页，点「打开表」选一张表后开在这个 tab 里，按 `c` 开出 console；
  - 分割出的新 pane、没有 tab 的 pane 都显示引导页。
- [ ] golden：引导页；混放后的 tab 栏。
- [ ] tester 改写 f1.6 里依赖「`+` 聚焦树的过滤框」的用例，f0.12 里检查 `data` 图标的断言跟着改。

## F3.8 执行与结果区 · 状态：todo

- **依赖**：F3.7
- **涉及**：`internal/app`（结果区、日志）、`internal/ui`（结果 tab、工具行）、`internal/db`、`internal/config`

**开发**
- [ ] 执行的单位：光标所在的语句，或选区里的文字（字符、行选区）；块选区执行它覆盖到的那几整行，同 V-LINE。用 sqlkit 分句；逐条执行、遇错就停（§11「执行」）。
- [ ] 行数上限与截断显示（§11）；配置加 `result_height`、`[console] max_rows`。
- [ ] 执行中：同一个 console 再按 `↵` 忽略；别的 console 在 Worker 的锁上排队，占位照样显示；状态栏 busy，`C-c` 取消 `Main`。
- [ ] 结果区的出现与关闭：第一次执行时把根节点包进纵向节点，比例取 window 记住的值（初始 `1 − result_height`）；关闭时记下比例，日志保留在 window 上，结果 tab 全部丢掉（包括固定的）。
- [ ] 标题 `⟨3⟩ <result 图标> console_1 #42`，右侧 `3 行 · 8ms` 和 5 个按钮（重跑 `result.rerun`、转置、固定 `result.pin`、导出 `result.export`、关闭 `result.close`），放不下时的舍弃顺序见 §11「工具行」；新图标 `result`、`pin`、`export`、`close`（§7.7）。
- [ ] tab：第一个是「日志」；结果 tab 名 `console_1 #42`，多个结果为 `#42`、`#42·2`……；序号 `Session.RunSeq` 每次执行加 1；固定的画 `pin` 图标。
- [ ] 替换规则：同一个 console 的未固定结果 tab 整组替换，新的一组放在原来那组第一个的位置；没有结果集时保留上一次的，切到日志（§11）。
- [ ] 占位：内容区第一行 `dim` 色 `执行中 · 3s · C-c 取消`，每秒刷新。
- [ ] 日志：每条一行 `14:05:12  console_1  <语句第一行>  3 行 · 8ms`，非查询写 `UPDATE 3 · 2ms`；出错为 `error` 色的 `ERROR: <Message>`，DETAIL / HINT 另起行；取消记「已取消」；新的在下，自动滚到底；日志 tab 上 grid 的 `j` / `k` / `gg` / `G` 和滚轮滚动日志。
- [ ] 出错：切到日志，出错语句的 ▶ 变红，前面成功的照常出 tab。取消：切到日志，▶ 不变红，toast「查询已取消」。
- [ ] 焦点：执行后留在 console，结果区切到这次的第一个结果 tab。
- [ ] 结果表格只读：`hjkl gg G 0 $ T`、滚轮、点击可用，`↵` / `i` 不做事，`x` 等同 `q`；`[keys.result]` 加 `R` → `result.rerun`。
- [ ] 重跑：用这组结果当时的 SQL，作为来源 console 的一次新执行。
- [ ] 导出 CSV 写到当前目录的 `console_1-42.csv`，toast 显示路径（§11）。
- [ ] DDL 成功后重新加载 catalog（§11）。

**验收**
- [ ] 集成测试：多条语句中第二条出错时，第一条已经提交、第三条没执行；取消后 `Main` 还能继续用。
- [ ] e2e：
  - 第一次执行时，在底部出现结果区；
  - 再次执行时替换原有的结果 tab，序号加 1；按 `P` 后再执行，会新开一个 tab；
  - 选中多条语句执行时，产生多个结果 tab；执行 DDL / DML 只写进日志；
  - 执行出错时，切到日志 tab，并出现红色的 ▶；
  - 返回 1001 行的查询显示 `1000+ 行`；
  - `create table` 之后树里出现这张表；
  - 导出的 CSV 文件在当前目录。
- [ ] golden：结果区标题与按钮在几种宽度下的舍弃；日志 tab。

## F3.9 格式化 · 状态：todo

- **依赖**：F3.6
- **涉及**：`internal/sqlkit`（format，embed sql-formatter）

**开发**

§9.5 的全部内容，另加：

- [ ] sql-formatter 用 15.9.0，文件放在 `internal/sqlkit/sql-formatter.min.js`，旁边放它的 MIT LICENSE；spike 的几项检查在 golden 里重跑。
- [ ] 范围：NORMAL 下是当前语句，不含 `;`，`;` 留在原处；VISUAL 下字符选区就是选中的文字，V-LINE、V-BLOCK 是覆盖到的整行，格式化后退出 VISUAL。整次格式化算一个撤销步，结果和原文相同时不记撤销步。格式化后光标照 vim 的 `gq` 停在格式化文字的最后一行（行首第一个非空白字符）。
- [ ] `keyword_case` 只接受 `lower` / `upper` / `preserve`，其他值启动报错；`formatprg = ""` 用内置的。失败时 toast「格式化失败：<错误>」。
- [ ] 在 Cmd 里异步执行，回来时缓冲区已经变过就丢弃结果。
- [ ] 选项：`language` 为 postgresql，`keywordCase` 取 `keyword_case`（默认 lower），`tabWidth` 取 `tab_width`；配置加 `keyword_case`、`formatprg`。
- [ ] Unicode 属性名的替换按名字逐个断言次数（15.9.0 为 Alphabetic 3、Mark 1、Decimal_Number 1，§9.5），升级打包文件后次数一变测试就失败。
- [ ] `formatprg`：`sh -c`，stdin 输入、stdout 输出，5s 超时；失败时 toast 显示 stderr 的第一行，缓冲区不变。

**验收**
- [ ] 格式化的 golden 测试通过，PG 和 MySQL 各准备若干条语句。
- [ ] 第一次格式化的耗时小于 300ms（单测，`-race` 下跳过）。
- [ ] 配置 `formatprg` 后，改用外部命令格式化；格式化失败时，缓冲区内容不变。

## F3.10 console 的补全 · 状态：todo

- **依赖**：F3.5、F3.6
- **涉及**：`internal/sqlkit`（补全上下文）、`internal/app`

**开发**
- [ ] `sqlkit.CompletionContext(text, pos, dialect)` 建在扫描器上，参考 lazysql 的 `sql_context.go`（别名、CTE、子查询层级、`schema.`），用例参考它的测试；`WhereContext` 保留为单独的函数（§9.7）。
- [ ] 候选按 §9.7 的表分组：表名取这个 console 选的 schema（F3.11 之前取树当前的 schema），输入 `schema.` 后只列那个 schema 的表；CTE 名算作表；列在第一次用到时从 `Meta` 获取，与打开表共用列缓存。
- [ ] 快速 SQL 的补全也改用 `CompletionContext`。
- [ ] 按键：`C-n` 手动唤起；`esc` 第一次关列表、第二次退出 INSERT；`↵` 接受后文字有变化就接受，没有变化就换行（§9.7）。
- [ ] 自动弹出：输入标识符字符且前缀不为空时；或者刚输入 `.`、前面的限定名能解析时（别名 / 表名 → 列，schema 名 → 表），前缀为空也弹。输入非标识符字符、方向键、离开 INSERT、没有匹配时关闭；粘贴不弹。接受算作输入，属于这次 INSERT 的撤销步。
- [ ] 缓存里没有的列从 `Meta` 取（与打开表共用），取回时 console 还在 INSERT、光标还在原处就重新补全；每次补全每张表最多取一次。插入的只是表名，不带 schema、不加引号（需要加引号的表名是已知上限）。

**验收**
- [ ] 上下文判断有单元测试。
- [ ] e2e：
  - 输入 `select * from t_o` 时，候选中出现 `t_order`；
  - 输入别名加 `.` 后，列出这张表的列；
  - CTE 的名字会出现在候选中；
  - 快速 SQL 里 `select o. from t_order o` 的 `o.` 后面列出 t_order 的列。

## F3.11 schema 下拉框（PG） · 状态：todo

- **依赖**：F3.8
- **涉及**：`internal/app`（consoleTab.Schema、dropdown）、`internal/db`

**开发**
- [ ] console 标题上的 `<库名>.<schema> ▾`，库名记在 Session 上；`gs` 或点击打开下拉框，复用 `app/dropdown.go`，加一种 dropSchema，位置和样式照 §8.6。
- [ ] search_path 的比较与重设（§8.6「执行方式」）。
- [ ] 新 console 默认用树当前的 schema，之后两者互不影响。
- [ ] MySQL 的 console 不显示这个下拉框（M5 时生效）。

**验收**
- [ ] 集成测试与 e2e：
  - console 选择 `agentable` 后，`select * from <agentable 中的表>` 能执行成功；
  - 两个 console 分别选择不同的 schema，交替执行，结果都正确；
  - console 里自己 `set search_path` 后，下一次执行照下拉框的选择重新设置。
- [ ] 补回 f0.2 里和 schema 下拉框有关的 e2e：160 与 200 宽的 console 标题（对象名 + `<库名>.<schema> ▾` + `▶ run ↵`）；各宽度下下拉框按钮的退让顺序（`▶ run` > 下拉框 > `↵`）。
