# 使用指南

本篇按材料准备、构建、定位与查询的顺序介绍使用方式。首次 Build 示例见 [README](../README.zh-CN.md)。
字段和方法的精确契约以源码中的公共 API 注释为准；设计理由见 [内核设计](kernel.md)。

## 准备材料

调用方负责获取同一快照的材料，将完整源码放入 Document.Content，并提供快照内的逻辑路径。
路径不必对应本地文件，但需要为语言识别和相对导入提供上下文。
源码、无 grammar 材料和 gitlink 的处理见 [Document 契约](document.md)。

同一批次可包含多种语言；ModulePath 只影响 Go 模块导入。
CodeGraph 不跨语言按同名绑定，不自动获取依赖。缺少材料时可以先消费局部事实，再决定是否补料。

`Extract` 提取单 Document 事实而不发布图状态，适合构图前探索 import 或其他线索。
事实包括声明、导入绑定、调用候选、引用、类型关系和 marker；语句事实的语言覆盖见 [语言能力](language-support.md)。
同内容的后续构建复用提取缓存。单文件事实和已解析的跨文件关系是不同产物，不能互相替代。

## 构建与补料

一次性处理材料可使用 Build；需要逐批提供材料时，先 New，再 AddDocuments。
以下片段放在已引入 context、fmt、codegraph 的调用方函数中，ctx 为该操作的上下文：

```go
g, err := codegraph.New("revision-1", codegraph.Options{})
if err != nil {
    return err
}
main := codegraph.Document{
    Path: "main.go", Content: []byte("package demo\nfunc Entry(){ Work() }"),
}
if err := g.AddDocuments(ctx, main,
    codegraph.Document{Path: "work.go", Content: []byte("package demo\nfunc Work(){}")},
); err != nil {
    return err
}
task, err := g.GetDocument(main.ID())
if err != nil {
    return err
}
facts, err := task.Wait()
if err != nil {
    return err
}
_ = facts // 可用于发现依赖，不表示跨文件关系已经发布。
report, err := g.Wait(ctx)
if err != nil {
    return err
}
fmt.Println(report.Diagnostics)
```

相关入口各自负责：

| 入口 | 用途 |
|---|---|
| AddDocuments | 接纳批次并启动后台构建 |
| AddDocument | 提交单份材料，直接取得事实任务 |
| GetDocument | 按已提交的 Document ID 获取事实任务 |
| FindAsync | 从已提交材料的单文件结果中提前查声明 |
| Wait | 等待调用前已提交工作完成，并取得构建报告 |

后台构建自行解析和发布，不依赖 Wait 触发。构建期间查询上一已发布批次；后续提交可能与等待中的
工作共享一次发布。GetDocument 对未提交 ID 返回 ErrDocumentNotFound。

补充依赖后可以再次提交并等待，关系会基于全部已加载材料重新解析。
before / after 应使用不同 Graph；同路径不同内容会产生 ErrSnapshotChanged。

重复分析相邻快照时，可创建 `NewExtractionCache(maxDocuments, maxSourceBytes)`，
将同一个缓存通过 `Options.ExtractionCache` 传给两个 Graph，复用未变文件的提取结果。
缓存只保留单文件事实，不复用已绑定关系；容量与生命周期见 [Document 契约](document.md)。

### 取消与容量

- 提交上下文取消会使对应构建失败；取消 Wait 仅结束本次等待，不取消后台构建。
- 单文件解析失败保留 Document 与诊断，其他文件的可用事实仍可发布。
- 预算失败、快照冲突及构建取消阻止整个失败批次发布；查询错误不返回部分行。
- Options 为材料、事实、时间和查询结果提供有限预算；MaxEvidence 与 MaxRelations 分别限制证据及关系发生数。
- BuildConcurrency 限制每个 Graph 的提取并发；同时使用多个 Graph 时由调用方控制总容量。

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

Query 接受参数化、只读 Cypher，返回 Node、Relation、Path 或普通 Go 值。
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
| Gitlink Document | gitlink，表示父仓固定的子仓 commit |
| Marker | markers 为种类列表，spec/case/rule/link/doc 为内容列表，markerData 为完整结构 JSON |
| Relation | id、kind、source、target、confidence、bases、evidenceData 及发生位置 |

Node.Kind 对应具体标签。完整类别见 [NodeKind](../node.go)，某语言实际支持的类别见 Capabilities。
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
