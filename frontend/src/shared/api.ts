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

export interface CoreEvent {
  type: string
  payload?: unknown
  time: string
}