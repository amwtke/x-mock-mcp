# Project rules

- 左端 = 对接应用；右端 = 对接外部依赖。
- 两端均通过独立版本的进程插件安装、启用、停用和卸载。
- 核心依赖策略接口和注册表，不 import 具体协议插件。
- MySQL 的 QA 输入、代码/DDL 场景校验与状态语义归右端插件。
- 不启动真实数据库或替代 SQL 引擎；数据生成仅由 Coding Agent 经 MCP 提供，不配置额外模型 API。
- 所有运行固定插件、契约、场景和输入证据版本。
- 未支持行为明确失败，不用空结果或自动成功掩盖缺陷。
- 用户选择在当前会话逐项实现和验证，不启用子代理。
- 按 docs/superpowers/plans/2026-09-12-p0-implementation-plan.md 执行，实际进度见 docs/implementation-progress.md。
