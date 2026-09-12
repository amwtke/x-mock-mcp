# Project rules

- 左端 = 对接应用；右端 = 对接外部依赖。
- 两端均通过独立版本的进程插件安装、启用、停用和卸载。
- 核心依赖策略接口和注册表，不 import 具体协议插件。
- MySQL 的 QA 输入、代码/DDL 场景校验与状态语义归右端插件。
- 不安装、不启动真实 MySQL 或其他数据库，不使用容器数据库、替代 SQL 引擎或真实数据库转发兜底。DDL/SQL 解析器仅用于离线校验。
- Spring Boot 的 JDBC 只连接左端插件的 MySQL 协议端口；Coding Agent 经 MCP 将 QA 自然语言、源码和 DDL 转为场景与参考数据，右端维护状态，左端编码协议应答。不配置额外模型 API。
- 购物车等业务写入必须由应用实际请求产生，不能预装终态或自动成功来走通测试。
- 所有运行固定插件、契约、场景和输入证据版本。
- 未支持行为明确失败，不用空结果或自动成功掩盖缺陷。
- 用户选择在当前会话逐项实现和验证，不启用子代理。
- P0 已合并 master。当前按 docs/superpowers/plans/2026-09-12-p1-mybatis-implementation.md 执行，实际进度见 docs/implementation-progress.md。
