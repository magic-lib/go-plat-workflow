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
  const p = getProject();
  document.getElementById('project-badge').textContent = p || '-';
  // 持久化当前选择到 localStorage，刷新页面后自动恢复
  try { localStorage.setItem('wf_selected_project', p || ''); } catch (_) {}
  updateProjectPermission();
  if (!p) return;
  // 刷新当前 tab 数据
  const activeTab = document.querySelector('.tab-btn.active');
  if (activeTab) {
    const tab = activeTab.dataset.tab;
    if (tab === 'nodes') loadNodeEnvOptions();
    else if (tab === 'activities') { loadActivityEnvOptions(); }
    else if (tab === 'sub-chains') loadSubChains();
    else if (tab === 'root-chains') loadRootChains();
    refreshExecEnvs();
  }
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
    // 恢复之前选中值：优先 localStorage 中保存的项目，其次当前下拉值，最后回退到第一项
    let saved = '';
    try { saved = localStorage.getItem('wf_selected_project') || ''; } catch (_) {}
    if (saved && list.some(p => p.project === saved)) {
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
      tbody.innerHTML = '<tr><td colspan="6"><div class="empty-state"><p>暂无环境配置</p></div></td></tr>';
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
        <td class="actions">
          <button class="btn btn-sm btn-outline" onclick="editEnvConfigByIndex(${i})">编辑</button>
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
  const redisAddr = document.getElementById('env-redis-addr').value.trim();
  const redis = redisAddr ? {
    addr: redisAddr,
    password: document.getElementById('env-redis-pwd').value,
    db: parseInt(document.getElementById('env-redis-db').value) || 0,
    username: document.getElementById('env-redis-user').value.trim(),
  } : null;
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
  const body = {
    project: p,
    env_name: envName,
    description: document.getElementById('env-description').value.trim(),
    env_vars: vars,
    redis_config: redis,
    mysql_config: mysql,
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

// ============================================================
// Table filter
// ============================================================
function filterTable(tableId, q) {
  const rows = document.querySelectorAll('#' + tableId + ' tbody tr');
  const lq = q.toLowerCase();
  rows.forEach(r => r.style.display = r.textContent.toLowerCase().includes(lq) ? '' : 'none');
}

function filterNodes() {
  const searchEl = document.querySelector('#tab-nodes .search-box');
  const kindEl = document.getElementById('node-kind-filter');
  const tagEl = document.getElementById('node-tag-filter');
  const q = (searchEl ? searchEl.value : '').toLowerCase();
  const kind = kindEl ? kindEl.value : '';
  const tag = tagEl ? tagEl.value : '';
  const rows = document.querySelectorAll('#nodes-table tbody tr');
  rows.forEach(r => {
    let show = true;
    if (q && !r.textContent.toLowerCase().includes(q)) show = false;
    if (show && kind) {
      // 第4列是"大类"列（0-based index 3），badge 文本为"查询获取"或"策略执行"
      const kindCell = r.cells[3];
      if (kindCell) {
        const kindText = kindCell.textContent.trim();
        if (kind === 'condition' && kindText !== '查询获取') show = false;
        if (kind === 'action' && kindText !== '策略执行') show = false;
      }
    }
    if (show && tag) {
      // 第6列是"标签"列（0-based index 5），取所有 chip 文本
      const tagCell = r.cells[5];
      const has = tagCell && Array.prototype.some.call(tagCell.querySelectorAll('.tag-chip'), c => c.textContent.replace(' ✓','').trim() === tag);
      if (!has) show = false;
    }
    r.style.display = show ? '' : 'none';
  });
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
  loadNodes();
}

// 初始化 Nodes 环境筛选下拉（复用环境配置列表），恢复上次选择，并加载列表
async function loadNodeEnvOptions() {
  const sel = document.getElementById('node-env-filter');
  if (!sel) return;
  try {
    const envs = await api('/api/env-configs');
    const savedEnv = (() => { try { return localStorage.getItem('wf_node_env') || ''; } catch (_) { return ''; } })();
    const list = Array.isArray(envs) ? envs : [];
    sel.innerHTML = '<option value="">全部环境</option>' + list.map(e =>
      '<option value="' + esc(e.env_name || e.name || e) + '">' + esc(e.env_name || e.name || e) + '</option>'
    ).join('');
    sel.value = savedEnv;
  } catch (e) { /* 环境列表可选，失败不影响节点列表 */ }
  loadNodes();
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
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
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
    if (!nodes.length) {
      tbody.innerHTML = '<tr><td colspan="11"><div class="empty-state"><div class="icon">📦</div><p>项目 <b>' + esc(getProject()) + '</b> 暂无节点数据</p></div></td></tr>';
      return;
    }
    window._nodesForEdit = nodes; // 缓存供编辑按钮索引使用
    refreshNodeNamespaceFilter(); // 同步命名空间过滤下拉（基于当前项目 Activity 缓存）
    refreshNodeTagFilter(); // 同步标签过滤下拉（基于当前列表已有标签）
    tbody.innerHTML = nodes.map((n, i) => `
      <tr>
        <td class="code-cell" title="${esc(n.node_id)}">${esc(n.node_id)}</td>
        <td>${esc(n.name)}<span style="margin-left:4px">${nodeHeartbeatIconHtml(n.node_heartbeats)}</span></td>
        <td><span class="code-cell">${esc(n.type)}</span></td>
        <td><span class="badge ${n.kind==='condition'?'badge-warning':'badge-info'}">${n.kind==='condition'?'查询获取':'策略执行'}</span></td>
        <td>${esc(n.category || '-')}</td>
        <td>${renderTagChipsHtml(n.tags)}</td>
        <td>${esc(n.namespace || '-')}</td>
        <td><span class="badge ${n.status==1?'badge-on':'badge-off'}">${n.status==1?'启用':'禁用'}</span></td>
        <td>${esc(n.version || '-')}</td>
        <td title="${esc(n.description||'')}">${esc(trunc(n.description, 30))}</td>
        <td class="actions">
          <button class="btn btn-sm btn-outline edit-only" onclick="editNodeByIndex(${i})">编辑</button>
          <button class="btn btn-sm btn-primary" onclick="openTestNodeModal('${esc(n.node_id)}')">测试</button>
          <button class="btn btn-sm btn-outline" onclick="openNodeLogModal('${esc(n.node_id)}')">日志</button>
          <button class="btn btn-sm btn-danger edit-only" onclick="deleteNode('${esc(n.node_id)}')">删除</button>
        </td>
      </tr>`).join('');
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
  document.getElementById('node-status').value = node ? String(node.status) : '1';
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
function onNodeTypeChange() {
  const type = document.getElementById('node-type').value;
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
  // condSwitch：显示条件串输入，并回填已有条件
  const isCond = type === 'custom/CondSwitch';
  document.getElementById('node-cond-section').style.display = isCond ? '' : 'none';
  if (isCond) {
    let cfg = {};
    try { cfg = JSON.parse(document.getElementById('node-configuration').value || '{}'); } catch(e) { cfg = {}; }
    const cond = (cfg.node_config && cfg.node_config.condition) || '';
    document.getElementById('node-condition').value = cond;
    syncCondConfig();
  }
}

// 将条件串输入框的内容写入 Configuration（CommConfiguration 结构：node_config.condition 等）
function syncCondConfig() {
  if (document.getElementById('node-type').value !== 'custom/CondSwitch') return;
  let cfg;
  try { cfg = JSON.parse(document.getElementById('node-configuration').value || '{}'); } catch(e) { cfg = {}; }
  if (typeof cfg !== 'object' || cfg === null) cfg = {};
  if (!cfg.node_config || typeof cfg.node_config !== 'object') cfg.node_config = {};
  cfg.node_config.condition = document.getElementById('node-condition').value;
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
      acts.push({
        act_namespace: row.getAttribute('data-ns') || '',
        act_name: row.getAttribute('data-name') || '',
        id: row.getAttribute('data-id') || '',
        name: row.getAttribute('data-display-name') || (row.getAttribute('data-ns') + '/' + row.getAttribute('data-name')),
        args: collectActivityArgBinds(row)
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
        args: it.args || (it.arguments ? convertArgumentsToArgs(it.arguments) : {})
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
        args: it.args || (it.arguments ? convertArgumentsToArgs(it.arguments) : {})
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
        args: it.args || (it.arguments ? convertArgumentsToArgs(it.arguments) : {})
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
    args[k] = { source: src, value: val, ref: ref };
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
      '<button type="button" class="btn btn-sm btn-outline" style="margin-left:auto" onclick="removeActivityStage(this)" title="删除此阶段">删除阶段</button>' +
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
  syncActivityConfig();
}

// 重新编号阶段序号
function refreshStageIndex() {
  document.querySelectorAll('#node-activity-stages .stage').forEach((stage, i) => {
    const idx = stage.querySelector('.stage-index');
    if (idx) idx.textContent = 'S' + (i + 1);
  });
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
      return {
        act_namespace: it.act_namespace,
        act_name: it.act_name,
        id: it.id || it.act_name,
        arguments: argumentsArr
      };
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
  // activity 类型：校验 activity 编排，并写入 Configuration
  if (body.type === 'custom/Activity') {
    if (typeof body.configuration !== 'object' || body.configuration === null) body.configuration = {};
    if (!body.configuration.node_config || typeof body.configuration.node_config !== 'object') body.configuration.node_config = {};
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
        return {
          act_namespace: it.act_namespace,
          act_name: it.act_name,
          id: it.id || it.act_name,
          arguments: argumentsArr
        };
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

function editNodeByIndex(i) { openNodeModal(window._nodesForEdit[i]); }

async function deleteNode(id) {
  if (!confirm('确定删除节点 ' + id + ' 吗？')) return;
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
    _actPage = 1; // 重新加载后回到第一页
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

// 初始化 Activities 环境筛选下拉（复用环境配置列表），恢复上次选择的环境，并加载列表
async function loadActivityEnvOptions() {
  const sel = document.getElementById('act-env-filter');
  if (!sel) return;
  try {
    const envs = await api('/api/env-configs');
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
const _actPageSize = 50;

// 环境筛选变化：持久化选择 + 重新加载列表
function onActivityEnvFilterChange() {
  try { localStorage.setItem('wf_activity_env', document.getElementById('act-env-filter').value); } catch (_) {}
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
    tbody.innerHTML = '<tr><td colspan="8" class="empty-state"><div class="icon">📋</div>暂无 activity 记录</td></tr>';
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
    const tagsHtml = (a.tags || []).map(t => '<span class="badge ' + tagColorClass(t) + '" style="margin-right:4px">' + escHtml(t) + '</span>').join('') || '<span style="color:var(--text-muted)">-</span>';
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
      <td><span class="code-cell">${escHtml(a.activity_id)}</span></td>
      <td>${testIcon}${escHtml(a.name)}<span style="margin-left:4px">${hasActivityEnvSelected ? heartbeatIconHtml(a.heartbeat) : ''}</span></td>
      <td>${tagsHtml}</td>
      <td><span class="code-cell">${escHtml(a.act_namespace)}</span></td>
      <td><span class="code-cell">${escHtml(a.act_name)}</span></td>
      <td class="arg-count" data-args="${argJson}" ${argCount ? '' : 'data-empty="1"'}>${argCount || '0'}</td>
      <td>${escHtml(a.created_at ? a.created_at.substring(0,10) : '-')}</td>
      <td><div class="actions">
        <button class="btn btn-sm btn-outline edit-only" onclick="editActivity('${escHtml(a.activity_id)}')">编辑</button>
        <button class="btn btn-sm btn-primary" onclick="openTestActivityModal('${escHtml(a.activity_id)}')">测试</button>
        <button class="btn btn-sm btn-outline" onclick="showActivityListenerCode('${escHtml(a.activity_id)}')">监听代码</button>
        <button class="btn btn-sm btn-outline" onclick="openActivityLogsModal('${escHtml(a.activity_id)}', '${escHtml(a.act_name)}')">日志</button>
        <button class="btn btn-sm btn-danger edit-only" onclick="deleteActivityById('${escHtml(a.activity_id)}')">删除</button>
      </div></td>
    </tr>`;
  }).join('');
  renderActivityPager(all.length);
}

// 渲染 Activities 分页器；总数不超过一页时隐藏
function renderActivityPager(total) {
  const pager = document.getElementById('act-pager');
  if (!pager) return;
  const totalPages = Math.max(1, Math.ceil(total / _actPageSize));
  if (total <= _actPageSize) {
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
function editActivity(activityID) {
  const a = window._activityCache.find(x => x.activity_id === activityID);
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
  if (!confirm('确定删除 activity ' + activityID + ' 吗？')) return;
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
            <button class="btn btn-sm btn-danger" onclick="deleteNodeTestRecord('${esc(r.record_id)}','${esc(nodeId)}')">删除</button>
          </span>
        </div>
        <div style="color:var(--text-muted);margin:4px 0">${esc(r.created_at || '')}${(r.duration_ms !== undefined && r.duration_ms !== null)?((' · 耗时: ' + formatDuration(r.duration_ms))):''}${r.trace_id?(' · trace_id: <code style="font-size:.72rem">'+esc(r.trace_id)+'</code>'):''}</div>
        <div><b>入参:</b> <code style="font-size:.72rem;white-space:pre-wrap;word-break:break-all;overflow-wrap:break-word;display:block">${esc(trunc(r.input_params||'',200))}</code></div>
        <div><b>结果:</b> <code style="font-size:.72rem;white-space:pre-wrap;word-break:break-all;overflow-wrap:break-word;display:block">${esc(trunc(r.result||r.error_msg||'',200))}</code></div>
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
    (r.env ? '<span class="log-meta">env=' + esc(r.env) + '</span>' : ''),
    (r.trace_id ? '<span class="log-meta">trace_id=' + esc(r.trace_id) + '</span>' : ''),
    (r.relation_type ? '<span class="log-meta log-relation">relation=' + esc(r.relation_type) + '</span>' : ''),
  ].join(' ');
  // request/fail/start 展示入参（取 payload 内的 arguments 字段），response 展示返回值（result）
  let body = '';
  const showPayload = (r.event_id === 'request' || r.event_id === 'fail' || r.event_id === 'start');
  if (showPayload) {
    // 入参：从 payload.arguments 中取（payload 可能是字符串或对象）
    let args = null;
    try {
      const p = (typeof r.payload === 'string') ? JSON.parse(r.payload) : r.payload;
      if (p && typeof p === 'object' && p.arguments !== undefined) {
        args = p.arguments;
      } else if (p && typeof p === 'object') {
        args = p; // 兼容没有 arguments 包裹的情况
      }
    } catch (e) { args = r.payload; }
    if (args !== null && args !== undefined && !(typeof args === 'object' && Object.keys(args).length === 0)) {
      let pretty = args;
      try { pretty = JSON.stringify(typeof args === 'string' ? JSON.parse(args) : args, null, 2); } catch (e) {}
      body = '<div class="log-json"><div class="log-json-label">参数 (arguments)</div><pre>' + esc(pretty) + '</pre></div>';
    }
  } else {
    const raw = r.result;
    if (raw) {
      let pretty = raw;
      try { pretty = JSON.stringify(JSON.parse(typeof raw === 'string' ? raw : JSON.stringify(raw)), null, 2); } catch (e) {}
      body = '<div class="log-json"><div class="log-json-label">返回值 (result)</div><pre>' + esc(pretty) + '</pre></div>';
    }
  }
  if (r.error_msg) {
    body += '<div class="log-error-msg">错误: ' + esc(r.error_msg) + '</div>';
  }
  return '<div class="log-item ' + level + '"><div class="log-head">' + head + '</div>' + body + '</div>';
}

// 普通 node 运行日志列表项：在 renderNodeLogItem 基础上附加"查看该 node 下 Activities"下钻按钮。
// 点击后用本项自身的 trace_id + node_span_id（即 node 日志的 span_id）查询该 node 关联的 activity 日志并内联展开。
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
  if (r.event_id) left.push('<span class="log-meta">event_id=' + esc(r.event_id) + '</span>');
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
  if (payloadStr) segs.push('<div class="log-json"><span class="k">payload:</span> ' + esc(trunc(payloadStr, 800)) + '</div>');
  if (resultStr) segs.push('<div class="log-json"><span class="k">result:</span> ' + esc(trunc(resultStr, 800)) + '</div>');
  const attrStr = toStr(r.attributes);
  if (attrStr) segs.push('<div class="log-json" style="background:#1e293b;color:#cbd5e1"><span class="k">attributes:</span> ' + esc(trunc(attrStr, 800)) + '</div>');
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

// 打开日志页：初始化环境下拉后加载最新日志
async function openAllLogsTab() {
  await loadAllLogEnvOptions();
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
  if (lv) params.set('level', lv);
  if (env) params.set('env', env);
  if (trace) params.set('trace_id', trace);
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
  if (lv) lv.value = '';
  if (env) env.value = '';
  if (trace) trace.value = '';
  _allLogPage = 1;
  loadAllLogs();
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
        <div><b>入参:</b> <code style="font-size:.72rem;white-space:pre-wrap;word-break:break-all;overflow-wrap:break-word;display:block">${esc(trunc(r.input_params||'',200))}</code></div>
        <div><b>结果:</b> <code style="font-size:.72rem;white-space:pre-wrap;word-break:break-all;overflow-wrap:break-word;display:block">${esc(trunc(r.result||r.error_msg||'',200))}</code></div>
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
      <option value="string" ${type==='string'?'selected':''}>string</option>
      <option value="int64" ${type==='int64'?'selected':''}>int64</option>
      <option value="float64" ${type==='float64'?'selected':''}>float64</option>
      <option value="bool" ${type==='bool'?'selected':''}>bool</option>
      <option value="slice" ${type==='slice'?'selected':''}>slice</option>
      <option value="map" ${type==='map'?'selected':''}>map</option>
      <option value="formula" ${type==='formula'?'selected':''}>formula</option>
    </select>
    <label class="param-required"><input type="checkbox" ${reqChecked}>必填</label>
    <select class="param-policy">${policyHTML}</select>
    <input class="param-default" placeholder="默认值" value="${esc(defaultValue||'')}">
    <input class="param-desc" placeholder="描述" value="${esc(description||'')}">
    <button class="btn-remove-param" onclick="removeParamRow('param-row-${paramSeq}')" title="删除此参数">&times;</button>
  `;
  container.appendChild(row);
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
    <span class="output-input-wrap" style="flex:1;display:flex;gap:6px;align-items:center;min-width:0"></span>
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
      // return_value 的 key 为空表示返回活动返回的全部内容：选中后等价「返回值整体」，
      // 选项 value 置空，拼路径时回退为 {{id.responses}}（与返回全部数据完全一致）
      const val = rvKey === '' ? '' : rvName;
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
    '<input class="output-value" placeholder="配置值/表达式" value="' + esc(value) + '" style="flex:1 1 110px;min-width:0;padding:4px 6px;border:1px solid var(--border);border-radius:6px;font-size:.78rem;font-family:monospace">';
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
      <option value="string" ${type==='string'?'selected':''}>string</option>
      <option value="int64" ${type==='int64'?'selected':''}>int64</option>
      <option value="float64" ${type==='float64'?'selected':''}>float64</option>
      <option value="bool" ${type==='bool'?'selected':''}>bool</option>
      <option value="slice" ${type==='slice'?'selected':''}>slice</option>
      <option value="map" ${type==='map'?'selected':''}>map</option>
      <option value="formula" ${type==='formula'?'selected':''}>formula</option>
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

  const typeOptions = ['', 'string', 'int64', 'float64', 'bool', 'formula', 'json'];
  const typeLabels = { '': '不转换', 'string': 'string', 'int64': 'int64', 'float64': 'float64', 'bool': 'bool', 'formula': 'formula', 'json': 'json' };
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
// Sub Chains
// ============================================================
async function loadSubChains() {
  try {
    const chains = await api('/api/sub-chains');
    const tbody = document.querySelector('#sub-table tbody');
    if (!chains.length) {
      tbody.innerHTML = '<tr><td colspan="6"><div class="empty-state"><div class="icon">🔗</div><p>项目 <b>' + esc(getProject()) + '</b> 暂无子链数据</p></div></td></tr>';
      return;
    }
    window._subChainsForEdit = chains; // 缓存供编辑按钮索引使用
    tbody.innerHTML = chains.map((c, i) => {
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
          <button class="btn btn-sm btn-primary" onclick="executeSubChain('${esc(c.chain_id)}')">Execute</button>
          <button class="btn btn-sm btn-outline edit-only" onclick="orchOpenInPage('${esc(c.chain_id)}')">编排</button>
          <button class="btn btn-sm btn-outline edit-only" onclick="editSubChainByIndex(${i})">编辑</button>
          <button class="btn btn-sm btn-danger edit-only" onclick="deleteSubChain('${esc(c.chain_id)}')">删除</button>
        </td>
      </tr>`;
    }).join('');
  } catch (e) { showToast('加载子链失败: ' + e.message, 'error'); }
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
      await api('/api/sub-chains/' + encodeURIComponent(body.chain_id), { method: 'PUT', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
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

async function executeSubChain(chainId) {
  const payload = prompt('输入子链执行参数 (JSON)：', '{"input": "hello world"}');
  if (payload === null) return;
  // 打开 Execute 模态框展示结果
  openExecuteModal();
  const resultBox = document.getElementById('exec-result');
  resultBox.textContent = '子链 ' + chainId + ' 执行中...';
  try {
    const data = await api('/api/sub-chains/' + encodeURIComponent(chainId) + '/execute', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({ payload: payload }),
    });
    resultBox.textContent = JSON.stringify(data, null, 2);
    showToast('子链执行成功', 'success');
  } catch (e) {
    resultBox.textContent = 'ERROR: ' + e.message;
    showToast('子链执行失败: ' + e.message, 'error');
  }
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
    const tbody = document.querySelector('#root-table tbody');
    if (!chains.length) {
      tbody.innerHTML = '<tr><td colspan="10"><div class="empty-state"><div class="icon">🌳</div><p>项目 <b>' + esc(getProject()) + '</b> 暂无根链数据，通过 Execute 页面执行工作流后会自动生成</p></div></td></tr>';
      return;
    }
    window._rootChainsForEdit = chains; // 缓存供按钮索引使用
    tbody.innerHTML = chains.map((c, i) => {
      let connCount = 0;
      try { connCount = JSON.parse(c.connections_data || '[]').length; } catch(e){}
      const curVer = curVerMap[c.chain_id];
      return `
      <tr>
        <td class="code-cell" title="${esc(c.chain_id)}">${esc(c.chain_id)}</td>
        <td class="code-cell" title="${esc(c.chain_key||'')}">${esc(c.chain_key||'')}</td>
        <td>${esc(c.name)}</td>
        <td title="${esc(c.description||'')}">${esc(trunc(c.description, 40))}</td>
        <td><span class="badge ${c.status==1?'badge-on':'badge-off'}">${c.status==1?'启用':'禁用'}</span></td>
        <td class="code-cell" title="${esc(c.node_ids||'')}">${esc(trunc(c.node_ids, 20))}</td>
        <td class="code-cell" title="${esc(c.sub_chain_ids||'')}">${esc(trunc(c.sub_chain_ids, 20))}</td>
        <td>${connCount} 条</td>
        <td>${curVer ? '<span class="badge badge-info">v' + curVer + '</span>' : '<span style="color:var(--text-muted);font-size:.8rem">未发布</span>'}</td>
        <td class="actions">
          <button class="btn btn-sm btn-outline edit-only" onclick="orchOpenInPageRoot('${esc(c.chain_id)}')">编排</button>
          <button class="btn btn-sm btn-primary edit-only" onclick="publishRootChain('${esc(c.chain_id)}')">发布</button>
          <button class="btn btn-sm btn-outline" onclick="openReleaseModal('${esc(c.chain_id)}')">记录</button>
          <button class="btn btn-sm btn-outline" onclick="showFlowchartByIndex(${i})">流程图</button>
          <button class="btn btn-sm btn-outline" onclick="loadToExecuteByIndex(${i})">Execute</button>
          <button class="btn btn-sm btn-danger edit-only" onclick="deleteRootChain('${esc(c.chain_id)}')">删除</button>
        </td>
      </tr>`;
    }).join('');
  } catch (e) { showToast('加载根链失败: ' + e.message, 'error'); }
}

async function deleteRootChain(id) {
  if (!confirm('确定删除根链 ' + id + ' 吗？发布记录会保留。')) return;
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
  try {
    const releases = await api('/api/root-chains/' + encodeURIComponent(_releaseChainId) + '/releases');
    if (!releases.length) {
      tbody.innerHTML = '<tr><td colspan="6"><div class="empty-state"><div class="icon">🚀</div><p>暂无发布记录，点击行内"发布"按钮创建第一个版本</p></div></td></tr>';
      return;
    }
    window._releasesCache = {}; // 先清空旧缓存，避免历史版本残留
    tbody.innerHTML = releases.map(r => {
      // 缓存该版本完整记录（含 dsl_json），Execute 时直接使用，避免再次请求加载更多内容到内存
      if (window._releasesCache) window._releasesCache[r.version] = r;
      return `
      <tr>
        <td><b>v${r.version}</b></td>
        <td>${esc(r.name)}</td>
        <td style="white-space:nowrap">${esc(fmtTime(r.published_at))}</td>
        <td>${(r.dsl_json||'').length} chars</td>
        <td>${r.is_current ? '<span class="badge badge-on">当前生效</span>' : '<span class="badge badge-off">历史</span>'}</td>
        <td class="actions">
          <button class="btn btn-sm btn-outline" onclick="viewReleaseDsl(${r.version})">查看 DSL</button>
          <button class="btn btn-sm btn-primary" onclick="executeRelease(${r.version})">Execute</button>
          ${r.is_current
            ? ''
            : `<button class="btn btn-sm btn-primary" onclick="setCurrentRelease(${r.version})">设为生效</button>
               <button class="btn btn-sm btn-danger" onclick="deleteReleaseVersion(${r.version})">删除</button>`}
        </td>
      </tr>`;
    }).join('');
  } catch (e) {
    tbody.innerHTML = '<tr><td colspan="6" style="color:#ef4444">加载失败: ' + esc(e.message) + '</td></tr>';
  }
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
  if (!confirm('确定删除发布版本 v' + version + ' 吗？\n此操作不可恢复（当前生效版本不允许删除）。')) return;
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

function loadToExecuteByIndex(i) { loadToExecute(window._rootChainsForEdit[i]); }
function showFlowchartByIndex(i) { showFlowchart(window._rootChainsForEdit[i]); }
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



function loadToExecute(c) {
  // Execute 已改为独立模态框，由 Root Chains / SubChains 列表的 Execute 按钮打开
  openExecuteModal();

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
  showToast('已加载根链: ' + esc(c.chain_id), 'success');
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
    if (window._currentTestCase) {
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
  try {
    const data = await api('/api/workflow/execute', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
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
  sel.innerHTML = '<option value="">-- 选择环境 --</option>';
  if (!p) return;
  try {
    const envs = await api('/api/env-configs');
    const seenEnv = {};
    (envs || []).forEach(e => {
      if (!e || !e.env_name) return;
      // 按 env_name 去重：仅 trim 前后空格，保留大小写区分（test 与 Test 视为不同环境）
      const name = String(e.env_name).trim();
      if (!name || seenEnv[name]) return;
      seenEnv[name] = true;
      const opt = document.createElement('option');
      opt.value = name;
      opt.textContent = name + (e.description ? ' (' + e.description + ')' : '');
      sel.appendChild(opt);
    });
    if (cur) sel.value = cur;
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
        <button class="btn btn-sm btn-danger" onclick="deleteTestCase('${esc(c.case_id)}')" title="删除用例">✕</button>
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

function buildMermaidSyntax(chain, nameMap) {
  nameMap = nameMap || {};
  const nodeIds = (chain.node_ids || '').split(',').map(s => s.trim()).filter(Boolean);
  const subIds = (chain.sub_chain_ids || '').split(',').map(s => s.trim()).filter(Boolean);
  let conns = [];
  try { conns = JSON.parse(chain.connections_data || '[]'); } catch(e) {}

  const nodeSet = new Set(nodeIds);
  const subSet = new Set(subIds);

  // 收集所有出现在 connections 中的 ID
  const allIds = new Set();
  conns.forEach(c => { allIds.add(c.from_id); allIds.add(c.to_id); });
  nodeIds.forEach(id => allIds.add(id));
  subIds.forEach(id => allIds.add(id));

  // 对 ID 做 Mermaid 安全处理：将特殊字符替换为下划线，同时记录映射
  const idMap = {};
  const revMap = {};
  let counter = 0;
  allIds.forEach(id => {
    let safe = id.replace(/[^a-zA-Z0-9_\u4e00-\u9fff]/g, '_');
    if (!safe || /^\d/.test(safe)) safe = 'id' + safe;
    // 确保唯一
    while (Object.values(idMap).includes(safe)) safe = safe + '_' + (++counter);
    idMap[id] = safe;
    revMap[safe] = id;
  });

  let lines = ['flowchart TD'];

  // 添加节点定义（不同的形状区分 node 和 subchain），显示中文名 + NodeId + 实例Id（与编排 Live Preview 一致）
  function baseNodeId(id) {
    return id.indexOf('__') >= 0 ? id.split('__')[0] : id;
  }
  function nodeName(id) {
    if (nameMap[id]) return nameMap[id];
    return nameMap[baseNodeId(id)] || '';
  }
  nodeSet.forEach(id => {
    const safe = idMap[id];
    const cn = nodeName(id);
    const base = baseNodeId(id);
    const labelLine = cn ? `<b>⚙ ${esc(cn)}</b>` : `<b>⚙ ${esc(id)}</b>`;
    const idLine = `<small>NodeId: ${esc(base)}</small>`;
    const instLine = id !== base ? `<small>Id: ${esc(id)}</small>` : '';
    lines.push(`    ${safe}["${labelLine}<br/>${idLine}${instLine ? '<br/>' + instLine : ''}"]`);
  });
  subSet.forEach(id => {
    const safe = idMap[id];
    const cn = nodeName(id);
    const subLabel = cn ? id : '';
    lines.push(`    ${safe}(["<b>🔗 ${esc(cn || id)}</b>${subLabel ? '<br/><small>' + esc(subLabel) + '</small>' : ''}"])`);
  });
  // 未分类的 ID（只在 connections 中出现）
  allIds.forEach(id => {
    if (!nodeSet.has(id) && !subSet.has(id)) {
      const safe = idMap[id];
      const cn = nodeName(id);
      const base = baseNodeId(id);
      const labelLine = cn ? `<b>? ${esc(cn)}</b>` : `<b>? ${esc(id)}</b>`;
      const idLine = `<small>NodeId: ${esc(base)}</small>`;
      const instLine = id !== base ? `<small>Id: ${esc(id)}</small>` : '';
      lines.push(`    ${safe}["${labelLine}<br/>${idLine}${instLine ? '<br/>' + instLine : ''}"]`);
    }
  });

  // 添加连接关系
  conns.forEach(c => {
    const from = idMap[c.from_id];
    const to = idMap[c.to_id];
    if (from && to) {
      lines.push(`    ${from} -->|${c.type || 'Success'}| ${to}`);
    }
  });

  // 样式
  const nodeClasses = [...nodeSet].map(id => idMap[id]).filter(Boolean).join(',');
  const subClasses = [...subSet].map(id => idMap[id]).filter(Boolean).join(',');
  const otherClasses = [];
  allIds.forEach(id => { if (!nodeSet.has(id) && !subSet.has(id)) otherClasses.push(idMap[id]); });

  lines.push(`    classDef nodeCls fill:#e0e7ff,stroke:#4f46e5,color:#1e1b4b,stroke-width:2px`);
  lines.push(`    classDef subCls fill:#fef3c7,stroke:#d97706,color:#78350f,stroke-width:2px`);
  if (otherClasses.length) lines.push(`    classDef otherCls fill:#f3f4f6,stroke:#9ca3af,color:#374151,stroke-width:2px`);
  if (nodeClasses) lines.push(`    class ${nodeClasses} nodeCls`);
  if (subClasses) lines.push(`    class ${subClasses} subCls`);
  if (otherClasses.length) lines.push(`    class ${otherClasses.join(',')} otherCls`);

  return lines.join('\n');
}

async function showFlowchart(chain) {
  const title = document.getElementById('flowchart-modal-title');
  title.textContent = '流程图: ' + esc(chain.chain_id) + (chain.name ? ' - ' + esc(chain.name) : '');

  const container = document.getElementById('flowchart-mermaid');
  // 清除旧内容
  container.innerHTML = '';
  container.removeAttribute('data-processed');

  const nameMap = await ensureNameMaps();
  const syntax = buildMermaidSyntax(chain, nameMap);
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

async function loadOrchData() {
  try {
    _orchNodes = await api('/api/nodes?only_enabled=true');
    _orchSubChains = await api('/api/sub-chains?only_enabled=true');
    refreshOrchNodeNsFilter();
    refreshOrchNodeTagFilter();
    renderOrchNodeList();
    renderOrchSubList();
    refreshOrchConnOptions();
    onOrchChange();
  } catch(e) { showToast('加载编排数据失败: ' + e.message, 'error'); }
}

function renderOrchNodeList(filterText, filterNs, filterTag) {
  const q = (filterText||'').toLowerCase();
  const ns = (filterNs||'').trim();
  const tag = (filterTag||'').trim();
  _orchNodeListFiltered = (_orchNodes || []).filter(n => {
    if (ns && (n.namespace||'') !== ns) return false;
    if (tag && !((n.tags||[]).includes(tag))) return false;
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
    const kindClass = n.kind === 'condition' ? 'badge-warning' : 'badge-info';
    const kindLabel = n.kind === 'condition' ? '查询' : '执行';
    let outputsJson = '[]';
    try { outputsJson = JSON.stringify(n.outputs || []); } catch(e) {}
    const outCount = (function(){
      try { return (JSON.parse(outputsJson) || []).length; } catch(e){ return 0; }
    })();
    return `<label class="orch-select-item" title="${esc(n.node_id)} - ${esc(n.description||'')}">
      <span class="node-id">${esc(n.node_id)}</span>
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
  renderOrchNodeSelected();
  onOrchSelectionChange();
}

// 删除指定节点实例
function removeOrchNodeInstance(instanceId) {
  if (!window._orchNodeInstances) return;
  window._orchNodeInstances = window._orchNodeInstances.filter(i => i.instanceId !== instanceId);
  renderOrchNodeSelected();
  onOrchSelectionChange();
}

// 从 Live Preview 点击节点触发：二次确认后删除该实例（避免误删）
function removeOrchNodeInstanceConfirm(instanceId) {
  const inst = (window._orchNodeInstances || []).find(i => i.instanceId === instanceId);
  if (!inst) return;
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
        return `<tr title="${esc(i.instanceId)}">
          <td>${idx + 1}</td>
          <td>${esc(i.name)}</td>
          <td class="code-cell">${esc(i.nodeId)}</td>
          <td class="code-cell">${esc(i.instanceId)}</td>
          <td style="text-align:right"><button class="btn btn-sm btn-danger" type="button" onclick="removeOrchNodeInstance('${esc(i.instanceId)}')">删除</button></td>
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
    return {
      instanceId: raw,
      nodeId: baseId,
      name: n ? (n.name || baseId) : baseId,
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
  renderOrchNodeList(q, ns, tag);
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
  container.innerHTML = filtered.map(s =>
    `<label class="orch-select-item" title="${esc(s.chain_id)} - ${esc(s.description||'')}">
      <input type="checkbox" value="${esc(s.chain_id)}" data-kind="sub" onchange="onOrchSelectionChange()">
      <span class="node-id" style="color:#d97706">${esc(s.chain_id)}</span>
      <span class="node-name">${esc(s.name||s.chain_id)}</span>
      <span class="node-type-tag">sub</span>
    </label>`
  ).join('');
}

function filterOrchSubChains() {
  const q = document.querySelector('#orch-sub-list').previousElementSibling.value;
  renderOrchSubList(q);
}

function getSelectedOrchNodeIds() {
  // 返回节点实例 ID 列表（形如 nodeId__N，支持同一节点多次添加）
  return (window._orchNodeInstances || []).map(i => i.instanceId);
}

function getSelectedOrchSubIds() {
  const cbs = document.querySelectorAll('#orch-sub-list input[type="checkbox"]:checked');
  return [...cbs].map(cb => cb.value);
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
function refreshOrchConnOptions() {
  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();

  // Build options for each node (显示名称 + 类型，instanceId 全局唯一不再展示序号)
  const nodeOpts = nodeIds.map(id => {
    const inst = (window._orchNodeInstances || []).find(i => i.instanceId === id);
    const label = inst ? inst.name : id;
    const extra = inst ? (inst.nodeId + ' · ' + inst.type) : '';
    return `<option value="${esc(id)}">⚙ ${esc(label)}${extra ? ' ('+esc(extra)+')' : ''}</option>`;
  }).join('');

  const subOpts = subIds.map(id => {
    const s = _orchSubChains.find(x => x.chain_id === id);
    const label = s && s.name ? s.name : id;
    const extra = s && s.name ? id : '';
    return `<option value="${esc(id)}">🔗 ${esc(label)}${extra ? ' ('+esc(extra)+')' : ''}</option>`;
  }).join('');

  const allOpts = nodeOpts + subOpts;
  const hasSubs = subIds.length > 0;

  // Update all existing connection rows' select elements
  document.querySelectorAll('#orch-conn-container .orch-conn-row').forEach(row => {
    const fromSel = row.querySelector('[data-role="orch-from"]');
    const toSel = row.querySelector('[data-role="orch-to"]');
    if (fromSel) {
      const curVal = fromSel.value;
      fromSel.innerHTML = allOpts || '<option value="">-- 请选择 --</option>';
      if (curVal && [...getSelectedOrchNodeIds(), ...getSelectedOrchSubIds()].includes(curVal)) {
        fromSel.value = curVal;
      }
    }
    if (toSel) {
      const curVal = toSel.value;
      toSel.innerHTML = allOpts || '<option value="">-- 请选择 --</option>';
      if (curVal && [...getSelectedOrchNodeIds(), ...getSelectedOrchSubIds()].includes(curVal)) {
        toSel.value = curVal;
      }
    }
  });
}

function addOrchConnRow(fromId, toId, connType) {
  orchConnSeq++;
  const container = document.getElementById('orch-conn-container');
  const emptyEl = document.getElementById('orch-conn-empty');
  if (emptyEl) emptyEl.style.display = 'none';

  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();
  const nodeOpts = nodeIds.map(id => {
    // id 是实例 ID（nodeId__随机段），需按实例的 nodeId 查定义
    const inst = (window._orchNodeInstances || []).find(i => i.instanceId === id);
    const nodeId = inst ? inst.nodeId : id.split('__')[0];
    const n = _orchNodes.find(x => x.node_id === nodeId);
    const label = n && n.name ? n.name : id;
    const extra = n && n.name ? id + ' · ' + n.type : (n ? n.type : '');
    return `<option value="${esc(id)}">⚙ ${esc(label)}${extra ? ' ('+esc(extra)+')' : ''}</option>`;
  }).join('');
  const subOpts = subIds.map(id => {
    const s = _orchSubChains.find(x => x.chain_id === id);
    const label = s && s.name ? s.name : id;
    const extra = s && s.name ? id : '';
    return `<option value="${esc(id)}">🔗 ${esc(label)}${extra ? ' ('+esc(extra)+')' : ''}</option>`;
  }).join('');
  const allOpts = nodeOpts + subOpts;

  const row = document.createElement('div');
  row.className = 'orch-conn-row';
  row.id = 'orch-conn-row-' + orchConnSeq;
  row.innerHTML = `
    <select data-role="orch-from">${allOpts || '<option value="">-- 请选择 --</option>'}</select>
    <span class="conn-arrow">→</span>
    <select data-role="orch-to">${allOpts || '<option value="">-- 请选择 --</option>'}</select>
    <input type="text" data-role="orch-type" class="conn-type-sel" list="conn-type-datalist" placeholder="True" value="${esc(connType||'True')}" title="连接类型：Success / Failure / True / False / Stream 或自定义">
    <button class="btn-remove" onclick="removeOrchConnRow('orch-conn-row-${orchConnSeq}')" title="删除">&times;</button>
  `;
  container.appendChild(row);

  // Set initial values
  if (fromId) row.querySelector('[data-role="orch-from"]').value = fromId;
  if (toId) row.querySelector('[data-role="orch-to"]').value = toId;

  // Trigger preview update
  row.querySelector('[data-role="orch-from"]').addEventListener('change', onOrchChange);
  row.querySelector('[data-role="orch-to"]').addEventListener('change', onOrchChange);
  row.querySelector('[data-role="orch-type"]').addEventListener('input', onOrchChange);
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

// Build Mermaid syntax for orchestration preview
function orchBuildMermaid() {
  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();
  const conns = collectOrchConnections();

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
    const inst = (_orchNodeInstances || []).find(i => i.instanceId === id);
    const n = _orchNodes.find(x => x.node_id === id);
    const baseId = n ? n.node_id : (inst ? inst.nodeId : id);
    const cnName = (inst && inst.name) ? inst.name : (n && n.name ? n.name : id);
    const k = (n && n.kind) || 'action';
    const clsName = 'nodeKind_' + k.replace(/[^a-zA-Z0-9_]/g, '_');
    // 标签：第一行中文名，第二行 node ID，第三行 实例 ID（同一节点多次添加时区分）
    const labelLine = `<b>⚙ ${esc(cnName)}</b>`;
    const idLine = `<small>NodeId: ${esc(baseId)}</small>`;
    const instLine = inst && inst.instanceId !== baseId ? `<small>Id: ${esc(inst.instanceId)}</small>` : '';
    lines.push(`    ${safe}["${labelLine}<br/>${idLine}${instLine ? '<br/>'+instLine : ''}"]`);
    (kindSafeIds[k] = kindSafeIds[k] || []).push(safe);
  });
  subSet.forEach(id => {
    const safe = idMap[id];
    const s = _orchSubChains.find(x => x.chain_id === id);
    const label = s && s.name ? s.name : id;
    const subLabel = s && s.name ? id : '';
    lines.push(`    ${safe}(["<b>🔗 ${esc(label)}</b>${subLabel ? '<br/><small>'+esc(subLabel)+'</small>' : ''}"])`);
  });
  allIds.forEach(id => {
    if (!nodeSet.has(id) && !subSet.has(id)) {
      const safe = idMap[id];
      lines.push(`    ${safe}["<b>? ${esc(id)}</b>"]`);
    }
  });
  conns.forEach(c => {
    const from = idMap[c.from_id];
    const to = idMap[c.to_id];
    if (from && to) lines.push(`    ${from} -->|${c.type||'True'}| ${to}`);
  });
  const subClasses = [...subSet].map(id => idMap[id]).filter(Boolean).join(',');
  // 根据节点 kind 类型动态生成不同背景色（每种 kind 一组 classDef）
  const kindPalette = {
    condition: { fill: '#fef3c7', stroke: '#d97706', color: '#78350f' }, // 查询获取=黄
    action:    { fill: '#dbeafe', stroke: '#2563eb', color: '#1e3a8a' }, // 策略执行=蓝
  };
  const kindFallback = { fill: '#e0e7ff', stroke: '#4f46e5', color: '#1e1b4b' }; // 未知 kind 默认靛蓝
  Object.keys(kindSafeIds).forEach(k => {
    const c = kindPalette[k] || kindFallback;
    const clsName = 'nodeKind_' + k.replace(/[^a-zA-Z0-9_]/g, '_');
    lines.push(`    classDef ${clsName} fill:${c.fill},stroke:${c.stroke},color:${c.color},stroke-width:2px,text-align:left`);
    lines.push(`    class ${kindSafeIds[k].join(',')} ${clsName}`);
  });
  lines.push('    classDef subCls fill:#ede9fe,stroke:#7c3aed,color:#4c1d95,stroke-width:2px,text-align:left');
  if (subClasses) lines.push(`    class ${subClasses} subCls`);
  // 暴露 id 映射供预览图叠加删除叉时反向查询原始实例 ID
  window._orchIdMap = idMap;
  return lines.join('\n');
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
    return { id, type: n.type, name: n.name, configuration: config };
  });

  const subNodes = subIds.map(id => {
    const s = _orchSubChains.find(x => x.chain_id === id);
    // Flow Node 的 ID 直接使用子链自身 chain_id，不再加 flow_ 前缀
    return { id: id, type: 'flow', name: s? s.name : id, configuration: { ruleChainId: getProject()+':'+id } };
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

// 渲染节点参数配置区：为每个选中 node 的每个参数生成"来源 + 值"控件
function renderOrchParamOverrides() {
  const container = document.getElementById('orch-param-list');
  const emptyEl = document.getElementById('orch-param-empty');
  const countEl = document.getElementById('orch-param-count');
  if (!container) return;

  const nodeIds = getSelectedOrchNodeIds();
  // 以"实例"为单位渲染（同一节点可多次添加），每个实例独立配置参数。
  // instances: [{instanceId, nodeId, name, type, ...}]，仅保留有参数定义的实例。
  const instances = (window._orchNodeInstances || []).filter(inst => {
    const def = _orchNodes.find(n => n.node_id === inst.nodeId);
    return def && parseNodeParams(def).length > 0;
  });
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

  let html = '';
  nodes.forEach(({ inst, def }) => {
    const params = parseNodeParams(def);
    html += `<div class="override-node-block">
      <div class="override-node-header" onclick="this.nextElementSibling.classList.toggle('hidden')">
        <span class="toggle-icon">▾</span>
        <span class="code-cell">${esc(inst.instanceId)}</span>
        <span style="font-weight:400">${esc(def.name || '')}</span>
        <span style="margin-left:auto;font-weight:400;color:var(--text-muted);font-size:.72rem">${params.length} 个参数</span>
      </div>
      <div class="override-node-body">`;
    params.forEach(p => {
      const required = p.required ? ' <span class="param-required-star">*</span>' : '';
      const typeTag = p.type ? `<span class="param-type-tag">(${esc(p.type)})</span>` : '';
      html += `<div class="override-field param-row" data-node="${esc(inst.instanceId)}" data-key="${esc(p.key)}" style="flex-wrap:wrap">
        <input class="param-key" value="${esc(p.key)}" readonly title="参数英文名" style="flex:0 1 130px;min-width:110px;background:var(--bg-muted)">
        <input class="param-label" value="${esc(p.label || '')}" readonly title="显示名" style="flex:1 1 120px;min-width:90px;background:var(--bg-muted)">
        <select class="param-type" disabled style="flex:0 1 92px;min-width:80px">
          <option value="string" ${p.type==='string'?'selected':''}>string</option>
          <option value="int64" ${p.type==='int64'?'selected':''}>int64</option>
          <option value="float64" ${p.type==='float64'?'selected':''}>float64</option>
          <option value="bool" ${p.type==='bool'?'selected':''}>bool</option>
          <option value="slice" ${p.type==='slice'?'selected':''}>slice</option>
          <option value="map" ${p.type==='map'?'selected':''}>map</option>
          <option value="formula" ${p.type==='formula'?'selected':''}>formula</option>
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
  container.querySelectorAll('.override-field').forEach(field => {
    const nodeId = field.getAttribute('data-node');
    const key = field.getAttribute('data-key');
    let preset = (_orchParamPreset[nodeId] && _orchParamPreset[nodeId][key]) || null;
    // 兜底：未单独保存 node_param_overrides 时，从 DSL 节点 arguments 恢复（已写入配置后的值）
    if (!preset) {
      const inst = (_orchNodeInstances || []).find(i => i.instanceId === nodeId);
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
    initParamValueControl(field, preset);
    // 回显"私有"勾选状态
    const privCheck = field.querySelector('.param-private-check');
    if (privCheck) privCheck.checked = !!(preset && preset.private);
  });
}

// 暂存已编辑的参数值（key=nodeId, value={key: {src, value}}），用于重渲染时保留
let _orchParamPreset = {};

// 根据来源切换值控件
function onParamSrcChange(sel) {
  const field = sel.closest('.override-field');
  const src = sel.value;
  const slot = field.querySelector('.param-value-slot');
  const nodeId = field.getAttribute('data-node');
  const key = field.getAttribute('data-key');
  const preset = (_orchParamPreset[nodeId] && _orchParamPreset[nodeId][key]) || { src: orchParamSrcToInternal(src), value: '' };

  if (src === PARAM_SRC_REF_ACT) {
    preset.src = PARAM_SRC_UPSTREAM;
    renderOrchRefNodeControl(slot, nodeId, preset.value);
  } else if (src === PARAM_SRC_REF_NODE) {
    preset.src = PARAM_SRC_ENTRY;
    preset.value = '';
    renderCallInputHint(slot, key, nodeId, !!preset.private);
  } else {
    preset.src = PARAM_SRC_FIXED;
    renderFixedInput(slot, preset.value, '填写固定值');
  }
  updateParamPrivateVisibility(field);
  storeParamPreset();
}

function initParamValueControl(field, preset) {
  const sel = field.querySelector('.param-src-select');
  const slot = field.querySelector('.param-value-slot');
  if (!preset) {
    sel.value = PARAM_SRC_VALUE;
    renderFixedInput(slot, '', '填写固定值');
    updateParamPrivateVisibility(field);
    return;
  }
  sel.value = orchParamSrcToDisplay(preset.src) || PARAM_SRC_VALUE;
  if (preset.src === PARAM_SRC_UPSTREAM) {
    renderOrchRefNodeControl(slot, field.getAttribute('data-node'), preset.value);
  } else if (preset.src === PARAM_SRC_ENTRY) {
    renderCallInputHint(slot, field.getAttribute('data-key'), field.getAttribute('data-node'), !!preset.private);
  } else {
    renderFixedInput(slot, preset.value, '填写固定值');
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

// 收集「已选择的其它节点/子链」用于「引用节点」选择（excludeId 为当前实例 instanceId）
function getOrchRefNodeCandidates(excludeId) {
  const cands = [];
  // 已选节点实例（按 instanceId 排除自身）
  (window._orchNodeInstances || []).forEach(inst => {
    if (inst.instanceId === excludeId) return;
    const def = (_orchNodes || []).find(n => n.node_id === inst.nodeId);
    if (!def) return;
    cands.push({ id: inst.instanceId, name: inst.name || inst.nodeId, type: inst.type, params: parseNodeParams(def), outputs: parseNodeOutputs(def) });
  });
  // 已选子链（按 chain_id 排除自身）
  getSelectedOrchSubIds().forEach(subId => {
    if (subId === excludeId) return;
    const s = (_orchSubChains || []).find(x => x.chain_id === subId);
    if (!s) return;
    cands.push({ id: s.chain_id, name: s.name || s.chain_id, type: 'sub', params: parseNodeParams(s), outputs: parseNodeOutputs(s) });
  });
  return cands;
}

// 渲染「引用节点」控件：先选节点，再选 参数定义/返回值定义
function renderOrchRefNodeControl(slot, excludeId, presetValue) {
  // 引用节点控件较宽，独占整行并允许内部换行，避免被右侧面板遮挡
  slot.style.flex = '1 1 100%';
  slot.style.flexWrap = 'wrap';
  const parsed = parseStepsRef(presetValue);
  const cands = getOrchRefNodeCandidates(excludeId);
  const selNodeId = parsed ? parsed.nodeId : (cands[0] ? cands[0].id : '');
  const cand = cands.find(c => c.id === selNodeId) || cands[0] || null;
  const kind = parsed ? parsed.kind : 'arguments';
  const presetKey = parsed ? parsed.key : '';

  let nodeOpts = `<option value="">— 选择节点 —</option>`;
  cands.forEach(c => {
    nodeOpts += `<option value="${esc(c.id)}" ${c.id === selNodeId ? 'selected' : ''}>${esc(c.name)} <${esc(c.id)}></option>`;
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
    if (!_orchParamPreset[nodeId]) _orchParamPreset[nodeId] = {};
    _orchParamPreset[nodeId][key] = { src, value, private: isPrivate };
  });
}

// 收集为 node_param_overrides：{ nodeId: { key: { src, value } } }
// 同时保存来源(src)与最终值(value)，确保编辑回显时能准确还原"来源选择"，
// 而不依赖值格式推断。value 为最终值（引用节点即选择的值：{{steps.<节点ID>.<kind>.<字段>}}），
// 后端写入节点 arguments 时取其中的 value 字段。
function collectOrchParamOverrides() {
  const result = {};
  Object.keys(_orchParamPreset).forEach(nodeId => {
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
          const bareKey = lastDot >= 0 ? path.slice(lastDot + 1) : path;
          finalVal = '{{arguments.' + nodeId + '.' + bareKey + '}}';
        }
      }
      if (!result[nodeId]) result[nodeId] = {};
      result[nodeId][key] = { src: finalSrc, value: finalVal, private: !!isPrivate };
    });
  });
  return result;
}

// 回显已保存的 node_param_overrides（编辑加载时调用）
// 保存结构：{ paramKey: { src, value } }，src 为内部来源(fixed/upstream/entry)
// 兼容旧格式：paramKey 直接是字符串值（纯值，按格式推断来源）
function applyOrchParamOverrides(saved) {
  _orchParamPreset = {};
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
          _orchParamPreset[nodeId][key] = { src, value, private: !!raw.private };
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
  // 缩放后内容尺寸可能变化，下一帧将预览区滚动到内容中心，方便查看
  requestAnimationFrame(centerOrchPreview);
}

// 将 Live Preview 滚动到内容中心（节点多/放大后自动居中显示）
function centerOrchPreview() {
  const area = document.getElementById('orch-preview');
  if (!area) return;
  area.scrollLeft = Math.max(0, (area.scrollWidth - area.clientWidth) / 2);
  area.scrollTop = Math.max(0, (area.scrollHeight - area.clientHeight) / 2);
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
  const container = document.getElementById('orch-preview');
  const scaleBox = document.getElementById('orch-preview-scale');
  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();
  const conns = collectOrchConnections();

  if (nodeIds.length === 0 && subIds.length === 0) {
    scaleBox.innerHTML = '<div class="orch-preview-empty">选择节点并添加连接后，这里将显示实时流程图</div>';
    _orchPreviewScale = 1;
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
    }
    applyOrchPreviewScale();
    // mermaid 内部布局可能稍晚稳定，延迟再居中一次确保内容位于视野中间
    setTimeout(centerOrchPreview, 60);
  } catch(e) {
    scaleBox.innerHTML = '<div class="orch-preview-empty" style="color:#ef4444">渲染失败: ' + esc(e.message) + '</div><pre style="font-size:.7rem;margin-top:8px;white-space:pre-wrap;max-height:150px;overflow:auto">' + esc(syntax) + '</pre>';
  }
}

// 在 Live Preview 的 mermaid 节点右上角叠加"删除叉"，hover 节点时显示，点击删除该实例
// 注：叉默认常显（pointer-events:all），避免部分环境下 :hover 不触发导致无法看到/点击
function enhanceOrchPreviewNodes(container) {
  if (!container) return;
  const idMap = window._orchIdMap || {};
  const nodeIds = getSelectedOrchNodeIds();
  const subIds = getSelectedOrchSubIds();
  const SVGNS = 'http://www.w3.org/2000/svg';
  const nodeGs = Array.from(container.querySelectorAll('g.node'));
  nodeGs.forEach((g, idx) => {
    // 反查实例 ID：优先用 g.id 在 idMap 中匹配（mermaid 保留自定义 id 时有效）
    let instId = null;
    if (g.id) {
      for (const k in idMap) { if (idMap[k] === g.id) { instId = k; break; } }
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
    const x1 = document.createElementNS(SVGNS, 'line');
    const x2 = document.createElementNS(SVGNS, 'line');
    x1.setAttribute('x1', '-4'); x1.setAttribute('y1', '-4'); x1.setAttribute('x2', '4'); x1.setAttribute('y2', '4');
    x2.setAttribute('x1', '-4'); x2.setAttribute('y1', '4'); x2.setAttribute('x2', '4'); x2.setAttribute('y2', '-4');
    x1.setAttribute('class', 'orch-node-del-x'); x2.setAttribute('class', 'orch-node-del-x');
    del.appendChild(x1); del.appendChild(x2);
    del.addEventListener('click', (e) => {
      e.stopPropagation();
      e.preventDefault();
      removeOrchNodeInstanceConfirm(instId);
    });
    g.appendChild(del);
  });
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

// Generate & save root chain
// 编排目标：'root' = Root Chain，'sub' = Sub Chain
window._orchTarget = 'root';

// 切换编排目标（Root Chain / Sub Chain）。
// sub 目标下不显示 Sub Chains 选择区（子链嵌套子链引用在连接中已是可选目标，仍可引用）。
function setOrchTarget(type) {
  window._orchTarget = type;
  // root/sub 模式内容区分：切换 root-only / sub-only 区块显隐
  const showRoot = (type === 'root');
  document.querySelectorAll('.root-only').forEach(el => {
    el.style.display = showRoot ? '' : 'none';
  });
  document.querySelectorAll('.sub-only').forEach(el => {
    el.style.display = showRoot ? 'none' : '';
  });
  // 标题与区分提示
  const titleEl = document.getElementById('orch-target-title');
  const hintEl = document.getElementById('orch-target-hint');
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

// 编排新建：切换到 Orchestrate 页，目标设为 Root Chain，清空表单。
function newSubChainViaOrch() {
  // 切换到 Orchestrate Tab
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
  document.querySelector('[data-tab="orchestrate"]').classList.add('active');
  document.getElementById('tab-orchestrate').classList.add('active');

  // 清空表单
  document.getElementById('orch-chain-id').value = '';
  document.getElementById('orch-chain-key').value = '';
  document.getElementById('orch-chain-name').value = '';
  document.getElementById('orch-chain-desc').value = '';
  document.getElementById('orch-debug-mode').checked = false;
  _orchParamPreset = {}; // 重置节点参数配置暂存
  window._orchNodeInstances = []; // 重置已选节点实例
  document.querySelectorAll('#orch-conn-container .orch-conn-row').forEach(r => r.remove());
  const emptyEl = document.getElementById('orch-conn-empty');
  if (emptyEl) emptyEl.style.display = '';
  renderOrchNodeSelected();

  // 确保数据已加载再取消勾选
  loadOrchData().then(() => {
    document.querySelectorAll('#orch-sub-list input[type="checkbox"]').forEach(cb => cb.checked = false);
    onOrchSelectionChange();
  });

  const btn = document.getElementById('orch-generate-btn');
  btn.dataset.edit = '';
  setOrchTarget('root');
  showToast('已切换到编排页，目标为 Sub Chain', 'success');
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

    const nodeIds = (c.node_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
    restoreOrchNodeInstances(nodeIds);
    const subIds = (c.sub_chain_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
    document.querySelectorAll('#orch-sub-list input[type="checkbox"]').forEach(cb => {
      cb.checked = subIds.includes(cb.value);
    });
    onOrchSelectionChange();

    document.querySelectorAll('#orch-conn-container .orch-conn-row').forEach(r => r.remove());
    const emptyEl = document.getElementById('orch-conn-empty');
    try {
      const conns = JSON.parse(c.connections_data || '[]');
      conns.forEach(conn => addOrchConnRow(conn.from_id, conn.to_id, conn.type));
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
  };

  const btn = document.getElementById('orch-generate-btn');
  const isEdit = btn.dataset.edit === '1';
  btn.disabled = true;
  btn.textContent = '生成中...';

  const endpoint = isSub
    ? (isEdit ? '/api/sub-chains/' + encodeURIComponent(chainId) + '/build' : '/api/sub-chains/build')
    : '/api/root-chains';
  const method = isSub && isEdit ? 'PUT' : 'POST';

  try {
    await api(endpoint, { method, headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
    showToast((isSub ? 'Sub Chain' : 'Root Chain') + ' 已保存: ' + (chainId || '(自动生成)'), 'success');
    if (isSub) {
      const subTab = document.getElementById('tab-sub-chains');
      if (subTab && subTab.classList.contains('active')) loadSubChains();
    } else {
      const rootTab = document.getElementById('tab-root-chains');
      if (rootTab && rootTab.classList.contains('active')) loadRootChains();
    }
    // 保存成功后清除编辑标记，恢复默认文案
    btn.dataset.edit = '';
    setOrchTarget(isSub ? 'sub' : 'root');
  } catch(e) {
    showToast('生成失败: ' + e.message, 'error');
  } finally {
    btn.disabled = false;
    btn.textContent = isSub ? (isEdit ? '💾 更新 Sub Chain' : '🔧 生成 Sub Chain') : (isEdit ? '💾 更新 Root Chain' : '🔧 生成 Root Chain');
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
    // 恢复已保存的节点参数配置（在勾选节点前 apply，renderOrchParamOverrides 会回显）
    applyOrchParamOverrides(c.node_param_overrides);

    // Check nodes
    const nodeIds = (c.node_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
    document.querySelectorAll('#orch-node-list input[type="checkbox"]').forEach(cb => {
      cb.checked = nodeIds.includes(cb.value);
    });
    // Check sub-chains
    const subIds = (c.sub_chain_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
    document.querySelectorAll('#orch-sub-list input[type="checkbox"]').forEach(cb => {
      cb.checked = subIds.includes(cb.value);
    });
    onOrchSelectionChange();

    // Load connections
    document.querySelectorAll('#orch-conn-container .orch-conn-row').forEach(r => r.remove());
    const emptyEl = document.getElementById('orch-conn-empty');
    try {
      const conns = JSON.parse(c.connections_data || '[]');
      conns.forEach(conn => addOrchConnRow(conn.from_id, conn.to_id, conn.type));
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
function esc(s) { if (s === null || s === undefined) return ''; return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;'); }
// 安全取值：元素可能已被 UI 隐藏/移除，缺失时返回默认值
function safeVal(id, def) { const el = document.getElementById(id); return el ? el.value.trim() : (def || ''); }
function safeChecked(id) { const el = document.getElementById(id); return el ? el.checked : false; }
function trunc(s, n) { if (!s) return '-'; return s.length > n ? s.substring(0, n) + '...' : s; }
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
      // return_value 的 key 为空表示返回活动返回的全部内容：选中后等价「返回值整体」，
      // 选项 value 置空，拼路径时回退为 {{id.responses}}（与返回全部数据完全一致）
      const val = rvKey === '' ? '' : rvName;
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
    const typeSel = '<select class="arg-type" onchange="syncActivityConfig()" title="值类型" style="flex:0 0 70px;width:70px;padding:3px 4px;border:1px solid var(--border);border-radius:6px;font-size:.72rem">' +
      ['string', 'int64', 'float64', 'formula'].map(t =>
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
      return;
    }
  } catch (_) {}
  // 未登录：显示遮罩
  document.getElementById('login-overlay').classList.remove('hidden');
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
  const valid = ['activities', 'nodes', 'sub-chains', 'root-chains', 'logs', 'orchestrate'];
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
    const chains = await api('/api/root-chains');
    const c = chains.find(x => x.chain_id === chainId);
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
      applyOrchParamOverrides(c.node_param_overrides);
      const nodeIds = (c.node_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
      restoreOrchNodeInstances(nodeIds, c.dsl_json);
      const subIds = (c.sub_chain_ids||'').split(',').map(s=>s.trim()).filter(Boolean);
      document.querySelectorAll('#orch-sub-list input[type="checkbox"]').forEach(cb => { cb.checked = subIds.includes(cb.value); });
      onOrchSelectionChange();
      document.querySelectorAll('#orch-conn-container .orch-conn-row').forEach(r => r.remove());
      try {
        const conns = JSON.parse(c.connections_data || '[]');
        conns.forEach(conn => addOrchConnRow(conn.from_id, conn.to_id, conn.type));
      } catch(e) {}
      onOrchChange();
      showToast('已加载根链到编排页，修改后点击"更新 Root Chain"保存', 'success');
    });
  } catch (e) { showToast('加载根链失败: ' + e.message, 'error'); }
}

// 启动：先鉴权，再加载主界面
bootstrapAuth();
