# 生成器与私有方法声明复测

日期：2026-09-28；CodeGraph 0.7.16，基于主线 `2abc3995b0b94f672426f6057319b1e8644b8720`
加本轮 ECMAScript 适配改动。沿用 [控制语句绑定复测](control-bindings.md) 的固定 Doctor 快照、
578 个源码文件、工具链和独立参照，不修改评分规则或分母。

## 根因与契约

生成器函数使用 generator_function_declaration，私有方法名使用 private_property_identifier，
原声明查询没有覆盖这两种形态。TypeScript 的 `private async *streamWithRetry` 还有另一种情况：
当前上游语法树同时暴露 async 和实际方法名为 property_identifier，且 name 字段指向 async。
宽泛查询因此产生名称冲突，直接采用 name 字段也无法得到正确答案。

ECMAScript adapter 拥有完整的声明查询，复用现有 Outliner 与后续构建流程。方法名根据位于
参数或类型参数之前的直接语法位置确定，允许中间出现注释。生成器仍为 Function，私有方法仍为
Method；不增加公共节点类别。生成器声明名和被误识别为标识符的修饰符不作为引用事实。

声明的完整范围、包含关系和导出声明的 marker 附着保持原契约。
计算属性表达式仍作为读取处理，本轮没有为动态方法名推断确定目标。

## Doctor 同身份比较

| 指标 | 修复前 | 修复后 |
|---|---:|---:|
| 契约声明 | 10305/10314 | 10314/10314 |
| 整体声明 | 10305/37196 | 10314/37196 |
| 额外声明 | 0 | 0 |
| 额外引用事实 | 11467 | 11465 |
| 内部引用目标命中 | 10652/33188 | 10653/33188 |
| 内部调用目标命中 | 3655/5288 | 3656/5288 |
| 错误 exact 目标 | 0 | 0 |
| 静默引用目标缺口 | 1447 | 1426 |

补齐 7 个私有方法、streamWithRetry 和异步生成器 parseSseStream。
契约声明满额命中仅说明本次 profile 中已声明支持的类别和范围通过比较；整体声明分母仍包含
参数等契约外绑定。编译器仍有 1531 条诊断，第三方类型未知与动态分派仍未评估。

图中未评估调用边从 232 增至 245；这些边不能计入已验证调用目标。
语法引用、调用表达式、import 位置的命中数保持不变，其他候选数与最大候选集合未增加。

## 验证

新增 JS / TS / TSX 契约覆盖普通和异步生成器、私有方法、可见性修饰符、泛型、getter / setter、
合法的 async 方法名、方法名后的注释、声明范围、归属和 marker。

原诊断测试依赖真实 TypeScript 的名称冲突；为使解析能力改善后仍能检验诊断契约，改用独立
注册 grammar 的显式歧义查询。继续检查局部缺口、其他文档的模块事实、未受影响声明和快照隔离。

`make fix lint test build test-corpus` 通过，六仓显式基线比较通过。既有三个 Go 仓库和两个
Python 仓库的覆盖指标保持不变。复现方式：

```sh
make test-typescript-corpus \
  CORPUS_BASELINE_DIR=/path/to/control-bindings \
  CORPUS_REPORT_DIR=/path/to/callable-declarations
```

原始产物通过 summary.md 链接到 report.json、oracle.json 和 graph.json，不纳入 Git。
