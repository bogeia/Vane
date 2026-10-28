# 架构整改记录

本清单最初记录 `9bd9f5c` 的架构偏差；本轮实现基于 `34f8806`，核心与 HTTP 整改冻结于 `a8e7760`。下列七组已完成实现及调用链审查，测试证据见末尾。持续变更规则以 [AGENTS.md](../../AGENTS.md) 为准。

参考 `kratos-layout` 的手写 service → UseCase → Repository → data 链路，以及 `internal/server/http.go` 的集中构建方式；Vane 保留 Echo/Huma/GORM/slog，不引入参考框架或通用事务封装。开发阶段直接使用目标接口，不保留废弃签名、兼容转发或旧版本 codec 分支。

## 1. 用例入口

HTTP 的 `NewItemsService`、`NewStoriesService`、`NewOperationsService`、`NewHotService`、`NewReportsService` 均依赖对应 `*biz.*UseCase`。`StoriesUseCase.Get`、`OperationsUseCase.Health` 补齐原仓储旁路，Health 保留两秒超时。

CLI 的 status 经 `OperationsUseCase.Status`，report/push 经 `ReportsUseCase`；评测导出与运行分别经 `EvalExportUseCase.Export`、`EvaluationUseCase.Run`，依赖生命周期独立。job 的 `Runner` 调度 `Collector`、`Processor`、`ReportsUseCase`，不承接业务规则。

核验入口：`internal/biz/{stories,operations,reports,eval}.go`、`cmd/vane/{main,eval,wire}.go`、`internal/job/runner.go`。

## 2. DTO、DO 与存储投影

biz 模型已移除 JSON/XML/YAML/GORM 标签。HTTP DTO 与转换归 `internal/service/dto.go`；CLI JSON/YAML 与 Gold 解析归 `cmd/vane/{dto,gold}.go`；LLM 协议归 `internal/infra/llm/dto.go`。data 查询使用本层扫描形状后显式转换，报告与 Fact JSONB 由 `internal/data/snapshots.go` 的存储 DTO 编解码。

转换分别保留 nil、空集合、可空字段与日期；报告协议隐藏 Roundup、PushedAt 和原始评分。当前契约测试位于各边界的 `dto_test.go`、`data/snapshots_test.go`，不依赖领域标签或历史兼容分支。

## 3. 分页、派生值与资源引用

`ItemPage.NextCursor` 使用 `*biz.Cursor`；公开游标编解码归 `service/cursor.go`，用例显式处理 ID 转换错误。`CompleteItem`、`AverageScore` 在 biz 统一标题回退与综合评分，data 只返回原始值。

sitemap 仓储返回资源引用与修改时间，`ItemsUseCase.SitemapPages` 控制报告优先、剩余数量及 365 天窗口，service 映射 URI。核验入口：`biz/{items,sitemap}.go`、`data/{items,sitemap}.go`、`service/sitemap.go`，以及 biz/service 的 sitemap 测试。

## 4. 报告编排

`ReportsUseCase.generate` 决定时间窗口、最近七份历史去重与 `CompileReport`。`ReportRequest` 和专用 compile 回调让 `reportsRepo.GenerateReport` 在原事务与 advisory lock 内读取候选、复用快照、原子保存并提交，保留覆盖和 pushed_at 语义。

核验入口：`internal/biz/reports.go`、`internal/data/reports.go`；报告、并发生成、推送及 CLI 集成测试验证业务结果和原子性。

## 5. 归组、处理与采集

`PlanGrouping` 在 biz 决定 14 天召回窗口、主题、代表资料与有界重判；data 保留事务外模型选择、锁内候选复验及事件/资料原子更新。`NewCollectionResult` 决定首次采集、缓存元信息和下次调度，`SaveCollection` 保留唯一冲突去重、实际新增计数及同事务抓取记录。

`Processor` 产生显式 `StepResult.Stage`、`ProcessResult.Stage/ProcessedAt`，data 负责保存。独立连接上的 session advisory lock、取消清理与坏连接丢弃仍由 data 承担。

核验入口：`biz/{group,collect,process}.go`、`data/{group,collect,process}.go`；锁、取消、并发、恢复重试与分步处理集成测试。

## 6. ProviderSet 与配置边界

biz 仅 `providers.go` 依赖运行配置，映射为 `CollectorOptions`、`ProcessorOptions`、`EvaluationOptions`。data、biz、service、server、job 完整注册本层构造；cmd Wire 只组合集合与应用根对象，移除 `newCollector`、`newProcessor`。

Status 与 EvalExport 专用集合只 Open，不迁移或同步信源；EvalRun 不构造数据库。核验入口：各层 `providers.go` / `wire.go`、`cmd/vane/wire_gen.go`，以及 status/serve/eval CLI 集成测试。

## 7. 错误与枚举

`biz/states.go` 统一阶段、预筛、归组关系、分级、视图、分类、趋势与评测标签，生产 SQL 参数和业务命令共用。普通 SQL 及 schema 静态约束没有机械抽取成常量。

biz 定义语义错误，data 映射可识别存储错误，service 使用 `errors.Is` 映射状态；未知错误记录后脱敏返回，保留原错误文本与重试行为。核验入口：biz 错误定义、data 仓储映射及 service 错误处理。

## HTTP 服务封装

`server/http.go` 集中 Echo 构建与有序中间件，`routes.go` 负责路由装配，`server.go` 负责超时配置与运行/关闭。请求日志包住恢复层，以最终状态记录一次访问；Huma 保留结构校验，service 负责语义校验。请求截止时间向下传播 context，保留缓存、安全头、GET/HEAD、SPA、404 与取消语义。

核验入口：`internal/server/{http,routes,server}.go` 及 server 测试；协议 DTO、分类 RSS 和 sitemap 路径生成由 service 承担。

## 2026-10-27 目录与 HTTP 职责整理

当前资源 handler 位于 `internal/service/http_documents.go`（RSS、OPML、sitemap、robots）和 `internal/service/agent/http.go`（Agent HTTP 参数转换、Markdown 与 llms.txt 交付）。它们继续调用既有 service 与 biz 用例；文档生成失败在 handler 记录一次原始原因，再返回通用 500，访问日志仍由中间件记录一次。`server/routes.go`、`server/agent.go` 只注册资源路由与 MCP transport，`server/response.go` 保留通用 HTTP 错误响应。中间件顺序、GET/HEAD、缓存、安全头与取消传播保持原有契约。

Agent 实现与对应测试集中在 `internal/service/agent/`，`service.go` 保留 `AgentService` 及公共回答规则，`wire.go` 提供 Agent 自身的 ProviderSet。父 service 聚合集合并仅提供一次 Site；Agent 子包不导入父 service，不保留父包别名或兼容转发。

CLI 的 Gold 解析与评测命令合并在 `cmd/vane/eval.go`，主题重建命令与其它顶层命令放在 `cmd/vane/main.go`；`dto.go` 继续承担输出协议转换，`app.go` 继续承担启动生命周期，Wire injector 与各专用 ProviderSet 不变。上述 `gold.go` 等路径属于前期整改记录，当前核验使用这里列出的最终路径。

## 2026-10-27 核心调用链复查与功能归位

复查 server、service、biz、data 的现有调用链：资源 handler 依赖用例，JSON DTO 显式转换，Agent HTTP/MCP 共用服务校验，文档错误在服务边界记录一次。此次未发现新的跨层旁路；没有按资源数量机械拆包，也未扩大为整层重构。

OPML 编解码作为独立协议功能放在 `internal/service/opml/codec.go`：`Parse` 处理外部文档，`Marshal` 生成信源列表，XML 模型、解析测试与原样 fixture 随功能归位。`SourcesService.OPML` 仍先经用例取得启用信源，再调用编解码；CLI 直接使用 `opml.Parse`。协议子包不读取配置、访问存储或决定信源启用规则。

JSON DTO 与转换回到所属现有资源文件：资料、信源展示与主题在 `service/items.go`，事件在 `stories.go`，热点榜在 `hot.go`，日报在 `reports.go`；共享资料 DTO 仍在同一服务包复用。原 `service/dto.go` 删除，跨资源的 JSON 形状测试保留在 `dto_test.go`。DTO 字段、标签、nil/空集合以及错误契约保持不变；Agent 包、RSS、sitemap、装配和数据查询边界未因此拆分。

主题归属规则与对应测试迁入已有 `biz/taxonomy/assign.go`、`assign_test.go`。`taxonomy.AssignTopics` 只接收标题、原标题、主体公司、标签和分类，不依赖资料处理模型；归组发布的 `biz/story.go` 与批量重建的 `biz/topics_rebuild.go` 显式传入这些字段，沿用同一规则，没有新增根包转发。

data 侧复查未发现需要独立子包的新职责：PO、查询、DO 转换、锁内重验与原子持久化仍由 data 承担。归组、主题重建和出刊的事务不因目录整理机械拆开；本轮不修改数据库模型、事务边界或查询口径。

## 验证记录

- `a8e7760` 已完成 `make generate`、`go build ./...`、全仓 `go test -c`（仅编译）及 `git diff --check`。
- 根与 HTTP 同伴对调用链、模型归属、事务/锁、装配和 HTTP 结构完成只读审查，无阻断项。
- Luna 全仓测试已结束：除 LLM roundup 旧 DO 序列化断言及两项 eval run 旧 YAML fixture 外，其余测试通过。生产实现未因这些测试回退；测试改为直接验证领域结果或显式构造当前 CLI 协议。
- Luna 在 `2fc174c` 定向复测 `go test ./internal/infra/llm -count=1` 与 integration `TestEvalRun*` 全部通过（集成 4.952s）；再次确认 `make generate` 幂等、`go build ./...` 通过、工作区干净。原全量其余测试与修正后的受影响测试共同构成本轮行为验证，未声称再次运行全仓。

## 2026-10-28 图标资源与 HTTP 表达

`biz.SourceIcon.Version` 统一内容版本（SHA-256 前 12 字节的十六进制表示，空内容无版本）；`biz.ItemSource.IconVersion` 与信源 ID 表达当前资源引用，不携带本站 HTTP 路径。`Source.IconURL` 仍是配置提供的外部图标发现地址。data 保存 `icon_version`、输入指纹、内容与内容类型，资料、热点和历史日报查询只返回这些资源事实；历史日报图标仍按当前信源补全，不写入编辑快照。

公开 JSON 的 `icon_url` 保持不变：service DTO 与 CLI 各自在输出边界生成路径；service 图标 handler 使用同一内容版本判断查询版本、ETag 与缓存策略。现有 HEAD、404、安全头、错误及取消处理保留。ProviderSet 和 Wire 构造签名未变。

仓储 `IconState` 只读取当前输入指纹和资源版本，由 biz `Complete` 决定缓存复用；输入一致但缺版本时执行正常发现与保存，发现失败保留内容并允许后续重试。没有旧格式兼容、历史回填或读取时写入。模型不再声明 `icon_url`，但 AutoMigrate 不删除既有列；旧列不再读取。已有开发库通过正常补全获得当前资源版本，补全前或失败时列表输出空图标地址并使用现有浏览器 fallback。未执行运行库重置或修改真实配置。

验证：biz/data/service/server/cmd 相关包全量单测通过；独立临时 PostgreSQL 的 `Test.*(Report|SourceIcon|Migrate|Migration)` 集成测试通过。新增版本固定向量、空资源、输出 URL 转义与不完整资源失败/恢复回归。未运行全仓测试，独立验收另行记录。

## 2026-10-28 serve 启动前预留端口

`cmd/vane/serve` 在配置加载后调用 `server.Listen`，实际取得并持有监听端口，然后才初始化日志、装配 `NewServe`。入口以 defer 释放 listener；数据库装配失败或初始化取消同样释放。`App.Run` 将同一 listener 交给 `server.Run`，不再次绑定；已取消的调用不启动 job。HTTP 的关闭预算、请求处理和后台任务退出次序保持不变。构造器与 ProviderSet 未改，不涉及 Wire 重新生成；status 专用装配仍只打开已有存储。

验证：cmd/server 全包测试通过；TCP 数据库哨兵证明端口被占时没有数据库连接；初始化失败、初始化期间取消和 HTTP 关闭后均可重新绑定。独立临时 PostgreSQL 的真实 serve 子进程 HTTP/优雅退出、serve 初始化与 status CLI 集成测试通过。未操作运行实例、运行数据库或真实配置，未修改端口自动选择策略。

独立验收：Luna 的 `go test -race ./cmd/vane ./internal/server` 通过；隔离 PostgreSQL 下真实 serve 测试以 race 模式复跑通过。空白临时库执行 status 后仍无业务表，证明专用模式不会隐式迁移。

## 2026-10-28 Docker 构建与远端运行验证

Dockerfile 保留 `make build`、指定 GOPROXY 和 netbase，修复 apt 安装命令的续行及最终产物路径；Go 阶段使用 `CGO_ENABLED=0`，最终镜像只复制 `/src/bin/vane` 与独立 Node 阶段的 `web/dist`。构建上下文继续排除真实配置、日志、构建产物，并排除索引、环境文件和密钥文件。

在 172.18.80.22（Linux amd64、Docker 29.6.0）执行实际 `docker build` 成功，镜像为 `vane:validation-serve-listener`。提取成品二进制确认 Go 1.25.14、GOOS=linux、GOARCH=amd64、CGO_ENABLED=0。实际镜像中验证前端资源、CA 证书、netbase 文件及私有配置未打包。

容器使用独立 Docker network、独立 PostgreSQL 16、公开示例派生的禁用采集/LLM且空信源配置；未连接既有运行库。映射端口18080，首页、静态资源、资料/主题/热点/日报接口及 OpenAPI 返回200。容器内再次运行 serve 占用同一端口，TCP 数据库哨兵观测到0次连接。SIGTERM 退出码为0，原容器再次启动后健康检查通过，日志挂载可写。

此验证覆盖镜像构建、配置与静态资源路径、数据库初始化、端口冲突和进程生命周期；没有验证生产信源采集、外部模型调用及推送，也未发布镜像到远端仓库。

Terra 对远端真实容器的首页、日报与关于页完成1440和390两种视口冒烟：6次页面请求均为200，无页面异常、资源失败、损坏图片或横向溢出；空资料与空刊提示符合验证库状态。
