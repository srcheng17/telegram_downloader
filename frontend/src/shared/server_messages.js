const EXACT_MESSAGE_TRANSLATIONS = {
    home: {
        'Please provide a Telegraph URL.': '请先输入 Telegraph 链接。',
        'Only telegra.ph or graph.org URLs are supported.': '仅支持 telegra.ph 或 graph.org 链接。',
    },
    logs: {
        'Stored file is unavailable.': '缓存文件不可用。',
        'Task not found.': '任务不存在。',
        'Task is not completed yet.': '任务尚未完成。',
        'Output file not found for this task.': '任务输出文件不存在。',
        'Cancellation requested.': '已提交取消请求。',
        'Cancellation already requested.': '已提交取消请求。',
    },
};

const PREFIX_MESSAGE_TRANSLATIONS = {
    logs: [{ prefix: 'Task already finished with status ', translation: '任务已结束，无法取消。' }],
};

function normalizeMessage(message) {
    return String(message || '').trim();
}

function normalizeDomain(domain) {
    const normalized = String(domain || 'common').trim().toLowerCase();
    return normalized || 'common';
}

function hasOwnTranslation(catalog, message) {
    return Boolean(catalog && Object.prototype.hasOwnProperty.call(catalog, message));
}

export function localizeServerMessage(message, domain = 'common') {
    const normalizedMessage = normalizeMessage(message);
    if (!normalizedMessage) {
        return '';
    }

    const domainKey = normalizeDomain(domain);
    const domainCatalog = EXACT_MESSAGE_TRANSLATIONS[domainKey];
    if (hasOwnTranslation(domainCatalog, normalizedMessage)) {
        return domainCatalog[normalizedMessage];
    }

    const commonCatalog = EXACT_MESSAGE_TRANSLATIONS.common;
    if (hasOwnTranslation(commonCatalog, normalizedMessage)) {
        return commonCatalog[normalizedMessage];
    }

    const domainPrefixRules = PREFIX_MESSAGE_TRANSLATIONS[domainKey] || [];
    for (const rule of domainPrefixRules) {
        if (normalizedMessage.startsWith(rule.prefix)) {
            return rule.translation;
        }
    }

    const commonPrefixRules = PREFIX_MESSAGE_TRANSLATIONS.common || [];
    for (const rule of commonPrefixRules) {
        if (normalizedMessage.startsWith(rule.prefix)) {
            return rule.translation;
        }
    }

    return normalizedMessage;
}
