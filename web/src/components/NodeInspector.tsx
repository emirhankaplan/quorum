import { useEffect, useState } from 'react'
import { api, fmtClock } from '../api'
import type { ClusterState, VersionedValue } from '../types'

interface Props {
  state: ClusterState | null
  // bump changes whenever an operation completes, so the inspector refreshes.
  refreshKey: number
}

// NodeInspector reads each replica's raw stored data so you can see divergence
// directly: during a partition one node holds a value its peers do not, and
// after read-repair they converge.
export default function NodeInspector({ state, refreshKey }: Props) {
  const members = state?.members ?? []
  const [selected, setSelected] = useState('')
  const [data, setData] = useState<Record<string, VersionedValue[]>>({})
  const node = selected || members[0] || ''

  useEffect(() => {
    if (!node) return
    let cancelled = false
    api
      .inspect(node)
      .then((d) => {
        if (!cancelled) setData(d || {})
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [node, refreshKey])

  const keys = Object.keys(data).sort()

  return (
    <div className="panel flex flex-col gap-3 p-4">
      <div className="flex items-center justify-between">
        <span className="panel-title">node inspector</span>
        <select className="input py-1 text-xs" value={node} onChange={(e) => setSelected(e.target.value)}>
          {members.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
      </div>

      {keys.length === 0 ? (
        <p className="text-sm text-slate-600">No keys stored on {node || 'this node'} yet.</p>
      ) : (
        <div className="flex flex-col gap-1 text-sm">
          {keys.map((k) => (
            <div key={k} className="rounded bg-ink-900/50 px-2 py-1.5">
              <div className="mb-1 font-semibold text-slate-200">{k}</div>
              {data[k].map((v, i) => (
                <div key={i} className="flex items-center justify-between text-xs">
                  <span className={v.tombstone ? 'text-slate-600 line-through' : 'text-slate-300'}>
                    {v.value || '(empty)'}
                  </span>
                  <span className="text-slate-500">{fmtClock(v.clock)}</span>
                </div>
              ))}
              {data[k].length > 1 && <span className="text-[10px] text-bad">⚠ {data[k].length} siblings</span>}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
