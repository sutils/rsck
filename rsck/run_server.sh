#!/bin/bash
go run main.go -s -auth cny:sco -acl abc=123 -f 'tcp://:8082>abc://localhost:80' -f "tcp://:8083>abc://14.23.162.171:8389"