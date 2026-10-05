function setActionButton(button, options, win) {
    if (!button) {
        return false;
    }

    const resolvedOptions = options && typeof options === 'object' ? options : {};
    const isVisible = Boolean(resolvedOptions.visible);
    if (!isVisible) {
        button.classList.add('is-hidden');
        button.onclick = null;
        button.disabled = false;
        return false;
    }

    if (resolvedOptions.label) {
        button.textContent = resolvedOptions.label;
    }

    button.disabled = false;
    button.classList.remove('is-hidden');

    if (typeof resolvedOptions.onClick === 'function') {
        button.onclick = resolvedOptions.onClick;
        return true;
    }

    const targetUrl = resolvedOptions.url ? String(resolvedOptions.url).trim() : '';
    if (!targetUrl) {
        button.classList.add('is-hidden');
        button.onclick = null;
        return false;
    }

    button.onclick = () => {
        win.location.href = targetUrl;
    };
    return true;
}

export function resetHomeActionButtons(doc, win = window) {
    const actions = doc.getElementById('download-actions');
    const logsButton = doc.getElementById('download-action-logs');
    const downloadButton = doc.getElementById('download-action-download');
    const forceButton = doc.getElementById('download-action-force');
    const useExistingButton = doc.getElementById('download-action-use-existing');

    setActionButton(logsButton, { visible: false }, win);
    setActionButton(downloadButton, { visible: false }, win);
    setActionButton(forceButton, { visible: false }, win);
    setActionButton(useExistingButton, { visible: false }, win);

    if (!actions) {
        return;
    }
    actions.classList.add('is-hidden');
    actions.setAttribute('aria-hidden', 'true');
}

export function showHomeActionButtons(doc, options, win = window) {
    const actions = doc.getElementById('download-actions');
    if (!actions) {
        return;
    }
    const resolved = options && typeof options === 'object' ? options : {};
    const logsButton = doc.getElementById('download-action-logs');
    const downloadButton = doc.getElementById('download-action-download');
    const forceButton = doc.getElementById('download-action-force');
    const useExistingButton = doc.getElementById('download-action-use-existing');

    const hasLogs = setActionButton(logsButton, {
        visible: Boolean(resolved.logsUrl),
        url: resolved.logsUrl,
        label: '查看任务',
    }, win);
    const hasDownload = setActionButton(downloadButton, {
        visible: Boolean(resolved.downloadUrl),
        url: resolved.downloadUrl,
        label: resolved.downloadLabel || '立即下载',
    }, win);
    const hasForce = setActionButton(forceButton, {
        visible: typeof resolved.onForce === 'function',
        onClick: resolved.onForce,
        label: '生成新的CBZ',
    }, win);
    const hasUseExisting = setActionButton(useExistingButton, {
        visible: typeof resolved.onUseExisting === 'function',
        onClick: resolved.onUseExisting,
        label: '取消并下载已有文件',
    }, win);

    if (hasLogs || hasDownload || hasForce || hasUseExisting) {
        actions.classList.remove('is-hidden');
        actions.setAttribute('aria-hidden', 'false');
        return;
    }
    actions.classList.add('is-hidden');
    actions.setAttribute('aria-hidden', 'true');
}

