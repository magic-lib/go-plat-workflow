// 流程执行路径查看页（/flow-view）：只渲染根链流程图，并按 trace_id 高亮执行路径。
// 用法：/flow-view?project=yangloan&chain_id=R000012&trace_id=xxx
// 独立轻量实现，不依赖 common.js；节点配色、标签、路径高亮规则与编排页 Live Preview 保持一致。
(function () {
  'use strict';

  const params = new URLSearchParams(location.search);
  const project = (params.get('project') || '').trim();
  const chainId = (params.get('chain_id') || '').trim();
  const traceId = (params.get('trace_id') || '').trim();

  const $ = id => document.getElementById(id);
  let idMap = {};      // 实例 ID -> mermaid 安全 ID
  let baseSize = null; // SVG 原始尺寸 { w, h }
  let scale = 1;

  function esc(s) {
    if (s === null || s === undefined) return '';
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }
  // Mermaid 标签内文本转义（实体写法，避免破坏标签语法）
  function escMermaidText(s) {
    if (s === null || s === undefined) return '';
    const map = { '&': '#amp;', '<': '#lt;', '>': '#gt;', '"': '#quot;', "'": '#39;', '#': '#35;', ';': '#59;' };
    return String(s).replace(/[&<>"'#;]/g, c => map[c]);
  }
  function parseJSON(v, def) {
    if (v === null || v === undefined || v === '') return def;
    if (typeof v !== 'string') return v;
    try { return JSON.parse(v); } catch (e) { return def; }
  }
  function splitIds(s) {
    return String(s || '').split(',').map(x => x.trim()).filter(Boolean);
  }

  function showMsg(html) {
    const el = $('fv-msg');
    el.innerHTML = html;
    el.style.display = html ? '' : 'none';
  }

  async function api(path) {
    const sep = path.includes('?') ? '&' : '?';
    const res = await fetch(path + sep + 'project=' + encodeURIComponent(project), { credentials: 'same-origin' });
    let data = null;
    try { data = await res.json(); } catch (e) { data = null; }
    if (!res.ok) {
      const err = new Error((data && data.error) || res.statusText);
      err.status = res.status;
      throw err;
    }
    return data;
  }

  // ---------- 构建 Mermaid ----------
  const KIND_PALETTE = {
    condition: { fill: '#dcfce7', stroke: '#16a34a', color: '#14532d' }, // 查询获取=浅绿
    action: { fill: '#fee2e2', stroke: '#dc2626', color: '#7f1d1d' },    // 策略执行=淡红
    return: { fill: '#fef9c3', stroke: '#ca8a04', color: '#713f12' },    // 返回值节点=浅黄
    call: { fill: '#e0f2fe', stroke: '#0891b2', color: '#164e63' },      // 调用根链=浅青
    input: { fill: '#f3e8ff', stroke: '#9333ea', color: '#581c87' },     // 参数输入=浅紫
  };
  const KIND_FALLBACK = { fill: '#f3f4f6', stroke: '#9ca3af', color: '#374151' };
  const ROUTE_FILL = { fill: '#dbeafe', stroke: '#2563eb', color: '#1e3a8a' };

  function buildMermaid(chain, nodeDefs) {
    const nodeIds = splitIds(chain.node_ids);
    const subIds = splitIds(chain.sub_chain_ids);
    const conns = parseJSON(chain.connections_data, []) || [];
    const nameOv = parseJSON(chain.node_name_overrides, {}) || {};
    const switchOv = parseJSON(chain.node_switch_overrides, {}) || {};
    const dsl = parseJSON(chain.dsl_json, {}) || {};
    let dslNodes = ((dsl.metadata || {}).nodes) || [];
    if (!dslNodes.length) dslNodes = ((dsl.ruleChain || {}).nodes) || [];
    const dslById = {};
    dslNodes.forEach(n => { if (n && n.id) dslById[n.id] = n; });
    const defById = {};
    (nodeDefs || []).forEach(n => { if (n && n.node_id) defById[n.node_id] = n; });

    const allIds = new Set([...nodeIds, ...subIds]);
    conns.forEach(c => { allIds.add(c.from_id); allIds.add(c.to_id); });
    const nodeSet = new Set(nodeIds);
    const subSet = new Set(subIds);

    idMap = {};
    const used = new Set();
    let counter = 0;
    allIds.forEach(id => {
      let safe = String(id).replace(/[^a-zA-Z0-9_一-鿿]/g, '_');
      if (!safe || /^\d/.test(safe)) safe = 'id' + safe;
      while (used.has(safe)) safe = safe + '_' + (++counter);
      used.add(safe);
      idMap[id] = safe;
    });

    const lines = ['flowchart TD'];
    const kindSafeIds = {};
    nodeSet.forEach(id => {
      const safe = idMap[id];
      const baseId = id.indexOf('__') >= 0 ? id.split('__')[0] : id;
      const def = defById[baseId] || null;
      const dn = dslById[id] || null;
      const type = (def && def.type) || (dn && dn.type) || '';
      const cfg = (dn && dn.configuration) || {};
      const name = (nameOv[id] && String(nameOv[id]).trim()) || (dn && dn.name) || (def && def.name) || baseId;
      const isReturn = type === 'custom/ReturnValue';
      const kind = isReturn ? 'return' : ((def && def.kind) || 'action');
      let expr = '';
      if (type === 'custom/Activity' || type === 'custom/CondSwitch') {
        expr = Object.prototype.hasOwnProperty.call(switchOv, id) ? switchOv[id]
          : ((cfg.node_config && cfg.node_config.switch_condition) || '');
        expr = String(expr || '').replace(/\s+/g, ' ').trim();
      }
      const routing = !isReturn && (!!(def && def.has_switch_condition) || expr !== '');
      const clsName = 'nodeKind_' + kind.replace(/[^a-zA-Z0-9_]/g, '_') + (routing ? '_route' : '');
      const routeMark = routing ? " <span class='orch-route-mark'>🔀路由</span>" : '';
      let label = `<b>${routeMark}⚙ ${esc(name)}</b><br/><small>NodeId: ${esc(baseId)}</small>`;
      if (id !== baseId) label += `<br/><small>Id: ${esc(id)}</small>`;
      if (type === 'custom/RootChainCall' && cfg.target_chain_id) label += `<br/><small>→ 调用根链: ${esc(cfg.target_chain_id)}</small>`;
      if (expr) label += `<span class='orch-sw-cond'>⑂ ${escMermaidText(expr)}</span>`;
      lines.push(`    ${safe}["${label}"]`);
      (kindSafeIds[clsName] = kindSafeIds[clsName] || []).push(safe);
    });
    subSet.forEach(id => {
      const dn = dslById[id];
      const name = (dn && dn.name) || '';
      lines.push(`    ${idMap[id]}(["<b>🔗 ${esc(name || id)}</b>${name ? '<br/><small>' + esc(id) + '</small>' : ''}"])`);
    });
    allIds.forEach(id => {
      if (!nodeSet.has(id) && !subSet.has(id)) lines.push(`    ${idMap[id]}["<b>? ${esc(id)}</b>"]`);
    });
    // 相同 from->to 的连线合并为一条，关系类型逗号分隔
    const merged = [];
    const mergedMap = {};
    conns.forEach(c => {
      const from = idMap[c.from_id];
      const to = idMap[c.to_id];
      if (!from || !to) return;
      const key = c.from_id + '|' + c.to_id;
      const t = c.type || 'Success';
      if (mergedMap[key]) {
        if (!mergedMap[key].types.includes(t)) mergedMap[key].types.push(t);
      } else {
        mergedMap[key] = { from: from, to: to, types: [t] };
        merged.push(mergedMap[key]);
      }
    });
    merged.forEach(m => lines.push(`    ${m.from} -->|${m.types.join(',')}| ${m.to}`));
    Object.keys(kindSafeIds).forEach(clsName => {
      const routing = clsName.endsWith('_route');
      const baseKey = clsName.replace(/^nodeKind_/, '').replace(/_route$/, '');
      const c = routing ? ROUTE_FILL : (KIND_PALETTE[baseKey] || KIND_FALLBACK);
      lines.push(`    classDef ${clsName} fill:${c.fill},stroke:${c.stroke},color:${c.color},stroke-width:2px,text-align:left`);
      lines.push(`    class ${kindSafeIds[clsName].join(',')} ${clsName}`);
    });
    lines.push('    classDef subCls fill:#ede9fe,stroke:#7c3aed,color:#4c1d95,stroke-width:2px,text-align:left');
    const subClasses = [...subSet].map(id => idMap[id]).filter(Boolean).join(',');
    if (subClasses) lines.push(`    class ${subClasses} subCls`);
    return { syntax: lines.join('\n'), graphIds: new Set([...nodeIds, ...subIds]), conns: conns };
  }

  // ---------- 执行路径 ----------
  async function fetchTraceLogs() {
    const pageSize = 200;
    let all = [];
    for (let page = 1; page <= 5; page++) {
      const q = new URLSearchParams({ trace_id: traceId, root_chain_id: chainId, page: String(page), page_size: String(pageSize) });
      const resp = await api('/api/node-logs?' + q.toString());
      const list = (resp && resp.list) || [];
      all = all.concat(list);
      if (list.length < pageSize || all.length >= ((resp && resp.total) || 0)) break;
    }
    all = all.filter(r => r && r.node_id && r.level !== 'flow_total');
    const t = r => {
      const ms = Date.parse(r.created_at || '');
      return isNaN(ms) ? (Number(r.timestamp) || 0) * 1000 : ms;
    };
    all.sort((a, b) => (t(a) - t(b)) || ((a.id || 0) - (b.id || 0)));
    return all;
  }

  function buildTracePath(logs, graphIds, conns) {
    const nodes = {};
    const steps = [];
    logs.forEach(r => {
      if (!graphIds.has(r.node_id)) return;
      const order = steps.length + 1;
      steps.push({ instId: r.node_id, order: order, log: r });
      const n = nodes[r.node_id] || (nodes[r.node_id] = { orders: [], error: false });
      n.orders.push(order);
      if (r.level === 'error' || r.error_msg) n.error = true;
    });
    const edges = new Set();
    steps.forEach(s => {
      const rel = s.log.relation_type || 'Success';
      conns.forEach(c => {
        if (c.from_id !== s.instId || (c.type || 'Success') !== rel) return;
        const target = nodes[c.to_id];
        if (target && target.orders.some(o => o > s.order)) edges.add(c.from_id + '|' + c.to_id);
      });
    });
    for (let i = 0; i + 1 < steps.length; i++) {
      const a = steps[i].instId;
      const b = steps[i + 1].instId;
      const hasOut = [...edges].some(k => k.startsWith(a + '|'));
      if (!hasOut && conns.some(c => c.from_id === a && c.to_id === b)) edges.add(a + '|' + b);
    }
    return { steps: steps, nodes: nodes, edges: edges };
  }

  function applyTraceHighlight(trace) {
    const svg = document.querySelector('#fv-graph svg');
    if (!svg || !trace || !trace.steps.length) return;
    const SVGNS = 'http://www.w3.org/2000/svg';
    const reEsc = x => String(x).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    svg.classList.add('fv-trace-on');
    let defs = svg.querySelector('defs');
    if (!defs) { defs = document.createElementNS(SVGNS, 'defs'); svg.insertBefore(defs, svg.firstChild); }
    const marker = document.createElementNS(SVGNS, 'marker');
    marker.setAttribute('id', 'fv-trace-arrow');
    marker.setAttribute('viewBox', '0 0 10 10');
    marker.setAttribute('refX', '9'); marker.setAttribute('refY', '5');
    marker.setAttribute('markerWidth', '5'); marker.setAttribute('markerHeight', '5');
    marker.setAttribute('orient', 'auto-start-reverse');
    marker.setAttribute('markerUnits', 'strokeWidth');
    const tip = document.createElementNS(SVGNS, 'path');
    tip.setAttribute('d', 'M 0 0 L 10 5 L 0 10 z');
    tip.setAttribute('fill', '#ef4444');
    marker.appendChild(tip);
    defs.appendChild(marker);

    const nodeGs = Array.from(svg.querySelectorAll('g.node'));
    Object.keys(trace.nodes).forEach(instId => {
      const safe = idMap[instId];
      if (!safe) return;
      const re = new RegExp('flowchart-' + reEsc(safe) + '-\\d+$');
      const g = nodeGs.find(x => re.test(x.id || ''));
      if (!g) return;
      const info = trace.nodes[instId];
      g.classList.add('fv-trace-node');
      g.querySelectorAll(':scope > .label-container, :scope > g > .label-container').forEach(shape => {
        shape.style.setProperty('stroke', info.error ? '#7f1d1d' : '#ef4444', 'important');
        shape.style.setProperty('stroke-width', '4px', 'important');
        if (info.error) shape.style.setProperty('stroke-dasharray', '6 3', 'important');
      });
    });
    const edgePaths = Array.from(svg.querySelectorAll('path[data-id]'));
    const labelEls = Array.from(svg.querySelectorAll('.edgeLabel [data-id], .edgeLabels [data-id]'));
    trace.edges.forEach(key => {
      const parts = key.split('|');
      const from = idMap[parts[0]];
      const to = idMap[parts[1]];
      if (!from || !to) return;
      const re = new RegExp('^L_' + reEsc(from) + '_' + reEsc(to) + '_\\d+$');
      edgePaths.filter(p => re.test(p.getAttribute('data-id') || '')).forEach(p => {
        p.classList.add('fv-trace-edge');
        p.setAttribute('marker-end', 'url(#fv-trace-arrow)');
      });
      labelEls.filter(l => re.test(l.getAttribute('data-id') || '')).forEach(l => {
        (l.closest('g.edgeLabel') || l).classList.add('fv-trace-label');
      });
    });
  }

  // ---------- 缩放与拖拽 ----------
  function applyScale() {
    const svg = document.querySelector('#fv-graph svg');
    if (!svg || !baseSize) return;
    svg.style.width = Math.round(baseSize.w * scale) + 'px';
    svg.style.height = Math.round(baseSize.h * scale) + 'px';
    $('fv-zoom-label').textContent = Math.round(scale * 100) + '%';
  }
  // whole=true：整张图都放进窗口（"适应"按钮）；否则只按宽度适配（初始默认，长流程纵向滚动查看，保证文字清晰）
  function fitScale(whole) {
    const stage = $('fv-stage');
    if (!baseSize) return;
    const sw = (stage.clientWidth - 32) / baseSize.w;
    const sh = (stage.clientHeight - 32) / baseSize.h;
    scale = Math.max(0.2, Math.min(1, sw, whole ? sh : Infinity));
    applyScale();
  }
  function initControls() {
    $('fv-zoom').addEventListener('click', e => {
      const btn = e.target.closest('button[data-zoom]');
      if (!btn) return;
      const v = btn.getAttribute('data-zoom');
      if (v === 'fit') fitScale(true);
      else { scale = Math.max(0.2, Math.min(3, scale + Number(v))); applyScale(); }
    });
    const stage = $('fv-stage');
    let drag = null;
    stage.addEventListener('mousedown', e => {
      if (e.button !== 0) return;
      drag = { x: e.clientX, y: e.clientY, l: stage.scrollLeft, t: stage.scrollTop };
      stage.classList.add('panning');
    });
    window.addEventListener('mousemove', e => {
      if (!drag) return;
      stage.scrollLeft = drag.l - (e.clientX - drag.x);
      stage.scrollTop = drag.t - (e.clientY - drag.y);
    });
    window.addEventListener('mouseup', () => { drag = null; stage.classList.remove('panning'); });
  }

  function renderInfo(chain, trace, logsCount) {
    const parts = ['<b>' + esc(chain.name || chain.chain_id) + '</b>', esc(chain.chain_id)];
    if (traceId) {
      parts.push('traceId: ' + esc(traceId));
      if (!trace) parts.push('执行路径查询中…');
      else if (!logsCount) parts.push('<span class="err">该 traceId 在当前根链下没有节点日志</span>');
      else if (!trace.steps.length) parts.push('<span class="err">查到日志，但节点都不在当前图中</span>');
      else {
        const errCount = Object.values(trace.nodes).filter(n => n.error).length;
        parts.push('执行 ' + trace.steps.length + ' 步' + (errCount ? '，<span class="err">' + errCount + ' 个节点报错</span>' : ''));
      }
    }
    const info = $('fv-info');
    info.innerHTML = parts.join(' · ');
    info.title = info.textContent;
  }

  async function main() {
    if (!project || !chainId) {
      showMsg('缺少参数：请在地址中带上 project 和 chain_id，例如 /flow-view?project=xxx&chain_id=R000001&trace_id=xxx');
      return;
    }
    if (typeof mermaid === 'undefined') { showMsg('流程图组件（mermaid）加载失败，请检查网络'); return; }
    mermaid.initialize({ startOnLoad: false, theme: 'default', securityLevel: 'loose',
      flowchart: { useMaxWidth: false, htmlLabels: true, curve: 'basis' } });
    initControls();
    try {
      const [chain, nodeDefs] = await Promise.all([
        api('/api/root-chains/' + encodeURIComponent(chainId)),
        api('/api/nodes'),
      ]);
      if (!chain) { showMsg('未找到根链：' + esc(chainId)); return; }
      const built = buildMermaid(chain, nodeDefs);
      const logsPromise = traceId ? fetchTraceLogs() : Promise.resolve([]);
      const { svg } = await mermaid.render('fv-mermaid-' + Date.now(), built.syntax);
      $('fv-graph').innerHTML = svg;
      const svgEl = document.querySelector('#fv-graph svg');
      const vb = svgEl && svgEl.viewBox && svgEl.viewBox.baseVal;
      baseSize = vb && vb.width ? { w: vb.width, h: vb.height } : { w: svgEl.getBBox().width, h: svgEl.getBBox().height };
      $('fv-bar').classList.remove('hidden');
      $('fv-zoom').style.display = '';
      renderInfo(chain, null, 0);
      fitScale();
      showMsg('');
      const logs = await logsPromise;
      const trace = buildTracePath(logs, built.graphIds, built.conns);
      applyTraceHighlight(trace);
      renderInfo(chain, trace, logs.length);
    } catch (e) {
      if (e.status === 401) {
        showMsg('未登录或会话已过期，请先<a href="/" target="_blank" rel="noopener">登录流程管理中心</a>后刷新本页');
      } else {
        showMsg('加载失败：' + esc(e.message));
      }
    }
  }

  main();
})();
