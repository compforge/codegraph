# 关系、证据与源码绑定

## Relation 与 Evidence

Relation 表达一次有向代码关系，分类与方向见 [图模型](semantic-kinds.md#nodekind-与-relationkind)。
身份由 Source、Target、Kind 与发生位置组成，位置使用路径及字节范围。
相同端点之间的不同关系或不同调用位置保留为独立边，也允许自递归。

Evidence 记录建立关系的依据：Basis 说明推导规则，Confidence 说明证据强度，
可选 Location 指向支撑语法。同一次发生的多条依据合并、去重并确定性排序，避免最后写入覆盖证据。

| Confidence | 证据语义 |
|---|---|
| `exact` | 在给定材料与已支持的静态语义下，绑定或结构关系已确定 |
| `scoped` | 已有词法绑定、导入、接收者或类型等实际约束，但尚不足以确定绑定 |
| `name_only` | 主要依据名称匹配，未证明名称在该位置的绑定 |
| `heuristic` | 依赖约定、模式或不完整的结构相似性 |

精度顺序为 `heuristic < name_only < scoped < exact`，不按字符串排序。
目标数量与精度相互独立：只有一个同名目标也不能自动升级；多个合理目标继续保留。
Basis 描述“如何得到这条证据”，Confidence 描述“这条证据能证明到什么程度”；
同一种 Basis 可以因材料和约束不同而得到不同档位，不建立 Basis 到 Confidence 的固定映射表。

**`Relation.Confidence` 由 `Evidence[].Confidence` 推导，不是另一份独立判断。**

- 同一关系的独立证据取最高档，并保留全部 Evidence；重复或更多弱证据不会投票升级。
- 一条证据依赖多个必要推导环节时，取这些环节中的最低档；例如导入、转导出、继承查找不能丢失上游的不确定性。
- Confidence 不参与关系身份，调整分级不会制造另一条同位置关系。
- 同一调用或引用位置出现不同目标的 exact 断言时，保留关系并报告 `conflicting_binding`；较低档的备选目标不构成该冲突。

例如，同一调用关系有 name_only 的名称证据和 scoped 的接收者证据，关系投影为 scoped；
导入为 scoped、转导出为 exact 的必要推导链，最终证据仍为 scoped。
无法提出目标的引用保留使用节点及适用的诊断，不虚构关系端点。

confidence 不是概率，exact 也不承诺运行时行为或完整编译器类型检查。
符号间的距离取决于所选关系：词法共同祖先、语义共同所有者和调用路径分别表达不同结构。
通用路径查询提供事实，各消费者选择关系种类及权重。Path 与 Subgraph 保留原始关系及其证据；跨关系的多跳衰减、影响阈值、筛选与排序由消费者决定。

## 源码使用与目标绑定

`payment.charge(order)` 的调用由所属声明到目标的 `Calls` 表达；传递函数值则是 `Reads`。
`UsesType`、`Writes`、`Captures` 和 `UsesResource` 分别表达类型依赖、存储写入、词法捕获和资源关联。
复合赋值同时产生 Reads 与 Writes，已由 Calls 表达的 callee 绑定不再重复发布 Reads。

源码导航按需发布 `CallSite`、`ReadSite`、`WriteSite`、`TypeUse`、`CaptureSite`、
`ResourceUse` 与 `DecoratorUse`。`occurs_in` 指向所属声明或 Document，`binds_to` 指向
当前材料能够证明的直接目标或 Import。`aliases` 保留 Import → Export → 声明的名称链。
导航绑定与声明依赖使用相同证据，但使用不同 RelationKind，不混计 Calls。
名称、接收者线索和位置随发生点提供；receiver 是词法线索。没有目标的可发布发生点仍保留身份。

资源的 `Resource.Expression` 保留路径表达式，`Resource.Path` 保留可证明的快照相对路径。
补料后 ResourceUse 通过 `binds_to` 连接 Document，所属声明通过 `uses_resource` 连接同一目标。
它不证明读取、写入或执行文件。核心不内置平台 API 名称，规则由调用方显式提供。
详见 [资源引用](resource-references.md)。

Import / Export 保存每项的名称、位置和 ModuleBinding：specifier、importedName、localName、
exportedName、form、typeOnly。属性描述该源码项；aliases 表达它连接谁。specifier 不是已解析路径，
侧效应导入和通配转导出不虚构本地名。通配项保留导入来源规则，查询具体公开名称时沿模块规则
选取来源的具名 Export 或声明；不会把 `*` 伪装成模块别名。显式导出优先，default 不经通配传播。
Go 未显式命名的 import 只有获得唯一包身份后才补出 localName，不能从路径末段猜包名。

修饰应用使用 DecoratorUse，decorates 指向被修饰声明，binds_to
指向修饰器定义或导入绑定；即使修饰器不在图内，应用关系仍为源码事实。它不自动推导路由或
handler 等业务角色。extends / implements 使用也保留独立 TypeUse，动态基类表达式保留
源码及角色，但不将表达式中被调用的函数误连成基类。

使用和模块项不是声明，不进入 declares / encloses / contains 构成的声明结构，因而不污染 outline。
源码存在和目标可绑定相互独立；同一个绑定链中的局部 binds_to 可以是 exact，而跨模块 aliases
仍是 scoped。每条边保留本环节的证据，消费者不能把局部确定性当作整条链的确定性。

图只组织显式接纳的 Document 及其代码事实，不获取外部源码，也不为缺失目标制造占位符号。
没有 binds_to / aliases 可能来自材料缺失、动态行为或分析能力限制，结合诊断与语言能力判断。
身份取决于源码项而非目标。补入依赖后，同一使用或绑定项可获得新目标，旧边及其精度也可能修订；
已返回的 Graph 保持不变。提取器演进可以扩大覆盖，未提取到某种结构不等于源码中不存在它。
