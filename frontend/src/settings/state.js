export function readSelectedDownloadActionMode(form) {
    if (!form || typeof form.querySelector !== 'function') {
        return 'browser';
    }
    const checked = form.querySelector('input[name="download_action_mode"]:checked');
    return checked ? String(checked.value || '').trim() || 'browser' : 'browser';
}

export function setSelectedDownloadActionMode(form, mode) {
    if (!form || typeof form.querySelectorAll !== 'function') {
        return;
    }
    form.querySelectorAll('input[name="download_action_mode"]').forEach((radio) => {
        radio.checked = String(radio.value || '').trim() === String(mode || '').trim();
    });
}

export function buildSettingsPayload(form) {
    return {
        timeout: Number(form.querySelector('#timeout')?.value || 0),
        retries: Number(form.querySelector('#retries')?.value || 0),
        image_concurrency: Number(form.querySelector('#image_concurrency')?.value || 0),
        download_action_mode: readSelectedDownloadActionMode(form),
    };
}

export function applySettingsSnapshot(win, form, snapshot) {
    if (!snapshot || typeof snapshot !== 'object') {
        return;
    }
    const timeoutInput = form.querySelector('#timeout');
    const retriesInput = form.querySelector('#retries');
    const imageConcurrencyInput = form.querySelector('#image_concurrency');
    if (timeoutInput) {
        timeoutInput.value = String(snapshot.timeout ?? timeoutInput.value ?? '');
    }
    if (retriesInput) {
        retriesInput.value = String(snapshot.retries ?? retriesInput.value ?? '');
    }
    if (imageConcurrencyInput) {
        imageConcurrencyInput.value = String(snapshot.image_concurrency ?? imageConcurrencyInput.value ?? '');
    }
    setSelectedDownloadActionMode(form, snapshot.download_action_mode || 'browser');
    win.__telegraphSettingsState = {
        ...(win.__telegraphSettingsState || {}),
        downloadActionMode: snapshot.download_action_mode || 'browser',
    };
}

