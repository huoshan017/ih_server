package main

import (
	"ih_server/libs/log"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	"ih_server/src/table_config"
	"time"

	"github.com/golang/protobuf/proto"
)

const (
	CHAT_CHANNEL_NONE    = iota
	CHAT_CHANNEL_WORLD   = 1 // 世界
	CHAT_CHANNEL_GUILD   = 2 // 公会
	CHAT_CHANNEL_RECRUIT = 3 // 招募
	CHAT_CHANNEL_SYSTEM  = 4 // 系统公告
)

type PlayerChatData struct {
	curr_msg       *ChatItem
	curr_send_time int32
}

var world_chat_mgr ChatMgr
var recruit_chat_mgr ChatMgr
var system_chat_mgr ChatMgr

func get_chat_config_data(channel int32) *table_config.ChatConfig {
	if channel == CHAT_CHANNEL_WORLD {
		return &global_config.WorldChatData
	} else if channel == CHAT_CHANNEL_GUILD {
		return &global_config.GuildChatData
	} else if channel == CHAT_CHANNEL_RECRUIT {
		return &global_config.RecruitChatData
	} else if channel == CHAT_CHANNEL_SYSTEM {
		return &global_config.SystemChatData
	}
	return nil
}

func (p *Player) get_chat_mgr(channel int32) *ChatMgr {
	var chat_mgr *ChatMgr
	if channel == CHAT_CHANNEL_WORLD {
		chat_mgr = &world_chat_mgr
	} else if channel == CHAT_CHANNEL_GUILD {
		guild_id := p.db.Guild.GetId()
		chat_mgr = guild_manager.GetChatMgr(guild_id)
	} else if channel == CHAT_CHANNEL_RECRUIT {
		chat_mgr = &recruit_chat_mgr
	} else if channel == CHAT_CHANNEL_SYSTEM {
		chat_mgr = &system_chat_mgr
	}
	return chat_mgr
}

func (p *Player) get_chat_data(channel int32) (chat_data *PlayerChatData) {
	if channel == CHAT_CHANNEL_WORLD {
		chat_data = &p.world_chat_data
	} else if channel == CHAT_CHANNEL_GUILD {
		chat_data = &p.guild_chat_data
	} else if channel == CHAT_CHANNEL_RECRUIT {
		chat_data = &p.recruit_chat_data
	} else if channel == CHAT_CHANNEL_SYSTEM {
		chat_data = &p.system_chat_data
	}
	return
}

func (p *Player) chat(channel int32, content []byte, evalue int32) int32 {
	if config.DisableTestCommand && channel == CHAT_CHANNEL_SYSTEM {
		log.Error("Player[%v] cant chat in system channel", p.Id)
		return -1
	}

	chat_mgr := p.get_chat_mgr(channel)
	if chat_mgr == nil {
		log.Error("Player[%v] get chat mgr by channel %v failed", p.Id, channel)
		return int32(msg_client_message.E_ERR_CHAT_CHANNEL_CANT_GET)
	}

	now_time := int32(time.Now().Unix())
	cooldown_seconds := get_chat_send_msg_cooldown(channel)
	max_bytes := get_chat_msg_max_bytes(channel)

	last_chat_time, _ := p.db.Chats.GetLastChatTime(channel)
	if now_time-last_chat_time < cooldown_seconds {
		log.Error("Player[%v] channel[%v] chat is cooling down !", channel, p.Id)
		return int32(msg_client_message.E_ERR_CHAT_SEND_MSG_COOLING_DOWN)
	}
	if int32(len(content)) > max_bytes {
		log.Error("Player[%v] channel[%v] chat content length is too long !", channel, p.Id)
		return int32(msg_client_message.E_ERR_CHAT_SEND_MSG_BYTES_TOO_LONG)
	}

	var lvl, extra_value int32
	var name string
	if channel == CHAT_CHANNEL_RECRUIT {
		guild := guild_manager._get_guild(p.Id, false)
		if guild != nil {
			lvl = guild.GetLevel()
			name = guild.GetName()
		}
		extra_value = p.db.Guild.GetId()
	} else if channel == CHAT_CHANNEL_SYSTEM {
		extra_value = evalue
	} else {
		lvl = p.db.Info.GetLvl()
		name = p.db.GetName()
	}
	if !chat_mgr.push_chat_msg(content, extra_value, p.Id, lvl, name, p.db.Info.GetHead()) {
		return int32(msg_client_message.E_ERR_CHAT_CANT_SEND_WITH_NO_FREE)
	}

	if !p.db.Chats.HasIndex(channel) {
		p.db.Chats.Add(&dbPlayerChatData{
			Channel:      channel,
			LastChatTime: now_time,
		})
	} else {
		p.db.Chats.SetLastChatTime(channel, now_time)
	}

	response := &msg_client_message.S2CChatResponse{
		Channel: channel,
		Content: content,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_CHAT_RESPONSE), response)

	log.Trace("Player[%v] chat content[%v] in channel[%v]", p.Id, content, channel)

	return 1
}

func (p *Player) _pull_chat(chat_mgr *ChatMgr, channel, now_time int32, empty_is_send bool) int32 {
	msgs := chat_mgr.pull_chat(p)
	if !empty_is_send {
		if len(msgs) == 0 {
			return 0
		}
	}
	if !p.db.Chats.HasIndex(channel) {
		p.db.Chats.Add(&dbPlayerChatData{
			Channel:      channel,
			LastPullTime: now_time,
		})
	} else {
		p.db.Chats.SetLastPullTime(channel, now_time)
	}

	response := &msg_client_message.S2CChatMsgPullResponse{
		Channel: channel,
		Items:   msgs,
	}
	p.Send(uint16(msg_client_message_id.MSGID_S2C_CHAT_MSG_PULL_RESPONSE), response)

	log.Trace("Player[%v] pulled chat channel %v msgs %v", p.Id, channel, response)

	return 1
}

func (p *Player) pull_chat(channel int32) int32 {
	chat_mgr := p.get_chat_mgr(channel)
	if chat_mgr == nil {
		log.Error("Player[%v] get chat mgr by channel %v failed", p.Id, channel)
		return int32(msg_client_message.E_ERR_CHAT_CHANNEL_CANT_GET)
	}
	pull_msg_cooldown := get_chat_pull_msg_cooldown(channel)
	if pull_msg_cooldown < 0 {
		log.Error("Player[%v] pull chat with unknown channel %v", p.Id, channel)
		return int32(msg_client_message.E_ERR_CHAT_CHANNEL_CANT_GET)
	}

	now_time := int32(time.Now().Unix())
	pull_time, _ := p.db.Chats.GetLastPullTime(channel)
	if now_time-pull_time < pull_msg_cooldown {
		log.Error("Player[%v] pull channel[%v] chat msg is cooling down", p.Id, channel)
		//return int32(msg_client_message.E_ERR_CHAT_PULL_COOLING_DOWN)
		return 0
	}

	return p._pull_chat(chat_mgr, channel, now_time, true)
}

func (p *Player) has_new_chat_msg(channel int32) bool {
	chat_mgr := p.get_chat_mgr(channel)
	if chat_mgr == nil {
		//log.Error("Player[%v] get chat mgr by channel %v failed", p.Id, channel)
		return false
	}

	return chat_mgr.has_new_msg(p)
}

func (p *Player) check_and_pull_chat() {
	var channel_type_array []int32 = []int32{
		CHAT_CHANNEL_WORLD, CHAT_CHANNEL_GUILD, CHAT_CHANNEL_RECRUIT, CHAT_CHANNEL_SYSTEM,
	}
	now_time := int32(time.Now().Unix())
	for _, c := range channel_type_array {
		chat_mgr := p.get_chat_mgr(c)
		if chat_mgr == nil {
			continue
		}
		pull_msg_cooldown := get_chat_pull_msg_cooldown(c)
		if pull_msg_cooldown < 0 {
			continue
		}
		pull_time, _ := p.db.Chats.GetLastPullTime(c)
		if now_time-pull_time < pull_msg_cooldown {
			continue
		}
		p._pull_chat(chat_mgr, c, now_time, false)
	}
}

func C2SChatHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SChatRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}

	return p.chat(req.GetChannel(), req.GetContent(), 0)
}

func C2SChatPullMsgHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SChatMsgPullRequest
	err := proto.Unmarshal(msg_data, &req)
	if err != nil {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}

	if req.GetChannel() == CHAT_CHANNEL_WORLD {
		p.pull_chat(CHAT_CHANNEL_SYSTEM)
	}

	return p.pull_chat(req.GetChannel())
}
