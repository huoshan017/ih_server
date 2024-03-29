package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"ih_server/libs/log"
	"ih_server/libs/timer"
	"ih_server/libs/utils"
	msg_client_message "ih_server/proto/gen_go/client_message"
	msg_client_message_id "ih_server/proto/gen_go/client_message_id"
	msg_server_message "ih_server/proto/gen_go/server_message"
	"ih_server/src/server_config"
	"ih_server/src/share_data"
	"io/ioutil"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	uuid "github.com/satori/go.uuid"
)

type WaitCenterInfo struct {
	res_chan chan *msg_server_message.C2LPlayerAccInfo
}

type LoginServer struct {
	start_time         time.Time
	quit               bool
	shutdown_lock      *sync.Mutex
	shutdown_completed bool
	ticker             *timer.TickTimer
	initialized        bool

	login_http_listener net.Listener
	login_http_server   http.Server
	use_https           bool

	redis_conn *utils.RedisConn

	acc2c_wait      map[string]*WaitCenterInfo
	acc2c_wait_lock *sync.RWMutex
}

var server *LoginServer

func (server *LoginServer) Init() (ok bool) {
	server.start_time = time.Now()
	server.shutdown_lock = &sync.Mutex{}
	server.acc2c_wait = make(map[string]*WaitCenterInfo)
	server.acc2c_wait_lock = &sync.RWMutex{}
	server.redis_conn = &utils.RedisConn{}
	account_mgr_init()

	server.initialized = true

	return true
}

func (server *LoginServer) Start(use_https bool) bool {
	if !server.redis_conn.Connect(config.RedisServerIP) {
		return false
	}

	/*if !share_data.LoadAccountsPlayerList(server.redis_conn) {
		return false
	}*/

	go server_list.Run()

	if use_https {
		go server.StartHttps(server_config.GetConfPathFile("server.crt"), server_config.GetConfPathFile("server.key"))
	} else {
		go server.StartHttp()
	}

	server.use_https = use_https
	log.Event("服务器已启动", nil, log.Property{Name: "IP", Value: config.ListenClientIP})
	log.Trace("**************************************************")

	server.Run()

	return true
}

func (server *LoginServer) Run() {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}

		server.shutdown_completed = true
	}()

	server.ticker = timer.NewTickTimer(1000)
	server.ticker.Start()
	defer server.ticker.Stop()

	go server.redis_conn.Run(100)

	for d := range server.ticker.Chan {
		begin := time.Now()
		server.OnTick(d)
		time_cost := time.Since(begin).Seconds()
		if time_cost > 1 {
			log.Trace("耗时 %v", time_cost)
			if time_cost > 30 {
				log.Error("耗时 %v", time_cost)
			}
		}
	}
}

func (server *LoginServer) Shutdown() {
	if !server.initialized {
		return
	}

	server.shutdown_lock.Lock()
	defer server.shutdown_lock.Unlock()

	if server.quit {
		return
	}
	server.quit = true

	server.redis_conn.Close()

	log.Trace("关闭游戏主循环")

	begin := time.Now()

	if server.ticker != nil {
		server.ticker.Stop()
	}

	for {
		if server.shutdown_completed {
			break
		}

		time.Sleep(time.Millisecond * 100)
	}

	server.login_http_listener.Close()
	center_conn.ShutDown()
	hall_agent_manager.net.Shutdown()

	dbc.Save(false)
	dbc.Shutdown()

	log.Trace("关闭游戏主循环耗时 %v 秒", time.Since(begin).Seconds())
}

func (server *LoginServer) OnTick(t timer.TickTime) {
}

/*
func (server *LoginServer) add_to_c_wait(acc string, c_wait *WaitCenterInfo) {
	server.acc2c_wait_lock.Lock()
	defer server.acc2c_wait_lock.Unlock()

	server.acc2c_wait[acc] = c_wait
}

func (server *LoginServer) remove_c_wait(acc string) {
	server.acc2c_wait_lock.Lock()
	defer server.acc2c_wait_lock.Unlock()

	delete(server.acc2c_wait, acc)
}

func (server *LoginServer) get_c_wait_by_acc(acc string) *WaitCenterInfo {
	server.acc2c_wait_lock.RLock()
	defer server.acc2c_wait_lock.RUnlock()

	return server.acc2c_wait[acc]
}
*/

func (server *LoginServer) pop_c_wait_by_acc(acc string) *WaitCenterInfo {
	server.acc2c_wait_lock.Lock()
	defer server.acc2c_wait_lock.Unlock()

	cur_wait := server.acc2c_wait[acc]
	if nil != cur_wait {
		delete(server.acc2c_wait, acc)
		return cur_wait
	}

	return nil
}

//=================================================================================

type LoginHttpHandle struct{}

func (server *LoginServer) StartHttp() bool {
	var err error
	server.reg_http_mux()

	server.login_http_listener, err = net.Listen("tcp", config.ListenClientIP)
	if nil != err {
		log.Error("LoginServer StartHttp Failed %s", err.Error())
		return false
	}

	login_http_server := http.Server{
		Handler:     &LoginHttpHandle{},
		ReadTimeout: 6 * time.Second,
	}

	err = login_http_server.Serve(server.login_http_listener)
	if err != nil {
		log.Error("启动Login Http Server %s", err.Error())
		return false
	}

	return true
}

func (server *LoginServer) StartHttps(crt_file, key_file string) bool {
	server.reg_http_mux()

	server.login_http_server = http.Server{
		Addr:        config.ListenClientIP,
		Handler:     &LoginHttpHandle{},
		ReadTimeout: 6 * time.Second,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}

	err := server.login_http_server.ListenAndServeTLS(crt_file, key_file)
	if err != nil {
		log.Error("启动https server error[%v]", err.Error())
		return false
	}

	return true
}

var login_http_mux map[string]func(http.ResponseWriter, *http.Request)

func (server *LoginServer) reg_http_mux() {
	login_http_mux = make(map[string]func(http.ResponseWriter, *http.Request))
	//login_http_mux["/register"] = register_http_handler
	//login_http_mux["/bind_new_account"] = bind_new_account_http_handler
	//login_http_mux["/login"] = login_http_handler
	//login_http_mux["/select_server"] = select_server_http_handler
	//login_http_mux["/set_password"] = set_password_http_handler
	login_http_mux["/client"] = client_http_handler
}

func (server *LoginHttpHandle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var act_str, url_str string
	url_str = r.URL.String()
	idx := strings.Index(url_str, "?")
	if -1 == idx {
		act_str = url_str
	} else {
		act_str = string([]byte(url_str)[:idx])
	}
	log.Info("ServeHTTP actstr(%s)", act_str)
	if h, ok := login_http_mux[act_str]; ok {
		h(w, r)
	}
}

/*
type JsonRequestData struct {
	MsgId   int32  // 消息ID
	MsgData []byte // 消息体
}

type JsonResponseData struct {
	Code    int32  // 错误码
	MsgId   int32  // 消息ID
	MsgData []byte // 消息体
}
*/

func _check_register(account, password string) (err_code int32) {
	if b, err := regexp.MatchString(`^[\.a-zA-Z0-9_-]+@[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+)+$`, account); !b {
		if err != nil {
			log.Error("account[%v] not valid account, err %v", account, err.Error())
		} else {
			log.Error("account[%v] not match", account)
		}
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_IS_INVALID)
		return
	}

	if password == "" {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_PASSWORD_INVALID)
		return
	}

	err_code = 1
	return
}

func _generate_account_uuid(account string) string {
	uid, err := uuid.NewV1()
	if err != nil {
		log.Error("Account %v generate uuid error %v", account, err.Error())
		return ""
	}
	return uid.String()
}

func register_handler(account, password string, is_guest bool) (err_code int32, resp_data []byte) {
	if len(account) > 128 {
		log.Error("Account[%v] length %v too long", account, len(account))
		return -1, nil
	}

	if len(password) > 32 {
		log.Error("Account[%v] password[%v] length %v too long", account, password, len(password))
		return -1, nil
	}

	if dbc.Accounts.GetRow(account) != nil {
		log.Error("Account[%v] already exists", account)
		return int32(msg_client_message.E_ERR_ACCOUNT_ALREADY_REGISTERED), nil
	}

	if !is_guest {
		err_code = _check_register(account, password)
		if err_code < 0 {
			return
		}
	}

	uid := _generate_account_uuid(account)
	if uid == "" {
		err_code = -1
		return
	}

	row := dbc.Accounts.AddRow(account)
	if row == nil {
		err_code = -1
		return
	}
	row.SetUniqueId(uid)
	row.SetPassword(password)
	row.SetRegisterTime(int32(time.Now().Unix()))
	if is_guest {
		row.SetChannel("guest")
	}

	var response msg_client_message.S2CRegisterResponse = msg_client_message.S2CRegisterResponse{
		Account:  account,
		Password: password,
		IsGuest:  is_guest,
	}

	var err error
	resp_data, err = proto.Marshal(&response)
	if err != nil {
		err_code = int32(msg_client_message.E_ERR_INTERNAL)
		log.Error("login_handler marshal response error: %v", err.Error())
		return
	}

	log.Debug("Account[%v] password[%v] registered", account, password)

	err_code = 1
	return
}

func bind_new_account_handler(server_id int32, account, password, new_account, new_password, new_channel string) (err_code int32, resp_data []byte) {
	if len(new_account) > 128 {
		log.Error("Account[%v] length %v too long", new_account, len(new_account))
		return -1, nil
	}

	if new_channel != "facebook" && len(new_password) > 32 {
		log.Error("Account[%v] password[%v] length %v too long", new_account, new_password, len(new_password))
		return -1, nil
	}

	if account == new_account {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_NAME_MUST_DIFFRENT_TO_OLD)
		log.Error("Account %v can not bind same new account", account)
		return
	}

	row := dbc.Accounts.GetRow(account)
	if row == nil {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_NOT_REGISTERED)
		log.Error("Account %v not registered, cant bind new account", account)
		return
	}

	ban_row := dbc.BanPlayers.GetRow(row.GetUniqueId())
	if ban_row != nil && ban_row.GetStartTime() > 0 {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_BE_BANNED)
		log.Error("Account %v has been banned, cant login", account)
		return
	}

	if row.GetPassword() != password {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_PASSWORD_INVALID)
		log.Error("Account %v password %v invalid, cant bind new account", account, password)
		return
	}

	if row.GetChannel() != "guest" && row.GetChannel() != "facebook" {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_NOT_GUEST)
		log.Error("Account %v not guest and not facebook user", account)
		return
	}

	if row.GetChannel() != "facebook" && row.GetBindNewAccount() != "" {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_ALREADY_BIND)
		log.Error("Account %v already bind", account)
		return
	}

	if dbc.Accounts.GetRow(new_account) != nil {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_NEW_BIND_ALREADY_EXISTS)
		log.Error("New Account %v to bind already exists", new_account)
		return
	}

	if new_channel != "" {
		if new_channel == "facebook" {
			err_code = _verify_facebook_login(new_account, new_password)
			if err_code < 0 {
				return
			}
		} else {
			err_code = -1
			log.Error("Account %v bind a unsupported channel %v account %v", account, new_channel, new_account)
			return
		}
	} else {
		err_code = _check_register(new_account, new_password)
		if err_code < 0 {
			return
		}
	}

	row.SetBindNewAccount(new_account)
	register_time := row.GetRegisterTime()
	uid := row.GetUniqueId()

	last_server_id := row.GetLastSelectServerId()

	row = dbc.Accounts.AddRow(new_account)
	if row == nil {
		err_code = -1
		log.Error("Account %v bind new account %v database error", account, new_account)
		return
	}

	if new_channel == "" {
		row.SetPassword(new_password)
	}
	row.SetRegisterTime(register_time)
	row.SetUniqueId(uid)
	row.SetOldAccount(account)
	row.SetLastSelectServerId(last_server_id)

	//dbc.Accounts.RemoveRow(account) // 暂且不删除

	hall_agent := hall_agent_manager.GetAgentByID(server_id)
	if nil == hall_agent {
		err_code = int32(msg_client_message.E_ERR_PLAYER_SELECT_SERVER_NOT_FOUND)
		log.Error("login_http_handler get hall_agent failed")
		return
	}

	req := &msg_server_message.L2HBindNewAccountRequest{
		UniqueId:   uid,
		Account:    account,
		NewAccount: new_account,
	}
	hall_agent.Send(uint16(msg_server_message.MSGID_L2H_BIND_NEW_ACCOUNT_REQUEST), req)

	response := &msg_client_message.S2CGuestBindNewAccountResponse{
		Account:     account,
		NewAccount:  new_account,
		NewPassword: new_password,
		NewChannel:  new_channel,
	}

	var err error
	resp_data, err = proto.Marshal(response)
	if err != nil {
		err_code = int32(msg_client_message.E_ERR_INTERNAL)
		log.Error("login_handler marshal response error: %v", err.Error())
		return
	}

	log.Debug("Account[%v] bind new account[%v]", account, new_account)
	err_code = 1
	return
}

func _verify_facebook_login(user_id, input_token string) int32 {

	var resp *http.Response
	var err error
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	type _facebook_data struct {
		AppID     string `json:"app_id"`
		IsValid   bool   `json:"is_valid"`
		UserID    string `json:"user_id"`
		IssuedAt  int    `json:"issued_at"`
		ExpiresAt int    `json:"expires_at"`
	}

	type facebook_data struct {
		Data _facebook_data `json:"data"`
	}

	var verified bool
	for i := 0; i < len(config.Facebook); i++ {
		url_str := fmt.Sprintf("https://graph.facebook.com/debug_token?input_token=%v&access_token=%v|%v", input_token, config.Facebook[i].FacebookAppID, config.Facebook[i].FacebookAppSecret)
		log.Debug("verify facebook url: %v", url_str)

		client := &http.Client{Transport: tr}
		resp, err = client.Get(url_str)
		if nil != err {
			log.Error("Facebook verify error %s", err.Error())
			continue
		}

		if resp.StatusCode != 200 {
			log.Error("Facebook verify response code %v", resp.StatusCode)
			continue
		}

		var data []byte
		data, err = ioutil.ReadAll(resp.Body)
		if nil != err {
			log.Error("Read facebook verify result err(%s) !", err.Error())
			continue
		}

		log.Debug("facebook verify result data: %v", string(data))

		var fdata facebook_data
		err = json.Unmarshal(data, &fdata)
		if nil != err {
			log.Error("Facebook verify ummarshal err(%s)", err.Error())
			continue
		}

		if !fdata.Data.IsValid {
			log.Error("Facebook verify input_token[%v] failed", input_token)
			continue
		}

		if fdata.Data.UserID != user_id {
			log.Error("Facebook verify client user_id[%v] different to result user_id[%v]", user_id, fdata.Data.UserID)
			continue
		}

		verified = true
		break
	}

	if !verified {
		return -1
	}

	log.Debug("Facebook verify user_id[%v] and input_token[%v] success", user_id, input_token)

	return 1
}

func _save_aaid(account, aaid string) {
	if aaid != "" {
		acc_aaid := account + "_" + aaid
		acc_aaid_row := dbc.AccountAAIDs.GetRow(acc_aaid)
		if acc_aaid_row == nil {
			acc_aaid_row = dbc.AccountAAIDs.AddRow(acc_aaid)
			acc_aaid_row.SetAccount(account)
			acc_aaid_row.SetAAID(aaid)
		}
	}
}

func login_handler(account, password, channel, client_os, aaid string) (err_code int32, resp_data []byte) {
	var err error
	acc_row := dbc.Accounts.GetRow(account)
	now_time := time.Now()
	if config.VerifyAccount {
		if channel == "" {
			if acc_row == nil {
				err_code = int32(msg_client_message.E_ERR_PLAYER_ACC_OR_PASSWORD_ERROR)
				log.Error("Account %v not exist", account)
				return
			}
			if acc_row.GetPassword() != password {
				err_code = int32(msg_client_message.E_ERR_PLAYER_ACC_OR_PASSWORD_ERROR)
				log.Error("Account %v password %v invalid", account, password)
				return
			}
		} else if channel == "facebook" {
			err_code = _verify_facebook_login(account, password)
			if err_code < 0 {
				return
			}
			if acc_row == nil {
				acc_row = dbc.Accounts.AddRow(account)
				if acc_row == nil {
					log.Error("Account %v add row with channel facebook failed")
					return -1, nil
				}
				acc_row.SetChannel("facebook")
				acc_row.SetRegisterTime(int32(now_time.Unix()))
			}
			acc_row.SetPassword(password)
		} else if channel == "guest" {
			if acc_row == nil {
				acc_row = dbc.Accounts.AddRow(account)
				if acc_row == nil {
					log.Error("Account %v add row with channel guest failed")
					return -1, nil
				}
				acc_row.SetChannel("guest")
				acc_row.SetRegisterTime(int32(now_time.Unix()))
			} else {
				if acc_row.GetPassword() != password {
					err_code = int32(msg_client_message.E_ERR_PLAYER_ACC_OR_PASSWORD_ERROR)
					log.Error("Account %v password %v invalid", account, password)
					return
				}
			}
		} else {
			log.Error("Account %v use unsupported channel %v login", account, channel)
			return -1, nil
		}
	} else {
		if acc_row == nil {
			acc_row = dbc.Accounts.AddRow(account)
			if acc_row == nil {
				log.Error("Account %v add row without verify failed")
				return -1, nil
			}
			acc_row.SetRegisterTime(int32(now_time.Unix()))
		}
	}

	if acc_row.GetUniqueId() == "" {
		uid := _generate_account_uuid(account)
		if uid != "" {
			acc_row.SetUniqueId(uid)
		}
	}

	playerList := share_data.GetUidPlayerList(server.redis_conn, acc_row.GetUniqueId())
	last_time := acc_row.GetLastGetAccountPlayerListTime()
	if int32(now_time.Unix())-last_time >= 5*60 {
		if playerList == nil {
			log.Warn("load player(uid %v) list failed", acc_row.GetUniqueId())
		} else {
			acc_row.SetLastGetAccountPlayerListTime(int32(now_time.Unix()))
		}
	}

	ban_row := dbc.BanPlayers.GetRow(acc_row.GetUniqueId())
	if ban_row != nil && ban_row.GetStartTime() > 0 {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_BE_BANNED)
		log.Error("Account %v has been banned, cant login", account)
		return
	}

	// --------------------------------------------------------------------------------------------
	// 选择默认服
	var select_server_id int32
	select_server_id = acc_row.GetLastSelectServerId()
	if select_server_id <= 0 {
		select_server_id = acc_row.GetLastSelectIOSServerId()
		if select_server_id <= 0 {
			server := server_list.RandomOneServer(client_os)
			if server == nil {
				err_code = int32(msg_client_message.E_ERR_INTERNAL)
				log.Error("Server List random null !!!")
				return
			}
			select_server_id = server.Id
			acc_row.SetLastSelectServerId(select_server_id)
		}
	}

	var hall_ip, token string
	err_code, hall_ip, token = _select_server(acc_row.GetUniqueId(), account, select_server_id)
	if err_code < 0 {
		return
	}
	// --------------------------------------------------------------------------------------------

	account_login(account, token, client_os)

	acc_row.SetToken(token)

	_save_aaid(account, aaid)

	response := &msg_client_message.S2CLoginResponse{
		Acc:    account,
		Token:  token,
		GameIP: hall_ip,
	}

	if server_list.Servers == nil {
		response.Servers = make([]*msg_client_message.ServerInfo, 0)
	} else {
		servers := server_list.GetServers(client_os)
		l := len(servers)
		response.Servers = make([]*msg_client_message.ServerInfo, l)
		for i := 0; i < l; i++ {
			response.Servers[i] = &msg_client_message.ServerInfo{
				Id:   servers[i].Id,
				Name: servers[i].Name,
				IP:   servers[i].IP,
			}
		}
	}

	if playerList == nil {
		response.InfoList = []*msg_client_message.AccountPlayerInfo{}
	} else {
		response.InfoList = playerList.GetList()
	}
	response.LastServerId = select_server_id
	if channel == "guest" {
		response.BoundAccount = acc_row.GetBindNewAccount()
	}

	resp_data, err = proto.Marshal(response)
	if err != nil {
		err_code = int32(msg_client_message.E_ERR_INTERNAL)
		log.Error("login_handler marshal response error: %v", err.Error())
		return
	}

	log.Debug("Account[%v] channel[%v] logined", account, channel)

	return
}

func _select_server(unique_id, account string, server_id int32) (err_code int32, hall_ip, access_token string) {
	sinfo := server_list.GetServerById(server_id)
	if sinfo == nil {
		err_code = int32(msg_client_message.E_ERR_PLAYER_SELECT_SERVER_NOT_FOUND)
		log.Error("select_server_handler player[%v] select server[%v] not found")
		return
	}

	hall_agent := hall_agent_manager.GetAgentByID(server_id)
	if nil == hall_agent {
		err_code = int32(msg_client_message.E_ERR_PLAYER_SELECT_SERVER_NOT_FOUND)
		log.Error("login_http_handler account %v get hall_agent failed by server_id %v", account, server_id)
		return
	}

	access_token = share_data.GenerateAccessToken(unique_id)
	hall_agent.Send(uint16(msg_server_message.MSGID_L2H_SYNC_ACCOUNT_TOKEN), &msg_server_message.L2HSyncAccountToken{
		UniqueId: unique_id,
		Account:  account,
		Token:    access_token,
	})

	hall_ip = sinfo.IP

	err_code = 1

	return
}

func select_server_handler(account, token string, server_id int32) (err_code int32, resp_data []byte) {
	row := dbc.Accounts.GetRow(account)
	if row == nil {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_NOT_REGISTERED)
		log.Error("select_server_handler: account[%v] not register", account)
		return
	}

	ban_row := dbc.BanPlayers.GetRow(row.GetUniqueId())
	if ban_row != nil && ban_row.GetStartTime() > 0 {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_BE_BANNED)
		log.Error("Account %v has been banned, cant login", account)
		return
	}

	/*acc := account_info_get(account, false)
	if acc == nil {
		err_code = int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
		log.Error("select_server_handler: player[%v] not found", account)
		return
	}*/

	// 暂时不区分IOS或android
	/*client_os := acc.get_client_os()
	if !server_list.HasId(client_os, server_id) {
		err_code = int32(msg_client_message.E_ERR_PLAYER_SELECT_SERVER_NOT_FOUND)
		log.Error("account %v select server id %v not found in client os %v", account, server_id, client_os)
		return
	}*/

	// 暂时不判断状态
	/*if acc.get_state() != 1 {
		err_code = int32(msg_client_message.E_ERR_PLAYER_ALREADY_SELECTED_SERVER)
		log.Error("select_server_handler player[%v] already selected server", account)
		return
	}*/

	if /*token != acc.get_token()*/ token != row.GetToken() {
		err_code = int32(msg_client_message.E_ERR_PLAYER_TOKEN_ERROR)
		log.Error("select_server_handler player[%v] token[%v] invalid, need[%v]", account, token, row.GetToken())
		return
	}

	err_code, hall_ip, access_token := _select_server(row.GetUniqueId(), account, server_id)
	if err_code < 0 {
		return
	}

	if server.use_https {
		hall_ip = "https://" + hall_ip
	} else {
		hall_ip = "http://" + hall_ip
	}

	response := &msg_client_message.S2CSelectServerResponse{
		Acc:   account,
		Token: access_token,
		IP:    hall_ip,
	}

	var err error
	resp_data, err = proto.Marshal(response)
	if err != nil {
		err_code = int32(msg_client_message.E_ERR_INTERNAL)
		log.Error("select_server_handler marshal response error: %v", err.Error())
		return
	}

	row.SetLastSelectServerId(server_id)

	log.Trace("Account %v selected server %v", account, server_id)

	return
}

func set_password_handler(account, password, new_password string) (err_code int32, resp_data []byte) {
	row := dbc.Accounts.GetRow(account)
	if row == nil {
		err_code = int32(msg_client_message.E_ERR_PLAYER_NOT_EXIST)
		log.Error("set_password_handler account[%v] not found", account)
		return
	}

	ban_row := dbc.BanPlayers.GetRow(row.GetUniqueId())
	if ban_row != nil && ban_row.GetStartTime() > 0 {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_BE_BANNED)
		log.Error("Account %v has been banned, cant login", account)
		return
	}

	if row.GetPassword() != password {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_PASSWORD_INVALID)
		log.Error("set_password_handler account[%v] password is invalid", account)
		return
	}

	row.SetPassword(new_password)

	response := &msg_client_message.S2CSetLoginPasswordResponse{
		Account:     account,
		Password:    password,
		NewPassword: new_password,
	}

	var err error
	resp_data, err = proto.Marshal(response)
	if err != nil {
		err_code = int32(msg_client_message.E_ERR_INTERNAL)
		log.Error("set_password_handler marshal response error: %v", err.Error())
		return
	}

	return
}

func save_aaid_handler(account, aaid string) (err_code int32, resp_data []byte) {
	if account == "" || aaid == "" {
		err_code = int32(msg_client_message.E_ERR_ACCOUNT_AAID_DONT_EMPTY)
		return
	}

	_save_aaid(account, aaid)
	response := &msg_client_message.S2CSaveAAIDResponse{
		Account: account,
		AAID:    aaid,
	}
	var err error
	resp_data, err = proto.Marshal(response)
	if err != nil {
		err_code = int32(msg_client_message.E_ERR_INTERNAL)
		log.Error("save_aaid_handler marshal response error: %v", err.Error())
		return
	}
	return
}

/*
func response_error(err_code int32, w http.ResponseWriter) {
	err_response := JsonResponseData{
		Code: err_code,
	}
	data, err := json.Marshal(err_response)
	if nil != err {
		log.Error("login_http_handler json mashal error")
		return
	}
	w.Write(data)
}
*/

func _send_error(w http.ResponseWriter, msg_id, ret_code int32) {
	m := &msg_client_message.S2C_ONE_MSG{ErrorCode: ret_code}
	res2cli := &msg_client_message.S2C_MSG_DATA{MsgList: []*msg_client_message.S2C_ONE_MSG{m}}
	final_data, err := proto.Marshal(res2cli)
	if nil != err {
		log.Error("client_msg_handler marshal 1 client msg failed err(%s)", err.Error())
		return
	}

	data := final_data
	data = append(data, byte(0))

	iret, err := w.Write(data)
	if nil != err {
		log.Error("client_msg_handler write data 1 failed err[%s] ret %d", err.Error(), iret)
		return
	}
}

func client_http_handler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
			debug.PrintStack()
		}
	}()

	defer r.Body.Close()

	data, err := ioutil.ReadAll(r.Body)
	if nil != err {
		_send_error(w, 0, -1)
		log.Error("client_http_handler ReadAll err[%s]", err.Error())
		return
	}

	var msg msg_client_message.C2S_ONE_MSG
	err = proto.Unmarshal(data, &msg)
	if nil != err {
		_send_error(w, 0, -1)
		log.Error("client_http_handler proto Unmarshal err[%s]", err.Error())
		return
	}

	var err_code, msg_id int32
	if msg.MsgCode == int32(msg_client_message_id.MSGID_C2S_LOGIN_REQUEST) {
		var login_msg msg_client_message.C2SLoginRequest
		err = proto.Unmarshal(msg.GetData(), &login_msg)
		if err != nil {
			_send_error(w, 0, -1)
			log.Error("Msg C2SLoginRequest unmarshal err %v", err.Error())
			return
		}
		if login_msg.GetAcc() == "" {
			_send_error(w, 0, -1)
			log.Error("Acc is empty")
			return
		}
		msg_id = int32(msg_client_message_id.MSGID_S2C_LOGIN_RESPONSE)
		err_code, data = login_handler(login_msg.GetAcc(), login_msg.GetPassword(), login_msg.GetChannel(), login_msg.GetClientOS(), login_msg.GetAAID())
	} else if msg.MsgCode == int32(msg_client_message_id.MSGID_C2S_SELECT_SERVER_REQUEST) {
		var select_msg msg_client_message.C2SSelectServerRequest
		err = proto.Unmarshal(msg.GetData(), &select_msg)
		if err != nil {
			_send_error(w, 0, -1)
			log.Error("Msg C2SSelectServerRequest unmarshal err %v", err.Error())
			return
		}
		msg_id = int32(msg_client_message_id.MSGID_S2C_SELECT_SERVER_RESPONSE)
		err_code, data = select_server_handler(select_msg.GetAcc(), select_msg.GetToken(), select_msg.GetServerId())
	} else if msg.MsgCode == int32(msg_client_message_id.MSGID_C2S_REGISTER_REQUEST) {
		var register_msg msg_client_message.C2SRegisterRequest
		err = proto.Unmarshal(msg.GetData(), &register_msg)
		if err != nil {
			_send_error(w, 0, -1)
			log.Error("Msg C2SRegisterRequest unmarshal err %v", err.Error())
			return
		}
		msg_id = int32(msg_client_message_id.MSGID_S2C_REGISTER_RESPONSE)
		err_code, data = register_handler(register_msg.GetAccount(), register_msg.GetPassword(), register_msg.GetIsGuest())
	} else if msg.MsgCode == int32(msg_client_message_id.MSGID_C2S_SET_LOGIN_PASSWORD_REQUEST) {
		var pass_msg msg_client_message.C2SSetLoginPasswordRequest
		err = proto.Unmarshal(msg.GetData(), &pass_msg)
		if err != nil {
			_send_error(w, 0, -1)
			log.Error("Msg C2SSetLoginPasswordRequest unmarshal err %v", err.Error())
			return
		}
		msg_id = int32(msg_client_message_id.MSGID_S2C_SET_LOGIN_PASSWORD_RESPONSE)
		err_code, data = set_password_handler(pass_msg.GetAccount(), pass_msg.GetPassword(), pass_msg.GetNewPassword())
	} else if msg.MsgCode == int32(msg_client_message_id.MSGID_C2S_GUEST_BIND_NEW_ACCOUNT_REQUEST) {
		var bind_msg msg_client_message.C2SGuestBindNewAccountRequest
		err = proto.Unmarshal(msg.GetData(), &bind_msg)
		if err != nil {
			_send_error(w, 0, -1)
			log.Error("Msg C2SGuestBindNewAccountRequest unmarshal err %v", err.Error())
			return
		}
		msg_id = int32(msg_client_message_id.MSGID_S2C_GUEST_BIND_NEW_ACCOUNT_RESPONSE)
		err_code, data = bind_new_account_handler(bind_msg.GetServerId(), bind_msg.GetAccount(), bind_msg.GetPassword(), bind_msg.GetNewAccount(), bind_msg.GetNewPassword(), bind_msg.GetNewChannel())
	} else if msg.MsgCode == int32(msg_client_message_id.MSGID_C2S_SAVE_AAID_REQUEST) {
		var aaid_msg msg_client_message.C2SSaveAAIDRequest
		err = proto.Unmarshal(msg.GetData(), &aaid_msg)
		if err != nil {
			_send_error(w, 0, -1)
			log.Error("Msg C2SSaveAAIDRequest unmarshal err %v", err.Error())
			return
		}
		msg_id = int32(msg_client_message_id.MSGID_S2C_SAVE_AAID_RESPONSE)
		err_code, data = save_aaid_handler(aaid_msg.GetAccount(), aaid_msg.GetAAID())
	} else {
		if msg.MsgCode > 0 {
			_send_error(w, msg.MsgCode, int32(msg_client_message.E_ERR_PLAYER_MSG_ID_NOT_FOUND))
			log.Error("Unsupported msg %v", msg.MsgCode)
		} else {
			_send_error(w, msg.MsgCode, int32(msg_client_message.E_ERR_PLAYER_MSG_ID_INVALID))
			log.Error("Invalid msg %v", msg.MsgCode)
		}
		return
	}

	var resp_msg msg_client_message.S2C_ONE_MSG
	resp_msg.MsgCode = msg_id
	resp_msg.ErrorCode = err_code
	resp_msg.Data = data
	data, err = proto.Marshal(&resp_msg)
	if nil != err {
		_send_error(w, 0, -1)
		log.Error("client_msg_handler marshal 2 client msg failed err(%s)", err.Error())
		return
	}

	iret, err := w.Write(data)
	if nil != err {
		_send_error(w, 0, -1)
		log.Error("client_msg_handler write data 2 failed err[%s] ret %d", err.Error(), iret)
		return
	}
}
