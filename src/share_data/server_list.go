package share_data

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"ih_server/libs/log"
	"io/ioutil"
	"math/rand"
	"sync"
	"time"
)

const (
	CLIENT_OS_DEFAULT = ""
	CLIENT_OS_ANDROID = "android"
	CLIENT_OS_IOS     = "ios"
)

type HallServerInfo struct {
	Id        int32
	Name      string
	IP        string
	Weight    int32
	ClientOS  string
	VerifyUse bool
}

type CrossInfo struct {
	Id        int32
	ServerIds []int32
}

type ServerList struct {
	CommonIP          string
	Servers           []*HallServerInfo
	TotalWeight       int32
	CrossList         []*CrossInfo
	Id2Cross          map[int32]*CrossInfo
	ServerId2Cross    map[int32]*CrossInfo
	IosVerifyServerId int32
	ConfigPath        string
	MD5Str            string
	LastTime          int32
	Locker            sync.RWMutex
}

func _get_md5(data []byte) string {
	// md5校验码
	md5_ctx := md5.New()
	md5_ctx.Write(data)
	cipherStr := md5_ctx.Sum(nil)
	return hex.EncodeToString(cipherStr)
}

func (sl *ServerList) _read_config(data []byte) bool {
	err := json.Unmarshal(data, sl)
	if err != nil {
		fmt.Printf("解析配置文件失败 %v", err.Error())
		return false
	}

	var total_weight int32
	if sl.Servers != nil {
		for i := 0; i < len(sl.Servers); i++ {
			s := sl.Servers[i]
			if s.Weight < 0 {
				log.Error("Server Id %v Weight invalid %v", s.Id, s.Weight)
				return false
			} else if s.Weight == 0 {
				log.Trace("Server Id %v Weight %v", s.Id, s.Weight)
			}
			total_weight += s.Weight
		}
	}

	if total_weight <= 0 {
		log.Error("Server List Total Weight is invalid %v", total_weight)
		return false
	}

	sl.TotalWeight = total_weight

	if sl.CrossList != nil {
		sl.Id2Cross = make(map[int32]*CrossInfo)
		sl.ServerId2Cross = make(map[int32]*CrossInfo)
		for i := 0; i < len(sl.CrossList); i++ {
			c := sl.CrossList[i]
			if c == nil {
				continue
			}
			sl.Id2Cross[c.Id] = c
			if c.ServerIds != nil {
				for n := 0; n < len(c.ServerIds); n++ {
					sl.ServerId2Cross[c.ServerIds[n]] = c
				}
			}
		}
	}

	sl.MD5Str = _get_md5(data)

	return true
}

func (sl *ServerList) ReadConfig(filepath string) bool {
	data, err := ioutil.ReadFile(filepath)
	if err != nil {
		fmt.Printf("读取配置文件失败 %v", err)
		return false
	}

	if !sl._read_config(data) {
		return false
	}

	sl.ConfigPath = filepath

	return true
}

func (sl *ServerList) RereadConfig() bool {
	sl.Locker.Lock()
	defer sl.Locker.Unlock()

	data, err := ioutil.ReadFile(sl.ConfigPath)
	if err != nil {
		fmt.Printf("重新读取配置文件失败 %v", err)
		return false
	}

	md5_str := _get_md5(data)
	if md5_str == sl.MD5Str {
		return true
	}

	sl.Servers = nil
	sl.TotalWeight = 0

	if !sl._read_config(data) {
		return false
	}

	log.Trace("**** Server List updated")

	return true
}

func (sl *ServerList) GetServerById(id int32) (info *HallServerInfo) {
	sl.Locker.RLock()
	defer sl.Locker.RUnlock()

	if sl.Servers == nil {
		return
	}

	for i := 0; i < len(sl.Servers); i++ {
		if sl.Servers[i] == nil {
			continue
		}
		if sl.Servers[i].Id == id {
			info = sl.Servers[i]
			break
		}
	}
	return
}

func (sl *ServerList) RandomOneServer(client_os string) (info *HallServerInfo) {
	sl.Locker.RLock()
	defer sl.Locker.RUnlock()

	total_weight := sl.TotalWeight

	now_time := time.Now()
	rand.Seed(now_time.Unix() + now_time.UnixNano())
	r := rand.Int31n(total_weight)

	log.Debug("!!!!! Server List Random %v with client os %v", r, client_os)

	for i := 0; i < len(sl.Servers); i++ {
		s := sl.Servers[i]
		if s.Weight <= 0 {
			continue
		}
		if r < s.Weight {
			info = s
			break
		}
		r -= s.Weight
	}

	return
}

func (sl *ServerList) GetIosVerifyServerId() int32 {
	return sl.IosVerifyServerId
}

func (sl *ServerList) GetServers(client_os string) (servers []*HallServerInfo) {
	sl.Locker.RLock()
	defer sl.Locker.RUnlock()

	for i := 0; i < len(sl.Servers); i++ {
		s := sl.Servers[i]
		servers = append(servers, s)
	}
	return
}

func (sl *ServerList) HasServerId(server_id int32) bool {
	sl.Locker.RLock()
	defer sl.Locker.RUnlock()

	var found bool
	for i := 0; i < len(sl.Servers); i++ {
		s := sl.Servers[i]
		if s.Id == server_id {
			found = true
			break
		}
	}
	return found
}

func (sl *ServerList) GetCrossServers(id int32) *CrossInfo {
	sl.Locker.RLock()
	defer sl.Locker.RUnlock()
	return sl.Id2Cross[id]
}

func (sl *ServerList) GetCrossByServerId(server_id int32) *CrossInfo {
	sl.Locker.RLock()
	defer sl.Locker.RUnlock()
	return sl.ServerId2Cross[server_id]
}

func (sl *ServerList) IsSameCross(server1_id, server2_id int32) bool {
	sl.Locker.RLock()
	defer sl.Locker.RUnlock()

	return sl.ServerId2Cross[server1_id] == sl.ServerId2Cross[server2_id]
}

func (sl *ServerList) Run() {
	for {
		now_time := time.Now()
		if int32(now_time.Unix())-sl.LastTime >= 60 {
			sl.RereadConfig()
			sl.LastTime = int32(now_time.Unix())
		}
		time.Sleep(time.Minute)
	}
}
