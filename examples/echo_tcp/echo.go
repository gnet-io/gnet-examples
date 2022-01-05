package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/panjf2000/gnet/v2"
)

type echoServer struct {
	*gnet.BuiltinEventEngine
}

func (es *echoServer) OnTraffic(c gnet.Conn) gnet.Action {
	buf, _ := c.Next(-1)
	c.Write(buf)
	return gnet.None
}

func main() {
	var port int
	var multicore, reuseport bool

	// Example command: go run echo.go --port 9000 --multicore=true --reuseport=true
	flag.IntVar(&port, "port", 9000, "--port 9000")
	flag.BoolVar(&multicore, "multicore", false, "--multicore true")
	flag.BoolVar(&reuseport, "reuseport", false, "--reuseport true")
	flag.Parse()
	echo := new(echoServer)
	log.Fatal(gnet.Run(echo, fmt.Sprintf("tcp://:%d", port), gnet.WithMulticore(multicore), gnet.WithReusePort(reuseport)))
}
