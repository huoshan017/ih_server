package main

import (
	"ih_server/libs/log"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	"ih_server/src/share_data"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/orcaman/concurrent-map"
)

type PlayerManager struct {
	//uid2players        map[string]*Player
	//uid2players_locker *sync.RWMutex
	uid2players cmap.ConcurrentMap
	//id2players        map[int32]*Player
	//id2players_locker *sync.RWMutex
	id2players cmap.ConcurrentMap
}

var player_mgr PlayerManager

func (pm *PlayerManager) Init() bool {
	//this.uid2players = make(map[string]*Player)
	//this.uid2players_locker = &sync.RWMutex{}
	//this.id2players = make(map[int32]*Player)
	//this.id2players_locker = &sync.RWMutex{}
	pm.uid2players = cmap.New()
	pm.id2players = cmap.New()
	return true
}

func (pm *PlayerManager) GetPlayersNum() int32 {
	//this.uid2players_locker.RLock()
	//defer this.uid2players_locker.RUnlock()
	//return int32(len(this.uid2players))
	return int32(pm.uid2players.Count())
}

func (pm *PlayerManager) GetPlayerById(id int32) *Player {
	//this.id2players_locker.Lock()
	//defer this.id2players_locker.Unlock()
	//return this.id2players[id]
	p, o := pm.id2players.Get(strconv.Itoa(int(id)))
	if !o {
		return nil
	}
	return p.(*Player)
}

func (pm *PlayerManager) Add2IdMap(p *Player) {
	if nil == p {
		log.Error("Player_agent_mgr Add2IdMap p nil !")
		return
	}
	//this.id2players_locker.Lock()
	//defer this.id2players_locker.Unlock()

	id := strconv.Itoa(int(p.Id))
	if _, o := pm.id2players.Get(id); !o {
		log.Error("PlayerManager Add2IdMap already have player(%d)", p.Id)
	}

	pm.id2players.Set(id, p)
}

func (pm *PlayerManager) RemoveFromIdMap(id int32) {
	//this.id2players_locker.Lock()
	//defer this.id2players_locker.Unlock()

	//cur_p := this.id2players[id]
	//if nil != cur_p {
	//	delete(this.id2players, id)
	//}
	pm.id2players.Remove(strconv.Itoa(int(id)))
}

func (pm *PlayerManager) Add2UidMap(unique_id string, p *Player) {
	if unique_id == "" {
		return
	}

	/*this.uid2players_locker.Lock()
	defer this.uid2players_locker.Unlock()

	if this.uid2players[unique_id] != nil {
		log.Warn("UniqueId %v already added", unique_id)
		return
	}

	this.uid2players[unique_id] = p*/
	pm.uid2players.Set(unique_id, p)
}

func (pm *PlayerManager) RemoveFromUidMap(unique_id string) {
	//this.uid2players_locker.Lock()
	//defer this.uid2players_locker.Unlock()

	//delete(this.uid2players, unique_id)
	pm.uid2players.Remove(unique_id)
}

func (pm *PlayerManager) GetPlayerByUid(unique_id string) *Player {
	//this.uid2players_locker.RLock()
	//defer this.uid2players_locker.RUnlock()

	//return this.uid2players[unique_id]
	p, o := pm.uid2players.Get(unique_id)
	if !o {
		return nil
	}
	return p.(*Player)
}

func (pm *PlayerManager) PlayerLogout(p *Player) {
	if nil == p {
		log.Error("PlayerManager PlayerLogout p nil !")
		return
	}

	//this.RemoveFromAccMap(p.Account)
	pm.RemoveFromUidMap(p.UniqueId)

	p.OnLogout(true)
}

func (pm *PlayerManager) OnTick() {

}

//==============================================================================
func (pm *PlayerManager) RegMsgHandler() {
	if !config.DisableTestCommand {
		msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_TEST_COMMAND), C2STestCommandHandler)
	}

	msg_handler_mgr.SetMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ENTER_GAME_REQUEST), C2SEnterGameRequestHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_LEAVE_GAME_REQUEST), C2SLeaveGameRequestHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_HEARTBEAT), C2SHeartbeatHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_DATA_SYNC_REQUEST), C2SDataSyncHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_PLAYER_CHANGE_NAME_REQUEST), C2SPlayerChangeNameHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_PLAYER_CHANGE_HEAD_REQUEST), C2SPlayerChangeHeadHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ACCOUNT_PLAYER_LIST_REQUEST), C2SAccountPlayerListHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_RECONNECT_REQUEST), C2SReconnectHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_BATTLE_RESULT_REQUEST), C2SFightHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_SET_TEAM_REQUEST), C2SSetTeamHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_BATTLE_SET_HANGUP_CAMPAIGN_REQUEST), C2SSetHangupCampaignHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CAMPAIGN_HANGUP_INCOME_REQUEST), C2SCampaignHangupIncomeHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CAMPAIGN_DATA_REQUEST), C2SCampaignDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CAMPAIGN_ACCELERATE_INCOME_REQUEST), C2SCampaignAccelGetIncomeHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CAMPAIGN_ACCELERATE_REFRESH_REQUEST), C2SCampaignAccelNumRefreshHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_ATTRS_REQUEST), C2SRoleAttrsHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_LEVELUP_REQUEST), C2SRoleLevelUpHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_RANKUP_REQUEST), C2SRoleRankUpHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_DECOMPOSE_REQUEST), C2SRoleDecomposeHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_FUSION_REQUEST), C2SRoleFusionHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_LOCK_REQUEST), C2SRoleLockHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_HANDBOOK_REQUEST), C2SRoleHandbookHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_LEFTSLOT_OPEN_REQUEST), C2SRoleLeftSlotOpenHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_ONEKEY_EQUIP_REQUEST), C2SRoleOneKeyEquipHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_ONEKEY_UNEQUIP_REQUEST), C2SRoleOneKeyUnequipHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_LEFTSLOT_RESULT_SAVE_REQUEST), C2SRoleLeftSlotUpgradeSaveHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_LEFTSLOT_RESULT_CANCEL_REQUEST), C2SRoleLeftSlotResultCancelHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_DISPLACE_REQUEST), C2SRoleDisplaceHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ROLE_DISPLACE_CONFIRM_REQUEST), C2SRoleDisplaceConfirmHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ITEM_FUSION_REQUEST), C2SItemFusionHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ITEM_SELL_REQUEST), C2SItemSellHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ITEM_EQUIP_REQUEST), C2SItemEquipHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ITEM_UNEQUIP_REQUEST), C2SItemUnequipHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ITEM_UPGRADE_REQUEST), C2SItemUpgradeHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ITEM_ONEKEY_UPGRADE_REQUEST), C2SItemOneKeyUpgradeHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_MAIL_SEND_REQUEST), C2SMailSendHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_MAIL_LIST_REQUEST), C2SMailListHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_MAIL_DETAIL_REQUEST), C2SMailDetailHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_MAIL_GET_ATTACHED_ITEMS_REQUEST), C2SMailGetAttachedItemsHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_MAIL_DELETE_REQUEST), C2SMailDeleteHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_BATTLE_RECORD_LIST_REQUEST), C2SBattleRecordListHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_BATTLE_RECORD_REQUEST), C2SBattleRecordHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_BATTLE_RECORD_DELETE_REQUEST), C2SBattleRecordDeleteHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_TALENT_UP_REQUEST), C2STalentUpHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_TALENT_LIST_REQUEST), C2STalentListHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_TALENT_RESET_REQUEST), C2STalentResetHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_TOWER_DATA_REQUEST), C2STowerDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_TOWER_RECORDS_INFO_REQUEST), C2STowerRecordsInfoHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_TOWER_RECORD_DATA_REQUEST), C2STowerRecordDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_TOWER_RANKING_LIST_REQUEST), C2STowerRankingListHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_DRAW_CARD_REQUEST), C2SDrawCardHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_DRAW_DATA_REQUEST), C2SDrawDataHandler)

	// ?????????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GOLD_HAND_DATA_REQUEST), C2SGoldHandDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_TOUCH_GOLD_REQUEST), C2STouchGoldHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_SHOP_DATA_REQUEST), C2SShopDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_SHOP_BUY_ITEM_REQUEST), C2SShopBuyItemHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_SHOP_REFRESH_REQUEST), C2SShopRefreshHandler)

	// ?????????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ARENA_DATA_REQUEST), C2SArenaDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ARENA_PLAYER_DEFENSE_TEAM_REQUEST), C2SArenaPlayerDefenseTeamHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ARENA_MATCH_PLAYER_REQUEST), C2SArenaMatchPlayerHandler)

	// ?????????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_RANK_LIST_REQUEST), C2SRankListHandler)

	// ????????????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ACTIVE_STAGE_DATA_REQUEST), C2SActiveStageDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ACTIVE_STAGE_BUY_CHALLENGE_NUM_REQUEST), C2SActiveStageBuyChallengeNumHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ACTIVE_STAGE_ASSIST_ROLE_LIST_REQUEST), C2SActiveStageGetAssistRoleListHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_RECOMMEND_REQUEST), C2SFriendsRecommendHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_LIST_REQUEST), C2SFriendListHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_ASK_PLAYER_LIST_REQUEST), C2SFriendAskListHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_ASK_REQUEST), C2SFriendAskHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_AGREE_REQUEST), C2SFriendAgreeHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_REFUSE_REQUEST), C2SFriendRefuseHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_REMOVE_REQUEST), C2SFriendRemoveHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_GIVE_POINTS_REQUEST), C2SFriendGivePointsHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_GET_POINTS_REQUEST), C2SFriendGetPointsHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_SEARCH_BOSS_REQUEST), C2SFriendSearchBossHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIENDS_BOSS_LIST_REQUEST), C2SFriendGetBossListHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_BOSS_ATTACK_LIST_REQUEST), C2SFriendBossAttackListHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_DATA_REQUEST), C2SFriendDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_SET_ASSIST_ROLE_REQUEST), C2SFriendSetAssistRoleHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_GIVE_AND_GET_POINTS_REQUEST), C2SFriendGiveAndGetPointsHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_FRIEND_GET_ASSIST_POINTS_REQUEST), C2SFriendGetAssistPointsHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_TASK_DATA_REQUEST), C2STaskDataHanlder)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_TASK_REWARD_REQUEST), C2SGetTaskRewardHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_EXPLORE_DATA_REQUEST), C2SExploreDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_EXPLORE_SEL_ROLE_REQUEST), C2SExploreSelRoleHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_EXPLORE_START_REQUEST), C2SExploreStartHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_EXPLORE_SPEEDUP_REQUEST), C2SExploreSpeedupHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_EXPLORE_REFRESH_REQUEST), C2SExploreTasksRefreshHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_EXPLORE_LOCK_REQUEST), C2SExploreTaskLockHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_EXPLORE_GET_REWARD_REQUEST), C2SExploreGetRewardHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_EXPLORE_CANCEL_REQUEST), C2SExploreCancelHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CHAT_REQUEST), C2SChatHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CHAT_MSG_PULL_REQUEST), C2SChatPullMsgHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_DATA_REQUEST), C2SGuildDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_RECOMMEND_REQUEST), C2SGuildRecommendHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_SEARCH_REQUEST), C2SGuildSearchHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_DISMISS_REQUEST), C2SGuildDismissHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_CREATE_REQUEST), C2SGuildCreateHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_CANCEL_DISMISS_REQUEST), C2SGuildCancelDismissHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_INFO_MODIFY_REQUEST), C2SGuildInfoModifyHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_ANOUNCEMENT_REQUEST), C2SGuildSetAnouncementHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_MEMBERS_REQUEST), C2SGuildMembersHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_ASK_JOIN_REQUEST), C2SGuildAskJoinHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_AGREE_JOIN_REQUEST), C2SGuildAgreeJoinHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_ASK_LIST_REQUEST), C2SGuildAskListHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_QUIT_REQUEST), C2SGuildQuitHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_LOGS_REQUEST), C2SGuildLogsHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_SIGN_IN_REQUEST), C2SGuildSignInHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_SET_OFFICER_REQUEST), C2SGuildSetOfficerHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_KICK_MEMBER_REQUEST), C2SGuildKickMemberHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_CHANGE_PRESIDENT_REQUEST), C2SGuildChangePresidentHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_RECRUIT_REQUEST), C2SGuildRecruitHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_DONATE_LIST_REQUEST), C2SGuildDonateListHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_ASK_DONATE_REQUEST), C2SGuildAskDonateHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_DONATE_REQUEST), C2SGuildDonateHandler)

	// ????????????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_STAGE_DATA_REQUEST), C2SGuildStageDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_STAGE_RANK_LIST_REQUEST), C2SGuildStageRankListHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_STAGE_RESET_REQUEST), C2SGuildStageResetHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUILD_STAGE_PLAYER_RESPAWN_REQUEST), C2SGuildStagePlayerRespawnHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_SIGN_DATA_REQUEST), C2SSignDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_SIGN_AWARD_REQUEST), C2SSignAwardHandler)

	// ?????????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_SEVENDAYS_DATA_REQUEST), C2SSevenDaysDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_SEVENDAYS_AWARD_REQUEST), C2SSevenDaysAwardHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CHARGE_DATA_REQUEST), C2SChargeDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CHARGE_REQUEST), C2SChargeHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CHARGE_FIRST_AWARD_REQUEST), C2SChargeFirstAwardHandler)

	// ????????????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_RED_POINT_STATES_REQUEST), C2SRedPointStatesHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_GUIDE_DATA_SAVE_REQUEST), C2SGuideDataSaveHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ACTIVITY_DATA_REQUEST), C2SActivityDataHandler)
	//msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ACTIVITY_EXCHANGE_REQUEST), C2SActivityExchangeHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_EXPEDITION_DATA_REQUEST), C2SExpeditionDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_EXPEDITION_LEVEL_DATA_REQUEST), C2SExpeditionLevelDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_EXPEDITION_PURIFY_REWARD_REQUEST), C2SExpeditionPurifyRewardHandler)

	// ??????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ARTIFACT_DATA_REQUEST), C2SArtifactDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ARTIFACT_UNLOCK_REQUEST), C2SArtifactUnlockHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ARTIFACT_LEVELUP_REQUEST), C2SArtifactLevelUpHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ARTIFACT_RANKUP_REQUEST), C2SArtifactRankUpHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_ARTIFACT_RESET_REQUEST), C2SArtifactResetHandler)

	// ?????????
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CARNIVAL_DATA_REQUEST), C2SCarnivalDataHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CARNIVAL_TASK_SET_REQUEST), C2SCarnivalTaskSetHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CARNIVAL_ITEM_EXCHANGE_REQUEST), C2SCarnivalItemExchangeHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CARNIVAL_SHARE_REQUEST), C2SCarnivalShareHandler)
	msg_handler_mgr.SetPlayerMsgHandler(uint16(msg_client_message_id.MSGID_C2S_CARNIVAL_BE_INVITED_REQUEST), C2SCarnivalBeInvitedHander)
}

func C2SEnterGameRequestHandler(msg_data []byte) (int32, *Player) {
	var p *Player
	var req msg_client_message.C2SEnterGameRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s) !", err.Error())
		return -1, p
	}

	uid := login_token_mgr.GetUidByAccount(req.GetAcc())
	if uid == "" {
		log.Error("PlayerEnterGameHandler account[%v] cant get", req.GetAcc())
		return int32(msg_client_message.E_ERR_PLAYER_TOKEN_ERROR), p
	}

	row := dbc.BanPlayers.GetRow(uid)
	if row != nil && row.GetStartTime() > 0 {
		log.Error("Player unique id %v be banned", uid)
		return int32(msg_client_message.E_ERR_ACCOUNT_BE_BANNED), p
	}

	var is_new bool
	p = player_mgr.GetPlayerByUid(uid)
	if nil == p {
		global_row := dbc.Global.GetRow()
		player_id := global_row.GetNextPlayerId()
		pdb := dbc.Players.AddRow(player_id)
		if nil == pdb {
			log.Error("player_db_to_msg AddRow pid(%d) failed !", player_id)
			return -1, p
		}
		pdb.SetUniqueId(uid)
		pdb.SetAccount(req.GetAcc())
		pdb.SetCurrReplyMsgNum(0)
		p = new_player(player_id, uid, req.GetAcc(), "", pdb)
		p.OnCreate()
		player_mgr.Add2IdMap(p)
		player_mgr.Add2UidMap(uid, p)
		is_new = true
		log.Info("player_db_to_msg new player(%d) !", player_id)
	} else {
		p.Account = req.GetAcc()
		pdb := dbc.Players.GetRow(p.Id)
		if pdb != nil {
			pdb.SetCurrReplyMsgNum(0)
		}
	}

	p.send_enter_game(req.Acc, p.Id)
	p.OnLogin()
	if !is_new {
		p.send_items()
		p.send_roles()
	} else {
		p.check_and_send_items_change()
		p.check_and_send_roles_change()
	}
	p.send_talent_list()
	p.send_info()
	p.send_teams()
	p.send_explore_data()
	p.get_sign_data()
	p.charge_data()
	p.notify_enter_complete()

	log.Info("PlayerEnterGameHandler account[%s]", req.GetAcc())

	return 1, p
}

func C2SLeaveGameRequestHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SLeaveGameRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s) !", err.Error())
		return -1
	}
	p.OnLogout(true)
	return 1
}

func C2SHeartbeatHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SHeartbeat
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s) !", err.Error())
		return -1
	}

	if p.IsOffline() {
		log.Error("Player[%v] is offline", p.Id)
		return int32(msg_client_message.E_ERR_PLAYER_IS_OFFLINE)
	}

	need_level := system_unlock_table_mgr.GetUnlockLevel("TowerEnterLevel")
	if need_level <= p.db.Info.GetLvl() {
		p.check_and_send_tower_data()
	}

	// ??????????????????
	p.CheckSystemMail()

	// ??????
	p.check_and_pull_chat()

	response := &msg_client_message.S2CHeartbeat{
		SysTime: int32(time.Now().Unix()),
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_HEARTBEAT), response)

	return 1
}

func C2SDataSyncHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SDataSyncRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	if req.Base {
		p.send_info()
	}
	if req.Items {
		p.send_items()
	}
	if req.Roles {
		p.send_roles()
	}
	if req.Teams {
		p.send_teams()
	}
	if req.Campaigns {
		p.send_campaigns()
	}
	if req.ActiveStage {
		p.send_active_stage_data(0)
	}
	if req.Arena {
		p.send_arena_data()
	}
	if req.Chat {
		p.pull_chat(CHAT_CHANNEL_WORLD)
		p.pull_chat(CHAT_CHANNEL_WORLD)
		p.pull_chat(CHAT_CHANNEL_WORLD)
	}
	if req.Explore {
		p.send_explore_data()
	}
	if req.Friend {
		p.send_friend_list()
	}
	if req.GoldHand {
		p.send_gold_hand()
	}
	if req.Mail {
		p.GetMailList()
	}
	if req.SevenDays {
		p.seven_days_data()
	}
	if req.Sign {
		p.get_sign_data()
	}
	if req.Talent {
		p.send_talent_list()
	}
	if req.Task {
		p.send_task(0)
	}
	if req.Tower {
		p.send_tower_data(true)
	}
	return 1
}

func C2SPlayerChangeNameHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SPlayerChangeNameRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	if len(req.GetNewName()) > int(global_config.MaxNameLen) {
		log.Error("Player[%v] change new name[%v] is too long", p.Id, req.GetNewName())
		return int32(msg_client_message.E_ERR_PLAYER_NAME_TOO_LONG)
	}
	if p.db.GetName() != "" {
		if global_config.ChgNameCost != nil && len(global_config.ChgNameCost) > 0 {
			if p.get_diamond() < global_config.ChgNameCost[0] {
				return int32(msg_client_message.E_ERR_PLAYER_DIAMOND_NOT_ENOUGH)
			}
			p.add_diamond(-global_config.ChgNameCost[0])
		}
	}
	p.db.SetName(req.GetNewName())
	p.Send(uint16(msg_client_message_id.MSGID_S2C_PLAYER_CHANGE_NAME_RESPONSE), &msg_client_message.S2CPlayerChangeNameResponse{
		NewName: req.GetNewName(),
	})

	share_data.SaveUidPlayerInfo(hall_server.redis_conn, p.UniqueId, &msg_client_message.AccountPlayerInfo{
		ServerId:    config.ServerId,
		PlayerName:  req.GetNewName(),
		PlayerLevel: p.db.Info.GetLvl(),
		PlayerHead:  p.db.Info.GetHead(),
	})
	return 1
}

func C2SPlayerChangeHeadHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SPlayerChangeHeadRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.change_head(req.GetNewHead())
}

func C2SRedPointStatesHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRedPointStatesRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.send_red_point_states(req.GetModules())
}

func (p *Player) send_account_player_list() int32 {
	share_data.LoadUidPlayerList(hall_server.redis_conn, p.UniqueId)
	if share_data.GetUidPlayer(p.UniqueId, config.ServerId) == nil {
		share_data.SaveUidPlayerInfo(hall_server.redis_conn, p.UniqueId, &msg_client_message.AccountPlayerInfo{
			ServerId:    config.ServerId,
			PlayerName:  p.db.GetName(),
			PlayerLevel: p.db.Info.GetLvl(),
			PlayerHead:  p.db.Info.GetHead(),
		})
	}
	response := &msg_client_message.S2CAccountPlayerListResponse{
		InfoList: share_data.GetUidPlayerList(p.UniqueId),
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ACCOUNT_PLAYER_LIST_RESPONSE), response)
	log.Debug("Account[%v] player list %v", p.Account, response)
	return 1
}

func C2SAccountPlayerListHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SAccountPlayerListRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.send_account_player_list()
}

/*
func (this *Player) send_guide_data() int32 {
	response := &msg_client_message.S2CGuideDataResponse{
		Data: this.db.GuideData.GetData(),
	}
	this.Send(uint16(msg_client_message_id.MSGID_S2C_GUIDE_DATA_RESPONSE), response)
	return 1
}
*/

func C2SGuideDataSaveHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGuideDataSaveRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	p.db.GuideData.SetData(req.GetData())
	response := &msg_client_message.S2CGuideDataSaveResponse{
		Data: req.GetData(),
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_GUIDE_DATA_SAVE_RESPONSE), response)
	log.Debug("Player[%v] guide save %v", p.Id, req.GetData())
	return 1
}

func (p *Player) reconnect() int32 {
	uid := p.db.GetUniqueId()
	row := dbc.BanPlayers.GetRow(uid)
	if row != nil && row.GetStartTime() > 0 {
		log.Error("Player unique id %v be banned", uid)
		return int32(msg_client_message.E_ERR_ACCOUNT_BE_BANNED)
	}

	new_token := share_data.GenerateAccessToken(uid)
	login_token_mgr.SetToken(uid, new_token, p.Id)
	conn_timer_wheel.Remove(p.Id)
	atomic.StoreInt32(&p.is_login, 1)

	response := &msg_client_message.S2CReconnectResponse{
		NewToken: new_token,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_RECONNECT_RESPONSE), response)

	p.send_items()

	log.Trace("Player[%v] reconnected, new token %v", p.Id, new_token)
	return 1
}

func C2SReconnectHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SReconnectRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}

	return p.reconnect()
}
