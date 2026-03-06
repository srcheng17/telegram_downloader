(function (global) {
    'use strict';

    const CANCELABLE_STATUS = new Set(['QUEUED', 'RUNNING']);

    const state = {
        page: 1,
        perPage: 20,
        status: '',
        q: '',
    };

    function escapeHTML(value) {
        return String(value || '')
            .replaceAll('&', '&amp;')
            .replaceAll('<', '&lt;')
            .replaceAll('>', '&gt;')
            .replaceAll('"', '&quot;')
            .replaceAll("'", '&#39;');
    }

    function setVisible(node, visible) {
        if (!node) {
            return;
        }
        if (visible) {
            node.classList.remove('is-hidden');
            return;
        }
        node.classList.add('is-hidden');
    }

    function setFeedback(message, kind) {
        const feedback = document.getElementById('v2-tasks-feedback');
        if (!feedback) {
            return;
        }
        const text = String(message || '').trim();
        feedback.textContent = text;
        feedback.classList.remove('feedback-success', 'feedback-error', 'feedback-info', 'is-visible');
        if (!text) {
            return;
        }
        const resolvedKind = String(kind || 'info').trim() || 'info';
        feedback.classList.add(`feedback-${resolvedKind}`, 'is-visible');
    }

    function parsePositiveInt(rawValue, fallback) {
        const value = Number.parseInt(String(rawValue || ''), 10);
        if (!Number.isFinite(value) || value <= 0) {
            return fallback;
        }
        return value;
    }

    function readFiltersFromQuery() {
        const query = new URLSearchParams(global.location.search);
        state.page = parsePositiveInt(query.get('page'), state.page);
        state.perPage = parsePositiveInt(query.get('per_page'), state.perPage);
        state.status = String(query.get('status') || '').trim().toUpperCase();
        state.q = String(query.get('q') || '').trim();
    }

    function syncFormFromState() {
        const status = document.getElementById('v2-filter-status');
        const q = document.getElementById('v2-filter-query');
        const page = document.getElementById('v2-filter-page');
        const perPage = document.getElementById('v2-filter-per-page');
        if (status) {
            status.value = state.status;
        }
        if (q) {
            q.value = state.q;
        }
        if (page) {
            page.value = String(state.page);
        }
        if (perPage) {
            perPage.value = String(state.perPage);
        }
    }

    function readStateFromForm() {
        const status = document.getElementById('v2-filter-status');
        const q = document.getElementById('v2-filter-query');
        const page = document.getElementById('v2-filter-page');
        const perPage = document.getElementById('v2-filter-per-page');

        state.status = String(status ? status.value : '').trim().toUpperCase();
        state.q = String(q ? q.value : '').trim();
        state.page = parsePositiveInt(page ? page.value : state.page, 1);
        state.perPage = parsePositiveInt(perPage ? perPage.value : state.perPage, 20);
    }

    function syncURLFromState() {
        const query = new URLSearchParams();
        if (state.status) {
            query.set('status', state.status);
        }
        if (state.q) {
            query.set('q', state.q);
        }
        if (state.page > 1) {
            query.set('page', String(state.page));
        }
        if (state.perPage !== 20) {
            query.set('per_page', String(state.perPage));
        }
        const base = global.location.pathname;
        const nextURL = query.toString() ? `${base}?${query.toString()}` : base;
        global.history.replaceState(null, '', nextURL);
    }

    function updatePagination(result) {
        const totalNode = document.getElementById('v2-total-count');
        const pageNode = document.getElementById('v2-current-page');
        const totalPagesNode = document.getElementById('v2-total-pages');
        const prevButton = document.getElementById('v2-page-prev');
        const nextButton = document.getElementById('v2-page-next');

        const currentPage = parsePositiveInt(result && result.page, 1);
        const totalPages = Math.max(parsePositiveInt(result && result.total_pages, 1), 1);
        const total = Number.parseInt(String((result && result.total) || 0), 10) || 0;

        if (totalNode) {
            totalNode.textContent = String(total);
        }
        if (pageNode) {
            pageNode.textContent = String(currentPage);
        }
        if (totalPagesNode) {
            totalPagesNode.textContent = String(totalPages);
        }
        if (prevButton) {
            prevButton.disabled = currentPage <= 1;
        }
        if (nextButton) {
            nextButton.disabled = currentPage >= totalPages;
        }
    }

    function renderRows(tasks) {
        const body = document.getElementById('v2-tasks-table-body');
        if (!body) {
            return;
        }
        if (!Array.isArray(tasks) || tasks.length === 0) {
            body.innerHTML = '<tr><td colspan="6" class="muted">暂无任务。</td></tr>';
            return;
        }

        body.innerHTML = tasks
            .map((task) => {
                const id = escapeHTML(task.id);
                const canonicalURL = escapeHTML(task.canonical_url || task.url || '');
                const status = escapeHTML(task.status || '');
                const updatedAt = escapeHTML(task.updated_at || '');
                const canCancel = CANCELABLE_STATUS.has(String(task.status || '').toUpperCase());
                const canDownload = String(task.status || '').toUpperCase() === 'SUCCESS';

                return `
                    <tr data-task-id="${id}">
                        <td><code>${id}</code></td>
                        <td title="${canonicalURL}">${canonicalURL}</td>
                        <td>${status}</td>
                        <td>${updatedAt}</td>
                        <td>${escapeHTML(task.error || '')}</td>
                        <td class="log-action-cell">
                            <button type="button" class="btn-secondary v2-cancel-btn ${canCancel ? '' : 'is-hidden'}">取消</button>
                            <button type="button" class="v2-download-btn ${canDownload ? '' : 'is-hidden'}">下载</button>
                        </td>
                    </tr>
                `;
            })
            .join('');
    }

    async function loadTasks() {
        const loading = document.getElementById('v2-tasks-loading');
        setVisible(loading, true);
        setFeedback('', 'info');
        syncURLFromState();

        try {
            const result = await global.TelegraphV2API.listTasks({
                status: state.status,
                q: state.q,
                page: state.page,
                per_page: state.perPage,
            });
            renderRows(result && result.tasks);
            updatePagination(result || {});
        } catch (error) {
            renderRows([]);
            updatePagination({});
            const message = error && error.message ? error.message : '加载任务失败';
            setFeedback(message, 'error');
        } finally {
            setVisible(loading, false);
        }
    }

    async function handleTableClick(event) {
        const target = event.target;
        if (!(target instanceof HTMLElement)) {
            return;
        }

        const row = target.closest('tr[data-task-id]');
        if (!row) {
            return;
        }
        const taskID = String(row.getAttribute('data-task-id') || '').trim();
        if (!taskID) {
            return;
        }

        if (target.classList.contains('v2-cancel-btn')) {
            target.setAttribute('disabled', 'disabled');
            try {
                await global.TelegraphV2API.cancelTask(taskID);
                setFeedback(`任务 ${taskID} 已提交取消。`, 'success');
                await loadTasks();
            } catch (error) {
                const message = error && error.message ? error.message : '取消任务失败';
                setFeedback(message, 'error');
            } finally {
                target.removeAttribute('disabled');
            }
        }

        if (target.classList.contains('v2-download-btn')) {
            try {
                await global.TelegraphV2API.downloadArtifact(taskID);
            } catch (error) {
                const message = error && error.message ? error.message : '下载失败';
                setFeedback(message, 'error');
            }
        }
    }

    async function handleFilterSubmit(event) {
        event.preventDefault();
        readStateFromForm();
        state.page = 1;
        await loadTasks();
    }

    async function handlePrevPage() {
        if (state.page <= 1) {
            return;
        }
        state.page -= 1;
        syncFormFromState();
        await loadTasks();
    }

    async function handleNextPage() {
        state.page += 1;
        syncFormFromState();
        await loadTasks();
    }

    function bindEvents() {
        const form = document.getElementById('v2-tasks-filter-form');
        const body = document.getElementById('v2-tasks-table-body');
        const prev = document.getElementById('v2-page-prev');
        const next = document.getElementById('v2-page-next');
        if (form) {
            form.addEventListener('submit', handleFilterSubmit);
        }
        if (body) {
            body.addEventListener('click', handleTableClick);
        }
        if (prev) {
            prev.addEventListener('click', function () {
                void handlePrevPage();
            });
        }
        if (next) {
            next.addEventListener('click', function () {
                void handleNextPage();
            });
        }
    }

    function initTasksPage() {
        const page = document.getElementById('v2-tasks-page');
        if (!page || !global.TelegraphV2API) {
            return;
        }
        readFiltersFromQuery();
        syncFormFromState();
        bindEvents();
        void loadTasks();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initTasksPage);
    } else {
        initTasksPage();
    }
})(window);
