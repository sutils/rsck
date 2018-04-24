package rsck

import (
	"encoding/binary"
	"fmt"
	"net"
	"regexp"
	"sync"
	"sync/atomic"

	"github.com/Centny/gwf/log"
	"github.com/Centny/gwf/netw"
	"github.com/Centny/gwf/netw/impl"
	"github.com/Centny/gwf/pool"
	"github.com/Centny/gwf/util"
)

var ChannelByteTick = []byte{0}

var ChannelByteLogin = []byte{10}

var ChannelByteDail = []byte{20}

var ChannelByteData = []byte{30}

var ChannelByteClose = []byte{40}

type ChannelServer struct {
	L            *netw.Listener
	obdh         *impl.OBDH
	cons         map[string]netw.Con
	consLck      sync.RWMutex
	ACL          map[string]string
	aclLck       sync.RWMutex
	sequence     uint32
	rawCons      map[uint32]net.Conn
	rawConsLck   sync.RWMutex
	Listeners    map[string]net.Listener
	ListenersLck sync.RWMutex
}

func NewChannelServer(port string, n string) (server *ChannelServer) {
	server = &ChannelServer{
		obdh:         impl.NewOBDH(),
		cons:         map[string]netw.Con{},
		consLck:      sync.RWMutex{},
		ACL:          map[string]string{},
		aclLck:       sync.RWMutex{},
		rawCons:      map[uint32]net.Conn{},
		rawConsLck:   sync.RWMutex{},
		Listeners:    map[string]net.Listener{},
		ListenersLck: sync.RWMutex{},
	}
	server.L = netw.NewListenerN(pool.BP, port, n, netw.NewCCH(server, server.obdh), impl.Json_NewCon)
	// server.L.Runner_ = &netw.LenRunner{}
	server.obdh.AddF(ChannelByteLogin[0], server.OnLoginF)
	server.obdh.AddF(ChannelByteDail[0], server.OnDailBackF)
	server.obdh.AddF(ChannelByteData[0], server.OnDataF)
	server.obdh.AddF(ChannelByteClose[0], server.OnRawCloseF)
	server.obdh.AddH(ChannelByteTick[0], netw.NewDoNotH())
	return server
}

func (c *ChannelServer) Start() error {
	return c.L.Run()
}

func (c *ChannelServer) Dail(raw net.Conn, name, network, uri string) (err error) {
	c.consLck.RLock()
	defer c.consLck.RUnlock()
	con := c.cons[name]
	if con == nil {
		err = fmt.Errorf("channel not found by %v", name)
		return
	}
	cid := atomic.AddUint32(&c.sequence, 1)
	_, err = con.Writev2([]byte{ChannelByteDail[0]}, util.Map{
		"cid":     cid,
		"network": network,
		"uri":     uri,
	})
	if err == nil {
		c.rawConsLck.Lock()
		c.rawCons[cid] = raw
		c.rawConsLck.Unlock()
	}
	return
}

func (c *ChannelServer) AddForward(network, local, name, remote string, limit int) (err error) {
	listener, err := net.Listen(network, local)
	if err != nil {
		return
	}
	c.ListenersLck.Lock()
	c.Listeners[network+"/"+local] = listener
	c.ListenersLck.Unlock()
	go c.runForward(listener, name, network, remote, limit)
	return
}

func (c *ChannelServer) RemoveForward(network, local string) {
	c.ListenersLck.Lock()
	listener := c.Listeners[network+"/"+local]
	delete(c.Listeners, network+"/"+local)
	c.ListenersLck.Unlock()
	if listener != nil {
		listener.Close()
	}
}

func (c *ChannelServer) runForward(listener net.Listener, name, network, remote string, limit int) {
	for {
		raw, err := listener.Accept()
		if err != nil {
			log.E("ChannelServer forward listener(%v/%v/%v) accept fail with %v", listener.Addr(), name, remote, err)
			break
		}
		err = c.Dail(raw, name, network, remote)
		if err != nil {
			log.E("ChannelServer forward listener(%v/%v/%v) dail fail with %v", listener.Addr(), name, remote, err)
			raw.Close()
			continue
		}
		if limit > 0 {
			limit--
			if limit < 1 {
				listener.Close()
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
	var uri, network, errmsg string
	err := args.ValidF(`
		cid,R|I,R:0;
		uri,R|S,L:0;
		network,R|S,L:0;
		error,R|S,L:0;
		`, &cid, &uri, &network, &errmsg)
	if err != nil {
		log.E("ChannelServer do dail back fail with parse argument error:%v", err)
		con.Close()
		return -1
	}
	if errmsg != "NONE" {
		log.W("ChannelServer dail to %v/%v/%v fail with error:%v", con.Kvs().StrVal("name"), network, uri, errmsg)
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
	buf := make([]byte, 1024*1024)
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
	}
	c.consLck.Unlock()
}

// netw.NewListenerN(p *pool.BytePool, port string, n string, h netw.CCHandler, ncf netw.NewConF)

type ChannelRunner struct {
	R          *netw.NConRunner
	obdh       *impl.OBDH
	Name       string
	Token      string
	rawCons    map[uint32]net.Conn
	rawConsLck sync.RWMutex
}

func NewChannelRunner(addr, name, token string) (runner *ChannelRunner) {
	runner = &ChannelRunner{
		Name:       name,
		Token:      token,
		obdh:       impl.NewOBDH(),
		rawCons:    map[uint32]net.Conn{},
		rawConsLck: sync.RWMutex{},
	}
	runner.R = netw.NewNConRunnerN(pool.BP, addr, runner.obdh, impl.Json_NewCon)
	// runner.R.Runner_ = &netw.LenRunner{}
	runner.R.ConH = runner
	runner.R.TickData = append(ChannelByteTick, []byte("Tick\n")...)
	runner.obdh.AddF(ChannelByteDail[0], runner.OnDailF)
	runner.obdh.AddF(ChannelByteData[0], runner.OnDataF)
	runner.obdh.AddF(ChannelByteClose[0], runner.OnRawCloseF)
	return
}

func (c *ChannelRunner) Start() {
	c.R.StartRunner()
}

func (c *ChannelRunner) OnDailF(con netw.Cmd) int {
	var args = util.Map{}
	con.V(&args)
	var cid uint32
	var uri, network string = "", "tcp"
	err := args.ValidF(`
		cid,R|I,R:0;
		uri,R|S,L:0;
		network,O|S,L:0;
		`, &cid, &uri, &network)
	if err != nil {
		log.E("ChannelRunner on dail fail with %v", err)
		con.Writev(util.Map{
			"error": err.Error(),
		})
		return -1
	}
	rawCon, err := net.Dial(network, uri)
	if err != nil {
		log.E("ChannelRunner dail to %v fail with %v", uri, err)
		con.Writev(util.Map{
			"cid":     cid,
			"uri":     uri,
			"network": network,
			"error":   err.Error(),
		})
		return -1
	}
	c.rawConsLck.Lock()
	c.rawCons[cid] = rawCon
	go c.readRawCon(cid, rawCon)
	c.rawConsLck.Unlock()
	con.Writev(util.Map{
		"cid":     cid,
		"uri":     uri,
		"network": network,
		"error":   "NONE",
	})
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
	rawCon.Write(buf[4:])
	return 0
}

func (c *ChannelRunner) readRawCon(cid uint32, raw net.Conn) {
	buf := make([]byte, 1024*1024)
	binary.BigEndian.PutUint32(buf, cid)
	for {
		readed, err := raw.Read(buf[4:])
		if err != nil {
			log.D("ChannelRunner read %v raw conn fail with %v", cid, err)
			break
		}
		channel := c.R.C
		_, err = channel.Writeb(ChannelByteData, buf[:readed+4])
		if err != nil {
			log.D("ChannelRunner read %v raw and write to channel fail with %v", cid, err)
			break
		}
	}
	raw.Close()
	c.rawConsLck.Lock()
	delete(c.rawCons, cid)
	c.rawConsLck.Unlock()
	c.R.C.Writev2(ChannelByteClose, util.Map{
		"cid": cid,
	})
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
	return 0
}

func (c *ChannelRunner) OnConn(con netw.Con) bool {
	go func() {
		con.Writeb([]byte{ChannelByteLogin[0]}, []byte(util.S2Json(util.Map{
			"name":  c.Name,
			"token": c.Token,
		})))
	}()
	return true
}

func (c *ChannelRunner) OnClose(con netw.Con) {
}
