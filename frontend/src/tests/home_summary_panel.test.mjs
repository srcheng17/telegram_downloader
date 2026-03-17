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
