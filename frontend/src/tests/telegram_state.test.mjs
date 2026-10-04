import test from 'node:test';
import assert from 'node:assert/strict';
import { acceptAttempt } from '../telegram/state.js';
const now = Date.now();
const sample = extra => ({ attempt_id: 'a'.repeat(32), seq: 1, revision: 1, state: 'waiting_qr', expires_at: new Date(now + 60000).toISOString(), ...extra });
test('Telegram attempt rejects other attempts and stale sequence/revision', () => {
    const current = acceptAttempt(null, sample(), now);
    for (const value of [sample(), sample({ seq: 2 }), sample({ seq: 2, revision: 2, attempt_id: 'b'.repeat(32) }), sample({ state: 'unknown' })]) assert.equal(acceptAttempt(current, value, now), current);
    assert.equal(acceptAttempt(current, sample({ seq: 2, revision: 2, state: 'password_required' }), now).state, 'password_required');
});
test('Telegram QR only survives a current waiting state and bounded PNG value', () => {
    const qr = { qr: 'data:image/png;base64,eA==', qr_expires_at: new Date(now + 30000).toISOString() };
    assert.equal(acceptAttempt(null, sample(qr), now).qr, qr.qr);
    for (const values of [{ state: 'connected' }, { state: 'password_required' }, { expires_at: new Date(now - 1).toISOString() }, { qr_expires_at: new Date(now - 1).toISOString() }, { qr: 'https://outside.example/qr' }, { qr: `data:image/png;base64,${'A'.repeat(25000)}` }]) assert.equal(acceptAttempt(null, sample({ ...qr, ...values }), now).qr, '');
});
