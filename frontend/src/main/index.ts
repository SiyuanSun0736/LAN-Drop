import { app, BrowserWindow, dialog, ipcMain, type OpenDialogOptions } from 'electron'
import { existsSync } from 'node:fs'
import { join } from 'node:path'
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import type { BackendState } from '../shared/backend'

const BACKEND_CHANNEL = 'backend:state'

class CoreProcessManager {
  private process: ChildProcessWithoutNullStreams | null = null
  private state: BackendState = {
    status: 'idle',
    message: 'Core Agent 尚未启动'
  }

  constructor(private readonly publish: (state: BackendState) => void) {}

  getState(): BackendState {
    return this.state
  }

  async start(): Promise<void> {
    const binaryPath = this.resolveBinaryPath()

    if (!binaryPath) {
      this.setState({
        status: 'missing',
        message: '未找到 landrop-agent 二进制文件。后续将由 Electron 主进程负责拉起 Go Core。'
      })
      return
    }

    this.setState({
      status: 'starting',
      binaryPath,
      message: '正在启动 Core Agent...'
    })

    const child = spawn(binaryPath)
    this.process = child

    child.stdout.setEncoding('utf8')
    child.stderr.setEncoding('utf8')

    child.stdout.on('data', (chunk) => {
      const text = String(chunk)
      const endpoint = text.match(/IPC listening on (http:\/\/127\.0\.0\.1:\d+)/)?.[1]

      if (endpoint) {
        this.setState({
          status: 'running',
          binaryPath,
          endpoint,
          message: 'Core Agent 已建立本地 IPC。'
        })
        return
      }

      const message = text.trim()

      if (message) {
        this.setState({
          ...this.state,
          binaryPath,
          message
        })
      }
    })

    child.stderr.on('data', (chunk) => {
      const message = String(chunk).trim()

      if (!message) {
        return
      }

      this.setState({
        ...this.state,
        binaryPath,
        status: this.state.status === 'running' ? 'running' : 'error',
        message
      })
    })

    child.on('exit', (code) => {
      this.process = null
      this.setState({
        status: code === 0 ? 'idle' : 'error',
        binaryPath,
        message: code === 0 ? 'Core Agent 已退出。' : `Core Agent 异常退出，退出码 ${code ?? 'unknown'}`
      })
    })
  }

  async stop(): Promise<void> {
    if (!this.process) {
      return
    }

    this.process.kill()
    this.process = null
    this.setState({
      status: 'idle',
      message: 'Core Agent 已停止'
    })
  }

  private resolveBinaryPath(): string | undefined {
    const binaryName = process.platform === 'win32' ? 'landrop-agent.exe' : 'landrop-agent'
    const candidates = [
      process.env.LANDROP_CORE_BINARY,
      join(process.cwd(), '..', 'backend', 'bin', binaryName),
      join(app.getAppPath(), '..', 'backend', 'bin', binaryName),
      join(process.resourcesPath, 'backend', binaryName)
    ].filter((value): value is string => Boolean(value))

    return candidates.find((candidate) => existsSync(candidate))
  }

  private setState(nextState: BackendState): void {
    this.state = nextState
    this.publish(nextState)
  }
}

let mainWindow: BrowserWindow | null = null
let coreManager: CoreProcessManager | null = null

function broadcastBackendState(state: BackendState): void {
  mainWindow?.webContents.send(BACKEND_CHANNEL, state)
}

function resolvePreloadPath(): string {
  const candidates = [
    join(__dirname, '../preload/index.mjs'),
    join(__dirname, '../preload/index.js')
  ]

  const match = candidates.find((candidate) => existsSync(candidate))
  return match ?? candidates[0]
}

function createWindow(): void {
  const rendererURL = process.env.ELECTRON_RENDERER_URL ?? process.env.VITE_DEV_SERVER_URL

  mainWindow = new BrowserWindow({
    width: 1280,
    height: 820,
    minWidth: 1040,
    minHeight: 720,
    titleBarStyle: 'hiddenInset',
    backgroundColor: '#0e1b18',
    webPreferences: {
      sandbox: false,
      preload: resolvePreloadPath()
    }
  })

  if (rendererURL) {
    void mainWindow.loadURL(rendererURL)
  } else {
    void mainWindow.loadFile(join(__dirname, '../renderer/index.html'))
  }

  mainWindow.on('closed', () => {
    mainWindow = null
  })
}

app.whenReady().then(async () => {
  coreManager = new CoreProcessManager(broadcastBackendState)

  ipcMain.handle('backend:get-state', () => coreManager?.getState() ?? { status: 'idle' })
  ipcMain.handle('files:pick', async () => {
    const options: OpenDialogOptions = {
      title: '选择要发送的文件',
      buttonLabel: '加入发送队列',
      properties: ['openFile', 'multiSelections']
    }

    const response = mainWindow
      ? await dialog.showOpenDialog(mainWindow, options)
      : await dialog.showOpenDialog(options)

    return response.canceled ? [] : response.filePaths
  })

  createWindow()
  await coreManager.start()

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow()
    }
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    void coreManager?.stop()
    app.quit()
  }
})