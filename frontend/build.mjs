// Deterministic, dependency-free frontend build. It copies the static source
// into dist/ so the Go server can serve it. Pass --serve to run a tiny static
// preview server for local development.
import { mkdirSync, copyFileSync, readdirSync, statSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';

const root = dirname(fileURLToPath(import.meta.url));
const src = join(root, 'src');
const dist = join(root, 'dist');

function copyDir(from, to) {
  mkdirSync(to, { recursive: true });
  for (const entry of readdirSync(from)) {
    const s = join(from, entry);
    const d = join(to, entry);
    if (statSync(s).isDirectory()) {
      copyDir(s, d);
    } else {
      copyFileSync(s, d);
    }
  }
}

copyDir(src, dist);
console.log(`built frontend -> ${dist}`);

if (process.argv.includes('--serve')) {
  const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' };
  createServer(async (req, res) => {
    const path = req.url === '/' ? '/index.html' : req.url.split('?')[0];
    const file = join(dist, path);
    try {
      const body = await readFile(file);
      const ext = file.slice(file.lastIndexOf('.'));
      res.writeHead(200, { 'Content-Type': mime[ext] || 'application/octet-stream' });
      res.end(body);
    } catch {
      res.writeHead(404);
      res.end('not found');
    }
  }).listen(4173, () => console.log('preview on http://localhost:4173'));
}
