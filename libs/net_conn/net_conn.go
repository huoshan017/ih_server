package net_conn

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
	cRECV_LEN            = 4096
	cMSG_BUFFER_LEN      = 8192
	cRECV_BUFFER_LEN     = 4096
	cMSG_SEND_LEN        = 32768
	cMSG_HEADER_LENGTH   = 9
	cPACKET_HISTORY_SIZE = 10

	E_DISCONNECT_REASON_NONE                    E_DISCONNECT_REASON = 0
	E_DISCONNECT_REASON_INTERNAL_ERROR          E_DISCONNECT_REASON = 1
	E_DISCONNECT_REASON_SERVER_SHUTDOWN         E_DISCONNECT_REASON = 2
	E_DISCONNECT_REASON_NET_ERROR               E_DISCONNECT_REASON = 3
	E_DISCONNECT_REASON_MAX_CONNECTIONS_REACHED E_DISCONNECT_REASON = 4
	E_DISCONNECT_REASON_MAX_PLAYERS_REACHED     E_DISCONNECT_REASON = 5
	E_DISCONNECT_REASON_KICK_BY_GM              E_DISCONNECT_REASON = 6
	E_DISCONNECT_REASON_LOGIN_FROM_OTHER_PLACE  E_DISCONNECT_REASON = 7
	E_DISCONNECT_REASON_LOGIC_ERROR             E_DISCONNECT_REASON = 9
	E_DISCONNECT_REASON_PLAYER_NOT_LOGGED       E_DISCONNECT_REASON = 10
	E_DISCONNECT_REASON_PACKET_MALFORMED        E_DISCONNECT_REASON = 11
	E_DISCONNECT_REASON_VERSION_CHECK_FAILED    E_DISCONNECT_REASON = 12
	E_DISCONNECT_REASON_LOGGIN_FAILED           E_DISCONNECT_REASON = 13
	E_DISCONNECT_REASON_ALREADY_LOGGED          E_DISCONNECT_REASON = 15
	E_DISCONNECT_REASON_CLIENT_DISCONNECT       E_DISCONNECT_REASON = 16
	E_DISCONNECT_REASON_LOGIN_TIMEOUT           E_DISCONNECT_REASON = 17
	E_DISCONNECT_REASON_LONG_TIME_NO_ACTION     E_DISCONNECT_REASON = 18
	E_DISCONNECT_REASON_PACKET_HANDLE_EXCEPTION E_DISCONNECT_REASON = 19
	E_DISCONNECT_REASON_FORCE_CLOSED            E_DISCONNECT_REASON = 20
)

type MessageItem struct {
	type_id uint16
	data    []byte
	next    *MessageItem
}

type MessageGroup struct {
	length int32
	first  *MessageItem
	last   *MessageItem
}

func (this *TcpConn) GetAddr() (addr string) {
	return this.addr
}

func (this *TcpConn) IsServer() (ok bool) {
	return this.is_server
}

func (this *TcpConn) push_timeout(r bool, w bool) {
	c := this.c
	if c == nil {
		return
	}
	if r {
		if this.node.recv_timeout > 0 {
			c.SetReadDeadline(time.Now().Add(this.node.recv_timeout))
		}
	}
	if w {
		if this.node.send_timeout > 0 {
			c.SetWriteDeadline(time.Now().Add(this.node.send_timeout))
		}
	}
}

func (this *TcpConn) err(err error, desc string) {
	if !strings.Contains(err.Error(), "use of closed network connection") &&
		!strings.Contains(err.Error(), "An existing connection was forcibly closed by the remote host") &&
		!strings.Contains(err.Error(), "An established connection was aborted by the software in your host machine") &&
		!strings.Contains(err.Error(), "connected party did not properly respond after a period of time") {
		log.Error("[ip:%v][%v][%v][%v]", this.addr, err, desc, reflect.TypeOf(err))
	}
	log.Trace("[ip:%v][%v][%v][%v]", this.addr, err, desc, reflect.TypeOf(err))
}

func (this *TcpConn) release() {
	defer func() {
		this.closed = true
	}()

	c := this.GetConn()
	if c == nil {
		return
	}

	defer c.Close()
	defer close(this.disc_chan)
	defer close(this.recv_chan)
	defer close(this.send_chan)
	defer this.node.remove_conn(this)

	log.Trace("[ip:%v]??????????????? <???????????? %v ??????><???????????? %v ??????>", this.addr, this.bytes_sended, this.bytes_recved)
}

func (this *TcpConn) event_disc(reason E_DISCONNECT_REASON) {
	defer func() {
		if err := recover(); err != nil {
			//nothing
		}
	}()

	if this.closed {
		return
	}

	log.Trace("reason %v", reason)

	this.closing = true

	this.disc_chan <- reason
}

func (this *TcpConn) event_recv(data []byte) {
	defer func() {
		if err := recover(); err != nil {
			//nothing
		}
	}()

	if this.closed || this.closing {
		return
	}

	this.recv_chan <- data
}

func (this *TcpConn) event_send(group *MessageGroup) {
	defer func() {
		if err := recover(); err != nil {
			//nothing
		}
	}()

	if this.closed || this.closing {
		return
	}

	this.send_chan <- group
}

func (this *TcpConn) send_data(type_id uint16, data []byte, immediate bool) {
	defer func() {
		if err := recover(); err != nil {
			log.Error("[ip:%v] send_data (type_id:%d)", this.addr, type_id)
			log.Stack(err)
		}
	}()

	this.send_lock.Lock()
	defer this.send_lock.Unlock()

	if this.closing || this.closed {
		return
	}

	length := int32(len(data)) + 4
	if length > cMSG_SEND_LEN-4 {
		log.Error("msg(%d) length(%d) too long", type_id, length)
		return
	}

	item := &MessageItem{}
	item.type_id = type_id
	item.data = data

	if !immediate && this.handing {
		if this.send_group == nil {
			this.send_group = &MessageGroup{}
		} else {
			if this.send_group.length+length > cMSG_SEND_LEN {
				log.Error("GREAT %v", type_id)
				this.event_send(this.send_group)
				this.send_group = &MessageGroup{}
			} //no
		}

		this.send_group.length += length
		if this.send_group.last == nil {
			this.send_group.first = item
			this.send_group.last = item
		} else {
			this.send_group.last.next = item
			this.send_group.last = item
		}

		this.event_send(this.send_group)
		this.send_group = nil
	} else {
		if this.send_group != nil {
			this.event_send(this.send_group)
			this.send_group = nil
		}

		group := &MessageGroup{}
		group.length = length
		group.first = item
		this.event_send(group)
	}
}

func (this *TcpConn) send_flush() {
	defer func() {
		if err := recover(); err != nil {
			log.Error("[ip:%v] send_flush", this.addr)
			log.Stack(err)
		}
	}()

	this.send_lock.Lock()
	defer this.send_lock.Unlock()

	if this.closing || this.closed {
		return
	}

	if this.send_group != nil {
		this.event_send(this.send_group)
		this.send_group = nil
	}
}

func (this *TcpConn) Send(msgid uint16, msg proto.Message, immediate bool) {
	if msg == nil {
		log.Error("[ip:%v] ????????????", this.addr)
		this.event_disc(E_DISCONNECT_REASON_INTERNAL_ERROR)
		return
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		log.Error("[ip:%v] ????????????????????? %v %v", this.addr, err, msg.String())
		this.event_disc(E_DISCONNECT_REASON_INTERNAL_ERROR)
		return
	}

	length := int32(len(data))
	this.bytes_sended += int64(length)

	//log.Trace("??????[ip:%v][%v??????][%v??????] %v ",
	//	this.addr, length, this.bytes_sended, "{"+msg.MessageTypeName()+":{"+msg.String()+"}}")

	this.send_data(msgid, data, immediate)
}

func (this *TcpConn) on_recv(d []byte) {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
			this.event_disc(E_DISCONNECT_REASON_PACKET_HANDLE_EXCEPTION)
		}
	}()

	this.last_message_time = time.Now()

	length := int32(len(d))
	this.bytes_recved += int64(length)
	if length < 2 {
		log.Error("[ip:%v] ????????????2", this.addr)
		this.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
		return
	}

	ctrl := d[0]
	compressed := (ctrl&0x01 != 0)

	var data []byte
	if compressed {
		b := new(bytes.Buffer)
		reader := bytes.NewReader(d[1:])
		r := flate.NewReader(reader)
		_, err := io.Copy(b, r)
		if err != nil {
			defer r.Close()
			log.Error("[ip:%v] decompress copy failed %v", this.addr, err)
			this.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
			return
		}
		err = r.Close()
		if err != nil {
			log.Error("[ip:%v] flate Close failed %v", this.addr, err)
			this.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
			return
		}
		data = b.Bytes()
	} else {
		data = d[1:]
	}

	for {
		if len(data) < 4 {
			log.Error("[ip:%v] ??????????????????7", this.addr)
			this.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
			return
		}

		var type_id uint16
		type_id = uint16(data[0])
		type_id = type_id << 8
		type_id += uint16(data[1])

		var ml uint16
		ml = uint16(data[2])
		ml = ml << 8
		ml += uint16(data[3])

		//log.Error("OnRecv msg %v %v %v %v", type_id, ml, d, data)

		if len(data) < 4+int(ml) {
			log.Error("[ip:%v] ????????????????????????", this.addr)
			this.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
			return
		}

		p := this.node.handler_map[type_id]
		if p.t == nil {
			log.Warn("[ip:%v] ??????[%v]????????????", this.addr, type_id)
			this.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
			return
		}

		msg := reflect.New(p.t).Interface().(proto.Message)
		err := proto.Unmarshal(data[4:4+ml], msg)
		if err != nil {
			log.Error("[ip:%v] ??????[%v]??????????????????", this.addr, type_id)
			this.event_disc(E_DISCONNECT_REASON_PACKET_MALFORMED)
			return
		}

		/*log.Trace("??????[ip:%v][%v??????][%v??????][ml:%v??????]%v",
		this.addr,
		len(d),
		this.bytes_recved,
		ml,
		"{"+msg.MessageTypeName()+":{"+msg.String()+"}}")*/

		begin := time.Now()

		if this.is_server {
			this.handing = true
			p.h(this, msg)
			this.handing = false
			this.send_flush()
		} else {
			p.h(this, msg)
		}

		time_cost := time.Now().Sub(begin).Seconds()
		if time_cost > 3 {
			// ??????????????????protobuf????????????????????????????????????ID?????????????????????
			log.Warn("[???????????? %v][ip:%v]??????%v", time_cost, this.addr /*msg.MessageTypeId()*/, 0)
		} else {
			//log.Trace("[??????%v][%v][??????%v]??????%v", time_cost, this.addr, this.T, this.node.get_message_name(type_id))
		}

		if 4+int(ml) == len(data) {
			break
		}

		data = data[4+ml:]
	}

	return
}

type Handler func(c *TcpConn, m proto.Message)

type AckHandler func(c *TcpConn, seq uint8)

type handler_info struct {
	t reflect.Type
	h Handler
}

func (this *TcpConn) write_msgs(group *MessageGroup, w io.Writer) (err error) {
	m := group.first
	if m == nil {
		return errors.New("no msg to write")
	}

	count := 0
	bytes := 0

	for {
		var h [4]byte
		h[0] = byte(m.type_id >> 8)
		h[1] = byte(m.type_id)
		h[2] = byte(len(m.data) >> 8)
		h[3] = byte(len(m.data))
		_, err = w.Write(h[:])
		if err != nil {
			this.err(err, "write msg header failed")
			return
		}
		_, err = w.Write(m.data)
		if err != nil {
			this.err(err, "write msg body failed")
			return
		}

		count++
		bytes += (4 + len(m.data))

		m = m.next
		if m == nil {
			break
		}
	}

	if count > 1 {
		//log.Error("batch %v %v", count, bytes)
	}

	return nil
}

func (this *TcpConn) send_loop() {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}

		log.Trace("send loop quit %v", this.addr)

		this.event_disc(E_DISCONNECT_REASON_NET_ERROR)
	}()

	c := this.GetConn()
	if c == nil {
		return
	}
	for {
		select {
		case d, ok := <-this.send_chan:
			{
				if this.closing {
					return
				}

				if !ok {
					return
				}

				//log.Error(d.seq)
				this.push_timeout(false, true)
				if d.length > 128 {
					var b bytes.Buffer
					w, err := flate.NewWriter(&b, 1)
					if err != nil {
						this.err(err, "flate.NewWriter failed")
						return
					}
					err = this.write_msgs(d, w)
					if err != nil {
						this.err(err, "write_msgs flate.Write failed")
						return
					}
					err = w.Close()
					if err != nil {
						this.err(err, "flate.Close failed")
						return
					}
					data := b.Bytes()
					length := len(data) + 1
					var h [3]byte
					h[0] = byte(length >> 8)
					h[1] = byte(length)
					h[2] |= 0x01
					_, err = c.Write(h[:])
					if err != nil {
						this.err(err, "write compress header failed")
						return
					}
					_, err = c.Write(data)
					if err != nil {
						this.err(err, "write compress body failed")
						return
					}

					//log.Error("compress %v %v", d.length-int32(len(data)), d.length)
				} else {
					length := d.length + 1
					var h [3]byte
					h[0] = byte(length >> 8)
					h[1] = byte(length)
					h[2] = 0
					_, err := c.Write(h[:])
					if err != nil {
						this.err(err, "write header failed")
						return
					}
					err = this.write_msgs(d, c)
					if err != nil {
						this.err(err, "write body failed")
						return
					}
				}

				if this.closing {
					return
				}

				time.Sleep(time.Millisecond * 5)
			}
		}
	}
}

func (this *TcpConn) recv_loop() {
	reason := E_DISCONNECT_REASON_NET_ERROR
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)

		}

		log.Trace("recv loop quit %v", this.addr)

		this.event_disc(reason)
	}()

	c := this.GetConn()
	l := uint16(0)
	mb := bytes.NewBuffer(make([]byte, 0, cMSG_BUFFER_LEN))
	rb := make([]byte, cRECV_BUFFER_LEN)
	for {
		this.push_timeout(true, false)
		if this.closing {
			log.Trace("recv loop quit when closing 1")
			break
		}

		rl, err := c.Read(rb)

		if this.closing {
			log.Trace("recv loop quit when closing 2")
			break
		}

		if err == io.EOF {
			log.Trace("[ip:%v] ????????????????????????", this.addr)
			reason = E_DISCONNECT_REASON_CLIENT_DISCONNECT
			return
		}

		if err != nil {
			this.err(err, "??????????????????")
			return
		}

		mb.Write(rb[:rl])

		for {
			if l == 0 && mb.Len() >= 2 {
				b, err := mb.ReadByte()
				if err != nil {
					this.err(err, "??????????????????")
					return
				}
				l = uint16(b)
				l = l << 8
				b, err = mb.ReadByte()
				if err != nil {
					this.err(err, "??????????????????2")
					return
				}
				l += uint16(b)
				if l > cRECV_LEN {
					this.err(errors.New("???????????? "+strconv.Itoa(int(l))), "")
					return
				}
			}
			if l > 0 && mb.Len() >= int(l) {
				n := mb.Next(int(l))
				d := make([]byte, len(n))
				for i, v := range n {
					d[i] = v
				}

				if this.closing || this.closed {
					log.Trace("recv loop quit when closing")
					return
				}

				this.event_recv(d)

				l = 0

				//time.Sleep(time.Millisecond * 100)
			} else {
				break
			}
		}
	}
}

func (this *TcpConn) main_loop() {
	var disc_reason E_DISCONNECT_REASON
	defer func() {
		defer func() {
			if err := recover(); err != nil {
				log.Stack(err)
			}
		}()

		defer this.release()

		if err := recover(); err != nil {
			log.Stack(err)
		}

		log.Trace("main_loop quit %v %v", this.T, disc_reason)

		if this.node.callback != nil {
			this.node.callback.OnDisconnect(this, disc_reason)
		}

		log.Trace("main loop quit completed")
	}()

	t := timer.NewTickTimer(this.node.update_interval_ms)
	t.Start()
	defer t.Stop()

	for {
		select {
		case d, ok := <-this.recv_chan:
			{
				if !ok {
					disc_reason = E_DISCONNECT_REASON_INTERNAL_ERROR
					log.Trace("recv_chan read error")
					return
				}

				this.on_recv(d)

				if this.self_closing {
					begin := time.Now()
					for {
						select {
						case reason, ok := <-this.disc_chan:
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

						time.Sleep(100)
						if dur := time.Now().Sub(begin).Seconds(); dur > 3 {
							log.Trace("wait client close timeout %v", dur)
							return
						}
					}
				}
			}
		case reason, ok := <-this.disc_chan:
			{
				if !ok {
					disc_reason = E_DISCONNECT_REASON_INTERNAL_ERROR
					log.Trace("disc_chan read error")
					return
				}

				disc_reason = reason
				log.Trace("disc_chan read reason %v", disc_reason)

				return
			}
		case d, ok := <-t.Chan:
			{
				if !ok {
					disc_reason = E_DISCONNECT_REASON_INTERNAL_ERROR
					log.Trace("t.Chan read error")
					return
				}

				if this.node.callback != nil {
					this.node.callback.OnUpdate(this, d)
				}
			}
		}
	}
}

func (this *TcpConn) Close(reason E_DISCONNECT_REASON) {
	if this == nil {
		return
	}

	log.Trace("??????[ip:%v] ??????", this.addr)

	if this.handing {
		log.Trace("self closing")
		this.self_closing = true
		return
	}

	begin := time.Now()
	for {
		if dur := time.Now().Sub(begin).Seconds(); dur > 2 {
			log.Trace("?????????????????? %v", dur)
			break
		}

		if this.closing {
			log.Trace("wait remote close closing break")
			break
		}

		if this.closed {
			log.Trace("wait remote close closed break")
			return
		}

		time.Sleep(time.Millisecond * 100)
	}

	if !this.closing && !this.closed {
		log.Trace("???????????? event_disc")
		this.event_disc(reason)
	}

	begin = time.Now()
	logged := false
	for {
		if this.closed {
			return
		}

		time.Sleep(time.Millisecond * 100)

		if dur := time.Now().Sub(begin).Seconds(); dur > 5 {
			if !logged {
				logged = true
				log.Error("?????????????????? %v %v %v", this.addr, this.T, dur)
			}
		}
	}
}

func (this *TcpConn) IsClosing() (closing bool) {
	return this.closing
}

func (this *TcpConn) GetStartTime() (t time.Time) {
	return this.start_time
}

func (this *TcpConn) GetLastMessageTime() (t time.Time) {
	return this.last_message_time
}

func (this *TcpConn) GetConn() (conn net.Conn) {
	if this.c == nil {
		conn = this.tls_c
	} else {
		conn = this.c
	}
	return
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
	send_chan         chan *MessageGroup
	send_lock         *sync.Mutex
	handing           bool
	send_group        *MessageGroup
	start_time        time.Time
	last_message_time time.Time
	bytes_sended      int64
	bytes_recved      int64

	// tls
	tls_c net.Conn

	T     int32
	I     interface{}
	State interface{}
}

func common_conn(c *TcpConn, node *Node, is_server bool, send_msg_count, recv_msg_count, disc_reason_count int32) {
	c.node = node
	c.is_server = is_server
	if send_msg_count == 0 {
		send_msg_count = 32
	}
	c.send_chan = make(chan *MessageGroup, send_msg_count)
	if recv_msg_count == 0 {
		recv_msg_count = 8
	}
	c.recv_chan = make(chan []byte, recv_msg_count)
	if disc_reason_count == 0 {
		disc_reason_count = 32
	}
	c.disc_chan = make(chan E_DISCONNECT_REASON, disc_reason_count)
	c.send_lock = &sync.Mutex{}
	c.start_time = time.Now()
}

func new_conn(node *Node, conn *net.TCPConn, is_server bool, send_msg_count, recv_msg_count, disc_reason_count int32) *TcpConn {
	c := &TcpConn{}
	common_conn(c, node, is_server, send_msg_count, recv_msg_count, disc_reason_count)
	c.c = conn
	c.addr = conn.RemoteAddr().String()
	return c
}

func tls_new_conn(node *Node, conn net.Conn, is_server bool, send_msg_count, recv_msg_count, disc_reason_count int32) *TcpConn {
	c := &TcpConn{}
	common_conn(c, node, is_server, send_msg_count, recv_msg_count, disc_reason_count)
	c.tls_c = conn
	c.addr = conn.RemoteAddr().String()
	return c
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
	handler_map        map[uint16]handler_info
	ack_handler        AckHandler
	conn_map           map[*TcpConn]int32
	conn_map_lock      *sync.RWMutex
	shutdown_lock      *sync.Mutex
	initialized        bool

	// tls
	tls_addr     net.Addr
	tls_listener net.Listener
	tls_config   *tls.Config

	recv_buff_len     int
	send_buff_len     int
	recv_msg_count    int32
	send_msg_count    int32
	disc_reason_count int32

	I interface{}
}

func NewNode(cb ICallback, recv_timeout time.Duration, send_timeout time.Duration, update_interval_ms int32, recv_buff_len int, send_buff_len int, recv_msg_count, send_msg_count, disc_reason_count int32) (new_node *Node) {
	new_node = &Node{}
	new_node.callback = cb
	new_node.recv_timeout = recv_timeout
	new_node.send_timeout = send_timeout
	new_node.update_interval_ms = update_interval_ms
	new_node.handler_map = make(map[uint16]handler_info)
	new_node.conn_map = make(map[*TcpConn]int32)
	new_node.conn_map_lock = &sync.RWMutex{}
	new_node.shutdown_lock = &sync.Mutex{}
	new_node.recv_buff_len = recv_buff_len
	new_node.send_buff_len = send_buff_len
	new_node.recv_msg_count = recv_msg_count
	new_node.send_msg_count = send_msg_count
	new_node.disc_reason_count = disc_reason_count
	new_node.initialized = true
	return
}

func (this *Node) UseTls(cert_file string, key_file string) (err error) {
	/*
		cert, err := tls.LoadX509KeyPair(cert_file, key_file)
		if err != nil {
			return
		}

		this.tls_config = &tls.Config{
			//RootCAs: pool,
			//InsecureSkipVerify: true,
			//ClientAuth: tls.RequireAndVerifyClientCert,
			Certificates:             []tls.Certificate{cert},
			CipherSuites:             []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA},
			PreferServerCipherSuites: true,
		}

		now := time.Now()
		this.tls_config.Time = func() time.Time { return now }
		this.tls_config.Rand = rand.Reader
	*/
	return nil
}

func (this *Node) UseTlsClient() {
	this.tls_config = &tls.Config{
		InsecureSkipVerify: true,
	}

	now := time.Now()
	this.tls_config.Time = func() time.Time { return now }
	this.tls_config.Rand = rand.Reader
}

func (this *Node) add_conn(c *TcpConn) {
	this.conn_map_lock.Lock()
	defer this.conn_map_lock.Unlock()

	this.conn_map[c] = 0
}

func (this *Node) remove_conn(c *TcpConn) {
	this.conn_map_lock.Lock()
	defer this.conn_map_lock.Unlock()

	delete(this.conn_map, c)
}

func (this *Node) ConnCount() (n int32) {
	this.conn_map_lock.RLock()
	defer this.conn_map_lock.RUnlock()

	return int32(len(this.conn_map))
}

func (this *Node) get_all_conn() (conn_map map[*TcpConn]int32) {
	this.conn_map_lock.RLock()
	defer this.conn_map_lock.RUnlock()

	conn_map = make(map[*TcpConn]int32)
	for i, v := range this.conn_map {
		conn_map[i] = v
	}

	return
}

func (this *Node) SetHandler(type_id uint16, typ reflect.Type, h Handler) {
	_, has := this.handler_map[type_id]
	if has {
		log.Warn("[%v]???????????????????????????,???????????? %v %v", this.addr, type_id, typ)
	}
	this.handler_map[type_id] = handler_info{typ, h}
}

func (this *Node) GetHandler(type_id uint16) (h Handler) {
	hi, has := this.handler_map[type_id]
	if !has {
		return nil
	}

	return hi.h
}

func (this *Node) Listen(server_addr string, max_conn int32) (err error) {
	addr, err := net.ResolveTCPAddr("tcp", server_addr)
	if err != nil {
		return
	}

	this.addr = addr
	this.max_conn = max_conn

	if this.tls_config == nil {
		var l *net.TCPListener
		l, err = net.ListenTCP(this.addr.Network(), this.addr)
		if err != nil {
			return
		}
		this.listener = l
	} else {
		var tls_l net.Listener
		tls_l, err = tls.Listen("tcp", server_addr, this.tls_config)
		if err != nil {
			return
		}
		this.tls_listener = tls_l
	}

	var delay time.Duration
	var conn *net.TCPConn
	var tls_conn net.Conn
	log.Trace("[%v]??????????????????", this.addr)
	for !this.quit {
		log.Trace("[%v]??????????????????", this.addr)
		if this.tls_config == nil {
			conn, err = this.listener.AcceptTCP()
		} else {
			tls_conn, err = this.tls_listener.Accept()
		}
		if err != nil {
			if this.quit {
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

		if this.quit {
			break
		}

		if conn != nil {
			log.Trace("[%v]?????????????????????", this.addr)
			if this.max_conn > 0 {
				if this.ConnCount() > this.max_conn {
					log.Trace("[%v]????????????????????????", this.addr)
					conn.Close()
					continue
				}
			}
		} else if tls_conn != nil {
			log.Trace("[%v]????????????????????? %v", this.addr, tls_conn.RemoteAddr())
			if this.max_conn > 0 {
				if this.ConnCount() > this.max_conn {
					log.Trace("[%v]???????????????????????? %v", this.addr, this.max_conn)
					tls_conn.Close()
					continue
				}
			}
		}

		var c *TcpConn
		if conn != nil {
			conn.SetKeepAlive(true)
			conn.SetKeepAlivePeriod(time.Minute * 2)
			if this.recv_buff_len > 0 {
				conn.SetReadBuffer(this.recv_buff_len)
			}
			if this.send_buff_len > 0 {
				conn.SetWriteBuffer(this.send_buff_len)
			}
			c = new_conn(this, conn, true, this.send_msg_count, this.recv_msg_count, this.disc_reason_count)
		} else if tls_conn != nil {
			c = tls_new_conn(this, tls_conn, true, this.send_msg_count, this.recv_msg_count, this.disc_reason_count)
		}
		this.add_conn(c)

		go c.send_loop()
		go c.recv_loop()
		if this.callback != nil {
			this.callback.OnAccept(c)
		}
		go c.main_loop()
	}

	return nil
}

func (this *Node) Connect(server_addr string, timeout time.Duration) (err error) {
	err = this.ClientConnect(server_addr, timeout)
	if err != nil {
		return
	}
	this.ClientRun()
	return
}

func (this *Node) ClientConnect(server_addr string, timeout time.Duration) (err error) {
	addr, err := net.ResolveTCPAddr("tcp", server_addr)
	if err != nil {
		return
	}
	this.addr = addr
	this.send_timeout = timeout

	var c *TcpConn
	if this.tls_config == nil {
		var conn *net.TCPConn
		conn, err = net.DialTCP(this.addr.Network(), nil, this.addr)
		if err != nil {
			return
		}
		c = new_conn(this, conn, false, this.send_msg_count, this.recv_msg_count, this.disc_reason_count)
	} else {
		var tls_conn net.Conn
		tls_conn, err = tls.Dial("tcp", server_addr, this.tls_config)
		if err != nil {
			return
		}
		c = tls_new_conn(this, tls_conn, false, this.send_msg_count, this.recv_msg_count, this.disc_reason_count)
	}

	c.I = this.I
	this.client = c

	if this.callback != nil {
		this.callback.OnConnect(this.client)
	}

	log.Trace("[%v]????????????", server_addr)
	return
}

func (this *Node) ClientRun() {
	go this.client.send_loop()
	go this.client.recv_loop()
	go this.client.main_loop()
}

func (this *Node) ClientDisconnect() {
	this.client.Close(E_DISCONNECT_REASON_NONE)
}

func (this *Node) GetClient() (c *TcpConn) {
	return this.client
}

func (this *Node) Shutdown() {
	log.Trace("[%v]??????????????????", this.addr)
	if !this.initialized {
		return
	}

	this.shutdown_lock.Lock()
	defer this.shutdown_lock.Unlock()

	if this.quit {
		return
	}
	this.quit = true

	begin := time.Now()

	if this.listener != nil {
		this.listener.Close()
	} else if this.tls_listener != nil {
		this.tls_listener.Close()
	}

	conn_map := this.get_all_conn()
	log.Trace("[%v]?????? %v ?????????????????????", this.addr, len(conn_map))
	for k, _ := range conn_map {
		go k.Close(E_DISCONNECT_REASON_SERVER_SHUTDOWN)
	}

	for {
		conn_count := this.ConnCount()
		if conn_count == 0 {
			break
		}

		time.Sleep(time.Millisecond * 100)
	}

	log.Trace("[%v]???????????????????????? %v ???", this.addr, time.Now().Sub(begin).Seconds())
}

func (this *Node) Broadcast(msgid uint16, msg proto.Message) {
	this.conn_map_lock.Lock()
	defer this.conn_map_lock.Unlock()

	for c, _ := range this.conn_map {
		c.Send(msgid, msg, true)
	}
}
