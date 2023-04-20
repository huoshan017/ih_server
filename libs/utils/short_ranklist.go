package utils

import (
	"ih_server/libs/log"
	"sync"
)

const (
	SHORT_RANK_ITEM_MAX_NUM = 500
)

type ShortRankItem interface {
	Less(item ShortRankItem) bool
	Greater(item ShortRankItem) bool
	GetKey() interface{}
	GetValue() interface{}
	Assign(item ShortRankItem)
	Add(item ShortRankItem)
	New() ShortRankItem
}

type ShortRankList struct {
	items    []ShortRankItem
	max_num  int32
	curr_num int32
	keys_map map[interface{}]int32
	locker   sync.RWMutex
}

func (list *ShortRankList) Init(max_num int32) bool {
	if max_num <= 0 {
		return false
	}

	list.items = make([]ShortRankItem, max_num)
	list.max_num = max_num
	list.keys_map = make(map[interface{}]int32)

	return true
}

func (list *ShortRankList) GetLength() int32 {
	list.locker.RLock()
	defer list.locker.RUnlock()
	return list.curr_num
}

func (list *ShortRankList) Update(item ShortRankItem, add bool) bool {
	list.locker.Lock()
	defer list.locker.Unlock()

	idx, o := list.keys_map[item.GetKey()]
	if !o && list.curr_num >= list.max_num {
		log.Warn("Short Rank List length %v is max, cant insert new item", list.curr_num)
		return false
	}

	if !o {
		new_item := item.New()
		new_item.Assign(item)
		list.items[list.curr_num] = new_item
		list.keys_map[item.GetKey()] = list.curr_num

		i := list.curr_num - 1
		for ; i >= 0; i-- {
			if !item.Greater(list.items[i]) {
				break
			}
		}

		if i+1 != list.curr_num {
			for n := list.curr_num - 1; n >= i+1; n-- {
				list.items[n+1] = list.items[n]
				list.keys_map[list.items[n+1].GetKey()] = n + 1
			}
			list.items[i+1] = new_item
			list.keys_map[item.GetKey()] = i + 1
		}

		list.curr_num += 1
	} else {
		if list.items[idx] == nil {
			log.Error("!!!!!!!!!!!!! !!!!!!!!!!!!! !!!!!!!!!!!! Short Rank List idx[%v] item value is null", idx)
			return false
		}

		if add {
			item.Add(list.items[idx])
		}
		var i, b, e, pos int32
		if item.Greater(list.items[idx]) {
			i = idx - 1
			for ; i >= 0; i-- {
				if !item.Greater(list.items[i]) {
					break
				}
			}
			b = i + 1
			e = idx - 1
			pos = b
		} else if item.Less(list.items[idx]) {
			i = idx + 1
			for ; i < list.curr_num; i++ {
				if item.Greater(list.items[i]) {
					break
				}
			}
			b = idx + 1
			e = i - 1
			pos = e
		} else {
			return false
		}

		var the_item ShortRankItem
		if pos != idx {
			the_item = list.items[idx]
			if pos < idx {
				for i = e; i >= b; i-- {
					list.items[i+1] = list.items[i]
					list.keys_map[list.items[i+1].GetKey()] = i + 1
				}
			} else {
				for i = b; i <= e; i++ {
					list.items[i-1] = list.items[i]
					list.keys_map[list.items[i-1].GetKey()] = i - 1
				}
			}
		}
		if the_item != nil {
			list.items[pos] = the_item
		}
		list.items[pos].Assign(item)
		list.keys_map[list.items[pos].GetKey()] = pos
	}

	return true
}

func (list *ShortRankList) Delete(key interface{}) bool {
	list.locker.Lock()
	defer list.locker.Unlock()

	idx, o := list.keys_map[key]
	if !o {
		return false
	}

	for i := idx; i < list.curr_num-1; i++ {
		list.items[i] = list.items[i+1]
		list.keys_map[list.items[i+1].GetKey()] = i
	}
	list.items[list.curr_num-1] = nil

	delete(list.keys_map, key)

	list.curr_num -= 1

	return true
}

func (list *ShortRankList) Clear() {
	list.locker.Lock()
	defer list.locker.Unlock()

	for i := int32(0); i < list.max_num; i++ {
		list.items[i] = nil
	}

	list.curr_num = 0
	list.keys_map = make(map[interface{}]int32)
}

func (list *ShortRankList) GetRank(key interface{}) (rank int32) {
	list.locker.RLock()
	defer list.locker.RUnlock()

	var o bool
	rank, o = list.keys_map[key]
	if !o {
		return
	}
	rank += 1
	return
}

func (list *ShortRankList) GetByRank(rank int32) (key interface{}, value interface{}) {
	list.locker.RLock()
	defer list.locker.RUnlock()

	if list.curr_num < rank {
		return
	}
	item := list.items[rank-1]
	if item == nil {
		return
	}
	key = item.GetKey()
	value = item.GetValue()
	return
}

func (list *ShortRankList) GetIndex(rank int32) int32 {
	list.locker.RLock()
	defer list.locker.RUnlock()

	if list.curr_num < rank {
		return -1
	}
	item := list.items[rank-1]
	if item == nil {
		return -1
	}
	return list.keys_map[item.GetKey()]
}

type TestShortRankItem struct {
	Id    int32
	Value int32
}

func (list *TestShortRankItem) Less(item ShortRankItem) bool {
	it := item.(*TestShortRankItem)
	if it == nil {
		return false
	}
	if list.Value < it.Value {
		return true
	}
	return false
}

func (list *TestShortRankItem) Greater(item ShortRankItem) bool {
	it := item.(*TestShortRankItem)
	if it == nil {
		return false
	}
	if list.Value > it.Value {
		return true
	}
	return false
}

func (list *TestShortRankItem) GetKey() interface{} {
	return list.Id
}

func (list *TestShortRankItem) GetValue() interface{} {
	return list.Value
}

func (list *TestShortRankItem) Assign(item ShortRankItem) {
	it := item.(*TestShortRankItem)
	if it == nil {
		return
	}
	list.Id = it.Id
	list.Value = it.Value
}

func (list *TestShortRankItem) Add(item ShortRankItem) {
	it := item.(*TestShortRankItem)
	if it == nil {
		return
	}
	if list.Id == it.Id {
		list.Value += it.Value
	}
}

func (list *TestShortRankItem) New() ShortRankItem {
	return &TestShortRankItem{}
}
