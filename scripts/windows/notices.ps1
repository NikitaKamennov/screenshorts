# File preparation only: read Go build metadata and copy public notices.
# Neither the application nor any shipped worker/runtime is executed here.

function Test-ScreenShortsNoticeName {
    param([string]$Name)
    return $Name -match '^(LICENSE|LICENCE|COPYING|NOTICE|COPYRIGHT|PATENTS|AUTHORS|THIRD[-_]?PARTY[-_]?NOTICES)([._-].*)?$'
}

function Copy-ScreenShortsNoticeFile {
    param([string]$Source, [string]$Destination, [string]$ProjectRoot)
    $taskTarget = Assert-ScreenShortsOutputPath $Destination $ProjectRoot
    $taskSourceItem = Get-Item -LiteralPath $Source -Force -ErrorAction Stop
    if ($taskSourceItem.PSIsContainer -or ($taskSourceItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $taskSourceItem.Length -gt 8388608) { throw "Недопустимый файл уведомления: $Source" }
    if (Test-Path -LiteralPath $taskTarget) { throw "Уведомление не перезаписывает существующий файл: $taskTarget" }
    $null = New-ScreenShortsDirectory ([IO.Path]::GetDirectoryName($taskTarget)) $ProjectRoot
    Copy-Item -LiteralPath $Source -Destination $taskTarget
}

function New-ScreenShortsGoNotices {
    param([string]$Go, [string]$Executable, [string]$ModuleCache, [string]$Destination, [string]$ProjectRoot)
    $taskDestination = New-ScreenShortsDirectory $Destination $ProjectRoot
    # `go version -m` reads the PE build-info section. It does not run the EXE.
    $taskBuildInfo = @(& $Go version -m $Executable)
    if ($LASTEXITCODE -ne 0 -or $taskBuildInfo.Count -lt 2 -or $taskBuildInfo[0] -notmatch ':\s+(go[0-9][^\s]+)$') { throw 'Не удалось прочитать Go build info из собранного EXE.' }
    $taskGoVersion = $Matches[1]
    $taskCompilerVersion = & $Go env GOVERSION
    if ($LASTEXITCODE -ne 0 -or $taskCompilerVersion -ne $taskGoVersion) { throw 'Версия компилятора отличается от версии runtime в EXE; уведомления не подставлены по аналогии.' }
    $taskGoRoot = & $Go env GOROOT
    if ($LASTEXITCODE -ne 0 -or -not [IO.Path]::IsPathRooted($taskGoRoot)) { throw 'Не определён каталог фактически использованного Go toolchain.' }
    $taskGoRoot = [IO.Path]::GetFullPath($taskGoRoot).TrimEnd('\')
    if ((Get-Item -LiteralPath $taskGoRoot -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Ссылочный GOROOT требует явной подготовки уведомлений.' }
    foreach ($taskName in @('LICENSE','PATENTS')) {
        Copy-ScreenShortsNoticeFile (Join-Path $taskGoRoot $taskName) (Join-Path $taskDestination ('toolchain\'+$taskName)) $ProjectRoot
    }
    # Go's standard library embeds additional vendored code. Only public notice
    # files under this compiler's src/vendor tree are read from the toolchain.
    $taskVendor = Join-Path $taskGoRoot 'src\vendor'
    foreach ($taskFile in Get-ChildItem -LiteralPath $taskVendor -Recurse -File) {
        if (-not (Test-ScreenShortsNoticeName $taskFile.Name)) { continue }
        $taskRelative = $taskFile.FullName.Substring($taskGoRoot.Length+1)
        Copy-ScreenShortsNoticeFile $taskFile.FullName (Join-Path $taskDestination ('toolchain\'+$taskRelative)) $ProjectRoot
    }
    $taskSumLines = @(Get-Content -LiteralPath (Join-Path $ProjectRoot 'go.sum'))
    $taskModules = @(foreach ($taskLine in $taskBuildInfo | Select-Object -Skip 1) {
        if ($taskLine -match '^\s*=>') { throw 'Go replacement в EXE требует отдельного учёта лицензий.' }
        if ($taskLine -notmatch '^\s*dep\s+(\S+)\s+(\S+)\s+(h1:\S+)\s*$') { continue }
        $taskModule = $Matches[1]; $taskVersion = $Matches[2]; $taskSum = $Matches[3]
        if ($taskModule -cnotmatch '^[a-z0-9._~/-]+$' -or $taskModule -match '(^|/)\.{1,2}(/|$)' -or
            $taskVersion -cnotmatch '^v[0-9][a-zA-Z0-9.+-]*$' -or ($taskSumLines -notcontains ($taskModule+' '+$taskVersion+' '+$taskSum))) { throw "Go-зависимость не соответствует исходному lock: $taskModule $taskVersion" }
        $taskModuleRoot = Assert-ScreenShortsOutputPath (Join-Path $ModuleCache ($taskModule+'@'+$taskVersion)) $ProjectRoot
        Assert-ScreenShortsPlainTree $taskModuleRoot $ProjectRoot
        $taskFiles = @(Get-ChildItem -LiteralPath $taskModuleRoot -Recurse -File | Where-Object { Test-ScreenShortsNoticeName $_.Name })
        $taskRootLicenses = @($taskFiles | Where-Object { $_.DirectoryName -eq $taskModuleRoot -and $_.Name -match '^(LICENSE|LICENCE|COPYING)([._-].*)?$' })
        if ($taskRootLicenses.Count -eq 0) { throw "Нет корневой лицензии Go-зависимости: $taskModule $taskVersion" }
        $taskModuleOutput = Join-Path $taskDestination ('modules\'+$taskModule+'@'+$taskVersion)
        foreach ($taskFile in $taskFiles) {
            $taskRelative = $taskFile.FullName.Substring($taskModuleRoot.Length+1)
            Copy-ScreenShortsNoticeFile $taskFile.FullName (Join-Path $taskModuleOutput $taskRelative) $ProjectRoot
        }
        [ordered]@{module=$taskModule;version=$taskVersion;go_sum=$taskSum;notice_directory=('modules/'+$taskModule+'@'+$taskVersion);notice_files=$taskFiles.Count}
    })
    if ($taskModules.Count -eq 0) { throw 'В EXE отсутствует ожидаемый перечень Go-зависимостей.' }
    $taskFileList = @(Get-ChildItem -LiteralPath $taskDestination -Recurse -File | Sort-Object FullName | ForEach-Object {
        [ordered]@{path=$_.FullName.Substring($taskDestination.Length+1).Replace('\','/');bytes=$_.Length;sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}
    })
    $taskNoticeManifest = [ordered]@{schema='screenshorts-go-notices-v1';go_version=$taskGoVersion;executable_sha256=(Get-FileHash -LiteralPath $Executable -Algorithm SHA256).Hash.ToLowerInvariant();modules=$taskModules;files=$taskFileList}
    [IO.File]::WriteAllText((Join-Path $taskDestination 'manifest.json'),($taskNoticeManifest | ConvertTo-Json -Depth 7)+[Environment]::NewLine,[Text.UTF8Encoding]::new($false))
    Write-Host "Включены уведомления Go runtime и $($taskModules.Count) модулей из build info EXE."
}

function New-ScreenShortsRuntimeNotices {
    param([string]$Runtime, $Manifest, [string]$ProjectRoot)
    $taskFlat = Get-Content -LiteralPath (Join-Path $ProjectRoot 'licenses\flatbuffers\source.json') -Raw -Encoding UTF8 | ConvertFrom-Json
    $taskArtifact = Get-ScreenShortsArtifactByID $Manifest 'flatbuffers'
    $taskFlatLicense = Assert-ScreenShortsOutputPath (Join-Path $ProjectRoot 'licenses\flatbuffers\LICENSE') $ProjectRoot
    if ($taskFlat.version -ne $taskArtifact.version -or (Get-Item -LiteralPath $taskFlatLicense).Length -ne $taskFlat.bytes -or
        (Get-FileHash -LiteralPath $taskFlatLicense -Algorithm SHA256).Hash.ToLowerInvariant() -ne $taskFlat.sha256) { throw 'FlatBuffers license/SHA не соответствует выбранному wheel.' }
    Copy-ScreenShortsPlainTree (Join-Path $ProjectRoot 'licenses\flatbuffers') (Join-Path $Runtime 'licenses\flatbuffers') $ProjectRoot
    $taskExpected = @('runtime/python/LICENSE.txt','runtime/python/Lib/site-packages/onnxruntime/LICENSE','runtime/python/Lib/site-packages/onnxruntime/ThirdPartyNotices.txt',
        'runtime/python/Lib/site-packages/numpy-2.5.5.dist-info/licenses/LICENSE.txt','runtime/python/Lib/site-packages/pillow-13.0.0.dist-info/licenses/LICENSE',
        'runtime/python/Lib/site-packages/packaging-26.3.dist-info/licenses/LICENSE','runtime/python/Lib/site-packages/packaging-26.3.dist-info/licenses/LICENSE.APACHE',
        'runtime/python/Lib/site-packages/packaging-26.3.dist-info/licenses/LICENSE.BSD','runtime/python/Lib/site-packages/protobuf-7.36.2.dist-info/LICENSE',
        'licenses/flatbuffers/LICENSE','licenses/tessdata-fast/LICENSE.txt','licenses/tesseract-distribution/doc/LICENSE')
    $taskNoticeFiles = @(foreach ($taskRelative in $taskExpected) {
        $taskPath = Assert-ScreenShortsOutputPath (Join-Path $Runtime $taskRelative) $ProjectRoot
        if (-not (Test-Path -LiteralPath $taskPath -PathType Leaf)) { throw "Нет обязательного уведомления runtime: $taskRelative" }
        [ordered]@{path=$taskRelative;bytes=(Get-Item -LiteralPath $taskPath).Length;sha256=(Get-FileHash -LiteralPath $taskPath -Algorithm SHA256).Hash.ToLowerInvariant()}
    })
    # This receipt deliberately records the remaining audit, rather than treating
    # an existing top-level LICENSE as proof for all DLLs or codecs.
    $taskReceipt = [ordered]@{schema='screenshorts-runtime-notices-v1';audit_status='incomplete';notice_files=$taskNoticeFiles;open_items=@('Microsoft WebView2 terms and complete offline third-party notices','Microsoft CRT redistribution terms','Tesseract distribution DLL licenses and corresponding source requirements')}
    [IO.File]::WriteAllText((Join-Path $Runtime 'licenses\runtime-notices.json'),($taskReceipt | ConvertTo-Json -Depth 6)+[Environment]::NewLine,[Text.UTF8Encoding]::new($false))
}
