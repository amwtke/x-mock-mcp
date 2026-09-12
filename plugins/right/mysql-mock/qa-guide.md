# mysql-mock/0.3.0：QA 与 Coding Agent 输入指南

左端对接应用；右端对接外部依赖。此右端拥有 QA 契约、DDL/SQL 编译、实体状态与事务语义。数据仅由当前 Coding Agent 经 MCP 提供，不配置额外模型 API，也不安装、不启动真实数据库、容器数据库或替代 SQL 引擎。

## QA 必须提供的业务资料

可以使用自然语言，无需写 SQL：用例名称与目标、起始页面、用户身份、初始条件、有顺序的操作和每步可观察预期、必须固定的值、允许生成的范围、写入及重复操作规则、覆盖与不覆盖范围。规则不明确时列入 ambiguities，准备阶段会阻止发布；不要猜测。

购物示例：U1/U2 初始空车，首页显示测试键盘 99 元，详情库存 10；首次加 1 件产生稳定记录，重复加 1 件累计到 2，合计 198 元；U2 仍为空，库存不变。购物车必须由应用的 INSERT/UPDATE 产生。

## 接入方提供的技术资料

当前项目的最终 CREATE TABLE DDL、相关前后端源码、Controller/Service/Repository/DTO、测试入口、测试身份与连接配置。sources 每项包含项目内相对路径、kind 和当前文件的 SHA-256。database_scenario.database 必须与实际 JDBC URL 的数据库名称相同。

## Agent 准备流程

1. 通过 mock_capabilities 指定 role=right、plugin_id=mysql-mock、version=0.3.0，分别读取 qa_input、candidate、guide。
2. 保留 QA 原文到 input.qa.natural_language，归一化步骤、fixed、初态/终态断言；引用并摘要实际文件。只传 input 调用 mock_scenario_prepare，检查缺资料/歧义/不支持项。此阶段不监听 MySQL 端口。
3. 分析代码，生成步骤 → API → 来源路径 → SQL 规则的映射。SQL 必须来自所引用源码；参数必须有类型和有限 allowed 域。提供实际 API 预期，不能将这些 JSON 直接回给 JDBC。
4. candidate 保留 input 的 qa_contract/evidence。database_scenario.mode 使用 stateful；只读固定夹具才使用 fixture。不要编造执行 plan/tables，由 prepare 从 SQL AST/DDL 生成。
5. BIGINT 使用带类型的十进制字符串，例如 {"type":"BIGINT","value":"1001"}；VARCHAR 使用 {"type":"VARCHAR","value":"测试键盘"}。NULL 的表示见 schema。禁止用浮点数承载 BIGINT。
6. initial 保持真实初态；next_ids 指定自增种子；data_bindings 将 QA fixed 约束绑定到实体字段。verification 记录 SQL 调用数量和最终状态；initial_assertions/final_assertions 固定 QA 的状态要求。含写入时提供按实际调用顺序执行的 preview，包含事务 BEGIN/COMMIT。
7. AgentFill 的 generation_slots 只用于不可变参考实体。P0 要求 fixed 足以离线预演并满足 QA；运行时 Agent 返回这些约束下的 EntityFill，不能补造购物车或绕过应用写入。StrictReplay 需要物化所需 slot。
8. input/candidate 都是原生 JSON 对象。prepare 返回 ready=true 后，以 compiled_body 保存 document；首次 expected_version=0，后续用当前版本。put 和创建环境会再次检查源码摘要，源码变化需重新准备新版本。

## MyBatis XML 来源策略（0.2.0）

QA 仍只需上述自然语言业务资料。接入方另提供实际 Mapper XML、Mapper Java 接口、Repository/Service 调用、参数/结果对象、启用的 MyBatis 配置和锁定依赖版本（例如 pom.xml），全部列入带 SHA-256 的 sources。Spring Boot 的 JDBC URL 指向左端 mysql-wire 的 MySQL TCP 端口（默认 3306），不是 MCP HTTP 地址。

每条 XML 语句增加明确来源，例如：

```json
{"source_path":"src/main/resources/mappers/ShopMapper.xml","source":{"strategy":"mybatis-xml","namespace":"local.xmock.ShopMapper","statement_id":"item"},"sql":"SELECT id, user_id, product_id, quantity FROM cart_items WHERE user_id = ? AND product_id = ?","parameters":[{"name":"userId","type":"BIGINT","nullable":false,"allowed":[{"type":"BIGINT","value":"2001"}]},{"name":"productId","type":"BIGINT","nullable":false,"allowed":[{"type":"BIGINT","value":"1001"}]}]}
```

这只是 Statement 的来源/SQL/参数片段，完整声明还须包含唯一 id。右端从真实 XML 的 namespace/id 定位静态 SELECT/INSERT/UPDATE，按 `#{property}` 或 `#{property,jdbcType=BIGINT}` 顺序产生 `?`，逐一检查参数名/类型，并比较完整 SQL AST。CDATA 与 XML 注释可用；引用的 SQL 条件、参数顺序或字面量不同都会拒绝。XML 不能省略 source 来走 Java 原文策略。旧 Java 场景可省略 source 或使用 `java-literal`。

本版验收了 constructor resultMap、JDBC 生成键、写后读、提交/回滚及购物自然语言 QA。使用现有 BIGINT/VARCHAR 与 READ COMMITTED 范围。动态 if/foreach/include/selectKey、`${}`、自定义 DTD/实体、CALLABLE、databaseId、自定义语言或行内 typeHandler 未支持；框架全局自定义拦截器/类型处理器不在验收范围。Plus 自动 SQL 使用下述独立策略。未知能力应报出缺口，不能安装真实 MySQL 兜底。

## MyBatis-Plus 来源策略（0.3.0）

QA 提供同样的业务资料；涉及移除时必须说明归属、删除后的查询、重复删除及最终状态，见 `examples/scenarios/plus-delete.qa.md`。Agent 另外读取并摘要实体类、直接继承 BaseMapper 的 Mapper 接口、调用类、实际 POM 和启用的 Plus 配置。使用 `source.strategy=mybatis-plus`，不要伪装成 Java SQL 字符串或 Mapper XML。

```json
{
  "source_path": "src/main/java/example/CartRepository.java",
  "source": {
    "strategy": "mybatis-plus",
    "namespace": "example.CartMapper",
    "statement_id": "selectOne",
    "mybatis_plus": {
      "entity_path": "src/main/java/example/CartItem.java",
      "mapper_path": "src/main/java/example/CartMapper.java",
      "call_method": "item",
      "version": "3.5.17",
      "build_path": "pom.xml",
      "config_path": "src/main/resources/application-mybatis-plus.properties"
    }
  }
}
```

这是来源片段，SQL 和参数仍须由 Agent 按实际代码生成。右端使用离线 Java AST 解析已校验 SHA 的文件，不执行项目 Java。核对实体映射、真实 BaseMapper 类型、唯一方法调用和直接 `new LambdaQueryWrapper<Entity>()` 链；渲染支持的 SQL 后比较完整 SQL AST，并检查参数名、顺序、类型。`statement_id` 是实际调用的 BaseMapper API，例如 selectOne；它在真实框架内部使用 selectList 的 MappedStatement。

本版限定 Plus Boot3 starter **3.5.17**（POM 直接写字面版本，不使用 profiles/dependencyManagement）及 properties 中的三项配置：`mybatis-plus.configuration.map-underscore-to-camel-case=true`、`mybatis-plus.configuration.cache-enabled=false`、`mybatis-plus.configuration.local-cache-scope=STATEMENT`。同命名空间 XML 覆盖、额外 MyBatis/Plus 依赖、未知 Plus 配置均拒绝。自定义全局注入器、拦截器、类型处理器及外部配置覆盖不在支持范围；当前静态证据校验不能证明任意 Spring 运行时配置，实际 SQL 不匹配仍会失败。

实体是显式 `@TableName`、`@TableId(AUTO/INPUT)`、`@TableField` 的普通 Long/String POJO，getter 直接返回对应字段；Mapper 是空接口，直接继承 BaseMapper<Entity>。支持 selectById/selectList/selectOne、insert、updateById、deleteById 和有条件 delete；Wrapper 仅直接 eq 和单字段 orderByAsc/Desc。Lombok、继承映射、逻辑删除、字段填充、自定义类型、局部 Wrapper 变量、条件重载、原始 SQL、or 和动态部分字段写入均未支持。

参数必须使用真实框架名称：selectById 的 `id`、insert 的实体属性名、updateById 的 `et.property`、Wrapper 的 `ew.paramNameValuePairs.MPGENVALn`。字段与顺序来自实际映射；所有参数非 NULL 且有 QA 约束下的有限 allowed 域。insert 省略 AUTO 主键，其余字段非空；updateById 覆盖全部非主键字段。真实动态 SQL 产生其他字段组合时拒绝，不能当成完整 Plus 兼容。

受限 DELETE 必须是单表、有 WHERE 的等值 AND；不支持多表/JOIN、ORDER/LIMIT、逻辑删除或级联。返回实际影响行数，删除己写可见、提交后其他连接可见，回滚/断连恢复；外键父行删除返回1451，并发写冲突返回1213。删除只是应用请求引起的状态变化，Agent 不得回填购物车终态。

## 运行与回放

创建环境时 data_strategy 与 right_config.mode 同为 AgentFill 或 StrictReplay。后台 mock_run_start 返回 run_id 后，立即循环 mock_requests_next；不要先等待整个 E2E 结束。首次保存 owner_token，此后每次复用。租约只有 15 秒，应先根据 QA/DDL 做好准备，再领取并回填；业务请求截止时间不会因重领延长。

mock_requests_resolve 传 request_id、owner_token、lease_token 和符合 need.schema 的 candidate，等待右端实际校验/执行完成后才确认 resolved。不能把“已接收候选”当成“JDBC 成功”。HTTP/UI 预期由真实测试执行，右端只验证 SQL/状态。

成功后显式选择已确认 request_id 导出。导出只物化参考实体，保留空购物车和原自增种子。保存新版本，在新环境用 StrictReplay 运行相同测试；最后销毁环境，两端分别停用、卸载。

## 当前支持边界

仅 BIGINT/VARCHAR、单表显式列 SELECT（等值 AND、ORDER BY、常量 LIMIT）、单行 INSERT、受限 UPDATE/DELETE、单列主键/组合唯一键/外键和有限 READ COMMITTED。DDL 只作离线结构输入，表选项（含 ENGINE）未实现。JOIN、聚合、DECIMAL、时间类型、运行时 DDL、SAVEPOINT、其他隔离等级与完整 InnoDB 锁行为会明确拒绝。发现能力缺口应扩展插件和测试，不能改 QA 来迁就 mock。
