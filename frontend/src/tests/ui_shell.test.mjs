import test from 'node:test';
import assert from 'node:assert/strict';
import { createWorkspaceShell } from '../ui_shell/index.js';
import { createDocument } from './metadata_dom_fixture.mjs';
function fixture(adapters) {
    const doc = createDocument();
    const root = doc.createElement('div');
    const host = doc.createElement('div'); host.id = 'metadata-editor';
    const status = doc.createElement('p'); status.dataset.metadataStatus = '';
    root.appendChild(host); root.appendChild(status);
    return { root, status, shell: createWorkspaceShell({ root, adapters, doc }) };
}
const schema = { schema_version: 1, definitions_version: 'v1', definitions: { title: { key: 'title', label: '标题', type: 'string', enabled: true, editable: true } } };

test('missing production adapter stays unavailable rather than fabricating a ready editor', async () => {
    const { root, status, shell } = fixture({});
    await shell.mount();
    assert.equal(root.dataset.metadataState, 'error');
    assert.match(status.textContent, /不可用/);
    assert.throws(() => shell.getDocument(), /尚未就绪/);
});

test('schema response after unmount cannot re-create sensitive state', async () => {
    let finish;
    const { root, shell } = fixture({ loadSchema: () => new Promise(resolve => { finish = resolve; }) });
    const pending = shell.mount();
    shell.unmount();
    finish(schema);
    await pending;
    assert.equal(shell.getDraft(), null);
    assert.equal(root.dataset.metadataState, 'idle');
});

test('fake adapter only drives injected state: cancel leaving retains draft, accepted disposal invalidates it', async () => {
    const { shell } = fixture({ loadSchema: async () => schema });
    await shell.mount();
    const draft = shell.getDraft();
    draft.setField('title', '作品');
    assert.equal(shell.canLeave(() => false), false);
    assert.equal(shell.getDocument().fields.title.value, '作品');
    assert.equal(shell.canLeave(() => true), true);
    shell.unmount();
    assert.throws(() => draft.getSnapshot(), /销毁/);
});

test('retrying definitions never discards an edited draft', async () => {
    let loads = 0;
    const { shell } = fixture({ loadSchema: async () => { loads += 1; return schema; } });
    await shell.mount();
    shell.getDraft().setField('title', '保留');
    assert.equal(await shell.retry(), false);
    assert.equal(loads, 1);
    assert.equal(shell.getDocument().fields.title.value, '保留');
});
