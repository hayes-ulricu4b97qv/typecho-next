@echo off
setlocal

if not defined APP_PORT set "APP_PORT=8080"
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
"%~dp0typecho-next.exe"
exit /b %errorlevel%
