import copy
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import presence_auto as a
import presence_fleet as f
from presence_panel import equivalent_core


class AutoEnrollmentTests(unittest.TestCase):
    def setUp(self):
        self.now = 10000
        self.node = {'id': 20, 'name': 'new', 'address': '192.0.2.20', 'status': 'connected', 'core_id': 1}
        self.core = {'id': 1, 'type': 'xray', 'name': 'shared', 'config': {
            'inbounds': [{'tag': 'Mostaghim', 'port': 208, 'protocol': 'vless',
                          'settings': {'clients': []}, 'streamSettings': {'network': 'ws'}}]},
            'exclude_inbound_tags': [], 'fallbacks_inbound_tags': []}
        self.snapshot = {'nodes': [self.node], 'cores': {'1': self.core}, 'candidates': [1, 2, 3]}
        self.inv = {'node_container': 'node', 'node_image_id': 'sha256:' + 'a'*64,
                    'xray_sha256': 'b'*64, 'pid': 200, 'start_ticks': '12345'}
        self.reports = {'m': {'received': self.now, 'peer_ip': '192.0.2.20',
                              'inventory': self.inv, 'status': 'discovered'}}
        self.monitors = {'m': {'location': {'public_ip': '192.0.2.20'}}}
        self.fleet = {'nodes': {}, 'auto_enroll': {'allow_initial_restart': True, 'probe_host': 'example.test'}}
        self.state = {}
        self.events = []
        self.control = Mock(side_effect=self.operation)

    def save(self):
        self.events.append(('save', copy.deepcopy(self.state), copy.deepcopy(self.fleet)))

    def operation(self, action, entry):
        self.events.append((action, copy.deepcopy(entry)))
        if action == 'prepare':
            return {'target': dict(self.core, id=7, config=a.online_config(self.core['config']))}
        if action == 'activate':
            return {'node': {'id': 20, 'core_config_id': 7, 'status': 'connected'}}
        if action == 'rollback':
            return {'node': {'id': 20, 'core_config_id': 1, 'status': 'connecting'}}
        raise AssertionError(action)

    def run_reconcile(self, now=None, manual=()):
        return a.reconcile(self.fleet, self.snapshot, self.monitors, self.reports, self.state,
                           set(manual), self.control, self.save, self.now if now is None else now)

    def test_disabled_policy_clone_and_single_target_activation(self):
        original = copy.deepcopy(self.core)
        self.assertEqual(self.run_reconcile(), ['m'])
        self.assertEqual(self.core, original, 'shared config mutated')
        self.assertEqual([call.args[0] for call in self.control.call_args_list], ['prepare', 'activate'])
        before_activate = next(i for i, event in enumerate(self.events) if event[0] == 'activate')
        journal = self.events[before_activate-1]
        self.assertEqual(journal[0], 'save')
        self.assertEqual(journal[1]['nodes']['m']['phase'], 'activating')
        self.assertEqual(journal[2]['nodes']['m']['core_config_id'], 7)
        self.control.reset_mock();self.run_reconcile(now=self.now+30);self.control.assert_not_called()

    def test_already_enabled_policy_never_calls_panel_mutations(self):
        self.core['config'] = a.online_config(self.core['config'])
        self.run_reconcile();self.control.assert_not_called()
        self.assertEqual(self.state['nodes']['m']['phase'], 'verifying')
        self.assertFalse(self.state['nodes']['m']['restart_attempted'])

    def test_no_restart_mode_waits_for_online_policy(self):
        self.fleet['auto_enroll']['allow_initial_restart'] = False
        self.run_reconcile();self.control.assert_not_called()
        self.assertEqual(self.state['nodes']['m']['phase'], 'awaiting_online_policy')

    def test_existing_manual_node_follows_ip_and_runtime_without_restart(self):
        self.core['config'] = a.online_config(self.core['config'])
        previous = a.profile_for(self.node, self.core, self.inv, 'Mostaghim', self.fleet['auto_enroll'])
        previous.pop('automatic')
        previous.update(node_address='192.0.2.19', xray_sha256='c'*64)
        self.fleet['nodes']['m'] = previous
        self.run_reconcile(manual=['m'])
        self.control.assert_not_called()
        self.assertEqual(self.fleet['nodes']['m']['node_address'], self.node['address'])
        self.assertEqual(self.fleet['nodes']['m']['xray_sha256'], self.inv['xray_sha256'])
        self.assertNotIn('automatic', self.fleet['nodes']['m'])
        self.assertNotIn('m', self.state['nodes'])
        for variant in ['different_identity', 'disabled_policy', 'disabled_profile']:
            self.node['id'] = 20
            self.core['config'] = a.online_config(self.core['config'])
            self.fleet['nodes']['m']['enabled'] = True
            original = copy.deepcopy(self.fleet['nodes']['m'])
            if variant == 'different_identity': self.node['id'] = 99
            if variant == 'disabled_policy': self.core['config'].pop('policy')
            if variant == 'disabled_profile': self.fleet['nodes']['m']['enabled'] = False
            self.inv['xray_sha256'] = 'd'*64
            self.run_reconcile(manual=['m']);self.control.assert_not_called()
            self.assertEqual(self.fleet['nodes']['m']['xray_sha256'], original['xray_sha256'])

    def test_floating_ip_and_ambiguous_ownership(self):
        self.node['address'] = '192.0.2.90'
        self.inv['addresses'] = ['192.0.2.20', '192.0.2.90']
        self.core['config'] = a.online_config(self.core['config'])
        self.run_reconcile();self.control.assert_not_called()
        self.assertEqual(self.fleet['nodes']['m']['node_address'], '192.0.2.90')
        self.monitors['n'] = {'location': {'public_ip': '192.0.2.21'}}
        self.reports['n'] = dict(self.reports['m'], peer_ip='192.0.2.21')
        self.assertEqual(a.matched_nodes(self.monitors, self.reports, self.snapshot, self.now), {})

    def test_manual_nodes_and_ambiguous_or_forged_matches_never_mutate(self):
        for variant in ['manual', 'peer', 'duplicate_panel', 'duplicate_monitor', 'stale', 'bad_digest', 'missing_inventory']:
            with self.subTest(variant=variant):
                self.setUp()
                if variant == 'peer':self.reports['m']['peer_ip'] = '192.0.2.99'
                if variant == 'duplicate_panel':self.snapshot['nodes'].append(dict(self.node, id=21))
                if variant == 'duplicate_monitor':self.monitors['other'] = copy.deepcopy(self.monitors['m'])
                if variant == 'stale':self.reports['m']['received'] -= 601
                if variant == 'bad_digest':self.inv['xray_sha256'] = 'not a digest'
                if variant == 'missing_inventory':self.reports['m'].pop('inventory')
                self.run_reconcile(manual=['m'] if variant == 'manual' else [])
                self.control.assert_not_called();self.assertEqual(self.fleet['nodes'], {})

    def test_reinstalled_monitor_ignores_only_confirmed_inactive_predecessor(self):
        self.core['config'] = a.online_config(self.core['config'])
        self.monitors['old'] = copy.deepcopy(self.monitors['m'])
        self.monitors['old']['last_contact'] = datetime.fromtimestamp(self.now-601,timezone.utc).isoformat()
        self.reports['old'] = dict(self.reports['m'], received=self.now-601)
        self.fleet['nodes']['old'] = a.profile_for(self.node,self.core,self.inv,'Mostaghim',self.fleet['auto_enroll'])
        self.assertEqual(self.run_reconcile(manual=['old']),['m'])
        self.control.assert_not_called()
        self.assertEqual(a.replaced_scope(self.fleet,self.monitors,self.reports,self.state,self.snapshot,self.now),{'old':'m'})
        self.assertIn('old',self.monitors,'predecessor history must not be deleted')
        for bad in [self.now-600,self.now+1]:
            self.reports['old']['received']=bad
            self.assertEqual(a.matched_nodes(self.monitors,self.reports,self.snapshot,self.now),{})
        self.reports['old']['received']=self.now-601
        self.monitors['old'].pop('last_contact')
        self.assertEqual(a.matched_nodes(self.monitors,self.reports,self.snapshot,self.now),{})

    def test_replacement_scope_never_hides_different_panel_assignment(self):
        self.test_reinstalled_monitor_ignores_only_confirmed_inactive_predecessor()
        self.monitors['old']['last_contact']=datetime.fromtimestamp(self.now-601,timezone.utc).isoformat()
        self.fleet['nodes']['old']['node_id']=99
        self.assertEqual(a.replaced_scope(self.fleet,self.monitors,self.reports,self.state,self.snapshot,self.now),{})

    def test_unsupported_core_is_not_restarted(self):
        self.core['config']['inbounds'][0]['streamSettings']['network'] = 'tcp'
        self.run_reconcile();self.control.assert_not_called()
        self.assertEqual(self.state['nodes']['m']['phase'], 'unsupported_or_unavailable')

    def test_excluded_probe_is_not_activated(self):
        self.core['exclude_inbound_tags'] = ['Mostaghim']
        self.run_reconcile();self.control.assert_not_called()
        self.assertEqual(self.state['nodes']['m']['phase'], 'unsupported_or_unavailable')

    def test_equivalent_core_ignores_only_tag_set_order(self):
        other = copy.deepcopy(self.core)
        self.core['exclude_inbound_tags'] = ['a', 'b']
        other['exclude_inbound_tags'] = ['b', 'a']
        self.assertTrue(equivalent_core(other, self.core))
        other['config']['inbounds'][0]['port'] = 9999
        self.assertFalse(equivalent_core(other, self.core))

    def test_lost_activation_response_never_repeats_put(self):
        def operation(action, entry):
            if action == 'activate':raise TimeoutError()
            return self.operation(action, entry)
        self.control.side_effect = operation
        self.run_reconcile()
        self.assertEqual(self.state['nodes']['m']['phase'], 'activation_unknown')
        self.control.reset_mock();self.run_reconcile(now=self.now+130)
        self.control.assert_not_called();self.assertEqual(self.state['nodes']['m']['phase'], 'assignment_changed')

    def test_runtime_proof_required_then_repeated_scans_are_inert(self):
        self.run_reconcile()
        self.reports['m'].update(status='ready', received=self.now+1, proof={'existing_account_flow': True})
        self.run_reconcile(now=self.now+30)
        self.assertEqual(self.state['nodes']['m']['phase'], 'verifying')
        self.reports['m']['proof']['tls_verified'] = True
        self.run_reconcile(now=self.now+31)
        self.assertEqual(self.state['nodes']['m']['phase'], 'ready')
        self.control.reset_mock()
        self.run_reconcile(now=self.now+50);self.control.assert_not_called()
        # A new local runtime receives a fresh proof without a second restart.
        self.inv['xray_sha256'] = 'c'*64
        self.run_reconcile(now=self.now+60);self.control.assert_not_called()
        self.assertEqual(self.state['nodes']['m']['phase'], 'verifying')
        self.assertEqual(self.fleet['nodes']['m']['xray_sha256'], 'c'*64)

    def test_only_one_new_node_is_activated_per_pass(self):
        self.snapshot['nodes'].append(dict(self.node, id=21, address='192.0.2.21'))
        self.monitors['n'] = {'location': {'public_ip': '192.0.2.21'}}
        self.reports['n'] = dict(self.reports['m'], peer_ip='192.0.2.21')
        self.run_reconcile()
        self.assertEqual(sum(c.args[0] == 'activate' for c in self.control.call_args_list), 1)
        self.control.reset_mock();self.run_reconcile(now=self.now+30)
        self.control.assert_not_called()

    def test_failed_connection_gets_one_guarded_rollback(self):
        self.run_reconcile();self.node['status'] = 'error';self.control.reset_mock()
        self.run_reconcile(now=self.now+181)
        self.assertEqual([c.args[0] for c in self.control.call_args_list], ['rollback'])
        self.assertEqual(self.state['nodes']['m']['phase'], 'rolled_back')
        self.assertNotIn('m', self.fleet['nodes'])
        self.control.reset_mock();self.run_reconcile(now=self.now+200);self.control.assert_not_called()

    def test_idle_ticks_do_not_poll_database_or_rewrite_plans(self):
        with tempfile.TemporaryDirectory() as td:
            root=Path(td);reports=root/'reports';reports.mkdir()
            paths={'STATE':root/'state.json','CACHE':root/'cache.json','SCOPE':root/'scope.json',
                   'REPORTS':reports,'MONITORS':root/'monitors.json'}
            paths['STATE'].write_text(json.dumps({'nodes':{},'last_panel_read':self.now}))
            paths['CACHE'].write_text(json.dumps(self.snapshot))
            paths['SCOPE'].write_text('{"ids":[]}')
            paths['MONITORS'].write_text(json.dumps({'nodes':self.monitors}))
            (reports/'m.json').write_text(json.dumps(self.reports['m']))
            fleet=root/'fleet.json';plans=root/'plans.json';plans.write_text('{}');os.utime(plans,(self.now,self.now))
            self.fleet['auto_enroll']['enabled']=True
            fleet.write_text(json.dumps(self.fleet))
            writes=[]
            def write(path,value,**kwargs):
                writes.append(path);path.write_text(json.dumps(value))
            with patch.multiple(a,**paths),patch.multiple(f,FLEET=fleet,PLANS=plans), \
                 patch.object(f,'root_json',side_effect=lambda p:json.loads(p.read_text())), \
                 patch.object(a.r,'atomic_json',side_effect=write), \
                 patch.object(a,'panel_call',return_value=copy.deepcopy(self.snapshot)) as panel, \
                 patch.object(f,'publish_plans') as publish, \
                 patch.dict(sys.modules,{'grp':SimpleNamespace(getgrnam=lambda name:SimpleNamespace(gr_gid=0))}):
                cfg={'presence_node_ids':['m']}
                for elapsed in [0,30,60,120,240]:
                    with patch.object(a.time,'time',return_value=self.now+elapsed):a.tick(cfg)
                panel.assert_not_called();publish.assert_not_called();self.assertEqual(writes,[])
                with patch.object(a.time,'time',return_value=self.now+301):a.tick(cfg)
                self.assertEqual(panel.call_count,1)
                self.assertEqual(json.loads(paths['STATE'].read_text())['last_panel_read'],self.now+301)
                with patch.object(a.time,'time',return_value=self.now+330):a.tick(cfg)
                self.assertEqual(panel.call_count,1,'snapshot throttle was not persisted')


if __name__ == '__main__':
    unittest.main(verbosity=2)
