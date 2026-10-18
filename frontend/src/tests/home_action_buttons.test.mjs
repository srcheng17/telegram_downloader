import test from 'node:test';
import assert from 'node:assert/strict';

import { resetHomeActionButtons, showHomeActionButtons } from '../home/action_buttons.js';

function createButton() {
  return {
    textContent: '',
    disabled: false,
    onclick: null,
    classList: {
      values: new Set(),
      add(value) { this.values.add(value); },
      remove(value) { this.values.delete(value); },
      contains(value) { return this.values.has(value); },
    },
  };
}

test('showHomeActionButtons reveals logs button with url navigation', () => {
  const actions = {
    classList: { values: new Set(['is-hidden']), add(v){this.values.add(v);}, remove(v){this.values.delete(v);} },
    attrs: {},
    setAttribute(name, value) { this.attrs[name] = value; },
  };
  const logs = createButton();
  const download = createButton();
  const force = createButton();
  const existing = createButton();
  const doc = {
    getElementById(id) {
      return {
        'download-actions': actions,
        'download-action-logs': logs,
        'download-action-download': download,
        'download-action-force': force,
        'download-action-use-existing': existing,
      }[id] || null;
    },
  };
  const win = { location: { href: '' } };

  showHomeActionButtons(doc, { logsUrl: '/logs' }, win);
  assert.equal(logs.textContent, '查看日志');
  logs.onclick();
  assert.equal(win.location.href, '/logs');
  assert.equal(actions.classList.values.has('is-hidden'), false);
});

test('resetHomeActionButtons hides all controls', () => {
  const actions = {
    classList: { values: new Set(), add(v){this.values.add(v);}, remove(v){this.values.delete(v);} },
    attrs: {},
    setAttribute(name, value) { this.attrs[name] = value; },
  };
  const logs = createButton();
  const download = createButton();
  const force = createButton();
  const existing = createButton();
  const doc = {
    getElementById(id) {
      return {
        'download-actions': actions,
        'download-action-logs': logs,
        'download-action-download': download,
        'download-action-force': force,
        'download-action-use-existing': existing,
      }[id] || null;
    },
  };

  resetHomeActionButtons(doc, { location: { href: '' } });
  assert.equal(actions.classList.values.has('is-hidden'), true);
  assert.equal(actions.attrs['aria-hidden'], 'true');
  assert.equal(logs.onclick, null);
  assert.equal(download.onclick, null);
});

