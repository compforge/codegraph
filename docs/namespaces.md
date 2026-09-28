# 命名空间组织

## 概念与关系

Document 是源码材料，Symbol 是代码声明，Namespace 是组织名称与成员的语言单元。
Symbol 与 Namespace 是允许重叠的逻辑概念，Class 自身也组织成员；图中只使用具体的
Document、Package、Module、Class、Function 等 Kind，不增加 Symbol 标签或互斥三分类字段。
具体 Namespace Kind 留给语言显式声明的 namespace。

`declares` 从 Document 指向它声明或构成的节点，保留源码贡献位置。`contains` 从语义所有者
指向直接成员，支持 Namespace 任意层级嵌套；祖先关系通过有界路径查询获得。

```text
Document: src/app/__init__.py ─declares→ Package: app
                                           │ contains
Document: src/app/api/__init__.py ─declares→ Package: app.api
                                           │ contains
Document: src/app/api/user.py ────declares→ Module: app.api.user
                     │                     │ contains
                     ├───────────declares→ Class: User
                     │                     │ contains
                     └───────────declares→ Method: save
```

源码来源与语义归属可以跨文件。Go 一个 Package 可以有多份 Document 贡献，接收者方法的
Document 可以与类型不同。方法归属接收者类型；包级成员归属 Package。找不到接收者时保留
局部诊断和源码贡献，不假定方法是包级函数。再导出产生导入绑定关系，不改写原声明归属。

## 构建与名称解析

语言 Organizer 给出实体及 Document 的语义根，全部实体登记后形成成员视图，
Binder 补充跨文件接收者归属。组织与 Class 共用成员查询入口，源码贡献从 declares 读取。
成员索引和已绑定的 extends 供后续引用与调用解析使用，阶段接口见 [语言构建流程](language-pipeline.md)。

词法 Scope/Binding 负责可见性与遮蔽，contains 负责成员归属；二者的关系见 [内核设计](kernel.md)。

Go imports 指向 Package，每条导入语句对一个包只建立一条边。Python imports 指向 Module /
Package，具名导入另保留最终声明目标；模块名称引用也指向相应组织节点。调用与声明引用仍直接
连接实际目标。JS/TS 文件以 Module 组织顶层成员，模块导入及模块名称引用指向 Module；
显式 Namespace/Class 按声明嵌套在其中。Document 保留材料身份及源码贡献。

`Extract`、`GetDocument` 和 `FindAsync` 提供单文件声明事实，不提前承诺跨材料组织结果。
`Wait` 后通过 Cypher 或 `Node`、`RelationsFrom`、`RelationsTo` 查询组织与贡献：

```cypher
MATCH (:Document {path:$path})-[:declares]->(p:Package)
RETURN p
```

```cypher
MATCH (:Package {name:$name})-[:contains*1..8]->(member)
RETURN member
```

## 身份与位置

组织 ID 使用语言、具体类别和源码锚点，不使用首个成员、成员列表或加载顺序：

- Go Package 由逻辑目录和 package 名锚定；不同目录以及 `p` / `p_test` 不合并。
- Python Module 由文件路径锚定，普通 Package 由初始化文件所在目录及源码/类型桩形式锚定。
- JS/TS 文件 Module 由语言与源码路径锚定，限定名使用去扩展名的逻辑路径。
- `.py` / `.pyi` 是不同来源候选，不通过同名自动合并；导入存在多个来源时保留 scoped。
- Python 限定名只拼接已证明的普通包祖先。补入父包可以丰富显示名，既有节点 ID 不变。
  快照根目录的初始化文件没有已知包名时，以 `.` 表达源码锚点，不猜测运行时导入名。

没有单一源码发生位置的合成组织节点，其 `Node.Location` 为 nil；JSON 省略 location，
Cypher 不提供虚构的 path/line 等源码属性。通过入向 declares 读取所有贡献及位置。普通声明和 Document 继续保留位置；
`Find` 只按具体源码位置查声明。返回值包含独立的位置副本，修改查询结果不会污染图。

Document 使用材料路径身份；声明及合成实体统一使用 `node:` 身份前缀，其稳定键分别来自声明位置
和语言锚点。ID 不承载 Symbol/Namespace 逻辑角色，也不使用图引擎内部数字 ID。
新组织节点、来源与嵌套边都计入构图预算，失败或取消不会发布半个组织层级。

## 语言证据边界

Go 同目录同 package 名的已加载文件共同贡献 Package；测试文件不成为生产 import 的目标。
Go 目录层次不产生父子 Package，go.mod 对应的依赖管理 module 不作为这层 Namespace。

Python 根据已加载的 `__init__.py` / `__init__.pyi` 建立普通包与直接子包、子模块的 contains。
目录向上收敛，组织层级无环。没有初始化材料时保留文件 Module，不把普通目录猜成 Package；
namespace package 的跨来源合并和动态搜索路径未解析。绝对导入仍保留 scoped。

JS/TS 文件 Module 表达所提供源码单元的静态组织，不证明 ESM/CommonJS 加载模式，也不合并
脚本的运行时全局名称。扩展名候选、导出与再导出仍遵循语言绑定策略；同名不同路径不合并。

明确的源码贡献与直接归属使用 exact；多个包/模块候选使用 scoped。exact 表达已加载范围的
源码证据，不证明运行时导入配置或包成员完整性。缺少导入目标保留局部诊断，不虚构外部依赖节点。
组织关系用于查询代码事实，是否沿它扩大影响范围由消费者决定。

契约测试与固定仓库测量见[统一实体模型验证](../tests/corpus/namespace-model.md)。
