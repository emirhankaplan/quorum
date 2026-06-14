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
  const r = await fetch('/api/token')
  const j = await r.json()
  token = j.token
  return token
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const r = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const j = await r.json()
  if (!r.ok) throw new Error(j.error || `HTTP ${r.status}`)
  return j as T
}

export const api = {
  state: () => fetch('/api/state').then((r) => r.json()) as Promise<ClusterState>,

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
    fetch(`/api/inspect?node=${encodeURIComponent(node)}`).then((r) => r.json()) as Promise<
      Record<string, VersionedValue[]>
    >,
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
