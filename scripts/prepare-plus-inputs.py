#!/usr/bin/env python3
"""Build reproducible test fixtures using actual MyBatis-Plus BoundSql output.

Run prepare-shop-inputs.py, prepare-mybatis-inputs.py and the offline
PlusMapperContractTest first. This is a test fixture builder, not a runtime
generator: connected Coding Agents analyze QA/code/DDL through MCP.
"""
import copy
import hashlib
import json
import pathlib

root = pathlib.Path(__file__).resolve().parents[1]
scenarios = root / 'examples/scenarios'
project = 'examples/springboot-shop/'
java = project + 'src/main/java/local/xmock/plus/'
captured = {item['id']: item for item in json.loads((root / project / 'target/plus-statements.json').read_text())}
input_body = json.loads((scenarios / 'mybatis-input.json').read_text())
body = json.loads((scenarios / 'shop-candidate.json').read_text())
body['evidence'] = input_body['sources']

def value(n):
    return {'type': 'VARCHAR' if isinstance(n, str) else 'BIGINT', 'value': str(n)}

# API and domains come from the fixed QA; SQL and native parameter names come
# from the real framework, independently checked by the right Java AST strategy.
operations = [
    ('list-products', 'products', 'selectList', 'Product', [['ON_SALE']]),
    ('product-detail', 'product', 'selectById', 'Product', [[1001]]),
    ('find-cart-item', 'item', 'selectOne', 'Cart', [[2001, 2002], [1001]]),
    ('list-cart', 'cart', 'selectList', 'Cart', [[2001, 2002]]),
    ('insert-cart', 'insert', 'insert', 'Cart', [[2001], [1001], [1]]),
    ('increment-cart', 'increment', 'updateById', 'Cart', [[2001], [1001], [1, 2], [5001]]),
    ('cart-by-user-id', 'increment', 'selectOne', 'Cart', [[5001], [2001, 2002]]),
    ('delete-cart', 'removeForUser', 'delete', 'Cart', [[5001], [2001, 2002]]),
]

def statement(operation):
    identity, call, method, entity, domains = operation
    actual = captured[identity]
    return {
        'id': identity, 'sql': actual['sql'], 'source_path': java + 'PlusShopRepository.java',
        'parameters': [{'name': name, 'type': value(domain[0])['type'], 'nullable': False,
                        'allowed': [value(item) for item in domain]}
                       for name, domain in zip(actual['parameters'], domains, strict=True)],
        'source': {'strategy': 'mybatis-plus', 'namespace': 'local.xmock.plus.' + entity + 'Mapper',
                   'statement_id': method, 'mybatis_plus': {
                       'entity_path': java + ('PlusProduct.java' if entity == 'Product' else 'PlusCartItem.java'),
                       'mapper_path': java + entity + 'Mapper.java', 'call_method': call, 'version': '3.5.17',
                       'build_path': project + 'pom.xml',
                       'config_path': project + 'src/main/resources/application-mybatis-plus.properties'}}}

body['database_scenario']['statements'] = [statement(op) for op in operations[:-1]]
body['step_bindings'][4]['statements'].insert(2, 'cart-by-user-id')
body['verification']['expect_calls']['cart-by-user-id'] = {'min': 1, 'max': 1}
preview = []
for call in body['preview']:
    if call.get('statement_id') == 'increment-cart':
        preview.append({'statement_id': 'cart-by-user-id', 'params': [value(5001), value(2001)], 'connection': 'preview'})
        call['params'] = [value(n) for n in (2001, 1001, 2, 5001)]
    preview.append(call)
body['preview'] = preview

def write(prefix, inputs, candidate):
    for suffix, data in [('input', inputs), ('candidate', candidate)]:
        (scenarios / f'{prefix}-{suffix}.json').write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')

write('plus', input_body, body)
explore = copy.deepcopy(body)
product = explore['database_scenario']['initial']['products'].pop()
explore['database_scenario']['generation_slots'] = [
    {'id': 'p1', 'table': 'products', 'keys': ['1001'], 'fixed': product, 'seed': 42, 'materialized': False}]
explore['replay']['requires_generation'] = True
(scenarios / 'plus-explore-candidate.json').write_text(json.dumps(explore, ensure_ascii=False, indent=2) + '\n')

delete_input, delete_body = copy.deepcopy(input_body), copy.deepcopy(body)
qa_path = 'examples/scenarios/plus-delete.qa.md'
qa_raw = (root / qa_path).read_bytes()
delete_input['sources'] = [entry for entry in delete_input['sources'] if entry['kind'] != 'qa']
delete_input['sources'].append({'path': qa_path, 'kind': 'qa', 'sha256': hashlib.sha256(qa_raw).hexdigest()})
qa = delete_input['qa']
qa['id'], qa['goal'] = 'shopping-delete', '购物后验证归属限制、真实删除和重复删除'
qa['natural_language'] = qa_raw.decode()
qa['write_rules'] += '；仅本人能删除购物车记录，重复删除404，商品保留'
qa['coverage'] += '；删除、删除后的查询、重复删除和用户隔离'
qa['final_assertions'] = [{'table': 'cart_items', 'where': {}, 'count': 0}, qa['final_assertions'][-1]]
extra = [('S7', 'U2 删除 U1 记录', '404；U1 的一条记录数量仍为2', 404),
         ('S8', 'U1 删除本人记录并查询购物车', '204；购物车为空，合计0', 204),
         ('S9', 'U1 再删除并查询商品详情', '404；商品仍存在，库存10', 404)]
for identity, action, expected, status in extra:
    qa['steps'].append({'id': identity, 'action': action, 'expected': expected})
    delete_body['step_bindings'].append({'step_id': identity, 'api': 'DELETE /api/cart/items/5001',
        'source_path': java + 'PlusCartController.java', 'statements': ['delete-cart', 'list-cart', 'product-detail']})
    delete_body['api_expectations'].append({'step_id': identity, 'method': 'DELETE', 'path': '/api/cart/items/5001',
        'status': status, 'assertions': {'qa_expected': expected}})
delete_body['qa_contract'], delete_body['evidence'] = qa, delete_input['sources']
delete_body['database_scenario']['statements'].append(statement(operations[-1]))
delete_body['verification']['final_state'] = qa['final_assertions']
delete_body['verification']['expect_calls']['delete-cart'] = {'min': 3, 'max': 3}
for user in (2002, 2001, 2001):
    delete_body['preview'] += [
        {'control': 'BEGIN', 'connection': 'preview'},
        {'statement_id': 'delete-cart', 'params': [value(5001), value(user)], 'connection': 'preview'},
        {'control': 'COMMIT', 'connection': 'preview'}]
    if user == 2002:
        delete_body['preview'].append({'statement_id': 'list-cart', 'params': [value(2001)], 'connection': 'preview'})
delete_body['preview'] += [{'statement_id': 'list-cart', 'params': [value(2001)], 'connection': 'preview'},
                          {'statement_id': 'product-detail', 'params': [value(1001)], 'connection': 'preview'}]
write('plus-delete', delete_input, delete_body)
