package main

import (
	"ih_server/libs/log"
	msg_server_message "ih_server/proto/gen_go/server_message"
	"sync"

	"github.com/golang/protobuf/proto"
)

type LoginConnectionMgr struct {
	id2loginconn      map[int32]*LoginConnection
	id2loginconn_lock *sync.RWMutex
}

var login_conn_mgr LoginConnectionMgr

func (m *LoginConnectionMgr) Init() bool {
	m.id2loginconn = make(map[int32]*LoginConnection)
	m.id2loginconn_lock = &sync.RWMutex{}
	return true
}

func (m *LoginConnectionMgr) DisconnectAll() {
	log.Info("LoginConnectionMgr DisconnectAll")

	cur_conns := m.reset()
	for _, conn := range cur_conns {
		if nil != conn {
			conn.ForceClose(true)
		}
	}
}

func (m *LoginConnectionMgr) reset() []*LoginConnection {
	log.Info("LoginConnectionMgr reset")

	m.id2loginconn_lock.Lock()
	defer m.id2loginconn_lock.Unlock()
	ret_conns := make([]*LoginConnection, len(m.id2loginconn))
	for _, conn := range m.id2loginconn {
		if nil == conn {
			continue
		}

		ret_conns = append(ret_conns, conn)
	}

	m.id2loginconn = make(map[int32]*LoginConnection)
	return ret_conns
}

func (m *LoginConnectionMgr) GetLoginById(id int32) *LoginConnection {
	m.id2loginconn_lock.RLock()
	defer m.id2loginconn_lock.RUnlock()
	return m.id2loginconn[id]
}

func (m *LoginConnectionMgr) AddLogin(msg_login *msg_server_message.LoginServerInfo) {
	if nil == msg_login {
		log.Error("LoginConnectionMgr AddLogin msg_login empty")
		return
	}

	serverid := msg_login.GetServerId()

	m.id2loginconn_lock.Lock()
	defer m.id2loginconn_lock.Unlock()

	old_conn := m.id2loginconn[serverid]
	if nil != old_conn {
		//old_conn.Connect(LOGIN_CONN_STATE_FORCE_CLOSE)
		delete(m.id2loginconn, serverid)
	}

	log.Info("LoginConnectionMgr AddLogin", serverid, msg_login.GetServerName())
	new_conn := new_login_conn(serverid, msg_login.GetServerName(), msg_login.GetListenGameIP())
	if nil == new_conn {
		log.Info("LoginConnectionMgr AddLogin new login conn failed", serverid, msg_login.GetServerName(), msg_login.GetListenGameIP())
		return
	}
	m.id2loginconn[serverid] = new_conn
}

func (m *LoginConnectionMgr) RemoveLogin(id int32) {
	m.id2loginconn_lock.Lock()
	defer m.id2loginconn_lock.Unlock()
	if nil != m.id2loginconn[id] {
		delete(m.id2loginconn, id)
	}
}

func (m *LoginConnectionMgr) Send(msg_id uint16, msg proto.Message) {
	m.id2loginconn_lock.Lock()
	defer m.id2loginconn_lock.Unlock()

	for _, c := range m.id2loginconn {
		if c != nil {
			c.Send(msg_id, msg)
		}
	}
}
