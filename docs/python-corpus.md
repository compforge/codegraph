# Python 真实仓库评测

Python 评测把源码事实覆盖与目标绑定正确性分开测量。CPython AST 提供独立语法参照，人工审定的
绑定清单提供有限、可解释的关系参照。结果为 measured，不能解释为整个 Python 仓库的完整调用图。

## 材料与参照

`tests/corpus/python/repos.json` 固定 python-stdx 和 agentue 的 commit、源码压缩包 SHA-256 及源码根目录。
评测器复用公共语料下载和校验流程；输入清单记录仓库全部 `.py` 文件的摘要，以及是否属于指定源码根。
测试、示例和其他根目录之外的源码保留为排除项。各仓库独立构图，外部依赖不加载为本仓库声明。

CPython 在隔离模式下运行测试层的参照脚本，只读取和解析源码，不 import 目标包、运行初始化代码或
访问 Redis、模型和数据库。它不安装目标仓库依赖。解释器版本、评测器摘要、源码摘要和完整输入清单
进入报告；解析失败使采集失败，不能以空参照继续评分。

独立参照由两部分组成：

- **语法事实**：AST 枚举类、同步/异步函数、参数与 Name Store 位置、Name/Attribute Load 引用、
  Call 表达式及 import 模块。tokenize 提供名称的 UTF-8 字节位置，并统一声明末尾同行注释的范围。
  类、函数和方法属于声明契约；参数和赋值位置另列，重复赋值也保留位置，不能将其数量解释为唯一变量数。
- **审定绑定**：每个仓库的 JSON 清单以路径、行号、表达式原文和名称定位使用处，以路径和限定名定位
  声明；同时记录判断理由。使用处与目标必须唯一匹配，否则报告错误。审定内容覆盖局部方法、导入的类和
  函数、注解及实例调用，另显式记录调用方提供的回调和外部框架方法。

没有审定的目标全部保持 unknown。清单不会根据 CodeGraph 输出生成或自动更新；源码变化必须重新审定。
这里比较的是源码可支持的声明绑定，不承诺对象在任意 monkey patch、子类替换或动态导入后的运行时目标。

## 评分与边界

全量语法事实按位置比较；import 允许同一模块的 token 与包含它的语句采用不同范围。
引用位置参照针对读取，CodeGraph 的额外赋值属性、关键字参数名等记录保留为 unexpected，需逐项核对，
不能直接当成错误目标。字符串注解与动态字符串内容不自动展开为名字引用。

引用和调用的内部目标分母只包括审定清单，报告同时展示未审定使用位置和未评分图边。
错误 exact 目标令门禁失败；非 exact 的其他目标、静默缺口和局部诊断分别保留。
生成的 dataclass/Pydantic 成员、装饰器变换、动态回调实现及运行时属性求值不冒充确定目标。
Package/Module 身份、declares/contains、审定绑定的来源与位置、明确源码布局下的模块 imports 单独评分；
Evidence 推导语义、类型关系与符号转导入仍未评估。

`oracle.json` 保留独立事实及已审定目标；`graph.json` 保留 CodeGraph 的原始图和事实；
`report.json` 给出分项指标与位置差异；`summary.md` 明确显示有限绑定分母和未评估范围。

## 执行与回归

需要 Go、CPython 3.11+；格式检查还需要 Ruff。可用 `PYTHON=/path/to/python3` 选择解释器。

```sh
make fix lint test build
make test-python-corpus
make test-corpus CORPUS=python-stdx
make test-python-corpus CORPUS_BASELINE_DIR=/path/to/reviewed-reports
```

普通测试验证语法参照、UTF-8 位置、注释范围、源码筛选、审定清单失效，以及错误边注入、删除事实后分母
不变和动态调用未评估；真实语料仍显式选择执行。

基线比较复用[公共机制](corpus.md#回归与语料维护)。Python 脚本、契约测试、固定仓库清单和审定绑定
共同进入评测器摘要。改动参照或扩大绑定清单后需要新建基线，不能将分母变化解释为解析质量变化。

固定快照的首次测量与已核对缺口见[首轮记录](../tests/corpus/python/first-run.md)。

参数注解与遮蔽修复的同基线结果见[注解绑定复测](../tests/corpus/python/annotation-bindings.md)。

组织身份、源码贡献、直接成员以及可由当前 profile 确定的源码模块导入，现由公共 corpus 评分器
单独评估；同端点的不同关系和不同发生位置保留独立分母。身份与显式重评规则见[真实仓库评测](corpus.md)。
