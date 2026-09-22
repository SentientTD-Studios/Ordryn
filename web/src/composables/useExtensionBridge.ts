import { api } from '@/api/client'
import { APIError } from '@/api/types'

export type ExtensionBridgeContext = {
  project_id: number
  sprint_id: number
  role: string
  user_id: number
  user_name: string
  can_write: boolean
}

type BridgeRequest = {
  v?: number
  id?: string
  type?: string
  key?: string
  revision?: number
  value?: unknown
}

type BridgeReply = {
  v: 1
  id?: string
  type: string
  ok?: boolean
  data?: unknown
  error?: string
}

export function attachExtensionBridge(
  iframe: HTMLIFrameElement,
  opts: {
    extensionId: string
    projectId: number
    getContext: () => ExtensionBridgeContext
  },
): { pushContext: () => void; notifyStoreChanged: (key: string) => void; destroy: () => void } {
  const onMessage = (ev: MessageEvent) => {
    if (ev.source !== iframe.contentWindow) return
    const msg = ev.data as BridgeRequest | null
    if (!msg || typeof msg !== 'object' || msg.v !== 1 || !msg.id || !msg.type) return
    void handle(msg)
  }
  window.addEventListener('message', onMessage)

  async function handle(msg: BridgeRequest) {
    const id = String(msg.id)
    const type = String(msg.type)
    try {
      if (type === 'context' || type === 'ready') {
        reply({ v: 1, id, type: 'context', ok: true, data: opts.getContext() })
        return
      }
      if (type === 'store.list') {
        const out = await api.listExtensionStore(opts.projectId, opts.extensionId)
        reply({ v: 1, id, type, ok: true, data: out })
        return
      }
      const key = String(msg.key || '').trim()
      if (!key) {
        reply({ v: 1, id, type, ok: false, error: 'store key is required' })
        return
      }
      if (type === 'store.get') {
        try {
          const doc = await api.getExtensionStore(opts.projectId, opts.extensionId, key)
          reply({ v: 1, id, type, ok: true, data: { revision: doc.revision, value: doc.value } })
        } catch (err) {
          if (err instanceof APIError && err.status === 404) {
            reply({ v: 1, id, type, ok: true, data: { revision: 0, value: null } })
            return
          }
          throw err
        }
        return
      }
      if (type === 'store.put') {
        try {
          const doc = await api.putExtensionStore(
            opts.projectId,
            opts.extensionId,
            key,
            Number(msg.revision) || 0,
            msg.value ?? null,
          )
          reply({ v: 1, id, type, ok: true, data: { revision: doc.revision, value: doc.value } })
        } catch (err) {
          if (err instanceof APIError && err.status === 409) {
            const payload = err.payload as { current?: { revision?: number; value?: unknown } } | null
            reply({
              v: 1,
              id,
              type,
              ok: false,
              error: 'conflict',
              data: payload?.current
                ? { revision: payload.current.revision, value: payload.current.value }
                : undefined,
            })
            return
          }
          throw err
        }
        return
      }
      reply({ v: 1, id, type, ok: false, error: 'unknown request' })
    } catch (err) {
      const message = err instanceof Error ? err.message : 'request failed'
      reply({ v: 1, id, type, ok: false, error: message })
    }
  }

  function reply(payload: BridgeReply) {
    iframe.contentWindow?.postMessage(payload, '*')
  }

  return {
    pushContext() {
      reply({ v: 1, type: 'context', ok: true, data: opts.getContext() })
    },
    notifyStoreChanged(key: string) {
      reply({ v: 1, type: 'store.changed', data: { key } })
    },
    destroy() {
      window.removeEventListener('message', onMessage)
    },
  }
}
