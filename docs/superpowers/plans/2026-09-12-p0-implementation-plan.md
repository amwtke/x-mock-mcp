# X-Mock-MCP P0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付可独立安装和卸载的左右端插件框架，用 MySQL 读取场景完成 Spring Boot、Codex、Claude Code 的首次协作与固定回放。

**Architecture:** 左端对接应用，右端对接外部依赖；两端均为独立进程插件，以显式 binding 和版本化契约连接。核心只依赖策略接口、注册表和通用请求信封；MCP 是两个 Coding Agent 共用的控制入口。运行时的数据补全采用 Agent 主动取件与回填，固定回放不调用模型。

**Tech Stack:** Go 1.27.1；官方 MCP Go SDK v1.7.0；go-mysql v1.16.0；JSON Schema；本地 JSON-RPC Plugin API；Spring Boot 3.5.16、JDK 21、Connector/J 9.7.0、HikariCP 6.3.3。

---

## 已确认设计与实施范围

依据：[已确认设计](../specs/2026-09-12-x-mock-mcp-design.md)。用户于 2026-09-12 确认。

本文件是执行入口，三个子计划按顺序执行，每个子计划有明确产物和独立验收。P0 不包含 MySQL 写事务、ClickHouse、Kafka、独立模型 API、真实依赖转发或在线插件市场。

| 顺序 | 子计划 | 完成后的可验证产物 |
| --- | --- | --- |
| P0A | [双端插件框架](2026-09-12-p0a-plugin-foundation.md) | 可安装的左右端测试插件，通过真实子进程完成路由、取消、回滚和卸载 |
| P0B | [MySQL 插件对](2026-09-12-p0b-mysql-plugin-pair.md) | `mysql-wire` + `mysql-mock`，真实 JDBC/Hikari 读取、预处理和类型验证通过 |
| P0C | [MCP、场景协作与应用验收](2026-09-12-p0c-agent-e2e.md) | daemon、CLI、stdio 桥接、Agent 回填、Spring Boot 流程和两个宿主的实际验收 |

禁止把只通过 MCP Inspector、只成功建立 TCP 连接、或只有 SELECT 1 成功当作 P0 完成。

## 版本与执行环境

本轮只编写计划，没有安装运行时、编译产品或执行产品测试。当前环境核对结果：

- 仓库原先为空；已确认设计已提交为 `453cbe9`。
- `go` 不在 PATH 中，P0A 第一个任务准备项目本地 Go 工具链。
- `java` 是 Homebrew JDK 21.0.12；Maven 3.9.16 默认使用 JDK 26.0.2。Java 验收命令显式指定 JDK 21，避免运行组合漂移。
- 本机 Codex CLI 是 0.154.0；未找到 Claude Code 命令。实际宿主验收记录实施时的版本，缺少宿主时保持该项未通过，不用脚本模拟结果代替。

锁定以下依赖，实施时提交 go.sum 和 Maven dependency tree。它们是首个验收组合，不是“兼容所有版本”的声明。

| 项目 | 固定值 | 依据 |
| --- | --- | --- |
| Go | 1.27.1 | [官方下载页](https://go.dev/dl/) |
| MCP Go SDK | v1.7.0 | [发布版本](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0)；官方兼容表包含 2026-07-28 与旧协议 |
| MySQL 协议库 | github.com/go-mysql-org/go-mysql v1.16.0 | [固定 tag](https://github.com/go-mysql-org/go-mysql/tree/v1.16.0)；已阅读 prepared metadata 处理源码 |
| SQL parser | github.com/pingcap/tidb/pkg/parser v0.0.0-20260504140133-511dba1dbe17 | go-mysql v1.16.0 的固定依赖；只由 MySQL 契约/右端包使用 |
| JSON Schema | github.com/google/jsonschema-go v0.4.3 | MCP Go SDK v1.7.0 已固定的依赖 |
| YAML | gopkg.in/yaml.v3 v3.0.1 | 项目配置解析；启用 KnownFields 拒绝拼错字段 |
| Spring Boot | 3.5.16 | [固定 BOM](https://repo.maven.apache.org/maven2/org/springframework/boot/spring-boot-dependencies/3.5.16/spring-boot-dependencies-3.5.16.pom) |
| Connector/J / HikariCP | 9.7.0 / 6.3.3 | 上述 BOM 的 mysql.version 与 hikaricp.version |
| Java 验收 | JDK 21.0.12，Maven 3.9.16 | 当前本机可用组合；CI 也固定 JDK 21 |

MCP SDK 的新协议 HTTP 模式设置 `Stateless: true`，业务状态通过 environment_id/run_id 保存。参考 [SDK 兼容表](https://github.com/modelcontextprotocol/go-sdk#version-compatibility)。不会因为去掉 MCP 会话而去掉应用运行状态。

## 文件和依赖边界

```text
cmd/x-mock/                         CLI 与 daemon 入口
pluginapi/                         通用策略接口、manifest、信封、错误码
internal/plugin/catalog/           项目插件包、版本、启用状态、引用计数
internal/plugin/ipc/               并发 JSON-RPC、取消、消息大小限制
internal/plugin/runtime/           子进程、策略代理、启动与退出清理
internal/binding/                  契约协商、实例绑定、创建回滚
internal/scenario/                 场景版本、固定快照、导出
internal/generation/               StrictReplay、AgentFill 与领取队列
internal/run/                      后台测试任务、运行状态、停止
internal/trace/                    请求事件与分页运行记录
internal/app/                      跨模块用例、daemon 实例锁
internal/transport/mcp/            MCP 工具、HTTP、stdio 桥接
contracts/mysqlv1/                 MySQL 请求/响应/场景契约
plugins/left/mysql-wire/           MySQL 左端可执行插件
plugins/right/mysql-mock/          MySQL 右端可执行插件
internal/testkit/                  最小左右端进程插件和通用测试工具
integration/                      跨进程、MCP、插件故障与回放测试
examples/springboot-orders/        JDBC/Hikari 与 Spring Boot HTTP 样例
examples/scenarios/                固定查询与待补全查询场景
scripts/                          构建、打包、验收入口
docs/compatibility/                实测组合、宿主轨迹与已知范围
```

依赖规则：pluginapi 只依赖 Go 标准库；binding、catalog、generation 不 import 具体协议包；左右端插件可以依赖 pluginapi 和相应 contracts；MCP SDK 只由 transport/mcp 和协议验收测试使用。核心不 import plugins，插件也不 import internal/app。

不要创建多语言运行框架、服务发现集群、全局插件市场或跨项目共享卸载系统。P0 的项目数据放在 `.x-mock/`，运行产物和本地凭据不提交。

## 公共行为约束

1. 安装只导入插件包；启用允许创建实例；创建 binding 才启动左右端和监听端口。停用和卸载遇到活动实例返回 PLUGIN_IN_USE。
2. 右端先就绪，左端再监听；任一步失败则创建环境整体回滚。不同 binding 的进程与状态隔离。
3. 请求按不可变 binding 路由；校验插件 API、契约版本与能力交集，不按数据库名称编写核心 switch。
4. MySQL P0 只支持已声明的读取行为、必要的连接设置和确定性的系统元数据；DDL、写入、显式事务返回未支持。
5. 自动验收使用短超时；人工 Agent 探索配置 request_timeout_ms=120000、lease_ms=60000，MCP 单次拉取最多等待 2000ms。剩余时限不足时不接受新的领取或回填。
6. request_id、lease_token、continuation_token 都使用不可预测标识；结果校验通过后仍需右端 Complete，候选数据不能直通左端。
7. scenario_version、插件版本、包摘要、schema/contract 版本在创建环境时固定；运行中不切换到新版本。
8. 回归失败以测试退出码、未匹配请求、协议错误和预期调用数量共同判断。模型不能修改正在执行的测试断言。

## 执行顺序与提交边界

- [ ] 执行 P0A 的 A1–A6，每个任务完成其失败验证、实现、成功验证与一次聚焦提交。
- [ ] 执行 P0B 的 B1–B4，先证明列元数据与驱动行为，再连接 Agent。
- [ ] 执行 P0C 的 C1–C6，自动化验证通过后再做真实宿主验收。
- [ ] 按下面的覆盖表逐项填写证据路径；只有全部必要项目通过才标记 P0 完成。

测试失败说明兼容问题，不是放宽规则的理由。例如 server prepare 不通时，修正 metadata 实现，不能在所有测试中禁用 server prepare 后声称已支持。

## 设计要求到任务的映射

| 必须覆盖的要求 | 实施任务 | 验收证据 |
| --- | --- | --- |
| 固定左右端定义、策略接口 | A1、A2 | pluginapi 与核心导入边界测试 |
| 两端独立安装/启停/卸载 | A3、A6 | catalog 生命周期与真实插件测试 |
| 跨进程注册、协议协商、路由 | A2、A4、A5、A6 | 两端契约不匹配、并发响应和取消测试 |
| 端口冲突、启动回滚、崩溃隔离 | A5、A6、C3 | 故障测试及无残留进程/监听器断言 |
| MySQL 驱动、探活、文本查询 | B1、B2、B3、B4 | 固定 JDK/JDBC/Hikari 测试 |
| server prepare 与类型元数据 | B1、B3、B4 | prepare 元数据、执行结果、NULL/空集/错误测试 |
| 场景版本、固定回放、错误查询失败 | B2、C1、C5 | 相同快照重复回放、参数变化负例 |
| 领取、回填、过期、重复、断连 | C2、B3、C5 | fake clock、竞态与 TCP 断连测试 |
| 后台运行，不让 Agent 与测试互相等待 | C3、C5 | run_start 立即返回，之后可 resolve |
| MCP HTTP 和 stdio 共用 daemon | C4、C5 | MCP 旧/新协议及桥接实例计数 |
| Codex 和 Claude Code 实际协作 | C6 | 各一份实际宿主轨迹与版本记录 |
| 插件版本与报告可追溯 | A3、C1、C6 | 包摘要、场景版本与依赖树 |

## 完成标准

P0 的演示顺序固定为：安装左右端 → 启用 → 导入场景 → 创建 binding → 启动后台测试 → 获取缺少数据的查询 → 回填 → 应用断言通过 → 导出场景 → 新环境离线回放 → 销毁环境 → 停用并卸载。

自动测试输出保存到 `artifacts/p0/`；宿主验收摘要写到 `docs/compatibility/p0.md`。实际运行记录会包含具体耗时与版本，计划中的命令和断言不作为运行成功的证据。
