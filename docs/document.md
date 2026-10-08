# Document 材料契约

## 定位与身份

Document 是调用方提供的一份快照材料，由逻辑路径及文件内容或 gitlink 固定 commit 组成。
它可以来自文件系统、Git revision 或内存；材料获取、仓库发现与依赖枚举由调用方负责。
CodeGraph 只分析显式提供的材料，使相同输入的结果不受本机 checkout 状态影响。

路径使用快照相对、斜杠分隔的逻辑名称，满足 fs.ValidPath，不是绝对路径或 URL。
路径不必在磁盘存在，用于材料身份、语言识别、相对导入上下文及源码位置。
快照身份由 Graph 持有；Document.ID 与对应 Document 节点 ID 一致。

## 材料类别与语法格式

图中的 Document 节点通过 DocumentKind 区分 source、gitlink 和 unknown；Language 描述 grammar。
JSON/TOML/gomod 等数据或清单材料使用 unknown，清单角色由 manifest 标签表达。
Gitlink 由调用方提供的 Git 元数据识别，分类不依赖路径后缀。
Document 与 Directory 的 Tags 保存路径匹配结果，同一节点可以同时属于多个分类。

输入 Document 与图中的 DocumentNode 职责不同：前者提供材料，后者组合 Node，作为同一图节点的
类型化查询结果。`g.Document(path)`、`g.Node(DocumentID(path))` 和 Cypher 读取相同事实，
返回值均可独立修改；DocumentNode 不另存成员或 outline。

## Directory 与路径标签

Builder 根据已接纳 Document 的路径补齐祖先 Directory，包括根目录 `.`，同一路径只创建一个节点。
Directory 的 Path 是快照相对路径，不带末尾斜杠；其身份为 `DirectoryID(path)`。
`g.Directory(path)` 返回同一图节点的类型化视图。目录存在只证明它是已提供材料的祖先，
不证明内容完整，也不触发文件系统扫描。没有输入材料时不生成目录；gitlink 自身仍是 Document。

Document 和子 Directory 通过 `in_directory` 指向直接父 Directory。它表达路径结构；
语言 Package、Module 等节点由适配器组织，namespace 查询继续使用语义关系。
Directory 不具有源码 Location，其路径可通过 Node.Path 或 Cypher 的 path 属性查询。

`Tag` 是以 string 为底层类型的开放枚举，内置 ManifestTag、GeneratedTag、TestFixtureTag、
DependencyTag、BuildOutputTag、CacheTag、MinifiedTag，也允许调用方定义新值。
`TagRule{Name, Pattern}` 按规范化路径应用 Go 正则；规则可以匹配 Document 和 Directory。
默认使用 `BuiltinTagRules()`：支持清单、常见生成文件、测试素材、依赖、构建输出、缓存及压缩资源。
nil TagRules 选择内置规则，显式空集合关闭标签，非空集合替换内置规则；追加方式如下：

```go
rules := append(codegraph.BuiltinTagRules(), codegraph.TagRule{
    Name:    codegraph.Tag("test"),
    Pattern: `(^|/)tests?(/|$)`,
})
builder, err := codegraph.NewBuilder("revision", codegraph.Options{TagRules: rules})
```

只需要路径分类时，可以独立创建匹配器，无需提供 Document 或构图：

```go
matcher, err := codegraph.NewTagMatcher(rules)
if err != nil {
    return err
}
tags := matcher.Match("tests/input.pb.go")
```

`NewTagMatcher` 校验并编译规则，nil、空集合和替换规则的含义与 Builder 一致。
匹配器创建后固定，可以并发复用；调用方后续修改规则或返回的标签不会影响它。
`Match` 接收规范化的快照相对路径，使用 `/` 分隔、目录无末尾斜杠、根目录为 `.`。
Builder 使用同一个匹配器实现，为已接纳的材料和目录计算标签。

所有匹配结果累加，同名标签去重并按名称排序；
规则顺序不影响结果。正则默认允许部分匹配，完整匹配需要显式添加 `^` 和 `$`。
节点标签只来自自身路径匹配，不继承祖先标签。例如 `^vendor$` 只标记 vendor 目录，
`(^|/)vendor(/|$)` 则分别匹配目录与其后代材料。

标签在构图阶段计算，不进入提取缓存，也不控制 parser、语义解析或材料排除。
同一份 Facts 可以在不同 Builder 中使用不同标签规则；未知格式和解析失败的 Document 仍保留路径标签。
是否根据 dependency 或 generated 跳过评审由消费者决定。

```cypher
MATCH (d:Document)-[:in_directory]->(p:Directory)
WHERE 'generated' IN d.tags
RETURN d.path, d.tags, p.path
```

## Manifest 材料

`go.mod`、`pyproject.toml` 和 `package.json` 分别复用 gotreesitter 的 gomod、toml、json grammar。
格式适配器从语法树提取脱离 parser 的元数据，发布到 Document 的 Manifest 属性，保留名称与版本的
源码位置。直接声明的项目名、版本及 project、build-system、workspace 结构存在性可查询；
依赖约束、版本求解、构建执行及动态版本计算不在该能力范围内。

- go.mod 声明的 Module 通过 declares 与材料连接；只提供清单也能保留模块身份。
  源码所属 Package 通过 contains 归属该 Module，组织与导入解析共用模块上下文。
- pyproject.toml 的 project 名和 package.json 的 name 是打包项目元数据，不能直接替代 Python
  import Package 或 JS/TS 文件 Module，也不能自动判定工程 Component。
- pyproject 支持普通、引号、点号和内联表的单行字符串元数据。多行字符串及不支持的字段值
  保留 unsupported_manifest 诊断；没有静态 version 不代表项目没有版本。

显式提供的 go.mod 声明优先于同根的 ModulePath / GoModules 提示，冲突发布
conflicting_module_context 诊断。不同根按最近模块边界组织，嵌套 module 是独立所有者。
补充清单会在下一次 Build 重新组织和绑定；同一快照中替换清单内容仍属于材料冲突。
默认规则按路径添加 manifest 标签，解析失败仍保留该标签；失败清单不发布猜测的元数据或声明。
修改标签规则不会改变格式识别与清单解析；为普通文件添加 manifest 标签也不会使其产生清单语义。

```cypher
MATCH (d:Document)
WHERE 'manifest' IN d.tags
RETURN d.path, d.manifestFormat, d.manifestName, d.manifestVersion
```

```cypher
MATCH (d:Document {path:'go.mod'})-[:declares]->(m:Module)-[:contains]->(p:Package)
RETURN d.path, m.name, p.qualifiedName
```

## 源码材料

源码 Document 提供完整 Content，源码位置从该内容的起点计算：

```go
codegraph.Document{
    Path:    "src/main.go",
    Content: []byte("package demo\nfunc Entry() {}"),
}
```

注册 grammar 的源码由语言适配器提取事实；解析能力与关系绑定能力分别声明。
没有 grammar 时仍保留 Document 节点，并以 unsupported_language 报告覆盖缺口，
保留路径结构，不产生语言声明与语义关系。是否纳入非代码材料由调用方选择。
源码解析失败同样保留材料身份及文档级诊断，不补造声明。

Extractor 统一分类与提取材料，返回可独立传给 Builder 的 Facts。
Facts 保留完整只读构图材料，可跨快照复用；公开字段用于生产侧检查与依赖探索。
消费侧通过 Document 节点及其 declares、encloses 关系读取文件结构，见 [使用指南](usage.md#从-graph-读取文件结构)。

调用方可用 `ExtractionOptions.Cache` 在共享 Extractor 内复用单文件事实，
Builder 的材料提交接口也支持 `Options.ExtractionCache`。缓存身份包含逻辑路径、
材料类型及内容摘要，保留独立于 parser 的原始事实；每个快照仍重新组织和绑定关系。
缓存由调用方限定生命周期、文档版本数和源码字节容量，达到容量后正常提取而不新增缓存。
缓存命中不绕过 Graph 的范围、接纳及构建预算；共享期间 grammar 注册应保持不变。

## Gitlink 材料

### 父仓中的子仓边界

Git mode 160000 的树条目只记录子仓路径与固定 commit。调用方将其作为 Document 提供，
CodeGraph 保存父仓事实，不展开子仓源码。gitlink 本身不声明依赖；导入关系来自源码 import。
Git 元数据读取及包名映射仍归调用方。

```go
codegraph.Document{
    Path:    "sdk",
    Gitlink: "0123456789abcdef0123456789abcdef01234567",
}
```

Content 必须为空，commit 为完整的 40 或 64 位十六进制对象 ID。
Extract 返回带 Gitlink 的事实；入图后仍是 Document，可通过 Node.Gitlink 或 n.gitlink 查询版本。
此材料不需要 grammar，也不产生 unsupported_language；其位置仅标识逻辑路径，字节范围为空。

### 导入如何连接

语言适配器按自身规则计算 import 路径；路径位于显式 gitlink 内时，从导入方 Document
向 gitlink Document 建立 imports。每个导入位置保留独立证据，Basis 为 gitlink_boundary。

- JS/TS/TSX 相对路径支持直接指向边界或其内部路径。
- Python 使用模块路径规则：显式相对导入可为 exact，绝对导入仍为 scoped。
- Go 使用调用方提供的 ModulePath / GoModules 组织 Module 与 Package，并将模块内导入换算为路径。

路径前缀按目录段匹配，sdk-extra 不属于 sdk。@example/sdk 等包名不能从 commit 推断，
缺少路径证据时仍保留未解析导入。内部 Module、Package、声明与调用需要子仓源码；
连接到边界不证明内部符号绑定已经完成，相关缺口仍保留。

```cypher
MATCH (source:Document)-[r:imports]->(gitlink:Document)
WHERE 'gitlink_boundary' IN r.bases
RETURN source.path, gitlink.path, gitlink.gitlink, r.confidence
```

### 子仓快照

同一 Graph 中，gitlink 及其后代材料不能同时存在：父仓固定 commit 与任意子仓工作区源码
不构成同一份快照。检查覆盖新批次、此前入队和已发布的材料。
需要分析内部源码时，由调用方另建子仓快照。

## 接纳、冲突与预算

输入在接纳时复制，调用方可在提交返回后复用缓冲区。相同路径、材料类型与内容重复提交保持幂等；
内容、commit 或材料类型冲突时拒绝该次提交，保留已发布图。
提取缓存同样包含材料类型和内容身份，避免跨材料或跨版本复用错误事实。

材料接纳和构建受有限预算约束。gitlink commit 字节计入材料容量，Document 和 Node 数量
沿用构建预算；Directory、in_directory 及其证据也计入节点、关系与证据预算，不增加 Document 计数。
不会触发额外读取或下载。失败批次不发布部分图，构建与等待的取消语义见 [使用指南](usage.md)。

## 图中表达

每份材料对应一个 Document 节点，Go 常量为 DocumentNodeKind，Cypher 标签为 Document。
源码通过 declares 连接声明或组织贡献，通过 in_namespace 连接语言确定的组织根；
Package、Module 等实体通过 contains 组织直接成员。路径层级通过 in_directory 连接到 Directory，
路径对应的语言语义由语言适配器解释。
Document 的来源身份与 Namespace 的成员身份各有职责，允许多个文件贡献同一个组织。

gitlink 只保留材料边界及指向它的导入证据，不产生内部声明。
组织的身份、嵌套和贡献位置见 [命名空间组织](namespaces.md)，核心模型见 [内核设计](kernel.md)。
