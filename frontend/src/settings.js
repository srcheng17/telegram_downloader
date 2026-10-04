import { createIntegrationsModule } from './settings/integrations.js';
import { createMetadataFieldsModule } from './settings/metadata_fields.js';
import { createExtractionRulesModule } from './settings/extraction_rules.js';
import { createTelegramModule } from './telegram/index.js';
import { createKomgaConnectionModule } from './settings/komga_connection.js';
import { createTabs } from './shared/tabs.js';
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

export function createSettingsModule(win = window, doc = document, factories = {}) {
    const api = createTasksApi((url, options) => win.fetch(url, options), { win });
    const settingsApi = createSettingsApi(api);
    const modules = new Map();
    let tabs;
    let downloadLoaded = false;
    let connectionsActive = false;
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
        // A failed read is retryable; an active read or edited form must be retained.
        if (downloadLoaded || state.hydrateController || state.saveController || state.editVersion > 0) return;
        const controller = new AbortController();
        state.hydrateController = controller;
        showSettingsFeedback(doc, '正在读取下载设置…', 'info');
        try {
            const { response, payload } = await settingsApi.getSettings({ signal: controller.signal });
            if (controller.signal.aborted || state.form !== form) return;
            if (!response.ok || !payload) {
                showSettingsFeedback(doc, '下载设置读取失败，返回此分类时将重试。当前输入已保留。', 'error');
                return;
            }
            applySettingsSnapshot(win, form, payload);
            downloadLoaded = true;
            showSettingsFeedback(doc, '', null);
        } catch {
            if (!controller.signal.aborted && state.form === form) showSettingsFeedback(doc, '下载设置读取失败，返回此分类时将重试。当前输入已保留。', 'error');
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
            downloadLoaded = true;
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
            if (state.hydrateController && !state.hydrateController.signal.aborted) {
                state.hydrateController.abort();
                showSettingsFeedback(doc, '已保留当前编辑，尚未保存。', 'info');
            }
        };
        form.addEventListener('input', state.inputHandler);
        state.submitHandler = submitSettings;
        form.addEventListener('submit', state.submitHandler);
        const page = doc.getElementById('settings-page');
        if (page) {
            tabs = createTabs({ root: page, win, defaultTab: 'download', hash: true, onChange: selectTab });
            tabs.mount();
        } else selectTab('download');
    }

    function selectTab(id, previous) {
        if (previous === 'connections') {
            modules.get(previous)?.unmount();
            connectionsActive = false;
        } else modules.get(previous)?.pause?.();
        if (id === 'download') {
            void hydrateSettings(state.form);
            return;
        }
        let module = modules.get(id);
        if (!module) {
            const create = factories[id] || (() => {
                if (['sources', 'ai', 'security'].includes(id)) return createIntegrationsModule(win, doc, null, { sections: [id] });
                if (id === 'fields') return createMetadataFieldsModule({ doc, api });
                if (id === 'rules') return createExtractionRulesModule({ doc, api });
                if (id === 'connections') {
                    const telegram = createTelegramModule(win, doc);
                    const komga = createKomgaConnectionModule(win, doc);
                    return { mount() { telegram.mount(); komga.mount(); }, unmount() { telegram.unmount(); komga.unmount(); } };
                }
            });
            module = create();
            if (!module) return;
            modules.set(id, module);
            module.mount();
        } else if (id === 'connections') module.mount();
        else module.resume?.();
        if (id === 'connections') connectionsActive = true;
    }

    function unmount() {
        tabs?.unmount(); tabs = null;
        for (const [id, module] of modules) if (id !== 'connections' || connectionsActive) module.unmount();
        modules.clear();
        downloadLoaded = false;
        connectionsActive = false;
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
