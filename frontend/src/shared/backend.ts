export type CoreStatus = 'idle' | 'starting' | 'running' | 'missing' | 'error'

export interface BackendState {
  status: CoreStatus
  endpoint?: string
  message?: string
  binaryPath?: string
}