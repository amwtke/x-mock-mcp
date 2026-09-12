#!/usr/bin/env python3
import hashlib,json,pathlib,platform,subprocess,zipfile
root=pathlib.Path(__file__).resolve().parents[1]
platform_id={'Darwin':'darwin','Linux':'linux'}[platform.system()]+'/'+{'arm64':'arm64','aarch64':'arm64','x86_64':'amd64'}[platform.machine()]
for role,name in (('left','mysql-wire'),('right','mysql-mock')):
 binary=root/'bin'/name
 descriptor=json.loads(subprocess.check_output([str(binary),'--describe']))
 files={'bin/plugin':binary.read_bytes()}
 if role=='right':
  for path in sorted((root/'plugins/right/mysql-mock').glob('*.schema.json')):files[path.name]=path.read_bytes()
  files['qa-guide.md']=(root/'plugins/right/mysql-mock/qa-guide.md').read_bytes()
 manifest={'descriptor':descriptor,'platform':platform_id,'entrypoint':'bin/plugin','files':{path:hashlib.sha256(raw).hexdigest() for path,raw in files.items()}}
 archive=root/'dist'/f'{role}-{name}-{descriptor["ref"]["version"]}-{platform_id.replace("/","-")}.zip'
 with zipfile.ZipFile(archive,'w',compression=zipfile.ZIP_DEFLATED) as output:
  for path,raw in {'manifest.json':(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n').encode(),**files}.items():
   entry=zipfile.ZipInfo(path,date_time=(2026,9,12,0,0,0));entry.compress_type=zipfile.ZIP_DEFLATED;entry.create_system=3;entry.external_attr=(0o100700 if path=='bin/plugin' else 0o100600)<<16;output.writestr(entry,raw)
 digest=hashlib.sha256(archive.read_bytes()).hexdigest();archive.with_suffix('.zip.sha256').write_text(digest+'  '+archive.name+'\n')
 (root/'plugins'/role/name/'manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
 print(archive.name+' '+digest)
