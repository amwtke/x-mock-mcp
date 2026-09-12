# X-Mock-MCP P0A Plugin Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立可独立安装/卸载的左右端插件框架，以真实测试插件验证策略注册、请求路由、进程清理和环境隔离。

**Architecture:** 左端对接应用，右端对接外部依赖。核心通过版本化 Plugin API 和通用策略代理管理插件；包安装状态与运行实例状态分开。先启动右端，再启动左端，创建失败整体回滚。

**Tech Stack:** Go 1.27.1、标准库 archive/zip、JSON、JSON-RPC 2.0 over private stdio、context、os/exec、testing。

---

执行入口与版本来源：[P0 主计划](2026-09-12-p0-implementation-plan.md)。本子计划不添加 MySQL 特殊分支，也不启动 Coding Agent。

## 文件职责

| 文件 | 责任 |
| --- | --- |
| `go.mod`、`go.sum`、`.gitignore`、`AGENTS.md` | 模块、固定依赖、产物忽略、固定左右端定义与依赖规则 |
| `pluginapi/manifest.go`、`manifest_test.go` | 插件角色、包身份、契约与校验 |
| `pluginapi/request.go`、`strategy.go`、`errors.go` | 请求信封、三类策略、结构化错误 |
| `pluginapi/preparation.go`、`preparation_test.go` | 通用资料/候选准备扩展、缺口报告及可运行状态校验 |
| `internal/plugin/catalog/archive.go`、`archive_test.go` | 解包、路径与摘要校验、原子安装 |
| `internal/plugin/catalog/catalog.go`、`catalog_test.go` | 启用状态、引用、停用卸载、恢复 |
| `internal/plugin/ipc/frame.go`、`peer.go`、`peer_test.go` | 行分帧、并发请求关联、取消与关闭 |
| `internal/plugin/runtime/process.go`、`proxy.go`、`process_test.go` | 进程所有权、启动握手、左右端 RPC 代理 |
| `internal/binding/manager.go`、`negotiate.go`、`manager_test.go` | 路由、能力交集、端口与启动回滚 |
| `internal/testkit/plugins/left-echo/main.go` | 左端测试插件，监听 TCP，转发 echo/v1 请求 |
| `internal/testkit/plugins/right-echo/main.go` | 右端测试插件，按配置返回带实例标识的结果 |
| `internal/testkit/bundle.go`、`process.go` | 编译与打包测试插件、等待就绪、清理子进程 |
| `integration/plugin_lifecycle_test.go`、`plugin_failure_test.go` | 真实进程完整生命周期和故障测试 |

## Task A1: 固定工具链与项目边界

- [x] 在执行时使用隔离工作树；读取 using-git-worktrees 后创建实施分支。当前设计与计划留在已确认提交中。
- [x] 准备项目本地 Go 1.27.1：从 go.dev 的 JSON 下载目录取得当前平台压缩包和 SHA-256，下载到 `.tools/downloads/`，校验后解压到 `.tools/go/1.27.1/`。不修改系统 Go 或全局 shell 配置；校验失败删除临时包并退出。
- [x] 创建 `go.mod`，完整初始内容：

```go
module xmock.local/x-mock-mcp

go 1.27.0

toolchain go1.27.1
```

- [x] 创建 `.gitignore`，忽略 `.tools/`、`.x-mock/`、`bin/`、`dist/`、`artifacts/`、`target/`。创建 `AGENTS.md`，写入以下执行约束：

```text
左端 = 对接应用；右端 = 对接外部依赖。
左右端均通过独立插件包安装、启用、停用和卸载。
核心依赖策略接口与注册表，不 import 具体协议插件。
所有运行固定插件版本、契约版本和场景版本。
MySQL 策略提供 QA 输入要求；依据自然语言、代码和 DDL 编译场景，不启动真实数据库。
先验证受支持行为；不以自动成功响应掩盖未实现能力。
依据 docs/superpowers/plans/2026-09-12-p0-implementation-plan.md 执行。
```

- [x] 运行 `.tools/go/1.27.1/go/bin/go version`，要求输出包含 `go1.27.1` 和实际平台。后续命令中的 `go` 均指这个可执行文件；执行会话把它所在目录加入自己的 PATH。
- [x] 提交：`chore: establish plugin project boundaries`。这是项目配置任务，不添加仅验证文件存在的测试。

## Task A2: 定义插件契约与策略接口

- [x] 创建 `pluginapi/manifest_test.go` 的表驱动测试：左/右合法角色通过；空 ID、`../mysql`、未知角色、`latest`、重复契约版本失败。先运行 `go test ./pluginapi -run TestManifestValidation -count=1`，确认缺少实现时失败。
- [x] 在 `pluginapi/manifest.go` 定义以下字段及 JSON 名称：

```go
package pluginapi

import "encoding/json"

type Role string

const (
    Left Role = "left"
    Right Role = "right"
)

type Reference struct {
    ID string `json:"id"`
    Version string `json:"version"`
    Role Role `json:"role"`
}

type Contract struct {
    ID string `json:"id"`
    Version int `json:"version"`
    Capabilities []string `json:"capabilities"`
}

type Descriptor struct {
    Ref Reference `json:"ref"`
    APIMajor int `json:"plugin_api_version"`
    Contracts []Contract `json:"contracts"`
    ConfigSchema json.RawMessage `json:"config_schema"`
    ScenarioSchema json.RawMessage `json:"scenario_schema,omitempty"`
    Preparation *PreparationContract `json:"preparation,omitempty"`
    Features []string `json:"features,omitempty"`
}

type Manifest struct {
    Descriptor Descriptor `json:"descriptor"`
    Platform string `json:"platform"`
    Entrypoint string `json:"entrypoint"`
    Files map[string]string `json:"files"`
}
```

ID 使用 `[a-z][a-z0-9-]{0,63}`；插件版本使用明确的三段数字版本，P0 不接受版本范围；APIMajor 必须为 1。Platform 使用 GOOS/GOARCH 格式，例如 darwin/arm64。契约 ID 是插件声明的字符串，核心不维护 mysql/kafka 枚举。每个 Files 值是小写 64 位十六进制 SHA-256。

- [x] 在 `pluginapi/request.go` 与 `strategy.go` 定义公共类型；不得把 MySQL 字段放进信封：

```go
package pluginapi

import (
    "context"
    "encoding/json"
)

type InstanceSpec struct {
    InstanceID string `json:"instance_id"`
    EnvironmentID string `json:"environment_id"`
    BindingID string `json:"binding_id"`
    ResourceID string `json:"resource_id"`
    Contract Contract `json:"contract"`
    Config json.RawMessage `json:"config"`
    Scenario json.RawMessage `json:"scenario"`
    ScenarioVersion int64 `json:"scenario_version"`
}

type Request struct {
    ID string `json:"request_id"`
    EnvironmentID string `json:"environment_id"`
    RunID string `json:"run_id"`
    BindingID string `json:"binding_id"`
    ResourceID string `json:"resource_id"`
    ConnectionID string `json:"connection_id"`
    ContractID string `json:"contract_id"`
    ContractVersion int `json:"contract_version"`
    ScenarioVersion int64 `json:"scenario_version"`
    DeadlineUnixMS int64 `json:"deadline_unix_ms"`
    Operation string `json:"operation"`
    Payload json.RawMessage `json:"payload"`
}

type Need struct {
    RequestID string `json:"request_id"`
    Continuation string `json:"continuation_token"`
    StateVersion int64 `json:"state_version"`
    DeadlineUnixMS int64 `json:"deadline_unix_ms"`
    Schema json.RawMessage `json:"schema"`
    Context json.RawMessage `json:"context"`
}

type Decision struct {
    Kind string `json:"kind"`
    Payload json.RawMessage `json:"payload,omitempty"`
    Need *Need `json:"need,omitempty"`
    Error *Failure `json:"error,omitempty"`
}

type Resolution struct {
    RequestID string `json:"request_id"`
    Continuation string `json:"continuation_token"`
    StateVersion int64 `json:"state_version"`
    Payload json.RawMessage `json:"payload"`
}

type Ready struct {
    InstanceID string `json:"instance_id"`
    Endpoints map[string]string `json:"endpoints"`
}

type Dispatch func(context.Context, Request) (json.RawMessage, error)

type OutcomeObserver func(Request, json.RawMessage, error)

type LeftStrategy interface {
    Describe(context.Context) (Descriptor, error)
    ValidateConfig(context.Context, json.RawMessage) error
    Start(context.Context, InstanceSpec, Dispatch) (Ready, error)
    Stop(context.Context) error
}

type RightStrategy interface {
    Describe(context.Context) (Descriptor, error)
    ValidateConfig(context.Context, json.RawMessage) error
    Start(context.Context, InstanceSpec) (Ready, error)
    Execute(context.Context, Request) (Decision, error)
    Complete(context.Context, Resolution) (json.RawMessage, error)
    Stop(context.Context) error
}

type DataStrategy interface {
    Resolve(context.Context, Need) (json.RawMessage, error)
}

type Capture struct {
    Request Request `json:"request"`
    Candidate json.RawMessage `json:"candidate,omitempty"`
    GenerationContext json.RawMessage `json:"generation_context,omitempty"`
    Response json.RawMessage `json:"response"`
}

type ExportSpec struct {
    Snapshot json.RawMessage `json:"snapshot"`
    Captures []Capture `json:"captures"`
}

type Verification struct {
    Passed bool `json:"passed"`
    Issues []Failure `json:"issues"`
}

type ScenarioExporter interface {
    Export(context.Context, ExportSpec) (json.RawMessage, error)
}

type ScenarioVerifier interface {
    Verify(context.Context) (Verification, error)
}
```

右端可声明 scenario.export、scenario.verify 两项 Features，分别通过独立扩展接口实现场景导出和业务调用约束校验。MySQL P0 必须实现；最小 echo 测试插件可以不声明。核心不解析协议专属场景，未声明扩展时也不发送对应 IPC 调用。

- [x] 在 preparation.go 增加可选 `scenario.prepare` 扩展，MySQL P0 必须声明；Descriptor.Preparation 只有声明该 feature 时才允许存在。核心按这些通用类型调用，不增加数据库字段：

```go
package pluginapi

import (
    "context"
    "encoding/json"
)

type PreparationContract struct {
    InputSchema json.RawMessage `json:"input_schema"`
    CandidateSchema json.RawMessage `json:"candidate_schema"`
    Guide string `json:"guide"`
}

type PreparationSpec struct {
    ProjectRoot string `json:"project_root"`
    Input json.RawMessage `json:"input"`
    Candidate json.RawMessage `json:"candidate,omitempty"`
}

type PreparationIssue struct {
    Code string `json:"code"`
    Path string `json:"path"`
    Message string `json:"message"`
    StepIDs []string `json:"step_ids,omitempty"`
}

type PreparationReport struct {
    Ready bool `json:"ready"`
    MissingInputs []PreparationIssue `json:"missing_inputs"`
    Ambiguities []PreparationIssue `json:"ambiguities"`
    Unsupported []PreparationIssue `json:"unsupported"`
    Diagnostics []PreparationIssue `json:"diagnostics"`
    Instructions string `json:"instructions,omitempty"`
    CompiledBody json.RawMessage `json:"compiled_body,omitempty"`
    InputDigest string `json:"input_digest,omitempty"`
    CompiledDigest string `json:"compiled_digest,omitempty"`
}

type ScenarioPreparer interface {
    Prepare(context.Context, PreparationSpec) (PreparationReport, error)
}
```

Ready=true 要求四个 issue 列表为空且存在 compiled_body 和两个摘要；只检查资料、不带 candidate 时不能返回 Ready=true。报告总大小遵循 IPC 上限。ProjectRoot 由 daemon 注入，不接受 MCP 客户端覆盖。通用错误描述资料/状态问题，数据库类型和 SQL 诊断仍归插件。

- [x] 添加准备契约校验测试：feature 与 schema 缺失/冲突、左端错误声明右端扩展、ready=true 但仍有缺口、ready=false 却包含可运行产物均拒绝。运行 `go test ./pluginapi -count=1`。非准备插件不必实现接口或伪造成功报告。

`Failure` 在 `pluginapi/errors.go` 定义为包含 Code、Message、Details(json.RawMessage) 的结构，实现 Error()。固定通用码：INVALID_ARGUMENT、PLUGIN_API_MISMATCH、CONTRACT_MISMATCH、PLUGIN_IN_USE、PLUGIN_NOT_ENABLED、PLUGIN_EXITED、PORT_IN_USE、DEADLINE_EXCEEDED、CANCELLED、STATE_CONFLICT、UNSUPPORTED、UNMATCHED_REQUEST、QUEUE_FULL、INVALID_RESULT。数据库错误码留在插件 payload 中。

Decision.Kind 只允许 completed、needs_data、unsupported；分别要求且仅允许 Payload、Need、Error 中对应的一项。先实现并验证这个通用联合类型校验，避免空的 completed 被当作成功。

- [x] 实现 `Descriptor.Validate() error`、`Manifest.Validate() error`、`Decision.Validate() error`。校验失败不产生进程或文件副作用。加入实际数据测试：

```go
func TestDecisionRejectsConflictingOutcomes(t *testing.T) {
    d := Decision{
        Kind: "completed",
        Payload: json.RawMessage(`{"value":1}`),
        Need: &Need{RequestID: "req-1"},
    }
    if err := d.Validate(); err == nil {
        t.Fatal("conflicting completion and generation request was accepted")
    }
}
```

该测试位于 package pluginapi，导入 encoding/json 与 testing。成功命令：`go test ./pluginapi -count=1`。提交：`feat: define versioned left and right plugin strategies`。

## Task A3: 安装、启用与卸载

- [x] 为 catalog 增加 zip 夹具构造 helper `bundle(t, manifest, files) (path, sha256)`，使用 archive/zip 写入 t.TempDir()，Files 摘要由夹具函数计算。测试安装后包存在且未启用；重复安装同摘要幂等；同 ID/版本但不同摘要拒绝。
- [x] 执行 `go test ./internal/plugin/catalog -run 'TestInstall|TestUninstall' -count=1`，记录失败原因。
- [x] 在 `archive.go` 按以下严格顺序实现 Install：

```text
Open package -> compare caller's archive SHA-256 -> read manifest
-> validate role/platform/API/paths -> extract to same-filesystem temp directory
-> reject symlinks, absolute paths, .., duplicate names, unlisted files
-> enforce 64 MiB archive / 256 MiB extracted total / 1024 files
-> verify each declared file digest and executable entrypoint
-> atomically rename into plugins/<role>/<id>/<version>
-> persist catalog using temp + rename -> return Reference
```

manifest.json 不要求在自身 Files 表中。除 manifest.json 外每个文件必须列入 Files；每项必须存在。入口路径必须位于包内且是普通可执行文件。失败清理本次临时目录，保留先前已安装版本。

- [x] 实现以下 catalog 公共方法，所有元数据修改持有项目级文件锁；Install 完成前不注册记录：

```text
Open(root string) (*Catalog, error)
Install(ctx context.Context, archivePath string, expectedSHA256 string) (pluginapi.Reference, error)
Enable(ref pluginapi.Reference) error
Disable(ref pluginapi.Reference) error
Uninstall(ref pluginapi.Reference) error
Acquire(ref pluginapi.Reference) (release func(), err error)
List() []Record
```

Record 包含 Ref、ArchiveSHA256、Enabled、ActiveInstances。Acquire 只接受已启用版本，增加引用并返回一次性 release；在持锁区内检查活动引用，避免“检查后又启动”的竞态。进程内引用在 daemon 重启时根据实例恢复流程处理，不盲信磁盘上的旧计数。

- [x] 测试停用/卸载有活动引用时返回 PLUGIN_IN_USE；release 后停用、卸载成功，另一端插件仍然存在。增加 `../escape`、符号链接、损坏摘要、不同平台与并行 Acquire/Uninstall 的负例。
- [x] 运行 `go test -race ./internal/plugin/catalog -count=1`。提交：`feat: manage independent plugin package lifecycles`。

## Task A4: 并发 Plugin IPC

- [x] 先用 net.Pipe 或 io.Pipe 创建一对 Peer；提交两个请求，让服务端倒序返回，断言客户端分别收到正确响应。增加其中一个请求取消而另一个成功的测试。
- [x] 运行 `go test ./internal/plugin/ipc -run 'TestOutOfOrder|TestCancel' -count=1`，确认失败。
- [x] 实现 Peer 的公共 API 和消息形式：

```text
NewPeer(reader io.ReadCloser, writer io.WriteCloser, handler Handler) *Peer
Call(ctx context.Context, method string, params any, result any) error
Notify(ctx context.Context, method string, params any) error
Close() error

Handler = func(ctx context.Context, method string, params json.RawMessage) (any, error)

request:  {"jsonrpc":"2.0","id":"core-1","method":"execute","params":{}}
response: {"jsonrpc":"2.0","id":"core-1","result":{}}
cancel:   {"jsonrpc":"2.0","method":"cancel","params":{"request_id":"core-1"}}
```

每行一条 UTF-8 JSON 消息，最大 4 MiB。使用单读循环、加锁单写器、pending[id] waiter 表和每个入站请求的 cancel func；处理函数放到有并发上限的 goroutine 中，读循环不能被处理函数阻塞。核心与插件使用不同请求 ID 前缀。

ctx 完成时原子摘除 pending，发送 cancel 通知并返回 ctx 错误；收到迟到响应丢弃。EOF/Close 时完成所有 waiter 并取消正在执行的 handler。JSON number 解码使用 UseNumber，不能把业务 BIGINT 自动变成 float64。

- [x] 添加超过消息上限、未知响应 ID、重复响应、非法 JSON、请求与响应字段混用、处理函数 panic 的测试；错误不能杀死 daemon 或使其他 waiter 永久等待。
- [x] 运行 `go test -race ./internal/plugin/ipc -count=1`。提交：`feat: add cancellable multiplexed plugin IPC`。

## Task A5: 进程策略代理与原子 binding

- [x] 先实现 testkit 的三个进程模式：正常 Describe/Start、Start 返回错误、启动后主动退出。进程模式由测试专用可执行文件参数确定，不在生产 Registry 中写测试插件名称分支。
- [x] 为创建 binding 编写失败测试：右端成功后左端失败，断言右端 Stop 被调用、两个 catalog 引用释放、路由未发布。运行 `go test ./internal/binding -run TestCreateRollsBack -count=1`。
- [x] Process 启动使用 exec.Command 的参数数组，入口从已验证安装目录解析；stdin/stdout 专用于 Peer，stderr 进入有界实例日志。Describe 返回的角色/API/契约必须与 manifest 一致。Start 限时 10 秒，Stop 限时 5 秒；超时结束所属进程并 Wait 回收。
- [x] 实现 LeftProxy/RightProxy，分别实现 A2 接口。左端回调 `dispatch` 的信封中，environment_id、binding_id、resource_id、run_id 由核心按实例归属覆写；插件不能伪造另一个环境的身份。
- [x] RightProxy 按声明转发 prepare/export/verify 扩展；为准备工具提供短生命周期进程路径：Acquire 已启用版本 → 启动进程/Describe → Prepare → 关闭 IPC/Wait → release。不调用 Start，不创建 binding 或端口；取消/崩溃也必须回收引用。配置准备时限 30 秒，不在这里等待 LLM。
- [x] 实现 binding.Manager 的算法：

```text
Validate requested references, enabled state, config and contract intersection
Acquire left/right catalog references
Create private right and left strategy proxies
Start right with the immutable scenario snapshot
Register provisional dispatch context for this binding
Start left with the effective capabilities and requested listen address
Check returned endpoints and READY instance ID
Publish binding as ready
On any error: stop created instances in reverse order, remove route, release refs
```

Manager 接口固定为 Create(ctx, Config) (Binding, error)、Dispatch(ctx, Request) (json.RawMessage, error)、Destroy(ctx, bindingID) error。Config 包含左右端 Reference 与配置、所需 Contract、environment/resource ID、固定场景、DataStrategy 及可选 OutcomeObserver。Binding 包含 ID、Ready endpoints 与固定版本。

Dispatch 只按 bindingID 查已建立策略；调用右端 Execute；completed 返回 Payload，unsupported 返回其错误，needs_data 调用 DataStrategy.Resolve 后调用同一个右端 Complete。阶段切换校验截止时间与实例存活，禁止换到别的右端完成旧 continuation。Dispatch 返回前调用一次 OutcomeObserver，报告最终 payload 或错误；DataStrategy 本身不调用右端 Complete。

- [x] 创建能力不相交、重复端口、重复销毁、并发创建/销毁测试。测试右端崩溃后取消相关请求并关闭同 binding 左端；另一个 binding 继续完成请求。创建环境含多 binding 时，某一个创建失败也撤销本环境已创建的其他 binding。
- [x] 用声明 scenario.prepare 的测试进程验证准备无需左端、不会监听应用端口、停用后不能准备、准备期间不能卸载、取消后引用释放；测试插件只回传 opaque 输入，核心不解释 QA 或 DDL。
- [x] 运行 `go test -race ./internal/plugin/runtime ./internal/binding -count=1`。提交：`feat: bind isolated plugin strategies with rollback`。

## Task A6: 双端真实插件验收

- [x] 在两个 testkit 插件 main 中实现统一 Plugin API。左端监听 `127.0.0.1:0`，每行接收 `{"value":"hello"}` 并 dispatch；右端返回 `{"value":"hello","instance_id":"configured-id"}`。右端只持有其实例配置，核心不认识这个 payload。
- [x] 使用测试工具分别编译、打包两个插件，安装到同一个临时项目；测试只调用 catalog、runtime、binding 公共 API，不从测试中直接 new 右端具体类型。
- [x] 执行以下真实外部行为场景，每个场景拥有独立项目和 cleanup：

```text
TestPluginLifecycle:
  install both -> assert no listener -> enable -> create binding
  -> TCP request -> assert response instance ID
  -> disable while active fails -> destroy -> disable -> uninstall left only
  -> right package still installed -> uninstall right

TestPluginCrashIsolation:
  create binding A and B with independent plugin processes
  -> terminate right A -> A fails and left A closes
  -> B returns its original instance ID -> destroy B

TestContractMismatch:
  left supports echo/v1, right supports different/v1
  -> create fails before a listener exists

TestPluginExtensionWithoutCoreImport:
  install a second package with a new ID and the echo/v1 contract
  -> route through it without rebuilding or changing core packages
```

- [x] 运行 `go test -race ./integration -run 'TestPlugin|TestContractMismatch' -count=1`，然后运行 `go test ./... -count=1`。后一个命令此时只覆盖已创建的 P0A 包。
- [x] 用 `go list -deps ./internal/binding ./internal/plugin/catalog` 检查没有 contracts/mysqlv1、plugins 或 MCP SDK 导入。将这个依赖约束加入 integration 的 Go 包依赖检查，防止未来协议实现渗入核心。
- [x] 提交：`test: verify installable dual-end plugins across processes`。P0A 完成后继续 P0B，不宣称数据库兼容已完成。
