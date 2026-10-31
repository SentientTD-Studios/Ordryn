import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import { build as esbuild } from 'esbuild'
import path from 'node:path'
import { fileURLToPath, URL } from 'node:url'

const AGENT_ENTRY = fileURLToPath(new URL('../tools/ollama-agent/agent.mjs', import.meta.url))
const AGENT_MODULE = 'virtual:ollama-agent'
// Must match BAKED_RAW in tools/ollama-agent/agent.mjs; the generator replaces it with the settings.
const AGENT_PLACEHOLDER = '__ORDRYN_AGENT_CONFIG__'

/**
 * Bundles the local Ollama agent (tools/ollama-agent) into one file and exposes its
 * source as `virtual:ollama-agent`, so the agent settings page can hand people a
 * single downloadable script that matches this build's API.
 */
function ollamaAgentBundle(): Plugin {
  const resolved = '\0' + AGENT_MODULE
  return {
    name: 'ollama-agent-bundle',
    resolveId: (id) => (id === AGENT_MODULE ? resolved : null),
    async load(id) {
      if (id !== resolved) return null
      const out = await esbuild({
        entryPoints: [AGENT_ENTRY],
        bundle: true,
        platform: 'node',
        format: 'esm',
        target: 'node18',
        write: false,
        legalComments: 'none',
        metafile: true,
      })
      // Metafile paths are relative to the working directory; watch them so edits reload in dev.
      for (const input of Object.keys(out.metafile.inputs)) this.addWatchFile(path.resolve(input))
      const source = out.outputFiles[0].text
      const hits = source.split(AGENT_PLACEHOLDER).length - 1
      if (hits !== 1) this.error(`ollama agent bundle: expected the settings placeholder once, found ${hits}`)
      return `export default ${JSON.stringify(source)}`
    },
  }
}

// https://vite.dev/config/
export default defineConfig(({ command }) => ({
  plugins: [vue(), ollamaAgentBundle()],
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
      '^/changelog$': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // Split shared vendor code out of the entry chunk so it stays under the
        // 500 kB warning and caches independently of app releases.
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (/node_modules[\\/](@vue|vue|vue-router)[\\/]/.test(id)) return 'vendor-vue'
          if (/node_modules[\\/](marked|dompurify|highlight\.js)[\\/]/.test(id)) return 'vendor-markdown'
          if (/node_modules[\\/](bootstrap|@popperjs)[\\/]/.test(id)) return 'vendor-bootstrap'
        },
      },
    },
  },
}))
