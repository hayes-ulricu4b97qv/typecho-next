import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const apiProxyTarget = process.env.VITE_API_PROXY_TARGET || `http://127.0.0.1:${process.env.APP_PORT || '8080'}`

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': apiProxyTarget,
      '/media': apiProxyTarget,
      '/healthz': apiProxyTarget,
      '/readyz': apiProxyTarget,
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
