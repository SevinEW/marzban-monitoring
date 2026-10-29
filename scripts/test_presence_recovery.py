import copy
import os
import tempfile
import unittest
from unittest.mock import Mock, patch
import recover_pasarguard_presence as r
from presence_source import minimal_source,source_slot


class RecoveryTest(unittest.TestCase):
    def test_never_connected_account_is_absent_not_a_failed_counter_lookup(self):
        # Xray's per-account GetStatsOnline RPC errors if that counter has never
        # existed. The bulk API represents this state accurately as an empty set.
        with patch.object(r, 'run', return_value='{"users": []}') as run:
            self.assertEqual(r.online_names('/proc/42/exe', '127.0.0.1:1234'), set())
        self.assertEqual(run.call_args.args[0][2], 'statsgetallonlineusers')
        with patch.object(r, 'run', return_value='{"users": "invalid"}'):
            with self.assertRaises(r.Unverified): r.online_names('/proc/42/exe', '127.0.0.1:1234')

    def setUp(self):
        self.profile = dict(node_name='Master', node_address='192.0.2.1', core_config_id=5, probe_inbound='probe')
        self.source = dict(name='Master', address='192.0.2.1', core_id=5, config={
            'policy': {'levels': {'0': {'statsUserOnline': True}}},
            'inbounds': [{'tag': 'probe', 'port': 208, 'protocol': 'vless', 'settings': {'clients': []},
                          'streamSettings': {'network': 'ws'}}]})
        self.binding = dict(schema=1, boot_id='boot-a', namespace='0123456789abcdef', tracking_verified=True,
                            cores=[dict(address='127.0.0.1:1234', pid=42, start_ticks='100')])
        self.state = dict(boot_id='boot-a', binding_digest=r.digest(self.binding))

    def test_fast_path_only_reads_process_metadata(self):
        read = Mock(return_value={'start_ticks': '100'})
        with patch.object(r, 'run', side_effect=AssertionError('no commands on fast path')):
            self.assertTrue(r.still_bound(self.binding, self.state, 'boot-a', read))
        read.assert_called_once_with(42)

    def test_reboot_pid_reuse_and_dead_core_require_new_proof(self):
        for boot, ticks in [('boot-b', '100'), ('boot-a', '101')]:
            self.assertFalse(r.still_bound(self.binding, self.state, boot, lambda _: {'start_ticks': ticks}))
        self.assertFalse(r.still_bound(self.binding, self.state, 'boot-a', Mock(side_effect=FileNotFoundError)))

    def test_external_binding_change_and_legacy_binding_require_proof(self):
        b = copy.deepcopy(self.binding)
        b['cores'][0]['address'] = '127.0.0.1:5678'
        self.assertFalse(r.still_bound(b, self.state, 'boot-a', Mock(return_value={'start_ticks': '100'})))
        b.pop('boot_id')
        self.assertFalse(r.still_bound(b, self.state, 'boot-a'))

    def test_disabled_or_mixed_policy_is_not_false_zero(self):
        self.assertEqual(r.validate_policy(self.source, self.profile)['tag'], 'probe')
        variants = []
        a = copy.deepcopy(self.source); a['config']['policy']['levels']['0']['statsUserOnline'] = False; variants.append(a)
        a = copy.deepcopy(self.source); a['config']['policy']['levels']['1'] = {'statsUserOnline': True}; variants.append(a)
        a = copy.deepcopy(self.source); a['config']['inbounds'][0]['settings']['clients'] = [{'level': 1}]; variants.append(a)
        a = copy.deepcopy(self.source); a['core_id'] = 1; variants.append(a)
        a = copy.deepcopy(self.source); a['config']['inbounds'][0]['streamSettings']['security'] = 'tls'; variants.append(a)
        for source in variants:
            with self.assertRaises(r.Unverified): r.validate_policy(source, self.profile)

    def test_api_rotation_discovers_only_owned_non_inbound_candidates(self):
        path = Mock()
        ports = {('127.0.0.1', 208), ('0.0.0.0', 9000), ('127.0.0.1', 30000), ('127.0.0.1', 40000)}
        with patch.object(r, 'run', side_effect=['{"status":"unavailable"}', '{"status":"ok"}']) as run:
            b = r.discover_api(self.binding, 'boot-b', {'pid': 43, 'start_ticks': '1'}, path, ports, {208})
        self.assertEqual(run.call_count, 2)
        self.assertEqual(b['cores'][0]['address'], '127.0.0.1:40000')
        self.assertEqual(b['boot_id'], 'boot-b')
        self.assertEqual(b['namespace'], self.binding['namespace'])

    def test_ambiguous_api_does_not_bind(self):
        with patch.object(r, 'run', return_value='{"status":"ok"}'):
            with self.assertRaises(r.Unverified):
                r.discover_api(self.binding, 'boot-b', {'pid': 43, 'start_ticks': '1'}, Mock(),
                               {('127.0.0.1', 1234), ('127.0.0.1', 5678)}, set())

    def test_non_api_timeout_does_not_hide_active_native_accounts(self):
        with patch.object(r, 'run', side_effect=[r.Unverified('source_timeout:marzwatch_presence-check'),
                                              '{"status":"ok","users":7}']):
            b = r.discover_api(self.binding, 'boot-b', {'pid': 43, 'start_ticks': '1'}, Mock(),
                               {('127.0.0.1', 1234), ('127.0.0.1', 5678)}, set())
        self.assertEqual(b['verification_method'], 'native_active_accounts')
        self.assertEqual(b['cores'][0]['address'], '127.0.0.1:5678')

    def test_empty_native_api_still_requires_a_real_flow_proof(self):
        with patch.object(r, 'run', return_value='{"status":"ok","users":0}'):
            b = r.discover_api(self.binding, 'boot-b', {'pid': 43, 'start_ticks': '1'}, Mock(),
                               {('127.0.0.1', 1234)}, set())
        self.assertNotIn('verification_method', b)

    def test_remote_plan_never_exports_account_or_tls_secrets(self):
        source=copy.deepcopy(self.source)
        source['candidates']=[1,2,3,4,5,6]
        source['config']['inbounds'][0]['settings']['clients']=[{'level':0,'id':'private-client-uuid','email':'private-email'}]
        source['config']['inbounds'][0]['streamSettings']['tlsSettings']={'privateKey':'private-key'}
        source['config']['outbounds']=[{'password':'private-password'}]
        source['other_secret']='private-token'
        result=minimal_source(source,'probe')
        self.assertNotIn('private-',str(result))
        self.assertEqual(result['candidates'],[1,2,3,4,5])
        self.assertEqual(r.validate_policy(result,self.profile)['port'],208)

    def test_remote_source_uses_pinned_host_and_no_shell_command(self):
        profile=dict(self.profile,panel_ssh={'host':'192.0.2.2','identity_file':'/etc/marzwatch/key','known_hosts_file':'/etc/marzwatch/known_hosts'})
        with patch.object(r,'run',return_value='{}') as run:
            r.read_source(profile)
        args=run.call_args.args[0]
        self.assertIn('StrictHostKeyChecking=yes',args)
        self.assertIn('BatchMode=yes',args)
        self.assertEqual(args[-1],'root@192.0.2.2')
        self.assertEqual(r.probe_destination(dict(profile,probe_destination='192.0.2.2')),'192.0.2.2')
        with self.assertRaises(r.Unverified):r.probe_destination(dict(profile,probe_destination='192.0.2.3'))

    @unittest.skipUnless(os.name=='posix','Linux source concurrency guard')
    def test_fleet_reboot_limits_concurrent_source_reads(self):
        with tempfile.TemporaryDirectory() as d:
            with source_slot(d):
                with source_slot(d):
                    with self.assertRaises(r.Unverified):source_slot(d)
                with source_slot(d):pass

    def test_docker_metadata_is_bounded_read_only_local_socket(self):
        for path in ['/containers/node/restart','/containers/node/stop','http://external/containers/json','/containers/../json']:
            with self.assertRaises(r.Unverified):r.docker_json(path)
        response=Mock();response.status=200;response.read.return_value=b'{"State":{"Running":true}}'
        with patch.object(r.http.client.HTTPConnection,'request') as request,patch.object(r.http.client.HTTPConnection,'getresponse',return_value=response):
            self.assertTrue(r.docker_json('/containers/node/json')['State']['Running'])
            request.assert_called_once_with('GET','/containers/node/json')
        response.status=503
        with patch.object(r.http.client.HTTPConnection,'request'),patch.object(r.http.client.HTTPConnection,'getresponse',return_value=response):
            with self.assertRaises(r.Unverified):r.docker_json('/containers/json')


if __name__ == '__main__': unittest.main()
