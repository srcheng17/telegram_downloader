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
        inputHandler: null,
        hydrateController: null,
        saveController: null,
        editVersion: 0,
    };
    win.__telegraphSettingsModuleState = state;

    async function hydrateSettings(form) {
        const controller = new AbortController();
        state.hydrateController = controller;
        try {
            const { response, payload } = await settingsApi.getSettings({ signal: controller.signal });
            if (controller.signal.aborted || state.form !== form) return;
            if (!response.ok || !payload) {
                return;
            }
            applySettingsSnapshot(win, form, payload);
        } catch (error) {
            if (!controller.signal.aborted) console.error('Failed to load settings snapshot:', error);
        } finally {
            if (state.hydrateController === controller) state.hydrateController = null;
        }
    }

    async function submitSettings(event) {
        event.preventDefault();
        if (state.saveController) return;
        const form = event.currentTarget;
        const controller = new AbortController();
        const editVersion = state.editVersion;
        state.saveController = controller;
        if (state.hydrateController) state.hydrateController.abort();
        const button = form.querySelector('button[type="submit"]');
        if (button) button.disabled = true;
        const payload = buildSettingsPayload(form);
        showSettingsFeedback(doc, '正在保存设置...', 'info');
        try {
            const { response, payload: result } = await settingsApi.saveSettings(payload, { signal: controller.signal });
            if (controller.signal.aborted || state.form !== form) return;
            if (!response.ok || !result) {
                showSettingsFeedback(doc, `保存失败（${response.status}）`, 'error');
                return;
            }
            if (state.editVersion === editVersion) {
                applySettingsSnapshot(win, form, result);
            } else {
                win.__telegraphSettingsState = { downloadActionMode: result.download_action_mode || 'browser' };
            }
            showSettingsFeedback(doc, state.editVersion === editVersion ? '设置已保存。' : '已保存提交时的设置，后续编辑尚未保存。', 'success');
        } catch (error) {
            if (controller.signal.aborted || state.form !== form) return;
            console.error('Failed to save settings snapshot:', error);
            showSettingsFeedback(doc, '设置保存失败。', 'error');
        } finally {
            if (state.saveController === controller) state.saveController = null;
            if (state.form === form && button) button.disabled = false;
        }
    }

    function mount() {
        const form = doc.querySelector('form.settings-form');
        if (!form) {
            return;
        }
        if (state.form === form) return;
        unmount();
        state.form = form;
        const button = form.querySelector('button[type="submit"]');
        if (button) button.disabled = false;
        state.editVersion = 0;
        state.inputHandler = () => {
            state.editVersion += 1;
            if (state.hydrateController) state.hydrateController.abort();
        };
        form.addEventListener('input', state.inputHandler);
        state.submitHandler = submitSettings;
        form.addEventListener('submit', state.submitHandler);
        hydrateSettings(form);
    }

    function unmount() {
        if (state.hydrateController) state.hydrateController.abort();
        if (state.saveController) state.saveController.abort();
        state.hydrateController = null;
        state.saveController = null;
        if (state.form && state.inputHandler) state.form.removeEventListener('input', state.inputHandler);
        state.inputHandler = null;
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
