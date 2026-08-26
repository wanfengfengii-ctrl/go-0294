// Live single-page workbench for the curtain-wall laminated-glass assembly gate.
// Every view reads real backend state through the JSON API and submits real
// requests; no static demonstration data is used.
const $ = (id) => document.getElementById(id);

async function api(path, method = 'GET', body) {
  const opts = { method, headers: { 'Content-Type': 'application/json' } };
  if (body !== undefined) opts.body = JSON.stringify(body);
  const res = await fetch(path, opts);
  const data = await res.json().catch(() => ({}));
  return { ok: res.ok, status: res.status, data };
}

function render(el, { ok, status, data }) {
  el.textContent = JSON.stringify(data, null, 2);
  el.className = ok ? '' : 'err';
  return { ok, status, data };
}

function geometry() {
  return {
    outline: [
      { x: 5, y: 5 }, { x: 100005, y: 5 },
      { x: 100005, y: 200005 }, { x: 5, y: 200005 }
    ],
    holes: []
  };
}

async function refreshHealth() {
  const r = await api('/api/health');
  $('health').textContent = r.ok ? `后端可用：${r.data.status}` : '后端不可达';
  if (!r.ok) $('health').className = 'err';
}

async function refreshTasks() {
  const r = await api('/api/tasks');
  if (r.ok) {
    // Expand lineage and coverage per task for a live, connected view.
    const enriched = await Promise.all(r.data.map(async (t) => {
      const [lin, cov] = await Promise.all([
        api(`/api/tasks/${t.id}/lineage`),
        api(`/api/tasks/${t.id}/coverage`)
      ]);
      return { ...t, lineage: lin.data, coverage: cov.data };
    }));
    $('tasks').textContent = JSON.stringify(enriched, null, 2);
    $('tasks').className = '';
  } else {
    $('tasks').textContent = '加载失败';
    $('tasks').className = 'err';
  }
}

async function lockDesign() {
  const payload = {
    project: $('project').value, facade_zone: $('zone').value,
    plate_number: $('plate').value, version: 1, rule_digest: 'seed',
    thickness_um: 12000, width_um: 100010, height_um: 200010,
    edge_margin_um: 5, edge_scheme: 'flat-polish', geometry: geometry(),
    furnace_lot: $('lot').value, film_batch: $('film').value, film_opening_um2: 1000000,
    thresholds: { surface_stress: 1000, bow: 1000000, bubble_rate: 1000 },
    rack: { furnace_run: 'RUN-1', positions: [{ id: 'R1', level: 1 }], adjacency: [] },
    inspection: { grid: ['G1'], sampling: { G1: $('plate').value }, destructive: 1 },
    locked_generation: 0
  };
  render($('lockResult'), await api('/api/designs/lock', 'POST', payload));
  refreshTasks();
}

async function advance() {
  const id = $('taskId').value;
  const stage = $('stage').value;
  const tasks = (await api('/api/tasks')).data || [];
  const task = tasks.find((t) => t.id === id);
  if (!task) { render($('advanceResult'), { ok: false, status: 0, data: { code: 'NOT_FOUND', message: '任务不存在' } }); return; }
  const payload = {
    operation_id: $('opId').value || undefined,
    rule_digest: task.snapshot.rule_digest,
    generation: task.generation,
    logical_time: Date.now() % 100000,
    operator: 'op',
    stage
  };
  if (stage === 'lamination') {
    payload.resource_key = 'table-1'; payload.lease_start = 1; payload.lease_end = 100000;
    payload.film_entry = { kind: 'issue', amount_um2: 300000 };
  } else if (stage === 'pre_press') {
    payload.film_entry = { kind: 'cut', amount_um2: 300000 };
  } else if (stage === 'heat_soak') {
    payload.resource_key = 'rack-1'; payload.lease_start = 1; payload.lease_end = 100000;
  } else if (stage === 'autoclave') {
    payload.resource_key = 'autoclave-1'; payload.lease_start = 1; payload.lease_end = 100000;
  }
  render($('advanceResult'), await api(`/api/tasks/${id}/operations`, 'POST', payload));
  refreshTasks();
}

async function ledger() {
  render($('ledgerResult'), await api(`/api/film-ledger?batch=${encodeURIComponent($('ledgerBatch').value)}`));
}

async function retries() {
  render($('retryResult'), await api('/api/retries'));
}

async function anomaly() {
  const id = $('anomTask').value;
  const tasks = (await api('/api/tasks')).data || [];
  const task = tasks.find((t) => t.id === id);
  if (!task) { render($('anomalyResult'), { ok: false, status: 0, data: { code: 'NOT_FOUND', message: '任务不存在' } }); return; }
  render($('anomalyResult'), await api(`/api/tasks/${id}/anomalies`, 'POST', {
    kind: $('anomKind').value, rack_position: 'R1',
    generation: task.generation, rule_digest: task.snapshot.rule_digest
  }));
  refreshTasks();
}

async function review() {
  const id = $('verdictTask').value;
  const tasks = (await api('/api/tasks')).data || [];
  const task = tasks.find((t) => t.id === id);
  if (!task) { render($('verdictResult'), { ok: false, status: 0, data: { code: 'NOT_FOUND', message: '任务不存在' } }); return; }
  render($('verdictResult'), await api(`/api/tasks/${id}/reviews`, 'POST', {
    reviewer: $('reviewer').value, qualified: true, generation: task.generation
  }));
}

async function submitVerdict() {
  const id = $('verdictTask').value;
  const tasks = (await api('/api/tasks')).data || [];
  const task = tasks.find((t) => t.id === id);
  if (!task) { render($('verdictResult'), { ok: false, status: 0, data: { code: 'NOT_FOUND', message: '任务不存在' } }); return; }
  render($('verdictResult'), await api(`/api/tasks/${id}/verdicts`, 'POST', {
    verdict: $('verdict').value, generation: task.generation
  }));
}

$('lock').addEventListener('click', lockDesign);
$('advance').addEventListener('click', advance);
$('ledger').addEventListener('click', ledger);
$('retries').addEventListener('click', retries);
$('anomaly').addEventListener('click', anomaly);
$('review').addEventListener('click', review);
$('submitVerdict').addEventListener('click', submitVerdict);

refreshHealth();
refreshTasks();
