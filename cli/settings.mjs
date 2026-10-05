import { constants, writeSync } from 'node:fs';
import { open } from 'node:fs/promises';
import { authenticatedContext } from './session_context.mjs';
import { readInput } from './tasks.mjs';
import { readSecret, requireInteractive } from './tty.mjs';
import { CliError, EXIT } from './errors.mjs';
import { decodeMetadataSchema } from '../frontend/src/shared/metadata/schema.js';
import { extractRules, validateRuleSet } from '../frontend/src/ocr/rules.js';

const fields = new Set(['expected_definitions_version', 'definitions']);
const ruleFields = new Set(['expected_version', 'definitions_version', 'rules']);
const sourceFields = new Set(['expected_version', 'enabled', 'priority', 'filters', 'field_preferences']);
const aiFields = new Set(['expected_version', 'enabled', 'base_url', 'model_id']);
const downloadFields = new Set(['expected_version', 'timeout', 'retries', 'image_concurrency', 'download_action_mode']);
const operations = {
    download: ['get', 'set'], sources: ['list', 'review', 'set', 'test'], ai: ['get', 'review', 'set', 'models', 'test'],
    fields: ['get', 'review', 'set'], rules: ['get', 'review', 'set', 'preview'],
};
const businessFlags = ['id', 'url', 'file', 'output', 'status', 'query', 'page', 'perPage', 'timeout', 'interval', 'force', 'inputFile', 'inputJson', 'provider', 'credential', 'configVersion', 'model', 'attemptId'];

function checkArgs(section, action, options) {
    if (!operations[section]?.includes(action)) throw new CliError('invalid_command', '未知的设置命令。');
    const allowed = [];
    if (action === 'set' || action === 'preview') allowed.push('inputFile', 'inputJson');
    if (section === 'sources' && action !== 'list') allowed.push('provider');
    if ((section === 'sources' || section === 'ai') && action === 'set') allowed.push('credential');
    if (action === 'test' || action === 'models') allowed.push('configVersion');
    if (section === 'ai' && action === 'test') allowed.push('model');
    for (const key of businessFlags) {
        if (options[key] !== undefined && options[key] !== false && !allowed.includes(key)) throw new CliError('invalid_input', '当前设置命令不支持指定的参数。');
    }
    if (section === 'sources' && action !== 'list' && (!options.provider || !/^[a-z][a-z0-9_-]{0,63}$/.test(options.provider))) {
        throw new CliError('invalid_input', '请指定有效的来源 ID。');
    }
}

function exactFields(input, allowed) {
    if (!input || Object.keys(input).some((key) => !allowed.has(key)) || [...allowed].some((key) => !Object.hasOwn(input, key))) {
        throw new CliError('invalid_input', '设置 JSON 字段不完整或包含未知字段。');
    }
}

function version(value) {
    if (typeof value !== 'string' || !/^(?:0|[1-9][0-9]*)$/.test(value)) throw new CliError('invalid_input', '请提供有效的配置版本。');
    const number = Number(value);
    if (!Number.isSafeInteger(number) || number < 0) throw new CliError('invalid_input', '请提供有效的配置版本。');
    return number;
}

function credentialAction(options) {
    const action = options.credential || 'keep';
    if (!['keep', 'clear', 'replace'].includes(action)) throw new CliError('invalid_input', '凭据操作只能是 keep、clear 或 replace。');
    return action;
}

function safeDownload(data) {
    if (!data || !Number.isSafeInteger(data.config_version)) throw new CliError('invalid_response', '下载设置响应无效。', EXIT.transport);
    return {
        timeout: data.timeout, retries: data.retries, image_concurrency: data.image_concurrency,
        download_action_mode: data.download_action_mode, config_version: data.config_version,
    };
}

function safeSource(data) {
    if (!data || typeof data.provider_id !== 'string' || !Number.isSafeInteger(data.config_version)) throw new CliError('invalid_response', '来源设置响应无效。', EXIT.transport);
    return {
        provider_id: data.provider_id, enabled: data.enabled === true, priority: data.priority,
        auth_mode: data.auth_mode, credential_configured: data.credential_configured === true,
        config_version: data.config_version,
        filters: data.filters && typeof data.filters === 'object' ? data.filters : {},
        field_preferences: data.field_preferences && typeof data.field_preferences === 'object' ? data.field_preferences : {},
        ...(data.descriptor ? { capabilities: Array.isArray(data.descriptor.capabilities) ? data.descriptor.capabilities : [],
            auth_modes: Array.isArray(data.descriptor.auth_modes) ? data.descriptor.auth_modes : [],
            available_filters: data.descriptor.filters && typeof data.descriptor.filters === 'object' ? data.descriptor.filters : {},
            available_field_preferences: data.descriptor.field_preferences && typeof data.descriptor.field_preferences === 'object' ? data.descriptor.field_preferences : {} } : {}),
    };
}

function safeAI(data) {
    if (!data || !Number.isSafeInteger(data.config_version)) throw new CliError('invalid_response', 'AI 设置响应无效。', EXIT.transport);
    return {
        enabled: data.enabled === true,
        target_configured: typeof data.base_url === 'string' && data.base_url.length > 0,
        model_id: typeof data.model_id === 'string' ? data.model_id : '',
        credential_configured: data.credential_configured === true,
        config_version: data.config_version,
    };
}

function safeFields(data) {
    if (!data || typeof data.definitions_version !== 'string' || !Array.isArray(data.definitions)) throw new CliError('invalid_response', '字段设置响应无效。', EXIT.transport);
    return {
        definitions_version: data.definitions_version,
        limits: data.limits && typeof data.limits === 'object' ? Object.fromEntries(Object.entries(data.limits).filter(([, value]) => Number.isSafeInteger(value))) : {},
        definitions: data.definitions.map((item) => ({ key: item.key, type: item.type, enabled: item.enabled, editable: item.editable, extractable: item.extractable, export_status: item.export_status })),
    };
}

function safeRules(data) {
    if (!data || !Number.isSafeInteger(data.rules_version) || !Array.isArray(data.rules)) throw new CliError('invalid_response', '识别规则响应无效。', EXIT.transport);
    return {
        rules_version: data.rules_version, definitions_version: data.definitions_version,
        rules: data.rules.map((item) => ({ id: item.id, target_key: item.target_key, mode: item.mode, label_count: Array.isArray(item.labels) ? item.labels.length : 0 })),
        warnings: Array.isArray(data.warnings) ? data.warnings.map((item) => ({ key: item.key, code: item.code })) : [],
    };
}

async function requiredInput(options, stdin) {
    const input = await readInput(options, stdin);
    if (!input) throw new CliError('invalid_input', '请用 --input-json - 或 --input-file 提供设置 JSON。');
    return input;
}

async function reviewInTerminal(value) {
    let handle;
    try {
        handle = await open('/dev/tty', constants.O_WRONLY | constants.O_NOCTTY);
        writeSync(handle.fd, `${JSON.stringify(value, null, 2)}\n`);
    } catch {
        throw new CliError('interaction_required', '无法打开独立用户终端。', EXIT.interaction);
    } finally { await handle?.close(); }
}

export async function runSettings(section, action, options, {
    stdin = process.stdin, secretReader = readSecret, reviewer = reviewInTerminal,
    interactive = options.interactive ?? Boolean(process.stdin.isTTY && process.stderr.isTTY),
} = {}) {
    checkArgs(section, action, options);
    const credential = (section === 'sources' || section === 'ai') && action === 'set' ? credentialAction(options) : null;
    if (credential === 'replace' || action === 'review') requireInteractive({ json: options.json, interactive });
    const context = await authenticatedContext(options);
    const { client, session } = context;
    const get = async (path) => (await client.request('GET', path, { cookie: session.cookie })).data;
    const write = async (method, path, body, timeoutMs = 15_000) => (await client.request(method, path, { cookie: session.cookie, csrf: session.csrf, timeoutMs, ...body })).data;

    if (section === 'download') {
        if (action === 'get') return safeDownload(await get('/api/settings/download'));
        const input = await requiredInput(options, stdin);
        exactFields(input.data, downloadFields);
        return safeDownload(await write('PUT', '/api/settings/download', { rawBody: input.raw }));
    }
    if (section === 'sources') {
        const path = `/api/settings/sources/${encodeURIComponent(options.provider)}`;
        if (action === 'list' || action === 'review') {
            const data = await get('/api/settings/sources');
            if (!Array.isArray(data?.sources)) throw new CliError('invalid_response', '来源列表响应无效。', EXIT.transport);
            if (action === 'review') {
                const selected = data.sources.find((item) => item?.provider_id === options.provider);
                if (!selected) throw new CliError('not_found', '来源不存在。');
                const summary = safeSource(selected);
                await reviewer({ provider_id: summary.provider_id, enabled: summary.enabled, priority: summary.priority,
                    auth_mode: summary.auth_mode, credential_configured: summary.credential_configured,
                    config_version: summary.config_version, filters: summary.filters, field_preferences: summary.field_preferences });
                return { ...summary, reviewed: true };
            }
            return { sources: data.sources.map(safeSource) };
        }
        if (action === 'test') {
            const data = await write('POST', `${path}/test`, { body: { config_version: version(options.configVersion) } }, 30_000);
            return { provider_id: data.provider_id, config_version: data.config_version, status: data.status, scope: data.scope, checked_at: data.checked_at };
        }
        const input = await requiredInput(options, stdin);
        exactFields(input.data, sourceFields);
        const value = credential === 'replace' ? await secretReader('来源 Bearer 凭据') : undefined;
        if (credential === 'replace' && !value) throw new CliError('invalid_input', '凭据不能为空。');
        return safeSource(await write('PUT', path, { body: { ...input.data, credential: { action: credential, ...(value ? { value } : {}) } } }));
    }
    if (section === 'ai') {
        if (action === 'get' || action === 'review') {
            const data = await get('/api/settings/ai');
            const summary = safeAI(data);
            if (action === 'review') {
                await reviewer({ enabled: summary.enabled, base_url: data.base_url, model_id: summary.model_id,
                    credential_configured: summary.credential_configured, config_version: summary.config_version });
                return { ...summary, reviewed: true };
            }
            return summary;
        }
        if (action === 'models') {
            const data = await write('POST', '/api/settings/ai/models', { body: { config_version: version(options.configVersion) } }, 20_000);
            return { config_version: data.config_version, ignored: data.ignored,
                models: Array.isArray(data.models) ? data.models.map((item) => ({ id: item.id, capability: item.capability, selectable: item.selectable === true })) : [] };
        }
        if (action === 'test') {
            if (!options.model) throw new CliError('invalid_input', '请指定已保存的模型 ID。');
            const data = await write('POST', '/api/settings/ai/test', { body: { config_version: version(options.configVersion), model_id: options.model } }, 130_000);
            return { config_version: data.config_version, model_id: data.model_id, status: data.status,
                checked_at: data.checked_at, field_key_count: Array.isArray(data.field_keys) ? data.field_keys.length : 0,
                fixture_version: data.fixture_version };
        }
        const input = await requiredInput(options, stdin);
        exactFields(input.data, aiFields);
        const value = credential === 'replace' ? await secretReader('AI API 凭据') : undefined;
        if (credential === 'replace' && !value) throw new CliError('invalid_input', '凭据不能为空。');
        return safeAI(await write('PUT', '/api/settings/ai', { body: { ...input.data, credential: { action: credential, ...(value ? { value } : {}) } } }));
    }
    if (section === 'fields') {
        if (action === 'get' || action === 'review') {
            const data = await get('/api/settings/metadata-fields');
            const summary = safeFields(data);
            if (action === 'review') {
                await reviewer({ definitions_version: data.definitions_version, limits: data.limits, definitions: data.definitions });
                return { ...summary, reviewed: true };
            }
            return summary;
        }
        const input = await requiredInput(options, stdin);
        exactFields(input.data, fields);
        return safeFields(await write('PUT', '/api/settings/metadata-fields', { rawBody: input.raw }));
    }
    if (section === 'rules') {
        if (action === 'get' || action === 'review') {
            const data = await get('/api/settings/extraction-rules');
            const summary = safeRules(data);
            if (action === 'review') {
                await reviewer({ rules_version: data.rules_version, definitions_version: data.definitions_version,
                    rules: data.rules, warnings: data.warnings });
                return { ...summary, reviewed: true };
            }
            return summary;
        }
        const input = await requiredInput(options, stdin);
        if (action === 'set') {
            exactFields(input.data, ruleFields);
            return safeRules(await write('PUT', '/api/settings/extraction-rules', { rawBody: input.raw }));
        }
        if (typeof input.data.text !== 'string' || Object.keys(input.data).some((key) => !['text', 'rule_set'].includes(key))) {
            throw new CliError('invalid_input', '规则预览需要 text 和可选的 rule_set。');
        }
        const [schemaRaw, savedRules] = await Promise.all([get('/api/metadata/schema'), get('/api/settings/extraction-rules')]);
        const schema = decodeMetadataSchema(schemaRaw);
        const ruleSet = input.data.rule_set || savedRules;
        try { validateRuleSet(ruleSet, schema); }
        catch { throw new CliError('invalid_input', '规则或字段版本无效。'); }
        let result;
        try { result = extractRules({ text: input.data.text, ruleSet, schema, document: { revision: 0, fields: {} }, inputRevision: 0 }); }
        catch { throw new CliError('invalid_input', '规则预览文字或配置无效。'); }
        return { candidate_count: result.candidates.length,
            candidates: result.candidates.map((item) => ({ candidate_id: item.candidate_id, field_keys: Object.keys(item.fields) })),
            warnings: result.warnings.map((item) => ({ key: item.key, code: item.code })) };
    }
    throw new CliError('invalid_command', '未知的设置命令。');
}
