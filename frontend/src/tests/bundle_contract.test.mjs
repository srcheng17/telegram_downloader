import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

test('lint script excludes generated dist bundles but keeps source/static entrypoints', () => {
  const packageJson = JSON.parse(readFileSync(resolve(process.cwd(), 'package.json'), 'utf8'));
  const lintScript = String(packageJson.scripts?.lint || '');

  assert.equal(lintScript.includes('static/**/*.js'), false);
  assert.equal(lintScript.includes('static/*.js'), true);
  assert.equal(lintScript.includes('static/v2/**/*.js'), true);
});

test('vite build keeps bundle entry filenames stable', () => {
  const viteConfigText = readFileSync(resolve(process.cwd(), 'vite.config.js'), 'utf8');
  assert.equal(viteConfigText.includes("entryFileNames: '[name].bundle.js'"), true);
});
