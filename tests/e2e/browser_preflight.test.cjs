const test = require('node:test');
const assert = require('node:assert/strict');

const {
  buildBrowserPreflightFailureMessage,
  getCandidateProjects,
  resolvePlayableProject,
  summarizeLaunchError,
} = require('./browser_preflight.cjs');

test('getCandidateProjects prefers chrome before chromium on local darwin', () => {
  assert.deepEqual(
    getCandidateProjects({
      env: {},
      platform: 'darwin',
      ci: false,
    }),
    ['chrome', 'chromium'],
  );
});

test('getCandidateProjects honors explicit override project', () => {
  assert.deepEqual(
    getCandidateProjects({
      env: { E2E_BROWSER_PROJECT: 'chromium' },
      platform: 'darwin',
      ci: false,
    }),
    ['chromium'],
  );
});

test('resolvePlayableProject returns the first project whose probe succeeds', async () => {
  const result = await resolvePlayableProject({
    candidates: ['chrome', 'chromium'],
    probeProject: async (project) => {
      if (project === 'chrome') {
        return {
          ok: false,
          detail: 'chrome crashed',
        };
      }
      return {
        ok: true,
        detail: 'chromium launched',
      };
    },
  });

  assert.equal(result.project, 'chromium');
  assert.deepEqual(result.attempts, [
    { project: 'chrome', ok: false, detail: 'chrome crashed' },
    { project: 'chromium', ok: true, detail: 'chromium launched' },
  ]);
});

test('buildBrowserPreflightFailureMessage includes attempted projects and override hints', () => {
  const message = buildBrowserPreflightFailureMessage({
    candidates: ['chrome', 'chromium'],
    attempts: [
      { project: 'chrome', ok: false, detail: 'signal=SIGABRT' },
      { project: 'chromium', ok: false, detail: 'Permission denied (1100)' },
    ],
  });

  assert.match(message, /Unable to launch any configured Playwright browser project/);
  assert.match(message, /chrome: signal=SIGABRT/);
  assert.match(message, /chromium: Permission denied \(1100\)/);
  assert.match(message, /E2E_BROWSER_PROJECT=/);
  assert.match(message, /E2E_SKIP_BROWSER_PREFLIGHT=1/);
});

test('summarizeLaunchError strips ansi sequences from launcher output', () => {
  const summary = summarizeLaunchError(
    new Error('\u001b[2m  - [pid=40658] <process did exit: exitCode=null, signal=SIGABRT>\u001b[22m'),
  );

  assert.equal(summary, '- [pid=40658] <process did exit: exitCode=null, signal=SIGABRT>');
});

test('getCandidateProjects reads browser override from the process environment', () => {
  const previous = process.env.E2E_BROWSER_PROJECT;
  process.env.E2E_BROWSER_PROJECT = 'chromium';
  try {
    assert.deepEqual(getCandidateProjects({ platform: 'darwin' }), ['chromium']);
  } finally {
    if (previous === undefined) delete process.env.E2E_BROWSER_PROJECT;
    else process.env.E2E_BROWSER_PROJECT = previous;
  }
});
