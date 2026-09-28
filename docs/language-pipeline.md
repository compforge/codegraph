# 语言构建流程

## 模型与职责

CodeGraph 从同一快照的 Document 提取事实，由语言规则建立组织与绑定，再发布带源码证据的图。
Document 表达材料来源，Symbol 表达代码实体，Namespace 表达名称与成员的组织；后两者可以重叠。
图中使用具体 Kind，源码贡献使用 declares，直接语义归属使用 contains。

公共分析层拥有实体引用、源码贡献与直接成员索引。合成 Package/Module 与显式 Namespace、Class
的成员使用相同索引；跨文件接收者归属在绑定后补入。继承查找在直接成员之上遍历已绑定的 extends。
词法作用域、名称遮蔽和导出规则由语言实现解释，不由 contains 自动推导。

## 阶段与数据流

| 阶段 | Core 责任 | 语言接口与产物 |
|---|---|---|
| Extract | 有界调度、parser/AST 生命周期、取消 | Extractor 返回脱离 AST 的单 Document 事实 |
| Organize | 登记身份、来源及声明直接成员 | Organizer 返回语言组织及有证据的组织关系 |
| Bind | 汇总绑定结果，补齐成员和类型关系 | Binder 返回导入、类型、接收者关系及本轮 Resolver |
| Resolve | 汇总关系证据和局部缺口 | RelationResolver 使用完成绑定的索引解析引用与调用 |
| Finalize | 端点校验、预算、物化和原子发布 | 由 Core 执行 |

阶段之间有显式屏障：所有语言完成 Organize 后才开始 Bind，所有 Bind 完成后才开始 Resolve。
这使晚加入的基类、接收者类型以及混合 JS/TS 文件能够参与同一轮关系解析。
语言内部可进行有界类型传播；它不能提前发布结果或自行启动另一轮构建。

语言实现只消费调用方提供的源码材料和当前批次索引，不读取仓库、安装依赖或操作图存储。
Binder 与 Resolver 将输入索引视为只读，其关系结果由 Core 登记；失败或取消返回错误，整个批次不发布。
候选目标保留 confidence 与 basis，已识别的遮蔽阻止同名回退，未解析引用保留局部诊断。

## 语言接入

`internal/analysis/stages.go` 定义阶段接口，`internal/language` 统一注册实现与能力说明。
具体阶段实现集中在 `language/adapters/`；适配器复用的语法与绑定算法位于 `language/syntax/`、
`language/module/`，共享组件依赖分析契约，由各适配器提供语言策略。
新增语言选择需要实现的阶段，并把实际能力和限制放入同一个注册项；Core 的阶段编排不识别具体语言名。
仅注册 grammar 的语言继续使用通用 outline，报告关系解析覆盖缺口。

Go、Python、ECMAScript 各自拥有语义规则；共享算法由它们显式调用：

- 通用 outline 与 marker 提取保持一个实现。
- Python 和 JS/TS 共享模块语法遍历，通过 dialect 接口提供参数、签名、接收者和基类等差异。
- 模块绑定共享转导出与候选汇总算法，路径、兼容语言和导出策略由各自实现提供。
- Go 的类型形状与传播证据位于 Go 模块，通过强类型的 Extension 携带，不进入公共 Node。

注册项不保存跨快照可变状态。Extract 只能在调用期间借用语法树，返回的事实及语言扩展证据必须
脱离 parser 且只读；Binder 返回的 Resolver 状态属于本轮构建。公共索引始终由当前源码快照重建。

## 验证边界

接入需要验证阶段依赖、源码身份、语言隔离、取消与预算，并为实际新增的语义建立契约。
测试适配器验证无需修改 Core 即可接入组织、绑定和关系解析；现有真实语言契约验证 grammar 兜底及语义规则。
结构调整应比较固定语料的完整图、诊断和独立评测结果，而不只比较节点数量。

这些接口是内部实现边界，公共 API 仍为 Graph、Document、Facts、Node、Relation 和查询能力。
影响传播、排序、测试选择及执行策略属于消费者。
