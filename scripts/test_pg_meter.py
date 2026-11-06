import importlib.util
from pathlib import Path
import tempfile
import unittest
from types import SimpleNamespace
spec=importlib.util.spec_from_file_location('meter',Path(__file__).with_name('pg_meter.py'))
meter=importlib.util.module_from_spec(spec);spec.loader.exec_module(meter)

class LedgerTest(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.path=str(Path(self.tmp.name)/'meter.db');self.l=meter.Ledger(self.path)
 def tearDown(self):self.l.db.close();self.tmp.cleanup()
 def sample(self,t,v):self.l.sample(1,t,{'vless|downlink':v,'vless|uplink':v//2})
 def test_window_ages_out_at_same_minute_not_midnight(self):
  self.sample(100000,1000);self.sample(100010,2000);self.sample(100020,4000)
  self.assertEqual(self.l.totals(100020)['1']['rx'],3000)
  self.assertEqual(self.l.totals(186415)['1']['rx'],1000)
  self.assertEqual(self.l.totals(186420)['1']['rx'],0)
 def test_restart_and_duplicate(self):
  self.sample(100000,1000);self.sample(100010,2000);self.l.commit(100010)
  self.l.db.close();self.l=meter.Ledger(self.path);self.sample(100010,2000);self.sample(100020,3000)
  self.assertEqual(self.l.totals(100020)['1']['rx'],2000)
 def test_new_baseline_reset_gap_and_partial(self):
  self.sample(100000,10**12);self.assertEqual(self.l.totals(100000)['1']['rx'],0)
  self.sample(100010,100);self.sample(100020,200)
  value=self.l.totals(100020)['1'];self.assertEqual(value['rx'],100);self.assertFalse(value['complete'])
 def test_retention_is_bounded(self):
  self.sample(100000,1000);self.sample(100010,2000);self.l.commit(200000)
  self.assertEqual(self.l.db.execute('SELECT COUNT(*) FROM intervals').fetchone()[0],0)
 def test_uptime_detects_reset_even_with_larger_counter(self):
  self.l.sample(1,100000,{'a|downlink':100},1000)
  self.l.sample(1,100010,{'a|downlink':200},5)
  self.assertEqual(self.l.totals(100010)['1']['rx'],0)
  self.l.sample(1,100020,{'a|downlink':250},15)
  self.assertEqual(self.l.totals(100020)['1']['rx'],50)
 def test_uptime_normal_progress_and_independent_nodes(self):
  self.l.sample(1,100000,{'a|downlink':100},1000)
  self.l.sample(1,100010,{'a|downlink':200},1010)
  self.l.sample(2,100000,{'a|downlink':10000},500)
  self.l.sample(2,100010,{'a|downlink':10300},510)
  self.assertEqual(self.l.totals(100010)['1']['rx'],100)
  self.assertEqual(self.l.totals(100010)['2']['rx'],300)
 def test_gap_never_claims_full_coverage(self):
  self.sample(100000,100);self.sample(190000,500)
  self.assertFalse(self.l.totals(190000)['1']['complete'])
 def test_changed_inbounds_do_not_add_lifetime_values(self):
  self.l.sample(1,100000,{'a|uplink':100})
  self.l.sample(1,100010,{'a|uplink':200,'b|uplink':10**12})
  self.assertEqual(self.l.totals(100010)['1']['tx'],100)
 def test_retired_node_history_ages_out_instead_of_disappearing(self):
  self.sample(100000,1000);self.sample(100010,2000)
  snap=meter.snapshot(self.l,{},100010)
  self.assertEqual(snap['nodes']['1']['rx'],1000)
  self.assertFalse(snap['nodes']['1']['active'])
  self.l.commit(200000)
  self.assertEqual(meter.snapshot(self.l,{},200000)['nodes'],{})
 def test_short_resume_keeps_counter_proven_bytes_and_coverage(self):
  self.l.sample(1,200000,{'a|downlink':100,'a|uplink':50},100000)
  self.l.db.execute('UPDATE nodes SET first=100000,gap=100000 WHERE id=1')
  self.l.sample(1,200035,{'a|downlink':300,'a|uplink':150},100035)
  v=self.l.totals(200035)['1'];self.assertTrue(v['complete']);self.assertEqual((v['rx'],v['tx']),(200,100))
 def test_only_complete_client_inbound_counters_are_accepted(self):
  config={'inbounds':[{'tag':'vpn','protocol':'vless'},{'tag':'api','protocol':'dokodemo-door'}]}
  tags=meter.accounting_tags(config);self.assertEqual(tags,{'vpn'})
  rows=[SimpleNamespace(name='vpn',type=d,value=v) for d,v in [('uplink',100),('downlink',200)]]
  self.assertEqual(sum(meter.validated_counters(rows,tags).values()),300)
  with self.assertRaises(ValueError):meter.validated_counters(rows[:1],tags)
  with self.assertRaises(ValueError):meter.validated_counters(rows+rows[:1],tags)
  self.assertEqual(sum(meter.validated_counters(rows+[SimpleNamespace(name='api',type='uplink',value=99999)],tags).values()),300)

if __name__=='__main__':unittest.main()
