[CmdletBinding()]
param(
    [string]$Executable = '',
    [string]$DataRoot = '',
    [string]$RuntimeConfig = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot 'windows\common.ps1')

try {
    Assert-ScreenShooterWindows
    $ScreenShooterRoot = Split-Path -Parent $PSScriptRoot
    $ScreenShooterDevExe = Join-Path $ScreenShooterRoot '.local-data\build\windows-x64\ScreenShooterPro.exe'
    if (-not $Executable) { $Executable = $ScreenShooterDevExe }
    $Executable = [IO.Path]::GetFullPath($ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Executable))
    if (-not (Test-Path -LiteralPath $Executable -PathType Leaf)) {
        throw "Нет EXE: $Executable. Сначала собери приложение отдельно: .\scripts\build-windows.ps1"
    }
    if (-not $RuntimeConfig) {
        if ([string]::Equals($Executable, [IO.Path]::GetFullPath($ScreenShooterDevExe), [StringComparison]::OrdinalIgnoreCase)) {
            $RuntimeConfig = Join-Path $ScreenShooterRoot '.local-data\background-windows\runtime-windows.json'
        }
        if (-not $RuntimeConfig -or -not (Test-Path -LiteralPath $RuntimeConfig -PathType Leaf)) {
            $RuntimeConfig = Join-Path (Split-Path -Parent $Executable) 'config\runtime-windows.json'
        }
    }
    $RuntimeConfig = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($RuntimeConfig)
    if (-not (Test-Path -LiteralPath $RuntimeConfig -PathType Leaf)) { throw "Нет профиля runtime: $RuntimeConfig" }
    $ScreenShooterArguments = @('--runtime-config', $RuntimeConfig)
    if ($DataRoot) {
        if ($DataRoot -notmatch '^[A-Za-z]:[\\/]') {
            throw 'В -DataRoot укажи отдельный абсолютный каталог данных на локальном NTFS.'
        }
        $DataRoot = [IO.Path]::GetFullPath($DataRoot)
        $ScreenShooterArguments += @('--data-root', $DataRoot)
    }
    $ScreenShooterCommandLine = ($ScreenShooterArguments | ForEach-Object { ConvertTo-ScreenShooterWindowsArgument $_ }) -join ' '
    # This is the explicitly invoked interactive launcher, never a build hook.
    $ScreenShooterProcess = Start-Process -FilePath $Executable -ArgumentList $ScreenShooterCommandLine -WorkingDirectory (Split-Path -Parent $Executable) -Wait -PassThru
    exit $ScreenShooterProcess.ExitCode
} catch {
    Write-Error $_ -ErrorAction Continue
    exit 1
}
