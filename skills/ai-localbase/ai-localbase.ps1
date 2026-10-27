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
$KbConfigLock = Join-Path $ScriptDir "knowledge.json.lock"

function Show-Usage {
  @"
用法:
  ./ai-localbase.ps1 init [目录]
  ./ai-localbase.ps1 tools
  ./ai-localbase.ps1 call [工具名] [arguments JSON]
  ./ai-localbase.ps1 list
  ./ai-localbase.ps1 upload [文件名] [内容] [目录]
  ./ai-localbase.ps1 upload-file [文件名] [内容文件] [目录]
  ./ai-localbase.ps1 append [documentId] [内容] [目录]
  ./ai-localbase.ps1 append-file [documentId] [内容文件] [目录]
  ./ai-localbase.ps1 update [documentId] [内容] [目录]
  ./ai-localbase.ps1 update-file [documentId] [内容文件] [目录]
  ./ai-localbase.ps1 delete [documentId] [目录]
  ./ai-localbase.ps1 search [关键词] [目录] [topK]
  ./ai-localbase.ps1 chat [问题] [目录]

说明:
  - init: 初始化当前目录对应的知识库映射并输出摘要 JSON
  - tools: 通过 tools/list 列出当前可用工具能力、调用方式、参数和响应字段
  - call: 通过工具名和 arguments JSON 调用任意已发现 MCP 工具
  - list: 通过 knowledge_base.list 列出现有知识库名称和知识库 ID
  - upload: 上传文本内容到知识库；内容参数可写成 @文件路径 或 file:文件路径 以读取文件
  - upload-file: 从文件读取内容并上传，避免 PowerShell 转义多行 Markdown
  - append: 向已有文档追加文本内容；内容参数可写成 @文件路径 或 file:文件路径 以读取文件
  - append-file: 从文件读取内容并追加，避免 PowerShell 转义多行 Markdown
  - update: 用新内容覆盖已有文档；内容参数可写成 @文件路径 或 file:文件路径 以读取文件
  - update-file: 从文件读取内容并覆盖，避免 PowerShell 转义多行 Markdown
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

function Read-TextFileArgument {
  param(
    [string]$Path,
    [string]$ParameterName = "内容文件"
  )

  if ([string]::IsNullOrWhiteSpace($Path)) {
    throw "错误: $ParameterName 不能为空"
  }

  $resolvedPath = Resolve-WorkDir $Path
  if (-not (Test-Path -LiteralPath $resolvedPath -PathType Leaf)) {
    throw "错误: $ParameterName 不存在: $resolvedPath"
  }

  # 内容从文件读取，避免 PowerShell 命令行参数中的反引号、引号和换行被重新解析。
  return Get-Content -LiteralPath $resolvedPath -Raw -Encoding UTF8
}

function Resolve-ContentArgument {
  param(
    [string]$Content,
    [string]$ParameterName = "内容"
  )

  if ([string]::IsNullOrWhiteSpace($Content)) {
    return $Content
  }

  if ($Content.StartsWith("@") -and $Content.Length -gt 1) {
    return Read-TextFileArgument -Path $Content.Substring(1) -ParameterName "$ParameterName 文件"
  }

  if ($Content.StartsWith("file:") -and $Content.Length -gt 5) {
    return Read-TextFileArgument -Path $Content.Substring(5) -ParameterName "$ParameterName 文件"
  }

  return $Content
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

  try {
    $obj = $raw | ConvertFrom-Json
  } catch {
    $stamp = Get-Date -Format "yyyyMMddHHmmss"
    $backupPath = "$KbConfig.corrupt.$stamp"
    Copy-Item -LiteralPath $KbConfig -Destination $backupPath -Force
    throw "knowledge.json 解析失败，已备份到 $backupPath。请先修复缓存文件后重试。原始错误: $($_.Exception.Message)"
  }
  $map = @{}
  foreach ($prop in $obj.PSObject.Properties) {
    $map[$prop.Name] = $prop.Value
  }
  return $map
}

function Invoke-WithKbConfigLock {
  param([scriptblock]$Body)

  $lockStream = $null
  try {
    for ($i = 0; $i -lt 50; $i++) {
      try {
        $lockStream = [System.IO.File]::Open(
          $KbConfigLock,
          [System.IO.FileMode]::OpenOrCreate,
          [System.IO.FileAccess]::ReadWrite,
          [System.IO.FileShare]::None
        )
        break
      } catch [System.IO.IOException] {
        Start-Sleep -Milliseconds 100
      }
    }

    if (-not $lockStream) {
      throw "获取 knowledge.json 写锁超时"
    }

    & $Body
  } finally {
    if ($lockStream) {
      $lockStream.Dispose()
    }
  }
}

function Write-KbMap {
  param([hashtable]$Map)

  Invoke-WithKbConfigLock {
    $json = $Map | ConvertTo-Json -Depth 12 -Compress
    $tempPath = "$KbConfig.tmp.$PID"
    $backupPath = "$KbConfig.bak"
    [System.IO.File]::WriteAllText($tempPath, $json, [System.Text.UTF8Encoding]::new($false))

    if (Test-Path -LiteralPath $KbConfig) {
      [System.IO.File]::Replace($tempPath, $KbConfig, $backupPath, $true)
    } else {
      Move-Item -LiteralPath $tempPath -Destination $KbConfig -Force
    }
  }
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

function Get-KbIdFromItem {
  param([object]$Item)

  if (-not $Item) {
    return $null
  }
  if ($Item.PSObject.Properties.Name -contains "knowledgeBaseId" -and -not [string]::IsNullOrWhiteSpace([string]$Item.knowledgeBaseId)) {
    return [string]$Item.knowledgeBaseId
  }
  if ($Item.PSObject.Properties.Name -contains "id" -and -not [string]::IsNullOrWhiteSpace([string]$Item.id)) {
    return [string]$Item.id
  }
  return $null
}

function Test-KbItemMatchesProject {
  param(
    [object]$Item,
    [string]$Name,
    [string]$WorkDir
  )

  $itemName = [string]$Item.name
  if ($itemName -eq $Name) {
    return "exact-name"
  }
  if ($itemName.StartsWith("$Name.") -or $itemName.StartsWith("$Name-")) {
    return "name-prefix"
  }

  $description = [string]$Item.description
  if (-not [string]::IsNullOrWhiteSpace($description)) {
    $descriptionPath = $description.Trim()
    if ($descriptionPath.StartsWith("目录:")) {
      $descriptionPath = $descriptionPath.Substring(3).Trim()
    }

    try {
      $normalizedDescriptionPath = [System.IO.Path]::GetFullPath($descriptionPath).TrimEnd("\", "/").Replace("/", "\").ToLowerInvariant()
      $normalizedWorkDir = [System.IO.Path]::GetFullPath($WorkDir).TrimEnd("\", "/").Replace("/", "\").ToLowerInvariant()
      if ($normalizedDescriptionPath -eq $normalizedWorkDir) {
        return "description-path"
      }
    } catch {
      # description 可能是普通说明文本；无法解析为路径时不参与目录匹配。
    }
  }

  return $null
}

function Find-KbBindingByName {
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

  $matches = @()
  foreach ($item in @($ListResponse.structuredContent.items)) {
    if (-not $item) {
      continue
    }
    $kbId = Get-KbIdFromItem $item
    if ([string]::IsNullOrWhiteSpace($kbId)) {
      continue
    }

    $reason = Test-KbItemMatchesProject -Item $item -Name $Name -WorkDir $script:WorkDir
    if ([string]::IsNullOrWhiteSpace($reason)) {
      continue
    }

    $docCount = 0
    if ($item.PSObject.Properties.Name -contains "documentCount") {
      $docCount = [int]$item.documentCount
    }
    $matches += [pscustomobject]@{
      id            = $kbId
      name          = [string]$item.name
      documentCount = $docCount
      matchReason   = $reason
    }
  }

  if ($matches.Count -eq 0) {
    return $null
  }

  $primary = $matches | Where-Object { $_.name -eq $Name } | Select-Object -First 1
  if (-not $primary) {
    $primary = $matches | Sort-Object -Property @{ Expression = "documentCount"; Descending = $true }, name | Select-Object -First 1
  }

  $ids = @($matches | Select-Object -ExpandProperty id -Unique)
  return [pscustomobject]@{
    primaryId          = [string]$primary.id
    knowledgeBaseIds   = $ids
    boundKnowledgeBases = @($matches)
  }
}

function Convert-KbBindingForCache {
  param([object]$Binding)

  @{
    primaryId          = [string]$Binding.primaryId
    knowledgeBaseIds   = @($Binding.knowledgeBaseIds)
    boundKnowledgeBases = @($Binding.boundKnowledgeBases)
    updatedAt          = (Get-Date).ToUniversalTime().ToString("o")
  }
}

function Write-KbBindingToMap {
  param(
    [hashtable]$Map,
    [string]$Name,
    [object]$Binding
  )

  # 顶层仍写旧格式字符串，兼容只读取 map[项目名] 的历史脚本。
  $Map[$Name] = [string]$Binding.primaryId

  $bindings = @{}
  if ($Map.ContainsKey("_bindings") -and $Map["_bindings"]) {
    if ($Map["_bindings"] -is [System.Collections.IDictionary]) {
      foreach ($key in $Map["_bindings"].Keys) {
        $bindings[[string]$key] = $Map["_bindings"][$key]
      }
    } else {
      foreach ($prop in $Map["_bindings"].PSObject.Properties) {
        $bindings[$prop.Name] = $prop.Value
      }
    }
  }
  $bindings[$Name] = Convert-KbBindingForCache $Binding
  $Map["_bindings"] = $bindings
}

function Set-CurrentKbBinding {
  param([object]$Binding)

  $script:KbId = [string]$Binding.primaryId
  $script:KbIds = @($Binding.knowledgeBaseIds)
  $script:BoundKnowledgeBases = @($Binding.boundKnowledgeBases)
}

function Invoke-SearchAcrossBoundKnowledgeBases {
  param(
    [string]$Query,
    [int]$TopK
  )

  $items = @()
  $responses = @()
  foreach ($kbId in @($script:KbIds)) {
    $response = Invoke-AiLocalBaseTool -ToolName "knowledge_base.search" -ArgumentsMap @{
      knowledgeBaseId = $kbId
      query           = $Query
      topK            = $TopK
    }
    $responses += $response
    if ($response.PSObject.Properties.Name -contains "structuredContent" -and
        $response.structuredContent -and
        $response.structuredContent.PSObject.Properties.Name -contains "items") {
      $items += @($response.structuredContent.items)
    }
  }

  @{
    content = @(@{ type = "text"; text = "共检索到 $($items.Count) 条结果，覆盖 $($script:KbIds.Count) 个知识库" })
    isError = $false
    name = "knowledge_base.search.multi"
    structuredContent = @{
      knowledgeBaseIds = @($script:KbIds)
      primaryKnowledgeBaseId = $script:KbId
      boundKnowledgeBases = @($script:BoundKnowledgeBases)
      items = @($items | Sort-Object -Property @{ Expression = "score"; Descending = $true })
      responses = @($responses)
    }
  }
}

function Invoke-ChatAcrossBoundKnowledgeBases {
  param([string]$Message)

  if (@($script:KbIds).Count -le 1) {
    return Invoke-AiLocalBaseTool -ToolName "chat.ask" -ArgumentsMap @{
      knowledgeBaseId = $script:KbId
      message         = $Message
    }
  }

  $items = @()
  foreach ($kbId in @($script:KbIds)) {
    $response = Invoke-AiLocalBaseTool -ToolName "chat.ask" -ArgumentsMap @{
      knowledgeBaseId = $kbId
      message         = $Message
    }
    $kbName = (@($script:BoundKnowledgeBases) | Where-Object { $_.id -eq $kbId } | Select-Object -First 1).name
    $items += [pscustomobject]@{
      knowledgeBaseId = $kbId
      knowledgeBaseName = $kbName
      response = $response
    }
  }

  @{
    content = @(@{ type = "text"; text = "共返回 $($items.Count) 个知识库问答结果" })
    isError = $false
    name = "chat.ask.multi"
    structuredContent = @{
      knowledgeBaseIds = @($script:KbIds)
      primaryKnowledgeBaseId = $script:KbId
      boundKnowledgeBases = @($script:BoundKnowledgeBases)
      items = @($items)
    }
  }
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

  $binding = Find-KbBindingByName -ListResponse $listResponse -Name $script:KbName
  if ($binding) {
    Set-CurrentKbBinding $binding
    Write-KbBindingToMap -Map $map -Name $script:KbName -Binding $binding
    Write-KbMap $map
    Write-Host "匹配到已有知识库: $($script:KbName) (主ID: $($script:KbId)，绑定: $(@($script:KbIds).Count) 个)"
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

  $binding = [pscustomobject]@{
    primaryId          = $kbId
    knowledgeBaseIds   = @($kbId)
    boundKnowledgeBases = @([pscustomobject]@{
      id            = $kbId
      name          = $script:KbName
      documentCount = 0
      matchReason   = "created"
    })
  }
  Set-CurrentKbBinding $binding
  Write-KbBindingToMap -Map $map -Name $script:KbName -Binding $binding
  Write-KbMap $map

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
    knowledgeBaseIds = @($script:KbIds)
    boundKnowledgeBases = @($script:BoundKnowledgeBases)
  } | ConvertTo-Json -Depth 5 -Compress
}

function Invoke-Tools {
  Ensure-Requirements
  Load-EnvFile
  Invoke-ToolsList | ConvertTo-Json -Depth 12
}

function Invoke-Call {
  param(
    [string]$ToolName,
    [string]$ArgumentsJson
  )

  if ([string]::IsNullOrWhiteSpace($ToolName)) {
    throw "错误: call 需要 [工具名] [arguments JSON]"
  }
  if ($ToolName -notmatch '^[A-Za-z0-9._-]+$') {
    throw "错误: 工具名只能包含字母、数字、点、下划线和连字符"
  }
  if ([string]::IsNullOrWhiteSpace($ArgumentsJson)) {
    $ArgumentsJson = "{}"
  }

  try {
    $parsedArguments = $ArgumentsJson | ConvertFrom-Json
  } catch {
    throw "错误: arguments JSON 无效: $($_.Exception.Message)"
  }
  if ($null -eq $parsedArguments -or $parsedArguments -is [System.Array] -or $parsedArguments -is [System.ValueType] -or $parsedArguments -is [string]) {
    throw "错误: arguments JSON 必须是对象"
  }

  $argumentsMap = @{}
  foreach ($property in $parsedArguments.PSObject.Properties) {
    $argumentsMap[$property.Name] = $property.Value
  }

  Ensure-Requirements
  Load-EnvFile
  Invoke-AiLocalBaseTool -ToolName $ToolName -ArgumentsMap $argumentsMap | ConvertTo-Json -Depth 12
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
  Write-Host "检索: $Query (主知识库: $($script:KbId)，绑定: $(@($script:KbIds).Count) 个)"

  $response = Invoke-SearchAcrossBoundKnowledgeBases -Query $Query -TopK $TopK

  $response | ConvertTo-Json -Depth 10
}

function Invoke-Chat {
  param(
    [string]$Message,
    [string]$WorkDirInput
  )

  Prepare-Context $WorkDirInput
  Ensure-KbId
  Write-Host "问答: $Message (主知识库: $($script:KbId)，绑定: $(@($script:KbIds).Count) 个)"

  $response = Invoke-ChatAcrossBoundKnowledgeBases -Message $Message

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
  "call" {
    $toolName = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $argumentsJson = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "{}" }
    Invoke-Call $toolName $argumentsJson
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
    $content = Resolve-ContentArgument -Content $content -ParameterName "upload 内容"
    Invoke-Upload $filename $content $workDir
    break
  }
  "upload-file" {
    $filename = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $contentFile = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    if ([string]::IsNullOrWhiteSpace($filename)) {
      throw "错误: upload-file 需要 [文件名] [内容文件] [目录]"
    }
    $content = Read-TextFileArgument -Path $contentFile -ParameterName "upload-file 内容文件"
    Invoke-Upload $filename $content $workDir
    break
  }
  "append" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $content = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    $content = Resolve-ContentArgument -Content $content -ParameterName "append 内容"
    Invoke-Append $documentId $content $workDir
    break
  }
  "append-file" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $contentFile = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    $content = Read-TextFileArgument -Path $contentFile -ParameterName "append-file 内容文件"
    Invoke-Append $documentId $content $workDir
    break
  }
  "update" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $content = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    $content = Resolve-ContentArgument -Content $content -ParameterName "update 内容"
    Invoke-Update $documentId $content $workDir
    break
  }
  "update-file" {
    $documentId = if ($Arguments.Count -ge 1) { $Arguments[0] } else { "" }
    $contentFile = if ($Arguments.Count -ge 2) { $Arguments[1] } else { "" }
    $workDir = if ($Arguments.Count -ge 3) { $Arguments[2] } else { (Get-Location).ProviderPath }
    $content = Read-TextFileArgument -Path $contentFile -ParameterName "update-file 内容文件"
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
