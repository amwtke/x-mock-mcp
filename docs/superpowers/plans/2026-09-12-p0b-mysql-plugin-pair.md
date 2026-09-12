# X-Mock-MCP P0B MySQL Plugin Pair Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付独立安装的 mysql-wire 左端与 mysql-mock 右端，提供 QA 输入契约、代码/DDL 场景编译、购物车状态与最小事务，并用真实 Connector/J/HikariCP 验证。

**Architecture:** 左端处理应用协议；右端定义 QA 要求，从 SQL AST/DDL 编译受限计划并执行共享状态。Agent 生成候选业务数据，右端确定性校验；预处理元数据提前固定，运行时只补显式允许的参考实体缺口。核心保持通用策略路由，全程不启动真实数据库。

**Tech Stack:** P0A Plugin API、Go 1.27.1、go-mysql v1.16.0、固定 TiDB parser、JDK 21、Spring Boot 3.5.16 BOM、Connector/J 9.7.0、HikariCP 6.3.3。

---

前置条件：[P0A](2026-09-12-p0a-plugin-foundation.md) 已通过真实进程测试。执行入口：[P0 主计划](2026-09-12-p0-implementation-plan.md)。

业务与产物契约：[MySQL QA/场景设计](../specs/2026-09-12-mysql-qa-scenario-contract.md)。六个任务顺序为类型 → QA/编译 → 右端查询 → 状态/事务 → 左端协议 → 真实驱动。

## 文件职责

| 文件 | 责任 |
| --- | --- |
| `contracts/mysqlv1/types.go`、`schema.json` | 参数、列、结果、场景的 JSON 契约 |
| `contracts/mysqlv1/validate.go`、`validate_test.go` | 类型范围、NULL、列数与结果大小限制 |
| `plugins/right/mysql-mock/qa-input.schema.json`、`candidate.schema.json`、`qa-guide.md` | 随右端发布的 QA/项目资料契约及生成指导 |
| `plugins/right/mysql-mock/prepare.go`、`prepare_test.go` | Prepare 扩展、缺口/歧义报告、源码与 DDL 证据校验 |
| `plugins/right/mysql-mock/ddl.go`、`compile.go`、`compile_test.go` | 离线 DDL 目录、SQL AST 到受限执行计划 |
| `plugins/right/mysql-mock/main.go`、`plugin.go` | 右端 Plugin API 接入与实例状态 |
| `plugins/right/mysql-mock/scenario.go`、`scenario_test.go` | 查询签名、参数匹配、固定结果、NeedsData |
| `plugins/right/mysql-mock/system.go`、`system_test.go` | 固定驱动组合需要的系统变量与连接设置 |
| `plugins/right/mysql-mock/state.go`、`transaction.go`、`state_test.go`、`transaction_test.go` | 共享表状态、写入约束、自增、工作集与提交冲突 |
| `plugins/left/mysql-wire/main.go`、`plugin.go` | 左端生命周期、监听、连接关闭 |
| `plugins/left/mysql-wire/handler.go` | go-mysql Handler 到右端操作的映射 |
| `plugins/left/mysql-wire/resultset.go`、`resultset_test.go` | 按显式类型构造文本/二进制结果 |
| `plugins/left/mysql-wire/connection.go`、`connection_test.go` | 唯一读循环、断连取消、连接与语句状态 |
| `plugins/left/mysql-wire/manifest.json` | 左端角色、MySQL 契约、入口与配置 schema |
| `plugins/right/mysql-mock/manifest.json` | 右端角色、MySQL 契约、入口与场景 schema |
| `examples/scenarios/driver-contract.json` | 独立只读驱动夹具，不能用于购物车业务回放 |
| `examples/scenarios/shop.qa.md`、`shop-input.json`、`shop.json` | QA 用例、带真实证据引用的准备输入、规范化购物场景 |
| `examples/springboot-shop/schema.sql` | 离线 DDL 输入，不在应用启动时执行 |
| `examples/springboot-shop/pom.xml` | 固定 Java 依赖和测试插件 |
| `examples/springboot-shop/src/test/java/local/xmock/DriverContractTest.java`、`TransactionContractTest.java` | JDBC/Hikari 实际驱动与两连接事务契约 |
| `integration/mysql_driver_test.go` | 构建安装插件、启动环境、运行 Maven、清理 |

## Task B1: 锁定 MySQL 操作与类型契约

- [ ] 将主计划固定的 go-mysql 与 parser 版本加入 go.mod，运行 go mod tidy 并提交 go.sum；读取固定 tag 下的 server/command.go、server/stmt.go、stmt/stmt.go，记录 prepared metadata 传递路径。发现 API 与计划不同则先修正本适配层，不能修改核心策略接口来迁就库。
- [ ] 新建 contracts/mysqlv1 类型，使用以下完整数据形状：

```go
package mysqlv1

import "encoding/json"

type Value struct {
    Type string `json:"type"`
    Value json.RawMessage `json:"value"`
}

type Column struct {
    Name string `json:"name"`
    Type string `json:"type"`
    Nullable bool `json:"nullable"`
}

type Query struct {
    Database string `json:"database"`
    SQL string `json:"sql"`
    Params []Value `json:"params"`
    StatementID string `json:"statement_id,omitempty"`
}

type Metadata struct {
    StatementID string `json:"statement_id"`
    Parameters []Column `json:"parameters"`
    Columns []Column `json:"columns"`
}

type Rows struct {
    Kind string `json:"kind"`
    Columns []Column `json:"columns"`
    Rows [][]json.RawMessage `json:"rows"`
    Status SessionStatus `json:"status"`
}

type SessionStatus struct {
    Autocommit bool `json:"autocommit"`
    InTransaction bool `json:"in_transaction"`
}

type OK struct {
    Kind string `json:"kind"`
    AffectedRows string `json:"affected_rows"`
    LastInsertID string `json:"last_insert_id"`
    Status SessionStatus `json:"status"`
    Warnings uint16 `json:"warnings"`
}

type EntityFill struct {
    SlotID string `json:"slot_id"`
    Rows []map[string]Value `json:"rows"`
}

type Error struct {
    Kind string `json:"kind"`
    Number uint16 `json:"number"`
    SQLState string `json:"sql_state"`
    Message string `json:"message"`
}
```

初版业务列类型限定为 BIGINT、VARCHAR；所有 BIGINT 使用有符号 64 位十进制字符串，VARCHAR 使用 JSON 字符串，NULL 使用 JSON null。系统查询也以这两种列类型返回可解析的值。未声明类型直接拒绝，不推测 DECIMAL、时间或二进制的编码。

购物样例金额使用 BIGINT 的分值。真实工程包含 DECIMAL 等类型时，prepare 返回具体能力缺口；不能把真实列自动变成字符串来声称兼容。OK 的两个计数字段使用非负 uint64 范围十进制字符串，编码前校验；Rows 和 OK 均携带右端权威连接状态。MySQL 错误载荷与核心 Failure 分开。

MySQL 契约 ID=`mysql.operation`，Version=1。操作名固定为 connection.open、connection.close、database.use、query、statement.prepare、statement.execute、statement.close、statement.reset。prepare/execute 使用 Query/Metadata；关闭、重置只携带 statement_id，归属校验必须包含 connection_id。连接打开 payload 包含协商后的 found_rows 等语义选项；事务命令通过 query 传右端解析，核心不识别事务 SQL。DML prepare 的 Columns 为空数组。

- [ ] 为 ValidateRows(metadata, rows) 编写负例：BIGINT 超出 int64、错误列名、缺列、多列、非 nullable 的 NULL、字符串冒充数字、结果超过 1000 行或 1 MiB。文本 "9007199254740993" 必须保持精确值。

```go
func TestBigintWireValueValidation(t *testing.T) {
    meta := Metadata{Columns: []Column{{Name: "id", Type: "BIGINT"}}}
    cases := []struct { name, value string; wantError bool }{
        {"beyond-float-precision", `"9007199254740993"`, false},
        {"outside-int64", `"9223372036854775808"`, true},
        {"unsafe-json-number", `9007199254740993`, true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            rows := Rows{
                Kind: "rows", Columns: meta.Columns,
                Rows: [][]json.RawMessage{{json.RawMessage(tc.value)}},
            }
            err := ValidateRows(meta, rows)
            if (err != nil) != tc.wantError {
                t.Fatalf("ValidateRows error=%v, wantError=%v", err, tc.wantError)
            }
        })
    }
}
```

把此测试与元数据比较测试放在 validate_test.go，导入 encoding/json、testing；ValidateRows 的签名为 `ValidateRows(metadata Metadata, rows Rows) error`。类型校验测试不能替代后面的真实驱动测试。

- [ ] 加入 OK 计数溢出、非法负数、事务状态、DML 零列 metadata、EntityFill 类型及 slot 归属的测试。运行 `go test ./contracts/mysqlv1 -count=1`；提交：`feat: define typed mysql query and mutation contracts`。

## Task B2: QA 输入契约与离线场景编译

- [ ] 将设计中的 [QA 模板](../../templates/mysql-qa-scenario.md) 内容写入插件 qa-guide.md，并定义 qa-input.schema.json。必需字段覆盖目标、角色、初态、顺序步骤/预期、生成自由度、写入规则、覆盖范围；技术资料包含显式源码/DDL 引用及摘要。自然语言和归一化字段同时保留，字段来源区分 QA、接入方与 Agent 推导。
- [ ] 新建 prepare_test.go：缺步骤预期、未明确重复加入规则、找不到 DDL、源码摘要变化、代码列与 DDL 不一致、未实现 DECIMAL、候选擅自改变 QA 固定价格均返回 ready=false 及具体路径。运行 `go test ./plugins/right/mysql-mock -run TestPrepare -count=1` 确认失败。
- [ ] 实现 A2 ScenarioPreparer。只在 daemon 注入的 ProjectRoot 内按输入引用读取文件，校验相对路径、符号链接目标、摘要和读取上限；不扫描用户主目录、不访问数据库。无 candidate 时给出资料缺口/分析指南；有 candidate 时执行以下确定性流水线：

```text
validate QA and project input -> verify evidence file digests
-> parse final DDL offline -> validate initial entities and constraints
-> parse source-linked SQL templates -> infer parameters/columns
-> compile supported AST nodes into fixed plans
-> check step/API/SQL bindings and declared parameter domains
-> simulate ordered operations on a disposable state copy
-> compare QA constraints and expected final state
-> return canonical bundle, input digest and compiled digest
```

候选包字段固定为 qa_contract、evidence、step_bindings、api_expectations、database_scenario、verification、replay，含义遵循设计。Agent 给出源码证据和 SQL 候选，插件验证引用与结构；无法静态证明的 API 映射标为待真实 E2E 验证，不能伪报已验证。编译器从 AST 推导动作，不执行 LLM 提供的代码。

- [ ] ddl.go 支持本样例的 CREATE TABLE 离线声明：BIGINT、VARCHAR(n)、nullable、字面量 default、主键、单列自增、复合唯一键、外键。结构校验和初始行校验共用目录。其他 DDL、类型、排序规则行为与无法还原的迁移明确报告 unsupported；在运行时发 CREATE/ALTER 仍拒绝。
- [ ] compile.go 仅编译单表显式列 SELECT、等值 AND 条件、显式 ORDER BY、固定 LIMIT；单行 INSERT VALUES；UPDATE 的列赋值及 BIGINT 加法。参数位置、类型、允许域和操作符保留，算术溢出失败。无 WHERE 的 SELECT 仅接受明确声明的模板；无 WHERE 的 UPDATE、JOIN、子查询、批量写、DELETE、ON DUPLICATE KEY 和锁定读均报告未支持。字符串相等/排序仅覆盖声明支持的比较语义；P0 样例 WHERE/ORDER BY 使用数字键和固定状态枚举。
- [ ] 编写 SQL 改表名、漏用户条件、参数交换、数量表达式变化的负例；声明支持的 AST 和数据状态才可编译。候选把写后读结果预置为静态购物车、对可变表声明 generation slot、或让断言由运行结果推导时拒绝。
- [ ] 创建有真实内容摘要的微型源码/DDL 测试夹具，验证“浏览 → 详情 → 加入 → 查询 → 再加入”的场景预演；只能将预演标记为编译验证。复制 QA 示例为 shop.qa.md，最终 shop-input.json 的代码证据在 C5 样例建立后从实际文件生成，不填写伪造 hash。
- [ ] 运行 `go test -race ./plugins/right/mysql-mock -run 'TestPrepare|TestCompile|TestDDL' -count=1`。提交：`feat: add plugin-owned QA intake and mysql scenario compilation`。

## Task B3: 右端场景与确定性元数据

- [ ] 在 scenario_test.go 先写三组行为：声明的查询与参数返回固定行；同一查询新参数在 replay 失败；改变 WHERE 运算符或表名后，即使 explore 也不能当成原查询自动成功。运行 `go test ./plugins/right/mysql-mock -run TestScenarioMatching -count=1` 验证失败。
- [ ] database_scenario 使用显式 mode：fixture 是独立只读驱动夹具；stateful 是购物业务场景。同一资源不能混用两种执行来源。以下仅为 fixture 的内部结构，外层仍需完整场景包；schema 拒绝未知字段：

```json
{
  "id": "order-query",
  "database": "app",
  "mode": "fixture",
  "queries": [{
    "id": "order-by-id",
    "sql": "SELECT id, status FROM orders WHERE id = ?",
    "parameters": [{"name": "id", "type": "BIGINT", "nullable": false}],
    "columns": [
      {"name": "id", "type": "BIGINT", "nullable": false},
      {"name": "status", "type": "VARCHAR", "nullable": true}
    ],
    "allow_generation": false,
    "cases": [
      {"params": [{"type": "BIGINT", "value": "1001"}], "rows": [["1001", "PAID"]]},
      {"params": [{"type": "BIGINT", "value": "9007199254740993"}], "rows": [["9007199254740993", null]]},
      {"params": [{"type": "BIGINT", "value": "404"}], "rows": []}
    ]
  }]
}
```

stateful 模式包含 database、tables（DDL 目录与初始行）、statements（SQL、参数域与编译计划）、generation_slots、transaction。执行计划由 B2 产生并在加载时重新核对，调用方不能绕过编译器指定语义。fixture 不允许生成；购物查询从状态计算，不能用静态 case 绕过真实写入。

- [ ] 重用 B2 parser/计划进行运行时匹配。保留表名、列名、运算符、排序、limit 等结构；参数类型、值和允许域参与匹配。文本 SQL 字面量仅在对应已声明参数位置转换；不能抹掉 WHERE 条件。参数域可以显式列出 U1/U2、P1 与数量 1，不默认为所有值允许。
- [ ] 实现右端 Execute：连接、数据库选择和 prepare 确定性执行；fixture 命中返回固定行；stateful 查询从共享表及本连接工作集计算投影/过滤/排序结果。未声明 SQL/域外参数失败，只有依赖显式 generation slot 的读取可返回 NeedsData。
- [ ] 定义 generation slot：slot_id、不可变参考表、允许主键集合、列 schema、QA 固定值/约束及种子。一个 slot 最多 100 行；本 P0 场景只有一个缺口 slot。列表与详情查询若涉及该 slot，物化前不得先返回缺数据的半份列表。每 slot 一次生成，多连接等待同一个结果；不同 slot 可并行。
- [ ] Complete 接收 B1 EntityFill；continuation 固定 request/connection、slot、查询、参数、场景/slot 版本及 deadline。校验实体类型、主键、DDL/QA 约束后原子填充共享参考状态，再执行原查询返回 Rows。同一 slot 已被相同候选填充时复用，冲突失败。取消只撤销该请求；没有存活等待者时废止填充。禁止覆盖已有实体或生成购物车/写操作结果。
- [ ] 记录 EntityFill、实体摘要和原始查询为导出证据；运行 Snapshot 保持不变，运行态参考实体允许完成一次物化。填写成功前后列表/详情结果必须一致且满足 QA 约束。
- [ ] 在 system.go 实现驱动启动所需的查询与连接设置，先覆盖 database、version、autocommit=1、character_set_client/connection/results、collation_connection、time_zone/system_time_zone、transaction_isolation、transaction_read_only。值随固定配置返回；未知系统变量或 SET 项必须报能力缺失，不能统一返回空字符串或 OK。

READ COMMITTED、autocommit 与事务状态由 B4 实现。运行时 DDL、DELETE、未登记 DML、其他隔离等级、保存点和锁定读返回 MySQL 1235 / SQLSTATE 42000。replay 存在未物化 slot 或查询未匹配时返回 1105 / HY000，内部记录 UNMATCHED_REQUEST。

- [ ] 对库初始化实际产生的其他系统查询，在 B6 中捕获并逐项判断后补到本文件的显式映射；增加相应驱动回归验证，不能扩大为通配成功。
- [ ] 运行 `go test -race ./contracts/mysqlv1 ./plugins/right/mysql-mock -count=1`；提交：`feat: implement deterministic mysql dependency plugin`。

## Task B4: 共享写入状态与最小事务

- [ ] 先编写 state_test.go：空购物车查询为空；INSERT 返回键 5001/影响一行，后续读取数量 1；UPDATE 加 1 后仍同一行且数量 2；另一用户为空；重复唯一键、无效外键、VARCHAR 过长和 BIGINT 加法溢出均不改变状态。运行 `go test ./plugins/right/mysql-mock -run TestState -count=1` 确认失败。
- [ ] state.go 根据 B2 编译计划执行插入/更新，不接收任意动作脚本。自动提交在实例锁内完成校验、分配键、写入和计算结果；同语句失败不部分写入。分别计算 matched/changed 行数，根据连接协商的 CLIENT_FOUND_ROWS 语义选择 affected_rows；未修改字段的 UPDATE 有独立测试。
- [ ] transaction.go 保存连接 autocommit、in_transaction、工作集和记录基线版本。默认隔离语义 READ COMMITTED；每条查询取最新已提交状态加本连接覆盖。提交在实例锁内检查被写行版本/唯一键冲突，原子应用；冲突返回 1213/40001 并回滚工作集。只读查询不承诺锁定读或可重复读。
- [ ] 实现 SET autocommit=0/1、BEGIN/START TRANSACTION、COMMIT、ROLLBACK。false→true 提交未提交工作集，提交/回滚后 autocommit=false 连接在下一个事务性语句开始新事务；BEGIN 不改变 session autocommit 设置。连接断开丢弃工作集。活动事务内再 BEGIN 明确 unsupported，禁止隐式丢写。系统查询与结果状态来自同一连接状态对象。
- [ ] 自增计数器属于环境；事务插入保留已分配键，回滚可留空洞，新环境重置起点。不会因为两个连接读取相同 next_id 而重复分配。主键结果经 OK 返回，必要的 SELECT LAST_INSERT_ID() 读取连接记录，具体行为以 B6 固定驱动路径验证。
- [ ] 两连接测试用显式同步屏障证明：自身可读未提交写入、其他连接不可见、提交后可见、回滚/断连不泄漏、并发提交冲突不丢更新、连接池复用无遗留事务。增加协议取消发生在状态应用前/后测试；已提交但响应未送达时运行失败且 trace 保留提交事件，不自动重试写操作。
- [ ] 运行 `go test -race ./plugins/right/mysql-mock -run 'TestState|TestTransaction' -count=1`。提交：`feat: execute stateful mysql writes and bounded transactions`。

## Task B5: 左端协议与预处理

- [ ] 对 resultset.go 先写文本与二进制结果测试：BIGINT、大整数、NULL、空行集、VARCHAR、列名和 nullable 标记。运行 `go test ./plugins/left/mysql-wire -run TestResultsetEncoding -count=1`，确认未实现时失败。
- [ ] 左端按 manifest 配置监听，默认端口 3306，测试使用 port=0；只在 ready 返回实际地址后发布 binding。每个 socket 有独立 connection_id、context、statement 映射。固定本地认证采用 mysql_native_password，配置专用测试用户；TLS 在 P0 测试 profile 明确关闭。
- [ ] 将 server.Handler 对应方法分别映射到 B1 的右端操作。COM_PING、本地协议 framing、认证由左端确定性处理；数据库选择、系统查询和业务查询经过右端。仅启动时服务器版本字符串可使用 `8.0.0-xmock`，不得据此声称支持全部 MySQL 8.0 语义。
- [ ] HandleStmtPrepare 取得右端 Metadata 后，在 go-mysql 的 context 中返回 `*stmt.PreparedStmt`，填充 RawParamFields 和 RawColumnFields。固定版本源码支持该路径：[server/command.go](https://github.com/go-mysql-org/go-mysql/blob/v1.16.0/server/command.go)、[stmt/stmt.go](https://github.com/go-mysql-org/go-mysql/blob/v1.16.0/stmt/stmt.go)。

```text
Right statement.prepare -> Metadata
-> each Parameter/Column encoded to mysql.Field packet
-> stmt.PreparedStmt.RawParamFields / RawColumnFields
-> return parameter count, column count, *stmt.PreparedStmt
```

保留右端 statement_id 与左端 prepared context 的关联，执行时传递实际类型化参数。statement.close/reset 只处理所属连接。准备阶段缺元数据必须返回错误，不能填通用 VARCHAR 列占位。

- [ ] 结果编码使用已经声明的列类型与 flags，不能靠第一行值推断类型；第一行 NULL 和完全空结果也必须返回同样的 BIGINT/VARCHAR 元数据。文本模式使用 length-encoded field，NULL 为协议 NULL；binary 模式使用 NULL bitmap、BIGINT 的 little-endian int64 和 VARCHAR 长度编码。
- [ ] DML prepare 返回参数元数据与零列；执行生成真实 OK packet 的 affected_rows、last_insert_id、warnings 和 status_flags。每次响应同步右端 SessionStatus；左端不自己推断事务成功。验证文本和预处理 DML 的编码，以及连接协商的 found_rows 选项传递。[OK packet 定义](https://dev.mysql.com/doc/dev/mysql-server/latest/page_protocol_basic_ok_packet.html)
- [ ] connection.go 使用唯一的底层读循环发现 EOF，并把完整帧交给 go-mysql 连接读取；限制单帧与排队内存，不能让另一个 goroutine 与 go-mysql 同时读取原 socket。对正在等待核心响应的查询，EOF 触发 connection context 取消，并通过 IPC 撤销旧请求。禁止用 Peek 误消费下一条命令。
- [ ] 添加等待右端期间关闭客户端、关闭环境、超时和两个连接同时 prepare/execute 的测试。使用短 deadline 或显式信号控制时序，不依赖长 sleep。右端慢响应不能阻塞另一连接的探活。
- [ ] 运行 `go test -race ./plugins/left/mysql-wire -count=1`；提交：`feat: implement mysql application protocol plugin`。

## Task B6: 真实 Connector/J 与 HikariCP 验收

- [ ] 创建 Maven 项目，parent 为 org.springframework.boot:spring-boot-starter-parent:3.5.16，java.version=21，依赖 spring-boot-starter-jdbc、spring-boot-starter-web、mysql-connector-j(runtime)、spring-boot-starter-test(test)。不配置本地真实 MySQL、H2 替换驱动或自动建表。
- [ ] 创建 DriverContractTest 的核心测试：

```java
package local.xmock;

import com.zaxxer.hikari.HikariConfig;
import com.zaxxer.hikari.HikariDataSource;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.Types;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class DriverContractTest {
    @Test
    void serverPreparePreservesMetadataAndLargeInteger() throws Exception {
        String url = System.getenv("X_MOCK_MYSQL_URL");
        assertNotNull(url, "integration harness must supply X_MOCK_MYSQL_URL");
        HikariConfig config = new HikariConfig();
        config.setJdbcUrl(url);
        config.setUsername("mock");
        config.setPassword("mock-local");
        config.setMaximumPoolSize(2);
        try (HikariDataSource pool = new HikariDataSource(config);
             Connection connection = pool.getConnection();
             PreparedStatement query = connection.prepareStatement(
                 "SELECT id, status FROM orders WHERE id = ?")) {
            assertTrue(connection.isValid(2));
            assertEquals(Types.BIGINT, query.getMetaData().getColumnType(1));
            assertEquals("status", query.getMetaData().getColumnLabel(2));
            query.setLong(1, 9007199254740993L);
            try (ResultSet result = query.executeQuery()) {
                assertTrue(result.next());
                assertEquals(9007199254740993L, result.getLong("id"));
                assertNull(result.getString("status"));
                assertTrue(result.wasNull());
                assertFalse(result.next());
            }
        }
    }
}
```

integration/mysql_driver_test.go 负责构建左右端、独立安装和启用、创建临时环境、设置 X_MOCK_MYSQL_URL 并运行 Maven；URL 包含 `sslMode=DISABLED&useServerPrepStmts=true&emulateUnsupportedPstmts=false&cachePrepStmts=false&socketTimeout=10000`。随后使用 `useServerPrepStmts=false` 验证文本路径，两个模式分别记录。驱动只读夹具和购物状态测试使用不同环境、完整场景包；微型夹具为 QA 的驱动契约类用例，也经过 prepare 校验。

- [ ] 将固定 JDK 21 的 Home 路径放入该子进程的 JAVA_HOME；本机路径可从 Java 可执行文件向上定位到 Contents/Home，不能依赖当前 `/usr/libexec/java_home` 的发现结果。Maven 子进程日志必须打印实际 Java 版本。
- [ ] 补齐外部行为：1001 返回 PAID，404 空集元数据，未知表/改 WHERE 运算符失败；购物 INSERT 的 executeUpdate=1/getGeneratedKeys=5001，UPDATE 后查询数量 2，错误用户查不到；默认与 useAffectedRows=true 分别验证 no-op UPDATE 计数。每个断言都通过 JDBC 检查，不能从右端内部内存读取期望作为成功依据。
- [ ] TransactionContractTest 用两条真实 JDBC 连接执行 B4 的可见性/回滚/冲突矩阵，覆盖 setAutoCommit、commit、rollback、恢复自动提交、关闭后重新借连接；断言 getAutoCommit/getTransactionIsolation 与返回状态一致。不支持的 SAVEPOINT/REPEATABLE READ/DDL 明确 SQLException。
- [ ] 运行 `go test ./integration -run 'TestMySQLDriverContract|TestMySQLTransactionContract' -count=1 -v`，预期 Maven 退出码 0；失败时保存轨迹，修复 B3/B4/B5 后只重跑受影响的驱动组。
- [ ] 运行 `mvn -f examples/springboot-shop/pom.xml dependency:tree`，保存精确 JDBC/Hikari 版本。提交：`test: validate mysql plugins with real JDBC and Hikari`。

P0B 完成意味着 QA/候选准备和 MySQL 已验证的读写/事务子集可用；自然语言到真实购物浏览器流程、Agent 协作与回放由 P0C 完成。
