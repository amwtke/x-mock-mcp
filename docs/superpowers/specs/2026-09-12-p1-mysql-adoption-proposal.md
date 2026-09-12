# P1：MySQL 真实工程接入提案

状态：方向已获用户确认。P0 已合并并推送 origin/master（68ba0aa）。用户选择 MyBatis / MyBatis-Plus，并同意逐项推进，再次明确绝不安装 MySQL。未提供独立目标工程，先用仓库内样例验证。P1.1 以独立计划交付；MyBatis-Plus 自动 CRUD 的证据策略和验收作为下一独立增量，不能以 XML 通过替代。

## 固定约束

- 左端对接应用，右端对接外部依赖；两端仍独立安装、启用、停用、卸载。
- SQL 来源校验、QA 契约和数据库语义留在 MySQL 右端策略，通用核心不识别 ORM 或数据库名称。
- 不安装、不启动真实 MySQL、容器数据库或替代 SQL 引擎，不向真实数据库转发兜底。只有 Coding Agent 经 MCP 生成场景和参考数据，不配置额外模型 API。
- Spring Boot JDBC 连接左端提供的 MySQL TCP 端口；QA 自然语言 → Coding Agent 分析源码/DDL → MCP 场景/回填 → 右端校验和状态 → 左端 MySQL 协议应答。这是后续阶段必须保留的核心链路。
- QA 预期固定；应用负责实际写入。每次回放从初态开始，不能把业务终态预装进去。
- Codex / Claude Code 保持相同 MCP 接口。Claude 的真实模型 E2E 继续遵循用户豁免，不要求账号。

## 为什么先做这一步

P0 已证明真实浏览器 → Spring Boot → JDBC → MySQL 协议 → 状态/事务 → AgentFill → 回放的闭环，但样例使用 JdbcTemplate。当前右端在 prepare.go 中用空白归一化后的源码包含检查确认 SQL 来源。

MyBatis Mapper 中的 `#{property}` 会映射成 JDBC PreparedStatement 的 `?` 参数；包含动态节点的 Mapper 还会生成不同 SQL 分支，因此直接使用当前检查会拒绝正常映射。下一步必须验证源码到执行 SQL 的转换依据，不能直接取消源码检查。[MyBatis Mapper 文档](https://mybatis.org/mybatis-3/sqlmap-xml.html)、[动态 SQL 文档](https://mybatis.org/mybatis-3/dynamic-sql.html)。

## 可选推进方式

| 方式 | 收益 | 代价与范围 |
| --- | --- | --- |
| 先适配有明确 SQL 的 MyBatis Mapper（建议） | 复用 MySQL 协议与状态引擎，增加常见工程入口；保留源码可追溯性 | 先约束 Mapper 语法，复杂动态 SQL 分批扩展 |
| 先适配 JPA/Hibernate 或 MyBatis-Plus 自动生成 CRUD | 直接覆盖对应框架的工程 | SQL 不一定以最终形式存在于源码，需要另行设计 ORM 映射证据；范围更大 |
| 先做 ClickHouse/Kafka | 增加依赖种类，验证更多插件对 | 同时增加协议与状态语义；MySQL 的当前接入缺口仍存在 |

延续总体设计中的 P1 → P2 ClickHouse HTTP → P3 Kafka 顺序。若用户的实际工程依赖优先级不同，再据工程证据调整。

## 建议的 P1.1：MyBatis Mapper 接入

目标：原购物 QA 的首页、详情、两次加入、购物车和多用户隔离，在使用 MyBatis Mapper 的真实 Spring Boot 应用中通过；继续支持 AgentFill 和干净初态回放。

### 组件与职责

MySQL 右端增加源码校验策略接口，由策略注册表选择实现：现有 Java SQL 来源校验，以及 MyBatis XML 来源校验。核心 binding、队列与 MCP 工具继续转发通用信封。左端继续接收 JDBC/MySQL 报文，不解析 Mapper。

候选必须引用源码文件摘要、Mapper namespace/statement ID 和参数映射依据。右端读取实际 XML，确定性生成可验证 SQL 与参数顺序，再交现有 SQL AST 编译器处理。Agent 负责分析业务、选择声明过的语句和数据域；它不能声明一个源码中不存在的 SQL 分支。

第一批只包含静态 XML SELECT/INSERT/UPDATE、明确的 `#{}` 参数映射，以及样例所需的结果映射和生成键路径。动态节点、`${}` 字符串替换、自动生成 CRUD 等不按静态 SQL 假装兼容，准备报告应定位不支持项。后续动态 SQL 必须单独定义分支输入与有限展开规则。

先复用 P0 的 BIGINT/VARCHAR 和有限 READ COMMITTED；数据类型、SQL 新语法分别作为后续增量，避免把 Mapper 来源问题与线协议扩展混成一次改动。

### 数据流与失败行为

QA/实际源码/DDL → Agent 候选 → 右端源码策略校验 → SQL AST 编译和状态预演 → 版本化场景 → 真实 MyBatis/JDBC 测试 → Agent 缺口回填 → 导出与回放。

Mapper 文件变化、statement ID 不存在、参数顺序或 SQL 条件不匹配、未支持动态节点均在 prepare 阶段给出明确报告。运行时超出已登记 SQL/参数域仍失败。新增右端版本独立安装，旧版本和旧场景保留；若线契约不变，应验证与现有左端版本组合工作。

### 验收

1. 真正使用 MyBatis 的 Mapper 绑定、结果映射与生成键，不绕到 JdbcTemplate 代替。
2. 同一购物 QA 通过真实 HTTP 和 Chromium，仍由实际 INSERT/UPDATE 把购物车数量从 1 改为 2。
3. 变更 Mapper 条件或交换参数后失败；刷新源码摘要也不能绕过语义校验。
4. AgentFill 成功补全后，只导出参考实体；三个新环境 StrictReplay 的业务结果一致。
5. 同一核心和左端可分别绑定旧/新右端版本；两端独立卸载，已有 P0 回归保持通过。

## 后续增量

P1.2 按目标工程的实际 DDL/SQL 清单补能力，例如常见表选项、INT、DECIMAL、时间类型、DELETE、分页/IN。每项同时考虑 DDL、参数、行编码、状态约束和回归，不能只在 schema 中增加类型名。

P1.3 优先补 MyBatis-Plus 的 BaseMapper / Wrapper 自动 SQL 来源证据，再扩展有界动态 Mapper，增加用例驱动的错误注入。MyBatis-Plus 自动 CRUD 的通过标准单独记录，不能以普通 MyBatis XML 通过代替；JPA 留待后续选定工程。

P2 新增 ClickHouse HTTP 左右端插件对；P3 新增 Kafka 左右端插件对，分别验证实际驱动行为。Kafka 需要生产、分区日志、offset 与消费组状态，继续由应用完成本应发送的消息。

P1.1 锁定 MyBatis Spring Boot Starter 3.0.5、MyBatis 3.5.19、MyBatis-Spring 3.0.5，与既有 Spring Boot 3.5.16 / JDK 21 配合。Starter 官方矩阵将 3.0 系列对应 Boot 3.2–3.5；具体传递版本以 Maven Central 3.0.5 父 POM 和本地 dependency:tree 复核。[官方矩阵](https://mybatis.org/spring-boot-starter/mybatis-spring-boot-autoconfigure/)、[发布 POM](https://repo.maven.apache.org/maven2/org/mybatis/spring/boot/mybatis-spring-boot/3.0.5/mybatis-spring-boot-3.0.5.pom)。

在现有购物样例增加互斥的 `mybatis` profile 和 Mapper Repository，复用原 Controller/Service/浏览器断言；默认 JdbcTemplate 路径保留。右端升级 0.2.0，左端保持 0.1.0、线契约保持 mysql.operation/v1。`statement.source` 可选字段包含 `strategy`、`namespace`、`statement_id`；缺省只兼容 Java 原文 SQL，XML 不可降级绕过。静态 XML 参数支持属性名和 BIGINT/VARCHAR 的 jdbcType，SQL 以完整 AST 规范化结果比较，保持字符串字面量内容。XML 外部实体不解析；动态节点、文本替换、CALLABLE、自定义语言/类型处理器明确拒绝。

实施计划见 [P1.1](../plans/2026-09-12-p1-mybatis-implementation.md)。验收完成前不声称已支持对应框架。
