import test from 'node:test';
import assert from 'node:assert/strict';

import { toggleFieldHint } from '../home/field_hints.js';

test('toggleFieldHint expands and collapses hidden help text', () => {
    const hint = {
        classList: createClassList(['is-hidden']),
        getAttribute(name) {
            return this[name];
        },
        setAttribute(name, value) {
            this[name] = value;
        },
    };

    toggleFieldHint(hint);
    assert.equal(hint.classList.contains('is-hidden'), false);
    assert.equal(hint['aria-hidden'], 'false');

    toggleFieldHint(hint);
    assert.equal(hint.classList.contains('is-hidden'), true);
    assert.equal(hint['aria-hidden'], 'true');
});

function createClassList(initial = []) {
    const values = new Set(initial);
    return {
        add(...tokens) {
            tokens.forEach((token) => values.add(token));
        },
        remove(...tokens) {
            tokens.forEach((token) => values.delete(token));
        },
        contains(token) {
            return values.has(token);
        },
    };
}
