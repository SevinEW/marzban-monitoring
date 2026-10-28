import copy
import hashlib
import json
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import Mock, patch
import presence_core_stage as c
import recover_pasarguard_presence as r


class CoreStageTest(unittest.TestCase):
    def setUp(self):
        self.old = c.PREVIOUS_SHA
        self.inv = dict(node_container='node', node_image_id='sha256:image', pid=42,
                        start_ticks='50', xray_sha256=self.old)
        self.plan = dict(monitor_id='one', expires=time.time()+30, profile=self.inv,
                         core_stage=dict(kind=c.KIND, sha256=c.SHA, previous_sha256=self.old))
        self.ident = dict(node_id='one', node_secret='private-test-secret')
        self.cfg = dict(central_url='https://192.0.2.1:28443', cert_fingerprint=hashlib.sha256(b'certificate').hexdigest())

    def test_no_approval_no_action_and_wrong_identity_fails_closed(self):
        with patch.object(c.r, 'run') as run:
            self.assertIsNone(c.stage({}, self.cfg, self.ident, self.inv))
            for field,value in [('monitor_id','other'),('expires',0)]:
                p=copy.deepcopy(self.plan);p[field]=value
                with self.assertRaises(r.Unverified):c.stage(p,self.cfg,self.ident,self.inv)
            p=copy.deepcopy(self.plan);p['core_stage']['sha256']='b'*64
            with self.assertRaises(r.Unverified):c.stage(p,self.cfg,self.ident,self.inv)
            run.assert_not_called()

    def test_invalid_certificate_sends_no_identity_and_writes_no_asset(self):
        con=Mock();con.sock.getpeercert.return_value=b'impostor'
        with tempfile.TemporaryDirectory() as tmp, patch.object(c.http.client,'HTTPSConnection',return_value=con):
            target=Path(tmp)/'xray'
            with self.assertRaises(r.Unverified):c.download(self.cfg,self.ident,target)
            self.assertFalse(target.exists());con.request.assert_not_called()

    def test_staging_is_atomic_backed_up_idempotent_and_never_restarts(self):
        with tempfile.TemporaryDirectory() as tmp:
            state=Path(tmp)/'core-stage.json'
            calls=[]
            info={'State':{'Running':True,'Pid':40},'Image':'sha256:image','Config':{'Env':[]},'Mounts':[]}
            proc=dict(pid=42,start_ticks='50',parent=40)
            replaced=False
            def run(args,**kwargs):
                nonlocal replaced
                calls.append(args)
                if args[:2]==['docker','inspect']:return json.dumps([info])
                if args[:2]==['docker','cp']:
                    if args[2].startswith('node:'):Path(args[3]).write_bytes(b'backup')
                    return ''
                if 'sha256sum' in args:
                    return (c.SHA if replaced or '.marzwatch-' in args[-1] else self.old)+'  xray'
                if args[-1]=='version':return 'Xray 26.10.21 (test) (go1.27 linux/amd64)'
                if 'mv' in args:
                    self.assertTrue(state.exists(),'journal must precede rename')
                    self.assertTrue((state.parent/'core-backups'/self.old).exists())
                    replaced=True
                return ''
            with patch.object(c,'STATE',state),patch.object(c.r,'run',side_effect=run),patch.object(c.r,'process',return_value=proc),patch.object(c.r,'file_digest',return_value=self.old),patch.object(c.r,'atomic_json',side_effect=lambda p,v:p.write_text(json.dumps(v))),patch.object(c,'download',side_effect=lambda cfg,ident,p:p.write_bytes(b'candidate')):
                result=c.stage(self.plan,self.cfg,self.ident,self.inv)
                self.assertEqual(result['status'],'staged')
                count=len(calls);c.stage(self.plan,self.cfg,self.ident,self.inv)
                self.assertEqual(count,len(calls),'staging repeated')
                active=c.stage(self.plan,self.cfg,self.ident,dict(self.inv,xray_sha256=c.SHA))
                self.assertEqual(active['status'],'active')
            self.assertFalse(any(any(v in cmd for v in ['restart','stop','kill','sh','bash']) for cmd in calls))


if __name__=='__main__':unittest.main()
