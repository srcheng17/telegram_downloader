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
    let mounted = false;
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
        cancel();
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
    async function resolve(source, record) {
        cancel(); const ticket = sequence; const base = snapshot(); controller = new AbortController();
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
            if (!current(ticket, base)) return;
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
            showCandidate(payload.candidate, { beforeApply });
            status.textContent = '详情已送至字段对照区。请核对作品与版本，再明确选择要采用的字段；现有草稿尚未修改。';
        } catch (error) { if (current(ticket, base) && error.name !== 'AbortError') status.textContent = error.message; }
        finally { if (ticket === sequence) controller = null; }
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
    async function mount() {
        if (mounted || !root) return;
        mounted = true; cancel(); const ticket = sequence; controller = new AbortController();
        root.replaceChildren();
        root.appendChild(element('h3', '搜索书目'));
        status = element('p', '正在加载来源设置…'); status.setAttribute('role', 'status'); status.setAttribute('aria-live', 'polite'); root.appendChild(status);
        try {
            const { response, payload } = await api.getJson('/api/metadata/providers', { signal: controller.signal });
            if (!mounted || ticket !== sequence) return;
            if (!response.ok || !Array.isArray(payload?.sources)) throw new Error('来源设置加载失败，请重新打开工作区。');
            sources = payload.sources;
            const label = element('label', '确认要发送的标题或别名关键词'); keyword = element('input'); keyword.type = 'text'; keyword.maxLength = 512; keyword.id = 'provider-keyword'; label.appendChild(keyword); root.appendChild(label); listen(keyword, 'input', invalidated);
            const list = element('fieldset'); list.appendChild(element('legend', '选择本次发送来源'));
            choices = sources.map(source => {
                const label = element('label', `${source.descriptor.label} · 优先级 ${source.priority} · ${source.enabled ? (source.credential_configured ? '凭据已配置' : '匿名') : '未启用'}`);
                const input = element('input'); input.type = 'checkbox'; input.id = `provider-select-${source.provider_id}`; input.disabled = !source.enabled; label.appendChild(input); list.appendChild(label); listen(input, 'change', invalidated); return { source, input };
            }); root.appendChild(list);
            const link = element('a', '管理来源启停、优先级和授权'); link.href = '/settings'; root.appendChild(link);
            renderMappings(root);
            confirmation = element('p'); root.appendChild(confirmation);
            searchButton = element('button', '确认关键词与来源并搜索'); searchButton.id = 'provider-search-submit'; searchButton.type = 'button'; listen(searchButton, 'click', () => { void search(); }); root.appendChild(searchButton);
            results = element('div'); results.id = 'provider-search-results'; root.appendChild(results);
            unsubscribe = getDraft().subscribe(() => { if (controller) { cancel(); status.textContent = '草稿已修改，旧请求已取消。请重新核对所选作品。'; } });
            updateConfirmation(); status.textContent = sources.some(source => source.enabled) ? '选择来源并确认关键词后开始。' : '尚未启用书目来源；手工录入与下载仍可继续。';
            controller = null;
        } catch (error) { if (mounted && ticket === sequence && error.name !== 'AbortError') status.textContent = error.message; }
    }
    function unmount() { if (!mounted) return; mounted = false; cancel(); unsubscribe?.(); unsubscribe = null; cleanup.splice(0).forEach(remove => remove()); choices = []; mappings = []; sources = []; root.replaceChildren(); }
    return { mount, unmount };
}
