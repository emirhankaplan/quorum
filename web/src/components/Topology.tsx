import { useEffect, useMemo, useRef, useState } from 'react'
import type { ClusterState, WsEvent } from '../types'

interface Pulse {
  id: number
  from: string
  to: string
  mode: string // write | read | repair
}

const COLORS: Record<string, string> = {
  write: '#5cf2ff',
  read: '#fbbf24',
  repair: '#34d399',
}

interface Props {
  state: ClusterState | null
  onEvent: (fn: (e: WsEvent) => void) => () => void
}

// Topology is a hand-drawn SVG of the cluster: nodes on a ring, faint links
// between them, and animated pulses that fire on every replication so a write
// or read is something you watch travel across the cluster. Partitions recolour
// the cross-group links; a conflict flashes the whole graph red.
export default function Topology({ state, onEvent }: Props) {
  const [pulses, setPulses] = useState<Pulse[]>([])
  const [conflict, setConflict] = useState(false)
  const counter = useRef(0)
  const W = 640
  const H = 460

  const nodes = state?.nodes ?? []
  const ids = nodes.map((n) => n.id)

  // Place nodes evenly on a circle.
  const pos = useMemo(() => {
    const map: Record<string, { x: number; y: number }> = {}
    const cx = W / 2
    const cy = H / 2
    const r = Math.min(W, H) * 0.32
    const n = ids.length || 1
    ids.forEach((id, i) => {
      const a = (i / n) * Math.PI * 2 - Math.PI / 2
      map[id] = { x: cx + r * Math.cos(a), y: cy + r * Math.sin(a) }
    })
    return map
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ids.join(',')])

  // group lookup for partition colouring
  const groupOf = useMemo(() => {
    const g: Record<string, number> = {}
    state?.partition?.forEach((grp, i) => grp.forEach((id) => (g[id] = i + 1)))
    return g
  }, [state?.partition])

  useEffect(() => {
    const timers: number[] = []
    const expire = (id: number) =>
      timers.push(window.setTimeout(() => setPulses((p) => p.filter((x) => x.id !== id)), 900))
    const off = onEvent((e) => {
      if (e.type === 'replicate') {
        const { from, to, mode } = e.data
        const id = ++counter.current
        setPulses((p) => [...p, { id, from, to, mode: mode === 'read' ? 'read' : 'write' }])
        expire(id)
      } else if (e.type === 'repair') {
        const { from, to } = e.data
        const id = ++counter.current
        setPulses((p) => [...p, { id, from, to, mode: 'repair' }])
        expire(id)
      } else if (e.type === 'conflict') {
        setConflict(true)
        timers.push(window.setTimeout(() => setConflict(false), 1600))
      }
    })
    return () => {
      off()
      timers.forEach(clearTimeout)
    }
  }, [onEvent])

  const partitioned = !!state?.partition && state.partition.length > 0

  return (
    <div className="panel relative h-full overflow-hidden">
      <div className="absolute left-4 top-3 z-10 flex items-center gap-2">
        <span className="panel-title">cluster topology</span>
        {partitioned && (
          <span className="chip border-bad/50 bg-bad/10 text-bad animate-pulseGlow">⚡ partitioned</span>
        )}
        {conflict && <span className="chip border-bad/60 bg-bad/20 text-bad">⚠ conflict</span>}
      </div>

      <svg viewBox={`0 0 ${W} ${H}`} className="h-full w-full">
        {/* static links between every pair of nodes */}
        {ids.map((a, i) =>
          ids.slice(i + 1).map((b) => {
            const cross = groupOf[a] !== groupOf[b]
            return (
              <line
                key={`${a}-${b}`}
                x1={pos[a].x}
                y1={pos[a].y}
                x2={pos[b].x}
                y2={pos[b].y}
                stroke={partitioned && cross ? '#fb7185' : '#2a3340'}
                strokeWidth={partitioned && cross ? 1.5 : 1}
                strokeDasharray={partitioned && cross ? '5 5' : undefined}
                opacity={partitioned && cross ? 0.7 : 0.5}
              />
            )
          }),
        )}

        {/* animated replication pulses */}
        {pulses.map((p) => {
          const a = pos[p.from]
          const b = pos[p.to]
          if (!a || !b) return null
          return (
            <circle key={p.id} r={5} fill={COLORS[p.mode] ?? '#5cf2ff'} opacity={0.95}>
              <animateMotion
                dur="0.85s"
                repeatCount="1"
                fill="freeze"
                path={`M${a.x},${a.y} L${b.x},${b.y}`}
              />
            </circle>
          )
        })}

        {/* nodes */}
        {nodes.map((n) => {
          const p = pos[n.id]
          if (!p) return null
          const g = groupOf[n.id]
          const ring =
            !n.alive ? '#475569' : conflict ? '#fb7185' : partitioned ? '#fbbf24' : '#5cf2ff'
          return (
            <g key={n.id} transform={`translate(${p.x},${p.y})`}>
              {n.alive && (
                <circle r={38} fill="none" stroke={ring} strokeWidth={1} opacity={0.25} className="animate-pulseGlow" />
              )}
              <circle
                r={30}
                fill={n.alive ? '#0f141c' : '#0b0d10'}
                stroke={ring}
                strokeWidth={2}
                opacity={n.alive ? 1 : 0.5}
              />
              <text textAnchor="middle" y={-3} fill={n.alive ? '#e2e8f0' : '#64748b'} fontSize={14} fontWeight={700}>
                {n.id}
              </text>
              <text textAnchor="middle" y={13} fill="#64748b" fontSize={9}>
                {n.alive ? `${n.ops} ops` : 'DOWN'}
              </text>
              <text textAnchor="middle" y={48} fill="#475569" fontSize={9}>
                {n.keys} keys{g ? ` · grp ${g}` : ''}
              </text>
            </g>
          )
        })}
      </svg>
    </div>
  )
}
