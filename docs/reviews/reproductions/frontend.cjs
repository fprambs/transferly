const fs = require('node:fs');
const { createRequire } = require('node:module');
const assert = require('node:assert/strict');
const root = '/workspace/frontend';
const req = createRequire(`${root}/package.json`);
const ts = req('typescript');
const { QueryClient } = req('@tanstack/react-query');
function load(relative, mocks = {}) {
  const file = `${root}/src/${relative}`;
  const js = ts.transpileModule(fs.readFileSync(file, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  const module = { exports: {} };
  new Function('require', 'module', 'exports', js)(
    (name) => Object.hasOwn(mocks, name) ? mocks[name] : req(name), module, module.exports,
  );
  return module.exports;
}
async function main() {
  const { collectFiles } = load('features/explorer/download.ts');
  let listed = 0;
  const files = await collectFiles(async () => {
    listed++;
    return listed < 2000 ? [{ type: 'directory', path: `/d${listed}`, name: `d${listed}` }] : [];
  }, '/', 1);
  assert.equal(listed, 2000);
  assert.equal(files.length, 0);
  console.log('OBSERVED: collectFiles(limit=1) traversed 2000 directories without stopping.');
  const sentinel = new Error('stopped by the reproduction');
  listed = 0;
  await assert.rejects(() => collectFiles(async () => {
    if (++listed === 20) throw sentinel;
    return [{ type: 'directory', path: '/', name: 'loop' }];
  }, '/', 1), (error) => error === sentinel);
  console.log('OBSERVED: repeated directory path visited 20 times; no cycle detection.');

  const key = ['session'];
  const { endSession } = load('app/session-expired.ts', { './session': { sessionKey: key } });
  const cache = new QueryClient();
  cache.setQueryData(key, { user: { id: 'A' } });
  cache.setQueryData(['workspaces'], [{ id: 'workspace-A', name: 'Private A' }]);
  const { useTransfersStore } = load('features/transfers/transfers-store.ts');
  useTransfersStore.getState().upsert([{ id: 'transfer-A', name: 'private-A.txt', createdAt: '2026-10-08', status: 'done' }]);
  endSession({ setCsrfToken() {} }, cache, { navigate: async () => {} });
  cache.setQueryData(key, { user: { id: 'B' } });
  assert.equal(cache.getQueryData(['workspaces'])[0].id, 'workspace-A');
  assert.ok(useTransfersStore.getState().items['transfer-A']);
  console.log('OBSERVED: after session expiry and replacement, workspace cache and transfer store still hold A data.');
  cache.clear();
  assert.ok(useTransfersStore.getState().items['transfer-A']);
  console.log('OBSERVED: queryClient.clear(), used at explicit logout, does not clear the transfer store.');

  const { remoteSource, localSource } = load('features/preview/source.ts');
  const entry = { path: '/notes.txt', name: 'notes.txt', size: 3, modifiedAt: '2026-10-08T12:00:00Z' };
  const api = { files: {} };
  const before = remoteSource(api, 'w', 's', entry);
  const after = remoteSource(api, 'w', 's', { ...entry, size: 8, modifiedAt: '2026-10-08T12:01:00Z' });
  assert.equal(before.key, after.key);
  let read = 0;
  const options = (source, value) => ({ queryKey: ['preview', source.key, 'all'], staleTime: Infinity, queryFn: async () => { read++; return value; } });
  assert.equal(await cache.fetchQuery(options(before, 'old')), 'old');
  assert.equal(await cache.fetchQuery(options(after, 'new')), 'old');
  assert.equal(read, 1);
  const firstRoot = localSource(entry, async () => {});
  const secondRoot = localSource(entry, async () => {});
  assert.equal(firstRoot.key, secondRoot.key);
  console.log('OBSERVED: changed remote file reuses stale preview; different local roots share the same preview identity.');
  cache.clear();
}
main().catch((error) => { console.error(error); process.exitCode = 1; });
