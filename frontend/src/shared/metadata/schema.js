const types = new Set(['string', 'string[]', 'integer', 'boolean', 'date', 'identifiers']);
const forbiddenKeys = new Set(['__proto__', 'constructor', 'prototype']);
const encoder = new TextEncoder();

export function isRecord(value) {
    return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

export function copy(value) {
    return structuredClone(value);
}

export function byteLength(value) {
    return encoder.encode(value).length;
}

export function decodeMetadataSchema(payload) {
    if (!isRecord(payload) || payload.schema_version !== 1 || typeof payload.definitions_version !== 'string' || !payload.definitions_version) {
        throw new Error('不支持此元数据定义版本，请刷新页面后重试。');
    }
    if (!isRecord(payload.definitions)) throw new Error('元数据字段定义不可用。');
    for (const [key, definition] of Object.entries(payload.definitions)) {
        if (forbiddenKeys.has(key) || !isRecord(definition) || definition.key !== key || !types.has(definition.type) || typeof definition.label !== 'string') {
            throw new Error('元数据字段定义无效。');
        }
        if (definition.enum !== undefined && (!Array.isArray(definition.enum) || !definition.enum.every(item => typeof item === 'string'))) {
            throw new Error('元数据字段选项无效。');
        }
    }
    return copy(payload);
}

function isPrivateIPv4(host) {
    const parts = host.split('.').map(Number);
    if (parts.length !== 4 || parts.some(part => !Number.isInteger(part) || part < 0 || part > 255)) return false;
    return parts.every(part => part === 0) || parts[0] === 10 || parts[0] === 127
        || (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31)
        || (parts[0] === 192 && parts[1] === 168) || (parts[0] === 169 && parts[1] === 254)
        || (parts[0] >= 224 && parts[0] <= 239);
}

function validPublicURL(value) {
    if (/[\r\n\t \\]/u.test(value) || !/^https?:\/\//u.test(value)) return false;
    try {
        const url = new URL(value);
        // An empty userinfo marker still represents a credential-bearing URL.
        if (value.split('/')[2].includes('@') || url.username || url.password) return false;
        const host = url.hostname.toLowerCase().replace(/^\[|\]$/g, '').replace(/\.$/, '');
        if (!host || host === 'localhost' || /\.(?:localhost|local|internal)$/u.test(host)) return false;
        if (host.includes(':')) {
            if (host === '::' || host === '::1' || /^(?:f[cd][0-9a-f]{2}:|fe[89ab][0-9a-f]:|ff)/u.test(host)) return false;
            if (host.startsWith('::ffff:')) {
                const words = host.slice(7).split(':');
                const number = parseInt(words[0], 16) * 65536 + parseInt(words[1], 16);
                if (isPrivateIPv4([number >>> 24, (number >>> 16) & 255, (number >>> 8) & 255, number & 255].join('.'))) return false;
            }
        } else if (!host.includes('.') || isPrivateIPv4(host)) return false;
        if (/%(?![0-9a-f]{2})/iu.test(value) || url.search.includes(';')) return false;
        for (const key of url.searchParams.keys()) {
            if (/token|secret|password|api_key|apikey/u.test(key.toLowerCase()) || ['key', 'authorization', 'signature'].includes(key.toLowerCase())) return false;
        }
        return true;
    } catch { return false; }
}

export function validateFieldValue(definition, value) {
    if (!definition || !types.has(definition.type)) throw new Error('此字段未注册或类型不受支持。');
    const text = (item, maximum) => {
        if (typeof item !== 'string') throw new Error('请填写文字。');
        if (!item.trim()) throw new Error('内容不能为空；如需删除已有值，请使用“清空”。');
        if ([...item].some(character => /\p{Cc}/u.test(character) && !'\n\r\t'.includes(character))) throw new Error('文字包含无效控制字符。');
        if (maximum && byteLength(item) > maximum) throw new Error(`文字长度超过 ${maximum} 字节，请缩短后重试。`);
        // Lone surrogates cannot make the same round trip through Go's UTF-8 decoder.
        if (typeof item.isWellFormed === 'function' && !item.isWellFormed()) throw new Error('文字包含无效字符。');
    };
    switch (definition.type) {
    case 'string':
        text(value, definition.max_bytes);
        if (definition.enum && !definition.enum.includes(value)) throw new Error('请选择字段支持的选项。');
        if (definition.key === 'language') {
            if (!/^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$/u.test(value)) throw new Error('请填写有效语言代码，例如 zh、zh-Hant 或 ja。');
            try { new Intl.Locale(value); } catch { throw new Error('请填写有效语言代码，例如 zh、zh-Hant 或 ja。'); }
        }
        if (definition.key === 'web' && !validPublicURL(value)) throw new Error('请填写公开 HTTP(S) 链接，不得包含凭据、私有地址或秘密参数。');
        break;
    case 'string[]':
        if (!Array.isArray(value)) throw new Error('请填写文字列表。');
        if (value.length > (definition.max_items || 64)) throw new Error('列表项目过多。');
        value.forEach(item => text(item, definition.item_max_bytes || 1024));
        break;
    case 'integer':
        if (!Number.isSafeInteger(value)) throw new Error('请填写有效整数。');
        if ((definition.minimum !== undefined && value < definition.minimum) || (definition.maximum !== undefined && value > definition.maximum)) throw new Error('数值超出允许范围。');
        break;
    case 'boolean':
        if (typeof value !== 'boolean') throw new Error('请选择是或否，不能用文字代替布尔值。');
        break;
    case 'date': {
        if (!isRecord(value) || Object.keys(value).some(key => !['year', 'month', 'day'].includes(key)) || !Number.isInteger(value.year) || value.year < 1 || value.year > 9999) throw new Error('请输入有效年份。');
        if (value.month !== undefined && (!Number.isInteger(value.month) || value.month < 1 || value.month > 12)) throw new Error('请输入有效月份。');
        if (value.day !== undefined) {
            const leap = value.year % 4 === 0 && (value.year % 100 !== 0 || value.year % 400 === 0);
            const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
            if (value.month === undefined || !Number.isInteger(value.day) || value.day < 1 || value.day > days[value.month - 1]) throw new Error('请输入有效日期；填写日时需要月份。');
        }
        break;
    }
    case 'identifiers':
        if (!Array.isArray(value) || value.length > (definition.max_items || 64)) throw new Error('标识符列表无效或过长。');
        value.forEach(item => {
            if (!isRecord(item) || Object.keys(item).some(key => !['scheme', 'value'].includes(key)) || typeof item.scheme !== 'string' || !/^[a-z][a-z0-9_.-]{0,63}$/u.test(item.scheme) || typeof item.value !== 'string' || !item.value.trim()) throw new Error('每个标识符都需要类型和编号。');
            text(item.scheme, 64);
            text(item.value, definition.item_max_bytes || 1024);
        });
        break;
    }
    return copy(value);
}

export function parseFieldInput(definition, input) {
    const raw = String(input);
    let value = raw;
    if (definition.type === 'string[]') value = raw === '' ? [] : raw.split('\n').map(item => item.trim()).filter(Boolean);
    if (definition.type === 'boolean') {
        if (!['true', 'false'].includes(raw)) throw new Error('请选择是或否；未知可使用“清空”。');
        value = raw === 'true';
    }
    if (definition.type === 'integer') {
        if (!/^-?\d+$/.test(raw.trim())) throw new Error('请填写有效整数。');
        value = Number(raw);
    }
    if (definition.type === 'date') {
        const match = /^(\d{1,4})(?:-(\d{1,2})(?:-(\d{1,2}))?)?$/.exec(raw.trim());
        if (!match) throw new Error('请填写年、年-月或年-月-日。');
        value = { year: Number(match[1]), ...(match[2] ? { month: Number(match[2]) } : {}), ...(match[3] ? { day: Number(match[3]) } : {}) };
    }
    if (definition.type === 'identifiers') {
        value = raw === '' ? [] : raw.split('\n').filter(line => line.trim()).map(line => {
            const split = line.indexOf(':');
            if (split < 1) throw new Error('每行使用“类型:编号”，例如 isbn:9780000000000。');
            return { scheme: line.slice(0, split).trim(), value: line.slice(split + 1).trim() };
        });
    }
    return validateFieldValue(definition, value);
}

export function formatFieldValue(definition, value) {
    if (value === undefined || value === null) return '';
    if (definition?.type === 'string[]') return Array.isArray(value) ? value.join('\n') : '';
    if (definition?.type === 'identifiers') return Array.isArray(value) ? value.map(item => `${item.scheme}:${item.value}`).join('\n') : '';
    if (definition?.type === 'date' && isRecord(value)) return [String(value.year), ...(value.month !== undefined ? [String(value.month).padStart(2, '0')] : []), ...(value.day !== undefined ? [String(value.day).padStart(2, '0')] : [])].join('-');
    return String(value);
}
