# gitlink 材料

## 父仓中的子仓边界

Git mode `160000` 的树条目只记录子仓路径与固定 commit。调用方将该条目作为 Document 提供，
CodeGraph 保留同一份父仓事实；即使本机已有子仓 checkout，图的结果也不依赖其工作区状态。
gitlink 表示父仓的一条材料记录，本身不声明代码依赖；导入关系来自源码中的 import 证据。
材料获取、Git 元数据读取及包名映射由调用方负责。

```go
codegraph.Document{
    Path:    "sdk",
    Gitlink: "0123456789abcdef0123456789abcdef01234567",
}
```

`Content` 必须为空，commit 必须为完整的 40 或 64 位十六进制对象 ID。
`Extract` 返回带 Gitlink 的事实；入图后仍是 `document:sdk`、Kind 为 `Document`，
可通过 `Node.Gitlink` 或 Cypher `n.gitlink` 查询固定版本。此材料不需要 grammar，也不产生
`unsupported_language` 诊断。其位置仅标识逻辑路径，字节范围为空。

## 导入如何连接

语言适配器先按自己的规则计算 import 路径；路径位于显式 gitlink 内时，创建从导入方 Document
到 gitlink Document 的 `imports` 边。每个导入位置保留独立证据，basis 为 `gitlink_boundary`。

- JS/TS/TSX 相对路径支持直接指向边界或其内部路径。
- Python 使用现有模块路径规则：显式相对导入可为 exact，绝对导入仍为 candidate。
- Go 使用调用方提供的 ModulePath 将模块内导入换算为路径。

路径前缀按目录段匹配，`sdk-extra` 不属于 `sdk`。包名如 `@example/sdk` 无法由 gitlink commit
推断，仍沿用未解析导入的行为。目标内部的 Module、Package、符号、调用关系需要源码证据；
导入边成立并不意味着内部绑定已完成，相关未解析符号诊断仍会保留。

```cypher
MATCH (source:Document)-[r:imports]->(gitlink:Document)
WHERE r.basis = 'gitlink_boundary'
RETURN source.path, gitlink.path, gitlink.gitlink, r.confidence
```

## 快照与预算

同一 Graph 中，gitlink 及其后代材料不能同时存在：父仓固定 commit 与任意子仓工作区源码
不构成同一份快照。批次入队前检查这一边界，也覆盖此前入队和已发布的材料。

路径相同而 commit 或材料类型不同，按快照内容冲突处理。提取缓存也包含这些身份信息，
避免把普通源码提取结果用于 gitlink。commit 字节计入材料容量预算，Document 和 Node 数量
沿用已有预算；它们不触发额外文件读取或后台下载。
