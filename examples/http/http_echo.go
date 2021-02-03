package main

import (
	"fmt"
	"github.com/panjf2000/gnet"
)

const http_close_resp = `HTTP/1.1 200 OK
Server: Gnet-Server
Content-Type: text/plain; charset=UTF-8
Connection: Close

Hello world!
`

type echoGnetServer struct {
	*gnet.EventServer
}

func (es *echoGnetServer) React(frame []byte, c gnet.Conn) (out []byte, action gnet.Action) {
	in := string(frame)
	fmt.Println(in)
	out = []byte(http_close_resp)
	action = gnet.Close
	return
}

func (es *echoGnetServer) OnClosed(c gnet.Conn, err error) (action gnet.Action) {
	fmt.Println("closed")
	return
}

func main() {
	echo := new(echoGnetServer)
	_ = gnet.Serve(echo, "tcp://:9000", gnet.WithMulticore(true))

}
