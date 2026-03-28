import test from 'node:test';
import assert from 'node:assert/strict';

import { buildLogsUrl, readLogsFiltersFromForm } from '../logs/state.js';

test('readLogsFiltersFromForm normalizes status and query', () => {
  const doc = {
    getElementById(id) {
      return {
        'status-filter': { value: 'failed' },
        'query-filter': { value: '  demo  ' },
      }[id] || null;
    },
  };
  assert.deepEqual(readLogsFiltersFromForm(doc), { status: 'FAILED', q: 'demo' });
});

test('buildLogsUrl includes page, perPage, and filters', () => {
  assert.equal(buildLogsUrl({ status: 'FAILED', q: 'demo' }, 2, 25), '/api/logs?page=2&per_page=25&status=FAILED&q=demo');
});

