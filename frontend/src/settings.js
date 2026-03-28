import { createTasksApi } from './shared/api/tasks_api.js';
import { createSettingsApi } from './settings/api.js';
import { applySettingsSnapshot, buildSettingsPayload } from './settings/state.js';

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

export function createSettingsModule(win = window, doc = document) {
    const api = createTasksApi((url, options) => win.fetch(url, options));
    const settingsApi = createSettingsApi(api);
    const state = win.__telegraphSettingsModuleState || {
        form: null,
        submitHandler: null,
    };
    win.__telegraphSettingsModuleState = state;

    async function hydrateSettings(form) {
        try {
            const { response, payload } = await settingsApi.getSettings();
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
            const { response, payload: result } = await settingsApi.saveSettings(payload);
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
