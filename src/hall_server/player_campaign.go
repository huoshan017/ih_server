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

// 下一关
func get_next_campaign_id(campaign_id int32) int32 {
	campaign := campaign_table_mgr.Get(campaign_id)
	if campaign == nil {
		return 0
	}
	return campaign.UnlockMap
}

/*
// 获得关卡章节和难度
func get_campaign_chapter_and_difficulty(campaign_id int32) (int32, int32) {
	campaign := campaign_table_mgr.Get(campaign_id)
	if campaign == nil {
		return 0, 0
	}
	return campaign.ChapterMap, campaign.Difficulty
}
*/

// 获取stage_id
func get_stage_by_campaign(campaign_id int32) *table_config.XmlPassItem {
	campaign := campaign_table_mgr.Get(campaign_id)
	if campaign == nil {
		log.Error("战役[%v]找不到", campaign_id)
		return nil
	}
	return stage_table_mgr.Get(campaign.StageId)
}

// 是否解锁下一难度
/*
func (p *Player) is_unlock_next_difficulty(curr_campaign_id int32) (bool, int32) {
	campaign := campaign_table_mgr.Get(curr_campaign_id)
	if campaign == nil {
		return false, 0
	}

	campaign_ids := campaign_table_mgr.GetDifficultyCampaign(campaign.Difficulty)
	if campaign_ids == nil || len(campaign_ids) == 0 {
		return false, 0
	}

	for i := 0; i < len(campaign_ids); i++ {
		if !p.db.Campaigns.HasIndex(campaign_ids[i]) {
			return false, 0
		}
	}

	if curr_campaign_id != campaign_ids[len(campaign_ids)-1] {
		return false, 0
	}

	next_campaign := campaign_table_mgr.Get(campaign.UnlockMap)
	if next_campaign == nil {
		return false, 0
	}

	return true, next_campaign.Difficulty
}*/

func (p *Player) _update_campaign_rank_data(campaign_id, update_time int32) {
	var data = PlayerInt32RankItem{
		Value:      campaign_id,
		UpdateTime: update_time,
		PlayerId:   p.Id,
	}
	rank_list_mgr.UpdateItem(RANK_LIST_TYPE_CAMPAIGN, &data)
}

func (p *Player) LoadCampaignRankData() {
	campaign_id := p.db.CampaignCommon.GetLastestPassedCampaignId()
	if campaign_id <= 0 {
		return
	}
	update_time := p.db.CampaignCommon.GetPassCampaginTime()
	if update_time == 0 {
		update_time = p.db.Info.GetLastLogin()
	}
	p._update_campaign_rank_data(campaign_id, update_time)
}

func (p *Player) FightInStage(stage_type int32, stage *table_config.XmlPassItem, friend *Player, guild *dbGuildRow) (err int32, is_win bool, my_team, target_team []*msg_client_message.BattleMemberItem, my_artifact_id, target_artifact_id int32, enter_reports []*msg_client_message.BattleReportItem, rounds []*msg_client_message.BattleRoundReports, has_next_wave bool) {
	var attack_team *BattleTeam
	var team_type int32
	if stage_type == 1 {
		// PVP竞技场
		if p.attack_team == nil {
			p.attack_team = &BattleTeam{}
		}
		attack_team = p.attack_team
		team_type = BATTLE_TEAM_ATTACK
	} else if stage_type == 2 {
		// PVE战役
		if p.campaign_team == nil {
			p.campaign_team = &BattleTeam{}
		}
		attack_team = p.campaign_team
		team_type = BATTLE_TEAM_CAMPAIN
	} else if stage_type == 3 {
		// 爬塔
		if p.tower_team == nil {
			p.tower_team = &BattleTeam{}
		}
		attack_team = p.tower_team
		team_type = BATTLE_TEAM_TOWER
	} else if stage_type == 4 {
		// 活动副本，助战角色
		if p.active_stage_team == nil {
			p.active_stage_team = &BattleTeam{}
		}
		attack_team = p.active_stage_team
		team_type = BATTLE_TEAM_ACTIVE_STAGE
	} else if stage_type == 5 {
		// 好友BOSS
		if p.friend_boss_team == nil {
			p.friend_boss_team = &BattleTeam{}
		}
		attack_team = p.friend_boss_team
		team_type = BATTLE_TEAM_FRIEND_BOSS
	} else if stage_type == 6 || stage_type == 7 {
		// 探索副本
		if p.explore_team == nil {
			p.explore_team = &BattleTeam{}
		}
		attack_team = p.explore_team
		team_type = BATTLE_TEAM_EXPLORE
	} else if stage_type == 9 {
		// 公会副本
		if p.guild_stage_team == nil {
			p.guild_stage_team = &BattleTeam{}
		}
		attack_team = p.guild_stage_team
		team_type = BATTLE_TEAM_GUILD_STAGE
	} else {
		err = int32(msg_client_message.E_ERR_PLAYER_TEAM_TYPE_INVALID)
		log.Error("Stage type %v invalid", stage_type)
		return
	}

	if p.target_stage_team == nil {
		p.target_stage_team = &BattleTeam{}
	}

	// 新的关卡初始化
	if stage.Id != p.stage_id {
		p.stage_wave = 0
		err = attack_team.Init(p, team_type, 0)
		if err < 0 {
			log.Error("Player[%v] init attack team failed", p.Id)
			return
		}
	} else {
		if p.stage_wave == 0 {
			err = attack_team.Init(p, team_type, 0)
			if err < 0 {
				log.Error("Player[%v] init attack team failed", p.Id)
				return
			}
		}
	}

	self_member_num := attack_team.MembersNum()
	if p.assist_role_id > 0 && p.assist_role_id >= 0 {
		self_member_num = self_member_num - 1
	}

	if stage.PlayerCardMax > 0 && self_member_num > stage.PlayerCardMax {
		log.Error("Player[%v] fight stage %v is limited with member num", p.Id, stage.Id)
		err = int32(msg_client_message.E_ERR_PLAYER_STAGE_ROLE_NUM_LIMITED)
		return
	}

	if !p.target_stage_team.InitWithStage(1, stage.Id, p.stage_wave, friend, guild) {
		err = -1
		log.Error("Player[%v] init stage[%v] wave[%v] team failed", p.Id, stage.Id, p.stage_wave)
		return
	}

	my_team = attack_team._format_members_for_msg()
	target_team = p.target_stage_team._format_members_for_msg()
	if attack_team.artifact != nil && attack_team.artifact.artifact != nil {
		my_artifact_id = attack_team.artifact.artifact.ClientIndex
	}
	if p.target_stage_team.artifact != nil && p.target_stage_team.artifact.artifact != nil {
		target_artifact_id = p.target_stage_team.artifact.artifact.ClientIndex
	}

	// 扫荡状态
	if p.sweep_num > 0 {
		attack_team.is_sweeping = true
		p.target_stage_team.is_sweeping = true
	}

	is_win, enter_reports, rounds = attack_team.Fight(p.target_stage_team, BATTLE_END_BY_ROUND_OVER, stage.MaxRound)

	// 清除扫荡状态
	if attack_team.is_sweeping {
		attack_team.is_sweeping = false
	}
	if p.target_stage_team.is_sweeping {
		p.target_stage_team.is_sweeping = false
	}

	p.stage_id = stage.Id
	p.stage_wave += 1
	if p.stage_wave >= stage.MaxWaves {
		p.stage_wave = 0
	} else {
		has_next_wave = true
	}

	err = 1

	return
}

func (p *Player) send_stage_reward(rewards []int32, reward_type int32, income_remain_seconds int32) {
	if len(rewards) == 0 {
		return
	}

	var item_rewards []*msg_client_message.ItemInfo
	// 奖励
	for i := 0; i < len(rewards)/2; i++ {
		item_id := rewards[2*i]
		item_num := rewards[2*i+1]
		p.add_resource(item_id, item_num)
		item_rewards = append(item_rewards, &msg_client_message.ItemInfo{
			Id:    item_id,
			Value: item_num,
		})
	}
	p._send_stage_reward(item_rewards, reward_type, income_remain_seconds)
}

func (p *Player) _send_stage_reward(item_rewards []*msg_client_message.ItemInfo, reward_type int32, income_remain_seconds int32) {
	response := &msg_client_message.S2CCampaignHangupIncomeResponse{
		Rewards:                   item_rewards,
		IncomeType:                reward_type,
		HangupIncomeRemainSeconds: income_remain_seconds,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_CAMPAIGN_HANGUP_INCOME_RESPONSE), response)
}

func (p *Player) FightInCampaign(campaign_id int32) int32 {
	stage := get_stage_by_campaign(campaign_id)
	if stage == nil {
		log.Error("Cant found stage by campaign[%v]", campaign_id)
		return int32(msg_client_message.E_ERR_PLAYER_NOT_FOUND_CAMPAIGN_TABLE_DATA)
	}

	if p.db.Campaigns.HasIndex(campaign_id) {
		log.Error("Player[%v] already fight campaign[%v]", p.Id, campaign_id)
		return int32(msg_client_message.E_ERR_PLAYER_ALREADY_FIGHT_CAMPAIGN)
	}

	current_campaign_id := p.db.CampaignCommon.GetCurrentCampaignId()
	if current_campaign_id == 0 {
		if campaign_id != campaign_table_mgr.Array[0].Id {
			log.Error("Player[%v] fight first campaign[%v] invalid", p.Id, campaign_id)
			return -1
		}
	} else if current_campaign_id != campaign_id {
		log.Error("Player[%v] fight campaign[%v] cant allow", p.Id, campaign_id)
		return int32(msg_client_message.E_ERR_PLAYER_CAMPAIGN_MUST_PlAY_NEXT)
	}

	err, is_win, my_team, target_team, my_artifact_id, target_artifact_id, enter_reports, rounds, has_next_wave := p.FightInStage(2, stage, nil, nil)
	if err < 0 {
		log.Error("Player[%v] fight campaign %v failed, err %v", p.Id, campaign_id, err)
		return err
	}

	next_campaign_id := int32(0)
	if is_win && !has_next_wave {
		p.db.Campaigns.Add(&dbPlayerCampaignData{
			CampaignId: campaign_id,
		})
		next_campaign_id = get_next_campaign_id(campaign_id)
		p.db.CampaignCommon.SetCurrentCampaignId(next_campaign_id)
		// 产生剧情探索任务
		campaign := campaign_table_mgr.Get(campaign_id)
		if campaign != nil && campaign.CampaignTask > 0 {
			p.explore_gen_story_task(campaign.CampaignTask)
		}
	} else {
		p.db.CampaignCommon.SetCurrentCampaignId(campaign_id)
	}

	member_damages := p.campaign_team.common_data.members_damage
	member_cures := p.campaign_team.common_data.members_cure
	response := &msg_client_message.S2CBattleResultResponse{
		IsWin:               is_win,
		EnterReports:        enter_reports,
		Rounds:              rounds,
		MyTeam:              my_team,
		TargetTeam:          target_team,
		MyMemberDamages:     member_damages[p.campaign_team.side],
		TargetMemberDamages: member_damages[p.target_stage_team.side],
		MyMemberCures:       member_cures[p.campaign_team.side],
		TargetMemberCures:   member_cures[p.target_stage_team.side],
		HasNextWave:         has_next_wave,
		NextCampaignId:      next_campaign_id,
		BattleType:          2,
		BattleParam:         campaign_id,
		MyArtifactId:        my_artifact_id,
		TargetArtifactId:    target_artifact_id,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_BATTLE_RESULT_RESPONSE), response)

	if is_win && !has_next_wave {
		p.db.CampaignCommon.SetLastestPassedCampaignId(campaign_id)
		now_time := int32(time.Now().Unix())
		p.db.CampaignCommon.SetPassCampaginTime(now_time)
		p.send_stage_reward(stage.RewardList, 2, 0)
		// 更新排名
		p._update_campaign_rank_data(campaign_id, now_time)
		// 更新任务 通过章节
		p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_PASS_CAMPAIGN, false, campaign_id, 1)
	}

	Output_S2CBattleResult(p, response)

	log.Trace("Player %v fight campaign stage %v", p.Id, campaign_id)
	return 1
}

// 设置挂机战役关卡
func (p *Player) set_hangup_campaign_id(campaign_id int32) int32 {
	hangup_id := p.db.CampaignCommon.GetHangupCampaignId()
	if hangup_id == 0 {
		if campaign_id != campaign_table_mgr.Array[0].Id {
			return int32(msg_client_message.E_ERR_PLAYER_CANT_FIGHT_THE_CAMPAIGN)
		}
	} else if campaign_id != hangup_id {
		//if !p.db.Campaigns.HasIndex(campaign_id) {
		current_campaign_id := p.db.CampaignCommon.GetCurrentCampaignId()
		//next_campaign_id := get_next_campaign_id(current_campaign_id)
		if current_campaign_id < campaign_id {
			return int32(msg_client_message.E_ERR_PLAYER_CAMPAIGN_MUST_PlAY_NEXT)
		}

		// 关卡完成就结算一次挂机收益
		if hangup_id != 0 {
			p.campaign_hangup_income_get(0, true)
			p.campaign_hangup_income_get(1, true)
		}
		//}
	}

	// 设置挂机开始时间
	now_time := int32(time.Now().Unix())
	if hangup_id == 0 {
		p.db.CampaignCommon.SetHangupLastDropStaticIncomeTime(now_time)
		p.db.CampaignCommon.SetHangupLastDropRandomIncomeTime(now_time)
	}
	p.db.CampaignCommon.SetHangupCampaignId(campaign_id)

	return 1
}

func (p *Player) campaign_cache_static_income(item_id, item_num int32) *msg_client_message.ItemInfo {
	if !p.db.CampaignStaticIncomes.HasIndex(item_id) {
		p.db.CampaignStaticIncomes.Add(&dbPlayerCampaignStaticIncomeData{
			ItemId:  item_id,
			ItemNum: item_num,
		})
	} else {
		p.db.CampaignStaticIncomes.IncbyItemNum(item_id, item_num)
	}

	item_num, _ = p.db.CampaignStaticIncomes.GetItemNum(item_id)
	return &msg_client_message.ItemInfo{
		Id:    item_id,
		Value: item_num,
	}
}

func (p *Player) campaign_get_static_income(campaign *table_config.XmlCampaignItem, last_time, now_time int32, is_cache bool) (incomes []*msg_client_message.ItemInfo, correct_secs int32) {
	st := now_time - last_time
	correct_secs = (st % campaign.StaticRewardSec)
	var tmp_cache_items map[int32]int32

	// 固定掉落
	n := st / campaign.StaticRewardSec
	for i := 0; i < len(campaign.StaticRewardItem)/2; i++ {
		item_id := campaign.StaticRewardItem[2*i]
		item_num := n * campaign.StaticRewardItem[2*i+1]
		if is_cache {
			income := p.campaign_cache_static_income(item_id, item_num)
			incomes = append(incomes, income)
		} else {
			if tmp_cache_items == nil {
				tmp_cache_items = make(map[int32]int32)
			}
			d := tmp_cache_items[item_id]
			tmp_cache_items[item_id] = d + item_num
		}
	}

	if !is_cache {
		cache := p.db.CampaignStaticIncomes.GetAllIndex()
		if cache != nil {
			for i := 0; i < len(cache); i++ {
				n, _ := p.db.CampaignStaticIncomes.GetItemNum(cache[i])
				d := tmp_cache_items[cache[i]]
				tmp_cache_items[cache[i]] = d + n
			}
			p.db.CampaignStaticIncomes.Clear()
		}
		for k, v := range tmp_cache_items {
			if p.add_resource(k, v) {
				incomes = append(incomes, &msg_client_message.ItemInfo{
					Id:    k,
					Value: v,
				})
			}
		}
	}

	return
}

func (p *Player) campaign_has_random_income() bool {
	campaign := campaign_table_mgr.Get(p.db.CampaignCommon.GetHangupCampaignId())
	if campaign == nil {
		return false
	}

	random_income_time := p.db.CampaignCommon.GetHangupLastDropRandomIncomeTime()
	now_time := int32(time.Now().Unix())
	if now_time-random_income_time >= campaign.RandomDropSec {
		return true
	}

	if p.db.CampaignRandomIncomes.NumAll() > 0 {
		return true
	}

	return false
}

func (p *Player) campaign_cache_random_income(item_id, item_num int32) {
	if !p.db.CampaignRandomIncomes.HasIndex(item_id) {
		p.db.CampaignRandomIncomes.Add(&dbPlayerCampaignRandomIncomeData{
			ItemId:  item_id,
			ItemNum: item_num,
		})
	} else {
		p.db.CampaignRandomIncomes.IncbyItemNum(item_id, item_num)
	}
}

func (p *Player) campaign_get_random_income(campaign *table_config.XmlCampaignItem, last_time, now_time int32, is_cache bool) (has_income bool, incomes []*msg_client_message.ItemInfo, correct_secs int32) {
	rt := now_time - last_time
	correct_secs = rt % campaign.RandomDropSec
	// 随机掉落
	rand.Seed(time.Now().Unix())
	var tmp_cache_items = make(map[int32]int32)
	n := rt / campaign.RandomDropSec
	for k := 0; k < int(n); k++ {
		for i := 0; i < len(campaign.RandomDropIDList)/2; i++ {
			group_id := campaign.RandomDropIDList[2*i]
			count := campaign.RandomDropIDList[2*i+1]
			for j := 0; j < int(count); j++ {
				p.drop_item_by_id(group_id, false, nil, tmp_cache_items)
			}
		}
	}

	log.Debug("now_time: %v   last_time: %v   rt: %v   n: %v   tmp_cache_items: %v", now_time, last_time, rt, n, tmp_cache_items)

	if !is_cache {
		// 缓存的收益
		cache := p.db.CampaignRandomIncomes.GetAllIndex()
		if cache != nil {
			for i := 0; i < len(cache); i++ {
				n, _ := p.db.CampaignRandomIncomes.GetItemNum(cache[i])

				d := tmp_cache_items[cache[i]]
				tmp_cache_items[cache[i]] = d + n
			}
			p.db.CampaignRandomIncomes.Clear()
		}

		for k, v := range tmp_cache_items {
			if p.add_resource(k, v) {
				incomes = append(incomes, &msg_client_message.ItemInfo{
					Id:    k,
					Value: v,
				})
				has_income = true
			}
		}
	} else {
		for k, v := range tmp_cache_items {
			p.campaign_cache_random_income(k, v)
		}

		if p.db.CampaignRandomIncomes.NumAll() > 0 {
			has_income = true
		}
	}

	return
}

// 关卡挂机收益
func (p *Player) campaign_hangup_income_get(income_type int32, is_cache bool) (incomes []*msg_client_message.ItemInfo, income_remain_seconds int32) {
	hangup_id := p.db.CampaignCommon.GetHangupCampaignId()
	if hangup_id == 0 {
		return
	}

	campaign := campaign_table_mgr.Get(hangup_id)
	if campaign == nil {
		return
	}

	now_time := int32(time.Now().Unix())
	last_logout := p.db.Info.GetLastLogout()
	var has_income bool
	if income_type == 0 {
		static_income_time := p.db.CampaignCommon.GetHangupLastDropStaticIncomeTime()
		var cs int32
		if last_logout > 0 && last_logout >= static_income_time && now_time-last_logout >= 8*3600 {
			incomes, cs = p.campaign_get_static_income(campaign, static_income_time, last_logout+8*3600, is_cache)
		} else {
			incomes, cs = p.campaign_get_static_income(campaign, static_income_time, now_time, is_cache)
		}

		p.db.CampaignCommon.SetHangupLastDropStaticIncomeTime(now_time - cs)
		income_remain_seconds = campaign.RandomDropSec - cs
	} else {
		random_income_time := p.db.CampaignCommon.GetHangupLastDropRandomIncomeTime()
		var cr int32
		if last_logout > 0 && last_logout >= random_income_time && now_time-last_logout >= 8*3600 {
			has_income, incomes, cr = p.campaign_get_random_income(campaign, random_income_time, last_logout+8*3600, is_cache)
		} else {
			has_income, incomes, cr = p.campaign_get_random_income(campaign, random_income_time, now_time, is_cache)
		}

		p.db.CampaignCommon.SetHangupLastDropRandomIncomeTime(now_time - cr)
		income_remain_seconds = campaign.RandomDropSec - cr
	}

	if has_income || (len(incomes) > 0) {
		income_remain_seconds = 0
	}

	if !is_cache {
		p._send_stage_reward(incomes, income_type, income_remain_seconds)
		if incomes != nil {
			// 更新任务
			p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_HUANG_UP_NUM, false, 0, 1)
			log.Debug("Player[%v] hangup %v incomes: %v", p.Id, income_type, incomes)
		}
	}

	return
}

func (p *dbPlayerCampaignColumn) GetPassedCampaignIds() []int32 {
	p.m_row.m_lock.UnSafeRLock("dbPlayerCampaignColumn.GetPassedCampaignIds")
	defer p.m_row.m_lock.UnSafeRUnlock()

	var ids []int32
	for id := range p.m_data {
		ids = append(ids, id)
	}
	return ids
}

func (p *Player) send_campaigns() {
	incomes, _ := p.campaign_hangup_income_get(0, true)
	_, remain_seconds := p.campaign_hangup_income_get(1, true)
	passed_ids := p.db.Campaigns.GetPassedCampaignIds()
	response := &msg_client_message.S2CCampaignDataResponse{}
	response.PassedCampaignIds = passed_ids
	response.UnlockCampaignId = p.db.CampaignCommon.GetCurrentCampaignId()
	response.HangupCampaignId = p.db.CampaignCommon.GetHangupCampaignId()
	response.StaticIncomes = incomes
	response.IncomeRemainSeconds = remain_seconds
	response.AccelerateRefreshRemainSeconds, response.RemainAccelerateNum, response.AccelerateCostDiamond = p.campaign_check_accel_refresh()
	p.Send(uint16(msg_client_message_id.MSGID_S2C_CAMPAIGN_DATA_RESPONSE), response)

	log.Trace("Player[%v] campaign data %v", p.Id, response)
}

func (p *Player) campaign_check_accel_refresh() (remain_refresh_seconds, remain_accel_num, next_cost_diamond int32) {
	last_refresh := p.db.CampaignCommon.GetVipAccelRefreshTime()
	remain_refresh_seconds = utils.GetRemainSeconds2NextDayTime(last_refresh, "00:00:00")
	if remain_refresh_seconds <= 0 {
		p.db.CampaignCommon.SetVipAccelNum(0)
		now_time := time.Now()
		p.db.CampaignCommon.SetVipAccelRefreshTime(int32(now_time.Unix()))
		remain_refresh_seconds = utils.GetRemainSeconds2NextDayTime(int32(now_time.Unix()), "00:00:00")
	}
	accel_num := p.db.CampaignCommon.GetVipAccelNum()
	vip_info := vip_table_mgr.Get(p.db.Info.GetVipLvl())
	if vip_info != nil {
		remain_accel_num = vip_info.AccelTimes - accel_num
	}
	accel_info := accel_cost_table_mgr.Get(accel_num + 1)
	if accel_info != nil {
		next_cost_diamond = accel_info.Cost
	}
	return
}

func (p *Player) campaign_accel_get_income() int32 {
	p.campaign_check_accel_refresh()

	lvl := p.db.Info.GetVipLvl()
	vip_info := vip_table_mgr.Get(lvl)
	if vip_info == nil {
		log.Error("Player[%v] vip level %v not found in vip table", p.Id, lvl)
		return -1
	}

	accel_num := p.db.CampaignCommon.GetVipAccelNum()
	if accel_num >= vip_info.AccelTimes {
		log.Error("Player[%v] vip level %v accelerate campaign income num %v used out", p.Id, lvl, vip_info.AccelTimes)
		return -1
	}

	accel_info := accel_cost_table_mgr.Get(accel_num + 1)
	if accel_info == nil {
		log.Error("AccelCost table data with accel num %v not found", accel_num)
		return -1
	}

	if p.get_diamond() < accel_info.Cost {
		log.Error("Player[%v] accelerate get campagin income not enough diamond", p.Id)
		return int32(msg_client_message.E_ERR_PLAYER_DIAMOND_NOT_ENOUGH_FOR_ACCEL)
	}

	hungup_id := p.db.CampaignCommon.GetHangupCampaignId()
	campaign := campaign_table_mgr.Get(hungup_id)
	if campaign == nil {
		log.Error("Player %v hung up campagin %v table data not found", p.Id, hungup_id)
		return int32(msg_client_message.E_ERR_PLAYER_NOT_FOUND_CAMPAIGN_TABLE_DATA)
	}

	var incomes map[int32]int32 = make(map[int32]int32)
	// 固定掉落
	n := 2 * 3600 / campaign.StaticRewardSec
	for i := 0; i < len(campaign.StaticRewardItem)/2; i++ {
		item_id := campaign.StaticRewardItem[2*i]
		item_num := n * campaign.StaticRewardItem[2*i+1]
		p.add_resource(item_id, item_num)
		incomes[item_id] += item_num
	}
	// 随机掉落
	rand.Seed(time.Now().Unix() + time.Now().UnixNano())
	n = 2 * 3600 / campaign.RandomDropSec
	for k := 0; k < int(n); k++ {
		for i := 0; i < len(campaign.RandomDropIDList)/2; i++ {
			group_id := campaign.RandomDropIDList[2*i]
			count := campaign.RandomDropIDList[2*i+1]
			for j := 0; j < int(count); j++ {
				if o, item := p.drop_item_by_id(group_id, false, nil, nil); o && item != nil {
					p.add_resource(item.GetId(), item.GetValue())
					incomes[item.GetId()] += item.GetValue()
				}
			}
		}
	}

	p.add_diamond(-accel_info.Cost)
	accel_num = p.db.CampaignCommon.IncbyVipAccelNum(1)

	var next_cost_diamond int32
	accel_info = accel_cost_table_mgr.Get(accel_num)
	if accel_info != nil {
		next_cost_diamond = accel_info.Cost
	}

	response := &msg_client_message.S2CCampaignAccelerateIncomeResponse{
		Incomes:         Map2ItemInfos(incomes),
		RemainNum:       vip_info.AccelTimes - accel_num,
		NextCostDiamond: next_cost_diamond,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_CAMPAIGN_ACCELERATE_INCOME_RESPONSE), response)

	log.Trace("Player[%v] accelerate campagin %v income response %v", p.Id, hungup_id, response)

	return 1
}

func (p *Player) campaign_accel_num_refresh() int32 {
	p.campaign_check_accel_refresh()

	vip_lvl := p.db.Info.GetVipLvl()
	vip_info := vip_table_mgr.Get(vip_lvl)
	if vip_info == nil {
		log.Error("Player[%v] vip level %v not found in vip table", p.Id, vip_lvl)
		return -1
	}

	if p.get_diamond() < global_config.AccelHungupRefreshCostDiamond {
		log.Error("Player[%v] not enough diamond to refresh accel hungup", p.Id)
		return -1
	}

	if p.db.CampaignCommon.GetVipAccelNum() == 0 {
		log.Error("Player[%v] no need to refresh campaign accel num", p.Id)
		return -1
	}

	p.db.CampaignCommon.SetVipAccelNum(0)
	p.add_diamond(-global_config.AccelHungupRefreshCostDiamond)

	response := &msg_client_message.S2CCampaignAccelerateRefreshResponse{
		RemainNum: vip_info.AccelTimes,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_CAMPAIGN_ACCELERATE_REFRESH_RESPONSE), response)

	log.Trace("Player[%v] refreshed hungup accel num", p.Id)

	return 1
}

func C2SCampaignAccelGetIncomeHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SCampaignAccelerateIncomeRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.campaign_accel_get_income()
}

func C2SCampaignAccelNumRefreshHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SCampaignAccelerateRefreshRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.campaign_accel_num_refresh()
}

func C2SSetHangupCampaignHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SBattleSetHangupCampaignRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s) !", err.Error())
		return -1
	}

	res := p.set_hangup_campaign_id(req.GetCampaignId())
	if res < 0 {
		log.Warn("Player[%v] set hangup campaign %v failed[%v]", p.Id, req.GetCampaignId(), res)
		return res
	}

	response := &msg_client_message.S2CBattleSetHangupCampaignResponse{}
	response.CampaignId = req.GetCampaignId()
	p.Send(uint16(msg_client_message_id.MSGID_S2C_BATTLE_SET_HANGUP_CAMPAIGN_RESPONSE), response)

	log.Trace("Player[%v] set hangup campaign %v success", p.Id, req.GetCampaignId())

	return 1
}

func C2SCampaignHangupIncomeHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SCampaignHangupIncomeRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s) !", err.Error())
		return -1
	}

	t := req.GetIncomeType()
	p.campaign_hangup_income_get(t, false)
	return 1
}

func C2SCampaignDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SCampaignDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s) !", err.Error())
		return -1
	}
	p.send_campaigns()
	return 1
}
