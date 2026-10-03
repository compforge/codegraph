# 语言能力与限制

本篇说明语言适配器能从已提供源码中建立哪些证据，以及哪些行为仍缺少静态依据。
组织身份见 [命名空间组织](namespaces.md)，实现接入见 [语言构建流程](language-pipeline.md)。

## 查询能力

| API | 表达的信息 |
|---|---|
| Language(path) | 路径对应的注册 grammar |
| Languages() | 已注册的 grammar 名称，不加载全部 parser |
| Capabilities() | 内置语言适配器的组织、声明、使用位置、关系、文档、marker 与限制 |
| Capabilities("rust", "java") | 按需查询其他注册语言的 outline 能力；未知名称不返回能力 |

grammar 可用不表示语义完整。具体类别与语言覆盖以实际注册项、契约测试及本次 BuildReport 为准。
没有 grammar 的材料处理见 [Document 契约](document.md)。

## 文件结构

Graph 通过 encloses 和声明位置保留词法嵌套，通过 contains 保留语义成员归属。
消费侧从节点和关系生成文件 outline，见 [从 Graph 读取文件结构](usage.md#从-graph-读取文件结构)。

Python 和通用适配器使用注册的声明 query，JS/TS 使用适配器维护的声明 query，
内部复用 gotreesitter Outline 提取声明与层级；Go 通过专有声明分析提取。
结构视图覆盖图已接纳的声明，query 遗漏及未支持的结构由 BuildReport 说明。

## 声明文档

`Capability.Documentation` 表示适配器支持普通声明文档提取；`Limitations` 同时声明语言规则。
文档复用已有解析过程，绑定到具体声明发生位置，不按裸名称关联，也不从普通文档推导 marker。

| 语言 | 提取范围与归属规则 |
|---|---|
| Go | AST 关联的声明、字段和接口方法前置文档注释；单项声明可继承组注释，多项组注释不分配给某个成员；不提取行尾注释 |
| Python | 类和函数体的首条普通字符串表达式，包括嵌套、decorator、async、括号和相邻字符串；排除 bytes、f-string、后续字符串与模块 docstring |
| JS / TS / TSX | 已提取声明紧邻的独立 JSDoc 块，支持 export、ambient 和单项变量声明包装；排除空行分隔、行尾及多项变量语句的共享注释 |
| 其他 grammar | 尚未提供普通文档提取，Documentation 为 false |

源码原文及坐标契约见 [使用指南](usage.md#读取声明文档)。空列表表示当前提取规则没有得到文档，
不能据此判断源码在所有文档约定下都没有说明。

## 共同证据规则

Go、Python、JS/TS 保留标识符使用位置及其最内层声明归属；声明名、注释和字符串不作为引用。
Graph 的 Reference 节点以 referenceKind 保留使用角色，occurs_in 连接最内层已保留声明或 Document。
references 连接直接目标或本地 Import；Import / Export 的 aliases 保留名称链。声明间的 calls /
references 继续连接最终目标，共用语言解析规则；它们与源码位置边是不同的查询粒度。
未知目标保留源码节点，已知遮蔽阻止同名回退，不为内建对象或未保留局部声明制造目标。
Capabilities.References 声明可提取角色，SourceItems 声明 Import / Export 支持。

| 语言 | Reference 角色 | 模块项与修饰覆盖 |
|---|---|---|
| Go | calls、references、extends | Import 保存显式别名及未解析路径；隐式本地名依赖已加载包名；无独立 Export 语法节点 |
| Python | calls、references、extends、decorates | Import 保留每项名称和 alias；decorator 应用连接已保留声明；不以 Export 伪造隐式公开名称或动态 `__all__` |
| JS / TS / TSX | calls、references、extends、decorates；TS / TSX 另有 implements | Import / Export 保留具名、default、namespace、通配、侧效应项及 typeOnly；支持嵌套具名 namespace 的显式导出 |
| 其他 grammar | 未提供 | 声明支持不自动获得源码使用、导入项或修饰应用能力 |

动态基类表达式保留角色与原文，不求值；未解析基类/接口的角色不依赖目标是否入图。
非字面量动态导入保存 dynamic 项和原文；默认导出表达式没有已保留声明时保存 Export 和诊断。
不将语法错误恢复结果当作可靠提取：例如当前 grammar 不接受的 TypeScript `export type *`
仍按解析缺口处理。Annotation 支持受语言适配器限制，Java 等只有声明能力的语言尚未提供修饰关系。

imports 连接语言组织单元，具名导入及显式转导出还可连接最终声明。
模块绑定供引用、调用及显式类型关系共用；候选模块或目标的不确定性沿转导出链保留。
路径长度不会自动降低关系 confidence，业务传播策略由消费者定义。

Facts.Calls.Targets 保存语法提出的名称、接收者类型、模块限定及依据，不表示运行时分派结果。
候选方法可以沿已绑定的 extends 查找；直接同名方法停止该分支，多个基类保留候选，重复祖先去重。
继承调用使用 scoped / inherited_method，并受导入与 extends 必要环节的精度上限约束；implements 不作为继承依据。
已知接收者或类型的调用采用 scoped；缺少接收者约束、仅按方法名提出的目标采用 name_only，
即使只有一个同名方法也不升级。具体分级由已有语义证据决定，不由 Basis 名称直接决定。
动态实现选择、运行时方法修改、未知回调及未建模闭包保留缺口。

候选解析消费已加载材料，遵守取消与预算；补料后重解析，失败批次不发布部分关系。

## Go

### 声明与组织

提取函数、方法、命名 struct/interface、字段、接口显式方法、其他命名类型、类型别名、
单名称变量和常量，以及声明文档注释 marker。Type 表达其他命名类型，TypeAlias 表达显式别名，
不会因右侧语法而丢失别名身份。

命名 struct/interface 只展开直接成员，匿名嵌套类型和提升成员不展开。
同组字段各有身份并互为兄弟，内嵌结构体字段按非限定类型名命名。
包及跨文件接收者归属规则见 [命名空间组织](namespaces.md)。

### 引用与类型线索

同文件可证明的词法引用为 exact，同包跨文件和 import 名称匹配的引用为 scoped。
复合字面量的字段键按已加载类型及别名提出 scoped / composite_field；map、数组键保留词法引用。
未知类型、匿名或省略类型的字段键保留缺口，不连接偶然同名的变量或类型。

成员引用与调用共用接收者类型传播：

- 显式类型、局部初始化和变量别名提供字段、方法与接口嵌入方法的候选。
- 成员链沿字段声明类型传播；索引和 range 从容器元素或键获得线索，类型断言保留断言类型。
- 函数、方法及有签名回调沿已加载返回类型传播，多返回值按槽位对应，空白变量不改变槽位。
- 类型线索保留声明文件的导入与词法上下文，跨文件使用时不套用调用方环境。
- 类型别名共享目标；新定义的具体类型仅共享底层字段，不继承底层接收者方法。

类型语法与值表达式分开处理，泛型实例化不当作容器索引；递归类型与循环赋值的传播有界。
未知签名、泛型实参替换、隐式成员提升与未知接收者保留 unresolved_reference。
局部类型保持词法身份，同名预声明标识符不使真实成员使用被过滤。

### 调用与类型关系

解析静态同包函数及模块内导入函数调用；显式接收者、局部初始化和函数/方法别名可提出调用候选。
重赋值保留可能目标，不作语句流判定。接收者未知时仅在同包按方法名提出候选，不匹配普通函数。

interface 嵌入形成 extends，struct 嵌入表达组合。
同包具名类型的直接方法名集合可提出 heuristic / method_name_set 的 implements；
该证据不验证签名、值/指针方法集或类型约束。空接口、嵌入接口和类型项接口不参与推断，提升方法不展开。

不评估 build tags、第三方模块或编译器类型检查；//go:embed 记录 unsupported_resource，不读取嵌入资源。
能力测量与独立参照见 [Go 仓库评测](corpus.md)。

## Python

### 模块与导入

提取 outline 声明、源码导入、显式模块绑定、局部函数调用和前置注释 marker。
import 指向 Module / Package，from-import 与 alias 可连接已加载声明。
函数内 import 不泄漏到其他函数；显式导入的类可以参与构造与方法绑定。

绝对 import 缺少运行时搜索路径证据，即使只有一个本地目标也保持 scoped。
路径候选来自快照根，以及与导入首段同名的当前文件祖先目录，支持源码子目录和嵌套 SDK 布局。
无关目录不会自动成为搜索根。普通包、类型桩及 namespace package 边界见 [命名空间组织](namespaces.md)。

不执行包初始化或动态搜索路径；星号导入、未起 alias 的多段模块属性及动态属性查找保留未解析事实。

### 词法绑定与调用

Scope/Binding 区分签名表达式与函数体：参数类型、默认值、返回注解在外层作用域读取，
参数名及函数体局部赋值仅影响函数体。嵌套签名仍受外层函数的真实绑定约束。
普通、带类型、带默认值、仅关键字和可变参数遵循同一区分；字符串注解保持字符串边界。
方法词法查找跳过类体层级。

Lambda 默认值调用属于创建函数时的表达式，函数体调用仍保留延迟执行缺口。
动态重绑定、执行顺序及 global/nonlocal 重定向未证明时保留局部缺口。
实例首参数、显式类型及局部实例初始化可定位类方法；类构造调用指向 Class。
接收者未知时仅在同模块按方法名提出候选，运行时 MRO 不在查找模型内。

具名基类复用模块绑定，唯一可证明目标为 exact，绝对导入或多个来源仍为 scoped；
动态基类表达式和缺少目标产生局部诊断。

### 单文件语句事实

Facts.Statements 保留语句顺序、作用域和受限表达式词汇，供生产侧解析搜索路径等线索。
参数目标只保留绑定名，默认值独立保留在函数 prelude；注释和普通字符串内部文本不消耗上下文展开预算，
字符串插值仍保留可执行表达式。达到语句、表达式或深度预算时保留 `context_limit` 诊断。
import 事实保存绑定名及语句挂靠。结构是语言中立的，当前只有 Python 捕获，未实现的语言为空。
图消费侧读取绑定后的节点与关系。测量边界见 [Python 仓库评测](python-corpus.md)。

## JavaScript / TypeScript / TSX

### 模块绑定

提取 outline 声明、源码 import、局部函数调用及前置注释 marker，包含 export 包装。
文件以 Module 组织顶层声明；具名、default、namespace import 及显式 re-export 链连接已加载目标。
显式导出优先于星号转导出，星号转导出不传播 default；循环按路径和公开名称检测。

相对路径支持 index 文件；多个匹配来源保留 scoped。
不求值 package metadata、tsconfig alias、CommonJS 导出表达式和动态属性，
namespace 转导出的嵌套属性仍可能未解析。Module 不证明 ESM/CommonJS 运行时加载模式。

### 可见性与类型关系

Scope/Binding 区分函数、块、catch 与循环作用域，var 归最近函数。
引用与调用共用绑定索引，局部遮蔽阻止同名回退；参数和绑定模式的具体覆盖由契约测试约束。
this、显式类型、局部实例初始化及显式导入的类可提供候选方法，构造调用指向 Class。
接收者未知时只在同模块提出同名方法候选。

具名基类和显式接口关系形成 extends / implements；唯一语法绑定为 exact，多个目标保持 scoped。
继承方法查找复用已绑定类型关系，运行时分派仍未证明。
独立编译器参照与未评估范围见 [TypeScript 仓库评测](typescript-corpus.md)。

## 其他 grammar 与扩展

通过 gotreesitter 的 grammars.Register / RegisterExtension 注册 grammar 后，通用适配器
使用 tags 和所有权规则提取具体声明、源码贡献及声明内成员。
grammar 明确提供 body 边界时，声明节点也保留签名。未知声明类别保留诊断；缺少 outline、未支持的类别及关系解析能力分别报告覆盖缺口。
仅提供 grammar 的语言以 unsupported_resolution 表示引用未覆盖，不使用通用 Symbol 节点兜底。

Java、Rust、C/C++、Ruby 有真实源码声明契约测试；grammar 接入示例见
[language_extension_test.go](../language_extension_test.go)。完整组织与绑定规则需要实现内部阶段接口，
见 [语言构建流程](language-pipeline.md)。

## 声明到图的覆盖契约

枚举值说明图能表达什么；语言能力声明说明适配器承诺什么；BuildReport 说明本次输入遇到了什么缺口。
测试 [static_structure_test.go](../tests/semantics/static_structure_test.go) 同时检查类别、名称位置、签名与关系，
避免只验证存在同名节点。

| 输入结构 | 图中表达 | 覆盖边界 |
|---|---|---|
| Go `var A, B int` / 多名 const | 每个名称独立 Variable / Constant、declares、encloses；签名可共享同一源码范围 | 变量不成为其他同组变量的词法父级 |
| Go 函数、方法、类型、字段 | 具体节点、名称范围、声明头签名 | 保留显式语法；不代替 go/types |
| Python 函数、类、方法 | Function / Class / Method、词法结构、声明头签名 | 动态绑定仍按语言限制报告 |
| JS/TS 类字段、TS 接口属性与方法 | Field / Property / Method，词法和成员关系 | 未支持的计算名称、解构和签名形态记录 unsupported_declaration |
| TS/TSX 具名 namespace | Namespace、encloses、contains；内部绑定与显式导出访问 | 合并声明记录 unsupported_namespace_merge；点分声明、ambient module 保留缺口并隔离其子声明 |

普通函数 calls 与对应 references 使用同一词法证明，多个已保留绑定提供 scoped 候选；
未知绑定遮蔽同名函数。未支持的容器不把子声明提升为文件顶层声明。
词法父级必须在同一文件严格包含子声明范围，因此每个已保留声明有唯一、无环的 encloses 路径。

已知缺口检测不等于全语言完备性：尚未建模的语法仍需由能力说明、语料对照和新增契约测试发现。
