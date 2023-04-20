package main

import (
	"ih_server/src/login_db"
	"sync"
)

type AccountInfo struct {
	acc_row *login_db.Account
	state   int32 // 0 未登录   1 已登陆   2 已进入游戏
	//client_os string
	locker sync.RWMutex
}

func newAccountInfo() *AccountInfo {
	return &AccountInfo{}
}

func (ai *AccountInfo) set_state(state int32) {
	ai.locker.Lock()
	defer ai.locker.Unlock()
	ai.state = state
}

/*var account_mgr map[string]*AccountInfo
var account_locker *sync.RWMutex

func account_mgr_init() {
	account_mgr = make(map[string]*AccountInfo)
	account_locker = &sync.RWMutex{}
}

func account_info_get(account string, first_create bool) *AccountInfo {
	account_locker.RLock()
	account_info := account_mgr[account]
	account_locker.RUnlock()

	if first_create && account_info == nil {
		account_locker.Lock()
		account_info = account_mgr[account]
		// double check
		if account_info == nil {
			account_info = &AccountInfo{
				account: account,
			}
			account_mgr[account] = account_info
		}
		account_locker.Unlock()
	}

	return account_info
}

func account_login(acc, token, client_os string) {
	account_info := account_info_get(acc, true)
	if account_info == nil {
		return
	}
	account_info.set_state(1)
	account_info.set_token(token)
	account_info.set_client_os(client_os)
}

func account_logout(acc string) {
	account_info := account_info_get(acc, false)
	if account_info == nil {
		return
	}
	account_info.set_state(0)
}*/
