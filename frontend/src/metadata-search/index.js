import { createTasksApi } from '../shared/api/tasks_api.js';
import { decodeMetadataSchema } from '../shared/metadata/schema.js';

const errors = {
    no_results: '没有匹配结果；不能据此判断条目不存在或被过滤。',
    auth_required: '此请求需要来源授权。', auth_invalid: '来源凭据无效，请在设置中检查。',
    permission_denied: '当前来源权限不足。', rate_limited: '来源请求过于频繁，请稍后重试。',
    timeout: '来源请求超时。', unavailable: '来源暂不可用。', invalid_response: '来源返回格式暂不支持。',
    cancelled: '查询已取消。', search_conflict: '来源设置或字段定义已变化，请重新搜索。',
};
const relationships = { unknown: '层级待核对', series: '系列条目', volume: '单卷条目', volume_in_series: '所属系列已识别' };

export function createMetadataSearchModule({ root, doc = globalThis.document, api = createTasksApi(), getDraft, getRegistry, showCandidate, onInputChange = () => getDraft().getContext() }) {
    let mounted = false; let ready; let prepared = null;
    let sequence = 0;
    let controller;
    let sources = [];
    let choices = [];
    let mappings = [];
    let keyword;
    let status;
    let confirmation;
    let results;
    let searchButton;
    let unsubscribe;
    const cleanup = [];
    function element(tag, text = '') { const node = doc.createElement(tag); node.textContent = text; return node; }
    function listen(node, type, handler) { node.addEventListener(type, handler); cleanup.push(() => node.removeEventListener(type, handler)); }
    function selected() { return choices.filter(choice => choice.input.checked && !choice.input.disabled).map(choice => choice.source.provider_id); }
    function cancel() { sequence += 1; controller?.abort(); controller = null; }
    function invalidated() {
        cancel(); prepared = null;
        onInputChange('provider');
        results.replaceChildren();
        status.textContent = '关键词或来源已变化，请确认后重新搜索。';
        updateConfirmation();
    }
    function updateConfirmation() {
        const labels = choices.filter(choice => selected().includes(choice.source.provider_id)).map(choice => choice.source.descriptor.label);
        confirmation.textContent = `将发送关键词「${keyword.value.trim()}」到：${labels.join('、') || '尚未选择来源'}。仅发送本次关键词，不发送截图或完整识别文本。`;
        searchButton.disabled = labels.length === 0 || !keyword.value.trim();
    }
    function snapshot() {
        const draft = getDraft();
        const document = draft.getSnapshot();
        const context = draft.getContext();
        return { draft, document, context };
    }
    function current(ticket, base) {
        if (!mounted || ticket !== sequence || !base) return false;
        try {
            const now = snapshot();
            return now.draft === base.draft && now.document.revision === base.document.revision && now.document.definitions_version === base.document.definitions_version && now.context.inputRevision === base.context.inputRevision && now.context.configRevision === base.context.configRevision;
        } catch { return false; }
    }
    function attribution(parent, info) {
        const text = info?.attribution;
        if (!text) return;
        const note = element('p', `${text.label}：${text.license}`);
        parent.appendChild(note);
        // Links come only from the fixed local descriptor, never remote response URLs.
        const allowed = ['https://mangabaka.org/about/data-license', 'https://api.mangaupdates.com/', 'https://bgm.tv/about/copyright'];
        if (allowed.includes(text.license_url)) {
            const link = element('a', '查看来源条款'); link.href = text.license_url; link.target = '_blank'; link.rel = 'noopener noreferrer'; parent.appendChild(link);
        }
    }
    async function resolve(source, record, { automatic = false, signal } = {}) {
        cancel(); const ticket = sequence; const base = snapshot(); controller = new AbortController();
        const request = controller; const abort = () => request.abort(); signal?.addEventListener('abort', abort, { once:true }); if (signal?.aborted) request.abort();
        status.textContent = '正在加载所选条目的字段，请稍候…';
        const customMappings = {};
        for (const mapping of mappings) {
            const [provider, field] = mapping.input.value.split(':');
            if (provider === source.provider_id && field) customMappings[mapping.key] = field;
        }
        const input = {
            provider_id: source.provider_id, record_id: record.record_id, query_revision: base.context.inputRevision,
            base_document_revision: base.document.revision,
            field_revisions: Object.fromEntries(Object.entries(base.document.fields).map(([key, field]) => [key, field.revision])),
            schema_version: base.document.schema_version, definitions_version: base.document.definitions_version,
            config_version: source.config_version, ...(base.context.configRevision === undefined ? {} : { config_revision: base.context.configRevision }),
            custom_mappings: customMappings,
        };
        try {
            const { response, payload } = await api.postJson('/api/metadata/candidates/resolve', input, { signal: controller.signal });
            if (!current(ticket, base) || request.signal.aborted) { if (automatic) throw new DOMException('查询已取消。', 'AbortError'); return; }
            if (!response.ok || !payload?.candidate) throw new Error(errors[payload?.code] || '详情加载失败，请重试。');
            if (payload.config_version !== source.config_version || payload.candidate.input_revision !== base.context.inputRevision || payload.candidate.config_revision !== base.context.configRevision) throw new Error('候选已过期，请重新搜索。');
            const beforeApply = async ({ signal }) => {
                const checkCurrent = () => {
                    if (!mounted || ticket !== sequence || signal.aborted || getDraft() !== base.draft) throw new Error('候选已过期。');
                };
                checkCurrent();
                const [registry, settings] = await Promise.all([
                    api.getJson('/api/metadata/schema', { signal, cache: 'no-store' }),
                    api.getJson('/api/settings/sources', { signal, cache: 'no-store' }),
                ]);
                checkCurrent();
                if (!registry.response.ok || !settings.response.ok || !Array.isArray(settings.payload?.sources)) throw new Error('候选校验暂不可用。');
                const schema = decodeMetadataSchema(registry.payload);
                if (schema.schema_version !== input.schema_version || schema.definitions_version !== input.definitions_version) throw new Error('字段定义已变化。');
                const configured = settings.payload.sources.find(item => item.provider_id === input.provider_id);
                // config_revision belongs to the shared AI context. The source has its own config_version.
                if (!Number.isSafeInteger(input.config_version) || configured?.enabled !== true || configured.config_version !== input.config_version) throw new Error('来源设置已变化。');
            };
            if (automatic) return { candidate:payload.candidate,beforeApply };
            showCandidate(payload.candidate, { beforeApply });
            status.textContent = '详情已送至字段对照区。请核对作品与版本，再明确选择要采用的字段；现有草稿尚未修改。';
        } catch (error) { if (automatic) throw error; if (current(ticket, base) && error.name !== 'AbortError') status.textContent = error.message; }
        finally { signal?.removeEventListener('abort',abort); if (ticket === sequence) controller = null; }
    }
    function renderResults(payload) {
        results.replaceChildren();
        for (const source of payload.sources) {
            const group = element('section');
            const configured = sources.find(item => item.provider_id === source.provider_id);
            group.appendChild(element('h4', configured?.descriptor?.label || source.provider_id));
            attribution(group, source);
            if (source.visibility === 'permission_dependent') group.appendChild(element('p', 'Bangumi 的结果受授权与可见性影响；本次搜索无法证明成人资料覆盖率。'));
            if (source.error) group.appendChild(element('p', `${errors[source.error.code] || '来源请求未完成。'}${source.error.retry_after ? ` 建议等待 ${source.error.retry_after} 秒。` : ''}`));
            for (const record of source.candidates || []) {
                const row = element('article');
                row.appendChild(element('h5', record.title));
                row.appendChild(element('p', `来源编号：${record.record_id} · ${record.format || '类型未知'} · ${relationships[record.relationship] || '层级待核对'}`));
                if (record.aliases?.length) row.appendChild(element('p', `别名：${record.aliases.join(' / ')}`));
                for (const [role, names] of Object.entries(record.creators || {})) if (names.length) row.appendChild(element('p', `${({ writer: '作者／原作', penciller: '作画', translator: '翻译' })[role] || role}：${names.join('、')}`));
                const button = element('button', '选择并核对字段'); button.type = 'button';
                // Row listeners disappear together with their owned DOM subtree.
                button.onclick = () => { void resolve(source, record); };
                row.appendChild(button); group.appendChild(row);
            }
            results.appendChild(group);
        }
    }
    async function search() {
        if (searchButton.disabled) return;
        cancel(); onInputChange('provider'); const ticket = sequence; const base = snapshot(); controller = new AbortController();
        const ids = selected(); const query = keyword.value.trim();
        results.replaceChildren(); status.textContent = '正在向已确认的来源查询…';
        try {
            const { response, payload } = await api.postJson('/api/metadata/search', { keyword: query, provider_ids: ids, query_revision: base.context.inputRevision }, { signal: controller.signal });
            if (!current(ticket, base)) return;
            if (!response.ok || !Array.isArray(payload?.sources)) throw new Error(errors[payload?.code] || '查询失败，请重试。');
            if (payload.query_revision !== base.context.inputRevision) throw new Error('查询已过期，请重试。');
            renderResults(payload); status.textContent = '查询结束。请明确选择作品；同名作品不会自动合并。';
        } catch (error) { if (current(ticket, base) && error.name !== 'AbortError') status.textContent = error.message; }
        finally { if (ticket === sequence) controller = null; }
    }
    async function prepare({ title = '', aliases = [], writers = [], signal, retry = false } = {}) {
        if (signal?.aborted) throw new DOMException('查询已取消。', 'AbortError');
        if (retry) {
            const previous = {
                keyword: keyword?.value,
                choices: new Map(choices.map(choice => [choice.source.provider_id, { enabled: choice.source.enabled, checked: choice.input.checked }])),
                mappings: new Map(mappings.map(mapping => [mapping.key, mapping.input.value])),
            };
            unmount();
            ready = hydrate({ signal, previous });
        }
        await mount();
        if (signal?.aborted || !mounted) throw new DOMException('查询已取消。', 'AbortError');
        if (!keyword || !searchButton) return { entries:[],warnings:['来源设置暂不可用，可以手工继续。'] };
        const titles = [...new Set([title,...aliases].filter(value => typeof value === 'string' && value.trim()).map(value => value.trim()))];
        const ids = selected();
        if (!titles.length || !ids.length) return { entries:[],warnings:[!titles.length ? '尚无标题或别名；可以手工填写后继续。' : '没有启用的书目来源，可以继续。'] };
        const queries = titles.slice(0,2);
        const cacheKey = JSON.stringify({queries,writers,ids,context:getDraft().getContext(),versions:choices.filter(choice => ids.includes(choice.source.provider_id)).map(choice => choice.source.config_version)});
        if (prepared?.key === cacheKey) return prepared.result;
        keyword.value = queries[0]; updateConfirmation();
        cancel(); const ticket = sequence; const base = snapshot(); controller = new AbortController(); const request = controller;
        const abort = () => request.abort(); signal?.addEventListener('abort',abort,{once:true});
        const check = () => { if (signal?.aborted || request.signal.aborted || !current(ticket,base)) throw new DOMException('查询已取消。','AbortError'); };
        const warnings = []; const groups = new Map(); let partialFailure = false;
        status.textContent = '正在用标题和别名查询已启用来源…';
        try {
            for (const query of queries) {
                check();
                const {response,payload} = await api.postJson('/api/metadata/search',{keyword:query,provider_ids:ids,query_revision:base.context.inputRevision},{signal:request.signal});
                check();
                if (!response.ok || !Array.isArray(payload?.sources) || payload.query_revision !== base.context.inputRevision) { partialFailure = true; warnings.push(errors[payload?.code] || '书目查询暂不可用，可以继续。'); continue; }
                for (const source of payload.sources) {
                    if (!ids.includes(source.provider_id)) continue;
                    if (source.error && source.error.code !== 'no_results') { partialFailure = true; warnings.push(errors[source.error.code] || '部分来源未完成，可以继续。'); }
                    const group = groups.get(source.provider_id) || {...source,candidates:[]};
                    for (const record of source.candidates || []) if (!group.candidates.some(item => item.record_id === record.record_id)) group.candidates.push(record);
                    groups.set(source.provider_id,group);
                }
                if (ids.some(id => !payload.sources.some(source => source.provider_id === id))) { partialFailure = true; warnings.push('部分来源未返回结果，可以继续。'); }
            }
            check(); renderResults({sources:[...groups.values()]});
            // Literal title/alias plus the complete writer identity and explicit
            // single-volume level. Similarity, a first row or a series record
            // alone cannot establish that this is the uploaded work.
            const normalize = value => String(value).normalize('NFC').trim().replace(/\s+/gu,' ').toLocaleLowerCase('en');
            const names = values => [...new Set(values.map(normalize))].sort();
            const expectedWriters = names(writers);
            const matches = [];
            for (const source of groups.values()) for (const record of source.candidates || []) {
                if (record.relationship !== 'volume' || !expectedWriters.length) continue;
                if (![record.title,...(record.aliases || [])].some(value => titles.some(title => normalize(title) === normalize(value)))) continue;
                if (JSON.stringify(names(record.creators?.writer || [])) !== JSON.stringify(expectedWriters)) continue;
                matches.push({source,record});
            }
            const entries = [];
            if (matches.length === 1 && !partialFailure) {
                // resolve owns a new request generation and its adoption preflight.
                const entry = await resolve(matches[0].source,matches[0].record,{automatic:true,signal});
                if (entry) entries.push(entry);
            } else warnings.push(matches.length > 1 ? '存在多个同名同作者条目，请在高级书目结果中核对版本。' : '没有可唯一确认身份的书目条目，已保留识别结果；可跳过或手工选择。');
            const result = {entries,warnings};
            if (!partialFailure) prepared = {key:cacheKey,result};
            status.textContent = entries.length ? '已准备身份一致的书目建议，等待集中核对。' : warnings.join(' ');
            return result;
        } catch(error) {
            if (error.name === 'AbortError' || signal?.aborted || !mounted) throw new DOMException('查询已取消。','AbortError');
            const result = {entries:[],warnings:[...warnings,'书目查询暂不可用，识别结果已保留，可以继续。']};
            if (mounted) status.textContent = result.warnings.join(' '); return result;
        } finally { signal?.removeEventListener('abort',abort); if (controller === request) controller = null; }
    }
    function renderMappings(parent) {
        const definitions = getRegistry?.()?.definitions || {};
        for (const [key, definition] of Object.entries(definitions)) {
            if (!key.startsWith('custom.user.') || definition.enabled === false || !definition.extractable?.includes('provider')) continue;
            const options = sources.flatMap(source => (source.custom_fields || []).filter(field => field.type === definition.type).map(field => ({ value: `${source.provider_id}:${field.key}`, text: `${source.descriptor.label} · ${field.label}` })));
            if (!options.length) continue;
            const label = element('label', `${definition.label}（显式来源映射）`);
            const input = element('select'); input.id = `provider-mapping-${key}`;
            const empty = element('option', '不采用来源扩展字段'); empty.value = ''; input.appendChild(empty);
            for (const option of options) { const item = element('option', option.text); item.value = option.value; input.appendChild(item); }
            label.appendChild(input); parent.appendChild(label); mappings.push({ key, input }); listen(input, 'change', invalidated);
        }
    }
    function mount() { if (!mounted) ready = hydrate(); return ready; }
    async function hydrate({ signal, previous } = {}) {
        if (mounted || !root) return;
        mounted = true; cancel(); const ticket = sequence; controller = new AbortController();
        const request = controller; const abort = () => request.abort();
        signal?.addEventListener('abort', abort, { once: true }); if (signal?.aborted) request.abort();
        root.replaceChildren();
        root.appendChild(element('h3', '搜索书目'));
        status = element('p', '正在加载来源设置…'); status.setAttribute('role', 'status'); status.setAttribute('aria-live', 'polite'); root.appendChild(status);
        try {
            const { response, payload } = await api.getJson('/api/metadata/providers', { signal: request.signal, cache: 'no-store' });
            if (!mounted || ticket !== sequence || request.signal.aborted) return;
            if (!response.ok || !Array.isArray(payload?.sources)) throw new Error('来源设置加载失败，请重新打开工作区。');
            sources = payload.sources;
            const label = element('label', '确认要发送的标题或别名关键词'); keyword = element('input'); keyword.type = 'text'; keyword.maxLength = 512; keyword.id = 'provider-keyword'; keyword.value = getDraft().getSnapshot().fields.title?.value || ''; label.appendChild(keyword); root.appendChild(label); listen(keyword, 'input', invalidated);
            const list = element('fieldset'); list.appendChild(element('legend', '选择本次发送来源'));
            choices = sources.map(source => {
                const label = element('label', `${source.descriptor.label} · 优先级 ${source.priority} · ${source.enabled ? (source.credential_configured ? '凭据已配置' : '匿名') : '未启用'}`);
                const input = element('input'); input.type = 'checkbox'; input.id = `provider-select-${source.provider_id}`; input.disabled = !source.enabled; input.checked = source.enabled === true; label.appendChild(input); list.appendChild(label); listen(input, 'change', invalidated); return { source, input };
            }); const advanced = element('details'); advanced.appendChild(element('summary', '高级书目来源与字段')); advanced.appendChild(list); root.appendChild(advanced);
            const link = element('a', '管理来源启停、优先级和授权'); link.href = '/settings'; root.appendChild(link);
            renderMappings(advanced);
            if (previous) {
                keyword.value = previous.keyword ?? keyword.value;
                for (const choice of choices) {
                    const old = previous.choices.get(choice.source.provider_id);
                    if (old?.enabled && choice.source.enabled) choice.input.checked = old.checked;
                }
                for (const mapping of mappings) {
                    const value = previous.mappings.get(mapping.key);
                    if ([...mapping.input.children].some(option => option.value === value)) mapping.input.value = value;
                }
            }
            confirmation = element('p'); root.appendChild(confirmation);
            searchButton = element('button', '确认关键词与来源并搜索'); searchButton.id = 'provider-search-submit'; searchButton.type = 'button'; listen(searchButton, 'click', () => { void search(); }); root.appendChild(searchButton);
            results = element('div'); results.id = 'provider-search-results'; root.appendChild(results);
            unsubscribe = getDraft().subscribe(() => { if (controller) { cancel(); status.textContent = '草稿已修改，旧请求已取消。请重新核对所选作品。'; } });
            updateConfirmation(); status.textContent = sources.some(source => source.enabled) ? '已默认选择启用来源；添加素材后可自动准备，也可手工搜索。' : '尚未启用书目来源；手工录入与下载仍可继续。';
            controller = null;
        } catch (error) { if (mounted && ticket === sequence && !request.signal.aborted && error.name !== 'AbortError') status.textContent = '来源设置加载失败，请重试自动准备。'; }
        finally { signal?.removeEventListener('abort', abort); if (controller === request) controller = null; }
    }
    function unmount() { if (!mounted) return; mounted = false; cancel(); unsubscribe?.(); unsubscribe = null; cleanup.splice(0).forEach(remove => remove()); choices = []; mappings = []; sources = []; keyword = null; searchButton = null; prepared = null; ready = null; root.replaceChildren(); }
    return { mount, unmount, prepare, cancelPreparation:cancel };
}
