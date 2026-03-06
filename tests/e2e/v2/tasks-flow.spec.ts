import { expect, test, type Route } from '@playwright/test';

const shouldSkipV2E2E = process.env.RUN_V2_E2E === '0';

type MockTask = {
  id: string;
  url: string;
  canonical_url: string;
  status: string;
  error: string | null;
  updated_at: string;
};

function jsonResponse(route: Route, status: number, payload: unknown) {
  return route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(payload),
  });
}

test.describe('v2 tasks flow', () => {
  test.skip(shouldSkipV2E2E, 'v2 e2e is disabled by RUN_V2_E2E=0');

  test('create -> cancel -> filter -> download', async ({ page }) => {
    const tasks: MockTask[] = [
      {
        id: 'task-running',
        url: 'https://telegra.ph/task-running',
        canonical_url: 'https://telegra.ph/task-running',
        status: 'RUNNING',
        error: null,
        updated_at: '2026-03-06T10:00:00Z',
      },
      {
        id: 'task-success',
        url: 'https://telegra.ph/task-success',
        canonical_url: 'https://telegra.ph/task-success',
        status: 'SUCCESS',
        error: null,
        updated_at: '2026-03-06T10:01:00Z',
      },
    ];
    let artifactRequestedTaskID = '';

    await page.route(/\/v2\/tasks(?:\/[^/?]+(?:\/(?:cancel|artifact))?)?(?:\?.*)?$/, async (route) => {
      const request = route.request();
      const method = request.method();
      const url = new URL(request.url());
      const path = url.pathname;
      const cancelMatch = path.match(/^\/v2\/tasks\/([^/]+)\/cancel$/);
      const artifactMatch = path.match(/^\/v2\/tasks\/([^/]+)\/artifact$/);
      const detailMatch = path.match(/^\/v2\/tasks\/([^/]+)$/);

      if (method === 'POST' && path === '/v2/tasks') {
        const createdTask: MockTask = {
          id: 'task-created',
          url: 'https://telegra.ph/task-created',
          canonical_url: 'https://telegra.ph/task-created',
          status: 'QUEUED',
          error: null,
          updated_at: '2026-03-06T10:02:00Z',
        };
        tasks.unshift(createdTask);
        return jsonResponse(route, 202, {
          task_id: createdTask.id,
          status: createdTask.status,
        });
      }

      if (method === 'GET' && path === '/v2/tasks') {
        const statusFilter = String(url.searchParams.get('status') || '').trim().toUpperCase();
        const queryFilter = String(url.searchParams.get('q') || '').trim();
        const pageNum = Math.max(Number.parseInt(url.searchParams.get('page') || '1', 10) || 1, 1);
        const perPage = Math.max(Number.parseInt(url.searchParams.get('per_page') || '20', 10) || 20, 1);

        const filtered = tasks.filter((task) => {
          if (statusFilter && task.status !== statusFilter) {
            return false;
          }
          if (!queryFilter) {
            return true;
          }
          return task.id.includes(queryFilter) || task.url.includes(queryFilter);
        });
        const total = filtered.length;
        const totalPages = total > 0 ? Math.ceil(total / perPage) : 0;
        const start = (pageNum - 1) * perPage;
        const pageItems = filtered.slice(start, start + perPage);

        return jsonResponse(route, 200, {
          tasks: pageItems,
          total,
          page: pageNum,
          per_page: perPage,
          total_pages: totalPages,
        });
      }

      if (detailMatch && method === 'GET') {
        const taskID = decodeURIComponent(detailMatch[1]);
        const task = tasks.find((item) => item.id === taskID);
        if (!task) {
          return jsonResponse(route, 404, { error: 'task not found' });
        }
        return jsonResponse(route, 200, task);
      }

      if (cancelMatch && method === 'POST') {
        const taskID = decodeURIComponent(cancelMatch[1]);
        const task = tasks.find((item) => item.id === taskID);
        if (!task) {
          return jsonResponse(route, 404, { error: 'task not found' });
        }
        task.status = 'CANCELED';
        task.updated_at = '2026-03-06T10:03:00Z';
        return jsonResponse(route, 202, {
          task_id: task.id,
          status: task.status,
        });
      }

      if (artifactMatch && (method === 'GET' || method === 'HEAD')) {
        artifactRequestedTaskID = decodeURIComponent(artifactMatch[1]);
        return route.fulfill({
          status: 200,
          headers: {
            'Content-Type': 'application/zip',
            'Content-Disposition': 'attachment; filename="task-success.zip"',
          },
          body: method === 'HEAD' ? '' : 'PK\x03\x04mock',
        });
      }

      return route.fallback();
    });

    await test.step('create task', async () => {
      await page.goto('/v2');
      await page.locator('#v2-url').fill('https://telegra.ph/demo-v2');
      await page.locator('#v2-create-submit').click();
      await expect(page.locator('#v2-create-result-text')).toContainText('task-created');
    });

    await test.step('cancel task from list page', async () => {
      await page.goto('/v2/tasks-ui');
      const runningRow = page.locator('#v2-tasks-table-body tr', { hasText: 'task-running' }).first();
      await expect(runningRow).toBeVisible();
      await runningRow.getByRole('button', { name: '取消' }).click();
      await expect(page.locator('#v2-tasks-feedback')).toContainText('已提交取消');
    });

    await test.step('filter canceled tasks', async () => {
      await page.locator('#v2-filter-status').selectOption('CANCELED');
      await page.locator('#v2-filter-query').fill('task-running');
      await page.getByRole('button', { name: '筛选' }).click();
      const canceledRow = page.locator('#v2-tasks-table-body tr', { hasText: 'task-running' }).first();
      await expect(canceledRow).toContainText('CANCELED');
    });

    await test.step('filter success task and download artifact', async () => {
      await page.locator('#v2-filter-status').selectOption('SUCCESS');
      await page.locator('#v2-filter-query').fill('task-success');
      await page.getByRole('button', { name: '筛选' }).click();
      const successRow = page.locator('#v2-tasks-table-body tr', { hasText: 'task-success' }).first();
      await expect(successRow).toContainText('SUCCESS');
      await successRow.getByRole('button', { name: '下载' }).click();
      await expect.poll(() => artifactRequestedTaskID).toBe('task-success');
    });
  });
});
