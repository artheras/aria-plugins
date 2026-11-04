'use strict';
const el=id=>document.getElementById(id);
let csrf='';
function feedback(message,error=false){el('feedback').textContent=message;el('feedback').classList.toggle('error',error);}
async function api(path,body){
 const options={credentials:'same-origin',headers:{}};
 if(body!==undefined){options.method='POST';options.headers={'Content-Type':'application/json','X-CSRF-Token':csrf};options.body=JSON.stringify(body);}
 const response=await fetch(path,options);const result=await response.json();
 if(!response.ok)throw new Error(result.error||'Request failed.');return result;
}
async function action(button,run){button.disabled=true;try{await run();}catch(e){feedback(e.message,true);}finally{button.disabled=false;}}
async function provider(){
 el('provider-state').textContent='Checking';
 try{
  const status=await api('/api/account/paypal');
  el('provider-state').textContent=status.connected?'Connected':'Not connected';
  el('provider-summary').textContent=status.connected?'PayPal merchant: '+status.merchant_id+' · '+status.environment:'Your PayPal account is not connected.';
  el('provider-detail').textContent=status.connected?'Verified capabilities: '+(status.capabilities.join(', ')||'No business-record reads granted'):'No PayPal account data has been read. Aria sign-in and a client connection do not grant PayPal access.';
 }catch(e){el('provider-state').textContent='Unavailable';el('provider-summary').textContent='Could not verify PayPal authorization.';el('provider-detail').textContent='Previously displayed data is not proof of a current connection.';throw e;}
}
function row(target,entry,manual=false){
 const item=document.createElement('li');item.className='app';const text=document.createElement('span');text.textContent=entry.name||entry.label||'Client';
 const details=document.createElement('small');details.textContent=entry.scopes.join(', ')+' · expires '+new Date(entry.expires_at*1000).toLocaleString();text.append(details);item.append(text);
 const button=document.createElement('button');button.type='button';button.textContent='Revoke';button.setAttribute('aria-label','Revoke '+text.firstChild.textContent);
 button.addEventListener('click',()=>action(button,async()=>{await api(manual?'/api/account/connections/revoke':'/api/account/apps/revoke',{id:entry.id});item.remove();feedback('Connection revoked. Further access and renewal are stopped.');}));item.append(button);target.append(item);
}
async function init(){
 try{
  const config=await api('/api/config');el('mcp-url').value=location.origin+'/mcp';
  if(config.auth_mode!=='oidc'){feedback('This deployment is a local demonstration. Personal PayPal account connections are not configured.');return;}
  const session=await api('/api/session');if(!session.authenticated){el('signin').hidden=false;feedback('Sign in to manage your connections.');return;}
  csrf=session.csrf;el('provider').hidden=false;el('sign-out').hidden=false;el('clients').hidden=false;
  const apps=await api('/api/account/apps');apps.apps.forEach(c=>row(el('apps'),c));if(!apps.apps.length)el('apps').textContent='No connected AI clients.';
  const manual=await api('/api/account/connections');manual.connections.forEach(c=>row(el('manual-apps'),c,true));
  await provider();feedback('Connection settings loaded.');
 }catch(e){feedback(e.message,true);}
}
el('refresh-provider').addEventListener('click',()=>action(el('refresh-provider'),provider));
el('sign-out').addEventListener('click',()=>action(el('sign-out'),async()=>{await api('/api/logout',{});location.replace('/');}));
init();
