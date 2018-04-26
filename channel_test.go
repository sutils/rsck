package rsck

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Centny/gwf/netw"
)

func TestChannel(t *testing.T) {
	netw.MOD_MAX_SIZE = 4
	// MOD_MAX_SIZE = 4
	// netw.ShowLog = true
	// netw.ShowLog_C = true
	server := NewChannelServer(":2832", "Server")
	server.ACL["^[ax.*$"] = "abc"
	server.ACL["^test.*$"] = "abc"
	err := server.Start()
	if err != nil {
		t.Error(err)
		return
	}
	runner := NewChannelRunner("localhost:2832", "test0", "abc")
	runner.AddDailer(NewTCPDailer())
	runner.Start()
	time.Sleep(time.Second)
	//
	echo, err := NewEchoServer("tcp", ":2833")
	if err != nil {
		t.Error(err)
		return
	}
	echo.Start()
	//
	err = server.AddUriForward("tcp://:2831<test0>tcp://localhost:2833")
	if err != nil {
		t.Error(err)
		return
	}
	err = server.AddUriForward("tcp://:2830<test0>tcp://localhost:2834")
	if err != nil {
		t.Error(err)
		return
	}
	{
		wg := sync.WaitGroup{}
		wg.Add(100)
		for i := 0; i < 100; i++ {
			go func() {
				con, err := net.Dial("tcp", "localhost:2831")
				if err != nil {
					t.Error(err)
					return
				}
				allwrited := 0
				allreaded := 0
				go func() {
					buf := make([]byte, 10240)
					for {
						readed, err := con.Read(buf)
						if err != nil {
							break
						}
						allreaded += readed
						// fmt.Println("->", string(buf[:readed]))
						fmt.Printf("allreaded:%d,allwrited:%d\n", allreaded, allwrited)
					}
				}()
				for i := 0; i < 100; i++ {
					bys := make([]byte, rand.Int()%1024)
					allwrited += len(bys)
					con.Write(bys)
				}
				for allreaded < allwrited {
					time.Sleep(time.Millisecond)
				}
				con.Close()
				wg.Done()
			}()
		}
		wg.Wait()
	}
	{
		con, err := net.Dial("tcp", "localhost:2830")
		if err != nil {
			t.Error(err)
			return
		}
		buf := make([]byte, 5)
		_, err = con.Read(buf)
		if err == nil {
			t.Error(err)
			return
		}
	}
	{
		fmt.Println("---->")
		runner2 := NewChannelRunner("localhost:2832", "txss", "abcx")
		runner2.Start()
		time.Sleep(time.Second)
	}
	time.Sleep(time.Second)
}
