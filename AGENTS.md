# Vane 开发规范

本文件定义架构与变更验收标准。七组整改及 HTTP 封装的实现、核验入口和实际验证状态见 [架构整改记录](docs/agents/architecture-followups.md)。
后续修改仍须逐链核验，不以目录齐全或既有测试通过替代边界检查。

## 开始工作

- 使用用户最新消息的主要语言回应；中文使用简体中文，保留代码、命令和标识符原文。
- 根目录存在 `.codegraph/` 时，理解或定位代码先用 `codegraph_explore` 或 `codegraph explore "问题或符号"`；结果不足再用 `rg` 和源码读取。没有索引时直接搜索，不自行建索引。
- 创建任务、拆分 issue、读取需求或追加讨论前，读 [本地 Issue 约定](docs/agents/issue-tracker.md)。
- 分诊、标记可执行状态或更新 issue 状态前，读 [分诊标签](docs/agents/triage-labels.md)。
- 探索业务概念、调整领域规则或判断架构决策前，读 [领域文档约定](docs/agents/domain.md)，按其指针读取相关词汇表与 ADR。
- 从最新本地 `dev` 创建工作分支和隔离 worktree，开发期间本地提交，验收后合入 `dev`，清理已合并的工作分支与 worktree；保留其他任务的修改。用户要求提前合并或暂停测试时照做，并注明未验证项。
- 本机启动前确认配置路径、端口及所连数据库，避免覆盖正在使用的实例。

## 调用链与模型归属

```text
HTTP → service DTO ↔ biz UseCase → biz Repository 接口 → data PO → PostgreSQL
CLI / job → biz UseCase → biz Repository 接口 → data
```

| 层 | 职责与边界 |
| --- | --- |
| `internal/service` | HTTP 入参校验、DTO 与 DO 转换、Huma 错误映射；RSS、sitemap 等协议文档生成。依赖 biz 用例。 |
| `internal/biz` | 纯领域模型、业务规则、流程编排、用例、仓储与外部能力接口、语义错误。 |
| `internal/data` | PO、查询、DO 与 PO 转换、仓储实现、存储及外部客户端适配、事务与锁、原子持久化。 |
| `internal/server` | Echo/Huma 构建、路由注册、中间件、协议响应及静态资源交付。调用 service。 |
| `internal/job` | 调度与取消，调用 biz 用例；业务流程由用例编排。 |
| `cmd/vane` | 参数与配置加载、启动生命周期、ProviderSet 组合；CLI 调用用例并在输出边界序列化。 |

- service 即使只引用 `biz.Repository` 接口，直接调用仓储仍属于绕过用例；HTTP、CLI、job 的业务入口全部经过 biz 用例。
- service 和 biz 不导入 data；biz 不依赖 service、HTTP 框架或存储客户端。data 通过实现 biz 接口提供能力。
- DTO 归 service；HTTP Body 使用 DTO 并显式转换，不直接暴露 biz 模型。CLI 的输出形状与序列化归入口边界。
- DO 归 biz，不含 JSON、XML、Huma、GORM 等协议或持久化标签；PO 与扫描投影归 data。ID 类型按业务语义选择，字符串 ID 本身不构成违规。
- 分页协议字符串在 service 或 CLI 边界编解码，biz 与 data 传递类型化查询条件和结果。
- 业务选择、阈值、时间窗口、主题归属、代表资料、报告编译与业务状态流转由 biz 决定；data 将查询条件转成 SQL 并保存结果。
- 为锁内一致性执行领域计算时，使用 biz 定义的端口或业务回调。事务、锁、重新校验及原子提交留在 data；不得通过机械拆分事务破坏一致性，也不得让 data 自行选择业务策略。

## HTTP 服务封装

- 服务端入口集中展示中间件顺序，分离构造、配置、路由注册与生命周期；资源 handler 和 DTO 归 service。
- 复用 Echo 官方安全头、请求日志与恢复中间件；请求日志包住恢复层，以最终状态记录一次请求，未知 500 只向客户端返回通用错误。
- 参数绑定与结构校验使用 Huma 内置能力，业务入参校验归 service；保留结构错误 422 与语义错误 400 的现有契约，不增加反射校验层。
- 请求截止时间通过 context 向下传播，不启动脱离请求的并发 handler；保留缓存、安全头、HEAD、404、静态资源与取消行为。

## 仓储、配置与依赖装配

- 资源使用 `<Resource>Service`、`<Resource>UseCase`、`<Resource>Repository`；data 实现为私有 `<resource>Repo`。`Processor`、`Collector` 等业务名称可保留，但应明确其用例职责。
- 仓储各自持有 `data *Data`，`New<Resource>Repo` 返回 `biz.<Resource>Repository` 接口。存储形状不同处显式转换，不把 ORM builder 或 PO 传出 data。
- `Data` 只持有共享 `ORM *gorm.DB`；需要连接池时通过 `.DB()` 获取。连接初始化、迁移、信源同步和释放由 data 管理，仓储复用共享客户端。
- 各层 `ProviderSet` 完整注册本层构造器、接口绑定及配置 provider；cmd 的 Wire injector 只组合集合与应用根对象，不补列层内构造器。
- 运行配置到 biz 自有参数或选项的转换放在所属层装配文件（`wire.go` / `providers.go`）；biz 包内仅装配文件可依赖 `internal/config`，biz 用例及领域规则不依赖运行配置类型或读取配置文件。纯转发的 `newProcessor`、`newCollector` 不留在 cmd。
- 同一 Go 类型的不同语义值通过具名选项或参数结构区分，不能依赖 Wire 猜测字段或按位置选值。
- status 使用专用 ProviderSet，保留只打开既有存储、不迁移、不同步信源的语义。
- 改配置前读 [ADR-0001](docs/adr/0001-all-config-from-one-yaml-file.md)，同步公开示例、读取/校验和受影响调用方；同步本机被忽略配置所缺的配置段，保留已有值且不输出或提交秘密。
- 改数据模型、迁移、锁或事务前读 [ADR-0002](docs/adr/0002-gorm-model-migrations.md)，保留其中的一致性约束。

## 错误、常量与生成代码

- biz 定义语义错误；data 将可识别的驱动错误映射为 biz 错误，其余错误保留原因链。调用方用 `errors.Is` 判断，不匹配错误文本。
- service 将 biz 错误映射到 HTTP；未知错误记录到 `slog` 并返回通用 500，不暴露数据库、密钥或内部错误细节。CLI 在其边界处理输出和退出错误。
- 状态、类别等枚举值由所属层定义具名常量并统一引用；SQL 查询和业务流程使用同一语义来源。普通 SQL 文本不必一律抽成常量。
- 沿用 Echo、Huma、slog、GORM 及已有工具；参考分层思想不意味着引入 Kratos、Proto、ent 或 AIP。
- 修改 Wire 源定义后重新生成 `wire_gen.go`，生成文件与源改动同步；不手改生成文件。

## 变更与 review 验收

开发阶段架构整改以目标接口为准：移除废弃接口、纯兼容转发及绑定旧实现的测试，不新增旧代码或旧测试兼容层；测试围绕当前产品契约和业务不变量重写。

1. 追踪每条受影响的 HTTP、CLI、job 链路，列明用例、接口实现及调用方；检查新增入口是否绕过用例。
2. 核对 DTO/DO/PO 的归属与双向转换、错误映射、枚举值和配置同步；现有偏差不能成为新增旁路的理由。
3. 核对完整 ProviderSet、配置 provider、生成结果及专用启动模式，确认入口仅组合。
4. 涉及数据库或后台任务时，核对取消、重试、并发、锁、事务与部分成功的原有行为，给出对应测试证据。
5. 运行与改动相关的检查并报告实际结果；文档任务按授权范围核验引用与差异。测试通过只证明已覆盖行为，不能替代上述架构检查。

完成条件：每条受影响链路、接口实现、模型转换、装配与行为约束均可核验；明确尚未验证项及风险。本轮七组整改的实现与验证证据记录于 [架构整改记录](docs/agents/architecture-followups.md)；后续变更继续按上述条件验收。
