import test from 'node:test';
import assert from 'node:assert/strict';
import { createKomgaEditor } from '../komga/edit.js';
import { createDocument, walk } from './metadata_dom_fixture.mjs';

const tick = () => new Promise(resolve => setImmediate(resolve));
const ok = payload => ({ response: { ok: true, status: 200 }, payload });

function fixture(overrides = {}, detailChanges = {}) {
    const doc = createDocument();
    const container = doc.createElement('section');
    const controller = new AbortController();
    const definitions = {
        title: { key: 'title', label: '标题', type: 'string', enabled: true, editable: true, max_bytes: 4096, export_status: 'mapped' },
        series: { key: 'series', label: '系列', type: 'string', enabled: true, editable: true, max_bytes: 4096, export_status: 'mapped' },
        summary: { key: 'summary', label: '简介', type: 'string', enabled: true, editable: true, max_bytes: 16384, export_status: 'mapped' },
        'custom.user.note': { key: 'custom.user.note', label: '自定义备注', type: 'string', enabled: true, editable: true, max_bytes: 100, export_status: 'internal_only' },
    };
    const detail = {
        book: { id: 'book1' }, source_version: 'source-sha',
        schema: { schema_version: 1, definitions_version: 'standard-v1', definitions, limits: { document_bytes: 262144 } },
        document: { schema_version: 1, definitions_version: 'standard-v1', revision: 0, fields: {
            title: { state: 'value', value: '旧标题', revision: 0, manual_locked: false, provenance: [] },
            summary: { state: 'value', value: '旧简介', revision: 0, manual_locked: false, provenance: [] },
        }, definition_snapshot: {} },
        fields: [
            { key: 'title', label: '标题', type: 'string', can_set: true, can_clear: false, reason: 'no_clear' },
            { key: 'summary', label: '简介', type: 'string', can_set: true, can_clear: true },
            { key: 'custom.user.note', label: '自定义备注', type: 'string', can_set: false, can_clear: false, reason: 'internal_only' },
        ],
        has_comicinfo: true, page_count: 3, warnings: [], can_save: true,
        ...detailChanges,
    };
    const requests = { preview: [], save: [], operation: [], retry: [], restore: [] };
    const operation = { id: 'op1', book_id: 'book1', state: 'sync_pending', file_committed: true, file_no_change: false, file_restored: false, projection_applicable: true, projection_consistent: false, analyze_verified: false, last_error_code: '', available_actions: ['retry_sync', 'restore'] };
    const api = {
        edit: async () => ok(detail),
        preview: async (_id, input, options) => { requests.preview.push({ input, signal: options.signal }); return ok({ preview_token: 'token1', expires_at: '2026-10-05T00:00:00Z', file_changed: true, diffs: [{ key: 'title', action: 'set', before: '旧标题', after: '新标题' }], warnings: [], can_save: true }); },
        save: async (_id, input, options) => { requests.save.push({ input, signal: options.signal }); return ok({ operation }); },
        operation: async id => { requests.operation.push(id); return ok({ operation }); },
        retrySync: async id => { requests.retry.push(id); return ok({ operation: { ...operation, state: 'current_value_consistent', projection_consistent: true, available_actions: ['restore'] } }); },
        restore: async id => { requests.restore.push(id); return ok({ operation: { ...operation, state: 'restore_needed', available_actions: [] } }); },
        ...overrides,
    };
    let confirmation = true;
    const win = { crypto: { randomUUID: () => 'safe-idempotency-uuid' }, confirm: () => confirmation };
    const editor = createKomgaEditor({ win, doc, api, book: { id: 'book1' }, container, signal: controller.signal });
    const findButton = label => walk(container).find(node => node.tagName === 'BUTTON' && node.textContent === label);
    const field = key => walk(container).find(node => node.getAttribute('data-field-key') === key);
    const input = key => field(key)?.children.find(node => ['INPUT', 'TEXTAREA', 'SELECT'].includes(node.tagName));
    const submitPreview = () => { const form = walk(container).find(node => node.tagName === 'FORM'); for (const fn of form.listeners.get('submit')) fn({ preventDefault() {} }); };
    return { editor, container, controller, requests, api, win, detail, operation, findButton, field, input, submitPreview, setConfirmation: value => { confirmation = value; } };
}

test('file editor renders safe field matrix, previews exact changes, and saves one reviewed revision', async () => {
    const f = fixture(); await tick();
    assert.equal(f.input('title').value, '旧标题');
    assert.equal(f.input('summary').value, '旧简介');
    assert.equal(f.input('custom.user.note'), undefined);
    assert.equal(walk(f.field('title')).some(node => node.tagName === 'BUTTON' && node.textContent === '明确清空'), false);
    f.input('title').value = '新标题'; f.input('title').emit('input');
    const summaryClear = walk(f.field('summary')).find(node => node.tagName === 'BUTTON' && node.textContent === '明确清空');
    summaryClear.emit('click');
    assert.equal(f.editor.hasUnsavedChanges(), true);
    f.submitPreview(); await tick();
    assert.deepEqual(f.requests.preview[0].input, { source_version: 'source-sha', definitions_version: 'standard-v1', changes: [
        { key: 'title', state: 'value', value: '新标题' }, { key: 'summary', state: 'cleared' },
    ] });
    assert.equal(f.findButton('确认保存到 CBZ').disabled, false);
    f.findButton('确认保存到 CBZ').emit('click'); await tick();
    assert.equal(f.requests.save[0].input.preview_token, 'token1');
    assert.equal(f.requests.save[0].input.idempotency_key, 'safe-idempotency-uuid');
    assert.equal(f.editor.hasUnsavedChanges(), false);
    assert.equal(f.findButton('预览写回差异').disabled, true);
    const summary = walk(f.container).filter(node => node.tagName === 'LI').map(node => node.textContent);
    assert.match(summary.join(' '), /ComicInfo 已保存/);
    assert.match(summary.join(' '), /待同步/);
    assert.match(summary.join(' '), /无法证明/);
    f.editor.dispose();
});

test('a late preview cannot authorize save after the draft changes', async () => {
    let complete;
    const f = fixture({ preview: async (_id, input, options) => { f.requests.preview.push({ input, signal: options.signal }); return new Promise(resolve => { complete = resolve; }); } });
    await tick();
    f.input('title').value = '第一次修改'; f.input('title').emit('input');
    f.submitPreview(); await tick();
    f.input('title').value = '第二次修改'; f.input('title').emit('input');
    complete(ok({ preview_token: 'stale', diffs: [], warnings: [], can_save: true })); await tick();
    assert.equal(f.findButton('确认保存到 CBZ').disabled, true);
    assert.equal(f.requests.preview[0].signal.aborted, true);
    assert.equal(f.input('title').value, '第二次修改');
    f.editor.dispose();
});

test('edits made during save stay dirty after the submitted revision is written', async () => {
    let complete;
    const f = fixture({ save: async (_id, input, options) => { f.requests.save.push({ input, signal: options.signal }); return new Promise(resolve => { complete = resolve; }); } });
    await tick();
    f.input('title').value = '提交版本'; f.input('title').emit('input');
    f.submitPreview(); await tick();
    f.findButton('确认保存到 CBZ').emit('click'); await tick();
    f.input('title').value = '后续修改'; f.input('title').emit('input');
    complete(ok({ operation: f.operation })); await tick();
    assert.equal(f.editor.hasUnsavedChanges(), true);
    assert.equal(f.input('title').value, '后续修改');
    assert.match(walk(f.container).find(node => node.className === 'inline-feedback').textContent, /随后修改的草稿仍保留/);
    f.editor.dispose();
});

test('lost save response retries the same reviewed snapshot after later edits', async () => {
    let attempts = 0;
    const f = fixture({ save: async (_id, input, options) => {
        f.requests.save.push({ input, signal: options.signal });
        if (++attempts === 1) throw new Error('connection lost after request');
        return ok({ operation: f.operation });
    } });
    await tick();
    f.input('title').value = '提交版本'; f.input('title').emit('input');
    f.submitPreview(); await tick();
    f.findButton('确认保存到 CBZ').emit('click'); await tick();
    assert.equal(f.findButton('重试确认同一保存操作').hidden, false);
    f.input('title').value = '后续修改'; f.input('title').emit('input');
    f.submitPreview(); await tick();
    assert.equal(f.requests.preview.length, 1);
    f.findButton('重试确认同一保存操作').emit('click'); await tick();
    assert.equal(f.requests.save.length, 2);
    assert.deepEqual(f.requests.save[1].input, f.requests.save[0].input);
    assert.deepEqual(f.requests.save[1].input.changes, [{ key: 'title', state: 'value', value: '提交版本' }]);
    assert.equal(f.input('title').value, '后续修改');
    assert.equal(f.editor.hasUnsavedChanges(), true);
    f.editor.dispose();
});

test('registered save remains pending until an operation refresh confirms the file', async () => {
    const unfinished = { id: 'op1', book_id: 'book1', state: 'prepared', file_committed: false,
        file_no_change: false, file_restored: false, projection_applicable: true, projection_consistent: false, analyze_verified: false, available_actions: [] };
    const f = fixture({
        save: async (_id, input, options) => { f.requests.save.push({ input, signal: options.signal }); return ok({ operation: unfinished }); },
        operation: async id => { f.requests.operation.push(id); return ok({ operation: f.operation }); },
    });
    await tick();
    f.input('title').value = '提交版本'; f.input('title').emit('input');
    f.submitPreview(); await tick();
    f.findButton('确认保存到 CBZ').emit('click'); await tick();
    assert.equal(f.editor.hasUnsavedChanges(), true);
    assert.equal(f.findButton('重试确认同一保存操作').hidden, false);
    assert.equal(f.findButton('确认保存到 CBZ').disabled, true);
    f.findButton('刷新状态').emit('click'); await tick();
    assert.deepEqual(f.requests.operation, ['op1']);
    assert.equal(f.editor.hasUnsavedChanges(), false);
    assert.equal(f.findButton('重试确认同一保存操作').hidden, true);
    assert.equal(f.findButton('预览写回差异').disabled, true);
    f.editor.dispose();
});

test('aborted preparation releases the saved key and keeps the draft editable', async () => {
    let attempts = 0;
    const f = fixture({ save: async () => {
        if (++attempts === 1) throw new Error('response lost');
        return ok({ operation: { ...f.operation, state: 'aborted', file_committed: false,
            projection_consistent: false, available_actions: [] } });
    } });
    await tick();
    f.input('title').value = '新标题'; f.input('title').emit('input');
    f.submitPreview(); await tick();
    f.findButton('确认保存到 CBZ').emit('click'); await tick();
    assert.equal(f.findButton('重试确认同一保存操作').hidden, false);
    f.findButton('重试确认同一保存操作').emit('click'); await tick();
    assert.equal(f.findButton('重试确认同一保存操作').hidden, true);
    assert.equal(f.editor.hasUnsavedChanges(), true);
    assert.match(walk(f.container).find(node => node.className === 'inline-feedback').textContent, /写回前终止/);
    f.submitPreview(); await tick();
    assert.equal(f.requests.preview.length, 2);
    f.editor.dispose();
});

test('restored operation releases an uncertain save and requires file reload', async () => {
    let attempts = 0;
    const f = fixture({ save: async () => {
        if (++attempts === 1) throw new Error('response lost');
        return ok({ operation: { ...f.operation, state: 'restored', file_committed: false,
            file_restored: true, projection_consistent: false, available_actions: [] } });
    } });
    await tick();
    f.input('title').value = '新标题'; f.input('title').emit('input');
    f.submitPreview(); await tick();
    f.findButton('确认保存到 CBZ').emit('click'); await tick();
    f.findButton('重试确认同一保存操作').emit('click'); await tick();
    assert.equal(f.findButton('重试确认同一保存操作').hidden, true);
    assert.equal(f.findButton('预览写回差异').disabled, true);
    assert.equal(f.editor.hasUnsavedChanges(), true);
    assert.match(walk(f.container).find(node => node.className === 'inline-feedback').textContent, /重新读取文件/);
    f.editor.dispose();
});

test('restore-needed operation preserves its ID and blocks a new write after reload', async () => {
    let attempts = 0;
    const f = fixture({ save: async () => {
        if (++attempts === 1) throw new Error('response lost');
        return ok({ operation: { ...f.operation, id: 'op-needs-review', state: 'restore_needed',
            file_committed: false, projection_consistent: false, available_actions: [] } });
    } });
    await tick();
    f.input('title').value = '新标题'; f.input('title').emit('input');
    f.submitPreview(); await tick();
    f.findButton('确认保存到 CBZ').emit('click'); await tick();
    f.findButton('重试确认同一保存操作').emit('click'); await tick();
    assert.equal(f.findButton('重试确认同一保存操作').hidden, true);
    assert.equal(f.editor.hasUnsavedChanges(), true);
    assert.equal(f.findButton('预览写回差异').disabled, true);
    assert.match(walk(f.container).map(node => node.textContent).join(' '), /op-needs-review/);
    f.submitPreview(); await tick();
    assert.equal(f.requests.preview.length, 1);
    f.findButton('重新读取文件').emit('click'); await tick();
    assert.equal(f.findButton('预览写回差异').disabled, true);
    assert.match(walk(f.container).find(node => node.className === 'inline-feedback').textContent, /人工核对/);
    f.editor.dispose();
});

test('only server-authorized operation actions appear; restoration needs an explicit confirmation', async () => {
    const f = fixture(); await tick();
    f.input('title').value = '新标题'; f.input('title').emit('input');
    f.submitPreview(); await tick();
    f.findButton('确认保存到 CBZ').emit('click'); await tick();
    assert.ok(f.findButton('重试 Komga 同步'));
    assert.ok(f.findButton('恢复原件'));
    f.findButton('重试 Komga 同步').emit('click'); await tick();
    assert.deepEqual(f.requests.retry, ['op1']);
    assert.equal(f.findButton('重试 Komga 同步'), undefined);
    f.setConfirmation(false); f.findButton('恢复原件').emit('click'); await tick();
    assert.deepEqual(f.requests.restore, []);
    f.setConfirmation(true); f.findButton('恢复原件').emit('click'); await tick();
    assert.deepEqual(f.requests.restore, ['op1']);
    f.editor.dispose();
});

test('blocked file detail disables all edits and preserves the original', async () => {
    const f = fixture({}, { can_save: false, block_reason: 'unsafe_archive' }); await tick();
    assert.equal(f.input('title').disabled, true);
    assert.equal(f.findButton('预览写回差异').disabled, true);
    assert.equal(f.findButton('确认保存到 CBZ').disabled, true);
    assert.match(walk(f.container).find(node => node.className === 'inline-feedback').textContent, /保真/);
    f.editor.dispose();
});

test('server field and book block reasons are shown with their specific guidance', async () => {
    const locked = fixture({}, { fields: [
        { key: 'title', label: '标题', type: 'string', can_set: false, can_clear: false, reason: 'komga_field_locked' },
        { key: 'summary', label: '简介', type: 'string', can_set: true, can_clear: true },
        { key: 'series', label: '系列', type: 'string', can_set: false, can_clear: false, reason: 'series_scope' },
    ] });
    await tick();
    assert.match(walk(locked.field('title')).map(node => node.textContent).join(' '), /Komga 锁定/);
    assert.match(walk(locked.field('series')).map(node => node.textContent).join(' '), /系列元数据/);
    locked.editor.dispose();

    const barcode = fixture({}, { fields: [
        { key: 'title', label: '标题', type: 'string', can_set: true, can_clear: false },
        { key: 'summary', label: '简介', type: 'string', can_set: true, can_clear: false, reason: 'barcode_isbn_clear_disabled' },
    ] });
    await tick();
    assert.match(walk(barcode.field('summary')).map(node => node.textContent).join(' '), /条码 ISBN 导入/);
    barcode.editor.dispose();

    const blocked = fixture({}, { can_save: false, block_reason: 'backup_unavailable' });
    await tick();
    assert.match(walk(blocked.container).find(node => node.className === 'inline-feedback').textContent, /备份目录不可用/);
    blocked.editor.dispose();
});

test('file-only edits do not claim Komga book metadata was verified', async () => {
    const f = fixture({ save: async () => ok({ operation: {
        id: 'op-file-only', book_id: 'book1', state: 'current_value_consistent', file_committed: true,
        file_no_change: false, file_restored: false, projection_applicable: false, projection_consistent: true,
        analyze_verified: false, available_actions: ['restore'],
    } }) });
    await tick();
    f.input('title').value = '新标题'; f.input('title').emit('input');
    f.submitPreview(); await tick();
    f.findButton('确认保存到 CBZ').emit('click'); await tick();
    const messages = walk(f.container).filter(node => node.tagName === 'LI').map(node => node.textContent).join(' ');
    assert.match(messages, /没有可核对的 Komga 单书字段/);
    assert.doesNotMatch(messages, /Komga 当前元数据值已一致/);
    f.editor.dispose();
});

test('explicit restore reports the restored file state', async () => {
    const f = fixture({ restore: async () => ok({ operation: {
        id: 'op1', book_id: 'book1', state: 'restored', file_committed: false, file_no_change: false,
        file_restored: true, projection_applicable: true, projection_consistent: false,
        analyze_verified: false, available_actions: [],
    } }) });
    await tick();
    f.input('title').value = '新标题'; f.input('title').emit('input');
    f.submitPreview(); await tick();
    f.findButton('确认保存到 CBZ').emit('click'); await tick();
    f.findButton('恢复原件').emit('click'); await tick();
    const messages = walk(f.container).filter(node => node.tagName === 'LI').map(node => node.textContent).join(' ');
    assert.match(messages, /CBZ 已恢复原件/);
    assert.doesNotMatch(messages, /文件尚未确认写入/);
    f.editor.dispose();
});

test('page count correction requires an explicit confirmation bound to preview and save', async () => {
    const f = fixture({}, { page_count_correction_needed: true }); await tick();
    const correction = walk(f.container).find(node => node.tagName === 'INPUT' && node.id === 'komga-correct-page-count');
    assert.ok(correction);
    assert.equal(f.findButton('预览写回差异').disabled, true);
    correction.checked = true; correction.emit('change');
    assert.equal(f.findButton('预览写回差异').disabled, false);
    assert.equal(f.editor.hasUnsavedChanges(), true);
    f.submitPreview(); await tick();
    assert.deepEqual(f.requests.preview[0].input, {
        source_version: 'source-sha', definitions_version: 'standard-v1', changes: [], correct_page_count: true,
    });
    correction.checked = false; correction.emit('change');
    assert.equal(f.findButton('确认保存到 CBZ').disabled, true);
    correction.checked = true; correction.emit('change');
    f.submitPreview(); await tick();
    f.findButton('确认保存到 CBZ').emit('click'); await tick();
    assert.equal(f.requests.save[0].input.correct_page_count, true);
    assert.equal(f.editor.hasUnsavedChanges(), false);
    f.editor.dispose();
});
