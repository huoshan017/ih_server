package main

import (
	"ih_server/libs/log"
	"ih_server/proto/gen_go/client_message"
	"ih_server/proto/gen_go/client_message_id"
	"ih_server/src/table_config"
	"time"

	"github.com/golang/protobuf/proto"
)

func (p *Player) reset_gold_hand_left_nums() []int32 {
	ln := []int32{1, 1, 1}
	p.db.GoldHand.SetLeftNum(ln)
	return ln
}

func (p *Player) get_gold_hand_left_nums() []int32 {
	ln := p.db.GoldHand.GetLeftNum()
	if len(ln) == 0 {
		ln = p.reset_gold_hand_left_nums()
	}
	return ln
}

func (p *Player) if_gold_hand_reseted() bool {
	ln := p.get_gold_hand_left_nums()
	for i := 0; i < len(ln); i++ {
		if ln[i] <= 0 {
			return false
		}
	}
	return true
}

func (p *Player) check_reset_gold_hand(gold_hand_data *table_config.XmlGoldHandItem) (remain_seconds int32) {
	last_refresh := p.db.GoldHand.GetLastRefreshTime()
	now_time := int32(time.Now().Unix())
	remain_seconds = gold_hand_data.RefreshCD - (now_time - last_refresh)
	if remain_seconds < 0 {
		p.reset_gold_hand_left_nums()
		remain_seconds = 0
	}
	return
}

func (p *Player) has_free_gold_hand() bool {
	lvl := p.db.Info.GetLvl()
	gold_hand_data := goldhand_table_mgr.Get(lvl)
	if gold_hand_data == nil {
		log.Error("Goldhand data with level %v not found", lvl)
		return false
	}

	remain_seconds := p.check_reset_gold_hand(gold_hand_data)
	left_nums := p.get_gold_hand_left_nums()
	if remain_seconds <= 0 {
		if gold_hand_data.GemCost1 <= 0 && left_nums[0] > 0 {
			return true
		}
		if gold_hand_data.GemCost2 <= 0 && left_nums[2-1] > 0 {
			return true
		}
		if gold_hand_data.GemCost3 <= 0 && left_nums[3-1] > 0 {
			return true
		}
	}
	return false
}

func (p *Player) send_gold_hand() int32 {
	lvl := p.db.Info.GetLvl()
	gold_hand_data := goldhand_table_mgr.Get(lvl)
	if gold_hand_data == nil {
		log.Error("Goldhand data with level %v not found", lvl)
		return int32(msg_client_message.E_ERR_PLAYER_GOLDHAND_DATA_NOT_FOUND)
	}

	remain_seconds := p.check_reset_gold_hand(gold_hand_data)

	left_nums := make(map[int32]int32)
	ln := p.get_gold_hand_left_nums()
	for i := 0; i < len(ln); i++ {
		left_nums[int32(i+1)] = ln[i]
	}
	response := &msg_client_message.S2CGoldHandDataResponse{
		RemainRefreshSeconds: remain_seconds,
		LeftNums:             Map2ItemInfos(left_nums),
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_GOLD_HAND_DATA_RESPONSE), response)
	return 1
}

func (p *Player) touch_gold(t int32) int32 {
	lvl := p.db.Info.GetLvl()
	gold_hand := goldhand_table_mgr.Get(lvl)
	if gold_hand == nil {
		log.Error("Goldhand data with level %v not found", lvl)
		return int32(msg_client_message.E_ERR_PLAYER_GOLDHAND_DATA_NOT_FOUND)
	}
	/*last_refresh := p.db.GoldHand.GetLastRefreshTime()
	now_time := int32(time.Now().Unix())
	if now_time-last_refresh < gold_hand.RefreshCD {
		log.Error("Player[%v] gold hand is cooling down", p.Id)
		return int32(msg_client_message.E_ERR_PLAYER_GOLDHAND_REFRESH_IS_COOLINGDOWN)
	}*/

	p.check_reset_gold_hand(gold_hand)

	var gold, diamond int32
	if t == 1 {
		gold = gold_hand.GoldReward1
		diamond = gold_hand.GemCost1
	} else if t == 2 {
		gold = gold_hand.GoldReward2
		diamond = gold_hand.GemCost2
	} else if t == 3 {
		gold = gold_hand.GoldReward3
		diamond = gold_hand.GemCost3
	} else {
		log.Error("Gold Hand type[%v] invalid")
		return -1
	}

	left_nums := p.get_gold_hand_left_nums()
	if left_nums[t-1] <= 0 {
		log.Error("Player[%v] cant touch gold hand with type[%v], num not enough", p.Id, t)
		return int32(msg_client_message.E_ERR_PLAYER_GOLDHAND_REFRESH_IS_COOLINGDOWN)
	}

	if p.get_diamond() < diamond {
		log.Error("Player[%v] diamond not enough, cant touch gold %v", p.Id, t)
		return int32(msg_client_message.E_ERR_PLAYER_DIAMOND_NOT_ENOUGH)
	}

	var vip_gold int32
	vip_info := vip_table_mgr.Get(p.db.Info.GetVipLvl())
	if vip_info != nil {
		vip_gold = int32(int64(gold) * int64(vip_info.GoldFingerBonus) / 10000)
	}

	p.add_gold(gold + vip_gold)
	p.add_diamond(-diamond)

	if p.if_gold_hand_reseted() {
		p.db.GoldHand.SetLastRefreshTime(int32(time.Now().Unix()))
	}

	left_nums[t-1] -= 1
	p.db.GoldHand.SetLeftNum(left_nums)

	response := &msg_client_message.S2CTouchGoldResponse{
		Type:                     t,
		GetGold:                  gold + vip_gold,
		CostDiamond:              diamond,
		NextRefreshRemainSeconds: gold_hand.RefreshCD,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_TOUCH_GOLD_RESPONSE), response)

	p.send_gold_hand()

	// 更新任务
	p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_GOLD_HAND_NUM, false, 0, 1)

	return 1
}

func C2SGoldHandDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SGoldHandDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.send_gold_hand()
}

func C2STouchGoldHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2STouchGoldRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s)", err.Error())
		return -1
	}
	return p.touch_gold(req.GetType())
}
