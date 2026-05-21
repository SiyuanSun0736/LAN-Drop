import { useEffect, useState, type ReactElement } from 'react'
import type { Device, DevicesResponse, CoreEvent, TransferJob, TransferRequest } from '../../shared/api'
import type { BackendState } from '../../shared/backend'

const fallbackState: BackendState = {
  status: 'idle',
  message: '等待主进程回报 Core Agent 状态'
}

const transferEventTypes = new Set([
  'transfer.incoming',
  'transfer.queued',
  'transfer.started',
  'transfer.progress',
  'transfer.completed',
  'transfer.failed'
])

const jobStatusLabels: Record<string, string> = {
  queued: '排队中',
  connecting: '连接中',
  running: '传输中',
  completed: '已完成',
  error: '失败'
}

function upsertTransferJob(current: TransferJob[], next: TransferJob): TransferJob[] {
  const jobs = current.filter((job) => job.id !== next.id)
  jobs.unshift(next)

  return jobs.sort((left, right) => {
    const rightTime = new Date(right.createdAt).getTime()
    const leftTime = new Date(left.createdAt).getTime()
    return rightTime - leftTime
  })
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '0 B'
  }

  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = value
  let unitIndex = 0

  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex += 1
  }

  const precision = unitIndex === 0 ? 0 : unitIndex === 1 ? 1 : 2
  return `${size.toFixed(precision)} ${units[unitIndex]}`
}

function progressPercent(job: TransferJob): number {
  if (!job.bytesTotal) {
    return job.status === 'completed' ? 100 : 0
  }

  return Math.min(100, Math.round((job.bytesTransferred / job.bytesTotal) * 100))
}

function shortPath(path: string): string {
  const segments = path.split(/[/\\]/)
  return segments[segments.length - 1] ?? path
}

function formatTime(value: string): string {
  return new Date(value).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

function eventMessage(event: CoreEvent): string {
  if (!event.payload || typeof event.payload !== 'object') {
    return ''
  }

  if ('message' in event.payload && typeof event.payload.message === 'string') {
    return event.payload.message
  }

  if ('targetName' in event.payload && typeof event.payload.targetName === 'string') {
    return event.payload.targetName
  }

  return ''
}

function App(): ReactElement {
  const [backendState, setBackendState] = useState<BackendState>(fallbackState)
  const [devices, setDevices] = useState<Device[]>([])
  const [events, setEvents] = useState<CoreEvent[]>([])
  const [selectedDeviceId, setSelectedDeviceId] = useState<string>('')
  const [selectedFiles, setSelectedFiles] = useState<string[]>([])
  const [jobs, setJobs] = useState<TransferJob[]>([])
  const [isPickingFiles, setIsPickingFiles] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [actionMessage, setActionMessage] = useState<string>('')

  useEffect(() => {
    let isMounted = true

    void window.landrop.getBackendState().then((state) => {
      if (isMounted) {
        setBackendState(state)
      }
    })

    const unsubscribe = window.landrop.onBackendState((state) => {
      setBackendState(state)
    })

    return () => {
      isMounted = false
      unsubscribe()
    }
  }, [])

  useEffect(() => {
    if (!backendState.endpoint) {
      setDevices([])
      setEvents([])
      setJobs([])
      return
    }

    const controller = new AbortController()
    const websocketURL = `${backendState.endpoint.replace(/^http/, 'ws')}/ws`
    const socket = new WebSocket(websocketURL)

    const loadDevices = async (): Promise<void> => {
      const response = await fetch(`${backendState.endpoint}/api/v1/devices`, {
        signal: controller.signal
      })

      if (!response.ok) {
        throw new Error(`读取设备列表失败: ${response.status}`)
      }

      const payload = (await response.json()) as DevicesResponse
      setDevices(payload.items)
    }

    void loadDevices().catch((error: unknown) => {
      const message = error instanceof Error ? error.message : '无法连接本地 IPC 服务'
      setEvents((current) => [
        {
          type: 'ipc.error',
          payload: { message },
          time: new Date().toISOString()
        },
        ...current
      ].slice(0, 8))
    })

    socket.addEventListener('message', (event) => {
      const payload = JSON.parse(String(event.data)) as CoreEvent

      setEvents((current) => [payload, ...current].slice(0, 8))

      if (payload.type === 'device.upserted') {
        const device = payload.payload as Device

        setDevices((current) => {
          const next = current.filter((item) => item.id !== device.id)
          return [device, ...next].sort((left, right) => left.name.localeCompare(right.name))
        })
        return
      }

      if (transferEventTypes.has(payload.type) && payload.payload) {
        const job = payload.payload as TransferJob
        setJobs((current) => upsertTransferJob(current, job))
      }
    })

    socket.addEventListener('error', () => {
      setEvents((current) => [
        {
          type: 'ipc.ws.error',
          payload: { message: 'WebSocket 连接失败' },
          time: new Date().toISOString()
        },
        ...current
      ].slice(0, 8))
    })

    return () => {
      controller.abort()
      socket.close()
    }
  }, [backendState.endpoint])

  useEffect(() => {
    if (selectedDeviceId && !devices.some((device) => device.id === selectedDeviceId)) {
      setSelectedDeviceId('')
    }
  }, [devices, selectedDeviceId])

  const selectedDevice = devices.find((device) => device.id === selectedDeviceId)
  const activeJobs = jobs.filter((job) => job.status !== 'completed' && job.status !== 'error').length
  const readyToSend = Boolean(backendState.endpoint && selectedDevice && selectedFiles.length > 0 && !isSubmitting)

  const pickFiles = async (): Promise<void> => {
    setActionMessage('')
    setIsPickingFiles(true)

    try {
      const paths = await window.landrop.pickFiles()
      if (paths.length > 0) {
        setSelectedFiles(paths)
      }
    } finally {
      setIsPickingFiles(false)
    }
  }

  const sendFiles = async (): Promise<void> => {
    if (!backendState.endpoint) {
      setActionMessage('Core Agent 尚未准备好，本次发送已取消。')
      return
    }

    if (!selectedDevice) {
      setActionMessage('请先选择一个在线设备。')
      return
    }

    if (selectedFiles.length === 0) {
      setActionMessage('请先选择至少一个文件。')
      return
    }

    setIsSubmitting(true)
    setActionMessage('')

    try {
      const requestBody: TransferRequest = {
        targetId: selectedDevice.id,
        files: selectedFiles
      }

      const response = await fetch(`${backendState.endpoint}/api/v1/transfers`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify(requestBody)
      })

      if (!response.ok) {
        const payload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(payload?.error ?? `发起传输失败: ${response.status}`)
      }

      const job = (await response.json()) as TransferJob
      setJobs((current) => upsertTransferJob(current, job))
      setActionMessage(`已向 ${selectedDevice.name} 发起 ${selectedFiles.length} 个文件的发送任务。`)
      setSelectedFiles([])
    } catch (error: unknown) {
      setActionMessage(error instanceof Error ? error.message : '发送请求失败')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <main className="workspace">
      <aside className="sidebar">
        <div className="sidebar-header">
          <span className="sidebar-title">LAN-Drop</span>
          <span className={`status-badge status-${backendState.status}`}>{backendState.status}</span>
        </div>

        <section className="sidebar-card">
          <div className="section-heading section-heading-compact">
            <h1>发送控制台</h1>
          </div>

          <dl className="summary-grid">
            <div>
              <dt>目标设备</dt>
              <dd>{selectedDevice?.name ?? '未选择'}</dd>
            </div>
            <div>
              <dt>已选文件</dt>
              <dd>{selectedFiles.length === 0 ? '0 个' : `${selectedFiles.length} 个`}</dd>
            </div>
            <div>
              <dt>IPC</dt>
              <dd>{backendState.endpoint ?? '未连接'}</dd>
            </div>
          </dl>

          <div className="actions actions-vertical">
            <button type="button" className="primary-button" onClick={() => void pickFiles()} disabled={isPickingFiles || !backendState.endpoint}>
              {isPickingFiles ? '正在打开...' : '选择文件'}
            </button>
            <button type="button" className="secondary-button" onClick={() => void sendFiles()} disabled={!readyToSend}>
              {isSubmitting ? '正在提交...' : '发送到所选设备'}
            </button>
          </div>

          <div className="selection-list">
            {selectedFiles.length === 0 ? (
              <p className="empty-state">暂无文件</p>
            ) : (
              selectedFiles.map((file) => (
                <div key={file} className="selection-item">
                  <strong>{shortPath(file)}</strong>
                  <span>{file}</span>
                </div>
              ))
            )}
          </div>

          {actionMessage ? <p className="inline-message">{actionMessage}</p> : null}
        </section>

        <section className="sidebar-card sidebar-status">
          <span className="field-label">状态</span>
          <strong>{backendState.message ?? '等待 Core 状态'}</strong>
        </section>
      </aside>

      <section className="content-area">
        <header className="toolbar">
          <div className="toolbar-item">
            <span className="field-label">在线设备</span>
            <strong>{devices.length}</strong>
          </div>
          <div className="toolbar-item">
            <span className="field-label">活动任务</span>
            <strong>{activeJobs}</strong>
          </div>
          <div className="toolbar-item toolbar-item-wide">
            <span className="field-label">当前目标</span>
            <strong>{selectedDevice?.name ?? '未选择'}</strong>
          </div>
        </header>

        <section className="content-grid">
          <article className="surface">
            <div className="section-heading">
              <h2>设备</h2>
              <span>{selectedDevice ? '已选择' : '点击右侧卡片选择'}</span>
            </div>

            <div className="device-list">
              {devices.length === 0 ? (
                <p className="empty-state">暂无设备</p>
              ) : (
                devices.map((device) => (
                  <button
                    key={device.id}
                    type="button"
                    className={`device-card selectable-card ${device.id === selectedDeviceId ? 'is-selected' : ''}`}
                    onClick={() => {
                      setSelectedDeviceId(device.id)
                      setActionMessage('')
                    }}
                  >
                    <strong>{device.name}</strong>
                    <span>{device.os || 'unknown OS'}</span>
                    <span>{device.address}:{device.port}</span>
                  </button>
                ))
              )}
            </div>
          </article>

          <article className="surface surface-wide">
            <div className="section-heading">
              <h2>传输任务</h2>
              <span>{jobs.length} 条</span>
            </div>

            <div className="job-list">
              {jobs.length === 0 ? (
                <p className="empty-state">暂无任务</p>
              ) : (
                jobs.map((job) => {
                  const percent = progressPercent(job)

                  return (
                    <div key={job.id} className="job-card">
                      <div className="job-header">
                        <div>
                          <strong>{job.direction === 'incoming' ? `来自 ${job.targetName ?? '远端设备'}` : `发送到 ${job.targetName ?? '远端设备'}`}</strong>
                          <span>{jobStatusLabels[job.status] ?? job.status}</span>
                        </div>
                        <span className="job-percent">{percent}%</span>
                      </div>

                      <div className="progress-track">
                        <div className="progress-fill" style={{ width: `${percent}%` }} />
                      </div>

                      <p className="job-message">{job.message ?? '等待状态更新'}</p>
                      <div className="job-meta">
                        <span>{formatBytes(job.bytesTransferred)} / {formatBytes(job.bytesTotal)}</span>
                        <span>{job.currentFile ?? job.files[0] ?? '待命中'}</span>
                      </div>
                      {job.error ? <p className="job-error">{job.error}</p> : null}
                      {job.receiveDir ? <p className="job-path">落盘目录：{job.receiveDir}</p> : null}
                    </div>
                  )
                })
              )}
            </div>
          </article>

          <article className="surface surface-wide">
            <div className="section-heading">
              <h2>事件</h2>
              <span>{events.length} 条</span>
            </div>

            <div className="event-log">
              {events.length === 0 ? (
                <p className="empty-state">暂无事件</p>
              ) : (
                events.map((event) => (
                  <div key={`${event.type}-${event.time}`} className="event-item">
                    <strong>{event.type}</strong>
                    <span>{eventMessage(event) || formatTime(event.time)}</span>
                  </div>
                ))
              )}
            </div>
          </article>
        </section>
      </section>
    </main>
  )
}

export default App