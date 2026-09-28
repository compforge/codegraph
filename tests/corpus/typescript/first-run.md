# Doctor 固定语料首次测量

日期：2026-09-28；CodeGraph 版本 0.7.14，基于主线 `262765f33668d1f3b8ad17547546c1eba2386451`
加本轮评测层改动。运行时解析逻辑未修改。工具链为 Go 1.27.1、Node v24.7.0、TypeScript 5.6.3。

源码及版本摘要：`1ce50d16c47ce312fecc82d0b09876b1325c504f7831ab6ef6082398a0a68e1e`。
评测器摘要：`80f3739ab93309f5b374437d9a7af7b03bc5535c73598f4742f2c322c8ef02b6`。
Doctor 固定提交为 `2c6dd6edaf045ce126f3ad176d5036e68407561c`；校验值见 [repos.json](repos.json)。

## 输入与事实覆盖

纳入 578 个源码文件；198 个文件位于 sourceRoot 外，3 个声明文件仅用于编译器支持，
9 个配置文件参与输入身份。profile 覆盖 cli、plugin SDK、agent SDK 和 example plugin 的 src。

| 测量 | 命中 / 期望 | 额外输出 |
|---|---:|---:|
| 契约声明 | 9630 / 10314 | 见整体声明 |
| 整体声明 | 9630 / 37196 | 0 |
| 引用语法事实 | 87532 / 89891 | 12142 |
| 调用表达式 | 18376 / 18382 | 21 |
| import 模块位置 | 3089 / 3089 | 13 |

参照初版遗漏了构造函数声明，曾造成 27 条额外声明误报；核对源码后修正参照并补充测试，
表中为修正后的完整复跑结果。额外引用、调用和 import 事实仍需审定，不能全部归为解析器错误。

## 目标绑定

| 关系 | 内部目标命中 | 正确 / 错误 exact | 命中 / 其他 candidate | 图边未评估 | 静默目标缺口 |
|---|---:|---:|---:|---:|---:|
| references | 10652 / 33188 | 10652 / 0 | 0 / 0 | 50 | 1447 |
| calls | 3655 / 5288 | 3561 / 0 | 94 / 2 | 232 | 3 |

最大候选集合为 2；两个其他候选来自 method_name 规则，不作为错误 exact。
另有 21,089 个引用目标缺口、1,630 个调用目标缺口有局部诊断。

编译器记录 1,531 条诊断，包括 646 条缺失模块诊断；跨项目诊断按各自 Program 保留。
参照中引用 unknown 为 15,618，外部引用为 6,843，契约外目标为 34,286；调用 unknown 为 6,570，
外部调用为 4,890，运行时分派为 1,634。缺失依赖使这些目标无法完整审定，不能把已评分范围的
零错误 exact 推广到全部代码。578 个 Module 组织节点单列未评估。

## 已核对的下一步

1. **循环与 catch 绑定声明**：684 个契约声明缺口中，675 个为 Variable；逐项按源码位置核对，
   包括 431 个 for / for-await 绑定和 244 个 catch 绑定。这是首个适合缩减契约并修复的集中缺口。
2. **引用绑定**：缺口样本包含本地变量和跨 workspace 类型引用，如 `agent-commands.ts` 的
   `entries`、`root`、`bin` 与 `PluginDefinition`。尚未将全部缺口归因到同一个实现问题。
3. **特殊声明与泛型调用**：剩余声明缺口为 7 个私有方法、`server-agent.ts` 的 `streamWithRetry`
   和 `protocol/sse.ts` 的异步生成器 `parseSseStream`。3 个静默调用缺口分别为
   `defineCommand<CollectInput, CollectOutput>`、`promptListedChoice<string | null>` 和
   `defineExecutionRecord<KubernetesCommandConfig["kubernetes"]>`。

源码路径均相对于固定 Doctor 快照。后续应先将这些形态缩减为可审定测试，再修改语言实现。

## 验证与复现

`make fix lint test build test-corpus` 通过，覆盖 6 个 Node 参照测试、5 个 Python 参照测试、
Go race 测试、错误目标注入和全部六仓测量。本轮结果为 measured，表示完成测量。

| 既有仓库 | 契约声明 | 内部引用目标 | 内部调用目标 |
|---|---:|---:|---:|
| go-stdx | 67/67 | 125/125 | 16/16 |
| agentgo | 2578/2578 | 9656/9689 | 1152/1262 |
| repocli | 635/635 | 2885/2959 | 340/363 |
| python-stdx | 422/422 | 7/11 | 4/4 |
| agentue | 104/104 | 5/8 | 3/4 |

Doctor 再次运行并显式比较本轮首份报告，通过相同快照、工具链、评测器和输入清单的回归门禁，
各项指标一致。评测器身份已扩展，不能将此前不同摘要的报告直接作为同身份回归基线。
复现命令及评分边界见 [TypeScript 评测说明](../../../docs/typescript-corpus.md)。
原始 JSON 不纳入 Git；执行后通过产物 summary.md 访问完整参照、实际图与差异位置。
