# Local data preparation from pinned archives. No app, OCR or Python worker is
# executed here; the only child is the verified build-time archive extractor.

function Resolve-ScreenShortsArchivePath {
    param([string]$Destination, [string]$Name, [string]$ProjectRoot)
    $taskRelative = $Name.Replace('/', '\')
    if (-not $taskRelative -or $taskRelative -match '^[\\/]|:|[\x00-\x1f]') { throw "Недопустимый путь архива: $Name" }
    foreach ($taskPart in $taskRelative.Split('\')) {
        if ($taskPart -eq '') { continue }
        if ($taskPart -in @('.', '..') -or $taskPart.TrimEnd([char[]]@(' ', '.')) -ne $taskPart -or
            $taskPart -match '[<>"|?*]' -or $taskPart -match '^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(?:\.|$)') {
            throw "Недопустимый сегмент пути архива: $Name"
        }
    }
    $taskBase = [IO.Path]::GetFullPath($Destination).TrimEnd('\')
    $taskPath = Assert-ScreenShortsOutputPath (Join-Path $taskBase $taskRelative) $ProjectRoot
    if (-not $taskPath.StartsWith($taskBase + '\', [StringComparison]::OrdinalIgnoreCase)) { throw "Выход архива за каталог назначения: $Name" }
    return $taskPath
}

function Assert-ScreenShortsPlainTree {
    param([string]$Directory, [string]$ProjectRoot, [long]$MaxBytes = 2147483648)
    $taskRoot = Assert-ScreenShortsOutputPath $Directory $ProjectRoot
    $taskStack = [Collections.Generic.Stack[string]]::new()
    $taskStack.Push($taskRoot)
    $taskCount = 0
    $taskBytes = [long]0
    while ($taskStack.Count -gt 0) {
        foreach ($taskItem in Get-ChildItem -LiteralPath $taskStack.Pop() -Force) {
            $taskCount++
            if ($taskCount -gt 30000 -or ($taskItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Архив содержит ссылку либо слишком много файлов.' }
            $null = Assert-ScreenShortsOutputPath $taskItem.FullName $ProjectRoot
            if ($taskItem.PSIsContainer) { $taskStack.Push($taskItem.FullName) }
            else { $taskBytes += $taskItem.Length; if ($taskBytes -gt $MaxBytes) { throw 'Превышен лимит распакованных компонентов.' } }
        }
    }
}

function Expand-ScreenShortsZip {
    param([string]$Archive, [string]$Destination, [string]$ProjectRoot, [long]$MaxBytes = 536870912)
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $null = New-ScreenShortsDirectory $Destination $ProjectRoot
    $taskZip = [IO.Compression.ZipFile]::OpenRead($Archive)
    try {
        $taskNames = @{}
        $taskTotal = [long]0
        if ($taskZip.Entries.Count -gt 20000) { throw 'Слишком много ZIP entries.' }
        foreach ($taskEntry in $taskZip.Entries) {
            $taskPath = Resolve-ScreenShortsArchivePath $Destination $taskEntry.FullName $ProjectRoot
            if ($taskNames.ContainsKey($taskPath)) { throw "Повторяющийся путь ZIP: $($taskEntry.FullName)" }
            $taskNames[$taskPath] = $true
            if ((($taskEntry.ExternalAttributes -shr 16) -band 61440) -eq 40960 -or ($taskEntry.ExternalAttributes -band 1024) -ne 0) { throw 'Ссылки в ZIP не допускаются.' }
            $taskTotal += $taskEntry.Length
            if ($taskEntry.Length -lt 0 -or $taskTotal -gt $MaxBytes) { throw 'Превышен лимит распакованного ZIP.' }
            if ($taskEntry.FullName.EndsWith('/') -or $taskEntry.FullName.EndsWith('\')) { continue }
            if (Test-Path -LiteralPath $taskPath) { throw "ZIP не перезаписывает существующий файл: $taskPath" }
        }
        foreach ($taskEntry in $taskZip.Entries) {
            $taskPath = Resolve-ScreenShortsArchivePath $Destination $taskEntry.FullName $ProjectRoot
            if ($taskEntry.FullName.EndsWith('/') -or $taskEntry.FullName.EndsWith('\')) { $null = New-ScreenShortsDirectory $taskPath $ProjectRoot; continue }
            $null = New-ScreenShortsDirectory ([IO.Path]::GetDirectoryName($taskPath)) $ProjectRoot
            $taskSource = $taskEntry.Open()
            $taskTarget = [IO.File]::Open($taskPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
            try { $taskSource.CopyTo($taskTarget); $taskTarget.Flush($true) }
            finally { $taskTarget.Dispose(); $taskSource.Dispose() }
        }
    } finally { $taskZip.Dispose() }
}

function Invoke-ScreenShortsArchiveTool {
    param([string]$Executable, [string[]]$Arguments, [string]$WorkingDirectory)
    $taskStart = [Diagnostics.ProcessStartInfo]::new()
    $taskStart.FileName = $Executable
    $taskStart.Arguments = ($Arguments | ForEach-Object { ConvertTo-ScreenShortsWindowsArgument $_ }) -join ' '
    $taskStart.WorkingDirectory = $WorkingDirectory
    $taskStart.UseShellExecute = $false
    $taskStart.CreateNoWindow = $true
    $taskStart.WindowStyle = [Diagnostics.ProcessWindowStyle]::Hidden
    $taskStart.RedirectStandardOutput = $true
    $taskStart.RedirectStandardError = $true
    $taskStart.StandardOutputEncoding = [Text.UTF8Encoding]::new($false)
    $taskStart.StandardErrorEncoding = [Text.UTF8Encoding]::new($false)
    $taskProcess = [Diagnostics.Process]::new()
    $taskProcess.StartInfo = $taskStart
    try {
        $null = $taskProcess.Start()
        $taskOutput = $taskProcess.StandardOutput.ReadToEndAsync()
        $taskError = $taskProcess.StandardError.ReadToEndAsync()
        if (-not $taskProcess.WaitForExit(120000)) {
            $taskProcess.Kill()
            $null = $taskProcess.WaitForExit(5000)
            throw 'Распаковка компонента превысила 120 секунд; операция остановлена.'
        }
        if ($taskProcess.ExitCode -ne 0) { throw "Распаковщик завершился с кодом $($taskProcess.ExitCode): $($taskError.Result)" }
        return $taskOutput.Result
    } finally { $taskProcess.Dispose() }
}

function Get-ScreenShortsCRC32 {
    param([byte[]]$Bytes, [int]$Offset, [int]$Count)
    $taskCRC = [uint32]::MaxValue
    for ($taskIndex=$Offset; $taskIndex -lt $Offset+$Count; $taskIndex++) {
        $taskCRC = $taskCRC -bxor [uint32]$Bytes[$taskIndex]
        for ($taskBit=0; $taskBit -lt 8; $taskBit++) {
            if (($taskCRC -band 1) -ne 0) { $taskCRC = ($taskCRC -shr 1) -bxor [uint32]3988292384 }
            else { $taskCRC = $taskCRC -shr 1 }
        }
    }
    return [uint32]($taskCRC -bxor [uint32]::MaxValue)
}

function Initialize-ScreenShortsArchiveTool {
    param($Manifest, [string]$Cache, [string]$Work, [string]$ProjectRoot)
    $taskBootstrapArtifact = $Manifest.artifacts | Where-Object { $_.id -eq 'sevenzip-bootstrap' }
    $taskSFXArtifact = $Manifest.artifacts | Where-Object { $_.id -eq 'sevenzip' }
    $taskBootstrap = Assert-ScreenShortsCachedArtifact $taskBootstrapArtifact $Cache $ProjectRoot
    $taskSFX = Assert-ScreenShortsCachedArtifact $taskSFXArtifact $Cache $ProjectRoot
    $null = New-ScreenShortsDirectory $Work $ProjectRoot
    $taskBytes = [IO.File]::ReadAllBytes($taskSFX)
    $taskSignature = [byte[]]@(55,122,188,175,39,28)
    $taskStart = -1
    $taskLength = 0
    for ($taskIndex=0; $taskIndex -le $taskBytes.Length-32; $taskIndex++) {
        if ($taskBytes[$taskIndex] -ne 55) { continue }
        $taskMatches = $true
        for ($taskPart=1; $taskPart -lt 6; $taskPart++) { if ($taskBytes[$taskIndex+$taskPart] -ne $taskSignature[$taskPart]) { $taskMatches=$false; break } }
        if (-not $taskMatches) { continue }
        $taskCRC = Get-ScreenShortsCRC32 $taskBytes ($taskIndex+12) 20
        if ($taskCRC -ne [BitConverter]::ToUInt32($taskBytes,$taskIndex+8)) { continue }
        $taskNext = [BitConverter]::ToUInt64($taskBytes,$taskIndex+12)
        $taskSize = [BitConverter]::ToUInt64($taskBytes,$taskIndex+20)
        if ($taskNext -gt $taskBytes.Length -or $taskSize -gt $taskBytes.Length -or 32+$taskNext+$taskSize -gt $taskBytes.Length-$taskIndex) { continue }
        $taskStart=$taskIndex; $taskLength=[int](32+$taskNext+$taskSize); break
    }
    if ($taskStart -lt 0) { throw 'В закреплённом 7-Zip SFX не найден допустимый 7z payload.' }
    $taskPayload = Assert-ScreenShortsOutputPath (Join-Path $Work '7zip-payload.7z') $ProjectRoot
    $taskFile = [IO.File]::Open($taskPayload,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
    try { $taskFile.Write($taskBytes,$taskStart,$taskLength) } finally { $taskFile.Dispose() }
    $taskToolDirectory = New-ScreenShortsDirectory (Join-Path $Work '7zip') $ProjectRoot
    $null = Invoke-ScreenShortsArchiveTool $taskBootstrap @('x','-y','-bd','-bso0','-bsp0','-sccUTF-8',('-o'+$taskToolDirectory),$taskPayload) $Work
    Assert-ScreenShortsPlainTree $taskToolDirectory $ProjectRoot 67108864
    $taskExecutable = Assert-ScreenShortsOutputPath (Join-Path $taskToolDirectory '7z.exe') $ProjectRoot
    if (-not (Test-Path -LiteralPath $taskExecutable -PathType Leaf) -or -not (Test-Path -LiteralPath (Join-Path $taskToolDirectory '7z.dll') -PathType Leaf)) { throw 'Из SFX не получен полный 7-Zip x64.' }
    return $taskExecutable
}

function Expand-ScreenShortsArchive {
    param([string]$Tool, [string]$Archive, [string]$Destination, [string]$ProjectRoot, [long]$MaxBytes = 2147483648)
    $null = New-ScreenShortsDirectory $Destination $ProjectRoot
    $taskListing = Invoke-ScreenShortsArchiveTool $Tool @('l','-slt','-ba','-bd','-sccUTF-8',$Archive) $Destination
    $taskCount = 0
    $taskBytes = [long]0
    foreach ($taskLine in ($taskListing -split '\r?\n')) {
        if ($taskLine.StartsWith('Path = ')) {
            $null = Resolve-ScreenShortsArchivePath $Destination $taskLine.Substring(7) $ProjectRoot
            $taskCount++
            if ($taskCount -gt 20000) { throw 'Слишком много файлов в архиве компонента.' }
        } elseif ($taskLine -match '^Size = ([0-9]+)$') {
            $taskBytes += [long]$Matches[1]
            if ($taskBytes -gt $MaxBytes) { throw 'Архив превышает лимит распакованных компонентов.' }
        } elseif ($taskLine -match '^(Symbolic Link|Hard Link) = .+') { throw 'Ссылки в архиве компонентов не допускаются.' }
    }
    if ($taskCount -eq 0) { throw 'Архив компонента пуст или не прочитан.' }
    $null = Invoke-ScreenShortsArchiveTool $Tool @('x','-y','-bd','-bso0','-bsp0','-sccUTF-8','-mmt=2',('-o'+$Destination),$Archive) $Destination
    Assert-ScreenShortsPlainTree $Destination $ProjectRoot $MaxBytes
}
