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

func (s *SignalMgr) Init() bool {
	s.signal_c = make(chan os.Signal, 10)
	s.close_map = make(map[string]*SignalRegRecod)
	s.b_closing = false
	signal.Notify(s.signal_c, os.Interrupt, syscall.SIGTERM)

	go s.DoAllCloseFunc()
	return true
}

func (s *SignalMgr) RegCloseFunc(modname string, close_func CLOSE_FUNC) {
	if nil == s.close_map[modname] {
		s.close_map[modname] = &SignalRegRecod{
			close_func: close_func,
			close_flag: false,
		}
	} else {
		s.close_map[modname].close_func = close_func
	}
}

func (s *SignalMgr) DoAllCloseFunc() {
	<-s.signal_c
	s.b_closing = true
	hall_server.Shutdown()
	for _, info := range s.close_map {
		info.close_func(info)
	}
}

func (s *SignalMgr) WaitAllCloseOver() {
	for index, info := range s.close_map {
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

func (s *SignalMgr) Close() {
	signal.Stop(s.signal_c)
}

func (s *SignalMgr) IfClosing() bool {
	return s.b_closing
}
