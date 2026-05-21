# LAN-Drop

LAN-Drop 是一个面向局域网场景的 P2P 加密文件传输项目，采用 Go Core + Electron UI 的双进程架构。

## 当前状态

- 已建立 Electron + React 桌面端骨架，并预留 Go Core 生命周期入口。
- 已建立 Go Core 工程骨架，包括本地 HTTP API、WebSocket 事件流、mDNS 设备发现和占位 TLS 传输监听。
- 传输任务当前只做到排队和事件广播，真正的文件流式发送仍需继续接入。

## 目录结构

```text
frontend/  Electron UI Client
backend/   Go Core Agent
```

## 前端开发

```powershell
Set-Location frontend
npm install
npm run dev
```

Electron 主进程会尝试按以下顺序寻找 Go 二进制：

1. 环境变量 LANDROP_CORE_BINARY
2. ../backend/bin/landrop-agent(.exe)
3. 打包后的 resources/backend/

## 后端开发

当前环境未检测到 Go，因此尚未执行 Go 侧构建验证。安装 Go 1.22+ 后可执行：

```powershell
Set-Location backend
go mod tidy
go run ./cmd/landrop-agent
```

启动成功后，标准输出会打印：

```text
IPC listening on http://127.0.0.1:xxxxx
```

这条日志会被 Electron 主进程识别为 IPC 就绪信号。