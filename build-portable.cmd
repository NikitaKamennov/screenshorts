@echo off
setlocal
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\build-windows.ps1" -Portable %*
set "SCREENSHORTS_BUILD_EXIT=%ERRORLEVEL%"
if not "%SCREENSHORTS_BUILD_EXIT%"=="0" echo Build failed. Exit code: %SCREENSHORTS_BUILD_EXIT%
if /i not "%SCREENSHORTS_NO_PAUSE%"=="1" pause
exit /b %SCREENSHORTS_BUILD_EXIT%
