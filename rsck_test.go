package rsck

import (
	"fmt"
	"io"
	"testing"
	"time"
)

// func TestParseForwardUri(t *testing.T) {
// 	fmt.Println(ParseForwardUri("udp://:2342>test://localhost:5342"))
// }

func TestPipeConn(t *testing.T) {
	a, b, err := CreatePipedConn()
	if err != nil {
		t.Error("error")
		return
	}
	go io.Copy(b, b)
	fmt.Fprintf(a, "val-%v", 0)
	buf := make([]byte, 100)
	readed, err := a.Read(buf)
	if err != nil {
		t.Error("error")
		return
	}
	fmt.Printf("-->%v\n", string(buf[0:readed]))
	a.Close()
	b.Close()
	//for cover
	a.SetDeadline(time.Now())
	a.SetReadDeadline(time.Now())
	a.SetWriteDeadline(time.Now())
	fmt.Println(a.LocalAddr(), a.RemoteAddr(), a.String(), a.Network())
}
