[CmdletBinding()]
param(
    [switch]$Offline,
    [switch]$IncludeInstaller,
    [switch]$Extract
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot 'windows\common.ps1')
. (Join-Path $PSScriptRoot 'windows\downloads.ps1')
. (Join-Path $PSScriptRoot 'windows\archives.ps1')
. (Join-Path $PSScriptRoot 'windows\components.ps1')
. (Join-Path $PSScriptRoot 'windows\notices.ps1')
$ScreenShortsRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$ScreenShortsCacheLock = $null
$ScreenShortsOldTLS = [Net.ServicePointManager]::SecurityProtocol
try {
    Assert-ScreenShortsWindows
    $ScreenShortsManifest = Read-ScreenShortsWindowsDownloads $ScreenShortsRoot
    $ScreenShortsCache = Get-ScreenShortsWindowsCache $ScreenShortsRoot
    $ScreenShortsCacheLock = Open-ScreenShortsBuildLock (Join-Path $ScreenShortsCache '.runtime-downloads.lock') $ScreenShortsRoot
    $ScreenShortsArtifacts = @($ScreenShortsManifest.artifacts | Where-Object { $_.role -ne 'installer-tool' -or $IncludeInstaller })
    $ScreenShortsMissing = @($ScreenShortsArtifacts | Where-Object { -not (Test-Path -LiteralPath (Join-Path $ScreenShortsCache $_.filename) -PathType Leaf) })
    if ($Offline -and $ScreenShortsMissing.Count -gt 0) {
        throw ('В офлайн-кэше отсутствуют: ' + (($ScreenShortsMissing | ForEach-Object { $_.filename }) -join ', '))
    }
    $ScreenShortsNeeded = [long]268435456
    foreach ($ScreenShortsMissingArtifact in $ScreenShortsMissing) { $ScreenShortsNeeded += [long]$ScreenShortsMissingArtifact.bytes }
    $ScreenShortsDrive = [IO.DriveInfo]::new([IO.Path]::GetPathRoot($ScreenShortsCache))
    if ($ScreenShortsDrive.AvailableFreeSpace -lt $ScreenShortsNeeded) { throw "Недостаточно места: требуется $ScreenShortsNeeded байт с резервом." }
    [Net.ServicePointManager]::SecurityProtocol = $ScreenShortsOldTLS -bor [Net.SecurityProtocolType]::Tls12
    $ScreenShortsPrepared = @(foreach ($ScreenShortsArtifact in $ScreenShortsArtifacts) {
        $null = Receive-ScreenShortsArtifact $ScreenShortsArtifact $ScreenShortsCache $ScreenShortsRoot -Offline:$Offline
        [ordered]@{ id=$ScreenShortsArtifact.id; filename=$ScreenShortsArtifact.filename; bytes=$ScreenShortsArtifact.bytes; sha256=$ScreenShortsArtifact.sha256 }
    })
    $ScreenShortsReceipt = [ordered]@{
        schema = 'screenshorts-windows-download-receipt-v1'
        manifest_sha256 = (Get-FileHash -LiteralPath (Join-Path $ScreenShortsRoot 'config\windows-downloads.json') -Algorithm SHA256).Hash.ToLowerInvariant()
        prepared_utc = [DateTime]::UtcNow.ToString('o')
        include_installer = [bool]$IncludeInstaller
        artifacts = $ScreenShortsPrepared
    }
    $ScreenShortsReceiptPath = Assert-ScreenShortsOutputPath (Join-Path $ScreenShortsCache 'download-receipt.json') $ScreenShortsRoot
    $ScreenShortsReceiptTemporary = Assert-ScreenShortsOutputPath (Join-Path $ScreenShortsCache ('.receipt-' + [Guid]::NewGuid().ToString('N') + '.tmp')) $ScreenShortsRoot
    try {
        [IO.File]::WriteAllText($ScreenShortsReceiptTemporary, ($ScreenShortsReceipt | ConvertTo-Json -Depth 8) + [Environment]::NewLine, [Text.UTF8Encoding]::new($false))
        if (Test-Path -LiteralPath $ScreenShortsReceiptPath) { [IO.File]::Replace($ScreenShortsReceiptTemporary, $ScreenShortsReceiptPath, [NullString]::Value) }
        else { [IO.File]::Move($ScreenShortsReceiptTemporary, $ScreenShortsReceiptPath) }
    } finally {
        if (Test-Path -LiteralPath $ScreenShortsReceiptTemporary) {
            $null = Assert-ScreenShortsOutputPath $ScreenShortsReceiptTemporary $ScreenShortsRoot
            Remove-Item -LiteralPath $ScreenShortsReceiptTemporary -Force
        }
    }
    Write-Host "Закреплённые компоненты подготовлены: $ScreenShortsCache"
    if ($Extract) {
        $ScreenShortsComponents = New-ScreenShortsRuntimeComponents $ScreenShortsManifest $ScreenShortsCache $ScreenShortsRoot
        Write-Host "Распакованы компоненты приложения: $ScreenShortsComponents"
        Write-Host 'Работал только закреплённый распаковщик. Установщики, приложение, Python, тесты, OCR и модели не запускались.'
    } else { Write-Host 'Подготовлены только файлы: установщики, архиваторы, приложение, тесты, OCR и модели не запускались.' }
    exit 0
} catch {
    Write-Error $_ -ErrorAction Continue
    exit 1
} finally {
    [Net.ServicePointManager]::SecurityProtocol = $ScreenShortsOldTLS
    if ($ScreenShortsCacheLock) { $ScreenShortsCacheLock.Dispose() }
}
