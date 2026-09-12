# Spring Boot 商品查询与下单 Implementation Plan

> **For agentic workers:** 使用 superpowers:executing-plans 在当前会话逐项执行，按用户约定不启用子代理。

**Goal:** 在用户指定的 `example/springboot` 提供可运行的商品查询、下单、订单查询与库存扣减程序、完整E2E和README。

**Architecture:** 独立 Spring Boot/JdbcTemplate 模块，真实HTTP与浏览器调用应用；JDBC只连接现有 mysql-wire/0.1.0 → mysql-mock/0.3.0。QA、DDL、源码与候选均保存在示例内，经MCP prepare/put/create/run验证；商品作为场景初态，订单初态为空，扣库存和订单INSERT由同一应用事务执行。复用插件，不修改协议或核心，不启动任何数据库或额外模型API。

**Tech Stack:** 已验证的 Spring Boot3.5.16、JDK21、Connector/J9.7.0、JdbcTemplate、JUnit、Playwright1.62.0；现有Go1.27.1 MCP与插件测试工具。

---

### Task 1：HTTP测试和应用

Files：创建 `example/springboot/pom.xml`、`src/main/java/local/xmock/order/{OrderApplication,Product,PurchaseOrder,OrderRepository,OrderService,OrderController,ApiErrors}.java`、application.properties、`src/test/java/local/xmock/order/OrderFlowTest.java`。

- [x] 先搭建只有启动类的模块并写真实HTTP测试，运行 Maven（仅红灯阶段排除DataSource自动配置），观察商品列表404而不是预期200。
- [x] 实现 `GET /api/products`、`GET /api/products/{id}`、`POST /api/orders`、`GET /api/orders/{id}`、`GET /api/orders`。下单输入 `{"productId":1001,"quantity":2}`，U1=2001；商品9900分/库存10，返回201/订单9001/19800分，库存变8。`@Transactional(isolation=READ_COMMITTED)` 包含等值CAS库存UPDATE与INSERT，失败回滚。数量非法400、商品/订单不存在404、库存不足409、U2不可查U1订单。
- [x] 添加简洁商品列表/详情/数量/订单回执页面与 Playwright 浏览器测试，执行真实点击和上述断言。

### Task 2：QA场景、MCP与负例

Files：创建 `example/springboot/mock/{qa.md,schema.sql,input.json,candidate.json}`、`refresh-evidence.py`、`integration/springboot_orders_test.go`。

- [x] 按固定QA/实际SQL生成候选：初始products一行，orders为空，金额BIGINT分；记录API/SQL映射、有限参数域、BEGIN/UPDATE/INSERT/COMMIT预演及最终库存8、订单数量1。输入路径均为仓库相对路径且SHA真实；刷新脚本只更新摘要，不编造SQL或放宽QA。
- [x] 通过真实MCP客户端安装两端、prepare/put、创建环境，启动真实Maven HTTP和浏览器任务，每项新环境；检查run验证、SQL轨迹、实际UPDATE/INSERT与终态；销毁环境并分别卸载。
- [x] 隔离副本注入漏INSERT、漏库存UPDATE、事务回滚，保持QA与候选不变并刷新源码SHA，要求真实HTTP/最终状态断言失败。验证不能靠预装订单或成功码掩盖缺陷。

### Task 3：直接运行、README与交付

Files：创建 `example/springboot/{prepare.sh,test.sh,run.sh,run.py,README.md}`；更新根README和实施记录。

- [x] prepare.sh只准备JVM依赖和Chromium；test.sh执行指定Go集成测试；run.py在独立临时项目启动已有daemon，经CLI/MCP创建mock环境，再启动Spring Boot，打印实际HTTP/MySQL地址，Ctrl-C清理所属进程及环境。run.sh负责工具链与构建。示例所有数据库连接均来自创建的左端环境。
- [x] README写明目录、接口、QA、金额/身份/库存规则、运行和curl步骤、测试范围及无数据库约束。明确初始参考商品由Coding Agent按QA/代码/DDL准备，自动测试复用已审查候选，未额外调用模型。
- [x] 执行 `bash example/springboot/prepare.sh`、`bash example/springboot/test.sh`；实际执行run命令并通过HTTP完成查询/下单/订单查询/库存检查。审计新模块Java依赖；现有全量验证回归；记录证据。
- [ ] 按会话授权提交、合并并推送master。
