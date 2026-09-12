# P1.3 MyBatis-Plus Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans task-by-task. 用户要求当前会话执行，不启用子代理。

**Goal:** 真正使用 MyBatis-Plus 自动 CRUD 与受限 Wrapper 的 Spring Boot E2E，全部数据依赖由我们的 MySQL 协议插件提供。

**Architecture:** MySQL 右端新增 Java AST 来源策略，读取已验证摘要的实体、Mapper 与调用源码，确定性构造支持的 SQL 后用现有 parser 校验。受限 DELETE 扩展现有事务状态。左端、核心、MCP 及 Coding Agent 唯一数据生成入口保持不变，不安装数据库或替代 SQL 引擎。

**Tech Stack:** Go 1.27.1；Tree-sitter Go 0.24.0 / Java 0.23.5（CGO/C 编译器）；MyBatis-Plus 3.5.17；既有 Boot 3.5.16 / JDK21 / Connector/J；JUnit、Playwright。

---

复用 `.worktrees/p0`，新分支 `feat/p1-mybatis-plus`，基线 a85214f 已完整验证。运行时以现有 X_MOCK_GO、X_MOCK_JAVA_HOME 和 PLAYWRIGHT_BROWSERS_PATH 指定已安装的工具链。

## Task 1：离线 Java AST 与多文件策略

Files：新增 `plugins/right/mysql-mock/plus_java.go`、`plus_entity.go`、`plus_java_test.go`；修改 `source.go`、`source_mybatis.go`、go.mod/go.sum。

- [x] 先写解析测试，实际 Java 字符串覆盖 Long/String、显式实体映射、AUTO 主键、getter、直接 BaseMapper 继承；同名伪造注解、继承、字段填充、SQL 方法覆盖和损坏源码拒绝。运行 `$X_MOCK_GO test ./plugins/right/mysql-mock -run TestPlusJava -count=1` 观察失败。
- [x] 增加锁定语法依赖；实现每次解析均 Close 的 AST 文档、import/包名解析、显式实体/Mapper提取。sourceStrategy 接收已经校验的文件 map，旧策略取 statement.SourcePath 内容；不自行读任意文件或执行项目源码。

```go
type PlusSource struct {
 EntityPath string `json:"entity_path"`
 MapperPath string `json:"mapper_path"`
 CallMethod string `json:"call_method"`
 Version string `json:"version"`
 BuildPath string `json:"build_path"`
 ConfigPath string `json:"config_path"`
}
type sourceStrategy interface { Validate(Statement, map[string][]byte) error }
// StatementSource 增加 Plus *PlusSource `json:"mybatis_plus,omitempty"`。
```

- [x] 运行新增测试和既有来源测试变绿，提交 `feat: parse MyBatis-Plus source evidence offline`。

## Task 2：BaseMapper / Wrapper SQL 来源

Files：新增 `plus_call.go`、`source_plus.go`、`source_plus_test.go`；修改 `source.go` 注册表。

- [x] 写来源行为测试：源码目录含实体、Mapper、Repository 三份实际 SHA；正例 selectById/selectList/selectOne/insert/updateById/deleteById/delete；负例改表名、改字段、漏 Wrapper 条件、换 Mapper 类型/方法/版本、改参数名顺序，且更新文件 SHA 后仍拒绝不一致候选。
- [x] 实现调用点唯一性、Mapper 字段类型解析和直接 LambdaQueryWrapper 链展开。支持 `eq(Entity::getField,value)`、`orderByAsc/Desc(Entity::getField)`；拒绝 `last/apply/or`、条件重载、变量 Wrapper、自定义 CRUD 与未知字段。根据真实实体信息渲染 SQL、Plus 原生参数名（id、et.property、ew.paramNameValuePairs.MPGENVALn）和类型，完整 canonicalSourceSQL 比较。

```java
return products.selectList(new LambdaQueryWrapper<Product>()
    .eq(Product::getStatus, status).orderByAsc(Product::getId));
// 来源策略应产生明确列 SELECT，status 的参数名为 ew.paramNameValuePairs.MPGENVAL1。
```

- [x] 执行 `$X_MOCK_GO test -race ./plugins/right/mysql-mock -run 'Test(Plus|Source|MyBatis)' -count=1`；提交 `feat: verify generated BaseMapper and Wrapper SQL`。

## Task 3：受限 DELETE 与事务

Files：新增 `plugins/right/mysql-mock/delete_test.go`；修改 compile.go/state.go/transaction.go/source_mybatis.go。

- [x] 写失败测试：有条件删除返回 1，再删 0；读己之删、其他连接提交前可见、回滚/断连恢复、提交删除；并发 UPDATE/DELETE 冲突；父行外键拒绝1451。无 WHERE、JOIN、多表、ORDER/LIMIT 不支持。
- [x] Compile 增加 ast.DeleteStmt 的单表等值 AND 计划。用 `nil Entity` 作为事务/已提交墓碑并保留版本；view 隐藏墓碑，commit 保留冲突检测，操作失败不得改变行。删除父行违反 FK 单独返回1451，其他约束不降级。
- [x] 执行 `$X_MOCK_GO test -race ./plugins/right/mysql-mock -count=1`，提交 `feat: implement bounded transactional mock deletes`。

## Task 4：真实 Plus 应用与固定 QA

Files：新增 `examples/springboot-shop/src/main/java/local/xmock/plus/{PlusProduct,PlusCartItem,ProductMapper,CartMapper,PlusShopRepository,PlusCartController}.java`；新增 application-mybatis-plus.properties；修改 pom.xml、Repository profile、静态 XML profile配置；新增 `PlusMapperContractTest.java`、`PlusShopFlowTest.java`、`PlusShoppingBrowserTest.java`、`PlusDeleteFlowTest.java`。

- [x] 先写真实框架离线测试：MybatisConfiguration.addMapper，取实际 MappedStatement/BoundSql；不创建 DataSource，核对全部购物操作及 DELETE 的 SQL/参数/生成键。先观察缺少实体/Mapper的失败，再实现样例。
- [x] POM 使用 Plus Boot3 starter3.5.17，移除重复原 Starter；原 MyBatis/XML和JdbcTemplate profile继续验收。Plus Repository 通过继承来的 BaseMapper 方法查询、插入、读当前行后 updateById 累计数量；不用手写 SQL 或 Mapper XML 代替自动 CRUD。
- [x] 新增 `scripts/prepare-plus-inputs.py`，使用固定 QA、实体/Mapper/调用的真实来源，生成 plus-* 夹具。原 S1–S6 预期不变；独立 plus-delete-* QA 增加“删除后为空、商品未删”及固定最终断言和 DELETE 预演。
- [x] 执行真实 `PlusMapperContractTest`，再经插件监听执行 Plus HTTP/浏览器与删除 API；只允许 binding 创建的 JDBC URL。提交 `feat: exercise real MyBatis-Plus CRUD through mock MySQL`。

## Task 5：回填、缺陷、版本与交付

Files：新增 integration/mybatis_plus_test.go；扩展 agent_flow/shopping_defects 和版本测试；新增 scripts/verify-plus.sh；更新 QA guide、schemas、manifest、README、进度、compatibility/p1-mybatis-plus.md。

- [x] 复用 MCP 真实客户端辅助函数跑 Plus AgentFill 与三个新环境回放，比较业务轨迹并保留空车初态导出。增加 Plus API/SQL缺陷：错累计、漏写/删、漏用户条件、错误实体列映射；不能只靠旧 SHA 拒绝。
- [x] 右端升级0.3.0；旧0.1.0实际源码升级测试继续与新右端共享左端0.1.0。修改旧版本测试固定“当前”为0.3.0。构建仍独立打包，检查左端包摘要。
- [x] `verify-plus.sh` 复用 verify-p1.sh，增加 Plus离线框架契约和依赖审计。运行准备依赖（只 JVM依赖/Chromium）、全量 core-race/integration/vet/build；更新可直接运行的 Plus HTTP/浏览器/删除演示并实际执行。
- [x] 记录真实版本、能力边界、回放ID和业务轨迹摘要。保留本地工具链与验收证据，不要求 Claude 账号。
- [x] 沿用用户授权，提交、合并并推送 master（实现交付 df5949e，2026-09-13 确认远端一致）。

## 自审

不启动数据库（所有任务）、独立右端策略（1–2）、原生自动 SQL（2/4）、真实删除与事务（3）、QA 不变和缺陷失败（4–5）、仅 Coding Agent 经 MCP生成（5）、两端独立版本与卸载（5）均有对应实现/验收。范围限于显式实体映射、默认框架配置和上述 SQL；Lombok、继承映射、条件 Wrapper、插件扩展和部分实体动态字段分支明确留在未支持列表。
