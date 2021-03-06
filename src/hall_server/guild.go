package main

import (
	"ih_server/libs/log"
	"ih_server/libs/utils"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/proto"
)

const (
	GUILD_MAX_NUM       = 10000 // 公会最大数目
	GUILD_RECOMMEND_NUM = 5     // 公会推荐数目
	GUILD_LOG_MAX_NUM   = 10    // 公会日志数
)

const (
	GUILD_EXIST_TYPE_NONE        = iota // 未删除
	GUILD_EXIST_TYPE_WILL_DELETE = 1    // 将删除
	GUILD_EXIST_TYPE_DELETED     = 2    // 已删除
)

const (
	GUILD_POSITION_MEMBER    = iota // 成员
	GUILD_POSITION_PRESIDENT = 1    // 会长
	GUILD_POSITION_OFFICER   = 2    // 官员
)

const (
	GUILD_LOG_TYPE_NONE             = iota
	GUILD_LOG_TYPE_CREATE           = 1
	GUILD_LOG_TYPE_MEMBER_JOIN      = 2
	GUILD_LOG_TYPE_MEMBER_QUIT      = 3
	GUILD_LOG_TYPE_MEMBER_KICK      = 4
	GUILD_LOG_TYPE_MEMBER_APPOINT   = 5
	GUILD_LOG_TYPE_OFFICER_DISMISS  = 6
	GUILD_LOG_TYPE_PRESIDENT_CHANGE = 7
)

func _player_get_guild_id(player_id int32) int32 {
	player := player_mgr.GetPlayerById(player_id)
	if player == nil {
		return int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
	}
	guild_id := player.db.Guild.GetId()
	return guild_id
}

func _remove_guild(guild *dbGuildRow) {
	guild.SetExistType(GUILD_EXIST_TYPE_DELETED)
	guild.Members.Clear()
	guild.AskLists.Clear()
	guild_manager._remove_guild(guild.GetId(), guild.GetName())
}

type Guild struct {
	id                    int32
	name                  string
	chat_mgr              *ChatMgr
	stage_damage_list_map map[int32]*utils.ShortRankList
	stage_fight_state     int32
	locker                *sync.RWMutex
}

func (g *Guild) Init(id int32, name string) {
	g.id = id
	g.name = name
	g.chat_mgr = &ChatMgr{}
	g.chat_mgr.Init(CHAT_CHANNEL_GUILD)
	// 伤害列表
	damage_list_map := make(map[int32]*utils.ShortRankList)
	for i := 0; i < len(guild_boss_table_mgr.Array); i++ {
		rank_list := &utils.ShortRankList{}
		rank_list.Init(guild_levelup_table_mgr.GetMaxMemberNum())
		boss_id := guild_boss_table_mgr.Array[i].Id
		damage_list_map[boss_id] = rank_list
	}
	g.stage_damage_list_map = damage_list_map
	g.locker = &sync.RWMutex{}
}

func (g *Guild) CanStageFight() bool {
	return atomic.CompareAndSwapInt32(&g.stage_fight_state, 0, 1)
}

func (g *Guild) CancelStageFight() bool {
	return atomic.CompareAndSwapInt32(&g.stage_fight_state, 1, 0)
}

type GuildManager struct {
	guilds           *dbGuildTable
	guild_ids        []int32
	guild_num        int32
	guild_id_map     map[int32]int32
	guild_name_map   map[string]int32
	guilds_ex_map    map[int32]*Guild
	guild_ids_locker *sync.RWMutex
}

var guild_manager GuildManager

func (g *GuildManager) get_guild_num() int32 {
	g.guild_ids_locker.RLock()
	defer g.guild_ids_locker.RUnlock()
	return g.guild_num
}

func (g *GuildManager) _add_guild(guild_id int32, guild_name string) bool {
	g.guild_ids_locker.Lock()
	defer g.guild_ids_locker.Unlock()

	if _, o := g.guild_id_map[guild_id]; o {
		return false
	}
	if _, o := g.guild_name_map[guild_name]; o {
		return false
	}
	g.guild_ids[g.guild_num] = guild_id
	g.guild_num += 1
	g.guild_id_map[guild_id] = guild_id
	g.guild_name_map[guild_name] = guild_id

	guild_ex := &Guild{}
	guild_ex.Init(guild_id, guild_name)
	g.guilds_ex_map[guild_id] = guild_ex

	return true
}

func (g *GuildManager) _remove_guild(guild_id int32, guild_name string) bool {
	g.guild_ids_locker.Lock()
	defer g.guild_ids_locker.Unlock()

	if _, o := g.guild_id_map[guild_id]; !o {
		return false
	}
	if _, o := g.guild_name_map[guild_name]; !o {
		return false
	}
	for i := int32(0); i < g.guild_num; i++ {
		if g.guild_ids[i] == guild_id {
			g.guild_ids[i] = g.guild_ids[g.guild_num-1]
			g.guild_num -= 1
			break
		}
	}
	delete(g.guild_id_map, guild_id)
	delete(g.guild_name_map, guild_name)
	delete(g.guilds_ex_map, guild_id)

	return true
}

func (g *GuildManager) _has_guild_by_name(guild_name string) bool {
	g.guild_ids_locker.RLock()
	defer g.guild_ids_locker.RUnlock()

	if _, o := g.guild_name_map[guild_name]; o {
		return true
	}

	return false
}

func (g *GuildManager) _change_name(guild_id int32, new_name string) bool {
	g.guild_ids_locker.Lock()
	defer g.guild_ids_locker.Unlock()

	row := g.guilds.GetRow(guild_id)
	if row == nil {
		return false
	}

	id, o := g.guild_name_map[row.GetName()]
	if !o {
		return false
	}

	if id == guild_id {
		delete(g.guild_name_map, row.GetName())
	}

	g.guild_name_map[new_name] = guild_id

	row.SetName(new_name)

	return true
}

func (g *GuildManager) Init() {
	g.guilds = dbc.Guilds
	g.guild_ids = make([]int32, GUILD_MAX_NUM)
	g.guild_id_map = make(map[int32]int32)
	g.guild_name_map = make(map[string]int32)
	g.guilds_ex_map = make(map[int32]*Guild)
	g.guild_ids_locker = &sync.RWMutex{}
	for gid, guild := range g.guilds.m_rows {
		if _guild_get_exist_type(guild) == GUILD_EXIST_TYPE_DELETED {
			continue
		}
		g._add_guild(gid, guild.GetName())
	}
}

func (g *GuildManager) LoadDB4StageDamageList() {
	for guild_id, guild_ex_map := range g.guilds_ex_map {
		for boss_id, rank_list := range guild_ex_map.stage_damage_list_map {
			guild_stage_manager.LoadDB2RankList(guild_id, boss_id, rank_list)
		}
	}
}

func (g *GuildManager) CreateGuild(player_id int32, guild_name string, logo int32) int32 {
	player := player_mgr.GetPlayerById(player_id)
	if player == nil {
		return int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
	}

	if int32(len(guild_name)) > global_config.GuildNameLength {
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_NAME_TOO_LONG)
	}

	player.join_guild_locker.Lock()

	if g._has_guild_by_name(guild_name) {
		player.join_guild_locker.Unlock()
		log.Error("Player[%v] create guild with name %v is already used", player_id, guild_name)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_NAME_IS_USED)
	}

	guild_id := player.db.Guild.GetId()
	if guild_id > 0 && g.GetGuild(guild_id) != nil {
		player.join_guild_locker.Unlock()
		log.Error("Player[%v] already create guild[%v|%v]", player_id, guild_name, guild_id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_ALREADY_CREATED_OR_JOINED)
	}

	guild_id = dbc.Global.GetRow().GetNextGuildId()
	row := g.guilds.AddRow(guild_id)
	if row == nil {
		player.join_guild_locker.Unlock()
		log.Error("Player[%v] create guild add db row failed", player_id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CREATED_DB_ERROR)
	}

	row.SetName(guild_name)
	row.SetCreater(player_id)
	row.SetCreateTime(int32(time.Now().Unix()))
	row.SetLevel(1)
	row.SetLogo(logo)
	row.SetPresident(player_id)
	row.Members.Add(&dbGuildMemberData{
		PlayerId: player_id,
	})

	player.db.Guild.SetId(guild_id)
	player.db.Guild.SetPosition(GUILD_POSITION_PRESIDENT)

	player.join_guild_locker.Unlock()

	g._add_guild(guild_id, guild_name)

	return guild_id
}

func (g *GuildManager) GetGuild(guild_id int32) *dbGuildRow {
	guild := g.guilds.GetRow(guild_id)
	if guild == nil {
		return nil
	}
	exist_type := _guild_get_exist_type(guild)
	if exist_type == GUILD_EXIST_TYPE_DELETED {
		return nil
	}
	return guild
}

func (g *GuildManager) GetGuildEx(guild_id int32) *Guild {
	g.guild_ids_locker.RLock()
	defer g.guild_ids_locker.RUnlock()

	return g.guilds_ex_map[guild_id]
}

func (g *GuildManager) GetChatMgr(guild_id int32) *ChatMgr {
	g.guild_ids_locker.RLock()
	defer g.guild_ids_locker.RUnlock()

	guild_ex := g.guilds_ex_map[guild_id]
	if guild_ex == nil {
		return nil
	}
	return guild_ex.chat_mgr
}

func (g *GuildManager) GetStageDamageList(guild_id, boss_id int32) *utils.ShortRankList {
	g.guild_ids_locker.RLock()
	defer g.guild_ids_locker.RUnlock()

	guild_ex := g.guilds_ex_map[guild_id]
	if guild_ex == nil {
		return nil
	}
	return guild_ex.stage_damage_list_map[boss_id]
}

func (g *GuildManager) CanStageFight(guild_id int32) bool {
	g.guild_ids_locker.RLock()
	defer g.guild_ids_locker.RUnlock()

	guild_ex := g.guilds_ex_map[guild_id]
	if guild_ex == nil {
		return false
	}
	return guild_ex.CanStageFight()
}

func (g *GuildManager) CancelStageFight(guild_id int32) bool {
	g.guild_ids_locker.RLock()
	defer g.guild_ids_locker.RUnlock()

	guild_ex := g.guilds_ex_map[guild_id]
	if guild_ex == nil {
		return false
	}
	return guild_ex.CancelStageFight()
}

func (g *GuildManager) Recommend(player_id int32) (err int32, guild_ids []int32) {
	guild_id := _player_get_guild_id(player_id)
	if guild_id > 0 && g.GetGuild(guild_id) != nil {
		err = int32(msg_client_message.E_ERR_PLAYER_GUILD_ALREADY_CREATED_OR_JOINED)
		log.Error("Player[%v] already joined one guild", player_id)
		return
	}

	g.guild_ids_locker.RLock()
	defer g.guild_ids_locker.RUnlock()

	if g.guild_num == 0 {
		err = int32(msg_client_message.E_ERR_PLAYER_GUILD_NO_GUILDS_TO_RECOMMEND)
		log.Error("No guild to recommend")
		return
	}

	rand.Seed(time.Now().Unix() + time.Now().UnixNano())

	for i := 0; i < GUILD_RECOMMEND_NUM; i++ {
		r := rand.Int31n(g.guild_num)
		sr := r
		for {
			has := false
			if guild_ids != nil {
				for n := 0; n < len(guild_ids); n++ {
					if guild_ids[n] == g.guild_ids[sr] {
						has = true
						break
					}
				}
			}
			if !has {
				break
			}
			sr = (sr + 1) % g.guild_num
			if sr == r {
				err = 1
				log.Info("GuildManager Recommend guild count[%v] not enough to random for recommend", g.guild_num)
				return
			}
		}
		guild_id = g.guild_ids[sr]
		if guild_id <= 0 {
			break
		}
		guild_ids = append(guild_ids, guild_id)
	}
	err = 1
	return
}

func (g *GuildManager) Search(key string) (guild_ids []int32) {
	guild_id, err := strconv.Atoi(key)
	/*if err != nil {
		log.Error("!!!! search key %v must be number", key)
		return
	}*/

	g.guild_ids_locker.RLock()
	defer g.guild_ids_locker.RUnlock()

	if err == nil {
		if g.guild_id_map[int32(guild_id)] > 0 {
			guild_ids = []int32{int32(guild_id)}
		}
	}

	if guild_id, o := g.guild_name_map[key]; o {
		guild_ids = append(guild_ids, guild_id)
	}
	return
}

func (g *GuildManager) _get_guild(player_id int32, is_president bool) (guild *dbGuildRow) {
	player := player_mgr.GetPlayerById(player_id)
	if player == nil {
		return nil
	}
	guild_id := player.db.Guild.GetId()
	if guild_id <= 0 {
		return nil
	}
	guild = g.GetGuild(guild_id)
	if guild == nil || (is_president && guild.GetPresident() != player_id) {
		return nil
	}

	position := player.db.Guild.GetPosition()
	if position <= GUILD_POSITION_MEMBER && guild.GetPresident() == player_id {
		player.db.Guild.SetPosition(GUILD_POSITION_PRESIDENT)
	} else if position == GUILD_POSITION_PRESIDENT && guild.GetPresident() != player_id {
		guild.SetPresident(player_id)
	}

	if guild.GetPresident() == player_id && !guild.Members.HasIndex(player_id) {
		guild.Members.Add(&dbGuildMemberData{
			PlayerId: player_id,
		})
	}

	return guild
}

func _guild_member_num_limit(guild *dbGuildRow) int32 {
	levelup_data := guild_levelup_table_mgr.Get(guild.GetLevel())
	if levelup_data == nil {
		return 0
	}
	return levelup_data.MemberNum
}

func _format_guild_base_info_to_msg(guild *dbGuildRow) (msg *msg_client_message.GuildBaseInfo) {
	msg = &msg_client_message.GuildBaseInfo{
		Id:             guild.GetId(),
		Name:           guild.GetName(),
		Level:          guild.GetLevel(),
		Logo:           guild.GetLogo(),
		MemberNum:      guild.Members.NumAll(),
		MemberNumLimit: _guild_member_num_limit(guild),
	}
	return
}

func _format_guilds_base_info_to_msg(guild_ids []int32) (guilds_msg []*msg_client_message.GuildBaseInfo) {
	for _, gid := range guild_ids {
		guild := guild_manager.GetGuild(gid)
		if guild == nil {
			continue
		}
		guild_msg := _format_guild_base_info_to_msg(guild)
		guilds_msg = append(guilds_msg, guild_msg)
	}
	return
}

func _guild_get_dismiss_remain_seconds(guild *dbGuildRow) (dismiss_remain_seconds int32) {
	if guild.GetExistType() != GUILD_EXIST_TYPE_WILL_DELETE {
		return
	}
	dismiss_time := guild.GetDismissTime()
	dismiss_remain_seconds = GetRemainSeconds(dismiss_time, global_config.GuildDismissWaitingSeconds)
	if dismiss_remain_seconds == 0 {
		// 广播
		member_ids := guild.Members.GetAllIndex()
		if member_ids != nil {
			notify := &msg_client_message.S2CGuildDeleteNotify{
				GuildId: guild.GetId(),
			}
			for _, mid := range member_ids {
				p := player_mgr.GetPlayerById(mid)
				if p == nil {
					continue
				}
				p.db.Guild.SetId(0)
				p.db.Guild.SetPosition(0)
				p.db.Guild.SetDonateNum(0)
				p.db.Guild.SetJoinTime(0)
				//p.db.Guild.SetLastAskDonateTime(0)
				p.db.Guild.SetQuitTime(0)
				//p.db.Guild.SetSignTime(0)
				p.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_DELETE_NOTIFY), notify)
				RealSendMail(p, mid, MAIL_TYPE_GUILD, 1108, "", "", nil, 0)
			}
		}
		_remove_guild(guild)
	}
	return
}

func _guild_get_exist_type(guild *dbGuildRow) int32 {
	_guild_get_dismiss_remain_seconds(guild)
	return guild.GetExistType()
}

// 公会日志
func push_new_guild_log(guild *dbGuildRow, log_type, player_id int32) {
	player := player_mgr.GetPlayerById(player_id)
	if player == nil {
		return
	}

	var min_id int32
	ids := guild.Logs.GetAllIndex()
	if ids != nil && len(ids) >= int(GUILD_LOG_MAX_NUM) {
		for _, id := range ids {
			if min_id == 0 || id < min_id {
				min_id = id
			}
		}
		if min_id > 0 {
			guild.Logs.Remove(min_id)
		}
	}

	guild.Logs.Add(&dbGuildLogData{
		Time:     int32(time.Now().Unix()),
		LogType:  log_type,
		PlayerId: player_id,
	})
}

func (g *Player) _format_guild_info_to_msg(guild *dbGuildRow) (msg *msg_client_message.GuildInfo) {
	level := guild.GetLevel()
	if level <= 0 {
		level = 1
		guild.SetLevel(level)
	}
	var dismiss_remain_seconds, sign_remain_seconds, ask_donate_remain_seconds, donate_reset_remain_seconds int32
	dismiss_remain_seconds = _guild_get_dismiss_remain_seconds(guild)
	sign_remain_seconds = utils.GetRemainSeconds2NextDayTime(g.db.Guild.GetSignTime(), global_config.GuildSignRefreshTime)
	ask_donate_remain_seconds = GetRemainSeconds(g.db.Guild.GetLastAskDonateTime(), global_config.GuildAskDonateCDSecs)
	donate_reset_remain_seconds = utils.GetRemainSeconds2NextDayTime( /*guild.GetLastDonateRefreshTime()*/ g.db.Guild.GetLastDonateTime(), global_config.GuildDonateRefreshTime)
	president_id := guild.GetPresident()
	var president_name string
	president := player_mgr.GetPlayerById(president_id)
	if president != nil {
		president_name = president.db.GetName()
	}
	msg = &msg_client_message.GuildInfo{
		Id:                       guild.GetId(),
		Name:                     guild.GetName(),
		Level:                    level,
		Exp:                      guild.GetExp(),
		Logo:                     guild.GetLogo(),
		Anouncement:              guild.GetAnouncement(),
		DismissRemainSeconds:     dismiss_remain_seconds,
		SignRemainSeconds:        sign_remain_seconds,
		AskDonateRemainSeconds:   ask_donate_remain_seconds,
		DonateResetRemainSeconds: donate_reset_remain_seconds,
		President:                president_id,
		PresidentName:            president_name,
		MemberNum:                guild.Members.NumAll(),
		MemberNumLimit:           _guild_member_num_limit(guild),
		Position:                 g.db.Guild.GetPosition(),
		DonateNum:                g.db.Guild.GetDonateNum(),
		MaxDonateNum:             global_config.GuildDonateLimitDay,
	}
	return
}

// 公会基本数据
func (g *Player) send_guild_data() int32 {
	need_level := system_unlock_table_mgr.GetUnlockLevel("GuildEnterLevel")
	if need_level > g.db.Info.GetLvl() {
		log.Error("Player[%v] level not enough level %v enter guild", g.Id, need_level)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_NOT_ENOUGH_LEVEL_TO_OPEN)
	}
	guild_id := g.db.Guild.GetId()
	if guild_id <= 0 {
		log.Error("Player[%v] no guild data", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_NOT_JOINED)
	}
	guild := guild_manager.GetGuild(guild_id)
	if guild == nil {
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	response := &msg_client_message.S2CGuildDataResponse{
		Info: g._format_guild_info_to_msg(guild),
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_DATA_RESPONSE), response)

	log.Trace("Player[%v] guild data %v", g.Id, response)

	g.send_guild_donate_list(guild)

	return 1
}

// 公会推荐
func (g *Player) guild_recommend() int32 {
	if g.db.Info.GetLvl() < global_config.GuildOpenLevel {
		log.Error("Player[%v] level not enough to open guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_NOT_ENOUGH_LEVEL_TO_OPEN)
	}

	err, gids := guild_manager.Recommend(g.Id)
	if err < 0 {
		return err
	}

	guilds_msg := _format_guilds_base_info_to_msg(gids)

	response := &msg_client_message.S2CGuildRecommendResponse{
		InfoList: guilds_msg,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_RECOMMEND_RESPONSE), response)

	log.Trace("Player[%v] recommend guilds %v", g.Id, response)

	return 1
}

// 公会搜索
func (g *Player) guild_search(key string) int32 {
	if g.db.Info.GetLvl() < global_config.GuildOpenLevel {
		log.Error("Player[%v] level not enough to open guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_NOT_ENOUGH_LEVEL_TO_OPEN)
	}

	if g.db.Guild.GetId() > 0 {
		log.Error("Player[%v] already joined one guild, cant search", g.Id)
		return -1
	}

	var guilds_msg []*msg_client_message.GuildBaseInfo
	guild_ids := guild_manager.Search(key)
	if guild_ids != nil {
		guilds_msg = _format_guilds_base_info_to_msg(guild_ids)
	} else {
		guilds_msg = make([]*msg_client_message.GuildBaseInfo, 0)
	}
	response := &msg_client_message.S2CGuildSearchResponse{
		InfoList: guilds_msg,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_SEARCH_RESPONSE), response)

	log.Trace("Player[%v] searched guild %v with key %v", g.Id, response, key)

	return 1
}

// 公会创建
func (g *Player) guild_create(name, anouncement string, logo int32) int32 {
	if g.db.Info.GetLvl() < global_config.GuildOpenLevel {
		log.Error("Player[%v] cant create guild because level not enough", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_NOT_ENOUGH_LEVEL_TO_OPEN)
	}

	if g.get_diamond() < global_config.GuildCreateCostGem {
		log.Error("Player[%v] create guild need diamond %v, but only %v", g.Id, global_config.GuildCreateCostGem, g.get_diamond())
		return int32(msg_client_message.E_ERR_PLAYER_DIAMOND_NOT_ENOUGH)
	}

	guild_id := guild_manager.CreateGuild(g.Id, name, logo)
	if guild_id < 0 {
		log.Error("Player[%v] create guild failed, err %v", g.Id, guild_id)
		return guild_id
	}

	g.add_diamond(-global_config.GuildCreateCostGem)

	guild := guild_manager.GetGuild(guild_id)
	if guild == nil {
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}
	guild.SetAnouncement(anouncement)

	guild_msg := g._format_guild_info_to_msg(guild)
	response := &msg_client_message.S2CGuildCreateResponse{
		Info: guild_msg,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_CREATE_RESPONSE), response)

	// 日志
	push_new_guild_log(guild, GUILD_LOG_TYPE_CREATE, g.Id)

	log.Trace("Player[%v] created guild %v", g.Id, response)

	return 1
}

// 公会解散
func (g *Player) guild_dismiss() int32 {
	guild := guild_manager._get_guild(g.Id, true)
	if guild == nil {
		log.Error("Player[%v] cant dismiss guild because cant get guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_GET_WITH_AUTHORITY)
	}
	if guild.GetExistType() != GUILD_EXIST_TYPE_NONE {
		log.Error("Player[%v] cant dismiss guild because guild exist type is %v", g.Id, guild.GetExistType())
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_STATE_IS_DELETED_OR_DELETING)
	}
	guild.SetDismissTime(int32(time.Now().Unix()))
	guild.SetExistType(GUILD_EXIST_TYPE_WILL_DELETE)
	response := &msg_client_message.S2CGuildDismissResponse{
		RealDismissRemainSeconds: global_config.GuildDismissWaitingSeconds,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_DISMISS_RESPONSE), response)

	log.Trace("Player[%v] dismiss guild %v", g.Id, response)

	return 1
}

// 公会取消解散
func (g *Player) guild_cancel_dismiss() int32 {
	guild := guild_manager._get_guild(g.Id, true)
	if guild == nil {
		log.Error("Player[%v] cant cancel dismissing guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_GET_WITH_AUTHORITY)
	}
	if guild.GetExistType() != GUILD_EXIST_TYPE_WILL_DELETE {
		log.Error("Player[%v] cant cancel dismissing guild because guild exit type is %v", g.Id, guild.GetExistType())
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_STATE_IS_NOT_DELETING)
	}
	guild.SetDismissTime(0)
	guild.SetExistType(GUILD_EXIST_TYPE_NONE)
	response := &msg_client_message.S2CGuildCancelDismissResponse{}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_CANCEL_DISMISS_RESPONSE), response)

	log.Trace("Player[%v] cancelled dismiss guild", g.Id)

	return 1
}

// 公会修改名称或logo
func (g *Player) guild_info_modify(name string, logo int32) int32 {
	guild := guild_manager._get_guild(g.Id, true)
	if guild == nil {
		log.Error("Player[%v] cant get guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_GET_WITH_AUTHORITY)
	}

	var cost_diamond int32
	if name != "" {
		if g.get_diamond() < global_config.GuildChangeNameCostGem {
			log.Error("Player[%v] diamond not enough, change name failed", g.Id)
			return int32(msg_client_message.E_ERR_PLAYER_DIAMOND_NOT_ENOUGH)
		}
		if !guild_manager._change_name(guild.GetId(), name) {
			log.Error("Player[%v] change guild name %v to new %v failed", g.Id, guild.GetName(), name)
			return int32(msg_client_message.E_ERR_PLAYER_GUILD_CHANGE_NAME_FAILED)
		}
		cost_diamond = global_config.GuildChangeNameCostGem
		g.add_diamond(-cost_diamond)
	}

	if logo != 0 {
		guild.SetLogo(logo)
	}

	response := &msg_client_message.S2CGuildInfoModifyResponse{
		NewGuildName: name,
		NewGuildLogo: logo,
		CostDiamond:  cost_diamond,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_INFO_MODIFY_RESPONSE), response)

	log.Trace("Player[%v] modified guild info %v", g.Id, response)

	return 1
}

// 公会公告
func (g *Player) guild_anouncement(content string) int32 {
	guild := guild_manager._get_guild(g.Id, true)
	if guild == nil {
		log.Error("Player[%v] cant get guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_GET_WITH_AUTHORITY)
	}

	guild.SetAnouncement(content)
	response := &msg_client_message.S2CGuildAnouncementResponse{
		Content: content,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_ANOUNCEMENT_RESPONSE), response)

	return 1
}

func (g *Player) _get_offline_seconds() int32 {
	var offline_seconds int32
	if atomic.LoadInt32(&g.is_login) == 0 {
		last_login := g.db.Info.GetLastLogin()
		last_logout := g.db.Info.GetLastLogout()
		if last_logout < last_login {
			return 0
		}
		now_time := int32(time.Now().Unix())
		offline_seconds = now_time - last_logout
		if offline_seconds < 0 {
			offline_seconds = 0
		}
	}
	return offline_seconds
}

func (g *Player) _format_guild_member_to_msg() (msg *msg_client_message.GuildMember) {
	next_sign_remain_seconds := utils.GetRemainSeconds2NextDayTime(g.db.Guild.GetSignTime(), global_config.GuildSignRefreshTime)
	msg = &msg_client_message.GuildMember{
		Id:                    g.Id,
		Name:                  g.db.GetName(),
		Level:                 g.db.Info.GetLvl(),
		Head:                  g.db.Info.GetHead(),
		Position:              g.db.Guild.GetPosition(),
		LastOnlineTime:        g._get_offline_seconds(),
		NextSignRemainSeconds: next_sign_remain_seconds,
		JoinTime:              g.db.Guild.GetJoinTime(),
	}
	return
}

// 公会成员列表
func (g *Player) guild_members_list() int32 {
	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		log.Error("Player[%v] no guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	var members_msg []*msg_client_message.GuildMember
	member_ids := guild.Members.GetAllIndex()
	for _, mid := range member_ids {
		mem := player_mgr.GetPlayerById(mid)
		if mem == nil {
			guild.Members.Remove(mid)
			continue
		}
		msg := mem._format_guild_member_to_msg()
		members_msg = append(members_msg, msg)
	}

	response := &msg_client_message.S2CGuildMemebersResponse{
		Members: members_msg,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_MEMBERS_RESPONSE), response)

	log.Trace("Player[%v] get guild[%v] members %v", g.Id, guild.GetId(), response)

	return 1
}

// 公会申请加入
func (g *Player) guild_ask_join(guild_id int32) int32 {
	last_quit_time := g.db.Guild.GetQuitTime()
	if last_quit_time > 0 {
		now_time := int32(time.Now().Unix())
		if now_time-last_quit_time < global_config.GuildQuitAskJoinCDSecs {
			log.Error("Player[%v] is already in cool down to last quit", g.Id)
			return int32(msg_client_message.E_ERR_PLAYER_GUILD_JOIN_NEED_COOLDOWN)
		}
	}

	if _player_get_guild_id(g.Id) > 0 {
		log.Error("Player[%v] already joined guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_ALREADY_CREATED_OR_JOINED)
	}

	guild := guild_manager.GetGuild(guild_id)
	if guild == nil {
		log.Error("Player[%v] ask join guild[%v] not found", g.Id, guild_id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	if guild.Members.HasIndex(g.Id) {
		log.Error("Player[%v] already joined guild %v", g.Id, guild_id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_IS_ALREADY_MEMBER)
	}

	if guild.AskLists.HasIndex(g.Id) {
		log.Warn("Player[%v] already asked join guild %v", g.Id, guild_id)
	} else {
		guild.AskLists.Add(&dbGuildAskListData{
			PlayerId: g.Id,
		})
	}

	response := &msg_client_message.S2CGuildAskJoinResponse{
		GuildId: guild_id,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_ASK_JOIN_RESPONSE), response)

	log.Trace("Player[%v] asked join guild %v", g.Id, guild_id)

	return 1
}

// 公会同意申请加入
func (g *Player) guild_agree_join(player_ids []int32, is_refuse bool) int32 {
	if len(player_ids) == 0 {
		return -1
	}

	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		log.Error("Player[%v] cant get guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	if !is_refuse {
		// 升级配置数据
		levelup_data := guild_levelup_table_mgr.Get(guild.GetLevel())
		if levelup_data == nil {
			log.Error("Guild level up table data not found with level %v", guild.GetLevel())
			return int32(msg_client_message.E_ERR_PLAYER_GUILD_LEVELUP_TABLE_DATA_NOT_FOUND)
		}

		// 人数限制
		if levelup_data.MemberNum < guild.Members.NumAll()+int32(len(player_ids)) {
			log.Error("Guild %v members num is max, player %v cant agree the players %v join", guild.GetId(), g.Id, player_ids)
			return int32(msg_client_message.E_ERR_PLAYER_GUILD_MEMBER_NUM_LIMITED)
		}
	}

	// 职位
	if g.db.Guild.GetPosition() <= GUILD_POSITION_MEMBER {
		log.Error("Player[%v] no authority to agree new member join", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_GET_WITH_AUTHORITY)
	}

	player2res := make(map[int32]int32)
	for _, player_id := range player_ids {
		player2res[player_id] = 1

		if is_refuse {
			guild.AskLists.Remove(player_id)
			continue
		}

		player := player_mgr.GetPlayerById(player_id)
		if player == nil {
			player2res[player_id] = int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
			log.Error("Player[%v] not found", player_id)
			continue
		}

		// 是否已申请
		if !guild.AskLists.HasIndex(player_id) {
			player2res[player_id] = int32(msg_client_message.E_ERR_PLAYER_GUILD_NOT_ASK_JOIN)
			log.Error("Player[%v] not found in guild[%v] ask list", player_id, guild.GetId())
			continue
		}

		// 是否已是工会成员
		if guild.Members.HasIndex(player_id) {
			player2res[player_id] = int32(msg_client_message.E_ERR_PLAYER_GUILD_IS_ALREADY_MEMBER)
			log.Error("Player[%v] already joined guild", player_id)
			continue
		}

		player.join_guild_locker.Lock()

		// 是否已是其他工会的成员
		if player.db.Guild.GetId() > 0 {
			player.join_guild_locker.Unlock()
			player2res[player_id] = int32(msg_client_message.E_ERR_PLAYER_GUILD_ALREADY_CREATED_OR_JOINED)
			log.Error("Player[%v] already joined other guild", player_id)
			continue
		}

		if !is_refuse {
			guild.Members.Add(&dbGuildMemberData{
				PlayerId: player_id,
			})
			player.db.Guild.SetId(guild.GetId())
			player.db.Guild.SetJoinTime(int32(time.Now().Unix()))

			// 日志
			push_new_guild_log(guild, GUILD_LOG_TYPE_MEMBER_JOIN, player_id)
		}

		player.join_guild_locker.Unlock()

		guild.AskLists.Remove(player_id)
	}

	response := &msg_client_message.S2CGuildAgreeJoinResponse{
		Player2Res: Map2ItemInfos(player2res),
		IsRefuse:   is_refuse,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_AGREE_JOIN_RESPONSE), response)

	if !is_refuse {
		// 通知加入的成员
		for _, player_id := range player_ids {
			player := player_mgr.GetPlayerById(player_id)
			if player == nil {
				continue
			}

			notify := &msg_client_message.S2CGuildAgreeJoinNotify{
				NewMemberId: player_id,
				GuildId:     guild.GetId(),
			}
			player.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_AGREE_JOIN_NOTIFY), notify)
			log.Trace("Player[%v] agreed player[%v] join guild %v", g.Id, player_id, guild.GetId())
		}
	}

	if !is_refuse {
		log.Trace("Player[%v] agreed players %v join guild %v", g.Id, player_ids, guild.GetId())
	} else {
		log.Trace("Player[%v] refused players %v ask to join guild %v", g.Id, player_ids, guild.GetId())
	}

	return 1
}

// 公会申请列表
func (g *Player) guild_ask_list() int32 {
	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		log.Error("Player[%v] cant get guild or guild not found", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	if g.db.Guild.GetPosition() <= GUILD_POSITION_MEMBER {
		log.Error("Player[%v] no authority to get ask list", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_GET_WITH_AUTHORITY)
	}

	var info_list []*msg_client_message.PlayerInfo
	ids := guild.AskLists.GetAllIndex()
	if ids != nil {
		info_list = _format_players_info(ids)
	}

	response := &msg_client_message.S2CGuildAskListResponse{
		AskList: info_list,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_ASK_LIST_RESPONSE), response)

	log.Trace("Player[%v] get ask list %v", g.Id, response)

	return 1
}

func (g *Player) clear_guild_data() {
	g.db.Guild.SetId(0)
	g.db.Guild.SetPosition(0)
	//g.db.Guild.SetDonateNum(0)
	//g.db.Guild.SetSignTime(0)
	g.db.Guild.SetJoinTime(0)
	//g.db.Guild.SetLastAskDonateTime(0)
	g.db.Guild.SetQuitTime(int32(time.Now().Unix()))
}

// 退出公会
func (g *Player) guild_quit() int32 {
	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		log.Error("Player[%v] cant get guild or guild not found", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	if guild.GetPresident() == g.Id {
		log.Error("Player[%v] is president, cant quit guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_PRESIDENT_CANT_QUIT)
	}

	guild.Members.Remove(g.Id)
	g.clear_guild_data()

	response := &msg_client_message.S2CGuildQuitResponse{
		RejoinRemainSeconds: global_config.GuildQuitAskJoinCDSecs,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_QUIT_RESPONSE), response)

	// 日志
	push_new_guild_log(guild, GUILD_LOG_TYPE_MEMBER_QUIT, g.Id)

	log.Trace("Player[%v] quit guild %v, rejoin remain seconds %v", g.Id, guild.GetId(), response.GetRejoinRemainSeconds())

	return 1
}

// 公会日志
func (g *Player) guild_logs() int32 {
	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		log.Error("Player[%v] cant get guild or guild not found", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	var logs []*msg_client_message.GuildLog
	log_ids := guild.Logs.GetAllIndex()
	for _, log_id := range log_ids {
		player_id, _ := guild.Logs.GetPlayerId(log_id)
		player := player_mgr.GetPlayerById(player_id)
		if player == nil {
			continue
		}
		log_type, _ := guild.Logs.GetLogType(log_id)
		log_time, _ := guild.Logs.GetTime(log_id)
		log := &msg_client_message.GuildLog{
			Id:         log_id,
			Type:       log_type,
			Time:       log_time,
			PlayerId:   player_id,
			PlayerName: player.db.GetName(),
		}
		logs = append(logs, log)
	}

	response := &msg_client_message.S2CGuildLogsResponse{
		Logs: logs,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_LOGS_RESPONSE), response)

	log.Trace("Player[%v] get guild logs %v", g.Id, response)

	return 1
}

// 公会增加经验
func guild_add_exp(guild *dbGuildRow, add_exp int32) (level, exp int32, is_levelup bool) {
	old_level := guild.GetLevel()
	level = old_level
	if level <= 0 {
		level = 1
		guild.SetLevel(level)
	}
	exp = guild.GetExp() + add_exp

	for {
		level_data := guild_levelup_table_mgr.Get(level)
		if level_data == nil || level_data.Exp <= 0 {
			break
		}
		if level_data.Exp > exp {
			break
		}
		exp -= level_data.Exp
		level += 1
	}

	if level != old_level {
		guild.SetLevel(level)
	}
	guild.SetExp(exp)

	if level > old_level {
		is_levelup = true
	}

	return
}

// 可以签到
func (g *Player) guild_can_sign_in() (int32, *dbGuildRow) {
	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		//log.Error("Player[%v] cant get guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND), nil
	}

	if !utils.CheckDayTimeArrival(g.db.Guild.GetSignTime(), global_config.GuildSignRefreshTime) {
		//log.Error("Player[%v] cant sign in guild, time not arrival", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_SIGN_IN_IS_COOLDOWN), nil
	}
	return 1, guild
}

// 公会签到
func (g *Player) guild_sign_in() int32 {
	res, guild := g.guild_can_sign_in()
	if res < 0 || guild == nil {
		return res
	}
	now_time := int32(time.Now().Unix())
	g.db.Guild.SetSignTime(now_time)
	// 奖励
	if global_config.GuildSignReward != nil {
		for i := 0; i < len(global_config.GuildSignReward)/2; i++ {
			rid := global_config.GuildSignReward[2*i]
			rnum := global_config.GuildSignReward[2*i+1]
			g.add_resource(rid, rnum)
		}
	}
	// 增加经验
	level, exp, is_levelup := guild_add_exp(guild, global_config.GuildSignAddExp)

	next_remain_seconds := utils.GetRemainSeconds2NextDayTime(now_time, global_config.GuildSignRefreshTime)
	response := &msg_client_message.S2CGuildSignInResponse{
		NextSignInRemainSeconds: next_remain_seconds,
		RewardItems:             global_config.GuildSignReward,
		GuildLevel:              level,
		GuildExp:                exp,
		IsLevelup:               is_levelup,
		MemberNumLimit:          guild_levelup_table_mgr.GetMemberNumLimit(level),
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_SIGN_IN_RESPONSE), response)

	log.Trace("Player[%v] sign in guild[%v]", g.Id, guild.GetId())

	return 1
}

// 公会任免官员
func (g *Player) guild_set_officer(player_ids []int32, set_type int32) int32 {
	// 只有会长有权限
	guild := guild_manager._get_guild(g.Id, true)
	if guild == nil {
		log.Error("Player[%v] cant get guild or guild not exist", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_SET_OFFICER_ONLY_PRESIDENT)
	}

	var position int32
	if set_type == 1 {
		position = GUILD_POSITION_OFFICER
	} else if set_type == 2 {
		position = GUILD_POSITION_MEMBER
	}

	for i, player_id := range player_ids {
		if player_id == g.Id {
			player_ids[i] = 0
			continue
		}
		if !guild.Members.HasIndex(player_id) {
			player_ids[i] = 0
			log.Error("Player[%v] is not member of guild %v", player_id, guild.GetId())
			continue
		}

		player := player_mgr.GetPlayerById(player_id)
		if player == nil {
			player_ids[i] = 0
			log.Error("Player[%v] not found", player_id)
			continue
		}

		player.db.Guild.SetPosition(position)
	}

	response := &msg_client_message.S2CGuildSetOfficerResponse{
		PlayerIds: player_ids,
		SetType:   set_type,
		Position:  position,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_SET_OFFICER_RESPONSE), response)

	// 通知被任免成员
	notify := &msg_client_message.S2CGuildSetOfficerNotify{
		SetType:     set_type,
		NewPosition: position,
	}
	for _, player_id := range player_ids {
		player := player_mgr.GetPlayerById(player_id)
		if player == nil {
			continue
		}
		notify.MemberId = player_id
		player.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_SET_OFFICER_NOTIFY), notify)

		// 日志
		if set_type == 1 {
			push_new_guild_log(guild, GUILD_LOG_TYPE_MEMBER_APPOINT, player_id)
		} else if set_type == 2 {
			push_new_guild_log(guild, GUILD_LOG_TYPE_OFFICER_DISMISS, player_id)
		}
	}

	log.Trace("Player[%v] set officer %v in guild %v", g.Id, response, guild.GetId())

	return 1
}

// 公会驱逐会员
func (g *Player) guild_kick_member(player_ids []int32) int32 {
	if player_ids == nil {
		return -1
	}

	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		log.Error("Player[%v] cant get guild or guild not exist", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	if g.db.Guild.GetPosition() <= GUILD_POSITION_MEMBER {
		log.Error("Player[%v] position %v no authority to kick member", g.Id, g.db.Guild.GetPosition())
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_GET_WITH_AUTHORITY)
	}

	var player_res []int32 = make([]int32, len(player_ids))
	for i, player_id := range player_ids {
		if player_id == g.Id {
			player_res[i] = -1
			continue
		}
		if !guild.Members.HasIndex(player_id) {
			player_res[i] = int32(msg_client_message.E_ERR_PLAYER_GUILD_NOT_JOINED)
			log.Error("Player[%v] is not member of guild[%v]", player_id, guild.GetId())
			continue
		}
		player := player_mgr.GetPlayerById(player_id)
		if player == nil {
			player_res[i] = int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
			continue
		}
		if player.db.Guild.GetPosition() != GUILD_POSITION_MEMBER {
			player_res[i] = int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_DISMISS_OFFICER)
			continue
		}
		guild.Members.Remove(player_id)
		player.clear_guild_data()
	}

	response := &msg_client_message.S2CGuildKickMemberResponse{
		PlayerIds:  player_ids,
		Player2Res: player_res,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_KICK_MEMBER_RESPONSE), response)

	// 通知被驱逐成员
	notify := &msg_client_message.S2CGuildKickMemberNotify{}
	for _, player_id := range player_ids {
		player := player_mgr.GetPlayerById(player_id)
		if player == nil {
			continue
		}
		notify.MemberId = player_id
		player.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_KICK_MEMBER_NOTIFY), notify)

		log.Debug("Player[%v] dismissed guild member %v to notify", g.Id, player_id)

		// 日志
		push_new_guild_log(guild, GUILD_LOG_TYPE_MEMBER_KICK, player_id)
	}

	log.Trace("Player[%v] kick members %v from guild %v", g.Id, player_ids, guild.GetId())

	return 1
}

// 公会转让会长
func (g *Player) guild_change_president(player_id int32) int32 {
	if player_id == g.Id {
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_CHANGE_PRESIDENT_SELF)
	}

	player := player_mgr.GetPlayerById(player_id)
	if player == nil {
		log.Error("Player[%v] not found", player_id)
		return int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
	}

	guild := guild_manager._get_guild(g.Id, true)
	if guild == nil {
		log.Error("Player[%v] cant get guild or guild not exist", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_GET_WITH_AUTHORITY)
	}

	if !guild.Members.HasIndex(player_id) {
		log.Error("Guild %v no member %v, cant change president", guild.GetId(), player_id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_NOT_JOINED)
	}

	guild.SetPresident(player_id)
	g.db.Guild.SetPosition(GUILD_POSITION_MEMBER)
	player.db.Guild.SetPosition(GUILD_POSITION_PRESIDENT)

	response := &msg_client_message.S2CGuildChangePresidentResponse{
		NewPresidentId: player_id,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_CHANGE_PRESIDENT_RESPONSE), response)

	// 通知新会长
	notify := &msg_client_message.S2CGuildChangePresidentNotify{
		OldPresidentId: g.Id,
		NewPresidentId: player_id,
	}
	player.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_CHANGE_PRESIDENT_NOTIFY), notify)

	// 日志
	push_new_guild_log(guild, GUILD_LOG_TYPE_PRESIDENT_CHANGE, player_id)

	log.Trace("Player[%v] change guild %v president to %v", g.Id, guild.GetId(), player_id)

	return 1
}

// 公会招募
func (g *Player) guild_recruit(content []byte) int32 {
	guild_id := g.db.Guild.GetId()
	if guild_id <= 0 {
		log.Error("Player[%v] no join in guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_NOT_JOINED)
	}

	position := g.db.Guild.GetPosition()
	if position <= GUILD_POSITION_MEMBER {
		log.Error("Player[%v] recruit in guild %v failed, position %v not enough", g.Id, guild_id, position)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_GET_WITH_AUTHORITY)
	}

	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		log.Error("Player[%v] cant get guild", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	now_time := int32(time.Now().Unix())
	last_recruit_time := guild.GetLastRecruitTime()
	if (now_time - last_recruit_time) < global_config.RecruitChatData.SendMsgCooldown {
		log.Error("Player[%v] recruit too frequently", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_RECRUIT_IS_COOLDOWN)
	}

	res := g.chat(CHAT_CHANNEL_RECRUIT, content, 0)
	if res < 0 {
		return res
	}

	guild.SetLastRecruitTime(now_time)

	response := &msg_client_message.S2CGuildRecruitResponse{
		Content: content,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_RECRUIT_RESPONSE), response)

	log.Trace("Player[%v] recruit with content[%v]", g.Id, content)

	return 1
}

func (g *Player) send_guild_donate_list(guild *dbGuildRow) {
	var donate_list []*msg_client_message.GuildAskDonateInfo
	player_ids := guild.AskDonates.GetAllIndex()
	for _, player_id := range player_ids {
		player := player_mgr.GetPlayerById(player_id)
		if player == nil {
			continue
		}

		item_id, _ := guild.AskDonates.GetItemId(player_id)
		item_num, _ := guild.AskDonates.GetItemNum(player_id)
		ask_time, _ := guild.AskDonates.GetAskTime(player_id)
		name, level, head := GetPlayerBaseInfo(player_id)
		remain_exist_seconds := GetRemainSeconds(ask_time, global_config.GuildAskDonateCDSecs)
		donate_item := &msg_client_message.GuildAskDonateInfo{
			PlayerId:           player_id,
			PlayerName:         name,
			PlayerHead:         head,
			PlayerLevel:        level,
			ItemId:             item_id,
			ItemNum:            item_num,
			AskTime:            ask_time,
			RemainExistSeconds: remain_exist_seconds,
		}
		donate_list = append(donate_list, donate_item)
	}

	response := &msg_client_message.S2CGuildDonateListResponse{
		InfoList: donate_list,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_DONATE_LIST_RESPONSE), response)

	log.Trace("Player[%v] get donate list %v", g.Id, response)
}

// 检测捐赠列表
func guild_check_donate_list(guild *dbGuildRow) (changed bool) {
	all_ids := guild.AskDonates.GetAllIndex()
	if all_ids == nil {
		return
	}

	var notify msg_client_message.S2CGuildDonateItemNotify
	for _, player_id := range all_ids {
		ask_time, _ := guild.AskDonates.GetAskTime(player_id)
		// 超时就删除
		if GetRemainSeconds(ask_time, global_config.GuildAskDonateCDSecs) <= 1 {

			// 通知被捐赠者
			player := player_mgr.GetPlayerById(player_id)
			if player == nil {
				continue
			}

			item_id, _ := guild.AskDonates.GetItemId(player_id)
			item_num, _ := guild.AskDonates.GetItemNum(player_id)
			player.add_resource(item_id, item_num)
			guild.AskDonates.Remove(player_id)

			notify.ItemNum = item_num
			notify.ItemId = item_id
			notify.DonateOver = false
			player.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_DONATE_ITEM_NOTIFY), &notify)

			changed = true
		}
	}
	return
}

// 公会捐赠刷新
func (g *Player) guild_check_donate_refresh() bool {
	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		return false
	}
	last_refresh_time := g.db.Guild.GetLastDonateTime()
	if !utils.CheckDayTimeArrival(last_refresh_time, global_config.GuildDonateRefreshTime) {
		if GetRemainSeconds(last_refresh_time, global_config.GuildAskDonateExistSeconds) > 1 {
			return false
		}
	}
	g.db.Guild.SetLastDonateTime(int32(time.Now().Unix()))
	g.db.Guild.SetDonateNum(0)
	g.send_guild_data()
	return true
}

// 公会捐赠列表
func (g *Player) guild_donate_list() int32 {
	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		log.Error("Player[%v] cant get guild or guild not exist", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	guild_check_donate_list(guild)

	g.send_guild_donate_list(guild)

	return 1
}

// 公会请求捐赠
func (g *Player) guild_ask_donate(item_id int32) int32 {
	item := guild_donate_table_mgr.Get(item_id)
	if item == nil {
		log.Error("Guild Donate item table not found %v", item_id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DONATE_TABLE_DAT_NOT_FOUND)
	}

	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		log.Error("Player[%v] cant get guild or guild not exist", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	guild_check_donate_list(guild)
	g.guild_check_donate_refresh()

	ask_time := g.db.Guild.GetLastAskDonateTime()
	if GetRemainSeconds(ask_time, global_config.GuildAskDonateCDSecs) > 1 {
		log.Error("Player[%v] ask donate is cooling down", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_ALREADY_ASKED_DONATE)
	}

	if guild.AskDonates.HasIndex(g.Id) {
		log.Error("Player[%v] already asked donate", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_ALREADY_ASKED_DONATE)
	}

	guild.AskDonates.Add(&dbGuildAskDonateData{
		PlayerId: g.Id,
		ItemId:   item_id,
		ItemNum:  0,
		AskTime:  int32(time.Now().Unix()),
	})
	g.db.Guild.SetLastAskDonateTime(int32(time.Now().Unix()))

	response := &msg_client_message.S2CGuildAskDonateResponse{
		ItemId:               item_id,
		ItemNum:              item.RequestNum,
		NextAskRemainSeconds: global_config.GuildAskDonateCDSecs,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_ASK_DONATE_RESPONSE), response)

	log.Trace("Player[%v] asked donate %v", g.Id, response)

	return 1
}

// 公会捐赠
func (g *Player) guild_donate(player_id int32) int32 {
	if g.Id == player_id {
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_CANT_ASK_DONATE_TO_SELF)
	}

	player := player_mgr.GetPlayerById(player_id)
	if player == nil {
		log.Error("Player[%v] not exist", player_id)
		return int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
	}

	guild := guild_manager._get_guild(g.Id, false)
	if guild == nil {
		log.Error("Player[%v] cant get guild or guild not exist", g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
	}

	guild_check_donate_list(guild)
	g.guild_check_donate_refresh()

	if !guild.AskDonates.HasIndex(player_id) {
		log.Error("Player[%v] no ask donate, player[%v] cant donate", player_id, g.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_NOT_ASK_DONATE)
	}

	item_id, _ := guild.AskDonates.GetItemId(player_id)
	item := guild_donate_table_mgr.Get(item_id)
	if item == nil {
		log.Error("Guild Donate item table not found %v", item_id)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_DONATE_TABLE_DAT_NOT_FOUND)
	}

	// 捐献次数（分数）
	donate_num := g.db.Guild.GetDonateNum()
	if donate_num+item.LimitScore > global_config.GuildDonateLimitDay {
		log.Error("Player[%v] left donate score %v not enough to donate", g.Id, global_config.GuildDonateLimitDay-donate_num)
		return int32(msg_client_message.E_ERR_PLAYER_GUILD_NOT_ENOUGH_DONATE_SCORE)
	}

	if g.get_resource(item_id) < 1 {
		log.Error("Player[%v] not enough %v to donate", g.Id, item_id)
		return int32(msg_client_message.E_ERR_PLAYER_ITEM_NUM_NOT_ENOUGH)
	}

	var donate_over bool
	item_num, _ := guild.AskDonates.GetItemNum(player_id)
	if item_num < item.RequestNum {
		g.add_resource(item_id, -1)
		item_num += 1
		guild.AskDonates.SetItemNum(player_id, item_num)
	}

	if item_num >= item.RequestNum {
		player.add_resource(item_id, item.RequestNum)
		guild.AskDonates.Remove(player_id)
		donate_over = true
	}

	// 已捐赠的分数
	g.db.Guild.SetDonateNum(donate_num + item.LimitScore)

	// 奖励
	if item.DonateRewardItem != nil {
		g.add_resources(item.DonateRewardItem)
	}

	response := &msg_client_message.S2CGuildDonateResponse{
		PlayerId:   player_id,
		ItemId:     item_id,
		ItemNum:    item_num,
		DonateNum:  donate_num + item.LimitScore,
		DonateOver: donate_over,
	}
	g.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_DONATE_RESPONSE), response)

	// 通知被捐赠者
	notify := &msg_client_message.S2CGuildDonateItemNotify{
		ItemId:     item_id,
		ItemNum:    response.GetItemNum(),
		DonateOver: donate_over,
	}
	player.Send(uint16(msg_client_message_id.MSGID_S2C_GUILD_DONATE_ITEM_NOTIFY), notify)

	log.Trace("Player[%v] donate to player[%v] result %v", g.Id, player_id, response)

	return 1
}

func C2SGuildDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%s)", err.Error())
		return -1
	}
	return p.send_guild_data()
}

func C2SGuildRecommendHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildRecommendRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_recommend()
}

func C2SGuildSearchHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildSearchRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_search(req.GetKey())
}

func C2SGuildCreateHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildCreateRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}

	return p.guild_create(req.GetGuildName(), string(req.GetAnouncement()), req.GetGuildLogo())
}

func C2SGuildDismissHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildDismissRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}

	return p.guild_dismiss()
}

func C2SGuildCancelDismissHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildCancelDismissRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_cancel_dismiss()
}

func C2SGuildInfoModifyHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildInfoModifyRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}

	return p.guild_info_modify(req.GetNewGuildName(), req.GetNewGuildLogo())
}

func C2SGuildSetAnouncementHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildAnouncementRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_anouncement(req.GetContent())
}

func C2SGuildMembersHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildMembersRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}

	return p.guild_members_list()
}

func C2SGuildAskJoinHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildAskJoinRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}

	return p.guild_ask_join(req.GetGuildId())
}

func C2SGuildAgreeJoinHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildAgreeJoinRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_agree_join(req.GetPlayerIds(), req.GetIsRefuse())
}

func C2SGuildAskListHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildAskListRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_ask_list()
}

func C2SGuildQuitHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildQuitRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_quit()
}

func C2SGuildLogsHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildLogsRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_logs()
}

func C2SGuildSignInHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildSignInRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_sign_in()
}

func C2SGuildSetOfficerHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildSetOfficerRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_set_officer(req.GetPlayerIds(), req.GetSetType())
}

func C2SGuildKickMemberHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildKickMemberRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_kick_member(req.GetPlayerIds())
}

func C2SGuildChangePresidentHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildChangePresidentRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_change_president(req.GetNewPresidentId())
}

func C2SGuildRecruitHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildRecruitRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_recruit(req.GetContent())
}

func C2SGuildDonateListHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildDonateListRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_donate_list()
}

func C2SGuildAskDonateHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildAskDonateRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_ask_donate(req.GetItemId())
}

func C2SGuildDonateHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuildDonateRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed, err(%v)", err.Error())
		return -1
	}
	return p.guild_donate(req.GetPlayerId())
}
