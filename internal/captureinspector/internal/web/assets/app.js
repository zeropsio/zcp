const state = {
  captures: [], captureId: '', view: null, tab: 'trace', query: '', revealed: false,
  compareLeft: '', compareRight: '', comparison: null, poll: null,
  providerEvents: [], providerCaptureId: '', providerLoading: false,
  rawItems: [], rawFile: '', rawAfter: 0, rawHasMore: false, rawCaptureId: '', rawLoading: false,
  trace: null, traceLoading: false, traceSession: '', traceInvocation: '', traceDensity: 'story', traceFocus: 'all', traceContent: {}, traceContentLoading: {},
  tracePresentation: 'flow', flowSelected: '', flowSelectedEdge: '', flowFocusPath: false, flowReplayOrder: 0, flowPlaying: false, flowTimer: null,
  flowDetail: null, flowDetailTab: 'overview', flowDetailItem: '', flowDetailSearch: '', flowDetailRawMode: 'pretty', flowInspectorScrollTop: 0,
};

const tabs = [
  ['trace', 'Session story'], ['index', 'Capture index'], ['overview', 'Overview'], ['hierarchy', 'Hierarchy'], ['timeline', 'Timeline'], ['provider', 'Provider SSE'], ['client', 'Client / eval'],
  ['context', 'Model context'], ['tools', 'Tools'], ['mcp', 'MCP'], ['sources', 'Sources'],
  ['metrics', 'Metrics'], ['artifacts', 'Artifacts'], ['raw', 'Raw'], ['compare', 'Compare'],
];

const app = document.querySelector('#app');

function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
}
function fmt(value) { return Number(value || 0).toLocaleString(); }
function bytes(value) {
  const n = Number(value || 0);
  if (n >= 1024 ** 3) return `${(n / 1024 ** 3).toFixed(2)} GB`;
  if (n >= 1024 ** 2) return `${(n / 1024 ** 2).toFixed(1)} MB`;
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${fmt(n)} B`;
}
function exactBytes(value) { const n=Number(value||0); return n>=1024?`${bytes(n)} · ${fmt(n)} B`:bytes(n); }
function duration(value) {
  let ms = Number(value || 0);
  if (ms < 1000) return `${fmt(ms)} ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)} s`;
  const minutes = Math.floor(ms / 60000); ms -= minutes * 60000;
  return `${minutes}m ${(ms / 1000).toFixed(0)}s`;
}
function date(value) { return value ? new Date(value).toLocaleString() : 'unknown'; }
function short(value, n = 16) { const s = String(value || ''); return s.length > n ? `${s.slice(0, n)}…` : s; }
function badge(text, kind = '') { return `<span class="badge ${esc(kind)}">${esc(text || 'unknown')}</span>`; }
function statusBadge(value) { return badge(value || 'unknown', `status-${value || 'unknown'}`); }
function metric(label, value, detail = '') {
  return `<article class="metric"><strong>${esc(value)}</strong><span>${esc(label)}</span>${detail ? `<small>${esc(detail)}</small>` : ''}</article>`;
}
function api(path, options) {
  return fetch(path, options).then(async response => {
    if (!response.ok) {
      let message = `${response.status} ${response.statusText}`;
      try { message = (await response.json()).error || message; } catch (_) {}
      throw new Error(message);
    }
    if (response.status === 204) return null;
    return response.json();
  });
}

async function boot() {
  try {
    state.captures = await api('/api/v1/captures');
    const requested = new URL(location.href).searchParams.get('capture');
    state.captureId = requested && state.captures.some(c => c.id === requested) ? requested : (state.captures[0]?.id || '');
    state.compareLeft = state.captureId;
    state.compareRight = state.captures[1]?.id || state.captureId;
    if (state.captureId) { await loadView(); if(state.tab==='trace') await loadTrace(); }
    render();
  } catch (error) { renderError(error); }
}

async function loadView() {
  clearTimeout(state.poll);
  if (state.providerCaptureId !== state.captureId) { state.providerCaptureId=state.captureId; state.providerEvents=[]; }
  if (state.rawCaptureId !== state.captureId) { state.rawCaptureId=state.captureId; state.rawItems=[]; state.rawFile=''; state.rawAfter=0; state.rawHasMore=false; }
  if (state.trace?.captureId !== state.captureId) { stopFlowPlayback();state.trace=null;state.traceSession='';state.traceInvocation='';state.traceContent={};state.traceContentLoading={};state.flowSelected='';state.flowSelectedEdge='';state.flowReplayOrder=0;resetFlowDetail(); }
  state.view = await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/view`);
  history.replaceState(null, '', `?capture=${encodeURIComponent(state.captureId)}`);
  if (state.view.capture.status === 'running') {
    state.poll = setTimeout(async () => { try { await loadView(); if(state.tab==='trace')await loadTrace(); render(); } catch (_) {} }, 2000);
  }
}

function render() {
  const previousInspector=document.querySelector('.flow-inspector');
  if(previousInspector)state.flowInspectorScrollTop=previousInspector.scrollTop;
  if (!state.captures.length) {
    app.innerHTML = `<main class="empty"><h1>ZCP Capture Inspector</h1><p>No capture manifests found.</p></main>`;
    return;
  }
  const view = state.view;
  app.innerHTML = `
    <header class="topbar">
      <div><h1>ZCP Capture Inspector</h1><p>Local read-only evidence view</p></div>
      <label>Capture<select id="capture-select">${state.captures.map(c => `<option value="${esc(c.id)}" ${c.id === state.captureId ? 'selected' : ''}>${esc(c.label || c.id)} · ${esc(c.status)}</option>`).join('')}</select></label>
      <div class="top-status">${statusBadge(view?.integrity?.state)} ${statusBadge(view?.capture?.status)} ${badge('PLAINTEXT', 'danger')}</div>
      <button class="button ${state.revealed ? 'active' : ''}" data-action="reveal">${state.revealed ? 'Plaintext enabled' : 'Reveal plaintext'}</button>
    </header>
    ${view && !view.integrity.complete ? `<div class="integrity-banner"><b>${esc(view.integrity.state.toUpperCase())}</b> — this evidence is not complete. Charts describe only the available validated/persisted prefix.</div>` : ''}
    <nav class="tabs">${tabs.map(([id, title]) => `<button data-tab="${id}" class="${state.tab === id ? 'active' : ''}">${esc(title)}</button>`).join('')}</nav>
    <section class="toolbar"><input id="query" type="search" value="${esc(state.query)}" placeholder="Filter current view"><span>${view ? `${view.rawRecordTotalKnown?fmt(view.rawRecordTotal):'unknown'} raw records · ${bytes(view.overview.bundleBytes)}` : ''}</span></section>
    <main class="content">${view ? renderTab(view) : '<p>Loading…</p>'}</main>
    <button id="drawer-backdrop" class="drawer-backdrop" data-action="close-drawer" aria-label="Close detail panel"></button>
    <aside id="drawer" class="drawer" role="dialog" aria-modal="true" aria-label="Evidence detail"><header><b id="drawer-title">Evidence</b><button data-action="close-drawer" aria-label="Close evidence drawer">×</button></header><div id="drawer-body"></div></aside>
  `;
  bind();
  const nextInspector=document.querySelector('.flow-inspector');
  if(nextInspector)nextInspector.scrollTop=state.flowInspectorScrollTop;
}

function renderTab(view) {
  switch (state.tab) {
    case 'trace': return renderSessionTrace(view);
    case 'index': return renderCaptureIndex();
    case 'hierarchy': return renderHierarchy(view);
    case 'timeline': return renderTimeline(view);
    case 'provider': return renderProvider(view);
    case 'client': return renderClient(view);
    case 'context': return renderContext(view);
    case 'tools': return renderTools(view);
    case 'mcp': return renderMCP(view);
    case 'sources': return renderSources(view);
    case 'metrics': return renderMetrics(view);
    case 'artifacts': return renderArtifacts(view);
    case 'raw': return renderRaw(view);
    case 'compare': return renderCompare();
    default: return renderOverview(view);
  }
}

async function loadTrace() {
  if(!state.captureId||state.traceLoading)return;
  state.traceLoading=true; render();
  const query=new URLSearchParams();
  if(state.traceSession)query.set('session',state.traceSession);
  if(state.traceInvocation)query.set('invocation',state.traceInvocation);
  try {
    state.trace=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/session-trace?${query}`);
    state.traceSession=state.trace.sessionId||'';
    state.traceInvocation=state.trace.invocationId||'';
    state.traceContent={};state.traceContentLoading={};state.flowSelected='';state.flowSelectedEdge='';state.flowReplayOrder=0;resetFlowDetail();stopFlowPlayback();
  } catch(error) { state.trace=null; alert(`Session trace: ${error.message}`); }
  finally { state.traceLoading=false; render(); }
}

function traceInvocations(view) {
  return (view.evalRuns||[]).flatMap(run=>run.scenarios.flatMap(scenario=>scenario.invocations));
}

function visibleTraceSteps() {
  if(!state.trace)return [];
  let steps=state.trace.steps||[];
  if(state.traceDensity==='story')steps=steps.filter(step=>!step.hiddenByDefault);
  if(state.traceFocus==='important')steps=steps.filter(step=>step.kind==='phase'||step.kind==='prompt'||step.status==='error'||step.propagation==='different'||step.propagation==='missing'||step.propagation==='ambiguous'||step.title==='Final model response');
  if(state.traceFocus==='tools')steps=steps.filter(step=>step.kind==='phase'||step.kind==='tool');
  return steps;
}

function renderSessionTrace(view) {
  const sessions=view.sessions||[]; const invocations=traceInvocations(view);
  if(state.traceLoading&&!state.trace)return '<section class="panel trace-empty"><h2>Building session story…</h2></section>';
  if(!state.trace)return `<section class="panel trace-empty"><h2>Session story</h2><p>No trace loaded.</p><button class="button" data-action="trace-reload">Load trace</button></section>`;
  const steps=visibleTraceSteps(); const maxSize=Math.max(1,...steps.filter(s=>s.sizeObserved).map(s=>s.sizeBytes));
  const presentation=state.tracePresentation;
  return `<section class="trace-page">${renderTraceHeader(view,sessions,invocations)}<section class="trace-summary metric-grid">${metric('Visible steps',`${fmt(steps.length)}/${fmt(state.trace.summary.stepCount)}`)}${metric('Model turns',fmt(state.trace.flow?.summary?.turnCount||0),`${state.trace.flow?.summary?.branchCount||0} branched`)}${metric('Tool operations',fmt(state.trace.summary.toolCount),`${state.trace.summary.errorCount} errors`)}${metric('Result differences',fmt(state.trace.summary.differenceCount))}${metric('Visible content',state.trace.summary.contentBytesKnown?bytes(state.trace.summary.contentBytes):'unknown')}${metric('Scope',state.traceInvocation?'one phase':'whole session')}</section><div class="trace-viewbar"><div class="presentation-toggle" role="group" aria-label="Trace presentation"><button class="button ${presentation==='cards'?'active':''}" data-presentation="cards">Cards</button><button class="button ${presentation==='flow'?'active':''}" data-presentation="flow">Flow map</button><button class="button ${presentation==='split'?'active':''}" data-presentation="split">Split</button></div><div class="trace-actions">${state.revealed?`<button class="button primary" data-action="trace-load-visible">Load visible content</button>`:'<span class="muted">Reveal plaintext to read payloads.</span>'}<span class="muted">Ordering follows evidence sequence; time only annotates observed events.</span></div></div>${presentation==='cards'?renderTraceCards(steps,maxSize):renderFlowMap(presentation==='split')}</section>`;
}

function renderTraceHeader(view,sessions,invocations) {
  return `<header class="trace-header panel"><div><p class="eyebrow">CAUSAL SESSION INSPECTOR</p><h2>${esc(view.capture.label||view.capture.id)}</h2><p class="muted">Follow user input, context growth, model turns, tool branches and exact result propagation.</p></div><div class="trace-controls"><label>Session<select id="trace-session"><option value="">Auto-select session</option>${sessions.map(s=>`<option value="${esc(s.id)}" ${s.id===state.traceSession?'selected':''}>${esc(short(s.id,28))} · ${fmt(s.providerExchanges)} exchanges</option>`).join('')}</select></label><label>Phase<select id="trace-invocation"><option value="">Whole session</option>${invocations.filter(i=>!state.traceSession||i.clientSessionId===state.traceSession).map(i=>`<option value="${esc(i.id)}" ${i.id===state.traceInvocation?'selected':''}>${esc(i.phase)} · ${esc(short(i.id,28))}</option>`).join('')}</select></label><label>Focus<select id="trace-focus"><option value="all" ${state.traceFocus==='all'?'selected':''}>Everything</option><option value="important" ${state.traceFocus==='important'?'selected':''}>Important only</option><option value="tools" ${state.traceFocus==='tools'?'selected':''}>Tools only</option></select></label><div class="density-toggle"><button class="button ${state.traceDensity==='story'?'active':''}" data-density="story">Story</button><button class="button ${state.traceDensity==='detailed'?'active':''}" data-density="detailed">Detailed</button></div></div></header>`;
}

function renderTraceCards(steps,maxSize) {
  return `<div class="trace-layout"><main class="trace-feed">${steps.map(step=>renderTraceStep(step,maxSize)).join('')}</main><aside class="trace-minimap" aria-label="Session minimap">${steps.filter(s=>s.kind!=='phase').map(step=>`<button class="mini-${esc(step.kind)} ${step.status==='error'?'error':''}" data-trace-jump="${step.order}" title="${esc(step.title)} · ${step.sizeObserved?bytes(step.sizeBytes):'unknown'}"><svg viewBox="0 0 100 6" preserveAspectRatio="none" aria-hidden="true"><rect x="0" y="0" width="${step.sizeObserved?Math.max(3,step.sizeBytes/maxSize*100):3}" height="6"></rect></svg></button>`).join('')}</aside></div>`;
}

const flowGeometry={width:1160,header:62,lane:{user:{x:34,w:205},context:{x:300,w:220},model:{x:582,w:230},tool:{x:882,w:240}},nodeHeight:72,gap:16};

function flowVisibleData() {
  const graph=state.trace?.flow||{turns:[],nodes:[],edges:[],phases:[]};
  const turnIds=new Set((graph.turns||[]).filter(turn=>state.traceDensity!=='story'||!turn.hiddenByDefault).map(turn=>turn.id));
  const nodes=(graph.nodes||[]).filter(node=>turnIds.has(node.turnId)&&(state.traceDensity!=='story'||!node.hiddenByDefault)).sort((a,b)=>a.order-b.order);
  const nodeIds=new Set(nodes.map(node=>node.id));
  const edges=(graph.edges||[]).filter(edge=>nodeIds.has(edge.fromId)&&nodeIds.has(edge.toId)&&(state.traceDensity!=='story'||!edge.hiddenByDefault));
  return {graph,nodes,edges,nodeIds,turns:(graph.turns||[]).filter(turn=>turnIds.has(turn.id))};
}

function buildFlowLayout(data) {
  const positions={};const turnBounds={};const phaseLabels=[];let y=flowGeometry.header+20;let previousPhase='';
  const nodesByTurn=new Map();data.nodes.forEach(node=>{if(!nodesByTurn.has(node.turnId))nodesByTurn.set(node.turnId,[]);nodesByTurn.get(node.turnId).push(node);});
  for(const turn of data.turns){
    const phaseKey=`${turn.invocationId||''}\u0000${turn.phase||''}`;
    if(phaseKey!==previousPhase){phaseLabels.push({title:turn.phase||'Session',invocationId:turn.invocationId||'',y});y+=34;previousPhase=phaseKey;}
    const turnNodes=nodesByTurn.get(turn.id)||[];const lanes={user:[],context:[],model:[],tool:[]};turnNodes.forEach(node=>(lanes[node.lane]||lanes.context).push(node));
    const rows=Math.max(1,...Object.values(lanes).map(values=>values.length));const height=Math.max(150,34+rows*(flowGeometry.nodeHeight+flowGeometry.gap));
    turnBounds[turn.id]={x:12,y,width:flowGeometry.width-24,height,turn};
    for(const [lane,values] of Object.entries(lanes)){const laneGeo=flowGeometry.lane[lane];const contentHeight=values.length*flowGeometry.nodeHeight+Math.max(0,values.length-1)*flowGeometry.gap;let nodeY=y+(height-contentHeight)/2;for(const node of values){positions[node.id]={x:laneGeo.x,y:nodeY,w:laneGeo.w,h:flowGeometry.nodeHeight,node};nodeY+=flowGeometry.nodeHeight+flowGeometry.gap;}}
    y+=height+18;
  }
  return {positions,turnBounds,phaseLabels,width:flowGeometry.width,height:Math.max(240,y+20),nodes:data.nodes,edges:data.edges,turns:data.turns};
}

function renderFlowMap(split=false) {
  const data=flowVisibleData();
  if(!data.nodes.length)return '<section class="panel trace-empty"><h2>No flow nodes in this scope</h2><p>Switch to Detailed density or select another session.</p></section>';
  const layout=buildFlowLayout(data);const selected=state.flowDetail||state.flowSelected||state.flowSelectedEdge;const sequenceCount=data.nodes.length;
  return `<section class="flow-section"><div class="flow-toolbar"><div><p class="eyebrow">CAUSAL FLOW MAP</p><strong>${fmt(layout.turns.length)} turns · ${fmt(data.nodes.length)} nodes · ${fmt(data.edges.length)} evidence links</strong></div><div class="flow-replay"><button class="icon-button" data-action="flow-prev" aria-label="Previous evidence step">‹</button><button class="button ${state.flowPlaying?'active':''}" data-action="flow-play">${state.flowPlaying?'Pause':'Replay'}</button><button class="icon-button" data-action="flow-next" aria-label="Next evidence step">›</button><input id="flow-replay-range" type="range" min="0" max="${sequenceCount}" value="${Math.min(state.flowReplayOrder,sequenceCount)}" aria-label="Evidence sequence position"><span>${state.flowReplayOrder?`${Math.min(state.flowReplayOrder,sequenceCount)}/${sequenceCount}`:'all'}</span><button class="button ${state.flowFocusPath?'active':''}" data-action="flow-focus" ${state.flowSelected?'':'disabled'}>Focus path</button><button class="button" data-action="flow-reset">Reset</button></div></div><div class="flow-legend"><span><i class="legend-user"></i>User input</span><span><i class="legend-context"></i>Context river</span><span><i class="legend-model"></i>Claude turn</span><span><i class="legend-tool"></i>Tool operation</span><span><i class="legend-different"></i>Changed result</span><span><i class="legend-unknown"></i>Unknown</span></div><div class="flow-shell ${selected||split?'with-inspector':''} ${state.flowDetail?'detail-expanded':''}"><div class="flow-viewport" tabindex="0" aria-label="Causal session flow"><div class="flow-world">${renderFlowSVG(layout,data)}</div></div>${selected||split?renderFlowInspector(data,split):''}</div></section>`;
}

function renderFlowSVG(layout,data) {
  const maxTool=Math.max(1,...data.edges.filter(edge=>edge.kind==='tool-result').map(edge=>edge.bytesObserved?Math.abs(edge.bytes):0));const pathSet=flowFocusSet(data);const activeNode=state.flowReplayOrder?data.nodes[Math.min(state.flowReplayOrder,data.nodes.length)-1]?.id:'';const reached=new Set(state.flowReplayOrder?data.nodes.slice(0,state.flowReplayOrder).map(node=>node.id):data.nodes.map(node=>node.id));
  const laneGuides=Object.entries(flowGeometry.lane).map(([lane,geo])=>`<g class="flow-lane-guide lane-${lane}"><line x1="${geo.x+geo.w/2}" y1="${flowGeometry.header}" x2="${geo.x+geo.w/2}" y2="${layout.height-10}"></line><text x="${geo.x+geo.w/2}" y="26">${esc(({user:'USER INPUT',context:'MODEL CONTEXT',model:'CLAUDE',tool:'TOOLS'})[lane])}</text></g>`).join('');
  const phases=layout.phaseLabels.map((phase,index)=>{const next=layout.phaseLabels[index+1];const end=next?next.y-8:layout.height-12;return `<g class="flow-phase-band"><rect x="8" y="${phase.y}" width="${layout.width-16}" height="${Math.max(30,end-phase.y)}" rx="14"></rect><text x="22" y="${phase.y+20}">${esc(phase.title.toUpperCase())}</text></g>`;}).join('');
  const turns=Object.values(layout.turnBounds).map(bound=>`<g class="flow-turn-band"><line x1="18" y1="${bound.y+bound.height}" x2="${layout.width-18}" y2="${bound.y+bound.height}"></line><text x="${layout.width-24}" y="${bound.y+17}" text-anchor="end">TURN ${bound.turn.order} · ${esc(short(bound.turn.exchangeId,20))}</text></g>`).join('');
  const edges=data.edges.map(edge=>renderFlowEdge(edge,layout.positions,maxTool,pathSet,activeNode,reached)).join('');
  const nodes=data.nodes.map((node,index)=>renderFlowNode(node,layout.positions[node.id],index,pathSet)).join('');
  return `<svg class="flow-wires" width="${layout.width}" height="${layout.height}" viewBox="0 0 ${layout.width} ${layout.height}" role="group" aria-label="Causal session flow"><defs><marker id="arrow-exact" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z"></path></marker><marker id="arrow-warning" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z"></path></marker><marker id="arrow-error" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z"></path></marker></defs>${phases}${laneGuides}${turns}${edges}${nodes}</svg>`;
}

function renderFlowEdge(edge,positions,maxTool,pathSet,activeNode,reached) {
  const from=positions[edge.fromId],to=positions[edge.toId];if(!from||!to)return '';
  const path=flowEdgePath(edge,from,to);const warning=edge.status==='different'||edge.status==='rewritten'||edge.status==='reset';const error=edge.status==='missing'||edge.status==='ambiguous';const marker=error?'error':warning?'warning':'exact';
  const width=edge.kind==='context-carry'?flowContextWidth(edge):edge.kind==='tool-result'?2+Math.sqrt(Math.max(0,edge.bytes||0)/maxTool)*4:2;
  const focused=!state.flowFocusPath||!state.flowSelected||pathSet.has(edge.fromId)&&pathSet.has(edge.toId);const replayed=!state.flowReplayOrder||reached.has(edge.fromId)||reached.has(edge.toId);const active=edge.fromId===activeNode||edge.toId===activeNode;
  const label=flowEdgeLabel(edge);const labelPos=flowEdgeLabelPosition(edge,from,to);
  return `<g class="flow-edge-group kind-${esc(edge.kind)} status-${esc(edge.status)} ${focused?'':'flow-dim'} ${replayed?'':'flow-future'} ${active?'flow-active':''}"><path class="flow-edge" d="${path}" stroke-width="${width}" marker-end="url(#arrow-${marker})"></path><path class="flow-edge-hit" d="${path}" data-flow-edge="${esc(edge.id)}" tabindex="0" role="button" aria-label="${esc(`${edge.kind}, ${edge.status}, ${edge.basis}`)}"></path>${label?`<text class="flow-edge-label" x="${labelPos.x}" y="${labelPos.y}" text-anchor="middle">${esc(label)}</text>`:''}</g>`;
}

function flowEdgePath(edge,from,to) {
  if(edge.kind==='context-carry'){const x1=from.x+from.w/2,y1=from.y+from.h,x2=to.x+to.w/2,y2=to.y;const mid=(y1+y2)/2;return `M ${x1} ${y1} C ${x1} ${mid}, ${x2} ${mid}, ${x2} ${y2}`;}
  if(edge.kind==='observed-next-request'){const x1=from.x+from.w/2,y1=from.y+from.h,x2=to.x+to.w/2,y2=to.y;return `M ${x1} ${y1} C ${x1} ${y1+45}, ${x2+80} ${y2-45}, ${x2} ${y2}`;}
  const forward=to.x>=from.x;const x1=forward?from.x+from.w:from.x;const x2=forward?to.x:to.x+to.w;const y1=from.y+from.h/2,y2=to.y+to.h/2;const bend=Math.max(55,Math.abs(x2-x1)*.45);return `M ${x1} ${y1} C ${x1+(forward?bend:-bend)} ${y1}, ${x2+(forward?-bend:bend)} ${y2}, ${x2} ${y2}`;
}

function flowEdgeLabelPosition(edge,from,to) { if(edge.kind==='context-carry')return{x:from.x+from.w/2+32,y:(from.y+from.h+to.y)/2};return{x:(from.x+from.w/2+to.x+to.w/2)/2,y:(from.y+from.h/2+to.y+to.h/2)/2-8}; }
function flowEdgeLabel(edge) { if(edge.kind==='tool-result'){if(edge.status==='different')return `DIFFERENT ${edge.deltaBytesObserved?formatSignedBytes(edge.deltaBytes):''}`.trim();if(edge.status==='missing'||edge.status==='ambiguous')return edge.status.toUpperCase();return `${String(edge.status||'result').toUpperCase()} · ${edge.bytesObserved?bytes(edge.bytes):'unknown'}`;}if(edge.kind==='provider-request')return edge.bytesObserved?bytes(edge.bytes):'unknown';if(edge.kind==='context-carry'&&(edge.status==='reset'||edge.status==='rewritten'))return edge.status.toUpperCase();return ''; }
function flowContextWidth(edge){const max=Math.max(1,state.trace?.flow?.summary?.maxContextBytes||1);return 4+Math.sqrt(Math.max(0,edge.targetBytes||edge.bytes||0)/max)*14;}
function formatSignedBytes(value){const n=Number(value||0);return `${n>=0?'+':'−'}${bytes(Math.abs(n))}`;}

function renderFlowNode(node,position,index,pathSet) {
  if(!position)return '';
  const focused=!state.flowFocusPath||!state.flowSelected||pathSet.has(node.id);const replayed=!state.flowReplayOrder||index<state.flowReplayOrder;const active=state.flowReplayOrder===index+1;const selected=state.flowSelected===node.id;const queryMatches=!state.query||`${node.title} ${node.subtitle||''} ${node.status||''} ${node.propagation||''}`.toLowerCase().includes(state.query.toLowerCase());const focusMatches=flowFocusMatches(node);
  return `<foreignObject x="${position.x}" y="${position.y}" width="${position.w}" height="${position.h}" class="flow-node-object"><button xmlns="http://www.w3.org/1999/xhtml" class="flow-node flow-node-svg lane-${esc(node.lane)} kind-${esc(node.kind)} status-${esc(node.status)} ${selected?'selected':''} ${focused&&queryMatches&&focusMatches?'':'flow-dim'} ${replayed?'':'flow-future'} ${active?'flow-active':''}" data-flow-node="${esc(node.id)}" aria-label="${esc(`${node.title}, ${node.primaryBytesObserved?exactBytes(node.primaryBytes):'unknown'}`)}"><span class="flow-node-top"><small>${esc(flowNodeKicker(node))}</small>${flowNodeBadge(node)}</span><strong>${esc(node.title)}</strong><span class="flow-node-bottom"><small>${esc(flowNodeDetail(node))}</small><b>${node.primaryBytesObserved?bytes(node.primaryBytes):'unknown'}</b></span>${renderFlowNodeVisual(node)}</button></foreignObject>`;
}

function flowNodeKicker(node){return ({user:'USER',context:'CONTEXT',model:'MODEL TURN',tool:'TOOL'})[node.lane]||String(node.kind||'NODE').toUpperCase();}
function flowNodeBadge(node){if(node.status==='error')return badge('ERROR','danger');if(node.propagation==='client-result')return badge('CLIENT','');if(node.propagation&&node.propagation!=='exact')return badge(node.propagation,'warning');return '';}
function flowNodeDetail(node){if(node.lane==='context')return node.deltaBytesObserved?`new ${formatSignedBytes(node.deltaBytes).replace('+','')}`:'request';if(node.lane==='model')return [node.textBlockCount?`${node.textBlockCount} text`:'',node.thinkingBlockCount?`${node.thinkingBlockCount} thinking`:'',node.toolCount?`${node.toolCount} tools`:''].filter(Boolean).join(' · ')||node.stopReason||'response';if(node.lane==='tool')return node.timingObserved?duration(node.durationMs):(node.subtitle||'result');return node.subtitle||'provider-visible input';}
function renderFlowNodeVisual(node){if(node.lane==='context'){const parts=(node.dimensions||[]).filter(part=>['system','tool schemas','messages','metadata / other'].includes(part.label));const total=Math.max(1,parts.reduce((sum,part)=>sum+(part.observed?part.bytes:0),0));let x=0;return `<svg class="flow-context-stack" viewBox="0 0 100 4" preserveAspectRatio="none" aria-hidden="true">${parts.map(part=>{const width=part.observed?part.bytes/total*100:0;const result=`<rect class="part-${esc(part.label.replaceAll(' ','-').replaceAll('/','-'))}" x="${x}" y="0" width="${width}" height="4"><title>${esc(part.label)} ${part.observed?bytes(part.bytes):'unknown'}</title></rect>`;x+=width;return result;}).join('')}</svg>`;}if(node.deltaBytesObserved&&Number(node.deltaBytes||0)!==0)return `<span class="flow-delta ${node.deltaBytes>0?'positive':'negative'}">${formatSignedBytes(node.deltaBytes)}</span>`;return '<span class="flow-node-pulse"></span>';}
function flowFocusMatches(node){if(state.traceFocus==='important')return node.kind==='prompt'||node.title==='Final answer'||node.status==='error'||node.propagation==='different'||node.propagation==='missing'||node.propagation==='ambiguous'||node.contextReset||node.historyRewritten;if(state.traceFocus==='tools')return node.lane==='context'||node.lane==='model'||node.lane==='tool';return true;}
function flowFocusSet(data) {
  const included=new Set();if(!state.flowSelected)return included;included.add(state.flowSelected);let frontier=[state.flowSelected];
  for(let depth=0;depth<3&&frontier.length;depth++){const next=[];for(const edge of data.edges){if(edge.kind==='observed-next-request')continue;if(frontier.includes(edge.fromId)&&!included.has(edge.toId)){included.add(edge.toId);next.push(edge.toId);}if(frontier.includes(edge.toId)&&!included.has(edge.fromId)){included.add(edge.fromId);next.push(edge.fromId);}}frontier=next;}
  return included;
}

function renderFlowInspector(data,split) {
  if(state.flowDetail)return renderExpandedFlowDetail(data,split);
  const node=(data.graph.nodes||[]).find(item=>item.id===state.flowSelected);const edge=(data.graph.edges||[]).find(item=>item.id===state.flowSelectedEdge);
  if(!node&&!edge)return `<aside class="flow-inspector ${split?'split':''}" tabindex="-1" role="complementary" aria-label="Causal flow detail"><div class="flow-inspector-empty"><span>◎</span><h3>Select a node or edge</h3><p>Inspect exact byte dimensions, correlation basis and linked Story evidence.</p></div></aside>`;
  if(edge)return renderFlowEdgeInspector(edge,split);
  const steps=(state.trace.steps||[]).filter(step=>(node.stepIds||[]).includes(step.id));const max=Math.max(1,...steps.map(step=>step.sizeObserved?step.sizeBytes:0));
  return `<aside class="flow-inspector ${split?'split':''}" tabindex="-1" role="complementary" aria-label="Causal flow detail"><header><div><p class="eyebrow">SELECTED ${esc(node.lane.toUpperCase())}</p><h3>${esc(node.title)}</h3></div><button class="icon-button" data-action="flow-clear" aria-label="Close flow detail">×</button></header><div class="flow-inspector-meta">${node.status?statusBadge(node.status):''}${node.propagation?badge(node.propagation,node.propagation==='exact'?'ok':'warning'):''}<strong>${node.primaryBytesObserved?exactBytes(node.primaryBytes):'unknown'}</strong></div>${renderFlowDimensions(node.dimensions||[])}${renderFlowInspectorAction(node)}${steps.map(step=>renderFlowStepDetail(step,max)).join('')||'<p class="muted">This structural node has no plaintext payload. Its evidence is available below.</p>'}<div class="flow-evidence"><button class="evidence-link" data-flow-evidence='${esc(JSON.stringify(node.evidence||[]))}' data-flow-evidence-title="${esc(node.title)}">Open evidence</button><small>${esc(node.exchangeId||node.toolExecutionId||'')}</small></div></aside>`;
}

function renderFlowInspectorAction(node){if(node.toolExecutionId)return `<button class="button" data-flow-tool-detail="${esc(node.toolExecutionId)}">Open formatted tool detail</button>`;if(node.exchangeId)return `<button class="button" data-flow-context-detail="${esc(node.exchangeId)}">Open formatted model context</button>`;return '';}
function renderFlowEdgeInspector(edge,split){return `<aside class="flow-inspector ${split?'split':''}" tabindex="-1" role="complementary" aria-label="Causal flow detail"><header><div><p class="eyebrow">EVIDENCE LINK</p><h3>${esc(edge.kind.replaceAll('-',' '))}</h3></div><button class="icon-button" data-action="flow-clear" aria-label="Close flow detail">×</button></header><div class="flow-inspector-meta">${badge(edge.status,edge.status==='exact'?'ok':edge.status==='different'||edge.status==='rewritten'?'warning':'')}<strong>${edge.bytesObserved?exactBytes(edge.bytes):'unknown'}</strong></div><div class="detail-grid"><span>Correlation basis</span><b>${esc(edge.basis)}</b><span>From</span><b>${esc(edge.fromId)}</b><span>To</span><b>${esc(edge.toId)}</b><span>Source payload</span><b>${edge.sourceBytesObserved?exactBytes(edge.sourceBytes):'unknown'}</b><span>Target payload</span><b>${edge.targetBytesObserved?exactBytes(edge.targetBytes):'unknown'}</b><span>Delta</span><b>${edge.deltaBytesObserved?formatSignedBytes(edge.deltaBytes):'unknown'}</b></div><div class="flow-evidence"><button class="evidence-link" data-flow-evidence='${esc(JSON.stringify(edge.evidence||[]))}' data-flow-evidence-title="${esc(edge.kind)}">Open evidence</button></div></aside>`;}
function renderFlowDimensions(dimensions){if(!dimensions.length)return '';const max=Math.max(1,...dimensions.filter(d=>d.observed).map(d=>Math.abs(d.bytes)));return `<div class="flow-dimensions">${dimensions.map(d=>`<div><span>${esc(d.label)}</span><progress max="${max}" value="${d.observed?Math.abs(d.bytes):0}"></progress><strong title="${d.observed?`${fmt(d.bytes)} B`:'unknown'}">${d.observed?bytes(d.bytes):'unknown'}</strong></div>`).join('')}</div>`;}
function renderFlowStepDetail(step,max){const refs=(step.contentRefs||[]).map(ref=>renderTraceContentRef(ref)).join('');return `<article class="flow-step-detail"><header><span>${traceIcon(step)}</span><div><small>${esc(traceActor(step))}</small><h4>${esc(step.title)}</h4></div><strong>${step.sizeObserved?bytes(step.sizeBytes):'unknown'}</strong></header><progress class="flow-step-size" max="${max}" value="${step.sizeObserved?step.sizeBytes:0}"></progress>${refs}${renderTraceDifference(step)}<footer><span>${esc(step.correlationBasis)}</span></footer></article>`;}

function renderExpandedFlowDetail(flowData,split){
  const detail=state.flowDetail;
  if(!detail)return '';
  if(detail.loading)return renderFlowDetailFrame('LOADING',detail.title||'Detail','<div class="flow-detail-loading"><i></i><p>Reading reveal-gated evidence…</p></div>',split);
  if(detail.error)return renderFlowDetailFrame('DETAIL ERROR',detail.title||'Detail',`<p class="error-text">${esc(detail.error)}</p>`,split);
  if(detail.kind==='context')return renderContextFlowDetail(flowData,detail,split);
  if(detail.kind==='tool')return renderToolFlowDetail(flowData,detail,split);
  if(detail.kind==='evidence')return renderEvidenceFlowDetail(detail,split);
  return renderFlowDetailFrame('DETAIL',detail.title||'Detail','<p class="muted">Unsupported detail type.</p>',split);
}

function renderFlowDetailFrame(kicker,title,body,split){return `<aside class="flow-inspector flow-detail-workspace ${split?'split':''}" tabindex="-1" role="complementary" aria-label="Causal flow detail"><header><div><button class="flow-back" data-action="flow-detail-back">← Back</button><p class="eyebrow">${esc(kicker)}</p><h3>${esc(title)}</h3></div><button class="icon-button" data-action="flow-clear" aria-label="Close flow detail">×</button></header>${body}</aside>`;}
function flowDetailTabs(items){return `<nav class="flow-detail-tabs" aria-label="Detail sections">${items.map(([id,label,count])=>`<button class="${state.flowDetailTab===id?'active':''}" data-flow-detail-tab="${esc(id)}">${esc(label)}${count==null?'':` <span>${fmt(count)}</span>`}</button>`).join('')}</nav>`;}

function renderContextFlowDetail(flowData,detail,split){
  const context=detail.data;const system=contextArray(context.system);const tools=context.tools||[];const messages=context.messages||[];
  const tabs=flowDetailTabs([['overview','Overview'],['system','System',system.length],['tools','Tools',tools.length],['messages','Messages',messages.length],['raw','Raw request']]);
  let body='';
  if(state.flowDetailTab==='system')body=renderContextCollection('system',system,(item,index)=>`#${index+1} · ${item?.type||typeof item}`,(item,index)=>renderContextSystemBlock(item,index));
  else if(state.flowDetailTab==='tools')body=renderContextCollection('tools',tools,(item,index)=>item.name||`Tool ${index+1}`,(item,index)=>renderContextTool(item,index));
  else if(state.flowDetailTab==='messages')body=renderContextCollection('messages',messages,(item,index)=>`#${index+1} · ${item.role||'unknown'} · ${bytes(item.bytes)}`,(item,index)=>renderContextMessage(item,index));
  else if(state.flowDetailTab==='raw')body=renderContextRaw(context.rawRequest);
  else body=renderContextOverview(flowData,detail,system,tools,messages);
  return renderFlowDetailFrame('FULL MODEL CONTEXT',`Context ${short(context.exchangeId,24)}`,`${tabs}<div class="flow-detail-body">${body}</div>`,split);
}

function contextArray(value){if(value==null)return [];return Array.isArray(value)?value:[value];}
function renderContextOverview(flowData,detail,system,tools,messages){
  const node=(flowData.graph.nodes||[]).find(item=>item.exchangeId===detail.id&&item.lane==='context');
  return `<section class="context-overview"><div class="context-hero"><div><span>Model</span><strong>${esc(detail.data.model||node?.model||'unknown')}</strong></div><div><span>Exact request</span><strong>${exactBytes(detail.data.requestBytes)}</strong></div></div><div class="context-counts"><article><strong>${fmt(system.length)}</strong><span>system blocks</span></article><article><strong>${fmt(tools.length)}</strong><span>tool definitions</span></article><article><strong>${fmt(messages.length)}</strong><span>messages</span></article></div>${node?renderFlowDimensions(node.dimensions||[]):''}<div class="context-guide"><h4>How to read this request</h4><p>This is the exact model-visible request. Open a section above to inspect decoded blocks without JSON string escapes. Raw source stays available separately.</p></div></section>`;
}

function renderContextCollection(kind,items,labelFor,renderItem){
  if(!items.length)return `<div class="flow-detail-empty">No ${esc(kind)} items in this request.</div>`;
  const query=state.flowDetailSearch.trim().toLowerCase();const matches=items.map((item,index)=>({item,index,label:labelFor(item,index)})).filter(entry=>!query||`${entry.label} ${JSON.stringify(entry.item)}`.toLowerCase().includes(query));
  const selectedToken=String(state.flowDetailItem||'');let selected=matches.find(entry=>selectedToken===`${kind}:${entry.index}`);if(!selected&&matches.length===1)selected=matches[0];
  const grid=`<div class="detail-item-grid">${matches.map(entry=>`<button class="${selected?.index===entry.index?'active':''}" data-flow-detail-item="${kind}:${entry.index}"><span>${esc(entry.label)}</span></button>`).join('')||'<p class="muted">No matching items.</p>'}</div>`;
  return `<div class="context-collection"><label class="detail-search">Filter ${esc(kind)}<input id="flow-detail-search" type="search" value="${esc(state.flowDetailSearch)}" placeholder="Search ${esc(kind)}"></label>${selected?`<div class="detail-selected-item">${renderItem(selected.item,selected.index)}</div><details class="detail-picker"><summary>Choose another ${esc(kind)} item</summary>${grid}</details>`:`${grid}<div class="flow-detail-empty">Choose an item above to inspect its formatted content.</div>`}</div>`;
}

function renderContextSystemBlock(block,index){
  if(!block||typeof block!=='object')return `<section class="formatted-section"><h4>System block ${index+1}</h4>${renderContentValue(block)}</section>`;
  const cache=block.cache_control;const text=block.text;const content=block.content;
  return `<section class="formatted-section"><header><div><p class="eyebrow">SYSTEM BLOCK ${index+1}</p><h4>${esc(block.type||'unknown')}</h4></div>${cache?badge(`cache ${cache.type||'control'}`,'system'):''}</header>${typeof text==='string'?renderRichText(text):content!=null?renderContentValue(content):renderJSONTree(block,500,1)}<details class="formatted-raw"><summary>Block evidence representation</summary>${renderJSONTree(block,500,1)}</details></section>`;
}

function renderContextTool(tool,index){
  const value=tool?.json||{};const description=value.description;const schema=value.input_schema||value.inputSchema;
  return `<section class="formatted-section"><header><div><p class="eyebrow">TOOL DEFINITION ${index+1}</p><h4>${esc(tool?.name||value.name||'unnamed')}</h4></div><strong>${tool?.bytes!=null?exactBytes(tool.bytes):'unknown'}</strong></header>${typeof description==='string'?`<h5>Description</h5>${renderRichText(description)}`:''}${schema!=null?`<h5>Input schema</h5>${renderJSONTree(schema,500,2)}`:''}<details class="formatted-raw"><summary>Complete definition</summary>${renderJSONTree(value,600,1)}</details></section>`;
}

function renderContextMessage(message,index){
  const value=message?.json||{};const content=value.content;
  return `<section class="formatted-section"><header><div><p class="eyebrow">MESSAGE ${index+1}</p><h4>${esc(message?.role||value.role||'unknown')}</h4></div><strong>${message?.bytes!=null?exactBytes(message.bytes):'unknown'}</strong></header><div class="content-type-row">${(message?.contentTypes||[]).map(type=>badge(type)).join('')}</div>${renderMessageContent(content)}<details class="formatted-raw"><summary>Complete message representation</summary>${renderJSONTree(value,600,1)}</details></section>`;
}

function renderMessageContent(content){
  if(typeof content==='string')return renderRichText(content);
  if(!Array.isArray(content))return renderContentValue(content);
  return `<div class="message-blocks">${content.map((block,index)=>renderMessageBlock(block,index)).join('')}</div>`;
}
function renderMessageBlock(block,index){
  if(!block||typeof block!=='object')return `<article class="message-block"><header>Block ${index+1}</header>${renderContentValue(block)}</article>`;
  const type=block.type||'unknown';let body='';
  if(typeof block.text==='string')body=renderRichText(block.text);
  else if(type==='tool_use')body=`${block.name?`<h5>${esc(block.name)}</h5>`:''}${renderContentValue(block.input)}`;
  else if(type==='tool_result')body=renderContentValue(block.content);
  else if(block.content!=null)body=renderContentValue(block.content);
  else body=renderJSONTree(block,350,1);
  return `<article class="message-block type-${esc(type)}"><header><span>Block ${index+1}</span>${badge(type,type==='tool_result'?'warning':'')}</header>${body}</article>`;
}

function renderContextRaw(raw){return `<div class="raw-mode-toggle"><button class="button ${state.flowDetailRawMode==='pretty'?'active':''}" data-flow-raw-mode="pretty">Pretty</button><button class="button ${state.flowDetailRawMode==='raw'?'active':''}" data-flow-raw-mode="raw">Raw source</button></div>${state.flowDetailRawMode==='raw'?`<pre class="raw-request-source">${esc(JSON.stringify(raw,null,2))}</pre>`:renderJSONTree(raw,900,1)}`;}

function renderToolFlowDetail(flowData,detail,split){
  const tool=detail.data;const node=(flowData.graph.nodes||[]).find(item=>item.toolExecutionId===detail.id);const hasProvider=typeof tool.providerResultText==='string'&&tool.providerResultText!=='';
  const tabs=flowDetailTabs([['overview','Overview'],['arguments','Arguments'],['result','Tool result'],['provider','Model context'],['difference','Difference'],['sources','Sources']]);let body='';
  if(state.flowDetailTab==='arguments')body=renderContentValue(tool.argumentsJson);
  else if(state.flowDetailTab==='result')body=renderContentValue(tool.resultText);
  else if(state.flowDetailTab==='provider')body=hasProvider?renderContentValue(tool.providerResultText):'<div class="flow-detail-empty">No provider-context result was observed.</div>';
  else if(state.flowDetailTab==='difference')body=hasProvider?renderDifferenceValues(tool.resultText,tool.providerResultText):'<div class="flow-detail-empty">Two observed payloads are required for a difference view.</div>';
  else if(state.flowDetailTab==='sources')body=renderJSONTree({currentCorpus:tool.sourceMatches||[],composition:tool.compositionMatches||[]},500,2);
  else body=`<div class="tool-detail-hero"><div>${tool.isError?badge('ERROR','danger'):badge('COMPLETED','ok')}${badge(tool.propagation,tool.propagation==='exact'||tool.propagation==='client-result'?'ok':'warning')}</div><strong>${esc(tool.toolName)}</strong><span>${esc(tool.category)}</span></div>${node?renderFlowDimensions(node.dimensions||[]):''}<div class="context-guide"><h4>Propagation</h4><p>The exact execution result and the later provider-context result remain separate evidence dimensions.</p></div>`;
  return renderFlowDetailFrame('FULL TOOL EVIDENCE',tool.toolName||detail.title,`${tabs}<div class="flow-detail-body">${body}</div>`,split);
}

function renderEvidenceFlowDetail(detail,split){const refs=detail.data||[];const body=`<div class="flow-detail-body"><p class="muted">Canonical coordinates only. Opening a raw record closes this inspector so detail surfaces never overlap.</p><div class="evidence-cards">${refs.map(ref=>`<article><header><b>${esc(ref.file||'unknown file')}</b><span>#${fmt(ref.seqStart)}${ref.seqEnd&&ref.seqEnd!==ref.seqStart?`–${fmt(ref.seqEnd)}`:''}</span></header><dl><dt>Exchange</dt><dd>${esc(ref.exchangeId||'—')}</dd><dt>Stream offset</dt><dd>${ref.streamOffset==null?'unknown':fmt(ref.streamOffset)}</dd><dt>Decoded offset</dt><dd>${ref.decodedOffset==null?'unknown':fmt(ref.decodedOffset)}</dd><dt>Bytes</dt><dd>${ref.byteLength==null?'unknown':exactBytes(ref.byteLength)}</dd></dl>${ref.file&&ref.seqStart?`<button class="button" data-flow-open-raw="${esc(ref.file)}" data-flow-open-seq="${ref.seqStart}">Open canonical raw record</button>`:''}</article>`).join('')||'<p>No evidence coordinates.</p>'}</div></div>`;return renderFlowDetailFrame('EVIDENCE COORDINATES',detail.title||'Evidence',body,split);}

function renderContentValue(value){
  if(value==null)return '<span class="json-null">null</span>';
  if(typeof value!=='string')return renderJSONTree(value,500,2);
  const trimmed=value.trim();if(trimmed){try{const parsed=JSON.parse(trimmed);if(parsed&&typeof parsed==='object')return `<div class="decoded-content">${badge('decoded JSON','ok')}${renderJSONTree(parsed,600,2)}<details class="formatted-raw"><summary>Original string</summary><pre>${esc(value)}</pre></details></div>`;}catch(_){}}
  return renderRichText(value);
}

function renderRichText(value){
  const text=String(value??'');const lines=text.split('\n');const markdownLike=lines.some(line=>/^\s*(#{1,6}\s|[-*+]\s|\d+\.\s|```)/.test(line));
  if(!markdownLike)return `<pre class="formatted-text">${esc(text)}</pre>`;
  let inCode=false;const rendered=[];
  for(const line of lines){if(/^\s*```/.test(line)){inCode=!inCode;rendered.push(inCode?'<pre class="rich-code">':'</pre>');continue;}if(inCode){rendered.push(`${esc(line)}\n`);continue;}const heading=line.match(/^\s*(#{1,6})\s+(.*)$/);if(heading){const level=Math.min(6,heading[1].length+3);rendered.push(`<h${level}>${renderInlineText(heading[2])}</h${level}>`);continue;}const bullet=line.match(/^\s*[-*+]\s+(.*)$/);if(bullet){rendered.push(`<div class="rich-list"><span>•</span><p>${renderInlineText(bullet[1])}</p></div>`);continue;}const numbered=line.match(/^\s*(\d+)\.\s+(.*)$/);if(numbered){rendered.push(`<div class="rich-list"><span>${esc(numbered[1])}.</span><p>${renderInlineText(numbered[2])}</p></div>`);continue;}rendered.push(line.trim()?`<p>${renderInlineText(line)}</p>`:'<div class="rich-space"></div>');}
  if(inCode)rendered.push('</pre>');return `<div class="rich-text">${rendered.join('')}</div>`;
}
function renderInlineText(value){return esc(value).replace(/`([^`]+)`/g,'<code>$1</code>').replace(/\*\*([^*]+)\*\*/g,'<strong>$1</strong>');}
function renderJSONTree(value,max=400,openDepth=2){const budget={count:0,max,openDepth};return `<div class="json-tree">${renderJSONNode(value,'',0,budget)}</div>`;}

function renderDifferenceValues(left,right){left=String(left??'');right=String(right??'');let common=0;while(common<left.length&&common<right.length&&left[common]===right[common])common++;const removed=left.slice(common),added=right.slice(common);return `<section class="trace-diff expanded"><header>${badge('RESULT CHANGED','warning')}<span>${exactBytes(new TextEncoder().encode(left.slice(0,common)).length)} common · ${removed?`-${exactBytes(new TextEncoder().encode(removed).length)}`:'no removal'} · ${added?`+${exactBytes(new TextEncoder().encode(added).length)}`:'no addition'}</span></header>${removed?`<div class="diff-removed"><b>Removed</b>${renderContentValue(removed)}</div>`:''}${added?`<div class="diff-added"><b>Added before next model request</b>${renderContentValue(added)}</div>`:''}</section>`;}

function renderStandaloneContextDetail(detail){
  const system=contextArray(detail.system),tools=detail.tools||[],messages=detail.messages||[];
  return `<div class="standalone-workspace"><div class="context-hero"><div><span>Model</span><strong>${esc(detail.model||'unknown')}</strong></div><div><span>Exact request</span><strong>${exactBytes(detail.requestBytes)}</strong></div></div><div class="context-counts"><article><strong>${fmt(system.length)}</strong><span>system blocks</span></article><article><strong>${fmt(tools.length)}</strong><span>tool definitions</span></article><article><strong>${fmt(messages.length)}</strong><span>messages</span></article></div><details class="workspace-section" open><summary>System <span>${fmt(system.length)}</span></summary>${system.map((block,index)=>`<details class="workspace-item"><summary>#${index+1} · ${esc(block?.type||typeof block)}</summary>${renderContextSystemBlock(block,index)}</details>`).join('')}</details><details class="workspace-section"><summary>Tool definitions <span>${fmt(tools.length)}</span></summary>${tools.map((tool,index)=>`<details class="workspace-item"><summary>${esc(tool.name||`Tool ${index+1}`)} · ${exactBytes(tool.bytes)}</summary>${renderContextTool(tool,index)}</details>`).join('')}</details><details class="workspace-section"><summary>Messages <span>${fmt(messages.length)}</span></summary>${messages.map((message,index)=>`<details class="workspace-item"><summary>#${index+1} · ${esc(message.role||'unknown')} · ${exactBytes(message.bytes)}</summary>${renderContextMessage(message,index)}</details>`).join('')}</details><details class="workspace-section formatted-raw"><summary>Complete raw provider request</summary><pre class="raw-request-source">${esc(JSON.stringify(detail.rawRequest,null,2))}</pre></details></div>`;
}
function renderStandaloneToolDetail(data){return `<div class="standalone-workspace"><div class="tool-detail-hero"><div>${data.isError?badge('ERROR','danger'):badge('COMPLETED','ok')}${badge(data.propagation,data.propagation==='exact'||data.propagation==='client-result'?'ok':'warning')}</div><strong>${esc(data.toolName)}</strong><span>${esc(data.category)}</span></div><details class="workspace-section" open><summary>Arguments</summary>${data.argumentsTruncated?'<p class="warning-text">Preview truncated.</p>':''}${renderContentValue(data.argumentsJson)}</details><details class="workspace-section" open><summary>Exact tool result</summary>${data.resultTruncated?'<p class="warning-text">Preview truncated.</p>':''}${renderContentValue(data.resultText)}</details>${typeof data.providerResultText==='string'&&data.providerResultText!==''?`<details class="workspace-section" open><summary>Result in model context</summary>${data.providerResultTruncated?'<p class="warning-text">Preview truncated.</p>':''}${renderContentValue(data.providerResultText)}</details><details class="workspace-section"><summary>Propagation difference</summary>${renderDifferenceValues(data.resultText,data.providerResultText)}</details>`:''}<details class="workspace-section"><summary>Source ownership</summary>${renderJSONTree({currentCorpus:data.sourceMatches||[],composition:data.compositionMatches||[]},500,2)}</details>${evidenceDrawer(data.evidence)}</div>`;}

function resetFlowInspectorScroll(){state.flowInspectorScrollTop=0;const inspector=document.querySelector('.flow-inspector');if(inspector)inspector.scrollTop=0;}
function focusFlowInspector(){document.querySelector('.flow-inspector')?.focus({preventScroll:true});}
function resetFlowDetail(){state.flowDetail=null;state.flowDetailTab='overview';state.flowDetailItem='';state.flowDetailSearch='';state.flowDetailRawMode='pretty';resetFlowInspectorScroll();}
async function loadFlowContextDetail(exchange){if(!state.revealed)return promptReveal();state.flowDetail={kind:'context',id:exchange,title:`Context ${exchange}`,loading:true};state.flowDetailTab='overview';resetFlowInspectorScroll();render();try{const data=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/context?exchange=${encodeURIComponent(exchange)}`);if(state.flowDetail?.kind==='context'&&state.flowDetail.id===exchange)state.flowDetail={kind:'context',id:exchange,title:`Context ${exchange}`,data};}catch(error){if(state.flowDetail?.id===exchange)state.flowDetail={kind:'context',id:exchange,title:`Context ${exchange}`,error:error.message};}render();}
async function loadFlowToolDetail(id){if(!state.revealed)return promptReveal();state.flowDetail={kind:'tool',id,title:`Tool ${id}`,loading:true};state.flowDetailTab='overview';resetFlowInspectorScroll();render();try{const data=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/tool?id=${encodeURIComponent(id)}`);if(state.flowDetail?.kind==='tool'&&state.flowDetail.id===id)state.flowDetail={kind:'tool',id,title:data.toolName||id,data};}catch(error){if(state.flowDetail?.id===id)state.flowDetail={kind:'tool',id,title:id,error:error.message};}render();}
function openFlowEvidence(evidence,title){state.flowDetail={kind:'evidence',title,data:evidence||[]};state.flowDetailTab='overview';resetFlowInspectorScroll();render();}

function stopFlowPlayback(renderAfter=false){if(state.flowTimer){clearInterval(state.flowTimer);state.flowTimer=null;}state.flowPlaying=false;if(renderAfter)render();}
function toggleFlowPlayback(){const count=flowVisibleData().nodes.length;if(!count)return;if(state.flowPlaying){stopFlowPlayback(true);return;}if(state.flowReplayOrder>=count)state.flowReplayOrder=0;state.flowPlaying=true;state.flowTimer=setInterval(()=>{const total=flowVisibleData().nodes.length;if(state.flowReplayOrder>=total){stopFlowPlayback();render();return;}state.flowReplayOrder++;render();},900);render();}
function moveFlowReplay(delta){stopFlowPlayback();const count=flowVisibleData().nodes.length;state.flowReplayOrder=Math.max(0,Math.min(count,state.flowReplayOrder+delta));render();}

function traceIcon(step) { return ({prompt:'↗','model-text':'✦',thinking:'◌',tool:'⚙',context:'△',phase:'◆','provider-block':'◇'})[step.kind]||'•'; }
function traceActor(step) { return ({user:'USER',claude:'CLAUDE',tool:'TOOL',context:'CONTEXT',phase:'PHASE',provider:'PROVIDER'})[step.actor]||String(step.actor||'EVENT').toUpperCase(); }

function renderTraceStep(step,maxSize) {
  if(step.kind==='phase')return `<div class="trace-phase" id="trace-step-${step.order}"><span></span><b>${esc(step.title)}</b><small>${esc(step.invocationId||'')}</small></div>`;
  const contents=(step.contentRefs||[]).map(ref=>renderTraceContentRef(ref)).join('');
  const timing=step.timingObserved?duration(step.durationMs):'';
  return `<article class="trace-card kind-${esc(step.kind)} importance-${esc(step.importance)} ${step.status==='error'?'has-error':''}" id="trace-step-${step.order}"><div class="trace-rail"><span>${traceIcon(step)}</span><i></i></div><div class="trace-card-body"><header><div><span class="trace-actor">${esc(traceActor(step))}</span><h3>${esc(step.title)}</h3></div><div class="trace-card-meta">${step.status?statusBadge(step.status):''}${step.propagation?badge(step.propagation,step.propagation==='exact'?'ok':step.propagation==='different'?'warning':'danger'):''}${timing?`<span>${esc(timing)}</span>`:''}<strong>${step.sizeObserved?bytes(step.sizeBytes):'unknown'}</strong></div></header><progress class="trace-size-progress" max="${maxSize}" value="${step.sizeObserved?step.sizeBytes:0}"></progress>${(step.sizes||[]).length>1?`<div class="trace-size-parts">${step.sizes.map(size=>`<span>${esc(size.label)} <b>${size.observed?bytes(size.bytes):'unknown'}</b></span>`).join('')}</div>`:''}${contents}${renderTraceDifference(step)}<footer>${step.stopReason?`<span>stop: ${esc(step.stopReason)}</span>`:''}<button class="evidence-link" data-evidence='${esc(JSON.stringify(step.evidence||[]))}'>Evidence</button><span class="muted">${esc(step.correlationBasis)}</span></footer></div></article>`;
}

function renderTraceDifference(step) {
  if(step.kind!=='tool'||step.propagation!=='different')return '';
  const resultRef=(step.contentRefs||[]).find(ref=>ref.kind==='tool-result');const providerRef=(step.contentRefs||[]).find(ref=>ref.kind==='provider-result');
  if(!resultRef||!providerRef)return '';
  const exact=state.traceContent[resultRef.id];const observed=state.traceContent[providerRef.id];
  if(!exact||!observed)return `<div class="trace-diff-pending">${badge('DIFFERENT','warning')} Load both result payloads to display the exact difference.</div>`;
  return renderDifferenceValues(exact.content,observed.content);
}

function renderTraceContentRef(ref) {
  const detail=state.traceContent[ref.id];
  if(detail)return `<section class="trace-content"><header><b>${esc(ref.label)}</b><span>${exactBytes(detail.bytes)}${detail.truncated?' · preview truncated':''}</span></header>${renderTracePayload(detail)}</section>`;
  if(state.traceContentLoading[ref.id])return `<div class="trace-content-placeholder">Loading ${esc(ref.label)}…</div>`;
  return `<button class="trace-content-placeholder" data-trace-ref="${esc(ref.id)}"><span>${esc(ref.label)}</span><b>${ref.bytesObserved?bytes(ref.bytes):'unknown'}</b><small>${state.revealed?'Click to read':'Reveal plaintext to read'}</small></button>`;
}

function renderTracePayload(detail) {
  let parsed; let mode='text';
  try { parsed=JSON.parse(detail.content);mode='json';if(typeof parsed==='string'){try{const nested=JSON.parse(parsed);parsed=nested;mode='nested-json';}catch(_){}} } catch(_) {}
  if(mode==='json'||mode==='nested-json') return `${mode==='nested-json'?badge('decoded nested JSON','warning'):''}${renderJSONTree(parsed,500,2)}<details class="formatted-raw"><summary>Raw source</summary><pre>${esc(detail.content)}</pre></details>`;
  return renderRichText(detail.content);
}

function renderJSONNode(value,key,depth,budget) {
  if(budget.count>=budget.max)return '<span class="json-muted">… projection node limit …</span>';budget.count++;
  const keyHTML=key!==''?`<span class="json-key">${esc(key)}</span><span class="json-punct">: </span>`:'';const summaryKey=key!==''?`${keyHTML}`:'';const open=depth<(budget.openDepth??2)?' open':'';
  if(value===null)return `${keyHTML}<span class="json-null">null</span>`;
  if(Array.isArray(value)){const children=[];let omitted=0;for(let index=0;index<value.length;index++){if(budget.count>=budget.max){omitted=value.length-index;break;}children.push(`<div>${renderJSONNode(value[index],String(index),depth+1,budget)}</div>`);}return `<details class="json-node"${open}><summary>${summaryKey}<span class="json-type">Array</span> <span class="json-muted">[${value.length}]</span></summary><div>${children.join('')}${omitted?`<div class="json-omitted">… ${fmt(omitted)} items omitted from this projection</div>`:''}</div></details>`;}
  if(typeof value==='object'){const entries=Object.entries(value),children=[];let omitted=0;for(let index=0;index<entries.length;index++){if(budget.count>=budget.max){omitted=entries.length-index;break;}const [childKey,item]=entries[index];children.push(`<div>${renderJSONNode(item,childKey,depth+1,budget)}</div>`);}return `<details class="json-node"${open}><summary>${summaryKey}<span class="json-type">Object</span> <span class="json-muted">{${entries.length}}</span></summary><div>${children.join('')}${omitted?`<div class="json-omitted">… ${fmt(omitted)} properties omitted from this projection</div>`:''}</div></details>`;}
  if(typeof value==='string'){
    const valueBytes=new TextEncoder().encode(value).length;const trimmed=value.trim();let nested;
    if(trimmed&&(trimmed.startsWith('{')||trimmed.startsWith('['))){try{nested=JSON.parse(trimmed);}catch(_){nested=null;}}
    if(nested&&typeof nested==='object')return `<details class="json-node json-nested"${open}><summary>${summaryKey}<span class="json-type">JSON string</span> <span class="json-muted">${exactBytes(valueBytes)}</span></summary><div>${renderJSONNode(nested,'decoded',depth+1,budget)}<details class="formatted-raw"><summary>Original encoded string</summary><pre>${esc(value)}</pre></details></div></details>`;
    if(value.includes('\n')||value.length>240)return `<details class="json-node json-long-string"><summary>${summaryKey}<span class="json-type">String</span> <span class="json-muted">${fmt(value.split('\n').length)} lines · ${exactBytes(valueBytes)}</span></summary><div class="json-string-content">${renderRichText(value)}</div></details>`;
    return `${keyHTML}<span class="json-string">&quot;${esc(value)}&quot;</span>`;
  }
  if(typeof value==='number')return `${keyHTML}<span class="json-number">${esc(value)}</span>`;
  if(typeof value==='boolean')return `${keyHTML}<span class="json-boolean">${value}</span>`;
  return `${keyHTML}<span>${esc(value)}</span>`;
}

function renderCaptureIndex() {
  const rows=filterRows(state.captures);
  return `<section class="panel"><h2>Capture windows</h2><p class="muted">Manifest index only. Integrity is verified when a capture is opened.</p><table><thead><tr><th>Started</th><th>Label / ID</th><th>Status</th><th>Integrity</th><th>Duration</th><th>Size</th><th>Build</th></tr></thead><tbody>${rows.map(c=>`<tr class="clickable" data-capture-id="${esc(c.id)}"><td>${esc(date(c.startedAt))}</td><td><b>${esc(c.label||'unlabelled')}</b><small>${esc(c.id)}</small></td><td>${statusBadge(c.status)}</td><td>${statusBadge(c.integrity)}</td><td>${c.endedAt?duration(c.durationMs):'unknown'}</td><td>${bytes(c.sizeBytes)}</td><td>${esc(c.buildVersion||'unknown')}<small>${esc(short(c.buildCommit,14))}</small></td></tr>`).join('')}</tbody></table></section>`;
}

function renderOverview(view) {
  const o = view.overview;
  const diagnostics = filterRows(view.diagnostics);
  const total = Math.max(1, o.bundleBytes);
  return `
    <section class="metric-grid">
      ${metric('Capture duration', metricDisplay(view,'capture.duration'))}
      ${metric('Provider exchanges', fmt(o.providerExchanges), `${o.unattributedExchanges} unattributed`)}
      ${metric('Client sessions', fmt(o.clientSessions))}
      ${metric('Invocations', fmt(o.invocations))}
      ${metric('MCP processes', fmt(o.mcpProcesses))}
      ${metric('Tool executions', fmt(o.toolExecutions), `${o.toolErrors} errors`)}
      ${metric('Request wire bytes', bytes(o.totalRequestBytes))}
      ${metric('Largest request', bytes(o.largestRequestBytes))}
      ${metric('Cache read tokens', metricDisplay(view,'provider.tokens.cache_read'), 'provider-reported observed sum')}
      ${metric('Output tokens', metricDisplay(view,'provider.tokens.output'), 'provider-reported observed sum')}
      ${metric('Propagation exact', `${fmt(o.propagationExact)}/${fmt(o.propagationExact+o.propagationMissing)}`)}
      ${metric('Bundle size', bytes(o.bundleBytes))}
    </section>
    <div class="two-col">
      <section class="panel"><h2>Evidence storage</h2>${Object.entries(o.bytesByKind || {}).sort((a,b)=>b[1]-a[1]).map(([kind,n]) => barRow(kind, n, total, bytes(n))).join('')}</section>
      <section class="panel"><h2>Capture identity</h2><dl>
        <dt>ID</dt><dd>${esc(view.capture.id)}</dd><dt>Label</dt><dd>${esc(view.capture.label || '—')}</dd>
        <dt>Started</dt><dd>${esc(date(view.capture.startedAt))}</dd><dt>Ended</dt><dd>${esc(date(view.capture.endedAt))}</dd>
        <dt>Build</dt><dd>${esc(view.capture.buildVersion || 'unknown')} · ${esc(view.capture.buildCommit || 'unknown')}</dd>
        <dt>Provider</dt><dd>${esc(view.capture.providerOrigin || 'unknown')}</dd>
      </dl></section>
    </div>
    <section class="panel"><h2>Structural diagnostics</h2>${diagnostics.length ? diagnostics.map(d => `<button class="diagnostic ${esc(d.severity)}" data-evidence='${esc(JSON.stringify(d.evidence || []))}'><b>${esc(d.code)}</b><span>${esc(d.summary)}</span>${badge(d.basis)}</button>`).join('') : '<p class="muted">No structural diagnostics.</p>'}</section>
  `;
}

function renderHierarchy(view) {
  const runs = filterRows(view.evalRuns);
  const unattributed = filterRows(view.exchanges.filter(e=>!e.clientSessionId));
  return `<section class="panel"><h2>Eval → scenario → invocation → session/process</h2>
    ${runs.length ? runs.map(run => `<details open><summary>${statusBadge(run.status)} <b>Eval ${esc(run.id)}</b></summary>${run.scenarios.map(s => `<details open><summary>${statusBadge(s.status)} Scenario ${esc(s.id)} · ${fmt(s.artifacts?.length)} artifacts</summary>${s.invocations.map(i => `<article class="hierarchy-row"><div>${statusBadge(i.status)} ${badge(i.phase)} <b>${esc(i.id)}</b></div><div class="muted">session ${esc(short(i.clientSessionId, 24))} · ${fmt(i.providerExchanges)} exchanges · ${fmt(i.mcpProcesses)} MCP · ${i.timingObserved?duration(i.durationMs):'unknown'}</div>${evidenceButtons(i.evidence)}</article>`).join('')}</details>`).join('')}</details>`).join('') : '<p class="muted">No eval lifecycle. Unattributed provider traffic remains visible in Timeline and Raw.</p>'}
    <h3>Observed client sessions</h3><div class="cards">${view.sessions.map(s => `<article class="card"><b>${esc(short(s.id, 28))}</b><span>${fmt(s.providerExchanges)} exchanges</span><span>${esc((s.models || []).join(', '))}</span><small>${s.timingObserved?duration(s.durationMs):'unknown'}</small>${evidenceButtons(s.evidence)}</article>`).join('')}</div><h3>Unattributed provider exchanges (${fmt(unattributed.length)})</h3>${unattributed.length?`<table><thead><tr><th>Exchange</th><th>Method/path</th><th>Status</th><th>Time</th><th>Bytes in/out</th></tr></thead><tbody>${unattributed.map(e=>`<tr><td>${esc(e.id)}</td><td>${esc(e.method)} ${esc(e.path)}</td><td>${statusBadge(e.status)}</td><td>${esc(date(e.startedAt))}</td><td>${bytes(e.requestBytes)} / ${bytes(e.responseBytes)}</td></tr>`).join('')}</tbody></table>`:'<p class="muted">None.</p>'}
  </section>`;
}

function renderTimeline(view) {
  const events = filterRows(view.timeline);
  if (!events.length) return '<section class="panel"><p>No matching timeline events.</p></section>';
  const times = events.map(e => new Date(e.startedAt).getTime()).filter(Number.isFinite);
  const min = Math.min(...times), maxEnd = Math.max(...events.map(e => new Date(e.endedAt || e.startedAt).getTime()).filter(Number.isFinite));
  const span = Math.max(1, maxEnd - min);
  const lanes = [...new Set(events.map(e => e.lane))];
  return `<section class="panel timeline-panel"><h2>Unified evidence timeline</h2><p class="muted">Wall time positions events; per-stream sequence and evidence links remain authoritative.</p>
    <div class="timeline">${lanes.map(lane => `<div class="timeline-lane"><div class="lane-label">${esc(lane)}</div><div class="lane-track"><svg class="timeline-track-svg" viewBox="0 0 1000 38" preserveAspectRatio="none" aria-label="${esc(lane)} events">${events.filter(e => e.lane === lane).map(e => {
      const start = new Date(e.startedAt).getTime(); const end = new Date(e.endedAt || e.startedAt).getTime();
      const left = Math.max(0, (start-min)/span*1000); const width = Math.max(3.5, (Math.max(start,end)-start)/span*1000);
      return `<g class="timeline-event-svg status-${esc(e.status || 'unknown')}" data-event="${esc(e.id)}" tabindex="0" role="button" aria-label="${esc(`${e.title} · ${duration(e.durationMs)}`)}"><rect x="${left}" y="5" width="${width}" height="28" rx="4"><title>${esc(e.title)} · ${duration(e.durationMs)}</title></rect></g>`;
    }).join('')}</svg></div></div>`).join('')}</div>
    <div class="timeline-list">${events.slice(0, 1000).map(e => `<button class="event-row" data-event="${esc(e.id)}"><time>${esc(date(e.startedAt))}</time>${badge(e.lane)}${statusBadge(e.status)}<b>${esc(e.title)}</b><span>${duration(e.durationMs)}</span></button>`).join('')}</div>
  </section>`;
}

function renderProvider(view) {
  const blocks = filterRows(view.providerBlocks);
  const events = filterRows(state.providerEvents);
  const blockTypes = Object.entries(blocks.reduce((all,b)=>{all[b.type]=(all[b.type]||0)+1;return all;},{}));
  return `<section class="panel"><h2>Provider SSE event index</h2><p class="muted">Decoded order and offsets are exact. Existing gzip captures expose response-entity-end time only; it is labeled as such and not presented as an exact per-event timestamp.</p><div class="metric-grid">${metric('SSE events loaded',`${fmt(state.providerEvents.length)}/${fmt(view.providerEventTotal)}`)}${metric('Content blocks',fmt(blocks.length))}${blockTypes.slice(0,6).map(([type,count])=>metric(type,fmt(count))).join('')}</div><h2>Content blocks</h2><table><thead><tr><th>Exchange/index</th><th>Type</th><th>Tool identity</th><th>Text</th><th>Thinking</th><th>Input JSON</th><th>Decoded offsets</th><th>Status</th></tr></thead><tbody>${blocks.map(b=>`<tr><td>${esc(b.exchangeId)}<small>#${b.index}</small></td><td>${badge(b.type)}</td><td>${esc(b.toolName||'—')}<small>${esc(b.toolUseId||'')}</small></td><td>${bytes(b.textBytes)}</td><td>${bytes(b.thinkingBytes)}</td><td>${bytes(b.inputJsonBytes)}</td><td>${fmt(b.startedOffset)} → ${fmt(b.completedOffset)}</td><td>${statusBadge(b.status)}</td></tr>`).join('')}</tbody></table><h2>Every SSE event</h2><table><thead><tr><th>Exchange / ordinal</th><th>Event</th><th>Block / delta</th><th>Stop reason</th><th>Decoded offset</th><th>Payload</th><th>Timestamp basis</th></tr></thead><tbody>${events.map(e=>`<tr class="clickable" data-provider-event="${esc(e.exchangeId)}" data-ordinal="${e.ordinal}"><td>${esc(e.exchangeId)}<small>#${e.ordinal}</small></td><td>${esc(e.type)}</td><td>${esc(e.blockType||'—')} / ${esc(e.deltaType||'—')}</td><td>${esc(e.stopReason||'—')}</td><td>${fmt(e.decodedOffset)}</td><td>${bytes(e.payloadBytes)}</td><td>${badge(e.timestampBasis)}</td></tr>`).join('')}</tbody></table>${state.providerEvents.length<view.providerEventTotal?`<button class="button" data-action="provider-more">${state.providerLoading?'Loading…':'Load more SSE events'}</button>`:''}</section>`;
}

function renderClient(view) {
  const rows = filterRows(view.clientRuns);
  const conversation = filterRows(view.conversation);
  return `<section class="panel"><h2>Claude stream-json and eval evidence</h2><p class="muted">Client-reported timing/cost is external artifact evidence and remains separate from provider wire metrics. Content is metadata-only until explicit plaintext reveal.</p><div class="metric-grid">${metric('Client turns',metricDisplay(view,'client.turns'))}${metric('Client duration',metricDisplay(view,'client.duration'))}${metric('Reported cost',metricDisplay(view,'client.cost'))}${metric('Rate-limit events',fmt(view.overview.rateLimitEvents))}${metric('Thinking events',fmt(view.overview.thinkingEvents))}${metric('Permission denials',fmt(view.overview.permissionDenials))}</div>${rows.map(r=>`<article class="process"><header>${statusBadge(r.resultStatus)}<b>${esc(r.kind)}</b>${badge(r.model)}</header><div>${esc(r.artifactPath)}</div><div class="muted">session ${esc(short(r.clientSessionId,28))} · client ${esc(r.clientVersion||'unknown')}</div><div class="metric-grid compact">${metric('Turns',r.turnReports?fmt(r.turns):'unknown')}${metric('Duration',r.durationReports?duration(r.durationMs):'unknown')}${metric('TTFT',r.ttftReports?duration(r.ttftMs):'unknown')}${metric('Cost',r.costReports?`$${Number(r.reportedCostUsd).toFixed(4)}`:'unknown')}${metric('Assistant/user events',`${fmt(r.assistantEvents)}/${fmt(r.userEvents)}`)}${metric('Thinking/rate limits',`${fmt(r.thinkingEvents)}/${fmt(r.rateLimitEvents)}`)}</div><footer>stop ${esc(r.stopReason||'unknown')} · terminal ${esc(r.terminalReason||'unknown')}</footer></article>`).join('')||'<p>No stream-json artifacts.</p>'}<h2>Conversation/event index</h2><table><thead><tr><th>Artifact / line</th><th>Type</th><th>Role</th><th>Content blocks</th><th>Content</th><th>Thinking</th><th>Tools use/result</th><th>Session/request</th></tr></thead><tbody>${conversation.map(e=>`<tr class="clickable" data-artifact-line="${esc(e.artifactPath)}" data-line="${e.line}"><td>${esc(e.artifactKind)}<small>#${e.line}</small></td><td>${statusBadge(e.isError?'error':e.subtype)}${esc(e.type)}</td><td>${esc(e.role||'—')}</td><td>${esc((e.contentTypes||[]).join(', ')||'—')}</td><td>${bytes(e.contentBytes)}<small>text ${bytes(e.textBytes)}</small></td><td>${bytes(e.thinkingBytes)}</td><td>${fmt(e.toolUses)} / ${fmt(e.toolResults)}</td><td>${esc(short(e.clientSessionId,16))}<small>${esc(short(e.requestId,16))}</small></td></tr>`).join('')}</tbody></table></section>`;
}

function renderContext(view) {
  const rows = filterRows(view.contexts);
  const max = Math.max(1, ...rows.map(r => r.requestBytes));
  return `<section class="panel"><h2>Model context by provider request</h2><div class="legend">${badge('system','system')}${badge('tool schemas','tools')}${badge('messages','messages')}${badge('metadata/other','other')}</div>
    <div class="context-chart">${rows.map((r, index) => `<button class="context-row" data-context-exchange="${esc(r.exchangeId)}">
      <span class="context-label">${esc(r.exchangeId)}<small>${esc(r.model)} · ${fmt(r.messageCount)} messages</small></span>
      <svg class="context-stack-svg" viewBox="0 0 ${max} 20" preserveAspectRatio="none" aria-label="Context composition"><rect class="system" x="0" y="1" width="${r.systemBytes}" height="18"></rect><rect class="tools" x="${r.systemBytes}" y="1" width="${r.toolBytes}" height="18"></rect><rect class="messages" x="${r.systemBytes+r.toolBytes}" y="1" width="${r.messageBytes}" height="18"></rect><rect class="other" x="${r.systemBytes+r.toolBytes+r.messageBytes}" y="1" width="${r.otherBytes}" height="18"></rect></svg>
      <span>${bytes(r.requestBytes)} · +${bytes(r.addedMessageBytes)} · cache ${fmt(r.cacheReadInputTokens)}</span>
      ${r.contextReset?badge('history reset','danger'):''}${r.historyRewritten?badge('history rewritten','warning'):''}${(r.systemChanged || r.toolsChanged) ? badge('context changed','warning') : ''}
    </button>`).join('')}</div>
    <table><thead><tr><th>Exchange</th><th>System</th><th>Tools</th><th>Messages</th><th>Added / removed</th><th>Input</th><th>Cache create/read</th><th>Output</th></tr></thead><tbody>${rows.map(r => `<tr><td>${esc(r.exchangeId)}</td><td>${fmt(r.systemBlocks)} / ${bytes(r.systemBytes)}</td><td>${fmt(r.toolCount)} / ${bytes(r.toolBytes)}</td><td>${fmt(r.messageCount)} / ${bytes(r.messageBytes)}</td><td>+${fmt(r.addedMessages)} / -${fmt(r.removedMessages)} / ~${fmt(r.rewrittenMessages)}<small>${bytes(r.addedMessageBytes)}</small></td><td>${observed(r.inputTokens,r.inputTokensObserved)}</td><td>${observed(r.cacheCreationInputTokens,r.cacheCreationInputTokensObserved)} / ${observed(r.cacheReadInputTokens,r.cacheReadInputTokensObserved)}</td><td>${observed(r.outputTokens,r.outputTokensObserved)}</td></tr>`).join('')}</tbody></table>
  </section>`;
}

function renderTools(view) {
  const rows = filterRows(view.tools);
  return `<section class="panel"><h2>Causal tool executions</h2><table><thead><tr><th>#</th><th>Phase/session</th><th>Tool</th><th>Arguments</th><th>Result</th><th>Duration</th><th>Propagation</th><th>Owners</th></tr></thead><tbody>
    ${rows.map((t, i) => `<tr class="clickable" data-tool-id="${esc(t.id)}"><td>${i+1}</td><td>${esc(t.invocationId || 'unattributed')}<small>${esc(short(t.clientSessionId,20))}</small></td><td>${statusBadge(t.isError?'error':'ok')}${badge(t.category)}<b>${esc(t.toolName || t.mcpToolName)}</b><small>${esc(t.correlationBasis)}</small></td><td>${bytes(t.argumentsBytes)} ${t.argumentsEqual?badge('exact','ok'):badge('differs','warning')}</td><td>${bytes(t.resultBytes)}</td><td>${t.timingObserved?duration(t.durationMs):'unknown'}</td><td>${badge(t.propagation,t.propagation==='exact'||t.propagation==='client-result'?'ok':'warning')}</td><td>${esc([...(t.sourceOwners||[]),...(t.compositionOwners||[])].join(', ')||'—')}</td></tr>`).join('')}
  </tbody></table></section>`;
}

function renderMCP(view) {
  const rows = filterRows(view.mcpProcesses);
  const calls = filterRows(view.mcpCalls);
  const max = Math.max(1, ...rows.map(r => r.inputBytes + r.outputBytes));
  return `<section class="panel"><h2>MCP process streams</h2>${rows.map(p => `<article class="process"><header>${statusBadge(p.status)}<b>${esc(p.file)}</b>${badge(p.phase)}</header><div class="muted">${esc(p.invocationId || 'unattributed')}</div>${barRow('stdin',p.inputBytes,max,bytes(p.inputBytes))}${barRow('stdout',p.outputBytes,max,bytes(p.outputBytes))}<footer>${fmt(p.toolCalls)} tool calls · ${fmt(p.progressNotifications)} progress notifications</footer></article>`).join('')}<h2>All JSON-RPC methods</h2><table><thead><tr><th>Time</th><th>Method / tool</th><th>Kind</th><th>Status</th><th>ID</th><th>Bytes in/out</th><th>Latency</th><th>Scope</th></tr></thead><tbody>${calls.map(c=>`<tr><td>${esc(date(c.startedAt||c.completedAt))}</td><td><b>${esc(c.method||'response')}</b><small>${esc(c.toolName||'')}</small></td><td>${badge(c.kind)}</td><td>${statusBadge(c.status)}</td><td>${esc(c.requestId||'—')}</td><td>${bytes(c.requestBytes)} / ${bytes(c.responseBytes)}</td><td>${c.timingObserved?duration(c.durationMs):'unknown'}</td><td>${esc(c.phase||'')}<small>${esc(c.invocationId||'unattributed')}</small></td></tr>`).join('')}</tbody></table></section>`;
}

function renderSources(view) {
  const rows = filterRows(view.sources).sort((a,b)=>b.matchedBytes-a.matchedBytes);
  const max = Math.max(1, ...rows.map(r => r.matchedBytes));
  return `<section class="panel"><h2>Guidance source ownership</h2><p class="muted">Capture-time composition proof and current-corpus candidate matches remain separate.</p>${rows.length ? rows.map(s => `<article class="source-row"><div>${badge(s.kind)}<b>${esc(s.owner)}</b><small>${esc(s.file || '')}</small></div>${barRow(`${fmt(s.occurrences)} occurrences`,s.matchedBytes,max,bytes(s.matchedBytes))}<span>${fmt(s.toolIds?.length)} tool results</span></article>`).join('') : '<p>No provenance matches.</p>'}</section>`;
}

function renderMetrics(view) {
  const rows = filterRows(view.metrics);
  return `<section class="panel"><h2>Evidence-backed metrics</h2><p class="muted">Unknown is never coerced to zero. Every value carries its scope, unit, denominator, evidence basis, missing count, and canonical file/range coordinates.</p><table><thead><tr><th>Metric</th><th>Value</th><th>Unit</th><th>Denominator</th><th>Observed / missing</th><th>Basis</th><th>Evidence</th><th>Scope</th></tr></thead><tbody>${rows.map(m=>`<tr title="${esc(m.description||'')}"><td>${esc(m.name)}<small>${esc(m.id)}</small></td><td>${formatMetric(m.value,m.unit)}</td><td>${esc(m.unit)}</td><td>${m.denominator==null?'n/a':fmt(m.denominator)}</td><td>${fmt(m.sampleCount)} / ${fmt(m.missingCount)}</td><td>${badge(m.evidenceBasis)}</td><td><button class="evidence-link" data-evidence='${esc(JSON.stringify(m.evidence||[]))}'>${fmt((m.evidence||[]).length)} refs</button></td><td>${esc(m.scope)}</td></tr>`).join('')}</tbody></table></section>`;
}

function renderArtifacts(view) {
  const rows = filterRows(view.artifacts);
  return `<section class="panel"><h2>Eval artifacts</h2><div class="artifact-grid">${rows.map(a => `<button class="artifact" data-artifact="${esc(a.path)}"><b>${esc(a.type || 'artifact')}</b><span>${esc(a.path)}</span><small>${bytes(a.sizeBytes)} · ${esc(short(a.sha256,14))}</small></button>`).join('') || '<p>No eval artifacts.</p>'}</div></section>`;
}

function renderRaw(view) {
  const rawFiles=(view.rawFiles||[]).filter(f=>['provider','mcp','lifecycle','provenance'].includes(f.kind));
  const source=state.rawItems.length||state.rawFile?state.rawItems:view.rawRecords;
  const rows=filterRows(source);
  return `<section class="panel"><h2>Canonical raw record index</h2><p class="muted">Summary records contain coordinates and sizes only. Full content requires plaintext reveal. Large streams use bounded pages.</p><label>Raw stream<select id="raw-file-select"><option value="">Initial cross-file projection (${view.rawRecordTotalKnown?fmt(view.rawRecordTotal):'unavailable'})</option>${rawFiles.map(f=>`<option value="${esc(f.path)}" ${f.path===state.rawFile?'selected':''}>${esc(f.path)} · ${bytes(f.sizeBytes)}</option>`).join('')}</select></label>${view.rawRecordsTruncated&&!state.rawFile?`<p class="warning-text">Initial projection truncated at ${fmt(view.rawRecords.length)} records; select a stream to page all coordinates.</p>`:''}<table><thead><tr><th>File / seq</th><th>Time</th><th>Kind</th><th>Scope</th><th>Bytes</th><th>Flags</th></tr></thead><tbody>${rows.map(r => `<tr class="clickable" data-raw-file="${esc(r.file)}" data-raw-seq="${r.seq}"><td>${esc(r.file)}<small>#${r.seq}</small></td><td>${esc(date(r.time))}</td><td>${esc(r.kind)}</td><td>${esc(r.exchangeId || r.invocationId || r.phase || '')}</td><td>${bytes(r.bodyBytes)}</td><td>${r.hasBody?badge('body'):''}${r.hasError?badge('error','danger'):''}${r.captureStatus?statusBadge(r.captureStatus):''}</td></tr>`).join('')}</tbody></table>${state.rawFile&&state.rawHasMore?`<button class="button" data-action="raw-more">${state.rawLoading?'Loading…':'Load more records'}</button>`:''}</section>`;
}

function renderCompare() {
  const options = state.captures.map(c=>`<option value="${esc(c.id)}">${esc(c.label || c.id)}</option>`).join('');
  return `<section class="panel"><h2>Compare captures</h2><div class="compare-controls"><label>Left<select id="compare-left">${state.captures.map(c=>`<option value="${esc(c.id)}" ${c.id===state.compareLeft?'selected':''}>${esc(c.label||c.id)}</option>`).join('')}</select></label><label>Right<select id="compare-right">${state.captures.map(c=>`<option value="${esc(c.id)}" ${c.id===state.compareRight?'selected':''}>${esc(c.label||c.id)}</option>`).join('')}</select></label><button class="button" data-action="compare">Compare</button></div>${state.comparison ? `<table><thead><tr><th>Metric</th><th>Left</th><th>Right</th><th>Delta</th><th>%</th><th>Missing L/R</th><th>Basis</th></tr></thead><tbody>${state.comparison.metrics.map(m=>`<tr><td>${esc(m.name)}<small>${esc(m.id)} · ${esc(m.unit)}</small></td><td>${formatMetric(m.left,m.unit)}</td><td>${formatMetric(m.right,m.unit)}</td><td class="${m.delta>0?'warning-text':m.delta<0?'ok-text':''}">${m.delta>0?'+':''}${formatMetric(m.delta,m.unit)}</td><td>${m.percent==null?'unknown':`${m.percent.toFixed(1)}%`}</td><td>${fmt(m.leftMissingCount)} / ${fmt(m.rightMissingCount)}</td><td>${badge(m.evidenceBasis)}</td></tr>`).join('')}</tbody></table>` : '<p class="muted">Choose two captures. Deltas are structural, not a semantic pass/fail score.</p>'}</section>`;
}

function formatMetric(value, unit) { if(value==null) return 'unknown'; return unit === 'bytes' ? bytes(value) : unit === 'ms' ? duration(value) : unit === 'usd' ? `$${Number(value).toFixed(4)}` : unit === 'boolean' ? (value?'true':'false') : fmt(value); }
function metricDisplay(view,id) { const found=(view.metrics||[]).find(m=>m.id===id); return found?formatMetric(found.value,found.unit):'unknown'; }
function observed(value,isObserved) { return isObserved?fmt(value):'unknown'; }
function barRow(label, value, max, formatted) { return `<div class="bar-row"><span>${esc(label)}</span><progress max="${Math.max(1,max)}" value="${Math.max(0,Number(value||0))}"></progress><strong>${esc(formatted)}</strong></div>`; }
function evidenceButtons(evidence) { return (evidence||[]).map(e=>`<button class="evidence-link" data-raw-file="${esc(e.file)}" data-raw-seq="${e.seqStart}">raw ${esc(e.file)}#${e.seqStart}</button>`).join(''); }
function filterRows(rows) { const q=state.query.trim().toLowerCase(); return !q ? (rows||[]) : (rows||[]).filter(row=>JSON.stringify(row).toLowerCase().includes(q)); }

function bind() {
  document.querySelector('#capture-select')?.addEventListener('change', async event => {
    state.captureId=event.target.value; state.compareLeft=state.captureId; state.comparison=null;
    try { await loadView(); if(state.tab==='trace')await loadTrace(); render(); } catch(error){ renderError(error); }
  });
  document.querySelector('#query')?.addEventListener('input', event => { state.query=event.target.value; render(); document.querySelector('#query')?.focus(); });
  document.querySelectorAll('[data-tab]').forEach(button => button.addEventListener('click',async()=>{state.tab=button.dataset.tab;if(state.tab!=='trace')stopFlowPlayback();render();if(state.tab==='trace'&&!state.trace)await loadTrace();if(state.tab==='provider'&&!state.providerEvents.length)await loadProviderEvents();if(state.tab==='raw'&&state.rawFile&&!state.rawItems.length)await loadRawRecords();}));
  document.querySelectorAll('[data-capture-id]').forEach(row=>row.addEventListener('click',async()=>{state.captureId=row.dataset.captureId;state.compareLeft=state.captureId;state.comparison=null;try{await loadView();state.tab='trace';await loadTrace();render();}catch(error){renderError(error);}}));
  document.querySelectorAll('[data-action]').forEach(button => button.addEventListener('click',()=>handleAction(button.dataset.action)));
  document.querySelector('.flow-inspector')?.addEventListener('keydown',event=>{if(event.key==='Escape'){event.preventDefault();handleAction('flow-clear');}});
  document.querySelectorAll('[data-raw-file]').forEach(button => button.addEventListener('click',()=>showRaw(button.dataset.rawFile,button.dataset.rawSeq)));
  document.querySelectorAll('[data-context-exchange]').forEach(button => button.addEventListener('click',()=>showContextDetail(button.dataset.contextExchange)));
  document.querySelectorAll('[data-provider-event]').forEach(button => button.addEventListener('click',()=>showProviderEvent(button.dataset.providerEvent,button.dataset.ordinal)));
  document.querySelectorAll('[data-tool-id]').forEach(button => button.addEventListener('click',()=>showTool(button.dataset.toolId)));
  document.querySelectorAll('[data-artifact]').forEach(button => button.addEventListener('click',()=>showArtifact(button.dataset.artifact)));
  document.querySelectorAll('[data-artifact-line]').forEach(button => button.addEventListener('click',()=>showArtifactLine(button.dataset.artifactLine,button.dataset.line)));
  document.querySelectorAll('[data-event]').forEach(button=>{const open=()=>showEvent(button.dataset.event);button.addEventListener('click',open);button.addEventListener('keydown',event=>{if(event.key==='Enter'||event.key===' '){event.preventDefault();open();}});});
  document.querySelectorAll('[data-evidence]').forEach(button => button.addEventListener('click',()=>showEvidence(JSON.parse(button.dataset.evidence||'[]'),button.textContent.trim())));
  document.querySelector('#raw-file-select')?.addEventListener('change',async event=>{state.rawFile=event.target.value;state.rawItems=[];state.rawAfter=0;state.rawHasMore=!!state.rawFile;render();if(state.rawFile)await loadRawRecords();});
  document.querySelector('#trace-session')?.addEventListener('change',async event=>{state.traceSession=event.target.value;state.traceInvocation='';await loadTrace();});
  document.querySelector('#trace-invocation')?.addEventListener('change',async event=>{state.traceInvocation=event.target.value;await loadTrace();});
  document.querySelector('#trace-focus')?.addEventListener('change',event=>{state.traceFocus=event.target.value;render();});
  document.querySelectorAll('[data-density]').forEach(button=>button.addEventListener('click',()=>{stopFlowPlayback();state.traceDensity=button.dataset.density;state.flowReplayOrder=0;render();}));
  document.querySelectorAll('[data-presentation]').forEach(button=>button.addEventListener('click',()=>{state.tracePresentation=button.dataset.presentation;if(state.tracePresentation==='cards')stopFlowPlayback();render();}));
  document.querySelectorAll('[data-flow-node]').forEach(button=>button.addEventListener('click',()=>{stopFlowPlayback();state.flowReplayOrder=0;resetFlowDetail();state.flowSelected=button.dataset.flowNode;state.flowSelectedEdge='';resetFlowInspectorScroll();render();focusFlowInspector();}));
  document.querySelectorAll('[data-flow-edge]').forEach(path=>{const select=()=>{stopFlowPlayback();state.flowReplayOrder=0;resetFlowDetail();state.flowSelectedEdge=path.dataset.flowEdge;state.flowSelected='';state.flowFocusPath=false;resetFlowInspectorScroll();render();focusFlowInspector();};path.addEventListener('click',select);path.addEventListener('keydown',event=>{if(event.key==='Enter'||event.key===' '){event.preventDefault();select();}});});
  document.querySelectorAll('[data-flow-context-detail]').forEach(button=>button.addEventListener('click',()=>loadFlowContextDetail(button.dataset.flowContextDetail)));
  document.querySelectorAll('[data-flow-tool-detail]').forEach(button=>button.addEventListener('click',()=>loadFlowToolDetail(button.dataset.flowToolDetail)));
  document.querySelectorAll('[data-flow-evidence]').forEach(button=>button.addEventListener('click',()=>openFlowEvidence(JSON.parse(button.dataset.flowEvidence||'[]'),button.dataset.flowEvidenceTitle||'Evidence')));
  document.querySelectorAll('[data-flow-detail-tab]').forEach(button=>button.addEventListener('click',()=>{state.flowDetailTab=button.dataset.flowDetailTab;state.flowDetailItem='';state.flowDetailSearch='';resetFlowInspectorScroll();render();}));
  document.querySelectorAll('[data-flow-detail-item]').forEach(button=>button.addEventListener('click',()=>{state.flowDetailItem=button.dataset.flowDetailItem;render();}));
  document.querySelectorAll('[data-flow-raw-mode]').forEach(button=>button.addEventListener('click',()=>{state.flowDetailRawMode=button.dataset.flowRawMode;render();}));
  document.querySelectorAll('[data-flow-open-raw]').forEach(button=>button.addEventListener('click',()=>{if(!state.revealed)return promptReveal();const file=button.dataset.flowOpenRaw,seq=button.dataset.flowOpenSeq;resetFlowDetail();state.flowSelected='';state.flowSelectedEdge='';render();showRaw(file,seq);}));
  document.querySelectorAll('[data-trace-ref]').forEach(button=>button.addEventListener('click',()=>loadTraceContent(button.dataset.traceRef)));
  document.querySelectorAll('[data-trace-jump]').forEach(button=>button.addEventListener('click',()=>document.querySelector(`#trace-step-${button.dataset.traceJump}`)?.scrollIntoView({behavior:'smooth',block:'center'})));
  document.querySelector('#flow-replay-range')?.addEventListener('input',event=>{stopFlowPlayback();state.flowReplayOrder=Number(event.target.value)||0;render();});
  document.querySelector('#flow-detail-search')?.addEventListener('input',event=>{state.flowDetailSearch=event.target.value;render();document.querySelector('#flow-detail-search')?.focus();});
}

async function loadTraceContent(ref,quiet=false) {
  if(!state.revealed){if(!quiet)promptReveal();return;}
  if(state.traceContent[ref]||state.traceContentLoading[ref])return;
  state.traceContentLoading[ref]=true;if(!quiet)render();
  try { state.traceContent[ref]=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/trace-content?ref=${encodeURIComponent(ref)}`); }
  catch(error){if(!quiet)alert(`Trace content: ${error.message}`);}
  finally {delete state.traceContentLoading[ref];if(!quiet)render();}
}

async function loadTraceVisibleContent() {
  if(!state.revealed)return promptReveal();
  const refs=visibleTraceSteps().flatMap(step=>step.contentRefs||[]).map(ref=>ref.id).filter(ref=>!state.traceContent[ref]&&!state.traceContentLoading[ref]).slice(0,50);
  if(!refs.length)return;
  refs.forEach(ref=>{state.traceContentLoading[ref]=true;});render();
  await Promise.all(refs.map(async ref=>{try{state.traceContent[ref]=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/trace-content?ref=${encodeURIComponent(ref)}`);}catch(_){ }finally{delete state.traceContentLoading[ref];}}));
  render();
}

async function loadProviderEvents() {
  if(state.providerLoading||!state.view||state.providerEvents.length>=state.view.providerEventTotal)return;
  state.providerLoading=true; render();
  try { const page=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/provider-events?offset=${state.providerEvents.length}&limit=1000`); state.providerEvents.push(...page.items); }
  catch(error){ alert(`Provider events: ${error.message}`); }
  finally { state.providerLoading=false; render(); }
}

async function loadRawRecords() {
  if(state.rawLoading||!state.rawFile||(!state.rawHasMore&&state.rawItems.length))return;
  state.rawLoading=true; render();
  try { const page=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/records?file=${encodeURIComponent(state.rawFile)}&after=${state.rawAfter}&limit=500`); state.rawItems.push(...page.items);state.rawAfter=page.nextAfter;state.rawHasMore=page.hasMore; }
  catch(error){ alert(`Raw records: ${error.message}`); }
  finally { state.rawLoading=false; render(); }
}

async function handleAction(action) {
  if (action==='close-drawer') return closeDrawer();
  if (action==='provider-more') return loadProviderEvents();
  if (action==='raw-more') return loadRawRecords();
  if (action==='trace-reload') return loadTrace();
  if (action==='trace-load-visible') return loadTraceVisibleContent();
  if (action==='flow-prev') return moveFlowReplay(-1);
  if (action==='flow-next') return moveFlowReplay(1);
  if (action==='flow-play') return toggleFlowPlayback();
  if (action==='flow-focus') { state.flowFocusPath=!state.flowFocusPath; return render(); }
  if (action==='flow-detail-back') { resetFlowDetail(); return render(); }
  if (action==='flow-clear') { resetFlowDetail();state.flowSelected='';state.flowSelectedEdge='';state.flowFocusPath=false;if(state.tracePresentation==='split')state.tracePresentation='flow'; return render(); }
  if (action==='flow-reset') { stopFlowPlayback();resetFlowDetail();state.flowSelected='';state.flowSelectedEdge='';state.flowFocusPath=false;state.flowReplayOrder=0; return render(); }
  if (action==='reveal') {
    if (state.revealed) return;
    if (!confirm('Capture bodies can contain plaintext prompts, source code, tool inputs/results, and secrets. Authorization headers are absent, but bodies are not redacted. Reveal plaintext in this browser session?')) return;
    try { await api('/api/v1/reveal',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({confirm:'REVEAL'})}); state.revealed=true; render(); if(state.tab==='trace')await loadTraceVisibleContent(); } catch(error){ renderError(error); }
  }
  if (action==='compare') {
    state.compareLeft=document.querySelector('#compare-left').value; state.compareRight=document.querySelector('#compare-right').value;
    try { state.comparison=await api(`/api/v1/compare?left=${encodeURIComponent(state.compareLeft)}&right=${encodeURIComponent(state.compareRight)}`); render(); } catch(error){ renderError(error); }
  }
}

async function showProviderEvent(exchange,ordinal) {
  if (!state.revealed) return promptReveal();
  try { openDrawer(`Provider ${exchange} #${ordinal}`,'Loading…'); const data=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/provider-event?exchange=${encodeURIComponent(exchange)}&ordinal=${encodeURIComponent(ordinal)}`); let payload=data.payload; try { payload=JSON.stringify(JSON.parse(payload),null,2); } catch(_) {} openDrawer(`Provider ${exchange} #${ordinal}`,`<pre>${esc(payload)}</pre>${evidenceDrawer([data.evidence])}`); } catch(error){openDrawer('Provider event error',`<p class="error-text">${esc(error.message)}</p>`);}
}

async function showContextDetail(exchange) {
  if (!state.revealed) return promptReveal();
  try { openDrawer(`Context ${exchange}`,'<div class="flow-detail-loading"><i></i><p>Reading model context…</p></div>','context-workspace');const detail=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/context?exchange=${encodeURIComponent(exchange)}`);openDrawer(`Context ${exchange}`,renderStandaloneContextDetail(detail),'context-workspace'); }
  catch(error) { openDrawer('Context detail error',`<p class="error-text">${esc(error.message)}</p>`,'context-workspace'); }
}

async function showRaw(file, seq) {
  if (!state.revealed) return promptReveal();
  try { openDrawer(`${file} #${seq}`,'Loading…'); const data=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/raw?file=${encodeURIComponent(file)}&seq=${encodeURIComponent(seq)}`); openDrawer(`${file} #${seq}`,`<pre>${esc(JSON.stringify(data,null,2))}</pre>`); } catch(error){ openDrawer('Raw evidence error',`<p class="error-text">${esc(error.message)}</p>`); }
}
async function showTool(id) {
  if (!state.revealed) return promptReveal();
  try { openDrawer(`Tool execution ${id}`,'<div class="flow-detail-loading"><i></i><p>Reading tool evidence…</p></div>','tool-workspace');const data=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/tool?id=${encodeURIComponent(id)}`);openDrawer(`${data.category} · ${data.toolName}`,renderStandaloneToolDetail(data),'tool-workspace'); }
  catch(error){openDrawer('Tool detail error',`<p class="error-text">${esc(error.message)}</p>`,'tool-workspace');}
}
async function showArtifactLine(path,line) {
  if (!state.revealed) return promptReveal();
  try { openDrawer(`${path} #${line}`,'Loading…'); const data=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/artifact-line?path=${encodeURIComponent(path)}&line=${encodeURIComponent(line)}`); let content=data.content; try { content=JSON.stringify(JSON.parse(content),null,2); } catch(_) {} openDrawer(`${path} #${line}`,`${data.truncated?'<p class="warning-text">Line preview truncated.</p>':''}<pre>${esc(content)}</pre>`); } catch(error){openDrawer('Artifact line error',`<p class="error-text">${esc(error.message)}</p>`);}
}

async function showArtifact(path) {
  if (!state.revealed) return promptReveal();
  try { openDrawer(path,'Loading…'); const data=await api(`/api/v1/captures/${encodeURIComponent(state.captureId)}/artifact?path=${encodeURIComponent(path)}`); openDrawer(path,`${data.truncated?'<p class="warning-text">Preview truncated.</p>':''}<pre>${esc(data.content)}</pre>`); } catch(error){openDrawer('Artifact error',`<p class="error-text">${esc(error.message)}</p>`);}
}
function showEvent(id) { const event=state.view.timeline.find(item=>item.id===id); if(event) showEvidence(event.evidence,`${event.title} · ${event.kind}`); }
function showEvidence(evidence,title) { openDrawer(title,`<pre>${esc(JSON.stringify(evidence,null,2))}</pre>${evidenceDrawer(evidence)}`); }
function evidenceDrawer(evidence) { return (evidence||[]).map(e=>`<button class="button drawer-raw" data-file="${esc(e.file)}" data-seq="${e.seqStart}">Open ${esc(e.file)} #${e.seqStart}</button>`).join(''); }
function openDrawer(title,body,kind='') { const drawer=document.querySelector('#drawer');drawer.className=`drawer open ${kind}`.trim();document.querySelector('#drawer-backdrop')?.classList.add('open');document.querySelector('#drawer-title').textContent=title;document.querySelector('#drawer-body').innerHTML=body;document.querySelectorAll('.drawer-raw').forEach(b=>b.addEventListener('click',()=>showRaw(b.dataset.file,b.dataset.seq))); }
function closeDrawer(){document.querySelector('#drawer')?.classList.remove('open');document.querySelector('#drawer-backdrop')?.classList.remove('open');}
function promptReveal(){alert('Use “Reveal plaintext” first. Summary metrics remain available without exposing bodies.');}
function renderError(error){app.innerHTML=`<main class="empty"><h1>Capture inspector error</h1><pre>${esc(error?.stack||error)}</pre><button id="reload-app">Reload</button></main>`;document.querySelector('#reload-app')?.addEventListener('click',()=>location.reload());}

document.addEventListener('keydown',event=>{if(event.key!=='Escape')return;const drawer=document.querySelector('#drawer');if(drawer?.classList.contains('open'))return closeDrawer();if(state.flowDetail){resetFlowDetail();return render();}if(state.flowSelected||state.flowSelectedEdge){state.flowSelected='';state.flowSelectedEdge='';state.flowFocusPath=false;render();}});
boot();
