function toNonNegativeInteger(value) {
    const parsed = Number(value);
    if (!Number.isFinite(parsed) || parsed < 0) {
        return 0;
    }
    return Math.round(parsed);
}

export function parseStartupRecovery(summary) {
    if (!summary || typeof summary !== 'object') {
        return null;
    }

    const raw = summary.startup_recovery;
    if (!raw || typeof raw !== 'object') {
        return null;
    }

    const recoveredFailed = toNonNegativeInteger(raw.recovered_failed ?? raw.failed_tasks);
    const recoveredCanceled = toNonNegativeInteger(raw.recovered_canceled ?? raw.canceled_tasks);
    const recoveredTotal = Math.max(
        toNonNegativeInteger(raw.recovered_total ?? raw.total),
        recoveredFailed + recoveredCanceled,
    );
    const happened = raw.happened === true || raw.happened === 'true' || recoveredTotal > 0;
    if (!happened) {
        return null;
    }

    const signature = String(
        raw.event_id ||
            raw.summary_id ||
            raw.happened_at ||
            raw.occurred_at ||
            `${recoveredTotal}-${recoveredFailed}-${recoveredCanceled}`,
    );

    return {
        recoveredFailed,
        recoveredCanceled,
        recoveredTotal,
        signature,
    };
}

export function getStartupRecoveryDismissKey(prefix, signature) {
    return `${prefix}${signature}`;
}

export function buildStartupRecoveryMessage(recovery) {
    if (recovery.recoveredTotal > 0) {
        return `启动恢复已处理 ${recovery.recoveredTotal} 个任务（失败 ${recovery.recoveredFailed}，取消 ${recovery.recoveredCanceled}）。`;
    }
    return '启动恢复已完成，任务状态已更新。';
}
