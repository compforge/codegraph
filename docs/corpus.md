# 真实仓库评测

评测回答两个问题：给定源码范围中哪些事实被提取出来，哪些引用和调用被连接到了正确目标。
仓库材料、参照生成、评分与回归门禁由 `tests/corpus` 独立测试模块负责，通过 CodeGraph 公共 API
消费事实。它不改变运行时图模型，也不向 BuildReport 增加全局完整性结论。

## Go 语料与参照

Python 与 TypeScript 的独立参照分别见 [Python 仓库评测](python-corpus.md) 和
[TypeScript 仓库评测](typescript-corpus.md)，与 Go 共用快照、图观察、评分和基线机制。

`tests/corpus/repos.json` 固定 go-stdx、agentgo、repocli 的完整 commit 和源码压缩包 SHA-256。
下载内容必须通过校验；缓存只保存压缩包，每次运行解压到独立临时目录。源码保留原始许可证。
依赖解析使用原仓库的 go.mod/go.sum 与只读 module 模式，不使用本机 go.work。

当前 profile 选择根 Go module 在 **linux/amd64、CGO_ENABLED=0** 下的非测试包，包括可编译的
examples。所有 `.go` 文件都进入输入清单，记录摘要及纳入/排除状态；测试文件、平台不匹配文件、
testdata、嵌套 module 等不混入已评估分母。此 profile 不代表所有平台和构建标签。

参照使用官方 `go/packages` 加载源码和类型信息，再独立收集 `go/ast` 声明、调用表达式、import，
以及 `go/types` 的定义和使用绑定。CodeGraph 的 Go 适配也使用部分标准库 AST，二者不是完全不同
的解析器；参照侧不调用 CodeGraph 内部提取和绑定逻辑，并使用跨包类型信息核对目标。

参照加载或类型检查失败会保留 error 报告并使命令失败，不把残缺参照当成完整空答案。
参照目标使用源码路径与标识符字节位置，比较时不依赖 CodeGraph 生成的 ID。

## Go 评分口径

| 测量 | 分母和比较内容 |
|---|---|
| declarations/all | 非空白、非 import 别名和标签的源码定义；包含参数、短变量声明等图契约外绑定 |
| declarations/contract/* | 当前声明契约内的函数、类型、显式单名称 var/const、命名类型成员；比较名称、类别与完整范围 |
| reference_facts | 类型检查器识别的标识符使用位置；内建名称及包限定符单列排除 |
| call_expressions | AST 调用表达式位置；分别报告普通调用、动态调用、内建函数与类型转换 |
| import_facts | import 路径与源码发生位置 |
| references/calls target hits | 可静态确定且目标为已纳入图声明类别的内部使用位置，正确目标出现在图边中即命中 |

`found / expected` 表达召回；`unexpected` 保留无法对应参照的输出。声明整体口径与契约口径同时
展示，避免缩小支持范围掩盖真实缺口。关系的目标命中率也不代表所有这些绑定形式都已被库承诺支持。

关系按 exact 正确/错误、candidate 命中/其他目标、未评估分别计数。候选集合最大大小同时记录，
防止通过扩大候选集合掩盖质量退化。candidate 的其他目标表示不等于本次编译器绑定，仍需结合
候选规则审定，不能直接解释成错误的 exact 关系。

函数变量调用、接口动态分派等没有完整静态运行时答案的调用保持未评估。外部依赖目标不纳入内部
目标召回分母；但如果误连到本仓库目标，仍能发现与编译器答案的差异。

未命中的内部目标按同位置的关系诊断、文档级诊断和静默遗漏分别统计。诊断数下降不是通过条件。
Package/Module 使用独立来源身份、名称、限定名和完整贡献文件集合核对；declares、contains、
已支持的源码模块 imports，以及内部引用/调用的来源和发生位置由 `relation_occurrences/*` 单独评分。
Go 接收者归属来自 go/types；同组字段是兄弟成员。Python 包树来自源文件与 initializer，
只在明确源码包布局下评估 import 目标，不把纳入语料的目录误当搜索根。TS 模块 import 目标来自编译器。
组织评测不读取生产 Organizer，也不复刻 CodeGraph ID。类型关系、符号转导入、Evidence 的推导语义、
marker 与运行时完整调用图仍未评估；Evidence 合并、置信派生和复制隔离由库契约测试覆盖。

查询验证将节点和关系的 Cypher 投影与公共访问器逐项比较。这证明已发布证据可以读出，不把两个
CodeGraph 接口相互一致当成语义正确的 ground truth。

## 执行与证据

```sh
make setup-typescript-corpus             # 首次安装锁定的 TypeScript 编译器参照
make lint test build                     # 库测试和评测器本地契约；真实仓库评测显示 skipped
make test-corpus                         # 全部 Go / Python / TypeScript 固定仓库，允许下载源码及 Go 依赖
make test-python-corpus                  # python-stdx、agentue，仅静态分析源码
make test-typescript-corpus              # Doctor，仅静态分析源码
make test-corpus CORPUS=go-stdx,agentgo    # 精确选择语料
```

只加载源码、类型检查和构图，不执行目标仓库的 init、业务测试或服务。每仓最多运行五分钟；构图与
查询有显式预算。无需模型凭据、数据库或 Kubernetes 环境。普通 Go 测试仍需要其正常的模块依赖缓存。评测器的 Python 契约测试需要 CPython 3.11+；
`make fix` / `make lint` 还需要 PATH 中的 Ruff。`PYTHON` 可指定解释器路径。TypeScript 参照契约需要
Node.js 20+、npm 和锁定的 TypeScript 5.6.3；安装入口只安装评测器依赖，不安装目标应用依赖。

默认产物在 gitignored `.corpus-results/<repo>/`，可通过 `CORPUS_REPORT_DIR` 指定：

- `summary.md`：本轮执行状态、主要指标及证据入口。
- `report.json`：输入范围、版本/构建信息、评测器摘要、分项计数及全部差异。
- `oracle.json`：独立参照，保留每个定义、使用位置、目标和未评估原因。
- `graph.json`：实际节点、关系、单文件事实与局部诊断。

报告的执行状态 `measured` 表示完成测量，评测 verdict `measured` 表示尚未作全面质量认证。
错误 exact 目标使 verdict 为 `failed` 并使测试退出非零。没有错误 exact 也不能推出“完备”，
必须同时阅读召回与未评估范围。失败报告及原始证据仍保留。

## 回归与语料维护

经审定的历史报告目录可显式作为基线，测试不会自动更新或接受基线：

```sh
make test-corpus CORPUS=go-stdx \
  CORPUS_BASELINE_DIR=/path/to/reviewed-report \
  CORPUS_REPORT_DIR=/path/to/new-report
```

报告 schema 2 分离三种身份：

- InputIdentity：仓库固定快照、profile 与材料清单及内容摘要。
- EvaluatorIdentity：评分/oracle 源码、工具链、实际 evaluator 依赖闭包。Go oracle 记录
  go/packages 及传递依赖的版本、校验和和所用源码摘要（本地 replace 的内容变化也可识别）；
  TypeScript 编译器依赖由锁文件与实际版本约束。
- SubjectIdentity：被测 CodeGraph 的 revision、改动摘要、源码摘要及构建依赖信息。

比较要求 InputIdentity、EvaluatorIdentity 和 schema 一致，允许 SubjectIdentity 改变。
单独升级 parser 不会再误判为 oracle 变化；如果升级同时改变 oracle 的共享依赖，则仍拒绝比较。
分母变化会拒绝比较；
召回下降、额外输出增加、错误 exact 增加、候选膨胀或静默目标缺口增加会触发回归门禁。
错误 exact 的零容忍条件独立存在，不能通过接受失败基线将其绕过。

参照或评分规则变更后先审定新的测量；不能把口径变化解释成库质量变化。升级固定仓库也应独立进行。
真实语料发现的差异先核对参照和源码，再缩减为人工可审定的契约用例。没有足够证据的差异保持待审。

参照器自身的契约覆盖错误目标注入、删除输出后分母不变、动态分派未评估、字段键与类型同名、链式
调用与导入别名、类型加载失败、输入平台过滤、基线身份漂移及压缩包路径边界。

组织节点使用 `organizations/*` 分母，不混入源码声明分母。删除某个 occurrence、改错组织身份、
重复一条关系的破坏性用例验证评分器不会靠端点去重掩盖错误；Python 包嵌套与 TS 编译器模块导入
有独立 oracle 契约。

### 显式重评旧产物

修改 oracle 或 schema 后，可以显式用当前 oracle 重新评分历史 `graph.json`，输出到新目录：

```sh
make test-corpus CORPUS_REEVALUATE_DIR=/path/to/original-report \
  CORPUS_REPORT_DIR=/path/to/reevaluated-report
make test-corpus CORPUS_BASELINE_DIR=/path/to/reevaluated-report \
  CORPUS_REPORT_DIR=/path/to/new-report
```

重评仍校验输入身份，并重新生成独立 oracle；不重新运行旧版 CodeGraph。
新报告沿用原 SubjectIdentity，记录原 graph/report 的 SHA-256。schema 1 的单 Basis 只在评测器
读取旧产物时转换为一条 Evidence；库 API 不保留兼容字段。原报告与原图保持不变。
新评测器引入了原图不含的信息时，相应指标只能描述可重评范围，不能补造历史证据。

本轮新契约的六仓比较见 [0.8 评测记录](../tests/corpus/semantic-contracts.md)。
