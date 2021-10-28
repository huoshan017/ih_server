package main

import (
	_ "ih_server/libs/log"
	"ih_server/libs/utils"
	"time"
)

type RolePowerRankItem struct {
	Power  int32
	RoleId int32
}

func (rankItem *RolePowerRankItem) Less(item utils.ShortRankItem) bool {
	it := item.(*RolePowerRankItem)
	if it == nil {
		return false
	}
	if rankItem.Power < it.Power {
		return true
	}
	return false
}

func (rankItem *RolePowerRankItem) Greater(item utils.ShortRankItem) bool {
	it := item.(*RolePowerRankItem)
	if it == nil {
		return false
	}
	if rankItem.Power > it.Power {
		return true
	}
	return false
}

func (rankItem *RolePowerRankItem) GetKey() interface{} {
	return rankItem.RoleId
}

func (rankItem *RolePowerRankItem) GetValue() interface{} {
	return rankItem.Power
}

func (rankItem *RolePowerRankItem) Assign(item utils.ShortRankItem) {
	it := item.(*RolePowerRankItem)
	if it == nil {
		return
	}
	rankItem.RoleId = it.RoleId
	rankItem.Power = it.Power
}

func (rankItem *RolePowerRankItem) Add(item utils.ShortRankItem) {
}

func (rankItem *RolePowerRankItem) New() utils.ShortRankItem {
	return &RolePowerRankItem{}
}

// 更新排名
const (
	MAX_ROLES_POWER_NUM_TO_RANK_ITEM = 4
)

func (p *Player) _update_role_power_rank_info(role_id, power int32) {
	var item = RolePowerRankItem{
		RoleId: role_id,
		Power:  power,
	}
	p.role_power_ranklist.Update(&item, false)
}

func (p *Player) _update_roles_power_rank_info(update_time int32) {
	// 放入所有玩家的角色战力中排序
	var power int32
	for r := 1; r <= MAX_ROLES_POWER_NUM_TO_RANK_ITEM; r++ {
		_, value := p.role_power_ranklist.GetByRank(int32(r))
		if value == nil {
			continue
		}
		p := value.(int32)
		power += p
	}
	if power <= 0 {
		return
	}
	var data = PlayerInt32RankItem{
		Value:      power,
		UpdateTime: update_time,
		PlayerId:   p.Id,
	}
	rank_list_mgr.UpdateItem(RANK_LIST_TYPE_ROLE_POWER, &data)
}

func (p *Player) _update_roles_power_rank_info_now() {
	now_time := int32(time.Now().Unix())
	p._update_roles_power_rank_info(now_time)
	p.db.RoleCommon.SetPowerUpdateTime(now_time)
}

func (p *Player) UpdateRolePowerRank(role_id int32) {
	p.role_update_suit_attr_power(role_id, false, true)
	power := p.get_role_power(role_id)
	before_rank := p.role_power_ranklist.GetRank(role_id)
	p._update_role_power_rank_info(role_id, power)
	after_rank := p.role_power_ranklist.GetRank(role_id)
	if (before_rank >= 1 && before_rank <= MAX_ROLES_POWER_NUM_TO_RANK_ITEM) || (after_rank >= 1 && after_rank <= MAX_ROLES_POWER_NUM_TO_RANK_ITEM) {
		p._update_roles_power_rank_info_now()
	}
}

func (p *Player) DeleteRolePowerRank(role_id int32) {
	if p.role_power_ranklist == nil {
		return
	}
	rank := p.role_power_ranklist.GetRank(role_id)
	p.role_power_ranklist.Delete(role_id)
	if rank >= 1 && rank <= MAX_ROLES_POWER_NUM_TO_RANK_ITEM {
		p._update_roles_power_rank_info_now()
	}
}

// 载入数据库角色战力
func (p *Player) LoadRolesPowerRankData() {
	ids := p.db.Roles.GetAllIndex()
	if ids == nil {
		return
	}

	// 载入个人角色战力并排序
	for _, id := range ids {
		p.role_update_suit_attr_power(id, false, true)
		power := p.get_role_power(id)
		if power > 0 {
			p._update_role_power_rank_info(id, power)
		}
	}

	update_time := p.db.RoleCommon.GetPowerUpdateTime()
	if update_time == 0 {
		update_time = p.db.Info.GetLastLogin()
	}
	p._update_roles_power_rank_info(update_time)
}
