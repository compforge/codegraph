# 控制语句绑定声明复测

日期：2026-09-28；CodeGraph 0.7.15，基于主线 `583bcb19fd4ee0cbd7c3c47c4cdb54351dab4535`
加本轮 ECMAScript 适配改动。沿用[首次测量](first-run.md)的 Doctor 快照、输入范围、
Go 1.27.1 / Node v24.7.0 / TypeScript 5.6.3 和编译器参照。

## 改动与边界

循环中的简单命名绑定和 catch 参数在 grammar 中没有 variable_declarator 包装，需独立声明查询。
循环必须带 var / let / const 关键字，避免将 `for (existing of items)` 的赋值目标误认为新声明。
声明范围只包含绑定本身；TypeScript catch 的类型注解包含在声明范围中，控制语句体不包含在内。
声明名同时从引用事实中排除。

具体规则由 ECMAScript adapter 拥有，复用现有 Outliner、组织和发布流程。
覆盖 JS、TS、TSX；解构绑定仍在契约外。本轮补充声明身份，没有增加局部变量引用解析能力。
contains 表达最近的声明归属，不能据此推断循环或 catch 的词法可见范围。

## Doctor 同身份比较

| 指标 | 修复前 | 修复后 |
|---|---:|---:|
| 契约声明 | 9630/10314 | 10305/10314 |
| 整体声明 | 9630/37196 | 10305/37196 |
| 额外声明 | 0 | 0 |
| 额外引用事实 | 12142 | 11467 |
| 内部引用目标命中 | 10652/33188 | 10652/33188 |
| 内部调用目标命中 | 3655/5288 | 3655/5288 |
| 错误 exact 目标 | 0 | 0 |

补齐全部 431 个循环绑定和 244 个 catch 绑定，移除对应 675 条声明名伪引用。
剩余 9 个契约声明缺口为 7 个私有方法、streamWithRetry 和异步生成器 parseSseStream。
语法引用命中仍为 87532/89891，调用表达式仍为 18376/18382，import 位置仍为 3089/3089。
候选集合、静默目标缺口及未知范围均未扩大。

参照仍有 1531 条编译器诊断，未安装第三方依赖带来的 unknown 保持未评估。
本轮没有修改参照或缩减分母，零错误 exact 仍仅适用于已评分目标。

## 验证

`make fix lint test build test-corpus` 通过，六仓显式比较首轮同身份报告通过。
三个 Go 仓库和两个 Python 仓库的测量保持不变。新增契约先确认修复前失败，再验证修复后通过，
覆盖绑定范围、所属声明、重复 catch 名称、typed catch、赋值循环、解构及遮蔽负例。
加强原始引用事实断言后，相关 race 测试和最终 Doctor 基线比较再次通过。

```sh
make test-typescript-corpus \
  CORPUS_BASELINE_DIR=/path/to/first-run \
  CORPUS_REPORT_DIR=/path/to/control-bindings
```

产物仍包括 summary.md、report.json、oracle.json 和 graph.json；原始 JSON 不纳入 Git。

剩余 9 个声明缺口的修复与复测见 [生成器与私有方法声明复测](callable-declarations.md)。
