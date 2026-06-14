import { useCallback, useEffect, useRef, useState } from 'react'
import type { ClusterState, WsEvent } from './types'

// useCluster opens the live WebSocket, tracks the latest cluster snapshot and a
// rolling event log, and lets components subscribe to the raw event stream
// (used to drive the topology animations). It auto-reconnects.
export function useCluster() {
  const [state, setState] = useState<ClusterState | null>(null)
  const [events, setEvents] = useState<WsEvent[]>([])
  const [connected, setConnected] = useState(false)
  const listeners = useRef<Array<(e: WsEvent) => void>>([])

  const onEvent = useCallback((fn: (e: WsEvent) => void) => {
    listeners.current.push(fn)
    return () => {
      listeners.current = listeners.current.filter((l) => l !== fn)
    }
  }, [])

  useEffect(() => {
    let stopped = false
    let ws: WebSocket | null = null
    let retry: ReturnType<typeof setTimeout> | undefined

    function connect() {
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      ws = new WebSocket(`${proto}://${location.host}/ws`)
      ws.onopen = () => setConnected(true)
      ws.onclose = () => {
        setConnected(false)
        if (!stopped) retry = setTimeout(connect, 1000)
      }
      ws.onerror = () => ws?.close()
      ws.onmessage = (ev) => {
        const msg = JSON.parse(ev.data)
        if (msg.type === 'state') {
          setState(msg.data as ClusterState)
        } else if (msg.type === 'event') {
          const e = msg.data as WsEvent
          setEvents((prev) => [e, ...prev].slice(0, 200))
          listeners.current.forEach((fn) => fn(e))
        }
      }
    }
    connect()

    return () => {
      stopped = true
      if (retry) clearTimeout(retry)
      ws?.close()
    }
  }, [])

  // pushState lets callers apply a state snapshot returned by a REST call for
  // instant feedback, without waiting for the next WebSocket heartbeat.
  return { state, events, connected, onEvent, pushState: setState }
}
