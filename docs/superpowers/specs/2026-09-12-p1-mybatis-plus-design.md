# P1.3 MyBatis-Plus 自动 CRUD 设计

用户已选择 MyBatis / MyBatis-Plus，P1.1 静态 XML 已合并 master（a85214f），本次“继续”推进此前确认的 Plus 自动 SQL 独立增量。保持当前会话逐项实现，不启用子代理。

## 不变的核心价值

不安装、不启动真实 MySQL、容器数据库、替代 SQL 引擎，不代理真实数据库，不配置额外模型 API。左端对接应用，右端对接外部依赖；两端仍为独立安装、启用、停用、卸载的进程插件。

QA 自然语言 → Coding Agent 分析实际源码/DDL → MCP 提交场景与参考实体 → 右端校验/维护状态 → 左端将结果编码为 MySQL 报文。应用 JDBC 只连接左端端口，购物车必须由应用真实请求写入。新增删除用例独立保存，不改掉原购物 S1–S6 的固定预期。

## 方案选择

采用右端内的离线 Java AST 来源策略。相比仅接受 Agent 提供的 SQL 清单，它能核对实际实体映射、Mapper 继承和 Wrapper 字段；相比启动应用收集 SQL，它无需执行项目代码或启动数据源。使用 Tree-sitter Java 做语法解析，SQL 仍由已有 TiDB parser 离线编译；两者都不是 SQL 执行引擎。

增加 `mybatis-plus` 来源策略；现有 `java-literal` 和 `mybatis-xml` 保留。所有策略接收 prepare 已校验 SHA 的文件集合，通用核心、MCP 工具和左端不认识 ORM。右端升级 0.3.0；左端 0.1.0、mysql.operation/v1 和 16 个 MCP 工具不变。

## 来源与支持范围

`statement.source` 保留 strategy/namespace/statement_id，新增 `mybatis_plus` 对象：`entity_path`、`mapper_path`、`call_method`、`version`。`source_path` 指向实际调用 Mapper 的 Java 类；namespace 是 Mapper 全限定名，statement_id 是被继承的 BaseMapper 方法名。所有引用文件都必须在 input.sources 中且摘要正确。

右端离线读取 Java AST：

1. 实体是普通顶层 POJO，显式 @TableName、@TableId（AUTO/INPUT）及 @TableField；先支持 Long/String、简单 getter/setter、无继承/逻辑删除/版本字段/填充/类型处理器。注解和类型必须解析到实际 MyBatis-Plus 类型，不能用同名自定义注解冒充。
2. Mapper 是直接继承 `com.baomidou.mybatisplus.core.mapper.BaseMapper<该实体>` 的接口，方法体为空；不允许自定义同名 CRUD 覆盖。
3. 在指定调用方法中找到唯一匹配的 Mapper 字段与方法调用；Wrapper 是直接构造的 `LambdaQueryWrapper<Entity>`，支持无条件重载 `eq(getter,value)`、`orderByAsc/Desc(getter)`。不接受变量中拼装的 Wrapper、条件开关、嵌套/OR、raw SQL、分页或拦截器假设。
4. 根据实体字段顺序、主键和实际调用形状构造 SQL/参数元数据；再比对候选完整 SQL AST 与参数名/顺序/类型。基础方法支持 selectById、insert（AUTO 主键未赋值、全部普通字段非空）、updateById（全部普通字段非空）、deleteById；Wrapper 方法支持 selectList/selectOne/delete。部分实体动态字段掩码不伪装成已支持分支。
5. 真实框架默认列映射与 NOT_NULL 字段策略，由固定版本下的真实 BoundSql 测试交叉核对；运行时发生不同分支仍返回未匹配错误。

资料包含实际 pom.xml、Plus 配置、实体/Mapper/调用源码和 QA/DDL。当前框架版本锁定 3.5.17。全局自定义 SQL 注入、租户、逻辑删除、乐观锁、字段填充、自定义类型处理器不在本阶段支持范围，不能通过候选属性自行开启。

## DELETE 状态语义

增加受限单表 DELETE：必须有等值 AND 条件，不接受无条件、JOIN、多表、LIMIT/ORDER BY 或返回集。执行真实匹配删除，返回受影响行数；未命中为 0。事务使用行删除标记，支持本连接立即不可见、其他连接提交前仍可见、回滚/断连恢复、提交后删除、版本冲突检测。删除被外键引用的父行以 1451/23000 拒绝，不做隐式级联；原有插入外键错误保留 1452。

## 样例和验收

Spring Boot 样例添加 `mybatis-plus` Repository/实体/BaseMapper，使用 Plus Starter 3.5.17（替换重复的原 Starter，底层 MyBatis 仍为 3.5.19 / MyBatis-Spring 3.0.5）。原 JdbcTemplate 和静态 XML profile 都保留回归；共享 Controller/Service 和原自然语言 QA。Plus 专用删除 API 增加独立的“购物后移除记录”用例与固定初终态。

离线 Java 测试用真实 MybatisConfiguration / BaseMapper SQL 注入和 BoundSql 校验生成 SQL、参数名/顺序、生成键，不配置 DataSource。真实 HTTP/Chromium 跑 S1–S6，经 MCP 测试客户端回填并做三次干净回放；删除用例通过真实 API 验证。负例包括实体/Mapper/Wrapper变化、伪造参数、漏写入、错误数量、漏用户条件、错误/遗漏删除。更新摘要也不能绕过来源和业务校验。

全量回归和依赖边界审计继续覆盖生产与测试。Claude 沿用无需账号/真实模型 E2E 的用户豁免；自动 MCP 客户端不冒充原生模型验收。

## 工具链与依据

Tree-sitter Go v0.24.0、Java grammar v0.23.5；构建右端需 C 编译器，安装后的插件包包含独立可执行文件，不需要目标工程运行 Java 分析器。保留关闭 parser/tree 的资源管理和每文件 1 MiB/资料总量 2 MiB 的上限。

[Plus 安装说明](https://baomidou.com/en/getting-started/install/)、[BaseMapper API](https://baomidou.com/en/guides/data-interface/)、[注解说明](https://baomidou.com/en/reference/annotation/)、[Tree-sitter Go](https://github.com/tree-sitter/go-tree-sitter)、[Java grammar](https://github.com/tree-sitter/tree-sitter-java)。
