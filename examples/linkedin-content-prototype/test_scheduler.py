import concurrent.futures
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path
from scheduler import Scheduler

class SchedulerTest(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(); self.db=Path(self.tmp.name)/'tasks.db'; self.s=Scheduler(self.db)
 def tearDown(self): self.tmp.cleanup()
 def test_local_daily_and_pause(self):
  sid=self.s.create('09:00','Asia/Shanghai','Draft only')
  t=datetime(2026,11,1,0,59,tzinfo=timezone.utc)
  self.assertEqual([],self.s.claim_due(t))
  t=datetime(2026,11,1,1,0,tzinfo=timezone.utc)
  run=self.s.claim_due(t)[0]; self.assertEqual('2026-11-01',run['local_date'])
  self.assertEqual([],self.s.claim_due(t))
  self.s.complete(run['run_id'],result={'text':'draft'})
  self.assertEqual('awaiting_review',self.s.list_runs()[0]['status'])
  self.s.set_enabled(sid,False)
  self.assertEqual([],self.s.claim_due(datetime(2026,11,2,2,tzinfo=timezone.utc)))
 def test_concurrent_claims_only_one_run(self):
  self.s.create('09:00','Asia/Shanghai','Draft only')
  t=datetime(2026,11,1,2,tzinfo=timezone.utc)
  with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
   results=list(pool.map(lambda _:Scheduler(self.db).claim_due(t),range(8)))
  self.assertEqual(1,sum(map(len,results)))
 def test_no_automatic_retry_after_crash(self):
  self.s.create('09:00','Asia/Shanghai','Draft only')
  self.s.claim_due(datetime(2026,11,1,1,tzinfo=timezone.utc))
  self.assertEqual([],self.s.claim_due(datetime(2026,11,1,3,tzinfo=timezone.utc)))
 def test_invalid_input(self):
  with self.assertRaises(ValueError): self.s.create('25:00','UTC','draft')
  with self.assertRaises(ValueError): self.s.create('09:00','UTC',' ')
  with self.assertRaises(ValueError): self.s.claim_due(datetime(2026,11,1))
if __name__=='__main__': unittest.main()
