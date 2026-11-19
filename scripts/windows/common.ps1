# Shared by native Windows preparation/build scripts. Dot-sourcing does not
# launch any tool, application, worker or network operation.

function Assert-ScreenShortsWindows {
    if ($env:OS -ne 'Windows_NT' -or $env:PROCESSOR_ARCHITECTURE -ne 'AMD64' -or [IntPtr]::Size -ne 8) {
        throw 'Нужна нативная Windows x64 и 64-разрядный PowerShell.'
    }
}

# Check the complete existing path down to the project root before mutation.
function Assert-ScreenShortsOutputPath {
    param([string]$Path, [string]$ProjectRoot)
    $taskRoot = [IO.Path]::GetFullPath($ProjectRoot).TrimEnd([char[]]@('\', '/'))
    $taskPath = [IO.Path]::GetFullPath($Path)
    if (-not $taskPath.StartsWith($taskRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Путь вне каталога проекта: $taskPath"
    }
    $taskCurrent = $taskPath
    while ($true) {
        $taskItem = Get-Item -LiteralPath $taskCurrent -Force -ErrorAction SilentlyContinue
        if ($null -ne $taskItem) {
            if (($taskItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Junction/symlink в пути сборки: $taskCurrent"
            }
        }
        if ([string]::Equals($taskCurrent, $taskRoot, [StringComparison]::OrdinalIgnoreCase)) { break }
        $taskCurrent = [IO.Path]::GetDirectoryName($taskCurrent)
        if (-not $taskCurrent) { throw 'Не удалось проверить границы пути сборки.' }
    }
    return $taskPath
}

function New-ScreenShortsDirectory {
    param([string]$Path, [string]$ProjectRoot)
    $taskPath = Assert-ScreenShortsOutputPath $Path $ProjectRoot
    New-Item -ItemType Directory -Path $taskPath -Force | Out-Null
    return $taskPath
}

function New-ScreenShortsOwnedOutput {
    param([string]$Path, [string]$ProjectRoot, [string]$Kind)
    $taskPath = Assert-ScreenShortsOutputPath $Path $ProjectRoot
    if (Test-Path -LiteralPath $taskPath) { throw "Временный выход уже существует: $taskPath" }
    New-Item -ItemType Directory -Path $taskPath | Out-Null
    $taskOwner = [ordered]@{
        schema = 'screenshorts-windows-output-v1'
        project_root = [IO.Path]::GetFullPath($ProjectRoot)
        kind = $Kind
    }
    $taskMarker = Join-Path $taskPath '.screenshorts-output.json'
    [IO.File]::WriteAllText($taskMarker, ($taskOwner | ConvertTo-Json) + [Environment]::NewLine, [Text.UTF8Encoding]::new($false))
    return $taskPath
}

function Assert-ScreenShortsOwnedOutput {
    param([string]$Path, [string]$ProjectRoot, [string]$Kind)
    $taskPath = Assert-ScreenShortsOutputPath $Path $ProjectRoot
    if (-not (Test-Path -LiteralPath $taskPath -PathType Container)) { throw "Нет выходного каталога: $taskPath" }
    $taskMarker = Assert-ScreenShortsOutputPath (Join-Path $taskPath '.screenshorts-output.json') $ProjectRoot
    if (-not (Test-Path -LiteralPath $taskMarker -PathType Leaf)) {
        throw "Каталог без маркера сборщика не изменён: $taskPath. Сохрани/перемести прежнюю папку вручную и повтори сборку."
    }
    if ((Get-Item -LiteralPath $taskMarker).Length -gt 4096) { throw "Некорректный маркер выхода: $taskMarker" }
    $taskOwner = Get-Content -LiteralPath $taskMarker -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($taskOwner.schema -ne 'screenshorts-windows-output-v1' -or $taskOwner.kind -ne $Kind -or
        -not [string]::Equals($taskOwner.project_root, [IO.Path]::GetFullPath($ProjectRoot), [StringComparison]::OrdinalIgnoreCase)) {
        throw "Каталог принадлежит другой сборке: $taskPath"
    }
    return $taskPath
}

function Remove-ScreenShortsOwnedOutput {
    param([string]$Path, [string]$ProjectRoot, [string]$Kind)
    $taskPath = Assert-ScreenShortsOwnedOutput $Path $ProjectRoot $Kind
    # Walk without following reparse points before a recursive delete. Unknown
    # links stop cleanup; they are never traversed through another shell.
    $taskPending = [Collections.Generic.Stack[string]]::new()
    $taskPending.Push($taskPath)
    while ($taskPending.Count -gt 0) {
        $taskDirectory = $taskPending.Pop()
        foreach ($taskItem in Get-ChildItem -LiteralPath $taskDirectory -Force) {
            if (($taskItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Очистка остановлена: junction/symlink в $($taskItem.FullName)"
            }
            if ($taskItem.PSIsContainer) { $taskPending.Push($taskItem.FullName) }
        }
    }
    $null = Assert-ScreenShortsOutputPath $taskPath $ProjectRoot
    Remove-Item -LiteralPath $taskPath -Recurse -Force
}

function Open-ScreenShortsBuildLock {
    param([string]$Path, [string]$ProjectRoot)
    $taskPath = Assert-ScreenShortsOutputPath $Path $ProjectRoot
    try { return [IO.File]::Open($taskPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None) }
    catch { throw "Выход занят другой сборкой либо блокировка недоступна: $taskPath. $($_.Exception.Message)" }
}

function Assert-ScreenShortsDevOutput {
    param([string]$Path, [string]$ProjectRoot)
    $taskPath = Assert-ScreenShortsOwnedOutput $Path $ProjectRoot 'dev'
    foreach ($taskItem in Get-ChildItem -LiteralPath $taskPath -Force) {
        if (($taskItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Junction/symlink в dev-выходе: $($taskItem.FullName)"
        }
        if ($taskItem.Name -notin @('.screenshorts-output.json', 'ScreenShortsPro.exe', 'config')) {
            throw "В dev-каталоге есть дополнительные файлы: $($taskItem.FullName). Сохрани/перемести их вручную; сборщик их не удаляет."
        }
    }
    $taskConfig = Assert-ScreenShortsOutputPath (Join-Path $taskPath 'config') $ProjectRoot
    if (Test-Path -LiteralPath $taskConfig) {
        foreach ($taskItem in Get-ChildItem -LiteralPath $taskConfig -Force) {
            if (($taskItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Junction/symlink в dev-конфиге: $($taskItem.FullName)"
            }
            if ($taskItem.Name -ne 'runtime-windows.json' -or $taskItem.PSIsContainer) {
                throw "В dev-конфиге есть дополнительные файлы: $($taskItem.FullName). Они не будут удалены сборщиком."
            }
        }
    }
}

function Assert-ScreenShortsExecutableStopped {
    param([string]$Executable)
    $taskExpected = [IO.Path]::GetFullPath($Executable)
    foreach ($taskProcess in Get-Process -Name 'ScreenShortsPro' -ErrorAction SilentlyContinue) {
        try { $taskProcessPath = $taskProcess.Path }
        catch { throw "Не удалось определить путь ScreenShortsPro PID $($taskProcess.Id); закрой приложение перед сборкой." }
        if (-not $taskProcessPath) { throw 'Не удалось определить путь работающего ScreenShortsPro; закрой приложение перед сборкой.' }
        if ([string]::Equals($taskProcessPath, $taskExpected, [StringComparison]::OrdinalIgnoreCase)) {
            throw "Приложение запущено: $taskExpected. Выбери «Выйти» и повтори сборку."
        }
    }
}

function ConvertTo-ScreenShortsWindowsArgument {
    param([string]$Value)
    # Double backslashes before quotes and at the end of a quoted argument.
    $taskEscaped = [regex]::Replace($Value, '(\\*)"', '$1$1\"')
    $taskEscaped = [regex]::Replace($taskEscaped, '(\\+)$', '$1$1')
    return '"' + $taskEscaped + '"'
}

function Publish-ScreenShortsOwnedOutput {
    param([string]$Stage, [string]$Destination, [string]$ProjectRoot, [string]$Kind)
    $taskStage = Assert-ScreenShortsOwnedOutput $Stage $ProjectRoot $Kind
    $taskDestination = Assert-ScreenShortsOutputPath $Destination $ProjectRoot
    $taskBackup = $null
    if (Test-Path -LiteralPath $taskDestination) {
        $null = Assert-ScreenShortsOwnedOutput $taskDestination $ProjectRoot $Kind
        $taskBackup = Assert-ScreenShortsOutputPath ($taskDestination + '.backup-' + [Guid]::NewGuid().ToString('N')) $ProjectRoot
        Move-Item -LiteralPath $taskDestination -Destination $taskBackup
    }
    try { Move-Item -LiteralPath $taskStage -Destination $taskDestination }
    catch {
        if ($taskBackup -and -not (Test-Path -LiteralPath $taskDestination)) {
            $null = Assert-ScreenShortsOwnedOutput $taskBackup $ProjectRoot $Kind
            $null = Assert-ScreenShortsOutputPath $taskDestination $ProjectRoot
            Move-Item -LiteralPath $taskBackup -Destination $taskDestination
        }
        throw
    }
    if ($taskBackup) {
        try { Remove-ScreenShortsOwnedOutput $taskBackup $ProjectRoot $Kind }
        catch { Write-Warning "Предыдущий выход оставлен в $taskBackup. $($_.Exception.Message)" }
    }
    return $taskDestination
}
