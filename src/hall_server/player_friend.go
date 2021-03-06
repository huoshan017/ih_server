package main

import (
	"ih_server/libs/log"
	"ih_server/libs/utils"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	"ih_server/src/table_config"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/proto"
)

const MAX_FRIEND_RECOMMEND_PLAYER_NUM int32 = 10000

type FriendRecommendMgr struct {
	player_ids    map[int32]int32
	players_array []int32
	locker        *sync.RWMutex
	add_chan      chan int32
	to_end        int32
}

var friend_recommend_mgr FriendRecommendMgr

func (f *FriendRecommendMgr) Init() {
	f.player_ids = make(map[int32]int32)
	f.players_array = make([]int32, MAX_FRIEND_RECOMMEND_PLAYER_NUM)
	f.locker = &sync.RWMutex{}
	f.add_chan = make(chan int32, 10000)
	f.to_end = 0
}

func (f *FriendRecommendMgr) AddPlayer(player_id int32) {
	f.add_chan <- player_id
	log.Debug("Friend Recommend Manager to add player[%v]", player_id)
}

func (f *FriendRecommendMgr) CheckAndAddPlayer(player_id int32) bool {
	p := player_mgr.GetPlayerById(player_id)
	if p == nil {
		return false
	}

	if _, o := f.player_ids[player_id]; o {
		//log.Warn("Player[%v] already added Friend Recommend mgr", player_id)
		return false
	}

	var add_pos int32
	num := int32(len(f.player_ids))
	if num >= MAX_FRIEND_RECOMMEND_PLAYER_NUM {
		add_pos = rand.Int31n(num)
		// 删掉一个随机位置的
		delete(f.player_ids, f.players_array[add_pos])
		f.players_array[add_pos] = 0
	} else {
		add_pos = num
	}

	now_time := int32(time.Now().Unix())
	if now_time-p.db.Info.GetLastLogout() > 24*3600*2 && atomic.LoadInt32(&p.is_login) == 0 {
		return false
	}

	if p.db.Friends.NumAll() >= global_config.FriendMaxNum {
		return false
	}

	f.player_ids[player_id] = add_pos
	f.players_array[add_pos] = player_id

	//log.Debug("Friend Recommend Manager add player[%v], total count[%v], player_ids: %v, players_array: %v", player_id, len(f.player_ids), f.player_ids, f.players_array[:len(f.player_ids)])

	return true
}

func (f *FriendRecommendMgr) Run() {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	var last_check_remove_time int32
	for {
		if atomic.LoadInt32(&f.to_end) > 0 {
			break
		}
		// 处理操作队列
		is_break := false
		for !is_break {
			select {
			case player_id, ok := <-f.add_chan:
				{
					if !ok {
						log.Error("conn timer wheel op chan receive invalid !!!!!")
						return
					}
					f.CheckAndAddPlayer(player_id)
				}
			default:
				{
					is_break = true
				}
			}
		}

		now_time := int32(time.Now().Unix())
		if now_time-last_check_remove_time >= 60*10 {
			f.locker.Lock()
			player_num := len(f.player_ids)
			for i := 0; i < player_num; i++ {
				p := player_mgr.GetPlayerById(f.players_array[i])
				if p == nil {
					continue
				}
				if (now_time-p.db.Info.GetLastLogout() >= 2*24*3600 && atomic.LoadInt32(&p.is_login) == 0) || p.db.Friends.NumAll() >= global_config.FriendMaxNum {
					delete(f.player_ids, f.players_array[i])
					f.players_array[i] = f.players_array[player_num-1]
					player_num -= 1
				}
			}
			f.locker.Unlock()
			last_check_remove_time = now_time
		}

		time.Sleep(time.Second * 1)
	}
}

func (f *FriendRecommendMgr) Random(player_id int32) (ids []int32) {
	player := player_mgr.GetPlayerById(player_id)
	if player == nil {
		return
	}

	f.locker.RLock()
	defer f.locker.RUnlock()

	cnt := int32(len(f.player_ids))
	if cnt == 0 {
		return
	}

	if cnt > global_config.FriendRecommendNum {
		cnt = global_config.FriendRecommendNum
	}

	rand.Seed(time.Now().Unix() + time.Now().UnixNano())
	for i := int32(0); i < cnt; i++ {
		var pid int32
		r := rand.Int31n(int32(len(f.player_ids)))
		sr := r
		for {
			pid = f.players_array[sr]
			p := player_mgr.GetPlayerById(pid)
			has := false
			if pid == player_id || player.db.Friends.HasIndex(pid) || (p != nil && p.db.FriendAsks.HasIndex(player_id)) || player.db.FriendAsks.HasIndex(pid) {
				has = true
			} else {
				if ids != nil {
					for n := 0; n < len(ids); n++ {
						if ids[n] == pid {
							has = true
							break
						}
					}
				}
			}
			if !has {
				break
			}
			sr = (sr + 1) % int32(len(f.player_ids))
			if sr == r {
				log.Info("Friend Recommend Mgr player count[%v] not enough to random a player to recommend", len(f.player_ids))
				return
			}
		}
		if pid <= 0 {
			break
		}
		ids = append(ids, pid)
	}
	return ids
}

// ----------------------------------------------------------------------------

func (f *Player) _friend_get_give_remain_seconds(friend *Player, now_time int32) (give_remain_seconds int32) {
	last_give_time, _ := f.db.Friends.GetLastGivePointsTime(friend.Id)
	give_remain_seconds = utils.GetRemainSeconds2NextDayTime(last_give_time, global_config.FriendRefreshTime)
	return
}

func (f *Player) _friend_get_points(friend *Player, now_time int32) (get_points int32) {
	last_give_time, _ := friend.db.Friends.GetLastGivePointsTime(f.Id)
	get_points, _ = f.db.Friends.GetGetPoints(friend.Id)
	if utils.GetRemainSeconds2NextDayTime(last_give_time, global_config.FriendRefreshTime) == 0 {
		get_points = 0
	} else {
		if get_points+f.db.FriendCommon.GetGetPointsDay() > global_config.FriendPointsGetLimitDay {
			get_points = global_config.FriendPointsGetLimitDay - f.db.FriendCommon.GetGetPointsDay()
		}
	}
	return
}

func (f *Player) _check_friend_data_refresh(friend *Player, now_time int32) (give_remain_seconds, get_points int32) {
	give_remain_seconds = f._friend_get_give_remain_seconds(friend, now_time)
	get_points = f._friend_get_points(friend, now_time)
	return
}

func (f *Player) _format_friend_info(p *Player, now_time int32) (friend_info *msg_client_message.FriendInfo) {
	remain_seconds, get_points := f._check_friend_data_refresh(p, now_time)
	friend_info = &msg_client_message.FriendInfo{
		Id:                      p.Id,
		Name:                    p.db.GetName(),
		Level:                   p.db.Info.GetLvl(),
		Head:                    p.db.Info.GetHead(),
		IsOnline:                atomic.LoadInt32(&p.is_login) > 0,
		OfflineSeconds:          p._get_offline_seconds(),
		RemainGivePointsSeconds: remain_seconds,
		BossId:                  p.db.FriendCommon.GetFriendBossTableId(),
		BossHpPercent:           p.get_friend_boss_hp_percent(),
		Power:                   p.get_defense_team_power(),
		GetPoints:               get_points,
	}
	return
}

func (f *Player) _format_friends_info(friend_ids []int32) (friends_info []*msg_client_message.FriendInfo) {
	if len(friend_ids) == 0 {
		friends_info = make([]*msg_client_message.FriendInfo, 0)
	} else {
		now_time := int32(time.Now().Unix())
		for i := 0; i < len(friend_ids); i++ {
			if friend_ids[i] <= 0 {
				continue
			}
			p := player_mgr.GetPlayerById(friend_ids[i])
			if p == nil {
				continue
			}
			player := f._format_friend_info(p, now_time)
			friends_info = append(friends_info, player)
		}
	}
	return
}

func _format_players_info(player_ids []int32) (players_info []*msg_client_message.PlayerInfo) {
	if len(player_ids) == 0 {
		players_info = make([]*msg_client_message.PlayerInfo, 0)
	} else {
		for i := 0; i < len(player_ids); i++ {
			if player_ids[i] <= 0 {
				continue
			}
			p := player_mgr.GetPlayerById(player_ids[i])
			if p == nil {
				continue
			}

			player := &msg_client_message.PlayerInfo{
				Id:    player_ids[i],
				Name:  p.db.GetName(),
				Level: p.db.Info.GetLvl(),
				Head:  p.db.Info.GetHead(),
			}
			players_info = append(players_info, player)
		}
	}
	return
}

// 好友推荐列表
func (f *Player) send_recommend_friends() int32 {
	var player_ids []int32
	last_recommend_time := f.db.FriendCommon.GetLastRecommendTime()
	if last_recommend_time == 0 || utils.CheckDayTimeArrival(last_recommend_time, global_config.FriendRefreshTime) {
		player_ids = friend_recommend_mgr.Random(f.Id)
		if player_ids != nil {
			f.db.FriendRecommends.Clear()
			for i := 0; i < len(player_ids); i++ {
				f.db.FriendRecommends.Add(&dbPlayerFriendRecommendData{
					PlayerId: player_ids[i],
				})
			}
		}
		f.db.FriendCommon.SetLastRecommendTime(int32(time.Now().Unix()))
	} else {
		player_ids = f.db.FriendRecommends.GetAllIndex()
	}
	players := f._format_friends_info(player_ids)
	response := &msg_client_message.S2CFriendRecommendResponse{
		Players: players,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_RECOMMEND_RESPONSE), response)

	log.Trace("Player[%v] recommend friends %v", f.Id, response)

	return 1
}

// 好友列表
func (f *Player) send_friend_list() int32 {
	friend_ids := f.db.Friends.GetAllIndex()
	friends := f._format_friends_info(friend_ids)
	response := &msg_client_message.S2CFriendListResponse{
		Friends: friends,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_LIST_RESPONSE), response)

	log.Trace("Player[%v] friend list: %v", f.Id, response)

	return 1
}

// 检测是否好友增加
func (f *Player) check_and_send_friend_add() int32 {
	f.friend_add_locker.Lock()
	if f.friend_add == nil || len(f.friend_add) == 0 {
		f.friend_add_locker.Unlock()
		return 0
	}
	friends := f._format_friends_info(f.friend_add)
	f.friend_add = nil
	f.friend_add_locker.Unlock()

	response := &msg_client_message.S2CFriendListAddNotify{
		FriendsAdd: friends,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_LIST_ADD_NOTIFY), response)

	log.Trace("Player[%v] friend add: %v", f.Id, response)

	return 1
}

// 申请好友增加
func (f *Player) friend_ask_add_ids(player_ids []int32) {
	f.friend_ask_add_locker.Lock()
	defer f.friend_ask_add_locker.Unlock()
	f.friend_ask_add = append(f.friend_ask_add, player_ids...)
}

// 申请好友
func (f *Player) friend_ask(player_ids []int32) int32 {
	for i := 0; i < len(player_ids); i++ {
		player_id := player_ids[i]
		p := player_mgr.GetPlayerById(player_id)
		if p == nil {
			log.Error("Player[%v] not found", player_id)
			return int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
		}

		if f.db.Friends.HasIndex(player_id) {
			log.Error("Player[%v] already add player[%v] to friend", f.Id, player_id)
			return int32(msg_client_message.E_ERR_PLAYER_FRIEND_ALREADY_ADD)
		}

		if p.db.FriendAsks.HasIndex(f.Id) {
			log.Error("Player[%v] already asked player[%v] to friend", f.Id, player_id)
			return int32(msg_client_message.E_ERR_PLAYER_FRIEND_ALREADY_ASKED)
		}
	}

	for i := 0; i < len(player_ids); i++ {
		player_id := player_ids[i]
		p := player_mgr.GetPlayerById(player_id)
		if p == nil {
			continue
		}
		p.db.FriendAsks.Add(&dbPlayerFriendAskData{
			PlayerId: f.Id,
		})
		p.friend_ask_add_ids([]int32{f.Id})
		p.check_and_send_friend_ask_add()
		f.db.FriendRecommends.Remove(p.Id)
	}

	response := &msg_client_message.S2CFriendAskResponse{
		PlayerIds: player_ids,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_ASK_RESPONSE), response)

	log.Trace("Player[%v] asked players[%v] to friend", f.Id, player_ids)

	return 1
}

// 检测好友申请增加
func (f *Player) check_and_send_friend_ask_add() int32 {
	f.friend_ask_add_locker.Lock()
	if f.friend_ask_add == nil || len(f.friend_ask_add) == 0 {
		f.friend_ask_add_locker.Unlock()
		return 0
	}
	players := _format_players_info(f.friend_ask_add)
	f.friend_ask_add = nil
	f.friend_ask_add_locker.Unlock()

	response := &msg_client_message.S2CFriendAskPlayerListAddNotify{
		PlayersAdd: players,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_ASK_PLAYER_LIST_ADD_NOTIFY), response)

	log.Trace("Player[%v] checked friend ask add %v", f.Id, response)

	return 1
}

// 好友申请列表
func (f *Player) send_friend_ask_list() int32 {
	friend_ask_ids := f.db.FriendAsks.GetAllIndex()
	players := _format_players_info(friend_ask_ids)
	response := &msg_client_message.S2CFriendAskPlayerListResponse{
		Players: players,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_ASK_PLAYER_LIST_RESPONSE), response)

	log.Trace("Player[%v] friend ask list %v", f.Id, response)

	return 1
}

// 好友增加
func (f *Player) friend_add_ids(player_ids []int32) {
	f.friend_add_locker.Lock()
	defer f.friend_add_locker.Unlock()
	f.friend_add = append(f.friend_add, player_ids...)
}

// 同意加为好友
func (f *Player) agree_friend_ask(player_ids []int32) int32 {
	if len(player_ids) == 0 {
		return -1
	}

	if f.db.Friends.NumAll()+int32(len(player_ids)) > global_config.FriendMaxNum {
		return int32(msg_client_message.E_ERR_PLAYER_FRIEND_NUM_MAX)
	}

	for i := 0; i < len(player_ids); i++ {
		p := player_mgr.GetPlayerById(player_ids[i])
		if p == nil {
			log.Error("Player[%v] not found on agree friend ask", player_ids[i])
			return int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
		}
		if !f.db.FriendAsks.HasIndex(player_ids[i]) {
			log.Error("Player[%v] friend ask list not player[%v]", f.Id, player_ids[i])
			return int32(msg_client_message.E_ERR_PLAYER_FRIEND_PLAYER_NO_IN_ASK_LIST)
		}
	}

	for i := 0; i < len(player_ids); i++ {
		p := player_mgr.GetPlayerById(player_ids[i])
		if p == nil {
			player_ids[i] = 0
			log.Warn("Player[%v] agree friend %v not found", f.Id, player_ids[i])
			continue
		}
		if p.db.Friends.NumAll() >= global_config.FriendMaxNum {
			if len(player_ids) == 1 {
				log.Error("Player[%v] cant agree add friend %v", f.Id, p.Id)
				return int32(msg_client_message.E_ERR_PLAYER_FRIEND_OTHER_SIDE_FRIEND_MAX)
			}
			player_ids[i] = int32(msg_client_message.E_ERR_PLAYER_FRIEND_OTHER_SIDE_FRIEND_MAX)
			log.Warn("Player[%v] friend num is max, agree failed", p.Id)
			continue
		}
		p.db.Friends.Add(&dbPlayerFriendData{
			PlayerId: f.Id,
		})
		p.friend_add_ids([]int32{f.Id})
		p.check_and_send_friend_add()
		f.db.Friends.Add(&dbPlayerFriendData{
			PlayerId: player_ids[i],
		})
		f.db.FriendAsks.Remove(player_ids[i])
		f.db.FriendRecommends.Remove(player_ids[i])
	}

	response := &msg_client_message.S2CFriendAgreeResponse{
		Friends: f._format_friends_info(player_ids),
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_AGREE_RESPONSE), response)

	f.friend_add_ids(player_ids)
	f.check_and_send_friend_add()

	log.Trace("Player[%v] agreed players[%v] friend ask", f.Id, player_ids)

	return 1
}

// 拒绝好友申请
func (f *Player) refuse_friend_ask(player_ids []int32) int32 {
	if player_ids == nil {
		return 0
	}

	for _, player_id := range player_ids {
		if !f.db.FriendAsks.HasIndex(player_id) {
			log.Error("Player[%v] ask list no player[%v]", f.Id, player_id)
			return int32(msg_client_message.E_ERR_PLAYER_FRIEND_PLAYER_NO_IN_ASK_LIST)
		}
	}

	for _, player_id := range player_ids {
		f.db.FriendAsks.Remove(player_id)
	}

	response := &msg_client_message.S2CFriendRefuseResponse{
		PlayerIds: player_ids,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_REFUSE_RESPONSE), response)

	log.Trace("Player[%v] refuse players %v friend ask", f.Id, player_ids)

	return 1
}

// 删除好友
func (f *Player) remove_friend(friend_ids []int32) int32 {
	for i := 0; i < len(friend_ids); i++ {
		if !f.db.Friends.HasIndex(friend_ids[i]) {
			log.Error("Player[%v] no friend[%v]", f.Id, friend_ids[i])
			return int32(msg_client_message.E_ERR_PLAYER_FRIEND_NOT_FOUND)
		}
	}

	for i := 0; i < len(friend_ids); i++ {
		f.db.Friends.Remove(friend_ids[i])
		friend := player_mgr.GetPlayerById(friend_ids[i])
		if friend != nil {
			friend.db.Friends.Remove(f.Id)
		}
	}

	response := &msg_client_message.S2CFriendRemoveResponse{
		PlayerIds: friend_ids,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_REMOVE_RESPONSE), response)

	var notify = msg_client_message.S2CFriendRemoveNotify{
		FriendId: f.Id,
	}
	for i := 0; i < len(friend_ids); i++ {
		friend := player_mgr.GetPlayerById(friend_ids[i])
		if friend != nil {
			friend.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_REMOVE_NOTIFY), &notify)
		}
	}

	log.Trace("Player[%v] removed friends: %v", f.Id, friend_ids)

	return 1
}

// 检测获取友情点刷新
func (f *Player) check_get_friend_points_day_refresh() bool {
	if utils.CheckDayTimeArrival(f.db.FriendCommon.GetLastGetPointsTime(), global_config.FriendRefreshTime) {
		f.db.FriendCommon.SetLastGetPointsTime(int32(time.Now().Unix()))
		f.db.FriendCommon.SetGetPointsDay(0)
		return true
	}
	return false
}

// 赠送友情点
func (f *Player) give_friends_points(friend_ids []int32) int32 {
	for i := 0; i < len(friend_ids); i++ {
		if !f.db.Friends.HasIndex(friend_ids[i]) {
			log.Error("Player[%v] no friend[%v]", f.Id, friend_ids[i])
			return int32(msg_client_message.E_ERR_PLAYER_FRIEND_NOT_FOUND)
		}
	}

	is_gived := make([]bool, len(friend_ids))
	now_time := int32(time.Now().Unix())
	for i := 0; i < len(friend_ids); i++ {
		friend := player_mgr.GetPlayerById(friend_ids[i])
		if friend == nil {
			continue
		}

		remain_seconds := f._friend_get_give_remain_seconds(friend, now_time)
		if remain_seconds == 0 {
			f.db.Friends.SetLastGivePointsTime(friend_ids[i], now_time)
			friend.db.Friends.SetGetPoints(f.Id, global_config.FriendPointsOnceGive)
			is_gived[i] = true
			// 更新任务
			f.TaskUpdate(table_config.TASK_COMPLETE_TYPE_GIVE_POINTS_NUM, false, 0, 1)
		}
	}

	response := &msg_client_message.S2CFriendGivePointsResponse{
		FriendIds:    friend_ids,
		IsGivePoints: is_gived,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_GIVE_POINTS_RESPONSE), response)

	log.Trace("Player[%v] give friends %v points, is gived %v", f.Id, friend_ids, is_gived)

	return 1
}

// 收取友情点
func (f *Player) get_friend_points(friend_ids []int32) int32 {
	for i := 0; i < len(friend_ids); i++ {
		if !f.db.Friends.HasIndex(friend_ids[i]) {
			log.Error("Player[%v] no friend[%v]", f.Id, friend_ids[i])
			return int32(msg_client_message.E_ERR_PLAYER_FRIEND_NOT_FOUND)
		}
	}

	f.check_get_friend_points_day_refresh()

	get_points := make([]int32, len(friend_ids))
	for i := 0; i < len(friend_ids); i++ {
		friend := player_mgr.GetPlayerById(friend_ids[i])
		if friend == nil {
			continue
		}
		get_point := f._friend_get_points(friend, int32(time.Now().Unix()))
		if get_point > 0 {
			f.add_resource(global_config.FriendPointItemId, get_point)
			f.db.Friends.SetGetPoints(friend_ids[i], -1)
			f.db.FriendCommon.IncbyGetPointsDay(get_point)
			get_points[i] = get_point
		}
	}

	f.check_and_send_items_change()

	response := &msg_client_message.S2CFriendGetPointsResponse{
		FriendIds: friend_ids,
		GetPoints: get_points,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_GET_POINTS_RESPONSE), response)

	log.Trace("Player[%v] get friends %v points %v", f.Id, friend_ids, get_points)

	return 1
}

func (f *Player) friend_search_boss_check(now_time int32, output bool) (int32, *table_config.XmlFriendBossItem) {
	if f.db.FriendCommon.GetFriendBossTableId() > 0 {
		return int32(msg_client_message.E_ERR_PLAYER_FRIEND_BOSS_NO_NEED_TO_REFRESH), nil
	}
	last_refresh_time := f.db.FriendCommon.GetLastBossRefreshTime()
	if last_refresh_time > 0 && now_time-last_refresh_time < global_config.FriendSearchBossRefreshMinutes*60 {
		if output {
			log.Error("Player[%v] friend boss search is cool down", f.Id)
		}
		return int32(msg_client_message.E_ERR_PLAYER_FRIEND_BOSS_REFRESH_IS_COOLDOWN), nil
	}

	friend_boss_tdata := friend_boss_table_mgr.GetWithLevel(f.db.Info.GetLvl())
	if friend_boss_tdata == nil {
		log.Error("Player[%v] cant searched friend boss with level %v", f.Id, f.db.Info.GetLvl())
		return int32(msg_client_message.E_ERR_PLAYER_FRIEND_BOSS_DATA_NOT_FOUND), nil
	}
	return 1, friend_boss_tdata
}

// 搜索BOSS
func (f *Player) friend_search_boss() int32 {
	need_level := system_unlock_table_mgr.GetUnlockLevel("FriendBossEnterLevel")
	if need_level > f.db.Info.GetLvl() {
		log.Error("Player[%v] level not enough level %v enter friend boss", f.Id, need_level)
		return int32(msg_client_message.E_ERR_PLAYER_LEVEL_NOT_ENOUGH)
	}

	now_time := int32(time.Now().Unix())
	res, friend_boss_tdata := f.friend_search_boss_check(now_time, true)
	if res < 0 {
		return res
	}

	var boss_id int32
	var items []*msg_client_message.ItemInfo
	r := rand.Int31n(10000)
	if r >= friend_boss_tdata.SearchBossChance {
		// 掉落
		o, item := f.drop_item_by_id(friend_boss_tdata.SearchItemDropID, true, nil, nil)
		if !o {
			log.Error("Player[%v] search friend boss to drop item with id %v failed", f.Id, friend_boss_tdata.SearchItemDropID)
			return -1
		}
		items = []*msg_client_message.ItemInfo{item}
	} else {
		stage_id := friend_boss_tdata.BossStageID
		stage := stage_table_mgr.Get(stage_id)
		if stage == nil {
			log.Error("Stage[%v] table data not found in friend boss", stage_id)
			return -1
		}
		if stage.Monsters == nil {
			log.Error("Stage[%v] monster list is empty", stage_id)
			return -1
		}

		f.db.FriendCommon.SetFriendBossTableId(friend_boss_tdata.Id)
		f.db.FriendBosss.Clear()
		for i := 0; i < len(stage.Monsters); i++ {
			f.db.FriendBosss.Add(&dbPlayerFriendBossData{
				MonsterPos: stage.Monsters[i].Slot - 1,
				MonsterId:  stage.Monsters[i].MonsterID,
			})
		}
		boss_id = friend_boss_tdata.Id
	}

	f.db.FriendCommon.SetLastBossRefreshTime(now_time)
	f.db.FriendCommon.SetAttackBossPlayerList(nil)

	response := &msg_client_message.S2CFriendSearchBossResponse{
		FriendBossTableId: boss_id,
		Items:             items,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_SEARCH_BOSS_RESPONSE), response)

	if boss_id > 0 {
		log.Trace("Player[%v] search friend boss %v", f.Id, boss_id)
	} else {
		log.Trace("Player[%v] search friend boss get items %v", f.Id, items)
	}

	return 1
}

// 获得好友BOSS列表
func (f *Player) get_friends_boss_list() int32 {
	need_level := system_unlock_table_mgr.GetUnlockLevel("FriendBossEnterLevel")
	if need_level > f.db.Info.GetLvl() {
		log.Error("Player[%v] level not enough level %v enter friend boss", f.Id, need_level)
		return int32(msg_client_message.E_ERR_PLAYER_LEVEL_NOT_ENOUGH)
	}

	friend_ids := f.db.Friends.GetAllIndex()
	if len(friend_ids) == 0 {
		log.Error("Player[%v] no friends", f.Id)
		return int32(msg_client_message.E_ERR_PLAYER_FRIEND_NONE)
	}

	now_time := int32(time.Now().Unix())
	level := f.db.Info.GetLvl()
	// 加上自己的
	friend_ids = append(friend_ids, f.Id)
	var friend_boss_list []*msg_client_message.FriendBossInfo
	for i := 0; i < len(friend_ids); i++ {
		p := player_mgr.GetPlayerById(friend_ids[i])
		if p == nil {
			continue
		}
		last_refresh_time := p.db.FriendCommon.GetLastBossRefreshTime()
		if now_time-last_refresh_time >= global_config.FriendSearchBossRefreshMinutes*60 {
			continue
		}
		friend_boss_table_id := p.db.FriendCommon.GetFriendBossTableId()
		if friend_boss_table_id == 0 {
			continue
		}
		friend_boss_tdata := friend_boss_table_mgr.Get(friend_boss_table_id)
		if friend_boss_tdata == nil {
			log.Error("Player[%v] stored friend boss table id[%v] not found", friend_ids[i], friend_boss_table_id)
			continue
		}

		if friend_boss_tdata.LevelMin > level || friend_boss_tdata.LevelMax < level {
			continue
		}

		hp_percent := p.get_friend_boss_hp_percent()
		friend_boss_info := &msg_client_message.FriendBossInfo{
			FriendId:            friend_ids[i],
			FriendBossTableId:   friend_boss_table_id,
			FriendBossHpPercent: hp_percent,
		}
		friend_boss_list = append(friend_boss_list, friend_boss_info)
	}

	response := &msg_client_message.S2CFriendsBossListResponse{
		BossList: friend_boss_list,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIENDS_BOSS_LIST_RESPONSE), response)

	log.Trace("Player[%v] get friend boss list %v", f.Id, response)

	return 1
}

func (f *Player) can_friend_boss_to_fight() bool {
	return atomic.CompareAndSwapInt32(&f.fighing_friend_boss, 0, 1)
}

func (f *Player) cancel_friend_boss_fight() bool {
	return atomic.CompareAndSwapInt32(&f.fighing_friend_boss, 1, 0)
}

// 战斗随机奖励
func (f *Player) battle_random_reward_notify(drop_id, drop_num int32) {
	var fake_items []int32
	if drop_num < 1 {
		for i := 0; i < 2; i++ {
			o, item := f.drop_item_by_id(drop_id, false, nil, nil)
			if !o {
				log.Error("Player[%v] drop id %v invalid on friend boss attack", f.Id, drop_id)
			}
			fake_items = append(fake_items, item.Id)
		}
	}

	var items map[int32]int32
	if drop_num < 1 {
		drop_num = 1
	}
	for i := int32(0); i < drop_num; i++ {
		o, item := f.drop_item_by_id(drop_id, false, nil, nil)
		if !o {
			log.Error("Player[%v] drop id %v invalid on friend boss attack", f.Id, drop_id)
		}
		f.add_resource(item.Id, item.Value)
		if items == nil {
			items = make(map[int32]int32)
		}
		items[item.GetId()] += item.GetValue()
	}

	if items != nil {
		notify := &msg_client_message.S2CBattleRandomRewardNotify{
			Items:     Map2ItemInfos(items),
			FakeItems: fake_items,
		}
		f.Send(uint16(msg_client_message_id.MSGID_S2C_BATTLE_RANDOM_REWARD_NOTIFY), notify)
		log.Trace("Player[%v] battle random reward %v", f.Id, notify)
	}
}

// 挑战好友BOSS
func (f *Player) friend_boss_challenge(friend_id int32) int32 {
	/*need_level := system_unlock_table_mgr.GetUnlockLevel("FriendBossEnterLevel")
	if need_level > f.db.Info.GetLvl() {
		log.Error("Player[%v] level not enough level %v enter friend boss", f.Id, need_level)
		return int32(msg_client_message.E_ERR_PLAYER_LEVEL_NOT_ENOUGH)
	}*/

	if f.sweep_num < 0 || f.sweep_num > global_config.FriendStaminaLimit {
		return -1
	}

	if friend_id == 0 {
		friend_id = f.Id
	}

	p := player_mgr.GetPlayerById(friend_id)
	if p == nil {
		log.Error("Player[%v] not found", friend_id)
		return int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
	}

	if friend_id == f.Id {
		p = f
	}

	last_refresh_time := p.db.FriendCommon.GetLastBossRefreshTime()
	if last_refresh_time == 0 {
		log.Error("Player[%v] fight friend[%v] boss not found")
		return int32(msg_client_message.E_ERR_PLAYER_FRIEND_BOSS_NOT_FOUND)
	}

	f.check_and_add_friend_stamina()

	// 是否正在挑战好友BOSS
	if !p.can_friend_boss_to_fight() {
		log.Warn("Player[%v] friend boss is fighting", p.Id)
		return int32(msg_client_message.E_ERR_PLAYER_FRIEND_BOSS_IS_FIGHTING)
	}

	// 获取好友BOSS
	friend_boss_table_id := p.db.FriendCommon.GetFriendBossTableId()
	if friend_boss_table_id == 0 {
		p.cancel_friend_boss_fight()
		log.Error("Player[%v] fight friend[%v] boss is finished or not refreshed", f.Id, friend_id)
		return int32(msg_client_message.E_ERR_PLAYER_FRIEND_BOSS_NOT_FOUND)
	}

	friend_boss_tdata := friend_boss_table_mgr.Get(friend_boss_table_id)
	if friend_boss_tdata == nil {
		p.cancel_friend_boss_fight()
		log.Error("Player[%v] stored friend boss table id %v not found", p.Id, friend_boss_table_id)
		return int32(msg_client_message.E_ERR_PLAYER_FRIEND_BOSS_DATA_NOT_FOUND)
	}

	stage := stage_table_mgr.Get(friend_boss_tdata.BossStageID)
	if stage == nil {
		p.cancel_friend_boss_fight()
		log.Error("Friend Boss Stage %v not found")
		return int32(msg_client_message.E_ERR_PLAYER_STAGE_TABLE_DATA_NOT_FOUND)
	}

	// 体力
	var need_stamina int32
	if f.sweep_num == 0 {
		need_stamina = global_config.FriendBossAttackCostStamina
	} else {
		need_stamina = global_config.FriendBossAttackCostStamina * f.sweep_num
	}

	stamina := f.get_resource(global_config.FriendStaminaItemId)
	if stamina < need_stamina {
		p.cancel_friend_boss_fight()
		log.Error("Player[%v] friend stamina item not enough", f.Id)
		return int32(msg_client_message.E_ERR_PLAYER_FRIEND_BOSS_STAMINA_NOT_ENOUGH)
	}

	var fight_num int32
	if f.sweep_num == 0 {
		fight_num = 1
	} else {
		fight_num = f.sweep_num
	}

	var err, n, my_artifact_id, target_artifact_id int32
	var is_win, has_next_wave bool
	var my_team, target_team []*msg_client_message.BattleMemberItem
	var enter_reports []*msg_client_message.BattleReportItem
	var rounds []*msg_client_message.BattleRoundReports
	for ; n < fight_num; n++ {
		err, is_win, my_team, target_team, my_artifact_id, target_artifact_id, enter_reports, rounds, has_next_wave = f.FightInStage(5, stage, p, nil)
		if err < 0 {
			p.cancel_friend_boss_fight()
			log.Error("Player[%v] fight friend %v boss %v failed, team is empty", f.Id, friend_id, friend_boss_table_id)
			return err
		}

		// 助战玩家列表
		attack_list := p.db.FriendCommon.GetAttackBossPlayerList()
		if attack_list == nil {
			attack_list = []int32{f.Id}
			p.db.FriendCommon.SetAttackBossPlayerList(attack_list)
		} else {
			has := false
			for i := 0; i < len(attack_list); i++ {
				if attack_list[i] == f.Id {
					has = true
					break
				}
			}
			if !has {
				attack_list = append(attack_list, f.Id)
				p.db.FriendCommon.SetAttackBossPlayerList(attack_list)
			}
		}

		if is_win {
			p.db.FriendCommon.SetFriendBossTableId(0)
			p.db.FriendCommon.SetFriendBossHpPercent(0)
			if f.sweep_num > 0 {
				n += 1
				break
			}
		}
	}

	// 退出挑战
	p.cancel_friend_boss_fight()

	// 实际消耗体力
	f.add_resource(global_config.FriendStaminaItemId, -n*global_config.FriendBossAttackCostStamina)
	if n > 0 && stamina >= global_config.FriendStaminaLimit {
		f.db.FriendCommon.SetLastGetStaminaTime(int32(time.Now().Unix()))
	}

	member_damages := f.friend_boss_team.common_data.members_damage
	member_cures := f.friend_boss_team.common_data.members_cure
	response := &msg_client_message.S2CBattleResultResponse{
		IsWin:               is_win,
		EnterReports:        enter_reports,
		Rounds:              rounds,
		MyTeam:              my_team,
		TargetTeam:          target_team,
		MyMemberDamages:     member_damages[f.friend_boss_team.side],
		TargetMemberDamages: member_damages[f.target_stage_team.side],
		MyMemberCures:       member_cures[f.friend_boss_team.side],
		TargetMemberCures:   member_cures[f.target_stage_team.side],
		HasNextWave:         has_next_wave,
		BattleType:          5,
		BattleParam:         friend_id,
		SweepNum:            f.sweep_num,
		ExtraValue:          p.get_friend_boss_hp_percent(),
		MyArtifactId:        my_artifact_id,
		TargetArtifactId:    target_artifact_id,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_BATTLE_RESULT_RESPONSE), response)

	// 最后一击
	if is_win {
		f.send_stage_reward(stage.RewardList, 5, 0)
		RealSendMail(nil, f.Id, MAIL_TYPE_SYSTEM, 1109, "", "", friend_boss_tdata.RewardLastHit, 0)
		RealSendMail(nil, friend_id, MAIL_TYPE_SYSTEM, 1106, "", "", friend_boss_tdata.RewardOwner, 0)
	} else {
		if f.sweep_num == 0 {
			n = 0
		}
	}

	if f.sweep_num > 0 {
		f.sweep_num = 0
	}

	f.battle_random_reward_notify(friend_boss_tdata.ChallengeDropID, n)

	log.Trace("Player %v fight friend %v boss", f.Id, friend_id)

	return 1
}

// 获取好友BOSS攻击记录列表
func (f *Player) friend_boss_get_attack_list(friend_id int32) int32 {
	friend := player_mgr.GetPlayerById(friend_id)
	if friend == nil {
		log.Error("Player[%v] not found", friend_id)
		return int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
	}

	if friend.db.FriendCommon.GetFriendBossTableId() <= 0 {
		log.Error("Player[%v] friend boss is finished")
		return int32(msg_client_message.E_ERR_PLAYER_FRIEND_BOSS_IS_FINISHED)
	}

	var player_list []*msg_client_message.PlayerInfo
	attack_list := friend.db.FriendCommon.GetAttackBossPlayerList()
	if len(attack_list) == 0 {
		player_list = make([]*msg_client_message.PlayerInfo, 0)
	} else {
		for i := 0; i < len(attack_list); i++ {
			attacker := player_mgr.GetPlayerById(attack_list[i])
			if attacker == nil {
				continue
			}
			player_info := &msg_client_message.PlayerInfo{
				Id:    attack_list[i],
				Name:  attacker.db.GetName(),
				Level: attacker.db.Info.GetLvl(),
				Head:  attacker.db.Info.GetHead(),
			}
			player_list = append(player_list, player_info)
		}
	}

	response := &msg_client_message.S2CFriendBossAttackListResponse{
		AttackList: player_list,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_BOSS_ATTACK_LIST_RESPONSE), response)

	log.Trace("Player[%v] get friend[%v] boss attack list: %v", f.Id, friend_id, response)

	return 1
}

// 检测好友体力数据
func (f *Player) check_and_add_friend_stamina() (add_stamina int32, remain_seconds int32) {
	now_time := int32(time.Now().Unix())
	last_get_stamina_time := f.db.FriendCommon.GetLastGetStaminaTime()
	if last_get_stamina_time == 0 {
		f.add_resource(global_config.FriendStaminaItemId, global_config.FriendStaminaLimit)
		add_stamina = global_config.FriendStaminaLimit
		//remain_seconds = global_config.FriendStaminaResumeOnePointNeedHours * 3600
		f.db.FriendCommon.SetLastGetStaminaTime(now_time)
	} else {
		stamina := f.get_resource(global_config.FriendStaminaItemId)
		if stamina < global_config.FriendStaminaLimit {
			need_stamina := global_config.FriendStaminaLimit - stamina
			cost_seconds := now_time - last_get_stamina_time
			y := cost_seconds % (global_config.FriendStaminaResumeOnePointNeedHours * 3600)
			add_stamina = (cost_seconds - y) / (global_config.FriendStaminaResumeOnePointNeedHours * 3600)
			if add_stamina > 0 {
				if add_stamina > need_stamina {
					add_stamina = need_stamina
				}
				f.add_resource(global_config.FriendStaminaItemId, add_stamina)
				stamina = f.get_resource(global_config.FriendStaminaItemId)
				now_time -= y
				f.db.FriendCommon.SetLastGetStaminaTime(now_time)
			}
			if stamina < global_config.FriendStaminaLimit {
				remain_seconds = global_config.FriendStaminaResumeOnePointNeedHours*3600 - y
			}
		}
	}

	return
}

func (f *Player) get_friend_boss_hp_percent() int32 {
	if f.db.FriendCommon.GetFriendBossTableId() <= 0 {
		return 0
	}
	hp_percent := f.db.FriendCommon.GetFriendBossHpPercent()
	if hp_percent == 0 {
		hp_percent = 100
	}
	return hp_percent
}

// 获取好友相关数据
func (f *Player) friend_data(send bool) int32 {
	add_stamina, remain_seconds := f.check_and_add_friend_stamina()
	if send {
		last_refresh_boss_time := f.db.FriendCommon.GetLastBossRefreshTime()
		now_time := int32(time.Now().Unix())
		boss_remain_seconds := global_config.FriendSearchBossRefreshMinutes*60 - (now_time - last_refresh_boss_time)
		if boss_remain_seconds < 0 {
			boss_remain_seconds = 0
		}
		f.check_active_stage_refresh(false)
		f.check_get_friend_points_day_refresh()
		response := &msg_client_message.S2CFriendDataResponse{
			StaminaItemId:            global_config.FriendStaminaItemId,
			AddStamina:               add_stamina,
			RemainSecondsNextStamina: remain_seconds,
			StaminaLimit:             global_config.FriendStaminaLimit,
			StaminaResumeOneCostTime: global_config.FriendStaminaResumeOnePointNeedHours,
			BossId:                   f.db.FriendCommon.GetFriendBossTableId(),
			BossHpPercent:            f.get_friend_boss_hp_percent(),
			AssistGetPoints:          f.get_assist_points(),
			SearchBossRemainSeconds:  boss_remain_seconds,
			AssistRoleId:             f.db.FriendCommon.GetAssistRoleId(),
			TotalAssistGetPoints:     f.db.ActiveStageCommon.GetWithdrawPoints(),
			TotalFriendGiveGetPoints: f.db.FriendCommon.GetGetPointsDay(),
		}
		f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_DATA_RESPONSE), response)

		log.Trace("Player[%v] friend data %v", f.Id, response)
	}

	return 1
}

// 设置助战角色
func (f *Player) friend_set_assist_role(role_id int32) int32 {
	if !f.db.Roles.HasIndex(role_id) {
		log.Error("Player[%v] not have role %v", f.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_NOT_FOUND)
	}

	_, o := f.db.Roles.GetIsLock(role_id)
	if !o {
		log.Error("Player[%v] role[%v] is locked", f.Id, role_id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_IS_LOCKED)
	}

	old_assist_role := f.db.FriendCommon.GetAssistRoleId()
	if old_assist_role == role_id {
		log.Error("Player[%v] set assist role is same to old", f.Id)
		return -1
	}

	if old_assist_role > 0 {
		f.db.Roles.SetIsLock(old_assist_role, 0)
		f.roles_id_change_info.id_update(old_assist_role)
	}

	f.db.FriendCommon.SetAssistRoleId(role_id)
	f.db.Roles.SetIsLock(role_id, 1)
	f.roles_id_change_info.id_update(role_id)

	response := &msg_client_message.S2CFriendSetAssistRoleResponse{
		RoleId: role_id,
	}
	f.Send(uint16(msg_client_message_id.MSGID_S2C_FRIEND_SET_ASSIST_ROLE_RESPONSE), response)

	f.check_and_send_roles_change()

	log.Trace("Player[%v] set assist role %v for friends", f.Id, role_id)

	return 1
}

// ------------------------------------------------------

func C2SFriendsRecommendHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendRecommendRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.send_recommend_friends()
}

func C2SFriendListHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendListRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	p.friend_data(true)
	return p.send_friend_list()
}

func C2SFriendAskListHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendAskPlayerListRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.send_friend_ask_list()
}

func C2SFriendAskHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendAskRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.friend_ask(req.GetPlayerIds())
}

func C2SFriendAgreeHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendAgreeRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.agree_friend_ask(req.GetPlayerIds())
}

func C2SFriendRefuseHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendRefuseRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.refuse_friend_ask(req.GetPlayerIds())
}

func C2SFriendRemoveHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendRemoveRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.remove_friend(req.GetPlayerIds())
}

func C2SFriendGivePointsHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendGivePointsRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.give_friends_points(req.GetFriendIds())
}

func C2SFriendGetPointsHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendGetPointsRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.get_friend_points(req.GetFriendIds())
}

func C2SFriendSearchBossHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendSearchBossRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.friend_search_boss()
}

func C2SFriendGetBossListHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendsBossListRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.get_friends_boss_list()
}

func C2SFriendBossAttackListHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendBossAttackListRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.friend_boss_get_attack_list(req.GetFriendId())
}

func C2SFriendDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.friend_data(true)
}

func C2SFriendSetAssistRoleHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendSetAssistRoleRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.friend_set_assist_role(req.GetRoleId())
}

func C2SFriendGiveAndGetPointsHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendGiveAndGetPointsRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}

	res := p.give_friends_points(req.GetFriendIds())
	if res < 0 {
		return res
	}
	return p.get_friend_points(req.GetFriendIds())
}

func C2SFriendGetAssistPointsHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SFriendGetAssistPointsRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.active_stage_withdraw_assist_points()
}
