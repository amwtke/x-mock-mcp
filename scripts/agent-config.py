#!/usr/bin/env python3
"""Print project MCP config without including its local bearer token."""
import argparse
import json
import pathlib

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('host', choices=['codex', 'claude'])
parser.add_argument('--transport', choices=['stdio', 'http'], default='stdio')
parser.add_argument('--root', type=pathlib.Path, default=pathlib.Path.cwd())
args = parser.parse_args()
root = args.root.resolve(strict=True)
binary = pathlib.Path(__file__).resolve().parents[1] / 'bin/x-mock'
if not binary.is_file():
    parser.error('run scripts/build.sh first')
if args.transport == 'http':
    try:
        url = json.loads((root / '.x-mock/ready.json').read_text())['address']
    except (OSError, ValueError, KeyError):
        parser.error('start x-mock serve for this project first')
    server = {'type': 'http', 'url': url,
              'headers': {'Authorization': 'Bearer ${X_MOCK_MCP_TOKEN}'}}
else:
    server = {'type': 'stdio', 'command': str(binary),
              'args': ['mcp', 'stdio', '--root', str(root)]}
if args.host == 'claude':
    print(json.dumps({'mcpServers': {'xmock': server}}, ensure_ascii=False, indent=2))
else:
    print('[mcp_servers.xmock]')
    if args.transport == 'http':
        print('url = ' + json.dumps(url))
        print('bearer_token_env_var = "X_MOCK_MCP_TOKEN"')
    else:
        print('command = ' + json.dumps(server['command']))
        print('args = ' + json.dumps(server['args']))
    print('startup_timeout_sec = 20\ntool_timeout_sec = 210')
