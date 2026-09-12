# P1.3 MyBatis-Plus 接入与验收

QA 自然语言经 Coding Agent 分析实际代码与 DDL，由 MCP 提交场景和参考实体。Spring Boot 的真实 MyBatis-Plus/JDBC 请求只访问左端的 MySQL TCP 监听；右端策略校验 SQL 来源并维护读写、删除和事务状态，左端编码协议应答。不安装、不启动数据库、容器数据库或替代 SQL 引擎，不代理真实数据库，不配置额外模型 API。

## 固定版本

| 组件 | 版本 |
| --- | --- |
| 左端 mysql-wire / 右端 mysql-mock | 0.1.0 / 0.3.0 |
| 线契约 / MCP 工具 | mysql.operation/v1 / 16 个 |
| MyBatis-Plus Boot3 starter | 3.5.17 |
| MyBatis / MyBatis-Spring | 3.5.19 / 3.0.5 |
| Spring Boot / JDK | 3.5.16 / 21.0.12 |
| Connector/J / HikariCP | 9.7.0 / 6.3.3 |
| Go / Tree-sitter Go / Java grammar | 1.27.1 / 0.24.0 / 0.23.5 |
| Playwright / Chromium | 1.62.0 / 151 |
| 验收平台 / C 编译器 | macOS arm64 / Apple clang 21.0.0 |

样例以 Plus Boot3 starter 替换原 MyBatis starter，避免重复自动配置，保留 JdbcTemplate、静态 XML 和 Plus 三种应用 profile。依赖版本由实际 Maven 依赖树核对。[Plus 安装说明](https://baomidou.com/en/getting-started/install/)、[3.5.17 发布 POM](https://repo.maven.apache.org/maven2/com/baomidou/mybatis-plus-spring-boot3-starter/3.5.17/mybatis-plus-spring-boot3-starter-3.5.17.pom)。

## 校验与实际行为

| 验证 | 内容 |
| --- | --- |
| 独立来源策略 | 右端注册 mybatis-plus；读取已验证 SHA 的实体、Mapper、调用类、POM 和 properties；核心及左端不依赖 ORM |
| Java AST | 实际注解/类型、直接 BaseMapper 继承、简单 getter、唯一调用、Wrapper 字段和顺序；拒绝同名伪造、继承/覆盖、动态/原始 SQL |
| 真实框架 | MybatisConfiguration 注入原生方法，BoundSql 核对 SQL 和参数、Jdbc3KeyGenerator、自增 ID；不创建 DataSource |
| HTTP / Chromium | 真实 PlusShopRepository；原 S1–S6 预期共用，INSERT 生成5001，重复 updateById 数量1→2，合计99→198元，U2空车 |
| 删除 API | 独立 QA：越权404、归属用户204、再查询空车、重复404；商品与库存保留 |
| 删除事务 | 己删不可见、提交前隔离、提交/回滚/断连、UPDATE/DELETE 及删除后重插冲突；父行外键拒绝1451 |
| MCP 回填 / 回放 | MCP 测试客户端经 next/resolve 回填受 QA 约束的参考实体，双客户端换主；3个新环境 StrictReplay 比较完整有序业务轨迹；导出保留空购物车 |
| 8类缺陷 | 漏 INSERT、错累计、强制回滚、漏用户条件、参数交换、错误实体列、漏 DELETE、漏删除归属条件；刷新实际 SHA 且保留 QA，来源或真实 HTTP 断言拒绝 |
| 独立版本 | 实际历史源码68ba0aa构建右端0.1.0；与0.3.0共用左端0.1.0，独立安装、绑定与卸载 |

Plus 自动 CRUD 使用真实实体映射和默认非空字段策略；`selectOne` 调用内部委托 `selectList`，不是虚构的同名 MappedStatement。Wrapper 的原生参数名是 `ew.paramNameValuePairs.MPGENVALn`；updateById 使用 `et.property`。[BaseMapper API](https://baomidou.com/en/guides/data-interface/)、[注解行为](https://baomidou.com/en/reference/annotation/)。

## 执行与证据

```bash
bash scripts/prepare-java.sh
bash scripts/verify-plus.sh
```

2026-09-12 实际执行 `verify-plus.sh` 全部通过：完整 P0/P1 core-race、integration、vet、独立构建、MyBatis/Plus 离线框架契约及生产/测试 Go 与 Java 依赖审计。完整 integration 用时194秒。最终右端0.3.0的 AgentFill 和3次新环境回放各产生18条有序业务操作，摘要一致；8类缺陷全部被拒绝。脱敏运行ID、包摘要与结果见 [验收摘要](mybatis-plus-2026-09-12.json)。

实际执行 plus-http、plus-browser、plus-delete 三个CLI演示，均成功；所有创建的环境已销毁，所属daemon正常退出。左端0.1.0安装包SHA-256与P0/P1.1完全相同。Plus 原始 Maven、JUnit、浏览器 trace/截图、协议与业务轨迹保存在 `artifacts/p1-plus/`，演示证据在其 `demo-runtime/`；全量日志沿用 `artifacts/p0/`。这些检查不需要模型账号。

`prepare-plus-inputs.py` 只构建自动验收夹具，SQL/原生参数名读取真实框架测试生成的 `target/plus-statements.json`，由右端独立 AST 校验。重新生成时先运行离线 `PlusMapperContractTest`，再依次运行 prepare-shop-inputs.py、prepare-mybatis-inputs.py、prepare-plus-inputs.py。运行时仍由连接的 Coding Agent 分析 QA/源码/DDL，经 MCP 提供候选与参考实体。

Codex / Claude Code 的 HTTP/stdio 和16工具兼容回归沿用同一协议。本阶段的自动 MCP 客户端验收不冒充原生模型验收；P0 的真实 Codex 记录仅对应当时版本。用户已免除 Claude 账号和真实模型 E2E，当前交付继续遵守。

## 明确边界

支持显式 TableName/TableId(AUTO/INPUT)/TableField 的普通 Long/String 实体、直接 BaseMapper 空接口和调用处直接构造的 LambdaQueryWrapper。方法限 selectById/selectList/selectOne、insert、updateById、deleteById、有条件 delete；Wrapper 限 eq、单字段 orderByAsc/Desc。insert 的 AUTO 主键不赋值，其余字段非空；updateById 全部非主键字段非空。条件重载、局部变量 Wrapper、or、raw SQL、逻辑删除、Lombok、继承映射、填充和动态部分字段分支未支持。

来源验证限定实际 POM 直接声明 Plus 3.5.17，指定 properties 显式开启下划线转驼峰、关闭缓存、使用 STATEMENT 本地缓存范围。同命名空间 XML 覆盖、POM profiles/dependencyManagement、额外 MyBatis/Plus 依赖和未知 Plus 配置拒绝。静态证据校验不能证明任意 Spring 外部配置或全局自定义注入器/拦截器/类型处理器；这些扩展不在支持和验收范围，实际产生未匹配 SQL 时失败。

DELETE 只支持单表、有条件的等值 AND，不支持 ORDER/LIMIT、多表、级联或逻辑删除。底层仍仅 BIGINT/VARCHAR、受限单表 SQL 和有限 READ COMMITTED；JOIN、聚合、DECIMAL、时间类型、DDL执行、TLS、完整 InnoDB 锁和其他数据库留待独立增量。Tree-sitter 仅离线语法解析；构建右端需要 C 编译器，安装编译好的插件包不需要 Java 分析器或数据库服务。
