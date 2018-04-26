#!/bin/bash
##############################
#####Setting Environments#####
echo "Setting Environments"
set -e
export cpwd=`pwd`
export LD_LIBRARY_PATH=/usr/local/lib:/usr/lib
export PATH=$PATH:$GOPATH/bin:$HOME/bin:$GOROOT/bin
output=build
rm -rf $output
mkdir -p $output

#### Package ####
srv_name=rsck
srv_ver=1.1.0
##
srv_deamon="$srv_name"d
srv_out=$output/$srv_name
mkdir $srv_out
##build normal
echo "Build $srv_name normal executor..."
go build -o $srv_out/$srv_name github.com/sutils/rsck/rsck

###
if [ "$1" != "" ];then
	curl -o $srv_out/srvd $1/srvd
	curl -o $srv_out/srvd_i $1/srvd_i
	chmod +x $srv_out/srvd
	chmod +x $srv_out/srvd_i
	echo "./srvd_i \$1 $srv_name \$2 \$3" >$srv_out/install.sh
	chmod +x $srv_out/install.sh
fi
###
cd $output
zip -r $srv_name-$srv_ver-`uname`.zip $srv_name
cd ../
echo "Package $srv_name done..."