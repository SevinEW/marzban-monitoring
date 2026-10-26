import copy
import unittest
from unittest.mock import Mock, patch
import recover_pasarguard_presence as r


class RecoveryTest(unittest.TestCase):
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


if __name__ == '__main__': unittest.main()
