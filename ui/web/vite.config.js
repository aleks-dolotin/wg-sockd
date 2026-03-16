import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'
import http from 'node:http'

const socketPath = process.env.WG_SOCKD_SOCKET

function socketProxyConfig() {
  if (!socketPath) {
    return { target: 'http://localhost:8080', changeOrigin: true }
  }
  const agent = new http.Agent({ socketPath })
  return {
    target: 'http://localhost',
    agent,
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    proxy: {
      '/api': socketProxyConfig(),
      '/ui': socketProxyConfig(),
    },
  },
})
