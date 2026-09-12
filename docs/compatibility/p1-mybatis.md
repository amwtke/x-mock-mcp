# P1.1 MyBatis 接入与验收

本阶段保持核心价值：不安装、不启动真实 MySQL、容器数据库或替代 SQL 引擎。真实 Spring Boot / MyBatis / JDBC 请求只发送到左端 mysql-wire 的 TCP 监听；右端从 QA、代码、DDL 校验的场景维护 mock 状态并产生结果，由左端编码成 MySQL 应答。只有已连接 Coding Agent 经 MCP 准备场景/回填参考实体，无额外模型 API。

## 固定版本

| 组件 | 本阶段版本 |
| --- | --- |
| 左端 mysql-wire | 0.1.0，包摘要与 P0 相同 |
| 右端 mysql-mock | 0.2.0，增加源码策略与 schema 可选 source |
| 线契约 / MCP 工具 | mysql.operation/v1 / 原 16 工具 |
| MyBatis Starter / MyBatis-Spring | 3.0.5 / 3.0.5 |
| MyBatis | 3.5.19 |
| Spring Boot / JDK | 3.5.16 / 21.0.12 |
| Connector/J / HikariCP | 9.7.0 / 6.3.3 |
| Playwright / Chromium | 1.62.0 / 已准备的 Chromium 151 |
| 当前平台 | macOS arm64 |

Starter 3.0 对应 Boot 3.2–3.5；具体依赖同时经 Maven dependency:tree 确认。[官方版本矩阵](https://mybatis.org/spring-boot-starter/mybatis-spring-boot-autoconfigure/)、[3.0.5 发布 POM](https://repo.maven.apache.org/maven2/org/mybatis/spring/boot/mybatis-spring-boot/3.0.5/mybatis-spring-boot-3.0.5.pom)。

## 覆盖范围

| 验证 | 内容 |
| --- | --- |
| 右端策略 | java-literal / mybatis-xml 独立注册；XML 不能走 Java 降级 |
| SQL 来源 | 实际 XML 摘要、namespace/id、完整 SQL AST、参数名/顺序/jdbcType；CDATA、注释、标准 DTD 本地识别 |
| 真实框架 | MyBatis XMLMapperBuilder / BoundSql 离线解析，不配置 DataSource；constructor resultMap 和 Jdbc3KeyGenerator |
| HTTP / Chromium | 同一 S1–S6 QA，原业务 Service/Controller，共用测试断言；真实 Mapper 执行六条 SQL |
| MCP 回填 / 回放 | 测试 MCP 客户端模拟 Agent 回填，实际插件、HTTP、浏览器；AgentFill 后 3 个干净环境 StrictReplay，业务轨迹一致 |
| 写入 / 隔离 | 首次 INSERT 返回稳定生成键，重复 UPDATE 数量 1→2；合计 99→198 元；U2 空车，商品库存不变 |
| 缺陷 | 漏 INSERT、错误累计、漏用户条件、强制回滚、Mapper 实参交换；刷新源码摘要但保留 QA，prepare 或真实 HTTP/状态断言失败 |
| 插件升级 | 从 68ba0aa 历史源码构建实际 0.1.0 右端，和 0.2.0 同项目安装并分别绑定同一 0.1.0 左端；卸载互不影响 |
| 数据库边界 | 检查解析后的 Go/Java 依赖，拒绝已列明的数据库引擎和容器启动库；客户端驱动和 parser 保留 |

MyBatis 的 `#{}` 最终绑定为 PreparedStatement 参数；生成键通过 JDBC 返回。实现与验收使用真实框架完成这两条路径。[官方 Mapper 文档](https://mybatis.org/mybatis-3/sqlmap-xml.html)。

## 执行与证据

2026-09-12 实际执行 `verify-p1.sh`，版本/依赖、core-race、integration、vet、build、离线 Mapper 与依赖审计全部 PASS。随后将 Go 审计扩大到生产与测试全部依赖并单独验证通过。演示脚本的 mybatis-http、mybatis-browser 与默认 shop-http 均实际成功，创建的环境已销毁、daemon 已正常退出。可核对 [脱敏运行摘要](mybatis-2026-09-12.json)。

```bash
bash scripts/prepare-java.sh
bash scripts/verify-p1.sh
```

脚本只准备 JVM 客户端依赖和 Chromium，不安装数据库。全量 core-race/integration/vet/build 记录在 `artifacts/p0/`，离线 Mapper 和依赖审计记录在 `artifacts/p1/`，每次 MyBatis 回填/回放保留 run ID、Maven/JUnit、浏览器 trace/截图及协议业务轨迹。固定 QA 与输入来源见 `examples/scenarios/mybatis-*.json`。`prepare-mybatis-inputs.py` 仅是自动验收夹具构建器，未将它冒充为 LLM 数据生成。

Codex / Claude Code 继续共用相同 MCP API、HTTP/stdio 配置及现有兼容性回归。本阶段未重跑真实宿主模型；P0 的真实 Codex 记录仍对应当时插件版本。Claude 按用户豁免只要求支持，不需要账号或模型 E2E。

## 明确边界

当前只支持静态 XML SELECT/INSERT/UPDATE，以及原 BIGINT/VARCHAR、受限单表 SQL 和有限 READ COMMITTED。动态 if/foreach/include/selectKey、`${}`、自定义 DTD/实体、CALLABLE、databaseId、自定义语言和行内 typeHandler 会在来源检查中拒绝；全局自定义拦截器/类型处理器不在本阶段验收范围。

MyBatis-Plus BaseMapper/Wrapper 自动 SQL 不是静态 XML 的同义能力，留在下一独立来源策略增量；不声称本阶段已支持 Plus 自动 CRUD、JPA、JOIN、DELETE、DECIMAL、时间类型或完整 MySQL。应用错误不能通过预装购物车终态、自动成功或真实数据库兜底消除。
