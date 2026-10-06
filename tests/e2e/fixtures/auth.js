const base = require('@playwright/test');
const { expect } = base;
const fs = require('node:fs/promises');
const path = require('node:path');

function adminPassword() {
  const password = process.env.E2E_ADMIN_PASSWORD;
  if (!password || password.length < 12) throw new Error('E2E_ADMIN_PASSWORD must be the isolated stack synthetic administrator password.');
  return password;
}

async function loginWithAPI(context, baseURL) {
  const session = await context.request.get('/api/auth/session');
  expect(session.ok()).toBeTruthy();
  const preauth = await session.json();
  expect(preauth.csrf_token).toHaveLength(43);
  const response = await context.request.post('/api/auth/login', {
    headers: { Origin: new URL(baseURL).origin, 'X-CSRF-Token': preauth.csrf_token },
    data: { password: adminPassword() },
  });
  expect(response.status(), 'synthetic administrator login').toBe(200);
  const authenticated = await response.json();
  expect(authenticated.authenticated).toBe(true);
  return authenticated;
}

// The page and API assertions share the same real HttpOnly session cookie.
// Only this convenience client's writes inject CSRF; security tests use context.request directly.
const test = base.test.extend({
  workerAuthState: [async ({}, use) => {
    // Failed tests restart Playwright workers. Keep one isolated synthetic session
    // for the entire runner invocation instead of exhausting real login limits.
    const baseURL = process.env.E2E_BASE_URL;
    if (!baseURL) throw new Error('Use the isolated E2E runner to initialize authenticated fixtures.');
    adminPassword();
    const statePath = path.join(process.env.E2E_ARTIFACTS_DIR || 'tests/e2e/.artifacts', '.auth-session.json');
    let cached;
    try { cached = JSON.parse(await fs.readFile(statePath, 'utf8')); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
    const request = await base.request.newContext({ baseURL, storageState: cached?.baseURL === baseURL ? cached.state : undefined });
    try {
      const session = await request.get('/api/auth/session');
      const data = await session.json();
      if (!data.authenticated) await loginWithAPI({ request }, baseURL);
      const state = await request.storageState();
      await fs.mkdir(path.dirname(statePath), { recursive: true });
      await fs.writeFile(statePath, JSON.stringify({ baseURL, state }), { mode: 0o600 });
      await use(state);
    } finally { await request.dispose(); }
  }, { scope: 'worker' }],
  storageState: async ({ workerAuthState }, use) => { await use(workerAuthState); },
  authSession: [async ({ context }, use) => {
    const response = await context.request.get('/api/auth/session');
    expect(response.status()).toBe(200);
    const session = await response.json();
    expect(session.authenticated).toBe(true);
    await use(session);
  }, { auto: true }],
  request: async ({ context, baseURL, authSession }, use) => {
    const client = {};
    for (const method of ['get', 'head', 'post', 'put', 'patch', 'delete']) {
      client[method] = (url, options = {}) => context.request[method](url, {
        ...options,
        headers: { ...(['get', 'head'].includes(method) ? {} : { Origin: new URL(baseURL).origin, 'X-CSRF-Token': authSession.csrf_token }), ...options.headers },
      });
    }
    await use(client);
  },
});

async function waitForMetadata(page) {
  await expect(page.locator('#home-page')).toHaveAttribute('data-metadata-state', 'ready');
  await expect(page.locator('#metadata-title')).toBeAttached();
}

async function enterReview(page, { automatic = false } = {}) {
  await waitForMetadata(page);
  await page.locator('#automatic-preparation').setChecked(automatic);
  await page.locator('[data-workflow-next]').click();
  await expect(page.locator('#home-page')).toHaveAttribute('data-workflow-step', 'review');
  await expect(page.locator('#metadata-title')).toBeVisible();
}

function metadataDocument(schema, values, { cleared = [], revision = 1 } = {}) {
  const fields = {};
  const definition_snapshot = {};
  for (const [key, value] of Object.entries(values)) {
    expect(schema.definitions[key], `registered definition ${key}`).toBeTruthy();
    fields[key] = { state: 'value', value, revision, manual_locked: true, provenance: [{ kind: 'manual', source_id: 'user' }] };
    definition_snapshot[key] = schema.definitions[key];
  }
  for (const key of cleared) {
    fields[key] = { state: 'cleared', revision, manual_locked: true, provenance: [{ kind: 'manual', source_id: 'user' }] };
    definition_snapshot[key] = schema.definitions[key];
  }
  return { schema_version: schema.schema_version, definitions_version: schema.definitions_version, revision, fields, definition_snapshot };
}

module.exports = { test, expect, anonymousTest: base.test, adminPassword, loginWithAPI, waitForMetadata, enterReview, metadataDocument };
