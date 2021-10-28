package main

import (
	"ih_server/libs/log"
	"ih_server/proto/gen_go/client_message"
	"ih_server/proto/gen_go/client_message_id"
	"ih_server/src/table_config"
	"math/rand"
	"time"

	"github.com/golang/protobuf/proto"
)

func (p *Player) drop_item_by_id(id int32, add bool, used_drop_ids map[int32]int32, tmp_cache_items map[int32]int32) (bool, *msg_client_message.ItemInfo) {
	drop_lib := drop_table_mgr.Map[id]
	if nil == drop_lib {
		return false, nil
	}
	item := p.drop_item(drop_lib, add, used_drop_ids, tmp_cache_items)
	return true, item
}

func (p *Player) drop_item(drop_lib *table_config.DropTypeLib, badd bool, used_drop_ids map[int32]int32, tmp_cache_items map[int32]int32) (item *msg_client_message.ItemInfo) {
	get_same := false
	check_cnt := drop_lib.TotalCount
	rand_val := rand.Int31n(drop_lib.TotalWeight)
	var tmp_item *table_config.XmlDropItem
	for i := int32(0); i < drop_lib.TotalCount; i++ {
		tmp_item = drop_lib.DropItems[i]
		if nil == tmp_item {
			continue
		}

		if tmp_item.Weight > rand_val || get_same {
			if tmp_item.DropItemID == 0 {
				return nil
			}

			if used_drop_ids != nil {
				if _, o := used_drop_ids[tmp_item.DropItemID]; o {
					get_same = true
					check_cnt -= 1
					if check_cnt <= 0 {
						break
					}
					//log.Debug("!!!!!!!!!!! !!!!!!!! total_count[%v]  used_drop_ids len[%v]  i[%v]", drop_lib.TotalCount, len(used_drop_ids), i)
					continue
				}
			}
			_, num := rand31n_from_range(tmp_item.Min, tmp_item.Max)
			if nil != item_table_mgr.Map[tmp_item.DropItemID] {
				if badd {
					if !p.add_resource(tmp_item.DropItemID, num) {
						log.Error("Player[%v] rand dropid[%d] not item resource", p.Id, tmp_item.DropItemID)
						continue
					}
				}
			} else {
				if card_table_mgr.GetCards(tmp_item.DropItemID) != nil {
					if badd {
						for j := int32(0); j < num; j++ {
							res := p.new_role(tmp_item.DropItemID, 1, 1)
							if res == 0 {
								log.Error("Player[%v] rand dropid[%d] not role resource", p.Id, tmp_item.DropItemID)
								continue
							} else if res < 0 {
								log.Warn("Player[%v] rand dropid[%v] transfer to piece because role bag is full", p.Id, tmp_item.DropItemID)
							}
						}
					}
				}
			}

			item = &msg_client_message.ItemInfo{Id: tmp_item.DropItemID, Value: num}
			if tmp_cache_items != nil {
				tmp_cache_items[tmp_item.DropItemID] += item.Value
			}
			break
		}

		rand_val -= tmp_item.Weight
	}

	return
}

func (p *Player) has_free_draw(draw_type int32, now_time int32) (bool, *table_config.XmlDrawItem) {
	draw := draw_table_mgr.Get(draw_type)
	if draw == nil {
		log.Error("Player[%v] draw id[%v] not found", p.Id, draw_type)
		return false, nil
	}

	var is_free bool

	if draw.FreeExtractTime > 0 {
		last_draw, o := p.db.Draws.GetLastDrawTime(draw_type)
		if !o || now_time-last_draw >= draw.FreeExtractTime {
			is_free = true
		}
	}

	return is_free, draw
}

func (p *Player) draw_card(draw_type int32) int32 {
	if draw_type > 100 {
		need_level := system_unlock_table_mgr.GetUnlockLevel("LifeTreeEnterLevel")
		if need_level > p.db.Info.GetLvl() {
			log.Error("Player[%v] level not enough level %v enter LifeTree", p.Id, need_level)
			return int32(msg_client_message.E_ERR_PLAYER_LEVEL_NOT_ENOUGH)
		}
	}

	now_time := time.Now()
	is_free, draw := p.has_free_draw(draw_type, int32(now_time.Unix()))
	if draw == nil {
		return -1
	}

	if p.db.Roles.NumAll()+draw.NeedBlank > global_config.MaxRoleCount {
		log.Error("Player[%v] role inventory not enough space", p.Id)
		return int32(msg_client_message.E_ERR_PLAYER_ROLE_INVENTORY_NOT_ENOUGH_SPACE)
	}

	// 资源
	is_enough := 0
	var res_condition []int32
	if !is_free {
		if (draw.ResCondition1 == nil || len(draw.ResCondition1) == 0) && (draw.ResCondition2 == nil || len(draw.ResCondition2) == 0) {
			is_enough = 3
		}
		if draw.ResCondition1 != nil && len(draw.ResCondition1) > 0 {
			i := 0
			for ; i < len(draw.ResCondition1)/2; i++ {
				res_id := draw.ResCondition1[2*i]
				res_num := draw.ResCondition1[2*i+1]
				if p.get_resource(res_id) < res_num {
					break
				}
			}
			if i >= len(draw.ResCondition1)/2 {
				res_condition = draw.ResCondition1
				is_enough = 1
			}
		}
		if is_enough == 0 {
			if draw.ResCondition2 != nil && len(draw.ResCondition2) > 0 {
				i := 0
				for ; i < len(draw.ResCondition2)/2; i++ {
					res_id := draw.ResCondition2[2*i]
					res_num := draw.ResCondition2[2*i+1]
					if p.get_resource(res_id) < res_num {
						break
					}
				}
				if i >= len(draw.ResCondition2)/2 {
					res_condition = draw.ResCondition2
					is_enough = 2
				}
			}
		}
		if is_enough == 0 {
			log.Error("Player[%v] not enough res to draw card", p.Id)
			return int32(msg_client_message.E_ERR_PLAYER_ITEM_NUM_NOT_ENOUGH)
		}
	}

	var drop_id []int32
	if (draw.FirstDropID != nil && len(draw.FirstDropID) > 0) && (!p.db.Draws.HasIndex(draw_type)) {
		drop_id = draw.FirstDropID
	} else {
		drop_id = draw.DropId
	}

	rand.Seed(now_time.Unix() + now_time.UnixNano())
	var role_ids []int32
	for i := 0; i < len(drop_id)/2; i++ {
		did := drop_id[2*i]
		dn := drop_id[2*i+1]
		for j := 0; j < int(dn); j++ {
			o, item := p.drop_item_by_id(did, true, nil, nil)
			if !o {
				log.Error("Player[%v] draw type[%v] with drop_id[%v] failed", p.Id, draw_type, did)
				return -1
			}
			role_ids = append(role_ids, []int32{item.GetId(), item.GetValue()}...)
		}
	}

	if !p.db.Draws.HasIndex(draw_type) {
		p.db.Draws.Add(&dbPlayerDrawData{
			Type:         draw_type,
			LastDrawTime: int32(now_time.Unix()),
		})
	}

	if !is_free {
		if res_condition != nil {
			for i := 0; i < len(res_condition)/2; i++ {
				res_id := res_condition[2*i]
				res_num := res_condition[2*i+1]
				p.add_resource(res_id, -res_num)
			}
		}
	} else {
		p.db.Draws.SetLastDrawTime(draw_type, int32(now_time.Unix()))
	}

	response := &msg_client_message.S2CDrawCardResponse{
		DrawType:    draw_type,
		RoleTableId: role_ids,
		IsFreeDraw:  is_free,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_DRAW_CARD_RESPONSE), response)

	if is_free {
		p.send_draw_data()
	}

	// 任务更新
	var a int32
	if draw_type == 1 || draw_type == 2 {
		a = 1
	} else if draw_type == 3 || draw_type == 4 {
		a = 2
	}
	p.TaskUpdate(table_config.TASK_COMPLETE_TYPE_DRAW_NUM, false, a, 1)
	if a == 2 {
		p.activitys_update(ACTIVITY_EVENT_DRAW_SCORE, draw.NeedBlank, 0, 0, 0)
	}

	log.Trace("Player[%v] drawed card[%v] with draw type[%v], is free[%v]", p.Id, role_ids, draw_type, is_free)

	return 1
}

func (p *Player) send_draw_data() int32 {
	free_secs := make(map[int32]int32)
	all_type := p.db.Draws.GetAllIndex()
	if len(all_type) > 0 {
		now_time := int32(time.Now().Unix())
		for _, t := range all_type {
			draw_time, _ := p.db.Draws.GetLastDrawTime(t)
			draw_data := draw_table_mgr.Get(t)
			if draw_data == nil {
				log.Warn("Cant found draw data with id[%v] in send player[%v] data", t, p.Id)
				continue
			}
			remain_seconds := draw_data.FreeExtractTime - (now_time - draw_time)
			if remain_seconds < 0 {
				remain_seconds = 0
			}
			free_secs[t] = remain_seconds
		}
	} else {
		for _, d := range draw_table_mgr.Array {
			if d != nil && d.FreeExtractTime > 0 {
				free_secs[d.Id] = 0
			}
		}
	}

	response := &msg_client_message.S2CDrawDataResponse{
		FreeDrawRemainSeconds: Map2ItemInfos(free_secs),
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_DRAW_DATA_RESPONSE), response)

	log.Trace("Player[%v] draw data is %v", p.Id, response)

	return 1
}

func C2SDrawCardHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SDrawCardRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.draw_card(req.GetDrawType())
}

func C2SDrawDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SDrawDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}

	return p.send_draw_data()
}
