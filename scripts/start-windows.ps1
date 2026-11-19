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
    Assert-ScreenShortsWindows
    $ScreenShortsRoot = Split-Path -Parent $PSScriptRoot
    $ScreenShortsDevExe = Join-Path $ScreenShortsRoot '.local-data\build\windows-x64\ScreenShortsPro.exe'
    if (-not $Executable) { $Executable = $ScreenShortsDevExe }
    $Executable = [IO.Path]::GetFullPath($ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Executable))
    if (-not (Test-Path -LiteralPath $Executable -PathType Leaf)) {
        throw "Нет EXE: $Executable. Сначала собери приложение отдельно: .\scripts\build-windows.ps1"
    }
    if (-not $RuntimeConfig) {
        if ([string]::Equals($Executable, [IO.Path]::GetFullPath($ScreenShortsDevExe), [StringComparison]::OrdinalIgnoreCase)) {
            $RuntimeConfig = Join-Path $ScreenShortsRoot '.local-data\background-windows\runtime-windows.json'
        }
        if (-not $RuntimeConfig -or -not (Test-Path -LiteralPath $RuntimeConfig -PathType Leaf)) {
            $RuntimeConfig = Join-Path (Split-Path -Parent $Executable) 'config\runtime-windows.json'
        }
    }
    $RuntimeConfig = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($RuntimeConfig)
    if (-not (Test-Path -LiteralPath $RuntimeConfig -PathType Leaf)) { throw "Нет профиля runtime: $RuntimeConfig" }
    $ScreenShortsArguments = @('--runtime-config', $RuntimeConfig)
    if ($DataRoot) {
        if ($DataRoot -notmatch '^[A-Za-z]:[\\/]') {
            throw 'В -DataRoot укажи отдельный абсолютный каталог данных на локальном NTFS.'
        }
        $DataRoot = [IO.Path]::GetFullPath($DataRoot)
        $ScreenShortsArguments += @('--data-root', $DataRoot)
    }
    $ScreenShortsCommandLine = ($ScreenShortsArguments | ForEach-Object { ConvertTo-ScreenShortsWindowsArgument $_ }) -join ' '
    # This is the explicitly invoked interactive launcher, never a build hook.
    $ScreenShortsProcess = Start-Process -FilePath $Executable -ArgumentList $ScreenShortsCommandLine -WorkingDirectory (Split-Path -Parent $Executable) -Wait -PassThru
    exit $ScreenShortsProcess.ExitCode
} catch {
    Write-Error $_ -ErrorAction Continue
    exit 1
}
