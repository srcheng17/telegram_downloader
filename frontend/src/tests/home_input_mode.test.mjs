import test from 'node:test';
import assert from 'node:assert/strict';

import { applyInputMode, getSelectedMode } from '../home/input_mode.js';

test('removed Telegram source and unknown modes normalize to the visible URL source', () => {
    for (const value of ['telegram', 'invalid', '']) {
        assert.equal(getSelectedMode({ querySelector: () => ({ value }) }), 'url');
        assert.equal(applyInputMode({ getElementById: () => null }, value), 'url');
    }
});

test('applyInputMode toggles url and upload controls', () => {
    const nodes = {
        'url-input-group': {
            classList: createClassList(),
            setAttribute(name, value) { this[name] = value; },
        },
        'archive-input-group': {
            classList: createClassList(['is-hidden']),
            setAttribute(name, value) { this[name] = value; },
        },
        url: { disabled: false, required: true },
        archive_file: { disabled: true, required: false },
    };
    const doc = {
        getElementById(id) {
            return nodes[id] || null;
        },
    };

    applyInputMode(doc, 'upload');
    assert.equal(nodes.url.disabled, true);
    assert.equal(nodes.url.required, false);
    assert.equal(nodes.archive_file.disabled, false);
    assert.equal(nodes.archive_file.required, true);
    assert.equal(nodes['url-input-group'].classList.contains('is-hidden'), true);
    assert.equal(nodes['archive-input-group'].classList.contains('is-hidden'), false);

    applyInputMode(doc, 'url');
    assert.equal(nodes.url.disabled, false);
    assert.equal(nodes.url.required, true);
    assert.equal(nodes.archive_file.disabled, true);
    assert.equal(nodes.archive_file.required, false);
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
