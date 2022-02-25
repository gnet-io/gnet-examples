package main

import (
	"flag"
	"fmt"
	"sync"

	"github.com/gnet-io/gnet-examples/codec/config"

	"github.com/panjf2000/gnet"
)

type codeClient struct {
	*gnet.EventServer
	wg sync.WaitGroup
}

func (cs *codeClient) React(frame []byte, c gnet.Conn) (out []byte, action gnet.Action) {
	fmt.Println("received: ", string(frame))
	cs.wg.Done()
	return
}

func main() {
	var port int
	var count int
	// Example command: go run client.go --port 9000 --count 10
	flag.IntVar(&port, "port", 9000, "server port")
	flag.IntVar(&count, "count", 10, "message count")
	flag.Parse()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	codec := gnet.NewLengthFieldBasedFrameCodec(config.EncoderConfig, config.DecoderConfig)
	cs := &codeClient{wg: sync.WaitGroup{}}
	client, err := gnet.NewClient(
		cs,
		gnet.WithCodec(codec),
		gnet.WithTCPNoDelay(gnet.TCPNoDelay),
	)
	if err != nil {
		panic(err)
	}

	err = client.Start()
	if err != nil {
		panic(err)
	}

	conn, err := client.Dial("tcp", addr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	cs.wg.Add(count)

	for i := 0; i < count; i++ {
		err = conn.AsyncWrite([]byte("hello world"))
		if err != nil {
			panic(err)
		}
	}
	cs.wg.Wait() // wait for completing async write operation
}
