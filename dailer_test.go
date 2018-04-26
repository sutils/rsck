package rsck

import (
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/Centny/gwf/util"
)

func TestCmdDailer(t *testing.T) {
	cmd := NewCmdDailer()
	raw, err := cmd.Dail(10, "tcp://cmd?exec=bash")
	if err != nil {
		t.Error(err)
		return
	}
	go io.Copy(os.Stdout, raw)
	fmt.Fprintf(raw, "ls\n")
	fmt.Fprintf(raw, "ls /tmp/\n")
	fmt.Fprintf(raw, "echo abc\n")
	time.Sleep(time.Second)
}

func TestWebDailer(t *testing.T) {
	l, err := net.Listen("tcp", ":2422")
	if err != nil {
		t.Error(err)
		return
	}
	dailer := NewWebDailer()
	dailer.Bootstrap()
	go func() {
		var cid uint32
		for {
			con, err := l.Accept()
			if err != nil {
				break
			}
			raw, err := dailer.Dail(cid, "tcp://web?dir=/tmp")
			if err != nil {
				panic(err)
			}
			go func() {
				buf := make([]byte, 1024)
				for {
					n, err := raw.Read(buf)
					if err != nil {
						con.Close()
						break
					}
					con.Write(buf[0:n])
				}
			}()
			go io.Copy(raw, con)
		}
	}()
	fmt.Println(util.HGet("http://localhost:2422/"))
}
