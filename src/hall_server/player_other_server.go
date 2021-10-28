package main

type OtherServerPlayerMgr struct {
	players *dbOtherServerPlayerTable
}

var os_player_mgr OtherServerPlayerMgr

func (m *OtherServerPlayerMgr) Init() {
	m.players = dbc.OtherServerPlayers
}

func (m *OtherServerPlayerMgr) GetPlayer(player_id int32) *dbOtherServerPlayerRow {
	if m.players == nil {
		return nil
	}
	row := m.players.GetRow(player_id)
	if row == nil {
		row = m.players.AddRow(player_id)
	}
	return row
}
