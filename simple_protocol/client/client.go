package main

import (
	"bufio"
	"bytes"
	"flag"
	"io"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/panjf2000/gnet/v2/pkg/logging"

	"github.com/gnet-io/gnet-examples/simple_protocol/protocol"
)

func logErr(err error) {
	logging.Error(err)
	if err != nil {
		panic(err)
	}
}

func main() {
	var (
		network     string
		addr        string
		concurrency int
		packetSize  int
		packetBatch int
		packetCount int
	)

	// Example command: go run client.go --network tcp --address ":9000" --concurrency 100 --packet_size 1024 --packet_batch 20 --packet_count 1000
	flag.StringVar(&network, "network", "tcp", "--network tcp")
	flag.StringVar(&addr, "address", "127.0.0.1:9000", "--address 127.0.0.1:9000")
	flag.IntVar(&concurrency, "concurrency", 1024, "--concurrency 500")
	flag.IntVar(&packetSize, "packet_size", 1024, "--packe_size 256")
	flag.IntVar(&packetBatch, "packet_batch", 100, "--packe_batch 100")
	flag.IntVar(&packetCount, "packet_count", 10000, "--packe_count 10000")
	flag.Parse()

	logging.Infof("start %d clients...", concurrency)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			runClient(network, addr, packetSize, packetBatch, packetCount)
			wg.Done()
		}()
	}
	wg.Wait()
	logging.Infof("all %d clients are done", concurrency)
}

func runClient(network, addr string, packetSize, batch, count int) {
	rand.Seed(time.Now().UnixNano())
	c, err := net.Dial(network, addr)
	logErr(err)
	logging.Infof("connection=%s starts...", c.LocalAddr().String())
	defer func() {
		logging.Infof("connection=%s stops...", c.LocalAddr().String())
		c.Close()
	}()
	rd := bufio.NewReader(c)
	msg, err := rd.ReadBytes('\n')
	logErr(err)
	expectMsg := "sweetness\r\n"
	if string(msg) != expectMsg {
		logging.Fatalf("the first response packet mismatches, expect: %s, but got: %s", expectMsg, msg)
	}

	for i := 0; i < count; i++ {
		batchSendAndRecv(c, rd, packetSize, batch)
	}
}

func batchSendAndRecv(c net.Conn, rd *bufio.Reader, packetSize, batch int) {
	codec := protocol.SimpleCodec{}
	var (
		requests  [][]byte
		buf       []byte
		packetLen int
	)
	for i := 0; i < batch; i++ {
		req := make([]byte, packetSize)
		_, err := rand.Read(req)
		logErr(err)
		requests = append(requests, req)
		packet, _ := codec.Encode(req)
		packetLen = len(packet)
		buf = append(buf, packet...)
	}
	_, err := c.Write(buf)
	logErr(err)
	respPacket := make([]byte, batch*packetLen)
	_, err = io.ReadFull(rd, respPacket)
	logErr(err)
	for i, req := range requests {
		rsp, err := codec.Unpack(respPacket[i*packetLen:])
		logErr(err)
		if !bytes.Equal(req, rsp) {
			logging.Fatalf("request and response mismatch, conn=%s, packet size: %d, batch: %d",
				c.LocalAddr().String(), packetSize, batch)
		}
	}
}
