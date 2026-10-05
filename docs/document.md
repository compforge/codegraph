# Document 材料契约

## 定位与身份

Document 是调用方提供的一份快照材料，由逻辑路径及源码内容或 gitlink 固定 commit 组成。
它可以来自文件系统、Git revision 或内存；材料获取、仓库发现与依赖枚举由调用方负责。
CodeGraph 只分析显式提供的材料，使相同输入的结果不受本机 checkout 状态影响。

路径使用快照相对、斜杠分隔的逻辑名称，满足 fs.ValidPath，不是绝对路径或 URL。
路径不必在磁盘存在，用于材料身份、语言识别、相对导入上下文及源码位置。
快照身份由 Graph 持有；Document.ID 与对应 Document 节点 ID 一致。

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
不产生声明和关系。是否纳入非代码材料由调用方选择。
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
沿用构建预算；不会触发额外读取或下载。失败批次不发布部分图，构建与等待的取消语义见 [使用指南](usage.md)。

## 图中表达

每份材料对应一个 Document 节点，Go 常量为 DocumentKind，Cypher 标签为 Document。
源码通过 declares 连接声明或组织贡献；Package、Module 等实体通过 contains 组织直接成员。
Document 的来源身份与 Namespace 的成员身份各有职责，允许多个文件贡献同一个组织。

gitlink 只保留材料边界及指向它的导入证据，不产生内部声明。
组织的身份、嵌套和贡献位置见 [命名空间组织](namespaces.md)，核心模型见 [内核设计](kernel.md)。
