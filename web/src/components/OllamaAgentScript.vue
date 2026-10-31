<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { pathPrefix } from '@/base'
import { useToast } from '@/composables/useToast'
import {
  CONTEXT_PRESETS,
  DEFAULT_OLLAMA_SETTINGS,
  MODEL_SUGGESTIONS,
  agentConfig,
  agentFileName,
  bakeAgentScript,
  runCommand,
  settingsProblems,
  siteUrl,
  type OllamaAgentSettings,
} from '@/utils/ollamaAgentScript'

const props = defineProps<{
  agentId: number
  handle: string
  /** A key minted on this page and still on screen; it can be built into the file. */
  mintedKey: string | null
}>()

const toast = useToast()

// Remember choices per agent in this browser (never the key).
const storageKey = computed(() => `ordryn.ollamaAgent.${props.agentId}`)
function loadSaved(): OllamaAgentSettings {
  try {
    const raw = localStorage.getItem(storageKey.value)
    if (raw) return { ...DEFAULT_OLLAMA_SETTINGS, ...JSON.parse(raw) }
  } catch {
    /* storage unavailable or bad JSON: use defaults */
  }
  return { ...DEFAULT_OLLAMA_SETTINGS }
}
const s = reactive<OllamaAgentSettings>(loadSaved())
watch(
  s,
  (v) => {
    try {
      localStorage.setItem(storageKey.value, JSON.stringify(v))
    } catch {
      /* not saved; fine */
    }
  },
  { deep: true },
)

const includeKey = ref(true)
const keyBuiltIn = computed(() => !!props.mintedKey && includeKey.value)
const shell = ref<'powershell' | 'bash'>(navigator.userAgent.includes('Windows') ? 'powershell' : 'bash')

const site = computed(() => siteUrl(window.location.origin, pathPrefix()))
const problems = computed(() => settingsProblems(s))
const fileName = computed(() => agentFileName(props.handle))
const modelNote = computed(() => MODEL_SUGGESTIONS.find((m) => m.name === s.model.trim())?.note ?? '')
const contextHint = computed(() => CONTEXT_PRESETS.find((p) => p.tokens === s.numCtx)?.hint ?? '')
const pullCommand = computed(() => `ollama pull ${s.model.trim() || '<model>'}`)
const startCommand = computed(() => runCommand(fileName.value, shell.value, keyBuiltIn.value))

const building = ref(false)
async function download() {
  if (problems.value.length) return
  building.value = true
  try {
    const { default: source } = await import('virtual:ollama-agent')
    const cfg = agentConfig(s, site.value, keyBuiltIn.value ? props.mintedKey! : '')
    const script = bakeAgentScript(source, cfg, { handle: props.handle, site: site.value, generatedAt: new Date() })
    const url = URL.createObjectURL(new Blob([script], { type: 'text/javascript' }))
    const a = document.createElement('a')
    a.href = url
    a.download = fileName.value
    a.click()
    URL.revokeObjectURL(url)
  } catch (err) {
    toast.push(err instanceof Error ? err.message : 'Could not build the script', 'error')
  } finally {
    building.value = false
  }
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.push('Copied', 'success')
  } catch {
    toast.push('Copy failed — select the text and copy it manually', 'error')
  }
}

const id = (name: string) => `ollama-${props.agentId}-${name}`
</script>

<template>
  <div>
    <h6 class="small fw-semibold mb-1">Run a local model (Ollama)</h6>
    <p class="small text-muted mb-2">
      Download a script that runs <strong>@{{ handle }}</strong> with a model on your own computer. The script connects
      to this site to pick up work, so there's nothing to set up on a router or server. It works only while it's
      running.
    </p>

    <div class="row g-2 mb-2">
      <div class="col-sm-6">
        <label class="form-label small mb-1" :for="id('model')">Model</label>
        <input :id="id('model')" v-model="s.model" type="text" class="form-control form-control-sm" :list="id('models')" spellcheck="false" />
        <datalist :id="id('models')">
          <option v-for="m in MODEL_SUGGESTIONS" :key="m.name" :value="m.name">{{ m.note }}</option>
        </datalist>
        <div class="form-text">{{ modelNote || 'Any model you have in Ollama (see ollama list).' }}</div>
      </div>
      <div class="col-sm-6">
        <label class="form-label small mb-1" :for="id('ctx')">Context size</label>
        <select :id="id('ctx')" v-model.number="s.numCtx" class="form-select form-select-sm">
          <option v-for="p in CONTEXT_PRESETS" :key="p.tokens" :value="p.tokens">{{ p.label }} tokens</option>
          <option v-if="!CONTEXT_PRESETS.some((p) => p.tokens === s.numCtx)" :value="s.numCtx">{{ s.numCtx }} tokens</option>
        </select>
        <div class="form-text">{{ contextHint || 'How much the model can read at once. Bigger needs more GPU memory.' }}</div>
      </div>
      <div class="col-sm-6">
        <label class="form-label small mb-1" :for="id('poll')">Check for work every (seconds)</label>
        <input :id="id('poll')" v-model.number="s.pollSeconds" type="number" min="2" max="3600" class="form-control form-control-sm" />
      </div>
      <div class="col-sm-6 d-flex align-items-end">
        <div class="form-check mb-1">
          <input :id="id('think')" v-model="s.think" type="checkbox" class="form-check-input" />
          <label class="form-check-label small" :for="id('think')">Let the model think first (slower; models that support it)</label>
        </div>
      </div>
    </div>

    <fieldset class="mb-2">
      <legend class="form-label small mb-1">Access to code on your computer</legend>
      <div class="form-check">
        <input :id="id('code-none')" v-model="s.codeAccess" type="radio" value="none" class="form-check-input" />
        <label class="form-check-label small" :for="id('code-none')">None. Works from the task and its discussion only</label>
      </div>
      <div class="form-check">
        <input :id="id('code-read')" v-model="s.codeAccess" type="radio" value="read" class="form-check-input" />
        <label class="form-check-label small" :for="id('code-read')">Read one folder (proposes changes as a diff)</label>
      </div>
      <div class="form-check">
        <input :id="id('code-write')" v-model="s.codeAccess" type="radio" value="write" class="form-check-input" />
        <label class="form-check-label small" :for="id('code-write')">Read and edit one git checkout, on its own branch</label>
      </div>
    </fieldset>

    <div v-if="s.codeAccess !== 'none'" class="row g-2 mb-2 ps-sm-4">
      <div class="col-12">
        <label class="form-label small mb-1" :for="id('dir')">Folder</label>
        <input :id="id('dir')" v-model="s.repoDir" type="text" class="form-control form-control-sm" spellcheck="false"
          :placeholder="shell === 'powershell' ? 'C:\\Users\\you\\code\\my-app' : '/home/you/code/my-app'" />
        <div class="form-text">
          Secrets (.env files, keys), .git and node_modules are never read. File contents go only to the Ollama address
          below.
        </div>
      </div>
      <div class="col-sm-4">
        <label class="form-label small mb-1" :for="id('steps')">File look-ups per run</label>
        <input :id="id('steps')" v-model.number="s.maxToolSteps" type="number" min="1" max="100" class="form-control form-control-sm" />
      </div>
      <template v-if="s.codeAccess === 'write'">
        <div class="col-sm-4">
          <label class="form-label small mb-1" :for="id('writes')">File edits per run</label>
          <input :id="id('writes')" v-model.number="s.maxFileWrites" type="number" min="1" max="200" class="form-control form-control-sm" />
        </div>
        <div class="col-sm-4">
          <label class="form-label small mb-1" :for="id('protected')">Never edit branches</label>
          <input :id="id('protected')" v-model="s.protectedBranches" type="text" class="form-control form-control-sm" spellcheck="false" />
        </div>
        <div class="col-12">
          <div class="alert alert-warning small py-2 mb-0">
            Whatever people write on a task becomes instructions for a model that can change files on your computer.
            Limit <strong>Who can send work</strong> above to people you trust. The script never commits, pushes, or
            runs commands, and it switches that folder to a new branch, so point it at a separate clone or
            <code>git worktree</code>.
          </div>
        </div>
      </template>
    </div>

    <details class="small mb-2">
      <summary>Advanced</summary>
      <div class="mt-2" style="max-width: 24rem">
        <label class="form-label small mb-1" :for="id('url')">Ollama address</label>
        <input :id="id('url')" v-model="s.ollamaUrl" type="url" class="form-control form-control-sm" spellcheck="false" />
      </div>
    </details>

    <div v-if="mintedKey" class="form-check mb-2">
      <input :id="id('key')" v-model="includeKey" type="checkbox" class="form-check-input" />
      <label class="form-check-label small" :for="id('key')">
        Put the new key in the file (keep the file private; revoke the key if it leaks)
      </label>
    </div>
    <p v-else class="small text-muted mb-2">
      Create a key above to have it built into the file, or set <code>GOTODO_AGENT_KEY</code> when you run it.
    </p>

    <ul v-if="problems.length" class="small text-danger mb-2">
      <li v-for="p in problems" :key="p">{{ p }}</li>
    </ul>
    <button type="button" class="btn btn-sm btn-primary mb-3" :disabled="building || problems.length > 0" @click="download">
      <i class="bi bi-download me-1" aria-hidden="true"></i>{{ building ? 'Building…' : `Download ${fileName}` }}
    </button>

    <ol class="small ps-3 mb-0">
      <li class="mb-1">
        Install <a href="https://ollama.com/download" target="_blank" rel="noopener">Ollama</a> and
        <a href="https://nodejs.org" target="_blank" rel="noopener">Node.js</a> 18 or newer.
      </li>
      <li class="mb-1">
        Download the model:
        <div class="d-flex gap-2 align-items-start mt-1">
          <pre class="small bg-body-tertiary p-2 rounded mb-0 flex-grow-1 text-wrap text-break"><code>{{ pullCommand }}</code></pre>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="copy(pullCommand)">Copy</button>
        </div>
      </li>
      <li>
        In the folder you saved the script to, start it and leave it running (Ctrl+C stops it):
        <div v-if="!keyBuiltIn" class="btn-group btn-group-sm my-1" role="group" aria-label="Shell">
          <button type="button" class="btn" :class="shell === 'powershell' ? 'btn-secondary' : 'btn-outline-secondary'" @click="shell = 'powershell'">PowerShell</button>
          <button type="button" class="btn" :class="shell === 'bash' ? 'btn-secondary' : 'btn-outline-secondary'" @click="shell = 'bash'">macOS / Linux</button>
        </div>
        <div class="d-flex gap-2 align-items-start mt-1">
          <pre class="small bg-body-tertiary p-2 rounded mb-0 flex-grow-1 text-wrap text-break"><code>{{ startCommand }}</code></pre>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="copy(startCommand)">Copy</button>
        </div>
        <div class="form-text">
          Add <code>--once --dry-run</code> to test: it handles the queue once and prints what it would do without
          changing anything.
        </div>
      </li>
    </ol>
  </div>
</template>
