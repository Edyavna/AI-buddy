import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const [base, layer] = process.argv.slice(2);
if (!base || !['backend', 'frontend'].includes(layer)) {
  throw new Error('Usage: node scripts/hr-coverage.mjs BASE backend|frontend');
}

const git = (...args) => execFileSync('git', args, { cwd: root, encoding: 'utf8' });
const diff = git('diff', '--no-ext-diff', '--no-color', '--unified=0', base, '--', layer);
const changed = new Map();
let file;
for (const line of diff.split(/\r?\n/)) {
  if (line.startsWith('+++ b/')) {
    file = line.slice(6);
    continue;
  }
  const range = line.match(/^@@ .* \+(\d+)(?:,(\d+))? @@/);
  if (!range || !file) continue;
  const allowed = layer === 'backend'
    ? /\.go$/.test(file) && !/_test\.go$|\/testkit\/|\/vendor\//.test(file)
    : /^frontend\/src\/.*\.tsx?$/.test(file) && !/\.(test|spec|d)\.tsx?$|\/generated\/|\/test\/|\/mocks\//.test(file);
  if (!allowed) continue;
  const lines = changed.get(file) ?? new Set();
  for (let i = 0; i < Number(range[2] ?? 1); i++) lines.add(Number(range[1]) + i);
  if (lines.size) changed.set(file, lines);
}

if (!changed.size) {
  console.log(`No changed executable ${layer} files.`);
  process.exit(0);
}

const coverage = new Map();
const data = readFileSync(new URL(layer === 'backend' ? '../backend/coverage/lines.tsv' : '../frontend/coverage/lcov.info', import.meta.url), 'utf8');
for (const line of data.split(/\r?\n/)) {
  if (layer === 'frontend' && line.startsWith('SF:')) {
    const path = line.slice(3).replaceAll('\\', '/');
    file = 'frontend/' + (path.startsWith('src/') ? path : path.slice(path.lastIndexOf('/src/') + 1));
  }
  const entry = layer === 'backend' ? line.match(/^(.+?)\t(\d+)\t([01])$/) : line.match(/^DA:(\d+),(\d+)/);
  if (!entry) continue;
  const [path, number, hits] = layer === 'backend' ? ['backend/' + entry[1].replaceAll('\\', '/'), entry[2], entry[3]] : [file, entry[1], entry[2]];
  const lines = coverage.get(path) ?? new Map();
  lines.set(Number(number), Number(hits) > 0);
  coverage.set(path, lines);
}

let total = 0;
let covered = 0;
for (const [path, lines] of changed) {
  const actual = coverage.get(path);
  if (!actual) {
    // Type-only modules and declaration changes can have an empty coverage record.
    // A newly added runtime source file must be measured by the normal coverage run.
    const source = readFileSync(new URL('../' + path, import.meta.url), 'utf8');
    if (layer === 'backend' && /\bfunc\s+[^\n]+\{/.test(source)) throw new Error('Missing Go coverage entry for ' + path);
    if (layer === 'frontend' && /\b(function|const|let|class)\b/.test(source) &&
      !/^(?:\s*export \{[^}]+\} from [^\n]+;?\s*)+$/.test(source)) {
      throw new Error(`Missing LCOV entry for ${path}`);
    }
    continue;
  }
  const executable = [...lines].filter(line => actual.has(line));
  const missing = executable.filter(line => !actual.get(line));
  if (!executable.length) continue;
  total += executable.length;
  covered += executable.length - missing.length;
  console.log(`${path}\t${executable.join(',')}\tuncovered=${missing.join(',')}`);
}
const percent = total ? (100 * covered / total).toFixed(2) : 'n/a';
console.log(`${layer} changed line coverage ${percent}% (${covered}/${total})`);
if (total && 100 * covered <= 90 * total) process.exitCode = 1;
