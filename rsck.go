package rsck

import (
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/Centny/gwf/log"
)

var ShowLog int

func log_d(f string, args ...interface{}) {
	if ShowLog > 1 {
		log.D_(1, f, args...)
	}
}

// func ParseForwardUri(uri string) (network, local, name, remote string, limit int, err error) {
// 	parts := strings.SplitN(uri, ">", 2)
// 	if len(parts) < 2 {
// 		err = fmt.Errorf("invalid forward uri(%v)", uri)
// 		return
// 	}
// 	lurl, err := url.Parse(parts[0])
// 	if err != nil {
// 		return
// 	}
// 	network = lurl.Scheme
// 	local = lurl.Host
// 	rurl, err := url.Parse(parts[1])
// 	if err != nil {
// 		return
// 	}
// 	name = rurl.Scheme
// 	remote = rurl.Host
// 	lv := rurl.Query().Get("limit")
// 	if len(lv) > 0 {
// 		limit, err = strconv.Atoi(lv)
// 	}
// 	return
// }

type DuplexPiped struct {
	UpReader   *os.File
	UpWriter   *os.File
	DownReader *os.File
	DownWriter *os.File
	closed     uint32
}

func (d *DuplexPiped) Close() error {
	if !atomic.CompareAndSwapUint32(&d.closed, 0, 1) {
		return fmt.Errorf("closed")
	}
	d.UpWriter.Close()
	d.DownWriter.Close()
	return nil
}

type PipedConn struct {
	piped *DuplexPiped
	up    bool
}

func CreatePipedConn() (a, b *PipedConn, err error) {
	piped := &DuplexPiped{}
	piped.UpReader, piped.DownWriter, err = os.Pipe()
	if err == nil {
		piped.DownReader, piped.UpWriter, err = os.Pipe()
	}
	if err == nil {
		a = &PipedConn{
			piped: piped,
			up:    true,
		}
		b = &PipedConn{
			piped: piped,
			up:    false,
		}
	}
	return
}

func (p *PipedConn) Read(b []byte) (n int, err error) {
	if p.up {
		n, err = p.piped.UpReader.Read(b)
	} else {
		n, err = p.piped.DownReader.Read(b)
	}
	return
}

func (p *PipedConn) Write(b []byte) (n int, err error) {
	if p.up {
		n, err = p.piped.UpWriter.Write(b)
	} else {
		n, err = p.piped.DownWriter.Write(b)
	}
	return
}

func (p *PipedConn) Close() error {
	return p.piped.Close()
}

func (p *PipedConn) LocalAddr() net.Addr {
	return p
}
func (p *PipedConn) RemoteAddr() net.Addr {
	return p
}
func (p *PipedConn) SetDeadline(t time.Time) error {
	return nil
}
func (p *PipedConn) SetReadDeadline(t time.Time) error {
	return nil
}
func (p *PipedConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (p *PipedConn) Network() string {
	return "piped"
}
func (p *PipedConn) String() string {
	return "piped"
}
