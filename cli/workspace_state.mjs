import { randomUUID } from 'node:crypto';
import { performance } from 'node:perf_hooks';
import { isDeepStrictEqual } from 'node:util';
import { createMetadataDraft } from '../frontend/src/shared/metadata/draft.js';
import { candidateFromHistory } from '../frontend/src/shared/metadata/candidates.js';
import { byteLength, decodeMetadataSchema, validateFieldValue } from '../frontend/src/shared/metadata/schema.js';
import { extractRules, validateRuleSet } from '../frontend/src/ocr/rules.js';
import { IMAGE_LIMITS } from '../frontend/src/ocr/images.js';
import { validateLanguages, readImageFile, readClipboardImage, createNodeRecognizer } from './ocr.mjs';
import { CliError, EXIT } from './errors.mjs';

function inputError(message) { return new CliError('invalid_input', message); }
function conflict(message = '工作区内容已变化，请重新读取状态。') { return new CliError('workspace_conflict', message, EXIT.conflict); }
function safeCode(value) { return typeof value === 'string' && /^[a-z][a-z0-9_]{0,63}$/u.test(value) ? value : 'unknown'; }
function safeWarning(item) { return { ...(typeof item.key === 'string' && FIELD_KEYS.test(item.key) ? { key: item.key } : {}), code: safeCode(item.code) }; }
function safeCandidate(item) { return { candidate_id: item.candidate_id, origin: safeCode(item.origin), field_keys: Object.keys(item.fields || {}).filter((key) => FIELD_KEYS.test(key)), warning_codes: (item.warnings || []).map((warning) => safeCode(warning.code)) }; }
function safeImage(item) { return { image_id: item.id, status: item.status, bytes: item.bytes?.length || 0, width: item.info.width, height: item.info.height, text_bytes: byteLength(item.text || ''), raw_bytes: byteLength(item.rawText || ''), text_revision: item.textRevision, edited: item.dirty }; }
const FIELD_KEYS = /^[a-z][a-z0-9_.-]{0,127}$/u;
const REVEAL_TTL_MS = 60_000;
const REVEAL_MAX_BYTES = 256 * 1024;

export function createWorkspaceState({ schema: schemaInput, get, post, submit, recognizer = createNodeRecognizer(), onChange = () => {}, now = () => performance.now() }) {
    const schema = decodeMetadataSchema(schemaInput);
    const draft = createMetadataDraft({ definitions: schema.definitions, definitionsVersion: schema.definitions_version, limits: schema.limits });
    const images = []; const candidates = new Map(); const records = new Map(); const history = new Map();
    let revision = 0; let inputRevision = 0; let configRevision;
    let languages = ['chi_sim', 'eng']; let manualText = '';
    let sendSnapshot = null; let reviewTicket = null; let revealTicket = null; let running = false; let stopped = false; let lastSubmission = null;
    draft.setContext({ inputRevision, configRevision });

    function live() { if (stopped) throw new CliError('workspace_closed', '工作区已关闭。', EXIT.unavailable); }
    function changed({ input = false } = {}) {
        revision++;
        revealTicket = null;
        if (input) { inputRevision++; candidates.clear(); records.clear(); reviewTicket = null; if (sendSnapshot) sendSnapshot.stale = true; }
        draft.setContext({ inputRevision, configRevision }); onChange();
    }
    function findImage(id) {
        const item = images.find((entry) => entry.id === id);
        if (!item) throw inputError('图片 ID 不存在。');
        return item;
    }
    function merged(acceptPartial = false) {
        const excluded = images.filter((item) => item.status !== 'done');
        if (excluded.length && !acceptPartial) throw conflict('仍有图片未识别完成；请等待，或明确 --accept-partial。');
        let text = ''; const segments = []; const warnings = []; const seen = new Set();
        for (const item of images.filter((entry) => entry.status === 'done')) {
            if (!item.text) continue;
            if (text) text += '\n\n';
            const start = text.length; text += item.text;
            segments.push({ image_id: item.id, text_revision: item.textRevision, start, end: text.length });
            for (const line of item.text.split('\n').map((value) => value.trim()).filter(Boolean)) {
                if (seen.has(line) && !warnings.includes('overlapping_text')) warnings.push('overlapping_text');
                seen.add(line);
            }
        }
        if (manualText.trim()) { if (text) text += '\n\n'; text += manualText; }
        if (byteLength(text) > 65536) throw inputError('合并文字不能超过 64 KiB。');
        return { text, segments, excluded: excluded.map((item) => item.id), warnings };
    }
    function status() {
        live(); const document = draft.getSnapshot();
        return { revision, document_revision: document.revision, definitions_version: document.definitions_version,
            input_revision: inputRevision, image_count: images.length, images: images.map(safeImage),
            candidate_count: candidates.size, candidates: [...candidates.values()].map((entry) => safeCandidate(entry.candidate)),
            field_keys: Object.keys(document.fields), send_prepared: Boolean(sendSnapshot), send_stale: Boolean(sendSnapshot?.stale),
            last_submission: lastSubmission };
    }
    function pump() {
        if (running || stopped) return;
        running = true;
        void (async () => {
            try {
                while (!stopped) {
                    const item = images.find((entry) => entry.status === 'queued');
                    if (!item) break;
                    item.status = 'running'; changed();
                    const generation = ++item.generation;
                    try {
                        const text = await recognizer.recognize(item.bytes, item.info, languages);
                        if (stopped || !images.includes(item) || item.generation !== generation) continue;
                        item.rawText = text;
                        if (!item.dirty) { item.text = text; item.textRevision++; }
                        item.status = 'done'; changed({ input: true });
                    } catch {
                        if (stopped || !images.includes(item) || item.generation !== generation) continue;
                        item.status = 'failed'; changed({ input: true });
                    }
                }
            } finally { running = false; if (!stopped && images.some((item) => item.status === 'queued')) pump(); }
        })();
    }
    function candidateEntry(id) {
        const entry = candidates.get(id);
        if (!entry) throw inputError('候选 ID 不存在或已过期。');
        return entry;
    }
    function revealContent(kind, id) {
        switch (kind) {
        case 'merged':
            if (id !== undefined) throw inputError('合并文字无需指定 ID。');
            return { text: merged().text };
        case 'image':
            if (typeof id !== 'string' || !id) throw inputError('请指定图片 ID。');
            return { text: findImage(id).text };
        case 'candidate': {
            if (typeof id !== 'string' || !id) throw inputError('请指定候选 ID。');
            const fields = candidateEntry(id).candidate.fields;
            return { fields: Object.fromEntries(Object.entries(fields).map(([key, item]) => [key,
                { state: item.state, ...(item.state === 'value' ? { value: item.value } : {}) }])) };
        }
        case 'field': {
            if (typeof id !== 'string' || !FIELD_KEYS.test(id)) throw inputError('请指定字段 key。');
            const field = draft.getSnapshot().fields[id];
            if (!field) throw inputError('字段尚无内容。');
            return { state: field.state, ...(field.state === 'value' ? { value: field.value } : {}) };
        }
        case 'record': {
            if (typeof id !== 'string' || !id) throw inputError('请指定书目结果 ID。');
            const record = records.get(id)?.record;
            if (!record) throw inputError('书目结果 ID 不存在或已过期。');
            return { title: record.title, ...(record.aliases ? { aliases: record.aliases } : {}),
                ...(record.creators ? { creators: record.creators } : {}),
                ...(record.format ? { format: record.format } : {}),
                ...(record.relationship ? { relationship: record.relationship } : {}) };
        }
        default: throw inputError('reveal 类型无效。');
        }
    }
    async function authority(entry) {
        const candidate = entry.candidate;
        const [currentRaw, settings] = await Promise.all([
            get('/api/metadata/schema'),
            candidate.origin === 'legacy' ? Promise.resolve(null) :
                get(candidate.origin === 'rule' ? '/api/settings/extraction-rules' : candidate.origin === 'ai' ? '/api/settings/ai' : '/api/settings/sources'),
        ]);
        const current = decodeMetadataSchema(currentRaw);
        if (current.schema_version !== candidate.schema_version || current.definitions_version !== candidate.definitions_version) throw conflict('字段定义已变化，请重新获取候选。');
        if (candidate.origin === 'legacy') return;
        if (candidate.origin === 'rule') {
            try { validateRuleSet(settings, current); } catch { throw conflict('规则已变化，请重新提取。'); }
            if (settings.rules_version !== entry.rulesVersion) throw conflict('规则已变化，请重新提取。');
        } else if (candidate.origin === 'ai') {
            if (settings.enabled !== true || !settings.model_id || settings.config_version !== entry.configVersion) throw conflict('AI 设置已变化，请重新提取。');
        } else if (candidate.origin === 'provider') {
            const source = settings.sources?.find((item) => item.provider_id === entry.providerId);
            if (source?.enabled !== true || source.config_version !== entry.sourceVersion) throw conflict('来源设置已变化，请重新检索。');
        }
    }
    function remember(list, extra = {}) {
        for (const candidate of list) {
            draft.previewCandidate(candidate);
            if (candidates.size >= 64) throw inputError('候选数量超过限制。');
            candidates.set(candidate.candidate_id, { candidate, ...extra });
        }
        changed();
        return { candidate_count: list.length, candidates: list.map(safeCandidate) };
    }
    async function submissionDocument(acceptPartial) {
        if (images.some((item) => item.status !== 'done') && acceptPartial !== true) throw conflict('仍有图片未识别完成；请等待，或明确 --accept-partial。');
        const current = decodeMetadataSchema(await get('/api/metadata/schema'));
        if (current.schema_version !== schema.schema_version || current.definitions_version !== schema.definitions_version || !isDeepStrictEqual(current.definitions, schema.definitions)) {
            throw conflict('字段定义已变化，请重新创建工作区。');
        }
        const document = draft.getSnapshot();
        const result = await post('/api/metadata/validate', { document });
        if (!isDeepStrictEqual(result?.document, document)) throw conflict('元数据校验结果与当前草稿不一致，请重新核对。');
        return { document, warningCodes: (result.warnings || []).map(safeWarning) };
    }
    async function apply(op, args = {}, expectedRevision) {
        live();
        switch (op) {
        case 'status': return status();
        case 'reveal-prepare': {
            if (expectedRevision !== revision) throw conflict();
            const content = revealContent(args.kind, args.id);
            const bytes = byteLength(JSON.stringify(content));
            if (bytes > REVEAL_MAX_BYTES) throw inputError('所选内容超过单次展示上限。');
            const ticket = randomUUID();
            revealTicket = { ticket, revision, expiresAt: now() + REVEAL_TTL_MS,
                payload: { revision, kind: args.kind, ...(args.id === undefined ? {} : { id: args.id }), content } };
            return { revision, kind: args.kind, ticket, bytes, expires_in_seconds: REVEAL_TTL_MS / 1000 };
        }
        case 'reveal-consume': {
            if (expectedRevision !== revision || !revealTicket || typeof args.ticket !== 'string' || args.ticket !== revealTicket.ticket ||
                revealTicket.revision !== revision || now() >= revealTicket.expiresAt) {
                throw conflict('reveal 票据已失效，请重新准备并核对工作区修订。');
            }
            const payload = revealTicket.payload;
            revealTicket = null;
            return payload;
        }
        case 'review': {
            let mergePreview;
            try { mergePreview = merged(true); }
            catch { mergePreview = { text: '', warning_codes: ['merge_unavailable'] }; }
            return {
            ...status(), document: draft.getSnapshot(), images: images.map((item) => ({ ...safeImage(item), raw_text: item.rawText, edited_text: item.text })),
            candidates: [...candidates.values()].map((entry) => entry.candidate), merged: mergePreview,
            results: [...records.entries()].map(([resultId, entry]) => ({ result_id: resultId, provider_id: entry.source.provider_id, record: entry.record })),
            history: [...history.entries()].map(([entryId, entry]) => ({ entry_id: entryId, task_id: entry.task_id,
                submitted_title: entry.metadata_document?.fields?.title?.value,
                effective_title: entry.effective_metadata_document?.fields?.title?.value,
                has_submitted: Boolean(entry.metadata_document), has_effective: Boolean(entry.effective_metadata_document) })),
            send: sendSnapshot ? { text: sendSnapshot.text, stale: sendSnapshot.stale, target: sendSnapshot.target, model_id: sendSnapshot.modelId, field_keys: sendSnapshot.fieldKeys } : null,
            };
        }
        case 'image-add': {
            if (!Array.isArray(args.paths) || (args.paths.length === 0 && !args.clipboard) || args.paths.length > IMAGE_LIMITS.count) throw inputError('请指定 1–10 张图片。');
            const sources = [];
            for (const path of args.paths) sources.push(await readImageFile(path));
            if (args.clipboard) sources.push(await readClipboardImage());
            if (images.length + sources.length > IMAGE_LIMITS.count || images.reduce((sum, item) => sum + item.bytes.length, 0) + sources.reduce((sum, item) => sum + item.bytes.length, 0) > IMAGE_LIMITS.totalBytes) throw inputError('图片数量或组内总大小超过限制。');
            const added = sources.map(({ bytes, info }) => ({ id: `image-${randomUUID()}`, bytes, info, status: 'queued', rawText: '', text: '', textRevision: 0, dirty: false, generation: 0 }));
            images.push(...added); changed({ input: true }); pump();
            return { revision, images: added.map(safeImage) };
        }
        case 'image-languages': languages = validateLanguages(args.languages); changed({ input: true }); return { revision, languages };
        case 'image-edit': {
            const item = findImage(args.imageId);
            if (typeof args.text !== 'string' || byteLength(args.text) > 65536) throw inputError('单图校对文字不能超过 64 KiB。');
            item.text = args.text; item.dirty = true; item.textRevision++; changed({ input: true }); return { revision, image: safeImage(item) };
        }
        case 'image-accept-raw': {
            const item = findImage(args.imageId); if (item.status !== 'done') throw conflict('此图片尚未识别成功。');
            item.text = item.rawText; item.dirty = false; item.textRevision++; changed({ input: true }); return { revision, image: safeImage(item) };
        }
        case 'image-retry': {
            const item = findImage(args.imageId); if (['queued', 'running'].includes(item.status)) throw conflict('图片已在识别队列中。');
            item.status = 'queued'; changed({ input: true }); pump(); return { revision, image: safeImage(item) };
        }
        case 'image-remove': {
            const item = findImage(args.imageId); item.generation++; images.splice(images.indexOf(item), 1);
            item.bytes.fill(0); item.rawText = ''; item.text = ''; changed({ input: true }); return { revision, image_count: images.length };
        }
        case 'image-move': {
            const item = findImage(args.imageId); const old = images.indexOf(item); const next = old + (args.direction === 'up' ? -1 : args.direction === 'down' ? 1 : 0);
            if (next < 0 || next >= images.length || next === old) throw inputError('图片无法按指定方向移动。');
            [images[old], images[next]] = [images[next], images[old]]; changed({ input: true }); return { revision, order: images.map((entry) => entry.id) };
        }
        case 'image-cancel': {
            for (const item of images.filter((entry) => entry.status === 'queued')) item.status = 'cancelled';
            const active = images.find((entry) => entry.status === 'running'); if (active) { active.generation++; active.status = 'cancelled'; await recognizer.close(); }
            changed({ input: true }); return { revision, images: images.map(safeImage) };
        }
        case 'text-set': {
            if (typeof args.text !== 'string' || byteLength(args.text) > 65536) throw inputError('补充文字不能超过 64 KiB。');
            manualText = args.text; changed({ input: true }); return { revision, text_bytes: byteLength(manualText) };
        }
        case 'merge': {
            const result = merged(args.acceptPartial === true);
            return { revision, text_bytes: byteLength(result.text), excluded_image_ids: result.excluded, warning_codes: result.warnings };
        }
        case 'field-set': {
            const definition = draft.getDefinition(args.key);
            if (!FIELD_KEYS.test(args.key || '') || !definition) throw inputError('字段不存在。');
            try { validateFieldValue(definition, args.value); draft.setField(args.key, args.value); }
            catch { throw inputError('字段值不符合定义；请检查类型、长度和选项。'); }
            changed(); return { revision, document_revision: draft.getSnapshot().revision, field_key: args.key };
        }
        case 'field-clear': draft.clearField(args.key); changed(); return { revision, document_revision: draft.getSnapshot().revision, field_key: args.key };
        case 'field-unlock': draft.unlockField(args.key); changed(); return { revision, document_revision: draft.getSnapshot().revision, field_key: args.key };
        case 'rules-preview': {
            const ruleSet = await get('/api/settings/extraction-rules'); validateRuleSet(ruleSet, schema);
            const source = merged(args.acceptPartial === true);
            const result = extractRules({ text: source.text, segments: source.segments, ruleSet, schema, document: draft.getSnapshot(), inputRevision, configRevision });
            const remembered = remember(result.candidates, { rulesVersion: ruleSet.rules_version });
            return { ...remembered, warning_codes: result.warnings.map(safeWarning) };
        }
        case 'provider-list': {
            const data = await get('/api/metadata/providers');
            if (!Array.isArray(data?.sources)) throw new CliError('invalid_response', '书目来源响应无效。', EXIT.transport);
            return { sources: data.sources.map((item) => ({ provider_id: item.provider_id, enabled: item.enabled === true, config_version: item.config_version, visibility: safeCode(item.visibility) })) };
        }
        case 'provider-search': {
            if (typeof args.keyword !== 'string' || !args.keyword.trim() || byteLength(args.keyword) > 512 || !Array.isArray(args.providerIds) || !args.providerIds.length) throw inputError('请提供关键词及至少一个来源 ID。');
            changed({ input: true });
            const result = await post('/api/metadata/search', { keyword: args.keyword, provider_ids: args.providerIds, query_revision: inputRevision });
            if (result.query_revision !== inputRevision || !Array.isArray(result.sources)) throw conflict('搜索结果与工作区修订不一致。');
            const rows = [];
            for (const source of result.sources) for (const record of source.candidates || []) {
                const id = `result-${randomUUID()}`; records.set(id, { source, record, queryRevision: inputRevision });
                rows.push({ result_id: id, provider_id: source.provider_id, relationship: safeCode(record.relationship), format: safeCode(record.format) });
            }
            changed(); return { revision, result_count: rows.length, results: rows, sources: result.sources.map((item) => ({ provider_id: item.provider_id, error_code: item.error ? safeCode(item.error.code) : null, result_count: item.candidates?.length || 0 })) };
        }
        case 'provider-resolve': {
            const chosen = records.get(args.resultId); if (!chosen) throw inputError('搜索结果 ID 不存在，请重新搜索。');
            const document = draft.getSnapshot(); const context = draft.getContext();
            const request = { provider_id: chosen.source.provider_id, record_id: chosen.record.record_id, query_revision: chosen.queryRevision,
                base_document_revision: document.revision, field_revisions: Object.fromEntries(Object.entries(document.fields).map(([key, field]) => [key, field.revision])),
                schema_version: document.schema_version, definitions_version: document.definitions_version, config_version: chosen.source.config_version,
                ...(context.configRevision === undefined ? {} : { config_revision: context.configRevision }), custom_mappings: args.customMappings || {} };
            const result = await post('/api/metadata/candidates/resolve', request);
            if (result.config_version !== request.config_version || result.candidate?.input_revision !== inputRevision) throw conflict('书目候选已过期。');
            return remember([result.candidate], { providerId: request.provider_id, sourceVersion: request.config_version });
        }
        case 'history-list': {
            const limit = Number.isSafeInteger(args.limit) && args.limit > 0 && args.limit <= 100 ? args.limit : 20;
            const entries = await get(`/api/metadata-history?limit=${limit}`); if (!Array.isArray(entries)) throw new CliError('invalid_response', '元数据历史响应无效。', EXIT.transport);
            history.clear(); const rows = entries.map((entry) => { const id = `history-${randomUUID()}`; history.set(id, entry); return { entry_id: id, has_submitted: Boolean(entry.metadata_document), has_effective: Boolean(entry.effective_metadata_document) }; });
            changed(); return { revision, entries: rows };
        }
        case 'history-select': {
            const entry = history.get(args.entryId); if (!entry) throw inputError('历史记录 ID 不存在。');
            const candidate = candidateFromHistory({ entry, view: args.view || 'submitted', draft, requestId: randomUUID() });
            return remember([candidate]);
        }
        case 'candidate-preview': {
            const candidate = candidateEntry(args.candidateId).candidate;
            return { revision, candidate: safeCandidate(candidate), fields: draft.previewCandidate(candidate).map((row) => ({ key: row.key, conflict_code: row.conflict ? 'candidate_conflict' : null, requires_confirmation: row.requiresConfirmation })) };
        }
        case 'candidate-adopt': {
            const entry = candidateEntry(args.candidateId);
            if (!Array.isArray(args.keys) || !args.keys.length || args.keys.some((key) => !Object.hasOwn(entry.candidate.fields, key))) throw inputError('请指定候选中要采用的字段 key。');
            await authority(entry);
            try { draft.applyCandidate(entry.candidate, args.keys, { confirmLocked: args.confirmLocked === true }); }
            catch { throw conflict('候选与当前草稿冲突，请重新核对。'); }
            candidates.delete(args.candidateId); changed();
            return { revision, document_revision: draft.getSnapshot().revision, adopted_keys: args.keys };
        }
        case 'ai-prepare': {
            const [ai, currentSchemaRaw] = await Promise.all([get('/api/settings/ai'), get('/api/metadata/schema')]);
            const currentSchema = decodeMetadataSchema(currentSchemaRaw);
            if (currentSchema.definitions_version !== schema.definitions_version) throw conflict('字段定义已变化，请重新创建工作区。');
            if (ai?.enabled !== true || !ai.model_id || !Number.isSafeInteger(ai.config_version)) throw conflict('AI 设置尚未启用或模型未保存。');
            const keys = args.keys;
            if (!Array.isArray(keys) || !keys.length || keys.length > 64 || keys.some((key) => !schema.definitions[key]?.enabled || !schema.definitions[key].extractable?.includes('ai'))) throw inputError('AI 字段选择无效。');
            const source = merged(args.acceptPartial === true);
            if (!source.text.trim()) throw inputError('没有可发送的文字。');
            configRevision = ai.config_version; draft.setContext({ inputRevision, configRevision });
            sendSnapshot = { text: source.text, fieldKeys: [...new Set(keys)], modelId: ai.model_id, target: ai.base_url, configVersion: ai.config_version, rulesVersion: undefined, stale: false, inputRevision };
            changed(); return { revision, text_bytes: byteLength(source.text), field_keys: sendSnapshot.fieldKeys, model_id: sendSnapshot.modelId, review_required: true };
        }
        case 'ai-edit': {
            if (!sendSnapshot) throw conflict('请先生成 AI 发送预览。');
            if (typeof args.text !== 'string' || !args.text.trim() || byteLength(args.text) > 65536) throw inputError('AI 发送文字必须在 1–64 KiB 之间。');
            sendSnapshot.text = args.text; sendSnapshot.stale = false; sendSnapshot.inputRevision = inputRevision; reviewTicket = null; changed();
            return { revision, text_bytes: byteLength(args.text), review_required: true };
        }
        case 'ai-retain': {
            if (!sendSnapshot) throw conflict('请先生成 AI 发送预览。');
            sendSnapshot.stale = false; sendSnapshot.inputRevision = inputRevision; reviewTicket = null; changed();
            return { revision, text_bytes: byteLength(sendSnapshot.text), review_required: true };
        }
        case 'ai-review': {
            if (!sendSnapshot || sendSnapshot.stale || sendSnapshot.inputRevision !== inputRevision) throw conflict('AI 发送预览已过期，请重新生成。');
            reviewTicket = randomUUID(); return { ticket: reviewTicket, text: sendSnapshot.text, target: sendSnapshot.target, model_id: sendSnapshot.modelId, field_keys: sendSnapshot.fieldKeys };
        }
        case 'ai-extract': {
            if (!sendSnapshot || sendSnapshot.stale || sendSnapshot.inputRevision !== inputRevision || args.ticket !== reviewTicket) throw conflict('请在独立终端重新核对发送文字后确认。');
            reviewTicket = null;
            const ai = await get('/api/settings/ai');
            if (ai.enabled !== true || ai.config_version !== sendSnapshot.configVersion || ai.model_id !== sendSnapshot.modelId || ai.base_url !== sendSnapshot.target) throw conflict('AI 目标或模型已变化。');
            const document = draft.getSnapshot(); const context = draft.getContext(); const requestId = `ai-${randomUUID()}`;
            const request = { request_id: requestId, text: sendSnapshot.text, field_keys: sendSnapshot.fieldKeys, schema_version: schema.schema_version, definitions_version: schema.definitions_version,
                base_document_revision: document.revision, field_revisions: Object.fromEntries(sendSnapshot.fieldKeys.map((key) => [key, document.fields[key]?.revision || 0])),
                input_revision: context.inputRevision, config_revision: ai.config_version };
            const result = await post('/api/metadata/extract', request, 130_000);
            if (request.input_revision !== inputRevision || document.revision !== draft.getSnapshot().revision || result.request_id !== requestId || !Array.isArray(result.candidates)) throw conflict('提取结果与当前草稿修订不一致。');
            return { ...remember(result.candidates, { configVersion: ai.config_version }), warning_codes: (result.warnings || []).map(safeWarning) };
        }
        case 'validate': {
            const result = await post('/api/metadata/validate', { document: draft.getSnapshot() });
            return { valid: true, document_revision: result.document?.revision, warning_codes: (result.warnings || []).map(safeWarning) };
        }
        case 'task-create-url':
        case 'task-upload': {
            if (typeof submit !== 'function') throw new CliError('unavailable', '任务提交暂不可用。', EXIT.unavailable);
            if (op === 'task-create-url' && (typeof args.url !== 'string' || !args.url.trim())) throw inputError('请提供 Telegraph 链接。');
            if (op === 'task-upload' && (typeof args.file !== 'string' || !args.file)) throw inputError('请指定本地压缩包。');
            const expectedRevision = revision;
            const { document, warningCodes } = await submissionDocument(args.acceptPartial);
            if (revision !== expectedRevision || !isDeepStrictEqual(draft.getSnapshot(), document)) throw conflict('工作区在校验期间已变化，请重新核对后提交。');
            const action = op === 'task-create-url' ? 'create-url' : 'upload';
            const result = await submit(action, args, document);
            const snapshotAttached = action === 'upload' || result.created === true;
            if (snapshotAttached) {
                lastSubmission = { task_id: result.task.id, document_revision: document.revision, snapshot_attached: true };
                changed();
            }
            return { ...result, revision, document_revision: document.revision, snapshot_attached: snapshotAttached, warning_codes: warningCodes };
        }
        case 'document': return { document: draft.getSnapshot() }; // private IPC; never machine stdout
        case 'close': await close(); return { closed: true };
        default: throw new CliError('invalid_command', '未知工作区操作。');
        }
    }
    async function close() {
        if (stopped) return;
        stopped = true; await recognizer.close().catch(() => {});
        for (const item of images) { item.bytes.fill(0); item.rawText = ''; item.text = ''; }
        images.length = 0; candidates.clear(); records.clear(); history.clear(); sendSnapshot = null; manualText = ''; reviewTicket = null; revealTicket = null; lastSubmission = null; draft.dispose(); onChange();
    }
    return { apply, close, status, get revision() { return revision; } };
}
