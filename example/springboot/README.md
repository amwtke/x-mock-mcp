# 商品查询与下单 Spring Boot 示例

一个独立的小型 Spring Boot 程序，提供商品列表、商品详情、下单、订单详情和订单列表，并带有可点击的网页。

**不安装、不启动 MySQL、Docker 数据库或其他 SQL 引擎。** 程序使用真实 JdbcTemplate / Connector/J，只连接我们开发的 MySQL 协议插件。没有额外模型 API 或模型凭据。

```text
浏览器 / HTTP测试 → Spring Boot → JDBC
                                  ↓ MySQL TCP，默认3306
                         左端 mysql-wire/0.1.0
                                  ↓ mysql.operation/v1
                         右端 mysql-mock/0.3.0
                         场景初态、读写状态、事务

QA自然语言 + 源码 + DDL → Coding Agent → MCP prepare / put
```

左端对接应用，右端对接外部依赖。两端独立安装、启用、停用、卸载。商品参考数据在场景准备时提供；**订单初态为空，扣库存和订单 INSERT 必须由应用实际执行**。

## 快速体验

以下命令均在 **仓库根目录** 执行。需要 Go 1.27.1、C 编译器、Python 3、JDK 21、Maven。Go、Java 可分别通过 `X_MOCK_GO`、`X_MOCK_JAVA_HOME` 指定；默认也会识别仓库 `.tools/go/1.27.1/go/bin/go`，macOS可探测JDK21。

```bash
# 如需手工指定工具链，替换为本机实际路径
export X_MOCK_JAVA_HOME=/absolute/path/to/jdk-21
# export X_MOCK_GO=/absolute/path/to/go

# 首次准备：仅下载Java依赖和Chromium，不安装数据库
bash example/springboot/prepare.sh

# 启动网页和我们的mock服务，默认 HTTP 8080、MySQL协议3306
bash example/springboot/run.sh
```

打开终端打印的页面地址，默认是 `http://127.0.0.1:8080`。操作顺序：**查看详情 → 数量填2 → 下单 → 查询我的订单**。应看到订单9001、金额198元，库存从10变为8。

已有程序占用端口时，使用空闲端口；0表示自动选择：

```bash
bash example/springboot/run.sh --http-port 0 --mysql-port 0
```

`run.sh` 构建项目已有的CLI和插件包。`run.py` 创建独立运行副本，通过CLI/MCP安装插件、校验并保存场景、创建MySQL端点，再启动真实Spring Boot。JDBC地址来自刚创建的左端实例。它不会连接系统中已有的数据库，也不占用已有项目daemon的状态目录。

按 **Ctrl-C** 停止：应用进程、此次mock环境及所属daemon会清理；日志和MCP输入/结果保留在 `example/springboot/target/runs/<本次ID>/`。每次重新启动恢复库存10、空订单和自增起点9001。运行中使用源码副本，修改源码后需重启。

## HTTP接口

商品查询是公开接口；订单接口使用示例测试身份头 `X-Test-User-Id: 2001` 或 `2002`。网页固定使用2001。这是测试身份约定，没有实现生产登录服务。

| 方法 | 地址 | 行为 |
| --- | --- | --- |
| GET | `/api/products` | 查询已上架商品列表 |
| GET | `/api/products/1001` | 查询商品详情和当前库存 |
| POST | `/api/orders` | `{"productId":1001,"quantity":2}`，成功返回201 |
| GET | `/api/orders/9001` | 查询当前用户的订单详情 |
| GET | `/api/orders` | 查询当前用户的订单列表 |

在一个全新启动的环境中执行下列命令；如果已经从网页下单，当前库存和订单ID会随真实写入改变。自动端口模式请替换URL。

```bash
curl http://127.0.0.1:8080/api/products
curl http://127.0.0.1:8080/api/products/1001

curl -i http://127.0.0.1:8080/api/orders \
  -H 'Content-Type: application/json' -H 'X-Test-User-Id: 2001' \
  -d '{"productId":1001,"quantity":2}'

curl http://127.0.0.1:8080/api/orders/9001 -H 'X-Test-User-Id: 2001'
curl http://127.0.0.1:8080/api/orders -H 'X-Test-User-Id: 2001'
curl http://127.0.0.1:8080/api/products/1001

# U2不可看到U1的订单：404
curl -i http://127.0.0.1:8080/api/orders/9001 -H 'X-Test-User-Id: 2002'
```

首次下单响应：

```json
{"id":9001,"userId":2001,"productId":1001,"productName":"测试键盘","priceCents":9900,"quantity":2,"totalCents":19800,"status":"CREATED"}
```

金额字段单位均为整数“分”，页面转换成人民币显示。数量0或超过100返回400；示例商品库存不足返回409；已登记的缺失商品9999返回404；无有效测试身份的订单请求返回401。未知SQL、超出场景参数域的请求会明确失败，不伪造数据。当前固定场景的商品ID为1001/缺失ID9999，用户2001/2002，订单ID9001–9010/缺失ID9999；需要其他数据时应由Agent准备新场景。

## 一键测试完整流程

先完成 `prepare.sh`，然后运行：

```bash
bash example/springboot/test.sh
```

无需事先运行 `run.sh`。测试自己创建独立环境、安装左右插件，经真实MCP prepare/put/create/run启动Spring Boot，完成后销毁环境、分别停用和卸载插件。每个测试从空订单开始，SQL行为和最终状态也必须通过右端校验。

| 测试 | 覆盖 |
| --- | --- |
| `http` | 商品列表/详情、初始空订单、下单201、生成键、订单写后读、库存10→8、金额19800分；U2隔离、401、数量400、商品404、库存409 |
| `browser` | Chromium真实点击详情、填数量2、下单、查看订单回执、库存8、查询订单列表；保留截图和浏览器trace |
| `missing-insert` | 隔离源码副本漏掉订单INSERT，原QA断言失败；库存回滚10，订单仍为空 |
| `missing-stock-update` | 隔离源码副本漏扣库存，原QA断言失败；观察到订单存在而库存错误地保留10 |
| `forced-rollback` | 隔离源码副本把完整下单事务回滚，原QA断言失败；库存10、订单为空 |

后三项要求被测应用失败才算负例验收通过。它们刷新修改后的源码摘要，保留QA、SQL和业务预期，避免只靠旧SHA拒绝。失败状态在应用退出前通过真实HTTP检查。应用代码没有故障开关或测试专用下单分支。

日志在仓库 `artifacts/orders/verification.log`；每轮另存 `artifacts/orders/<测试名>-<runID>/`，含Maven、JUnit和协议轨迹，浏览器轮另含 `browser/order.png`、`browser/trace.zip`。

2026-09-13已实际通过上述五项、运行脚本的HTTP体验流程和原有完整回归；新模块的Java依赖也通过无数据库引擎/启动库检查。可核对 [脱敏验收摘要](../../docs/compatibility/springboot-orders-2026-09-13.json)。

## 代码和场景

| 文件 | 用途 |
| --- | --- |
| `src/main/java/local/xmock/order/` | Controller、Service、JdbcTemplate Repository、商品/订单DTO与错误响应 |
| `src/main/resources/static/` | 无前端构建工具的HTML/JavaScript网页 |
| `src/main/resources/application.properties` | 只接受 `X_MOCK_MYSQL_URL` 注入的JDBC地址，关闭SQL初始化与迁移 |
| `mock/qa.md` | 自然语言用例、初态/终态和固定业务规则 |
| `mock/schema.sql` | 提供给右端离线解析的DDL，不交给数据库执行 |
| `mock/input.json` | 归一化QA与源码/DDL证据摘要 |
| `mock/candidate.json` | 当前Coding Agent按QA与实际源码准备的SQL、参数域、初态、事务预演和断言 |
| `refresh-evidence.py` | 仅刷新已审查文件的SHA，不生成SQL、不改QA或断言 |
| `../../integration/springboot_orders_test.go` | MCP、插件生命周期、真实应用和缺陷验收 |

`OrderService.place()` 使用READ COMMITTED事务：读取商品 → 按原库存等值检查UPDATE → INSERT订单并获取生成键 → 查询已写入的订单 → 提交。库存条件不匹配或mock事务版本冲突返回409。失败会回滚，不用预装订单终态。

自动化测试复用可审查的固定候选，没有额外调用模型。接入其他QA时，由已连接的Codex或Claude Code阅读QA、代码和DDL，经MCP提交新场景。本例商品表会被应用扣库存，属于可写状态，因此在准备阶段提供初态，不使用仅适用于不可变参考实体的运行时回填槽。

修改源码后，先让Agent核对场景是否仍与实际SQL和QA一致；只有证据摘要需要更新时执行：

```bash
python3 example/springboot/refresh-evidence.py
bash example/springboot/test.sh
```

源码SQL与候选不一致时，单纯刷新摘要仍会被右端拒绝。此例仅涵盖商品查询和单商品下单，不涉及支付、取消、退款、完整登录或完整MySQL兼容。
