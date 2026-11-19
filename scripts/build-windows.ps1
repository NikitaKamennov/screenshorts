[CmdletBinding()]
param([switch]$Bundle, [switch]$Portable, [switch]$Release, [switch]$Offline, [string]$OutputTag = '')

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot 'windows\common.ps1')
. (Join-Path $PSScriptRoot 'windows\go-build.ps1')

if (([int]$Bundle.IsPresent + [int]$Portable.IsPresent + [int]$Release.IsPresent) -gt 1) { throw 'Выбери один режим: -Bundle, -Portable или -Release.' }
if ($OutputTag -and (-not ($Bundle -or $Portable -or $Release) -or $OutputTag -cnotmatch '^[a-z0-9][a-z0-9-]{0,47}$')) { throw '-OutputTag допускает строчные латинские буквы, цифры и дефис, только в полном режиме сборки.' }
if ($Bundle -or $Portable -or $Release) {
    try {
        Assert-ScreenShortsWindows
        . (Join-Path $PSScriptRoot 'windows\downloads.ps1')
        . (Join-Path $PSScriptRoot 'windows\archives.ps1')
        . (Join-Path $PSScriptRoot 'windows\components.ps1')
        . (Join-Path $PSScriptRoot 'windows\notices.ps1')
        . (Join-Path $PSScriptRoot 'windows\package.ps1')
        . (Join-Path $PSScriptRoot 'windows\installer.ps1')
        New-ScreenShortsWindowsPackage ([IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))) -Portable:$Portable -Release:$Release -Offline:$Offline -OutputTag $OutputTag
        exit 0
    } catch { Write-Error $_ -ErrorAction Continue; exit 1 }
}

$ScreenShortsBuildLock = $null
$ScreenShortsStage = $null
$ScreenShortsBackup = $null
$ScreenShortsRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$ScreenShortsBuildRoot = Join-Path $ScreenShortsRoot '.local-data\build'
$ScreenShortsOutput = Join-Path $ScreenShortsBuildRoot 'windows-x64'
$ScreenShortsExe = Join-Path $ScreenShortsOutput 'ScreenShortsPro.exe'
try {
    Assert-ScreenShortsWindows
    $ScreenShortsGo = Get-Command go -CommandType Application -ErrorAction SilentlyContinue
    if (-not $ScreenShortsGo) { throw 'Установи Go 1.25 или новее на машине сборки; смотри README.md.' }
    $null = New-ScreenShortsDirectory $ScreenShortsBuildRoot $ScreenShortsRoot
    $ScreenShortsBuildLock = Open-ScreenShortsBuildLock (Join-Path $ScreenShortsBuildRoot 'windows-x64.lock') $ScreenShortsRoot
    if (Test-Path -LiteralPath $ScreenShortsOutput) {
        Assert-ScreenShortsDevOutput $ScreenShortsOutput $ScreenShortsRoot
        Assert-ScreenShortsExecutableStopped $ScreenShortsExe
    }
    $ScreenShortsStage = New-ScreenShortsOwnedOutput (Join-Path $ScreenShortsBuildRoot ('.windows-x64-stage-' + [Guid]::NewGuid().ToString('N'))) $ScreenShortsRoot 'dev'
    $ScreenShortsStageExe = Join-Path $ScreenShortsStage 'ScreenShortsPro.exe'
    Invoke-ScreenShortsGoBuild $ScreenShortsRoot $ScreenShortsStageExe -Offline:$Offline
    $ScreenShortsConfig = New-ScreenShortsDirectory (Join-Path $ScreenShortsStage 'config') $ScreenShortsRoot
    Copy-Item -LiteralPath (Join-Path $ScreenShortsRoot 'config\runtime-windows.json') -Destination $ScreenShortsConfig
    if (Test-Path -LiteralPath $ScreenShortsOutput) {
        Assert-ScreenShortsDevOutput $ScreenShortsOutput $ScreenShortsRoot
        Assert-ScreenShortsExecutableStopped $ScreenShortsExe
        $ScreenShortsBackup = Assert-ScreenShortsOutputPath (Join-Path $ScreenShortsBuildRoot ('.windows-x64-backup-' + [Guid]::NewGuid().ToString('N'))) $ScreenShortsRoot
        $null = Assert-ScreenShortsOutputPath $ScreenShortsOutput $ScreenShortsRoot
        Move-Item -LiteralPath $ScreenShortsOutput -Destination $ScreenShortsBackup
    }
    try {
        $null = Assert-ScreenShortsOutputPath $ScreenShortsStage $ScreenShortsRoot
        $null = Assert-ScreenShortsOutputPath $ScreenShortsOutput $ScreenShortsRoot
        Move-Item -LiteralPath $ScreenShortsStage -Destination $ScreenShortsOutput
        $ScreenShortsStage = $null
    } catch {
        if ($ScreenShortsBackup -and -not (Test-Path -LiteralPath $ScreenShortsOutput)) {
            $null = Assert-ScreenShortsOwnedOutput $ScreenShortsBackup $ScreenShortsRoot 'dev'
            $null = Assert-ScreenShortsOutputPath $ScreenShortsOutput $ScreenShortsRoot
            Move-Item -LiteralPath $ScreenShortsBackup -Destination $ScreenShortsOutput
            $ScreenShortsBackup = $null
        }
        throw
    }
    if ($ScreenShortsBackup) {
        try { Remove-ScreenShortsOwnedOutput $ScreenShortsBackup $ScreenShortsRoot 'dev'; $ScreenShortsBackup = $null }
        catch { Write-Warning "Новая сборка готова; прежняя папка оставлена в $ScreenShortsBackup. $($_.Exception.Message)" }
    }
    Write-Host "Подготовлен EXE разработчика: $ScreenShortsExe"
    Write-Host 'Этот режим пока использует системный WebView2 и не комплектует OCR/Python.'
    Write-Host 'Приложение, тесты, OCR и модели не запускались. Самостоятельный комплект: -Bundle, -Portable или -Release.'
} catch {
    Write-Error $_ -ErrorAction Continue
    exit 1
} finally {
    if ($ScreenShortsStage -and (Test-Path -LiteralPath $ScreenShortsStage)) {
        try { Remove-ScreenShortsOwnedOutput $ScreenShortsStage $ScreenShortsRoot 'dev' }
        catch { Write-Warning "Временная папка оставлена: $ScreenShortsStage. $($_.Exception.Message)" }
    }
    if ($ScreenShortsBuildLock) { $ScreenShortsBuildLock.Dispose() }
}
