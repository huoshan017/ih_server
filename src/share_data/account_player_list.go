package share_data

import (
	"ih_server/libs/log"
	"ih_server/libs/utils"
	"sync"

	msg_client_message "ih_server/proto/gen_go/client_message"

	"github.com/golang/protobuf/proto"
)

const (
	UID_PLAYER_LIST_KEY = "ih:share_data:uid_player_list"
)

type PlayerList struct {
	player_list        []*msg_client_message.AccountPlayerInfo
	player_list_locker sync.RWMutex
}

func NewPlayerList(player_list []*msg_client_message.AccountPlayerInfo) *PlayerList {
	return &PlayerList{
		player_list: player_list,
	}
}

func (pl *PlayerList) GetList() []*msg_client_message.AccountPlayerInfo {
	pl.player_list_locker.RLock()
	defer pl.player_list_locker.RUnlock()
	return pl.player_list
}

func (pl *PlayerList) Insert(player *msg_client_message.AccountPlayerInfo) {
	pl.player_list_locker.Lock()
	defer pl.player_list_locker.Unlock()
	for i := 0; i < len(pl.player_list); i++ {
		if pl.player_list[i].GetServerId() == player.GetServerId() {
			return
		}
	}
	pl.player_list = append(pl.player_list, player)
}

func (pl *PlayerList) GetInfo(serverId uint32) *msg_client_message.AccountPlayerInfo {
	pl.player_list_locker.RLock()
	defer pl.player_list_locker.RUnlock()
	for i := 0; i < len(pl.player_list); i++ {
		if pl.player_list[i].GetServerId() == int32(serverId) {
			return pl.player_list[i]
		}
	}
	return nil
}

var player_list_map sync.Map

/*func LoadUidsPlayerList(redis_conn *utils.RedisConn) bool {
	var values map[string]string
	values, err := redis.StringMap(redis_conn.Do("HGETALL", UID_PLAYER_LIST_KEY))
	if err != nil {
		log.Error("redis get hashset %v all data err %v", UID_PLAYER_LIST_KEY, err.Error())
		return false
	}
	var msg msg_client_message.S2CAccountPlayerListResponse
	for k, v := range values {
		err = proto.Unmarshal([]byte(v), &msg)
		if err != nil {
			log.Error("account %v S2CAccountPlayerListResponse data unmarshal err %v", k, err.Error())
			return false
		}

		pl := NewPlayerList(msg.InfoList)
		player_list_map.Store(k, pl)
	}

	return true
}*/

func loadFromRedis(redis_conn *utils.RedisConn, uid string) []*msg_client_message.AccountPlayerInfo {
	cmd := redis_conn.Do("HGET", UID_PLAYER_LIST_KEY, uid)
	if cmd.Err() != nil {
		log.Error("redis get hashset %v with key %v err %v", UID_PLAYER_LIST_KEY, uid, cmd.Err())
		return nil
	}
	var msg msg_client_message.S2CAccountPlayerListResponse
	err := proto.Unmarshal([]byte(cmd.String()), &msg)
	if err != nil {
		log.Error("uid %v S2CAccountPlayerListResponse data unmarshal err %v", uid, err.Error())
		return nil
	}

	return msg.InfoList
}

func GetUidPlayerList(redis_conn *utils.RedisConn, uid string) *PlayerList {
	pl, o := player_list_map.Load(uid)
	if o {
		return pl.(*PlayerList)
	}

	playerList := loadFromRedis(redis_conn, uid)
	if playerList == nil {
		return nil
	}
	pl = NewPlayerList(playerList)
	player_list_map.Store(uid, pl)
	return pl.(*PlayerList)
}

func SaveUidPlayerInfo(redis_conn *utils.RedisConn, uid string, info *msg_client_message.AccountPlayerInfo) {
	player_list, o := player_list_map.Load(uid)
	if !o {
		player_list = NewPlayerList([]*msg_client_message.AccountPlayerInfo{info})
		player_list_map.Store(uid, player_list)
	} else {
		player_list.(*PlayerList).Insert(info)
	}
	var msg msg_client_message.S2CAccountPlayerListResponse
	msg.InfoList = player_list.(*PlayerList).player_list
	data, err := proto.Marshal(&msg)
	if err != nil {
		log.Error("redis marshal account %v info err %v", uid, err.Error())
		return
	}
	cmd := redis_conn.Do("HSET", UID_PLAYER_LIST_KEY, uid, data)
	if cmd.Err() != nil {
		log.Error("redis set hashset %v key %v data %v err %v", UID_PLAYER_LIST_KEY, uid, data, cmd.Err())
		return
	}
}

func GetUidPlayer(redis_conn *utils.RedisConn, uid string, server_id uint32) *msg_client_message.AccountPlayerInfo {
	player_list := GetUidPlayerList(redis_conn, uid)
	if player_list == nil {
		return nil
	}
	return player_list.GetInfo(server_id)
}
