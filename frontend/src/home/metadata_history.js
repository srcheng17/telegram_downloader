function readControl(form, name) {
    if (!form || typeof form.querySelector !== 'function') {
        return null;
    }
    return form.querySelector(`[name="${name}"]`);
}

function setControlValue(form, name, value) {
    const control = readControl(form, name);
    if (!control) {
        return;
    }
    control.value = String(value || '');
}

function normalizeTaskType(taskType) {
    return String(taskType || '').trim().toLowerCase() === 'upload' ? 'upload' : 'url';
}

export function applyHistoryEntryToForm(form, entry) {
    const resolved = entry && typeof entry === 'object' ? entry : {};
    const mode = normalizeTaskType(resolved.task_type);

    setControlValue(form, 'author', resolved.author);
    setControlValue(form, 'series_name', resolved.series_name);
    setControlValue(form, 'comic_name', resolved.comic_name);
    setControlValue(form, 'summary', resolved.summary);
    setControlValue(form, 'tags', resolved.tags);
    setControlValue(form, 'genres', resolved.genres);
    if (mode === 'url') {
        setControlValue(form, 'url', resolved.url);
    }
    return mode;
}

function historyLabel(entry) {
    const parts = [entry.author, entry.series_name, entry.comic_name].filter((value) => String(value || '').trim());
    if (!parts.length) {
        return normalizeTaskType(entry.task_type) === 'upload' ? '上传任务' : 'URL 任务';
    }
    return parts.join(' / ');
}

export function renderMetadataHistory(doc, entries, onSelect) {
    const list = doc.getElementById('metadata-history-list');
    if (!list) {
        return;
    }
    list.innerHTML = '';

    const historyEntries = Array.isArray(entries) ? entries : [];
    if (!historyEntries.length) {
        const emptyItem = doc.createElement('li');
        emptyItem.className = 'metadata-history-empty';
        emptyItem.textContent = '暂无最近填写记录。';
        list.appendChild(emptyItem);
        return;
    }

    historyEntries.forEach((entry) => {
        const item = doc.createElement('li');
        const button = doc.createElement('button');
        button.type = 'button';
        button.className = 'metadata-history-item';
        button.textContent = historyLabel(entry);
        button.addEventListener('click', () => {
            if (typeof onSelect === 'function') {
                onSelect(entry);
            }
        });
        item.appendChild(button);
        list.appendChild(item);
    });
}
