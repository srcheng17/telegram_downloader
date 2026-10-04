const endpoint = '/api/settings/metadata-fields';
const fieldTypes = [['string', '文本'], ['string[]', '文本列表'], ['integer', '整数'], ['boolean', '是／否']];
const extractionKinds = [['archive', '归档导入'], ['ocr', '截图识别'], ['rule', '提取规则'], ['ai', 'AI 建议'], ['provider', '书目来源'], ['legacy', '历史回填']];

// The shared transport owns authentication and CSRF. This module owns only its
// settings slot, requests and listeners; no page-global state or secret storage.
export function createMetadataFieldsModule({ doc = document, api } = {}) {
    let root = null;
    let version = '';
    let limits = null;
    let rows = [];
    let list;
    let status;
    let add;
    let save;
    let reload;
    let controller = null;
    let generation = 0;
    let busy = false;
    let listeners = [];

    function element(tag, text, id) {
        const node = doc.createElement(tag);
        if (text !== undefined) node.textContent = text;
        if (id) node.id = id;
        return node;
    }
    function listen(node, type, handler) {
        node.addEventListener(type, handler);
        listeners.push(() => node.removeEventListener(type, handler));
    }
    function button(text, id, action) {
        const node = element('button', text, id);
        node.type = 'button';
        node.className = 'btn btn-secondary';
        listen(node, 'click', action);
        return node;
    }
    function feedback(text) { if (status) status.textContent = text; }
    function updateControls() {
        if (!root) return;
        add.disabled = busy || !limits || rows.length >= limits.custom_fields;
        save.disabled = busy || !version;
        reload.disabled = busy;
        for (const row of rows) row.container.disabled = busy;
    }
    function labeled(container, text, control) {
        const label = element('label', text);
        label.setAttribute('for', control.id);
        container.appendChild(label);
        container.appendChild(control);
        return control;
    }
    function input(id, type, value) {
        const node = element('input', undefined, id);
        node.type = type;
        if (type === 'checkbox') node.checked = value;
        else node.value = value;
        return node;
    }
    function appendRow(definition = null) {
        if (!limits || rows.length >= limits.custom_fields || busy) return;
        const index = rows.length;
        const prefix = `metadata-field-${index}`;
        const container = element('fieldset');
        container.className = 'settings-field-group';
        container.appendChild(element('legend', definition?.label || `新字段 ${index + 1}`));
        const name = labeled(container, '字段标识（小写字母、数字、下划线）', input(`${prefix}-name`, 'text', definition?.key.replace(/^custom\.user\./, '') || ''));
        name.maxLength = 32;
        name.disabled = !!definition;
        const label = labeled(container, '中文名称', input(`${prefix}-label`, 'text', definition?.label || ''));
        label.maxLength = 128;
        const type = element('select', undefined, `${prefix}-type`);
        for (const [value, text] of fieldTypes) {
            const option = element('option', text);
            option.value = value;
            type.appendChild(option);
        }
        type.value = definition?.type || 'string';
        type.disabled = !!definition;
        labeled(container, '字段类型（保存后不可更改）', type);
        const enabled = labeled(container, '允许新录入（停用后保留已保存的值）', input(`${prefix}-enabled`, 'checkbox', definition?.enabled ?? true));
        const editable = labeled(container, '允许手工编辑', input(`${prefix}-editable`, 'checkbox', definition?.editable ?? true));
        const sources = element('fieldset');
        sources.appendChild(element('legend', '允许生成建议的用途'));
        const extractable = extractionKinds.map(([kind, text]) => ({
            kind,
            control: labeled(sources, text, input(`${prefix}-${kind}`, 'checkbox', definition?.extractable?.includes(kind) ?? false)),
        }));
        container.appendChild(sources);
        container.appendChild(element('p', '仅项目内保存，不导出到 ComicInfo。'));
        list.appendChild(container);
        rows.push({ container, name, label, type, enabled, editable, extractable, definition });
        updateControls();
    }
    function acceptSnapshot(payload) {
        const nextLimits = payload?.limits;
        if (!payload || typeof payload.definitions_version !== 'string' || !payload.definitions_version || !Array.isArray(payload.definitions)
            || !Number.isSafeInteger(nextLimits?.custom_fields) || nextLimits.custom_fields < 1
            || !Number.isSafeInteger(nextLimits.string_bytes) || !Number.isSafeInteger(nextLimits.list_items) || !Number.isSafeInteger(nextLimits.list_item_bytes)
            || payload.definitions.length > nextLimits.custom_fields) throw new Error('invalid snapshot');
        for (const definition of payload.definitions) {
            if (!/^custom\.user\.[a-z][a-z0-9_]{0,31}$/.test(definition.key) || !fieldTypes.some(([type]) => type === definition.type)
                || typeof definition.label !== 'string' || !Array.isArray(definition.extractable)) throw new Error('invalid definition');
        }
        version = payload.definitions_version;
        limits = nextLimits;
        rows = [];
        list.replaceChildren();
        // Rows are populated while idle, then the request's finally restores controls.
        const wasBusy = busy;
        busy = false;
        for (const definition of payload.definitions) appendRow(definition);
        busy = wasBusy;
        updateControls();
    }
    function buildPayload() {
        const seen = new Set();
        const definitions = rows.map(row => {
            const key = row.definition?.key || `custom.user.${row.name.value.trim()}`;
            if (!/^custom\.user\.[a-z][a-z0-9_]{0,31}$/.test(key) || seen.has(key)) throw new Error('字段标识须以小写字母开头，仅含小写字母、数字、下划线，且不能重复。');
            seen.add(key);
            const label = row.label.value.trim();
            if (!/\p{Script=Han}/u.test(label) || new TextEncoder().encode(label).length > 128) throw new Error('请填写含中文的字段名称，长度不超过 128 字节。');
            const type = row.definition?.type || row.type.value;
            if (!fieldTypes.some(([value]) => type === value)) throw new Error('请选择受支持的字段类型。');
            const definition = row.definition ? { ...row.definition } : { key, type, export_status: 'internal_only' };
            if (!row.definition) {
                if (type === 'string') definition.max_bytes = limits.string_bytes;
                if (type === 'string[]') { definition.max_items = limits.list_items; definition.item_max_bytes = limits.list_item_bytes; }
                if (type === 'integer') { definition.minimum = -2147483648; definition.maximum = 2147483647; }
            }
            return { ...definition, label, enabled: row.enabled.checked, editable: row.editable.checked, extractable: row.extractable.filter(item => item.control.checked).map(item => item.kind) };
        });
        return { expected_definitions_version: version, definitions };
    }
    async function request(action) {
        if (!root || busy) return;
        let input;
        if (action === 'save') {
            try { input = buildPayload(); } catch (error) { feedback(error.message); return; }
        }
        const currentRoot = root;
        const currentGeneration = generation;
        const current = new AbortController();
        controller = current;
        busy = true;
        updateControls();
        feedback(action === 'save' ? '正在保存自定义字段…' : '正在载入自定义字段…');
        try {
            const { response, payload } = action === 'save'
                ? await api.putJson(endpoint, input, { signal: current.signal })
                : await api.getJson(endpoint, { signal: current.signal, cache: 'no-store' });
            if (current.signal.aborted || root !== currentRoot || generation !== currentGeneration) return;
            if (!response.ok || !payload) {
                feedback(response.status === 409 ? '字段设置已被其他页面更新。本页编辑已保留，请核对后重新载入。' : `字段设置${action === 'save' ? '保存' : '载入'}失败，请重试。`);
                return;
            }
            acceptSnapshot(payload);
            if (action === 'save' && doc.defaultView?.CustomEvent) {
                doc.dispatchEvent(new doc.defaultView.CustomEvent('metadata-definitions-changed', { detail: { definitionsVersion: payload.definitions_version } }));
            }
            feedback(action === 'save' ? '自定义字段已保存。新建任务可使用更新后的字段；已有任务保留原有定义。' : '可新增字段、调整名称和用途，或停用字段。');
        } catch {
            if (!current.signal.aborted && root === currentRoot && generation === currentGeneration) feedback('字段设置暂时不可用，请重试。');
        } finally {
            if (controller === current) {
                controller = null;
                busy = false;
                updateControls();
            }
        }
    }
    function mount() {
        const nextRoot = doc.querySelector('[data-settings-slot="metadata-fields"]') || doc.querySelector('[data-module-slot="metadata-fields"]');
        if (!nextRoot || nextRoot === root) return;
        unmount();
        root = nextRoot;
        root.replaceChildren();
        root.appendChild(element('h3', '自定义元数据字段'));
        root.appendChild(element('p', '自定义字段可用于录入、提取建议和历史回填。已保存字段的标识和类型保持不变。'));
        list = element('div');
        root.appendChild(list);
        add = button('新增字段', 'metadata-fields-add', () => appendRow());
        save = button('保存字段设置', 'metadata-fields-save', () => request('save'));
        reload = button('重新载入已保存设置', 'metadata-fields-reload', () => request('load'));
        root.appendChild(add); root.appendChild(save); root.appendChild(reload);
        status = element('p', '', 'metadata-fields-status');
        status.setAttribute('role', 'status');
        status.setAttribute('aria-live', 'polite');
        root.appendChild(status);
        request('load');
    }
    function unmount() {
        generation += 1;
        controller?.abort();
        controller = null;
        busy = false;
        for (const remove of listeners) remove();
        listeners = [];
        root?.replaceChildren();
        root = null;
        status = null;
        version = '';
        limits = null;
        rows = [];
    }
    return { mount, unmount };
}
