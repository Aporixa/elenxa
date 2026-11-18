@echo off
rem Elenxa start script (Windows)
rem
rem Usage:
rem   start.bat                         run in the foreground (Ctrl-C to stop)
rem   start.bat -addr 127.0.0.1:9000    extra args are passed through to elenxa
rem
rem Hardened local build: there is no self-update and no restart-on-exit-75
rem loop. The script just runs elenxa.exe once and returns its exit code.

setlocal
cd /d "%~dp0"

set "BIN=elenxa.exe"
if not exist "%BIN%" (
	echo [elenxa] executable not found: %BIN% 1>&2
	exit /b 1
)

"%BIN%" %*
exit /b %ERRORLEVEL%
