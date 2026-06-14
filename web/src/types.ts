// Mirrors of the Go JSON payloads. Kept in one place so the wire protocol is
// documented on the client side too.

export type Clock = Record<string, number>

export interface VersionedValue {
  value: string
  clock: Clock
  tombstone: boolean
  updatedAt: string
  coordinator?: string
}

export interface NodeState {
  id: string
  alive: boolean
  keys: number
  ops: number
}

export interface ClusterState {
  nodes: NodeState[]
  members: string[]
  partition: string[][] | null
  latencyMs: number
}

export interface ReplicaOutcome {
  node: string
  ok: boolean
  err?: string
}

export interface PutResult {
  key: string
  value: string
  coordinator: string
  n: number
  w: number
  clock: Clock
  replicas: ReplicaOutcome[]
  acks: number
  ok: boolean
}

export interface GetResult {
  key: string
  coordinator: string
  n: number
  r: number
  replicas: ReplicaOutcome[]
  responses: number
  values: VersionedValue[]
  conflict: boolean
  readRepaired: string[] | null
  ok: boolean
}

export interface SpoofResult {
  attacker: string
  target: string
  rejected: boolean
  detail: string
}

export interface ForgeResult {
  attacker: string
  key: string
  op: string
  rejected: boolean
  reason: string
  tokenPreview: string
}

export interface WsEvent {
  seq: number
  type: string
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  data: any
}

export type WsMessage =
  | { type: 'state'; data: ClusterState }
  | { type: 'event'; data: WsEvent }
