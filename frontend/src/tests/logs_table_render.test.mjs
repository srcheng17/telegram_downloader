import test from 'node:test';
import assert from 'node:assert/strict';

import { buildStatusBadgeModel, createStatusCellElement, formatProgressValue, getTaskTypeLabel, shouldShowRetryAction } from '../logs/table_render.js';

test('buildStatusBadgeModel uses status catalog labels when available', () => {
  const model = buildStatusBadgeModel('success', { SUCCESS: { label: '已完成' } });
  assert.equal(model.label, '已完成');
  assert.equal(model.statusCode, 'SUCCESS');
});

test('formatProgressValue renders upload byte progress for upload tasks', () => {
  const value = formatProgressValue({
    task_type: 'upload',
    status: 'UPLOADING',
    upload_loaded_bytes: 12,
    upload_total_bytes: 40,
  });
  assert.equal(value, '12 / 40');
});

test('getTaskTypeLabel maps upload tasks to 上传', () => {
  assert.equal(getTaskTypeLabel({ task_type: 'upload' }), '上传');
  assert.equal(getTaskTypeLabel({ task_type: 'url' }), 'URL');
});

test('shouldShowRetryAction returns true for failed retryable url tasks', () => {
  assert.equal(
    shouldShowRetryAction({ task_type: 'url', status: 'FAILED', retryable: true, id: 'task-url-failed' }),
    true,
  );
});

test('shouldShowRetryAction returns true for canceled retryable tasks', () => {
  assert.equal(
    shouldShowRetryAction({ task_type: 'url', status: 'CANCELED', retryable: true, id: 'task-url-canceled' }),
    true,
  );
});

test('formatProgressValue renders preparing state for running url tasks before totals exist', () => {
  const value = formatProgressValue({
    task_type: 'url',
    status: 'IN_PROGRESS',
    progress: 0,
    total_images: 0,
  });
  assert.equal(value, '准备中');
});

test('buildStatusBadgeModel exposes stable task status label marker', () => {
  const model = buildStatusBadgeModel('running', { RUNNING: { label: '运行中' } });
  assert.equal(model.label, '运行中');
  assert.equal(model.statusCode, 'RUNNING');
  assert.equal(model.taskStatusLabel, '运行中');
});


function createFakeDocument() {
  function createElement(tagName) {
    const attributes = new Map();
    const children = [];
    const element = {
      tagName,
      className: '',
      textContent: '',
      title: '',
      children,
      appendChild(child) {
        children.push(child);
        return child;
      },
      setAttribute(name, value) {
        attributes.set(name, String(value));
      },
      getAttribute(name) {
        return attributes.get(name) || null;
      },
      matches(selector) {
        const match = selector.match(/^\[([^=]+)="([^"]*)"\]$/);
        return Boolean(match && attributes.get(match[1]) === match[2]);
      },
      querySelector(selector) {
        if (this.matches(selector)) {
          return this;
        }
        for (const child of children) {
          const match = typeof child.querySelector === 'function' ? child.querySelector(selector) : null;
          if (match) {
            return match;
          }
        }
        return null;
      },
    };
    return element;
  }

  return { createElement };
}

test('createStatusCellElement renders stable status marker attributes into row DOM', () => {
  const cell = createStatusCellElement(
    createFakeDocument(),
    { status: 'RUNNING', statusLabel: '' },
    { RUNNING: { label: '运行中' } },
  );
  const badge = cell.querySelector('[data-task-status-label="运行中"]');

  assert.ok(badge);
  assert.equal(badge.textContent, '运行中');
  assert.equal(badge.getAttribute('data-task-status-label'), '运行中');
  assert.equal(badge.getAttribute('data-task-status-code'), 'RUNNING');
  assert.equal(cell.querySelector('[data-task-status-code="RUNNING"]'), badge);
});
