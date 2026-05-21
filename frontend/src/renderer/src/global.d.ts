import type { BackendState } from '../../shared/backend'

declare global {
  interface Window {
    landrop: {
      getBackendState: () => Promise<BackendState>
      pickFiles: () => Promise<string[]>
      onBackendState: (listener: (state: BackendState) => void) => () => void
    }
  }
}

export {}