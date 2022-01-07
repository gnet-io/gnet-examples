package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/evanphx/wildcat"
	"github.com/panjf2000/gnet"
)

type httpServer struct {
	*gnet.EventServer
}

type httpCodec struct {
	parser *wildcat.HTTPParser
	buf    []byte
}

func (hc *httpCodec) appendResponse() {
	hc.buf = append(hc.buf, "HTTP/1.1 200 OK\r\nServer: gnet\r\nContent-Type: text/plain\r\nDate: "...)
	hc.buf = time.Now().AppendFormat(hc.buf, "Mon, 02 Jan 2006 15:04:05 GMT")
	hc.buf = append(hc.buf, "\r\nContent-Length: 12\r\n\r\nHello World!"...)
}

func (hs *httpServer) OnInitComplete(srv gnet.Server) (action gnet.Action) {
	log.Printf("HTTP server is listening on %s (multi-cores: %t, event-loops: %d)\n",
		srv.Addr.String(), srv.Multicore, srv.NumEventLoop)
	return
}

func (hs httpServer) OnOpened(c gnet.Conn) ([]byte, gnet.Action) {
	c.SetContext(&httpCodec{parser: wildcat.NewHTTPParser()})
	return nil, gnet.None
}

func (hs *httpServer) React(data []byte, c gnet.Conn) (out []byte, action gnet.Action) {
	hc := c.Context().(*httpCodec)

pipeline:
	headerOffset, err := hc.parser.Parse(data)
	if err != nil {
		return []byte("500 Error"), gnet.Close
	}
	hc.appendResponse()
	bodyLen := int(hc.parser.ContentLength())
	if bodyLen == -1 {
		bodyLen = 0
	}
	data = data[headerOffset+bodyLen:]
	if len(data) > 0 {
		goto pipeline
	}

	// handle the request
	out = hc.buf
	hc.buf = hc.buf[:0]
	return
}

func main() {
	var port int
	var multicore bool

	// Example command: go run main.go --port 8080 --multicore=true
	flag.IntVar(&port, "port", 8080, "server port")
	flag.BoolVar(&multicore, "multicore", true, "multicore")
	flag.Parse()

	http := new(httpServer)

	// Start serving!
	log.Println("server exists:", gnet.Serve(http, fmt.Sprintf("tcp://127.0.0.1:%d", port), gnet.WithMulticore(multicore)))
}
