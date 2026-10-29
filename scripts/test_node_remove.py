import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch, Mock
import node_remove as n


class RemovalTests(unittest.TestCase):
    def test_fixed_scope_never_contains_vpn_or_host_services(self):
        self.assertTrue(all(x.startswith('marzwatch') for x in n.UNITS))
        self.assertTrue(all('marzwatch' in str(x) for x in n.targets()))
        for protected in ['/opt/pasarguard', '/etc/docker', '/etc/wireguard', '/swapfile']:
            self.assertFalse(any(protected.startswith(str(x)+'/') or protected == str(x) for x in n.targets()))

    def test_central_is_protected_before_any_action(self):
        with patch('os.geteuid', return_value=0, create=True), patch('presence_fleet.root_json', return_value={'role': 'central'}), patch.object(n, 'exchange') as api, patch.object(n, 'cleanup') as cleanup:
            with self.assertRaisesRegex(RuntimeError, 'Central'): n.run()
            api.assert_not_called(); cleanup.assert_not_called()

    def test_signal_is_not_authority_and_start_is_fixed(self):
        with patch.object(n, 'request_nonce', return_value='a'*32), patch.object(n.subprocess, 'run') as execute:
            self.assertTrue(n.maybe_start())
            self.assertEqual(execute.call_args.args[0], ['systemctl','start','--no-block','marzwatch-uninstall.service'])

    def test_tls_pin_is_verified_before_sending_credentials(self):
        conn = Mock(); conn.sock.getpeercert.return_value=b'wrong cert'
        with patch('presence_fleet.peer_host', return_value=('127.0.0.1',443)), patch.object(n.http.client,'HTTPSConnection',return_value=conn):
            with self.assertRaisesRegex(RuntimeError, 'certificate mismatch'):
                n.exchange({'cert_fingerprint':'a'*64},{'node_id':'nodeA','node_secret':'secret'},'b'*32,'poll')
        conn.request.assert_not_called();conn.close.assert_called_once()

    def test_rejects_expired_or_cross_node_plan(self):
        import hashlib
        conn=Mock();conn.sock.getpeercert.return_value=b'cert'
        response=conn.getresponse.return_value;response.status=200
        cfg={'cert_fingerprint':hashlib.sha256(b'cert').hexdigest()}
        base={'action':'uninstall-marzwatch','node_id':'nodeA','nonce':'b'*32,'status':'pending','expires':n.time.time()+120}
        for change in [{'node_id':'nodeB'},{'nonce':'c'*32},{'expires':0},{'action':'shell'},{'status':'removed'}]:
            response.read.return_value=json.dumps(dict(base,**change)).encode()
            with patch('presence_fleet.peer_host',return_value=('127.0.0.1',443)),patch.object(n.http.client,'HTTPSConnection',return_value=conn):
                with self.assertRaises(RuntimeError):n.exchange(cfg,{'node_id':'nodeA','node_secret':'secret'},'b'*32,'poll')

    def test_cleanup_failure_does_not_touch_files(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'owned';p.write_text('keep')
            with patch.object(n.subprocess,'run',return_value=Mock(stdout='active')):
                with self.assertRaisesRegex(RuntimeError,'still active'):n.cleanup([p])
            self.assertEqual(p.read_text(),'keep')

    def test_symlink_cleanup_does_not_follow_target(self):
        import os
        if os.name!='posix':self.skipTest('POSIX filesystem safety test')
        with tempfile.TemporaryDirectory() as d:
            base=Path(d);outside=base/'vpn';outside.mkdir();(outside/'keep').write_text('safe')
            link=base/'monitor';link.symlink_to(outside,target_is_directory=True)
            with patch.object(n.subprocess,'run',return_value=Mock(stdout='inactive')),patch('pwd.getpwnam',side_effect=KeyError),patch('grp.getgrnam',side_effect=KeyError):n.cleanup([link])
            self.assertFalse(link.is_symlink());self.assertEqual((outside/'keep').read_text(),'safe')


if __name__=='__main__':unittest.main()
