#!/usr/bin/env python3
"""Refresh explicit evidence hashes for the driver acceptance fixtures."""
import hashlib,json,pathlib,re
root=pathlib.Path(__file__).resolve().parents[1]
def value(n):return {'type':'BIGINT','value':str(n)}
def source(path,kind):return {'path':path,'kind':kind,'sha256':hashlib.sha256((root/path).read_bytes()).hexdigest()}
def write(name,obj):(root/'examples/scenarios'/name).write_text(json.dumps(obj,ensure_ascii=False,indent=2)+'\n')
for mode in ('driver','transaction'):
 code='examples/springboot-shop/src/test/java/local/xmock/'+('DriverContractTest.java' if mode=='driver' else 'TransactionContractTest.java')
 ddl='examples/scenarios/driver-schema.sql' if mode=='driver' else 'examples/springboot-shop/schema.sql'
 qa={'id':mode,'goal':'真实 JDBC 类型与事务契约','natural_language':'通过实际驱动验证大整数、NULL、空集。事务写入本连接可见，提交后其他连接可见，回滚不泄漏，并发提交冲突失败。','start_page':'JDBC 契约入口','role':'mock 测试用户','initial_state':'商品 1001 存在，购物车为空；驱动 orders 使用独立只读夹具','data_policy':'全部固定，无运行时生成','write_rules':'自增从 5001 开始，回滚允许空洞；更新累计数量；不扣库存','coverage':'只覆盖明示的 JDBC 调用','steps':[{'id':'S1','action':'执行真实 JDBC 契约','expected':'所有外部断言通过'}],'fixed':{}}
 sources=[source(ddl,'ddl'),source(code,'code')]
 statements=[]
 if mode=='driver':
  statements=[{'id':'orders','sql':'SELECT id, status FROM orders WHERE id = ?','source_path':code,'parameters':[{'name':'id','type':'BIGINT','nullable':False,'allowed':[value(n) for n in (1001,404,9007199254740993)]}],'cases':[{'params':[value(1001)],'rows':[['1001','PAID']]},{'params':[value(404)],'rows':[]},{'params':[value(9007199254740993)],'rows':[['9007199254740993',None]]}]}]
  db={'database':'app','mode':'fixture','initial':{'orders':[]},'statements':statements}
 else:
  for name in ('READ','INSERT','UPDATE','NOOP'):
   sql=re.search(r'String '+name+r'="([^"]+)"',(root/code).read_text()).group(1)
   domains=[[2001,2002],[1001]] if name=='READ' else ([[2001,2002],[1001],[1]] if name=='INSERT' else [[1,2],[2001],[1001]])
   statements.append({'id':name.lower(),'sql':sql,'source_path':code,'parameters':[{'name':'p'+str(i),'type':'BIGINT','nullable':False,'allowed':[value(n) for n in ns]} for i,ns in enumerate(domains)]})
  db={'database':'app','mode':'stateful','initial':{'products':[{'id':value(1001),'name':{'type':'VARCHAR','value':'测试键盘'},'price_cents':value(9900)}],'cart_items':[]},'statements':statements,'next_ids':{'cart_items':'5001'}}
 body={'qa_contract':qa,'evidence':sources,'step_bindings':[{'step_id':'S1','api':'JDBC contract','source_path':code,'statements':[s['id'] for s in statements]}],'api_expectations':[],'data_bindings':[],'database_scenario':db,'verification':{},'replay':{'seed':42,'clock':'2026-09-12T00:00:00Z','requires_generation':False}}
 if mode=='transaction':body['preview']=[{'statement_id':'insert','params':[value(2001),value(1001),value(1)],'connection':'preview'}]
 write(mode+'-input.json',{'qa':qa,'sources':sources});write(mode+'-candidate.json',body)
