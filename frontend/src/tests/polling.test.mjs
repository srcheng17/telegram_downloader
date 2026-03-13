import test from 'node:test';
import assert from 'node:assert/strict';

import { resolvePollDelay } from '../shared/polling.js';

test('resolvePollDelay backs off and respects visibility state', () => {
    assert.equal(resolvePollDelay({ hidden: true, failures: 0, active: false }), 30000);
    assert.equal(resolvePollDelay({ hidden: false, failures: 3, active: true }), 8000);
    assert.equal(resolvePollDelay({ hidden: false, failures: 0, active: false }), 8000);
});
