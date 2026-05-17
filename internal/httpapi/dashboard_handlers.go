package httpapi

import (
	"fmt"
	"net/http"
)

const v2DashboardPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>V2 任务创建</title>
</head>
<body>
  <h1>V2 任务创建</h1>
  <p><a href="/v2/tasks-ui">查看任务列表</a></p>
  <form id="v2-create-task-form">
    <label for="v2-url">Telegraph URL</label>
    <input id="v2-url" name="url" type="url" required placeholder="https://telegra.ph/...">
    <button type="submit">创建任务</button>
  </form>
  <pre id="v2-feedback"></pre>
  <script>
    (function () {
      const form = document.getElementById('v2-create-task-form');
      const feedback = document.getElementById('v2-feedback');
      form.addEventListener('submit', async function (event) {
        event.preventDefault();
        const url = String(document.getElementById('v2-url').value || '').trim();
        if (!url) {
          feedback.textContent = '请输入 URL';
          return;
        }
        feedback.textContent = '提交中...';
        try {
          const response = await fetch('/v2/tasks', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
            body: JSON.stringify({ url: url }),
          });
          const payload = await response.json().catch(() => ({}));
          if (!response.ok) {
            feedback.textContent = payload.error || ('请求失败(' + response.status + ')');
            return;
          }
          feedback.textContent = '任务已创建: ' + (payload.task_id || '-') + ' (' + (payload.status || '-') + ')';
          form.reset();
        } catch (error) {
          feedback.textContent = '网络错误';
        }
      });
    })();
  </script>
</body>
</html>
`

const v2TasksPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>V2 任务列表</title>
</head>
<body>
  <h1>V2 任务列表</h1>
  <p><a href="/v2">创建任务</a></p>
  <form id="v2-filter-form">
    <label for="status">状态</label>
    <select id="status" name="status">
      <option value="">全部</option>
      <option value="QUEUED">QUEUED</option>
      <option value="RUNNING">RUNNING</option>
      <option value="SUCCESS">SUCCESS</option>
      <option value="FAILED">FAILED</option>
      <option value="CANCELED">CANCELED</option>
    </select>
    <label for="q">关键词</label>
    <input id="q" name="q" type="text">
    <label for="page">页码</label>
    <input id="page" name="page" type="number" min="1" value="1">
    <label for="per_page">每页</label>
    <input id="per_page" name="per_page" type="number" min="1" max="100" value="20">
    <button type="submit">查询</button>
  </form>
  <pre id="v2-tasks-feedback"></pre>
  <table border="1" cellpadding="6">
    <thead>
      <tr><th>ID</th><th>URL</th><th>状态</th><th>错误</th><th>操作</th></tr>
    </thead>
    <tbody id="v2-tasks-table-body"></tbody>
  </table>
  <script>
    (function () {
      const feedback = document.getElementById('v2-tasks-feedback');
      const tbody = document.getElementById('v2-tasks-table-body');
      const form = document.getElementById('v2-filter-form');

      function escapeHTML(input) {
        return String(input || '')
          .replaceAll('&', '&amp;')
          .replaceAll('<', '&lt;')
          .replaceAll('>', '&gt;')
          .replaceAll('"', '&quot;')
          .replaceAll("'", '&#39;');
      }

      function buildQuery() {
        const params = new URLSearchParams();
        const status = String(document.getElementById('status').value || '').trim();
        const q = String(document.getElementById('q').value || '').trim();
        const page = String(document.getElementById('page').value || '').trim();
        const perPage = String(document.getElementById('per_page').value || '').trim();
        if (status) params.set('status', status);
        if (q) params.set('q', q);
        if (page) params.set('page', page);
        if (perPage) params.set('per_page', perPage);
        return params.toString();
      }

      async function loadTasks() {
        feedback.textContent = '加载中...';
        const query = buildQuery();
        const response = await fetch('/v2/tasks' + (query ? ('?' + query) : ''), {
          headers: { 'Accept': 'application/json' },
        });
        const payload = await response.json().catch(() => ({}));
        if (!response.ok) {
          feedback.textContent = payload.error || ('加载失败(' + response.status + ')');
          tbody.innerHTML = '';
          return;
        }
        const tasks = Array.isArray(payload.tasks) ? payload.tasks : [];
        if (!tasks.length) {
          tbody.innerHTML = '<tr><td colspan="5">暂无任务</td></tr>';
        } else {
          tbody.innerHTML = tasks.map((task) => {
            const status = String(task.status || '').toUpperCase();
            const canCancel = status === 'QUEUED' || status === 'RUNNING';
            const canDownload = status === 'SUCCESS';
            const buttons = []
            if (canCancel) {
              buttons.push('<button data-action="cancel" data-task-id="' + escapeHTML(task.id) + '">取消</button>');
            }
            if (canDownload) {
              buttons.push('<button data-action="download" data-task-id="' + escapeHTML(task.id) + '">下载</button>');
            }
            return '<tr>' +
              '<td>' + escapeHTML(task.id) + '</td>' +
              '<td>' + escapeHTML(task.canonical_url || task.url || '') + '</td>' +
              '<td>' + escapeHTML(task.status || '') + '</td>' +
              '<td>' + escapeHTML(task.error || '') + '</td>' +
              '<td>' + buttons.join(' ') + '</td>' +
              '</tr>';
          }).join('');
        }
        feedback.textContent = '共 ' + (payload.total || 0) + ' 条';
      }

      tbody.addEventListener('click', async function (event) {
        const target = event.target;
        if (!(target instanceof HTMLElement)) {
          return;
        }
        const action = String(target.getAttribute('data-action') || '');
        const taskID = String(target.getAttribute('data-task-id') || '');
        if (!action || !taskID) {
          return;
        }
        if (action === 'download') {
          window.location.href = '/v2/tasks/' + encodeURIComponent(taskID) + '/artifact';
          return;
        }
        if (action === 'cancel') {
          target.setAttribute('disabled', 'disabled');
          try {
            const response = await fetch('/v2/tasks/' + encodeURIComponent(taskID) + '/cancel', {
              method: 'POST',
              headers: { 'Accept': 'application/json' },
            });
            const payload = await response.json().catch(() => ({}));
            if (!response.ok) {
              feedback.textContent = payload.error || ('取消失败(' + response.status + ')');
              return;
            }
            feedback.textContent = '任务 ' + taskID + ' 已提交取消';
            await loadTasks();
          } finally {
            target.removeAttribute('disabled');
          }
        }
      });

      form.addEventListener('submit', function (event) {
        event.preventDefault();
        void loadTasks();
      });
      void loadTasks();
    })();
  </script>
</body>
</html>
`

func (a *API) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/v2", http.StatusFound)
}

func (a *API) handleLogsPageRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/v2/tasks-ui", http.StatusFound)
}

func (a *API) handleV2DashboardPage(w http.ResponseWriter, _ *http.Request) {
	writeHTML(w, http.StatusOK, v2DashboardPageHTML)
}

func (a *API) handleV2TasksPage(w http.ResponseWriter, _ *http.Request) {
	writeHTML(w, http.StatusOK, v2TasksPageHTML)
}

func (a *API) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "go-backend"})
}

func (a *API) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if a.readyzChecker == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ready": true, "service": "go-backend"})
		return
	}

	ready, err := a.readyzChecker(r.Context())
	if err != nil {
		writeInternalError(w, fmt.Errorf("readyz check failed: %w", err))
		return
	}
	if !ready {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":      false,
			"ready":   false,
			"service": "go-backend",
			"reason":  "migrations_pending",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ready": true, "service": "go-backend"})
}
