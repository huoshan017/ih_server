package main

import (
	"errors"
	"fmt"
	"ih_server/libs/log"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_server_message "ih_server/proto/gen_go/server_message"
	"ih_server/src/rpc_proto"
	"sync/atomic"
	"time"
)

// GM调用
type G2H_Proc struct {
}

func (proc *G2H_Proc) Test(args *rpc_proto.GmTestCmd, result *rpc_proto.GmCommonResponse) error {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	result.Res = 1

	log.Trace("@@@ G2H_Proc::Test %v", args)
	return nil
}

func (proc *G2H_Proc) Anouncement(args *rpc_proto.GmAnouncementCmd, result *rpc_proto.GmCommonResponse) error {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	if !system_chat_mgr.push_chat_msg(args.Content, args.RemainSeconds, 0, 0, "", 0) {
		err_str := fmt.Sprintf("@@@ G2H_Proc::Anouncement %v failed", args)
		return errors.New(err_str)
	}

	result.Res = 1

	log.Trace("@@@ G2H_Proc::Anouncement %v", args)
	return nil
}

func (proc *G2H_Proc) SysMail(args *rpc_proto.GmSendSysMailCmd, result *rpc_proto.GmCommonResponse) error {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	// 群发邮件
	if args.PlayerId <= 0 {
		row := dbc.SysMails.AddRow()
		if row == nil {
			log.Error("@@@ G2H_Proc::SysMail add new db row failed")
			result.Res = -1
		}
		result.Res = mail_has_subtype(args.MailTableID)
		if result.Res > 0 {
			row.SetTableId(args.MailTableID)
			row.AttachedItems.SetItemList(args.AttachItems)
			row.SetSendTime(int32(time.Now().Unix()))
			dbc.SysMailCommon.GetRow().SetCurrMailId(row.GetId())
		}
	} else {
		result.Res = RealSendMail(nil, args.PlayerId, MAIL_TYPE_SYSTEM, args.MailTableID, "", "", args.AttachItems, 0)
	}

	log.Trace("@@@ G2H_Proc::SysMail %v", args)
	return nil
}

func (proc *G2H_Proc) PlayerInfo(args *rpc_proto.GmPlayerInfoCmd, result *rpc_proto.GmPlayerInfoResponse) error {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	p := player_mgr.GetPlayerById(args.Id)
	if p == nil {
		result.Id = int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
		return nil
	}

	result.Id = args.Id
	result.Account = p.db.GetAccount()
	result.UniqueId = p.db.GetUniqueId()
	result.CreateTime = p.db.Info.GetCreateUnix()
	result.IsLogin = atomic.LoadInt32(&p.is_login)
	result.LastLoginTime = p.db.Info.GetLastLogin()
	result.LogoutTime = p.db.Info.GetLastLogout()
	result.Level = p.db.Info.GetLvl()
	result.VipLevel = p.db.Info.GetVipLvl()
	result.Gold = p.db.Info.GetGold()
	result.Diamond = p.db.Info.GetDiamond()
	result.GuildId = p.db.Guild.GetId()
	if result.GuildId > 0 {
		guild := guild_manager.GetGuild(result.GuildId)
		result.GuildName = guild.GetName()
		result.GuildLevel = guild.GetLevel()
	}
	result.UnlockCampaignId = p.db.CampaignCommon.GetCurrentCampaignId()
	result.HungupCampaignId = p.db.CampaignCommon.GetHangupCampaignId()
	result.ArenaScore = p.db.Arena.GetScore()
	talents := p.db.Talents.GetAllIndex()
	if talents != nil {
		for i := 0; i < len(talents); i++ {
			lvl, _ := p.db.Talents.GetLevel(talents[i])
			result.TalentList = append(result.TalentList, []int32{talents[i], lvl}...)
		}
	}
	result.TowerId = p.db.TowerCommon.GetCurrId()
	result.SignIn = p.db.Sign.GetSignedIndex()
	items := p.db.Items.GetAllIndex()
	var item_list []int32
	if items != nil {
		for i := 0; i < len(items); i++ {
			c, o := p.db.Items.GetCount(items[i])
			if !o {
				continue
			}
			item_list = append(item_list, []int32{items[i], c}...)
		}
	}
	result.Items = item_list
	var role_list []int32
	roles := p.db.Roles.GetAllIndex()
	if roles != nil {
		for i := 0; i < len(roles); i++ {
			role_cid, o := p.db.Roles.GetTableId(roles[i])
			if !o {
				continue
			}
			role_rank, _ := p.db.Roles.GetRank(roles[i])
			role_level, _ := p.db.Roles.GetLevel(roles[i])
			role_list = append(role_list, []int32{roles[i], role_cid, role_rank, role_level}...)
		}
	}
	result.Roles = role_list
	log.Trace("@@@ G2H_Proc::PlayerInfo %v %v", args, result)

	return nil
}

func (proc *G2H_Proc) OnlinePlayerNum(args *rpc_proto.GmOnlinePlayerNumCmd, result *rpc_proto.GmOnlinePlayerNumResponse) error {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	result.PlayerNum = []int32{conn_timer_wheel.GetCurrPlayerNum(), player_mgr.GetPlayersNum(), guild_manager.get_guild_num()}

	log.Trace("@@@ G2H_Proc::OnlinePlayerNum")

	return nil
}

func (proc *G2H_Proc) MonthCardSend(args *rpc_proto.GmMonthCardSendCmd, result *rpc_proto.GmCommonResponse) error {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	cards := pay_table_mgr.GetMonthCards()
	if len(cards) == 0 {
		result.Res = -1
		return nil
	}

	var found bool
	for i := 0; i < len(cards); i++ {
		if cards[i].BundleId == args.BundleId {
			found = true
			break
		}
	}

	if !found {
		log.Error("@@@ Not found month card with bundle id %v", args.BundleId)
		result.Res = -1
		return nil
	}

	p := player_mgr.GetPlayerById(args.PlayerId)
	if p == nil {
		log.Error("@@@ Month card send cant found player %v", args.PlayerId)
		result.Res = int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
		return nil
	}

	res, _ := p._charge_with_bundle_id(0, args.BundleId, nil, nil, -1)
	if res < 0 {
		log.Error("@@@ Month card send with error %v", res)
		result.Res = res
		return nil
	}

	log.Trace("@@@ G2H_Proc::MonthCardSend %v", args)

	return nil
}

func (proc *G2H_Proc) GetPlayerUniqueId(args *rpc_proto.GmGetPlayerUniqueIdCmd, result *rpc_proto.GmGetPlayerUniqueIdResponse) error {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	if args.PlayerId > 0 {
		p := player_mgr.GetPlayerById(args.PlayerId)
		if p == nil {
			result.PlayerUniqueId = "Cant found player"
			log.Error("@@@ Get player %v cant found", args.PlayerId)
			return nil
		}

		result.PlayerUniqueId = p.db.GetUniqueId()
	}

	log.Trace("@@@ G2H_Proc::GetPlayerUniqueId %v", args)

	return nil
}

func (proc *G2H_Proc) BanPlayer(args *rpc_proto.GmBanPlayerByUniqueIdCmd, result *rpc_proto.GmCommonResponse) error {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	p := player_mgr.GetPlayerByUid(args.PlayerUniqueId)
	if p == nil {
		result.Res = int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
		log.Error("@@@ Player cant get by unique id %v", args.PlayerUniqueId)
		return nil
	}

	p.OnLogout(true)

	row := dbc.BanPlayers.GetRow(args.PlayerUniqueId)
	if args.BanOrFree > 0 {
		now_time := time.Now()
		if row == nil {
			row = dbc.BanPlayers.AddRow(args.PlayerUniqueId)
			row.SetAccount(p.db.GetAccount())
			row.SetPlayerId(p.db.GetPlayerId())
		}
		row.SetStartTime(int32(now_time.Unix()))
		row.SetStartTimeStr(now_time.Format("2006-01-02 15:04:05"))
	} else {
		if row != nil {
			row.SetStartTime(0)
			row.SetStartTimeStr("")
		}
	}

	if args.PlayerId == p.db.GetPlayerId() {
		login_conn_mgr.Send(uint16(msg_server_message.MSGID_H2L_ACCOUNT_BAN), &msg_server_message.H2LAccountBan{
			UniqueId:  args.PlayerUniqueId,
			BanOrFree: args.BanOrFree,
			Account:   p.db.GetAccount(),
			PlayerId:  p.db.GetPlayerId(),
		})
	}

	log.Trace("@@@ G2H_Proc::BanPlayer %v", args)

	return nil
}

func (proc *G2H_Proc) GuildInfo(arg *rpc_proto.GmGuildInfoCmd, result *rpc_proto.GmGuildInfoResponse) error {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()
	guild := guild_manager.GetGuild(arg.GuildId)
	if guild == nil {
		result.Info.Id = int32(msg_client_message.E_ERR_PLAYER_GUILD_DATA_NOT_FOUND)
		log.Error("@@@ Guild cant get by id %v", arg.GuildId)
		return nil
	}

	result.Info.Id = guild.GetId()
	result.Info.Name = guild.GetName()
	result.Info.Level = guild.GetLevel()
	result.Info.Logo = guild.GetLogo()
	result.Info.PresidentId = guild.GetPresident()
	president := player_mgr.GetPlayerById(result.Info.PresidentId)
	if president != nil {
		result.Info.PresidentName = president.db.GetName()
		result.Info.PresidentLevel = president.db.GetLevel()
	}
	result.Info.CurrMemNum = guild.Members.NumAll()
	result.Info.MaxMemNum = _guild_member_num_limit(guild)
	result.Info.CreateTime = guild.GetCreateTime()
	result.Info.Creater = guild.GetCreater()

	// 成员
	ids := guild.Members.GetAllIndex()
	for _, id := range ids {
		mem := player_mgr.GetPlayerById(id)
		if mem == nil {
			continue
		}
		result.Info.MemList = append(result.Info.MemList, &rpc_proto.GmGuildMemberInfo{
			PlayerId:          id,
			PlayerName:        mem.db.GetName(),
			PlayerLevel:       mem.db.GetLevel(),
			Position:          mem.db.Guild.GetPosition(),
			JoinTime:          mem.db.Guild.GetJoinTime(),
			QuitTime:          mem.db.Guild.GetQuitTime(),
			SignTime:          mem.db.Guild.GetSignTime(),
			DonateNum:         mem.db.Guild.GetDonateNum(),
			LastAskDonateTime: mem.db.Guild.GetLastAskDonateTime(),
			LastDonateTime:    mem.db.Guild.GetLastDonateTime(),
		})
	}

	log.Trace("@@@ G2H_Proc::GuildInfo %v", arg)

	return nil
}

func (proc *G2H_Proc) GuildList(arg *rpc_proto.GmGuildListCmd, result *rpc_proto.GmGuildListResponse) error {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()
	return nil
}
