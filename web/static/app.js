import { renderChart, renderComparisonChart, disposeChart, csvCell } from './charts.js';

const $ = selector => document.querySelector(selector);
const h = value => String(value ?? '').replace(/[&<>"']/g, c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const num = new Intl.NumberFormat('en-US',{maximumFractionDigits:6});
const compact = new Intl.NumberFormat('en-US',{notation:'compact',maximumFractionDigits:2});
const date = value => value ? new Date(value).toISOString().slice(0,10) : '—';
const datetime = value => value ? new Date(value).toISOString().slice(0,16).replace('T',' ') : '—';
const price = value => {
  if(value===null||value===undefined)return '—';
  const n=Number(value);
  return n!==0&&Math.abs(n)<.000001?n.toExponential(4).replace(/\.0+(?=e)/,''):num.format(n);
};
const amount = value => value===null||value===undefined ? '—' : '$'+compact.format(Number(value));
const bps = value => value===null||value===undefined ? '—' : (Number(value)>0?'+':'')+num.format(Number(value))+' bps';
const labels = {stable:'阈值内',warning:'预警',critical:'严重偏离',unknown:'锚定价待确认'};
const badge = (status,text) => `<span class="badge ${h(status)}">${h(text??labels[status]??status)}</span>`;
const brand = '<span class="brand-mark" aria-hidden="true">◇</span><span>Depeg<small>稳定币研究</small></span>';
const sessionKey='depeg.session.v1';
const usernamePattern='[A-Za-z0-9][A-Za-z0-9_.\\-]{2,31}';
const state={user:null,tokens:null,overview:null,epoch:0,controller:null,refresh:null,chartField:'price',points:[],filter:'all',search:'',sort:'cap',marketView:'list',comparison:null};
try { state.tokens=JSON.parse(sessionStorage.getItem(sessionKey)); } catch { /* Storage can be disabled. */ }

function saveTokens(tokens) {
  state.tokens=tokens;
  try { if(tokens)sessionStorage.setItem(sessionKey,JSON.stringify(tokens));else sessionStorage.removeItem(sessionKey); } catch { /* The session remains in memory. */ }
}
function clearAccount() {
  saveTokens(null);state.user=null;state.overview=null;state.points=[];state.epoch++;state.controller?.abort();
  state.marketView='list';state.comparison=null;
}
function toast(message) {
  const n=$('#toast');n.textContent=message;n.hidden=false;clearTimeout(state.toastTimer);state.toastTimer=setTimeout(()=>n.hidden=true,5500);
}
class APIError extends Error { constructor(message,status=0,code){super(message);this.status=status;this.code=code;} }
const messages={400:'参数不正确，请检查日期范围、锚定价与阈值。',401:'登录已过期，请重新登录。',403:'当前访问来源未获允许，请通过后端服务地址打开网页。',404:'没有找到对应记录。',409:'记录已存在或检测规则发生变化，请刷新后重试。',503:'数据库或服务暂不可用。请确认 MySQL 已启动，并执行过最新数据库迁移。'};
const verificationMessages={
  invalid_email_code:'验证码不正确、已过期或已达到尝试上限，请核对验证码或重新发送。',
  email_code_cooldown:'验证码发送间隔为 60 秒，请稍后再试。',
  email_send_limit:'验证码请求过于频繁，请稍后再试。'
};

async function raw(path,{method='GET',body,token,signal}={}) {
  const controller=new AbortController(),timer=setTimeout(()=>controller.abort(new DOMException('请求超时','TimeoutError')),30000);
  const cancel=()=>controller.abort(signal.reason);if(signal?.aborted)cancel();else signal?.addEventListener('abort',cancel,{once:true});
  try {
    const headers={Accept:'application/json'};if(token)headers.Authorization='Bearer '+token;if(body!==undefined)headers['Content-Type']='application/json';
    const response=await fetch('/api/v1'+path,{method,headers,body:body===undefined?undefined:JSON.stringify(body),signal:controller.signal,cache:'no-store',credentials:'omit'});
    if(response.status===204)return null;
    let result;try{result=await response.json();}catch{throw new APIError('服务返回了无法读取的响应。',response.status);}
    if(!response.ok)throw new APIError(messages[response.status]??'请求失败，请稍后重试。',response.status,result?.error?.code);
    return result.data;
  } catch(error) {
    if(signal?.aborted)throw new DOMException('已取消','AbortError');
    if(error instanceof APIError)throw error;
    throw new APIError(controller.signal.aborted?'请求超时，请稍后重试。':'无法连接服务，请确认后端仍在运行。');
  } finally { clearTimeout(timer);signal?.removeEventListener('abort',cancel); }
}

async function api(path,options={}) {
  const used=state.tokens?.access_token;
  try { return await raw(path,{...options,token:used}); }
  catch(error) {
    if(error.status!==401)throw error;
    if(!state.tokens?.refresh_token)throw error;
    if(state.tokens.access_token===used) {
      if(!state.refresh)state.refresh=raw('/auth/refresh',{method:'POST',body:{refresh_token:state.tokens.refresh_token}}).then(tokens=>{saveTokens(tokens);}).catch(e=>{
        if(e.status===401){clearAccount();renderAuth('登录已过期，请重新登录。');}throw e;
      }).finally(()=>state.refresh=null);
      await state.refresh;
    }
    if(options.signal?.aborted)throw new DOMException('已取消','AbortError');
    return raw(path,{...options,token:state.tokens?.access_token});
  }
}

function renderAuth(message='',mode='login',success=false) {
  const register=mode==='register',reset=mode==='reset';
  const emailFields=register||reset?`
    <label>${reset?'已验证的邮箱':'邮箱'}<input id="email" name="email" type="email" autocomplete="email" required maxlength="254" placeholder="${reset?'填写账号已验证的邮箱':'填写可以收信的邮箱'}"></label>
    <div class="auth-code">
      <label>邮箱验证码<input id="email-code" name="email_code" type="text" autocomplete="one-time-code" inputmode="numeric" required pattern="[0-9]{6}" maxlength="6" placeholder="6 位数字"></label>
      <button class="button" id="send-email-code" type="button">${reset?'发送重置码':'发送验证码'}</button>
    </div>`:'';
  const passwordFields=`<label>${reset?'新密码':'密码'}<input id="password" name="password" type="password" autocomplete="${register||reset?'new-password':'current-password'}" required maxlength="72" placeholder="8–72 字节"></label>
    ${reset?'<label>确认新密码<input name="password_confirmation" type="password" autocomplete="new-password" required maxlength="72"></label>':''}`;
  const credentials=reset?'':`<label>用户名<input id="username" name="username" autocomplete="username" required minlength="3" maxlength="32" pattern="${h(usernamePattern)}" placeholder="3–32 位英文、数字或 _ . -"></label>${passwordFields}`;
  const tabs=`<div class="auth-tabs" role="tablist" aria-label="账号操作"><button class="auth-tab ${register?'':'active'}" role="tab" aria-selected="${!register}" id="login-tab">登录</button><button class="auth-tab ${register?'active':''}" role="tab" aria-selected="${register}" id="register-tab">注册</button></div>`;
  $('#app').innerHTML = `
    <div class="auth-layout">
      <header class="auth-header"><a class="brand" href="#/overview">${brand}</a><span>稳定币研究工作台</span></header>
      <main id="main" class="auth-side">
        <section class="auth-box">
          <div class="auth-caption">Depeg 账号</div>
          <h1>${reset?'重置密码':register?'创建研究账号':'登录工作台'}</h1>
          <p>${reset?'通过已验证的邮箱接收验证码，然后设置新密码。':'查看历史行情，保存你的监测规则和检测记录。'}</p>
          ${reset?'':tabs}
          <form class="auth-form" id="auth-form">
            ${credentials}${emailFields}${reset?passwordFields:''}
            <p id="auth-message" class="form-message${success?' success':''}" role="status">${h(message)}</p>
            <button class="button primary" type="submit">${reset?'重置密码':register?'注册并进入工作台':'登录'}</button>
            ${register?'':`<button id="auth-mode-link" class="text-button" type="button">${reset?'返回登录':'忘记密码？'}</button>`}
          </form>
          <p class="auth-footer">账号用于保存个人监测设置。行情来自项目已有的历史数据，观测时间以 UTC 展示。</p>
        </section>
      </main>
      <footer class="auth-bottom">Depeg · 稳定币历史价格与阈值研究</footer>
    </div>`;
  if(!reset) {
    $('#login-tab').onclick=()=>renderAuth();
    $('#register-tab').onclick=()=>renderAuth('','register');
  }
  if(!register){
    $('#auth-mode-link').onclick=()=>{
      renderAuth('',reset?'login':'reset');
      $(reset?'#username':'#email').focus();
    };
  }
  const form=$('#auth-form'),submit=form.querySelector('[type=submit]'),feedback=$('#auth-message');
  const email=$('#email'),emailCode=$('#email-code'),send=$('#send-email-code');
  const resendByEmail=new Map();
  let sending=false,submitting=false,cooldownTimer;
  function showAuthMessage(text,success=false) {
    feedback.textContent=text;
    feedback.classList.toggle('success',success);
  }
  function updateSendButton() {
    if(!send?.isConnected)return;
    const resendAt=resendByEmail.get(email.value.trim())??0;
    const seconds=Math.max(0,Math.ceil((resendAt-Date.now())/1000));
    send.disabled=sending||submitting||seconds>0;
    send.textContent=sending?'发送中…':seconds>0?`${seconds} 秒后重发`:reset?'发送重置码':'发送验证码';
    clearTimeout(cooldownTimer);
    if(seconds>0)cooldownTimer=setTimeout(updateSendButton,1000);
  }
  if(send) {
    email.addEventListener('input',()=>{emailCode.value='';showAuthMessage('');updateSendButton();});
    send.onclick=async()=>{
      if(sending||submitting||!email.reportValidity())return;
      const recipient=email.value.trim();
      sending=true;email.readOnly=true;updateSendButton();showAuthMessage('');
      try {
        const path=reset?'/auth/password-reset/email-code':'/auth/register/email-code';
        await raw(path,{method:'POST',body:{email:recipient}});
        resendByEmail.set(recipient,Date.now()+60000);
        if(!reset)emailCode.value='';
        showAuthMessage(reset?'如果该邮箱已验证，你将收到重置码。验证码 10 分钟内有效。':'验证码已发送，10 分钟内有效。请查看收件箱或垃圾邮件。',true);
        if(emailCode.isConnected)emailCode.focus();
      } catch(error) {
        if(error.code==='email_code_cooldown')resendByEmail.set(recipient,Date.now()+60000);
        showAuthMessage(verificationMessages[error.code]??(error.status===400?'请填写有效的邮箱地址。':error.status===503?'暂时无法发送验证码，请稍后重试或联系管理员。':error.message));
      } finally {
        sending=false;email.readOnly=false;updateSendButton();
      }
    };
  }
  form.onsubmit=async event=>{
    event.preventDefault();
    if(submitting)return;
    if(sending){showAuthMessage('正在发送验证码，请稍候。');return;}
    const password=form.elements.password.value;
    const passwordBytes=new TextEncoder().encode(password).length;
    if(passwordBytes<8||passwordBytes>72){showAuthMessage('密码的 UTF-8 长度应为 8–72 字节。');return;}
    if(reset&&password!==form.elements.password_confirmation.value){showAuthMessage('两次输入的新密码不一致。');return;}
    const username=reset?'':form.elements.username.value.trim().toLowerCase();
    submitting=true;submit.disabled=true;updateSendButton();showAuthMessage('');
    let registered=false;
    try {
      if(reset){
        await raw('/auth/password-reset',{method:'POST',body:{email:email.value.trim(),email_code:emailCode.value,password}});
        if(form.isConnected){
          clearAccount();
          renderAuth('密码已重置，请用新密码登录。','login',true);
          $('#username').focus();
        }
        return;
      }
      if(register){
        await raw('/auth/register',{method:'POST',body:{username,password,email:email.value.trim(),email_code:emailCode.value}});
        registered=true;
      }
      const tokens=await raw('/auth/login',{method:'POST',body:{username,password}});saveTokens(tokens);
      state.user=await api('/auth/me');state.overview=null;renderShell();
    } catch(error) {
      if(registered){renderAuth('账号已创建，请用用户名和密码登录。');$('#username').value=username;return;}
      showAuthMessage(verificationMessages[error.code]??(error.status===409?'用户名或邮箱已被使用，请核对后重试。':error.status===401?'用户名或密码不正确。':error.status===400?(reset?'请检查邮箱、验证码和新密码。':'请检查账号信息。'):error.message));
      return;
    } finally {
      submitting=false;submit.disabled=false;updateSendButton();
    }
    if(!location.hash.startsWith('#/'))location.hash='#/overview';else await renderRoute();
  };
}

function renderShell() {
  const username = state.user.username ?? state.user.Username ?? '';
  $('#app').innerHTML = `
    <div class="layout">
      <header class="site-header">
        <div class="site-header-inner">
          <a class="brand" href="#/overview">${brand}</a>
          <nav class="site-nav" aria-label="主导航">
            <a class="nav-link" data-nav="overview" href="#/overview">市场总览</a>
            <a class="nav-link" data-nav="events" href="#/events">阈值记录</a>
            <a class="nav-link" data-nav="guide" href="#/guide">数据说明</a>
          </nav>
          <div class="account-actions">
            <span class="user-chip"><span class="avatar" aria-hidden="true">${h(username.slice(0,1).toUpperCase()||'U')}</span><span class="user-name">${h(username)}</span></span>
            <button id="logout" class="text-button" type="button">退出</button>
          </div>
        </div>
      </header>
      <div class="workspace">
        <div class="topbar"><span id="crumb">工作台 / 市场总览</span><span class="source-label"><span class="tiny-dot" aria-hidden="true"></span>历史行情 · UTC</span></div>
        <main id="main" tabindex="-1"></main>
      </div>
    </div>`;
  $('#logout').onclick=async()=>{
    const b=$('#logout');b.disabled=true;
    try{if(state.refresh)await state.refresh;await api('/auth/logout',{method:'POST'});clearAccount();renderAuth();}
    catch(error){toast(error.message);b.disabled=false;}
  };
}

async function getOverview(signal,force=false) {
  if(!state.overview||force)state.overview=await api('/dashboard',{signal});return state.overview;
}
function empty(title,description,action='') {return `<div class="empty"><div class="empty-icon" aria-hidden="true">◇</div><h3>${h(title)}</h3><p>${h(description)}</p>${action}</div>`;}
function assetLink(id){return '#/assets/'+encodeURIComponent(id);}
function coin(a,index=0){return `<span class="coin tone-${index%4}" aria-hidden="true">${h(a.symbol.slice(0,2))}</span>`;}
function streamLabel(s){return `${s.kind==='daily'?'日快照':s.interval_seconds===3600?'小时行情':s.interval_seconds===60?'分钟行情':s.interval_seconds/60+' 分钟行情'} · ${s.source}`;}
function quoteFor(a,stream){return stream?.kind==='daily'?'USD':a.asset.quote_currency;}
function chosen(a){return a.selected??a.streams[0]??null;}

async function renderRoute(force=false) {
  if(location.hash==='#main')return;
  if(!state.user)return;
  const epoch=++state.epoch;state.controller?.abort();state.controller=new AbortController();const signal=state.controller.signal;
  state.points=[];
  const route=location.hash.startsWith('#/')?location.hash.slice(2):'overview';
  const active=route==='events'?'events':route==='guide'?'guide':'overview';
  document.querySelectorAll('[data-nav]').forEach(n=>{n.classList.toggle('active',n.dataset.nav===active);if(n.dataset.nav===active)n.setAttribute('aria-current','page');else n.removeAttribute('aria-current');});
  $('#crumb').textContent='工作台 / '+({events:'阈值记录',guide:'数据说明',overview:'市场总览'}[active]);
  $('#main').innerHTML='<div class="loading-line"></div><p class="loading-text">正在读取数据…</p>';
  try {
    if(route==='guide'){renderGuide();return;}
    const overview=await getOverview(signal,force);if(epoch!==state.epoch)return;
    if(route==='events'){renderEventPage(overview,signal);return;}
    if(route.startsWith('assets/')) {
      let id;try{id=decodeURIComponent(route.slice(7));}catch{throw new APIError('币种地址格式不正确。');}
      const a=overview.assets.find(a=>a.asset.id===id);
      if(!a){$('#main').innerHTML=empty('未找到该币种','请返回总览选择已导入的稳定币。','<a class="button" href="#/overview">返回总览</a>');return;}
      await renderDetail(a,signal);return;
    }
    renderOverview(overview,signal);
  } catch(error) {
    if(error.name==='AbortError'||epoch!==state.epoch)return;
    $('#main').innerHTML=`<div class="notice error" role="alert">${h(error.message)}</div><button class="button" id="retry">重新读取</button>`;$('#retry').onclick=()=>renderRoute(true);
  }
}

function renderOverview(overview,signal) {
  const assets=overview.assets,configured=assets.filter(a=>a.rule).length,abnormal=assets.filter(a=>a.status==='warning'||a.status==='critical').length;
  const times=assets.flatMap(a=>a.streams.map(s=>s.latest.observed_at)).sort(),latest=times.at(-1);
  document.title='市场总览 · Depeg';
  const evaluated = assets.some(a => a.status !== 'unknown');
  $('#main').innerHTML=`<div class="page-head"><div><h1>稳定币行情</h1><p>查看历史价格、锚定偏离和个人监测规则。</p></div><div class="head-actions"><button class="button" id="refresh-data">↻ 刷新数据</button></div></div><div class="metrics"><div class="metric"><div class="label">覆盖资产</div><div class="value mono">${assets.length}<span class="small muted"> 个</span></div><div class="foot">按资产 ID 区分币种</div></div><div class="metric"><div class="label">已配置监测</div><div class="value mono">${configured}<span class="small muted"> / ${assets.length}</span></div><div class="foot">当前账号的锚定价与阈值</div></div><div class="metric"><div class="label">最新观测超阈值</div><div class="value mono ${abnormal?'negative':''}">${evaluated?abnormal:'—'}<span class="small muted">${evaluated?' 个':''}</span></div><div class="foot">${evaluated?'基于已确认锚定价的资产':'设置锚定价与阈值后计算'}</div></div><div class="metric"><div class="label">最新数据日期</div><div class="value date mono">${h(date(latest))}</div><div class="foot">数据观测时间 · UTC</div></div></div><div class="notice market-note"><span class="symbol" aria-hidden="true">ⓘ</span><span>价格为数据中的最新观测。进入币种详情设置锚定价与阈值，可检测所选历史区间。</span></div><section class="panel market-panel"><div class="panel-head"><div><h2>稳定币市场</h2><p>价格、偏离程度与数据覆盖</p></div><div class="market-panel-actions"><span class="small muted">${assets.length} 个资产</span><div class="market-switch" role="tablist" aria-label="市场展示方式"><button id="market-list-tab" class="market-view-tab" role="tab" aria-selected="true" aria-controls="market-list-panel" tabindex="0">资产列表</button><button id="market-trends-tab" class="market-view-tab" role="tab" aria-selected="false" aria-controls="market-trends-panel" tabindex="-1">趋势对比</button></div></div></div><div id="market-list-panel" role="tabpanel" aria-labelledby="market-list-tab"><div class="table-toolbar"><div class="tabs" role="group" aria-label="状态筛选"><button class="tab" data-filter="all">全部资产</button><button class="tab" data-filter="abnormal">价格超阈值</button><button class="tab" data-filter="unknown">待确认锚定价</button><button class="tab" data-filter="configured">已配置</button></div><div class="filters"><input class="search" id="asset-search" aria-label="搜索币种" placeholder="搜索币种名称、代码或 ID" value="${h(state.search)}"><select id="asset-sort" aria-label="排序"><option value="cap">市值优先</option><option value="deviation">偏离程度</option><option value="name">币种名称</option><option value="latest">观测时间</option></select></div></div><div class="table-scroll"><table class="market-table"><thead><tr><th scope="col">#</th><th scope="col">币种 / 资产 ID</th><th>最新记录价</th><th>价格偏离</th><th>记录状态</th><th>市值</th><th>观测时间 · UTC</th><th>详情</th></tr></thead><tbody id="asset-rows"></tbody></table></div><div id="asset-empty" hidden></div><div class="table-foot"><span id="asset-count"></span><span>1 bps = 0.01% · 日快照与 OHLC 分开展示</span></div></div><div id="market-trends-panel" role="tabpanel" aria-labelledby="market-trends-tab" hidden></div></section>`;
  $('#refresh-data').onclick=()=>renderRoute(true);
  $('#asset-sort').value=state.sort;
  $('#asset-search').oninput=e=>{state.search=e.target.value;updateAssetRows(assets);};
  $('#asset-sort').onchange=e=>{state.sort=e.target.value;updateAssetRows(assets);};
  document.querySelectorAll('[data-filter]').forEach(n=>n.onclick=()=>{state.filter=n.dataset.filter;updateAssetRows(assets);});
  updateAssetRows(assets);
  let closeTrends=null;
  const tabs=[$('#market-list-tab'),$('#market-trends-tab')];
  function changeView(view) {
    state.marketView=view;
    const trends=view==='trends';
    $('#market-list-panel').hidden=trends;$('#market-trends-panel').hidden=!trends;
    tabs.forEach((tab,i)=>{const active=trends?i===1:i===0;tab.setAttribute('aria-selected',String(active));tab.tabIndex=active?0:-1;});
    if(trends&&!closeTrends)closeTrends=renderMarketTrends($('#market-trends-panel'),assets,signal);
    if(!trends&&closeTrends){closeTrends();closeTrends=null;}
  }
  tabs.forEach((tab,index)=>{
    tab.onclick=()=>changeView(index?'trends':'list');
    tab.onkeydown=event=>{
      if(['ArrowLeft','ArrowRight','Home','End'].includes(event.key)){
        event.preventDefault();const next=event.key==='Home'?0:event.key==='End'?1:1-index;
        changeView(next?'trends':'list');tabs[next].focus();
      }
    };
  });
  changeView(state.marketView);
}

const comparisonPalette=['#4b8f29','#367ae0','#bd7d1d','#8060c1','#d45171','#1591a4','#a76a42','#7c911f'];

function renderMarketTrends(container, assets, parentSignal) {
  const byGroup=new Map();
  for(const a of assets)for(const stream of a.streams) {
    const quote=quoteFor(a,stream),key=JSON.stringify([stream.kind,stream.source,stream.interval_seconds,quote]);
    if(!byGroup.has(key))byGroup.set(key,{key,stream,quote,items:[]});
    byGroup.get(key).items.push({asset:a,stream});
  }
  const groups=[...byGroup.values()].sort((a,b)=>(a.stream.kind==='daily'?0:1)-(b.stream.kind==='daily'?0:1)||(a.quote==='USD'?0:1)-(b.quote==='USD'?0:1)||a.stream.interval_seconds-b.stream.interval_seconds||a.stream.source.localeCompare(b.stream.source));
  for(const group of groups)group.items.sort((a,b)=>Number(b.stream.latest.market_cap_usd??-1)-Number(a.stream.latest.market_cap_usd??-1)||a.asset.asset.symbol.localeCompare(b.asset.asset.symbol)||a.asset.asset.id.localeCompare(b.asset.asset.id));
  if(!groups.length){container.innerHTML=empty('还没有可对比的行情','导入日快照或 OHLC 数据后，可在这里比较多个资产的历史趋势。');return()=>{};}
  let controller=null,request=0,disposed=false,loaded=[],loadedRange=null;
  function cleanup() {
    disposed=true;request++;controller?.abort();
    const chart=container.querySelector('#comparison-chart');if(chart)disposeChart(chart);
    parentSignal.removeEventListener('abort',cleanup);
  }
  parentSignal.addEventListener('abort',cleanup,{once:true});
  if(parentSignal.aborted){cleanup();return cleanup;}
  function chooseGroup(key) {
    const group=byGroup.get(key)??groups[0];
    if(!state.comparison||state.comparison.group!==group.key) {
      const latest=Math.min(Date.now(),Math.max(...group.items.map(item=>Date.parse(item.stream.last_at))));
      const ids=group.items.slice(0,3).map(item=>item.asset.asset.id);
      state.comparison={group:group.key,ids,colors:Object.fromEntries(ids.map((id,i)=>[id,i])),mode:state.comparison?.mode??'price',...defaultDates({last_at:new Date(latest).toISOString()},30)};
    }
    const settings=state.comparison;
    settings.ids=settings.ids.filter(id=>group.items.some(item=>item.asset.asset.id===id));
    request++;controller?.abort();loaded=[];loadedRange=null;
    const oldChart=container.querySelector('#comparison-chart');if(oldChart)disposeChart(oldChart);
    container.innerHTML=`<div class="trend-intro"><div><h3>历史趋势对比</h3><p>不同颜色代表不同资产。选择日期，比较它们在同一数据来源下的历史表现。</p></div><span class="badge history">最多 8 个资产</span></div>
      <div class="trend-settings"><label class="trend-source-label">数据口径<select id="trend-source">${groups.map(g=>`<option value="${h(g.key)}">${h(streamLabel(g.stream))} · ${h(g.quote)}</option>`).join('')}</select></label><div class="trend-mode" role="group" aria-label="趋势展示方式"><button class="tab" data-trend-mode="price">价格</button><button class="tab" data-trend-mode="change">区间涨跌幅</button></div></div>
      <form id="trend-date-form" class="date-form trend-dates"><label>开始日期 · UTC<input type="date" id="trend-from" value="${h(settings.from)}" required></label><label>结束日期 · UTC<input type="date" id="trend-to" value="${h(settings.to)}" required></label><button class="button" type="submit" id="trend-apply">应用日期</button><div class="range-tabs" role="group" aria-label="趋势日期快捷选择">${(group.stream.kind==='daily'?[7,30,90,365,0]:[7,30]).map(days=>`<button class="range-button" type="button" data-trend-days="${days}">${days===0?'全部':days===365?'1 年':days+' 天'}</button>`).join('')}</div></form>
      <div class="trend-picker"><div class="trend-picker-head"><span>选择对比资产 <strong id="trend-selected-count"></strong></span><div><button class="text-button" type="button" id="trend-select-all">${group.items.length>8?'选择前 8 个':'全选'}</button><button class="text-button" type="button" id="trend-select-none">清空</button></div></div><div id="trend-choices" class="trend-choices"></div></div>
      <div class="trend-feedback"><p id="trend-status" class="small muted" role="status" aria-live="polite"></p><p id="trend-error" class="form-message" role="alert" hidden></p><button class="button" id="trend-retry" type="button" hidden>重新读取趋势</button></div>
      <figure class="comparison-figure"><div id="comparison-chart" class="chart comparison-chart"></div><figcaption id="trend-caption">价格使用同一报价货币，纵轴自动缩放；悬停或使用左右方向键查看读数。</figcaption></figure><div id="trend-summary" class="trend-summary"></div><div class="trend-foot"><span>UTC 时间 · 缺失记录处断线 · 历史数据不代表实时价格</span><span id="trend-record-count"></span></div>`;
    container.querySelector('#trend-source').value=group.key;
    container.querySelector('#trend-source').onchange=event=>chooseGroup(event.target.value);
    const find=selector=>container.querySelector(selector);
    function error(message) {find('#trend-error').textContent=message;find('#trend-error').hidden=!message;}
    function setColors() {
      for(const id of Object.keys(settings.colors))if(!settings.ids.includes(id))delete settings.colors[id];
      for(const id of settings.ids)if(settings.colors[id]===undefined)settings.colors[id]=comparisonPalette.findIndex((_,i)=>!Object.values(settings.colors).includes(i));
    }
    function choices() {
      setColors();find('#trend-selected-count').textContent=`${settings.ids.length} / ${Math.min(8,group.items.length)}`;
      find('#trend-choices').innerHTML=group.items.map(({asset:a})=>{
        const id=a.asset.id,selected=settings.ids.includes(id),index=settings.colors[id]??0;
        return `<label class="trend-asset trend-color-${index} ${selected?'selected':''}"><input type="checkbox" data-trend-asset="${h(id)}" ${selected?'checked':''} ${!selected&&settings.ids.length>=8?'disabled':''}><span class="trend-dot" aria-hidden="true"></span><span><strong>${h(a.asset.symbol)}</strong><small title="${h(id)}">${h(id.replace(/^coingecko\./,''))}</small></span></label>`;
      }).join('');
      container.querySelectorAll('[data-trend-asset]').forEach(input=>input.onchange=()=>{
        const id=input.dataset.trendAsset;
        settings.ids=input.checked?[...settings.ids,id]:settings.ids.filter(value=>value!==id);
        choices();load();
      });
    }
    function paint() {
      container.querySelectorAll('[data-trend-mode]').forEach(button=>{
        const active=button.dataset.trendMode===settings.mode;button.classList.toggle('active',active);button.setAttribute('aria-pressed',String(active));
      });
      const series=loaded.map(item=>({...item,color:comparisonPalette[settings.colors[item.id]]}));
      renderComparisonChart(find('#comparison-chart'),series,{mode:settings.mode,quote:group.quote,from:loadedRange?.from,to:loadedRange?.to});
      find('#trend-caption').textContent=settings.mode==='change'?'每个资产以所选区间内第一条有效价格为起点计算涨跌幅；起始记录日期可能不同，详见下方卡片。':'价格使用同一报价货币，纵轴自动缩放；悬停或使用左右方向键查看同一时刻的读数。';
      find('#trend-summary').innerHTML=series.map(item=>{
        const first=item.records[0],last=item.records.at(-1),change=(Number(last.price)/Number(first.price)-1)*100;
        return `<article class="trend-stat trend-color-${settings.colors[item.id]}"><div class="trend-stat-head"><span class="trend-dot" aria-hidden="true"></span><a href="${assetLink(item.id)}">${h(item.label)}</a><span class="mono ${change<0?'negative':'positive'}">${change>0?'+':''}${h(num.format(change))}%</span></div><strong class="mono">${h(price(last.price))}<small>${h(group.quote)}</small></strong><p>${h(date(first.time))} → ${h(date(last.time))} · ${num.format(item.records.length)} 条</p><small class="trend-stat-id">${h(item.id)}</small></article>`;
      }).join('');
    }
    async function load() {
      const current=++request;controller?.abort();controller=new AbortController();const signal=controller.signal;
      error('');loaded=[];loadedRange=null;find('#trend-summary').replaceChildren();find('#trend-record-count').textContent='';find('#trend-retry').hidden=true;
      disposeChart(find('#comparison-chart'));find('#comparison-chart').replaceChildren();
      if(!settings.ids.length){find('#trend-status').textContent='请选择至少一个资产。';find('#comparison-chart').innerHTML=empty('选择资产，开始对比','勾选上方币种，将历史曲线叠加在同一张图中。');return;}
      let range;
      try{range=queryDates(find('#trend-from').value,find('#trend-to').value,group.stream);}
      catch(e){error(e.message);find('#trend-status').textContent='尚未读取趋势，请调整日期。';return;}
      settings.from=find('#trend-from').value;settings.to=find('#trend-to').value;
      const selected=settings.ids.map(id=>group.items.find(item=>item.asset.asset.id===id));
      const results=new Array(selected.length);let next=0,completed=0;
      find('#trend-status').textContent=`正在读取 ${selected.length} 个资产的历史记录…`;
      find('#comparison-chart').innerHTML='<div class="loading-line"></div><p class="loading-text">正在对齐时间轴与历史记录…</p>';
      await Promise.all(Array.from({length:Math.min(2,selected.length)},async()=>{
        while(next<selected.length&&!signal.aborted){
          const i=next++,item=selected[i];
          try{results[i]={records:await loadPoints(item.asset,item.stream,range,signal,12000)};}
          catch(e){results[i]={error:e};}
          if(current===request&&!disposed){completed++;find('#trend-status').textContent=`正在读取历史记录 ${completed} / ${selected.length}…`;}
        }
      }));
      if(disposed||current!==request||signal.aborted)return;
      const failed=[],missing=[];
      selected.forEach((item,i)=>{
        const result=results[i],asset=item.asset.asset;
        if(result.error)failed.push(`${asset.symbol}：${result.error.message}`);
        else if(!result.records.length)missing.push(asset.symbol);
        else loaded.push({id:asset.id,label:asset.symbol,records:result.records,cadence:item.stream.interval_seconds*1000});
      });
      loadedRange=range;paint();
      const count=loaded.reduce((sum,item)=>sum+item.records.length,0);
      find('#trend-status').textContent=`已展示 ${loaded.length} / ${selected.length} 个资产${missing.length?'；'+missing.join('、')+' 在此区间没有记录':''}。`;
      find('#trend-record-count').textContent=`${num.format(count)} 条源记录`;
      if(failed.length){error('部分趋势未能读取。'+failed.join('；'));find('#trend-retry').hidden=false;}
    }
    find('#trend-date-form').onsubmit=event=>{event.preventDefault();load();};
    find('#trend-retry').onclick=()=>load();
    find('#trend-select-all').onclick=()=>{settings.ids=group.items.slice(0,8).map(item=>item.asset.asset.id);choices();load();};
    find('#trend-select-none').onclick=()=>{settings.ids=[];choices();load();};
    container.querySelectorAll('[data-trend-mode]').forEach(button=>button.onclick=()=>{settings.mode=button.dataset.trendMode;if(loadedRange)paint();else container.querySelectorAll('[data-trend-mode]').forEach(n=>{n.classList.toggle('active',n===button);n.setAttribute('aria-pressed',String(n===button));});});
    container.querySelectorAll('[data-trend-days]').forEach(button=>button.onclick=()=>{
      const items=group.items.filter(item=>settings.ids.includes(item.asset.asset.id)),scope=items.length?items:group.items;
      const latest=Math.min(Date.now(),Math.max(...scope.map(item=>Date.parse(item.stream.last_at))));
      const days=Number(button.dataset.trendDays);
      const dates=days?defaultDates({last_at:new Date(latest).toISOString()},days):{from:date(Math.min(...scope.map(item=>Date.parse(item.stream.first_at)))),to:date(latest)};
      find('#trend-from').value=dates.from;find('#trend-to').value=dates.to;
      container.querySelectorAll('[data-trend-days]').forEach(n=>n.classList.toggle('active',n===button));load();
    });
    choices();
    container.querySelectorAll('[data-trend-mode]').forEach(button=>{button.classList.toggle('active',button.dataset.trendMode===settings.mode);button.setAttribute('aria-pressed',String(button.dataset.trendMode===settings.mode));});
    load();
  }
  chooseGroup(state.comparison?.group);
  return cleanup;
}

function updateAssetRows(assets) {
  const needle=state.search.trim().toLowerCase();
  const filtered=assets.filter(a=>`${a.asset.id} ${a.asset.symbol}`.toLowerCase().includes(needle)).filter(a=>state.filter==='all'||state.filter==='abnormal'&&['warning','critical'].includes(a.status)||state.filter==='unknown'&&a.status==='unknown'||state.filter==='configured'&&a.rule);
  filtered.sort((a,b)=>{
    const ap=chosen(a)?.latest,bp=chosen(b)?.latest;
    if(state.sort==='deviation')return (b.deviation_bps===null?-1:Math.abs(Number(b.deviation_bps)))-(a.deviation_bps===null?-1:Math.abs(Number(a.deviation_bps)));
    if(state.sort==='name')return a.asset.symbol.localeCompare(b.asset.symbol)||a.asset.id.localeCompare(b.asset.id);
    if(state.sort==='latest')return new Date(bp?.observed_at??0)-new Date(ap?.observed_at??0);
    return Number(bp?.market_cap_usd??-1)-Number(ap?.market_cap_usd??-1)||a.asset.symbol.localeCompare(b.asset.symbol);
  });
  $('#asset-rows').innerHTML=filtered.map((a,i)=>{
    const s=chosen(a),p=s?.latest,change=a.deviation_bps;
    return `<tr><td>${i+1}</td><td><a class="asset-cell" href="${assetLink(a.asset.id)}">${coin(a.asset,i)}<span><span class="asset-name">${h(a.asset.symbol)}</span><span class="asset-id secondary" title="${h(a.asset.id)}">${h(a.asset.id.replace(/^coingecko\./,''))}</span></span></a></td><td class="mono">${h(price(p?.price))}<span class="secondary">${h(s?quoteFor(a,s):'—')} · ${h(s?s.kind==='daily'?'日快照':'收盘价':'暂无记录')}</span></td><td class="mono ${change===null?'muted':Number(change)<0?'negative':'positive'}">${h(bps(change))}<span class="secondary">${change===null?'设置锚定价后计算':h((Number(change)/100).toFixed(4))+'%'}</span></td><td>${badge(a.status)}<span class="secondary">${a.freshness==='historical'?'历史记录':a.freshness==='future'?'数据时间在未来':a.freshness==='missing'?'暂无记录':'较新记录'}</span></td><td class="mono">${h(amount(p?.market_cap_usd))}<span class="secondary">${s?.kind==='daily'?'该日快照':'此数据源不含市值'}</span></td><td class="mono">${h(datetime(p?.observed_at))}<span class="secondary">${h(s?.source??'尚未导入行情')}</span></td><td><a class="arrow" href="${assetLink(a.asset.id)}" aria-label="查看 ${h(a.asset.symbol)} 详情">↗</a></td></tr>`;
  }).join('');
  const n=$('#asset-empty');n.hidden=filtered.length>0;n.innerHTML=assets.length?empty('没有匹配的币种','尝试其他名称，或切换筛选条件。'):empty('还没有行情数据','完成数据导入后，资产列表和历史趋势会出现在这里。导入步骤见项目 README。');
  $('#asset-count').textContent=`显示 ${filtered.length} / ${assets.length} 个资产`;
  document.querySelectorAll('[data-filter]').forEach(n=>n.classList.toggle('active',n.dataset.filter===state.filter));
}

function defaultDates(stream,days) {
  const last=new Date(stream.last_at),end=new Date(Date.UTC(last.getUTCFullYear(),last.getUTCMonth(),last.getUTCDate())+86400000),start=new Date(end.getTime()-days*86400000);
  return {from:date(start),to:date(last)};
}
function queryDates(fromValue,toValue,stream) {
  const from=new Date(fromValue+'T00:00:00Z'),dayEnd=new Date(toValue+'T00:00:00Z').getTime()+86400000,to=new Date(Math.min(dayEnd,Date.now()));
  const maxDays=stream.kind==='daily'?3660:31;
  if(!Number.isFinite(from.getTime())||!Number.isFinite(to.getTime())||to<=from||to-from>maxDays*86400000)throw new APIError(`请选择有效日期范围；此数据源单次最多查询 ${maxDays} 天。`);
  return {from:from.toISOString(),to:to.toISOString()};
}

async function loadPoints(a,stream,range,signal,maxPoints=50000) {
  const points=[];let cursor='';const seen=new Set();
  for(;;) {
    const params=new URLSearchParams({asset_id:a.asset.id,source:stream.source,from:range.from,to:range.to,limit:'2000'});
    if(cursor)params.set('cursor',cursor);
    if(stream.kind==='ohlc')params.set('interval',stream.interval_seconds+'s');
    const result=await api((stream.kind==='daily'?'/market-snapshots?':'/trends?')+params,{signal});
    const rows=stream.kind==='daily'?result.points:result.candles;
    for(const p of rows)points.push(stream.kind==='daily'?{...p,price:p.price_usd,observed_at:p.time}:{...p,price:p.close,observed_at:p.source_time??new Date(new Date(p.time).getTime()+stream.interval_seconds*1000).toISOString(),market_cap_usd:null,volume_24h_usd:null});
    if(points.length>maxPoints)throw new APIError(`当前区间超过 ${num.format(maxPoints)} 条记录，请缩小日期范围。`);
    if(!result.next_cursor)break;
    if(seen.has(result.next_cursor))throw new APIError('服务返回了重复的分页位置，请重新读取。');
    seen.add(result.next_cursor);cursor=result.next_cursor;
  }
  return points;
}

async function renderDetail(a,signal,options={}) {
	const parentSignal=signal;
	state.detailController?.abort();
	const child=new AbortController();state.detailController=child;
	const abortChild=()=>child.abort();
	if(parentSignal.aborted)child.abort();else parentSignal.addEventListener('abort',abortChild,{once:true});
	child.signal.addEventListener('abort',()=>parentSignal.removeEventListener('abort',abortChild),{once:true});
	signal=child.signal;
  const selected=options.streamIndex??Math.max(0,a.streams.findIndex(s=>a.selected&&s.kind===a.selected.kind&&s.source===a.selected.source&&s.interval_seconds===a.selected.interval_seconds));
  const stream=a.streams[selected];document.title=`${a.asset.symbol} · Depeg`;
  if(!stream){$('#main').innerHTML=empty('该币种还没有行情记录','导入对应的日快照或 OHLC 行情后再查看趋势。','<a class="button" href="#/overview">返回总览</a>');return;}
  const quote=quoteFor(a,stream),dates=options.dates??defaultDates(stream,30),ruleMatches=a.rule&&a.rule.kind===stream.kind&&a.rule.source===stream.source&&a.rule.interval_seconds===stream.interval_seconds;
  const anchor=ruleMatches?a.rule.anchor_price:(stream.kind==='daily'&&a.asset.quote_currency!=='USD'?null:a.asset.anchor_price);
  const warning=ruleMatches?a.rule.warning_bps:50,critical=ruleMatches?a.rule.critical_bps:100;
  let activeAnchor=anchor,activeWarning=warning,activeCritical=critical,dirty=!ruleMatches,currentRange=null,points=[],loading=true,fetchRevision=0;
  $('#main').innerHTML=`<div class="breadcrumb"><a href="#/overview">市场总览</a><span>/</span><span>${h(a.asset.symbol)}</span></div><div class="page-head"><div class="detail-title">${coin(a.asset)}<div><h1>${h(a.asset.symbol)} <span class="small muted">/ ${h(quote)}</span></h1><p class="mono">${h(a.asset.id)}</p></div></div><div class="head-actions"><button class="button" id="export" disabled>↓ 导出 CSV</button></div></div><div class="notice"><span class="symbol" aria-hidden="true">ⓘ</span><span>数据覆盖 ${h(date(stream.first_at))} 至 ${h(date(stream.last_at))} · ${h(streamLabel(stream))}。${stream.kind==='daily'?'每日价格快照不作为 OHLC 使用。':'曲线展示已存周期的收盘价，横轴按区间开始时间标注。'} 数据缺口不会补零。</span></div><div class="metrics" id="detail-metrics"></div><div class="detail-grid"><section class="panel"><div class="panel-head"><div><h2>历史趋势</h2><p id="point-count">正在读取实际记录…</p></div><select id="stream-select" aria-label="数据来源">${a.streams.map((s,i)=>`<option value="${i}" ${i===selected?'selected':''}>${h(streamLabel(s))}</option>`).join('')}</select></div><div class="chart-controls"><div class="tabs" role="group" aria-label="图表指标"><button class="tab" data-field="price">价格</button><button class="tab" data-field="deviation" ${anchor?'':'disabled'}>偏离</button>${stream.kind==='daily'?'<button class="tab" data-field="market_cap_usd">市值</button><button class="tab" data-field="volume_24h_usd">24h 成交额</button>':''}</div><div class="range-tabs">${(stream.kind==='daily'?[7,30,90,365]:[1,7,30]).map(d=>`<button class="range-button" data-days="${d}">${d===365?'1Y':d+'D'}</button>`).join('')}</div></div><form id="date-form" class="date-form"><label>开始日期 · UTC<input type="date" id="from-date" required value="${dates.from}" min="1970-01-01" max="${date(new Date())}"></label><span class="muted small">至</span><label>结束日期 · UTC<input type="date" id="to-date" required value="${dates.to}" min="1970-01-01" max="${date(new Date())}"></label><button class="button" type="submit">应用</button></form><p class="form-message panel-body" id="date-error" hidden role="alert"></p><div id="chart" class="chart"><p class="loading-text">正在读取趋势…</p></div><div class="chart-footer"><span><i class="legend-line"></i>实际观测</span><span id="anchor-legend" ${anchor?'':'hidden'}><i class="legend-line anchor"></i>锚定价 / 零偏离</span><span>鼠标悬停或方向键查看 · UTC</span></div></section><aside class="panel"><div class="panel-head"><div><h2>我的监测阈值</h2><p>仅对当前账号和所选来源生效</p></div></div><div class="panel-body"><form id="rule-form" class="rule-form"><label>锚定价 · ${h(quote)}<input id="anchor" inputmode="decimal" type="text" required maxlength="39" pattern="(?:0|[1-9][0-9]{0,19})(?:\\.[0-9]{1,18})?" placeholder="请确认币种的锚定价" value="${h(anchor??'')}"></label><div class="row"><label>预警偏离 · %<input id="warning" type="number" min="0.01" max="999.99" step="0.01" required value="${warning/100}"></label><label>严重偏离 · %<input id="critical" type="number" min="0.02" max="1000" step="0.01" required value="${critical/100}"></label></div><p class="hint">以价格高于或低于锚定价的绝对偏离判断。0.5% = 50 bps。达到阈值即命中。</p><p class="form-message" id="rule-message" role="status"></p><button class="button primary" type="submit">保存阈值</button><button class="button" type="button" id="detect" disabled>检测当前区间</button><p class="hint" id="detect-hint">${ruleMatches?'检测使用已保存的阈值；历史记录按观测时间保存。':'请先确认锚定价并保存阈值，再检测当前区间。'}</p><div class="rule-meta"><div class="definition"><span>数据来源</span><span>${h(stream.source)}</span></div><div class="definition"><span>记录总数</span><span>${num.format(stream.count)}</span></div><div class="definition"><span>最新观测</span><span>${h(datetime(stream.latest.observed_at))} UTC</span></div><div class="definition"><span>说明</span><span>阈值检测 · 论文预测待接入</span></div></div></form></div></aside></div><section class="panel" id="detail-events"><div class="panel-head"><div><h2>阈值命中记录</h2><p>当前账号 · ${h(a.asset.symbol)} · 包括此前阈值版本</p></div></div><div class="events-note">每条记录对应一次超过阈值的观测，连续多条命中不代表多次独立脱锚事件。缺失记录期间的状态未知。</div><div id="events-content"></div></section>`;
  if(!['price','deviation',...(stream.kind==='daily'?['market_cap_usd','volume_24h_usd']:[])].includes(state.chartField)||state.chartField==='deviation'&&!anchor)state.chartField='price';
  function draw() {
    document.querySelectorAll('[data-field]').forEach(n=>n.classList.toggle('active',n.dataset.field===state.chartField));
    renderChart($('#chart'),points,{field:state.chartField,anchor:Number(activeAnchor),warning:activeWarning,critical:activeCritical,cadence:stream.interval_seconds*1000,quote:state.chartField==='deviation'?'bps':['market_cap_usd','volume_24h_usd'].includes(state.chartField)?'USD':quote});
  }
  function setDetect() {const b=$('#detect');if(b)b.disabled=dirty||loading||!a.rule||!points.length||signal.aborted;}
  async function fetchRange() {
    const revision=++fetchRevision;loading=true;setDetect();$('#export').disabled=true;$('#date-error').hidden=true;$('#chart').innerHTML='<p class="loading-text">正在读取全部分页记录…</p>';
    try {
      const range=queryDates($('#from-date').value,$('#to-date').value,stream);
      const loaded=await loadPoints(a,stream,range,signal);if(signal.aborted||revision!==fetchRevision)return;
      points=loaded;state.points=points;currentRange=range;loading=false;
      let high=null,low=null,maxDev=null,gaps=0;
      for(let i=0;i<points.length;i++){const p=Number(points[i].price);high=high===null?p:Math.max(high,p);low=low===null?p:Math.min(low,p);if(Number(activeAnchor)>0)maxDev=Math.max(maxDev??0,Math.abs((p-Number(activeAnchor))/Number(activeAnchor)*10000));if(i&&new Date(points[i].time)-new Date(points[i-1].time)>stream.interval_seconds*1800)gaps++;}
      $('#point-count').textContent=`${num.format(points.length)} 条实际记录 · ${gaps} 处采样缺口 · ${num.format(stream.count)} 条源记录`;
      $('#detail-metrics').innerHTML=`<div class="metric"><div class="label">区间最后${stream.kind==='daily'?'快照价':'收盘价'}</div><div class="value mono">${h(price(points.at(-1)?.price))}</div><div class="foot">${h(quote)} · ${h(datetime(points.at(-1)?.observed_at))}</div></div><div class="metric"><div class="label">区间最高记录价</div><div class="value mono">${h(price(high))}</div><div class="foot">最低 ${h(price(low))} ${h(quote)}</div></div><div class="metric"><div class="label">区间最大绝对偏离</div><div class="value mono">${maxDev===null?'—':h(num.format(maxDev))}</div><div class="foot">${maxDev===null?'确认锚定价后计算':'bps · 按当前保存的锚定价'}</div></div><div class="metric"><div class="label">区间有效记录</div><div class="value mono">${num.format(points.length)}</div><div class="foot">${gaps} 处缺口 · 不插值、不补零</div></div>`;
      draw();$('#export').disabled=!points.length;setDetect();
    } catch(error) {
      if(error.name==='AbortError'||signal.aborted||revision!==fetchRevision)return;
      points=[];state.points=[];currentRange=null;loading=false;setDetect();$('#date-error').textContent=error.message;$('#date-error').hidden=false;$('#chart').innerHTML=empty('未能读取趋势',error.message);$('#point-count').textContent='读取未完成';$('#detail-metrics').replaceChildren();
    }
  }
  $('#stream-select').onchange=e=>renderDetail(a,parentSignal,{streamIndex:Number(e.target.value)});
  $('#date-form').onsubmit=e=>{e.preventDefault();document.querySelectorAll('[data-days]').forEach(n=>n.classList.remove('active'));fetchRange();};
  document.querySelectorAll('[data-days]').forEach(n=>n.onclick=()=>{const d=defaultDates(stream,Number(n.dataset.days));$('#from-date').value=d.from;$('#to-date').value=d.to;document.querySelectorAll('[data-days]').forEach(b=>b.classList.toggle('active',b===n));fetchRange();});
  document.querySelectorAll('[data-field]').forEach(n=>n.onclick=()=>{state.chartField=n.dataset.field;if(!loading)draw();});
  $('#rule-form').oninput=()=>{dirty=true;setDetect();$('#rule-message').textContent='阈值有改动，请保存后再检测。';$('#rule-message').classList.remove('success');};
  $('#rule-form').onsubmit=async e=>{
    e.preventDefault();const button=e.currentTarget.querySelector('[type=submit]'),message=$('#rule-message');button.disabled=true;message.textContent='';
    const inputs=[...e.currentTarget.querySelectorAll('input')];inputs.forEach(n=>n.disabled=true);
    try {
      const w=Number($('#warning').value)*100,c=Number($('#critical').value)*100;
      if(Math.abs(w-Math.round(w))>1e-7||Math.abs(c-Math.round(c))>1e-7||c<=w)throw new APIError('严重偏离阈值必须大于预警阈值，百分比最多填写两位小数。');
      const rule=await api('/monitor/rules',{method:'POST',signal,body:{asset_id:a.asset.id,kind:stream.kind,source:stream.source,interval_seconds:stream.interval_seconds,anchor_price:$('#anchor').value.trim(),warning_bps:Math.round(w),critical_bps:Math.round(c)}});
      if(signal.aborted)return;
      a.rule=rule;activeAnchor=rule.anchor_price;activeWarning=rule.warning_bps;activeCritical=rule.critical_bps;dirty=false;state.overview=null;message.textContent='阈值已保存，仅对你的账号生效。';message.classList.add('success');$('#detect-hint').textContent='使用已保存的阈值检测当前区间，重复检测不会重复写入。';$('[data-field="deviation"]').disabled=false;$('#anchor-legend').hidden=false;setDetect();await fetchRange();
    } catch(error){if(error.name!=='AbortError'){message.textContent=error.message;message.classList.remove('success');}}finally{button.disabled=false;inputs.forEach(n=>n.disabled=false);setDetect();}
  };
  $('#detect').onclick=async()=>{
    if(dirty||!currentRange||!a.rule)return;
    const button=$('#detect'),message=$('#rule-message');button.disabled=true;button.textContent='正在检测…';
    try {
      const result=await api('/monitor/replay',{method:'POST',signal,body:{asset_id:a.asset.id,...currentRange,rule_version:a.rule.version}});
      if(signal.aborted)return;
      message.textContent=`已检查 ${result.scanned} 条，命中 ${result.matched} 条，新增 ${result.inserted} 条。`;message.classList.add('success');toast('区间检测完成，记录已保存。');await loadEvents($('#events-content'),a.asset.id,signal);
    } catch(error){if(error.name!=='AbortError'){message.textContent=error.message;message.classList.remove('success');}}
    finally{button.textContent='检测当前区间';setDetect();}
  };
  $('#export').onclick=()=>{
    const columns=['asset_id','source','kind','interval_seconds','time_utc','observed_at_utc','price','quote_currency','open','high','low','close','volume','market_cap_usd','volume_24h_usd','quality'];
    const rows=points.map(p=>[a.asset.id,stream.source,stream.kind,stream.interval_seconds,p.time,p.observed_at,p.price,quote,p.open,p.high,p.low,p.close,p.volume,p.market_cap_usd,p.volume_24h_usd,p.quality_flags?.join(';')??p.quality]);
    const content='\uFEFF'+[columns,...rows].map(row=>row.map(csvCell).join(',')).join('\r\n');
    const url=URL.createObjectURL(new Blob([content],{type:'text/csv;charset=utf-8'})),link=document.createElement('a');link.href=url;link.download=`${a.asset.id.replace(/[^a-zA-Z0-9_.-]/g,'_')}-${stream.kind}-${date(currentRange.from)}.csv`;document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),1000);toast(`已导出 ${points.length} 条实际记录。`);
  };
  const events=loadEvents($('#events-content'),a.asset.id,signal);await fetchRange();await events;
}

async function loadEvents(container,assetID,signal) {
  let cursor='',events=[];container.innerHTML='<p class="loading-text">正在读取检测记录…</p>';
  async function page() {
    try {
      const params=new URLSearchParams({limit:'50'});if(assetID)params.set('asset_id',assetID);if(cursor)params.set('cursor',cursor);
      const result=await api('/monitor/events?'+params,{signal});if(signal.aborted||!container.isConnected)return;
      events.push(...result.events);cursor=result.next_cursor;
      if(!events.length){container.innerHTML=empty('还没有阈值命中记录','选择币种、保存阈值并检测历史区间后，超出阈值的观测会出现在这里。','<a class="button primary" href="#/overview">去选择币种</a>');return;}
      container.innerHTML=`<div class="table-scroll"><table><thead><tr><th>观测时间 · UTC</th><th>资产 / 来源</th><th>观测价 / 锚定价</th><th>偏离</th><th>程度</th><th>阈值版本 / 检测时间</th></tr></thead><tbody>${events.map(event=>`<tr><td class="mono">${h(datetime(event.observed_at))}<span class="secondary">${event.kind==='daily'?'日快照':'收盘价'} · ${h(event.direction==='below'?'低于锚定价':'高于锚定价')}</span></td><td><a href="${assetLink(event.asset_id)}">${h(event.asset_id.replace(/^coingecko\./,''))}</a><span class="secondary">${h(event.source)}</span></td><td class="mono">${h(price(event.price))}<span class="secondary">锚定 ${h(price(event.anchor_price))}</span></td><td class="mono ${event.direction==='below'?'negative':'positive'}">${h(bps(event.deviation_bps))}</td><td>${badge(event.severity)}<span class="secondary">预警 ${event.warning_bps/100}% / 严重 ${event.critical_bps/100}%</span></td><td class="mono"><span title="${h(event.rule_version)}">${h(event.rule_version.slice(0,8))}</span><span class="secondary">${h(datetime(event.detected_at))}</span></td></tr>`).join('')}</tbody></table></div><div class="table-foot"><span>已显示 ${events.length} 条 · 按观测时间倒序</span>${cursor?'<button class="button" data-more>加载更多</button>':'<span>已显示全部记录</span>'}</div>`;
      if(cursor)container.querySelector('[data-more]').onclick=e=>{e.currentTarget.disabled=true;page();};
    } catch(error){if(error.name!=='AbortError'&&!signal.aborted&&container.isConnected){const box=document.createElement('div');box.className='notice error';box.textContent=error.message;const retry=document.createElement('button');retry.className='button';retry.textContent='重试读取';retry.onclick=()=>{box.remove();page();};box.append(retry);container.append(box);}}
  }
  await page();
}

function renderEventPage(overview,signal) {
  document.title='阈值记录 · Depeg';
  $('#main').innerHTML=`<div class="page-head"><div><h1>阈值记录</h1><p>查看你发起的历史检测结果，追溯每次观测使用的规则。</p></div><button class="button" id="reload-events">↻ 刷新记录</button></div><div class="notice"><span class="symbol" aria-hidden="true">ⓘ</span><span>每条记录对应一次超阈值观测。检测结果按账号保存，调整阈值后，旧版本记录仍可回看。</span></div><section class="panel"><div class="panel-head"><div><h2>历史命中记录</h2><p>更改阈值后，旧版本记录会保留</p></div><select id="event-asset" aria-label="筛选记录的币种"><option value="">全部币种</option>${overview.assets.map(a=>`<option value="${h(a.asset.id)}">${h(a.asset.symbol)} · ${h(a.asset.id.replace(/^coingecko\./,''))}</option>`).join('')}</select></div><div id="all-events"></div></section>`;
  let child=new AbortController();const abort=()=>child.abort();signal.addEventListener('abort',abort,{once:true});
  const reload=()=>{child.abort();child=new AbortController();if(signal.aborted){child.abort();return;}loadEvents($('#all-events'),$('#event-asset').value,child.signal);};
  $('#event-asset').onchange=reload;$('#reload-events').onclick=reload;reload();
}

function renderGuide() {
  document.title = '数据说明 · Depeg';
  $('#main').innerHTML = `
    <div class="page-head"><div><h1>数据说明</h1><p>了解价格偏离的计算方式、数据来源与检测规则。</p></div></div>
    <div class="guide-layout">
      <div class="guide-content">
        <article class="guide-lead">
          <h2>怎样计算价格偏离？</h2>
          <p>将记录价格与确认的锚定价比较。正数表示高于锚定价，负数表示低于锚定价，1 bps 等于 0.01%。</p>
          <code class="guide-formula">偏离 bps = (记录价格 ÷ 锚定价 − 1) × 10,000</code>
          <div class="guide-example">
            <div><span>锚定价</span><strong>1 USD</strong></div>
            <div><span>记录价格</span><strong>0.995 USD</strong></div>
            <div><span>价格偏离</span><strong class="negative">−50 bps</strong></div>
          </div>
          <p>上例相当于偏离 −0.5%。检测时以偏离的绝对值与阈值比较，达到预警或严重阈值即命中。</p>
        </article>
        <details class="guide-section" open>
          <summary>为什么需要确认锚定价？</summary>
          <p>稳定币的锚定对象可能是美元、其他货币或其他资产。锚定价应使用当前数据来源的报价货币。在详情页确认并保存后，才能根据你的规则计算偏离。</p>
          <p>未确认锚定价时，页面显示“锚定价待确认”。个人设置按账号保存，原始资产信息另行保留。</p>
        </details>
        <details class="guide-section">
          <summary>价格与时间代表什么？</summary>
          <p>所有时间均以 UTC 展示。总览价格来自所选来源的最后一条记录，最新数据日期表示数据本身的观测时间。超过三个采样周期的观测标记为历史记录。</p>
          <p>日快照保留原始观测时刻。OHLC 横轴使用区间开始时间，最新观测时间优先使用来源的收盘时间，缺少该字段时采用开始时间加采样周期。</p>
        </details>
        <details class="guide-section">
          <summary>如何理解来源、缺口与空值？</summary>
          <p>日快照和 OHLC 各自展示。切换来源时，曲线读取对应来源的记录。缺失价格处断线，缺失市值或成交量显示为“—”。</p>
          <p>日快照的 24 小时成交额与 OHLC 的成交量是不同字段，图表和 CSV 导出均保留各自的数据口径。</p>
        </details>
        <details class="guide-section">
          <summary>阈值记录如何保存？</summary>
          <p>每个账号可以为一个币种保存一套规则，规则同时绑定数据来源与采样周期。保存后检测所选区间，超过阈值的实际观测会写入该账号的记录。</p>
          <p>相同规则重复检测同一条观测时，记录按唯一标识更新。调整规则会生成新版本，旧记录继续保留。连续多条命中可能来自同一次持续偏离。</p>
        </details>
        <details class="guide-section">
          <summary>当前接入了哪些研究功能？</summary>
          <p>当前提供历史行情查询、价格偏离计算和手动区间检测。刷新会重新读取项目数据库，检测由你在币种详情页发起。</p>
          <p>论文模型推理、实时采集和消息通知属于后续接入内容。当前结果以历史观测与阈值规则为依据。</p>
        </details>
      </div>
      <aside class="guide-steps">
        <h2>开始一次检测</h2>
        <ol><li>在市场总览选择币种。</li><li>确认数据来源与日期范围。</li><li>填写锚定价、预警和严重阈值，保存规则。</li><li>点击“检测当前区间”，在阈值记录中查看结果。</li></ol>
        <a class="button primary" href="#/overview">去选择币种</a>
      </aside>
    </div>`;
}

window.addEventListener('hashchange',()=>renderRoute());
async function start() {
  if(!state.tokens?.access_token){renderAuth();return;}
  try { state.user=await api('/auth/me');renderShell();await renderRoute(); }
  catch(error){if(state.user)return;renderAuth(error.message);}
}
start();
