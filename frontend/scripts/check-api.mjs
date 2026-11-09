import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
const path = new URL('../src/shared/api/generated/schema.ts', import.meta.url);
const before = readFileSync(path, 'utf8');
execFileSync(process.execPath, ['node_modules/openapi-typescript/bin/cli.js', '../contracts/openapi.yaml', '-o', 'src/shared/api/generated/schema.ts'], { stdio: 'inherit' });
if (readFileSync(path, 'utf8') !== before) {
  throw new Error('API types were stale: commit regenerated schema.ts');
}
