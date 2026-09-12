这是 X-Mock-MCP 的真实 Coding Agent 宿主验收。只作为受测客户端完成流程，不开发或修改 X-Mock-MCP，不启用子代理。

使用已连接的 xmock MCP 工具。左端对接应用，右端对接外部依赖；已安装启用 mysql-wire/0.1.0 和 mysql-mock/0.2.0，均使用 mysql.operation/v1。

1. 读取 mysql-mock 的 guide、qa_input、candidate schema。读取 examples/scenarios/shop.qa.md、样例 Controller/Service/Repository/DTO、测试和 schema.sql。自己分析，不读取其他项目的预制场景。
2. 保留 QA 原文，归一化 QA 输入。source 引用从当前文件计算 SHA-256。先调用 mock_scenario_prepare(input) 检查资料，再自己编写候选并调用 prepare/put。保存实际 input/candidate/compiled 到 acceptance/。QA 固定价格9900分、名称测试键盘、库存10、状态ON_SALE；内部ID固定P1=1001、U1=2001、U2=2002、购物车自增5001。最后U1同一记录数量2、U2空车、商品不扣库存；initial_assertions要求初始空购物车，final_assertions和verification.final_state一致。
3. SQL从真实 Repository 字面量推导，每个参数有明确allowed域；别填Plan（由插件编译）。写操作必须包含符合应用流程的preview SQL步骤和COMMIT；QA/步骤/API/SQL/数据绑定必须完整。唯一一个商品参考实体缺口slot p1属于products表，fixed包含所有DDL字段；初始products为空，cart_items为空，禁止生成购物车。
4. prepare/put通过后，创建AgentFill环境：bindings数组只有app资源，left mysql-wire、right mysql-mock，两端role正确。left_config={"host":"127.0.0.1","port":0,"username":"mock","password":"mock-local"}；right_config={"mode":"AgentFill"}；contract={"id":"mysql.operation","version":1,"capabilities":[]}；timeout_ms=180000。
5. 后台启动job_name=shopping-browser，立即主动mock_requests_next(limit=1,wait_ms=2000)；保存并复用owner_token。依QA/DDL填写EntityFill，调用mock_requests_resolve。不要长时间离开领取循环，业务deadline为180秒。你生成实体，mock本身不调用模型。
6. 用mock_run_status和mock_run_trace核对浏览器/HTTP、INSERT/UPDATE、最终状态通过。导出明确成功request_id对应的参考实体，保存导出候选；只保存初始空购物车和参考实体，不复制终态。按expected_version保存新版本，在新StrictReplay环境（right_config.mode同步修改）再运行shopping-browser，确认无生成请求且成功。
7. 保存两个run_id、候选、工具结果摘要到acceptance/report.md；销毁两个环境，再分别停用并卸载左端、右端，验证卸载左端时右端还在。不要停止daemon。

不得修改应用代码、SQL、QA或测试断言使测试通过，不启动真实或替代数据库，不配置额外模型API。可用终端读文件、计算摘要、保存候选；控制操作应通过原生 xmock MCP 工具。如果MCP参数较大，可从已保存JSON读取后传给工具，不要用CLI替代所有原生MCP调用。
