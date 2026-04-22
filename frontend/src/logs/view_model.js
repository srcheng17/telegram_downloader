import { fallbackStatusLabel, getStatusMeta, normalizeStatusCode } from '../shared/models/status_catalog.js';

function buildLegacyTaskProgressText(task, statusCode, taskType) {
    let progressText = '暂无';
    if (taskType === 'upload') {
        if (statusCode === 'UPLOADING') {
            progressText = `${Number(task.upload_loaded_bytes || 0)} / ${Number(task.upload_total_bytes || 0)}`;
        } else if (Number(task.upload_total_bytes || 0) > 0) {
            progressText = '处理中';
        }
    } else if (statusCode === 'IN_PROGRESS' && Number(task.total_images || 0) === 0) {
        progressText = '准备中';
    } else if (Number(task.total_images || 0) > 0) {
        progressText = `${Number(task.progress || 0)} / ${Number(task.total_images || 0)}`;
    }
    return progressText;
}

export function buildTaskCoreProgressLabel(task) {
    const progress = task && task.progress && typeof task.progress === 'object' ? task.progress : null;
    if (!progress) return '';
    const label = String(task.phase_label || progress.message || '').trim();
    const current = Number(progress.current || 0);
    const total = Number(progress.total || 0);
    if (total > 0) return `${label} ${current}/${total}`.trim();
    return label;
}

export function buildStatusBadgeModel(status, statusCatalog) {
    const statusCode = normalizeStatusCode(status) || 'UNKNOWN';
    const statusMeta = getStatusMeta(statusCatalog, statusCode);
    return {
        statusCode,
        label: statusMeta ? statusMeta.label : fallbackStatusLabel(statusCode),
        className: `status-badge status-${statusCode.toLowerCase().replace(/[^a-z_]/g, '') || 'unknown'}`,
    };
}

export function mapTaskToLogViewModel(log) {
    const task = log && typeof log === 'object' ? log : {};
    const statusCode = normalizeStatusCode(task.status);
    const statusLabel = String(task.status_label || '').trim() || fallbackStatusLabel(statusCode);
    const taskType = String(task.task_type || '').trim().toLowerCase() === 'upload' ? 'upload' : 'url';
    const startTimeSeconds = Number(task.start_time || 0);
    const taskCoreProgressText = buildTaskCoreProgressLabel(task);
    const progressText = taskCoreProgressText || buildLegacyTaskProgressText(task, statusCode, taskType);
    const availableActions = Array.isArray(task.available_actions)
        ? task.available_actions.slice()
        : Array.isArray(task.availableActions)
          ? task.availableActions.slice()
          : [];

    return {
        raw: task,
        id: String(task.id || ''),
        url: String(task.url || ''),
        status: statusCode,
        statusLabel,
        taskType,
        taskTypeLabel: taskType === 'upload' ? '上传' : 'URL',
        progressText,
        errorText: String(task.error || '').trim(),
        startTimeLabel: new Date(startTimeSeconds * 1000).toLocaleString(),
        canRetry: Boolean(task.id && (statusCode === 'FAILED' || statusCode === 'CANCELED') && task.retryable),
        availableActions,
    };
}
