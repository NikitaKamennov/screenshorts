@echo off
setlocal
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\build-windows.ps1" -Bundle %*
set "SCREENSHOOTER_BUILD_EXIT=%ERRORLEVEL%"
if not "%SCREENSHOOTER_BUILD_EXIT%"=="0" echo Build failed. Exit code: %SCREENSHOOTER_BUILD_EXIT%
if /i not "%SCREENSHOOTER_NO_PAUSE%"=="1" pause
exit /b %SCREENSHOOTER_BUILD_EXIT%
