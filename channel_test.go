package rsck

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

func TestChannel(t *testing.T) {
	server := NewChannelServer(":2832", "Server")
	server.ACL["^[ax.*$"] = "abc"
	server.ACL["^test.*$"] = "abc"
	err := server.Start()
	if err != nil {
		t.Error(err)
		return
	}
	runner := NewChannelRunner("localhost:2832", "test0", "abc")
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
	err = server.AddForward("tcp", ":2831", "test0", "localhost:2833", 0)
	if err != nil {
		t.Error(err)
		return
	}
	err = server.AddForward("tcp", ":2830", "test0", "localhost:2834", 0)
	if err != nil {
		t.Error(err)
		return
	}
	{
		con, err := net.Dial("tcp", "localhost:2831")
		if err != nil {
			t.Error(err)
			return
		}
		wait := sync.WaitGroup{}
		wait.Add(100)
		go func() {
			buf := make([]byte, 5)
			for {
				readed, err := con.Read(buf)
				if err != nil {
					break
				}
				if readed != len(buf) {
					break
				}
				// fmt.Println("->", string(buf[:readed]))
				wait.Done()
			}
		}()
		for i := 0; i < 100; i++ {
			con.Write([]byte("val-x"))
			time.Sleep(time.Millisecond)
		}
		wait.Wait()
		con.Close()
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
