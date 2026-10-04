import { createKomgaApi } from './api.js';
import { createKomgaEditor } from './edit.js';
import { messageForFailure } from './messages.js';

const pageSize = 24;

const readOnlyReasons = Object.freeze({
    unsupported_format: '此格式暂不支持写回 ComicInfo；可以查看作品信息。',
    unmapped_library: '此书库尚未配置共享写入挂载；可以查看作品信息。',
    library_unavailable: '书库当前不可用，或共享挂载与 Komga 书库不一致。',
    comicinfo_import_disabled: '书库尚未启用 ComicInfo 导入，暂不能写回。',
    file_unavailable: '无法安全打开此文件，暂不能写回。',
});

class DisplayError extends Error {}
function displayError(message) { return new DisplayError(message); }
function safeErrorMessage(error, fallback) { return error instanceof DisplayError ? error.message : fallback; }

function validBook(book) {
    return book && typeof book === 'object' && typeof book.id === 'string' && book.id.length > 0 &&
        typeof book.library_id === 'string' && typeof book.editable === 'boolean';
}

function makeText(doc, tag, value, className = '') {
    const node = doc.createElement(tag);
    node.textContent = typeof value === 'string' ? value : '';
    if (className) node.className = className;
    return node;
}

// onBookDetail is the integration point for the file-backed metadata editor.
// It may return { hasUnsavedChanges(), canLeave(confirmDiscard), dispose() }.
// List and detail refreshes never dispose a dirty editor implicitly.
export function createKomgaModule(win = window, doc = document, api = createKomgaApi(win), { onBookDetail } = {}) {
    const detailHook = onBookDetail === undefined ? ({ book, container, signal }) => createKomgaEditor({ win, doc, api, book, container, signal }) : onBookDetail;
    let root;
    let elements;
    let mounted = false;
    let generation = 0;
    let listVersion = 0;
    let detailVersion = 0;
    let libraryController;
    let listController;
    let detailController;
    let searchTimer;
    let editor;
    let libraries = [];
    let books = [];
    let libraryID = '';
    let appliedQuery = '';
    let page = 0;
    let loadedPage = 0;
    let loadedKey = '';
    let totalPages = 0;
    let totalElements = 0;
    let selectedID = '';

    function feedback(text, state = 'info') {
        if (!elements) return;
        elements.feedback.textContent = text;
        elements.feedback.dataset.state = state;
    }
    function active(run) { return mounted && generation === run && doc.querySelector('#komga-workspace') === root; }
    function dirty() { return Boolean(editor?.hasUnsavedChanges?.()); }
    function canLeave(confirmDiscard) {
        if (!dirty()) return true;
        if (typeof editor?.canLeave === 'function') return Boolean(editor.canLeave(confirmDiscard));
        return Boolean(confirmDiscard?.());
    }
    function disposeEditor() { editor?.dispose?.(); editor = null; }

    function renderLibraries() {
        const options = libraries.map(library => {
            const option = doc.createElement('option');
            option.value = library.id;
            option.textContent = library.unavailable ? `${library.name}（不可用）` : library.name;
            return option;
        });
        elements.library.replaceChildren(...options);
        elements.library.value = libraryID;
        elements.library.disabled = libraries.length === 0;
    }

    function renderList() {
        const rows = books.map(book => {
            const item = doc.createElement('li');
            const button = doc.createElement('button');
            button.type = 'button';
            button.className = 'komga-book-row';
            button.dataset.bookId = book.id;
            button.setAttribute('aria-pressed', String(book.id === selectedID));
            button.appendChild(makeText(doc, 'strong', book.title || book.file_name || '未命名作品'));
            const subtitle = [book.series_title, book.file_name, book.file_type].filter(value => typeof value === 'string' && value.trim()).join(' · ');
            if (subtitle) button.appendChild(makeText(doc, 'span', subtitle, 'komga-book-meta'));
            button.appendChild(makeText(doc, 'span', book.editable ? '可编辑' : '只读', book.editable ? 'komga-book-state is-editable' : 'komga-book-state is-read-only'));
            button.addEventListener('click', () => selectBook(book.id));
            item.appendChild(button);
            return item;
        });
        elements.list.replaceChildren(...rows);
        elements.prev.disabled = page <= 0;
        elements.next.disabled = totalPages === 0 || page >= totalPages - 1;
        if (!books.length) feedback('此条件下没有作品。', 'empty');
        else feedback(`共 ${totalElements} 本 · 第 ${page + 1} / ${Math.max(1, totalPages)} 页`, 'ready');
    }

    function renderDetail(book, signal) {
        const container = elements.detail;
        const heading = makeText(doc, 'h2', book.title || book.file_name || '未命名作品');
        const context = makeText(doc, 'p', [book.series_title, book.file_name].filter(value => typeof value === 'string' && value).join(' · '), 'komga-detail-context');
        const canOpenEditor = book.editable && typeof detailHook === 'function';
        const stateText = !book.editable ? (readOnlyReasons[book.read_only_reason] || '此作品目前只可查看。') : canOpenEditor ? '此格式支持 ComicInfo 编辑；最终资格以文件检查结果为准。' : '此作品符合写回条件，但元数据编辑器暂不可用。';
        const state = makeText(doc, 'p', stateText, canOpenEditor ? 'komga-detail-state is-editable' : 'komga-detail-state is-read-only');
        const source = makeText(doc, 'p', '下方 Komga 数据仅供核对；保存目标是 CBZ 内的 ComicInfo。', 'komga-detail-note');
        container.replaceChildren(heading, context, state, source);
        // A separate editor may mount beneath this summary. Its owner must
        // preserve its own draft and handle preview/save/sync independently.
        if (book.editable && typeof detailHook === 'function') {
            try {
                editor = detailHook({ book, container, signal }) || null;
            } catch {
                editor = null;
                container.replaceChildren(heading, context, state, source, makeText(doc, 'p', '元数据编辑器暂时无法打开，请稍后重新选择此作品。', 'komga-detail-error'));
            }
        }
    }

    async function selectBook(id, { refresh = false } = {}) {
        if (!mounted || !id) return;
        if (id === selectedID && !refresh) return;
        if (dirty()) {
            if (id === selectedID) {
                feedback('已保留未保存的元数据草稿。', 'info');
                return;
            }
            if (!canLeave(() => win.confirm?.('当前作品有未保存的元数据修改，确定放弃吗？'))) {
                feedback('已保留未保存的元数据草稿。', 'info');
                return;
            }
        }
        const run = generation;
        const ticket = ++detailVersion;
        detailController?.abort();
        const controller = new AbortController();
        detailController = controller;
        let delivered = false;
        feedback('正在读取作品详情…', 'loading');
        try {
            const result = await api.book(id, { signal: controller.signal });
            if (!active(run) || ticket !== detailVersion || controller.signal.aborted) return;
            if (!result?.response?.ok) throw displayError(messageForFailure(result));
            const book = result.payload?.book || result.payload;
            if (!validBook(book) || book.id !== id || book.library_id !== libraryID) throw displayError('作品详情与当前书库不一致，请刷新列表。');
            if (dirty()) {
                feedback('详情已更新，但未保存的元数据草稿仍保持原样。', 'info');
                return;
            }
            disposeEditor();
            selectedID = id;
            renderDetail(book, controller.signal);
            delivered = true;
            renderList();
        } catch (error) {
            if (active(run) && ticket === detailVersion && !controller.signal.aborted) feedback(safeErrorMessage(error, '作品详情读取失败，请重试。'), 'error');
        } finally {
            if (!delivered && detailController === controller) detailController = null;
        }
    }

    async function loadBooks() {
        if (!mounted || !libraryID) return;
        const run = generation;
        const ticket = ++listVersion;
        const requestKey = `${libraryID}\u0000${appliedQuery}`;
        const requestPage = page;
        listController?.abort();
        const controller = new AbortController();
        listController = controller;
        elements.prev.disabled = true;
        elements.next.disabled = true;
        feedback('正在读取作品列表…', 'loading');
        try {
            const result = await api.books({ libraryID, query: appliedQuery, page, size: pageSize }, { signal: controller.signal });
            if (!active(run) || ticket !== listVersion || controller.signal.aborted) return;
            if (!result?.response?.ok) throw displayError(messageForFailure(result));
            const payload = result.payload;
            if (!payload || !Array.isArray(payload.books) || payload.books.length > pageSize || !Number.isInteger(payload.page) || payload.page !== page || !Number.isInteger(payload.size) || payload.size < 1 || payload.size > pageSize || !Number.isInteger(payload.total_pages) || payload.total_pages < 0 || !Number.isSafeInteger(payload.total_elements) || payload.total_elements < 0) {
                throw displayError('Komga 作品列表格式无效，请稍后重试。');
            }
            if (payload.books.some(book => !validBook(book) || book.library_id !== libraryID)) throw displayError('Komga 作品列表与当前书库不一致。');
            books = payload.books;
            totalPages = payload.total_pages;
            totalElements = payload.total_elements;
            loadedKey = requestKey;
            loadedPage = requestPage;
            renderList();
        } catch (error) {
            if (active(run) && ticket === listVersion && !controller.signal.aborted) {
                if (loadedKey === requestKey) {
                    page = loadedPage;
                    renderList();
                } else {
                    books = [];
                    totalPages = 0;
                    totalElements = 0;
                    elements.list.replaceChildren();
                }
                feedback(safeErrorMessage(error, '作品列表读取失败，请重试。'), 'error');
            }
        } finally {
            if (listController === controller) listController = null;
        }
    }

    function applySearch() {
        if (!mounted) return;
        if (searchTimer) { win.clearTimeout(searchTimer); searchTimer = null; }
        const query = elements.search.value.trim();
        if (query === appliedQuery) return;
        appliedQuery = query;
        page = 0;
        loadBooks();
    }
    function onSearchInput() {
        if (searchTimer) win.clearTimeout(searchTimer);
        searchTimer = win.setTimeout(() => { searchTimer = null; applySearch(); }, 300);
    }
    function onSearchKeydown(event) {
        if (event.key !== 'Enter') return;
        event.preventDefault();
        applySearch();
    }
    function onSearchSubmit(event) {
        event.preventDefault();
        applySearch();
    }
    function onLibraryChange() {
        const id = elements.library.value;
        if (!libraries.some(library => library.id === id)) return;
        if (id === libraryID) return;
        if (!canLeave(() => win.confirm?.('当前作品有未保存的元数据修改，确定切换书库吗？'))) {
            elements.library.value = libraryID;
            return;
        }
        disposeEditor();
        detailController?.abort();
        detailVersion++;
        elements.detail.replaceChildren();
        selectedID = '';
        libraryID = id;
        page = 0;
        loadBooks();
    }
    function previousPage() { if (page > 0) { page -= 1; loadBooks(); } }
    function nextPage() { if (page + 1 < totalPages) { page += 1; loadBooks(); } }

    async function loadLibraries() {
        const run = generation;
        libraryController?.abort();
        const controller = new AbortController();
        libraryController = controller;
        feedback('正在读取 Komga 书库…', 'loading');
        try {
            const result = await api.libraries({ signal: controller.signal });
            if (!active(run) || controller.signal.aborted) return;
            if (!result?.response?.ok) throw displayError(messageForFailure(result));
            if (!Array.isArray(result.payload?.libraries)) throw displayError('Komga 书库列表格式无效。');
            const incoming = result.payload.libraries.filter(library => library && typeof library.id === 'string' && library.id && typeof library.name === 'string');
            if (incoming.length !== result.payload.libraries.length) throw displayError('Komga 书库列表格式无效。');
            if (libraryID && !incoming.some(library => library.id === libraryID)) {
                if (dirty()) {
                    feedback('当前书库已不可用；已保留未保存的元数据草稿。', 'error');
                    return;
                }
                detailController?.abort();
                detailVersion++;
                disposeEditor();
                elements.detail.replaceChildren();
                selectedID = '';
                page = 0;
            }
            libraries = incoming;
            libraryID = libraries.some(library => library.id === libraryID) ? libraryID : (libraries[0]?.id || '');
            renderLibraries();
            if (!libraries.length) {
                feedback('没有可用书库，请检查 Komga 连接和书库权限。', 'empty');
                return;
            }
            await loadBooks();
        } catch (error) {
            if (active(run) && !controller.signal.aborted) feedback(safeErrorMessage(error, '书库读取失败，请稍后重试。'), 'error');
        } finally {
            if (libraryController === controller) libraryController = null;
        }
    }

    function mount() {
        const current = doc.querySelector('#komga-workspace');
        if (!current) return;
        if (mounted && root === current) return;
        if (mounted) unmount();
        const found = {
            library: current.querySelector('#komga-library'), search: current.querySelector('#komga-search'),
            list: current.querySelector('#komga-list'), detail: current.querySelector('#komga-detail'),
            feedback: current.querySelector('#komga-feedback'), prev: current.querySelector('#komga-prev'),
            next: current.querySelector('#komga-next'),
        };
        if (Object.values(found).some(value => !value)) return;
        root = current;
        elements = found;
        mounted = true;
        generation++;
        libraries = [];
        books = [];
        libraryID = '';
        appliedQuery = '';
        page = 0;
        loadedPage = 0;
        loadedKey = '';
        totalPages = 0;
        totalElements = 0;
        selectedID = '';
        elements.library.addEventListener('change', onLibraryChange);
        elements.search.addEventListener('input', onSearchInput);
        elements.search.addEventListener('keydown', onSearchKeydown);
        elements.search.form?.addEventListener('submit', onSearchSubmit);
        elements.prev.addEventListener('click', previousPage);
        elements.next.addEventListener('click', nextPage);
        elements.prev.disabled = true;
        elements.next.disabled = true;
        loadLibraries();
    }

    function unmount() {
        if (!mounted) return;
        mounted = false;
        generation++;
        listVersion++;
        detailVersion++;
        libraryController?.abort(); libraryController = null;
        listController?.abort(); listController = null;
        detailController?.abort(); detailController = null;
        if (searchTimer) win.clearTimeout(searchTimer);
        searchTimer = null;
        elements.library.removeEventListener('change', onLibraryChange);
        elements.search.removeEventListener('input', onSearchInput);
        elements.search.removeEventListener('keydown', onSearchKeydown);
        elements.search.form?.removeEventListener('submit', onSearchSubmit);
        elements.prev.removeEventListener('click', previousPage);
        elements.next.removeEventListener('click', nextPage);
        disposeEditor();
        elements.library.replaceChildren();
        elements.search.value = '';
        elements.list.replaceChildren();
        elements.detail.replaceChildren();
        elements.feedback.textContent = '';
        root = null;
        elements = null;
        libraries = [];
        books = [];
        libraryID = '';
        selectedID = '';
        loadedKey = '';
    }

    return {
        mount, unmount,
        refresh() { if (mounted) { loadLibraries(); if (selectedID && !dirty()) selectBook(selectedID, { refresh: true }); } },
        hasUnsavedChanges: dirty,
        canLeave,
    };
}
