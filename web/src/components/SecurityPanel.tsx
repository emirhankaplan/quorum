import { useState } from 'react'
import { api } from '../api'
import type { ClusterState, ForgeResult, SpoofResult } from '../types'

interface Props {
  state: ClusterState | null
}

// SecurityPanel demonstrates Quorum's trust boundary by attacking its own
// cluster: an unauthenticated peer tries to join (rejected by node identity
// auth), and a forged/escalated capability token tries to write (rejected by
// signature verification). Both are defensive proofs — nothing external is
// touched.
export default function SecurityPanel({ state }: Props) {
  const [target, setTarget] = useState('')
  const [spoof, setSpoof] = useState<SpoofResult | null>(null)
  const [forge, setForge] = useState<ForgeResult | null>(null)
  const [busy, setBusy] = useState(false)
  const members = state?.members ?? []
  const tgt = target || members[0] || 'n1'

  async function doSpoof() {
    setBusy(true)
    try {
      setSpoof(await api.spoof(tgt))
    } finally {
      setBusy(false)
    }
  }

  async function doForge() {
    setBusy(true)
    try {
      setForge(await api.forge('secret', 'put'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="panel flex flex-col gap-4 p-4">
      <div className="panel-title text-accent">🛡 trust boundary</div>

      <div className="flex flex-col gap-2">
        <span className="text-[11px] uppercase text-slate-500">spoof a node (mutual auth)</span>
        <div className="flex gap-2">
          <select className="input flex-1" value={tgt} onChange={(e) => setTarget(e.target.value)}>
            {members.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
          <button className="btn btn-danger" disabled={busy} onClick={doSpoof}>
            inject rogue node
          </button>
        </div>
        {spoof && (
          <div
            className={`rounded-md border px-3 py-2 text-sm ${
              spoof.rejected ? 'border-ok/40 bg-ok/10 text-ok' : 'border-bad/40 bg-bad/10 text-bad'
            }`}
          >
            <b>{spoof.rejected ? 'REJECTED' : 'BREACH'}</b> — rogue “{spoof.attacker}” → {spoof.target}
            <div className="text-xs opacity-90">{spoof.detail}</div>
          </div>
        )}
      </div>

      <div className="flex flex-col gap-2 border-t border-ink-600 pt-3">
        <span className="text-[11px] uppercase text-slate-500">forge a capability token</span>
        <button className="btn btn-danger w-full" disabled={busy} onClick={doForge}>
          escalate token → PUT secret
        </button>
        {forge && (
          <div
            className={`rounded-md border px-3 py-2 text-sm ${
              forge.rejected ? 'border-ok/40 bg-ok/10 text-ok' : 'border-bad/40 bg-bad/10 text-bad'
            }`}
          >
            <b>{forge.rejected ? 'REJECTED' : 'BREACH'}</b> — {forge.reason}
            <div className="mt-1 truncate font-mono text-[10px] text-slate-500">{forge.tokenPreview}</div>
          </div>
        )}
      </div>
    </div>
  )
}
