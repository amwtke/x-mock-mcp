"""Audit resolved dependencies for database services/engines and bootstrap tools."""
import argparse
import pathlib
import re


def violations(go_dependencies, java_dependencies):
    forbidden_go = (
        'github.com/dolthub/go-mysql-server', 'github.com/mattn/go-sqlite3',
        'modernc.org/sqlite', 'github.com/testcontainers/',
        'github.com/ory/dockertest', 'github.com/docker/docker',
    )
    result = []
    for line in go_dependencies.splitlines():
        name = line.strip()
        tidb_engine = name.startswith('github.com/pingcap/tidb/') and not (
            name == 'github.com/pingcap/tidb/pkg/parser' or name.startswith('github.com/pingcap/tidb/pkg/parser/'))
        if tidb_engine or name.startswith(forbidden_go):
            result.append(name)
    forbidden_java = re.compile(
        r'(com\.h2database:h2|org\.hsqldb:hsqldb|org\.xerial:sqlite-jdbc|'
        r'org\.apache\.derby:[\w-]+|org\.testcontainers:[\w-]+|'
        r'com\.wix:wix-embedded-mysql|ch\.vorburger\.mariaDB4j:[\w-]+|'
        r'io\.zonky\.test:embedded-[\w-]+|ru\.yandex\.qatools\.embed:[\w-]+):')
    for line in java_dependencies.splitlines():
        if forbidden_java.search(line):
            result.append(line.strip())
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go-deps', type=pathlib.Path, required=True)
    parser.add_argument('--java-deps', type=pathlib.Path, required=True)
    args = parser.parse_args()
    found = violations(args.go_deps.read_text(), args.java_deps.read_text())
    if found:
        raise SystemExit('Forbidden database service/engine dependency:\n' + '\n'.join(found))
    print('PASS database boundary: resolved dependencies contain no listed database engines or service bootstrap tools.')
