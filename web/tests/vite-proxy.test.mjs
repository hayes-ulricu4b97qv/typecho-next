import assert from 'node:assert/strict'
import http from 'node:http'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { createServer } from 'vite'

const configFile = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../vite.config.ts')

async function startServer(server, host = '127.0.0.1') {
  await new Promise((resolve, reject) => server.listen(0, host, resolve).once('error', reject))
  return server.address().port
}

async function closeServer(server) {
  if (server.listening) await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
}

async function unusedPort() {
  const server = http.createServer()
  const port = await startServer(server)
  await closeServer(server)
  return port
}

async function assertProxyTarget({ appPort, override, expected }) {
  const previousAppPort = process.env.APP_PORT
  const previousOverride = process.env.VITE_API_PROXY_TARGET
  const upstream = http.createServer((_request, response) => {
    response.writeHead(200, { 'content-type': 'application/json' })
    response.end(JSON.stringify({ upstream: expected }))
  })
  let vite

  try {
    const upstreamPort = await startServer(upstream)
    const vitePort = await unusedPort()
    process.env.APP_PORT = String(appPort ?? upstreamPort)
    if (override) process.env.VITE_API_PROXY_TARGET = override
    else delete process.env.VITE_API_PROXY_TARGET

    vite = await createServer({
      configFile,
      server: { host: '127.0.0.1', port: vitePort, strictPort: true },
    })
    await vite.listen()
    const address = vite.httpServer.address()
    assert.ok(address && typeof address !== 'string')
    for (const route of ['/api/v1/proxy-test', '/media/proxy-test.png', '/healthz', '/readyz']) {
      const response = await fetch(`http://127.0.0.1:${address.port}${route}`)
      assert.equal(response.status, 200, `${route} should reach the upstream`)
      assert.deepEqual(await response.json(), { upstream: expected })
    }
  } finally {
    if (vite) await vite.close()
    await closeServer(upstream)
    if (previousAppPort === undefined) delete process.env.APP_PORT
    else process.env.APP_PORT = previousAppPort
    if (previousOverride === undefined) delete process.env.VITE_API_PROXY_TARGET
    else process.env.VITE_API_PROXY_TARGET = previousOverride
  }
}

test('development proxy follows APP_PORT on loopback', async () => {
  await assertProxyTarget({ expected: 'APP_PORT' })
})

test('VITE_API_PROXY_TARGET overrides the local default', async () => {
  const selected = http.createServer((_request, response) => {
    response.writeHead(200, { 'content-type': 'application/json' })
    response.end(JSON.stringify({ upstream: 'override' }))
  })
  const selectedPort = await startServer(selected)
  try {
    await assertProxyTarget({
      appPort: selectedPort + 1,
      override: `http://127.0.0.1:${selectedPort}`,
      expected: 'override',
    })
  } finally {
    await closeServer(selected)
  }
})
