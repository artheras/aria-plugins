'use strict';
const $ = id => document.getElementById(id);
let config, csrf = '', order = null, busy = false, catalogue = [], catalogueDigest = '', historyCursor = '';
const money = cents => new Intl.NumberFormat('en-US', {style:'currency',currency:'USD'}).format(cents/100);
const labels = {draft:'Draft · needs your review',approved:'Approved by you',awaiting_payment:'Awaiting payment',paid:'Payment reconciled',cancelled:'Cancelled',expired:'Expired',refunded:'Full refund reconciled',refund_pending:'Refund pending',payment_review:'Provider review required'};
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
  $('action-note').textContent=order.operation?(order.operation.needs_review?'This payment operation needs merchant review. Keep the same order ID.':'A payment operation is being reconciled. Refresh this order shortly.'):notes[order.state] || '';
  if(order.state==='awaiting_payment') {
    $('capture').hidden=false; $('capture').textContent=config.mode==='demo'?'Simulate payment & reconcile →':'Capture & verify Sandbox payment →';
    if(order.payment && order.payment.checkout_url) { $('paypal-link').hidden=false; $('paypal-link').href=order.payment.checkout_url; }
  }
  if(order.operation) { $('checkout').hidden=true;$('capture').hidden=true; }
  if(['paid','refunded','refund_pending','payment_review'].includes(order.state)) { $('receipt').hidden=false; $('receipt').href='/api/orders/'+order.id+'/receipt'; }
  if(order.state==='refunded') $('action-note').textContent=config.mode==='demo'?'Simulated full refund complete. No money moved.':'Full refund verified against the original capture.';
  if(order.state==='refund_pending') $('action-note').textContent='The refund is pending. Keep this order and check provider status; do not issue a replacement refund.';
  $('payment-info').textContent=order.payment?'Provider: '+order.payment.provider+' · '+order.payment.order_id:'No payment has been created.';
  const editable=['draft','approved'].includes(order.state) && !order.payment && !order.operation;
  $('edit-order').hidden=!editable; $('cancel-order').hidden=!editable;
  $('resume-order').hidden=!order.operation?.needs_review || !['create','capture'].includes(order.operation.kind);
  $('refund-order').hidden=order.state!=='paid' || !!order.operation;
  $('sync-payment').hidden=!order.payment?.capture_id || config.mode==='demo';
  if(order.operation?.needs_review) $('action-note').textContent+=' Reason: '+(order.operation.last_code || 'Review required')+'.';
  if(order.state==='cancelled' || order.state==='expired') $('action-note').textContent='This unstarted order is closed. Create a new request to continue.';
  const stages=['draft','approved','awaiting_payment','paid'], current=stages.indexOf(order.state);
  document.querySelectorAll('.steps li').forEach((li,index)=>{li.classList.toggle('current',index===current);li.classList.toggle('done',index<current);});
  $('audit').replaceChildren();
  for(const event of order.events) {
    const li=document.createElement('li'); appendText(li,'strong',event.action.replaceAll('_',' ')); appendText(li,'p',event.detail);
    const time=appendText(li,'time',new Date(event.at).toLocaleString('en-US',{dateStyle:'medium',timeStyle:'short'})); time.dateTime=event.at; $('audit').append(li);
  }
  const address=new URL(location.href); address.searchParams.set('order',order.id); address.searchParams.delete('token'); address.searchParams.delete('PayerID'); history.replaceState(null,'',address);
}
async function loadOrder(id) { order=await api('/api/orders/'+encodeURIComponent(id)); render(); }
function clearOrder() {
  order=null;$('empty').hidden=false;$('order-content').hidden=true;$('refresh').hidden=true;$('state').textContent='Ready when you are';$('state').classList.remove('paid');$('audit').replaceChildren();appendText($('audit'),'li','Select an order or create a draft to view its activity.','audit-placeholder');
}
async function loadCatalogue() {
  catalogue=await api('/api/catalog'); $('catalog').replaceChildren();
  for(const product of catalogue) {
    const card=document.createElement('div');card.className='product';
    appendText(card,'span',product.sku,'product-code');appendText(card,'span',product.name,'product-name');appendText(card,'span',money(product.unit_amount),'product-price');$('catalog').append(card);
  }
}
async function loadHistory(more=false) {
  const params=new URLSearchParams({limit:'12',q:$('history-query').value,state:$('history-state').value,review:$('history-review').checked?'1':'0'});
  if(more && historyCursor) params.set('cursor',historyCursor);
  const page=await api('/api/orders?'+params);
  if(!more) $('history-list').replaceChildren();
  for(const record of page.orders) {
    const button=document.createElement('button');button.type='button';button.className='order-card';
    appendText(button,'strong',money(record.amount)+' · '+(labels[record.state] || record.state));
    appendText(button,'span',record.items.map(item=>item.quantity+' × '+item.name).join(', '));
    appendText(button,'small',record.id+(record.operation?.needs_review?' · Needs merchant review':''));
    button.addEventListener('click',()=>run(async()=>{await loadOrder(record.id);$('workspace').scrollIntoView({behavior:'smooth',block:'start'});feedback('Order opened.');}));
    $('history-list').append(button);
  }
  if(!more && !page.orders.length) appendText($('history-list'),'p',page.next_cursor?'No matching orders in this page. Load more to continue searching.':'No orders match this filter.','fine');
  historyCursor=page.next_cursor || ''; $('history-more').hidden=!historyCursor;
}
$('history-form').addEventListener('submit',e=>{e.preventDefault();run(()=>loadHistory());});
$('refresh-history').addEventListener('click',()=>run(()=>loadHistory()));
$('history-more').addEventListener('click',()=>run(()=>loadHistory(true)));
$('edit-order').addEventListener('click',()=>run(async()=>{
  await loadCatalogue();$('edit-products').replaceChildren();
  for(const p of catalogue) {
    const label=appendText($('edit-products'),'label',p.name+' · '+money(p.unit_amount));
    const input=document.createElement('input');input.type='number';input.min='0';input.max='100';input.step='1';input.required=true;input.dataset.sku=p.sku;input.value=order.items.find(i=>i.sku===p.sku)?.quantity || 0;label.append(input);
  }
  $('edit-error').textContent='';$('edit-dialog').showModal();feedback('Review all quantities before saving.');
}));
$('close-edit').addEventListener('click',()=>$('edit-dialog').close());
$('edit-form').addEventListener('submit',e=>{e.preventDefault();run(async()=>{
  const items=[...$('edit-products').querySelectorAll('input')].map(i=>({sku:i.dataset.sku,quantity:Number(i.value)})).filter(i=>i.quantity>0);
  try { order=await api('/api/orders/'+order.id+'/revise',{digest:order.digest,items});$('edit-dialog').close();render();await loadHistory();feedback('Draft revised. Review the new total and approve again.'); }
  catch(error) { $('edit-error').textContent=error.message;throw error; }
});});
$('cancel-order').addEventListener('click',()=>run(async()=>{
  if(!window.confirm('Cancel this unstarted order? No payment will be made.')) { feedback('Order kept.');return; }
  order=await api('/api/orders/'+order.id+'/cancel',{digest:order.digest});render();await loadHistory();feedback('Order cancelled.');
}));
$('resume-order').addEventListener('click',()=>run(async()=>{
  if(!window.confirm('Confirm this exact invoice and resume the existing operation? Payment capture remains a separate action.')) { feedback('Review kept pending.');return; }
  order=await api('/api/orders/'+order.id+'/resume',{digest:order.digest});render();await loadHistory();feedback('Same operation resumed for background reconciliation.');
}));
$('refund-order').addEventListener('click',()=>run(async()=>{
  if(!window.confirm((config.mode==='demo'?'Simulate a full refund of ':'Refund the full captured amount of ')+money(order.amount)+'?')) { feedback('Refund cancelled.');return; }
  order=await api('/api/orders/'+order.id+'/refund',{digest:order.digest});render();await loadHistory();feedback(order.state==='refunded'?'Full refund reconciled.':'Refund submitted. Keep the same order while the provider confirms it.');
}));
$('sync-payment').addEventListener('click',()=>run(async()=>{order=await api('/api/orders/'+order.id+'/sync',{});render();await loadHistory();feedback('Current provider status checked.');}));
function productRow(p={sku:'',name:'',unit_amount:100}) {
  const row=document.createElement('div');row.className='catalogue-row';
  for(const [field,labelText,value] of [['sku','SKU',p.sku],['name','Product name',p.name],['price','Price (USD)',(p.unit_amount/100).toFixed(2)]]) {
    const label=appendText(row,'label',labelText);const input=document.createElement('input');input.dataset.field=field;input.value=value;input.required=true;input.maxLength=field==='name'?160:64;if(field==='price'){input.inputMode='decimal';input.pattern='[0-9]+(\\.[0-9]{1,2})?';}label.append(input);
  }
  const remove=appendText(row,'button','Remove','quiet');remove.type='button';remove.addEventListener('click',()=>row.remove());$('catalogue-rows').append(row);
}
$('edit-catalogue').addEventListener('click',()=>run(async()=>{
  const snap=await api('/api/catalog/snapshot');catalogueDigest=snap.digest;$('catalogue-rows').replaceChildren();snap.products.forEach(productRow);$('catalogue-error').textContent='';$('catalogue-dialog').showModal();feedback('Catalogue loaded.');
}));
$('add-product').addEventListener('click',()=>productRow());
$('close-catalogue').addEventListener('click',()=>$('catalogue-dialog').close());
$('catalogue-form').addEventListener('submit',e=>{e.preventDefault();run(async()=>{
  try {
    const products=[...$('catalogue-rows').children].map(row=>{
      const value=field=>row.querySelector('[data-field="'+field+'"]').value.trim();const price=value('price');
      if(!/^\d+(\.\d{1,2})?$/.test(price)) throw new Error('Enter a USD price with at most two decimal places.');
      const parts=price.split('.');return {sku:value('sku'),name:value('name'),currency:'USD',unit_amount:Number(parts[0])*100+Number((parts[1] || '').padEnd(2,'0'))};
    });
    await api('/api/catalog/update',{digest:catalogueDigest,products});$('catalogue-dialog').close();await loadCatalogue();feedback('Products saved. Existing reviewed invoices keep their original prices.');
  } catch(error) { $('catalogue-error').textContent=error.message;throw error; }
});});
$('draft-form').addEventListener('submit',event=>{
  event.preventDefault(); run(async()=>{order=await api('/api/orders',{prompt:$('prompt').value});render();await loadHistory();feedback('Draft ready. Review the invoice before approval.');});
});
$('example').addEventListener('click',()=>{$('prompt').value='Create an order for 3 caps and 2 mugs.';$('prompt').focus();});
$('approve').addEventListener('click',()=>run(async()=>{order=await api('/api/orders/'+order.id+'/approve',{digest:order.digest});render();await loadHistory();feedback('You approved this exact invoice.');}));
$('checkout').addEventListener('click',()=>run(async()=>{order=await api('/api/orders/'+order.id+'/checkout',{});render();await loadHistory();feedback(config.mode==='demo'?'Simulated checkout created.':'PayPal Sandbox checkout created.');}));
$('capture').addEventListener('click',()=>run(async()=>{order=await api('/api/orders/'+order.id+'/capture',{});render();await loadHistory();feedback(config.mode==='demo'?'Demo payment reconciled. No real payment was made.':'PayPal capture reconciled successfully.');}));
$('refresh').addEventListener('click',()=>run(async()=>{await loadOrder(order.id);await loadHistory();feedback('Order refreshed from server storage.');}));
$('connect').addEventListener('click',()=>$('login-dialog').showModal());
$('close-login').addEventListener('click',()=>$('login-dialog').close());
$('login-form').addEventListener('submit',async event=>{
  event.preventDefault(); $('login-error').textContent='';
  try {
    const session=await api('/api/login',{token:$('token').value});csrf=session.csrf;$('token').value='';$('login-dialog').close();$('catalogue-admin').hidden=!session.merchant;clearOrder();await loadHistory();
    const id=new URL(location.href).searchParams.get('order');
    if(id) {
      try { await loadOrder(id); }
      catch(error) { const address=new URL(location.href);address.searchParams.delete('order');history.replaceState(null,'',address);feedback('Merchant connected. The previous order is unavailable in this session; select an order or create a draft.');return; }
    }
    feedback('Merchant session connected.');
  } catch(e) {$('login-error').textContent=e.message;}
});
document.querySelectorAll('[data-copy]').forEach(button=>{
  button.addEventListener('click',async()=>{
    const command=$(button.dataset.copy);
    try {
      if (!navigator.clipboard) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(command.textContent);
      $('copy-status').textContent='Copied. Paste this into your Aria Code session.';
    } catch {
      const range=document.createRange();range.selectNodeContents(command);
      const selection=window.getSelection();selection.removeAllRanges();selection.addRange(range);
      $('copy-status').textContent='Command selected. Copy it and paste it into your Aria Code session.';
    }
  });
});
async function init() {
  try {
    config=await api('/api/config');
    $('mode-text').textContent=config.mode==='demo'?'DEMO MODE · Local parser and simulated payment. No AI call, PayPal transaction, or money movement.':'PAYPAL SANDBOX · AI Gateway drafts. Human approval required. Sandbox funds only.';
    $('drafter-note').textContent='Draft engine: '+config.drafter;
    $('connect').hidden=!config.merchant_login_available;
    await loadCatalogue();
    const session=await api('/api/session');csrf=session.csrf || '';
    if(!session.authenticated) {$('login-dialog').showModal();return;}
    $('catalogue-admin').hidden=!session.merchant;await loadHistory();
    const params=new URL(location.href).searchParams,id=params.get('order');
    if(id) {await loadOrder(id);feedback(params.has('cancelled')?'PayPal checkout was cancelled. You can reopen it or create a new draft.':'Order restored. Payment status comes from server records.');}
  } catch(e) {feedback(e.message,true);}
}
init();

