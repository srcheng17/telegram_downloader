import test from 'node:test';
import assert from 'node:assert/strict';

import { normalizeHeadResult } from '../logs/download_preflight.js';

test('normalizeHeadResult returns inline error for missing artifact', async () => {
  const result = await normalizeHeadResult(new Response('', { status: 404 }));
  assert.equal(result.ok, false);
  assert.equal(result.status, 404);
});
