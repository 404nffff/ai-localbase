param(
  [Parameter(Position = 0)]
  [string]$Action = "help",

  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]]$Arguments
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$WorkerScript = Join-Path $ScriptDir "scripts/worker.py"
$SyncSkillScript = Join-Path (Split-Path -Parent $ScriptDir) "ai-localbase/ai-localbase.ps1"

function Show-Usage {
  @"
用法:
  ./ai-localbase-background.ps1 init [目录]
  ./ai-localbase-background.ps1 tools
  ./ai-localbase-background.ps1 list
  ./ai-localbase-background.ps1 upload [文件名] [内容] [目录]
  ./ai-localbase-background.ps1 append [documentId] [内容] [目录]
  ./ai-localbase-background.ps1 update [documentId] [内容] [目录]
  ./ai-localbase-background.ps1 delete [documentId] [目录]
  ./ai-localbase-background.ps1 worker-start [目录]
  ./ai-localbase-background.ps1 worker-status [目录]
  ./ai-localbase-background.ps1 worker-logs [目录] [行数]
  ./ai-localbase-background.ps1 worker-stop [目录]
  ./ai-localbase-background.ps1 queue-upload [文件名] [内容] [目录]
  ./ai-localbase-background.ps1 queue-append [documentId] [内容] [目录]
  ./ai-localbase-background.ps1 queue-update [documentId] [内容] [目录]
  ./ai-localbase-background.ps1 queue-delete [documentId] [目录]
  ./ai-localbase-background.ps1 search [关键词] [目录] [topK]
  ./ai-localbase-background.ps1 chat [问题] [目录]
  ./ai-localbase-background.ps1 job-status [jobId] [目录]
  ./ai-localbase-background.ps1 job-result [jobId] [目录]
"@
}

function Find-Python {
  $py = Get-Command py -ErrorAction SilentlyContinue
  if ($py) {
    return @("py", "-3")
  }

  $python = Get-Command python -ErrorAction SilentlyContinue
  if ($python) {
    return @("python")
  }

  throw "错误: 未找到 py 或 python，请先安装 Python 3"
}

function Invoke-Worker {
  param([string[]]$WorkerArgs)

  $pythonCommand = Find-Python
  $exe = $pythonCommand[0]
  $prefix = @()
  if ($pythonCommand.Count -gt 1) {
    $prefix = $pythonCommand[1..($pythonCommand.Count - 1)]
  }

  & $exe @prefix $WorkerScript @WorkerArgs
}

function Invoke-SyncSkill {
  param([string[]]$SkillArgs)

  if (-not (Test-Path -LiteralPath $SyncSkillScript)) {
    throw "错误: 未找到同步 skill 入口 $SyncSkillScript"
  }

  & $SyncSkillScript @SkillArgs
}

switch ($Action) {
  "init" {
    $workDir = if ($Arguments.Count -ge 1) { $Arguments[0] } else { (Get-Location).ProviderPath }
    Invoke-Worker @("init", "--workdir", $workDir)
    break
  }
  "tools" {
    Invoke-SyncSkill @("tools")
    break
  }
  "list" {
    Invoke-SyncSkill @("list")
    break
  }
  "upload" {
    $filename = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "example.md" }
    $content = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "# 示例文档`n`n这是测试内容。" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    Invoke-SyncSkill @("upload", $filename, $content, $workDir)
    break
  }
  "append" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $content = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    Invoke-SyncSkill @("append", $documentId, $content, $workDir)
    break
  }
  "update" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $content = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    Invoke-SyncSkill @("update", $documentId, $content, $workDir)
    break
  }
  "delete" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $workDir = if ($Arguments.Count -ge 2) { $Arguments[1] } else { (Get-Location).ProviderPath }
    Invoke-SyncSkill @("delete", $documentId, $workDir)
    break
  }
  "worker-start" {
    $workDir = if ($Arguments.Count -ge 1) { $Arguments[0] } else { (Get-Location).ProviderPath }
    Invoke-Worker @("worker-start", "--workdir", $workDir)
    break
  }
  "worker-status" {
    $workDir = if ($Arguments.Count -ge 1) { $Arguments[0] } else { (Get-Location).ProviderPath }
    Invoke-Worker @("worker-status", "--workdir", $workDir)
    break
  }
  "worker-logs" {
    $workDir = if ($Arguments.Count -ge 1) { $Arguments[0] } else { (Get-Location).ProviderPath }
    $lines = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "50" }
    Invoke-Worker @("worker-logs", "--workdir", $workDir, "--lines", $lines)
    break
  }
  "worker-stop" {
    $workDir = if ($Arguments.Count -ge 1) { $Arguments[0] } else { (Get-Location).ProviderPath }
    Invoke-Worker @("worker-stop", "--workdir", $workDir)
    break
  }
  "queue-upload" {
    $filename = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $content = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    Invoke-Worker @("queue-upload", "--filename", $filename, "--content", $content, "--workdir", $workDir)
    break
  }
  "queue-append" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $content = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    Invoke-Worker @("queue-append", "--document-id", $documentId, "--content", $content, "--workdir", $workDir)
    break
  }
  "queue-update" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $content = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    Invoke-Worker @("queue-update", "--document-id", $documentId, "--content", $content, "--workdir", $workDir)
    break
  }
  "queue-delete" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $workDir = if ($Arguments.Count -ge 2) { $Arguments[1] } else { (Get-Location).ProviderPath }
    Invoke-Worker @("queue-delete", "--document-id", $documentId, "--workdir", $workDir)
    break
  }
  "search" {
    $query = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "示例" }
    $workDir = if ($Arguments.Count -ge 2) { $Arguments[1] } else { (Get-Location).ProviderPath }
    $topK = if ($Arguments.Count -ge 3) { $Arguments[2] } else { "3" }
    Invoke-SyncSkill @("search", $query, $workDir, $topK)
    break
  }
  "chat" {
    $message = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "这是什么内容？" }
    $workDir = if ($Arguments.Count -ge 2) { $Arguments[1] } else { (Get-Location).ProviderPath }
    Invoke-SyncSkill @("chat", $message, $workDir)
    break
  }
  "job-status" {
    $jobId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $workDir = if ($Arguments.Count -ge 2) { $Arguments[1] } else { (Get-Location).ProviderPath }
    Invoke-Worker @("job-status", "--job-id", $jobId, "--workdir", $workDir)
    break
  }
  "job-result" {
    $jobId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $workDir = if ($Arguments.Count -ge 2) { $Arguments[1] } else { (Get-Location).ProviderPath }
    Invoke-Worker @("job-result", "--job-id", $jobId, "--workdir", $workDir)
    break
  }
  "help" {
    Show-Usage
    break
  }
  default {
    Write-Error "错误: 不支持的动作 $Action"
    Show-Usage
    exit 1
  }
}
