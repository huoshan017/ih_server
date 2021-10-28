package main

import (
	"ih_server/libs/log"
	"ih_server/libs/server_conn"
	"ih_server/libs/timer"
	msg_server_message "ih_server/proto/gen_go/server_message"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
)

const (
	HALL_AGENT_DISCONNECT = iota
	HALL_AGENT_CONNECTED  = 1
	HALL_AGENT_CREATED    = 2
)

type HallAgent struct {
	conn             *server_conn.ServerConn // 连接
	state            int32                   // agent状态
	name             string                  // game_server name
	id               int32                   // game_server ID
	max_player_num   int32                   // 最大在线人数
	curr_player_num  int32                   // 当前在线人数
	aids             map[string]int32        // 已有的账号
	aids_lock        *sync.RWMutex
	listen_client_ip string // 监听客户端IP
}

func new_agent(c *server_conn.ServerConn, state int32) (agent *HallAgent) {
	agent = &HallAgent{}
	agent.conn = c
	agent.state = state
	agent.aids = make(map[string]int32)
	agent.aids_lock = &sync.RWMutex{}
	return
}

func (agent *HallAgent) HasAid(aid string) (ok bool) {
	agent.aids_lock.RLock()
	defer agent.aids_lock.RUnlock()

	state, o := agent.aids[aid]
	if !o {
		return
	}
	if state <= 0 {
		return
	}
	ok = true
	return
}

func (agent *HallAgent) AddAid(aid string) (ok bool) {
	agent.aids_lock.Lock()
	defer agent.aids_lock.Unlock()

	_, o := agent.aids[aid]
	if o {
		return
	}
	agent.aids[aid] = 1
	ok = true
	return
}

func (agent *HallAgent) RemoveAid(aid string) (ok bool) {
	agent.aids_lock.Lock()
	defer agent.aids_lock.Unlock()

	_, o := agent.aids[aid]
	if !o {
		return
	}

	delete(agent.aids, aid)
	ok = true
	return
}

func (agent *HallAgent) UpdatePlayersNum(max_num, curr_num int32) {
	agent.aids_lock.Lock()
	defer agent.aids_lock.Unlock()

	agent.max_player_num = max_num
	agent.curr_player_num = curr_num
}

func (agent *HallAgent) GetPlayersNum() (max_num, curr_num int32) {
	agent.aids_lock.RLock()
	defer agent.aids_lock.RUnlock()

	max_num = agent.max_player_num
	curr_num = agent.curr_player_num
	return
}

func (agent *HallAgent) Send(msg_id uint16, msg proto.Message) {
	agent.conn.Send(msg_id, msg, true)
}

func (agent *HallAgent) Close(force bool) {
	agent.aids_lock.Lock()
	defer agent.aids_lock.Unlock()
	if force {
		agent.conn.Close(server_conn.E_DISCONNECT_REASON_FORCE_CLOSED)
	} else {
		agent.conn.Close(server_conn.E_DISCONNECT_REASON_LOGGIN_FAILED)
	}
}

//========================================================================

type HallAgentManager struct {
	net                *server_conn.Node
	id2agents          map[int32]*HallAgent
	conn2agents        map[*server_conn.ServerConn]*HallAgent
	agents_lock        *sync.RWMutex
	inited             bool
	shutdown_lock      *sync.Mutex
	shutdown_completed bool
	ticker             *timer.TickTimer
	listen_err_chan    chan error
}

var hall_agent_manager HallAgentManager

func (mgr *HallAgentManager) Init() (ok bool) {
	mgr.id2agents = make(map[int32]*HallAgent)
	mgr.conn2agents = make(map[*server_conn.ServerConn]*HallAgent)
	mgr.agents_lock = &sync.RWMutex{}
	mgr.net = server_conn.NewNode(mgr, 0, 0, 5000,
		0,
		0,
		2048,
		2048,
		2048)
	mgr.net.SetDesc("HallAgent", "大厅服务器")

	mgr.shutdown_lock = &sync.Mutex{}
	mgr.listen_err_chan = make(chan error)
	mgr.init_message_handle()
	mgr.inited = true
	ok = true
	return
}

func (mgr *HallAgentManager) wait_listen_res() (err error) {
	timeout := make(chan bool, 1)
	go func() {
		time.Sleep(5 * time.Second)
		timeout <- true
	}()

	var o bool
	select {
	case err, o = <-mgr.listen_err_chan:
		{
			if !o {
				log.Trace("wait listen_err_chan failed")
				return
			}
		}
	case <-timeout:
		{
		}
	}

	return
}

func (mgr *HallAgentManager) Start() (err error) {
	log.Event("HallAgentManager已启动", nil, log.Property{Name: "IP", Value: config.ListenGameIP})
	log.Trace("**************************************************")

	go mgr.Run()

	go mgr.Listen()

	err = mgr.wait_listen_res()

	return
}

func (mgr *HallAgentManager) Listen() {
	err := mgr.net.Listen(config.ListenGameIP, config.MaxGameConnections)
	if err != nil {
		mgr.listen_err_chan <- err
		log.Error("启动HallAgentManager失败 %v", err)
	} else {
		close(mgr.listen_err_chan)
	}
}

func (mgr *HallAgentManager) Run() {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
		mgr.shutdown_completed = true
	}()

	mgr.ticker = timer.NewTickTimer(1000)
	mgr.ticker.Start()
	defer mgr.ticker.Stop()

	for d := range mgr.ticker.Chan {
		begin := time.Now()
		mgr.OnTick(d)
		time_cost := time.Since(begin).Seconds()
		if time_cost > 1 {
			log.Trace("耗时 %v", time_cost)
			if time_cost > 30 {
				log.Error("耗时 %v", time_cost)
			}
		}
	}
}

func (mgr *HallAgentManager) OnAccept(c *server_conn.ServerConn) {
	mgr.AddAgent(c, HALL_AGENT_CONNECTED)
	log.Trace("新的HallAgent连接")
}

func (mgr *HallAgentManager) OnConnect(c *server_conn.ServerConn) {
}

func (mgr *HallAgentManager) OnUpdate(c *server_conn.ServerConn, t timer.TickTime) {
}

func (mgr *HallAgentManager) OnDisconnect(c *server_conn.ServerConn, reason server_conn.E_DISCONNECT_REASON) {
	mgr.DisconnectAgent(c, reason)
	log.Trace("断开HallAgent连接")
}

func (mgr *HallAgentManager) OnTick(t timer.TickTime) {
}

func (mgr *HallAgentManager) set_ih(type_id uint16, h server_conn.Handler) {
	mgr.net.SetHandler(type_id, h)
}

func (mgr *HallAgentManager) HasAgent(server_id int32) (ok bool) {
	mgr.agents_lock.RLock()
	defer mgr.agents_lock.RUnlock()
	_, o := mgr.id2agents[server_id]
	if !o {
		return
	}
	ok = true
	return
}

func (mgr *HallAgentManager) GetAgent(c *server_conn.ServerConn) (agent *HallAgent) {
	mgr.agents_lock.RLock()
	defer mgr.agents_lock.RUnlock()
	a, o := mgr.conn2agents[c]
	if !o {
		return
	}
	agent = a
	return
}

func (mgr *HallAgentManager) GetAgentByID(hall_id int32) (agent *HallAgent) {
	mgr.agents_lock.RLock()
	defer mgr.agents_lock.RUnlock()
	a, o := mgr.id2agents[hall_id]
	if !o {
		return
	}
	agent = a
	return
}

func (mgr *HallAgentManager) AddAgent(c *server_conn.ServerConn, state int32) (agent *HallAgent) {
	mgr.agents_lock.Lock()
	defer mgr.agents_lock.Unlock()

	_, o := mgr.conn2agents[c]
	if o {
		return
	}

	agent = new_agent(c, state)
	mgr.conn2agents[c] = agent
	return
}

func (mgr *HallAgentManager) SetAgentByID(id int32, agent *HallAgent) (ok bool) {
	mgr.agents_lock.Lock()
	defer mgr.agents_lock.Unlock()

	agent.id = id

	mgr.id2agents[id] = agent
	ok = true
	return
}

func (mgr *HallAgentManager) RemoveAgent(c *server_conn.ServerConn, lock bool) (ok bool) {
	if lock {
		mgr.agents_lock.Lock()
		defer mgr.agents_lock.Unlock()
	}

	agent, o := mgr.conn2agents[c]
	if !o {
		return
	}

	delete(mgr.conn2agents, c)
	delete(mgr.id2agents, agent.id)

	agent.aids = nil

	ok = true
	return
}

func (mgr *HallAgentManager) DisconnectAgent(c *server_conn.ServerConn, reason server_conn.E_DISCONNECT_REASON) (ok bool) {
	if c == nil {
		return
	}

	ok = mgr.RemoveAgent(c, true)

	res := &msg_server_message.L2HDissconnectNotify{}
	res.Reason = int32(reason)
	c.Send(uint16(msg_server_message.MSGID_L2H_DISCONNECT_NOTIFY), res, true)
	return
}

func (mgr *HallAgentManager) SetMessageHandler(type_id uint16, h server_conn.Handler) {
	mgr.set_ih(type_id, h)
}

func (mgr *HallAgentManager) UpdatePlayersNum(server_id int32, max_num, curr_num int32) {
	mgr.agents_lock.RLock()
	defer mgr.agents_lock.RUnlock()

	agent := mgr.id2agents[server_id]
	if agent == nil {
		return
	}

	agent.UpdatePlayersNum(max_num, curr_num)
}

func (mgr *HallAgentManager) GetPlayersNum(server_id int32) (agent *HallAgent, max_num, curr_num int32) {
	mgr.agents_lock.RLock()
	defer mgr.agents_lock.RUnlock()

	agent = mgr.id2agents[server_id]
	if agent == nil {
		return
	}

	max_num, curr_num = agent.GetPlayersNum()
	return
}

func (mgr *HallAgentManager) GetSuitableHallAgent() *HallAgent {
	mgr.agents_lock.RLock()
	defer mgr.agents_lock.RUnlock()

	for _, agent := range mgr.id2agents {
		if nil != agent {
			return agent
		}
	}

	return nil
}

//====================================================================================================

func (mgr *HallAgentManager) init_message_handle() {
	mgr.net.SetPid2P(hall_agent_msgid2msg)
	mgr.SetMessageHandler(uint16(msg_server_message.MSGID_H2L_HALL_SERVER_REGISTER), H2LHallServerRegisterHandler)
	mgr.SetMessageHandler(uint16(msg_server_message.MSGID_H2L_ACCOUNT_LOGOUT_NOTIFY), H2LAccountLogoutNotifyHandler)
	mgr.SetMessageHandler(uint16(msg_server_message.MSGID_H2L_ACCOUNT_BAN), H2LAccountBanHandler)
}

func hall_agent_msgid2msg(msg_id uint16) proto.Message {
	if msg_id == uint16(msg_server_message.MSGID_H2L_HALL_SERVER_REGISTER) {
		return &msg_server_message.H2LHallServerRegister{}
	} else if msg_id == uint16(msg_server_message.MSGID_H2L_ACCOUNT_LOGOUT_NOTIFY) {
		return &msg_server_message.H2LAccountLogoutNotify{}
	} else if msg_id == uint16(msg_server_message.MSGID_H2L_ACCOUNT_BAN) {
		return &msg_server_message.H2LAccountBan{}
	} else {
		log.Error("Cant found proto message by msg_id[%v]", msg_id)
	}
	return nil
}

func H2LHallServerRegisterHandler(conn *server_conn.ServerConn, m proto.Message) {
	req := m.(*msg_server_message.H2LHallServerRegister)
	if nil == req {
		log.Error("H2LHallServerRegisterHandler param error !")
		return
	}

	server_id := req.GetServerId()
	server_name := req.GetServerName()

	a := hall_agent_manager.GetAgent(conn)
	if a == nil {
		log.Error("Agent[%v] not found", conn)
		return
	}

	if a.id == server_id /*hall_agent_manager.HasAgent(server_id)*/ {
		hall_agent_manager.DisconnectAgent(a.conn, server_conn.E_DISCONNECT_REASON_FORCE_CLOSED)
		log.Error("大厅服务器[%v]已有，不能有重复的ID", server_id)
		return
	}

	a.id = server_id
	a.name = server_name
	a.state = HALL_AGENT_CONNECTED
	a.listen_client_ip = req.GetListenClientIP()

	hall_agent_manager.SetAgentByID(server_id, a)

	log.Trace("大厅服务器[%d %s %s]已连接", server_id, server_name, a.listen_client_ip)
}

func H2LAccountLogoutNotifyHandler(conn *server_conn.ServerConn, m proto.Message) {
	req := m.(*msg_server_message.H2LAccountLogoutNotify)
	if req == nil {
		log.Error("H2LAccountLogoutNotifyHandler param invalid")
		return
	}

	account_logout(req.GetAccount())

	log.Trace("Account %v log out notify", req.GetAccount())
}

func H2LAccountBanHandler(conn *server_conn.ServerConn, m proto.Message) {
	req := m.(*msg_server_message.H2LAccountBan)
	if req == nil {
		log.Error("H2LAccountBanHandler msg invalid")
		return
	}

	uid := req.GetUniqueId()
	ban := req.GetBanOrFree()
	row := dbc.BanPlayers.GetRow(uid)
	if ban > 0 {
		if row == nil {
			row = dbc.BanPlayers.AddRow(uid)
		}
		row.SetAccount(req.GetAccount())
		row.SetPlayerId(req.GetPlayerId())
		now_time := time.Now()
		row.SetStartTime(int32(now_time.Unix()))
		row.SetStartTimeStr(now_time.Format("2006-01-02 15:04:05"))
	} else {
		if row != nil {
			row.SetStartTime(0)
			row.SetStartTimeStr("")
		}
	}

	log.Trace("Unique id %v ban %v", uid, ban)
}
