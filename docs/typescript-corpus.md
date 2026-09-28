# TypeScript 仓库评测

首个语料为 [compforge/doctor](https://github.com/compforge/doctor)。它包含 CLI、Plugin SDK、
Agent SDK 和示例插件，覆盖跨 workspace 导入、类型与值引用、类成员、泛型及异步调用。
固定 commit 和压缩包 SHA-256 见 [repos.json](../tests/corpus/typescript/repos.json)，
源码范围见 [doctor.json](../tests/corpus/typescript/doctor.json)。首次测量见
[测量记录](../tests/corpus/typescript/first-run.md)，循环与 catch 声明修复的复测见
[控制语句绑定复测](../tests/corpus/typescript/control-bindings.md)，剩余声明缺口的修复见
[生成器与私有方法声明复测](../tests/corpus/typescript/callable-declarations.md)。

## 输入与独立参照

参照使用官方 TypeScript 5.6.3 Compiler API，版本及 npm 完整性摘要由独立 package-lock 固定，
不调用 CodeGraph 或 gotreesitter 的解析与绑定实现。每个项目读取原始 tsconfig，分别建立 Program；
只将配置包含且位于指定 sourceRoot 下的源码传给 CodeGraph。文件清单保留摘要与排除原因，
`.d.ts` 作为编译器支持材料，不作为图输入。源码 package.json 的 workspace 名称链接到临时快照中，
模块 exports、路径和条件由 TypeScript 自己解析。

这是**源码限定 profile**：不安装 Doctor 的第三方依赖、不运行其脚本、测试或业务代码。
缺少外部类型信息可能影响推断。语义诊断完整记录在 oracle.json；只有已加载且能唯一定位的源码
声明才进入内部目标评分，不能解析或存在多个声明的绑定保留 unknown 原因，不猜测答案。
原始配置或语法解析失败则直接使评测失败，不生成空参照。

参照以源码路径、标识符 UTF-8 字节范围标识声明。Compiler API 的 UTF-16 位置转换为 UTF-8 后
再比较，避免中文和非 BMP 字符造成位置漂移。源码输入、profile、编译器锁文件与参照实现共同参与
基线身份；工具链记录 Go、Node 和 TypeScript 版本。

## 评分与边界

| 项目 | 口径 |
|---|---|
| 声明总量 | AST 声明清单，包含参数、类型参数、属性签名、解构绑定等契约外声明 |
| 契约声明 | 函数实现、Class、Interface、TypeAlias、Enum、Module、方法实现和简单命名 Variable；比较名称、类别与范围 |
| 引用事实 | 标识符使用位置；声明名、导入导出绑定名及标签不作为使用，模块限定符单列 |
| 调用事实 | call / new 表达式位置，与是否能绑定目标分开统计 |
| import 事实 | import / export 中的字符串模块路径及位置 |
| 引用目标 | 编译器唯一定位到已纳入图声明类别的内部目标 |
| 调用目标 | 可唯一定位的命名函数直接调用或类构造；方法与回调运行时分派保持未评估 |

总量分母独立于 CodeGraph 输出，不能通过少输出声明来减少期望目标。exact 正确/错误、candidate
命中/其他目标、最大候选数、未评估及局部诊断/静默缺口沿用[统一评分](corpus.md)。
额外语法事实需要逐项审定，不自动等同于错误绑定。方法签名可解析不代表已经证明实际运行时目标。

Module 等组织节点单列未评估；组织层级、declares/contains、关系来源归属与其他关系当前没有
独立编译器评分。组织契约继续由库的专门测试覆盖。没有错误 exact 只适用于已评分目标，不能解释为
整个仓库已被完备解析。

## 执行

需要 Node.js 20+、npm，以及公共评测入口要求的 Go 工具链。首次安装只下载锁定的编译器：

```sh
make setup-typescript-corpus
make test-typescript-corpus
make test-typescript-corpus \
  CORPUS_BASELINE_DIR=/path/to/reviewed-report \
  CORPUS_REPORT_DIR=/path/to/new-report
```

默认产物为 `.corpus-results/doctor/{summary.md,report.json,oracle.json,graph.json}`。
`make test-corpus` 同时评测全部六仓；`make test` 只跑本地参照器和库契约，不下载 Doctor。
`make test-corpus NODE=/path/to/node` 可指定参照器的 Node。

参照契约覆盖别名、重导出、workspace exports、简写属性、UTF-8 位置、构造函数、计算属性、
合并声明歧义、缺失依赖、动态分派、语法失败，以及错误 exact 注入和删除输出后分母不变。
真实源码差异应先核对参照，再缩减为解析器契约测试。
