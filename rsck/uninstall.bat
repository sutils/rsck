@echo off
cd /d %~dp0
nssm stop "RSck Runner"
nssm remove "RSck Runner" confirm
pause