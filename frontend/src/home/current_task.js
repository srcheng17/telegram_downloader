import { canCancelTaskAction, canCopyToKomgaTaskAction, canRetryTaskAction } from '../logs/task_actions.js';
import { resolvePollDelay } from '../shared/polling.js';

const validKomgaIdentity = value => typeof value === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(value);

// Observe a single submitted task; action eligibility remains owned by Task Core.
export function createCurrentTask({ api, win, doc, onChange }) {
    let taskId = ''; let target = 'download'; let task = null; let timer; let controller;
    let generation = 0; let failures = 0; let deliveryAttempts = 0;
    let delivery = { status: 'waiting' }; let error = '';
    const publish = () => onChange({ task, target, delivery: { ...delivery }, error, busy: Boolean(controller) });
    function clearTimer() { if (timer) win.clearTimeout(timer); timer = null; }
    function schedule() {
        clearTimer();
        if (!taskId || controller || doc.visibilityState === 'hidden') return;
        const pending = delivery.status === 'pending' && deliveryAttempts < 6;
        if (!failures && task && !canCancelTaskAction(task) && !pending) return;
        timer = win.setTimeout(refresh, resolvePollDelay({ active: true, failures, activePollMs: pending ? 4000 : 2000 }));
    }
    async function refresh() {
        if (!taskId || controller || doc.visibilityState === 'hidden') return;
        clearTimer(); const run = generation; const active = new AbortController(); controller = active;
        const current = () => generation === run && controller === active && !active.signal.aborted;
        try {
            const response = await api.getJson(`/api/tasks/${encodeURIComponent(taskId)}`, { signal: active.signal });
            if (!current()) return;
            if (!response.response.ok || response.payload?.task?.id !== taskId) throw new Error('读取失败');
            task = response.payload.task; error = ''; failures = 0;
            if (target === 'komga' && canCopyToKomgaTaskAction(task) && ['waiting', 'pending'].includes(delivery.status) && deliveryAttempts < 6) {
                delivery = { status: 'copying' }; publish(); deliveryAttempts += 1;
                try {
                    const result = await api.postJson(`/api/tasks/${encodeURIComponent(taskId)}/copy-to-komga`, {}, { signal: active.signal });
                    if (!current()) return;
                    if (!result.response.ok || !result.payload?.ok) throw new Error('交付失败');
                    delivery = result.payload.komga_indexed === 'verified' && validKomgaIdentity(result.payload.book_id) && validKomgaIdentity(result.payload.library_id)
                        ? { status: 'indexed', bookId: result.payload.book_id, libraryId: result.payload.library_id }
                        : { status: 'pending' };
                } catch {
                    if (!current()) return;
                    delivery = { status: 'failed' }; error = '归档已保留，Komga 交付暂未完成，可从此处重试。';
                }
            } else if (target === 'komga' && !canCancelTaskAction(task) && !canCopyToKomgaTaskAction(task) && delivery.status === 'waiting') {
                delivery = { status: 'unavailable' };
            }
        } catch {
            if (!current()) return;
            failures += 1; error = '当前任务状态暂时读取失败，正在重试。';
        } finally {
            if (current()) { controller = null; publish(); schedule(); }
        }
    }
    async function retry() {
        if (controller || !taskId) return;
        if (task && canRetryTaskAction(task)) {
            const active = new AbortController(); controller = active; const run = generation;
            try {
                const result = await api.postJson(`/api/tasks/${encodeURIComponent(taskId)}/retry`, {}, { signal: active.signal });
                if (run !== generation || active.signal.aborted) return;
                if (!result.response.ok || !result.payload?.ok) throw new Error('重试失败');
            } catch {
                if (run !== generation || active.signal.aborted) return;
                error = '当前任务暂时无法重试，请稍后再试。';
                return;
            } finally { if (controller === active) { controller = null; publish(); } }
        }
        deliveryAttempts = 0; delivery = { status: 'waiting' }; await refresh();
    }
    function visibility() { clearTimer(); if (doc.visibilityState !== 'hidden') refresh(); }
    function stop() { generation += 1; clearTimer(); controller?.abort(); controller = null; taskId = ''; task = null; doc.removeEventListener('visibilitychange', visibility); }
    return {
        async start(id, destination) {
            stop(); taskId = String(id || ''); target = destination; delivery = { status: 'waiting' }; deliveryAttempts = 0; failures = 0; error = '';
            doc.addEventListener('visibilitychange', visibility); publish(); await refresh();
        },
        refresh, retry, stop,
    };
}
