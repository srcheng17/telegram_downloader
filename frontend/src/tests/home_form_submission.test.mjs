import test from 'node:test';
import assert from 'node:assert/strict';

import { buildDuplicateActions } from '../home/form_submission.js';

test('submitDownload maps duplicate response to action buttons', async () => {
  const result = buildDuplicateActions(
    { download_url: '/api/tasks/t1/download', logs_url: '/logs' },
    { url: 'https://telegra.ph/demo' },
  );

  assert.equal(result.downloadLabel, '立即下载');
  assert.equal(result.downloadUrl, '/api/tasks/t1/download');
  assert.deepEqual(result.basePayload, { url: 'https://telegra.ph/demo' });
});
