// v7-go launcher: thin TypeScript supervisor for the Go forge binary.
//
// The launcher owns the trial lifecycle only: it spawns ./forge (the
// statically linked Go binary in this directory), learns the bound port
// from the binary's FORGE_PORT stdout line, registers the port with the
// trial env, and long-polls /done exactly like the reference bootstrap.
// It contains NO request handling. On done (or SIGTERM/SIGINT) it kills
// the binary and exits 0.
//
// If direct exec fails (e.g. a missing +x bit after transport), the
// launcher falls back to copying the binary to a temp dir, chmod +x,
// and execing the copy.

// SPDX-License-Identifier: MIT

import { spawn } from 'node:child_process';
import type { ChildProcess } from 'node:child_process';
import { chmodSync, copyFileSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = dirname(fileURLToPath(import.meta.url));
const FORGE_PATH = join(HERE, 'forge');

// True once any shutdown path has begun stopping the binary, so the
// child's expected exit never looks like an unexpected death.
let stopping = false;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

interface Spawned {
  child: ChildProcess;
  port: number;
}

// Spawn the binary at `binaryPath` and wait for its FORGE_PORT line.
function spawnAndWaitForPort(binaryPath: string, wantPort: number): Promise<Spawned> {
  return new Promise((resolve, reject) => {
    const child = spawn(binaryPath, [], {
      env: { ...process.env, PORT: String(wantPort) },
      stdio: ['ignore', 'pipe', 'inherit'],
    });
    let settled = false;
    let buffer = '';
    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;
      try { child.kill('SIGKILL'); } catch { /* already gone */ }
      reject(new Error('timed out waiting for FORGE_PORT'));
    }, 15000);
    timer.unref();
    const fail = (err: Error): void => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      reject(err);
    };
    child.on('error', fail);
    child.on('exit', (code, signal) => {
      fail(new Error(`forge exited before reporting its port (code=${code} signal=${signal})`));
    });
    const stdout = child.stdout;
    if (!stdout) {
      fail(new Error('no stdout pipe for forge'));
      return;
    }
    stdout.on('data', (chunk: Buffer) => {
      if (settled) {
        // The binary prints nothing else to stdout; forward strays to stderr.
        process.stderr.write(chunk);
        return;
      }
      buffer += chunk.toString('utf8');
      const nl = buffer.indexOf('\n');
      if (nl === -1) return;
      const line = buffer.slice(0, nl).trim();
      const rest = buffer.slice(nl + 1);
      if (rest.length > 0) process.stderr.write(rest);
      const m = /^FORGE_PORT=(\d+)$/.exec(line);
      if (!m) {
        fail(new Error(`unexpected first stdout line from forge: ${JSON.stringify(line)}`));
        return;
      }
      settled = true;
      clearTimeout(timer);
      child.removeAllListeners('error');
      child.removeAllListeners('exit');
      resolve({ child, port: Number(m[1]) });
    });
  });
}

async function launch(): Promise<Spawned> {
  const raw = process.env.PORT ?? '';
  const parsed = parseInt(raw, 10);
  const wantPort = Number.isFinite(parsed) ? parsed : 0;
  try {
    return await spawnAndWaitForPort(FORGE_PATH, wantPort);
  } catch (err) {
    // Fallback: copy the binary to a temp dir, chmod +x, exec the copy.
    // This covers a lost executable bit (e.g. after a transport that
    // strips modes). Only the spawn failure is retried, once.
    const code = (err as NodeJS.ErrnoException).code ?? '';
    if (code !== 'EACCES' && code !== 'ENOENT' && code !== 'EPERM') throw err;
    const dir = mkdtempSync(join(tmpdir(), 'forge-v7-'));
    const copyPath = join(dir, 'forge');
    copyFileSync(FORGE_PATH, copyPath);
    chmodSync(copyPath, 0o755);
    console.error('v7-go launcher: direct exec failed, running temp copy');
    return await spawnAndWaitForPort(copyPath, wantPort);
  }
}

async function trialLifecycle(envUrl: string, port: number, child: ChildProcess): Promise<never> {
  const base = envUrl.replace(/\/+$/, '');
  // Register: first register wins; a later one gets 409. Retry until accepted.
  let delay = 250;
  for (let attempt = 0; attempt < 80; attempt += 1) {
    try {
      const resp = await fetch(`${base}/register`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ port }),
      });
      if (resp.status === 200 || resp.status === 201 || resp.status === 409) break;
    } catch {
      // Env not reachable yet; retry with backoff.
    }
    await sleep(delay);
    delay = Math.min(2000, delay * 2);
  }
  // Poll /done until the suite finishes, then exit 0.
  let pollDelay = 250;
  for (;;) {
    try {
      const resp = await fetch(`${base}/done`);
      if (resp.ok) {
        const data = (await resp.json()) as { done?: boolean };
        if (data && data.done === true) {
          await stopChild(child);
          process.exit(0);
        }
      }
    } catch {
      // Keep polling through transient failures.
    }
    await sleep(pollDelay);
    pollDelay = Math.min(2000, pollDelay * 2);
  }
}

function stopChild(child: ChildProcess): Promise<void> {
  stopping = true;
  return new Promise((resolve) => {
    if (child.exitCode !== null || child.signalCode !== null) {
      resolve();
      return;
    }
    const timer = setTimeout(() => {
      try { child.kill('SIGKILL'); } catch { /* already gone */ }
    }, 3000);
    timer.unref();
    child.once('exit', () => {
      clearTimeout(timer);
      resolve();
    });
    try { child.kill('SIGTERM'); } catch { /* already gone */ }
  });
}

async function main(): Promise<void> {
  const { child, port } = await launch();
  console.error(`forge listening on port ${port}`);
  child.on('error', () => { /* spawn errors are handled in launch(); later errors surface via exit */ });
  const onSignal = async (): Promise<void> => {
    if (stopping) return;
    stopping = true;
    await stopChild(child);
    process.exit(0);
  };
  process.on('SIGTERM', () => { void onSignal(); });
  process.on('SIGINT', () => { void onSignal(); });
  child.on('exit', (code, signal) => {
    if (stopping) return;
    // The binary died on its own: propagate its outcome.
    process.exit(signal !== null ? 1 : (code ?? 1));
  });
  const envUrl = process.env.MAKEFUN_ENV_URL ?? '';
  if (envUrl.length > 0) {
    await trialLifecycle(envUrl, port, child);
  }
  // Local mode: serve forever; signals and child exit drive shutdown.
}

main().catch((err: unknown) => {
  console.error('v7-go launcher failed:', err instanceof Error ? err.message : err);
  process.exit(1);
});
