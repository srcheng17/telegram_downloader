import { decodeMetadataSchema } from '../shared/metadata/schema.js';
import { createMetadataDraft } from '../shared/metadata/draft.js';
import { createMetadataEditor } from '../shared/metadata/editor.js';

// Network/auth transport is injected by the application. No production fake fallback.
export function createWorkspaceShell({ root, adapters = {}, document: initialDocument, doc = globalThis.document }) {
    let generation = 0;
    let controller;
    let editor;
    let draft;
    let mounted = false;
    const status = root?.querySelector('[data-metadata-status]');
    const host = root?.querySelector('#metadata-editor');
    function showStatus(message, state) {
        if (status) status.textContent = message;
        if (root) root.dataset.metadataState = state;
    }
    async function mount() {
        if (mounted || !root || !host) return;
        mounted = true;
        const run = ++generation;
        controller = new AbortController();
        showStatus('正在读取元数据字段…', 'loading');
        try {
            if (typeof adapters.loadSchema !== 'function') throw new Error('元数据编辑暂不可用，请稍后重试。');
            const payload = await adapters.loadSchema({ signal: controller.signal });
            if (!mounted || run !== generation || controller.signal.aborted) return;
            const schema = decodeMetadataSchema(payload);
            draft = createMetadataDraft({ document: initialDocument, definitions: schema.definitions, definitionsVersion: schema.definitions_version, limits: schema.limits });
            editor = createMetadataEditor({ root: host, draft, schema, doc });
            editor.mount();
            showStatus('可直接填写，也可逐项采用识别或书目候选。', 'ready');
            adapters.onReady?.({ draft, editor, schema });
        } catch (error) {
            if (!mounted || run !== generation || controller?.signal.aborted) return;
            showStatus(error.message || '元数据字段读取失败，请重试。', 'error');
        }
    }
    function unmount() {
        mounted = false;
        generation += 1;
        controller?.abort();
        controller = null;
        editor?.unmount();
        editor = null;
        draft?.dispose();
        draft = null;
        showStatus('', 'idle');
    }
    return {
        mount,
        unmount,
        async retry() {
            if (draft?.isDirty() || editor?.hasPendingEdits()) {
                showStatus('已保留当前草稿，请先处理未提交修改再重新读取字段。', 'ready');
                return false;
            }
            unmount();
            await mount();
            return root?.dataset.metadataState === 'ready';
        },
        getDraft: () => draft,
        markClean(revision) {
            if (!draft) return;
            draft.markClean(revision);
            if (draft.getSnapshot().revision === revision) editor?.markClean();
        },
        hasUnsavedChanges: () => Boolean(draft?.isDirty() || editor?.hasPendingEdits()),
        getDocument() {
            if (!draft || root.dataset.metadataState !== 'ready') throw new Error('元数据字段尚未就绪。');
            if (editor?.hasErrors()) throw new Error('请先修正元数据字段中的错误。');
            return draft.getSnapshot();
        },
        showCandidate(candidate, options) { editor?.showCandidate(candidate, options); },
        canLeave(confirmDiscard) { return (!draft?.isDirty() && !editor?.hasPendingEdits()) || Boolean(confirmDiscard?.()); },
    };
}

export { createNavigationDisclosure } from '../shared/navigation.js';
