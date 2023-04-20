package main

import (
	"errors"
	"ih_server/libs/log"
	"ih_server/libs/rpc"
	"ih_server/libs/timer"
	"ih_server/src/table_config"
	"sync"
	"time"
)

type RpcServer struct {
	quit                  bool
	shutdown_lock         *sync.Mutex
	shutdown_completed    bool
	ticker                *timer.TickTimer
	initialized           bool
	rpc_service           *rpc.Service              // rpc服务
	hall_rpc_clients      map[uint32]*HallRpcClient // 连接HallServer的rpc客户端(key: HallId, value: *rpc.Client)
	hall_rpc_clients_lock *sync.RWMutex
}

var server RpcServer
var global_config table_config.GlobalConfig

func (server *RpcServer) Init() (err error) {
	if server.initialized {
		return
	}

	server.shutdown_lock = &sync.Mutex{}

	if !server.OnInit() {
		return errors.New("RpcServer OnInit Failed")
	}
	server.initialized = true

	return
}

func (server *RpcServer) load_config() bool {
	/*if !shop_mgr.Init() {
		return false
	}*/
	return global_config.Init("global.json")
}

func (server *RpcServer) OnInit() bool {
	if !server.load_config() {
		log.Error("load config failed")
		return false
	}

	/*if !hall_group_mgr.Init() {
		log.Error("hall group manager init failed")
		return false
	}*/

	if !server.init_proc_service() {
		log.Error("init rpc service failed")
		return false
	}
	if !server.init_hall_clients() {
		log.Error("init rpc clients failed")
		return false
	}
	if !global_data.Init() {
		log.Error("init global data failed")
		return false
	}

	return true
}

func (server *RpcServer) Start() {
	if !server.initialized {
		return
	}

	go server_list.Run()

	server.Run()
}

func (server *RpcServer) Run() {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}

		server.shutdown_completed = true
	}()

	server.ticker = timer.NewTickTimer(1000)
	server.ticker.Start()
	defer server.ticker.Stop()

	// rpc服务
	go server.rpc_service.Serve()
	// redis
	go global_data.RunRedis()

	for range server.ticker.Chan {
		begin := time.Now()
		time_cost := time.Since(begin).Seconds()
		if time_cost > 1 {
			log.Trace("耗时 %v", time_cost)
			if time_cost > 30 {
				log.Error("耗时 %v", time_cost)
			}
		}
	}
}

func (server *RpcServer) Shutdown() {
	if !server.initialized {
		return
	}

	server.shutdown_lock.Lock()
	defer server.shutdown_lock.Unlock()

	server.uninit_proc_service()
	server.uninit_hall_clients()
	global_data.Close()

	if server.quit {
		return
	}
	server.quit = true

	log.Trace("关闭游戏主循环")

	begin := time.Now()

	if server.ticker != nil {
		server.ticker.Stop()
	}

	for {
		if server.shutdown_completed {
			break
		}

		time.Sleep(time.Millisecond * 100)
	}

	log.Trace("关闭游戏主循环耗时 %v 秒", time.Since(begin).Seconds())
}

func (server *RpcServer) init_hall_clients() bool {
	if server.hall_rpc_clients == nil {
		server.hall_rpc_clients = make(map[uint32]*HallRpcClient)
	}
	if server.hall_rpc_clients_lock == nil {
		server.hall_rpc_clients_lock = &sync.RWMutex{}
	}
	return true
}

func (server *RpcServer) uninit_hall_clients() {
	if server.hall_rpc_clients != nil {
		for _, c := range server.hall_rpc_clients {
			if c.rpc_client != nil {
				c.rpc_client.Close()
				c.rpc_client = nil
			}
		}
	}
	if server.hall_rpc_clients_lock != nil {
		server.hall_rpc_clients_lock = nil
	}
}

func (server *RpcServer) connect_hall(addr string, server_id uint32) bool {
	server.hall_rpc_clients_lock.Lock()
	defer server.hall_rpc_clients_lock.Unlock()

	for _, c := range server.hall_rpc_clients {
		if c.server_ip == addr {
			c.rpc_client.Close()
			log.Info("断掉旧的HallServer[%v]的连接", c.server_ip)
			break
		}
	}

	rc := &rpc.Client{}
	if !rc.Dial(addr) {
		log.Error("到HallServer[%v]的rpc连接失败", addr)
		return false
	}

	hr := &HallRpcClient{}
	hr.server_ip = addr
	hr.rpc_client = rc
	hr.server_id = server_id

	server.hall_rpc_clients[server_id] = hr

	log.Info("到HallServer[%v]的rpc连接成功", addr)
	return true
}

/*
func (server *RpcServer) check_connect() {
	var args = rpc_proto.H2R_Ping{}
	var result = rpc_proto.H2R_Pong{}

	var to_del_ids map[int32]int32

	server.hall_rpc_clients_lock.RLock()
	for id, c := range server.hall_rpc_clients {
		var del bool
		if c == nil {
			del = true
		} else if c.rpc_client == nil {
			del = true
		} else {
			if c.rpc_client.Call("H2R_PingProc.Do", args, &result) != nil {
				del = true
			}
		}
		if del {
			if to_del_ids == nil {
				to_del_ids = make(map[int32]int32)
			}
			to_del_ids[id] = id
		}
	}
	server.hall_rpc_clients_lock.RUnlock()

	if to_del_ids != nil {
		server.hall_rpc_clients_lock.Lock()
		for id, _ := range to_del_ids {
			delete(server.hall_rpc_clients, id)
		}
		server.hall_rpc_clients_lock.Unlock()
	}
}
*/

func (server *RpcServer) get_rpc_client(server_id uint32) *HallRpcClient {
	server.hall_rpc_clients_lock.RLock()
	defer server.hall_rpc_clients_lock.RUnlock()
	return server.hall_rpc_clients[server_id]
}

func (server *RpcServer) get_rpc_client_list() (rpc_clients []*rpc.Client) {
	server.hall_rpc_clients_lock.RLock()
	defer server.hall_rpc_clients_lock.RUnlock()

	for _, r := range server.hall_rpc_clients {
		rpc_clients = append(rpc_clients, r.rpc_client)
	}
	return
}
