@echo off
cd /d %~dp0
set /p name=���������ƣ�
set /p token=���������룺
mkdir logs
nssm install "RSck Runner" %CD%\rsck.exe -r -server rsck.snows.io:8241 -name %name% -token %token%
nssm set "RSck Runner" AppStdout %CD%\logs\out.log
nssm set "RSck Runner" AppStderr %CD%\logs\err.log
nssm start "RSck Runner"
pause