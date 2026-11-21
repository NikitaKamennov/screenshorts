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
        Assert-ScreenShooterWindows
        . (Join-Path $PSScriptRoot 'windows\downloads.ps1')
        . (Join-Path $PSScriptRoot 'windows\archives.ps1')
        . (Join-Path $PSScriptRoot 'windows\components.ps1')
        . (Join-Path $PSScriptRoot 'windows\notices.ps1')
        . (Join-Path $PSScriptRoot 'windows\package.ps1')
        . (Join-Path $PSScriptRoot 'windows\installer.ps1')
        New-ScreenShooterWindowsPackage ([IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))) -Portable:$Portable -Release:$Release -Offline:$Offline -OutputTag $OutputTag
        exit 0
    } catch { Write-Error $_ -ErrorAction Continue; exit 1 }
}

$ScreenShooterBuildLock = $null
$ScreenShooterStage = $null
$ScreenShooterBackup = $null
$ScreenShooterRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$ScreenShooterBuildRoot = Join-Path $ScreenShooterRoot '.local-data\build'
$ScreenShooterOutput = Join-Path $ScreenShooterBuildRoot 'windows-x64'
$ScreenShooterExe = Join-Path $ScreenShooterOutput 'ScreenShooterPro.exe'
try {
    Assert-ScreenShooterWindows
    $ScreenShooterGo = Get-Command go -CommandType Application -ErrorAction SilentlyContinue
    if (-not $ScreenShooterGo) { throw 'Установи Go 1.25 или новее на машине сборки; смотри README.md.' }
    $null = New-ScreenShooterDirectory $ScreenShooterBuildRoot $ScreenShooterRoot
    $ScreenShooterBuildLock = Open-ScreenShooterBuildLock (Join-Path $ScreenShooterBuildRoot 'windows-x64.lock') $ScreenShooterRoot
    if (Test-Path -LiteralPath $ScreenShooterOutput) {
        Assert-ScreenShooterDevOutput $ScreenShooterOutput $ScreenShooterRoot
        Assert-ScreenShooterExecutableStopped $ScreenShooterExe
    }
    $ScreenShooterStage = New-ScreenShooterOwnedOutput (Join-Path $ScreenShooterBuildRoot ('.windows-x64-stage-' + [Guid]::NewGuid().ToString('N'))) $ScreenShooterRoot 'dev'
    $ScreenShooterStageExe = Join-Path $ScreenShooterStage 'ScreenShooterPro.exe'
    Invoke-ScreenShooterGoBuild $ScreenShooterRoot $ScreenShooterStageExe -Offline:$Offline
    $ScreenShooterConfig = New-ScreenShooterDirectory (Join-Path $ScreenShooterStage 'config') $ScreenShooterRoot
    Copy-Item -LiteralPath (Join-Path $ScreenShooterRoot 'config\runtime-windows.json') -Destination $ScreenShooterConfig
    if (Test-Path -LiteralPath $ScreenShooterOutput) {
        Assert-ScreenShooterDevOutput $ScreenShooterOutput $ScreenShooterRoot
        Assert-ScreenShooterExecutableStopped $ScreenShooterExe
        $ScreenShooterBackup = Assert-ScreenShooterOutputPath (Join-Path $ScreenShooterBuildRoot ('.windows-x64-backup-' + [Guid]::NewGuid().ToString('N'))) $ScreenShooterRoot
        $null = Assert-ScreenShooterOutputPath $ScreenShooterOutput $ScreenShooterRoot
        Move-Item -LiteralPath $ScreenShooterOutput -Destination $ScreenShooterBackup
    }
    try {
        $null = Assert-ScreenShooterOutputPath $ScreenShooterStage $ScreenShooterRoot
        $null = Assert-ScreenShooterOutputPath $ScreenShooterOutput $ScreenShooterRoot
        Move-Item -LiteralPath $ScreenShooterStage -Destination $ScreenShooterOutput
        $ScreenShooterStage = $null
    } catch {
        if ($ScreenShooterBackup -and -not (Test-Path -LiteralPath $ScreenShooterOutput)) {
            $null = Assert-ScreenShooterOwnedOutput $ScreenShooterBackup $ScreenShooterRoot 'dev'
            $null = Assert-ScreenShooterOutputPath $ScreenShooterOutput $ScreenShooterRoot
            Move-Item -LiteralPath $ScreenShooterBackup -Destination $ScreenShooterOutput
            $ScreenShooterBackup = $null
        }
        throw
    }
    if ($ScreenShooterBackup) {
        try { Remove-ScreenShooterOwnedOutput $ScreenShooterBackup $ScreenShooterRoot 'dev'; $ScreenShooterBackup = $null }
        catch { Write-Warning "Новая сборка готова; прежняя папка оставлена в $ScreenShooterBackup. $($_.Exception.Message)" }
    }
    Write-Host "Подготовлен EXE разработчика: $ScreenShooterExe"
    Write-Host 'Этот режим пока использует системный WebView2 и не комплектует OCR/Python.'
    Write-Host 'Приложение, тесты, OCR и модели не запускались. Самостоятельный комплект: -Bundle, -Portable или -Release.'
} catch {
    Write-Error $_ -ErrorAction Continue
    exit 1
} finally {
    if ($ScreenShooterStage -and (Test-Path -LiteralPath $ScreenShooterStage)) {
        try { Remove-ScreenShooterOwnedOutput $ScreenShooterStage $ScreenShooterRoot 'dev' }
        catch { Write-Warning "Временная папка оставлена: $ScreenShooterStage. $($_.Exception.Message)" }
    }
    if ($ScreenShooterBuildLock) { $ScreenShooterBuildLock.Dispose() }
}
