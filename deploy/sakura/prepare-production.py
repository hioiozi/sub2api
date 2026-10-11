"""Stage an immutable image and rollback files; never restart production."""
import argparse
import hashlib
import json
import subprocess
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('binary', type=Path)
parser.add_argument('--host', default='sub2new')
args = parser.parse_args()
digest = hashlib.sha256(args.binary.read_bytes()).hexdigest()
release = '/data/sub2api-deploy/releases/sakura-' + digest[:12]
tag = 'gateway/sub2api-sakura:production-' + digest[:12]

def remote(script):
    result = subprocess.run(['ssh', '-o', 'BatchMode=yes', args.host, 'python3', '-'],
                            input=script, text=True, encoding='utf-8', capture_output=True)
    if result.returncode:
        raise RuntimeError(result.stderr[-1500:])
    return result.stdout

remote(f'from pathlib import Path\nPath({release!r}).mkdir(parents=True, exist_ok=True)')
subprocess.run(['scp', '-C', '-q', str(args.binary.resolve()), args.host + ':' + release + '/sub2api'], check=True)
script = r"""
import hashlib,json,subprocess
from pathlib import Path
root=Path('/data/sub2api-deploy'); release=Path(RELEASE)
assert hashlib.sha256((release/'sub2api').read_bytes()).hexdigest()==DIGEST
commands=[]
def run(argv):
    r=subprocess.run(argv,cwd=root,text=True,capture_output=True)
    commands.append({'command':argv,'exit_status':r.returncode,'stdout':r.stdout,'stderr':r.stderr})
    if r.returncode: raise RuntimeError(r.stderr[-1500:])
    return r.stdout.strip()
before=json.loads(run(['docker','inspect','sub2api']))[0]
service=before['Config']['Labels']['com.docker.compose.service']
assert service=='sub2api'
old=before['Config']['Image']
assert '@sha256:' in old, 'Rollback image must be immutable'
(release/'Dockerfile').write_text(DOCKERFILE)
run(['docker','build','-t',TAG,str(release)])
image_id=run(['docker','image','inspect',TAG,'--format','{{.Id}}'])
(release/'image.yml').write_text('services:\n  sub2api:\n    image: '+image_id+'\n')
(release/'rollback.yml').write_text('services:\n  sub2api:\n    image: '+old+'\n')
# Existing compose/environment files stay unchanged. CF settings are prepared separately.
rollback='''#!/bin/sh
set -eu
release_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd /data/sub2api-deploy
docker compose -f docker-compose.yml -f "$release_dir/rollback.yml" config --quiet
docker compose -f docker-compose.yml -f "$release_dir/rollback.yml" up -d --no-deps --wait --wait-timeout 240 sub2api
docker inspect sub2api --format '{{.State.Health.Status}}'
'''
(release/'rollback.sh').write_text(rollback); (release/'rollback.sh').chmod(0o700)
run(['sh','-n',str(release/'rollback.sh')])
for file in ('image.yml','rollback.yml'):
    run(['docker','compose','-f','docker-compose.yml','-f',str(release/file),'config','--quiet'])
after=json.loads(run(['docker','inspect','sub2api']))[0]
assert after['Id']==before['Id'] and after['Config']['Image']==old
report={'result':'PREPARED','production_changed':False,'binary_sha256':DIGEST,'image':TAG,'image_id':image_id,'rollback_image':old,'release':str(release),'commands': [c for c in commands if c['command'][:2] != ['docker','inspect']]}
(release/'preparation.json').write_text(json.dumps(report,indent=2))
print(json.dumps(report))
"""
script = script.replace('RELEASE', repr(release)).replace('DIGEST', repr(digest)).replace('DOCKERFILE', repr(Path(__file__).with_name('Dockerfile').read_text())).replace('TAG', repr(tag))
compile(script, '<production-prepare>', 'exec')
report = json.loads(remote(script))
args.binary.with_name('production-preparation.json').write_text(json.dumps(report, indent=2), encoding='utf-8')
print(json.dumps({key: value for key, value in report.items() if key != 'commands'}, ensure_ascii=False))
