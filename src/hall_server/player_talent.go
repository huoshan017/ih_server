package main

import (
	"ih_server/libs/log"
	"ih_server/proto/gen_go/client_message"
	"ih_server/proto/gen_go/client_message_id"

	"github.com/golang/protobuf/proto"
)

func (p *Player) get_talent_list() []*msg_client_message.TalentInfo {
	all := p.db.Talents.GetAllIndex()
	if len(all) == 0 {
		return make([]*msg_client_message.TalentInfo, 0)
	}

	var talents []*msg_client_message.TalentInfo
	for i := 0; i < len(all); i++ {
		lvl, o := p.db.Talents.GetLevel(all[i])
		if !o {
			continue
		}
		talents = append(talents, &msg_client_message.TalentInfo{
			Id:    all[i],
			Level: lvl,
		})
	}
	return talents
}

func (p *Player) send_talent_list() int32 {
	talents := p.get_talent_list()
	response := &msg_client_message.S2CTalentListResponse{
		Talents: talents,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_TALENT_LIST_RESPONSE), response)

	log.Debug("Player[%v] send talent list %v", p.Id, talents)

	return 1
}

func (p *Player) up_talent(talent_id int32) int32 {
	level, _ := p.db.Talents.GetLevel(talent_id)
	talent := talent_table_mgr.GetByIdLevel(talent_id, level+1)
	if talent == nil {
		log.Error("Talent[%v,%v] data not found", talent_id, level+1)
		return int32(msg_client_message.E_ERR_PLAYER_TALENT_NOT_FOUND)
	}

	if talent.CanLearn <= 0 {
		log.Error("talent[%v] cant learn", talent_id)
		return -1
	}

	if talent.PrevSkillCond > 0 {
		prev_level, o := p.db.Talents.GetLevel(talent.PrevSkillCond)
		if !o || prev_level < talent.PreSkillLevCond {
			log.Error("Player[%v] up talent %v need prev talent[%v] level[%v]", p.Id, talent_id, talent.PrevSkillCond, talent.PreSkillLevCond)
			return int32(msg_client_message.E_ERR_PLAYER_TALENT_UP_NEED_PREV_TALENT)
		}
	}

	// check cost
	for i := 0; i < len(talent.UpgradeCost)/2; i++ {
		rid := talent.UpgradeCost[2*i]
		rct := talent.UpgradeCost[2*i+1]
		if p.get_resource(rid) < rct {
			log.Error("Player[%v] up talent[%v] not enough resource[%v]", p.Id, talent_id, rid)
			return int32(msg_client_message.E_ERR_PLAYER_TALENT_UP_NOT_ENOUGH_RESOURCE)
		}
	}

	// cost resource
	for i := 0; i < len(talent.UpgradeCost)/2; i++ {
		rid := talent.UpgradeCost[2*i]
		rct := talent.UpgradeCost[2*i+1]
		p.add_resource(rid, -rct)
	}

	if level == 0 {
		level += 1
		p.db.Talents.Add(&dbPlayerTalentData{
			Id:    talent_id,
			Level: level,
		})
	} else {
		level += 1
		p.db.Talents.SetLevel(talent_id, level)
	}

	response := &msg_client_message.S2CTalentUpResponse{
		TalentId: talent_id,
		Level:    level,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_TALENT_UP_RESPONSE), response)

	log.Trace("Player[%v] update talent[%v] to level[%v]", p.Id, talent_id, level)

	return 1
}

func (p *Player) talent_reset(tag int32) int32 {
	if p.db.Talents.NumAll() <= 0 {
		return -1
	}

	if p.get_diamond() < global_config.TalentResetCostDiamond {
		log.Error("Player[%v] reset talent need diamond not enough", p.Id)
		return int32(msg_client_message.E_ERR_PLAYER_DIAMOND_NOT_ENOUGH)
	}

	return_items := make(map[int32]int32)
	talent_ids := p.db.Talents.GetAllIndex()
	for i := 0; i < len(talent_ids); i++ {
		talent_id := talent_ids[i]
		level, _ := p.db.Talents.GetLevel(talent_id)
		for l := int32(1); l <= level; l++ {
			t := talent_table_mgr.GetByIdLevel(talent_id, l)
			if t == nil {
				continue
			}
			if t.Tag != tag {
				continue
			}
			for n := 0; n < len(t.UpgradeCost)/2; n++ {
				rid := t.UpgradeCost[2*n]
				rcnt := t.UpgradeCost[2*n+1]
				return_items[rid] += rcnt
				p.add_resource(rid, rcnt)
			}
			if p.db.Talents.HasIndex(talent_id) {
				p.db.Talents.Remove(talent_id)
			}
		}
	}

	p.add_diamond(-global_config.TalentResetCostDiamond)

	var items []int32
	for k, v := range return_items {
		items = append(items, []int32{k, v}...)
	}
	response := &msg_client_message.S2CTalentResetResponse{
		Tag:         tag,
		ReturnItems: items,
		CostDiamond: global_config.TalentResetCostDiamond,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_TALENT_RESET_RESPONSE), response)

	log.Trace("Player[%v] reset talents tag[%v], return items %v, cost diamond %v", p.Id, tag, items, response.GetCostDiamond())

	return 1
}

func (p *Player) add_talent_attr(member *TeamMember) {
	all_tid := p.db.Talents.GetAllIndex()
	if all_tid == nil {
		return
	}

	for i := 0; i < len(all_tid); i++ {
		lvl, _ := p.db.Talents.GetLevel(all_tid[i])
		t := talent_table_mgr.GetByIdLevel(all_tid[i], lvl)
		if t == nil {
			log.Error("Player[%v] talent[%v] level[%v] data not found", p.Id, all_tid[i], lvl)
			continue
		}

		//log.Debug("talent[%v] effect_cond[%v] attrs[%v] skills[%v] first_hand[%v]", all_tid[i], t.TalentEffectCond, t.TalentAttr, t.TalentSkillList, t.TeamSpeedBonus)
		if member != nil && !member.is_dead() {
			if !_skill_check_cond(member, t.TalentEffectCond) {
				continue
			}
			member.add_attrs(t.TalentAttr)
			for k := 0; k < len(t.TalentSkillList); k++ {
				member.add_passive_skill(t.TalentSkillList[k])
			}
		}
	}
}

func C2STalentListHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2STalentListRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.send_talent_list()
}

func C2STalentUpHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2STalentUpRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.up_talent(req.GetTalentId())
}

func C2STalentResetHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2STalentResetRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.talent_reset(req.GetTag())
}
