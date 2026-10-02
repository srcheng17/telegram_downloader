#!/usr/bin/env node

const { chromium } = require('@playwright/test');
const ANSI_PATTERN = /\u001b\[[0-9;]*m/g;

function normalizeProjectName(rawValue) {
  const normalized = String(rawValue || '').trim().toLowerCase();
  if (!normalized) {
    return '';
  }
  if (normalized === 'chrome' || normalized === 'chromium') {
    return normalized;
  }
  return '';
}

function isTruthy(value) {
  return ['1', 'true', 'yes', 'on'].includes(String(value || '').trim().toLowerCase());
}

function getCandidateProjects(options = {}) {
  const env = options.env || process.env;
  const platform = options.platform || process.platform;
  const ci = options.ci === undefined ? isTruthy(env.CI) : Boolean(options.ci);
  const explicitProject = normalizeProjectName(env.E2E_BROWSER_PROJECT || env.PLAYWRIGHT_PROJECT);
  if (explicitProject) {
    return [explicitProject];
  }
  if (ci) {
    return ['chromium'];
  }
  if (platform === 'darwin') {
    return ['chrome', 'chromium'];
  }
  return ['chromium'];
}

function summarizeLaunchError(error) {
  if (!error) {
    return 'unknown launch failure';
  }
  const message = typeof error.message === 'string' && error.message.trim() ? error.message.trim() : String(error);
  const lines = message
    .split('\n')
    .map((line) => line.replace(ANSI_PATTERN, '').trim())
    .filter(Boolean);
  for (const line of lines) {
    if (/Permission denied/i.test(line) || /signal=/i.test(line) || /Executable doesn't exist/i.test(line)) {
      return line;
    }
  }
  return lines[0] || 'unknown launch failure';
}

function buildBrowserPreflightFailureMessage(options = {}) {
  const candidates = Array.isArray(options.candidates) ? options.candidates : [];
  const attempts = Array.isArray(options.attempts) ? options.attempts : [];
  const lines = ['Unable to launch any configured Playwright browser project.'];

  if (candidates.length) {
    lines.push(`Candidates: ${candidates.join(', ')}`);
  }
  if (attempts.length) {
    lines.push('Attempt details:');
    for (const attempt of attempts) {
      lines.push(`- ${attempt.project}: ${attempt.detail || 'launch failed'}`);
    }
  }

  lines.push('Hints:');
  lines.push('- Force one project with E2E_BROWSER_PROJECT=chrome or E2E_BROWSER_PROJECT=chromium');
  lines.push('- Bypass preflight with E2E_SKIP_BROWSER_PREFLIGHT=1 if you want Playwright to fail directly');
  lines.push('- On local macOS shells, browser automation may be blocked by session/permission limits');

  return lines.join('\n');
}

async function probeProject(project) {
  const launchOptions =
    project === 'chrome'
      ? {
          channel: 'chrome',
          headless: true,
        }
      : {
          headless: true,
        };

  try {
    const browser = await chromium.launch(launchOptions);
    await browser.close();
    return {
      ok: true,
      detail: 'launch ok',
    };
  } catch (error) {
    return {
      ok: false,
      detail: summarizeLaunchError(error),
    };
  }
}

async function resolvePlayableProject(options = {}) {
  const candidates = Array.isArray(options.candidates) ? options.candidates : [];
  const probe = typeof options.probeProject === 'function' ? options.probeProject : probeProject;
  const attempts = [];

  for (const project of candidates) {
    const result = await probe(project);
    const record = {
      project,
      ok: Boolean(result && result.ok),
      detail: result && result.detail ? String(result.detail) : '',
    };
    attempts.push(record);
    if (record.ok) {
      return {
        project,
        attempts,
      };
    }
  }

  throw new Error(buildBrowserPreflightFailureMessage({ candidates, attempts }));
}

async function main() {
  const candidates = getCandidateProjects();
  if (isTruthy(process.env.E2E_SKIP_BROWSER_PREFLIGHT)) {
    process.stdout.write(`${candidates[0]}\n`);
    return;
  }

  const result = await resolvePlayableProject({ candidates });
  process.stdout.write(`${result.project}\n`);
}

if (require.main === module) {
  main().catch((error) => {
    console.error(error && error.message ? error.message : String(error));
    process.exit(1);
  });
}

module.exports = {
  buildBrowserPreflightFailureMessage,
  getCandidateProjects,
  probeProject,
  resolvePlayableProject,
  summarizeLaunchError,
};
