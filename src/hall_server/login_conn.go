package main

import (
	"ih_server/libs/log"
	"ih_server/libs/server_conn"
	"ih_server/libs/timer"
	msg_server_message "ih_server/proto/gen_go/server_message"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/proto"
)

const (
	LOGIN_CONN_STATE_DISCONNECT  = 0
	LOGIN_CONN_STATE_CONNECTED   = 1
	LOGIN_CONN_STATE_FORCE_CLOSE = 2
)

type LoginConnection struct {
	serverid        int32
	servername      string
	listen_match_ip string
	client_node     *server_conn.Node
	state           int32

	last_conn_time int32
}

func new_login_conn(serverid int32, servername, ip string) *LoginConnection {
	if ip == "" {
		log.Error("new_login_conn param error !")
		return nil
	}

	ret_login_conn := &LoginConnection{
		serverid:        serverid,
		servername:      servername,
		listen_match_ip: ip}

	ret_login_conn.Init()
	go ret_login_conn.Start()

	return ret_login_conn
}

func (c *LoginConnection) Init() {
	c.client_node = server_conn.NewNode(c, 0, 0, 100, 0, 0, 0, 0, 0)
	c.client_node.SetDesc("登录服务器", "")
	c.state = LOGIN_CONN_STATE_DISCONNECT
	c.RegisterMsgHandler()
}

func (c *LoginConnection) Start() {
	if c.Connect(LOGIN_CONN_STATE_DISCONNECT) {
		log.Event("连接LoginServer成功", nil, log.Property{Name: "IP", Value: c.listen_match_ip})
	}
	for {
		state := atomic.LoadInt32(&c.state)
		if state == LOGIN_CONN_STATE_CONNECTED {
			time.Sleep(time.Second * 2)
			continue
		}

		if state == LOGIN_CONN_STATE_FORCE_CLOSE {
			c.client_node.ClientDisconnect()
			log.Event("与login的连接被强制关闭", nil)
			break
		}
		if c.Connect(state) {
			log.Event("连接loginServer成功", nil, log.Property{Name: "IP", Value: c.listen_match_ip})
		}
	}
}

func (c *LoginConnection) Connect(state int32) (ok bool) {
	if LOGIN_CONN_STATE_DISCONNECT == state {
		var err error
		for {
			log.Trace("连接loginServer %v", c.listen_match_ip)
			err = c.client_node.ClientConnect(c.listen_match_ip, time.Second*10)
			if nil == err {
				break
			}

			// 每隔30秒输出一次连接信息
			now := time.Now().Unix()
			if int32(now)-c.last_conn_time >= 30 {
				log.Trace("LoginServer连接中...")
				c.last_conn_time = int32(now)
			}
			time.Sleep(time.Second * 5)
		}
	}

	if atomic.CompareAndSwapInt32(&c.state, state, LOGIN_CONN_STATE_CONNECTED) {
		go c.client_node.ClientRun()
		ok = true
	}
	return
}

func (c *LoginConnection) OnAccept(s *server_conn.ServerConn) {
	log.Error("Impossible accept")
}

func (c *LoginConnection) OnConnect(s *server_conn.ServerConn) {
	log.Trace("Server[%v][%v] on LoginServer connect", config.ServerId, config.ServerName)
	s.T = c.serverid
	notify := &msg_server_message.H2LHallServerRegister{}
	notify.ServerId = config.ServerId
	notify.ServerName = config.ServerName
	notify.ListenClientIP = config.ListenClientOutIP
	s.Send(uint16(msg_server_message.MSGID_H2L_HALL_SERVER_REGISTER), notify, true)
}

func (c *LoginConnection) OnUpdate(s *server_conn.ServerConn, t timer.TickTime) {
}

func (c *LoginConnection) OnDisconnect(s *server_conn.ServerConn, reason server_conn.E_DISCONNECT_REASON) {
	/*
		if reason == server_conn.E_DISCONNECT_REASON_FORCE_CLOSED {
			c.state = LOGIN_CONN_STATE_FORCE_CLOSE
		} else {
			c.state = LOGIN_CONN_STATE_DISCONNECT
		}
	*/
	c.state = LOGIN_CONN_STATE_FORCE_CLOSE
	log.Event("与LoginServer连接断开", nil)
	if s.T > 0 {
		login_conn_mgr.RemoveLogin(s.T)
	}
}

func (c *LoginConnection) ForceClose(bimmidate bool) {
	c.state = LOGIN_CONN_STATE_FORCE_CLOSE
	if bimmidate {
		c.client_node.ClientDisconnect()
	}
}

func (c *LoginConnection) Send(msg_id uint16, msg proto.Message) {
	if LOGIN_CONN_STATE_CONNECTED != c.state {
		log.Info("与登录服务器未连接，不能发送消息!!!")
		return
	}
	if nil == c.client_node {
		return
	}
	c.client_node.GetClient().Send(msg_id, msg, false)
}

//=============================================================================

func (c *LoginConnection) RegisterMsgHandler() {
	c.client_node.SetPid2P(login_conn_msgid2msg)
	c.SetMessageHandler(uint16(msg_server_message.MSGID_L2H_SYNC_ACCOUNT_TOKEN), L2HSyncAccountTokenHandler)
	c.SetMessageHandler(uint16(msg_server_message.MSGID_L2H_DISCONNECT_NOTIFY), L2HDissconnectNotifyHandler)
	c.SetMessageHandler(uint16(msg_server_message.MSGID_L2H_BIND_NEW_ACCOUNT_REQUEST), L2HBindNewAccountHandler)
}

func (c *LoginConnection) SetMessageHandler(type_id uint16, h server_conn.Handler) {
	c.client_node.SetHandler(type_id, h)
}

func login_conn_msgid2msg(msg_id uint16) proto.Message {
	if msg_id == uint16(msg_server_message.MSGID_L2H_SYNC_ACCOUNT_TOKEN) {
		return &msg_server_message.L2HSyncAccountToken{}
	} else if msg_id == uint16(msg_server_message.MSGID_L2H_DISCONNECT_NOTIFY) {
		return &msg_server_message.L2HDissconnectNotify{}
	} else if msg_id == uint16(msg_server_message.MSGID_L2H_BIND_NEW_ACCOUNT_REQUEST) {
		return &msg_server_message.L2HBindNewAccountRequest{}
	} else {
		log.Error("Cant found proto message by msg_id[%v]", msg_id)
	}
	return nil
}

func L2HSyncAccountTokenHandler(conn *server_conn.ServerConn, msg proto.Message) {
	req := msg.(*msg_server_message.L2HSyncAccountToken)
	if nil == req {
		log.Error("ID_L2HSyncAccountTokenHandler param error !")
		return
	}

	login_token_mgr.AddToUid2Token(req.GetUniqueId(), req.GetAccount(), req.GetToken(), int32(req.GetPlayerId()), conn)
	log.Info("ID_L2HSyncAccountTokenHandler UniqueId[%v] Account[%v] Token[%v] PlayerId[%v]", req.GetUniqueId(), req.GetAccount(), req.GetToken(), req.GetPlayerId())
}

func L2HDissconnectNotifyHandler(_ *server_conn.ServerConn, _ proto.Message) {
	log.Info("L2HDissconnectNotifyHandler param error !")
}

func L2HBindNewAccountHandler(_ *server_conn.ServerConn, msg proto.Message) {
	req := msg.(*msg_server_message.L2HBindNewAccountRequest)
	if req == nil {
		log.Error("L2HBindNewAccountHandler msg param invalid")
		return
	}

	p := player_mgr.GetPlayerByUid(req.GetUniqueId())
	if p == nil {
		log.Error("Cant found account %v to bind new account %v", req.GetAccount(), req.GetNewAccount())
		return
	}

	row := dbc.Players.GetRow(p.Id)
	if row == nil {
		log.Error("Cant found db row with player account[%v] and id[%v]", req.GetAccount(), p.Id)
		return
	}

	p.Account = req.GetNewAccount() // 新账号
	row.SetAccount(req.GetNewAccount())

	login_token_mgr.BindNewAccount(req.GetUniqueId(), req.GetAccount(), req.GetNewAccount())

	log.Debug("Account %v bind new account %v", req.GetAccount(), req.GetNewAccount())
}
