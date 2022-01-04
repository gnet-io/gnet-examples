package main

import (
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/panjf2000/gnet"

	"github.com/gnet-io/gnet-examples/examples/custom_codec/protocol"
)

// Example command: go run client.go
type codeClient struct {
	*gnet.EventServer
	wg sync.WaitGroup
}

func (cs *codeClient) React(frame []byte, c gnet.Conn) (out []byte, action gnet.Action) {
	fmt.Println("received: ", string(frame))
	cs.wg.Done()
	return
}

func (cs *codeClient) OnOpened(c gnet.Conn) ([]byte, gnet.Action) {
	item := protocol.CustomLengthFieldProtocol{Version: protocol.DefaultProtocolVersion, ActionType: protocol.ActionData}
	c.SetContext(item)
	return nil, gnet.None
}

func main() {
	var port int
	var count int
	// Example command: go run client.go --port 9000 --count 10
	flag.IntVar(&port, "port", 9000, "server port")
	flag.IntVar(&count, "count", 10, "message count")
	flag.Parse()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	codec := &protocol.CustomLengthFieldProtocol{}
	cs := &codeClient{wg: sync.WaitGroup{}}
	client, err := gnet.NewClient(
		cs,
		gnet.WithCodec(codec),
		gnet.WithTCPNoDelay(gnet.TCPNoDelay),
		gnet.WithTCPKeepAlive(time.Minute*5),
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

	// store customize protocol header param using `c.SetContext()`

	cs.wg.Add(count)

	for i := 0; i < count; i++ {
		err = conn.AsyncWrite([]byte("hello"))
		if err != nil {
			panic(err)
		}
	}
	cs.wg.Wait() // wait for completing async write operation
}
