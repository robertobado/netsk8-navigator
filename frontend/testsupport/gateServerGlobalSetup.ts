import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import type { TestProject } from 'vitest/node'

// The kubeconfig kubeconfigCrud.contract.test.ts starts from — one cluster,
// one user, one current context. gateserver's kubeconfig.Editor needs a
// readable, parseable file at startup; the tests only ever add their own
// uniquely-named entries alongside these and clean them up, so this stays a
// stable baseline they don't mutate.
const SEED_KUBECONFIG = `apiVersion: v1
kind: Config
current-context: ctx1
clusters:
  - name: c1
    cluster:
      server: https://c1.example:6443
users:
  - name: u1
    user:
      token: seed-token
contexts:
  - name: ctx1
    context:
      cluster: c1
      user: u1
`

// Spawns backend/cmd/gateserver — the real api.Server routes over a real
// HTTP listener and a disposable config.json — for mcpGate.contract.test.ts.
// See that file, and cmd/gateserver/main.go, for why this exists: the unit
// tests in mcpGate.test.ts stub fetch, so nothing else exercises the actual
// wire contract with handlePutMCPGate / config.Store.
//
// Only wired into vitest.contract.config.ts, not the default `pnpm test` —
// it needs the Go toolchain on PATH and shouldn't slow or break the normal
// unit-test run when that's unavailable (e.g. most CI jobs).
const backendDir = path.resolve(__dirname, '../../backend')

let child: ChildProcess | undefined
let workDir: string | undefined

export default async function setup({ provide }: TestProject) {
  workDir = mkdtempSync(path.join(tmpdir(), 'netsk8-gateserver-'))
  const binPath = path.join(workDir, process.platform === 'win32' ? 'gateserver.exe' : 'gateserver')
  const configPath = path.join(workDir, 'config.json')
  const kubeconfigPath = path.join(workDir, 'kubeconfig')
  writeFileSync(kubeconfigPath, SEED_KUBECONFIG)

  // `go run` spawns the compiled binary as a child of its own process, and
  // doesn't reliably forward a kill signal down to it — teardown was
  // leaving that grandchild running and vitest's server never exited
  // cleanly. Building once and spawning the binary directly means `child`
  // below IS the server, so killing it is unambiguous.
  execFileSync('go', ['build', '-o', binPath, './cmd/gateserver'], { cwd: backendDir, stdio: 'inherit' })

  const url = await new Promise<string>((resolve, reject) => {
    const proc = spawn(binPath, ['-config', configPath, '-kubeconfig', kubeconfigPath])
    child = proc
    let out = ''
    const onData = (chunk: Buffer) => {
      out += chunk.toString()
      const match = out.match(/^READY (\S+)/m)
      if (match) {
        proc.stdout?.off('data', onData)
        resolve(match[1])
      }
    }
    proc.stdout?.on('data', onData)
    proc.stderr?.on('data', (chunk: Buffer) => process.stderr.write(`[gateserver] ${chunk}`))
    proc.once('error', reject)
    proc.once('exit', (code) => {
      if (!out.includes('READY')) reject(new Error(`gateserver exited early (code ${code})`))
    })
  })

  provide('gateServerUrl', url)

  return () => {
    child?.kill()
    if (workDir) rmSync(workDir, { recursive: true, force: true })
  }
}

declare module 'vitest' {
  export interface ProvidedContext {
    gateServerUrl: string
  }
}
