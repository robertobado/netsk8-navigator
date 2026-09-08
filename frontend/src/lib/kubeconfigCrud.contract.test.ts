import { afterEach, beforeEach, describe, expect, inject, it, vi } from 'vitest'

import {
  createKubeconfigContext,
  createKubeconfigUser,
  deleteKubeconfigContext,
  deleteKubeconfigUser,
  editKubeconfigContext,
  editKubeconfigUser,
  kubeconfigView,
} from './api'

// Contract test for the kubeconfig CRUD functions in api.ts against the REAL
// Go backend — cmd/gateserver wired with a real kubeconfig.Editor over a
// disposable file (see testsupport/gateServerGlobalSetup.ts).
//
// Every KubeconfigManagerDialog.test.tsx test mocks api.createKubeconfigUser
// et al. wholesale, so a wrong URL, method, body field name, or response
// shape in api.ts passes the entire frontend suite AND the entire backend
// suite — the exact gap that let the stdio MCP write bug through, one layer
// up. This file is the wire contract for that boundary.
//
// Each test adds only its own uniquely-named entries (never touching the
// seed's c1/u1/ctx1) and removes them afterwards, so the tests are
// order-independent and need no file reset.
//
// Run with `pnpm test:contract` (needs the Go toolchain on PATH; excluded
// from the default `pnpm test` — see vitest.contract.config.ts).

// Repeated from testsupport/gateServerGlobalSetup.ts: this file is
// type-checked under tsconfig.app.json's program, which doesn't include
// testsupport/, so that copy of the augmentation is invisible here.
declare module 'vitest' {
  export interface ProvidedContext {
    gateServerUrl: string
  }
}

const baseUrl = inject('gateServerUrl')

function pointFetchAtGateServer() {
  const realFetch = fetch
  vi.stubGlobal('fetch', (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' && input.startsWith('/') ? `${baseUrl}${input}` : input
    return realFetch(url, init)
  })
}

// Best-effort teardown: every id created in a test is registered here and
// removed after it, contexts before the users/clusters they reference.
let cleanup: Array<() => Promise<unknown>> = []

async function names() {
  return kubeconfigView()
}

describe('kubeconfig CRUD (api.ts) against the real backend', () => {
  beforeEach(() => {
    cleanup = []
    pointFetchAtGateServer()
  })

  afterEach(async () => {
    for (const fn of cleanup) await fn().catch(() => {})
    vi.unstubAllGlobals()
  })

  it('creates a token user — URL, method and body shape all line up', async () => {
    await createKubeconfigUser({ name: 'ct_tok', token: 'abc123' })
    cleanup.push(() => deleteKubeconfigUser('ct_tok'))

    const u = (await names()).users.find((x) => x.name === 'ct_tok')
    expect(u).toMatchObject({ hasToken: true, hasPassword: false, hasClientCertificateData: false })
  })

  it('creates a basic-auth user', async () => {
    await createKubeconfigUser({ name: 'ct_basic', username: 'alice', password: 's3cret' })
    cleanup.push(() => deleteKubeconfigUser('ct_basic'))

    const u = (await names()).users.find((x) => x.name === 'ct_basic')
    expect(u).toMatchObject({ username: 'alice', hasPassword: true, hasToken: false })
  })

  it('creates a client-cert user', async () => {
    await createKubeconfigUser({
      name: 'ct_cert',
      clientCertificateData: '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----',
      clientKeyData: '-----BEGIN PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----',
    })
    cleanup.push(() => deleteKubeconfigUser('ct_cert'))

    const u = (await names()).users.find((x) => x.name === 'ct_cert')
    expect(u).toMatchObject({ hasClientCertificateData: true, hasClientKeyData: true, hasToken: false })
  })

  it('rejects a user with no auth material and writes nothing', async () => {
    await expect(createKubeconfigUser({ name: 'ct_empty' })).rejects.toThrow()
    expect((await names()).users.find((x) => x.name === 'ct_empty')).toBeUndefined()
  })

  it('renames a user and rewires the contexts that referenced it', async () => {
    await createKubeconfigUser({ name: 'ct_ru', token: 't' })
    await createKubeconfigContext({ name: 'ct_rctx', cluster: 'c1', user: 'ct_ru' })
    cleanup.push(() => deleteKubeconfigContext('ct_rctx'))
    cleanup.push(() => deleteKubeconfigUser('ct_ru2'))
    cleanup.push(() => deleteKubeconfigUser('ct_ru'))

    await editKubeconfigUser('ct_ru', 'ct_ru2')

    const view = await names()
    expect(view.users.map((x) => x.name)).toContain('ct_ru2')
    expect(view.users.map((x) => x.name)).not.toContain('ct_ru')
    expect(view.contexts.find((c) => c.name === 'ct_rctx')?.user).toBe('ct_ru2')
  })

  it('deletes an unused user but refuses one a context still references', async () => {
    await createKubeconfigUser({ name: 'ct_spare', token: 't' })
    await deleteKubeconfigUser('ct_spare')
    expect((await names()).users.find((x) => x.name === 'ct_spare')).toBeUndefined()

    await createKubeconfigUser({ name: 'ct_used', token: 't' })
    await createKubeconfigContext({ name: 'ct_uctx', cluster: 'c1', user: 'ct_used' })
    cleanup.push(() => deleteKubeconfigContext('ct_uctx'))
    cleanup.push(() => deleteKubeconfigUser('ct_used'))

    await expect(deleteKubeconfigUser('ct_used')).rejects.toThrow(/ct_uctx/)
    expect((await names()).users.map((x) => x.name)).toContain('ct_used')
  })

  it('shares its wire shape with the context CRUD (create, rename, delete-with-orphans)', async () => {
    await createKubeconfigContext({ name: 'ct_c2', cluster: 'c1', user: 'u1', namespace: 'kube-system' })
    cleanup.push(() => deleteKubeconfigContext('ct_c2'))
    cleanup.push(() => deleteKubeconfigContext('ct_c2_renamed'))
    expect((await names()).contexts.find((c) => c.name === 'ct_c2')).toMatchObject({
      cluster: 'c1',
      user: 'u1',
      namespace: 'kube-system',
    })

    await editKubeconfigContext('ct_c2', { newName: 'ct_c2_renamed', namespace: 'default' })
    const view = await names()
    expect(view.contexts.find((c) => c.name === 'ct_c2_renamed')).toMatchObject({ namespace: 'default' })
    expect(view.contexts.find((c) => c.name === 'ct_c2')).toBeUndefined()

    // c1/u1 are still referenced by the seed's ctx1, so nothing is orphaned —
    // the point here is that the response deserializes to the declared shape.
    const orphans = await deleteKubeconfigContext('ct_c2_renamed')
    expect(orphans).toEqual(expect.any(Object))
    expect((await names()).contexts.find((c) => c.name === 'ct_c2_renamed')).toBeUndefined()
  })
})
