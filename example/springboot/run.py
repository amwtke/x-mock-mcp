#!/usr/bin/env python3
"""Run the order demo using our two process plugins; no database service."""
import argparse
import hashlib
import json
import os
import pathlib
import re
import shutil
import signal
import subprocess
import time
import urllib.request
import uuid

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--http-port', type=int, default=8080, help='Spring Boot port; 0 selects a free port')
parser.add_argument('--mysql-port', type=int, default=3306, help='Our MySQL wire port; 0 selects a free port')
args = parser.parse_args()
if not all(0 <= port <= 65535 for port in (args.http_port, args.mysql_port)):
    parser.error('ports must be between 0 and 65535')
project = pathlib.Path(__file__).resolve().parent
repo = project.parents[1]
binary = repo / 'bin/x-mock'
runtime = project / 'target/runs' / uuid.uuid4().hex
runtime.mkdir(parents=True)
shutil.copytree(project, runtime / 'example/springboot', ignore=shutil.ignore_patterns('target', '__pycache__'))
daemon = application = environment = None
sequence = 0


def call(command, body):
    global sequence
    sequence += 1
    path = runtime / f'{sequence:03d}-{"-".join(command)}.json'
    path.write_text(json.dumps(body, ensure_ascii=False, indent=2))
    result = subprocess.run([str(binary), *command, '--root', str(runtime), '--input', str(path)],
                            capture_output=True, text=True, timeout=30)
    path.with_suffix('.result.json').write_text(result.stdout)
    if result.returncode:
        raise RuntimeError(f'{" ".join(command)}: {result.stderr.strip()}; details: {path}')
    return json.loads(result.stdout)['structuredContent']


def stop(process):
    if process is None or process.poll() is not None:
        return
    os.killpg(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)


def interrupted(signum, frame):
    raise KeyboardInterrupt


signal.signal(signal.SIGTERM, interrupted)
try:
    with (runtime / 'daemon.log').open('w') as log:
        daemon = subprocess.Popen([str(binary), 'serve', '--root', str(runtime), '--listen', '127.0.0.1:0'],
                                  stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    ready = runtime / '.x-mock/ready.json'
    deadline = time.monotonic() + 20
    while not ready.exists():
        if daemon.poll() is not None or time.monotonic() >= deadline:
            raise RuntimeError(f'daemon did not start; see {runtime / "daemon.log"}')
        time.sleep(0.1)
    refs = {}
    for role, name, expected in [('left', 'mysql-wire', '0.1.0'), ('right', 'mysql-mock', '0.3.0')]:
        ref = json.loads(subprocess.check_output([str(repo / 'bin' / name), '--describe']))['ref']
        if ref['version'] != expected:
            raise RuntimeError(f'this demo pins {name}/{expected}; review the scenario before upgrading')
        refs[role] = ref
        packages = list((repo / 'dist').glob(f'{role}-{name}-{expected}-*.zip'))
        if len(packages) != 1:
            raise RuntimeError(f'build exactly one package for this platform: {role}/{name}/{expected}')
        archive = packages[0]
        call(['plugin', 'install'], {'package_path': str(archive), 'sha256': hashlib.sha256(archive.read_bytes()).hexdigest()})
        call(['plugin', 'enable'], {'role': role, 'plugin_id': name, 'version': expected})
    inputs = json.loads((runtime / 'example/springboot/mock/input.json').read_text())
    candidate = json.loads((runtime / 'example/springboot/mock/candidate.json').read_text())
    report = call(['scenario', 'prepare'], {'role': 'right', 'plugin_id': 'mysql-mock',
        'version': refs['right']['version'], 'input': inputs, 'candidate': candidate})
    if not report['ready']:
        raise RuntimeError(f'QA/source/DDL preparation rejected; see saved prepare result in {runtime}')
    doc = call(['scenario', 'put'], {'expected_version': 0, 'document': {
        'id': 'orders', 'version': 0, 'plugin_id': 'mysql-mock', 'plugin_version': refs['right']['version'],
        'contract_id': 'mysql.operation', 'contract_version': 1, 'input': inputs, 'body': report['compiled_body']}})
    environment = call(['env', 'create'], {'scenario_id': doc['id'], 'scenario_version': doc['version'],
        'data_strategy': 'StrictReplay', 'timeout_ms': 10000, 'bindings': [{
            'resource_id': 'app', 'left': refs['left'], 'right': refs['right'],
            'contract': {'id': 'mysql.operation', 'version': 1, 'capabilities': []},
            'left_config': {'host': '127.0.0.1', 'port': args.mysql_port, 'username': 'mock', 'password': 'mock-local'},
            'right_config': {'mode': 'StrictReplay'}}]})
    endpoint = environment['bindings'][0]['endpoints']['mysql']
    jdbc = f'jdbc:mysql://{endpoint}/app?sslMode=DISABLED&useServerPrepStmts=true&emulateUnsupportedPstmts=false&cachePrepStmts=false&socketTimeout=10000&connectionCollation=utf8mb4_bin'
    with (runtime / 'springboot.log').open('w') as log:
        application = subprocess.Popen(['mvn', '-B', '-ntp', '-o', '-f', 'example/springboot/pom.xml', 'spring-boot:run'],
            cwd=runtime, env={**os.environ, 'PORT': str(args.http_port), 'X_MOCK_MYSQL_URL': jdbc},
            stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    deadline = time.monotonic() + 60
    url = None
    while time.monotonic() < deadline:
        if application.poll() is not None:
            raise RuntimeError(f'Spring Boot exited; see {runtime / "springboot.log"}')
        match = re.search(r'Tomcat started on port (\d+)', (runtime / 'springboot.log').read_text())
        if match:
            url = 'http://127.0.0.1:' + match.group(1)
            with urllib.request.urlopen(url + '/api/products', timeout=10) as response:
                json.load(response)  # Confirm the real JDBC request reached our plugins.
            break
        time.sleep(0.2)
    if url is None:
        raise RuntimeError(f'Spring Boot startup timed out; see {runtime / "springboot.log"}')
    (project / 'target/last-run.json').write_text(json.dumps({
        'url': url, 'mysql': endpoint, 'runtime': str(runtime), 'runner_pid': os.getpid()}, indent=2) + '\n')
    print(f'商品下单页面：{url}\nMySQL 协议插件：{endpoint}\n日志与 MCP 结果：{runtime}\n按 Ctrl-C 停止应用并销毁本次 mock 环境。', flush=True)
    code = application.wait()
    if code:
        raise RuntimeError(f'Spring Boot exited with {code}; see {runtime / "springboot.log"}')
except KeyboardInterrupt:
    print('\n正在清理本次示例…', flush=True)
finally:
    stop(application)
    try:
        if environment and daemon and daemon.poll() is None:
            call(['env', 'destroy'], {'environment_id': environment['environment_id']})
    finally:
        stop(daemon)
