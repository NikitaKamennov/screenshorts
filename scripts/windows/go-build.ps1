function Invoke-ScreenShooterGoBuild {
    param([string]$ProjectRoot, [string]$Output, [ValidateSet('dev','bundle')][string]$Distribution='dev', [string]$Version='0.1.0-dev', [switch]$Offline, [string]$NoticeDirectory)
    if ($Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.]+)?$') { throw 'Некорректная версия Windows-сборки.' }
    $taskGo = Get-Command go -CommandType Application -ErrorAction Stop
    $taskOutput = Assert-ScreenShooterOutputPath $Output $ProjectRoot
    $taskCache = New-ScreenShooterDirectory (Join-Path $ProjectRoot '.local-data\build-cache\windows-x64\go-build') $ProjectRoot
    $taskModules = New-ScreenShooterDirectory (Join-Path $ProjectRoot '.local-data\build-cache\windows-x64\go-modules') $ProjectRoot
    $taskOld = @{}
    foreach ($taskName in @('GOOS','GOARCH','GOCACHE','GOMODCACHE','GOPROXY','GOSUMDB','GOTOOLCHAIN')) { $taskOld[$taskName] = [Environment]::GetEnvironmentVariable($taskName,'Process') }
    Push-Location $ProjectRoot
    try {
        $env:GOOS='windows'; $env:GOARCH='amd64'; $env:GOCACHE=$taskCache; $env:GOMODCACHE=$taskModules
        if ($Offline) { $env:GOPROXY='off'; $env:GOSUMDB='off'; $env:GOTOOLCHAIN='local' }
        $taskLinkFlags = '-s -w -H windowsgui -X main.distributionMode='+$Distribution+' -X main.buildVersion='+$Version
        & $taskGo.Source build -mod=readonly -trimpath -tags 'desktop,production' -ldflags $taskLinkFlags -o $taskOutput ./cmd/skrinpolka
        if ($LASTEXITCODE -ne 0) { throw "Сборка EXE завершилась с кодом $LASTEXITCODE; прежний выход сохранён." }
        if ($NoticeDirectory) { New-ScreenShooterGoNotices $taskGo.Source $taskOutput $taskModules $NoticeDirectory $ProjectRoot }
    } finally {
        Pop-Location
        foreach ($taskName in $taskOld.Keys) { [Environment]::SetEnvironmentVariable($taskName,$taskOld[$taskName],'Process') }
    }
}
