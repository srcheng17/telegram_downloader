import { createTasksApi } from './shared/api/tasks_api.js';

function readSelectedDownloadActionMode(form) {
    if (!form || typeof form.querySelector !== 'function') {
        return 'browser';
    }
    const checked = form.querySelector('input[name="download_action_mode"]:checked');
    return checked ? String(checked.value || '').trim() || 'browser' : 'browser';
}

function setSelectedDownloadActionMode(form, mode) {
    if (!form || typeof form.querySelectorAll !== 'function') {
        return;
    }
    form.querySelectorAll('input[name="download_action_mode"]').forEach((radio) => {
        radio.checked = String(radio.value || '').trim() === String(mode || '').trim();
    });
}

function showSettingsFeedback(doc, message, kind) {
    const feedback = doc.getElementById('settings-feedback');
    if (!feedback) {
        return;
    }
    feedback.textContent = message || '';
    feedback.classList.remove('feedback-success', 'feedback-error', 'feedback-info', 'is-visible');
    if (kind) {
        feedback.classList.add(`feedback-${kind}`);
    }
    if (message) {
        feedback.classList.add('is-visible');
    }
}

function buildSettingsPayload(form) {
    return {
        timeout: Number(form.querySelector('#timeout')?.value || 0),
        retries: Number(form.querySelector('#retries')?.value || 0),
        image_concurrency: Number(form.querySelector('#image_concurrency')?.value || 0),
        download_action_mode: readSelectedDownloadActionMode(form),
    };
}

function applySettingsSnapshot(win, form, snapshot) {
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

export function createSettingsModule(win = window, doc = document) {
    const api = createTasksApi((url, options) => win.fetch(url, options));
    const state = win.__telegraphSettingsModuleState || {
        form: null,
        submitHandler: null,
    };
    win.__telegraphSettingsModuleState = state;

    async function hydrateSettings(form) {
        try {
            const { response, payload } = await api.getJson('/v2/settings', { cache: 'no-store' });
            if (!response.ok || !payload) {
                return;
            }
            applySettingsSnapshot(win, form, payload);
        } catch (error) {
            console.error('Failed to load settings snapshot:', error);
        }
    }

    async function submitSettings(event) {
        event.preventDefault();
        const form = event.currentTarget;
        const payload = buildSettingsPayload(form);
        showSettingsFeedback(doc, '正在保存设置...', 'info');
        try {
            const { response, payload: result } = await api.putJson('/v2/settings', payload, {
                headers: { Accept: 'application/json' },
            });
            if (!response.ok || !result) {
                showSettingsFeedback(doc, `保存失败（${response.status}）`, 'error');
                return;
            }
            applySettingsSnapshot(win, form, result);
            showSettingsFeedback(doc, '设置已保存。', 'success');
        } catch (error) {
            console.error('Failed to save settings snapshot:', error);
            showSettingsFeedback(doc, '设置保存失败。', 'error');
        }
    }

    function mount() {
        const form = doc.querySelector('form.settings-form');
        if (!form) {
            return;
        }
        if (state.form && state.submitHandler) {
            state.form.removeEventListener('submit', state.submitHandler);
        }
        state.form = form;
        state.submitHandler = submitSettings;
        form.addEventListener('submit', state.submitHandler);
        hydrateSettings(form);
    }

    function unmount() {
        if (state.form && state.submitHandler) {
            state.form.removeEventListener('submit', state.submitHandler);
        }
        state.form = null;
        state.submitHandler = null;
    }

    return { mount, unmount };
}

if (!window.TelegraphDownloaderSettings) {
    window.TelegraphDownloaderSettings = createSettingsModule(window, document);
}
