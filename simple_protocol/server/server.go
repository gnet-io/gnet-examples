package main

import (
	"flag"
	"fmt"
	"sync/atomic"

	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"

	"github.com/gnet-io/gnet-examples/simple_protocol/protocol"
)

type simpleServer struct {
	gnet.BuiltinEventEngine
	eng          gnet.Engine
	network      string
	addr         string
	multicore    bool
	connected    int32
	disconnected int32
}

func (s *simpleServer) OnBoot(eng gnet.Engine) (action gnet.Action) {
	logging.Infof("running server on %s with multi-core=%t",
		fmt.Sprintf("%s://%s", s.network, s.addr), s.multicore)
	s.eng = eng
	return
}

func (s *simpleServer) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	c.SetContext(new(protocol.SimpleCodec))
	atomic.AddInt32(&s.connected, 1)
	out = []byte("sweetness\r\n")
	return
}

func (s *simpleServer) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	if err != nil {
		logging.Infof("error occurred on connection=%s, %v\n", c.RemoteAddr().String(), err)
	}
	disconnected := atomic.AddInt32(&s.disconnected, 1)
	connected := atomic.AddInt32(&s.connected, -1)
	if connected == 0 {
		logging.Infof("all %d connections are closed, shut it down", disconnected)
		action = gnet.Shutdown
	}
	return
}

func (s *simpleServer) OnTraffic(c gnet.Conn) (action gnet.Action) {
	codec := c.Context().(*protocol.SimpleCodec)
	var packets [][]byte
	for {
		data, err := codec.Decode(c)
		if err == protocol.ErrIncompletePacket {
			break
		}
		if err != nil {
			logging.Errorf("invalid packet: %v", err)
			return gnet.Close
		}
		packet, _ := codec.Encode(data)
		packets = append(packets, packet)
	}
	if n := len(packets); n > 1 {
		_, _ = c.Writev(packets)
	} else if n == 1 {
		_, _ = c.Write(packets[0])
	}
	return
}

func main() {
	var port int
	var multicore bool

	// Example command: go run server.go --port 9000 --multicore=true
	flag.IntVar(&port, "port", 9000, "--port 9000")
	flag.BoolVar(&multicore, "multicore", false, "--multicore=true")
	flag.Parse()
	ss := &simpleServer{
		network:   "tcp",
		addr:      fmt.Sprintf(":%d", port),
		multicore: multicore,
	}
	err := gnet.Run(ss, ss.network+"://"+ss.addr, gnet.WithMulticore(multicore))
	logging.Infof("server exits with error: %v", err)
}
