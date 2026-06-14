import { useEffect, useState } from 'react'
import { fmtClock } from '../api'
import type { GetResult } from '../types'

interface Props {
  lastGet: GetResult | null
  busy: boolean
  onResolve: (value: string) => void
}

// SiblingMerge appears whenever a read returns concurrent versions. It shows the
// siblings side by side with their version vectors and lets the user pick one
// (or type a merged value); resolving writes the choice back with a version
// vector that descends from every sibling, so the cluster converges.
export default function SiblingMerge({ lastGet, busy, onResolve }: Props) {
  const [merged, setMerged] = useState('')
  const conflict = !!lastGet?.conflict
  const siblings = lastGet?.values ?? []

  useEffect(() => {
    if (conflict && siblings.length > 0) setMerged(siblings.map((s) => s.value).join('+'))
  }, [conflict, lastGet?.key, siblings.length]) // eslint-disable-line react-hooks/exhaustive-deps

  if (!conflict) {
    return (
      <div className="panel flex flex-col gap-2 p-4">
        <div className="panel-title">conflict resolution</div>
        <p className="text-sm text-slate-500">
          No conflict. Partition the cluster and write the same key on both sides (with W below a
          majority) to create siblings — they will surface here for merge.
        </p>
      </div>
    )
  }

  return (
    <div className="panel flex flex-col gap-3 border-bad/40 p-4">
      <div className="flex items-center gap-2">
        <span className="panel-title text-bad">⚠ siblings — merge required</span>
      </div>
      <p className="text-xs text-slate-400">
        Two writes to <b className="text-slate-200">{lastGet?.key}</b> were concurrent (neither
        version vector descends the other), so both were kept instead of one being silently lost.
      </p>

      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        {siblings.map((s, i) => (
          <button
            key={i}
            onClick={() => setMerged(s.value)}
            className="flex flex-col items-start gap-1 rounded-md border border-ink-500 bg-ink-900/60 p-3 text-left transition hover:border-accent"
          >
            <span className="text-[11px] uppercase text-slate-500">
              sibling {i + 1}
              {s.coordinator ? ` · via ${s.coordinator}` : ''}
            </span>
            <span className="text-base font-semibold text-slate-100">{s.value}</span>
            <span className="text-xs text-slate-500">{fmtClock(s.clock)}</span>
          </button>
        ))}
      </div>

      <div className="flex flex-col gap-2">
        <span className="text-[11px] uppercase text-slate-500">resolved value</span>
        <div className="flex gap-2">
          <input className="input flex-1" value={merged} onChange={(e) => setMerged(e.target.value)} />
          <button className="btn btn-accent" disabled={busy || !merged} onClick={() => onResolve(merged)}>
            resolve & write back
          </button>
        </div>
        <span className="text-xs text-slate-500">
          Writes <code className="text-slate-300">{merged || '…'}</code> with a merged version vector
          descending from all siblings → next read converges.
        </span>
      </div>
    </div>
  )
}
