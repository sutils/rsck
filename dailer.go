package rsck

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Centny/gwf/log"
	"golang.org/x/net/webdav"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

type CombinedReadWriterCloser struct {
	io.Reader
	io.Writer
	Closer func() error
	closed uint32
}

func (c *CombinedReadWriterCloser) Close() (err error) {
	if !atomic.CompareAndSwapUint32(&c.closed, 0, 1) {
		return fmt.Errorf("closed")
	}
	if c.Closer != nil {
		err = c.Closer()
	}
	return
}

type Dailer interface {
	Bootstrap() error
	Matched(uri string) bool
	Dail(cid uint32, uri string) (r io.ReadWriteCloser, err error)
}

type TCPDailer struct {
	portMatcher *regexp.Regexp
}

func NewTCPDailer() *TCPDailer {
	return &TCPDailer{
		portMatcher: regexp.MustCompile("^.*:[0-9]+$"),
	}
}

func (t *TCPDailer) Bootstrap() error {
	return nil
}

func (t *TCPDailer) Matched(uri string) bool {
	return true
}

func (t *TCPDailer) Dail(cid uint32, uri string) (raw io.ReadWriteCloser, err error) {
	remote, err := url.Parse(uri)
	if err == nil {
		network := remote.Scheme
		host := remote.Host
		switch network {
		case "http":
			network = "tcp"
			if !t.portMatcher.MatchString(uri) {
				host += ":80"
			}
		case "https":
			network = "tcp"
			if !t.portMatcher.MatchString(uri) {
				host += ":443"
			}
		}
		raw, err = net.Dial(network, host)
	}
	return
}

func (t *TCPDailer) String() string {
	return "TCPDailer"
}

var CMD_CTRL_C = []byte{255, 244, 255, 253, 6}

type CmdStdinWriter struct {
	io.Writer
	Replace  []byte
	CloseTag []byte
}

func (c *CmdStdinWriter) Write(p []byte) (n int, err error) {
	if len(c.CloseTag) > 0 {
		newp := bytes.Replace(p, c.CloseTag, []byte{}, -1)
		if len(newp) != len(p) {
			err = fmt.Errorf("closed")
			return 0, err
		}
	}
	if len(c.Replace) > 0 {
		p = bytes.Replace(p, c.Replace, []byte{}, -1)
	}
	n, err = c.Writer.Write(p)
	return
}

type CmdDailer struct {
	Replace  []byte
	CloseTag []byte
}

func NewCmdDailer() *CmdDailer {
	return &CmdDailer{
		Replace:  []byte("\r"),
		CloseTag: CMD_CTRL_C,
	}
}

func (c *CmdDailer) Bootstrap() error {
	return nil
}

func (c *CmdDailer) Matched(uri string) bool {
	return strings.HasPrefix(uri, "tcp://cmd")
}

func (c *CmdDailer) Dail(cid uint32, uri string) (raw io.ReadWriteCloser, err error) {
	remote, err := url.Parse(uri)
	if err != nil {
		return
	}
	runnable := remote.Query().Get("exec")
	log.D("CmdDailer dail to cmd:%v", runnable)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/C", runnable)
	default:
		cmd = exec.Command("bash", "-c", runnable)
	}
	retReader, stdWriter, err := os.Pipe()
	if err != nil {
		return
	}
	stdin, _ := cmd.StdinPipe()
	cmd.Stdout = stdWriter
	cmd.Stderr = stdWriter
	cmdWriter := &CmdStdinWriter{
		Writer:   stdin,
		Replace:  c.Replace,
		CloseTag: c.CloseTag,
	}
	combined := &CombinedReadWriterCloser{
		Writer: cmdWriter,
		Reader: retReader,
		Closer: func() error {
			log.D("CmdDailer will kill the cmd(%v)", cid)
			stdWriter.Close()
			stdin.Close()
			cmd.Process.Kill()
			return nil
		},
	}
	//
	switch remote.Query().Get("LC") {
	case "zh_CN.GBK":
		combined.Reader = transform.NewReader(combined.Reader, simplifiedchinese.GBK.NewDecoder())
		cmdWriter.Writer = transform.NewWriter(cmdWriter.Writer, simplifiedchinese.GBK.NewEncoder())
	case "zh_CN.GB18030":
		combined.Reader = transform.NewReader(combined.Reader, simplifiedchinese.GB18030.NewDecoder())
		cmdWriter.Writer = transform.NewWriter(cmdWriter.Writer, simplifiedchinese.GB18030.NewEncoder())
	default:
	}
	raw = combined
	err = cmd.Start()
	if err == nil {
		go func() {
			cmd.Wait()
			combined.Close()
		}()
	}
	return
}

func (t *CmdDailer) String() string {
	return "CmdDailer"
}

type WebDailer struct {
	accept  chan net.Conn
	consLck sync.RWMutex
	cons    map[string]*WebDailerConn
	davsLck sync.RWMutex
	davs    map[string]*WebdavHandler
}

func NewWebDailer() (dailer *WebDailer) {
	dailer = &WebDailer{
		accept:  make(chan net.Conn, 10),
		consLck: sync.RWMutex{},
		cons:    map[string]*WebDailerConn{},
		davsLck: sync.RWMutex{},
		davs:    map[string]*WebdavHandler{},
	}
	return
}

func (web *WebDailer) Bootstrap() error {
	go func() {
		http.Serve(web, web)
		close(web.accept)
	}()
	return nil
}

func (web *WebDailer) Shutdown() error {
	web.accept <- nil
	return nil
}

func (web *WebDailer) Matched(uri string) bool {
	return strings.HasPrefix(uri, "http://web")
}

func (web *WebDailer) Dail(cid uint32, uri string) (raw io.ReadWriteCloser, err error) {
	conn, raw, err := PipeWebDailerConn(cid, uri)
	if err != nil {
		return
	}
	web.consLck.Lock()
	web.cons[fmt.Sprintf("%v", cid)] = conn
	web.consLck.Unlock()
	web.accept <- conn
	return
}

//for net.Listener
func (web *WebDailer) Accept() (conn net.Conn, err error) {
	conn = <-web.accept
	if conn == nil {
		err = fmt.Errorf("WebDail is closed")
	}
	return
}

func (web *WebDailer) Close() error {
	return nil
}
func (web *WebDailer) Addr() net.Addr {
	return web
}

func (web *WebDailer) Network() string {
	return "tcp"
}

func (web *WebDailer) String() string {
	return "WebDailer(0:0)"
}

//for http.Handler
func (web *WebDailer) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	cid := req.RemoteAddr
	web.consLck.Lock()
	conn := web.cons[cid]
	web.consLck.Unlock()
	if conn == nil {
		resp.WriteHeader(404)
		fmt.Fprintf(resp, "WebDailConn is not exist by cid(%v)", cid)
		return
	}
	web.davsLck.Lock()
	dav := web.davs[conn.DIR]
	if dav == nil {
		dav = NewWebdavHandler(conn.DIR)
		web.davs[conn.DIR] = dav
	}
	web.davsLck.Unlock()
	dav.ServeHTTP(resp, req)
}

type WebDailerConn struct {
	*PipedConn
	CID uint32
	URI string
	DIR string
}

func PipeWebDailerConn(cid uint32, uri string) (conn *WebDailerConn, raw io.ReadWriteCloser, err error) {
	args, err := url.Parse(uri)
	if err != nil {
		return
	}
	dir := args.Query().Get("dir")
	if len(dir) < 1 {
		err = fmt.Errorf("the dir arguemnt is required")
		return
	}
	conn = &WebDailerConn{
		CID: cid,
		URI: uri,
		DIR: dir,
	}
	conn.PipedConn, raw, err = CreatePipedConn()
	return
}

func (w *WebDailerConn) LocalAddr() net.Addr {
	return w
}
func (w *WebDailerConn) RemoteAddr() net.Addr {
	return w
}
func (w *WebDailerConn) Network() string {
	return "WebDailer"
}
func (w *WebDailerConn) String() string {
	return fmt.Sprintf("%v", w.CID)
}

type WebdavHandler struct {
	dav webdav.Handler
	fs  http.Handler
}

func NewWebdavHandler(dir string) *WebdavHandler {
	return &WebdavHandler{
		dav: webdav.Handler{
			FileSystem: webdav.Dir(dir),
			LockSystem: webdav.NewMemLS(),
		},
		fs: http.FileServer(http.Dir(dir)),
	}
}

func (w *WebdavHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	log.D("WebdavHandler proc %v", req.RequestURI)
	if req.Method == "GET" {
		w.fs.ServeHTTP(resp, req)
	} else {
		w.dav.ServeHTTP(resp, req)
	}
}
