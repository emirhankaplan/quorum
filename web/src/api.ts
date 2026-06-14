import type {
  Clock,
  ClusterState,
  ForgeResult,
  GetResult,
  PutResult,
  SpoofResult,
  VersionedValue,
} from './types'

// The bundled UI authenticates with a broad capability token fetched at start;
// every key-value request carries it so the server's trust boundary is always
// exercised (see the Security panel for what happens when a token is forged).
let token = ''

export async function loadToken(): Promise<string> {
  const { token: t } = await getJSON<{ token: string }>('/api/token')
  token = t
  return token
}

// parseJSON reads the body once and tolerates a non-JSON error page (e.g. a
// proxy 502) instead of throwing an opaque SyntaxError.
async function parseJSON(r: Response): Promise<{ ok: boolean; status: number; body: any }> {
  const text = await r.text()
  let body: any = {}
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      body = { error: text.slice(0, 200) }
    }
  }
  return { ok: r.ok, status: r.status, body }
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const r = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const { ok, status, body: j } = await parseJSON(r)
  if (!ok) throw new Error(j.error || `HTTP ${status}`)
  return j as T
}

async function getJSON<T>(path: string): Promise<T> {
  const r = await fetch(path)
  const { ok, status, body } = await parseJSON(r)
  if (!ok) throw new Error(body.error || `HTTP ${status}`)
  return body as T
}

export const api = {
  state: () => getJSON<ClusterState>('/api/state'),

  put: (p: { coordinator: string; key: string; value: string; n: number; w: number; context?: Clock }) =>
    post<PutResult>('/api/put', { ...p, token }),

  get: (p: { coordinator: string; key: string; n: number; r: number }) =>
    post<GetResult>('/api/get', { ...p, token }),

  kill: (node: string) => post<ClusterState>('/api/chaos/kill', { node }),
  revive: (node: string) => post<ClusterState>('/api/chaos/revive', { node }),
  partition: (groups: string[][]) => post<ClusterState>('/api/chaos/partition', { groups }),
  heal: () => post<ClusterState>('/api/chaos/heal', {}),
  latency: (ms: number) => post<ClusterState>('/api/chaos/latency', { ms }),

  spoof: (target: string) => post<SpoofResult>('/api/security/spoof', { target }),
  forge: (key: string, op: string) => post<ForgeResult>('/api/security/forge-token', { key, op }),

  inspect: (node: string) =>
    getJSON<Record<string, VersionedValue[]>>(`/api/inspect?node=${encodeURIComponent(node)}`),
}

// Pointwise-max merge of version vectors — used when resolving siblings so the
// resolution descends from every conflicting write.
export function mergeClocks(clocks: Clock[]): Clock {
  const out: Clock = {}
  for (const c of clocks) {
    for (const k in c) out[k] = Math.max(out[k] || 0, c[k])
  }
  return out
}

export function fmtClock(c: Clock): string {
  const keys = Object.keys(c).sort()
  if (keys.length === 0) return '∅'
  return keys.map((k) => `${k}:${c[k]}`).join(' ')
}
