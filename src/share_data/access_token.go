package share_data

import (
	"encoding/base64"
	"encoding/json"
	"ih_server/libs/log"

	"time"

	_ "github.com/gomodule/redigo/redis"
)

const (
	DEFAULT_ACCESS_TOKEN_EXIST_SECONDS = 24 * 3600
)

type AccessTokenInfo struct {
	UniqueId      string
	ExpireSeconds int32
}

func (ti *AccessTokenInfo) Init(unique_id string, exist_seconds int32) {
	ti.UniqueId = unique_id
	now_time := int32(time.Now().Unix())
	ti.ExpireSeconds = now_time + exist_seconds
}

func (ti *AccessTokenInfo) GetString() (bool, string) {
	bytes, err := json.Marshal(ti)
	if err != nil {
		log.Error("Access Token marshal json data err %v", err.Error())
		return false, ""
	}
	return true, base64.StdEncoding.EncodeToString(bytes)
}

func (ti *AccessTokenInfo) ParseString(token_string string) bool {
	bytes, err := base64.StdEncoding.DecodeString(token_string)
	if err != nil {
		log.Error("Access token decode token string %v err %v", token_string, err.Error())
		return false
	}
	err = json.Unmarshal(bytes, ti)
	if err != nil {
		log.Error("Access token decode json data err %v", err.Error())
		return false
	}
	return true
}

func (ti *AccessTokenInfo) IsExpired() bool {
	now_time := int32(time.Now().Unix())
	return now_time >= ti.ExpireSeconds
}

func GenerateAccessToken(unique_id string) string {
	var token AccessTokenInfo
	token.Init(unique_id, DEFAULT_ACCESS_TOKEN_EXIST_SECONDS)
	b, token_string := token.GetString()
	if !b {
		return ""
	}
	return token_string
}
