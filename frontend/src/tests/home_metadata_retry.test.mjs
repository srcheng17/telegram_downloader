import test from 'node:test';
import assert from 'node:assert/strict';
import { retryMetadataWorkspace } from '../home/index.js';

test('schema retry preserves unadopted OCR edits and the live draft', async () => {
    const calls = [];
    const state = { ocr: { isDirty: () => true, dispose: () => calls.push('dispose OCR') }, metadataSearch: { unmount: () => calls.push('dispose search') }, shell: { hasUnsavedChanges: () => false, retry: () => calls.push('retry') } };
    assert.equal(await retryMetadataWorkspace(state, message => calls.push(message)), false);
    assert.equal(calls.length, 1); assert.match(calls[0], /保留/); assert.ok(state.ocr);
});
test('clean schema retry cancels source modules before disposing the draft', async () => {
    const calls = [];
    const state = { ocr: { isDirty: () => false, dispose: () => calls.push('dispose OCR') }, metadataSearch: { unmount: () => calls.push('dispose search') }, shell: { hasUnsavedChanges: () => false, retry: async () => { calls.push('retry'); return true; } } };
    assert.equal(await retryMetadataWorkspace(state, () => {}), true);
    assert.deepEqual(calls, ['dispose OCR', 'dispose search', 'retry']); assert.equal(state.ocr, null); assert.equal(state.metadataSearch, null);
});
