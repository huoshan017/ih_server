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
	CENTER_CONN_STATE_DISCONNECT  = 0
	CENTER_CONN_STATE_CONNECTED   = 1
	CENTER_CONN_STATE_FORCE_CLOSE = 2
)

type CenterConnection struct {
	client_node    *server_conn.Node
	state          int32
	last_conn_time int32
}

var center_conn CenterConnection

func (conn *CenterConnection) Init() {
	conn.client_node = server_conn.NewNode(conn, 0, 0, 100, 0, 0, 0, 0, 0)
	conn.client_node.SetDesc("中心服务器", "")

	conn.state = CENTER_CONN_STATE_DISCONNECT
	//conn.RegisterMsgHandler()
}

func (conn *CenterConnection) Start() {
	if conn.Connect(CENTER_CONN_STATE_DISCONNECT) {
		log.Event("连接CenterServer成功", nil, log.Property{Name: "IP", Value: config.CenterServerIP})
	}
	for {
		state := atomic.LoadInt32(&conn.state)
		if state == CENTER_CONN_STATE_CONNECTED {
			time.Sleep(time.Second * 2)
			continue
		}

		if state == CENTER_CONN_STATE_FORCE_CLOSE {
			conn.client_node.ClientDisconnect()
			log.Event("与CenterServer的连接被强制关闭", nil)
			break
		}
		if conn.Connect(state) {
			log.Event("连接CenterServer成功", nil, log.Property{Name: "IP", Value: config.CenterServerIP})
		}
	}
}

func (conn *CenterConnection) Connect(state int32) (ok bool) {
	if CENTER_CONN_STATE_DISCONNECT == state {
		var err error
		for CENTER_CONN_STATE_FORCE_CLOSE != conn.state {
			log.Trace("连接CenterServer %v", config.CenterServerIP)
			err = conn.client_node.ClientConnect(config.CenterServerIP, time.Second*10)
			if nil == err {
				break
			}

			// 每隔30秒输出一次连接信息
			now := time.Now().Unix()
			if int32(now)-conn.last_conn_time >= 30 {
				log.Trace("CenterServer连接中...")
				conn.last_conn_time = int32(now)
			}
			time.Sleep(time.Second * 5)
		}
	}

	if CENTER_CONN_STATE_FORCE_CLOSE != conn.state && atomic.CompareAndSwapInt32(&conn.state, state, CENTER_CONN_STATE_CONNECTED) {
		go conn.client_node.ClientRun()
		ok = true
	}
	return
}

func (conn *CenterConnection) OnAccept(c *server_conn.ServerConn) {
	log.Error("Impossible accept")
}

func (conn *CenterConnection) OnConnect(c *server_conn.ServerConn) {
	if CENTER_CONN_STATE_FORCE_CLOSE != conn.state {
		log.Trace("LoginServer[%v][%v] on CenterServer connect", config.ServerId, config.ServerName)
		notify := &msg_server_message.L2CLoginServerRegister{}
		notify.ServerId = config.ServerId
		notify.ServerName = config.ServerName
		notify.ListenGameIP = config.ListenGameIP
		c.Send(uint16(msg_server_message.MSGID_L2C_LOGIN_SERVER_REGISTER), notify, true)
	} else {
		log.Trace("LoginServer[%v][%v] force closed on CenterServer connect", config.ServerId, config.ServerName)
	}
}

func (conn *CenterConnection) OnUpdate(c *server_conn.ServerConn, t timer.TickTime) {

}

func (conn *CenterConnection) OnDisconnect(c *server_conn.ServerConn, reason server_conn.E_DISCONNECT_REASON) {
	if reason == server_conn.E_DISCONNECT_REASON_FORCE_CLOSED {
		conn.state = CENTER_CONN_STATE_FORCE_CLOSE
	} else {
		conn.state = CENTER_CONN_STATE_DISCONNECT
	}
	log.Event("与CenterServer连接断开", nil)
}

func (conn *CenterConnection) ShutDown() {
	conn.state = CENTER_CONN_STATE_FORCE_CLOSE
	if nil != conn.client_node {
		conn.client_node.Shutdown()
	}
}

func (conn *CenterConnection) SetMessageHandler(type_id uint16, h server_conn.Handler) {
	conn.client_node.SetHandler(type_id, h)
}

func (conn *CenterConnection) Send(msg_id uint16, msg proto.Message) {
	if CENTER_CONN_STATE_CONNECTED != conn.state {
		log.Info("CenterServer未连接!!!")
		return
	}

	if nil == conn.client_node {
		return
	}

	conn.client_node.GetClient().Send(msg_id, msg, false)
}

//========================================================================

func (conn *CenterConnection) RegisterMsgHandler() {
	conn.client_node.SetPid2P(center_conn_msgid2msg)
	conn.SetMessageHandler(uint16(msg_server_message.MSGID_C2L_PLAYER_ACC_INFO), C2LPlayerAccInfoHandler)
}

func center_conn_msgid2msg(msg_id uint16) proto.Message {
	if msg_id == uint16(msg_server_message.MSGID_C2L_PLAYER_ACC_INFO) {
		return &msg_server_message.C2LPlayerAccInfo{}
	} else {
		log.Error("Cant get proto message by msg_id[%v]", msg_id)
	}
	return nil
}

func C2LPlayerAccInfoHandler(conn *server_conn.ServerConn, msg proto.Message) {
	res := msg.(*msg_server_message.C2LPlayerAccInfo)
	if nil == conn || nil == res {
		log.Error("C2LPlayerAccInfoHandler param error !")
		return
	}

	hallid := uint32(res.GetHallId())
	hall_agent := hall_agent_manager.GetAgentByID(hallid)
	if nil == hall_agent {
		log.Error("C2LPlayerAccInfoHandler can not find hall(%d)", hallid)
		return
	}

	acc := res.GetAccount()
	if acc == "" {
		log.Error("C2LPlayerAccInfoHandler acc empty")
		return
	}

	c_wait := server.pop_c_wait_by_acc(acc)
	if nil == c_wait {
		log.Error("C2LPlayerAccInfoHandler failed to get c_wait by acc(%s) !", acc)
		return
	}

	go send_res_to_wait(res, c_wait)
}

func send_res_to_wait(res *msg_server_message.C2LPlayerAccInfo, c_wait *WaitCenterInfo) {
	c_wait.res_chan <- res
	log.Trace("C2MAccountInfoResponseHandler %v", res)
}
