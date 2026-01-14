@echo off
echo ================================
echo   FIGHT CLUB - GO ENGINE
echo ================================

REM Load environment variables from parent .env file
for /f "tokens=1,2 delims==" %%a in ('type "..\\.env" 2^>nul ^| findstr /v "^#"') do (
    set "%%a=%%b"
)

echo Starting server...
echo Admin Panel: http://localhost:3000/admin
echo.

fight-club.exe

pause
