#!/bin/bash
##############################
#####Setting Environments#####
echo "Setting Environments"
set -e
export cpwd=`pwd`
export LD_LIBRARY_PATH=/usr/local/lib:/usr/lib
export PATH=$PATH:$GOPATH/bin:$HOME/bin:$GOROOT/bin
output=build

#### Package ####
srv_name=rsck
srv_ver=1.4.5
##
srv_deamon="$srv_name"d
srv_out=$output/$srv_name

rm -rf $srv_out
mkdir $srv_out
##build normal
echo "Build $srv_name normal executor..."
go build -o $srv_out/$srv_name github.com/sutils/rsck/rsck

###
mkdir $srv_out/certs/
echo "make server cert"
openssl req -new -nodes -x509 -out $srv_out/certs/server.pem -keyout $srv_out/certs/server.key -days 3650 -subj "/C=CN/ST=NRW/L=Earth/O=Random Company/OU=IT/CN=rsck.dyang.org/emailAddress=cert@dyang.org"
echo "make client cert"
openssl req -new -nodes -x509 -out $srv_out/certs/client.pem -keyout $srv_out/certs/client.key -days 3650 -subj "/C=CN/ST=NRW/L=Earth/O=Random Company/OU=IT/CN=rsck.dyang.org/emailAddress=cert@dyang.org"

###
cp -f rs-* $srv_out/

###
cd $output
zip -r $srv_name-$srv_ver-`uname`.zip $srv_name
cd ../
echo "Package $srv_name done..."