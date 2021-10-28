package main

import (
	"ih_server/libs/log"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	_ "ih_server/src/table_config"

	"github.com/golang/protobuf/proto"
)

func (p *Player) artifact_data_update_notify(id, rank, level int32) {
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ARTIFACT_DATA_UPDATE_NOTIFY), &msg_client_message.S2CArtifactDataUpdateNotify{
		Id:    id,
		Rank:  rank,
		Level: level,
	})
}

func (p *Player) artifact_data() int32 {
	var item_list []*msg_client_message.ArtifactData
	ids := p.db.Artifacts.GetAllIndex()
	for _, id := range ids {
		if !p.db.Artifacts.HasIndex(id) {
			continue
		}
		level, _ := p.db.Artifacts.GetLevel(id)
		rank, _ := p.db.Artifacts.GetRank(id)
		if artifact_table_mgr.Get(id, rank, level) == nil {
			p.db.Artifacts.Remove(id)
			continue
		}
		item_list = append(item_list, &msg_client_message.ArtifactData{
			Id:    id,
			Level: level,
			Rank:  rank,
		})
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ARTIFACT_DATA_RESPONSE), &msg_client_message.S2CArtifactDataResponse{
		ArtifactList: item_list,
	})

	log.Trace("Player %v artifact data %v", p.Id, item_list)

	return 1
}

func (p *Player) artifact_unlock(id int32) int32 {
	if p.db.Artifacts.HasIndex(id) {
		log.Error("Player %v artifact %v already unlocked", p.Id, id)
		return int32(msg_client_message.E_ERR_ARTIFACT_ALREADY_UNLOCKED)
	}
	au := artifact_unlock_table_mgr.Get(id)
	if au == nil {
		log.Error("artifact %v unlock table data not found", id)
		return int32(msg_client_message.E_ERR_ARTIFACT_TABLE_UNLOCK_DATA_NOT_FOUND)
	}
	if p.db.GetLevel() < au.UnLockLevel {
		log.Error("Player %v artifact %v unlock level not reached", p.Id, id)
		return int32(msg_client_message.E_ERR_ARTIFACT_CANT_UNLOCK_WITH_CONDITION)
	}
	if p.db.Info.GetVipLvl() < au.UnLockVIPLevel {
		log.Error("Player %v artifact %v unlock vip level not reached", p.Id, id)
		return int32(msg_client_message.E_ERR_ARTIFACT_CANT_UNLOCK_WITH_CONDITION)
	}

	if au.UnLockResCost != nil {
		if !p.check_resources(au.UnLockResCost) {
			log.Error("Player %v artifact %v unlock not enough resource", p.Id, id)
			return int32(msg_client_message.E_ERR_PLAYER_ITEM_NUM_NOT_ENOUGH)
		}
	}

	a := artifact_table_mgr.Get(id, 1, 1)
	if a == nil {
		log.Error("artifact %v unlock with rank 1 and level 1 table data not found", id)
		return int32(msg_client_message.E_ERR_ARTIFACT_TABLE_DATA_NOT_FOUND)
	}

	p.db.Artifacts.Add(&dbPlayerArtifactData{
		Id:    id,
		Rank:  1,
		Level: 1,
	})
	if au.UnLockResCost != nil {
		p.cost_resources(au.UnLockResCost)
	}

	p.Send(uint16(msg_client_message_id.MSGID_S2C_ARTIFACT_UNLOCK_RESPONSE), &msg_client_message.S2CArtifactUnlockResponse{
		Id: id,
	})

	p.artifact_data_update_notify(id, 1, 1)

	log.Trace("Player %v artifact %v unlocked", p.Id, id)

	return 1
}

func (p *Player) artifact_levelup(id int32) int32 {
	if !p.db.Artifacts.HasIndex(id) {
		log.Error("Player %v artifact %v has not unlock", p.Id, id)
		return int32(msg_client_message.E_ERR_ARTIFACT_HAS_NOT_UNLOCK)
	}

	rank, _ := p.db.Artifacts.GetRank(id)
	level, _ := p.db.Artifacts.GetLevel(id)
	item := artifact_table_mgr.Get(id, rank, level)
	if item == nil {
		log.Error("artifact table data with id[%v] rank[%v] level[%v] not found", id, rank, level)
		return int32(msg_client_message.E_ERR_ARTIFACT_TABLE_DATA_NOT_FOUND)
	}
	if level >= item.MaxLevel {
		if rank >= artifact_table_mgr.GetMaxRank(id) {
			log.Error("Player %v artifact %v level and rank all max", p.Id, id)
			return int32(msg_client_message.E_ERR_ARTIFACT_LEVEL_IS_MAX)
		} else {
			log.Error("Player %v artifact %v level is max with rank %v", p.Id, id, rank)
			return int32(msg_client_message.E_ERR_ARTIFACT_MUST_RANKUP_TO_LEVELUP)
		}
	}

	if item.LevelUpResCost != nil {
		if !p.check_resources(item.LevelUpResCost) {
			log.Error("Player %v not enough resource to levelup artifact %v", p.Id, id)
			return int32(msg_client_message.E_ERR_PLAYER_ITEM_NUM_NOT_ENOUGH)
		}
	}

	next_level := level + 1
	if !p.db.Artifacts.HasIndex(id) {
		p.db.Artifacts.Add(&dbPlayerArtifactData{
			Id:    id,
			Rank:  1,
			Level: next_level,
		})
		rank = 1
	} else {
		p.db.Artifacts.IncbyLevel(id, 1)
	}

	if item.LevelUpResCost != nil {
		p.cost_resources(item.LevelUpResCost)
	}

	response := &msg_client_message.S2CArtifactLevelUpResponse{
		Id:    id,
		Level: next_level,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ARTIFACT_LEVELUP_RESPONSE), response)

	p.artifact_data_update_notify(id, rank, next_level)

	log.Trace("Player %v level up artifact %v to %v", p.Id, id, next_level)

	return 1
}

func (p *Player) artifact_rankup(id int32) int32 {
	if !p.db.Artifacts.HasIndex(id) {
		log.Error("Player %v artifact %v has not unlock", p.Id, id)
		return int32(msg_client_message.E_ERR_ARTIFACT_HAS_NOT_UNLOCK)
	}
	max_rank := artifact_table_mgr.GetMaxRank(id)
	rank, _ := p.db.Artifacts.GetRank(id)
	if max_rank <= rank {
		log.Error("Player %v artifact %v rank is max", p.Id, id)
		return int32(msg_client_message.E_ERR_ARTIFACT_RANK_IS_MAX)
	}
	level, _ := p.db.Artifacts.GetLevel(id)
	a := artifact_table_mgr.Get(id, rank, level)
	if a == nil {
		log.Error("artifact table not found data with id %v and rank %v and level %v", id, rank+1, level)
		return int32(msg_client_message.E_ERR_ARTIFACT_TABLE_DATA_NOT_FOUND)
	}

	if a.RankUpResCost != nil {
		if !p.check_resources(a.RankUpResCost) {
			log.Error("Player %v not have enough resource to rank up artifact %v", p.Id, id)
			return int32(msg_client_message.E_ERR_PLAYER_ITEM_NUM_NOT_ENOUGH)
		}
	}
	rank = p.db.Artifacts.IncbyRank(id, 1)
	if a.RankUpResCost != nil {
		p.cost_resources(a.RankUpResCost)
	}

	response := &msg_client_message.S2CArtifactRankUpResponse{
		Id:   id,
		Rank: rank,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_ARTIFACT_RANKUP_RESPONSE), response)

	p.artifact_data_update_notify(id, rank, level)

	log.Trace("Player %v rank up artifact %v to rank %v", p.Id, id, rank)

	return 1
}

func (p *Player) artifact_reset(id int32) int32 {
	if !p.db.Artifacts.HasIndex(id) {
		log.Error("Player %v artifact %v has not active, cant reset", p.Id, id)
		return int32(msg_client_message.E_ERR_ARTIFACT_HAS_NOT_UNLOCK)
	}

	rank, _ := p.db.Artifacts.GetRank(id)
	level, _ := p.db.Artifacts.GetLevel(id)

	if rank <= 1 && level <= 1 {
		log.Error("Player %v artifact %v no need to reset", p.Id, id)
		return int32(msg_client_message.E_ERR_ARTIFACT_NO_NEED_TO_RESET)
	}

	a := artifact_table_mgr.Get(id, rank, level)
	if a == nil {
		log.Error("artifact %v table data not found with rank %v and level %v", id, rank, level)
		return int32(msg_client_message.E_ERR_ARTIFACT_TABLE_DATA_NOT_FOUND)
	}

	rank = 1
	level = 1
	p.db.Artifacts.SetRank(id, rank)
	p.db.Artifacts.SetLevel(id, level)
	if a.DecomposeRes != nil {
		p.add_resources(a.DecomposeRes)
	}

	p.Send(uint16(msg_client_message_id.MSGID_S2C_ARTIFACT_RESET_RESPONSE), &msg_client_message.S2CArtifactResetResponse{
		Id: id,
	})

	p.artifact_data_update_notify(id, rank, level)

	log.Trace("Player %v reset artifact %v", p.Id, id)

	return 1
}

func (p *Player) artifact_add_member_attrs(member *TeamMember) {
	ids := p.db.Artifacts.GetAllIndex()
	for _, id := range ids {
		if !p.db.Artifacts.HasIndex(id) {
			continue
		}
		rank, _ := p.db.Artifacts.GetRank(id)
		level, _ := p.db.Artifacts.GetLevel(id)
		a := artifact_table_mgr.Get(id, rank, level)
		if a == nil {
			continue
		}

		if member != nil && !member.is_dead() {
			member.add_attrs(a.ArtifactAttr)
		}
	}
}

func C2SArtifactDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SArtifactDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.artifact_data()
}

func C2SArtifactUnlockHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SArtifactUnlockRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.artifact_unlock(req.GetId())
}

func C2SArtifactLevelUpHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SArtifactLevelUpRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.artifact_levelup(req.GetId())
}

func C2SArtifactRankUpHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SArtifactRankUpRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.artifact_rankup(req.GetId())
}

func C2SArtifactResetHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SArtifactResetRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.artifact_reset(req.GetId())
}
