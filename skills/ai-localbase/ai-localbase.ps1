param(
  [Parameter(Position = 0)]
  [string]$Action = "help",

  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]]$Arguments
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$EnvFile = Join-Path $ScriptDir ".env"
$KbConfig = Join-Path $ScriptDir "knowledge.json"

function Show-Usage {
  @"
用法:
  ./ai-localbase.ps1 init [目录]
  ./ai-localbase.ps1 tools
  ./ai-localbase.ps1 list
  ./ai-localbase.ps1 upload [文件名] [内容] [目录]
  ./ai-localbase.ps1 append [documentId] [内容] [目录]
  ./ai-localbase.ps1 update [documentId] [内容] [目录]
  ./ai-localbase.ps1 delete [documentId] [目录]
  ./ai-localbase.ps1 search [关键词] [目录] [topK]
  ./ai-localbase.ps1 chat [问题] [目录]

说明:
  - init: 初始化当前目录对应的知识库映射并输出摘要 JSON
  - tools: 通过 tools/list 列出当前可用工具能力、调用方式、参数和响应字段
  - list: 通过 knowledge_base.list 列出现有知识库名称和知识库 ID
  - upload: 上传文本内容到知识库
  - append: 向已有文档追加文本内容
  - update: 用新内容覆盖已有文档
  - delete: 删除已有文档
  - search: 在知识库中检索片段
  - chat: 基于知识库上下文发起问答
"@
}

function Ensure-Requirements {
  if (-not (Get-Command Invoke-RestMethod -ErrorAction SilentlyContinue)) {
    throw "错误: 当前 PowerShell 环境不支持 Invoke-RestMethod"
  }
}

function Load-EnvFile {
  if (-not (Test-Path -LiteralPath $EnvFile)) {
    throw "错误: .env 文件不存在，请复制 .env.example 并配置"
  }

  foreach ($line in Get-Content -LiteralPath $EnvFile) {
    if ([string]::IsNullOrWhiteSpace($line)) {
      continue
    }
    if ($line.TrimStart().StartsWith("#")) {
      continue
    }

    $parts = $line -split "=", 2
    if ($parts.Count -ne 2) {
      continue
    }

    $name = $parts[0].Trim()
    $value = $parts[1]
    Set-Variable -Scope Script -Name $name -Value $value
  }

  if ([string]::IsNullOrWhiteSpace($script:MCP_API_BASE_URL)) {
    throw "错误: .env 中缺少 MCP_API_BASE_URL"
  }
  if ([string]::IsNullOrWhiteSpace($script:MCP_AUTH_TOKEN)) {
    throw "错误: .env 中缺少 MCP_AUTH_TOKEN"
  }

  $script:AuthHeader = @{
    Authorization = "Bearer $($script:MCP_AUTH_TOKEN)"
  }
}

function Resolve-WorkDir {
  param([string]$InputPath)

  if ([string]::IsNullOrWhiteSpace($InputPath)) {
    return (Get-Location).ProviderPath
  }

  if (Test-Path -LiteralPath $InputPath) {
    return (Resolve-Path -LiteralPath $InputPath).ProviderPath
  }

  return [System.IO.Path]::GetFullPath($InputPath)
}

function Test-ProjectRoot {
  param([string]$Path)

  if ([string]::IsNullOrWhiteSpace($Path) -or -not (Test-Path -LiteralPath $Path -PathType Container)) {
    return $false
  }

  $markers = @(
    "AGENTS.md",
    "agents.md",
    ".git",
    "package.json",
    "composer.json",
    "go.mod",
    "pyproject.toml",
    "README.md"
  )

  foreach ($marker in $markers) {
    if (Test-Path -LiteralPath (Join-Path $Path $marker)) {
      return $true
    }
  }

  return $false
}

function Resolve-ProjectWorkDir {
  param([string]$WorkDir)

  $fullPath = [System.IO.Path]::GetFullPath($WorkDir)
  $segments = $fullPath -split '[\\/]+' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }

  for ($index = $segments.Count - 1; $index -ge 1; $index--) {
    if ($segments[$index] -ieq "docs") {
      $candidateSegments = $segments[0..($index - 1)]
      $candidate = [System.IO.Path]::GetFullPath(($candidateSegments -join [System.IO.Path]::DirectorySeparatorChar))

      if (Test-ProjectRoot $candidate) {
        # docs 下任务目录只承载阶段文档；知识库必须按项目启动目录归属。
        return $candidate
      }
    }
  }

  return $fullPath
}

function Resolve-KbName {
  param([string]$WorkDir)

  Split-Path -Leaf $WorkDir
}

function Ensure-KbConfig {
  if (-not (Test-Path -LiteralPath $KbConfig)) {
    "{}" | Set-Content -LiteralPath $KbConfig -Encoding utf8
  }
}

function Read-KbMap {
  Ensure-KbConfig

  $raw = Get-Content -LiteralPath $KbConfig -Raw
  if ([string]::IsNullOrWhiteSpace($raw)) {
    return @{}
  }

  $raw = $raw.Trim()
  if ($raw -eq "{}") {
    return @{}
  }

  $obj = $raw | ConvertFrom-Json
  $map = @{}
  foreach ($prop in $obj.PSObject.Properties) {
    $map[$prop.Name] = [string]$prop.Value
  }
  return $map
}

function Write-KbMap {
  param([hashtable]$Map)

  $json = $Map | ConvertTo-Json -Depth 5 -Compress
  $json | Set-Content -LiteralPath $KbConfig -Encoding utf8
}

function Invoke-AiLocalBaseTool {
  param(
    [string]$ToolName,
    [hashtable]$ArgumentsMap
  )

  $uri = "$($script:MCP_API_BASE_URL)/tools/$ToolName/call"
  $body = @{ arguments = $ArgumentsMap } | ConvertTo-Json -Depth 8 -Compress
  Invoke-RestMethod -Uri $uri -Method Post -Headers $script:AuthHeader -ContentType "application/json" -Body $body
}

function Invoke-ToolsList {
  $body = @{
    jsonrpc = "2.0"
    id      = 1
    method  = "tools/list"
    params  = @{}
  } | ConvertTo-Json -Depth 8 -Compress

  Invoke-RestMethod -Uri $script:MCP_API_BASE_URL -Method Post -Headers $script:AuthHeader -ContentType "application/json" -Body $body
}

function Invoke-KnowledgeBaseList {
  Invoke-AiLocalBaseTool -ToolName "knowledge_base.list" -ArgumentsMap @{}
}

function Find-KbIdByName {
  param(
    [object]$ListResponse,
    [string]$Name
  )

  if (-not $ListResponse -or -not ($ListResponse.PSObject.Properties.Name -contains "structuredContent")) {
    return $null
  }
  if (-not $ListResponse.structuredContent -or -not ($ListResponse.structuredContent.PSObject.Properties.Name -contains "items")) {
    return $null
  }

  foreach ($item in @($ListResponse.structuredContent.items)) {
    if (-not $item) {
      continue
    }
    if ([string]$item.name -ne $Name) {
      continue
    }

    if ($item.PSObject.Properties.Name -contains "knowledgeBaseId" -and -not [string]::IsNullOrWhiteSpace([string]$item.knowledgeBaseId)) {
      return [string]$item.knowledgeBaseId
    }
    if ($item.PSObject.Properties.Name -contains "id" -and -not [string]::IsNullOrWhiteSpace([string]$item.id)) {
      return [string]$item.id
    }
  }

  return $null
}

function Prepare-Context {
  param([string]$WorkDirInput)

  Ensure-Requirements
  Load-EnvFile
  $resolvedWorkDir = Resolve-WorkDir $WorkDirInput
  $script:WorkDir = Resolve-ProjectWorkDir $resolvedWorkDir
  $script:KbName = Resolve-KbName $script:WorkDir
}

function Ensure-KbId {
  $map = Read-KbMap

  Write-Host "正在读取工具能力列表..."
  $toolsResponse = Invoke-ToolsList
  if (-not $toolsResponse) {
    throw "读取 tools/list 失败: 响应为空"
  }

  Write-Host "正在检索已有知识库..."
  $listResponse = Invoke-KnowledgeBaseList
  if (-not $listResponse) {
    throw "读取 knowledge_base.list 失败: 响应为空"
  }

  $matchedKbId = Find-KbIdByName -ListResponse $listResponse -Name $script:KbName
  if (-not [string]::IsNullOrWhiteSpace($matchedKbId)) {
    $script:KbId = $matchedKbId
    $map[$script:KbName] = $script:KbId
    Write-KbMap $map
    Write-Host "匹配到已有知识库: $($script:KbName) (ID: $($script:KbId))"
    return
  }

  Write-Host "未匹配到知识库名 $($script:KbName)，正在创建..."
  $response = Invoke-AiLocalBaseTool -ToolName "knowledge_base.create" -ArgumentsMap @{
    name        = $script:KbName
    description = "目录: $($script:WorkDir)"
  }

  $kbId = $null
  if ($response.PSObject.Properties.Name -contains "structuredContent" -and
      $response.structuredContent -and
      $response.structuredContent.PSObject.Properties.Name -contains "knowledgeBaseId") {
    $kbId = [string]$response.structuredContent.knowledgeBaseId
  }
  if (-not $kbId -and $response.PSObject.Properties.Name -contains "knowledgeBaseId") {
    $kbId = [string]$response.knowledgeBaseId
  }

  if ([string]::IsNullOrWhiteSpace($kbId)) {
    $responseJson = $response | ConvertTo-Json -Depth 10 -Compress
    throw "创建知识库失败: $responseJson"
  }

  $map[$script:KbName] = $kbId
  Write-KbMap $map

  $script:KbId = $kbId
  Write-Host "知识库创建成功: $($script:KbName) (ID: $kbId)"
}

function Invoke-Init {
  param([string]$WorkDirInput)

  Prepare-Context $WorkDirInput
  Ensure-KbId

  @{
    workDir         = $script:WorkDir
    knowledgeBaseName = $script:KbName
    knowledgeBaseId = $script:KbId
  } | ConvertTo-Json -Depth 5 -Compress
}

function Invoke-Tools {
  Ensure-Requirements
  Load-EnvFile
  Invoke-ToolsList | ConvertTo-Json -Depth 12
}

function Invoke-List {
  Ensure-Requirements
  Load-EnvFile
  Invoke-KnowledgeBaseList | ConvertTo-Json -Depth 12
}

function Invoke-Upload {
  param(
    [string]$Filename,
    [string]$Content,
    [string]$WorkDirInput
  )

  Prepare-Context $WorkDirInput
  Ensure-KbId
  Write-Host "上传文档到知识库: $($script:KbId)"

  $response = Invoke-AiLocalBaseTool -ToolName "document.upload" -ArgumentsMap @{
    knowledgeBaseId = $script:KbId
    filename        = $Filename
    content         = $Content
  }

  $response | ConvertTo-Json -Depth 10
}

function Invoke-Append {
  param(
    [string]$DocumentId,
    [string]$Content,
    [string]$WorkDirInput
  )

  if ([string]::IsNullOrWhiteSpace($DocumentId) -or [string]::IsNullOrWhiteSpace($Content)) {
    throw "错误: append 需要 [documentId] [内容] [目录]"
  }

  Prepare-Context $WorkDirInput
  Ensure-KbId
  Write-Host "追加文档到知识库: $($script:KbId) (文档: $DocumentId)"

  $response = Invoke-AiLocalBaseTool -ToolName "document.append" -ArgumentsMap @{
    knowledgeBaseId = $script:KbId
    documentId      = $DocumentId
    content         = $Content
  }

  $response | ConvertTo-Json -Depth 10
}

function Invoke-Update {
  param(
    [string]$DocumentId,
    [string]$Content,
    [string]$WorkDirInput
  )

  if ([string]::IsNullOrWhiteSpace($DocumentId) -or [string]::IsNullOrWhiteSpace($Content)) {
    throw "错误: update 需要 [documentId] [内容] [目录]"
  }

  Prepare-Context $WorkDirInput
  Ensure-KbId
  Write-Host "覆盖文档到知识库: $($script:KbId) (文档: $DocumentId)"

  $response = Invoke-AiLocalBaseTool -ToolName "document.update" -ArgumentsMap @{
    knowledgeBaseId = $script:KbId
    documentId      = $DocumentId
    content         = $Content
  }

  $response | ConvertTo-Json -Depth 10
}

function Invoke-Delete {
  param(
    [string]$DocumentId,
    [string]$WorkDirInput
  )

  if ([string]::IsNullOrWhiteSpace($DocumentId)) {
    throw "错误: delete 需要 [documentId] [目录]"
  }

  Prepare-Context $WorkDirInput
  Ensure-KbId
  Write-Host "删除文档: $($script:KbId) (文档: $DocumentId)"

  $response = Invoke-AiLocalBaseTool -ToolName "document.delete" -ArgumentsMap @{
    knowledgeBaseId = $script:KbId
    documentId      = $DocumentId
  }

  $response | ConvertTo-Json -Depth 10
}

function Invoke-Search {
  param(
    [string]$Query,
    [string]$WorkDirInput,
    [int]$TopK = 3
  )

  Prepare-Context $WorkDirInput
  Ensure-KbId
  Write-Host "检索: $Query (知识库: $($script:KbId))"

  $response = Invoke-AiLocalBaseTool -ToolName "knowledge_base.search" -ArgumentsMap @{
    knowledgeBaseId = $script:KbId
    query           = $Query
    topK            = $TopK
  }

  $response | ConvertTo-Json -Depth 10
}

function Invoke-Chat {
  param(
    [string]$Message,
    [string]$WorkDirInput
  )

  Prepare-Context $WorkDirInput
  Ensure-KbId
  Write-Host "问答: $Message (知识库: $($script:KbId))"

  $response = Invoke-AiLocalBaseTool -ToolName "chat.ask" -ArgumentsMap @{
    knowledgeBaseId = $script:KbId
    message         = $Message
  }

  $response | ConvertTo-Json -Depth 10
}

switch ($Action) {
  "init" {
    $targetDir = if ($Arguments.Count -ge 1) { $Arguments[0] } else { (Get-Location).ProviderPath }
    Invoke-Init $targetDir
    break
  }
  "tools" {
    Invoke-Tools
    break
  }
  "list" {
    Invoke-List
    break
  }
  "upload" {
    $filename = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "example.md" }
    $content = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "# 示例文档`n`n这是测试内容。" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    Invoke-Upload $filename $content $workDir
    break
  }
  "append" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $content = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    Invoke-Append $documentId $content $workDir
    break
  }
  "update" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $content = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    Invoke-Update $documentId $content $workDir
    break
  }
  "delete" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $workDir = if ($Arguments.Count -ge 2) { $Arguments[1] } else { (Get-Location).ProviderPath }
    Invoke-Delete $documentId $workDir
    break
  }
  "search" {
    $query = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "示例" }
    $workDir = if ($Arguments.Count -ge 2) { $Arguments[1] } else { (Get-Location).ProviderPath }
    $topK = if ($Arguments.Count -ge 3) { [int]$Arguments[2] } else { 3 }
    Invoke-Search $query $workDir $topK
    break
  }
  "chat" {
    $message = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "这是什么内容？" }
    $workDir = if ($Arguments.Count -ge 2) { $Arguments[1] } else { (Get-Location).ProviderPath }
    Invoke-Chat $message $workDir
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
