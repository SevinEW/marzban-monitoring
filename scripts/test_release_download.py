"""Hermetic Bash regressions: no network, systemd, or installation changes."""
import os, shutil, subprocess, tempfile, unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
BASH = os.environ.get('MARZWATCH_TEST_BASH') or shutil.which('bash')
if not BASH:
    raise SystemExit('Bash is required for release download regression tests')
START = '# BEGIN resilient release download'
END = '# END resilient release download.'

def source(name):
    return (ROOT/'scripts'/name).read_text(encoding='utf-8-sig')

def helper(name):
    s=source(name)
    return s[s.index(START):s.index(END)+len(END)]

MOCK = r'''
curl() {
  printf '%s\n' "$*" >> "$TRACE/args"
  local n out='' rc http
  n=$(cat "$TRACE/count"); n=$((n+1)); printf '%s' "$n" > "$TRACE/count"
  while [[ $# -gt 0 ]]; do
    case "$1" in --output) out="$2"; shift 2;; *) shift;; esac
  done
  rc=$(sed -n "${n}p" "$TRACE/codes")
  http=$(sed -n "${n}p" "$TRACE/http")
  if [[ "${EMPTY_FIRST:-0}" != 1 || "$n" != 1 ]]; then printf 'attempt-%s' "$n" > "$out"; fi
  printf '%s' "$http"
  return "$rc"
}
sleep() { printf '%s\n' "$1" >> "$TRACE/delays"; }
'''

class DownloadTests(unittest.TestCase):
    def run_script(self, script, folder, extra=None):
        p=folder/'run.sh';p.write_bytes(script.encode())
        env=os.environ.copy();env['TRACE']=str(folder).replace('\\','/')
        env.update(extra or {})
        return subprocess.run([BASH,str(p).replace('\\','/')],env=env,text=True,encoding="utf-8",capture_output=True,timeout=15)

    def test_standalone_helpers_match(self):
        self.assertEqual(helper('install.sh'),helper('auto-update.sh'))

    def test_download_failures_and_fallbacks(self):
        scenarios=[
            ('success',[0],['200'],1,True),
            ('TLS handshake',[35,0],['000','200'],2,True),
            ('IPv6 connection',[7,0],['000','200'],2,True),
            ('timeout then TLS',[28,35,0],['000','000','200'],3,True),
            ('partial transfer',[18,0],['200','200'],2,True),
            ('empty response',[0,0],['200','200'],2,True),
            ('TLS retry exhausted',[35]*4,['000']*4,4,False),
            ('certificate failure',[60],['000'],1,False),
            ('access denied',[22],['403'],1,False),
            ('server unavailable',[22,0],['503','200'],2,True),
            ('release replacement gap',[22,0],['404','200'],2,True),
        ]
        for name in ['install.sh','auto-update.sh']:
            for scenario,codes,http,calls,success in scenarios:
                with self.subTest(script=name,case=scenario),tempfile.TemporaryDirectory() as td:
                    d=Path(td);(d/'count').write_text('0');(d/'codes').write_text('\n'.join(map(str,codes)));(d/'http').write_text('\n'.join(http));(d/'output').write_text('ORIGINAL')
                    script='set -Eeuo pipefail\n'+helper(name)+'\n'+MOCK+'\ndownload_release_file https://github.com/example/release "$TRACE/output" 120\n'
                    r=self.run_script(script,d,{'EMPTY_FIRST':'1' if scenario=='empty response' else '0'})
                    self.assertEqual(r.returncode==0,success,r.stderr)
                    self.assertEqual(int((d/'count').read_text()),calls)
                    self.assertEqual((d/'output').read_text(),f'attempt-{calls}' if success else 'ORIGINAL')
                    self.assertEqual(list(d.glob('output.part.*')),[])
                    args=(d/'args').read_text().splitlines()
                    self.assertTrue(all(x.startswith('-q ') and '--proto =https --proto-redir =https' in x for x in args))
                    self.assertTrue(all('--insecure' not in x and ' -k ' not in x for x in args))
                    if calls>=2:self.assertIn('-4 --http1.1',args[1])
                    if calls>=3:self.assertIn('--http1.1',args[2]);self.assertNotIn(' -4 ',args[2])

    def test_reinstall_preserves_installation_until_download_verified(self):
        prefix=source('install.sh').split('\nrequire_root\nbanner\n')[0]
        for download_ok,expected in [(False,['download']),(True,['download','backup','cleanup'])]:
            with self.subTest(verified=download_ok),tempfile.TemporaryDirectory() as td:
                d=Path(td)
                script=prefix+r'''
is_installed() { return 0; }
backup_existing() { echo backup >> "$TRACE/events"; }
clean_marzwatch() { echo cleanup >> "$TRACE/events"; return 99; }
download_verified_binary() { echo download >> "$TRACE/events"; return DOWNLOAD_RESULT; }
fresh_install 2 <<< y
'''.replace('DOWNLOAD_RESULT','0' if download_ok else '35')
                r=self.run_script(script,d)
                self.assertNotEqual(r.returncode,0)
                self.assertEqual((d/'events').read_text().splitlines(),expected)

    def test_checksum_mismatch_still_rejected(self):
        prefix=source('install.sh').split('\nrequire_root\nbanner\n')[0]
        with tempfile.TemporaryDirectory() as td:
            script=prefix+r'''
detect_asset() { ASSET=test-asset; }
download_release_file() {
 if [[ "$1" == */SHA256SUMS ]]; then printf '%064d  test-asset\n' 0 > "$2"; else printf 'wrong-payload' > "$2"; fi
}
download_verified_binary
'''
            r=self.run_script(script,Path(td));self.assertNotEqual(r.returncode,0);self.assertIn('SHA256 verification failed',r.stdout)

if __name__=='__main__':unittest.main(verbosity=2)
