package rsck

import (
	"fmt"
	"io"
	"math/rand"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Centny/gwf/netw/impl"

	"github.com/Centny/gwf/util"

	"github.com/Centny/gwf/pool"

	"github.com/Centny/gwf/routing"

	"github.com/Centny/gwf/routing/httptest"

	"github.com/Centny/gwf/netw"
)

func TestForward(t *testing.T) {
	forward, err := NewForward("tcp://:2332?a=1<axx>tcp://localhost:2322?b=2")
	if err != nil {
		t.Error(err)
		return
	}
	if forward.Name != "axx" || forward.Local.Host != ":2332" || forward.Remote.Host != "localhost:2322" {
		t.Error("error")
		return
	}
	var a, b int
	err = forward.LocalValidF("a,R|I,R:0", &a)
	if err != nil || a != 1 {
		t.Error(err)
		return
	}
	err = forward.RemoteValidF("b,R|I,R:0", &b)
	if err != nil || b != 2 {
		t.Error(err)
		return
	}
	_, err = NewForward("tcp://:2332?a=1")
	if err == nil {
		t.Error(err)
		return
	}
	fs := []*Forward{}
	forward, _ = NewForward("tcp://1234<a>tcp://localhost:2234")
	fs = append(fs, forward)
	forward, _ = NewForward("tcp://1235<a>tcp://localhost:2235")
	fs = append(fs, forward)
	forward, _ = NewForward("tcp://1236<b>tcp://localhost:2235")
	fs = append(fs, forward)
	sort.Sort(ForwardSorter(fs))
}

func TestChannel(t *testing.T) {
	ShowLog = 3
	netw.MOD_MAX_SIZE = 4
	// MOD_MAX_SIZE = 4
	// netw.ShowLog = true
	// netw.ShowLog_C = true
	server := NewChannelServer(":2832", "Server")
	server.ACL["^[ax.*$"] = "abc"
	server.ACL["^test.*$"] = "abc"
	server.WebSuffix = ".loc"
	server.HbDelay = 2000
	err := server.Start()
	if err != nil {
		t.Error(err)
		return
	}
	runner := NewChannelRunner("localhost:2832", "test0", "abc")
	runner.R.Tick = 1000
	runner.AddDailer(NewWebDailer())
	runner.AddDailer(NewTCPDailer())
	runner.Start()
	runner2 := NewChannelRunner("localhost:2832", "test1", "abc")
	runner2.R.TickData = nil //disable tick
	runner2.AddDailer(NewWebDailer())
	runner2.AddDailer(NewTCPDailer())
	runner2.Start()
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
	err = server.AddUriForward("web://loctest2<test0>http://127.0.0.1")
	if err != nil {
		t.Error(err)
		return
	}
	err = server.AddUriForward("web://loctest1<test0>https://www.kuxiao.cn")
	if err != nil {
		t.Error(err)
		return
	}
	err = server.AddUriForward("web://loctest3<testx>http://192.168.1.1")
	if err != nil {
		t.Error(err)
		return
	}
	{ //test list all forward
		ns, fs := server.AllForwards()
		if len(ns) != 3 || len(fs) != 3 {
			t.Errorf("ns:%v,fs:%v", ns, fs)
		}
	}
	{ //test web forward
		ts := httptest.NewServer(func(hs *routing.HTTPSession) routing.HResult {
			// if hs.R.URL.Path == "/web" {
			// 	return server.ListWebForward(hs)
			// }
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
			case "loctest4":
				hs.R.Host = "loctest4.loc"
			}
			hs.R.URL.Path = "/"
			return server.ProcWebForward(hs)
		})
		//
		// data, err := ts.G("/web")
		// if err != nil {
		// 	t.Errorf("%v-%v", err, data)
		// 	return
		// }
		// fmt.Printf("data->:\n%v\n\n\n\n", data)
		//
		data, err := ts.G("/web/loctest0")
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
		//
		data, err = ts.G("/web/loctest4")
		if err == nil {
			t.Errorf("%v-%v", err, data)
			return
		}
		fmt.Printf("data->:\n%v\n\n\n\n", data)
		//
		server.WebAuth = "test:123"
		data, err = ts.G("/web/loctest3")
		if err == nil {
			t.Errorf("%v-%v", err, data)
			return
		}
		fmt.Printf("data->:\n%v\n\n\n\n", data)
		server.WebAuth = ""
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
	{ //test forward limit
		err = server.AddUriForward("tcp://:23221?limit=1<test0>tcp://localhost:2833")
		if err != nil {
			t.Error(err)
			return
		}
		con, err := net.Dial("tcp", "localhost:23221")
		if err != nil {
			t.Error(err)
			return
		}
		fmt.Fprintf(con, "value-%v", 1)
		buf := make([]byte, 100)
		readed, err := con.Read(buf)
		if err != nil {
			t.Error(err)
			return
		}
		if string(buf[0:readed]) != "value-1" {
			t.Error("error")
			return
		}
		_, err = net.Dial("tcp", "localhost:23221")
		if err == nil {
			t.Error(err)
			return
		}
		time.Sleep(200 * time.Millisecond)
		err = server.RemoveForward("tcp://:23221")
		if err == nil {
			t.Error(err)
			return
		}
	}
	{ //add/remove forward
		err = server.AddUriForward("tcp://:24221<test0>tcp://localhost:2422")
		if err != nil {
			t.Error(err)
			return
		}
		err = server.AddUriForward("tcp://:24221<test0>tcp://localhost:2422") //repeat
		if err == nil {
			t.Error(err)
			return
		}
		//
		err = server.AddUriForward("tcp://:2322?limit=xxx<test0>tcp://localhost:2422")
		if err != nil {
			t.Error(err)
			return
		}
		//
		err = server.AddUriForward("web://loc1<test0>http://localhost:2422")
		if err != nil {
			t.Error(err)
			return
		}
		err = server.AddUriForward("web://loc1<test0>http://localhost:2422") //repeat
		if err == nil {
			t.Error(err)
			return
		}
		//
		err = server.AddUriForward("xxxx://:24221<test0>tcp://localhost:2422") //not suppored
		if err == nil {
			t.Error(err)
			return
		}
		//
		err = server.RemoveForward("tcp://:24221")
		if err != nil {
			t.Error(err)
			return
		}
		err = server.RemoveForward("tcp://:2322")
		if err != nil {
			t.Error(err)
			return
		}
		err = server.RemoveForward("web://loc1")
		if err != nil {
			t.Error(err)
			return
		}
		//test error
		err = server.RemoveForward("tcp://:283x")
		if err == nil {
			t.Error(err)
			return
		}
		err = server.RemoveForward("web://loctestxxx")
		if err == nil {
			t.Error(err)
			return
		}
		err = server.RemoveForward("://loctestxxx")
		if err == nil {
			t.Error(err)
			return
		}
	}
	{ //test forward name not found
		err = server.AddUriForward("tcp://:23221<xxxx>tcp://localhost:2833")
		if err != nil {
			t.Error(err)
			return
		}
		con, err := net.Dial("tcp", "localhost:23221")
		if err != nil {
			t.Error(err)
			return
		}
		buf := make([]byte, 100)
		_, err = con.Read(buf)
		if err == nil {
			t.Error(err)
			return
		}
		err = server.RemoveForward("tcp://:23221")
		if err != nil {
			t.Error(err)
			return
		}
	}
	{ //test login exist name
		fmt.Println("test runner3....")
		runner3 := NewChannelRunner("localhost:2832", "test1", "abc")
		runner3.AddDailer(NewWebDailer())
		runner3.AddDailer(NewTCPDailer())
		runner3.Start()
		time.Sleep(time.Second)
		runner3.Stop()
		fmt.Println("stop runner3....")
	}
	{ //test add dailer error
		runner4 := NewChannelRunner("localhost:2832", "test1", "abc")
		runner4.AddDailer(NewErrDailer())
	}
	{ //test close runner
		runner2.Stop()
		time.Sleep(time.Second)
		fmt.Println("stop runner2....")
	}
	{ //test login fail
		runner2 := NewChannelRunner("localhost:2832", "txss", "abcx")
		runner2.Start()
		time.Sleep(time.Second)
		runner2.Stop()
	}
	{ //test close server
		server.Close()
		runner.Stop()
		echo.L.Close()
	}
	time.Sleep(time.Second)
}

type ErrDailer struct {
}

func NewErrDailer() *ErrDailer {
	return &ErrDailer{}
}

func (e *ErrDailer) Bootstrap() error {
	return fmt.Errorf("test erro")
}

func (e *ErrDailer) Matched(uri string) bool {
	return strings.HasPrefix(uri, "tcp://cmd")
}

func (e *ErrDailer) Dail(cid uint32, uri string) (raw io.ReadWriteCloser, err error) {
	return
}

func TestError(t *testing.T) {
	netw.MOD_MAX_SIZE = 4
	netw.ShowLog = true
	server := NewChannelServer(":2833", "Server")
	server.ACL["^test.*$"] = "abc"
	server.Start()
	runner := NewChannelRunner("localhost:2833", "test0", "abc")
	runner.Start()
	time.Sleep(time.Second)
	// l := rc.NewRC_Listener_m_j(pool.BP, ":28324", netw.NewDoNotH())
	conn := netw.NewCon_(nil, pool.BP, netw.NewBufCon3(""))
	conn.B2V_ = impl.Json_B2V
	conn.V2B_ = impl.Json_V2B
	//
	emptyCmd := netw.NewCmd_(conn, []byte(util.S2Json(util.Map{})))
	code := server.OnRawCloseF(emptyCmd)
	if code == 0 {
		t.Errorf("code is %v", code)
		return
	}
	code = server.OnDailBackF(emptyCmd)
	if code == 0 {
		t.Errorf("code is %v", code)
		return
	}
	code = runner.OnRawCloseF(emptyCmd)
	if code == 0 {
		t.Errorf("code is %v", code)
		return
	}
	code = runner.OnDailF(emptyCmd)
	if code == 0 {
		t.Errorf("code is %v", code)
		return
	}
	code = server.OnLoginF(emptyCmd)
	if code == 0 {
		t.Errorf("code is %v", code)
		return
	}
	//
	notExist := netw.NewCmd_(conn, []byte(util.S2Json(util.Map{
		"cid":   10000,
		"uri":   "xxx",
		"error": "NONE",
	})))
	code = server.OnDailBackF(notExist)
	if code == 0 {
		t.Errorf("code is %v", code)
		return
	}
	//
	data1 := netw.NewCmd_(conn, []byte("abc"))
	code = server.OnDataF(data1)
	if code == 0 {
		t.Errorf("code is %v", code)
		return
	}
	code = runner.OnDataF(data1)
	if code == 0 {
		t.Errorf("code is %v", code)
		return
	}
	//
	data2 := netw.NewCmd_(conn, []byte("11abc"))
	code = server.OnDataF(data2)
	if code != 0 {
		t.Errorf("code is %v", code)
		return
	}
	code = runner.OnDataF(data2)
	if code != 0 {
		t.Errorf("code is %v", code)
		return
	}
	//
	fmt.Printf("xxx--->\n")
	data3 := netw.NewCmd_(conn, []byte{0, 0, 0, 0, 'a', 'b', 'c', 'd'})
	runner.rawCons[0] = &ErrReadWriteCloser{}
	code = runner.OnDataF(data3)
	if code != 0 {
		t.Errorf("code is %v", code)
		return
	}
	// l.Close()
	//
	netw.MOD_MAX_SIZE = 2
	server.readRawCon("xx", 100, netw.NewBufCon3("data"))
	runner.readRawCon(100, netw.NewBufCon3("data"))
	server.cons["abx"] = netw.NewCon_(nil, pool.BP, &ErrReadWriteCloser{})
	server.readRawCon("abx", 100, netw.NewBufCon3("data"))
	//
	forward, _ := NewForward("tcp://:232<aa>tcp://localhost:222")
	_, err := server.procDailTLS("tcp", "loc:232", forward)
	if err == nil {
		t.Error(err)
		return
	}
}

type ErrReadWriteCloser struct {
}

func (e *ErrReadWriteCloser) Write(p []byte) (n int, err error) {
	err = fmt.Errorf("error")
	return
}

func (e *ErrReadWriteCloser) Read(p []byte) (n int, err error) {
	return
}

func (e *ErrReadWriteCloser) Close() (err error) {
	return
}

func (e *ErrReadWriteCloser) LocalAddr() net.Addr {
	return nil
}

func (e *ErrReadWriteCloser) RemoteAddr() net.Addr {
	return nil
}
func (e *ErrReadWriteCloser) SetDeadline(t time.Time) error {
	return nil
}

func (e *ErrReadWriteCloser) SetReadDeadline(t time.Time) error {
	return nil
}
func (e *ErrReadWriteCloser) SetWriteDeadline(t time.Time) error {
	return nil
}
