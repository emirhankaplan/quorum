import type { WsEvent } from '../types'

const META: Record<string, { color: string; icon: string }> = {
  write: { color: 'text-accent', icon: '✎' },
  read: { color: 'text-warn', icon: '⊙' },
  replicate: { color: 'text-slate-500', icon: '→' },
  ack: { color: 'text-ok', icon: '✓' },
  repair: { color: 'text-ok', icon: '↻' },
  conflict: { color: 'text-bad', icon: '⚠' },
  node: { color: 'text-warn', icon: '☠' },
  partition: { color: 'text-bad', icon: '⚡' },
  spoof: { color: 'text-bad', icon: '🛡' },
  token: { color: 'text-bad', icon: '🔑' },
  latency: { color: 'text-slate-500', icon: '⏱' },
}

function describe(e: WsEvent): string {
  const d = e.data || {}
  switch (e.type) {
    case 'write':
      return `PUT ${d.key}=${d.value} via ${d.coordinator} (N${d.n}/W${d.w})`
    case 'read':
      return `GET ${d.key} via ${d.coordinator} (N${d.n}/R${d.r})`
    case 'replicate':
      return `${d.from} → ${d.to}  ${d.key} (${d.mode})`
    case 'ack':
      return `${d.from} acked ${d.key}`
    case 'repair':
      return `read-repair ${d.from} → ${d.to}  ${d.key}`
    case 'conflict':
      return `CONFLICT on ${d.key}: ${(d.values || []).length} siblings`
    case 'node':
      return `${d.id} ${d.alive ? 'revived' : 'killed'}`
    case 'partition':
      return d.active ? `partitioned: ${(d.groups || []).map((g: string[]) => g.join('+')).join(' │ ')}` : 'healed'
    case 'spoof':
      return `rogue → ${d.target}: ${d.rejected ? 'REJECTED' : 'BREACH'}`
    case 'token':
      return `forged token: ${d.rejected ? 'REJECTED' : 'BREACH'}`
    case 'latency':
      return `latency = ${d.ms}ms`
    default:
      return e.type
  }
}

export default function EventLog({ events }: { events: WsEvent[] }) {
  return (
    <div className="panel flex h-full flex-col p-4">
      <div className="panel-title mb-2">event stream</div>
      <div className="flex-1 overflow-y-auto pr-1">
        {events.length === 0 && <p className="text-sm text-slate-600">Waiting for activity…</p>}
        <ul className="flex flex-col gap-0.5 text-xs">
          {events.map((e) => {
            const m = META[e.type] ?? { color: 'text-slate-400', icon: '·' }
            return (
              <li key={e.seq} className="flex items-start gap-2 py-0.5">
                <span className={`${m.color} w-4 shrink-0 text-center`}>{m.icon}</span>
                <span className="text-slate-600 tabular-nums">#{e.seq}</span>
                <span className="text-slate-300">{describe(e)}</span>
              </li>
            )
          })}
        </ul>
      </div>
    </div>
  )
}
