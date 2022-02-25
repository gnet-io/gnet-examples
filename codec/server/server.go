package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/gnet-io/gnet-examples/codec/config"

	"github.com/panjf2000/gnet"
	"github.com/panjf2000/gnet/pkg/pool/goroutine"
)

type codecServer struct {
	*gnet.EventServer
	addr       string
	multicore  bool
	async      bool
	codec      gnet.ICodec
	workerPool *goroutine.Pool
}

func (cs *codecServer) OnInitComplete(srv gnet.Server) (action gnet.Action) {
	log.Printf("Test config server is listening on %s (multi-cores: %t, loops: %d)\n",
		srv.Addr.String(), srv.Multicore, srv.NumEventLoop)
	return
}

func (cs *codecServer) React(frame []byte, c gnet.Conn) (out []byte, action gnet.Action) {
	fmt.Println("frame:", string(frame))
	if cs.async {
		data := append([]byte{}, frame...)
		_ = cs.workerPool.Submit(func() {
			c.AsyncWrite(data)
		})
		return
	}
	out = frame
	return
}

func testCodecServe(addr string, multicore, async bool, codec gnet.ICodec) {
	var err error
	if codec == nil {
		codec = gnet.NewLengthFieldBasedFrameCodec(config.EncoderConfig, config.DecoderConfig)
	}
	cs := &codecServer{addr: addr, multicore: multicore, async: async, codec: codec, workerPool: goroutine.Default()}
	err = gnet.Serve(cs, addr, gnet.WithMulticore(multicore), gnet.WithTCPKeepAlive(time.Minute*5), gnet.WithCodec(codec))
	if err != nil {
		panic(err)
	}
}

func main() {
	var port int
	var multicore bool

	// Example command: go run server.go --port 9000 --multicore=true
	flag.IntVar(&port, "port", 9000, "server port")
	flag.BoolVar(&multicore, "multicore", true, "multicore")
	flag.Parse()
	addr := fmt.Sprintf("tcp://:%d", port)
	testCodecServe(addr, multicore, false, nil)
}
