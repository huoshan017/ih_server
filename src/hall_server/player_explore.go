package main

import (
	"ih_server/libs/log"
	"ih_server/libs/utils"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	"ih_server/src/table_config"
	"math/rand"
	"time"

	"github.com/golang/protobuf/proto"
)

const (
	START_EXPLORE_TASKS_NUM = 5
)

const (
	EXPLORE_TASK_STATE_NO_START   = 0
	EXPLORE_TASK_STATE_STARTED    = 1
	EXPLORE_TASK_STATE_COMPLETE   = 2
	EXPLORE_TASK_STATE_FIGHT_BOSS = 3
)

func (p *dbPlayerExploreColumn) has_reward() bool {
	p.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.has_reward")
	defer p.m_row.m_lock.UnSafeRUnlock()

	for _, d := range p.m_data {
		if d.State == EXPLORE_TASK_STATE_COMPLETE || d.State == EXPLORE_TASK_STATE_FIGHT_BOSS {
			return true
		}
	}

	return false
}

func (p *dbPlayerExploreStoryColumn) has_reward() bool {
	p.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.has_reward")
	defer p.m_row.m_lock.UnSafeRUnlock()

	for _, d := range p.m_data {
		if d.State == EXPLORE_TASK_STATE_COMPLETE || d.State == EXPLORE_TASK_STATE_FIGHT_BOSS {
			return true
		}
	}
	return false
}

func (p *Player) check_explore_tasks_refresh(is_notify bool) (refresh bool) {
	last_refresh := p.db.ExploreCommon.GetLastRefreshTime()
	if !utils.CheckDayTimeArrival(last_refresh, global_config.ExploreTaskRefreshTime) {
		return
	}

	p.explore_random_task(true)
	now_time := int32(time.Now().Unix())
	p.db.ExploreCommon.SetLastRefreshTime(now_time)

	if is_notify {
		response := &msg_client_message.S2CExploreDataResponse{
			Datas:                p.explore_format_tasks(),
			StoryDatas:           p.explore_story_format_tasks(),
			RefreshRemainSeconds: utils.GetRemainSeconds2NextDayTime(now_time, global_config.ExploreTaskRefreshTime),
		}
		p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_DATA_RESPONSE), response)

		log.Trace("Player[%v] explore data refreshed: %v", p.Id, response)

		notify := &msg_client_message.S2CExploreAutoRefreshNotify{}
		p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_AUTO_REFRESH_NOTIFY), notify)
		log.Trace("Player[%v] explore tasks auto refreshed", p.Id)
	}

	refresh = true
	return
}

func (p *Player) is_explore_task_can_refresh(id int32, is_auto bool) bool {
	if !p.db.Explores.HasIndex(id) {
		return false
	}
	start_time, _ := p.db.Explores.GetStartTime(id)
	if start_time > 0 {
		return false
	}
	is_lock, _ := p.db.Explores.GetIsLock(id)
	if is_lock > 0 {
		return false
	}
	if is_auto {
		state, _ := p.db.Explores.GetState(id)
		if state != 0 {
			return false
		}
	}
	return true
}

func (p *Player) is_explore_task_locked(id int32) bool {
	is_lock, _ := p.db.Explores.GetIsLock(id)
	return is_lock > 0
}

func (p *Player) _explore_gen_task_data(etask *table_config.XmlSearchTaskItem) (camps, types []int32, roleid4task, nameid4task int32, random_rewards []int32) {
	if etask.CardCampNumCond > 0 {
		camps = randn_different(etask.CardCampCond, etask.CardCampNumCond)
	}
	if etask.CardTypeNumCond > 0 {
		types = randn_different(etask.CardTypeCond, etask.CardTypeNumCond)
	}
	if etask.TaskHeroNameList != nil && len(etask.TaskHeroNameList) > 0 {
		r := rand.Int31n(int32(len(etask.TaskHeroNameList)))
		roleid4task = etask.TaskHeroNameList[r]
	}
	if etask.TaskNameList != nil && len(etask.TaskNameList) > 0 {
		r := rand.Int31n(int32(len(etask.TaskNameList)))
		nameid4task = etask.TaskNameList[r]
	}
	if etask.RandomReward > 0 {
		o, item := p.drop_item_by_id(etask.RandomReward, false, nil, nil)
		if !o {
			log.Error("Player[%v] get explore task %v reward by drop id failed", p.Id, etask.Id)
			return
		}
		random_rewards = []int32{item.Id, item.Value}
	}
	return
}

func (p *Player) _explore_rand_task_data() (etask *table_config.XmlSearchTaskItem, camps, types []int32, roleid4task, nameid4task int32, random_rewards []int32) {
	etask = explore_task_mgr.RandomTask()
	if etask == nil {
		log.Error("random task failed")
		return
	}
	camps, types, roleid4task, nameid4task, random_rewards = p._explore_gen_task_data(etask)
	return
}

func (p *Player) explore_rand_one_task(id int32, is_new bool) (data *msg_client_message.ExploreData) {
	etask, camps, types, roleid4task, nameid4task, random_rewards := p._explore_rand_task_data()
	if is_new {
		p.db.Explores.Add(&dbPlayerExploreData{
			Id:               id,
			TaskId:           etask.Id,
			RoleCampsCanSel:  camps,
			RoleTypesCanSel:  types,
			RoleId4TaskTitle: roleid4task,
			NameId4TaskTitle: nameid4task,
			RandomRewards:    random_rewards,
		})
	} else {
		p.db.Explores.SetStartTime(id, 0)
		p.db.Explores.SetState(id, EXPLORE_TASK_STATE_NO_START)
		p.db.Explores.SetRoleCampsCanSel(id, camps)
		p.db.Explores.SetRoleTypesCanSel(id, types)
		p.db.Explores.SetTaskId(id, etask.Id)
		p.db.Explores.SetRoleId4TaskTitle(id, roleid4task)
		p.db.Explores.SetNameId4TaskTitle(id, nameid4task)
		p.db.Explores.SetRoleIds(id, nil)
		p.db.Explores.SetIsLock(id, 0)
		p.db.Explores.SetRandomRewards(id, random_rewards)
	}
	data = &msg_client_message.ExploreData{
		Id:              id,
		TaskId:          etask.Id,
		RoleCampsCanSel: camps,
		RoleTypesCanSel: types,
		RoleId4Title:    roleid4task,
		NameId4Title:    nameid4task,
		RemainSeconds:   etask.SearchTime,
		RandomRewards:   random_rewards,
	}
	return
}

func (p *Player) explore_gen_story_task(task_id int32) {
	etask := explore_task_mgr.Get(task_id)
	if etask == nil {
		return
	}
	camps, types, _, _, random_rewards := p._explore_gen_task_data(etask)
	p.db.ExploreStorys.Add(&dbPlayerExploreStoryData{
		TaskId:          task_id,
		RoleCampsCanSel: camps,
		RoleTypesCanSel: types,
		RandomRewards:   random_rewards,
	})
	notify := &msg_client_message.S2CExploreStoryNewNotify{
		TaskId: task_id,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_STORY_NEW_NOTIFY), notify)
	log.Trace("Player[%v] gen new explore story task %v", p.Id, task_id)
}

func (p *Player) explore_random_task(is_auto bool) (datas []*msg_client_message.ExploreData) {
	var need_random_num int32
	vip_info := vip_table_mgr.Get(p.db.Info.GetVipLvl())
	if vip_info != nil {
		need_random_num = vip_info.SearchTaskCount
	} else {
		need_random_num = START_EXPLORE_TASKS_NUM
	}
	rand.Seed(time.Now().Unix())
	if p.db.Explores.NumAll() > 0 {
		all := p.db.Explores.GetAllIndex()
		if all != nil {
			for i := 0; i < len(all); i++ {
				id := all[i]
				if !p.is_explore_task_can_refresh(id, is_auto) {
					if is_auto && p.is_explore_task_locked(id) {
						need_random_num -= 1
						if need_random_num <= 0 {
							break
						}
					}
					continue
				}

				d := p.explore_rand_one_task(id, false)
				datas = append(datas, d)

				if is_auto {
					need_random_num -= 1
					if need_random_num <= 0 {
						break
					}
				}
			}
		}
	}

	if is_auto {
		for i := int32(0); i < need_random_num; i++ {
			id := p.db.ExploreCommon.IncbyCurrentId(1)
			data := p.explore_rand_one_task(id, true)
			datas = append(datas, data)
		}
	}

	return
}

func (p *Player) explore_format_tasks() (tasks []*msg_client_message.ExploreData) {
	now_time := int32(time.Now().Unix())
	all := p.db.Explores.GetAllIndex()
	for i := 0; i < len(all); i++ {
		id := all[i]
		task_id, _ := p.db.Explores.GetTaskId(id)
		task := explore_task_mgr.Get(task_id)
		if task == nil {
			p.db.Explores.Remove(id)
			log.Error("Player[%v] explore task[%v] not found with table id %v", p.Id, id, task_id)
			continue
		}

		d := &msg_client_message.ExploreData{}
		d.Id = id
		d.TaskId = task_id
		start_time, _ := p.db.Explores.GetStartTime(id)
		state, _ := p.db.Explores.GetState(id)
		if state < EXPLORE_TASK_STATE_COMPLETE {
			if start_time == 0 {
				d.State = EXPLORE_TASK_STATE_NO_START
				d.RemainSeconds = task.SearchTime
				d.RoleCampsCanSel, _ = p.db.Explores.GetRoleCampsCanSel(id)
				d.RoleTypesCanSel, _ = p.db.Explores.GetRoleTypesCanSel(id)
			} else if now_time-start_time >= task.SearchTime {
				d.State = EXPLORE_TASK_STATE_COMPLETE
				d.RemainSeconds = 0
				d.RoleIds, _ = p.db.Explores.GetRoleIds(id)
			} else {
				d.State = EXPLORE_TASK_STATE_STARTED
				d.RemainSeconds = task.SearchTime - (now_time - start_time)
				d.RoleIds, _ = p.db.Explores.GetRoleIds(id)
			}
		} else {
			d.State = state
			d.RoleIds, _ = p.db.Explores.GetRoleIds(id)
		}
		d.RoleId4Title, _ = p.db.Explores.GetRoleId4TaskTitle(id)
		d.NameId4Title, _ = p.db.Explores.GetNameId4TaskTitle(id)
		d.RandomRewards, _ = p.db.Explores.GetRandomRewards(id)
		d.RewardStageId, _ = p.db.Explores.GetRewardStageId(id)
		is_lock, _ := p.db.Explores.GetIsLock(id)
		if is_lock > 0 {
			d.IsLock = true
		}
		p.db.Explores.SetState(id, d.State)
		tasks = append(tasks, d)
	}
	return
}

func (p *Player) explore_story_format_tasks() (story_tasks []*msg_client_message.ExploreData) {
	now_time := int32(time.Now().Unix())
	all := p.db.ExploreStorys.GetAllIndex()
	for i := 0; i < len(all); i++ {
		task_id := all[i]
		task := explore_task_mgr.Get(task_id)
		if task == nil {
			p.db.ExploreStorys.Remove(task_id)
			log.Error("Player[%v] explore story task[%v] not found", p.Id, task_id)
			continue
		}
		d := &msg_client_message.ExploreData{}
		d.TaskId = task_id
		d.Id = task_id
		start_time, _ := p.db.ExploreStorys.GetStartTime(task_id)
		if start_time == 0 {
			d.State = EXPLORE_TASK_STATE_NO_START
			d.RemainSeconds = task.SearchTime
			d.RoleCampsCanSel, _ = p.db.ExploreStorys.GetRoleCampsCanSel(task_id)
			d.RoleTypesCanSel, _ = p.db.ExploreStorys.GetRoleTypesCanSel(task_id)
		} else {
			d.RoleIds, _ = p.db.ExploreStorys.GetRoleIds(task_id)
			d.State, _ = p.db.ExploreStorys.GetState(task_id)
			if d.State < EXPLORE_TASK_STATE_COMPLETE {
				if now_time-start_time >= task.SearchTime {
					d.State = EXPLORE_TASK_STATE_COMPLETE
				} else {
					d.State = EXPLORE_TASK_STATE_STARTED
					d.RemainSeconds = task.SearchTime - (now_time - start_time)
				}
			}
		}
		d.RandomRewards, _ = p.db.ExploreStorys.GetRandomRewards(task_id)
		d.RewardStageId, _ = p.db.ExploreStorys.GetRewardStageId(task_id)
		p.db.ExploreStorys.SetState(task_id, d.State)
		story_tasks = append(story_tasks, d)
	}
	return
}

func (p *Player) send_explore_data() int32 {
	need_level := system_unlock_table_mgr.GetUnlockLevel("SearchTaskEnterLevel")
	if need_level > p.db.Info.GetLvl() {
		log.Warn("Player[%v] level not enough level %v enter explore", p.Id, need_level)
		return int32(msg_client_message.E_ERR_PLAYER_LEVEL_NOT_ENOUGH)
	}
	p.check_explore_tasks_refresh(false)
	tasks := p.explore_format_tasks()
	story_tasks := p.explore_story_format_tasks()
	response := &msg_client_message.S2CExploreDataResponse{
		Datas:                tasks,
		StoryDatas:           story_tasks,
		RefreshRemainSeconds: utils.GetRemainSeconds2NextDayTime(p.db.ExploreCommon.GetLastRefreshTime(), global_config.ExploreTaskRefreshTime),
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_DATA_RESPONSE), response)

	log.Trace("Player[%v] explore datas %v", p.Id, response)

	return 1
}

func (p *Player) is_explore_task_has_role(id, role_id int32) bool {
	if !p.db.Explores.HasIndex(id) {
		return false
	}
	role_ids, _ := p.db.Explores.GetRoleIds(id)
	if len(role_ids) == 0 {
		return false
	}

	for i := 0; i < len(role_ids); i++ {
		if role_id == role_ids[i] {
			return true
		}
	}

	return false
}

func (p *Player) explore_one_key_sel_role(id int32, is_story bool) (role_ids []int32) {
	all_roles := p.db.Roles.GetAllIndex()
	if len(all_roles) == 0 {
		return
	}

	var camps, types []int32
	var task *table_config.XmlSearchTaskItem
	if is_story {
		task = explore_task_mgr.Get(id)
		camps, _ = p.db.ExploreStorys.GetRoleCampsCanSel(id)
		types, _ = p.db.ExploreStorys.GetRoleTypesCanSel(id)
	} else {
		task_id, _ := p.db.Explores.GetTaskId(id)
		task = explore_task_mgr.Get(task_id)
		camps, _ = p.db.Explores.GetRoleCampsCanSel(id)
		types, _ = p.db.Explores.GetRoleTypesCanSel(id)
	}

	if task == nil {
		log.Error("Explore task[%v] table data not found", id)
		return
	}

	for i := 0; i < len(all_roles); i++ {
		role_id := all_roles[i]
		table_id, _ := p.db.Roles.GetTableId(role_id)
		rank, _ := p.db.Roles.GetRank(role_id)
		card := card_table_mgr.GetRankCard(table_id, rank)
		if card == nil {
			log.Error("Player[%v] role[%v] card[%v] table data not found", p.Id, role_id, table_id)
			return nil
		}

		// star cond
		if card.Rarity < task.CardStarCond {
			continue
		}

		// camp cond
		if len(camps) > 0 {
			n := 0
			for ; n < len(camps); n++ {
				if card.Camp == camps[n] {
					break
				}
			}
			if n >= len(camps) {
				continue
			}
		}

		// type cond
		if len(types) > 0 {
			n := 0
			for ; n < len(types); n++ {
				if card.Type == types[n] {
					break
				}
			}
			if n >= len(types) {
				continue
			}
		}

		role_ids = append(role_ids, role_id)
		if len(role_ids) >= int(task.CardNum) {
			break
		}
	}

	return
}

func (p *Player) explore_sel_role(id int32, is_story bool, role_ids []int32) int32 {
	if p.check_explore_tasks_refresh(true) {
		return 1
	}

	var task_id int32
	if is_story {
		task_id = id
	} else {
		task_id, _ = p.db.Explores.GetTaskId(id)
	}

	task := explore_task_mgr.Get(task_id)
	if task == nil {
		log.Error("Player[%v] explore task[%v] table data not found", p.Id, task_id)
		return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_TABLE_DATA_NOT_FOUND)
	}

	if len(role_ids) == 0 {
		log.Error("Player[%v] explore sel role is empty", p.Id)
		return -1
	}

	// 判断有无重复ID
	for i := 0; i < len(role_ids); i++ {
		if role_ids[i] == 0 {
			log.Error("Player[%v] explore task %v sel role with role_id")
			return -1
		}

		for j := i + 1; j < len(role_ids); j++ {
			if role_ids[i] == role_ids[j] {
				log.Error("Player[%v] set explore task[%v] roles have same role id", p.Id, id)
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_SEL_ROLES_CANT_SAME)
			}
		}
	}

	if is_story {
		if !p.db.ExploreStorys.HasIndex(id) {
			log.Error("Player[%v] no explore story task %v", p.Id, id)
			return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_USER_DATA_NOT_FOUND)
		}
	} else {
		if !p.db.Explores.HasIndex(id) {
			log.Error("Player[%v] no such explore task %v", p.Id, id)
			return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_USER_DATA_NOT_FOUND)
		}
	}

	var start_time int32
	if is_story {
		start_time, _ = p.db.ExploreStorys.GetStartTime(id)
	} else {
		start_time, _ = p.db.Explores.GetStartTime(id)
	}

	if start_time > 0 {
		log.Error("Player[%v] explore task %v already start, cant set roles", p.Id, id)
		return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_ALREADY_STARTED)
	}

	if len(role_ids) < int(task.CardNum) {
		log.Error("Player[%v] set explore task[%v] role ids %v not enough, need %v ", p.Id, id, role_ids, task.CardNum)
		return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_SEL_ROLE_NOT_ENOUGH)
	}

	var camps, types []int32
	if is_story {
		camps, _ = p.db.ExploreStorys.GetRoleCampsCanSel(id)
		types, _ = p.db.ExploreStorys.GetRoleTypesCanSel(id)
	} else {
		camps, _ = p.db.Explores.GetRoleCampsCanSel(id)
		types, _ = p.db.Explores.GetRoleTypesCanSel(id)
	}

	var role_tdatas []*table_config.XmlCardItem
	var star_reach bool
	for i := 0; i < len(role_ids); i++ {
		role_id := role_ids[i]
		if !p.db.Roles.HasIndex(role_id) {
			log.Error("Player[%v] role[%v] not found", p.Id, role_id)
			return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
		}

		state, _ := p.db.Roles.GetState(role_id)

		// 状态判断
		if !(state == ROLE_STATE_NONE || (state == ROLE_STATE_EXPLORE && p.is_explore_task_has_role(id, role_id))) {
			return int32(msg_client_message.E_ERR_PLAYER_ROLE_IS_LOCKED)
		}

		table_id, _ := p.db.Roles.GetTableId(role_id)
		rank, _ := p.db.Roles.GetRank(role_id)
		role_tdata := card_table_mgr.GetRankCard(table_id, rank)
		if role_tdata == nil {
			return int32(msg_client_message.E_ERR_PLAYER_FUSION_ROLE_TABLE_DATA_NOT_FOUND)
		}

		if role_tdata.Rarity >= task.CardStarCond {
			star_reach = true
		}

		role_tdatas = append(role_tdatas, role_tdata)
	}

	if !star_reach {
		log.Error("Player[%v] explore sel roles %v rarity %v not be satisfied", p.Id, role_ids, task.CardStarCond)
		return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_SEL_ROLE_STAR_NOT_ENOUGH)
	}

	if len(camps) > 0 {
		for i := 0; i < len(role_tdatas); i++ {
			role_tdata := role_tdatas[i]
			for j := 0; j < len(camps); j++ {
				if role_tdata.Camp == camps[j] {
					camps[j] = 0
					break
				}
			}
		}
		for i := 0; i < len(camps); i++ {
			if camps[i] > 0 {
				log.Error("Player[%v] sel roles %v to explore task[%v] failed, camp[%v] not be satisfied", p.Id, role_ids, task_id, camps[i])
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_SEL_ROLE_CAMP_INVALID)
			}
		}
	}

	if len(types) > 0 {
		for i := 0; i < len(role_tdatas); i++ {
			role_tdata := role_tdatas[i]
			for j := 0; j < len(types); j++ {
				if role_tdata.Type == types[j] {
					types[j] = 0
					break
				}
			}
		}
		for i := 0; i < len(types); i++ {
			if types[i] > 0 {
				log.Error("Player[%v] sel roles %v to explore task[%v] failed, type[%v] not be satisfied", p.Id, role_ids, task_id, types[i])
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_SEL_ROLE_TYPE_INVALID)
			}
		}
	}

	var old_ids []int32
	if is_story {
		old_ids, _ = p.db.ExploreStorys.GetRoleIds(id)
	} else {
		old_ids, _ = p.db.Explores.GetRoleIds(id)
	}

	for i := 0; i < len(role_ids); i++ {
		role_id := role_ids[i]
		if old_ids != nil && i < len(old_ids) {
			if old_ids[i] != role_id {
				p.db.Roles.SetState(old_ids[i], ROLE_STATE_NONE)
				p.roles_id_change_info.id_update(old_ids[i])
			}
		}
		p.db.Roles.SetState(role_id, ROLE_STATE_EXPLORE)
		p.roles_id_change_info.id_update(role_id)
	}

	if is_story {
		p.db.ExploreStorys.SetRoleIds(id, role_ids)
	} else {
		p.db.Explores.SetRoleIds(id, role_ids)
	}

	response := &msg_client_message.S2CExploreSelRoleResponse{
		Id:      id,
		RoleIds: role_ids,
		IsStory: is_story,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_SEL_ROLE_RESPONSE), response)

	log.Trace("Player[%v] set explore task %v roles %v, is story: %v", p.Id, id, role_ids, is_story)

	return 1
}

func (p *Player) _explore_roles_is_enough(task *table_config.XmlSearchTaskItem, role_ids []int32) bool {
	var role_len int32
	if role_ids != nil {
		role_len = int32(len(role_ids))
	}
	if role_len < task.CardNum {
		log.Error("Player[%v] start explore task %v failed with role num not enough", p.Id, task.Id)
		return false
	}
	return true
}

func (p *Player) explore_task_start(ids []int32, is_story bool) int32 {
	if p.check_explore_tasks_refresh(true) {
		return 1
	}

	if len(ids) == 0 {
		return -1
	}

	now_time := int32(time.Now().Unix())
	if is_story {
		for i := 0; i < len(ids); i++ {
			id := ids[i]
			if !p.db.ExploreStorys.HasIndex(id) {
				log.Error("Player[%v] no explore story task %v", p.Id, id)
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_USER_DATA_NOT_FOUND)
			}
			start_time, _ := p.db.ExploreStorys.GetStartTime(id)
			if start_time > 0 {
				log.Error("Player[%v] explore story task %v already start", p.Id, id)
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_ALREADY_STARTED)
			}

			task := explore_task_mgr.Get(id)
			if task == nil {
				log.Error("Explore story task %v table data not found", id)
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_TABLE_DATA_NOT_FOUND)
			}

			role_ids, _ := p.db.ExploreStorys.GetRoleIds(id)
			if !p._explore_roles_is_enough(task, role_ids) {
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_SEL_ROLE_NOT_ENOUGH)
			}
		}
		for i := 0; i < len(ids); i++ {
			id := ids[i]
			p.db.ExploreStorys.SetStartTime(id, now_time)
			p.db.ExploreStorys.SetState(id, EXPLORE_TASK_STATE_STARTED)
		}
	} else {
		for i := 0; i < len(ids); i++ {
			id := ids[i]
			if !p.db.Explores.HasIndex(id) {
				log.Error("Player[%v] no explore task[%v]", p.Id, id)
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_USER_DATA_NOT_FOUND)
			}

			start_time, _ := p.db.Explores.GetStartTime(id)
			if start_time > 0 {
				log.Error("Player[%v] explore task[%v] already start", p.Id, id)
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_ALREADY_STARTED)
			}

			task_id, _ := p.db.Explores.GetTaskId(id)
			task := explore_task_mgr.Get(task_id)
			if task == nil {
				log.Error("Explore story task %v table data not found", id)
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_TABLE_DATA_NOT_FOUND)
			}

			role_ids, _ := p.db.Explores.GetRoleIds(id)
			if !p._explore_roles_is_enough(task, role_ids) {
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_SEL_ROLE_NOT_ENOUGH)
			}
		}
		for i := 0; i < len(ids); i++ {
			id := ids[i]
			p.db.Explores.SetStartTime(id, now_time)
			p.db.Explores.SetState(id, EXPLORE_TASK_STATE_STARTED)
			p.db.Explores.SetIsLock(id, 1)
		}
	}

	response := &msg_client_message.S2CExploreStartResponse{
		Ids:     ids,
		IsStory: is_story,
		IsLock:  true,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_START_RESPONSE), response)

	log.Trace("Player[%v] explore tasks %v start", p.Id, ids)

	return 1
}

func (p *Player) explore_remove_roles(id int32, is_story bool) bool {
	var ids []int32
	var has bool
	if is_story {
		ids, has = p.db.ExploreStorys.GetRoleIds(id)
		p.db.ExploreStorys.SetRoleIds(id, nil)
	} else {
		ids, has = p.db.Explores.GetRoleIds(id)
		p.db.Explores.SetRoleIds(id, nil)
	}
	if has && ids != nil {
		for _, role_id := range ids {
			p.db.Roles.SetState(role_id, ROLE_STATE_NONE)
			p.roles_id_change_info.id_update(role_id)
		}
	}
	return has
}

func (p *Player) explore_remove_task(id int32, is_story bool) (has bool) {
	has = p.explore_remove_roles(id, is_story)
	if is_story {
		p.db.ExploreStorys.Remove(id)
	} else {
		p.db.Explores.Remove(id)
	}
	if has {
		notify := &msg_client_message.S2CExploreRemoveNotify{
			Id:      id,
			IsStory: is_story,
		}
		p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_REMOVE_NOTIFY), notify)
	}
	return
}

func (p *Player) explore_speedup(ids []int32, is_story bool) int32 {
	if p.check_explore_tasks_refresh(true) {
		return 1
	}

	var task *table_config.XmlSearchTaskItem
	var cost_diamond int32
	var state int32
	for i := 0; i < len(ids); i++ {
		id := ids[i]
		if is_story {
			if !p.db.ExploreStorys.HasIndex(id) {
				log.Error("Player[%v] no explore story task %v", p.Id, id)
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_USER_DATA_NOT_FOUND)
			}
			state, _ = p.db.ExploreStorys.GetState(id)
			task = explore_task_mgr.Get(id)
		} else {
			if !p.db.Explores.HasIndex(id) {
				log.Error("Player[%v] no explore task %v", p.Id, id)
				return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_USER_DATA_NOT_FOUND)
			}
			state, _ = p.db.Explores.GetState(id)
			task_id, _ := p.db.Explores.GetTaskId(id)
			task = explore_task_mgr.Get(task_id)
		}

		if state != EXPLORE_TASK_STATE_STARTED {
			log.Error("Player[%v] explore task[%v] state[%v] not doing, cant speed up", p.Id, id, state)
			return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_STATE_NOT_STARTED)
		}

		cost_diamond += task.AccelCost
	}

	if cost_diamond > p.get_diamond() {
		log.Error("Player[%v] speed up explore task not enough diamond, need %v, now %v", p.Id, cost_diamond, p.get_diamond())
		return int32(msg_client_message.E_ERR_PLAYER_DIAMOND_NOT_ENOUGH)
	}

	p.add_diamond(-cost_diamond)

	for i := 0; i < len(ids); i++ {
		id := ids[i]
		if is_story {
			p.db.ExploreStorys.SetState(id, EXPLORE_TASK_STATE_COMPLETE)
		} else {
			p.db.Explores.SetState(id, EXPLORE_TASK_STATE_COMPLETE)
		}
	}

	response := &msg_client_message.S2CExploreSpeedupResponse{
		Ids:         ids,
		IsStory:     is_story,
		CostDiamond: cost_diamond,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_SPEEDUP_RESPONSE), response)

	log.Trace("Player[%v] explore task %v (is_story: %v) speed up, cost diamond %v", p.Id, ids, is_story, cost_diamond)

	return 1
}

func (p *Player) get_explore_refresh_cost_diamond() (cost int32) {
	all := p.db.Explores.GetAllIndex()
	if len(all) == 0 {
		return
	}

	for i := 0; i < len(all); i++ {
		id := all[i]
		if !p.is_explore_task_can_refresh(id, false) {
			continue
		}
		cost += global_config.ExploreTaskRefreshCostDiamond
	}
	return
}

func (p *Player) explore_tasks_refresh() int32 {
	if p.check_explore_tasks_refresh(true) {
		return 1
	}

	cost_diamond := p.get_explore_refresh_cost_diamond()
	if p.get_diamond() < cost_diamond {
		log.Error("Player[%v] refresh explore tasks need diamond %v, but only diamond %v", p.Id, cost_diamond, p.get_diamond())
		return int32(msg_client_message.E_ERR_PLAYER_DIAMOND_NOT_ENOUGH)
	}

	datas := p.explore_random_task(false)

	p.add_diamond(-cost_diamond)

	response := &msg_client_message.S2CExploreRefreshResponse{
		Datas: datas,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_REFRESH_RESPONSE), response)

	log.Trace("Player[%v] explore task refreshed, cost diamond %v, tasks %v", p.Id, cost_diamond, datas)

	return 1
}

func (p *Player) explore_task_lock(ids []int32, is_lock bool) int32 {
	if p.check_explore_tasks_refresh(true) {
		return 1
	}

	if len(ids) == 0 {
		return -1
	}

	for i := 0; i < len(ids); i++ {
		id := ids[i]
		if !p.db.Explores.HasIndex(id) {
			log.Error("Player[%v] no explore task %v", p.Id, id)
			return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_USER_DATA_NOT_FOUND)
		}
		state, _ := p.db.Explores.GetState(id)
		if !is_lock && state > 0 {
			log.Error("Player[%v] explore task %v is start, cant unlock", p.Id, id)
			return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_CANT_UNLOCK_IF_STARTED)
		}
	}

	for i := 0; i < len(ids); i++ {
		id := ids[i]
		lock, _ := p.db.Explores.GetIsLock(id)
		if lock > 0 && !is_lock {
			p.db.Explores.SetIsLock(id, 0)
		} else if lock == 0 && is_lock {
			p.db.Explores.SetIsLock(id, 1)
		}
	}

	response := &msg_client_message.S2CExploreLockResponse{
		Ids:    ids,
		IsLock: is_lock,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_LOCK_RESPONSE), response)

	log.Trace("Player[%v] explore task %v lock %v", p.Id, ids, is_lock)

	return 1
}

func (p *Player) _explore_get_task_state(id int32, task *table_config.XmlSearchTaskItem, state, start_time int32) int32 {
	if state == EXPLORE_TASK_STATE_STARTED {
		now_time := int32(time.Now().Unix())
		if now_time-start_time >= task.SearchTime {
			state = EXPLORE_TASK_STATE_COMPLETE
			if task.Type == table_config.EXPLORE_TASK_TYPE_RANDOM {
				p.db.Explores.SetState(id, state)
			} else {
				p.db.ExploreStorys.SetState(id, state)
			}
		}
	}
	return state
}

func (p *Player) explore_get_reward(id int32, is_story bool) int32 {
	if p.check_explore_tasks_refresh(true) {
		return 1
	}

	var task *table_config.XmlSearchTaskItem
	var state int32
	var random_rewards []int32
	if is_story {
		if !p.db.ExploreStorys.HasIndex(id) {
			log.Error("Player[%v] no explore story task %v", p.Id, id)
			return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_USER_DATA_NOT_FOUND)
		}
		task = explore_task_mgr.Get(id)
		if task == nil {
			log.Error("Explore story task %v table data not found", id)
			return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_TABLE_DATA_NOT_FOUND)
		}
		old_state, _ := p.db.ExploreStorys.GetState(id)
		start_time, _ := p.db.ExploreStorys.GetStartTime(id)
		random_rewards, _ = p.db.ExploreStorys.GetRandomRewards(id)
		state = p._explore_get_task_state(id, task, old_state, start_time)
	} else {
		if !p.db.Explores.HasIndex(id) {
			log.Error("Player[%v] no explore task %v", p.Id, id)
			return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_USER_DATA_NOT_FOUND)
		}

		task_id, _ := p.db.Explores.GetTaskId(id)
		task = explore_task_mgr.Get(task_id)
		if task == nil {
			log.Error("Explore task %v table data not found", task_id)
			return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_TABLE_DATA_NOT_FOUND)
		}

		old_state, _ := p.db.Explores.GetState(id)
		start_time, _ := p.db.Explores.GetStartTime(id)
		random_rewards, _ = p.db.Explores.GetRandomRewards(id)
		state = p._explore_get_task_state(id, task, old_state, start_time)
	}

	if state != EXPLORE_TASK_STATE_COMPLETE {
		log.Error("Player[%v] explore task %v start not complete, cant get reward", p.Id, id)
		return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_IS_INCOMPLETE)
	}

	// 固定收益
	if task.ConstReward != nil {
		for i := 0; i < len(task.ConstReward)/2; i++ {
			rid := task.ConstReward[2*i]
			rnum := task.ConstReward[2*i+1]
			p.add_resource(rid, rnum)
		}
	}

	// 掉落收益
	var random_items []*msg_client_message.ItemInfo
	if len(random_rewards) > 0 {
		for i := 0; i < len(random_rewards)/2; i++ {
			rid := random_rewards[2*i]
			rnum := random_rewards[2*i+1]
			p.add_resource(rid, rnum)
			random_items = append(random_items, &msg_client_message.ItemInfo{
				Id:    rid,
				Value: rnum,
			})
		}
	}

	// 触发关卡
	var reward_stage_id int32
	b := rand.Int31n(10000) < task.BonusStageChance && p.db.Info.GetLvl() >= task.BonusStageLevelCond
	if b {
		boss := explore_task_boss_mgr.Random(task.BonusStageListID)
		if boss != nil {
			if is_story {
				p.db.ExploreStorys.SetState(id, EXPLORE_TASK_STATE_FIGHT_BOSS)
				p.db.ExploreStorys.SetRewardStageId(id, boss.StageId)
			} else {
				p.db.Explores.SetState(id, EXPLORE_TASK_STATE_FIGHT_BOSS)
				p.db.Explores.SetRewardStageId(id, boss.StageId)
			}
			reward_stage_id = boss.StageId
		}
		p.explore_remove_roles(id, is_story)
	}

	response := &msg_client_message.S2CExploreGetRewardResponse{
		Id:            id,
		IsStory:       is_story,
		RandomItems:   random_items,
		RewardStageId: reward_stage_id,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_GET_REWARD_RESPONSE), response)

	if !b {
		p.explore_remove_task(id, is_story)
		// 更新任务
		p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_EXPLORE_NUM, false, 0, 1)
		p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_PASS_STAR_EXPLORE, false, task.TaskStar, 1)
	}

	if !is_story {
		p.activitys_update(ACTIVITY_EVENT_EXPLORE, task.TaskStar, 1, 0, 0)
	}

	log.Trace("Player[%v] explore task %v get reward, reward stage %v", p.Id, id, reward_stage_id)

	return 1
}

func (p *Player) explore_fight(id int32, is_story bool) int32 {
	if p.check_explore_tasks_refresh(true) {
		return 1
	}

	var task_id, state, reward_stage_id int32
	if is_story {
		task_id = id
		state, _ = p.db.ExploreStorys.GetState(id)
		reward_stage_id, _ = p.db.ExploreStorys.GetRewardStageId(id)
	} else {
		task_id, _ = p.db.Explores.GetTaskId(id)
		state, _ = p.db.Explores.GetState(id)
		reward_stage_id, _ = p.db.Explores.GetRewardStageId(id)
	}

	task := explore_task_mgr.Get(task_id)
	if task == nil {
		log.Error("Explore task %v (is_story: %v) not found", id, is_story)
		return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_TABLE_DATA_NOT_FOUND)
	}

	if state != EXPLORE_TASK_STATE_FIGHT_BOSS {
		log.Error("Player[%v] explore task %v (is_story: %v) state is not to fight boss", p.Id, state, is_story)
		return int32(msg_client_message.E_ERR_PLAYER_EXPLORE_NO_FIGHT_BOSS_STATE)
	}

	stage := stage_table_mgr.Get(reward_stage_id)
	if stage == nil {
		log.Error("explore fight stage %v not found", reward_stage_id)
		return int32(msg_client_message.E_ERR_PLAYER_STAGE_TABLE_DATA_NOT_FOUND)
	}

	var battle_type int32
	if is_story {
		battle_type = 7
	} else {
		battle_type = 6
	}
	err, is_win, my_team, target_team, my_artifact_id, target_artifact_id, enter_reports, rounds, has_next_wave := p.FightInStage(battle_type, stage, nil, nil)
	if err < 0 {
		log.Error("Player[%v] fight explore task %v (is_story: %v) failed, team is empty", p.Id, id, is_story)
		return err
	}

	if enter_reports == nil {
		enter_reports = make([]*msg_client_message.BattleReportItem, 0)
	}
	if rounds == nil {
		rounds = make([]*msg_client_message.BattleRoundReports, 0)
	}

	member_damages := p.explore_team.common_data.members_damage
	member_cures := p.explore_team.common_data.members_cure
	response := &msg_client_message.S2CBattleResultResponse{
		IsWin:               is_win,
		EnterReports:        enter_reports,
		Rounds:              rounds,
		MyTeam:              my_team,
		TargetTeam:          target_team,
		MyMemberDamages:     member_damages[p.explore_team.side],
		TargetMemberDamages: member_damages[p.target_stage_team.side],
		MyMemberCures:       member_cures[p.explore_team.side],
		TargetMemberCures:   member_cures[p.target_stage_team.side],
		HasNextWave:         has_next_wave,
		BattleType:          battle_type,
		BattleParam:         id,
		MyArtifactId:        my_artifact_id,
		TargetArtifactId:    target_artifact_id,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_BATTLE_RESULT_RESPONSE), response)

	if is_win && !has_next_wave {
		p.send_stage_reward(stage.RewardList, 6, 0)

		// 更新任务
		p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_EXPLORE_NUM, false, 0, 1)
		p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_PASS_STAR_EXPLORE, false, task.TaskStar, 1)
	}

	if !is_story || is_win {
		p.explore_remove_task(id, is_story)
	}

	log.Trace("Player %v fight explore %v, is story %v", p.Id, id, is_story)

	return 1
}

func (p *Player) explore_cancel(id int32, is_story bool) int32 {
	if p.check_explore_tasks_refresh(true) {
		return 1
	}

	if !p.explore_remove_task(id, is_story) {
		log.Error("Player[%v] explore task [%v] is_story[%v] not found, cant cancel", p.Id, id, is_story)
		return -1
	}

	response := &msg_client_message.S2CExploreCancelResponse{
		Id:      id,
		IsStory: is_story,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_EXPLORE_CANCEL_RESPONSE), response)

	return 1
}

func C2SExploreDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SExploreDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}

	return p.send_explore_data()
}

func C2SExploreSelRoleHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SExploreSelRoleRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}

	return p.explore_sel_role(req.GetId(), req.GetIsStory(), req.GetRoleIds())
}

func C2SExploreStartHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SExploreStartRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}

	return p.explore_task_start(req.GetIds(), req.GetIsStory())
}

func C2SExploreSpeedupHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SExploreStartRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}

	return p.explore_speedup(req.GetIds(), req.GetIsStory())
}

func C2SExploreTasksRefreshHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SExploreRefreshRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}

	return p.explore_tasks_refresh()
}

func C2SExploreTaskLockHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SExploreLockRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}

	return p.explore_task_lock(req.GetIds(), req.GetIsLock())
}

func C2SExploreGetRewardHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SExploreGetRewardRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}

	return p.explore_get_reward(req.GetId(), req.GetIsStory())
}

func C2SExploreCancelHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SExploreCancelRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.explore_cancel(req.GetId(), req.GetIsStory())
}
