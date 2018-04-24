package rsck

import (
	"io"
	"net"
)

type EchoServer struct {
	L net.Listener
}

func NewEchoServer(network, address string) (echo *EchoServer, err error) {
	echo = &EchoServer{}
	echo.L, err = net.Listen(network, address)
	return
}

func (e *EchoServer) runAccept() {
	for {
		con, err := e.L.Accept()
		if err != nil {
			break
		}
		go io.Copy(con, con)
	}
}

func (e *EchoServer) Start() {
	go e.runAccept()
}
