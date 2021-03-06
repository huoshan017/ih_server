package main

import (
	"ih_server/libs/log"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	"ih_server/src/table_config"
	"math/rand"

	"github.com/golang/protobuf/proto"
)

const (
	ROLE_STATE_NONE    = iota
	ROLE_STATE_TEAM    = 1
	ROLE_STATE_EXPLORE = 2
)

func (d *dbPlayerRoleColumn) BuildMsg() (roles []*msg_client_message.Role) {
	d.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.BuildMsg")
	defer d.m_row.m_lock.UnSafeRUnlock()

	for _, v := range d.m_data {
		is_lock := false
		if v.IsLock > 0 {
			is_lock = true
		}
		role := &msg_client_message.Role{
			Id:      v.Id,
			TableId: v.TableId,
			Rank:    v.Rank,
			Level:   v.Level,
			IsLock:  is_lock,
			Equips:  v.Equip,
			State:   v.State,
		}
		roles = append(roles, role)
	}
	return
}

func (d *dbPlayerRoleColumn) BuildSomeMsg(ids []int32) (roles []*msg_client_message.Role) {
	d.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.BuildOneMsg")
	defer d.m_row.m_lock.UnSafeRUnlock()

	for i := 0; i < len(ids); i++ {
		v, o := d.m_data[ids[i]]
		if !o {
			return
		}

		is_lock := false
		if v.IsLock > 0 {
			is_lock = true
		}
		role := &msg_client_message.Role{
			Id:      v.Id,
			TableId: v.TableId,
			Rank:    v.Rank,
			Level:   v.Level,
			IsLock:  is_lock,
			Equips:  v.Equip,
			State:   v.State,
		}
		roles = append(roles, role)
	}
	return
}

func (p *Player) new_role(role_id int32, rank int32, level int32) int32 {
	card := card_table_mgr.GetRankCard(role_id, rank)
	if card == nil {
		log.Error("Cant get role card by id[%v] rank[%v]", role_id, rank)
		return 0
	}

	// 转成碎片
	if p.db.Roles.NumAll() >= global_config.MaxRoleCount {
		if card.BagFullChangeItem != nil {
			p.add_resources(card.BagFullChangeItem)
		}
		log.Debug("Player[%v] get new role[%v] to transfer to piece[%v] because role bag is full", p.Id, role_id, card.BagFullChangeItem)
		return -1
	}

	var role dbPlayerRoleData
	role.TableId = role_id
	role.Id = p.db.Global.IncbyCurrentRoleId(1)
	role.Rank = rank
	role.Level = level
	p.db.Roles.Add(&role)

	p.roles_id_change_info.id_add(role.Id)

	// 图鉴
	handbook := p.db.RoleHandbook.GetRole()
	if handbook == nil {
		p.db.RoleHandbook.SetRole([]int32{role_id})
		if !p.is_handbook_adds {
			p.is_handbook_adds = true
		}
	} else {
		found := false
		for i := 0; i < len(handbook); i++ {
			if handbook[i] == role_id {
				if !p.is_handbook_adds {
					p.is_handbook_adds = true
				}
				found = true
				break
			}
		}
		if !found {
			handbook = append(handbook, role_id)
			p.db.RoleHandbook.SetRole(handbook)
		}
	}

	// 头像
	p.add_item(card.HeadItem, 1)

	// 更新任务
	p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_GET_STAR_ROLES, false, card.Rarity, 1)

	log.Trace("Player[%v] create new role[%v] table_id[%v]", p.Id, role.Id, role_id)

	// 更新排行榜
	p.UpdateRolePowerRank(role.Id)

	top_power_match_manager.CheckDefensePowerUpdate(p)

	// 活动更新
	p.activitys_update(ACTIVITY_EVENT_GET_HERO, card.Rarity, 1, card.Camp, card.Type)

	return role.Id
}

func (p *Player) has_role(id int32) bool {
	all := p.db.Roles.GetAllIndex()
	for i := 0; i < len(all); i++ {
		table_id, o := p.db.Roles.GetTableId(all[i])
		if o && table_id == id {
			return true
		}
	}
	return false
}

func (p *Player) rand_role() int32 {
	// 转成碎片
	if p.db.Roles.NumAll() >= global_config.MaxRoleCount {
		log.Debug("Player[%v] rand new role failed because role bag is full", p.Id)
		return -1
	}

	if card_table_mgr.Array == nil {
		return 0
	}

	c := len(card_table_mgr.Array)
	r := rand.Intn(c)
	cr := r
	table_id := int32(0)
	var card *table_config.XmlCardItem
	for {
		card = card_table_mgr.Array[r%c]
		table_id = card.Id
		if !p.has_role(table_id) {
			break
		}
		r += 1
		if r-cr >= c {
			// 允许重复
			//table_id = 0
			break
		}
	}

	id := int32(0)
	if table_id > 0 {
		id = p.db.Global.IncbyCurrentRoleId(1)
		p.db.Roles.Add(&dbPlayerRoleData{
			Id:      id,
			TableId: table_id,
			Rank:    1,
			Level:   1,
		})

		p.roles_id_change_info.id_add(id)
		log.Debug("Player[%v] rand role[%v]", p.Id, table_id)

		// 头像
		p.add_item(card.HeadItem, 1)

		// 更新任务
		p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_GET_STAR_ROLES, false, card.Rarity, 1)

		// 更新排行榜
		p.UpdateRolePowerRank(id)

		top_power_match_manager.CheckDefensePowerUpdate(p)
	}

	return id
}

func (p *Player) role_is_using(role_id int32) bool {
	if role_id <= 0 {
		return false
	}

	// 是否在防守阵容或战役阵容中
	members_array := [][]int32{p.db.BattleTeam.GetCampaignMembers(), p.db.BattleTeam.GetDefenseMembers()}
	for n := 0; n < len(members_array); n++ {
		if members_array[n] == nil {
			continue
		}
		for i := 0; i < len(members_array[n]); i++ {
			if members_array[n][i] == role_id {
				return true
			}
		}
	}

	// 是否被设置成助战角色
	if p.db.FriendCommon.GetAssistRoleId() == role_id {
		return true
	}

	// 是否在探索任务中
	state, _ := p.db.Roles.GetState(role_id)
	return state != ROLE_STATE_NONE
}

func (p *Player) delete_role(role_id int32) (deleted bool, get_items map[int32]int32) {
	if !p.db.Roles.HasIndex(role_id) {
		return
	}
	is_lock, _ := p.db.Roles.GetIsLock(role_id)
	if is_lock > 0 {
		log.Warn("Player[%v] role[%v] is locked, cant delete", p.Id, role_id)
		return
	}

	if p.role_is_using(role_id) {
		log.Warn("Player[%v] role[%v] is using, cant delete", p.Id, role_id)
		return
	}

	// 脱下装备
	equips, _ := p.db.Roles.GetEquip(role_id)
	if equips != nil {
		for i := 0; i < len(equips); i++ {
			if equips[i] <= 0 {
				continue
			}
			if get_items == nil {
				get_items = make(map[int32]int32)
			}
			if int32(i) != EQUIP_TYPE_LEFT_SLOT {
				p.add_item(equips[i], 1)
				get_items[equips[i]] += 1
			} else {
				items := p.item_to_resource(equips[i])
				for k, v := range items {
					get_items[k] += v
				}
			}
		}
	}

	p.db.Roles.Remove(role_id)
	if p.team_member_mgr != nil {
		m := p.team_member_mgr[role_id]
		if m != nil {
			delete(p.team_member_mgr, role_id)
			team_member_pool.Put(m)
		}
	}

	p.roles_id_change_info.id_remove(role_id)

	// 更新排行榜
	p.DeleteRolePowerRank(role_id)

	if p.db.ExpeditionRoles.HasIndex(role_id) {
		p.db.ExpeditionRoles.Remove(role_id)
	}

	deleted = true
	return
}

func (p *Player) check_and_send_roles_change() {
	if p.roles_id_change_info.is_changed() {
		var msg msg_client_message.S2CRolesChangeNotify
		if p.roles_id_change_info.add != nil {
			roles := p.db.Roles.BuildSomeMsg(p.roles_id_change_info.add)
			if roles != nil {
				msg.Adds = roles
			}
		}
		if p.roles_id_change_info.remove != nil {
			msg.Removes = p.roles_id_change_info.remove
		}
		if p.roles_id_change_info.update != nil {
			roles := p.db.Roles.BuildSomeMsg(p.roles_id_change_info.update)
			if roles != nil {
				msg.Updates = roles
			}
		}
		p.roles_id_change_info.reset()
		p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLES_CHANGE_NOTIFY), &msg)
	}
}

func (p *Player) add_init_roles() {
	var team []int32
	init_roles := global_config.InitRoles
	for i := 0; i < len(init_roles)/3; i++ {
		iid := p.new_role(init_roles[3*i], init_roles[3*i+1], init_roles[3*i+2])
		if team == nil {
			team = []int32{iid}
			//} else if len(team) < BATTLE_TEAM_MEMBER_MAX_NUM {
			//team = append(team, iid)
		}
	}
}

func (p *Player) send_roles() {
	msg := &msg_client_message.S2CRolesResponse{}
	msg.Roles = p.db.Roles.BuildMsg()
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLES_RESPONSE), msg)
}

func (p *Player) get_team_member_by_role(role_id int32, team *BattleTeam, pos int32) (m *TeamMember) {
	var table_id, rank, level int32
	var equips []int32
	var o, use_assist bool

	if p.assist_friend != nil && p.assist_role_id > 0 && p.assist_role_pos == pos {
		use_assist = true
	}

	if !use_assist {
		table_id, o = p.db.Roles.GetTableId(role_id)
		if !o {
			log.Error("Cant get table id by role id[%v]", role_id)
			return
		}
		rank, o = p.db.Roles.GetRank(role_id)
		if !o {
			log.Error("Cant get rank by role id[%v]", role_id)
			return
		}
		level, o = p.db.Roles.GetLevel(role_id)
		if !o {
			log.Error("Cant get level by role id[%v]", role_id)
			return
		}
		equips, o = p.db.Roles.GetEquip(role_id)
		if !o {
			log.Error("Cant get equips by role id[%v]", role_id)
			return
		}
	} else {
		table_id, o = p.assist_friend.db.Roles.GetTableId(p.assist_role_id)
		if !o {
			return
		}
		rank, o = p.assist_friend.db.Roles.GetRank(p.assist_role_id)
		if !o {
			return
		}
		level, o = p.assist_friend.db.Roles.GetLevel(p.assist_role_id)
		if !o {
			return
		}
		equips, o = p.assist_friend.db.Roles.GetEquip(p.assist_role_id)
		if !o {
			log.Error("Player[%v] Cant get equips by assist friend[%v] role[%v]", p.Id, p.assist_friend.Id, p.assist_role_id)
			return
		}
	}
	role_card := card_table_mgr.GetRankCard(table_id, rank)
	if role_card == nil {
		log.Error("Cant get card by table id[%v] and rank[%v]", table_id, rank)
		return
	}

	m = team_member_pool.Get()
	if team == nil {
		// 计算属性
		m.init_attrs_equips_skills(level, role_card, equips, nil)
		p.role_update_suit_attr_power(role_id, true, true)
	} else {
		// 初始化阵型
		if use_assist {
			role_id = -role_id
		}
		m.init_all(team, role_id, level, role_card, pos, equips, nil)
	}
	if use_assist {
		p.assist_member = m
	}
	return
}

func (p *Player) send_role_attrs(role_id int32) int32 {
	if !p.db.Roles.HasIndex(role_id) {
		log.Error("Player[%v] no role[%v], send attrs failed", p.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
	}

	m := p.get_team_member_by_role(role_id, nil, -1)
	if m == nil {
		log.Error("Player[%v] get team member with role[%v] failed, cant send role attrs", p.Id, role_id)
		return -1
	}

	power := p.get_role_power(role_id)
	response := &msg_client_message.S2CRoleAttrsResponse{
		RoleId: role_id,
		Attrs:  m.attrs,
		Power:  power,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_ATTRS_RESPONSE), response)

	log.Trace("Player[%v] send role[%v] attrs: %v  power: %v", p.Id, role_id, m.attrs, power)

	return 1
}

func (p *Player) lock_role(role_id int32, is_lock bool) int32 {
	if !p.db.Roles.HasIndex(role_id) {
		log.Error("Player[%v] not found role[%v], lock failed", p.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
	}
	if is_lock {
		p.db.Roles.SetIsLock(role_id, 1)
	} else {
		p.db.Roles.SetIsLock(role_id, 0)
	}

	p.roles_id_change_info.id_update(role_id)
	p.check_and_send_roles_change()

	response := &msg_client_message.S2CRoleLockResponse{
		RoleId: role_id,
		IsLock: is_lock,
	}

	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_LOCK_RESPONSE), response)
	return 1
}

func (p *Player) _levelup_role(role_id, lvl int32, tmp_cache_items map[int32]int32) int32 {
	if len(levelup_table_mgr.Array) <= int(lvl) {
		log.Error("Player[%v] is already max level[%v]", p.Id, lvl)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_LEVEL_IS_MAX)
	}

	levelup_data := levelup_table_mgr.Get(lvl)
	if levelup_data == nil {
		log.Error("cant found level[%v] data", lvl)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_LEVEL_DATA_NOT_FOUND)
	}

	if levelup_data.CardLevelUpRes != nil {
		for i := 0; i < len(levelup_data.CardLevelUpRes)/2; i++ {
			resource_id := levelup_data.CardLevelUpRes[2*i]
			resource_num := levelup_data.CardLevelUpRes[2*i+1]
			now_num := p.get_resource(resource_id)
			if now_num < resource_num {
				log.Error("Player[%v] levelup role[%v] cost resource[%v] not enough, need[%v] now[%v]", p.Id, role_id, resource_id, resource_num, now_num)
				return int32(msg_client_message.E_ERR_PLAYER_ITEM_NUM_NOT_ENOUGH)
			}
			num := tmp_cache_items[resource_id]
			if num == 0 {
				tmp_cache_items[resource_id] = resource_num
			} else {
				tmp_cache_items[resource_id] = num + resource_num
			}
		}
	}
	return 1
}

func (p *Player) levelup_role(role_id, up_num int32) int32 {
	if up_num <= 0 {
		up_num = 1
	}

	lvl, o := p.db.Roles.GetLevel(role_id)
	if !o {
		log.Error("Player[%v] not have role[%v]", p.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
	}

	table_id, _ := p.db.Roles.GetTableId(role_id)
	rank, _ := p.db.Roles.GetRank(role_id)
	card := card_table_mgr.GetRankCard(table_id, rank)
	if card == nil {
		log.Error("Role table data %v not found", table_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_TABLE_ID_NOT_FOUND)
	}

	if card.MaxLevel <= lvl {
		log.Error("Player[%v] is already max level[%v]", p.Id, lvl)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_LEVEL_IS_MAX)
	}

	//if p.tmp_cache_items == nil || len(p.tmp_cache_items) > 0 {
	//	p.tmp_cache_items = make(map[int32]int32)
	//}
	var tmp_cache_items = make(map[int32]int32)
	res := int32(0)
	for i := int32(0); i < up_num; i++ {
		res = p._levelup_role(role_id, lvl+i, tmp_cache_items)
		if res < 0 {
			return res
		}
	}
	for id, num := range tmp_cache_items {
		p.add_resource(id, -num)
	}
	//p.tmp_cache_items = nil

	p.db.Roles.SetLevel(role_id, lvl+up_num)
	p.roles_id_change_info.id_update(role_id)
	p.check_and_send_roles_change()

	response := &msg_client_message.S2CRoleLevelUpResponse{
		RoleId: role_id,
		Level:  lvl + up_num,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_LEVELUP_RESPONSE), response)

	// 更新任务
	p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_LEVELUP_ROLE_WITH_CAMP, false, card.Camp, up_num)

	log.Trace("Player[%v] role[%v] up to level[%v]", p.Id, role_id, lvl+up_num)

	// 更新排行榜
	p.UpdateRolePowerRank(role_id)

	top_power_match_manager.CheckDefensePowerUpdate(p)

	return lvl
}

func (p *Player) rankup_role(role_id int32) int32 {
	rank, o := p.db.Roles.GetRank(role_id)
	if !o {
		log.Error("Player[%v] not have role[%v]", p.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
	}

	table_id, _ := p.db.Roles.GetTableId(role_id)
	cards := card_table_mgr.GetCards(table_id)
	if len(cards) <= int(rank) {
		log.Error("Player[%v] is already max rank[%v]", p.Id, rank)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_RANK_IS_MAX)
	}

	card_data := card_table_mgr.GetRankCard(table_id, rank)
	if card_data == nil {
		log.Error("Cant found card[%v,%v] data", table_id, rank)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_TABLE_ID_NOT_FOUND)
	}

	curr_level, _ := p.db.Roles.GetLevel(role_id)
	if card_data.MaxLevel > curr_level {
		log.Error("Player %v cant rank up role %v with level %v", role_id, role_id, curr_level)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_CANT_RANKUP_WITH_LEVEL)
	}

	rank_data := rankup_table_mgr.Get(rank)
	if rank_data == nil {
		log.Error("Cant found rankup[%v] data", rank)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_RANKUP_DATA_NOT_FOUND)
	}
	var cost_resources []int32
	if card_data.Type == 1 {
		cost_resources = rank_data.Type1RankUpRes
	} else if card_data.Type == 2 {
		cost_resources = rank_data.Type2RankUpRes
	} else if card_data.Type == 3 {
		cost_resources = rank_data.Type3RankUpRes
	} else {
		log.Error("Card[%v,%v] type[%v] invalid", table_id, rank, card_data.Type)
		return -1
	}

	for i := 0; i < len(cost_resources)/2; i++ {
		resource_id := cost_resources[2*i]
		resource_num := cost_resources[2*i+1]
		rn := p.get_resource(resource_id)
		if rn < resource_num {
			log.Error("Player[%v] rank[%] up failed, resource[%v] num[%v] not enough", p.Id, rank, resource_id, rn)
			return int32(msg_client_message.E_ERR_PLAYER_ITEM_NUM_NOT_ENOUGH)
		}
	}

	for i := 0; i < len(cost_resources)/2; i++ {
		resource_id := cost_resources[2*i]
		resource_num := cost_resources[2*i+1]
		p.add_resource(resource_id, -resource_num)
	}

	rank += 1
	p.db.Roles.SetRank(role_id, rank)
	p.roles_id_change_info.id_update(role_id)

	p.check_and_send_roles_change()

	response := &msg_client_message.S2CRoleRankUpResponse{
		RoleId: role_id,
		Rank:   rank,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_RANKUP_RESPONSE), response)

	log.Trace("Player[%v] role[%v] up rank[%v]", p.Id, role_id, rank)

	// 更新排行榜
	p.UpdateRolePowerRank(role_id)

	top_power_match_manager.CheckDefensePowerUpdate(p)

	return rank
}

/*
func get_decompose_rank_res(table_id, rank int32) []int32 {
	rank_data := rankup_table_mgr.Get(rank)
	if rank_data == nil {
		log.Error("Cant get rankup[%v] data", rank)
		return nil
	}
	var resources []int32
	card_data := card_table_mgr.GetRankCard(table_id, rank)
	if card_data == nil {
		log.Error("Cant found card[%v,%v] data", table_id, rank)
		return nil
	}
	if card_data.Type == 1 {
		resources = rank_data.Type1DecomposeRes
	} else if card_data.Type == 2 {
		resources = rank_data.Type2DecomposeRes
	} else if card_data.Type == 3 {
		resources = rank_data.Type3DecomposeRes
	} else {
		log.Error("Card[%v,%v] type[%v] invalid", table_id, rank, card_data.Type)
		return nil
	}

	return resources
}

func (p *Player) team_has_role(team_id int32, role_id int32) bool {
	var members []int32
	if team_id == BATTLE_TEAM_CAMPAIN {
		members = p.db.BattleTeam.GetCampaignMembers()
	} else if team_id == BATTLE_TEAM_DEFENSE {
		members = p.db.BattleTeam.GetDefenseMembers()
	}
	for _, m := range members {
		if role_id == m {
			return true
		}
	}
	return false
}
*/

func (p *Player) decompose_role(role_ids []int32) int32 {
	if role_ids == nil {
		return -1
	}
	log.Debug("Player[%v] will decompose roles %v", p.Id, role_ids)
	var num int32
	var tmp_cache_items map[int32]int32
	for i := 0; i < len(role_ids); i++ {
		role_id := role_ids[i]
		_, o := p.db.Roles.GetLevel(role_id)
		if !o {
			log.Error("Player[%v] not have role[%v]", p.Id, role_id)
			//return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
			continue
		}

		is_lock, _ := p.db.Roles.GetIsLock(role_id)
		if is_lock > 0 {
			log.Error("Player[%v] role[%v] is locked, cant decompose", p.Id, role_id)
			//return int32(msg_client_message.E_ERR_PLAYER_ROLE_IS_LOCKED)
			continue
		}

		if p.role_is_using(role_id) {
			log.Warn("Player[%v] role[%v] is busy, cant decompose", p.Id, role_id)
			continue
		}

		rank, _ := p.db.Roles.GetRank(role_id)
		table_id, _ := p.db.Roles.GetTableId(role_id)

		card_data := card_table_mgr.GetRankCard(table_id, rank)
		if card_data == nil {
			log.Error("Not found card data by table_id[%v] and rank[%v]", table_id, rank)
			//return int32(msg_client_message.E_ERR_PLAYER_ROLE_TABLE_ID_NOT_FOUND)
			continue
		}

		for n := 0; n < len(card_data.DecomposeRes)/2; n++ {
			item_id := card_data.DecomposeRes[2*n]
			item_num := card_data.DecomposeRes[2*n+1]
			p.add_resource(item_id, item_num)
			if tmp_cache_items == nil {
				tmp_cache_items = make(map[int32]int32)
			}
			tmp_cache_items[item_id] += item_num
		}

		items_map := p._return_role_resource(role_id)
		for k, v := range items_map {
			tmp_cache_items[k] += v
		}

		deleted, tmp_items := p.delete_role(role_id)
		if deleted {
			for k, v := range tmp_items {
				tmp_cache_items[k] += v
			}
		}
		num += 1
	}

	response := &msg_client_message.S2CRoleDecomposeResponse{
		RoleIds:  role_ids,
		GetItems: Map2ItemInfos(tmp_cache_items),
	}
	//if p.tmp_cache_items != nil {
	//	p.tmp_cache_items = nil
	//}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_DECOMPOSE_RESPONSE), response)

	p.check_and_send_roles_change()

	// 更新任务
	p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_DECOMPOSE_ROLES, false, 0, num)

	log.Trace("Player[%v] decompose roles %v", p.Id, role_ids)

	return 1
}

func (p *Player) check_fusion_role_cond(cost_role_ids []int32, cost_cond *table_config.FusionCostCond) int32 {
	for i := 0; i < len(cost_role_ids); i++ {
		if !p.db.Roles.HasIndex(cost_role_ids[i]) {
			log.Error("Player[%v] fusion role need role[%v] not found", p.Id, cost_role_ids[i])
			return int32(msg_client_message.E_ERR_PLAYER_FUSION_NEED_ROLE_NOT_FOUND)
		}

		is_lock, _ := p.db.Roles.GetIsLock(cost_role_ids[i])
		if is_lock > 0 {
			log.Error("Player[%v] role[%v] is locked, fusion check role cond failed", p.Id, cost_role_ids[i])
			return int32(msg_client_message.E_ERR_PLAYER_ROLE_IS_LOCKED)
		}

		table_id, _ := p.db.Roles.GetTableId(cost_role_ids[i])
		if cost_cond.CostId > 0 && table_id != cost_cond.CostId {
			log.Error("Player[%v] fusion cost role[%v] invalid", p.Id, cost_role_ids[i])
			return int32(msg_client_message.E_ERR_PLAYER_FUSION_ROLE_INVALID)
		} else {
			rank, _ := p.db.Roles.GetRank(cost_role_ids[i])
			card := card_table_mgr.GetRankCard(table_id, rank)
			if card == nil {
				log.Error("Player[%v] fusion role[%v] not found card[%v] with rank[%v]", p.Id, cost_role_ids[i], table_id, rank)
				return int32(msg_client_message.E_ERR_PLAYER_FUSION_ROLE_INVALID)
			}
			if cost_cond.CostCamp > 0 && card.Camp != cost_cond.CostCamp {
				log.Error("Player[%v] fusion role[%v] camp[%v] invalid", p.Id, cost_role_ids[i], card.Camp)
				return int32(msg_client_message.E_ERR_PLAYER_FUSION_ROLE_INVALID)
			}
			if cost_cond.CostType > 0 && card.Type != cost_cond.CostType {
				log.Error("Player[%v] fusion role[%v] type[%v] invalid", p.Id, cost_role_ids[i], card.Type)
				return int32(msg_client_message.E_ERR_PLAYER_FUSION_ROLE_INVALID)
			}
			if cost_cond.CostStar > 0 && card.Rarity != cost_cond.CostStar {
				log.Error("Player[%v] fusion role[%v] star[%v] invalid", p.Id, cost_role_ids[i], card.Type)
				return int32(msg_client_message.E_ERR_PLAYER_FUSION_ROLE_INVALID)
			}
		}
	}
	return 1
}

// 返还升级升阶消耗的资源
func (p *Player) _return_role_resource(role_id int32) (items_map map[int32]int32) {
	lvl, _ := p.db.Roles.GetLevel(role_id)
	rank, _ := p.db.Roles.GetRank(role_id)

	levelup_data := levelup_table_mgr.Get(lvl)
	if levelup_data == nil {
		return
	}
	d := levelup_data.CardDecomposeRes
	if d != nil {
		for j := 0; j < len(d)/2; j++ {
			if items_map == nil {
				items_map = make(map[int32]int32)
			}
			items_map[d[2*j]] += d[2*j+1]
		}
	}

	rankup_data := rankup_table_mgr.Get(rank)
	if rankup_data == nil {
		return
	}

	table_id, _ := p.db.Roles.GetTableId(role_id)
	card := card_table_mgr.GetRankCard(table_id, rank)
	if card == nil {
		return
	}

	var dr []int32
	if card.Type == table_config.CARD_ROLE_TYPE_ATTACK {
		dr = rankup_data.Type1DecomposeRes
	} else if card.Type == table_config.CARD_ROLE_TYPE_DEFENSE {
		dr = rankup_data.Type2DecomposeRes
	} else if card.Type == table_config.CARD_ROLE_TYPE_SKILL {
		dr = rankup_data.Type3DecomposeRes
	} else {
		return
	}

	for j := 0; j < len(dr)/2; j++ {
		if items_map == nil {
			items_map = make(map[int32]int32)
		}
		items_map[dr[2*j]] += dr[2*j+1]
	}

	for k, v := range items_map {
		p.add_resource(k, v)
	}

	return items_map
}

func (p *Player) fusion_role(fusion_id, main_role_id int32, cost_role_ids [][]int32) int32 {
	if cost_role_ids == nil {
		return -1
	}

	fusion := fusion_table_mgr.Get(fusion_id)
	if fusion == nil {
		log.Error("Fusion[%v] table data not found", fusion_id)
		return int32(msg_client_message.E_ERR_PLAYER_FUSION_ROLE_TABLE_DATA_NOT_FOUND)
	}

	// 资源是否足够
	for i := 0; i < len(fusion.ResCondition)/2; i++ {
		res_id := fusion.ResCondition[2*i]
		res_num := fusion.ResCondition[2*i+1]
		rn := p.get_resource(res_id)
		if rn < res_num {
			log.Error("Player[%v] fusion[%v] resource[%v] num[%v] not enough, need %v", p.Id, fusion_id, res_id, rn, res_num)
			return int32(msg_client_message.E_ERR_PLAYER_FUSION_NEED_RESOURCE_NOT_ENOUGH)
		}
	}

	// 固定配方
	if fusion.FusionType == 1 {
		if !p.db.Roles.HasIndex(main_role_id) {
			log.Error("Player[%v] fusion[%v] not found main role[%v]", p.Id, fusion_id, main_role_id)
			return int32(msg_client_message.E_ERR_PLAYER_FUSION_MAIN_ROLE_NOT_FOUND)
		}

		is_lock, _ := p.db.Roles.GetIsLock(main_role_id)
		if is_lock > 0 {
			log.Error("Player[%v] role[%v] is locked, cant fusion", p.Id, main_role_id)
			return int32(msg_client_message.E_ERR_PLAYER_ROLE_IS_LOCKED)
		}

		main_card_id, _ := p.db.Roles.GetTableId(main_role_id)
		if main_card_id != fusion.MainCardID {
			log.Error("Player[%v] fusion[%v] main card id[%v] is invalid", p.Id, fusion_id, main_card_id)
			return int32(msg_client_message.E_ERR_PLAYER_FUSION_MAIN_CARD_INVALID)
		}

		main_role_level, _ := p.db.Roles.GetLevel(main_role_id)
		if main_role_level < fusion.MainCardLevelCond {
			log.Error("Player[%v] fusion[%v] main card id[%v] level[%v] not enough, need level[%v]", p.Id, fusion_id, main_card_id, main_role_level, fusion.MainCardLevelCond)
			return int32(msg_client_message.E_ERR_PLAYER_FUSION_MAIN_CARD_INVALID)
		}
		//} else {
		/*if p.db.Roles.NumAll() >= global_config.MaxRoleCount {
			log.Error("Player[%v] role inventory is full", p.Id)
			return int32(msg_client_message.E_ERR_PLAYER_ROLE_INVENTORY_NOT_ENOUGH_SPACE)
		}*/
	}

	for i := 0; i < len(cost_role_ids); i++ {
		if i >= len(fusion.CostConds) {
			break
		}
		cn := int32(0)
		if cost_role_ids[i] != nil {
			cn = int32(len(cost_role_ids[i]))
		}
		if fusion.CostConds[i].CostNum > cn {
			log.Error("Player[%v] fusion[%v] cost num %v not enough, need %v", p.Id, fusion_id, cn, fusion.CostConds[i].CostNum)
			return int32(msg_client_message.E_ERR_PLAYER_FUSION_ROLE_MATERIAL_NOT_ENOUGH)
		}
		res := p.check_fusion_role_cond(cost_role_ids[i], fusion.CostConds[i])
		if res < 0 {
			return res
		}
	}

	var item *msg_client_message.ItemInfo
	var o bool
	if o, item = p.drop_item_by_id(fusion.ResultDropID, false, nil, nil); !o {
		log.Error("Player[%v] fusion[%v] drop new card failed", p.Id, fusion_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_FUSION_FAILED)
	}

	// 返还升级升阶的资源
	var get_items map[int32]int32
	for i := 0; i < len(cost_role_ids); i++ {
		for j := 0; j < len(cost_role_ids[i]); j++ {
			items := p._return_role_resource(cost_role_ids[i][j])
			if items != nil {
				if get_items == nil {
					get_items = make(map[int32]int32)
				}
				for k, v := range items {
					get_items[k] += v
				}
			}
			p.delete_role(cost_role_ids[i][j])
		}
	}

	new_role_id := int32(0)
	if fusion.FusionType == 1 {
		new_role_id = main_role_id
		p.db.Roles.SetTableId(main_role_id, item.Id)
		p.roles_id_change_info.id_update(main_role_id)
		// 排行榜更新
		p.UpdateRolePowerRank(main_role_id)
		top_power_match_manager.CheckDefensePowerUpdate(p)
	} else {
		new_role_id = p.new_role(item.Id, 1, 1)
	}

	for i := 0; i < len(fusion.ResCondition)/2; i++ {
		res_id := fusion.ResCondition[2*i]
		res_num := fusion.ResCondition[2*i+1]
		p.add_resource(res_id, -res_num)
	}

	p.check_and_send_roles_change()

	response := &msg_client_message.S2CRoleFusionResponse{
		NewCardId: item.Id,
		RoleId:    new_role_id,
		GetItems:  Map2ItemInfos(get_items),
		FusionId:  fusion_id,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_FUSION_RESPONSE), response)

	log.Trace("Player[%v] fusion[%v] main_card[%v] get new role[%v] new card[%v], cost cards[%v]", p.Id, fusion_id, main_role_id, new_role_id, item.Id, cost_role_ids)

	return 1
}

func (p *Player) get_role_handbook() int32 {
	response := &msg_client_message.S2CRoleHandbookResponse{
		Roles: p.db.RoleHandbook.GetRole(),
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_HANDBOOK_RESPONSE), response)
	return 1
}

func (p *Player) role_open_left_slot(role_id int32) int32 {
	equips, o := p.db.Roles.GetEquip(role_id)
	if !o {
		log.Error("Player[%v] not found role[%v]", p.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
	}

	if len(equips) >= EQUIP_TYPE_MAX {
		if equips[EQUIP_TYPE_LEFT_SLOT] > 0 {
			log.Warn("Player[%v] role[%v] left slot already opened", p.Id, role_id)
			return int32(msg_client_message.E_ERR_PLAYER_ROLE_LEFT_SLOT_ALREADY_OPENED)
		}
	}

	open_level := global_config.ItemLeftSlotOpenLevel
	lvl, _ := p.db.Roles.GetLevel(role_id)
	if lvl < open_level {
		log.Error("Player[%v] open left slot for role[%v] failed, level[%v] not enough", p.Id, role_id, lvl)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_OPEN_LEFTSLOT_LEVEL_NOT_ENOUGH)
	}

	left_drop_id := global_config.LeftSlotDropId
	b, left_item := p.drop_item_by_id(left_drop_id, false, nil, nil)
	if !b {
		log.Error("Player[%v] left slot drop with id[%v] failed for role[%v]", p.Id, left_drop_id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_LEFT_SLOT_DROP_FAILED)
	}

	if len(equips) == 0 {
		equips = make([]int32, EQUIP_TYPE_MAX)
	}
	equips[EQUIP_TYPE_LEFT_SLOT] = left_item.GetId()
	p.db.Roles.SetEquip(role_id, equips)

	p.roles_id_change_info.id_update(role_id)
	p.check_and_send_roles_change()

	response := &msg_client_message.S2CRoleLeftSlotOpenResponse{
		RoleId: role_id,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_LEFTSLOT_OPEN_RESPONSE), response)

	log.Trace("Player[%v] opened left slot for role[%v] with equip[%v]", p.Id, role_id, equips[EQUIP_TYPE_LEFT_SLOT])

	// 更新排行榜
	p.UpdateRolePowerRank(role_id)

	top_power_match_manager.CheckDefensePowerUpdate(p)

	return 1
}

func (p *Player) role_left_slot_upgrade_save() int32 {
	tmp_role_id := p.db.Equip.GetTmpSaveLeftSlotRoleId()
	tmp_left_slot_id := p.db.Equip.GetTmpLeftSlotItemId()
	equips, o := p.db.Roles.GetEquip(tmp_role_id)
	if !o {
		log.Error("Player[%v] role[%v] not found", p.Id, tmp_role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
	}

	if tmp_left_slot_id <= 0 {
		log.Error("Player[%v] not save left slot upgrade result", p.Id)
		return -1
	}

	equips[EQUIP_TYPE_LEFT_SLOT] = tmp_left_slot_id
	p.db.Roles.SetEquip(tmp_role_id, equips)
	p.db.Equip.SetTmpLeftSlotItemId(0)
	p.db.Equip.SetTmpSaveLeftSlotRoleId(0)

	p.roles_id_change_info.id_update(tmp_role_id)
	p.check_and_send_roles_change()

	response := &msg_client_message.S2CRoleLeftSlotResultSaveResponse{
		RoleId: tmp_role_id,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_LEFTSLOT_RESULT_SAVE_RESPONSE), response)

	return 1
}

func (p *Player) role_left_slot_result_cancel() int32 {
	role_id := p.db.Equip.GetTmpSaveLeftSlotRoleId()
	p.db.Equip.SetTmpLeftSlotItemId(0)
	p.db.Equip.SetTmpSaveLeftSlotRoleId(0)
	response := &msg_client_message.S2CRoleLeftSlotResultCancelResponse{
		RoleId: role_id,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_LEFTSLOT_RESULT_CANCEL_RESPONSE), response)
	return 1
}

func (p *Player) role_one_key_equip(role_id int32, equips []int32) int32 {
	role_equips, o := p.db.Roles.GetEquip(role_id)
	if role_equips == nil || !o {
		log.Error("Player[%v] no role[%v], one key equip failed", p.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
	}

	if equips == nil {
		equips = make([]int32, EQUIP_TYPE_MAX)
		copy(equips, role_equips)
		all_item := p.db.Items.GetAllIndex()
		for _, item_id := range all_item {
			item := item_table_mgr.Get(item_id)
			if item == nil || item.EquipType < 1 || item.EquipType == EQUIP_TYPE_LEFT_SLOT {
				continue
			}
			equiped_item := item_table_mgr.Get(equips[item.EquipType])
			if equips[item.EquipType] == 0 || (equiped_item != nil && equiped_item.BattlePower < item.BattlePower) {
				equips[item.EquipType] = item_id
			}

			/*if item.EquipType < int32(len(role_equips)) {
				if role_equips[item.EquipType] <= 0 {
					continue
				}
				e := item_table_mgr.Get(role_equips[item.EquipType])
				if e == nil {
					log.Warn("Player[%v] role[%v] equip type %v item %v not found table data", p.Id, role_id, item.EquipType, role_equips[item.EquipType])
					continue
				}
				// 已装备的大于背包中的，不替换
				if equiped_item != nil && e.BattlePower >= equiped_item.BattlePower {
					equips[item.EquipType] = role_equips[item.EquipType]
				}
			}*/
		}

		for i := 0; i < len(equips); i++ {
			if i == EQUIP_TYPE_LEFT_SLOT {
				continue
			}
			if equips[i] > 0 {
				if i < len(role_equips) && role_equips[i] > 0 {
					if equips[i] != role_equips[i] {
						p.del_item(equips[i], 1)
						p.add_item(role_equips[i], 1)
					}
				} else {
					p.del_item(equips[i], 1)
				}
			}
		}
	} else {
		for _, equip_id := range equips {
			if !p.db.Items.HasIndex(equip_id) {
				log.Error("Player[%v] no item[%v], role[%v] one key equip failed", p.Id, equip_id, role_id)
				return int32(msg_client_message.E_ERR_PLAYER_ITEM_NOT_FOUND)
			}
		}
		if role_equips != nil {
			for i := 0; i < len(role_equips); i++ {
				p.add_item(role_equips[i], 1)
			}
		}
		for _, equip_id := range equips {
			p.del_item(equip_id, 1)
		}
	}

	p.db.Roles.SetEquip(role_id, equips)

	p.roles_id_change_info.id_update(role_id)
	p.check_and_send_roles_change()

	response := &msg_client_message.S2CRoleOneKeyEquipResponse{
		RoleId: role_id,
		Equips: equips,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_ONEKEY_EQUIP_RESPONSE), response)

	log.Trace("Player[%v] role[%v] one key equips[%v]", p.Id, role_id, equips)

	// 更新排行榜
	p.UpdateRolePowerRank(role_id)

	top_power_match_manager.CheckDefensePowerUpdate(p)

	return 1
}

func (p *Player) role_one_key_unequip(role_id int32) int32 {
	equips, o := p.db.Roles.GetEquip(role_id)
	if !o {
		log.Error("Player[%v] not found role[%v], one key equip failed", p.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
	}

	if equips != nil {
		for i := 0; i < len(equips); i++ {
			if i == EQUIP_TYPE_LEFT_SLOT || equips[i] == 0 {
				continue
			}
			p.add_item(equips[i], 1)
			equips[i] = 0
		}
		p.db.Roles.SetEquip(role_id, equips)
	}

	p.roles_id_change_info.id_update(role_id)
	p.check_and_send_roles_change()

	response := &msg_client_message.S2CRoleOneKeyUnequipResponse{
		RoleId: role_id,
		Equips: equips,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_ONEKEY_UNEQUIP_RESPONSE), response)

	// 更新排行榜
	p.UpdateRolePowerRank(role_id)

	top_power_match_manager.CheckDefensePowerUpdate(p)

	return 1
}

func (p *Player) set_role_power(role_id, pow int32) {
	p.roles_power_locker.Lock()
	defer p.roles_power_locker.Unlock()

	if p.roles_power == nil {
		p.roles_power = make(map[int32]int32)
	}
	p.roles_power[role_id] = pow
}

func (p *Player) get_role_power(role_id int32) (power int32) {
	p.roles_power_locker.RLock()
	defer p.roles_power_locker.RUnlock()

	if p.roles_power == nil {
		return
	}
	return p.roles_power[role_id]
}

func calc_power_by_card(card *table_config.XmlCardItem, level int32) int32 {
	return card.BattlePower + (level-1)*card.BattlePowerGrowth/100
}

func (p *Player) role_update_suit_attr_power(role_id int32, get_suit_attr, get_power bool) int32 {
	if role_id == 0 {
		return 0
	}
	var equips []int32
	var table_id, rank, level int32
	var o bool
	if role_id > 0 {
		equips, o = p.db.Roles.GetEquip(role_id)
		if !o {
			log.Error("Player[%v] not found role[%v], update suits failed", p.Id, role_id)
			return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
		}
		table_id, _ = p.db.Roles.GetTableId(role_id)
		rank, _ = p.db.Roles.GetRank(role_id)
		level, _ = p.db.Roles.GetLevel(role_id)
	} else {
		if p.assist_friend == nil {
			log.Error("Player[%v] Assist friend not found", p.Id)
			return -1
		}
		equips, o = p.assist_friend.db.Roles.GetEquip(p.assist_role_id)
		if !o {
			log.Error("Assist friend[%v] not found role[%v], update suits failed", p.assist_friend.Id, p.assist_role_id)
			return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
		}
		table_id, _ = p.assist_friend.db.Roles.GetTableId(p.assist_role_id)
		rank, _ = p.assist_friend.db.Roles.GetRank(p.assist_role_id)
		level, _ = p.assist_friend.db.Roles.GetLevel(p.assist_role_id)
	}

	role_tdata := card_table_mgr.GetRankCard(table_id, rank)
	if role_tdata == nil {
		log.Error("Player[%v] get role[%v] card[%v] rank[%v] not found", p.Id, role_id, table_id, rank)
		return -1
	}

	power := calc_power_by_card(role_tdata, level)
	suits := make(map[*table_config.XmlSuitItem]int32)
	for _, e := range equips {
		if e <= 0 {
			continue
		}
		equip := item_table_mgr.Get(e)
		if equip == nil {
			log.Warn("Player[%v] role[%v] equip[%v] table data not found", p.Id, role_id, e)
			continue
		}

		if get_power {
			power += equip.BattlePower
		}

		if equip.SuitId <= 0 {
			continue
		}

		suit_data := suit_table_mgr.Get(equip.SuitId)
		if suit_data == nil {
			log.Warn("Suit id[%v] not found", equip.SuitId)
			continue
		}

		sn := suits[suit_data]
		if sn == 0 {
			suits[suit_data] = 1
		} else {
			suits[suit_data] = sn + 1
		}
	}

	var mem *TeamMember
	if get_suit_attr {
		if role_id > 0 {
			mem = p.team_member_mgr[role_id]
		} else {
			mem = p.assist_member
		}
	}

	for s, n := range suits {
		attrs := s.SuitAddAttrs[n]
		if mem != nil && attrs != nil {
			mem.add_attrs(attrs)
		}
		if get_power {
			for i := int32(2); i <= n; i++ {
				pow := s.SuitPowers[i]
				if pow > 0 {
					power += pow
				}
			}
		}
	}

	if get_power {
		p.set_role_power(role_id, power)
	}

	return 1
}

func (p *Player) get_defense_team_power() (power int32) {
	team := p.db.BattleTeam.GetDefenseMembers()
	if len(team) == 0 {
		return
	}

	for _, m := range team {
		if m == 0 {
			continue
		}
		p.role_update_suit_attr_power(m, false, true)
		power += p.get_role_power(m)
	}
	return
}

func (p *Player) role_displace(group_id, role_id int32) int32 {
	group := hero_convert_table_mgr.GetGroup(group_id)
	if group == nil {
		log.Error("Role Displace Group %v not found", group_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_DISPLACE_TABLE_DATA_INVALID)
	}
	role_table_id, o := p.db.Roles.GetTableId(role_id)
	if !o {
		log.Error("Player[%v] role %v not found", p.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
	}
	rank, _ := p.db.Roles.GetRank(role_id)
	card := card_table_mgr.GetRankCard(role_table_id, rank)
	if card == nil {
		log.Error("Card with table_id[%v] and rank[%v] not found", role_table_id, rank)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_TABLE_ID_NOT_FOUND)
	}
	if card.ConvertId1 != group_id && card.ConvertId2 != group_id {
		log.Error("Player[%v] role %v cant displace with group_id %v", p.Id, role_id, group_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_DISPLACE_TABLE_DATA_INVALID)
	}

	is_lock, _ := p.db.Roles.GetIsLock(role_id)
	if is_lock > 0 {
		log.Error("Player[%v] role[%v] is locked, cant decompose", p.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_IS_LOCKED)
	}

	if p.role_is_using(role_id) {
		log.Error("Player[%v] role[%v] is busy, cant decompose", p.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_IS_LOCKED)
	}

	if !p.check_resources(card.ConvertItem) {
		log.Error("Player[%v] not enough resource, cant displace role %v with group id %v", p.Id, role_id, group_id)
		return int32(msg_client_message.E_ERR_PLAYER_ITEM_NUM_NOT_ENOUGH)
	}

	r := group.TotalWeight
	var index int = -1
	for i := 0; i < len(group.HeroItems); i++ {
		if role_table_id == group.HeroItems[i].HeroId {
			index = i
			r -= group.HeroItems[i].Weight
			break
		}
	}

	var new_table_id int32
	r = rand.Int31n(r)
	for i := 0; i < len(group.HeroItems); i++ {
		if index == i {
			continue
		}
		if r < group.HeroItems[i].Weight {
			new_table_id = group.HeroItems[i].HeroId
			break
		}
		r -= group.HeroItems[i].Weight
	}

	if new_table_id == 0 {
		log.Error("Player[%v] displace role %v with group_id %v failed", p.Id, role_id, group_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_DISPLACE_FAILED)
	}

	p.db.RoleCommon.SetDisplaceRoleId(role_id)
	p.db.RoleCommon.SetDisplacedNewRoleTableId(new_table_id)
	p.db.RoleCommon.SetDisplaceGroupId(group_id)

	p.cost_resources(card.ConvertItem)

	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_DISPLACE_RESPONSE), &msg_client_message.S2CRoleDisplaceResponse{
		GroupId:        group_id,
		RoleId:         role_id,
		NewRoleTableId: new_table_id,
	})

	log.Trace("Player[%v] displace role %v and get new table id %v", p.Id, role_id, new_table_id)

	return 1
}

func (p *Player) role_displace_confirm() int32 {
	displace_role_id := p.db.RoleCommon.GetDisplaceRoleId()
	displaced_new_table_id := p.db.RoleCommon.GetDisplacedNewRoleTableId()
	group_id := p.db.RoleCommon.GetDisplaceGroupId()
	if displace_role_id == 0 || displaced_new_table_id == 0 || group_id == 0 {
		log.Error("Player[%v] no displaced role", p.Id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_DISPLACE_CONFIRM_FAILED)
	}

	is_lock, _ := p.db.Roles.GetIsLock(displace_role_id)
	if is_lock > 0 {
		log.Error("Player[%v] role[%v] is locked, cant decompose", p.Id, displace_role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_IS_LOCKED)
	}

	if p.role_is_using(displace_role_id) {
		log.Error("Player[%v] role[%v] is busy, cant decompose", p.Id, displace_role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_IS_LOCKED)
	}

	p.db.Roles.SetTableId(displace_role_id, displaced_new_table_id)
	p.db.RoleCommon.SetDisplaceRoleId(0)
	p.db.RoleCommon.SetDisplacedNewRoleTableId(0)
	p.db.RoleCommon.SetDisplaceGroupId(0)

	p.roles_id_change_info.id_update(displace_role_id)
	p.check_and_send_roles_change()

	p.Send(uint16(msg_client_message_id.MSGID_S2C_ROLE_DISPLACE_CONFIRM_RESPONSE), &msg_client_message.S2CRoleDisplaceConfirmResponse{})

	return 1
}

func C2SRoleAttrsHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleAttrsRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.send_role_attrs(req.GetRoleId())
}

func C2SRoleLockHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleLockRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.lock_role(req.GetRoleId(), req.GetIsLock())
}

func C2SRoleLevelUpHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleLevelUpRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s) !", err.Error())
		return -1
	}
	return p.levelup_role(req.GetRoleId(), req.GetUpNum())
}

func C2SRoleRankUpHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleRankUpRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s) !", err.Error())
		return -1
	}
	return p.rankup_role(req.GetRoleId())
}

func C2SRoleDecomposeHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleDecomposeRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.decompose_role(req.GetRoleIds())
}

func C2SRoleFusionHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleFusionRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.fusion_role(req.GetFusionId(), req.GetMainCardId(), [][]int32{req.GetCost1CardIds(), req.GetCost2CardIds(), req.GetCost3CardIds()})
}

func C2SRoleHandbookHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleHandbookRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}

	return p.get_role_handbook()
}

func C2SRoleLeftSlotOpenHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleLeftSlotOpenRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.role_open_left_slot(req.GetRoleId())
}

func C2SRoleOneKeyEquipHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleOneKeyEquipRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.role_one_key_equip(req.GetRoleId(), req.GetEquips())
}

func C2SRoleOneKeyUnequipHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleOnekeyUnequipRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.role_one_key_unequip(req.GetRoleId())
}

func C2SRoleLeftSlotUpgradeSaveHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleLeftSlotResultSaveRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.role_left_slot_upgrade_save()
}

func C2SRoleLeftSlotResultCancelHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleLeftSlotResultCancelRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.role_left_slot_result_cancel()
}

func C2SRoleDisplaceHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleDisplaceRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.role_displace(req.GetGroupId(), req.GetRoleId())
}

func C2SRoleDisplaceConfirmHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRoleDisplaceConfirmRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.role_displace_confirm()
}
