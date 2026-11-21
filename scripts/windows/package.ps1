function New-ScreenShooterWindowsPackage {
    param([string]$ProjectRoot, [switch]$Portable, [switch]$Release, [switch]$Offline, [string]$OutputTag = '')
    if ($OutputTag -and $OutputTag -cnotmatch '^[a-z0-9][a-z0-9-]{0,47}$') { throw 'Некорректная метка выходного каталога.' }
    $taskOutputSuffix = if ($OutputTag) { '-' + $OutputTag } else { '' }
    $taskRelease = Get-Content -LiteralPath (Join-Path $ProjectRoot 'config\windows-release.json') -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($taskRelease.schema -ne 'screenshorts-windows-release-v1' -or $taskRelease.architecture -ne 'amd64' -or $taskRelease.application_directory -ne 'ScreenShooterPro' -or $taskRelease.portable_filename -ne 'ScreenShooterPro-windows-x64-portable.zip') { throw 'Неподдержанный release-конфиг.' }
    if ($taskRelease.installer_filename -ne 'ScreenShooterPro-windows-x64-setup.exe' -or $taskRelease.file_version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$') { throw 'Некорректный конфиг установщика.' }
    $taskBuildRoot = New-ScreenShooterDirectory (Join-Path $ProjectRoot '.local-data\build') $ProjectRoot
    $taskBundleOutput = Join-Path $taskBuildRoot ('windows-bundle' + $taskOutputSuffix)
    $taskLock = Open-ScreenShooterBuildLock (Join-Path $taskBuildRoot 'windows-package.lock') $ProjectRoot
    $taskStage = $null
    $taskPortableStage = $null
    $taskArchiveKind = if ($Release) { 'windows-release' } else { 'windows-portable' }
    try {
        & (Join-Path $ProjectRoot 'scripts\prepare-windows-runtime.ps1') -Offline:$Offline -IncludeInstaller:$Release -Extract
        if ($LASTEXITCODE -ne 0) { throw 'Подготовка компонентов Windows завершилась ошибкой.' }
        $taskComponents = Assert-ScreenShooterOwnedOutput (Join-Path $ProjectRoot '.local-data\windows-runtime') $ProjectRoot 'runtime-components'
        $taskStage = New-ScreenShooterOwnedOutput (Join-Path $taskBuildRoot ('.windows-bundle-stage-'+[Guid]::NewGuid().ToString('N'))) $ProjectRoot 'windows-bundle'
        $taskApp = New-ScreenShooterDirectory (Join-Path $taskStage 'ScreenShooterPro') $ProjectRoot
        foreach ($taskItem in Get-ChildItem -LiteralPath $taskComponents -Force) {
            if ($taskItem.Name -eq '.screenshorts-output.json') { continue }
            if ($taskItem.Name -notin @('config','tools','runtime','workers','models','licenses','runtime-downloads.json','components-receipt.json')) { throw "Неожиданный файл runtime: $($taskItem.Name)" }
            $taskTarget = Assert-ScreenShooterOutputPath (Join-Path $taskApp $taskItem.Name) $ProjectRoot
            if ($taskItem.PSIsContainer) { Copy-ScreenShooterPlainTree $taskItem.FullName $taskTarget $ProjectRoot }
            else { Copy-Item -LiteralPath $taskItem.FullName -Destination $taskTarget }
        }
        $taskDescriptor = [ordered]@{schema='screenshorts-windows-bundle-v1';architecture='amd64';webview2_directory='../tools/webview2'}
        [IO.File]::WriteAllText((Join-Path $taskApp 'config\windows-bundle.json'),($taskDescriptor | ConvertTo-Json)+[Environment]::NewLine,[Text.UTF8Encoding]::new($false))
        Write-Host 'Сборка EXE для полной папки приложения.'
        Invoke-ScreenShooterGoBuild $ProjectRoot (Join-Path $taskApp 'ScreenShooterPro.exe') -Distribution bundle -Version $taskRelease.version -Offline:$Offline -NoticeDirectory (Join-Path $taskApp 'licenses\go')
        Copy-Item -LiteralPath (Join-Path $ProjectRoot 'LICENSE') -Destination (Join-Path $taskApp 'LICENSE')
        $taskReadme = "ScreenShooter Pro $($taskRelease.version)`r`n`r`nРаспакуй всю папку и запусти ScreenShooterPro.exe. Интернет и установка Python, OCR или WebView2 не требуются.`r`nДанные по умолчанию: %LOCALAPPDATA%\Skrinpolka. Для отдельной ручной пробы укажи --data-root с новым абсолютным путём на NTFS.`r`nСохраняй PNG вместе с соседним .png.json.`r`nЭтот кандидат сборки ещё не прошёл ручную приёмку. Приложение, OCR и модели сборщик не запускает.`r`n"
        [IO.File]::WriteAllText((Join-Path $taskApp 'README.txt'),$taskReadme,[Text.UTF8Encoding]::new($true))
        $taskPayload = @(Get-ChildItem -LiteralPath $taskApp -Recurse -File | Sort-Object FullName | ForEach-Object {
            [ordered]@{path=$_.FullName.Substring($taskApp.Length+1).Replace('\','/');bytes=$_.Length;sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}
        })
        $taskManifest = [ordered]@{schema='screenshorts-windows-bundle-manifest-v1';version=$taskRelease.version;architecture='amd64';acceptance='not-accepted';created_utc=[DateTime]::UtcNow.ToString('o');files=$taskPayload}
        $taskManifestPath = Join-Path $taskApp 'release-manifest.json'
        [IO.File]::WriteAllText($taskManifestPath,($taskManifest | ConvertTo-Json -Depth 6)+[Environment]::NewLine,[Text.UTF8Encoding]::new($false))
        $taskSums = @($taskPayload | ForEach-Object {$_.sha256+'  '+$_.path})
        $taskSums += (Get-FileHash -LiteralPath $taskManifestPath -Algorithm SHA256).Hash.ToLowerInvariant()+'  release-manifest.json'
        [IO.File]::WriteAllText((Join-Path $taskApp 'SHA256SUMS.txt'),($taskSums -join "`n")+"`n",[Text.UTF8Encoding]::new($false))
        Assert-ScreenShooterPlainTree $taskApp $ProjectRoot 3221225472
        Assert-ScreenShooterRuntimeStopped $taskBundleOutput
        $taskPublished = Publish-ScreenShooterOwnedOutput $taskStage $taskBundleOutput $ProjectRoot 'windows-bundle'
        $taskStage=$null
        $taskApp = Join-Path $taskPublished 'ScreenShooterPro'
        Write-Host "Готовая папка приложения: $taskApp"
        if ($Portable -or $Release) {
            $taskReleaseRoot = New-ScreenShooterDirectory (Join-Path $ProjectRoot '.local-data\releases\windows-x64') $ProjectRoot
            $taskPortableStage = New-ScreenShooterOwnedOutput (Join-Path $taskReleaseRoot ('.archive-stage-'+[Guid]::NewGuid().ToString('N'))) $ProjectRoot $taskArchiveKind
            $taskZip = Assert-ScreenShooterOutputPath (Join-Path $taskPortableStage $taskRelease.portable_filename) $ProjectRoot
            $taskInstaller = $null
            if ($Release) {
                $taskInstaller = New-ScreenShooterInstaller $ProjectRoot $taskApp $taskPortableStage $taskRelease (Read-ScreenShooterWindowsDownloads $ProjectRoot)
            }
            Add-Type -AssemblyName System.IO.Compression.FileSystem
            Write-Host 'Упаковка полной папки в portable ZIP.'
            [IO.Compression.ZipFile]::CreateFromDirectory($taskApp,$taskZip,[IO.Compression.CompressionLevel]::Optimal,$true)
            $taskZipSHA=(Get-FileHash -LiteralPath $taskZip -Algorithm SHA256).Hash.ToLowerInvariant()
            $taskArchiveSums = @($taskZipSHA+'  '+$taskRelease.portable_filename)
            if ($Release) {
                $taskArchiveSums += (Get-FileHash -LiteralPath $taskInstaller -Algorithm SHA256).Hash.ToLowerInvariant()+'  '+$taskRelease.installer_filename
                $taskArchiveSums += (Get-FileHash -LiteralPath (Join-Path $taskPortableStage 'Inno-Setup-LICENSE.txt') -Algorithm SHA256).Hash.ToLowerInvariant()+'  Inno-Setup-LICENSE.txt'
            }
            [IO.File]::WriteAllText((Join-Path $taskPortableStage 'SHA256SUMS.txt'),($taskArchiveSums -join "`n")+"`n",[Text.UTF8Encoding]::new($false))
            Copy-Item -LiteralPath (Join-Path $taskApp 'release-manifest.json') -Destination (Join-Path $taskPortableStage 'release-manifest.json')
            $taskArchiveDirectory = $(if ($Release) { 'release' } else { 'portable' }) + $taskOutputSuffix
            $taskPortableOutput=Publish-ScreenShooterOwnedOutput $taskPortableStage (Join-Path $taskReleaseRoot $taskArchiveDirectory) $ProjectRoot $taskArchiveKind
            $taskPortableStage=$null
            Write-Host ('Portable ZIP: '+(Join-Path $taskPortableOutput $taskRelease.portable_filename))
            if ($Release) { Write-Host ('Установщик: '+(Join-Path $taskPortableOutput $taskRelease.installer_filename)) }
        }
        Write-Host 'Сборщик не запускает приложение, тесты, OCR и модели. Комплект готов для ручной пробы.'
    } finally {
        if($taskStage -and (Test-Path -LiteralPath $taskStage)){Remove-ScreenShooterOwnedOutput $taskStage $ProjectRoot 'windows-bundle'}
        if($taskPortableStage -and (Test-Path -LiteralPath $taskPortableStage)){Remove-ScreenShooterOwnedOutput $taskPortableStage $ProjectRoot $taskArchiveKind}
        $taskLock.Dispose()
    }
}
