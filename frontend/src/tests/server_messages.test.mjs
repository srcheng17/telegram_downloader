import assert from 'node:assert/strict';
import test from 'node:test';

import { localizeServerMessage } from '../shared/server_messages.js';

test('localizeServerMessage maps known home messages to Chinese', () => {
    assert.equal(localizeServerMessage('Please provide a Telegraph URL.', 'home'), '请先输入 Telegraph 链接。');
    assert.equal(
        localizeServerMessage('Only telegra.ph or graph.org URLs are supported.', 'home'),
        '仅支持 telegra.ph 或 graph.org 链接。',
    );
});

test('localizeServerMessage maps known logs messages to Chinese', () => {
    const cases = [
        ['Stored file is unavailable.', '缓存文件不可用。'],
        ['Task not found.', '任务不存在。'],
        ['Task is not completed yet.', '任务尚未完成。'],
        ['Output file not found for this task.', '任务输出文件不存在。'],
        ['Cancellation requested.', '已提交取消请求。'],
        ['Cancellation already requested.', '已提交取消请求。'],
    ];

    cases.forEach(([english, chinese]) => {
        assert.equal(localizeServerMessage(english, 'logs'), chinese);
    });
});

test('localizeServerMessage supports prefix based mapping for logs', () => {
    const message = localizeServerMessage('Task already finished with status SUCCESS', 'logs');

    assert.equal(message, '任务已结束，无法取消。');
});

test('localizeServerMessage falls back to original text for unknown messages', () => {
    const message = 'Unexpected backend failure';

    assert.equal(localizeServerMessage(message, 'home'), message);
    assert.equal(localizeServerMessage(message, 'logs'), message);
});
