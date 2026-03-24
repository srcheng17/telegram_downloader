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

export function getTaskTypeLabel(log) {
    return String(log && log.task_type || '').trim().toLowerCase() === 'upload' ? '上传' : 'URL';
}

export function formatProgressValue(log) {
    const task = log && typeof log === 'object' ? log : {};
    if (String(task.task_type || '').trim().toLowerCase() === 'upload') {
        if (normalizeStatusCode(task.status) === 'UPLOADING') {
            return `${Number(task.upload_loaded_bytes || 0)} / ${Number(task.upload_total_bytes || 0)}`;
        }
        if (Number(task.upload_total_bytes || 0) > 0) {
            return '处理中';
        }
    }
    if (normalizeStatusCode(task.status) === 'IN_PROGRESS' && Number(task.total_images || 0) === 0) {
        return '准备中';
    }
    return Number(task.total_images || 0) > 0 ? `${Number(task.progress || 0)} / ${Number(task.total_images || 0)}` : '暂无';
}

export function shouldShowRetryAction(log) {
    const task = log && typeof log === 'object' ? log : {};
    const status = normalizeStatusCode(task.status);
    return Boolean(task.id && (status === 'FAILED' || status === 'CANCELED') && task.retryable);
}
