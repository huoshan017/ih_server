package main

import (
	"ih_server/libs/log"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	"time"

	"github.com/golang/protobuf/proto"
)

const (
	MAIL_TYPE_SYSTEM = 1 // 系统邮件
	MAIL_TYPE_PLAYER = 2 // 玩家邮件
	MAIL_TYPE_GUILD  = 3 // 公会邮件
)

func (p *dbPlayerMailColumn) GetMailList() (mails []*msg_client_message.MailBasicData) {
	p.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetMailList")
	defer p.m_row.m_lock.UnSafeRUnlock()

	now_time := int32(time.Now().Unix())

	var to_delete_mail []int32

	for _, v := range p.m_data {
		is_read := false
		if v.IsRead > 0 {
			is_read = true
		}

		has_attached := false
		if v.AttachItemIds != nil && len(v.AttachItemIds) > 0 {
			if now_time-v.SendUnix >= global_config.MailAttachExistDays*24*3600 {
				to_delete_mail = append(to_delete_mail, v.Id)
				continue
			}
			has_attached = true
		} else {
			if now_time-v.SendUnix >= global_config.MailNormalExistDays*24*3600 {
				to_delete_mail = append(to_delete_mail, v.Id)
				continue
			}
		}

		is_get_attached := false
		if v.IsGetAttached > 0 {
			is_get_attached = true
		}

		d := &msg_client_message.MailBasicData{
			Id:            v.Id,
			Type:          int32(v.Type),
			Subtype:       v.Subtype,
			Title:         v.Title,
			SenderId:      v.SenderId,
			SenderName:    v.SenderName,
			SendTime:      v.SendUnix,
			IsRead:        is_read,
			IsGetAttached: is_get_attached,
			HasAttached:   has_attached,
			Value:         v.ExtraValue,
		}
		mails = append(mails, d)
	}

	for _, v := range to_delete_mail {
		delete(p.m_data, v)
	}

	return
}

func (p *dbPlayerMailColumn) GetMailListByIds(mail_ids []int32) (mails []*msg_client_message.MailBasicData) {
	if mail_ids == nil {
		return
	}

	p.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetMailListByIds")
	defer p.m_row.m_lock.UnSafeRUnlock()
	for i := 0; i < len(mail_ids); i++ {
		md := p.m_data[mail_ids[i]]
		if md == nil {
			continue
		}
		is_read := false
		if md.IsRead > 0 {
			is_read = true
		}
		is_get_attached := false
		if md.IsGetAttached > 0 {
			is_get_attached = true
		}
		has_attached := false
		if md.AttachItemIds != nil && len(md.AttachItemIds) > 0 {
			has_attached = true
		}
		d := &msg_client_message.MailBasicData{
			Id:            md.Id,
			Type:          int32(md.Type),
			Subtype:       md.Subtype,
			Title:         md.Title,
			SendTime:      md.SendUnix,
			SenderId:      md.SenderId,
			SenderName:    md.SenderName,
			IsRead:        is_read,
			IsGetAttached: is_get_attached,
			HasAttached:   has_attached,
			Value:         md.ExtraValue,
		}
		mails = append(mails, d)
	}
	return
}

func _get_items_info_from(item_ids, item_nums []int32) (items []*msg_client_message.ItemInfo) {
	if item_ids != nil && item_nums != nil {
		ids_len := len(item_ids)
		nums_len := len(item_nums)
		l := ids_len
		if l > nums_len {
			l = nums_len
		}
		for i := 0; i < l; i++ {
			item := &msg_client_message.ItemInfo{
				Id:    item_ids[i],
				Value: item_nums[i],
			}
			items = append(items, item)
		}
	}
	return
}

func (p *dbPlayerMailColumn) GetMailDetail(mail_id int32) (attached_items []*msg_client_message.ItemInfo, content string) {
	p.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetMailDetail")
	defer p.m_row.m_lock.UnSafeRUnlock()

	d := p.m_data[mail_id]
	if d == nil {
		return
	}

	attached_items = _get_items_info_from(d.AttachItemIds, d.AttachItemNums)
	content = d.Content

	return
}

func (p *dbPlayerMailColumn) HasUnreadMail() bool {
	p.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.HasUnreadMail")
	defer p.m_row.m_lock.UnSafeRUnlock()

	now_time := int32(time.Now().Unix())
	for _, v := range p.m_data {
		if now_time-v.SendUnix < global_config.MailAttachExistDays*24*3600 {
			if v.IsRead <= 0 {
				return true
			}
		}
	}
	return false
}

func (p *Player) new_mail(typ int32, subtype int32, sender_id int32, sender_name, title, content string, extra_value int32) int32 {
	mail_max := global_config.MailMaxCount
	if p.db.Mails.NumAll() >= mail_max {
		first_id := int32(0)
		all_ids := p.db.Mails.GetAllIndex()
		if all_ids != nil {
			for i := 0; i < len(all_ids); i++ {
				if all_ids[i] < first_id || first_id == 0 {
					first_id = all_ids[i]
				}
			}
			if first_id > 0 {
				p.db.Mails.Remove(first_id)
			}
		}
	}
	new_id := p.db.MailCommon.IncbyCurrId(1)
	p.db.Mails.Add(&dbPlayerMailData{
		Id:         new_id,
		Type:       int8(typ),
		Subtype:    subtype,
		Title:      title,
		Content:    content,
		SendUnix:   int32(time.Now().Unix()),
		SenderId:   sender_id,
		SenderName: sender_name,
		ExtraValue: extra_value,
	})

	return new_id
}

func (p *Player) attach_mail_item(mail_id, item_id, item_num int32) int32 {
	if !p.db.Mails.HasIndex(mail_id) {
		return int32(msg_client_message.E_ERR_PLAYER_MAIL_NOT_FOUND)
	}
	item_ids, _ := p.db.Mails.GetAttachItemIds(mail_id)
	item_nums, _ := p.db.Mails.GetAttachItemNums(mail_id)
	item_ids = append(item_ids, item_id)
	item_nums = append(item_nums, item_num)
	p.db.Mails.SetAttachItemIds(mail_id, item_ids)
	p.db.Mails.SetAttachItemNums(mail_id, item_nums)
	return 1
}

/*
func (p *Player) delete_mail(mail_id int32) int32 {
	if !p.db.Mails.HasIndex(mail_id) {
		return int32(msg_client_message.E_ERR_PLAYER_MAIL_NOT_FOUND)
	}
	p.db.Mails.Remove(mail_id)
	return 1
}
*/

func (p *Player) cache_new_mail(mail_id int32) {
	p.new_mail_list_locker.Lock()
	defer p.new_mail_list_locker.Unlock()

	if p.new_mail_ids == nil {
		p.new_mail_ids = []int32{mail_id}
	} else {
		p.new_mail_ids = append(p.new_mail_ids, mail_id)
	}
}

func (p *Player) clear_cache_new_mails() {
	p.new_mail_list_locker.Lock()
	defer p.new_mail_list_locker.Unlock()
	if p.new_mail_ids != nil {
		p.new_mail_ids = nil
	}
}

func (p *Player) get_and_clear_cache_new_mails() (mails []*msg_client_message.MailBasicData) {
	p.new_mail_list_locker.Lock()
	defer p.new_mail_list_locker.Unlock()

	if p.new_mail_ids != nil {
		mails = p.db.Mails.GetMailListByIds(p.new_mail_ids)
		p.new_mail_ids = nil
	}
	return
}

func SendMail(sender *Player, receiver_id, mail_type, mail_subtype int32, title string, content string, attached_items []*msg_client_message.ItemInfo, _ int32) int32 {
	var items []int32
	if attached_items != nil {
		items = make([]int32, 2*len(attached_items))
		for i := 0; i < len(attached_items); i++ {
			items[2*i] = attached_items[i].GetId()
			items[2*i+1] = attached_items[i].GetValue()
		}
	}

	var err int32
	if mail_type == MAIL_TYPE_GUILD {
		if sender == nil {
			return -1
		}
		guild := guild_manager._get_guild(sender.Id, false)
		if guild == nil {
			log.Error("Player[%v] not join one guild, cant send guild mail", sender.Id)
			return int32(msg_client_message.E_ERR_PLAYER_MAIL_SEND_FAILED)
		}
		if sender.db.Guild.GetPosition() <= GUILD_POSITION_MEMBER {
			log.Error("Only president or officer send guild mail, player %v is not", sender.Id)
			return int32(msg_client_message.E_ERR_PLAYER_MAIL_SEND_FAILED)
		}
		ids := guild.Members.GetAllIndex()
		for _, id := range ids {
			err = RealSendMail(sender, id, mail_type, mail_subtype, title, content, items, 0)
			if err < 0 {
				break
			}
		}
	} else {
		err = RealSendMail(sender, receiver_id, mail_type, mail_subtype, title, content, items, 0)
	}

	return err
}

func mail_has_subtype(mail_subtype int32) int32 {
	var found bool
	arr := mail_table_mgr.Array
	if arr != nil {
		for i := 0; i < len(arr); i++ {
			if arr[i].MailSubtype == mail_subtype {
				found = true
				break
			}
		}
	}
	if !found {
		log.Error("System mail subtype %v not found", mail_subtype)
		return int32(msg_client_message.E_ERR_PLAYER_MAIL_SUBTYPE_UNKNOWN)
	}
	return 1
}

func RealSendMail(sender *Player, receiver_id, mail_type, mail_subtype int32, title string, content string, items []int32, extra_value int32) int32 {
	if int32(len(title)) > global_config.MailTitleBytes {
		if sender != nil {
			log.Error("Player[%v] send Mail title[%v] too long", sender.Id, title)
		} else {
			log.Error("Mail type[%v] title[%v] too long", mail_type, title)
		}
		return int32(msg_client_message.E_ERR_PLAYER_MAIL_TITLE_TOO_LONG)
	}
	if int32(len(content)) > global_config.MailContentBytes {
		if sender != nil {
			log.Error("Player[%v] send mail content[%v] too long", sender.Id, content)
		} else {
			log.Error("Mail type[%v] content[%v] too long", mail_type, content)
		}
		return int32(msg_client_message.E_ERR_PLAYER_MAIL_CONTENT_TOO_LONG)
	}

	now_time := int32(time.Now().Unix())
	if mail_type == MAIL_TYPE_PLAYER {
		if sender == nil {
			return -1
		}
		last_send := sender.db.MailCommon.GetLastSendPlayerMailTime()
		if now_time-last_send < global_config.MailPlayerSendCooldown {
			log.Error("Player[%v] tribe mail is cooldown", sender.Id)
			return int32(msg_client_message.E_ERR_PLAYER_MAIL_PLAYER_IS_COOLDOWN)
		}
	} else if mail_type == MAIL_TYPE_SYSTEM {
		res := mail_has_subtype(mail_subtype)
		if res < 0 {
			return res
		}
	}

	receiver := player_mgr.GetPlayerById(receiver_id)
	if receiver == nil {
		log.Error("Mail receiver[%v] not found", receiver_id)
		return int32(msg_client_message.E_ERR_PLAYER_MAIL_RECEIVER_NOT_FOUND)
	}

	var sender_id int32
	var sender_name string
	if sender != nil {
		sender_id = sender.Id
		sender_name = sender.db.GetName()
	}

	// 锁住保证新邮件生成的原子性
	receiver.receive_mail_locker.Lock()

	mail_id := receiver.new_mail(mail_type, mail_subtype, sender_id, sender_name, title, content, extra_value)
	if mail_id <= 0 {
		receiver.receive_mail_locker.Unlock()
		log.Error("new mail create failed")
		return int32(msg_client_message.E_ERR_PLAYER_MAIL_SEND_FAILED)
	}

	// 附件
	if items != nil {
		for i := 0; i < len(items)/2; i++ {
			item_id := items[2*i]
			item_num := items[2*i+1]
			if sender != nil {
				if sender.get_item(item_id) < item_num {
					receiver.receive_mail_locker.Unlock()
					log.Error("Player[%v] item[%v] not enough", sender.Id, item_id)
					return int32(msg_client_message.E_ERR_PLAYER_MAIL_SEND_FAILED)
				}
			}
			res := receiver.attach_mail_item(mail_id, item_id, item_num)
			if res < 0 {
				receiver.receive_mail_locker.Unlock()
				return res
			}
		}
		if sender != nil {
			for i := 0; i < len(items)/2; i++ {
				sender.add_resource(items[2*i], -items[2*i+1])
			}
		}
	}

	// 解锁
	receiver.receive_mail_locker.Unlock()

	// 缓存新邮件ID
	receiver.cache_new_mail(mail_id)

	// 个人邮件发送时间点保存
	if mail_type == MAIL_TYPE_PLAYER && sender != nil {
		sender.db.MailCommon.SetLastSendPlayerMailTime(now_time)
	}

	if sender != nil {
		log.Trace("Player[%v] send mail[%v] type[%v] title[%v] content[%v] to player %v", sender.Id, mail_id, mail_type, title, content, receiver_id)
	} else {
		log.Trace("System send mail[%v] type[%v] subtype[%v] to player %v", mail_id, mail_type, mail_subtype, receiver_id)
	}

	return mail_id
}

func (p *Player) SetSysMailSendTime(mail_id int32, send_time int32) bool {
	if !p.db.Mails.HasIndex(mail_id) {
		return false
	}
	p.db.Mails.SetSendUnix(mail_id, send_time)
	return true
}

func (p *Player) CheckNewMail() int32 {
	mails := p.get_and_clear_cache_new_mails()
	if mails == nil {
		return 1
	}
	response := &msg_client_message.S2CMailsNewNotify{
		Mails: mails,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_MAILS_NEW_NOTIFY), response)

	log.Trace("Player[%v] get new mails[%v] notify", p.Id, mails)

	return 1
}

func (p *Player) CheckSystemMail() {
	self_sys_mail_id := p.db.SysMail.GetCurrId()
	sys_mail_id := dbc.SysMailCommon.GetRow().GetCurrMailId()
	if self_sys_mail_id < sys_mail_id {
		for mail_id := self_sys_mail_id; mail_id <= sys_mail_id; mail_id++ {
			mail := dbc.SysMails.GetRow(mail_id)
			if mail == nil {
				continue
			}
			if mail.GetSendTime() >= p.db.Info.GetCreateUnix() {
				mid := RealSendMail(nil, p.Id, MAIL_TYPE_SYSTEM, mail.GetTableId(), "", "", mail.AttachedItems.Get().ItemList, 0)
				p.SetSysMailSendTime(mid, mail.GetSendTime())
			}
		}
		p.db.SysMail.SetCurrId(sys_mail_id)
	}
}

func (p *Player) GetMailList() int32 {
	p.clear_cache_new_mails()

	basic := p.db.Mails.GetMailList()
	if basic == nil {
		basic = make([]*msg_client_message.MailBasicData, 0)
	}
	response := &msg_client_message.S2CMailListResponse{
		Mails: basic,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_MAIL_LIST_RESPONSE), response)

	log.Debug("Player[%v] mail list: %v", p.Id, response)

	/*if p.db.NotifyStates.HasIndex(int32(msg_client_message.MODULE_STATE_NEW_MAIL)) {
		p.db.NotifyStates.Remove(int32(msg_client_message.MODULE_STATE_NEW_MAIL))
		p.notify_state_changed(int32(msg_client_message.MODULE_STATE_NEW_MAIL), 2)
	}*/

	return 1
}

func (p *Player) GetMailDetail(mail_ids []int32) int32 {
	if len(mail_ids) == 0 {
		return -1
	}

	var details []*msg_client_message.MailDetail
	for i := 0; i < len(mail_ids); i++ {
		if !p.db.Mails.HasIndex(mail_ids[i]) {
			return int32(msg_client_message.E_ERR_PLAYER_MAIL_NOT_FOUND)
		}

		p.db.Mails.SetIsRead(mail_ids[i], 1)
		attached_items, content := p.db.Mails.GetMailDetail(mail_ids[i])
		if attached_items == nil {
			attached_items = make([]*msg_client_message.ItemInfo, 0)
		}
		detail := &msg_client_message.MailDetail{
			Id:            mail_ids[i],
			Content:       content,
			AttachedItems: attached_items,
		}
		details = append(details, detail)
	}

	response := &msg_client_message.S2CMailDetailResponse{
		Mails: details,
	}

	p.Send(uint16(msg_client_message_id.MSGID_S2C_MAIL_DETAIL_RESPONSE), response)

	log.Trace("Player[%v] mails[%v] detail: %v", p.Id, mail_ids, response)

	return 1
}

func (p *Player) GetMailAttachedItems(mail_ids []int32) int32 {
	if mail_ids == nil {
		return -1
	}

	attached_items := make(map[int32]int32)
	for _, mail_id := range mail_ids {
		item_ids, o := p.db.Mails.GetAttachItemIds(mail_id)
		if !o {
			return int32(msg_client_message.E_ERR_PLAYER_MAIL_NOT_FOUND)
		}
		item_nums, _ := p.db.Mails.GetAttachItemNums(mail_id)
		items := _get_items_info_from(item_ids, item_nums)
		if items == nil {
			return int32(msg_client_message.E_ERR_PLAYER_MAIL_NO_ATTACHED_ITEM)
		}
		for i := 0; i < len(items); i++ {
			item_id := items[i].Id
			item_num := items[i].Value
			item := item_table_mgr.Get(item_id)
			if item != nil {
				p.add_resource(item_id, item_num)
			} else {
				if card_table_mgr.GetRankCard(item_id, 1) == nil {
					continue
				}
				if p.db.Roles.NumAll() >= global_config.MaxRoleCount {
					continue
				}
				p.new_role(item_id, 1, 1)
			}

			if attached_items[item_id] == 0 {
				attached_items[item_id] = item_num
			} else {
				attached_items[item_id] += item_num
			}
		}
		p.db.Mails.SetIsGetAttached(mail_id, 1)
		p.db.Mails.SetIsRead(mail_id, 1)
	}
	response := &msg_client_message.S2CMailGetAttachedItemsResponse{
		MailIds:       mail_ids,
		AttachedItems: Map2ItemInfos(attached_items),
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_MAIL_GET_ATTACHED_ITEMS_RESPONSE), response)

	log.Trace("Player[%v] mails[%v] get attached items: %v", p.Id, mail_ids, attached_items)

	return 1
}

func (p *Player) DeleteMails(mail_ids []int32) int32 {
	if len(mail_ids) == 0 {
		return -1
	}

	for i := 0; i < len(mail_ids); i++ {
		if !p.db.Mails.HasIndex(mail_ids[i]) {
			return int32(msg_client_message.E_ERR_PLAYER_MAIL_NOT_FOUND)
		}

		p.db.Mails.Remove(mail_ids[i])
	}

	response := &msg_client_message.S2CMailDeleteResponse{
		MailIds: mail_ids,
	}

	p.Send(uint16(msg_client_message_id.MSGID_S2C_MAIL_DELETE_RESPONSE), response)

	return 1
}

func C2SMailSendHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SMailSendRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	mail_id := SendMail(p, req.GetReceiverId(), req.GetMailType(), req.GetMailSubtype(), req.GetMailTitle(), req.GetMailContent(), req.GetAttachedItems(), 0)
	if mail_id > 0 {
		response := &msg_client_message.S2CMailSendResponse{
			MailId: mail_id,
		}
		p.Send(uint16(msg_client_message_id.MSGID_S2C_MAIL_SEND_RESPONSE), response)
	}
	return mail_id
}

func C2SMailListHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SMailListRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s) !", err.Error())
		return -1
	}
	return p.GetMailList()
}

func C2SMailDetailHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SMailDetailRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.GetMailDetail(req.GetIds())
}

func C2SMailGetAttachedItemsHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SMailGetAttachedItemsRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.GetMailAttachedItems(req.GetMailIds())
}

func C2SMailDeleteHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SMailDeleteRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.DeleteMails(req.GetMailIds())
}
