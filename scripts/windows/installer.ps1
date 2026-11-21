# The pinned Inno distribution is prepared only for an explicitly requested
# -Release build. Portable mode keeps compiler files inside this project.
function Invoke-ScreenShooterInstallerTool {
    param([string]$Executable, [string[]]$Arguments, [string]$WorkingDirectory, [string]$TemporaryDirectory, [int]$TimeoutSeconds)
    $taskStart = [Diagnostics.ProcessStartInfo]::new()
    $taskStart.FileName = $Executable
    $taskStart.Arguments = ($Arguments | ForEach-Object { ConvertTo-ScreenShooterWindowsArgument $_ }) -join ' '
    $taskStart.WorkingDirectory = $WorkingDirectory
    $taskStart.UseShellExecute = $false
    $taskStart.CreateNoWindow = $true
    $taskStart.WindowStyle = [Diagnostics.ProcessWindowStyle]::Hidden
    $taskStart.RedirectStandardOutput = $true
    $taskStart.RedirectStandardError = $true
    $taskStart.StandardOutputEncoding = [Text.UTF8Encoding]::new($false)
    $taskStart.StandardErrorEncoding = [Text.UTF8Encoding]::new($false)
    $taskStart.EnvironmentVariables['TEMP'] = $TemporaryDirectory
    $taskStart.EnvironmentVariables['TMP'] = $TemporaryDirectory
    $taskProcess = [Diagnostics.Process]::new()
    $taskProcess.StartInfo = $taskStart
    try {
        $null = $taskProcess.Start()
        $taskOutput = $taskProcess.StandardOutput.ReadToEndAsync()
        $taskError = $taskProcess.StandardError.ReadToEndAsync()
        if (-not $taskProcess.WaitForExit($TimeoutSeconds * 1000)) {
            # Inno Setup uses a loader child. Stop that build-tool tree, never
            # ScreenShooter Pro or any unrelated process.
            $taskChildren = @(Get-CimInstance Win32_Process -Filter ("ParentProcessId = " + $taskProcess.Id))
            foreach ($taskChild in $taskChildren) { Stop-Process -Id $taskChild.ProcessId -Force -ErrorAction SilentlyContinue }
            $taskProcess.Kill()
            $null = $taskProcess.WaitForExit(5000)
            throw "Инструмент установщика превысил лимит $TimeoutSeconds секунд."
        }
        if ($taskProcess.ExitCode -ne 0) { throw "Инструмент установщика завершился с кодом $($taskProcess.ExitCode). $($taskOutput.Result) $($taskError.Result)" }
        if ($taskError.Result) { Write-Host $taskError.Result }
        return $taskOutput.Result
    } finally { $taskProcess.Dispose() }
}

function New-ScreenShooterInstaller {
    param([string]$ProjectRoot, [string]$Application, [string]$Output, $Release, $Manifest)
    $taskWork = New-ScreenShooterOwnedOutput (Join-Path $ProjectRoot ('.local-data\build\.inno-stage-' + [Guid]::NewGuid().ToString('N'))) $ProjectRoot 'installer-tools'
    try {
        $taskCache = Get-ScreenShooterWindowsCache $ProjectRoot
        $taskArtifact = Get-ScreenShooterArtifactByID $Manifest 'inno-setup'
        $taskSetup = Assert-ScreenShooterCachedArtifact $taskArtifact $taskCache $ProjectRoot
        $taskNotice = Get-ScreenShooterArtifactByID $Manifest 'inno-license'
        $null = Assert-ScreenShooterCachedArtifact $taskNotice $taskCache $ProjectRoot
        $taskCompilerRoot = Assert-ScreenShooterOutputPath (Join-Path $taskWork 'compiler') $ProjectRoot
        $taskTemporary = New-ScreenShooterDirectory (Join-Path $taskWork 'temp') $ProjectRoot
        Write-Host 'Подготовка закреплённого Inno Setup в portable-режиме внутри .local-data (без системной установки).'
        $null = Invoke-ScreenShooterInstallerTool $taskSetup @('/PORTABLE=1','/CURRENTUSER','/VERYSILENT','/SUPPRESSMSGBOXES','/SP-','/NORESTART','/NOICONS','/TASKS=',('/DIR='+$taskCompilerRoot)) $taskWork $taskTemporary 120
        Assert-ScreenShooterPlainTree $taskWork $ProjectRoot 268435456
        $taskCompiler = Assert-ScreenShooterOutputPath (Join-Path $taskCompilerRoot 'ISCC.exe') $ProjectRoot
        if (-not (Test-Path -LiteralPath $taskCompiler -PathType Leaf)) { throw 'В portable-компиляторе отсутствует ISCC.exe.' }
        $taskCompilerLicense = Join-Path $taskCompilerRoot 'license.txt'
        if ((Get-FileHash -LiteralPath $taskCompilerLicense -Algorithm SHA256).Hash.ToLowerInvariant() -ne $taskNotice.sha256) { throw 'Лицензия подготовленного Inno Setup не соответствует manifest.' }
        $taskName = [IO.Path]::GetFileNameWithoutExtension($Release.installer_filename)
        $taskArguments = @('/Q',('/DPayloadRoot='+$Application),('/DInstallerOutput='+$Output),('/DInstallerName='+$taskName),('/DAppVersion='+$Release.version),('/DFileVersion='+$Release.file_version),(Join-Path $ProjectRoot 'scripts\windows\installer.iss'))
        Write-Host 'Компиляция офлайн-установщика для текущего пользователя. Приложение не запускается.'
        $taskCompilerOutput = Invoke-ScreenShooterInstallerTool $taskCompiler $taskArguments $taskWork $taskTemporary 600
        if ($taskCompilerOutput) { Write-Host $taskCompilerOutput }
        $taskInstaller = Assert-ScreenShooterOutputPath (Join-Path $Output $Release.installer_filename) $ProjectRoot
        if (-not (Test-Path -LiteralPath $taskInstaller -PathType Leaf)) { throw 'Компилятор не создал установщик.' }
        Copy-Item -LiteralPath $taskCompilerLicense -Destination (Join-Path $Output 'Inno-Setup-LICENSE.txt')
        return $taskInstaller
    } finally {
        Remove-ScreenShooterOwnedOutput $taskWork $ProjectRoot 'installer-tools'
    }
}
