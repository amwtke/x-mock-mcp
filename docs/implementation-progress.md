# 实施记录

## P1.1 MyBatis（2026-09-12）

执行方式仍为当前会话逐项实现与验证，无子代理。复用隔离工作树 `.worktrees/p0`，分支 `feat/p1-mybatis`，基于已合并 master 的 P0 68ba0aa。

固定约束再次明确：**不安装、不启动真实 MySQL、容器数据库或替代 SQL 引擎，不转发真实数据库兜底，不配置额外模型 API。** Spring Boot JDBC 只连接左端 MySQL 协议端口。Coding Agent 经 MCP 从 QA/源码/DDL 提交场景和参考实体，右端维护状态，左端编码 MySQL 应答；购物车由实际应用写入产生。

| 任务 | 状态 | 实际验证 |
| --- | --- | --- |
| P1.1-1 来源策略入口 | 完成 | 先观察 source 不识别和 XML 降级绕过的失败，再通过 Java / XML 路由回归；核心不 import ORM |
| P1.1-2 静态 Mapper | 完成 | 真实摘要、namespace/id、SQL AST、参数顺序与类型；动态节点、替换、实体、漏条件、重复 ID、字面量改变拒绝；右端 race 通过 |
| P1.1-3 MyBatis 样例 | 完成 | Starter 3.0.5 / MyBatis 3.5.19；真实 BoundSql 不使用 DataSource，真实 Mapper 结果映射与生成键；ShopDataAccess 选择互斥 Repository |
| P1.1-4 E2E / 回填 | 完成 | 真实 HTTP/Chromium；MCP 测试客户端 AgentFill + 3 次新环境回放轨迹一致；5 类应用缺陷保持 QA 并失败 |
| P1.1-5 版本与交付 | 完成 | 真实历史右端 0.1.0 和新版 0.2.0 共用原左端 0.1.0，独立卸载；完整 P0/P1 回归、vet、独立构建、依赖边界审计通过 |

全量入口 `scripts/verify-p1.sh` 已通过，含既有 JDBC、事务、P0 购物、MCP HTTP/stdio、MyBatis、版本并存和打包。更新后的 `demo-replay.py` 已分别执行 mybatis-http、mybatis-browser、shop-http；环境销毁，所属 daemon 正常退出。生产和测试 Go 依赖与 Maven 依赖均检查，未发现已列明的数据库引擎/启动库。

验收详情见 [P1.1 记录](compatibility/p1-mybatis.md) 与 [脱敏摘要](compatibility/mybatis-2026-09-12.json)。MyBatis-Plus BaseMapper/Wrapper 自动 SQL 是下一独立增量，本次未把静态 XML 通过当成 Plus 完成；动态 SQL/新数据库类型也未隐式扩大支持。Claude 账号/真实模型 E2E 的豁免继续有效。

## P0（已合并并推送 master）

执行方式：当前会话逐项实现与验证。工作分支 feat/p0。

固定要求：左端对接应用、右端对接外部依赖，均独立安装卸载；数据仅由 Coding Agent 提供；不启动真实数据库。

| 任务 | 状态 | 实际验证 |
| --- | --- | --- |
| A1 工具链与边界 | 完成 | 官方 SHA-256 校验通过，Go 1.27.1 darwin/arm64；隔离工作树与项目规则已建立 |
| A2 通用插件契约 | 完成 | 先观察缺少类型导致失败，随后 go test ./pluginapi 通过；覆盖角色/版本、互斥结果、资料缺口 |
| A3 插件包生命周期 | 完成 | go test -race ./internal/plugin/catalog 通过；独立角色、活动引用、并发、路径/摘要/平台/链接/重复包及失败清理 |
| A4 并发 IPC | 完成 | go test -race ./internal/plugin/ipc 通过；乱序、取消、写阻塞时取消、EOF、panic、4 MiB 上限与非法帧 |
| A5 进程代理与 binding | 完成 | runtime/binding race 测试、真实启动回滚、端口占用与准备进程回收通过 |
| A6 真实插件验证 | 完成 | 真实 TCP/子进程生命周期、崩溃隔离、新 ID 插件、无左端准备通过；go test ./... 通过 |
| B1 MySQL 类型契约 | 完成 | BIGINT 精度/溢出、NULL/列元数据、OK 计数与严格 JSON schema 测试通过 |
| B2 QA 输入与编译 | 完成 | QA/证据/DDL/SQL、固定初终态、写入预演与摘要漂移测试；真实业务负例在 C5 验证 |
| B3 右端查询 | 完成 | 查询匹配、确定性元数据、实体填充、迟到拒绝与初态导出 race 测试通过 |
| B4 写入与事务 | 完成 | 读写、外键/唯一键/溢出、found-rows、取消、提交/回滚/冲突与断连通过 |
| B5 左端 MySQL 协议 | 完成 | 文本/二进制编码、真实 TCP prepare/execute、EOF 取消与实际 JDBC 通过 |
| B6 JDBC/Hikari | 完成 | JDK 21.0.12、Connector/J 9.7.0、HikariCP 6.3.3；两查询模式及两 affected-rows 模式通过，日志 artifacts/p0/jdbc-*.log |
| C1 准备、快照与导出 | 完成 | 版本 CAS、持久化、快照、轨迹上限/游标、成功生成导出；保留初始空车；业务规则/状态版本/事务阶段随轨迹保存 |
| C2 Agent 请求队列 | 完成 | 领取、过期、显式换主、最终完成确认、幂等、schema 拒绝、上限与取消 race 测试通过 |
| C3 后台任务 | 完成 | 快速返回、具名 argv、超时/停止进程组、输出上限、非零退出、插件/轨迹故障分类、退出 0 仍需验证 |
| C4 MCP/CLI | 完成 | 16 工具、旧/新协议、HTTP 鉴权、真实 daemon 与两个 stdio 子进程、重连/项目锁/端口冲突；Claude schema 简写回归修复 |
| C5 购物浏览器 E2E | 完成 | 真 HTTP/Chromium；AgentFill 与三次新环境回放业务轨迹一致；双客户端换主；四类业务缺陷保持 QA 预期并被拒绝 |
| C6 宿主兼容与交付 | 完成 | Codex 原生 prepare/next/resolve、浏览器与回放通过，原生清理通过；Claude HTTP/stdio CLI 接入均 Connected，无登录/模型 E2E；构建、HTTP/浏览器演示、指南及完整自动验收均通过 |

验收入口：`bash scripts/verify-p0.sh`；详细版本、宿主范围和证据见 [兼容性记录](compatibility/p0.md)。原始日志和私有运行状态不提交；[Codex 摘要](compatibility/codex-native-2026-09-12.json) 不含令牌。准备依赖、HTTP 演示和浏览器演示脚本均已实际执行。

实施调整：

- 用户明确免除 Claude 账号和真实模型 E2E；保留真实 Claude CLI 接入检查，多客户端共连由自动 MCP 客户端验证，未将其冒充为 Claude 模型结果。
- B2 的写入预演依赖 B4，B2–B4 按一个可编译的右端插件提交；C1–C4 的通用编排、队列、MCP/CLI 也按相互依赖的实现组合提交。原计划细分文件在现有小包中合并，实际测试入口见兼容性表。
- go-mysql 1.16.0 未开放 CLIENT_FOUND_ROWS 配置，左端在首个 HandshakeV10 报文中显式宣布该能力；实际 JDBC 两种计数配置均已验证。VARCHAR prepared 参数适配了驱动实际使用的 TypedBytes。
- 换主在右端已进入 resolving 时返回冲突；租约固定 15 秒，环境业务请求最长 300 秒。停止所属进程组先 TERM，1 秒后 KILL，而非原计划 5 秒。
- QA 可选空字段按规范 JSON 比较，防止编译产物再次保存时误拒绝；初终态断言保持不变。右端记录未支持行为/错误，测试吞掉错误也不能直接得到成功验证。
- 实际 Claude CLI 发现工具字段 schema 的布尔简写不兼容，MCP 控制面改为明确 JSON object；插件字段仍独立校验。HTTP/stdio 修复前后输出均保留。
- 负例的漏用户过滤在 prepare 阶段被源码 SQL 校验拒绝，其余三种由真实 HTTP/状态断言拒绝；浏览器完成购物正例和生成/回放验收。

最终验证：`scripts/verify-p0.sh` 的版本/依赖、core-race、integration、vet、build 全部 PASS；修正文档链接并刷新 QA 文件摘要后，真实 HTTP/浏览器样例再次通过。两个手工启动的 daemon 均正常退出，示例环境销毁后两端活动引用为 0。后续已按用户要求推送 feat/p0 并合并、推送 master；两者当时均为 68ba0aa，本地证据保留。
