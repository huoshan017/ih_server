package main

import (
	"ih_server/libs/log"
	"sync"
	"time"
)

type ConnTimer struct {
	player_id int32
	next      *ConnTimer
	prev      *ConnTimer
}

type ConnTimerList struct {
	head *ConnTimer
	tail *ConnTimer
}

func (c *ConnTimerList) add(player_id int32) *ConnTimer {
	node := &ConnTimer{
		player_id: player_id,
	}
	if c.head == nil {
		c.head = node
		c.tail = node
	} else {
		node.prev = c.tail
		c.tail.next = node
		c.tail = node
	}
	return node
}

func (c *ConnTimerList) remove(timer *ConnTimer) {
	if timer.prev != nil {
		timer.prev.next = timer.next
	}
	if timer.next != nil {
		timer.next.prev = timer.prev
	}
	if timer == c.head {
		c.head = timer.next
	}
	if timer == c.tail {
		c.tail = timer.prev
	}
}

type ConnTimerPlayer struct {
	player_id  int32
	timer_list *ConnTimerList
	timer      *ConnTimer
}

type ConnTimerMgr struct {
	timer_lists      []*ConnTimerList
	curr_timer_index int32
	last_check_time  int32
	players          map[int32]*ConnTimerPlayer
	locker           *sync.Mutex
}

func (c *ConnTimerMgr) Init() {
	c.timer_lists = make([]*ConnTimerList, global_config.HeartbeatInterval*3)
	c.curr_timer_index = -1
	c.players = make(map[int32]*ConnTimerPlayer)
	c.locker = &sync.Mutex{}
}

func (c *ConnTimerMgr) Insert(player_id int32) bool {
	c.locker.Lock()
	defer c.locker.Unlock()

	p := c.players[player_id]
	if p != nil {
		c.remove(p)
	}
	lists_len := int32(len(c.timer_lists))
	insert_list_index := (c.curr_timer_index + global_config.HeartbeatInterval) % lists_len
	list := c.timer_lists[insert_list_index]
	if list == nil {
		list = &ConnTimerList{}
		c.timer_lists[insert_list_index] = list
	}
	timer := list.add(player_id)
	if p == nil {
		c.players[player_id] = &ConnTimerPlayer{
			player_id:  player_id,
			timer_list: list,
			timer:      timer,
		}
	} else {
		p.player_id = player_id
		p.timer_list = list
		p.timer = timer
		c.players[player_id] = p
	}
	log.Debug("Player[%v] conn insert in index[%v] list", player_id, insert_list_index)
	return true
}

func (c *ConnTimerMgr) remove(p *ConnTimerPlayer) bool {
	p.timer_list.remove(p.timer)
	delete(c.players, p.player_id)
	return true
}

func (c *ConnTimerMgr) Remove(player_id int32) bool {
	c.locker.Lock()
	defer c.locker.Unlock()
	p := c.players[player_id]
	if p == nil {
		return false
	}
	return c.remove(p)
}

func (c *ConnTimerMgr) Run() {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	for {
		c.locker.Lock()

		now_time := int32(time.Now().Unix())
		if c.last_check_time == 0 {
			c.last_check_time = now_time
		}

		var players []*Player
		lists_len := int32(len(c.timer_lists))
		diff_secs := now_time - c.last_check_time
		if diff_secs > 0 {
			var idx int32
			if diff_secs >= lists_len {
				if c.curr_timer_index > 0 {
					idx = c.curr_timer_index - 1
				} else {
					idx = lists_len - 1
				}
			} else {
				idx = (c.curr_timer_index + diff_secs) % lists_len
			}

			i := (c.curr_timer_index + 1) % lists_len
			for {
				list := c.timer_lists[i]
				if list != nil {
					t := list.head
					for t != nil {
						p := player_mgr.GetPlayerById(t.player_id)
						if p != nil {
							players = append(players, p)
							log.Debug("############### to offline player[%v]", t.player_id)
						}
						t = t.next
					}
					c.timer_lists[i] = nil
				}
				if i == idx {
					break
				}
				i = (i + 1) % lists_len
			}
			c.curr_timer_index = idx
			c.last_check_time = now_time
		}

		c.locker.Unlock()

		if players != nil {
			for i := 0; i < len(players); i++ {
				if players[i] != nil {
					players[i].OnLogout(true)
				}
			}
		}

		time.Sleep(time.Second * 1)
	}
}

var conn_timer_mgr ConnTimerMgr
