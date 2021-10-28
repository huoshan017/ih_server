package main

import (
	"ih_server/libs/log"
	"ih_server/libs/utils"
	"ih_server/proto/gen_go/client_message"
	"ih_server/proto/gen_go/client_message_id"
	_ "ih_server/src/table_config"
	"time"

	"github.com/golang/protobuf/proto"
)

const (
	SIGN_RESET_TIME = "00:00:00"
)

func (d *dbPlayerSignColumn) has_reward() bool {
	d.m_row.m_lock.UnSafeRLock("dbPlayerSignColumn.has_reward")
	defer d.m_row.m_lock.UnSafeRUnlock()

	return d.m_data.AwardIndex < d.m_data.SignedIndex
}

func (p *Player) check_signed() (signed int32) {
	now_time := time.Now()
	last_signed := p.db.Sign.GetLastSignedTime()
	if last_signed == 0 {
		item := sign_table_mgr.Array[0]
		if item == nil {
			log.Error("Sign table is empty")
			return int32(msg_client_message.E_ERR_SIGN_TABLE_DATA_NOT_FOUND)
		}
		//p.db.Sign.SetCurrGroup(item.Group)
		p.db.Sign.SetSignedIndex(1)
		signed = 1
	} else {
		t := time.Unix(int64(last_signed), 0)
		/*curr_group := p.db.Sign.GetCurrGroup()
		group_items := sign_table_mgr.GetGroup(curr_group)
		if group_items == nil {
			log.Error("Sign table not found group %v data", curr_group)
			return int32(msg_client_message.E_ERR_SIGN_TABLE_DATA_NOT_FOUND)
		}*/
		if !(now_time.Year() == t.Year() && now_time.Month() == t.Month() && now_time.Day() == t.Day()) {
			curr_signed := p.db.Sign.GetSignedIndex()
			/*if int(curr_signed) >= len(group_items) {
				next_group := curr_group + 1
				group_items = sign_table_mgr.GetGroup(next_group)
				if group_items == nil {
					log.Error("Sign table not found next group %v data", next_group)
					return int32(msg_client_message.E_ERR_SIGN_TABLE_DATA_NOT_FOUND)
				}
				p.db.Sign.SetCurrGroup(next_group)
				p.db.Sign.SetSignedIndex(1)
				p.db.Sign.SetAwardIndex(0)
			} else {*/
			p.db.Sign.SetSignedIndex(curr_signed + 1)
			//}
			signed = 1
		}
	}

	if signed > 0 {
		p.db.Sign.SetLastSignedTime(int32(now_time.Unix()))
	}

	return
}

func (p *Player) get_sign_data() int32 {
	p.check_signed()
	response := &msg_client_message.S2CSignDataResponse{
		Group:                 p.db.Sign.GetCurrGroup(),
		TakeAwardIndex:        p.db.Sign.GetAwardIndex(),
		SignedIndex:           p.db.Sign.GetSignedIndex(),
		NextSignRemainSeconds: utils.GetRemainSeconds2NextDayTime(p.db.Sign.GetLastSignedTime(), SIGN_RESET_TIME),
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_SIGN_DATA_RESPONSE), response)
	log.Debug("Player[%v] sign data %v", p.Id, response)
	return 1
}

func (p *Player) sign_award(id int32) int32 {
	award_index := p.db.Sign.GetAwardIndex()
	if award_index >= id {
		log.Error("Player[%v] already award sign %v", p.Id, id)
		return int32(msg_client_message.E_ERR_SIGN_ALREADY_AWARD)
	}

	if award_index+1 != id {
		log.Error("Player[%v] must award in sequence", p.Id)
		return int32(msg_client_message.E_ERR_SIGN_MUST_AWARD_IN_SEQUENCE)
	}

	signed_index := p.db.Sign.GetSignedIndex()

	if award_index >= signed_index {
		log.Error("Player[%v] award all signs", p.Id)
		return int32(msg_client_message.E_ERR_SIGN_ALL_AWARDED)
	}

	/*curr_group := p.db.Sign.GetCurrGroup()
	group_items := sign_table_mgr.GetGroup(curr_group)
	if group_items == nil {
		log.Error("Player[%v] sign award with group[%v] not found", p.Id, curr_group)
		return -1
	}*/

	sign_item := sign_table_mgr.Get(id)
	if sign_item == nil {
		log.Error("Player[%v] sign award with id[%v] not found", p.Id, id)
		return -1
	}

	var rewards map[int32]int32
	reward := sign_item.Reward
	if reward != nil {
		p.add_resources(reward)
		for n := 0; n < len(reward)/2; n++ {
			if rewards == nil {
				rewards = make(map[int32]int32)
			}
			rewards[reward[2*n]] += reward[2*n+1]
		}
	}

	// 已领到的索引
	p.db.Sign.SetAwardIndex(id)

	response := &msg_client_message.S2CSignAwardResponse{
		Index:   id,
		Rewards: Map2ItemInfos(rewards),
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_SIGN_AWARD_RESPONSE), response)

	log.Trace("Player[%v] sign award %v", p.Id, response)

	return 1
}

func C2SSignDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SSignDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.get_sign_data()
}

func C2SSignAwardHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SSignAwardRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.sign_award(req.GetIndex())
}
