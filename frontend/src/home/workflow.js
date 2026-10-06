// Navigation state only. Metadata and source bytes stay in their owning modules.
export function createGuidedWorkflow({ prepare, getPreparationKey, onStep = () => {}, createKey = () => crypto.randomUUID() }) {
    let step = 'materials';
    let generation = 0;
    let controller;
    let preparedKey;
    let prepared = false;
    let submission = null;
    let busy = false;
    let taskId = '';
    function go(next) { step = next; onStep(next); }
    function cancel() { generation += 1; controller?.abort(); controller = null; }
    return {
        get step() { return step; },
        get submission() { return submission; },
        get busy() { return busy; },
        get taskId() { return taskId; },
        async next({ retry = false } = {}) {
            if (busy) return;
            if (taskId) { go('result'); return; }
            if (!retry && prepared && preparedKey === getPreparationKey()) { go('review'); return; }
            cancel(); const run = generation; const active = new AbortController(); controller = active;
            go('preparing');
            try {
                await prepare({ signal: active.signal, retry });
                if (run !== generation || active.signal.aborted) return;
                preparedKey = getPreparationKey(); prepared = true;
                go('review');
            } catch (error) {
                if (run !== generation || active.signal.aborted) return;
                go('materials'); throw error;
            } finally { if (controller === active) controller = null; }
        },
        back() {
            if (busy) return;
            cancel();
            go(taskId ? 'review' : 'materials');
        },
        confirm(input) {
            if (taskId) throw new Error('此作品已提交，请查看当前任务。');
            if (busy) throw new Error('正在提交，请稍候。');
            if (step !== 'review') throw new Error('请先核对作品信息。');
            cancel();
            const { file, ...values } = input;
            const fingerprint = JSON.stringify(values);
            if (!submission || submission.fingerprint !== fingerprint || submission.snapshot.file !== file) {
                submission = { key: createKey(), fingerprint, snapshot: { ...JSON.parse(fingerprint), file } };
            }
            busy = true;
            return submission;
        },
        matches(input) {
            const { file, ...values } = input;
            return submission?.fingerprint === JSON.stringify(values) && submission.snapshot.file === file;
        },
        failed() { busy = false; },
        complete(id) { busy = false; taskId = String(id || ''); go('result'); },
        dispose() { cancel(); submission = null; preparedKey = undefined; prepared = false; busy = false; taskId = ''; },
    };
}
