#!/usr/bin/env python3
"""Refresh SHA evidence after reviewed source changes; never rewrite QA or SQL.

Changed business behavior needs a Coding Agent to review QA/DDL/SQL again.
Refreshing a hash alone cannot make an inconsistent candidate pass prepare.
"""
import hashlib
import json
import pathlib

project = pathlib.Path(__file__).resolve().parent
root = project.parents[1]
input_path = project / 'mock/input.json'
candidate_path = project / 'mock/candidate.json'
inputs = json.loads(input_path.read_text())
candidate = json.loads(candidate_path.read_text())
paths = [(project / 'mock/qa.md', 'qa'), (project / 'mock/schema.sql', 'ddl'), (project / 'pom.xml', 'config')]
paths += [(path, 'code') for path in sorted((project / 'src/main').rglob('*')) if path.is_file()]
paths += [(path, 'test') for path in sorted((project / 'src/test').rglob('*.java'))]
inputs['sources'] = [{'path': path.relative_to(root).as_posix(), 'kind': kind,
                      'sha256': hashlib.sha256(path.read_bytes()).hexdigest()} for path, kind in paths]
assert inputs['qa'] == candidate['qa_contract'], 'Review changed QA explicitly before refreshing evidence'
assert inputs['qa']['natural_language'] == (project / 'mock/qa.md').read_text(), 'QA changed: ask the Coding Agent to review the scenario'
candidate['evidence'] = inputs['sources']
for path, value in [(input_path, inputs), (candidate_path, candidate)]:
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
print('Refreshed evidence; QA, SQL, domains and assertions are unchanged.')
