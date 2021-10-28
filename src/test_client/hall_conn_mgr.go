package main

import (
	"ih_server/libs/log"
	"sync"
)

type HallConnMgr struct {
	acc2hconn      map[string]*HallConnection
	acc_arr        []*HallConnection
	acc2hconn_lock *sync.RWMutex
}

var hall_conn_mgr HallConnMgr

func (mgr *HallConnMgr) Init() bool {
	mgr.acc2hconn = make(map[string]*HallConnection)
	mgr.acc2hconn_lock = &sync.RWMutex{}
	return true
}

func (mgr *HallConnMgr) AddHallConn(conn *HallConnection) {
	if nil == conn {
		log.Error("HallConnMgr AddHallConn param error !")
		return
	}

	mgr.acc2hconn_lock.Lock()
	defer mgr.acc2hconn_lock.Unlock()

	mgr.acc2hconn[conn.acc] = conn
	mgr.acc_arr = append(mgr.acc_arr, conn)
	log.Debug("add new hall connection %v", conn.acc)
}

func (mgr *HallConnMgr) RemoveHallConnByAcc(acc string) {
	mgr.acc2hconn_lock.Lock()
	defer mgr.acc2hconn_lock.Unlock()

	conn := mgr.acc2hconn[acc]
	if conn == nil {
		return
	}
	delete(mgr.acc2hconn, acc)
	if mgr.acc_arr != nil {
		for i := 0; i < len(mgr.acc_arr); i++ {
			if mgr.acc_arr[i] == conn {
				mgr.acc_arr[i] = nil
				break
			}
		}
	}
}

func (mgr *HallConnMgr) GetHallConnByAcc(acc string) *HallConnection {
	mgr.acc2hconn_lock.RLock()
	defer mgr.acc2hconn_lock.RUnlock()

	return mgr.acc2hconn[acc]
}
