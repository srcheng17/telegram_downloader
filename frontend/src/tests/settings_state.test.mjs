import test from 'node:test';
import assert from 'node:assert/strict';

import { applySettingsSnapshot, buildSettingsPayload } from '../settings/state.js';

test('buildSettingsPayload reads numeric fields and download action mode', () => {
  const form = {
    querySelector(selector) {
      const values = {
        '#timeout': { value: '30' },
        '#retries': { value: '5' },
        '#image_concurrency': { value: '2' },
        'input[name="download_action_mode"]:checked': { value: 'komga_copy' },
      };
      return values[selector] || null;
    },
  };
  assert.deepEqual(buildSettingsPayload(form), {
    timeout: 30,
    retries: 5,
    image_concurrency: 2,
    download_action_mode: 'komga_copy',
  });
});

test('applySettingsSnapshot writes fields and caches mode', () => {
  const timeoutInput = { value: '' };
  const retriesInput = { value: '' };
  const imageInput = { value: '' };
  const radios = [{ value: 'browser', checked: false }, { value: 'komga_copy', checked: false }];
  const form = {
    querySelector(selector) {
      const values = {
        '#timeout': timeoutInput,
        '#retries': retriesInput,
        '#image_concurrency': imageInput,
      };
      return values[selector] || null;
    },
    querySelectorAll(selector) {
      return selector === 'input[name="download_action_mode"]' ? radios : [];
    },
  };
  const win = {};
  applySettingsSnapshot(win, form, {
    timeout: 45,
    retries: 6,
    image_concurrency: 3,
    download_action_mode: 'komga_copy',
  });
  assert.equal(timeoutInput.value, '45');
  assert.equal(retriesInput.value, '6');
  assert.equal(imageInput.value, '3');
  assert.equal(radios[1].checked, true);
  assert.equal(win.__telegraphSettingsState.downloadActionMode, 'komga_copy');
});

