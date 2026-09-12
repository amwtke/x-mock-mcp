#!/usr/bin/env python3
"""Replay the supplied shopping fixture through CLI/MCP and real Spring Boot.

Start the project's daemon with examples/x-mock.yaml before running this script.
Agent generation is a separate workflow: this script supplies no LLM or API.
"""
import argparse
import hashlib
import json
import pathlib
import subprocess
import time
import uuid

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--root', type=pathlib.Path, default=pathlib.Path.cwd())
parser.add_argument('--job', choices=['shop-http', 'shopping-browser', 'mybatis-http', 'mybatis-browser', 'plus-http', 'plus-browser', 'plus-delete'], default='shop-http')
args = parser.parse_args()
root = args.root.resolve(strict=True)
repo = pathlib.Path(__file__).resolve().parents[1]
binary = repo / 'bin/x-mock'
output = root / 'artifacts/demo' / uuid.uuid4().hex
output.mkdir(parents=True)


def call(command, body):
    name = '-'.join(command)
    path = output / f'{len(list(output.iterdir())):03d}-{name}-input.json'
    path.write_text(json.dumps(body, ensure_ascii=False, indent=2))
    result = subprocess.run([str(binary), *command, '--root', str(root), '--input', str(path)],
                            capture_output=True, text=True, timeout=250)
    path.with_name(path.stem.replace('-input', '-result') + '.json').write_text(result.stdout)
    if result.returncode:
        raise RuntimeError(f'{name}: {result.stderr.strip()} (see {output})')
    envelope = json.loads(result.stdout)
    return envelope['structuredContent']


env = None
try:
    caps = call(['capabilities'], {})
    refs = {}
    for role, name in [('left', 'mysql-wire'), ('right', 'mysql-mock')]:
        descriptor = json.loads(subprocess.check_output([str(repo / 'bin' / name), '--describe']))
        ref = descriptor['ref']
        refs[role] = ref
        version = ref['version']
        matches = list((repo / 'dist').glob(f'{role}-{name}-{version}-*.zip'))
        if len(matches) != 1:
            raise RuntimeError('run scripts/build.sh; dist must contain one package per role for this platform')
        package = matches[0]
        digest = hashlib.sha256(package.read_bytes()).hexdigest()
        current = next((p for p in caps['plugins'] if p['ref'] == ref), None)
        if current is None:
            call(['plugin', 'install'], {'package_path': str(package), 'sha256': digest})
        elif current['sha256'] != digest:
            raise RuntimeError(f'{name}/{version} already installed with another digest; explicitly retire that version first')
        call(['plugin', 'enable'], {'role': role, 'plugin_id': name, 'version': version})
    fixture = {'mybatis-http': 'mybatis', 'mybatis-browser': 'mybatis',
               'plus-http': 'plus', 'plus-browser': 'plus', 'plus-delete': 'plus-delete'}.get(args.job, 'shop')
    input_body = json.loads((root / f'examples/scenarios/{fixture}-input.json').read_text())
    candidate = json.loads((root / f'examples/scenarios/{fixture}-candidate.json').read_text())
    report = call(['scenario', 'prepare'], {'role': 'right', 'plugin_id': 'mysql-mock',
                   'version': refs['right']['version'], 'input': input_body, 'candidate': candidate})
    if not report['ready']:
        raise RuntimeError('scenario preparation rejected input; inspect the saved preparation report')
    document = call(['scenario', 'put'], {'expected_version': 0, 'document': {
        'id': 'demo-' + uuid.uuid4().hex, 'version': 0, 'plugin_id': 'mysql-mock', 'plugin_version': refs['right']['version'],
        'contract_id': 'mysql.operation', 'contract_version': 1,
        'input': input_body, 'body': report['compiled_body']}})
    env = call(['env', 'create'], {'scenario_id': document['id'], 'scenario_version': document['version'],
        'data_strategy': 'StrictReplay', 'timeout_ms': 10000, 'bindings': [{
            'resource_id': 'app', 'left': refs['left'],
            'right': refs['right'],
            'contract': {'id': 'mysql.operation', 'version': 1, 'capabilities': []},
            'left_config': {'host': '127.0.0.1', 'port': 0, 'username': 'mock', 'password': 'mock-local'},
            'right_config': {'mode': 'StrictReplay'}}]})
    run = call(['run', 'start'], {'environment_id': env['environment_id'], 'job_name': args.job})
    deadline = time.monotonic() + 260
    while time.monotonic() < deadline:
        status = call(['run', 'status'], {'run_id': run['run_id']})['run']
        if status['state'] != 'running':
            if status['state'] != 'succeeded':
                raise RuntimeError(f'run {status["state"]}: {status.get("error")}; {status["log_path"]}')
            print(f'StrictReplay succeeded: {run["run_id"]}\nMaven: {status["log_path"]}\nMCP results: {output}')
            break
        time.sleep(1)
    else:
        raise TimeoutError('demo run exceeded wait deadline')
finally:
    if env:
        call(['env', 'destroy'], {'environment_id': env['environment_id']})
