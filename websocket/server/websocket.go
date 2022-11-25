package main

import (
	"flag"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/gobwas/ws/wsutil"
	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"
)

type wsServer struct {
	gnet.BuiltinEventEngine

	addr      string
	multicore bool
	eng       gnet.Engine
	connected int64
}

func (wss *wsServer) OnBoot(eng gnet.Engine) gnet.Action {
	wss.eng = eng
	logging.Infof("echo server with multi-core=%t is listening on %s", wss.multicore, wss.addr)
	return gnet.None
}

func (wss *wsServer) OnOpen(c gnet.Conn) ([]byte, gnet.Action) {
	c.SetContext(new(wsCodec))
	atomic.AddInt64(&wss.connected, 1)
	return nil, gnet.None
}

func (wss *wsServer) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	if err != nil {
		logging.Warnf("error occurred on connection=%s, %v\n", c.RemoteAddr().String(), err)
	}
	atomic.AddInt64(&wss.connected, -1)
	logging.Infof("conn[%v] disconnected", c.RemoteAddr().String())
	return gnet.None
}

func (wss *wsServer) OnTraffic(c gnet.Conn) (action gnet.Action) {
	ws := c.Context().(*wsCodec)
	if ws.readBufferBytes(c) == gnet.Close {
		return gnet.Close
	}
	ok, action := ws.upgrade(c)
	if !ok {
		return
	}

	if ws.buf.Len() <= 0 {
		return gnet.None
	}
	messages, err := ws.Decode(c)
	if err != nil {
		return gnet.Close
	}
	if messages == nil {
		return
	}
	for _, message := range messages {
		msgLen := len(message.Payload)
		if msgLen > 128 {
			logging.Infof("conn[%v] receive [op=%v] [msg=%v..., len=%d]", c.RemoteAddr().String(), message.OpCode, string(message.Payload[:128]), len(message.Payload))
		} else {
			logging.Infof("conn[%v] receive [op=%v] [msg=%v, len=%d]", c.RemoteAddr().String(), message.OpCode, string(message.Payload), len(message.Payload))
		}
		// This is the echo server
		err = wsutil.WriteServerMessage(c, message.OpCode, message.Payload)
		if err != nil {
			logging.Infof("conn[%v] [err=%v]", c.RemoteAddr().String(), err.Error())
			return gnet.Close
		}
	}
	return gnet.None
}

func (wss *wsServer) OnTick() (delay time.Duration, action gnet.Action) {
	logging.Infof("[connected-count=%v]", atomic.LoadInt64(&wss.connected))
	return 3 * time.Second, gnet.None
}

func main() {
	var port int
	var multicore bool

	// Example command: go run main.go --port 8080 --multicore=true
	flag.IntVar(&port, "port", 9080, "server port")
	flag.BoolVar(&multicore, "multicore", true, "multicore")
	flag.Parse()

	wss := &wsServer{addr: fmt.Sprintf("tcp://127.0.0.1:%d", port), multicore: multicore}

	// Start serving!
	log.Println("server exits:", gnet.Run(wss, wss.addr, gnet.WithMulticore(multicore), gnet.WithReusePort(true), gnet.WithTicker(true)))
}
