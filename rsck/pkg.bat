@echo off
set srv_name=rsck
set srv_ver=1.2.0
del /Q /S build\%srv_name%
mkdir build
mkdir build\%srv_name%
go build -o build\rsck\rsck.exe github.com/sutils/rsck/rsck
reg Query "HKLM\Hardware\Description\System\CentralProcessor\0" | find /i "x86" > NUL && set OS=x86||set OS=x64
xcopy win-%OS%\nssm.exe build\%srv_name%
xcopy configure.bat build\%srv_name%
xcopy install.bat build\%srv_name%
xcopy uninstall.bat build\%srv_name%
mkdir build\%srv_name%\certs
echo "make server cert"
openssl req -new -nodes -x509 -out build\%srv_name%\certs\server.pem -keyout build\%srv_name%\certs\server.key -days 3650 -subj "/C=CN/ST=NRW/L=Earth/O=Random Company/OU=IT/CN=rsck.dyang.org/emailAddress=cert@dyang.org"
echo "make client cert"
openssl req -new -nodes -x509 -out build\%srv_name%\certs\client.pem -keyout build\%srv_name%\certs\client.key -days 3650 -subj "/C=CN/ST=NRW/L=Earth/O=Random Company/OU=IT/CN=rsck.dyang.org/emailAddress=cert@dyang.org"
cd build
7z a -r %srv_name%-%srv_ver%-Win-%OS%.zip %srv_name%
cd ..\
goto :esuccess

:efail
echo "Build fail"
exit 1

:esuccess
echo "Build success"