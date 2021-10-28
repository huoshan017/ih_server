package main

import (
	"ih_server/libs/log"
	"sync"
	"time"
)

type Mutex struct {
	Mutex       *sync.Mutex
	m_timer     *time.Timer
	TimeoutMSec int32
}

func NewMutex() (m *Mutex) {
	m = &Mutex{}
	m.Mutex = &sync.Mutex{}
	m.TimeoutMSec = 2000

	return m
}

/*
func NewMutexWithTime(timeout_msec int32) (m *Mutex) {
	m = &Mutex{}
	m.Mutex = &sync.Mutex{}
	m.TimeoutMSec = timeout_msec
	return m
}
*/

func (m *Mutex) Lock(name string) {
	m.Mutex.Lock()

	if m.m_timer != nil {
		m.m_timer.Stop()
	}

	m.m_timer = time.AfterFunc(time.Millisecond*time.Duration(m.TimeoutMSec), func() {
		log.Error("RWMutex Lock timeout %v" + name)
	})
}

func (m *Mutex) Unlock() {
	if m.m_timer != nil {
		m.m_timer.Stop()
		m.m_timer = nil
	}
	m.Mutex.Unlock()
}

func (m *Mutex) UnSafeLock(name string) {
	m.Mutex.Lock()
}

func (m *Mutex) UnSafeUnlock() {
	m.Mutex.Unlock()
}

type RWMutex struct {
	RWMutex     *sync.RWMutex
	m_timer     *time.Timer
	TimeoutMSec int32
}

func NewRWMutex() (m *RWMutex) {
	m = &RWMutex{}
	m.RWMutex = &sync.RWMutex{}
	m.TimeoutMSec = 5000

	return m
}

/*
func NewRWMutexWithTime(timeout_msec int32) (m *RWMutex) {
	m = &RWMutex{}
	m.RWMutex = &sync.RWMutex{}
	m.TimeoutMSec = timeout_msec
	return m
}
*/

func (m *RWMutex) Lock(name string) {
	m.RWMutex.Lock()

	if m.m_timer != nil {
		m.m_timer.Stop()
	}
	m.m_timer = time.AfterFunc(time.Millisecond*time.Duration(m.TimeoutMSec), func() {
		log.Error("RWMutex Lock timeout %v" + name)
	})
}

func (m *RWMutex) Unlock() {
	if m.m_timer != nil {
		m.m_timer.Stop()
		m.m_timer = nil
	}
	m.RWMutex.Unlock()
}

func (m *RWMutex) RLock(name string) {
	m.RWMutex.RLock()
	/*
		if m.m_timer != nil {
			m.m_timer.Stop()
		}
		m.m_timer = time.AfterFunc(time.Millisecond*time.Duration(m.TimeoutMSec), func() {
			log.Error("RWMutex RLock timeout %v" + name)
		})
	*/
}

func (m *RWMutex) RUnlock() {
	/*
		if m.m_timer != nil {
			m.m_timer.Stop()
			//m.m_timer = nil
		}
	*/
	m.RWMutex.RUnlock()
}

func (m *RWMutex) UnSafeLock(name string) {
	m.RWMutex.Lock()
}

func (m *RWMutex) UnSafeUnlock() {
	m.RWMutex.Unlock()
}

func (m *RWMutex) UnSafeRLock(name string) {
	m.RWMutex.RLock()
}

func (m *RWMutex) UnSafeRUnlock() {
	m.RWMutex.RUnlock()
}
