# LAN-Drop

LAN-Drop 是一个面向局域网场景的 P2P 加密文件传输项目，采用 Go Core + Electron UI 的双进程架构。

## 当前状态

- 已建立 Electron + React 桌面端骨架，并预留 Go Core 生命周期入口。
- 已建立 Go Core 工程骨架，包括本地 HTTP API、WebSocket 事件流、mDNS 设备发现和 TLS 1.3 分块文件传输。
- 发送端会按块读取本地文件并通过事件流广播进度，接收端会直接落盘到本机下载目录。

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

安装 Go 1.22+ 后可执行：

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

### 接收目录

默认接收目录为当前用户下载目录下的 LAN-Drop 子目录，可通过环境变量 LANDROP_RECEIVE_DIR 覆盖。

### 传输安全

- mDNS 广播会附带当前 TLS 证书的 SHA-256 指纹。
- 发送端拨号时会校验目标设备指纹，避免直接信任任意自签名证书。
- 文件通过控制帧加固定长度字节流传输，避免将大文件一次性读入内存。

## 部署脚本

根目录新增 deploy.ps1，用于自动完成以下步骤：

1. 构建 Go Core 到 backend/bin/
2. 构建 Electron 前端
3. 将后端二进制同步到 frontend/resources/backend/
4. 将构建产物整理到 .deploy/{os}-{arch}/
5. 可选产出 Windows 安装包或便携包

示例：

```powershell
./deploy.ps1
./deploy.ps1 -TargetOS linux -TargetArch amd64
./deploy.ps1 -SkipElectronBinaryDownload -SkipNpmInstall
./deploy.ps1 -PackagePortable
./deploy.ps1 -PackageInstaller
```

### 分发打包

- frontend/package.json 已接入 electron-builder，可手动执行 npm run dist:portable 或 npm run dist:installer。
- deploy.ps1 会先把 backend 二进制同步到 frontend/resources/backend/，再调用 electron-builder 产出真正可分发的桌面包。
- 当前脚本内建的分发流程以 Windows 目标为主，产物会输出到 .deploy/{os}-{arch}/packages/。
