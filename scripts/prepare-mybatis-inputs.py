#!/usr/bin/env python3
"""Build automated-test fixtures from the fixed shopping QA and actual Mapper.

This is not a runtime generator. Connected Coding Agents still analyze QA/code/
DDL and submit their own candidates through MCP. No database is used here.
Run prepare-shop-inputs.py first when shared application sources change.
"""
import copy
import hashlib
import json
import pathlib
import re
import xml.etree.ElementTree as ET

root = pathlib.Path(__file__).resolve().parents[1]
scenarios = root / 'examples/scenarios'
input_body = json.loads((scenarios / 'shop-input.json').read_text())
body = json.loads((scenarios / 'shop-candidate.json').read_text())
mapper_path = 'examples/springboot-shop/src/main/resources/mappers/ShopMapper.xml'
mapper = ET.fromstring((root / mapper_path).read_bytes())
method_ids = ['products', 'product', 'item', 'cart', 'insert', 'increment']
for statement, method in zip(body['database_scenario']['statements'], method_ids, strict=True):
    node = next(node for node in mapper if node.get('id') == method)
    if len(node):
        raise ValueError(f'{method}: static Mapper required')
    source_sql = node.text.strip()
    properties = []

    def parameter(match):
        properties.append(match.group(1).split(',')[0].strip())
        return '?'

    statement['sql'] = re.sub(r'#\{([^}]+)\}', parameter, source_sql)
    statement['source_path'] = mapper_path
    statement['source'] = {'strategy': 'mybatis-xml', 'namespace': mapper.get('namespace'), 'statement_id': method}
    for param, name in zip(statement['parameters'], properties, strict=True):
        param['name'] = name

known = {entry['path'] for entry in input_body['sources']}
additional = [(root / 'examples/springboot-shop/pom.xml', 'config')]
additional += [(path, 'test') for path in sorted((root / 'examples/springboot-shop/src/test').rglob('*.java'))]
for path, kind in additional:
    relative = path.relative_to(root).as_posix()
    if relative not in known:
        input_body['sources'].append({'path': relative, 'kind': kind, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()})
        known.add(relative)
body['evidence'] = input_body['sources']
explore = copy.deepcopy(body)
product = explore['database_scenario']['initial']['products'].pop()
explore['database_scenario']['generation_slots'] = [{'id': 'p1', 'table': 'products', 'keys': ['1001'], 'fixed': product, 'seed': 42, 'materialized': False}]
explore['replay']['requires_generation'] = True
for name, value in [('mybatis-input.json', input_body), ('mybatis-candidate.json', body), ('mybatis-explore-candidate.json', explore)]:
    (scenarios / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
