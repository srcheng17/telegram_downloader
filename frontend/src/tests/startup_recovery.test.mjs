import test from 'node:test';
import assert from 'node:assert/strict';

import {
    buildStartupRecoveryMessage,
    getStartupRecoveryDismissKey,
    parseStartupRecovery,
} from '../shared/startup_recovery.js';

test('parseStartupRecovery normalizes mixed payload fields', () => {
    const parsed = parseStartupRecovery({
        startup_recovery: {
            recovered_total: 1,
            failed_tasks: 2,
            canceled_tasks: 1,
            happened: true,
        },
    });

    assert.deepEqual(parsed, {
        recoveredFailed: 2,
        recoveredCanceled: 1,
        recoveredTotal: 3,
        signature: '3-2-1',
    });
});

test('buildStartupRecoveryMessage returns completion text when total is zero', () => {
    const message = buildStartupRecoveryMessage({
        recoveredTotal: 0,
        recoveredFailed: 0,
        recoveredCanceled: 0,
    });

    assert.equal(message, '启动恢复已完成，任务状态已更新。');
});

test('buildStartupRecoveryMessage returns stats text when total is positive', () => {
    const message = buildStartupRecoveryMessage({
        recoveredTotal: 5,
        recoveredFailed: 2,
        recoveredCanceled: 3,
    });

    assert.equal(message, '启动恢复已处理 5 个任务（失败 2，取消 3）。');
});

test('getStartupRecoveryDismissKey preserves prefix stability', () => {
    const key = getStartupRecoveryDismissKey('telegraph.startup_recovery.dismissed.', 'event-123');

    assert.equal(key, 'telegraph.startup_recovery.dismissed.event-123');
});
