# X-Mock-MCP P0B MySQL Plugin Pair Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付独立安装的 mysql-wire 左端与 mysql-mock 右端，使用真实 Connector/J 和 HikariCP 验证读取、预处理、列元数据与错误行为。

**Architecture:** 左端解码应用请求并编码响应，右端处理 MySQL 场景、系统元数据与读取结果；核心只路由通用信封。预处理的参数与列元数据在 prepare 阶段从右端取得，运行时缺少行数据才返回 NeedsData。

**Tech Stack:** P0A Plugin API、Go 1.27.1、go-mysql v1.16.0、固定 TiDB parser、JDK 21、Spring Boot 3.5.16 BOM、Connector/J 9.7.0、HikariCP 6.3.3。

---

前置条件：[P0A](2026-09-12-p0a-plugin-foundation.md) 已通过真实进程测试。执行入口：[P0 主计划](2026-09-12-p0-implementation-plan.md)。

## 文件职责

| 文件 | 责任 |
| --- | --- |
| `contracts/mysqlv1/types.go`、`schema.json` | 参数、列、结果、场景的 JSON 契约 |
| `contracts/mysqlv1/validate.go`、`validate_test.go` | 类型范围、NULL、列数与结果大小限制 |
| `plugins/right/mysql-mock/main.go`、`plugin.go` | 右端 Plugin API 接入与实例状态 |
| `plugins/right/mysql-mock/scenario.go`、`scenario_test.go` | 查询签名、参数匹配、固定结果、NeedsData |
| `plugins/right/mysql-mock/system.go`、`system_test.go` | 固定驱动组合需要的系统变量与连接设置 |
| `plugins/left/mysql-wire/main.go`、`plugin.go` | 左端生命周期、监听、连接关闭 |
| `plugins/left/mysql-wire/handler.go` | go-mysql Handler 到右端操作的映射 |
| `plugins/left/mysql-wire/resultset.go`、`resultset_test.go` | 按显式类型构造文本/二进制结果 |
| `plugins/left/mysql-wire/connection.go`、`connection_test.go` | 唯一读循环、断连取消、连接与语句状态 |
| `plugins/left/mysql-wire/manifest.json` | 左端角色、MySQL 契约、入口与配置 schema |
| `plugins/right/mysql-mock/manifest.json` | 右端角色、MySQL 契约、入口与场景 schema |
| `examples/scenarios/order-query.json` | 固定查询与参数样例 |
| `examples/springboot-orders/pom.xml` | 固定 Java 依赖和测试插件 |
| `examples/springboot-orders/src/test/java/local/xmock/DriverContractTest.java` | JDBC/Hikari 实际驱动契约 |
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
}

type Error struct {
    Number uint16 `json:"number"`
    SQLState string `json:"sql_state"`
    Message string `json:"message"`
}
```

初版业务列类型限定为 BIGINT、VARCHAR；所有 BIGINT 使用有符号 64 位十进制字符串，VARCHAR 使用 JSON 字符串，NULL 使用 JSON null。系统查询也以这两种列类型返回可解析的值。未声明类型直接拒绝，不推测 DECIMAL、时间或二进制的编码。

MySQL 契约 ID=`mysql.operation`，Version=1。操作名固定为 connection.open、connection.close、database.use、query、statement.prepare、statement.execute、statement.close、statement.reset。前四个 statement 操作使用 Query/Metadata；关闭、重置只携带 statement_id，归属校验必须包含 connection_id。

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

- [ ] 运行 `go test ./contracts/mysqlv1 -count=1`；提交：`feat: define typed mysql read contract`。

## Task B2: 右端场景与确定性元数据

- [ ] 在 scenario_test.go 先写三组行为：声明的查询与参数返回固定行；同一查询新参数在 replay 失败；改变 WHERE 运算符或表名后，即使 explore 也不能当成原查询自动成功。运行 `go test ./plugins/right/mysql-mock -run TestScenarioMatching -count=1` 验证失败。
- [ ] 场景文件采用下面的数据结构；schema 严格拒绝未知字段，程序里定义对应结构并导入契约类型：

```json
{
  "id": "order-query",
  "database": "app",
  "queries": [{
    "id": "order-by-id",
    "sql": "SELECT id, status FROM orders WHERE id = ?",
    "parameters": [{"name": "id", "type": "BIGINT", "nullable": false}],
    "columns": [
      {"name": "id", "type": "BIGINT", "nullable": false},
      {"name": "status", "type": "VARCHAR", "nullable": true}
    ],
    "allow_generation": true,
    "cases": [
      {"params": [{"type": "BIGINT", "value": "1001"}], "rows": [["1001", "PAID"]]},
      {"params": [{"type": "BIGINT", "value": "9007199254740993"}], "rows": [["9007199254740993", null]]},
      {"params": [{"type": "BIGINT", "value": "404"}], "rows": []}
    ]
  }]
}
```

P0 的生成范围是“已经声明结构的查询缺少某个参数组合的结果”；未声明的查询结构要求先增加场景版本、再创建新环境。这样 prepare 元数据在运行前已知，也避免模型替错误查询随意生成成功结果。

- [ ] 使用固定 parser 将查询解析为单条 SELECT AST。保留表名、列名、运算符、排序、limit 等结构；将参数节点与对应类型化值单独参与匹配。文本协议 SQL 中的字面量只有在其位置对应已声明参数时才转为该参数的类型化值。不能通过字符串替换去掉所有数字或所有 WHERE 条件。
- [ ] 实现右端 Execute：连接、数据库选择和 prepare 均确定性执行；查询命中 case 返回 Completed；已声明查询允许生成但缺数据时返回 NeedsData；不支持的操作返回 Unsupported。新建 continuation 保存 request_id、connection_id、查询签名、参数、声明元数据、场景版本和时限。
- [ ] 实现 Complete：验证 continuation 尚未完成且仍属于连接与场景版本；校验行类型、列定义、截止时间；原子完成并返回 Rows。不能修改固定场景；生成结果记录为本次运行的候选 case，由 C1 的显式导出生成新场景。
- [ ] 在 system.go 实现驱动启动所需的查询与连接设置，先覆盖 database、version、autocommit=1、character_set_client/connection/results、collation_connection、time_zone/system_time_zone、transaction_isolation、transaction_read_only。值随固定配置返回；未知系统变量或 SET 项必须报能力缺失，不能统一返回空字符串或 OK。

连接的 autocommit=false、BEGIN、COMMIT、ROLLBACK、INSERT、UPDATE、DELETE 和 DDL 在 P0 返回 MySQL 1235 / SQLSTATE 42000。没有匹配结果的 replay 请求返回 1105 / HY000，并在内部轨迹记录 request_id 与 UNMATCHED_REQUEST。

- [ ] 对库初始化实际产生的其他系统查询，在 B4 中捕获并逐项判断后补到本文件的显式映射；增加相应驱动回归验证，不能扩大为通配成功。
- [ ] 运行 `go test -race ./contracts/mysqlv1 ./plugins/right/mysql-mock -count=1`；提交：`feat: implement deterministic mysql dependency plugin`。

## Task B3: 左端协议与预处理

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
- [ ] connection.go 使用唯一的底层读循环发现 EOF，并把完整帧交给 go-mysql 连接读取；限制单帧与排队内存，不能让另一个 goroutine 与 go-mysql 同时读取原 socket。对正在等待核心响应的查询，EOF 触发 connection context 取消，并通过 IPC 撤销旧请求。禁止用 Peek 误消费下一条命令。
- [ ] 添加等待右端期间关闭客户端、关闭环境、超时和两个连接同时 prepare/execute 的测试。使用短 deadline 或显式信号控制时序，不依赖长 sleep。右端慢响应不能阻塞另一连接的探活。
- [ ] 运行 `go test -race ./plugins/left/mysql-wire -count=1`；提交：`feat: implement mysql application protocol plugin`。

## Task B4: 真实 Connector/J 与 HikariCP 验收

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

integration/mysql_driver_test.go 负责构建左右端、独立安装和启用、创建临时环境、设置 X_MOCK_MYSQL_URL 并运行 Maven；URL 包含 `sslMode=DISABLED&useServerPrepStmts=true&emulateUnsupportedPstmts=false&cachePrepStmts=false&socketTimeout=10000`。随后使用 `useServerPrepStmts=false` 验证文本路径，两个模式分别记录。

- [ ] 将固定 JDK 21 的 Home 路径放入该子进程的 JAVA_HOME；本机路径可从 Java 可执行文件向上定位到 Contents/Home，不能依赖当前 `/usr/libexec/java_home` 的发现结果。Maven 子进程日志必须打印实际 Java 版本。
- [ ] 补齐以下外部行为测试：1001 返回 PAID，404 返回带正确元数据的空集，未知表失败，改变 WHERE 运算符失败，显式事务失败，连接池复用与并行连接互不污染。每个断言都通过 JDBC 检查，不能从右端内部内存读取期望结果作为成功依据。
- [ ] 运行 `go test ./integration -run TestMySQLDriverContract -count=1 -v`，预期 Maven 测试退出码 0；首次失败时保留完整协议轨迹，修复 B2/B3 后只重跑受影响的驱动组。
- [ ] 运行 `mvn -f examples/springboot-orders/pom.xml dependency:tree`，保存精确 JDBC/Hikari 版本。提交：`test: validate mysql plugins with real JDBC and Hikari`。

P0B 完成意味着 MySQL 已验证的读取子集可用；用户的 Agent 协作与后台应用测试由 P0C 完成。
