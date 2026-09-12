#!/usr/bin/env python3
import pathlib,shutil,sys
root=pathlib.Path(__file__).resolve().parents[1]
name=sys.argv[1]
if name not in ('codex','claude'):raise SystemExit('host must be codex or claude')
target=root/'artifacts/p0/hosts'/name
if target.exists():raise SystemExit('host project already exists; preserve previous acceptance evidence')
shutil.copytree(root/'examples/springboot-shop',target/'examples/springboot-shop',ignore=shutil.ignore_patterns('target'))
(target/'examples/scenarios').mkdir(parents=True)
shutil.copy2(root/'examples/scenarios/shop.qa.md',target/'examples/scenarios/shop.qa.md')
shutil.copy2(root/'examples/x-mock.yaml',target/'x-mock.yaml')
(target/'.tools').mkdir();(target/'.tools/playwright-browsers').symlink_to(root/'.tools/playwright-browsers')
(target/'acceptance').mkdir();shutil.copy2(root/'scripts/host-acceptance-prompt.md',target/'acceptance/prompt.md')
(target/'AGENTS.md').write_text('受测 MCP 客户端项目。只执行 acceptance/prompt.md 的宿主验收；不要开发主项目，不启用子代理，不修改 QA/应用/测试。所有测试资源都是本地合成数据。\n')
print(target)
