package main

import (
	"flag"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/panjf2000/gnet/v2"
)

type pushServer struct {
	gnet.BuiltinEventEngine
	tick             time.Duration
	connectedSockets sync.Map
}

func (ps *pushServer) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	log.Printf("Socket with addr: %s has been opened...\n", c.RemoteAddr().String())
	ps.connectedSockets.Store(c.RemoteAddr().String(), c)
	return
}

func (ps *pushServer) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	log.Printf("Socket with addr: %s is closing...\n", c.RemoteAddr().String())
	ps.connectedSockets.Delete(c.RemoteAddr().String())
	return
}

func (ps *pushServer) OnTick() (delay time.Duration, action gnet.Action) {
	log.Println("It's time to push data to clients!!!")
	ps.connectedSockets.Range(func(key, value interface{}) bool {
		addr := key.(string)
		c := value.(gnet.Conn)
		c.AsyncWrite([]byte(fmt.Sprintf("heart beating to %s\n", addr)), nil)
		return true
	})
	delay = ps.tick
	return
}

func (ps *pushServer) OnTraffic(c gnet.Conn) gnet.Action {
	data, _ := c.Next(-1)
	c.Write(data)
	return gnet.None
}

func main() {
	var port int
	var multicore bool
	var interval time.Duration
	var ticker bool

	// Example command: go run push.go --port 9000 --tick 1s --multicore=true
	flag.IntVar(&port, "port", 9000, "server port")
	flag.BoolVar(&multicore, "multicore", true, "multicore")
	flag.DurationVar(&interval, "tick", 100, "pushing tick")
	flag.Parse()
	if interval > 0 {
		ticker = true
	}
	push := &pushServer{tick: interval}
	log.Fatal(gnet.Run(push, fmt.Sprintf("tcp://:%d", port), gnet.WithMulticore(multicore), gnet.WithTicker(ticker)))
}
