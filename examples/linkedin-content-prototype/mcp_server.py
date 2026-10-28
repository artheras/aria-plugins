#!/usr/bin/env python3
"""Local MCP stdio bridge. Only draft creation and read operations are exposed."""
import json, sys
import app
app.init()
TOOLS=[{'name':'linkedin_create_draft','description':'Create an unapproved LinkedIn draft. Time must include timezone. Never publishes.','inputSchema':{'type':'object','properties':{'account':{'type':'string','enum':['company','personal']},'scheduled':{'type':'string'},'body':{'type':'string'}},'required':['account','scheduled','body'],'additionalProperties':False}},{'name':'linkedin_list_posts','description':'List local drafts and their review and queue status.','inputSchema':{'type':'object','properties':{},'additionalProperties':False}}]
def respond(req):
 method=req.get('method'); p=req.get('params') or {}; rid=req.get('id')
 if method=='initialize': result={'protocolVersion':p.get('protocolVersion','2025-03-26'),'capabilities':{'tools':{}},'serverInfo':{'name':'aria-linkedin-content','version':'0.1.0'}}
 elif method=='ping': result={}
 elif method=='tools/list': result={'tools':TOOLS}
 elif method=='tools/call':
  try:
   name=p.get('name'); args=p.get('arguments') or {}
   if name=='linkedin_create_draft':
    if set(args)!={'account','scheduled','body'} or not all(isinstance(v,str) for v in args.values()): raise ValueError('account, scheduled and body must be strings')
    app.change('create',args); value='Draft created. Review and approve it in the local browser.'
   elif name=='linkedin_list_posts':
    if args: raise ValueError('Unexpected arguments')
    with app.db() as c: value=json.dumps([dict(x) for x in c.execute('SELECT id,account,scheduled,body,status,approved_at,attempts,last_error FROM posts ORDER BY scheduled,id')],ensure_ascii=False)
   else: raise ValueError('Unknown tool')
   result={'content':[{'type':'text','text':value}],'isError':False}
  except (ValueError,KeyError,TypeError) as e: result={'content':[{'type':'text','text':str(e)}],'isError':True}
 elif rid is None: return None
 else: return {'jsonrpc':'2.0','id':rid,'error':{'code':-32601,'message':'Method not found'}}
 return None if rid is None else {'jsonrpc':'2.0','id':rid,'result':result}
for line in sys.stdin:
 try:
  req=json.loads(line); response=respond(req)
  if response is not None: print(json.dumps(response,ensure_ascii=False),flush=True)
 except Exception as e:
  print(json.dumps({'jsonrpc':'2.0','id':None,'error':{'code':-32700,'message':'Invalid request'}}),flush=True)
