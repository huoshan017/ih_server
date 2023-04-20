package main

import (
	"encoding/json"
	"ih_server/libs/log"
	"ih_server/libs/server_conn"
	"sync"
	"time"
)

const (
	UID_TOKEN_KEY = "ih:hall_server:uid_token_key"
)

type RedisLoginTokenInfo struct {
	Token      string
	PlayerId   int32
	CreateTime int32
}

type LoginTokenInfo struct {
	account      string
	token        string
	playerid     int32
	create_time  int32
	login_server *server_conn.ServerConn
}

type LoginTokenMgr struct {
	uid2token        map[string]*LoginTokenInfo
	acc2uid          map[string]string
	uid2token_locker *sync.RWMutex
}

var login_token_mgr LoginTokenMgr

func (m *LoginTokenMgr) Init() bool {
	m.uid2token = make(map[string]*LoginTokenInfo)
	m.acc2uid = make(map[string]string)
	m.uid2token_locker = &sync.RWMutex{}
	return true
}

func (m *LoginTokenMgr) LoadRedisData() int32 {
	cmd := hall_server.redis_conn.HGetAll("UID_TOKEN_KEY")
	result, err := cmd.Result()
	if err != nil {
		log.Error("redis获取集合[%v]数据失败[%v]", UID_TOKEN_KEY, err)
		return -1
	}

	for k, item := range result {
		jitem := &RedisLoginTokenInfo{}
		if err := json.Unmarshal([]byte(item), jitem); err != nil {
			log.Error("##### Load RedisLoginTokenInfo item[%v] error[%v]", item, err.Error())
			return -1
		}
		m.uid2token[k] = &LoginTokenInfo{
			token:       jitem.Token,
			create_time: jitem.CreateTime,
			playerid:    jitem.PlayerId,
		}
	}
	return 1
}

func _save_redis_login_token(uid, token string, now_time time.Time, player_id int32) {
	// serialize to redis
	var item RedisLoginTokenInfo = RedisLoginTokenInfo{
		Token:      token,
		CreateTime: int32(now_time.Unix()),
		PlayerId:   player_id,
	}
	bytes, err := json.Marshal(item)
	if err != nil {
		log.Error("##### Serialize item[%v] error[%v]", item, err.Error())
		return
	}
	cmd := hall_server.redis_conn.Do("HSET", UID_TOKEN_KEY, uid, string(bytes))
	if cmd.Err() != nil {
		log.Error("redis设置集合[%v]数据失败[%v]", UID_TOKEN_KEY, err.Error())
		return
	}
}

func (m *LoginTokenMgr) AddToUid2Token(uid, acc, token string, playerid int32, login_server *server_conn.ServerConn) {
	if uid == "" || acc == "" || token == "" {
		log.Error("LoginTokenMgr AddToUid2Token uid or acc or token empty")
		return
	}

	m.uid2token_locker.Lock()
	defer m.uid2token_locker.Unlock()

	now_time := time.Now()
	m.uid2token[uid] = &LoginTokenInfo{account: acc, token: token, create_time: int32(now_time.Unix()), playerid: playerid, login_server: login_server}
	m.acc2uid[acc] = uid

	_save_redis_login_token(uid, token, now_time, playerid)
}

func (m *LoginTokenMgr) BindNewAccount(uid, acc, new_acc string) bool {
	m.uid2token_locker.Lock()
	defer m.uid2token_locker.Unlock()

	token_info := m.uid2token[uid]
	if token_info.account != acc {
		log.Error("Bind New Account for old account %v invalid", acc)
		return false
	}

	token_info.account = new_acc

	delete(m.acc2uid, acc)
	m.acc2uid[new_acc] = uid

	return true
}

func (m *LoginTokenMgr) RemoveFromUid2Token(uid string) {
	if uid == "" {
		log.Error("LoginTokenMgr RemoveFromUid2Token uid empty !")
		return
	}

	m.uid2token_locker.Lock()
	defer m.uid2token_locker.Unlock()

	d := m.uid2token[uid]
	if nil != d {
		delete(m.acc2uid, d.account)
		delete(m.uid2token, uid)
	}
}

func (m *LoginTokenMgr) GetTokenByUid(uid string) *LoginTokenInfo {
	if uid == "" {
		log.Error("LoginTokenMgr GetTockenByUid uid empty")
		return nil
	}

	m.uid2token_locker.Lock()
	defer m.uid2token_locker.Unlock()

	return m.uid2token[uid]
}

func (m *LoginTokenMgr) GetLoginServerByUid(uid string) *server_conn.ServerConn {
	m.uid2token_locker.RLock()
	defer m.uid2token_locker.RUnlock()

	item := m.uid2token[uid]
	if item == nil {
		return nil
	}
	return item.login_server
}

func (m *LoginTokenMgr) SetToken(uid, token string, player_id int32) bool {
	m.uid2token_locker.Lock()
	defer m.uid2token_locker.Unlock()

	item := m.uid2token[uid]
	if item == nil {
		return false
	}
	item.token = token

	_save_redis_login_token(uid, token, time.Now(), player_id)

	return true
}

func (m *LoginTokenMgr) GetUidByAccount(acc string) string {
	m.uid2token_locker.RLock()
	defer m.uid2token_locker.RUnlock()

	return m.acc2uid[acc]
}
