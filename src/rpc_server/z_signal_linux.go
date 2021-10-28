package main

import (
	"ih_server/libs/log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type CLOSE_FUNC func(info *SignalRegRecod)

type SignalRegRecod struct {
	close_func CLOSE_FUNC
	close_flag bool
}

var signal_mgr SignalMgr

type SignalMgr struct {
	signal_c  chan os.Signal
	close_map map[string]*SignalRegRecod
	b_closing bool
}

func (m *SignalMgr) Init() bool {
	m.signal_c = make(chan os.Signal, 10)
	m.close_map = make(map[string]*SignalRegRecod)
	m.b_closing = false
	signal.Notify(m.signal_c, os.Interrupt, syscall.SIGTERM)

	go m.DoAllCloseFunc()
	return true
}

func (m *SignalMgr) RegCloseFunc(modname string, close_func CLOSE_FUNC) {
	if nil == m.close_map[modname] {
		m.close_map[modname] = &SignalRegRecod{
			close_func: close_func,
			close_flag: false,
		}
	} else {
		m.close_map[modname].close_func = close_func
	}
}

func (m *SignalMgr) DoAllCloseFunc() {
	<-m.signal_c
	{
		m.b_closing = true
		server.Shutdown()
		for _, info := range m.close_map {
			info.close_func(info)
		}
	}
}

func (m *SignalMgr) WaitAllCloseOver() {
	for index, info := range m.close_map {
		if nil == info {
			continue
		}

		for {
			time.Sleep(time.Millisecond * 10)
			if info.close_flag {
				log.Info(index + " ended !")
				break
			}
		}
	}
}

func (m *SignalMgr) Close() {
	signal.Stop(m.signal_c)
}

func (m *SignalMgr) IfClosing() bool {
	return m.b_closing
}
