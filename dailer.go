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
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Centny/gwf/log"
	"golang.org/x/net/webdav"
)

type CombinedReadWriterCloser struct {
	io.Reader
	io.Writer
	Closer func() error
}

func (c *CombinedReadWriterCloser) Close() (err error) {
	if r, ok := c.Reader.(io.Closer); ok {
		err = r.Close()
	}
	if r, ok := c.Writer.(io.Closer); ok {
		err = r.Close()
	}
	if c.Closer != nil {
		err = c.Closer()
	}
	return
}

type Dailer interface {
	Matched(uri string) bool
	Dail(cid uint32, uri string) (r io.ReadWriteCloser, err error)
}

type TCPDailer struct {
}

func NewTCPDailer() *TCPDailer {
	return &TCPDailer{}
}

func (t *TCPDailer) Matched(uri string) bool {
	return strings.HasPrefix(uri, "tcp://")
}

func (t *TCPDailer) Dail(cid uint32, uri string) (raw io.ReadWriteCloser, err error) {
	remote, err := url.Parse(uri)
	if err == nil {
		raw, err = net.Dial(remote.Scheme, remote.Host)

	}
	return
}

func (t *TCPDailer) String() string {
	return "TCPDailer"
}

type CmdStdinWriter struct {
	Closer func() error
	io.Writer
	Replace  []byte
	CloseTag []byte
}

func (c *CmdStdinWriter) Write(p []byte) (n int, err error) {
	if c.Closer != nil && len(c.CloseTag) > 0 {
		newp := bytes.Replace(p, c.CloseTag, []byte{}, -1)
		if len(newp) != len(p) {
			c.Closer()
			err = fmt.Errorf("closed")
			return 0, err
		}
	}
	if len(c.Replace) > 0 {
		n = len(p)
		_, err = c.Writer.Write(bytes.Replace(p, c.Replace, []byte{}, -1))
		return
	}
	n, err = c.Writer.Write(p)
	return
}

type CmdDailer struct {
}

func NewCmdDailer() *CmdDailer {
	return &CmdDailer{}
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
	closer := func() error {
		log.D("CmdDailer will kill the cmd(%v)", cid)
		retReader.Close()
		stdin.Close()
		return cmd.Process.Kill()
	}
	raw = &CombinedReadWriterCloser{
		Closer: closer,
		Reader: retReader,
		Writer: &CmdStdinWriter{
			Closer:   closer,
			Writer:   stdin,
			Replace:  []byte("\r"),
			CloseTag: []byte{255, 244, 255, 253, 6},
		},
	}
	err = cmd.Start()
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

func (web *WebDailer) Start() error {
	go http.Serve(web, web)
	return nil
}

func (web *WebDailer) Matched(uri string) bool {
	return strings.HasPrefix(uri, "tcp://web")
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
	close(web.accept)
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
	io.ReadCloser
	io.Writer
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
	inReader, outWriter, err := os.Pipe()
	if err != nil {
		return
	}
	outReader, inWriter, err := os.Pipe()
	if err != nil {
		return
	}
	conn = &WebDailerConn{
		ReadCloser: inReader,
		Writer:     inWriter,
		CID:        cid,
		URI:        uri,
		DIR:        dir,
	}
	raw = &CombinedReadWriterCloser{
		Reader: outReader,
		Writer: outWriter,
	}
	return
}

// func (w *WebDailerConn) Write(p []byte) (n int, err error) {
// 	n, err = w.Writer.Write(p)
// 	return
// }

// func (w *WebDailerConn) Read(p []byte) (n int, err error) {
// 	n, err = w.ReadCloser.Read(p)
// 	return
// }

func (w *WebDailerConn) LocalAddr() net.Addr {
	return w
}
func (w *WebDailerConn) RemoteAddr() net.Addr {
	return w
}
func (w *WebDailerConn) SetDeadline(t time.Time) error {
	return nil
}
func (w *WebDailerConn) SetReadDeadline(t time.Time) error {
	return nil
}
func (w *WebDailerConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (w *WebDailerConn) Network() string {
	return "tcp"
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
	if req.Method == "GET" {
		w.fs.ServeHTTP(resp, req)
	} else {
		w.dav.ServeHTTP(resp, req)
	}
}
