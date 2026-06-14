import { useEffect, useState } from 'react'
import { api, loadToken, mergeClocks } from './api'
import { useCluster } from './ws'
import Topology from './components/Topology'
import QuorumControls from './components/QuorumControls'
import SiblingMerge from './components/SiblingMerge'
import Chaos from './components/Chaos'
import SecurityPanel from './components/SecurityPanel'
import EventLog from './components/EventLog'
import NodeInspector from './components/NodeInspector'
import type { GetResult, PutResult } from './types'

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

type Toast = { msg: string; kind: 'ok' | 'bad' | 'info' }

export default function App() {
  const { state, events, connected, onEvent, pushState } = useCluster()

  const [keyName, setKeyName] = useState('cart')
  const [value, setValue] = useState('apples')
  const [n, setN] = useState(3)
  const [w, setW] = useState(2)
  const [r, setR] = useState(2)
  const [putCoord, setPutCoord] = useState('')
  const [getCoord, setGetCoord] = useState('')
  const [lastPut, setLastPut] = useState<PutResult | null>(null)
  const [lastGet, setLastGet] = useState<GetResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<Toast | null>(null)
  const [refresh, setRefresh] = useState(0)

  useEffect(() => {
    loadToken().catch(() => setToast({ msg: 'could not load auth token', kind: 'bad' }))
  }, [])

  useEffect(() => {
    if (!toast) return
    const t = setTimeout(() => setToast(null), 2800)
    return () => clearTimeout(t)
  }, [toast])

  const bump = () => setRefresh((x) => x + 1)
  const firstLive = () => state?.nodes.find((nd) => nd.alive)?.id ?? ''

  // N changes clamp R/W down so they never exceed N.
  function changeN(v: number) {
    setN(v)
    setR((x) => Math.min(x, v))
    setW((x) => Math.min(x, v))
  }

  async function put(p: { coordinator: string; key: string; value: string; n: number; w: number; context?: Record<string, number> }) {
    setBusy(true)
    try {
      const res = await api.put(p)
      setLastPut(res)
      bump()
      setToast({ msg: `PUT ${res.key}: ${res.acks}/${res.n} acks ${res.ok ? '✓' : '✗ < W'}`, kind: res.ok ? 'ok' : 'bad' })
      return res
    } catch (e) {
      setToast({ msg: String((e as Error).message), kind: 'bad' })
    } finally {
      setBusy(false)
    }
  }

  async function get(p: { coordinator: string; key: string; n: number; r: number }) {
    setBusy(true)
    try {
      const res = await api.get(p)
      setLastGet(res)
      bump()
      setToast({
        msg: res.conflict ? `GET ${res.key}: ⚠ ${res.values.length} siblings` : `GET ${res.key}: ${res.responses}/${res.n} responded`,
        kind: res.conflict ? 'bad' : 'ok',
      })
      return res
    } catch (e) {
      setToast({ msg: String((e as Error).message), kind: 'bad' })
    } finally {
      setBusy(false)
    }
  }

  const onPut = () => put({ coordinator: putCoord, key: keyName, value, n, w })
  const onGet = () => get({ coordinator: getCoord, key: keyName, n, r })

  async function onResolve(resolved: string) {
    if (!lastGet) return
    const context = mergeClocks(lastGet.values.map((v) => v.clock))
    await put({ coordinator: putCoord || firstLive(), key: lastGet.key, value: resolved, n, w, context })
    await get({ coordinator: getCoord || firstLive(), key: lastGet.key, n, r })
  }

  // chaos handlers update state instantly from the REST response.
  const onKill = async (node: string) => pushState(await api.kill(node))
  const onRevive = async (node: string) => pushState(await api.revive(node))
  const onPartition = async (groups: string[][]) => pushState(await api.partition(groups))
  const onHeal = async () => pushState(await api.heal())
  const onLatency = async (ms: number) => pushState(await api.latency(ms))

  // Guided "CAP theorem" tour: split the cluster, write the same key on both
  // sides with a sloppy quorum, heal, then read — siblings appear.
  async function runCapDemo() {
    const m = state?.members ?? []
    if (m.length < 2) return
    setBusy(true)
    try {
      await api.heal()
      setKeyName('cart')
      changeN(3)
      setW(1)
      setR(2)
      pushState(await api.partition([[m[0]], m.slice(1)]))
      await sleep(600)
      await api.put({ coordinator: m[0], key: 'cart', value: 'from-side-A', n: 3, w: 1 }).then(setLastPut)
      await sleep(500)
      await api.put({ coordinator: m[1], key: 'cart', value: 'from-side-B', n: 3, w: 1 }).then(setLastPut)
      await sleep(500)
      pushState(await api.heal())
      await sleep(500)
      const g = await api.get({ coordinator: m[0], key: 'cart', n: 3, r: 2 })
      setLastGet(g)
      bump()
      setToast({ msg: g.conflict ? `CAP demo: ⚠ ${g.values.length} siblings surfaced` : 'CAP demo done', kind: 'bad' })
    } catch (e) {
      setToast({ msg: String((e as Error).message), kind: 'bad' })
    } finally {
      setBusy(false)
    }
  }

  function strongPreset() {
    changeN(3)
    setW(2)
    setR(2)
    setToast({ msg: 'R+W>N — strongly consistent', kind: 'ok' })
  }
  function sloppyPreset() {
    changeN(3)
    setW(1)
    setR(1)
    setToast({ msg: 'R+W≤N — sloppy / available', kind: 'info' })
  }

  return (
    <div className="mx-auto flex min-h-screen max-w-[1500px] flex-col gap-4 p-4 lg:p-6">
      {/* header */}
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-extrabold tracking-tight text-white">
            QUORUM
            <span className="ml-3 align-middle text-sm font-normal text-accent">
              break a distributed database on purpose
            </span>
          </h1>
          <p className="text-xs text-slate-500">
            leaderless quorum replication · version-vector conflicts · node-to-node trust — live
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs">
          <span className="chip border-ink-500 text-slate-400">DDIA</span>
          <span className="chip border-ink-500 text-slate-400">C++ Concurrency</span>
          <span className="chip border-ink-500 text-slate-400">WAHH</span>
          <span className={`chip ${connected ? 'border-ok/50 text-ok' : 'border-bad/50 text-bad'}`}>
            <span className={`h-1.5 w-1.5 rounded-full ${connected ? 'bg-ok' : 'bg-bad'}`} />
            {connected ? 'live' : 'offline'}
          </span>
        </div>
      </header>

      {/* scenario presets */}
      <div className="panel flex flex-wrap items-center gap-2 p-3">
        <span className="panel-title mr-1">scenarios</span>
        <button className="btn text-xs" disabled={busy} onClick={strongPreset}>
          strong (R+W&gt;N)
        </button>
        <button className="btn text-xs" disabled={busy} onClick={sloppyPreset}>
          sloppy (W=1)
        </button>
        <button className="btn btn-accent text-xs" disabled={busy} onClick={runCapDemo}>
          ▶ auto CAP partition demo
        </button>
      </div>

      {/* main grid */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-12">
        <div className="flex flex-col gap-4 lg:col-span-3">
          <QuorumControls
            state={state}
            keyName={keyName}
            setKeyName={setKeyName}
            value={value}
            setValue={setValue}
            n={n}
            r={r}
            w={w}
            setN={changeN}
            setR={setR}
            setW={setW}
            putCoord={putCoord}
            setPutCoord={setPutCoord}
            getCoord={getCoord}
            setGetCoord={setGetCoord}
            busy={busy}
            onPut={onPut}
            onGet={onGet}
            lastPut={lastPut}
            lastGet={lastGet}
          />
        </div>

        <div className="flex flex-col gap-4 lg:col-span-6">
          <div className="h-[460px]">
            <Topology state={state} onEvent={onEvent} />
          </div>
          <SiblingMerge lastGet={lastGet} busy={busy} onResolve={onResolve} />
        </div>

        <div className="flex flex-col gap-4 lg:col-span-3">
          <Chaos
            state={state}
            busy={busy}
            onKill={onKill}
            onRevive={onRevive}
            onPartition={onPartition}
            onHeal={onHeal}
            onLatency={onLatency}
          />
          <SecurityPanel state={state} />
        </div>
      </div>

      {/* bottom row */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <div className="h-[280px]">
          <EventLog events={events} />
        </div>
        <NodeInspector state={state} refreshKey={refresh} />
      </div>

      <footer className="pb-2 text-center text-[11px] text-slate-600">
        Quorum · all attacks target only this bundled cluster · see THREAT_MODEL.md
      </footer>

      {toast && (
        <div
          className={`fixed bottom-5 right-5 z-50 rounded-md border px-4 py-2 text-sm shadow-glow ${
            toast.kind === 'ok'
              ? 'border-ok/50 bg-ink-800 text-ok'
              : toast.kind === 'bad'
                ? 'border-bad/50 bg-ink-800 text-bad'
                : 'border-ink-500 bg-ink-800 text-slate-200'
          }`}
        >
          {toast.msg}
        </div>
      )}
    </div>
  )
}
