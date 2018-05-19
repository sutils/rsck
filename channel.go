package rsck

import (
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Centny/gwf/log"
	"github.com/Centny/gwf/netw"
	"github.com/Centny/gwf/netw/impl"
	"github.com/Centny/gwf/pool"
	"github.com/Centny/gwf/routing"
	"github.com/Centny/gwf/util"
)

var ChannelByteHeartbeat = []byte{0}

var ChannelByteLogin = []byte{10}

var ChannelByteDail = []byte{20}

var ChannelByteData = []byte{30}

var ChannelByteClose = []byte{40}

type Forward struct {
	Name   string
	Local  *url.URL
	Remote *url.URL
}

func NewForward(uri string) (forward *Forward, err error) {
	parts := regexp.MustCompile("[<>]").Split(uri, 3)
	if len(parts) != 3 {
		err = fmt.Errorf("invalid uri:%v", uri)
		return
	}
	forward = &Forward{}
	forward.Name = parts[1]
	forward.Local, err = url.Parse(parts[0])
	if err == nil {
		forward.Remote, err = url.Parse(parts[2])
	}
	return
}

func (f *Forward) LocalValidF(format string, args ...interface{}) error {
	return util.ValidAttrF(format, f.Local.Query().Get, true, args...)
}

func (f *Forward) RemoteValidF(format string, args ...interface{}) error {
	return util.ValidAttrF(format, f.Remote.Query().Get, true, args...)
}

func (f *Forward) String() string {
	return fmt.Sprintf("%v<%v>%v", f.Local, f.Name, f.Remote)
}

type ForwardListener struct {
	*Forward
	L net.Listener
}

func (f *ForwardListener) String() string {
	return fmt.Sprintf("%v", f.Forward)
}

type ForwardSorter []*Forward

func (f ForwardSorter) Len() int {
	return len(f)
}

func (f ForwardSorter) Less(i, j int) bool {
	if f[i].Name == f[j].Name {
		return f[i].Local.String() < f[j].Local.String()
	}
	return f[i].Name < f[j].Name
}

func (f ForwardSorter) Swap(i, j int) {
	f[i], f[j] = f[j], f[i]
}

type Channel struct {
	Name   string
	Online bool
	FS     []*Forward
	Remote string
}

type ChannelServer struct {
	L            *netw.Listener
	obdh         *impl.OBDH
	cons         map[string]netw.Con
	consLck      sync.RWMutex
	hbs          map[string]int64
	hbsLck       sync.RWMutex
	ACL          map[string]string
	aclLck       sync.RWMutex
	sequence     uint32
	rawCons      map[uint32]net.Conn
	rawConsLck   sync.RWMutex
	listeners    map[string]*ForwardListener
	listenersLck sync.RWMutex
	HbDelay      int64
	running      bool
	//
	webForward    map[string]*Forward
	webForwardLck sync.RWMutex
	WebSuffix     string
	WebAuth       string
}

func NewChannelServer(port string, n string) (server *ChannelServer) {
	server = &ChannelServer{
		obdh:          impl.NewOBDH(),
		cons:          map[string]netw.Con{},
		consLck:       sync.RWMutex{},
		hbs:           map[string]int64{},
		hbsLck:        sync.RWMutex{},
		ACL:           map[string]string{},
		aclLck:        sync.RWMutex{},
		rawCons:       map[uint32]net.Conn{},
		rawConsLck:    sync.RWMutex{},
		listeners:     map[string]*ForwardListener{},
		listenersLck:  sync.RWMutex{},
		HbDelay:       10000,
		webForward:    map[string]*Forward{},
		webForwardLck: sync.RWMutex{},
	}
	server.L = netw.NewListenerN(pool.BP, port, n, netw.NewCCH(server, server.obdh), impl.Json_NewCon)
	// server.L.Runner_ = &netw.LenRunner{}
	server.obdh.AddF(ChannelByteLogin[0], server.OnLoginF)
	server.obdh.AddF(ChannelByteDail[0], server.OnDailBackF)
	server.obdh.AddF(ChannelByteData[0], server.OnDataF)
	server.obdh.AddF(ChannelByteClose[0], server.OnRawCloseF)
	server.obdh.AddF(ChannelByteHeartbeat[0], server.OnHeartbeatF)
	return server
}

func (c *ChannelServer) Start() error {
	err := c.L.Run()
	if err == nil {
		c.running = true
		go c.loopHb()
	}
	return err
}

func (c *ChannelServer) Close() {
	c.L.Close()
}

func (c *ChannelServer) loopHb() {
	log.D("ChannelServer(%v) start heartbeat by delay(%vms)", c, c.HbDelay)
	for c.running {
		c.consLck.Lock()
		c.hbsLck.Lock()
		now := util.Now()
		for name, con := range c.cons {
			if last, ok := c.hbs[name]; ok && now-last < c.HbDelay {
				continue
			}
			log.D("ChannelServer(%v) check %v heartbeat is timeout, will close it", c, name)
			con.Close()
		}
		c.hbsLck.Unlock()
		c.consLck.Unlock()
		time.Sleep(time.Duration(c.HbDelay) * time.Millisecond)
	}
}

func (c *ChannelServer) OnHeartbeatF(con netw.Cmd) int {
	name := con.Kvs().StrVal("name")
	c.hbsLck.Lock()
	c.hbs[name] = util.Now()
	c.hbsLck.Unlock()
	return 0
}

func (c *ChannelServer) AllForwards() (ns []string, fs map[string]*Channel) {
	c.consLck.RLock()
	c.listenersLck.RLock()
	c.webForwardLck.RLock()
	fs = map[string]*Channel{}
	nameForward := map[string][]*Forward{}
	for _, f := range c.listeners {
		nameForward[f.Name] = append(nameForward[f.Name], f.Forward)
	}
	for _, f := range c.webForward {
		nameForward[f.Name] = append(nameForward[f.Name], f)
	}
	for name, nfs := range nameForward {
		sort.Sort(ForwardSorter(nfs))
		con, online := c.cons[name]
		fs[name] = &Channel{
			FS:     nfs,
			Name:   name,
			Online: online,
		}
		if online {
			fs[name].Remote = con.RemoteAddr().String()
		}
		ns = append(ns, name)
	}
	for name, con := range c.cons {
		if _, ok := fs[name]; ok {
			continue
		}
		fs[name] = &Channel{
			Name:   name,
			Online: true,
			Remote: con.RemoteAddr().String(),
		}
		ns = append(ns, name)
	}
	c.webForwardLck.RUnlock()
	c.listenersLck.RUnlock()
	c.consLck.RUnlock()
	sort.Sort(util.NewStringSorter(ns))
	return
}

func (c *ChannelServer) Dail(raw net.Conn, name string, remote *url.URL) (err error) {
	c.consLck.RLock()
	defer c.consLck.RUnlock()
	con := c.cons[name]
	if con == nil {
		err = fmt.Errorf("channel not found by %v", name)
		return
	}
	cid := atomic.AddUint32(&c.sequence, 1)
	log.D("ChannelServer(%v) start dail to <%v>%v by cid(%v)", c, name, remote, cid)
	_, err = con.Writev2([]byte{ChannelByteDail[0]}, util.Map{
		"cid": cid,
		"uri": remote.String(),
	})
	if err == nil {
		c.rawConsLck.Lock()
		c.rawCons[cid] = raw
		c.rawConsLck.Unlock()
	}
	return
}

func (c *ChannelServer) AddUriForward(uri string) (err error) {
	forward, err := NewForward(uri)
	if err == nil {
		err = c.AddForward(forward)
	}
	return
}

func (c *ChannelServer) AddForward(forward *Forward) (err error) {
	switch forward.Local.Scheme {
	case "tcp":
		var l net.Listener
		l, err = net.Listen(forward.Local.Scheme, forward.Local.Host)
		if err != nil {
			log.W("ChannelServer(%v) add tcp forward by %v fail with %v", c, forward, err)
			return
		}
		c.listenersLck.Lock()
		forwardListener := &ForwardListener{
			L:       l,
			Forward: forward,
		}
		c.listeners[forward.Local.Host] = forwardListener
		c.listenersLck.Unlock()
		go c.runForward(forwardListener)
		log.D("ChannelServer(%v) add tcp forward by %v success", c, forward)
	case "web":
		c.webForwardLck.Lock()
		if _, ok := c.webForward[forward.Local.Host]; ok {
			err = fmt.Errorf("web host key(%v) is exists", forward.Local.Host)
			log.W("ChannelServer(%v) add web forward by %v fail with key exists", c, forward)
		} else {
			c.webForward[forward.Local.Host] = forward
			log.D("ChannelServer(%v) add web forward by %v success", c, forward)
		}
		c.webForwardLck.Unlock()
	default:
		err = fmt.Errorf("scheme %v is not suppored", forward.Local.Scheme)
	}
	return
}

func (c *ChannelServer) RemoveForward(local string) (err error) {
	rurl, err := url.Parse(local)
	if err != nil {
		return
	}
	if rurl.Scheme == "web" {
		c.webForwardLck.Lock()
		forward := c.webForward[rurl.Host]
		delete(c.webForward, rurl.Host)
		c.webForwardLck.Unlock()
		if forward != nil {
			log.D("ChannelServer(%v) removing web forward by %v success", c, local)
		} else {
			err = fmt.Errorf("web forward is not exist by %v", local)
			log.D("ChannelServer(%v) removing web forward by %v fail with not exists", c, local)
		}
	} else {
		c.listenersLck.Lock()
		listener := c.listeners[rurl.Host]
		delete(c.listeners, rurl.Host)
		c.listenersLck.Unlock()
		if listener != nil {
			listener.L.Close()
			log.D("ChannelServer(%v) removing forward by %v success", c, local)
		} else {
			err = fmt.Errorf("forward is not exitst")
			log.D("ChannelServer(%v) removing forward by %v fail with not exists", c, local)
		}
	}
	return
}

func (c *ChannelServer) runForward(forward *ForwardListener) {
	var limit int
	err := forward.LocalValidF(`limit,O|I,R:-1`, &limit)
	if err != nil {
		log.W("ChannelServer(%v) forward listener(%v) get the limit valid fail with %v", c, forward, err)
	}
	for {
		raw, err := forward.L.Accept()
		if err != nil {
			log.D("ChannelServer(%v) forward listener(%v) accept fail with %v", c, forward, err)
			break
		}
		log.D("ChannelServer(%v) forward listener(%v) accept from %v", c, forward, raw.RemoteAddr())
		err = c.Dail(raw, forward.Name, forward.Remote)
		if err != nil {
			log.W("ChannelServer(%v) forward listener(%v) dail fail with %v", c, forward, err)
			raw.Close()
			continue
		}

		if limit > 0 {
			limit--
			if limit < 1 {
				forward.L.Close()
			}
		}
	}
	c.listenersLck.Lock()
	delete(c.listeners, forward.Local.Host)
	c.listenersLck.Unlock()
}

// func (c *ChannelServer) ListWebForward(hs *routing.HTTPSession) routing.HResult {
// 	var fs = fls{}
// 	c.webForwardLck.Lock()
// 	for _, forward := range c.webForward {
// 		fs = append(fs, forward)
// 	}
// 	c.webForwardLck.Unlock()
// 	tmpl, _ := template.New("list").Parse("<pre>\n{{range $v:=.fs}}<a href=\"{{$v.Local.Host}}{{.suffix}}\">{{$v}}</a>\n{{end}}</pre>")
// 	sort.Sort(fs)
// 	hs.W.Header().Set("Content-Type", "text/html;charset=utf-8")
// 	tmpl.Execute(hs.W, map[string]interface{}{
// 		"fs":     fs,
// 		"suffix": c.WebSuffix,
// 	})
// 	return routing.HRES_RETURN
// }

func (c *ChannelServer) ProcWebForward(hs *routing.HTTPSession) routing.HResult {
	name := strings.Trim(strings.TrimSuffix(hs.R.Host, c.WebSuffix), ". ")
	c.webForwardLck.Lock()
	forward := c.webForward[name]
	c.webForwardLck.Unlock()
	if forward == nil {
		hs.W.WriteHeader(404)
		return hs.Printf("alias not exist by name:%v", name)
	}
	hs.R.URL.Scheme = forward.Remote.Scheme
	hs.R.URL.Host = forward.Remote.Host
	if len(c.WebAuth) > 0 && forward.Local.Query().Get("auth") != "0" {
		username, password, ok := hs.R.BasicAuth()
		if !(ok && c.WebAuth == fmt.Sprintf("%v:%s", username, password)) {
			hs.W.Header().Set("WWW-Authenticate", "Basic realm=Reverse Server")
			hs.W.WriteHeader(401)
			hs.Printf("%v", "401 Unauthorized")
			return routing.HRES_RETURN
		}
	}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.Host = req.URL.Host
		},
		Transport: &http.Transport{
			Dial: func(network, addr string) (raw net.Conn, err error) {
				return c.procDail(network, addr, forward)
			},
			DialTLS: func(network, addr string) (raw net.Conn, err error) {
				return c.procDailTLS(network, addr, forward)
			},
		},
	}
	proxy.ServeHTTP(hs.W, hs.R)
	return routing.HRES_RETURN
}
func (c *ChannelServer) procDail(network, addr string, forward *Forward) (raw net.Conn, err error) {
	piped, raw, err := CreatePipedConn()
	if err == nil {
		err = c.Dail(piped, forward.Name, forward.Remote)
		if err != nil {
			piped.Close()
		}
	}
	return
}
func (c *ChannelServer) procDailTLS(network, addr string, forward *Forward) (raw net.Conn, err error) {
	pipedA, pipedB, err := CreatePipedConn()
	if err == nil {
		tlsConn := tls.Client(pipedB, &tls.Config{
			InsecureSkipVerify: true,
		})
		err = c.Dail(pipedA, forward.Name, forward.Remote)
		if err == nil {
			err = tlsConn.Handshake()
		}
		if err == nil {
			raw = tlsConn
		} else {
			pipedB.Close()
			tlsConn.Close()
		}
	}
	return
}
func (c *ChannelServer) OnLoginF(con netw.Cmd) int {
	var args = util.Map{}
	con.V(&args)
	var name, token string
	err := args.ValidF(`
		name,R|S,L:0;
		token,R|S,L:0;
		`, &name, &token)
	if err != nil {
		log.E("ChannelServer(%v) login fail with parse argument error:%v", c, err)
		con.Close()
		return -1
	}
	c.aclLck.RLock()
	defer c.aclLck.RUnlock()
	var access bool
	for n, k := range c.ACL {
		reg, err := regexp.Compile(n)
		if err != nil {
			log.E("ChannelServer(%v) regex compile acl name(%v) fail with %v", c, n, err)
			continue
		}
		if reg.MatchString(name) && k == token {
			access = true
			break
		}
	}
	if !access {
		log.E("ChannelServer(%v) login fail with not access by name(%v),token(%v)", c, name, token)
		con.Close()
		return -1
	}
	c.hbsLck.Lock()
	c.hbs[name] = util.Now()
	c.hbsLck.Unlock()
	c.consLck.Lock()
	if having, ok := c.cons[name]; ok {
		log.W("ChannelServer(%v) login with having name(%v) connection, will close it", c, name)
		having.Close()
	}
	con.SetWait(true)
	con.Kvs().SetVal("name", name)
	con.Kvs().SetVal("token", token)
	c.cons[name] = con.BaseCon()
	c.consLck.Unlock()
	log.D("ChannelServer(%v) accept channel(%v) from %v", c, name, con.RemoteAddr())
	return 0
}

func (c *ChannelServer) OnDailBackF(con netw.Cmd) int {
	var args = util.Map{}
	con.V(&args)
	var cid uint32
	var uri, errmsg string
	err := args.ValidF(`
		cid,R|I,R:0;
		uri,R|S,L:0;
		error,R|S,L:0;
		`, &cid, &uri, &errmsg)
	if err != nil {
		log.E("ChannelServer(%v) do dail back fail with parse argument error:%v", c, err)
		con.Close()
		return -1
	}
	if errmsg != "NONE" {
		log.W("ChannelServer(%v) dail to %v/%v fail with error:%v", c, con.Kvs().StrVal("name"), uri, errmsg)
		c.rawConsLck.Lock()
		raw := c.rawCons[cid]
		if raw != nil {
			raw.Close()
		}
		delete(c.rawCons, cid)
		c.rawConsLck.Unlock()
		return -1
	}
	c.rawConsLck.Lock()
	raw := c.rawCons[cid]
	c.rawConsLck.Unlock()
	if raw == nil {
		log.W("ChannelServer(%v) do dail back fail with raw con by %v is not exist", c, cid)
		con.BaseCon().Writev2(ChannelByteClose, util.Map{
			"cid": cid,
		})
		return -1
	}
	go c.readRawCon(con.Kvs().StrVal("name"), cid, raw)
	return 0
}

func (c *ChannelServer) readRawCon(name string, cid uint32, raw net.Conn) {
	var buf []byte
	if netw.MOD_MAX_SIZE == 4 {
		buf = make([]byte, 1024*1024*2)
	} else {
		buf = make([]byte, 50000)
	}
	binary.BigEndian.PutUint32(buf, cid)
	for {
		readed, err := raw.Read(buf[4:])
		if err != nil {
			log.D("ChannelServer(%v) read %v raw conn fail with %v", c, cid, err)
			break
		}
		c.consLck.RLock()
		channel := c.cons[name]
		c.consLck.RUnlock()
		if channel == nil {
			log.D("ChannelServer(%v) read %v raw con will stop by channel(%v) not found", c, cid, name)
			break
		}
		log_d("ChannelServer(%v) read raw(%v):%v", c, cid, readed)
		_, err = channel.Writeb(ChannelByteData, buf[:readed+4])
		if err != nil {
			log.D("ChannelServer(%v) %v raw write to channel fail with %v", c, cid, err)
			break
		}
	}
	raw.Close()
	c.rawConsLck.Lock()
	delete(c.rawCons, cid)
	c.rawConsLck.Unlock()
	c.consLck.RLock()
	channel := c.cons[name]
	c.consLck.RUnlock()
	if channel != nil { //notify tunnel closed
		channel.Writev2(ChannelByteClose, util.Map{
			"cid": cid,
		})
	}
}

func (c *ChannelServer) OnDataF(con netw.Cmd) int {
	buf := con.Data()
	if len(buf) < 5 {
		log.E("ChannelServer(%v) receive bad data by less 5", c)
		return -1
	}
	cid := binary.BigEndian.Uint32(buf)
	c.rawConsLck.Lock()
	rawCon := c.rawCons[cid]
	c.rawConsLck.Unlock()
	if rawCon == nil { //notify tunnel is closed
		con.BaseCon().Writev2(ChannelByteClose, util.Map{
			"cid": cid,
		})
		return 0
	}
	rawCon.Write(buf[4:])
	return 0
}

func (c *ChannelServer) OnRawCloseF(con netw.Cmd) int {
	var args = util.Map{}
	con.V(&args)
	var cid uint32
	err := args.ValidF(`
		cid,R|I,R:0;
		`, &cid)
	if err != nil {
		log.E("ChannelServer(%v) do raw close fail with parse argument error:%v", c, err)
		con.Close()
		return -1
	}
	log.D("ChannelServer(%v) receive close notify on raw(%v)", c, cid)
	c.rawConsLck.Lock()
	rawCon := c.rawCons[cid]
	c.rawConsLck.Unlock()
	if rawCon != nil {
		rawCon.Close()
	}
	return 0
}

func (c *ChannelServer) OnConn(con netw.Con) bool {
	return true
}

func (c *ChannelServer) OnClose(con netw.Con) {
	name := con.Kvs().StrVal("name")
	c.consLck.Lock()
	if c.cons[name] == con {
		delete(c.cons, name)
		c.consLck.Unlock()
		c.hbsLck.Lock()
		delete(c.hbs, name)
		c.hbsLck.Unlock()
	} else {
		c.consLck.Unlock()
	}
	log.D("ChannelServer(%v) channel(%v) from %v is closed", c, name, con.RemoteAddr())
}

func (c *ChannelServer) String() string {
	return c.L.Id()
}

type ChannelRunner struct {
	R          *netw.NConRunner
	obdh       *impl.OBDH
	Name       string
	Token      string
	rawCons    map[uint32]io.ReadWriteCloser
	rawConsLck sync.RWMutex
	Dailers    []Dailer
}

func NewChannelRunner(addr, name, token string) (runner *ChannelRunner) {
	runner = &ChannelRunner{
		Name:       name,
		Token:      token,
		obdh:       impl.NewOBDH(),
		rawCons:    map[uint32]io.ReadWriteCloser{},
		rawConsLck: sync.RWMutex{},
	}
	runner.R = netw.NewNConRunnerN(pool.BP, addr, runner.obdh, impl.Json_NewCon)
	// runner.R.Runner_ = &netw.LenRunner{}
	runner.R.ConH = runner
	runner.R.TickData = append(ChannelByteHeartbeat, []byte("ClientTick\n")...)
	runner.R.Tick = 3000
	runner.obdh.AddF(ChannelByteDail[0], runner.OnDailF)
	runner.obdh.AddF(ChannelByteData[0], runner.OnDataF)
	runner.obdh.AddF(ChannelByteClose[0], runner.OnRawCloseF)
	runner.obdh.AddH(ChannelByteHeartbeat[0], netw.NewDoNotH())
	return
}

func (c *ChannelRunner) AddDailer(dailer Dailer) error {
	err := dailer.Bootstrap()
	if err != nil {
		log.E("ChannelRunner(%v) bootstrap dailer fail with %v", c, err)
		return err
	}
	c.Dailers = append(c.Dailers, dailer)
	return nil
}

func (c *ChannelRunner) Start() {
	c.R.StartRunner()
}

func (c *ChannelRunner) Stop() {
	c.R.StopRunner()
}

func (c *ChannelRunner) OnDailF(con netw.Cmd) int {
	var args = util.Map{}
	con.V(&args)
	var cid uint32
	var uri string
	err := args.ValidF(`
		cid,R|I,R:0;
		uri,R|S,L:0;
		`, &cid, &uri)
	if err != nil {
		log.E("ChannelRunner(%v) on dail fail with %v", err, c)
		con.Writev(util.Map{
			"error": err.Error(),
		})
		return -1
	}
	var rawCon io.ReadWriteCloser
	err = fmt.Errorf("not matched dailer for %v", uri)
	for _, dailer := range c.Dailers {
		if dailer.Matched(uri) {
			log.D("ChannelRunner(%v) will use %v to dail by %v", dailer, uri, c)
			rawCon, err = dailer.Dail(cid, uri)
			break
		}
	}
	if err != nil {
		log.E("ChannelRunner(%v) dail to %v fail with %v", uri, err, c)
		con.Writev(util.Map{
			"cid":   cid,
			"uri":   uri,
			"error": err.Error(),
		})
		return -1
	}
	c.rawConsLck.Lock()
	c.rawCons[cid] = rawCon
	go c.readRawCon(cid, rawCon)
	c.rawConsLck.Unlock()
	con.Writev(util.Map{
		"cid":   cid,
		"uri":   uri,
		"error": "NONE",
	})
	log.D("ChannelRunner(%v) dail to %v success", uri, c)
	return 0
}

func (c *ChannelRunner) OnDataF(con netw.Cmd) int {
	buf := con.Data()
	if len(buf) < 5 {
		log.E("ChannelRunner(%v) receive bad data by less 5", c)
		return -1
	}
	cid := binary.BigEndian.Uint32(buf)
	c.rawConsLck.Lock()
	rawCon := c.rawCons[cid]
	c.rawConsLck.Unlock()
	if rawCon == nil {
		con.BaseCon().Writev2(ChannelByteClose, util.Map{
			"cid": cid,
		})
		return 0
	}
	_, err := rawCon.Write(buf[4:])
	if err != nil {
		log.D("ChannelRunner(%v) send data to raw(%v) fail with %v", c, cid, err)
		rawCon.Close()
	}
	return 0
}

func (c *ChannelRunner) readRawCon(cid uint32, raw io.ReadWriteCloser) {
	var buf []byte
	if netw.MOD_MAX_SIZE == 4 {
		buf = make([]byte, 1024*1024*2)
	} else {
		buf = make([]byte, 50000)
	}
	binary.BigEndian.PutUint32(buf, cid)
	for {
		readed, err := raw.Read(buf[4:])
		if err != nil {
			log.D("ChannelRunner(%v) read %v raw conn fail with %v", c, cid, err)
			break
		}
		log_d("ChannelRunner(%v) read raw(%v):%v", c, cid, readed)
		c.R.Writeb(ChannelByteData, buf[:readed+4])
		// if err != nil {
		// 	log.D("ChannelRunner read %v raw and write to channel fail with %v", cid, err)
		// 	break
		// }
	}
	raw.Close()
	c.rawConsLck.Lock()
	delete(c.rawCons, cid)
	c.rawConsLck.Unlock()
	c.R.Writev2(ChannelByteClose, util.Map{
		"cid": cid,
	})
	log.D("ChannelRunner(%v) %v raw conn reader is done", c, cid)
}

func (c *ChannelRunner) OnRawCloseF(con netw.Cmd) int {
	var args = util.Map{}
	con.V(&args)
	var cid uint32
	err := args.ValidF(`
		cid,R|I,R:0;
		`, &cid)
	if err != nil {
		log.E("ChannelRunner(%v) do raw close fail with parse argument error:%v", c, err)
		con.Close()
		return -1
	}
	c.rawConsLck.Lock()
	rawCon := c.rawCons[cid]
	c.rawConsLck.Unlock()
	if rawCon != nil {
		rawCon.Close()
	}
	log.D("ChannelRunner(%v) receive notify raw(%v) is closed", c, cid)
	return 0
}

func (c *ChannelRunner) OnConn(con netw.Con) bool {
	go func() {
		log.D("ChannelRunner(%v) do login by name:%v,token:%v", c, c.Name, c.Token)
		con.Writeb([]byte{ChannelByteLogin[0]}, []byte(util.S2Json(util.Map{
			"name":  c.Name,
			"token": c.Token,
		})))
	}()
	return true
}

func (c *ChannelRunner) OnClose(con netw.Con) {
	log.D("ChannelRunner(%v) channel is closed", c)
}

func (c *ChannelRunner) String() string {
	return c.R.Id()
}
