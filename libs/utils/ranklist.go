package utils

import (
	"ih_server/libs/log"
	"sync"
)

// -----------------------------------------------------------------------------
/* 通用排行榜 */
// -----------------------------------------------------------------------------
type CommonRankingList struct {
	ranking_items *Skiplist
	key2item      map[interface{}]SkiplistNode
	root_node     SkiplistNode
	max_rank      int32
	items_pool    *sync.Pool
	locker        *sync.RWMutex
}

func NewCommonRankingList(root_node SkiplistNode, max_rank int32) *CommonRankingList {
	ranking_list := &CommonRankingList{
		ranking_items: NewSkiplist(),
		key2item:      make(map[interface{}]SkiplistNode),
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

func (list *CommonRankingList) GetByRank(rank int32) SkiplistNode {
	list.locker.RLock()
	defer list.locker.RUnlock()

	item := list.ranking_items.GetByRank(rank)
	if item == nil {
		return nil
	}
	new_item := list.items_pool.Get().(SkiplistNode)
	new_item.Assign(item)
	return new_item
}

func (list *CommonRankingList) GetByKey(key interface{}) SkiplistNode {
	list.locker.RLock()
	defer list.locker.RUnlock()

	item, o := list.key2item[key]
	if !o || item == nil {
		return nil
	}
	new_item := list.items_pool.Get().(SkiplistNode)
	new_item.Assign(item)
	return new_item
}

func (list *CommonRankingList) SetValueByKey(key interface{}, value interface{}) {
	list.locker.RLock()
	defer list.locker.RUnlock()

	item, o := list.key2item[key]
	if !o || item == nil {
		return
	}
	item.SetValue(value)
}

func (list *CommonRankingList) insert(key interface{}, item SkiplistNode, is_lock bool) bool {
	if is_lock {
		list.locker.Lock()
		defer list.locker.Unlock()
	}
	list.ranking_items.Insert(item)
	list.key2item[key] = item
	return true
}

func (list *CommonRankingList) Insert(key interface{}, item SkiplistNode) bool {
	return list.insert(key, item, true)
}

func (list *CommonRankingList) delete(key interface{}, is_lock bool) bool {
	if is_lock {
		list.locker.Lock()
		defer list.locker.Unlock()
	}

	item, o := list.key2item[key]
	if !o {
		log.Error("CommonRankingList key[%v] not found", key)
		return false
	}
	if !list.ranking_items.Delete(item) {
		log.Error("CommonRankingList delete key[%v] value[%v] in ranking list failed", key, item.GetValue())
		return false
	}
	if is_lock {
		list.items_pool.Put(item)
	}
	//delete(list.key2item, key)
	return true
}

func (list *CommonRankingList) Delete(key interface{}) bool {
	return list.delete(key, true)
}

func (list *CommonRankingList) Update(item SkiplistNode) bool {
	list.locker.Lock()
	defer list.locker.Unlock()

	old_item, o := list.key2item[item.GetKey()]
	if o {
		if !list.delete(item.GetKey(), false) {
			log.Error("Update key[%v] for Ranking List failed", item)
			return false
		}
		old_item.Assign(item)
		return list.insert(item.GetKey(), old_item, false)
	} else {
		new_item := list.items_pool.Get().(SkiplistNode)
		new_item.Assign(item)
		return list.insert(item.GetKey(), new_item, false)
	}
}

func (list *CommonRankingList) GetRangeNodes(rank_start, rank_num int32, nodes []interface{}) (num int32) {
	if rank_start <= int32(0) || rank_start > list.max_rank {
		log.Warn("Ranking list rank_start[%v] invalid", rank_start)
		return
	}

	list.locker.RLock()
	defer list.locker.RUnlock()

	if int(rank_start) > len(list.key2item) {
		log.Warn("Ranking List rank range[1,%v], rank_start[%v] over rank list", len(list.key2item), rank_start)
		return
	}

	real_num := int32(len(list.key2item)) - rank_start + 1
	if real_num < rank_num {
		rank_num = real_num
	}

	items := make([]SkiplistNode, rank_num)
	b := list.ranking_items.GetByRankRange(rank_start, rank_num, items)
	if !b {
		log.Warn("Ranking List rank range[%v,%v] is empty", rank_start, rank_num)
		return
	}

	for i := int32(0); i < rank_num; i++ {
		item := items[i]
		if item == nil {
			log.Error("Get Rank[%v] for Ranking List failed")
			continue
		}
		node := nodes[i]
		item.CopyDataTo(node)
		num += 1
	}
	return
}

func (list *CommonRankingList) GetRank(key interface{}) int32 {
	list.locker.RLock()
	defer list.locker.RUnlock()

	item, o := list.key2item[key]
	if !o {
		return 0
	}
	return list.ranking_items.GetRank(item)
}

func (list *CommonRankingList) GetRankAndValue(key interface{}) (rank int32, value interface{}) {
	list.locker.RLock()
	defer list.locker.RUnlock()

	item, o := list.key2item[key]
	if !o {
		return 0, nil
	}

	return list.ranking_items.GetRank(item), item.GetValue()
}

func (list *CommonRankingList) GetRankRange(start, num int32) (int32, int32) {
	list.locker.RLock()
	defer list.locker.RUnlock()

	l := int32(len(list.key2item))
	if list.key2item == nil || l == 0 {
		return 0, 0
	}

	if start > l {
		return 0, 0
	}

	if l-start+1 < num {
		num = l - start + 1
	}
	return start, num
}

func (list *CommonRankingList) GetLastRankRange(num int32) (int32, int32) {
	list.locker.RLock()
	defer list.locker.RUnlock()

	l := int32(len(list.key2item))
	if list.key2item == nil || l == 0 {
		return 0, 0
	}

	if num > l {
		num = l
	}

	return l - num + 1, num
}

func (list *CommonRankingList) GetLastRank() int32 {
	list.locker.RLock()
	defer list.locker.RUnlock()

	return int32(len(list.key2item))
}

func (list *CommonRankingList) GetLength() int32 {
	list.locker.RLock()
	defer list.locker.RUnlock()
	return list.ranking_items.GetLength()
}
