import { useState } from 'react'
import type { ClusterState } from '../types'

interface Props {
  state: ClusterState | null
  busy: boolean
  onKill: (node: string) => void
  onRevive: (node: string) => void
  onPartition: (groups: string[][]) => void
  onHeal: () => void
  onLatency: (ms: number) => void
}

// Chaos is the lever room: kill nodes (clean cooperative drain), split the
// network into two reachable groups, heal, and inject replication latency.
export default function Chaos({ state, busy, onKill, onRevive, onPartition, onHeal, onLatency }: Props) {
  const members = state?.members ?? []
  const [sideA, setSideA] = useState<Set<string>>(new Set())
  const partitioned = !!state?.partition && state.partition.length > 0

  function toggle(id: string) {
    setSideA((prev) => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  function applySplit() {
    const a = members.filter((m) => sideA.has(m))
    const b = members.filter((m) => !sideA.has(m))
    if (a.length === 0 || b.length === 0) return
    onPartition([a, b])
  }

  function presetSplit() {
    if (members.length < 2) return
    const a = [members[0]]
    const b = members.slice(1)
    setSideA(new Set(a))
    onPartition([a, b])
  }

  return (
    <div className="panel flex flex-col gap-4 p-4">
      <div className="panel-title text-bad">☠ chaos</div>

      <div className="flex flex-col gap-1.5">
        {state?.nodes.map((nd) => (
          <div key={nd.id} className="flex items-center justify-between rounded bg-ink-900/50 px-2 py-1.5 text-sm">
            <span className="flex items-center gap-2">
              <span className={`h-2 w-2 rounded-full ${nd.alive ? 'bg-ok' : 'bg-slate-600'}`} />
              {nd.id}
            </span>
            {nd.alive ? (
              <button className="btn btn-danger py-0.5 text-xs" disabled={busy} onClick={() => onKill(nd.id)}>
                kill
              </button>
            ) : (
              <button className="btn py-0.5 text-xs" disabled={busy} onClick={() => onRevive(nd.id)}>
                revive
              </button>
            )}
          </div>
        ))}
      </div>

      <div className="flex flex-col gap-2 border-t border-ink-600 pt-3">
        <span className="text-[11px] uppercase text-slate-500">network partition</span>
        <p className="text-xs text-slate-500">Pick nodes for side A; the rest form side B.</p>
        <div className="flex flex-wrap gap-1.5">
          {members.map((m) => (
            <button
              key={m}
              onClick={() => toggle(m)}
              className={`chip cursor-pointer ${
                sideA.has(m) ? 'border-accent bg-accent/15 text-accent' : 'border-ink-500 text-slate-400'
              }`}
            >
              {m}
            </button>
          ))}
        </div>
        <div className="flex gap-2">
          <button className="btn flex-1 text-xs" disabled={busy} onClick={applySplit}>
            apply split
          </button>
          <button className="btn flex-1 text-xs" disabled={busy} onClick={presetSplit}>
            quick 1│rest
          </button>
          <button className="btn btn-accent flex-1 text-xs" disabled={busy || !partitioned} onClick={onHeal}>
            heal
          </button>
        </div>
      </div>

      <div className="flex flex-col gap-2 border-t border-ink-600 pt-3">
        <div className="flex items-center justify-between text-[11px] uppercase text-slate-500">
          <span>replication latency</span>
          <span className="text-accent">{state?.latencyMs ?? 0} ms</span>
        </div>
        <input
          type="range"
          min={0}
          max={300}
          step={10}
          value={state?.latencyMs ?? 0}
          onChange={(e) => onLatency(Number(e.target.value))}
          className="accent-accent"
        />
      </div>
    </div>
  )
}
