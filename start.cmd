@echo off
chcp 65001 >nul
setlocal
REM Стартовый скрипт tunnelctl для Windows. Реальная логика находится в start.ps1.
set SCRIPT_DIR=%~dp0
powershell -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_DIR%start.ps1"
if errorlevel 1 (
  echo.
  echo Ошибка запуска. Попробуй открыть PowerShell в этой папке и выполнить:
  echo   powershell -NoProfile -ExecutionPolicy Bypass -File .\start.ps1
  pause
  exit /b 1
)
pause
