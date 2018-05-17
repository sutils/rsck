package rsck

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Centny/gwf/log"
	"github.com/Centny/gwf/netw"
	"github.com/Centny/gwf/netw/impl"
	"github.com/Centny/gwf/pool"
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

type Channel struct {
	Name   string
	Online bool
	FS     []*Forward
	Remote string
}

type fls []*Forward

func (f fls) Len() int {
	return len(f)
}

func (f fls) Less(i, j int) bool {
	if f[i].Name == f[j].Name {
		return f[i].Local.String() < f[j].Local.String()
	}
	return f[i].Name < f[j].Name
}

func (f fls) Swap(i, j int) {
	f[i], f[j] = f[j], f[i]
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
	Listeners    map[string]*ForwardListener
	ListenersLck sync.RWMutex
	HbDelay      int64
	running      bool
}

func NewChannelServer(port string, n string) (server *ChannelServer) {
	server = &ChannelServer{
		obdh:         impl.NewOBDH(),
		cons:         map[string]netw.Con{},
		consLck:      sync.RWMutex{},
		hbs:          map[string]int64{},
		hbsLck:       sync.RWMutex{},
		ACL:          map[string]string{},
		aclLck:       sync.RWMutex{},
		rawCons:      map[uint32]net.Conn{},
		rawConsLck:   sync.RWMutex{},
		Listeners:    map[string]*ForwardListener{},
		ListenersLck: sync.RWMutex{},
		HbDelay:      10000,
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
	log.D("ChannelServer start heartbeat by delay(%vms)", c.HbDelay)
	for c.running {
		c.consLck.Lock()
		c.hbsLck.Lock()
		now := util.Now()
		for name, con := range c.cons {
			if last, ok := c.hbs[name]; ok && now-last < c.HbDelay {
				continue
			}
			log.D("ChannelServer check %v heartbeat is timeout, will close it", name)
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
	c.ListenersLck.RLock()
	fs = map[string]*Channel{}
	nameForward := map[string][]*Forward{}
	for _, f := range c.Listeners {
		nameForward[f.Name] = append(nameForward[f.Name], f.Forward)
	}
	for name, nfs := range nameForward {
		sort.Sort(fls(nfs))
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
	c.ListenersLck.RUnlock()
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
	log.D("ChannelServer start dail to <%v>%v by cid(%v)", name, remote, cid)
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
	l, err := net.Listen(forward.Local.Scheme, forward.Local.Host)
	if err != nil {
		log.W("ChannelServer add forward by %v fail with %v", forward, err)
		return
	}
	c.ListenersLck.Lock()
	forwardListener := &ForwardListener{
		L:       l,
		Forward: forward,
	}
	c.Listeners[forward.Local.String()] = forwardListener
	c.ListenersLck.Unlock()
	go c.runForward(forwardListener)
	log.D("ChannelServer add forward by %v success", forward)
	return
}

func (c *ChannelServer) RemoveForward(local string) {
	log.D("ChannelServer removing forward by %v success", local)
	c.ListenersLck.Lock()
	listener := c.Listeners[local]
	delete(c.Listeners, local)
	c.ListenersLck.Unlock()
	if listener != nil {
		listener.L.Close()
	}
}

func (c *ChannelServer) runForward(forward *ForwardListener) {
	var limit int
	err := forward.LocalValidF(`limit,O|I,R:-1`, &limit)
	if err != nil {
		log.W("ChannelServer forward listener(%v) get the limit valid fail with %v", forward, err)
	}
	for {
		raw, err := forward.L.Accept()
		if err != nil {
			log.D("ChannelServer forward listener(%v) accept fail with %v", forward, err)
			break
		}
		log.D("ChannelServer forward listener(%v) accept from %v", forward, raw.RemoteAddr())
		err = c.Dail(raw, forward.Name, forward.Remote)
		if err != nil {
			log.W("ChannelServer forward listener(%v) dail fail with %v", forward, err)
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
		log.E("ChannelServer login fail with parse argument error:%v", err)
		con.Close()
		return -1
	}
	c.aclLck.RLock()
	defer c.aclLck.RUnlock()
	var access bool
	for n, k := range c.ACL {
		reg, err := regexp.Compile(n)
		if err != nil {
			log.E("ChannelServer regex compile acl name(%v) fail with %v", n, err)
			continue
		}
		if reg.MatchString(name) && k == token {
			access = true
			break
		}
	}
	if !access {
		log.E("ChannelServer login fail with not access by name(%v),token(%v)", name, token)
		con.Close()
		return -1
	}
	c.hbsLck.Lock()
	c.hbs[name] = util.Now()
	c.hbsLck.Unlock()
	c.consLck.Lock()
	if having, ok := c.cons[name]; ok {
		log.W("ChannelServer login with having name(%v) connection, will close it", name)
		having.Close()
	}
	con.SetWait(true)
	con.Kvs().SetVal("name", name)
	con.Kvs().SetVal("token", token)
	c.cons[name] = con.BaseCon()
	c.consLck.Unlock()
	log.D("ChannelServer accept channel(%v) from %v", name, con.RemoteAddr())
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
		log.E("ChannelServer do dail back fail with parse argument error:%v", err)
		con.Close()
		return -1
	}
	if errmsg != "NONE" {
		log.W("ChannelServer dail to %v/%v fail with error:%v", con.Kvs().StrVal("name"), uri, errmsg)
		c.rawConsLck.Lock()
		raw := c.rawCons[cid]
		if raw != nil {
			raw.Close()
		}
		delete(c.rawCons, cid)
		c.rawConsLck.Unlock()
		return 0
	}
	c.rawConsLck.Lock()
	raw := c.rawCons[cid]
	c.rawConsLck.Unlock()
	if raw == nil {
		log.W("ChannelServer do dail back fail with raw con by %v is not exist", cid)
		con.BaseCon().Writev2(ChannelByteClose, util.Map{
			"cid": cid,
		})
		return 0
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
			log.D("ChannelServer read %v raw conn fail with %v", cid, err)
			break
		}
		c.consLck.RLock()
		channel := c.cons[name]
		c.consLck.RUnlock()
		if channel == nil {
			log.D("ChannelServer read %v raw con will stop by channel(%v) not found", cid, name)
			break
		}
		log_d("ChannelServer read raw(%v):%v", cid, buf[4:readed+4])
		_, err = channel.Writeb(ChannelByteData, buf[:readed+4])
		if err != nil {
			log.D("ChannelServer %v raw write to channel fail with %v", cid, err)
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
		log.E("ChannelServer receive bad data by less 5")
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
		log.E("ChannelServer do raw close fail with parse argument error:%v", err)
		con.Close()
		return -1
	}
	log.D("ChannelServer receive close notify on raw(%v)", cid)
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
	log.D("ChannelRunner channel(%v) from %v is closed", name, con.RemoteAddr())
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
		log.E("ChannelRunner bootstrap dailer fail with %v", err)
		return err
	}
	c.Dailers = append(c.Dailers, dailer)
	return nil
}

func (c *ChannelRunner) Start() {
	c.R.StartRunner()
}

func (c *ChannelRunner) OnDailF(con netw.Cmd) int {
	var args = util.Map{}
	con.V(&args)
	var cid uint32
	var uri string = ""
	err := args.ValidF(`
		cid,R|I,R:0;
		uri,R|S,L:0;
		`, &cid, &uri)
	if err != nil {
		log.E("ChannelRunner on dail fail with %v", err)
		con.Writev(util.Map{
			"error": err.Error(),
		})
		return -1
	}
	var rawCon io.ReadWriteCloser
	err = fmt.Errorf("not matched dailer for %v", uri)
	for _, dailer := range c.Dailers {
		if dailer.Matched(uri) {
			log.D("ChannelRunner will use %v to dail by %v", dailer, uri)
			rawCon, err = dailer.Dail(cid, uri)
			break
		}
	}
	if err != nil {
		log.E("ChannelRunner dail to %v fail with %v", uri, err)
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
	log.D("ChannelRunner dail to %v success", uri)
	return 0
}

func (c *ChannelRunner) OnDataF(con netw.Cmd) int {
	buf := con.Data()
	if len(buf) < 5 {
		log.E("ChannelRunner receive bad data by less 5")
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
		log.D("ChannelRunner send data to raw(%v) fail with %v", cid, err)
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
			log.D("ChannelRunner read %v raw conn fail with %v", cid, err)
			break
		}
		log_d("ChannelRunner read raw(%v):%v", cid, buf[4:readed+4])
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
	log.D("ChannelRunner %v raw conn reader is done", cid)
}

func (c *ChannelRunner) OnRawCloseF(con netw.Cmd) int {
	var args = util.Map{}
	con.V(&args)
	var cid uint32
	err := args.ValidF(`
		cid,R|I,R:0;
		`, &cid)
	if err != nil {
		log.E("ChannelRunner do raw close fail with parse argument error:%v", err)
		con.Close()
		return -1
	}
	c.rawConsLck.Lock()
	rawCon := c.rawCons[cid]
	c.rawConsLck.Unlock()
	if rawCon != nil {
		rawCon.Close()
	}
	log.D("ChannelRunner receive notify raw(%v) is closed", cid)
	return 0
}

func (c *ChannelRunner) OnConn(con netw.Con) bool {
	go func() {
		log.D("ChannelRunner do login by name:%v,token:%v", c.Name, c.Token)
		con.Writeb([]byte{ChannelByteLogin[0]}, []byte(util.S2Json(util.Map{
			"name":  c.Name,
			"token": c.Token,
		})))
	}()
	return true
}

func (c *ChannelRunner) OnClose(con netw.Con) {
	log.D("ChannelRunner channel is closed")
}
