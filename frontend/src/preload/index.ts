import { contextBridge, ipcRenderer } from 'electron'
import type { BackendState } from '../shared/backend'

const BACKEND_CHANNEL = 'backend:state'

contextBridge.exposeInMainWorld('landrop', {
  getBackendState: (): Promise<BackendState> => ipcRenderer.invoke('backend:get-state'),
  pickFiles: (): Promise<string[]> => ipcRenderer.invoke('files:pick'),
  onBackendState: (listener: (state: BackendState) => void): (() => void) => {
    const wrapped = (_event: Electron.IpcRendererEvent, state: BackendState) => listener(state)

    ipcRenderer.on(BACKEND_CHANNEL, wrapped)

    return () => {
      ipcRenderer.removeListener(BACKEND_CHANNEL, wrapped)
    }
  }
})