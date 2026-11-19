# Reusable local runtime assembly. It does not build or launch ScreenShorts Pro,
# install system components, run pip, execute Python, OCR or a model.

function Get-ScreenShortsArtifactByID {
    param($Manifest, [string]$ID)
    $taskMatches = @($Manifest.artifacts | Where-Object { $_.id -eq $ID })
    if ($taskMatches.Count -ne 1) { throw "Нет единственного закреплённого компонента $ID." }
    return $taskMatches[0]
}

function Copy-ScreenShortsPlainTree {
    param([string]$Source, [string]$Destination, [string]$ProjectRoot)
    Assert-ScreenShortsPlainTree $Source $ProjectRoot
    $null = New-ScreenShortsDirectory $Destination $ProjectRoot
    foreach ($taskItem in Get-ChildItem -LiteralPath $Source -Force) {
        $taskTarget = Assert-ScreenShortsOutputPath (Join-Path $Destination $taskItem.Name) $ProjectRoot
        if (Test-Path -LiteralPath $taskTarget) { throw "Копирование дерева не перезаписывает: $taskTarget" }
        Copy-Item -LiteralPath $taskItem.FullName -Destination $taskTarget -Recurse
    }
}

function Assert-ScreenShortsRuntimeStopped {
    param([string]$Directory)
    if (Get-Process -Name ScreenShortsPro -ErrorAction SilentlyContinue) { throw 'Перед подготовкой runtime полностью выйди из ScreenShorts Pro.' }
    $taskPrefix = [IO.Path]::GetFullPath($Directory).TrimEnd('\') + '\'
    foreach ($taskProcess in Get-Process -Name python,pythonw,tesseract,msedgewebview2 -ErrorAction SilentlyContinue) {
        try { $taskProcessPath = $taskProcess.Path } catch { continue }
        if ($taskProcessPath -and $taskProcessPath.StartsWith($taskPrefix,[StringComparison]::OrdinalIgnoreCase)) { throw 'Компонент прежнего runtime ещё работает; дождись его остановки.' }
    }
}

function Write-ScreenShortsRuntimeProfile {
    param([string]$Directory, $Manifest, [string]$ProjectRoot)
    $taskTesseract = Get-ScreenShortsArtifactByID $Manifest 'tesseract'
    $taskLanguages = Get-ScreenShortsArtifactByID $Manifest 'tessdata-eng'
    $taskModel = Get-ScreenShortsArtifactByID $Manifest 'u2netp'
    $taskProfile = [ordered]@{
        schema = 'skrinpolka-runtime-v1'
        ocr = [ordered]@{
            enabled = $true
            executable = '../tools/tesseract/tesseract.exe'
            tessdata_directory = '../tools/tesseract/tessdata'
            languages = @('rus','eng')
            read_roots = @('../tools/tesseract')
            memory_bytes = 805306368
            threads = 2
            timeout_seconds = 90
            model_id = 'tesseract-lstm-rus-eng'
            model_revision = ($taskTesseract.version + '+tessdata-fast-' + $taskLanguages.version)
        }
        background_removal = [ordered]@{
            enabled = $true
            executable = '../runtime/python/python.exe'
            script = '../workers/background/remove_background.py'
            model_path = '../models/background/u2netp.onnx'
            model_sha256 = $taskModel.sha256
            model_id = 'u2netp'
            read_roots = @('../runtime/python','../workers/background','../models/background')
            memory_bytes = 1073741824
            threads = 2
            timeout_seconds = 120
        }
    }
    $taskConfig = New-ScreenShortsDirectory (Join-Path $Directory 'config') $ProjectRoot
    [IO.File]::WriteAllText((Join-Path $taskConfig 'runtime-windows.json'), ($taskProfile | ConvertTo-Json -Depth 8) + [Environment]::NewLine, [Text.UTF8Encoding]::new($false))
}

function New-ScreenShortsRuntimeComponents {
    param($Manifest, [string]$Cache, [string]$ProjectRoot)
    if ($Manifest.python_abi -ne 'cp313') { throw 'Для смены Python ABI требуется адаптация комплектователя Windows.' }
    $taskOutput = Assert-ScreenShortsOutputPath (Join-Path $ProjectRoot '.local-data\windows-runtime') $ProjectRoot
    Assert-ScreenShortsRuntimeStopped $taskOutput
    if (Test-Path -LiteralPath $taskOutput) { $null = Assert-ScreenShortsOwnedOutput $taskOutput $ProjectRoot 'runtime-components' }
    foreach ($taskArtifact in $Manifest.artifacts | Where-Object { $_.role -ne 'installer-tool' }) { $null = Assert-ScreenShortsCachedArtifact $taskArtifact $Cache $ProjectRoot }
    if (([IO.DriveInfo]::new([IO.Path]::GetPathRoot($taskOutput))).AvailableFreeSpace -lt 3221225472) { throw 'Для staging runtime требуется 3 GiB свободного места, без удаления прежних файлов.' }
    $taskStage = New-ScreenShortsOwnedOutput (Join-Path $ProjectRoot ('.local-data\.windows-runtime-stage-' + [Guid]::NewGuid().ToString('N'))) $ProjectRoot 'runtime-components'
    try {
        $taskWork = New-ScreenShortsDirectory (Join-Path $taskStage '.packaging-tools') $ProjectRoot
        Write-Host 'Подготовка распаковщика из закреплённого SFX без установки 7-Zip.'
        $taskTool = Initialize-ScreenShortsArchiveTool $Manifest $Cache $taskWork $ProjectRoot

        Write-Host 'Извлечение OCR, DLL и уведомлений без запуска Tesseract.'
        $taskOCRSource = New-ScreenShortsDirectory (Join-Path $taskWork 'tesseract') $ProjectRoot
        $taskArtifact = Get-ScreenShortsArtifactByID $Manifest 'tesseract'
        Expand-ScreenShortsArchive $taskTool (Assert-ScreenShortsCachedArtifact $taskArtifact $Cache $ProjectRoot) $taskOCRSource $ProjectRoot 536870912
        $taskOCREntries = @(Get-ChildItem -LiteralPath $taskOCRSource -Filter 'tesseract.exe' -Recurse -File)
        if ($taskOCREntries.Count -ne 1) { throw 'В закреплённом дистрибутиве нет единственного tesseract.exe.' }
        $taskOCRRoot = $taskOCREntries[0].DirectoryName
        $taskOCR = New-ScreenShortsDirectory (Join-Path $taskStage 'tools\tesseract') $ProjectRoot
        foreach ($taskFile in Get-ChildItem -LiteralPath $taskOCRRoot -File) {
            if ($taskFile.Name -eq 'tesseract.exe' -or $taskFile.Extension -eq '.dll') { Copy-Item -LiteralPath $taskFile.FullName -Destination (Join-Path $taskOCR $taskFile.Name) }
        }
        $taskData = New-ScreenShortsDirectory (Join-Path $taskOCR 'tessdata') $ProjectRoot
        foreach ($taskLanguage in @('eng','rus')) {
            $taskArtifact = Get-ScreenShortsArtifactByID $Manifest ('tessdata-'+$taskLanguage)
            Copy-Item -LiteralPath (Assert-ScreenShortsCachedArtifact $taskArtifact $Cache $ProjectRoot) -Destination (Join-Path $taskData ($taskLanguage+'.traineddata'))
        }
        foreach ($taskSubdirectory in @('configs','tessconfigs')) {
            $taskSource = Join-Path $taskOCRRoot ('tessdata\'+$taskSubdirectory)
            if (Test-Path -LiteralPath $taskSource -PathType Container) { Copy-ScreenShortsPlainTree $taskSource (Join-Path $taskData $taskSubdirectory) $ProjectRoot }
        }
        $taskOCRLicenses = New-ScreenShortsDirectory (Join-Path $taskStage 'licenses\tesseract-distribution') $ProjectRoot
        # Retain the complete documentation subtree, plus root notices. Installer
        # plugin DLLs and unrelated CLI utilities are not runtime dependencies.
        foreach ($taskSubdirectory in @('doc','share\licenses','share\doc')) {
            $taskSource = Join-Path $taskOCRRoot $taskSubdirectory
            if (Test-Path -LiteralPath $taskSource -PathType Container) { Copy-ScreenShortsPlainTree $taskSource (Join-Path $taskOCRLicenses $taskSubdirectory.Replace('\','-')) $ProjectRoot }
        }
        foreach ($taskFile in Get-ChildItem -LiteralPath $taskOCRRoot -File | Where-Object { $_.Name -match 'license|copying|notice|authors' -or $_.Extension -in @('.txt','.html','.rtf') }) {
            Copy-Item -LiteralPath $taskFile.FullName -Destination (Join-Path $taskOCRLicenses $taskFile.Name)
        }

        Write-Host 'Подготовка переносимого Python, CPU wheels и app-local CRT без pip/инференса.'
        $taskPython = New-ScreenShortsDirectory (Join-Path $taskStage 'runtime\python') $ProjectRoot
        $taskArtifact = Get-ScreenShortsArtifactByID $Manifest 'python'
        Expand-ScreenShortsZip (Assert-ScreenShortsCachedArtifact $taskArtifact $Cache $ProjectRoot) $taskPython $ProjectRoot
        $taskSite = New-ScreenShortsDirectory (Join-Path $taskPython 'Lib\site-packages') $ProjectRoot
        foreach ($taskID in @('onnxruntime','numpy','pillow','flatbuffers','packaging','protobuf')) {
            $taskArtifact = Get-ScreenShortsArtifactByID $Manifest $taskID
            Expand-ScreenShortsZip (Assert-ScreenShortsCachedArtifact $taskArtifact $Cache $ProjectRoot) $taskSite $ProjectRoot
        }
        $taskPth = Assert-ScreenShortsOutputPath (Join-Path $taskPython 'python313._pth') $ProjectRoot
        if (-not (Test-Path -LiteralPath $taskPth -PathType Leaf)) { throw 'В Python нет ожидаемого python313._pth.' }
        [IO.File]::WriteAllText($taskPth, "python313.zip`n.`nLib/site-packages`nimport site`n", [Text.UTF8Encoding]::new($false))
        $taskVC = New-ScreenShortsDirectory (Join-Path $taskWork 'vc-runtime') $ProjectRoot
        $taskArtifact = Get-ScreenShortsArtifactByID $Manifest 'vc-runtime'
        Expand-ScreenShortsZip (Assert-ScreenShortsCachedArtifact $taskArtifact $Cache $ProjectRoot) $taskVC $ProjectRoot 67108864
        $taskVCDLLs = @(Get-ChildItem -LiteralPath $taskVC -Recurse -File -Filter '*.dll' | Where-Object { $_.FullName -match '\\x64\\Microsoft\.VC[0-9]+\.CRT\\' })
        if ($taskVCDLLs.Count -lt 5) { throw 'В закреплённом app-local CRT нет ожидаемого набора x64 DLL.' }
        foreach ($taskDLL in $taskVCDLLs) {
            foreach ($taskDirectory in @($taskPython, (Join-Path $taskSite 'onnxruntime\capi'))) {
                $taskTarget = Assert-ScreenShortsOutputPath (Join-Path $taskDirectory $taskDLL.Name) $ProjectRoot
                [IO.File]::Copy($taskDLL.FullName,$taskTarget,$true)
            }
        }
        $taskWorker = New-ScreenShortsDirectory (Join-Path $taskStage 'workers\background') $ProjectRoot
        foreach ($taskName in @('remove_background.py','runtime-manifest.json','requirements.lock')) {
            $taskSource = Assert-ScreenShortsOutputPath (Join-Path $ProjectRoot ('workers\background\'+$taskName)) $ProjectRoot
            Copy-Item -LiteralPath $taskSource -Destination (Join-Path $taskWorker $taskName)
        }
        $taskModelDirectory = New-ScreenShortsDirectory (Join-Path $taskStage 'models\background') $ProjectRoot
        $taskArtifact = Get-ScreenShortsArtifactByID $Manifest 'u2netp'
        Copy-Item -LiteralPath (Assert-ScreenShortsCachedArtifact $taskArtifact $Cache $ProjectRoot) -Destination (Join-Path $taskModelDirectory 'u2netp.onnx')

        Write-Host 'Извлечение полного WebView2 Fixed Version без запуска браузера.'
        $taskWebSource = New-ScreenShortsDirectory (Join-Path $taskWork 'webview2') $ProjectRoot
        $taskArtifact = Get-ScreenShortsArtifactByID $Manifest 'webview2'
        Expand-ScreenShortsArchive $taskTool (Assert-ScreenShortsCachedArtifact $taskArtifact $Cache $ProjectRoot) $taskWebSource $ProjectRoot 2147483648
        $taskWebEntries = @(Get-ChildItem -LiteralPath $taskWebSource -Recurse -File -Filter 'msedgewebview2.exe')
        if ($taskWebEntries.Count -ne 1) { throw 'Нет единственного msedgewebview2.exe в Fixed Version.' }
        Copy-ScreenShortsPlainTree $taskWebEntries[0].DirectoryName (Join-Path $taskStage 'tools\webview2') $ProjectRoot

        foreach ($taskDirectory in @('u2net','rembg','onnxruntime','zvukbook')) {
            Copy-ScreenShortsPlainTree (Join-Path $ProjectRoot ('licenses\'+$taskDirectory)) (Join-Path $taskStage ('licenses\'+$taskDirectory)) $ProjectRoot
        }
        $taskLicenseDirectory = New-ScreenShortsDirectory (Join-Path $taskStage 'licenses\tessdata-fast') $ProjectRoot
        $taskArtifact = Get-ScreenShortsArtifactByID $Manifest 'tessdata-license'
        Copy-Item -LiteralPath (Assert-ScreenShortsCachedArtifact $taskArtifact $Cache $ProjectRoot) -Destination (Join-Path $taskLicenseDirectory 'LICENSE.txt')
        New-ScreenShortsRuntimeNotices $taskStage $Manifest $ProjectRoot
        $taskLicenses = Join-Path $taskStage 'licenses'
        Copy-Item -LiteralPath (Join-Path $ProjectRoot 'THIRD-PARTY.md') -Destination $taskLicenses
        Copy-Item -LiteralPath (Join-Path $ProjectRoot 'config\windows-downloads.json') -Destination (Join-Path $taskStage 'runtime-downloads.json')
        Write-ScreenShortsRuntimeProfile $taskStage $Manifest $ProjectRoot

        # Build tools are never part of the runtime payload. This directory was
        # created by this invocation and every file was checked for reparse points.
        Assert-ScreenShortsPlainTree $taskWork $ProjectRoot
        $null = Assert-ScreenShortsOutputPath $taskWork $ProjectRoot
        Remove-Item -LiteralPath $taskWork -Recurse -Force
        $taskReceipt = [ordered]@{
            schema='screenshorts-windows-components-v1'
            architecture='amd64'
            python_version=$Manifest.python_version
            manifest_sha256=(Get-FileHash -LiteralPath (Join-Path $ProjectRoot 'config\windows-downloads.json') -Algorithm SHA256).Hash.ToLowerInvariant()
            worker_sha256=(Get-FileHash -LiteralPath (Join-Path $taskWorker 'remove_background.py') -Algorithm SHA256).Hash.ToLowerInvariant()
            prepared_utc=[DateTime]::UtcNow.ToString('o')
            execution='no app, OCR, Python or inference executed'
        }
        [IO.File]::WriteAllText((Join-Path $taskStage 'components-receipt.json'), ($taskReceipt | ConvertTo-Json -Depth 5)+[Environment]::NewLine, [Text.UTF8Encoding]::new($false))
        Assert-ScreenShortsPlainTree $taskStage $ProjectRoot 3221225472
        Assert-ScreenShortsRuntimeStopped $taskOutput
        $taskPublished = Publish-ScreenShortsOwnedOutput $taskStage $taskOutput $ProjectRoot 'runtime-components'
        $taskStage = $null
        return $taskPublished
    } finally {
        if ($taskStage -and (Test-Path -LiteralPath $taskStage)) {
            try { Remove-ScreenShortsOwnedOutput $taskStage $ProjectRoot 'runtime-components' }
            catch { Write-Warning "Незавершённый staging оставлен: $taskStage. $($_.Exception.Message)" }
        }
    }
}
