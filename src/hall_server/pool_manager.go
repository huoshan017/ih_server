package main

import (
	"ih_server/proto/gen_go/client_message"
	"sync"
)

// 战报
type BattleReportPool struct {
	pool *sync.Pool
}

func (p *BattleReportPool) Init() {
	p.pool = &sync.Pool{
		New: func() interface{} {
			m := &msg_client_message.BattleReportItem{}
			return m
		},
	}
}

func (p*BattleReportPool) Get() *msg_client_message.BattleReportItem {
	return p.pool.Get().(*msg_client_message.BattleReportItem)
}

func (p*BattleReportPool) Put(m *msg_client_message.BattleReportItem) {
	p.pool.Put(m)
}

// 阵型成员
type TeamMemberPool struct {
	pool   *sync.Pool
	locker *sync.RWMutex
}

func (p *TeamMemberPool) Init() {
	p.pool = &sync.Pool{
		New: func() interface{} {
			m := &TeamMember{}
			return m
		},
	}
	p.locker = &sync.RWMutex{}
}

func (p *TeamMemberPool) Get() *TeamMember {
	return p.pool.Get().(*TeamMember)
}

func (p *TeamMemberPool) Put(m *TeamMember) {
	p.pool.Put(m)
}

// BUFF
type BuffPool struct {
	pool *sync.Pool
}

func (p *BuffPool) Init() {
	p.pool = &sync.Pool{
		New: func() interface{} {
			return &Buff{}
		},
	}
}

func (p *BuffPool) Get() *Buff {
	return p.pool.Get().(*Buff)
}

func (p *BuffPool) Put(b *Buff) {
	p.pool.Put(b)
}

// MemberPassiveTriggerData
type MemberPassiveTriggerDataPool struct {
	pool *sync.Pool
}

func (p *MemberPassiveTriggerDataPool) Init() {
	p.pool = &sync.Pool{
		New: func() interface{} {
			return &PassiveTriggerData{}
		},
	}
}

func (p *MemberPassiveTriggerDataPool) Get() *PassiveTriggerData {
	return p.pool.Get().(*PassiveTriggerData)
}

func (p *MemberPassiveTriggerDataPool) Put(d *PassiveTriggerData) {
	p.pool.Put(d)
}

// MsgBattleMemberItemPool
type MsgBattleMemberItemPool struct {
	pool *sync.Pool
}

func (p *MsgBattleMemberItemPool) Init() {
	p.pool = &sync.Pool{
		New: func() interface{} {
			return &msg_client_message.BattleMemberItem{}
		},
	}
}

func (p *MsgBattleMemberItemPool) Get() *msg_client_message.BattleMemberItem {
	return p.pool.Get().(*msg_client_message.BattleMemberItem)
}

func (p *MsgBattleMemberItemPool) Put(item *msg_client_message.BattleMemberItem) {
	p.pool.Put(item)
}

// MsgBattleFighterPool
type MsgBattleFighterPool struct {
	pool *sync.Pool
}

func (p *MsgBattleFighterPool) Init() {
	p.pool = &sync.Pool{
		New: func() interface{} {
			return &msg_client_message.BattleFighter{}
		},
	}
}

func (p *MsgBattleFighterPool) Get() *msg_client_message.BattleFighter {
	return p.pool.Get().(*msg_client_message.BattleFighter)
}

func (p *MsgBattleFighterPool) Put(fighter *msg_client_message.BattleFighter) {
	p.pool.Put(fighter)
}

// MsgBattleMemberBuffPool
type MsgBattleMemberBuffPool struct {
	pool *sync.Pool
}

func (p *MsgBattleMemberBuffPool) Init() {
	p.pool = &sync.Pool{
		New: func() interface{} {
			return &msg_client_message.BattleMemberBuff{}
		},
	}
}

func (p *MsgBattleMemberBuffPool) Get() *msg_client_message.BattleMemberBuff {
	return p.pool.Get().(*msg_client_message.BattleMemberBuff)
}

func (p *MsgBattleMemberBuffPool) Put(buff *msg_client_message.BattleMemberBuff) {
	p.pool.Put(buff)
}

// MsgBattleReportItemPool
type MsgBattleReportItemPool struct {
	pool *sync.Pool
}

func (p *MsgBattleReportItemPool) Init() {
	p.pool = &sync.Pool{
		New: func() interface{} {
			return &msg_client_message.BattleReportItem{}
		},
	}
}

func (p *MsgBattleReportItemPool) Get() *msg_client_message.BattleReportItem {
	return p.pool.Get().(*msg_client_message.BattleReportItem)
}

func (p *MsgBattleReportItemPool) Put(item *msg_client_message.BattleReportItem) {
	p.pool.Put(item)
}

// MsgBattleRoundReportsPool
type MsgBattleRoundReportsPool struct {
	pool *sync.Pool
}

func (p *MsgBattleRoundReportsPool) Init() {
	p.pool = &sync.Pool{
		New: func() interface{} {
			return &msg_client_message.BattleRoundReports{}
		},
	}
}

func (p *MsgBattleRoundReportsPool) Get() *msg_client_message.BattleRoundReports {
	return p.pool.Get().(*msg_client_message.BattleRoundReports)
}

func (p *MsgBattleRoundReportsPool) Put(reports *msg_client_message.BattleRoundReports) {
	p.pool.Put(reports)
}

// DelaySkillPool
type DelaySkillPool struct {
	pool *sync.Pool
}

func (p *DelaySkillPool) Init() {
	p.pool = &sync.Pool{
		New: func() interface{} {
			return &DelaySkill{}
		},
	}
}

func (p *DelaySkillPool) Get() *DelaySkill {
	return p.pool.Get().(*DelaySkill)
}

func (p *DelaySkillPool) Put(ds *DelaySkill) {
	p.pool.Put(ds)
}
