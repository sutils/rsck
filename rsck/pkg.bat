@echo off
set srv_name=rsck
set srv_ver=1.1.0
del /Q /S build
mkdir build
mkdir build\%srv_name%
go build -o build\rsck\rsck.exe github.com/sutils/rsck/rsck
reg Query "HKLM\Hardware\Description\System\CentralProcessor\0" | find /i "x86" > NUL && set OS=x86 || set OS=x64
xcopy win-%OS%\nssm.exe build\%srv_name%
xcopy configure.bat build\%srv_name%
xcopy install.bat build\%srv_name%
xcopy uninstall.bat build\%srv_name%
cd build
zip -r %srv_name%-%srv_ver%-Win-%OS%.zip %srv_name%
cd ..\
goto :esuccess

:efail
echo "Build fail"
exit 1

:esuccess
echo "Build success"