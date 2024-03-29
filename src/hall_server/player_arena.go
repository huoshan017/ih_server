package main

import (
	"ih_server/libs/log"
	"ih_server/libs/utils"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	"ih_server/src/table_config"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/proto"
)

const (
	ARENA_RANK_MAX = 100000
)

type ArenaRankItem struct {
	Score      int32
	UpdateTime int32
	PlayerId   int32
}

func (a *ArenaRankItem) Less(value interface{}) bool {
	item := value.(*ArenaRankItem)
	if item == nil {
		return false
	}
	if a.Score < item.Score {
		return true
	} else if a.Score == item.Score {
		if a.UpdateTime > item.UpdateTime {
			return true
		}
		if a.UpdateTime == item.UpdateTime {
			if a.PlayerId > item.PlayerId {
				return true
			}
		}
	}
	return false
}

func (a *ArenaRankItem) Greater(value interface{}) bool {
	item := value.(*ArenaRankItem)
	if item == nil {
		return false
	}
	if a.Score > item.Score {
		return true
	} else if a.Score == item.Score {
		if a.UpdateTime < item.UpdateTime {
			return true
		}
		if a.UpdateTime == item.UpdateTime {
			if a.PlayerId < item.PlayerId {
				return true
			}
		}
	}
	return false
}

func (a *ArenaRankItem) KeyEqual(value interface{}) bool {
	item := value.(*ArenaRankItem)
	if item == nil {
		return false
	}
	if item == nil {
		return false
	}
	if a.PlayerId == item.PlayerId {
		return true
	}
	return false
}

func (a *ArenaRankItem) GetKey() interface{} {
	return a.PlayerId
}

func (a *ArenaRankItem) GetValue() interface{} {
	return a.Score
}

func (a *ArenaRankItem) SetValue(value interface{}) {
	item := value.(*ArenaRankItem)
	if item == nil {
		return
	}
	a.Score = item.Score
	a.UpdateTime = item.UpdateTime
}

func (a *ArenaRankItem) New() utils.SkiplistNode {
	return &ArenaRankItem{}
}

func (a *ArenaRankItem) Assign(node utils.SkiplistNode) {
	n := node.(*ArenaRankItem)
	if n == nil {
		return
	}
	a.Score = n.Score
	a.UpdateTime = n.UpdateTime
	a.PlayerId = n.PlayerId
}

func (a *ArenaRankItem) CopyDataTo(node interface{}) {
	n := node.(*ArenaRankItem)
	if n == nil {
		return
	}
	n.Score = a.Score
	n.UpdateTime = a.UpdateTime
	n.PlayerId = a.PlayerId
}

type ArenaRobot struct {
	robot_data *table_config.XmlArenaRobotItem
	power      int32
}

func (a *ArenaRobot) Init(robot *table_config.XmlArenaRobotItem) {
	a.robot_data = robot
	a._calculate_power()
}

func (a *ArenaRobot) _calculate_power() {
	card_list := a.robot_data.RobotCardList
	if card_list == nil {
		return
	}

	for i := 0; i < len(card_list); i++ {
		for j := 0; j < len(card_list[i].EquipID); j++ {
			equip_item := item_table_mgr.Get(card_list[i].EquipID[j])
			if equip_item == nil {
				continue
			}
			a.power += equip_item.BattlePower
		}
		card := card_table_mgr.GetRankCard(card_list[i].MonsterID, card_list[i].Rank)
		if card != nil {
			a.power += calc_power_by_card(card, card_list[i].Level)
		}
	}
}

type ArenaRobotManager struct {
	robots map[int32]*ArenaRobot
}

var arena_robot_mgr ArenaRobotManager

func (a *ArenaRobotManager) Init() {
	array := arena_robot_table_mgr.Array
	if array == nil {
		return
	}

	a.robots = make(map[int32]*ArenaRobot)
	for _, r := range array {
		robot := &ArenaRobot{}
		robot.Init(r)
		a.robots[r.Id] = robot
		if r.IsExpedition == 0 {
			// 用于竞技场
			var d = ArenaRankItem{
				Score:      r.RobotScore,
				PlayerId:   r.Id,
				UpdateTime: 0,
			}
			rank_list_mgr.UpdateItem(RANK_LIST_TYPE_ARENA, &d)
		} else {
			// 用于远征
			top_power_match_manager.Update(r.Id, robot.power)
		}
	}
}

func (a *ArenaRobotManager) Get(robot_id int32) *ArenaRobot {
	return a.robots[robot_id]
}

func (a *Player) check_arena_tickets_refresh() (remain_seconds int32) {
	last_refresh := a.db.Arena.GetLastTicketsRefreshTime()
	remain_seconds = utils.GetRemainSeconds2NextDayTime(last_refresh, global_config.ArenaTicketRefreshTime)
	if remain_seconds > 0 {
		return
	}
	a.set_resource(global_config.ArenaTicketItemId, global_config.ArenaTicketsDay)
	a.db.Arena.SetLastTicketsRefreshTime(int32(time.Now().Unix()))
	return
}

func (a *Player) _update_arena_score(data *ArenaRankItem) {
	rank_list_mgr.UpdateItem(RANK_LIST_TYPE_ARENA, data)
}

func (a *Player) LoadArenaScore() {
	if a.defense_team_is_empty() {
		return
	}

	score := a.db.Arena.GetScore()
	if score <= 0 {
		return
	}
	update_time := a.db.Arena.GetUpdateScoreTime()
	if update_time == 0 {
		update_time = a.db.Info.GetLastLogin()
	}
	var data = ArenaRankItem{
		Score:      score,
		UpdateTime: update_time,
		PlayerId:   a.Id,
	}

	a._update_arena_score(&data)
}

func (a *Player) UpdateArenaScore(is_win bool) (score, add_score int32) {
	now_score := a.db.Arena.GetScore()
	division := arena_division_table_mgr.GetByScore(now_score)
	if division == nil {
		log.Error("Arena division table data with score[%v] is not found", now_score)
		return
	}

	if is_win {
		add_score = division.WinScore
		if a.db.Arena.IncbyRepeatedWinNum(1) >= global_config.ArenaRepeatedWinNum {
			add_score += division.WinningStreakScoreBonus
		}
		if a.db.Arena.GetRepeatedLoseNum() > 0 {
			a.db.Arena.SetRepeatedLoseNum(0)
		}
	} else {
		if a.db.Arena.GetRepeatedWinNum() > 0 {
			a.db.Arena.SetRepeatedWinNum(0)
		}
		a.db.Arena.IncbyRepeatedLoseNum(1)
		add_score = division.LoseScore
	}

	if add_score > 0 {
		now_time := int32(time.Now().Unix())
		score = a.db.Arena.IncbyScore(add_score)
		a.db.Arena.SetUpdateScoreTime(now_time)

		var data = ArenaRankItem{
			Score:      score,
			UpdateTime: now_time,
			PlayerId:   a.Id,
		}
		a._update_arena_score(&data)

		top_rank := a.db.Arena.GetHistoryTopRank()
		rank := rank_list_mgr.GetRankByKey(RANK_LIST_TYPE_ARENA, a.Id)
		if top_rank == 0 || rank < top_rank {
			a.db.Arena.SetHistoryTopRank(rank)
		}

		// 段位奖励
		new_division := arena_division_table_mgr.GetByScore(score)
		if new_division != nil && new_division.Id > division.Id {
			RealSendMail(nil, a.Id, MAIL_TYPE_SYSTEM, 1104, "", "", new_division.RewardList, new_division.Id)
			notify := &msg_client_message.S2CArenaGradeRewardNotify{
				Grade: new_division.Id,
			}
			a.Send(uint16(msg_client_message_id.MSGID_S2C_ARENA_GRADE_REWARD_NOTIFY), notify)
		}

		// 更新任务
		a.TaskUpdate(table_config.TASK_COMPLETE_TYPE_ARENA_REACH_SCORE, false, 0, add_score)
	}

	a.activitys_update(ACTIVITY_EVENT_ARENA_SCORE, func() int32 {
		if is_win {
			return 2
		} else {
			return 1
		}
	}(), 0, 0, 0)

	return
}

func (a *Player) OutputArenaRankItems(rank_start, rank_num int32) {
	rank_items, self_rank, self_value := rank_list_mgr.GetItemsByRange(RANK_LIST_TYPE_ARENA, a.Id, rank_start, rank_num)
	if rank_items == nil {
		log.Warn("Player[%v] get rank list with range[%v,%v] failed", a.Id, rank_start, rank_num)
		return
	}

	l := int32(len(rank_items))
	for rank := rank_start; rank < l; rank++ {
		item := (rank_items[rank-rank_start]).(*ArenaRankItem)
		if item == nil {
			log.Error("Player[%v] get arena rank list by rank[%v] item failed")
			continue
		}
		log.Debug("Rank: %v   Player[%v] Score[%v]", rank, item.PlayerId, item.Score)
	}

	if self_value != nil && self_rank > 0 {
		log.Debug("Player[%v] score[%v] rank[%v]", a.Id, self_value.(int32), self_rank)
	}
}

// 匹配对手
func (a *Player) MatchArenaPlayer() (player_id, player_rank int32) {
	rank := rank_list_mgr.GetRankByKey(RANK_LIST_TYPE_ARENA, a.Id)
	if rank < 0 {
		log.Error("Player[%v] get arena rank list rank failed", a.Id)
		return
	}

	var start_rank, rank_num, last_rank int32
	match_num := global_config.ArenaMatchPlayerNum
	if rank == 0 {
		start_rank, rank_num = rank_list_mgr.GetLastRankRange(RANK_LIST_TYPE_ARENA, match_num)
		if start_rank < 0 {
			log.Error("Player[%v] match arena player failed", a.Id)
			return
		}
	} else {
		last_rank, _ = rank_list_mgr.GetLastRankRange(RANK_LIST_TYPE_ARENA, 1)
		half_num := match_num / 2
		var left, right int32
		if a.db.Arena.GetRepeatedWinNum() >= global_config.ArenaRepeatedWinNum {
			right = rank - 1
			left = rank - match_num
		} else if a.db.Arena.GetRepeatedLoseNum() >= global_config.ArenaLoseRepeatedNum {
			right = rank + match_num
			left = rank + 1
		} else {
			right = rank + half_num
			left = rank - half_num
		}

		if left < 1 {
			left = 1
		}

		if right > last_rank {
			right = last_rank
		}

		if right-left < match_num {
			if left > 1 {
				left -= (match_num - (right - left))
				if left < 1 {
					left = 1
				}
			} else {
				if right < last_rank {
					right += (match_num - (right - left))
				}
				if right > last_rank {
					right = last_rank
				}
			}
		}

		start_rank = left
		rank_num = right - start_rank + 1
	}

	_, r := rand31n_from_range(start_rank, start_rank+rank_num)
	// 如果是自己
	if rank == r {
		r += 1
		if r > last_rank {
			r -= 2
		}
	}
	item := rank_list_mgr.GetItemByRank(RANK_LIST_TYPE_ARENA, r)
	if item == nil {
		log.Error("Player[%v] match arena player by random rank[%v] get empty item", a.Id, r)
		return
	}

	player_id = item.(*ArenaRankItem).PlayerId
	player_rank = r

	log.Trace("Player[%v] match arena players rank range [start:%v, num:%v], rand the rank %v, match player[%v]", a.Id, start_rank, rank_num, r, player_id)

	return
}

func (a *Player) send_arena_data() int32 {
	need_level := system_unlock_table_mgr.GetUnlockLevel("ArenaEnterLevel")
	if need_level > a.db.Info.GetLvl() {
		log.Error("Player[%v] level not enough level %v enter arena", a.Id, need_level)
		return int32(msg_client_message.E_ERR_PLAYER_LEVEL_NOT_ENOUGH)
	}

	tickets_remain := a.check_arena_tickets_refresh()
	score := a.db.Arena.GetScore()
	top_rank := a.db.Arena.GetHistoryTopRank()
	rank := rank_list_mgr.GetRankByKey(RANK_LIST_TYPE_ARENA, a.Id)
	day_remain, season_remain := arena_season_mgr.GetRemainSeconds()
	response := &msg_client_message.S2CArenaDataResponse{
		Score:                       score,
		Grade:                       arena_division_table_mgr.GetGradeByScore(score),
		Rank:                        rank,
		TopRank:                     top_rank,
		DayRemainSeconds:            day_remain,
		SeasonRemainSeconds:         season_remain,
		TicketsRefreshRemainSeconds: tickets_remain,
	}
	a.Send(uint16(msg_client_message_id.MSGID_S2C_ARENA_DATA_RESPONSE), response)
	log.Trace("Player[%v] arena data: %v", a.Id, response)
	return 1
}

func (a *Player) arena_player_defense_team(player_id int32) int32 {
	var level, head, guild_id int32
	var guild_name, player_name string
	var defense_team []int32
	var robot *ArenaRobot
	p := player_mgr.GetPlayerById(player_id)
	if p == nil {
		robot = arena_robot_mgr.Get(player_id)
		if robot == nil {
			log.Error("Player[%v] not found", player_id)
			return int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
		}
		level = robot.robot_data.RobotLevel
		head = robot.robot_data.RobotHead
		player_name = robot.robot_data.RobotName
		guild_id = 0
	} else {
		defense_team = p.db.BattleTeam.GetDefenseMembers()
		level = p.db.Info.GetLvl()
		head = p.db.Info.GetHead()
		guild_id = p.db.Guild.GetId()
		if guild_id > 0 {
			guild := guild_manager.GetGuild(guild_id)
			if guild != nil {
				guild_name = guild.GetName()
			}
		}
		player_name = p.db.GetName()
	}

	var power int32
	var team []*msg_client_message.PlayerTeamRole
	if defense_team != nil {
		for i := 0; i < len(defense_team); i++ {
			m := defense_team[i]
			if m <= 0 {
				continue
			}
			table_id, _ := p.db.Roles.GetTableId(m)
			level, _ := p.db.Roles.GetLevel(m)
			rank, _ := p.db.Roles.GetRank(m)
			team = append(team, &msg_client_message.PlayerTeamRole{
				TableId: table_id,
				Pos:     int32(i),
				Level:   level,
				Rank:    rank,
			})
		}
		power = p.get_defense_team_power()
	} else {
		for i := 0; i < len(robot.robot_data.RobotCardList); i++ {
			m := robot.robot_data.RobotCardList[i]
			if m == nil {
				continue
			}
			team = append(team, &msg_client_message.PlayerTeamRole{
				TableId: m.MonsterID,
				Pos:     m.Slot - 1,
				Level:   m.Level,
				Rank:    m.Rank,
			})
		}
		power = robot.power
	}

	response := &msg_client_message.S2CArenaPlayerDefenseTeamResponse{
		PlayerId:    player_id,
		PlayerLevel: level,
		PlayerHead:  head,
		DefenseTeam: team,
		Power:       power,
		GuildId:     guild_id,
		GuildName:   guild_name,
		PlayerName:  player_name,
	}
	a.Send(uint16(msg_client_message_id.MSGID_S2C_ARENA_PLAYER_DEFENSE_TEAM_RESPONSE), response)
	log.Trace("Player[%v] get arena player[%v] defense team[%v]", a.Id, player_id, team)
	return 1
}

func (a *Player) defense_team_is_empty() bool {
	defense_team := a.db.BattleTeam.GetDefenseMembers()
	if len(defense_team) == 0 {
		return true
	}
	for _, d := range defense_team {
		if d > 0 {
			return false
		}
	}
	return true
}

func (a *Player) arena_match() int32 {
	if a.get_resource(global_config.ArenaTicketItemId) < 1 {
		log.Error("Player[%v] match arena player failed, ticket not enough", a.Id)
		return int32(msg_client_message.E_ERR_PLAYER_ARENA_TICKETS_NOT_ENOUGH)
	}

	if a.defense_team_is_empty() {
		log.Error("Player[%v] must set defense team", a.Id)
		return int32(msg_client_message.E_ERR_PLAYER_TEAM_MEMBERS_IS_EMPTY)
	}

	rank_list := rank_list_mgr.GetRankList(RANK_LIST_TYPE_ARENA)
	if rank_list == nil {
		log.Error("rank list %v not found, arena match failed", RANK_LIST_TYPE_ARENA)
		return -1
	}

	var pid, rank int32
	if a.db.Arena.GetScore() == 0 {
		l := arena_robot_table_mgr.Array
		pid, rank = l[0].Id, rank_list.RankNum()
	} else {
		pid, rank = a.MatchArenaPlayer()
	}

	var robot *ArenaRobot
	p := player_mgr.GetPlayerById(pid)
	if p == nil {
		robot = arena_robot_mgr.Get(pid)
		if robot == nil {
			log.Error("Player[%v] matched id[%v] is not player and not robot", a.Id, pid)
			return int32(msg_client_message.E_ERR_PLAYER_ARENA_MATCH_PLAYER_FAILED)
		}
	}

	// 当前匹配到的玩家
	a.db.Arena.SetMatchedPlayerId(pid)
	a.add_resource(global_config.ArenaTicketItemId, -1)

	name, level, head, score, grade, power := GetFighterInfo(pid)
	response := &msg_client_message.S2CArenaMatchPlayerResponse{
		PlayerId:    pid,
		PlayerName:  name,
		PlayerLevel: level,
		PlayerHead:  head,
		PlayerScore: score,
		PlayerGrade: grade,
		PlayerPower: power,
		PlayerRank:  rank,
	}
	a.Send(uint16(msg_client_message_id.MSGID_S2C_ARENA_MATCH_PLAYER_RESPONSE), response)
	log.Trace("Player[%v] matched arena player[%v]", a.Id, response)
	return 1
}

func C2SArenaDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SArenaDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.send_arena_data()
}

func C2SArenaPlayerDefenseTeamHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SArenaPlayerDefenseTeamRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.arena_player_defense_team(req.GetPlayerId())
}

func C2SArenaMatchPlayerHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SArenaMatchPlayerRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.arena_match()
}

const (
	ARENA_STATE_IDLE          = iota
	ARENA_STATE_DOING         = 1
	ARENA_STATE_DAY_REWARD    = 2
	ARENA_STATE_SEASON_REWARD = 3
)

// 竞技场赛季管理
type ArenaSeasonMgr struct {
	state          int32 // 0 结束  1 开始
	start_time     int32
	day_checker    *utils.DaysTimeChecker
	season_checker *utils.DaysTimeChecker
	to_exit        int32
}

var arena_season_mgr ArenaSeasonMgr

func (a *ArenaSeasonMgr) Init() bool {
	a.day_checker = &utils.DaysTimeChecker{}
	if !a.day_checker.Init(dbc.ArenaSeason.GetRow().Data.GetLastDayResetTime(), global_config.ArenaDayResetTime, 1) {
		log.Error("ArenaSeasonMgr day checker init failed")
		return false
	}
	a.season_checker = &utils.DaysTimeChecker{}
	if !a.season_checker.Init(dbc.ArenaSeason.GetRow().Data.GetLastSeasonResetTime(), global_config.ArenaSeasonResetTime, global_config.ArenaSeasonDays) {
		log.Error("ArenaSeasonMgr season checker init failed")
		return false
	}
	return true
}

func (a *ArenaSeasonMgr) ToEnd() {
	atomic.StoreInt32(&a.to_exit, 1)
}

func (a *ArenaSeasonMgr) SeasonStart() {
	for {
		if !atomic.CompareAndSwapInt32(&a.state, ARENA_STATE_IDLE, ARENA_STATE_DOING) {
			time.Sleep(time.Second * 1)
			continue
		}
		break
	}
	a.start_time = int32(time.Now().Unix())
}

func (a *ArenaSeasonMgr) SeasonEnd() {
	atomic.StoreInt32(&a.state, ARENA_STATE_IDLE)
}

func (a *ArenaSeasonMgr) IsSeasonStart() bool {
	return atomic.LoadInt32(&a.state) == ARENA_STATE_DOING
}

func (a *ArenaSeasonMgr) IsSeasonEnd() bool {
	return atomic.LoadInt32(&a.state) == ARENA_STATE_IDLE
}

func (a *ArenaSeasonMgr) GetRemainSeconds() (day_remain int32, season_remain int32) {
	now_time := int32(time.Now().Unix())
	day_set := dbc.ArenaSeason.GetRow().Data.GetLastDayResetTime()
	if day_set == 0 {
		dbc.ArenaSeason.GetRow().Data.SetLastDayResetTime(now_time)
		day_set = now_time
	}
	season_set := dbc.ArenaSeason.GetRow().Data.GetLastSeasonResetTime()
	if season_set == 0 {
		dbc.ArenaSeason.GetRow().Data.SetLastSeasonResetTime(now_time)
		season_set = now_time
	}

	day_remain = a.day_checker.RemainSecondsToNextRefresh(day_set)
	season_remain = a.season_checker.RemainSecondsToNextRefresh(season_set)

	return
}

func (a *ArenaSeasonMgr) IsRewardArrive(now_time int32) (day_arrive, season_arrive bool) {
	day_set := dbc.ArenaSeason.GetRow().Data.GetLastDayResetTime()
	if day_set == 0 {
		dbc.ArenaSeason.GetRow().Data.SetLastDayResetTime(now_time)
		day_set = now_time
	}
	season_set := dbc.ArenaSeason.GetRow().Data.GetLastSeasonResetTime()
	if season_set == 0 {
		dbc.ArenaSeason.GetRow().Data.SetLastSeasonResetTime(now_time)
		season_set = now_time
	}
	day_arrive = a.day_checker.IsArrival(day_set)
	season_arrive = a.season_checker.IsArrival(season_set)
	return
}

func (a *ArenaSeasonMgr) Reward(typ int32) {
	rank_list := rank_list_mgr.GetRankList(RANK_LIST_TYPE_ARENA)
	if rank_list == nil {
		return
	}
	rank_num := rank_list.RankNum()
	for rank := int32(1); rank <= rank_num; rank++ {
		item := rank_list.GetItemByRank(rank)
		if item == nil {
			log.Warn("Cant found rank[%v] item in arena rank list with reset", rank)
			continue
		}
		arena_item := item.(*ArenaRankItem)
		if arena_item == nil {
			log.Warn("Arena rank[%v] item convert failed on DayReward", rank)
			continue
		}

		bonus := arena_bonus_table_mgr.GetByRank(rank)
		if bonus == nil {
			log.Warn("Arena rank[%v] item get bonus failed on DayReward", rank)
			continue
		}

		p := player_mgr.GetPlayerById(arena_item.PlayerId)
		if p == nil {
			continue
		}

		if typ == 1 {
			RealSendMail(nil, arena_item.PlayerId, MAIL_TYPE_SYSTEM, 1102, "", "", bonus.DayRewardList, rank)
		} else if typ == 2 {
			RealSendMail(nil, arena_item.PlayerId, MAIL_TYPE_SYSTEM, 1103, "", "", bonus.SeasonRewardList, rank)
		}
	}
}

func (a *ArenaSeasonMgr) Reset() {
	for {
		// 等待直到结束
		if atomic.LoadInt32(&a.state) == 1 {
			time.Sleep(time.Second * 1)
			continue
		}
		break
	}

	rank_list := rank_list_mgr.GetRankList(RANK_LIST_TYPE_ARENA)
	if rank_list == nil {
		return
	}

	now_time := int32(time.Now().Unix())
	var update_item ArenaRankItem
	rank_num := rank_list.RankNum()
	for rank := int32(1); rank <= rank_num; rank++ {
		item := rank_list.GetItemByRank(rank)
		if item == nil {
			log.Warn("Cant found rank[%v] item in arena rank list with reset", rank)
			continue
		}
		arena_item := item.(*ArenaRankItem)
		if arena_item == nil {
			log.Warn("Arena rank[%v] item convert failed", rank)
			continue
		}
		division := arena_division_table_mgr.GetByScore(arena_item.Score)
		if division == nil {
			log.Error("arena division not found by player[%v] score[%v]", arena_item.PlayerId, arena_item.Score)
			continue
		}

		p := player_mgr.GetPlayerById(arena_item.PlayerId)
		if p != nil {
			p.db.Arena.SetScore(division.NewSeasonScore)
		} else {
			robot := arena_robot_mgr.Get(arena_item.PlayerId)
			if robot != nil {
				robot.robot_data.RobotScore = division.NewSeasonScore
			}
		}
		update_item.UpdateTime = now_time - (rank_num - rank)
		update_item.PlayerId = arena_item.PlayerId
		update_item.Score = division.NewSeasonScore
		rank_list.SetValueByKey(arena_item.PlayerId, &update_item)
	}
}

func (a *ArenaSeasonMgr) Run() {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	a.SeasonStart()

	for {
		if atomic.LoadInt32(&a.to_exit) > 0 {
			break
		}
		// 检测时间
		now_time := int32(time.Now().Unix())
		day_arrive, season_arrive := a.IsRewardArrive(now_time)
		if day_arrive {
			atomic.StoreInt32(&a.state, ARENA_STATE_DAY_REWARD)
			a.Reward(1)
			dbc.ArenaSeason.GetRow().Data.SetLastDayResetTime(now_time)
			a.day_checker.ToNextTimePoint()
			atomic.StoreInt32(&a.state, ARENA_STATE_DOING)
			log.Info("Arena Day Reward")
		}

		if season_arrive {
			// 发奖
			atomic.StoreInt32(&a.state, ARENA_STATE_SEASON_REWARD)
			a.Reward(2)
			dbc.ArenaSeason.GetRow().Data.SetLastSeasonResetTime(now_time)
			a.season_checker.ToNextTimePoint()
			atomic.StoreInt32(&a.state, ARENA_STATE_IDLE)
			log.Info("Arena Season Reward")

			// 重置
			a.Reset()
			a.SeasonStart()
			time.Sleep(time.Millisecond * 500)
			continue
		}

		time.Sleep(time.Millisecond * 500)
	}
}
