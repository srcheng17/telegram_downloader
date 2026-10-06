import test from 'node:test';
import assert from 'node:assert/strict';
import { createGuidedWorkflow } from '../home/workflow.js';

const deferred = () => { let resolve; const promise = new Promise(done => { resolve = done; }); return { promise, resolve }; };
test('preparing never submits; unchanged inputs return to review without repeating external preparation', async () => {
    let prepares = 0; let revision = 1; const steps = [];
    const workflow = createGuidedWorkflow({ prepare: async () => { prepares += 1; }, getPreparationKey: () => revision, onStep: step => steps.push(step), createKey: () => 'submission-key-0001' });
    await workflow.next(); assert.equal(workflow.step, 'review'); assert.equal(workflow.submission, null);
    workflow.back(); await workflow.next(); assert.equal(prepares, 1);
    workflow.back(); revision += 1; await workflow.next(); assert.equal(prepares, 2);
    assert.deepEqual(steps.slice(0, 2), ['preparing', 'review']);
});
test('returning while preparation is pending aborts it and late completion cannot advance', async () => {
    const result = deferred(); let signal;
    const workflow = createGuidedWorkflow({ prepare: options => { signal = options.signal; return result.promise; }, getPreparationKey: () => 1 });
    const waiting = workflow.next(); workflow.back(); assert.equal(signal.aborted, true);
    result.resolve(); await waiting; assert.equal(workflow.step, 'materials');
});
test('confirmation binds an immutable snapshot and retains the same key after an uncertain response', async () => {
    let keys = 0; const workflow = createGuidedWorkflow({ prepare: async () => {}, getPreparationKey: () => 1, createKey: () => `key-${++keys}` });
    const file = {}; const input = { document: { revision: 1, fields: { title: 'A' } }, context: { inputRevision: 2 }, source: { mode: 'upload', name: 'a.zip' }, target: 'download', file };
    assert.throws(() => workflow.confirm(input), /核对/);
    await workflow.next(); const first = workflow.confirm(input); input.document.fields.title = 'B';
    assert.equal(first.snapshot.document.fields.title, 'A'); assert.equal(first.snapshot.file, file);
    assert.throws(() => workflow.confirm(input), /提交/);
    workflow.failed(); input.document.fields.title = 'A'; assert.equal(workflow.confirm(input).key, first.key);
    workflow.failed(); input.document.revision += 1; assert.notEqual(workflow.confirm(input).key, first.key);
});
test('a submitted workspace returns to its result rather than creating another task', async () => {
    const workflow = createGuidedWorkflow({ prepare: async () => {}, getPreparationKey: () => 1, createKey: () => 'key' });
    await workflow.next(); workflow.confirm({ document: {}, source: {}, target: 'download' }); workflow.complete('task-1');
    workflow.back(); assert.equal(workflow.step, 'review');
    await workflow.next(); assert.equal(workflow.step, 'result'); assert.equal(workflow.taskId, 'task-1');
    assert.throws(() => workflow.confirm({}), /已提交/);
});
