import test from 'node:test';
import assert from 'node:assert/strict';
import { createOCRQueue } from '../ocr/queue.js';
const tick = () => new Promise(resolve => setImmediate(resolve));
function harness() {
    const jobs = []; const revoked = []; let active = 0; let maximum = 0; let terminated = 0;
    const queue = createOCRQueue({ validate: async () => ({ width: 10, height: 10 }), createURL: f => `blob:${f.name}`, revokeURL: url => revoked.push(url), id: (() => { let n = 0; return () => `image-${++n}`; })(), recognizerFactory: () => ({
        recognize(file, languages, progress) { active++; maximum = Math.max(maximum, active); return new Promise((resolve, reject) => jobs.push({ file, languages, progress, resolve: text => { active--; resolve(text); }, reject })); },
        async terminate() { active = 0; terminated++; },
    }) });
    return { queue, jobs, revoked, stats: () => ({ maximum, terminated }) };
}
const file = name => ({ name, size: 100 });
test('serial queue retains raw OCR and edited text independently across retry', async () => {
    const { queue, jobs, stats } = harness();
    await queue.add([file('a'), file('b')]); await tick();
    assert.equal(jobs.length, 1); jobs[0].resolve('first'); await tick();
    const a = queue.snapshot().images[0].id; queue.edit(a, 'my correction');
    jobs[1].resolve('second'); await tick(); queue.retry(a); await tick(); jobs[2].resolve('new OCR'); await tick();
    assert.equal(queue.snapshot().images[0].rawText, 'new OCR'); assert.equal(queue.snapshot().images[0].text, 'my correction');
    assert.equal(stats().maximum, 1); await queue.dispose();
});
test('cancellation terminates worker, late callbacks cannot resurrect removed images', async () => {
    const { queue, jobs, revoked, stats } = harness();
    await queue.add([file('a'), file('b')]); await tick(); const a = queue.snapshot().images[0].id;
    queue.remove(a); await tick(); assert.equal(stats().terminated, 1); assert.deepEqual(revoked, ['blob:a']);
    jobs[0].progress(1); jobs[0].resolve('stale'); await tick(); assert.equal(queue.snapshot().images.length, 1);
    jobs[1].resolve('ok'); await tick(); await queue.dispose(); assert.deepEqual(queue.snapshot().images, []);
    assert.deepEqual(revoked, ['blob:a', 'blob:b']);
});
test('partial merge requires consent, order changes revisions and never deduplicates text', async () => {
    const { queue, jobs } = harness(); await queue.add([file('a'), file('b')]); await tick(); jobs[0].resolve('same'); await tick();
    assert.throws(() => queue.merge()); assert.equal(queue.merge({ acceptPartial: true }).text, 'same');
    jobs[1].resolve('same'); await tick(); const before = queue.snapshot();
    queue.move(before.images[1].id, -1); const merged = queue.merge();
    assert.equal(merged.text, 'same\n\nsame'); assert.equal(merged.segments[0].image_id, before.images[1].id);
    assert.ok(queue.snapshot().inputRevision > before.inputRevision); assert.ok(merged.warnings.length); await queue.dispose();
});
test('bulk and concurrent admissions retain accepted files and enforce count', async () => {
    const { queue } = harness(); const inputs = Array.from({ length: 12 }, (_, i) => file(String(i)));
    const results = await Promise.all([queue.add(inputs.slice(0, 6)), queue.add(inputs.slice(6))]);
    assert.equal(queue.snapshot().images.length, 10); assert.equal(results.flat().length, 2); await queue.dispose();
});
