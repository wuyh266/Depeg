const NS = 'http://www.w3.org/2000/svg';
const observers = new WeakMap();
const number = new Intl.NumberFormat('en-US', {maximumFractionDigits:6});
const display = v => v!==0&&(Math.abs(v)<.000001||Math.abs(v)>=1e12)?v.toExponential(2):number.format(v);
const el = (tag, attrs = {}, text) => {
  const n = document.createElementNS(NS, tag);
  for (const [key, value] of Object.entries(attrs)) n.setAttribute(key, String(value));
  if (text !== undefined) n.textContent = text;
  return n;
};

// Keep local extrema when reducing SVG work. Full records remain available for export.
export function sampleExtrema(points, budget = 700) {
  if (points.length <= budget) return points;
  const out = [points[0]], step = Math.ceil(points.length / Math.max(1, Math.floor(budget/2)));
  for (let i=1; i<points.length-1; i+=step) {
    const bucket=points.slice(i,Math.min(i+step,points.length-1));
    let low=bucket[0], high=bucket[0];
    for (const p of bucket) { if(p.y<low.y)low=p;if(p.y>high.y)high=p; }
    if(low.t<high.t)out.push(low,high);else if(low.t>high.t)out.push(high,low);else out.push(low);
  }
  out.push(points.at(-1));return out;
}

export function splitSegments(points, cadence) {
  const segments=[];let current=[];
  for (const p of points) {
    if(!Number.isFinite(p.y)) { if(current.length)segments.push(current);current=[];continue; }
    if(current.length && p.t-current.at(-1).t>cadence*1.8) { segments.push(current);current=[]; }
    current.push(p);
  }
  if(current.length)segments.push(current);return segments;
}

export function renderChart(container, records, {field='price', anchor=null, warning=50, critical=100, cadence=86400000, quote='USD'} = {}) {
  observers.get(container)?.disconnect();
  observers.delete(container);
  container.replaceChildren();
  const deviation=field==='deviation';
  const all=records.map(p=>({t:new Date(p.time).getTime(),y:deviation?(anchor>0?(Number(p.price)-anchor)/anchor*10000:NaN):Number(p[field]??NaN)}));
  const valid=all.filter(p=>Number.isFinite(p.y)&&Number.isFinite(p.t));
  if(!valid.length) {
    const n=document.createElement('div');n.className='empty';n.textContent=deviation?'设置锚定价后可查看偏离走势。':'当前区间没有可绘制的数据。';container.append(n);return;
  }
  const layout=getComputedStyle(container),measured=container.clientWidth-parseFloat(layout.paddingLeft)-parseFloat(layout.paddingRight);
  const width=Math.max(320,Math.min(1000,measured)),height=300,pad={l:73,r:23,t:30,b:40},plotW=width-pad.l-pad.r,plotH=height-pad.t-pad.b;
  let ymin=Infinity,ymax=-Infinity;for(const p of valid){ymin=Math.min(ymin,p.y);ymax=Math.max(ymax,p.y);}
  const ref=deviation?0:(field==='price'&&anchor>0?anchor:null);
  if(ref!==null){ymin=Math.min(ymin,ref);ymax=Math.max(ymax,ref);}
  const span=ymax-ymin||Math.max(Math.abs(ymin)*.002,.0001);ymin-=span*.16;ymax+=span*.16;
  const tmin=all[0].t,tmax=all.at(-1).t,x=t=>pad.l+(t-tmin)/(tmax-tmin||1)*plotW,y=v=>pad.t+(ymax-v)/(ymax-ymin)*plotH;
  const svg=el('svg',{viewBox:`0 0 ${width} ${height}`,role:'img','aria-label':`${deviation?'价格偏离':field==='market_cap_usd'?'市值':field==='volume_24h_usd'?'24 小时成交额':'价格'}历史走势，${valid.length} 条有效记录`,tabindex:'0'});
  svg.append(el('title',{},'使用左右方向键逐条查看数据，时间使用 UTC。缺失数据处断开曲线。'));
  for(let i=0;i<=4;i++) {
    const v=ymin+(ymax-ymin)*i/4,py=y(v);
    svg.append(el('line',{x1:pad.l,x2:width-pad.r,y1:py,y2:py,stroke:'#e5e9ed','stroke-dasharray':'3 5'}));
    const label=Math.abs(v)>=1e12?display(v):Math.abs(v)>=1e9?`${number.format(v/1e9)}B`:Math.abs(v)>=1e6?`${number.format(v/1e6)}M`:Math.abs(v)>=1e3?`${number.format(v/1e3)}K`:display(v);
    svg.append(el('text',{x:pad.l-12,y:py+4,'text-anchor':'end'},label));
  }
  for(let i=0;i<=4;i++) {
    const t=tmin+(tmax-tmin)*i/4,date=new Date(t),short=tmax-tmin<2*86400000?date.toISOString().slice(11,16):date.toISOString().slice(5,10);
    svg.append(el('text',{x:pad.l+i/4*plotW,y:height-12,'text-anchor':'middle'},short));
  }
  svg.append(el('text',{x:pad.l,y:15},deviation?'偏离 / bps':quote));
  if(ref!==null) {
    svg.append(el('line',{x1:pad.l,x2:width-pad.r,y1:y(ref),y2:y(ref),stroke:'#b58c24','stroke-dasharray':'6 5',opacity:'.75'}));
    const bands=deviation?[warning,-warning,critical,-critical]:field==='price'?[anchor*(1+warning/10000),anchor*(1-warning/10000),anchor*(1+critical/10000),anchor*(1-critical/10000)]:[];
    for(const v of bands)if(v>ymin&&v<ymax)svg.append(el('line',{x1:pad.l,x2:width-pad.r,y1:y(v),y2:y(v),stroke:'#b58c24','stroke-dasharray':'2 7',opacity:'.35'}));
  }
  const segments=splitSegments(all,cadence);
  const budget=Math.max(20,Math.floor(1000/Math.max(1,segments.length)));
  for(const segment of segments) {
    const points=sampleExtrema(segment,budget);
    if(points.length===1)svg.append(el('circle',{cx:x(points[0].t),cy:y(points[0].y),r:2.5,fill:'#4b8f29'}));
    else svg.append(el('path',{d:points.map((p,i)=>`${i?'L':'M'}${x(p.t).toFixed(2)},${y(p.y).toFixed(2)}`).join(' '),fill:'none',stroke:'#4b8f29','stroke-width':2,'stroke-linejoin':'round','vector-effect':'non-scaling-stroke'}));
  }
  const cross=el('line',{x1:0,x2:0,y1:pad.t,y2:height-pad.b,stroke:'#8a969f','stroke-dasharray':'4 4',visibility:'hidden'});
  const dot=el('circle',{cx:0,cy:0,r:4,fill:'#4b8f29',stroke:'#ffffff','stroke-width':2,visibility:'hidden'});
  const tooltip=el('g',{visibility:'hidden'}),box=el('rect',{width:236,height:51,rx:6,class:'chart-tooltip'}),dateText=el('text',{x:10,y:18}),valueText=el('text',{x:10,y:37});tooltip.append(box,dateText,valueText);
  const overlay=el('rect',{x:pad.l,y:pad.t,width:plotW,height:plotH,fill:'transparent'});
  svg.append(cross,dot,tooltip,overlay);container.append(svg);
  let focused=valid.length-1;
  function show(index) {
    focused=Math.max(0,Math.min(valid.length-1,index));const p=valid[focused],px=x(p.t),py=y(p.y);
    cross.setAttribute('x1',px);cross.setAttribute('x2',px);cross.setAttribute('visibility','visible');
    dot.setAttribute('cx',px);dot.setAttribute('cy',py);dot.setAttribute('visibility','visible');
    tooltip.setAttribute('transform',`translate(${Math.max(pad.l,Math.min(px-118,width-pad.r-236))},${pad.t+5})`);tooltip.setAttribute('visibility','visible');
    dateText.textContent=`${new Date(p.t).toISOString().slice(0,16).replace('T',' ')} UTC`;
    valueText.textContent=`${display(p.y)} ${deviation?'bps':quote}`;
  }
  overlay.addEventListener('pointermove',event=>{
    const rect=svg.getBoundingClientRect(),px=(event.clientX-rect.left)/rect.width*width,target=tmin+(px-pad.l)/plotW*(tmax-tmin);
    let lo=0,hi=valid.length-1;while(lo<hi){const mid=(lo+hi)>>1;if(valid[mid].t<target)lo=mid+1;else hi=mid;}
    if(lo>0&&Math.abs(valid[lo-1].t-target)<Math.abs(valid[lo].t-target))lo--;show(lo);
  });
  overlay.addEventListener('pointerleave',()=>{for(const n of[cross,dot,tooltip])n.setAttribute('visibility','hidden');});
  svg.addEventListener('focus',()=>show(focused));
  svg.addEventListener('keydown',event=>{if(event.key==='ArrowLeft'||event.key==='ArrowRight'){event.preventDefault();show(focused+(event.key==='ArrowLeft'?-1:1));}});
  if(typeof ResizeObserver!=='undefined') {
    let lastWidth=container.clientWidth;
    const observer=new ResizeObserver(()=>{
      if(!container.isConnected){observer.disconnect();observers.delete(container);return;}
      if(Math.abs(container.clientWidth-lastWidth)>1){lastWidth=container.clientWidth;renderChart(container,records,{field,anchor,warning,critical,cadence,quote});}
    });
    observers.set(container,observer);observer.observe(container);
  }
}

export function csvCell(value) {
  let s=value===null||value===undefined?'':String(value);
  if(/^[=+@\-\t\r]/.test(s)&&!/^\-?[0-9]+(?:\.[0-9]+)?$/.test(s))s="'"+s;
  return `"${s.replaceAll('"','""')}"`;
}

export function disposeChart(container) {
  observers.get(container)?.disconnect();
  observers.delete(container);
}

// Normalization is for display only; source decimal strings remain on each point.
export function comparisonSeries(series, mode = 'price') {
  return series.map(s => {
    const points = s.records.map(record => ({
      t: new Date(record.time).getTime(),
      price: record.price,
      y: record.price === null || record.price === undefined || record.price === '' ? NaN : Number(record.price)
    })).filter(p => Number.isFinite(p.t)).sort((a, b) => a.t - b.t);
    const base = points.find(p => Number.isFinite(p.y) && p.y > 0)?.y;
    return {...s, points: points.map(p => ({...p, y: mode === 'change' ? (base > 0 ? (p.y / base - 1) * 100 : NaN) : p.y}))};
  });
}

export function renderComparisonChart(container, series, {mode = 'price', quote = 'USD', from, to} = {}) {
  disposeChart(container);
  container.replaceChildren();
  const prepared = comparisonSeries(series, mode);
  const validSeries = prepared.map(s => ({...s, valid: s.points.filter(p => Number.isFinite(p.y))})).filter(s => s.valid.length);
  if (!validSeries.length) {
    const message = document.createElement('div');
    message.className = 'empty';
    message.textContent = '当前选择和日期范围内没有可绘制的数据。';
    container.append(message);
    return;
  }
  const width = Math.max(280, Math.min(1400, container.clientWidth)), height = 390;
  const pad = {l: 76, r: 22, t: 32, b: 45}, plotW = width - pad.l - pad.r, plotH = height - pad.t - pad.b;
  let ymin = Infinity, ymax = -Infinity, first = Infinity, last = -Infinity;
  for (const s of validSeries) for (const p of s.valid) {
    ymin = Math.min(ymin, p.y); ymax = Math.max(ymax, p.y);
    first = Math.min(first, p.t); last = Math.max(last, p.t);
  }
  if (mode === 'change') { ymin = Math.min(0, ymin); ymax = Math.max(0, ymax); }
  const span = ymax - ymin || Math.max(Math.abs(ymin) * .002, .0001);
  ymin -= span * .15; ymax += span * .15;
  const parsedFrom = Date.parse(from), parsedTo = Date.parse(to);
  const tmin = Number.isFinite(parsedFrom) ? Math.min(parsedFrom, first) : first;
  const tmax = Number.isFinite(parsedTo) ? Math.max(parsedTo, last) : last;
  const x = t => pad.l + (t - tmin) / (tmax - tmin || 1) * plotW;
  const y = value => pad.t + (ymax - value) / (ymax - ymin) * plotH;
  const svg = el('svg', {viewBox: `0 0 ${width} ${height}`, role: 'img', tabindex: '0', 'aria-label': `${validSeries.map(s => s.label).join('、')}历史${mode === 'change' ? '区间涨跌幅' : '价格'}趋势对比`});
  svg.append(el('title', {}, '横轴为 UTC 时间。悬停或使用左右方向键查看同一时刻的各资产读数；缺失记录不插值。'));
  for (let i = 0; i <= 4; i++) {
    const value = ymin + (ymax - ymin) * i / 4, py = y(value);
    svg.append(el('line', {x1: pad.l, x2: width - pad.r, y1: py, y2: py, stroke: '#e5e9ed', 'stroke-dasharray': '3 5'}));
    svg.append(el('text', {x: pad.l - 12, y: py + 4, 'text-anchor': 'end'}, display(value) + (mode === 'change' ? '%' : '')));
  }
  const ticks = width < 480 ? 2 : 4;
  for (let i = 0; i <= ticks; i++) {
    const t = tmin + (tmax - tmin) * i / ticks;
    const iso = new Date(t).toISOString();
    const label = tmax - tmin > 365 * 86400000 ? iso.slice(0, 7) : tmax - tmin < 2 * 86400000 ? iso.slice(11, 16) : iso.slice(5, 10);
    svg.append(el('text', {x: pad.l + i / ticks * plotW, y: height - 17, 'text-anchor': 'middle'}, label));
  }
  svg.append(el('text', {x: pad.l, y: 16}, mode === 'change' ? '区间涨跌幅 / %' : `价格 / ${quote}`));
  svg.append(el('text', {x: width - pad.r, y: 16, 'text-anchor': 'end'}, 'UTC'));
  const reference = mode === 'change' ? 0 : quote === 'USD' && ymin <= 1 && ymax >= 1 ? 1 : null;
  if (reference !== null) svg.append(el('line', {x1: pad.l, x2: width - pad.r, y1: y(reference), y2: y(reference), stroke: '#8d98a2', 'stroke-dasharray': '6 6'}));
  for (const s of validSeries) {
    const segments = splitSegments(s.points, s.cadence);
    const budget = Math.max(8, Math.floor(1200 / Math.max(1, segments.length)));
    for (const segment of segments) {
      const points = sampleExtrema(segment, budget);
      if (points.length === 1) svg.append(el('circle', {cx: x(points[0].t), cy: y(points[0].y), r: 3, fill: s.color, 'data-series-id': s.id}));
      else svg.append(el('path', {d: points.map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(2)},${y(p.y).toFixed(2)}`).join(' '), fill: 'none', stroke: s.color, 'stroke-width': 2.2, 'stroke-linejoin': 'round', 'vector-effect': 'non-scaling-stroke', 'data-series-id': s.id}));
    }
  }
  const timestamps = [...new Set(validSeries.flatMap(s => s.valid.map(p => p.t)))].sort((a, b) => a - b);
  const lookups = validSeries.map(s => new Map(s.valid.map(p => [p.t, p])));
  const cross = el('line', {y1: pad.t, y2: height - pad.b, stroke: '#8a969f', 'stroke-dasharray': '4 4', visibility: 'hidden'});
  const dots = validSeries.map(s => el('circle', {r: 4, fill: s.color, stroke: '#ffffff', 'stroke-width': 2, visibility: 'hidden'}));
  const boxW = Math.min(320, width - 20), boxH = 39 + validSeries.length * 24;
  const tooltip = el('g', {visibility: 'hidden', 'pointer-events': 'none', 'data-comparison-tooltip': ''});
  tooltip.append(el('rect', {width: boxW, height: boxH, rx: 8, class: 'chart-tooltip'}));
  const heading = el('text', {x: 12, y: 22}); tooltip.append(heading);
  const rows = validSeries.map((s, i) => {
    tooltip.append(el('circle', {cx: 14, cy: 43 + i * 24, r: 3, fill: s.color}));
    const label = el('text', {x: 25, y: 47 + i * 24}, s.label.length > 14 ? s.label.slice(0, 13) + '…' : s.label);
    const value = el('text', {x: boxW - 12, y: 47 + i * 24, 'text-anchor': 'end'});
    tooltip.append(label, value); return value;
  });
  const overlay = el('rect', {x: pad.l, y: pad.t, width: plotW, height: plotH, fill: 'transparent', 'data-comparison-overlay': ''});
  svg.append(cross, ...dots, tooltip, overlay); container.append(svg);
  let focused = timestamps.length - 1;
  function show(index) {
    focused = Math.max(0, Math.min(index, timestamps.length - 1));
    const t = timestamps[focused], px = x(t);
    cross.setAttribute('x1', px); cross.setAttribute('x2', px); cross.setAttribute('visibility', 'visible');
    tooltip.setAttribute('transform', `translate(${Math.max(5, Math.min(px + 12, width - boxW - 5))},${pad.t + 8})`);
    tooltip.setAttribute('visibility', 'visible');
    heading.textContent = new Date(t).toISOString().slice(0, 16).replace('T', ' ') + ' UTC';
    validSeries.forEach((s, i) => {
      const p = lookups[i].get(t);
      rows[i].textContent = p ? display(p.y) + (mode === 'change' ? '%' : ` ${quote}`) : '—';
      dots[i].setAttribute('visibility', p ? 'visible' : 'hidden');
      if (p) { dots[i].setAttribute('cx', x(p.t)); dots[i].setAttribute('cy', y(p.y)); }
    });
  }
  function pointer(event) {
    const rect = svg.getBoundingClientRect(), px = (event.clientX - rect.left) / rect.width * width;
    const target = tmin + (px - pad.l) / plotW * (tmax - tmin);
    let lo = 0, hi = timestamps.length - 1;
    while (lo < hi) { const mid = (lo + hi) >> 1; if (timestamps[mid] < target) lo = mid + 1; else hi = mid; }
    if (lo > 0 && Math.abs(timestamps[lo - 1] - target) < Math.abs(timestamps[lo] - target)) lo--;
    show(lo);
  }
  overlay.addEventListener('pointermove', pointer);
  overlay.addEventListener('pointerdown', pointer);
  overlay.addEventListener('pointerleave', () => { cross.setAttribute('visibility', 'hidden'); tooltip.setAttribute('visibility', 'hidden'); dots.forEach(dot => dot.setAttribute('visibility', 'hidden')); });
  svg.addEventListener('focus', () => show(focused));
  svg.addEventListener('keydown', event => {
    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); show(focused + (event.key === 'ArrowLeft' ? -1 : 1)); }
  });
  if (typeof ResizeObserver !== 'undefined') {
    let lastWidth = container.clientWidth;
    const observer = new ResizeObserver(() => {
      if (!container.isConnected) { disposeChart(container); return; }
      if (container.clientWidth > 0 && Math.abs(container.clientWidth - lastWidth) > 1) {
        lastWidth = container.clientWidth; renderComparisonChart(container, series, {mode, quote, from, to});
      }
    });
    observers.set(container, observer); observer.observe(container);
  }
}
