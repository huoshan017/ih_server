package main

import (
	"ih_server/libs/log"
	"ih_server/libs/utils"
	"ih_server/proto/gen_go/client_message"
	"ih_server/proto/gen_go/client_message_id"
	"sync"

	"github.com/golang/protobuf/proto"
)

const (
	RANK_LIST_TYPE_NONE       = iota
	RANK_LIST_TYPE_ARENA      = 1 // 竞技场积分
	RANK_LIST_TYPE_CAMPAIGN   = 2 // 战役通关
	RANK_LIST_TYPE_ROLE_POWER = 3 // 角色战力
	RANK_LIST_TYPE_TOWER      = 4 // 爬塔
	RANK_LIST_TYPE_MAX        = 10
)

type RankList struct {
	rank_list *utils.CommonRankingList
	item_pool *sync.Pool
	root_node utils.SkiplistNode
}

func (r *RankList) Init(root_node utils.SkiplistNode) {
	r.root_node = root_node
	r.rank_list = utils.NewCommonRankingList(r.root_node, ARENA_RANK_MAX)
	r.item_pool = &sync.Pool{
		New: func() interface{} {
			return r.root_node.New()
		},
	}
}

func (r *RankList) GetItemByKey(key interface{}) (item utils.SkiplistNode) {
	return r.rank_list.GetByKey(key)
}

func (r *RankList) GetRankByKey(key interface{}) int32 {
	return r.rank_list.GetRank(key)
}

func (r *RankList) GetItemByRank(rank int32) (item utils.SkiplistNode) {
	return r.rank_list.GetByRank(rank)
}

func (r *RankList) SetValueByKey(key interface{}, value interface{}) {
	r.rank_list.SetValueByKey(key, value)
}

func (r *RankList) RankNum() int32 {
	return r.rank_list.GetLength()
}

// 获取排名项
func (r *RankList) GetItemsByRange(key interface{}, start_rank, rank_num int32) (rank_items []utils.SkiplistNode, self_rank int32, self_value interface{}) {
	start_rank, rank_num = r.rank_list.GetRankRange(start_rank, rank_num)
	if start_rank == 0 {
		log.Error("Get rank list range with [%v,%v] failed", start_rank, rank_num)
		return nil, 0, nil
	}

	nodes := make([]interface{}, rank_num)
	for i := int32(0); i < rank_num; i++ {
		nodes[i] = r.item_pool.Get().(utils.SkiplistNode)
	}

	num := r.rank_list.GetRangeNodes(start_rank, rank_num, nodes)
	if num == 0 {
		log.Error("Get rank list nodes failed")
		return nil, 0, nil
	}

	rank_items = make([]utils.SkiplistNode, num)
	for i := int32(0); i < num; i++ {
		rank_items[i] = nodes[i].(utils.SkiplistNode)
	}

	self_rank, self_value = r.rank_list.GetRankAndValue(key)
	return

}

// 获取最后的几个排名
func (r *RankList) GetLastRankRange(rank_num int32) (int32, int32) {
	return r.rank_list.GetLastRankRange(rank_num)
}

// 更新排行榜
func (r *RankList) UpdateItem(item utils.SkiplistNode) bool {
	if !r.rank_list.Update(item) {
		log.Error("Update rank item[%v] failed", item)
		return false
	}
	return true
}

// 删除指定值
func (r *RankList) DeleteItem(key interface{}) bool {
	return r.rank_list.Delete(key)
}

var root_rank_item = []utils.SkiplistNode{
	nil,                    // 0
	&ArenaRankItem{},       // 1
	&PlayerInt32RankItem{}, // 2
	&PlayerInt32RankItem{}, // 3
	&PlayerInt32RankItem{}, // 4
}

type RankListManager struct {
	rank_lists []*RankList
	rank_map   map[int32]*RankList
	locker     *sync.RWMutex
}

var rank_list_mgr RankListManager

func (r *RankListManager) Init() {
	r.rank_lists = make([]*RankList, RANK_LIST_TYPE_MAX)
	for i := int32(1); i < RANK_LIST_TYPE_MAX; i++ {
		if int(i) >= len(root_rank_item) {
			break
		}
		r.rank_lists[i] = &RankList{}
		r.rank_lists[i].Init(root_rank_item[i])
	}
	r.rank_map = make(map[int32]*RankList)
	r.locker = &sync.RWMutex{}
}

func (r *RankListManager) GetRankList(rank_type int32) (rank_list *RankList) {
	if int(rank_type) >= len(r.rank_lists) {
		return nil
	}
	if r.rank_lists[rank_type] == nil {
		return nil
	}
	return r.rank_lists[rank_type]
}

func (r *RankListManager) GetItemByKey(rank_type int32, key interface{}) (item utils.SkiplistNode) {
	if int(rank_type) >= len(r.rank_lists) {
		return nil
	}
	if r.rank_lists[rank_type] == nil {
		return nil
	}
	return r.rank_lists[rank_type].GetItemByKey(key)
}

func (r *RankListManager) GetRankByKey(rank_type int32, key interface{}) int32 {
	if int(rank_type) >= len(r.rank_lists) {
		return -1
	}
	if r.rank_lists[rank_type] == nil {
		return -1
	}
	return r.rank_lists[rank_type].GetRankByKey(key)
}

func (r *RankListManager) GetItemByRank(rank_type, rank int32) (item utils.SkiplistNode) {
	if int(rank_type) >= len(r.rank_lists) {
		return nil
	}
	if r.rank_lists[rank_type] == nil {
		return nil
	}
	return r.rank_lists[rank_type].GetItemByRank(rank)
}

func (r *RankListManager) GetItemsByRange(rank_type int32, key interface{}, start_rank, rank_num int32) (rank_items []utils.SkiplistNode, self_rank int32, self_value interface{}) {
	if int(rank_type) >= len(r.rank_lists) {
		return nil, 0, nil
	}
	if r.rank_lists[rank_type] == nil {
		return nil, 0, nil
	}
	return r.rank_lists[rank_type].GetItemsByRange(key, start_rank, rank_num)
}

func (r *RankListManager) GetLastRankRange(rank_type, rank_num int32) (int32, int32) {
	if int(rank_type) >= len(r.rank_lists) {
		return -1, -1
	}
	if r.rank_lists[rank_type] == nil {
		return -1, -1
	}
	return r.rank_lists[rank_type].GetLastRankRange(rank_num)
}

func (r *RankListManager) UpdateItem(rank_type int32, item utils.SkiplistNode) bool {
	if int(rank_type) >= len(r.rank_lists) {
		return false
	}
	if r.rank_lists[rank_type] == nil {
		return false
	}
	return r.rank_lists[rank_type].UpdateItem(item)
}

func (r *RankListManager) DeleteItem(rank_type int32, key interface{}) bool {
	if int(rank_type) >= len(r.rank_lists) {
		return false
	}
	if r.rank_lists[rank_type] == nil {
		return false
	}
	return r.rank_lists[rank_type].DeleteItem(key)
}

func (r *RankListManager) GetRankList2(rank_type int32) (rank_list *RankList) {
	r.locker.RLock()
	rank_list = r.rank_map[rank_type]
	if rank_list == nil {
		rank_list = &RankList{}
		r.rank_map[rank_type] = rank_list
	}
	r.locker.RUnlock()
	return
}

func (r *RankListManager) GetItemByKey2(rank_type int32, key interface{}) (item utils.SkiplistNode) {
	rank_list := r.GetRankList2(rank_type)
	if rank_list == nil {
		return
	}
	return rank_list.GetItemByKey(key)
}

func (r *RankListManager) GetRankByKey2(rank_type int32, key interface{}) int32 {
	rank_list := r.GetRankList2(rank_type)
	if rank_list == nil {
		return 0
	}
	return rank_list.GetRankByKey(key)
}

func (r *RankListManager) GetItemByRank2(rank_type, rank int32) (item utils.SkiplistNode) {
	rank_list := r.GetRankList2(rank_type)
	if rank_list == nil {
		return
	}
	return rank_list.GetItemByRank(rank)
}

func (r *RankListManager) GetItemsByRange2(rank_type int32, key interface{}, start_rank, rank_num int32) (rank_items []utils.SkiplistNode, self_rank int32, self_value interface{}) {
	rank_list := r.GetRankList2(rank_type)
	if rank_list == nil {
		return
	}
	return rank_list.GetItemsByRange(key, start_rank, rank_num)
}

func (r *RankListManager) GetLastRankRange2(rank_type, rank_num int32) (int32, int32) {
	rank_list := r.GetRankList2(rank_type)
	if rank_list == nil {
		return -1, -1
	}
	return rank_list.GetLastRankRange(rank_num)
}

func (r *RankListManager) UpdateItem2(rank_type int32, item utils.SkiplistNode) bool {
	rank_list := r.GetRankList2(rank_type)
	if rank_list == nil {
		return false
	}
	return rank_list.UpdateItem(item)
}

func (r *RankListManager) DeleteItem2(rank_type int32, key interface{}) bool {
	rank_list := r.GetRankList2(rank_type)
	if rank_list == nil {
		return false
	}
	return rank_list.DeleteItem(key)
}

func transfer_nodes_to_rank_items(rank_type int32, start_rank int32, items []utils.SkiplistNode) (rank_items []*msg_client_message.RankItemInfo) {
	var item *PlayerInt32RankItem
	var arena_item *ArenaRankItem
	for i := int32(0); i < int32(len(items)); i++ {
		if rank_type == RANK_LIST_TYPE_ARENA {
			arena_item = (items[i]).(*ArenaRankItem)
			if arena_item == nil {
				continue
			}
		} else {
			item = (items[i]).(*PlayerInt32RankItem)
			if item == nil {
				continue
			}
		}
		var rank_item *msg_client_message.RankItemInfo
		if rank_type == RANK_LIST_TYPE_ARENA {
			name, level, head, score, grade, power := GetFighterInfo(arena_item.PlayerId)
			rank_item = &msg_client_message.RankItemInfo{
				Rank:             start_rank + i,
				PlayerId:         arena_item.PlayerId,
				PlayerName:       name,
				PlayerLevel:      level,
				PlayerHead:       head,
				PlayerArenaScore: score,
				PlayerArenaGrade: grade,
				PlayerPower:      power,
			}
		} else if rank_type == RANK_LIST_TYPE_CAMPAIGN {
			name, level, head, campaign_id := GetPlayerCampaignInfo(item.PlayerId)
			rank_item = &msg_client_message.RankItemInfo{
				Rank:                   start_rank + i,
				PlayerId:               item.PlayerId,
				PlayerName:             name,
				PlayerLevel:            level,
				PlayerHead:             head,
				PlayerPassedCampaignId: campaign_id,
			}
		} else if rank_type == RANK_LIST_TYPE_ROLE_POWER {
			name, level, head := GetPlayerBaseInfo(item.PlayerId)
			rank_item = &msg_client_message.RankItemInfo{
				Rank:             start_rank + i,
				PlayerId:         item.PlayerId,
				PlayerName:       name,
				PlayerLevel:      level,
				PlayerHead:       head,
				PlayerRolesPower: item.Value,
			}
		} else {
			log.Error("invalid rank type[%v] transfer nodes to rank items", rank_type)
			return
		}
		rank_items = append(rank_items, rank_item)
	}
	return
}

func (r *Player) get_rank_list_items(rank_type, start_rank, num int32) int32 {
	items, self_rank, value := rank_list_mgr.GetItemsByRange(rank_type, r.Id, start_rank, num)
	if items == nil {
		return int32(msg_client_message.E_ERR_RANK_LIST_TYPE_INVALID)
	}

	var self_value, self_value2, self_top_rank int32
	if value != nil {
		self_value = value.(int32)
	}
	if rank_type == RANK_LIST_TYPE_ARENA {
		self_top_rank = r.db.Arena.GetHistoryTopRank()
		self_value = r.db.Arena.GetScore()
	} else if rank_type == RANK_LIST_TYPE_CAMPAIGN {
		self_value = r.db.CampaignCommon.GetLastestPassedCampaignId()
	//} else if rank_type == RANK_LIST_TYPE_ROLE_POWER {
	}
	rank_items := transfer_nodes_to_rank_items(rank_type, start_rank, items)
	response := &msg_client_message.S2CRankListResponse{
		RankListType:       rank_type,
		RankItems:          rank_items,
		SelfRank:           self_rank,
		SelfHistoryTopRank: self_top_rank,
		SelfValue:          self_value,
		SelfValue2:         self_value2,
	}
	r.Send(uint16(msg_client_message_id.MSGID_S2C_RANK_LIST_RESPONSE), response)
	log.Debug("Player[%v] get rank type[%v] list response", r.Id, rank_type)
	return 1
}

func C2SRankListHandler(p *Player, msg_data []byte) int32 {
	var req msg_client_message.C2SRankListRequest
	err := proto.Unmarshal(msg_data, &req)
	if nil != err {
		log.Error("Unmarshal msg failed err(%s)!", err.Error())
		return -1
	}
	return p.get_rank_list_items(req.GetRankListType(), 1, global_config.ArenaGetTopRankNum)
}
