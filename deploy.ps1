param(
    [string]$TargetOS = '',
    [string]$TargetArch = '',
    [switch]$SkipNpmInstall,
    [switch]$SkipGoTidy,
    [switch]$SkipElectronBinaryDownload,
    [switch]$PackagePortable,
    [switch]$PackageInstaller
)

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$ErrorActionPreference = 'Stop'

function Require-Command {
    param([Parameter(Mandatory = $true)][string]$Name)

    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "缺少必要命令: $Name"
    }
}

function Invoke-Step {
    param(
        [Parameter(Mandatory = $true)][string]$Message,
        [Parameter(Mandatory = $true)][scriptblock]$Script
    )

    Write-Host "==> $Message"
    & $Script
}

function Assert-LastExitCode {
    param([Parameter(Mandatory = $true)][string]$CommandName)

    if ($LASTEXITCODE -ne 0) {
        throw "$CommandName 执行失败，退出码 $LASTEXITCODE"
    }
}

$repoRoot = $PSScriptRoot
$backendDir = Join-Path $repoRoot 'backend'
$frontendDir = Join-Path $repoRoot 'frontend'
$backendBinDir = Join-Path $backendDir 'bin'

if ([string]::IsNullOrWhiteSpace($TargetOS)) {
    if ($env:GOOS) {
        $TargetOS = $env:GOOS
    }
    elseif ($env:OS -eq 'Windows_NT') {
        $TargetOS = 'windows'
    }
    elseif ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::OSX)) {
        $TargetOS = 'darwin'
    }
    else {
        $TargetOS = 'linux'
    }
}

if ([string]::IsNullOrWhiteSpace($TargetArch)) {
    if ($env:GOARCH) {
        $TargetArch = $env:GOARCH
    }
    else {
        $TargetArch = 'amd64'
    }
}

$bundleRoot = Join-Path $repoRoot ".deploy/$TargetOS-$TargetArch"
$frontendResourceBackend = Join-Path $frontendDir 'resources/backend'
$backendBinaryName = if ($TargetOS -eq 'windows') { 'landrop-agent.exe' } else { 'landrop-agent' }
$backendBinaryPath = Join-Path $backendBinDir $backendBinaryName
$packageTargets = @()

if ($PackagePortable) {
    $packageTargets += 'portable'
}

if ($PackageInstaller) {
    $packageTargets += 'nsis'
}

Require-Command -Name 'go'
Require-Command -Name 'npm'

if (-not (Test-Path $backendDir)) {
    throw '未找到 backend 目录'
}

if (-not (Test-Path $frontendDir)) {
    throw '未找到 frontend 目录'
}

New-Item -ItemType Directory -Force -Path $backendBinDir | Out-Null
New-Item -ItemType Directory -Force -Path $frontendResourceBackend | Out-Null
New-Item -ItemType Directory -Force -Path $bundleRoot | Out-Null

$originalGOOS = $env:GOOS
$originalGOARCH = $env:GOARCH
$originalSkipBinary = $env:ELECTRON_SKIP_BINARY_DOWNLOAD

try {
    $env:GOOS = $TargetOS
    $env:GOARCH = $TargetArch

    Invoke-Step -Message '构建 Go Core' -Script {
        Push-Location $backendDir
        try {
            if (-not $SkipGoTidy) {
                go mod tidy
                Assert-LastExitCode -CommandName 'go mod tidy'
            }

            go build -trimpath -ldflags '-s -w' -o $backendBinaryPath ./cmd/landrop-agent
            Assert-LastExitCode -CommandName 'go build'
        }
        finally {
            Pop-Location
        }
    }

    Invoke-Step -Message '构建 Electron 前端' -Script {
        Push-Location $frontendDir
        try {
            if ($SkipElectronBinaryDownload) {
                $env:ELECTRON_SKIP_BINARY_DOWNLOAD = '1'
            }

            if (-not $SkipNpmInstall) {
                npm install
                Assert-LastExitCode -CommandName 'npm install'
            }

            npm run build
            Assert-LastExitCode -CommandName 'npm run build'
        }
        finally {
            Pop-Location
        }
    }

    Invoke-Step -Message '同步构建产物' -Script {
        $bundleBackend = Join-Path $bundleRoot 'backend'
        $bundleFrontend = Join-Path $bundleRoot 'frontend'

        if (Test-Path $bundleBackend) {
            Remove-Item -Recurse -Force $bundleBackend
        }

        if (Test-Path $bundleFrontend) {
            Remove-Item -Recurse -Force $bundleFrontend
        }

        New-Item -ItemType Directory -Force -Path $bundleBackend | Out-Null
        New-Item -ItemType Directory -Force -Path $bundleFrontend | Out-Null

        Copy-Item -Force $backendBinaryPath (Join-Path $frontendResourceBackend $backendBinaryName)
        Copy-Item -Force $backendBinaryPath (Join-Path $bundleBackend $backendBinaryName)

        if (Test-Path (Join-Path $frontendDir 'out')) {
            Copy-Item -Recurse -Force (Join-Path $frontendDir 'out') $bundleFrontend
        }

        Copy-Item -Force (Join-Path $frontendDir 'package.json') (Join-Path $bundleFrontend 'package.json')
    }

    if ($packageTargets.Count -gt 0) {
        Invoke-Step -Message '生成分发包' -Script {
            if ($TargetOS -ne 'windows') {
                throw '当前 deploy.ps1 仅为 Windows 目标接入了 electron-builder 分发流程。'
            }

            Push-Location $frontendDir
            try {
                $packageOutputDir = Join-Path $bundleRoot 'packages'
                $frontendReleaseDir = Join-Path $frontendDir 'release'
                $builder = @(
                    (Join-Path $frontendDir 'node_modules/.bin/electron-builder.cmd'),
                    (Join-Path $frontendDir 'node_modules/.bin/electron-builder')
                ) | Where-Object { Test-Path $_ } | Select-Object -First 1

                if (-not $builder) {
                    throw '未找到 electron-builder，请在 frontend 目录重新执行 npm install。'
                }

                $builderArch = switch ($TargetArch) {
                    'amd64' { 'x64' }
                    '386' { 'ia32' }
                    default { $TargetArch }
                }

                if (Test-Path $frontendReleaseDir) {
                    Remove-Item -Recurse -Force $frontendReleaseDir
                }

                if (Test-Path $packageOutputDir) {
                    Remove-Item -Recurse -Force $packageOutputDir
                }

                $builderArgs = @('--win')
                $builderArgs += $packageTargets
                $builderArgs += '--' + $builderArch

                & $builder @builderArgs
                Assert-LastExitCode -CommandName 'electron-builder'

                if (-not (Test-Path $frontendReleaseDir)) {
                    throw 'electron-builder 未生成 release 目录。'
                }

                New-Item -ItemType Directory -Force -Path $packageOutputDir | Out-Null
                Copy-Item -Recurse -Force (Join-Path $frontendReleaseDir '*') $packageOutputDir
            }
            finally {
                Pop-Location
            }
        }
    }
}
finally {
    $env:GOOS = $originalGOOS
    $env:GOARCH = $originalGOARCH

    if ($null -eq $originalSkipBinary) {
        Remove-Item Env:ELECTRON_SKIP_BINARY_DOWNLOAD -ErrorAction SilentlyContinue
    }
    else {
        $env:ELECTRON_SKIP_BINARY_DOWNLOAD = $originalSkipBinary
    }
}

Write-Host ''
Write-Host "构建完成。Go Core: $backendBinaryPath"
Write-Host "前端资源目录: $frontendResourceBackend"
Write-Host "部署产物目录: $bundleRoot"
if ($packageTargets.Count -gt 0) {
    Write-Host "分发包目录: $(Join-Path $bundleRoot 'packages')"
}