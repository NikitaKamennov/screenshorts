# Content preparation only. This module never executes downloaded files.

function Read-ScreenShortsWindowsDownloads {
    param([string]$ProjectRoot)
    $taskPath = Assert-ScreenShortsOutputPath (Join-Path $ProjectRoot 'config\windows-downloads.json') $ProjectRoot
    if ((Get-Item -LiteralPath $taskPath).Length -gt 262144) { throw 'Windows download manifest превышает 256 KiB.' }
    $taskManifest = Get-Content -LiteralPath $taskPath -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($taskManifest.schema -ne 'screenshorts-windows-downloads-v1' -or $taskManifest.architecture -ne 'amd64') {
        throw 'Неподдержанный Windows download manifest.'
    }
    if (@($taskManifest.artifacts).Count -lt 1 -or @($taskManifest.artifacts).Count -gt 64) { throw 'Некорректное число Windows-компонентов.' }
    $taskIDs = @{}
    $taskNames = @{}
    foreach ($taskArtifact in $taskManifest.artifacts) {
        if ($taskArtifact.id -notmatch '^[a-z][a-z0-9-]{1,63}$' -or $taskIDs.ContainsKey($taskArtifact.id)) { throw 'Повторяющийся/некорректный ID Windows-компонента.' }
        if ($taskArtifact.filename -notmatch '^[A-Za-z0-9][A-Za-z0-9_.-]{0,159}$' -or $taskNames.ContainsKey($taskArtifact.filename) -or
            [IO.Path]::GetFileNameWithoutExtension($taskArtifact.filename) -match '^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$') {
            throw "Некорректное имя артефакта: $($taskArtifact.filename)"
        }
        if ($taskArtifact.sha256 -cnotmatch '^[0-9a-f]{64}$' -or [long]$taskArtifact.bytes -le 0 -or [long]$taskArtifact.bytes -gt 1073741824) {
            throw "Нужны точные размер и SHA-256 компонента $($taskArtifact.id)."
        }
        if ($taskArtifact.role -notin @('runtime', 'build-tool', 'installer-tool', 'notice')) { throw "Некорректная роль компонента $($taskArtifact.id)." }
        $taskURI = [Uri]$taskArtifact.url
        if (-not $taskURI.IsAbsoluteUri -or $taskURI.Scheme -ne 'https' -or $taskURI.UserInfo -or $taskURI.Fragment) { throw "Нужен закреплённый HTTPS URL: $($taskArtifact.id)" }
        Assert-ScreenShortsDownloadHost $taskURI
        if (-not $taskArtifact.version -or -not $taskArtifact.source -or -not $taskArtifact.license) { throw "Нет версии/источника/лицензии: $($taskArtifact.id)" }
        $taskIDs[$taskArtifact.id] = $true
        $taskNames[$taskArtifact.filename] = $true
    }
    $taskBackgroundPath = Assert-ScreenShortsOutputPath (Join-Path $ProjectRoot 'workers\background\runtime-manifest.json') $ProjectRoot
    $taskRequirementsPath = Assert-ScreenShortsOutputPath (Join-Path $ProjectRoot 'workers\background\requirements.lock') $ProjectRoot
    if ($taskManifest.source_locks.text_encoding -ne 'utf-8-lf' -or
        (Get-ScreenShortsSourceTextHash $taskBackgroundPath) -ne $taskManifest.source_locks.background_manifest_sha256 -or
        (Get-ScreenShortsSourceTextHash $taskRequirementsPath) -ne $taskManifest.source_locks.background_requirements_sha256) {
        throw 'Windows-комплект не соответствует текущему background manifest/hash-lock; требуется явное обновление manifest.'
    }
    return $taskManifest
}

function Get-ScreenShortsSourceTextHash {
    param([string]$Path)
    $taskText = [IO.File]::ReadAllText($Path).Replace("`r`n", "`n").Replace("`r", "`n")
    $taskHash = [Security.Cryptography.SHA256]::Create()
    try { return ([BitConverter]::ToString($taskHash.ComputeHash([Text.Encoding]::UTF8.GetBytes($taskText)))).Replace('-', '').ToLowerInvariant() }
    finally { $taskHash.Dispose() }
}

function Assert-ScreenShortsDownloadHost {
    param([Uri]$URI)
    $taskAllowed = @('www.python.org', 'raw.githubusercontent.com', 'github.com',
        'release-assets.githubusercontent.com', 'objects.githubusercontent.com',
        'files.pythonhosted.org', 'download.visualstudio.microsoft.com',
        'msedge.sf.dl.delivery.mp.microsoft.com')
    if ($URI.Scheme -ne 'https' -or $URI.DnsSafeHost -notin $taskAllowed) { throw "Неожиданный источник компонента: $($URI.Host)" }
}

function Assert-ScreenShortsCachedArtifact {
    param($Artifact, [string]$CacheDirectory, [string]$ProjectRoot)
    $taskPath = Assert-ScreenShortsOutputPath (Join-Path $CacheDirectory $Artifact.filename) $ProjectRoot
    if (-not (Test-Path -LiteralPath $taskPath -PathType Leaf)) { throw "В кэше нет $($Artifact.filename). Выполни явную подготовку компонентов с сетью." }
    $taskInfo = Get-Item -LiteralPath $taskPath -Force
    if ($taskInfo.Length -ne [long]$Artifact.bytes -or
        (Get-FileHash -LiteralPath $taskPath -Algorithm SHA256).Hash.ToLowerInvariant() -ne $Artifact.sha256) {
        throw "Размер/SHA-256 не совпал: $taskPath. Файл сохранён; неподходящий кэш не используется и не заменяется автоматически."
    }
    return $taskPath
}

function Receive-ScreenShortsArtifact {
    param($Artifact, [string]$CacheDirectory, [string]$ProjectRoot, [switch]$Offline)
    $taskDestination = Assert-ScreenShortsOutputPath (Join-Path $CacheDirectory $Artifact.filename) $ProjectRoot
    if (Test-Path -LiteralPath $taskDestination) { return Assert-ScreenShortsCachedArtifact $Artifact $CacheDirectory $ProjectRoot }
    if ($Offline) { throw "Офлайн-подготовка: отсутствует $($Artifact.filename). Сетевой fallback отключён." }
    $taskTemporary = Assert-ScreenShortsOutputPath (Join-Path $CacheDirectory ('.incoming-' + $Artifact.id + '-' + [Guid]::NewGuid().ToString('N') + '.part')) $ProjectRoot
    $taskResponse = $null
    $taskSource = $null
    $taskTarget = $null
    try {
        Write-Host "Загрузка компонента: $($Artifact.filename) ($($Artifact.bytes) байт)"
        $taskRequest = [Net.HttpWebRequest]::Create([Uri]$Artifact.url)
        $taskRequest.UserAgent = 'ScreenShortsPro-Windows-runtime-preparation/1'
        $taskRequest.Timeout = 30000
        $taskRequest.ReadWriteTimeout = 30000
        $taskRequest.MaximumAutomaticRedirections = 5
        $taskResponse = $taskRequest.GetResponse()
        Assert-ScreenShortsDownloadHost $taskResponse.ResponseUri
        if ($taskResponse.ContentLength -ge 0 -and $taskResponse.ContentLength -ne [long]$Artifact.bytes) {
            throw "Источник изменил размер $($Artifact.id); загрузка остановлена."
        }
        $taskSource = $taskResponse.GetResponseStream()
        $taskTarget = [IO.File]::Open($taskTemporary, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        $taskBuffer = [byte[]]::new(1048576)
        $taskTotal = [long]0
        $taskWatch = [Diagnostics.Stopwatch]::StartNew()
        while (($taskRead = $taskSource.Read($taskBuffer, 0, $taskBuffer.Length)) -gt 0) {
            $taskTotal += $taskRead
            if ($taskTotal -gt [long]$Artifact.bytes -or $taskWatch.Elapsed.TotalMinutes -gt 10) { throw 'Превышен закреплённый размер или лимит 10 минут загрузки.' }
            $taskTarget.Write($taskBuffer, 0, $taskRead)
        }
        $taskTarget.Flush($true)
        $taskTarget.Dispose()
        $taskTarget = $null
        if ($taskTotal -ne [long]$Artifact.bytes -or
            (Get-FileHash -LiteralPath $taskTemporary -Algorithm SHA256).Hash.ToLowerInvariant() -ne $Artifact.sha256) {
            throw "Не совпал размер/SHA-256 загруженного $($Artifact.id); файл не опубликован."
        }
        $null = Assert-ScreenShortsOutputPath $taskDestination $ProjectRoot
        Move-Item -LiteralPath $taskTemporary -Destination $taskDestination
        return $taskDestination
    } finally {
        if ($taskTarget) { $taskTarget.Dispose() }
        if ($taskSource) { $taskSource.Dispose() }
        if ($taskResponse) { $taskResponse.Dispose() }
        if (Test-Path -LiteralPath $taskTemporary) {
            $null = Assert-ScreenShortsOutputPath $taskTemporary $ProjectRoot
            Remove-Item -LiteralPath $taskTemporary -Force
        }
    }
}

function Get-ScreenShortsWindowsCache {
    param([string]$ProjectRoot)
    $taskCache = Join-Path $ProjectRoot '.local-data\windows-artifact-cache'
    if (Test-Path -LiteralPath $taskCache) {
        return Assert-ScreenShortsOwnedOutput $taskCache $ProjectRoot 'runtime-downloads'
    }
    return New-ScreenShortsOwnedOutput $taskCache $ProjectRoot 'runtime-downloads'
}
