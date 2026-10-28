#!/usr/bin/env python3
"""Local, single-user LinkedIn review calendar. No network publishing."""
import html, json, os, secrets, sqlite3
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse
from pathlib import Path

ROOT = Path(__file__).resolve().parent
DB = Path(os.environ.get('LINKEDIN_DB', ROOT / 'data.sqlite3'))
TOKEN = secrets.token_urlsafe(32)

def now(): return datetime.now(timezone.utc).isoformat(timespec='seconds')
def db():
    c = sqlite3.connect(DB, timeout=10); c.row_factory = sqlite3.Row; return c

def init():
    with db() as c:
        c.executescript('''CREATE TABLE IF NOT EXISTS posts(id INTEGER PRIMARY KEY, account TEXT NOT NULL, scheduled TEXT NOT NULL, body TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'draft', approved_body TEXT, approved_at TEXT, attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
        CREATE TABLE IF NOT EXISTS logs(id INTEGER PRIMARY KEY, post_id INTEGER, action TEXT NOT NULL, detail TEXT NOT NULL, at TEXT NOT NULL);''')

def log(c, pid, action, detail=''): c.execute('INSERT INTO logs(post_id,action,detail,at) VALUES(?,?,?,?)',(pid,action,detail,now()))
def esc(s): return html.escape(str(s or ''), quote=True)
def stamp(s):
    d=datetime.fromisoformat(s.replace('Z','+00:00'))
    if d.tzinfo is None: raise ValueError('时间必须包含时区，例如 +08:00')
    return d.astimezone(timezone.utc).isoformat(timespec='minutes')

def change(action, f):
    with db() as c:
        if action == 'create':
            account=f.get('account',''); body=f.get('body','').strip(); scheduled=stamp(f.get('scheduled',''))
            if account not in ('company','personal') or not body: raise ValueError('请选择账号并填写内容')
            cur=c.execute('INSERT INTO posts(account,scheduled,body,status,created_at,updated_at) VALUES(?,?,?,?,?,?)',(account,scheduled,body,'draft',now(),now())); log(c,cur.lastrowid,'created'); return
        pid=int(f['id']); p=c.execute('SELECT * FROM posts WHERE id=?',(pid,)).fetchone()
        if not p: raise ValueError('草稿不存在')
        if action == 'edit':
            if p['status'] == 'published': raise ValueError('已发布记录不能修改')
            body=f.get('body','').strip(); account=f.get('account',''); scheduled=stamp(f.get('scheduled',''))
            if not body or account not in ('company','personal'): raise ValueError('请填写完整内容')
            c.execute("UPDATE posts SET body=?, account=?, scheduled=?, status='draft', approved_body=NULL, approved_at=NULL, last_error=NULL, updated_at=? WHERE id=?",(body,account,scheduled,now(),pid)); log(c,pid,'edited','修改后需要重新批准')
        elif action == 'approve':
            if p['status'] not in ('draft','rejected'): raise ValueError('只有草稿或退回内容可批准')
            c.execute("UPDATE posts SET status='approved', approved_body=body, approved_at=?, updated_at=? WHERE id=?",(now(),now(),pid)); log(c,pid,'approved','批准当前正文、账号和时间')
        elif action == 'reject':
            if p['status'] == 'published': raise ValueError('已发布记录不可退回')
            c.execute("UPDATE posts SET status='rejected',approved_body=NULL,approved_at=NULL,updated_at=? WHERE id=?",(now(),pid)); log(c,pid,'rejected')
        elif action == 'queue':
            if p['status'] != 'approved' or p['approved_body'] != p['body']: raise ValueError('只有当前版本已批准的帖子可以进入队列')
            c.execute("UPDATE posts SET status='queued',updated_at=? WHERE id=?",(now(),pid)); log(c,pid,'queued','等待配置发布渠道；不会自动发送')
        elif action == 'retry':
            if p['status'] != 'failed' or not p['approved_at'] or p['approved_body'] != p['body']: raise ValueError('只能重试已批准的失败帖子')
            c.execute("UPDATE posts SET status='queued',last_error=NULL,updated_at=? WHERE id=?",(now(),pid)); log(c,pid,'retry_queued','等待发布渠道')
        else: raise ValueError('未知操作')

def page():
    with db() as c:
        posts=c.execute('SELECT * FROM posts ORDER BY scheduled,id').fetchall(); logs=c.execute('SELECT * FROM logs ORDER BY id DESC LIMIT 100').fetchall()
    form=lambda action,inner: f'<form method="post" action="/{action}"><input type="hidden" name="token" value="{TOKEN}">{inner}</form>'
    cards=''
    for p in posts:
        pid=p['id']; fields=f'<input type="hidden" name="id" value="{pid}"><select name="account"><option value="company" {"selected" if p["account"]=="company" else ""}>公司</option><option value="personal" {"selected" if p["account"]=="personal" else ""}>个人</option></select><input name="scheduled" value="{esc(p["scheduled"])}" required><textarea name="body" required>{esc(p["body"])}</textarea><button>保存修改（撤销批准）</button>'
        actions=''.join(form(a,f'<input type="hidden" name="id" value="{pid}"><button>{label}</button>') for a,label in [('approve','批准'),('reject','退回'),('queue','加入队列'),('retry','重试')])
        cards+=f'<article><b>#{pid} · {esc(p["account"])} · {esc(p["status"])}</b><p>计划时间（UTC）：{esc(p["scheduled"])} · 尝试次数：{p["attempts"]}</p>{form("edit",fields)}<div class="actions">{actions}</div></article>'
    new=form('create','<select name="account"><option value="company">公司</option><option value="personal">个人</option></select><input name="scheduled" placeholder="2026-11-01T09:00+08:00" required><textarea name="body" placeholder="粘贴英文草稿" required></textarea><button>创建草稿</button>')
    entries=''.join(f'<li>{esc(x["at"])} · #{esc(x["post_id"])} · {esc(x["action"])} · {esc(x["detail"])}</li>' for x in logs)
    return f'''<!doctype html><html lang="zh"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>LinkedIn 内容日历</title><style>body{{font:16px system-ui;max-width:1000px;margin:2rem auto;padding:0 1rem;background:#f5f7fa;color:#172433}}article,section{{background:white;padding:1rem;margin:1rem 0;border-radius:10px}}textarea{{display:block;width:100%;min-height:120px;box-sizing:border-box;margin:.7rem 0}}input,select,button{{padding:.5rem;margin:.2rem}}.actions{{display:flex;flex-wrap:wrap}}.notice{{background:#fff1cd;padding:1rem}}</style><h1>LinkedIn 内容日历</h1><p class="notice">本地审核模式：没有连接发布渠道，队列不会发送帖子。时间输入必须包含时区；修改任何字段都会撤销批准。</p><section><h2>新建草稿</h2>{new}</section><h2>内容日历与队列</h2>{cards or '<p>暂无帖子。</p>'}<section><h2>操作日志（最近 100 条）</h2><ul>{entries}</ul></section></html>'''
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if urlparse(self.path).path != '/': self.send_error(404); return
        b=page().encode(); self.send_response(200); self.send_header('Content-Type','text/html; charset=utf-8'); self.send_header('Content-Security-Policy',"default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"); self.end_headers(); self.wfile.write(b)
    def do_POST(self):
        try:
            if self.headers.get('Origin') not in (None,'http://127.0.0.1:8765'): raise ValueError('无效来源')
            n=int(self.headers.get('Content-Length','0'))
            if n>100000: raise ValueError('内容过长')
            f={k:v[0] for k,v in parse_qs(self.rfile.read(n).decode(),keep_blank_values=True).items()}
            if not secrets.compare_digest(f.get('token',''),TOKEN): raise ValueError('无效令牌')
            change(urlparse(self.path).path.strip('/'),f)
            self.send_response(303); self.send_header('Location','/'); self.end_headers()
        except (ValueError,KeyError) as e:
            self.send_response(400); self.send_header('Content-Type','text/plain; charset=utf-8'); self.end_headers(); self.wfile.write(str(e).encode())
if __name__=='__main__':
    init(); print('打开 http://127.0.0.1:8765 ；按 Ctrl+C 停止',flush=True); ThreadingHTTPServer(('127.0.0.1',8765),Handler).serve_forever()
