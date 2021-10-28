package main

import (
	"ih_server/libs/log"
	"ih_server/src/share_data"
	"sync/atomic"
)

func (d *DBC) on_preload() (err error) {
	var p *Player
	for _, db := range d.Players.m_rows {
		if nil == db {
			log.Error("DBC on_preload Players have nil db !")
			continue
		}

		p = new_player_with_db(db.m_PlayerId, db)
		if nil == p {
			continue
		}

		player_mgr.Add2IdMap(p)
		player_mgr.Add2UidMap(p.UniqueId, p)

		friend_recommend_mgr.CheckAndAddPlayer(p.Id)

		if p.db.GetLevel() == 0 {
			p.db.SetLevel(p.db.Info.GetLvl())
		}

		defense_power := p.get_defense_team_power()
		if defense_power > 0 {
			top_power_match_manager.Update(p.Id, defense_power)
		}
	}

	return
}

func (d *dbGlobalRow) GetNextPlayerId() int32 {
	curr_id := atomic.AddInt32(&d.m_CurrentPlayerId, 1)
	new_id := share_data.GeneratePlayerId(config.ServerId, curr_id) //((config.ServerId << 20) & 0x7ff00000) | curr_id
	d.m_lock.UnSafeLock("dbGlobalRow.GetNextPlayerId")
	d.m_CurrentPlayerId_changed = true
	d.m_lock.UnSafeUnlock()
	return new_id
}

func (d *dbGlobalRow) GetNextGuildId() int32 {
	curr_id := atomic.AddInt32(&d.m_CurrentGuildId, 1)
	new_id := share_data.GenerateGuildId(config.ServerId, curr_id) //((config.ServerId << 20) & 0x7ff00000) | curr_id
	d.m_lock.UnSafeLock("dbGlobalRow.GetNextGuildId")
	d.m_CurrentGuildId_changed = true
	d.m_lock.UnSafeUnlock()
	return new_id
}
