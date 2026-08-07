param(
  [ValidateSet('build','start','stop','status','run','credentials')]
  [string]$Action = 'start',
  [string]$Address = ':8080',
  [string]$BaseUrl = 'http://127.0.0.1:8080'
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$localRoot = Join-Path $projectRoot '.local'
$runtimePath = Join-Path $localRoot 'runtime.json'
$binaryPath = Join-Path $localRoot 'meridian-gateway.exe'
$pidPath = Join-Path $localRoot 'server.pid'
$stdoutPath = Join-Path $localRoot 'server.stdout.log'
$stderrPath = Join-Path $localRoot 'server.stderr.log'

function Get-GoExecutable {
  if ($env:MERIDIAN_GO -and (Test-Path -LiteralPath $env:MERIDIAN_GO)) { return $env:MERIDIAN_GO }
  $bundled = Join-Path $env:LOCALAPPDATA 'AtlasPay\tools\go1.26.5\go\bin\go.exe'
  if (Test-Path -LiteralPath $bundled) { return $bundled }
  $command = Get-Command go -ErrorAction SilentlyContinue
  if ($command) { return $command.Source }
  throw 'Go is not available. Set MERIDIAN_GO to go.exe.'
}

function New-RandomBase64([int]$length) {
  $bytes = New-Object byte[] $length
  [Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
  return [Convert]::ToBase64String($bytes)
}

function Get-RuntimeConfig {
  New-Item -ItemType Directory -Force -Path $localRoot | Out-Null
  if (-not (Test-Path -LiteralPath $runtimePath)) {
    $config = [ordered]@{
      master_key = New-RandomBase64 32
      admin_token = 'adm_' + (New-RandomBase64 32).Replace('+','-').Replace('/','_').TrimEnd('=')
      address = $Address
      base_url = $BaseUrl
      database_path = (Join-Path $localRoot 'meridian.db')
    }
    $config | ConvertTo-Json | Set-Content -LiteralPath $runtimePath -Encoding utf8NoBOM
  }
  return Get-Content -Raw -LiteralPath $runtimePath | ConvertFrom-Json
}

function Set-RuntimeEnvironment($config) {
  $env:MERIDIAN_MASTER_KEY = $config.master_key
  $env:MERIDIAN_ADMIN_TOKEN = $config.admin_token
  $env:MERIDIAN_ADDRESS = $config.address
  $env:MERIDIAN_BASE_URL = $config.base_url
  $env:MERIDIAN_DATABASE_PATH = $config.database_path
  $env:MERIDIAN_ENV = 'development'
}

function Build-Server {
  $go = Get-GoExecutable
  New-Item -ItemType Directory -Force -Path $localRoot | Out-Null
  & $go build -trimpath -ldflags '-s -w' -o $binaryPath ./cmd/server
  if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
}

function Get-RunningProcess {
  if (-not (Test-Path -LiteralPath $pidPath)) { return $null }
  $processId = [int](Get-Content -Raw -LiteralPath $pidPath)
  return Get-Process -Id $processId -ErrorAction SilentlyContinue
}

Set-Location -LiteralPath $projectRoot
$config = Get-RuntimeConfig

switch ($Action) {
  'build' { Build-Server; Write-Output $binaryPath }
  'credentials' { Write-Output "Admin URL: $($config.base_url)/admin"; Write-Output "Admin token: $($config.admin_token)" }
  'status' { $process = Get-RunningProcess; if ($process) { Write-Output "running pid=$($process.Id) url=$($config.base_url)" } else { Write-Output 'stopped' } }
  'stop' { $process = Get-RunningProcess; if ($process) { Stop-Process -Id $process.Id; $process.WaitForExit(10000) | Out-Null }; Remove-Item -LiteralPath $pidPath -ErrorAction SilentlyContinue; Write-Output 'stopped' }
  'run' { Build-Server; Set-RuntimeEnvironment $config; & $binaryPath }
  'start' {
    $process = Get-RunningProcess
    if ($process) { Write-Output "already running pid=$($process.Id) url=$($config.base_url)"; break }
    Build-Server
    Set-RuntimeEnvironment $config
    $process = Start-Process -FilePath $binaryPath -WorkingDirectory $projectRoot -WindowStyle Hidden -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath -PassThru
    Set-Content -LiteralPath $pidPath -Value $process.Id -Encoding ascii
    Write-Output "started pid=$($process.Id) url=$($config.base_url)"
  }
}
