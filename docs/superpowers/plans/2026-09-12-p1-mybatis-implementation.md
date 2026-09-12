# P1.1 MyBatis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. 用户已选择当前会话执行，不启用子代理。

**Goal:** 真实 MyBatis Spring Boot 购物 E2E 仅连接我们的 MySQL 协议插件，通过 MCP 回填与干净回放。

**Architecture:** 左端对接应用，右端对接外部依赖。右端以源码策略注册表验证 Java 原文 SQL 与静态 Mapper XML，再进入现有离线 AST 编译和内存状态；左端编码 JDBC 请求的 MySQL 应答。保持 16 个 MCP 工具，只有 Coding Agent 提交候选与参考数据，绝不安装或启动数据库。

**Tech Stack:** Go 1.27.1、encoding/xml、现有 TiDB parser（仅解析）、MyBatis Starter 3.0.5 / MyBatis 3.5.19、Spring Boot 3.5.16、JDK 21、Connector/J 9.7.0、JUnit / Playwright。

---

工作树复用 `.worktrees/p0`，新分支 `feat/p1-mybatis`；基线 68ba0aa 刚在 master 完整验证，复用这份基线记录。所有以下命令在该工作树运行：

```bash
export X_MOCK_GO="$PWD/.tools/go/1.27.1/go/bin/go"
export X_MOCK_JAVA_HOME=/opt/homebrew/Cellar/openjdk@21/21.0.12/libexec/openjdk.jdk/Contents/Home
export PLAYWRIGHT_BROWSERS_PATH="$PWD/.tools/playwright-browsers"
```

## 文件职责

- `plugins/right/mysql-mock/source.go`：来源描述、策略接口/注册表、旧 Java 校验和完整 SQL 规范化。
- `plugins/right/mysql-mock/source_mybatis.go`：静态 XML 解析、语句定位、占位符到参数顺序校验。
- `plugins/right/mysql-mock/source_test.go` / `source_mybatis_test.go`：来源可信性、边界与不支持诊断。
- `scenario_types.go` / `prepare.go`：可选来源字段与策略调用；不修改通用核心。
- `examples/springboot-shop/src/main/java/local/xmock/ShopMapper.java` / `MyBatisShopRepository.java` / `CartInsert.java`：真实 Mapper、数据访问适配及 JDBC 生成键对象。
- `examples/springboot-shop/src/main/resources/mappers/ShopMapper.xml`：购物六条静态 SQL 与结果映射。
- `scripts/prepare-mybatis-inputs.py`：自动验收夹具与真实源码摘要；不是运行时生成器。
- `integration/mybatis_test.go` / 现有 AgentFlow 辅助函数：真实框架调用、SQL/参数缺陷、MCP 回填/三次回放。

## Task 1：源码策略入口

- [x] 写 prepare 行为测试：原 Java 场景仍 ready；显式未知策略拒绝；XML 即使注释含候选 SQL 也不能走默认 Java 原文检查。测试使用 `preparationFixture(t)`，通过原生 JSON 注入新字段，预期缺少策略时失败。

```go
candidate["database_scenario"].(map[string]any)["statements"].([]any)[0].(map[string]any)["source"] = map[string]any{"strategy":"mybatis-xml", "namespace":"local.xmock.ShopMapper", "statement_id":"product"}
```

- [x] 运行 `$X_MOCK_GO test ./plugins/right/mysql-mock -run 'Test.*Source' -count=1`，确认新增正例因未识别 `source` 失败。
- [x] 增加可选来源类型和校验接口；注册表只在右端进程内部选择策略，旧默认仅接受 `.java` 文件。读取对象仍来自 prepare 已验证 SHA 的 `files`。

```go
type StatementSource struct {
    Strategy string `json:"strategy"`
    Namespace string `json:"namespace,omitempty"`
    StatementID string `json:"statement_id,omitempty"`
}
type sourceStrategy interface { Validate(Statement, []byte) error }
// Statement 增加 Source *StatementSource `json:"source,omitempty"`。
// prepare 的 literal Contains 检查替换为 validateStatementSource(statement, files)。
```

- [x] 同一命令变绿并运行全部右端测试；提交 `feat: validate SQL evidence through right-side strategies`。

## Task 2：静态 MyBatis XML

- [x] 写行为矩阵：`#{id}` / `#{id,jdbcType=BIGINT}` 接受；CDATA、XML 注释、显式 resultMap 不影响静态 SQL；更新真实文件并刷新 SHA 后的漏条件、参数顺序交换、错误 namespace/id、重复 id、伪造 SQL 拒绝。
- [x] 拒绝动态 `if/foreach/include/selectKey`、`${}`、外部实体/自定义 DTD、CALLABLE、databaseId、自定义 language/typeHandler；断言报告定位 XML 文件和 statement id。验证字面量中双空格不能被归一化为单空格。
- [x] 运行 `$X_MOCK_GO test ./plugins/right/mysql-mock -run 'Test.*MyBatis' -count=1` 观察预期失败。
- [x] 用 `encoding/xml.Decoder` 读取唯一 mapper 根，定位直接子语句；选中语句只允许文本/CDATA。标准 MyBatis DTD 只作声明，禁止内部子集，不访问网络。参数解析要求属性名与 `Statement.Parameters[i].Name` 一致，jdbcType 与当前类型一致；拒绝转义占位符和未知选项。两份完整 SQL 使用 parser AST Restore 比较，不使用忽略字面量的字符串空白归一化。

```go
// #{userId,jdbcType=BIGINT} -> SQL "?", mapping "userId" / BIGINT。
// 检查参数个数、名称顺序和 jdbcType；然后比较 canonicalSQL(rendered) 与 canonicalSQL(statement.SQL)。
// 任何错误由 prepare 归入 SOURCE_EVIDENCE 诊断，编译失败继续归入 UNSUPPORTED_SQL。
```

- [x] 运行上述测试和 `$X_MOCK_GO test -race ./plugins/right/mysql-mock`，提交 `feat: ground MyBatis statements in static mapper XML`。

## Task 3：真实购物 Mapper 与夹具

- [ ] 新增离线 `MapperContractTest`：使用真实 MyBatis XMLMapperBuilder 和 `MappedStatement.getBoundSql`，无需 DataSource；核对六条 SQL、参数 property 顺序、生成键与结果映射。运行 `mvn -B -ntp -f examples/springboot-shop/pom.xml -Dtest=MapperContractTest test` 观察 Mapper 资源缺失失败。
- [ ] POM 增加锁定的 MyBatis starter；原 Repository 增加 `!mybatis` profile。新增 `ShopDataAccess` 接口供原 Repository 和 `mybatis` Repository 实现，重用现有 Product/CartItem 公共类型，六个操作全部经注入的 `@Mapper` 调用，INSERT 校验 changed=1 和回填生成键。

```java
@Mapper
public interface ShopMapper {
  List<ShopRepository.Product> products(@Param("status") String status);
  ShopRepository.Product product(@Param("id") long id);
  ShopRepository.CartItem item(@Param("userId") long userId, @Param("productId") long productId);
  List<ShopRepository.CartItem> cart(@Param("userId") long userId);
  int insert(CartInsert row);
  int increment(@Param("quantity") long quantity, @Param("id") long id, @Param("userId") long userId);
}
```

- [ ] XML 复用原六条 SQL，用 `#{}` 明确映射；Product/CartItem record 采用 constructor resultMap；`insert useGeneratedKeys="true" keyProperty="id"`。新增 profile 配置 mapper-locations、`local-cache-scope=STATEMENT`，保证事务内写后读真实查询。
- [ ] 夹具脚本读取现有固定 QA、真实 XML 和源码，输出 `mybatis-input.json` / `mybatis-candidate.json` / `mybatis-explore-candidate.json`；初态空车、固定价格和 QA 原文保持一致。
- [ ] 运行离线 Mapper 测试与新 prepare 夹具测试，提交 `feat: exercise shopping through real MyBatis mappers`。

## Task 4：协议和 MCP 验收

- [ ] 增加 `TestMyBatisShoppingHTTPAndBrowser`：启动真实插件对，JDBC URL 只来自 binding 的实际监听地址，分别新环境执行 `ShopFlowTest` 和 `ShoppingBrowserTest`，启用 `mock,mybatis`。
- [ ] 将原 `TestAgentFlowAndReplay` 提取同文件参数化辅助函数，默认 JdbcTemplate 行为不变；新增 MyBatis 调用，读取 MyBatis 场景和 profile，保留实际 MCP 16 工具、next/resolve、换主、初态导出和三次回放业务轨迹比较。
- [ ] 正例命令：`$X_MOCK_GO test ./integration -run 'TestMyBatis(Shopping|Agent)' -v -count=1 -timeout=8m`。缺功能时保留红灯输出，修复真实映射或协议缺口后必须通过。
- [ ] 增加两类真实缺陷：刷新 Mapper SHA 仍保持原候选时漏用户条件被 prepare 拒绝；交换 Java Mapper 参数调用但不改 QA 时真实 HTTP 失败，不能因环境错误误判成功。参考现有 shopping_defects_test 的隔离源码和 AssertionFailedError 检查。
- [ ] 记录 artifacts/p1 的 JUnit、浏览器和协议业务轨迹，提交 `test: verify MyBatis over mock MySQL with agent fill and replay`。

## Task 5：版本和交付

- [ ] 右端版本升级 0.2.0；左端保持 0.1.0、契约 v1。将集成测试的右端版本取自实际包 descriptor，演示脚本从当前构建 descriptor 选择版本，避免取 dist 的旧包。
- [ ] 通过现有 catalog/runtime 在同项目装旧 0.1.0 和新 0.2.0 右端，分别绑定同一左端；旧场景仍可运行，卸载一个版本保留另一个。旧包使用 68ba0aa 真实源码构建，不能用改版本号的假旧实现代替。
- [ ] 更新右端 QA guide：明确 QA 必填项、XML/Mapper/配置依赖版本资料、source 对象示例、动态/Plus 自动 SQL 未支持诊断、无数据库约束。执行 `bash scripts/build.sh` 刷新 schemas/manifest。
- [ ] 新增 `scripts/verify-p1.sh`：先既有 `verify-p0.sh` 全量回归（含新 integration），然后执行 MapperContractTest 和无数据库依赖检查；脚本只能安装 JVM 客户端依赖/Chromium，绝无数据库安装命令或服务。
- [ ] 完成 README、compatibility/p1-mybatis.md、implementation-progress.md，列真实版本和实际范围；提交 `docs: ship MyBatis support without a database service`。

## 自审与后续边界

准备资料、源码策略、协议流、真实写入、回填/回放、版本并存、两端卸载分别由 Task 1–5 覆盖。不会在此计划顺手实现 SQL 引擎、JPA 或宽泛 DDL。MyBatis-Plus BaseMapper/Wrapper 自动 SQL 需要独立来源策略和真实框架用例，下一增量继续执行用户已选方向，不能将本次 XML 验收作为 Plus 完成证据。
