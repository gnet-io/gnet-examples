package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/panjf2000/gnet/v2"
)

type echoServer struct {
	gnet.BuiltinEventEngine
}

func (es *echoServer) OnTraffic(c gnet.Conn) gnet.Action {
	data, _ := c.Next(-1)
	c.Write(data)
	return gnet.None
}

func main() {
	var addr string
	var multicore bool

	// Example command: go run echo.go --sock echo.sock --multicore=true
	flag.StringVar(&addr, "sock", "echo.sock", "--port 9000")
	flag.BoolVar(&multicore, "multicore", false, "--multicore true")
	flag.Parse()

	echo := new(echoServer)
	log.Fatal(gnet.Run(echo, fmt.Sprintf("unix://%s", addr), gnet.WithMulticore(multicore)))
}
