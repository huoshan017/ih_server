package socket

import (
	"bytes"
	"compress/flate"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"ih_server/libs/log"
	"ih_server/libs/timer"
	"io"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
)

type E_DISCONNECT_REASON int32

const (
	cRECV_LEN        = 4096
	cMSG_BUFFER_LEN  = 8192
	cRECV_BUFFER_LEN = 4096
	cMSG_SEND_LEN    = 32768
	MSG_HEAD_LEN     = 6 // 整个消息头长度
	MSG_HEAD_SUB_LEN = 3 // 消息头除去长度之后的长度
	MSG_TAIL_LEN     = 2

	E_DISCONNECT_REASON_NONE                    E_DISCONNECT_REASON = 0
	E_DISCONNECT_REASON_INTERNAL_ERROR          E_DISCONNECT_REASON = 1
	E_DISCONNECT_REASON_SERVER_SHUTDOWN         E_DISCONNECT_REASON = 2
	E_DISCONNECT_REASON_NET_ERROR               E_DISCONNECT_REASON = 3
	E_DISCONNECT_REASON_OTHER_PLACE_LOGIN       E_DISCONNECT_REASON = 4
	E_DISCONNECT_REASON_PACKET_MALFORMED        E_DISCONNECT_REASON = 11
	E_DISCONNECT_REASON_CLIENT_DISCONNECT       E_DISCONNECT_REASON = 16
	E_DISCONNECT_REASON_PACKET_HANDLE_EXCEPTION E_DISCONNECT_REASON = 19
	E_DISCONNECT_REASON_FORCE_CLOSED_CLIENT     E_DISCONNECT_REASON = 20
)

type MessageItem struct {
	type_id uint16
	data    []byte
}

type RecentGroupInfo struct {
	RecentGroups []*MessageGroup
	CurGroupIdx  int32
}

type MessageGroup struct {
}

type TcpConn struct {
	addr              string
	node              *Node
	c                 *net.TCPConn
	is_server         bool
	closing           bool
	closed            bool
	self_closing      bool
	recv_chan         chan []byte
	disc_chan         chan E_DISCONNECT_REASON
	send_chan         chan *MessageItem
	send_chan_closed  bool
	send_lock         *sync.Mutex
	handing           bool
	start_time        time.Time
	last_message_time time.Time
	bytes_sended      int64
	bytes_recved      int64

	w_msg_hd []byte

	// tls
	tls_c net.Conn

	T             int64
	I             interface{}
	State         interface{}
	RecvSeq       int32
	LastSendGroup *MessageGroup
}

func new_conn(node *Node, conn *net.TCPConn, is_server bool) *TcpConn {
	c := TcpConn{}
	c.node = node
	c.c = conn
	c.is_server = is_server
	c.send_chan = make(chan *MessageItem, 256)
	c.recv_chan = make(chan []byte, 256)
	c.disc_chan = make(chan E_DISCONNECT_REASON, 128)
	c.addr = conn.RemoteAddr().String()
	c.send_lock = &sync.Mutex{}
	c.start_time = time.Now()
	c.w_msg_hd = make([]byte, 7)

	return &c
}

func tls_new_conn(node *Node, conn net.Conn, is_server bool) *TcpConn {
	c := TcpConn{}
	c.node = node
	c.tls_c = conn
	c.is_server = is_server
	c.send_chan = make(chan *MessageItem, 256)
	c.recv_chan = make(chan []byte, 256)
	c.disc_chan = make(chan E_DISCONNECT_REASON, 128)
	c.addr = conn.RemoteAddr().String()
	c.send_lock = &sync.Mutex{}
	c.start_time = time.Now()
	c.w_msg_hd = make([]byte, 7)

	return &c
}

func (node *TcpConn) GetAddr() (addr string) {
	return node.addr
}

func (node *TcpConn) IsServer() (ok bool) {
	return node.is_server
}

func (node *TcpConn) push_timeout(r bool, w bool) {
	c := node.GetConn()
	if c == nil {
		return
	}

	if r {
		if node.node.recv_timeout > 0 {
			c.SetReadDeadline(time.Now().Add(node.node.recv_timeout))
		}
	}
	if w {
		if node.node.send_timeout > 0 {
			c.SetWriteDeadline(time.Now().Add(node.node.send_timeout))
		}
	}
}

func (node *TcpConn) err(err error, desc string) {
	if !strings.Contains(err.Error(), "use of closed network connection") &&
		!strings.Contains(err.Error(), "An existing connection was forcibly closed by the remote host") &&
		!strings.Contains(err.Error(), "An established connection was aborted by the software in your host machine") &&
		!strings.Contains(err.Error(), "connected party did not properly respond after a period of time") {
		log.Error("[%v][玩家%v][%v][%v][%v]", node.addr, node.T, err, desc, reflect.TypeOf(err))
	}
	log.Trace("[%v][玩家%v][%v][%v][%v]", node.addr, node.T, err, desc, reflect.TypeOf(err))
}

func (node *TcpConn) release() {
	defer func() {
		node.closed = true
	}()

	c := node.GetConn()
	if c == nil {
		return
	}

	defer node.node.remove_conn(node)

	c.Close()
	close(node.disc_chan)
	close(node.recv_chan)
	if !node.send_chan_closed {
		node.send_chan_closed = true
		close(node.send_chan)
	}

	log.Trace("[%v][玩家%v]连接已释放 <总共发送 %v 字节><总共接收 %v 字节>", node.addr, node.T, node.bytes_sended, node.bytes_recved)
}

func (node *TcpConn) event_disc(reason E_DISCONNECT_REASON) {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	if node.closed {
		return
	}

	log.Trace("event_disc reason %v", reason)

	node.closing = true
	if !node.send_chan_closed {
		close(node.send_chan)
		node.send_chan_closed = true
	}
	node.disc_chan <- reason
}

func (node *TcpConn) Send(msgid uint16, msg proto.Message) {
	if msg == nil {
		log.Error("[%v][玩家%v] 消息为空", node.addr, node.T)
		node.event_disc(E_DISCONNECT_REASON_INTERNAL_ERROR)
		return
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		log.Error("[%v][玩家%v]序列化数据失败 %v %v", node.addr, node.T, err, msg.String())
		node.event_disc(E_DISCONNECT_REASON_INTERNAL_ERROR)
		return
	}

	length := int32(len(data))
	node.bytes_sended += int64(length)

	log.Debug("发送[%v][玩家%v][%v][%v][%v][%v字节][%v字节] %v ",
		node.addr, node.T, length, node.bytes_sended, "{{"+msg.String()+"}}")

	node.send_data(msgid, data)
}

func (node *TcpConn) send_data(type_id uint16, data []byte) {
	defer func() {
		if err := recover(); err != nil {
			log.Error("[%v][玩家%v]%v send_data", node.addr, node.T, node.node.get_message_name(type_id))
			log.Stack(err)
		}
	}()

	node.send_lock.Lock()
	defer node.send_lock.Unlock()

	if node.closing || node.closed {
		return
	}

	length := int32(len(data)) + 7
	if length > cMSG_SEND_LEN-4 {
		log.Error("msg length too long %v %v", node.node.get_message_name(type_id), length)
		return
	}

	item := &MessageItem{}
	item.type_id = type_id
	item.data = data
	node.event_send(item)
}

func (node *TcpConn) event_send(item *MessageItem) {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	if node.closed || node.closing {
		return
	}

	node.send_chan <- item
}

type Handler func(c *TcpConn, m proto.Message)

type AckHandler func(c *TcpConn, seq uint8)

type handler_info struct {
	t reflect.Type
	h Handler
}

func (node *TcpConn) send_loop() {
	b_remote_closed := false
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}

		log.Trace("send loop quit %v", node.addr)
		if !b_remote_closed {
			node.event_disc(E_DISCONNECT_REASON_NET_ERROR)
		} else {
			node.event_disc(E_DISCONNECT_REASON_FORCE_CLOSED_CLIENT)
		}
	}()

	c := node.GetConn()
	if c == nil {
		log.Trace("net connection disconnected, get conn failed")
		return
	}

	var h [MSG_HEAD_LEN]byte
	for item := range node.send_chan {
		if node.closing {
			log.Trace("send loop quit node.closing 1 %v", node.addr)
			return
		}

		node.push_timeout(false, true)
		if false { // d.length > 2048
			var b bytes.Buffer
			w, err := flate.NewWriter(&b, 1)
			if err != nil {
				node.err(err, "flate.NewWriter failed")
				return
			}
			_, err = w.Write(item.data)
			if err != nil {
				node.err(err, "write_msgs flate.Write failed")
				return
			}
			err = w.Close()
			if err != nil {
				node.err(err, "flate.Close failed")
				return
			}
			data := b.Bytes()

			length := len(data) + MSG_TAIL_LEN
			h[0] = byte(length)
			h[1] = byte(length >> 8)
			h[2] = byte(length >> 16)
			h[3] = 1
			h[4] = byte(item.type_id)
			h[5] = byte(item.type_id >> 8)
			_, err = c.Write(h[:])
			if err != nil {
				b_remote_closed = true
				node.err(err, "write compress header failed")
				return
			}
			_, err = c.Write(data)
			if err != nil {
				b_remote_closed = true
				node.err(err, "write compress body failed")
				return
			}

			/*
				_, err = c.Write(msg_tail[:])
				if nil != err {
					b_remote_closed = true
					node.err(err, "write tail failed")
					return
				}
			*/
		} else {
			length := len(item.data) + MSG_TAIL_LEN

			h[0] = byte(length)
			h[1] = byte(length >> 8)
			h[2] = byte(length >> 16)
			h[3] = 0
			h[4] = byte(item.type_id)
			h[5] = byte(item.type_id >> 8)

			_, err := c.Write(h[:])
			if err != nil {
				b_remote_closed = true
				node.err(err, "write header failed")
				return
			}

			_, err = c.Write(item.data)
			if err != nil {
				b_remote_closed = true
				node.err(err, "write body failed")
				return
			}
		}

		if node.closing {
			log.Trace("Send Loop Quit node.closing 2 !")
			return
		}

		time.Sleep(time.Millisecond * 5)
	}
}

func (node *TcpConn) event_recv(data []byte) {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	if node.closed || node.closing {
		return
	}

	node.recv_chan <- data
}

func (node *TcpConn) on_recv(d []byte) {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
			node.event_disc(E_DISCONNECT_REASON_PACKET_HANDLE_EXCEPTION)
		}
	}()

	node.last_message_time = time.Now()

	length := int32(len(d))
	node.bytes_recved += int64(length)
	if length < MSG_HEAD_SUB_LEN {
		log.Error("[%v][玩家%v]长度小于MSG_HEAD_SUB_LEN", node.addr, node.T)
		node.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
		return
	}

	ctrl := d[0]
	compressed := (ctrl&0x01 != 0)

	type_id := uint16(0)
	type_id += uint16(d[1]) + (uint16(d[2]) << 8)

	var data []byte
	if compressed {
		b := new(bytes.Buffer)
		reader := bytes.NewReader(d[MSG_HEAD_SUB_LEN:length])
		r := flate.NewReader(reader)
		_, err := io.Copy(b, r)
		if err != nil {
			defer r.Close()
			log.Error("[%v][玩家%v]decompress copy failed %v", node.addr, node.T, err)
			node.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
			return
		}
		err = r.Close()
		if err != nil {
			log.Error("[%v][玩家%v]flate Close failed %v", node.addr, node.T, err)
			node.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
			return
		}
		data = b.Bytes()
	} else {
		data = d[MSG_HEAD_SUB_LEN:length]
	}

	//log.Error("OnRecv msg %v %v %v %v", type_id, d, data)

	p := node.node.handler_map[type_id]
	if p.t == nil {
		log.Error("[%v][玩家%v]消息句柄为空 %v", node.addr, node.T, node.node.get_message_name(type_id))
		//node.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
		return
	}

	msg := reflect.New(p.t).Interface().(proto.Message)
	err := proto.Unmarshal(data[:], msg)
	if err != nil {
		log.Error("[%v][玩家%v]反序列化失败 %v data:%v err%s", node.addr, node.T, node.node.get_message_name(type_id), data, err.Error())
		node.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
		return
	}

	log.Debug("接收[%v][玩家%v][%v][%v字节][%v字节]%v",
		node.addr,
		node.T,
		len(d),
		node.bytes_recved,
		"{{"+msg.String()+"}}")

	begin := time.Now()

	if node.is_server {
		node.handing = true
		p.h(node, msg)
		node.handing = false
	} else {
		p.h(node, msg)
	}

	time_cost := time.Since(begin).Seconds()
	if time_cost > 3 {
		log.Trace("[时间过长 %v][%v][玩家%v]消息%v", time_cost, node.addr, node.T, node.node.get_message_name(type_id))
	//} else {
		//log.Trace("[时间%v][%v][玩家%v]消息%v", time_cost, node.addr, node.T, node.node.get_message_name(type_id))
	}
}

func (node *TcpConn) recv_loop() {
	bforeclosed := false
	reason := E_DISCONNECT_REASON_NET_ERROR
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)

		}

		log.Trace("recv loop quit %v", node.addr)

		if bforeclosed {
			node.event_disc(E_DISCONNECT_REASON_FORCE_CLOSED_CLIENT)
		} else {
			node.event_disc(reason)
		}

	}()

	c := node.GetConn()
	if c == nil {
		return
	}

	l := uint32(0)
	extra_len := int(MSG_HEAD_SUB_LEN - MSG_TAIL_LEN)
	mb := bytes.NewBuffer(make([]byte, 0, cMSG_BUFFER_LEN))
	rb := make([]byte, cRECV_BUFFER_LEN)
	for {
		node.push_timeout(true, false)
		if node.closing {
			bforeclosed = true
			log.Trace("recv loop quit when closing 1")
			break
		}

		rl, err := c.Read(rb)

		if node.closing {
			bforeclosed = true
			log.Trace("recv loop quit when closing 2")
			break
		}

		if io.EOF == err {
			log.Error("[%v][玩家%v]另一端关闭了连接", node.addr, node.T)
			reason = E_DISCONNECT_REASON_CLIENT_DISCONNECT
			return
		}

		if nil != err {
			net_err := err.(net.Error)
			if !net_err.Timeout() {
				bforeclosed = true
				log.Error("recv_loop Read error(%s) !", err.Error())
				//node.err(err, "读取数据失败")
				return
			}
		}

		//log.Info("收到客户端数据%d: %v", rl, rb[:rl])
		mb.Write(rb[:rl])

		if l == 0 && mb.Len() >= 3 {
			b, err := mb.ReadByte()
			if nil != err {
				node.err(err, "读取长度失败1")
				return
			}
			l += uint32(b)

			b, err = mb.ReadByte()
			if nil != err {
				node.err(err, "读取长度失败2")
				return
			}
			l += uint32(b) << 8

			b, err = mb.ReadByte()
			if nil != err {
				node.err(err, "读取长度失败3")
				return
			}
			l += uint32(b) << 16

			if l > cRECV_LEN || l < 2 {
				node.err(errors.New("消息长度不合法 "+strconv.Itoa(int(l))), "")
				return
			}

			//log.Info("读取到消息长度", l)
		}

		if l > 0 && mb.Len() >= int(l)+extra_len {

			n := mb.Next(int(l) + extra_len)
			d := make([]byte, len(n))
			for i := int(0); i < len(n); i++ {
				d[i] = n[i]
			}

			if node.closing || node.closed {
				log.Trace("recv loop quit when closing")
				return
			}

			node.event_recv(d)

			l = 0

			time.Sleep(time.Millisecond * 100)
		//} else {
			//log.Info("需要更多的数据", l, mb.Len(), int(l)+extra_len)
			//break
		}
	}
}

func (node *TcpConn) main_loop() {
	var disc_reason E_DISCONNECT_REASON
	defer func() {
		defer func() {
			if err := recover(); err != nil {
				log.Stack(err)
			}
		}()

		defer node.release()

		if err := recover(); err != nil {
			log.Stack(err)
		}

		log.Trace("main_loop quit %v %v", node.T, disc_reason)

		if node.node.callback != nil {
			node.node.callback.OnDisconnect(node, disc_reason)
		}

		log.Trace("main loop quit completed")
	}()

	t := timer.NewTickTimer(node.node.update_interval_ms)
	t.Start()
	defer t.Stop()

	for {
		select {
		case d, ok := <-node.recv_chan:
			{
				if !ok {
					disc_reason = E_DISCONNECT_REASON_INTERNAL_ERROR
					return
				}

				node.on_recv(d)

				if node.self_closing {
					begin := time.Now()
					for {
						select {
						case reason, ok := <-node.disc_chan:
							{
								log.Trace("self disc chan %v", ok)
								if !ok {
									disc_reason = E_DISCONNECT_REASON_INTERNAL_ERROR
									return
								}

								disc_reason = reason

								return
							}
						default:
							{
								//nothing
							}
						}

						time.Sleep(time.Duration(100))
						if dur := time.Since(begin).Seconds(); dur > 3 {
							log.Trace("wait client close timeout %v", dur)
							return
						}
					}
				}
			}
		case reason, ok := <-node.disc_chan:
			{
				if !ok {
					disc_reason = E_DISCONNECT_REASON_INTERNAL_ERROR
					return
				}

				disc_reason = reason

				return
			}
		case d, ok := <-t.Chan:
			{
				if !ok {
					disc_reason = E_DISCONNECT_REASON_INTERNAL_ERROR
					return
				}

				if node.node.callback != nil {
					node.node.callback.OnUpdate(node, d)
				}
			}
		}
	}
}

func (node *TcpConn) Close(reason E_DISCONNECT_REASON) {
	if node == nil {
		return
	}

	log.Trace("[%v][玩家%v]关闭连接", node.addr, node.T)

	if node.handing {
		log.Trace("self closing")
		node.self_closing = true
		return
	} else {
		node.event_disc(reason)
	}

	begin := time.Now()
	for {
		if dur := time.Since(begin).Seconds(); dur > 2 {
			log.Trace("等待断开超时 %v", dur)
			break
		}

		if node.closing {
			log.Trace("wait remote close closing break")
			break
		}

		if node.closed {
			log.Trace("wait remote close closed break")
			return
		}

		time.Sleep(time.Millisecond * 100)
	}

	if !node.closing && !node.closed {
		log.Trace("关闭连接 event_disc")
		node.event_disc(reason)
	}

	begin = time.Now()
	logged := false
	for {
		if node.closed {
			return
		}

		time.Sleep(time.Millisecond * 100)

		if dur := time.Since(begin).Seconds(); dur > 5 {
			if !logged {
				logged = true
				log.Error("关闭连接超时 %v %v %v", node.addr, node.T, dur)
			}
		}
	}
}

func (node *TcpConn) IsClosing() (closing bool) {
	return node.closing
}

func (node *TcpConn) GetStartTime() (t time.Time) {
	return node.start_time
}

func (node *TcpConn) GetLastMessageTime() (t time.Time) {
	return node.last_message_time
}

func (node *TcpConn) GetConn() (conn net.Conn) {
	if node.c != nil {
		conn = node.c
	} else {
		conn = node.tls_c
	}
	return
}

type ICallback interface {
	OnAccept(c *TcpConn)
	OnConnect(c *TcpConn)
	OnUpdate(c *TcpConn, t timer.TickTime)
	OnDisconnect(c *TcpConn, reason E_DISCONNECT_REASON)
}

type Node struct {
	addr               *net.TCPAddr
	max_conn           int32
	callback           ICallback
	recv_timeout       time.Duration
	send_timeout       time.Duration
	update_interval_ms int32
	listener           *net.TCPListener
	client             *TcpConn
	quit               bool
	type_names         map[uint16]string
	handler_map        map[uint16]handler_info
	ack_handler        AckHandler
	conn_map           map[*TcpConn]int32
	conn_map_lock      *sync.RWMutex
	shutdown_lock      *sync.Mutex
	initialized        bool

	// tls
	tls_listener net.Listener
	tls_config   *tls.Config

	I interface{}
}

func NewNode(cb ICallback, recv_timeout time.Duration, send_timeout time.Duration, update_interval_ms int32, type_names map[uint16]string) (node *Node) {
	node = &Node{}
	node.callback = cb
	node.recv_timeout = recv_timeout * time.Millisecond
	node.send_timeout = send_timeout * time.Millisecond
	node.update_interval_ms = update_interval_ms
	node.handler_map = make(map[uint16]handler_info)
	node.conn_map = make(map[*TcpConn]int32)
	node.conn_map_lock = &sync.RWMutex{}
	node.shutdown_lock = &sync.Mutex{}
	node.initialized = true
	node.type_names = make(map[uint16]string)
	for i, v := range type_names {
		node.type_names[i] = v
	}

	return node
}

func (node *Node) UseTls(cert_file string, key_file string) (err error) {
	cert, err := tls.LoadX509KeyPair(cert_file, key_file)
	if err != nil {
		return
	}

	node.tls_config = &tls.Config{
		//RootCAs: pool,
		//InsecureSkipVerify: true,
		//ClientAuth: tls.RequireAndVerifyClientCert,
		Certificates:             []tls.Certificate{cert},
		CipherSuites:             []uint16{tls.TLS_RSA_WITH_RC4_128_SHA},
		PreferServerCipherSuites: true,
	}

	now := time.Now()
	node.tls_config.Time = func() time.Time { return now }
	node.tls_config.Rand = rand.Reader

	return
}

func (node *Node) UseTlsClient() {
	node.tls_config = &tls.Config{
		InsecureSkipVerify: true,
	}

	now := time.Now()
	node.tls_config.Time = func() time.Time { return now }
	node.tls_config.Rand = rand.Reader
}

func (node *Node) add_conn(c *TcpConn) {
	node.conn_map_lock.Lock()
	defer node.conn_map_lock.Unlock()

	node.conn_map[c] = 0
}

func (node *Node) remove_conn(c *TcpConn) {
	node.conn_map_lock.Lock()
	defer node.conn_map_lock.Unlock()

	delete(node.conn_map, c)
}

func (node *Node) ConnCount() (n int32) {
	node.conn_map_lock.RLock()
	defer node.conn_map_lock.RUnlock()

	return int32(len(node.conn_map))
}

func (node *Node) get_all_conn() (conn_map map[*TcpConn]int32) {
	node.conn_map_lock.RLock()
	defer node.conn_map_lock.RUnlock()

	conn_map = make(map[*TcpConn]int32)
	for i, v := range node.conn_map {
		conn_map[i] = v
	}

	return
}

func (node *Node) SetHandler(type_id uint16, typ reflect.Type, h Handler) {
	_, has := node.handler_map[type_id]
	if has {
		log.Error("[%v]消息处理函数已设置,将被替换 %v %v", node.addr, type_id, node.get_message_name(type_id))
	}
	node.handler_map[type_id] = handler_info{typ, h}
}

func (node *Node) GetHandler(type_id uint16) (h Handler) {
	hi, has := node.handler_map[type_id]
	if !has {
		return nil
	}

	return hi.h
}

func (node *Node) get_message_name(type_id uint16) (name string) {
	if node.type_names == nil {
		return ""
	}

	name, ok := node.type_names[type_id]
	if ok {
		return name
	}

	return ""
}

func (node *Node) SetAckHandler(h AckHandler) {
	if node.ack_handler != nil {
		log.Error("[%v]ack句柄已经设置,将被替换", node.addr)
	}
	node.ack_handler = h
}

func (node *Node) Listen(server_addr string, max_conn int32) (err error) {
	addr, err := net.ResolveTCPAddr("tcp", server_addr)
	if err != nil {
		return
	}

	node.addr = addr
	node.max_conn = max_conn

	if node.tls_config == nil {
		var l *net.TCPListener
		l, err = net.ListenTCP(node.addr.Network(), node.addr)
		if err != nil {
			return
		}
		node.listener = l
	} else {
		var tls_l net.Listener
		tls_l, err = tls.Listen("tcp", server_addr, node.tls_config)
		if err != nil {
			return
		}
		node.tls_listener = tls_l
	}

	var delay time.Duration
	var conn *net.TCPConn
	var tls_conn net.Conn
	log.Trace("[%v]服务器监听中", node.addr)
	for !node.quit {
		log.Trace("[%v]等待新的连接", node.addr)
		if node.tls_config == nil {
			conn, err = node.listener.AcceptTCP()
		} else {
			tls_conn, err = node.tls_listener.Accept()
		}
		if err != nil {
			if node.quit {
				return nil
			}
			if net_err, ok := err.(net.Error); ok && net_err.Temporary() {
				if delay == 0 {
					delay = 5 * time.Millisecond
				} else {
					delay *= 2
				}
				if max := 1 * time.Second; delay > max {
					delay = max
				}
				log.Trace("Accept error: %v; retrying in %v", err, delay)
				time.Sleep(delay)
				continue
			}
			return err
		}

		delay = 0

		if node.quit {
			break
		}

		if node.tls_config == nil {
			log.Trace("[%v]客户端连接成功 %v", node.addr, conn.RemoteAddr())
			if node.max_conn > 0 {
				if node.ConnCount() > node.max_conn {
					log.Trace("[%v]已达到最大连接数 %v", node.addr, node.max_conn)
					conn.Close()
					continue
				}
			}
		} else {
			log.Trace("[%v]客户端连接成功 %v", node.addr, tls_conn.RemoteAddr())
			if node.max_conn > 0 {
				if node.ConnCount() > node.max_conn {
					log.Trace("[%v]已达到最大连接数 %v", node.addr, node.max_conn)
					tls_conn.Close()
					continue
				}
			}
		}

		var c *TcpConn
		if node.tls_config == nil {
			conn.SetKeepAlive(true)
			conn.SetKeepAlivePeriod(time.Minute * 2)
			c = new_conn(node, conn, true)
		} else {
			c = tls_new_conn(node, tls_conn, true)
		}

		node.add_conn(c)

		go c.send_loop()
		go c.recv_loop()
		if node.callback != nil {
			node.callback.OnAccept(c)
		}
		go c.main_loop()
	}

	return nil
}

func (node *Node) Connect(server_addr string, timeout time.Duration) (err error) {
	addr, err := net.ResolveTCPAddr("tcp", server_addr)
	if err != nil {
		return
	}
	node.addr = addr
	node.send_timeout = timeout

	var conn *net.TCPConn
	var tls_conn net.Conn
	if node.tls_config == nil {
		conn, err = net.DialTCP(node.addr.Network(), nil, node.addr)
	} else {
		tls_conn, err = tls.Dial("tcp", server_addr, node.tls_config)
	}

	if err != nil {
		return
	}

	log.Trace("[%v]连接成功", server_addr)

	var c *TcpConn
	if node.tls_config == nil {
		c = new_conn(node, conn, false)
	} else {
		c = tls_new_conn(node, tls_conn, false)
	}

	c.I = node.I
	node.client = c
	go c.send_loop()
	go c.recv_loop()
	if node.callback != nil {
		node.callback.OnConnect(c)
	}
	go c.main_loop()

	return
}

func (node *Node) GetClient() (c *TcpConn) {
	return node.client
}

func (node *Node) Shutdown() {
	log.Trace("[%v]关闭网络服务", node.addr)
	if !node.initialized {
		return
	}

	node.shutdown_lock.Lock()
	defer node.shutdown_lock.Unlock()

	if node.quit {
		return
	}
	node.quit = true

	begin := time.Now()

	if node.tls_config == nil {
		if node.listener != nil {
			node.listener.Close()
		}
	} else {
		if node.tls_listener != nil {
			node.tls_listener.Close()
		}
	}

	conn_map := node.get_all_conn()
	log.Trace("[%v]总共 %v 个连接需要关闭", node.addr, len(conn_map))
	for k := range conn_map {
		go k.Close(E_DISCONNECT_REASON_SERVER_SHUTDOWN)
	}

	for {
		conn_count := node.ConnCount()
		if conn_count == 0 {
			break
		}

		time.Sleep(time.Millisecond * 100)
	}

	log.Trace("[%v]关闭网络服务耗时 %v 秒", node.addr, time.Since(begin).Seconds())
}

func (node *Node) StopListen() {
	if node.tls_config == nil {
		if node.listener != nil {
			node.listener.Close()
		}
	} else {
		if node.tls_listener != nil {
			node.tls_listener.Close()
		}
	}
}
