(function (global) {
    'use strict';

    function setFeedback(node, message, kind) {
        if (!node) {
            return;
        }
        const text = String(message || '').trim();
        node.textContent = text;
        node.classList.remove('feedback-success', 'feedback-error', 'feedback-info', 'is-visible');
        if (!text) {
            return;
        }
        const resolvedKind = String(kind || 'info').trim() || 'info';
        node.classList.add(`feedback-${resolvedKind}`, 'is-visible');
    }

    function setResultVisible(resultNode, visible) {
        if (!resultNode) {
            return;
        }
        if (visible) {
            resultNode.classList.remove('is-hidden');
            resultNode.setAttribute('aria-hidden', 'false');
            return;
        }
        resultNode.classList.add('is-hidden');
        resultNode.setAttribute('aria-hidden', 'true');
    }

    async function handleSubmit(event) {
        event.preventDefault();

        const page = document.getElementById('v2-dashboard-page');
        const form = document.getElementById('v2-create-task-form');
        const urlInput = document.getElementById('v2-url');
        const submitButton = document.getElementById('v2-create-submit');
        const feedback = document.getElementById('v2-dashboard-feedback');
        const result = document.getElementById('v2-create-result');
        const resultText = document.getElementById('v2-create-result-text');
        const openTasks = document.getElementById('v2-open-tasks-link');

        if (!form || !urlInput || !submitButton || !feedback || !result || !resultText || !openTasks) {
            return;
        }

        const tasksPageURL = (page && page.dataset && page.dataset.tasksPageUrl) || '/v2/tasks-ui';
        openTasks.setAttribute('href', tasksPageURL);

        const url = String(urlInput.value || '').trim();
        if (!url) {
            setResultVisible(result, false);
            setFeedback(feedback, '请输入 Telegraph 链接。', 'error');
            return;
        }

        submitButton.disabled = true;
        setResultVisible(result, false);
        setFeedback(feedback, '正在创建任务…', 'info');

        try {
            const response = await global.TelegraphV2API.createTask({ url });
            const taskID = String((response && response.task_id) || '').trim();
            const status = String((response && response.status) || 'QUEUED').trim();
            resultText.textContent = taskID
                ? `任务已创建：${taskID}（${status}）`
                : `任务已创建（${status}）`;
            setResultVisible(result, true);
            setFeedback(feedback, '任务创建成功。', 'success');
            form.reset();
        } catch (error) {
            const message = error && error.message ? error.message : '创建任务失败';
            setResultVisible(result, false);
            setFeedback(feedback, message, 'error');
        } finally {
            submitButton.disabled = false;
        }
    }

    function initDashboardPage() {
        const page = document.getElementById('v2-dashboard-page');
        const form = document.getElementById('v2-create-task-form');
        if (!page || !form || !global.TelegraphV2API) {
            return;
        }
        form.addEventListener('submit', handleSubmit);
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initDashboardPage);
    } else {
        initDashboardPage();
    }
})(window);
