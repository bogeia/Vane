# 模型调用与摘要增强

`EnhanceSummary` 使用 biz 判定的 `SummaryType`，沿用 `llm.steps.write` 的连接、模型和 reasoning_effort，以及既有请求超时和有界重试。原文仍按既有规则最多取前 6000 字，不进行首中尾采样。模型只返回摘要；无法确认适用类型或原文信息不足时返回空摘要，由业务流程保留基础摘要。返回值复用 `NormalizeWriting` 的 400 字、拒答和退化输出约束，不修改传入的基础写作。

每次 HTTP 尝试通过既有 `slog` 默认日志记录 `model HTTP attempt`，字段包括 `step`、`model`、`result`、`http_status`、`duration`、`input_tokens`、`output_tokens`。摘要增强的 step 为 `summary_enhancement`。失败后的每次 HTTP 重试分别记一条，可能重复计费，不保证外部服务恰好执行一次。

`result` 区分 `request_error`、`http_error`、`read_error`、`invalid_response` 和 `http_success`。`http_success` 仅表示 HTTP 200、基本响应封装和模型 JSON 可解析，不代表业务字段或摘要质量验收成功；业务回退/成功由用例日志记录。`http_status=0` 表示未收到响应。token 使用兼容服务返回的 `usage.prompt_tokens` 和 `usage.completion_tokens`；缺失、不兼容或负值记为字符串 `unknown`，实际返回的零仍为数值 0。用量格式不兼容不会使原本有效的模型响应失败。

日志不包含 endpoint、API Key、输入材料、请求体或响应体。没有可靠单价时不估算实际费用。自动化检查不能替代三类各 10 条的人工摘要质量验收。
