import copy
import hashlib
import json
import time
import tempfile
from pathlib import Path
import unittest
from unittest.mock import Mock, patch
import presence_fleet as f
import recover_pasarguard_presence as r


class FleetTest(unittest.TestCase):
    def setUp(self):
        self.cfg={'central_url':'https://192.0.2.1:28443','cert_fingerprint':hashlib.sha256(b'certificate').hexdigest()}
        self.ident={'node_id':'node-a','node_secret':'private-secret'}
        self.inv={'node_container':'node','node_image_id':'sha256:image','xray_sha256':'a'*64}
        source={'name':'Node A','address':'192.0.2.2','core_id':7,'namespace':'private-namespace',
                'config':{'policy':{'levels':{'0':{'statsUserOnline':True}}},'inbounds':[
                    {'tag':'probe','port':208,'protocol':'vless','settings':{'clients':[]},'streamSettings':{'network':'ws'}}]}}
        self.plan={'schema':1,'monitor_id':'node-a','expires':time.time()+600,
                   'profile':dict(self.inv,node_name='Node A',node_address='192.0.2.2',node_id=1,core_config_id=7,
                                  probe_inbound='probe',https_host='example.test'), 'source':source}

    def test_no_credentials_sent_before_certificate_pin_matches(self):
        conn=Mock();conn.sock.getpeercert.return_value=b'impostor'
        with patch.object(f.http.client,'HTTPSConnection',return_value=conn):
            with self.assertRaises(r.Unverified):f.exchange(self.cfg,self.ident,{'status':'discovered'})
        conn.request.assert_not_called();conn.close.assert_called_once()

    def test_valid_pin_uses_identity_and_domain_separated_auth(self):
        conn=Mock();conn.sock.getpeercert.return_value=b'certificate'
        conn.getresponse.return_value.status=204;conn.getresponse.return_value.read.return_value=b''
        with patch.object(f.http.client,'HTTPSConnection',return_value=conn):
            self.assertIsNone(f.exchange(self.cfg,self.ident,{'status':'discovered'}))
        args=conn.request.call_args
        self.assertEqual(args.args[:2],('POST','/api/v1/presence-enroll'))
        self.assertEqual(args.kwargs['headers']['X-MW-Node'],'node-a')
        self.assertNotIn('private-secret',str(args))

    def test_plan_is_per_node_fresh_and_matches_local_runtime(self):
        self.assertEqual(f.approved_profile(self.plan,self.ident,self.inv,self.cfg)['probe_destination'],'192.0.2.1')
        for kind in ['node','expired','runtime','policy']:
            p=copy.deepcopy(self.plan)
            if kind=='node':p['monitor_id']='node-b'
            if kind=='expired':p['expires']=time.time()-1
            if kind=='runtime':p['profile']['xray_sha256']='b'*64
            if kind=='policy':p['source']['config']['policy']['levels']['0']['statsUserOnline']=False
            with self.subTest(kind=kind),self.assertRaises(r.Unverified):f.approved_profile(p,self.ident,self.inv,self.cfg)

    def test_remote_plan_cannot_inject_commands_paths_or_destination(self):
        self.plan['profile'].update(panel_ssh={'host':'192.0.2.99'},probe_destination='192.0.2.99',
                                    command='untrusted',identity_file='/root/key')
        p=f.approved_profile(self.plan,self.ident,self.inv,self.cfg)
        self.assertNotIn('panel_ssh',p);self.assertNotIn('command',p);self.assertNotIn('identity_file',p)
        self.assertEqual(p['probe_destination'],'192.0.2.1')

    def test_cached_container_rebind_avoids_docker_and_rejects_parent_reuse(self):
        cached=dict(self.inv, parent=20,parent_start_ticks='100',boot_id='boot')
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);(root/'sys/kernel/random').mkdir(parents=True)
            (root/'sys/kernel/random/boot_id').write_text('boot')
            (root/'20/task/20').mkdir(parents=True);(root/'20/task/20/children').write_text('42')
            (root/'42').mkdir();(root/'42/comm').write_text('xray')
            def fake_path(path): return root if str(path)=='/proc' else root/str(path).removeprefix('/proc/')
            def proc(pid): return dict(pid=pid,start_ticks='100' if pid==20 else '200',parent=1 if pid==20 else 20)
            with patch.object(f,'Path',side_effect=fake_path),patch.object(r,'process',side_effect=proc),patch.object(r,'file_digest',return_value='a'*64),patch.object(r,'run',side_effect=AssertionError('Docker must not run')):
                got=f.inventory(cached)
                self.assertEqual(got['pid'],42);self.assertEqual(got['start_ticks'],'200')
                self.assertIsNone(f.cached_runtime(dict(cached,parent_start_ticks='99')))
                self.assertIsNone(f.cached_runtime(dict(cached,boot_id='other-boot')))

    def test_transient_recovery_backoff_is_bounded_not_every_sample(self):
        for reason in ['source_timeout:docker_ps','runtime_not_approved','ambiguous_running_core']:
            self.assertEqual([f.retry_delay(reason,n) for n in range(1,7)],[30,60,120,240,300,300])
        self.assertEqual(f.retry_delay('runtime_tracking_unverified',1),300)


if __name__=='__main__':unittest.main()
