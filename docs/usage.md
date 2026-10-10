# 使用指南

本篇按材料准备、构建、定位与查询的顺序介绍使用方式。首次 Build 示例见 [README](../README.zh-CN.md)。
字段和方法的精确契约以源码中的公共 API 注释为准；设计理由见 [内核设计](kernel.md)。

## 准备材料

调用方负责获取同一快照的材料，将完整源码放入 Document.Content，并提供快照内的逻辑路径。
路径不必对应本地文件，但需要为语言识别和相对导入提供上下文。
源码、项目清单、无 grammar 材料和 gitlink 的处理见 [Document 契约](document.md)。

同一批次可包含多种语言及清单；提供 go.mod 可建立 Go 模块身份，局部图也可用 ModulePath 提示。
CodeGraph 不跨语言按同名绑定，不自动获取依赖。缺少材料时可以先消费局部事实，再决定是否补料。

`Extract` 提取单 Document 事实而不发布图状态，适合构图前探索 import 或其他线索。
这些 Facts 用于生产侧选择材料和提供 ResolutionContext，语句线索的语言覆盖见 [语言能力](language-support.md)。
同内容的后续构建复用提取缓存。消费侧从已发布 Graph 的 Node + Relation 读取代码事实；
outline、声明文档展示和调用链等结果均从图生成。

## 生产侧：独立提取与构图

需要探索依赖或比较快照时，显式传递 Facts，避免把解析与构图绑定在一起：

```go
extractor, err := codegraph.NewExtractor(codegraph.ExtractionOptions{})
if err != nil { return err }
facts, err := extractor.Extract(ctx, document)
if err != nil { return err }
builder, err := codegraph.NewBuilder("revision-1", codegraph.Options{})
if err != nil { return err }
if err := builder.Add(facts); err != nil { return err }
g, report, err := builder.Build(ctx)
if err != nil { return err }
_ = g
_ = report
```

`Extractor.Submit` 有界异步提取并复制输入，任务的每次 Wait 返回独立检查视图。
Facts 内部持有完整只读材料，公开字段用于检查；修改字段不改变构图输入，`View` 可重新取得视图。
`Builder.Add` 原子接纳一个 Facts 批次；同路径不同内容需要另建 Builder。
`Build` 返回只读 Graph，后续补充材料或修改 ResolutionContext 不改变既有结果。
失败构建保留 `Builder.Result`。单文件解析错误可用 `AddFailure` 保留材料身份与诊断，
取消和预算失败应直接返回。

`ResolutionContext.GoModules` 提供模块根映射；ModulePath 填充缺省根，显式 `.` 映射优先，
已提供的 go.mod 声明优先于同根提示并报告冲突，包组织与导入解析使用同一份模块上下文。`Imports` 提供具体导入位置的模块候选、
精度上限和依据。省略的导入沿用语言规则，显式空候选表示已知未解析。
上下文在接纳时复制，可用 `SetResolutionContext` 在下一次 Build 前替换。

跨快照共享 Extractor，并通过其 `ExtractionOptions.Cache` 配置有界缓存；
也可直接复用已持有的 Facts。两种方式都不复用绑定关系，不绕过构图预算。

## 查询 namespace 归属

对输入文件或声明，可查询其所在的 Package、Module、Namespace 等组织，以及多个节点的共同祖先。
例如寻找两份文件的共同 Go module 或 Python package：

```go
matches, err := g.CommonNamespaces(ctx, []string{
    codegraph.DocumentID("app/a.go"),
    codegraph.DocumentID("lib/b.go"),
}, codegraph.NamespaceOptions{
    Kinds: []codegraph.NodeKind{codegraph.Package, codegraph.Module, codegraph.Namespace},
})
if err != nil { return err }
for _, match := range matches {
    fmt.Println(match.Node.Kind, match.Node.QualifiedName, match.Depth, match.Confidence)
    for _, path := range match.Paths {
        for _, relation := range path.Relations {
            fmt.Println(relation.Kind, relation.Confidence, relation.Evidence)
        }
    }
}
```

`NamespaceAncestors(ctx, nodeID, options)` 查询单节点的祖先，namespace 本身也在结果中。
默认要求 exact 归属；若接受候选关系，可显式设置 MinConfidence。结果按距离由近及远，
每个结果包含从各输入出发的路径与证据；Document 的第一条关系是 in_namespace。
空结果表示当前图无法证明共同归属。语义与语言边界见 [命名空间组织](namespaces.md#祖先与共同归属)。

## 从 Graph 读取文件结构

通过 declares 取得文件贡献的声明，通过 encloses 取得声明的词法父子关系，按源码位置排序即可
组织文件的符号 outline。顶层父节点是 Document；Package / Module 等合成组织不进入词法树。
此 outline 是消费侧投影，全部输入来自图的节点和关系：

```cypher
MATCH (:Document {path:$path})-[:declares]->(n)<-[:encloses]-(parent)
RETURN parent, n
ORDER BY n.startByte, n.id
```

直接子级可用 `g.RelationsFrom(parentID, codegraph.Encloses)` 读取，返回顺序按源码位置确定；
`g.Node(edge.Target)` 返回声明及名称位置。递归投影树时使用节点 ID 连接，不能按名称连接。
类型的语义成员则查询 contains：Go 接收者方法可能在另一个文件，不应被搬进类型文件的 outline。

`Node.Location` 是完整声明范围，`Node.NameLocation` 是名称 token / 捕获范围，均使用零基字节
偏移、左闭右开范围和一基行列。合成组织、Document 或提取器未提供名称位置时，NameLocation 为 nil。

此视图覆盖 Graph 已保留的声明，不等于完整 AST。缺失声明仍由 BuildReport 的覆盖诊断说明；
补入依赖可以丰富 contains / calls 等语义关系，同一文件的 encloses 结构保持不变。

## 读取调用与引用位置

Reference 节点保留已识别的源码使用，referenceKind 区分 calls、references、extends、implements 和 decorates，
即使目标不在本次提供的 Documents 中也能查询。
例如只加入调用方文件时，仍可读取调用位置和所属声明：

```cypher
MATCH (use:Reference {referenceKind:'calls'})-[:occurs_in]->(owner)
OPTIONAL MATCH (use)-[binding:references]->(target)
RETURN use, owner, binding, target
```

没有目标的使用仍会返回，binding 和 target 为 null。查询标识符引用时改用 referenceKind:'references'；
节点的 name 和 receiver 是提取到的词法线索，Location 指向源码发生范围。
Go 访问器可通过 `g.RelationsTo(ownerID, codegraph.OccursIn)` 找使用节点，
再通过 `g.RelationsFrom(useID, codegraph.References)` 读取各候选及其证据。
`g.Find` 保持声明查询语义；这些使用节点通过 Nodes、Node、关系访问器或 Cypher 查询。

符号之间的 calls / references 与使用节点的 references 共用语言绑定规则。裸导入名首先连接 Import，
再沿 aliases 读取转导出链及最终声明；声明间的派生边直接连接最终目标。
统计声明依赖时排除 `source.kind = 'Reference'`，不要将两个查询粒度混计。
补入依赖并等待构建后，`builder.Result()` 中的原使用节点可获得目标；已有目标与证据也会重新计算，
并非只追加新关系。此前取得的 Graph 保持不变。同一路径源码发生变化时使用新的快照与 Builder。

没有 references 不保证存在 unresolved 诊断，例如语言内建对象或未发布的局部绑定也可能没有目标。
关系查询为空时，结合材料范围、Capabilities 和局部诊断判断原因。使用节点覆盖已提取的源码结构，
不保证静态分析识别了所有调用或引用；语法回退适配器目前仍主要提供声明结构。

## 读取导入、导出与修饰关系

Import / Export 以每个源码项为单位，目标缺失时仍有位置与名称信息。`Node.Binding` 保存源码
说明符、导入名、本地名、公开名、形式和 typeOnly；这些属性也可直接用于 Cypher 过滤。

```cypher
MATCH (i:Import {localName:'localFoo'})
OPTIONAL MATCH (i)-[:aliases]->(public:Export)
RETURN i, public
```

继续沿 aliases 可追溯具名转导出；namespace 别名连接组织，通配项只保留来源规则，侧效应导入
不建立 aliases。`Module / Namespace -[:exports]-> Export` 读取显式公开绑定，`occurs_in` 读取
源码归属。使用 Nodes / Node 或 Cypher 获取这些项；Find 只查询声明。

```cypher
MATCH (modifier:Reference {referenceKind:'decorates'})-[:decorates]->(declaration)
OPTIONAL MATCH (modifier)-[:references]->(definition)
RETURN modifier, declaration, definition
```

修饰器定义缺失不影响“修饰了哪个声明”这一源码关系。业务角色由消费方解释。

## 读取声明签名

`Node.Signature` 是声明头原文，`SignatureLocation` 指向对应源码范围，保留类型、参数及修饰符，
排除函数或类的实现体。支持 Go 声明以及 Python、JS/TS 的受支持声明；缺少提取规则时两者为空。
例如 Go 的 `func F(x int) int` 与 `func F(x any) any` 在图中可区分，单独修改函数体不改变签名。
签名使用原始语法，不进行格式化、类型求值或兼容性判断。

Cypher 可读取 `n.signature`、`n.signatureStartByte`、`n.signatureEndByte` 和相应行列属性；
`RETURN n` 与直接节点访问返回相同文本和位置。具体声明覆盖见 [语言能力](language-support.md)。

## 读取声明文档

`Node.Documentation` 保留普通声明文档的原文和来源。通过声明身份查找，
避免在消费方再次解析或按裸名称匹配同名方法：

```go
for _, node := range g.Find("work.go", codegraph.Function, "Work") {
    for _, doc := range node.Documentation {
        fmt.Println(doc.Text, doc.Location)
    }
}
```

`Text` 与 `Location` 指定的源码字节完全一致，包含注释定界符、字符串前缀和引号，不做去缩进、
转义求值或摘要。字节范围左闭右开，行和字节列从 1 开始。返回值可独立修改，不影响缓存或图。
消费者负责清理展示文本、生成摘要和选择注入范围。

普通文档与显式 `+doc` marker 分别读取。支持范围由 `Capability.Documentation` 和
`Limitations` 声明；空列表只表示当前规则没有提取到文档。各语言的归属规则见
[语言能力](language-support.md#声明文档)。

## 生产侧：异步构建与补料

一次性处理材料可使用 Build；需要逐批提供材料时，先 NewBuilder，再 AddDocuments。
以下片段放在已引入 context、fmt、codegraph 的调用方函数中，ctx 为该操作的上下文：

```go
builder, err := codegraph.NewBuilder("revision-1", codegraph.Options{})
if err != nil {
    return err
}
main := codegraph.Document{
    Path: "main.go", Content: []byte("package demo\nfunc Entry(){ Work() }"),
}
if err := builder.AddDocuments(ctx, main,
    codegraph.Document{Path: "work.go", Content: []byte("package demo\nfunc Work(){}")},
); err != nil {
    return err
}
task, err := builder.GetDocument(main.ID())
if err != nil {
    return err
}
facts, err := task.Wait()
if err != nil {
    return err
}
_ = facts // 可用于发现依赖，不表示跨文件关系已经发布。
report, err := builder.Wait(ctx)
if err != nil {
    return err
}
g := builder.Result() // 此后补料不会改变 g。
fmt.Println(g.Snapshot(), report.Diagnostics)
```

相关入口各自负责：

| 入口 | 用途 |
|---|---|
| AddDocuments | 接纳批次并启动后台构建 |
| AddDocument | 提交单份材料，直接取得构图材料任务 |
| GetDocument | 按已提交的 Document ID 获取构图材料任务 |
| FindAsync | 从已提交材料的单文件结果中提前查声明 |
| Wait | 等待调用前已提交工作完成，并取得构建报告 |

后台构建自行解析和发布，不依赖 Wait 触发。构建期间通过 Builder.Result 查询上一已发布批次；后续提交可能与等待中的
工作共享一次发布。GetDocument 对未提交 ID 返回 ErrDocumentNotFound。

补充依赖后可以再次提交并等待，关系会基于全部已加载材料重新解析。
before / after 应使用不同 Builder；同路径不同内容会产生 ErrSnapshotChanged。

重复分析相邻快照时，可创建 `NewExtractionCache(maxDocuments, maxSourceBytes)`，
将同一个缓存通过 `Options.ExtractionCache` 传给两个 Builder，复用未变文件的提取结果。
缓存只保留单文件事实，不复用已绑定关系；容量与生命周期见 [Document 契约](document.md)。

### 取消与容量

- 提交上下文取消会使对应构建失败；取消 Wait 仅结束本次等待，不取消后台构建。
- 单文件解析失败保留 Document 与诊断，其他文件的可用事实仍可发布。
- 预算失败、快照冲突及构建取消阻止整个失败批次发布；查询错误不返回部分行。
- Options 为材料、事实、时间和查询结果提供有限预算；MaxEvidence 与 MaxRelations 分别限制证据及关系发生数。
- 源码使用节点发布阶段的预算错误标明 `source-use`、耗尽的预算项、当前用量 `used`、待新增数量 `adding` 和对应 Options 上限；仍可用 `errors.Is(err, ErrBuildBudget)` 判断错误类别。
- ExtractionOptions.Concurrency 限制共享 Extractor 的提取并发；Builder 的材料提交接口使用 Options.BuildConcurrency。

配置字段及默认值见 [Options](../graph.go)。材料接纳与内存所有权见 [Document 契约](document.md)。

## 定位声明与成员

源码声明可直接按路径、类别和限定名查找，不需要在消费方重建 ID 算法：

```go
entries := g.Find("work.go", codegraph.Function, "Work")
for _, entry := range entries {
    callers := g.RelationsTo(entry.ID, codegraph.Calls)
    _ = callers
}
```

Find 按源码位置排序，空 kind 或限定名表示通配。Node、RelationsFrom、RelationsTo 返回独立副本。
声明位置包含起止行列，可用于将变更范围映射到声明；字节范围为左闭右开，行和字节列从 1 开始。

没有单一源码发生位置的组织节点，其 Location 为空；从入向 declares 查询贡献材料。
源码声明的 Class 等节点仍可保留自身位置。组织身份和成员规则见 [命名空间组织](namespaces.md)。

查询 Go 包成员及其来源：

```cypher
MATCH (:Package {name:$package})-[:contains]->(member)<-[:declares]-(source:Document)
RETURN member, source
```

查询结构体直接字段：

```cypher
MATCH (s:Struct)-[:contains]->(f:Field)
RETURN s, f
```

## 查询关系与证据

Query 首次调用时建立索引，接受参数化、只读 Cypher，返回 Node、Relation、Path 或普通 Go 值。
索引建立失败或取消可重试，已经发布的类型化图仍可用。
不允许写操作和过程调用；变长路径必须有明确上限。例如查询两跳以内、每条边都有 exact 证据的调用路径：

```cypher
MATCH p = (a {id: $nodeID})-[:calls*1..2]->(target)
WHERE all(r IN relationships(p) WHERE r.confidence = 'exact')
RETURN target, p, [r IN relationships(p) | r.line] AS lines
```

使用 n.id / r.id 读取源码侧身份；Cypher 的 id(n) 是图引擎内部标识。
Path 的节点顺序表示遍历方向，关系保留存储方向。返回值与底层图隔离，可以由调用方独立使用。
普通值包括 string、int64、float64、bool、null、list 和 map，其他 Cypher 专有值返回错误。

### 属性投影

| 对象 | 常用查询属性 |
|---|---|
| Node | id、kind、name、qualifiedName、language、snapshot；有源码位置时提供 path、line、column、endLine、endColumn、startByte、endByte |
| 声明名称位置 | nameStartByte、nameEndByte、nameLine、nameColumn、nameEndLine、nameEndColumn；RETURN n 返回 Node.NameLocation |
| Reference | referenceKind 为 calls / references / extends / implements / decorates；receiver 为词法接收者线索；name 与位置标识本次使用，QualifiedName 不伪装成已绑定目标 |
| Import / Export | specifier、importedName、localName、exportedName、form、typeOnly；RETURN n 返回 Node.Binding |
| Gitlink Document | gitlink，表示父仓固定的子仓 commit |
| Documentation | documentation 为原文列表，documentationData 为含源码位置的完整结构 JSON；RETURN n 返回 Node.Documentation |
| Marker | markers 为种类列表，spec/case/rule/link/doc 为内容列表，markerData 为完整结构 JSON |
| Relation | id、kind、source、target、confidence、bases、evidenceData 及发生位置 |

Node.Kind 对应具体标签。完整类别见 [NodeKind](../model.go)，某语言实际支持的类别见 Capabilities。
复杂结构提供便于筛选的属性与完整 JSON，返回实体时还原为结构化值。

Relation.Evidence 保存各条 Basis、Confidence 和可选支撑位置；r.bases 用于过滤推导规则，
r.evidenceData 返回完整 JSON，RETURN r 返回结构化证据。例如：

```cypher
MATCH ()-[r:contains]->()
WHERE 'receiver_declaration' IN r.bases
RETURN r
```

`Relation.Confidence` 由 `Evidence[].Confidence` 推导，取独立证据中的最高档；
证据自身的必要推导链取最低档，候选或证据数量增加不会升级。四档从高到低为
`exact`、`scoped`、`name_only`、`heuristic`，不使用字符串大小判断强弱。
Go 调用方可用 `r.Confidence.AtLeast(Scoped)`；Cypher 显式列出所需档位：

```cypher
MATCH ()-[r:calls]->()
WHERE r.confidence IN ['exact', 'scoped']
RETURN r
```

Relation、Path、Subgraph 与 `r.confidence` 都保留同一派生值，`r.evidenceData` 保留完整证据。
关系发生与依据的区别见 [内核设计](kernel.md)。

### 意图标记

```go
// +spec=`Calls must preserve source locations`
// +case:id=parallel,expect=`Preserve both call sites`
// +rule=`Do not merge relations by endpoints`
// +link=docs/kernel.md
// +doc=`Entry function`
func Entry() { Work(); Work() }
```

Go 从声明文档注释提取 marker；Python 与 JS/TS 从声明前置注释提取，包含 export 包装。
结构化内容原样保留，不求值其中的表达式。marker 作为声明属性参与查询，完整来源保存在 markerData。

## 理解构建结果

先检查 error，再结合 BuildReport.Diagnostics 判断局部事实的适用范围：

- 候选关系已有目标及证据，不重复生成歧义诊断；缺少目标时保留未解析诊断。
- 诊断标记受影响的 subject、源码范围和已知的关系类别，便于按本次任务筛选缺口。
- outline 计数描述提取候选，不代表源码全部声明；上游未提供遗漏范围时使用完整文档位置。
- 候选去重不等于事实遗漏；已有导入、声明和无关关系继续可用。

CodeGraph 不给整个局部图作业务可用性判定。是否补料、传播影响或扩大检查范围由消费者决定。
判断语言能力时同时阅读 [语言能力](language-support.md) 与本次报告，不能只看诊断总数。
