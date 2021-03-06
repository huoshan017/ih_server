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
	MONTH_CARD_DAYS_NUM = 30
)

type ChargeMonthCardManager struct {
	players_map     map[int32]*Player
	player_ids_chan chan int32
	to_end          int32
}

var charge_month_card_manager ChargeMonthCardManager

func (c *ChargeMonthCardManager) Init() {
	c.players_map = make(map[int32]*Player)
	c.player_ids_chan = make(chan int32, 10000)
}

func (c *ChargeMonthCardManager) InsertPlayer(player_id int32) {
	c.player_ids_chan <- player_id
}

func (c *ChargeMonthCardManager) _process_send_mails() {
	month_cards := pay_table_mgr.GetMonthCards()
	if month_cards == nil {
		return
	}

	now_time := time.Now()
	var to_delete_players map[int32]int32
	for pid, p := range c.players_map {
		if p == nil || !p.charge_month_card_award(month_cards, now_time) {
			if to_delete_players == nil {
				to_delete_players = make(map[int32]int32)
			}
			to_delete_players[pid] = 1
		}
	}

	for pid := range to_delete_players {
		delete(c.players_map, pid)
	}
}

func (c *ChargeMonthCardManager) Run() {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	for {
		if atomic.LoadInt32(&c.to_end) > 0 {
			break
		}

		var is_break bool
		for !is_break {
			select {
			case player_id, o := <-c.player_ids_chan:
				{
					if !o {
						log.Error("conn timer wheel op chan receive invalid !!!!!")
						return
					}
					if c.players_map[player_id] == nil {
						c.players_map[player_id] = player_mgr.GetPlayerById(player_id)
					}
				}
			default:
				{
					is_break = true
				}
			}
		}

		c._process_send_mails()

		time.Sleep(time.Minute)
	}
}

func (c *ChargeMonthCardManager) End() {
	atomic.StoreInt32(&c.to_end, 1)
}

func (c *Player) _charge_month_card_award(month_card *table_config.XmlPayItem, now_time time.Time) (send_num int32) {
	var bonus []int32 = []int32{ITEM_RESOURCE_ID_DIAMOND, month_card.MonthCardReward}
	// ?????????
	if month_card.Id == 2 {
		vip_info := vip_table_mgr.Get(c.db.Info.GetVipLvl())
		if vip_info != nil && vip_info.MonthCardItemBonus != nil && len(vip_info.MonthCardItemBonus) > 0 {
			bonus = append(bonus, vip_info.MonthCardItemBonus...)
		}
	}
	RealSendMail(nil, c.Id, MAIL_TYPE_SYSTEM, 1105, "", "", bonus, 0)
	send_num = c.db.Pays.IncbySendMailNum(month_card.BundleId, 1)
	log.Trace("Player[%v] charge month card %v get reward, send_num %v", c.Id, month_card.BundleId, send_num)
	return
}

func (c *Player) charge_month_card_award(month_cards []*table_config.XmlPayItem, now_time time.Time) bool {
	if !c.charge_has_month_card() {
		return false
	}
	for _, m := range month_cards {
		last_award_time, o := c.db.Pays.GetLastAwardTime(m.BundleId)
		if !o {
			continue
		}
		send_num, _ := c.db.Pays.GetSendMailNum(m.BundleId)
		if send_num >= MONTH_CARD_DAYS_NUM {
			continue
		}
		num := utils.GetDaysNumToLastSaveTime(last_award_time, global_config.MonthCardSendRewardTime, now_time)
		for i := int32(0); i < num; i++ {
			send_num = c._charge_month_card_award(m, now_time)
			if send_num >= MONTH_CARD_DAYS_NUM {
				break
			}
		}
		if num > 0 {
			c.db.Pays.SetLastAwardTime(m.BundleId, int32(now_time.Unix()))
		}
	}
	return true
}

func (c *Player) charge_has_month_card() bool {
	// ??????????????????
	arr := pay_table_mgr.GetMonthCards()
	if arr == nil {
		return false
	}

	for i := 0; i < len(arr); i++ {
		bundle_id := arr[i].BundleId
		send_num, o := c.db.Pays.GetSendMailNum(bundle_id)
		if o && send_num < MONTH_CARD_DAYS_NUM {
			return true
		}
	}

	return false
}

func (c *Player) charge_data() int32 {
	var charged_ids []string
	var datas []*msg_client_message.MonthCardData
	all_index := c.db.Pays.GetAllIndex()
	for _, idx := range all_index {
		pay_item := pay_table_mgr.GetByBundle(idx)
		if pay_item == nil {
			continue
		}
		if pay_item.PayType == table_config.PAY_TYPE_MONTH_CARD {
			payed_time, _ := c.db.Pays.GetLastPayedTime(idx)
			send_mail_num, _ := c.db.Pays.GetSendMailNum(idx)
			datas = append(datas, &msg_client_message.MonthCardData{
				BundleId:    idx,
				EndTime:     payed_time + MONTH_CARD_DAYS_NUM*24*3600,
				SendMailNum: send_mail_num,
			})
		}
		charged_ids = append(charged_ids, idx)
	}

	response := &msg_client_message.S2CChargeDataResponse{
		FirstChargeState: c.db.PayCommon.GetFirstPayState(),
		Datas:            datas,
		ChargedBundleIds: charged_ids,
	}
	c.Send(uint16(msg_client_message_id.MSGID_S2C_CHARGE_DATA_RESPONSE), response)

	log.Trace("Player[%v] charge data %v", c.Id, response)

	return 1
}

func (c *Player) charge(channel, id int32) int32 {
	pay_item := pay_table_mgr.Get(id)
	if pay_item == nil {
		return -1
	}
	res, _ := c._charge_with_bundle_id(channel, pay_item.BundleId, nil, nil, 0)
	return res
}

func (c *Player) _charge_with_bundle_id(channel int32, bundle_id string, purchase_data []byte, extra_data []byte, index int32) (int32, bool) {
	pay_item := pay_table_mgr.GetByBundle(bundle_id)
	if pay_item == nil {
		log.Error("pay %v table data not found", bundle_id)
		return int32(msg_client_message.E_ERR_CHARGE_TABLE_DATA_NOT_FOUND), false
	}

	var aid int32
	var sa *table_config.XmlSubActivityItem
	if pay_item.ActivePay > 0 {
		aid, sa = c.activity_get_one_charge(bundle_id)
		if aid <= 0 || sa == nil {
			log.Error("Player[%v] no activity charge %v with channel %v", c.Id, bundle_id, channel)
			return int32(msg_client_message.E_ERR_CHARGE_NO_ACTIVITY_ON_THIS_TIME), false
		}
	}

	has := c.db.Pays.HasIndex(bundle_id)
	if has {
		if pay_item.PayType == table_config.PAY_TYPE_MONTH_CARD {
			mail_num, o := c.db.Pays.GetSendMailNum(bundle_id)
			if o && mail_num < MONTH_CARD_DAYS_NUM {
				log.Error("Player[%v] payed month card %v is using, not outdate", c.Id, bundle_id)
				return int32(msg_client_message.E_ERR_CHARGE_MONTH_CARD_ALREADY_PAYED), false
			}
		}
	}

	if channel == 1 {
		err_code := verify_google_purchase_data(c, bundle_id, purchase_data, extra_data)
		if err_code < 0 {
			return err_code, false
		}
	} else if channel == 2 {
		err_code := verify_apple_purchase_data(c, bundle_id, purchase_data)
		if err_code < 0 {
			return err_code, false
		}
	} else if channel == 0 {
		// ?????????
	} else {
		log.Error("Player[%v] charge channel[%v] invalid", c.Id, channel)
		return int32(msg_client_message.E_ERR_CHARGE_CHANNEL_INVALID), false
	}

	if has {
		c.db.Pays.IncbyChargeNum(bundle_id, 1)
		c.add_diamond(pay_item.GemReward) // ??????????????????
		if pay_item.PayType == table_config.PAY_TYPE_MONTH_CARD {
			c.db.Pays.SetSendMailNum(bundle_id, 0)
			c.db.Pays.SetLastAwardTime(bundle_id, 0)
		}
	} else {
		c.db.Pays.Add(&dbPlayerPayData{
			BundleId: bundle_id,
		})
		c.add_diamond(pay_item.GemRewardFirst) // ??????????????????
	}

	c.add_resources(pay_item.ItemReward)

	if pay_item.PayType == table_config.PAY_TYPE_MONTH_CARD {
		charge_month_card_manager.InsertPlayer(c.Id)
	}

	now_time := time.Now()
	c.db.Pays.SetLastPayedTime(bundle_id, int32(now_time.Unix()))

	if aid > 0 && sa != nil {
		c.activity_update_one_charge(aid, sa)
	}

	if c.db.PayCommon.GetFirstPayState() == 0 {
		c.db.PayCommon.SetFirstPayState(1)
		// ????????????
		notify := &msg_client_message.S2CChargeFirstRewardNotify{}
		c.Send(uint16(msg_client_message_id.MSGID_S2C_CHARGE_FIRST_REWARD_NOTIFY), notify)
	}

	return 1, !has
}

func (c *Player) charge_with_bundle_id(channel int32, bundle_id string, purchase_data []byte, extra_data []byte, index int32) int32 {
	if channel <= 0 {
		log.Error("Player %v charge bundle id %v with channel %v invalid", c.Id, bundle_id, channel)
		return -1
	}

	res, is_first := c._charge_with_bundle_id(channel, bundle_id, purchase_data, extra_data, index)
	if res < 0 {
		return res
	}

	response := &msg_client_message.S2CChargeResponse{
		BundleId:    bundle_id,
		IsFirst:     is_first,
		ClientIndex: index,
	}
	c.Send(uint16(msg_client_message_id.MSGID_S2C_CHARGE_RESPONSE), response)

	log.Trace("Player[%v] charged bundle %v with channel %v", c.Id, response, channel)

	return 1
}

func (c *Player) charge_first_award() int32 {
	pay_state := c.db.PayCommon.GetFirstPayState()
	if pay_state == 0 {
		log.Error("Player[%v] not first charge, cant award", c.Id)
		return int32(msg_client_message.E_ERR_CHARGE_FIRST_NO_DONE)
	} else if pay_state == 2 {
		log.Error("Player[%v] first charge cant award repeated", c.Id)
		return int32(msg_client_message.E_ERR_CHARGE_FIRST_ALREADY_AWARD)
	} else if pay_state != 1 {
		log.Error("Player[%v] first charge state %v invalid", c.Id, pay_state)
		return -1
	}

	var rewards map[int32]int32
	first_charge_rewards := global_config.FirstChargeRewards
	if first_charge_rewards != nil {
		c.add_resources(first_charge_rewards)

		for i := 0; i < len(first_charge_rewards)/2; i++ {
			rid := first_charge_rewards[2*i]
			rn := first_charge_rewards[2*i+1]
			if rewards == nil {
				rewards = make(map[int32]int32)
			}
			rewards[rid] += rn
		}
	}

	c.db.PayCommon.SetFirstPayState(2)

	response := &msg_client_message.S2CChargeFirstAwardResponse{
		Rewards: Map2ItemInfos(rewards),
	}
	c.Send(uint16(msg_client_message_id.MSGID_S2C_CHARGE_FIRST_AWARD_RESPONSE), response)

	log.Trace("Player[%v] first charge award %v", c.Id, response)

	return 1
}

func C2SChargeDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SChargeDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%v)", err.Error())
		return -1
	}
	return p.charge_data()
}

func C2SChargeHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SChargeRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%v)", err.Error())
		return -1
	}
	return p.charge_with_bundle_id(req.GetChannel(), req.GetBundleId(), req.GetPurchareData(), req.GetExtraData(), req.GetClientIndex())
}

func C2SChargeFirstAwardHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SChargeFirstAwardRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%v)", err.Error())
		return -1
	}
	return p.charge_first_award()
}
