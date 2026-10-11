import importlib.util
from pathlib import Path
import tempfile
import unittest
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

if __name__=='__main__':unittest.main()
