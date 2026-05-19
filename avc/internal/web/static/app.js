/* AVC web UI — vanilla JS app. */
'use strict';

const state = {
  snapshots: [],
  selectedId: null,
};

// ── Utils ────────────────────────────────────────────────────────────────
function $(id) { return document.getElementById(id); }
function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  }[c]));
}
function formatBytes(b) {
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
  return `${(b / 1024 / 1024).toFixed(1)} MB`;
}
function formatTimestamp(unix) {
  return new Date(unix * 1000).toLocaleString();
}
function timeAgo(unix) {
  const now = Date.now();
  const ms = now - unix * 1000;
  const s = Math.floor(ms / 1000);
  const m = Math.floor(s / 60);
  const h = Math.floor(m / 60);
  if (s < 60)  return 'just now';
  if (m < 60)  return `${m}m ago`;
  if (h < 24)  return `${h}h ago`;
  const d = new Date(unix * 1000);
  const today = new Date();
  const yesterday = new Date(today); yesterday.setDate(today.getDate() - 1);
  const fmt = (date) => date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  if (d.toDateString() === yesterday.toDateString()) return `Yesterday ${d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}`;
  if (d.getFullYear() === today.getFullYear()) return fmt(d);
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || 'Request failed');
  }
  return res.json();
}

// ── Toast notifications ──────────────────────────────────────────────────
function showToast(message, type = 'success') {
  const container = $('toast-container');
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.innerHTML = `
    <span class="toast-icon">${type === 'success' ? '✓' : '✕'}</span>
    <span class="toast-msg">${escapeHtml(message)}</span>
    <button class="toast-close" aria-label="Dismiss">✕</button>
  `;
  const dismiss = () => {
    toast.style.animation = 'toastOut 0.2s ease forwards';
    setTimeout(() => toast.remove(), 200);
  };
  toast.querySelector('.toast-close').onclick = dismiss;
  container.appendChild(toast);
  setTimeout(dismiss, 4000);
}

// ── Date bucketing (mirrors sidebar.ts) ─────────────────────────────────
function bucketFor(timestamp, now) {
  const date = new Date(timestamp * 1000);
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const startOfYesterday = startOfToday - 24 * 60 * 60 * 1000;
  const startOfWeek      = startOfToday - 7  * 24 * 60 * 60 * 1000;
  const startOfMonth     = startOfToday - 30 * 24 * 60 * 60 * 1000;
  const ts = date.getTime();
  if (ts >= startOfToday)     return 'Today';
  if (ts >= startOfYesterday) return 'Yesterday';
  if (ts >= startOfWeek)      return 'This Week';
  if (ts >= startOfMonth)     return 'This Month';
  return 'Older';
}
const BUCKET_ORDER = ['Today', 'Yesterday', 'This Week', 'This Month', 'Older'];

// ── Snapshot list rendering ──────────────────────────────────────────────
function renderSnapshotList() {
  const container = $('snapshot-list');
  container.innerHTML = '';
  const countEl = $('snapshot-count');
  if (countEl) countEl.textContent = state.snapshots.length > 0 ? state.snapshots.length : '';

  if (state.snapshots.length === 0) {
    container.innerHTML = `
      <div class="sidebar-empty">
        <div class="empty-icon">◈</div>
        <p class="empty-title">No snapshots yet</p>
        <p class="empty-sub">Save your first snapshot to start tracking project state.</p>
        <button class="btn btn-primary" onclick="document.getElementById('btn-new-snapshot').click()">+ New Snapshot</button>
      </div>`;
    return;
  }

  const now = new Date();
  const buckets = new Map();
  for (const s of state.snapshots) {
    const bucket = bucketFor(s.timestamp, now);
    if (!buckets.has(bucket)) buckets.set(bucket, []);
    buckets.get(bucket).push(s);
  }

  let firstGroup = true;
  for (const name of BUCKET_ORDER) {
    if (!buckets.has(name)) continue;
    const snaps = buckets.get(name);
    const group = document.createElement('div');
    group.className = firstGroup ? 'snapshot-group' : 'snapshot-group collapsed';
    firstGroup = false;

    const header = document.createElement('div');
    header.className = 'group-header';
    header.innerHTML = `
      <span class="group-chevron">▼</span>
      <span>${escapeHtml(name)}</span>
      <span class="group-count">${snaps.length}</span>
    `;
    header.onclick = () => group.classList.toggle('collapsed');
    group.appendChild(header);

    const children = document.createElement('div');
    children.className = 'group-children';
    for (const s of snaps) {
      const row = document.createElement('div');
      row.className = 'snapshot-row';
      if (s.id === state.selectedId) row.classList.add('selected');
      row.innerHTML = `
        <div class="label">${escapeHtml(s.label)}</div>
        <div class="meta">
          ${s.agent_name ? `<span class="agent">${escapeHtml(s.agent_name)}</span>` : ''}
          ${timeAgo(s.timestamp)} · ${s.files_changed} file${s.files_changed === 1 ? '' : 's'}
        </div>
      `;
      row.onclick = () => selectSnapshot(s.id);
      children.appendChild(row);
    }
    group.appendChild(children);
    container.appendChild(group);
  }
}

// ── Snapshot detail / file tree ──────────────────────────────────────────
async function selectSnapshot(id) {
  state.selectedId = id;
  renderSnapshotList();

  const detail = $('detail');
  detail.innerHTML = '<p class="muted">Loading…</p>';

  try {
    const snap = await api(`/api/snapshots/${id}`);
    renderDetail(snap);
  } catch (err) {
    detail.innerHTML = `<p class="muted">Error: ${escapeHtml(err.message)}</p>`;
  }
}

function renderDetail(snap) {
  const detail = $('detail');
  const tree = buildTree(snap.files);
  detail.innerHTML = `
    <h2>${escapeHtml(snap.label)}</h2>
    <div class="detail-grid">
      <span class="label">ID</span>          <span class="value">${escapeHtml(snap.id)}</span>
      <span class="label">Timestamp</span>   <span class="value">${formatTimestamp(snap.timestamp)}</span>
      <span class="label">Agent</span>       <span class="value">${snap.agent_name ? escapeHtml(snap.agent_name) : '<em>none</em>'}</span>
      <span class="label">Files</span>       <span class="value">${snap.file_count}</span>
      <span class="label">Total Size</span>  <span class="value">${formatBytes(snap.total_size)}</span>
    </div>
    ${snap.notes ? `<div class="notes-block">${escapeHtml(snap.notes)}</div>` : ''}
    <div class="detail-actions">
      <button class="btn btn-primary" id="btn-restore-snap">↩ Restore Snapshot</button>
      <button class="btn" id="btn-diff-current">⇄ Diff with Current</button>
      <button class="btn" id="btn-diff-prev">↔ Diff vs Previous</button>
      <button class="btn btn-danger" id="btn-delete-snap">Delete</button>
    </div>
    <h3>Files (${snap.files.length})</h3>
    <div class="file-tree" id="file-tree">${renderTree(tree, snap.id, 0)}</div>
  `;

  // Wire up action buttons.
  $('btn-restore-snap').onclick = () => confirmRestore(snap.id, snap.label);
  $('btn-delete-snap').onclick  = () => confirmDelete(snap.id, snap.label);
  $('btn-diff-current').onclick = () => viewDiffCurrent(snap.id, snap.label);
  const idx = state.snapshots.findIndex(s => s.id === snap.id);
  const hasPrev = idx !== -1 && idx < state.snapshots.length - 1;
  const diffPrevBtn = $('btn-diff-prev');
  diffPrevBtn.disabled = !hasPrev;
  diffPrevBtn.title = hasPrev ? '' : 'No earlier snapshot to compare against';
  diffPrevBtn.onclick = hasPrev ? () => viewDiffPrevious(snap.id) : null;

  // Wire up file tree.
  document.querySelectorAll('.folder-row').forEach(row => {
    row.onclick = () => row.parentElement.classList.toggle('collapsed');
  });
  document.querySelectorAll('.restore-btn').forEach(btn => {
    btn.onclick = (e) => {
      e.stopPropagation();
      confirmRestoreFile(snap.id, btn.dataset.path);
    };
  });
}

// ── File tree builder ────────────────────────────────────────────────────
function buildTree(files) {
  const root = { name: '', path: '', children: new Map() };
  for (const f of files) {
    const parts = f.path.split('/').filter(Boolean);
    let current = root;
    parts.forEach((part, i) => {
      if (!current.children.has(part)) {
        current.children.set(part, {
          name: part,
          path: parts.slice(0, i + 1).join('/'),
          children: new Map(),
        });
      }
      current = current.children.get(part);
    });
    current.file = f;
  }
  return root;
}
function countFiles(node) {
  let count = node.file ? 1 : 0;
  for (const c of node.children.values()) count += countFiles(c);
  return count;
}
function fileIcon(name) {
  const ext = (name.split('.').pop() || '').toLowerCase();
  // Brand colours sourced from each language's official style guide.
  const badges = {
    py:    { label: 'py',    bg: '#3776ab', fg: '#fff' },
    go:    { label: 'go',    bg: '#00add8', fg: '#fff' },
    js:    { label: 'js',    bg: '#f7df1e', fg: '#000' },
    ts:    { label: 'ts',    bg: '#3178c6', fg: '#fff' },
    tsx:   { label: 'tsx',   bg: '#3178c6', fg: '#fff' },
    jsx:   { label: 'jsx',   bg: '#61dafb', fg: '#000' },
    json:  { label: 'json',  bg: '#292929', fg: '#fff' },
    md:    { label: 'md',    bg: '#083fa1', fg: '#fff' },
    css:   { label: 'css',   bg: '#264de4', fg: '#fff' },
    html:  { label: 'html',  bg: '#e34c26', fg: '#fff' },
    htm:   { label: 'html',  bg: '#e34c26', fg: '#fff' },
    sh:    { label: 'sh',    bg: '#4eaa25', fg: '#fff' },
    bash:  { label: 'sh',    bg: '#4eaa25', fg: '#fff' },
    yaml:  { label: 'yaml',  bg: '#cb171e', fg: '#fff' },
    yml:   { label: 'yaml',  bg: '#cb171e', fg: '#fff' },
    toml:  { label: 'toml',  bg: '#9c4121', fg: '#fff' },
    rs:    { label: 'rs',    bg: '#ce422b', fg: '#fff' },
    rb:    { label: 'rb',    bg: '#cc342d', fg: '#fff' },
    java:  { label: 'java',  bg: '#007396', fg: '#fff' },
    kt:    { label: 'kt',    bg: '#7f52ff', fg: '#fff' },
    swift: { label: 'swift', bg: '#f05138', fg: '#fff' },
    c:     { label: 'c',     bg: '#555',    fg: '#fff' },
    cpp:   { label: 'c++',   bg: '#00599c', fg: '#fff' },
    cs:    { label: 'c#',    bg: '#512bd4', fg: '#fff' },
    php:   { label: 'php',   bg: '#8892be', fg: '#fff' },
    lua:   { label: 'lua',   bg: '#000080', fg: '#fff' },
    r:     { label: 'r',     bg: '#276dc3', fg: '#fff' },
    sql:   { label: 'sql',   bg: '#e38d13', fg: '#fff' },
    tf:    { label: 'tf',    bg: '#7b42bc', fg: '#fff' },
    svg:   { label: 'svg',   bg: '#ffb13b', fg: '#000' },
    png:   { label: 'png',   bg: '#888',    fg: '#fff' },
    jpg:   { label: 'jpg',   bg: '#888',    fg: '#fff' },
    jpeg:  { label: 'jpg',   bg: '#888',    fg: '#fff' },
    gif:   { label: 'gif',   bg: '#888',    fg: '#fff' },
    webp:  { label: 'webp',  bg: '#888',    fg: '#fff' },
  };
  const b = badges[ext];
  if (!b) return `<span class="file-badge file-badge-default">&#x1F4C4;</span>`;
  return `<span class="file-badge" style="background:${b.bg};color:${b.fg}">${b.label}</span>`;
}
function renderTree(node, snapshotId, depth) {
  const sorted = [...node.children.values()].sort((a, b) => {
    const aIsDir = !a.file, bIsDir = !b.file;
    if (aIsDir && !bIsDir) return -1;
    if (!aIsDir && bIsDir) return 1;
    return a.name.localeCompare(b.name);
  });
  let html = '';
  for (const child of sorted) {
    const indent = depth * 16 + 4;
    if (!child.file) {
      const fileCount = countFiles(child);
      html += `<div class="tree-folder collapsed">
        <div class="tree-row folder-row" style="padding-left:${indent}px">
          <span class="tree-chevron">▼</span>
          <span class="folder-icon">📁</span>
          <span class="tree-name">${escapeHtml(child.name)}</span>
          <span class="tree-meta">${fileCount} file${fileCount === 1 ? '' : 's'}</span>
        </div>
        <div class="tree-children">${renderTree(child, snapshotId, depth + 1)}</div>
      </div>`;
    } else {
      const f = child.file;
      html += `<div class="tree-row file-row" style="padding-left:${indent + 14}px">
        <span class="file-icon">${fileIcon(child.name)}</span>
        <span class="tree-name">${escapeHtml(child.name)}</span>
        <span class="tree-size">${formatBytes(f.size)}</span>
        <button class="restore-btn" data-path="${escapeHtml(f.path)}">Restore</button>
      </div>`;
    }
  }
  return html;
}

// ── Action handlers ──────────────────────────────────────────────────────
function openModal(id) { $(id).hidden = false; }
function closeModal(id) { $(id).hidden = true; }

function setLoading(btn, loading) {
  if (loading) {
    btn.disabled = true;
    btn.dataset.origText = btn.textContent;
    btn.textContent = '…';
  } else {
    btn.disabled = false;
    btn.textContent = btn.dataset.origText || btn.textContent;
  }
}

function showConfirm(title, message, onYes) {
  $('confirm-title').textContent = title;
  $('confirm-message').textContent = message;
  const btn = $('confirm-yes');
  btn.textContent = 'Confirm';
  btn.disabled = false;
  const handler = async () => {
    btn.removeEventListener('click', handler);
    setLoading(btn, true);
    try {
      await onYes();
    } finally {
      setLoading(btn, false);
      closeModal('modal-confirm');
    }
  };
  btn.addEventListener('click', handler);
  openModal('modal-confirm');
}

function confirmRestore(id, label) {
  showConfirm(
    'Restore snapshot?',
    `This will overwrite all current files with the contents of "${label}". A safety snapshot will be created automatically.`,
    async () => {
      try {
        // Auto-snapshot before restore (safety net).
        await api('/api/snapshots/create', {
          method: 'POST',
          body: JSON.stringify({
            label: 'Pre-restore safety snapshot',
            agent: 'avc-ui',
            notes: `Auto-saved before restoring to "${label}"`,
          }),
        });
        const result = await api('/api/restore', {
          method: 'POST',
          body: JSON.stringify({ id }),
        });
        showToast(`Restored ${result.restored_files} file${result.restored_files === 1 ? '' : 's'}`);
        await refreshAll();
      } catch (err) {
        showToast(`Restore failed: ${err.message}`, 'error');
      }
    }
  );
}

function confirmDelete(id, label) {
  showConfirm(
    'Delete snapshot?',
    `Snapshot "${label}" will be permanently deleted. This cannot be undone.`,
    async () => {
      try {
        await api(`/api/snapshots/${id}`, { method: 'DELETE' });
        state.selectedId = null;
        $('detail').innerHTML = '<div class="empty-state"><p>Snapshot deleted.</p></div>';
        await refreshAll();
      } catch (err) {
        showToast(`Delete failed: ${err.message}`, 'error');
      }
    }
  );
}

function confirmRestoreFile(id, path) {
  showConfirm(
    'Restore file?',
    `"${path}" will be overwritten with the version from this snapshot.`,
    async () => {
      try {
        await api('/api/restore-file', {
          method: 'POST',
          body: JSON.stringify({ id, path }),
        });
        showToast(`Restored ${path}`);
      } catch (err) {
        showToast(`Restore failed: ${err.message}`, 'error');
      }
    }
  );
}

// ── Diff viewer ──────────────────────────────────────────────────────────
async function viewDiffCurrent(snapId, label) {
  try {
    const result = await api(`/api/diff-current?id=${encodeURIComponent(snapId)}`);
    showDiff(result, label, 'Working Tree');
  } catch (err) {
    showToast(`Diff failed: ${err.message}`, 'error');
  }
}

async function viewDiffPrevious(snapId) {
  const idx = state.snapshots.findIndex(s => s.id === snapId);
  const prev = state.snapshots[idx + 1];
  if (!prev) {
    showToast('No earlier snapshot to compare against.', 'error');
    return;
  }
  try {
    const result = await api(`/api/diff?from=${encodeURIComponent(prev.id)}&to=${encodeURIComponent(snapId)}`);
    showDiff(result, prev.label, state.snapshots[idx].label);
  } catch (err) {
    showToast(`Diff failed: ${err.message}`, 'error');
  }
}

function showDiff(result, fromLabel, toLabel) {
  $('diff-title').textContent = `${fromLabel} → ${toLabel}`;
  const totalAdded   = result.files.reduce((s, f) => s + f.lines_added,   0);
  const totalRemoved = result.files.reduce((s, f) => s + f.lines_removed, 0);
  $('diff-summary').innerHTML =
    `${result.files.length} file(s) changed &nbsp; <span class="added">+${totalAdded}</span> &nbsp; <span class="removed">-${totalRemoved}</span>`;

  const body = $('diff-body');
  body.innerHTML = result.files.map(f => `
    <div class="diff-file">
      <div class="diff-file-header">
        <span>${escapeHtml(f.path)}</span>
        <span>
          <span class="diff-badge ${f.type}">${f.type}</span>
          <span class="added">+${f.lines_added}</span>
          <span class="removed">-${f.lines_removed}</span>
        </span>
      </div>
      ${f.diff_preview ? `<div>${renderUnifiedDiff(f.diff_preview)}</div>` : ''}
    </div>
  `).join('');
  openModal('modal-diff');
}

function renderUnifiedDiff(preview) {
  let oldLine = 1, newLine = 1;
  const rows = preview.split('\n').filter(l => l.length > 0).map(line => {
    if (line.startsWith('@@ ')) {
      const m = line.match(/@@ -(\d+)[,\d]* \+(\d+)/);
      if (m) { oldLine = +m[1]; newLine = +m[2]; }
      return `<tr class="hunk-row"><td colspan="3">${escapeHtml(line)}</td></tr>`;
    }
    const prefix = line[0];
    const rawCode = line.slice(1);
    if (prefix === '+') return `<tr class="add-row"><td class="ln">${newLine++}</td><td class="sign">+</td><td class="code">${escapeHtml(rawCode)}</td></tr>`;
    if (prefix === '-') return `<tr class="del-row"><td class="ln">${oldLine++}</td><td class="sign">-</td><td class="code">${escapeHtml(rawCode)}</td></tr>`;
    if (prefix === ' ') { const n = oldLine++; newLine++; return `<tr class="ctx-row"><td class="ln">${n}</td><td class="sign"> </td><td class="code">${escapeHtml(rawCode)}</td></tr>`; }
    return '';
  }).join('');
  return `<table class="diff-table">${rows}</table>`;
}

// ── Save snapshot modal ──────────────────────────────────────────────────
function setupSaveModal() {
  $('btn-new-snapshot').onclick = () => {
    $('save-label').value = '';
    $('save-agent').value = '';
    $('save-notes').value = '';
    openModal('modal-save');
    setTimeout(() => $('save-label').focus(), 0);
  };

  // Submit on Enter in any text input (not textarea — Enter there adds a newline).
  $('modal-save').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && e.target.tagName !== 'TEXTAREA') {
      e.preventDefault();
      $('confirm-save').click();
    }
  });
  $('confirm-save').onclick = async () => {
    const label = $('save-label').value.trim();
    if (!label) { showToast('Label is required', 'error'); return; }
    const btn = $('confirm-save');
    setLoading(btn, true);
    try {
      const snap = await api('/api/snapshots/create', {
        method: 'POST',
        body: JSON.stringify({
          label,
          agent: $('save-agent').value.trim(),
          notes: $('save-notes').value.trim(),
        }),
      });
      closeModal('modal-save');
      await refreshAll();
      selectSnapshot(snap.id);
      showToast('Snapshot created');
    } catch (err) {
      showToast(`Snapshot failed: ${err.message}`, 'error');
    } finally {
      setLoading(btn, false);
    }
  };
}

// ── Bootstrap ────────────────────────────────────────────────────────────
async function refreshAll() {
  try {
    state.snapshots = await api('/api/snapshots');
    renderSnapshotList();
  } catch (err) {
    $('snapshot-list').innerHTML = `<p class="muted">Error: ${escapeHtml(err.message)}</p>`;
  }
}

async function loadProjectInfo() {
  try {
    const info = await api('/api/project');
    $('project-name').textContent = info.name;
    $('branch-badge').textContent = info.active_branch;
  } catch {
    $('project-name').textContent = '(unknown)';
  }
}

function setupModalCloseButtons() {
  document.querySelectorAll('[data-close]').forEach(btn => {
    btn.onclick = () => closeModal(btn.dataset.close);
  });
  // Click outside modal card to close.
  document.querySelectorAll('.modal').forEach(modal => {
    modal.addEventListener('click', (e) => {
      if (e.target === modal) modal.hidden = true;
    });
  });
}

window.addEventListener('DOMContentLoaded', () => {
  setupSaveModal();
  setupModalCloseButtons();
  $('btn-refresh').onclick = refreshAll;
  loadProjectInfo();
  refreshAll();

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      document.querySelectorAll('.modal:not([hidden])').forEach(m => { m.hidden = true; });
    }
  });
});
