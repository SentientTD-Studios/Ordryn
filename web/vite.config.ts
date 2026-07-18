import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// https://vite.dev/config/
export default defineConfig(({ command }) => ({
  plugins: [vue()],
  // Dev: serve at /. Production: relative assets so one build works under / or /gotodo/.
  base: process.env.VITE_BASE || (command === 'serve' ? '/' : './'),
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/openapi.yaml': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/documentation': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/changelog': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
}))
