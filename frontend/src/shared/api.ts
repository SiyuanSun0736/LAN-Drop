export interface Device {
  id: string
  name: string
  os: string
  address: string
  port: number
  protocol: string
  version: string
  lastSeen: string
}

export interface DevicesResponse {
  items: Device[]
}

export interface TransferRequest {
  targetId: string
  files: string[]
}

export interface TransferJob {
  id: string
  targetId: string
  targetName?: string
  direction: 'incoming' | 'outgoing'
  files: string[]
  status: string
  message?: string
  createdAt: string
  startedAt?: string
  completedAt?: string
  bytesTotal: number
  bytesTransferred: number
  currentFile?: string
  receiveDir?: string
  error?: string
}

export interface CoreEvent {
  type: string
  payload?: unknown
  time: string
}