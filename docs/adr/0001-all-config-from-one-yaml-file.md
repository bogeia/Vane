# 主配置显式引用独立信源文件

Vane 通过 `--conf` 指定主 YAML 文件，保存数据库连接串、模型 API Key、采集和模型调用开关等运行参数；主文件用必填的 `sources_file` 显式引用独立信源 YAML 文件。信源清单较长且独立维护，因此将它从主文件拆出。程序不读环境变量，也不回填默认值；不存在多文件覆盖优先级。启动时两份文件都必须存在且通过字段、类型和取值校验，否则拒绝启动。

这项决策更新原先“全部配置放在单个 YAML”的约定，保留单一显式配置入口。相对信源路径以主文件目录解析，绝对路径直接使用；不按当前工作目录寻找、不自动猜测文件名、不递归 include。信源文件在顶层 `sources:` 下按 `official` 和 `media` 分组，两组列表均须显式出现，空组写 `[]`；信源内禁止再写 `tier`，解码后由分组赋值，ID 跨组唯一。旧的平铺列表和未知分组明确报错，校验错误沿用分组内路径（如 `sources.media[2].block`），不输出配置值；主文件里的旧内联 `sources` 明确报错，避免迁移时静默忽略其中一份。

`config.example.yaml` 与 `sources.example.yaml` 作为公开示例追踪；真实 `config.yaml` 与 `sources.yaml` 同时从 Git 和 Docker 构建上下文忽略。部署时挂载包含这两份真实文件的配置目录，测试只复制公开示例到临时目录，绝不隐式读取真实信源清单。

## Considered Options

- 环境变量覆盖密钥（12-factor 常见做法）：没有采用，因为会出现两个配置来源。代价是密钥也写在文件里：部署时挂载包含 `/data/conf/config.yaml` 与所引用信源文件的目录，仓库里只放不含密钥的示例。
- 用 knadh/koanf 替换 spf13/viper：没有采用。echo-layout 已经用 viper 做严格解码，信源列表中某一项缺字段也能报错，换库没有收益。

## 配置值校验的选择

配置值校验使用 `github.com/go-playground/validator/v10`，代替逐字段手写的通用判断。规则放在 `internal/config` 的配置结构体上；这项选择只覆盖启动配置，不要求业务模型或 API 入参统一使用同一个库。

对比的另一方案是 `github.com/go-ozzo/ozzo-validation/v4`。两者都能完成当前校验，主要区别在规则的表达方式，而非是否支持复杂校验。

| 方面 | ozzo-validation | go-playground/validator |
| --- | --- | --- |
| 规则位置 | 在 `ValidateStruct`、`Field` 等 Go 调用中组合规则，例如 `validation.Min(1)` | 在字段旁声明标签，例如 `validate:"gt=0"` |
| 条件和跨字段规则 | 用 `When`、`Else` 和普通 Go 表达式表达条件，字段引用便于编译器和 IDE 检查 | 提供条件、跨字段标签，也支持字段级和结构体级自定义校验；标签中的字段名是字符串 |
| 嵌套结构和集合 | 使用 `Validatable`、`Each` 等机制组合校验 | 递归校验嵌套结构体，使用 `dive` 校验集合元素 |
| 错误处理 | 可按字段组织 `validation.Errors`，在规则上定制消息 | 返回 `ValidationErrors`，可取得字段路径、失败标签等信息，由调用方组织消息 |

上述能力见 [ozzo 官方说明](https://github.com/go-ozzo/ozzo-validation#readme)和 [validator 官方说明](https://github.com/go-playground/validator#readme)。

选择 validator 的具体原因：

- 当前规则以静态字段约束为主：正数、非负数和列表元素校验。`gt=0`、`gte=0`、`unique=Name,dive` 可以直接放在字段旁，减少单独维护字段校验清单的代码。信源 ID 由配置读取边界跨组检查唯一性，逐个信源的值校验也在分组路径下执行。
- 配置结构体由本项目控制，增加标签不会侵入业务层。阅读字段定义即可看到大部分约束。
- 少量特殊规则通过注册自定义校验保留原有语义，无需为整个配置层重写一套规则组合代码。

选择的代价同样明确：标签不是编译期检查的 Go 表达式，拼错规则或字段引用可能在运行时才暴露；无效标签甚至可能触发 panic，见 [validator 的说明](https://pkg.go.dev/github.com/go-playground/validator/v10#hdr-Panics)。当前 LLM 条件校验通过结构体级自定义规则读取 `Enabled` 并校验合并后的每步配置，不如 ozzo 的 `When` 表达直观。配置测试必须覆盖这些条件分支。如果后续规则主要变成跨字段、分场景的组合，应重新比较 ozzo，而不是继续堆叠自定义标签。本次没有做性能基准，也不以性能、流行度或未经核验的维护状态作为选择理由。

## 解码与校验的边界

validator 校验的是解码后的值，不能代替 YAML 解码阶段对字段是否出现的检查。继续由 Viper/mapstructure 的严格解码拒绝缺字段、未知字段（包括主配置和信源配置的各层嵌套字段）及不符合约定的类型，并由 `rejectNull` 拒绝显式 `null`。例如开关的 `false` 是合法值，不能用 `required` 标签代替“该字段必须写在 YAML 中”。

自定义规则仅保留项目需要的语义：去除空白后非空、绝对 HTTP(S) URL、带主机和数据库名的 PostgreSQL URL、监听端口范围、分钟转 `time.Duration` 的溢出边界，以及启用 LLM 后才校验其地址、Key 和模型。禁用 LLM 时不校验未使用的这三个值。不能直接用通用 `required` 或 URL 规则替换后改变原有接受范围。

配置值校验失败时，只输出字段路径和规则名，不输出字段值或整个配置对象，避免泄露数据库凭据和模型 API Key。错误文本由项目控制；换用任何校验库都应保留这条约束。

## 每步模型的显式继承

`llm.steps.prefilter`、`llm.steps.score`、`llm.steps.write`、`llm.steps.group` 都必须显式写出 `base_url`、`api_key`、`model`、`reasoning_effort`。前三项写空字符串表示沿用 `llm` 下对应的值；这是配置里明确表达的继承，不是程序补默认值。`reasoning_effort` 没有上层值，空字符串表示请求不发送该字段，非空值作为请求体顶层字段原样发送，不根据模型名判断。

启用模型时，每一步合并后的地址必须是 HTTP(S) URL，Key 和模型名必须非空；只要各步覆盖后有效，公共字段可以为空。禁用模型时仍要求字段出现，但不校验这些未使用的连接值。超时和重试继续共用 `llm.request_timeout` 与 `llm.retry`。

## 外部请求与任务重试

外部 HTTP 请求使用 `github.com/cenkalti/backoff/v4`（锁定 `v4.3.0`）执行有次数上限的指数退避，加入随机抖动并遵守调用方的 context。LLM 与 RSS/原文抓取分别配置请求超时、总尝试次数和退避间隔；后台资料处理另行配置每轮超时、总失败次数和调度间隔。配置入口为 `llm.request_timeout`、`llm.retry`、`fetch.request_timeout`、`fetch.retry`、`worker.attempt_timeout` 和 `worker.retry`；各 `retry` 使用 `max_attempts`、`initial_interval`、`max_interval`。所有重试参数仍来自主 YAML 文件，示例配置写明推荐值，程序不隐式补默认值。`max_attempts` 包含首次执行，设为 1 表示不重试；不允许以 0 表示无限重试。

选择依据与备选方案：

| 方案 | 判断 |
| --- | --- |
| `github.com/cenkalti/backoff/v4` | 采用。已有指数退避、随机抖动、context 取消、尝试次数限制及永久错误机制，能删除当前手写的等待循环，并让请求执行与错误分类分别保持清楚的职责。 |
| `avast/retry-go` | 支持错误筛选、回调和多种延迟策略，也能满足需求；本项目没有按错误分别配置次数或收集每次错误等额外需求，不因选项更多而采用。 |
| `go-resty/resty` v3（模块路径 `resty.dev/v3`） | 是完整 HTTP/REST 客户端。当前已有标准库客户端及 DNS 地址校验、重定向限制和响应体上限，本次只需要重试执行；引入 Resty 会扩大迁移和验证范围。不是 Resty 无法保留这些限制，而是本次没有替换整个 HTTP 客户端的收益。 |
| 保留手写循环 | 不采用。请求次数、取消、退避和等待预算容易在 LLM 与抓取代码间重复，后续修改需要同时维护。 |

能力依据见 [backoff 文档](https://pkg.go.dev/github.com/cenkalti/backoff/v4)、[retry-go 官方说明](https://github.com/avast/retry-go)和 [Resty v3 官方说明](https://github.com/go-resty/resty/tree/v3)。不以星数、未经验证的性能比较或维护状态作为选库理由。

库只负责重试执行，不决定业务错误是否值得重试。临时网络故障以及 HTTP 429、500、502、503、504 允许有限重试；认证失败、普通客户端错误、无效地址和安全校验失败停止重试。永久错误标记必须传到后台处理，避免 HTTP 层已停止、任务层却继续请求。无效模型输出可以由任务层有限重试；调用方取消应及时退出，不能归为永久业务错误。

429/503 的有效 `Retry-After` 支持秒数和 HTTP 日期。不能为了满足本地最大退避间隔而提前重试；如果等待超出当前请求预算，则结束本轮。单次 HTTP 超时控制单次请求，调用方 context 控制整个操作，重试等待不能绕过总预算。重发 LLM 请求可能重复计费，库无法保证外部调用恰好执行一次。

分钟级资料重试继续将次数与下次执行时间持久化到数据库，由调度器重新领取；不能改成进程内等待，否则重启会丢失调度状态。两层分别有界，避免请求次数无控制地叠加。并发步骤返回多个等待时间时，任务使用其中最晚的时间。RSS 最终失败后保留错误，下次采集时间取采集周期与服务器要求的最早重试时间中较晚者；原文抓取失败仍允许使用已有 RSS 正文。配置测试覆盖非法次数、非法时长与间隔关系；请求和任务测试覆盖分类、取消、次数、限流等待及永久错误终止。

## 订阅解析与生成的选择

2026-10-22：保留 `github.com/mmcdole/gofeed v1.5.0` 解析外部信源，新增 `github.com/gorilla/feeds v1.2.0` 生成本站订阅。两者承担不同方向的工作，不互相替代。

| 方面 | mmcdole/gofeed | gorilla/feeds |
| --- | --- | --- |
| 主要职责 | 将外部订阅文档解析为 Go 数据 | 将 Go 数据生成为订阅文档 |
| 支持格式 | 解析 RSS、Atom、JSON Feed，并提供统一的 Feed/Item 模型 | 生成 RSS 2.0、Atom、JSON Feed 1.1，提供通用及格式专用模型 |
| Vane 用途 | 采集器读取各信源的资料 | 服务层生成 `/feed.xml`、`/feed/all.xml`、`/feed/daily.xml` |
| 项目继续负责 | 安全抓取、条件请求、超时、响应体限制和资料规范化 | 内容选择、事件折叠、综合稿排除、GUID 和发布时间等业务语义 |

能力依据见 [gofeed 官方说明](https://github.com/mmcdole/gofeed#readme)、[feeds 官方说明](https://github.com/gorilla/feeds#readme)和 [feeds API 文档](https://pkg.go.dev/github.com/gorilla/feeds)。本次通过 `go list -m github.com/gorilla/feeds@latest` 核实最新稳定版为 `v1.2.0`，并锁定该版本。

选择 feeds 的直接收益是复用现成的 RSS 类型、根元素和 XML 序列化，删除四个自定义 RSS 结构及手动拼接 XML 声明的代码，减少协议映射的维护工作。库还提供作者、图片、附件、分类和全文内容等字段，后续确有需求时可以使用；本次没有因此新增 Atom、JSON Feed 或额外 RSS 字段。

采用格式专用的 `feeds.RssFeed`、`feeds.RssItem`、`feeds.RssGuid` 与 `feeds.ToXML`，而不是直接使用通用 `Feed.ToRss`，因为通用 Feed 没有 Language 字段，直接转换会丢失现有的 `zh-CN`。保留现有 GUID 值、资料和精选的 `isPermaLink="false"`、日报的 `isPermaLink="true"` 以及 RFC1123Z 日期格式，避免改变阅读器识别订阅项的依据。

备选方案是继续使用自定义结构和标准库 `encoding/xml`。原实现已经正确进行 XML 转义，本次替换不代表原实现存在安全缺陷，也不以性能或流行度作为选库理由。选择库的代价是新增一个固定版本依赖，以及 XML 字节布局变化：输出带缩进和 `content` 命名空间，XML 声明后的换行也不同。测试应比较解析后的 RSS 内容与属性，不要求字节完全相同；仍需覆盖三种订阅的语言、GUID、日期、中文特殊字符、空订阅及业务筛选规则。生成库不能替代这些契约测试，也不保证所有阅读器的兼容性。

## 按类型出现的信源子段

信源的 `web_list` 子段按 `kind` 出现：仅 `kind: web_list` 必填，其他类型禁止出现；这不属于“补默认值”。解码允许该指针子段未出现，仍显式校验所属类型以及四个必填 CSS 选择器，其他必填字段保持严格要求。
