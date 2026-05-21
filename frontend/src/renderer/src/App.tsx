import { useEffect, useState, type ReactElement } from 'react'
import type { Device, DevicesResponse, CoreEvent } from '../../shared/api'
import type { BackendState } from '../../shared/backend'

const fallbackState: BackendState = {
  status: 'idle',
  message: '等待主进程回报 Core Agent 状态'
}

const milestones = [
  {
    title: '设备发现',
    body: '通过 mDNS 广播 _landrop._tcp.local 服务，在 UI 中实时维护在线设备列表。'
  },
  {
    title: '本地 IPC',
    body: 'Electron 通过 HTTP 指令接口和 WebSocket 事件流与 Go Core 解耦通信。'
  },
  {
    title: '大文件传输',
    body: '后端采用流式分块与 TLS 1.3，保证千兆局域网下稳定吞吐和低内存占用。'
  }
]

function App(): ReactElement {
  const [backendState, setBackendState] = useState<BackendState>(fallbackState)
  const [devices, setDevices] = useState<Device[]>([])
  const [events, setEvents] = useState<CoreEvent[]>([])

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

  return (
    <main className="app-shell">
      <section className="hero-panel">
        <p className="eyebrow">LAN-Drop / Local Secure Transfer</p>
        <h1>局域网内直接传，控制面与数据面彻底分离。</h1>
        <p className="hero-copy">
          当前 UI 已完成 Electron 壳体、预加载桥接和 Core Agent 生命周期入口。下一步只需让 Go 进程按约定输出
          IPC 就绪地址，即可接上真实设备发现与传输流程。
        </p>

        <div className="status-strip">
          <div>
            <span className="label">Core 状态</span>
            <strong>{backendState.status}</strong>
          </div>
          <div>
            <span className="label">IPC 端点</span>
            <strong>{backendState.endpoint ?? '等待 Go Core 上报'}</strong>
          </div>
        </div>
      </section>

      <section className="grid">
        <article className="panel">
          <h2>主进程接线</h2>
          <p>{backendState.message ?? '尚无状态消息'}</p>
          <dl className="facts">
            <div>
              <dt>拉起方式</dt>
              <dd>child_process.spawn</dd>
            </div>
            <div>
              <dt>状态同步</dt>
              <dd>ipcRenderer + preload bridge</dd>
            </div>
            <div>
              <dt>二进制发现</dt>
              <dd>环境变量 / backend/bin / 打包资源目录</dd>
            </div>
          </dl>
        </article>

        <article className="panel">
          <h2>实施路线</h2>
          <div className="milestone-list">
            {milestones.map((item) => (
              <div key={item.title} className="milestone-item">
                <h3>{item.title}</h3>
                <p>{item.body}</p>
              </div>
            ))}
          </div>
        </article>

        <article className="panel panel-accent">
          <h2>在线设备</h2>
          <div className="device-list">
            {devices.length === 0 ? (
              <p>当前还没有发现其他设备。等 Go Core 启动并完成 mDNS 浏览后，这里会实时刷新。</p>
            ) : (
              devices.map((device) => (
                <div key={device.id} className="device-card">
                  <strong>{device.name}</strong>
                  <span>{device.os || 'unknown OS'}</span>
                  <span>{device.address}:{device.port}</span>
                </div>
              ))
            )}
          </div>
        </article>

        <article className="panel panel-log">
          <h2>下一步协定</h2>
          <ul>
            <li>Go Core 启动成功后输出：IPC listening on http://127.0.0.1:PORT</li>
            <li>HTTP API 负责发起发送、取消任务、查询设备列表</li>
            <li>WebSocket 负责设备上线、进度更新、传输结束等事件广播</li>
          </ul>

          <div className="event-log">
            {events.length === 0 ? (
              <p>等待本地事件流接入。</p>
            ) : (
              events.map((event) => (
                <div key={`${event.type}-${event.time}`} className="event-item">
                  <strong>{event.type}</strong>
                  <span>{new Date(event.time).toLocaleTimeString()}</span>
                </div>
              ))
            )}
          </div>
        </article>
      </section>
    </main>
  )
}

export default App