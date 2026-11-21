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
$ScreenShooterRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$ScreenShooterCacheLock = $null
$ScreenShooterOldTLS = [Net.ServicePointManager]::SecurityProtocol
try {
    Assert-ScreenShooterWindows
    $ScreenShooterManifest = Read-ScreenShooterWindowsDownloads $ScreenShooterRoot
    $ScreenShooterCache = Get-ScreenShooterWindowsCache $ScreenShooterRoot
    $ScreenShooterCacheLock = Open-ScreenShooterBuildLock (Join-Path $ScreenShooterCache '.runtime-downloads.lock') $ScreenShooterRoot
    $ScreenShooterArtifacts = @($ScreenShooterManifest.artifacts | Where-Object { $_.role -ne 'installer-tool' -or $IncludeInstaller })
    $ScreenShooterMissing = @($ScreenShooterArtifacts | Where-Object { -not (Test-Path -LiteralPath (Join-Path $ScreenShooterCache $_.filename) -PathType Leaf) })
    if ($Offline -and $ScreenShooterMissing.Count -gt 0) {
        throw ('В офлайн-кэше отсутствуют: ' + (($ScreenShooterMissing | ForEach-Object { $_.filename }) -join ', '))
    }
    $ScreenShooterNeeded = [long]268435456
    foreach ($ScreenShooterMissingArtifact in $ScreenShooterMissing) { $ScreenShooterNeeded += [long]$ScreenShooterMissingArtifact.bytes }
    $ScreenShooterDrive = [IO.DriveInfo]::new([IO.Path]::GetPathRoot($ScreenShooterCache))
    if ($ScreenShooterDrive.AvailableFreeSpace -lt $ScreenShooterNeeded) { throw "Недостаточно места: требуется $ScreenShooterNeeded байт с резервом." }
    [Net.ServicePointManager]::SecurityProtocol = $ScreenShooterOldTLS -bor [Net.SecurityProtocolType]::Tls12
    $ScreenShooterPrepared = @(foreach ($ScreenShooterArtifact in $ScreenShooterArtifacts) {
        $null = Receive-ScreenShooterArtifact $ScreenShooterArtifact $ScreenShooterCache $ScreenShooterRoot -Offline:$Offline
        [ordered]@{ id=$ScreenShooterArtifact.id; filename=$ScreenShooterArtifact.filename; bytes=$ScreenShooterArtifact.bytes; sha256=$ScreenShooterArtifact.sha256 }
    })
    $ScreenShooterReceipt = [ordered]@{
        schema = 'screenshorts-windows-download-receipt-v1'
        manifest_sha256 = (Get-FileHash -LiteralPath (Join-Path $ScreenShooterRoot 'config\windows-downloads.json') -Algorithm SHA256).Hash.ToLowerInvariant()
        prepared_utc = [DateTime]::UtcNow.ToString('o')
        include_installer = [bool]$IncludeInstaller
        artifacts = $ScreenShooterPrepared
    }
    $ScreenShooterReceiptPath = Assert-ScreenShooterOutputPath (Join-Path $ScreenShooterCache 'download-receipt.json') $ScreenShooterRoot
    $ScreenShooterReceiptTemporary = Assert-ScreenShooterOutputPath (Join-Path $ScreenShooterCache ('.receipt-' + [Guid]::NewGuid().ToString('N') + '.tmp')) $ScreenShooterRoot
    try {
        [IO.File]::WriteAllText($ScreenShooterReceiptTemporary, ($ScreenShooterReceipt | ConvertTo-Json -Depth 8) + [Environment]::NewLine, [Text.UTF8Encoding]::new($false))
        if (Test-Path -LiteralPath $ScreenShooterReceiptPath) { [IO.File]::Replace($ScreenShooterReceiptTemporary, $ScreenShooterReceiptPath, [NullString]::Value) }
        else { [IO.File]::Move($ScreenShooterReceiptTemporary, $ScreenShooterReceiptPath) }
    } finally {
        if (Test-Path -LiteralPath $ScreenShooterReceiptTemporary) {
            $null = Assert-ScreenShooterOutputPath $ScreenShooterReceiptTemporary $ScreenShooterRoot
            Remove-Item -LiteralPath $ScreenShooterReceiptTemporary -Force
        }
    }
    Write-Host "Закреплённые компоненты подготовлены: $ScreenShooterCache"
    if ($Extract) {
        $ScreenShooterComponents = New-ScreenShooterRuntimeComponents $ScreenShooterManifest $ScreenShooterCache $ScreenShooterRoot
        Write-Host "Распакованы компоненты приложения: $ScreenShooterComponents"
        Write-Host 'Работал только закреплённый распаковщик. Установщики, приложение, Python, тесты, OCR и модели не запускались.'
    } else { Write-Host 'Подготовлены только файлы: установщики, архиваторы, приложение, тесты, OCR и модели не запускались.' }
    exit 0
} catch {
    Write-Error $_ -ErrorAction Continue
    exit 1
} finally {
    [Net.ServicePointManager]::SecurityProtocol = $ScreenShooterOldTLS
    if ($ScreenShooterCacheLock) { $ScreenShooterCacheLock.Dispose() }
}
