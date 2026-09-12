#!/usr/bin/env python3
"""Compile explicit sample inputs from the checked-in QA and source evidence.
This fixture builder makes automated infrastructure tests repeatable; host
acceptance separately asks the Coding Agent to analyze the same sources.
"""
import hashlib,json,pathlib,re,copy
root=pathlib.Path(__file__).resolve().parents[1]
def v(n):return {'type':'BIGINT','value':str(n)}
def text(s):return {'type':'VARCHAR','value':s}
def write(name,obj):(root/'examples/scenarios'/name).write_text(json.dumps(obj,ensure_ascii=False,indent=2)+'\n')
qa_path='examples/scenarios/shop.qa.md'
if not (root/qa_path).exists():(root/qa_path).write_text((root/'docs/templates/mysql-qa-scenario.md').read_text())
repo='examples/springboot-shop/src/main/java/local/xmock/ShopRepository.java'
paths=[('examples/springboot-shop/schema.sql','ddl'),(qa_path,'qa')]
paths += [(str(p.relative_to(root)),'code') for p in sorted((root/'examples/springboot-shop/src/main').rglob('*')) if p.is_file()]
paths += [('examples/springboot-shop/src/test/java/local/xmock/'+name,'test') for name in ('ShopFlowTest.java','ShoppingBrowserTest.java')]
sources=[{'path':p,'kind':k,'sha256':hashlib.sha256((root/p).read_bytes()).hexdigest()} for p,k in paths]
steps=[('打开首页','测试键盘，99 元'),('点击测试键盘','同商品、同价格、库存 10'),('数量 1 加入购物车','HTTP 201，稳定记录标识，数量 1'),('打开购物车','一条记录，数量 1，合计 99 元'),('返回详情再加入 1，查看购物车','HTTP 200，同一记录，数量 2，合计 198 元'),('U2 查看购物车','为空，不含 U1 商品')]
final=[{'table':'cart_items','where':{'user_id':v(2001)},'count':1,'fields':{'id':v(5001),'product_id':v(1001),'quantity':v(2)}},{'table':'cart_items','where':{'user_id':v(2002)},'count':0},{'table':'products','where':{'id':v(1001)},'count':1,'fields':{'name':text('测试键盘'),'price_cents':v(9900),'stock':v(10),'status':text('ON_SALE')}}]
qa={'id':'shopping','goal':'浏览商品并重复加入购物车，验证用户隔离','natural_language':(root/qa_path).read_text(),'start_page':'/','role':'U1=2001 已登录；U2=2002 已登录','initial_state':'P1=1001 已上架，测试键盘，99 元，库存10；两个用户购物车为空','data_policy':'QA 名称、价格、库存和预期固定；使用约定的内部 ID','write_rules':'重复加入累计数量并保留同一记录；不扣库存','coverage':'首页、详情、首次与重复加入、购物车、多用户；异常及并发另测','steps':[{'id':'S'+str(i+1),'action':a,'expected':e} for i,(a,e) in enumerate(steps)],'fixed':{'name':'测试键盘','price_cents':'9900','stock':'10','status':'ON_SALE'},'initial_assertions':[{'table':'cart_items','where':{},'count':0}],'final_assertions':final}
sqls=re.findall(r'"((?:SELECT|INSERT|UPDATE) [^"]+)"',(root/repo).read_text())
ids=['list-products','product-detail','find-cart-item','list-cart','insert-cart','increment-cart']
domains=[[[text('ON_SALE')]],[[v(1001)]],[[v(2001),v(2002)],[v(1001)]],[[v(2001),v(2002)]],[[v(2001)],[v(1001)],[v(1)]],[[v(1)],[v(5001)],[v(2001)]]]
# Keep each parameter's actual type and bounded values.
statements=[]
for name,sql,allowed in zip(ids,sqls,domains):
 if name=='list-products':allowed=[[text('ON_SALE')]]
 statements.append({'id':name,'sql':sql,'source_path':repo,'parameters':[{'name':'p'+str(i),'type':vals[0]['type'],'nullable':False,'allowed':vals} for i,vals in enumerate(allowed)]})
product={'id':v(1001),'name':text('测试键盘'),'price_cents':v(9900),'stock':v(10),'status':text('ON_SALE')}
apis=[('GET','/api/products',200,['list-products']),('GET','/api/products/1001',200,['product-detail']),('POST','/api/cart/items',201,['product-detail','find-cart-item','insert-cart']),('GET','/api/cart',200,['list-cart','product-detail']),('POST','/api/cart/items',200,['product-detail','find-cart-item','increment-cart','list-cart']),('GET','/api/cart',200,['list-cart'])]
preview=[]
def query(id,*values):preview.append({'statement_id':id,'params':[x if isinstance(x,dict) else v(x) for x in values],'connection':'preview'})
def control(sql):preview.append({'control':sql,'connection':'preview'})
query('list-products',text('ON_SALE'));query('product-detail',1001)
for repeat in (False,True):
 control('BEGIN');query('product-detail',1001);query('find-cart-item',2001,1001)
 if not repeat:query('insert-cart',2001,1001,1)
 else:query('increment-cart',1,5001,2001)
 query('find-cart-item',2001,1001);control('COMMIT');query('list-cart',2001);query('product-detail',1001)
query('list-cart',2002)
body={'qa_contract':qa,'evidence':sources,'step_bindings':[{'step_id':'S'+str(i+1),'api':m+' '+p,'source_path':'examples/springboot-shop/src/main/java/local/xmock/'+('ProductController.java' if i<2 else 'CartController.java'),'statements':ids} for i,(m,p,status,ids) in enumerate(apis)],'api_expectations':[{'step_id':'S'+str(i+1),'method':m,'path':p,'status':status,'assertions':{'qa_expected':qa['steps'][i]['expected']}} for i,(m,p,status,ids) in enumerate(apis)],'data_bindings':[{'qa_key':key,'table':'products','key':'1001','column':key} for key in qa['fixed']],'database_scenario':{'database':'app','mode':'stateful','initial':{'products':[product],'cart_items':[]},'statements':statements,'next_ids':{'cart_items':'5001'},'transaction':'READ-COMMITTED'},'verification':{'expect_calls':{'insert-cart':{'min':1,'max':1},'increment-cart':{'min':1,'max':1},'list-cart':{'min':3,'max':8},'product-detail':{'min':5,'max':20}},'final_state':final},'replay':{'seed':42,'clock':'2026-09-12T00:00:00Z','requires_generation':False},'preview':preview}
write('shop-input.json',{'qa':qa,'sources':sources});write('shop-candidate.json',body)
explore=copy.deepcopy(body);explore['database_scenario']['initial']['products']=[];explore['database_scenario']['generation_slots']=[{'id':'p1','table':'products','keys':['1001'],'fixed':product,'seed':42,'materialized':False}];explore['replay']['requires_generation']=True
write('shop-explore-candidate.json',explore)
