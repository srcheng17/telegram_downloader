import { fallbackStatusLabel, getStatusMeta, normalizeStatusCode } from '../shared/models/status_catalog.js';

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
    const taskType = String(task.task_type || '').trim().toLowerCase() === 'upload' ? 'upload' : 'url';
    const startTimeSeconds = Number(task.start_time || 0);

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

    return {
        raw: task,
        id: String(task.id || ''),
        url: String(task.url || ''),
        status: statusCode,
        taskType,
        taskTypeLabel: taskType === 'upload' ? '上传' : 'URL',
        progressText,
        errorText: String(task.error || '').trim(),
        startTimeLabel: new Date(startTimeSeconds * 1000).toLocaleString(),
        canRetry: Boolean(task.id && (statusCode === 'FAILED' || statusCode === 'CANCELED') && task.retryable),
    };
}

