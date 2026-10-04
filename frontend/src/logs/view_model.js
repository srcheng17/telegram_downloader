import { fallbackStatusLabel, getStatusMeta, normalizeStatusCode } from '../shared/models/status_catalog.js';

function normalizeText(value) {
    return String(value || '').trim();
}

function firstNonEmpty(...values) {
    for (const value of values) {
        const normalized = normalizeText(value);
        if (normalized) {
            return normalized;
        }
    }
    return '';
}

function basename(value) {
    const raw = normalizeText(value).split('?')[0].split('#')[0];
    if (!raw) {
        return '';
    }
    const normalized = raw.replaceAll('\\', '/').split('/').filter(Boolean).pop() || raw;
    try {
        return decodeURIComponent(normalized);
    } catch (error) {
        return normalized;
    }
}

function removeGeneratedTimestamp(fileName) {
    const name = normalizeText(fileName);
    if (!name) {
        return '';
    }
    const dotIndex = name.lastIndexOf('.');
    const stem = dotIndex > 0 ? name.slice(0, dotIndex) : name;
    const extension = dotIndex > 0 ? name.slice(dotIndex) : '';
    const withoutTimestamp = stem.replace(/[\s_-]+\d{10,13}$/, '');
    return (withoutTimestamp || stem) + extension;
}

function buildMetadataFileName(task) {
    const parts = [
        normalizeText(task.author),
        normalizeText(task.series_name),
        normalizeText(task.comic_name),
    ].filter(Boolean);
    if (!parts.length) {
        return '';
    }
    return `${parts.join('_')}.cbz`;
}

function buildURLLabel(task) {
    const artifactName = firstNonEmpty(
        task.artifact_name,
        basename(task.result_zip_path),
        basename(task.source_archive_name),
    );
    if (artifactName) {
        return removeGeneratedTimestamp(artifactName);
    }
    const metadataName = buildMetadataFileName(task);
    if (metadataName) {
        return metadataName;
    }
    const urlName = basename(task.url);
    if (urlName) {
        return removeGeneratedTimestamp(urlName);
    }
    return normalizeText(task.url);
}

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
    const label = statusMeta ? statusMeta.label : fallbackStatusLabel(statusCode);
    return {
        statusCode,
        label,
        taskStatusLabel: label,
        className: `status-badge status-${statusCode.toLowerCase().replace(/[^a-z_]/g, '') || 'unknown'}`,
    };
}

export function mapTaskToLogViewModel(log) {
    const task = log && typeof log === 'object' ? log : {};
    const statusCode = normalizeStatusCode(task.status);
    const statusLabel = String(task.status_label || '').trim();
    const rawType = String(task.task_type || '').trim().toLowerCase();
    const taskType = ['upload', 'telegram'].includes(rawType) ? rawType : 'url';
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
        url: normalizeText(task.url),
        urlLabel: buildURLLabel(task),
        status: statusCode,
        statusLabel,
        taskType,
        taskTypeLabel: taskType === 'upload' ? '上传' : taskType === 'telegram' ? 'Telegram' : 'URL',
        progressText,
        errorText: String(task.error || '').trim(),
		metadataWarnings: Array.isArray(task.metadata_warnings) ? task.metadata_warnings.map(warning => String(warning?.message || '').trim()).filter(Boolean) : [],
        startTimeLabel: new Date(startTimeSeconds * 1000).toLocaleString(),
        canRetry: Boolean(task.id && (statusCode === 'FAILED' || statusCode === 'CANCELED') && task.retryable),
        availableActions,
    };
}
