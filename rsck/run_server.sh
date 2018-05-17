#!/bin/bash
go run main.go -s -auth cny:sco -acl abc=123 -showlog=1 -f 'tcp://:2425<abc>tcp://cmd?exec=cmd' # -f 'tcp://:13389<abc>tcp://localhost:3389' -f 'tcp://:2426<abc>tcp://web?dir=C:\'