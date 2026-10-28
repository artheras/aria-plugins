"""Persistent daily draft-run coordination; no model or publisher side effects.

Host calls claim_due(), runs its authorized model, then complete(). An expired
lease needs explicit recovery: never automatically rerun potentially costly work.
"""
import json, sqlite3, uuid
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from zoneinfo import ZoneInfo
from pathlib import Path
UTC=timezone.utc
class Scheduler:
 def __init__(self,path):
  self.path=str(path)
  with self.db() as c:
   c.executescript('''CREATE TABLE IF NOT EXISTS schedules(id TEXT PRIMARY KEY, time_local TEXT NOT NULL, timezone TEXT NOT NULL, enabled INTEGER NOT NULL, prompt TEXT NOT NULL, created_at TEXT NOT NULL);
   CREATE TABLE IF NOT EXISTS runs(id TEXT PRIMARY KEY, schedule_id TEXT NOT NULL, local_date TEXT NOT NULL, status TEXT NOT NULL, lease_until TEXT NOT NULL, result TEXT, error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(schedule_id,local_date));''')
 @contextmanager
 def db(self):
  c=sqlite3.connect(self.path,timeout=10); c.row_factory=sqlite3.Row
  try:
   with c: yield c
  finally: c.close()
 def create(self,time_local,zone,prompt):
  ZoneInfo(zone)
  try:
   h,m=map(int,time_local.split(':')); assert 0<=h<24 and 0<=m<60 and len(time_local)==5
  except (ValueError,AssertionError): raise ValueError('Time must be HH:MM')
  if not prompt.strip(): raise ValueError('Prompt required')
  sid=uuid.uuid4().hex
  with self.db() as c: c.execute('INSERT INTO schedules VALUES(?,?,?,?,?,?)',(sid,time_local,zone,1,prompt,datetime.now(UTC).isoformat()))
  return sid
 def claim_due(self,at=None):
  at=at or datetime.now(UTC)
  if at.tzinfo is None: raise ValueError('timezone required')
  claims=[]
  with self.db() as c:
   c.execute('BEGIN IMMEDIATE')
   for s in c.execute('SELECT * FROM schedules WHERE enabled=1').fetchall():
    local=at.astimezone(ZoneInfo(s['timezone'])); date=local.date().isoformat()
    # After the chosen wall time, run once for today's date. No catch-up for prior dates.
    if local.strftime('%H:%M')<s['time_local']: continue
    rid=uuid.uuid4().hex; stamp=at.astimezone(UTC).isoformat(); lease=(at.astimezone(UTC)+timedelta(minutes=30)).isoformat()
    cur=c.execute("INSERT OR IGNORE INTO runs VALUES(?,?,?,?,?,?,?,?,?)",(rid,s['id'],date,'running',lease,None,None,stamp,stamp))
    if cur.rowcount: claims.append({'run_id':rid,'schedule_id':s['id'],'local_date':date,'prompt':s['prompt']})
  return claims
 def complete(self,run_id,result=None,error=None):
  if (result is None)==(error is None): raise ValueError('Provide result or error')
  with self.db() as c:
   cur=c.execute("UPDATE runs SET status=?,result=?,error=?,updated_at=? WHERE id=? AND status='running'",('failed' if error else 'awaiting_review',json.dumps(result,ensure_ascii=False) if result is not None else None,error,datetime.now(UTC).isoformat(),run_id))
   if cur.rowcount!=1: raise ValueError('Run not active')
 def list_runs(self):
  with self.db() as c: return [dict(r) for r in c.execute('SELECT * FROM runs ORDER BY created_at DESC')]
 def set_enabled(self,sid,enabled):
  with self.db() as c:
   if c.execute('UPDATE schedules SET enabled=? WHERE id=?',(int(bool(enabled)),sid)).rowcount!=1: raise ValueError('Schedule not found')
