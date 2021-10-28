package main

import (
	"ih_server/libs/log"
	"ih_server/src/table_config"
	"sync"
	"time"

	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"

	"github.com/golang/protobuf/proto"
)

const (
	ACTIVITY_EVENT_CHARGE        = 301 // 充值购买
	ACTIVITY_EVENT_EXCHAGE_ITEM  = 302 // 兑换物品
	ACTIVITY_EVENT_CHARGE_RETURN = 303 // 充值返利
	ACTIVITY_EVENT_GET_HERO      = 304 // 获得英雄
	ACTIVITY_EVENT_DIAMOND_COST  = 305 // 钻石消耗
	ACTIVITY_EVENT_EXPLORE       = 306 // 探索任务
	ACTIVITY_EVENT_DRAW_SCORE    = 307 // 高级抽卡
	ACTIVITY_EVENT_ARENA_SCORE   = 308 // 竞技场积分
)

const (
	ACTIVITY_CHARGE_EXTEND_MINUTES = 5
)

type ActivityManager struct {
	data_map map[int32]*table_config.XmlActivityItem
	locker   sync.RWMutex
}

var activity_mgr ActivityManager

func (am *ActivityManager) Run() {
	if activity_table_mgr.Array == nil {
		return
	}

	if am.data_map == nil {
		am.data_map = make(map[int32]*table_config.XmlActivityItem)
	}

	for {
		now_time := time.Now()
		for _, d := range activity_table_mgr.Array {
			start_time := d.StartTime
			end_time := d.EndTime
			// 充值活动延长5分钟
			if d.EventId == ACTIVITY_EVENT_CHARGE {
				end_time += ACTIVITY_CHARGE_EXTEND_MINUTES * 60
			}
			if start_time <= int32(now_time.Unix()) && end_time >= int32(now_time.Unix()) {
				am.locker.RLock()
				if am.data_map[d.Id] != nil {
					am.locker.RUnlock()
					continue
				}
				am.locker.RUnlock()

				am.locker.Lock()
				if am.data_map[d.Id] == nil {
					am.data_map[d.Id] = d
				}
				am.locker.Unlock()

				if dbc.ActivitysToDeletes.GetRow(d.Id) != nil {
					dbc.ActivitysToDeletes.RemoveRow(d.Id)
				}
			} else if end_time < int32(now_time.Unix()) {
				am.locker.RLock()
				if am.data_map[d.Id] == nil {
					am.locker.RUnlock()
					continue
				}
				am.locker.RUnlock()

				am.locker.Lock()
				if am.data_map[d.Id] != nil {
					delete(am.data_map, d.Id)
				}
				am.locker.Unlock()

				if dbc.ActivitysToDeletes.GetRow(d.Id) == nil {
					row := dbc.ActivitysToDeletes.AddRow(d.Id)
					if row != nil {
						row.SetStartTime(start_time)
						row.SetEndTime(end_time)
					}
				}
			}
		}
		time.Sleep(time.Second)
	}
}

func (am *ActivityManager) GetData() (data []*msg_client_message.ActivityData) {
	am.locker.RLock()
	defer am.locker.RUnlock()

	for _, v := range am.data_map {
		remain_seconds := GetRemainSeconds(v.StartTime, v.EndTime-v.StartTime)
		if remain_seconds > 0 {
			data = append(data, &msg_client_message.ActivityData{
				Id:            v.Id,
				RemainSeconds: remain_seconds,
			})
		}
	}

	return
}

func (am *ActivityManager) GetActivitysByEvent(event_type int32) (items []*table_config.XmlActivityItem) {
	am.locker.RLock()
	defer am.locker.RUnlock()

	for _, v := range am.data_map {
		if v.EventId != event_type {
			continue
		}
		start_time := v.StartTime
		end_time := v.EndTime
		if event_type == ACTIVITY_EVENT_CHARGE {
			end_time += ACTIVITY_CHARGE_EXTEND_MINUTES * 60
		}
		if GetRemainSeconds(start_time, end_time-start_time) > 0 {
			items = append(items, v)
		}
	}

	return
}

func (am *ActivityManager) IsDoing(id int32) bool {
	am.locker.RLock()
	defer am.locker.RUnlock()

	if am.data_map[id] == nil {
		return false
	}

	d := activity_table_mgr.Get(id)
	if d == nil {
		return false
	}

	if GetRemainSeconds(d.StartTime, d.EndTime-d.StartTime) <= 0 {
		return false
	}

	return true
}

func (am *Player) activity_data() int32 {
	ids := am.db.ActivityDatas.GetAllIndex()
	datas := activity_mgr.GetData()
	for _, id := range ids {
		if activity_table_mgr.Get(id) == nil {
			am.db.ActivityDatas.Remove(id)
			continue
		}
		if !activity_mgr.IsDoing(id) {
			am.db.ActivityDatas.Remove(id)
			continue
		}
		var found bool
		if ids != nil {
			for _, d := range datas {
				if d.GetId() == id {
					found = true
					break
				}
			}
		}
		if !found {
			datas = append(datas, &msg_client_message.ActivityData{Id: id})
		}
	}

	for _, d := range datas {
		id := int32(d.GetId())
		if dbc.ActivitysToDeletes.GetRow(id) != nil {
			if !activity_mgr.IsDoing(id) && am.db.ActivityDatas.HasIndex(id) {
				am.db.ActivityDatas.Remove(id)
				continue
			}
		}

		sub_num, _ := am.db.ActivityDatas.GetSubNum(id)
		sub_ids, _ := am.db.ActivityDatas.GetSubIds(id)
		sub_values, _ := am.db.ActivityDatas.GetSubValues(id)
		if sub_num > 0 && sub_ids != nil && sub_values != nil {
			for i := 0; i < int(sub_num); i++ {
				if i >= len(sub_ids) || i >= len(sub_values) {
					break
				}

				sid := sub_ids[i]
				d.SubDatas = append(d.SubDatas, &msg_client_message.SubActivityData{
					SubId: sid,
					Value: sub_values[i],
				})
			}
		}
	}

	am.Send(uint16(msg_client_message_id.MSGID_S2C_ACTIVITY_DATA_RESPONSE), &msg_client_message.S2CActivityDataResponse{
		Data: datas,
	})
	log.Trace("Player[%v] activity data %v", am.Id, datas)
	return 1
}

func (am *Player) activity_check_and_add_sub(id, sub_id, value int32) (sub_value, sub_num int32) {
	if !am.db.ActivityDatas.HasIndex(id) {
		am.db.ActivityDatas.Add(&dbPlayerActivityDataData{
			Id: id,
		})
	}

	sub_ids, _ := am.db.ActivityDatas.GetSubIds(id)
	sub_values, _ := am.db.ActivityDatas.GetSubValues(id)

	var found bool
	for i := 0; i < len(sub_ids); i++ {
		if sub_id == sub_ids[i] {
			sub_values[i] += value
			sub_value = sub_values[i]
			found = true
			break
		}
	}

	if !found {
		sub_ids = append(sub_ids, sub_id)
		sub_values = append(sub_values, value)
		sub_value = value
	}

	am.db.ActivityDatas.SetSubIds(id, sub_ids)
	am.db.ActivityDatas.SetSubValues(id, sub_values)
	sub_num = am.db.ActivityDatas.IncbySubNum(id, 1)

	return
}

func (am *Player) activity_get_one_charge(bundle_id string) (int32, *table_config.XmlSubActivityItem) {
	as := activity_mgr.GetActivitysByEvent(ACTIVITY_EVENT_CHARGE)
	if as == nil {
		return -1, nil
	}

	for _, a := range as {
		if a.SubActiveList == nil || len(a.SubActiveList) == 0 {
			continue
		}

		sub_num, o := am.db.ActivityDatas.GetSubNum(a.Id)
		sub_ids, _ := am.db.ActivityDatas.GetSubIds(a.Id)
		sub_values, _ := am.db.ActivityDatas.GetSubValues(a.Id)

		for _, sa_id := range a.SubActiveList {
			sa := sub_activity_table_mgr.Get(sa_id)
			if sa == nil || sa.EventCount <= 0 {
				continue
			}
			if sa.BundleID != bundle_id {
				continue
			}

			if !o {
				return a.Id, sa
			}

			var v int32
			if sub_ids != nil && sub_values != nil {
				for i := 0; i < int(sub_num); i++ {
					if i >= len(sub_ids) || i >= len(sub_values) {
						break
					}
					if sub_ids[i] == sa_id {
						if sa.EventCount > sub_values[i] {
							v = sub_values[i]
							break
						}
					}
				}
				if v < sa.EventCount {
					return a.Id, sa
				}
			}
		}
	}

	return -1, nil
}

// 充值
func (am *Player) activity_update_one_charge(id int32, sa *table_config.XmlSubActivityItem) {
	am.add_resources(sa.Reward)

	value, _ := am.activity_check_and_add_sub(id, sa.Id, 1)

	am.Send(uint16(msg_client_message_id.MSGID_S2C_ACTIVITY_DATA_NOTIFY), &msg_client_message.S2CActivityDataNotify{
		Id:    id,
		SubId: sa.Id,
		Value: value,
	})

	log.Trace("Player[%v] activity[%v,%v] update progress %v/%v", am.Id, id, sa.Id, value, sa.EventCount)
}

// 活动更新
func (am *Player) activity_update(a *table_config.XmlActivityItem, param1, param2, param3, param4 int32) {
	if a.SubActiveList == nil {
		return
	}

	for _, sa_id := range a.SubActiveList {
		sa := sub_activity_table_mgr.Get(sa_id)
		if sa == nil {
			continue
		}

		var param_value, sa_param int32
		if a.EventId == ACTIVITY_EVENT_GET_HERO {
			if sa.Param1 != param1 {
				continue
			}
			if sa.Param3 > 0 && sa.Param3 != param3 {
				continue
			}
			if sa.Param4 > 0 && sa.Param4 != param4 {
				continue
			}
			param_value = param2
			sa_param = sa.Param2
		} else if a.EventId == ACTIVITY_EVENT_DIAMOND_COST || a.EventId == ACTIVITY_EVENT_DRAW_SCORE || a.EventId == ACTIVITY_EVENT_ARENA_SCORE || a.EventId == ACTIVITY_EVENT_CHARGE_RETURN {
			param_value = param1
			sa_param = sa.Param1
		} else if a.EventId == ACTIVITY_EVENT_EXPLORE {
			if sa.Param1 != param1 {
				continue
			}
			param_value = param2
			sa_param = sa.Param2
		} else {
			continue
		}

		var v int32
		sub_num, _ := am.db.ActivityDatas.GetSubNum(a.Id)
		sub_ids, _ := am.db.ActivityDatas.GetSubIds(a.Id)
		sub_values, _ := am.db.ActivityDatas.GetSubValues(a.Id)
		if sub_ids != nil && sub_values != nil {
			for i := 0; i < int(sub_num); i++ {
				if i >= len(sub_ids) || i >= len(sub_values) {
					break
				}
				if sub_ids[i] == sa_id {
					v = sub_values[i]
					break
				}
			}
			if v >= sa_param {
				continue
			}
		}

		sub_value, _ := am.activity_check_and_add_sub(a.Id, sa_id, param_value)

		am.Send(uint16(msg_client_message_id.MSGID_S2C_ACTIVITY_DATA_NOTIFY), &msg_client_message.S2CActivityDataNotify{
			Id:    a.Id,
			SubId: sa_id,
			Value: sub_value,
		})

		if sub_value >= sa_param && a.RewardMailId > 0 {
			RealSendMail(nil, am.Id, MAIL_TYPE_SYSTEM, a.RewardMailId, "", "", sa.Reward, sa_param)
		}

		if a.EventId == ACTIVITY_EVENT_GET_HERO {
			log.Trace("Player[%v] get hero[star:%v camp:%v type:%v] for activity[%v,%v] update progress %v/%v", am.Id, param1, param3, param4, a.Id, sa_id, sub_value, sa_param)
		} else if a.EventId == ACTIVITY_EVENT_DIAMOND_COST {
			log.Trace("Player[%v] cost diamond %v for activity[%v,%v] update progress %v/%v", am.Id, param1, a.Id, sa_id, sub_value, sa_param)
		} else if a.EventId == ACTIVITY_EVENT_DRAW_SCORE {
			log.Trace("Player[%v] get draw score %v for activity[%v,%v] update progress %v/%v", am.Id, param1, a.Id, sa_id, sub_value, sa_param)
		} else if a.EventId == ACTIVITY_EVENT_ARENA_SCORE {
			log.Trace("Player[%v] get arena score %v for activity[%v,%v] update progress %v/%v", am.Id, param1, a.Id, sa_id, sub_value, sa_param)
		} else if a.EventId == ACTIVITY_EVENT_EXPLORE {
			log.Trace("Player[%v] explore star %v for activity[%v,%v] update progress %v/%v", am.Id, param1, a.Id, sa_id, sub_value, sa_param)
		} else if a.EventId == ACTIVITY_EVENT_CHARGE_RETURN {
			log.Trace("Player[%v] charged %v vip exp for activity[%v,%v] update progress %v/%v", am.Id, param1, a.Id, sa_id, sub_value, sa_param)
		}
	}
}

// 活动更新
func (am *Player) activitys_update(event_type, param1, param2, param3, param4 int32) {
	var as []*table_config.XmlActivityItem
	if event_type == ACTIVITY_EVENT_GET_HERO || event_type == ACTIVITY_EVENT_DIAMOND_COST || event_type == ACTIVITY_EVENT_EXPLORE || event_type == ACTIVITY_EVENT_DRAW_SCORE || event_type == ACTIVITY_EVENT_ARENA_SCORE || event_type == ACTIVITY_EVENT_CHARGE_RETURN { // 获得英雄
		as = activity_mgr.GetActivitysByEvent(event_type)
	} else {
		return
	}

	if as == nil {
		return
	}

	for _, a := range as {
		am.activity_update(a, param1, param2, param3, param4)
	}
}

func C2SActivityDataHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SActivityDataRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.activity_data()
}
