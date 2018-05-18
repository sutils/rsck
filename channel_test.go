package rsck

import (
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Centny/gwf/routing"

	"github.com/Centny/gwf/routing/httptest"

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
	server.WebSuffix = ".loc"
	err := server.Start()
	if err != nil {
		t.Error(err)
		return
	}
	runner := NewChannelRunner("localhost:2832", "test0", "abc")
	runner.AddDailer(NewWebDailer())
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
	err = server.AddUriForward("web://loctest0<test0>http://web?dir=/tmp")
	if err != nil {
		t.Error(err)
		return
	}
	err = server.AddUriForward("web://loctest2<test0>http://192.168.1.1")
	if err != nil {
		t.Error(err)
		return
	}
	err = server.AddUriForward("web://loctest1<test0>https://w.dyang.org")
	if err != nil {
		t.Error(err)
		return
	}
	err = server.AddUriForward("web://loctest3<test1>http://192.168.1.1")
	if err != nil {
		t.Error(err)
		return
	}
	{ //test web forward
		ts := httptest.NewServer(func(hs *routing.HTTPSession) routing.HResult {
			if hs.R.URL.Path == "/web" {
				return server.ListWebForward(hs)
			}
			name := strings.TrimPrefix(hs.R.URL.Path, "/web/")
			switch name {
			case "loctest0":
				hs.R.Host = "loctest0.loc"
			case "loctest1":
				hs.R.Host = "loctest1.loc"
			case "loctest2":
				hs.R.Host = "loctest2.loc"
			case "loctest3":
				hs.R.Host = "loctest3.loc"
			}
			hs.R.URL.Path = "/"
			return server.ProcWebForward(hs)
		})
		//
		data, err := ts.G("/web")
		if err != nil {
			t.Errorf("%v-%v", err, data)
			return
		}
		fmt.Printf("data->:\n%v\n\n\n\n", data)
		//
		data, err = ts.G("/web/loctest0")
		if err != nil {
			t.Errorf("%v-%v", err, data)
			return
		}
		fmt.Printf("data->:\n%v\n\n\n\n", data)
		//
		data, err = ts.G("/web/loctest1")
		if err != nil {
			t.Errorf("%v-%v", err, data)
			return
		}
		fmt.Printf("data->:\n%v\n\n\n\n", data)
		//
		data, err = ts.G("/web/loctest2")
		if err != nil {
			t.Errorf("%v-%v", err, data)
			return
		}
		fmt.Printf("data->:\n%v\n\n\n\n", data)
		//
		data, err = ts.G("/web/loctest3")
		if err == nil {
			t.Errorf("%v-%v", err, data)
			return
		}
		fmt.Printf("data->:\n%v\n\n\n\n", data)
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
						//fmt.Printf("allreaded:%d,allwrited:%d\n", allreaded, allwrited)
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
