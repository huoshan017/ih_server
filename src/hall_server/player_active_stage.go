package main

import (
	"ih_server/libs/log"
	"ih_server/libs/utils"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	"ih_server/src/table_config"
	_ "math/rand"
	_ "sync"
	"time"

	"github.com/golang/protobuf/proto"
)

const (
	ACTIVE_STAGE_TYPE_GOLD_CHALLENGE    = 1
	ACTIVE_STAGE_TYPE_WARRIOR_CHALLENGE = 2
	ACTIVE_STAGE_TYPE_HERO_CHALLENGE    = 3
)

var active_stage_types []int32 = []int32{
	ACTIVE_STAGE_TYPE_GOLD_CHALLENGE,
	ACTIVE_STAGE_TYPE_WARRIOR_CHALLENGE,
	ACTIVE_STAGE_TYPE_HERO_CHALLENGE,
}

func (a *Player) _get_active_stage_purchase_num() int32 {
	var purchase_num int32
	vip_info := vip_table_mgr.Get(a.db.Info.GetVipLvl())
	if vip_info != nil {
		purchase_num = vip_info.ActiveStageBuyTimes
	}
	return purchase_num
}

func (a *Player) _active_stage_get_data(t int32) *msg_client_message.ActiveStageData {
	purchase_num := a._get_active_stage_purchase_num()
	remain_num, _ := a.db.ActiveStages.GetCanChallengeNum(t)
	purchased_num, _ := a.db.ActiveStages.GetPurchasedNum(t)
	return &msg_client_message.ActiveStageData{
		StageType:             t,
		RemainChallengeNum:    remain_num,
		RemainBuyChallengeNum: purchase_num - purchased_num,
	}
}

func (a *Player) _send_active_stage_data(typ int32) {
	var datas []*msg_client_message.ActiveStageData
	if typ == 0 {
		for _, t := range active_stage_types {
			if a.db.ActiveStages.HasIndex(t) {
				datas = append(datas, a._active_stage_get_data(t))
			}
		}
	} else {
		datas = []*msg_client_message.ActiveStageData{a._active_stage_get_data(typ)}
	}

	last_refresh := a.db.ActiveStageCommon.GetLastRefreshTime()
	response := &msg_client_message.S2CActiveStageDataResponse{
		StageDatas:            datas,
		MaxChallengeNum:       global_config.ActiveStageChallengeNumOfDay,
		RemainSeconds4Refresh: utils.GetRemainSeconds2NextDayTime(last_refresh, global_config.ActiveStageRefreshTime),
		ChallengeNumPrice:     global_config.ActiveStageChallengeNumPrice,
		GetPointsDay:          a.db.FriendCommon.GetGetPointsDay(),
	}
	a.Send(uint16(msg_client_message_id.MSGID_S2C_ACTIVE_STAGE_DATA_RESPONSE), response)
	log.Trace("Player[%v] active stage data: %v", a.Id, response)
}

func (a *Player) check_active_stage_refresh(send bool) bool {
	// 固定时间点自动刷新
	if global_config.ActiveStageRefreshTime == "" {
		return false
	}

	now_time := int32(time.Now().Unix())
	last_refresh := a.db.ActiveStageCommon.GetLastRefreshTime()

	if !utils.CheckDayTimeArrival(last_refresh, global_config.ActiveStageRefreshTime) {
		return false
	}

	if a.db.ActiveStages.NumAll() == 0 {
		for _, t := range active_stage_types {
			a.db.ActiveStages.Add(&dbPlayerActiveStageData{
				Type:            t,
				CanChallengeNum: global_config.ActiveStageChallengeNumOfDay,
				PurchasedNum:    0,
			})
		}
	} else {
		for _, t := range active_stage_types {
			a.db.ActiveStages.SetCanChallengeNum(t, global_config.ActiveStageChallengeNumOfDay)
			a.db.ActiveStages.SetPurchasedNum(t, 0)
		}
	}

	a.db.ActiveStageCommon.SetGetPointsDay(0)
	a.db.ActiveStageCommon.SetWithdrawPoints(0)
	a.db.ActiveStageCommon.SetLastRefreshTime(now_time)

	if send {
		a._send_active_stage_data(0)

		notify := &msg_client_message.S2CActiveStageRefreshNotify{}
		a.Send(uint16(msg_client_message_id.MSGID_S2C_ACTIVE_STAGE_REFRESH_NOTIFY), notify)
	}

	log.Trace("Player[%v] active stage refreshed", a.Id)
	return true
}

func (a *Player) send_active_stage_data(typ int32) int32 {
	if a.check_active_stage_refresh(false) {
		return 1
	}
	a._send_active_stage_data(typ)
	return 1
}

func (a *Player) active_stage_challenge_num_purchase(typ, num int32) int32 {
	if num <= 0 {
		return -1
	}

	purchased_num, o := a.db.ActiveStages.GetPurchasedNum(typ)
	if !o {
		log.Error("Player[%v] purchase active stage challenge num with type %v invalid", a.Id, typ)
		return -1
	}

	purchase_num := a._get_active_stage_purchase_num()
	if purchase_num-purchased_num < num {
		log.Error("Player[%v] left purchase num %v for active stage type %v not enough", a.Id, purchased_num-purchase_num, typ)
		return int32(msg_client_message.E_ERR_PLAYER_ACTIVE_STAGE_PURCHASE_NUM_OUT)
	}

	diamond := a.get_resource(ITEM_RESOURCE_ID_DIAMOND)
	if diamond < global_config.ActiveStageChallengeNumPrice*num {
		log.Error("Player[%v] buy active stage challenge num failed, diamond %v not enough, need %v", a.Id, diamond, global_config.ActiveStageChallengeNumPrice)
		return int32(msg_client_message.E_ERR_PLAYER_DIAMOND_NOT_ENOUGH)
	}

	if num == 0 {
		log.Error("Player[%v] active stage challenge num cant buy with 0", a.Id)
		return -1
	}

	a.db.ActiveStages.IncbyCanChallengeNum(typ, num)
	purchased_num = a.db.ActiveStages.IncbyPurchasedNum(typ, num)
	a.add_resource(ITEM_RESOURCE_ID_DIAMOND, -global_config.ActiveStageChallengeNumPrice*num)

	response := &msg_client_message.S2CActiveStageBuyChallengeNumResponse{
		StageType:    typ,
		RemainBuyNum: purchase_num - purchased_num,
	}
	a.Send(uint16(msg_client_message_id.MSGID_S2C_ACTIVE_STAGE_BUY_CHALLENGE_NUM_RESPONSE), response)

	a._send_active_stage_data(typ)

	log.Trace("Player[%v] active stage purchased challenge num response %v", a.Id, response)

	return 1
}

// 助战友情点
func (a *Player) get_assist_points() int32 {
	curr_points := a.db.ActiveStageCommon.GetGetPointsDay()
	withdraw_points := a.db.ActiveStageCommon.GetWithdrawPoints()
	get_points := curr_points - withdraw_points
	if get_points < 0 {
		get_points = 0
	} else if get_points > 0 {
		if get_points+withdraw_points > global_config.FriendAssistPointsGetLimitDay {
			get_points = global_config.FriendAssistPointsGetLimitDay - withdraw_points
		}
	}
	log.Trace("Player[%v] assist points %v", a.Id, get_points)
	return get_points
}

// 提现助战友情点
func (a *Player) active_stage_withdraw_assist_points() int32 {
	get_points := a.get_assist_points()
	if get_points > 0 {
		a.db.ActiveStageCommon.IncbyWithdrawPoints(get_points)
		a.add_resource(global_config.FriendPointItemId, get_points)
	}
	response := &msg_client_message.S2CFriendGetAssistPointsResponse{
		GetPoints:      get_points,
		TotalGetPoints: a.db.ActiveStageCommon.GetWithdrawPoints(),
		CanGetPoints:   0,
	}
	a.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_GET_ASSIST_POINTS_RESPONSE), response)
	return 1
}

func (a *Player) active_stage_get_friends_assist_role_list() int32 {
	var roles []*msg_client_message.Role
	friend_ids := a.db.Friends.GetAllIndex()
	if len(friend_ids) > 0 {
		for i := 0; i < len(friend_ids); i++ {
			friend := player_mgr.GetPlayerById(friend_ids[i])
			if friend == nil {
				continue
			}
			role_id := friend.db.FriendCommon.GetAssistRoleId()
			if role_id == 0 || !friend.db.Roles.HasIndex(role_id) {
				continue
			}
			table_id, _ := friend.db.Roles.GetTableId(role_id)
			level, _ := friend.db.Roles.GetLevel(role_id)
			rank, _ := friend.db.Roles.GetRank(role_id)
			equips, _ := friend.db.Roles.GetEquip(role_id)
			roles = append(roles, &msg_client_message.Role{
				Id:       role_id,
				TableId:  table_id,
				Level:    level,
				Rank:     rank,
				Equips:   equips,
				PlayerId: friend_ids[i],
			})
		}
	}
	response := &msg_client_message.S2CActiveStageAssistRoleListResponse{
		Roles: roles,
	}
	a.Send(uint16(msg_client_message_id.MSGID_S2C_ACTIVE_STAGE_ASSIST_ROLE_LIST_RESPONSE), response)

	log.Trace("Player[%v] active stage get assist role list %v", a.Id, response)

	return 1
}

// 增加友情点
func (a *Player) friend_assist_add_points(points int32) bool {
	a.db.ActiveStageCommon.IncbyGetPointsDay(points)
	log.Debug("Add assist friend points %v to player %v", points, a.Id)
	return true
}

func (a *Player) fight_active_stage(active_stage_id int32) int32 {
	active_stage := active_stage_table_mgr.Get(active_stage_id)
	if active_stage == nil {
		log.Error("Active stage %v table data not found", active_stage_id)
		return -1
	}

	if active_stage.PlayerLevel > a.db.Info.GetLvl() {
		log.Error("Player[%v] fight active stage %v level %v not enough, need %v", a.Id, active_stage_id, a.db.Info.GetLvl(), active_stage.PlayerLevel)
		return int32(msg_client_message.E_ERR_PLAYER_ACTIVE_STAGE_LEVEL_NOT_ENOUGH)
	}

	stage_id := active_stage.StageId
	stage := stage_table_mgr.Get(stage_id)
	if stage == nil {
		log.Error("Active stage[%v] stage[%v] not found", active_stage_id, stage_id)
		return int32(msg_client_message.E_ERR_PLAYER_STAGE_TABLE_DATA_NOT_FOUND)
	}

	a.check_active_stage_refresh(false)

	can_num, _ := a.db.ActiveStages.GetCanChallengeNum(active_stage.Type)
	if can_num <= 0 {
		log.Error("Player[%v] active stage challenge num used out", a.Id)
		return -1
	}

	err, is_win, my_team, target_team, my_artifact_id, target_artifact_id, enter_reports, rounds, _ := a.FightInStage(4, stage, nil, nil)
	if err < 0 {
		log.Error("Player[%v] fight active stage %v failed", a.Id, active_stage_id)
		return err
	}

	if is_win {
		a.db.ActiveStages.IncbyCanChallengeNum(active_stage.Type, -1)
		a.send_stage_reward(stage.RewardList, 4, 0)
	}

	member_damages := a.active_stage_team.common_data.members_damage
	member_cures := a.active_stage_team.common_data.members_cure
	var assist_friend_id int32
	if a.assist_friend != nil {
		if is_win {
			a.assist_friend.friend_assist_add_points(global_config.FriendAssistPointsGet)
		}
		assist_friend_id = a.assist_friend.Id
	}
	response := &msg_client_message.S2CBattleResultResponse{
		IsWin:               is_win,
		MyTeam:              my_team,
		TargetTeam:          target_team,
		EnterReports:        enter_reports,
		Rounds:              rounds,
		MyMemberDamages:     member_damages[a.active_stage_team.side],
		TargetMemberDamages: member_damages[a.target_stage_team.side],
		MyMemberCures:       member_cures[a.active_stage_team.side],
		TargetMemberCures:   member_cures[a.target_stage_team.side],
		BattleType:          4,
		BattleParam:         active_stage_id,
		AssistFriendId:      assist_friend_id,
		AssistRoleId:        a.assist_role_id,
		AssistPos:           a.assist_role_pos,
		MyArtifactId:        my_artifact_id,
		TargetArtifactId:    target_artifact_id,
	}
	a.Send(uint16(msg_client_message_id.MSGID_S2C_BATTLE_RESULT_RESPONSE), response)

	if is_win {
		// 更新任务
		a.TaskUpdate(table_config.TASK_COMPLETE_TYPE_ACTIVE_STAGE_WIN_NUM, false, 0, 1)
	}

	//Output_S2CBattleResult(a, response)

	log.Trace("Player %v fight active stage %v", a.Id, active_stage_id)

	return 1
}

func C2SActiveStageDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SActiveStageDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.send_active_stage_data(req.GetStageType())
}

func C2SActiveStageBuyChallengeNumHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SActiveStageBuyChallengeNumRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.active_stage_challenge_num_purchase(req.GetStageType(), req.GetNum())
}

func C2SActiveStageGetAssistRoleListHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SActiveStageAssistRoleListRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.active_stage_get_friends_assist_role_list()
}
