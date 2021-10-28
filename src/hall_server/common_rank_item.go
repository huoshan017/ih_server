package main

import (
	"ih_server/libs/utils"
	"time"
)

type PlayerInt32RankItem struct {
	Value      int32
	UpdateTime int32
	PlayerId   int32
}

func (p *PlayerInt32RankItem) Less(value interface{}) bool {
	item := value.(*PlayerInt32RankItem)
	if item == nil {
		return false
	}
	if p.Value < item.Value {
		return true
	} else if p.Value == item.Value {
		if p.UpdateTime > item.UpdateTime {
			return true
		}
		if p.UpdateTime == item.UpdateTime {
			if p.PlayerId > item.PlayerId {
				return true
			}
		}
	}
	return false
}

func (p *PlayerInt32RankItem) Greater(value interface{}) bool {
	item := value.(*PlayerInt32RankItem)
	if item == nil {
		return false
	}
	if p.Value > item.Value {
		return true
	} else if p.Value == item.Value {
		if p.UpdateTime < item.UpdateTime {
			return true
		}
		if p.UpdateTime == item.UpdateTime {
			if p.PlayerId < item.PlayerId {
				return true
			}
		}
	}
	return false
}

func (p *PlayerInt32RankItem) KeyEqual(value interface{}) bool {
	item := value.(*PlayerInt32RankItem)
	if item == nil {
		return false
	}
	if item == nil {
		return false
	}
	if p.PlayerId == item.PlayerId {
		return true
	}
	return false
}

func (p *PlayerInt32RankItem) GetKey() interface{} {
	return p.PlayerId
}

func (p *PlayerInt32RankItem) GetValue() interface{} {
	return p.Value
}

func (p *PlayerInt32RankItem) SetValue(value interface{}) {
	p.Value = value.(int32)
	p.UpdateTime = int32(time.Now().Unix())
}

func (p *PlayerInt32RankItem) New() utils.SkiplistNode {
	return &PlayerInt32RankItem{}
}

func (p *PlayerInt32RankItem) Assign(node utils.SkiplistNode) {
	n := node.(*PlayerInt32RankItem)
	if n == nil {
		return
	}
	p.Value = n.Value
	p.UpdateTime = n.UpdateTime
	p.PlayerId = n.PlayerId
}

func (p *PlayerInt32RankItem) CopyDataTo(node interface{}) {
	n := node.(*PlayerInt32RankItem)
	if n == nil {
		return
	}
	n.Value = p.Value
	n.UpdateTime = p.UpdateTime
	n.PlayerId = p.PlayerId
}
