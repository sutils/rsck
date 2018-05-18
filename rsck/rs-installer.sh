#!/bin/bash
case "$1" in
  runner)
    useradd rs-runner
    cp -f rs-runner.service /etc/systemd/system/
    systemctl enable rs-runner.service
    ;;
  server)
    useradd rs-server
    cp -f rs-server.service /etc/systemd/system/
    systemctl enable rs-server.service
    ;;
  *)
    echo "Usage: ./rs-installer.sh (runner|server)"
    ;;
esac