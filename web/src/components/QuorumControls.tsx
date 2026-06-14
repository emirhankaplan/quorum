import { fmtClock } from '../api'
import type { ClusterState, GetResult, PutResult } from '../types'

interface Props {
  state: ClusterState | null
  keyName: string
  setKeyName: (v: string) => void
  value: string
  setValue: (v: string) => void
  n: number
  r: number
  w: number
  setN: (v: number) => void
  setR: (v: number) => void
  setW: (v: number) => void
  putCoord: string
  setPutCoord: (v: string) => void
  getCoord: string
  setGetCoord: (v: string) => void
  busy: boolean
  onPut: () => void
  onGet: () => void
  lastPut: PutResult | null
  lastGet: GetResult | null
}

function Slider({
  label,
  value,
  min,
  max,
  onChange,
}: {
  label: string
  value: number
  min: number
  max: number
  onChange: (v: number) => void
}) {
  return (
    <label className="flex items-center gap-3 text-sm">
      <span className="w-6 font-bold text-accent">{label}</span>
      <input
        type="range"
        min={min}
        max={max}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="flex-1 accent-accent"
      />
      <span className="w-6 text-right tabular-nums">{value}</span>
    </label>
  )
}

function CoordSelect({
  state,
  value,
  onChange,
}: {
  state: ClusterState | null
  value: string
  onChange: (v: string) => void
}) {
  return (
    <select className="input w-full" value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">auto (first live)</option>
      {state?.nodes.map((nd) => (
        <option key={nd.id} value={nd.id} disabled={!nd.alive}>
          {nd.id}
          {nd.alive ? '' : ' (down)'}
        </option>
      ))}
    </select>
  )
}

function ReplicaChips({ replicas }: { replicas: { node: string; ok: boolean; err?: string }[] }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {replicas.map((rep) => (
        <span
          key={rep.node}
          title={rep.err}
          className={`chip ${rep.ok ? 'border-ok/50 bg-ok/10 text-ok' : 'border-bad/50 bg-bad/10 text-bad'}`}
        >
          {rep.ok ? '✓' : '✗'} {rep.node}
        </span>
      ))}
    </div>
  )
}

export default function QuorumControls(props: Props) {
  const { state, n, r, w } = props
  const members = state?.members.length ?? 3
  const strong = r + w > n
  const total = r + w

  return (
    <div className="panel flex flex-col gap-4 p-4">
      <div className="panel-title">read / write</div>

      <div className="grid grid-cols-2 gap-2">
        <input
          className="input"
          placeholder="key"
          value={props.keyName}
          onChange={(e) => props.setKeyName(e.target.value)}
        />
        <input
          className="input"
          placeholder="value"
          value={props.value}
          onChange={(e) => props.setValue(e.target.value)}
        />
      </div>

      <div className="flex flex-col gap-2 rounded-md border border-ink-600 bg-ink-900/50 p-3">
        <Slider label="N" value={n} min={1} max={members} onChange={props.setN} />
        <Slider label="W" value={w} min={1} max={n} onChange={props.setW} />
        <Slider label="R" value={r} min={1} max={n} onChange={props.setR} />
      </div>

      {/* The headline teaching moment: R + W > N ⇒ a read overlaps the latest write. */}
      <div
        className={`rounded-md border px-3 py-2 text-sm ${
          strong ? 'border-ok/40 bg-ok/10 text-ok' : 'border-warn/40 bg-warn/10 text-warn'
        }`}
      >
        <div className="font-bold">
          R + W = {total} {strong ? '>' : '≤'} N = {n}
        </div>
        <div className="text-xs opacity-90">
          {strong
            ? 'Read and write quorums overlap → a read is guaranteed to see the latest write.'
            : 'Quorums may not overlap → reads can return stale data and concurrent writes can create siblings.'}
        </div>
      </div>

      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-1">
          <span className="text-[11px] uppercase text-slate-500">write via</span>
          <CoordSelect state={state} value={props.putCoord} onChange={props.setPutCoord} />
          <button className="btn btn-accent" disabled={props.busy} onClick={props.onPut}>
            PUT
          </button>
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-[11px] uppercase text-slate-500">read via</span>
          <CoordSelect state={state} value={props.getCoord} onChange={props.setGetCoord} />
          <button className="btn" disabled={props.busy} onClick={props.onGet}>
            GET
          </button>
        </div>
      </div>

      {props.lastPut && (
        <div className="flex flex-col gap-1.5 border-t border-ink-600 pt-3 text-sm">
          <div className="flex items-center justify-between">
            <span className="text-slate-400">
              wrote <b className="text-slate-200">{props.lastPut.key}</b> = {props.lastPut.value}
            </span>
            <span className={props.lastPut.ok ? 'text-ok' : 'text-bad'}>
              {props.lastPut.acks}/{props.lastPut.n} acks {props.lastPut.ok ? '✓' : '✗ < W'}
            </span>
          </div>
          <div className="text-xs text-slate-500">clock {fmtClock(props.lastPut.clock)}</div>
          <ReplicaChips replicas={props.lastPut.replicas} />
        </div>
      )}

      {props.lastGet && (
        <div className="flex flex-col gap-1.5 border-t border-ink-600 pt-3 text-sm">
          <div className="flex items-center justify-between">
            <span className="text-slate-400">
              read <b className="text-slate-200">{props.lastGet.key}</b>
            </span>
            <span className={props.lastGet.ok ? 'text-ok' : 'text-bad'}>
              {props.lastGet.responses}/{props.lastGet.n} responded
            </span>
          </div>
          {props.lastGet.conflict && (
            <span className="chip w-fit border-bad/60 bg-bad/20 text-bad">
              ⚠ {props.lastGet.values.length} siblings — resolve below
            </span>
          )}
          {props.lastGet.values.map((v, i) => (
            <div key={i} className="flex items-center justify-between rounded bg-ink-900/60 px-2 py-1">
              <span className="text-slate-200">{v.tombstone ? '(deleted)' : v.value}</span>
              <span className="text-xs text-slate-500">{fmtClock(v.clock)}</span>
            </div>
          ))}
          {props.lastGet.readRepaired && props.lastGet.readRepaired.length > 0 && (
            <div className="text-xs text-ok">↻ read-repaired: {props.lastGet.readRepaired.join(', ')}</div>
          )}
        </div>
      )}
    </div>
  )
}
