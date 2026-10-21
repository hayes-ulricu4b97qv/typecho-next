@echo off
setlocal

if not defined APP_PORT set "APP_PORT=8081"
if not defined APP_LISTEN_HOST set "APP_LISTEN_HOST=127.0.0.1"
if not defined APP_DATA_DIR (
    if not defined LOCALAPPDATA (
        echo LOCALAPPDATA is unavailable. Set APP_DATA_DIR before starting Typecho Next.
        exit /b 1
    )
    set "APP_DATA_DIR=%LOCALAPPDATA%\TypechoNext\data"
)
if not defined APP_MEDIA_DIR set "APP_MEDIA_DIR=%APP_DATA_DIR%\uploads"
if not defined APP_WEB_DIR set "APP_WEB_DIR=%~dp0web\dist"

if not exist "%APP_DATA_DIR%" mkdir "%APP_DATA_DIR%"
if errorlevel 1 exit /b %errorlevel%
if not exist "%APP_MEDIA_DIR%" mkdir "%APP_MEDIA_DIR%"
if errorlevel 1 exit /b %errorlevel%

cd /d "%~dp0"
set "APP_EXE=%~dp0typecho-next.exe"
if not exist "%APP_EXE%" if exist "%~dp0bin\typecho-next.exe" set "APP_EXE=%~dp0bin\typecho-next.exe"
if not exist "%APP_EXE%" (
    echo Could not find typecho-next.exe in this folder or its bin subfolder.
    pause
    exit /b 1
)

echo Starting Typecho Next at http://127.0.0.1:%APP_PORT%/setting
"%APP_EXE%"
set "APP_EXIT_CODE=%ERRORLEVEL%"
if not "%APP_EXIT_CODE%"=="0" (
    echo.
    echo Typecho Next exited with code %APP_EXIT_CODE%.
    pause
)
exit /b %APP_EXIT_CODE%
