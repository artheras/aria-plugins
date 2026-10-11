'use strict';
const $ = id => document.getElementById(id);
let config, csrf = '', order = null, busy = false;
const money = cents => new Intl.NumberFormat('en-US', {style:'currency',currency:'USD'}).format(cents/100);
const labels = {draft:'Draft · needs your review',approved:'Approved by you',awaiting_payment:'Awaiting payment',paid:'Payment reconciled'};
async function api(path, body) {
  const options = {credentials:'same-origin',headers:{}};
  if (body !== undefined) { options.method='POST'; options.headers={'Content-Type':'application/json','X-CSRF-Token':csrf}; options.body=JSON.stringify(body); }
  const response = await fetch(path,options);
  const data = await response.json();
  if (!response.ok) { if(response.status===401) $('login-dialog').showModal(); throw new Error(data.error || 'Request failed'); }
  return data;
}
function feedback(message,error=false) { $('feedback').textContent=message; $('feedback').classList.toggle('error',error); }
async function run(action) {
  if(busy) return;
  busy=true; document.querySelectorAll('button').forEach(b=>b.disabled=true);
  feedback('Working…');
  try { await action(); } catch(e) { feedback(e.message,true); }
  finally { busy=false; document.querySelectorAll('button').forEach(b=>b.disabled=false); }
}
function appendText(parent,tag,text,className) {
  const element=document.createElement(tag); element.textContent=text; if(className) element.className=className; parent.append(element); return element;
}
function render() {
  if (!order) return;
  $('empty').hidden=true; $('order-content').hidden=false; $('refresh').hidden=false;
  $('state').textContent=labels[order.state] || order.state; $('state').classList.toggle('paid',order.state==='paid');
  $('summary').textContent=order.summary; $('order-id').textContent='ORDER '+order.id;
  $('items').replaceChildren();
  for(const item of order.items) {
    const row=document.createElement('tr'); const name=appendText(row,'td',item.name);
    appendText(name,'small',item.sku+' · '+money(item.unit_amount)+' each');
    appendText(row,'td',String(item.quantity)); appendText(row,'td',money(item.quantity*item.unit_amount)); $('items').append(row);
  }
  $('total').textContent=money(order.amount);
  ['approve','checkout','capture','paypal-link','receipt'].forEach(id=>$(id).hidden=true);
  $('approve').hidden=order.state!=='draft'; $('checkout').hidden=order.state!=='approved';
  const notes={draft:'Review each item and the total. Approval applies only to this invoice.',approved:'Approved. Checkout can now be created for this invoice.',awaiting_payment:config.mode==='demo'?'Simulate a completed payment to test reconciliation. No money moves.':'Complete approval in PayPal Sandbox, then return here and verify the payment.',paid:config.mode==='demo'?'Simulation complete. This receipt is clearly labelled as demo data.':'PayPal capture verified against the approved invoice.'};
  $('action-note').textContent=notes[order.state] || '';
  if(order.state==='awaiting_payment') {
    $('capture').hidden=false; $('capture').textContent=config.mode==='demo'?'Simulate payment & reconcile →':'Capture & verify Sandbox payment →';
    if(order.payment && order.payment.checkout_url) { $('paypal-link').hidden=false; $('paypal-link').href=order.payment.checkout_url; }
  }
  if(order.state==='paid') { $('receipt').hidden=false; $('receipt').href='/api/orders/'+order.id+'/receipt'; }
  $('payment-info').textContent=order.payment?'Provider: '+order.payment.provider+' · '+order.payment.order_id:'No payment has been created.';
  const stages=['draft','approved','awaiting_payment','paid'], current=stages.indexOf(order.state);
  document.querySelectorAll('.steps li').forEach((li,index)=>{li.classList.toggle('current',index===current);li.classList.toggle('done',index<current);});
  $('audit').replaceChildren();
  for(const event of order.events) {
    const li=document.createElement('li'); appendText(li,'strong',event.action.replaceAll('_',' ')); appendText(li,'p',event.detail);
    const time=appendText(li,'time',new Date(event.at).toLocaleString()); time.dateTime=event.at; $('audit').append(li);
  }
  const address=new URL(location.href); address.searchParams.set('order',order.id); address.searchParams.delete('token'); address.searchParams.delete('PayerID'); history.replaceState(null,'',address);
}
async function loadOrder(id) { order=await api('/api/orders/'+encodeURIComponent(id)); render(); }
$('draft-form').addEventListener('submit',event=>{
  event.preventDefault(); run(async()=>{order=await api('/api/orders',{prompt:$('prompt').value});render();feedback('Draft ready. Review the invoice before approval.');});
});
$('example').addEventListener('click',()=>{$('prompt').value='Create an order for 3 caps and 2 mugs.';$('prompt').focus();});
$('approve').addEventListener('click',()=>run(async()=>{order=await api('/api/orders/'+order.id+'/approve',{digest:order.digest});render();feedback('You approved this exact invoice.');}));
$('checkout').addEventListener('click',()=>run(async()=>{order=await api('/api/orders/'+order.id+'/checkout',{});render();feedback(config.mode==='demo'?'Simulated checkout created.':'PayPal Sandbox checkout created.');}));
$('capture').addEventListener('click',()=>run(async()=>{order=await api('/api/orders/'+order.id+'/capture',{});render();feedback(config.mode==='demo'?'Demo payment reconciled. No real payment was made.':'PayPal capture reconciled successfully.');}));
$('refresh').addEventListener('click',()=>run(async()=>{await loadOrder(order.id);feedback('Order refreshed from server storage.');}));
$('connect').addEventListener('click',()=>$('login-dialog').showModal());
$('close-login').addEventListener('click',()=>$('login-dialog').close());
$('login-form').addEventListener('submit',async event=>{
  event.preventDefault(); $('login-error').textContent='';
  try {
    const session=await api('/api/login',{token:$('token').value});csrf=session.csrf;$('token').value='';$('login-dialog').close();
    const id=new URL(location.href).searchParams.get('order'); if(id) await loadOrder(id); feedback('Merchant session connected.');
  } catch(e) {$('login-error').textContent=e.message;}
});
async function init() {
  try {
    config=await api('/api/config');
    $('mode-text').textContent=config.mode==='demo'?'DEMO MODE · Local parser and simulated payment. No AI call, PayPal transaction, or money movement.':'PAYPAL SANDBOX · AI Gateway drafts. Human approval required. Sandbox funds only.';
    $('drafter-note').textContent='Draft engine: '+config.drafter;
    $('connect').hidden=!config.merchant_login_available;
    const products=await api('/api/catalog');
    for(const product of products) {
      const card=document.createElement('div');card.className='product';
      appendText(card,'span',product.sku,'product-code');appendText(card,'span',product.name,'product-name');appendText(card,'span',money(product.unit_amount),'product-price');$('catalog').append(card);
    }
    const session=await api('/api/session');csrf=session.csrf || '';
    if(!session.authenticated) {$('login-dialog').showModal();return;}
    const params=new URL(location.href).searchParams,id=params.get('order');
    if(id) {await loadOrder(id);feedback(params.has('cancelled')?'PayPal checkout was cancelled. You can reopen it or create a new draft.':'Order restored. Payment status comes from server records.');}
  } catch(e) {feedback(e.message,true);}
}
init();
