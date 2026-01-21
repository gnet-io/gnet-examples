package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gobwas/httphead"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsflate"
	"github.com/gobwas/ws/wsutil"

	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"
)

type WsCodec struct {
	preMessagePayload  []byte // 上一个帧的数据
	currMessagePayload []byte // 当前帧的数据
	upgraded           bool
	compression        bool
	preMessageOpCode   ws.OpCode
	currMessageOpCode  ws.OpCode
	messageRsv         byte // 0~7 的整数，由于控制帧可以穿插在消息帧的分片中，但是控制帧又没有 rsv，所有只需一个字段即可，当控制帧穿插进来的时候，无需转移保存
}

func (w *WsCodec) resetCurrMessage() {
	if w.preMessageOpCode == 255 {
		w.currMessageOpCode = 255
		w.messageRsv = 0
		w.currMessagePayload = w.currMessagePayload[:0]
	} else {
		w.currMessageOpCode = w.preMessageOpCode
		w.currMessagePayload = w.preMessagePayload
		w.preMessageOpCode = 255
		w.preMessagePayload = nil
	}
}

func (w *WsCodec) upgrade(c gnet.Conn) gnet.Action {
	peek, err := c.Peek(-1)
	if err != nil {
		return gnet.Close
	}
	//判断 http head 是否完整
	if l := len(peek); l < 4 || bytes.Equal(peek[l-4:], []byte("\r\n\r\n")) == false {
		return gnet.None
	}
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(peek)))
	if err != nil {
		//返回 HTTP 400
		resp := http.Response{
			StatusCode: http.StatusBadRequest,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Body:       io.NopCloser(strings.NewReader(http.StatusText(http.StatusBadRequest))),
		}
		_ = resp.Write(c)
		return gnet.Close
	}
	if req.Method != "GET" {
		//响应 HTTP 405
		resp := http.Response{
			StatusCode: http.StatusMethodNotAllowed,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Body:       io.NopCloser(strings.NewReader(http.StatusText(http.StatusMethodNotAllowed))),
		}
		_ = resp.Write(c)
		return gnet.Close
	}
	//没有Upgrade头，说明无须升级协议，响应一个正常的http ok
	if _, ok := req.Header["Upgrade"]; ok == false {
		resp := http.Response{
			StatusCode: http.StatusOK,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Body:       io.NopCloser(strings.NewReader("nice to meet you")),
		}
		_ = resp.Write(c)
		return gnet.Close
	}

	//验证 Upgrade 头
	if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") == false {
		resp := http.Response{
			StatusCode: http.StatusBadRequest,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Body:       io.NopCloser(strings.NewReader("invalid Upgrade header")),
		}
		_ = resp.Write(c)
		return gnet.Close
	}

	//验证 Connection 头包含 "Upgrade"
	if connection := req.Header.Get("Connection"); connection != "Upgrade" && connection != "upgrade" {
		resp := http.Response{
			StatusCode: http.StatusBadRequest,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Body:       io.NopCloser(strings.NewReader("invalid Connection header")),
		}
		_ = resp.Write(c)
		return gnet.Close
	}

	//验证 Sec-WebSocket-Version == 13
	if req.Header.Get("Sec-WebSocket-Version") != "13" {
		// 返回 426 Upgrade Required + 正确版本
		resp := http.Response{
			StatusCode: http.StatusUpgradeRequired,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header: http.Header{
				"Sec-WebSocket-Version": []string{"13"},
			},
			Body: io.NopCloser(strings.NewReader(http.StatusText(http.StatusUpgradeRequired))),
		}
		_ = resp.Write(c)
		return gnet.Close
	}

	//协商压缩扩展
	if ext := req.Header.Get("Sec-WebSocket-Extensions"); ext != "" {
		options, ok := httphead.ParseOptions([]byte(ext), nil)
		if ok == false {
			resp := http.Response{
				StatusCode: http.StatusBadRequest,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Body:       io.NopCloser(strings.NewReader("invalid Sec-WebSocket-Extensions header")),
			}
			_ = resp.Write(c)
			return gnet.Close
		}
		for _, option := range options {
			if bytes.Equal(option.Name, wsflate.ExtensionNameBytes) {
				w.compression = true
				break
			}
		}
	}

	//获取 Sec-WebSocket-Key
	key := req.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		resp := http.Response{
			StatusCode: http.StatusBadRequest,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Body:       io.NopCloser(strings.NewReader("missing Sec-WebSocket-Key")),
		}
		_ = resp.Write(c)
		return gnet.Close
	}
	//计算 Sec-WebSocket-Accept
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	acceptKey := base64.StdEncoding.EncodeToString(h.Sum(nil))

	//构造 101 Switching Protocols 响应
	resp := http.Response{
		StatusCode: http.StatusSwitchingProtocols,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header: http.Header{
			"Upgrade":              []string{"websocket"},
			"Connection":           []string{"Upgrade"},
			"Sec-WebSocket-Accept": []string{acceptKey},
		},
	}

	//添加压缩扩展
	if w.compression {
		//两个参数直接决定了系统的内存开销和压缩率，除非你的并发连接数非常少，且对压缩率有极致要求，否则永远选择 no_context_takeover。
		//这是一个典型的用少量性能损失换取巨大可伸缩性和稳定性的架构决策。
		// server_no_context_takeover //告诉客户端，服务器不会为客户端的不同消息复用同一个 LZ77 滑动窗口（即压缩上下文），每次压缩都是独立的。
		// client_no_context_takeover 告诉客户端，它在解压缩来自服务器的消息时，也不应该复用解压上下文。
		resp.Header["sec-websocket-extensions"] = []string{"permessage-deflate; server_no_context_takeover; client_no_context_takeover"}
	}

	//清除所有请求数据，有些请求会在get中添加数据，所以需要清空
	_, err = c.Discard(c.InboundBuffered())
	if err != nil {
		return gnet.Close
	}

	//发送响应
	err = resp.Write(c)
	if err != nil {
		return gnet.Close
	}

	//升级成功
	w.upgraded = true
	return gnet.None
}

func (w *WsCodec) decode(c gnet.Conn) (ws.StatusCode, error) {
	for {
		peek, err := c.Peek(-1)
		if err != nil {
			if errors.Is(err, io.ErrShortBuffer) {
				//数据不完整
				return ws.StatusCode(0), io.ErrShortBuffer
			}
			return ws.StatusInternalServerError, err
		}
		if len(peek) < 2 {
			return ws.StatusCode(0), io.ErrShortBuffer
		}
		//ws.ReadHeader()
		var head ws.Header
		head.Fin = peek[0]&0x80 != 0
		head.Rsv = (peek[0] & 0x70) >> 4
		head.OpCode = ws.OpCode(peek[0] & 0x0f)
		validOp := head.OpCode == ws.OpContinuation ||
			head.OpCode == ws.OpText ||
			head.OpCode == ws.OpBinary ||
			head.OpCode == ws.OpClose ||
			head.OpCode == ws.OpPing ||
			head.OpCode == ws.OpPong
		if !validOp {
			return ws.StatusUnsupportedData, errors.New("unsupported opcode")
		}
		if head.Rsv != 0 {
			//未协商开启压缩扩展，RSV1 bits MUST be 0
			if w.compression == false {
				return ws.StatusProtocolError, errors.New("RSV1 bits must be 0 without extensions")
			} else if head.OpCode.IsControl() {
				//控制帧的 RSV1 不对
				return ws.StatusProtocolError, errors.New("RSV1 must be 0")
			}
		}
		//已经得到了首帧，此刻的是后续帧
		if w.currMessageOpCode != 255 {
			//控制帧不允许分片
			if w.currMessageOpCode.IsControl() {
				return ws.StatusProtocolError, errors.New("control message MUST NOT be fragmented")
			}
			//后续帧
			if head.OpCode.IsControl() {
				//控制帧穿插在数据帧分片之间，保存之前的数据帧
				w.preMessageOpCode = w.currMessageOpCode
				w.preMessagePayload = w.currMessagePayload
				w.currMessageOpCode = 255
				w.currMessagePayload = nil
			} else {
				//数据帧的连续帧的 opcode 不对
				if head.OpCode != ws.OpContinuation {
					return ws.StatusProtocolError, errors.New("non-first fragment must be continuation")
				}
				//协商已开启压缩扩展，连续帧的 RSV1 不对
				if w.compression && head.Rsv != 0 {
					return ws.StatusProtocolError, errors.New("RSV1 must be 0")
				}
			}
		}
		if peek[1]&0x80 == 0 {
			return ws.StatusProtocolError, errors.New("client must mask data")
		}
		var extra = 0
		length := peek[1] & 0x7f
		switch {
		case length < 126:
			head.Length = int64(length)
			extra = 4 // 2 bytes header + 4 bytes mask
		case length == 126:
			extra = 6 // 2 bytes header + 2 bytes length + 4 bytes mask
		case length == 127:
			extra = 12 // 2 bytes header + 8 bytes length + 4 bytes mask
		default:
			return ws.StatusProtocolError, errors.New("unexpected payload length bits")
		}
		if len(peek) < 2+extra {
			//数据不完整
			return ws.StatusCode(0), io.ErrShortBuffer
		}
		peek = peek[2:]
		switch {
		case length == 126:
			head.Length = int64(binary.BigEndian.Uint16(peek[:2]))
			peek = peek[2:]
		case length == 127:
			if peek[0]&0x80 != 0 {
				return ws.StatusProtocolError, errors.New("the most significant bit must be 0")
			}
			head.Length = int64(binary.BigEndian.Uint64(peek[:8]))
			peek = peek[8:]
		}
		//校验 Ping/Pong/Close 的 payload 长度
		if head.OpCode.IsControl() && head.Length > 125 {
			return ws.StatusProtocolError, errors.New("control frame too long")
		}
		if len(peek) < (int)(head.Length)+4 {
			//数据不完整
			return ws.StatusCode(0), io.ErrShortBuffer
		}
		copy(head.Mask[:], peek[:4])
		//即将得到一个完整的消息帧，此刻可以记住首帧的opcode、RSV
		if w.currMessageOpCode == 255 {
			if w.compression && head.Rsv != 4 && head.Rsv != 0 {
				//协商启用了压缩扩展，首帧的 RSV1 不对
				return ws.StatusProtocolError, errors.New("RSV1 must be 0 or 4")
			}
			w.currMessageOpCode = head.OpCode
			w.messageRsv = head.Rsv
		}
		if cap(w.currMessagePayload) == 0 {
			w.currMessagePayload = make([]byte, 0, (int)(head.Length))
		}
		w.currMessagePayload = append(w.currMessagePayload, peek[4:4+(int)(head.Length)]...)
		_, err = c.Discard(2 + extra + (int)(head.Length))
		if err != nil {
			return ws.StatusInternalServerError, err
		}
		ws.Cipher(w.currMessagePayload[len(w.currMessagePayload)-(int)(head.Length):], head.Mask, 0)
		if head.Fin {
			//当前 header 已经是一个完整消息
			if w.currMessageOpCode == ws.OpText {
				//解压缩数据
				if w.compression && w.messageRsv == 4 {
					w.currMessagePayload, err = wsflate.DefaultHelper.Decompress(w.currMessagePayload)
					if err != nil {
						logging.Infof("invalid deflate stream: %s", err)
						return ws.StatusInvalidFramePayloadData, errors.New("invalid deflate stream")
					}
				}
				//文本消息，校验utf8
				if utf8.Valid(w.currMessagePayload) == false {
					return ws.StatusInvalidFramePayloadData, errors.New("invalid utf8 in text message")
				}
			} else if w.currMessageOpCode == ws.OpBinary {
				//解压缩数据
				if w.compression && w.messageRsv == 4 {
					w.currMessagePayload, err = wsflate.DefaultHelper.Decompress(w.currMessagePayload)
					if err != nil {
						logging.Infof("invalid deflate stream: %s", err)
						return ws.StatusInvalidFramePayloadData, errors.New("invalid deflate stream")
					}
				}
			} else if w.currMessageOpCode == ws.OpClose && len(w.currMessagePayload) > 0 {
				//关闭消息，校验关闭码
				pl := len(w.currMessagePayload)
				if pl == 1 || pl > 125 {
					return ws.StatusProtocolError, errors.New("close frame payload length invalid")
				}
				if pl >= 2 {
					code := ws.StatusCode(binary.BigEndian.Uint16(w.currMessagePayload[:2]))
					invalidCode := code.In(ws.StatusRangeNotInUse) ||
						code == ws.StatusNoMeaningYet ||
						code == ws.StatusNoStatusRcvd ||
						code == ws.StatusAbnormalClosure ||
						code == 1016 ||
						code == 1100 ||
						code == 2000 ||
						code == 2999
					if invalidCode {
						return ws.StatusProtocolError, errors.New("invalid close status code")
					}
					if pl > 2 && utf8.Valid(w.currMessagePayload[2:]) == false {
						return ws.StatusInvalidFramePayloadData, errors.New("invalid utf8 in text message")
					}
				}
			}
			return ws.StatusCode(0), nil
		}
	}
}

type WsServer struct {
	gnet.BuiltinEventEngine

	addr      string
	eng       gnet.Engine
	connected int64
}

func (wss *WsServer) OnBoot(eng gnet.Engine) gnet.Action {
	wss.eng = eng
	return gnet.None
}

func (wss *WsServer) OnOpen(c gnet.Conn) ([]byte, gnet.Action) {
	w := new(WsCodec)
	w.preMessageOpCode = 255
	w.currMessageOpCode = 255
	c.SetContext(w)
	atomic.AddInt64(&wss.connected, 1)
	return nil, gnet.None
}

func (wss *WsServer) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	if err != nil {
		isLog := true
		if errors.Is(err, io.EOF) {
			isLog = false
		} else {
			var sysErr *os.SyscallError
			if errors.As(err, &sysErr) && sysErr.Err.Error() == "connection reset by peer" {
				isLog = false
			}
		}
		if isLog {
			logging.Warnf("error occurred on connection=%s, %+v %T\n", c.RemoteAddr().String(), err, err)
		}
	}
	atomic.AddInt64(&wss.connected, -1)
	return gnet.None
}

func (wss *WsServer) OnTraffic(c gnet.Conn) (action gnet.Action) {
	wsCodec := c.Context().(*WsCodec)
	if !wsCodec.upgraded {
		return wsCodec.upgrade(c)
	}
loop:
	statusCode, err := wsCodec.decode(c)
	if err != nil {
		if !statusCode.Empty() {
			logging.Infof("error reading message! %d --> %v", statusCode, err)
			//数据帧解析错误，立即构造并发送close帧
			payload := ws.NewCloseFrameBody(statusCode, err.Error())
			_ = wsutil.WriteServerMessage(c, ws.OpClose, payload)
			return gnet.Close
		}
		//等待更多数据
		if errors.Is(err, io.ErrShortBuffer) {
			return gnet.None
		}
		logging.Infof("error reading message! %d --> %v", statusCode, err)
		//这行代码应该不会执行
		return gnet.Close
	}
	if wsCodec.currMessageOpCode == ws.OpPing {
		//返回pong，并将ping的payload一并返回
		err = wsutil.WriteServerMessage(c, ws.OpPong, wsCodec.currMessagePayload)
		if err != nil {
			logging.Infof("error writing ping message! %v", err.Error())
			return gnet.Close
		}
		wsCodec.resetCurrMessage()
		if c.InboundBuffered() > 0 {
			goto loop
		}
		return gnet.None
	} else if wsCodec.currMessageOpCode.IsData() {
		if wsCodec.compression {
			//协商已开启扩展，压缩数据
			wsCodec.currMessagePayload, err = wsflate.DefaultHelper.Compress(wsCodec.currMessagePayload)
			if err != nil {
				logging.Infof("error compressing message! %v", err.Error())
				return gnet.Close
			}
		}
		frame := ws.NewFrame(wsCodec.currMessageOpCode, true, wsCodec.currMessagePayload)
		frame.Header.Rsv = wsCodec.messageRsv
		err = ws.WriteFrame(c, frame)
		if err != nil {
			logging.Infof("conn[%v] [err=%v]", c.RemoteAddr().String(), err.Error())
			return gnet.Close
		}
	} else if wsCodec.currMessageOpCode == ws.OpClose {
		//返回close，回显客户端的状态码和原因
		_ = wsutil.WriteServerMessage(c, ws.OpClose, wsCodec.currMessagePayload)
		return gnet.Close
	} else if wsCodec.currMessageOpCode.IsReserved() {
		//不支持的数据帧
		payload := ws.NewCloseFrameBody(ws.StatusUnsupportedData, "unsupported opcode")
		_ = wsutil.WriteServerMessage(c, ws.OpClose, payload)
		return gnet.Close
	} else if wsCodec.currMessageOpCode == ws.OpPong {
		//不处理
		wsCodec.resetCurrMessage()
		if c.InboundBuffered() > 0 {
			//缓冲区还有数据，继续解析
			goto loop
		}
		return gnet.None
	}
	wsCodec.resetCurrMessage()
	if c.InboundBuffered() > 0 {
		//继续处理数据
		goto loop
	}
	return gnet.None
}

func (wss *WsServer) OnTick() (delay time.Duration, action gnet.Action) {
	logging.Infof("[connected-count=%v]", atomic.LoadInt64(&wss.connected))
	return 3 * time.Second, gnet.None
}

func main() {
	var port int
	var multicore bool

	// Example command: go run main.go --port 8080 --multicore=true
	flag.IntVar(&port, "port", 9080, "server port")
	flag.BoolVar(&multicore, "multicore", false, "multicore")
	flag.Parse()

	wss := &WsServer{addr: fmt.Sprintf("tcp://127.0.0.1:%d", port)}

	// Start serving!
	log.Println("server exits:", gnet.Run(
		wss,
		wss.addr,
		gnet.WithMulticore(multicore),
		gnet.WithTicker(true),
	))
}
