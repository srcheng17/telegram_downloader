import { copy } from './schema.js';

const signature = field => JSON.stringify([field.state, field.value]);

// One review document, with explicit decisions only for conflicting/protected
// fields. The existing draft remains the authority for every actual mutation.
export function createCandidateReview({ draft, onChange = () => {} }) {
    const pending = new Map();
    const prepared = new Map();
    const adopted = new Map();
    let generation = 0;
    let controller;
    let disposed = false;
    let resolving = false;
    const snapshot = () => ({
        pending: [...pending.values()].map(group => ({ key: group.key, current: copy(draft.getSnapshot().fields[group.key]), options: group.options.map(option => ({ field: copy(option.field), sources: [...option.sources] })) })),
        prepared: [...prepared.values()].map(copy),
    });
    const notify = () => { if (!disposed) onChange(snapshot()); };
    const unsubscribe = draft.subscribe(document => {
        for (const [key, item] of adopted) {
            if (document.fields[key]?.revision !== item.revision) adopted.delete(key);
        }
        // A deliberate edit/clear in the form itself settles that field. Other
        // automatic fills and unrelated edits cannot dismiss its decision.
        for (const [key, group] of pending) {
            const current = document.fields[key];
            if (current?.manual_locked && current.revision !== group.fieldRevision) pending.delete(key);
        }
        notify();
    });
    function clear() {
        generation++; controller?.abort(); controller = null;
        pending.clear(); prepared.clear(); notify();
    }
    function recordAdopted(entry, keys) {
        if (disposed || typeof entry?.beforeApply !== 'function') return;
        const document = draft.getSnapshot();
        for (const key of keys) {
            if (document.fields[key]) adopted.set(key, { entry, revision: document.fields[key].revision });
        }
    }
    async function prepare(entries, { signal } = {}) {
        const run = ++generation;
        controller?.abort(); const request = new AbortController(); controller = request;
        const abort = () => request.abort();
        signal?.addEventListener('abort', abort, { once: true });
        if (signal?.aborted) request.abort();
        const active = () => !disposed && run === generation && !request.signal.aborted;
        const result = { appliedKeys: [], unresolvedCount: pending.size, warnings: [] };
        try {
            const valid = await Promise.all((entries || []).map(async entry => {
                try {
                    if (!active()) return null;
                    if (entry.beforeApply && await entry.beforeApply({ signal: request.signal }) === false) throw new Error('rejected');
                    if (!active()) return null;
                    return { entry, rows: draft.previewCandidate(entry.candidate) };
                } catch {
                    if (active() && !result.warnings.length) result.warnings.push('部分建议暂时无法校验；当前文字和手工修改已保留。');
                    return null;
                }
            }));
            if (!active()) return result;
            const groups = new Map();
            for (const item of valid.filter(Boolean)) for (const row of item.rows) {
                if (row.conflict) {
                    if (!result.warnings.length) result.warnings.push('部分字段已变化，旧建议未采用；请核对当前信息。');
                    continue;
                }
                let group = groups.get(row.key);
                if (!group) {
                    const existing = pending.get(row.key);
                    group = { key: row.key, fieldRevision: draft.getSnapshot().fields[row.key]?.revision || 0, options: existing ? [...existing.options] : [] };
                    groups.set(row.key, group);
                }
                const same = group.options.find(option => signature(option.field) === signature(row.proposed));
                if (same) {
                    if (!same.sources.includes(item.entry.candidate.origin)) same.sources.push(item.entry.candidate.origin);
                } else group.options.push({ field: row.proposed, entry: item.entry, sources: [item.entry.candidate.origin] });
            }
            for (const [key, group] of groups) {
                const current = draft.getSnapshot().fields[key];
                const only = group.options.length === 1 ? group.options[0] : null;
                if (only && current && signature(current) === signature(only.field)) {
                    if (!current.manual_locked || adopted.has(key)) adopted.set(key, { entry: only.entry, revision: current.revision });
                    pending.delete(key);
                    prepared.set(key, { key, sources: [...new Set([...(current.provenance || []).map(source => source.kind), ...only.sources])] });
                    continue;
                }
                const warnings = only && [...(only.field.warnings || []), ...(only.entry.candidate.warnings || [])];
                if (only && !current && only.field.state === 'value' && !warnings.length) {
                    try {
                        draft.applyCandidate(only.entry.candidate, [key]);
                        adopted.set(key, { entry: only.entry, revision: draft.getSnapshot().fields[key].revision });
                        result.appliedKeys.push(key);
                        prepared.set(key, { key, sources: only.sources });
                        pending.delete(key);
                        continue;
                    } catch {
                        result.warnings.push('建议与当前输入不一致，请核对后重试。');
                    }
                }
                pending.set(key, group);
            }
            result.unresolvedCount = pending.size; notify();
            return result;
        } finally {
            signal?.removeEventListener('abort', abort);
            if (controller === request) controller = null;
        }
    }
    async function resolve(key, selection) {
        const group = pending.get(key);
        if (!group || disposed || resolving) return;
        if (selection === 'keep') { pending.delete(key); notify(); return; }
        const option = group.options[selection];
        if (!option) throw new Error('请选择有效的字段建议。');
        const run = generation; const request = new AbortController(); controller = request;
        resolving = true;
        try {
            try {
                if (option.entry.beforeApply && await option.entry.beforeApply({ signal: request.signal }) === false) throw new Error('rejected');
            } catch { throw new Error('建议校验未通过，请重新准备；当前内容已保留。'); }
            if (disposed || run !== generation || request.signal.aborted || pending.get(key) !== group) return;
            draft.applyCandidate(option.entry.candidate, [key], { confirmLocked: true });
            adopted.set(key, { entry: option.entry, revision: draft.getSnapshot().fields[key].revision });
            pending.delete(key); prepared.set(key, { key, sources: option.sources }); notify();
        } finally { resolving = false; if (controller === request) controller = null; }
    }
    async function preflight({ signal } = {}) {
        const run = generation;
        const entries = new Set([...adopted.values()].map(item => item.entry));
        for (const entry of entries) {
            if (disposed || signal?.aborted || run !== generation) throw new Error('核对内容已变化，请重新确认。');
            try {
                if (entry.beforeApply && await entry.beforeApply({ signal }) === false) throw new Error('rejected');
            } catch { throw new Error('建议所依据的设置已变化或暂不可验证，请重新准备或手工核对后再开始。'); }
        }
        if (disposed || signal?.aborted || run !== generation) throw new Error('核对内容已变化，请重新确认。');
    }
    return { prepare, resolve, clear, snapshot, preflight, recordAdopted, hasUnresolved: () => pending.size > 0,
        dispose() { clear(); adopted.clear(); disposed = true; unsubscribe(); },
    };
}
