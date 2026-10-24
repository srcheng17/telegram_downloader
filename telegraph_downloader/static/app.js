const archiveForm = document.querySelector('#archive-form');
const settingsForm = document.querySelector('#settings-form');
const jobsSection = document.querySelector('[data-jobs]');
const list = document.querySelector('[data-job-list]');
const busyJobs = new Set();
let mutationVersion = 0;
let pollTimer;
let pollController;
let polling = false;
let pageActive = true;

function messageFor(payload, status) {
  if (typeof payload.detail === 'string') return payload.detail;
  if (typeof payload.error === 'string') return payload.error;
  if (Array.isArray(payload.detail)) return '提交的信息不完整或格式不正确，请检查后重试。';
  return `服务暂时无法响应（${status}），请重试。`;
}

async function requestJSON(url, options = {}) {
  const response = await fetch(url, { cache: 'no-store', ...options });
  const payload = await response.json().catch(() => null);
  if (!response.ok) throw new Error(messageFor(payload || {}, response.status));
  if (!payload || typeof payload !== 'object') throw new Error('服务返回的信息不完整，请刷新页面后重试。');
  return payload;
}

function feedback(node, message, error = false) {
  node.textContent = message;
  node.classList.toggle('is-error', error);
}

function formatSize(value) {
  const size = Number(value);
  if (!Number.isFinite(size) || size <= 0) return '';
  const unit = Math.min(3, Math.floor(Math.log(size) / Math.log(1024)));
  return `${(size / 1024 ** unit).toLocaleString('zh-CN', { maximumFractionDigits: 1 })} ${['B', 'KiB', 'MiB', 'GiB'][unit]}`;
}

function formatTime(value) {
  const date = new Date(value);
  if (!value || Number.isNaN(date.getTime())) return '';
  return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false });
}

function updateRow(row, job) {
  const metadata = job.metadata && typeof job.metadata === 'object' ? job.metadata : {};
  const actions = Array.isArray(job.available_actions) ? job.available_actions : [];
  const id = String(job.id);
  row.dataset.jobId = id;
  row.dataset.status = job.status || '';
  row.querySelector('[data-job-title]').textContent = metadata.comic_name || job.filename || (job.kind === 'upload' ? '上传的漫画' : 'Telegraph 图集');
  row.querySelector('[data-job-status]').textContent = job.status_label || '状态待更新';
  row.querySelector('[data-job-source]').textContent = job.source_label || (job.kind === 'upload' ? '上传压缩包' : 'Telegraph 图集');
  row.querySelector('[data-job-library]').textContent = job.library_name || '未指定书库';
  row.querySelector('[data-job-series]').textContent = [metadata.series_name, metadata.series_number].filter(Boolean).join(' · ') || '未填写系列';
  row.querySelector('[data-job-facts]').textContent = [Number(job.page_count) > 0 ? `${job.page_count} 页` : '', formatSize(job.size)].filter(Boolean).join(' / ');
  const time = row.querySelector('[data-job-time]');
  time.textContent = formatTime(job.created_at);
  time.dateTime = job.created_at || '';
  for (const name of ['phase', 'error']) {
    const node = row.querySelector(`[data-job-${name}]`);
    node.textContent = job[name] || '';
    node.hidden = !job[name];
  }
  row.setAttribute('aria-busy', String(busyJobs.has(id)));
  row.querySelector('.job-actions').hidden = !actions.some(action => ['cancel', 'retry'].includes(action));
  for (const control of row.querySelectorAll('[data-action]')) {
    const action = control.dataset.action;
    control.hidden = !actions.includes(action);
    if (control.tagName === 'BUTTON') control.disabled = busyJobs.has(id);
  }
}

function newRow(job) {
  const row = jobsSection.querySelector('[data-job-template]').content.firstElementChild.cloneNode(true);
  updateRow(row, job);
  return row;
}

function renderJobs(jobs) {
  const existing = new Map([...list.children].map(row => [row.dataset.jobId, row]));
  jobs.forEach((job, index) => {
    const id = String(job.id);
    const row = existing.get(id) || newRow(job);
    updateRow(row, job);
    if (list.children[index] !== row) list.insertBefore(row, list.children[index] || null);
    existing.delete(id);
  });
  for (const row of existing.values()) row.remove();
  jobsSection.querySelector('[data-jobs-empty]').hidden = jobs.length > 0;
}

async function refreshJobs() {
  if (!jobsSection || !pageActive || document.hidden || polling) return;
  polling = true;
  clearTimeout(pollTimer);
  const version = mutationVersion;
  pollController = new AbortController();
  const deadline = setTimeout(() => pollController.abort(), 10000);
  try {
    const payload = await requestJSON('/api/jobs', { signal: pollController.signal });
    if (!Array.isArray(payload.jobs)) throw new Error('任务列表暂时不可用。');
    if (version === mutationVersion) renderJobs(payload.jobs);
    feedback(jobsSection.querySelector('[data-poll-feedback]'), '');
  } catch (error) {
    if (pageActive && !document.hidden) feedback(jobsSection.querySelector('[data-poll-feedback]'), '任务更新暂时中断，正在重试。你也可以刷新页面。', true);
  } finally {
    clearTimeout(deadline);
    polling = false;
    if (pageActive && !document.hidden) pollTimer = setTimeout(refreshJobs, 2000);
  }
}

if (archiveForm) {
  const url = archiveForm.elements.url;
  const file = archiveForm.elements.file;
  const library = archiveForm.elements.library_id;
  const modes = [...document.querySelectorAll('[name="input_mode"]')];
  const configured = document.body.dataset.komgaEnabled === 'true';
  let submitting = false;
  function applyMode() {
    const upload = modes.find(mode => mode.checked).value === 'upload';
    archiveForm.action = upload ? '/api/jobs/upload' : '/api/jobs/url';
    archiveForm.enctype = upload ? 'multipart/form-data' : 'application/x-www-form-urlencoded';
    for (const panel of archiveForm.querySelectorAll('[data-source-panel]')) panel.hidden = (panel.dataset.sourcePanel === 'upload') !== upload;
    url.disabled = submitting || upload;
    url.required = !upload;
    file.disabled = submitting || !upload;
    file.required = upload;
    library.disabled = submitting || !configured;
    for (const mode of modes) mode.disabled = submitting;
  }
  for (const mode of modes) mode.addEventListener('change', applyMode);
  file.addEventListener('change', () => {
    const selected = file.files[0];
    archiveForm.querySelector('[data-file-description]').textContent = selected ? `${selected.name} / ${formatSize(selected.size)}` : '尚未选择文件';
  });
  archiveForm.addEventListener('submit', async event => {
    event.preventDefault();
    if (submitting) return;
    const upload = modes.find(mode => mode.checked).value === 'upload';
    const receipt = document.querySelector('#submit-feedback');
    const status = receipt.querySelector('[data-submit-message]');
    const link = receipt.querySelector('[data-testid="view-submitted-job"]');
    receipt.hidden = false;
    link.hidden = true;
    if (!configured) {
      feedback(status, '请先在书库设置中完成 Komga 配置。', true);
      return;
    }
    if (upload && file.files[0]?.size > 64 * 1024 * 1024) {
      feedback(status, '压缩包超过 64 MiB，请选择较小的文件。', true);
      return;
    }
    const data = new FormData(archiveForm);
    data.delete('input_mode');
    const options = { method: 'POST' };
    if (upload) options.body = data;
    else {
      data.delete('file');
      options.headers = { 'Content-Type': 'application/json' };
      options.body = JSON.stringify(Object.fromEntries(data));
    }
    submitting = true;
    applyMode();
    archiveForm.setAttribute('aria-busy', 'true');
    archiveForm.querySelector('.metadata-fields').disabled = true;
    const button = archiveForm.querySelector('[type="submit"]');
    button.disabled = true;
    archiveForm.querySelector('[data-submit-label]').textContent = upload ? '正在上传…' : '正在提交…';
    feedback(status, upload ? '正在上传压缩包，请保持此页面打开。' : '正在创建归档任务…');
    try {
      const job = await requestJSON(upload ? '/api/jobs/upload' : '/api/jobs/url', options);
      if (!job.id) throw new Error('未收到有效的任务信息，请刷新任务记录后确认。');
      feedback(status, '任务已提交，完成后会自动入库。');
      link.hidden = false;
      if (upload) {
        file.value = '';
        archiveForm.querySelector('[data-file-description]').textContent = '尚未选择文件';
      } else url.value = '';
    } catch (error) {
      feedback(status, error.message || '提交失败，请重试。', true);
    } finally {
      submitting = false;
      applyMode();
      archiveForm.removeAttribute('aria-busy');
      archiveForm.querySelector('.metadata-fields').disabled = false;
      button.disabled = !configured;
      archiveForm.querySelector('[data-submit-label]').textContent = '生成并入库';
    }
  });
  applyMode();
}

if (list) {
  list.addEventListener('click', async event => {
    const control = event.target.closest('[data-action]');
    if (!control) return;
    event.preventDefault();
    const row = control.closest('[data-job-id]');
    const id = row.dataset.jobId;
    if (busyJobs.has(id)) return;
    const action = control.dataset.action;
    if (!['cancel', 'retry'].includes(action)) return;
    const status = row.querySelector('[data-job-feedback]');
    busyJobs.add(id);
    mutationVersion += 1;
    row.setAttribute('aria-busy', 'true');
    for (const button of row.querySelectorAll('button')) button.disabled = true;
    feedback(status, '正在处理…');
    try {
      const payload = await requestJSON(`/api/jobs/${encodeURIComponent(id)}/${action}`, { method: 'POST' });
      if (!payload.id) throw new Error('未收到有效的任务信息，请刷新后确认。');
      updateRow(row, payload);
      feedback(status, action === 'cancel' ? '已提交取消请求，状态会继续更新。' : '已重新加入处理队列。');
    } catch (error) {
      feedback(status, error.message || '操作失败，请重试。', true);
    } finally {
      busyJobs.delete(id);
      mutationVersion += 1;
      row.removeAttribute('aria-busy');
      for (const button of row.querySelectorAll('button')) button.disabled = false;
      refreshJobs();
    }
  });
  document.addEventListener('visibilitychange', () => {
    clearTimeout(pollTimer);
    if (document.hidden) pollController?.abort();
    else refreshJobs();
  });
  window.addEventListener('pagehide', () => {
    pageActive = false;
    clearTimeout(pollTimer);
    pollController?.abort();
  });
  window.addEventListener('pageshow', () => {
    pageActive = true;
    refreshJobs();
  });
  refreshJobs();
}

if (settingsForm) {
  const libraryList = settingsForm.querySelector('[data-library-list]');
  const libraryTemplate = settingsForm.querySelector('[data-library-template]');
  const addButton = settingsForm.querySelector('[data-add-library]');
  const fields = settingsForm.querySelector('.komga-fields');
  const status = settingsForm.querySelector('[data-settings-feedback]');
  let nextLibraryIndex = libraryList.children.length;

  function addLibrary(library = {}) {
    const row = libraryTemplate.content.firstElementChild.cloneNode(true);
    const index = ++nextLibraryIndex;
    row.dataset.libraryId = library.id || '';
    for (const name of ['name', 'path']) {
      const input = row.querySelector(`[data-library-${name}]`);
      input.id = `library-${name}-${index}`;
      input.value = library[name] || '';
      row.querySelector(`[data-library-${name}-label]`).htmlFor = input.id;
    }
    row.querySelector('[data-remove-library]').setAttribute('aria-label', `移除${library.name || '此'}书库配置`);
    libraryList.append(row);
    addButton.disabled = libraryList.children.length >= 20;
    return row;
  }

  addButton.disabled = libraryList.children.length >= 20;
  addButton.addEventListener('click', () => {
    if (libraryList.children.length >= 20) return;
    addLibrary().querySelector('[data-library-name]').focus();
    feedback(status, '已添加书库，请填写名称和目录后保存。');
  });
  libraryList.addEventListener('input', event => {
    if (!event.target.matches('[data-library-name]')) return;
    const row = event.target.closest('[data-library-row]');
    row.querySelector('[data-remove-library]').setAttribute('aria-label', `移除${event.target.value.trim() || '此'}书库配置`);
  });
  libraryList.addEventListener('click', event => {
    const button = event.target.closest('[data-remove-library]');
    if (!button) return;
    const row = button.closest('[data-library-row]');
    const index = [...libraryList.children].indexOf(row);
    row.remove();
    if (!libraryList.children.length) addLibrary();
    addButton.disabled = libraryList.children.length >= 20;
    libraryList.children[Math.min(index, libraryList.children.length - 1)].querySelector('[data-library-name]').focus();
    feedback(status, '已移除书库配置，保存后生效。');
  });

  settingsForm.addEventListener('submit', async event => {
    event.preventDefault();
    const button = settingsForm.querySelector('[type="submit"]');
    if (button.disabled) return;
    const data = new FormData(settingsForm);
    const libraries = [...libraryList.children].map(row => ({
      id: row.dataset.libraryId || '',
      name: row.querySelector('[data-library-name]').value.trim(),
      path: row.querySelector('[data-library-path]').value.trim(),
    }));
    const values = { komga_url: data.get('komga_url').trim(), libraries };
    button.disabled = true;
    fields.disabled = true;
    button.textContent = '正在保存…';
    feedback(status, '正在保存书库配置…');
    try {
      const payload = await requestJSON('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(values) });
      const validLibraries = Array.isArray(payload.libraries) && payload.libraries.length <= 20 && payload.libraries.every(library => library && typeof library.id === 'string' && library.id && typeof library.name === 'string' && typeof library.path === 'string');
      if (typeof payload.configured !== 'boolean' || typeof payload.komga_url !== 'string' || !validLibraries) throw new Error('未收到已保存的配置，请刷新后确认。');
      settingsForm.elements.komga_url.value = payload.komga_url;
      libraryList.replaceChildren();
      for (const library of payload.libraries) addLibrary(library);
      if (!libraryList.children.length) addLibrary();
      document.body.dataset.komgaEnabled = String(payload.configured);
      document.querySelector('[data-komga-state]').textContent = payload.configured ? `已配置 ${payload.libraries.length} 个书库` : '尚未配置';
      const link = document.querySelector('[data-open-komga]');
      link.hidden = !payload.komga_url;
      link.href = payload.komga_url || '#';
      feedback(status, 'Komga 配置已保存。');
    } catch (error) {
      feedback(status, error.message || '保存失败，请重试。', true);
    } finally {
      button.disabled = false;
      fields.disabled = false;
      button.textContent = '保存 Komga 配置';
    }
  });
}
