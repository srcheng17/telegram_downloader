import test from 'node:test';
import assert from 'node:assert/strict';

import { renderSummary } from '../home/summary_panel.js';

test('renderSummary writes active and success counters', () => {
  const nodes = {
    'summary-total': { textContent: '' },
    'summary-active': { textContent: '' },
    'summary-success': { textContent: '' },
    'summary-failed': { textContent: '' },
  };
  const doc = {
    getElementById(id) {
      return nodes[id] || null;
    },
  };

  renderSummary(doc, { total_tasks: 8, active_tasks: 2, success_tasks: 5, failed_tasks: 1, canceled_tasks: 0 });

  assert.equal(nodes['summary-active'].textContent, '2');
  assert.equal(nodes['summary-success'].textContent, '5');
});


test('createHomeModule mount loads summary without throwing', async () => {
  const { createHomeModule } = await import('../home/index.js');

  const nodes = {
    'summary-panel': {},
    'summary-collapsible-home': { open: false },
    'summary-total': { textContent: '-' },
    'summary-active': { textContent: '-' },
    'summary-success': { textContent: '-' },
    'summary-failed': { textContent: '-' },
    'startup-recovery-banner-home': { classList: { add() {}, remove() {} }, setAttribute() {} },
    'startup-recovery-text-home': { textContent: '' },
    'startup-recovery-dismiss-home': null,
    'download-actions': { classList: { add() {}, remove() {} }, setAttribute() {} },
    'download-action-logs': { classList: { add() {}, remove() {} } },
    'download-action-download': { classList: { add() {}, remove() {} } },
    'download-action-force': { classList: { add() {}, remove() {} } },
    'download-action-use-existing': { classList: { add() {}, remove() {} } },
  };
  const doc = {
    body: {},
    getElementById(id) { return Object.prototype.hasOwnProperty.call(nodes, id) ? nodes[id] : null; },
    querySelector() { return null; },
  };
  const win = {
    __telegraphHomeState: undefined,
    fetch: async () => new Response(JSON.stringify({ total_tasks: 8, active_tasks: 2, success_tasks: 5, failed_tasks: 1, canceled_tasks: 0 }), { status: 200 }),
    matchMedia: () => ({ matches: false }),
    sessionStorage: { getItem() { return null; }, setItem() {} },
    addEventListener() {},
    removeEventListener() {},
    setInterval() { return 1; },
    clearInterval() {},
  };

  const module = createHomeModule(win, doc);
  module.mount();
  await new Promise((resolve) => setTimeout(resolve, 0));

  assert.equal(nodes['summary-total'].textContent, '8');
  assert.equal(nodes['summary-active'].textContent, '2');
});
