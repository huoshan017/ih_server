package main

import (
	"fmt"
	"ih_server/libs/log"
	"ih_server/libs/utils"
	"math"
	"math/rand"
	"sync"
	"time"
)

type TopPowerRankItem struct {
	TopPower int32
}

func (rankItem *TopPowerRankItem) Less(value interface{}) bool {
	item := value.(*TopPowerRankItem)
	if item == nil {
		return false
	}
	if rankItem.TopPower < item.TopPower {
		return true
	}
	return false
}

func (rankItem *TopPowerRankItem) Greater(value interface{}) bool {
	item := value.(*TopPowerRankItem)
	if item == nil {
		return false
	}
	if rankItem.TopPower > item.TopPower {
		return true
	}
	return false
}

func (rankItem *TopPowerRankItem) KeyEqual(value interface{}) bool {
	item := value.(*TopPowerRankItem)
	return rankItem.TopPower == item.TopPower
}

func (rankItem *TopPowerRankItem) GetKey() interface{} {
	return rankItem.TopPower
}

func (rankItem *TopPowerRankItem) GetValue() interface{} {
	return rankItem.TopPower
}

func (rankItem *TopPowerRankItem) SetValue(value interface{}) {
}

func (rankItem *TopPowerRankItem) New() utils.SkiplistNode {
	return &TopPowerRankItem{}
}

func (rankItem *TopPowerRankItem) Assign(node utils.SkiplistNode) {
	n := node.(*TopPowerRankItem)
	if n == nil {
		return
	}
	rankItem.TopPower = n.TopPower
}

func (rankItem *TopPowerRankItem) CopyDataTo(node interface{}) {
	n := node.(*TopPowerRankItem)
	if n == nil {
		return
	}
	n.TopPower = rankItem.TopPower
}

type TopPowerPlayers struct {
	player2index map[int32]int32
	players      []int32
	locker       sync.RWMutex
}

func (players *TopPowerPlayers) Init() {
	players.player2index = make(map[int32]int32)
}

func (players *TopPowerPlayers) Insert(player_id int32) bool {
	players.locker.Lock()
	defer players.locker.Unlock()
	_, o := players.player2index[player_id]
	if o {
		return false
	}
	var idx int = len(players.player2index)
	if players.players != nil && len(players.players) > idx {
		players.players[idx] = player_id
	} else {
		players.players = append(players.players, player_id)
	}
	players.player2index[player_id] = int32(idx)
	return true
}

func (players *TopPowerPlayers) Delete(player_id int32) bool {
	players.locker.Lock()
	defer players.locker.Unlock()

	idx, o := players.player2index[player_id]
	if !o {
		return false
	}

	if int(idx) >= len(players.players) {
		return false
	}

	if players.players[idx] != player_id {
		log.Error("Idx player id is not %v, else is %v", player_id, players.players[idx])
		return false
	}

	players.players[idx] = player_id
	delete(players.player2index, player_id)

	return true
}

func (players *TopPowerPlayers) IsEmpty() bool {
	players.locker.RLock()
	defer players.locker.RUnlock()
	return len(players.player2index) > 0
}

func (players *TopPowerPlayers) Random() int32 {
	players.locker.RLock()
	defer players.locker.RUnlock()

	l := len(players.player2index)
	if l == 0 {
		return -1
	}
	rand.Seed(time.Now().Unix())
	r := rand.Int31n(int32(l))
	return players.players[r]
}

type TopPowerMatchManager struct {
	rank_powers   *utils.Skiplist            // 防守战力排序
	player2power  map[int32]int32            // 保存玩家当前防守战力
	power2players map[int32]*TopPowerPlayers // 防守战力到玩家的映射
	root_node     *TopPowerRankItem
	max_rank      int32
	items_pool    *sync.Pool
	locker        *sync.RWMutex
}

var top_power_match_manager *TopPowerMatchManager

func NewTopPowerMatchManager(root_node *TopPowerRankItem, max_rank int32) *TopPowerMatchManager {
	ranking_list := &TopPowerMatchManager{
		rank_powers:   utils.NewSkiplist(),
		player2power:  make(map[int32]int32),
		power2players: make(map[int32]*TopPowerPlayers),
		root_node:     root_node,
		max_rank:      max_rank,
		items_pool: &sync.Pool{
			New: func() interface{} {
				return root_node.New()
			},
		},
		locker: &sync.RWMutex{},
	}

	return ranking_list
}

func (m *TopPowerMatchManager) Update(player_id, power int32) bool {
	if power <= 0 || player_id == 0 {
		return false
	}

	m.locker.Lock()
	defer m.locker.Unlock()

	old_power, o := m.player2power[player_id]
	if o {
		if power == old_power {
			return false
		}

		ps, oo := m.power2players[old_power]
		if !oo {
			log.Error("No old power %v players", old_power)
			return false
		}

		if !ps.Delete(player_id) {
			log.Error("Update old_power[%v] to power[%v] for player %v failed", old_power, power, player_id)
			return false
		}

		if ps.IsEmpty() {
			delete(m.power2players, old_power)
			var dt = TopPowerRankItem{
				TopPower: old_power,
			}
			if !m.rank_powers.Delete(&dt) {
				log.Warn("Delete empty power %v players to top power rank list failed", old_power)
			}
		}
	}

	var players *TopPowerPlayers
	players, o = m.power2players[power]
	if !o {
		players = &TopPowerPlayers{}
		players.Init()
		m.power2players[power] = players
	}
	players.Insert(player_id)

	m.player2power[player_id] = power

	t := m.items_pool.Get().(*TopPowerRankItem)
	t.TopPower = power
	if m.rank_powers.GetNode(t) == nil {
		m.rank_powers.Insert(t)
	}

	return true
}

func (m *TopPowerMatchManager) CheckDefensePowerUpdate(p *Player) bool {
	m.locker.RLock()
	power := m.player2power[p.Id]
	m.locker.RUnlock()

	now_power := p.get_defense_team_power()
	if power != now_power {
		m.Update(p.Id, now_power)
		return true
	}

	return false
}

func (m *TopPowerMatchManager) _get_power_by_rank(rank int32) int32 {
	item := m.rank_powers.GetByRank(rank)
	it := item.(*TopPowerRankItem)
	if it == nil {
		log.Error("@@@@@@@@@@@ Expedition power rank %v item type invalid", rank)
		return -1
	}
	return it.TopPower
}

func (m *TopPowerMatchManager) GetNearestRandPlayer(power int32) int32 {
	m.locker.RLock()
	defer m.locker.RUnlock()

	l := m.rank_powers.GetLength()
	if l < 1 {
		return -1
	}

	players, o := m.power2players[power]
	if !o {
		left := int32(1)
		right := int32(l)
		var r, new_power int32
		new_power = power
		for {
			mid := (left + right) / 2
			mid_power := m._get_power_by_rank(mid)
			if mid_power < 0 {
				return -1
			}

			// 比较相邻的另一个战力差距，取差值小的那个
			if r == mid {
				if new_power > power {
					if r+2 <= l {
						next_power := m._get_power_by_rank(r + 2)
						if next_power > 0 {
							if math.Abs(float64(new_power-power)) > math.Abs(float64(power-next_power)) {
								new_power = next_power
							}
						}
					}
				} else if new_power < power {
					if r-2 >= 1 {
						next_power := m._get_power_by_rank(r - 2)
						if next_power > 0 {
							if math.Abs(float64(new_power-power)) > math.Abs(float64(power-next_power)) {
								new_power = next_power
							}
						}
					}
				}
				break
			}

			r = mid
			new_power = mid_power
			if new_power < power {
				right = r
			} else if new_power > power {
				left = r
			} else {
				break
			}
		}
		players = m.power2players[new_power]
		if players == nil {
			log.Error("@@@@ New power %v have no players", new_power)
			return 0
		}
	}

	return players.Random()
}

func (m *TopPowerMatchManager) OutputList() {
	m.locker.RLock()
	defer m.locker.RUnlock()

	l := m.rank_powers.GetLength()
	if l > 0 {
		var s string
		for r := int32(1); r < l; r++ {
			v := m.rank_powers.GetByRank(r)
			vv := v.(*TopPowerRankItem)
			if vv != nil {
				if r > 1 {
					s = fmt.Sprintf("%v,%v", s, vv.TopPower)
				} else {
					s = fmt.Sprintf("%v", vv.TopPower)
				}
			}
		}
		log.Trace("@@@@@ TopPowerRanklist %v", s)
	}
}
