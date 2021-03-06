package main

import (
	"ih_server/libs/log"
	"ih_server/libs/utils"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	"math/rand"
	"strconv"
	"time"

	"github.com/golang/protobuf/proto"
)

func set_level_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}
	var level int
	var err error
	level, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	p.db.Info.SetLvl(int32(level))
	p.db.SetLevel(int32(level))
	p.b_base_prop_chg = true
	p.send_info()
	return 1
}

func test_lua_cmd(p *Player, args []string) int32 {
	/*L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()
	for _, pair := range []struct {
		n string
		f lua.LGFunction
	}{
		{lua.LoadLibName, lua.OpenPackage}, // Must be first
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
	} {
		if err := L.CallByParam(lua.P{
			Fn:      L.NewFunction(pair.f),
			NRet:    0,
			Protect: true,
		}, lua.LString(pair.n)); err != nil {
			panic(err)
		}
	}
	if err := L.DoFile("main.lua"); err != nil {
		panic(err)
	}*/
	return 1
}

func rand_role_cmd(p *Player, _ []string) int32 {
	role_id := p.rand_role()
	if role_id <= 0 {
		log.Warn("Cant rand role")
	} else {
		log.Debug("Rand role: %v", role_id)
	}
	return 1
}

func new_role_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var table_id, num, rank, level int
	var err error
	table_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换角色配置ID[%v]错误[%v]", args[0], err.Error())
		return -1
	}

	if len(args) > 1 {
		level, err = strconv.Atoi(args[1])
		if err != nil {
			return -1
		}
		if level < 1 {
			return -1
		}
	} else {
		level = 1
	}

	if len(levelup_table_mgr.Array) < level {
		log.Error("level[%v] greater max", level)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_LEVEL_IS_MAX)
	}

	if len(args) > 2 {
		rank, err = strconv.Atoi(args[2])
		if err != nil {
			return -1
		}
		if rank < 1 || rank > len(rankup_table_mgr.Array) {
			return -1
		}
	} else {
		rank = 1
	}

	if len(args) > 3 {
		num, err = strconv.Atoi(args[3])
		if err != nil {
			log.Error("转换角色数量[%v]错误[%v]", args[3], err.Error())
			return -1
		}
	}

	if num == 0 {
		num = 1
	}

	for i := 0; i < num; i++ {
		p.new_role(int32(table_id), int32(rank), int32(level))
	}
	return 1
}

func all_roles_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var num, level, rank int
	var err error
	num, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	if len(args) > 1 {
		level, err = strconv.Atoi(args[1])
		if err != nil {
			return -1
		}
	} else {
		level = 1
	}

	if len(args) > 2 {
		rank, err = strconv.Atoi(args[1])
		if err != nil {
			return -1
		}
	} else {
		rank = 1
	}

	var curr_base_id int32
	for _, c := range card_table_mgr.Array {
		if c.Id != curr_base_id {
			for i := 0; i < num; i++ {
				p.new_role(c.Id, int32(rank), int32(level))
			}
			curr_base_id = c.Id
		}
	}
	return 1
}

func list_role_cmd(p *Player, args []string) int32 {
	var camp, typ, star int
	var err error
	if len(args) > 0 {
		camp, err = strconv.Atoi(args[0])
		if err != nil {
			log.Error("转换阵营[%v]错误[%v]", args[0], err.Error())
			return -1
		}
		if len(args) > 1 {
			typ, err = strconv.Atoi(args[1])
			if err != nil {
				log.Error("转换卡牌类型[%v]错误[%v]", args[1], err.Error())
				return -1
			}
			if len(args) > 2 {
				star, err = strconv.Atoi(args[2])
				if err != nil {
					log.Error("转换卡牌星级[%v]错误[%v]", args[2], err.Error())
					return -1
				}
			}
		}
	}
	all := p.db.Roles.GetAllIndex()
	if all != nil {
		for i := 0; i < len(all); i++ {
			table_id, o := p.db.Roles.GetTableId(all[i])
			if !o {
				continue
			}

			level, _ := p.db.Roles.GetLevel(all[i])
			rank, _ := p.db.Roles.GetRank(all[i])

			card := card_table_mgr.GetRankCard(table_id, rank)
			if card == nil {
				continue
			}

			if camp > 0 && card.Camp != int32(camp) {
				continue
			}
			if typ > 0 && card.Type != int32(typ) {
				continue
			}
			if star > 0 && card.Rarity != int32(star) {
				continue
			}

			equips, _ := p.db.Roles.GetEquip(all[i])
			log.Debug("role_id:%v, table_id:%v, level:%v, rank:%v, camp:%v, type:%v, star:%v, equips:%v", all[i], table_id, level, rank, card.Camp, card.Type, card.Rarity, equips)
		}
	}
	return 1
}

func set_attack_team_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var role_id int
	var team []int32
	for i := 0; i < len(args); i++ {
		role_id, err = strconv.Atoi(args[i])
		if err != nil {
			log.Error("转换角色ID[%v]错误[%v]", role_id, err.Error())
			return -1
		}
		team = append(team, int32(role_id))
	}

	if p.SetTeam(BATTLE_TEAM_ATTACK, team, 0) < 0 {
		log.Error("设置玩家[%v]攻击阵容失败", p.Id)
		return -1
	}

	return 1
}

func set_defense_team_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var role_id int
	var team []int32
	for i := 0; i < len(args); i++ {
		role_id, err = strconv.Atoi(args[i])
		if err != nil {
			log.Error("转换角色ID[%v]错误[%v]", role_id, err.Error())
			return -1
		}
		team = append(team, int32(role_id))
	}

	if p.SetTeam(BATTLE_TEAM_DEFENSE, team, 0) < 0 {
		log.Error("设置玩家[%v]防守阵容失败", p.Id)
		return -1
	}

	return 1
}

func list_teams_cmd(p *Player, _ []string) int32 {
	for bt := int32(1); bt < BATTLE_TEAM_MAX; bt++ {
		if bt == BATTLE_TEAM_DEFENSE {
			log.Debug("defense team: %v, artifact: %v", p.db.BattleTeam.GetDefenseMembers(), p.db.BattleTeam.GetDefenseArtifactId())
		} else if bt == BATTLE_TEAM_CAMPAIN {
			log.Debug("campaign team: %v, artifact: %v", p.db.BattleTeam.GetCampaignMembers(), p.db.BattleTeam.GetCampaignArtifactId())
		} else {
			if p.tmp_teams != nil {
				tmp_team := p.tmp_teams[bt]
				if tmp_team != nil {
					log.Debug("team %v: %v, artifact %v", bt, tmp_team.members, tmp_team.artifact.Id)
				}
			}
		}
	}
	return 1
}

func pvp_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var player_id int
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换玩家ID[%v]失败[%v]", args[0], err.Error())
		return -1
	}

	p.Fight2Player(8, int32(player_id))

	log.Debug("玩家[%v]pvp玩家[%v]", p.Id, player_id)
	return 1
}

func fight_stage_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var stage_id, stage_type int
	stage_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换关卡[%v]失败[%v]", args[0], err.Error())
		return -1
	}

	stage := stage_table_mgr.Get(int32(stage_id))
	if stage == nil {
		log.Error("关卡[%v]不存在", stage_id)
		return -1
	}

	if len(args) > 1 {
		stage_type, err = strconv.Atoi(args[1])
		if err != nil {
			log.Error("转换关卡类型[%v]失败[%v]", args[1], err.Error())
			return -1
		}
	} else {
		stage_type = 1
	}

	err_code, is_win, my_team, target_team, my_artifact_id, target_artifact_id, enter_reports, rounds, has_next_wave := p.FightInStage(int32(stage_type), stage, nil, nil)
	if err_code < 0 {
		log.Error("Player[%v] fight stage %v, team is empty", p.Id, stage_id)
		return err_code
	}

	response := &msg_client_message.S2CBattleResultResponse{}
	response.IsWin = is_win
	response.MyTeam = my_team
	response.TargetTeam = target_team
	response.EnterReports = enter_reports
	response.Rounds = rounds
	response.HasNextWave = has_next_wave
	response.MyArtifactId = my_artifact_id
	response.TargetArtifactId = target_artifact_id
	p.Send(uint16(msg_client_message_id.MSGID_S2C_BATTLE_RESULT_RESPONSE), response)
	log.Debug("玩家[%v]挑战了关卡[%v]", p.Id, stage_id)
	return 1
}

func fight_campaign_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var campaign_id int
	campaign_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换关卡ID[%v]失败[%v]", args[0], err.Error())
		return -1
	}

	res := p.FightInCampaign(int32(campaign_id))
	if res < 0 {
		log.Error("玩家[%v]挑战战役关卡[%v]失败[%v]", p.Id, campaign_id, res)
	} else {
		log.Debug("玩家[%v]挑战了战役关卡[%v]", p.Id, campaign_id)
	}
	return res
}

func start_hangup_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var campaign_id int
	campaign_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换战役ID[%v]失败[%v]", args[0], err.Error())
		return -1
	}

	res := p.set_hangup_campaign_id(int32(campaign_id))
	if res < 0 {
		return res
	}

	log.Debug("玩家[%v]设置了挂机战役关卡[%v]", p.Id, campaign_id)
	return 1
}

func hangup_income_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var income_type int
	income_type, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换收益类型[%v]失败[%v]", args[0], err.Error())
		return -1
	}

	p.campaign_hangup_income_get(int32(income_type), false)

	log.Debug("玩家[%v]获取了类型[%v]挂机收益", p.Id, income_type)

	return 1
}

func campaign_data_cmd(p *Player, _ []string) int32 {
	p.send_campaigns()
	return 1
}

func leave_game_cmd(p *Player, _ []string) int32 {
	p.OnLogout(true)
	return 1
}

func add_item_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var item_id, item_num int
	item_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换物品ID[%v]失败[%v]", args[0], err.Error())
		return -1
	}
	item_num, err = strconv.Atoi(args[1])
	if err != nil {
		log.Error("转换物品数量[%v]失败[%v]", args[1], err.Error())
		return -1
	}

	if !p.add_resource(int32(item_id), int32(item_num)) {
		return -1
	}

	log.Debug("玩家[%v]增加了资源[%v,%v]", p.Id, item_id, item_num)
	return 1
}

func all_items_cmd(p *Player, _ []string) int32 {
	a := item_table_mgr.Array
	for _, item := range a {
		p.add_resource(item.Id, 10000)
	}
	return 1
}

func clear_items_cmd(p *Player, _ []string) int32 {
	p.db.Items.Clear()
	p.send_items()
	return 1
}

func role_levelup_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var role_id, up_num int
	role_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换角色ID[%v]失败[%v]", args[0], err.Error())
		return -1
	}
	if len(args) > 1 {
		up_num, err = strconv.Atoi(args[1])
		if err != nil {
			log.Error("转换升级次数[%v]失败[%v]", args[1], err.Error())
			return -1
		}
	}

	res := p.levelup_role(int32(role_id), int32(up_num))
	if res > 0 {
		log.Debug("玩家[%v]升级了角色[%v]等级[%v]", p.Id, role_id, res)
	}

	return res
}

func role_rankup_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var role_id int
	role_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换角色ID[%v]失败[%v]", args[0], err.Error())
		return -1
	}

	res := p.rankup_role(int32(role_id))
	if res > 0 {
		log.Debug("玩家[%v]升级了角色[%v]品阶[%v]", p.Id, role_id, res)
	}

	return res
}

func role_decompose_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var role_id int
	var role_ids []int32
	for i := 0; i < len(args); i++ {
		role_id, err = strconv.Atoi(args[i])
		if err != nil {
			log.Error("转换角色ID[%v]失败[%v]", args[i], err.Error())
			return -1
		}
		role_ids = append(role_ids, int32(role_id))
	}

	return p.decompose_role(role_ids)
}

func item_fusion_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var piece_id, fusion_num int
	piece_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换碎片ID[%v]失败[%v]", args[0], err.Error())
		return -1
	}
	fusion_num, err = strconv.Atoi(args[1])
	if err != nil {
		log.Error("转换合成次数[%v]失败[%v]", args[1], err.Error())
		return -1
	}

	return p.fusion_item(int32(piece_id), int32(fusion_num))
}

func item_sell_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var item_id, item_num int
	item_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换物品ID[%v]失败[%v]", args[0], err.Error())
		return -1
	}
	item_num, err = strconv.Atoi(args[1])
	if err != nil {
		log.Error("转换物品数量[%v]失败[%v]", args[1], err.Error())
		return -1
	}

	return p.sell_item(int32(item_id), int32(item_num), true)
}

func fusion_role_cmd(p *Player, args []string) int32 {
	if len(args) < 3 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var err error
	var fusion_id, main_card_id int
	fusion_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("转换合成角色ID[%v]失败[%v]", args[0], err.Error())
		return -1
	}
	main_card_id, err = strconv.Atoi(args[1])
	if err != nil {
		log.Error("转换主卡ID[%v]失败[%v]", args[1], err.Error())
		return -1
	}

	var cost1_ids, cost2_ids, cost3_ids []int32
	cost1_ids = parse_xml_str_arr2(args[2], "|")
	if len(cost1_ids) == 0 {
		log.Error("消耗角色1系列转换错误")
		return -1
	}
	if len(args) > 3 {
		cost2_ids = parse_xml_str_arr2(args[3], "|")
		if len(cost2_ids) == 0 {
			log.Error("消耗角色2系列转换错误")
			return -1
		}
		if len(args) > 4 {
			cost3_ids = parse_xml_str_arr2(args[4], "|")
			if len(cost3_ids) == 0 {
				log.Error("消耗角色3系列转换错误")
				return -1
			}
		}
	}

	return p.fusion_role(int32(fusion_id), int32(main_card_id), [][]int32{cost1_ids, cost2_ids, cost3_ids})
}

func send_mail_cmd(p *Player, args []string) int32 {
	if len(args) < 4 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var receiver_id, mail_type int
	//var title, content string
	var err error
	receiver_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("接收者ID[%v]转换失败[%v]", receiver_id, err.Error())
		return -1
	}
	mail_type, err = strconv.Atoi(args[1])
	if err != nil {
		log.Error("邮件类型[%v]转换失败[%v]", mail_type, err.Error())
		return -1
	}

	var attach_item_id int
	if len(args) > 4 {
		attach_item_id, err = strconv.Atoi(args[4])
		if err != nil {
			log.Error("邮件附件[%v]转换失败[%v]", attach_item_id, err.Error())
			return -1
		}
	}

	var items []*msg_client_message.ItemInfo
	if attach_item_id > 0 {
		item := &msg_client_message.ItemInfo{
			Id:    int32(attach_item_id),
			Value: 1,
		}
		items = []*msg_client_message.ItemInfo{item}
	}
	return SendMail(p, int32(receiver_id), int32(mail_type), 0, args[2], args[3], items, 0)
}

func mail_list_cmd(p *Player, args []string) int32 {
	return p.GetMailList()
}

func mail_detail_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	mail_ids := parse_xml_str_arr2(args[0], "|")
	return p.GetMailDetail(mail_ids)
}

func mail_items_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var mail_ids []int32
	for i := 0; i < len(args); i++ {
		var mail_id int
		var err error
		mail_id, err = strconv.Atoi(args[0])
		if err != nil {
			log.Error("邮件ID[%v]转换失败[%v]", args[0], err.Error())
			return -1
		}
		mail_ids = append(mail_ids, int32(mail_id))
	}

	return p.GetMailAttachedItems(mail_ids)
}

func delete_mail_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	mail_ids := parse_xml_str_arr2(args[0], "|")
	return p.DeleteMails(mail_ids)
}

func talent_data_cmd(p *Player, args []string) int32 {
	return p.send_talent_list()
}

func up_talent_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var talent_id int
	var err error
	talent_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("天赋ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.up_talent(int32(talent_id))
}

func talent_reset_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var tag int
	var err error
	tag, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.talent_reset(int32(tag))
}

func tower_data_cmd(p *Player, args []string) int32 {
	return p.send_tower_data(true)
}

func fight_tower_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var tower_id int
	var err error
	tower_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("爬塔ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.fight_tower(int32(tower_id))
}

func get_tower_key_cmd(p *Player, _ []string) int32 {
	tower_key_max := global_config.TowerKeyMax
	p.db.TowerCommon.SetKeys(tower_key_max)
	return p.send_tower_data(false)
}

func tower_records_info_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var tower_id int
	var err error
	tower_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("爬塔ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.get_tower_records_info(int32(tower_id))
}

func tower_record_data_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var tower_fight_id int
	var err error
	tower_fight_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("爬塔战斗ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.get_tower_record_data(int32(tower_fight_id))
}

func tower_ranklist_cmd(_ *Player, _ []string) int32 {
	rank_list := get_tower_rank_list()
	log.Debug("TowerRankList: %v", rank_list)
	return 1
}

func set_tower_id_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var tower_id int
	var err error
	tower_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("爬塔ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	p.db.TowerCommon.SetCurrId(int32(tower_id))
	return p.send_tower_data(true)
}

func battle_recordlist_cmd(p *Player, args []string) int32 {
	return p.GetBattleRecordList()
}

func battle_record_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var record_id int
	var err error
	record_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("战斗录像ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.GetBattleRecord(int32(record_id))
}

func test_stw_cmd(_ *Player, _ []string) int32 {
	stw := utils.NewSimpleTimeWheel()
	stw.Run()
	return 1
}

func item_upgrade_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var item_id, item_num int
	var err error
	item_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("物品ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	if len(args) > 1 {
		item_num, err = strconv.Atoi(args[1])
		if err != nil {
			log.Error("物品数量[%v]转换失败[%v]", args[1], err.Error())
			return -1
		}
	}

	return p.item_upgrade(0, int32(item_id), int32(item_num), 0)
}

func role_item_upgrade_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var role_id, item_id, item_num, upgrade_type int
	var err error
	role_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("角色ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}
	item_id, err = strconv.Atoi(args[1])
	if err != nil {
		log.Error("物品ID[%v]转换失败[%v]", args[1], err.Error())
		return -1
	}
	if len(args) > 2 {
		item_num, err = strconv.Atoi(args[1])
		if err != nil {
			log.Error("物品数量[%v]转换失败[%v]", args[1], err.Error())
			return -1
		}
	}
	if len(args) > 3 {
		upgrade_type, err = strconv.Atoi(args[2])
		if err != nil {
			log.Error("升级类型[%v]转换失败[%v]", args[2], err.Error())
			return -1
		}
	}

	return p.item_upgrade(int32(role_id), int32(item_id), int32(item_num), int32(upgrade_type))
}

func equip_item_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var role_id, equip_id int
	var err error
	role_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("角色ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}
	equip_id, err = strconv.Atoi(args[1])
	if err != nil {
		log.Error("物品ID[%v]转换失败[%v]", args[1], err.Error())
		return -1
	}
	return p.equip(int32(role_id), int32(equip_id))
}

func unequip_item_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var role_id, equip_type int
	var err error
	role_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("角色ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}
	equip_type, err = strconv.Atoi(args[1])
	if err != nil {
		log.Error("装备类型[%v]转换失败[%v]", args[1], err.Error())
		return -1
	}
	return p.equip(int32(role_id), int32(equip_type))
}

func list_items_cmd(p *Player, _ []string) int32 {
	items := p.db.Items.GetAllIndex()
	if items == nil {
		return 0
	}

	log.Debug("Player[%v] Items:", p.Id)
	for _, item := range items {
		c, _ := p.db.Items.GetCount(item)
		log.Debug("    item: %v,  num: %v", item, c)
	}
	return 1
}

func get_role_attrs_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var role_id int
	var err error
	role_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("角色ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.send_role_attrs(int32(role_id))
}

func onekey_equip_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var role_id int
	var err error
	role_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("角色id[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.role_one_key_equip(int32(role_id), nil)
}

func onekey_unequip_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var role_id int
	var err error
	role_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("角色id[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.role_one_key_unequip(int32(role_id))
}

func left_slot_open_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var role_id int
	var err error
	role_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("角色id[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.role_open_left_slot(int32(role_id))
}

func role_equips_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var role_id int
	var err error
	role_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("角色id[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	equips, o := p.db.Roles.GetEquip(int32(role_id))
	if !o {
		log.Error("玩家[%v]没有角色[%v]", p.Id, role_id)
		return -1
	}

	log.Debug("玩家[%v]角色[%v]已装备物品[%v]", p.Id, role_id, equips)

	return 1
}

func draw_card_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var draw_type int
	var err error
	draw_type, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("抽卡类型[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.draw_card(int32(draw_type))
}

func draw_data_cmd(p *Player, args []string) int32 {
	return p.send_draw_data()
}

func shop_data_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var shop_id int
	var err error
	shop_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("商店ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.send_shop(int32(shop_id))
}

func buy_item_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var shop_id, item_id, item_num int
	var err error
	shop_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("商店[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}
	item_id, err = strconv.Atoi(args[1])
	if err != nil {
		log.Error("商品[%v]转换失败[%v]", args[1], err.Error())
		return -1
	}

	if len(args) > 2 {
		item_num, err = strconv.Atoi(args[2])
		if err != nil {
			log.Error("商品数量[%v]转换失败[%v]", args[2], err.Error())
			return -1
		}
	}

	return p.shop_buy_item(int32(shop_id), int32(item_id), int32(item_num))
}

func shop_refresh_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var shop_id int
	var err error
	shop_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("商店[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.shop_refresh(int32(shop_id))
}

func arena_ranklist_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var start, num int
	var err error
	start, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("开始排名[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}
	num, err = strconv.Atoi(args[1])
	if err != nil {
		log.Error("排名数[%v]转换失败[%v]", args[1], err.Error())
		return -1
	}

	p.OutputArenaRankItems(int32(start), int32(num))

	return 1
}

func arena_data_cmd(p *Player, args []string) int32 {
	return p.send_arena_data()
}

func arena_player_team_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("玩家ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.arena_player_defense_team(int32(player_id))
}

func arena_match_cmd(p *Player, args []string) int32 {
	return p.arena_match()
}

func arena_reset_cmd(_ *Player, _ []string) int32 {
	if arena_season_mgr.IsSeasonStart() {
		arena_season_mgr.SeasonEnd()
	}
	arena_season_mgr.Reset()
	arena_season_mgr.SeasonStart()
	return 1
}

func rank_list_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var rank_type int
	var err error
	rank_type, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("排行榜类型[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.get_rank_list_items(int32(rank_type), 1, global_config.ArenaGetTopRankNum)
}

func player_info_cmd(p *Player, _ []string) int32 {
	p.send_info()
	return 1
}

func item_onekey_upgrade_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var item_ids []int32
	var item_id int
	var err error
	item_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("物品ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}
	item_ids = append(item_ids, int32(item_id))
	if len(args) > 1 {
		for i := 1; i < len(args); i++ {
			item_id, err = strconv.Atoi(args[i])
			if err != nil {
				return -1
			}
			item_ids = append(item_ids, int32(item_id))
		}
	}

	return p.items_one_key_upgrade(item_ids)
}

func active_stage_data_cmd(p *Player, args []string) int32 {
	return p.send_active_stage_data(0)
}

func active_stage_buy_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var active_stage_type int
	var err error
	active_stage_type, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("活动副本类型[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}
	return p.active_stage_challenge_num_purchase(int32(active_stage_type), 1)
}

func fight_active_stage_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var active_stage_id int
	var err error
	active_stage_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("活动副本ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.fight_active_stage(int32(active_stage_id))
}

func friend_recommend_cmd(p *Player, _ []string) int32 {
	player_ids := friend_recommend_mgr.Random(p.Id)
	log.Debug("Recommended friend ids: %v", player_ids)
	return 1
}

func friend_data_cmd(p *Player, args []string) int32 {
	return p.friend_data(true)
}

func friend_ask_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		log.Error("玩家ID[%v]转换失败[%v]", args[0], err.Error())
		return -1
	}

	return p.friend_ask([]int32{int32(player_id)})
}

func friend_agree_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.agree_friend_ask([]int32{int32(player_id)})
}

func friend_refuse_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.refuse_friend_ask([]int32{int32(player_id)})
}

func friend_remove_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	return p.remove_friend([]int32{int32(player_id)})
}

func friend_list_cmd(p *Player, args []string) int32 {
	return p.send_friend_list()
}

func friend_ask_list_cmd(p *Player, args []string) int32 {
	return p.send_friend_ask_list()
}

func friend_give_points_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	return p.give_friends_points([]int32{int32(player_id)})
}

func friend_get_points_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}
	var player_id int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	return p.get_friend_points([]int32{int32(player_id)})
}

func friend_search_boss_cmd(p *Player, args []string) int32 {
	return p.friend_search_boss()
}

func friend_boss_list_cmd(p *Player, args []string) int32 {
	return p.get_friends_boss_list()
}

func friend_boss_attacks_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	return p.friend_boss_get_attack_list(int32(player_id))
}

func friend_fight_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var friend_id int
	var err error
	friend_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	var sweep_num int
	if len(args) > 1 {
		sweep_num, err = strconv.Atoi(args[1])
		if err != nil {
			return -1
		}
	}

	p.sweep_num = int32(sweep_num)
	p.curr_sweep = 0
	return p.friend_boss_challenge(int32(friend_id))
}

func assist_list_cmd(p *Player, args []string) int32 {
	return p.active_stage_get_friends_assist_role_list()
}

func friend_set_assist_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var role_id int
	var err error
	role_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	return p.friend_set_assist_role(int32(role_id))
}

func use_assist_cmd(p *Player, args []string) int32 {
	if len(args) < 5 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var battle_type, battle_param, friend_id, role_id, member_pos, artifact_id int
	var err error
	battle_type, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	battle_param, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}
	friend_id, err = strconv.Atoi(args[2])
	if err != nil {
		return -1
	}
	role_id, err = strconv.Atoi(args[3])
	if err != nil {
		return -1
	}
	member_pos, err = strconv.Atoi(args[4])
	if err != nil {
		return -1
	}
	if len(args) > 5 {
		artifact_id, err = strconv.Atoi(args[5])
		if err != nil {
			return -1
		}
	}
	return p.fight(nil, int32(battle_type), int32(battle_param), int32(friend_id), int32(role_id), int32(member_pos), int32(artifact_id))
}

func task_data_cmd(p *Player, args []string) int32 {
	var task_type int
	var err error
	if len(args) > 1 {
		task_type, err = strconv.Atoi(args[0])
		if err != nil {
			return -1
		}
	}
	return p.send_task(int32(task_type))
}

func task_reward_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}
	var task_id int
	var err error
	task_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	return p.task_get_reward(int32(task_id))
}

func explore_data_cmd(p *Player, args []string) int32 {
	return p.send_explore_data()
}

func explore_sel_role_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var task_id, is_story int
	var err error
	task_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	is_story, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}

	var role_id int
	var role_ids []int32
	if len(args) > 2 {
		for i := 2; i < len(args); i++ {
			role_id, err = strconv.Atoi(args[i])
			if err != nil {
				return -1
			}
			role_ids = append(role_ids, int32(role_id))
		}
	}

	story := false
	if is_story > 0 {
		story = true
	}

	if role_ids == nil {
		role_ids = p.explore_one_key_sel_role(int32(task_id), story)
	}

	return p.explore_sel_role(int32(task_id), story, role_ids)
}

func explore_start_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id, is_story int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	is_story, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}

	story := false
	if is_story > 0 {
		story = true
	}

	return p.explore_task_start([]int32{int32(id)}, story)
}

func explore_reward_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id, is_story int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	is_story, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}

	story := false
	if is_story > 0 {
		story = true
	}

	return p.explore_get_reward(int32(id), story)
}

func explore_fight_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id, is_story int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	is_story, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}

	story := false
	if is_story > 0 {
		story = true
	}

	return p.explore_fight(int32(id), story)
}

func explore_refresh_cmd(p *Player, args []string) int32 {
	return p.explore_tasks_refresh()
}

func explore_lock_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id, lock int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	lock, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}

	is_lock := false
	if lock > 0 {
		is_lock = true
	}

	return p.explore_task_lock([]int32{int32(id)}, is_lock)
}

func explore_speedup_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id, is_story int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	is_story, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}

	story := false
	if is_story > 0 {
		story = true
	}

	return p.explore_speedup([]int32{int32(id)}, story)
}

func guild_search_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	return p.guild_search(args[0])
}

func guild_recommend_cmd(p *Player, args []string) int32 {
	return p.guild_recommend()
}

func guild_data_cmd(p *Player, args []string) int32 {
	return p.send_guild_data()
}

func guild_create_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var name string
	var logo int
	var err error
	name = args[0]
	logo, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}

	return p.guild_create(name, "", int32(logo))
}

func guild_dismiss_cmd(p *Player, args []string) int32 {
	return p.guild_dismiss()
}

func guild_cancel_dismiss_cmd(p *Player, args []string) int32 {
	return p.guild_cancel_dismiss()
}

func guild_modify_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var name string
	var logo int
	var err error
	name = args[0]
	logo, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}

	return p.guild_info_modify(name, int32(logo))
}

func guild_members_cmd(p *Player, args []string) int32 {
	return p.guild_members_list()
}

func guild_ask_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var guild_id int
	var err error
	guild_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.guild_ask_join(int32(guild_id))
}

func guild_agree_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	var is_refuse int
	if len(args) > 1 {
		is_refuse, err = strconv.Atoi(args[1])
		if err != nil {
			return -1
		}
	}

	return p.guild_agree_join([]int32{int32(player_id)}, func() bool {
		return is_refuse > 0
	}())
}

func guild_ask_list_cmd(p *Player, args []string) int32 {
	return p.guild_ask_list()
}

func guild_quit_cmd(p *Player, args []string) int32 {
	return p.guild_quit()
}

func guild_logs_cmd(p *Player, args []string) int32 {
	return p.guild_logs()
}

func guild_sign_cmd(p *Player, args []string) int32 {
	return p.guild_sign_in()
}

func guild_set_officer_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id, set_type int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	set_type, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}

	return p.guild_set_officer([]int32{int32(player_id)}, int32(set_type))
}

func guild_kick_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var member_id int
	var err error
	member_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.guild_kick_member([]int32{int32(member_id)})
}

func guild_change_president_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var member_id int
	var err error
	member_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.guild_change_president(int32(member_id))
}

func guild_recruit_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	return p.guild_recruit([]byte(args[0]))
}

func guild_donate_list_cmd(p *Player, args []string) int32 {
	return p.guild_donate_list()
}

func guild_ask_donate_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var item_id int
	var err error
	item_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.guild_ask_donate(int32(item_id))
}

func guild_donate_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.guild_donate(int32(player_id))
}

func chat_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var content []byte
	var channel int
	var eval int
	var err error
	content = []byte(args[0])
	channel, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}
	if len(args) > 2 {
		eval, err = strconv.Atoi(args[2])
		if err != nil {
			return -1
		}
	}

	return p.chat(int32(channel), content, int32(eval))
}

func pull_chat_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var channel int
	var err error
	channel, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.pull_chat(int32(channel))
}

func guild_stage_data_cmd(p *Player, args []string) int32 {
	return p.send_guild_stage_data(true)
}

func guild_stage_ranklist_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var boss_id int
	var err error
	boss_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.guild_stage_rank_list(int32(boss_id))
}

func guild_stage_fight_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var boss_id int
	var err error
	boss_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.guild_stage_fight(int32(boss_id))
}

func guild_stage_reset_cmd(p *Player, args []string) int32 {
	return p.guild_stage_reset()
}

func guild_stage_respawn_cmd(p *Player, args []string) int32 {
	return p.guild_stage_player_respawn()
}

func test_short_rank_cmd(_ *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var num int
	var err error
	num, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	var rank_list utils.ShortRankList
	rank_list.Init(int32(num))

	var test_item utils.TestShortRankItem
	rand.Seed(time.Now().Unix())
	for i := 0; i < 30000; i++ {
		o, id := rand31n_from_range(1, int32(num))
		if !o {
			log.Error("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
			break
		}
		test_item.Id = id
		_, test_item.Value = rand31n_from_range(10000, 10000000)
		rank_list.Update(&test_item, false)
	}

	log.Debug("Test Short Rank Item List:")
	for r := int32(1); r <= rank_list.GetLength(); r++ {
		k, v := rank_list.GetByRank(r)
		idx := rank_list.GetIndex(r)
		log.Debug("    rank: %v,  key[%v] value[%v] index[%v]", r, k, v, idx)
	}

	for i := 0; i < num/2; i++ {
		o, id := rand31n_from_range(1, int32(num))
		if o {
			if !rank_list.Delete(id) {
				log.Warn("Test Short Rank cant delete %v", id)
			} else {
				log.Trace("Test Short Rank deleted %v", id)
			}
		}
	}

	log.Debug("After deleted Test Short Rank Item List:")
	for r := int32(1); r <= rank_list.GetLength(); r++ {
		k, v := rank_list.GetByRank(r)
		idx := rank_list.GetIndex(r)
		log.Debug("    rank: %v,  key[%v] value[%v] index[%v]", r, k, v, idx)
	}

	return 1
}

func reset_tasks_cmd(p *Player, _ []string) int32 {
	p.db.Tasks.Clear()
	p.db.Tasks.ResetDailyTask()
	p.first_gen_achieve_tasks()
	p.send_task(0)
	return 1
}

func sign_data_cmd(p *Player, args []string) int32 {
	return p.get_sign_data()
}

func sign_award_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.sign_award(int32(id))
}

func seven_days_data_cmd(p *Player, args []string) int32 {
	return p.seven_days_data()
}

func seven_days_award_cmd(p *Player, args []string) int32 {
	return p.seven_days_award()
}

func charge_data_cmd(p *Player, args []string) int32 {
	return p.charge_data()
}

func charge_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.charge(0, int32(id))
}

func charge_first_award_cmd(p *Player, args []string) int32 {
	return p.charge_first_award()
}

func get_red_states_cmd(p *Player, args []string) int32 {
	return p.send_red_point_states(nil)
}

func clear_tower_save_cmd(_ *Player, _ []string) int32 {
	for id := range dbc.TowerFightSaves.m_rows {
		dbc.TowerFightSaves.RemoveRow(id)
	}
	return 1
}

func account_players_cmd(p *Player, args []string) int32 {
	return p.send_account_player_list()
}

func reset_sign_award_cmd(p *Player, _ []string) int32 {
	p.db.Sign.SetAwardIndex(0)
	return 1
}

func accel_campaign_cmd(p *Player, args []string) int32 {
	return p.campaign_accel_get_income()
}

func reconnect_cmd(p *Player, args []string) int32 {
	return p.reconnect()
}

func role_displace_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var group_id, role_id int
	var err error
	group_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	role_id, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}

	return p.role_displace(int32(group_id), int32(role_id))
}

func role_displace_confirm_cmd(p *Player, args []string) int32 {
	return p.role_displace_confirm()
}

func activity_data_cmd(p *Player, args []string) int32 {
	return p.activity_data()
}

func test_power_cmd(_ *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var power int
	var err error
	power, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	var count int = 1
	if len(args) > 1 {
		count, err = strconv.Atoi(args[1])
		if err != nil {
			return -1
		}
	}

	type info struct {
		player_id int32
		power     int32
	}

	var info_num int32 = 10000
	rand.Seed(time.Now().Unix())
	var info_list []*info = make([]*info, info_num)
	for i := int32(0); i < info_num; i++ {
		info_list[i] = &info{}
		_, info_list[i].player_id = rand31n_from_range(1, 100000)
		_, info_list[i].power = rand31n_from_range(1, 100000)
	}

	rank_list := NewTopPowerMatchManager(&TopPowerRankItem{}, 100000)

	for _, v := range info_list {
		rank_list.Update(v.player_id, v.power)
	}
	rank_list.OutputList()

	begin := time.Now()
	var pid int32
	for i := 0; i < count; i++ {
		pid = rank_list.GetNearestRandPlayer(int32(power))
	}
	cost_ms := time.Since(begin).Nanoseconds() / 1000000

	log.Trace("@@@ get the last nearest player is %v, count %v, cost ms %v", pid, count, cost_ms)

	return 1
}

func expedition_data_cmd(p *Player, args []string) int32 {
	return p.send_expedition_data()
}

func expedition_level_data_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var level int
	var err error
	level, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	return p.get_expedition_level_data_with_level(int32(level))
}

func expedition_fight_cmd(p *Player, _ []string) int32 {
	all_roles := p.db.Roles.GetAllIndex()
	if all_roles == nil {
		return -1
	}

	var mems []int32
	for i := 0; i < len(all_roles); i++ {
		rid := all_roles[i]
		if p.db.ExpeditionRoles.HasIndex(rid) {
			hp, _ := p.db.ExpeditionRoles.GetHP(rid)
			if hp <= 0 {
				log.Debug("Player %v role %v hp is zero, cant expedition fight", p.Id, rid)
				continue
			}
			weak, _ := p.db.ExpeditionRoles.GetWeak(rid)
			if weak > 0 {
				log.Debug("Player %v role %v is weak, cant expedition fight", p.Id, rid)
				continue
			}
		}
		mems = append(mems, rid)
		if len(mems) >= 5 {
			break
		}
	}

	if len(mems) == 0 {
		log.Error("expedition team is empty!!!")
		return -1
	}

	res := p.SetTeam(BATTLE_TEAM_EXPEDITION, mems, 0)
	if res < 0 {
		return res
	}

	return p.expedition_fight()
}

func expedition_powerlist_cmd(_ *Player, _ []string) int32 {
	top_power_match_manager.OutputList()
	return 1
}

func expedition_team_set_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var mids []int32
	var id int
	var err error
	for i := 0; i < len(args); i++ {
		id, err = strconv.Atoi(args[i])
		if err != nil {
			return -1
		}
		mids = append(mids, int32(id))
	}
	return p.SetTeam(BATTLE_TEAM_EXPEDITION, mids, 0)
}

func expedition_purify_points_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var purify_points int
	var err error
	purify_points, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	p.db.ExpeditionData.SetPurifyPoints(int32(purify_points))
	p.expedition_sync_purify_points()
	return 1
}

func expeditin_match_cmd(p *Player, args []string) int32 {
	return p.MatchExpeditionPlayer()
}

func artifact_data_cmd(p *Player, args []string) int32 {
	return p.artifact_data()
}

func artifact_unlock_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.artifact_unlock(int32(id))
}

func artifact_levelup_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.artifact_levelup(int32(id))
}

func artifact_rankup_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.artifact_rankup(int32(id))
}

func artifact_reset_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.artifact_reset(int32(id))
}

func set_team_artifact_cmd(p *Player, args []string) int32 {
	if len(args) < 2 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var team_id, artifact_id int
	var err error
	team_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}
	artifact_id, err = strconv.Atoi(args[1])
	if err != nil {
		return -1
	}
	return p.SetTeamArtifact(int32(team_id), int32(artifact_id))
}

func remote_player_info_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	player_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	player := player_mgr.GetPlayerById(int32(player_id))
	if player != nil {
		player.send_info()
	} else {
		resp, err_code := remote_get_player_info(p.Id, int32(player_id))
		if err_code < 0 {
			log.Error("remote_get_player_info %v err %v", player_id, err_code)
		} else {
			if resp != nil {
				log.Trace("remote_get_player_info %v", resp)
			}
		}
	}

	return 1
}

func remote_players_info_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	var player_ids []int32
	for i := 0; i < len(args); i++ {
		player_id, err = strconv.Atoi(args[i])
		if err != nil {
			return -1
		}
		player_ids = append(player_ids, int32(player_id))
	}

	idx := SplitLocalAndRemotePlayers(player_ids)
	if int(idx) >= len(player_ids) {
		log.Error("not found remote player id from %v", player_ids)
		return -1
	}

	log.Trace("splited idx %v", idx)

	resp, err_code := remote_get_multi_player_info(p.Id, player_ids[idx+1:])
	if err_code < 0 {
		log.Error("remote get multi players %v info err %v", player_ids[idx+1:], err_code)
		return err_code
	}

	if resp == nil {
		log.Error("remote get multi players %v response is empty", player_ids[idx+1])
		return -1
	}

	log.Trace("remote multi players: ")
	for i := 0; i < len(resp.PlayerInfos); i++ {
		pinfo := resp.PlayerInfos[i]
		if pinfo == nil {
			continue
		}
		log.Trace("    player %v", pinfo)
	}

	return 1
}

func split_player_ids_cmd(_ *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var player_id int
	var err error
	var player_ids []int32
	for i := 0; i < len(args); i++ {
		player_id, err = strconv.Atoi(args[i])
		if err != nil {
			return -1
		}
		player_ids = append(player_ids, int32(player_id))
	}

	var idx int32 = SplitLocalAndRemotePlayers(player_ids)

	log.Debug("splited players: %v, split index: %v", player_ids, idx)

	return 1
}

func invite_code_generate_cmd(_ *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var id int
	var err error
	id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	code := invite_code_generator.Generate(int32(id))
	id2 := invite_code_generator.GetId(code)

	log.Debug("generate code %v by id %v", code, id)
	log.Debug("generator get id %v by code %v", id2, code)

	return 1
}

func carnival_data_cmd(p *Player, args []string) int32 {
	return p.carnival_data()
}

func carnival_task_set_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var task_id int
	var err error
	task_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.carnival_task_set(int32(task_id))
}

func carnival_item_exchange_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	var task_id int
	var err error
	task_id, err = strconv.Atoi(args[0])
	if err != nil {
		return -1
	}

	return p.carnival_item_exchange(int32(task_id))
}

func carnival_share_cmd(p *Player, args []string) int32 {
	return p.carnival_share()
}

func carnival_be_invite_cmd(p *Player, args []string) int32 {
	if len(args) < 1 {
		log.Error("参数[%v]不够", len(args))
		return -1
	}

	return p.carnival_be_invited(args[0])
}

type test_cmd_func func(*Player, []string) int32

var test_cmd2funcs = map[string]test_cmd_func{
	"set_level":                set_level_cmd,
	"test_lua":                 test_lua_cmd,
	"rand_role":                rand_role_cmd,
	"new_role":                 new_role_cmd,
	"all_roles":                all_roles_cmd,
	"list_role":                list_role_cmd,
	"set_attack_team":          set_attack_team_cmd,
	"set_defense_team":         set_defense_team_cmd,
	"list_teams":               list_teams_cmd,
	"pvp":                      pvp_cmd,
	"fight_stage":              fight_stage_cmd,
	"fight_campaign":           fight_campaign_cmd,
	"start_hangup":             start_hangup_cmd,
	"hangup_income":            hangup_income_cmd,
	"campaign_data":            campaign_data_cmd,
	"leave_game":               leave_game_cmd,
	"add_item":                 add_item_cmd,
	"all_items":                all_items_cmd,
	"clear_items":              clear_items_cmd,
	"role_levelup":             role_levelup_cmd,
	"role_rankup":              role_rankup_cmd,
	"role_decompose":           role_decompose_cmd,
	"item_fusion":              item_fusion_cmd,
	"fusion_role":              fusion_role_cmd,
	"item_sell":                item_sell_cmd,
	"send_mail":                send_mail_cmd,
	"mail_list":                mail_list_cmd,
	"mail_detail":              mail_detail_cmd,
	"mail_items":               mail_items_cmd,
	"delete_mail":              delete_mail_cmd,
	"talent_data":              talent_data_cmd,
	"talent_up":                up_talent_cmd,
	"talent_reset":             talent_reset_cmd,
	"tower_data":               tower_data_cmd,
	"get_tower_key":            get_tower_key_cmd,
	"fight_tower":              fight_tower_cmd,
	"tower_records_info":       tower_records_info_cmd,
	"tower_record_data":        tower_record_data_cmd,
	"tower_ranklist":           tower_ranklist_cmd,
	"set_tower_id":             set_tower_id_cmd,
	"battle_recordlist":        battle_recordlist_cmd,
	"battle_record":            battle_record_cmd,
	"test_stw":                 test_stw_cmd,
	"item_upgrade":             item_upgrade_cmd,
	"role_item_up":             role_item_upgrade_cmd,
	"equip_item":               equip_item_cmd,
	"unequip_item":             unequip_item_cmd,
	"list_item":                list_items_cmd,
	"role_attrs":               get_role_attrs_cmd,
	"onekey_equip":             onekey_equip_cmd,
	"onekey_unequip":           onekey_unequip_cmd,
	"left_slot_open":           left_slot_open_cmd,
	"role_equips":              role_equips_cmd,
	"draw_card":                draw_card_cmd,
	"draw_data":                draw_data_cmd,
	"shop_data":                shop_data_cmd,
	"buy_item":                 buy_item_cmd,
	"shop_refresh":             shop_refresh_cmd,
	"arena_ranklist":           arena_ranklist_cmd,
	"arena_data":               arena_data_cmd,
	"arena_player_team":        arena_player_team_cmd,
	"arena_match":              arena_match_cmd,
	"arena_reset":              arena_reset_cmd,
	"rank_list":                rank_list_cmd,
	"player_info":              player_info_cmd,
	"item_onekey_upgrade":      item_onekey_upgrade_cmd,
	"active_stage_data":        active_stage_data_cmd,
	"active_stage_buy":         active_stage_buy_cmd,
	"fight_active_stage":       fight_active_stage_cmd,
	"friend_recommend":         friend_recommend_cmd,
	"friend_data":              friend_data_cmd,
	"friend_ask":               friend_ask_cmd,
	"friend_agree":             friend_agree_cmd,
	"friend_refuse":            friend_refuse_cmd,
	"friend_remove":            friend_remove_cmd,
	"friend_list":              friend_list_cmd,
	"friend_ask_list":          friend_ask_list_cmd,
	"friend_give_points":       friend_give_points_cmd,
	"friend_get_points":        friend_get_points_cmd,
	"friend_search_boss":       friend_search_boss_cmd,
	"friend_boss_list":         friend_boss_list_cmd,
	"friend_boss_attacks":      friend_boss_attacks_cmd,
	"friend_fight":             friend_fight_cmd,
	"assist_list":              assist_list_cmd,
	"set_assist":               friend_set_assist_cmd,
	"use_assist":               use_assist_cmd,
	"task_data":                task_data_cmd,
	"task_reward":              task_reward_cmd,
	"explore_data":             explore_data_cmd,
	"explore_sel_role":         explore_sel_role_cmd,
	"explore_start":            explore_start_cmd,
	"explore_reward":           explore_reward_cmd,
	"explore_fight":            explore_fight_cmd,
	"explore_refresh":          explore_refresh_cmd,
	"explore_lock":             explore_lock_cmd,
	"explore_speedup":          explore_speedup_cmd,
	"guild_search":             guild_search_cmd,
	"guild_recommend":          guild_recommend_cmd,
	"guild_data":               guild_data_cmd,
	"guild_create":             guild_create_cmd,
	"guild_dismiss":            guild_dismiss_cmd,
	"guild_cancel_dismiss":     guild_cancel_dismiss_cmd,
	"guild_modify":             guild_modify_cmd,
	"guild_members":            guild_members_cmd,
	"guild_ask":                guild_ask_cmd,
	"guild_agree":              guild_agree_cmd,
	"guild_ask_list":           guild_ask_list_cmd,
	"guild_quit":               guild_quit_cmd,
	"guild_logs":               guild_logs_cmd,
	"guild_sign":               guild_sign_cmd,
	"guild_kick":               guild_kick_cmd,
	"guild_set_officer":        guild_set_officer_cmd,
	"guild_change_president":   guild_change_president_cmd,
	"guild_recruit":            guild_recruit_cmd,
	"guild_donate_list":        guild_donate_list_cmd,
	"guild_ask_donate":         guild_ask_donate_cmd,
	"guild_donate":             guild_donate_cmd,
	"chat":                     chat_cmd,
	"pull_chat":                pull_chat_cmd,
	"guild_stage_data":         guild_stage_data_cmd,
	"guild_stage_ranklist":     guild_stage_ranklist_cmd,
	"guild_stage_fight":        guild_stage_fight_cmd,
	"guild_stage_reset":        guild_stage_reset_cmd,
	"guild_stage_respawn":      guild_stage_respawn_cmd,
	"test_short_rank":          test_short_rank_cmd,
	"reset_tasks":              reset_tasks_cmd,
	"sign_data":                sign_data_cmd,
	"sign_award":               sign_award_cmd,
	"seven_data":               seven_days_data_cmd,
	"seven_award":              seven_days_award_cmd,
	"charge_data":              charge_data_cmd,
	"charge":                   charge_cmd,
	"charge_first_award":       charge_first_award_cmd,
	"get_red_states":           get_red_states_cmd,
	"clear_tower_save":         clear_tower_save_cmd,
	"account_players":          account_players_cmd,
	"reset_sign_award":         reset_sign_award_cmd,
	"accel_campaign":           accel_campaign_cmd,
	"reconnect":                reconnect_cmd,
	"role_displace":            role_displace_cmd,
	"role_displace_confirm":    role_displace_confirm_cmd,
	"activity_data":            activity_data_cmd,
	"test_power":               test_power_cmd,
	"expedition_data":          expedition_data_cmd,
	"expedition_level_data":    expedition_level_data_cmd,
	"expedition_fight":         expedition_fight_cmd,
	"expedition_powerlist":     expedition_powerlist_cmd,
	"expedition_team_set":      expedition_team_set_cmd,
	"expedition_purify_points": expedition_purify_points_cmd,
	"expedition_match":         expeditin_match_cmd,
	"artifact_data":            artifact_data_cmd,
	"artifact_unlock":          artifact_unlock_cmd,
	"artifact_levelup":         artifact_levelup_cmd,
	"artifact_rankup":          artifact_rankup_cmd,
	"artifact_reset":           artifact_reset_cmd,
	"set_team_artifact":        set_team_artifact_cmd,
	"remote_player_info":       remote_player_info_cmd,
	"remote_players_info":      remote_players_info_cmd,
	"split_players":            split_player_ids_cmd,
	"invite_code_generate":     invite_code_generate_cmd,
	"carnival_data":            carnival_data_cmd,
	"carnival_task_set":        carnival_task_set_cmd,
	"carnival_item_exchange":   carnival_item_exchange_cmd,
	"carnival_share":           carnival_share_cmd,
	"carnival_be_invite":       carnival_be_invite_cmd,
}

func C2STestCommandHandler(p *Player /*msg proto.Message*/, msg_data []byte) int32 {
	var req msg_client_message.C2S_TEST_COMMAND
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("client_msg_handler unmarshal sub msg failed err(%s) !", err.Error())
		return -1
	}

	cmd := req.GetCmd()
	args := req.GetArgs()
	res := int32(0)

	fun := test_cmd2funcs[cmd]
	if fun != nil {
		res = fun(p, args)
	} else {
		log.Warn("不支持的测试命令[%v]", cmd)
	}

	return res
}
