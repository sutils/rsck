#!/bin/bash
# call this script with an email address (valid or not).
# like:
# ./makecert.sh joe@random.com
mkdir certs
rm certs/*
echo "make server cert"
openssl req -new -nodes -x509 -out certs/server.pem -keyout certs/server.key -days 3650 -subj "/C=CN/ST=NRW/L=Earth/O=Random Company/OU=IT/CN=rsck.dyang.org/emailAddress=$1"
echo "make client cert"
openssl req -new -nodes -x509 -out certs/client.pem -keyout certs/client.key -days 3650 -subj "/C=CN/ST=NRW/L=Earth/O=Random Company/OU=IT/CN=rsck.dyang.org/emailAddress=$1"
