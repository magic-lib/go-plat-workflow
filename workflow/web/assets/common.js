// ============================================================
// DSL modal helpers (shared by index.html and orch.html)
// ============================================================
function openDslModal(title, content) {
  document.getElementById('dsl-modal-title').innerHTML = esc(title) + '<button class="modal-close" onclick="closeDslModal()" title="关闭">&times;</button>';
  document.getElementById('dsl-modal-content').textContent = content;
  document.getElementById('dsl-modal-overlay').classList.add('show');
}
function closeDslModal() {
  document.getElementById('dsl-modal-overlay').classList.remove('show');
}
function copyDslContent() {
  const txt = document.getElementById('dsl-modal-content').textContent;
  navigator.clipboard.writeText(txt).then(() => showToast('已复制', '成功'), () => showToast('复制失败', 'error'));
}

// ============================================================
// Project management
// ============================================================
function getProject() {
  // 优先从地址栏 URL 的 ?project= 取值（权威来源），回退到下拉框
  const fromUrl = new URLSearchParams(window.location.search).get('project');
  if (fromUrl) return fromUrl;
  return document.getElementById('project-select').value;
}

// 根据当前用户对当前项目的角色，设置 body 权限标记：
//   .is-project-editor：可编辑（admin 或该项目 editor 角色）
//   .is-project-viewer：只读（该项目 viewer 角色，仅可查日志/单元测试）
function updateProjectPermission() {
  const p = getProject();
  const u = currentUser;
  let editable = false;
  if (u) {
    if (u.role === 'admin') editable = true;
    else if (u.project_roles && u.project_roles[p] === 'editor') editable = true;
  }
  document.body.classList.toggle('is-project-editor', editable);
  document.body.classList.toggle('is-project-viewer', !editable);
}

function onProjectChange() {
  // 直接从下拉框取当前选中的「新项目」。注意：不要调 getProject()，因为它优先读地址栏 URL，
  // 而下拉框刚改变时 URL 仍是旧项目（见下方 replaceState），会导致 p 取到旧值、切换失效。
  const p = document.getElementById('project-select').value;
  document.getElementById('project-badge').textContent = p || '-';
  // 将当前项目同步到地址栏 URL（?project=），刷新/分享后保持一致
  try {
    const url = new URL(location.href);
    if (p) url.searchParams.set('project', p);
    else url.searchParams.delete('project');
    history.replaceState(null, '', url.toString());
  } catch (_) {}
  // 持久化当前选择到 localStorage，刷新页面后自动恢复
  try { localStorage.setItem('wf_selected_project', p || ''); } catch (_) {}
  updateProjectPermission();
  if (!p) return;
  // 切换项目后，重新加载当前激活 tab 的数据（与 switchTab 的加载逻辑一致），
  // 保证所有查询都基于新项目；其余 tab 在点击时也会用 getProject()（已=新项目）重新加载。
  const activeTab = document.querySelector('.tab-btn.active');
  const tab = activeTab ? activeTab.dataset.tab : '';
  if (tab === 'nodes') loadNodeEnvOptions();
  else if (tab === 'activities') loadActivityEnvOptions();
  else if (tab === 'sub-chains') loadSubChains();
  else if (tab === 'root-chains') loadRootChains();
  else if (tab === 'logs') openAllLogsTab();
  else if (tab === 'statistics') loadStatistics();
  else if (tab === 'orchestrate') loadOrchData();
  else if (tab === 'execute') refreshConnSuggestions();
  refreshExecEnvs();
}

async function loadProjects() {
  try {
    const data = await api('/api/projects');
    // 前端兜底：普通用户（非 admin）仅展示被授权的项目，过滤掉无权限项。
    let list = data || [];
    if (currentUser && currentUser.role !== 'admin' && Array.isArray(currentUser.projects)) {
      const allowed = new Set(currentUser.projects);
      list = list.filter(p => allowed.has(p.project));
    }
    const sel = document.getElementById('project-select');
    const cur = sel.value;
    sel.innerHTML = '<option value="">-- 选择项目 --</option>';
    (list || []).forEach(p => {
      const opt = document.createElement('option');
      opt.value = p.project;
      opt.textContent = p.name ? p.project + ' (' + p.name + ')' : p.project;
      sel.appendChild(opt);
    });
    // 恢复之前选中值：优先地址栏 URL 的 ?project=，其次 localStorage，其次当前下拉值，最后回退到第一项
    const urlProject = new URLSearchParams(window.location.search).get('project') || '';
    let saved = '';
    try { saved = localStorage.getItem('wf_selected_project') || ''; } catch (_) {}
    if (urlProject && list.some(p => p.project === urlProject)) {
      sel.value = urlProject;
      onProjectChange();
    } else if (saved && list.some(p => p.project === saved)) {
      sel.value = saved;
      onProjectChange();
    } else if (cur && list.some(p => p.project === cur)) {
      sel.value = cur;
    } else if (list.length > 0) {
      sel.value = list[0].project;
      onProjectChange();
    }
    // 确保权限标记与当前项目一致（含未触发 onProjectChange 的分支）
    updateProjectPermission();
  } catch (e) { showToast('加载项目列表失败: ' + e.message, 'error'); }
}

function openProjectModal(proj) {
  // 普通用户（viewer）也能打开弹窗创建自己的项目；
  // 已有项目列表/密钥/环境配置等管理功能仅 admin 可见。
  const admin = isAdmin();
  document.querySelectorAll('#project-modal-overlay .admin-only').forEach(el => {
    el.style.display = admin ? '' : 'none';
  });
  // 重置表单
  resetProjectForm();
  if (proj) {
    document.getElementById('project-is-edit').value = '1';
    document.getElementById('project-modal-title').textContent = '项目管理';
    document.getElementById('project-project-id').value = proj.project;
    document.getElementById('project-project-id').readOnly = true;
    document.getElementById('project-name').value = proj.name || '';
    document.getElementById('project-status').value = String(proj.status);
    document.getElementById('project-description').value = proj.description || '';
    document.getElementById('project-save-btn').textContent = '更新';
    loadProjectSecrets(proj.project); // admin 可见：加载该项目的多密钥列表
  } else {
    loadProjectSecrets(''); // 新建态清空密钥列表
  }
  document.getElementById('project-modal-overlay').classList.add('show');
  loadProjectTable();
  showEnvConfigPanel();
}

function resetProjectForm() {
  document.getElementById('project-is-edit').value = '';
  document.getElementById('project-modal-title').textContent = '项目管理';
  document.getElementById('project-project-id').value = '';
  document.getElementById('project-project-id').readOnly = false;
  document.getElementById('project-name').value = '';
  document.getElementById('project-status').value = '1';
  document.getElementById('project-description').value = '';
  document.getElementById('project-save-btn').textContent = '新增';
  document.getElementById('project-secret').value = '';
  document.getElementById('project-secret-remark').value = '';
  _projectSecrets = [];
  renderProjectSecrets();
}

function closeProjectModal() {
  document.getElementById('project-modal-overlay').classList.remove('show');
}

async function loadProjectTable() {
  try {
    const data = await api('/api/projects');
    // 普通用户（非 admin）在已有项目列表中仅展示自己创建的项目
    let list = data || [];
    if (currentUser && currentUser.role !== 'admin' && currentUser.username) {
      list = list.filter(p => p.created_by === currentUser.username);
    }
    const tbody = document.querySelector('#projects-table tbody');
    if (!list.length) {
      tbody.innerHTML = '<tr><td colspan="5"><div class="empty-state"><p>暂无项目</p></div></td></tr>';
      return;
    }
    window._projectsForEdit = list; // 缓存供编辑按钮索引使用
    tbody.innerHTML = list.map((p, i) => `
      <tr>
        <td class="code-cell" title="${esc(p.project)}">${esc(p.project)}</td>
        <td>${esc(p.name)}</td>
        <td title="${esc(p.description||'')}">${esc(trunc(p.description, 30))}</td>
        <td><span class="badge ${p.status==1?'badge-on':'badge-off'}">${p.status==1?'启用':'禁用'}</span></td>
        <td class="actions">
          <button class="btn btn-sm btn-outline" onclick="editProjectByIndex(${i})">编辑</button>
          <button class="btn btn-sm btn-danger" onclick="deleteProject('${esc(p.project)}')">删除</button>
        </td>
      </tr>`).join('');

  } catch (e) { /* ignore */ }
}

function editProjectByIndex(i) { openProjectModal(window._projectsForEdit[i]); }

async function saveProject() {
  const isEdit = document.getElementById('project-is-edit').value === '1';
  const body = {
    project: document.getElementById('project-project-id').value.trim(),
    name: document.getElementById('project-name').value.trim(),
    status: parseInt(document.getElementById('project-status').value),
    description: document.getElementById('project-description').value.trim(),
  };
  if (!body.project) { showToast('Project ID 不能为空', 'error'); return; }
  try {
    let res;
    if (isEdit) {
      res = await fetch('/api/projects/' + encodeURIComponent(body.project), { method: 'PUT', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
      if (!res.ok) { const t = await res.json().catch(()=>({})); throw new Error(t.error || ('HTTP '+res.status)); }
      showToast('项目已更新', 'success');
    } else {
      res = await fetch('/api/projects', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
      if (!res.ok) { const t = await res.json().catch(()=>({})); throw new Error(t.error || ('HTTP '+res.status)); }
      showToast('项目已创建，已自动绑定到你的账号', 'success');
    }
    resetProjectForm();
    loadProjectTable();
    loadProjects(); // 刷新顶部下拉
  } catch (e) { showToast('保存失败: ' + e.message, 'error'); }
}

async function deleteProject(id) {
  if (!confirm('确定删除项目 ' + id + ' 吗？相关节点和链不会被删除。')) return;
  try {
    await fetch('/api/projects/' + encodeURIComponent(id), { method: 'DELETE' });
    showToast('项目已删除', 'success');
    loadProjectTable();
    loadProjects(); // 刷新顶部下拉
  } catch (e) { showToast('删除失败: ' + e.message, 'error'); }
}

let _projectSecrets = [];

async function loadProjectSecrets(project) {
  if (!project) { _projectSecrets = []; renderProjectSecrets(); return; }
  try {
    const list = await api('/api/projects/' + encodeURIComponent(project) + '/secrets');
    _projectSecrets = list || [];
  } catch (e) {
    _projectSecrets = [];
  }
  renderProjectSecrets();
}

function renderProjectSecrets() {
  const box = document.getElementById('project-secrets-list');
  if (!box) return;
  if (!_projectSecrets || !_projectSecrets.length) {
    box.innerHTML = '<div class="empty-state" style="padding:10px"><p style="margin:0">暂无密钥，添加后可分配给不同账户</p></div>';
    return;
  }
  box.innerHTML = _projectSecrets.map((s, i) => `
    <div class="secret-item">
      <code class="secret-mask copyable" title="点击复制" data-secret="${esc(s.key)}" onclick="copySecret(this)">${esc(s.key)}</code>
      <span class="secret-remark">${esc(s.remark || '')}</span>
      <button class="btn btn-sm btn-danger" onclick="deleteProjectSecret(${i})">删除</button>
    </div>`).join('');
}

function maskSecret(k) {
  if (!k) return '';
  if (k.length <= 4) return k;
  return k.slice(0, 2) + '****' + k.slice(-2);
}

async function addProjectSecret() {
  const project = document.getElementById('project-project-id').value.trim();
  if (!project) { showToast('请先填写 Project ID 并创建项目', 'error'); return; }
  const secret = document.getElementById('project-secret').value;
  if (!secret) { showToast('密钥不能为空', 'error'); return; }
  const remark = document.getElementById('project-secret-remark').value.trim();
  try {
    const res = await fetch('/api/projects/' + encodeURIComponent(project) + '/secrets', {
      method: 'POST', headers: {'Content-Type':'application/json'},
      body: JSON.stringify({ secret_key: secret, remark })
    });
    if (!res.ok) { const t = await res.json().catch(()=>({})); throw new Error(t.error || ('HTTP '+res.status)); }
    document.getElementById('project-secret').value = '';
    document.getElementById('project-secret-remark').value = '';
    showToast('密钥已添加', 'success');
    await loadProjectSecrets(project);
  } catch (e) { showToast('添加密钥失败: ' + e.message, 'error'); }
}

async function deleteProjectSecret(idx) {
  const s = _projectSecrets[idx];
  if (!s) return;
  const project = document.getElementById('project-project-id').value.trim();
  if (!project) { showToast('请先填写 Project ID', 'error'); return; }
  if (!confirm('确定删除该密钥吗？使用该密钥的账户将立即失去访问权限。')) return;
  try {
    const res = await fetch('/api/projects/' + encodeURIComponent(project) + '/secrets', {
      method: 'DELETE', headers: {'Content-Type':'application/json'},
      body: JSON.stringify({ secret_key: s.key })
    });
    if (!res.ok) { const t = await res.json().catch(()=>({})); throw new Error(t.error || ('HTTP '+res.status)); }
    showToast('密钥已删除', 'success');
    await loadProjectSecrets(project);
  } catch (e) { showToast('删除密钥失败: ' + e.message, 'error'); }
}

// copySecret 点击密钥文本即复制到剪贴板（带降级方案）
function copySecret(el) {
  const text = el.getAttribute('data-secret') || '';
  if (!text) return;
  const done = () => showToast('密钥已复制到剪贴板', 'success');
  const fallback = () => {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); done(); }
    catch (e) { showToast('复制失败，请手动复制', 'error'); }
    document.body.removeChild(ta);
  };
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(done).catch(fallback);
  } else {
    fallback();
  }
}

async function queryProjectConfig() {
  const project = document.getElementById('project-project-id').value.trim();
  if (!project) { showToast('请先填写 Project ID', 'error'); return; }
  const secret = document.getElementById('project-query-secret').value;
  if (!secret) { showToast('请输入项目密钥', 'error'); return; }
  try {
    const res = await fetch('/api/projects/' + encodeURIComponent(project) + '/config', {
      method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ secret_key: secret })
    });
    if (!res.ok) { const t = await res.json().catch(()=>({})); throw new Error(t.error || res.status); }
    const cfg = await res.json();
    renderProjectConfig(cfg);
    document.getElementById('project-config-overlay').classList.add('show');
  } catch (e) { showToast('查询失败: ' + e.message, 'error'); }
}

function renderProjectConfig(cfg) {
  const envs = (cfg.env_configs || []);
  const chains = (cfg.root_chains || []);
  const envRows = envs.length ? envs.map(e => `
    <tr>
      <td class="code-cell">${esc(e.env_name)}</td>
      <td>${esc(e.description||'')}</td>
      <td>${e.redis_config? esc(e.redis_config.addr||'') : '-'}</td>
      <td>${e.mysql_config? esc((e.mysql_config.host||'')+':'+(e.mysql_config.port||'')) : '-'}</td>
      <td>${(e.env_vars||[]).length}</td>
    </tr>`).join('') : '<tr><td colspan="5"><div class="empty-state"><p>暂无环境配置</p></div></td></tr>';

  const chainRows = chains.length ? chains.map(c => `
    <tr>
      <td class="code-cell">${esc(c.chain_id)}</td>
      <td>${esc(c.name)}</td>
      <td title="${esc(c.description||'')}">${esc(trunc(c.description||'', 40))}</td>
    </tr>`).join('') : '<tr><td colspan="3"><div class="empty-state"><p>暂无可执行的 RootChains</p></div></td></tr>';

  document.getElementById('project-config-body').innerHTML = `
    <div class="form-group" style="margin:10px 0">
      <label>项目</label>
      <div>${esc(cfg.project)} ${cfg.name? '(' + esc(cfg.name) + ')' : ''}</div>
    </div>
    <h4 style="font-size:.85rem;margin:12px 0 8px">环境配置（${envs.length}）</h4>
    <div class="table-wrap">
      <table>
        <thead><tr><th>环境名</th><th>描述</th><th>Redis</th><th>MySQL</th><th>变量数</th></tr></thead>
        <tbody>${envRows}</tbody>
      </table>
    </div>
    <h4 style="font-size:.85rem;margin:14px 0 8px">可执行的 RootChains（${chains.length}）</h4>
    <div class="table-wrap">
      <table>
        <thead><tr><th>Chain ID</th><th>名称</th><th>描述</th></tr></thead>
        <tbody>${chainRows}</tbody>
      </table>
    </div>`;
}

function closeProjectConfigModal() {
  document.getElementById('project-config-overlay').classList.remove('show');
}

// ============================================================
// 环境配置管理（挂在项目下：环境变量 / Redis / MySQL）
// ============================================================
function currentEnvProject() {
  // 环境配置跟随项目弹窗中正在编辑/新建的项目
  return (document.getElementById('project-is-edit').value === '1')
    ? document.getElementById('project-project-id').value.trim()
    : '';
}

function addEnvVarRow(key, value, desc) {
  const list = document.getElementById('env-vars-list');
  const row = document.createElement('div');
  row.className = 'form-row env-var-row';
  row.style.marginBottom = '6px';
  row.innerHTML = `
    <div class="form-group"><input class="env-var-key" placeholder="KEY" value="${esc(key||'')}"></div>
    <div class="form-group"><input class="env-var-value" placeholder="VALUE" value="${esc(value||'')}"></div>
    <div class="form-group"><input class="env-var-desc" placeholder="说明(可选)" value="${esc(desc||'')}"></div>
    <button type="button" title="删除" onclick="this.closest('.env-var-row').remove()" style="flex:0 0 auto;width:26px;height:26px;padding:0;border:1px solid #fca5a5;border-radius:6px;background:#fff;color:#dc2626;font-size:14px;line-height:1;cursor:pointer">×</button>`;
  list.appendChild(row);
}

function resetEnvConfigForm() {
  document.getElementById('env-name').value = '';
  document.getElementById('env-name').readOnly = false;
  document.getElementById('env-description').value = '';
  document.getElementById('env-vars-list').innerHTML = '';
  addEnvVarRow();
  ['env-redis-addr','env-redis-pwd','env-redis-user','env-mysql-host','env-mysql-user','env-mysql-pwd','env-mysql-db','env-mysql-dsn'].forEach(id=>document.getElementById(id).value='');
  document.getElementById('env-redis-db').value = '0';
  document.getElementById('env-mysql-port').value = '3306';
  document.getElementById('env-save-btn').textContent = '保存环境配置';
  const r = document.getElementById('env-redis-test-result');
  if (r) { r.style.display = 'none'; r.textContent = ''; r.className = 'redis-test-result'; }
}

function showEnvConfigPanel() {
  const p = currentEnvProject();
  const panel = document.getElementById('env-config-panel');
  const empty = document.getElementById('env-config-empty');
  if (p) {
    panel.style.display = '';
    empty.style.display = 'none';
    resetEnvConfigForm();
    loadEnvConfigTable();
  } else {
    panel.style.display = 'none';
    empty.style.display = '';
  }
}

async function loadEnvConfigTable() {
  const p = currentEnvProject();
  if (!p) return;
  try {
    const res = await fetch('/api/env-configs?project=' + encodeURIComponent(p));
    const data = await res.json();
    const tbody = document.querySelector('#env-configs-table tbody');
    if (!data.length) {
      tbody.innerHTML = '<tr><td colspan="7"><div class="empty-state"><p>暂无环境配置</p></div></td></tr>';
      return;
    }
    window._envConfigsForEdit = data;
    tbody.innerHTML = data.map((c, i) => `
      <tr>
        <td class="code-cell" title="${esc(c.env_name)}">${esc(c.env_name)}</td>
        <td title="${esc(c.description||'')}">${esc(trunc(c.description, 20))}</td>
        <td>${c.redis_config ? '<span class="badge badge-on">已配</span>' : '<span class="badge badge-off">未配</span>'}</td>
        <td>${c.mysql_config ? '<span class="badge badge-on">已配</span>' : '<span class="badge badge-off">未配</span>'}</td>
        <td>${(c.env_vars||[]).length}</td>
        <td title="开启后该环境下已发布的 Activity 掉线会发送告警">${
          (c.alert_config && c.alert_config.enabled)
            ? '<span class="badge badge-on">开</span>' : '<span class="badge badge-off">关</span>'}</td>
        <td class="actions">
          <button class="btn btn-sm btn-outline" onclick="editEnvConfigByIndex(${i})">编辑</button>
          <button class="btn btn-sm btn-outline" onclick="openEnvAlertConfig('${esc(c.env_name)}')" title="配置该环境的告警（通道/Webhook/阈值/提醒间隔）">🔔 告警</button>
          <button class="btn btn-sm btn-danger" onclick="deleteEnvConfig('${esc(c.env_name)}')">删除</button>
        </td>
      </tr>`).join('');
  } catch (e) { /* ignore */ }
}

function editEnvConfigByIndex(i) {
  const c = window._envConfigsForEdit[i];
  if (!c) return;
  document.getElementById('env-name').value = c.env_name || '';
  document.getElementById('env-name').readOnly = true;
  document.getElementById('env-description').value = c.description || '';
  document.getElementById('env-vars-list').innerHTML = '';
  (c.env_vars || []).forEach(v => addEnvVarRow(v.key, v.value, v.desc));
  if (!(c.env_vars || []).length) addEnvVarRow();
  const rc = c.redis_config || {};
  document.getElementById('env-redis-addr').value = rc.addr || '';
  document.getElementById('env-redis-pwd').value = rc.password || '';
  document.getElementById('env-redis-db').value = (rc.db == null ? 0 : rc.db);
  document.getElementById('env-redis-user').value = rc.username || '';
  const mc = c.mysql_config || {};
  document.getElementById('env-mysql-host').value = mc.host || '';
  document.getElementById('env-mysql-port').value = (mc.port == null ? 3306 : mc.port);
  document.getElementById('env-mysql-user').value = mc.user || '';
  document.getElementById('env-mysql-pwd').value = mc.password || '';
  document.getElementById('env-mysql-db').value = mc.db_name || '';
  document.getElementById('env-mysql-dsn').value = mc.dsn || '';
  document.getElementById('env-save-btn').textContent = '更新环境配置';
}

async function saveEnvConfig() {
  const p = currentEnvProject();
  if (!p) { showToast('请先填写并选择项目', 'error'); return; }
  const envName = document.getElementById('env-name').value.trim();
  if (!envName) { showToast('环境名不能为空', 'error'); return; }
  const vars = [];
  document.querySelectorAll('#env-vars-list .env-var-row').forEach(row => {
    const k = row.querySelector('.env-var-key').value.trim();
    const v = row.querySelector('.env-var-value').value;
    const d = row.querySelector('.env-var-desc').value.trim();
    if (k) vars.push({ key: k, value: v, desc: d });
  });
  const redis = envRedisFormConfig();
  const mysqlHost = document.getElementById('env-mysql-host').value.trim();
  const mysqlPort = parseInt(document.getElementById('env-mysql-port').value) || 3306;
  const mysql = (mysqlHost || document.getElementById('env-mysql-dsn').value.trim()) ? {
    host: mysqlHost,
    port: mysqlPort,
    user: document.getElementById('env-mysql-user').value.trim(),
    password: document.getElementById('env-mysql-pwd').value,
    db_name: document.getElementById('env-mysql-db').value.trim(),
    dsn: document.getElementById('env-mysql-dsn').value.trim(),
  } : null;
  // 告警配置由「🔔 告警」弹窗单独维护（存 alert_config JSON），
  // 这里必须带上原值，否则保存环境表单会把告警设置清掉
  const body = {
    project: p,
    env_name: envName,
    description: document.getElementById('env-description').value.trim(),
    env_vars: vars,
    redis_config: redis,
    mysql_config: mysql,
    alert_config: currentEnvAlertConfig(envName),
  };
  try {
    await fetch('/api/env-configs?project=' + encodeURIComponent(p), {
      method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body),
    });
    showToast('环境配置已保存', 'success');
    resetEnvConfigForm();
    loadEnvConfigTable();
  } catch (e) { showToast('保存失败: ' + e.message, 'error'); }
}

// 取指定环境当前的告警配置（从已加载的列表缓存中读取，未配置返回 null）
function currentEnvAlertConfig(envName) {
  const list = window._envConfigsForEdit || [];
  const c = list.find(x => x && x.env_name === envName);
  return (c && c.alert_config) ? c.alert_config : null;
}

// 打开环境告警设置弹窗
function openEnvAlertConfig(envName) {
  const p = currentEnvProject();
  if (!p) { showToast('请先选择项目', 'error'); return; }
  const cfg = currentEnvAlertConfig(envName) || {};
  document.getElementById('env-alert-env-name').value = envName;
  const tip = document.getElementById('env-alert-env-tip');
  if (tip) tip.textContent = p + ' / ' + envName;
  document.getElementById('env-alert-enabled').checked = !!cfg.enabled;
  document.getElementById('env-alert-channel').value = cfg.channel || 'feishu';
  document.getElementById('env-alert-webhook').value = cfg.webhook || '';
  document.getElementById('env-alert-threshold').value =
    (cfg.threshold_seconds == null ? '' : cfg.threshold_seconds);
  document.getElementById('env-alert-remind').value =
    (cfg.remind_minutes == null ? '' : cfg.remind_minutes);
  const ov = document.getElementById('env-alert-modal-overlay');
  if (ov) ov.classList.add('show');
}

function closeEnvAlertModal() {
  const ov = document.getElementById('env-alert-modal-overlay');
  if (ov) ov.classList.remove('show');
}

// 保存环境告警设置：读取该环境现有配置，仅替换 alert_config 后整体保存
// （后台以 JSON 存 alert_config；未填的阈值/提醒间隔留空即回落全局配置）
async function saveEnvAlertConfig() {
  const p = currentEnvProject();
  const envName = document.getElementById('env-alert-env-name').value.trim();
  if (!p || !envName) { showToast('环境信息不完整', 'error'); return; }
  const cur = (window._envConfigsForEdit || []).find(x => x && x.env_name === envName);
  if (!cur) { showToast('请先加载该环境的配置', 'error'); return; }

  const enabled = document.getElementById('env-alert-enabled').checked;
  const thresholdRaw = document.getElementById('env-alert-threshold').value.trim();
  const remindRaw = document.getElementById('env-alert-remind').value.trim();
  const threshold = thresholdRaw === '' ? null : (parseInt(thresholdRaw, 10) || null);
  const remind = remindRaw === '' ? null : parseInt(remindRaw, 10);
  if (remind !== null && (isNaN(remind) || remind < 0)) { showToast('重复提醒间隔不能为负数', 'error'); return; }
  if (threshold !== null && threshold <= 0) { showToast('离线判定阈值需大于 0', 'error'); return; }

  const alertConfig = {
    enabled: enabled,
    channel: document.getElementById('env-alert-channel').value || 'feishu',
    webhook: document.getElementById('env-alert-webhook').value.trim(),
  };
  // 用指针语义区分「未配置（回落全局）」与「显式 0（只提醒一次）」：不填即不带该字段
  if (threshold !== null) alertConfig.threshold_seconds = threshold;
  if (remind !== null) alertConfig.remind_minutes = remind;

  try {
    await fetch('/api/env-configs?project=' + encodeURIComponent(p), {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        project: p,
        env_name: envName,
        description: cur.description || '',
        env_vars: cur.env_vars || [],
        redis_config: cur.redis_config || null,
        mysql_config: cur.mysql_config || null,
        alert_config: alertConfig,
      }),
    });
    showToast(enabled ? '告警设置已保存（已开启）' : '告警设置已保存（已关闭）', 'success');
    closeEnvAlertModal();
    loadEnvConfigTable();
  } catch (e) { showToast('保存失败: ' + e.message, 'error'); }
}

// 从环境表单读取当前填写的 Redis 配置（未填 addr 返回 null）
function envRedisFormConfig() {
  const redisAddr = document.getElementById('env-redis-addr').value.trim();
  if (!redisAddr) return null;
  return {
    addr: redisAddr,
    password: document.getElementById('env-redis-pwd').value,
    db: parseInt(document.getElementById('env-redis-db').value) || 0,
    username: document.getElementById('env-redis-user').value.trim(),
  };
}

// 用表单里填写的 Redis 配置测试连接（不落库，保存前先确认连得上）
async function testEnvRedisConn() {
  const p = currentEnvProject();
  const cfg = envRedisFormConfig();
  const resultEl = document.getElementById('env-redis-test-result');
  const btn = document.getElementById('env-redis-test-btn');
  if (!cfg) {
    if (resultEl) {
      resultEl.style.display = 'inline-block';
      resultEl.className = 'redis-test-result redis-test-fail';
      resultEl.textContent = '✗ 请先填写 Addr';
    }
    showToast('请先填写 Redis Addr', 'error');
    return;
  }
  const originText = btn ? btn.textContent : '';
  if (btn) { btn.disabled = true; btn.textContent = '测试中…'; }
  if (resultEl) {
    resultEl.style.display = 'inline-block';
    resultEl.className = 'redis-test-result';
    resultEl.textContent = '正在连接 ' + cfg.addr + ' (db' + cfg.db + ')…';
  }
  try {
    const resp = await fetch('/api/env-configs/test-redis?project=' + encodeURIComponent(p || ''), {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ project: p || '', env_name: document.getElementById('env-name').value.trim(), redis_config: cfg }),
    });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) throw new Error(data.error || data.message || ('HTTP ' + resp.status));
    if (data.ok) {
      const parts = ['✓ 连接成功', data.ping ? 'PING=' + data.ping : '', data.server_version ? 'v' + data.server_version : '',
                     data.latency_ms != null ? data.latency_ms + 'ms' : '', data.db_size != null ? data.db_size + ' keys' : ''];
      const msg = parts.filter(Boolean).join(' · ');
      if (resultEl) { resultEl.className = 'redis-test-result redis-test-ok'; resultEl.textContent = msg; }
      showToast(msg, 'success');
    } else {
      const msg = '✗ 连接失败：' + (data.error || '未知错误');
      if (resultEl) { resultEl.className = 'redis-test-result redis-test-fail'; resultEl.textContent = msg; resultEl.title = data.error || ''; }
      showToast(msg, 'error');
    }
  } catch (e) {
    const msg = '✗ 连接失败：' + e.message;
    if (resultEl) { resultEl.className = 'redis-test-result redis-test-fail'; resultEl.textContent = msg; }
    showToast(msg, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = originText || '🔌 测试连接'; }
  }
}

async function deleteEnvConfig(envName) {
  const p = currentEnvProject();
  if (!p) return;
  if (!confirm('确定删除环境 ' + envName + ' 吗？')) return;
  try {
    await fetch('/api/env-configs/' + encodeURIComponent(envName) + '?project=' + encodeURIComponent(p), { method: 'DELETE' });
    showToast('环境配置已删除', 'success');
    loadEnvConfigTable();
  } catch (e) { showToast('删除失败: ' + e.message, 'error'); }
}

// ============================================================
// Tab switching
// ============================================================
document.querySelectorAll('.tab-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
    btn.classList.add('active');
    const tab = btn.dataset.tab;
    const content = document.getElementById('tab-' + tab);
    if (!content) return; // 跳转类按钮（如 orch.html 菜单跳回 index）无本页 content
    content.classList.add('active');
    if (tab === 'nodes') loadNodeEnvOptions();
    else if (tab === 'activities') { loadActivityEnvOptions(); }
    else if (tab === 'sub-chains') loadSubChains();
    else if (tab === 'root-chains') loadRootChains();
    else if (tab === 'logs') openAllLogsTab();
    else if (tab === 'orchestrate') loadOrchData();
    // 同步地址栏 ?tab=，刷新后停留在当前 tab（保留其它查询参数；用 replaceState 不产生多余后退记录）
    const url = new URL(location.href);
    url.searchParams.set('tab', tab);
    history.replaceState(null, '', url.toString());
  });
});

// ============================================================
// Toast
// ============================================================
function showToast(msg, type) {
  const t = document.createElement('div');
  t.className = 'toast ' + type;
  t.textContent = msg;
  document.body.appendChild(t);
  setTimeout(() => t.remove(), 3000);
}

// 复制文本到剪贴板（优先 navigator.clipboard，降级 execCommand）
// silent=true 时不弹出包含完整内容的提示，仅由调用方自行反馈（避免大段文本刷屏）
function copyToClipboard(text, silent) {
  const done = () => { if (!silent) showToast('已复制: ' + text, 'success'); };
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(done).catch(() => fallbackCopy(text, done));
  } else {
    fallbackCopy(text, done);
  }
}
function fallbackCopy(text, done) {
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  try { document.execCommand('copy'); done(); } catch (e) { showToast('复制失败', 'error'); }
  ta.remove();
}

// ============================================================
// Table filter
// ============================================================
function filterTable(tableId, q) {
  const rows = document.querySelectorAll('#' + tableId + ' tbody tr');
  const lq = q.toLowerCase();
  rows.forEach(r => r.style.display = r.textContent.toLowerCase().includes(lq) ? '' : 'none');
}

// Nodes 列表分页状态（前端分页：接口返回当前命名空间/标签/环境下的全量节点，搜索与大类筛选在前端完成）
let _nodePage = 1;
let _nodePageSize = 20;
let _nodeListRows = []; // 最近一次 /api/nodes 返回的列表（为空时不覆盖 _nodesForEdit，故单独记录）

// 搜索框 / 大类筛选变化：结果集改变，回到第一页
function filterNodes() {
  _nodePage = 1;
  renderNodeTable();
}

// 按搜索词、大类、标签过滤当前列表；保留原始下标（与 _nodesForEdit 一致，编辑按钮按下标取节点）
function applyNodeListFilter() {
  const searchEl = document.querySelector('#tab-nodes .search-box');
  const kindEl = document.getElementById('node-kind-filter');
  const tagEl = document.getElementById('node-tag-filter');
  const q = (searchEl ? searchEl.value : '').trim().toLowerCase();
  const kind = kindEl ? kindEl.value : '';
  const tag = tagEl ? tagEl.value : '';
  const result = [];
  _nodeListRows.forEach((n, i) => {
    const meta = nodeKindMeta(n.kind);
    if (kind && meta.kind !== kind) return;
    if (tag && !(n.tags || []).includes(tag)) return;
    if (q) {
      const text = [n.node_id, n.name, n.type, meta.label, n.namespace].concat(n.tags || []).join(' ').toLowerCase();
      if (!text.includes(q)) return;
    }
    result.push({ n: n, i: i });
  });
  return result;
}

// 渲染 Nodes 表格当前页；数据变少（筛选/删除）导致页码越界时退回最后一页
function renderNodeTable() {
  const tbody = document.querySelector('#nodes-table tbody');
  if (!tbody) return;
  const all = applyNodeListFilter();
  if (all.length === 0) {
    tbody.innerHTML = '<tr><td colspan="8"><div class="empty-state"><div class="icon">🔍</div><p>无匹配节点</p></div></td></tr>';
    renderNodePager(0);
    return;
  }
  const totalPages = Math.max(1, Math.ceil(all.length / _nodePageSize));
  if (_nodePage > totalPages) _nodePage = totalPages;
  if (_nodePage < 1) _nodePage = 1;
  const start = (_nodePage - 1) * _nodePageSize;
  tbody.innerHTML = all.slice(start, start + _nodePageSize).map(({ n, i }) => `
      <tr>
        <td class="code-cell" title="${esc(n.node_id)}">${esc(n.node_id)}</td>
        <td>${esc(n.name)}<span style="margin-left:4px">${nodeHeartbeatIconHtml(n.node_heartbeats)}</span>${nodeRouteBadge(n, true)}${n.published_in_root_chain ? publishedLockBadge('已发布') : ''}</td>
        <td><span class="code-cell">${esc(n.type)}</span></td>
        <td data-kind="${esc(nodeKindMeta(n.kind).kind)}"><span class="badge ${nodeKindMeta(n.kind).cls}">${nodeKindMeta(n.kind).label}</span></td>
        <td>${renderTagChipsHtml(n.tags)}</td>
        <td>${esc(n.namespace || '-')}</td>
        <td class="pubref-count" data-chains="${publishedRefsJsonAttr(n)}" data-empty="${(n.published_root_chains && n.published_root_chains.length) ? '' : '1'}">${publishedRefsCountHtml(n)}</td>
        <td>
          <div class="actions">
            <button class="btn btn-sm btn-outline btn-icon edit-only" ${actionIconAttrs('编辑', n.published_in_root_chain, '已发布到根链，禁止编辑')} onclick="editNodeByIndex(${i})">${actionIconSvg('edit')}</button>
            <button class="btn btn-sm btn-primary btn-icon" ${actionIconAttrs('测试')} onclick="openTestNodeModal('${esc(n.node_id)}')">${actionIconSvg('test')}</button>
            <button class="btn btn-sm btn-outline btn-icon" ${actionIconAttrs('日志')} onclick="openNodeLogModal('${esc(n.node_id)}')">${actionIconSvg('log')}</button>
            <button class="btn btn-sm btn-danger btn-icon edit-only" ${actionIconAttrs('删除', n.published_in_root_chain, '已发布到根链，禁止删除')} onclick="deleteNode('${esc(n.node_id)}')">${actionIconSvg('delete')}</button>
          </div>
        </td>
      </tr>`).join('');
  renderNodePager(all.length);
}

// 渲染 Nodes 分页器：有数据即展示总数与页码，便于切换每页条数
function renderNodePager(total) {
  const pager = document.getElementById('node-pager');
  if (!pager) return;
  if (total <= 0) {
    pager.style.display = 'none';
    return;
  }
  const totalPages = Math.max(1, Math.ceil(total / _nodePageSize));
  pager.style.display = 'flex';
  document.getElementById('node-page-info').textContent =
    '第 ' + _nodePage + ' / ' + totalPages + ' 页（共 ' + total + ' 条）';
  document.getElementById('node-prev').disabled = _nodePage <= 1;
  document.getElementById('node-next').disabled = _nodePage >= totalPages;
}

// 切换 Nodes 页码（基于当前筛选结果）
function changeNodePage(delta) {
  const totalPages = Math.max(1, Math.ceil(applyNodeListFilter().length / _nodePageSize));
  const newPage = _nodePage + delta;
  if (newPage < 1 || newPage > totalPages) return;
  _nodePage = newPage;
  renderNodeTable();
}

// 每页条数变化：持久化选择并回到第一页
function onNodePageSizeChange() {
  const sel = document.getElementById('node-page-size');
  const size = parseInt(sel ? sel.value : '', 10);
  if (!(size > 0)) return;
  _nodePageSize = size;
  try { localStorage.setItem('wf_node_page_size', String(size)); } catch (_) {}
  _nodePage = 1;
  renderNodeTable();
}

// 恢复上次选择的每页条数（仅接受下拉中存在的选项）
function restoreNodePageSize() {
  const sel = document.getElementById('node-page-size');
  if (!sel) return;
  let saved = '';
  try { saved = localStorage.getItem('wf_node_page_size') || ''; } catch (_) {}
  if (saved && Array.prototype.some.call(sel.options, o => o.value === saved)) sel.value = saved;
  _nodePageSize = parseInt(sel.value, 10) || 20;
}

// 渲染标签 chip 列表（列表列展示用，不可点击）
function renderTagChipsHtml(tags) {
  if (!tags || !tags.length) return '<span style="color:var(--text-muted)">-</span>';
  return tags.map(t => '<span class="badge ' + tagColorClass(t) + '" style="margin-right:4px">' + escHtml(t) + '</span>').join('');
}

// 读取节点弹窗当前已选标签（input 手动输入 + 已点选 chip）
function getNodeSelectedTags() {
  return document.getElementById('node-tags').value.split(',')
    .map(s => s.trim()).filter(s => s !== '');
}

// 设置节点弹窗当前已选标签（写回 input，并刷新 chips 高亮）
function setNodeSelectedTags(tags) {
  const uniq = [];
  (tags || []).forEach(t => { if (t && uniq.indexOf(t) < 0) uniq.push(t); });
  document.getElementById('node-tags').value = uniq.join(',');
  renderNodeTagChips();
}

// 收集节点弹窗当前已选标签（保存时调用）
function collectNodeTags() {
  return getNodeSelectedTags();
}

// 渲染可点击的历史标签 chips（已选中的高亮，点击切换）；数据来源为节点列表缓存
function renderNodeTagChips() {
  const box = document.getElementById('node-tags-chips');
  if (!box) return;
  const allTags = {};
  (window._nodesForEdit || []).forEach(n => {
    (n.tags || []).forEach(t => { if (t) allTags[t] = true; });
  });
  const selected = getNodeSelectedTags();
  const tags = Object.keys(allTags).sort();
  if (tags.length === 0) { box.innerHTML = ''; return; }
  box.innerHTML = tags.map(t => {
    const v = escHtml(t);
    const on = selected.indexOf(t) >= 0 ? ' on' : '';
    return '<span class="tag-chip' + on + '" onclick="toggleNodeTag(\'' + v.replace(/'/g, "\\'") + '\')">' + v + (on ? ' ✓' : '') + '</span>';
  }).join('');
}

// 点击 chip 切换标签选中状态
function toggleNodeTag(tag) {
  const selected = getNodeSelectedTags();
  const idx = selected.indexOf(tag);
  if (idx >= 0) {
    selected.splice(idx, 1);
  } else {
    selected.push(tag);
  }
  setNodeSelectedTags(selected);
}

// 根据当前节点列表所有标签刷新筛选下拉项
function refreshNodeTagFilter() {
  const sel = document.getElementById('node-tag-filter');
  if (!sel) return;
  const cur = sel.value;
  const tagSet = {};
  (window._nodesForEdit || []).forEach(n => {
    (n.tags || []).forEach(t => { if (t) tagSet[t] = true; });
  });
  const tags = Object.keys(tagSet).sort();
  sel.innerHTML = '<option value="">全部标签</option>' + tags.map(t => {
    const v = escHtml(t);
    return '<option value="' + v + '">' + v + '</option>';
  }).join('');
  if (tags.indexOf(cur) >= 0) sel.value = cur;
}

// 标签筛选变化：结果集改变并重新加载列表
function onNodeTagFilterChange() {
  _nodePage = 1;
  loadNodes();
}

// 命名空间筛选变化：结果集改变，回到第一页并重新加载列表
function onNodeNamespaceFilterChange() {
  _nodePage = 1;
  loadNodes();
}

// 读取 Nodes 列表上选择的环境（空字符串表示"全部环境"）
function getNodeListEnv() {
  const sel = document.getElementById('node-env-filter');
  return sel ? (sel.value || '') : '';
}

// 环境筛选变化：持久化选择 + 重新加载列表（有环境才带心跳）
function onNodeEnvFilterChange() {
  try { localStorage.setItem('wf_node_env', document.getElementById('node-env-filter').value); } catch (_) {}
  _nodePage = 1;
  loadNodes();
}

// Nodes 列表加载按项目缓存本次会话结果，避免重复拉取。
// 与 loadActivityEnvOptions / loadOrchData 同理：初始化时序里 顶层 loadProjects(.then) /
// bootstrap(onLoginSuccess→loadProjects) / applyInitialTab(switchTab) 都会触发 loadNodeEnvOptions，
// 导致 /api/nodes 与 /api/env-configs 同一项目被重复拉取。
let _nodeEnvLoadPromise = null;
let _nodeEnvLoadedProject = '';

// 初始化 Nodes 环境筛选下拉（复用环境配置列表），恢复上次选择，并加载列表
async function loadNodeEnvOptions(force = false) {
  const sel = document.getElementById('node-env-filter');
  if (!sel) return;
  const p = getProject();
  // 同一项目下并发/重复调用复用同一请求，仅拉取一次；force=true 可强制刷新。
  if (!force && _nodeEnvLoadPromise && _nodeEnvLoadedProject === p) {
    return _nodeEnvLoadPromise;
  }
  _nodeEnvLoadedProject = p;
  _nodePage = 1;
  restoreNodePageSize();
  _nodeEnvLoadPromise = (async () => {
    try {
      const envs = await fetchEnvConfigs();
      const savedEnv = (() => { try { return localStorage.getItem('wf_node_env') || ''; } catch (_) { return ''; } })();
      const list = Array.isArray(envs) ? envs : [];
      sel.innerHTML = '<option value="">全部环境</option>' + list.map(e =>
        '<option value="' + esc(e.env_name || e.name || e) + '">' + esc(e.env_name || e.name || e) + '</option>'
      ).join('');
      sel.value = savedEnv;
    } catch (e) { /* 环境列表可选，失败不影响节点列表 */ }
    loadNodes();
  })();
  return _nodeEnvLoadPromise;
}

// ============================================================
// API helpers (project query param auto appended)
// ============================================================
function apiUrl(path) {
  const sep = path.includes('?') ? '&' : '?';
  return path + sep + 'project=' + encodeURIComponent(getProject());
}

async function api(path, opts = {}) {
  const res = await fetch(apiUrl(path), opts);
  const data = await res.json();
  if (!res.ok) {
    const err = new Error(data.error || res.statusText);
    err.status = res.status;
    err.code = data.code || '';
    throw err;
  }
  return data;
}

// 保存子链：子链被已发布根链引用且编排内容有变化时，后端返回 409 + code=sub_chain_published，
// 提示这些根链需重新保存并发布才生效；确认后 doRequest(true) 带确认标记重新提交，取消返回 null。
async function apiSubChainSaveWithConfirm(doRequest) {
  try {
    return await doRequest(false);
  } catch (e) {
    if (e.code !== 'sub_chain_published') throw e;
    // 去掉后端错误链末尾的内部英文说明（workflow: ...），只给用户看中文提示
    const msg = String(e.message || '').replace(/（workflow:[^）]*）\s*$/, '');
    if (!window.confirm('⚠️ ' + msg + '\n\n确定保存子链吗？')) return null;
    return await doRequest(true);
  }
}

function escHtml(s) {
  if (!s) return '';
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

// 用于 HTML 属性值内部的转义（额外转义单引号）
function escAttr(s) {
  if (!s) return '';
  return escHtml(s).replace(/'/g,'&#39;');
}

// ============================================================
// Nodes
// ============================================================
async function loadNodes() {
  try {
    const env = getNodeListEnv();
    const nsFilter = document.getElementById('node-namespace-filter');
    const ns = nsFilter ? nsFilter.value : '';
    const tagFilter = document.getElementById('node-tag-filter');
    const tag = tagFilter ? tagFilter.value : '';
    const params = [];
    if (env) params.push('env=' + encodeURIComponent(env));
    if (ns) params.push('namespace=' + encodeURIComponent(ns));
    if (tag) params.push('tag=' + encodeURIComponent(tag));
    const envQ = params.length ? ('?' + params.join('&')) : '';
    const nodes = await api('/api/nodes' + envQ);
    const tbody = document.querySelector('#nodes-table tbody');
    _nodeListRows = nodes || [];
    if (!nodes.length) {
      tbody.innerHTML = '<tr><td colspan="8"><div class="empty-state"><div class="icon">📦</div><p>项目 <b>' + esc(getProject()) + '</b> 暂无节点数据</p></div></td></tr>';
      renderNodePager(0);
      return;
    }
    window._nodesForEdit = nodes; // 缓存供编辑按钮索引使用
    refreshNodeNamespaceFilter(); // 同步命名空间过滤下拉（基于当前项目 Activity 缓存）
    refreshNodeTagFilter(); // 同步标签过滤下拉（基于当前列表已有标签）
    // 保持当前页码（删除/编辑后刷新仍停留在当前页；当前页删空时 renderNodeTable 自动退回上一页）
    renderNodeTable();
  } catch (e) { showToast('加载节点失败: ' + e.message, 'error'); }
}

// 从项目下 Activity 缓存去重得到命名空间列表，填充 Node 弹窗的命名空间选择框
function refreshNodeNamespaceOptions() {
  const sel = document.getElementById('node-namespace');
  if (!sel) return;
  const cur = sel.value;
  const cache = (window._activityCache || []);
  const nsSet = {};
  cache.forEach(a => { if (a.act_namespace) nsSet[a.act_namespace] = true; });
  const namespaces = Object.keys(nsSet).sort();
  sel.innerHTML = '<option value="">（无）</option>' + namespaces.map(ns =>
    `<option value="${esc(ns)}">${esc(ns)}</option>`
  ).join('');
  // 保留当前选择（若有）
  sel.value = namespaces.indexOf(cur) >= 0 ? cur : '';
}

// 用当前项目下 Node 去重命名空间填充 Nodes 列表的命名空间过滤下拉
function refreshNodeNamespaceFilter() {
  const sel = document.getElementById('node-namespace-filter');
  if (!sel) return;
  const cur = sel.value;
  const nodes = (window._nodesForEdit || []);
  const nsSet = {};
  nodes.forEach(n => { if (n.namespace) nsSet[n.namespace] = true; });
  const namespaces = Object.keys(nsSet).sort();
  sel.innerHTML = '<option value="">全部命名空间</option>' + namespaces.map(ns =>
    `<option value="${esc(ns)}">${esc(ns)}</option>`
  ).join('');
  sel.value = namespaces.indexOf(cur) >= 0 ? cur : '';
}

function openNodeModal(node) {
  document.getElementById('node-is-edit').value = node ? '1' : '';
  document.getElementById('node-modal-title-text').textContent = node ? '编辑 Node' : '新增 Node';
  document.getElementById('node-node-id').value = node ? node.node_id : '';
  document.getElementById('node-node-id').readOnly = !!node;
  document.getElementById('node-name').value = node ? node.name || '' : '';
  // 命名空间选项来自该项目下 Activity 的去重命名空间；缓存就绪后再填充并回显选中值
  refreshNodeNamespaceOptions();
  document.getElementById('node-namespace').value = node ? node.namespace || '' : '';
  document.getElementById('node-type').value = node ? node.type : 'log';
  document.getElementById('node-kind').value = node ? node.kind || 'action' : 'action';
  document.getElementById('node-category').value = node ? node.category || '' : '';
  document.getElementById('node-tags').value = node && Array.isArray(node.tags) ? node.tags.join(',') : '';
  renderNodeTagChips();
  document.getElementById('node-version').value = node ? node.version || '' : '';
  document.getElementById('node-status').value = node ? String(node.status) : '0';
  document.getElementById('node-configuration').value = node ? prettyJson(node.configuration) : '{}';
  document.getElementById('node-additional-info').value = node ? prettyJson(node.additional_info) : '{}';
  // 回显 activity 编排（优先读 configuration.activities，兼容旧版 node_config 单 activity）
  let nodeCfg = {};
  if (node && node.configuration) {
    try { nodeCfg = typeof node.configuration === 'string' ? JSON.parse(node.configuration) : node.configuration; } catch(e) { /* ignore */ }
  }
  const nc = (nodeCfg && nodeCfg.node_config) || {};
  // 先加载本节点参数定义（#node-params-container），确保后续渲染 activity 参数明细时，
  // 「引用节点」来源能正确列出节点参数下拉，编辑回显时也能自动选中已绑定的节点参数。
  clearParamRows();
  if (node && node.params) {
    try {
      const params = typeof node.params === 'string' ? JSON.parse(node.params) : node.params;
      if (Array.isArray(params)) {
        params.forEach(p => addParamRow(p.key, p.label, p.type, p.required, p.value, p.description, p.policy));
      }
    } catch(e) { /* ignore */ }
  }
  // 还原已选活动（二维数组：stages）
  const stagesRoot = document.getElementById('node-activity-stages');
  stagesRoot.querySelectorAll('.stage').forEach(el => el.remove());
  let stages = parseStagesFromConfig(nodeCfg, nc);
  if (stages.length > 0) {
    stages.forEach(stage => addActivityStage(stage));
    document.getElementById('node-activity-empty').style.display = 'none';
    document.getElementById('node-activity-custom').style.display = 'none';
    document.getElementById('node-act-namespace').value = '';
    document.getElementById('node-act-name').value = '';
  } else {
    document.getElementById('node-activity-empty').style.display = '';
    document.getElementById('node-activity-custom').style.display = '';
    document.getElementById('node-act-namespace').value = nc.act_namespace || nodeCfg.act_namespace || '';
    document.getElementById('node-act-name').value = nc.act_name || nodeCfg.act_name || '';
  }
  onNodeTypeChange();
  // 如果缓存为空则先加载 activity 列表，加载完成后刷新各阶段下拉并重渲染已选 activity 的参数明细
  const afterCache = () => {
    document.querySelectorAll('#node-activity-stages .stage').forEach(s => populateStageSelect(s));
    // 缓存就绪后用真实中文名补全卡片标题（编辑回显时若缓存未加载，标题可能暂为 ns/act_name）
    document.querySelectorAll('#node-activity-stages .act-item').forEach(row => {
      const ns = row.getAttribute('data-ns') || '';
      const nm = row.getAttribute('data-name') || '';
      const cacheAct = (window._activityCache || []).find(a => a.act_namespace === ns && a.act_name === nm);
      if (cacheAct && cacheAct.name) {
        row.setAttribute('data-display-name', cacheAct.name);
        const titleSpan = row.querySelector('.act-item-name');
        if (titleSpan) {
          // 仅替换标题中第一个文本节点（中文名部分），保留括号内的 ns/act_name 与 #id
          titleSpan.childNodes[0].nodeValue = cacheAct.name + '  ';
        }
      }
    });
    document.querySelectorAll('#node-activity-stages .act-item').forEach(row => renderActivityItemParams(row));
    // 缓存就绪后补齐各卡片的测试示例展示
    document.querySelectorAll('#node-activity-stages .act-item').forEach(row => {
      const sampleBox = row.querySelector('.act-sample-box');
      if (sampleBox) loadActivitySample(row.getAttribute('data-ns') || '', row.getAttribute('data-name') || '', sampleBox);
    });
    // 缓存就绪后再渲染返回值定义：依赖本节点 Activity 引用，确保来源/类型/Activity选择/字段正确回显
    clearOutputRows();
    if (node && node.outputs) {
      try {
        const outputs = typeof node.outputs === 'string' ? JSON.parse(node.outputs) : node.outputs;
        if (Array.isArray(outputs)) {
          outputs.forEach(o => addOutputRow({ key: o.key, label: o.label, type: o.type, source: o.source || 'value', ref: o.ref || '', value: o.value || '' }));
        }
      } catch(e) { /* ignore */ }
    }
    // 缓存就绪后命名空间选项才完整，重新填充并回显选中值
    refreshNodeNamespaceOptions();
    document.getElementById('node-namespace').value = node ? node.namespace || '' : '';
  };
  if (window._activityCache.length === 0) {
    refreshActivityCache().then(afterCache);
  } else {
    afterCache();
  }
  document.getElementById('node-description').value = node ? node.description || '' : '';
  document.getElementById('node-modal-overlay').classList.add('show');
}

function closeNodeModal() {
  clearParamRows();
  document.getElementById('node-modal-overlay').classList.remove('show');
}

// 类型切换为 activity 时显示命名空间/活动名称区域
// 节点大类的显示名与徽标样式；未知或为空的大类按「策略执行」处理
const NODE_KIND_META = {
  condition: { label: '查询获取', cls: 'badge-warning' },
  action:    { label: '策略执行', cls: 'badge-info' },
  return:    { label: '返回值',   cls: 'badge-return' },
  call:      { label: '调用根链', cls: 'badge-call' },
  input:     { label: '参数输入', cls: 'badge-input' },
  loop:      { label: '循环',     cls: 'badge-loop' },
};
function nodeKindMeta(kind) {
  const k = NODE_KIND_META[kind] ? kind : 'action';
  return Object.assign({ kind: k }, NODE_KIND_META[k]);
}

// 内置节点类型对应的固定大类；其它类型只能选「查询获取 / 策略执行」
const NODE_TYPE_FIXED_KIND = {
  'custom/ReturnValue': 'return',
  'custom/RootChainCall': 'call',
  'custom/ParamInput': 'input',
  'custom/Loop': 'loop',
};

// 按节点类型同步「节点大类」：内置类型锁定为对应大类；普通类型若当前是内置大类则回到「策略执行」
function syncNodeKindByType(type) {
  const kindEl = document.getElementById('node-kind');
  if (!kindEl) return;
  const fixed = NODE_TYPE_FIXED_KIND[type];
  Array.from(kindEl.options).forEach(o => {
    const builtin = Object.values(NODE_TYPE_FIXED_KIND).includes(o.value);
    o.disabled = fixed ? o.value !== fixed : builtin;
  });
  if (fixed) kindEl.value = fixed;
  else if (!kindEl.value || Object.values(NODE_TYPE_FIXED_KIND).includes(kindEl.value)) kindEl.value = 'action';
}

function onNodeTypeChange() {
  const type = document.getElementById('node-type').value;
  syncNodeKindByType(type);
  ensureLoopItemsParamRow(type);
  toggleNodeChainCallSection(type);
  const isActivity = type === 'custom/Activity';
  document.getElementById('node-activity-section').style.display = isActivity ? '' : 'none';
  if (!isActivity) {
    // 非 activity 类型，恢复命名空间/活动名称可编辑
    document.getElementById('node-act-namespace').readOnly = false;
    document.getElementById('node-act-name').readOnly = false;
  }
  if (isActivity) {
    // 切换到 activity 且阶段为空时，若 configuration.node_config 有旧单 activity，则重建为首阶段首项
    if (collectStages().length === 0) {
      let cfg = {};
      try { cfg = JSON.parse(document.getElementById('node-configuration').value || '{}'); } catch(e) { cfg = {}; }
      const ncc = (cfg && cfg.node_config) || {};
      if (ncc.act_namespace && ncc.act_name) {
        addActivityStage([{ act_namespace: ncc.act_namespace, act_name: ncc.act_name, id: '', name: '', args: {} }]);
        document.getElementById('node-activity-empty').style.display = 'none';
        document.getElementById('node-activity-custom').style.display = 'none';
      }
    }
    syncActivityConfig();
    document.querySelectorAll('#node-activity-stages .stage').forEach(s => populateStageSelect(s));
  }
  // condSwitch / Activity 共用 node-cond-section 与 switch_condition 输入框
  const isCond = type === 'custom/CondSwitch';
  document.getElementById('node-cond-section').style.display = (isCond || isActivity) ? '' : 'none';
  if (isCond || isActivity) {
    let cfg = {};
    try { cfg = JSON.parse(document.getElementById('node-configuration').value || '{}'); } catch(e) { cfg = {}; }
    const ncfg = cfg.node_config || {};
    // 只读新字段 switch_condition（旧 condition 已完全废弃，不做回退兼容）
    document.getElementById('node-switch-condition').value = ncfg.switch_condition || '';
    if (isActivity) {
      // 执行条件：只读新字段 enable_condition（旧 node_condition 已完全废弃，不做回退兼容）
      const ecEl = document.getElementById('node-enable-condition');
      if (ecEl) {
        ecEl.value = (cfg.node_config && cfg.node_config.enable_condition) || '';
      }
      syncEnableConfig();
    }
    syncSwitchConfig();
    // 刷新路由条件下方的「可用变量」提示（列出本节点参数与返回值引用）
    renderCondVarSnippets();
  } else {
    document.getElementById('node-switch-condition').value = '';
  }
}

// 将「路由条件」输入框的内容写入 Configuration（node_config.switch_condition）。
// condSwitch / Activity 共用该字段：condSwitch 按返回值路由分支，Activity 执行成功后按返回值路由分支。
function syncSwitchConfig() {
  const t = document.getElementById('node-type').value;
  if (t !== 'custom/CondSwitch' && t !== 'custom/Activity') return;
  let cfg;
  try { cfg = JSON.parse(document.getElementById('node-configuration').value || '{}'); } catch(e) { cfg = {}; }
  if (typeof cfg !== 'object' || cfg === null) cfg = {};
  if (!cfg.node_config || typeof cfg.node_config !== 'object') cfg.node_config = {};
  cfg.node_config.switch_condition = document.getElementById('node-switch-condition').value;
  // 保证 CommConfiguration 各字段存在
  if (typeof cfg.arg_mapping === 'undefined') cfg.arg_mapping = {};
  if (typeof cfg.ret_mapping === 'undefined') cfg.ret_mapping = {};
  if (typeof cfg.arguments === 'undefined') cfg.arguments = [];
  if (typeof cfg.responses === 'undefined') cfg.responses = {};
  // 旧字段 condition 已废弃：保存时直接丢弃，统一使用 switch_condition
  if (cfg.node_config.condition !== undefined) {
    delete cfg.node_config.condition;
  }
  document.getElementById('node-configuration').value = prettyJson(cfg);
}

// 路由条件下方的「可用变量」提示：实时列出本节点的全部参数与返回值，
// 格式为 [arguments.参数key] / [responses.返回值key]，点击即可复制，供编写条件表达式使用。
function renderCondVarSnippets() {
  const box = document.getElementById('node-cond-var-snippets');
  if (!box) return;
  const items = [];
  // 节点参数：来自「参数定义」表单，格式 [arguments.<key>]
  nodeParamDefs().forEach(p => {
    if (!p.key) return;
    items.push({ ref: '[arguments.' + p.key + ']', label: p.label || '', type: p.type || '' });
  });
  // 节点返回值：来自「返回值定义」表单，格式 [responses.<key>]
  const outRows = document.querySelectorAll('#node-outputs-container .param-row');
  outRows.forEach(row => {
    const keyEl = row.querySelector('.output-key');
    const key = keyEl ? keyEl.value.trim() : '';
    if (!key) return;
    const labelEl = row.querySelector('.output-label');
    const typeEl = row.querySelector('.output-type');
    items.push({
      ref: '[responses.' + key + ']',
      label: labelEl ? labelEl.value.trim() : '',
      type: typeEl ? typeEl.value : ''
    });
  });
  if (items.length === 0) {
    box.innerHTML = '<div style="font-size:.72rem;color:var(--text-muted)">暂无可用变量：请先在下方「参数定义」或「返回值定义」中添加条目</div>';
    return;
  }
  const rows = items.map(it => {
    const meta = [it.label, it.type].filter(Boolean).join(' / ');
    return '<div class="cond-var-snippet" onclick="copyToClipboard(' + JSON.stringify(it.ref) + ')" title="点击复制" style="cursor:pointer;display:inline-flex;align-items:center;gap:6px;margin:0 6px 6px 0;padding:3px 8px;border:1px solid var(--border);border-radius:4px;font-family:monospace;font-size:.74rem;color:var(--accent);background:#f3f6fb">' +
      '<span>' + escHtml(it.ref) + '</span>' +
      (meta ? '<span style="color:var(--text-muted);font-size:.7rem;font-family:inherit">' + escHtml(meta) + '</span>' : '') +
      '</div>';
  }).join('');
  box.innerHTML = '<div style="font-size:.72rem;color:var(--text-muted);margin-bottom:6px">可用变量（点击复制）：</div>' +
    '<div style="display:flex;flex-wrap:wrap">' + rows + '</div>';
  bindCondVarSnippetWatch();
}

// 事件委托：监听参数/返回值容器的输入与增删，实时刷新「可用变量」列表。
// 只绑定一次（标记在容器上），避免每次渲染重复绑定。
function bindCondVarSnippetWatch() {
  ['node-params-container', 'node-outputs-container'].forEach(id => {
    const box = document.getElementById(id);
    if (!box || box.dataset.condVarBound === '1') return;
    box.dataset.condVarBound = '1';
    box.addEventListener('input', renderCondVarSnippets);
    // 增删行不触发 input，用 MutationObserver 覆盖
    const mo = new MutationObserver(() => renderCondVarSnippets());
    mo.observe(box, { childList: true, subtree: false });
  });
}

// 将 Activity 节点的「执行条件」输入框内容写入 Configuration（node_config.enable_condition）。
// 满足才执行该节点，否则跳过（不执行活动）。旧字段 node_condition 已完全废弃。
function syncEnableConfig() {
  if (document.getElementById('node-type').value !== 'custom/Activity') return;
  const ecEl = document.getElementById('node-enable-condition');
  if (!ecEl) return; // 当前页面无该输入框（如仅管理页未含此区块）则跳过
  let cfg;
  try { cfg = JSON.parse(document.getElementById('node-configuration').value || '{}'); } catch(e) { cfg = {}; }
  if (typeof cfg !== 'object' || cfg === null) cfg = {};
  if (!cfg.node_config || typeof cfg.node_config !== 'object') cfg.node_config = {};
  cfg.node_config.enable_condition = ecEl.value;
  // 旧字段 node_condition 已废弃：保存时直接丢弃，统一使用 enable_condition
  if (cfg.node_config.node_condition !== undefined) {
    delete cfg.node_config.node_condition;
  }
  // 保证 CommConfiguration 各字段存在
  if (typeof cfg.arg_mapping === 'undefined') cfg.arg_mapping = {};
  if (typeof cfg.ret_mapping === 'undefined') cfg.ret_mapping = {};
  if (typeof cfg.arguments === 'undefined') cfg.arguments = [];
  if (typeof cfg.responses === 'undefined') cfg.responses = {};
  document.getElementById('node-configuration').value = prettyJson(cfg);
}

// 收集所有阶段（二维数组）：外层为串行阶段，内层为该阶段并行执行的 activity 列表。
// 每个元素：{ act_namespace, act_name, id, name, args }
function collectStages() {
  const stages = [];
  document.querySelectorAll('#node-activity-stages .stage').forEach(stageEl => {
    const acts = [];
    stageEl.querySelectorAll('.act-item').forEach(row => {
      let argTpl = null, retTpl = null;
      try { const v = row.getAttribute('data-arg-template'); if (v && v !== '{}') argTpl = JSON.parse(v); } catch (e) {}
      try { const v = row.getAttribute('data-ret-template'); if (v && v !== '{}') retTpl = JSON.parse(v); } catch (e) {}
      acts.push({
        act_namespace: row.getAttribute('data-ns') || '',
        act_name: row.getAttribute('data-name') || '',
        id: row.getAttribute('data-id') || '',
        name: row.getAttribute('data-display-name') || (row.getAttribute('data-ns') + '/' + row.getAttribute('data-name')),
        args: collectActivityArgBinds(row),
        arg_template: argTpl,
        ret_template: retTpl
      });
    });
    stages.push(acts);
  });
  return stages;
}

// 收集全部已选 activity（跨所有阶段，扁平化），用于去重下拉等。
function collectAllActivities() {
  const all = [];
  collectStages().forEach(stage => stage.forEach(it => all.push(it)));
  return all;
}

// 从 node 配置解析出二维 stages 结构，兼容历史格式：
//   1) node_config.activities（新二维数组，直接返回）
//   2) node_config.stages（旧二维数组，直接返回）
//   3) activities（旧扁平数组 + mode）：连续 serial 起新 stage，其后的 parallel 并入同 stage
//   4) node_config.act_namespace + act_name（旧单 activity）
function parseStagesFromConfig(nodeCfg, nc) {
  nc = nc || {};
  if (nodeCfg && Array.isArray(nodeCfg.node_config && nodeCfg.node_config.activities)) {
    const raw = nodeCfg.node_config.activities;
    if (Array.isArray(raw) && raw.length > 0) {
      return raw.map(stage => (Array.isArray(stage) ? stage : []).map(it => ({
        act_namespace: it.act_namespace || '',
        act_name: it.act_name || '',
        id: it.id || '',
        name: it.name || (it.act_namespace + '/' + it.act_name),
        args: it.args || (it.arguments ? convertArgumentsToArgs(it.arguments) : {}),
        arg_template: it.arg_template || null,
        ret_template: it.ret_template || null
      })));
    }
  }
  if (nodeCfg && Array.isArray(nodeCfg.node_config && nodeCfg.node_config.stages)) {
    const raw = nodeCfg.node_config.stages;
    if (Array.isArray(raw) && raw.length > 0) {
      return raw.map(stage => (Array.isArray(stage) ? stage : []).map(it => ({
        act_namespace: it.act_namespace || '',
        act_name: it.act_name || '',
        id: it.id || '',
        name: it.name || (it.act_namespace + '/' + it.act_name),
        args: it.args || (it.arguments ? convertArgumentsToArgs(it.arguments) : {}),
        arg_template: it.arg_template || null,
        ret_template: it.ret_template || null
      })));
    }
  }
  if (nodeCfg && Array.isArray(nodeCfg.activities) && nodeCfg.activities.length > 0) {
    const stages = [];
    let cur = null;
    nodeCfg.activities.forEach(it => {
      const mode = it.mode === 'parallel' ? 'parallel' : 'serial';
      const item = {
        act_namespace: it.act_namespace || '',
        act_name: it.act_name || '',
        id: it.id || '',
        name: it.name || (it.act_namespace + '/' + it.act_name),
        args: it.args || (it.arguments ? convertArgumentsToArgs(it.arguments) : {}),
        arg_template: it.arg_template || null,
        ret_template: it.ret_template || null
      };
      if (mode === 'serial' || cur === null) {
        cur = [item];
        stages.push(cur);
      } else {
        cur.push(item);
      }
    });
    return stages;
  }
  if (nc.act_namespace && nc.act_name) {
    return [[{ act_namespace: nc.act_namespace, act_name: nc.act_name, id: '', name: '', args: {} }]];
  }
  return [];
}

// 将后端 arguments 数组（[{key,value}]）转换为前端 args 绑定结构（{key:{source,value,ref}}）
function convertArgumentsToArgs(argumentsArr) {
  const args = {};
  if (!Array.isArray(argumentsArr)) return args;
  argumentsArr.forEach(a => {
    const k = a.key || '';
    if (!k) return;
    const src = a.source || 'value';
    const val = a.value !== undefined ? String(a.value) : '';
    const ref = a.ref !== undefined && a.ref !== null && a.ref !== '' ? a.ref : val;
    // 保留 type：固定配置的类型（string/int64/slice/...）在 arguments 数组形态下也必须带回，
    // 否则重新打开节点时 arg-type 下拉会回退到默认 string。
    const typ = a.type || 'string';
    args[k] = { source: src, value: val, ref: ref, type: typ };
  });
  return args;
}

// 新建一个串行阶段（stage），可传入初始 activity 列表（用于回显）。
// stageActs: [{ act_namespace, act_name, id, name, args }]
function addActivityStage(stageActs) {
  const root = document.getElementById('node-activity-stages');
  const stageIdx = root.querySelectorAll('.stage').length; // 0-based，用于展示序号
  const stage = document.createElement('div');
  stage.className = 'stage';
  stage.style.cssText = 'border:1px solid #bae6fd;border-left:4px solid #0ea5e9;border-radius:var(--radius);padding:10px;background:#f0f9ff';
  stage.innerHTML =
    '<div style="display:flex;align-items:center;gap:8px;margin-bottom:8px">' +
      '<span class="stage-index" style="display:inline-flex;align-items:center;justify-content:center;min-width:24px;height:24px;border-radius:6px;background:#ecfeff;color:#0e7490;font-size:.78rem;font-weight:700">S' + (stageIdx + 1) + '</span>' +
      '<span style="font-size:.8rem;color:var(--text-muted)">串行阶段（与上一阶段顺序执行，本阶段内 Activity 并行）</span>' +
      '<span style="margin-left:auto;display:inline-flex;gap:6px">' +
        '<button type="button" class="btn btn-sm btn-outline stage-move-up" onclick="moveActivityStage(this,-1)" title="上移阶段">↑</button>' +
        '<button type="button" class="btn btn-sm btn-outline stage-move-down" onclick="moveActivityStage(this,1)" title="下移阶段">↓</button>' +
        '<button type="button" class="btn btn-sm btn-outline" onclick="removeActivityStage(this)" title="删除此阶段">删除阶段</button>' +
      '</span>' +
    '</div>' +
    '<div class="stage-acts" style="display:flex;flex-direction:column;gap:8px;margin-bottom:8px"></div>' +
    '<div style="display:flex;align-items:center;gap:6px">' +
      '<div class="stage-act-combobox" style="flex:1;min-width:0;position:relative">' +
        '<input class="stage-act-filter" type="text" autocomplete="off" placeholder="+ 添加 Activity 到本阶段（并行）" ' +
          'onfocus="openStageCombobox(this)" oninput="filterStageCombobox(this)" ' +
          'style="width:100%;padding:5px 8px;font-size:.8rem;border:1px solid var(--border);border-radius:6px">' +
        '<ul class="stage-act-list" style="display:none;position:absolute;z-index:50;top:100%;left:0;right:0;max-height:220px;overflow:auto;margin:2px 0 0;padding:4px;list-style:none;background:#fff;border:1px solid var(--border);border-radius:6px;box-shadow:0 4px 12px rgba(0,0,0,.12)"></ul>' +
      '</div>' +
    '</div>';
  root.appendChild(stage);
  // 渲染该阶段内的活动
  const acts = Array.isArray(stageActs) ? stageActs : [];
  acts.forEach(it => addActivityItemRow(it, stage));
  refreshStageIndex();
  // 填充本阶段下拉（排除已选）
  populateStageSelect(stage);
  // 隐藏空提示与单 activity 旧入口
  document.getElementById('node-activity-empty').style.display = 'none';
  document.getElementById('node-activity-custom').style.display = 'none';
  refreshStageMoveButtons();
  syncActivityConfig();
}

// 删除一个阶段（连同其内 activity）
function removeActivityStage(btn) {
  const stage = btn.closest('.stage');
  if (stage) stage.remove();
  refreshStageIndex();
  if (collectStages().length === 0) {
    document.getElementById('node-activity-empty').style.display = '';
    document.getElementById('node-activity-custom').style.display = '';
  } else {
    // 阶段减少后重渲染所有引用下拉（前序集合变化）
    document.querySelectorAll('#node-activity-stages .act-item').forEach(r => renderActivityItemParams(r));
  }
  document.querySelectorAll('#node-activity-stages .stage').forEach(s => populateStageSelect(s));
  refreshStageMoveButtons();
  syncActivityConfig();
}

// 重新编号阶段序号
function refreshStageIndex() {
  document.querySelectorAll('#node-activity-stages .stage').forEach((stage, i) => {
    const idx = stage.querySelector('.stage-index');
    if (idx) idx.textContent = 'S' + (i + 1);
  });
}

// 刷新各阶段上移/下移按钮的可用状态：首阶段不能上移，末阶段不能下移
function refreshStageMoveButtons() {
  const stages = document.querySelectorAll('#node-activity-stages .stage');
  stages.forEach((s, i) => {
    const up = s.querySelector('.stage-move-up');
    const down = s.querySelector('.stage-move-down');
    if (up) up.disabled = (i === 0);
    if (down) down.disabled = (i === stages.length - 1);
  });
}

// 整体移动一个串行阶段的顺序（dir<0 上移，dir>0 下移）。
// 仅移动阶段本身，不影响阶段内并行 Activity 的排列。
function moveActivityStage(btn, dir) {
  const stage = btn.closest('.stage');
  const root = document.getElementById('node-activity-stages');
  if (!stage || !root) return;
  const stages = Array.from(root.querySelectorAll('.stage'));
  const idx = stages.indexOf(stage);
  const target = idx + dir;
  // 边界：第一个阶段不能上移（target<0），最后一个阶段不能下移（target>=length）
  if (target < 0 || target >= stages.length) return;
  if (dir < 0) {
    // 上移：插入到目标阶段之前
    root.insertBefore(stage, stages[target]);
  } else {
    // 下移：插入到目标阶段之后（即目标阶段的下一个兄弟节点之前）
    root.insertBefore(stage, stages[target].nextElementSibling);
  }
  refreshStageIndex();
  refreshStageMoveButtons();
  // 阶段顺序变化会影响「前序阶段 Activity」引用集合，重渲染参数下拉与可选项
  document.querySelectorAll('#node-activity-stages .act-item').forEach(r => renderActivityItemParams(r));
  document.querySelectorAll('#node-activity-stages .stage').forEach(s => populateStageSelect(s));
  syncActivityConfig();
}

// 填充某个阶段内的"添加 Activity"可过滤下拉（允许同一 activity 重复添加，不做去重禁用）
function populateStageSelect(stage) {
  const cb = stage.querySelector('.stage-act-combobox');
  if (!cb) return;
  const list = cb.querySelector('.stage-act-list');
  const input = cb.querySelector('.stage-act-filter');
  if (!list) return;
  const acts = window._activityCache || [];
  if (acts.length === 0) {
    list.innerHTML = '<li style="padding:6px 8px;color:var(--text-muted);font-size:.78rem">无可用 Activity</li>';
  } else {
    list.innerHTML = acts.map(a => {
      const val = a.act_namespace + '|' + a.act_name;
      const label = a.name + ' (' + a.act_namespace + '/' + a.act_name + ')';
      return '<li class="stage-act-opt" data-ns="' + escAttr(a.act_namespace || '') + '" data-name="' + escAttr(a.act_name || '') +
        '" data-val="' + escAttr(val) + '" data-text="' + escAttr(label) + '" ' +
        'style="padding:6px 8px;font-size:.78rem;cursor:pointer;border-radius:4px" onclick="pickStageActivity(this)">' + escHtml(label) + '</li>';
    }).join('');
  }
  if (input) input.value = '';
  list.style.display = 'none';
}

// 打开可过滤下拉（聚焦时），确保用最新缓存填充
function openStageCombobox(input) {
  const cb = input.closest('.stage-act-combobox');
  if (!cb) return;
  const stage = cb.closest('.stage');
  if (stage) populateStageSelect(stage);
  const list = cb.querySelector('.stage-act-list');
  if (list) { filterStageCombobox(input); list.style.display = ''; }
}

// 输入过滤：按文本包含（不区分大小写）实时筛选选项
function filterStageCombobox(input) {
  const cb = input.closest('.stage-act-combobox');
  if (!cb) return;
  const list = cb.querySelector('.stage-act-list');
  if (!list) return;
  const kw = (input.value || '').trim().toLowerCase();
  let visible = 0;
  list.querySelectorAll('.stage-act-opt').forEach(li => {
    const txt = (li.getAttribute('data-text') || '').toLowerCase();
    const hit = !kw || txt.indexOf(kw) >= 0;
    li.style.display = hit ? '' : 'none';
    if (hit) visible++;
  });
  let emptyEl = list.querySelector('.stage-act-empty');
  if (visible === 0) {
    if (!emptyEl) {
      emptyEl = document.createElement('li');
      emptyEl.className = 'stage-act-empty';
      emptyEl.style.cssText = 'padding:6px 8px;color:var(--text-muted);font-size:.78rem';
      emptyEl.textContent = '无匹配 Activity';
      list.appendChild(emptyEl);
    }
  } else if (emptyEl) {
    emptyEl.remove();
  }
}

// 点击选项：按 ns/name 追加到该阶段（并行），支持同一 activity 重复添加
function pickStageActivity(li) {
  const cb = li.closest('.stage-act-combobox');
  const stage = cb ? cb.closest('.stage') : null;
  if (!stage) return;
  const ns = li.getAttribute('data-ns') || '';
  const name = li.getAttribute('data-name') || '';
  const act = (window._activityCache || []).find(a => a.act_namespace === ns && a.act_name === name);
  const dispName = act ? act.name : (ns + '/' + name);
  addActivityItemRow({ act_namespace: ns, act_name: name, id: '', name: dispName, args: {} }, stage);
  // 关闭并清空输入框；同一 activity 可重复添加，无需去重，仅重填所有阶段下拉
  if (cb) {
    const input = cb.querySelector('.stage-act-filter');
    const list = cb.querySelector('.stage-act-list');
    if (input) input.value = '';
    if (list) list.style.display = 'none';
  }
  document.querySelectorAll('#node-activity-stages .stage').forEach(s => populateStageSelect(s));
  syncActivityConfig();
}

// 点击页面其它区域时关闭所有打开的阶段下拉
document.addEventListener('click', function(e) {
  if (!e.target.closest || !e.target.closest('.stage-act-combobox')) {
    document.querySelectorAll('#node-activity-stages .stage-act-list').forEach(l => { l.style.display = 'none'; });
  }
});

// 生成 5 位 base36 随机串（小写字母+数字），用于实例 ID 后缀
function randomSuffix5() {
  return Math.random().toString(36).slice(2, 7);
}

// 生成全局唯一的 activity 实例 id（当未显式指定时）。
// 同一 activity 可在 node 内重复添加多次（参数不同效果不同），必须有唯一 id 作为后端 stepId/引用标识。
// 格式与 node 实例一致：<activity_id>__<5位随机串>，如 A000015__sfdfd。
// 用 "__" 双下划线分隔（"-" 在变量引用路径中不可用）；前缀优先取 activity 模板 id（形如 A000015，简短好记），
// 查不到模板 id 时回退到 act_name。
function genUniqueActivityId(ns, name) {
  let base = (name && String(name).trim()) ? name.trim() : ((ns && String(ns).trim()) ? ns.trim() : 'act');
  const act = (window._activityCache || []).find(a => a.act_namespace === (ns || '') && a.act_name === (name || ''));
  if (act && act.activity_id) base = act.activity_id;
  const exist = new Set(collectAllActivities().map(it => String(it.id || '').trim()).filter(Boolean));
  let cand;
  let guard = 0;
  do {
    cand = base + '__' + randomSuffix5();
    guard++;
  } while (exist.has(cand) && guard < 100);
  return cand;
}

// 在每个阶段内追加一行已选 activity（并行卡片）
function addActivityItemRow(item, stage) {
  const list = stage.querySelector('.stage-acts');
  if (!list) return;
  // 未指定 id 时生成唯一实例 id（允许同一 activity 重复添加）
  const instId = (item.id && String(item.id).trim()) ? item.id : genUniqueActivityId(item.act_namespace || '', item.act_name || '');
  // 解析中文显示名：优先用传入 name；为空时尝试从 activity 缓存按 ns/name 匹配真实中文名；再回退到 ns/act_name
  const cacheAct = (window._activityCache || []).find(a => a.act_namespace === (item.act_namespace || '') && a.act_name === (item.act_name || ''));
  const dispName = (item.name && String(item.name).trim()) ? item.name
    : (cacheAct && cacheAct.name ? cacheAct.name : ((item.act_namespace || '') + '/' + (item.act_name || '')));
  const row = document.createElement('div');
  row.className = 'act-item';
  row.setAttribute('data-ns', item.act_namespace || '');
  row.setAttribute('data-name', item.act_name || '');
  row.setAttribute('data-id', instId || '');
  row.setAttribute('data-display-name', dispName);
  try { row.setAttribute('data-args', JSON.stringify(item.args || {})); } catch(e) { row.setAttribute('data-args', '{}'); }
  // 携带该 activity 的 arg_template / ret_template（节点执行时用于参数解析）。
  // 若 item 未带（新增时），则从 activity 缓存取活动定义模板填充，避免保存时被丢弃。
  let argTpl = item.arg_template;
  let retTpl = item.ret_template;
  if (cacheAct) {
    if (!argTpl && cacheAct.arg_template) {
      try { argTpl = typeof cacheAct.arg_template === 'string' ? JSON.parse(cacheAct.arg_template) : cacheAct.arg_template; } catch (e) { argTpl = null; }
    }
    if (!retTpl && cacheAct.return_values) {
      try { retTpl = typeof cacheAct.return_values === 'string' ? JSON.parse(cacheAct.return_values) : cacheAct.return_values; } catch (e) { retTpl = null; }
    }
  }
  try { row.setAttribute('data-arg-template', JSON.stringify(argTpl || {})); } catch (e) { row.setAttribute('data-arg-template', '{}'); }
  try { row.setAttribute('data-ret-template', JSON.stringify(retTpl || {})); } catch (e) { row.setAttribute('data-ret-template', '{}'); }
  row.style.cssText = 'padding:8px 10px;border:1px solid #e2e8f0;border-left:4px solid #94a3b8;border-radius:var(--radius);background:#ffffff';
  const title = dispName ? (dispName + '  ') : '';
  // 该 activity 配置的输出返回值（执行后会返回的值），从缓存同步展示，方便配置人理解
  let rvHtml = '<span style="color:var(--text-muted);font-size:.76rem">无 ReturnValues 配置</span>';
  let rvList = [];
  try { rvList = cacheAct && cacheAct.return_values ? (typeof cacheAct.return_values === 'string' ? JSON.parse(cacheAct.return_values) : cacheAct.return_values) : []; } catch(e) { rvList = []; }
  if (Array.isArray(rvList) && rvList.length > 0) {
    rvHtml = rvList.map(rv => {
      // 字段名 = name（引用路径字段名）；中文名 = label（Activity 修改后的名字）；类型 = type
      const fieldName = escHtml(rv.name || '');
      const cnName = escHtml(rv.label || '');
      const namePart = cnName && cnName !== fieldName ? (fieldName + ': ' + cnName) : fieldName;
      const tp = rv.type ? (' <span style="color:var(--text-muted)">(' + escHtml(rv.type) + ')</span>') : '';
      return '<span style="display:inline-block;margin:2px 6px 2px 0;padding:1px 7px;border:1px solid var(--border);border-radius:10px;background:#eef6ff;color:#0e7490;font-family:monospace;font-size:.72rem">' + namePart + tp + '</span>';
    }).join('');
  }
  row.innerHTML =
    '<div style="display:flex;align-items:center;gap:8px">' +
      '<span class="act-item-name" style="flex:1;font-weight:600;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + escHtml(title) + '<span style="color:var(--text-muted);font-weight:400">(' + escHtml(item.act_namespace) + '/' + escHtml(item.act_name) + ')</span></span>' +
      '<button type="button" class="btn btn-sm btn-outline" onclick="toggleActivityParams(this)" title="展开/收起参数">参数</button>' +
      '<button type="button" class="btn btn-sm btn-outline" onclick="removeActivityItem(this)" title="移除">移除</button>' +
    '</div>' +
    '<div style="margin-top:4px;font-size:.74rem;color:#0e7490;font-family:monospace;word-break:break-all" title="实例 ID（用于参数引用与后端执行 stepId）">实例ID: ' + escHtml(instId) + '</div>' +
    '<div style="margin-top:6px">' +
      '<div style="font-size:.72rem;color:var(--text-muted);margin-bottom:2px">返回值 (ReturnValues)：执行后输出</div>' +
      '<div>' + rvHtml + '</div>' +
    '</div>' +
    '<div class="act-sample-box" data-ns="' + escAttr(item.act_namespace || '') + '" data-name="' + escAttr(item.act_name || '') + '" style="margin-top:6px;display:none"></div>' +
    '<div class="act-params-box" style="display:none;margin-top:8px;border-top:1px dashed var(--border);padding-top:8px"></div>';
  list.appendChild(row);
  renderActivityItemParams(row);
  // 默认展开参数明细，便于直接进行参数映射
  const box = row.querySelector('.act-params-box');
  if (box) box.style.display = '';
  // 缓存已就绪时直接加载最近成功测试示例；否则交给 openNodeModal 的 afterCache 统一补
  if (window._activityCache && window._activityCache.length > 0) {
    loadActivitySample(item.act_namespace || '', item.act_name || '', row.querySelector('.act-sample-box'));
  }
}

// 加载并展示某个 activity 最近一次成功测试的结果示例，帮助配置人理解实际返回结构。
// 通过 ns/act_name 从缓存反查 activity_id，再请求测试记录接口，取第一条 status=success 的 result 渲染。
async function loadActivitySample(ns, name, boxEl) {
  if (!boxEl) return;
  const act = (window._activityCache || []).find(a => a.act_namespace === ns && a.act_name === name);
  const activityId = act ? act.activity_id : '';
  if (!activityId) { boxEl.style.display = 'none'; return; }
  try {
    const p = getProject();
    if (!p) { boxEl.style.display = 'none'; return; }
    const records = await api('/api/activities/' + encodeURIComponent(activityId) + '/test-records?project=' + encodeURIComponent(p));
    let latest = null;
    for (const r of (records || [])) {
      if (r.status === 'success') { latest = r; break; }
    }
    if (!latest) { boxEl.style.display = 'none'; return; }
    let obj = latest.result;
    if (typeof obj === 'string') { try { obj = JSON.parse(obj); } catch (e) { obj = null; } }
    let preview = '';
    if (obj && typeof obj === 'object') {
      preview = JSON.stringify(obj, null, 2);
    } else if (obj !== null && obj !== undefined) {
      preview = String(obj);
    }
    if (!preview) { boxEl.style.display = 'none'; return; }
    boxEl.innerHTML =
      '<div style="font-size:.72rem;color:var(--text-muted);margin-bottom:2px">最近一次成功测试返回示例</div>' +
      '<pre style="margin:0;max-height:160px;overflow:auto;background:#0f172a;color:#e2e8f0;padding:8px 10px;border-radius:6px;font-size:.72rem;line-height:1.4;white-space:pre-wrap;word-break:break-all">' + escHtml(preview) + '</pre>';
    boxEl.style.display = '';
  } catch (e) {
    boxEl.style.display = 'none';
  }
}

// 展开/收起当前 activity 的参数明细
function toggleActivityParams(btn) {
  const row = btn.closest('.act-item');
  if (!row) return;
  const box = row.querySelector('.act-params-box');
  if (!box) return;
  if (box.style.display === 'none') {
    renderActivityItemParams(row);
    box.style.display = '';
  } else {
    box.style.display = 'none';
  }
}

// 收集当前 activity item 内所有参数的绑定（来自 .arg-bind-row）
// bind 结构：{ source: 'value'|'ref_act'|'ref_node', value, ref }
function collectActivityArgBinds(row) {
  const binds = {};
  row.querySelectorAll('.arg-bind-row').forEach(r => {
    const key = r.getAttribute('data-arg-key') || '';
    if (!key) return;
    const source = r.querySelector('.arg-src') ? r.querySelector('.arg-src').value : 'value';
    let value = '';
    let ref = '';
    let typ = '';
    if (source === 'value') {
      value = r.querySelector('.arg-val') ? r.querySelector('.arg-val').value : '';
      const typEl = r.querySelector('.arg-type');
      typ = typEl ? typEl.value : 'string';
    } else {
      const finalEl = r.querySelector('.arg-ref-final');
      if (finalEl) {
        ref = finalEl.value || ''; // 新三级联动：此处已是拼装好的最终路径
      } else {
        ref = r.querySelector('.arg-ref') ? r.querySelector('.arg-ref').value : '';
      }
      value = ref; // 引用模式最终值即引用路径字符串
      // 引用模式：type 取「当前这个 Activity 里该参数所定义的 type」（不是被引用对象的类型）。
      // 即：用绑定 key 去当前 Activity 的参数定义里查 type；找不到定义或未配置则留空，最终回退 string。
      const curNs = row.getAttribute('data-ns') || '';
      const curName = row.getAttribute('data-name') || '';
      const curAct = (window._activityCache || []).find(a => a.act_namespace === curNs && a.act_name === curName);
      let curParams = [];
      if (curAct && curAct.arguments) {
        try { curParams = (typeof curAct.arguments === 'string') ? JSON.parse(curAct.arguments || '[]') : (curAct.arguments || []); } catch(e) { curParams = []; }
      }
      const curParam = Array.isArray(curParams) ? curParams.find(x => (x.key || '') === key) : null;
      typ = curParam && (curParam.type || curParam.value_type) ? (curParam.type || curParam.value_type) : '';
    }
    binds[key] = { source: source, value: value, ref: ref, type: typ };
  });
  return binds;
}

// 计算「可引用」的前序 activity 列表。
// 关键约束：只有「串行排在前面」的 stage 中的 activity 才能被引用——
// 同 stage 内的 activity 是并行同时执行的，彼此无法保证先后，故不能互相引用。
// 返回 [{ id, act_name, ns, params:[{key,label}] }]，均为当前 row 所属 stage 之前的所有 stage 的 activity。
function prevActivitiesBefore(row) {
  const stages = Array.from(document.querySelectorAll('#node-activity-stages .stage'));
  const myStage = row.closest('.stage');
  const myIdx = stages.indexOf(myStage);
  const prevs = [];
  stages.forEach((stage, si) => {
    if (si >= myIdx) return; // 仅取「前面」的 stage，跳过当前及之后的（含同 stage 并行兄弟）
    stage.querySelectorAll('.act-item').forEach(prev => {
      prevs.push(prev);
    });
  });
  return prevs.map(prev => {
    const ns = prev.getAttribute('data-ns') || '';
    const name = prev.getAttribute('data-name') || '';
    const id = prev.getAttribute('data-id') || name; // 无显式 id 时用 act_name 作为引用标识
    const act = (window._activityCache || []).find(a => a.act_namespace === ns && a.act_name === name);
    let params = [];
    if (act && act.arguments) {
      try { params = (typeof act.arguments === 'string') ? JSON.parse(act.arguments || '[]') : (act.arguments || []); } catch(e) { params = []; }
    }
    // 该前序 activity 的 ReturnValues 列表（每项 {name, key, type}），用于「引用前序」时直接选择返回值字段
    let returnValues = [];
    if (act && act.return_values) {
      try { returnValues = (typeof act.return_values === 'string') ? JSON.parse(act.return_values || '[]') : (act.return_values || []); } catch(e) { returnValues = []; }
    }
    return { id: id, act_name: name, ns: ns, params: Array.isArray(params) ? params : [], returnValues: Array.isArray(returnValues) ? returnValues : [] };
  });
}

// 收集本节点参数定义（来自节点参数表单），用于"引用节点参数"下拉
function nodeParamDefs() {
  const defs = [];
  document.querySelectorAll('#node-params-container .param-row').forEach(r => {
    const key = r.querySelector('.param-key') ? r.querySelector('.param-key').value.trim() : '';
    const label = r.querySelector('.param-label') ? r.querySelector('.param-label').value.trim() : '';
    const type = r.querySelector('.param-type') ? r.querySelector('.param-type').value : '';
    if (key) defs.push({ key: key, label: label, type: type });
  });
  return defs;
}

// 渲染某个 activity item 的参数明细 + 可编辑绑定输入
// 绑定来源三选：
//   1) value      手工配置（固定值）
//   2) ref_act    引用前面某 Activity 的返回值（路径形如 {{steps.id.responses}} 或 {{steps.id.responses.field}}，所有 Activity 都在 steps 下）
//   3) ref_node   引用本节点的参数定义（路径形如 {{node_param_key}}）
function renderActivityItemParams(row) {
  const box = row.querySelector('.act-params-box');
  if (!box) return;
  const ns = row.getAttribute('data-ns') || '';
  const name = row.getAttribute('data-name') || '';
  let existingBinds = {};
  try { existingBinds = JSON.parse(row.getAttribute('data-args') || '{}') || {}; } catch(e) { existingBinds = {}; }
  const act = (window._activityCache || []).find(a => a.act_namespace === ns && a.act_name === name);
  let params = [];
  if (act && act.arguments) {
    try {
      params = (typeof act.arguments === 'string') ? JSON.parse(act.arguments || '[]') : (act.arguments || []);
    } catch (e) { params = []; }
  }

  // 信息明细表
  const infoRows = params.map(p => {
    const key = escHtml(p.key || '-');
    const label = escHtml(p.label || '');
    const type = escHtml(p.type || (p.value_type || '-'));
    const req = p.required ? '<span style="color:#dc2626">必填</span>' : '<span style="color:var(--text-muted)">可选</span>';
    const def = (p.value !== undefined && p.value !== null && p.value !== '') ? escHtml(String(p.value))
              : (p.default_value !== undefined && p.default_value !== null && p.default_value !== '') ? escHtml(String(p.default_value)) : '-';
    const desc = escHtml(p.description || '');
    const policy = escHtml(p.policy || '-');
    return '<tr>' +
      '<td style="font-family:monospace;font-weight:600;white-space:nowrap">' + key + '</td>' +
      '<td>' + label + '</td>' +
      '<td style="white-space:nowrap">' + type + '</td>' +
      '<td style="white-space:nowrap">' + req + '</td>' +
      '<td style="white-space:nowrap">' + def + '</td>' +
      '<td style="white-space:nowrap">' + policy + '</td>' +
      '<td style="color:var(--text-muted);max-width:220px">' + (desc || '-') + '</td>' +
    '</tr>';
  }).join('');
  const infoTable = params.length > 0
    ? '<div style="font-size:.78rem;color:var(--text-muted);margin-bottom:6px">参数明细</div>' +
      '<table style="width:100%;border-collapse:collapse;font-size:.8rem">' +
        '<thead><tr style="background:#f3f4f6">' +
          '<th style="padding:6px 8px;text-align:left">参数 Key</th>' +
          '<th style="padding:6px 8px;text-align:left">标签</th>' +
          '<th style="padding:6px 8px;text-align:left">类型</th>' +
          '<th style="padding:6px 8px;text-align:left">必填</th>' +
          '<th style="padding:6px 8px;text-align:left">默认值</th>' +
          '<th style="padding:6px 8px;text-align:left">策略</th>' +
          '<th style="padding:6px 8px;text-align:left">描述</th>' +
        '</tr></thead>' +
        '<tbody>' + infoRows + '</tbody>' +
      '</table>'
    : '<div style="font-size:.8rem;color:var(--text-muted);padding:4px 0">该 Activity 无参数定义（无可映射字段）</div>';

  // 引用来源下拉选项
  const prevs = prevActivitiesBefore(row);
  const nodeParams = nodeParamDefs();
  // 本节点参数下拉：直接列出参数定义中的 key，生成 {{arguments.key}}
  const nodeRefOptions = nodeParams.map(p =>
    '<option value="' + escAttr('{{arguments.' + p.key + '}}') + '">' + escHtml((p.label || p.key) + ' (' + p.key + ')') + '</option>'
  ).join('');

  // 前序 activity 的"参数值"key 列表（来自其 arguments 定义，前端有缓存），用于引用精确字段
  const prevArgKeysById = {};
  prevs.forEach(p => {
    const keys = (p.params || []).map(pp => pp.key || '').filter(Boolean);
    prevArgKeysById[p.id] = keys;
  });

  // 根据已存引用路径 b.ref 反推联动控件初值：{ refId, refType, refField, raw }
  function argParseRefPath(ref) {
    const m = /^\\{\\{([^}]+)\\}\\}$/.exec((ref || '').trim());
    if (!m) return { raw: ref || '' };
    const inner = m[1];
    const dot = inner.indexOf('.');
    if (dot < 0) return { refId: inner, refType: 'responses', refField: '' };
  const refId = inner.slice(0, dot);
  const rest = inner.slice(dot + 1);
  if (rest === 'responses') return { refId: refId, refType: 'responses', refField: '' };
  if (rest === 'arguments') return { refId: refId, refType: 'arguments', refField: '' };
  if (rest.startsWith('responses.')) return { refId: refId, refType: 'responses_field', refField: rest.slice('responses.'.length) };
  if (rest.startsWith('arguments.')) return { refId: refId, refType: 'arguments', refField: rest.slice('arguments.'.length) };
  return { refId: refId, refType: 'responses', refField: rest };
}
  // 根据联动控件拼出最终引用路径
  function argBuildRefPath(refId, refType, refField) {
    if (!refId) return '';
    const field = (refField || '').trim();
    let inner;
    if (refType === 'responses') inner = refId + '.responses';
    // 返回值.字段：field 为空（未选具体字段，或所选 return_value 的 key 为空=返回全部）一律回退为整体 {{id.responses}}
    else if (refType === 'responses_field') inner = refId + '.responses' + (field ? '.' + field : '');
    else if (refType === 'arguments') inner = refId + '.arguments.' + (field || 'key');
    else inner = refId;
    return '{{' + inner + '}}';
  }
  // 生成「引用前序」第三级字段选择下拉：
  //   - type=responses_field：列出前序 activity 的 ReturnValues（显示的字段名即引用路径里的 field，对应 ReturnValue.Name）
  //   - type=arguments：列出前序 activity 的 arguments key
  //   - type=responses：无字段（整体引用），返回空串
  function argRenderRefFieldSlot(prevs, prevId, type, selectedField) {
    if (type === 'responses') return '';
    const prev = (prevs || []).find(p => p.id === prevId);
    if (!prev) {
      return '<span style="flex:1;color:var(--text-muted);font-size:.78rem">请先选择前序 Activity</span>';
    }
    if (type === 'responses_field') {
      const rvs = prev.returnValues || [];
      if (rvs.length === 0) {
        return '<span style="flex:1;color:var(--text-muted);font-size:.78rem">该 Activity 无 ReturnValues 配置</span>';
      }
      const opts = rvs.map(rv => {
        const rvName = rv.name || '';
        const rvLabel = rv.label || rv.name || '';
        const rvKey = rv.key || '';
        // 中文显示优先用 label，引用路径 value 仍用 name（代码名）
        const label = rvLabel + (rvKey ? ' (' + rvKey + ')' : '');
        return '<option value="' + escAttr(rvName) + '"' + (rvName === selectedField ? ' selected' : '') + '>' + escHtml(label) + '</option>';
      }).join('');
      return '<select class="arg-refield" onchange="onRefChange(this)" style="flex:1 1 110px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
          '<option value="">— 选择返回值字段 —</option>' + opts + '</select>';
    }
    // type === 'arguments'
    const keys = (prev.params || []).map(pp => pp.key || '').filter(Boolean);
    if (keys.length === 0) {
      return '<span style="flex:1;color:var(--text-muted);font-size:.78rem">该 Activity 无参数定义</span>';
    }
    const opts = keys.map(k => '<option value="' + escAttr(k) + '"' + (k === selectedField ? ' selected' : '') + '>' + escHtml(k) + '</option>').join('');
    return '<select class="arg-refield" onchange="onRefChange(this)" style="flex:1 1 110px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
        '<option value="">— 选择参数 key —</option>' + opts + '</select>';
}
  // rebuildRefFieldSlot 已提升为全局函数（供 onRefChange 跨作用域调用）

  // 逐参数绑定行：调用全局可复用函数，便于来源切换时仅局部重建当前行，避免整卡重渲染串扰其它参数
  const bindRows = params.map(p => {
    const key = p.key || '';
    const bind = existingBinds[key] || {};
    const defVal = (p.value !== undefined && p.value !== null && p.value !== '') ? String(p.value)
                 : (p.default_value !== undefined && p.default_value !== null && p.default_value !== '') ? String(p.default_value) : '';
    bind._defVal = defVal;
    return argRenderArgBindRow(row, key, bind, { prevs: prevs, nodeParams: nodeParams, nodeRefOptions: nodeRefOptions });
  }).join('');

  box.innerHTML = infoTable +
    '<div style="font-size:.78rem;font-weight:600;color:var(--text);margin:10px 0 4px">参数值绑定</div>' +
    (params.length > 0
      ? '<div class="arg-bind-list" style="background:#f8fafc;border:1px solid var(--border);border-radius:8px;padding:6px 10px">' + bindRows + '</div>'
      : '');

  // 回显：反填联动控件初值
  row.querySelectorAll('.arg-bind-row').forEach(r => {
    const bkey = r.getAttribute('data-arg-key') || '';
    const b = existingBinds[bkey];
    if (!b || !b.ref) return;
    const wrap = r.querySelector('.arg-input-wrap');
    if (!wrap) return;
    const parsed = argParseRefPath(b.ref);
    if (Object.prototype.hasOwnProperty.call(parsed, 'raw')) {
      // 无法反推的裸路径：直接作为节点参数最终值
      const nodeSel = wrap.querySelector('.arg-ref-node');
      if (nodeSel) nodeSel.value = parsed.raw;
      return;
    }
    if (b.source === 'ref_node') {
      const nodeSel = wrap.querySelector('.arg-ref-node');
      if (nodeSel) nodeSel.value = b.ref;
      return;
    }
    const prevSel = wrap.querySelector('.arg-ref');
    const typeSel = wrap.querySelector('.arg-reftype');
    const fieldInput = wrap.querySelector('.arg-refield');
    if (prevSel && parsed.refId) prevSel.value = parsed.refId;
    if (typeSel && parsed.refType) typeSel.value = parsed.refType;
    if (fieldInput && parsed.refField) fieldInput.value = parsed.refField;
  });
}

// 来源切换：手工配置 / 引用前序Activity / 引用节点参数
// 关键：仅局部重建「当前参数行」的输入控件，不重渲染整张卡片，
// 保证同一 Activity 内多个参数各自独立配置、互不串扰。
function onArgSourceChange(sel) {
  const rowBind = sel.closest('.arg-bind-row');
  if (!rowBind) return;
  const wrap = rowBind.querySelector('.arg-input-wrap');
  if (!wrap) return;
  const actItem = sel.closest('.act-item');
  if (!actItem) return;
  const key = rowBind.getAttribute('data-arg-key') || '';
  const newSrc = sel.value;
  // 先把整卡所有参数行的实时值（来自 DOM）同步回 data-args，作为权威快照（不影响其它行 DOM）
  const liveBinds = collectActivityArgBinds(actItem);
  if (key) {
    const b = liveBinds[key] || {};
    b.source = newSrc;
    if (newSrc === 'value') { b.ref = ''; } // 切回手工配置时清空引用路径
    liveBinds[key] = b;
  }
  actItem.setAttribute('data-args', JSON.stringify(liveBinds));
  // 仅重建当前参数行的输入控件（局部重建），其它行的 DOM 与已选值完全保留
  const prevs = prevActivitiesBefore(actItem);
  const nodeParams = nodeParamDefs();
  const nodeRefOptions = nodeParams.map(p =>
    '<option value="' + escAttr('{{arguments.' + p.key + '}}') + '">' + escHtml((p.label || p.key) + ' (' + p.key + ')') + '</option>'
  ).join('');
  const bind = liveBinds[key] || {};
  wrap.innerHTML = argRenderArgInput(key, bind, { prevs: prevs, nodeParams: nodeParams, nodeRefOptions: nodeRefOptions });
  // 回填联动初值（引用前序/引用节点时）
  if (bind.ref) {
    const parsed = argParseRefPath(bind.ref);
    if (Object.prototype.hasOwnProperty.call(parsed, 'raw')) {
      const nodeSel = wrap.querySelector('.arg-ref-node');
      if (nodeSel) nodeSel.value = parsed.raw;
    } else if (bind.source === 'ref_node') {
      const nodeSel = wrap.querySelector('.arg-ref-node');
      if (nodeSel) nodeSel.value = bind.ref;
    } else {
      const prevSel = wrap.querySelector('.arg-ref');
      const typeSel = wrap.querySelector('.arg-reftype');
      const fieldInput = wrap.querySelector('.arg-refield');
      if (prevSel && parsed.refId) prevSel.value = parsed.refId;
      if (typeSel && parsed.refType) typeSel.value = parsed.refType;
      if (fieldInput && parsed.refField) fieldInput.value = parsed.refField;
    }
  }
  syncActivityConfig();
}

// 固定配置类型变化：即时同步进当前 activity 的 data-args 权威快照，避免后续整卡重渲染/保存时丢失
function onArgTypeChange(sel) {
  const rowBind = sel.closest('.arg-bind-row');
  if (!rowBind) return;
  const actItem = sel.closest('.act-item');
  if (!actItem) return;
  const key = rowBind.getAttribute('data-arg-key') || '';
  const liveBinds = collectActivityArgBinds(actItem);
  if (key) {
    const b = liveBinds[key] || {};
    b.type = sel.value;
    liveBinds[key] = b;
  }
  actItem.setAttribute('data-args', JSON.stringify(liveBinds));
}

// 引用下拉/输入变化：拼装最终引用路径写入同行的 .arg-ref-final，供 collect 读取
function onRefChange(sel) {
  const wrap = sel.closest('.arg-input-wrap');
  if (!wrap) return;
  let finalInput = wrap.querySelector('.arg-ref-final');
  if (!finalInput) return;
  const nodeSel = wrap.querySelector('.arg-ref-node');
  if (nodeSel) {
    // 引用节点参数：value 即最终路径 {{key}}
    finalInput.value = nodeSel.value || '';
  } else {
    // 引用前序：三级联动拼装
    const prevSel = wrap.querySelector('.arg-ref');
    const typeSel = wrap.querySelector('.arg-reftype');
    const slot = wrap.querySelector('.arg-ref-field-slot');
    // 若切换的是取值类型，重建第三级字段下拉（从 ReturnValues / arguments key 列表）
    if (sel === typeSel) {
      rebuildRefFieldSlot(wrap);
    }
    // 切换前序 Activity 后：整行输入控件随新前序重建（取值类型选项、第三级字段均按新前序动态生成），
    // 并回填当前已选的前序 Activity，保证其余状态不丢、不串扰。
    if (sel === prevSel) {
      const rowBind = sel.closest('.arg-bind-row');
      const key = rowBind ? (rowBind.getAttribute('data-arg-key') || '') : '';
      const prevs = argGetPrevsForWrap(wrap);
      const nodeParams = nodeParamDefs();
      const nodeRefOptions = nodeParams.map(p =>
        '<option value="' + escAttr('{{arguments.' + p.key + '}}') + '">' + escHtml((p.label || p.key) + ' (' + p.key + ')') + '</option>'
      ).join('');
      const bind = { source: 'ref_act', ref: '{{steps.' + prevSel.value + '.responses}}' };
      wrap.innerHTML = argRenderArgInput(key, bind, { prevs: prevs, nodeParams: nodeParams, nodeRefOptions: nodeRefOptions });
      const newPrevSel = wrap.querySelector('.arg-ref');
      if (newPrevSel && prevSel.value) newPrevSel.value = prevSel.value;
      finalInput = wrap.querySelector('.arg-ref-final'); // wrap 已重建，需重新获取最终值输入节点
    }
    const fieldSel = wrap.querySelector('.arg-refield');
    const refId = prevSel ? prevSel.value : '';
    const typeSelNow = wrap.querySelector('.arg-reftype');
    const refType = typeSelNow ? typeSelNow.value : 'responses';
    const refField = fieldSel ? fieldSel.value : '';
    if (finalInput) finalInput.value = argBuildRefPath(refId, refType, refField);
  }
  syncActivityConfig();
}

// 移除一行 activity
function removeActivityItem(btn) {
  const row = btn.closest('.act-item');
  if (row) row.remove();
  if (collectAllActivities().length === 0) {
    document.getElementById('node-activity-empty').style.display = '';
    document.getElementById('node-activity-custom').style.display = '';
  } else {
    // 移除后前序活动集合变化，重渲染剩余 item 的引用下拉
    document.querySelectorAll('#node-activity-stages .act-item').forEach(r => renderActivityItemParams(r));
  }
  document.querySelectorAll('#node-activity-stages .stage').forEach(s => populateStageSelect(s));
  syncActivityConfig();
}

// 将二维 activities 编排同步进 Configuration.node_config.activities
// 每个 stage 为数组，元素结构：{ act_namespace, act_name, id, arguments:[{key,value}] }
function syncActivityConfig() {
  if (document.getElementById('node-type').value !== 'custom/Activity') return;
  // 把每个 activity 当前 DOM 中的参数绑定实时值同步回 data-args，
  // 作为后续整卡重渲染（来源切换/移除/缓存加载）的权威快照，避免多行之间互相覆盖
  document.querySelectorAll('#node-activity-stages .act-item').forEach(row => {
    try { row.setAttribute('data-args', JSON.stringify(collectActivityArgBinds(row))); } catch (e) {}
  });
  let cfg;
  try {
    cfg = JSON.parse(document.getElementById('node-configuration').value || '{}');
  } catch (e) {
    cfg = {};
  }
  if (typeof cfg !== 'object' || cfg === null) cfg = {};
  if (!cfg.node_config || typeof cfg.node_config !== 'object') cfg.node_config = {};

  const stages = collectStages();
  if (stages.length > 0 && stages.some(s => s.length > 0)) {
    // 二维数组写入 node_config.activities（与后端 ActivityNode.Init 的 cfgActs.Activities 结构一致）
    cfg.node_config.activities = stages.map(stage => stage.map(it => {
      const args = it.args || {};
        const argumentsArr = Object.keys(args).map(k => {
          const b = args[k] || {};
          return {
            key: k,
            source: b.source || 'value',
            value: b.value !== undefined ? b.value : (b.ref || ''),
            ref: b.ref || '',
            type: b.type || 'string'
          };
        });
      const entry = {
        act_namespace: it.act_namespace,
        act_name: it.act_name,
        id: it.id || it.act_name,
        arguments: argumentsArr
      };
      // 保留每个 activity 的 arg_template / ret_template（节点执行时用于参数解析），避免保存时被丢弃
      if (it.arg_template && Object.keys(it.arg_template).length > 0) entry.arg_template = it.arg_template;
      if (it.ret_template && Object.keys(it.ret_template).length > 0) entry.ret_template = it.ret_template;
      return entry;
    }));
    delete cfg.node_config.stages; // 废弃旧的 stages 结构
    delete cfg.activities; // 废弃旧的扁平 activities 结构
    delete cfg.node_config.activity_type;
    delete cfg.node_config.act_namespace; // 不再需要单 activity 回退字段
    delete cfg.node_config.act_name;
  } else {
    // 无阶段
    delete cfg.node_config.stages;
    delete cfg.activities;
    delete cfg.node_config.act_namespace;
    delete cfg.node_config.act_name;
  }
  document.getElementById('node-configuration').value = prettyJson(cfg);
}

async function saveNode() {
  const isEdit = document.getElementById('node-is-edit').value === '1';
  const body = {
    node_id: document.getElementById('node-node-id').value.trim(),
    name: document.getElementById('node-name').value.trim(),
    type: document.getElementById('node-type').value,
    kind: document.getElementById('node-kind').value,
    category: document.getElementById('node-category').value.trim(),
    tags: collectNodeTags(),
    version: document.getElementById('node-version').value.trim(),
    status: parseInt(document.getElementById('node-status').value),
    description: document.getElementById('node-description').value.trim(),
    namespace: document.getElementById('node-namespace').value.trim(),
  };
  // Parse JSON fields
  try {
    body.configuration = JSON.parse(document.getElementById('node-configuration').value || '{}');
    body.additional_info = JSON.parse(document.getElementById('node-additional-info').value || '{}');
    body.params = collectParams();
    body.outputs = collectOutputs();
  } catch (e) { showToast('JSON 格式错误: ' + e.message, 'error'); return; }
  // 将「节点参数配置」「返回值定义」同步写入 Configuration 第一层，供节点执行时调用：
  //   params  -> configuration.arguments（param.BindConfig 数组：[{ key, value, policy }]）
  //   outputs -> configuration.responses（返回值定义数组：[{ key, label, type, source, value|ref }]）
  syncParamsToConfigArguments(body);
  // 引用节点参数（ref_node）校验：本节点尚未定义任何参数时，若存在 ref_node 绑定则拦截保存，
  // 避免用户遗漏参数配置（界面此时已提示"本节点未定义参数，无法引用"）。
  if (nodeParamDefs().length === 0) {
    let hasNodeRef = false;
    if (body.type === 'custom/Activity') {
      const stages = collectStages();
      stages.forEach(s => s.forEach(it => {
        const args = it.args || {};
        Object.keys(args).forEach(k => { if ((args[k].source || '') === 'ref_node') hasNodeRef = true; });
      }));
    }
    if (!hasNodeRef) {
      collectOutputs().forEach(o => { if ((o.source || '') === 'ref_node') hasNodeRef = true; });
    }
    if (hasNodeRef) {
      showToast('存在「引用节点参数」的绑定，但本节点尚未定义任何参数。请先在「节点参数」中设置参数后再保存', 'error');
      return;
    }
  }
  // 路由条件（switch_condition）：condSwitch / Activity 共用，将输入框内容写入 Configuration.node_config，确保落库。
  // 必须放在 Activity 分支之前，否则 CondSwitch 节点不会走 Activity 分支导致 switch_condition 丢失。
  if (body.type === 'custom/CondSwitch' || body.type === 'custom/Activity') {
    if (typeof body.configuration !== 'object' || body.configuration === null) body.configuration = {};
    if (!body.configuration.node_config || typeof body.configuration.node_config !== 'object') body.configuration.node_config = {};
    body.configuration.node_config.switch_condition = document.getElementById('node-switch-condition')?.value || '';
    // 旧字段 condition 已废弃：保存时直接丢弃，统一使用 switch_condition
    if (body.configuration.node_config.condition !== undefined) {
      delete body.configuration.node_config.condition;
    }
  }
  // activity 类型：校验 activity 编排，并写入 Configuration
  if (body.type === 'custom/Activity') {
    if (typeof body.configuration !== 'object' || body.configuration === null) body.configuration = {};
    if (!body.configuration.node_config || typeof body.configuration.node_config !== 'object') body.configuration.node_config = {};
    // 执行前判断条件（enable_condition）：将输入框内容写入 Configuration.node_config，确保落库
    // 用可选链保护：仅编排页(orch.html)含该输入框，管理页(index.html)无此 UI 时跳过取值。
    body.configuration.node_config.enable_condition = document.getElementById('node-enable-condition')?.value || '';
    if (body.configuration.node_config.node_condition !== undefined) {
      delete body.configuration.node_config.node_condition; // 清理旧字段
    }
    const stages = collectStages();
    const flat = [];
    stages.forEach(s => s.forEach(it => flat.push(it)));
    if (flat.length > 0) {
      // 二维 activities 模式：每项都必须有 act_name
      for (const it of flat) {
        if (!it.act_name) { showToast('每个 Activity 都必须填写活动名称 (act_name)', 'error'); return; }
      }
      body.configuration.node_config.activities = stages.map(stage => stage.map(it => {
        const args = it.args || {};
        const argumentsArr = Object.keys(args).map(k => {
          const b = args[k] || {};
          return {
            key: k,
            source: b.source || 'value',
            value: b.value !== undefined ? b.value : (b.ref || ''),
            ref: b.ref || '',
            type: b.type || 'string'
          };
        });
        const entry = {
          act_namespace: it.act_namespace,
          act_name: it.act_name,
          id: it.id || it.act_name,
          arguments: argumentsArr
        };
        // 保留每个 activity 的 arg_template / ret_template（节点执行时用于参数解析），避免保存时被丢弃
        if (it.arg_template && Object.keys(it.arg_template).length > 0) entry.arg_template = it.arg_template;
        if (it.ret_template && Object.keys(it.ret_template).length > 0) entry.ret_template = it.ret_template;
        return entry;
      }));
      delete body.configuration.node_config.stages;
      delete body.configuration.activities;
      delete body.configuration.node_config.act_namespace;
      delete body.configuration.node_config.act_name;
    } else {
      // 旧单 activity 自定义输入模式
      const name = document.getElementById('node-act-name').value.trim();
      if (name === '') { showToast('activity 类型必须填写活动名称', 'error'); return; }
      delete body.configuration.node_config.stages;
      delete body.configuration.activities;
      delete body.configuration.node_config.act_namespace;
      delete body.configuration.node_config.act_name;
    }
  }

  try {
    if (isEdit) {
      await api('/api/nodes/' + encodeURIComponent(body.node_id), { method: 'PUT', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
      showToast('节点已更新', 'success');
    } else {
      await api('/api/nodes', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
      showToast('节点已创建', 'success');
    }
    closeNodeModal();
    loadNodes();
  } catch (e) { showToast('保存失败: ' + e.message, 'error'); }
}

async function editNodeByIndex(i) {
  const n = window._nodesForEdit[i];
  if (!n) return;
  if (n.published_in_root_chain) {
    if (isAdmin()) {
      if (!confirm('该节点已发布到根链（线上生效版本引用），修改可能影响线上调用。\n\n确认要继续编辑吗？')) return;
    } else {
      showPublishedLockedToast('该节点'); return;
    }
  }
  // 列表接口已瘦身，不返回 configuration / additional_info / params / outputs 等大字段；
  // 编辑需要全量定义，按 ID 单条获取。
  let full;
  try {
    full = await api('/api/nodes/' + encodeURIComponent(n.node_id));
  } catch (e) { showToast('加载节点失败: ' + (e.message || e), 'error'); return; }
  openNodeModal(full);
}

async function deleteNode(id) {
  const n = (window._nodesForEdit || []).find(x => x.node_id === id);
  if (n && n.published_in_root_chain) {
    if (isAdmin()) {
      if (!confirm('该节点已发布到根链（线上生效版本引用），删除可能影响线上调用。\n\n确认要删除吗？')) return;
    } else {
      showPublishedLockedToast('该节点'); return;
    }
  } else {
    if (!confirm('确定删除节点 ' + id + ' 吗？')) return;
  }
  try {
    await api('/api/nodes/' + encodeURIComponent(id), { method: 'DELETE' });
    showToast('节点已删除', 'success');
    loadNodes();
  } catch (e) { showToast('删除失败: ' + e.message, 'error'); }
}

// ============================================================
// Activities Tab 管理
// ============================================================

// 当前 project 的 activity 缓存，供 Node 编辑器下拉使用
window._activityCache = [];

// 根据标签名称生成稳定的颜色 class，使不同标签在列表中显示为不同颜色
function tagColorClass(tag) {
  const s = String(tag || '');
  // 两个特殊标签固定颜色：条件查询=绿，策略执行=红
  if (s === '条件查询') return 'tag-cond';
  if (s === '策略执行') return 'tag-act';
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) >>> 0;
  }
  return 'tag-c' + (h % 10);
}

// 加载 activity 列表（拉取全部，前端按标签筛选，保证筛选项稳定）
async function loadActivities() {
  const p = getProject();
  if (!p) return;
  const env = document.getElementById('act-env-filter') ? document.getElementById('act-env-filter').value : '';
  const envQ = env ? ('&env=' + encodeURIComponent(env)) : '';
  try {
    const data = await api('/api/activities' + '?_=1' + envQ);
    window._activityCache = data || []; // 全量缓存（已按所选环境带心跳/测试状态）
    // 保持当前页码（删除/编辑后刷新仍停留在当前页；当前页删空时 renderActivityTable 自动退回上一页）
    renderActivityTable(applyActivityTagFilter(window._activityCache));
    refreshActivityTagFilterOptions();
  } catch (e) { showToast('加载 activities 失败: ' + e.message, 'error'); }
}

// 读取 Activities 列表上选择的环境（空字符串表示"全部环境"）
function getActivityListEnv() {
  const sel = document.getElementById('act-env-filter');
  return sel ? (sel.value || '') : '';
}

// 为 Activity 相关弹窗（测试/监听代码/日志）选择默认环境。
// 优先级：列表已选环境 > 上次使用环境(last_test_env) > 首个环境。
// envs 为环境配置数组；allowEmpty=true 时（如日志弹窗有"全部环境"选项）不回退到首个环境。
function pickActivityModalEnv(envs, allowEmpty) {
  const list = envs || [];
  const has = name => list.some(e => e.env_name === name);
  const listEnv = getActivityListEnv();
  if (listEnv && has(listEnv)) return listEnv;
  if (listEnv === '' && allowEmpty) return '';
  const last = localStorage.getItem('last_test_env');
  if (last && has(last)) return last;
  if (!allowEmpty && list.length > 0) return list[0].env_name;
  return '';
}

// 环境配置列表按项目共享缓存：nodes / activities 等多个初始化入口都复用同一请求，
// 避免同一项目在刷新时重复拉取 /api/env-configs（index.html 中 nodes-table 与 act-env-filter
// 同时存在于 DOM，初始化会分别触发 loadNodeEnvOptions 与 loadActivityEnvOptions，各自拉一次）。
let _envConfigsPromise = null;
let _envConfigsProject = '';
async function fetchEnvConfigs(force = false) {
  const p = getProject();
  if (!force && _envConfigsPromise && _envConfigsProject === p) return _envConfigsPromise;
  _envConfigsProject = p;
  _envConfigsPromise = api('/api/env-configs').catch(e => { _envConfigsPromise = null; throw e; });
  return _envConfigsPromise;
}

// Activities 列表加载按项目缓存本次会话结果，避免重复拉取。
// 与 loadOrchData 同理：初始化时序里 顶层 loadProjects(.then) / bootstrap(onLoginSuccess→loadProjects)
// / applyInitialTab(switchTab) 都会触发 loadActivityEnvOptions，导致 /api/activities 同一项目被拉取多次。
let _activityEnvLoadPromise = null;
let _activityEnvLoadedProject = '';

// 初始化 Activities 环境筛选下拉（复用环境配置列表），恢复上次选择的环境，并加载列表
async function loadActivityEnvOptions(force = false) {
  const sel = document.getElementById('act-env-filter');
  if (!sel) return;
  const p = getProject();
  // 同一项目下并发/重复调用复用同一请求，仅拉取一次；force=true 可强制刷新。
  if (!force && _activityEnvLoadPromise && _activityEnvLoadedProject === p) {
    return _activityEnvLoadPromise;
  }
  _activityEnvLoadedProject = p;
  _actPage = 1;
  restoreActivityPageSize();
  _activityEnvLoadPromise = (async () => {
    try {
      const envs = await fetchEnvConfigs();
      const savedEnv = (() => { try { return localStorage.getItem('wf_activity_env') || ''; } catch (_) { return ''; } })();
      sel.innerHTML = '<option value="">全部环境</option>';
      (envs || []).forEach(e => {
        const opt = document.createElement('option');
        opt.value = e.env_name;
        opt.textContent = e.env_name + (e.description ? ' (' + e.description + ')' : '');
        sel.appendChild(opt);
      });
      // 恢复上次选择的环境（仅当该环境仍存在时），否则回退到默认"全部环境"
      if (savedEnv && (envs || []).some(e => e.env_name === savedEnv)) sel.value = savedEnv;
      else sel.value = '';
    } catch (e) { /* 环境列表可选 */ }
    // 环境选项就绪后再加载列表，确保带上恢复后的环境去查询
    loadActivities();
  })();
  return _activityEnvLoadPromise;
}

// 关键词搜索变化：在全量数据上过滤（而非仅当前页），并回到第一页
function onActivitySearchChange() {
  _actPage = 1;
  renderActivityTable(applyActivityTagFilter(window._activityCache));
}

// 按当前选择的标签 + 搜索关键词过滤列表
function applyActivityTagFilter(list) {
  let out = list || [];
  const tag = document.getElementById('act-tag-filter').value;
  if (tag) out = out.filter(a => (a.tags || []).indexOf(tag) >= 0);
  const searchEl = document.getElementById('act-search');
  const kw = searchEl ? searchEl.value.trim().toLowerCase() : '';
  if (kw) {
    out = out.filter(a => [a.activity_id, a.name, a.act_namespace, a.act_name, a.activity_type]
      .concat(a.tags || [])
      .some(v => v && String(v).toLowerCase().includes(kw)));
  }
  return out;
}

// 根据当前列表所有标签刷新筛选下拉项
function refreshActivityTagFilterOptions() {
  const sel = document.getElementById('act-tag-filter');
  const cur = sel.value;
  const tagSet = {};
  (window._activityCache || []).forEach(a => {
    (a.tags || []).forEach(t => { if (t) tagSet[t] = true; });
  });
  const tags = Object.keys(tagSet).sort();
  sel.innerHTML = '<option value="">全部标签</option>' + tags.map(t => {
    const v = escHtml(t);
    return '<option value="' + v + '">' + v + '</option>';
  }).join('');
  // 优先恢复上次保存的标签（仅当该标签仍存在），否则保持当前选择
  let restored = cur;
  try {
    const savedTag = localStorage.getItem('wf_activity_tag') || '';
    if (savedTag && tags.indexOf(savedTag) >= 0) restored = savedTag;
  } catch (_) {}
  sel.value = restored;
}

// 刷新 activity 缓存（不重新渲染表格，由调用方决定）
async function refreshActivityCache() {
  const p = getProject();
  if (!p) return;
  try {
    const data = await api('/api/activities');
    window._activityCache = data || [];
  } catch(e) { /* ignore */ }
}

// 根据参数值推断来源（调用传入/引用节点/固定配置），用于从 DSL arguments 兜底回显
function inferOrchParamPreset(val) {
  const s = (val == null) ? '' : String(val);
  if (!s) return null;
  if (/^\{\{steps\.[^.]+\.(arguments|responses)\.[^}]+?\}\}$/.test(s)) {
    return { src: PARAM_SRC_UPSTREAM, value: s };
  }
  if (/^\{\{[^}]+\}\}$/.test(s)) {
    // 调用传入：{{参数名}}
    return { src: PARAM_SRC_ENTRY, value: s.replace(/^\{\{|\}\}$/g, '') };
  }
  return { src: PARAM_SRC_FIXED, value: s };
}

// Activities 列表分页状态（前端分页：接口返回全量，保证标签筛选项稳定）
let _actPage = 1;
let _actPageSize = 20;

// 环境筛选变化：持久化选择 + 重新加载列表
function onActivityEnvFilterChange() {
  try { localStorage.setItem('wf_activity_env', document.getElementById('act-env-filter').value); } catch (_) {}
  _actPage = 1;
  loadActivities();
}

// 标签筛选变化：结果集改变，回到第一页，并持久化选择
function onActivityTagFilterChange() {
  try { localStorage.setItem('wf_activity_tag', document.getElementById('act-tag-filter').value); } catch (_) {}
  _actPage = 1;
  renderActivityTable(applyActivityTagFilter(window._activityCache));
}
function renderActivityTable(list) {
  const tbody = document.getElementById('act-table');
  const all = list || [];
  if (all.length === 0) {
    tbody.innerHTML = '<tr><td colspan="9" class="empty-state"><div class="icon">📋</div>暂无 activity 记录</td></tr>';
    renderActivityPager(0);
    return;
  }
  // 数据变少（如筛选/删除）时修正越界页码
  const totalPages = Math.max(1, Math.ceil(all.length / _actPageSize));
  if (_actPage > totalPages) _actPage = totalPages;
  if (_actPage < 1) _actPage = 1;
  const start = (_actPage - 1) * _actPageSize;
  const pageList = all.slice(start, start + _actPageSize);
  // 是否已选择具体环境：未选择时不展示测试状态图标与心跳（跨环境聚合无意义）
  const hasActivityEnvSelected = getActivityListEnv() !== '';
  tbody.innerHTML = pageList.map(a => {
    let argCount = 0;
    let argData = [];
    try {
      const args = typeof a.arguments === 'string' ? JSON.parse(a.arguments) : a.arguments;
      if (Array.isArray(args)) {
        argCount = args.length;
        argData = args.map(p => ({ key: p.key || '-', label: p.label || '' }));
      }
    } catch(e) {}
    const argJson = escAttr(JSON.stringify(argData));
    // 标签每个一行，最多显示 4 个汉字宽，超出用省略号，悬停看全名
    const tagsHtml = (a.tags || []).map(t => '<span class="badge act-tag-badge ' + tagColorClass(t) + '" title="' + escAttr(t) + '">' + escHtml(t) + '</span>').join('') || '<span style="color:var(--text-muted)">-</span>';
    // 测试状态图标：success=绿色勾，failed=红色叉，none/无=灰色图标。
    // 未选择具体环境时不展示（避免多环境聚合导致的误判）。
    let testIcon = '';
    if (hasActivityEnvSelected) {
      testIcon = '<span title="未测试" style="color:#9ca3af">○</span>';
      if (a.test_status === 'success') {
        testIcon = '<span title="测试成功" style="color:#16a34a;font-weight:700">✓</span>';
      } else if (a.test_status === 'failed') {
        testIcon = '<span title="测试全部失败" style="color:#dc2626;font-weight:700">✕</span>';
      }
      testIcon += ' ';
    }
    return `<tr>
      <td class="act-col-id"><span class="code-cell">${escHtml(a.activity_id)}</span></td>
      <td>${testIcon}${escHtml(a.name)}<span style="margin-left:4px">${hasActivityEnvSelected ? heartbeatIconHtml(a.heartbeat) : ''}</span>${a.published_in_root_chain ? publishedLockBadge('已发布') : ''}</td>
      <td class="act-col-tag">${tagsHtml}</td>
      <td class="act-ns-name"><div class="code-cell act-ns" title="命名空间：${escAttr(a.act_namespace)}">${escHtml(a.act_namespace)}</div><div class="code-cell" title="活动名称：${escAttr(a.act_name)}">${escHtml(a.act_name)}</div></td>
      <td class="arg-count" data-args="${argJson}" ${argCount ? '' : 'data-empty="1"'}>${argCount || '0'}</td>
      <td class="pubref-count" data-chains="${publishedRefsJsonAttr(a)}" data-empty="${(a.published_root_chains && a.published_root_chains.length) ? '' : '1'}">${publishedRefsCountHtml(a)}</td>
      <td class="noderef-count" data-nodes="${refNodesJsonAttr(a)}" data-empty="${activityRefNodes(a).length ? '' : '1'}">${refNodesCountHtml(a)}</td>
      <td class="act-col-time">${escHtml(a.created_at ? a.created_at.substring(0,10) : '-')}</td>
      <td class="act-col-ops"><div class="actions">
        <button class="btn btn-sm btn-outline btn-icon edit-only" ${actionIconAttrs('编辑', a.published_in_root_chain, '已发布到根链，禁止编辑')} onclick="editActivity('${escHtml(a.activity_id)}')">${actionIconSvg('edit')}</button>
        <button class="btn btn-sm btn-primary btn-icon" ${actionIconAttrs('测试')} onclick="openTestActivityModal('${escHtml(a.activity_id)}')">${actionIconSvg('test')}</button>
        <button class="btn btn-sm btn-outline btn-icon" ${actionIconAttrs('监听代码')} onclick="showActivityListenerCode('${escHtml(a.activity_id)}')">${actionIconSvg('code')}</button>
        <button class="btn btn-sm btn-outline btn-icon" ${actionIconAttrs('日志')} onclick="openActivityLogsModal('${escHtml(a.activity_id)}', '${escHtml(a.act_name)}')">${actionIconSvg('log')}</button>
        <button class="btn btn-sm btn-danger btn-icon edit-only" ${actionIconAttrs('删除', a.published_in_root_chain, '已发布到根链，禁止删除')} onclick="deleteActivityById('${escHtml(a.activity_id)}')">${actionIconSvg('delete')}</button>
      </div></td>
    </tr>`;
  }).join('');
  renderActivityPager(all.length);
}

// 渲染 Activities 分页器：有数据即展示总数与页码，便于切换每页条数
function renderActivityPager(total) {
  const pager = document.getElementById('act-pager');
  if (!pager) return;
  const totalPages = Math.max(1, Math.ceil(total / _actPageSize));
  if (total <= 0) {
    pager.style.display = 'none';
    return;
  }
  pager.style.display = 'flex';
  document.getElementById('act-page-info').textContent =
    '第 ' + _actPage + ' / ' + totalPages + ' 页（共 ' + total + ' 条）';
  document.getElementById('act-prev').disabled = _actPage <= 1;
  document.getElementById('act-next').disabled = _actPage >= totalPages;
}

// 切换 Activities 页码（基于当前标签筛选后的结果）
function changeActivityPage(delta) {
  const list = applyActivityTagFilter(window._activityCache);
  const totalPages = Math.max(1, Math.ceil((list || []).length / _actPageSize));
  const newPage = _actPage + delta;
  if (newPage < 1 || newPage > totalPages) return;
  _actPage = newPage;
  renderActivityTable(list);
}

// 每页条数变化：持久化选择并回到第一页
function onActivityPageSizeChange() {
  const sel = document.getElementById('act-page-size');
  const size = parseInt(sel ? sel.value : '', 10);
  if (!(size > 0)) return;
  _actPageSize = size;
  try { localStorage.setItem('wf_activity_page_size', String(size)); } catch (_) {}
  _actPage = 1;
  renderActivityTable(applyActivityTagFilter(window._activityCache));
}

// 恢复上次选择的每页条数（仅接受下拉中存在的选项）
function restoreActivityPageSize() {
  const sel = document.getElementById('act-page-size');
  if (!sel) return;
  let saved = '';
  try { saved = localStorage.getItem('wf_activity_page_size') || ''; } catch (_) {}
  if (saved && Array.prototype.some.call(sel.options, o => o.value === saved)) sel.value = saved;
  _actPageSize = parseInt(sel.value, 10) || 20;
}

// 根据心跳信息渲染心跳图标（♥）：最近1分钟内无心跳（count==0）显示灰色（worker 未启动/已挂），
// 否则按存活比例：≥90% 绿，≥60% 黄，≥30% 橙，<30% 红。
function heartbeatIconHtml(hb) {
  if (!hb || typeof hb.ratio !== 'number') return '';
  const pct = Math.round(hb.ratio * 100);
  if (!hb.count || hb.count <= 0) {
    return `<span class="hb-heart gray" title="最近1分钟无心跳（worker 可能未启动）">&#10084;<span class="hb-offline">离线</span></span>`;
  }
  let cls = 'red';
  if (hb.ratio >= 0.9) cls = 'green';
  else if (hb.ratio >= 0.6) cls = 'yellow';
  else if (hb.ratio >= 0.3) cls = 'orange';
  return `<span class="hb-heart ${cls}" title="最近1分钟心跳存活比例 ${pct}%（${hb.count} 次）">&#10084;</span>`;
}

// 根据 node 内各 activity 的心跳信息渲染聚合心跳图标（♥）：
// 只要有一个 activity 离线（最近1分钟无心跳）则整个 node 显示离线（灰色）；
// 否则按最差的存活比例着色。悬停（pop 层）展示每个 activity 的心跳状态。
// 未选择具体环境（node_heartbeats 为空）时不展示图标。
function nodeHeartbeatIconHtml(heartbeats) {
  if (!heartbeats || !heartbeats.length) return '';
  const total = heartbeats.length;
  let anyOffline = false;
  let worst = 1.0;
  heartbeats.forEach(h => {
    if (!h.count || h.count <= 0) anyOffline = true;
    if (typeof h.ratio === 'number' && h.ratio < worst) worst = h.ratio;
  });
  let cls, title;
  if (anyOffline) {
    cls = 'gray';
    title = '节点内存在离线 activity（共 ' + total + ' 个）';
  } else {
    if (worst >= 0.9) cls = 'green';
    else if (worst >= 0.6) cls = 'yellow';
    else if (worst >= 0.3) cls = 'orange';
    else cls = 'red';
    title = '节点内 ' + total + ' 个 activity 心跳正常（最差存活比例 ' + Math.round(worst * 100) + '%）';
  }
  const items = heartbeats.map(h => {
    const status = (!h.count || h.count <= 0) ? '离线' : (Math.round(h.ratio * 100) + '%');
    const name = (h.act_namespace ? h.act_namespace + '/' : '') + h.act_name;
    return '<div class="hb-pop-item">' + heartbeatIconHtml(h) +
      '<span class="hb-pop-name">' + esc(name) + '</span>' +
      '<span class="hb-pop-status">' + status + '</span></div>';
  }).join('');
  const popHtml = '<div class="hb-pop-title">节点内 activity 心跳（共 ' + total + ' 个）</div>' + items;
  return `<span class="node-hb"><span class="hb-heart ${cls}" title="${esc(title)}" ` +
    `onmouseenter="showHbPop(event,this)" onmousemove="moveHbPop(event)" onmouseleave="hideHbPop()">&#10084;</span>` +
    `<span style="display:none" data-pop="${encodeURIComponent(popHtml)}"></span></span>`;
}

// node 心跳 pop 层用固定在视口的全局浮层，避免被表格 .table-wrap 的 overflow 裁剪。
let _hbPopEl = null;
let _hbPopHideTimer = null;
function showHbPop(e, el) {
  if (_hbPopHideTimer) { clearTimeout(_hbPopHideTimer); _hbPopHideTimer = null; }
  if (!_hbPopEl) {
    _hbPopEl = document.createElement('div');
    _hbPopEl.id = 'hb-popover';
    _hbPopEl.className = 'hb-pop';
    document.body.appendChild(_hbPopEl);
  }
  const holder = el.parentElement.querySelector('span[data-pop]');
  _hbPopEl.innerHTML = holder ? decodeURIComponent(holder.getAttribute('data-pop')) : '';
  _hbPopEl.style.display = 'block';
  moveHbPop(e);
}
function moveHbPop(e) {
  if (!_hbPopEl) return;
  const pad = 12;
  const w = _hbPopEl.offsetWidth, h = _hbPopEl.offsetHeight;
  let left = e.clientX + pad;
  let top = e.clientY + pad;
  if (left + w > window.innerWidth) left = e.clientX - w - pad;
  if (top + h > window.innerHeight) top = window.innerHeight - h - pad;
  if (top < 0) top = pad;
  _hbPopEl.style.left = left + 'px';
  _hbPopEl.style.top = top + 'px';
}
function hideHbPop() {
  if (!_hbPopEl) return;
  _hbPopHideTimer = setTimeout(() => { if (_hbPopEl) _hbPopEl.style.display = 'none'; }, 120);
}

// 打开 activity 编辑弹窗
function openActivityModal(act) {
  const isEdit = !!act;
  document.getElementById('act-modal-title').innerHTML = (isEdit ? '编辑 Activity' : '新建 Activity') + '<button class="modal-close" onclick="closeActivityModal()" title="关闭">&times;</button>';
  document.getElementById('act-is-edit').value = isEdit ? '1' : '0';
  document.getElementById('act-activity-id').value = act ? act.activity_id : '';
  document.getElementById('act-name').value = act ? act.name : '';
  document.getElementById('act-namespace').value = act ? act.act_namespace : '';
  document.getElementById('act-act-name').value = act ? act.act_name : '';
  document.getElementById('act-activity-type').value = act ? (act.activity_type || '') : '';
  document.getElementById('act-status').value = act ? act.status : 1;
  document.getElementById('act-arg-template').value = act ? (act.arg_template || '') : '';
  document.getElementById('act-description').value = act ? (act.description || '') : '';
  document.getElementById('act-tags').value = act && Array.isArray(act.tags) ? act.tags.join(',') : '';
  renderActTagChips();
  // 大类型 Kind + HTTP 配置
  const kind = act && act.kind ? act.kind : 'redis';
  document.getElementById('act-kind').value = kind;
  let httpCfg = {};
  if (act && act.http_config) {
    try { httpCfg = (typeof act.http_config === 'string') ? JSON.parse(act.http_config) : act.http_config; } catch(e) { httpCfg = {}; }
  }
  document.getElementById('act-http-method').value = httpCfg.method || 'POST';
  document.getElementById('act-http-url').value = httpCfg.url || '';
  // headers 转为 "key: value" 多行文本
  let headerText = '';
  if (httpCfg.headers && typeof httpCfg.headers === 'object') {
    headerText = Object.entries(httpCfg.headers).map(([k, v]) => k + ': ' + v).join('\n');
  }
  document.getElementById('act-http-headers').value = headerText;
  document.getElementById('act-http-body').value = httpCfg.body_template || '';
  onActKindChange();
  // 参数定义：逐条填充到参数编辑区（arguments 为 []*param.BindConfig 数组）
  clearActParamRows();
  let args = act ? act.arguments : null;
  if (typeof args === 'string') { try { args = JSON.parse(args); } catch(e) { args = null; } }
  if (Array.isArray(args) && args.length > 0) {
    args.forEach(p => addActParamRow(p.key, p.label, p.type, p.required, p.value !== undefined && p.value !== null ? p.value : p.default_value, p.description, p.policy));
  }
  // Responses 保持为 JSON 字符串
  let resp = act ? act.responses : null;
  if (typeof resp === 'object' && resp !== null) resp = JSON.stringify(resp);
  document.getElementById('act-responses').value = resp ? (typeof resp === 'string' ? resp : JSON.stringify(resp)) : '';
  // 返回值设置 (ReturnValues) 回显
  clearReturnValueRows();
  let rvs = act ? act.return_values : null;
  if (typeof rvs === 'string') { try { rvs = JSON.parse(rvs); } catch(e) { rvs = null; } }
  if (Array.isArray(rvs) && rvs.length > 0) {
    rvs.forEach(it => addReturnValueRow(it.name, it.key || '', it.type || '', it.label || ''));
  }
  // 异步加载测试成功记录的返回 map 所有 key，回填 key 下拉选项
  loadReturnValueKeyOptions(act ? act.activity_id : '');
  document.getElementById('act-modal-overlay').classList.add('show');
}

// 根据大类型切换 HTTP 配置区块的显示
function onActKindChange() {
  const kind = document.getElementById('act-kind').value;
  document.getElementById('act-http-config').style.display = (kind === 'http') ? '' : 'none';
}

// 读取当前已选标签（input 手动输入 + 已点选 chip）
function getActSelectedTags() {
  return document.getElementById('act-tags').value.split(',')
    .map(s => s.trim()).filter(s => s !== '');
}

// 设置当前已选标签（写回 input，并刷新 chips 高亮）
function setActSelectedTags(tags) {
  const uniq = [];
  (tags || []).forEach(t => { if (t && uniq.indexOf(t) < 0) uniq.push(t); });
  document.getElementById('act-tags').value = uniq.join(',');
  renderActTagChips();
}

// 渲染可点击的历史标签 chips（已选中的高亮，点击切换）
function renderActTagChips() {
  const box = document.getElementById('act-tags-chips');
  if (!box) return;
  const allTags = {};
  (window._activityCache || []).forEach(a => {
    (a.tags || []).forEach(t => { if (t) allTags[t] = true; });
  });
  const selected = getActSelectedTags();
  const tags = Object.keys(allTags).sort();
  if (tags.length === 0) { box.innerHTML = ''; return; }
  box.innerHTML = tags.map(t => {
    const v = escHtml(t);
    const on = selected.indexOf(t) >= 0 ? ' on' : '';
    return '<span class="tag-chip' + on + '" onclick="toggleActTag(\'' + v.replace(/'/g, "\\'") + '\')">' + v + (on ? ' ✓' : '') + '</span>';
  }).join('');
}

// 点击 chip 切换标签选中状态
function toggleActTag(tag) {
  const selected = getActSelectedTags();
  const idx = selected.indexOf(tag);
  if (idx >= 0) {
    selected.splice(idx, 1);
  } else {
    selected.push(tag);
  }
  setActSelectedTags(selected);
}

// 关闭 activity 弹窗
function closeActivityModal() {
  clearActParamRows();
  document.getElementById('act-modal-overlay').classList.remove('show');
}

// ============================================================
// 从其它项目复制 Activity
// ============================================================
let _copyCandidates = [];          // 过滤后的候选列表（来源项目去重后）
let _copySelected = new Set();      // 选中的 activity_id 集合

// 打开复制弹窗：加载项目列表（排除当前项目）
async function openCopyActivityModal() {
  const p = getProject();
  if (!p) { showToast('请先选择项目', 'error'); return; }
  _copyCandidates = [];
  _copySelected = new Set();
  // 确保当前项目的 activity 缓存最新（用于去重过滤）
  if (!window._activityCache || window._activityCache.length === 0) {
    try { await loadActivities(); } catch (_) {}
  }
  const projectSel = document.getElementById('copy-act-project');
  const searchEl = document.getElementById('copy-act-search');
  if (searchEl) searchEl.value = '';
  // 加载项目列表（排除当前项目）
  try {
    const projects = await api('/api/projects');
    const cur = projectSel.value;
    projectSel.innerHTML = '<option value="">-- 选择目标项目 --</option>';
    (projects || [])
      .filter(pr => pr.project !== p)
      .forEach(pr => {
        const opt = document.createElement('option');
        opt.value = pr.project;
        opt.textContent = pr.project + (pr.description ? ' (' + pr.description + ')' : '');
        projectSel.appendChild(opt);
      });
    if (cur && projectSel.querySelector('option[value="' + cur + '"]')) projectSel.value = cur;
  } catch (e) { showToast('加载项目列表失败: ' + e.message, 'error'); }
  document.getElementById('copy-act-list').innerHTML = '<div class="empty-state" style="padding:20px">请先选择来源项目</div>';
  document.getElementById('copy-act-confirm').disabled = true;
  document.getElementById('copy-act-overlay').classList.add('show');
}

function closeCopyActivityModal() {
  document.getElementById('copy-act-overlay').classList.remove('show');
}

// 加载来源项目的 Activity，并过滤掉当前项目已有的（命名空间+活动名称）组合
async function loadCopyActivityCandidates() {
  const p = getProject();
  const target = document.getElementById('copy-act-project').value;
  const listEl = document.getElementById('copy-act-list');
  const confirmBtn = document.getElementById('copy-act-confirm');
  if (!target) {
    listEl.innerHTML = '<div class="empty-state" style="padding:20px">请先选择来源项目</div>';
    confirmBtn.disabled = true;
    return;
  }
  listEl.innerHTML = '<div class="empty-state" style="padding:20px">加载中...</div>';
  confirmBtn.disabled = true;
  try {
    // 拉取来源项目（target）的 Activity 列表
    const targetActs = await api('/api/activities?project=' + encodeURIComponent(target));
    // 当前项目已存在的 (namespace|act_name) 集合，用于去重
    const existKeys = {};
    (window._activityCache || []).forEach(a => {
      existKeys[(a.act_namespace || '') + '|' + (a.act_name || '')] = true;
    });
    // 候选 = 来源项目 Act 减去当前项目已存在的（命名空间+活动名相同即视为已存在）
    _copyCandidates = (targetActs || []).filter(a => {
      const key = (a.act_namespace || '') + '|' + (a.act_name || '');
      return !existKeys[key];
    });
    _copySelected = new Set();
    renderCopyActivityCandidates();
  } catch (e) {
    listEl.innerHTML = '<div class="empty-state" style="padding:20px;color:#dc2626">加载失败: ' + escHtml(e.message) + '</div>';
  }
}

// 渲染候选列表（带搜索过滤）
function renderCopyActivityCandidates() {
  const listEl = document.getElementById('copy-act-list');
  const searchEl = document.getElementById('copy-act-search');
  const kw = searchEl ? searchEl.value.trim().toLowerCase() : '';
  const list = (_copyCandidates || []).filter(a => {
    if (!kw) return true;
    return [a.name, a.act_namespace, a.act_name, a.activity_type]
      .some(v => v && String(v).toLowerCase().includes(kw));
  });
  const confirmBtn = document.getElementById('copy-act-confirm');
  if (list.length === 0) {
    listEl.innerHTML = '<div class="empty-state" style="padding:20px">' +
      (_copyCandidates.length === 0 ? '来源项目的 Activity 在当前项目中都已存在，无需复制' : '无匹配项') +
      '</div>';
    confirmBtn.disabled = true;
    return;
  }
  listEl.innerHTML = list.map(a => {
    const checked = _copySelected.has(a.activity_id) ? 'checked' : '';
    return '<label class="copy-act-item" style="display:flex;align-items:center;gap:10px;padding:8px 4px;border-bottom:1px solid var(--border)">' +
      '<input type="checkbox" ' + checked + ' onchange="onCopyCandidateToggle(\'' + escHtml(a.activity_id) + '\', this.checked)">' +
      '<span style="flex:1">' +
        '<strong>' + escHtml(a.name) + '</strong>' +
        '<span style="color:var(--text-muted);font-size:.8rem;margin-left:8px">' + escHtml(a.act_namespace) + ' / ' + escHtml(a.act_name) + '</span>' +
        (a.activity_type ? '<span class="badge" style="margin-left:8px">' + escHtml(a.activity_type) + '</span>' : '') +
      '</span>' +
      '<span style="color:var(--text-muted);font-size:.78rem">' + escHtml(a.activity_id) + '</span>' +
    '</label>';
  }).join('');
  confirmBtn.disabled = _copySelected.size === 0;
}

function onCopyCandidateToggle(activityID, checked) {
  if (checked) _copySelected.add(activityID);
  else _copySelected.delete(activityID);
  document.getElementById('copy-act-confirm').disabled = _copySelected.size === 0;
}

// 将选中的 Activity（来自来源项目）配置复制到当前项目
async function copyActivitiesToProject() {
  const p = getProject();
  const source = document.getElementById('copy-act-project').value;
  if (!source) { showToast('请先选择来源项目', 'error'); return; }
  const ids = Array.from(_copySelected);
  if (ids.length === 0) { showToast('请选择要复制的 Activity', 'error'); return; }
  const candidatesByID = {};
  _copyCandidates.forEach(a => { candidatesByID[a.activity_id] = a; });
  const confirmBtn = document.getElementById('copy-act-confirm');
  confirmBtn.disabled = true;
  let ok = 0, fail = 0;
  for (const id of ids) {
    const src = candidatesByID[id];
    if (!src) continue;
    // 克隆配置：project 改为当前项目，activity_id 留空由后端生成新的 ID（不复用来源 ID）
    const body = {
      name: src.name,
      act_namespace: src.act_namespace,
      act_name: src.act_name,
      activity_type: src.activity_type || '',
      kind: src.kind || '',
      http_config: src.http_config || '',
      arguments: src.arguments || null,
      arg_template: src.arg_template || '',
      responses: src.responses || null,
      status: src.status || 1,
      description: src.description || '',
      tags: src.tags || []
    };
    try {
      await api('/api/activities?project=' + encodeURIComponent(p), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      });
      ok++;
    } catch (e) {
      fail++;
      showToast('复制 ' + (src.name || id) + ' 失败: ' + e.message, 'error');
    }
  }
  if (ok > 0) showToast('已复制 ' + ok + ' 个 Activity 到当前项目 ' + p + (fail > 0 ? ('，' + fail + ' 个失败') : ''), 'success');
  // 复制后刷新当前项目列表，使新项立即可见
  if (ok > 0) loadActivities();
  closeCopyActivityModal();
}

// ============================================================
// 从其它项目复制 Node
// ============================================================
let _copyNodeCandidates = [];       // 过滤后的候选列表（来源项目去重后，含完整配置）
let _copyNodeSelected = new Set();  // 选中的 node_id 集合

// nodeCopyKey 判断 Node 是否已存在的键：命名空间 + 名称
function nodeCopyKey(n) {
  return (n.namespace || '') + '|' + (n.name || '');
}

// nodeConfigActivities 从 Node 配置中提取引用的 Activity（命名空间 + 活动名称）
function nodeConfigActivities(n) {
  let cfg = n.configuration;
  if (typeof cfg === 'string') { try { cfg = JSON.parse(cfg); } catch (_) { cfg = null; } }
  const nc = cfg && cfg.node_config;
  const out = [];
  if (!nc) return out;
  if (Array.isArray(nc.activities)) {
    nc.activities.forEach(stage => (Array.isArray(stage) ? stage : [stage]).forEach(it => {
      if (it && it.act_name) out.push({ act_namespace: it.act_namespace || '', act_name: it.act_name });
    }));
  } else if (nc.act_name) {
    out.push({ act_namespace: nc.act_namespace || '', act_name: nc.act_name });
  }
  return out;
}

// 打开复制 Node 弹窗：加载项目列表（排除当前项目）
async function openCopyNodeModal() {
  const p = getProject();
  if (!p) { showToast('请先选择项目', 'error'); return; }
  _copyNodeCandidates = [];
  _copyNodeSelected = new Set();
  // 用于标出来源 Node 引用、但当前项目尚不存在的 Activity
  if (!window._activityCache || window._activityCache.length === 0) {
    try { await loadActivities(); } catch (_) {}
  }
  const projectSel = document.getElementById('copy-node-project');
  const searchEl = document.getElementById('copy-node-search');
  if (searchEl) searchEl.value = '';
  try {
    const projects = await api('/api/projects');
    const cur = projectSel.value;
    projectSel.innerHTML = '<option value="">-- 选择来源项目 --</option>';
    (projects || [])
      .filter(pr => pr.project !== p)
      .forEach(pr => {
        const opt = document.createElement('option');
        opt.value = pr.project;
        opt.textContent = pr.project + (pr.description ? ' (' + pr.description + ')' : '');
        projectSel.appendChild(opt);
      });
    if (cur && projectSel.querySelector('option[value="' + cur + '"]')) projectSel.value = cur;
  } catch (e) { showToast('加载项目列表失败: ' + e.message, 'error'); }
  document.getElementById('copy-node-overlay').classList.add('show');
  if (projectSel.value) loadCopyNodeCandidates();
  else {
    document.getElementById('copy-node-list').innerHTML = '<div class="empty-state" style="padding:20px">请先选择来源项目</div>';
    document.getElementById('copy-node-confirm').disabled = true;
  }
}

function closeCopyNodeModal() {
  document.getElementById('copy-node-overlay').classList.remove('show');
}

// 加载来源项目的 Node（完整配置），并过滤掉当前项目已有的（命名空间+名称）组合
async function loadCopyNodeCandidates() {
  const source = document.getElementById('copy-node-project').value;
  const listEl = document.getElementById('copy-node-list');
  const confirmBtn = document.getElementById('copy-node-confirm');
  if (!source) {
    listEl.innerHTML = '<div class="empty-state" style="padding:20px">请先选择来源项目</div>';
    confirmBtn.disabled = true;
    return;
  }
  listEl.innerHTML = '<div class="empty-state" style="padding:20px">加载中...</div>';
  confirmBtn.disabled = true;
  try {
    // 当前项目列表可能带了命名空间/标签过滤，这里重新拉取当前项目全部 Node 用于去重
    const [sourceNodes, curNodes] = await Promise.all([
      api('/api/nodes?full=true&project=' + encodeURIComponent(source)),
      api('/api/nodes')
    ]);
    const existKeys = {};
    (curNodes || []).forEach(n => { existKeys[nodeCopyKey(n)] = true; });
    // 内置返回值节点每个项目自动生成，不参与复制
    _copyNodeCandidates = (sourceNodes || []).filter(n =>
      n.type !== 'custom/ReturnValue' && !existKeys[nodeCopyKey(n)]);
    _copyNodeSelected = new Set();
    renderCopyNodeCandidates();
  } catch (e) {
    listEl.innerHTML = '<div class="empty-state" style="padding:20px;color:#dc2626">加载失败: ' + escHtml(e.message) + '</div>';
  }
}

// 渲染候选列表（带搜索过滤），并标出当前项目缺失的 Activity
function renderCopyNodeCandidates() {
  const listEl = document.getElementById('copy-node-list');
  const searchEl = document.getElementById('copy-node-search');
  const kw = searchEl ? searchEl.value.trim().toLowerCase() : '';
  const list = (_copyNodeCandidates || []).filter(n => {
    if (!kw) return true;
    return [n.name, n.namespace, n.type, n.node_id]
      .some(v => v && String(v).toLowerCase().includes(kw));
  });
  const confirmBtn = document.getElementById('copy-node-confirm');
  if (list.length === 0) {
    listEl.innerHTML = '<div class="empty-state" style="padding:20px">' +
      (_copyNodeCandidates.length === 0 ? '来源项目的 Node 在当前项目中都已存在，无需复制' : '无匹配项') +
      '</div>';
    confirmBtn.disabled = true;
    return;
  }
  const actKeys = {};
  (window._activityCache || []).forEach(a => { actKeys[(a.act_namespace || '') + '|' + (a.act_name || '')] = true; });
  listEl.innerHTML = list.map(n => {
    const checked = _copyNodeSelected.has(n.node_id) ? 'checked' : '';
    const missing = nodeConfigActivities(n)
      .filter(a => !actKeys[a.act_namespace + '|' + a.act_name])
      .map(a => a.act_namespace + '/' + a.act_name);
    let warn = '';
    if (missing.length > 0) {
      warn = '<div style="color:#d97706;font-size:.76rem;margin-top:2px">当前项目缺少 Activity：' + escHtml(missing.join('、')) + '</div>';
    }
    if (n.type === 'custom/RootChainCall') {
      warn += '<div style="color:#d97706;font-size:.76rem;margin-top:2px">调用的是来源项目的根链，复制后需重新选择目标根链</div>';
    }
    return '<label class="copy-act-item" style="display:flex;align-items:center;gap:10px;padding:8px 4px;border-bottom:1px solid var(--border)">' +
      '<input type="checkbox" ' + checked + ' onchange="onCopyNodeCandidateToggle(\'' + escAttr(n.node_id) + '\', this.checked)">' +
      '<span style="flex:1">' +
        '<strong>' + escHtml(n.name) + '</strong>' +
        (n.namespace ? '<span style="color:var(--text-muted);font-size:.8rem;margin-left:8px">' + escHtml(n.namespace) + '</span>' : '') +
        (n.type ? '<span class="badge" style="margin-left:8px">' + escHtml(n.type) + '</span>' : '') +
        warn +
      '</span>' +
      '<span style="color:var(--text-muted);font-size:.78rem">' + escHtml(n.node_id) + '</span>' +
    '</label>';
  }).join('');
  confirmBtn.disabled = _copyNodeSelected.size === 0;
}

function onCopyNodeCandidateToggle(nodeID, checked) {
  if (checked) _copyNodeSelected.add(nodeID);
  else _copyNodeSelected.delete(nodeID);
  document.getElementById('copy-node-confirm').disabled = _copyNodeSelected.size === 0;
}

// 将选中的 Node（来自来源项目）配置复制到当前项目
async function copyNodesToProject() {
  const p = getProject();
  const source = document.getElementById('copy-node-project').value;
  if (!source) { showToast('请先选择来源项目', 'error'); return; }
  const ids = Array.from(_copyNodeSelected);
  if (ids.length === 0) { showToast('请选择要复制的 Node', 'error'); return; }
  const candidatesByID = {};
  _copyNodeCandidates.forEach(n => { candidatesByID[n.node_id] = n; });
  const confirmBtn = document.getElementById('copy-node-confirm');
  confirmBtn.disabled = true;
  let ok = 0, fail = 0;
  for (const id of ids) {
    const src = candidatesByID[id];
    if (!src) continue;
    // 克隆配置：project 改为当前项目，node_id 留空由后端生成新的 ID（不复用来源 ID）
    const body = {
      name: src.name,
      namespace: src.namespace || '',
      type: src.type,
      kind: src.kind || '',
      category: src.category || '',
      tags: src.tags || [],
      version: src.version || '',
      status: src.status || 1,
      description: src.description || '',
      debug_mode: !!src.debug_mode,
      configuration: src.configuration || {},
      additional_info: src.additional_info || {},
      params: src.params || [],
      outputs: src.outputs || []
    };
    try {
      await api('/api/nodes?project=' + encodeURIComponent(p), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      });
      ok++;
    } catch (e) {
      fail++;
      showToast('复制 ' + (src.name || id) + ' 失败: ' + e.message, 'error');
    }
  }
  if (ok > 0) showToast('已复制 ' + ok + ' 个 Node 到当前项目 ' + p + (fail > 0 ? ('，' + fail + ' 个失败') : ''), 'success');
  if (ok > 0) loadNodes();
  closeCopyNodeModal();
}

// 保存 activity
async function saveActivity() {
  const isEdit = document.getElementById('act-is-edit').value === '1';
  const p = getProject();
  if (!p) { showToast('请先选择项目', 'error'); return; }
  const ns = document.getElementById('act-namespace').value.trim();
  const aName = document.getElementById('act-act-name').value.trim();
  const label = document.getElementById('act-name').value.trim();
  if (!label) { showToast('名称不能为空', 'error'); return; }
  if (!ns) { showToast('命名空间不能为空', 'error'); return; }
  if (!aName) { showToast('活动名称不能为空', 'error'); return; }
  // 参数定义：从逐条编辑区收集为 arguments 数组（后端按 []*param.BindConfig 解析）
  let argumentsVal = collectActParams();
  let responsesVal = null;
  const respRaw = document.getElementById('act-responses').value.trim();
  if (respRaw) {
    try { responsesVal = JSON.parse(respRaw); } catch(e) { showToast('Responses JSON 格式错误: ' + e.message, 'error'); return; }
  }
  const body = {
    name: label,
    act_namespace: ns,
    act_name: aName,
    activity_type: document.getElementById('act-activity-type').value.trim(),
    kind: document.getElementById('act-kind').value.trim() || 'redis',
    arguments: argumentsVal,
    arg_template: document.getElementById('act-arg-template').value.trim(),
    responses: responsesVal,
    return_values: collectReturnValues(),
    status: parseInt(document.getElementById('act-status').value),
    description: document.getElementById('act-description').value.trim(),
    tags: document.getElementById('act-tags').value.split(',').map(s => s.trim()).filter(s => s !== ''),
  };
  // HTTP 配置（仅当 Kind=http 时收集）
  if (body.kind === 'http') {
    const headers = {};
    document.getElementById('act-http-headers').value.split('\n').forEach(line => {
      const idx = line.indexOf(':');
      if (idx > 0) {
        const k = line.slice(0, idx).trim();
        const v = line.slice(idx + 1).trim();
        if (k) headers[k] = v;
      }
    });
    const urlVal = document.getElementById('act-http-url').value.trim();
    if (!urlVal) { showToast('HTTP 类型必须填写 URL', 'error'); return; }
    body.http_config = JSON.stringify({
      method: document.getElementById('act-http-method').value.trim() || 'POST',
      url: urlVal,
      headers: headers,
      body_template: document.getElementById('act-http-body').value,
    });
  }
  try {
    if (isEdit) {
      body.activity_id = document.getElementById('act-activity-id').value;
      await api('/api/activities/' + encodeURIComponent(body.activity_id), { method: 'PUT', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
      showToast('Activity 已更新', 'success');
    } else {
      await api('/api/activities', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
      showToast('Activity 已创建', 'success');
    }
    closeActivityModal();
    loadActivities();
  } catch (e) { showToast('保存失败: ' + e.message, 'error'); }
}

// 编辑指定 activity
async function editActivity(activityID) {
  // 列表接口已瘦身（http_config / responses 置空），编辑需要全量定义，先按 ID 单条获取。
  let a;
  try {
    a = await api('/api/activities/' + encodeURIComponent(activityID));
  } catch (e) { showToast('加载 activity 失败: ' + (e.message || e), 'error'); return; }
  if (a && a.published_in_root_chain) {
    if (isAdmin()) {
      if (!confirm('该 activity 已发布到根链（线上生效版本引用），修改可能影响线上调用。\n\n确认要继续编辑吗？')) return;
    } else {
      showPublishedLockedToast('该 activity'); return;
    }
  }
  if (a) openActivityModal(a);
}

// ============================================================
// Activity 远程监听代码示例（变量按当前 Activity / 环境配置替换）
// ============================================================
let _listenerActivity = null;
let _listenerEnvs = [];

// 打开弹窗并加载环境列表
async function showActivityListenerCode(activityID) {
  const a = window._activityCache.find(x => x.activity_id === activityID);
  if (!a) { showToast('未找到该 activity', 'error'); return; }
  _listenerActivity = a;
  document.getElementById('act-listener-title').textContent = '远程监听代码: ' + a.activity_id;
  const envSel = document.getElementById('listener-env');
  envSel.innerHTML = '<option value="">-- 选择环境 --</option>';
  try {
    const envs = await api('/api/env-configs');
    _listenerEnvs = envs || [];
    _listenerEnvs.forEach(e => {
      const opt = document.createElement('option');
      opt.value = e.env_name;
      opt.textContent = e.env_name + (e.description ? ' (' + e.description + ')' : '');
      envSel.appendChild(opt);
    });
    // 默认与 Activities 列表所选环境保持一致，其次回退上次选择
    envSel.value = pickActivityModalEnv(_listenerEnvs, false);
  } catch (e) {
    _listenerEnvs = [];
  }
  await renderActivityListenerCode();
  document.getElementById('act-listener-overlay').classList.add('show');
}

// 监听代码环境变更，保存上次选择
function onListenerEnvChange() {
  const v = document.getElementById('listener-env').value;
  if (v) localStorage.setItem('last_test_env', v);
  renderActivityListenerCode();
}

// 根据所选环境从缓存中取 Redis 配置（addr 形如 host:port）
function getListenerRedisCfg(envName) {
  const def = _listenerEnvs.find(e => e.env_name === envName);
  const cfg = { host: '127.0.0.1', port: '6379', password: '', db: '0' };
  if (def && def.redis_config && def.redis_config.addr) {
    const parts = def.redis_config.addr.split(':');
    if (parts[0]) cfg.host = parts[0];
    if (parts[1]) cfg.port = parts[1];
    if (def.redis_config.password) cfg.password = def.redis_config.password;
    // redis database 序号（不填默认 0），缺失会导致连接到错误库而访问不到数据
    if (def.redis_config.db !== undefined && def.redis_config.db !== null && def.redis_config.db !== '') {
      cfg.db = String(def.redis_config.db);
    }
  }
  return cfg;
}

// 生成并渲染代码示例
async function renderActivityListenerCode() {
  const a = _listenerActivity;
  if (!a) return;
  const project = getProject() || 'your_project';
  const env = document.getElementById('listener-env').value || 'test';
  const redis = getListenerRedisCfg(env);
  const ns = a.act_namespace || 'your_namespace';
  const an = a.act_name || 'your_activity';
  const q = s => JSON.stringify(s === '' ? '' : s); // 双引号字符串字面量

  const code =
`package main

import (
	"context"

	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/utils"
	"github.com/magic-lib/go-plat-workflow/workflow"
)

func main() {
	projectName := ${q(project)}
	env := ${q(env)}

	w, err := workflow.NewMQWorker(projectName, env, &conn.Connect{
		Host:     ${q(redis.host)},
		Port:     ${q(redis.port)},
		Password: ${q(redis.password)},
		Database: ${q(redis.db)},
	})
	if err != nil {
		panic(err)
	}

	// 你的 activity 处理函数：签名为 func(ctx context.Context, args any) (any, error)
	// 可以使用这个方法进行转换： h, err = utils.ContextMethodToAnyHandler[*types.NidMobileReq, bool](m6Info.BankInMember)
	// args 即测试时前端传入的参数（map[string]any）
	handler := func(ctx context.Context, args any) (any, error) {
		// TODO: 在此实现业务逻辑
		return nil, nil
	}

	// 注册到 activity/${ns}/${an} 通道
	err = w.SubscribeActivity(${q(ns)}, ${q(an)}, handler)
	if err != nil {
		panic(err)
	}

	// 阻塞运行，使 worker 持续监听
	select {}
}`;
  document.getElementById('listener-code').textContent = code;
}

// 复制代码到剪贴板
function copyActivityListenerCode() {
  const text = document.getElementById('listener-code').textContent || '';
  navigator.clipboard.writeText(text).then(
    () => showToast('代码已复制', 'success'),
    () => showToast('复制失败，请手动选择复制', 'error')
  );
}

// 关闭弹窗
function closeActivityListenerCode() {
  document.getElementById('act-listener-overlay').classList.remove('show');
}

// 删除 activity
async function deleteActivityById(activityID) {
  const a = (window._activityCache || []).find(x => x.activity_id === activityID);
  if (a && a.published_in_root_chain) {
    if (isAdmin()) {
      if (!confirm('该 activity 已发布到根链（线上生效版本引用），删除可能影响线上调用。\n\n确认要删除吗？')) return;
    } else {
      showPublishedLockedToast('该 activity'); return;
    }
  } else {
    if (!confirm('确定删除 activity ' + activityID + ' 吗？')) return;
  }
  const p = getProject();
  if (!p) { showToast('请先选择项目', 'error'); return; }
  try {
    await api('/api/activities/' + encodeURIComponent(activityID), { method: 'DELETE' });
    showToast('Activity 已删除', 'success');
    loadActivities();
  } catch (e) { showToast('删除失败: ' + e.message, 'error'); }
}

// ============================================================
// 测试单个 Node（MQ 分布式执行）
// ============================================================

// 打开测试弹窗：加载环境列表 + 节点参数定义
async function openTestNodeModal(nodeId) {
  const p = getProject();
  if (!p) { showToast('请先选择项目', 'error'); return; }
  if (!nodeId) return;
  document.getElementById('test-node-id').value = nodeId;
  // 从节点缓存中反查中文名称，便于在标题处展示
  let nodeTitleName = '';
  const cachedNode = (window._nodesForEdit || []).find(n => n.node_id === nodeId);
  if (cachedNode && cachedNode.name) nodeTitleName = cachedNode.name;
  document.getElementById('test-node-modal-title').textContent = '测试 Node: ' + nodeId + (nodeTitleName ? '（' + nodeTitleName + '）' : '');
  document.getElementById('test-node-result').textContent = '点击"执行测试"后显示结果';
  document.getElementById('test-node-save').value = 'true';

  try {
    // 加载环境列表
    const envs = await api('/api/env-configs');
    const envSel = document.getElementById('test-node-env');
    envSel.innerHTML = '<option value="">-- 选择环境 --</option>';
    (envs || []).forEach(e => {
      const opt = document.createElement('option');
      opt.value = e.env_name;
      opt.textContent = e.env_name + (e.description ? ' (' + e.description + ')' : '');
      envSel.appendChild(opt);
    });
    // 默认恢复上次选择的环境
    const lastNode = localStorage.getItem('last_test_env');
    if (lastNode && (envs || []).some(e => e.env_name === lastNode)) {
      envSel.value = lastNode;
    } else if ((envs || []).length > 0) {
      envSel.value = envs[0].env_name;
    }

    // 加载节点详情（参数定义）：以 map 形式（key -> 参数定义）列出该 node 的所有参数，方便测试填写
    const node = await api('/api/nodes/' + encodeURIComponent(nodeId));
    let allParams = [];
    try {
      const raw = node.params;
      allParams = (typeof raw === 'string') ? JSON.parse(raw || '[]') : (raw || []);
    } catch (_) { allParams = []; }
    // 转为以 key 为索引的 map，避免 params 为数组/字符串差异导致解析失败
    window._testNodeParamsMap = {};
    if (Array.isArray(allParams)) {
      allParams.forEach(p => { if (p && p.key) window._testNodeParamsMap[p.key] = p; });
    }
    renderTestParams();
  } catch (e) { showToast('加载测试配置失败: ' + e.message, 'error'); }

  document.getElementById('test-node-modal-overlay').classList.add('show');
  loadNodeTestRecords(nodeId);
}

function closeTestNodeModal() {
  document.getElementById('test-node-modal-overlay').classList.remove('show');
}

// 渲染参数输入表单（以 map 形式列出节点所有参数，标注必传项）
function renderTestParams() {
  const container = document.getElementById('test-node-params');
  const map = window._testNodeParamsMap || {};
  const keys = Object.keys(map);
  if (!keys.length) {
    container.innerHTML = '<div id="test-node-params-empty" style="text-align:center;padding:16px;color:var(--text-muted);font-size:.85rem">该节点无参数定义</div>';
    return;
  }
  container.innerHTML = '<div style="font-size:.72rem;color:var(--text-muted);margin-bottom:6px">已列出该节点定义的所有参数（<span style="color:#dc2626">*</span> 为必传：在节点参数定义中勾选了「必填」的参数）</div>' +
    keys.map(key => {
    const bc = map[key];
    const required = (bc.required === true || bc.required === 'true');
    const reqMark = required ? ' <span style="color:#dc2626">*</span>' : '';
    const policyTag = bc.policy ? ' <span style="font-weight:400;color:#6b7280;font-size:.7rem">[' + esc(bc.policy) + ']</span>' : '';
    const ph = bc.value !== undefined && bc.value !== null ? String(bc.value) : '';
    // 以 key 生成唯一 input id（替换非单词字符避免 id 非法）
    const inputId = 'test-param-' + key.replace(/[^\w]/g, '_');
    return `
      <div class="form-row" style="margin-bottom:8px;align-items:flex-end">
        <div class="form-group" style="flex:1">
          <label>${esc(key)}${reqMark}${policyTag}</label>
          <input id="${inputId}" data-key="${esc(key)}" placeholder="${esc(ph)}" value="${esc(ph)}">
        </div>
      </div>`;
  }).join('');
}

function resetTestParams() {
  renderTestParams();
}

function onTestEnvChange() {
  const v = document.getElementById('test-node-env').value;
  if (v) localStorage.setItem('last_test_env', v);
}

// 收集表单参数（以 key -> value 的 JSON map 结构提交）
function collectTestParams() {
  const map = window._testNodeParamsMap || {};
  const out = {};
  Object.keys(map).forEach(key => {
    const bc = map[key];
    const el = document.getElementById('test-param-' + key.replace(/[^\w]/g, '_'));
    if (!el) return;
    const raw = el.value;
    let val = raw;
    if (raw !== '' && bc.value !== undefined && bc.value !== null) {
      if (typeof bc.value === 'number') {
        const n = Number(raw); if (!isNaN(n)) val = n;
      } else if (typeof bc.value === 'boolean') {
        if (raw === 'true') val = true; else if (raw === 'false') val = false;
      } else if (typeof bc.value === 'object') {
        try { val = JSON.parse(raw); } catch (_) { val = raw; }
      }
    } else if (raw !== '') {
      if (!isNaN(raw) && raw.trim() !== '') { const n = Number(raw); if (String(n) === raw) val = n; }
    }
    out[key] = val;
  });
  return out;
}

async function testNode() {
  const p = getProject();
  const nodeId = document.getElementById('test-node-id').value;
  const envName = document.getElementById('test-node-env').value;
  const save = document.getElementById('test-node-save').value === 'true';
  if (!envName) { showToast('请先选择环境', 'error'); return; }

  const inputParams = collectTestParams();
  const resultEl = document.getElementById('test-node-result');
  resultEl.textContent = '执行中...';

  try {
    const q = '?project=' + encodeURIComponent(p) + (save ? '' : '&save_record=false');
    const resp = await api('/api/nodes/' + encodeURIComponent(nodeId) + '/test' + q, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ env_name: envName, input_params: inputParams })
    });
    resultEl.textContent = JSON.stringify(resp, null, 2);
    if (resp.status === 'success') {
      showToast('测试成功' + (resp.record_id ? '，记录已保存: ' + resp.record_id : ''), 'success');
    } else {
      showToast('测试失败: ' + (resp.error_msg || ''), 'error');
    }
    loadNodeTestRecords(nodeId);
  } catch (e) {
    resultEl.textContent = '请求失败: ' + e.message;
    showToast('测试请求失败: ' + e.message, 'error');
  }
}

let _nodeTestRecords = [];

async function loadNodeTestRecords(nodeId) {
  const p = getProject();
  const box = document.getElementById('test-node-records');
  try {
    const records = await api('/api/nodes/' + encodeURIComponent(nodeId) + '/test-records?project=' + encodeURIComponent(p));
    _nodeTestRecords = records || [];
    if (!records.length) {
      box.innerHTML = '<div style="text-align:center;padding:12px;color:var(--text-muted);font-size:.82rem">暂无测试记录</div>';
      return;
    }
    box.innerHTML = records.map(r => `
      <div style="border:1px solid var(--border);border-radius:var(--radius);padding:8px;margin-bottom:6px;font-size:.78rem">
        <div style="display:flex;justify-content:space-between;align-items:center">
          <span><b>${esc(r.record_id)}</b> <span class="badge ${r.status==='success'?'badge-on':'badge-off'}">${r.status==='success'?'成功':'失败'}</span> ${r.env_name?('· 环境 '+esc(r.env_name)):''}</span>
          <span>
            <button class="btn btn-sm btn-outline" onclick="copyNodeTestRecord('${esc(r.record_id)}')">复制</button>
            <button class="btn btn-sm btn-danger edit-only" onclick="deleteNodeTestRecord('${esc(r.record_id)}','${esc(nodeId)}')">删除</button>
          </span>
        </div>
        <div style="color:var(--text-muted);margin:4px 0">${esc(r.created_at || '')}${(r.duration_ms !== undefined && r.duration_ms !== null)?((' · 耗时: ' + formatDuration(r.duration_ms))):''}${r.trace_id?(' · trace_id: <code style="font-size:.72rem">'+esc(r.trace_id)+'</code>'):''}</div>
        <div><b>入参:</b> <code style="font-size:.72rem;white-space:pre-wrap;word-break:break-all;overflow-wrap:break-word;display:block">${esc(trunc(r.input_params||'',2000000))}</code></div>
        <div><b>结果:</b> <code style="font-size:.72rem;white-space:pre-wrap;word-break:break-all;overflow-wrap:break-word;display:block">${esc(trunc(r.result||r.error_msg||'',2000000))}</code></div>
      </div>`).join('');
  } catch (e) {
    box.innerHTML = '<div style="text-align:center;padding:12px;color:var(--text-muted);font-size:.82rem">加载记录失败</div>';
  }
}

async function copyNodeTestRecord(recordId) {
  const rec = (_nodeTestRecords || []).find(x => x.record_id === recordId);
  if (!rec) { showToast('未找到该记录', 'error'); return; }
  const pretty = (val) => {
    if (val === undefined || val === null || val === '') return '';
    if (typeof val === 'string') {
      try { return JSON.stringify(JSON.parse(val), null, 2); } catch (e) { return val; }
    }
    try { return JSON.stringify(val, null, 2); } catch (e) { return String(val); }
  };
  const text =
    '参数:\n' + pretty(rec.input_params) +
    '\n\n返回值:\n' + pretty(rec.result !== undefined && rec.result !== '' ? rec.result : rec.error_msg);
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text);
    } else {
      const ta = document.createElement('textarea');
      ta.value = text; document.body.appendChild(ta); ta.select();
      document.execCommand('copy'); document.body.removeChild(ta);
    }
    showToast('已复制参数和返回值', 'success');
  } catch (e) { showToast('复制失败: ' + e.message, 'error'); }
}

async function deleteNodeTestRecord(recordId, nodeId) {
  if (!confirm('确定删除测试记录 ' + recordId + ' 吗？')) return;
  try {
    await api('/api/node-test-records/' + encodeURIComponent(recordId) + '?project=' + encodeURIComponent(getProject()), { method: 'DELETE' });
    showToast('记录已删除', 'success');
    loadNodeTestRecords(nodeId);
  } catch (e) { showToast('删除失败: ' + e.message, 'error'); }
}

async function clearNodeTestRecords() {
  const nodeId = document.getElementById('test-node-id').value;
  if (!nodeId) { showToast('未找到当前节点', 'error'); return; }
  if (!confirm('确定清除该节点的全部测试记录吗？此操作不可恢复。')) return;
  try {
    const resp = await api('/api/nodes/' + encodeURIComponent(nodeId) + '/test-records?project=' + encodeURIComponent(getProject()), { method: 'DELETE' });
    showToast('已清除全部测试记录' + (resp && resp.deleted ? ('（' + resp.deleted + ' 条）') : ''), 'success');
    loadNodeTestRecords(nodeId);
  } catch (e) { showToast('清除失败: ' + e.message, 'error'); }
}

// ============================================================
// Activity 执行日志查看
// ============================================================

let _logActivityId = '';
let _logActName = '';
let _logPage = 1;
const _logPageSize = 50;

async function openActivityLogsModal(activityId, actName) {
  _logActivityId = activityId;
  _logActName = actName || '';
  document.getElementById('activity-logs-name').textContent = _logActName ? ('Activity: ' + _logActName + ' (' + activityId + ')') : activityId;
  document.getElementById('activity-logs-modal-overlay').classList.add('show');
  clearLogFilters();
  // 先等环境下拉初始化（默认取列表所选环境），再按该环境查询，避免首次查询漏带 env
  await loadLogEnvOptions();
  loadActivityLogs();
}

function closeActivityLogsModal() {
  document.getElementById('activity-logs-modal-overlay').classList.remove('show');
}

function openTraceLogsModal() {
  document.getElementById('trace-logs-modal-overlay').classList.add('show');
  document.getElementById('trace-node-log-list').innerHTML = '<div class="log-empty">请输入 trace_id 后点击查询</div>';
  const input = document.getElementById('trace-log-trace-id');
  if (input) setTimeout(() => input.focus(), 50);
}

function closeTraceLogsModal() {
  document.getElementById('trace-logs-modal-overlay').classList.remove('show');
}

// 事件委托：trace 弹窗内 node 日志项的"查看该 node 下 activities"按钮
document.addEventListener('click', function (e) {
  const t = e.target;
  if (t && t.getAttribute && t.getAttribute('data-node-acts-toggle') === '1') {
    toggleNodeActivities(t);
  }
});

async function loadTraceLogs() {
  const nodeBox = document.getElementById('trace-node-log-list');
  const p = getProject();
  if (!p) { showToast('请先选择项目', 'error'); return; }
  const trace = document.getElementById('trace-log-trace-id').value.trim();
  if (!trace) { showToast('请输入 trace_id', 'error'); return; }
  _traceLogLastQuery = { trace: trace };
  nodeBox.innerHTML = '<div class="log-empty">加载中…</div>';
  try {
    // 查询该 trace 关联的 node 运行日志（来自 wf_node_logs），点开可查看该 trace 下 activities
    const nodeParams = new URLSearchParams();
    nodeParams.set('trace_id', trace);
    nodeParams.set('page', '1');
    nodeParams.set('page_size', '200');
    const nodeResp = await api('/api/node-logs?' + nodeParams.toString());
    const nodeData = Array.isArray(nodeResp) ? nodeResp : (nodeResp && nodeResp.list) || [];
    if (nodeData.length === 0) {
      nodeBox.innerHTML = '<div class="log-empty">该 trace_id 暂无 Node 运行日志</div>';
    } else {
      nodeBox.innerHTML = nodeData.map(r => renderTraceNodeLogItem(r, trace)).join('');
    }
  } catch (e) {
    nodeBox.innerHTML = '<div class="log-empty">加载 Node 日志失败: ' + esc(e.message) + '</div>';
  }
}

// 渲染 trace 弹窗中的 node 运行日志项：在 node 日志基础上附加"查看该 node 下 activities"下钻按钮。
// 点击后用 trace_id + node_span_id（即 node 日志的 span_id）查询该 node 关联的 activity 日志并内联展开。
function renderTraceNodeLogItem(r, traceId) {
  const base = renderNodeLogItem(r);
  const uid = 'tn-' + (r.trace_id || '') + '-' + (r.node_id || '') + '-' + (r.event_id || '') + '-' + (r.timestamp || '');
  const btn = '<button class="mini-btn" data-node-acts-toggle="1" data-uid="' + esc(uid) + '" data-node-id="' + esc(r.node_id || '') + '" data-node-span-id="' + esc(r.span_id || '') + '">查看该 node 下 Activities ▾</button>';
  const actsBox = '<div class="trace-node-acts" id="' + esc(uid) + '" style="display:none"></div>';
  return base.replace('</div>', btn + actsBox + '</div>');
}

// 通过 trace_id + node_span_id 查询该 node 关联的所有 activities 的执行记录并内联展开
async function toggleNodeActivities(btn) {
  const uid = btn.getAttribute('data-uid');
  const nodeSpanId = btn.getAttribute('data-node-span-id');
  const box = document.getElementById(uid);
  if (!box) return;
  if (box.style.display === 'none') {
    box.style.display = 'block';
    btn.textContent = '收起该 node 下 Activities ▴';
    if (!box.dataset.loaded) {
      box.dataset.loaded = '1';
      box.innerHTML = '<div class="log-empty">加载中…</div>';
      // trace_id 优先取按钮自带 data-trace-id（普通 node 日志列表场景），回退到 trace 弹窗输入框
      const trace = (btn.getAttribute('data-trace-id') || '').trim()
        || (document.getElementById('trace-log-trace-id') ? document.getElementById('trace-log-trace-id').value.trim() : '');
      const params = new URLSearchParams();
      params.set('trace_id', trace);
      if (nodeSpanId) params.set('node_span_id', nodeSpanId);
      params.set('page', '1');
      params.set('page_size', '200');
      try {
        const resp = await api('/api/activity-logs?' + params.toString());
        const data = Array.isArray(resp) ? resp : (resp && resp.list) || [];
        if (data.length === 0) {
          box.innerHTML = '<div class="log-empty">该 node 下未查询到 Activity 执行记录</div>';
        } else {
          box.innerHTML = '<div class="trace-node-acts-title">该 node 下 Activities 执行记录（' + data.length + ' 条）</div>' + data.map(renderLogItem).join('');
        }
      } catch (e) {
        box.innerHTML = '<div class="log-empty">加载失败: ' + esc(e.message) + '</div>';
      }
    }
  } else {
    box.style.display = 'none';
    btn.textContent = '查看该 node 下 Activities ▾';
  }
}

async function loadLogEnvOptions() {
  const sel = document.getElementById('log-env');
  if (!sel) return;
  try {
    const envs = await api('/api/env-configs');
    sel.innerHTML = '<option value="">环境：全部</option>';
    (envs || []).forEach(e => {
      const opt = document.createElement('option');
      opt.value = e.env_name;
      opt.textContent = e.env_name + (e.description ? ' (' + e.description + ')' : '');
      sel.appendChild(opt);
    });
    // 默认与 Activities 列表所选环境保持一致；列表选"全部环境"时这里也为全部
    sel.value = pickActivityModalEnv(envs, true);
  } catch (e) { /* 环境列表可选，失败不影响日志查询 */ }
}

function clearLogFilters() {
  document.getElementById('log-keyword').value = '';
  document.getElementById('log-level').value = '';
  document.getElementById('log-event-id').value = '';
  document.getElementById('log-root-chain-id').value = '';
  document.getElementById('log-trace-id').value = '';
  document.getElementById('log-span-id').value = '';
  const envSel = document.getElementById('log-env');
  // 重置为 Activities 列表当前所选环境，而非固定"全部"
  if (envSel) envSel.value = getActivityListEnv();
  _logPage = 1;
}

// ---------------- Node 运行日志弹窗 ----------------
let _nodeLogNodeId = '';
let _nodeLogNodeName = '';
let _nodeLogPage = 1;
const _nodeLogPageSize = 50;

async function openNodeLogModal(nodeId) {
  const node = (window._nodesForEdit || []).find(n => n.node_id === nodeId);
  _nodeLogNodeId = nodeId;
  _nodeLogNodeName = node ? node.name : nodeId;
  _nodeLogPage = 1;
  document.getElementById('node-logs-modal-overlay').classList.add('show');
  document.getElementById('node-logs-modal-title').firstChild.textContent =
    'Node 运行日志 (' + _nodeLogNodeId + ') ';
  document.getElementById('node-logs-name').textContent = _nodeLogNodeName;
  // 同步环境下拉
  await loadNodeLogEnvOptions();
  await loadNodeLogs();
}

function closeNodeLogsModal() {
  document.getElementById('node-logs-modal-overlay').classList.remove('show');
}

async function loadNodeLogEnvOptions() {
  const sel = document.getElementById('node-log-env');
  if (!sel) return;
  try {
    const envs = await api('/api/env-configs');
    const list = Array.isArray(envs) ? envs : [];
    sel.innerHTML = '<option value="">环境：全部</option>' + list.map(e =>
      '<option value="' + esc(e.env_name || e.name || e) + '">' + esc(e.env_name || e.name || e) + '</option>'
    ).join('');
    sel.value = getActivityListEnv();
  } catch (e) { /* 环境列表可选，失败不影响日志查询 */ }
}

async function loadNodeLogs() {
  const box = document.getElementById('node-log-list');
  if (!_nodeLogNodeId) return;
  const p = getProject();
  if (!p) { showToast('请先选择项目', 'error'); return; }
  const params = new URLSearchParams();
  const lv = document.getElementById('node-log-level').value;
  const trace = document.getElementById('node-log-trace-id').value.trim();
  const env = document.getElementById('node-log-env') ? document.getElementById('node-log-env').value.trim() : '';
  if (lv) params.set('level', lv);
  if (trace) params.set('trace_id', trace);
  if (env) params.set('env', env);
  params.set('page', String(_nodeLogPage));
  params.set('page_size', String(_nodeLogPageSize));
  box.innerHTML = '<div class="log-empty">加载中…</div>';
  try {
    const resp = await api('/api/nodes/' + encodeURIComponent(_nodeLogNodeId) + '/logs?' + params.toString());
    const data = (resp && resp.list) || [];
    const total = (resp && typeof resp.total === 'number') ? resp.total : data.length;
    if (data.length === 0) {
      box.innerHTML = '<div class="log-empty">暂无运行日志</div>';
      renderNodeLogPager(total);
      return;
    }
    box.innerHTML = data.map(renderNodeLogItemWithActs).join('');
    renderNodeLogPager(total);
  } catch (e) {
    box.innerHTML = '<div class="log-empty">加载日志失败: ' + esc(e.message) + '</div>';
  }
}

function renderNodeLogPager(total) {
  const pager = document.getElementById('node-log-pager');
  const info = document.getElementById('node-log-page-info');
  const prev = document.getElementById('node-log-prev');
  const next = document.getElementById('node-log-next');
  const totalPages = Math.max(1, Math.ceil(total / _nodeLogPageSize));
  if (total <= 0) { pager.style.display = 'none'; return; }
  pager.style.display = 'flex';
  info.textContent = '第 ' + _nodeLogPage + ' / ' + totalPages + ' 页（共 ' + total + ' 条）';
  prev.disabled = _nodeLogPage <= 1;
  next.disabled = _nodeLogPage >= totalPages;
}

function changeNodeLogPage(delta) {
  const newPage = _nodeLogPage + delta;
  if (newPage < 1) return;
  _nodeLogPage = newPage;
  loadNodeLogs();
}

function clearNodeLogFilters() {
  document.getElementById('node-log-level').value = '';
  document.getElementById('node-log-trace-id').value = '';
  const envSel = document.getElementById('node-log-env');
  if (envSel) envSel.value = getActivityListEnv();
  _nodeLogPage = 1;
}

function renderNodeLogItem(r) {
  const level = (r.level === 'error') ? 'error' : 'info';
  const t = r.timestamp ? new Date(r.timestamp * 1000).toLocaleString() : '';
  const eventLabel = {
    request: '入参', response: '返回值', fail: '失败', start: '开始', end: '结束',
  }[r.event_id] || r.event_id;
  // node 耗时：duration_ms 存在时格式化展示（end 事件才有实际耗时）
  let durHtml = '';
  if (typeof r.duration_ms === 'number' && r.duration_ms > 0) {
    const dur = r.duration_ms >= 1000
      ? (r.duration_ms / 1000).toFixed(2) + ' s'
      : r.duration_ms + ' ms';
    durHtml = '<span class="log-meta log-duration">耗时=' + esc(dur) + '</span>';
  }
  const head = [
    '<span class="log-level ' + level + '">' + esc(r.level || 'info') + '</span>',
    '<span class="log-event">' + esc(eventLabel) + '</span>',
    '<span class="log-meta">' + esc(t) + '</span>',
    durHtml,
    (r.node_name ? '<span class="log-meta log-node-name">node=' + esc(r.node_name) + '</span>' : ''),
    (r.node_id ? '<span class="log-meta log-node-id">nodeId=' + esc(r.node_id) + '</span>' : ''),
    nodeLogChainTag(r),
    (r.env ? '<span class="log-meta">env=' + esc(r.env) + '</span>' : ''),
    (r.trace_id ? '<span class="log-meta">trace_id=' + esc(r.trace_id) + '</span>' : ''),
    (r.relation_type ? '<span class="log-meta log-relation">relation=' + esc(r.relation_type) + '</span>' : ''),
  ].join(' ');
  // 始终展示「参数 (arguments)」与「返回值 (result)」：优先取表里的 arguments 字段，
  // 回退到 payload.arguments（兼容旧数据未填 arguments 列的情况）。
  let body = '';
  let args = null;
  if (r.arguments !== undefined && r.arguments !== null && r.arguments !== '') {
    args = r.arguments;
  } else {
    // 回退：从 payload.arguments 取（payload 可能是字符串或对象）
    try {
      const p = (typeof r.payload === 'string') ? JSON.parse(r.payload) : r.payload;
      if (p && typeof p === 'object' && p.arguments !== undefined) {
        args = p.arguments;
      } else if (p && typeof p === 'object') {
        args = p; // 兼容没有 arguments 包裹的情况
      }
    } catch (e) { args = r.payload; }
  }
  if (args !== null && args !== undefined && args !== '') {
    let pretty = args;
    try { pretty = JSON.stringify(typeof args === 'string' ? JSON.parse(args) : args, null, 2); } catch (e) {}
    if (pretty && pretty !== '{}') {
      body += '<div class="log-json"><div class="log-json-label">参数 (arguments)</div><pre>' + esc(pretty) + '</pre></div>';
    }
  }
  // 返回值（result）：任意事件类型都展示，便于对照入参与返回值
  const raw = r.result;
  if (raw) {
    let pretty = raw;
    try { pretty = JSON.stringify(JSON.parse(typeof raw === 'string' ? raw : JSON.stringify(raw)), null, 2); } catch (e) {}
    body += '<div class="log-json"><div class="log-json-label">返回值 (result)</div><pre>' + esc(pretty) + '</pre></div>';
  }
  if (r.error_msg) {
    body += '<div class="log-error-msg">错误: ' + esc(r.error_msg) + '</div>';
  }
  return '<div class="log-item ' + level + '"><div class="log-head">' + head + '</div>' + body + '</div>';
}

// 普通 node 运行日志列表项：在 renderNodeLogItem 基础上附加"查看该 node 下 Activities"下钻按钮。
// 点击后用本项自身的 trace_id + node_span_id（即 node 日志的 span_id）查询该 node 关联的 activity 日志并内联展开。
// 子链里节点的日志标签：所在子链 + 调用路径（调用该子链的子链节点实例 ID，嵌套用 / 拼接），主链节点不显示
function nodeLogChainTag(r) {
  if (!r || !r.call_path) return '';
  const title = '所在子链 ' + (r.chain_id || '') + '，调用路径 ' + r.call_path;
  return '<span class="log-meta log-sub-chain" title="' + esc(title) + '" style="background:#fef3c7;color:#92400e;border-radius:4px;padding:0 6px">子链 ' +
    esc(r.chain_id || '') + ' · ' + esc(r.call_path) + '</span>';
}

function renderNodeLogItemWithActs(r) {
  const base = renderNodeLogItem(r);
  const uid = 'nl-' + (r.trace_id || '') + '-' + (r.node_id || '') + '-' + (r.event_id || '') + '-' + (r.timestamp || '');
  const btn = '<button class="mini-btn" data-node-acts-toggle="1" data-uid="' + esc(uid) + '" data-node-id="' + esc(r.node_id || '') + '" data-node-span-id="' + esc(r.span_id || '') + '" data-trace-id="' + esc(r.trace_id || '') + '">查看该 node 下 Activities ▾</button>';
  const actsBox = '<div class="trace-node-acts" id="' + esc(uid) + '" style="display:none"></div>';
  return base.replace('</div>', btn + actsBox + '</div>');
}

async function loadActivityLogs() {
  const box = document.getElementById('log-list');
  if (!_logActivityId) return;
  const p = getProject();
  if (!p) { showToast('请先选择项目', 'error'); return; }
  const params = new URLSearchParams();
  const kw = document.getElementById('log-keyword').value.trim();
  const lv = document.getElementById('log-level').value;
  const ev = document.getElementById('log-event-id').value.trim();
  const rootChain = document.getElementById('log-root-chain-id').value.trim();
  const trace = document.getElementById('log-trace-id').value.trim();
  const span = document.getElementById('log-span-id').value.trim();
  const env = document.getElementById('log-env') ? document.getElementById('log-env').value.trim() : '';
  if (kw) params.set('keyword', kw);
  if (lv) params.set('level', lv);
  if (ev) params.set('event_id', ev);
  if (rootChain) params.set('root_chain_id', rootChain);
  if (trace) params.set('trace_id', trace);
  if (span) params.set('span_id', span);
  if (env) params.set('env', env);
  params.set('page', String(_logPage));
  params.set('page_size', String(_logPageSize));
  box.innerHTML = '<div class="log-empty">加载中…</div>';
  try {
    const resp = await api('/api/activities/' + encodeURIComponent(_logActivityId) + '/logs?' + params.toString());
    const data = Array.isArray(resp) ? resp : (resp && resp.list) || [];
    const total = (resp && typeof resp.total === 'number') ? resp.total : data.length;
    if (data.length === 0) {
      box.innerHTML = '<div class="log-empty">暂无执行日志</div>';
      renderLogPager(total);
      return;
    }
    box.innerHTML = data.map(renderLogItem).join('');
    renderLogPager(total);
  } catch (e) {
    box.innerHTML = '<div class="log-empty">加载日志失败: ' + esc(e.message) + '</div>';
  }
}

function renderLogPager(total) {
  const pager = document.getElementById('log-pager');
  const info = document.getElementById('log-page-info');
  const prev = document.getElementById('log-prev');
  const next = document.getElementById('log-next');
  const totalPages = Math.max(1, Math.ceil(total / _logPageSize));
  if (total <= 0) {
    pager.style.display = 'none';
    return;
  }
  pager.style.display = 'flex';
  info.textContent = '第 ' + _logPage + ' / ' + totalPages + ' 页（共 ' + total + ' 条）';
  prev.disabled = _logPage <= 1;
  next.disabled = _logPage >= totalPages;
}

function changeLogPage(delta) {
  const newPage = _logPage + delta;
  if (newPage < 1) return;
  _logPage = newPage;
  loadActivityLogs();
}

function renderLogItem(r) {
  const level = (r.level === 'error') ? 'error' : 'info';
  const t = r.timestamp ? new Date(r.timestamp * 1000).toLocaleString() : '';
  const left = [];
  left.push('<span class="log-level ' + level + '">' + esc(r.level || 'info') + '</span>');
  left.push('<span class="log-meta">' + esc(t) + '</span>');
  if (r.env) left.push('<span class="log-meta">env=' + esc(r.env) + '</span>');
  if (r.act_namespace) left.push('<span class="log-meta">ns=' + esc(r.act_namespace) + '</span>');
  if (r.act_name) left.push('<span class="log-meta">act=' + esc(r.act_name) + '</span>');
  if (r.trace_id) left.push('<span class="log-meta">trace_id=' + esc(r.trace_id) + '</span>');
  if (r.span_id) left.push('<span class="log-meta">span_id=' + esc(r.span_id) + '</span>');
  if (r.root_chain_id) left.push('<span class="log-meta">rc=' + esc(r.root_chain_id) + '</span>');
  if (typeof r.duration_ms === 'number') left.push('<span class="log-meta">' + r.duration_ms + 'ms</span>');
  const segs = [];
  segs.push('<div class="log-item-head">' + left.join('') + '</div>');
  const payloadStr = toStr(r.payload);
  const resultStr = toStr(r.result);
  const errStr = toStr(r.error_msg);
  if (errStr) segs.push('<div class="log-json" style="background:#3f1d1d;color:#fecaca">' + esc(errStr) + '</div>');
  if (payloadStr) segs.push('<div class="log-json"><span class="k">payload:</span> ' + esc(trunc(payloadStr, 2000000)) + '</div>');
  if (resultStr) segs.push('<div class="log-json"><span class="k">result:</span> ' + esc(trunc(resultStr, 2000000)) + '</div>');
  const attrStr = toStr(r.attributes);
  if (attrStr) segs.push('<div class="log-json" style="background:#1e293b;color:#cbd5e1"><span class="k">attributes:</span> ' + esc(trunc(attrStr, 2000000)) + '</div>');
  return '<div class="log-item">' + segs.join('') + '</div>';
}

// 将后端日志字段（可能是字符串、JSON 对象/数组或数字）统一转为可显示的字符串。
function toStr(v) {
  if (v === null || v === undefined) return '';
  if (typeof v === 'string') return v;
  try { return JSON.stringify(v); } catch (e) { return String(v); }
}

// ============================================================
// 所有日志列表页（Logs Tab）
// ============================================================
let _allLogType = 'node';   // 当前日志类型：node | activity
let _allLogPage = 1;
const _allLogPageSize = 20; // 默认每页 20 条

// 打开日志页：初始化环境下拉与 Root Chain 下拉后加载最新日志
async function openAllLogsTab() {
  await loadAllLogEnvOptions();
  await loadAllLogRootChainOptions();
  await loadAllLogs();
}

// 切换日志类型（Node 运行日志 / Activity 执行日志）
function switchAllLogType() {
  const sel = document.getElementById('all-log-type');
  if (!sel) return;
  _allLogType = sel.value;
  _allLogPage = 1;
  loadAllLogs();
}

// 加载日志页环境下拉选项
async function loadAllLogEnvOptions() {
  const sel = document.getElementById('all-log-env');
  if (!sel) return;
  try {
    const envs = await api('/api/env-configs');
    const list = Array.isArray(envs) ? envs : [];
    const cur = sel.value;
    sel.innerHTML = '<option value="">全部环境</option>' + list.map(e =>
      '<option value="' + esc(e.env_name || e.name || e) + '">' + esc(e.env_name || e.name || e) + '</option>'
    ).join('');
    if (cur) sel.value = cur;
  } catch (e) { /* 环境列表可选，失败不影响日志查询 */ }
}

// 加载所有日志列表（默认最新 20 条，按时间倒序）
async function loadAllLogs() {
  const box = document.getElementById('all-log-list');
  const p = getProject();
  if (!p) { showToast('请先选择项目', 'error'); return; }
  if (!box) return;

  const params = new URLSearchParams();
  const lv = document.getElementById('all-log-level') ? document.getElementById('all-log-level').value : '';
  const env = document.getElementById('all-log-env') ? document.getElementById('all-log-env').value.trim() : '';
  const trace = document.getElementById('all-log-trace-id') ? document.getElementById('all-log-trace-id').value.trim() : '';
  const rootChainId = document.getElementById('all-log-root-chain-id') ? document.getElementById('all-log-root-chain-id').value.trim() : '';
  const chainId = document.getElementById('all-log-chain-id') ? document.getElementById('all-log-chain-id').value.trim() : '';
  const payload = document.getElementById('all-log-payload') ? document.getElementById('all-log-payload').value.trim() : '';
  if (lv) params.set('level', lv);
  if (env) params.set('env', env);
  if (trace) params.set('trace_id', trace);
  if (rootChainId) params.set('root_chain_id', rootChainId);
  if (chainId && _allLogType !== 'activity') params.set('chain_id', chainId);
  if (payload) params.set('keyword', payload);
  params.set('page', String(_allLogPage));
  params.set('page_size', String(_allLogPageSize));

  box.innerHTML = '<div class="log-empty">加载中…</div>';
  try {
    const endpoint = _allLogType === 'activity' ? '/api/activity-logs' : '/api/node-logs';
    const resp = await api(endpoint + '?' + params.toString());
    const data = (resp && resp.list) || [];
    const total = (resp && typeof resp.total === 'number') ? resp.total : data.length;
    if (data.length === 0) {
      box.innerHTML = '<div class="log-empty">暂无日志</div>';
      renderAllLogPager(total);
      return;
    }
    box.innerHTML = data.map(_allLogType === 'activity' ? renderLogItem : renderNodeLogItemWithActs).join('');
    renderAllLogPager(total);
  } catch (e) {
    box.innerHTML = '<div class="log-empty">加载日志失败: ' + esc(e.message) + '</div>';
  }
}

// 渲染日志页分页器
function renderAllLogPager(total) {
  const pager = document.getElementById('all-log-pager');
  const info = document.getElementById('all-log-page-info');
  const prev = document.getElementById('all-log-prev');
  const next = document.getElementById('all-log-next');
  const totalPages = Math.max(1, Math.ceil(total / _allLogPageSize));
  if (total <= 0) { pager.style.display = 'none'; return; }
  pager.style.display = 'flex';
  info.textContent = '第 ' + _allLogPage + ' / ' + totalPages + ' 页（共 ' + total + ' 条）';
  prev.disabled = _allLogPage <= 1;
  next.disabled = _allLogPage >= totalPages;
}

// 日志页翻页
function changeAllLogPage(delta) {
  const newPage = _allLogPage + delta;
  if (newPage < 1) return;
  _allLogPage = newPage;
  loadAllLogs();
}

// 重置日志页筛选条件
function clearAllLogFilters() {
  const lv = document.getElementById('all-log-level');
  const env = document.getElementById('all-log-env');
  const trace = document.getElementById('all-log-trace-id');
  const rc = document.getElementById('all-log-root-chain-id');
  const chain = document.getElementById('all-log-chain-id');
  const payload = document.getElementById('all-log-payload');
  if (lv) lv.value = '';
  if (env) env.value = '';
  if (trace) trace.value = '';
  if (rc) rc.value = '';
  if (chain) chain.value = '';
  if (payload) payload.value = '';
  _allLogPage = 1;
  loadAllLogs();
}

// 加载日志页 Root Chain 下拉选项（含"全部 Root Chain"表示忽略该过滤）
async function loadAllLogRootChainOptions() {
  const sel = document.getElementById('all-log-root-chain-id');
  if (!sel) return;
  const cur = sel.value;
  try {
    const p = getProject();
    if (!p) return;
    const list = await api('/api/root-chains?project=' + encodeURIComponent(p));
    const arr = Array.isArray(list) ? list : (list && list.list) || [];
    sel.innerHTML = '<option value="">全部 Root Chain</option>' + arr.map(c =>
      '<option value="' + esc(c.chain_id) + '">' + esc(c.name || c.chain_id) + ' · ' + esc(c.chain_id) + '</option>'
    ).join('');
    if (cur) sel.value = cur;
  } catch (e) { /* 选项可选，失败不影响日志查询 */ }
  loadAllLogChainOptions();
}

// 加载日志页「所属链」下拉选项：项目下的子链（选中后只看该子链里节点的 Node 日志）
async function loadAllLogChainOptions() {
  const sel = document.getElementById('all-log-chain-id');
  if (!sel) return;
  const cur = sel.value;
  try {
    const p = getProject();
    if (!p) return;
    const list = await api('/api/sub-chains?project=' + encodeURIComponent(p));
    const arr = Array.isArray(list) ? list : (list && list.list) || [];
    sel.innerHTML = '<option value="">全部所属链</option>' + arr.map(c =>
      '<option value="' + esc(c.chain_id) + '">子链 ' + esc(c.name || c.chain_id) + ' · ' + esc(c.chain_id) + '</option>'
    ).join('');
    if (cur) sel.value = cur;
  } catch (e) { /* 选项可选，失败不影响日志查询 */ }
}

// ===================== 统计页 =====================
// 加载「统计」页：概览卡片、根链执行统计、错误排行（/api/stats/overview）与 Node 每日访问量柱状图。
// 概览后端缓存 1 分钟；force=true（点「刷新」）时跳过缓存重新统计。
async function loadStatistics(force) {
  const curProject = getProject();
  const cards = document.getElementById('stat-cards');
  if (!cards) return;
  if (!curProject) {
    cards.innerHTML = '<div class="log-empty">请先在右上角选择一个项目</div>';
    return;
  }
  await loadStatFilterOptions();
  await Promise.all([loadStatOverview(force), loadStatChart()]);
}

// 重建统计页的环境与 Node 下拉（保留当前选中值）。
async function loadStatFilterOptions() {
  // 确保 Node 缓存已加载（首次进入统计页时可能尚未访问 Nodes 页，下拉才会只有「全部 Node」）
  if (!window._nodesForEdit || window._nodesForEdit.length === 0) {
    try {
      const ns = await api('/api/nodes');
      if (ns && ns.length) window._nodesForEdit = ns;
    } catch (e) { /* 忽略 */ }
  }
  const statNode = document.getElementById('stat-node');
  if (statNode) {
    const sel = statNode.value;
    statNode.innerHTML = '<option value="">全部 Node</option>';
    // Node 列表即节点模板；日志里的实例（模板ID__后缀）由后端按模板归并
    (window._nodesForEdit || []).forEach(n => {
      if (n && n.node_id) {
        const o = document.createElement('option');
        o.value = n.node_id;
        o.textContent = (n.name || n.node_id) + ' (' + n.node_id + ')';
        statNode.appendChild(o);
      }
    });
    if (sel) statNode.value = sel;
  }
  const statEnv = document.getElementById('stat-env');
  if (statEnv) {
    let envList = [];
    try {
      envList = await api('/api/env-configs');
    } catch (e) { /* 忽略：env 可选 */ }
    const sel = statEnv.value;
    statEnv.innerHTML = '<option value="">全部环境</option>';
    (envList || []).forEach(e => {
      const v = e.env_name || e.name || e.env || '';
      if (!v) return;
      const o = document.createElement('option');
      o.value = v;
      o.textContent = v;
      statEnv.appendChild(o);
    });
    if (sel) statEnv.value = sel;
  }
}

// 统计页公共查询参数：环境 + 天数。
function statQueryParams() {
  const params = new URLSearchParams();
  const env = document.getElementById('stat-env');
  const days = document.getElementById('stat-days');
  if (env && env.value) params.set('env', env.value);
  if (days && days.value) params.set('days', days.value);
  return params;
}

// 毫秒耗时转可读文本。
function fmtStatMs(ms) {
  ms = Number(ms) || 0;
  if (ms < 1000) return ms + ' ms';
  const s = ms / 1000;
  if (s < 60) return s.toFixed(1) + ' 秒';
  if (s < 3600) return (s / 60).toFixed(1) + ' 分钟';
  if (s < 86400) return (s / 3600).toFixed(1) + ' 小时';
  return (s / 86400).toFixed(1) + ' 天';
}

// 比例转百分比文本。
function fmtStatRate(r) {
  return ((Number(r) || 0) * 100).toFixed(1) + '%';
}

// 毫秒时间戳转 MM-DD HH:mm:ss。
function fmtStatTime(ms) {
  if (!ms) return '-';
  const d = new Date(ms);
  const p = n => String(n).padStart(2, '0');
  return p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
}

let _statOverview = null;

// 加载并渲染概览卡片、根链统计、错误排行。
async function loadStatOverview(force) {
  const cards = document.getElementById('stat-cards');
  const chainTable = document.getElementById('stat-chain-table');
  const errTable = document.getElementById('stat-error-table');
  const updated = document.getElementById('stat-updated');
  cards.innerHTML = '<div class="log-empty">加载中…</div>';
  chainTable.innerHTML = '';
  errTable.innerHTML = '';
  let data;
  try {
    const params = statQueryParams();
    if (force) params.set('refresh', '1');
    data = await api('/api/stats/overview?' + params.toString());
  } catch (e) {
    cards.innerHTML = '<div class="log-empty">加载失败：' + escHtml(String((e && e.message) || e)) + '</div>';
    return;
  }
  _statOverview = data || {};
  if (updated) updated.textContent = '更新于 ' + fmtStatTime(Date.now()) + '（切换条件时最多复用 1 分钟内的结果，点刷新取最新）';
  renderStatCards(cards, _statOverview.summary || {});
  renderStatChainTable(chainTable, _statOverview.root_chains || []);
  renderStatErrorTable(errTable, _statOverview.errors || []);
}

// 渲染概览卡片。
function renderStatCards(container, s) {
  const card = (label, value, sub, bad) =>
    '<div class="stat-card"><div class="stat-card-label">' + escHtml(label) + '</div>' +
    '<div class="stat-card-value' + (bad ? ' bad' : '') + '">' + escHtml(String(value)) + '</div>' +
    (sub ? '<div class="stat-card-sub">' + escHtml(sub) + '</div>' : '') + '</div>';
  const exec = s.executions || 0;
  const failed = s.failed_executions || 0;
  let html = '';
  html += card('根链执行次数', exec.toLocaleString(), 'trace_id + 根链 + 10 分钟窗口');
  html += card('失败执行', failed.toLocaleString(), '成功率 ' + (exec ? fmtStatRate(s.success_rate) : '-'), failed > 0);
  html += card('Node 执行 / 报错', (s.node_total || 0).toLocaleString() + ' / ' + (s.node_errors || 0).toLocaleString(), '含节点单测等非根链执行', (s.node_errors || 0) > 0);
  html += card('平均耗时', exec ? fmtStatMs(s.avg_ms) : '-', '根链端到端');
  html += card('P95 耗时', exec ? '≈ ' + fmtStatMs(s.p95_ms) : '-', '95% 的执行不超过该值');
  if (s.activity_published !== undefined && s.activity_published !== null) {
    const off = (s.activity_published || 0) - (s.activity_online || 0);
    html += card('Activity 在线 / 已发布', (s.activity_online || 0) + ' / ' + s.activity_published, off > 0 ? off + ' 个当前无心跳' : '全部在线', off > 0);
  } else {
    html += card('Activity 在线 / 已发布', '-', '选择具体环境后显示');
  }
  container.innerHTML = html;
}

// 失败率文本：大于 0 标红。
function statRateCell(stat) {
  if (!stat.executions) return '-';
  const cls = stat.failed > 0 ? ' class="stat-rate-bad"' : '';
  return '<span' + cls + '>' + fmtStatRate(stat.fail_rate) + '</span>';
}

// 渲染根链执行统计表（点击行展开各发布版本）。
function renderStatChainTable(table, list) {
  let html = '<thead><tr><th>根链</th><th class="num">执行次数</th><th class="num">失败</th><th class="num">失败率</th>' +
    '<th class="num">平均耗时</th><th class="num">P95 耗时</th><th class="num">最近执行</th><th>日志</th></tr></thead><tbody>';
  if (!list.length) {
    html += '<tr><td colspan="8"><div class="log-empty">所选条件下暂无根链执行记录</div></td></tr>';
  }
  list.forEach((c, i) => {
    const vers = c.versions || [];
    html += '<tr class="stat-chain-row" onclick="toggleStatChainVersions(' + i + ')">' +
      '<td><span id="stat-chain-arrow-' + i + '" style="display:inline-block;width:14px;color:var(--text-muted)">' + (vers.length ? '▸' : '') + '</span>' +
      escHtml(c.name || c.chain_id) + ' <span style="color:var(--text-muted);font-size:.75rem">' + escHtml(c.chain_id) + '</span></td>' +
      '<td class="num">' + (c.executions || 0).toLocaleString() + '</td>' +
      '<td class="num">' + (c.failed || 0).toLocaleString() + '</td>' +
      '<td class="num">' + statRateCell(c) + '</td>' +
      '<td class="num">' + fmtStatMs(c.avg_ms) + '</td>' +
      '<td class="num">' + fmtStatMs(c.p95_ms) + '</td>' +
      '<td class="num">' + fmtStatTime(c.last_at) + '</td>' +
      '<td><button class="btn btn-sm btn-outline" onclick="event.stopPropagation();gotoLogsFromStats({rootChainId:\'' + esc(c.chain_id) + '\'})">查看</button></td></tr>';
    vers.forEach(v => {
      html += '<tr class="stat-ver-row stat-ver-of-' + i + '" style="display:none">' +
        '<td>' + (v.version ? 'v' + v.version : '草稿执行') + '</td>' +
        '<td class="num">' + (v.executions || 0).toLocaleString() + '</td>' +
        '<td class="num">' + (v.failed || 0).toLocaleString() + '</td>' +
        '<td class="num">' + statRateCell(v) + '</td>' +
        '<td class="num">' + fmtStatMs(v.avg_ms) + '</td>' +
        '<td class="num">' + fmtStatMs(v.p95_ms) + '</td>' +
        '<td class="num">' + fmtStatTime(v.last_at) + '</td><td></td></tr>';
    });
  });
  html += '</tbody>';
  table.innerHTML = html;
}

// 展开 / 收起某根链的版本明细行。
function toggleStatChainVersions(i) {
  const rows = document.querySelectorAll('.stat-ver-of-' + i);
  if (!rows.length) return;
  const show = rows[0].style.display === 'none';
  rows.forEach(r => { r.style.display = show ? '' : 'none'; });
  const arrow = document.getElementById('stat-chain-arrow-' + i);
  if (arrow) arrow.textContent = show ? '▾' : '▸';
}

// 渲染错误排行表。
function renderStatErrorTable(table, list) {
  let html = '<thead><tr><th class="num">#</th><th>节点</th><th>错误信息</th><th class="num">次数</th><th class="num">最近出现</th><th>日志</th></tr></thead><tbody>';
  if (!list.length) {
    html += '<tr><td colspan="6"><div class="log-empty">所选条件下没有报错</div></td></tr>';
  }
  list.forEach((e, i) => {
    html += '<tr><td class="num">' + (i + 1) + '</td>' +
      '<td>' + escHtml(e.node_name || e.node_id) + '<div style="color:var(--text-muted);font-size:.72rem">' + escHtml(e.node_id) + '</div></td>' +
      '<td class="stat-err-msg">' + escHtml(e.message) + '</td>' +
      '<td class="num">' + (e.count || 0).toLocaleString() + '</td>' +
      '<td class="num">' + fmtStatTime(e.last_at) + '</td>' +
      '<td>' + (e.trace_id ? '<button class="btn btn-sm btn-outline" onclick="gotoLogsFromStats({traceId:\'' + esc(e.trace_id) + '\',level:\'error\'})">样例</button>' : '') + '</td></tr>';
  });
  html += '</tbody>';
  table.innerHTML = html;
}

// 从统计页跳到日志页并带上筛选条件（环境沿用统计页所选）。opts: rootChainId / traceId / level。
// 日志页切换时会重建下拉并保留当前值，这里先塞入对应选项，避免值因选项尚未加载而丢失。
function gotoLogsFromStats(opts) {
  opts = opts || {};
  const presetSelect = (id, value) => {
    const sel = document.getElementById(id);
    if (!sel) return;
    if (value && !Array.from(sel.options).some(o => o.value === value)) {
      const o = document.createElement('option');
      o.value = value;
      o.textContent = value;
      sel.appendChild(o);
    }
    sel.value = value || '';
  };
  const typeSel = document.getElementById('all-log-type');
  if (typeSel) typeSel.value = 'node';
  _allLogType = 'node';
  _allLogPage = 1;
  const statEnv = document.getElementById('stat-env');
  presetSelect('all-log-env', statEnv ? statEnv.value : '');
  presetSelect('all-log-level', opts.level || '');
  presetSelect('all-log-root-chain-id', opts.rootChainId || '');
  const trace = document.getElementById('all-log-trace-id');
  if (trace) trace.value = opts.traceId || '';
  const payload = document.getElementById('all-log-payload');
  if (payload) payload.value = '';
  switchTab('logs');
}

// 加载 Node 每日访问量柱状图（只受 Node、环境、天数筛选影响）。
async function loadStatChart() {
  const chart = document.getElementById('stat-chart');
  if (!chart) return;
  const statNode = document.getElementById('stat-node');
  const params = statQueryParams();
  if (statNode && statNode.value) params.set('node_id', statNode.value);
  chart.innerHTML = '<div class="log-empty">加载中…</div>';
  let data;
  try {
    data = await api('/api/node-logs/stats?' + params.toString());
  } catch (e) {
    chart.innerHTML = '<div class="log-empty">加载失败：' + escHtml(String((e && e.message) || e)) + '</div>';
    return;
  }
  renderStatChart(chart, (data && data.list) || []);
}

// 纯 CSS 柱状图渲染：横轴为日期，每日期两根柱（访问量蓝 / 错误量红），悬停显示当天明细。
function renderStatChart(container, list) {
  if (!list.length) {
    container.innerHTML = '<div class="log-empty">所选条件下暂无统计数据</div>';
    return;
  }
  // 按日期聚合（同一天多个 node / 多个实例求和）
  const byDate = {};
  const nodeIds = {};
  list.forEach(s => {
    const d = String(s.date || '').slice(0, 10);
    if (!byDate[d]) byDate[d] = { date: d, total: 0, errors: 0 };
    byDate[d].total += (s.total || 0);
    byDate[d].errors += (s.errors || 0);
    nodeIds[s.node_id] = true;
  });
  const dates = Object.keys(byDate).sort();
  let maxV = 1;
  dates.forEach(d => { if (byDate[d].total > maxV) maxV = byDate[d].total; });
  // 日期多时隔几个显示一个刻度，柱顶数字也只在 31 天以内显示，避免文字挤在一起
  const labelStep = Math.max(1, Math.ceil(dates.length / 15));
  const showValues = dates.length <= 31;
  const h = 200;
  let html = '<div style="display:flex;align-items:flex-end;gap:' + (dates.length > 31 ? 4 : 10) + 'px;min-height:' + (h + 40) + 'px;padding:14px 6px 6px;border-bottom:1px solid var(--border)">';
  dates.forEach((d, i) => {
    const item = byDate[d];
    const totalH = Math.max(2, Math.round((item.total / maxV) * h));
    const errH = Math.max(0, Math.round((item.errors / maxV) * h));
    const tip = d + '\n访问量: ' + item.total + '\n错误量: ' + item.errors;
    const barW = dates.length > 31 ? 6 : 14;
    html += '<div title="' + esc(tip) + '" style="display:flex;flex-direction:column;align-items:center;justify-content:flex-end;flex:1 1 ' + (barW * 2 + 6) + 'px;min-width:' + (barW * 2 + 4) + 'px">';
    html += '<div style="font-size:.68rem;color:var(--text-muted);margin-bottom:3px;min-height:1em">' + (showValues ? item.total : '') + '</div>';
    html += '<div style="display:flex;align-items:flex-end;height:' + h + 'px;width:100%;justify-content:center;gap:2px">';
    html += '<div style="width:' + barW + 'px;background:#3b82f6;border-radius:3px 3px 0 0;height:' + totalH + 'px"></div>';
    html += '<div style="width:' + barW + 'px;background:#ef4444;border-radius:3px 3px 0 0;height:' + errH + 'px"></div>';
    html += '</div>';
    html += '<div style="font-size:.68rem;color:var(--text-muted);margin-top:5px;white-space:nowrap;min-height:1em">' + (i % labelStep === 0 ? escHtml(d.slice(5)) : '') + '</div>';
    html += '</div>';
  });
  html += '</div>';
  html += '<div style="font-size:.75rem;color:var(--text-muted);margin-top:8px">共 ' + Object.keys(nodeIds).length +
    ' 个节点实例有记录，按天汇总；鼠标悬停柱子查看当天明细。</div>';
  container.innerHTML = html;
}

// ============================================================
// 测试单个 Activity（MQ 分布式执行）
// ============================================================

let _testActivityParamsMap = {};

// 打开测试弹窗：加载环境列表 + activity 参数定义（Arguments 中的 BindConfig 数组）
async function openTestActivityModal(activityId) {
  const p = getProject();
  if (!p) { showToast('请先选择项目', 'error'); return; }
  if (!activityId) return;
  document.getElementById('test-activity-id').value = activityId;
  document.getElementById('test-activity-modal-title').textContent = '测试 Activity: ' + activityId;
  // 显示名称
  const taNameEl = document.getElementById('test-activity-name');
  if (taNameEl) taNameEl.textContent = '';
  const taDescEl = document.getElementById('test-activity-desc');
  if (taDescEl) { taDescEl.textContent = ''; taDescEl.style.display = 'none'; }
  document.getElementById('test-activity-result').textContent = '点击"执行测试"后显示结果';
  document.getElementById('test-activity-save').value = 'true';

  try {
    // 加载环境列表
    const envs = await api('/api/env-configs');
    const envSel = document.getElementById('test-activity-env');
    envSel.innerHTML = '<option value="">-- 选择环境 --</option>';
    (envs || []).forEach(e => {
      const opt = document.createElement('option');
      opt.value = e.env_name;
      opt.textContent = e.env_name + (e.description ? ' (' + e.description + ')' : '');
      envSel.appendChild(opt);
    });
    // 默认与 Activities 列表所选环境保持一致，其次回退上次选择
    envSel.value = pickActivityModalEnv(envs, false);

    // 加载 activity 详情（参数定义）：Arguments 为 []*param.BindConfig 数组（同 Node.Params）
    const act = await api('/api/activities/' + encodeURIComponent(activityId));
    let allParams = [];
    try {
      const raw = act.arguments;
      allParams = (typeof raw === 'string') ? JSON.parse(raw || '[]') : (raw || []);
    } catch (_) { allParams = []; }
    _testActivityParamsMap = {};
    if (Array.isArray(allParams)) {
      allParams.forEach(p => { if (p && p.key) _testActivityParamsMap[p.key] = p; });
    }
    renderTestActivityParams();

    // 显示 activity 名称
    const taNameEl = document.getElementById('test-activity-name');
    if (taNameEl) taNameEl.textContent = act.name || '';

    // 显示 activity 描述
    const taDescEl = document.getElementById('test-activity-desc');
    if (taDescEl) {
      if (act.description) {
        taDescEl.textContent = act.description;
        taDescEl.style.display = '';
      } else {
        taDescEl.textContent = '';
        taDescEl.style.display = 'none';
      }
    }

    // 根据大类型调整环境要求与提示
    const kind = act.kind || 'redis';
    const envHint = document.getElementById('test-activity-env-hint');
    const kindHint = document.getElementById('test-activity-kind-hint');
    if (kind === 'http') {
      if (envHint) envHint.textContent = '（HTTP 类型可选，仅用于占位符中的环境变量替换）';
      if (kindHint) kindHint.innerHTML = '<div class="empty-state" style="text-align:left;padding:8px 0"><p style="color:#16a34a">当前为 HTTP 直连类型，测试将直接请求配置中的 URL，无需 Redis。环境变量可选，用于替换 URL/Header/Body 中的占位符。</p></div>';
      // http 类型默认不预选环境
      envSel.value = '';
    } else {
      if (envHint) envHint.textContent = '（决定 Redis 等依赖配置，必选）';
      if (kindHint) kindHint.innerHTML = '<div class="empty-state" style="text-align:left;padding:8px 0"><p>当前为 Redis 类型，测试通过依赖 Redis 的 MQ 远程监听执行，需选择环境获取 Redis 连接。</p></div>';
    }
  } catch (e) { showToast('加载测试配置失败: ' + e.message, 'error'); }

  document.getElementById('test-activity-modal-overlay').classList.add('show');
  loadActivityTestRecords(activityId);
}

function closeTestActivityModal() {
  document.getElementById('test-activity-modal-overlay').classList.remove('show');
}

// 渲染参数输入表单（以 map 形式列出 activity 所有参数，标注必传项）
function renderTestActivityParams() {
  const container = document.getElementById('test-activity-params');
  const map = _testActivityParamsMap || {};
  const keys = Object.keys(map);
  if (!keys.length) {
    container.innerHTML = '<div id="test-activity-params-empty" style="text-align:center;padding:16px;color:var(--text-muted);font-size:.85rem">该活动无参数定义</div>';
    return;
  }
  container.innerHTML = '<div style="font-size:.72rem;color:var(--text-muted);margin-bottom:6px">已列出该活动定义的所有参数（<span style="color:#dc2626">*</span> 为必传：在活动定义中勾选了「必传」的参数）</div>' +
    keys.map(key => {
    const bc = map[key];
    const required = (bc.required === true || bc.required === 'true');
    const reqMark = required ? ' <span style="color:#dc2626">*</span>' : '';
    const policyTag = bc.policy ? ' <span style="font-weight:400;color:#6b7280;font-size:.7rem">[' + esc(bc.policy) + ']</span>' : '';
    const ph = bc.value !== undefined && bc.value !== null ? String(bc.value) : '';
    const inputId = 'test-act-param-' + key.replace(/[^\w]/g, '_');
    return `
      <div class="form-row" style="margin-bottom:8px;align-items:flex-end">
        <div class="form-group" style="flex:1">
          <label>${esc(key)}${reqMark}${policyTag}</label>
          <input id="${inputId}" data-key="${esc(key)}" placeholder="${esc(ph)}" value="${esc(ph)}">
        </div>
      </div>`;
  }).join('');
}

function resetTestActivityParams() {
  renderTestActivityParams();
}

function onTestActivityEnvChange() {
  const v = document.getElementById('test-activity-env').value;
  if (v) localStorage.setItem('last_test_env', v);
}

// 收集表单参数（以 key -> value 的 JSON map 结构提交，按参数声明的 type 转换值类型）
function collectTestActivityParams() {
  const map = _testActivityParamsMap || {};
  const out = {};
  Object.keys(map).forEach(key => {
    const bc = map[key];
    const el = document.getElementById('test-act-param-' + key.replace(/[^\w]/g, '_'));
    if (!el) return;
    const raw = el.value.trim();
    // 空值：number/boolean/json 不写入，string 留空串
    if (raw === '') {
      if (bc.type !== 'number' && bc.type !== 'boolean' && bc.type !== 'json' && bc.type !== 'object') {
        out[key] = '';
      }
      return;
    }
    let val = raw;
    if (bc.type === 'number') {
      const n = Number(raw);
      val = isNaN(n) ? raw : n;            // number：转数值，序列化后不带引号
    } else if (bc.type === 'boolean') {
      val = (raw === 'true' || raw === '1'); // boolean：转布尔
    } else if (bc.type === 'json' || bc.type === 'object') {
      try { val = JSON.parse(raw); } catch (_) { val = raw; } // json：解析为对象/数组
    } else if (!bc.type) {
      // 未声明 type：纯数字串智能转为 number，其余保持字符串
      if (!isNaN(raw) && raw !== '' && /^-?\d+(\.\d+)?$/.test(raw)) {
        val = Number(raw);
      }
    }
    // 其余（string 或未声明但非纯数字）：保持原字符串
    out[key] = val;
  });
  return out;
}

async function testActivity() {
  const p = getProject();
  const activityId = document.getElementById('test-activity-id').value;
  const envName = document.getElementById('test-activity-env').value;
  const save = document.getElementById('test-activity-save').value === 'true';
  // redis 类型需要环境（用于 Redis 连接）；http 类型环境可选
  const kindHint = document.getElementById('test-activity-kind-hint');
  const isHTTP = kindHint && kindHint.innerHTML.indexOf('HTTP 直连类型') >= 0;
  if (!isHTTP && !envName) { showToast('请先选择环境', 'error'); return; }

  const inputParams = collectTestActivityParams();
  const resultEl = document.getElementById('test-activity-result');
  resultEl.textContent = '执行中...';

  try {
    const q = '?project=' + encodeURIComponent(p) + (save ? '' : '&save_record=false');
    const resp = await api('/api/activities/' + encodeURIComponent(activityId) + '/test' + q, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ env_name: envName, input_params: inputParams })
    });
    resultEl.textContent = JSON.stringify(resp, null, 2);
    if (resp.status === 'success') {
      showToast('测试成功' + (resp.record_id ? '，记录已保存: ' + resp.record_id : ''), 'success');
    } else {
      showToast('测试失败: ' + (resp.error_msg || ''), 'error');
    }
    loadActivityTestRecords(activityId);
  } catch (e) {
    resultEl.textContent = '请求失败: ' + e.message;
    showToast('测试请求失败: ' + e.message, 'error');
  }
}

async function loadActivityTestRecords(activityId) {
  const p = getProject();
  const box = document.getElementById('test-activity-records');
  try {
    const records = await api('/api/activities/' + encodeURIComponent(activityId) + '/test-records?project=' + encodeURIComponent(p));
    if (!records.length) {
      box.innerHTML = '<div style="text-align:center;padding:12px;color:var(--text-muted);font-size:.82rem">暂无测试记录</div>';
      return;
    }
    box.innerHTML = records.map(r => `
      <div style="border:1px solid var(--border);border-radius:var(--radius);padding:8px;margin-bottom:6px;font-size:.78rem">
        <div style="display:flex;justify-content:space-between;align-items:center">
          <span><b>${esc(r.record_id)}</b> <span class="badge ${r.status==='success'?'badge-on':'badge-off'}">${r.status==='success'?'成功':'失败'}</span> ${r.env_name?('· 环境 '+esc(r.env_name)):''}</span>
          <button class="btn btn-sm btn-danger edit-only" onclick="deleteActivityTestRecord('${esc(r.record_id)}','${esc(activityId)}')">删除</button>
        </div>
        <div style="color:var(--text-muted);margin:4px 0">${esc(r.created_at || '')}</div>
        <div><b>入参:</b> <code style="font-size:.72rem;white-space:pre-wrap;word-break:break-all;overflow-wrap:break-word;display:block">${esc(trunc(r.input_params||'',2000000))}</code></div>
        <div><b>结果:</b> <code style="font-size:.72rem;white-space:pre-wrap;word-break:break-all;overflow-wrap:break-word;display:block">${esc(trunc(r.result||r.error_msg||'',2000000))}</code></div>
      </div>`).join('');
  } catch (e) {
    box.innerHTML = '<div style="text-align:center;padding:12px;color:var(--text-muted);font-size:.82rem">加载记录失败</div>';
  }
}

async function deleteActivityTestRecord(recordId, activityId) {
  if (!confirm('确定删除测试记录 ' + recordId + ' 吗？')) return;
  try {
    await api('/api/activity-test-records/' + encodeURIComponent(recordId) + '?project=' + encodeURIComponent(getProject()), { method: 'DELETE' });
    showToast('记录已删除', 'success');
    loadActivityTestRecords(activityId);
  } catch (e) { showToast('删除失败: ' + e.message, 'error'); }
}

// ============================================================
// Node Params (参数定义)
// ============================================================
let paramSeq = 0;

// 参数「值类型」统一候选列表（7 种），参数定义（addParamRow）与 Activity 参数值绑定（argRenderArgInput）
// 共用同一份，保证两处可选择的转换类型完全一致。顺序：string / int64 / float64 / bool / slice / map / formula。
const ARG_TYPE_OPTIONS = ['string', 'int64', 'float64', 'bool', 'slice', 'map', 'formula'];

function addParamRow(key, label, type, required, defaultValue, description, policy) {
  paramSeq++;
  const container = document.getElementById('node-params-container');
  const emptyEl = document.getElementById('param-rows-empty');
  if (emptyEl) emptyEl.style.display = 'none';

  const row = document.createElement('div');
  row.className = 'param-row';
  row.id = 'param-row-' + paramSeq;
  const reqChecked = required ? 'checked' : '';
  const policyOptions = [
    { v: 'frontend+', t: '用户优先' },
    { v: 'backend+', t: '节点优先' },
    { v: 'backend', t: '节点锁定' },
    { v: 'frontend', t: '用户强制' },
    { v: 'default', t: '仅默认值' },
  ];
  const policyHTML = policyOptions.map(o =>
    `<option value="${o.v}" ${(policy||'frontend+')===o.v?'selected':''}>${o.t}</option>`
  ).join('');
  row.innerHTML = `
    <input class="param-key" placeholder="参数键" value="${esc(key||'')}">
    <input class="param-label" placeholder="显示名" value="${esc(label||'')}">
    <select class="param-type">
      ${ARG_TYPE_OPTIONS.map(t => `<option value="${t}" ${t===type?'selected':''}>${t}</option>`).join('')}
    </select>
    <label class="param-required"><input type="checkbox" ${reqChecked}>必填</label>
    <select class="param-policy">${policyHTML}</select>
    <input class="param-default" placeholder="默认值" value="${esc(defaultValue||'')}">
    <input class="param-desc" placeholder="描述" value="${esc(description||'')}">
    <button class="btn-remove-param" onclick="removeParamRow('param-row-${paramSeq}')" title="删除此参数">&times;</button>
  `;
  container.appendChild(row);
}

// 循环节点：参数定义里自动补一条循环数据 loop_items（slice），编排时在「节点参数配置」里为它配置来源（引用节点 / 调用传入 / 固定配置）。
// 已有同名参数时不重复添加。
function ensureLoopItemsParamRow(type) {
  if (type !== 'custom/Loop') return;
  const exists = Array.from(document.querySelectorAll('#node-params-container .param-row .param-key'))
    .some(el => (el.value || '').trim() === 'loop_items');
  if (exists) return;
  addParamRow('loop_items', '循环数据', 'slice', true, '', '要遍历的数组（或对象，按键遍历），每个元素执行一次循环体；条件循环模式下不需要配置', 'frontend+');
}

function removeParamRow(rowId) {
  const row = document.getElementById(rowId);
  if (row) row.remove();
  const container = document.getElementById('node-params-container');
  if (!container.querySelector('.param-row')) {
    const emptyEl = document.getElementById('param-rows-empty');
    if (emptyEl) emptyEl.style.display = '';
  }
}

function collectParams() {
  const rows = document.querySelectorAll('#node-params-container .param-row');
  const params = [];
  rows.forEach(row => {
    const key = row.querySelector('.param-key').value.trim();
    const label = row.querySelector('.param-label').value.trim();
    const type = row.querySelector('.param-type').value;
    const required = row.querySelector('.param-required input').checked;
    const policy = row.querySelector('.param-policy') ? row.querySelector('.param-policy').value : 'frontend+';
    const value = row.querySelector('.param-default').value.trim();
    const description = row.querySelector('.param-desc').value.trim();
    if (key) {
      params.push({ key, label, type, required, policy, value: value || '', description });
    }
  });
  return params;
}

// 将节点参数定义（params）按类型转换后写入 Configuration 第一层 arguments。
// 对应后端 commnode.CommConfiguration.Arguments（[]*param.BindConfig），
// 每项保留 key / value / type / policy 字段；type 与 outputs 保持一致写入，
// 供后端按类型计算（formula）及前端 checkAllNodesArguments 解析 {{arguments.xxx}} 入参。
function paramsToBindConfigs(params) {
  return (params || [])
    .filter(p => p && p.key)
    .map(p => ({
      key: p.key,
      value: castParamValue(p.value, p.type),
      type: p.type || '',
      policy: p.policy || 'frontend+'
    }));
}

// 按参数类型把默认值字符串转换成对应的 JS 类型；无法转换时原样返回字符串。
function castParamValue(raw, type) {
  const s = raw === undefined || raw === null ? '' : String(raw);
  if (s === '') return '';
  if (type === 'number') {
    const n = Number(s);
    return isNaN(n) ? s : n;
  }
  if (type === 'boolean') {
    if (s === 'true') return true;
    if (s === 'false') return false;
    return s;
  }
  if (type === 'json' || type === 'slice' || type === 'map') {
    try { return JSON.parse(s); } catch (e) { return s; }
  }
  return s;
}

// 保存时把 params / outputs 同步进 Configuration 第一层。
// params  -> configuration.arguments（BindConfig 数组）
// outputs -> configuration.responses（返回值定义数组，原样保留 source/value/ref）
// 对应集合为空时移除该字段，避免残留旧数据。
function syncParamsToConfigArguments(body) {
  if (typeof body.configuration !== 'object' || body.configuration === null) body.configuration = {};
  const args = paramsToBindConfigs(body.params);
  if (args.length > 0) {
    body.configuration.arguments = args;
  } else {
    delete body.configuration.arguments;
  }
  const resp = outputsToResponses(body.outputs);
  if (resp.length > 0) {
    body.configuration.responses = resp;
  } else {
    delete body.configuration.responses;
  }
}

// 将返回值定义（outputs）转换为 Configuration 第一层 responses 数组。
// 保留 key/label/type/source，并按 source 保留对应的取值字段：
//   value    -> value（按 type 转换后的字面量）
//   ref_act  -> ref（{{steps.actId.responses[.field]}} / {{steps.actId.arguments[.key]}}）
//   ref_node -> ref（{{nodeParamKey}}）
function outputsToResponses(outputs) {
  return (outputs || [])
    .filter(o => o && o.key)
    .map(o => {
      // type 写入 BindConfig.Type，供后端按类型计算（float64/formula）。
      const item = { key: o.key, label: o.label || '', type: o.type || '', source: o.source || 'value' };
      if (item.source === 'value') {
        item.value = castParamValue(o.value, o.type);
      } else {
        // ref_act / ref_node：ref 为引用路径，同时把 value 设为与 ref 一致，
        // 便于执行引擎（param.BindConfig）统一从 value 字段读取取值来源。
        item.ref = o.ref || '';
        item.value = o.value || o.ref || '';
      }
      return item;
    });
}

function clearParamRows() {
  const container = document.getElementById('node-params-container');
  container.querySelectorAll('.param-row').forEach(r => r.remove());
  const emptyEl = document.getElementById('param-rows-empty');
  if (emptyEl) emptyEl.style.display = '';
}

// ============================================================
// Node Outputs (返回值定义)
// ============================================================
let outputSeq = 0;

// 返回值类型下拉（与 Activity 返回值设置一致：含"不转换"）
const OUTPUT_TYPE_OPTIONS = ['', 'string', 'int64', 'float64', 'bool', 'formula', 'slice', 'map'];
const OUTPUT_TYPE_LABELS = { '': '不转换', 'string': 'string', 'int64': 'int64', 'float64': 'float64', 'bool': 'bool', 'formula': 'formula', 'slice': 'slice', 'map': 'map' };
function outputTypeHTML(type) {
  return OUTPUT_TYPE_OPTIONS.map(t =>
    '<option value="' + t + '"' + ((type||'') === t ? ' selected' : '') + '>' + OUTPUT_TYPE_LABELS[t] + '</option>'
  ).join('');
}

function addOutputRow(opts) {
  opts = opts || {};
  const key = opts.key || '';
  const label = opts.label || '';
  const type = opts.type || '';
  const source = opts.source || 'value'; // value=手工配置, ref_act=activity选择
  const ref = opts.ref || '';
  const value = opts.value || '';
  outputSeq++;
  const container = document.getElementById('node-outputs-container');
  const emptyEl = document.getElementById('output-rows-empty');
  if (emptyEl) emptyEl.style.display = 'none';

  const row = document.createElement('div');
  row.className = 'param-row';
  row.id = 'output-row-' + outputSeq;
  row.innerHTML = `
    <span class="output-input-wrap" style="flex:1;display:flex;flex-wrap:wrap;gap:6px;align-items:center;min-width:0"></span>
    <button class="btn-remove-param" onclick="removeOutputRow('output-row-${outputSeq}')" title="删除此返回值">&times;</button>
  `;
  container.appendChild(row);
  const wrap = row.querySelector('.output-input-wrap');
  wrap.innerHTML = outputRenderInput({ key: key, label: label, type: type, source: source, ref: ref, value: value });
  // 回显引用联动初值
  if (source === 'ref_act' && ref) {
    const parsed = outputParseRefPath(ref);
    if (!Object.prototype.hasOwnProperty.call(parsed, 'raw')) {
      const prevSel = wrap.querySelector('.arg-ref');
      const typeSel = wrap.querySelector('.arg-reftype');
      const fieldInput = wrap.querySelector('.arg-refield');
      if (prevSel && parsed.refId) prevSel.value = parsed.refId;
      if (typeSel && parsed.refType) typeSel.value = parsed.refType;
      if (fieldInput && parsed.refField) fieldInput.value = parsed.refField;
    }
  }
  if (source === 'ref_node' && ref) {
    const nSel = wrap.querySelector('.output-ref-node');
    if (nSel) nSel.value = ref;
  }
}

function removeOutputRow(rowId) {
  const row = document.getElementById(rowId);
  if (row) row.remove();
  const container = document.getElementById('node-outputs-container');
  if (!container.querySelector('.param-row')) {
    const emptyEl = document.getElementById('output-rows-empty');
    if (emptyEl) emptyEl.style.display = '';
  }
}

// ============================================================
// Node 返回值定义：来源联动（手工配置 / activity选择）
// activity选择 与「引用前序」同构，但可选范围为本节点已配置的全部 Activity 的参数与返回值
// ============================================================

// 本节点已配置的全部 Activity（按阶段顺序），每项含 {id, act_name, ns, params, returnValues}
function outputAllActivities() {
  const rows = document.querySelectorAll('#node-activity-stages .act-item');
  const list = [];
  rows.forEach(prev => {
    list.push(prev);
  });
  return list.map(prev => {
    const ns = prev.getAttribute('data-ns') || '';
    const name = prev.getAttribute('data-name') || '';
    const id = prev.getAttribute('data-id') || name; // 无显式 id 时用 act_name 作为引用标识
    const act = (window._activityCache || []).find(a => a.act_namespace === ns && a.act_name === name);
    let params = [];
    if (act && act.arguments) {
      try { params = (typeof act.arguments === 'string') ? JSON.parse(act.arguments || '[]') : (act.arguments || []); } catch(e) { params = []; }
    }
    let returnValues = [];
    if (act && act.return_values) {
      try { returnValues = (typeof act.return_values === 'string') ? JSON.parse(act.return_values || '[]') : (act.return_values || []); } catch(e) { returnValues = []; }
    }
    return { id: id, act_name: name, ns: ns, params: Array.isArray(params) ? params : [], returnValues: Array.isArray(returnValues) ? returnValues : [] };
  });
}

// 解析引用路径 → 联动初值
function outputParseRefPath(ref) {
  const m = /^\{\{([^}]+)\}\}$/.exec((ref || '').trim());
  if (!m) return { raw: ref || '' };
  let inner = m[1];
  // 兼容 steps. 前缀（引用前序统一生成 {{steps.id...}}）
  if (inner.startsWith('steps.')) inner = inner.slice('steps.'.length);
  const dot = inner.indexOf('.');
  if (dot < 0) return { refId: inner, refType: 'ref_node', refField: '' };
  const refId = inner.slice(0, dot);
  const rest = inner.slice(dot + 1);
  if (rest === 'responses') return { refId: refId, refType: 'responses', refField: '' };
  if (rest === 'arguments') return { refId: refId, refType: 'arguments', refField: '' };
  if (rest.startsWith('responses.')) return { refId: refId, refType: 'responses_field', refField: rest.slice('responses.'.length) };
  if (rest.startsWith('arguments.')) return { refId: refId, refType: 'arguments', refField: rest.slice('arguments.'.length) };
  return { refId: refId, refType: 'responses', refField: rest };
}

// 联动控件拼出最终引用路径
// 取值类型与字段的拼装规则（与 activity 参数逻辑一致）：
//   - responses / responses_field 但字段为空 → {{id.responses}}（整体，不带尾点）
//   - responses_field 且字段非空       → {{id.responses.<field>}}
//   - arguments 但字段为空             → {{id.arguments}}
//   - arguments 且字段非空             → {{id.arguments.<field>}}
// 即：字段为空时不保留尾点，降级为对应整体形式。
function outputBuildRefPath(refId, refType, refField) {
  if (!refId) return '';
  const field = (refField || '').trim();
  let inner;
  if (refType === 'responses' || (refType === 'responses_field' && !field)) inner = refId + '.responses';
  else if (refType === 'responses_field') inner = refId + '.responses.' + field;
  else if (refType === 'arguments') inner = refId + '.arguments' + (field ? '.' + field : '');
  else inner = refId;
  // 所有 Activity 都在 steps 下，引用前序统一加 steps. 前缀
  return '{{steps.' + inner + '}}';
}

// 取值类型下拉（带隐藏逻辑：无 ReturnValues 则隐藏「返回值.字段」，无 params 则隐藏「参数值.字段」）
function outputRenderRefTypeSel(prev, parsed) {
  const hasReturnValues = !!(prev && Array.isArray(prev.returnValues) && prev.returnValues.length > 0);
  const hasParams = !!(prev && Array.isArray(prev.params) && prev.params.length > 0);
  let effType = parsed.refType || 'responses';
  if (effType === 'responses_field' && !hasReturnValues) effType = 'responses';
  if (effType === 'arguments' && !hasParams) effType = 'responses';
  let html = '<select class="arg-reftype" onchange="onOutputRefChange(this)" style="flex:1 1 96px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">';
  html += '<option value="responses"' + (effType === 'responses' ? ' selected' : '') + '>返回值整体</option>';
  if (hasReturnValues) html += '<option value="responses_field"' + (effType === 'responses_field' ? ' selected' : '') + '>返回值.字段</option>';
  if (hasParams) html += '<option value="arguments"' + (effType === 'arguments' ? ' selected' : '') + '>参数值.字段</option>';
  html += '</select>';
  return html;
}

// 第三级字段下拉（返回值字段 / 参数 key）
function outputRenderRefFieldSlot(prevs, prevId, type, selectedField) {
  if (type === 'responses') return '';
  const prev = (prevs || []).find(p => p.id === prevId);
  if (!prev) return '<span style="flex:1;color:var(--text-muted);font-size:.78rem">请先选择 Activity</span>';
  if (type === 'responses_field') {
    const rvs = prev.returnValues || [];
    if (rvs.length === 0) return '<span style="flex:1;color:var(--text-muted);font-size:.78rem">该 Activity 无 ReturnValues 配置</span>';
    const opts = rvs.map(rv => {
      const rvName = rv.name || '';
      const rvKey = rv.key || '';
      // 引用字段名统一用 name（与 outputRefSnippetHTML 一致）：name 是 Activity 返回值 JSON 里的实际字段名。
      // 不再因 key 为空而把选项 value 置空——否则选中后字段输入框为空，拼路径时回退为整体
      // {{id.responses}}，导致选了「返回值.字段」却保存不上（表现为未提交）。
      const val = rvName;
      // 选项文本括号里展示经 Activity 修改后的中文名（label/name），而非数据原始返回的 key
      const dispName = rv.label || rv.name || '';
      const label = rvName + (dispName ? ' (' + dispName + ')' : '');
      return '<option value="' + escAttr(val) + '"' + (val === selectedField ? ' selected' : '') + '>' + escHtml(label) + '</option>';
    }).join('');
    return '<select class="arg-refield" onchange="onOutputRefChange(this)" style="flex:1 1 110px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
        '<option value="">— 选择返回值字段 —</option>' + opts + '</select>';
  }
  const params = (prev.params || []).filter(pp => (pp.key || '').trim());
  if (params.length === 0) return '<span style="flex:1;color:var(--text-muted);font-size:.78rem">该 Activity 无参数定义</span>';
  const opts = params.map(pp => {
    const pk = pp.key || '';
    const pLabel = pp.label || '';
    const label = pk + (pLabel ? ' (' + pLabel + ')' : '');
    return '<option value="' + escAttr(pk) + '"' + (pk === selectedField ? ' selected' : '') + '>' + escHtml(label) + '</option>';
  }).join('');
  return '<select class="arg-refield" onchange="onOutputRefChange(this)" style="flex:1 1 110px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
      '<option value="">— 选择参数 key —</option>' + opts + '</select>';
}

// 来源选择框（手工配置 / activity选择），紧跟在类型选择框之后，宽度收窄
function outputSourceSelectHTML(source) {
  const s = source || 'value';
  return '<select class="output-source" onchange="onOutputSourceChange(this)" style="flex:0 0 78px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">' +
    '<option value="value"' + (s === 'value' ? ' selected' : '') + '>固定配置</option>' +
    '<option value="ref_act"' + (s === 'ref_act' ? ' selected' : '') + '>activity选择</option>' +
    '<option value="ref_node"' + (s === 'ref_node' ? ' selected' : '') + '>引用节点</option>' +
    '</select>';
}

// 固定配置分支下：列出本节点全部上游 Activity 的返回值引用路径，供用户点选复制。
// 形如 {{steps.A000014__no3m7.responses.source_key_list}}
function outputRefSnippetHTML() {
  const prevs = outputAllActivities();
  const items = [];
  prevs.forEach(p => {
    const rvs = Array.isArray(p.returnValues) ? p.returnValues : [];
    if (rvs.length === 0) {
      // 该 Activity 未配置 ReturnValues：仍给出整体引用 {{steps.id.responses}}
      items.push({ act: p, ref: '{{steps.' + p.id + '.responses}}', label: p.act_name + '（整体）' });
      return;
    }
    rvs.forEach(rv => {
      const rvName = (rv.name || '').trim();
      const rvKey = rv.key || '';
      // 已配置 ReturnValues：把列表中每个项的 name 都作为可引用字段展示（数组即全部展开）。
      // 若 name 为空则回退为整体引用 {{steps.id.responses}}。
      if (rvName === '') {
        items.push({ act: p, ref: '{{steps.' + p.id + '.responses}}', label: p.act_name + '（整体）' });
      } else {
        items.push({ act: p, ref: '{{steps.' + p.id + '.responses.' + rvName + '}}', label: p.act_name + ' / ' + (rv.label || rvKey || rvName) });
      }
    });
  });
  if (items.length === 0) return '';
  const rows = items.map(it =>
    '<div class="output-ref-snippet" onclick="copyToClipboard(' + JSON.stringify(it.ref) + ')" title="点击复制" style="cursor:pointer;display:flex;justify-content:space-between;gap:8px;padding:3px 6px;border-radius:4px;font-family:monospace;font-size:.74rem;color:var(--accent);background:#f3f6fb">' +
      '<span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + escHtml(it.ref) + '</span>' +
      '<span style="flex:0 0 auto;color:var(--text-muted);font-size:.7rem">' + escHtml(it.label) + '</span>' +
    '</div>'
  ).join('');
  return '<div class="output-ref-snippet-box" style="flex-basis:100%;margin-top:6px">' +
    '<div style="font-size:.72rem;color:var(--text-muted);margin-bottom:4px">上游 Activity 返回值引用路径（点击复制）：</div>' +
    rows + '</div>';
}

// 渲染单行输入控件（按来源）
function outputRenderInput(bind) {
  bind = bind || {};
  const source = bind.source || 'value';
  if (source === 'ref_act') {
    const prevs = outputAllActivities();
    const parsed = bind.ref ? outputParseRefPath(bind.ref) : { refId: '', refType: 'responses', refField: '' };
    const refId = parsed.refId || '';
    const curPrev = refId ? (prevs.find(p => p.id === refId) || null) : null;
    const key = bind.key || '';
    const label = bind.label || '';
    const type = bind.type || '';
    const prevOpts = '<option value="">— 选择 Activity —</option>' + prevs.map(p =>
      '<option value="' + escAttr(p.id) + '"' + (p.id === refId ? ' selected' : '') + '>' + escHtml((p.act_name || p.id) + ' (' + p.ns + ')') + '</option>'
    ).join('');
    let html = '<input class="output-key" placeholder="键 (key)" value="' + esc(key) + '" style="flex:1 1 70px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
      '<input class="output-label" placeholder="显示名 (label)" value="' + esc(label) + '" style="flex:1 1 70px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">' +
      '<select class="output-type" style="flex:0 0 70px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">' + outputTypeHTML(type) + '</select>' +
      outputSourceSelectHTML(source) +
      '<span class="arg-ref-combo" style="flex:1 1 200px;display:flex;gap:4px;align-items:center;min-width:0">' +
        '<select class="arg-ref" onchange="onOutputRefChange(this)" style="flex:1 1 120px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' + prevOpts + '</select>' +
        outputRenderRefTypeSel(curPrev, parsed) +
      '</span>' +
      '<span class="arg-ref-field-slot" style="flex:1 1 120px;display:flex;gap:4px;align-items:center;min-width:0">' + outputRenderRefFieldSlot(prevs, refId, parsed.refType, parsed.refField) + '</span>';
    html += '<input type="hidden" class="output-ref-final" value="' + escAttr(bind.ref || '') + '">';
    return html;
  }
  if (source === 'ref_node') {
    // 引用节点参数定义：列出本节点已定义的参数 key，生成 {{arguments.key}}
    const nodeParams = nodeParamDefs();
    const nodeRef = bind.ref || '';
    const key = bind.key || '';
    const label = bind.label || '';
    const type = bind.type || '';
    let refCtrl;
    if (nodeParams.length === 0) {
      refCtrl = '<span style="flex:1;color:var(--text-muted);font-size:.78rem">本节点未定义参数，无法引用</span>';
    } else {
      const opts = nodeParams.map(p =>
        '<option value="' + escAttr('{{arguments.' + p.key + '}}') + '"' + ('{{arguments.' + p.key + '}}' === nodeRef ? ' selected' : '') + '>' + escHtml((p.label || p.key) + ' (' + p.key + ')') + '</option>'
      ).join('');
      refCtrl = '<select class="output-ref-node" onchange="onOutputRefChange(this)" style="flex:1;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
        '<option value="">— 选择节点参数 —</option>' + opts + '</select>';
    }
    let html = '<input class="output-key" placeholder="键 (key)" value="' + esc(key) + '" style="flex:1 1 70px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
      '<input class="output-label" placeholder="显示名 (label)" value="' + esc(label) + '" style="flex:1 1 70px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">' +
      '<select class="output-type" style="flex:0 0 70px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">' + outputTypeHTML(type) + '</select>' +
      outputSourceSelectHTML(source) +
      refCtrl;
    html += '<input type="hidden" class="output-ref-final" value="' + escAttr(bind.ref || '') + '">';
    return html;
  }
  // 手工配置：key / label / 类型(含 float64/formula 计算) / 来源 / 配置值
  const key = bind.key || '';
  const label = bind.label || '';
  const type = bind.type || '';
  const value = bind.value || '';
  return '<input class="output-key" placeholder="键 (key)" value="' + esc(key) + '" style="flex:1 1 90px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
    '<input class="output-label" placeholder="显示名 (label)" value="' + esc(label) + '" style="flex:1 1 90px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">' +
    '<select class="output-type" title="返回值类型/计算类型" style="flex:0 0 78px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">' + outputTypeHTML(type) + '</select>' +
    outputSourceSelectHTML(bind.source) +
    '<input class="output-value" placeholder="配置值/表达式" value="' + esc(value) + '" style="flex:1 1 110px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
    outputRefSnippetHTML();
}

// 重建第三级字段下拉
function outputRebuildRefFieldSlot(wrap) {
  const prevSel = wrap.querySelector('.arg-ref');
  const typeSel = wrap.querySelector('.arg-reftype');
  const slot = wrap.querySelector('.arg-ref-field-slot');
  if (!prevSel || !typeSel || !slot) return;
  const prevs = outputAllActivities();
  const prev = prevs.find(p => p.id === prevSel.value) || null;
  slot.innerHTML = outputRenderRefFieldSlot(prevs, prevSel.value, typeSel.value, '');
}

// 来源切换：手工配置 / activity选择（局部重建当前行）
function onOutputSourceChange(sel) {
  const row = sel.closest('.param-row');
  if (!row) return;
  const wrap = row.querySelector('.output-input-wrap');
  if (!wrap) return;
  const newSrc = sel.value;
  // 保留已填值（key/label/type 在两种来源下都保留；value 与 ref 各自保留）
  const prevKey = row.querySelector('.output-key') ? row.querySelector('.output-key').value.trim() : '';
  const prevLabel = row.querySelector('.output-label') ? row.querySelector('.output-label').value.trim() : '';
  const prevType = row.querySelector('.output-type') ? row.querySelector('.output-type').value : '';
  const prevValue = row.querySelector('.output-value') ? row.querySelector('.output-value').value : '';
  const finalInput = row.querySelector('.output-ref-final');
  const prevRef = finalInput ? finalInput.value.trim() : '';
  const bind = {
    source: newSrc,
    key: prevKey,
    label: prevLabel,
    type: prevType,
    value: prevValue,
    ref: prevRef,
  };
  wrap.innerHTML = outputRenderInput(bind);
  // 回显引用联动初值
  if (newSrc === 'ref_act' && bind.ref) {
    const parsed = outputParseRefPath(bind.ref);
    if (!Object.prototype.hasOwnProperty.call(parsed, 'raw')) {
      const pSel = wrap.querySelector('.arg-ref');
      const tSel = wrap.querySelector('.arg-reftype');
      const fInput = wrap.querySelector('.arg-refield');
      if (pSel && parsed.refId) pSel.value = parsed.refId;
      if (tSel && parsed.refType) tSel.value = parsed.refType;
      if (fInput && parsed.refField) fInput.value = parsed.refField;
    }
  }
  if (newSrc === 'ref_node' && bind.ref) {
    const nSel = wrap.querySelector('.output-ref-node');
    if (nSel) nSel.value = bind.ref;
  }
}

// 引用下拉/输入变化：拼装最终引用路径写入 .output-ref-final
function onOutputRefChange(sel) {
  const wrap = sel.closest('.output-input-wrap');
  if (!wrap) return;
  let finalInput = wrap.querySelector('.output-ref-final');
  if (!finalInput) return;
  // 引用节点参数：直接以下拉选中值（形如 {{arguments.key}}）作为最终引用路径
  const nodeSel = wrap.querySelector('.output-ref-node');
  if (nodeSel && sel === nodeSel) {
    finalInput.value = nodeSel.value;
    return;
  }
  const prevSel = wrap.querySelector('.arg-ref');
  const typeSel = wrap.querySelector('.arg-reftype');
  const slot = wrap.querySelector('.arg-ref-field-slot');
  // 切换取值类型：重建第三级字段
  if (sel === typeSel) {
    outputRebuildRefFieldSlot(wrap);
  }
  // 切换 Activity：整行随新 Activity 重建（取值类型选项、字段均动态生成），回填当前已选 Activity 与 key/label/type
  if (sel === prevSel) {
    const parsed = outputParseRefPath(finalInput.value);
    const refId = prevSel.value;
    const prevs = outputAllActivities();
    const curPrev = refId ? (prevs.find(p => p.id === refId) || null) : null;
    const row = sel.closest('.param-row');
    const key = row && row.querySelector('.output-key') ? row.querySelector('.output-key').value.trim() : '';
    const label = row && row.querySelector('.output-label') ? row.querySelector('.output-label').value.trim() : '';
    const type = row && row.querySelector('.output-type') ? row.querySelector('.output-type').value : '';
    // 保留当前来源（切换 Activity 时来源不变，必须带回，否则重建后 .output-source 丢失，collectOutputs 会回退为 value）
    const curSource = row && row.querySelector('.output-source') ? row.querySelector('.output-source').value : 'ref_act';
    const typeSelHTML = outputRenderRefTypeSel(curPrev, parsed.refType ? parsed : { refType: 'responses' });
    let html = '<input class="output-key" placeholder="键 (key)" value="' + esc(key) + '" style="flex:1 1 80px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
      '<input class="output-label" placeholder="显示名 (label)" value="' + esc(label) + '" style="flex:1 1 80px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">' +
      '<select class="output-type" style="flex:0 0 80px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">' + outputTypeHTML(type) + '</select>' +
      outputSourceSelectHTML(curSource) +
      '<span class="arg-ref-combo" style="flex:1 1 160px;display:flex;gap:4px;align-items:center;min-width:0">' +
        '<select class="arg-ref" onchange="onOutputRefChange(this)" style="flex:1 1 90px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
          '<option value="">— 选择 Activity —</option>' + prevs.map(p =>
            '<option value="' + escAttr(p.id) + '"' + (p.id === refId ? ' selected' : '') + '>' + escHtml((p.act_name || p.id) + ' (' + p.ns + ')') + '</option>'
          ).join('') + '</select>' +
        typeSelHTML +
      '</span>';
    html += '<span class="arg-ref-field-slot" style="flex:1 1 120px;display:flex;gap:4px;align-items:center;min-width:0">' + outputRenderRefFieldSlot(prevs, refId, (curPrev && parsed.refType) || 'responses', parsed.refField || '') + '</span>';
    html += '<input type="hidden" class="output-ref-final" value="' + escAttr(finalInput.value) + '">';
    wrap.innerHTML = html;
    const newPrevSel = wrap.querySelector('.arg-ref');
    if (newPrevSel && refId) newPrevSel.value = refId;
    finalInput = wrap.querySelector('.output-ref-final');
  }
  // 拼装最终引用路径
  const pSel = wrap.querySelector('.arg-ref');
  const tSel = wrap.querySelector('.arg-reftype');
  const fInput = wrap.querySelector('.arg-refield');
  if (pSel && tSel) {
    finalInput.value = outputBuildRefPath(pSel.value, tSel.value, fInput ? fInput.value : '');
  }
  // 自动补全 key/label（仅当为空）：选择 Activity 或具体返回值字段后，用对应字段名/中文名填充
  const autoPrevSel = wrap.querySelector('.arg-ref');
  if (autoPrevSel && autoPrevSel.value) {
    const autoPrevs = outputAllActivities();
    const autoCur = autoPrevs.find(p => p.id === autoPrevSel.value) || null;
    if (autoCur) {
      const autoKeyInp = wrap.querySelector('.output-key');
      const autoLabelInp = wrap.querySelector('.output-label');
      if (autoKeyInp || autoLabelInp) {
        const autoF = fInput ? (fInput.value || '') : '';
        const autoType = tSel ? (tSel.value || '') : '';
        // 仅选了具体字段（返回值.字段 / 参数值.字段）才自动填充；选「返回值整体」不填充
        if (!autoF) return;
        let fk = '', fl = '';
        if (autoType === 'arguments') {
          const pp = (autoCur.params || []).find(p => (p.key || '') === autoF);
          if (!pp) return;
          fk = pp.key || '';
          fl = pp.label || '';
        } else {
          // 默认值（未显式类型时按返回值字段处理）
          const rv = (autoCur.returnValues || []).find(r => (r.name || '') === autoF);
          if (!rv) return;
          fk = rv.name || '';
          fl = rv.label || rv.name || '';
        }
        if (autoKeyInp && !autoKeyInp.value.trim()) autoKeyInp.value = fk;
        if (autoLabelInp && !autoLabelInp.value.trim()) autoLabelInp.value = fl;
      }
    }
  }
}

function collectOutputs() {
  const rows = document.querySelectorAll('#node-outputs-container .param-row');
  const outputs = [];
  rows.forEach(row => {
    const source = row.querySelector('.output-source') ? row.querySelector('.output-source').value : 'value';
    const key = row.querySelector('.output-key') ? row.querySelector('.output-key').value.trim() : '';
    const label = row.querySelector('.output-label') ? row.querySelector('.output-label').value.trim() : '';
    const type = row.querySelector('.output-type') ? row.querySelector('.output-type').value : '';
    const out = { key: key, label: label, type: type, source: source };
    if (source === 'value') {
      const valueEl = row.querySelector('.output-value');
      out.value = valueEl ? valueEl.value : '';
    } else if (source === 'ref_act') {
      const finalInput = row.querySelector('.output-ref-final');
      let ref = finalInput ? finalInput.value.trim() : '';
      // 兜底：若隐藏域为空（如时序/重建导致未写入），用当前下拉实时拼装，确保选择不丢
      if (!ref) {
        const pSel = row.querySelector('.arg-ref');
        const tSel = row.querySelector('.arg-reftype');
        const fInput = row.querySelector('.arg-refield');
        if (pSel && tSel) {
          ref = outputBuildRefPath(pSel.value, tSel.value, fInput ? fInput.value : '');
        }
      }
      out.ref = ref;
      out.value = ref; // 与 value 来源保持一致，便于消费端统一读取 value
    } else if (source === 'ref_node') {
      // 引用节点参数定义：最终路径为 {{arguments.key}}，与「手工配置」共用 value 字段语义不同，单独存 ref
      const finalInput = row.querySelector('.output-ref-final');
      let ref = finalInput ? finalInput.value.trim() : '';
      // 兜底：隐藏域为空时用当前下拉实时取值，确保选择不丢
      if (!ref) {
        const nSel = row.querySelector('.output-ref-node');
        if (nSel) ref = nSel.value;
      }
      out.ref = ref;
      out.value = ref; // 与 value 来源保持一致，便于消费端统一读取 value
    }
    if (key) {
      outputs.push(out);
    }
  });
  return outputs;
}

function clearOutputRows() {
  const container = document.getElementById('node-outputs-container');
  container.querySelectorAll('.param-row').forEach(r => r.remove());
  const emptyEl = document.getElementById('output-rows-empty');
  if (emptyEl) emptyEl.style.display = '';
}

// ============================================================
// Activity 参数定义（逐条编辑，与 Node 参数定义同构）
// ============================================================
let actParamSeq = 0;

function addActParamRow(key, label, type, required, value, description, policy) {
  actParamSeq++;
  const container = document.getElementById('act-params-container');
  const emptyEl = document.getElementById('act-param-rows-empty');
  if (emptyEl) emptyEl.style.display = 'none';

  const row = document.createElement('div');
  row.className = 'param-row';
  row.id = 'act-param-row-' + actParamSeq;
  const reqChecked = required ? 'checked' : '';
  const policyOptions = [
    { v: 'frontend+', t: '用户优先' },
    { v: 'backend+', t: '节点优先' },
    { v: 'backend', t: '节点锁定' },
    { v: 'frontend', t: '用户强制' },
    { v: 'default', t: '仅默认值' },
  ];
  const policyHTML = policyOptions.map(o =>
    `<option value="${o.v}" ${(policy||'frontend+')===o.v?'selected':''}>${o.t}</option>`
  ).join('');
  row.innerHTML = `
    <input class="param-key" placeholder="参数键" value="${esc(key||'')}">
    <input class="param-label" placeholder="显示名" value="${esc(label||'')}">
    <select class="param-type">
      ${ARG_TYPE_OPTIONS.map(t => `<option value="${t}" ${t===type?'selected':''}>${t}</option>`).join('')}
    </select>
    <label class="param-required"><input type="checkbox" ${reqChecked}>必填</label>
    <select class="param-policy">${policyHTML}</select>
    <input class="param-default" placeholder="默认值" value="${esc(value||'')}">
    <input class="param-desc" placeholder="描述" value="${esc(description||'')}">
    <button class="btn-remove-param" onclick="removeActParamRow('act-param-row-${actParamSeq}')" title="删除此参数">&times;</button>
  `;
  container.appendChild(row);
}

function removeActParamRow(rowId) {
  const row = document.getElementById(rowId);
  if (row) row.remove();
  const container = document.getElementById('act-params-container');
  if (!container.querySelector('.param-row')) {
    const emptyEl = document.getElementById('act-param-rows-empty');
    if (emptyEl) emptyEl.style.display = '';
  }
}

function collectActParams() {
  const rows = document.querySelectorAll('#act-params-container .param-row');
  const params = [];
  rows.forEach(row => {
    const key = row.querySelector('.param-key').value.trim();
    const label = row.querySelector('.param-label').value.trim();
    const type = row.querySelector('.param-type').value;
    const required = row.querySelector('.param-required input').checked;
    const policy = row.querySelector('.param-policy') ? row.querySelector('.param-policy').value : 'frontend+';
    let value = row.querySelector('.param-default').value.trim();
    const description = row.querySelector('.param-desc').value.trim();
    if (!key) return; // 跳过未填 key 的空行
    // 按类型转换默认值，确保写入 arguments 的值类型正确
    if (type === 'number') {
      const n = Number(value);
      value = value === '' || isNaN(n) ? '' : n;
    } else if (type === 'boolean') {
      value = (value === 'true' || value === '1');
    } else if (type === 'json' && value) {
      try { value = JSON.parse(value); } catch (e) { /* 非法 JSON 保持原字符串 */ }
    }
    params.push({ key, label, type, required, policy, value: value === '' ? '' : value, description });
  });
  return params;
}

// ============================================================
// Activity 返回值设置 (ReturnValues)
// ============================================================
// 当前 activity 最近一条测试成功记录中提取出的返回值 map key 列表（全局缓存，供 key 下拉/输入提示使用）
let _returnValueKeyOptions = [];

// 异步加载最近一条测试成功记录中的返回值 map 全部 key。
// 仅取「最近的一条」测试成功记录（按接口返回顺序，第一条 success 视为最近），而非多条合并。
async function loadReturnValueKeyOptions(activityId) {
  _returnValueKeyOptions = [];
  try {
    const p = getProject();
    if (!activityId || !p) return;
    const records = await api('/api/activities/' + encodeURIComponent(activityId) + '/test-records?project=' + encodeURIComponent(p));
    // 找到最近一条（列表第一条）测试成功的记录
    let latest = null;
    for (const r of (records || [])) {
      if (r.status === 'success') { latest = r; break; }
    }
    if (latest) {
      let obj = latest.result;
      if (typeof obj === 'string') { try { obj = JSON.parse(obj); } catch (e) { obj = null; } }
      if (obj && typeof obj === 'object') {
        _returnValueKeyOptions = Object.keys(obj);
      }
    }
    // 重新渲染已存在行的 key 输入提示
    document.querySelectorAll('#act-return-values-container .rv-row').forEach(renderReturnValueKeyOptions);
  } catch (e) { /* 忽略加载失败 */ }
}

// 渲染某行的 key 候选（datalist 提示，支持手动输入）
function renderReturnValueKeyOptions(row) {
  const input = row.querySelector('.rv-key');
  if (!input) return;
  let dl = row.querySelector('.rv-key-list');
  if (!dl) {
    // 首次创建 datalist
    dl = document.createElement('datalist');
    dl.className = 'rv-key-list';
    dl.id = 'rv-keylist-' + Math.random().toString(36).slice(2);
    input.setAttribute('list', dl.id);
    row.appendChild(dl);
  }
  dl.innerHTML = _returnValueKeyOptions.map(k =>
    '<option value="' + escAttr(k) + '">' + escHtml(k) + '</option>'
  ).join('');
}

function addReturnValueRow(name, key, type, label) {
  const container = document.getElementById('act-return-values-container');
  const emptyEl = document.getElementById('act-return-values-empty');
  if (emptyEl) emptyEl.style.display = 'none';

  const typeOptions = ['', 'string', 'int64', 'float64', 'bool', 'slice', 'map', 'formula'];
  const typeLabels = { '': '不转换', 'string': 'string', 'int64': 'int64', 'float64': 'float64', 'bool': 'bool', 'slice': 'slice', 'map': 'map', 'formula': 'formula' };
  const typeHTML = typeOptions.map(t =>
    '<option value="' + t + '"' + ((type||'') === t ? ' selected' : '') + '>' + typeLabels[t] + '</option>'
  ).join('');

  const row = document.createElement('div');
  row.className = 'rv-row';
  row.style.cssText = 'display:flex;gap:8px;align-items:center;padding:6px 0;border-bottom:1px dotted #eef0f3';
  row.innerHTML = `
    <input class="rv-name" placeholder="返回值名称" value="${esc(name||'')}" style="flex:1;min-width:0;padding:6px 8px;border:1px solid var(--border);border-radius:6px;font-size:.82rem">
    <input class="rv-label" placeholder="中文显示名（可选）" value="${esc(label||'')}" style="flex:1;min-width:0;padding:6px 8px;border:1px solid var(--border);border-radius:6px;font-size:.82rem">
    <input class="rv-key" placeholder="返回值 key（留空=返回所有内容，可手动输入）" value="${esc(key||'')}" style="flex:1;min-width:0;padding:6px 8px;border:1px solid var(--border);border-radius:6px;font-size:.82rem">
    <select class="rv-type" title="输出格式转换类型" style="flex:0 0 96px;min-width:0;padding:6px 8px;border:1px solid var(--border);border-radius:6px;font-size:.82rem">${typeHTML}</select>
    <button class="btn-remove-param" onclick="removeReturnValueRow(this)" title="删除此返回值">&times;</button>
  `;
  container.appendChild(row);
  renderReturnValueKeyOptions(row);
}

function removeReturnValueRow(btn) {
  const row = btn.closest('.rv-row');
  if (row) row.remove();
  const container = document.getElementById('act-return-values-container');
  if (!container.querySelector('.rv-row')) {
    const emptyEl = document.getElementById('act-return-values-empty');
    if (emptyEl) emptyEl.style.display = '';
  }
}

function collectReturnValues() {
  const rows = document.querySelectorAll('#act-return-values-container .rv-row');
  const list = [];
  rows.forEach(row => {
    const name = row.querySelector('.rv-name').value.trim();
    const label = row.querySelector('.rv-label') ? row.querySelector('.rv-label').value.trim() : '';
    const key = row.querySelector('.rv-key').value;
    const type = row.querySelector('.rv-type') ? row.querySelector('.rv-type').value : '';
    if (!name) return; // 跳过未填名称的空行
    // label 缺省时回退为 name，保证后端 BindConfig.Label 始终有值（中文显示）
    list.push({ name: name, label: label || name, key: key || '', type: type || '' });
  });
  return list;
}

function clearReturnValueRows() {
  const container = document.getElementById('act-return-values-container');
  if (!container) return;
  container.querySelectorAll('.rv-row').forEach(r => r.remove());
  const emptyEl = document.getElementById('act-return-values-empty');
  if (emptyEl) emptyEl.style.display = '';
}

function clearActParamRows() {
  const container = document.getElementById('act-params-container');
  container.querySelectorAll('.param-row').forEach(r => r.remove());
  const emptyEl = document.getElementById('act-param-rows-empty');
  if (emptyEl) emptyEl.style.display = '';
}

// ============================================================
// Sub Chains / Root Chains 列表前端分页（两个列表共用）
// ============================================================
// rows 为最近一次接口返回的全量列表；搜索在前端按字段匹配，筛选/翻页时保留原始下标（编辑等按钮按下标取数据）。
// project 记录 rows 所属项目，切换项目后回到第 1 页。
const _chainListPagers = {
  sub:  { page: 1, size: 20, rows: [], project: '', sizeRestored: false, colspan: 6, tbody: '#sub-table tbody', search: '#tab-sub-chains .search-box', storageKey: 'wf_sub_page_size' },
  root: { page: 1, size: 20, rows: [], project: '', sizeRestored: false, colspan: 7, tbody: '#root-table tbody', search: '#tab-root-chains .search-box', storageKey: 'wf_root_page_size' },
};

// 根链线上版本映射（chain_id → version），loadRootChains 时刷新，供行渲染与搜索使用
let _rootCurVerMap = {};

// 接口数据到达后更新列表缓存并渲染当前页（删除/编辑后刷新仍停留在当前页；当前页删空时自动退回上一页）
function setChainListRows(key, rows) {
  const st = _chainListPagers[key];
  if (!st.sizeRestored) {
    st.sizeRestored = true;
    const sel = document.getElementById(key + '-page-size');
    let saved = '';
    try { saved = localStorage.getItem(st.storageKey) || ''; } catch (_) {}
    if (sel && saved && Array.prototype.some.call(sel.options, o => o.value === saved)) sel.value = saved;
    st.size = parseInt(sel ? sel.value : '', 10) || 20;
  }
  const p = getProject();
  if (st.project !== p) { st.project = p; st.page = 1; }
  st.rows = rows || [];
  renderChainList(key);
}

// 按搜索框关键字过滤，返回 [{ c, i }]（i 为在 rows 中的原始下标）
function filterChainListRows(key) {
  const st = _chainListPagers[key];
  const el = document.querySelector(st.search);
  const q = (el ? el.value : '').trim().toLowerCase();
  const result = [];
  st.rows.forEach((c, i) => {
    if (q && !chainListSearchText(key, c).toLowerCase().includes(q)) return;
    result.push({ c: c, i: i });
  });
  return result;
}

// 参与搜索的字段（对应表格中展示的文字列）
function chainListSearchText(key, c) {
  const fields = [c.chain_id, c.name, c.description, c.status == 1 ? '启用' : '禁用'];
  if (key === 'root') {
    const curVer = _rootCurVerMap[c.chain_id];
    fields.push(c.chain_key, curVer ? 'v' + curVer : '未发布');
    fields.push.apply(fields, c.must_input_params || []);
  }
  return fields.filter(Boolean).join(' ');
}

// 渲染列表当前页与分页条
function renderChainList(key) {
  const st = _chainListPagers[key];
  const tbody = document.querySelector(st.tbody);
  if (!tbody) return;
  const all = filterChainListRows(key);
  if (all.length === 0) {
    tbody.innerHTML = '<tr><td colspan="' + st.colspan + '"><div class="empty-state"><div class="icon">🔍</div><p>无匹配数据</p></div></td></tr>';
    renderChainListPager(key, 0);
    return;
  }
  const totalPages = Math.max(1, Math.ceil(all.length / st.size));
  if (st.page > totalPages) st.page = totalPages;
  if (st.page < 1) st.page = 1;
  const start = (st.page - 1) * st.size;
  const rowHtml = key === 'sub' ? subChainRowHtml : rootChainRowHtml;
  tbody.innerHTML = all.slice(start, start + st.size).map(({ c, i }) => rowHtml(c, i)).join('');
  renderChainListPager(key, all.length);
}

// 渲染分页条：有数据即展示总数与页码；无数据时隐藏
function renderChainListPager(key, total) {
  const st = _chainListPagers[key];
  const pager = document.getElementById(key + '-pager');
  if (!pager) return;
  if (total <= 0) {
    pager.style.display = 'none';
    return;
  }
  const totalPages = Math.max(1, Math.ceil(total / st.size));
  pager.style.display = 'flex';
  document.getElementById(key + '-page-info').textContent =
    '第 ' + st.page + ' / ' + totalPages + ' 页（共 ' + total + ' 条）';
  document.getElementById(key + '-prev').disabled = st.page <= 1;
  document.getElementById(key + '-next').disabled = st.page >= totalPages;
}

// 搜索关键字变化：结果集改变，回到第一页
function onChainListSearch(key) {
  _chainListPagers[key].page = 1;
  renderChainList(key);
}

// 切换页码（基于当前搜索结果）
function changeChainListPage(key, delta) {
  const st = _chainListPagers[key];
  const totalPages = Math.max(1, Math.ceil(filterChainListRows(key).length / st.size));
  const newPage = st.page + delta;
  if (newPage < 1 || newPage > totalPages) return;
  st.page = newPage;
  renderChainList(key);
}

// 每页条数变化：持久化选择并回到第一页
function onChainListPageSizeChange(key) {
  const st = _chainListPagers[key];
  const sel = document.getElementById(key + '-page-size');
  const size = parseInt(sel ? sel.value : '', 10);
  if (!(size > 0)) return;
  st.size = size;
  try { localStorage.setItem(st.storageKey, String(size)); } catch (_) {}
  st.page = 1;
  renderChainList(key);
}

// ============================================================
// Sub Chains
// ============================================================
async function loadSubChains() {
  try {
    const chains = await api('/api/sub-chains');
    const tbody = document.querySelector('#sub-table tbody');
    if (!chains.length) {
      _chainListPagers.sub.rows = [];
      tbody.innerHTML = '<tr><td colspan="6"><div class="empty-state"><div class="icon">🔗</div><p>项目 <b>' + esc(getProject()) + '</b> 暂无子链数据</p></div></td></tr>';
      renderChainListPager('sub', 0);
      return;
    }
    window._subChainsForEdit = chains; // 缓存供编辑按钮索引使用
    setChainListRows('sub', chains);
  } catch (e) { showToast('加载子链失败: ' + e.message, 'error'); }
}

// 子链列表单行（i 为 _subChainsForEdit 中的原始下标）
function subChainRowHtml(c, i) {
  let dslSize = '-';
  try { dslSize = (c.dsl_json || '').length + ' chars'; } catch(e){}
  return `
      <tr>
        <td class="code-cell" title="${esc(c.chain_id)}">${esc(c.chain_id)}</td>
        <td>${esc(c.name)}</td>
        <td title="${esc(c.description||'')}">${esc(trunc(c.description, 40))}</td>
        <td><span class="badge ${c.status==1?'badge-on':'badge-off'}">${c.status==1?'启用':'禁用'}</span></td>
        <td class="code-cell">${dslSize}</td>
        <td class="actions">
          <button class="btn btn-sm btn-primary btn-icon" ${actionIconAttrs('Execute')} onclick="executeSubChain('${esc(c.chain_id)}')">${actionIconSvg('test')}</button>
          <button class="btn btn-sm btn-outline btn-icon edit-only" ${actionIconAttrs('编排')} onclick="orchOpenInPage('${esc(c.chain_id)}')">${actionIconSvg('orch')}</button>
          <button class="btn btn-sm btn-outline btn-icon edit-only" ${actionIconAttrs('编辑')} onclick="editSubChainByIndex(${i})">${actionIconSvg('edit')}</button>
          <button class="btn btn-sm btn-danger btn-icon edit-only" ${actionIconAttrs('删除')} onclick="deleteSubChain('${esc(c.chain_id)}')">${actionIconSvg('delete')}</button>
        </td>
      </tr>`;
}

function openSubChainModal(chain) {
  document.getElementById('sub-is-edit').value = chain ? '1' : '';
  document.getElementById('sub-modal-title').textContent = chain ? '编辑 Sub Chain' : '新增 Sub Chain';
  document.getElementById('sub-chain-id').value = chain ? chain.chain_id : '';
  document.getElementById('sub-chain-id').readOnly = !!chain;
  document.getElementById('sub-name').value = chain ? chain.name || '' : '';
  document.getElementById('sub-status').value = chain ? String(chain.status) : '1';
  document.getElementById('sub-description').value = chain ? chain.description || '' : '';
  document.getElementById('sub-dsl-json').value = chain ? prettyJson(chain.dsl_json) : '{\n  "ruleChain": {\n    "id": "sub1",\n    "name": "sub chain"\n  },\n  "metadata": {\n    "nodes": [],\n    "connections": []\n  }\n}';
  document.getElementById('sub-modal-overlay').classList.add('show');
}

function closeSubChainModal() {
  document.getElementById('sub-modal-overlay').classList.remove('show');
}

async function saveSubChain() {
  const isEdit = document.getElementById('sub-is-edit').value === '1';
  const body = {
    chain_id: document.getElementById('sub-chain-id').value.trim(),
    name: document.getElementById('sub-name').value.trim(),
    status: parseInt(document.getElementById('sub-status').value),
    description: document.getElementById('sub-description').value.trim(),
    dsl_json: document.getElementById('sub-dsl-json').value,
  };
  if (!body.name) { showToast('名称不能为空', 'error'); return; }
  try {
    if (isEdit) {
      const saved = await apiSubChainSaveWithConfirm(confirmed => api('/api/sub-chains/' + encodeURIComponent(body.chain_id) + (confirmed ? '?confirm_published=1' : ''), { method: 'PUT', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) }));
      if (!saved) return;
      showToast('子链已更新', 'success');
    } else {
      await api('/api/sub-chains', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
      showToast('子链已创建', 'success');
    }
    closeSubChainModal();
    loadSubChains();
  } catch (e) { showToast('保存失败: ' + e.message, 'error'); }
}

function editSubChainByIndex(i) { openSubChainModal(window._subChainsForEdit[i]); }

// 子链测试：与根链 Execute 同一个执行弹窗（自动列出调用传入参数、选执行环境、展示执行结果），
// 执行走 /api/sub-chains/{id}/execute，返回结构与根链测试执行一致。
async function executeSubChain(chainId) {
  let full;
  try {
    full = await api('/api/sub-chains/' + encodeURIComponent(chainId));
  } catch (e) { showToast('加载子链失败: ' + (e.message || e), 'error'); return; }
  loadToExecute(full, true);
}

async function deleteSubChain(id) {
  if (!confirm('确定删除子链 ' + id + ' 吗？')) return;
  try {
    await api('/api/sub-chains/' + encodeURIComponent(id), { method: 'DELETE' });
    showToast('子链已删除', 'success');
    loadSubChains();
  } catch (e) { showToast('删除失败: ' + e.message, 'error'); }
}

// ============================================================
// Root Chains
// ============================================================
async function loadRootChains() {
  try {
    const [chains, currents] = await Promise.all([
      api('/api/root-chains'),
      api('/api/releases/current').catch(() => []),
    ]);
    const curVerMap = {};
    (currents || []).forEach(r => { curVerMap[r.chain_id] = r.version; });
    _rootCurVerMap = curVerMap;
    const tbody = document.querySelector('#root-table tbody');
    if (!chains.length) {
      _chainListPagers.root.rows = [];
      tbody.innerHTML = '<tr><td colspan="7"><div class="empty-state"><div class="icon">🌳</div><p>项目 <b>' + esc(getProject()) + '</b> 暂无根链数据，通过 Execute 页面执行工作流后会自动生成</p></div></td></tr>';
      renderChainListPager('root', 0);
      return;
    }
    window._rootChainsForEdit = chains; // 缓存供按钮索引使用
    setChainListRows('root', chains);
  } catch (e) { showToast('加载根链失败: ' + e.message, 'error'); }
}

// 根链列表单行（i 为 _rootChainsForEdit 中的原始下标）
function rootChainRowHtml(c, i) {
  const curVer = _rootCurVerMap[c.chain_id];
  // 必需入参：与 Activity 列表展示参数方式一致，用芯片展示每个 {{arguments.xxx}} 的参数名
  // 最多显示 2 个，多余数量用 "+N" 触发泡泡窗（复用 showHbPop/moveHbPop/hideHbPop 浮层）列出全部
  const params = c.must_input_params || [];
  let paramsHtml;
  if (!params.length) {
    paramsHtml = '<span style="color:var(--text-muted);font-size:.78rem">无</span>';
  } else {
    const shown = params.slice(0, 2);
    const chip = k => '<span style="font-family:monospace;font-size:.74rem;background:#eef2ff;color:#3730a3;border:1px solid #c7d2fe;border-radius:6px;padding:2px 6px;white-space:nowrap">' + esc(k) + '</span>';
    // 列表里的参数名可能是多级私有参数（如 N000002__fim5n.N000001__eka03.phone），过长时截断显示，悬停看全名，
    // 避免撑宽「必需参数」列把名称、描述挤成竖排；泡泡窗里仍用完整芯片列出全部参数
    const shortChip = k => '<span title="' + esc(k) + '" style="display:inline-block;max-width:180px;overflow:hidden;text-overflow:ellipsis;vertical-align:middle;font-family:monospace;font-size:.74rem;background:#eef2ff;color:#3730a3;border:1px solid #c7d2fe;border-radius:6px;padding:2px 6px;white-space:nowrap">' + esc(k) + '</span>';
    const shownHtml = shown.map(shortChip).join('');
    const moreHtml = params.length > 2
      ? '<span class="param-more" style="font-family:monospace;font-size:.74rem;background:#f1f5f9;color:#334155;border:1px solid #cbd5e1;border-radius:6px;padding:2px 6px;white-space:nowrap;cursor:pointer" ' +
        'onmouseenter="showHbPop(event,this)" onmousemove="moveHbPop(event)" onmouseleave="hideHbPop()">+' + (params.length - 2) + '</span>'
      : '';
    const popItems = params.map(k => '<div class="hb-pop-item">' + chip(k) + '</div>').join('');
    const popHtml = '<div class="hb-pop-title">必需入参（共 ' + params.length + ' 个）</div>' + popItems;
    paramsHtml = '<div style="background:#f8fafc;border:1px solid var(--border);border-radius:8px;padding:4px 8px;display:flex;flex-wrap:wrap;align-items:center;gap:4px;width:max-content;max-width:240px">' +
      shownHtml + moreHtml +
      (moreHtml ? '<span style="display:none" data-pop="' + encodeURIComponent(popHtml) + '"></span>' : '') +
      '</div>';
  }
  return `
  <tr>
    <td class="code-cell" title="${esc(c.chain_id)}">${esc(c.chain_id)}</td>
    <td class="code-cell" title="${esc(c.chain_key||'')}">${esc(c.chain_key||'')}</td>
    <td style="min-width:7em" title="${esc(c.description||'')}">${esc(c.name)}</td>
    <td><span class="badge ${c.status==1?'badge-on':'badge-off'}">${c.status==1?'启用':'禁用'}</span></td>
    <td>${curVer ? '<span class="badge badge-info">v' + curVer + '</span>' : '<span style="color:var(--text-muted);font-size:.8rem">未发布</span>'}</td>
    <td>${paramsHtml}</td>
    <td><div class="actions">
      <button class="btn btn-sm btn-outline btn-icon edit-only" ${actionIconAttrs('编排')} onclick="orchOpenInPageRoot('${esc(c.chain_id)}')">${actionIconSvg('orch')}</button>
      <button class="btn btn-sm btn-primary btn-icon edit-only" ${actionIconAttrs('发布')} onclick="publishRootChain('${esc(c.chain_id)}')">${actionIconSvg('publish')}</button>
      <button class="btn btn-sm btn-outline btn-icon" ${actionIconAttrs('记录')} onclick="openReleaseModal('${esc(c.chain_id)}')">${actionIconSvg('history')}</button>
      <button class="btn btn-sm btn-outline btn-icon" ${actionIconAttrs('流程图')} onclick="showFlowchartByIndex(${i})">${actionIconSvg('flow')}</button>
      <button class="btn btn-sm btn-outline btn-icon" ${actionIconAttrs('Execute')} onclick="loadToExecuteByIndex(${i})">${actionIconSvg('test')}</button>
      <button class="btn btn-sm btn-outline btn-icon edit-only" ${actionIconAttrs('复制')} onclick="copyRootChain('${esc(c.chain_id)}')">${actionIconSvg('copy')}</button>
      ${c.has_releases ? '' : `<button class="btn btn-sm btn-danger btn-icon edit-only" ${actionIconAttrs('删除')} onclick="deleteRootChain('${esc(c.chain_id)}')">${actionIconSvg('delete')}</button>`}
    </div></td>
  </tr>`;
}

// 复制根链：新建一条内容与该链当前草稿一致的根链（名称加「-副本」，不复制发布记录）
async function copyRootChain(id) {
  const c = (window._rootChainsForEdit || []).find(x => x.chain_id === id);
  if (!confirm('确定复制根链 ' + id + (c && c.name ? '（' + c.name + '）' : '') + ' 吗？\n将新建一条内容相同的根链（复制当前草稿，不复制发布记录）。')) return;
  try {
    const def = await api('/api/root-chains/' + encodeURIComponent(id) + '/copy', { method: 'POST' });
    showToast('已复制为新根链 ' + def.chain_id + '（' + def.name + '）', 'success');
    loadRootChains();
  } catch (e) { showToast('复制失败: ' + e.message, 'error'); }
}

async function deleteRootChain(id) {
  if (!confirm('确定删除根链 ' + id + ' 吗？')) return;
  try {
    await api('/api/root-chains/' + encodeURIComponent(id), { method: 'DELETE' });
    showToast('根链已删除', 'success');
    loadRootChains();
  } catch (e) { showToast('删除失败: ' + e.message, 'error'); }
}

// ============================================================
// 新增 Root Chain（仅录入基本信息，DSL 为空，编排后更新）
// ============================================================
function openCreateRootChainModal() {
  document.getElementById('create-root-name').value = '';
  document.getElementById('create-root-key').value = '';
  document.getElementById('create-root-status').value = '0';
  document.getElementById('create-root-desc').value = '';
  document.getElementById('create-root-error').textContent = '';
  document.getElementById('create-root-overlay').classList.add('show');
}

function closeCreateRootChainModal() {
  document.getElementById('create-root-overlay').classList.remove('show');
}

async function createRootChain() {
  const name = document.getElementById('create-root-name').value.trim();
  const key = document.getElementById('create-root-key').value.trim();
  const status = parseInt(document.getElementById('create-root-status').value, 10);
  const desc = document.getElementById('create-root-desc').value.trim();
  const errEl = document.getElementById('create-root-error');
  if (!name) { errEl.textContent = '请填写名称'; return; }
  try {
    const def = await api('/api/root-chains/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ chain_key: key, name: name, description: desc, status: status }),
    });
    showToast('已创建 Root Chain：' + (def.chain_id || '') + (def.chain_key ? '（' + def.chain_key + '）' : ''), 'success');
    closeCreateRootChainModal();
    loadRootChains();
  } catch (e) { errEl.textContent = e.message || '创建失败'; }
}

// ============================================================
// Root Chain 发布与回滚
// ============================================================
async function publishRootChain(chainId) {
  if (!confirm('确定发布根链 ' + chainId + ' 吗？\n当前草稿将快照为新版本，并成为生产环境使用的版本。\n发布后版本不可修改；后续变更请编辑草稿测试后重新发布。')) return;
  try {
    const rel = await api('/api/root-chains/' + encodeURIComponent(chainId) + '/publish', { method: 'POST' });
    showToast('已发布: ' + chainId + ' v' + rel.version, 'success');
    loadRootChains();
  } catch (e) { showToast('发布失败: ' + e.message, 'error'); }
}

let _releaseChainId = '';
// _releasesCache 缓存发布记录列表中每个版本行的完整记录（含 dsl_json），
// Execute 时直接取用，避免再次请求加载更多内容到内存。
let _releasesCache = {};

function openReleaseModal(chainId) {
  _releaseChainId = chainId;
  document.getElementById('release-modal-title').innerHTML =
    '发布记录: ' + esc(chainId) + '<button class="modal-close" onclick="closeReleaseModal()" title="关闭">&times;</button>';
  document.getElementById('release-modal-overlay').classList.add('show');
  loadReleaseTable();
}

function closeReleaseModal() {
  document.getElementById('release-modal-overlay').classList.remove('show');
}

async function loadReleaseTable() {
  const tbody = document.querySelector('#release-table tbody');
  tbody.innerHTML = '<tr><td colspan="6" style="text-align:center;color:var(--text-muted)">加载中...</td></tr>';
  window._releaseList = [];
  try {
    const releases = await api('/api/root-chains/' + encodeURIComponent(_releaseChainId) + '/releases');
    window._releaseList = releases || [];
    window._releasesCache = {}; // 先清空旧缓存，避免历史版本残留
    // 缓存每个版本完整记录（含 dsl_json），Execute / 对比时直接使用，避免再次请求加载更多内容到内存
    window._releaseList.forEach(r => { window._releasesCache[r.version] = r; });
    renderReleaseRows();
  } catch (e) {
    tbody.innerHTML = '<tr><td colspan="6" style="color:#ef4444">加载失败: ' + esc(e.message) + '</td></tr>';
  }
}

// 渲染发布记录行；勾选「只看稳定版本」时只显示稳定版本和当前生效版本
function renderReleaseRows() {
  const tbody = document.querySelector('#release-table tbody');
  if (!tbody) return;
  const all = window._releaseList || [];
  if (!all.length) {
    tbody.innerHTML = '<tr><td colspan="6"><div class="empty-state"><div class="icon">🚀</div><p>暂无发布记录，点击行内"发布"按钮创建第一个版本</p></div></td></tr>';
    return;
  }
  const onlyStable = !!(document.getElementById('release-only-stable') || {}).checked;
  const releases = onlyStable ? all.filter(r => r.is_stable || r.is_current) : all;
  if (!releases.length) {
    tbody.innerHTML = '<tr><td colspan="6" style="text-align:center;color:var(--text-muted)">暂无稳定版本，可在版本行点击"标记稳定"</td></tr>';
    return;
  }
  tbody.innerHTML = releases.map(r => {
    const statusHtml = (r.is_current ? '<span class="badge badge-on">当前生效</span>' : '<span class="badge badge-off">历史</span>') +
      (r.is_stable ? ' <span class="badge badge-stable" title="线上稳定版本">稳定</span>' : '') +
      (r.is_stable && r.stable_note ? '<div style="margin-top:4px;font-size:.74rem;color:var(--text-muted);max-width:220px;white-space:normal" title="' + esc(r.stable_note) + '">' + esc(r.stable_note) + '</div>' : '');
    const stableBtn = r.is_stable
      ? `<button class="btn btn-sm btn-outline edit-only" onclick="setReleaseStable(${r.version}, false)">取消稳定</button>`
      : `<button class="btn btn-sm btn-outline edit-only" onclick="setReleaseStable(${r.version}, true)">标记稳定</button>`;
    return `
      <tr${r.is_stable ? ' style="background:#f0fdf4"' : ''}>
        <td><b>v${r.version}</b></td>
        <td>${esc(r.name)}</td>
        <td style="white-space:nowrap">${esc(fmtTime(r.published_at))}</td>
        <td>${(r.dsl_json||'').length} chars</td>
        <td>${statusHtml}</td>
        <td><div class="actions">
          <button class="btn btn-sm btn-outline" onclick="viewReleaseDsl(${r.version})">查看 DSL</button>
          <button class="btn btn-sm btn-primary" onclick="executeRelease(${r.version})">Execute</button>
          ${stableBtn}
          ${r.is_current ? '' : `<button class="btn btn-sm btn-primary" onclick="setCurrentRelease(${r.version})">设为生效</button>`}
          ${r.is_current || r.is_stable ? '' : `<button class="btn btn-sm btn-danger" onclick="deleteReleaseVersion(${r.version})">删除</button>`}
        </div></td>
      </tr>`;
  }).join('');
}

// 标记 / 取消稳定版本；标记时可填写备注（如上线时间、验证情况）
async function setReleaseStable(version, stable) {
  let note = '';
  if (stable) {
    const input = prompt('将 v' + version + ' 标记为线上稳定版本。\n可填写备注（选填，如上线时间、验证情况）：', '');
    if (input === null) return; // 取消
    note = input.trim();
  } else if (!confirm('确定取消 v' + version + ' 的稳定标记吗？取消后该版本可以被删除。')) {
    return;
  }
  try {
    await api('/api/root-chains/' + encodeURIComponent(_releaseChainId) + '/releases/' + version + '/stable', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({ stable: stable, note: note }),
    });
    showToast(stable ? '已将 v' + version + ' 标记为稳定版本' : '已取消 v' + version + ' 的稳定标记', 'success');
    loadReleaseTable();
  } catch (e) { showToast('操作失败: ' + e.message, 'error'); }
}

// 回退到最近稳定版：取除当前生效版本外、版本号最大的稳定版本，确认后设为生效
async function rollbackToLatestStable() {
  const list = window._releaseList || [];
  const target = list.filter(r => r.is_stable && !r.is_current).sort((a, b) => b.version - a.version)[0];
  if (!target) {
    const cur = list.find(r => r.is_current);
    showToast(cur && cur.is_stable ? '当前生效的 v' + cur.version + ' 已是稳定版本，没有其它稳定版本可回退' : '没有可回退的稳定版本，请先标记稳定版本', 'error');
    return;
  }
  const cur = list.find(r => r.is_current);
  const msg = '确定将生产环境从 ' + (cur ? 'v' + cur.version : '当前版本') + ' 回退到稳定版本 v' + target.version + ' 吗？' +
    (target.stable_note ? '\n稳定备注：' + target.stable_note : '') + '\n回退后生产环境将使用该版本的 DSL 快照。';
  if (!confirm(msg)) return;
  await applyCurrentRelease(target.version);
}

// ===================== 发布记录版本 DSL 对比 =====================

// openCompareModal 打开版本对比弹窗，填充左右版本下拉（默认左=最新、右=次新），并渲染 diff。
function openCompareModal() {
  const leftSel = document.getElementById('compare-left');
  const rightSel = document.getElementById('compare-right');
  const cache = window._releasesCache || {};
  const versions = Object.keys(cache).sort((x, y) => Number(y) - Number(x)); // 版本号降序
  if (!versions.length) { showToast('无发布记录可对比', 'error'); return; }
  const fill = (sel, defIdx) => {
    sel.innerHTML = versions.map(v => {
      const r = cache[v];
      const suffix = (r && r.is_current) ? ' (当前生效)' : '';
      const name = (r && r.name) ? (' · ' + r.name) : '';
      return '<option value="' + escAttr(v) + '">v' + escHtml(v) + escHtml(name) + escHtml(suffix) + '</option>';
    }).join('');
    if (versions[defIdx] != null) sel.value = versions[defIdx];
  };
  fill(leftSel, 0);
  fill(rightSel, Math.min(1, versions.length - 1));
  document.getElementById('compare-modal-overlay').classList.add('show');
  renderCompareDiff();
}

function closeCompareModal() {
  document.getElementById('compare-modal-overlay').classList.remove('show');
}

// diffLines 行级 LCS diff：返回 [{type:'same'|'del'|'add', text}]，用于 GitHub 风格统一视图展示。
function diffLines(a, b) {
  const aArr = (a || '').split('\n');
  const bArr = (b || '').split('\n');
  const n = aArr.length, m = bArr.length;
  const dp = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = aArr[i] === bArr[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const res = [];
  let i = 0, j = 0;
  while (i < n && j < m) {
    if (aArr[i] === bArr[j]) { res.push({ type: 'same', text: aArr[i] }); i++; j++; }
    else if (dp[i + 1][j] >= dp[i][j + 1]) { res.push({ type: 'del', text: aArr[i] }); i++; }
    else { res.push({ type: 'add', text: bArr[j] }); j++; }
  }
  while (i < n) { res.push({ type: 'del', text: aArr[i] }); i++; }
  while (j < m) { res.push({ type: 'add', text: bArr[j] }); j++; }
  return res;
}

// renderCompareDiff 取左右版本的 dsl_json（美化后）做行级 diff，以左右双栏（split）展示：
// 左框显示左版本、右框显示右版本，与上方下拉一一对应；两版本不一致的行标红。
async function renderCompareDiff() {
  const leftSel = document.getElementById('compare-left');
  const rightSel = document.getElementById('compare-right');
  const box = document.getElementById('compare-diff');
  if (!leftSel || !rightSel || !box) return;
  const lv = leftSel.value, rv = rightSel.value;
  if (!lv || !rv) { box.innerHTML = '<div style="padding:12px;color:#94a3b8">请选择左右两个版本</div>'; return; }
  if (lv === rv) { box.innerHTML = '<div style="padding:12px;color:#94a3b8">左右版本相同，无需对比</div>'; return; }
  const cache = window._releasesCache || {};
  const leftRel = cache[lv], rightRel = cache[rv];
  if (!leftRel || !rightRel) { box.innerHTML = '<div style="padding:12px;color:#f87171">版本数据缺失，请刷新发布记录后重试</div>'; return; }
  const pretty = (j) => { try { return JSON.stringify(JSON.parse(j || '{}'), null, 2); } catch (e) { return j || ''; } };
  const a = pretty(leftRel.dsl_json);
  const b = pretty(rightRel.dsl_json);
  const diff = diffLines(a, b);
  // 转换为左右对齐的行：same->{left,right,changed:false}；del->{left,right:null}；add->{left:null,right}
  const rows = diff.map(d => {
    if (d.type === 'same') return { left: d.text, right: d.text, changed: false };
    if (d.type === 'del') return { left: d.text, right: null, changed: true };
    return { left: null, right: d.text, changed: true };
  });
  const html = rows.map(r => {
    const lCls = r.changed ? 'background:#7f1d1d;color:#fecaca' : '';
    const rCls = r.changed ? 'background:#7f1d1d;color:#fecaca' : '';
    const lTxt = escHtml(r.left != null ? r.left : '');
    const rTxt = escHtml(r.right != null ? r.right : '');
    return '<div style="display:flex">' +
      '<div style="flex:1;min-height:1.5em;padding:0 10px;border-right:1px solid #334155;' + lCls + '">' + lTxt + '</div>' +
      '<div style="flex:1;min-height:1.5em;padding:0 10px;' + rCls + '">' + rTxt + '</div>' +
    '</div>';
  }).join('');
  box.innerHTML = html;
}

async function rollbackRelease(version) {
  if (!confirm('确定将生产环境回滚到 v' + version + ' 吗？\n回滚后生产环境将使用该版本的 DSL 快照。')) return;
  await setCurrentRelease(version);
}

async function viewReleaseDsl(version) {
  try {
    const releases = await api('/api/root-chains/' + encodeURIComponent(_releaseChainId) + '/releases');
    const rel = (releases || []).find(r => r.version === version);
    if (!rel) { showToast('版本不存在', 'error'); return; }
    let pretty = rel.dsl_json;
    try { pretty = JSON.stringify(JSON.parse(rel.dsl_json), null, 2); } catch(e) {}
    openDslModal('DSL 内容 · ' + _releaseChainId + ' v' + version, pretty);
  } catch (e) { showToast('加载 DSL 失败: ' + e.message, 'error'); }
}

// executeRelease 直接从发布记录列表缓存中取当前版本行的完整记录（含 dsl_json），
// 用 chain_id（格式如 R000012）作为 ID 加载并打开 Execute 面板进行测试，避免再发请求加载更多内容到内存。
async function executeRelease(version) {
  const rel = window._releasesCache ? window._releasesCache[version] : null;
  if (!rel) { showToast('请先刷新发布记录列表后再试', 'error'); return; }
  // 数据全部使用该版本行记录；ID 用 _releaseChainId（格式如 R000012）
  const c = {
    chain_id: _releaseChainId,
    chain_key: rel.chain_key || _releaseChainId,
    name: rel.name || ('v' + version),
    dsl_json: rel.dsl_json,
    node_ids: rel.node_ids || '',
    sub_chain_ids: rel.sub_chain_ids || '',
  };
  // 使用完毕后清空缓存，避免历史记录长期驻留内存
  window._releasesCache = {};
  loadToExecute(c);
}

async function setCurrentRelease(version) {
  if (!confirm('确定将 v' + version + ' 设为生产环境当前生效版本吗？')) return;
  await applyCurrentRelease(version);
}

// 将指定版本设为生产环境当前生效版本（调用方负责确认）
async function applyCurrentRelease(version) {
  try {
    await api('/api/root-chains/' + encodeURIComponent(_releaseChainId) + '/set-current', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({ version: version }),
    });
    showToast('已切换生效版本到 v' + version, 'success');
    loadReleaseTable();
    loadRootChains();
  } catch (e) { showToast('操作失败: ' + e.message, 'error'); }
}

async function deleteReleaseVersion(version) {
  if (!confirm('确定删除发布版本 v' + version + ' 吗？\n该操作为逻辑删除：前端不再显示，但数据仍保留在数据库中（可恢复），当前生效版本不允许删除。')) return;
  try {
    await api('/api/root-chains/' + encodeURIComponent(_releaseChainId) + '/releases/' + version, {
      method: 'DELETE',
    });
    showToast('已删除 v' + version, 'success');
    loadReleaseTable();
    loadRootChains();
  } catch (e) { showToast('删除失败: ' + e.message, 'error'); }
}

function fmtTime(s) {
  if (!s) return '-';
  const d = new Date(s);
  if (isNaN(d.getTime())) return s;
  const pad = n => String(n).padStart(2, '0');
  return d.getFullYear() + '-' + pad(d.getMonth()+1) + '-' + pad(d.getDate()) + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
}

// ============================================================
// Execute Workflow
// ============================================================
// 动态 connection 行管理
let connSeq = 0; // 用于生成唯一 row id

function addConnRow(fromId, toId, connType) {
  connSeq++;
  const container = document.getElementById('exec-connections-container');
  // 隐藏空状态提示
  const emptyEl = document.getElementById('conn-rows-empty');
  if (emptyEl) emptyEl.style.display = 'none';

  const row = document.createElement('div');
  row.className = 'conn-row';
  row.id = 'conn-row-' + connSeq;
  row.innerHTML = `
    <input type="text" placeholder="from_id" value="${esc(fromId||'')}" list="conn-id-datalist" data-role="from" oninput="rebuildConnDatalist()">
    <span class="conn-arrow">→</span>
    <input type="text" placeholder="to_id" value="${esc(toId||'')}" list="conn-id-datalist" data-role="to" oninput="rebuildConnDatalist()">
    <input type="text" data-role="type" list="conn-type-datalist" placeholder="类型" value="${esc(connType||'True')}" title="连接类型：Success / Failure / True / False / Stream 或自定义">
    <button class="btn-remove" onclick="removeConnRow('conn-row-${connSeq}')" title="删除此连接">&times;</button>
  `;
  container.appendChild(row);
  rebuildConnDatalist();
}

function removeConnRow(rowId) {
  const row = document.getElementById(rowId);
  if (row) row.remove();
  // 如果没有行了，显示空状态
  const container = document.getElementById('exec-connections-container');
  if (!container.querySelector('.conn-row')) {
    const emptyEl = document.getElementById('conn-rows-empty');
    if (emptyEl) emptyEl.style.display = '';
  }
}

function clearAllConnRows() {
  const container = document.getElementById('exec-connections-container');
  container.querySelectorAll('.conn-row').forEach(r => r.remove());
  const emptyEl = document.getElementById('conn-rows-empty');
  if (emptyEl) emptyEl.style.display = '';
}

function collectConnections() {
  const rows = document.querySelectorAll('#exec-connections-container .conn-row');
  const conns = [];
  rows.forEach(row => {
    const from = row.querySelector('[data-role="from"]').value.trim();
    const to = row.querySelector('[data-role="to"]').value.trim();
    const type = row.querySelector('[data-role="type"]').value;
    if (from && to) {
      conns.push({ from_id: from, to_id: to, type: type });
    }
  });
  return conns;
}

function rebuildConnDatalist() {
  const dl = document.getElementById('conn-id-datalist');
  if (!dl) return;
  const ids = new Set();

  // 从 node_ids 获取
  const nodeStr = document.getElementById('exec-node-ids').value;
  if (nodeStr) nodeStr.split(',').map(s => s.trim()).filter(Boolean).forEach(id => ids.add(id));

  // 从 sub_chain_ids 获取
  const subStr = document.getElementById('exec-sub-ids').value;
  if (subStr) subStr.split(',').map(s => s.trim()).filter(Boolean).forEach(id => ids.add(id));

  // 从已有 connection rows 获取
  document.querySelectorAll('#exec-connections-container [data-role]').forEach(el => {
    const v = el.value.trim();
    if (v) ids.add(v);
  });

  // 从全局缓存的 nodes 和 sub-chains 获取（如果已加载）
  if (typeof _cachedNodes !== 'undefined' && _cachedNodes) {
    _cachedNodes.forEach(n => ids.add(n.node_id));
  }
  if (typeof _cachedSubChains !== 'undefined' && _cachedSubChains) {
    _cachedSubChains.forEach(s => ids.add(s.chain_id));
  }

  dl.innerHTML = [...ids].sort().map(id => `<option value="${esc(id)}">`).join('');
}

// 缓存节点和子链数据，用于 datalist 建议
let _cachedNodes = null;
let _cachedSubChains = null;

async function refreshConnSuggestions() {
  try {
    _cachedNodes = await api('/api/nodes?only_enabled=true');
    _cachedSubChains = await api('/api/sub-chains?only_enabled=true');
    rebuildConnDatalist();
  } catch(e) { /* ignore */ }
}

// 根据参数值推断来源（调用传入/引用节点/固定配置），用于从 DSL arguments 兜底回显
function inferOrchParamPreset(val) {
  const s = (val == null) ? '' : String(val);
  if (!s) return null;
  if (/^\{\{steps\.[^.]+\.(arguments|responses)\.[^}]+?\}\}$/.test(s)) {
    return { src: PARAM_SRC_UPSTREAM, value: s };
  }
  if (/^\{\{[^}]+\}\}$/.test(s)) {
    // 调用传入：{{参数名}}
    return { src: PARAM_SRC_ENTRY, value: s.replace(/^\{\{|\}\}$/g, '') };
  }
  return { src: PARAM_SRC_FIXED, value: s };
}

async function loadToExecuteByIndex(i) {
  const c = window._rootChainsForEdit[i];
  if (!c) return;
  // 列表接口已瘦身，不返回 dsl_json 等大字段；执行需要全量定义，按 ID 单条获取。
  let full;
  try {
    full = await api('/api/root-chains/' + encodeURIComponent(c.chain_id));
  } catch (e) { showToast('加载根链失败: ' + (e.message || e), 'error'); return; }
  loadToExecute(full);
}
// 流程图：跳转到独立编排页（orch.html）的只读预览模式（view=1），
// 与「编排」打开同一页面，但隐藏左侧编辑面板、Live Preview 全宽展示。
function showFlowchartByIndex(i) {
  const c = window._rootChainsForEdit[i];
  if (!c) return;
  const p = encodeURIComponent(getProject() || '');
  location.href = '/orch?target=root&chain_id=' + encodeURIComponent(c.chain_id) + '&view=1' + (p ? '&project=' + p : '');
}
function editRootChainByIndex(i) {
  const c = window._rootChainsForEdit[i];
  if (c) orchOpenInPageRoot(c.chain_id);
}

// extractEntryParams 从 DSL JSON 中提取所有「调用传入」参数（去重，保留 key 与中文名 label）。
// 「调用传入」即节点参数配置中 value 形如 {{arguments.参数key}} 的项；
// 中文名来自 DSL 节点 additionalInfo.node_param_labels（后端在生成 DSL 时按 key→label 注入）。
// 注意：rulego DSL 的节点位于 metadata.nodes（而非 ruleChain.nodes），且
// configuration / additionalInfo 可能为对象或字符串，这里做兼容解析。
function extractEntryParams(dslJson) {
  const result = [];        // [{key, label, private, nodeId}]
  const seen = {};          // 去重键：公共用 key，私有用 nodeId + '.' + key
  try {
    const root = JSON.parse(dslJson || '{}');
    // 优先 metadata.nodes（rulego 原生 DSL 结构），兼容 ruleChain.nodes 写法
    let nodes = (root.metadata && root.metadata.nodes) || [];
    if (!nodes.length && root.ruleChain && root.ruleChain.nodes) {
      nodes = root.ruleChain.nodes;
    }
    // 预建 key -> label 映射（来自所有节点的参数定义）
    const labelMap = {};
    nodes.forEach(node => {
      if (!node) return;
      let addInfo = node.additionalInfo;
      if (typeof addInfo === 'string') {
        try { addInfo = JSON.parse(addInfo); } catch (e) { addInfo = null; }
      }
      const raw = addInfo && addInfo.node_param_labels;
      if (!raw) return;
      let m = raw;
      if (typeof raw === 'string') {
        try { m = JSON.parse(raw); } catch (e) { m = {}; }
      }
      if (m && typeof m === 'object') {
        Object.keys(m).forEach(k => { if (!(k in labelMap)) labelMap[k] = m[k]; });
      }
    });
    // 提取调用传入参数 key（区分公共/私有）
    nodes.forEach(node => {
      if (!node) return;
      // configuration 可能为字符串，兼容解析
      let config = node.configuration;
      if (typeof config === 'string') {
        try { config = JSON.parse(config); } catch (e) { config = null; }
      }
      const args = (config && config.arguments) || [];
      if (!Array.isArray(args)) return;
      args.forEach(a => {
        const val = (a && a.value != null) ? String(a.value) : '';
        const m = val.match(/^\{\{arguments\.([^}]+)\}\}$/);
        if (!m || !m[1]) return;
        const path = m[1];
        let key = path;
        let privateFlag = false;
        let nodeId = '';
        // 二级结构 {{arguments.<nodeId>.<key>}} 视为私有参数
        const dotIdx = path.indexOf('.');
        if (dotIdx > 0) {
          nodeId = path.slice(0, dotIdx);
          key = path.slice(dotIdx + 1);
          privateFlag = true;
        }
        const dedupKey = privateFlag ? (nodeId + '.' + key) : key;
        if (seen[dedupKey]) return;
        seen[dedupKey] = true;
        result.push({ key: key, label: labelMap[key] || '', private: privateFlag, nodeId: nodeId });
      });
    });
  } catch (e) { /* 解析失败则视为无参数 */ }
  return result;
}

// renderExecEntryParams 将调用传入参数渲染到"输入参数"区：
// 每行展示「key（中文名）」+ 一个可输入文本框，供执行时直接填写。
// 私有参数在 key 前加节点 id 前缀标识，data-node/data-private 供执行与接口说明使用。
function renderExecEntryParams(params) {
  const box = document.getElementById('exec-entry-params');
  if (!box) return;
  if (!params || !params.length) {
    box.innerHTML = '<span style="color:var(--text-muted)">暂无调用传入参数</span>';
    return;
  }
  box.innerHTML = params.map(p => {
    const labelHtml = p.label ? '（' + esc(p.label) + '）' : '';
    const displayKey = p.private ? (p.nodeId + '.' + p.key) : p.key;
    return '<div style="margin:6px 0;display:flex;align-items:center">' +
             '<label style="white-space:nowrap;font-family:monospace;font-size:.8rem;color:var(--text);margin-right:8px">' +
               esc(displayKey) + '<span style="color:#2563eb">' + labelHtml + '</span>' +
               (p.private ? '<span style="color:#d97706;font-size:.7rem;margin-left:4px">(私有)</span>' : '') +
             '</label>' +
             '<input type="text" class="exec-entry-input" data-key="' + escAttr(p.key) + '" ' +
               'data-private="' + (p.private ? '1' : '0') + '" data-node="' + escAttr(p.nodeId || '') + '" ' +
               'placeholder="请输入 ' + escAttr(displayKey) + '" ' +
               'style="flex:1;min-width:0;padding:6px 8px;border:1px solid var(--border);border-radius:6px;font-size:.82rem" />' +
           '</div>';
  }).join('');
}

// renderExecApiDoc 在执行结果上方展示接口说明：接口名 + POST 方法 + 参数 JSON 结构（代码界面）
// 动态填充当前项目名、当前 chain_key、当前所选环境。
function renderExecApiDoc() {
  const box = document.getElementById('exec-api-doc');
  if (!box) return;
  if (window._execSubChainId) {
    box.textContent = '子链不能单独对外调用，只能在根链里作为节点使用。\n\n这里的执行用于测试子链本身：按上方「输入参数」作为子链的 arguments 执行草稿 DSL。';
    return;
  }
  const chainIdEl = document.getElementById('exec-chain-id');
  const chainId = chainIdEl ? chainIdEl.value.trim() : '';
  const params = window._execEntryParams || [];
  const payload = {};
  params.forEach(p => {
    const placeholder = '<' + (p.label || p.key) + '>';
    // 私有参数用完整 key（nodeId.key）作为扁平键名，不转成嵌套对象
    const fullKey = p.private ? (p.nodeId + '.' + p.key) : p.key;
    payload[fullKey] = placeholder;
  });
  // 动态取值：项目名 / chain_key / 当前环境
  const project = getProject() || '<项目名>';
  const chainKey = window._execChainKey || '<chain_key>';
  const envEl = document.getElementById('exec-env');
  const env = (envEl && envEl.value) ? envEl.value : '<环境>';
  const bodyObj = {
    project: project,
    chain_key: chainKey,
    metadata: {
      env: env,
      trace_id: '',
    },
    payload: payload,
  };
  box.textContent = 'POST /api/workflow/invoke\n\n请求体 (application/json)：\n' + JSON.stringify(bodyObj, null, 4) + "\n\n备注：需要发布以后，才能执行，永远会执行当前的那条规则链";
}



function loadToExecute(c, isSubChain) {
  // Execute 已改为独立模态框，由 Root Chains / SubChains 列表的 Execute 按钮打开
  openExecuteModal();
  // 子链测试时记下子链 ID，执行改走子链执行接口
  window._execSubChainId = isSubChain ? (c.chain_id || '') : '';
  const resultBox = document.getElementById('exec-result');
  if (resultBox) resultBox.textContent = '点击执行按钮开始...';

  document.getElementById('exec-chain-id').value = c.chain_id || '';
  document.getElementById('exec-chain-name').value = c.name || '';
  document.getElementById('exec-node-ids').value = c.node_ids || '';
  document.getElementById('exec-sub-ids').value = c.sub_chain_ids || '';
  // 保存当前 chain_key，供 renderExecApiDoc 动态填充接口说明
  window._execChainKey = c.chain_key || c.chain_id || '';

  // 列出该流程 DSL 中「调用传入」参数（去重，含中文名），展示在"输入参数"区供填写
  const entryParams = extractEntryParams(c.dsl_json);
  window._execEntryParams = entryParams;
  renderExecEntryParams(entryParams);
  renderExecApiDoc();

  // 清除旧连接行并填充新连接
  clearAllConnRows();
  try {
    const conns = JSON.parse(c.connections_data || '[]');
    conns.forEach(conn => addConnRow(conn.from_id, conn.to_id, conn.type));
  } catch(e) { clearAllConnRows(); }
  if (!document.querySelector('#exec-connections-container .conn-row')) {
    addConnRow('', '', 'True');
  }

  rebuildConnDatalist();
  refreshConnSuggestions();
  window._currentTestCase = null;
  refreshTestCases();
  showToast((isSubChain ? '已加载子链: ' : '已加载根链: ') + esc(c.chain_id), 'success');
}

function openExecuteModal() {
  const overlay = document.getElementById('execute-modal-overlay');
  if (overlay) overlay.classList.add('show');
  // 每次打开重置"输入参数"区，由具体加载函数（loadToExecute）填充
  const box = document.getElementById('exec-entry-params');
  if (box) box.innerHTML = '<span style="color:var(--text-muted)">暂无调用传入参数</span>';
}
function closeExecuteModal() {
  const overlay = document.getElementById('execute-modal-overlay');
  if (overlay) overlay.classList.remove('show');
}

async function executeWorkflow() {
  const resultBox = document.getElementById('exec-result');
  resultBox.textContent = '执行中...';
  const nodeIdsStr = document.getElementById('exec-node-ids').value.trim();
  const subIdsStr = document.getElementById('exec-sub-ids').value.trim();
  const connections = collectConnections();

  const body = {
    project: getProject(),
    chain_id: document.getElementById('exec-chain-id').value.trim(),
    chain_name: document.getElementById('exec-chain-name').value.trim(),
    node_ids: nodeIdsStr ? nodeIdsStr.split(',').map(s=>s.trim()).filter(Boolean) : [],
    sub_chain_ids: subIdsStr ? subIdsStr.split(',').map(s=>s.trim()).filter(Boolean) : [],
    connections: connections,
    payload: safeVal('exec-payload', '{}'),
    debug_mode: safeChecked('exec-debug'),
    use_release: safeChecked('exec-use-release'),
  };
  try {
    const data = await api('/api/workflow/execute', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
    const resultStr = JSON.stringify(data, null, 2);
    resultBox.textContent = resultStr;
    showToast('执行成功', 'success');
    // 若当前已选中某测试用例，将执行结果回写其 last_result，便于下次直接查看
    // （viewer 只读，不能保存测试用例，跳过回写）
    if (window._currentTestCase && !document.body.classList.contains('is-project-viewer')) {
      const c = window._currentTestCase;
      try {
        await api('/api/test-cases', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({
          project: getProject(),
          case_id: c.case_id,
          owner_id: c.owner_id,
          owner_type: c.owner_type,
          name: c.name,
          chain_id: c.chain_id,
          chain_name: c.chain_name,
          node_ids: c.node_ids,
          sub_chain_ids: c.sub_chain_ids,
          connections_data: c.connections_data,
          payload: c.payload,
          debug_mode: c.debug_mode,
          use_release: c.use_release,
          node_param_overrides: c.node_param_overrides,
          last_result: resultStr,
        }) });
      } catch (e) { /* 回写失败不影响主流程 */ }
    }
  } catch (e) {
    resultBox.textContent = 'ERROR: ' + e.message;
    showToast('执行失败: ' + e.message, 'error');
  }
}

// 本地执行 rootChain：直接在当前进程内通过 rulego 执行（/api/workflow/execute）
async function executeWorkflowByMQ() {
  const resultBox = document.getElementById('exec-result');
  resultBox.textContent = '执行中...';

  // 执行环境为必填：后台执行时按环境将数据打入对应的 Redis
  const envName = document.getElementById('exec-env').value;
  if (!envName) { showToast('请先选择执行环境', 'error'); return; }

  // 收集「输入参数」区中用户填写的调用传入参数，组装为 JSON 对象作为执行 payload。
  // 私有参数用完整 key（nodeId.key）作为扁平键名，不转成嵌套对象。
  const args = {};
  document.querySelectorAll('#exec-entry-params .exec-entry-input').forEach(inp => {
    const k = inp.getAttribute('data-key');
    if (!k) return;
    const isPrivate = inp.getAttribute('data-private') === '1';
    if (isPrivate) {
      const nodeId = inp.getAttribute('data-node') || '';
      args[nodeId + '.' + k] = inp.value;
    } else {
      args[k] = inp.value;
    }
  });

  const body = {
    project: getProject(),
    chain_id: document.getElementById('exec-chain-id').value.trim(),
    payload: args,
    env_name: envName,
    use_release: safeChecked('exec-use-release'),
  };
  const subChainId = window._execSubChainId || '';
  const url = subChainId ? '/api/sub-chains/' + encodeURIComponent(subChainId) + '/execute' : '/api/workflow/execute';
  try {
    const data = await api(url, { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
    resultBox.textContent = JSON.stringify(data, null, 2);
    showToast('执行成功', 'success');
  } catch (e) {
    resultBox.textContent = 'ERROR: ' + e.message;
    showToast('执行失败: ' + e.message, 'error');
  }
}

// 填充 Execute Tab 的环境下拉（复用环境配置）
async function refreshExecEnvs() {
  const p = getProject();
  const sel = document.getElementById('exec-env');
  if (!sel) return;
  const cur = sel.value;
  if (!p) { sel.innerHTML = '<option value="">-- 选择环境 --</option>'; return; }
  try {
    const envs = await api('/api/env-configs');
    const list = Array.isArray(envs) ? envs : [];
    // 关键：等数据回来后再一次性替换 innerHTML（而不是先清空再 append）。
    // 否则在连续两次未 await 的调用（如切 tab + 切项目）并发时，
    // 两次 append 会叠加，导致「执行环境」下拉里同一环境出现重复选项。
    const seen = {};
    const frag = document.createDocumentFragment();
    frag.appendChild(new Option('-- 选择环境 --', ''));
    list.forEach(e => {
      if (!e || !e.env_name) return;
      // 按 env_name 去重：仅 trim 前后空格，保留大小写区分（test 与 Test 视为不同环境）
      const name = String(e.env_name).trim();
      if (!name || seen[name]) return;
      seen[name] = true;
      const opt = new Option(name, name);
      opt.textContent = name + (e.description ? ' (' + e.description + ')' : '');
      frag.appendChild(opt);
    });
    sel.innerHTML = '';
    sel.appendChild(frag);
    if (cur && Array.prototype.some.call(sel.options, o => o.value === cur)) sel.value = cur;
  } catch (e) { /* 忽略环境加载失败 */ }
}

// ============================================================
// TestCase 管理
// ============================================================

// 全局：当前选中的测试用例
let _currentTestCase = null;

// owner 类型由 Chain ID 前缀推断：F 开头视为 sub，其余走 root
function inferOwnerType(chainId) {
  if (!chainId) return 'root';
  // 子链 ID 形如 F000012，root 链常为自定义 ID；这里约定：以 F 开头视为 sub
  if (/^F/i.test(chainId)) return 'sub';
  return 'root';
}

// 收集当前 Execute 表单输入，返回测试用例所需字段
function collectTestCaseFields() {
  const nodeIdsStr = document.getElementById('exec-node-ids').value.trim();
  const subIdsStr = document.getElementById('exec-sub-ids').value.trim();
  const connections = collectConnections();
  return {
    chain_id: document.getElementById('exec-chain-id').value.trim(),
    chain_name: document.getElementById('exec-chain-name').value.trim(),
    node_ids: nodeIdsStr ? nodeIdsStr : '',
    sub_chain_ids: subIdsStr ? subIdsStr : '',
    connections_data: JSON.stringify(connections),
    payload: safeVal('exec-payload', '{}'),
    debug_mode: safeChecked('exec-debug'),
    use_release: safeChecked('exec-use-release'),
  };
}

// 把测试用例字段填充回 Execute 表单
function applyTestCaseToForm(c) {
  document.getElementById('exec-chain-id').value = c.chain_id || '';
  document.getElementById('exec-chain-name').value = c.chain_name || '';
  document.getElementById('exec-node-ids').value = c.node_ids || '';
  document.getElementById('exec-sub-ids').value = c.sub_chain_ids || '';
  const payloadEl = document.getElementById('exec-payload'); if (payloadEl) payloadEl.value = c.payload || '{}';
  const debugEl = document.getElementById('exec-debug'); if (debugEl) debugEl.checked = !!c.debug_mode;
  const releaseEl = document.getElementById('exec-use-release'); if (releaseEl) releaseEl.checked = !!c.use_release;

  // 填充连接
  clearAllConnRows();
  try {
    const conns = JSON.parse(c.connections_data || '[]');
    conns.forEach(conn => addConnRow(conn.from_id, conn.to_id, conn.type));
  } catch (e) {}
  if (!document.querySelector('#exec-connections-container .conn-row')) {
    addConnRow('', '', 'True');
  }
  rebuildConnDatalist();
}

async function refreshTestCases() {
  const chainId = document.getElementById('exec-chain-id').value.trim();
  const listEl = document.getElementById('test-case-list');
  if (!chainId) {
    listEl.innerHTML = '<div id="test-case-empty" style="text-align:center;padding:12px;color:var(--text-muted);font-size:.85rem">请先填写 Chain ID</div>';
    return;
  }
  try {
    const cases = await api('/api/test-cases?owner_id=' + encodeURIComponent(chainId));
    if (!cases || cases.length === 0) {
      listEl.innerHTML = '<div id="test-case-empty" style="text-align:center;padding:12px;color:var(--text-muted);font-size:.85rem">暂无测试用例，测试后可点击"保存测试用例"</div>';
      return;
    }
    listEl.innerHTML = '';
    cases.forEach(c => {
      const row = document.createElement('div');
      row.style.cssText = 'display:flex;align-items:center;gap:6px;padding:4px 6px;border-bottom:1px solid #eee';
      row.innerHTML = `
        <button class="btn btn-sm btn-outline" onclick="loadTestCase('${esc(c.case_id)}')" title="加载此用例到表单并执行">▸ ${esc(c.name)}</button>
        <button class="btn btn-sm btn-danger edit-only" onclick="deleteTestCase('${esc(c.case_id)}')" title="删除用例">✕</button>
      `;
      listEl.appendChild(row);
    });
  } catch (e) {
    listEl.innerHTML = '<div style="text-align:center;padding:12px;color:var(--text-muted);font-size:.85rem">加载失败: ' + esc(e.message) + '</div>';
  }
}

async function saveTestCase() {
  const chainId = document.getElementById('exec-chain-id').value.trim();
  const name = document.getElementById('test-case-name').value.trim();
  if (!chainId) { showToast('请先填写 Chain ID', 'error'); return; }
  if (!name) { showToast('请填写用例名称', 'error'); return; }

  const f = collectTestCaseFields();
  const body = {
    project: getProject(),
    owner_id: chainId,
    owner_type: inferOwnerType(chainId),
    name: name,
    chain_id: f.chain_id,
    chain_name: f.chain_name,
    node_ids: f.node_ids,
    sub_chain_ids: f.sub_chain_ids,
    connections_data: f.connections_data,
    payload: f.payload,
    debug_mode: f.debug_mode,
    use_release: f.use_release,
  };
  try {
    const saved = await api('/api/test-cases', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
    showToast('测试用例已保存: ' + esc(saved.case_id), 'success');
    document.getElementById('test-case-name').value = '';
    await refreshTestCases();
  } catch (e) {
    showToast('保存失败: ' + e.message, 'error');
  }
}

async function loadTestCase(caseId) {
  try {
    const c = await api('/api/test-cases/' + encodeURIComponent(caseId));
    window._currentTestCase = c;
    applyTestCaseToForm(c);
    // 若有历史结果，直接展示
    if (c.last_result) {
      document.getElementById('exec-result').textContent = c.last_result;
    } else {
      document.getElementById('exec-result').textContent = '（该用例暂无历史执行结果，点击执行开始测试）';
    }
    showToast('已加载用例: ' + esc(c.name), 'success');
  } catch (e) {
    showToast('加载失败: ' + e.message, 'error');
  }
}

async function deleteTestCase(caseId) {
  if (!confirm('确认删除该测试用例？')) return;
  try {
    await api('/api/test-cases/' + encodeURIComponent(caseId), { method: 'DELETE' });
    if (window._currentTestCase && window._currentTestCase.case_id === caseId) {
      window._currentTestCase = null;
    }
    showToast('已删除', 'success');
    await refreshTestCases();
  } catch (e) {
    showToast('删除失败: ' + e.message, 'error');
  }
}

// ============================================================
// Flowchart (Mermaid)
// ============================================================
// ensureNameMaps 确保节点与子链的中文名缓存已就绪（用于流程图显示中文名）。
// 若缓存为空则并行请求接口补全，避免用户在 root-chains tab 直接点"流程图"时映射缺失。
async function ensureNameMaps() {
  // 始终拉取最新数据，确保拿到当前 project 下所有 node / sub-chain 的中文名，
  // 避免因缓存为空或过期导致流程图只显示 ID。请求失败时降级为空映射。
  try {
    const [nodes, subs] = await Promise.all([
      api('/api/nodes').catch(() => []),
      api('/api/sub-chains').catch(() => [])
    ]);
    window._nodesForEdit = nodes || [];
    window._subChainsForEdit = subs || [];
  } catch (e) {
    window._nodesForEdit = window._nodesForEdit || [];
    window._subChainsForEdit = window._subChainsForEdit || [];
  }
  // 构建 id -> 中文名 映射
  const nameMap = {};
  (window._nodesForEdit || []).forEach(n => { if (n && n.node_id) nameMap[n.node_id] = n.name || ''; });
  (window._subChainsForEdit || []).forEach(c => { if (c && c.chain_id) nameMap[c.chain_id] = c.name || ''; });
  return nameMap;
}

async function showFlowchart(chain) {
  const title = document.getElementById('flowchart-modal-title');
  title.textContent = '流程图: ' + esc(chain.chain_id) + (chain.name ? ' - ' + esc(chain.name) : '');

  const container = document.getElementById('flowchart-mermaid');
  // 清除旧内容
  container.innerHTML = '';
  container.removeAttribute('data-processed');

  // 拉取节点/子链定义（含 kind/name/has_switch_condition），复用与编排 Live Preview 完全相同的渲染逻辑
  await ensureNameMaps();
  const nodes = window._nodesForEdit || [];
  const subChains = window._subChainsForEdit || [];
  const nodeIds = (chain.node_ids || '').split(',').map(s => s.trim()).filter(Boolean);
  const subIds = (chain.sub_chain_ids || '').split(',').map(s => s.trim()).filter(Boolean);
  let conns = [];
  try { conns = JSON.parse(chain.connections_data || '[]'); } catch(e) {}

  const syntax = buildMermaidFromState(nodes, subChains, nodeIds, subIds, conns);
  container.textContent = syntax;

  document.getElementById('flowchart-modal-overlay').classList.add('show');

  try {
    await mermaid.run({ nodes: [container] });
  } catch(e) {
    container.innerHTML = '<p style="color:#ef4444;font-weight:600">渲染失败: ' + esc(e.message) + '</p><pre style="font-size:.75rem;margin-top:8px;white-space:pre-wrap">' + esc(syntax) + '</pre>';
  }
}

function closeFlowchartModal() {
  document.getElementById('flowchart-modal-overlay').classList.remove('show');
}

// ============================================================
// Orchestrate Tab - 流程编排
// ============================================================
let _orchNodes = [];      // 缓存的节点数据
let _orchSubChains = [];  // 缓存的子链数据
let orchConnSeq = 0;
let _orchNodeListFiltered = [];  // 当前过滤后的节点列表（供分页复用）
let _orchNodeListPage = 1;       // 当前页码
let _orchNodeListPageSize = 20;  // 每页条数
let _orchSwitchOverrides = {};   // 每节点实例的 switch_condition 覆盖（key=实例 instanceId，仅本链生效，不改节点定义）
let _orchNameOverrides = {};     // 每节点实例的名称覆盖（key=实例 instanceId，仅本链生效，不改节点定义）
let _orchSwitchEditId = '';      // 当前正在编辑 switch_condition 的节点实例 ID
let _orchSwitchDefaultName = ''; // 当前编辑节点的节点定义默认名称（用于「用节点默认值」复位）

// 编排数据（nodes / sub-chains）按项目缓存本次会话结果，避免重复拉取。
// 初始化时序里 onProjectChange / bootstrap / orchLoadRootChainById 都可能触发 loadOrchData，
// 而 /api/nodes?full=true 是较重接口，重复拉取会产生多次相同请求（同一项目 3 次）。
let _orchDataLoadPromise = null;
let _orchDataLoadedProject = '';

async function loadOrchData(force = false) {
  const p = getProject();
  // 同一项目下并发/重复调用复用同一请求，仅拉取一次；force=true 可强制刷新。
  if (!force && _orchDataLoadPromise && _orchDataLoadedProject === p) {
    return _orchDataLoadPromise;
  }
  _orchDataLoadedProject = p;
  _orchDataLoadPromise = (async () => {
    try {
      // 编排页依赖节点定义的完整 configuration（路由条件、参数/返回值下拉等），
      // 故通过 full=true 获取未瘦身的完整节点列表，避免读取被置空的字段报错。
      _orchNodes = await api('/api/nodes?only_enabled=true&full=true');
      _orchSubChains = await api('/api/sub-chains?only_enabled=true');
      refreshOrchNodeNsFilter();
      refreshOrchNodeTagFilter();
      refreshOrchNodeTypeFilter();
      renderOrchNodeList();
      renderOrchSubList();
      refreshOrchConnOptions();
      onOrchChange();
    } catch (e) {
      _orchDataLoadPromise = null; // 失败则清空缓存，允许后续重试
      showToast('加载编排数据失败: ' + e.message, 'error');
    }
  })();
  return _orchDataLoadPromise;
}

function renderOrchNodeList(filterText, filterNs, filterTag, filterType) {
  const q = (filterText||'').toLowerCase();
  const ns = (filterNs||'').trim();
  const tag = (filterTag||'').trim();
  const type = (filterType||'').trim();
  _orchNodeListFiltered = (_orchNodes || []).filter(n => {
    if (ns && (n.namespace||'') !== ns) return false;
    if (tag && !((n.tags||[]).includes(tag))) return false;
    if (type && (n.type||'') !== type) return false;
    if (!q) return true;
    return (n.node_id||'').toLowerCase().includes(q) || (n.name||'').toLowerCase().includes(q) || (n.type||'').toLowerCase().includes(q);
  });
  // 过滤条件变化，回到第一页
  _orchNodeListPage = 1;
  renderOrchNodeListPage();
}

// 前端分页渲染：每页 _orchNodeListPageSize 条
function renderOrchNodeListPage() {
  const container = document.getElementById('orch-node-list');
  const pager = document.getElementById('orch-node-pager');
  const filtered = _orchNodeListFiltered || [];
  const pageSize = _orchNodeListPageSize;
  const total = filtered.length;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  if (_orchNodeListPage > totalPages) _orchNodeListPage = totalPages;
  if (_orchNodeListPage < 1) _orchNodeListPage = 1;
  const startIdx = (pageSize * (_orchNodeListPage - 1));
  const pageItems = filtered.slice(startIdx, startIdx + pageSize);

  if (!total) {
    container.innerHTML = '<div class="orch-select-empty">无匹配节点</div>';
    if (pager) pager.innerHTML = '';
    return;
  }
  container.innerHTML = pageItems.map(n => {
    const kindClass = n.kind === 'condition' ? 'badge-warning' : (n.kind === 'return' ? 'badge-return' : (n.kind === 'call' ? 'badge-call' : (n.kind === 'input' ? 'badge-input' : (n.kind === 'loop' ? 'badge-loop' : 'badge-info'))));
    const kindLabel = n.kind === 'condition' ? '查询' : (n.kind === 'return' ? '返回值' : (n.kind === 'call' ? '调用根链' : (n.kind === 'input' ? '参数输入' : (n.kind === 'loop' ? '循环' : '执行'))));
    let outputsJson = '[]';
    try { outputsJson = JSON.stringify(n.outputs || []); } catch(e) {}
    const outCount = (function(){
      try { return (JSON.parse(outputsJson) || []).length; } catch(e){ return 0; }
    })();
    return `<label class="orch-select-item" title="${esc(n.node_id)} - ${esc(n.description||'')}">
      <span class="node-id">${esc(n.node_id)}</span>
      ${nodeRouteBadge(n)}
      <span class="node-name">${esc(n.name||n.node_id)}</span>
      <span class="node-type-tag">${esc(n.type)}</span>
      <span class="node-kind badge ${kindClass}">${kindLabel}</span>
      ${outCount ? `<span class="node-out-badge" title="该节点定义了 ${outCount} 个返回值，供下游引用">↩ ${outCount}</span>` : ''}
      <button class="btn btn-sm btn-outline orch-add-btn" type="button" onclick="addOrchNodeInstance('${esc(n.node_id)}')" title="添加到编排（可多次添加同一节点）">+ 添加</button>
    </label>`;
  }).join('');

  if (pager) {
    pager.innerHTML = `
      <button class="btn btn-sm btn-outline" type="button" ${_orchNodeListPage<=1?'disabled':''} onclick="orchNodeListGoPage(${_orchNodeListPage-1})">上一页</button>
      <span class="orch-pager-info">第 ${_orchNodeListPage}/${totalPages} 页 · 共 ${total} 个</span>
      <button class="btn btn-sm btn-outline" type="button" ${_orchNodeListPage>=totalPages?'disabled':''} onclick="orchNodeListGoPage(${_orchNodeListPage+1})">下一页</button>`;
  }
}

// 翻页（不改变过滤条件）
function orchNodeListGoPage(page) {
  _orchNodeListPage = page;
  renderOrchNodeListPage();
}

// 生成全局唯一的实例 ID：nodeId + "__" + 5位 base36 随机串（约 36^5 ≈ 6e7 组合，配合唯一性校验跨子链/删除均不冲突）
function genOrchInstanceId(nodeId) {
  let rnd;
  do {
    rnd = Math.random().toString(36).slice(2, 7);
  } while ((window._orchNodeInstances || []).some(i => i.instanceId === nodeId + '__' + rnd));
  return nodeId + '__' + rnd;
}

// 添加节点实例到编排（同一节点可多次添加，实例 ID 形如 nodeId__<随机段>，全局唯一且不依赖序号）
function addOrchNodeInstance(nodeId) {
  if (!window._orchNodeInstances) window._orchNodeInstances = [];
  const n = (_orchNodes || []).find(x => x.node_id === nodeId);
  const nodeType = n ? n.type : '';
  // end 节点只能添加一个，避免多个不好控制
  if (nodeType === 'end' && window._orchNodeInstances.some(i => i.type === 'end')) {
    showToast('end 节点只能添加一个，如需调整请先删除已添加的 end 节点', 'error');
    return;
  }
  // 调用根链节点：先选择目标根链，选定后再创建实例（见 confirmOrchCallChainPicker）
  if (nodeType === ORCH_CALL_NODE_TYPE && !orchCallDefTarget(nodeId)) {
    openOrchCallChainPicker({ nodeId: nodeId });
    return;
  }
  // 循环节点：先创建实例（默认配置），再弹框配置循环模式与循环体
  if (nodeType === ORCH_LOOP_NODE_TYPE) {
    const loopInstId = genOrchInstanceId(nodeId);
    window._orchNodeInstances.push({
      instanceId: loopInstId,
      nodeId: nodeId,
      name: n ? (n.name || n.node_id) : nodeId,
      type: nodeType,
      kind: n ? (n.kind || 'loop') : 'loop',
      outputs: [],
    });
    _orchParamPreset[loopInstId] = {};
    setOrchLoopConfig(loopInstId, getOrchLoopConfig(loopInstId));
    renderOrchNodeSelected();
    onOrchSelectionChange();
    openOrchLoopEditor(loopInstId);
    return;
  }
  // 参数输入节点：先创建实例（参数列表为空），再弹框定义参数
  if (nodeType === ORCH_INPUT_NODE_TYPE) {
    const inputInstId = genOrchInstanceId(nodeId);
    window._orchNodeInstances.push({
      instanceId: inputInstId,
      nodeId: nodeId,
      name: n ? (n.name || n.node_id) : nodeId,
      type: nodeType,
      kind: n ? (n.kind || 'input') : 'input',
      outputs: [],
    });
    setOrchInputDefs(inputInstId, []);
    renderOrchNodeSelected();
    onOrchSelectionChange();
    openOrchInputParamEditor(inputInstId);
    return;
  }
  // 用随机短码代替序号，避免删除导致空洞、跨子链序号碰撞
  const instanceId = genOrchInstanceId(nodeId);
  window._orchNodeInstances.push({
    instanceId: instanceId,
    nodeId: nodeId,
    name: n ? (n.name || n.node_id) : nodeId,
    type: nodeType,
    kind: n ? (n.kind || 'action') : 'action',
    outputs: n ? (n.outputs || []) : [],
  });
  // 返回值节点（custom/ReturnValue）无节点参数定义，参数即根链返回值定义里的 key；
  // 其余节点按节点定义 params 预置默认来源=调用传入。
  if (nodeType === 'custom/ReturnValue') {
    seedOrchReturnDefaults(instanceId);
  } else {
    seedOrchParamDefaults(instanceId);
  }
  // 条件判断节点：节点定义里写好的参数直接作为本链参数带入，可在「编辑节点」里修改，以编排为准
  if (nodeType === ORCH_COND_NODE_TYPE) setOrchInputDefs(instanceId, orchCondDefsFromNode(nodeId));
  renderOrchNodeSelected();
  onOrchSelectionChange();
}

// 为返回值节点实例预置参数默认值：按根链返回值定义，每个 key 默认来源=调用传入(内部 entry)、值为空
function seedOrchReturnDefaults(instanceId) {
  const inst = (window._orchNodeInstances || []).find(i => i.instanceId === instanceId);
  if (!inst) return;
  const preset = _orchParamPreset;
  (_orchRootResponses || []).forEach(rr => {
    const key = (rr.key || '').trim();
    if (!key) return;
    if (!preset[instanceId]) preset[instanceId] = {};
    if (!preset[instanceId][key]) {
      // 像普通 Activity 节点一样预置完整参数记录（key/label/type/required/policy/value/description）
      preset[instanceId][key] = {
        src: PARAM_SRC_FIXED, value: '', type: rr.type || 'string',
        key: key, label: rr.label || '', required: !!rr.required, description: rr.description || '', policy: 'frontend'
      };
    }
  });
}

// 为新加入的节点实例预置参数默认值：每个参数默认来源=调用传入(内部 entry)
// 仅对"新增"节点生效，编辑已保存链时由 applyOrchParamOverrides 恢复既有配置，不覆盖
function seedOrchParamDefaults(instanceId) {
  const inst = (window._orchNodeInstances || []).find(i => i.instanceId === instanceId);
  if (!inst) return;
  const def = (_orchNodes || []).find(n => n.node_id === inst.nodeId);
  if (!def) return;
  const params = parseNodeParams(def);
  if (!params.length) return;
  const preset = {};
  params.forEach(p => {
    preset[p.key] = { src: PARAM_SRC_ENTRY, value: '' };
  });
  // 不覆盖已存在（理论上新增节点不会存在），确保默认生效
  if (!_orchParamPreset[instanceId]) _orchParamPreset[instanceId] = preset;
}

// 删除指定节点实例
function removeOrchNodeInstance(instanceId) {
  if (!window._orchNodeInstances) return;
  // 链路中间节点（同时存在父节点与子节点）禁止直接删除，避免破坏拓扑
  if (orchNodeHasParentAndChild(instanceId)) {
    showToast('该节点存在父节点与子节点，不能直接删除。请先调整 Connections 连线。', 'error');
    return;
  }
  window._orchNodeInstances = window._orchNodeInstances.filter(i => i.instanceId !== instanceId);
  // 同步清理该实例残留的覆盖配置，避免保存时写入已删除节点的冗余/错误数据
  delete _orchParamPreset[instanceId];
  delete _orchNameOverrides[instanceId];
  delete _orchSwitchOverrides[instanceId];
  // 端点节点（仅 to 或仅 from）删除时，顺带删除 Connections 中与之相关的连线，保持拓扑完整
  removeOrchConnRowsByInstance(instanceId);
  renderOrchNodeSelected();
  onOrchSelectionChange();
}

// 删除所有引用了指定节点实例的连接行（from_id 或 to_id 命中即移除）
function removeOrchConnRowsByInstance(instanceId) {
  if (!instanceId) return;
  document.querySelectorAll('#orch-conn-container .orch-conn-row').forEach(row => {
    const fromSel = row.querySelector('[data-role="orch-from"]');
    const toSel = row.querySelector('[data-role="orch-to"]');
    if ((fromSel && fromSel.value === instanceId) || (toSel && toSel.value === instanceId)) {
      row.remove();
    }
  });
  const container = document.getElementById('orch-conn-container');
  if (container && !container.querySelector('.orch-conn-row')) {
    const emptyEl = document.getElementById('orch-conn-empty');
    if (emptyEl) emptyEl.style.display = '';
  }
  refreshOrchConnOptions();
}

// 判断某节点实例在 Connections 中是否既存在父节点（作为 to 被指向）又存在子节点（作为 from 指向别人）。
// 同时满足时认为该节点处于链路中间，前端禁止直接删除，需先调整连线。
function orchNodeHasParentAndChild(instanceId) {
  if (!instanceId) return false;
  const conns = collectOrchConnections();
  let hasParent = false, hasChild = false;
  conns.forEach(c => {
    if (c.to_id === instanceId) hasParent = true;
    if (c.from_id === instanceId) hasChild = true;
  });
  return hasParent && hasChild;
}

// 从 Live Preview 点击节点触发：二次确认后删除该实例（避免误删）
function removeOrchNodeInstanceConfirm(instanceId) {
  const inst = (window._orchNodeInstances || []).find(i => i.instanceId === instanceId);
  if (!inst) return;
  // 链路中间节点（同时存在父节点与子节点）禁止直接删除，避免破坏拓扑
  if (orchNodeHasParentAndChild(instanceId)) {
    showToast('该节点处于链路中间（同时存在上游父节点与下游子节点），不能直接删除。请先调整 Connections 连线。', 'error');
    return;
  }
  const name = inst.name || instanceId;
  if (!window.confirm('确定要删除该节点实例吗？\n\n节点：' + name + '\n实例ID：' + instanceId + '\n\n删除后将从流程图中移除，且不可撤销。')) return;
  removeOrchNodeInstance(instanceId);
  // 删除后实时刷新预览与参数配置区
  if (typeof renderOrchPreview === 'function') renderOrchPreview();
  if (typeof renderOrchDslPreview === 'function') renderOrchDslPreview();
  if (typeof renderOrchParamOverrides === 'function') renderOrchParamOverrides();
}

// 渲染已选节点实例列表（支持同一节点多次）
function renderOrchNodeSelected() {
  const box = document.getElementById('orch-node-selected');
  const toggle = document.getElementById('orch-selected-toggle');
  if (!box) return;
  const list = window._orchNodeInstances || [];
  if (toggle) toggle.textContent = '已选择的 Nodes (' + list.length + ')';
  if (!list.length) {
    box.innerHTML = '<div class="orch-select-empty" style="padding:8px;font-size:.78rem">尚未添加节点</div>';
    return;
  }
  box.innerHTML = `    <table class="data-table" style="width:100%;font-size:.85rem">
      <thead><tr><th>#</th><th>节点名称</th><th>Node ID</th><th>实例 ID</th><th style="text-align:right">操作</th></tr></thead>
      <tbody>${list.map((i, idx) => {
        // 实例对象只存摘要字段（无 configuration），需按 nodeId 回查完整定义才能判断路由功能
        const def = (_orchNodes || []).find(n => n.node_id === i.nodeId);
        // 链路中间节点（同时存在父节点与子节点）禁止直接删除：隐藏删除按钮，显示锁定提示
        const locked = orchNodeHasParentAndChild(i.instanceId);
        const delCell = locked
          ? `<span class="orch-node-locked" title="该节点处于链路中间（同时存在上游父节点与下游子节点），不能直接删除。请先调整 Connections 连线。">🔒 中间节点</span>`
          : `<button class="btn btn-sm btn-danger" type="button" onclick="removeOrchNodeInstance('${esc(i.instanceId)}')">删除</button>`;
        return `<tr title="${esc(i.instanceId)}">
          <td>${idx + 1}</td>
          <td>${nodeRouteBadge(def)}${esc(i.name)}${isOrchCallInst(i) ? `<div style="font-size:.75rem;color:var(--text-muted)">→ 调用根链：${esc(getOrchCallTargetLabel(i.instanceId) || '未选择')} <button class="btn btn-sm btn-outline edit-only" type="button" onclick="openOrchCallChainPicker({ instanceId: '${esc(i.instanceId)}' })">更换根链</button></div>` : (i.type === ORCH_CALL_NODE_TYPE ? `<div style="font-size:.75rem;color:var(--text-muted)">→ 调用根链：${esc(orchCallTargetText(i))}</div>` : '')}${isOrchInputInst(i) ? `<div style="font-size:.75rem;color:var(--text-muted)">→ ${esc(orchInputDefsSummary(i.instanceId))} <button class="btn btn-sm btn-outline edit-only" type="button" onclick="openOrchInputParamEditor('${esc(i.instanceId)}')">编辑参数</button></div>` : ''}${isOrchLoopInst(i) ? `<div style="font-size:.75rem;color:var(--text-muted)">→ ${esc(orchLoopSummary(i.instanceId))} <button class="btn btn-sm btn-outline edit-only" type="button" onclick="openOrchLoopEditor('${esc(i.instanceId)}')">编辑循环</button></div>` : ''}</td>
          <td class="code-cell">${esc(i.nodeId)}</td>
          <td class="code-cell">${esc(i.instanceId)}</td>
          <td style="text-align:right">${delCell}</td>
        </tr>`;
      }).join('')}</tbody>
    </table>`;
}

// 展开/收起已选节点面板
function openOrchSelectedModal() {
  renderOrchNodeSelected();
  const overlay = document.getElementById('orch-selected-modal-overlay');
  if (overlay) overlay.style.display = 'flex';
}
function closeOrchSelectedModal() {
  const overlay = document.getElementById('orch-selected-modal-overlay');
  if (overlay) overlay.style.display = 'none';
}

// 从保存的 node_ids（可能含实例后缀 baseId__N）还原已选节点实例列表
// dslJson 可选：编辑态传入，用于解析各节点已写入 DSL 的 arguments，作为回显兜底
function restoreOrchNodeInstances(nodeIds, dslJson) {
  // 解析 DSL 中各实例节点的 arguments（配置后的值），用于重新编辑时自动回显 select
  const overrideByInst = {};
  const privateByInst = {}; // { instanceId: [privateKey, ...] } 从 DSL 恢复私有参数标志
  if (dslJson) {
    try {
      const dsl = typeof dslJson === 'string' ? JSON.parse(dslJson) : dslJson;
      // rulego DSL 节点位于 metadata.nodes（兼容 ruleChain.nodes 写法）
      let nodes = (((dsl || {}).metadata || {}).nodes) || [];
      if (!nodes.length) nodes = (((dsl || {}).ruleChain || {}).nodes) || [];
      nodes.forEach(nd => {
        const cfg = (nd.configuration || {});
        const args = cfg.arguments || cfg.Arguments || [];
        if (Array.isArray(args) && args.length) overrideByInst[nd.id] = args;
        // 从 additionalInfo.node_private_params 恢复私有参数 key 列表
        let addInfo = nd.additionalInfo;
        if (typeof addInfo === 'string') { try { addInfo = JSON.parse(addInfo); } catch (e) { addInfo = null; } }
        if (addInfo && addInfo.node_private_params) {
          let pv = addInfo.node_private_params;
          if (typeof pv === 'string') { try { pv = JSON.parse(pv); } catch (e) { pv = null; } }
          if (Array.isArray(pv) && pv.length) privateByInst[nd.id] = pv;
        }
      });
    } catch (e) { /* ignore */ }
  }
  window._orchNodeInstances = (nodeIds || []).map(raw => {
    const baseId = raw.indexOf('__') >= 0 ? raw.split('__')[0] : raw;
    const seq = raw.indexOf('__') >= 0 ? (raw.split('__')[1] || '') : '';
    const n = (_orchNodes || []).find(x => x.node_id === baseId);
    // 应用每节点实例名称覆盖（仅本链生效，不影响节点定义）
    const nameOverride = Object.prototype.hasOwnProperty.call(_orchNameOverrides, raw) && _orchNameOverrides[raw];
    const instName = nameOverride
      ? _orchNameOverrides[raw]
      : (n ? (n.name || baseId) : baseId);
    return {
      instanceId: raw,
      nodeId: baseId,
      name: instName,
      type: n ? n.type : '',
      kind: n ? (n.kind || 'action') : 'action',
      outputs: n ? (n.outputs || []) : [],
      override: overrideByInst[raw] || [],
      privateKeys: privateByInst[raw] || [],
      _seq: seq,
    };
  });
  renderOrchNodeSelected();
}

function filterOrchNodes() {
  const q = document.getElementById('orch-node-search').value;
  const ns = document.getElementById('orch-node-ns-filter').value;
  const tag = document.getElementById('orch-node-tag-filter').value;
  const typeSel = document.getElementById('orch-node-type-filter');
  renderOrchNodeList(q, ns, tag, typeSel ? typeSel.value : '');
}

// 根据当前编排可选项节点刷新命名空间过滤下拉（去重）
function refreshOrchNodeNsFilter() {
  const sel = document.getElementById('orch-node-ns-filter');
  if (!sel) return;
  const cur = sel.value;
  const nss = Array.from(new Set((_orchNodes || []).map(n => (n.namespace || '').trim()).filter(Boolean))).sort();
  sel.innerHTML = '<option value="">全部命名空间</option>' + nss.map(ns => `<option value="${esc(ns)}">${esc(ns)}</option>`).join('');
  sel.value = nss.includes(cur) ? cur : '';
}

// 根据当前编排可选项节点刷新标签过滤下拉（去重）
function refreshOrchNodeTagFilter() {
  const sel = document.getElementById('orch-node-tag-filter');
  if (!sel) return;
  const cur = sel.value;
  const tagSet = {};
  (_orchNodes || []).forEach(n => { (n.tags || []).forEach(t => { if (t) tagSet[t] = true; }); });
  const tags = Object.keys(tagSet).sort();
  sel.innerHTML = '<option value="">全部标签</option>' + tags.map(t => `<option value="${esc(t)}">${esc(t)}</option>`).join('');
  sel.value = tags.includes(cur) ? cur : '';
}

// 根据当前编排可选项节点刷新节点类型过滤下拉（去重）
function refreshOrchNodeTypeFilter() {
  const sel = document.getElementById('orch-node-type-filter');
  if (!sel) return;
  const cur = sel.value;
  const types = Array.from(new Set((_orchNodes || []).map(n => (n.type || '').trim()).filter(Boolean))).sort();
  sel.innerHTML = '<option value="">全部类型</option>' + types.map(t => `<option value="${esc(t)}">${esc(t)}</option>`).join('');
  sel.value = types.includes(cur) ? cur : '';
}

function renderOrchSubList(filterText) {
  const container = document.getElementById('orch-sub-list');
  const q = (filterText||'').toLowerCase();
  // 编辑子链时隐藏自身，避免自引用（目标为 sub 且处于编辑模式）
  const editingSubID = (window._orchTarget === 'sub' &&
    document.getElementById('orch-generate-btn').dataset.edit === '1')
    ? document.getElementById('orch-chain-id').value.trim() : '';
  const filtered = _orchSubChains.filter(s => {
    if (s.chain_id === editingSubID) return false; // 隐藏自己
    if (!q) return true;
    return (s.chain_id||'').toLowerCase().includes(q) || (s.name||'').toLowerCase().includes(q);
  });
  if (!filtered.length) {
    container.innerHTML = '<div class="orch-select-empty">无匹配子链</div>';
    return;
  }
  // 已添加次数以 _orchSelectedSubIds（子链实例 ID）为准：搜索过滤、重新加载列表时都不会丢失
  const counts = {};
  (window._orchSelectedSubIds || []).forEach(id => { const b = orchSubBaseId(id); counts[b] = (counts[b] || 0) + 1; });
  container.innerHTML = filtered.map(s => {
    const n = counts[s.chain_id] || 0;
    return `<label class="orch-select-item" title="${esc(s.chain_id)} - ${esc(s.description||'')}">
      <span class="node-id" style="color:#d97706">${esc(s.chain_id)}</span>
      <span class="node-name">${esc(s.name||s.chain_id)}</span>
      <span class="node-type-tag">sub</span>
      ${n ? `<span class="node-out-badge" title="已添加 ${n} 次，在 Live Preview 中点子链节点的 ✕ 移除">已添加 ${n}</span>` : ''}
      <button class="btn btn-sm btn-outline orch-add-btn" type="button" onclick="addOrchSubInstance('${esc(s.chain_id)}')" title="添加到编排（可多次添加同一子链）">+ 添加</button>
    </label>`;
  }).join('');
}

// 按当前搜索框内容重绘子链列表（更新「已添加 N」）
function rerenderOrchSubList() {
  const list = document.getElementById('orch-sub-list');
  if (!list) return;
  const input = list.previousElementSibling;
  renderOrchSubList(input && input.value ? input.value : '');
}

function filterOrchSubChains() {
  const q = document.querySelector('#orch-sub-list').previousElementSibling.value;
  renderOrchSubList(q);
}

function getSelectedOrchNodeIds() {
  // 返回节点实例 ID 列表（形如 nodeId__N，支持同一节点多次添加）
  return (window._orchNodeInstances || []).map(i => i.instanceId);
}

// 已选子链 ID（按勾选顺序）。不从 DOM 读取：子链列表会因搜索过滤、重新加载而重绘，
// 重绘后的勾选框不代表真实选择，从 DOM 读会把已选子链和它的连线一起丢掉。
window._orchSelectedSubIds = [];

// 子链实例 ID：同一子链可在一条链里添加多次，实例 ID 形如 F000001__ab12c（与节点实例规则一致）；
// 旧数据不带后缀（F000001），实例 ID 即子链 ID。
function orchSubBaseId(id) {
  const s = String(id || '');
  const i = s.indexOf('__');
  return i >= 0 ? s.slice(0, i) : s;
}

// 按子链实例 ID 取子链定义
function orchSubDefOf(id) {
  const base = orchSubBaseId(id);
  return (_orchSubChains || []).find(x => x.chain_id === base);
}

// 生成全局唯一的子链实例 ID：chainId + "__" + 5 位 base36 随机串（不与节点实例、已有子链实例冲突）
function genOrchSubInstanceId(chainId) {
  const used = new Set([...getSelectedOrchNodeIds(), ...(window._orchSelectedSubIds || [])]);
  let id;
  do {
    id = chainId + '__' + Math.random().toString(36).slice(2, 7);
  } while (used.has(id));
  return id;
}

function getSelectedOrchSubIds() {
  return (window._orchSelectedSubIds || []).slice();
}

// 设置已选子链实例（加载已保存的链、新建时清空），并刷新列表上的「已添加 N」
function setOrchSelectedSubIds(ids) {
  const seen = new Set();
  window._orchSelectedSubIds = (ids || []).filter(id => id && !seen.has(id) && seen.add(id));
  rerenderOrchSubList();
}

// 添加子链实例到编排（同一子链可多次添加）
function addOrchSubInstance(chainId) {
  if (!chainId) return;
  if (!window._orchSelectedSubIds) window._orchSelectedSubIds = [];
  window._orchSelectedSubIds.push(genOrchSubInstanceId(chainId));
  rerenderOrchSubList();
  onOrchSelectionChange();
}

// 移除子链实例（流程图上点 ✕）：与节点实例删除规则一致——
// 处于链路中间（既有上游又有下游）时不允许移除，需先调整连线；有相关连线时二次确认并一起删除。
// 返回是否已移除。
function removeOrchSubChain(instanceId) {
  if (!(window._orchSelectedSubIds || []).includes(instanceId)) return false;
  if (orchNodeHasParentAndChild(instanceId)) {
    showToast('该子链处于链路中间（同时存在上游与下游连线），不能直接移除。请先调整 Connections 连线。', 'error');
    return false;
  }
  const related = collectOrchConnections().filter(c => c.from_id === instanceId || c.to_id === instanceId).length;
  if (related > 0) {
    const s = orchSubDefOf(instanceId);
    const name = (s && s.name) || orchSubBaseId(instanceId);
    if (!window.confirm('确定要移除该子链吗？\n\n子链：' + name + '\n实例ID：' + instanceId + '\n\n与它相关的 ' + related + ' 条连线会一起删除。')) return false;
  }
  window._orchSelectedSubIds = window._orchSelectedSubIds.filter(id => id !== instanceId);
  // 同步清理该子链实例的参数配置、名称覆盖与路由条件，避免保存时写入已移除实例的冗余数据
  delete _orchParamPreset[instanceId];
  delete _orchNameOverrides[instanceId];
  delete _orchSwitchOverrides[instanceId];
  rerenderOrchSubList();
  removeOrchConnRowsByInstance(instanceId);
  onOrchSelectionChange();
  return true;
}

function onOrchSelectionChange() {
  const nodeCount = (window._orchNodeInstances || []).length;
  const subCount = getSelectedOrchSubIds().length;
  document.getElementById('orch-node-count').textContent = nodeCount + ' selected';
  document.getElementById('orch-sub-count').textContent = subCount + ' selected';
  refreshOrchConnOptions();
  onOrchChange();
}

// Refresh connection dropdown options based on selected items
// 生成 Connections 下拉的 option HTML：仅展示中文名（与右侧 Live Preview 名称一致），
// 同一中文名出现多次时（同一 node 多次添加）附加实例后缀便于区分，使其与右侧 Id: 标识对应。
function orchConnOptionsHtml() {
  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();
  const items = [];
  nodeIds.forEach(id => {
    const inst = (window._orchNodeInstances || []).find(i => i.instanceId === id);
    // 与右侧 Live Preview 一致：用实例生效名（优先 _orchNameOverrides 名称覆盖），
    // 不能用 inst.name（实例缓存重建会被重置回节点定义名）。
    const def = orchNodeDefById((inst && inst.nodeId) || id);
    const name = orchInstanceDisplayName(inst, def) || id;
    items.push({ id: id, name: name, prefix: '⚙' });
  });
  subIds.forEach(id => {
    const s = orchSubDefOf(id);
    items.push({ id: id, name: (s && s.name) ? s.name : id, prefix: '🔗' });
  });
  // 按 ID 排序（数字按自然序），便于在下拉中快速定位节点；Node（N…）自然排在 Sub Chain 前
  items.sort((a, b) => String(a.id).localeCompare(String(b.id), undefined, { numeric: true }));
  // 名称后始终附加完整实例 ID（如 N000034__hflz1），方便区分，与右侧 Live Preview 的 Id: 标识对应
  return items.map(it => {
    const label = it.name + ' · ' + it.id;
    return `<option value="${esc(it.id)}">${it.prefix} ${esc(label)}</option>`;
  }).join('');
}

// 给连接行的 from/to 下拉设置值。值不在当前可选项里（节点/子链已移除或失效）时，
// 追加一个「已失效」选项保持原值，不能让浏览器默认选中第一项，否则连线会被悄悄改到别的节点上。
function setOrchConnSelectValue(sel, val) {
  if (!sel || !val) return;
  if (![...sel.options].some(o => o.value === val)) {
    const opt = document.createElement('option');
    opt.value = val;
    opt.textContent = '⚠ 已失效 · ' + val;
    sel.appendChild(opt);
  }
  sel.value = val;
}

function refreshOrchConnOptions() {
  const allOpts = orchConnOptionsHtml();

  // Update all existing connection rows' select elements
  document.querySelectorAll('#orch-conn-container .orch-conn-row').forEach(row => {
    ['[data-role="orch-from"]', '[data-role="orch-to"]'].forEach(q => {
      const sel = row.querySelector(q);
      if (!sel) return;
      const curVal = sel.value;
      sel.innerHTML = allOpts || '<option value="">-- 请选择 --</option>';
      setOrchConnSelectValue(sel, curVal);
    });
    syncOrchConnRowTooltip(row);
  });
  refreshOrchConnWarns(); // 节点名称/路由条件变化后同步校验
}

// 鼠标移到 Connection 的 select 上时，用 title 显示完整选中名称（select 自身较窄会截断）
function syncOrchConnRowTooltip(row) {
  if (!row) return;
  ['orch-from', 'orch-to'].forEach(role => {
    const sel = row.querySelector('[data-role="' + role + '"]');
    if (!sel) return;
    const opt = sel.selectedOptions && sel.selectedOptions[0];
    sel.title = opt ? opt.textContent : sel.value;
  });
}

function addOrchConnRow(fromId, toId, connType) {
  orchConnSeq++;
  // 新增连接（未传 connType）默认关系为 Success；加载已有连接时按原值回显（包括空字符串），不改动已保存的数据
  if (connType === undefined || connType === null) connType = 'Success';
  const container = document.getElementById('orch-conn-container');
  const emptyEl = document.getElementById('orch-conn-empty');
  if (emptyEl) emptyEl.style.display = 'none';

  const allOpts = orchConnOptionsHtml();

  const row = document.createElement('div');
  row.className = 'orch-conn-row';
  row.id = 'orch-conn-row-' + orchConnSeq;
  row.innerHTML = `
    <select data-role="orch-from">${allOpts || '<option value="">-- 请选择 --</option>'}</select>
    <span class="conn-arrow">→</span>
    <select data-role="orch-to">${allOpts || '<option value="">-- 请选择 --</option>'}</select>
    <input type="text" data-role="orch-type" class="conn-type-sel" list="conn-type-datalist" placeholder="Success" value="${esc(connType||'')}" title="连接类型：新增默认 Success（可改为 Failure / True / False / Stream 或自定义）">
    <button type="button" class="orch-cond-warn" data-role="orch-cond-warn" style="display:none" onclick="onOrchConnWarnClick(this)" title=""></button>
    <button class="btn-remove" onclick="removeOrchConnRow('orch-conn-row-${orchConnSeq}')" title="删除">&times;</button>
  `;
  container.appendChild(row);

  // Set initial values
  setOrchConnSelectValue(row.querySelector('[data-role="orch-from"]'), fromId);
  setOrchConnSelectValue(row.querySelector('[data-role="orch-to"]'), toId);

  // 悬停显示完整选中名称
  syncOrchConnRowTooltip(row);

  // Trigger preview update；切换选项时同步刷新 tooltip
  row.querySelector('[data-role="orch-from"]').addEventListener('change', () => { syncOrchConnRowTooltip(row); onOrchChange(); });
  row.querySelector('[data-role="orch-to"]').addEventListener('change', () => { syncOrchConnRowTooltip(row); onOrchChange(); });
  row.querySelector('[data-role="orch-type"]').addEventListener('input', onOrchChange);
  refreshOrchConnWarns();
}

// ============================================================
// Connections 自定义关系必须配置路由条件（switch_condition）的校验
// 规则：relationType 只要不是 Success / Failure（留空视为 Success），
// 起点节点就必须配置 switch_condition，否则运行时该分支根本不会命中。
// ============================================================
const ORCH_SKIP_ROUTE_TYPES = ['success', 'failure'];

// 该连接类型是否需要起点节点配置 switch_condition
function orchConnNeedsSwitch(connType) {
  const t = String(connType == null ? '' : connType).trim().toLowerCase();
  if (t === '') return false; // 留空 = Success
  return ORCH_SKIP_ROUTE_TYPES.indexOf(t) < 0;
}

// 实例在本链中生效的展示名（与右侧 Live Preview 一致）。
// 优先级：本链名称覆盖 _orchNameOverrides > 实例名 inst.name（保存名称时已同步） > 节点定义名。
// 注意不要直接用 def.name：那只是节点定义名，用户在图上改名后这里会显示旧名。
function orchInstanceDisplayName(inst, def) {
  const id = inst ? inst.instanceId : '';
  const ov = (id && _orchNameOverrides) ? _orchNameOverrides[id] : '';
  if (ov && String(ov).trim() !== '') return String(ov).trim();
  if (inst && inst.name) return String(inst.name);
  return (def && def.name) || '';
}

// 按 nodeId 取节点定义。
// 注意：_orchNodes 是本文件顶层的 let 声明，【不会挂到 window 上】（这与 window._orchNodeInstances 不同），
// 必须直接引用该词法变量；写 window._orchNodes 会恒为 undefined，导致校验全部误判为空。
function orchNodeDefById(nodeId) {
  const list = (typeof _orchNodes !== 'undefined' && _orchNodes) ? _orchNodes : (window._orchNodes || []);
  return list.find(x => x.node_id === nodeId) || null;
}

// 取该实例【在本页面上实际生效】的 switch_condition。
// 优先级：本链实例覆盖（node_switch_overrides，页面 🔀 编辑写入） > 节点定义默认值。
// 返回 known=false 表示「无法确定」（节点定义不在缓存中，如节点已禁用），调用方不应据此判空。
function orchInstanceSwitchInfo(instanceId) {
  if (!instanceId) return { known: false, text: '', from: '' };
  if (Object.prototype.hasOwnProperty.call(_orchSwitchOverrides || {}, instanceId)) {
    const v = _orchSwitchOverrides[instanceId];
    return { known: true, text: String(v == null ? '' : v).trim(), from: 'override' };
  }
  const baseId = String(instanceId).split('__')[0] || instanceId;
  const n = orchNodeDefById(baseId);
  if (!n) return { known: false, text: '', from: '' };
  return { known: true, text: String(nodeSwitchConditionText(n) || '').trim(), from: 'def' };
}

// 兼容旧调用：只需「生效文本」的场景
function orchInstanceSwitchText(instanceId) {
  return orchInstanceSwitchInfo(instanceId).text;
}

// 判断连接起点类别：node(可配路由) / sub(子链) / unsupported(节点类型不支持) / unknown(未找到定义)
function orchConnFromKind(fromId) {
  if (!fromId) return 'none';
  const inst = (window._orchNodeInstances || []).find(x => x.instanceId === fromId);
  if (inst) {
    let t = inst.type || '';
    // 实例摘要缺 type 时回查节点定义（baseId = instanceId 去掉 __后缀）
    if (!t) {
      const n = orchNodeDefById(String(fromId).split('__')[0] || fromId);
      t = n ? (n.type || '') : '';
    }
    return (t === 'custom/Activity' || t === 'custom/CondSwitch') ? 'node' : 'unsupported';
  }
  const subs = (typeof _orchSubChains !== 'undefined' && _orchSubChains) ? _orchSubChains : (window._orchSubChains || []);
  if (getSelectedOrchSubIds().includes(fromId) || subs.some(s => s.chain_id === orchSubBaseId(fromId))) return 'sub';
  return 'unknown';
}

// 刷新每行的路由条件警告；返回必须修复的问题列表（起点可配路由但 switch_condition 为空）
// 同时把「默认分支却仍配了路由条件」的冲突写入 window._orchConnConflicts（保存时提示用）
function refreshOrchConnWarns() {
  const problems = [];
  const conflicts = [];
  const rows = Array.from(document.querySelectorAll('#orch-conn-container .orch-conn-row'));
  rows.forEach(row => {
    const fromSel = row.querySelector('[data-role="orch-from"]');
    const typeEl = row.querySelector('[data-role="orch-type"]');
    const warnEl = row.querySelector('[data-role="orch-cond-warn"]');
    const fromId = fromSel ? fromSel.value : '';
    const type = typeEl ? typeEl.value : '';
    let level = '';
    let msg = '';
    if (fromId && !orchConnNeedsSwitch(type)) {
      // 反向校验：Success / Failure（留空=Success）是默认分支，若起点仍配了 switch_condition，
      // 输出会按条件走自定义分支，该 Success/Failure 分支很可能不会命中 → 流程走错
      const kind = orchConnFromKind(fromId);
      const subSw = kind === 'sub' ? String((_orchSwitchOverrides || {})[fromId] || '').trim() : '';
      if (subSw && String(type).trim() !== 'Failure') {
        level = 'conflict';
        msg = '关系「' + (String(type).trim() || 'Success') + '」是默认分支，但起点子链实例 ' + fromId + ' 配置了 switch_condition，' +
              '输出会按条件走自定义分支，该分支很可能不会命中。可点击此处清空该子链实例的路由条件。';
        conflicts.push({ row: row, fromId: fromId, type: String(type).trim() || 'Success', from: 'override' });
      } else if (kind === 'node') {
        const info = orchInstanceSwitchInfo(fromId);
        if (info.known && info.text) {
          level = 'conflict';
          const srcTxt = info.from === 'override' ? '本链实例设置（页面 🔀 编辑）' : '节点默认定义';
          const howTxt = info.from === 'override'
            ? '可在本页点击此处把该实例的 switch_condition 清空（注意：清空后会回退到节点定义值，若节点定义也有值需一并清除）'
            : '该值来自节点定义，需到 Node 编辑页清除；仅在本页清空覆盖会回退回节点定义值';
          msg = '关系「' + (String(type).trim() || 'Success') + '」是默认分支，但起点 ' + fromId + ' 仍配置了 switch_condition（来源：' +
                srcTxt + '）。配置路由条件后，输出会按条件走自定义分支，该 ' + (String(type).trim() || 'Success') +
                ' 分支很可能不会命中，流程会走错。' + howTxt + '。';
          conflicts.push({ row: row, fromId: fromId, type: String(type).trim() || 'Success', from: info.from });
        }
      }
    } else if (orchConnNeedsSwitch(type) && fromId) {
      const kind = orchConnFromKind(fromId);
      if (kind === 'node') {
        const info = orchInstanceSwitchInfo(fromId);
        if (!info.known) {
          // 节点定义不在页面缓存中（如已禁用），无法确定 → 只橙色提示，绝不判为空，避免误报
          level = 'warn';
          msg = '关系「' + String(type).trim() + '」需要路由条件，但未加载到起点 ' + fromId +
                ' 的节点定义（可能已禁用），无法确认是否已配置。';
        } else if (!info.text) {
          level = 'error';
          msg = '关系「' + String(type).trim() + '」需要路由条件，但起点 ' + fromId +
                '（' + (info.from === 'override' ? '本链实例设置' : '节点默认定义') + '）的 switch_condition 为空，' +
                '运行时不会命中该分支。点击此处配置。';
        }
      } else if (kind === 'sub') {
        // 子链实例未配路由条件时沿用子链结束关系（子链可能本身就以该关系结束），只提示不拦截
        if (!String((_orchSwitchOverrides || {})[fromId] || '').trim()) {
          level = 'warn';
          msg = '关系「' + String(type).trim() + '」需要路由条件：起点子链实例 ' + fromId + ' 未配置 switch_condition，' +
                '将沿用子链结束时的关系。请确认子链会以该关系结束，或点击此处给该子链实例配置路由条件。';
        }
      } else if (kind === 'unsupported') {
        level = 'warn';
        msg = '关系「' + String(type).trim() + '」需要路由条件，但起点节点类型不支持 switch_condition（仅 custom/Activity 与 custom/CondSwitch 支持）。';
      } else {
        level = 'warn';
        msg = '关系「' + String(type).trim() + '」需要路由条件，但未找到起点节点定义，无法校验 switch_condition。';
      }
    }
    row.classList.toggle('conn-row-cond-error', level === 'error');
    row.classList.toggle('conn-row-cond-warn', level === 'warn' || level === 'conflict');
    if (warnEl) {
      warnEl.style.display = level ? 'inline-flex' : 'none';
      warnEl.dataset.level = level;
      warnEl.title = msg;
      warnEl.textContent = level === 'error' ? '⚠️ 缺路由条件' : (level === 'conflict' ? '⚠️ 路由冲突' : '⚠️');
    }
    if (level === 'error') problems.push({ row: row, fromId: fromId, type: String(type).trim() });
  });
  window._orchConnConflicts = conflicts;
  updateOrchConnCondSummary(problems.length, conflicts.length);
  return problems;
}

// Connections 区块顶部的汇总提示
function updateOrchConnCondSummary(count, conflictCount) {
  const el = document.getElementById('orch-conn-cond-summary');
  if (!el) return;
  if (!count && !conflictCount) { el.style.display = 'none'; el.innerHTML = ''; return; }
  el.style.display = 'block';
  let html = '';
  if (count) {
    html += '<div>⚠️ 有 ' + count + ' 条连接使用了自定义关系（非 Success/Failure），但起点节点的 switch_condition 为空，' +
            '运行时不会命中这些分支。请点击行内的「⚠️ 缺路由条件」配置。</div>';
  }
  if (conflictCount) {
    html += '<div class="orch-cond-summary-conflict">⚠️ 有 ' + conflictCount +
            ' 条连接的关系是 Success/Failure（含留空），但起点仍配置了 switch_condition，' +
            '输出会走自定义分支导致该连接可能不命中、流程走错。请点击行内的「⚠️ 路由冲突」清空。</div>';
  }
  el.innerHTML = html;
}

// 点击行内警告：直接打开起点节点的 switch_condition 编辑器
function onOrchConnWarnClick(btn) {
  const row = btn && btn.closest ? btn.closest('.orch-conn-row') : null;
  if (!row) return;
  const fromSel = row.querySelector('[data-role="orch-from"]');
  const id = fromSel ? fromSel.value : '';
  if (!id) { showToast('请先选择起点节点', 'error'); return; }
  orchOpenNodeSwitchEditor(id);
}

function removeOrchConnRow(rowId) {
  const row = document.getElementById(rowId);
  if (row) row.remove();
  const container = document.getElementById('orch-conn-container');
  if (!container.querySelector('.orch-conn-row')) {
    const emptyEl = document.getElementById('orch-conn-empty');
    if (emptyEl) emptyEl.style.display = '';
  }
  onOrchChange();
}

// Connections 列表排序：按 from 实例 ID 自然序（数字 ID 在前、随机后缀在后，与下拉排序一致），
// from 相同再按 relationType 排（留空视为 Success），再按 to 排，使同一节点的连接排在一起；
// 以上都相同的多条保持原有相对顺序（稳定排序）。
// 在加载链、图上拖拽新增连接后及点「刷新排序」时调用；未选 from 的空行排在最后。
function sortOrchConnRows() {
  const container = document.getElementById('orch-conn-container');
  if (!container) return;
  const rows = Array.from(container.querySelectorAll(':scope > .orch-conn-row'));
  if (rows.length < 2) return;
  const cmp = (a, b) => {
    if (!a && !b) return 0;
    if (!a) return 1;
    if (!b) return -1;
    return a.localeCompare(b, undefined, { numeric: true });
  };
  const keyed = rows.map((row, idx) => ({
    row, idx,
    from: row.querySelector('[data-role="orch-from"]').value,
    to: row.querySelector('[data-role="orch-to"]').value,
    type: row.querySelector('[data-role="orch-type"]').value.trim() || 'Success',
  }));
  keyed.sort((x, y) => cmp(x.from, y.from) || cmp(x.type, y.type) || cmp(x.to, y.to) || (x.idx - y.idx));
  keyed.forEach(k => container.appendChild(k.row));
}

// 「刷新排序」按钮：手动「+ 添加连接」的行固定追加在末尾（编辑时不自动重排），点此按规则统一重新排序
function refreshOrchConnSort() {
  if (!document.querySelector('#orch-conn-container .orch-conn-row')) return;
  sortOrchConnRows();
  onOrchChange();
  showToast('连接列表已重新排序', 'success');
}

function collectOrchConnections() {
  const rows = document.querySelectorAll('#orch-conn-container .orch-conn-row');
  const conns = [];
  rows.forEach(row => {
    const from = row.querySelector('[data-role="orch-from"]').value;
    const to = row.querySelector('[data-role="orch-to"]').value;
    const type = row.querySelector('[data-role="orch-type"]').value;
    if (from && to) conns.push({ from_id: from, to_id: to, type: type });
  });
  return conns;
}

// 通用 Mermaid 流程图构建：编排 Live Preview 与 Root Chains 流程图共用同一套着色/标签逻辑（无重复代码）
// nodes/subChains 为节点/子链定义数组（需含 node_id/chain_id、kind、name、has_switch_condition 等字段）
function buildMermaidFromState(nodes, subChains, nodeIds, subIds, conns) {
  nodes = nodes || [];
  subChains = subChains || [];
  conns = conns || [];

  const allIds = new Set([...nodeIds, ...subIds]);
  conns.forEach(c => { allIds.add(c.from_id); allIds.add(c.to_id); });

  const nodeSet = new Set(nodeIds);
  const subSet = new Set(subIds);

  // Mermaid-safe IDs
  const idMap = {};
  let counter = 0;
  allIds.forEach(id => {
    let safe = id.replace(/[^a-zA-Z0-9_\u4e00-\u9fff]/g, '_');
    if (!safe || /^\d/.test(safe)) safe = 'id' + safe;
    while (Object.values(idMap).includes(safe)) safe = safe + '_' + (++counter);
    idMap[id] = safe;
  });

  let lines = ['flowchart TD'];
  // 按节点 kind 分组收集 safe id，动态生成配色 classDef
  const kindSafeIds = {};
  nodeSet.forEach(id => {
    const safe = idMap[id];
    const inst = (window._orchNodeInstances || []).find(i => i.instanceId === id);
    // id 是实例 ID（nodeId__随机段），需按 nodeId（去后缀）查节点定义，才能取到 has_switch_condition / configuration
    const nodeId = inst ? inst.nodeId : id.split('__')[0];
    const n = nodes.find(x => x.node_id === nodeId);
    const baseId = nodeId;
    // 优先使用本链实例名称覆盖(_orchNameOverrides)，与节点列表/参数区标题显示一致；
    // 不能只看 inst.name（实例缓存重建时会被重置回定义名，导致预览图还原成原名）。
    const cnName = orchInstanceDisplayName(inst, n);
    // 返回值节点（custom/ReturnValue）固定用 return 配色（浅黄），直接按节点类型判定，
    // 不依赖 kind 字段值，避免 kind 不一致时退化成 action（红）。
    const isReturnNode = (n && n.type === 'custom/ReturnValue') || (inst && inst.type === 'custom/ReturnValue');
    const k = isReturnNode ? 'return' : ((n && n.kind) || 'action');
    // 返回值节点不参与「路由」着色（保持专属浅黄），其余节点按 has_switch_condition 判定路由（淡蓝）。
    const routing = !isReturnNode && !!(n && (typeof n.has_switch_condition === 'boolean'
      ? n.has_switch_condition : nodeSwitchConditionText(n) !== ''));
    const clsName = 'nodeKind_' + k.replace(/[^a-zA-Z0-9_]/g, '_') + (routing ? '_route' : '');
    // 标签：第一行路由标记 + 中文名，第二行 node ID，第三行 实例 ID（同一节点多次添加时区分）
    const labelLine = `<b>${orchNodeRouteMark(n)}⚙ ${esc(cnName)}</b>`;
    const idLine = `<small>NodeId: ${esc(baseId)}</small>`;
    const instLine = inst && inst.instanceId !== baseId ? `<small>Id: ${esc(inst.instanceId)}</small>` : '';
    const callLine = (inst && inst.type === ORCH_CALL_NODE_TYPE) ? `<small>→ 调用根链: ${esc(orchCallTargetText(inst))}</small>`
      : (isOrchInputInst(inst) ? `<small>→ ${esc(orchInputDefsSummary(inst.instanceId))}</small>`
        : (isOrchLoopInst(inst) ? `<small>→ ${esc(orchLoopSummary(inst.instanceId))}</small>` : ''));
    // 该实例生效的 switch_condition（覆盖值优先，其次节点默认；仅 Activity / CondSwitch 有意义）直接显示在节点内：
    // 用醒目配色标识，最多两行、超出省略（样式见 .orch-sw-cond），取代原来的悬停泡泡层（会引起布局抖动）
    let swLine = '';
    if (n && (n.type === 'custom/Activity' || n.type === 'custom/CondSwitch')) {
      const effExpr = Object.prototype.hasOwnProperty.call(_orchSwitchOverrides, id)
        ? _orchSwitchOverrides[id] : nodeSwitchConditionText(n);
      const expr = String(effExpr || '').replace(/\s+/g, ' ').trim();
      // 不能写行内 style：Mermaid 会对含「style…:…#…;」的标签文本删掉最后一个分号，破坏 #39; 等实体；
      // 文字颜色改由 common.css 中更高优先级的选择器（带容器 ID）覆盖 classDef 配色
      if (expr) swLine = `<span class='orch-sw-cond'>⑂ ${escMermaidText(expr)}</span>`;
    }
    lines.push(`    ${safe}["${labelLine}<br/>${idLine}${instLine ? '<br/>'+instLine : ''}${callLine ? '<br/>'+callLine : ''}${swLine}"]`);
    (kindSafeIds[clsName] = kindSafeIds[clsName] || []).push(safe);
  });
  subSet.forEach(id => {
    const safe = idMap[id];
    // 子链实例 ID（F000001__ab12c）按子链 ID 取定义；不带后缀的旧数据实例 ID 即子链 ID
    const baseId = String(id).split('__')[0];
    const s = subChains.find(x => x.chain_id === baseId);
    const label = s && s.name ? s.name : id;
    const subLabel = s && s.name ? id : '';
    // 子链实例配置了路由条件时与路由节点一样在节点内显示条件（样式见 .orch-sw-cond）
    const subExpr = String((_orchSwitchOverrides || {})[id] || '').replace(/\s+/g, ' ').trim();
    const subSwLine = subExpr ? `<span class='orch-sw-cond'>⑂ ${escMermaidText(subExpr)}</span>` : '';
    lines.push(`    ${safe}(["<b>🔗 ${esc(label)}</b>${subLabel ? '<br/><small>'+esc(subLabel)+'</small>' : ''}${subSwLine}"])`);
  });
  allIds.forEach(id => {
    if (!nodeSet.has(id) && !subSet.has(id)) {
      const safe = idMap[id];
      lines.push(`    ${safe}["<b>? ${esc(id)}</b>"]`);
    }
  });
  // 合并相同 from->to 的连线：relationType 用逗号分隔统一在一条线上展示，减少线条使图更清晰
  const mergedEdges = [];
  const mergedEdgeMap = {};
  conns.forEach(c => {
    const from = idMap[c.from_id];
    const to = idMap[c.to_id];
    if (!from || !to) return;
    const key = c.from_id + '|' + c.to_id;
    const t = c.type || 'Success';
    if (mergedEdgeMap[key]) {
      if (!mergedEdgeMap[key].types.includes(t)) mergedEdgeMap[key].types.push(t);
    } else {
      const entry = { from: from, to: to, types: [t] };
      mergedEdgeMap[key] = entry;
      mergedEdges.push(entry);
    }
  });
  // 暴露边映射供 Live Preview 点击连线标签时反查 from/to 实例 ID
  const edgeMap = {};
  conns.forEach(c => {
    const from = idMap[c.from_id];
    const to = idMap[c.to_id];
    if (from && to) edgeMap['L_' + from + '_' + to] = { from_id: c.from_id, to_id: c.to_id };
  });
  window._orchEdgeMap = edgeMap;
  mergedEdges.forEach(m => {
    lines.push(`    ${m.from} -->|${m.types.join(',')}| ${m.to}`);
  });
  const subClasses = [...subSet].map(id => idMap[id]).filter(Boolean).join(',');
  // 根据节点 kind 类型动态生成不同背景色（每种 kind 一组 classDef）
  // condition=查询获取（浅绿）；action=策略执行（淡红）；带路由功能的节点统一用淡蓝背景覆盖
  const kindPalette = {
    condition: { fill: '#dcfce7', stroke: '#16a34a', color: '#14532d' }, // 查询获取=浅绿
    action:    { fill: '#fee2e2', stroke: '#dc2626', color: '#7f1d1d' }, // 策略执行=淡红
    return:    { fill: '#fef9c3', stroke: '#ca8a04', color: '#713f12' }, // 返回值节点=浅黄（区别于其它节点）
    call:      { fill: '#e0f2fe', stroke: '#0891b2', color: '#164e63' }, // 调用根链节点=浅青
    input:     { fill: '#f3e8ff', stroke: '#9333ea', color: '#581c87' }, // 参数输入节点=浅紫
    loop:      { fill: '#ffedd5', stroke: '#ea580c', color: '#7c2d12' }, // 循环节点=浅橙
  };
  const kindFallback = { fill: '#f3f4f6', stroke: '#9ca3af', color: '#374151' }; // 未知 kind 默认灰
  const routeFill = { fill: '#dbeafe', stroke: '#2563eb', color: '#1e3a8a' }; // 路由=淡蓝
  Object.keys(kindSafeIds).forEach(clsName => {
    const routing = clsName.endsWith('_route');
    // clsName 形如 nodeKind_condition / nodeKind_action / nodeKind_condition_route，需去掉前缀再查 palette
    const baseKey = clsName.replace(/^nodeKind_/, '').replace(/_route$/, '');
    const c = routing ? routeFill : (kindPalette[baseKey] || kindFallback);
    lines.push(`    classDef ${clsName} fill:${c.fill},stroke:${c.stroke},color:${c.color},stroke-width:2px,text-align:left`);
    lines.push(`    class ${kindSafeIds[clsName].join(',')} ${clsName}`);
  });
  lines.push('    classDef subCls fill:#ede9fe,stroke:#7c3aed,color:#4c1d95,stroke-width:2px,text-align:left');
  if (subClasses) lines.push(`    class ${subClasses} subCls`);
  // 暴露 id 映射供预览图叠加删除叉时反向查询原始实例 ID
  window._orchIdMap = idMap;
  return lines.join('\n');
}

// Build Mermaid syntax for orchestration preview（复用通用构建函数）
function orchBuildMermaid() {
  return buildMermaidFromState(
    _orchNodes,
    _orchSubChains,
    getSelectedOrchNodeIds(),
    getSelectedOrchSubIds(),
    collectOrchConnections()
  );
}

// Build DSL preview JSON (approximate)
function orchBuildDslPreview() {
  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();
  const conns = collectOrchConnections();

  const nodes = nodeIds.map(id => {
    // id 是实例 ID（nodeId__随机段），需从 _orchNodeInstances 取实例，再按 nodeId 查定义
    const inst = (window._orchNodeInstances || []).find(i => i.instanceId === id);
    const nodeId = inst ? inst.nodeId : id.split('__')[0];
    const n = _orchNodes.find(x => x.node_id === nodeId);
    if (!n) return { id, type: 'unknown' };
    // configuration 默认填充 node 数据库里存的 configuration 内容
    let config = {};
    try { config = JSON.parse(typeof n.configuration === 'string' ? n.configuration : JSON.stringify(n.configuration||{})); } catch(e){}
    if (typeof config !== 'object' || config === null) config = {};
    // arguments 以 BindConfig 数组格式注入（保留 key/value/policy），便于后期执行时按策略判断覆盖
    try {
      const params = typeof n.params === 'string' ? JSON.parse(n.params) : (n.params || []);
      if (Array.isArray(params) && params.length > 0) {
        config.arguments = params;
      }
    } catch(e){}
    // 叠加该实例在"参数配置区"填写的 override（按实例 ID 匹配），覆盖同名参数
    const preset = (_orchParamPreset && _orchParamPreset[id]) || {};
    Object.keys(preset).forEach(key => {
      const { src, value } = preset[key];
      if (!value || !value.trim()) return;
      let finalVal = value.trim();
      // 调用传入：已在 collectOrchParamOverrides 统一包成 {{arguments.参数key}}，此处仅兜底（旧数据裸值/{{x}} 再包一次），避免双重包裹
      if (src === 'entry' && !/^\{\{arguments\.[^}]+\}\}$/.test(finalVal)) {
        let k = finalVal;
        if (/^\{\{[^}]+\}\}$/.test(k)) k = k.replace(/^\{\{|\}\}$/g, '');
        while (k.startsWith('arguments.')) k = k.slice('arguments.'.length);
        finalVal = '{{arguments.' + k + '}}';
      }
      if (typeof config.arguments === 'undefined') config.arguments = [];
      const arr = Array.isArray(config.arguments) ? config.arguments : [];
      const idx = arr.findIndex(a => a && a.key === key);
      if (idx >= 0) { arr[idx].value = finalVal; }
      else { arr.push({ key, value: finalVal }); }
      config.arguments = arr;
    });
    // 应用 Live Preview 中对该实例的名称覆盖，使 DSL Preview 与图上编辑联动
    const nameOv = (_orchNameOverrides || {})[id];
    const finalName = (nameOv && nameOv.trim()) ? nameOv.trim() : n.name;
    // 应用 Live Preview 中对该实例的 switch_condition 覆盖（仅 Activity/CondSwitch 生效，与后端一致）
    const swOv = (_orchSwitchOverrides || {})[id];
    if (swOv && swOv.trim() && (n.type === 'custom/Activity' || n.type === 'custom/CondSwitch')) {
      config.node_config = config.node_config || {};
      config.node_config.switch_condition = swOv.trim();
    }
    // 与后端保存行为一致：arguments / responses 按 key 自动排序，便于对比与阅读
    if (Array.isArray(config.arguments)) config.arguments = config.arguments.slice().sort((a, b) => String((a && a.key) || '').localeCompare(String((b && b.key) || '')));
    if (Array.isArray(config.responses)) config.responses = config.responses.slice().sort((a, b) => String((a && a.key) || '').localeCompare(String((b && b.key) || '')));
    return { id, type: n.type, name: finalName, configuration: config };
  });

  // 子链实例生成调用子链节点（与后端一致）：ID 为子链实例 ID，引用的子链为 project:子链ID，
  // arguments 为本实例参数配置（按 key 排序）
  const subOverrides = collectOrchParamOverrides();
  const subNodes = subIds.map(id => {
    const ov = subOverrides[id] || {};
    const args = Object.keys(ov).sort().map(k => (ov[k].type ? { key: k, value: ov[k].value, type: ov[k].type } : { key: k, value: ov[k].value }));
    const configuration = { ruleChainId: getProject()+':'+orchSubBaseId(id), arguments: args };
    // 本实例的路由条件（与后端一致：node_config.switch_condition）
    const sw = String((_orchSwitchOverrides || {})[id] || '').trim();
    if (sw) configuration.node_config = { switch_condition: sw };
    return { id: id, type: ORCH_SUB_NODE_TYPE, name: orchSubDisplayName(id), configuration: configuration };
  });

  return JSON.stringify({
    ruleChain: {
      id: document.getElementById('orch-chain-id').value || 'auto',
      name: document.getElementById('orch-chain-name').value || '',
      debugMode: document.getElementById('orch-debug-mode').checked,
      // 子链不是根链：预览与后端 AssembleSubChain 一致（Root=false），根链才为 true
      root: window._orchTarget !== 'sub',
    },
    metadata: {
      nodes: [...nodes, ...subNodes],
      connections: conns.map(c => {
        // Flow Node 的 ID 即子链自身 chain_id，无需加 flow_ 前缀
        return { fromId: c.from_id, toId: c.to_id, type: c.type };
      }),
    }
  }, null, 2);
}

// Called whenever anything changes
let _orchChangeTimer = null;
function onOrchChange() {
  clearTimeout(_orchChangeTimer);
  updateOrchTargetByChainId(); // 链 ID 变化即时更新标题与目标
  _orchChangeTimer = setTimeout(() => {
    renderOrchPreview();
    renderOrchDslPreview();
    renderOrchParamOverrides();
    renderOrchNodeSelected(); // 连线变化后刷新已选节点表，链路中间节点锁定状态实时更新
    refreshOrchConnWarns();   // 连线/关系变化后实时校验自定义关系是否缺 switch_condition
  }, 200);
}

// ============================================================
// Upstream Output Hints (根据上游 outputs 自动提示可引用值)
// ============================================================

// 解析节点的 outputs（兼容字符串/数组/对象）
function parseNodeOutputs(node) {
  if (!node) return [];
  let raw = node.outputs;
  if (typeof raw === 'string') {
    try { raw = JSON.parse(raw); } catch(e) { return []; }
  }
  if (!Array.isArray(raw)) return [];
  return raw.filter(o => o && o.key);
}

// ============================================================
// Node Param Overrides (节点参数配置：每个 node 的每个参数指定值来源)
// ============================================================
// 来源类型常量（编排页面参数配置来源下拉，界面展示三档）
const PARAM_SRC_VALUE = 'value';     // 固定配置 → 内部 fixed
const PARAM_SRC_REF_ACT = 'ref_act'; // 引用节点 → 内部 upstream
const PARAM_SRC_REF_NODE = 'ref_node'; // 调用传入 → 内部 entry
// 内部存储来源（与 collectOrchParamOverrides 兼容）
const PARAM_SRC_FIXED = 'fixed';
const PARAM_SRC_ENTRY = 'entry';
const PARAM_SRC_UPSTREAM = 'upstream';
// 界面来源 <-> 内部来源 互转
function orchParamSrcToInternal(s) {
  if (s === PARAM_SRC_VALUE) return PARAM_SRC_FIXED;
  if (s === PARAM_SRC_REF_ACT) return PARAM_SRC_UPSTREAM;
  if (s === PARAM_SRC_REF_NODE) return PARAM_SRC_ENTRY;
  return PARAM_SRC_FIXED;
}
function orchParamSrcToDisplay(s) {
  if (s === PARAM_SRC_FIXED) return PARAM_SRC_VALUE;
  if (s === PARAM_SRC_UPSTREAM) return PARAM_SRC_REF_ACT;
  if (s === PARAM_SRC_ENTRY) return PARAM_SRC_REF_NODE;
  return PARAM_SRC_VALUE;
}

// 按 instanceId 排序比较：格式如 N000015__xsvkn，先按 __ 前的 baseId，再按随机后缀
function cmpInstanceId(a, b) {
  a = String(a || '');
  b = String(b || '');
  const pa = a.split('__');
  const pb = b.split('__');
  const c = (pa[0] || '').localeCompare(pb[0] || '');
  if (c !== 0) return c;
  return (pa[1] || '').localeCompare(pb[1] || '');
}

// 解析节点 params 定义（兼容字符串/数组）
function parseNodeParams(node) {
  if (!node) return [];
  let raw = node.params;
  if (typeof raw === 'string') {
    try { raw = JSON.parse(raw); } catch(e) { return []; }
  }
  if (!Array.isArray(raw)) return [];
  return raw.filter(p => p && p.key);
}

// 取得某节点实例的参数列表：普通节点取自节点定义的 params；
// 内置返回值节点（custom/ReturnValue）无节点定义，参数即根链返回值定义里的 key（Label/Type 同定义）。
function getOrchInstanceParams(inst) {
  if (!inst) return [];
  if (isOrchCallInst(inst)) return getOrchCallParams(inst);
  if (isOrchSubInst(inst)) return getOrchSubParams(inst.instanceId);
  if (isOrchInputInst(inst)) return getOrchInputParams(inst.instanceId);
  if (isOrchLoopInst(inst)) return getOrchLoopParams(inst);
  if (isOrchCondInst(inst)) return mergeOrchCondParams(inst, parseNodeParams(_orchNodes.find(n => n.node_id === inst.nodeId)));
  if (inst.type === 'custom/ReturnValue') {
    return (_orchRootResponses || [])
      .filter(rr => (rr.key || '').trim())
      .map(rr => ({ key: rr.key.trim(), label: rr.label || rr.key, type: rr.type || 'string', required: false }));
  }
  return parseNodeParams(_orchNodes.find(n => n.node_id === inst.nodeId));
}

// 渲染节点参数配置区：为每个选中 node 的每个参数生成"来源 + 值"控件
function renderOrchParamOverrides() {
  const container = document.getElementById('orch-param-list');
  const emptyEl = document.getElementById('orch-param-empty');
  const countEl = document.getElementById('orch-param-count');
  if (!container) return;

  const nodeIds = getSelectedOrchNodeIds();
  // 以"实例"为单位渲染（同一节点可多次添加），每个实例独立配置参数。
  // instances: [{instanceId, nodeId, name, type, ...}]，仅保留有参数定义的实例。
  // 按 instanceId 升序排序，方便在参数配置区快速定位某个节点的实例
  // 子链实例按普通节点处理：参数取子链调用签名的入参，一并列出
  const instances = orchParamInstances().filter(inst => {
    return getOrchInstanceParams(inst).length > 0;
  }).sort((a, b) => cmpInstanceId(a.instanceId, b.instanceId));
  const nodes = instances.map(inst => ({
    inst,
    def: _orchNodes.find(n => n.node_id === inst.nodeId),
  }));

  if (!nodes.length) {
    if (emptyEl) emptyEl.style.display = '';
    container.querySelectorAll('.override-node-block').forEach(e => e.remove());
    if (countEl) countEl.textContent = '0 个节点';
    return;
  }
  if (emptyEl) emptyEl.style.display = 'none';
  if (countEl) countEl.textContent = nodes.length + ' 个节点（含重复实例）';

  // 预扫描：来源为「引用节点」但引用目标已失效的参数（上游被删除 / 移出链路不再是祖先）
  // key=instanceId → [{key, ref}]，仅用于强制展开该节点并给出提示，不修改用户配置
  // （用户可能只是临时断开连线，后面还会接上，自动改写会导致重新配置非常繁琐）
  const brokenRefMap = {};
  nodes.forEach(({ inst, def }) => {
    getOrchInstanceParams(inst).forEach(p => {
      if (!orchParamRefBroken(inst.instanceId, resolveOrchParamPreset(inst.instanceId, p.key))) return;
      const preset = resolveOrchParamPreset(inst.instanceId, p.key);
      (brokenRefMap[inst.instanceId] = brokenRefMap[inst.instanceId] || []).push({ key: p.key, ref: String(preset.value || '') });
    });
  });

  let html = '';
  nodes.forEach(({ inst, def }) => {
    // 按 key 排序，保持与后端输出（node_config.arguments 经 sortBindConfigsByKey 排序）顺序一致
    const params = getOrchInstanceParams(inst).slice().sort((a, b) => String(a.key || '').localeCompare(String(b.key || '')));
    // 存在失效引用时强制展开，避免用户看不到需要重新选择的参数
    const collapsed = _orchCollapseState && _orchCollapseState[inst.instanceId] === true && !brokenRefMap[inst.instanceId];
    const activeCls = inst.instanceId === _orchActiveParamInst ? ' override-node-active' : '';
    html += `<div class="override-node-block${activeCls}" data-inst="${esc(inst.instanceId)}">
      <div class="override-node-header" onclick="toggleOrchParamBlock(this, '${esc(inst.instanceId)}', event)">
        <span class="toggle-icon">${collapsed ? '▸' : '▾'}</span>
        <span class="ov-inst-id ov-copyable" title="点击可选中复制">${esc(inst.instanceId)}</span>
        <span class="ov-inst-name ov-copyable" title="点击可选中复制">${esc(orchInstanceDisplayName(inst, def))}</span>
        <span class="ov-inst-count">${params.length} 个参数</span>
      </div>
      <div class="override-node-body${collapsed ? ' hidden' : ''}">`;
    params.forEach(p => {
      const required = p.required ? ' <span class="param-required-star">*</span>' : '';
      const typeTag = p.type ? `<span class="param-type-tag">(${esc(p.type)})</span>` : '';
      // 类型下拉仅在「固定配置」来源时可编辑；其它来源（引用节点/调用传入）锁定并还原为参数默认类型。
      const preset = resolveOrchParamPreset(inst.instanceId, p.key);
      const srcDisp = preset ? (orchParamSrcToDisplay(preset.src) || PARAM_SRC_VALUE) : PARAM_SRC_VALUE;
      const isFixed = srcDisp === PARAM_SRC_VALUE;
      const effType = (isFixed && preset && preset.type) ? preset.type : (p.type || 'string');
      const typeDisabledAttr = isFixed ? '' : 'disabled';
      html += `<div class="override-field param-row" data-node="${esc(inst.instanceId)}" data-key="${esc(p.key)}" style="flex-wrap:wrap">
        <input class="param-key" value="${esc(p.key)}" readonly title="参数英文名" style="flex:0 1 130px;min-width:110px;background:var(--bg-muted)">
        <input class="param-label" value="${esc(p.label || '')}" readonly title="显示名" style="flex:1 1 120px;min-width:90px;background:var(--bg-muted)">
        <select class="param-type" ${typeDisabledAttr} onchange="onParamTypeChange(this)" style="flex:0 1 92px;min-width:80px">
          <option value="string" ${effType==='string'?'selected':''}>string</option>
          <option value="int64" ${effType==='int64'?'selected':''}>int64</option>
          <option value="float64" ${effType==='float64'?'selected':''}>float64</option>
          <option value="bool" ${effType==='bool'?'selected':''}>bool</option>
          <option value="slice" ${effType==='slice'?'selected':''}>slice</option>
          <option value="map" ${effType==='map'?'selected':''}>map</option>
          <option value="formula" ${effType==='formula'?'selected':''}>formula</option>
        </select>
        <select class="param-src-select" style="flex:0 1 110px;min-width:96px" onchange="onParamSrcChange(this)">
          <option value="value">固定配置</option>
          <option value="ref_act">引用节点</option>
          <option value="ref_node">调用传入</option>
        </select>
        <div class="param-extra-row" style="flex:1 1 100%;display:flex;gap:6px;align-items:center;min-width:0">
          <span class="param-value-slot" style="flex:0 0 320px;display:flex;gap:4px;align-items:center;min-width:0;overflow:hidden"></span>
          <label class="param-private" style="flex:0 0 auto;display:none;align-items:center;gap:4px;font-size:.72rem;color:var(--text-muted);cursor:pointer;white-space:nowrap" title="勾选后该参数属于此节点私有，调用时需放在该节点 id 的二级结构下，避免同名 key 冲突">
            <input type="checkbox" class="param-private-check" onchange="onParamPrivateChange(this)"> 私有
          </label>
          <label class="param-required" style="flex:0 0 auto;white-space:nowrap">${p.required ? '<span class="param-required-star">*</span>必填' : ''}</label>
        </div>
      </div>`;
    });
    html += `</div></div>`;
  });

  // 用新内容替换（保留 empty 占位）
  container.querySelectorAll('.override-node-block').forEach(e => e.remove());
  container.insertAdjacentHTML('beforeend', html);

  // 为每个参数初始化值控件（默认固定值文本框）
  let brokenTotal = 0;
  let firstBrokenField = null;
  container.querySelectorAll('.override-field').forEach(field => {
    const nodeId = field.getAttribute('data-node');
    const key = field.getAttribute('data-key');
    const preset = resolveOrchParamPreset(nodeId, key);
    // 引用目标已失效：保持用户原配置不动（可能是临时断开，后面还会接上），仅给出提示
    const broken = (brokenRefMap[nodeId] || []).find(b => b.key === key);
    initParamValueControl(field, preset);
    // 回显"私有"勾选状态
    const privCheck = field.querySelector('.param-private-check');
    if (privCheck) privCheck.checked = !!(preset && preset.private);
    if (broken) {
      markOrchParamRefBroken(field, broken);
      brokenTotal++;
      if (!firstBrokenField) firstBrokenField = field;
    }
  });

  if (brokenTotal > 0) {
    const msg = '有 ' + brokenTotal + ' 个参数引用的节点已不在其上游（被删除或已移出链路），' +
                '运行时可能取不到值，请确认后重新选择来源（若只是临时断开连线，重新连上后提示会自动消失）。';
    if (typeof showToast === 'function') showToast(msg, 'error');
    if (firstBrokenField && firstBrokenField.scrollIntoView) {
      firstBrokenField.scrollIntoView({ block: 'center' });
    }
  }
}

// 解析参数当前生效的配置（暂存优先，其次从 DSL 节点 arguments 兜底回显）
function resolveOrchParamPreset(nodeId, key) {
  let preset = (_orchParamPreset[nodeId] && _orchParamPreset[nodeId][key]) || null;
  // 兜底：未单独保存 node_param_overrides 时，从 DSL 节点 arguments 恢复（已写入配置后的值）
  if (!preset) {
    const inst = (window._orchNodeInstances || []).find(i => i.instanceId === nodeId);
    if (inst && Array.isArray(inst.override)) {
      const bc = inst.override.find(b => (b.Key || b.key) === key);
      if (bc && bc.Value != null && bc.Value !== '') {
        preset = inferOrchParamPreset(bc.Value);
        // 从 DSL 恢复私有标志
        if (preset && Array.isArray(inst.privateKeys) && inst.privateKeys.includes(key)) {
          preset.private = true;
        }
      }
    }
  }
  return preset;
}

// 「引用节点」失效判定：来源为 upstream，但引用的目标已不在当前节点的可选上游里
// （目标节点被删除、或连线变动后不再是该节点的祖先），也包括无法解析的历史脏数据。
// 空值视为「尚未选择」，不算失效。
function orchParamRefBroken(nodeId, preset) {
  if (!preset || preset.src !== PARAM_SRC_UPSTREAM) return false;
  const ref = String(preset.value || '').trim();
  if (!ref) return false;
  const parsed = parseStepsRef(ref);
  if (!parsed || !parsed.nodeId) return true;
  return !getOrchRefNodeCandidates(nodeId).some(c => c.id === parsed.nodeId);
}

// 保存前检查：扫描参数面板当前 UI 状态，收集所有失效的「引用节点」配置
// 只用于提示，不修改配置、不阻断保存
function collectOrchBrokenRefs() {
  const out = [];
  const container = document.getElementById('orch-param-list');
  if (!container) return out;
  container.querySelectorAll('.override-field').forEach(field => {
    const nodeId = field.getAttribute('data-node');
    const key = field.getAttribute('data-key');
    const preset = resolveOrchParamPreset(nodeId, key);
    if (!preset || preset.src !== PARAM_SRC_UPSTREAM) return; // 仅校验「引用节点」来源（用内部值，避免界面值比较错误）
    const ref = (preset.value || '').trim();
    if (!ref) return;
    const parsed = parseStepsRef(ref);
    if (!parsed || !getOrchRefNodeCandidates(nodeId).some(c => c.id === parsed.nodeId)) {
      out.push({ nodeId: nodeId, key: key, ref: String(ref) });
    }
  });
  return out;
}

// 保存前检查：扫描所有节点参数配置，找出「引用节点」选中的上游节点不在当前节点所有连接路径上的情况
// （存在不经过它的分支路径），运行时可能走不到那条、取值不正确。仅提示，不修改配置、不阻断保存。
function collectOrchNonCommonRefs() {
  const out = [];
  orchParamInstances().forEach(inst => {
    getOrchInstanceParams(inst).forEach(p => {
      const preset = resolveOrchParamPreset(inst.instanceId, p.key);
      if (!preset || preset.src !== PARAM_SRC_UPSTREAM) return;
      const ref = (preset.value || '').trim();
      if (!ref) return;
      const parsed = parseStepsRef(ref);
      if (!parsed || !parsed.nodeId) return;
      if (orchParamRefBroken(inst.instanceId, preset)) return; // 已失效由另一检查负责，避免重复提示
      const dominators = getOrchDominatorIds(inst.instanceId);
      if (!dominators.has(parsed.nodeId)) {
        out.push({ nodeId: inst.instanceId, key: p.key, ref: String(ref), refNode: parsed.nodeId });
      }
    });
  });
  return out;
}

// 清除参数行上的失效提示（用户重新选择来源后调用）
function clearOrchParamRefBroken(field) {
  if (!field) return;
  field.classList.remove('param-ref-broken');
  const sel = field.querySelector('.param-src-select');
  if (sel) sel.classList.remove('param-src-need-reselect');
  const warn = field.querySelector('.param-ref-broken-warn');
  if (warn) warn.remove();
}

// 在参数行上标记失效提示，引导用户重新选择来源
function markOrchParamRefBroken(field, broken) {
  field.classList.add('param-ref-broken');
  const sel = field.querySelector('.param-src-select');
  if (sel) sel.classList.add('param-src-need-reselect');
  const extra = field.querySelector('.param-extra-row');
  if (!extra) return;
  let warn = field.querySelector('.param-ref-broken-warn');
  if (!warn) {
    warn = document.createElement('span');
    warn.className = 'param-ref-broken-warn';
    extra.appendChild(warn);
  }
  const refId = (parseStepsRef(broken.ref) || {}).nodeId || broken.ref;
  const text = '⚠️ 引用的节点 ' + refId + ' 已不在该节点的上游（已删除或已移出链路），运行时可能取不到值。' +
               '请重新选择来源，或重新连上连线后本提示会自动消失。';
  warn.textContent = text;
  warn.title = text;
}

// 在参数行上标记「引用节点不在该节点的所有连接路径上（存在不经过它的分支）」提示，复用 param-ref-broken 标红样式
function markOrchParamNotCommon(field, info) {
  field.classList.add('param-ref-broken');
  const sel = field.querySelector('.param-src-select');
  if (sel) sel.classList.add('param-src-need-reselect');
  const extra = field.querySelector('.param-extra-row');
  if (!extra) return;
  let warn = field.querySelector('.param-ref-broken-warn');
  if (!warn) {
    warn = document.createElement('span');
    warn.className = 'param-ref-broken-warn';
    extra.appendChild(warn);
  }
  const text = '⚠️ 引用的节点 ' + info.refNode + ' 不在该节点的所有连接路径上（存在不经过它的分支），运行时可能走不到那条、取值不正确。请改用所有路径都经过的节点。';
  warn.textContent = text;
  warn.title = text;
}

// 暂存已编辑的参数值（key=nodeId, value={key: {src, value}}），用于重渲染时保留
let _orchParamPreset = {};
// 标记某参数是否已「由调用传入首次切到固定配置并清空过默认值」，用于仅首次自动清空、之后不再改动
let _orchFixedSeeded = {};
// 节点参数配置区收起状态（key=instanceId, value=true 表示收起），持久化到数据库
let _orchCollapseState = {};

// 固定配置输入框占位提示：按节点类型与参数类型区分
function orchParamValuePlaceholder(instanceId, type) {
  const inst = (window._orchNodeInstances || []).find(i => i.instanceId === instanceId);
  if (inst && inst.type === 'custom/ReturnValue') {
    return '填写取值表达式：{{steps.<节点ID>.responses.<key>}} / {{arguments.<key>}} / 字面量';
  }
  if (type === 'formula') return '公式，如 {{arguments.a}} + {{arguments.b}}';
  return '填写固定值';
}

// 取得参数定义默认类型：普通节点取节点定义 params 的 type；返回值节点取根链返回值定义的 type
function orchParamDefType(nodeId, key) {
  const inst = (window._orchNodeInstances || []).find(i => i.instanceId === nodeId);
  if (!inst) return 'string';
  const p = (getOrchInstanceParams(inst) || []).find(x => x.key === key);
  if (p && p.type) return p.type;
  if (inst.type === 'custom/ReturnValue') {
    const rr = (_orchRootResponses || []).find(r => (r.key || '') === key);
    if (rr && rr.type) return rr.type;
  }
  return 'string';
}

// 固定配置下切换参数类型：刷新输入框占位提示（formula 需 {{...}} 提示），保留已填值，并暂存
function onParamTypeChange(sel) {
  const field = sel.closest('.override-field');
  if (!field) return;
  const nodeId = field.getAttribute('data-node');
  const slot = field.querySelector('.param-value-slot');
  const valEl = field.querySelector('.param-value-input');
  const curVal = valEl ? valEl.value : '';
  const newType = sel.value;
  // 只有「固定配置」来源类型下拉才可编辑，这里仅刷新占位提示，不改动值
  renderFixedInput(slot, curVal, orchParamValuePlaceholder(nodeId, newType));
  storeParamPreset();
}

// 根据来源切换值控件
function onParamSrcChange(sel) {
  const field = sel.closest('.override-field');
  clearOrchParamRefBroken(field); // 用户重新选择了来源，去掉失效提示
  const src = sel.value;
  const slot = field.querySelector('.param-value-slot');
  const nodeId = field.getAttribute('data-node');
  const key = field.getAttribute('data-key');
  const preset = (_orchParamPreset[nodeId] && _orchParamPreset[nodeId][key]) || { src: orchParamSrcToInternal(src), value: '' };
  const prevSrc = preset.src; // 切换前的来源（用于判断是否由「调用传入」切来）

  if (src === PARAM_SRC_REF_ACT) {
    preset.src = PARAM_SRC_UPSTREAM;
    // 由「调用传入」切来：原值是入参名（子链 / 调用根链实例默认带入参名），不是引用路径，清空后再选
    if (prevSrc === PARAM_SRC_ENTRY) preset.value = '';
    renderOrchRefNodeControl(slot, nodeId, preset.value);
  } else if (src === PARAM_SRC_REF_NODE) {
    preset.src = PARAM_SRC_ENTRY;
    preset.value = '';
    renderCallInputHint(slot, key, nodeId, !!preset.private);
  } else {
    preset.src = PARAM_SRC_FIXED;
    // 首次由「调用传入」切到「固定配置」：清空默认值（调用传入的隐藏值里默认带的是参数 key，不应带出）。
    // 仅首次如此，之后用户手动改过的值不再被自动清空（按 seedKey 去重）。
    const seedKey = nodeId + '\x00' + key;
    if (prevSrc === PARAM_SRC_ENTRY && !_orchFixedSeeded[seedKey]) {
      preset.value = '';
      _orchFixedSeeded[seedKey] = true;
    }
    renderFixedInput(slot, preset.value, orchParamValuePlaceholder(nodeId, 'string'));
  }
  // 类型下拉：仅「固定配置」可编辑；其它来源锁定并还原为参数定义默认类型（p.type / 返回值定义 type）
  const typeSel = field.querySelector('.param-type');
  if (typeSel) {
    if (src === PARAM_SRC_VALUE) {
      typeSel.disabled = false;
    } else {
      typeSel.disabled = true;
      typeSel.value = orchParamDefType(nodeId, key);
    }
  }
  updateParamPrivateVisibility(field);
  storeParamPreset();
}

function initParamValueControl(field, preset) {
  const sel = field.querySelector('.param-src-select');
  const slot = field.querySelector('.param-value-slot');
  const nodeId = field.getAttribute('data-node');
  if (!preset) {
    sel.value = PARAM_SRC_VALUE;
    const tSel = field.querySelector('.param-type');
    renderFixedInput(slot, '', orchParamValuePlaceholder(nodeId, tSel ? tSel.value : 'string'));
    updateParamPrivateVisibility(field);
    return;
  }
  sel.value = orchParamSrcToDisplay(preset.src) || PARAM_SRC_VALUE;
  if (preset.src === PARAM_SRC_UPSTREAM) {
    renderOrchRefNodeControl(slot, nodeId, preset.value);
  } else if (preset.src === PARAM_SRC_ENTRY) {
    renderCallInputHint(slot, field.getAttribute('data-key'), nodeId, !!preset.private);
  } else {
    const tSel = field.querySelector('.param-type');
    renderFixedInput(slot, preset.value, orchParamValuePlaceholder(nodeId, tSel ? tSel.value : 'string'));
  }
  updateParamPrivateVisibility(field);
}

// 调用传入：无需输入框，由调用方在接口调用时传入
// 显示「参数名」根据是否私有动态变化：
//  - 私有：{{arguments.<nodeId>.<key>}}（二级结构，避免同名 key 冲突）
//  - 公共：{{arguments.<key>}}（一级结构）
// 隐藏 input 记录裸 key（paramKey），避免 value 累积 nodeId 前缀
function renderCallInputHint(slot, paramKey, nodeId, isPrivate) {
  slot.style.flex = '';
  slot.style.flexWrap = '';
  const displayRef = isPrivate
    ? '{{arguments.' + nodeId + '.' + paramKey + '}}'
    : '{{arguments.' + paramKey + '}}';
  // 参数名固定最大宽度 + 省略号截断，鼠标悬停显示完整名称（title）。
  // 整体不换行，保证数据变化（勾选私有后变长）时不会导致换行。
  slot.innerHTML =
    `<span class="param-call-hint" style="display:inline-flex;align-items:center;max-width:100%;white-space:nowrap">` +
      `<span style="white-space:nowrap;flex:0 0 auto">调用接口时由用户传入（参数名：</span>` +
      `<span class="param-call-ref" style="display:inline-block;max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;vertical-align:bottom" title="${esc(displayRef)}">${esc(displayRef)}</span>` +
      `<span style="white-space:nowrap;flex:0 0 auto">）</span>` +
    `</span>` +
    `<input class="param-value-input" type="hidden" value="${esc(paramKey || '')}">`;
}

// 私有复选框切换：保存状态后，若当前来源是「调用传入」，动态刷新参数名显示
function onParamPrivateChange(checkbox) {
  const field = checkbox.closest('.override-field');
  if (!field) return;
  storeParamPreset(); // 先持久化 private 状态到 _orchParamPreset
  const sel = field.querySelector('.param-src-select');
  if (sel && sel.value === PARAM_SRC_REF_NODE) {
    const nodeId = field.getAttribute('data-node');
    const key = field.getAttribute('data-key');
    const slot = field.querySelector('.param-value-slot');
    const preset = (_orchParamPreset[nodeId] && _orchParamPreset[nodeId][key]) || {};
    renderCallInputHint(slot, key, nodeId, !!preset.private);
  }
}

// 私有复选框仅在来源为「调用传入」时显示，其它来源隐藏（该值对其它来源无效）
function updateParamPrivateVisibility(field) {
  if (!field) return;
  const sel = field.querySelector('.param-src-select');
  const privLabel = field.querySelector('.param-private');
  if (!privLabel) return;
  const isEntry = sel && sel.value === PARAM_SRC_REF_NODE;
  privLabel.style.display = isEntry ? 'flex' : 'none';
}

// 解析 {{steps.<id>.arguments.<key>}} / {{steps.<id>.responses.<key>}}
function parseStepsRef(ref) {
  if (typeof ref !== 'string') return null;
  const m = ref.match(/^\{\{\s*steps\.([^.]+)\.(arguments|responses)\.([^}]+?)\s*\}\}$/);
  if (!m) return null;
  return { nodeId: m[1], kind: m[2], key: m[3] };
}

// 收集「已选择的其它节点/子链」用于「引用节点」选择（excludeId 为当前实例 instanceId）。
// 仅返回位于当前节点「所有父节点（祖先）」上的节点/子链：即从 excludeId 沿 Connections 反向
// 可达的节点。子节点、兄弟节点、无关节点由于时间上拿不到该参数，均不列出。
function getOrchRefNodeCandidates(excludeId) {
  const ancestorIds = getOrchAncestorIds(excludeId);
  const cands = [];
  // 已选节点实例（按 instanceId 排除自身，且必须在祖先集合内）
  (window._orchNodeInstances || []).forEach(inst => {
    if (inst.instanceId === excludeId) return;
    if (!ancestorIds.has(inst.instanceId)) return;
    const def = (_orchNodes || []).find(n => n.node_id === inst.nodeId);
    if (!def) return;
    if (isOrchCallInst(inst)) {
      cands.push({ id: inst.instanceId, name: inst.name || inst.nodeId, type: inst.type, params: getOrchCallParams(inst), outputs: getOrchCallOutputs(inst) });
      return;
    }
    if (isOrchInputInst(inst)) {
      // 参数输入节点：参数值同时写入 arguments 与 responses，两处都可引用
      const ps = getOrchInputParams(inst.instanceId);
      cands.push({ id: inst.instanceId, name: inst.name || inst.nodeId, type: inst.type, params: ps, outputs: ps.map(p => ({ key: p.key, label: p.label, type: p.type })) });
      return;
    }
    if (isOrchLoopInst(inst)) {
      // 循环节点：返回值为汇总结果（items / count 等）
      cands.push({ id: inst.instanceId, name: inst.name || inst.nodeId, type: inst.type, params: getOrchLoopParams(inst), outputs: ORCH_LOOP_OUTPUTS.slice() });
      return;
    }
    if (isOrchCondInst(inst)) {
      // 条件判断节点：本链定义的参数同样可被下游引用（arguments）
      cands.push({ id: inst.instanceId, name: inst.name || inst.nodeId, type: inst.type, params: mergeOrchCondParams(inst, parseNodeParams(def)), outputs: parseNodeOutputs(def) });
      return;
    }
    cands.push({ id: inst.instanceId, name: inst.name || inst.nodeId, type: inst.type, params: parseNodeParams(def), outputs: parseNodeOutputs(def) });
  });
  // 已选子链实例（按实例 ID 排除自身，且必须在祖先集合内）
  getSelectedOrchSubIds().forEach(subId => {
    if (subId === excludeId) return;
    if (!ancestorIds.has(subId)) return;
    const s = orchSubDefOf(subId);
    if (!s) return;
    // 子链实例按普通节点处理：参数为子链入参，返回值为子链返回值定义
    cands.push({ id: subId, name: orchSubDisplayName(subId), type: 'sub', params: getOrchSubParams(subId), outputs: getOrchSubOutputs(subId) });
  });
  return cands;
}

// 计算当前节点的所有祖先节点 ID 集合（含直接父节点及其上游，不含自身）。
// 基于 Connections 中 from_id → to_id 的有向关系反向遍历：
// 从 targetId 出发，沿「谁指向我」反向回溯，所有可达节点即为时间上先于 target 执行的祖先。
function getOrchAncestorIds(targetId) {
  const result = new Set();
  if (!targetId) return result;
  const conns = collectOrchConnections();
  // 反向邻接表：to_id -> [from_id, ...]
  const reverse = {};
  conns.forEach(c => {
    if (c.from_id && c.to_id) {
      (reverse[c.to_id] = reverse[c.to_id] || []).push(c.from_id);
    }
  });
  // 从 targetId 反向 BFS/DFS 收集祖先
  const stack = [targetId];
  const visited = new Set([targetId]);
  while (stack.length) {
    const cur = stack.pop();
    (reverse[cur] || []).forEach(from => {
      if (visited.has(from)) return;
      visited.add(from);
      result.add(from);
      stack.push(from);
    });
  }
  return result;
}

// 计算节点 targetId 的「必经节点」集合：从任意入口到 targetId 的每条连接路径都经过的节点。
// 等价于 targetId 所有直接父节点「祖先或自身」集合的交集 —— 引用节点选中的上游节点若不在此集合，
// 说明存在不经过它的分支路径，运行时可能走不到那条、取不到值。
function getOrchDominatorIds(targetId) {
  const conns = collectOrchConnections();
  const parents = [];
  conns.forEach(c => { if (c.to_id === targetId && c.from_id) parents.push(c.from_id); });
  if (parents.length === 0) return new Set(); // 入口节点无父节点，引用无意义
  let dominators = null;
  parents.forEach(p => {
    const closure = new Set(getOrchAncestorIds(p));
    closure.add(p); // 含父节点自身（父节点也是到 targetId 的必经点）
    if (dominators === null) dominators = closure;
    else dominators = new Set([...dominators].filter(id => closure.has(id)));
  });
  return dominators || new Set();
}

// 渲染「引用节点」控件：先选节点，再选 参数定义/返回值定义
function renderOrchRefNodeControl(slot, excludeId, presetValue) {
  // 引用节点控件较宽，独占整行并允许内部换行，避免被右侧面板遮挡
  slot.style.flex = '1 1 100%';
  slot.style.flexWrap = 'wrap';
  const parsed = parseStepsRef(presetValue);
  const cands = getOrchRefNodeCandidates(excludeId);
  // 已失效引用（目标被删除 / 不再是祖先 / 无法解析）：保留原值渲染，不静默改成别的节点，
  // 也不清空 —— 用户可能只是临时断开连线，接上后应自动恢复正常
  if (presetValue && (!parsed || !cands.some(c => c.id === parsed.nodeId))) {
    renderOrchRefNodeControlBroken(slot, excludeId, presetValue, parsed);
    return;
  }
  const selNodeId = parsed ? parsed.nodeId : (cands[0] ? cands[0].id : '');
  const cand = cands.find(c => c.id === selNodeId) || cands[0] || null;
  const kind = parsed ? parsed.kind : 'arguments';
  const presetKey = parsed ? parsed.key : '';

  let nodeOpts = `<option value="">— 选择节点 —</option>`;
  cands.forEach(c => {
    nodeOpts += `<option value="${esc(c.id)}" ${c.id === selNodeId ? 'selected' : ''}>${esc(c.id)} ${esc(c.name)}</option>`;
  });

  const fieldOpts = renderOrchRefNodeFieldOptions(cand, kind, presetKey);
  const finalRef = cand ? buildOrchRefRef(cand.id, kind, presetKey) : '';

  slot.innerHTML =
    `<select class="param-ref-node-select" style="flex:0 0 150px" onchange="onOrchRefNodeChange(this)">${nodeOpts}</select>` +
    `<select class="param-ref-kind-select" style="flex:0 0 110px" onchange="onOrchRefNodeChange(this)">` +
      `<option value="arguments" ${kind === 'arguments' ? 'selected' : ''}>参数定义</option>` +
      `<option value="responses" ${kind === 'responses' ? 'selected' : ''}>返回值定义</option>` +
    `</select>` +
    `<select class="param-value-input" style="flex:1;min-width:0" onchange="onOrchRefNodeFieldChange(this)">${fieldOpts}</select>` +
    `<input type="hidden" class="param-ref-final" value="${esc(finalRef)}">`;
}

// 失效引用的专用渲染：节点下拉里给出「⚠️ xxx（已不在上游）」占位项并选中，
// 字段下拉只有原引用值一项，保证 storeParamPreset 读到的仍是原值（不会被清空或串成别的节点）
function renderOrchRefNodeControlBroken(slot, excludeId, presetValue, parsed) {
  slot.style.flex = '1 1 100%';
  slot.style.flexWrap = 'wrap';
  const brokenId = parsed ? parsed.nodeId : (String(presetValue).replace(/^\{\{/, '').replace(/\}\}$/, ''));
  const kind = parsed ? parsed.kind : 'arguments';
  // 除失效占位项外，同时列出当前可用的上游节点，方便直接改选而无需切换来源
  let nodeOpts = `<option value="" selected>⚠️ ${esc(brokenId)}（已不在上游）</option>`;
  getOrchRefNodeCandidates(excludeId).forEach(c => {
    nodeOpts += `<option value="${esc(c.id)}">${esc(c.id)} ${esc(c.name)}</option>`;
  });
  slot.innerHTML =
    `<select class="param-ref-node-select param-ref-broken-select" style="flex:0 0 200px" onchange="onOrchRefNodeChange(this)">${nodeOpts}</select>` +
    `<select class="param-ref-kind-select" style="flex:0 0 110px" onchange="onOrchRefNodeChange(this)">` +
      `<option value="arguments" ${kind === 'arguments' ? 'selected' : ''}>参数定义</option>` +
      `<option value="responses" ${kind === 'responses' ? 'selected' : ''}>返回值定义</option>` +
    `</select>` +
    `<select class="param-value-input" style="flex:1;min-width:0" onchange="onOrchRefNodeFieldChange(this)">` +
      `<option value="${esc(presetValue)}" selected>${esc(presetValue)}（失效）</option>` +
    `</select>` +
    `<input type="hidden" class="param-ref-final" value="${esc(presetValue)}">`;
}

function renderOrchRefNodeFieldOptions(cand, kind, presetKey) {
  if (!cand) return `<option value="">— 无可用节点 —</option>`;
  const list = (kind === 'responses') ? cand.outputs : cand.params;
  if (!list || !list.length) {
    return `<option value="">— ${kind === 'responses' ? '无返回值定义' : '无参数定义'} —</option>`;
  }
  let opts = `<option value="">— 选择字段 —</option>`;
  list.forEach(f => {
    const ref = '{{steps.' + cand.id + '.' + kind + '.' + f.key + '}}';
    opts += `<option value="${esc(ref)}" ${f.key === presetKey ? 'selected' : ''}>${esc(f.label || f.key)} (${esc(f.key)})</option>`;
  });
  return opts;
}

function buildOrchRefRef(nodeId, kind, key) {
  if (!nodeId || !key) return '';
  return '{{steps.' + nodeId + '.' + kind + '.' + key + '}}';
}

// 切换节点 / 切换 参数定义·返回值定义
function onOrchRefNodeChange(sel) {
  const field = sel.closest('.override-field');
  const slot = field.querySelector('.param-value-slot');
  const nodeId = field.getAttribute('data-node');
  const nodeSel = slot.querySelector('.param-ref-node-select');
  const kindSel = slot.querySelector('.param-ref-kind-select');
  const finalInput = slot.querySelector('.param-ref-final');
  const cand = getOrchRefNodeCandidates(nodeId).find(c => c.id === nodeSel.value) || null;
  const fieldSel = slot.querySelector('.param-value-input');
  fieldSel.innerHTML = renderOrchRefNodeFieldOptions(cand, kindSel.value, '');
  finalInput.value = '';
  storeParamPreset();
}

// 选择具体字段，拼装最终引用路径
function onOrchRefNodeFieldChange(sel) {
  const field = sel.closest('.override-field');
  const slot = field.querySelector('.param-value-slot');
  const finalInput = slot.querySelector('.param-ref-final');
  finalInput.value = sel.value;
  storeParamPreset();
}

function renderFixedInput(slot, value, placeholder) {
  // 固定配置输入框独占整行，与来源选择框换行展示
  slot.style.flex = '1 1 100%';
  slot.style.flexWrap = 'wrap';
  slot.innerHTML = `<input class="param-value-input" style="flex:1;min-width:200px" placeholder="${esc(placeholder || '')}" value="${esc(value || '')}" oninput="storeParamPreset()">`;
}

// ============================================================
// 根节点返回值定义（Root Chain 的返回结构）
// 定义保存在 _orchRootResponses：[{key,label,type,value}]，随根链保存为 root_responses。
// value 支持占位符 {{steps.<实例ID>.arguments|responses.<字段>}} / {{arguments.<key>}} 或固定值，
// 执行结束后由后端替换为真实值并按 type 转换，写入 FlowContext.Responses 返回。
// ============================================================
let _orchRootResponses = [];

// 转换类型候选（与节点参数/返回值的类型转换保持一致）
const ROOT_RESPONSE_TYPES = ['', 'string', 'int64', 'float64', 'bool', 'slice', 'map', 'formula'];
const ROOT_RESPONSE_TYPE_LABELS = {
  '': '不转换', 'string': 'string', 'int64': 'int64', 'float64': 'float64',
  'bool': 'bool', 'slice': 'slice', 'map': 'map', 'formula': 'formula'
};

function orchRootResponseTypeHTML(type) {
  const cur = type || '';
  return ROOT_RESPONSE_TYPES.map(t =>
    '<option value="' + esc(t) + '"' + (cur === t ? ' selected' : '') + '>' + esc(ROOT_RESPONSE_TYPE_LABELS[t]) + '</option>'
  ).join('');
}

function addOrchRootResponseRow() {
  _orchRootResponses.push({ key: '', label: '', type: '', value: '' });
  renderOrchRootResponses();
  const box = document.getElementById('orch-rr-list');
  const rows = box ? box.querySelectorAll('.rr-row') : [];
  const last = rows[rows.length - 1];
  if (last) {
    const k = last.querySelector('.rr-key');
    if (k) k.focus();
    if (last.scrollIntoView) last.scrollIntoView({ block: 'center' });
  }
}

function removeOrchRootResponseRow(idx) {
  _orchRootResponses.splice(idx, 1);
  renderOrchRootResponses();
}

// 单行内把各控件的值写回状态数组（避免重渲染丢失未提交的输入）
function storeOrchRootResponseRow(row) {
  const idx = parseInt(row.getAttribute('data-idx'), 10);
  if (isNaN(idx) || !_orchRootResponses[idx]) return;
  const it = _orchRootResponses[idx];
  const key = row.querySelector('.rr-key');
  const label = row.querySelector('.rr-label');
  const type = row.querySelector('.rr-type');
  const valEl = row.querySelector('.rr-value');
  if (key) it.key = key.value.trim();
  if (label) it.label = label.value.trim();
  if (type) it.type = type.value;
  if (valEl) it.value = valEl.value;
}

function onOrchRootResponseChange(el) {
  const row = el && el.closest ? el.closest('.rr-row') : null;
  if (row) storeOrchRootResponseRow(row);
  const cnt = document.getElementById('orch-rr-count');
  if (cnt) cnt.textContent = collectOrchRootResponses().length + ' 个字段';
}

function renderOrchRootResponses() {
  const box = document.getElementById('orch-rr-list');
  const cnt = document.getElementById('orch-rr-count');
  if (!box) return;
  if (cnt) cnt.textContent = collectOrchRootResponses().length + ' 个字段';
  if (!_orchRootResponses.length) {
    box.innerHTML = '<div style="text-align:center;padding:14px;color:var(--text-muted);font-size:.82rem">' +
      '尚未定义返回字段。执行结果默认返回各节点的入参与返回值（steps），定义后则返回这里配置的结构。</div>';
    return;
  }
  box.innerHTML = _orchRootResponses.map((it, i) => {
    // 每个返回字段仅配置：key（字段名）/ 名称（说明）/ 类型 / 默认值
    return '<div class="rr-row" data-idx="' + i + '" style="display:flex;flex-wrap:wrap;align-items:center;gap:6px;padding:6px 0;border-bottom:1px dashed var(--border)">' +
        '<input class="rr-key" placeholder="key（字段名）" title="返回结构中的键名" style="flex:0 0 140px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace" value="' + esc(it.key || '') + '" oninput="onOrchRootResponseChange(this)">' +
        '<input class="rr-label" placeholder="名称（说明）" title="中文名/说明（选填）" style="flex:0 0 120px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem" value="' + esc(it.label || '') + '" oninput="onOrchRootResponseChange(this)">' +
        '<select class="rr-type" title="转换类型" style="flex:0 0 92px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem" onchange="onOrchRootResponseChange(this)">' + orchRootResponseTypeHTML(it.type) + '</select>' +
        '<input class="rr-value" placeholder="默认值" title="字段默认值，可填固定值或 {{steps.xxx.responses.key}} 等表达式" style="flex:1 1 220px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace" value="' + esc(it.value || '') + '" oninput="onOrchRootResponseChange(this)">' +
        '<button class="btn btn-sm btn-danger" type="button" title="删除该字段" onclick="removeOrchRootResponseRow(' + i + ')">✕</button>' +
      '</div>';
  }).join('');
}

// 收集为 root_responses（过滤掉未填字段名的项）
function collectOrchRootResponses() {
  return _orchRootResponses
    .filter(it => it && String(it.key || '').trim())
    .map(it => ({
      key: String(it.key).trim(),
      label: String(it.label || '').trim(),
      type: String(it.type || '').trim(),
      value: String(it.value || '').trim(),
      required: it.required || false,
      description: String(it.description || '').trim()
    }));
}

// 回显已保存的 root_responses（编辑加载时调用）。入参为 JSON 字符串或数组
function applyOrchRootResponses(saved) {
  _orchRootResponses = [];
  if (!saved) { renderOrchRootResponses(); return; }
  try {
    const list = typeof saved === 'string' ? JSON.parse(saved) : saved;
    (list || []).forEach(it => {
      if (!it) return;
      // 兼容旧格式：直接是字符串时按 {key: '', value: 字符串} 处理
      if (typeof it === 'string') { _orchRootResponses.push({ key: '', label: '', type: '', value: it }); return; }
      _orchRootResponses.push({
        key: it.key || '', label: it.label || '', type: it.type || '',
        value: it.value == null ? '' : String(it.value),
        source: it.source || '', arg: it.arg || '',
        required: !!it.required, description: it.description || ''
      });
    });
  } catch (e) { /* ignore */ }
  renderOrchRootResponses();
}

// 将当前 UI 中的参数配置写入暂存对象
function storeParamPreset() {
  const container = document.getElementById('orch-param-list');
  if (!container) return;
  container.querySelectorAll('.override-field').forEach(field => {
    const nodeId = field.getAttribute('data-node');
    const key = field.getAttribute('data-key');
    const src = orchParamSrcToInternal(field.querySelector('.param-src-select').value);
    const valEl = field.querySelector('.param-value-input');
    const value = valEl ? valEl.value : '';
    const privEl = field.querySelector('.param-private-check');
    const isPrivate = privEl ? !!privEl.checked : false;
    // type/label/required/description：返回值节点（custom/ReturnValue）无节点定义，
    // 这些字段取自根链返回值定义，需随实例参数一并保存；普通节点由后端按节点定义补充，此处留空不写。
    // 类型下拉值始终读取当前 UI（固定配置可为 formula 等；非固定配置已被重置为参数默认类型）
    const typeSel = field.querySelector('.param-type');
    let ptype = typeSel ? (typeSel.value || 'string') : 'string';
    let plabel = '';
    let prequired = false;
    let pdesc = '';
    const inst = (window._orchNodeInstances || []).find(i => i.instanceId === nodeId);
    if (inst && inst.type === 'custom/ReturnValue') {
      const rr = (_orchRootResponses || []).find(r => (r.key || '') === key);
      plabel = rr ? (rr.label || '') : '';
      prequired = rr ? !!rr.required : false;
      pdesc = rr ? (rr.description || '') : '';
      // 非固定配置时类型还原为返回值定义默认类型（typeSel 已在此前 onParamSrcChange 中重置）
      if (src !== PARAM_SRC_FIXED && rr && rr.type) ptype = rr.type;
    } else if (isOrchInputInst(inst) || isOrchCondInst(inst)) {
      const d = getOrchInputDefs(nodeId).find(x => x.key === key);
      plabel = d ? (d.label || '') : '';
      pdesc = plabel;
      // 非固定配置时类型跟随参数定义（空表示不转换）
      if (src !== PARAM_SRC_FIXED && d) ptype = d.type || '';
    }
    if (!_orchParamPreset[nodeId]) _orchParamPreset[nodeId] = {};
    _orchParamPreset[nodeId][key] = {
      src, value, private: isPrivate, type: ptype,
      key: key, label: plabel, required: prequired, description: pdesc, policy: 'frontend'
    };
  });
}

// 收集为 node_param_overrides：{ nodeId: { key: { src, value } } }
// 同时保存来源(src)与最终值(value)，确保编辑回显时能准确还原"来源选择"，
// 而不依赖值格式推断。value 为最终值（引用节点即选择的值：{{steps.<节点ID>.<kind>.<字段>}}），
// 后端写入节点 arguments 时取其中的 value 字段。
function collectOrchParamOverrides() {
  const result = {};
  // 按 instanceId 升序收集，使保存的 node_param_overrides 顺序与参数配置区展示顺序一致
  Object.keys(_orchParamPreset).sort(cmpInstanceId).forEach(nodeId => {
    const keys = _orchParamPreset[nodeId];
    Object.keys(keys).forEach(key => {
      const { src, value, private: isPrivate } = keys[key];
      if (!value || !value.trim()) return; // 空值不覆盖
      let finalVal = value.trim();
      let finalSrc = src; // _orchParamPreset 已存内部来源(fixed/upstream/entry)，无需再转换
      if (finalSrc === PARAM_SRC_ENTRY) {
        // 调用传入：包成 {{arguments.参数key}}
        // 已是正确格式则保持；否则规整为裸参数 key 再包裹（去掉可能存在的 arguments. 前缀与外层 {{}}）
        if (!/^\{\{arguments\.[^}]+\}\}$/.test(finalVal)) {
          let k = finalVal;
          if (/^\{\{[^}]+\}\}$/.test(k)) k = k.replace(/^\{\{|\}\}$/g, '');
          while (k.startsWith('arguments.')) k = k.slice('arguments.'.length);
          finalVal = '{{arguments.' + k + '}}';
        }
        // 私有参数：从该节点 id 的二级结构取值 {{arguments.<nodeId>.<key>}}，
        // 避免多个 node 同名 key 取值冲突，调用者按 { nodeId: { key: value } } 传参
        if (isPrivate) {
          // 提取 arguments. 后的完整路径（可能是裸 key，也可能是回显时已带 nodeId 前缀、甚至历史脏数据多级前缀）
          const path = finalVal.replace(/^\{\{arguments\./, '').replace(/\}\}$/, '');
          // 裸 key 取最后一个 '.' 之后的部分，忽略所有前导的 nodeId 前缀，避免 nodeId 重复拼接
          const lastDot = path.lastIndexOf('.');
          let bareKey = lastDot >= 0 ? path.slice(lastDot + 1) : path;
          // 调用根链节点：参数名是目标根链的入参名，本身可能带 "."（目标根链的私有参数），只去掉本节点 id 前缀
          const callInst = (window._orchNodeInstances || []).find(i => i.instanceId === nodeId);
          if (callInst && (callInst.type === ORCH_CALL_NODE_TYPE || callInst.type === ORCH_LOOP_NODE_TYPE)) {
            bareKey = path;
            while (bareKey.startsWith(nodeId + '.')) bareKey = bareKey.slice(nodeId.length + 1);
          }
          finalVal = '{{arguments.' + nodeId + '.' + bareKey + '}}';
        }
      }
      const inst = (window._orchNodeInstances || []).find(i => i.instanceId === nodeId);
      const isReturn = !!(inst && inst.type === 'custom/ReturnValue');
      const rr = isReturn ? ((_orchRootResponses || []).find(r => (r.key || '') === key)) : null;
      if (!result[nodeId]) result[nodeId] = {};
      const entry = { src: finalSrc, value: finalVal, private: !!isPrivate, type: keys[key].type || '' };
      if (isReturn) {
        // 像普通 Activity 节点一样保存完整的参数记录（key/label/type/required/policy/value/description）
        entry.key = key;
        entry.label = rr ? (rr.label || '') : '';
        entry.required = rr ? !!rr.required : false;
        entry.policy = 'frontend';
        entry.description = rr ? (rr.description || '') : '';
      }
      result[nodeId][key] = entry;
    });
  });
  return result;
}

// 回显已保存的 node_param_overrides（编辑加载时调用）
// 保存结构：{ paramKey: { src, value } }，src 为内部来源(fixed/upstream/entry)
// 兼容旧格式：paramKey 直接是字符串值（纯值，按格式推断来源）
function applyOrchParamOverrides(saved) {
  _orchParamPreset = {};
  _orchFixedSeeded = {};
  if (!saved) return;
  try {
    const obj = typeof saved === 'string' ? JSON.parse(saved) : saved;
    Object.keys(obj || {}).forEach(nodeId => {
      const kv = obj[nodeId] || {};
      Object.keys(kv).forEach(key => {
        const raw = kv[key];
        if (raw == null) return;
        if (!_orchParamPreset[nodeId]) _orchParamPreset[nodeId] = {};
        if (typeof raw === 'object') {
          // 新格式：明确携带 src、value 与 private
          const src = raw.src || PARAM_SRC_FIXED;
          const value = raw.value != null ? String(raw.value) : '';
          _orchParamPreset[nodeId][key] = {
            src, value, private: !!raw.private, type: raw.type || '',
            key: raw.key || key, label: raw.label || '', required: !!raw.required,
            description: raw.description || '', policy: raw.policy || 'frontend'
          };
        } else {
          // 旧格式：纯值，按格式推断
          const s = String(raw);
          let src = PARAM_SRC_FIXED;
          let value = '';
          if (/^\{\{steps\.[^.]+\.(arguments|responses)\.[^}]+?\}\}$/.test(s)) {
            src = PARAM_SRC_UPSTREAM;
            value = s;
          } else if (/^\{\{arguments\.[^}]+\}\}$/.test(s)) {
            // 调用传入：{{arguments.参数key}}
            src = PARAM_SRC_ENTRY;
            value = s.replace(/^\{\{arguments\.|\}\}$/g, '');
          } else if (/^\{\{[^}]+\}\}$/.test(s)) {
            // 调用传入（旧格式：{{参数名}}）
            src = PARAM_SRC_ENTRY;
            value = s.replace(/^\{\{|\}\}$/g, '');
          } else {
            src = PARAM_SRC_FIXED;
            value = s;
          }
          _orchParamPreset[nodeId][key] = { src, value };
        }
      });
    });
  } catch(e) { /* ignore */ }
}

// 回显已保存的 node_collapse_overrides（编辑加载时调用）。结构：{ instanceId: bool }，true=收起
function applyOrchCollapseOverrides(saved) {
  _orchCollapseState = {};
  if (!saved) return;
  try {
    const obj = typeof saved === 'string' ? JSON.parse(saved) : saved;
    Object.keys(obj || {}).forEach(id => { if (obj[id] === true) _orchCollapseState[id] = true; });
  } catch(e) { /* ignore */ }
}

// 切换某个节点参数配置实例的收起/展开，并即时更新状态（稍后随"保存"请求落到数据库）
// Live Preview 点击节点时同步选中左侧「节点参数配置」中该实例的配置块：仅高亮标记，
// 不滚动、不展开，避免打乱用户当前的浏览位置（需要时自行下拉查看）。
// 选中实例记录在 _orchActiveParamInst，参数区重渲染后保持高亮；无参数的节点在配置区没有对应块，只清除旧高亮。
let _orchActiveParamInst = '';
function selectOrchParamBlock(instanceId) {
  _orchActiveParamInst = instanceId || '';
  document.querySelectorAll('#orch-param-list .override-node-block').forEach(b => {
    b.classList.toggle('override-node-active', b.getAttribute('data-inst') === _orchActiveParamInst);
  });
}

function toggleOrchParamBlock(headerEl, instanceId, ev) {
  // 在名称 / instanceId 上拖选复制时不要折叠：这些元素已放开 user-select，点击它们只用于选中文本
  if (ev && ev.target && ev.target.closest && ev.target.closest('.ov-copyable')) return;
  const body = headerEl.nextElementSibling;
  if (!body) return;
  const nowCollapsed = body.classList.toggle('hidden');
  const icon = headerEl.querySelector('.toggle-icon');
  if (icon) icon.textContent = nowCollapsed ? '▸' : '▾';
  if (nowCollapsed) _orchCollapseState[instanceId] = true;
  else delete _orchCollapseState[instanceId];
}

// 收集收起状态（仅记录收起的实例），随保存请求持久化
function collectOrchCollapseOverrides() {
  const result = {};
  Object.keys(_orchCollapseState).forEach(id => { if (_orchCollapseState[id] === true) result[id] = true; });
  return result;
}

// ============ 每个节点实例的 switch_condition 覆盖（仅本链生效，不改节点定义）============

// 回显已保存的 node_switch_overrides（编辑加载时调用）。结构：{ instanceId: expr }
// 可直接是对象或 JSON 字符串。
function applyOrchSwitchOverrides(saved) {
  _orchSwitchOverrides = {};
  if (!saved) return;
  try {
    const obj = typeof saved === 'string' ? JSON.parse(saved) : saved;
    Object.keys(obj || {}).forEach(k => {
      const v = obj[k];
      if (v != null && String(v) !== '') _orchSwitchOverrides[k] = String(v);
    });
  } catch (e) { /* ignore */ }
}

// 收集当前所有节点的 switch_condition 覆盖，返回 { instanceId: expr }
function collectOrchSwitchOverrides() {
  const out = {};
  Object.keys(_orchSwitchOverrides || {}).forEach(k => {
    const v = _orchSwitchOverrides[k];
    if (v != null && String(v).trim() !== '') out[k] = String(v).trim();
  });
  return out;
}

// 回显已保存的 node_name_overrides（编辑加载时调用）。结构：{ instanceId: name }
function applyOrchNameOverrides(saved) {
  _orchNameOverrides = {};
  if (!saved) return;
  try {
    const obj = typeof saved === 'string' ? JSON.parse(saved) : saved;
    Object.keys(obj || {}).forEach(k => {
      const v = obj[k];
      if (v != null && String(v).trim() !== '') _orchNameOverrides[k] = String(v).trim();
    });
  } catch (e) { /* ignore */ }
}

// 收集当前所有节点的名称覆盖，返回 { instanceId: name }
function collectOrchNameOverrides() {
  const out = {};
  Object.keys(_orchNameOverrides || {}).forEach(k => {
    const v = _orchNameOverrides[k];
    if (v != null && String(v).trim() !== '') out[k] = String(v).trim();
  });
  return out;
}

// 生成「可用变量（点击复制）」样式的小块 HTML（与节点编辑页一致）
// 注意：点击复制的值放在 data-clip 属性中，由 copyOrchVarSnippet 读取，
// 避免 inline onclick 里 JSON.stringify 产生的双引号破坏属性导致复制失效。
function orchSwitchVarSnippetHtml(ref, meta) {
  return '<div class="cond-var-snippet" data-clip="' + escAttr(ref) + '" onclick="copyOrchVarSnippet(this)" title="点击复制" style="cursor:pointer;display:inline-flex;align-items:center;gap:6px;margin:0 6px 6px 0;padding:3px 8px;border:1px solid var(--border);border-radius:4px;font-family:monospace;font-size:.74rem;color:var(--accent);background:#f3f6fb">' +
    '<span>' + escHtml(ref) + '</span>' +
    (meta ? '<span style="color:var(--text-muted);font-size:.7rem;font-family:inherit">' + escHtml(meta) + '</span>' : '') +
    '</div>';
}

// 读取 data-clip 属性中的值并复制到剪贴板（供 Live Preview 编辑节点弹窗的变量小块使用）
function copyOrchVarSnippet(el) {
  const t = el.getAttribute('data-clip');
  if (t != null && t !== '') copyToClipboard(t);
}

// 渲染 switch 编辑器里的「输入参数 / 输出返回值」可复制列表（与节点编辑页「可用变量」一致）
function renderOrchSwitchVarBoxes(inBox, outBox, n, instanceId) {
  const parseVarArr = (v) => {
    if (!v) return [];
    if (Array.isArray(v)) return v;
    try { const p = JSON.parse(v); return Array.isArray(p) ? p : []; } catch(e) { return []; }
  };
  const inItems = [], outItems = [];
  // 条件判断节点：节点定义参数与本链定义的参数合并展示（同名以本链为准）
  const inst = instanceId ? (window._orchNodeInstances || []).find(x => x.instanceId === instanceId) : null;
  const inParams = isOrchCondInst(inst) ? mergeOrchCondParams(inst, n ? parseVarArr(n.params) : []) : (n ? parseVarArr(n.params) : []);
  inParams.forEach(p => {
    if (!p || !p.key) return;
    inItems.push({ ref: '[arguments.' + p.key + ']', meta: [p.label, p.type].filter(Boolean).join(' / ') });
  });
  if (n) {
    parseVarArr(n.outputs).forEach(o => {
      if (!o || !o.key) return;
      outItems.push({ ref: '[responses.' + o.key + ']', meta: [o.label, o.type].filter(Boolean).join(' / ') });
    });
  }
  inBox.innerHTML = inItems.length
    ? inItems.map(it => orchSwitchVarSnippetHtml(it.ref, it.meta)).join('')
    : '<div style="font-size:.72rem;color:var(--text-muted)">该节点无输入参数定义</div>';
  outBox.innerHTML = outItems.length
    ? outItems.map(it => orchSwitchVarSnippetHtml(it.ref, it.meta)).join('')
    : '<div style="font-size:.72rem;color:var(--text-muted)">该节点无输出返回值定义</div>';
}

// 打开某个节点实例（instanceId）的 switch_condition 编辑器。
// 支持 Activity / CondSwitch 节点：有节点定义时回填默认值，缺失时直接编辑本链值。
// 参数列表（输入参数/输出返回值）直接从节点详情接口拉取，确保与节点编辑页一致、不漏参数。
async function orchOpenNodeSwitchEditor(instanceId) {
  if (!instanceId) { showToast('未指定节点', 'error'); return; }
  _orchSwitchEditId = instanceId;
  const modal = document.getElementById('orch-switch-modal');
  if (!modal) { showToast('编辑器未找到', 'error'); return; }
  const openSubBtn = document.getElementById('orch-switch-open-sub');
  if (openSubBtn) openSubBtn.style.display = 'none';
  if (getSelectedOrchSubIds().includes(instanceId)) { orchOpenSubSwitchEditor(instanceId, modal); return; }

  // 尝试从已选实例/节点定义取默认值（缺失也不影响保存）
  const inst = (window._orchNodeInstances || []).find(x => x.instanceId === instanceId);
  const nodeId = inst ? inst.nodeId : (instanceId.split('__')[0] || instanceId);
  const n = orchNodeDefById(nodeId); // 注意 _orchNodes 不在 window 上，必须走该 helper
  const hasDef = !!n;
  const defExpr = hasDef ? nodeSwitchConditionText(n) : '';
  const hasOverride = Object.prototype.hasOwnProperty.call(_orchSwitchOverrides, instanceId);
  const curExpr = hasOverride ? _orchSwitchOverrides[instanceId] : defExpr;

  // 标题用实例生效名（优先名称覆盖 _orchNameOverrides，与 Live Preview 图上一致），
  // 不能只用 n.name（节点定义名），否则改过名后弹窗标题仍显示旧名。
  document.getElementById('orch-switch-node-name').textContent =
    (orchInstanceDisplayName(inst, n) || nodeId) + '（' + instanceId + '）';
  document.getElementById('orch-switch-override-flag').textContent = hasOverride
    ? '当前为覆盖值（仅本链生效，不影响节点定义）'
    : (hasDef ? '当前为节点默认值（未覆盖）' : '节点定义不在列表中，直接编辑本链中的路由条件');
  // 判断节点类型是否支持路由条件（仅 Activity / CondSwitch 适用）
  const nodeType = (inst && inst.type) ? inst.type : (hasDef ? n.type : '');
  const isSwitchable = (nodeType === 'custom/Activity' || nodeType === 'custom/CondSwitch');
  const condWrap = document.getElementById('orch-switch-cond-wrap');
  if (condWrap) condWrap.style.display = isSwitchable ? 'block' : 'none';
  // 参数输入节点 / 条件判断节点：查出已添加的参数列表，可继续新增、修改、删除
  const inputWrap = document.getElementById('orch-switch-input-wrap');
  const isInput = nodeType === ORCH_INPUT_NODE_TYPE || nodeType === ORCH_COND_NODE_TYPE;
  if (inputWrap) inputWrap.style.display = isInput ? 'block' : 'none';
  if (isInput) fillOrchInputParamRows(instanceId, ORCH_INPUT_SWITCH_BOX_ID);

  const ta = document.getElementById('orch-switch-text');
  ta.value = isSwitchable ? ((curExpr === undefined || curExpr === null) ? '' : curExpr) : '';
  ta.placeholder = '例如：In(\'K1\', [responses.tag_list])';
  // 名称输入框：回填当前实例生效名称（优先 _orchNameOverrides 名称覆盖，其次 inst.name，最后节点定义名）；
  // 并缓存节点定义默认名用于「用节点默认值」判定覆盖。
  _orchSwitchDefaultName = hasDef ? (n.name || '') : (inst ? (inst.name || '') : '');
  const nameInput = document.getElementById('orch-switch-name');
  const curName = orchInstanceDisplayName(inst, n);
  if (nameInput) nameInput.value = curName || '';
  // 先用缓存渲染，再用节点详情接口校准（保证参数/输出列表准确，不受列表接口过滤影响）
  const inBox = document.getElementById('orch-switch-input-vars');
  const outBox = document.getElementById('orch-switch-output-vars');
  renderOrchSwitchVarBoxes(inBox, outBox, hasDef ? n : null, instanceId);
  modal.style.display = 'flex';

  // 直接拉取节点详情（含 params/outputs/switch_condition），覆盖缓存结果，确保展示该节点真实参数与路由条件
  if (nodeId) {
    try {
      const detail = await api('/api/nodes/' + encodeURIComponent(nodeId));
      if (detail) {
        renderOrchSwitchVarBoxes(inBox, outBox, detail, instanceId);
        _orchSwitchDefaultName = detail.name || _orchSwitchDefaultName;
        // 若缓存未取到节点名，用详情补充标题
        if (!hasDef) {
          document.getElementById('orch-switch-node-name').textContent =
            ((detail.name || nodeId) + '（' + instanceId + '）');
          const ni2 = document.getElementById('orch-switch-name');
          if (ni2 && !ni2.value) ni2.value = detail.name || '';
        }
        // 用详情回填 switch_condition 文本框（缓存缺失时也能显示该节点真实路由条件）
        const detailType = detail.type;
        const isSwitchableDetail = (detailType === 'custom/Activity' || detailType === 'custom/CondSwitch');
        const ta2 = document.getElementById('orch-switch-text');
        if (ta2 && isSwitchableDetail) {
          const hasOverride = Object.prototype.hasOwnProperty.call(_orchSwitchOverrides, instanceId);
          if (!hasOverride) {
            const defExpr = nodeSwitchConditionText(detail);
            ta2.value = (defExpr === undefined || defExpr === null) ? '' : defExpr;
          }
        }
      }
    } catch (e) { /* 保留缓存渲染结果 */ }
  }
}

// 打开子链实例的编辑器：名称覆盖 + 路由条件（按本实例入参 [arguments.x] 与子链返回值 [responses.x] 求值），
// 未配置路由条件时沿用子链结束时的关系往下走。变量列表取子链调用签名（入参 / 返回值定义）。
function orchOpenSubSwitchEditor(instanceId, modal) {
  const s = orchSubDefOf(instanceId);
  const hasOverride = Object.prototype.hasOwnProperty.call(_orchSwitchOverrides, instanceId);
  document.getElementById('orch-switch-node-name').textContent = '🔗 ' + orchSubDisplayName(instanceId) + '（' + instanceId + '）';
  document.getElementById('orch-switch-override-flag').textContent = hasOverride
    ? '子链实例：按路由条件分流（仅本链生效）'
    : '子链实例：未配置路由条件，沿用子链结束时的关系（通常为 Success）';
  const condWrap = document.getElementById('orch-switch-cond-wrap');
  if (condWrap) condWrap.style.display = 'block';
  const inputWrap = document.getElementById('orch-switch-input-wrap');
  if (inputWrap) inputWrap.style.display = 'none';
  const ta = document.getElementById('orch-switch-text');
  ta.value = hasOverride ? String(_orchSwitchOverrides[instanceId] || '') : '';
  ta.placeholder = '例如：[responses.out] > 5（True/False），或 If([responses.out] > 5, \'Big\', \'Small\')（自定义关系）';
  _orchSwitchDefaultName = (s && s.name) ? s.name : orchSubBaseId(instanceId);
  const nameInput = document.getElementById('orch-switch-name');
  if (nameInput) nameInput.value = orchSubDisplayName(instanceId);
  const openSubBtn = document.getElementById('orch-switch-open-sub');
  if (openSubBtn) openSubBtn.style.display = '';
  renderOrchSwitchVarBoxes(document.getElementById('orch-switch-input-vars'), document.getElementById('orch-switch-output-vars'),
    { params: getOrchSubParams(instanceId), outputs: getOrchSubOutputs(instanceId) });
  modal.style.display = 'flex';
}

// 保存当前编辑的节点：名称覆盖 + switch_condition 覆盖（均仅本链实例生效，不修改节点定义）
function saveOrchSwitchOverride() {
  const id = _orchSwitchEditId;
  if (!id) { showToast('未指定编辑节点', 'error'); return; }
  const inst = (window._orchNodeInstances || []).find(x => x.instanceId === id);
  const isSub = getSelectedOrchSubIds().includes(id);
  const isSwitchable = isSub || (inst && (inst.type === 'custom/Activity' || inst.type === 'custom/CondSwitch'));
  // 参数输入节点 / 条件判断节点：先校验参数列表，不通过时不关闭弹框
  const hasInputDefs = isOrchInputInst(inst) || isOrchCondInst(inst);
  const inputDefs = hasInputDefs ? collectOrchInputParamRows(ORCH_INPUT_SWITCH_BOX_ID) : null;
  if (hasInputDefs && !inputDefs) return;
  const ta = document.getElementById('orch-switch-text');
  const val = (ta.value || '').trim();
  // 路由条件仅在 Activity / CondSwitch 节点写入；其它节点忽略，并清除可能残留的覆盖
  if (isSwitchable) {
    if (val === '') { delete _orchSwitchOverrides[id]; } else { _orchSwitchOverrides[id] = val; }
  } else {
    delete _orchSwitchOverrides[id];
  }
  // 名称覆盖：与节点定义默认名不同才写入，否则视为无覆盖（回退默认名）
  const nameInput = document.getElementById('orch-switch-name');
  const nameVal = nameInput ? (nameInput.value || '').trim() : '';
  if (nameVal === '' || nameVal === _orchSwitchDefaultName) {
    delete _orchNameOverrides[id];
  } else {
    _orchNameOverrides[id] = nameVal;
  }
  // 立即更新内存中该实例的显示名称，使 Live Preview 同步
  if (inst) inst.name = nameVal !== '' ? nameVal : _orchSwitchDefaultName;
  const modal = document.getElementById('orch-switch-modal');
  if (modal) modal.style.display = 'none';
  _orchSwitchEditId = '';
  renderOrchPreview();
  // 图上编辑后立即刷新下方 DSL Preview，保持联动
  renderOrchDslPreview();
  // 名称变更后同步刷新 Connections 下拉，使左侧 select 展示名与右侧 Live Preview 一致
  refreshOrchConnOptions();
  // 同步刷新「节点参数配置」列表：其标题用实例生效名，改名后必须重渲染才不会残留旧名
  renderOrchParamOverrides();
  if (inputDefs) {
    applyOrchInputDefs(id, inputDefs);
    renderOrchNodeSelected();
    onOrchSelectionChange();
  }
  showToast(isSwitchable ? (inputDefs ? '已更新该节点名称、参数与路由条件（仅本链生效）' : '已更新该节点名称与路由条件（仅本链生效）')
    : (inputDefs ? '已更新该节点名称与参数列表（仅本链实例生效）' : '已更新该节点名称（仅本链实例生效）'), 'success');
}

// 根据参数值推断来源（调用传入/引用节点/固定配置），用于从 DSL arguments 兜底回显
function inferOrchParamPreset(val) {
  const s = (val == null) ? '' : String(val);
  if (!s) return null;
  if (/^\{\{steps\.[^.]+\.(arguments|responses)\.[^}]+?\}\}$/.test(s)) {
    return { src: PARAM_SRC_UPSTREAM, value: s };
  }
  if (/^\{\{arguments\.[^}]+\}\}$/.test(s)) {
    // 调用传入：{{arguments.参数key}}（兼容已污染的 {{arguments.arguments.x}}）
    let k = s.replace(/^\{\{|\}\}$/g, '');
    while (k.startsWith('arguments.')) k = k.slice('arguments.'.length);
    return { src: PARAM_SRC_ENTRY, value: k };
  }
  if (/^\{\{[^}]+\}\}$/.test(s)) {
    // 调用传入（旧格式：{{参数名}}）
    return { src: PARAM_SRC_ENTRY, value: s.replace(/^\{\{|\}\}$/g, '') };
  }
  return { src: PARAM_SRC_FIXED, value: s };
}

// 根据参数值推断来源（调用传入/引用节点/固定配置），用于从 DSL arguments 兜底回显
function inferOrchParamPreset(val) {
  const s = (val == null) ? '' : String(val);
  if (!s) return null;
  if (/^\{\{steps\.[^.]+\.(arguments|responses)\.[^}]+?\}\}$/.test(s)) {
    return { src: PARAM_SRC_UPSTREAM, value: s };
  }
  if (/^\{\{arguments\.[^}]+\}\}$/.test(s)) {
    // 调用传入：{{arguments.参数key}}（兼容已污染的 {{arguments.arguments.x}}）
    let k = s.replace(/^\{\{|\}\}$/g, '');
    while (k.startsWith('arguments.')) k = k.slice('arguments.'.length);
    return { src: PARAM_SRC_ENTRY, value: k };
  }
  if (/^\{\{[^}]+\}\}$/.test(s)) {
    // 调用传入（旧格式：{{参数名}}）
    return { src: PARAM_SRC_ENTRY, value: s.replace(/^\{\{|\}\}$/g, '') };
  }
  return { src: PARAM_SRC_FIXED, value: s };
}

let _orchPreviewScale = 1;

function applyOrchPreviewScale() {
  const box = document.getElementById('orch-preview-scale');
  if (box) box.style.transform = 'scale(' + _orchPreviewScale + ')';
  const label = document.getElementById('orch-zoom-label');
  if (label) label.textContent = Math.round(_orchPreviewScale * 100) + '%';
  // 缩放后可视尺寸变化，重算尺寸补偿，使横向滚动条始终紧贴图片底部
  syncOrchPreviewSize();
  // 缩放后内容尺寸可能变化，下一帧将预览区滚动到内容中心，方便查看
  requestAnimationFrame(centerOrchPreview);
}

// 让预览区【只在横向】滚动：纵向不设高度（交给页面滚动条），横向保留滚动条避免把页面撑宽。
// 演进：① .orch-preview-scale 写死 height:100% + 预览区 flex:1 撑满剩余高度 →
//   图片矮时下方大片空白，横向滚动条离图片底部很远（要拖它得先往下滚一大段）。
//   ② 给预览区写死「图片可视高度」→ 图一高就出现纵向滚动条，默认打开也要滚。
//   ③ 现在预览区高度完全不设（height:auto），改用缩放层的 margin-bottom 抵消 transform 差值，
//      使「内容盒高度」恰好等于图片可视高度：横向滚动条紧贴图片底部，纵向永不溢出。
function syncOrchPreviewSize() {
  const area = document.getElementById('orch-preview');
  const box = document.getElementById('orch-preview-scale');
  if (!area || !box) return;
  // 先清掉上次写入的补偿值，否则本次量到的是被上一次撑开的尺寸
  box.style.height = '';
  box.style.marginBottom = '';
  // 纵向不设高度：内容多高，预览区就多高
  area.style.height = 'auto';
  area.style.maxHeight = 'none';
  const el = box.querySelector('.mermaid');
  if (!el) return;
  const svg = el.querySelector('svg');
  const layoutH = Math.max(el.offsetHeight || 0, svg ? (svg.offsetHeight || 0) : 0);
  if (layoutH <= 0) return;
  // 预览区在编排页是 flex:1（flex-basis:0% 会覆盖 height），必须改成不伸展才能真正按内容高度收缩；
  // 同时清掉只读模式下的 min-height:70vh，否则高度仍被撑开
  area.style.flex = '0 0 auto';
  area.style.minHeight = '0';
  box.style.height = layoutH + 'px';
  // transform 不影响布局：内容盒仍是 layoutH，但视觉上是 layoutH*scale。
  // 用 margin-bottom 补/抵掉差值（放大为正、缩小为负），使内容盒高度 = 可视高度，
  // 横向滚动条正好落在图片最下端，且不会出现纵向滚动条。
  const delta = Math.ceil(layoutH * (_orchPreviewScale - 1));
  if (delta !== 0) box.style.marginBottom = delta + 'px';
}

// 视口变化后图片布局尺寸可能变化，重新贴合一次（节流，避免拖拽时频繁重排）
let _orchResizeTimer = null;
window.addEventListener('resize', () => {
  if (_orchResizeTimer) clearTimeout(_orchResizeTimer);
  _orchResizeTimer = setTimeout(syncOrchPreviewSize, 150);
});

// 将 Live Preview 滚动到内容中心（节点多/放大后自动居中显示）
function centerOrchPreview() {
  const area = document.getElementById('orch-preview');
  if (!area) return;
  area.scrollLeft = Math.max(0, (area.scrollWidth - area.clientWidth) / 2);
  area.scrollTop = Math.max(0, (area.scrollHeight - area.clientHeight) / 2);
}

// Live Preview 空白处按住鼠标拖动平移画面（放大后不用来回拖滚动条）：
// 横向滚动预览区自身，纵向滚动页面（预览区纵向不溢出，纵向交给页面滚动条，见 syncOrchPreviewSize）。
// 在节点、连线标签、连线把手、泡泡层上按下不触发，保留原有点击/拖拽连线行为。只绑定一次。
function initOrchPreviewPan() {
  const area = document.getElementById('orch-preview');
  if (!area || area._orchPanBound) return;
  area._orchPanBound = true;
  area.addEventListener('mousedown', (e) => {
    if (e.button !== 0) return;
    const t = e.target;
    if (t && t.closest && t.closest('g.node, .orch-edge-label, .orch-node-link, button, input, textarea, select, a')) return;
    e.preventDefault();
    const startX = e.clientX, startY = e.clientY;
    const startLeft = area.scrollLeft, startTop = area.scrollTop;
    const startPageX = window.scrollX, startPageY = window.scrollY;
    area.classList.add('orch-panning');
    const onMove = (ev) => {
      const dx = ev.clientX - startX, dy = ev.clientY - startY;
      area.scrollLeft = startLeft - dx;
      area.scrollTop = startTop - dy;
      window.scrollTo(startPageX, startPageY - dy);
    };
    const onUp = () => {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
      area.classList.remove('orch-panning');
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  });
}

// 放大/缩小：delta>0 放大，delta<0 缩小，限制 0.3~3 倍
function zoomOrchPreview(delta) {
  _orchPreviewScale = Math.min(3, Math.max(0.3, _orchPreviewScale + delta));
  applyOrchPreviewScale();
}

function resetOrchPreview() {
  _orchPreviewScale = 1;
  applyOrchPreviewScale();
}

async function renderOrchPreview() {
  initOrchPreviewPan();
  const container = document.getElementById('orch-preview');
  const scaleBox = document.getElementById('orch-preview-scale');
  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();
  const conns = collectOrchConnections();

  if (nodeIds.length === 0 && subIds.length === 0) {
    scaleBox.innerHTML = '<div class="orch-preview-empty">选择节点并添加连接后，这里将显示实时流程图</div>';
    _orchPreviewScale = 1;
    scaleBox.style.height = '';
    scaleBox.style.marginBottom = '';
    container.style.height = '';
    container.style.maxHeight = '';
    container.style.flex = '';
    container.style.minHeight = '';
    applyOrchPreviewScale();
    return;
  }

  const syntax = orchBuildMermaid();
  // Use a unique ID for mermaid rendering
  const mermaidId = 'orch-mermaid-' + Date.now();
  scaleBox.innerHTML = `<div class="mermaid" id="${mermaidId}">${esc(syntax)}</div>`;
  // Re-render with mermaid
  try {
    const el = document.getElementById(mermaidId);
    if (el) {
      el.removeAttribute('data-processed');
      el.textContent = syntax;
      await mermaid.run({ nodes: [el] });
      enhanceOrchPreviewNodes(el);
      enhanceOrchPreviewEdges(el);
      // 重绘后重新叠加 traceId 执行路径高亮（未查询时无操作）
      applyOrchTraceHighlight();
    }
    applyOrchPreviewScale();
    // mermaid 内部布局可能稍晚稳定，延迟再居中一次确保内容位于视野中间
    setTimeout(centerOrchPreview, 60);
    // 图片尺寸在 mermaid 布局后才会最终确定，延迟再贴合一次高度
    setTimeout(syncOrchPreviewSize, 80);
  } catch(e) {
    scaleBox.innerHTML = '<div class="orch-preview-empty" style="color:#ef4444">渲染失败: ' + esc(e.message) + '</div><pre style="font-size:.7rem;margin-top:8px;white-space:pre-wrap;max-height:150px;overflow:auto">' + esc(syntax) + '</pre>';
  }
}

// ============================================================
// Live Preview 按 traceId 回放执行路径
// ============================================================
// 查询结果缓存：改连接、缩放等触发重绘后重新叠加高亮，直到点「清除」。
// 结构：{ traceId, chainId, steps:[{instId, order, log}], nodes:{instId:{orders, error}}, edges:Set('from|to') }
let _orchTrace = null;

// 按输入的 traceId + 当前根链 chain_id 查询 node 运行日志，按时间顺序在图上标出执行路径
async function queryOrchTracePath() {
  const input = document.getElementById('orch-trace-input');
  const traceId = ((input && input.value) || '').trim();
  if (!traceId) { showToast('请输入 traceId', 'error'); return; }
  const chainEl = document.getElementById('orch-chain-id');
  const chainId = ((chainEl && chainEl.value) || '').trim();
  if (!chainId) { showToast('当前没有加载根链，无法按 chain_id 查询日志', 'error'); return; }
  const btn = document.getElementById('orch-trace-btn');
  if (btn) { btn.disabled = true; btn.textContent = '查询中…'; }
  try {
    const logs = await fetchOrchTraceLogs(traceId, chainId);
    _orchTrace = buildOrchTracePath(traceId, chainId, logs);
    applyOrchTraceHighlight();
    syncOrchTraceClearBtn();
    if (!logs.length) showToast('该 traceId 在当前根链下没有节点日志', 'error');
    else if (!_orchTrace.steps.length) showToast('查到日志，但节点都不在当前图中', 'error');
  } catch (e) {
    showToast('查询日志失败: ' + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = '查询'; }
  }
}

// 清除执行路径高亮
function clearOrchTrace() {
  _orchTrace = null;
  applyOrchTraceHighlight();
  syncOrchTraceClearBtn();
}

// 分页拉取该 trace 在当前根链下的全部 node 日志（最多 1000 条），按执行时间正序返回
async function fetchOrchTraceLogs(traceId, chainId) {
  const pageSize = 200;
  const maxPages = 5;
  let all = [];
  for (let page = 1; page <= maxPages; page++) {
    const params = new URLSearchParams();
    params.set('trace_id', traceId);
    params.set('root_chain_id', chainId);
    params.set('page', String(page));
    params.set('page_size', String(pageSize));
    const resp = await api('/api/node-logs?' + params.toString());
    const list = (resp && resp.list) || [];
    all = all.concat(list);
    if (list.length < pageSize || all.length >= ((resp && resp.total) || 0)) break;
  }
  // flow_total 是整条链的汇总记录，没有 node_id，不参与路径
  all = all.filter(r => r && r.node_id && r.level !== 'flow_total');
  // ts 只到秒，同一秒内用 created_at（毫秒）和自增 id 兜底排序
  const t = r => {
    const ms = Date.parse(r.created_at || '');
    return isNaN(ms) ? (Number(r.timestamp) || 0) * 1000 : ms;
  };
  all.sort((a, b) => (t(a) - t(b)) || ((a.id || 0) - (b.id || 0)));
  return all;
}

// 由按时间排好序的日志计算执行路径：经过的节点（带执行序号）与走过的连线
function buildOrchTracePath(traceId, chainId, logs) {
  const graphIds = new Set([...getSelectedOrchNodeIds(), ...getSelectedOrchSubIds()]);
  const nodes = {};
  const steps = [];
  logs.forEach(r => {
    const id = r.node_id;
    if (!graphIds.has(id)) return;
    const order = steps.length + 1;
    steps.push({ instId: id, order: order, log: r });
    const n = nodes[id] || (nodes[id] = { orders: [], error: false });
    n.orders.push(order);
    if (r.level === 'error' || r.error_msg) n.error = true;
  });
  // 连线：节点执行完按 relation_type 往下走，目标节点在其之后也执行过才算走过这条线
  const conns = collectOrchConnections();
  const edges = new Set();
  steps.forEach(s => {
    const rel = s.log.relation_type || 'Success';
    conns.forEach(c => {
      if (c.from_id !== s.instId || (c.type || 'Success') !== rel) return;
      const target = nodes[c.to_id];
      if (target && target.orders.some(o => o > s.order)) edges.add(c.from_id + '|' + c.to_id);
    });
  });
  // 兜底：相邻两步之间存在连线、但 relation_type 没对上（如历史日志缺字段）时，仍按先后顺序连起来
  for (let i = 0; i + 1 < steps.length; i++) {
    const a = steps[i].instId;
    const b = steps[i + 1].instId;
    const hasOut = [...edges].some(k => k.startsWith(a + '|'));
    if (!hasOut && conns.some(c => c.from_id === a && c.to_id === b)) edges.add(a + '|' + b);
  }
  return { traceId: traceId, chainId: chainId, steps: steps, nodes: nodes, edges: edges };
}

// 在当前 Live Preview 的 SVG 上叠加/清除执行路径高亮
function applyOrchTraceHighlight() {
  const svg = document.querySelector('#orch-preview-scale .mermaid svg');
  if (!svg) return;
  const SVGNS = 'http://www.w3.org/2000/svg';
  // 先清掉上一次的标记
  svg.classList.remove('orch-trace-on');
  svg.querySelectorAll('.orch-trace-node').forEach(el => el.classList.remove('orch-trace-node', 'orch-trace-node-err'));
  svg.querySelectorAll('.orch-trace-edge').forEach(el => {
    el.classList.remove('orch-trace-edge');
    if (el.hasAttribute('data-orch-marker')) {
      const m = el.getAttribute('data-orch-marker');
      if (m) el.setAttribute('marker-end', m); else el.removeAttribute('marker-end');
      el.removeAttribute('data-orch-marker');
    }
  });
  svg.querySelectorAll('.orch-trace-label').forEach(el => el.classList.remove('orch-trace-label'));
  svg.querySelectorAll('[data-orch-style]').forEach(el => {
    el.setAttribute('style', el.getAttribute('data-orch-style'));
    el.removeAttribute('data-orch-style');
  });
  if (!_orchTrace || !_orchTrace.steps.length) return;

  svg.classList.add('orch-trace-on');
  const idMap = window._orchIdMap || {};
  const reEsc = x => String(x).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  // 路径专用箭头（醒目红色），mermaid 默认箭头是灰色
  let defs = svg.querySelector('defs');
  if (!defs) { defs = document.createElementNS(SVGNS, 'defs'); svg.insertBefore(defs, svg.firstChild); }
  if (!svg.querySelector('#orch-trace-arrow')) {
    const marker = document.createElementNS(SVGNS, 'marker');
    marker.setAttribute('id', 'orch-trace-arrow');
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
  }

  // 节点：加醒目描边（报错节点用深红虚线）
  const nodeGs = Array.from(svg.querySelectorAll('g.node'));
  Object.keys(_orchTrace.nodes).forEach(instId => {
    const safe = idMap[instId];
    if (!safe) return;
    const re = new RegExp('flowchart-' + reEsc(safe) + '-\\d+$');
    const g = nodeGs.find(x => re.test(x.id || ''));
    if (!g) return;
    const info = _orchTrace.nodes[instId];
    g.classList.add('orch-trace-node');
    if (info.error) g.classList.add('orch-trace-node-err');
    // mermaid classDef 把描边写在形状的内联 style（带 !important）上，样式表压不住，只能改内联并记下原值以便清除时还原
    g.querySelectorAll(':scope > .label-container, :scope > g > .label-container').forEach(shape => {
      shape.setAttribute('data-orch-style', shape.getAttribute('style') || '');
      shape.style.setProperty('stroke', info.error ? '#7f1d1d' : '#ef4444', 'important');
      shape.style.setProperty('stroke-width', '4px', 'important');
      if (info.error) shape.style.setProperty('stroke-dasharray', '6 3', 'important');
    });
  });

  // 连线：路径加粗变红并带流动动画，箭头换成红色；对应的 relationType 标签同步高亮
  const edgePaths = Array.from(svg.querySelectorAll('path[data-id]'));
  const labelEls = Array.from(svg.querySelectorAll('.edgeLabel [data-id], .edgeLabels [data-id]'));
  _orchTrace.edges.forEach(key => {
    const parts = key.split('|');
    const from = idMap[parts[0]];
    const to = idMap[parts[1]];
    if (!from || !to) return;
    const re = new RegExp('^L_' + reEsc(from) + '_' + reEsc(to) + '_\\d+$');
    edgePaths.filter(p => re.test(p.getAttribute('data-id') || '')).forEach(p => {
      p.classList.add('orch-trace-edge');
      p.setAttribute('data-orch-marker', p.getAttribute('marker-end') || '');
      p.setAttribute('marker-end', 'url(#orch-trace-arrow)');
    });
    labelEls.filter(l => re.test(l.getAttribute('data-id') || '')).forEach(l => {
      const lg = l.closest('g.edgeLabel') || l;
      lg.classList.add('orch-trace-label');
    });
  });
}

// 有查询结果时才显示「清除」按钮
function syncOrchTraceClearBtn() {
  const clearBtn = document.getElementById('orch-trace-clear');
  if (clearBtn) clearBtn.style.display = _orchTrace ? '' : 'none';
}

// 在 Live Preview 的 mermaid 节点右上角叠加"删除叉"，hover 节点时显示，点击删除该实例
// 注：叉默认常显（pointer-events:all），避免部分环境下 :hover 不触发导致无法看到/点击
function enhanceOrchPreviewNodes(container) {
  if (!container) return;
  const idMap = window._orchIdMap || {};
  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();
  const SVGNS = 'http://www.w3.org/2000/svg';
  const reEsc = x => String(x).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const nodeGs = Array.from(container.querySelectorAll('g.node'));
  nodeGs.forEach((g, idx) => {
    // 反查实例 ID：mermaid 节点 g.id 形如 flowchart-<safeId>-<n>，按 idMap 的 safeId 精确匹配
    let instId = null;
    if (g.id) {
      for (const k in idMap) {
        if (idMap[k] === g.id || new RegExp('flowchart-' + reEsc(idMap[k]) + '-\\d+$').test(g.id)) { instId = k; break; }
      }
    }
    // 兜底：按 DOM 顺序映射（mermaid 改写节点 id 时生效）；超出 node/sub 范围的孤立节点不加叉
    if (!instId) {
      if (idx < nodeIds.length) instId = nodeIds[idx];
      else if (idx < nodeIds.length + subIds.length) instId = subIds[idx - nodeIds.length];
    }
    if (!instId) return;
    // 防御：移除可能已存在的叉
    const old = g.querySelector(':scope > g.orch-node-del');
    if (old) old.remove();
    let bbox;
    try { bbox = g.getBBox(); } catch(e) { return; }
    if (!bbox || !bbox.width) return;
    const px = bbox.x + bbox.width - 9;
    const py = bbox.y + 9;
    const del = document.createElementNS(SVGNS, 'g');
    del.setAttribute('class', 'orch-node-del');
    del.setAttribute('transform', `translate(${px},${py})`);
    // 链路中间节点（同时存在父节点与子节点）不显示删除叉，避免误删破坏拓扑
    const isMidChain = orchNodeHasParentAndChild(instId);
    // 透明矩形扩大点击热区，避免误触节点本体打开编辑器
    const hit = document.createElementNS(SVGNS, 'rect');
    hit.setAttribute('x', '-9'); hit.setAttribute('y', '-9');
    hit.setAttribute('width', '18'); hit.setAttribute('height', '18');
    hit.setAttribute('fill', 'transparent');
    const x1 = document.createElementNS(SVGNS, 'line');
    const x2 = document.createElementNS(SVGNS, 'line');
    x1.setAttribute('x1', '-4'); x1.setAttribute('y1', '-4'); x1.setAttribute('x2', '4'); x1.setAttribute('y2', '4');
    x2.setAttribute('x1', '-4'); x2.setAttribute('y1', '4'); x2.setAttribute('x2', '4'); x2.setAttribute('y2', '-4');
    x1.setAttribute('class', 'orch-node-del-x'); x2.setAttribute('class', 'orch-node-del-x');
    del.appendChild(hit); del.appendChild(x1); del.appendChild(x2);
    if (!isMidChain) g.appendChild(del);

    // 节点底部中间的连线把手：从这里按下拖到另一个节点即新增一条 from → to 连接（只读预览不显示）
    g.setAttribute('data-orch-inst', instId);
    if (!document.body.classList.contains('orch-viewonly')) {
      const oldLink = g.querySelector(':scope > g.orch-node-link');
      if (oldLink) oldLink.remove();
      const link = document.createElementNS(SVGNS, 'g');
      link.setAttribute('class', 'orch-node-link');
      link.setAttribute('transform', `translate(${bbox.x + bbox.width / 2},${bbox.y + bbox.height})`);
      const linkHit = document.createElementNS(SVGNS, 'circle');
      linkHit.setAttribute('r', '9');
      linkHit.setAttribute('fill', 'transparent');
      const dot = document.createElementNS(SVGNS, 'circle');
      dot.setAttribute('r', '4.5');
      dot.setAttribute('class', 'orch-node-link-dot');
      const linkTitle = document.createElementNS(SVGNS, 'title');
      linkTitle.textContent = '按住拖到另一个节点，新增连接';
      link.appendChild(linkHit); link.appendChild(dot); link.appendChild(linkTitle);
      link.addEventListener('mousedown', (e) => startOrchLinkDrag(e, instId, g));
      g.appendChild(link);
    }

    // Live Preview 任意节点点击均可打开编辑器（改本链实例名称；Activity/CondSwitch 还可改路由条件）
    // 命中删除叉则删除，否则打开编辑器（用 target 判定，避免热区外误触或 stopPropagation 失效导致误开编辑器）
    g.style.cursor = 'pointer';
    g.addEventListener('click', (e) => {
      e.stopPropagation();
      e.preventDefault();
      if (_orchLinkJustDragged) return;
      if (e.target && e.target.closest && e.target.closest('.orch-node-link')) return;
      // 子链节点：叉 = 移除该子链实例；点击节点 = 打开编辑器（改名称、路由条件；编辑器里可新标签页打开该子链的编排页）
      if (subIds.includes(instId)) {
        if (e.target && e.target.closest && e.target.closest('.orch-node-del')) {
          if (removeOrchSubChain(instId) && typeof renderOrchPreview === 'function') renderOrchPreview();
          return;
        }
        orchOpenNodeSwitchEditor(instId);
        return;
      }
      if (e.target && e.target.closest && e.target.closest('.orch-node-del')) {
        removeOrchNodeInstanceConfirm(instId);
        return;
      }
      selectOrchParamBlock(instId);
      orchOpenNodeSwitchEditor(instId);
    });
  });
}

// Live Preview 连线上的 relationType 标签可点击：选中左侧 Connections 中该 from → to 的全部记录，
// 便于直接编辑（相同节点间多条连接在图上合并为一条线，因此可能同时选中多行）。只读预览不绑定。
function enhanceOrchPreviewEdges(container) {
  if (!container || document.body.classList.contains('orch-viewonly')) return;
  const edgeMap = window._orchEdgeMap || {};
  container.querySelectorAll('g.edgeLabel').forEach(lbl => {
    const inner = lbl.querySelector('[data-id]');
    const dataId = inner ? inner.getAttribute('data-id') : '';
    const edge = edgeMap[String(dataId || '').replace(/_\d+$/, '')];
    if (!edge) return;
    lbl.classList.add('orch-edge-label');
    lbl.addEventListener('click', (e) => {
      e.stopPropagation();
      e.preventDefault();
      selectOrchConnRows(edge.from_id, edge.to_id);
    });
  });
}

// 高亮并定位 Connections 中指定 from → to 的记录，聚焦第一条的连接类型输入框
function selectOrchConnRows(fromId, toId) {
  const rows = Array.from(document.querySelectorAll('#orch-conn-container .orch-conn-row'));
  rows.forEach(r => r.classList.remove('orch-conn-row-active'));
  const hits = rows.filter(r => r.querySelector('[data-role="orch-from"]').value === fromId
    && r.querySelector('[data-role="orch-to"]').value === toId);
  if (!hits.length) return;
  hits.forEach(r => r.classList.add('orch-conn-row-active'));
  hits[0].scrollIntoView({ block: 'center', behavior: 'smooth' });
  const typeInput = hits[0].querySelector('[data-role="orch-type"]');
  if (typeInput) typeInput.focus({ preventScroll: true });
}

// Live Preview 拖拽连线：拖拽结束后的那次 click 不应打开节点编辑器
let _orchLinkJustDragged = false;

// 从节点连线把手按下开始拖拽：SVG 上画一条跟随鼠标的虚线箭头，松开在另一节点上即新增连接。
// 坐标统一用 svg.getScreenCTM() 换算，预览区缩放（transform: scale）后也能对准。
function startOrchLinkDrag(e, fromId, fromG) {
  if (e.button !== 0) return;
  e.preventDefault();
  e.stopPropagation();
  const svg = fromG.ownerSVGElement;
  if (!svg) return;
  const SVGNS = 'http://www.w3.org/2000/svg';
  const toSvgPoint = (clientX, clientY) => {
    const m = svg.getScreenCTM();
    if (!m) return null;
    const pt = svg.createSVGPoint();
    pt.x = clientX; pt.y = clientY;
    return pt.matrixTransform(m.inverse());
  };
  const r = fromG.getBoundingClientRect();
  const start = toSvgPoint(r.left + r.width / 2, r.bottom);
  if (!start) return;
  if (!svg.querySelector('#orch-link-arrow')) {
    let defs = svg.querySelector(':scope > defs');
    if (!defs) { defs = document.createElementNS(SVGNS, 'defs'); svg.insertBefore(defs, svg.firstChild); }
    const marker = document.createElementNS(SVGNS, 'marker');
    marker.setAttribute('id', 'orch-link-arrow');
    marker.setAttribute('viewBox', '0 0 10 10');
    marker.setAttribute('refX', '9'); marker.setAttribute('refY', '5');
    marker.setAttribute('markerWidth', '7'); marker.setAttribute('markerHeight', '7');
    marker.setAttribute('orient', 'auto-start-reverse');
    const path = document.createElementNS(SVGNS, 'path');
    path.setAttribute('d', 'M0,0 L10,5 L0,10 z');
    path.setAttribute('class', 'orch-link-arrow-head');
    marker.appendChild(path);
    defs.appendChild(marker);
  }
  const line = document.createElementNS(SVGNS, 'line');
  line.setAttribute('class', 'orch-link-temp');
  line.setAttribute('x1', start.x); line.setAttribute('y1', start.y);
  line.setAttribute('x2', start.x); line.setAttribute('y2', start.y);
  line.setAttribute('marker-end', 'url(#orch-link-arrow)');
  svg.appendChild(line);
  document.body.classList.add('orch-linking');

  const findTarget = (ev) => {
    const el = document.elementFromPoint(ev.clientX, ev.clientY);
    const g = el && el.closest ? el.closest('g.node[data-orch-inst]') : null;
    return g && svg.contains(g) && g !== fromG ? g : null;
  };
  let hoverG = null;
  const onMove = (ev) => {
    const p = toSvgPoint(ev.clientX, ev.clientY);
    if (p) { line.setAttribute('x2', p.x); line.setAttribute('y2', p.y); }
    const t = findTarget(ev);
    if (hoverG !== t) {
      if (hoverG) hoverG.classList.remove('orch-link-target');
      hoverG = t;
      if (hoverG) hoverG.classList.add('orch-link-target');
    }
  };
  const onUp = (ev) => {
    document.removeEventListener('mousemove', onMove);
    document.removeEventListener('mouseup', onUp);
    line.remove();
    if (hoverG) hoverG.classList.remove('orch-link-target');
    document.body.classList.remove('orch-linking');
    _orchLinkJustDragged = true;
    setTimeout(() => { _orchLinkJustDragged = false; }, 0);
    const t = findTarget(ev);
    if (t) addOrchConnFromPreview(fromId, t.getAttribute('data-orch-inst'));
  };
  document.addEventListener('mousemove', onMove);
  document.addEventListener('mouseup', onUp);
}

// 图上拖拽连线落点后新增连接（忽略自连）。相同 from → to 允许多条（不同连接类型）：
// Connections 中逐条记录，Live Preview 合并为一条线、类型用逗号分隔展示。
// 首条连接类型默认 Success；已存在同 from → to 时，新行类型留空并聚焦类型输入框，提示填写不同的连接类型。
function addOrchConnFromPreview(fromId, toId) {
  if (!fromId || !toId || fromId === toId) return;
  const existCount = collectOrchConnections().filter(c => c.from_id === fromId && c.to_id === toId).length;
  // 首条连接默认 Success；已存在同 from → to 时留空，避免与已有连接类型重复，由用户填写不同类型
  addOrchConnRow(fromId, toId, existCount > 0 ? '' : 'Success');
  const newRow = document.getElementById('orch-conn-row-' + orchConnSeq);
  sortOrchConnRows();
  onOrchChange();
  if (existCount > 0) {
    const typeInput = newRow ? newRow.querySelector('[data-role="orch-type"]') : null;
    if (typeInput) {
      typeInput.scrollIntoView({ block: 'center', behavior: 'smooth' });
      typeInput.focus();
    }
    showToast('已新增第 ' + (existCount + 1) + ' 条相同节点间的连接，请在 Connections 中填写连接类型', 'success');
  }
}

function renderOrchDslPreview() {
  const container = document.getElementById('orch-dsl-preview');
  if (!container) return; // 当前页面（如 index.html）无 DSL Preview 区域，直接跳过
  if (!container.classList.contains('show')) return;
  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();
  if (nodeIds.length === 0 && subIds.length === 0) {
    container.textContent = '// 请先选择节点/子链';
    return;
  }
  container.textContent = orchBuildDslPreview();
}

function toggleOrchDsl() {
  const container = document.getElementById('orch-dsl-preview');
  if (!container) return; // 当前页面无 DSL Preview 区域，直接跳过
  const btn = document.getElementById('orch-dsl-toggle');
  container.classList.toggle('show');
  if (btn) btn.textContent = container.classList.contains('show') ? '收起' : '展开';
  if (container.classList.contains('show')) renderOrchDslPreview();
}

// 复制整个 DSL 的 JSON 内容到剪贴板
function copyOrchDsl() {
  const container = document.getElementById('orch-dsl-preview');
  if (!container) return; // 当前页面无 DSL Preview 区域，直接跳过
  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();
  if (nodeIds.length === 0 && subIds.length === 0) {
    if (window.showToast) showToast('请先选择节点/子链', 'error');
    return;
  }
  const text = orchBuildDslPreview();
  copyToClipboard(text, true);
  const btn = document.getElementById('orch-dsl-copy');
  if (btn) { btn.textContent = '已复制'; setTimeout(() => btn.textContent = '复制', 1200); }
}

// Generate & save root chain
// 编排目标：'root' = Root Chain，'sub' = Sub Chain
window._orchTarget = 'root';

// 切换编排目标（Root Chain / Sub Chain）。
// sub 目标下不显示 Sub Chains 选择区（子链嵌套子链引用在连接中已是可选目标，仍可引用）。
function setOrchTarget(type) {
  window._orchTarget = type;
  // root/sub 模式内容区分：切换 root-only / sub-only 区块显隐
  const showRoot = (type === 'root');
  // 注意：默认 CSS 把 .root-only/.sub-only 设为 display:none，这里显式设为 'block'
  // 才能真正覆盖（style.display='' 只会回退到 CSS 的 none，导致无法显示）。
  document.querySelectorAll('.root-only').forEach(el => {
    el.style.display = showRoot ? 'block' : 'none';
  });
  document.querySelectorAll('.sub-only').forEach(el => {
    el.style.display = showRoot ? 'none' : 'block';
  });
  // 标题与区分提示
  const titleEl = document.getElementById('orch-target-title');
  const hintEl = document.getElementById('orch-target-hint');
  const rrTitle = document.getElementById('orch-rr-title');
  if (rrTitle) rrTitle.textContent = showRoot ? '根节点返回值定义' : '子链返回值定义';
  if (titleEl) titleEl.textContent = showRoot ? '编排 Root Chain' : '编排 Sub Chain';
  if (hintEl) hintEl.textContent = showRoot
    ? 'Root Chain 为流程入口，可设置全局唯一的 Chain Key 供外部按业务键调用。'
    : 'Sub Chain 为可复用的子流程，供其他 Root/Sub Chain 在连接中引用，无 Chain Key。';
  const btn = document.getElementById('orch-generate-btn');
  const isEdit = btn.dataset.edit === '1';
  if (type === 'sub') {
    btn.textContent = isEdit ? '💾 更新 Sub Chain' : '🔧 生成 Sub Chain';
    document.getElementById('orch-sub-section').style.display = ''; // 子链也可再引用其他子链
  } else {
    btn.textContent = isEdit ? '💾 更新 Root Chain' : '🔧 生成 Root Chain';
  }
}

// 根据链 ID 前缀动态更新「编排目标」标题与编排目标：
//   F 开头 -> 编排 Sub Chain（隐藏 Root Chain 选项）
//   R 开头 -> 编排 Root Chain
//   （空或其他前缀 -> 保持调用方已设定的 target，标题回退为「编排目标」）
function updateOrchTargetByChainId() {
  const titleEl = document.getElementById('orch-target-title');
  if (!titleEl) return;
  const chainId = (document.getElementById('orch-chain-id').value || '').trim();
  if (chainId.startsWith('F')) {
    titleEl.textContent = '编排 Sub Chain';
    setOrchTarget('sub');
  } else if (chainId.startsWith('R')) {
    titleEl.textContent = '编排 Root Chain';
    setOrchTarget('root');
  } else {
    titleEl.textContent = '编排目标';
  }
}

// 编排新建子链：跳到独立编排页，目标为 Sub Chain（首页没有编排 Tab，不能在本页切换）。
function newSubChainViaOrch() {
  const p = encodeURIComponent(getProject() || '');
  location.href = '/orch?target=sub' + (p ? '&project=' + p : '');
}

// 将子链加载到编排页进行编辑（目标设为 Sub Chain）。
function orchSubChainByIndex(i) {
  const c = window._subChainsForEdit[i];
  if (!c) return;

  // 切换到 Orchestrate Tab
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
  document.querySelector('[data-tab="orchestrate"]').classList.add('active');
  document.getElementById('tab-orchestrate').classList.add('active');

  document.getElementById('orch-chain-id').value = c.chain_id || '';
  document.getElementById('orch-chain-key').value = c.chain_key || '';
  document.getElementById('orch-chain-name').value = c.name || '';
  document.getElementById('orch-chain-desc').value = c.description || '';

  // 从 dsl_json 中恢复 Debug Mode
  let debugMode = false;
  try { debugMode = !!((JSON.parse(c.dsl_json || '{}').ruleChain || {}).debugMode); } catch(e) {}
  document.getElementById('orch-debug-mode').checked = debugMode;

  // 编辑模式标记（保存走 PUT build）
  const btn = document.getElementById('orch-generate-btn');
  btn.dataset.edit = '1';
  setOrchTarget('sub');

  // 等待数据加载后恢复勾选与连接
  loadOrchData().then(() => {
    // 恢复已保存的节点参数配置（在勾选节点前 apply，renderOrchParamOverrides 会回显）
    applyOrchParamOverrides(c.node_param_overrides);
    // 恢复已保存的每节点 switch_condition 覆盖（仅本链生效）
    applyOrchSwitchOverrides(c.node_switch_overrides);
    // 恢复已保存的每节点名称覆盖（仅本链生效）
    applyOrchNameOverrides(c.node_name_overrides);
    // 恢复已保存的每节点参数配置区收起状态
    applyOrchCollapseOverrides(c.node_collapse_overrides);

    const nodeIds = (c.node_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
    restoreOrchNodeInstances(nodeIds);
    const subIds = (c.sub_chain_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
    setOrchSelectedSubIds(subIds);
    // 恢复子链返回值定义（存在子链 DSL 的 ruleChain.additionalInfo.root_responses）—— 必须在节点实例恢复之后
    applyOrchRootResponses(orchDslRootResponses(c.dsl_json));
    onOrchSelectionChange();

    document.querySelectorAll('#orch-conn-container .orch-conn-row').forEach(r => r.remove());
    const emptyEl = document.getElementById('orch-conn-empty');
    try {
      const conns = JSON.parse(c.connections_data || '[]');
      conns.forEach(conn => addOrchConnRow(conn.from_id, conn.to_id, conn.type));
      sortOrchConnRows();
    } catch(e) {}
    if (!document.querySelector('#orch-conn-container .orch-conn-row')) {
      if (emptyEl) emptyEl.style.display = '';
    }
    onOrchChange();
    showToast('已加载子链到编排页，修改后点击"更新 Sub Chain"保存', 'success');
  });
}

async function generateOrchRootChain() {
  const chainId = document.getElementById('orch-chain-id').value.trim();
  const isSub = window._orchTarget === 'sub';
  if (isSub && chainId) {
    // 子链编辑模式下 chain_id 必填，但创建时可空（后端自动生成）
  }
  if (!chainId && !isSub) {
    // root 的 Chain ID 由系统自动生成（R000001 格式），允许为空，放行
  }
  if (!chainId && isSub) {
    // 创建子链允许空 ID（自动生成），放行
  }

  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();
  const conns = collectOrchConnections();

  if (nodeIds.length === 0 && subIds.length === 0) { showToast('请至少选择一个 Node 或 Sub Chain', 'error'); return; }
  if (conns.length === 0) { showToast('请至少添加一条连接', 'error'); return; }

  // 自定义关系（非 Success/Failure）必须由起点节点的 switch_condition 路由，为空则运行时不命中
  const condProblems = refreshOrchConnWarns();
  if (condProblems.length > 0) {
    const firstRow = condProblems[0].row;
    if (firstRow && firstRow.scrollIntoView) firstRow.scrollIntoView({ block: 'center' });
    const lines = condProblems.slice(0, 10).map(p => '· ' + p.fromId + ' → 「' + p.type + '」').join('\n');
    const more = condProblems.length > 10 ? '\n…（共 ' + condProblems.length + ' 条）' : '';
    const goOn = window.confirm(
      '检测到 ' + condProblems.length + ' 条连接使用了自定义关系（非 Success/Failure），但起点节点的 switch_condition 为空：\n\n' +
      lines + more +
      '\n\n运行时这些分支不会命中。建议先点击行内的「⚠️ 缺路由条件」配置。\n\n仍要继续保存吗？');
    if (!goOn) return;
  }

  // 保存前统一检查「默认分支却仍配了路由条件」：只提示，不阻断保存
  const connConflicts = window._orchConnConflicts || [];
  if (connConflicts.length > 0) {
    const lines = connConflicts.slice(0, 5)
      .map(c => '· ' + c.fromId + ' → 「' + c.type + '」（路由条件来源：' + (c.from === 'override' ? '本链实例设置' : '节点定义') + '）')
      .join('；');
    const more = connConflicts.length > 5 ? ' …（共 ' + connConflicts.length + ' 条）' : '';
    showToast('⚠️ 有 ' + connConflicts.length + ' 条连接用的是 Success/Failure，但起点仍配置了 switch_condition：' +
              lines + more + '。这些分支可能不会命中、流程会走错，请清空对应路由条件。已照常保存。', 'error');
  }

  // 保存前统一检查「引用节点」是否仍生效：只提示，不阻断保存
  // （用户可能只是临时断开连线，更新后重新连上即可，所以不强制改写配置）
  const brokenRefs = collectOrchBrokenRefs();
  if (brokenRefs.length > 0) {
    const lines = brokenRefs.slice(0, 5).map(b => '· ' + b.nodeId + '.' + b.key + ' → ' + b.ref).join('；');
    const more = brokenRefs.length > 5 ? ' …（共 ' + brokenRefs.length + ' 个）' : '';
    showToast('⚠️ 有 ' + brokenRefs.length + ' 个参数的「引用节点」已失效（目标不在其上游）：' + lines + more +
              '。已照常保存，但运行时可能取不到值，请确认后重新选择来源。', 'error');
  }

  // 保存前检查「引用节点」所选上游节点是否为当前节点的「必经节点」（所有连接路径都经过）：
  // 仅提示，不阻断保存；不通过则标红 + 自动展开该节点的参数配置面板，便于定位。
  const nonCommonRefs = collectOrchNonCommonRefs();
  if (nonCommonRefs.length > 0) {
    const lines2 = nonCommonRefs.slice(0, 5).map(r => '· ' + r.nodeId + '.' + r.key + ' → ' + r.ref).join('；');
    const more2 = nonCommonRefs.length > 5 ? ' …（共 ' + nonCommonRefs.length + ' 个）' : '';
    showToast('⚠️ 有 ' + nonCommonRefs.length + ' 个参数的「引用节点」所选上游节点不在该节点的所有连接路径上（存在不经过它的分支）：' +
              lines2 + more2 + '。运行时可能走不到那条、取值不正确，请改用所有路径都经过的节点。已照常保存。', 'error');
    // 标红 + 自动展开出错的节点参数配置面板
    const container = document.getElementById('orch-param-list');
    const expandIds = new Set(nonCommonRefs.map(r => r.nodeId));
    expandIds.forEach(id => { if (_orchCollapseState) _orchCollapseState[id] = false; });
    if (container) {
      renderOrchParamOverrides(); // 重新渲染并强制展开出错节点
      const fields = container.querySelectorAll('.override-field');
      nonCommonRefs.forEach(r => {
        const field = [...fields].find(f => f.getAttribute('data-node') === r.nodeId && f.getAttribute('data-key') === r.key);
        if (field) markOrchParamNotCommon(field, r);
      });
      const first = nonCommonRefs[0];
      const f0 = [...fields].find(f => f.getAttribute('data-node') === first.nodeId && f.getAttribute('data-key') === first.key);
      if (f0 && f0.scrollIntoView) f0.scrollIntoView({ block: 'center' });
    }
  }

  const body = {
    project: getProject(),
    chain_id: chainId,
    chain_key: document.getElementById('orch-chain-key').value.trim(),
    chain_name: document.getElementById('orch-chain-name').value.trim(),
    description: document.getElementById('orch-chain-desc').value.trim(),
    node_ids: nodeIds,
    sub_chain_ids: subIds,
    connections: conns,
    debug_mode: document.getElementById('orch-debug-mode').checked,
    node_param_overrides: collectOrchParamOverrides(),
    node_switch_overrides: collectOrchSwitchOverrides(),
    node_name_overrides: collectOrchNameOverrides(),
    node_collapse_overrides: collectOrchCollapseOverrides(),
    // 返回值定义：根链为根节点返回值；子链为子链返回值（引用该子链的节点把它作为本节点返回值）
    root_responses: collectOrchRootResponses(),
  };

  const btn = document.getElementById('orch-generate-btn');
  const isEdit = btn.dataset.edit === '1';
  btn.disabled = true;
  btn.textContent = '生成中...';

  // 子链：后端按 chain_id 存在则更新、不存在则新增；有 chain_id 走 PUT，否则 POST 新建
  const endpoint = isSub
    ? (chainId ? '/api/sub-chains/' + encodeURIComponent(chainId) + '/build' : '/api/sub-chains/build')
    : '/api/root-chains';
  const method = isSub && chainId ? 'PUT' : 'POST';

  try {
    const doSave = confirmed => api(endpoint, { method, headers: {'Content-Type':'application/json'},
      body: JSON.stringify(confirmed ? Object.assign({}, body, { confirm_published: true }) : body) });
    // 子链被已发布根链引用时后端要求确认，确认后带 confirm_published 重新提交；取消则不保存
    const saved = isSub ? await apiSubChainSaveWithConfirm(doSave) : await doSave(false);
    if (!saved) return;
    const savedId = (saved && saved.chain_id) || chainId;
    showToast((isSub ? 'Sub Chain' : 'Root Chain') + ' 已保存: ' + (savedId || '(自动生成)'), 'success');
    if (isSub) {
      const subTab = document.getElementById('tab-sub-chains');
      if (subTab && subTab.classList.contains('active')) loadSubChains();
    } else {
      const rootTab = document.getElementById('tab-root-chains');
      if (rootTab && rootTab.classList.contains('active')) loadRootChains();
    }
    if (isSub) {
      // 子链保存后回填 chain_id 并进入编辑状态：再次点击就是更新，不会重复新建子链
      if (savedId) document.getElementById('orch-chain-id').value = savedId;
      btn.dataset.edit = '1';
    } else {
      // 保存成功后清除编辑标记，恢复默认文案
      btn.dataset.edit = '';
    }
    setOrchTarget(isSub ? 'sub' : 'root');
  } catch(e) {
    showToast('生成失败: ' + e.message, 'error');
  } finally {
    btn.disabled = false;
    const editNow = btn.dataset.edit === '1';
    btn.textContent = isSub ? (editNow ? '💾 更新 Sub Chain' : '🔧 生成 Sub Chain') : (isEdit ? '💾 更新 Root Chain' : '🔧 生成 Root Chain');
  }
}

// Load root chain into orchestrate for editing
function loadRootChainToOrch(c) {
  // 编排页已拆分为独立 orch.html，统一跳转到独立页编辑
  if (c && c.chain_id) { orchOpenInPageRoot(c.chain_id); return; }
  orchOpenInPageRoot('');
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
  const orchTab = document.querySelector('[data-tab="orchestrate"]');
  orchTab.classList.add('active');
  document.getElementById('tab-orchestrate').classList.add('active');

  document.getElementById('orch-chain-id').value = c.chain_id || '';
  document.getElementById('orch-chain-key').value = c.chain_key || '';
  document.getElementById('orch-chain-name').value = c.name || '';
  document.getElementById('orch-chain-desc').value = c.description || '';

  // 从 dsl_json 中恢复 Debug Mode
  let debugMode = false;
  try { debugMode = !!((JSON.parse(c.dsl_json || '{}').ruleChain || {}).debugMode); } catch(e) {}
  document.getElementById('orch-debug-mode').checked = debugMode;

  // 编辑模式：更新按钮文案（保存成功后 generateOrchRootChain 会恢复默认文案）
  const btn = document.getElementById('orch-generate-btn');
  btn.dataset.edit = '1';
  setOrchTarget('root');

  // Wait for data to load, then check the boxes
  loadOrchData().then(() => {
    // 先勾选并恢复画布节点实例与子链：根节点返回值定义的候选下拉依赖 _orchNodeInstances，
    // 必须在 applyOrchRootResponses 之前完成，否则节点/字段下拉无数据、无法回显已保存的选择。
    // Check nodes
    const nodeIds = (c.node_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
    document.querySelectorAll('#orch-node-list input[type="checkbox"]').forEach(cb => {
      cb.checked = nodeIds.includes(cb.value);
    });
    // Check sub-chains
    const subIds = (c.sub_chain_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
    setOrchSelectedSubIds(subIds);
    onOrchSelectionChange();

    // 恢复已保存的节点参数配置（在勾选节点后 apply，renderOrchParamOverrides 会回显）
    applyOrchParamOverrides(c.node_param_overrides);
    // 恢复已保存的每节点 switch_condition 覆盖（仅本链生效）
    applyOrchSwitchOverrides(c.node_switch_overrides);
    // 恢复已保存的每节点名称覆盖（仅本链生效）
    applyOrchNameOverrides(c.node_name_overrides);
    // 恢复已保存的每节点参数配置区收起状态
    applyOrchCollapseOverrides(c.node_collapse_overrides);
    // 恢复根节点返回值定义（Root Chain 专属）—— 必须在节点实例恢复之后
    applyOrchRootResponses(c.root_responses);

    // Load connections
    document.querySelectorAll('#orch-conn-container .orch-conn-row').forEach(r => r.remove());
    const emptyEl = document.getElementById('orch-conn-empty');
    try {
      const conns = JSON.parse(c.connections_data || '[]');
      conns.forEach(conn => addOrchConnRow(conn.from_id, conn.to_id, conn.type));
      sortOrchConnRows();
    } catch(e) {}
    if (!document.querySelector('#orch-conn-container .orch-conn-row')) {
      if (emptyEl) emptyEl.style.display = '';
    }
    onOrchChange();
    showToast('已加载根链到编排页，修改后点击"更新 Root Chain"保存', 'success');
  });
}

// ============================================================
// Utility
// ============================================================
// Mermaid 标签文本转义：Mermaid 会把 `#xxx;` 当作自身的实体语法解析，若用 esc() 生成的 HTML 实体（如 &#39;）
// 会被拆成 `&` + `'`，单引号显示成 &'。这里统一改用 Mermaid 实体写法：特殊字符（含 # 与 ;）都写成 #实体名;
function escMermaidText(s) {
  if (s === null || s === undefined) return '';
  const map = { '&': '#amp;', '<': '#lt;', '>': '#gt;', '"': '#quot;', "'": '#39;', '#': '#35;', ';': '#59;' };
  return String(s).replace(/[&<>"'#;]/g, c => map[c]);
}
function esc(s) { if (s === null || s === undefined) return ''; return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;'); }
// 安全取值：元素可能已被 UI 隐藏/移除，缺失时返回默认值
function safeVal(id, def) { const el = document.getElementById(id); return el ? el.value.trim() : (def || ''); }
function safeChecked(id) { const el = document.getElementById(id); return el ? el.checked : false; }
function trunc(s, n) { if (!s) return '-'; return s.length > n ? s.substring(0, n) + '...' : s; }
// 已发布到根链（生产快照引用）的 node / activity 禁止编辑与删除，避免影响线上调用。
// 以下两个辅助用于列表渲染：一个生成锁定角标，一个生成按钮禁用属性。
function publishedLockBadge(title) {
  return ' <span class="published-lock" title="' + esc(title || '已发布') + '">🔒</span>';
}

// ===== Nodes 列表「已发布引用」列 =====
// 展示该节点被多少条「当前已发布」的根链引用；悬停列出全部根链（ID / 名称 / 版本），
// 便于人工定位需要处理的根链。数据源为后端列表回填的 published_root_chains。
function nodePublishedRootChains(n) {
  return (n && Array.isArray(n.published_root_chains)) ? n.published_root_chains : [];
}
// 列单元格 JSON 属性（供悬停 pop 层读取，避免闭包串行化整个节点对象）
function publishedRefsJsonAttr(n) {
  try { return escAttr(JSON.stringify(nodePublishedRootChains(n))); } catch (e) { return '[]'; }
}
// 列单元格展示：有引用显示数量（可悬停），无引用显示淡灰 "-"
function publishedRefsCountHtml(n) {
  const list = nodePublishedRootChains(n);
  if (!list.length) return '<span style="color:var(--text-muted)">-</span>';
  return '<span class="pubref-num">' + list.length + '</span>';
}

// ===== Activities 列表「引用 Nodes」列 =====
// 与「已发布引用」是不同维度：这里是直接使用了该 activity 的 Node（影响面），
// 已发布则是最终生效的根链。修改 activity 前据此评估影响范围。
function activityRefNodes(a) {
  return (a && Array.isArray(a.ref_nodes)) ? a.ref_nodes : [];
}
function refNodesJsonAttr(a) {
  try { return escAttr(JSON.stringify(activityRefNodes(a))); } catch (e) { return '[]'; }
}
function refNodesCountHtml(a) {
  const list = activityRefNodes(a);
  if (!list.length) return '<span style="color:var(--text-muted)">-</span>';
  return '<span class="pubref-num">' + list.length + '</span>';
}
function publishedLockAttrs(published, title) {
  if (!published) return '';
  // 普通用户禁用；管理员仍可直接操作，但前端会二次确认，故只加提示 title 不禁用
  if (isAdmin()) return 'title="' + esc(title || '已发布到根链，管理员可直接编辑/删除（操作前会二次确认）') + '"';
  return 'disabled title="' + esc(title || '已发布到根链，禁止编辑/删除') + '"';
}
// 列表「操作」列图标按钮用的内联 SVG 图标（线条风格，颜色跟随按钮文字色），Activities / Nodes / Sub Chains / Root Chains 共用
const ACTION_ICON_PATHS = {
  edit:   '<path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/>',
  test:   '<polygon points="6 3 20 12 6 21 6 3"/>',
  code:   '<polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>',
  log:    '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M16 13H8"/><path d="M16 17H8"/><path d="M10 9H8"/>',
  delete: '<path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>',
  orch:    '<rect width="8" height="8" x="3" y="3" rx="2"/><path d="M7 11v4a2 2 0 0 0 2 2h4"/><rect width="8" height="8" x="13" y="13" rx="2"/>',
  publish: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" x2="12" y1="3" y2="15"/>',
  history: '<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/>',
  flow:    '<rect x="16" y="16" width="6" height="6" rx="1"/><rect x="2" y="16" width="6" height="6" rx="1"/><rect x="9" y="2" width="6" height="6" rx="1"/><path d="M5 16v-3a1 1 0 0 1 1-1h12a1 1 0 0 1 1 1v3"/><path d="M12 12V8"/>',
  copy:    '<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
};
function actionIconSvg(name) {
  return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' + (ACTION_ICON_PATHS[name] || '') + '</svg>';
}
// 图标按钮的 title / aria-label：悬停显示按钮文字；已发布锁定时沿用 publishedLockAttrs（含 disabled），提示前加上按钮文字
function actionIconAttrs(label, published, lockTitle) {
  const aria = 'aria-label="' + esc(label) + '"';
  if (published) return aria + ' ' + publishedLockAttrs(true, label + '：' + lockTitle);
  return aria + ' title="' + esc(label) + '"';
}

// 统一的已发布保护提示（后端也会拦截，此处用于前端即时反馈）
function showPublishedLockedToast(kind) {
  showToast((kind || '该记录') + '已发布到根链，禁止编辑/删除。请先发布新版本或改用新建副本。', 'error');
}
// 路由标记：节点配置了 switch_condition 即带路由功能（执行后按返回值路由到不同分支）。
// 后端列表已回填 has_switch_condition；此处保留本地解析作为兜底（旧数据 / 缓存对象）。
function nodeSwitchConditionText(n) {
  if (!n) return '';
  const t = n.type;
  if (t !== 'custom/CondSwitch' && t !== 'custom/Activity') return '';
  let cfg = n.configuration;
  try {
    if (typeof cfg === 'string') cfg = JSON.parse(cfg || '{}');
  } catch (e) { return ''; }
  const v = (cfg && cfg.node_config && cfg.node_config.switch_condition) || '';
  return String(v).trim();
}
// iconOnly=true 时只显示 🔀 图标（Nodes 列表用，省宽度），路由说明仍在悬停 title 里
function nodeRouteBadge(n, iconOnly) {
  if (!n) return '';
  const has = (typeof n.has_switch_condition === 'boolean')
    ? n.has_switch_condition
    : (nodeSwitchConditionText(n) !== '');
  if (!has) return '';
  const expr = nodeSwitchConditionText(n);
  const tip = expr ? ('已配置路由条件（switch_condition）：' + expr) : '已配置路由条件（switch_condition），执行后按返回值路由到不同分支';
  return ' <span class="route-badge" title="' + esc(tip) + '">' + (iconOnly ? '🔀' : '🔀路由') + '</span>';
}
// 编排 Live Preview（Mermaid 节点标签内）的路由标记。
// 与 nodeRouteBadge 判定逻辑一致，但 Mermaid 标签整体由双引号包裹且 class 对 SVG 内元素不生效，
// 故改用内联样式且 HTML 属性一律用单引号；无路由时返回空串。
function orchNodeRouteMark(n) {
  if (!n) return '';
  const has = (typeof n.has_switch_condition === 'boolean')
    ? n.has_switch_condition
    : (nodeSwitchConditionText(n) !== '');
  if (!has) return '';
  const expr = nodeSwitchConditionText(n);
  const tip = expr ? ('已配置路由条件（switch_condition）：' + expr) : '已配置路由条件（switch_condition），执行后按返回值路由到不同分支';
  // 仅用于 Mermaid 节点标签：不能写行内 style（Mermaid 会删掉含「style…:…#…;」整行的最后一个分号，破坏同行的 #39; 等实体），
  // 样式见 common.css .orch-route-mark；title 用 Mermaid 实体转义，避免单引号显示成 &'
  return ` <span class='orch-route-mark' title='${escMermaidText(tip)}'>🔀路由</span>`;
}
function formatDuration(ms) {
  if (ms === undefined || ms === null || ms < 0) return '-';
  if (ms < 1000) return ms + 'ms';
  const sec = ms / 1000;
  if (sec < 60) return sec.toFixed(2) + 's';
  const m = Math.floor(sec / 60);
  const s = Math.round(sec % 60);
  return m + 'm' + s + 's';
}
function prettyJson(v) {
  if (!v || v === 'null') return '{}';
  try {
    if (typeof v === 'string') return JSON.stringify(JSON.parse(v), null, 2);
    if (typeof v === 'object') return JSON.stringify(v, null, 2);
    return String(v);
  } catch (e) { return typeof v === 'string' ? v : '{}'; }
}

// ============================================================
// Init
// ============================================================
// 优先加载项目列表（即使 mermaid 等外部脚本失败也不影响主流程）
loadProjects().then(() => {
  // 仅主页（index）存在 nodes-table 时才加载节点列表；编排独立页(orch.html)无此 DOM，避免崩溃
  if (document.getElementById('nodes-table') && getProject()) loadNodeEnvOptions();
  if (document.getElementById('activity-env-select')) loadActivityEnvOptions();
});

// 初始化 Mermaid（外部脚本，失败不影响主流程）
try {
  if (typeof mermaid !== 'undefined' && mermaid.initialize) {
    mermaid.initialize({
      startOnLoad: true,
      theme: 'default',
      flowchart: { useMaxWidth: true, htmlLabels: true, curve: 'basis' },
      securityLevel: 'loose'
    });
  }
} catch (e) { /* mermaid 初始化失败忽略 */ }


// ============================================================
// Activity 参数数泡泡浮层
// ============================================================
(function() {
  let pop = document.getElementById('arg-popover');
  if (!pop) {
    pop = document.createElement('div');
    pop.id = 'arg-popover';
    pop.className = 'arg-popover';
    document.body.appendChild(pop);
  }
  let hideTimer = null;

  function buildContent(args, total) {
    if (!args || args.length === 0) {
      return '<div class="ap-none">无输入参数</div>';
    }
    let html = '<div class="ap-title">输入参数（' + total + '）</div>';
    html += args.map(p => {
      const key = escHtml(p.key);
      const label = p.label ? escHtml(p.label) : '';
      return '<div class="ap-item"><span class="ap-key">' + key + '</span>' + (label ? '<span class="ap-label">' + label + '</span>' : '') + '</div>';
    }).join('');
    return html;
  }

  function showPop(cell) {
    if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
    let args = [];
    try { args = JSON.parse(cell.getAttribute('data-args') || '[]'); } catch(e) {}
    const total = cell.getAttribute('data-empty') ? 0 : args.length;
    pop.innerHTML = buildContent(args, total);
    pop.classList.add('show');
    const r = cell.getBoundingClientRect();
    const pw = pop.offsetWidth, ph = pop.offsetHeight;
    let left = r.left + r.width / 2 - pw / 2;
    let top = r.top - ph - 8;
    if (top < 8) top = r.bottom + 8;                 // 上方放不下则放下方
    left = Math.max(8, Math.min(left, window.innerWidth - pw - 8));
    pop.style.left = left + 'px';
    pop.style.top = top + 'px';
  }

  function hidePop() {
    hideTimer = setTimeout(() => pop.classList.remove('show'), 80);
  }

  document.addEventListener('mouseover', e => {
    const cell = e.target.closest && e.target.closest('td.arg-count');
    if (cell) showPop(cell);
  });
  document.addEventListener('mouseout', e => {
    const cell = e.target.closest && e.target.closest('td.arg-count');
    if (cell) hidePop();
  });
  window.addEventListener('scroll', () => pop.classList.remove('show'), true);
})();

// ============================================================
// 列表「数量 + 悬停明细」通用浮层
// 复用 .arg-popover 样式。各类「计数列」（已发布引用、引用 Nodes）共用同一套
// 定位/显示/隐藏逻辑，仅内容渲染函数不同，避免重复实现。
// opts: { popId, selector, dataAttr, buildContent }
// ============================================================
function attachCountPopover(opts) {
  let pop = document.getElementById(opts.popId);
  if (!pop) {
    pop = document.createElement('div');
    pop.id = opts.popId;
    pop.className = 'arg-popover';
    document.body.appendChild(pop);
  }
  let hideTimer = null;

  function showPop(cell) {
    if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
    let items = [];
    try { items = JSON.parse(cell.getAttribute(opts.dataAttr) || '[]'); } catch(e) { items = []; }
    if (cell.getAttribute('data-empty')) items = [];
    pop.innerHTML = opts.buildContent(items);
    pop.classList.add('show');
    const r = cell.getBoundingClientRect();
    const pw = pop.offsetWidth, ph = pop.offsetHeight;
    let left = r.left + r.width / 2 - pw / 2;
    let top = r.top - ph - 8;
    if (top < 8) top = r.bottom + 8;                 // 上方放不下则显示到下方
    left = Math.max(8, Math.min(left, window.innerWidth - pw - 8));
    pop.style.left = left + 'px';
    pop.style.top = top + 'px';
  }

  function hidePop() {
    hideTimer = setTimeout(() => pop.classList.remove('show'), 80);
  }

  document.addEventListener('mouseover', e => {
    const cell = e.target.closest && e.target.closest(opts.selector);
    if (cell) showPop(cell);
  });
  document.addEventListener('mouseout', e => {
    const cell = e.target.closest && e.target.closest(opts.selector);
    if (cell) hidePop();
  });
  window.addEventListener('scroll', () => pop.classList.remove('show'), true);
}

// Nodes 列表「已发布引用」列：悬停列出引用该节点的全部已发布根链
attachCountPopover({
  popId: 'pubref-popover',
  selector: 'td.pubref-count',
  dataAttr: 'data-chains',
  buildContent: function(chains) {
    if (!chains || !chains.length) {
      return '<div class="ap-none">未被任何已发布根链引用</div>';
    }
    let html = '<div class="ap-title">已发布引用根链（' + chains.length + '）</div>';
    html += chains.map(function(c) {
      const cid = escHtml(c.chain_id || '-');
      const name = c.name ? escHtml(c.name) : '';
      const ver = (c.version === 0 || c.version) ? ('v' + c.version) : '';
      return '<div class="ap-item">' +
        '<span class="ap-key">' + cid + '</span>' +
        (name ? '<span class="ap-label">' + name + '</span>' : '') +
        (ver ? '<span class="ap-label" style="color:#9ca3af">' + ver + '</span>' : '') +
        '</div>';
    }).join('');
    return html;
  }
});

// Activities 列表「引用 Nodes」列：悬停列出使用该 activity 的全部节点，
// 并标注节点是否已发布（🔒）以区分线上还是草稿影响面。
attachCountPopover({
  popId: 'noderef-popover',
  selector: 'td.noderef-count',
  dataAttr: 'data-nodes',
  buildContent: function(nodes) {
    if (!nodes || !nodes.length) {
      return '<div class="ap-none">暂无 Node 引用该 activity</div>';
    }
    const live = nodes.filter(function(n) { return n.published; }).length;
    let html = '<div class="ap-title">引用该 activity 的 Node（' + nodes.length + '）</div>';
    if (live > 0) {
      html += '<div class="ap-none" style="color:#ef4444">其中 ' + live + ' 个已发布到根链，修改将影响线上</div>';
    }
    html += nodes.map(function(n) {
      const nid = escHtml(n.node_id || '-');
      const name = n.name ? escHtml(n.name) : '';
      return '<div class="ap-item">' +
        '<span class="ap-key">' + nid + '</span>' +
        (name ? '<span class="ap-label">' + name + '</span>' : '') +
        (n.published ? '<span class="ap-label" style="color:#ef4444">🔒已发布</span>'
                     : '<span class="ap-label" style="color:#9ca3af">草稿</span>') +
        '</div>';
    }).join('');
    return html;
  }
});

// ===== 参数值绑定相关全局函数（供 renderActivityItemParams / onArgSourceChange / onRefChange 跨作用域复用） =====

// 根据已存引用路径 b.ref 反推联动控件初值：{ refId, refType, refField, raw }
function argParseRefPath(ref) {
  const m = /^\{\{([^}]+)\}\}$/.exec((ref || '').trim());
  if (!m) return { raw: ref || '' };
  let inner = m[1];
  // 兼容 steps. 前缀（引用前序统一生成 {{steps.id...}}）
  if (inner.startsWith('steps.')) inner = inner.slice('steps.'.length);
  const dot = inner.indexOf('.');
  if (dot < 0) return { refId: inner, refType: 'responses', refField: '' };
  const refId = inner.slice(0, dot);
  const rest = inner.slice(dot + 1);
  if (rest === 'responses') return { refId: refId, refType: 'responses', refField: '' };
  if (rest === 'arguments') return { refId: refId, refType: 'arguments', refField: '' };
  if (rest.startsWith('responses.')) return { refId: refId, refType: 'responses_field', refField: rest.slice('responses.'.length) };
  if (rest.startsWith('arguments.')) return { refId: refId, refType: 'arguments', refField: rest.slice('arguments.'.length) };
  return { refId: refId, refType: 'responses', refField: rest };
}
// 根据联动控件拼出最终引用路径
function argBuildRefPath(refId, refType, refField) {
  if (!refId) return '';
  const field = (refField || '').trim();
  let inner;
  if (refType === 'responses') inner = refId + '.responses';
  // 返回值.字段：field 为空（未选具体字段，或所选 return_value 的 key 为空=返回全部）一律回退为整体 {{id.responses}}
  else if (refType === 'responses_field') inner = refId + '.responses' + (field ? '.' + field : '');
  else if (refType === 'arguments') inner = refId + '.arguments.' + (field || 'key');
  else inner = refId;
  // 所有 Activity 都在 steps 下，引用前序统一加 steps. 前缀
  return '{{steps.' + inner + '}}';
}
// 生成「引用前序」第三级字段选择下拉
function argRenderRefFieldSlot(prevs, prevId, type, selectedField) {
  if (type === 'responses') return '';
  const prev = (prevs || []).find(p => p.id === prevId);
  if (!prev) {
    return '<span style="flex:1;color:var(--text-muted);font-size:.78rem">请先选择前序 Activity</span>';
  }
  if (type === 'responses_field') {
    const rvs = prev.returnValues || [];
    if (rvs.length === 0) {
      return '<span style="flex:1;color:var(--text-muted);font-size:.78rem">该 Activity 无 ReturnValues 配置</span>';
    }
    const opts = rvs.map(rv => {
      const rvName = rv.name || '';
      const rvKey = rv.key || '';
      // 引用字段名统一用 name（与 outputRefSnippetHTML 一致）：name 是 Activity 返回值 JSON 里的实际字段名。
      // 不再因 key 为空而把选项 value 置空——否则选中后字段输入框为空，拼路径时回退为整体
      // {{id.responses}}，导致选了「返回值.字段」却保存不上（表现为未提交）。
      const val = rvName;
      // 选项文本括号里展示经 Activity 修改后的中文名（label/name），而非数据原始返回的 key
      const dispName = rv.label || rv.name || '';
      const label = rvName + (dispName ? ' (' + dispName + ')' : '');
      return '<option value="' + escAttr(val) + '"' + (val === selectedField ? ' selected' : '') + '>' + escHtml(label) + '</option>';
    }).join('');
    return '<select class="arg-refield" onchange="onRefChange(this)" style="flex:1 1 110px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
        '<option value="">— 选择返回值字段 —</option>' + opts + '</select>';
  }
  const keys = (prev.params || []).map(pp => pp.key || '').filter(Boolean);
  if (keys.length === 0) {
    return '<span style="flex:1;color:var(--text-muted);font-size:.78rem">该 Activity 无参数定义</span>';
  }
  const opts = keys.map(k => '<option value="' + escAttr(k) + '"' + (k === selectedField ? ' selected' : '') + '>' + escHtml(k) + '</option>').join('');
  return '<select class="arg-refield" onchange="onRefChange(this)" style="flex:1 1 110px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
      '<option value="">— 选择参数 key —</option>' + opts + '</select>';
}
// 生成「引用前序」第二级取值类型下拉（全局函数，供 argRenderArgInput / onRefChange 跨作用域调用）：
//   - 返回值整体：始终可选
//   - 返回值.字段：仅当前序 Activity 配置了 ReturnValues（后端返回值）时可选，否则隐藏
//   - 参数值.字段：仅当前序 Activity 有参数列表（arguments）时可选，否则隐藏
function argRenderRefTypeSel(prev, parsed) {
  const hasReturnValues = !!(prev && Array.isArray(prev.returnValues) && prev.returnValues.length > 0);
  const hasParams = !!(prev && Array.isArray(prev.params) && prev.params.length > 0);
  // 若已选类型因前序无对应配置而不再可选，回退到「返回值整体」
  let effType = parsed.refType || 'responses_field';
  if (effType === 'responses_field' && !hasReturnValues) effType = 'responses';
  if (effType === 'arguments' && !hasParams) effType = 'responses';
  let html = '<select class="arg-reftype" onchange="onRefChange(this)" style="flex:0 0 auto;width:auto;max-width:160px;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">';
  html += '<option value="responses"' + (effType === 'responses' ? ' selected' : '') + '>返回值整体</option>';
  if (hasReturnValues) {
    html += '<option value="responses_field"' + (effType === 'responses_field' ? ' selected' : '') + '>返回值.字段</option>';
  }
  if (hasParams) {
    html += '<option value="arguments"' + (effType === 'arguments' ? ' selected' : '') + '>参数值.字段</option>';
  }
  html += '</select>';
  return html;
}
// 单个参数绑定行的「输入控件」部分 HTML（返回 .arg-input-wrap 内部内容，供来源切换时局部重建）
function argRenderArgInput(key, bind, argCtx) {
  const prevs = argCtx.prevs || [];
  const nodeParams = argCtx.nodeParams || [];
  const nodeRefOptions = argCtx.nodeRefOptions || '';
  const src = bind.source === 'ref_act' ? 'ref_act' : (bind.source === 'ref_node' ? 'ref_node' : 'value');
  const defVal = (typeof bind._defVal !== 'undefined') ? bind._defVal : (bind.value !== undefined ? bind.value : '');
  const valVal = src === 'value' ? (bind.value !== undefined ? bind.value : defVal) : (bind.ref || '');

  if (src === 'value') {
    // 手工配置：在输入框后追加「类型」选择框（string/int64/float64/formula）。
    // type 会写入绑定结构，后端按类型决定是否做类型计算/公式求值。
    const typ = bind.type || 'string';
    const ph = typ === 'formula'
      ? '公式，如 {{arguments.a}} + {{arguments.b}}'
      : '固定配置（留空则用活动默认值）';
    const typeSel = '<select class="arg-type" onchange="syncActivityConfig()" title="值类型" style="flex:0 0 84px;width:84px;padding:3px 4px;border:1px solid var(--border);border-radius:6px;font-size:.72rem">' +
      ARG_TYPE_OPTIONS.map(t =>
        '<option value="' + t + '"' + (t === typ ? ' selected' : '') + '>' + t + '</option>'
      ).join('') + '</select>';
    return '<input class="arg-val" value="' + escAttr(valVal) + '" placeholder="' + escAttr(ph) + '" oninput="syncActivityConfig()" style="flex:1;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">' + typeSel;
  } else if (src === 'ref_node') {
    const parsed = argParseRefPath(bind.ref);
    if (nodeParams.length === 0) {
      return '<span style="flex:1;color:var(--text-muted);font-size:.78rem">本节点未定义参数，无法引用</span>';
    }
    const selVal = Object.prototype.hasOwnProperty.call(parsed, 'raw') ? parsed.raw : (bind.ref || '');
    let html = '<select class="arg-ref-node" onchange="onRefChange(this)" style="flex:1;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
        '<option value="">— 选择节点参数 —</option>' + nodeRefOptions +
      '</select>';
    html += (Object.prototype.hasOwnProperty.call(parsed, 'raw') && parsed.raw)
      ? '<input class="arg-ref-final" type="hidden" value="' + escAttr(parsed.raw) + '">'
      : '<input class="arg-ref-final" type="hidden" value="' + escAttr(selVal) + '">';
    return html;
  }
  const parsed = argParseRefPath(bind.ref);
  if (prevs.length === 0) {
    return '<span style="flex:1;color:var(--text-muted);font-size:.78rem">前面没有可引用的 Activity（并行阶段的兄弟不可互相引用）</span>';
  }
  const prevSel = '<select class="arg-ref" onchange="onRefChange(this)" style="flex:1 1 120px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">' +
      '<option value="">— 前序 Activity —</option>' +
      prevs.map(p => '<option value="' + escAttr(p.id) + '"' + (p.id === parsed.refId ? ' selected' : '') + '>' + escHtml(p.act_name + ' #' + p.id) + '</option>').join('') +
    '</select>';
  const curPrev = parsed.refId ? (prevs.find(p => p.id === parsed.refId) || null) : null;
  const typeSel = argRenderRefTypeSel(curPrev, parsed);
  const fieldSlot = '<span class="arg-ref-field-slot" data-prev="' + escAttr(parsed.refId || '') + '" data-type="' + escAttr(parsed.refType || 'responses_field') + '">' +
      argRenderRefFieldSlot(prevs, parsed.refId || '', parsed.refType || 'responses_field', parsed.refField || '') + '</span>';
  const finalHidden = '<input class="arg-ref-final" type="hidden" value="' + escAttr(bind.ref || '') + '">';
  return prevSel + typeSel + fieldSlot + finalHidden;
}
// 单个参数绑定行的完整 HTML（全局可复用，renderActivityItemParams 整卡渲染时调用）
// argCtx: { prevs, nodeParams, nodeRefOptions }
function argRenderArgBindRow(row, key, bind, argCtx) {
  const prevs = argCtx.prevs || [];
  const src = bind.source === 'ref_act' ? 'ref_act' : (bind.source === 'ref_node' ? 'ref_node' : 'value');
  const hasPrev = prevs.length > 0;
  const effSrc = (src === 'ref_act' && !hasPrev) ? 'value' : src;
  const srcOptions =
    '<option value="value"' + (effSrc === 'value' ? ' selected' : '') + '>固定配置</option>' +
    (hasPrev ? '<option value="ref_act"' + (effSrc === 'ref_act' ? ' selected' : '') + '>引用前序</option>' : '') +
    '<option value="ref_node"' + (effSrc === 'ref_node' ? ' selected' : '') + '>引用节点</option>';

  return '<div class="arg-bind-row" data-arg-key="' + escAttr(key) + '" style="display:flex;align-items:center;gap:6px;padding:5px 0;border-bottom:1px dotted #eef0f3;min-width:0">' +
      '<span style="font-family:monospace;font-weight:600;font-size:.78rem;flex:0 0 130px;min-width:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis" title="' + escAttr(key) + '">' + escHtml(key) + '</span>' +
      '<select class="arg-src" onchange="onArgSourceChange(this)" style="flex:0 0 auto;width:auto;max-width:140px;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem">' + srcOptions + '</select>' +
      '<span class="arg-input-wrap" style="display:flex;flex:1;gap:6px;min-width:0;overflow:hidden">' + argRenderArgInput(key, bind, argCtx) + '</span>' +
    '</div>';
}
// 切换前序 activity 或取值类型时，重建第三级字段下拉内容（全局函数，供 onRefChange 跨作用域调用）
function argGetPrevsForWrap(wrap) {
  const row = wrap.closest('.act-item');
  return row ? prevActivitiesBefore(row) : [];
}
function rebuildRefFieldSlot(wrap) {
  const slot = wrap.querySelector('.arg-ref-field-slot');
  if (!slot) return;
  const prevSel = wrap.querySelector('.arg-ref');
  const typeSel = wrap.querySelector('.arg-reftype');
  const fieldSel = wrap.querySelector('.arg-refield');
  const prevId = prevSel ? prevSel.value : '';
  const type = typeSel ? typeSel.value : 'responses_field';
  const selected = fieldSel ? fieldSel.value : '';
  const row = wrap.closest('.act-item');
  const prevs = row ? prevActivitiesBefore(row) : [];
  slot.setAttribute('data-prev', prevId);
  slot.setAttribute('data-type', type);
  slot.innerHTML = argRenderRefFieldSlot(prevs, prevId, type, selected);
}

// ============================================================
// 登录 / 登出 / 当前用户
// ============================================================
let currentUser = null; // { username, nickname, role, projects }

// 是否为管理员：由 onLoginSuccess 在 body 上标记 .is-admin
function isAdmin() {
  return document.body.classList.contains('is-admin');
}

async function bootstrapAuth() {
  try {
    const res = await fetch('/api/me');
    if (res.ok) {
      currentUser = await res.json();
      onLoginSuccess(currentUser);
      printVersion();
      return;
    }
  } catch (_) {}
  // 未登录：显示遮罩；仍打印版本（供发布后直观确认）
  document.getElementById('login-overlay').classList.remove('hidden');
  printVersion();
}

// printVersion 在浏览器控制台打印当前构建版本（git commit id），便于直观确认版本是否更新成功。
async function printVersion() {
  try {
    const res = await fetch('/api/version');
    if (!res.ok) return;
    const d = await res.json();
    const id = d && d.git_commit_id;
    if (id) {
      console.log('%c[app-version] git_commit_id: ' + id, 'color:#0e7490;font-weight:bold');
    }
  } catch (_) {}
}

async function onLoginSuccess(u) {
  currentUser = u;
  document.getElementById('login-overlay').classList.add('hidden');
  document.getElementById('login-err').textContent = '';
  const cu = document.getElementById('current-user');
  cu.style.display = 'inline';
  cu.textContent = (u.nickname || u.username) + (u.role === 'admin' ? ' (管理员)' : '');
  if (u.role === 'admin') document.body.classList.add('is-admin');
  else document.body.classList.remove('is-admin');
  // 登录后加载主界面数据
  await loadProjects();
  // 支持从其它页面（如 orch.html）通过 ?tab= 跳转回指定 tab
  applyInitialTab();
}

// 切换到指定 tab（仅 index 主页使用，依赖各 tab-content DOM）
function switchTab(tab) {
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
  const btn = document.querySelector('.tab-btn[data-tab="' + tab + '"]');
  if (!btn) return;
  btn.classList.add('active');
  const content = document.getElementById('tab-' + tab);
  if (content) content.classList.add('active');
  if (tab === 'nodes') loadNodeEnvOptions();
  else if (tab === 'activities') loadActivityEnvOptions();
  else if (tab === 'sub-chains') loadSubChains();
  else if (tab === 'root-chains') loadRootChains();
  else if (tab === 'logs') openAllLogsTab();
  else if (tab === 'statistics') loadStatistics();
  else if (tab === 'orchestrate') loadOrchData();
  else if (tab === 'execute') {
    refreshConnSuggestions();
    if (!document.querySelector('#exec-connections-container .conn-row')) {
      addExecConnRow('', '', '');
    }
  }
}

// 启动时根据 URL ?tab= 参数自动切换主页 tab
function applyInitialTab() {
  const tab = new URLSearchParams(location.search).get('tab');
  if (!tab) return;
  const valid = ['activities', 'nodes', 'sub-chains', 'root-chains', 'logs', 'statistics', 'orchestrate'];
  if (valid.includes(tab)) switchTab(tab);
}

async function doLogin() {
  const username = document.getElementById('login-username').value.trim();
  const password = document.getElementById('login-password').value;
  const errEl = document.getElementById('login-err');
  errEl.textContent = '';
  if (!username || !password) { errEl.textContent = '请输入用户名和密码'; return; }
  try {
    const res = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password })
    });
    if (res.ok) {
      const u = await res.json();
      onLoginSuccess(u);
    } else {
      const t = await res.json().catch(() => ({}));
      errEl.textContent = t.error || '登录失败';
    }
  } catch (e) {
    errEl.textContent = '网络错误: ' + e.message;
  }
}

async function doLogout() {
  try { await fetch('/api/logout', { method: 'POST' }); } catch (_) {}
  currentUser = null;
  document.body.classList.remove('is-admin');
  document.getElementById('current-user').style.display = 'none';
  document.getElementById('login-username').value = '';
  document.getElementById('login-password').value = '';
  document.getElementById('login-overlay').classList.remove('hidden');
}

// 修改密码（当前登录用户自助）
function openPwdModal() {
  document.getElementById('pwd-old').value = '';
  document.getElementById('pwd-new').value = '';
  document.getElementById('pwd-confirm').value = '';
  document.getElementById('pwd-err').textContent = '';
  document.getElementById('pwd-modal-overlay').classList.add('show');
}
function closePwdModal() {
  document.getElementById('pwd-modal-overlay').classList.remove('show');
}
async function doChangePwd() {
  const oldP = document.getElementById('pwd-old').value;
  const newP = document.getElementById('pwd-new').value;
  const confirmP = document.getElementById('pwd-confirm').value;
  const errEl = document.getElementById('pwd-err');
  errEl.textContent = '';
  if (!oldP || !newP || !confirmP) { errEl.textContent = '请填写完整'; return; }
  if (newP.length < 6) { errEl.textContent = '新密码至少 6 位'; return; }
  if (newP !== confirmP) { errEl.textContent = '两次新密码不一致'; return; }
  try {
    const res = await fetch('/api/me/password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ old_password: oldP, new_password: newP })
    });
    if (res.ok) {
      showToast('密码已修改', 'success');
      closePwdModal();
    } else {
      const t = await res.json().catch(() => ({}));
      errEl.textContent = t.error || '修改失败';
    }
  } catch (e) {
    errEl.textContent = '网络错误: ' + e.message;
  }
}

// ============================================================
// 用户管理（仅 admin）
// ============================================================
async function openUserModal() {
  await loadUserTable();
  document.getElementById('user-modal-overlay').classList.add('show');
}
function closeUserModal() {
  document.getElementById('user-modal-overlay').classList.remove('show');
}

async function loadUserTable() {
  const box = document.getElementById('user-list');
  box.innerHTML = '<div style="color:var(--text-muted);padding:12px">加载中...</div>';
  try {
    const res = await fetch('/api/users');
    if (!res.ok) { box.innerHTML = '<div class="login-err">加载失败</div>'; return; }
    const users = await res.json();
    if (!users.length) { box.innerHTML = '<div style="color:var(--text-muted);padding:12px">暂无用户</div>'; return; }
    box.innerHTML = users.map(u => {
      const roleTag = u.role === 'admin'
        ? '<span class="tag tag-admin">管理员</span>'
        : '<span class="tag tag-viewer">普通用户</span>';
      const statusTag = u.status === 1 ? '' : '<span class="tag tag-disabled">已禁用</span>';
      const projText = u.projects === 'all' ? '全部项目'
        : (u.projects && u.projects.length ? u.projects.join(', ') : '无授权项目');
      return '<div class="user-row">' +
        '<div class="u-main">' +
          '<div class="u-name">' + escHtml(u.username) + roleTag + statusTag + '</div>' +
          '<div class="u-meta">昵称: ' + escHtml(u.nickname || '-') + ' &middot; 授权项目: ' + escHtml(projText) + '</div>' +
        '</div>' +
        '<button class="btn btn-sm btn-outline" onclick="openUserEdit(' + u.id + ')">编辑</button>' +
        (u.username !== currentUser.username ? '<button class="btn btn-sm btn-danger" onclick="deleteUser(' + u.id + ')">删除</button>' : '') +
      '</div>';
    }).join('');
  } catch (e) {
    box.innerHTML = '<div class="login-err">加载失败: ' + escHtml(e.message) + '</div>';
  }
}

async function openUserEdit(id) {
  let allProjects = [];
  try {
    const pr = await fetch('/api/projects');
    if (pr.ok) { const pd = await pr.json(); allProjects = (pd || []).map(p => p.project); }
  } catch (_) {}
  let user = null;
  let authorizedRoles = {}; // project -> role
  if (id) {
    const res = await fetch('/api/users');
    if (res.ok) {
      const users = await res.json();
      user = users.find(x => x.id === id) || null;
      if (user && user.projects !== 'all') authorizedRoles = user.project_roles || {};
    }
  }
  document.getElementById('user-edit-id').value = id || '';
  document.getElementById('user-edit-title').textContent = id ? '编辑用户' : '新建用户';
  document.getElementById('user-edit-username').value = user ? user.username : '';
  document.getElementById('user-edit-username').disabled = !!id;
  document.getElementById('user-edit-nickname').value = user ? (user.nickname || '') : '';
  document.getElementById('user-edit-password').value = '';
  document.getElementById('user-edit-password').placeholder = id ? '留空表示不修改' : '请输入密码';
  document.getElementById('user-edit-role').value = user ? user.role : 'viewer';
  document.getElementById('user-edit-status').value = user ? String(user.status) : '1';
  const grid = document.getElementById('user-edit-projects');
  grid.innerHTML = allProjects.map(p => {
    const checked = p in authorizedRoles;
    const role = authorizedRoles[p] || 'viewer';
    return '<div class="user-proj-row" style="display:flex;align-items:center;gap:8px;margin:4px 0">' +
      '<label style="flex:1;display:flex;align-items:center;gap:4px;cursor:pointer">' +
        '<input type="checkbox" class="user-proj-check" value="' + escAttr(p) + '"' + (checked ? ' checked' : '') + '> ' + escHtml(p) +
      '</label>' +
      '<select class="user-proj-role" style="width:110px;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.8rem">' +
        '<option value="viewer"' + (role === 'editor' ? '' : ' selected') + '>只读</option>' +
        '<option value="editor"' + (role === 'editor' ? ' selected' : '') + '>管理</option>' +
      '</select>' +
    '</div>';
  }).join('') || '<span style="color:var(--text-muted)">暂无项目</span>';
  document.getElementById('user-edit-overlay').classList.add('show');
}

function closeUserEdit() {
  document.getElementById('user-edit-overlay').classList.remove('show');
}

async function saveUserEdit() {
  const id = document.getElementById('user-edit-id').value;
  // 收集勾选项目的角色映射 { project: role }
  const projects = {};
  document.querySelectorAll('#user-edit-projects .user-proj-row').forEach(row => {
    const check = row.querySelector('.user-proj-check');
    if (check && check.checked) {
      const roleSel = row.querySelector('.user-proj-role');
      projects[check.value] = roleSel ? roleSel.value : 'viewer';
    }
  });
  const payload = {
    username: document.getElementById('user-edit-username').value.trim(),
    password: document.getElementById('user-edit-password').value,
    nickname: document.getElementById('user-edit-nickname').value.trim(),
    role: document.getElementById('user-edit-role').value,
    status: parseInt(document.getElementById('user-edit-status').value, 10),
    projects: projects
  };
  if (!payload.username) { showToast('用户名必填', 'error'); return; }
  if (!id && !payload.password) { showToast('新建用户密码必填', 'error'); return; }
  try {
    const res = id
      ? await fetch('/api/users/' + id, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
      : await fetch('/api/users', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
    if (!res.ok) { const t = await res.json().catch(() => ({})); throw new Error(t.error || res.status); }
    showToast(id ? '用户已更新' : '用户已创建', 'success');
    closeUserEdit();
    loadUserTable();
    loadProjects();
  } catch (e) { showToast('保存失败: ' + e.message, 'error'); }
}

async function deleteUser(id) {
  if (!confirm('确认删除该用户？其项目授权也会一并清除。')) return;
  try {
    const res = await fetch('/api/users/' + id, { method: 'DELETE' });
    if (!res.ok) { const t = await res.json().catch(() => ({})); throw new Error(t.error || res.status); }
    showToast('用户已删除', 'success');
    loadUserTable();
  } catch (e) { showToast('删除失败: ' + e.message, 'error'); }
}

// 从列表页跳转到独立编排页（orch.html）编辑指定子链
// 新标签页打开子链编排页（从根链 Live Preview 点击子链节点进入）
function orchOpenSubChainInNewTab(instanceId) {
  const p = encodeURIComponent(getProject() || '');
  window.open('/orch?target=sub&chain_id=' + encodeURIComponent(orchSubBaseId(instanceId)) + (p ? '&project=' + p : ''), '_blank');
}

function orchOpenInPage(chainId) {
  const p = encodeURIComponent(getProject() || '');
  location.href = '/orch?target=sub&chain_id=' + encodeURIComponent(chainId) + (p ? '&project=' + p : '');
}

// 从列表页跳转到独立编排页（orch.html）编辑指定根链
function orchOpenInPageRoot(chainId) {
  const p = encodeURIComponent(getProject() || '');
  location.href = '/orch?target=root&chain_id=' + encodeURIComponent(chainId) + (p ? '&project=' + p : '');
}

// 子链编排独立页（orch.html）按 chain_id 载入编辑态
async function orchLoadSubChainById(chainId) {
  try {
    const chains = await api('/api/sub-chains');
    window._subChainsForEdit = chains;
    const i = chains.findIndex(c => c.chain_id === chainId);
    if (i < 0) { showToast('未找到子链: ' + chainId, 'error'); return; }
    orchSubChainByIndex(i);
  } catch (e) { showToast('加载子链失败: ' + e.message, 'error'); }
}

// 根链编排独立页（orch.html）按 chain_id 载入编辑态（不切换已删除的 tab）
async function orchLoadRootChainById(chainId) {
  try {
    // 列表接口已瘦身，不返回 dsl_json / 各 override / root_responses 等大字段，
    // 编排需要全量定义，这里按 ID 单条获取。
    const c = await api('/api/root-chains/' + encodeURIComponent(chainId));
    if (!c) { showToast('未找到根链: ' + chainId, 'error'); return; }
    document.getElementById('orch-chain-id').value = c.chain_id || '';
    document.getElementById('orch-chain-key').value = c.chain_key || '';
    document.getElementById('orch-chain-name').value = c.name || '';
    document.getElementById('orch-chain-desc').value = c.description || '';
    let debugMode = false;
    try { debugMode = !!((JSON.parse(c.dsl_json || '{}').ruleChain || {}).debugMode); } catch(e) {}
    document.getElementById('orch-debug-mode').checked = debugMode;
    const btn = document.getElementById('orch-generate-btn');
    btn.dataset.edit = '1';
    setOrchTarget('root');
    loadOrchData().then(() => {
      // 先恢复画布上的节点实例与子链选择：根节点返回值定义的候选下拉依赖它们，
      // 必须在 applyOrchRootResponses 之前完成，否则节点/字段下拉无数据、无法回显已保存的选择。
      const nodeIds = (c.node_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
      restoreOrchNodeInstances(nodeIds, c.dsl_json);
      const subIds = (c.sub_chain_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
      setOrchSelectedSubIds(subIds);
      applyOrchParamOverrides(c.node_param_overrides);
      applyOrchSwitchOverrides(c.node_switch_overrides);
      applyOrchNameOverrides(c.node_name_overrides);
    // 恢复已保存的每节点参数配置区收起状态
    applyOrchCollapseOverrides(c.node_collapse_overrides);
      // 恢复根节点返回值定义（Root Chain 专属）—— 必须在节点实例恢复之后
      applyOrchRootResponses(c.root_responses);
      onOrchSelectionChange();
      document.querySelectorAll('#orch-conn-container .orch-conn-row').forEach(r => r.remove());
      try {
        const conns = JSON.parse(c.connections_data || '[]');
        conns.forEach(conn => addOrchConnRow(conn.from_id, conn.to_id, conn.type));
      sortOrchConnRows();
      } catch(e) {}
      onOrchChange();
      showToast('已加载根链到编排页，修改后点击"更新 Root Chain"保存', 'success');
    });
  } catch (e) { showToast('加载根链失败: ' + e.message, 'error'); }
}

// 启动：先鉴权，再加载主界面
bootstrapAuth();

// ============================================================
// 子链实例按普通节点处理（custom/SubChainCall）
// ============================================================
// 子链实例（ID 形如 F000001__ab12c）在「节点参数配置」里和普通节点一样为每个入参选择来源，
// 入参取子链调用签名的 arguments（子链里所有 {{arguments.xxx}}），返回值取子链返回值定义，供下游「引用节点」选择。
// 参数配置随 node_param_overrides 保存（key 为子链实例 ID），后端组装 DSL 时写入调用子链节点的 arguments。
const ORCH_SUB_NODE_TYPE = 'custom/SubChainCall';
const _orchSubSigCache = {}; // 子链 ID -> { loading } | { sig } | { error }

function isOrchSubInst(inst) {
  return !!(inst && inst.type === ORCH_SUB_NODE_TYPE);
}

// 子链实例在参数配置区使用的实例对象（与节点实例同结构）
function orchSubParamInstances() {
  return getSelectedOrchSubIds().map(id => ({
    instanceId: id,
    nodeId: orchSubBaseId(id),
    name: orchSubDisplayName(id),
    type: ORCH_SUB_NODE_TYPE,
  }));
}

// 参数配置区的全部实例：节点实例 + 子链实例
function orchParamInstances() {
  return (window._orchNodeInstances || []).concat(orchSubParamInstances());
}

// 子链实例展示名：本链名称覆盖 > 子链名称 > 子链 ID
function orchSubDisplayName(instanceId) {
  const ov = (_orchNameOverrides || {})[instanceId];
  if (ov && String(ov).trim()) return String(ov).trim();
  const s = orchSubDefOf(instanceId);
  return (s && s.name) ? s.name : orchSubBaseId(instanceId);
}

// 取子链调用签名；未加载时异步拉取，拉取完成后刷新编排页
function getOrchSubSig(instanceId) {
  const base = orchSubBaseId(instanceId);
  if (!base) return null;
  const c = _orchSubSigCache[base];
  if (c && c.sig) return c.sig;
  if (!c) {
    _orchSubSigCache[base] = { loading: true };
    api('/api/sub-chains/' + encodeURIComponent(base) + '/call-signature').then(sig => {
      _orchSubSigCache[base] = { sig: sig };
      onOrchSelectionChange();
    }).catch(e => {
      _orchSubSigCache[base] = { error: e.message || String(e) };
      showToast('获取子链 ' + base + ' 的参数失败：' + (e.message || e), 'error');
    });
  }
  return null;
}

// 子链实例的参数：子链所需的入参。签名加载后同步实例的参数配置：
// 新增的入参预置来源=调用传入（同名参数，与子链直接读取本链入参的行为一致），子链已没有的入参删除。
function getOrchSubParams(instanceId) {
  const sig = getOrchSubSig(instanceId);
  if (!sig) return [];
  const keys = (sig.arguments || []).filter(Boolean);
  syncOrchSubParamPreset(instanceId, keys);
  return keys.map(k => ({ key: k, label: k, type: 'string', required: false }));
}

function syncOrchSubParamPreset(instanceId, keys) {
  if (!_orchParamPreset[instanceId]) _orchParamPreset[instanceId] = {};
  const preset = _orchParamPreset[instanceId];
  keys.forEach(k => {
    if (!preset[k]) preset[k] = { src: PARAM_SRC_ENTRY, value: k, type: 'string' };
  });
  Object.keys(preset).forEach(k => {
    if (!keys.includes(k)) delete preset[k];
  });
}

// 子链实例的返回值：子链返回值定义（供下游「引用节点」选择）
function getOrchSubOutputs(instanceId) {
  const sig = getOrchSubSig(instanceId);
  if (!sig) return [];
  return (sig.responses || []).filter(r => r && r.key)
    .map(r => ({ key: r.key, label: r.label || r.key, type: r.type || '' }));
}

// 从链 DSL 里取返回值定义（ruleChain.additionalInfo.root_responses），解析失败返回空数组
function orchDslRootResponses(dslJson) {
  try {
    const dsl = typeof dslJson === 'string' ? JSON.parse(dslJson || '{}') : (dslJson || {});
    const rr = ((dsl.ruleChain || {}).additionalInfo || {}).root_responses;
    return Array.isArray(rr) ? rr : [];
  } catch (e) {
    return [];
  }
}

// ============================================================
// 调用根链节点（custom/RootChainCall）
// ============================================================
// 每个实例选中的目标根链 ID 存在 _orchParamPreset[instanceId][ORCH_CALL_TARGET_KEY]，随 node_param_overrides 保存，
// 后端组装 DSL 时移到 configuration.target_chain_id；参数取目标根链当前发布版本的入参，返回值取其返回值定义。
const ORCH_CALL_NODE_TYPE = 'custom/RootChainCall';
const ORCH_CALL_TARGET_KEY = '__target_chain_id';
const _orchCallSigCache = {}; // chainId -> { loading } | { sig } | { error }

// 编排时再选择目标根链的调用根链实例（节点定义未固定目标根链）。
// 节点定义里已固定目标根链（configuration.target_chain_id）的调用节点按普通节点处理：参数、返回值取自节点定义。
function isOrchCallInst(inst) {
  return !!(inst && inst.type === ORCH_CALL_NODE_TYPE && !orchCallDefTarget(inst.nodeId));
}

// 节点定义里固定的目标根链 ID（未固定返回空）
function orchCallDefTarget(nodeId) {
  const def = (_orchNodes || []).find(n => n.node_id === nodeId);
  if (!def || def.type !== ORCH_CALL_NODE_TYPE) return '';
  let cfg = def.configuration;
  if (typeof cfg === 'string') { try { cfg = JSON.parse(cfg); } catch (e) { cfg = null; } }
  return cfg && cfg.target_chain_id ? String(cfg.target_chain_id).trim() : '';
}

// 已选列表 / 预览图上展示的调用目标：节点定义固定的直接显示，编排时选择的显示所选根链
function orchCallTargetText(inst) {
  if (!inst || inst.type !== ORCH_CALL_NODE_TYPE) return '';
  const fixed = orchCallDefTarget(inst.nodeId);
  if (fixed) return fixed + '（节点定义固定）';
  return getOrchCallTargetLabel(inst.instanceId) || '未选择';
}

// 取实例选中的目标根链 ID
function getOrchCallTarget(instanceId) {
  const p = _orchParamPreset[instanceId] && _orchParamPreset[instanceId][ORCH_CALL_TARGET_KEY];
  return p && p.value ? String(p.value).trim() : '';
}

// 取已加载的目标根链调用签名；未加载时异步拉取，拉取完成后刷新编排页
function getOrchCallSig(chainId) {
  if (!chainId) return null;
  const c = _orchCallSigCache[chainId];
  if (c && c.sig) return c.sig;
  if (!c) {
    _orchCallSigCache[chainId] = { loading: true };
    fetchOrchCallSig(chainId).then(() => {
      renderOrchNodeSelected();
      onOrchSelectionChange();
    });
  }
  return null;
}

async function fetchOrchCallSig(chainId) {
  try {
    const sig = await api('/api/root-chains/' + encodeURIComponent(chainId) + '/call-signature');
    _orchCallSigCache[chainId] = { sig: sig };
    return sig;
  } catch (e) {
    _orchCallSigCache[chainId] = { error: e.message || String(e) };
    showToast('获取根链 ' + chainId + ' 的参数失败：' + (e.message || e), 'error');
    return null;
  }
}

// 调用根链节点的参数：目标根链所需的入参
function getOrchCallParams(inst) {
  const sig = getOrchCallSig(getOrchCallTarget(inst.instanceId));
  if (!sig) return [];
  return (sig.arguments || []).filter(Boolean).map(k => ({ key: k, label: k, type: 'string', required: true }));
}

// 调用根链节点的返回值：目标根链的返回值定义（供下游「引用节点」选择）
function getOrchCallOutputs(inst) {
  const sig = getOrchCallSig(getOrchCallTarget(inst.instanceId));
  if (!sig) return [];
  return (sig.responses || []).filter(r => r && r.key)
    .map(r => ({ key: r.key, label: r.label || r.key, type: r.type || '' }));
}

// 目标根链显示名（已加载签名用名称，否则用 ID）
function getOrchCallTargetLabel(instanceId) {
  const id = getOrchCallTarget(instanceId);
  if (!id) return '';
  const c = _orchCallSigCache[id];
  return (c && c.sig && c.sig.name) ? (c.sig.name + '（' + id + '）') : id;
}

// 选中目标根链后重置实例参数：保留目标 key，其余参数按新根链入参预置为本节点私有的「调用传入」参数，
// 即调用方传 {{arguments.<本节点实例ID>.<目标根链入参名>}}，同一根链被调用多次时各实例入参互不冲突。
function seedOrchCallDefaults(instanceId, chainId, sig) {
  const preset = {};
  preset[ORCH_CALL_TARGET_KEY] = { src: PARAM_SRC_FIXED, value: chainId };
  (sig.arguments || []).filter(Boolean).forEach(k => {
    preset[k] = { src: PARAM_SRC_ENTRY, value: k, type: 'string', private: true };
  });
  _orchParamPreset[instanceId] = preset;
}

let _orchCallPickerCtx = null; // { nodeId } 新增实例 | { instanceId } 更换目标

// 打开目标根链选择框：只列出已发布的根链，排除当前正在编辑的根链
async function openOrchCallChainPicker(ctx) {
  _orchCallPickerCtx = ctx;
  let overlay = document.getElementById('orch-call-modal-overlay');
  if (!overlay) {
    overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.id = 'orch-call-modal-overlay';
    overlay.innerHTML = `<div class="modal" style="width:560px;max-width:96vw">
      <h2>选择要调用的根链<button class="modal-close" onclick="closeOrchCallChainPicker()" title="关闭">&times;</button></h2>
      <div class="form-group">
        <input type="text" id="orch-call-search" placeholder="按名称 / ID 过滤" oninput="renderOrchCallChainOptions()">
      </div>
      <div class="form-group">
        <select id="orch-call-select" size="10" style="width:100%"></select>
        <div style="font-size:.75rem;color:var(--text-muted);margin-top:6px">只列出已发布的根链；调用时使用目标根链的当前发布版本，参数和返回值也取自该版本。</div>
      </div>
      <div class="modal-footer">
        <button class="btn btn-outline" onclick="closeOrchCallChainPicker()">取消</button>
        <button class="btn btn-primary" onclick="confirmOrchCallChainPicker()">确定</button>
      </div>
    </div>`;
    document.body.appendChild(overlay);
  }
  document.getElementById('orch-call-search').value = '';
  const sel = document.getElementById('orch-call-select');
  sel.innerHTML = '<option value="">加载中…</option>';
  overlay.classList.add('show');
  try {
    const list = await api('/api/root-chains');
    const selfId = window._orchTarget !== 'sub' ? ((document.getElementById('orch-chain-id') || {}).value || '').trim() : '';
    window._orchCallChainList = (list || []).filter(c => c && c.has_releases && c.status !== 0 && c.chain_id !== selfId);
  } catch (e) {
    window._orchCallChainList = [];
    showToast('加载根链列表失败：' + (e.message || e), 'error');
  }
  renderOrchCallChainOptions();
}

function renderOrchCallChainOptions() {
  const sel = document.getElementById('orch-call-select');
  if (!sel) return;
  const q = ((document.getElementById('orch-call-search') || {}).value || '').trim().toLowerCase();
  const cur = _orchCallPickerCtx && _orchCallPickerCtx.instanceId ? getOrchCallTarget(_orchCallPickerCtx.instanceId) : '';
  const items = (window._orchCallChainList || []).filter(c =>
    !q || String(c.name || '').toLowerCase().includes(q) || String(c.chain_id || '').toLowerCase().includes(q)
      || String(c.chain_key || '').toLowerCase().includes(q));
  if (!items.length) {
    sel.innerHTML = '<option value="">— 没有可调用的已发布根链 —</option>';
    return;
  }
  sel.innerHTML = items.map(c =>
    `<option value="${esc(c.chain_id)}"${c.chain_id === cur ? ' selected' : ''}>${esc(c.name || c.chain_id)}（${esc(c.chain_id)}）</option>`).join('');
}

function closeOrchCallChainPicker() {
  const overlay = document.getElementById('orch-call-modal-overlay');
  if (overlay) overlay.classList.remove('show');
  _orchCallPickerCtx = null;
}

async function confirmOrchCallChainPicker() {
  const sel = document.getElementById('orch-call-select');
  const chainId = sel ? sel.value : '';
  if (!chainId) { showToast('请选择要调用的根链', 'error'); return; }
  const ctx = _orchCallPickerCtx || {};
  const sig = await fetchOrchCallSig(chainId);
  if (!sig) return;
  if (ctx.instanceId) {
    const inst = (window._orchNodeInstances || []).find(i => i.instanceId === ctx.instanceId);
    if (!inst) { closeOrchCallChainPicker(); return; }
    const oldSig = getOrchCallSig(getOrchCallTarget(ctx.instanceId));
    const curName = _orchNameOverrides[ctx.instanceId] || '';
    // 名称仍是自动生成的（空或等于旧根链名）时跟随新根链改名，用户手动改过的名称保留
    if (!curName || (oldSig && curName === oldSig.name)) _orchNameOverrides[ctx.instanceId] = sig.name || chainId;
    seedOrchCallDefaults(ctx.instanceId, chainId, sig);
  } else if (ctx.nodeId) {
    const n = (_orchNodes || []).find(x => x.node_id === ctx.nodeId);
    const instanceId = genOrchInstanceId(ctx.nodeId);
    if (!window._orchNodeInstances) window._orchNodeInstances = [];
    window._orchNodeInstances.push({
      instanceId: instanceId,
      nodeId: ctx.nodeId,
      name: sig.name || chainId,
      type: ORCH_CALL_NODE_TYPE,
      kind: n ? (n.kind || 'call') : 'call',
      outputs: [],
    });
    _orchNameOverrides[instanceId] = sig.name || chainId;
    seedOrchCallDefaults(instanceId, chainId, sig);
  }
  closeOrchCallChainPicker();
  renderOrchNodeSelected();
  onOrchSelectionChange();
}

// ============================================================
// 参数输入节点（custom/ParamInput）
// ============================================================
// 每个实例的参数定义（[{key, type, description}]）以 JSON 字符串存在 _orchParamPreset[instanceId][ORCH_INPUT_DEFS_KEY]，
// 随 node_param_overrides 保存，后端组装 DSL 时移到 configuration.input_params。
// 各参数的来源仍按普通节点参数配置（固定配置 / 引用节点 / 调用传入），运行时解析出的值写入
// steps.<实例ID>.arguments 与 steps.<实例ID>.responses，下游「引用节点」可直接选这些参数。
const ORCH_INPUT_NODE_TYPE = 'custom/ParamInput';
const ORCH_INPUT_DEFS_KEY = '__input_params';
const ORCH_INPUT_PARAM_TYPES = ['string', 'int64', 'float64', 'bool', 'slice', 'map'];
const ORCH_INPUT_KEY_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;

function isOrchInputInst(inst) {
  return !!(inst && inst.type === ORCH_INPUT_NODE_TYPE);
}

// 条件判断节点（custom/CondSwitch）：与参数输入节点一样可在编排时为本实例定义参数（存同一保留 key），
// 再在「节点参数配置」里为每个参数选来源，路由条件里用 [arguments.参数名] 判断。
const ORCH_COND_NODE_TYPE = 'custom/CondSwitch';

function isOrchCondInst(inst) {
  return !!(inst && inst.type === ORCH_COND_NODE_TYPE);
}

// 实例是否已有本链参数定义（空数组也算：表示参数已在编排里全部删除）
function hasOrchInputDefs(instanceId) {
  return !!(_orchParamPreset[instanceId] && _orchParamPreset[instanceId][ORCH_INPUT_DEFS_KEY]);
}

// 条件判断节点的参数列表：编排里定义过参数就以编排为准；没定义过（旧链）沿用节点定义参数
function mergeOrchCondParams(inst, defParams) {
  if (inst && hasOrchInputDefs(inst.instanceId)) return getOrchInputParams(inst.instanceId);
  return (defParams || []).filter(p => p && p.key);
}

// 节点定义里的参数转为编排参数定义（字段名 / 名称 / 转换类型 / 默认值），用于拉入节点时预填
function orchCondDefsFromNode(nodeId) {
  return parseNodeParams(orchNodeDefById(nodeId)).map(p => ({
    key: p.key, label: p.label || p.description || '', type: p.type || '',
    value: p.value == null ? '' : (typeof p.value === 'string' ? p.value : JSON.stringify(p.value)),
  }));
}

// 取实例的参数定义
function getOrchInputDefs(instanceId) {
  const p = _orchParamPreset[instanceId] && _orchParamPreset[instanceId][ORCH_INPUT_DEFS_KEY];
  if (!p || !p.value) return [];
  try {
    const arr = typeof p.value === 'string' ? JSON.parse(p.value) : p.value;
    if (!Array.isArray(arr)) return [];
    // 旧数据只有 description（说明），作为名称回显
    return arr.filter(d => d && d.key).map(d => ({
      key: d.key, label: d.label || d.description || '', type: d.type || '', value: d.value == null ? '' : String(d.value),
    }));
  } catch (e) {
    return [];
  }
}

// 保存实例的参数定义（空数组也保存，表示已添加但尚未定义参数）
function setOrchInputDefs(instanceId, defs) {
  if (!_orchParamPreset[instanceId]) _orchParamPreset[instanceId] = {};
  _orchParamPreset[instanceId][ORCH_INPUT_DEFS_KEY] = {
    src: PARAM_SRC_FIXED,
    value: JSON.stringify((defs || []).map(d => ({ key: d.key, label: d.label || '', type: d.type || '', value: d.value || '' }))),
  };
}

// 参数配置区与引用候选使用的参数列表
function getOrchInputParams(instanceId) {
  return getOrchInputDefs(instanceId).map(d => ({
    key: d.key, label: d.label || '', type: d.type || '', required: false, description: d.label || '', defaultValue: d.value || '',
  }));
}

// 已选列表 / 预览图里展示的参数摘要
function orchInputDefsSummary(instanceId) {
  const defs = getOrchInputDefs(instanceId);
  return defs.length ? '参数：' + defs.map(d => d.key).join(', ') : '尚未定义参数';
}

let _orchInputEditInstId = '';

// 参数列表（字段名 / 名称 / 转换类型 / 默认值）的公共编辑区：添加节点时的「编辑输入参数」弹框，
// 以及点击 Live Preview 节点打开的「编辑节点」弹框都用它，boxId 为行容器 id，其 data-count 指向计数元素 id。
const ORCH_INPUT_BOX_ID = 'orch-input-rows';
const ORCH_INPUT_SWITCH_BOX_ID = 'orch-switch-input-rows';

// 用实例已有的参数定义填充编辑区；没有参数时放一行空行方便直接填写
function fillOrchInputParamRows(instanceId, boxId) {
  const box = document.getElementById(boxId);
  if (!box) return;
  box.innerHTML = '';
  let defs = getOrchInputDefs(instanceId);
  // 条件判断节点还没在编排里定义过参数（旧链）：用节点定义里的参数预填，保存后以编排为准
  const inst = (window._orchNodeInstances || []).find(x => x.instanceId === instanceId);
  if (isOrchCondInst(inst) && !hasOrchInputDefs(instanceId)) defs = orchCondDefsFromNode(inst.nodeId);
  if (defs.length) defs.forEach(d => addOrchInputParamRow(d, boxId));
  else addOrchInputParamRow(null, boxId);
  refreshOrchInputParamCount(boxId);
}

// 打开参数定义编辑框（添加节点时自动打开，也可从已选列表的「编辑参数」打开）
function openOrchInputParamEditor(instanceId) {
  _orchInputEditInstId = instanceId;
  let overlay = document.getElementById('orch-input-modal-overlay');
  if (!overlay) {
    overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.id = 'orch-input-modal-overlay';
    overlay.innerHTML = `<div class="modal" style="width:680px;max-width:96vw">
      <h2>编辑输入参数<button class="modal-close" onclick="closeOrchInputParamEditor()" title="关闭">&times;</button></h2>
      <div style="font-size:.75rem;color:var(--text-muted);margin-bottom:8px">定义本节点输出的参数，确定后在「节点参数配置」里为每个参数配置来源；下游节点可通过「引用节点」直接引用这些参数。字段名只能包含字母、数字和下划线，且不能以数字开头；来源取不到值（为空）时使用默认值。之后点击 Live Preview 中的该节点也可以继续编辑参数。</div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:4px">
        <span id="orch-input-count" style="color:var(--text-muted);font-size:.78rem">0 个参数</span>
        <button class="btn btn-sm btn-outline" type="button" onclick="addOrchInputParamRow(null, '${ORCH_INPUT_BOX_ID}')">+ 添加参数</button>
      </div>
      <div id="${ORCH_INPUT_BOX_ID}" data-count="orch-input-count" style="max-height:50vh;overflow-y:auto"></div>
      <div class="modal-footer">
        <button class="btn btn-outline" onclick="closeOrchInputParamEditor()">取消</button>
        <button class="btn btn-primary" onclick="confirmOrchInputParamEditor()">确定</button>
      </div>
    </div>`;
    document.body.appendChild(overlay);
  }
  fillOrchInputParamRows(instanceId, ORCH_INPUT_BOX_ID);
  overlay.classList.add('show');
}

// 新增一行参数定义（字段名 / 名称 / 转换类型 / 默认值，与根节点返回值定义一致）；
// data-orig 记录原字段名，改名时把已配置的来源迁移到新名字下
function addOrchInputParamRow(def, boxId) {
  const box = document.getElementById(boxId || ORCH_INPUT_BOX_ID);
  if (!box) return;
  const d = def || { key: '', label: '', type: '', value: '' };
  const inputStyle = 'min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem';
  const row = document.createElement('div');
  row.className = 'orch-input-row';
  row.style.cssText = 'display:flex;flex-wrap:wrap;align-items:center;gap:6px;padding:6px 0;border-bottom:1px dashed var(--border)';
  row.setAttribute('data-orig', d.key || '');
  row.innerHTML = `<input class="orch-input-key" placeholder="key（字段名）" title="参数的键名" value="${esc(d.key || '')}" style="flex:0 0 140px;${inputStyle};font-family:monospace">
    <input class="orch-input-label" placeholder="名称（说明）" title="中文名/说明（选填）" value="${esc(d.label || '')}" style="flex:0 0 120px;${inputStyle}">
    <select class="orch-input-type" title="转换类型" style="flex:0 0 92px;${inputStyle}">${orchRootResponseTypeHTML(d.type || '')}</select>
    <input class="orch-input-value" placeholder="默认值" title="来源取不到值时使用，可填固定值或 {{arguments.xxx}} 等表达式" value="${esc(d.value || '')}" style="flex:1 1 180px;${inputStyle};font-family:monospace">
    <button class="btn btn-sm btn-danger" type="button" title="删除该参数" onclick="removeOrchInputParamRow(this)">✕</button>`;
  box.appendChild(row);
  refreshOrchInputParamCount(box.id);
  const keyEl = row.querySelector('.orch-input-key');
  if (!def && keyEl) keyEl.focus();
}

function removeOrchInputParamRow(btn) {
  const row = btn.closest('.orch-input-row');
  const box = row ? row.parentElement : null;
  if (row) row.remove();
  if (box) refreshOrchInputParamCount(box.id);
}

function refreshOrchInputParamCount(boxId) {
  const box = document.getElementById(boxId || ORCH_INPUT_BOX_ID);
  if (!box) return;
  const cnt = document.getElementById(box.getAttribute('data-count') || '');
  if (cnt) cnt.textContent = box.querySelectorAll('.orch-input-row').length + ' 个参数';
}

function closeOrchInputParamEditor() {
  const overlay = document.getElementById('orch-input-modal-overlay');
  if (overlay) overlay.classList.remove('show');
  _orchInputEditInstId = '';
}

// 读取编辑区里的参数定义并校验字段名；校验不通过时提示并返回 null
function collectOrchInputParamRows(boxId) {
  const rows = Array.from(document.querySelectorAll('#' + boxId + ' .orch-input-row'));
  const defs = [];
  const seen = {};
  for (const row of rows) {
    const key = (row.querySelector('.orch-input-key').value || '').trim();
    const label = (row.querySelector('.orch-input-label').value || '').trim();
    const type = row.querySelector('.orch-input-type').value || '';
    const value = (row.querySelector('.orch-input-value').value || '').trim();
    const orig = row.getAttribute('data-orig') || '';
    if (!key && !label && !value) continue; // 空行忽略
    if (!key) { showToast('有参数未填写字段名', 'error'); return null; }
    if (!ORCH_INPUT_KEY_RE.test(key)) { showToast('字段名 ' + key + ' 不合法：只能包含字母、数字和下划线，且不能以数字开头', 'error'); return null; }
    if (seen[key]) { showToast('字段名 ' + key + ' 重复', 'error'); return null; }
    seen[key] = true;
    defs.push({ key, label, type, value, orig });
  }
  return defs;
}

// 应用新的参数定义：同步参数来源配置（新增参数默认「调用传入」同名公共参数，删除参数清掉配置，
// 改名保留已配置的来源），并替换其它节点对改名参数的引用
function applyOrchInputDefs(instanceId, defs) {
  storeParamPreset(); // 先把参数配置区当前的编辑结果写回暂存
  const oldPreset = _orchParamPreset[instanceId] || {};
  const newPreset = {};
  const inst = (window._orchNodeInstances || []).find(x => x.instanceId === instanceId);
  const isCond = isOrchCondInst(inst);
  defs.forEach(d => {
    const prev = oldPreset[d.orig || d.key] || null;
    let p;
    if (prev) {
      p = Object.assign({}, prev, { key: d.key });
      // 来源不是固定配置时类型跟随参数定义；调用传入的同名参数随改名一起改
      if (p.src !== PARAM_SRC_FIXED) p.type = d.type;
      if (p.src === PARAM_SRC_ENTRY && !p.private && d.orig && d.orig !== d.key && String(p.value || '').trim() === d.orig) p.value = d.key;
    } else if (isCond) {
      // 条件判断节点的新参数一般取上游的值：默认「引用节点」，在节点参数配置里选具体来源
      p = { src: PARAM_SRC_UPSTREAM, value: '', private: false, type: d.type, key: d.key, required: false, policy: 'frontend' };
    } else {
      p = { src: PARAM_SRC_ENTRY, value: d.key, private: false, type: d.type, key: d.key, required: false, policy: 'frontend' };
    }
    p.label = d.label;
    p.description = d.label;
    newPreset[d.key] = p;
  });
  _orchParamPreset[instanceId] = newPreset;
  setOrchInputDefs(instanceId, defs);
  // 改名的参数：同步替换其它节点里对它的引用 {{steps.<本实例>.arguments|responses.<旧名>}}
  defs.filter(d => d.orig && d.orig !== d.key).forEach(d => {
    const from = new RegExp('\\{\\{steps\\.' + instanceId + '\\.(arguments|responses)\\.' + d.orig + '\\}\\}', 'g');
    Object.keys(_orchParamPreset).forEach(nid => {
      if (nid === instanceId) return;
      Object.values(_orchParamPreset[nid] || {}).forEach(p => {
        if (p && typeof p.value === 'string') p.value = p.value.replace(from, '{{steps.' + instanceId + '.$1.' + d.key + '}}');
      });
    });
  });
}

// 「编辑输入参数」弹框确定
function confirmOrchInputParamEditor() {
  const instanceId = _orchInputEditInstId;
  if (!instanceId) { closeOrchInputParamEditor(); return; }
  const defs = collectOrchInputParamRows(ORCH_INPUT_BOX_ID);
  if (!defs) return;
  applyOrchInputDefs(instanceId, defs);
  closeOrchInputParamEditor();
  renderOrchNodeSelected();
  onOrchSelectionChange();
}

// ============================================================
// Node 弹框：调用根链节点（custom/RootChainCall）选择目标根链
// ============================================================
// 选中的目标根链写入 configuration.target_chain_id；参数定义取该根链当前发布版本所需的全部入参
// （默认值留空，可手动填写），返回值定义取其返回值定义。
// 节点定义固定目标根链后，编排时按普通节点添加，不再弹出根链选择框。

function readNodeConfigJSON() {
  const ta = document.getElementById('node-configuration');
  try {
    const cfg = JSON.parse((ta && ta.value) || '{}');
    return (cfg && typeof cfg === 'object') ? cfg : {};
  } catch (e) {
    return null;
  }
}

function nodeChainCallTargetFromConfig() {
  const cfg = readNodeConfigJSON();
  return cfg && cfg.target_chain_id ? String(cfg.target_chain_id).trim() : '';
}

// 把目标根链写回 Configuration（JSON 解析失败时不覆盖用户内容，返回 false）
function writeNodeChainCallTarget(chainId) {
  const cfg = readNodeConfigJSON();
  if (!cfg) { showToast('Configuration 不是合法 JSON，无法写入目标根链', 'error'); return false; }
  if (chainId) cfg.target_chain_id = chainId; else delete cfg.target_chain_id;
  document.getElementById('node-configuration').value = prettyJson(cfg);
  return true;
}

// 类型为调用根链节点时显示目标根链选择，并加载已发布根链列表
async function toggleNodeChainCallSection(type) {
  const sec = document.getElementById('node-chaincall-section');
  if (!sec) return;
  const show = type === 'custom/RootChainCall';
  sec.style.display = show ? '' : 'none';
  if (!show) return;
  const sel = document.getElementById('node-chaincall-target');
  const cur = nodeChainCallTargetFromConfig();
  const hint = document.getElementById('node-chaincall-hint');
  if (hint) hint.textContent = '不选择时，在根链编排里添加该节点时再选择目标根链；目标根链重新发布后可点「重新加载参数」同步。';
  sel.innerHTML = '<option value="">加载中…</option>';
  let list = [];
  try {
    list = await api('/api/root-chains');
  } catch (e) {
    showToast('加载根链列表失败：' + (e.message || e), 'error');
  }
  const published = (list || []).filter(c => c && c.has_releases && c.status !== 0);
  let html = '<option value="">— 不指定（编排时再选择）—</option>' +
    published.map(c => '<option value="' + esc(c.chain_id) + '">' + esc(c.name || c.chain_id) + '（' + esc(c.chain_id) + '）</option>').join('');
  if (cur && !published.some(c => c.chain_id === cur)) {
    html += '<option value="' + esc(cur) + '">' + esc(cur) + '（未发布或已停用）</option>';
  }
  sel.innerHTML = html;
  sel.value = cur;
  sel.setAttribute('data-prev', cur);
}

function nodeHasParamOrOutputRows() {
  return !!document.querySelector('#node-params-container .param-row, #node-outputs-container .param-row');
}

// 切换目标根链：写入 Configuration，并按新根链重新生成参数与返回值定义（已有内容时先确认）
async function onNodeChainCallTargetChange() {
  const sel = document.getElementById('node-chaincall-target');
  const prev = sel.getAttribute('data-prev') || '';
  const chainId = sel.value;
  if (chainId && nodeHasParamOrOutputRows() &&
      !confirm('将按根链 ' + chainId + ' 的当前发布版本重新生成参数定义和返回值定义，覆盖当前已填写的内容，继续吗？')) {
    sel.value = prev;
    return;
  }
  if (!writeNodeChainCallTarget(chainId)) { sel.value = prev; return; }
  sel.setAttribute('data-prev', chainId);
  if (chainId) await loadNodeChainCallSignature(chainId);
}

// 按目标根链当前发布版本重新加载参数与返回值定义
async function reloadNodeChainCallSignature() {
  const chainId = (document.getElementById('node-chaincall-target') || {}).value || '';
  if (!chainId) { showToast('请先选择目标根链', 'error'); return; }
  if (nodeHasParamOrOutputRows() &&
      !confirm('将按根链 ' + chainId + ' 的当前发布版本重新生成参数定义和返回值定义，覆盖当前已填写的内容，继续吗？')) return;
  await loadNodeChainCallSignature(chainId);
}

async function loadNodeChainCallSignature(chainId) {
  let sig;
  try {
    sig = await api('/api/root-chains/' + encodeURIComponent(chainId) + '/call-signature');
  } catch (e) {
    showToast('获取根链 ' + chainId + ' 的参数失败：' + (e.message || e), 'error');
    return;
  }
  clearParamRows();
  (sig.arguments || []).filter(Boolean).forEach(k => {
    addParamRow(k, k, 'string', true, '', '目标根链 ' + chainId + ' 的入参', 'frontend+');
  });
  clearOutputRows();
  (sig.responses || []).filter(r => r && r.key).forEach(r => {
    addOutputRow({ key: r.key, label: r.label || r.key, type: r.type || '', source: 'value', value: '' });
  });
  const nameEl = document.getElementById('node-name');
  if (nameEl && !nameEl.value.trim()) nameEl.value = '调用' + (sig.name || chainId);
  const hint = document.getElementById('node-chaincall-hint');
  if (hint) hint.textContent = '已按 ' + (sig.name || chainId) + ' v' + sig.version + ' 加载 ' + (sig.arguments || []).length +
    ' 个参数、' + (sig.responses || []).length + ' 个返回值；目标根链重新发布后可点「重新加载参数」同步。';
  showToast('已加载根链 ' + chainId + ' 的参数与返回值定义', 'success');
}

// ============================================================
// 循环节点（custom/Loop）
// ============================================================
// 每个实例的循环配置（模式、条件、循环体等）以 JSON 字符串存在 _orchParamPreset[instanceId][ORCH_LOOP_CONFIG_KEY]，
// 随 node_param_overrides 保存，后端组装 DSL 时移到 configuration.loop。
// 遍历模式的循环数据是参数 loop_items，在「节点参数配置」里像普通参数一样配置来源（引用节点 / 调用传入 / 固定配置）；
// 循环体为根链时，其余参数是传给目标根链的入参映射（参数取目标根链当前发布版本的入参），
// 可引用循环变量 {{arguments._loop.item}} / {{arguments._loop.index}} / {{arguments._loop.last}}。
const ORCH_LOOP_NODE_TYPE = 'custom/Loop';
const ORCH_LOOP_CONFIG_KEY = '__loop_config';
const ORCH_LOOP_MODES = { foreach: '遍历数组', while: '条件循环' };
const ORCH_LOOP_ITEMS_KEY = 'loop_items';
const ORCH_LOOP_ITEMS_PARAM = { key: ORCH_LOOP_ITEMS_KEY, label: '循环数据', type: 'slice', required: true,
  description: '要遍历的数组（或对象，按键遍历），每个元素执行一次循环体' };
const ORCH_LOOP_OUTPUTS = [
  { key: 'items', label: '每轮结果（数组）', type: 'slice' },
  { key: 'count', label: '执行轮数', type: 'int64' },
  { key: 'success', label: '成功轮数', type: 'int64' },
  { key: 'failed', label: '失败轮数', type: 'int64' },
  { key: 'errors', label: '失败明细', type: 'slice' },
  { key: 'break_index', label: '跳出轮次（-1 表示未跳出）', type: 'int64' },
];

function isOrchLoopInst(inst) {
  return !!(inst && inst.type === ORCH_LOOP_NODE_TYPE);
}

// 取实例的循环配置（带默认值）
function getOrchLoopConfig(instanceId) {
  const def = { mode: 'foreach', condition: '', break_condition: '', body_type: 'subchain', body_id: '',
    max_iterations: 100, concurrency: 1, interval_ms: 0, fail_policy: 'stop' };
  const p = _orchParamPreset[instanceId] && _orchParamPreset[instanceId][ORCH_LOOP_CONFIG_KEY];
  if (!p || !p.value) return def;
  try {
    const cfg = typeof p.value === 'string' ? JSON.parse(p.value) : p.value;
    return Object.assign(def, cfg || {});
  } catch (e) {
    return def;
  }
}

function setOrchLoopConfig(instanceId, cfg) {
  if (!_orchParamPreset[instanceId]) _orchParamPreset[instanceId] = {};
  _orchParamPreset[instanceId][ORCH_LOOP_CONFIG_KEY] = { src: PARAM_SRC_FIXED, value: JSON.stringify(cfg) };
}

// 循环节点的参数：遍历模式先是循环数据 loop_items；循环体为根链时再加上目标根链所需的入参
// （循环体为子链时子链直接共享执行上下文，没有入参）
function getOrchLoopParams(inst) {
  const cfg = getOrchLoopConfig(inst.instanceId);
  const params = [];
  if (cfg.mode === 'foreach') {
    // 优先用节点定义里的 loop_items（编辑 Node 页选循环节点类型时自动添加）；旧的节点定义没有时用内置定义
    const def = (_orchNodes || []).find(n => n.node_id === inst.nodeId);
    const defParam = parseNodeParams(def).find(p => p.key === ORCH_LOOP_ITEMS_KEY);
    params.push(Object.assign({}, ORCH_LOOP_ITEMS_PARAM, defParam || {}));
  }
  if (cfg.body_type !== 'rootchain' || !cfg.body_id) return params;
  const sig = getOrchCallSig(cfg.body_id);
  if (!sig) return params;
  (sig.arguments || []).filter(k => k && k !== ORCH_LOOP_ITEMS_KEY)
    .forEach(k => params.push({ key: k, label: k, type: 'string', required: true }));
  return params;
}

// 循环体显示名
function orchLoopBodyLabel(cfg) {
  if (!cfg.body_id) return '未选择';
  if (cfg.body_type === 'rootchain') {
    const c = _orchCallSigCache[cfg.body_id];
    return '根链 ' + ((c && c.sig && c.sig.name) ? (c.sig.name + '（' + cfg.body_id + '）') : cfg.body_id);
  }
  const s = (_orchSubChains || []).find(x => x.chain_id === cfg.body_id);
  return '子链 ' + (s ? ((s.name || s.chain_id) + '（' + cfg.body_id + '）') : cfg.body_id);
}

// 已选列表 / 预览图上展示的循环摘要
function orchLoopSummary(instanceId) {
  const cfg = getOrchLoopConfig(instanceId);
  const parts = [ORCH_LOOP_MODES[cfg.mode] || cfg.mode];
  if (Number(cfg.concurrency) > 1) parts.push('并发 ' + cfg.concurrency);
  parts.push('循环体：' + orchLoopBodyLabel(cfg));
  return '循环：' + parts.join(' · ');
}

let _orchLoopEditInstId = '';

// 打开循环配置框（添加循环节点时自动打开，也可从已选列表的「编辑循环」打开）
async function openOrchLoopEditor(instanceId) {
  _orchLoopEditInstId = instanceId;
  let overlay = document.getElementById('orch-loop-modal-overlay');
  if (!overlay) {
    overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.id = 'orch-loop-modal-overlay';
    const row = 'display:flex;gap:10px;flex-wrap:wrap';
    const col = 'flex:1 1 200px;min-width:0';
    overlay.innerHTML = `<div class="modal" style="width:720px;max-width:96vw">
      <h2>循环配置<button class="modal-close" onclick="closeOrchLoopEditor()" title="关闭">&times;</button></h2>
      <div style="max-height:70vh;overflow-y:auto">
      <div style="${row}">
        <div class="form-group" style="${col}">
          <label>循环模式</label>
          <select id="orch-loop-mode" onchange="refreshOrchLoopEditorFields()">
            <option value="foreach">遍历数组：每个元素执行一次</option>
            <option value="while">条件循环：满足条件就继续</option>
          </select>
        </div>
        <div class="form-group" style="${col}">
          <label>循环体类型</label>
          <select id="orch-loop-body-type" onchange="renderOrchLoopBodyOptions()">
            <option value="subchain">子链（共享执行上下文）</option>
            <option value="rootchain">根链（按入参调用，执行当前发布版本）</option>
          </select>
        </div>
      </div>
      <div class="form-group" id="orch-loop-range-wrap">
        <div style="font-size:.75rem;color:var(--text-muted)">遍历的数据在确定后的「节点参数配置」里配置：参数 <code>loop_items</code>（循环数据），可引用上游节点返回的数组、由调用方传入，或固定写一个 JSON 数组。</div>
      </div>
      <div class="form-group" id="orch-loop-cond-wrap">
        <label>继续条件（每轮执行前判断，为 true 才执行）</label>
        <input type="text" id="orch-loop-condition" placeholder='如 [arguments._loop.index] == 0 || [arguments._loop.last.status] != "done"' style="font-family:monospace">
      </div>
      <div class="form-group">
        <label>跳出条件（选填，每轮执行后判断，为 true 即结束循环）</label>
        <input type="text" id="orch-loop-break" placeholder='如 [arguments._loop.last.ok] == true' style="font-family:monospace">
      </div>
      <div class="form-group">
        <label>循环体</label>
        <input type="text" id="orch-loop-body-search" placeholder="按名称 / ID 过滤" oninput="renderOrchLoopBodyOptions()" style="margin-bottom:6px">
        <select id="orch-loop-body" size="6" style="width:100%"></select>
        <div id="orch-loop-body-tip" style="font-size:.75rem;color:var(--text-muted);margin-top:4px"></div>
      </div>
      <div style="${row}">
        <div class="form-group" style="flex:1 1 120px"><label>最大循环次数</label><input type="number" id="orch-loop-max" min="1" max="1000"></div>
        <div class="form-group" style="flex:1 1 120px"><label>并发数</label><input type="number" id="orch-loop-concurrency" min="1" max="20"></div>
        <div class="form-group" style="flex:1 1 120px"><label>每轮间隔（毫秒）</label><input type="number" id="orch-loop-interval" min="0" max="60000"></div>
        <div class="form-group" style="flex:1 1 160px">
          <label>失败策略</label>
          <select id="orch-loop-fail">
            <option value="stop">任一轮失败就停止（走 Failure）</option>
            <option value="continue">跳过失败的轮次继续（走 Success）</option>
          </select>
        </div>
      </div>
      <div style="font-size:.75rem;color:var(--text-muted);line-height:1.6">
        循环变量：<code>{{arguments._loop.item}}</code> 当前元素、<code>{{arguments._loop.index}}</code> 轮次（从 0 开始）、
        <code>{{arguments._loop.key}}</code> 遍历对象时的键、<code>{{arguments._loop.last}}</code> 上一轮结果。
        循环体为子链时，在子链节点参数里用「固定配置」引用这些变量；为根链时，在本节点的参数配置里把它们映射到目标根链的入参。
        条件表达式里的字段路径要用方括号括起来，如 <code>[arguments._loop.index] &lt; 3</code>。
        需要固定执行 N 次时用条件循环，继续条件写 <code>[arguments._loop.index] &lt; N</code>。
        条件循环、配置了跳出条件时只能串行执行。下游节点可引用本节点的 items / count / success / failed / errors / break_index。
      </div>
      </div>
      <div class="modal-footer">
        <button class="btn btn-outline" onclick="closeOrchLoopEditor()">取消</button>
        <button class="btn btn-primary" onclick="confirmOrchLoopEditor()">确定</button>
      </div>
    </div>`;
    document.body.appendChild(overlay);
  }
  const cfg = getOrchLoopConfig(instanceId);
  document.getElementById('orch-loop-mode').value = cfg.mode || 'foreach';
  document.getElementById('orch-loop-body-type').value = cfg.body_type || 'subchain';
  document.getElementById('orch-loop-condition').value = cfg.condition || '';
  document.getElementById('orch-loop-break').value = cfg.break_condition || '';
  document.getElementById('orch-loop-max').value = cfg.max_iterations || 100;
  document.getElementById('orch-loop-concurrency').value = cfg.concurrency || 1;
  document.getElementById('orch-loop-interval').value = cfg.interval_ms || 0;
  document.getElementById('orch-loop-fail').value = cfg.fail_policy || 'stop';
  document.getElementById('orch-loop-body-search').value = '';
  window._orchLoopBodyCur = cfg.body_id || '';
  refreshOrchLoopEditorFields();
  overlay.classList.add('show');
  // 根链列表：只列出已发布、启用的根链，排除当前正在编辑的根链
  if (!window._orchLoopRootList) {
    try {
      const list = await api('/api/root-chains');
      window._orchLoopRootList = list || [];
    } catch (e) {
      window._orchLoopRootList = [];
      showToast('加载根链列表失败：' + (e.message || e), 'error');
    }
  }
  renderOrchLoopBodyOptions();
}

// 按循环模式显示对应的输入项
function refreshOrchLoopEditorFields() {
  const mode = (document.getElementById('orch-loop-mode') || {}).value || 'foreach';
  document.getElementById('orch-loop-range-wrap').style.display = mode === 'foreach' ? 'block' : 'none';
  document.getElementById('orch-loop-cond-wrap').style.display = mode === 'while' ? 'block' : 'none';
}

// 渲染循环体候选：子链取已启用的子链，根链取已发布的根链；都排除当前正在编辑的链
function renderOrchLoopBodyOptions() {
  const sel = document.getElementById('orch-loop-body');
  if (!sel) return;
  if (sel.value) window._orchLoopBodyCur = sel.value;
  const bodyType = document.getElementById('orch-loop-body-type').value;
  const q = (document.getElementById('orch-loop-body-search').value || '').trim().toLowerCase();
  const selfId = ((document.getElementById('orch-chain-id') || {}).value || '').trim();
  let list;
  if (bodyType === 'rootchain') {
    list = (window._orchLoopRootList || []).filter(c => c && c.has_releases && c.status !== 0 && c.chain_id !== selfId);
    document.getElementById('orch-loop-body-tip').textContent = '只列出已发布的根链；每轮调用目标根链的当前发布版本，取其返回值作为本轮结果。';
  } else {
    list = (_orchSubChains || []).filter(s => s && s.chain_id !== selfId);
    document.getElementById('orch-loop-body-tip').textContent = '每轮把当前执行上下文交给子链执行，取子链的返回值（或结束节点的返回值）作为本轮结果；子链只支持单出口。与调用子链一样，保存本链时嵌入子链当前内容，子链修改后需重新保存本链才会生效。';
  }
  const items = list.filter(c => !q || String(c.name || '').toLowerCase().includes(q) || String(c.chain_id || '').toLowerCase().includes(q));
  const cur = window._orchLoopBodyCur || '';
  if (!items.length) {
    sel.innerHTML = `<option value="">— 没有可选的${bodyType === 'rootchain' ? '已发布根链' : '子链'} —</option>`;
    return;
  }
  sel.innerHTML = items.map(c =>
    `<option value="${esc(c.chain_id)}"${c.chain_id === cur ? ' selected' : ''}>${esc(c.name || c.chain_id)}（${esc(c.chain_id)}）</option>`).join('');
}

function closeOrchLoopEditor() {
  const overlay = document.getElementById('orch-loop-modal-overlay');
  if (overlay) overlay.classList.remove('show');
  _orchLoopEditInstId = '';
}

// 读取并校验循环配置（与后端 LoopConfig.normalize 一致）；不通过时提示并返回 null
function collectOrchLoopEditor() {
  const v = id => (document.getElementById(id).value || '').trim();
  const cfg = {
    mode: v('orch-loop-mode') || 'foreach',
    condition: v('orch-loop-condition'),
    break_condition: v('orch-loop-break'),
    body_type: v('orch-loop-body-type') || 'subchain',
    body_id: v('orch-loop-body'),
    max_iterations: parseInt(v('orch-loop-max'), 10) || 100,
    concurrency: parseInt(v('orch-loop-concurrency'), 10) || 1,
    interval_ms: parseInt(v('orch-loop-interval'), 10) || 0,
    fail_policy: v('orch-loop-fail') || 'stop',
  };
  // 只保留当前模式用到的字段，避免切换模式后残留旧配置
  if (cfg.mode !== 'while') cfg.condition = '';
  const err = msg => { showToast(msg, 'error'); return null; };
  if (cfg.mode === 'while' && !cfg.condition) return err('请填写继续条件');
  if (!cfg.body_id) return err('请选择循环体');
  if (cfg.max_iterations < 1 || cfg.max_iterations > 1000) return err('最大循环次数需在 1~1000 之间');
  if (cfg.concurrency < 1 || cfg.concurrency > 20) return err('并发数需在 1~20 之间');
  if (cfg.concurrency > 1 && cfg.mode === 'while') return err('条件循环只能串行执行（并发数为 1）');
  if (cfg.concurrency > 1 && cfg.break_condition) return err('配置了跳出条件时只能串行执行（并发数为 1）');
  if (cfg.interval_ms < 0 || cfg.interval_ms > 60000) return err('每轮间隔需在 0~60000 毫秒之间');
  return cfg;
}

// 「循环配置」弹框确定：重置参数——遍历模式保留 loop_items（旧配置里写在循环配置的数据来源迁移过来），
// 循环体为根链时按目标根链入参重置（保留同名参数已配置的来源），为子链时没有入参
async function confirmOrchLoopEditor() {
  const instanceId = _orchLoopEditInstId;
  if (!instanceId) { closeOrchLoopEditor(); return; }
  const cfg = collectOrchLoopEditor();
  if (!cfg) return;
  let sig = null;
  if (cfg.body_type === 'rootchain') {
    sig = await fetchOrchCallSig(cfg.body_id);
    if (!sig) return;
  }
  storeParamPreset(); // 先把参数配置区当前的编辑结果写回暂存
  const oldPreset = _orchParamPreset[instanceId] || {};
  const oldCfg = getOrchLoopConfig(instanceId);
  const preset = {};
  if (cfg.mode === 'foreach') {
    const legacyRange = String(oldCfg.range || '').trim();
    preset[ORCH_LOOP_ITEMS_KEY] = oldPreset[ORCH_LOOP_ITEMS_KEY]
      || { src: legacyRange ? PARAM_SRC_FIXED : PARAM_SRC_UPSTREAM, value: legacyRange, type: 'slice' };
  }
  if (sig) {
    (sig.arguments || []).filter(k => k && k !== ORCH_LOOP_ITEMS_KEY).forEach(k => {
      preset[k] = oldPreset[k] || { src: PARAM_SRC_FIXED, value: '', type: 'string' };
    });
  }
  _orchParamPreset[instanceId] = preset;
  setOrchLoopConfig(instanceId, cfg);
  closeOrchLoopEditor();
  renderOrchNodeSelected();
  onOrchSelectionChange();
}
