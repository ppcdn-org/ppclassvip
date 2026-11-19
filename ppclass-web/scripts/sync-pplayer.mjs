import { copyFileSync, existsSync, mkdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(here, '..');
const destination = join(webRoot, 'public', 'vendor', 'pplayer');

const files = [
  'ppwebpublish.mjs',
  'ppplayer.mjs',
  'p2p-answerer.mjs',
  'nat-probe.mjs',
];

const candidates = [
  process.env.PPLAYER_SRC,
  resolve(webRoot, '..', '..', '..', '..', 'github', 'ppcdn', 'pplayer'),
  resolve(webRoot, '..', '..', 'pplayer'),
].filter(Boolean);

const source = candidates.find((candidate) => {
  try {
    return files.every((file) => existsSync(join(candidate, file)));
  } catch {
    return false;
  }
});

if (!source) {
  console.error('未找到 pplayer 源码目录。设置 PPLAYER_SRC 指向 pplayer 仓库后重试。');
  console.error('候选目录：');
  for (const candidate of candidates) console.error(`  - ${candidate}`);
  process.exit(1);
}

mkdirSync(destination, { recursive: true });
for (const file of files) {
  copyFileSync(join(source, file), join(destination, file));
  console.log(`copied ${file}`);
}
console.log(`pplayer vendored from ${source}`);
console.log(`output: ${destination}`);
