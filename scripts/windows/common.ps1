# Shared by native Windows preparation/build scripts. Dot-sourcing does not
# launch any tool, application, worker or network operation.

function Assert-ScreenShooterWindows {
    if ($env:OS -ne 'Windows_NT' -or $env:PROCESSOR_ARCHITECTURE -ne 'AMD64' -or [IntPtr]::Size -ne 8) {
        throw 'Нужна нативная Windows x64 и 64-разрядный PowerShell.'
    }
}

# Check the complete existing path down to the project root before mutation.
function Assert-ScreenShooterOutputPath {
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

function New-ScreenShooterDirectory {
    param([string]$Path, [string]$ProjectRoot)
    $taskPath = Assert-ScreenShooterOutputPath $Path $ProjectRoot
    New-Item -ItemType Directory -Path $taskPath -Force | Out-Null
    return $taskPath
}

function New-ScreenShooterOwnedOutput {
    param([string]$Path, [string]$ProjectRoot, [string]$Kind)
    $taskPath = Assert-ScreenShooterOutputPath $Path $ProjectRoot
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

function Assert-ScreenShooterOwnedOutput {
    param([string]$Path, [string]$ProjectRoot, [string]$Kind)
    $taskPath = Assert-ScreenShooterOutputPath $Path $ProjectRoot
    if (-not (Test-Path -LiteralPath $taskPath -PathType Container)) { throw "Нет выходного каталога: $taskPath" }
    $taskMarker = Assert-ScreenShooterOutputPath (Join-Path $taskPath '.screenshorts-output.json') $ProjectRoot
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

function Remove-ScreenShooterOwnedOutput {
    param([string]$Path, [string]$ProjectRoot, [string]$Kind)
    $taskPath = Assert-ScreenShooterOwnedOutput $Path $ProjectRoot $Kind
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
    $null = Assert-ScreenShooterOutputPath $taskPath $ProjectRoot
    Remove-Item -LiteralPath $taskPath -Recurse -Force
}

function Open-ScreenShooterBuildLock {
    param([string]$Path, [string]$ProjectRoot)
    $taskPath = Assert-ScreenShooterOutputPath $Path $ProjectRoot
    try { return [IO.File]::Open($taskPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None) }
    catch { throw "Выход занят другой сборкой либо блокировка недоступна: $taskPath. $($_.Exception.Message)" }
}

function Assert-ScreenShooterDevOutput {
    param([string]$Path, [string]$ProjectRoot)
    $taskPath = Assert-ScreenShooterOwnedOutput $Path $ProjectRoot 'dev'
    foreach ($taskItem in Get-ChildItem -LiteralPath $taskPath -Force) {
        if (($taskItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Junction/symlink в dev-выходе: $($taskItem.FullName)"
        }
        if ($taskItem.Name -notin @('.screenshorts-output.json', 'ScreenShooterPro.exe', 'ScreenShortsPro.exe', 'config')) {
            throw "В dev-каталоге есть дополнительные файлы: $($taskItem.FullName). Сохрани/перемести их вручную; сборщик их не удаляет."
        }
    }
    $taskConfig = Assert-ScreenShooterOutputPath (Join-Path $taskPath 'config') $ProjectRoot
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

function Assert-ScreenShooterExecutableStopped {
    param([string]$Executable)
    $taskExpected = [IO.Path]::GetFullPath($Executable)
    foreach ($taskProcess in Get-Process -Name 'ScreenShooterPro','ScreenShortsPro' -ErrorAction SilentlyContinue) {
        try { $taskProcessPath = $taskProcess.Path }
        catch { throw "Не удалось определить путь ScreenShooterPro PID $($taskProcess.Id); закрой приложение перед сборкой." }
        if (-not $taskProcessPath) { throw 'Не удалось определить путь работающего ScreenShooterPro; закрой приложение перед сборкой.' }
        if ([string]::Equals($taskProcessPath, $taskExpected, [StringComparison]::OrdinalIgnoreCase)) {
            throw "Приложение запущено: $taskExpected. Выбери «Выйти» и повтори сборку."
        }
    }
}

function ConvertTo-ScreenShooterWindowsArgument {
    param([string]$Value)
    # Double backslashes before quotes and at the end of a quoted argument.
    $taskEscaped = [regex]::Replace($Value, '(\\*)"', '$1$1\"')
    $taskEscaped = [regex]::Replace($taskEscaped, '(\\+)$', '$1$1')
    return '"' + $taskEscaped + '"'
}

function Publish-ScreenShooterOwnedOutput {
    param([string]$Stage, [string]$Destination, [string]$ProjectRoot, [string]$Kind)
    $taskStage = Assert-ScreenShooterOwnedOutput $Stage $ProjectRoot $Kind
    $taskDestination = Assert-ScreenShooterOutputPath $Destination $ProjectRoot
    $taskBackup = $null
    if (Test-Path -LiteralPath $taskDestination) {
        $null = Assert-ScreenShooterOwnedOutput $taskDestination $ProjectRoot $Kind
        $taskBackup = Assert-ScreenShooterOutputPath ($taskDestination + '.backup-' + [Guid]::NewGuid().ToString('N')) $ProjectRoot
        Move-Item -LiteralPath $taskDestination -Destination $taskBackup
    }
    try { Move-Item -LiteralPath $taskStage -Destination $taskDestination }
    catch {
        if ($taskBackup -and -not (Test-Path -LiteralPath $taskDestination)) {
            $null = Assert-ScreenShooterOwnedOutput $taskBackup $ProjectRoot $Kind
            $null = Assert-ScreenShooterOutputPath $taskDestination $ProjectRoot
            Move-Item -LiteralPath $taskBackup -Destination $taskDestination
        }
        throw
    }
    if ($taskBackup) {
        try { Remove-ScreenShooterOwnedOutput $taskBackup $ProjectRoot $Kind }
        catch { Write-Warning "Предыдущий выход оставлен в $taskBackup. $($_.Exception.Message)" }
    }
    return $taskDestination
}
