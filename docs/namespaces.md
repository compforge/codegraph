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

单文件提取保留源码事实，语言 Organizer 根据已加载材料建立 Package / Module 身份与组织关系。
Core 把组织关系、显式 Namespace 和 Class 等声明内的直接成员统一登记；Binder 补充跨文件接收者归属。
成员查找与继承遍历消费同一份 contains 索引，随后由 Resolver 解析引用与调用。

词法作用域与成员归属分别建模：contains 不决定某个源码位置的名称查找顺序。
语言实现负责导入别名、遮蔽、可见性及导出规则，Core 负责在同一快照中原子发布节点、关系及诊断。
阶段职责见[语言构建流程](language-pipeline.md)。

Go imports 指向 Package，每条导入语句对一个包只建立一条边。Python imports 指向 Module /
Package，具名导入另保留最终声明目标；模块名称引用也指向相应组织节点。调用与声明引用仍直接
连接实际目标。JS/TS 使用现有 Document 模块绑定，其显式声明可继续使用具体 Module / Namespace Kind。

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
- `.py` / `.pyi` 是不同来源候选，不通过同名自动合并；导入存在多个来源时保留 candidate。
- Python 限定名只拼接已证明的普通包祖先。补入父包可以丰富显示名，既有节点 ID 不变。
  快照根目录的初始化文件没有已知包名时，以 `.` 表达源码锚点，不猜测运行时导入名。

组织节点没有单一源码位置，`Node.Location` 为 nil；JSON 省略 location，Cypher 不提供虚构的
path/line 等源码属性。通过入向 declares 读取所有贡献及位置。普通声明和 Document 继续保留位置；
`Find` 只按具体源码位置查声明。返回值包含独立的位置副本，修改查询结果不会污染图。

声明 ID、Document ID 与组织 ID 都属于源码快照侧身份，不使用图引擎内部数字 ID。
新组织节点、来源与嵌套边都计入构图预算，失败或取消不会发布半个组织层级。

## 语言证据边界

Go 同目录同 package 名的已加载文件共同贡献 Package；测试文件不成为生产 import 的目标。
Go 目录层次不产生父子 Package，go.mod 对应的依赖管理 module 不作为这层 Namespace。

Python 根据已加载的 `__init__.py` / `__init__.pyi` 建立普通包与直接子包、子模块的 contains。
目录向上收敛，组织层级无环。没有初始化材料时保留文件 Module，不把普通目录猜成 Package；
namespace package 的跨来源合并和动态搜索路径未解析。绝对导入仍保留 candidate。

明确的源码贡献与直接归属使用 exact；多个包/模块候选使用 candidate。exact 表达已加载范围的
源码证据，不证明运行时导入配置或包成员完整性。缺少导入目标保留局部诊断，不虚构外部依赖节点。
组织关系用于查询代码事实，是否沿它扩大影响范围由消费者决定。

契约测试与固定仓库测量见[组织复测记录](../tests/corpus/namespaces.md)。
