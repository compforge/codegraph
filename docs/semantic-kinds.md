# Kind 与 lowering

`pkg` 承载独立于解析器与图引擎的公开值模型，包括 NodeKind、RelationKind、Node、Relation、Location、
Tag、Confidence 和报告。根包通过 alias 保留导入兼容；`pkg` 不依赖 `internal`。
两个 Kind 都是 string 定义的开放 enum。internal 可以定义相同类型的常量，只有发布入口检查公共词表。
Go 标识符大小写控制包可见性，序列化字符串的大小写不承担阶段或权限语义。

```text
source facts → lexical binding / resolution → lowering → admission → graph
                  intermediate operations       semantic relations
```

NameUse、CallTargetUse、CompositeKeyUse、ReadWriteUse 等中间操作参与解析，不直接成为图的 Kind。
遮蔽记录即使没有声明目标，也必须保留，防止错误绑定同名外层声明；多个已保留的词法候选仍参与解析。

| RelationKind | 含义 | 例子 |
|---|---|---|
| Calls | 调用目标 | `handler()` |
| Reads | 读取值或传递函数值 | `register(handler)` 的 handler |
| Writes | 写入可解析存储 | `state.value = x` |
| UsesType | 类型依赖 | 参数类型、类型转换、构造的类型 |
| Captures | 嵌套命名 callable 使用外层局部存储 | inner → outer.value |
| UsesResource | 显式 API 语义证明的文件关联 | 路径表达式 → Document |
| Aliases | 名称绑定链 | Import → Export → declaration |
| BindsTo | occurrence 的直接解析目标 | CallSite → declaration 或 Import |

`x += y` 对 x 产生 Reads 和 Writes。callee 已产生 Calls 时不再重复产生 Reads；不支持的动态调用
仍可保留已证明的值依赖及诊断。Reads / Writes 不表示文件 I/O；UsesResource 不推断文件读写效果。
Captures 与读写证据共存，admission 保留捕获的存储目标。Python 只提升被命名嵌套函数捕获的局部绑定，
未捕获且不受声明提取支持的局部仍是分析记录。匿名函数调用、Python global/nonlocal 仍受现有能力限制，
不凭 Kind 的存在承诺全语言覆盖。

发生点使用 CallSite、ReadSite、WriteSite、TypeUse、CaptureSite、ResourceUse、DecoratorUse。
`OccursIn` 表达归属，`BindsTo` 表达目标；语义依赖从所属声明或 Document 出发。
精简图不创建发生点，依赖关系无需借助中间节点才能成立。

预算分开计量：RawUses 默认 4,000,000，约束全部语法 use；Uses 默认 1,000,000，约束可参与语义解析的
候选（包括尚未绑定的名字，不包括已知仅遮蔽的局部名字）；Relations / Evidence 约束降低后的内部关系
和证据，MaxNodes / MaxRelations / MaxEvidence 约束实际发布。额度有限且失败原子，拒绝材料或不发布
发生点均不会关闭分析预算。BuildReport.Analysis 同时报告 RawUses 和 Uses。

迁移时将旧 `references` 查询按目的改为具体语义关系，源码导航改用 BindsTo。旧 Reference / References
常量保留以兼容编译，但 Builder 不再产生这两种图对象。`ReferenceKind` 字段仍可读取 occurrence 的角色。
依赖消费者应覆盖所有语义依赖；可使用 `RelationKind.IsDependency()`。BindsTo 不属于声明依赖。

Corpus 的历史 oracle 统计 identifier binding，测试对照层将细分边映射到这一评测口径，并用原始 callee
位置对照 Calls；原始观察产物保留新 Kind。该口径验证目标与位置，不验证操作分类；操作分类由独立语义
fixture 验证，不能把两类指标混为一谈。

## 固定基线验证

对 gotreesitter `ba95e4ea1b33276c6aa5f1174338adcab39be039` 使用相同输入和 AnalysisAdmission，
比较改动前后首个快照的分析、发布及依赖集合。原版仅临时提高 Uses 才能完成测量；新版使用默认额度。

| 指标 | 原版 | 新版 |
|---|---:|---:|
| 原始 use | 1,552,054 | 1,552,099 |
| 语义候选 use | 与原始 use 混计，超过 1,000,000 | 961,668 |
| 内部关系 | 833,621 | 704,470 |
| 公开节点 | 48,512 | 48,513 |
| 公开关系 | 650,594 | 518,105 |
| Calls | 146,280 | 146,280 |

Calls 的端点、类型、文件及 byte span 集合完全一致。旧版全部 502,105 条 calls / references，
均可在新版同端点、同位置语义关系中找到，或由同端点且覆盖 callee 位置的 Calls 承接，无依赖丢失。
公开关系减少约 20.4%；新增节点是捕获所需的局部存储，并产生 4 条 Captures。
该测量验证语义与额度，不是峰值内存或性能基准。
