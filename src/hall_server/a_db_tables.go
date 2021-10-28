package main

import (
	"github.com/golang/protobuf/proto"
	_ "github.com/go-sql-driver/mysql"
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"ih_server/libs/log"
	"math/rand"
	"os"
	"os/exec"
	"ih_server/proto/gen_go/db_hall"
	"strings"
	"sync/atomic"
	"time"
)

type dbArgs struct {
	args  []interface{}
	count int32
}

func new_db_args(count int32) (th *dbArgs) {
	th = &dbArgs{}
	th.args = make([]interface{}, count)
	th.count = 0
	return th
}
func (th *dbArgs) Push(arg interface{}) {
	th.args[th.count] = arg
	th.count++
}
func (th *dbArgs) GetArgs() (args []interface{}) {
	return th.args[0:th.count]
}
func (th *DBC) StmtPrepare(s string) (r *sql.Stmt, e error) {
	th.m_db_lock.Lock("DBC.StmtPrepare")
	defer th.m_db_lock.Unlock()
	return th.m_db.Prepare(s)
}
func (th *DBC) StmtExec(stmt *sql.Stmt, args ...interface{}) (r sql.Result, err error) {
	th.m_db_lock.Lock("DBC.StmtExec")
	defer th.m_db_lock.Unlock()
	return stmt.Exec(args...)
}
func (th *DBC) StmtQuery(stmt *sql.Stmt, args ...interface{}) (r *sql.Rows, err error) {
	th.m_db_lock.Lock("DBC.StmtQuery")
	defer th.m_db_lock.Unlock()
	return stmt.Query(args...)
}
func (th *DBC) StmtQueryRow(stmt *sql.Stmt, args ...interface{}) (r *sql.Row) {
	th.m_db_lock.Lock("DBC.StmtQueryRow")
	defer th.m_db_lock.Unlock()
	return stmt.QueryRow(args...)
}
func (th *DBC) Query(s string, args ...interface{}) (r *sql.Rows, e error) {
	th.m_db_lock.Lock("DBC.Query")
	defer th.m_db_lock.Unlock()
	return th.m_db.Query(s, args...)
}
func (th *DBC) QueryRow(s string, args ...interface{}) (r *sql.Row) {
	th.m_db_lock.Lock("DBC.QueryRow")
	defer th.m_db_lock.Unlock()
	return th.m_db.QueryRow(s, args...)
}
func (th *DBC) Exec(s string, args ...interface{}) (r sql.Result, e error) {
	th.m_db_lock.Lock("DBC.Exec")
	defer th.m_db_lock.Unlock()
	return th.m_db.Exec(s, args...)
}
func (th *DBC) Conn(name string, addr string, acc string, pwd string, db_copy_path string) (err error) {
	log.Trace("%v %v %v %v", name, addr, acc, pwd)
	th.m_db_name = name
	source := acc + ":" + pwd + "@tcp(" + addr + ")/" + name + "?charset=utf8"
	th.m_db, err = sql.Open("mysql", source)
	if err != nil {
		log.Error("open db failed %v", err)
		return
	}
	
	th.m_db.SetConnMaxLifetime(time.Second * 5)

	th.m_db_lock = NewMutex()
	th.m_shutdown_lock = NewMutex()

	if config.DBCST_MAX-config.DBCST_MIN <= 1 {
		return errors.New("DBCST_MAX sub DBCST_MIN should greater than 1s")
	}

	err = th.init_tables()
	if err != nil {
		log.Error("init tables failed")
		return
	}

	if os.MkdirAll(db_copy_path, os.ModePerm) == nil {
		os.Chmod(db_copy_path, os.ModePerm)
	}
	
	th.m_db_last_copy_time = int32(time.Now().Hour())
	th.m_db_copy_path = db_copy_path
	addr_list := strings.Split(addr, ":")
	th.m_db_addr = addr_list[0]
	th.m_db_account = acc
	th.m_db_password = pwd
	th.m_initialized = true

	return
}
func (th *DBC) check_files_exist() (file_name string) {
	f_name := fmt.Sprintf("%v/%v_%v", th.m_db_copy_path, th.m_db_name, time.Now().Format("20060102-15"))
	num := int32(0)
	for {
		if num == 0 {
			file_name = f_name
		} else {
			file_name = f_name + fmt.Sprintf("_%v", num)
		}
		_, err := os.Lstat(file_name)
		if err != nil {
			break
		}
		num++
	}
	return file_name
}
func (th *DBC) Loop() {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}

		log.Trace("数据库主循环退出")
		th.m_shutdown_completed = true
	}()

	for {
		t := config.DBCST_MIN + rand.Intn(config.DBCST_MAX-config.DBCST_MIN)
		if t <= 0 {
			t = 600
		}

		for i := 0; i < t; i++ {
			time.Sleep(time.Second)
			if th.m_quit {
				break
			}
		}

		if th.m_quit {
			break
		}

		begin := time.Now()
		err := th.Save(false)
		if err != nil {
			log.Error("save db failed %v", err)
		}
		log.Trace("db存数据花费时长: %vms", time.Now().Sub(begin).Nanoseconds()/1000000)
		
		now_time := time.Now()
		if int32(now_time.Unix())-24*3600 >= th.m_db_last_copy_time {
			args := []string {
				fmt.Sprintf("-h%v", th.m_db_addr),
				fmt.Sprintf("-u%v", th.m_db_account),
				fmt.Sprintf("-p%v", th.m_db_password),
				th.m_db_name,
			}
			cmd := exec.Command("mysqldump", args...)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd_err := cmd.Run()
			if cmd_err == nil {
				file_name := th.check_files_exist()
				file, file_err := os.OpenFile(file_name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0660)
				defer file.Close()
				if file_err == nil {
					_, write_err := file.Write(out.Bytes())
					if write_err == nil {
						log.Trace("数据库备份成功！备份文件名:%v", file_name)
					} else {
						log.Error("数据库备份文件写入失败！备份文件名%v", file_name)
					}
				} else {
					log.Error("数据库备份文件打开失败！备份文件名%v", file_name)
				}
				file.Close()
			} else {
				log.Error("数据库备份失败！")
			}
			th.m_db_last_copy_time = int32(now_time.Unix())
		}
		
		if th.m_quit {
			break
		}
	}

	log.Trace("数据库缓存主循环退出，保存所有数据")

	err := th.Save(true)
	if err != nil {
		log.Error("shutdwon save db failed %v", err)
		return
	}

	err = th.m_db.Close()
	if err != nil {
		log.Error("close db failed %v", err)
		return
	}
}
func (th *DBC) Shutdown() {
	if !th.m_initialized {
		return
	}

	th.m_shutdown_lock.UnSafeLock("DBC.Shutdown")
	defer th.m_shutdown_lock.UnSafeUnlock()

	if th.m_quit {
		return
	}
	th.m_quit = true

	log.Trace("关闭数据库缓存")

	begin := time.Now()

	for {
		if th.m_shutdown_completed {
			break
		}

		time.Sleep(time.Millisecond * 100)
	}

	log.Trace("关闭数据库缓存耗时 %v 秒", time.Now().Sub(begin).Seconds())
}


const DBC_VERSION = 1
const DBC_SUB_VERSION = 0

type dbPlayerInfoData struct{
	Lvl int32
	Exp int32
	CreateUnix int32
	Gold int32
	Diamond int32
	LastLogout int32
	LastLogin int32
	VipLvl int32
	Head int32
}
func (th* dbPlayerInfoData)from_pb(pb *db.PlayerInfo){
	if pb == nil {
		return
	}
	th.Lvl = pb.GetLvl()
	th.Exp = pb.GetExp()
	th.CreateUnix = pb.GetCreateUnix()
	th.Gold = pb.GetGold()
	th.Diamond = pb.GetDiamond()
	th.LastLogout = pb.GetLastLogout()
	th.LastLogin = pb.GetLastLogin()
	th.VipLvl = pb.GetVipLvl()
	th.Head = pb.GetHead()
	return
}
func (th* dbPlayerInfoData)to_pb()(pb *db.PlayerInfo){
	pb = &db.PlayerInfo{}
	pb.Lvl = proto.Int32(th.Lvl)
	pb.Exp = proto.Int32(th.Exp)
	pb.CreateUnix = proto.Int32(th.CreateUnix)
	pb.Gold = proto.Int32(th.Gold)
	pb.Diamond = proto.Int32(th.Diamond)
	pb.LastLogout = proto.Int32(th.LastLogout)
	pb.LastLogin = proto.Int32(th.LastLogin)
	pb.VipLvl = proto.Int32(th.VipLvl)
	pb.Head = proto.Int32(th.Head)
	return
}
func (th* dbPlayerInfoData)clone_to(d *dbPlayerInfoData){
	d.Lvl = th.Lvl
	d.Exp = th.Exp
	d.CreateUnix = th.CreateUnix
	d.Gold = th.Gold
	d.Diamond = th.Diamond
	d.LastLogout = th.LastLogout
	d.LastLogin = th.LastLogin
	d.VipLvl = th.VipLvl
	d.Head = th.Head
	return
}
type dbPlayerGlobalData struct{
	CurrentRoleId int32
}
func (th* dbPlayerGlobalData)from_pb(pb *db.PlayerGlobal){
	if pb == nil {
		return
	}
	th.CurrentRoleId = pb.GetCurrentRoleId()
	return
}
func (th* dbPlayerGlobalData)to_pb()(pb *db.PlayerGlobal){
	pb = &db.PlayerGlobal{}
	pb.CurrentRoleId = proto.Int32(th.CurrentRoleId)
	return
}
func (th* dbPlayerGlobalData)clone_to(d *dbPlayerGlobalData){
	d.CurrentRoleId = th.CurrentRoleId
	return
}
type dbPlayerItemData struct{
	Id int32
	Count int32
}
func (th* dbPlayerItemData)from_pb(pb *db.PlayerItem){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.Count = pb.GetCount()
	return
}
func (th* dbPlayerItemData)to_pb()(pb *db.PlayerItem){
	pb = &db.PlayerItem{}
	pb.Id = proto.Int32(th.Id)
	pb.Count = proto.Int32(th.Count)
	return
}
func (th* dbPlayerItemData)clone_to(d *dbPlayerItemData){
	d.Id = th.Id
	d.Count = th.Count
	return
}
type dbPlayerRoleCommonData struct{
	DisplaceRoleId int32
	DisplacedNewRoleTableId int32
	DisplaceGroupId int32
	PowerUpdateTime int32
}
func (th* dbPlayerRoleCommonData)from_pb(pb *db.PlayerRoleCommon){
	if pb == nil {
		return
	}
	th.DisplaceRoleId = pb.GetDisplaceRoleId()
	th.DisplacedNewRoleTableId = pb.GetDisplacedNewRoleTableId()
	th.DisplaceGroupId = pb.GetDisplaceGroupId()
	th.PowerUpdateTime = pb.GetPowerUpdateTime()
	return
}
func (th* dbPlayerRoleCommonData)to_pb()(pb *db.PlayerRoleCommon){
	pb = &db.PlayerRoleCommon{}
	pb.DisplaceRoleId = proto.Int32(th.DisplaceRoleId)
	pb.DisplacedNewRoleTableId = proto.Int32(th.DisplacedNewRoleTableId)
	pb.DisplaceGroupId = proto.Int32(th.DisplaceGroupId)
	pb.PowerUpdateTime = proto.Int32(th.PowerUpdateTime)
	return
}
func (th* dbPlayerRoleCommonData)clone_to(d *dbPlayerRoleCommonData){
	d.DisplaceRoleId = th.DisplaceRoleId
	d.DisplacedNewRoleTableId = th.DisplacedNewRoleTableId
	d.DisplaceGroupId = th.DisplaceGroupId
	d.PowerUpdateTime = th.PowerUpdateTime
	return
}
type dbPlayerRoleData struct{
	Id int32
	TableId int32
	Rank int32
	Level int32
	Equip []int32
	IsLock int32
	State int32
}
func (th* dbPlayerRoleData)from_pb(pb *db.PlayerRole){
	if pb == nil {
		th.Equip = make([]int32,0)
		return
	}
	th.Id = pb.GetId()
	th.TableId = pb.GetTableId()
	th.Rank = pb.GetRank()
	th.Level = pb.GetLevel()
	th.Equip = make([]int32,len(pb.GetEquip()))
	for i, v := range pb.GetEquip() {
		th.Equip[i] = v
	}
	th.IsLock = pb.GetIsLock()
	th.State = pb.GetState()
	return
}
func (th* dbPlayerRoleData)to_pb()(pb *db.PlayerRole){
	pb = &db.PlayerRole{}
	pb.Id = proto.Int32(th.Id)
	pb.TableId = proto.Int32(th.TableId)
	pb.Rank = proto.Int32(th.Rank)
	pb.Level = proto.Int32(th.Level)
	pb.Equip = make([]int32, len(th.Equip))
	for i, v := range th.Equip {
		pb.Equip[i]=v
	}
	pb.IsLock = proto.Int32(th.IsLock)
	pb.State = proto.Int32(th.State)
	return
}
func (th* dbPlayerRoleData)clone_to(d *dbPlayerRoleData){
	d.Id = th.Id
	d.TableId = th.TableId
	d.Rank = th.Rank
	d.Level = th.Level
	d.Equip = make([]int32, len(th.Equip))
	for _ii, _vv := range th.Equip {
		d.Equip[_ii]=_vv
	}
	d.IsLock = th.IsLock
	d.State = th.State
	return
}
type dbPlayerRoleHandbookData struct{
	Role []int32
}
func (th* dbPlayerRoleHandbookData)from_pb(pb *db.PlayerRoleHandbook){
	if pb == nil {
		th.Role = make([]int32,0)
		return
	}
	th.Role = make([]int32,len(pb.GetRole()))
	for i, v := range pb.GetRole() {
		th.Role[i] = v
	}
	return
}
func (th* dbPlayerRoleHandbookData)to_pb()(pb *db.PlayerRoleHandbook){
	pb = &db.PlayerRoleHandbook{}
	pb.Role = make([]int32, len(th.Role))
	for i, v := range th.Role {
		pb.Role[i]=v
	}
	return
}
func (th* dbPlayerRoleHandbookData)clone_to(d *dbPlayerRoleHandbookData){
	d.Role = make([]int32, len(th.Role))
	for _ii, _vv := range th.Role {
		d.Role[_ii]=_vv
	}
	return
}
type dbPlayerBattleTeamData struct{
	DefenseMembers []int32
	CampaignMembers []int32
	DefenseArtifactId int32
	CampaignArtifactId int32
}
func (th* dbPlayerBattleTeamData)from_pb(pb *db.PlayerBattleTeam){
	if pb == nil {
		th.DefenseMembers = make([]int32,0)
		th.CampaignMembers = make([]int32,0)
		return
	}
	th.DefenseMembers = make([]int32,len(pb.GetDefenseMembers()))
	for i, v := range pb.GetDefenseMembers() {
		th.DefenseMembers[i] = v
	}
	th.CampaignMembers = make([]int32,len(pb.GetCampaignMembers()))
	for i, v := range pb.GetCampaignMembers() {
		th.CampaignMembers[i] = v
	}
	th.DefenseArtifactId = pb.GetDefenseArtifactId()
	th.CampaignArtifactId = pb.GetCampaignArtifactId()
	return
}
func (th* dbPlayerBattleTeamData)to_pb()(pb *db.PlayerBattleTeam){
	pb = &db.PlayerBattleTeam{}
	pb.DefenseMembers = make([]int32, len(th.DefenseMembers))
	for i, v := range th.DefenseMembers {
		pb.DefenseMembers[i]=v
	}
	pb.CampaignMembers = make([]int32, len(th.CampaignMembers))
	for i, v := range th.CampaignMembers {
		pb.CampaignMembers[i]=v
	}
	pb.DefenseArtifactId = proto.Int32(th.DefenseArtifactId)
	pb.CampaignArtifactId = proto.Int32(th.CampaignArtifactId)
	return
}
func (th* dbPlayerBattleTeamData)clone_to(d *dbPlayerBattleTeamData){
	d.DefenseMembers = make([]int32, len(th.DefenseMembers))
	for _ii, _vv := range th.DefenseMembers {
		d.DefenseMembers[_ii]=_vv
	}
	d.CampaignMembers = make([]int32, len(th.CampaignMembers))
	for _ii, _vv := range th.CampaignMembers {
		d.CampaignMembers[_ii]=_vv
	}
	d.DefenseArtifactId = th.DefenseArtifactId
	d.CampaignArtifactId = th.CampaignArtifactId
	return
}
type dbPlayerCampaignCommonData struct{
	CurrentCampaignId int32
	HangupLastDropStaticIncomeTime int32
	HangupLastDropRandomIncomeTime int32
	HangupCampaignId int32
	LastestPassedCampaignId int32
	RankSerialId int32
	VipAccelNum int32
	VipAccelRefreshTime int32
	PassCampaginTime int32
}
func (th* dbPlayerCampaignCommonData)from_pb(pb *db.PlayerCampaignCommon){
	if pb == nil {
		return
	}
	th.CurrentCampaignId = pb.GetCurrentCampaignId()
	th.HangupLastDropStaticIncomeTime = pb.GetHangupLastDropStaticIncomeTime()
	th.HangupLastDropRandomIncomeTime = pb.GetHangupLastDropRandomIncomeTime()
	th.HangupCampaignId = pb.GetHangupCampaignId()
	th.LastestPassedCampaignId = pb.GetLastestPassedCampaignId()
	th.RankSerialId = pb.GetRankSerialId()
	th.VipAccelNum = pb.GetVipAccelNum()
	th.VipAccelRefreshTime = pb.GetVipAccelRefreshTime()
	th.PassCampaginTime = pb.GetPassCampaginTime()
	return
}
func (th* dbPlayerCampaignCommonData)to_pb()(pb *db.PlayerCampaignCommon){
	pb = &db.PlayerCampaignCommon{}
	pb.CurrentCampaignId = proto.Int32(th.CurrentCampaignId)
	pb.HangupLastDropStaticIncomeTime = proto.Int32(th.HangupLastDropStaticIncomeTime)
	pb.HangupLastDropRandomIncomeTime = proto.Int32(th.HangupLastDropRandomIncomeTime)
	pb.HangupCampaignId = proto.Int32(th.HangupCampaignId)
	pb.LastestPassedCampaignId = proto.Int32(th.LastestPassedCampaignId)
	pb.RankSerialId = proto.Int32(th.RankSerialId)
	pb.VipAccelNum = proto.Int32(th.VipAccelNum)
	pb.VipAccelRefreshTime = proto.Int32(th.VipAccelRefreshTime)
	pb.PassCampaginTime = proto.Int32(th.PassCampaginTime)
	return
}
func (th* dbPlayerCampaignCommonData)clone_to(d *dbPlayerCampaignCommonData){
	d.CurrentCampaignId = th.CurrentCampaignId
	d.HangupLastDropStaticIncomeTime = th.HangupLastDropStaticIncomeTime
	d.HangupLastDropRandomIncomeTime = th.HangupLastDropRandomIncomeTime
	d.HangupCampaignId = th.HangupCampaignId
	d.LastestPassedCampaignId = th.LastestPassedCampaignId
	d.RankSerialId = th.RankSerialId
	d.VipAccelNum = th.VipAccelNum
	d.VipAccelRefreshTime = th.VipAccelRefreshTime
	d.PassCampaginTime = th.PassCampaginTime
	return
}
type dbPlayerCampaignData struct{
	CampaignId int32
}
func (th* dbPlayerCampaignData)from_pb(pb *db.PlayerCampaign){
	if pb == nil {
		return
	}
	th.CampaignId = pb.GetCampaignId()
	return
}
func (th* dbPlayerCampaignData)to_pb()(pb *db.PlayerCampaign){
	pb = &db.PlayerCampaign{}
	pb.CampaignId = proto.Int32(th.CampaignId)
	return
}
func (th* dbPlayerCampaignData)clone_to(d *dbPlayerCampaignData){
	d.CampaignId = th.CampaignId
	return
}
type dbPlayerCampaignStaticIncomeData struct{
	ItemId int32
	ItemNum int32
}
func (th* dbPlayerCampaignStaticIncomeData)from_pb(pb *db.PlayerCampaignStaticIncome){
	if pb == nil {
		return
	}
	th.ItemId = pb.GetItemId()
	th.ItemNum = pb.GetItemNum()
	return
}
func (th* dbPlayerCampaignStaticIncomeData)to_pb()(pb *db.PlayerCampaignStaticIncome){
	pb = &db.PlayerCampaignStaticIncome{}
	pb.ItemId = proto.Int32(th.ItemId)
	pb.ItemNum = proto.Int32(th.ItemNum)
	return
}
func (th* dbPlayerCampaignStaticIncomeData)clone_to(d *dbPlayerCampaignStaticIncomeData){
	d.ItemId = th.ItemId
	d.ItemNum = th.ItemNum
	return
}
type dbPlayerCampaignRandomIncomeData struct{
	ItemId int32
	ItemNum int32
}
func (th* dbPlayerCampaignRandomIncomeData)from_pb(pb *db.PlayerCampaignRandomIncome){
	if pb == nil {
		return
	}
	th.ItemId = pb.GetItemId()
	th.ItemNum = pb.GetItemNum()
	return
}
func (th* dbPlayerCampaignRandomIncomeData)to_pb()(pb *db.PlayerCampaignRandomIncome){
	pb = &db.PlayerCampaignRandomIncome{}
	pb.ItemId = proto.Int32(th.ItemId)
	pb.ItemNum = proto.Int32(th.ItemNum)
	return
}
func (th* dbPlayerCampaignRandomIncomeData)clone_to(d *dbPlayerCampaignRandomIncomeData){
	d.ItemId = th.ItemId
	d.ItemNum = th.ItemNum
	return
}
type dbPlayerMailCommonData struct{
	CurrId int32
	LastSendPlayerMailTime int32
}
func (th* dbPlayerMailCommonData)from_pb(pb *db.PlayerMailCommon){
	if pb == nil {
		return
	}
	th.CurrId = pb.GetCurrId()
	th.LastSendPlayerMailTime = pb.GetLastSendPlayerMailTime()
	return
}
func (th* dbPlayerMailCommonData)to_pb()(pb *db.PlayerMailCommon){
	pb = &db.PlayerMailCommon{}
	pb.CurrId = proto.Int32(th.CurrId)
	pb.LastSendPlayerMailTime = proto.Int32(th.LastSendPlayerMailTime)
	return
}
func (th* dbPlayerMailCommonData)clone_to(d *dbPlayerMailCommonData){
	d.CurrId = th.CurrId
	d.LastSendPlayerMailTime = th.LastSendPlayerMailTime
	return
}
type dbPlayerMailData struct{
	Id int32
	Type int8
	Title string
	Content string
	SendUnix int32
	AttachItemIds []int32
	AttachItemNums []int32
	IsRead int32
	IsGetAttached int32
	SenderId int32
	SenderName string
	Subtype int32
	ExtraValue int32
}
func (th* dbPlayerMailData)from_pb(pb *db.PlayerMail){
	if pb == nil {
		th.AttachItemIds = make([]int32,0)
		th.AttachItemNums = make([]int32,0)
		return
	}
	th.Id = pb.GetId()
	th.Type = int8(pb.GetType())
	th.Title = pb.GetTitle()
	th.Content = pb.GetContent()
	th.SendUnix = pb.GetSendUnix()
	th.AttachItemIds = make([]int32,len(pb.GetAttachItemIds()))
	for i, v := range pb.GetAttachItemIds() {
		th.AttachItemIds[i] = v
	}
	th.AttachItemNums = make([]int32,len(pb.GetAttachItemNums()))
	for i, v := range pb.GetAttachItemNums() {
		th.AttachItemNums[i] = v
	}
	th.IsRead = pb.GetIsRead()
	th.IsGetAttached = pb.GetIsGetAttached()
	th.SenderId = pb.GetSenderId()
	th.SenderName = pb.GetSenderName()
	th.Subtype = pb.GetSubtype()
	th.ExtraValue = pb.GetExtraValue()
	return
}
func (th* dbPlayerMailData)to_pb()(pb *db.PlayerMail){
	pb = &db.PlayerMail{}
	pb.Id = proto.Int32(th.Id)
	temp_Type:=int32(th.Type)
	pb.Type = proto.Int32(temp_Type)
	pb.Title = proto.String(th.Title)
	pb.Content = proto.String(th.Content)
	pb.SendUnix = proto.Int32(th.SendUnix)
	pb.AttachItemIds = make([]int32, len(th.AttachItemIds))
	for i, v := range th.AttachItemIds {
		pb.AttachItemIds[i]=v
	}
	pb.AttachItemNums = make([]int32, len(th.AttachItemNums))
	for i, v := range th.AttachItemNums {
		pb.AttachItemNums[i]=v
	}
	pb.IsRead = proto.Int32(th.IsRead)
	pb.IsGetAttached = proto.Int32(th.IsGetAttached)
	pb.SenderId = proto.Int32(th.SenderId)
	pb.SenderName = proto.String(th.SenderName)
	pb.Subtype = proto.Int32(th.Subtype)
	pb.ExtraValue = proto.Int32(th.ExtraValue)
	return
}
func (th* dbPlayerMailData)clone_to(d *dbPlayerMailData){
	d.Id = th.Id
	d.Type = int8(th.Type)
	d.Title = th.Title
	d.Content = th.Content
	d.SendUnix = th.SendUnix
	d.AttachItemIds = make([]int32, len(th.AttachItemIds))
	for _ii, _vv := range th.AttachItemIds {
		d.AttachItemIds[_ii]=_vv
	}
	d.AttachItemNums = make([]int32, len(th.AttachItemNums))
	for _ii, _vv := range th.AttachItemNums {
		d.AttachItemNums[_ii]=_vv
	}
	d.IsRead = th.IsRead
	d.IsGetAttached = th.IsGetAttached
	d.SenderId = th.SenderId
	d.SenderName = th.SenderName
	d.Subtype = th.Subtype
	d.ExtraValue = th.ExtraValue
	return
}
type dbPlayerBattleSaveData struct{
	Id int32
	Side int32
	SaveTime int32
}
func (th* dbPlayerBattleSaveData)from_pb(pb *db.PlayerBattleSave){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.Side = pb.GetSide()
	th.SaveTime = pb.GetSaveTime()
	return
}
func (th* dbPlayerBattleSaveData)to_pb()(pb *db.PlayerBattleSave){
	pb = &db.PlayerBattleSave{}
	pb.Id = proto.Int32(th.Id)
	pb.Side = proto.Int32(th.Side)
	pb.SaveTime = proto.Int32(th.SaveTime)
	return
}
func (th* dbPlayerBattleSaveData)clone_to(d *dbPlayerBattleSaveData){
	d.Id = th.Id
	d.Side = th.Side
	d.SaveTime = th.SaveTime
	return
}
type dbPlayerTalentData struct{
	Id int32
	Level int32
}
func (th* dbPlayerTalentData)from_pb(pb *db.PlayerTalent){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.Level = pb.GetLevel()
	return
}
func (th* dbPlayerTalentData)to_pb()(pb *db.PlayerTalent){
	pb = &db.PlayerTalent{}
	pb.Id = proto.Int32(th.Id)
	pb.Level = proto.Int32(th.Level)
	return
}
func (th* dbPlayerTalentData)clone_to(d *dbPlayerTalentData){
	d.Id = th.Id
	d.Level = th.Level
	return
}
type dbPlayerTowerCommonData struct{
	CurrId int32
	Keys int32
	LastGetNewKeyTime int32
	RankSerialId int32
	PassTowerTime int32
}
func (th* dbPlayerTowerCommonData)from_pb(pb *db.PlayerTowerCommon){
	if pb == nil {
		return
	}
	th.CurrId = pb.GetCurrId()
	th.Keys = pb.GetKeys()
	th.LastGetNewKeyTime = pb.GetLastGetNewKeyTime()
	th.RankSerialId = pb.GetRankSerialId()
	th.PassTowerTime = pb.GetPassTowerTime()
	return
}
func (th* dbPlayerTowerCommonData)to_pb()(pb *db.PlayerTowerCommon){
	pb = &db.PlayerTowerCommon{}
	pb.CurrId = proto.Int32(th.CurrId)
	pb.Keys = proto.Int32(th.Keys)
	pb.LastGetNewKeyTime = proto.Int32(th.LastGetNewKeyTime)
	pb.RankSerialId = proto.Int32(th.RankSerialId)
	pb.PassTowerTime = proto.Int32(th.PassTowerTime)
	return
}
func (th* dbPlayerTowerCommonData)clone_to(d *dbPlayerTowerCommonData){
	d.CurrId = th.CurrId
	d.Keys = th.Keys
	d.LastGetNewKeyTime = th.LastGetNewKeyTime
	d.RankSerialId = th.RankSerialId
	d.PassTowerTime = th.PassTowerTime
	return
}
type dbPlayerTowerData struct{
	Id int32
}
func (th* dbPlayerTowerData)from_pb(pb *db.PlayerTower){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	return
}
func (th* dbPlayerTowerData)to_pb()(pb *db.PlayerTower){
	pb = &db.PlayerTower{}
	pb.Id = proto.Int32(th.Id)
	return
}
func (th* dbPlayerTowerData)clone_to(d *dbPlayerTowerData){
	d.Id = th.Id
	return
}
type dbPlayerDrawData struct{
	Type int32
	LastDrawTime int32
	Num int32
}
func (th* dbPlayerDrawData)from_pb(pb *db.PlayerDraw){
	if pb == nil {
		return
	}
	th.Type = pb.GetType()
	th.LastDrawTime = pb.GetLastDrawTime()
	th.Num = pb.GetNum()
	return
}
func (th* dbPlayerDrawData)to_pb()(pb *db.PlayerDraw){
	pb = &db.PlayerDraw{}
	pb.Type = proto.Int32(th.Type)
	pb.LastDrawTime = proto.Int32(th.LastDrawTime)
	pb.Num = proto.Int32(th.Num)
	return
}
func (th* dbPlayerDrawData)clone_to(d *dbPlayerDrawData){
	d.Type = th.Type
	d.LastDrawTime = th.LastDrawTime
	d.Num = th.Num
	return
}
type dbPlayerGoldHandData struct{
	LastRefreshTime int32
	LeftNum []int32
}
func (th* dbPlayerGoldHandData)from_pb(pb *db.PlayerGoldHand){
	if pb == nil {
		th.LeftNum = make([]int32,0)
		return
	}
	th.LastRefreshTime = pb.GetLastRefreshTime()
	th.LeftNum = make([]int32,len(pb.GetLeftNum()))
	for i, v := range pb.GetLeftNum() {
		th.LeftNum[i] = v
	}
	return
}
func (th* dbPlayerGoldHandData)to_pb()(pb *db.PlayerGoldHand){
	pb = &db.PlayerGoldHand{}
	pb.LastRefreshTime = proto.Int32(th.LastRefreshTime)
	pb.LeftNum = make([]int32, len(th.LeftNum))
	for i, v := range th.LeftNum {
		pb.LeftNum[i]=v
	}
	return
}
func (th* dbPlayerGoldHandData)clone_to(d *dbPlayerGoldHandData){
	d.LastRefreshTime = th.LastRefreshTime
	d.LeftNum = make([]int32, len(th.LeftNum))
	for _ii, _vv := range th.LeftNum {
		d.LeftNum[_ii]=_vv
	}
	return
}
type dbPlayerShopData struct{
	Id int32
	LastFreeRefreshTime int32
	LastAutoRefreshTime int32
	CurrAutoId int32
}
func (th* dbPlayerShopData)from_pb(pb *db.PlayerShop){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.LastFreeRefreshTime = pb.GetLastFreeRefreshTime()
	th.LastAutoRefreshTime = pb.GetLastAutoRefreshTime()
	th.CurrAutoId = pb.GetCurrAutoId()
	return
}
func (th* dbPlayerShopData)to_pb()(pb *db.PlayerShop){
	pb = &db.PlayerShop{}
	pb.Id = proto.Int32(th.Id)
	pb.LastFreeRefreshTime = proto.Int32(th.LastFreeRefreshTime)
	pb.LastAutoRefreshTime = proto.Int32(th.LastAutoRefreshTime)
	pb.CurrAutoId = proto.Int32(th.CurrAutoId)
	return
}
func (th* dbPlayerShopData)clone_to(d *dbPlayerShopData){
	d.Id = th.Id
	d.LastFreeRefreshTime = th.LastFreeRefreshTime
	d.LastAutoRefreshTime = th.LastAutoRefreshTime
	d.CurrAutoId = th.CurrAutoId
	return
}
type dbPlayerShopItemData struct{
	Id int32
	ShopItemId int32
	LeftNum int32
	ShopId int32
	BuyNum int32
}
func (th* dbPlayerShopItemData)from_pb(pb *db.PlayerShopItem){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.ShopItemId = pb.GetShopItemId()
	th.LeftNum = pb.GetLeftNum()
	th.ShopId = pb.GetShopId()
	th.BuyNum = pb.GetBuyNum()
	return
}
func (th* dbPlayerShopItemData)to_pb()(pb *db.PlayerShopItem){
	pb = &db.PlayerShopItem{}
	pb.Id = proto.Int32(th.Id)
	pb.ShopItemId = proto.Int32(th.ShopItemId)
	pb.LeftNum = proto.Int32(th.LeftNum)
	pb.ShopId = proto.Int32(th.ShopId)
	pb.BuyNum = proto.Int32(th.BuyNum)
	return
}
func (th* dbPlayerShopItemData)clone_to(d *dbPlayerShopItemData){
	d.Id = th.Id
	d.ShopItemId = th.ShopItemId
	d.LeftNum = th.LeftNum
	d.ShopId = th.ShopId
	d.BuyNum = th.BuyNum
	return
}
type dbPlayerArenaData struct{
	RepeatedWinNum int32
	RepeatedLoseNum int32
	Score int32
	UpdateScoreTime int32
	MatchedPlayerId int32
	HistoryTopRank int32
	FirstGetTicket int32
	LastTicketsRefreshTime int32
	SerialId int32
}
func (th* dbPlayerArenaData)from_pb(pb *db.PlayerArena){
	if pb == nil {
		return
	}
	th.RepeatedWinNum = pb.GetRepeatedWinNum()
	th.RepeatedLoseNum = pb.GetRepeatedLoseNum()
	th.Score = pb.GetScore()
	th.UpdateScoreTime = pb.GetUpdateScoreTime()
	th.MatchedPlayerId = pb.GetMatchedPlayerId()
	th.HistoryTopRank = pb.GetHistoryTopRank()
	th.FirstGetTicket = pb.GetFirstGetTicket()
	th.LastTicketsRefreshTime = pb.GetLastTicketsRefreshTime()
	th.SerialId = pb.GetSerialId()
	return
}
func (th* dbPlayerArenaData)to_pb()(pb *db.PlayerArena){
	pb = &db.PlayerArena{}
	pb.RepeatedWinNum = proto.Int32(th.RepeatedWinNum)
	pb.RepeatedLoseNum = proto.Int32(th.RepeatedLoseNum)
	pb.Score = proto.Int32(th.Score)
	pb.UpdateScoreTime = proto.Int32(th.UpdateScoreTime)
	pb.MatchedPlayerId = proto.Int32(th.MatchedPlayerId)
	pb.HistoryTopRank = proto.Int32(th.HistoryTopRank)
	pb.FirstGetTicket = proto.Int32(th.FirstGetTicket)
	pb.LastTicketsRefreshTime = proto.Int32(th.LastTicketsRefreshTime)
	pb.SerialId = proto.Int32(th.SerialId)
	return
}
func (th* dbPlayerArenaData)clone_to(d *dbPlayerArenaData){
	d.RepeatedWinNum = th.RepeatedWinNum
	d.RepeatedLoseNum = th.RepeatedLoseNum
	d.Score = th.Score
	d.UpdateScoreTime = th.UpdateScoreTime
	d.MatchedPlayerId = th.MatchedPlayerId
	d.HistoryTopRank = th.HistoryTopRank
	d.FirstGetTicket = th.FirstGetTicket
	d.LastTicketsRefreshTime = th.LastTicketsRefreshTime
	d.SerialId = th.SerialId
	return
}
type dbPlayerEquipData struct{
	TmpSaveLeftSlotRoleId int32
	TmpLeftSlotItemId int32
}
func (th* dbPlayerEquipData)from_pb(pb *db.PlayerEquip){
	if pb == nil {
		return
	}
	th.TmpSaveLeftSlotRoleId = pb.GetTmpSaveLeftSlotRoleId()
	th.TmpLeftSlotItemId = pb.GetTmpLeftSlotItemId()
	return
}
func (th* dbPlayerEquipData)to_pb()(pb *db.PlayerEquip){
	pb = &db.PlayerEquip{}
	pb.TmpSaveLeftSlotRoleId = proto.Int32(th.TmpSaveLeftSlotRoleId)
	pb.TmpLeftSlotItemId = proto.Int32(th.TmpLeftSlotItemId)
	return
}
func (th* dbPlayerEquipData)clone_to(d *dbPlayerEquipData){
	d.TmpSaveLeftSlotRoleId = th.TmpSaveLeftSlotRoleId
	d.TmpLeftSlotItemId = th.TmpLeftSlotItemId
	return
}
type dbPlayerActiveStageCommonData struct{
	LastRefreshTime int32
	GetPointsDay int32
	WithdrawPoints int32
}
func (th* dbPlayerActiveStageCommonData)from_pb(pb *db.PlayerActiveStageCommon){
	if pb == nil {
		return
	}
	th.LastRefreshTime = pb.GetLastRefreshTime()
	th.GetPointsDay = pb.GetGetPointsDay()
	th.WithdrawPoints = pb.GetWithdrawPoints()
	return
}
func (th* dbPlayerActiveStageCommonData)to_pb()(pb *db.PlayerActiveStageCommon){
	pb = &db.PlayerActiveStageCommon{}
	pb.LastRefreshTime = proto.Int32(th.LastRefreshTime)
	pb.GetPointsDay = proto.Int32(th.GetPointsDay)
	pb.WithdrawPoints = proto.Int32(th.WithdrawPoints)
	return
}
func (th* dbPlayerActiveStageCommonData)clone_to(d *dbPlayerActiveStageCommonData){
	d.LastRefreshTime = th.LastRefreshTime
	d.GetPointsDay = th.GetPointsDay
	d.WithdrawPoints = th.WithdrawPoints
	return
}
type dbPlayerActiveStageData struct{
	Type int32
	CanChallengeNum int32
	PurchasedNum int32
	BuyNum int32
}
func (th* dbPlayerActiveStageData)from_pb(pb *db.PlayerActiveStage){
	if pb == nil {
		return
	}
	th.Type = pb.GetType()
	th.CanChallengeNum = pb.GetCanChallengeNum()
	th.PurchasedNum = pb.GetPurchasedNum()
	th.BuyNum = pb.GetBuyNum()
	return
}
func (th* dbPlayerActiveStageData)to_pb()(pb *db.PlayerActiveStage){
	pb = &db.PlayerActiveStage{}
	pb.Type = proto.Int32(th.Type)
	pb.CanChallengeNum = proto.Int32(th.CanChallengeNum)
	pb.PurchasedNum = proto.Int32(th.PurchasedNum)
	pb.BuyNum = proto.Int32(th.BuyNum)
	return
}
func (th* dbPlayerActiveStageData)clone_to(d *dbPlayerActiveStageData){
	d.Type = th.Type
	d.CanChallengeNum = th.CanChallengeNum
	d.PurchasedNum = th.PurchasedNum
	d.BuyNum = th.BuyNum
	return
}
type dbPlayerFriendCommonData struct{
	LastRecommendTime int32
	LastBossRefreshTime int32
	FriendBossTableId int32
	FriendBossHpPercent int32
	AttackBossPlayerList []int32
	LastGetStaminaTime int32
	AssistRoleId int32
	LastGetPointsTime int32
	GetPointsDay int32
	SearchedBossNum int32
	LastSearchBossNumRefreshTime int32
}
func (th* dbPlayerFriendCommonData)from_pb(pb *db.PlayerFriendCommon){
	if pb == nil {
		th.AttackBossPlayerList = make([]int32,0)
		return
	}
	th.LastRecommendTime = pb.GetLastRecommendTime()
	th.LastBossRefreshTime = pb.GetLastBossRefreshTime()
	th.FriendBossTableId = pb.GetFriendBossTableId()
	th.FriendBossHpPercent = pb.GetFriendBossHpPercent()
	th.AttackBossPlayerList = make([]int32,len(pb.GetAttackBossPlayerList()))
	for i, v := range pb.GetAttackBossPlayerList() {
		th.AttackBossPlayerList[i] = v
	}
	th.LastGetStaminaTime = pb.GetLastGetStaminaTime()
	th.AssistRoleId = pb.GetAssistRoleId()
	th.LastGetPointsTime = pb.GetLastGetPointsTime()
	th.GetPointsDay = pb.GetGetPointsDay()
	th.SearchedBossNum = pb.GetSearchedBossNum()
	th.LastSearchBossNumRefreshTime = pb.GetLastSearchBossNumRefreshTime()
	return
}
func (th* dbPlayerFriendCommonData)to_pb()(pb *db.PlayerFriendCommon){
	pb = &db.PlayerFriendCommon{}
	pb.LastRecommendTime = proto.Int32(th.LastRecommendTime)
	pb.LastBossRefreshTime = proto.Int32(th.LastBossRefreshTime)
	pb.FriendBossTableId = proto.Int32(th.FriendBossTableId)
	pb.FriendBossHpPercent = proto.Int32(th.FriendBossHpPercent)
	pb.AttackBossPlayerList = make([]int32, len(th.AttackBossPlayerList))
	for i, v := range th.AttackBossPlayerList {
		pb.AttackBossPlayerList[i]=v
	}
	pb.LastGetStaminaTime = proto.Int32(th.LastGetStaminaTime)
	pb.AssistRoleId = proto.Int32(th.AssistRoleId)
	pb.LastGetPointsTime = proto.Int32(th.LastGetPointsTime)
	pb.GetPointsDay = proto.Int32(th.GetPointsDay)
	pb.SearchedBossNum = proto.Int32(th.SearchedBossNum)
	pb.LastSearchBossNumRefreshTime = proto.Int32(th.LastSearchBossNumRefreshTime)
	return
}
func (th* dbPlayerFriendCommonData)clone_to(d *dbPlayerFriendCommonData){
	d.LastRecommendTime = th.LastRecommendTime
	d.LastBossRefreshTime = th.LastBossRefreshTime
	d.FriendBossTableId = th.FriendBossTableId
	d.FriendBossHpPercent = th.FriendBossHpPercent
	d.AttackBossPlayerList = make([]int32, len(th.AttackBossPlayerList))
	for _ii, _vv := range th.AttackBossPlayerList {
		d.AttackBossPlayerList[_ii]=_vv
	}
	d.LastGetStaminaTime = th.LastGetStaminaTime
	d.AssistRoleId = th.AssistRoleId
	d.LastGetPointsTime = th.LastGetPointsTime
	d.GetPointsDay = th.GetPointsDay
	d.SearchedBossNum = th.SearchedBossNum
	d.LastSearchBossNumRefreshTime = th.LastSearchBossNumRefreshTime
	return
}
type dbPlayerFriendData struct{
	PlayerId int32
	LastGivePointsTime int32
	GetPoints int32
}
func (th* dbPlayerFriendData)from_pb(pb *db.PlayerFriend){
	if pb == nil {
		return
	}
	th.PlayerId = pb.GetPlayerId()
	th.LastGivePointsTime = pb.GetLastGivePointsTime()
	th.GetPoints = pb.GetGetPoints()
	return
}
func (th* dbPlayerFriendData)to_pb()(pb *db.PlayerFriend){
	pb = &db.PlayerFriend{}
	pb.PlayerId = proto.Int32(th.PlayerId)
	pb.LastGivePointsTime = proto.Int32(th.LastGivePointsTime)
	pb.GetPoints = proto.Int32(th.GetPoints)
	return
}
func (th* dbPlayerFriendData)clone_to(d *dbPlayerFriendData){
	d.PlayerId = th.PlayerId
	d.LastGivePointsTime = th.LastGivePointsTime
	d.GetPoints = th.GetPoints
	return
}
type dbPlayerFriendRecommendData struct{
	PlayerId int32
}
func (th* dbPlayerFriendRecommendData)from_pb(pb *db.PlayerFriendRecommend){
	if pb == nil {
		return
	}
	th.PlayerId = pb.GetPlayerId()
	return
}
func (th* dbPlayerFriendRecommendData)to_pb()(pb *db.PlayerFriendRecommend){
	pb = &db.PlayerFriendRecommend{}
	pb.PlayerId = proto.Int32(th.PlayerId)
	return
}
func (th* dbPlayerFriendRecommendData)clone_to(d *dbPlayerFriendRecommendData){
	d.PlayerId = th.PlayerId
	return
}
type dbPlayerFriendAskData struct{
	PlayerId int32
}
func (th* dbPlayerFriendAskData)from_pb(pb *db.PlayerFriendAsk){
	if pb == nil {
		return
	}
	th.PlayerId = pb.GetPlayerId()
	return
}
func (th* dbPlayerFriendAskData)to_pb()(pb *db.PlayerFriendAsk){
	pb = &db.PlayerFriendAsk{}
	pb.PlayerId = proto.Int32(th.PlayerId)
	return
}
func (th* dbPlayerFriendAskData)clone_to(d *dbPlayerFriendAskData){
	d.PlayerId = th.PlayerId
	return
}
type dbPlayerFriendBossData struct{
	MonsterPos int32
	MonsterId int32
	MonsterHp int32
	MonsterMaxHp int32
}
func (th* dbPlayerFriendBossData)from_pb(pb *db.PlayerFriendBoss){
	if pb == nil {
		return
	}
	th.MonsterPos = pb.GetMonsterPos()
	th.MonsterId = pb.GetMonsterId()
	th.MonsterHp = pb.GetMonsterHp()
	th.MonsterMaxHp = pb.GetMonsterMaxHp()
	return
}
func (th* dbPlayerFriendBossData)to_pb()(pb *db.PlayerFriendBoss){
	pb = &db.PlayerFriendBoss{}
	pb.MonsterPos = proto.Int32(th.MonsterPos)
	pb.MonsterId = proto.Int32(th.MonsterId)
	pb.MonsterHp = proto.Int32(th.MonsterHp)
	pb.MonsterMaxHp = proto.Int32(th.MonsterMaxHp)
	return
}
func (th* dbPlayerFriendBossData)clone_to(d *dbPlayerFriendBossData){
	d.MonsterPos = th.MonsterPos
	d.MonsterId = th.MonsterId
	d.MonsterHp = th.MonsterHp
	d.MonsterMaxHp = th.MonsterMaxHp
	return
}
type dbPlayerTaskCommonData struct{
	LastRefreshTime int32
}
func (th* dbPlayerTaskCommonData)from_pb(pb *db.PlayerTaskCommon){
	if pb == nil {
		return
	}
	th.LastRefreshTime = pb.GetLastRefreshTime()
	return
}
func (th* dbPlayerTaskCommonData)to_pb()(pb *db.PlayerTaskCommon){
	pb = &db.PlayerTaskCommon{}
	pb.LastRefreshTime = proto.Int32(th.LastRefreshTime)
	return
}
func (th* dbPlayerTaskCommonData)clone_to(d *dbPlayerTaskCommonData){
	d.LastRefreshTime = th.LastRefreshTime
	return
}
type dbPlayerTaskData struct{
	Id int32
	Value int32
	State int32
	Param int32
}
func (th* dbPlayerTaskData)from_pb(pb *db.PlayerTask){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.Value = pb.GetValue()
	th.State = pb.GetState()
	th.Param = pb.GetParam()
	return
}
func (th* dbPlayerTaskData)to_pb()(pb *db.PlayerTask){
	pb = &db.PlayerTask{}
	pb.Id = proto.Int32(th.Id)
	pb.Value = proto.Int32(th.Value)
	pb.State = proto.Int32(th.State)
	pb.Param = proto.Int32(th.Param)
	return
}
func (th* dbPlayerTaskData)clone_to(d *dbPlayerTaskData){
	d.Id = th.Id
	d.Value = th.Value
	d.State = th.State
	d.Param = th.Param
	return
}
type dbPlayerFinishedTaskData struct{
	Id int32
}
func (th* dbPlayerFinishedTaskData)from_pb(pb *db.PlayerFinishedTask){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	return
}
func (th* dbPlayerFinishedTaskData)to_pb()(pb *db.PlayerFinishedTask){
	pb = &db.PlayerFinishedTask{}
	pb.Id = proto.Int32(th.Id)
	return
}
func (th* dbPlayerFinishedTaskData)clone_to(d *dbPlayerFinishedTaskData){
	d.Id = th.Id
	return
}
type dbPlayerDailyTaskAllDailyData struct{
	CompleteTaskId int32
}
func (th* dbPlayerDailyTaskAllDailyData)from_pb(pb *db.PlayerDailyTaskAllDaily){
	if pb == nil {
		return
	}
	th.CompleteTaskId = pb.GetCompleteTaskId()
	return
}
func (th* dbPlayerDailyTaskAllDailyData)to_pb()(pb *db.PlayerDailyTaskAllDaily){
	pb = &db.PlayerDailyTaskAllDaily{}
	pb.CompleteTaskId = proto.Int32(th.CompleteTaskId)
	return
}
func (th* dbPlayerDailyTaskAllDailyData)clone_to(d *dbPlayerDailyTaskAllDailyData){
	d.CompleteTaskId = th.CompleteTaskId
	return
}
type dbPlayerExploreCommonData struct{
	LastRefreshTime int32
	CurrentId int32
}
func (th* dbPlayerExploreCommonData)from_pb(pb *db.PlayerExploreCommon){
	if pb == nil {
		return
	}
	th.LastRefreshTime = pb.GetLastRefreshTime()
	th.CurrentId = pb.GetCurrentId()
	return
}
func (th* dbPlayerExploreCommonData)to_pb()(pb *db.PlayerExploreCommon){
	pb = &db.PlayerExploreCommon{}
	pb.LastRefreshTime = proto.Int32(th.LastRefreshTime)
	pb.CurrentId = proto.Int32(th.CurrentId)
	return
}
func (th* dbPlayerExploreCommonData)clone_to(d *dbPlayerExploreCommonData){
	d.LastRefreshTime = th.LastRefreshTime
	d.CurrentId = th.CurrentId
	return
}
type dbPlayerExploreData struct{
	Id int32
	TaskId int32
	State int32
	RoleCampsCanSel []int32
	RoleTypesCanSel []int32
	RoleId4TaskTitle int32
	NameId4TaskTitle int32
	StartTime int32
	RoleIds []int32
	IsLock int32
	RandomRewards []int32
	RewardStageId int32
}
func (th* dbPlayerExploreData)from_pb(pb *db.PlayerExplore){
	if pb == nil {
		th.RoleCampsCanSel = make([]int32,0)
		th.RoleTypesCanSel = make([]int32,0)
		th.RoleIds = make([]int32,0)
		th.RandomRewards = make([]int32,0)
		return
	}
	th.Id = pb.GetId()
	th.TaskId = pb.GetTaskId()
	th.State = pb.GetState()
	th.RoleCampsCanSel = make([]int32,len(pb.GetRoleCampsCanSel()))
	for i, v := range pb.GetRoleCampsCanSel() {
		th.RoleCampsCanSel[i] = v
	}
	th.RoleTypesCanSel = make([]int32,len(pb.GetRoleTypesCanSel()))
	for i, v := range pb.GetRoleTypesCanSel() {
		th.RoleTypesCanSel[i] = v
	}
	th.RoleId4TaskTitle = pb.GetRoleId4TaskTitle()
	th.NameId4TaskTitle = pb.GetNameId4TaskTitle()
	th.StartTime = pb.GetStartTime()
	th.RoleIds = make([]int32,len(pb.GetRoleIds()))
	for i, v := range pb.GetRoleIds() {
		th.RoleIds[i] = v
	}
	th.IsLock = pb.GetIsLock()
	th.RandomRewards = make([]int32,len(pb.GetRandomRewards()))
	for i, v := range pb.GetRandomRewards() {
		th.RandomRewards[i] = v
	}
	th.RewardStageId = pb.GetRewardStageId()
	return
}
func (th* dbPlayerExploreData)to_pb()(pb *db.PlayerExplore){
	pb = &db.PlayerExplore{}
	pb.Id = proto.Int32(th.Id)
	pb.TaskId = proto.Int32(th.TaskId)
	pb.State = proto.Int32(th.State)
	pb.RoleCampsCanSel = make([]int32, len(th.RoleCampsCanSel))
	for i, v := range th.RoleCampsCanSel {
		pb.RoleCampsCanSel[i]=v
	}
	pb.RoleTypesCanSel = make([]int32, len(th.RoleTypesCanSel))
	for i, v := range th.RoleTypesCanSel {
		pb.RoleTypesCanSel[i]=v
	}
	pb.RoleId4TaskTitle = proto.Int32(th.RoleId4TaskTitle)
	pb.NameId4TaskTitle = proto.Int32(th.NameId4TaskTitle)
	pb.StartTime = proto.Int32(th.StartTime)
	pb.RoleIds = make([]int32, len(th.RoleIds))
	for i, v := range th.RoleIds {
		pb.RoleIds[i]=v
	}
	pb.IsLock = proto.Int32(th.IsLock)
	pb.RandomRewards = make([]int32, len(th.RandomRewards))
	for i, v := range th.RandomRewards {
		pb.RandomRewards[i]=v
	}
	pb.RewardStageId = proto.Int32(th.RewardStageId)
	return
}
func (th* dbPlayerExploreData)clone_to(d *dbPlayerExploreData){
	d.Id = th.Id
	d.TaskId = th.TaskId
	d.State = th.State
	d.RoleCampsCanSel = make([]int32, len(th.RoleCampsCanSel))
	for _ii, _vv := range th.RoleCampsCanSel {
		d.RoleCampsCanSel[_ii]=_vv
	}
	d.RoleTypesCanSel = make([]int32, len(th.RoleTypesCanSel))
	for _ii, _vv := range th.RoleTypesCanSel {
		d.RoleTypesCanSel[_ii]=_vv
	}
	d.RoleId4TaskTitle = th.RoleId4TaskTitle
	d.NameId4TaskTitle = th.NameId4TaskTitle
	d.StartTime = th.StartTime
	d.RoleIds = make([]int32, len(th.RoleIds))
	for _ii, _vv := range th.RoleIds {
		d.RoleIds[_ii]=_vv
	}
	d.IsLock = th.IsLock
	d.RandomRewards = make([]int32, len(th.RandomRewards))
	for _ii, _vv := range th.RandomRewards {
		d.RandomRewards[_ii]=_vv
	}
	d.RewardStageId = th.RewardStageId
	return
}
type dbPlayerExploreStoryData struct{
	TaskId int32
	State int32
	RoleCampsCanSel []int32
	RoleTypesCanSel []int32
	StartTime int32
	RoleIds []int32
	RandomRewards []int32
	RewardStageId int32
}
func (th* dbPlayerExploreStoryData)from_pb(pb *db.PlayerExploreStory){
	if pb == nil {
		th.RoleCampsCanSel = make([]int32,0)
		th.RoleTypesCanSel = make([]int32,0)
		th.RoleIds = make([]int32,0)
		th.RandomRewards = make([]int32,0)
		return
	}
	th.TaskId = pb.GetTaskId()
	th.State = pb.GetState()
	th.RoleCampsCanSel = make([]int32,len(pb.GetRoleCampsCanSel()))
	for i, v := range pb.GetRoleCampsCanSel() {
		th.RoleCampsCanSel[i] = v
	}
	th.RoleTypesCanSel = make([]int32,len(pb.GetRoleTypesCanSel()))
	for i, v := range pb.GetRoleTypesCanSel() {
		th.RoleTypesCanSel[i] = v
	}
	th.StartTime = pb.GetStartTime()
	th.RoleIds = make([]int32,len(pb.GetRoleIds()))
	for i, v := range pb.GetRoleIds() {
		th.RoleIds[i] = v
	}
	th.RandomRewards = make([]int32,len(pb.GetRandomRewards()))
	for i, v := range pb.GetRandomRewards() {
		th.RandomRewards[i] = v
	}
	th.RewardStageId = pb.GetRewardStageId()
	return
}
func (th* dbPlayerExploreStoryData)to_pb()(pb *db.PlayerExploreStory){
	pb = &db.PlayerExploreStory{}
	pb.TaskId = proto.Int32(th.TaskId)
	pb.State = proto.Int32(th.State)
	pb.RoleCampsCanSel = make([]int32, len(th.RoleCampsCanSel))
	for i, v := range th.RoleCampsCanSel {
		pb.RoleCampsCanSel[i]=v
	}
	pb.RoleTypesCanSel = make([]int32, len(th.RoleTypesCanSel))
	for i, v := range th.RoleTypesCanSel {
		pb.RoleTypesCanSel[i]=v
	}
	pb.StartTime = proto.Int32(th.StartTime)
	pb.RoleIds = make([]int32, len(th.RoleIds))
	for i, v := range th.RoleIds {
		pb.RoleIds[i]=v
	}
	pb.RandomRewards = make([]int32, len(th.RandomRewards))
	for i, v := range th.RandomRewards {
		pb.RandomRewards[i]=v
	}
	pb.RewardStageId = proto.Int32(th.RewardStageId)
	return
}
func (th* dbPlayerExploreStoryData)clone_to(d *dbPlayerExploreStoryData){
	d.TaskId = th.TaskId
	d.State = th.State
	d.RoleCampsCanSel = make([]int32, len(th.RoleCampsCanSel))
	for _ii, _vv := range th.RoleCampsCanSel {
		d.RoleCampsCanSel[_ii]=_vv
	}
	d.RoleTypesCanSel = make([]int32, len(th.RoleTypesCanSel))
	for _ii, _vv := range th.RoleTypesCanSel {
		d.RoleTypesCanSel[_ii]=_vv
	}
	d.StartTime = th.StartTime
	d.RoleIds = make([]int32, len(th.RoleIds))
	for _ii, _vv := range th.RoleIds {
		d.RoleIds[_ii]=_vv
	}
	d.RandomRewards = make([]int32, len(th.RandomRewards))
	for _ii, _vv := range th.RandomRewards {
		d.RandomRewards[_ii]=_vv
	}
	d.RewardStageId = th.RewardStageId
	return
}
type dbPlayerFriendChatUnreadIdData struct{
	FriendId int32
	MessageIds []int32
	CurrMessageId int32
}
func (th* dbPlayerFriendChatUnreadIdData)from_pb(pb *db.PlayerFriendChatUnreadId){
	if pb == nil {
		th.MessageIds = make([]int32,0)
		return
	}
	th.FriendId = pb.GetFriendId()
	th.MessageIds = make([]int32,len(pb.GetMessageIds()))
	for i, v := range pb.GetMessageIds() {
		th.MessageIds[i] = v
	}
	th.CurrMessageId = pb.GetCurrMessageId()
	return
}
func (th* dbPlayerFriendChatUnreadIdData)to_pb()(pb *db.PlayerFriendChatUnreadId){
	pb = &db.PlayerFriendChatUnreadId{}
	pb.FriendId = proto.Int32(th.FriendId)
	pb.MessageIds = make([]int32, len(th.MessageIds))
	for i, v := range th.MessageIds {
		pb.MessageIds[i]=v
	}
	pb.CurrMessageId = proto.Int32(th.CurrMessageId)
	return
}
func (th* dbPlayerFriendChatUnreadIdData)clone_to(d *dbPlayerFriendChatUnreadIdData){
	d.FriendId = th.FriendId
	d.MessageIds = make([]int32, len(th.MessageIds))
	for _ii, _vv := range th.MessageIds {
		d.MessageIds[_ii]=_vv
	}
	d.CurrMessageId = th.CurrMessageId
	return
}
type dbPlayerFriendChatUnreadMessageData struct{
	PlayerMessageId int64
	Message []byte
	SendTime int32
	IsRead int32
}
func (th* dbPlayerFriendChatUnreadMessageData)from_pb(pb *db.PlayerFriendChatUnreadMessage){
	if pb == nil {
		return
	}
	th.PlayerMessageId = pb.GetPlayerMessageId()
	th.Message = pb.GetMessage()
	th.SendTime = pb.GetSendTime()
	th.IsRead = pb.GetIsRead()
	return
}
func (th* dbPlayerFriendChatUnreadMessageData)to_pb()(pb *db.PlayerFriendChatUnreadMessage){
	pb = &db.PlayerFriendChatUnreadMessage{}
	pb.PlayerMessageId = proto.Int64(th.PlayerMessageId)
	pb.Message = th.Message
	pb.SendTime = proto.Int32(th.SendTime)
	pb.IsRead = proto.Int32(th.IsRead)
	return
}
func (th* dbPlayerFriendChatUnreadMessageData)clone_to(d *dbPlayerFriendChatUnreadMessageData){
	d.PlayerMessageId = th.PlayerMessageId
	d.Message = make([]byte, len(th.Message))
	for _ii, _vv := range th.Message {
		d.Message[_ii]=_vv
	}
	d.SendTime = th.SendTime
	d.IsRead = th.IsRead
	return
}
type dbPlayerHeadItemData struct{
	Id int32
}
func (th* dbPlayerHeadItemData)from_pb(pb *db.PlayerHeadItem){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	return
}
func (th* dbPlayerHeadItemData)to_pb()(pb *db.PlayerHeadItem){
	pb = &db.PlayerHeadItem{}
	pb.Id = proto.Int32(th.Id)
	return
}
func (th* dbPlayerHeadItemData)clone_to(d *dbPlayerHeadItemData){
	d.Id = th.Id
	return
}
type dbPlayerSuitAwardData struct{
	Id int32
	AwardTime int32
}
func (th* dbPlayerSuitAwardData)from_pb(pb *db.PlayerSuitAward){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.AwardTime = pb.GetAwardTime()
	return
}
func (th* dbPlayerSuitAwardData)to_pb()(pb *db.PlayerSuitAward){
	pb = &db.PlayerSuitAward{}
	pb.Id = proto.Int32(th.Id)
	pb.AwardTime = proto.Int32(th.AwardTime)
	return
}
func (th* dbPlayerSuitAwardData)clone_to(d *dbPlayerSuitAwardData){
	d.Id = th.Id
	d.AwardTime = th.AwardTime
	return
}
type dbPlayerChatData struct{
	Channel int32
	LastChatTime int32
	LastPullTime int32
	LastMsgIndex int32
}
func (th* dbPlayerChatData)from_pb(pb *db.PlayerChat){
	if pb == nil {
		return
	}
	th.Channel = pb.GetChannel()
	th.LastChatTime = pb.GetLastChatTime()
	th.LastPullTime = pb.GetLastPullTime()
	th.LastMsgIndex = pb.GetLastMsgIndex()
	return
}
func (th* dbPlayerChatData)to_pb()(pb *db.PlayerChat){
	pb = &db.PlayerChat{}
	pb.Channel = proto.Int32(th.Channel)
	pb.LastChatTime = proto.Int32(th.LastChatTime)
	pb.LastPullTime = proto.Int32(th.LastPullTime)
	pb.LastMsgIndex = proto.Int32(th.LastMsgIndex)
	return
}
func (th* dbPlayerChatData)clone_to(d *dbPlayerChatData){
	d.Channel = th.Channel
	d.LastChatTime = th.LastChatTime
	d.LastPullTime = th.LastPullTime
	d.LastMsgIndex = th.LastMsgIndex
	return
}
type dbPlayerAnouncementData struct{
	LastSendTime int32
}
func (th* dbPlayerAnouncementData)from_pb(pb *db.PlayerAnouncement){
	if pb == nil {
		return
	}
	th.LastSendTime = pb.GetLastSendTime()
	return
}
func (th* dbPlayerAnouncementData)to_pb()(pb *db.PlayerAnouncement){
	pb = &db.PlayerAnouncement{}
	pb.LastSendTime = proto.Int32(th.LastSendTime)
	return
}
func (th* dbPlayerAnouncementData)clone_to(d *dbPlayerAnouncementData){
	d.LastSendTime = th.LastSendTime
	return
}
type dbPlayerFirstDrawCardData struct{
	Id int32
	Drawed int32
}
func (th* dbPlayerFirstDrawCardData)from_pb(pb *db.PlayerFirstDrawCard){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.Drawed = pb.GetDrawed()
	return
}
func (th* dbPlayerFirstDrawCardData)to_pb()(pb *db.PlayerFirstDrawCard){
	pb = &db.PlayerFirstDrawCard{}
	pb.Id = proto.Int32(th.Id)
	pb.Drawed = proto.Int32(th.Drawed)
	return
}
func (th* dbPlayerFirstDrawCardData)clone_to(d *dbPlayerFirstDrawCardData){
	d.Id = th.Id
	d.Drawed = th.Drawed
	return
}
type dbPlayerGuildData struct{
	Id int32
	JoinTime int32
	QuitTime int32
	SignTime int32
	Position int32
	DonateNum int32
	LastAskDonateTime int32
	LastDonateTime int32
}
func (th* dbPlayerGuildData)from_pb(pb *db.PlayerGuild){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.JoinTime = pb.GetJoinTime()
	th.QuitTime = pb.GetQuitTime()
	th.SignTime = pb.GetSignTime()
	th.Position = pb.GetPosition()
	th.DonateNum = pb.GetDonateNum()
	th.LastAskDonateTime = pb.GetLastAskDonateTime()
	th.LastDonateTime = pb.GetLastDonateTime()
	return
}
func (th* dbPlayerGuildData)to_pb()(pb *db.PlayerGuild){
	pb = &db.PlayerGuild{}
	pb.Id = proto.Int32(th.Id)
	pb.JoinTime = proto.Int32(th.JoinTime)
	pb.QuitTime = proto.Int32(th.QuitTime)
	pb.SignTime = proto.Int32(th.SignTime)
	pb.Position = proto.Int32(th.Position)
	pb.DonateNum = proto.Int32(th.DonateNum)
	pb.LastAskDonateTime = proto.Int32(th.LastAskDonateTime)
	pb.LastDonateTime = proto.Int32(th.LastDonateTime)
	return
}
func (th* dbPlayerGuildData)clone_to(d *dbPlayerGuildData){
	d.Id = th.Id
	d.JoinTime = th.JoinTime
	d.QuitTime = th.QuitTime
	d.SignTime = th.SignTime
	d.Position = th.Position
	d.DonateNum = th.DonateNum
	d.LastAskDonateTime = th.LastAskDonateTime
	d.LastDonateTime = th.LastDonateTime
	return
}
type dbPlayerGuildStageData struct{
	RespawnNum int32
	RespawnState int32
	LastRefreshTime int32
}
func (th* dbPlayerGuildStageData)from_pb(pb *db.PlayerGuildStage){
	if pb == nil {
		return
	}
	th.RespawnNum = pb.GetRespawnNum()
	th.RespawnState = pb.GetRespawnState()
	th.LastRefreshTime = pb.GetLastRefreshTime()
	return
}
func (th* dbPlayerGuildStageData)to_pb()(pb *db.PlayerGuildStage){
	pb = &db.PlayerGuildStage{}
	pb.RespawnNum = proto.Int32(th.RespawnNum)
	pb.RespawnState = proto.Int32(th.RespawnState)
	pb.LastRefreshTime = proto.Int32(th.LastRefreshTime)
	return
}
func (th* dbPlayerGuildStageData)clone_to(d *dbPlayerGuildStageData){
	d.RespawnNum = th.RespawnNum
	d.RespawnState = th.RespawnState
	d.LastRefreshTime = th.LastRefreshTime
	return
}
type dbPlayerSignData struct{
	CurrGroup int32
	AwardIndex int32
	SignedIndex int32
	LastSignedTime int32
}
func (th* dbPlayerSignData)from_pb(pb *db.PlayerSign){
	if pb == nil {
		return
	}
	th.CurrGroup = pb.GetCurrGroup()
	th.AwardIndex = pb.GetAwardIndex()
	th.SignedIndex = pb.GetSignedIndex()
	th.LastSignedTime = pb.GetLastSignedTime()
	return
}
func (th* dbPlayerSignData)to_pb()(pb *db.PlayerSign){
	pb = &db.PlayerSign{}
	pb.CurrGroup = proto.Int32(th.CurrGroup)
	pb.AwardIndex = proto.Int32(th.AwardIndex)
	pb.SignedIndex = proto.Int32(th.SignedIndex)
	pb.LastSignedTime = proto.Int32(th.LastSignedTime)
	return
}
func (th* dbPlayerSignData)clone_to(d *dbPlayerSignData){
	d.CurrGroup = th.CurrGroup
	d.AwardIndex = th.AwardIndex
	d.SignedIndex = th.SignedIndex
	d.LastSignedTime = th.LastSignedTime
	return
}
type dbPlayerSevenDaysData struct{
	Awards []int32
	Days int32
}
func (th* dbPlayerSevenDaysData)from_pb(pb *db.PlayerSevenDays){
	if pb == nil {
		th.Awards = make([]int32,0)
		return
	}
	th.Awards = make([]int32,len(pb.GetAwards()))
	for i, v := range pb.GetAwards() {
		th.Awards[i] = v
	}
	th.Days = pb.GetDays()
	return
}
func (th* dbPlayerSevenDaysData)to_pb()(pb *db.PlayerSevenDays){
	pb = &db.PlayerSevenDays{}
	pb.Awards = make([]int32, len(th.Awards))
	for i, v := range th.Awards {
		pb.Awards[i]=v
	}
	pb.Days = proto.Int32(th.Days)
	return
}
func (th* dbPlayerSevenDaysData)clone_to(d *dbPlayerSevenDaysData){
	d.Awards = make([]int32, len(th.Awards))
	for _ii, _vv := range th.Awards {
		d.Awards[_ii]=_vv
	}
	d.Days = th.Days
	return
}
type dbPlayerPayCommonData struct{
	FirstPayState int32
}
func (th* dbPlayerPayCommonData)from_pb(pb *db.PlayerPayCommon){
	if pb == nil {
		return
	}
	th.FirstPayState = pb.GetFirstPayState()
	return
}
func (th* dbPlayerPayCommonData)to_pb()(pb *db.PlayerPayCommon){
	pb = &db.PlayerPayCommon{}
	pb.FirstPayState = proto.Int32(th.FirstPayState)
	return
}
func (th* dbPlayerPayCommonData)clone_to(d *dbPlayerPayCommonData){
	d.FirstPayState = th.FirstPayState
	return
}
type dbPlayerPayData struct{
	BundleId string
	LastPayedTime int32
	LastAwardTime int32
	SendMailNum int32
	ChargeNum int32
}
func (th* dbPlayerPayData)from_pb(pb *db.PlayerPay){
	if pb == nil {
		return
	}
	th.BundleId = pb.GetBundleId()
	th.LastPayedTime = pb.GetLastPayedTime()
	th.LastAwardTime = pb.GetLastAwardTime()
	th.SendMailNum = pb.GetSendMailNum()
	th.ChargeNum = pb.GetChargeNum()
	return
}
func (th* dbPlayerPayData)to_pb()(pb *db.PlayerPay){
	pb = &db.PlayerPay{}
	pb.BundleId = proto.String(th.BundleId)
	pb.LastPayedTime = proto.Int32(th.LastPayedTime)
	pb.LastAwardTime = proto.Int32(th.LastAwardTime)
	pb.SendMailNum = proto.Int32(th.SendMailNum)
	pb.ChargeNum = proto.Int32(th.ChargeNum)
	return
}
func (th* dbPlayerPayData)clone_to(d *dbPlayerPayData){
	d.BundleId = th.BundleId
	d.LastPayedTime = th.LastPayedTime
	d.LastAwardTime = th.LastAwardTime
	d.SendMailNum = th.SendMailNum
	d.ChargeNum = th.ChargeNum
	return
}
type dbPlayerGuideDataData struct{
	Data []byte
}
func (th* dbPlayerGuideDataData)from_pb(pb *db.PlayerGuideData){
	if pb == nil {
		return
	}
	th.Data = pb.GetData()
	return
}
func (th* dbPlayerGuideDataData)to_pb()(pb *db.PlayerGuideData){
	pb = &db.PlayerGuideData{}
	pb.Data = th.Data
	return
}
func (th* dbPlayerGuideDataData)clone_to(d *dbPlayerGuideDataData){
	d.Data = make([]byte, len(th.Data))
	for _ii, _vv := range th.Data {
		d.Data[_ii]=_vv
	}
	return
}
type dbPlayerActivityDataData struct{
	Id int32
	SubIds []int32
	SubValues []int32
	SubNum int32
}
func (th* dbPlayerActivityDataData)from_pb(pb *db.PlayerActivityData){
	if pb == nil {
		th.SubIds = make([]int32,0)
		th.SubValues = make([]int32,0)
		return
	}
	th.Id = pb.GetId()
	th.SubIds = make([]int32,len(pb.GetSubIds()))
	for i, v := range pb.GetSubIds() {
		th.SubIds[i] = v
	}
	th.SubValues = make([]int32,len(pb.GetSubValues()))
	for i, v := range pb.GetSubValues() {
		th.SubValues[i] = v
	}
	th.SubNum = pb.GetSubNum()
	return
}
func (th* dbPlayerActivityDataData)to_pb()(pb *db.PlayerActivityData){
	pb = &db.PlayerActivityData{}
	pb.Id = proto.Int32(th.Id)
	pb.SubIds = make([]int32, len(th.SubIds))
	for i, v := range th.SubIds {
		pb.SubIds[i]=v
	}
	pb.SubValues = make([]int32, len(th.SubValues))
	for i, v := range th.SubValues {
		pb.SubValues[i]=v
	}
	pb.SubNum = proto.Int32(th.SubNum)
	return
}
func (th* dbPlayerActivityDataData)clone_to(d *dbPlayerActivityDataData){
	d.Id = th.Id
	d.SubIds = make([]int32, len(th.SubIds))
	for _ii, _vv := range th.SubIds {
		d.SubIds[_ii]=_vv
	}
	d.SubValues = make([]int32, len(th.SubValues))
	for _ii, _vv := range th.SubValues {
		d.SubValues[_ii]=_vv
	}
	d.SubNum = th.SubNum
	return
}
type dbPlayerExpeditionDataData struct{
	RefreshTime int32
	CurrLevel int32
	PurifyPoints int32
}
func (th* dbPlayerExpeditionDataData)from_pb(pb *db.PlayerExpeditionData){
	if pb == nil {
		return
	}
	th.RefreshTime = pb.GetRefreshTime()
	th.CurrLevel = pb.GetCurrLevel()
	th.PurifyPoints = pb.GetPurifyPoints()
	return
}
func (th* dbPlayerExpeditionDataData)to_pb()(pb *db.PlayerExpeditionData){
	pb = &db.PlayerExpeditionData{}
	pb.RefreshTime = proto.Int32(th.RefreshTime)
	pb.CurrLevel = proto.Int32(th.CurrLevel)
	pb.PurifyPoints = proto.Int32(th.PurifyPoints)
	return
}
func (th* dbPlayerExpeditionDataData)clone_to(d *dbPlayerExpeditionDataData){
	d.RefreshTime = th.RefreshTime
	d.CurrLevel = th.CurrLevel
	d.PurifyPoints = th.PurifyPoints
	return
}
type dbPlayerExpeditionRoleData struct{
	Id int32
	HP int32
	Weak int32
	HpPercent int32
}
func (th* dbPlayerExpeditionRoleData)from_pb(pb *db.PlayerExpeditionRole){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.HP = pb.GetHP()
	th.Weak = pb.GetWeak()
	th.HpPercent = pb.GetHpPercent()
	return
}
func (th* dbPlayerExpeditionRoleData)to_pb()(pb *db.PlayerExpeditionRole){
	pb = &db.PlayerExpeditionRole{}
	pb.Id = proto.Int32(th.Id)
	pb.HP = proto.Int32(th.HP)
	pb.Weak = proto.Int32(th.Weak)
	pb.HpPercent = proto.Int32(th.HpPercent)
	return
}
func (th* dbPlayerExpeditionRoleData)clone_to(d *dbPlayerExpeditionRoleData){
	d.Id = th.Id
	d.HP = th.HP
	d.Weak = th.Weak
	d.HpPercent = th.HpPercent
	return
}
type dbPlayerExpeditionLevelData struct{
	Level int32
	PlayerId int32
	Power int32
	GoldIncome int32
	ExpeditionGoldIncome int32
}
func (th* dbPlayerExpeditionLevelData)from_pb(pb *db.PlayerExpeditionLevel){
	if pb == nil {
		return
	}
	th.Level = pb.GetLevel()
	th.PlayerId = pb.GetPlayerId()
	th.Power = pb.GetPower()
	th.GoldIncome = pb.GetGoldIncome()
	th.ExpeditionGoldIncome = pb.GetExpeditionGoldIncome()
	return
}
func (th* dbPlayerExpeditionLevelData)to_pb()(pb *db.PlayerExpeditionLevel){
	pb = &db.PlayerExpeditionLevel{}
	pb.Level = proto.Int32(th.Level)
	pb.PlayerId = proto.Int32(th.PlayerId)
	pb.Power = proto.Int32(th.Power)
	pb.GoldIncome = proto.Int32(th.GoldIncome)
	pb.ExpeditionGoldIncome = proto.Int32(th.ExpeditionGoldIncome)
	return
}
func (th* dbPlayerExpeditionLevelData)clone_to(d *dbPlayerExpeditionLevelData){
	d.Level = th.Level
	d.PlayerId = th.PlayerId
	d.Power = th.Power
	d.GoldIncome = th.GoldIncome
	d.ExpeditionGoldIncome = th.ExpeditionGoldIncome
	return
}
type dbPlayerExpeditionLevelRoleData struct{
	Pos int32
	TableId int32
	Rank int32
	Level int32
	Equip []int32
	HP int32
	HpPercent int32
}
func (th* dbPlayerExpeditionLevelRoleData)from_pb(pb *db.PlayerExpeditionLevelRole){
	if pb == nil {
		th.Equip = make([]int32,0)
		return
	}
	th.Pos = pb.GetPos()
	th.TableId = pb.GetTableId()
	th.Rank = pb.GetRank()
	th.Level = pb.GetLevel()
	th.Equip = make([]int32,len(pb.GetEquip()))
	for i, v := range pb.GetEquip() {
		th.Equip[i] = v
	}
	th.HP = pb.GetHP()
	th.HpPercent = pb.GetHpPercent()
	return
}
func (th* dbPlayerExpeditionLevelRoleData)to_pb()(pb *db.PlayerExpeditionLevelRole){
	pb = &db.PlayerExpeditionLevelRole{}
	pb.Pos = proto.Int32(th.Pos)
	pb.TableId = proto.Int32(th.TableId)
	pb.Rank = proto.Int32(th.Rank)
	pb.Level = proto.Int32(th.Level)
	pb.Equip = make([]int32, len(th.Equip))
	for i, v := range th.Equip {
		pb.Equip[i]=v
	}
	pb.HP = proto.Int32(th.HP)
	pb.HpPercent = proto.Int32(th.HpPercent)
	return
}
func (th* dbPlayerExpeditionLevelRoleData)clone_to(d *dbPlayerExpeditionLevelRoleData){
	d.Pos = th.Pos
	d.TableId = th.TableId
	d.Rank = th.Rank
	d.Level = th.Level
	d.Equip = make([]int32, len(th.Equip))
	for _ii, _vv := range th.Equip {
		d.Equip[_ii]=_vv
	}
	d.HP = th.HP
	d.HpPercent = th.HpPercent
	return
}
type dbPlayerSysMailData struct{
	CurrId int32
}
func (th* dbPlayerSysMailData)from_pb(pb *db.PlayerSysMail){
	if pb == nil {
		return
	}
	th.CurrId = pb.GetCurrId()
	return
}
func (th* dbPlayerSysMailData)to_pb()(pb *db.PlayerSysMail){
	pb = &db.PlayerSysMail{}
	pb.CurrId = proto.Int32(th.CurrId)
	return
}
func (th* dbPlayerSysMailData)clone_to(d *dbPlayerSysMailData){
	d.CurrId = th.CurrId
	return
}
type dbPlayerArtifactData struct{
	Id int32
	Rank int32
	Level int32
}
func (th* dbPlayerArtifactData)from_pb(pb *db.PlayerArtifact){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.Rank = pb.GetRank()
	th.Level = pb.GetLevel()
	return
}
func (th* dbPlayerArtifactData)to_pb()(pb *db.PlayerArtifact){
	pb = &db.PlayerArtifact{}
	pb.Id = proto.Int32(th.Id)
	pb.Rank = proto.Int32(th.Rank)
	pb.Level = proto.Int32(th.Level)
	return
}
func (th* dbPlayerArtifactData)clone_to(d *dbPlayerArtifactData){
	d.Id = th.Id
	d.Rank = th.Rank
	d.Level = th.Level
	return
}
type dbPlayerCarnivalCommonData struct{
	DayResetTime int32
}
func (th* dbPlayerCarnivalCommonData)from_pb(pb *db.PlayerCarnivalCommon){
	if pb == nil {
		return
	}
	th.DayResetTime = pb.GetDayResetTime()
	return
}
func (th* dbPlayerCarnivalCommonData)to_pb()(pb *db.PlayerCarnivalCommon){
	pb = &db.PlayerCarnivalCommon{}
	pb.DayResetTime = proto.Int32(th.DayResetTime)
	return
}
func (th* dbPlayerCarnivalCommonData)clone_to(d *dbPlayerCarnivalCommonData){
	d.DayResetTime = th.DayResetTime
	return
}
type dbPlayerCarnivalData struct{
	Id int32
	Value int32
	Value2 int32
}
func (th* dbPlayerCarnivalData)from_pb(pb *db.PlayerCarnival){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.Value = pb.GetValue()
	th.Value2 = pb.GetValue2()
	return
}
func (th* dbPlayerCarnivalData)to_pb()(pb *db.PlayerCarnival){
	pb = &db.PlayerCarnival{}
	pb.Id = proto.Int32(th.Id)
	pb.Value = proto.Int32(th.Value)
	pb.Value2 = proto.Int32(th.Value2)
	return
}
func (th* dbPlayerCarnivalData)clone_to(d *dbPlayerCarnivalData){
	d.Id = th.Id
	d.Value = th.Value
	d.Value2 = th.Value2
	return
}
type dbPlayerInviteCodesData struct{
	Code string
}
func (th* dbPlayerInviteCodesData)from_pb(pb *db.PlayerInviteCodes){
	if pb == nil {
		return
	}
	th.Code = pb.GetCode()
	return
}
func (th* dbPlayerInviteCodesData)to_pb()(pb *db.PlayerInviteCodes){
	pb = &db.PlayerInviteCodes{}
	pb.Code = proto.String(th.Code)
	return
}
func (th* dbPlayerInviteCodesData)clone_to(d *dbPlayerInviteCodesData){
	d.Code = th.Code
	return
}
type dbBattleSaveDataData struct{
	Data []byte
}
func (th* dbBattleSaveDataData)from_pb(pb *db.BattleSaveData){
	if pb == nil {
		return
	}
	th.Data = pb.GetData()
	return
}
func (th* dbBattleSaveDataData)to_pb()(pb *db.BattleSaveData){
	pb = &db.BattleSaveData{}
	pb.Data = th.Data
	return
}
func (th* dbBattleSaveDataData)clone_to(d *dbBattleSaveDataData){
	d.Data = make([]byte, len(th.Data))
	for _ii, _vv := range th.Data {
		d.Data[_ii]=_vv
	}
	return
}
type dbTowerFightSaveDataData struct{
	Data []byte
}
func (th* dbTowerFightSaveDataData)from_pb(pb *db.TowerFightSaveData){
	if pb == nil {
		return
	}
	th.Data = pb.GetData()
	return
}
func (th* dbTowerFightSaveDataData)to_pb()(pb *db.TowerFightSaveData){
	pb = &db.TowerFightSaveData{}
	pb.Data = th.Data
	return
}
func (th* dbTowerFightSaveDataData)clone_to(d *dbTowerFightSaveDataData){
	d.Data = make([]byte, len(th.Data))
	for _ii, _vv := range th.Data {
		d.Data[_ii]=_vv
	}
	return
}
type dbArenaSeasonDataData struct{
	LastDayResetTime int32
	LastSeasonResetTime int32
}
func (th* dbArenaSeasonDataData)from_pb(pb *db.ArenaSeasonData){
	if pb == nil {
		return
	}
	th.LastDayResetTime = pb.GetLastDayResetTime()
	th.LastSeasonResetTime = pb.GetLastSeasonResetTime()
	return
}
func (th* dbArenaSeasonDataData)to_pb()(pb *db.ArenaSeasonData){
	pb = &db.ArenaSeasonData{}
	pb.LastDayResetTime = proto.Int32(th.LastDayResetTime)
	pb.LastSeasonResetTime = proto.Int32(th.LastSeasonResetTime)
	return
}
func (th* dbArenaSeasonDataData)clone_to(d *dbArenaSeasonDataData){
	d.LastDayResetTime = th.LastDayResetTime
	d.LastSeasonResetTime = th.LastSeasonResetTime
	return
}
type dbGuildMemberData struct{
	PlayerId int32
}
func (th* dbGuildMemberData)from_pb(pb *db.GuildMember){
	if pb == nil {
		return
	}
	th.PlayerId = pb.GetPlayerId()
	return
}
func (th* dbGuildMemberData)to_pb()(pb *db.GuildMember){
	pb = &db.GuildMember{}
	pb.PlayerId = proto.Int32(th.PlayerId)
	return
}
func (th* dbGuildMemberData)clone_to(d *dbGuildMemberData){
	d.PlayerId = th.PlayerId
	return
}
type dbGuildAskListData struct{
	PlayerId int32
}
func (th* dbGuildAskListData)from_pb(pb *db.GuildAskList){
	if pb == nil {
		return
	}
	th.PlayerId = pb.GetPlayerId()
	return
}
func (th* dbGuildAskListData)to_pb()(pb *db.GuildAskList){
	pb = &db.GuildAskList{}
	pb.PlayerId = proto.Int32(th.PlayerId)
	return
}
func (th* dbGuildAskListData)clone_to(d *dbGuildAskListData){
	d.PlayerId = th.PlayerId
	return
}
type dbGuildLogData struct{
	Id int32
	LogType int32
	PlayerId int32
	Time int32
}
func (th* dbGuildLogData)from_pb(pb *db.GuildLog){
	if pb == nil {
		return
	}
	th.Id = pb.GetId()
	th.LogType = pb.GetLogType()
	th.PlayerId = pb.GetPlayerId()
	th.Time = pb.GetTime()
	return
}
func (th* dbGuildLogData)to_pb()(pb *db.GuildLog){
	pb = &db.GuildLog{}
	pb.Id = proto.Int32(th.Id)
	pb.LogType = proto.Int32(th.LogType)
	pb.PlayerId = proto.Int32(th.PlayerId)
	pb.Time = proto.Int32(th.Time)
	return
}
func (th* dbGuildLogData)clone_to(d *dbGuildLogData){
	d.Id = th.Id
	d.LogType = th.LogType
	d.PlayerId = th.PlayerId
	d.Time = th.Time
	return
}
type dbGuildAskDonateData struct{
	PlayerId int32
	ItemId int32
	ItemNum int32
	AskTime int32
}
func (th* dbGuildAskDonateData)from_pb(pb *db.GuildAskDonate){
	if pb == nil {
		return
	}
	th.PlayerId = pb.GetPlayerId()
	th.ItemId = pb.GetItemId()
	th.ItemNum = pb.GetItemNum()
	th.AskTime = pb.GetAskTime()
	return
}
func (th* dbGuildAskDonateData)to_pb()(pb *db.GuildAskDonate){
	pb = &db.GuildAskDonate{}
	pb.PlayerId = proto.Int32(th.PlayerId)
	pb.ItemId = proto.Int32(th.ItemId)
	pb.ItemNum = proto.Int32(th.ItemNum)
	pb.AskTime = proto.Int32(th.AskTime)
	return
}
func (th* dbGuildAskDonateData)clone_to(d *dbGuildAskDonateData){
	d.PlayerId = th.PlayerId
	d.ItemId = th.ItemId
	d.ItemNum = th.ItemNum
	d.AskTime = th.AskTime
	return
}
type dbGuildStageData struct{
	BossId int32
	HpPercent int32
	BossPos int32
	BossHP int32
}
func (th* dbGuildStageData)from_pb(pb *db.GuildStage){
	if pb == nil {
		return
	}
	th.BossId = pb.GetBossId()
	th.HpPercent = pb.GetHpPercent()
	th.BossPos = pb.GetBossPos()
	th.BossHP = pb.GetBossHP()
	return
}
func (th* dbGuildStageData)to_pb()(pb *db.GuildStage){
	pb = &db.GuildStage{}
	pb.BossId = proto.Int32(th.BossId)
	pb.HpPercent = proto.Int32(th.HpPercent)
	pb.BossPos = proto.Int32(th.BossPos)
	pb.BossHP = proto.Int32(th.BossHP)
	return
}
func (th* dbGuildStageData)clone_to(d *dbGuildStageData){
	d.BossId = th.BossId
	d.HpPercent = th.HpPercent
	d.BossPos = th.BossPos
	d.BossHP = th.BossHP
	return
}
type dbGuildStageDamageLogData struct{
	AttackerId int32
	Damage int32
}
func (th* dbGuildStageDamageLogData)from_pb(pb *db.GuildStageDamageLog){
	if pb == nil {
		return
	}
	th.AttackerId = pb.GetAttackerId()
	th.Damage = pb.GetDamage()
	return
}
func (th* dbGuildStageDamageLogData)to_pb()(pb *db.GuildStageDamageLog){
	pb = &db.GuildStageDamageLog{}
	pb.AttackerId = proto.Int32(th.AttackerId)
	pb.Damage = proto.Int32(th.Damage)
	return
}
func (th* dbGuildStageDamageLogData)clone_to(d *dbGuildStageDamageLogData){
	d.AttackerId = th.AttackerId
	d.Damage = th.Damage
	return
}
type dbSysMailAttachedItemsData struct{
	ItemList []int32
}
func (th* dbSysMailAttachedItemsData)from_pb(pb *db.SysMailAttachedItems){
	if pb == nil {
		th.ItemList = make([]int32,0)
		return
	}
	th.ItemList = make([]int32,len(pb.GetItemList()))
	for i, v := range pb.GetItemList() {
		th.ItemList[i] = v
	}
	return
}
func (th* dbSysMailAttachedItemsData)to_pb()(pb *db.SysMailAttachedItems){
	pb = &db.SysMailAttachedItems{}
	pb.ItemList = make([]int32, len(th.ItemList))
	for i, v := range th.ItemList {
		pb.ItemList[i]=v
	}
	return
}
func (th* dbSysMailAttachedItemsData)clone_to(d *dbSysMailAttachedItemsData){
	d.ItemList = make([]int32, len(th.ItemList))
	for _ii, _vv := range th.ItemList {
		d.ItemList[_ii]=_vv
	}
	return
}

func (th *dbGlobalRow)GetCurrentPlayerId( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGlobalRow.GetdbGlobalCurrentPlayerIdColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_CurrentPlayerId)
}
func (th *dbGlobalRow)SetCurrentPlayerId(v int32){
	th.m_lock.UnSafeLock("dbGlobalRow.SetdbGlobalCurrentPlayerIdColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_CurrentPlayerId=int32(v)
	th.m_CurrentPlayerId_changed=true
	return
}
func (th *dbGlobalRow)GetCurrentGuildId( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGlobalRow.GetdbGlobalCurrentGuildIdColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_CurrentGuildId)
}
func (th *dbGlobalRow)SetCurrentGuildId(v int32){
	th.m_lock.UnSafeLock("dbGlobalRow.SetdbGlobalCurrentGuildIdColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_CurrentGuildId=int32(v)
	th.m_CurrentGuildId_changed=true
	return
}
type dbGlobalRow struct {
	m_table *dbGlobalTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_Id        int32
	m_CurrentPlayerId_changed bool
	m_CurrentPlayerId int32
	m_CurrentGuildId_changed bool
	m_CurrentGuildId int32
}
func new_dbGlobalRow(table *dbGlobalTable, Id int32) (r *dbGlobalRow) {
	th := &dbGlobalRow{}
	th.m_table = table
	th.m_Id = Id
	th.m_lock = NewRWMutex()
	th.m_CurrentPlayerId_changed=true
	th.m_CurrentGuildId_changed=true
	return th
}
func (th *dbGlobalRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbGlobalRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(3)
		db_args.Push(th.m_Id)
		db_args.Push(th.m_CurrentPlayerId)
		db_args.Push(th.m_CurrentGuildId)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.m_CurrentPlayerId_changed||th.m_CurrentGuildId_changed{
			update_string = "UPDATE Global SET "
			db_args:=new_db_args(3)
			if th.m_CurrentPlayerId_changed{
				update_string+="CurrentPlayerId=?,"
				db_args.Push(th.m_CurrentPlayerId)
			}
			if th.m_CurrentGuildId_changed{
				update_string+="CurrentGuildId=?,"
				db_args.Push(th.m_CurrentGuildId)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE Id=?"
			db_args.Push(th.m_Id)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.m_CurrentPlayerId_changed = false
	th.m_CurrentGuildId_changed = false
	if release && th.m_loaded {
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbGlobalRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT Global exec failed %v ", th.m_Id)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE Global exec failed %v", th.m_Id)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
type dbGlobalTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_row *dbGlobalRow
	m_preload_select_stmt *sql.Stmt
	m_save_insert_stmt *sql.Stmt
}
func new_dbGlobalTable(dbc *DBC) (th *dbGlobalTable) {
	th = &dbGlobalTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	return th
}
func (th *dbGlobalTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS Global(Id int(11),PRIMARY KEY (Id))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS Global failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='Global'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasCurrentPlayerId := columns["CurrentPlayerId"]
	if !hasCurrentPlayerId {
		_, err = th.m_dbc.Exec("ALTER TABLE Global ADD COLUMN CurrentPlayerId int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN CurrentPlayerId failed")
			return
		}
	}
	_, hasCurrentGuildId := columns["CurrentGuildId"]
	if !hasCurrentGuildId {
		_, err = th.m_dbc.Exec("ALTER TABLE Global ADD COLUMN CurrentGuildId int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN CurrentGuildId failed")
			return
		}
	}
	return
}
func (th *dbGlobalTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT CurrentPlayerId,CurrentGuildId FROM Global WHERE Id=0")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbGlobalTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO Global (Id,CurrentPlayerId,CurrentGuildId) VALUES (?,?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbGlobalTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbGlobalTable) Preload() (err error) {
	r := th.m_dbc.StmtQueryRow(th.m_preload_select_stmt)
	var dCurrentPlayerId int32
	var dCurrentGuildId int32
	err = r.Scan(&dCurrentPlayerId,&dCurrentGuildId)
	if err!=nil{
		if err!=sql.ErrNoRows{
			log.Error("Scan failed")
			return
		}
	}else{
		row := new_dbGlobalRow(th,0)
		row.m_CurrentPlayerId=dCurrentPlayerId
		row.m_CurrentGuildId=dCurrentGuildId
		row.m_CurrentPlayerId_changed=false
		row.m_CurrentGuildId_changed=false
		row.m_valid = true
		row.m_loaded=true
		th.m_row=row
	}
	if th.m_row == nil {
		th.m_row = new_dbGlobalRow(th, 0)
		th.m_row.m_new = true
		th.m_row.m_valid = true
		err = th.Save(false)
		if err != nil {
			log.Error("save failed")
			return
		}
		th.m_row.m_loaded = true
	}
	return
}
func (th *dbGlobalTable) Save(quick bool) (err error) {
	if th.m_row==nil{
		return errors.New("row nil")
	}
	err, _, _ = th.m_row.Save(false)
	return err
}
func (th *dbGlobalTable) GetRow( ) (row *dbGlobalRow) {
	return th.m_row
}
func (th *dbPlayerRow)GetUniqueId( )(r string ){
	th.m_lock.UnSafeRLock("dbPlayerRow.GetdbPlayerUniqueIdColumn")
	defer th.m_lock.UnSafeRUnlock()
	return string(th.m_UniqueId)
}
func (th *dbPlayerRow)SetUniqueId(v string){
	th.m_lock.UnSafeLock("dbPlayerRow.SetdbPlayerUniqueIdColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_UniqueId=string(v)
	th.m_UniqueId_changed=true
	return
}
func (th *dbPlayerRow)GetAccount( )(r string ){
	th.m_lock.UnSafeRLock("dbPlayerRow.GetdbPlayerAccountColumn")
	defer th.m_lock.UnSafeRUnlock()
	return string(th.m_Account)
}
func (th *dbPlayerRow)SetAccount(v string){
	th.m_lock.UnSafeLock("dbPlayerRow.SetdbPlayerAccountColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Account=string(v)
	th.m_Account_changed=true
	return
}
func (th *dbPlayerRow)GetName( )(r string ){
	th.m_lock.UnSafeRLock("dbPlayerRow.GetdbPlayerNameColumn")
	defer th.m_lock.UnSafeRUnlock()
	return string(th.m_Name)
}
func (th *dbPlayerRow)SetName(v string){
	th.m_lock.UnSafeLock("dbPlayerRow.SetdbPlayerNameColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Name=string(v)
	th.m_Name_changed=true
	return
}
func (th *dbPlayerRow)GetToken( )(r string ){
	th.m_lock.UnSafeRLock("dbPlayerRow.GetdbPlayerTokenColumn")
	defer th.m_lock.UnSafeRUnlock()
	return string(th.m_Token)
}
func (th *dbPlayerRow)SetToken(v string){
	th.m_lock.UnSafeLock("dbPlayerRow.SetdbPlayerTokenColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Token=string(v)
	th.m_Token_changed=true
	return
}
func (th *dbPlayerRow)GetCurrReplyMsgNum( )(r int32 ){
	th.m_lock.UnSafeRLock("dbPlayerRow.GetdbPlayerCurrReplyMsgNumColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_CurrReplyMsgNum)
}
func (th *dbPlayerRow)SetCurrReplyMsgNum(v int32){
	th.m_lock.UnSafeLock("dbPlayerRow.SetdbPlayerCurrReplyMsgNumColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_CurrReplyMsgNum=int32(v)
	th.m_CurrReplyMsgNum_changed=true
	return
}
type dbPlayerInfoColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerInfoData
	m_changed bool
}
func (th *dbPlayerInfoColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerInfoData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerInfo{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerInfoData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerInfoColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerInfoColumn)Get( )(v *dbPlayerInfoData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInfoColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerInfoData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerInfoColumn)Set(v dbPlayerInfoData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerInfoData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerInfoColumn)GetLvl( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInfoColumn.GetLvl")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.Lvl
	return
}
func (th *dbPlayerInfoColumn)SetLvl(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.SetLvl")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Lvl = v
	th.m_changed = true
	return
}
func (th *dbPlayerInfoColumn)GetExp( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInfoColumn.GetExp")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.Exp
	return
}
func (th *dbPlayerInfoColumn)SetExp(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.SetExp")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Exp = v
	th.m_changed = true
	return
}
func (th *dbPlayerInfoColumn)IncbyExp(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.IncbyExp")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Exp += v
	th.m_changed = true
	return th.m_data.Exp
}
func (th *dbPlayerInfoColumn)GetCreateUnix( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInfoColumn.GetCreateUnix")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.CreateUnix
	return
}
func (th *dbPlayerInfoColumn)SetCreateUnix(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.SetCreateUnix")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CreateUnix = v
	th.m_changed = true
	return
}
func (th *dbPlayerInfoColumn)GetGold( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInfoColumn.GetGold")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.Gold
	return
}
func (th *dbPlayerInfoColumn)SetGold(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.SetGold")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Gold = v
	th.m_changed = true
	return
}
func (th *dbPlayerInfoColumn)IncbyGold(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.IncbyGold")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Gold += v
	th.m_changed = true
	return th.m_data.Gold
}
func (th *dbPlayerInfoColumn)GetDiamond( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInfoColumn.GetDiamond")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.Diamond
	return
}
func (th *dbPlayerInfoColumn)SetDiamond(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.SetDiamond")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Diamond = v
	th.m_changed = true
	return
}
func (th *dbPlayerInfoColumn)IncbyDiamond(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.IncbyDiamond")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Diamond += v
	th.m_changed = true
	return th.m_data.Diamond
}
func (th *dbPlayerInfoColumn)GetLastLogout( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInfoColumn.GetLastLogout")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastLogout
	return
}
func (th *dbPlayerInfoColumn)SetLastLogout(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.SetLastLogout")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastLogout = v
	th.m_changed = true
	return
}
func (th *dbPlayerInfoColumn)GetLastLogin( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInfoColumn.GetLastLogin")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastLogin
	return
}
func (th *dbPlayerInfoColumn)SetLastLogin(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.SetLastLogin")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastLogin = v
	th.m_changed = true
	return
}
func (th *dbPlayerInfoColumn)GetVipLvl( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInfoColumn.GetVipLvl")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.VipLvl
	return
}
func (th *dbPlayerInfoColumn)SetVipLvl(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.SetVipLvl")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.VipLvl = v
	th.m_changed = true
	return
}
func (th *dbPlayerInfoColumn)GetHead( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInfoColumn.GetHead")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.Head
	return
}
func (th *dbPlayerInfoColumn)SetHead(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerInfoColumn.SetHead")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Head = v
	th.m_changed = true
	return
}
type dbPlayerGlobalColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerGlobalData
	m_changed bool
}
func (th *dbPlayerGlobalColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerGlobalData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerGlobal{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerGlobalData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerGlobalColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerGlobalColumn)Get( )(v *dbPlayerGlobalData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGlobalColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerGlobalData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerGlobalColumn)Set(v dbPlayerGlobalData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerGlobalColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerGlobalData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerGlobalColumn)GetCurrentRoleId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGlobalColumn.GetCurrentRoleId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.CurrentRoleId
	return
}
func (th *dbPlayerGlobalColumn)SetCurrentRoleId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGlobalColumn.SetCurrentRoleId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrentRoleId = v
	th.m_changed = true
	return
}
func (th *dbPlayerGlobalColumn)IncbyCurrentRoleId(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGlobalColumn.IncbyCurrentRoleId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrentRoleId += v
	th.m_changed = true
	return th.m_data.CurrentRoleId
}
func (th *dbPlayerRow)GetLevel( )(r int32 ){
	th.m_lock.UnSafeRLock("dbPlayerRow.GetdbPlayerLevelColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_Level)
}
func (th *dbPlayerRow)SetLevel(v int32){
	th.m_lock.UnSafeLock("dbPlayerRow.SetdbPlayerLevelColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Level=int32(v)
	th.m_Level_changed=true
	return
}
type dbPlayerItemColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerItemData
	m_changed bool
}
func (th *dbPlayerItemColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerItemList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerItemData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerItemColumn)save( )(data []byte,err error){
	pb := &db.PlayerItemList{}
	pb.List=make([]*db.PlayerItem,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerItemColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerItemColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerItemColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerItemColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerItemColumn)GetAll()(list []dbPlayerItemData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerItemColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerItemData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerItemColumn)Get(id int32)(v *dbPlayerItemData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerItemColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerItemData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerItemColumn)Set(v dbPlayerItemData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerItemColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerItemColumn)Add(v *dbPlayerItemData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerItemColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerItemData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerItemColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerItemColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerItemColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerItemColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerItemData)
	th.m_changed = true
	return
}
func (th *dbPlayerItemColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerItemColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerItemColumn)GetCount(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerItemColumn.GetCount")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Count
	return v,true
}
func (th *dbPlayerItemColumn)SetCount(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerItemColumn.SetCount")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Count = v
	th.m_changed = true
	return true
}
func (th *dbPlayerItemColumn)IncbyCount(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerItemColumn.IncbyCount")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerItemData{}
		th.m_data[id] = d
	}
	d.Count +=  v
	th.m_changed = true
	return d.Count
}
type dbPlayerRoleCommonColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerRoleCommonData
	m_changed bool
}
func (th *dbPlayerRoleCommonColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerRoleCommonData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerRoleCommon{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerRoleCommonData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerRoleCommonColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerRoleCommonColumn)Get( )(v *dbPlayerRoleCommonData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleCommonColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerRoleCommonData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerRoleCommonColumn)Set(v dbPlayerRoleCommonData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleCommonColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerRoleCommonData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerRoleCommonColumn)GetDisplaceRoleId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleCommonColumn.GetDisplaceRoleId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.DisplaceRoleId
	return
}
func (th *dbPlayerRoleCommonColumn)SetDisplaceRoleId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleCommonColumn.SetDisplaceRoleId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.DisplaceRoleId = v
	th.m_changed = true
	return
}
func (th *dbPlayerRoleCommonColumn)GetDisplacedNewRoleTableId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleCommonColumn.GetDisplacedNewRoleTableId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.DisplacedNewRoleTableId
	return
}
func (th *dbPlayerRoleCommonColumn)SetDisplacedNewRoleTableId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleCommonColumn.SetDisplacedNewRoleTableId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.DisplacedNewRoleTableId = v
	th.m_changed = true
	return
}
func (th *dbPlayerRoleCommonColumn)GetDisplaceGroupId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleCommonColumn.GetDisplaceGroupId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.DisplaceGroupId
	return
}
func (th *dbPlayerRoleCommonColumn)SetDisplaceGroupId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleCommonColumn.SetDisplaceGroupId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.DisplaceGroupId = v
	th.m_changed = true
	return
}
func (th *dbPlayerRoleCommonColumn)GetPowerUpdateTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleCommonColumn.GetPowerUpdateTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.PowerUpdateTime
	return
}
func (th *dbPlayerRoleCommonColumn)SetPowerUpdateTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleCommonColumn.SetPowerUpdateTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.PowerUpdateTime = v
	th.m_changed = true
	return
}
type dbPlayerRoleColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerRoleData
	m_changed bool
}
func (th *dbPlayerRoleColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerRoleList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerRoleData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerRoleColumn)save( )(data []byte,err error){
	pb := &db.PlayerRoleList{}
	pb.List=make([]*db.PlayerRole,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerRoleColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerRoleColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerRoleColumn)GetAll()(list []dbPlayerRoleData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerRoleData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerRoleColumn)Get(id int32)(v *dbPlayerRoleData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerRoleData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerRoleColumn)Set(v dbPlayerRoleData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerRoleColumn)Add(v *dbPlayerRoleData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerRoleData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerRoleColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerRoleColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerRoleData)
	th.m_changed = true
	return
}
func (th *dbPlayerRoleColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerRoleColumn)GetTableId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.GetTableId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.TableId
	return v,true
}
func (th *dbPlayerRoleColumn)SetTableId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleColumn.SetTableId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.TableId = v
	th.m_changed = true
	return true
}
func (th *dbPlayerRoleColumn)GetRank(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.GetRank")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Rank
	return v,true
}
func (th *dbPlayerRoleColumn)SetRank(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleColumn.SetRank")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Rank = v
	th.m_changed = true
	return true
}
func (th *dbPlayerRoleColumn)GetLevel(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.GetLevel")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Level
	return v,true
}
func (th *dbPlayerRoleColumn)SetLevel(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleColumn.SetLevel")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Level = v
	th.m_changed = true
	return true
}
func (th *dbPlayerRoleColumn)GetEquip(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.GetEquip")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.Equip))
	for _ii, _vv := range d.Equip {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerRoleColumn)SetEquip(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleColumn.SetEquip")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Equip = make([]int32, len(v))
	for _ii, _vv := range v {
		d.Equip[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerRoleColumn)GetIsLock(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.GetIsLock")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.IsLock
	return v,true
}
func (th *dbPlayerRoleColumn)SetIsLock(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleColumn.SetIsLock")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.IsLock = v
	th.m_changed = true
	return true
}
func (th *dbPlayerRoleColumn)GetState(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleColumn.GetState")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.State
	return v,true
}
func (th *dbPlayerRoleColumn)SetState(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleColumn.SetState")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.State = v
	th.m_changed = true
	return true
}
type dbPlayerRoleHandbookColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerRoleHandbookData
	m_changed bool
}
func (th *dbPlayerRoleHandbookColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerRoleHandbookData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerRoleHandbook{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerRoleHandbookData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerRoleHandbookColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerRoleHandbookColumn)Get( )(v *dbPlayerRoleHandbookData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleHandbookColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerRoleHandbookData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerRoleHandbookColumn)Set(v dbPlayerRoleHandbookData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleHandbookColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerRoleHandbookData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerRoleHandbookColumn)GetRole( )(v []int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerRoleHandbookColumn.GetRole")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = make([]int32, len(th.m_data.Role))
	for _ii, _vv := range th.m_data.Role {
		v[_ii]=_vv
	}
	return
}
func (th *dbPlayerRoleHandbookColumn)SetRole(v []int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerRoleHandbookColumn.SetRole")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Role = make([]int32, len(v))
	for _ii, _vv := range v {
		th.m_data.Role[_ii]=_vv
	}
	th.m_changed = true
	return
}
type dbPlayerBattleTeamColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerBattleTeamData
	m_changed bool
}
func (th *dbPlayerBattleTeamColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerBattleTeamData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerBattleTeam{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerBattleTeamData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerBattleTeamColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerBattleTeamColumn)Get( )(v *dbPlayerBattleTeamData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleTeamColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerBattleTeamData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerBattleTeamColumn)Set(v dbPlayerBattleTeamData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerBattleTeamColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerBattleTeamData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerBattleTeamColumn)GetDefenseMembers( )(v []int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleTeamColumn.GetDefenseMembers")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = make([]int32, len(th.m_data.DefenseMembers))
	for _ii, _vv := range th.m_data.DefenseMembers {
		v[_ii]=_vv
	}
	return
}
func (th *dbPlayerBattleTeamColumn)SetDefenseMembers(v []int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerBattleTeamColumn.SetDefenseMembers")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.DefenseMembers = make([]int32, len(v))
	for _ii, _vv := range v {
		th.m_data.DefenseMembers[_ii]=_vv
	}
	th.m_changed = true
	return
}
func (th *dbPlayerBattleTeamColumn)GetCampaignMembers( )(v []int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleTeamColumn.GetCampaignMembers")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = make([]int32, len(th.m_data.CampaignMembers))
	for _ii, _vv := range th.m_data.CampaignMembers {
		v[_ii]=_vv
	}
	return
}
func (th *dbPlayerBattleTeamColumn)SetCampaignMembers(v []int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerBattleTeamColumn.SetCampaignMembers")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CampaignMembers = make([]int32, len(v))
	for _ii, _vv := range v {
		th.m_data.CampaignMembers[_ii]=_vv
	}
	th.m_changed = true
	return
}
func (th *dbPlayerBattleTeamColumn)GetDefenseArtifactId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleTeamColumn.GetDefenseArtifactId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.DefenseArtifactId
	return
}
func (th *dbPlayerBattleTeamColumn)SetDefenseArtifactId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerBattleTeamColumn.SetDefenseArtifactId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.DefenseArtifactId = v
	th.m_changed = true
	return
}
func (th *dbPlayerBattleTeamColumn)GetCampaignArtifactId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleTeamColumn.GetCampaignArtifactId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.CampaignArtifactId
	return
}
func (th *dbPlayerBattleTeamColumn)SetCampaignArtifactId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerBattleTeamColumn.SetCampaignArtifactId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CampaignArtifactId = v
	th.m_changed = true
	return
}
type dbPlayerCampaignCommonColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerCampaignCommonData
	m_changed bool
}
func (th *dbPlayerCampaignCommonColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerCampaignCommonData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerCampaignCommon{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerCampaignCommonData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerCampaignCommonColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerCampaignCommonColumn)Get( )(v *dbPlayerCampaignCommonData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignCommonColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerCampaignCommonData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerCampaignCommonColumn)Set(v dbPlayerCampaignCommonData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerCampaignCommonData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerCampaignCommonColumn)GetCurrentCampaignId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignCommonColumn.GetCurrentCampaignId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.CurrentCampaignId
	return
}
func (th *dbPlayerCampaignCommonColumn)SetCurrentCampaignId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.SetCurrentCampaignId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrentCampaignId = v
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignCommonColumn)GetHangupLastDropStaticIncomeTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignCommonColumn.GetHangupLastDropStaticIncomeTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.HangupLastDropStaticIncomeTime
	return
}
func (th *dbPlayerCampaignCommonColumn)SetHangupLastDropStaticIncomeTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.SetHangupLastDropStaticIncomeTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.HangupLastDropStaticIncomeTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignCommonColumn)GetHangupLastDropRandomIncomeTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignCommonColumn.GetHangupLastDropRandomIncomeTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.HangupLastDropRandomIncomeTime
	return
}
func (th *dbPlayerCampaignCommonColumn)SetHangupLastDropRandomIncomeTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.SetHangupLastDropRandomIncomeTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.HangupLastDropRandomIncomeTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignCommonColumn)GetHangupCampaignId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignCommonColumn.GetHangupCampaignId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.HangupCampaignId
	return
}
func (th *dbPlayerCampaignCommonColumn)SetHangupCampaignId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.SetHangupCampaignId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.HangupCampaignId = v
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignCommonColumn)GetLastestPassedCampaignId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignCommonColumn.GetLastestPassedCampaignId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastestPassedCampaignId
	return
}
func (th *dbPlayerCampaignCommonColumn)SetLastestPassedCampaignId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.SetLastestPassedCampaignId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastestPassedCampaignId = v
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignCommonColumn)GetRankSerialId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignCommonColumn.GetRankSerialId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.RankSerialId
	return
}
func (th *dbPlayerCampaignCommonColumn)SetRankSerialId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.SetRankSerialId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.RankSerialId = v
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignCommonColumn)IncbyRankSerialId(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.IncbyRankSerialId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.RankSerialId += v
	th.m_changed = true
	return th.m_data.RankSerialId
}
func (th *dbPlayerCampaignCommonColumn)GetVipAccelNum( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignCommonColumn.GetVipAccelNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.VipAccelNum
	return
}
func (th *dbPlayerCampaignCommonColumn)SetVipAccelNum(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.SetVipAccelNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.VipAccelNum = v
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignCommonColumn)IncbyVipAccelNum(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.IncbyVipAccelNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.VipAccelNum += v
	th.m_changed = true
	return th.m_data.VipAccelNum
}
func (th *dbPlayerCampaignCommonColumn)GetVipAccelRefreshTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignCommonColumn.GetVipAccelRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.VipAccelRefreshTime
	return
}
func (th *dbPlayerCampaignCommonColumn)SetVipAccelRefreshTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.SetVipAccelRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.VipAccelRefreshTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignCommonColumn)IncbyVipAccelRefreshTime(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.IncbyVipAccelRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.VipAccelRefreshTime += v
	th.m_changed = true
	return th.m_data.VipAccelRefreshTime
}
func (th *dbPlayerCampaignCommonColumn)GetPassCampaginTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignCommonColumn.GetPassCampaginTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.PassCampaginTime
	return
}
func (th *dbPlayerCampaignCommonColumn)SetPassCampaginTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignCommonColumn.SetPassCampaginTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.PassCampaginTime = v
	th.m_changed = true
	return
}
type dbPlayerCampaignColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerCampaignData
	m_changed bool
}
func (th *dbPlayerCampaignColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerCampaignList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerCampaignData{}
		d.from_pb(v)
		th.m_data[int32(d.CampaignId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerCampaignColumn)save( )(data []byte,err error){
	pb := &db.PlayerCampaignList{}
	pb.List=make([]*db.PlayerCampaign,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerCampaignColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerCampaignColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerCampaignColumn)GetAll()(list []dbPlayerCampaignData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerCampaignData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerCampaignColumn)Get(id int32)(v *dbPlayerCampaignData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerCampaignData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerCampaignColumn)Set(v dbPlayerCampaignData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.CampaignId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.CampaignId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerCampaignColumn)Add(v *dbPlayerCampaignData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.CampaignId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.CampaignId)
		return false
	}
	d:=&dbPlayerCampaignData{}
	v.clone_to(d)
	th.m_data[int32(v.CampaignId)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerCampaignColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerCampaignData)
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
type dbPlayerCampaignStaticIncomeColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerCampaignStaticIncomeData
	m_changed bool
}
func (th *dbPlayerCampaignStaticIncomeColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerCampaignStaticIncomeList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerCampaignStaticIncomeData{}
		d.from_pb(v)
		th.m_data[int32(d.ItemId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerCampaignStaticIncomeColumn)save( )(data []byte,err error){
	pb := &db.PlayerCampaignStaticIncomeList{}
	pb.List=make([]*db.PlayerCampaignStaticIncome,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerCampaignStaticIncomeColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignStaticIncomeColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerCampaignStaticIncomeColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignStaticIncomeColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerCampaignStaticIncomeColumn)GetAll()(list []dbPlayerCampaignStaticIncomeData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignStaticIncomeColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerCampaignStaticIncomeData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerCampaignStaticIncomeColumn)Get(id int32)(v *dbPlayerCampaignStaticIncomeData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignStaticIncomeColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerCampaignStaticIncomeData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerCampaignStaticIncomeColumn)Set(v dbPlayerCampaignStaticIncomeData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignStaticIncomeColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.ItemId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.ItemId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerCampaignStaticIncomeColumn)Add(v *dbPlayerCampaignStaticIncomeData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignStaticIncomeColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.ItemId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.ItemId)
		return false
	}
	d:=&dbPlayerCampaignStaticIncomeData{}
	v.clone_to(d)
	th.m_data[int32(v.ItemId)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerCampaignStaticIncomeColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignStaticIncomeColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignStaticIncomeColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignStaticIncomeColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerCampaignStaticIncomeData)
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignStaticIncomeColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignStaticIncomeColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerCampaignStaticIncomeColumn)GetItemNum(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignStaticIncomeColumn.GetItemNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.ItemNum
	return v,true
}
func (th *dbPlayerCampaignStaticIncomeColumn)SetItemNum(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignStaticIncomeColumn.SetItemNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.ItemNum = v
	th.m_changed = true
	return true
}
func (th *dbPlayerCampaignStaticIncomeColumn)IncbyItemNum(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignStaticIncomeColumn.IncbyItemNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerCampaignStaticIncomeData{}
		th.m_data[id] = d
	}
	d.ItemNum +=  v
	th.m_changed = true
	return d.ItemNum
}
type dbPlayerCampaignRandomIncomeColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerCampaignRandomIncomeData
	m_changed bool
}
func (th *dbPlayerCampaignRandomIncomeColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerCampaignRandomIncomeList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerCampaignRandomIncomeData{}
		d.from_pb(v)
		th.m_data[int32(d.ItemId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerCampaignRandomIncomeColumn)save( )(data []byte,err error){
	pb := &db.PlayerCampaignRandomIncomeList{}
	pb.List=make([]*db.PlayerCampaignRandomIncome,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerCampaignRandomIncomeColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignRandomIncomeColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerCampaignRandomIncomeColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignRandomIncomeColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerCampaignRandomIncomeColumn)GetAll()(list []dbPlayerCampaignRandomIncomeData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignRandomIncomeColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerCampaignRandomIncomeData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerCampaignRandomIncomeColumn)Get(id int32)(v *dbPlayerCampaignRandomIncomeData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignRandomIncomeColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerCampaignRandomIncomeData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerCampaignRandomIncomeColumn)Set(v dbPlayerCampaignRandomIncomeData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignRandomIncomeColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.ItemId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.ItemId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerCampaignRandomIncomeColumn)Add(v *dbPlayerCampaignRandomIncomeData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignRandomIncomeColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.ItemId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.ItemId)
		return false
	}
	d:=&dbPlayerCampaignRandomIncomeData{}
	v.clone_to(d)
	th.m_data[int32(v.ItemId)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerCampaignRandomIncomeColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignRandomIncomeColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignRandomIncomeColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignRandomIncomeColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerCampaignRandomIncomeData)
	th.m_changed = true
	return
}
func (th *dbPlayerCampaignRandomIncomeColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignRandomIncomeColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerCampaignRandomIncomeColumn)GetItemNum(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCampaignRandomIncomeColumn.GetItemNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.ItemNum
	return v,true
}
func (th *dbPlayerCampaignRandomIncomeColumn)SetItemNum(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignRandomIncomeColumn.SetItemNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.ItemNum = v
	th.m_changed = true
	return true
}
func (th *dbPlayerCampaignRandomIncomeColumn)IncbyItemNum(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCampaignRandomIncomeColumn.IncbyItemNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerCampaignRandomIncomeData{}
		th.m_data[id] = d
	}
	d.ItemNum +=  v
	th.m_changed = true
	return d.ItemNum
}
type dbPlayerMailCommonColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerMailCommonData
	m_changed bool
}
func (th *dbPlayerMailCommonColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerMailCommonData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerMailCommon{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerMailCommonData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerMailCommonColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerMailCommonColumn)Get( )(v *dbPlayerMailCommonData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailCommonColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerMailCommonData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerMailCommonColumn)Set(v dbPlayerMailCommonData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailCommonColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerMailCommonData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerMailCommonColumn)GetCurrId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailCommonColumn.GetCurrId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.CurrId
	return
}
func (th *dbPlayerMailCommonColumn)SetCurrId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailCommonColumn.SetCurrId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrId = v
	th.m_changed = true
	return
}
func (th *dbPlayerMailCommonColumn)IncbyCurrId(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailCommonColumn.IncbyCurrId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrId += v
	th.m_changed = true
	return th.m_data.CurrId
}
func (th *dbPlayerMailCommonColumn)GetLastSendPlayerMailTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailCommonColumn.GetLastSendPlayerMailTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastSendPlayerMailTime
	return
}
func (th *dbPlayerMailCommonColumn)SetLastSendPlayerMailTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailCommonColumn.SetLastSendPlayerMailTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastSendPlayerMailTime = v
	th.m_changed = true
	return
}
type dbPlayerMailColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerMailData
	m_changed bool
}
func (th *dbPlayerMailColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerMailList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerMailData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerMailColumn)save( )(data []byte,err error){
	pb := &db.PlayerMailList{}
	pb.List=make([]*db.PlayerMail,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerMailColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerMailColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerMailColumn)GetAll()(list []dbPlayerMailData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerMailData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerMailColumn)Get(id int32)(v *dbPlayerMailData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerMailData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerMailColumn)Set(v dbPlayerMailData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)Add(v *dbPlayerMailData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerMailData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerMailColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerMailData)
	th.m_changed = true
	return
}
func (th *dbPlayerMailColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerMailColumn)GetType(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetType")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = int32(d.Type)
	return v,true
}
func (th *dbPlayerMailColumn)SetType(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetType")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Type = int8(v)
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)GetTitle(id int32)(v string ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetTitle")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Title
	return v,true
}
func (th *dbPlayerMailColumn)SetTitle(id int32,v string)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetTitle")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Title = v
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)GetContent(id int32)(v string ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetContent")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Content
	return v,true
}
func (th *dbPlayerMailColumn)SetContent(id int32,v string)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetContent")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Content = v
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)GetSendUnix(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetSendUnix")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.SendUnix
	return v,true
}
func (th *dbPlayerMailColumn)SetSendUnix(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetSendUnix")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.SendUnix = v
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)GetAttachItemIds(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetAttachItemIds")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.AttachItemIds))
	for _ii, _vv := range d.AttachItemIds {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerMailColumn)SetAttachItemIds(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetAttachItemIds")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.AttachItemIds = make([]int32, len(v))
	for _ii, _vv := range v {
		d.AttachItemIds[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)GetAttachItemNums(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetAttachItemNums")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.AttachItemNums))
	for _ii, _vv := range d.AttachItemNums {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerMailColumn)SetAttachItemNums(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetAttachItemNums")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.AttachItemNums = make([]int32, len(v))
	for _ii, _vv := range v {
		d.AttachItemNums[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)GetIsRead(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetIsRead")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.IsRead
	return v,true
}
func (th *dbPlayerMailColumn)SetIsRead(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetIsRead")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.IsRead = v
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)GetIsGetAttached(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetIsGetAttached")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.IsGetAttached
	return v,true
}
func (th *dbPlayerMailColumn)SetIsGetAttached(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetIsGetAttached")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.IsGetAttached = v
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)GetSenderId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetSenderId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.SenderId
	return v,true
}
func (th *dbPlayerMailColumn)SetSenderId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetSenderId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.SenderId = v
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)GetSenderName(id int32)(v string ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetSenderName")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.SenderName
	return v,true
}
func (th *dbPlayerMailColumn)SetSenderName(id int32,v string)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetSenderName")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.SenderName = v
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)GetSubtype(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetSubtype")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Subtype
	return v,true
}
func (th *dbPlayerMailColumn)SetSubtype(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetSubtype")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Subtype = v
	th.m_changed = true
	return true
}
func (th *dbPlayerMailColumn)GetExtraValue(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerMailColumn.GetExtraValue")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.ExtraValue
	return v,true
}
func (th *dbPlayerMailColumn)SetExtraValue(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerMailColumn.SetExtraValue")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.ExtraValue = v
	th.m_changed = true
	return true
}
type dbPlayerBattleSaveColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerBattleSaveData
	m_changed bool
}
func (th *dbPlayerBattleSaveColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerBattleSaveList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerBattleSaveData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerBattleSaveColumn)save( )(data []byte,err error){
	pb := &db.PlayerBattleSaveList{}
	pb.List=make([]*db.PlayerBattleSave,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerBattleSaveColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleSaveColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerBattleSaveColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleSaveColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerBattleSaveColumn)GetAll()(list []dbPlayerBattleSaveData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleSaveColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerBattleSaveData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerBattleSaveColumn)Get(id int32)(v *dbPlayerBattleSaveData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleSaveColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerBattleSaveData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerBattleSaveColumn)Set(v dbPlayerBattleSaveData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerBattleSaveColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerBattleSaveColumn)Add(v *dbPlayerBattleSaveData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerBattleSaveColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerBattleSaveData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerBattleSaveColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerBattleSaveColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerBattleSaveColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerBattleSaveColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerBattleSaveData)
	th.m_changed = true
	return
}
func (th *dbPlayerBattleSaveColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleSaveColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerBattleSaveColumn)GetSide(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleSaveColumn.GetSide")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Side
	return v,true
}
func (th *dbPlayerBattleSaveColumn)SetSide(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerBattleSaveColumn.SetSide")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Side = v
	th.m_changed = true
	return true
}
func (th *dbPlayerBattleSaveColumn)GetSaveTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerBattleSaveColumn.GetSaveTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.SaveTime
	return v,true
}
func (th *dbPlayerBattleSaveColumn)SetSaveTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerBattleSaveColumn.SetSaveTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.SaveTime = v
	th.m_changed = true
	return true
}
type dbPlayerTalentColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerTalentData
	m_changed bool
}
func (th *dbPlayerTalentColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerTalentList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerTalentData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerTalentColumn)save( )(data []byte,err error){
	pb := &db.PlayerTalentList{}
	pb.List=make([]*db.PlayerTalent,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerTalentColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTalentColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerTalentColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTalentColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerTalentColumn)GetAll()(list []dbPlayerTalentData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTalentColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerTalentData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerTalentColumn)Get(id int32)(v *dbPlayerTalentData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTalentColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerTalentData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerTalentColumn)Set(v dbPlayerTalentData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerTalentColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerTalentColumn)Add(v *dbPlayerTalentData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerTalentColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerTalentData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerTalentColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerTalentColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerTalentColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerTalentColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerTalentData)
	th.m_changed = true
	return
}
func (th *dbPlayerTalentColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTalentColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerTalentColumn)GetLevel(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTalentColumn.GetLevel")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Level
	return v,true
}
func (th *dbPlayerTalentColumn)SetLevel(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerTalentColumn.SetLevel")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Level = v
	th.m_changed = true
	return true
}
type dbPlayerTowerCommonColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerTowerCommonData
	m_changed bool
}
func (th *dbPlayerTowerCommonColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerTowerCommonData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerTowerCommon{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerTowerCommonData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerTowerCommonColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerTowerCommonColumn)Get( )(v *dbPlayerTowerCommonData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTowerCommonColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerTowerCommonData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerTowerCommonColumn)Set(v dbPlayerTowerCommonData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerTowerCommonColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerTowerCommonData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerTowerCommonColumn)GetCurrId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTowerCommonColumn.GetCurrId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.CurrId
	return
}
func (th *dbPlayerTowerCommonColumn)SetCurrId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerTowerCommonColumn.SetCurrId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrId = v
	th.m_changed = true
	return
}
func (th *dbPlayerTowerCommonColumn)GetKeys( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTowerCommonColumn.GetKeys")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.Keys
	return
}
func (th *dbPlayerTowerCommonColumn)SetKeys(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerTowerCommonColumn.SetKeys")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Keys = v
	th.m_changed = true
	return
}
func (th *dbPlayerTowerCommonColumn)GetLastGetNewKeyTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTowerCommonColumn.GetLastGetNewKeyTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastGetNewKeyTime
	return
}
func (th *dbPlayerTowerCommonColumn)SetLastGetNewKeyTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerTowerCommonColumn.SetLastGetNewKeyTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastGetNewKeyTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerTowerCommonColumn)GetRankSerialId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTowerCommonColumn.GetRankSerialId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.RankSerialId
	return
}
func (th *dbPlayerTowerCommonColumn)SetRankSerialId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerTowerCommonColumn.SetRankSerialId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.RankSerialId = v
	th.m_changed = true
	return
}
func (th *dbPlayerTowerCommonColumn)GetPassTowerTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTowerCommonColumn.GetPassTowerTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.PassTowerTime
	return
}
func (th *dbPlayerTowerCommonColumn)SetPassTowerTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerTowerCommonColumn.SetPassTowerTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.PassTowerTime = v
	th.m_changed = true
	return
}
type dbPlayerTowerColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerTowerData
	m_changed bool
}
func (th *dbPlayerTowerColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerTowerList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerTowerData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerTowerColumn)save( )(data []byte,err error){
	pb := &db.PlayerTowerList{}
	pb.List=make([]*db.PlayerTower,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerTowerColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTowerColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerTowerColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTowerColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerTowerColumn)GetAll()(list []dbPlayerTowerData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTowerColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerTowerData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerTowerColumn)Get(id int32)(v *dbPlayerTowerData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTowerColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerTowerData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerTowerColumn)Set(v dbPlayerTowerData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerTowerColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerTowerColumn)Add(v *dbPlayerTowerData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerTowerColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerTowerData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerTowerColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerTowerColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerTowerColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerTowerColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerTowerData)
	th.m_changed = true
	return
}
func (th *dbPlayerTowerColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTowerColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
type dbPlayerDrawColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerDrawData
	m_changed bool
}
func (th *dbPlayerDrawColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerDrawList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerDrawData{}
		d.from_pb(v)
		th.m_data[int32(d.Type)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerDrawColumn)save( )(data []byte,err error){
	pb := &db.PlayerDrawList{}
	pb.List=make([]*db.PlayerDraw,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerDrawColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDrawColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerDrawColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDrawColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerDrawColumn)GetAll()(list []dbPlayerDrawData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDrawColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerDrawData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerDrawColumn)Get(id int32)(v *dbPlayerDrawData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDrawColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerDrawData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerDrawColumn)Set(v dbPlayerDrawData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerDrawColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Type)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Type)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerDrawColumn)Add(v *dbPlayerDrawData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerDrawColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Type)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Type)
		return false
	}
	d:=&dbPlayerDrawData{}
	v.clone_to(d)
	th.m_data[int32(v.Type)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerDrawColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerDrawColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerDrawColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerDrawColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerDrawData)
	th.m_changed = true
	return
}
func (th *dbPlayerDrawColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDrawColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerDrawColumn)GetLastDrawTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDrawColumn.GetLastDrawTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.LastDrawTime
	return v,true
}
func (th *dbPlayerDrawColumn)SetLastDrawTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerDrawColumn.SetLastDrawTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.LastDrawTime = v
	th.m_changed = true
	return true
}
func (th *dbPlayerDrawColumn)GetNum(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDrawColumn.GetNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Num
	return v,true
}
func (th *dbPlayerDrawColumn)SetNum(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerDrawColumn.SetNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Num = v
	th.m_changed = true
	return true
}
type dbPlayerGoldHandColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerGoldHandData
	m_changed bool
}
func (th *dbPlayerGoldHandColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerGoldHandData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerGoldHand{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerGoldHandData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerGoldHandColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerGoldHandColumn)Get( )(v *dbPlayerGoldHandData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGoldHandColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerGoldHandData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerGoldHandColumn)Set(v dbPlayerGoldHandData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerGoldHandColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerGoldHandData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerGoldHandColumn)GetLastRefreshTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGoldHandColumn.GetLastRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastRefreshTime
	return
}
func (th *dbPlayerGoldHandColumn)SetLastRefreshTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGoldHandColumn.SetLastRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastRefreshTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerGoldHandColumn)GetLeftNum( )(v []int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGoldHandColumn.GetLeftNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = make([]int32, len(th.m_data.LeftNum))
	for _ii, _vv := range th.m_data.LeftNum {
		v[_ii]=_vv
	}
	return
}
func (th *dbPlayerGoldHandColumn)SetLeftNum(v []int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGoldHandColumn.SetLeftNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LeftNum = make([]int32, len(v))
	for _ii, _vv := range v {
		th.m_data.LeftNum[_ii]=_vv
	}
	th.m_changed = true
	return
}
type dbPlayerShopColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerShopData
	m_changed bool
}
func (th *dbPlayerShopColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerShopList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerShopData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerShopColumn)save( )(data []byte,err error){
	pb := &db.PlayerShopList{}
	pb.List=make([]*db.PlayerShop,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerShopColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerShopColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerShopColumn)GetAll()(list []dbPlayerShopData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerShopData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerShopColumn)Get(id int32)(v *dbPlayerShopData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerShopData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerShopColumn)Set(v dbPlayerShopData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerShopColumn)Add(v *dbPlayerShopData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerShopData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerShopColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerShopColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerShopData)
	th.m_changed = true
	return
}
func (th *dbPlayerShopColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerShopColumn)GetLastFreeRefreshTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopColumn.GetLastFreeRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.LastFreeRefreshTime
	return v,true
}
func (th *dbPlayerShopColumn)SetLastFreeRefreshTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopColumn.SetLastFreeRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.LastFreeRefreshTime = v
	th.m_changed = true
	return true
}
func (th *dbPlayerShopColumn)GetLastAutoRefreshTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopColumn.GetLastAutoRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.LastAutoRefreshTime
	return v,true
}
func (th *dbPlayerShopColumn)SetLastAutoRefreshTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopColumn.SetLastAutoRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.LastAutoRefreshTime = v
	th.m_changed = true
	return true
}
func (th *dbPlayerShopColumn)GetCurrAutoId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopColumn.GetCurrAutoId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.CurrAutoId
	return v,true
}
func (th *dbPlayerShopColumn)SetCurrAutoId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopColumn.SetCurrAutoId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.CurrAutoId = v
	th.m_changed = true
	return true
}
func (th *dbPlayerShopColumn)IncbyCurrAutoId(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopColumn.IncbyCurrAutoId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerShopData{}
		th.m_data[id] = d
	}
	d.CurrAutoId +=  v
	th.m_changed = true
	return d.CurrAutoId
}
type dbPlayerShopItemColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerShopItemData
	m_changed bool
}
func (th *dbPlayerShopItemColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerShopItemList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerShopItemData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerShopItemColumn)save( )(data []byte,err error){
	pb := &db.PlayerShopItemList{}
	pb.List=make([]*db.PlayerShopItem,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerShopItemColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopItemColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerShopItemColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopItemColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerShopItemColumn)GetAll()(list []dbPlayerShopItemData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopItemColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerShopItemData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerShopItemColumn)Get(id int32)(v *dbPlayerShopItemData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopItemColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerShopItemData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerShopItemColumn)Set(v dbPlayerShopItemData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopItemColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerShopItemColumn)Add(v *dbPlayerShopItemData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopItemColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerShopItemData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerShopItemColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopItemColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerShopItemColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopItemColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerShopItemData)
	th.m_changed = true
	return
}
func (th *dbPlayerShopItemColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopItemColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerShopItemColumn)GetShopItemId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopItemColumn.GetShopItemId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.ShopItemId
	return v,true
}
func (th *dbPlayerShopItemColumn)SetShopItemId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopItemColumn.SetShopItemId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.ShopItemId = v
	th.m_changed = true
	return true
}
func (th *dbPlayerShopItemColumn)GetLeftNum(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopItemColumn.GetLeftNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.LeftNum
	return v,true
}
func (th *dbPlayerShopItemColumn)SetLeftNum(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopItemColumn.SetLeftNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.LeftNum = v
	th.m_changed = true
	return true
}
func (th *dbPlayerShopItemColumn)IncbyLeftNum(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopItemColumn.IncbyLeftNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerShopItemData{}
		th.m_data[id] = d
	}
	d.LeftNum +=  v
	th.m_changed = true
	return d.LeftNum
}
func (th *dbPlayerShopItemColumn)GetShopId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopItemColumn.GetShopId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.ShopId
	return v,true
}
func (th *dbPlayerShopItemColumn)SetShopId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopItemColumn.SetShopId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.ShopId = v
	th.m_changed = true
	return true
}
func (th *dbPlayerShopItemColumn)GetBuyNum(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerShopItemColumn.GetBuyNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.BuyNum
	return v,true
}
func (th *dbPlayerShopItemColumn)SetBuyNum(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopItemColumn.SetBuyNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.BuyNum = v
	th.m_changed = true
	return true
}
func (th *dbPlayerShopItemColumn)IncbyBuyNum(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerShopItemColumn.IncbyBuyNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerShopItemData{}
		th.m_data[id] = d
	}
	d.BuyNum +=  v
	th.m_changed = true
	return d.BuyNum
}
type dbPlayerArenaColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerArenaData
	m_changed bool
}
func (th *dbPlayerArenaColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerArenaData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerArena{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerArenaData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerArenaColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerArenaColumn)Get( )(v *dbPlayerArenaData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArenaColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerArenaData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerArenaColumn)Set(v dbPlayerArenaData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerArenaData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerArenaColumn)GetRepeatedWinNum( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArenaColumn.GetRepeatedWinNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.RepeatedWinNum
	return
}
func (th *dbPlayerArenaColumn)SetRepeatedWinNum(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.SetRepeatedWinNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.RepeatedWinNum = v
	th.m_changed = true
	return
}
func (th *dbPlayerArenaColumn)IncbyRepeatedWinNum(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.IncbyRepeatedWinNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.RepeatedWinNum += v
	th.m_changed = true
	return th.m_data.RepeatedWinNum
}
func (th *dbPlayerArenaColumn)GetRepeatedLoseNum( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArenaColumn.GetRepeatedLoseNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.RepeatedLoseNum
	return
}
func (th *dbPlayerArenaColumn)SetRepeatedLoseNum(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.SetRepeatedLoseNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.RepeatedLoseNum = v
	th.m_changed = true
	return
}
func (th *dbPlayerArenaColumn)IncbyRepeatedLoseNum(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.IncbyRepeatedLoseNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.RepeatedLoseNum += v
	th.m_changed = true
	return th.m_data.RepeatedLoseNum
}
func (th *dbPlayerArenaColumn)GetScore( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArenaColumn.GetScore")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.Score
	return
}
func (th *dbPlayerArenaColumn)SetScore(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.SetScore")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Score = v
	th.m_changed = true
	return
}
func (th *dbPlayerArenaColumn)IncbyScore(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.IncbyScore")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Score += v
	th.m_changed = true
	return th.m_data.Score
}
func (th *dbPlayerArenaColumn)GetUpdateScoreTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArenaColumn.GetUpdateScoreTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.UpdateScoreTime
	return
}
func (th *dbPlayerArenaColumn)SetUpdateScoreTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.SetUpdateScoreTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.UpdateScoreTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerArenaColumn)GetMatchedPlayerId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArenaColumn.GetMatchedPlayerId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.MatchedPlayerId
	return
}
func (th *dbPlayerArenaColumn)SetMatchedPlayerId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.SetMatchedPlayerId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.MatchedPlayerId = v
	th.m_changed = true
	return
}
func (th *dbPlayerArenaColumn)GetHistoryTopRank( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArenaColumn.GetHistoryTopRank")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.HistoryTopRank
	return
}
func (th *dbPlayerArenaColumn)SetHistoryTopRank(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.SetHistoryTopRank")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.HistoryTopRank = v
	th.m_changed = true
	return
}
func (th *dbPlayerArenaColumn)GetFirstGetTicket( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArenaColumn.GetFirstGetTicket")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.FirstGetTicket
	return
}
func (th *dbPlayerArenaColumn)SetFirstGetTicket(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.SetFirstGetTicket")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.FirstGetTicket = v
	th.m_changed = true
	return
}
func (th *dbPlayerArenaColumn)GetLastTicketsRefreshTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArenaColumn.GetLastTicketsRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastTicketsRefreshTime
	return
}
func (th *dbPlayerArenaColumn)SetLastTicketsRefreshTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.SetLastTicketsRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastTicketsRefreshTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerArenaColumn)GetSerialId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArenaColumn.GetSerialId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.SerialId
	return
}
func (th *dbPlayerArenaColumn)SetSerialId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArenaColumn.SetSerialId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.SerialId = v
	th.m_changed = true
	return
}
type dbPlayerEquipColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerEquipData
	m_changed bool
}
func (th *dbPlayerEquipColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerEquipData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerEquip{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerEquipData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerEquipColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerEquipColumn)Get( )(v *dbPlayerEquipData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerEquipColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerEquipData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerEquipColumn)Set(v dbPlayerEquipData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerEquipColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerEquipData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerEquipColumn)GetTmpSaveLeftSlotRoleId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerEquipColumn.GetTmpSaveLeftSlotRoleId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.TmpSaveLeftSlotRoleId
	return
}
func (th *dbPlayerEquipColumn)SetTmpSaveLeftSlotRoleId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerEquipColumn.SetTmpSaveLeftSlotRoleId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.TmpSaveLeftSlotRoleId = v
	th.m_changed = true
	return
}
func (th *dbPlayerEquipColumn)GetTmpLeftSlotItemId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerEquipColumn.GetTmpLeftSlotItemId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.TmpLeftSlotItemId
	return
}
func (th *dbPlayerEquipColumn)SetTmpLeftSlotItemId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerEquipColumn.SetTmpLeftSlotItemId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.TmpLeftSlotItemId = v
	th.m_changed = true
	return
}
type dbPlayerActiveStageCommonColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerActiveStageCommonData
	m_changed bool
}
func (th *dbPlayerActiveStageCommonColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerActiveStageCommonData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerActiveStageCommon{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerActiveStageCommonData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerActiveStageCommonColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerActiveStageCommonColumn)Get( )(v *dbPlayerActiveStageCommonData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageCommonColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerActiveStageCommonData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerActiveStageCommonColumn)Set(v dbPlayerActiveStageCommonData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageCommonColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerActiveStageCommonData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerActiveStageCommonColumn)GetLastRefreshTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageCommonColumn.GetLastRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastRefreshTime
	return
}
func (th *dbPlayerActiveStageCommonColumn)SetLastRefreshTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageCommonColumn.SetLastRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastRefreshTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerActiveStageCommonColumn)GetGetPointsDay( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageCommonColumn.GetGetPointsDay")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.GetPointsDay
	return
}
func (th *dbPlayerActiveStageCommonColumn)SetGetPointsDay(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageCommonColumn.SetGetPointsDay")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.GetPointsDay = v
	th.m_changed = true
	return
}
func (th *dbPlayerActiveStageCommonColumn)IncbyGetPointsDay(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageCommonColumn.IncbyGetPointsDay")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.GetPointsDay += v
	th.m_changed = true
	return th.m_data.GetPointsDay
}
func (th *dbPlayerActiveStageCommonColumn)GetWithdrawPoints( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageCommonColumn.GetWithdrawPoints")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.WithdrawPoints
	return
}
func (th *dbPlayerActiveStageCommonColumn)SetWithdrawPoints(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageCommonColumn.SetWithdrawPoints")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.WithdrawPoints = v
	th.m_changed = true
	return
}
func (th *dbPlayerActiveStageCommonColumn)IncbyWithdrawPoints(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageCommonColumn.IncbyWithdrawPoints")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.WithdrawPoints += v
	th.m_changed = true
	return th.m_data.WithdrawPoints
}
type dbPlayerActiveStageColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerActiveStageData
	m_changed bool
}
func (th *dbPlayerActiveStageColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerActiveStageList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerActiveStageData{}
		d.from_pb(v)
		th.m_data[int32(d.Type)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerActiveStageColumn)save( )(data []byte,err error){
	pb := &db.PlayerActiveStageList{}
	pb.List=make([]*db.PlayerActiveStage,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerActiveStageColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerActiveStageColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerActiveStageColumn)GetAll()(list []dbPlayerActiveStageData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerActiveStageData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerActiveStageColumn)Get(id int32)(v *dbPlayerActiveStageData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerActiveStageData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerActiveStageColumn)Set(v dbPlayerActiveStageData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Type)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Type)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerActiveStageColumn)Add(v *dbPlayerActiveStageData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Type)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Type)
		return false
	}
	d:=&dbPlayerActiveStageData{}
	v.clone_to(d)
	th.m_data[int32(v.Type)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerActiveStageColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerActiveStageColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerActiveStageData)
	th.m_changed = true
	return
}
func (th *dbPlayerActiveStageColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerActiveStageColumn)GetCanChallengeNum(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageColumn.GetCanChallengeNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.CanChallengeNum
	return v,true
}
func (th *dbPlayerActiveStageColumn)SetCanChallengeNum(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageColumn.SetCanChallengeNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.CanChallengeNum = v
	th.m_changed = true
	return true
}
func (th *dbPlayerActiveStageColumn)IncbyCanChallengeNum(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageColumn.IncbyCanChallengeNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerActiveStageData{}
		th.m_data[id] = d
	}
	d.CanChallengeNum +=  v
	th.m_changed = true
	return d.CanChallengeNum
}
func (th *dbPlayerActiveStageColumn)GetPurchasedNum(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageColumn.GetPurchasedNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.PurchasedNum
	return v,true
}
func (th *dbPlayerActiveStageColumn)SetPurchasedNum(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageColumn.SetPurchasedNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.PurchasedNum = v
	th.m_changed = true
	return true
}
func (th *dbPlayerActiveStageColumn)IncbyPurchasedNum(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageColumn.IncbyPurchasedNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerActiveStageData{}
		th.m_data[id] = d
	}
	d.PurchasedNum +=  v
	th.m_changed = true
	return d.PurchasedNum
}
func (th *dbPlayerActiveStageColumn)GetBuyNum(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActiveStageColumn.GetBuyNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.BuyNum
	return v,true
}
func (th *dbPlayerActiveStageColumn)SetBuyNum(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageColumn.SetBuyNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.BuyNum = v
	th.m_changed = true
	return true
}
func (th *dbPlayerActiveStageColumn)IncbyBuyNum(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerActiveStageColumn.IncbyBuyNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerActiveStageData{}
		th.m_data[id] = d
	}
	d.BuyNum +=  v
	th.m_changed = true
	return d.BuyNum
}
type dbPlayerFriendCommonColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerFriendCommonData
	m_changed bool
}
func (th *dbPlayerFriendCommonColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerFriendCommonData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerFriendCommon{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerFriendCommonData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerFriendCommonColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendCommonColumn)Get( )(v *dbPlayerFriendCommonData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerFriendCommonData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerFriendCommonColumn)Set(v dbPlayerFriendCommonData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerFriendCommonData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerFriendCommonColumn)GetLastRecommendTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.GetLastRecommendTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastRecommendTime
	return
}
func (th *dbPlayerFriendCommonColumn)SetLastRecommendTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.SetLastRecommendTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastRecommendTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerFriendCommonColumn)GetLastBossRefreshTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.GetLastBossRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastBossRefreshTime
	return
}
func (th *dbPlayerFriendCommonColumn)SetLastBossRefreshTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.SetLastBossRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastBossRefreshTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerFriendCommonColumn)GetFriendBossTableId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.GetFriendBossTableId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.FriendBossTableId
	return
}
func (th *dbPlayerFriendCommonColumn)SetFriendBossTableId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.SetFriendBossTableId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.FriendBossTableId = v
	th.m_changed = true
	return
}
func (th *dbPlayerFriendCommonColumn)GetFriendBossHpPercent( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.GetFriendBossHpPercent")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.FriendBossHpPercent
	return
}
func (th *dbPlayerFriendCommonColumn)SetFriendBossHpPercent(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.SetFriendBossHpPercent")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.FriendBossHpPercent = v
	th.m_changed = true
	return
}
func (th *dbPlayerFriendCommonColumn)GetAttackBossPlayerList( )(v []int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.GetAttackBossPlayerList")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = make([]int32, len(th.m_data.AttackBossPlayerList))
	for _ii, _vv := range th.m_data.AttackBossPlayerList {
		v[_ii]=_vv
	}
	return
}
func (th *dbPlayerFriendCommonColumn)SetAttackBossPlayerList(v []int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.SetAttackBossPlayerList")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.AttackBossPlayerList = make([]int32, len(v))
	for _ii, _vv := range v {
		th.m_data.AttackBossPlayerList[_ii]=_vv
	}
	th.m_changed = true
	return
}
func (th *dbPlayerFriendCommonColumn)GetLastGetStaminaTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.GetLastGetStaminaTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastGetStaminaTime
	return
}
func (th *dbPlayerFriendCommonColumn)SetLastGetStaminaTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.SetLastGetStaminaTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastGetStaminaTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerFriendCommonColumn)GetAssistRoleId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.GetAssistRoleId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.AssistRoleId
	return
}
func (th *dbPlayerFriendCommonColumn)SetAssistRoleId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.SetAssistRoleId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.AssistRoleId = v
	th.m_changed = true
	return
}
func (th *dbPlayerFriendCommonColumn)GetLastGetPointsTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.GetLastGetPointsTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastGetPointsTime
	return
}
func (th *dbPlayerFriendCommonColumn)SetLastGetPointsTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.SetLastGetPointsTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastGetPointsTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerFriendCommonColumn)GetGetPointsDay( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.GetGetPointsDay")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.GetPointsDay
	return
}
func (th *dbPlayerFriendCommonColumn)SetGetPointsDay(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.SetGetPointsDay")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.GetPointsDay = v
	th.m_changed = true
	return
}
func (th *dbPlayerFriendCommonColumn)IncbyGetPointsDay(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.IncbyGetPointsDay")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.GetPointsDay += v
	th.m_changed = true
	return th.m_data.GetPointsDay
}
func (th *dbPlayerFriendCommonColumn)GetSearchedBossNum( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.GetSearchedBossNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.SearchedBossNum
	return
}
func (th *dbPlayerFriendCommonColumn)SetSearchedBossNum(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.SetSearchedBossNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.SearchedBossNum = v
	th.m_changed = true
	return
}
func (th *dbPlayerFriendCommonColumn)IncbySearchedBossNum(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.IncbySearchedBossNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.SearchedBossNum += v
	th.m_changed = true
	return th.m_data.SearchedBossNum
}
func (th *dbPlayerFriendCommonColumn)GetLastSearchBossNumRefreshTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendCommonColumn.GetLastSearchBossNumRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastSearchBossNumRefreshTime
	return
}
func (th *dbPlayerFriendCommonColumn)SetLastSearchBossNumRefreshTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendCommonColumn.SetLastSearchBossNumRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastSearchBossNumRefreshTime = v
	th.m_changed = true
	return
}
type dbPlayerFriendColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerFriendData
	m_changed bool
}
func (th *dbPlayerFriendColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerFriendList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerFriendData{}
		d.from_pb(v)
		th.m_data[int32(d.PlayerId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendColumn)save( )(data []byte,err error){
	pb := &db.PlayerFriendList{}
	pb.List=make([]*db.PlayerFriend,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerFriendColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerFriendColumn)GetAll()(list []dbPlayerFriendData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerFriendData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerFriendColumn)Get(id int32)(v *dbPlayerFriendData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerFriendData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerFriendColumn)Set(v dbPlayerFriendData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.PlayerId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.PlayerId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendColumn)Add(v *dbPlayerFriendData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.PlayerId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.PlayerId)
		return false
	}
	d:=&dbPlayerFriendData{}
	v.clone_to(d)
	th.m_data[int32(v.PlayerId)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerFriendColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerFriendData)
	th.m_changed = true
	return
}
func (th *dbPlayerFriendColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerFriendColumn)GetLastGivePointsTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendColumn.GetLastGivePointsTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.LastGivePointsTime
	return v,true
}
func (th *dbPlayerFriendColumn)SetLastGivePointsTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendColumn.SetLastGivePointsTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.LastGivePointsTime = v
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendColumn)GetGetPoints(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendColumn.GetGetPoints")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.GetPoints
	return v,true
}
func (th *dbPlayerFriendColumn)SetGetPoints(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendColumn.SetGetPoints")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.GetPoints = v
	th.m_changed = true
	return true
}
type dbPlayerFriendRecommendColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerFriendRecommendData
	m_changed bool
}
func (th *dbPlayerFriendRecommendColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerFriendRecommendList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerFriendRecommendData{}
		d.from_pb(v)
		th.m_data[int32(d.PlayerId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendRecommendColumn)save( )(data []byte,err error){
	pb := &db.PlayerFriendRecommendList{}
	pb.List=make([]*db.PlayerFriendRecommend,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendRecommendColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendRecommendColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerFriendRecommendColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendRecommendColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerFriendRecommendColumn)GetAll()(list []dbPlayerFriendRecommendData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendRecommendColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerFriendRecommendData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerFriendRecommendColumn)Get(id int32)(v *dbPlayerFriendRecommendData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendRecommendColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerFriendRecommendData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerFriendRecommendColumn)Set(v dbPlayerFriendRecommendData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendRecommendColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.PlayerId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.PlayerId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendRecommendColumn)Add(v *dbPlayerFriendRecommendData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendRecommendColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.PlayerId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.PlayerId)
		return false
	}
	d:=&dbPlayerFriendRecommendData{}
	v.clone_to(d)
	th.m_data[int32(v.PlayerId)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendRecommendColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendRecommendColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerFriendRecommendColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendRecommendColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerFriendRecommendData)
	th.m_changed = true
	return
}
func (th *dbPlayerFriendRecommendColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendRecommendColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
type dbPlayerFriendAskColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerFriendAskData
	m_changed bool
}
func (th *dbPlayerFriendAskColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerFriendAskList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerFriendAskData{}
		d.from_pb(v)
		th.m_data[int32(d.PlayerId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendAskColumn)save( )(data []byte,err error){
	pb := &db.PlayerFriendAskList{}
	pb.List=make([]*db.PlayerFriendAsk,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendAskColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendAskColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerFriendAskColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendAskColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerFriendAskColumn)GetAll()(list []dbPlayerFriendAskData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendAskColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerFriendAskData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerFriendAskColumn)Get(id int32)(v *dbPlayerFriendAskData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendAskColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerFriendAskData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerFriendAskColumn)Set(v dbPlayerFriendAskData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendAskColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.PlayerId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.PlayerId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendAskColumn)Add(v *dbPlayerFriendAskData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendAskColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.PlayerId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.PlayerId)
		return false
	}
	d:=&dbPlayerFriendAskData{}
	v.clone_to(d)
	th.m_data[int32(v.PlayerId)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendAskColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendAskColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerFriendAskColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendAskColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerFriendAskData)
	th.m_changed = true
	return
}
func (th *dbPlayerFriendAskColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendAskColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
type dbPlayerFriendBossColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerFriendBossData
	m_changed bool
}
func (th *dbPlayerFriendBossColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerFriendBossList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerFriendBossData{}
		d.from_pb(v)
		th.m_data[int32(d.MonsterPos)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendBossColumn)save( )(data []byte,err error){
	pb := &db.PlayerFriendBossList{}
	pb.List=make([]*db.PlayerFriendBoss,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendBossColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendBossColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerFriendBossColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendBossColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerFriendBossColumn)GetAll()(list []dbPlayerFriendBossData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendBossColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerFriendBossData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerFriendBossColumn)Get(id int32)(v *dbPlayerFriendBossData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendBossColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerFriendBossData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerFriendBossColumn)Set(v dbPlayerFriendBossData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendBossColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.MonsterPos)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.MonsterPos)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendBossColumn)Add(v *dbPlayerFriendBossData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendBossColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.MonsterPos)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.MonsterPos)
		return false
	}
	d:=&dbPlayerFriendBossData{}
	v.clone_to(d)
	th.m_data[int32(v.MonsterPos)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendBossColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendBossColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerFriendBossColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendBossColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerFriendBossData)
	th.m_changed = true
	return
}
func (th *dbPlayerFriendBossColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendBossColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerFriendBossColumn)GetMonsterId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendBossColumn.GetMonsterId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.MonsterId
	return v,true
}
func (th *dbPlayerFriendBossColumn)SetMonsterId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendBossColumn.SetMonsterId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.MonsterId = v
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendBossColumn)GetMonsterHp(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendBossColumn.GetMonsterHp")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.MonsterHp
	return v,true
}
func (th *dbPlayerFriendBossColumn)SetMonsterHp(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendBossColumn.SetMonsterHp")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.MonsterHp = v
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendBossColumn)GetMonsterMaxHp(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendBossColumn.GetMonsterMaxHp")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.MonsterMaxHp
	return v,true
}
func (th *dbPlayerFriendBossColumn)SetMonsterMaxHp(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendBossColumn.SetMonsterMaxHp")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.MonsterMaxHp = v
	th.m_changed = true
	return true
}
type dbPlayerTaskCommonColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerTaskCommonData
	m_changed bool
}
func (th *dbPlayerTaskCommonColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerTaskCommonData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerTaskCommon{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerTaskCommonData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerTaskCommonColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerTaskCommonColumn)Get( )(v *dbPlayerTaskCommonData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTaskCommonColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerTaskCommonData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerTaskCommonColumn)Set(v dbPlayerTaskCommonData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerTaskCommonColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerTaskCommonData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerTaskCommonColumn)GetLastRefreshTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTaskCommonColumn.GetLastRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastRefreshTime
	return
}
func (th *dbPlayerTaskCommonColumn)SetLastRefreshTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerTaskCommonColumn.SetLastRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastRefreshTime = v
	th.m_changed = true
	return
}
type dbPlayerTaskColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerTaskData
	m_changed bool
}
func (th *dbPlayerTaskColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerTaskList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerTaskData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerTaskColumn)save( )(data []byte,err error){
	pb := &db.PlayerTaskList{}
	pb.List=make([]*db.PlayerTask,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerTaskColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTaskColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerTaskColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTaskColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerTaskColumn)GetAll()(list []dbPlayerTaskData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTaskColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerTaskData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerTaskColumn)Get(id int32)(v *dbPlayerTaskData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTaskColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerTaskData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerTaskColumn)Set(v dbPlayerTaskData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerTaskColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerTaskColumn)Add(v *dbPlayerTaskData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerTaskColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerTaskData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerTaskColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerTaskColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerTaskColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerTaskColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerTaskData)
	th.m_changed = true
	return
}
func (th *dbPlayerTaskColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTaskColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerTaskColumn)GetValue(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTaskColumn.GetValue")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Value
	return v,true
}
func (th *dbPlayerTaskColumn)SetValue(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerTaskColumn.SetValue")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Value = v
	th.m_changed = true
	return true
}
func (th *dbPlayerTaskColumn)IncbyValue(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerTaskColumn.IncbyValue")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerTaskData{}
		th.m_data[id] = d
	}
	d.Value +=  v
	th.m_changed = true
	return d.Value
}
func (th *dbPlayerTaskColumn)GetState(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTaskColumn.GetState")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.State
	return v,true
}
func (th *dbPlayerTaskColumn)SetState(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerTaskColumn.SetState")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.State = v
	th.m_changed = true
	return true
}
func (th *dbPlayerTaskColumn)GetParam(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerTaskColumn.GetParam")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Param
	return v,true
}
func (th *dbPlayerTaskColumn)SetParam(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerTaskColumn.SetParam")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Param = v
	th.m_changed = true
	return true
}
type dbPlayerFinishedTaskColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerFinishedTaskData
	m_changed bool
}
func (th *dbPlayerFinishedTaskColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerFinishedTaskList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerFinishedTaskData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFinishedTaskColumn)save( )(data []byte,err error){
	pb := &db.PlayerFinishedTaskList{}
	pb.List=make([]*db.PlayerFinishedTask,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFinishedTaskColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFinishedTaskColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerFinishedTaskColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFinishedTaskColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerFinishedTaskColumn)GetAll()(list []dbPlayerFinishedTaskData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFinishedTaskColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerFinishedTaskData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerFinishedTaskColumn)Get(id int32)(v *dbPlayerFinishedTaskData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFinishedTaskColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerFinishedTaskData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerFinishedTaskColumn)Set(v dbPlayerFinishedTaskData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFinishedTaskColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerFinishedTaskColumn)Add(v *dbPlayerFinishedTaskData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFinishedTaskColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerFinishedTaskData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerFinishedTaskColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFinishedTaskColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerFinishedTaskColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerFinishedTaskColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerFinishedTaskData)
	th.m_changed = true
	return
}
func (th *dbPlayerFinishedTaskColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFinishedTaskColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
type dbPlayerDailyTaskAllDailyColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerDailyTaskAllDailyData
	m_changed bool
}
func (th *dbPlayerDailyTaskAllDailyColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerDailyTaskAllDailyList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerDailyTaskAllDailyData{}
		d.from_pb(v)
		th.m_data[int32(d.CompleteTaskId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerDailyTaskAllDailyColumn)save( )(data []byte,err error){
	pb := &db.PlayerDailyTaskAllDailyList{}
	pb.List=make([]*db.PlayerDailyTaskAllDaily,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerDailyTaskAllDailyColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDailyTaskAllDailyColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerDailyTaskAllDailyColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDailyTaskAllDailyColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerDailyTaskAllDailyColumn)GetAll()(list []dbPlayerDailyTaskAllDailyData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDailyTaskAllDailyColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerDailyTaskAllDailyData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerDailyTaskAllDailyColumn)Get(id int32)(v *dbPlayerDailyTaskAllDailyData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDailyTaskAllDailyColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerDailyTaskAllDailyData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerDailyTaskAllDailyColumn)Set(v dbPlayerDailyTaskAllDailyData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerDailyTaskAllDailyColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.CompleteTaskId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.CompleteTaskId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerDailyTaskAllDailyColumn)Add(v *dbPlayerDailyTaskAllDailyData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerDailyTaskAllDailyColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.CompleteTaskId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.CompleteTaskId)
		return false
	}
	d:=&dbPlayerDailyTaskAllDailyData{}
	v.clone_to(d)
	th.m_data[int32(v.CompleteTaskId)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerDailyTaskAllDailyColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerDailyTaskAllDailyColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerDailyTaskAllDailyColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerDailyTaskAllDailyColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerDailyTaskAllDailyData)
	th.m_changed = true
	return
}
func (th *dbPlayerDailyTaskAllDailyColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerDailyTaskAllDailyColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
type dbPlayerExploreCommonColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerExploreCommonData
	m_changed bool
}
func (th *dbPlayerExploreCommonColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerExploreCommonData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerExploreCommon{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerExploreCommonData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerExploreCommonColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExploreCommonColumn)Get( )(v *dbPlayerExploreCommonData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreCommonColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerExploreCommonData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerExploreCommonColumn)Set(v dbPlayerExploreCommonData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreCommonColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerExploreCommonData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerExploreCommonColumn)GetLastRefreshTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreCommonColumn.GetLastRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastRefreshTime
	return
}
func (th *dbPlayerExploreCommonColumn)SetLastRefreshTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreCommonColumn.SetLastRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastRefreshTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerExploreCommonColumn)GetCurrentId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreCommonColumn.GetCurrentId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.CurrentId
	return
}
func (th *dbPlayerExploreCommonColumn)SetCurrentId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreCommonColumn.SetCurrentId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrentId = v
	th.m_changed = true
	return
}
func (th *dbPlayerExploreCommonColumn)IncbyCurrentId(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreCommonColumn.IncbyCurrentId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrentId += v
	th.m_changed = true
	return th.m_data.CurrentId
}
type dbPlayerExploreColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerExploreData
	m_changed bool
}
func (th *dbPlayerExploreColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerExploreList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerExploreData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExploreColumn)save( )(data []byte,err error){
	pb := &db.PlayerExploreList{}
	pb.List=make([]*db.PlayerExplore,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExploreColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerExploreColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerExploreColumn)GetAll()(list []dbPlayerExploreData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerExploreData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerExploreColumn)Get(id int32)(v *dbPlayerExploreData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerExploreData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerExploreColumn)Set(v dbPlayerExploreData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)Add(v *dbPlayerExploreData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerExploreData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerExploreColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerExploreData)
	th.m_changed = true
	return
}
func (th *dbPlayerExploreColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerExploreColumn)GetTaskId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetTaskId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.TaskId
	return v,true
}
func (th *dbPlayerExploreColumn)SetTaskId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.SetTaskId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.TaskId = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)GetState(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetState")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.State
	return v,true
}
func (th *dbPlayerExploreColumn)SetState(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.SetState")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.State = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)GetRoleCampsCanSel(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetRoleCampsCanSel")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.RoleCampsCanSel))
	for _ii, _vv := range d.RoleCampsCanSel {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerExploreColumn)SetRoleCampsCanSel(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.SetRoleCampsCanSel")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.RoleCampsCanSel = make([]int32, len(v))
	for _ii, _vv := range v {
		d.RoleCampsCanSel[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)GetRoleTypesCanSel(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetRoleTypesCanSel")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.RoleTypesCanSel))
	for _ii, _vv := range d.RoleTypesCanSel {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerExploreColumn)SetRoleTypesCanSel(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.SetRoleTypesCanSel")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.RoleTypesCanSel = make([]int32, len(v))
	for _ii, _vv := range v {
		d.RoleTypesCanSel[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)GetRoleId4TaskTitle(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetRoleId4TaskTitle")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.RoleId4TaskTitle
	return v,true
}
func (th *dbPlayerExploreColumn)SetRoleId4TaskTitle(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.SetRoleId4TaskTitle")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.RoleId4TaskTitle = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)GetNameId4TaskTitle(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetNameId4TaskTitle")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.NameId4TaskTitle
	return v,true
}
func (th *dbPlayerExploreColumn)SetNameId4TaskTitle(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.SetNameId4TaskTitle")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.NameId4TaskTitle = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)GetStartTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetStartTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.StartTime
	return v,true
}
func (th *dbPlayerExploreColumn)SetStartTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.SetStartTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.StartTime = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)GetRoleIds(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetRoleIds")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.RoleIds))
	for _ii, _vv := range d.RoleIds {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerExploreColumn)SetRoleIds(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.SetRoleIds")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.RoleIds = make([]int32, len(v))
	for _ii, _vv := range v {
		d.RoleIds[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)GetIsLock(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetIsLock")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.IsLock
	return v,true
}
func (th *dbPlayerExploreColumn)SetIsLock(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.SetIsLock")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.IsLock = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)GetRandomRewards(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetRandomRewards")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.RandomRewards))
	for _ii, _vv := range d.RandomRewards {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerExploreColumn)SetRandomRewards(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.SetRandomRewards")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.RandomRewards = make([]int32, len(v))
	for _ii, _vv := range v {
		d.RandomRewards[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreColumn)GetRewardStageId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreColumn.GetRewardStageId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.RewardStageId
	return v,true
}
func (th *dbPlayerExploreColumn)SetRewardStageId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreColumn.SetRewardStageId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.RewardStageId = v
	th.m_changed = true
	return true
}
type dbPlayerExploreStoryColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerExploreStoryData
	m_changed bool
}
func (th *dbPlayerExploreStoryColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerExploreStoryList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerExploreStoryData{}
		d.from_pb(v)
		th.m_data[int32(d.TaskId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExploreStoryColumn)save( )(data []byte,err error){
	pb := &db.PlayerExploreStoryList{}
	pb.List=make([]*db.PlayerExploreStory,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExploreStoryColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerExploreStoryColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerExploreStoryColumn)GetAll()(list []dbPlayerExploreStoryData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerExploreStoryData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerExploreStoryColumn)Get(id int32)(v *dbPlayerExploreStoryData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerExploreStoryData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerExploreStoryColumn)Set(v dbPlayerExploreStoryData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreStoryColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.TaskId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.TaskId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreStoryColumn)Add(v *dbPlayerExploreStoryData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreStoryColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.TaskId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.TaskId)
		return false
	}
	d:=&dbPlayerExploreStoryData{}
	v.clone_to(d)
	th.m_data[int32(v.TaskId)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreStoryColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreStoryColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerExploreStoryColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreStoryColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerExploreStoryData)
	th.m_changed = true
	return
}
func (th *dbPlayerExploreStoryColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerExploreStoryColumn)GetState(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.GetState")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.State
	return v,true
}
func (th *dbPlayerExploreStoryColumn)SetState(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreStoryColumn.SetState")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.State = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreStoryColumn)GetRoleCampsCanSel(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.GetRoleCampsCanSel")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.RoleCampsCanSel))
	for _ii, _vv := range d.RoleCampsCanSel {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerExploreStoryColumn)SetRoleCampsCanSel(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreStoryColumn.SetRoleCampsCanSel")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.RoleCampsCanSel = make([]int32, len(v))
	for _ii, _vv := range v {
		d.RoleCampsCanSel[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreStoryColumn)GetRoleTypesCanSel(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.GetRoleTypesCanSel")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.RoleTypesCanSel))
	for _ii, _vv := range d.RoleTypesCanSel {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerExploreStoryColumn)SetRoleTypesCanSel(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreStoryColumn.SetRoleTypesCanSel")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.RoleTypesCanSel = make([]int32, len(v))
	for _ii, _vv := range v {
		d.RoleTypesCanSel[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreStoryColumn)GetStartTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.GetStartTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.StartTime
	return v,true
}
func (th *dbPlayerExploreStoryColumn)SetStartTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreStoryColumn.SetStartTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.StartTime = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreStoryColumn)GetRoleIds(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.GetRoleIds")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.RoleIds))
	for _ii, _vv := range d.RoleIds {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerExploreStoryColumn)SetRoleIds(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreStoryColumn.SetRoleIds")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.RoleIds = make([]int32, len(v))
	for _ii, _vv := range v {
		d.RoleIds[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreStoryColumn)GetRandomRewards(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.GetRandomRewards")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.RandomRewards))
	for _ii, _vv := range d.RandomRewards {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerExploreStoryColumn)SetRandomRewards(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreStoryColumn.SetRandomRewards")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.RandomRewards = make([]int32, len(v))
	for _ii, _vv := range v {
		d.RandomRewards[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerExploreStoryColumn)GetRewardStageId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExploreStoryColumn.GetRewardStageId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.RewardStageId
	return v,true
}
func (th *dbPlayerExploreStoryColumn)SetRewardStageId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExploreStoryColumn.SetRewardStageId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.RewardStageId = v
	th.m_changed = true
	return true
}
type dbPlayerFriendChatUnreadIdColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerFriendChatUnreadIdData
	m_changed bool
}
func (th *dbPlayerFriendChatUnreadIdColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerFriendChatUnreadIdList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerFriendChatUnreadIdData{}
		d.from_pb(v)
		th.m_data[int32(d.FriendId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendChatUnreadIdColumn)save( )(data []byte,err error){
	pb := &db.PlayerFriendChatUnreadIdList{}
	pb.List=make([]*db.PlayerFriendChatUnreadId,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendChatUnreadIdColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadIdColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerFriendChatUnreadIdColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadIdColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerFriendChatUnreadIdColumn)GetAll()(list []dbPlayerFriendChatUnreadIdData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadIdColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerFriendChatUnreadIdData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerFriendChatUnreadIdColumn)Get(id int32)(v *dbPlayerFriendChatUnreadIdData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadIdColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerFriendChatUnreadIdData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerFriendChatUnreadIdColumn)Set(v dbPlayerFriendChatUnreadIdData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadIdColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.FriendId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.FriendId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendChatUnreadIdColumn)Add(v *dbPlayerFriendChatUnreadIdData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadIdColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.FriendId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.FriendId)
		return false
	}
	d:=&dbPlayerFriendChatUnreadIdData{}
	v.clone_to(d)
	th.m_data[int32(v.FriendId)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendChatUnreadIdColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadIdColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerFriendChatUnreadIdColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadIdColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerFriendChatUnreadIdData)
	th.m_changed = true
	return
}
func (th *dbPlayerFriendChatUnreadIdColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadIdColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerFriendChatUnreadIdColumn)GetMessageIds(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadIdColumn.GetMessageIds")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.MessageIds))
	for _ii, _vv := range d.MessageIds {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerFriendChatUnreadIdColumn)SetMessageIds(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadIdColumn.SetMessageIds")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.MessageIds = make([]int32, len(v))
	for _ii, _vv := range v {
		d.MessageIds[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendChatUnreadIdColumn)GetCurrMessageId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadIdColumn.GetCurrMessageId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.CurrMessageId
	return v,true
}
func (th *dbPlayerFriendChatUnreadIdColumn)SetCurrMessageId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadIdColumn.SetCurrMessageId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.CurrMessageId = v
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendChatUnreadIdColumn)IncbyCurrMessageId(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadIdColumn.IncbyCurrMessageId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerFriendChatUnreadIdData{}
		th.m_data[id] = d
	}
	d.CurrMessageId +=  v
	th.m_changed = true
	return d.CurrMessageId
}
type dbPlayerFriendChatUnreadMessageColumn struct{
	m_row *dbPlayerRow
	m_data map[int64]*dbPlayerFriendChatUnreadMessageData
	m_changed bool
}
func (th *dbPlayerFriendChatUnreadMessageColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerFriendChatUnreadMessageList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerFriendChatUnreadMessageData{}
		d.from_pb(v)
		th.m_data[int64(d.PlayerMessageId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendChatUnreadMessageColumn)save( )(data []byte,err error){
	pb := &db.PlayerFriendChatUnreadMessageList{}
	pb.List=make([]*db.PlayerFriendChatUnreadMessage,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFriendChatUnreadMessageColumn)HasIndex(id int64)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadMessageColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerFriendChatUnreadMessageColumn)GetAllIndex()(list []int64){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadMessageColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int64, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerFriendChatUnreadMessageColumn)GetAll()(list []dbPlayerFriendChatUnreadMessageData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadMessageColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerFriendChatUnreadMessageData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerFriendChatUnreadMessageColumn)Get(id int64)(v *dbPlayerFriendChatUnreadMessageData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadMessageColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerFriendChatUnreadMessageData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerFriendChatUnreadMessageColumn)Set(v dbPlayerFriendChatUnreadMessageData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadMessageColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int64(v.PlayerMessageId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.PlayerMessageId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendChatUnreadMessageColumn)Add(v *dbPlayerFriendChatUnreadMessageData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadMessageColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int64(v.PlayerMessageId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.PlayerMessageId)
		return false
	}
	d:=&dbPlayerFriendChatUnreadMessageData{}
	v.clone_to(d)
	th.m_data[int64(v.PlayerMessageId)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendChatUnreadMessageColumn)Remove(id int64){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadMessageColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerFriendChatUnreadMessageColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadMessageColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int64]*dbPlayerFriendChatUnreadMessageData)
	th.m_changed = true
	return
}
func (th *dbPlayerFriendChatUnreadMessageColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadMessageColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerFriendChatUnreadMessageColumn)GetMessage(id int64)(v []byte,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadMessageColumn.GetMessage")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]byte, len(d.Message))
	for _ii, _vv := range d.Message {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerFriendChatUnreadMessageColumn)SetMessage(id int64,v []byte)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadMessageColumn.SetMessage")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Message = make([]byte, len(v))
	for _ii, _vv := range v {
		d.Message[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendChatUnreadMessageColumn)GetSendTime(id int64)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadMessageColumn.GetSendTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.SendTime
	return v,true
}
func (th *dbPlayerFriendChatUnreadMessageColumn)SetSendTime(id int64,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadMessageColumn.SetSendTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.SendTime = v
	th.m_changed = true
	return true
}
func (th *dbPlayerFriendChatUnreadMessageColumn)GetIsRead(id int64)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFriendChatUnreadMessageColumn.GetIsRead")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.IsRead
	return v,true
}
func (th *dbPlayerFriendChatUnreadMessageColumn)SetIsRead(id int64,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFriendChatUnreadMessageColumn.SetIsRead")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.IsRead = v
	th.m_changed = true
	return true
}
type dbPlayerHeadItemColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerHeadItemData
	m_changed bool
}
func (th *dbPlayerHeadItemColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerHeadItemList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerHeadItemData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerHeadItemColumn)save( )(data []byte,err error){
	pb := &db.PlayerHeadItemList{}
	pb.List=make([]*db.PlayerHeadItem,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerHeadItemColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerHeadItemColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerHeadItemColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerHeadItemColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerHeadItemColumn)GetAll()(list []dbPlayerHeadItemData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerHeadItemColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerHeadItemData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerHeadItemColumn)Get(id int32)(v *dbPlayerHeadItemData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerHeadItemColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerHeadItemData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerHeadItemColumn)Set(v dbPlayerHeadItemData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerHeadItemColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerHeadItemColumn)Add(v *dbPlayerHeadItemData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerHeadItemColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerHeadItemData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerHeadItemColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerHeadItemColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerHeadItemColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerHeadItemColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerHeadItemData)
	th.m_changed = true
	return
}
func (th *dbPlayerHeadItemColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerHeadItemColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
type dbPlayerSuitAwardColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerSuitAwardData
	m_changed bool
}
func (th *dbPlayerSuitAwardColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerSuitAwardList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerSuitAwardData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerSuitAwardColumn)save( )(data []byte,err error){
	pb := &db.PlayerSuitAwardList{}
	pb.List=make([]*db.PlayerSuitAward,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerSuitAwardColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSuitAwardColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerSuitAwardColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSuitAwardColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerSuitAwardColumn)GetAll()(list []dbPlayerSuitAwardData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSuitAwardColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerSuitAwardData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerSuitAwardColumn)Get(id int32)(v *dbPlayerSuitAwardData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSuitAwardColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerSuitAwardData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerSuitAwardColumn)Set(v dbPlayerSuitAwardData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerSuitAwardColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerSuitAwardColumn)Add(v *dbPlayerSuitAwardData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerSuitAwardColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerSuitAwardData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerSuitAwardColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerSuitAwardColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerSuitAwardColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerSuitAwardColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerSuitAwardData)
	th.m_changed = true
	return
}
func (th *dbPlayerSuitAwardColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSuitAwardColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerSuitAwardColumn)GetAwardTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSuitAwardColumn.GetAwardTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.AwardTime
	return v,true
}
func (th *dbPlayerSuitAwardColumn)SetAwardTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerSuitAwardColumn.SetAwardTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.AwardTime = v
	th.m_changed = true
	return true
}
type dbPlayerChatColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerChatData
	m_changed bool
}
func (th *dbPlayerChatColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerChatList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerChatData{}
		d.from_pb(v)
		th.m_data[int32(d.Channel)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerChatColumn)save( )(data []byte,err error){
	pb := &db.PlayerChatList{}
	pb.List=make([]*db.PlayerChat,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerChatColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerChatColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerChatColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerChatColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerChatColumn)GetAll()(list []dbPlayerChatData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerChatColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerChatData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerChatColumn)Get(id int32)(v *dbPlayerChatData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerChatColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerChatData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerChatColumn)Set(v dbPlayerChatData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerChatColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Channel)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Channel)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerChatColumn)Add(v *dbPlayerChatData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerChatColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Channel)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Channel)
		return false
	}
	d:=&dbPlayerChatData{}
	v.clone_to(d)
	th.m_data[int32(v.Channel)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerChatColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerChatColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerChatColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerChatColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerChatData)
	th.m_changed = true
	return
}
func (th *dbPlayerChatColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerChatColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerChatColumn)GetLastChatTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerChatColumn.GetLastChatTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.LastChatTime
	return v,true
}
func (th *dbPlayerChatColumn)SetLastChatTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerChatColumn.SetLastChatTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.LastChatTime = v
	th.m_changed = true
	return true
}
func (th *dbPlayerChatColumn)GetLastPullTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerChatColumn.GetLastPullTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.LastPullTime
	return v,true
}
func (th *dbPlayerChatColumn)SetLastPullTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerChatColumn.SetLastPullTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.LastPullTime = v
	th.m_changed = true
	return true
}
func (th *dbPlayerChatColumn)GetLastMsgIndex(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerChatColumn.GetLastMsgIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.LastMsgIndex
	return v,true
}
func (th *dbPlayerChatColumn)SetLastMsgIndex(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerChatColumn.SetLastMsgIndex")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.LastMsgIndex = v
	th.m_changed = true
	return true
}
type dbPlayerAnouncementColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerAnouncementData
	m_changed bool
}
func (th *dbPlayerAnouncementColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerAnouncementData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerAnouncement{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerAnouncementData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerAnouncementColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerAnouncementColumn)Get( )(v *dbPlayerAnouncementData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerAnouncementColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerAnouncementData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerAnouncementColumn)Set(v dbPlayerAnouncementData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerAnouncementColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerAnouncementData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerAnouncementColumn)GetLastSendTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerAnouncementColumn.GetLastSendTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastSendTime
	return
}
func (th *dbPlayerAnouncementColumn)SetLastSendTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerAnouncementColumn.SetLastSendTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastSendTime = v
	th.m_changed = true
	return
}
type dbPlayerFirstDrawCardColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerFirstDrawCardData
	m_changed bool
}
func (th *dbPlayerFirstDrawCardColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerFirstDrawCardList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerFirstDrawCardData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFirstDrawCardColumn)save( )(data []byte,err error){
	pb := &db.PlayerFirstDrawCardList{}
	pb.List=make([]*db.PlayerFirstDrawCard,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerFirstDrawCardColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFirstDrawCardColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerFirstDrawCardColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFirstDrawCardColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerFirstDrawCardColumn)GetAll()(list []dbPlayerFirstDrawCardData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFirstDrawCardColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerFirstDrawCardData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerFirstDrawCardColumn)Get(id int32)(v *dbPlayerFirstDrawCardData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFirstDrawCardColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerFirstDrawCardData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerFirstDrawCardColumn)Set(v dbPlayerFirstDrawCardData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFirstDrawCardColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerFirstDrawCardColumn)Add(v *dbPlayerFirstDrawCardData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFirstDrawCardColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerFirstDrawCardData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerFirstDrawCardColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerFirstDrawCardColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerFirstDrawCardColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerFirstDrawCardColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerFirstDrawCardData)
	th.m_changed = true
	return
}
func (th *dbPlayerFirstDrawCardColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFirstDrawCardColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerFirstDrawCardColumn)GetDrawed(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerFirstDrawCardColumn.GetDrawed")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Drawed
	return v,true
}
func (th *dbPlayerFirstDrawCardColumn)SetDrawed(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerFirstDrawCardColumn.SetDrawed")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Drawed = v
	th.m_changed = true
	return true
}
type dbPlayerGuildColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerGuildData
	m_changed bool
}
func (th *dbPlayerGuildColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerGuildData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerGuild{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerGuildData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerGuildColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerGuildColumn)Get( )(v *dbPlayerGuildData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerGuildData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerGuildColumn)Set(v dbPlayerGuildData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerGuildData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerGuildColumn)GetId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildColumn.GetId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.Id
	return
}
func (th *dbPlayerGuildColumn)SetId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildColumn.SetId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Id = v
	th.m_changed = true
	return
}
func (th *dbPlayerGuildColumn)GetJoinTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildColumn.GetJoinTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.JoinTime
	return
}
func (th *dbPlayerGuildColumn)SetJoinTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildColumn.SetJoinTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.JoinTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerGuildColumn)GetQuitTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildColumn.GetQuitTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.QuitTime
	return
}
func (th *dbPlayerGuildColumn)SetQuitTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildColumn.SetQuitTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.QuitTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerGuildColumn)GetSignTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildColumn.GetSignTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.SignTime
	return
}
func (th *dbPlayerGuildColumn)SetSignTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildColumn.SetSignTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.SignTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerGuildColumn)GetPosition( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildColumn.GetPosition")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.Position
	return
}
func (th *dbPlayerGuildColumn)SetPosition(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildColumn.SetPosition")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Position = v
	th.m_changed = true
	return
}
func (th *dbPlayerGuildColumn)GetDonateNum( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildColumn.GetDonateNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.DonateNum
	return
}
func (th *dbPlayerGuildColumn)SetDonateNum(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildColumn.SetDonateNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.DonateNum = v
	th.m_changed = true
	return
}
func (th *dbPlayerGuildColumn)GetLastAskDonateTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildColumn.GetLastAskDonateTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastAskDonateTime
	return
}
func (th *dbPlayerGuildColumn)SetLastAskDonateTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildColumn.SetLastAskDonateTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastAskDonateTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerGuildColumn)GetLastDonateTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildColumn.GetLastDonateTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastDonateTime
	return
}
func (th *dbPlayerGuildColumn)SetLastDonateTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildColumn.SetLastDonateTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastDonateTime = v
	th.m_changed = true
	return
}
type dbPlayerGuildStageColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerGuildStageData
	m_changed bool
}
func (th *dbPlayerGuildStageColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerGuildStageData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerGuildStage{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerGuildStageData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerGuildStageColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerGuildStageColumn)Get( )(v *dbPlayerGuildStageData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildStageColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerGuildStageData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerGuildStageColumn)Set(v dbPlayerGuildStageData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildStageColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerGuildStageData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerGuildStageColumn)GetRespawnNum( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildStageColumn.GetRespawnNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.RespawnNum
	return
}
func (th *dbPlayerGuildStageColumn)SetRespawnNum(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildStageColumn.SetRespawnNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.RespawnNum = v
	th.m_changed = true
	return
}
func (th *dbPlayerGuildStageColumn)IncbyRespawnNum(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildStageColumn.IncbyRespawnNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.RespawnNum += v
	th.m_changed = true
	return th.m_data.RespawnNum
}
func (th *dbPlayerGuildStageColumn)GetRespawnState( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildStageColumn.GetRespawnState")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.RespawnState
	return
}
func (th *dbPlayerGuildStageColumn)SetRespawnState(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildStageColumn.SetRespawnState")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.RespawnState = v
	th.m_changed = true
	return
}
func (th *dbPlayerGuildStageColumn)GetLastRefreshTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuildStageColumn.GetLastRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastRefreshTime
	return
}
func (th *dbPlayerGuildStageColumn)SetLastRefreshTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuildStageColumn.SetLastRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastRefreshTime = v
	th.m_changed = true
	return
}
type dbPlayerSignColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerSignData
	m_changed bool
}
func (th *dbPlayerSignColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerSignData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerSign{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerSignData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerSignColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerSignColumn)Get( )(v *dbPlayerSignData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSignColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerSignData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerSignColumn)Set(v dbPlayerSignData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerSignColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerSignData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerSignColumn)GetCurrGroup( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSignColumn.GetCurrGroup")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.CurrGroup
	return
}
func (th *dbPlayerSignColumn)SetCurrGroup(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerSignColumn.SetCurrGroup")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrGroup = v
	th.m_changed = true
	return
}
func (th *dbPlayerSignColumn)GetAwardIndex( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSignColumn.GetAwardIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.AwardIndex
	return
}
func (th *dbPlayerSignColumn)SetAwardIndex(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerSignColumn.SetAwardIndex")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.AwardIndex = v
	th.m_changed = true
	return
}
func (th *dbPlayerSignColumn)GetSignedIndex( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSignColumn.GetSignedIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.SignedIndex
	return
}
func (th *dbPlayerSignColumn)SetSignedIndex(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerSignColumn.SetSignedIndex")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.SignedIndex = v
	th.m_changed = true
	return
}
func (th *dbPlayerSignColumn)GetLastSignedTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSignColumn.GetLastSignedTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastSignedTime
	return
}
func (th *dbPlayerSignColumn)SetLastSignedTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerSignColumn.SetLastSignedTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastSignedTime = v
	th.m_changed = true
	return
}
type dbPlayerSevenDaysColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerSevenDaysData
	m_changed bool
}
func (th *dbPlayerSevenDaysColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerSevenDaysData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerSevenDays{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerSevenDaysData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerSevenDaysColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerSevenDaysColumn)Get( )(v *dbPlayerSevenDaysData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSevenDaysColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerSevenDaysData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerSevenDaysColumn)Set(v dbPlayerSevenDaysData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerSevenDaysColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerSevenDaysData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerSevenDaysColumn)GetAwards( )(v []int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSevenDaysColumn.GetAwards")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = make([]int32, len(th.m_data.Awards))
	for _ii, _vv := range th.m_data.Awards {
		v[_ii]=_vv
	}
	return
}
func (th *dbPlayerSevenDaysColumn)SetAwards(v []int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerSevenDaysColumn.SetAwards")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Awards = make([]int32, len(v))
	for _ii, _vv := range v {
		th.m_data.Awards[_ii]=_vv
	}
	th.m_changed = true
	return
}
func (th *dbPlayerSevenDaysColumn)GetDays( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSevenDaysColumn.GetDays")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.Days
	return
}
func (th *dbPlayerSevenDaysColumn)SetDays(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerSevenDaysColumn.SetDays")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Days = v
	th.m_changed = true
	return
}
type dbPlayerPayCommonColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerPayCommonData
	m_changed bool
}
func (th *dbPlayerPayCommonColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerPayCommonData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerPayCommon{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerPayCommonData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerPayCommonColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerPayCommonColumn)Get( )(v *dbPlayerPayCommonData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerPayCommonColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerPayCommonData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerPayCommonColumn)Set(v dbPlayerPayCommonData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayCommonColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerPayCommonData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerPayCommonColumn)GetFirstPayState( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerPayCommonColumn.GetFirstPayState")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.FirstPayState
	return
}
func (th *dbPlayerPayCommonColumn)SetFirstPayState(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayCommonColumn.SetFirstPayState")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.FirstPayState = v
	th.m_changed = true
	return
}
type dbPlayerPayColumn struct{
	m_row *dbPlayerRow
	m_data map[string]*dbPlayerPayData
	m_changed bool
}
func (th *dbPlayerPayColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerPayList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerPayData{}
		d.from_pb(v)
		th.m_data[string(d.BundleId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerPayColumn)save( )(data []byte,err error){
	pb := &db.PlayerPayList{}
	pb.List=make([]*db.PlayerPay,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerPayColumn)HasIndex(id string)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerPayColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerPayColumn)GetAllIndex()(list []string){
	th.m_row.m_lock.UnSafeRLock("dbPlayerPayColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]string, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerPayColumn)GetAll()(list []dbPlayerPayData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerPayColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerPayData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerPayColumn)Get(id string)(v *dbPlayerPayData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerPayColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerPayData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerPayColumn)Set(v dbPlayerPayData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[string(v.BundleId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.BundleId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerPayColumn)Add(v *dbPlayerPayData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[string(v.BundleId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.BundleId)
		return false
	}
	d:=&dbPlayerPayData{}
	v.clone_to(d)
	th.m_data[string(v.BundleId)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerPayColumn)Remove(id string){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerPayColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[string]*dbPlayerPayData)
	th.m_changed = true
	return
}
func (th *dbPlayerPayColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerPayColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerPayColumn)GetLastPayedTime(id string)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerPayColumn.GetLastPayedTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.LastPayedTime
	return v,true
}
func (th *dbPlayerPayColumn)SetLastPayedTime(id string,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayColumn.SetLastPayedTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.LastPayedTime = v
	th.m_changed = true
	return true
}
func (th *dbPlayerPayColumn)GetLastAwardTime(id string)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerPayColumn.GetLastAwardTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.LastAwardTime
	return v,true
}
func (th *dbPlayerPayColumn)SetLastAwardTime(id string,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayColumn.SetLastAwardTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.LastAwardTime = v
	th.m_changed = true
	return true
}
func (th *dbPlayerPayColumn)GetSendMailNum(id string)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerPayColumn.GetSendMailNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.SendMailNum
	return v,true
}
func (th *dbPlayerPayColumn)SetSendMailNum(id string,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayColumn.SetSendMailNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.SendMailNum = v
	th.m_changed = true
	return true
}
func (th *dbPlayerPayColumn)IncbySendMailNum(id string,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayColumn.IncbySendMailNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerPayData{}
		th.m_data[id] = d
	}
	d.SendMailNum +=  v
	th.m_changed = true
	return d.SendMailNum
}
func (th *dbPlayerPayColumn)GetChargeNum(id string)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerPayColumn.GetChargeNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.ChargeNum
	return v,true
}
func (th *dbPlayerPayColumn)SetChargeNum(id string,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayColumn.SetChargeNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.ChargeNum = v
	th.m_changed = true
	return true
}
func (th *dbPlayerPayColumn)IncbyChargeNum(id string,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerPayColumn.IncbyChargeNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerPayData{}
		th.m_data[id] = d
	}
	d.ChargeNum +=  v
	th.m_changed = true
	return d.ChargeNum
}
type dbPlayerGuideDataColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerGuideDataData
	m_changed bool
}
func (th *dbPlayerGuideDataColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerGuideDataData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerGuideData{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerGuideDataData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerGuideDataColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerGuideDataColumn)Get( )(v *dbPlayerGuideDataData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuideDataColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerGuideDataData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerGuideDataColumn)Set(v dbPlayerGuideDataData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuideDataColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerGuideDataData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerGuideDataColumn)GetData( )(v []byte){
	th.m_row.m_lock.UnSafeRLock("dbPlayerGuideDataColumn.GetData")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = make([]byte, len(th.m_data.Data))
	for _ii, _vv := range th.m_data.Data {
		v[_ii]=_vv
	}
	return
}
func (th *dbPlayerGuideDataColumn)SetData(v []byte){
	th.m_row.m_lock.UnSafeLock("dbPlayerGuideDataColumn.SetData")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Data = make([]byte, len(v))
	for _ii, _vv := range v {
		th.m_data.Data[_ii]=_vv
	}
	th.m_changed = true
	return
}
type dbPlayerActivityDataColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerActivityDataData
	m_changed bool
}
func (th *dbPlayerActivityDataColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerActivityDataList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerActivityDataData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerActivityDataColumn)save( )(data []byte,err error){
	pb := &db.PlayerActivityDataList{}
	pb.List=make([]*db.PlayerActivityData,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerActivityDataColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActivityDataColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerActivityDataColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActivityDataColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerActivityDataColumn)GetAll()(list []dbPlayerActivityDataData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActivityDataColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerActivityDataData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerActivityDataColumn)Get(id int32)(v *dbPlayerActivityDataData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActivityDataColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerActivityDataData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerActivityDataColumn)Set(v dbPlayerActivityDataData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerActivityDataColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerActivityDataColumn)Add(v *dbPlayerActivityDataData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerActivityDataColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerActivityDataData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerActivityDataColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerActivityDataColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerActivityDataColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerActivityDataColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerActivityDataData)
	th.m_changed = true
	return
}
func (th *dbPlayerActivityDataColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActivityDataColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerActivityDataColumn)GetSubIds(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActivityDataColumn.GetSubIds")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.SubIds))
	for _ii, _vv := range d.SubIds {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerActivityDataColumn)SetSubIds(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerActivityDataColumn.SetSubIds")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.SubIds = make([]int32, len(v))
	for _ii, _vv := range v {
		d.SubIds[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerActivityDataColumn)GetSubValues(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActivityDataColumn.GetSubValues")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.SubValues))
	for _ii, _vv := range d.SubValues {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerActivityDataColumn)SetSubValues(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerActivityDataColumn.SetSubValues")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.SubValues = make([]int32, len(v))
	for _ii, _vv := range v {
		d.SubValues[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerActivityDataColumn)GetSubNum(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerActivityDataColumn.GetSubNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.SubNum
	return v,true
}
func (th *dbPlayerActivityDataColumn)SetSubNum(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerActivityDataColumn.SetSubNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.SubNum = v
	th.m_changed = true
	return true
}
func (th *dbPlayerActivityDataColumn)IncbySubNum(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerActivityDataColumn.IncbySubNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerActivityDataData{}
		th.m_data[id] = d
	}
	d.SubNum +=  v
	th.m_changed = true
	return d.SubNum
}
type dbPlayerExpeditionDataColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerExpeditionDataData
	m_changed bool
}
func (th *dbPlayerExpeditionDataColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerExpeditionDataData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerExpeditionData{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerExpeditionDataData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerExpeditionDataColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExpeditionDataColumn)Get( )(v *dbPlayerExpeditionDataData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionDataColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerExpeditionDataData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerExpeditionDataColumn)Set(v dbPlayerExpeditionDataData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionDataColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerExpeditionDataData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerExpeditionDataColumn)GetRefreshTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionDataColumn.GetRefreshTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.RefreshTime
	return
}
func (th *dbPlayerExpeditionDataColumn)SetRefreshTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionDataColumn.SetRefreshTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.RefreshTime = v
	th.m_changed = true
	return
}
func (th *dbPlayerExpeditionDataColumn)GetCurrLevel( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionDataColumn.GetCurrLevel")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.CurrLevel
	return
}
func (th *dbPlayerExpeditionDataColumn)SetCurrLevel(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionDataColumn.SetCurrLevel")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrLevel = v
	th.m_changed = true
	return
}
func (th *dbPlayerExpeditionDataColumn)IncbyCurrLevel(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionDataColumn.IncbyCurrLevel")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrLevel += v
	th.m_changed = true
	return th.m_data.CurrLevel
}
func (th *dbPlayerExpeditionDataColumn)GetPurifyPoints( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionDataColumn.GetPurifyPoints")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.PurifyPoints
	return
}
func (th *dbPlayerExpeditionDataColumn)SetPurifyPoints(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionDataColumn.SetPurifyPoints")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.PurifyPoints = v
	th.m_changed = true
	return
}
func (th *dbPlayerExpeditionDataColumn)IncbyPurifyPoints(v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionDataColumn.IncbyPurifyPoints")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.PurifyPoints += v
	th.m_changed = true
	return th.m_data.PurifyPoints
}
type dbPlayerExpeditionRoleColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerExpeditionRoleData
	m_changed bool
}
func (th *dbPlayerExpeditionRoleColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerExpeditionRoleList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerExpeditionRoleData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExpeditionRoleColumn)save( )(data []byte,err error){
	pb := &db.PlayerExpeditionRoleList{}
	pb.List=make([]*db.PlayerExpeditionRole,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExpeditionRoleColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionRoleColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerExpeditionRoleColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionRoleColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerExpeditionRoleColumn)GetAll()(list []dbPlayerExpeditionRoleData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionRoleColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerExpeditionRoleData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerExpeditionRoleColumn)Get(id int32)(v *dbPlayerExpeditionRoleData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionRoleColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerExpeditionRoleData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerExpeditionRoleColumn)Set(v dbPlayerExpeditionRoleData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionRoleColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionRoleColumn)Add(v *dbPlayerExpeditionRoleData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionRoleColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerExpeditionRoleData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionRoleColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionRoleColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerExpeditionRoleColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionRoleColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerExpeditionRoleData)
	th.m_changed = true
	return
}
func (th *dbPlayerExpeditionRoleColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionRoleColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerExpeditionRoleColumn)GetHP(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionRoleColumn.GetHP")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.HP
	return v,true
}
func (th *dbPlayerExpeditionRoleColumn)SetHP(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionRoleColumn.SetHP")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.HP = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionRoleColumn)GetWeak(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionRoleColumn.GetWeak")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Weak
	return v,true
}
func (th *dbPlayerExpeditionRoleColumn)SetWeak(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionRoleColumn.SetWeak")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Weak = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionRoleColumn)GetHpPercent(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionRoleColumn.GetHpPercent")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.HpPercent
	return v,true
}
func (th *dbPlayerExpeditionRoleColumn)SetHpPercent(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionRoleColumn.SetHpPercent")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.HpPercent = v
	th.m_changed = true
	return true
}
type dbPlayerExpeditionLevelColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerExpeditionLevelData
	m_changed bool
}
func (th *dbPlayerExpeditionLevelColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerExpeditionLevelList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerExpeditionLevelData{}
		d.from_pb(v)
		th.m_data[int32(d.Level)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExpeditionLevelColumn)save( )(data []byte,err error){
	pb := &db.PlayerExpeditionLevelList{}
	pb.List=make([]*db.PlayerExpeditionLevel,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExpeditionLevelColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerExpeditionLevelColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerExpeditionLevelColumn)GetAll()(list []dbPlayerExpeditionLevelData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerExpeditionLevelData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerExpeditionLevelColumn)Get(id int32)(v *dbPlayerExpeditionLevelData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerExpeditionLevelData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerExpeditionLevelColumn)Set(v dbPlayerExpeditionLevelData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Level)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Level)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelColumn)Add(v *dbPlayerExpeditionLevelData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Level)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Level)
		return false
	}
	d:=&dbPlayerExpeditionLevelData{}
	v.clone_to(d)
	th.m_data[int32(v.Level)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerExpeditionLevelColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerExpeditionLevelData)
	th.m_changed = true
	return
}
func (th *dbPlayerExpeditionLevelColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerExpeditionLevelColumn)GetPlayerId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelColumn.GetPlayerId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.PlayerId
	return v,true
}
func (th *dbPlayerExpeditionLevelColumn)SetPlayerId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelColumn.SetPlayerId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.PlayerId = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelColumn)GetPower(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelColumn.GetPower")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Power
	return v,true
}
func (th *dbPlayerExpeditionLevelColumn)SetPower(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelColumn.SetPower")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Power = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelColumn)GetGoldIncome(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelColumn.GetGoldIncome")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.GoldIncome
	return v,true
}
func (th *dbPlayerExpeditionLevelColumn)SetGoldIncome(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelColumn.SetGoldIncome")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.GoldIncome = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelColumn)GetExpeditionGoldIncome(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelColumn.GetExpeditionGoldIncome")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.ExpeditionGoldIncome
	return v,true
}
func (th *dbPlayerExpeditionLevelColumn)SetExpeditionGoldIncome(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelColumn.SetExpeditionGoldIncome")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.ExpeditionGoldIncome = v
	th.m_changed = true
	return true
}
type dbPlayerExpeditionLevelRoleColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerExpeditionLevelRoleData
	m_changed bool
}
func (th *dbPlayerExpeditionLevelRoleColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerExpeditionLevelRoleList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerExpeditionLevelRoleData{}
		d.from_pb(v)
		th.m_data[int32(d.Pos)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExpeditionLevelRoleColumn)save( )(data []byte,err error){
	pb := &db.PlayerExpeditionLevelRoleList{}
	pb.List=make([]*db.PlayerExpeditionLevelRole,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerExpeditionLevelRoleColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelRoleColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerExpeditionLevelRoleColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelRoleColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerExpeditionLevelRoleColumn)GetAll()(list []dbPlayerExpeditionLevelRoleData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelRoleColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerExpeditionLevelRoleData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerExpeditionLevelRoleColumn)Get(id int32)(v *dbPlayerExpeditionLevelRoleData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelRoleColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerExpeditionLevelRoleData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerExpeditionLevelRoleColumn)Set(v dbPlayerExpeditionLevelRoleData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelRoleColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Pos)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Pos)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelRoleColumn)Add(v *dbPlayerExpeditionLevelRoleData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelRoleColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Pos)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Pos)
		return false
	}
	d:=&dbPlayerExpeditionLevelRoleData{}
	v.clone_to(d)
	th.m_data[int32(v.Pos)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelRoleColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelRoleColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerExpeditionLevelRoleColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelRoleColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerExpeditionLevelRoleData)
	th.m_changed = true
	return
}
func (th *dbPlayerExpeditionLevelRoleColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelRoleColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerExpeditionLevelRoleColumn)GetTableId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelRoleColumn.GetTableId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.TableId
	return v,true
}
func (th *dbPlayerExpeditionLevelRoleColumn)SetTableId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelRoleColumn.SetTableId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.TableId = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelRoleColumn)GetRank(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelRoleColumn.GetRank")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Rank
	return v,true
}
func (th *dbPlayerExpeditionLevelRoleColumn)SetRank(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelRoleColumn.SetRank")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Rank = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelRoleColumn)GetLevel(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelRoleColumn.GetLevel")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Level
	return v,true
}
func (th *dbPlayerExpeditionLevelRoleColumn)SetLevel(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelRoleColumn.SetLevel")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Level = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelRoleColumn)GetEquip(id int32)(v []int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelRoleColumn.GetEquip")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = make([]int32, len(d.Equip))
	for _ii, _vv := range d.Equip {
		v[_ii]=_vv
	}
	return v,true
}
func (th *dbPlayerExpeditionLevelRoleColumn)SetEquip(id int32,v []int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelRoleColumn.SetEquip")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Equip = make([]int32, len(v))
	for _ii, _vv := range v {
		d.Equip[_ii]=_vv
	}
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelRoleColumn)GetHP(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelRoleColumn.GetHP")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.HP
	return v,true
}
func (th *dbPlayerExpeditionLevelRoleColumn)SetHP(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelRoleColumn.SetHP")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.HP = v
	th.m_changed = true
	return true
}
func (th *dbPlayerExpeditionLevelRoleColumn)GetHpPercent(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerExpeditionLevelRoleColumn.GetHpPercent")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.HpPercent
	return v,true
}
func (th *dbPlayerExpeditionLevelRoleColumn)SetHpPercent(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerExpeditionLevelRoleColumn.SetHpPercent")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.HpPercent = v
	th.m_changed = true
	return true
}
type dbPlayerSysMailColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerSysMailData
	m_changed bool
}
func (th *dbPlayerSysMailColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerSysMailData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerSysMail{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerSysMailData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerSysMailColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerSysMailColumn)Get( )(v *dbPlayerSysMailData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSysMailColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerSysMailData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerSysMailColumn)Set(v dbPlayerSysMailData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerSysMailColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerSysMailData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerSysMailColumn)GetCurrId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerSysMailColumn.GetCurrId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.CurrId
	return
}
func (th *dbPlayerSysMailColumn)SetCurrId(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerSysMailColumn.SetCurrId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.CurrId = v
	th.m_changed = true
	return
}
type dbPlayerArtifactColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerArtifactData
	m_changed bool
}
func (th *dbPlayerArtifactColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerArtifactList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerArtifactData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerArtifactColumn)save( )(data []byte,err error){
	pb := &db.PlayerArtifactList{}
	pb.List=make([]*db.PlayerArtifact,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerArtifactColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArtifactColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerArtifactColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArtifactColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerArtifactColumn)GetAll()(list []dbPlayerArtifactData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArtifactColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerArtifactData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerArtifactColumn)Get(id int32)(v *dbPlayerArtifactData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArtifactColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerArtifactData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerArtifactColumn)Set(v dbPlayerArtifactData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerArtifactColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerArtifactColumn)Add(v *dbPlayerArtifactData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerArtifactColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerArtifactData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerArtifactColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArtifactColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerArtifactColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerArtifactColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerArtifactData)
	th.m_changed = true
	return
}
func (th *dbPlayerArtifactColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArtifactColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerArtifactColumn)GetRank(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArtifactColumn.GetRank")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Rank
	return v,true
}
func (th *dbPlayerArtifactColumn)SetRank(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerArtifactColumn.SetRank")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Rank = v
	th.m_changed = true
	return true
}
func (th *dbPlayerArtifactColumn)IncbyRank(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArtifactColumn.IncbyRank")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerArtifactData{}
		th.m_data[id] = d
	}
	d.Rank +=  v
	th.m_changed = true
	return d.Rank
}
func (th *dbPlayerArtifactColumn)GetLevel(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerArtifactColumn.GetLevel")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Level
	return v,true
}
func (th *dbPlayerArtifactColumn)SetLevel(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerArtifactColumn.SetLevel")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Level = v
	th.m_changed = true
	return true
}
func (th *dbPlayerArtifactColumn)IncbyLevel(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerArtifactColumn.IncbyLevel")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerArtifactData{}
		th.m_data[id] = d
	}
	d.Level +=  v
	th.m_changed = true
	return d.Level
}
type dbPlayerCarnivalCommonColumn struct{
	m_row *dbPlayerRow
	m_data *dbPlayerCarnivalCommonData
	m_changed bool
}
func (th *dbPlayerCarnivalCommonColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbPlayerCarnivalCommonData{}
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerCarnivalCommon{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_data = &dbPlayerCarnivalCommonData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbPlayerCarnivalCommonColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerCarnivalCommonColumn)Get( )(v *dbPlayerCarnivalCommonData ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCarnivalCommonColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbPlayerCarnivalCommonData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbPlayerCarnivalCommonColumn)Set(v dbPlayerCarnivalCommonData ){
	th.m_row.m_lock.UnSafeLock("dbPlayerCarnivalCommonColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbPlayerCarnivalCommonData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbPlayerCarnivalCommonColumn)GetDayResetTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCarnivalCommonColumn.GetDayResetTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.DayResetTime
	return
}
func (th *dbPlayerCarnivalCommonColumn)SetDayResetTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCarnivalCommonColumn.SetDayResetTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.DayResetTime = v
	th.m_changed = true
	return
}
type dbPlayerCarnivalColumn struct{
	m_row *dbPlayerRow
	m_data map[int32]*dbPlayerCarnivalData
	m_changed bool
}
func (th *dbPlayerCarnivalColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerCarnivalList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerCarnivalData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerCarnivalColumn)save( )(data []byte,err error){
	pb := &db.PlayerCarnivalList{}
	pb.List=make([]*db.PlayerCarnival,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerCarnivalColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCarnivalColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerCarnivalColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCarnivalColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerCarnivalColumn)GetAll()(list []dbPlayerCarnivalData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCarnivalColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerCarnivalData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerCarnivalColumn)Get(id int32)(v *dbPlayerCarnivalData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCarnivalColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerCarnivalData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerCarnivalColumn)Set(v dbPlayerCarnivalData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCarnivalColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerCarnivalColumn)Add(v *dbPlayerCarnivalData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCarnivalColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.Id)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Id)
		return false
	}
	d:=&dbPlayerCarnivalData{}
	v.clone_to(d)
	th.m_data[int32(v.Id)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerCarnivalColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCarnivalColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerCarnivalColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerCarnivalColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbPlayerCarnivalData)
	th.m_changed = true
	return
}
func (th *dbPlayerCarnivalColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCarnivalColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbPlayerCarnivalColumn)GetValue(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCarnivalColumn.GetValue")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Value
	return v,true
}
func (th *dbPlayerCarnivalColumn)SetValue(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCarnivalColumn.SetValue")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Value = v
	th.m_changed = true
	return true
}
func (th *dbPlayerCarnivalColumn)IncbyValue(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCarnivalColumn.IncbyValue")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerCarnivalData{}
		th.m_data[id] = d
	}
	d.Value +=  v
	th.m_changed = true
	return d.Value
}
func (th *dbPlayerCarnivalColumn)GetValue2(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerCarnivalColumn.GetValue2")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Value2
	return v,true
}
func (th *dbPlayerCarnivalColumn)SetValue2(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerCarnivalColumn.SetValue2")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), id)
		return
	}
	d.Value2 = v
	th.m_changed = true
	return true
}
func (th *dbPlayerCarnivalColumn)IncbyValue2(id int32,v int32)(r int32){
	th.m_row.m_lock.UnSafeLock("dbPlayerCarnivalColumn.IncbyValue2")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		d = &dbPlayerCarnivalData{}
		th.m_data[id] = d
	}
	d.Value2 +=  v
	th.m_changed = true
	return d.Value2
}
type dbPlayerInviteCodesColumn struct{
	m_row *dbPlayerRow
	m_data map[string]*dbPlayerInviteCodesData
	m_changed bool
}
func (th *dbPlayerInviteCodesColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.PlayerInviteCodesList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetPlayerId())
		return
	}
	for _, v := range pb.List {
		d := &dbPlayerInviteCodesData{}
		d.from_pb(v)
		th.m_data[string(d.Code)] = d
	}
	th.m_changed = false
	return
}
func (th *dbPlayerInviteCodesColumn)save( )(data []byte,err error){
	pb := &db.PlayerInviteCodesList{}
	pb.List=make([]*db.PlayerInviteCodes,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetPlayerId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbPlayerInviteCodesColumn)HasIndex(id string)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInviteCodesColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbPlayerInviteCodesColumn)GetAllIndex()(list []string){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInviteCodesColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]string, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbPlayerInviteCodesColumn)GetAll()(list []dbPlayerInviteCodesData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInviteCodesColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbPlayerInviteCodesData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbPlayerInviteCodesColumn)Get(id string)(v *dbPlayerInviteCodesData){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInviteCodesColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbPlayerInviteCodesData{}
	d.clone_to(v)
	return
}
func (th *dbPlayerInviteCodesColumn)Set(v dbPlayerInviteCodesData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerInviteCodesColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[string(v.Code)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetPlayerId(), v.Code)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbPlayerInviteCodesColumn)Add(v *dbPlayerInviteCodesData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbPlayerInviteCodesColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[string(v.Code)]
	if has {
		log.Error("already added %v %v",th.m_row.GetPlayerId(), v.Code)
		return false
	}
	d:=&dbPlayerInviteCodesData{}
	v.clone_to(d)
	th.m_data[string(v.Code)]=d
	th.m_changed = true
	return true
}
func (th *dbPlayerInviteCodesColumn)Remove(id string){
	th.m_row.m_lock.UnSafeLock("dbPlayerInviteCodesColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbPlayerInviteCodesColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbPlayerInviteCodesColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[string]*dbPlayerInviteCodesData)
	th.m_changed = true
	return
}
func (th *dbPlayerInviteCodesColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbPlayerInviteCodesColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
type dbPlayerRow struct {
	m_table *dbPlayerTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_PlayerId        int32
	m_UniqueId_changed bool
	m_UniqueId string
	m_Account_changed bool
	m_Account string
	m_Name_changed bool
	m_Name string
	m_Token_changed bool
	m_Token string
	m_CurrReplyMsgNum_changed bool
	m_CurrReplyMsgNum int32
	Info dbPlayerInfoColumn
	Global dbPlayerGlobalColumn
	m_Level_changed bool
	m_Level int32
	Items dbPlayerItemColumn
	RoleCommon dbPlayerRoleCommonColumn
	Roles dbPlayerRoleColumn
	RoleHandbook dbPlayerRoleHandbookColumn
	BattleTeam dbPlayerBattleTeamColumn
	CampaignCommon dbPlayerCampaignCommonColumn
	Campaigns dbPlayerCampaignColumn
	CampaignStaticIncomes dbPlayerCampaignStaticIncomeColumn
	CampaignRandomIncomes dbPlayerCampaignRandomIncomeColumn
	MailCommon dbPlayerMailCommonColumn
	Mails dbPlayerMailColumn
	BattleSaves dbPlayerBattleSaveColumn
	Talents dbPlayerTalentColumn
	TowerCommon dbPlayerTowerCommonColumn
	Towers dbPlayerTowerColumn
	Draws dbPlayerDrawColumn
	GoldHand dbPlayerGoldHandColumn
	Shops dbPlayerShopColumn
	ShopItems dbPlayerShopItemColumn
	Arena dbPlayerArenaColumn
	Equip dbPlayerEquipColumn
	ActiveStageCommon dbPlayerActiveStageCommonColumn
	ActiveStages dbPlayerActiveStageColumn
	FriendCommon dbPlayerFriendCommonColumn
	Friends dbPlayerFriendColumn
	FriendRecommends dbPlayerFriendRecommendColumn
	FriendAsks dbPlayerFriendAskColumn
	FriendBosss dbPlayerFriendBossColumn
	TaskCommon dbPlayerTaskCommonColumn
	Tasks dbPlayerTaskColumn
	FinishedTasks dbPlayerFinishedTaskColumn
	DailyTaskAllDailys dbPlayerDailyTaskAllDailyColumn
	ExploreCommon dbPlayerExploreCommonColumn
	Explores dbPlayerExploreColumn
	ExploreStorys dbPlayerExploreStoryColumn
	FriendChatUnreadIds dbPlayerFriendChatUnreadIdColumn
	FriendChatUnreadMessages dbPlayerFriendChatUnreadMessageColumn
	HeadItems dbPlayerHeadItemColumn
	SuitAwards dbPlayerSuitAwardColumn
	Chats dbPlayerChatColumn
	Anouncement dbPlayerAnouncementColumn
	FirstDrawCards dbPlayerFirstDrawCardColumn
	Guild dbPlayerGuildColumn
	GuildStage dbPlayerGuildStageColumn
	Sign dbPlayerSignColumn
	SevenDays dbPlayerSevenDaysColumn
	PayCommon dbPlayerPayCommonColumn
	Pays dbPlayerPayColumn
	GuideData dbPlayerGuideDataColumn
	ActivityDatas dbPlayerActivityDataColumn
	ExpeditionData dbPlayerExpeditionDataColumn
	ExpeditionRoles dbPlayerExpeditionRoleColumn
	ExpeditionLevels dbPlayerExpeditionLevelColumn
	ExpeditionLevelRole0s dbPlayerExpeditionLevelRoleColumn
	ExpeditionLevelRole1s dbPlayerExpeditionLevelRoleColumn
	ExpeditionLevelRole2s dbPlayerExpeditionLevelRoleColumn
	ExpeditionLevelRole3s dbPlayerExpeditionLevelRoleColumn
	ExpeditionLevelRole4s dbPlayerExpeditionLevelRoleColumn
	ExpeditionLevelRole5s dbPlayerExpeditionLevelRoleColumn
	ExpeditionLevelRole6s dbPlayerExpeditionLevelRoleColumn
	ExpeditionLevelRole7s dbPlayerExpeditionLevelRoleColumn
	ExpeditionLevelRole8s dbPlayerExpeditionLevelRoleColumn
	ExpeditionLevelRole9s dbPlayerExpeditionLevelRoleColumn
	SysMail dbPlayerSysMailColumn
	Artifacts dbPlayerArtifactColumn
	CarnivalCommon dbPlayerCarnivalCommonColumn
	Carnivals dbPlayerCarnivalColumn
	InviteCodess dbPlayerInviteCodesColumn
}
func new_dbPlayerRow(table *dbPlayerTable, PlayerId int32) (r *dbPlayerRow) {
	th := &dbPlayerRow{}
	th.m_table = table
	th.m_PlayerId = PlayerId
	th.m_lock = NewRWMutex()
	th.m_UniqueId_changed=true
	th.m_Account_changed=true
	th.m_Name_changed=true
	th.m_Token_changed=true
	th.m_CurrReplyMsgNum_changed=true
	th.m_Level_changed=true
	th.Info.m_row=th
	th.Info.m_data=&dbPlayerInfoData{}
	th.Global.m_row=th
	th.Global.m_data=&dbPlayerGlobalData{}
	th.Items.m_row=th
	th.Items.m_data=make(map[int32]*dbPlayerItemData)
	th.RoleCommon.m_row=th
	th.RoleCommon.m_data=&dbPlayerRoleCommonData{}
	th.Roles.m_row=th
	th.Roles.m_data=make(map[int32]*dbPlayerRoleData)
	th.RoleHandbook.m_row=th
	th.RoleHandbook.m_data=&dbPlayerRoleHandbookData{}
	th.BattleTeam.m_row=th
	th.BattleTeam.m_data=&dbPlayerBattleTeamData{}
	th.CampaignCommon.m_row=th
	th.CampaignCommon.m_data=&dbPlayerCampaignCommonData{}
	th.Campaigns.m_row=th
	th.Campaigns.m_data=make(map[int32]*dbPlayerCampaignData)
	th.CampaignStaticIncomes.m_row=th
	th.CampaignStaticIncomes.m_data=make(map[int32]*dbPlayerCampaignStaticIncomeData)
	th.CampaignRandomIncomes.m_row=th
	th.CampaignRandomIncomes.m_data=make(map[int32]*dbPlayerCampaignRandomIncomeData)
	th.MailCommon.m_row=th
	th.MailCommon.m_data=&dbPlayerMailCommonData{}
	th.Mails.m_row=th
	th.Mails.m_data=make(map[int32]*dbPlayerMailData)
	th.BattleSaves.m_row=th
	th.BattleSaves.m_data=make(map[int32]*dbPlayerBattleSaveData)
	th.Talents.m_row=th
	th.Talents.m_data=make(map[int32]*dbPlayerTalentData)
	th.TowerCommon.m_row=th
	th.TowerCommon.m_data=&dbPlayerTowerCommonData{}
	th.Towers.m_row=th
	th.Towers.m_data=make(map[int32]*dbPlayerTowerData)
	th.Draws.m_row=th
	th.Draws.m_data=make(map[int32]*dbPlayerDrawData)
	th.GoldHand.m_row=th
	th.GoldHand.m_data=&dbPlayerGoldHandData{}
	th.Shops.m_row=th
	th.Shops.m_data=make(map[int32]*dbPlayerShopData)
	th.ShopItems.m_row=th
	th.ShopItems.m_data=make(map[int32]*dbPlayerShopItemData)
	th.Arena.m_row=th
	th.Arena.m_data=&dbPlayerArenaData{}
	th.Equip.m_row=th
	th.Equip.m_data=&dbPlayerEquipData{}
	th.ActiveStageCommon.m_row=th
	th.ActiveStageCommon.m_data=&dbPlayerActiveStageCommonData{}
	th.ActiveStages.m_row=th
	th.ActiveStages.m_data=make(map[int32]*dbPlayerActiveStageData)
	th.FriendCommon.m_row=th
	th.FriendCommon.m_data=&dbPlayerFriendCommonData{}
	th.Friends.m_row=th
	th.Friends.m_data=make(map[int32]*dbPlayerFriendData)
	th.FriendRecommends.m_row=th
	th.FriendRecommends.m_data=make(map[int32]*dbPlayerFriendRecommendData)
	th.FriendAsks.m_row=th
	th.FriendAsks.m_data=make(map[int32]*dbPlayerFriendAskData)
	th.FriendBosss.m_row=th
	th.FriendBosss.m_data=make(map[int32]*dbPlayerFriendBossData)
	th.TaskCommon.m_row=th
	th.TaskCommon.m_data=&dbPlayerTaskCommonData{}
	th.Tasks.m_row=th
	th.Tasks.m_data=make(map[int32]*dbPlayerTaskData)
	th.FinishedTasks.m_row=th
	th.FinishedTasks.m_data=make(map[int32]*dbPlayerFinishedTaskData)
	th.DailyTaskAllDailys.m_row=th
	th.DailyTaskAllDailys.m_data=make(map[int32]*dbPlayerDailyTaskAllDailyData)
	th.ExploreCommon.m_row=th
	th.ExploreCommon.m_data=&dbPlayerExploreCommonData{}
	th.Explores.m_row=th
	th.Explores.m_data=make(map[int32]*dbPlayerExploreData)
	th.ExploreStorys.m_row=th
	th.ExploreStorys.m_data=make(map[int32]*dbPlayerExploreStoryData)
	th.FriendChatUnreadIds.m_row=th
	th.FriendChatUnreadIds.m_data=make(map[int32]*dbPlayerFriendChatUnreadIdData)
	th.FriendChatUnreadMessages.m_row=th
	th.FriendChatUnreadMessages.m_data=make(map[int64]*dbPlayerFriendChatUnreadMessageData)
	th.HeadItems.m_row=th
	th.HeadItems.m_data=make(map[int32]*dbPlayerHeadItemData)
	th.SuitAwards.m_row=th
	th.SuitAwards.m_data=make(map[int32]*dbPlayerSuitAwardData)
	th.Chats.m_row=th
	th.Chats.m_data=make(map[int32]*dbPlayerChatData)
	th.Anouncement.m_row=th
	th.Anouncement.m_data=&dbPlayerAnouncementData{}
	th.FirstDrawCards.m_row=th
	th.FirstDrawCards.m_data=make(map[int32]*dbPlayerFirstDrawCardData)
	th.Guild.m_row=th
	th.Guild.m_data=&dbPlayerGuildData{}
	th.GuildStage.m_row=th
	th.GuildStage.m_data=&dbPlayerGuildStageData{}
	th.Sign.m_row=th
	th.Sign.m_data=&dbPlayerSignData{}
	th.SevenDays.m_row=th
	th.SevenDays.m_data=&dbPlayerSevenDaysData{}
	th.PayCommon.m_row=th
	th.PayCommon.m_data=&dbPlayerPayCommonData{}
	th.Pays.m_row=th
	th.Pays.m_data=make(map[string]*dbPlayerPayData)
	th.GuideData.m_row=th
	th.GuideData.m_data=&dbPlayerGuideDataData{}
	th.ActivityDatas.m_row=th
	th.ActivityDatas.m_data=make(map[int32]*dbPlayerActivityDataData)
	th.ExpeditionData.m_row=th
	th.ExpeditionData.m_data=&dbPlayerExpeditionDataData{}
	th.ExpeditionRoles.m_row=th
	th.ExpeditionRoles.m_data=make(map[int32]*dbPlayerExpeditionRoleData)
	th.ExpeditionLevels.m_row=th
	th.ExpeditionLevels.m_data=make(map[int32]*dbPlayerExpeditionLevelData)
	th.ExpeditionLevelRole0s.m_row=th
	th.ExpeditionLevelRole0s.m_data=make(map[int32]*dbPlayerExpeditionLevelRoleData)
	th.ExpeditionLevelRole1s.m_row=th
	th.ExpeditionLevelRole1s.m_data=make(map[int32]*dbPlayerExpeditionLevelRoleData)
	th.ExpeditionLevelRole2s.m_row=th
	th.ExpeditionLevelRole2s.m_data=make(map[int32]*dbPlayerExpeditionLevelRoleData)
	th.ExpeditionLevelRole3s.m_row=th
	th.ExpeditionLevelRole3s.m_data=make(map[int32]*dbPlayerExpeditionLevelRoleData)
	th.ExpeditionLevelRole4s.m_row=th
	th.ExpeditionLevelRole4s.m_data=make(map[int32]*dbPlayerExpeditionLevelRoleData)
	th.ExpeditionLevelRole5s.m_row=th
	th.ExpeditionLevelRole5s.m_data=make(map[int32]*dbPlayerExpeditionLevelRoleData)
	th.ExpeditionLevelRole6s.m_row=th
	th.ExpeditionLevelRole6s.m_data=make(map[int32]*dbPlayerExpeditionLevelRoleData)
	th.ExpeditionLevelRole7s.m_row=th
	th.ExpeditionLevelRole7s.m_data=make(map[int32]*dbPlayerExpeditionLevelRoleData)
	th.ExpeditionLevelRole8s.m_row=th
	th.ExpeditionLevelRole8s.m_data=make(map[int32]*dbPlayerExpeditionLevelRoleData)
	th.ExpeditionLevelRole9s.m_row=th
	th.ExpeditionLevelRole9s.m_data=make(map[int32]*dbPlayerExpeditionLevelRoleData)
	th.SysMail.m_row=th
	th.SysMail.m_data=&dbPlayerSysMailData{}
	th.Artifacts.m_row=th
	th.Artifacts.m_data=make(map[int32]*dbPlayerArtifactData)
	th.CarnivalCommon.m_row=th
	th.CarnivalCommon.m_data=&dbPlayerCarnivalCommonData{}
	th.Carnivals.m_row=th
	th.Carnivals.m_data=make(map[int32]*dbPlayerCarnivalData)
	th.InviteCodess.m_row=th
	th.InviteCodess.m_data=make(map[string]*dbPlayerInviteCodesData)
	return th
}
func (th *dbPlayerRow) GetPlayerId() (r int32) {
	return th.m_PlayerId
}
func (th *dbPlayerRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbPlayerRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(77)
		db_args.Push(th.m_PlayerId)
		db_args.Push(th.m_UniqueId)
		db_args.Push(th.m_Account)
		db_args.Push(th.m_Name)
		db_args.Push(th.m_Token)
		db_args.Push(th.m_CurrReplyMsgNum)
		dInfo,db_err:=th.Info.save()
		if db_err!=nil{
			log.Error("insert save Info failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dInfo)
		dGlobal,db_err:=th.Global.save()
		if db_err!=nil{
			log.Error("insert save Global failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dGlobal)
		db_args.Push(th.m_Level)
		dItems,db_err:=th.Items.save()
		if db_err!=nil{
			log.Error("insert save Item failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dItems)
		dRoleCommon,db_err:=th.RoleCommon.save()
		if db_err!=nil{
			log.Error("insert save RoleCommon failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dRoleCommon)
		dRoles,db_err:=th.Roles.save()
		if db_err!=nil{
			log.Error("insert save Role failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dRoles)
		dRoleHandbook,db_err:=th.RoleHandbook.save()
		if db_err!=nil{
			log.Error("insert save RoleHandbook failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dRoleHandbook)
		dBattleTeam,db_err:=th.BattleTeam.save()
		if db_err!=nil{
			log.Error("insert save BattleTeam failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dBattleTeam)
		dCampaignCommon,db_err:=th.CampaignCommon.save()
		if db_err!=nil{
			log.Error("insert save CampaignCommon failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dCampaignCommon)
		dCampaigns,db_err:=th.Campaigns.save()
		if db_err!=nil{
			log.Error("insert save Campaign failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dCampaigns)
		dCampaignStaticIncomes,db_err:=th.CampaignStaticIncomes.save()
		if db_err!=nil{
			log.Error("insert save CampaignStaticIncome failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dCampaignStaticIncomes)
		dCampaignRandomIncomes,db_err:=th.CampaignRandomIncomes.save()
		if db_err!=nil{
			log.Error("insert save CampaignRandomIncome failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dCampaignRandomIncomes)
		dMailCommon,db_err:=th.MailCommon.save()
		if db_err!=nil{
			log.Error("insert save MailCommon failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dMailCommon)
		dMails,db_err:=th.Mails.save()
		if db_err!=nil{
			log.Error("insert save Mail failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dMails)
		dBattleSaves,db_err:=th.BattleSaves.save()
		if db_err!=nil{
			log.Error("insert save BattleSave failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dBattleSaves)
		dTalents,db_err:=th.Talents.save()
		if db_err!=nil{
			log.Error("insert save Talent failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dTalents)
		dTowerCommon,db_err:=th.TowerCommon.save()
		if db_err!=nil{
			log.Error("insert save TowerCommon failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dTowerCommon)
		dTowers,db_err:=th.Towers.save()
		if db_err!=nil{
			log.Error("insert save Tower failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dTowers)
		dDraws,db_err:=th.Draws.save()
		if db_err!=nil{
			log.Error("insert save Draw failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dDraws)
		dGoldHand,db_err:=th.GoldHand.save()
		if db_err!=nil{
			log.Error("insert save GoldHand failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dGoldHand)
		dShops,db_err:=th.Shops.save()
		if db_err!=nil{
			log.Error("insert save Shop failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dShops)
		dShopItems,db_err:=th.ShopItems.save()
		if db_err!=nil{
			log.Error("insert save ShopItem failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dShopItems)
		dArena,db_err:=th.Arena.save()
		if db_err!=nil{
			log.Error("insert save Arena failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dArena)
		dEquip,db_err:=th.Equip.save()
		if db_err!=nil{
			log.Error("insert save Equip failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dEquip)
		dActiveStageCommon,db_err:=th.ActiveStageCommon.save()
		if db_err!=nil{
			log.Error("insert save ActiveStageCommon failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dActiveStageCommon)
		dActiveStages,db_err:=th.ActiveStages.save()
		if db_err!=nil{
			log.Error("insert save ActiveStage failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dActiveStages)
		dFriendCommon,db_err:=th.FriendCommon.save()
		if db_err!=nil{
			log.Error("insert save FriendCommon failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dFriendCommon)
		dFriends,db_err:=th.Friends.save()
		if db_err!=nil{
			log.Error("insert save Friend failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dFriends)
		dFriendRecommends,db_err:=th.FriendRecommends.save()
		if db_err!=nil{
			log.Error("insert save FriendRecommend failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dFriendRecommends)
		dFriendAsks,db_err:=th.FriendAsks.save()
		if db_err!=nil{
			log.Error("insert save FriendAsk failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dFriendAsks)
		dFriendBosss,db_err:=th.FriendBosss.save()
		if db_err!=nil{
			log.Error("insert save FriendBoss failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dFriendBosss)
		dTaskCommon,db_err:=th.TaskCommon.save()
		if db_err!=nil{
			log.Error("insert save TaskCommon failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dTaskCommon)
		dTasks,db_err:=th.Tasks.save()
		if db_err!=nil{
			log.Error("insert save Task failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dTasks)
		dFinishedTasks,db_err:=th.FinishedTasks.save()
		if db_err!=nil{
			log.Error("insert save FinishedTask failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dFinishedTasks)
		dDailyTaskAllDailys,db_err:=th.DailyTaskAllDailys.save()
		if db_err!=nil{
			log.Error("insert save DailyTaskAllDaily failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dDailyTaskAllDailys)
		dExploreCommon,db_err:=th.ExploreCommon.save()
		if db_err!=nil{
			log.Error("insert save ExploreCommon failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExploreCommon)
		dExplores,db_err:=th.Explores.save()
		if db_err!=nil{
			log.Error("insert save Explore failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExplores)
		dExploreStorys,db_err:=th.ExploreStorys.save()
		if db_err!=nil{
			log.Error("insert save ExploreStory failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExploreStorys)
		dFriendChatUnreadIds,db_err:=th.FriendChatUnreadIds.save()
		if db_err!=nil{
			log.Error("insert save FriendChatUnreadId failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dFriendChatUnreadIds)
		dFriendChatUnreadMessages,db_err:=th.FriendChatUnreadMessages.save()
		if db_err!=nil{
			log.Error("insert save FriendChatUnreadMessage failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dFriendChatUnreadMessages)
		dHeadItems,db_err:=th.HeadItems.save()
		if db_err!=nil{
			log.Error("insert save HeadItem failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dHeadItems)
		dSuitAwards,db_err:=th.SuitAwards.save()
		if db_err!=nil{
			log.Error("insert save SuitAward failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dSuitAwards)
		dChats,db_err:=th.Chats.save()
		if db_err!=nil{
			log.Error("insert save Chat failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dChats)
		dAnouncement,db_err:=th.Anouncement.save()
		if db_err!=nil{
			log.Error("insert save Anouncement failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dAnouncement)
		dFirstDrawCards,db_err:=th.FirstDrawCards.save()
		if db_err!=nil{
			log.Error("insert save FirstDrawCard failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dFirstDrawCards)
		dGuild,db_err:=th.Guild.save()
		if db_err!=nil{
			log.Error("insert save Guild failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dGuild)
		dGuildStage,db_err:=th.GuildStage.save()
		if db_err!=nil{
			log.Error("insert save GuildStage failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dGuildStage)
		dSign,db_err:=th.Sign.save()
		if db_err!=nil{
			log.Error("insert save Sign failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dSign)
		dSevenDays,db_err:=th.SevenDays.save()
		if db_err!=nil{
			log.Error("insert save SevenDays failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dSevenDays)
		dPayCommon,db_err:=th.PayCommon.save()
		if db_err!=nil{
			log.Error("insert save PayCommon failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dPayCommon)
		dPays,db_err:=th.Pays.save()
		if db_err!=nil{
			log.Error("insert save Pay failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dPays)
		dGuideData,db_err:=th.GuideData.save()
		if db_err!=nil{
			log.Error("insert save GuideData failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dGuideData)
		dActivityDatas,db_err:=th.ActivityDatas.save()
		if db_err!=nil{
			log.Error("insert save ActivityData failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dActivityDatas)
		dExpeditionData,db_err:=th.ExpeditionData.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionData failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionData)
		dExpeditionRoles,db_err:=th.ExpeditionRoles.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionRole failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionRoles)
		dExpeditionLevels,db_err:=th.ExpeditionLevels.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionLevel failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionLevels)
		dExpeditionLevelRole0s,db_err:=th.ExpeditionLevelRole0s.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionLevelRole0 failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionLevelRole0s)
		dExpeditionLevelRole1s,db_err:=th.ExpeditionLevelRole1s.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionLevelRole1 failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionLevelRole1s)
		dExpeditionLevelRole2s,db_err:=th.ExpeditionLevelRole2s.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionLevelRole2 failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionLevelRole2s)
		dExpeditionLevelRole3s,db_err:=th.ExpeditionLevelRole3s.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionLevelRole3 failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionLevelRole3s)
		dExpeditionLevelRole4s,db_err:=th.ExpeditionLevelRole4s.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionLevelRole4 failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionLevelRole4s)
		dExpeditionLevelRole5s,db_err:=th.ExpeditionLevelRole5s.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionLevelRole5 failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionLevelRole5s)
		dExpeditionLevelRole6s,db_err:=th.ExpeditionLevelRole6s.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionLevelRole6 failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionLevelRole6s)
		dExpeditionLevelRole7s,db_err:=th.ExpeditionLevelRole7s.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionLevelRole7 failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionLevelRole7s)
		dExpeditionLevelRole8s,db_err:=th.ExpeditionLevelRole8s.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionLevelRole8 failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionLevelRole8s)
		dExpeditionLevelRole9s,db_err:=th.ExpeditionLevelRole9s.save()
		if db_err!=nil{
			log.Error("insert save ExpeditionLevelRole9 failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dExpeditionLevelRole9s)
		dSysMail,db_err:=th.SysMail.save()
		if db_err!=nil{
			log.Error("insert save SysMail failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dSysMail)
		dArtifacts,db_err:=th.Artifacts.save()
		if db_err!=nil{
			log.Error("insert save Artifact failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dArtifacts)
		dCarnivalCommon,db_err:=th.CarnivalCommon.save()
		if db_err!=nil{
			log.Error("insert save CarnivalCommon failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dCarnivalCommon)
		dCarnivals,db_err:=th.Carnivals.save()
		if db_err!=nil{
			log.Error("insert save Carnival failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dCarnivals)
		dInviteCodess,db_err:=th.InviteCodess.save()
		if db_err!=nil{
			log.Error("insert save InviteCodes failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dInviteCodess)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.m_UniqueId_changed||th.m_Account_changed||th.m_Name_changed||th.m_Token_changed||th.m_CurrReplyMsgNum_changed||th.Info.m_changed||th.Global.m_changed||th.m_Level_changed||th.Items.m_changed||th.RoleCommon.m_changed||th.Roles.m_changed||th.RoleHandbook.m_changed||th.BattleTeam.m_changed||th.CampaignCommon.m_changed||th.Campaigns.m_changed||th.CampaignStaticIncomes.m_changed||th.CampaignRandomIncomes.m_changed||th.MailCommon.m_changed||th.Mails.m_changed||th.BattleSaves.m_changed||th.Talents.m_changed||th.TowerCommon.m_changed||th.Towers.m_changed||th.Draws.m_changed||th.GoldHand.m_changed||th.Shops.m_changed||th.ShopItems.m_changed||th.Arena.m_changed||th.Equip.m_changed||th.ActiveStageCommon.m_changed||th.ActiveStages.m_changed||th.FriendCommon.m_changed||th.Friends.m_changed||th.FriendRecommends.m_changed||th.FriendAsks.m_changed||th.FriendBosss.m_changed||th.TaskCommon.m_changed||th.Tasks.m_changed||th.FinishedTasks.m_changed||th.DailyTaskAllDailys.m_changed||th.ExploreCommon.m_changed||th.Explores.m_changed||th.ExploreStorys.m_changed||th.FriendChatUnreadIds.m_changed||th.FriendChatUnreadMessages.m_changed||th.HeadItems.m_changed||th.SuitAwards.m_changed||th.Chats.m_changed||th.Anouncement.m_changed||th.FirstDrawCards.m_changed||th.Guild.m_changed||th.GuildStage.m_changed||th.Sign.m_changed||th.SevenDays.m_changed||th.PayCommon.m_changed||th.Pays.m_changed||th.GuideData.m_changed||th.ActivityDatas.m_changed||th.ExpeditionData.m_changed||th.ExpeditionRoles.m_changed||th.ExpeditionLevels.m_changed||th.ExpeditionLevelRole0s.m_changed||th.ExpeditionLevelRole1s.m_changed||th.ExpeditionLevelRole2s.m_changed||th.ExpeditionLevelRole3s.m_changed||th.ExpeditionLevelRole4s.m_changed||th.ExpeditionLevelRole5s.m_changed||th.ExpeditionLevelRole6s.m_changed||th.ExpeditionLevelRole7s.m_changed||th.ExpeditionLevelRole8s.m_changed||th.ExpeditionLevelRole9s.m_changed||th.SysMail.m_changed||th.Artifacts.m_changed||th.CarnivalCommon.m_changed||th.Carnivals.m_changed||th.InviteCodess.m_changed{
			update_string = "UPDATE Players SET "
			db_args:=new_db_args(77)
			if th.m_UniqueId_changed{
				update_string+="UniqueId=?,"
				db_args.Push(th.m_UniqueId)
			}
			if th.m_Account_changed{
				update_string+="Account=?,"
				db_args.Push(th.m_Account)
			}
			if th.m_Name_changed{
				update_string+="Name=?,"
				db_args.Push(th.m_Name)
			}
			if th.m_Token_changed{
				update_string+="Token=?,"
				db_args.Push(th.m_Token)
			}
			if th.m_CurrReplyMsgNum_changed{
				update_string+="CurrReplyMsgNum=?,"
				db_args.Push(th.m_CurrReplyMsgNum)
			}
			if th.Info.m_changed{
				update_string+="Info=?,"
				dInfo,err:=th.Info.save()
				if err!=nil{
					log.Error("update save Info failed")
					return err,false,0,"",nil
				}
				db_args.Push(dInfo)
			}
			if th.Global.m_changed{
				update_string+="Global=?,"
				dGlobal,err:=th.Global.save()
				if err!=nil{
					log.Error("update save Global failed")
					return err,false,0,"",nil
				}
				db_args.Push(dGlobal)
			}
			if th.m_Level_changed{
				update_string+="Level=?,"
				db_args.Push(th.m_Level)
			}
			if th.Items.m_changed{
				update_string+="Items=?,"
				dItems,err:=th.Items.save()
				if err!=nil{
					log.Error("insert save Item failed")
					return err,false,0,"",nil
				}
				db_args.Push(dItems)
			}
			if th.RoleCommon.m_changed{
				update_string+="RoleCommon=?,"
				dRoleCommon,err:=th.RoleCommon.save()
				if err!=nil{
					log.Error("update save RoleCommon failed")
					return err,false,0,"",nil
				}
				db_args.Push(dRoleCommon)
			}
			if th.Roles.m_changed{
				update_string+="Roles=?,"
				dRoles,err:=th.Roles.save()
				if err!=nil{
					log.Error("insert save Role failed")
					return err,false,0,"",nil
				}
				db_args.Push(dRoles)
			}
			if th.RoleHandbook.m_changed{
				update_string+="RoleHandbook=?,"
				dRoleHandbook,err:=th.RoleHandbook.save()
				if err!=nil{
					log.Error("update save RoleHandbook failed")
					return err,false,0,"",nil
				}
				db_args.Push(dRoleHandbook)
			}
			if th.BattleTeam.m_changed{
				update_string+="BattleTeam=?,"
				dBattleTeam,err:=th.BattleTeam.save()
				if err!=nil{
					log.Error("update save BattleTeam failed")
					return err,false,0,"",nil
				}
				db_args.Push(dBattleTeam)
			}
			if th.CampaignCommon.m_changed{
				update_string+="CampaignCommon=?,"
				dCampaignCommon,err:=th.CampaignCommon.save()
				if err!=nil{
					log.Error("update save CampaignCommon failed")
					return err,false,0,"",nil
				}
				db_args.Push(dCampaignCommon)
			}
			if th.Campaigns.m_changed{
				update_string+="Campaigns=?,"
				dCampaigns,err:=th.Campaigns.save()
				if err!=nil{
					log.Error("insert save Campaign failed")
					return err,false,0,"",nil
				}
				db_args.Push(dCampaigns)
			}
			if th.CampaignStaticIncomes.m_changed{
				update_string+="CampaignStaticIncomes=?,"
				dCampaignStaticIncomes,err:=th.CampaignStaticIncomes.save()
				if err!=nil{
					log.Error("insert save CampaignStaticIncome failed")
					return err,false,0,"",nil
				}
				db_args.Push(dCampaignStaticIncomes)
			}
			if th.CampaignRandomIncomes.m_changed{
				update_string+="CampaignRandomIncomes=?,"
				dCampaignRandomIncomes,err:=th.CampaignRandomIncomes.save()
				if err!=nil{
					log.Error("insert save CampaignRandomIncome failed")
					return err,false,0,"",nil
				}
				db_args.Push(dCampaignRandomIncomes)
			}
			if th.MailCommon.m_changed{
				update_string+="MailCommon=?,"
				dMailCommon,err:=th.MailCommon.save()
				if err!=nil{
					log.Error("update save MailCommon failed")
					return err,false,0,"",nil
				}
				db_args.Push(dMailCommon)
			}
			if th.Mails.m_changed{
				update_string+="Mails=?,"
				dMails,err:=th.Mails.save()
				if err!=nil{
					log.Error("insert save Mail failed")
					return err,false,0,"",nil
				}
				db_args.Push(dMails)
			}
			if th.BattleSaves.m_changed{
				update_string+="BattleSaves=?,"
				dBattleSaves,err:=th.BattleSaves.save()
				if err!=nil{
					log.Error("insert save BattleSave failed")
					return err,false,0,"",nil
				}
				db_args.Push(dBattleSaves)
			}
			if th.Talents.m_changed{
				update_string+="Talents=?,"
				dTalents,err:=th.Talents.save()
				if err!=nil{
					log.Error("insert save Talent failed")
					return err,false,0,"",nil
				}
				db_args.Push(dTalents)
			}
			if th.TowerCommon.m_changed{
				update_string+="TowerCommon=?,"
				dTowerCommon,err:=th.TowerCommon.save()
				if err!=nil{
					log.Error("update save TowerCommon failed")
					return err,false,0,"",nil
				}
				db_args.Push(dTowerCommon)
			}
			if th.Towers.m_changed{
				update_string+="Towers=?,"
				dTowers,err:=th.Towers.save()
				if err!=nil{
					log.Error("insert save Tower failed")
					return err,false,0,"",nil
				}
				db_args.Push(dTowers)
			}
			if th.Draws.m_changed{
				update_string+="Draws=?,"
				dDraws,err:=th.Draws.save()
				if err!=nil{
					log.Error("insert save Draw failed")
					return err,false,0,"",nil
				}
				db_args.Push(dDraws)
			}
			if th.GoldHand.m_changed{
				update_string+="GoldHand=?,"
				dGoldHand,err:=th.GoldHand.save()
				if err!=nil{
					log.Error("update save GoldHand failed")
					return err,false,0,"",nil
				}
				db_args.Push(dGoldHand)
			}
			if th.Shops.m_changed{
				update_string+="Shops=?,"
				dShops,err:=th.Shops.save()
				if err!=nil{
					log.Error("insert save Shop failed")
					return err,false,0,"",nil
				}
				db_args.Push(dShops)
			}
			if th.ShopItems.m_changed{
				update_string+="ShopItems=?,"
				dShopItems,err:=th.ShopItems.save()
				if err!=nil{
					log.Error("insert save ShopItem failed")
					return err,false,0,"",nil
				}
				db_args.Push(dShopItems)
			}
			if th.Arena.m_changed{
				update_string+="Arena=?,"
				dArena,err:=th.Arena.save()
				if err!=nil{
					log.Error("update save Arena failed")
					return err,false,0,"",nil
				}
				db_args.Push(dArena)
			}
			if th.Equip.m_changed{
				update_string+="Equip=?,"
				dEquip,err:=th.Equip.save()
				if err!=nil{
					log.Error("update save Equip failed")
					return err,false,0,"",nil
				}
				db_args.Push(dEquip)
			}
			if th.ActiveStageCommon.m_changed{
				update_string+="ActiveStageCommon=?,"
				dActiveStageCommon,err:=th.ActiveStageCommon.save()
				if err!=nil{
					log.Error("update save ActiveStageCommon failed")
					return err,false,0,"",nil
				}
				db_args.Push(dActiveStageCommon)
			}
			if th.ActiveStages.m_changed{
				update_string+="ActiveStages=?,"
				dActiveStages,err:=th.ActiveStages.save()
				if err!=nil{
					log.Error("insert save ActiveStage failed")
					return err,false,0,"",nil
				}
				db_args.Push(dActiveStages)
			}
			if th.FriendCommon.m_changed{
				update_string+="FriendCommon=?,"
				dFriendCommon,err:=th.FriendCommon.save()
				if err!=nil{
					log.Error("update save FriendCommon failed")
					return err,false,0,"",nil
				}
				db_args.Push(dFriendCommon)
			}
			if th.Friends.m_changed{
				update_string+="Friends=?,"
				dFriends,err:=th.Friends.save()
				if err!=nil{
					log.Error("insert save Friend failed")
					return err,false,0,"",nil
				}
				db_args.Push(dFriends)
			}
			if th.FriendRecommends.m_changed{
				update_string+="FriendRecommends=?,"
				dFriendRecommends,err:=th.FriendRecommends.save()
				if err!=nil{
					log.Error("insert save FriendRecommend failed")
					return err,false,0,"",nil
				}
				db_args.Push(dFriendRecommends)
			}
			if th.FriendAsks.m_changed{
				update_string+="FriendAsks=?,"
				dFriendAsks,err:=th.FriendAsks.save()
				if err!=nil{
					log.Error("insert save FriendAsk failed")
					return err,false,0,"",nil
				}
				db_args.Push(dFriendAsks)
			}
			if th.FriendBosss.m_changed{
				update_string+="FriendBosss=?,"
				dFriendBosss,err:=th.FriendBosss.save()
				if err!=nil{
					log.Error("insert save FriendBoss failed")
					return err,false,0,"",nil
				}
				db_args.Push(dFriendBosss)
			}
			if th.TaskCommon.m_changed{
				update_string+="TaskCommon=?,"
				dTaskCommon,err:=th.TaskCommon.save()
				if err!=nil{
					log.Error("update save TaskCommon failed")
					return err,false,0,"",nil
				}
				db_args.Push(dTaskCommon)
			}
			if th.Tasks.m_changed{
				update_string+="Tasks=?,"
				dTasks,err:=th.Tasks.save()
				if err!=nil{
					log.Error("insert save Task failed")
					return err,false,0,"",nil
				}
				db_args.Push(dTasks)
			}
			if th.FinishedTasks.m_changed{
				update_string+="FinishedTasks=?,"
				dFinishedTasks,err:=th.FinishedTasks.save()
				if err!=nil{
					log.Error("insert save FinishedTask failed")
					return err,false,0,"",nil
				}
				db_args.Push(dFinishedTasks)
			}
			if th.DailyTaskAllDailys.m_changed{
				update_string+="DailyTaskAllDailys=?,"
				dDailyTaskAllDailys,err:=th.DailyTaskAllDailys.save()
				if err!=nil{
					log.Error("insert save DailyTaskAllDaily failed")
					return err,false,0,"",nil
				}
				db_args.Push(dDailyTaskAllDailys)
			}
			if th.ExploreCommon.m_changed{
				update_string+="ExploreCommon=?,"
				dExploreCommon,err:=th.ExploreCommon.save()
				if err!=nil{
					log.Error("update save ExploreCommon failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExploreCommon)
			}
			if th.Explores.m_changed{
				update_string+="Explores=?,"
				dExplores,err:=th.Explores.save()
				if err!=nil{
					log.Error("insert save Explore failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExplores)
			}
			if th.ExploreStorys.m_changed{
				update_string+="ExploreStorys=?,"
				dExploreStorys,err:=th.ExploreStorys.save()
				if err!=nil{
					log.Error("insert save ExploreStory failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExploreStorys)
			}
			if th.FriendChatUnreadIds.m_changed{
				update_string+="FriendChatUnreadIds=?,"
				dFriendChatUnreadIds,err:=th.FriendChatUnreadIds.save()
				if err!=nil{
					log.Error("insert save FriendChatUnreadId failed")
					return err,false,0,"",nil
				}
				db_args.Push(dFriendChatUnreadIds)
			}
			if th.FriendChatUnreadMessages.m_changed{
				update_string+="FriendChatUnreadMessages=?,"
				dFriendChatUnreadMessages,err:=th.FriendChatUnreadMessages.save()
				if err!=nil{
					log.Error("insert save FriendChatUnreadMessage failed")
					return err,false,0,"",nil
				}
				db_args.Push(dFriendChatUnreadMessages)
			}
			if th.HeadItems.m_changed{
				update_string+="HeadItems=?,"
				dHeadItems,err:=th.HeadItems.save()
				if err!=nil{
					log.Error("insert save HeadItem failed")
					return err,false,0,"",nil
				}
				db_args.Push(dHeadItems)
			}
			if th.SuitAwards.m_changed{
				update_string+="SuitAwards=?,"
				dSuitAwards,err:=th.SuitAwards.save()
				if err!=nil{
					log.Error("insert save SuitAward failed")
					return err,false,0,"",nil
				}
				db_args.Push(dSuitAwards)
			}
			if th.Chats.m_changed{
				update_string+="Chats=?,"
				dChats,err:=th.Chats.save()
				if err!=nil{
					log.Error("insert save Chat failed")
					return err,false,0,"",nil
				}
				db_args.Push(dChats)
			}
			if th.Anouncement.m_changed{
				update_string+="Anouncement=?,"
				dAnouncement,err:=th.Anouncement.save()
				if err!=nil{
					log.Error("update save Anouncement failed")
					return err,false,0,"",nil
				}
				db_args.Push(dAnouncement)
			}
			if th.FirstDrawCards.m_changed{
				update_string+="FirstDrawCards=?,"
				dFirstDrawCards,err:=th.FirstDrawCards.save()
				if err!=nil{
					log.Error("insert save FirstDrawCard failed")
					return err,false,0,"",nil
				}
				db_args.Push(dFirstDrawCards)
			}
			if th.Guild.m_changed{
				update_string+="Guild=?,"
				dGuild,err:=th.Guild.save()
				if err!=nil{
					log.Error("update save Guild failed")
					return err,false,0,"",nil
				}
				db_args.Push(dGuild)
			}
			if th.GuildStage.m_changed{
				update_string+="GuildStage=?,"
				dGuildStage,err:=th.GuildStage.save()
				if err!=nil{
					log.Error("update save GuildStage failed")
					return err,false,0,"",nil
				}
				db_args.Push(dGuildStage)
			}
			if th.Sign.m_changed{
				update_string+="Sign=?,"
				dSign,err:=th.Sign.save()
				if err!=nil{
					log.Error("update save Sign failed")
					return err,false,0,"",nil
				}
				db_args.Push(dSign)
			}
			if th.SevenDays.m_changed{
				update_string+="SevenDays=?,"
				dSevenDays,err:=th.SevenDays.save()
				if err!=nil{
					log.Error("update save SevenDays failed")
					return err,false,0,"",nil
				}
				db_args.Push(dSevenDays)
			}
			if th.PayCommon.m_changed{
				update_string+="PayCommon=?,"
				dPayCommon,err:=th.PayCommon.save()
				if err!=nil{
					log.Error("update save PayCommon failed")
					return err,false,0,"",nil
				}
				db_args.Push(dPayCommon)
			}
			if th.Pays.m_changed{
				update_string+="Pays=?,"
				dPays,err:=th.Pays.save()
				if err!=nil{
					log.Error("insert save Pay failed")
					return err,false,0,"",nil
				}
				db_args.Push(dPays)
			}
			if th.GuideData.m_changed{
				update_string+="GuideData=?,"
				dGuideData,err:=th.GuideData.save()
				if err!=nil{
					log.Error("update save GuideData failed")
					return err,false,0,"",nil
				}
				db_args.Push(dGuideData)
			}
			if th.ActivityDatas.m_changed{
				update_string+="ActivityDatas=?,"
				dActivityDatas,err:=th.ActivityDatas.save()
				if err!=nil{
					log.Error("insert save ActivityData failed")
					return err,false,0,"",nil
				}
				db_args.Push(dActivityDatas)
			}
			if th.ExpeditionData.m_changed{
				update_string+="ExpeditionData=?,"
				dExpeditionData,err:=th.ExpeditionData.save()
				if err!=nil{
					log.Error("update save ExpeditionData failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionData)
			}
			if th.ExpeditionRoles.m_changed{
				update_string+="ExpeditionRoles=?,"
				dExpeditionRoles,err:=th.ExpeditionRoles.save()
				if err!=nil{
					log.Error("insert save ExpeditionRole failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionRoles)
			}
			if th.ExpeditionLevels.m_changed{
				update_string+="ExpeditionLevels=?,"
				dExpeditionLevels,err:=th.ExpeditionLevels.save()
				if err!=nil{
					log.Error("insert save ExpeditionLevel failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionLevels)
			}
			if th.ExpeditionLevelRole0s.m_changed{
				update_string+="ExpeditionLevelRole0s=?,"
				dExpeditionLevelRole0s,err:=th.ExpeditionLevelRole0s.save()
				if err!=nil{
					log.Error("insert save ExpeditionLevelRole0 failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionLevelRole0s)
			}
			if th.ExpeditionLevelRole1s.m_changed{
				update_string+="ExpeditionLevelRole1s=?,"
				dExpeditionLevelRole1s,err:=th.ExpeditionLevelRole1s.save()
				if err!=nil{
					log.Error("insert save ExpeditionLevelRole1 failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionLevelRole1s)
			}
			if th.ExpeditionLevelRole2s.m_changed{
				update_string+="ExpeditionLevelRole2s=?,"
				dExpeditionLevelRole2s,err:=th.ExpeditionLevelRole2s.save()
				if err!=nil{
					log.Error("insert save ExpeditionLevelRole2 failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionLevelRole2s)
			}
			if th.ExpeditionLevelRole3s.m_changed{
				update_string+="ExpeditionLevelRole3s=?,"
				dExpeditionLevelRole3s,err:=th.ExpeditionLevelRole3s.save()
				if err!=nil{
					log.Error("insert save ExpeditionLevelRole3 failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionLevelRole3s)
			}
			if th.ExpeditionLevelRole4s.m_changed{
				update_string+="ExpeditionLevelRole4s=?,"
				dExpeditionLevelRole4s,err:=th.ExpeditionLevelRole4s.save()
				if err!=nil{
					log.Error("insert save ExpeditionLevelRole4 failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionLevelRole4s)
			}
			if th.ExpeditionLevelRole5s.m_changed{
				update_string+="ExpeditionLevelRole5s=?,"
				dExpeditionLevelRole5s,err:=th.ExpeditionLevelRole5s.save()
				if err!=nil{
					log.Error("insert save ExpeditionLevelRole5 failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionLevelRole5s)
			}
			if th.ExpeditionLevelRole6s.m_changed{
				update_string+="ExpeditionLevelRole6s=?,"
				dExpeditionLevelRole6s,err:=th.ExpeditionLevelRole6s.save()
				if err!=nil{
					log.Error("insert save ExpeditionLevelRole6 failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionLevelRole6s)
			}
			if th.ExpeditionLevelRole7s.m_changed{
				update_string+="ExpeditionLevelRole7s=?,"
				dExpeditionLevelRole7s,err:=th.ExpeditionLevelRole7s.save()
				if err!=nil{
					log.Error("insert save ExpeditionLevelRole7 failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionLevelRole7s)
			}
			if th.ExpeditionLevelRole8s.m_changed{
				update_string+="ExpeditionLevelRole8s=?,"
				dExpeditionLevelRole8s,err:=th.ExpeditionLevelRole8s.save()
				if err!=nil{
					log.Error("insert save ExpeditionLevelRole8 failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionLevelRole8s)
			}
			if th.ExpeditionLevelRole9s.m_changed{
				update_string+="ExpeditionLevelRole9s=?,"
				dExpeditionLevelRole9s,err:=th.ExpeditionLevelRole9s.save()
				if err!=nil{
					log.Error("insert save ExpeditionLevelRole9 failed")
					return err,false,0,"",nil
				}
				db_args.Push(dExpeditionLevelRole9s)
			}
			if th.SysMail.m_changed{
				update_string+="SysMail=?,"
				dSysMail,err:=th.SysMail.save()
				if err!=nil{
					log.Error("update save SysMail failed")
					return err,false,0,"",nil
				}
				db_args.Push(dSysMail)
			}
			if th.Artifacts.m_changed{
				update_string+="Artifacts=?,"
				dArtifacts,err:=th.Artifacts.save()
				if err!=nil{
					log.Error("insert save Artifact failed")
					return err,false,0,"",nil
				}
				db_args.Push(dArtifacts)
			}
			if th.CarnivalCommon.m_changed{
				update_string+="CarnivalCommon=?,"
				dCarnivalCommon,err:=th.CarnivalCommon.save()
				if err!=nil{
					log.Error("update save CarnivalCommon failed")
					return err,false,0,"",nil
				}
				db_args.Push(dCarnivalCommon)
			}
			if th.Carnivals.m_changed{
				update_string+="Carnivals=?,"
				dCarnivals,err:=th.Carnivals.save()
				if err!=nil{
					log.Error("insert save Carnival failed")
					return err,false,0,"",nil
				}
				db_args.Push(dCarnivals)
			}
			if th.InviteCodess.m_changed{
				update_string+="InviteCodess=?,"
				dInviteCodess,err:=th.InviteCodess.save()
				if err!=nil{
					log.Error("insert save InviteCodes failed")
					return err,false,0,"",nil
				}
				db_args.Push(dInviteCodess)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE PlayerId=?"
			db_args.Push(th.m_PlayerId)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.m_UniqueId_changed = false
	th.m_Account_changed = false
	th.m_Name_changed = false
	th.m_Token_changed = false
	th.m_CurrReplyMsgNum_changed = false
	th.Info.m_changed = false
	th.Global.m_changed = false
	th.m_Level_changed = false
	th.Items.m_changed = false
	th.RoleCommon.m_changed = false
	th.Roles.m_changed = false
	th.RoleHandbook.m_changed = false
	th.BattleTeam.m_changed = false
	th.CampaignCommon.m_changed = false
	th.Campaigns.m_changed = false
	th.CampaignStaticIncomes.m_changed = false
	th.CampaignRandomIncomes.m_changed = false
	th.MailCommon.m_changed = false
	th.Mails.m_changed = false
	th.BattleSaves.m_changed = false
	th.Talents.m_changed = false
	th.TowerCommon.m_changed = false
	th.Towers.m_changed = false
	th.Draws.m_changed = false
	th.GoldHand.m_changed = false
	th.Shops.m_changed = false
	th.ShopItems.m_changed = false
	th.Arena.m_changed = false
	th.Equip.m_changed = false
	th.ActiveStageCommon.m_changed = false
	th.ActiveStages.m_changed = false
	th.FriendCommon.m_changed = false
	th.Friends.m_changed = false
	th.FriendRecommends.m_changed = false
	th.FriendAsks.m_changed = false
	th.FriendBosss.m_changed = false
	th.TaskCommon.m_changed = false
	th.Tasks.m_changed = false
	th.FinishedTasks.m_changed = false
	th.DailyTaskAllDailys.m_changed = false
	th.ExploreCommon.m_changed = false
	th.Explores.m_changed = false
	th.ExploreStorys.m_changed = false
	th.FriendChatUnreadIds.m_changed = false
	th.FriendChatUnreadMessages.m_changed = false
	th.HeadItems.m_changed = false
	th.SuitAwards.m_changed = false
	th.Chats.m_changed = false
	th.Anouncement.m_changed = false
	th.FirstDrawCards.m_changed = false
	th.Guild.m_changed = false
	th.GuildStage.m_changed = false
	th.Sign.m_changed = false
	th.SevenDays.m_changed = false
	th.PayCommon.m_changed = false
	th.Pays.m_changed = false
	th.GuideData.m_changed = false
	th.ActivityDatas.m_changed = false
	th.ExpeditionData.m_changed = false
	th.ExpeditionRoles.m_changed = false
	th.ExpeditionLevels.m_changed = false
	th.ExpeditionLevelRole0s.m_changed = false
	th.ExpeditionLevelRole1s.m_changed = false
	th.ExpeditionLevelRole2s.m_changed = false
	th.ExpeditionLevelRole3s.m_changed = false
	th.ExpeditionLevelRole4s.m_changed = false
	th.ExpeditionLevelRole5s.m_changed = false
	th.ExpeditionLevelRole6s.m_changed = false
	th.ExpeditionLevelRole7s.m_changed = false
	th.ExpeditionLevelRole8s.m_changed = false
	th.ExpeditionLevelRole9s.m_changed = false
	th.SysMail.m_changed = false
	th.Artifacts.m_changed = false
	th.CarnivalCommon.m_changed = false
	th.Carnivals.m_changed = false
	th.InviteCodess.m_changed = false
	if release && th.m_loaded {
		atomic.AddInt32(&th.m_table.m_gc_n, -1)
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbPlayerRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT Players exec failed %v ", th.m_PlayerId)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE Players exec failed %v", th.m_PlayerId)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
func (th *dbPlayerRow) Touch(releasable bool) {
	th.m_touch = int32(time.Now().Unix())
	th.m_releasable = releasable
}
type dbPlayerRowSort struct {
	rows []*dbPlayerRow
}
func (th *dbPlayerRowSort) Len() (length int) {
	return len(th.rows)
}
func (th *dbPlayerRowSort) Less(i int, j int) (less bool) {
	return th.rows[i].m_touch < th.rows[j].m_touch
}
func (th *dbPlayerRowSort) Swap(i int, j int) {
	temp := th.rows[i]
	th.rows[i] = th.rows[j]
	th.rows[j] = temp
}
type dbPlayerTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_rows map[int32]*dbPlayerRow
	m_new_rows map[int32]*dbPlayerRow
	m_removed_rows map[int32]*dbPlayerRow
	m_gc_n int32
	m_gcing int32
	m_pool_size int32
	m_preload_select_stmt *sql.Stmt
	m_preload_max_id int32
	m_save_insert_stmt *sql.Stmt
	m_delete_stmt *sql.Stmt
}
func new_dbPlayerTable(dbc *DBC) (th *dbPlayerTable) {
	th = &dbPlayerTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	th.m_rows = make(map[int32]*dbPlayerRow)
	th.m_new_rows = make(map[int32]*dbPlayerRow)
	th.m_removed_rows = make(map[int32]*dbPlayerRow)
	return th
}
func (th *dbPlayerTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS Players(PlayerId int(11),PRIMARY KEY (PlayerId))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS Players failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='Players'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasUniqueId := columns["UniqueId"]
	if !hasUniqueId {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN UniqueId varchar(256) DEFAULT ''")
		if err != nil {
			log.Error("ADD COLUMN UniqueId failed")
			return
		}
	}
	_, hasAccount := columns["Account"]
	if !hasAccount {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Account varchar(256)")
		if err != nil {
			log.Error("ADD COLUMN Account failed")
			return
		}
	}
	_, hasName := columns["Name"]
	if !hasName {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Name varchar(256)")
		if err != nil {
			log.Error("ADD COLUMN Name failed")
			return
		}
	}
	_, hasToken := columns["Token"]
	if !hasToken {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Token varchar(256) DEFAULT ''")
		if err != nil {
			log.Error("ADD COLUMN Token failed")
			return
		}
	}
	_, hasCurrReplyMsgNum := columns["CurrReplyMsgNum"]
	if !hasCurrReplyMsgNum {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN CurrReplyMsgNum int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN CurrReplyMsgNum failed")
			return
		}
	}
	_, hasInfo := columns["Info"]
	if !hasInfo {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Info LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Info failed")
			return
		}
	}
	_, hasGlobal := columns["Global"]
	if !hasGlobal {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Global LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Global failed")
			return
		}
	}
	_, hasLevel := columns["Level"]
	if !hasLevel {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Level int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN Level failed")
			return
		}
	}
	_, hasItem := columns["Items"]
	if !hasItem {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Items LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Items failed")
			return
		}
	}
	_, hasRoleCommon := columns["RoleCommon"]
	if !hasRoleCommon {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN RoleCommon LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN RoleCommon failed")
			return
		}
	}
	_, hasRole := columns["Roles"]
	if !hasRole {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Roles LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Roles failed")
			return
		}
	}
	_, hasRoleHandbook := columns["RoleHandbook"]
	if !hasRoleHandbook {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN RoleHandbook LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN RoleHandbook failed")
			return
		}
	}
	_, hasBattleTeam := columns["BattleTeam"]
	if !hasBattleTeam {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN BattleTeam LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN BattleTeam failed")
			return
		}
	}
	_, hasCampaignCommon := columns["CampaignCommon"]
	if !hasCampaignCommon {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN CampaignCommon LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN CampaignCommon failed")
			return
		}
	}
	_, hasCampaign := columns["Campaigns"]
	if !hasCampaign {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Campaigns LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Campaigns failed")
			return
		}
	}
	_, hasCampaignStaticIncome := columns["CampaignStaticIncomes"]
	if !hasCampaignStaticIncome {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN CampaignStaticIncomes LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN CampaignStaticIncomes failed")
			return
		}
	}
	_, hasCampaignRandomIncome := columns["CampaignRandomIncomes"]
	if !hasCampaignRandomIncome {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN CampaignRandomIncomes LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN CampaignRandomIncomes failed")
			return
		}
	}
	_, hasMailCommon := columns["MailCommon"]
	if !hasMailCommon {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN MailCommon LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN MailCommon failed")
			return
		}
	}
	_, hasMail := columns["Mails"]
	if !hasMail {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Mails LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Mails failed")
			return
		}
	}
	_, hasBattleSave := columns["BattleSaves"]
	if !hasBattleSave {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN BattleSaves LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN BattleSaves failed")
			return
		}
	}
	_, hasTalent := columns["Talents"]
	if !hasTalent {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Talents LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Talents failed")
			return
		}
	}
	_, hasTowerCommon := columns["TowerCommon"]
	if !hasTowerCommon {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN TowerCommon LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN TowerCommon failed")
			return
		}
	}
	_, hasTower := columns["Towers"]
	if !hasTower {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Towers LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Towers failed")
			return
		}
	}
	_, hasDraw := columns["Draws"]
	if !hasDraw {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Draws LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Draws failed")
			return
		}
	}
	_, hasGoldHand := columns["GoldHand"]
	if !hasGoldHand {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN GoldHand LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN GoldHand failed")
			return
		}
	}
	_, hasShop := columns["Shops"]
	if !hasShop {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Shops LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Shops failed")
			return
		}
	}
	_, hasShopItem := columns["ShopItems"]
	if !hasShopItem {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ShopItems LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ShopItems failed")
			return
		}
	}
	_, hasArena := columns["Arena"]
	if !hasArena {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Arena LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Arena failed")
			return
		}
	}
	_, hasEquip := columns["Equip"]
	if !hasEquip {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Equip LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Equip failed")
			return
		}
	}
	_, hasActiveStageCommon := columns["ActiveStageCommon"]
	if !hasActiveStageCommon {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ActiveStageCommon LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ActiveStageCommon failed")
			return
		}
	}
	_, hasActiveStage := columns["ActiveStages"]
	if !hasActiveStage {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ActiveStages LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ActiveStages failed")
			return
		}
	}
	_, hasFriendCommon := columns["FriendCommon"]
	if !hasFriendCommon {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN FriendCommon LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN FriendCommon failed")
			return
		}
	}
	_, hasFriend := columns["Friends"]
	if !hasFriend {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Friends LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Friends failed")
			return
		}
	}
	_, hasFriendRecommend := columns["FriendRecommends"]
	if !hasFriendRecommend {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN FriendRecommends LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN FriendRecommends failed")
			return
		}
	}
	_, hasFriendAsk := columns["FriendAsks"]
	if !hasFriendAsk {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN FriendAsks LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN FriendAsks failed")
			return
		}
	}
	_, hasFriendBoss := columns["FriendBosss"]
	if !hasFriendBoss {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN FriendBosss LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN FriendBosss failed")
			return
		}
	}
	_, hasTaskCommon := columns["TaskCommon"]
	if !hasTaskCommon {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN TaskCommon LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN TaskCommon failed")
			return
		}
	}
	_, hasTask := columns["Tasks"]
	if !hasTask {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Tasks LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Tasks failed")
			return
		}
	}
	_, hasFinishedTask := columns["FinishedTasks"]
	if !hasFinishedTask {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN FinishedTasks LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN FinishedTasks failed")
			return
		}
	}
	_, hasDailyTaskAllDaily := columns["DailyTaskAllDailys"]
	if !hasDailyTaskAllDaily {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN DailyTaskAllDailys LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN DailyTaskAllDailys failed")
			return
		}
	}
	_, hasExploreCommon := columns["ExploreCommon"]
	if !hasExploreCommon {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExploreCommon LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExploreCommon failed")
			return
		}
	}
	_, hasExplore := columns["Explores"]
	if !hasExplore {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Explores LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Explores failed")
			return
		}
	}
	_, hasExploreStory := columns["ExploreStorys"]
	if !hasExploreStory {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExploreStorys LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExploreStorys failed")
			return
		}
	}
	_, hasFriendChatUnreadId := columns["FriendChatUnreadIds"]
	if !hasFriendChatUnreadId {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN FriendChatUnreadIds LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN FriendChatUnreadIds failed")
			return
		}
	}
	_, hasFriendChatUnreadMessage := columns["FriendChatUnreadMessages"]
	if !hasFriendChatUnreadMessage {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN FriendChatUnreadMessages LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN FriendChatUnreadMessages failed")
			return
		}
	}
	_, hasHeadItem := columns["HeadItems"]
	if !hasHeadItem {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN HeadItems LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN HeadItems failed")
			return
		}
	}
	_, hasSuitAward := columns["SuitAwards"]
	if !hasSuitAward {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN SuitAwards LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN SuitAwards failed")
			return
		}
	}
	_, hasChat := columns["Chats"]
	if !hasChat {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Chats LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Chats failed")
			return
		}
	}
	_, hasAnouncement := columns["Anouncement"]
	if !hasAnouncement {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Anouncement LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Anouncement failed")
			return
		}
	}
	_, hasFirstDrawCard := columns["FirstDrawCards"]
	if !hasFirstDrawCard {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN FirstDrawCards LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN FirstDrawCards failed")
			return
		}
	}
	_, hasGuild := columns["Guild"]
	if !hasGuild {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Guild LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Guild failed")
			return
		}
	}
	_, hasGuildStage := columns["GuildStage"]
	if !hasGuildStage {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN GuildStage LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN GuildStage failed")
			return
		}
	}
	_, hasSign := columns["Sign"]
	if !hasSign {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Sign LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Sign failed")
			return
		}
	}
	_, hasSevenDays := columns["SevenDays"]
	if !hasSevenDays {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN SevenDays LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN SevenDays failed")
			return
		}
	}
	_, hasPayCommon := columns["PayCommon"]
	if !hasPayCommon {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN PayCommon LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN PayCommon failed")
			return
		}
	}
	_, hasPay := columns["Pays"]
	if !hasPay {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Pays LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Pays failed")
			return
		}
	}
	_, hasGuideData := columns["GuideData"]
	if !hasGuideData {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN GuideData LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN GuideData failed")
			return
		}
	}
	_, hasActivityData := columns["ActivityDatas"]
	if !hasActivityData {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ActivityDatas LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ActivityDatas failed")
			return
		}
	}
	_, hasExpeditionData := columns["ExpeditionData"]
	if !hasExpeditionData {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionData LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionData failed")
			return
		}
	}
	_, hasExpeditionRole := columns["ExpeditionRoles"]
	if !hasExpeditionRole {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionRoles LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionRoles failed")
			return
		}
	}
	_, hasExpeditionLevel := columns["ExpeditionLevels"]
	if !hasExpeditionLevel {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionLevels LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionLevels failed")
			return
		}
	}
	_, hasExpeditionLevelRole0 := columns["ExpeditionLevelRole0s"]
	if !hasExpeditionLevelRole0 {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionLevelRole0s LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionLevelRole0s failed")
			return
		}
	}
	_, hasExpeditionLevelRole1 := columns["ExpeditionLevelRole1s"]
	if !hasExpeditionLevelRole1 {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionLevelRole1s LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionLevelRole1s failed")
			return
		}
	}
	_, hasExpeditionLevelRole2 := columns["ExpeditionLevelRole2s"]
	if !hasExpeditionLevelRole2 {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionLevelRole2s LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionLevelRole2s failed")
			return
		}
	}
	_, hasExpeditionLevelRole3 := columns["ExpeditionLevelRole3s"]
	if !hasExpeditionLevelRole3 {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionLevelRole3s LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionLevelRole3s failed")
			return
		}
	}
	_, hasExpeditionLevelRole4 := columns["ExpeditionLevelRole4s"]
	if !hasExpeditionLevelRole4 {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionLevelRole4s LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionLevelRole4s failed")
			return
		}
	}
	_, hasExpeditionLevelRole5 := columns["ExpeditionLevelRole5s"]
	if !hasExpeditionLevelRole5 {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionLevelRole5s LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionLevelRole5s failed")
			return
		}
	}
	_, hasExpeditionLevelRole6 := columns["ExpeditionLevelRole6s"]
	if !hasExpeditionLevelRole6 {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionLevelRole6s LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionLevelRole6s failed")
			return
		}
	}
	_, hasExpeditionLevelRole7 := columns["ExpeditionLevelRole7s"]
	if !hasExpeditionLevelRole7 {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionLevelRole7s LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionLevelRole7s failed")
			return
		}
	}
	_, hasExpeditionLevelRole8 := columns["ExpeditionLevelRole8s"]
	if !hasExpeditionLevelRole8 {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionLevelRole8s LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionLevelRole8s failed")
			return
		}
	}
	_, hasExpeditionLevelRole9 := columns["ExpeditionLevelRole9s"]
	if !hasExpeditionLevelRole9 {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN ExpeditionLevelRole9s LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN ExpeditionLevelRole9s failed")
			return
		}
	}
	_, hasSysMail := columns["SysMail"]
	if !hasSysMail {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN SysMail LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN SysMail failed")
			return
		}
	}
	_, hasArtifact := columns["Artifacts"]
	if !hasArtifact {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Artifacts LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Artifacts failed")
			return
		}
	}
	_, hasCarnivalCommon := columns["CarnivalCommon"]
	if !hasCarnivalCommon {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN CarnivalCommon LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN CarnivalCommon failed")
			return
		}
	}
	_, hasCarnival := columns["Carnivals"]
	if !hasCarnival {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN Carnivals LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Carnivals failed")
			return
		}
	}
	_, hasInviteCodes := columns["InviteCodess"]
	if !hasInviteCodes {
		_, err = th.m_dbc.Exec("ALTER TABLE Players ADD COLUMN InviteCodess LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN InviteCodess failed")
			return
		}
	}
	return
}
func (th *dbPlayerTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT PlayerId,UniqueId,Account,Name,Token,CurrReplyMsgNum,Info,Global,Level,Items,RoleCommon,Roles,RoleHandbook,BattleTeam,CampaignCommon,Campaigns,CampaignStaticIncomes,CampaignRandomIncomes,MailCommon,Mails,BattleSaves,Talents,TowerCommon,Towers,Draws,GoldHand,Shops,ShopItems,Arena,Equip,ActiveStageCommon,ActiveStages,FriendCommon,Friends,FriendRecommends,FriendAsks,FriendBosss,TaskCommon,Tasks,FinishedTasks,DailyTaskAllDailys,ExploreCommon,Explores,ExploreStorys,FriendChatUnreadIds,FriendChatUnreadMessages,HeadItems,SuitAwards,Chats,Anouncement,FirstDrawCards,Guild,GuildStage,Sign,SevenDays,PayCommon,Pays,GuideData,ActivityDatas,ExpeditionData,ExpeditionRoles,ExpeditionLevels,ExpeditionLevelRole0s,ExpeditionLevelRole1s,ExpeditionLevelRole2s,ExpeditionLevelRole3s,ExpeditionLevelRole4s,ExpeditionLevelRole5s,ExpeditionLevelRole6s,ExpeditionLevelRole7s,ExpeditionLevelRole8s,ExpeditionLevelRole9s,SysMail,Artifacts,CarnivalCommon,Carnivals,InviteCodess FROM Players")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbPlayerTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO Players (PlayerId,UniqueId,Account,Name,Token,CurrReplyMsgNum,Info,Global,Level,Items,RoleCommon,Roles,RoleHandbook,BattleTeam,CampaignCommon,Campaigns,CampaignStaticIncomes,CampaignRandomIncomes,MailCommon,Mails,BattleSaves,Talents,TowerCommon,Towers,Draws,GoldHand,Shops,ShopItems,Arena,Equip,ActiveStageCommon,ActiveStages,FriendCommon,Friends,FriendRecommends,FriendAsks,FriendBosss,TaskCommon,Tasks,FinishedTasks,DailyTaskAllDailys,ExploreCommon,Explores,ExploreStorys,FriendChatUnreadIds,FriendChatUnreadMessages,HeadItems,SuitAwards,Chats,Anouncement,FirstDrawCards,Guild,GuildStage,Sign,SevenDays,PayCommon,Pays,GuideData,ActivityDatas,ExpeditionData,ExpeditionRoles,ExpeditionLevels,ExpeditionLevelRole0s,ExpeditionLevelRole1s,ExpeditionLevelRole2s,ExpeditionLevelRole3s,ExpeditionLevelRole4s,ExpeditionLevelRole5s,ExpeditionLevelRole6s,ExpeditionLevelRole7s,ExpeditionLevelRole8s,ExpeditionLevelRole9s,SysMail,Artifacts,CarnivalCommon,Carnivals,InviteCodess) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbPlayerTable) prepare_delete_stmt() (err error) {
	th.m_delete_stmt,err=th.m_dbc.StmtPrepare("DELETE FROM Players WHERE PlayerId=?")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbPlayerTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	err=th.prepare_delete_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbPlayerTable) Preload() (err error) {
	r, err := th.m_dbc.StmtQuery(th.m_preload_select_stmt)
	if err != nil {
		log.Error("SELECT")
		return
	}
	var PlayerId int32
	var dUniqueId string
	var dAccount string
	var dName string
	var dToken string
	var dCurrReplyMsgNum int32
	var dInfo []byte
	var dGlobal []byte
	var dLevel int32
	var dItems []byte
	var dRoleCommon []byte
	var dRoles []byte
	var dRoleHandbook []byte
	var dBattleTeam []byte
	var dCampaignCommon []byte
	var dCampaigns []byte
	var dCampaignStaticIncomes []byte
	var dCampaignRandomIncomes []byte
	var dMailCommon []byte
	var dMails []byte
	var dBattleSaves []byte
	var dTalents []byte
	var dTowerCommon []byte
	var dTowers []byte
	var dDraws []byte
	var dGoldHand []byte
	var dShops []byte
	var dShopItems []byte
	var dArena []byte
	var dEquip []byte
	var dActiveStageCommon []byte
	var dActiveStages []byte
	var dFriendCommon []byte
	var dFriends []byte
	var dFriendRecommends []byte
	var dFriendAsks []byte
	var dFriendBosss []byte
	var dTaskCommon []byte
	var dTasks []byte
	var dFinishedTasks []byte
	var dDailyTaskAllDailys []byte
	var dExploreCommon []byte
	var dExplores []byte
	var dExploreStorys []byte
	var dFriendChatUnreadIds []byte
	var dFriendChatUnreadMessages []byte
	var dHeadItems []byte
	var dSuitAwards []byte
	var dChats []byte
	var dAnouncement []byte
	var dFirstDrawCards []byte
	var dGuild []byte
	var dGuildStage []byte
	var dSign []byte
	var dSevenDays []byte
	var dPayCommon []byte
	var dPays []byte
	var dGuideData []byte
	var dActivityDatas []byte
	var dExpeditionData []byte
	var dExpeditionRoles []byte
	var dExpeditionLevels []byte
	var dExpeditionLevelRole0s []byte
	var dExpeditionLevelRole1s []byte
	var dExpeditionLevelRole2s []byte
	var dExpeditionLevelRole3s []byte
	var dExpeditionLevelRole4s []byte
	var dExpeditionLevelRole5s []byte
	var dExpeditionLevelRole6s []byte
	var dExpeditionLevelRole7s []byte
	var dExpeditionLevelRole8s []byte
	var dExpeditionLevelRole9s []byte
	var dSysMail []byte
	var dArtifacts []byte
	var dCarnivalCommon []byte
	var dCarnivals []byte
	var dInviteCodess []byte
		th.m_preload_max_id = 0
	for r.Next() {
		err = r.Scan(&PlayerId,&dUniqueId,&dAccount,&dName,&dToken,&dCurrReplyMsgNum,&dInfo,&dGlobal,&dLevel,&dItems,&dRoleCommon,&dRoles,&dRoleHandbook,&dBattleTeam,&dCampaignCommon,&dCampaigns,&dCampaignStaticIncomes,&dCampaignRandomIncomes,&dMailCommon,&dMails,&dBattleSaves,&dTalents,&dTowerCommon,&dTowers,&dDraws,&dGoldHand,&dShops,&dShopItems,&dArena,&dEquip,&dActiveStageCommon,&dActiveStages,&dFriendCommon,&dFriends,&dFriendRecommends,&dFriendAsks,&dFriendBosss,&dTaskCommon,&dTasks,&dFinishedTasks,&dDailyTaskAllDailys,&dExploreCommon,&dExplores,&dExploreStorys,&dFriendChatUnreadIds,&dFriendChatUnreadMessages,&dHeadItems,&dSuitAwards,&dChats,&dAnouncement,&dFirstDrawCards,&dGuild,&dGuildStage,&dSign,&dSevenDays,&dPayCommon,&dPays,&dGuideData,&dActivityDatas,&dExpeditionData,&dExpeditionRoles,&dExpeditionLevels,&dExpeditionLevelRole0s,&dExpeditionLevelRole1s,&dExpeditionLevelRole2s,&dExpeditionLevelRole3s,&dExpeditionLevelRole4s,&dExpeditionLevelRole5s,&dExpeditionLevelRole6s,&dExpeditionLevelRole7s,&dExpeditionLevelRole8s,&dExpeditionLevelRole9s,&dSysMail,&dArtifacts,&dCarnivalCommon,&dCarnivals,&dInviteCodess)
		if err != nil {
			log.Error("Scan err[%v]", err.Error())
			return
		}
		if PlayerId>th.m_preload_max_id{
			th.m_preload_max_id =PlayerId
		}
		row := new_dbPlayerRow(th,PlayerId)
		row.m_UniqueId=dUniqueId
		row.m_Account=dAccount
		row.m_Name=dName
		row.m_Token=dToken
		row.m_CurrReplyMsgNum=dCurrReplyMsgNum
		err = row.Info.load(dInfo)
		if err != nil {
			log.Error("Info %v", PlayerId)
			return
		}
		err = row.Global.load(dGlobal)
		if err != nil {
			log.Error("Global %v", PlayerId)
			return
		}
		row.m_Level=dLevel
		err = row.Items.load(dItems)
		if err != nil {
			log.Error("Items %v", PlayerId)
			return
		}
		err = row.RoleCommon.load(dRoleCommon)
		if err != nil {
			log.Error("RoleCommon %v", PlayerId)
			return
		}
		err = row.Roles.load(dRoles)
		if err != nil {
			log.Error("Roles %v", PlayerId)
			return
		}
		err = row.RoleHandbook.load(dRoleHandbook)
		if err != nil {
			log.Error("RoleHandbook %v", PlayerId)
			return
		}
		err = row.BattleTeam.load(dBattleTeam)
		if err != nil {
			log.Error("BattleTeam %v", PlayerId)
			return
		}
		err = row.CampaignCommon.load(dCampaignCommon)
		if err != nil {
			log.Error("CampaignCommon %v", PlayerId)
			return
		}
		err = row.Campaigns.load(dCampaigns)
		if err != nil {
			log.Error("Campaigns %v", PlayerId)
			return
		}
		err = row.CampaignStaticIncomes.load(dCampaignStaticIncomes)
		if err != nil {
			log.Error("CampaignStaticIncomes %v", PlayerId)
			return
		}
		err = row.CampaignRandomIncomes.load(dCampaignRandomIncomes)
		if err != nil {
			log.Error("CampaignRandomIncomes %v", PlayerId)
			return
		}
		err = row.MailCommon.load(dMailCommon)
		if err != nil {
			log.Error("MailCommon %v", PlayerId)
			return
		}
		err = row.Mails.load(dMails)
		if err != nil {
			log.Error("Mails %v", PlayerId)
			return
		}
		err = row.BattleSaves.load(dBattleSaves)
		if err != nil {
			log.Error("BattleSaves %v", PlayerId)
			return
		}
		err = row.Talents.load(dTalents)
		if err != nil {
			log.Error("Talents %v", PlayerId)
			return
		}
		err = row.TowerCommon.load(dTowerCommon)
		if err != nil {
			log.Error("TowerCommon %v", PlayerId)
			return
		}
		err = row.Towers.load(dTowers)
		if err != nil {
			log.Error("Towers %v", PlayerId)
			return
		}
		err = row.Draws.load(dDraws)
		if err != nil {
			log.Error("Draws %v", PlayerId)
			return
		}
		err = row.GoldHand.load(dGoldHand)
		if err != nil {
			log.Error("GoldHand %v", PlayerId)
			return
		}
		err = row.Shops.load(dShops)
		if err != nil {
			log.Error("Shops %v", PlayerId)
			return
		}
		err = row.ShopItems.load(dShopItems)
		if err != nil {
			log.Error("ShopItems %v", PlayerId)
			return
		}
		err = row.Arena.load(dArena)
		if err != nil {
			log.Error("Arena %v", PlayerId)
			return
		}
		err = row.Equip.load(dEquip)
		if err != nil {
			log.Error("Equip %v", PlayerId)
			return
		}
		err = row.ActiveStageCommon.load(dActiveStageCommon)
		if err != nil {
			log.Error("ActiveStageCommon %v", PlayerId)
			return
		}
		err = row.ActiveStages.load(dActiveStages)
		if err != nil {
			log.Error("ActiveStages %v", PlayerId)
			return
		}
		err = row.FriendCommon.load(dFriendCommon)
		if err != nil {
			log.Error("FriendCommon %v", PlayerId)
			return
		}
		err = row.Friends.load(dFriends)
		if err != nil {
			log.Error("Friends %v", PlayerId)
			return
		}
		err = row.FriendRecommends.load(dFriendRecommends)
		if err != nil {
			log.Error("FriendRecommends %v", PlayerId)
			return
		}
		err = row.FriendAsks.load(dFriendAsks)
		if err != nil {
			log.Error("FriendAsks %v", PlayerId)
			return
		}
		err = row.FriendBosss.load(dFriendBosss)
		if err != nil {
			log.Error("FriendBosss %v", PlayerId)
			return
		}
		err = row.TaskCommon.load(dTaskCommon)
		if err != nil {
			log.Error("TaskCommon %v", PlayerId)
			return
		}
		err = row.Tasks.load(dTasks)
		if err != nil {
			log.Error("Tasks %v", PlayerId)
			return
		}
		err = row.FinishedTasks.load(dFinishedTasks)
		if err != nil {
			log.Error("FinishedTasks %v", PlayerId)
			return
		}
		err = row.DailyTaskAllDailys.load(dDailyTaskAllDailys)
		if err != nil {
			log.Error("DailyTaskAllDailys %v", PlayerId)
			return
		}
		err = row.ExploreCommon.load(dExploreCommon)
		if err != nil {
			log.Error("ExploreCommon %v", PlayerId)
			return
		}
		err = row.Explores.load(dExplores)
		if err != nil {
			log.Error("Explores %v", PlayerId)
			return
		}
		err = row.ExploreStorys.load(dExploreStorys)
		if err != nil {
			log.Error("ExploreStorys %v", PlayerId)
			return
		}
		err = row.FriendChatUnreadIds.load(dFriendChatUnreadIds)
		if err != nil {
			log.Error("FriendChatUnreadIds %v", PlayerId)
			return
		}
		err = row.FriendChatUnreadMessages.load(dFriendChatUnreadMessages)
		if err != nil {
			log.Error("FriendChatUnreadMessages %v", PlayerId)
			return
		}
		err = row.HeadItems.load(dHeadItems)
		if err != nil {
			log.Error("HeadItems %v", PlayerId)
			return
		}
		err = row.SuitAwards.load(dSuitAwards)
		if err != nil {
			log.Error("SuitAwards %v", PlayerId)
			return
		}
		err = row.Chats.load(dChats)
		if err != nil {
			log.Error("Chats %v", PlayerId)
			return
		}
		err = row.Anouncement.load(dAnouncement)
		if err != nil {
			log.Error("Anouncement %v", PlayerId)
			return
		}
		err = row.FirstDrawCards.load(dFirstDrawCards)
		if err != nil {
			log.Error("FirstDrawCards %v", PlayerId)
			return
		}
		err = row.Guild.load(dGuild)
		if err != nil {
			log.Error("Guild %v", PlayerId)
			return
		}
		err = row.GuildStage.load(dGuildStage)
		if err != nil {
			log.Error("GuildStage %v", PlayerId)
			return
		}
		err = row.Sign.load(dSign)
		if err != nil {
			log.Error("Sign %v", PlayerId)
			return
		}
		err = row.SevenDays.load(dSevenDays)
		if err != nil {
			log.Error("SevenDays %v", PlayerId)
			return
		}
		err = row.PayCommon.load(dPayCommon)
		if err != nil {
			log.Error("PayCommon %v", PlayerId)
			return
		}
		err = row.Pays.load(dPays)
		if err != nil {
			log.Error("Pays %v", PlayerId)
			return
		}
		err = row.GuideData.load(dGuideData)
		if err != nil {
			log.Error("GuideData %v", PlayerId)
			return
		}
		err = row.ActivityDatas.load(dActivityDatas)
		if err != nil {
			log.Error("ActivityDatas %v", PlayerId)
			return
		}
		err = row.ExpeditionData.load(dExpeditionData)
		if err != nil {
			log.Error("ExpeditionData %v", PlayerId)
			return
		}
		err = row.ExpeditionRoles.load(dExpeditionRoles)
		if err != nil {
			log.Error("ExpeditionRoles %v", PlayerId)
			return
		}
		err = row.ExpeditionLevels.load(dExpeditionLevels)
		if err != nil {
			log.Error("ExpeditionLevels %v", PlayerId)
			return
		}
		err = row.ExpeditionLevelRole0s.load(dExpeditionLevelRole0s)
		if err != nil {
			log.Error("ExpeditionLevelRole0s %v", PlayerId)
			return
		}
		err = row.ExpeditionLevelRole1s.load(dExpeditionLevelRole1s)
		if err != nil {
			log.Error("ExpeditionLevelRole1s %v", PlayerId)
			return
		}
		err = row.ExpeditionLevelRole2s.load(dExpeditionLevelRole2s)
		if err != nil {
			log.Error("ExpeditionLevelRole2s %v", PlayerId)
			return
		}
		err = row.ExpeditionLevelRole3s.load(dExpeditionLevelRole3s)
		if err != nil {
			log.Error("ExpeditionLevelRole3s %v", PlayerId)
			return
		}
		err = row.ExpeditionLevelRole4s.load(dExpeditionLevelRole4s)
		if err != nil {
			log.Error("ExpeditionLevelRole4s %v", PlayerId)
			return
		}
		err = row.ExpeditionLevelRole5s.load(dExpeditionLevelRole5s)
		if err != nil {
			log.Error("ExpeditionLevelRole5s %v", PlayerId)
			return
		}
		err = row.ExpeditionLevelRole6s.load(dExpeditionLevelRole6s)
		if err != nil {
			log.Error("ExpeditionLevelRole6s %v", PlayerId)
			return
		}
		err = row.ExpeditionLevelRole7s.load(dExpeditionLevelRole7s)
		if err != nil {
			log.Error("ExpeditionLevelRole7s %v", PlayerId)
			return
		}
		err = row.ExpeditionLevelRole8s.load(dExpeditionLevelRole8s)
		if err != nil {
			log.Error("ExpeditionLevelRole8s %v", PlayerId)
			return
		}
		err = row.ExpeditionLevelRole9s.load(dExpeditionLevelRole9s)
		if err != nil {
			log.Error("ExpeditionLevelRole9s %v", PlayerId)
			return
		}
		err = row.SysMail.load(dSysMail)
		if err != nil {
			log.Error("SysMail %v", PlayerId)
			return
		}
		err = row.Artifacts.load(dArtifacts)
		if err != nil {
			log.Error("Artifacts %v", PlayerId)
			return
		}
		err = row.CarnivalCommon.load(dCarnivalCommon)
		if err != nil {
			log.Error("CarnivalCommon %v", PlayerId)
			return
		}
		err = row.Carnivals.load(dCarnivals)
		if err != nil {
			log.Error("Carnivals %v", PlayerId)
			return
		}
		err = row.InviteCodess.load(dInviteCodess)
		if err != nil {
			log.Error("InviteCodess %v", PlayerId)
			return
		}
		row.m_UniqueId_changed=false
		row.m_Account_changed=false
		row.m_Name_changed=false
		row.m_Token_changed=false
		row.m_CurrReplyMsgNum_changed=false
		row.m_Level_changed=false
		row.m_valid = true
		th.m_rows[PlayerId]=row
	}
	return
}
func (th *dbPlayerTable) GetPreloadedMaxId() (max_id int32) {
	return th.m_preload_max_id
}
func (th *dbPlayerTable) fetch_rows(rows map[int32]*dbPlayerRow) (r map[int32]*dbPlayerRow) {
	th.m_lock.UnSafeLock("dbPlayerTable.fetch_rows")
	defer th.m_lock.UnSafeUnlock()
	r = make(map[int32]*dbPlayerRow)
	for i, v := range rows {
		r[i] = v
	}
	return r
}
func (th *dbPlayerTable) fetch_new_rows() (new_rows map[int32]*dbPlayerRow) {
	th.m_lock.UnSafeLock("dbPlayerTable.fetch_new_rows")
	defer th.m_lock.UnSafeUnlock()
	new_rows = make(map[int32]*dbPlayerRow)
	for i, v := range th.m_new_rows {
		_, has := th.m_rows[i]
		if has {
			log.Error("rows already has new rows %v", i)
			continue
		}
		th.m_rows[i] = v
		new_rows[i] = v
	}
	for i, _ := range new_rows {
		delete(th.m_new_rows, i)
	}
	return
}
func (th *dbPlayerTable) save_rows(rows map[int32]*dbPlayerRow, quick bool) {
	for _, v := range rows {
		if th.m_dbc.m_quit && !quick {
			return
		}
		err, delay, _ := v.Save(false)
		if err != nil {
			log.Error("save failed %v", err)
		}
		if th.m_dbc.m_quit && !quick {
			return
		}
		if delay&&!quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
}
func (th *dbPlayerTable) Save(quick bool) (err error){
	removed_rows := th.fetch_rows(th.m_removed_rows)
	for _, v := range removed_rows {
		_, err := th.m_dbc.StmtExec(th.m_delete_stmt, v.GetPlayerId())
		if err != nil {
			log.Error("exec delete stmt failed %v", err)
		}
		v.m_valid = false
		if !quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
	th.m_removed_rows = make(map[int32]*dbPlayerRow)
	rows := th.fetch_rows(th.m_rows)
	th.save_rows(rows, quick)
	new_rows := th.fetch_new_rows()
	th.save_rows(new_rows, quick)
	return
}
func (th *dbPlayerTable) AddRow(PlayerId int32) (row *dbPlayerRow) {
	th.m_lock.UnSafeLock("dbPlayerTable.AddRow")
	defer th.m_lock.UnSafeUnlock()
	row = new_dbPlayerRow(th,PlayerId)
	row.m_new = true
	row.m_loaded = true
	row.m_valid = true
	_, has := th.m_new_rows[PlayerId]
	if has{
		log.Error("已经存在 %v", PlayerId)
		return nil
	}
	th.m_new_rows[PlayerId] = row
	atomic.AddInt32(&th.m_gc_n,1)
	return row
}
func (th *dbPlayerTable) RemoveRow(PlayerId int32) {
	th.m_lock.UnSafeLock("dbPlayerTable.RemoveRow")
	defer th.m_lock.UnSafeUnlock()
	row := th.m_rows[PlayerId]
	if row != nil {
		row.m_remove = true
		delete(th.m_rows, PlayerId)
		rm_row := th.m_removed_rows[PlayerId]
		if rm_row != nil {
			log.Error("rows and removed rows both has %v", PlayerId)
		}
		th.m_removed_rows[PlayerId] = row
		_, has_new := th.m_new_rows[PlayerId]
		if has_new {
			delete(th.m_new_rows, PlayerId)
			log.Error("rows and new_rows both has %v", PlayerId)
		}
	} else {
		row = th.m_removed_rows[PlayerId]
		if row == nil {
			_, has_new := th.m_new_rows[PlayerId]
			if has_new {
				delete(th.m_new_rows, PlayerId)
			} else {
				log.Error("row not exist %v", PlayerId)
			}
		} else {
			log.Error("already removed %v", PlayerId)
			_, has_new := th.m_new_rows[PlayerId]
			if has_new {
				delete(th.m_new_rows, PlayerId)
				log.Error("removed rows and new_rows both has %v", PlayerId)
			}
		}
	}
}
func (th *dbPlayerTable) GetRow(PlayerId int32) (row *dbPlayerRow) {
	th.m_lock.UnSafeRLock("dbPlayerTable.GetRow")
	defer th.m_lock.UnSafeRUnlock()
	row = th.m_rows[PlayerId]
	if row == nil {
		row = th.m_new_rows[PlayerId]
	}
	return row
}
type dbBattleSaveDataColumn struct{
	m_row *dbBattleSaveRow
	m_data *dbBattleSaveDataData
	m_changed bool
}
func (th *dbBattleSaveDataColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbBattleSaveDataData{}
		th.m_changed = false
		return nil
	}
	pb := &db.BattleSaveData{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetId())
		return
	}
	th.m_data = &dbBattleSaveDataData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbBattleSaveDataColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbBattleSaveDataColumn)Get( )(v *dbBattleSaveDataData ){
	th.m_row.m_lock.UnSafeRLock("dbBattleSaveDataColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbBattleSaveDataData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbBattleSaveDataColumn)Set(v dbBattleSaveDataData ){
	th.m_row.m_lock.UnSafeLock("dbBattleSaveDataColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbBattleSaveDataData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbBattleSaveDataColumn)GetData( )(v []byte){
	th.m_row.m_lock.UnSafeRLock("dbBattleSaveDataColumn.GetData")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = make([]byte, len(th.m_data.Data))
	for _ii, _vv := range th.m_data.Data {
		v[_ii]=_vv
	}
	return
}
func (th *dbBattleSaveDataColumn)SetData(v []byte){
	th.m_row.m_lock.UnSafeLock("dbBattleSaveDataColumn.SetData")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Data = make([]byte, len(v))
	for _ii, _vv := range v {
		th.m_data.Data[_ii]=_vv
	}
	th.m_changed = true
	return
}
func (th *dbBattleSaveRow)GetSaveTime( )(r int32 ){
	th.m_lock.UnSafeRLock("dbBattleSaveRow.GetdbBattleSaveSaveTimeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_SaveTime)
}
func (th *dbBattleSaveRow)SetSaveTime(v int32){
	th.m_lock.UnSafeLock("dbBattleSaveRow.SetdbBattleSaveSaveTimeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_SaveTime=int32(v)
	th.m_SaveTime_changed=true
	return
}
func (th *dbBattleSaveRow)GetAttacker( )(r int32 ){
	th.m_lock.UnSafeRLock("dbBattleSaveRow.GetdbBattleSaveAttackerColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_Attacker)
}
func (th *dbBattleSaveRow)SetAttacker(v int32){
	th.m_lock.UnSafeLock("dbBattleSaveRow.SetdbBattleSaveAttackerColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Attacker=int32(v)
	th.m_Attacker_changed=true
	return
}
func (th *dbBattleSaveRow)GetDefenser( )(r int32 ){
	th.m_lock.UnSafeRLock("dbBattleSaveRow.GetdbBattleSaveDefenserColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_Defenser)
}
func (th *dbBattleSaveRow)SetDefenser(v int32){
	th.m_lock.UnSafeLock("dbBattleSaveRow.SetdbBattleSaveDefenserColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Defenser=int32(v)
	th.m_Defenser_changed=true
	return
}
func (th *dbBattleSaveRow)GetDeleteState( )(r int32 ){
	th.m_lock.UnSafeRLock("dbBattleSaveRow.GetdbBattleSaveDeleteStateColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_DeleteState)
}
func (th *dbBattleSaveRow)SetDeleteState(v int32){
	th.m_lock.UnSafeLock("dbBattleSaveRow.SetdbBattleSaveDeleteStateColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_DeleteState=int32(v)
	th.m_DeleteState_changed=true
	return
}
func (th *dbBattleSaveRow)GetIsWin( )(r int32 ){
	th.m_lock.UnSafeRLock("dbBattleSaveRow.GetdbBattleSaveIsWinColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_IsWin)
}
func (th *dbBattleSaveRow)SetIsWin(v int32){
	th.m_lock.UnSafeLock("dbBattleSaveRow.SetdbBattleSaveIsWinColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_IsWin=int32(v)
	th.m_IsWin_changed=true
	return
}
func (th *dbBattleSaveRow)GetAddScore( )(r int32 ){
	th.m_lock.UnSafeRLock("dbBattleSaveRow.GetdbBattleSaveAddScoreColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_AddScore)
}
func (th *dbBattleSaveRow)SetAddScore(v int32){
	th.m_lock.UnSafeLock("dbBattleSaveRow.SetdbBattleSaveAddScoreColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_AddScore=int32(v)
	th.m_AddScore_changed=true
	return
}
type dbBattleSaveRow struct {
	m_table *dbBattleSaveTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_Id        int32
	Data dbBattleSaveDataColumn
	m_SaveTime_changed bool
	m_SaveTime int32
	m_Attacker_changed bool
	m_Attacker int32
	m_Defenser_changed bool
	m_Defenser int32
	m_DeleteState_changed bool
	m_DeleteState int32
	m_IsWin_changed bool
	m_IsWin int32
	m_AddScore_changed bool
	m_AddScore int32
}
func new_dbBattleSaveRow(table *dbBattleSaveTable, Id int32) (r *dbBattleSaveRow) {
	th := &dbBattleSaveRow{}
	th.m_table = table
	th.m_Id = Id
	th.m_lock = NewRWMutex()
	th.m_SaveTime_changed=true
	th.m_Attacker_changed=true
	th.m_Defenser_changed=true
	th.m_DeleteState_changed=true
	th.m_IsWin_changed=true
	th.m_AddScore_changed=true
	th.Data.m_row=th
	th.Data.m_data=&dbBattleSaveDataData{}
	return th
}
func (th *dbBattleSaveRow) GetId() (r int32) {
	return th.m_Id
}
func (th *dbBattleSaveRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbBattleSaveRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(8)
		db_args.Push(th.m_Id)
		dData,db_err:=th.Data.save()
		if db_err!=nil{
			log.Error("insert save Data failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dData)
		db_args.Push(th.m_SaveTime)
		db_args.Push(th.m_Attacker)
		db_args.Push(th.m_Defenser)
		db_args.Push(th.m_DeleteState)
		db_args.Push(th.m_IsWin)
		db_args.Push(th.m_AddScore)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.Data.m_changed||th.m_SaveTime_changed||th.m_Attacker_changed||th.m_Defenser_changed||th.m_DeleteState_changed||th.m_IsWin_changed||th.m_AddScore_changed{
			update_string = "UPDATE BattleSaves SET "
			db_args:=new_db_args(8)
			if th.Data.m_changed{
				update_string+="Data=?,"
				dData,err:=th.Data.save()
				if err!=nil{
					log.Error("update save Data failed")
					return err,false,0,"",nil
				}
				db_args.Push(dData)
			}
			if th.m_SaveTime_changed{
				update_string+="SaveTime=?,"
				db_args.Push(th.m_SaveTime)
			}
			if th.m_Attacker_changed{
				update_string+="Attacker=?,"
				db_args.Push(th.m_Attacker)
			}
			if th.m_Defenser_changed{
				update_string+="Defenser=?,"
				db_args.Push(th.m_Defenser)
			}
			if th.m_DeleteState_changed{
				update_string+="DeleteState=?,"
				db_args.Push(th.m_DeleteState)
			}
			if th.m_IsWin_changed{
				update_string+="IsWin=?,"
				db_args.Push(th.m_IsWin)
			}
			if th.m_AddScore_changed{
				update_string+="AddScore=?,"
				db_args.Push(th.m_AddScore)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE Id=?"
			db_args.Push(th.m_Id)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.Data.m_changed = false
	th.m_SaveTime_changed = false
	th.m_Attacker_changed = false
	th.m_Defenser_changed = false
	th.m_DeleteState_changed = false
	th.m_IsWin_changed = false
	th.m_AddScore_changed = false
	if release && th.m_loaded {
		atomic.AddInt32(&th.m_table.m_gc_n, -1)
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbBattleSaveRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT BattleSaves exec failed %v ", th.m_Id)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE BattleSaves exec failed %v", th.m_Id)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
func (th *dbBattleSaveRow) Touch(releasable bool) {
	th.m_touch = int32(time.Now().Unix())
	th.m_releasable = releasable
}
type dbBattleSaveRowSort struct {
	rows []*dbBattleSaveRow
}
func (th *dbBattleSaveRowSort) Len() (length int) {
	return len(th.rows)
}
func (th *dbBattleSaveRowSort) Less(i int, j int) (less bool) {
	return th.rows[i].m_touch < th.rows[j].m_touch
}
func (th *dbBattleSaveRowSort) Swap(i int, j int) {
	temp := th.rows[i]
	th.rows[i] = th.rows[j]
	th.rows[j] = temp
}
type dbBattleSaveTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_rows map[int32]*dbBattleSaveRow
	m_new_rows map[int32]*dbBattleSaveRow
	m_removed_rows map[int32]*dbBattleSaveRow
	m_gc_n int32
	m_gcing int32
	m_pool_size int32
	m_preload_select_stmt *sql.Stmt
	m_preload_max_id int32
	m_save_insert_stmt *sql.Stmt
	m_delete_stmt *sql.Stmt
	m_max_id int32
	m_max_id_changed bool
}
func new_dbBattleSaveTable(dbc *DBC) (th *dbBattleSaveTable) {
	th = &dbBattleSaveTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	th.m_rows = make(map[int32]*dbBattleSaveRow)
	th.m_new_rows = make(map[int32]*dbBattleSaveRow)
	th.m_removed_rows = make(map[int32]*dbBattleSaveRow)
	return th
}
func (th *dbBattleSaveTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS BattleSavesMaxId(PlaceHolder int(11),MaxId int(11),PRIMARY KEY (PlaceHolder))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS BattleSavesMaxId failed")
		return
	}
	r := th.m_dbc.QueryRow("SELECT Count(*) FROM BattleSavesMaxId WHERE PlaceHolder=0")
	if r != nil {
		var count int32
		err = r.Scan(&count)
		if err != nil {
			log.Error("scan count failed")
			return
		}
		if count == 0 {
		_, err = th.m_dbc.Exec("INSERT INTO BattleSavesMaxId (PlaceHolder,MaxId) VALUES (0,0)")
			if err != nil {
				log.Error("INSERTBattleSavesMaxId failed")
				return
			}
		}
	}
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS BattleSaves(Id int(11),PRIMARY KEY (Id))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS BattleSaves failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='BattleSaves'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasData := columns["Data"]
	if !hasData {
		_, err = th.m_dbc.Exec("ALTER TABLE BattleSaves ADD COLUMN Data LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Data failed")
			return
		}
	}
	_, hasSaveTime := columns["SaveTime"]
	if !hasSaveTime {
		_, err = th.m_dbc.Exec("ALTER TABLE BattleSaves ADD COLUMN SaveTime int(11)")
		if err != nil {
			log.Error("ADD COLUMN SaveTime failed")
			return
		}
	}
	_, hasAttacker := columns["Attacker"]
	if !hasAttacker {
		_, err = th.m_dbc.Exec("ALTER TABLE BattleSaves ADD COLUMN Attacker int(11)")
		if err != nil {
			log.Error("ADD COLUMN Attacker failed")
			return
		}
	}
	_, hasDefenser := columns["Defenser"]
	if !hasDefenser {
		_, err = th.m_dbc.Exec("ALTER TABLE BattleSaves ADD COLUMN Defenser int(11)")
		if err != nil {
			log.Error("ADD COLUMN Defenser failed")
			return
		}
	}
	_, hasDeleteState := columns["DeleteState"]
	if !hasDeleteState {
		_, err = th.m_dbc.Exec("ALTER TABLE BattleSaves ADD COLUMN DeleteState int(11)")
		if err != nil {
			log.Error("ADD COLUMN DeleteState failed")
			return
		}
	}
	_, hasIsWin := columns["IsWin"]
	if !hasIsWin {
		_, err = th.m_dbc.Exec("ALTER TABLE BattleSaves ADD COLUMN IsWin int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN IsWin failed")
			return
		}
	}
	_, hasAddScore := columns["AddScore"]
	if !hasAddScore {
		_, err = th.m_dbc.Exec("ALTER TABLE BattleSaves ADD COLUMN AddScore int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN AddScore failed")
			return
		}
	}
	return
}
func (th *dbBattleSaveTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT Id,Data,SaveTime,Attacker,Defenser,DeleteState,IsWin,AddScore FROM BattleSaves")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbBattleSaveTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO BattleSaves (Id,Data,SaveTime,Attacker,Defenser,DeleteState,IsWin,AddScore) VALUES (?,?,?,?,?,?,?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbBattleSaveTable) prepare_delete_stmt() (err error) {
	th.m_delete_stmt,err=th.m_dbc.StmtPrepare("DELETE FROM BattleSaves WHERE Id=?")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbBattleSaveTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	err=th.prepare_delete_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbBattleSaveTable) Preload() (err error) {
	r_max_id := th.m_dbc.QueryRow("SELECT MaxId FROM BattleSavesMaxId WHERE PLACEHOLDER=0")
	if r_max_id != nil {
		err = r_max_id.Scan(&th.m_max_id)
		if err != nil {
			log.Error("scan max id failed")
			return
		}
	}
	r, err := th.m_dbc.StmtQuery(th.m_preload_select_stmt)
	if err != nil {
		log.Error("SELECT")
		return
	}
	var Id int32
	var dData []byte
	var dSaveTime int32
	var dAttacker int32
	var dDefenser int32
	var dDeleteState int32
	var dIsWin int32
	var dAddScore int32
	for r.Next() {
		err = r.Scan(&Id,&dData,&dSaveTime,&dAttacker,&dDefenser,&dDeleteState,&dIsWin,&dAddScore)
		if err != nil {
			log.Error("Scan err[%v]", err.Error())
			return
		}
		if Id>th.m_max_id{
			log.Error("max id ext")
			th.m_max_id = Id
			th.m_max_id_changed = true
		}
		row := new_dbBattleSaveRow(th,Id)
		err = row.Data.load(dData)
		if err != nil {
			log.Error("Data %v", Id)
			return
		}
		row.m_SaveTime=dSaveTime
		row.m_Attacker=dAttacker
		row.m_Defenser=dDefenser
		row.m_DeleteState=dDeleteState
		row.m_IsWin=dIsWin
		row.m_AddScore=dAddScore
		row.m_SaveTime_changed=false
		row.m_Attacker_changed=false
		row.m_Defenser_changed=false
		row.m_DeleteState_changed=false
		row.m_IsWin_changed=false
		row.m_AddScore_changed=false
		row.m_valid = true
		th.m_rows[Id]=row
	}
	return
}
func (th *dbBattleSaveTable) GetPreloadedMaxId() (max_id int32) {
	return th.m_preload_max_id
}
func (th *dbBattleSaveTable) fetch_rows(rows map[int32]*dbBattleSaveRow) (r map[int32]*dbBattleSaveRow) {
	th.m_lock.UnSafeLock("dbBattleSaveTable.fetch_rows")
	defer th.m_lock.UnSafeUnlock()
	r = make(map[int32]*dbBattleSaveRow)
	for i, v := range rows {
		r[i] = v
	}
	return r
}
func (th *dbBattleSaveTable) fetch_new_rows() (new_rows map[int32]*dbBattleSaveRow) {
	th.m_lock.UnSafeLock("dbBattleSaveTable.fetch_new_rows")
	defer th.m_lock.UnSafeUnlock()
	new_rows = make(map[int32]*dbBattleSaveRow)
	for i, v := range th.m_new_rows {
		_, has := th.m_rows[i]
		if has {
			log.Error("rows already has new rows %v", i)
			continue
		}
		th.m_rows[i] = v
		new_rows[i] = v
	}
	for i, _ := range new_rows {
		delete(th.m_new_rows, i)
	}
	return
}
func (th *dbBattleSaveTable) save_rows(rows map[int32]*dbBattleSaveRow, quick bool) {
	for _, v := range rows {
		if th.m_dbc.m_quit && !quick {
			return
		}
		err, delay, _ := v.Save(false)
		if err != nil {
			log.Error("save failed %v", err)
		}
		if th.m_dbc.m_quit && !quick {
			return
		}
		if delay&&!quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
}
func (th *dbBattleSaveTable) Save(quick bool) (err error){
	if th.m_max_id_changed {
		max_id := atomic.LoadInt32(&th.m_max_id)
		_, err := th.m_dbc.Exec("UPDATE BattleSavesMaxId SET MaxId=?", max_id)
		if err != nil {
			log.Error("save max id failed %v", err)
		}
	}
	removed_rows := th.fetch_rows(th.m_removed_rows)
	for _, v := range removed_rows {
		_, err := th.m_dbc.StmtExec(th.m_delete_stmt, v.GetId())
		if err != nil {
			log.Error("exec delete stmt failed %v", err)
		}
		v.m_valid = false
		if !quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
	th.m_removed_rows = make(map[int32]*dbBattleSaveRow)
	rows := th.fetch_rows(th.m_rows)
	th.save_rows(rows, quick)
	new_rows := th.fetch_new_rows()
	th.save_rows(new_rows, quick)
	return
}
func (th *dbBattleSaveTable) AddRow() (row *dbBattleSaveRow) {
	th.m_lock.UnSafeLock("dbBattleSaveTable.AddRow")
	defer th.m_lock.UnSafeUnlock()
	Id := atomic.AddInt32(&th.m_max_id, 1)
	th.m_max_id_changed = true
	row = new_dbBattleSaveRow(th,Id)
	row.m_new = true
	row.m_loaded = true
	row.m_valid = true
	th.m_new_rows[Id] = row
	atomic.AddInt32(&th.m_gc_n,1)
	return row
}
func (th *dbBattleSaveTable) RemoveRow(Id int32) {
	th.m_lock.UnSafeLock("dbBattleSaveTable.RemoveRow")
	defer th.m_lock.UnSafeUnlock()
	row := th.m_rows[Id]
	if row != nil {
		row.m_remove = true
		delete(th.m_rows, Id)
		rm_row := th.m_removed_rows[Id]
		if rm_row != nil {
			log.Error("rows and removed rows both has %v", Id)
		}
		th.m_removed_rows[Id] = row
		_, has_new := th.m_new_rows[Id]
		if has_new {
			delete(th.m_new_rows, Id)
			log.Error("rows and new_rows both has %v", Id)
		}
	} else {
		row = th.m_removed_rows[Id]
		if row == nil {
			_, has_new := th.m_new_rows[Id]
			if has_new {
				delete(th.m_new_rows, Id)
			} else {
				log.Error("row not exist %v", Id)
			}
		} else {
			log.Error("already removed %v", Id)
			_, has_new := th.m_new_rows[Id]
			if has_new {
				delete(th.m_new_rows, Id)
				log.Error("removed rows and new_rows both has %v", Id)
			}
		}
	}
}
func (th *dbBattleSaveTable) GetRow(Id int32) (row *dbBattleSaveRow) {
	th.m_lock.UnSafeRLock("dbBattleSaveTable.GetRow")
	defer th.m_lock.UnSafeRUnlock()
	row = th.m_rows[Id]
	if row == nil {
		row = th.m_new_rows[Id]
	}
	return row
}
type dbTowerFightSaveDataColumn struct{
	m_row *dbTowerFightSaveRow
	m_data *dbTowerFightSaveDataData
	m_changed bool
}
func (th *dbTowerFightSaveDataColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbTowerFightSaveDataData{}
		th.m_changed = false
		return nil
	}
	pb := &db.TowerFightSaveData{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetTowerFightId())
		return
	}
	th.m_data = &dbTowerFightSaveDataData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbTowerFightSaveDataColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetTowerFightId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbTowerFightSaveDataColumn)Get( )(v *dbTowerFightSaveDataData ){
	th.m_row.m_lock.UnSafeRLock("dbTowerFightSaveDataColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbTowerFightSaveDataData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbTowerFightSaveDataColumn)Set(v dbTowerFightSaveDataData ){
	th.m_row.m_lock.UnSafeLock("dbTowerFightSaveDataColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbTowerFightSaveDataData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbTowerFightSaveDataColumn)GetData( )(v []byte){
	th.m_row.m_lock.UnSafeRLock("dbTowerFightSaveDataColumn.GetData")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = make([]byte, len(th.m_data.Data))
	for _ii, _vv := range th.m_data.Data {
		v[_ii]=_vv
	}
	return
}
func (th *dbTowerFightSaveDataColumn)SetData(v []byte){
	th.m_row.m_lock.UnSafeLock("dbTowerFightSaveDataColumn.SetData")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.Data = make([]byte, len(v))
	for _ii, _vv := range v {
		th.m_data.Data[_ii]=_vv
	}
	th.m_changed = true
	return
}
func (th *dbTowerFightSaveRow)GetSaveTime( )(r int32 ){
	th.m_lock.UnSafeRLock("dbTowerFightSaveRow.GetdbTowerFightSaveSaveTimeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_SaveTime)
}
func (th *dbTowerFightSaveRow)SetSaveTime(v int32){
	th.m_lock.UnSafeLock("dbTowerFightSaveRow.SetdbTowerFightSaveSaveTimeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_SaveTime=int32(v)
	th.m_SaveTime_changed=true
	return
}
func (th *dbTowerFightSaveRow)GetAttacker( )(r int32 ){
	th.m_lock.UnSafeRLock("dbTowerFightSaveRow.GetdbTowerFightSaveAttackerColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_Attacker)
}
func (th *dbTowerFightSaveRow)SetAttacker(v int32){
	th.m_lock.UnSafeLock("dbTowerFightSaveRow.SetdbTowerFightSaveAttackerColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Attacker=int32(v)
	th.m_Attacker_changed=true
	return
}
func (th *dbTowerFightSaveRow)GetTowerId( )(r int32 ){
	th.m_lock.UnSafeRLock("dbTowerFightSaveRow.GetdbTowerFightSaveTowerIdColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_TowerId)
}
func (th *dbTowerFightSaveRow)SetTowerId(v int32){
	th.m_lock.UnSafeLock("dbTowerFightSaveRow.SetdbTowerFightSaveTowerIdColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_TowerId=int32(v)
	th.m_TowerId_changed=true
	return
}
type dbTowerFightSaveRow struct {
	m_table *dbTowerFightSaveTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_TowerFightId        int32
	Data dbTowerFightSaveDataColumn
	m_SaveTime_changed bool
	m_SaveTime int32
	m_Attacker_changed bool
	m_Attacker int32
	m_TowerId_changed bool
	m_TowerId int32
}
func new_dbTowerFightSaveRow(table *dbTowerFightSaveTable, TowerFightId int32) (r *dbTowerFightSaveRow) {
	th := &dbTowerFightSaveRow{}
	th.m_table = table
	th.m_TowerFightId = TowerFightId
	th.m_lock = NewRWMutex()
	th.m_SaveTime_changed=true
	th.m_Attacker_changed=true
	th.m_TowerId_changed=true
	th.Data.m_row=th
	th.Data.m_data=&dbTowerFightSaveDataData{}
	return th
}
func (th *dbTowerFightSaveRow) GetTowerFightId() (r int32) {
	return th.m_TowerFightId
}
func (th *dbTowerFightSaveRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbTowerFightSaveRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(5)
		db_args.Push(th.m_TowerFightId)
		dData,db_err:=th.Data.save()
		if db_err!=nil{
			log.Error("insert save Data failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dData)
		db_args.Push(th.m_SaveTime)
		db_args.Push(th.m_Attacker)
		db_args.Push(th.m_TowerId)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.Data.m_changed||th.m_SaveTime_changed||th.m_Attacker_changed||th.m_TowerId_changed{
			update_string = "UPDATE TowerFightSaves SET "
			db_args:=new_db_args(5)
			if th.Data.m_changed{
				update_string+="Data=?,"
				dData,err:=th.Data.save()
				if err!=nil{
					log.Error("update save Data failed")
					return err,false,0,"",nil
				}
				db_args.Push(dData)
			}
			if th.m_SaveTime_changed{
				update_string+="SaveTime=?,"
				db_args.Push(th.m_SaveTime)
			}
			if th.m_Attacker_changed{
				update_string+="Attacker=?,"
				db_args.Push(th.m_Attacker)
			}
			if th.m_TowerId_changed{
				update_string+="TowerId=?,"
				db_args.Push(th.m_TowerId)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE TowerFightId=?"
			db_args.Push(th.m_TowerFightId)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.Data.m_changed = false
	th.m_SaveTime_changed = false
	th.m_Attacker_changed = false
	th.m_TowerId_changed = false
	if release && th.m_loaded {
		atomic.AddInt32(&th.m_table.m_gc_n, -1)
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbTowerFightSaveRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT TowerFightSaves exec failed %v ", th.m_TowerFightId)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE TowerFightSaves exec failed %v", th.m_TowerFightId)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
func (th *dbTowerFightSaveRow) Touch(releasable bool) {
	th.m_touch = int32(time.Now().Unix())
	th.m_releasable = releasable
}
type dbTowerFightSaveRowSort struct {
	rows []*dbTowerFightSaveRow
}
func (th *dbTowerFightSaveRowSort) Len() (length int) {
	return len(th.rows)
}
func (th *dbTowerFightSaveRowSort) Less(i int, j int) (less bool) {
	return th.rows[i].m_touch < th.rows[j].m_touch
}
func (th *dbTowerFightSaveRowSort) Swap(i int, j int) {
	temp := th.rows[i]
	th.rows[i] = th.rows[j]
	th.rows[j] = temp
}
type dbTowerFightSaveTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_rows map[int32]*dbTowerFightSaveRow
	m_new_rows map[int32]*dbTowerFightSaveRow
	m_removed_rows map[int32]*dbTowerFightSaveRow
	m_gc_n int32
	m_gcing int32
	m_pool_size int32
	m_preload_select_stmt *sql.Stmt
	m_preload_max_id int32
	m_save_insert_stmt *sql.Stmt
	m_delete_stmt *sql.Stmt
}
func new_dbTowerFightSaveTable(dbc *DBC) (th *dbTowerFightSaveTable) {
	th = &dbTowerFightSaveTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	th.m_rows = make(map[int32]*dbTowerFightSaveRow)
	th.m_new_rows = make(map[int32]*dbTowerFightSaveRow)
	th.m_removed_rows = make(map[int32]*dbTowerFightSaveRow)
	return th
}
func (th *dbTowerFightSaveTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS TowerFightSaves(TowerFightId int(11),PRIMARY KEY (TowerFightId))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS TowerFightSaves failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='TowerFightSaves'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasData := columns["Data"]
	if !hasData {
		_, err = th.m_dbc.Exec("ALTER TABLE TowerFightSaves ADD COLUMN Data LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Data failed")
			return
		}
	}
	_, hasSaveTime := columns["SaveTime"]
	if !hasSaveTime {
		_, err = th.m_dbc.Exec("ALTER TABLE TowerFightSaves ADD COLUMN SaveTime int(11)")
		if err != nil {
			log.Error("ADD COLUMN SaveTime failed")
			return
		}
	}
	_, hasAttacker := columns["Attacker"]
	if !hasAttacker {
		_, err = th.m_dbc.Exec("ALTER TABLE TowerFightSaves ADD COLUMN Attacker int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN Attacker failed")
			return
		}
	}
	_, hasTowerId := columns["TowerId"]
	if !hasTowerId {
		_, err = th.m_dbc.Exec("ALTER TABLE TowerFightSaves ADD COLUMN TowerId int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN TowerId failed")
			return
		}
	}
	return
}
func (th *dbTowerFightSaveTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT TowerFightId,Data,SaveTime,Attacker,TowerId FROM TowerFightSaves")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbTowerFightSaveTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO TowerFightSaves (TowerFightId,Data,SaveTime,Attacker,TowerId) VALUES (?,?,?,?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbTowerFightSaveTable) prepare_delete_stmt() (err error) {
	th.m_delete_stmt,err=th.m_dbc.StmtPrepare("DELETE FROM TowerFightSaves WHERE TowerFightId=?")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbTowerFightSaveTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	err=th.prepare_delete_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbTowerFightSaveTable) Preload() (err error) {
	r, err := th.m_dbc.StmtQuery(th.m_preload_select_stmt)
	if err != nil {
		log.Error("SELECT")
		return
	}
	var TowerFightId int32
	var dData []byte
	var dSaveTime int32
	var dAttacker int32
	var dTowerId int32
		th.m_preload_max_id = 0
	for r.Next() {
		err = r.Scan(&TowerFightId,&dData,&dSaveTime,&dAttacker,&dTowerId)
		if err != nil {
			log.Error("Scan err[%v]", err.Error())
			return
		}
		if TowerFightId>th.m_preload_max_id{
			th.m_preload_max_id =TowerFightId
		}
		row := new_dbTowerFightSaveRow(th,TowerFightId)
		err = row.Data.load(dData)
		if err != nil {
			log.Error("Data %v", TowerFightId)
			return
		}
		row.m_SaveTime=dSaveTime
		row.m_Attacker=dAttacker
		row.m_TowerId=dTowerId
		row.m_SaveTime_changed=false
		row.m_Attacker_changed=false
		row.m_TowerId_changed=false
		row.m_valid = true
		th.m_rows[TowerFightId]=row
	}
	return
}
func (th *dbTowerFightSaveTable) GetPreloadedMaxId() (max_id int32) {
	return th.m_preload_max_id
}
func (th *dbTowerFightSaveTable) fetch_rows(rows map[int32]*dbTowerFightSaveRow) (r map[int32]*dbTowerFightSaveRow) {
	th.m_lock.UnSafeLock("dbTowerFightSaveTable.fetch_rows")
	defer th.m_lock.UnSafeUnlock()
	r = make(map[int32]*dbTowerFightSaveRow)
	for i, v := range rows {
		r[i] = v
	}
	return r
}
func (th *dbTowerFightSaveTable) fetch_new_rows() (new_rows map[int32]*dbTowerFightSaveRow) {
	th.m_lock.UnSafeLock("dbTowerFightSaveTable.fetch_new_rows")
	defer th.m_lock.UnSafeUnlock()
	new_rows = make(map[int32]*dbTowerFightSaveRow)
	for i, v := range th.m_new_rows {
		_, has := th.m_rows[i]
		if has {
			log.Error("rows already has new rows %v", i)
			continue
		}
		th.m_rows[i] = v
		new_rows[i] = v
	}
	for i, _ := range new_rows {
		delete(th.m_new_rows, i)
	}
	return
}
func (th *dbTowerFightSaveTable) save_rows(rows map[int32]*dbTowerFightSaveRow, quick bool) {
	for _, v := range rows {
		if th.m_dbc.m_quit && !quick {
			return
		}
		err, delay, _ := v.Save(false)
		if err != nil {
			log.Error("save failed %v", err)
		}
		if th.m_dbc.m_quit && !quick {
			return
		}
		if delay&&!quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
}
func (th *dbTowerFightSaveTable) Save(quick bool) (err error){
	removed_rows := th.fetch_rows(th.m_removed_rows)
	for _, v := range removed_rows {
		_, err := th.m_dbc.StmtExec(th.m_delete_stmt, v.GetTowerFightId())
		if err != nil {
			log.Error("exec delete stmt failed %v", err)
		}
		v.m_valid = false
		if !quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
	th.m_removed_rows = make(map[int32]*dbTowerFightSaveRow)
	rows := th.fetch_rows(th.m_rows)
	th.save_rows(rows, quick)
	new_rows := th.fetch_new_rows()
	th.save_rows(new_rows, quick)
	return
}
func (th *dbTowerFightSaveTable) AddRow(TowerFightId int32) (row *dbTowerFightSaveRow) {
	th.m_lock.UnSafeLock("dbTowerFightSaveTable.AddRow")
	defer th.m_lock.UnSafeUnlock()
	row = new_dbTowerFightSaveRow(th,TowerFightId)
	row.m_new = true
	row.m_loaded = true
	row.m_valid = true
	_, has := th.m_new_rows[TowerFightId]
	if has{
		log.Error("已经存在 %v", TowerFightId)
		return nil
	}
	th.m_new_rows[TowerFightId] = row
	atomic.AddInt32(&th.m_gc_n,1)
	return row
}
func (th *dbTowerFightSaveTable) RemoveRow(TowerFightId int32) {
	th.m_lock.UnSafeLock("dbTowerFightSaveTable.RemoveRow")
	defer th.m_lock.UnSafeUnlock()
	row := th.m_rows[TowerFightId]
	if row != nil {
		row.m_remove = true
		delete(th.m_rows, TowerFightId)
		rm_row := th.m_removed_rows[TowerFightId]
		if rm_row != nil {
			log.Error("rows and removed rows both has %v", TowerFightId)
		}
		th.m_removed_rows[TowerFightId] = row
		_, has_new := th.m_new_rows[TowerFightId]
		if has_new {
			delete(th.m_new_rows, TowerFightId)
			log.Error("rows and new_rows both has %v", TowerFightId)
		}
	} else {
		row = th.m_removed_rows[TowerFightId]
		if row == nil {
			_, has_new := th.m_new_rows[TowerFightId]
			if has_new {
				delete(th.m_new_rows, TowerFightId)
			} else {
				log.Error("row not exist %v", TowerFightId)
			}
		} else {
			log.Error("already removed %v", TowerFightId)
			_, has_new := th.m_new_rows[TowerFightId]
			if has_new {
				delete(th.m_new_rows, TowerFightId)
				log.Error("removed rows and new_rows both has %v", TowerFightId)
			}
		}
	}
}
func (th *dbTowerFightSaveTable) GetRow(TowerFightId int32) (row *dbTowerFightSaveRow) {
	th.m_lock.UnSafeRLock("dbTowerFightSaveTable.GetRow")
	defer th.m_lock.UnSafeRUnlock()
	row = th.m_rows[TowerFightId]
	if row == nil {
		row = th.m_new_rows[TowerFightId]
	}
	return row
}
type dbArenaSeasonDataColumn struct{
	m_row *dbArenaSeasonRow
	m_data *dbArenaSeasonDataData
	m_changed bool
}
func (th *dbArenaSeasonDataColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbArenaSeasonDataData{}
		th.m_changed = false
		return nil
	}
	pb := &db.ArenaSeasonData{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal ")
		return
	}
	th.m_data = &dbArenaSeasonDataData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbArenaSeasonDataColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Unmarshal ")
		return
	}
	th.m_changed = false
	return
}
func (th *dbArenaSeasonDataColumn)Get( )(v *dbArenaSeasonDataData ){
	th.m_row.m_lock.UnSafeRLock("dbArenaSeasonDataColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbArenaSeasonDataData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbArenaSeasonDataColumn)Set(v dbArenaSeasonDataData ){
	th.m_row.m_lock.UnSafeLock("dbArenaSeasonDataColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbArenaSeasonDataData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbArenaSeasonDataColumn)GetLastDayResetTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbArenaSeasonDataColumn.GetLastDayResetTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastDayResetTime
	return
}
func (th *dbArenaSeasonDataColumn)SetLastDayResetTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbArenaSeasonDataColumn.SetLastDayResetTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastDayResetTime = v
	th.m_changed = true
	return
}
func (th *dbArenaSeasonDataColumn)GetLastSeasonResetTime( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbArenaSeasonDataColumn.GetLastSeasonResetTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.LastSeasonResetTime
	return
}
func (th *dbArenaSeasonDataColumn)SetLastSeasonResetTime(v int32){
	th.m_row.m_lock.UnSafeLock("dbArenaSeasonDataColumn.SetLastSeasonResetTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.LastSeasonResetTime = v
	th.m_changed = true
	return
}
type dbArenaSeasonRow struct {
	m_table *dbArenaSeasonTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_Id        int32
	Data dbArenaSeasonDataColumn
}
func new_dbArenaSeasonRow(table *dbArenaSeasonTable, Id int32) (r *dbArenaSeasonRow) {
	th := &dbArenaSeasonRow{}
	th.m_table = table
	th.m_Id = Id
	th.m_lock = NewRWMutex()
	th.Data.m_row=th
	th.Data.m_data=&dbArenaSeasonDataData{}
	return th
}
func (th *dbArenaSeasonRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbArenaSeasonRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(2)
		db_args.Push(th.m_Id)
		dData,db_err:=th.Data.save()
		if db_err!=nil{
			log.Error("insert save Data failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dData)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.Data.m_changed{
			update_string = "UPDATE ArenaSeason SET "
			db_args:=new_db_args(2)
			if th.Data.m_changed{
				update_string+="Data=?,"
				dData,err:=th.Data.save()
				if err!=nil{
					log.Error("update save Data failed")
					return err,false,0,"",nil
				}
				db_args.Push(dData)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE Id=?"
			db_args.Push(th.m_Id)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.Data.m_changed = false
	if release && th.m_loaded {
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbArenaSeasonRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT ArenaSeason exec failed %v ", th.m_Id)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE ArenaSeason exec failed %v", th.m_Id)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
type dbArenaSeasonTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_row *dbArenaSeasonRow
	m_preload_select_stmt *sql.Stmt
	m_save_insert_stmt *sql.Stmt
}
func new_dbArenaSeasonTable(dbc *DBC) (th *dbArenaSeasonTable) {
	th = &dbArenaSeasonTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	return th
}
func (th *dbArenaSeasonTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS ArenaSeason(Id int(11),PRIMARY KEY (Id))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS ArenaSeason failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='ArenaSeason'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasData := columns["Data"]
	if !hasData {
		_, err = th.m_dbc.Exec("ALTER TABLE ArenaSeason ADD COLUMN Data LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Data failed")
			return
		}
	}
	return
}
func (th *dbArenaSeasonTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT Data FROM ArenaSeason WHERE Id=0")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbArenaSeasonTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO ArenaSeason (Id,Data) VALUES (?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbArenaSeasonTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbArenaSeasonTable) Preload() (err error) {
	r := th.m_dbc.StmtQueryRow(th.m_preload_select_stmt)
	var dData []byte
	err = r.Scan(&dData)
	if err!=nil{
		if err!=sql.ErrNoRows{
			log.Error("Scan failed")
			return
		}
	}else{
		row := new_dbArenaSeasonRow(th,0)
		err = row.Data.load(dData)
		if err != nil {
			log.Error("Data ")
			return
		}
		row.m_valid = true
		row.m_loaded=true
		th.m_row=row
	}
	if th.m_row == nil {
		th.m_row = new_dbArenaSeasonRow(th, 0)
		th.m_row.m_new = true
		th.m_row.m_valid = true
		err = th.Save(false)
		if err != nil {
			log.Error("save failed")
			return
		}
		th.m_row.m_loaded = true
	}
	return
}
func (th *dbArenaSeasonTable) Save(quick bool) (err error) {
	if th.m_row==nil{
		return errors.New("row nil")
	}
	err, _, _ = th.m_row.Save(false)
	return err
}
func (th *dbArenaSeasonTable) GetRow( ) (row *dbArenaSeasonRow) {
	return th.m_row
}
func (th *dbGuildRow)GetName( )(r string ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildNameColumn")
	defer th.m_lock.UnSafeRUnlock()
	return string(th.m_Name)
}
func (th *dbGuildRow)SetName(v string){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildNameColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Name=string(v)
	th.m_Name_changed=true
	return
}
func (th *dbGuildRow)GetCreater( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildCreaterColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_Creater)
}
func (th *dbGuildRow)SetCreater(v int32){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildCreaterColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Creater=int32(v)
	th.m_Creater_changed=true
	return
}
func (th *dbGuildRow)GetCreateTime( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildCreateTimeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_CreateTime)
}
func (th *dbGuildRow)SetCreateTime(v int32){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildCreateTimeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_CreateTime=int32(v)
	th.m_CreateTime_changed=true
	return
}
func (th *dbGuildRow)GetDismissTime( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildDismissTimeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_DismissTime)
}
func (th *dbGuildRow)SetDismissTime(v int32){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildDismissTimeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_DismissTime=int32(v)
	th.m_DismissTime_changed=true
	return
}
func (th *dbGuildRow)GetLogo( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildLogoColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_Logo)
}
func (th *dbGuildRow)SetLogo(v int32){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildLogoColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Logo=int32(v)
	th.m_Logo_changed=true
	return
}
func (th *dbGuildRow)GetLevel( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildLevelColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_Level)
}
func (th *dbGuildRow)SetLevel(v int32){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildLevelColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Level=int32(v)
	th.m_Level_changed=true
	return
}
func (th *dbGuildRow)GetExp( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildExpColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_Exp)
}
func (th *dbGuildRow)SetExp(v int32){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildExpColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Exp=int32(v)
	th.m_Exp_changed=true
	return
}
func (th *dbGuildRow)GetExistType( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildExistTypeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_ExistType)
}
func (th *dbGuildRow)SetExistType(v int32){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildExistTypeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_ExistType=int32(v)
	th.m_ExistType_changed=true
	return
}
func (th *dbGuildRow)GetAnouncement( )(r string ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildAnouncementColumn")
	defer th.m_lock.UnSafeRUnlock()
	return string(th.m_Anouncement)
}
func (th *dbGuildRow)SetAnouncement(v string){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildAnouncementColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Anouncement=string(v)
	th.m_Anouncement_changed=true
	return
}
func (th *dbGuildRow)GetPresident( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildPresidentColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_President)
}
func (th *dbGuildRow)SetPresident(v int32){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildPresidentColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_President=int32(v)
	th.m_President_changed=true
	return
}
type dbGuildMemberColumn struct{
	m_row *dbGuildRow
	m_data map[int32]*dbGuildMemberData
	m_changed bool
}
func (th *dbGuildMemberColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.GuildMemberList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetId())
		return
	}
	for _, v := range pb.List {
		d := &dbGuildMemberData{}
		d.from_pb(v)
		th.m_data[int32(d.PlayerId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbGuildMemberColumn)save( )(data []byte,err error){
	pb := &db.GuildMemberList{}
	pb.List=make([]*db.GuildMember,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbGuildMemberColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildMemberColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbGuildMemberColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbGuildMemberColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbGuildMemberColumn)GetAll()(list []dbGuildMemberData){
	th.m_row.m_lock.UnSafeRLock("dbGuildMemberColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbGuildMemberData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbGuildMemberColumn)Get(id int32)(v *dbGuildMemberData){
	th.m_row.m_lock.UnSafeRLock("dbGuildMemberColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbGuildMemberData{}
	d.clone_to(v)
	return
}
func (th *dbGuildMemberColumn)Set(v dbGuildMemberData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildMemberColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.PlayerId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), v.PlayerId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbGuildMemberColumn)Add(v *dbGuildMemberData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbGuildMemberColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.PlayerId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetId(), v.PlayerId)
		return false
	}
	d:=&dbGuildMemberData{}
	v.clone_to(d)
	th.m_data[int32(v.PlayerId)]=d
	th.m_changed = true
	return true
}
func (th *dbGuildMemberColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbGuildMemberColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbGuildMemberColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbGuildMemberColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbGuildMemberData)
	th.m_changed = true
	return
}
func (th *dbGuildMemberColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbGuildMemberColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
type dbGuildAskListColumn struct{
	m_row *dbGuildRow
	m_data map[int32]*dbGuildAskListData
	m_changed bool
}
func (th *dbGuildAskListColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.GuildAskListList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetId())
		return
	}
	for _, v := range pb.List {
		d := &dbGuildAskListData{}
		d.from_pb(v)
		th.m_data[int32(d.PlayerId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbGuildAskListColumn)save( )(data []byte,err error){
	pb := &db.GuildAskListList{}
	pb.List=make([]*db.GuildAskList,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbGuildAskListColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskListColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbGuildAskListColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskListColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbGuildAskListColumn)GetAll()(list []dbGuildAskListData){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskListColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbGuildAskListData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbGuildAskListColumn)Get(id int32)(v *dbGuildAskListData){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskListColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbGuildAskListData{}
	d.clone_to(v)
	return
}
func (th *dbGuildAskListColumn)Set(v dbGuildAskListData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildAskListColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.PlayerId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), v.PlayerId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbGuildAskListColumn)Add(v *dbGuildAskListData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbGuildAskListColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.PlayerId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetId(), v.PlayerId)
		return false
	}
	d:=&dbGuildAskListData{}
	v.clone_to(d)
	th.m_data[int32(v.PlayerId)]=d
	th.m_changed = true
	return true
}
func (th *dbGuildAskListColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbGuildAskListColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbGuildAskListColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbGuildAskListColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbGuildAskListData)
	th.m_changed = true
	return
}
func (th *dbGuildAskListColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskListColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbGuildRow)GetLastDonateRefreshTime( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildLastDonateRefreshTimeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_LastDonateRefreshTime)
}
func (th *dbGuildRow)SetLastDonateRefreshTime(v int32){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildLastDonateRefreshTimeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_LastDonateRefreshTime=int32(v)
	th.m_LastDonateRefreshTime_changed=true
	return
}
type dbGuildLogColumn struct{
	m_row *dbGuildRow
	m_data map[int32]*dbGuildLogData
	m_max_id int32
	m_changed bool
}
func (th *dbGuildLogColumn)load(max_id int32, data []byte)(err error){
	th.m_max_id=max_id
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.GuildLogList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetId())
		return
	}
	for _, v := range pb.List {
		d := &dbGuildLogData{}
		d.from_pb(v)
		th.m_data[int32(d.Id)] = d
	}
	th.m_changed = false
	return
}
func (th *dbGuildLogColumn)save( )(max_id int32,data []byte,err error){
	max_id=th.m_max_id

	pb := &db.GuildLogList{}
	pb.List=make([]*db.GuildLog,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbGuildLogColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildLogColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbGuildLogColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbGuildLogColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbGuildLogColumn)GetAll()(list []dbGuildLogData){
	th.m_row.m_lock.UnSafeRLock("dbGuildLogColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbGuildLogData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbGuildLogColumn)Get(id int32)(v *dbGuildLogData){
	th.m_row.m_lock.UnSafeRLock("dbGuildLogColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbGuildLogData{}
	d.clone_to(v)
	return
}
func (th *dbGuildLogColumn)Set(v dbGuildLogData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildLogColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.Id)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), v.Id)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbGuildLogColumn)Add(v *dbGuildLogData)(id int32){
	th.m_row.m_lock.UnSafeLock("dbGuildLogColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_max_id++
	id=th.m_max_id
	v.Id=id
	d:=&dbGuildLogData{}
	v.clone_to(d)
	th.m_data[v.Id]=d
	th.m_changed = true
	return
}
func (th *dbGuildLogColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbGuildLogColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbGuildLogColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbGuildLogColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbGuildLogData)
	th.m_changed = true
	return
}
func (th *dbGuildLogColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbGuildLogColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbGuildLogColumn)GetLogType(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildLogColumn.GetLogType")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.LogType
	return v,true
}
func (th *dbGuildLogColumn)SetLogType(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildLogColumn.SetLogType")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), id)
		return
	}
	d.LogType = v
	th.m_changed = true
	return true
}
func (th *dbGuildLogColumn)GetPlayerId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildLogColumn.GetPlayerId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.PlayerId
	return v,true
}
func (th *dbGuildLogColumn)SetPlayerId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildLogColumn.SetPlayerId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), id)
		return
	}
	d.PlayerId = v
	th.m_changed = true
	return true
}
func (th *dbGuildLogColumn)GetTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildLogColumn.GetTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Time
	return v,true
}
func (th *dbGuildLogColumn)SetTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildLogColumn.SetTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), id)
		return
	}
	d.Time = v
	th.m_changed = true
	return true
}
func (th *dbGuildRow)GetLastRecruitTime( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildLastRecruitTimeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_LastRecruitTime)
}
func (th *dbGuildRow)SetLastRecruitTime(v int32){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildLastRecruitTimeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_LastRecruitTime=int32(v)
	th.m_LastRecruitTime_changed=true
	return
}
type dbGuildAskDonateColumn struct{
	m_row *dbGuildRow
	m_data map[int32]*dbGuildAskDonateData
	m_changed bool
}
func (th *dbGuildAskDonateColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.GuildAskDonateList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetId())
		return
	}
	for _, v := range pb.List {
		d := &dbGuildAskDonateData{}
		d.from_pb(v)
		th.m_data[int32(d.PlayerId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbGuildAskDonateColumn)save( )(data []byte,err error){
	pb := &db.GuildAskDonateList{}
	pb.List=make([]*db.GuildAskDonate,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbGuildAskDonateColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskDonateColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbGuildAskDonateColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskDonateColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbGuildAskDonateColumn)GetAll()(list []dbGuildAskDonateData){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskDonateColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbGuildAskDonateData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbGuildAskDonateColumn)Get(id int32)(v *dbGuildAskDonateData){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskDonateColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbGuildAskDonateData{}
	d.clone_to(v)
	return
}
func (th *dbGuildAskDonateColumn)Set(v dbGuildAskDonateData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildAskDonateColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.PlayerId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), v.PlayerId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbGuildAskDonateColumn)Add(v *dbGuildAskDonateData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbGuildAskDonateColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.PlayerId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetId(), v.PlayerId)
		return false
	}
	d:=&dbGuildAskDonateData{}
	v.clone_to(d)
	th.m_data[int32(v.PlayerId)]=d
	th.m_changed = true
	return true
}
func (th *dbGuildAskDonateColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbGuildAskDonateColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbGuildAskDonateColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbGuildAskDonateColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbGuildAskDonateData)
	th.m_changed = true
	return
}
func (th *dbGuildAskDonateColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskDonateColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbGuildAskDonateColumn)GetItemId(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskDonateColumn.GetItemId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.ItemId
	return v,true
}
func (th *dbGuildAskDonateColumn)SetItemId(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildAskDonateColumn.SetItemId")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), id)
		return
	}
	d.ItemId = v
	th.m_changed = true
	return true
}
func (th *dbGuildAskDonateColumn)GetItemNum(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskDonateColumn.GetItemNum")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.ItemNum
	return v,true
}
func (th *dbGuildAskDonateColumn)SetItemNum(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildAskDonateColumn.SetItemNum")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), id)
		return
	}
	d.ItemNum = v
	th.m_changed = true
	return true
}
func (th *dbGuildAskDonateColumn)GetAskTime(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildAskDonateColumn.GetAskTime")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.AskTime
	return v,true
}
func (th *dbGuildAskDonateColumn)SetAskTime(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildAskDonateColumn.SetAskTime")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), id)
		return
	}
	d.AskTime = v
	th.m_changed = true
	return true
}
type dbGuildStageColumn struct{
	m_row *dbGuildRow
	m_data *dbGuildStageData
	m_changed bool
}
func (th *dbGuildStageColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbGuildStageData{}
		th.m_changed = false
		return nil
	}
	pb := &db.GuildStage{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetId())
		return
	}
	th.m_data = &dbGuildStageData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbGuildStageColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbGuildStageColumn)Get( )(v *dbGuildStageData ){
	th.m_row.m_lock.UnSafeRLock("dbGuildStageColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbGuildStageData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbGuildStageColumn)Set(v dbGuildStageData ){
	th.m_row.m_lock.UnSafeLock("dbGuildStageColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbGuildStageData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbGuildStageColumn)GetBossId( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbGuildStageColumn.GetBossId")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.BossId
	return
}
func (th *dbGuildStageColumn)SetBossId(v int32){
	th.m_row.m_lock.UnSafeLock("dbGuildStageColumn.SetBossId")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.BossId = v
	th.m_changed = true
	return
}
func (th *dbGuildStageColumn)GetHpPercent( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbGuildStageColumn.GetHpPercent")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.HpPercent
	return
}
func (th *dbGuildStageColumn)SetHpPercent(v int32){
	th.m_row.m_lock.UnSafeLock("dbGuildStageColumn.SetHpPercent")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.HpPercent = v
	th.m_changed = true
	return
}
func (th *dbGuildStageColumn)GetBossPos( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbGuildStageColumn.GetBossPos")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.BossPos
	return
}
func (th *dbGuildStageColumn)SetBossPos(v int32){
	th.m_row.m_lock.UnSafeLock("dbGuildStageColumn.SetBossPos")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.BossPos = v
	th.m_changed = true
	return
}
func (th *dbGuildStageColumn)GetBossHP( )(v int32 ){
	th.m_row.m_lock.UnSafeRLock("dbGuildStageColumn.GetBossHP")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = th.m_data.BossHP
	return
}
func (th *dbGuildStageColumn)SetBossHP(v int32){
	th.m_row.m_lock.UnSafeLock("dbGuildStageColumn.SetBossHP")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.BossHP = v
	th.m_changed = true
	return
}
func (th *dbGuildRow)GetLastStageResetTime( )(r int32 ){
	th.m_lock.UnSafeRLock("dbGuildRow.GetdbGuildLastStageResetTimeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_LastStageResetTime)
}
func (th *dbGuildRow)SetLastStageResetTime(v int32){
	th.m_lock.UnSafeLock("dbGuildRow.SetdbGuildLastStageResetTimeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_LastStageResetTime=int32(v)
	th.m_LastStageResetTime_changed=true
	return
}
type dbGuildRow struct {
	m_table *dbGuildTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_Id        int32
	m_Name_changed bool
	m_Name string
	m_Creater_changed bool
	m_Creater int32
	m_CreateTime_changed bool
	m_CreateTime int32
	m_DismissTime_changed bool
	m_DismissTime int32
	m_Logo_changed bool
	m_Logo int32
	m_Level_changed bool
	m_Level int32
	m_Exp_changed bool
	m_Exp int32
	m_ExistType_changed bool
	m_ExistType int32
	m_Anouncement_changed bool
	m_Anouncement string
	m_President_changed bool
	m_President int32
	Members dbGuildMemberColumn
	AskLists dbGuildAskListColumn
	m_LastDonateRefreshTime_changed bool
	m_LastDonateRefreshTime int32
	Logs dbGuildLogColumn
	m_LastRecruitTime_changed bool
	m_LastRecruitTime int32
	AskDonates dbGuildAskDonateColumn
	Stage dbGuildStageColumn
	m_LastStageResetTime_changed bool
	m_LastStageResetTime int32
}
func new_dbGuildRow(table *dbGuildTable, Id int32) (r *dbGuildRow) {
	th := &dbGuildRow{}
	th.m_table = table
	th.m_Id = Id
	th.m_lock = NewRWMutex()
	th.m_Name_changed=true
	th.m_Creater_changed=true
	th.m_CreateTime_changed=true
	th.m_DismissTime_changed=true
	th.m_Logo_changed=true
	th.m_Level_changed=true
	th.m_Exp_changed=true
	th.m_ExistType_changed=true
	th.m_Anouncement_changed=true
	th.m_President_changed=true
	th.m_LastDonateRefreshTime_changed=true
	th.m_LastRecruitTime_changed=true
	th.m_LastStageResetTime_changed=true
	th.Members.m_row=th
	th.Members.m_data=make(map[int32]*dbGuildMemberData)
	th.AskLists.m_row=th
	th.AskLists.m_data=make(map[int32]*dbGuildAskListData)
	th.Logs.m_row=th
	th.Logs.m_data=make(map[int32]*dbGuildLogData)
	th.AskDonates.m_row=th
	th.AskDonates.m_data=make(map[int32]*dbGuildAskDonateData)
	th.Stage.m_row=th
	th.Stage.m_data=&dbGuildStageData{}
	return th
}
func (th *dbGuildRow) GetId() (r int32) {
	return th.m_Id
}
func (th *dbGuildRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbGuildRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(20)
		db_args.Push(th.m_Id)
		db_args.Push(th.m_Name)
		db_args.Push(th.m_Creater)
		db_args.Push(th.m_CreateTime)
		db_args.Push(th.m_DismissTime)
		db_args.Push(th.m_Logo)
		db_args.Push(th.m_Level)
		db_args.Push(th.m_Exp)
		db_args.Push(th.m_ExistType)
		db_args.Push(th.m_Anouncement)
		db_args.Push(th.m_President)
		dMembers,db_err:=th.Members.save()
		if db_err!=nil{
			log.Error("insert save Member failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dMembers)
		dAskLists,db_err:=th.AskLists.save()
		if db_err!=nil{
			log.Error("insert save AskList failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dAskLists)
		db_args.Push(th.m_LastDonateRefreshTime)
		dMaxLogId,dLogs,db_err:=th.Logs.save()
		if db_err!=nil{
			log.Error("insert save Log failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dMaxLogId)
		db_args.Push(dLogs)
		db_args.Push(th.m_LastRecruitTime)
		dAskDonates,db_err:=th.AskDonates.save()
		if db_err!=nil{
			log.Error("insert save AskDonate failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dAskDonates)
		dStage,db_err:=th.Stage.save()
		if db_err!=nil{
			log.Error("insert save Stage failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dStage)
		db_args.Push(th.m_LastStageResetTime)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.m_Name_changed||th.m_Creater_changed||th.m_CreateTime_changed||th.m_DismissTime_changed||th.m_Logo_changed||th.m_Level_changed||th.m_Exp_changed||th.m_ExistType_changed||th.m_Anouncement_changed||th.m_President_changed||th.Members.m_changed||th.AskLists.m_changed||th.m_LastDonateRefreshTime_changed||th.Logs.m_changed||th.m_LastRecruitTime_changed||th.AskDonates.m_changed||th.Stage.m_changed||th.m_LastStageResetTime_changed{
			update_string = "UPDATE Guilds SET "
			db_args:=new_db_args(20)
			if th.m_Name_changed{
				update_string+="Name=?,"
				db_args.Push(th.m_Name)
			}
			if th.m_Creater_changed{
				update_string+="Creater=?,"
				db_args.Push(th.m_Creater)
			}
			if th.m_CreateTime_changed{
				update_string+="CreateTime=?,"
				db_args.Push(th.m_CreateTime)
			}
			if th.m_DismissTime_changed{
				update_string+="DismissTime=?,"
				db_args.Push(th.m_DismissTime)
			}
			if th.m_Logo_changed{
				update_string+="Logo=?,"
				db_args.Push(th.m_Logo)
			}
			if th.m_Level_changed{
				update_string+="Level=?,"
				db_args.Push(th.m_Level)
			}
			if th.m_Exp_changed{
				update_string+="Exp=?,"
				db_args.Push(th.m_Exp)
			}
			if th.m_ExistType_changed{
				update_string+="ExistType=?,"
				db_args.Push(th.m_ExistType)
			}
			if th.m_Anouncement_changed{
				update_string+="Anouncement=?,"
				db_args.Push(th.m_Anouncement)
			}
			if th.m_President_changed{
				update_string+="President=?,"
				db_args.Push(th.m_President)
			}
			if th.Members.m_changed{
				update_string+="Members=?,"
				dMembers,err:=th.Members.save()
				if err!=nil{
					log.Error("insert save Member failed")
					return err,false,0,"",nil
				}
				db_args.Push(dMembers)
			}
			if th.AskLists.m_changed{
				update_string+="AskLists=?,"
				dAskLists,err:=th.AskLists.save()
				if err!=nil{
					log.Error("insert save AskList failed")
					return err,false,0,"",nil
				}
				db_args.Push(dAskLists)
			}
			if th.m_LastDonateRefreshTime_changed{
				update_string+="LastDonateRefreshTime=?,"
				db_args.Push(th.m_LastDonateRefreshTime)
			}
			if th.Logs.m_changed{
				update_string+="MaxLogId=?,"
				update_string+="Logs=?,"
				dMaxLogId,dLogs,err:=th.Logs.save()
				if err!=nil{
					log.Error("insert save Log failed")
					return err,false,0,"",nil
				}
				db_args.Push(dMaxLogId)
				db_args.Push(dLogs)
			}
			if th.m_LastRecruitTime_changed{
				update_string+="LastRecruitTime=?,"
				db_args.Push(th.m_LastRecruitTime)
			}
			if th.AskDonates.m_changed{
				update_string+="AskDonates=?,"
				dAskDonates,err:=th.AskDonates.save()
				if err!=nil{
					log.Error("insert save AskDonate failed")
					return err,false,0,"",nil
				}
				db_args.Push(dAskDonates)
			}
			if th.Stage.m_changed{
				update_string+="Stage=?,"
				dStage,err:=th.Stage.save()
				if err!=nil{
					log.Error("update save Stage failed")
					return err,false,0,"",nil
				}
				db_args.Push(dStage)
			}
			if th.m_LastStageResetTime_changed{
				update_string+="LastStageResetTime=?,"
				db_args.Push(th.m_LastStageResetTime)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE Id=?"
			db_args.Push(th.m_Id)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.m_Name_changed = false
	th.m_Creater_changed = false
	th.m_CreateTime_changed = false
	th.m_DismissTime_changed = false
	th.m_Logo_changed = false
	th.m_Level_changed = false
	th.m_Exp_changed = false
	th.m_ExistType_changed = false
	th.m_Anouncement_changed = false
	th.m_President_changed = false
	th.Members.m_changed = false
	th.AskLists.m_changed = false
	th.m_LastDonateRefreshTime_changed = false
	th.Logs.m_changed = false
	th.m_LastRecruitTime_changed = false
	th.AskDonates.m_changed = false
	th.Stage.m_changed = false
	th.m_LastStageResetTime_changed = false
	if release && th.m_loaded {
		atomic.AddInt32(&th.m_table.m_gc_n, -1)
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbGuildRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT Guilds exec failed %v ", th.m_Id)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE Guilds exec failed %v", th.m_Id)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
func (th *dbGuildRow) Touch(releasable bool) {
	th.m_touch = int32(time.Now().Unix())
	th.m_releasable = releasable
}
type dbGuildRowSort struct {
	rows []*dbGuildRow
}
func (th *dbGuildRowSort) Len() (length int) {
	return len(th.rows)
}
func (th *dbGuildRowSort) Less(i int, j int) (less bool) {
	return th.rows[i].m_touch < th.rows[j].m_touch
}
func (th *dbGuildRowSort) Swap(i int, j int) {
	temp := th.rows[i]
	th.rows[i] = th.rows[j]
	th.rows[j] = temp
}
type dbGuildTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_rows map[int32]*dbGuildRow
	m_new_rows map[int32]*dbGuildRow
	m_removed_rows map[int32]*dbGuildRow
	m_gc_n int32
	m_gcing int32
	m_pool_size int32
	m_preload_select_stmt *sql.Stmt
	m_preload_max_id int32
	m_save_insert_stmt *sql.Stmt
	m_delete_stmt *sql.Stmt
}
func new_dbGuildTable(dbc *DBC) (th *dbGuildTable) {
	th = &dbGuildTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	th.m_rows = make(map[int32]*dbGuildRow)
	th.m_new_rows = make(map[int32]*dbGuildRow)
	th.m_removed_rows = make(map[int32]*dbGuildRow)
	return th
}
func (th *dbGuildTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS Guilds(Id int(11),PRIMARY KEY (Id))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS Guilds failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='Guilds'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasName := columns["Name"]
	if !hasName {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN Name varchar(256) DEFAULT ''")
		if err != nil {
			log.Error("ADD COLUMN Name failed")
			return
		}
	}
	_, hasCreater := columns["Creater"]
	if !hasCreater {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN Creater int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN Creater failed")
			return
		}
	}
	_, hasCreateTime := columns["CreateTime"]
	if !hasCreateTime {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN CreateTime int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN CreateTime failed")
			return
		}
	}
	_, hasDismissTime := columns["DismissTime"]
	if !hasDismissTime {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN DismissTime int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN DismissTime failed")
			return
		}
	}
	_, hasLogo := columns["Logo"]
	if !hasLogo {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN Logo int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN Logo failed")
			return
		}
	}
	_, hasLevel := columns["Level"]
	if !hasLevel {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN Level int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN Level failed")
			return
		}
	}
	_, hasExp := columns["Exp"]
	if !hasExp {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN Exp int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN Exp failed")
			return
		}
	}
	_, hasExistType := columns["ExistType"]
	if !hasExistType {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN ExistType int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN ExistType failed")
			return
		}
	}
	_, hasAnouncement := columns["Anouncement"]
	if !hasAnouncement {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN Anouncement varchar(256) DEFAULT ''")
		if err != nil {
			log.Error("ADD COLUMN Anouncement failed")
			return
		}
	}
	_, hasPresident := columns["President"]
	if !hasPresident {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN President int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN President failed")
			return
		}
	}
	_, hasMember := columns["Members"]
	if !hasMember {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN Members LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Members failed")
			return
		}
	}
	_, hasAskList := columns["AskLists"]
	if !hasAskList {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN AskLists LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN AskLists failed")
			return
		}
	}
	_, hasLastDonateRefreshTime := columns["LastDonateRefreshTime"]
	if !hasLastDonateRefreshTime {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN LastDonateRefreshTime int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN LastDonateRefreshTime failed")
			return
		}
	}
	_, hasMaxLog := columns["MaxLogId"]
	if !hasMaxLog {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN MaxLogId int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN map index MaxLogId failed")
			return
		}
	}
	_, hasLog := columns["Logs"]
	if !hasLog {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN Logs LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Logs failed")
			return
		}
	}
	_, hasLastRecruitTime := columns["LastRecruitTime"]
	if !hasLastRecruitTime {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN LastRecruitTime int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN LastRecruitTime failed")
			return
		}
	}
	_, hasAskDonate := columns["AskDonates"]
	if !hasAskDonate {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN AskDonates LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN AskDonates failed")
			return
		}
	}
	_, hasStage := columns["Stage"]
	if !hasStage {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN Stage LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN Stage failed")
			return
		}
	}
	_, hasLastStageResetTime := columns["LastStageResetTime"]
	if !hasLastStageResetTime {
		_, err = th.m_dbc.Exec("ALTER TABLE Guilds ADD COLUMN LastStageResetTime int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN LastStageResetTime failed")
			return
		}
	}
	return
}
func (th *dbGuildTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT Id,Name,Creater,CreateTime,DismissTime,Logo,Level,Exp,ExistType,Anouncement,President,Members,AskLists,LastDonateRefreshTime,MaxLogId,Logs,LastRecruitTime,AskDonates,Stage,LastStageResetTime FROM Guilds")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbGuildTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO Guilds (Id,Name,Creater,CreateTime,DismissTime,Logo,Level,Exp,ExistType,Anouncement,President,Members,AskLists,LastDonateRefreshTime,MaxLogId,Logs,LastRecruitTime,AskDonates,Stage,LastStageResetTime) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbGuildTable) prepare_delete_stmt() (err error) {
	th.m_delete_stmt,err=th.m_dbc.StmtPrepare("DELETE FROM Guilds WHERE Id=?")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbGuildTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	err=th.prepare_delete_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbGuildTable) Preload() (err error) {
	r, err := th.m_dbc.StmtQuery(th.m_preload_select_stmt)
	if err != nil {
		log.Error("SELECT")
		return
	}
	var Id int32
	var dName string
	var dCreater int32
	var dCreateTime int32
	var dDismissTime int32
	var dLogo int32
	var dLevel int32
	var dExp int32
	var dExistType int32
	var dAnouncement string
	var dPresident int32
	var dMembers []byte
	var dAskLists []byte
	var dLastDonateRefreshTime int32
	var dMaxLogId int32
	var dLogs []byte
	var dLastRecruitTime int32
	var dAskDonates []byte
	var dStage []byte
	var dLastStageResetTime int32
		th.m_preload_max_id = 0
	for r.Next() {
		err = r.Scan(&Id,&dName,&dCreater,&dCreateTime,&dDismissTime,&dLogo,&dLevel,&dExp,&dExistType,&dAnouncement,&dPresident,&dMembers,&dAskLists,&dLastDonateRefreshTime,&dMaxLogId,&dLogs,&dLastRecruitTime,&dAskDonates,&dStage,&dLastStageResetTime)
		if err != nil {
			log.Error("Scan err[%v]", err.Error())
			return
		}
		if Id>th.m_preload_max_id{
			th.m_preload_max_id =Id
		}
		row := new_dbGuildRow(th,Id)
		row.m_Name=dName
		row.m_Creater=dCreater
		row.m_CreateTime=dCreateTime
		row.m_DismissTime=dDismissTime
		row.m_Logo=dLogo
		row.m_Level=dLevel
		row.m_Exp=dExp
		row.m_ExistType=dExistType
		row.m_Anouncement=dAnouncement
		row.m_President=dPresident
		err = row.Members.load(dMembers)
		if err != nil {
			log.Error("Members %v", Id)
			return
		}
		err = row.AskLists.load(dAskLists)
		if err != nil {
			log.Error("AskLists %v", Id)
			return
		}
		row.m_LastDonateRefreshTime=dLastDonateRefreshTime
		err = row.Logs.load(dMaxLogId,dLogs)
		if err != nil {
			log.Error("Logs %v", Id)
			return
		}
		row.m_LastRecruitTime=dLastRecruitTime
		err = row.AskDonates.load(dAskDonates)
		if err != nil {
			log.Error("AskDonates %v", Id)
			return
		}
		err = row.Stage.load(dStage)
		if err != nil {
			log.Error("Stage %v", Id)
			return
		}
		row.m_LastStageResetTime=dLastStageResetTime
		row.m_Name_changed=false
		row.m_Creater_changed=false
		row.m_CreateTime_changed=false
		row.m_DismissTime_changed=false
		row.m_Logo_changed=false
		row.m_Level_changed=false
		row.m_Exp_changed=false
		row.m_ExistType_changed=false
		row.m_Anouncement_changed=false
		row.m_President_changed=false
		row.m_LastDonateRefreshTime_changed=false
		row.m_LastRecruitTime_changed=false
		row.m_LastStageResetTime_changed=false
		row.m_valid = true
		th.m_rows[Id]=row
	}
	return
}
func (th *dbGuildTable) GetPreloadedMaxId() (max_id int32) {
	return th.m_preload_max_id
}
func (th *dbGuildTable) fetch_rows(rows map[int32]*dbGuildRow) (r map[int32]*dbGuildRow) {
	th.m_lock.UnSafeLock("dbGuildTable.fetch_rows")
	defer th.m_lock.UnSafeUnlock()
	r = make(map[int32]*dbGuildRow)
	for i, v := range rows {
		r[i] = v
	}
	return r
}
func (th *dbGuildTable) fetch_new_rows() (new_rows map[int32]*dbGuildRow) {
	th.m_lock.UnSafeLock("dbGuildTable.fetch_new_rows")
	defer th.m_lock.UnSafeUnlock()
	new_rows = make(map[int32]*dbGuildRow)
	for i, v := range th.m_new_rows {
		_, has := th.m_rows[i]
		if has {
			log.Error("rows already has new rows %v", i)
			continue
		}
		th.m_rows[i] = v
		new_rows[i] = v
	}
	for i, _ := range new_rows {
		delete(th.m_new_rows, i)
	}
	return
}
func (th *dbGuildTable) save_rows(rows map[int32]*dbGuildRow, quick bool) {
	for _, v := range rows {
		if th.m_dbc.m_quit && !quick {
			return
		}
		err, delay, _ := v.Save(false)
		if err != nil {
			log.Error("save failed %v", err)
		}
		if th.m_dbc.m_quit && !quick {
			return
		}
		if delay&&!quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
}
func (th *dbGuildTable) Save(quick bool) (err error){
	removed_rows := th.fetch_rows(th.m_removed_rows)
	for _, v := range removed_rows {
		_, err := th.m_dbc.StmtExec(th.m_delete_stmt, v.GetId())
		if err != nil {
			log.Error("exec delete stmt failed %v", err)
		}
		v.m_valid = false
		if !quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
	th.m_removed_rows = make(map[int32]*dbGuildRow)
	rows := th.fetch_rows(th.m_rows)
	th.save_rows(rows, quick)
	new_rows := th.fetch_new_rows()
	th.save_rows(new_rows, quick)
	return
}
func (th *dbGuildTable) AddRow(Id int32) (row *dbGuildRow) {
	th.m_lock.UnSafeLock("dbGuildTable.AddRow")
	defer th.m_lock.UnSafeUnlock()
	row = new_dbGuildRow(th,Id)
	row.m_new = true
	row.m_loaded = true
	row.m_valid = true
	_, has := th.m_new_rows[Id]
	if has{
		log.Error("已经存在 %v", Id)
		return nil
	}
	th.m_new_rows[Id] = row
	atomic.AddInt32(&th.m_gc_n,1)
	return row
}
func (th *dbGuildTable) RemoveRow(Id int32) {
	th.m_lock.UnSafeLock("dbGuildTable.RemoveRow")
	defer th.m_lock.UnSafeUnlock()
	row := th.m_rows[Id]
	if row != nil {
		row.m_remove = true
		delete(th.m_rows, Id)
		rm_row := th.m_removed_rows[Id]
		if rm_row != nil {
			log.Error("rows and removed rows both has %v", Id)
		}
		th.m_removed_rows[Id] = row
		_, has_new := th.m_new_rows[Id]
		if has_new {
			delete(th.m_new_rows, Id)
			log.Error("rows and new_rows both has %v", Id)
		}
	} else {
		row = th.m_removed_rows[Id]
		if row == nil {
			_, has_new := th.m_new_rows[Id]
			if has_new {
				delete(th.m_new_rows, Id)
			} else {
				log.Error("row not exist %v", Id)
			}
		} else {
			log.Error("already removed %v", Id)
			_, has_new := th.m_new_rows[Id]
			if has_new {
				delete(th.m_new_rows, Id)
				log.Error("removed rows and new_rows both has %v", Id)
			}
		}
	}
}
func (th *dbGuildTable) GetRow(Id int32) (row *dbGuildRow) {
	th.m_lock.UnSafeRLock("dbGuildTable.GetRow")
	defer th.m_lock.UnSafeRUnlock()
	row = th.m_rows[Id]
	if row == nil {
		row = th.m_new_rows[Id]
	}
	return row
}
type dbGuildStageDamageLogColumn struct{
	m_row *dbGuildStageRow
	m_data map[int32]*dbGuildStageDamageLogData
	m_changed bool
}
func (th *dbGuildStageDamageLogColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_changed = false
		return nil
	}
	pb := &db.GuildStageDamageLogList{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetId())
		return
	}
	for _, v := range pb.List {
		d := &dbGuildStageDamageLogData{}
		d.from_pb(v)
		th.m_data[int32(d.AttackerId)] = d
	}
	th.m_changed = false
	return
}
func (th *dbGuildStageDamageLogColumn)save( )(data []byte,err error){
	pb := &db.GuildStageDamageLogList{}
	pb.List=make([]*db.GuildStageDamageLog,len(th.m_data))
	i:=0
	for _, v := range th.m_data {
		pb.List[i] = v.to_pb()
		i++
	}
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbGuildStageDamageLogColumn)HasIndex(id int32)(has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildStageDamageLogColumn.HasIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	_, has = th.m_data[id]
	return
}
func (th *dbGuildStageDamageLogColumn)GetAllIndex()(list []int32){
	th.m_row.m_lock.UnSafeRLock("dbGuildStageDamageLogColumn.GetAllIndex")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]int32, len(th.m_data))
	i := 0
	for k, _ := range th.m_data {
		list[i] = k
		i++
	}
	return
}
func (th *dbGuildStageDamageLogColumn)GetAll()(list []dbGuildStageDamageLogData){
	th.m_row.m_lock.UnSafeRLock("dbGuildStageDamageLogColumn.GetAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	list = make([]dbGuildStageDamageLogData, len(th.m_data))
	i := 0
	for _, v := range th.m_data {
		v.clone_to(&list[i])
		i++
	}
	return
}
func (th *dbGuildStageDamageLogColumn)Get(id int32)(v *dbGuildStageDamageLogData){
	th.m_row.m_lock.UnSafeRLock("dbGuildStageDamageLogColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return nil
	}
	v=&dbGuildStageDamageLogData{}
	d.clone_to(v)
	return
}
func (th *dbGuildStageDamageLogColumn)Set(v dbGuildStageDamageLogData)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildStageDamageLogColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[int32(v.AttackerId)]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), v.AttackerId)
		return false
	}
	v.clone_to(d)
	th.m_changed = true
	return true
}
func (th *dbGuildStageDamageLogColumn)Add(v *dbGuildStageDamageLogData)(ok bool){
	th.m_row.m_lock.UnSafeLock("dbGuildStageDamageLogColumn.Add")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[int32(v.AttackerId)]
	if has {
		log.Error("already added %v %v",th.m_row.GetId(), v.AttackerId)
		return false
	}
	d:=&dbGuildStageDamageLogData{}
	v.clone_to(d)
	th.m_data[int32(v.AttackerId)]=d
	th.m_changed = true
	return true
}
func (th *dbGuildStageDamageLogColumn)Remove(id int32){
	th.m_row.m_lock.UnSafeLock("dbGuildStageDamageLogColumn.Remove")
	defer th.m_row.m_lock.UnSafeUnlock()
	_, has := th.m_data[id]
	if has {
		delete(th.m_data,id)
	}
	th.m_changed = true
	return
}
func (th *dbGuildStageDamageLogColumn)Clear(){
	th.m_row.m_lock.UnSafeLock("dbGuildStageDamageLogColumn.Clear")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=make(map[int32]*dbGuildStageDamageLogData)
	th.m_changed = true
	return
}
func (th *dbGuildStageDamageLogColumn)NumAll()(n int32){
	th.m_row.m_lock.UnSafeRLock("dbGuildStageDamageLogColumn.NumAll")
	defer th.m_row.m_lock.UnSafeRUnlock()
	return int32(len(th.m_data))
}
func (th *dbGuildStageDamageLogColumn)GetDamage(id int32)(v int32 ,has bool){
	th.m_row.m_lock.UnSafeRLock("dbGuildStageDamageLogColumn.GetDamage")
	defer th.m_row.m_lock.UnSafeRUnlock()
	d := th.m_data[id]
	if d==nil{
		return
	}
	v = d.Damage
	return v,true
}
func (th *dbGuildStageDamageLogColumn)SetDamage(id int32,v int32)(has bool){
	th.m_row.m_lock.UnSafeLock("dbGuildStageDamageLogColumn.SetDamage")
	defer th.m_row.m_lock.UnSafeUnlock()
	d := th.m_data[id]
	if d==nil{
		log.Error("not exist %v %v",th.m_row.GetId(), id)
		return
	}
	d.Damage = v
	th.m_changed = true
	return true
}
type dbGuildStageRow struct {
	m_table *dbGuildStageTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_Id        int64
	DamageLogs dbGuildStageDamageLogColumn
}
func new_dbGuildStageRow(table *dbGuildStageTable, Id int64) (r *dbGuildStageRow) {
	th := &dbGuildStageRow{}
	th.m_table = table
	th.m_Id = Id
	th.m_lock = NewRWMutex()
	th.DamageLogs.m_row=th
	th.DamageLogs.m_data=make(map[int32]*dbGuildStageDamageLogData)
	return th
}
func (th *dbGuildStageRow) GetId() (r int64) {
	return th.m_Id
}
func (th *dbGuildStageRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbGuildStageRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(2)
		db_args.Push(th.m_Id)
		dDamageLogs,db_err:=th.DamageLogs.save()
		if db_err!=nil{
			log.Error("insert save DamageLog failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dDamageLogs)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.DamageLogs.m_changed{
			update_string = "UPDATE GuildStages SET "
			db_args:=new_db_args(2)
			if th.DamageLogs.m_changed{
				update_string+="DamageLogs=?,"
				dDamageLogs,err:=th.DamageLogs.save()
				if err!=nil{
					log.Error("insert save DamageLog failed")
					return err,false,0,"",nil
				}
				db_args.Push(dDamageLogs)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE Id=?"
			db_args.Push(th.m_Id)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.DamageLogs.m_changed = false
	if release && th.m_loaded {
		atomic.AddInt32(&th.m_table.m_gc_n, -1)
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbGuildStageRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT GuildStages exec failed %v ", th.m_Id)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE GuildStages exec failed %v", th.m_Id)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
func (th *dbGuildStageRow) Touch(releasable bool) {
	th.m_touch = int32(time.Now().Unix())
	th.m_releasable = releasable
}
type dbGuildStageRowSort struct {
	rows []*dbGuildStageRow
}
func (th *dbGuildStageRowSort) Len() (length int) {
	return len(th.rows)
}
func (th *dbGuildStageRowSort) Less(i int, j int) (less bool) {
	return th.rows[i].m_touch < th.rows[j].m_touch
}
func (th *dbGuildStageRowSort) Swap(i int, j int) {
	temp := th.rows[i]
	th.rows[i] = th.rows[j]
	th.rows[j] = temp
}
type dbGuildStageTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_rows map[int64]*dbGuildStageRow
	m_new_rows map[int64]*dbGuildStageRow
	m_removed_rows map[int64]*dbGuildStageRow
	m_gc_n int32
	m_gcing int32
	m_pool_size int32
	m_preload_select_stmt *sql.Stmt
	m_preload_max_id int64
	m_save_insert_stmt *sql.Stmt
	m_delete_stmt *sql.Stmt
}
func new_dbGuildStageTable(dbc *DBC) (th *dbGuildStageTable) {
	th = &dbGuildStageTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	th.m_rows = make(map[int64]*dbGuildStageRow)
	th.m_new_rows = make(map[int64]*dbGuildStageRow)
	th.m_removed_rows = make(map[int64]*dbGuildStageRow)
	return th
}
func (th *dbGuildStageTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS GuildStages(Id bigint(20),PRIMARY KEY (Id))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS GuildStages failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='GuildStages'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasDamageLog := columns["DamageLogs"]
	if !hasDamageLog {
		_, err = th.m_dbc.Exec("ALTER TABLE GuildStages ADD COLUMN DamageLogs LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN DamageLogs failed")
			return
		}
	}
	return
}
func (th *dbGuildStageTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT Id,DamageLogs FROM GuildStages")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbGuildStageTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO GuildStages (Id,DamageLogs) VALUES (?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbGuildStageTable) prepare_delete_stmt() (err error) {
	th.m_delete_stmt,err=th.m_dbc.StmtPrepare("DELETE FROM GuildStages WHERE Id=?")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbGuildStageTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	err=th.prepare_delete_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbGuildStageTable) Preload() (err error) {
	r, err := th.m_dbc.StmtQuery(th.m_preload_select_stmt)
	if err != nil {
		log.Error("SELECT")
		return
	}
	var Id int64
	var dDamageLogs []byte
		th.m_preload_max_id = 0
	for r.Next() {
		err = r.Scan(&Id,&dDamageLogs)
		if err != nil {
			log.Error("Scan err[%v]", err.Error())
			return
		}
		if Id>th.m_preload_max_id{
			th.m_preload_max_id =Id
		}
		row := new_dbGuildStageRow(th,Id)
		err = row.DamageLogs.load(dDamageLogs)
		if err != nil {
			log.Error("DamageLogs %v", Id)
			return
		}
		row.m_valid = true
		th.m_rows[Id]=row
	}
	return
}
func (th *dbGuildStageTable) GetPreloadedMaxId() (max_id int64) {
	return th.m_preload_max_id
}
func (th *dbGuildStageTable) fetch_rows(rows map[int64]*dbGuildStageRow) (r map[int64]*dbGuildStageRow) {
	th.m_lock.UnSafeLock("dbGuildStageTable.fetch_rows")
	defer th.m_lock.UnSafeUnlock()
	r = make(map[int64]*dbGuildStageRow)
	for i, v := range rows {
		r[i] = v
	}
	return r
}
func (th *dbGuildStageTable) fetch_new_rows() (new_rows map[int64]*dbGuildStageRow) {
	th.m_lock.UnSafeLock("dbGuildStageTable.fetch_new_rows")
	defer th.m_lock.UnSafeUnlock()
	new_rows = make(map[int64]*dbGuildStageRow)
	for i, v := range th.m_new_rows {
		_, has := th.m_rows[i]
		if has {
			log.Error("rows already has new rows %v", i)
			continue
		}
		th.m_rows[i] = v
		new_rows[i] = v
	}
	for i, _ := range new_rows {
		delete(th.m_new_rows, i)
	}
	return
}
func (th *dbGuildStageTable) save_rows(rows map[int64]*dbGuildStageRow, quick bool) {
	for _, v := range rows {
		if th.m_dbc.m_quit && !quick {
			return
		}
		err, delay, _ := v.Save(false)
		if err != nil {
			log.Error("save failed %v", err)
		}
		if th.m_dbc.m_quit && !quick {
			return
		}
		if delay&&!quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
}
func (th *dbGuildStageTable) Save(quick bool) (err error){
	removed_rows := th.fetch_rows(th.m_removed_rows)
	for _, v := range removed_rows {
		_, err := th.m_dbc.StmtExec(th.m_delete_stmt, v.GetId())
		if err != nil {
			log.Error("exec delete stmt failed %v", err)
		}
		v.m_valid = false
		if !quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
	th.m_removed_rows = make(map[int64]*dbGuildStageRow)
	rows := th.fetch_rows(th.m_rows)
	th.save_rows(rows, quick)
	new_rows := th.fetch_new_rows()
	th.save_rows(new_rows, quick)
	return
}
func (th *dbGuildStageTable) AddRow(Id int64) (row *dbGuildStageRow) {
	th.m_lock.UnSafeLock("dbGuildStageTable.AddRow")
	defer th.m_lock.UnSafeUnlock()
	row = new_dbGuildStageRow(th,Id)
	row.m_new = true
	row.m_loaded = true
	row.m_valid = true
	_, has := th.m_new_rows[Id]
	if has{
		log.Error("已经存在 %v", Id)
		return nil
	}
	th.m_new_rows[Id] = row
	atomic.AddInt32(&th.m_gc_n,1)
	return row
}
func (th *dbGuildStageTable) RemoveRow(Id int64) {
	th.m_lock.UnSafeLock("dbGuildStageTable.RemoveRow")
	defer th.m_lock.UnSafeUnlock()
	row := th.m_rows[Id]
	if row != nil {
		row.m_remove = true
		delete(th.m_rows, Id)
		rm_row := th.m_removed_rows[Id]
		if rm_row != nil {
			log.Error("rows and removed rows both has %v", Id)
		}
		th.m_removed_rows[Id] = row
		_, has_new := th.m_new_rows[Id]
		if has_new {
			delete(th.m_new_rows, Id)
			log.Error("rows and new_rows both has %v", Id)
		}
	} else {
		row = th.m_removed_rows[Id]
		if row == nil {
			_, has_new := th.m_new_rows[Id]
			if has_new {
				delete(th.m_new_rows, Id)
			} else {
				log.Error("row not exist %v", Id)
			}
		} else {
			log.Error("already removed %v", Id)
			_, has_new := th.m_new_rows[Id]
			if has_new {
				delete(th.m_new_rows, Id)
				log.Error("removed rows and new_rows both has %v", Id)
			}
		}
	}
}
func (th *dbGuildStageTable) GetRow(Id int64) (row *dbGuildStageRow) {
	th.m_lock.UnSafeRLock("dbGuildStageTable.GetRow")
	defer th.m_lock.UnSafeRUnlock()
	row = th.m_rows[Id]
	if row == nil {
		row = th.m_new_rows[Id]
	}
	return row
}
func (th *dbActivitysToDeleteRow)GetStartTime( )(r int32 ){
	th.m_lock.UnSafeRLock("dbActivitysToDeleteRow.GetdbActivitysToDeleteStartTimeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_StartTime)
}
func (th *dbActivitysToDeleteRow)SetStartTime(v int32){
	th.m_lock.UnSafeLock("dbActivitysToDeleteRow.SetdbActivitysToDeleteStartTimeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_StartTime=int32(v)
	th.m_StartTime_changed=true
	return
}
func (th *dbActivitysToDeleteRow)GetEndTime( )(r int32 ){
	th.m_lock.UnSafeRLock("dbActivitysToDeleteRow.GetdbActivitysToDeleteEndTimeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_EndTime)
}
func (th *dbActivitysToDeleteRow)SetEndTime(v int32){
	th.m_lock.UnSafeLock("dbActivitysToDeleteRow.SetdbActivitysToDeleteEndTimeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_EndTime=int32(v)
	th.m_EndTime_changed=true
	return
}
type dbActivitysToDeleteRow struct {
	m_table *dbActivitysToDeleteTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_Id        int32
	m_StartTime_changed bool
	m_StartTime int32
	m_EndTime_changed bool
	m_EndTime int32
}
func new_dbActivitysToDeleteRow(table *dbActivitysToDeleteTable, Id int32) (r *dbActivitysToDeleteRow) {
	th := &dbActivitysToDeleteRow{}
	th.m_table = table
	th.m_Id = Id
	th.m_lock = NewRWMutex()
	th.m_StartTime_changed=true
	th.m_EndTime_changed=true
	return th
}
func (th *dbActivitysToDeleteRow) GetId() (r int32) {
	return th.m_Id
}
func (th *dbActivitysToDeleteRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbActivitysToDeleteRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(3)
		db_args.Push(th.m_Id)
		db_args.Push(th.m_StartTime)
		db_args.Push(th.m_EndTime)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.m_StartTime_changed||th.m_EndTime_changed{
			update_string = "UPDATE ActivitysToDeletes SET "
			db_args:=new_db_args(3)
			if th.m_StartTime_changed{
				update_string+="StartTime=?,"
				db_args.Push(th.m_StartTime)
			}
			if th.m_EndTime_changed{
				update_string+="EndTime=?,"
				db_args.Push(th.m_EndTime)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE Id=?"
			db_args.Push(th.m_Id)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.m_StartTime_changed = false
	th.m_EndTime_changed = false
	if release && th.m_loaded {
		atomic.AddInt32(&th.m_table.m_gc_n, -1)
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbActivitysToDeleteRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT ActivitysToDeletes exec failed %v ", th.m_Id)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE ActivitysToDeletes exec failed %v", th.m_Id)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
func (th *dbActivitysToDeleteRow) Touch(releasable bool) {
	th.m_touch = int32(time.Now().Unix())
	th.m_releasable = releasable
}
type dbActivitysToDeleteRowSort struct {
	rows []*dbActivitysToDeleteRow
}
func (th *dbActivitysToDeleteRowSort) Len() (length int) {
	return len(th.rows)
}
func (th *dbActivitysToDeleteRowSort) Less(i int, j int) (less bool) {
	return th.rows[i].m_touch < th.rows[j].m_touch
}
func (th *dbActivitysToDeleteRowSort) Swap(i int, j int) {
	temp := th.rows[i]
	th.rows[i] = th.rows[j]
	th.rows[j] = temp
}
type dbActivitysToDeleteTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_rows map[int32]*dbActivitysToDeleteRow
	m_new_rows map[int32]*dbActivitysToDeleteRow
	m_removed_rows map[int32]*dbActivitysToDeleteRow
	m_gc_n int32
	m_gcing int32
	m_pool_size int32
	m_preload_select_stmt *sql.Stmt
	m_preload_max_id int32
	m_save_insert_stmt *sql.Stmt
	m_delete_stmt *sql.Stmt
}
func new_dbActivitysToDeleteTable(dbc *DBC) (th *dbActivitysToDeleteTable) {
	th = &dbActivitysToDeleteTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	th.m_rows = make(map[int32]*dbActivitysToDeleteRow)
	th.m_new_rows = make(map[int32]*dbActivitysToDeleteRow)
	th.m_removed_rows = make(map[int32]*dbActivitysToDeleteRow)
	return th
}
func (th *dbActivitysToDeleteTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS ActivitysToDeletes(Id int(11),PRIMARY KEY (Id))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS ActivitysToDeletes failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='ActivitysToDeletes'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasStartTime := columns["StartTime"]
	if !hasStartTime {
		_, err = th.m_dbc.Exec("ALTER TABLE ActivitysToDeletes ADD COLUMN StartTime int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN StartTime failed")
			return
		}
	}
	_, hasEndTime := columns["EndTime"]
	if !hasEndTime {
		_, err = th.m_dbc.Exec("ALTER TABLE ActivitysToDeletes ADD COLUMN EndTime int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN EndTime failed")
			return
		}
	}
	return
}
func (th *dbActivitysToDeleteTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT Id,StartTime,EndTime FROM ActivitysToDeletes")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbActivitysToDeleteTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO ActivitysToDeletes (Id,StartTime,EndTime) VALUES (?,?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbActivitysToDeleteTable) prepare_delete_stmt() (err error) {
	th.m_delete_stmt,err=th.m_dbc.StmtPrepare("DELETE FROM ActivitysToDeletes WHERE Id=?")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbActivitysToDeleteTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	err=th.prepare_delete_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbActivitysToDeleteTable) Preload() (err error) {
	r, err := th.m_dbc.StmtQuery(th.m_preload_select_stmt)
	if err != nil {
		log.Error("SELECT")
		return
	}
	var Id int32
	var dStartTime int32
	var dEndTime int32
		th.m_preload_max_id = 0
	for r.Next() {
		err = r.Scan(&Id,&dStartTime,&dEndTime)
		if err != nil {
			log.Error("Scan err[%v]", err.Error())
			return
		}
		if Id>th.m_preload_max_id{
			th.m_preload_max_id =Id
		}
		row := new_dbActivitysToDeleteRow(th,Id)
		row.m_StartTime=dStartTime
		row.m_EndTime=dEndTime
		row.m_StartTime_changed=false
		row.m_EndTime_changed=false
		row.m_valid = true
		th.m_rows[Id]=row
	}
	return
}
func (th *dbActivitysToDeleteTable) GetPreloadedMaxId() (max_id int32) {
	return th.m_preload_max_id
}
func (th *dbActivitysToDeleteTable) fetch_rows(rows map[int32]*dbActivitysToDeleteRow) (r map[int32]*dbActivitysToDeleteRow) {
	th.m_lock.UnSafeLock("dbActivitysToDeleteTable.fetch_rows")
	defer th.m_lock.UnSafeUnlock()
	r = make(map[int32]*dbActivitysToDeleteRow)
	for i, v := range rows {
		r[i] = v
	}
	return r
}
func (th *dbActivitysToDeleteTable) fetch_new_rows() (new_rows map[int32]*dbActivitysToDeleteRow) {
	th.m_lock.UnSafeLock("dbActivitysToDeleteTable.fetch_new_rows")
	defer th.m_lock.UnSafeUnlock()
	new_rows = make(map[int32]*dbActivitysToDeleteRow)
	for i, v := range th.m_new_rows {
		_, has := th.m_rows[i]
		if has {
			log.Error("rows already has new rows %v", i)
			continue
		}
		th.m_rows[i] = v
		new_rows[i] = v
	}
	for i, _ := range new_rows {
		delete(th.m_new_rows, i)
	}
	return
}
func (th *dbActivitysToDeleteTable) save_rows(rows map[int32]*dbActivitysToDeleteRow, quick bool) {
	for _, v := range rows {
		if th.m_dbc.m_quit && !quick {
			return
		}
		err, delay, _ := v.Save(false)
		if err != nil {
			log.Error("save failed %v", err)
		}
		if th.m_dbc.m_quit && !quick {
			return
		}
		if delay&&!quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
}
func (th *dbActivitysToDeleteTable) Save(quick bool) (err error){
	removed_rows := th.fetch_rows(th.m_removed_rows)
	for _, v := range removed_rows {
		_, err := th.m_dbc.StmtExec(th.m_delete_stmt, v.GetId())
		if err != nil {
			log.Error("exec delete stmt failed %v", err)
		}
		v.m_valid = false
		if !quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
	th.m_removed_rows = make(map[int32]*dbActivitysToDeleteRow)
	rows := th.fetch_rows(th.m_rows)
	th.save_rows(rows, quick)
	new_rows := th.fetch_new_rows()
	th.save_rows(new_rows, quick)
	return
}
func (th *dbActivitysToDeleteTable) AddRow(Id int32) (row *dbActivitysToDeleteRow) {
	th.m_lock.UnSafeLock("dbActivitysToDeleteTable.AddRow")
	defer th.m_lock.UnSafeUnlock()
	row = new_dbActivitysToDeleteRow(th,Id)
	row.m_new = true
	row.m_loaded = true
	row.m_valid = true
	_, has := th.m_new_rows[Id]
	if has{
		log.Error("已经存在 %v", Id)
		return nil
	}
	th.m_new_rows[Id] = row
	atomic.AddInt32(&th.m_gc_n,1)
	return row
}
func (th *dbActivitysToDeleteTable) RemoveRow(Id int32) {
	th.m_lock.UnSafeLock("dbActivitysToDeleteTable.RemoveRow")
	defer th.m_lock.UnSafeUnlock()
	row := th.m_rows[Id]
	if row != nil {
		row.m_remove = true
		delete(th.m_rows, Id)
		rm_row := th.m_removed_rows[Id]
		if rm_row != nil {
			log.Error("rows and removed rows both has %v", Id)
		}
		th.m_removed_rows[Id] = row
		_, has_new := th.m_new_rows[Id]
		if has_new {
			delete(th.m_new_rows, Id)
			log.Error("rows and new_rows both has %v", Id)
		}
	} else {
		row = th.m_removed_rows[Id]
		if row == nil {
			_, has_new := th.m_new_rows[Id]
			if has_new {
				delete(th.m_new_rows, Id)
			} else {
				log.Error("row not exist %v", Id)
			}
		} else {
			log.Error("already removed %v", Id)
			_, has_new := th.m_new_rows[Id]
			if has_new {
				delete(th.m_new_rows, Id)
				log.Error("removed rows and new_rows both has %v", Id)
			}
		}
	}
}
func (th *dbActivitysToDeleteTable) GetRow(Id int32) (row *dbActivitysToDeleteRow) {
	th.m_lock.UnSafeRLock("dbActivitysToDeleteTable.GetRow")
	defer th.m_lock.UnSafeRUnlock()
	row = th.m_rows[Id]
	if row == nil {
		row = th.m_new_rows[Id]
	}
	return row
}
func (th *dbSysMailCommonRow)GetCurrMailId( )(r int32 ){
	th.m_lock.UnSafeRLock("dbSysMailCommonRow.GetdbSysMailCommonCurrMailIdColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_CurrMailId)
}
func (th *dbSysMailCommonRow)SetCurrMailId(v int32){
	th.m_lock.UnSafeLock("dbSysMailCommonRow.SetdbSysMailCommonCurrMailIdColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_CurrMailId=int32(v)
	th.m_CurrMailId_changed=true
	return
}
type dbSysMailCommonRow struct {
	m_table *dbSysMailCommonTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_Id        int32
	m_CurrMailId_changed bool
	m_CurrMailId int32
}
func new_dbSysMailCommonRow(table *dbSysMailCommonTable, Id int32) (r *dbSysMailCommonRow) {
	th := &dbSysMailCommonRow{}
	th.m_table = table
	th.m_Id = Id
	th.m_lock = NewRWMutex()
	th.m_CurrMailId_changed=true
	return th
}
func (th *dbSysMailCommonRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbSysMailCommonRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(2)
		db_args.Push(th.m_Id)
		db_args.Push(th.m_CurrMailId)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.m_CurrMailId_changed{
			update_string = "UPDATE SysMailCommon SET "
			db_args:=new_db_args(2)
			if th.m_CurrMailId_changed{
				update_string+="CurrMailId=?,"
				db_args.Push(th.m_CurrMailId)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE Id=?"
			db_args.Push(th.m_Id)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.m_CurrMailId_changed = false
	if release && th.m_loaded {
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbSysMailCommonRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT SysMailCommon exec failed %v ", th.m_Id)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE SysMailCommon exec failed %v", th.m_Id)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
type dbSysMailCommonTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_row *dbSysMailCommonRow
	m_preload_select_stmt *sql.Stmt
	m_save_insert_stmt *sql.Stmt
}
func new_dbSysMailCommonTable(dbc *DBC) (th *dbSysMailCommonTable) {
	th = &dbSysMailCommonTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	return th
}
func (th *dbSysMailCommonTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS SysMailCommon(Id int(11),PRIMARY KEY (Id))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS SysMailCommon failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='SysMailCommon'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasCurrMailId := columns["CurrMailId"]
	if !hasCurrMailId {
		_, err = th.m_dbc.Exec("ALTER TABLE SysMailCommon ADD COLUMN CurrMailId int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN CurrMailId failed")
			return
		}
	}
	return
}
func (th *dbSysMailCommonTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT CurrMailId FROM SysMailCommon WHERE Id=0")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbSysMailCommonTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO SysMailCommon (Id,CurrMailId) VALUES (?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbSysMailCommonTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbSysMailCommonTable) Preload() (err error) {
	r := th.m_dbc.StmtQueryRow(th.m_preload_select_stmt)
	var dCurrMailId int32
	err = r.Scan(&dCurrMailId)
	if err!=nil{
		if err!=sql.ErrNoRows{
			log.Error("Scan failed")
			return
		}
	}else{
		row := new_dbSysMailCommonRow(th,0)
		row.m_CurrMailId=dCurrMailId
		row.m_CurrMailId_changed=false
		row.m_valid = true
		row.m_loaded=true
		th.m_row=row
	}
	if th.m_row == nil {
		th.m_row = new_dbSysMailCommonRow(th, 0)
		th.m_row.m_new = true
		th.m_row.m_valid = true
		err = th.Save(false)
		if err != nil {
			log.Error("save failed")
			return
		}
		th.m_row.m_loaded = true
	}
	return
}
func (th *dbSysMailCommonTable) Save(quick bool) (err error) {
	if th.m_row==nil{
		return errors.New("row nil")
	}
	err, _, _ = th.m_row.Save(false)
	return err
}
func (th *dbSysMailCommonTable) GetRow( ) (row *dbSysMailCommonRow) {
	return th.m_row
}
func (th *dbSysMailRow)GetTableId( )(r int32 ){
	th.m_lock.UnSafeRLock("dbSysMailRow.GetdbSysMailTableIdColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_TableId)
}
func (th *dbSysMailRow)SetTableId(v int32){
	th.m_lock.UnSafeLock("dbSysMailRow.SetdbSysMailTableIdColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_TableId=int32(v)
	th.m_TableId_changed=true
	return
}
type dbSysMailAttachedItemsColumn struct{
	m_row *dbSysMailRow
	m_data *dbSysMailAttachedItemsData
	m_changed bool
}
func (th *dbSysMailAttachedItemsColumn)load(data []byte)(err error){
	if data == nil || len(data) == 0 {
		th.m_data = &dbSysMailAttachedItemsData{}
		th.m_changed = false
		return nil
	}
	pb := &db.SysMailAttachedItems{}
	err = proto.Unmarshal(data, pb)
	if err != nil {
		log.Error("Unmarshal %v", th.m_row.GetId())
		return
	}
	th.m_data = &dbSysMailAttachedItemsData{}
	th.m_data.from_pb(pb)
	th.m_changed = false
	return
}
func (th *dbSysMailAttachedItemsColumn)save( )(data []byte,err error){
	pb:=th.m_data.to_pb()
	data, err = proto.Marshal(pb)
	if err != nil {
		log.Error("Marshal %v", th.m_row.GetId())
		return
	}
	th.m_changed = false
	return
}
func (th *dbSysMailAttachedItemsColumn)Get( )(v *dbSysMailAttachedItemsData ){
	th.m_row.m_lock.UnSafeRLock("dbSysMailAttachedItemsColumn.Get")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v=&dbSysMailAttachedItemsData{}
	th.m_data.clone_to(v)
	return
}
func (th *dbSysMailAttachedItemsColumn)Set(v dbSysMailAttachedItemsData ){
	th.m_row.m_lock.UnSafeLock("dbSysMailAttachedItemsColumn.Set")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data=&dbSysMailAttachedItemsData{}
	v.clone_to(th.m_data)
	th.m_changed=true
	return
}
func (th *dbSysMailAttachedItemsColumn)GetItemList( )(v []int32 ){
	th.m_row.m_lock.UnSafeRLock("dbSysMailAttachedItemsColumn.GetItemList")
	defer th.m_row.m_lock.UnSafeRUnlock()
	v = make([]int32, len(th.m_data.ItemList))
	for _ii, _vv := range th.m_data.ItemList {
		v[_ii]=_vv
	}
	return
}
func (th *dbSysMailAttachedItemsColumn)SetItemList(v []int32){
	th.m_row.m_lock.UnSafeLock("dbSysMailAttachedItemsColumn.SetItemList")
	defer th.m_row.m_lock.UnSafeUnlock()
	th.m_data.ItemList = make([]int32, len(v))
	for _ii, _vv := range v {
		th.m_data.ItemList[_ii]=_vv
	}
	th.m_changed = true
	return
}
func (th *dbSysMailRow)GetSendTime( )(r int32 ){
	th.m_lock.UnSafeRLock("dbSysMailRow.GetdbSysMailSendTimeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_SendTime)
}
func (th *dbSysMailRow)SetSendTime(v int32){
	th.m_lock.UnSafeLock("dbSysMailRow.SetdbSysMailSendTimeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_SendTime=int32(v)
	th.m_SendTime_changed=true
	return
}
type dbSysMailRow struct {
	m_table *dbSysMailTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_Id        int32
	m_TableId_changed bool
	m_TableId int32
	AttachedItems dbSysMailAttachedItemsColumn
	m_SendTime_changed bool
	m_SendTime int32
}
func new_dbSysMailRow(table *dbSysMailTable, Id int32) (r *dbSysMailRow) {
	th := &dbSysMailRow{}
	th.m_table = table
	th.m_Id = Id
	th.m_lock = NewRWMutex()
	th.m_TableId_changed=true
	th.m_SendTime_changed=true
	th.AttachedItems.m_row=th
	th.AttachedItems.m_data=&dbSysMailAttachedItemsData{}
	return th
}
func (th *dbSysMailRow) GetId() (r int32) {
	return th.m_Id
}
func (th *dbSysMailRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbSysMailRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(4)
		db_args.Push(th.m_Id)
		db_args.Push(th.m_TableId)
		dAttachedItems,db_err:=th.AttachedItems.save()
		if db_err!=nil{
			log.Error("insert save AttachedItems failed")
			return db_err,false,0,"",nil
		}
		db_args.Push(dAttachedItems)
		db_args.Push(th.m_SendTime)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.m_TableId_changed||th.AttachedItems.m_changed||th.m_SendTime_changed{
			update_string = "UPDATE SysMails SET "
			db_args:=new_db_args(4)
			if th.m_TableId_changed{
				update_string+="TableId=?,"
				db_args.Push(th.m_TableId)
			}
			if th.AttachedItems.m_changed{
				update_string+="AttachedItems=?,"
				dAttachedItems,err:=th.AttachedItems.save()
				if err!=nil{
					log.Error("update save AttachedItems failed")
					return err,false,0,"",nil
				}
				db_args.Push(dAttachedItems)
			}
			if th.m_SendTime_changed{
				update_string+="SendTime=?,"
				db_args.Push(th.m_SendTime)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE Id=?"
			db_args.Push(th.m_Id)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.m_TableId_changed = false
	th.AttachedItems.m_changed = false
	th.m_SendTime_changed = false
	if release && th.m_loaded {
		atomic.AddInt32(&th.m_table.m_gc_n, -1)
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbSysMailRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT SysMails exec failed %v ", th.m_Id)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE SysMails exec failed %v", th.m_Id)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
func (th *dbSysMailRow) Touch(releasable bool) {
	th.m_touch = int32(time.Now().Unix())
	th.m_releasable = releasable
}
type dbSysMailRowSort struct {
	rows []*dbSysMailRow
}
func (th *dbSysMailRowSort) Len() (length int) {
	return len(th.rows)
}
func (th *dbSysMailRowSort) Less(i int, j int) (less bool) {
	return th.rows[i].m_touch < th.rows[j].m_touch
}
func (th *dbSysMailRowSort) Swap(i int, j int) {
	temp := th.rows[i]
	th.rows[i] = th.rows[j]
	th.rows[j] = temp
}
type dbSysMailTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_rows map[int32]*dbSysMailRow
	m_new_rows map[int32]*dbSysMailRow
	m_removed_rows map[int32]*dbSysMailRow
	m_gc_n int32
	m_gcing int32
	m_pool_size int32
	m_preload_select_stmt *sql.Stmt
	m_preload_max_id int32
	m_save_insert_stmt *sql.Stmt
	m_delete_stmt *sql.Stmt
	m_max_id int32
	m_max_id_changed bool
}
func new_dbSysMailTable(dbc *DBC) (th *dbSysMailTable) {
	th = &dbSysMailTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	th.m_rows = make(map[int32]*dbSysMailRow)
	th.m_new_rows = make(map[int32]*dbSysMailRow)
	th.m_removed_rows = make(map[int32]*dbSysMailRow)
	return th
}
func (th *dbSysMailTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS SysMailsMaxId(PlaceHolder int(11),MaxId int(11),PRIMARY KEY (PlaceHolder))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS SysMailsMaxId failed")
		return
	}
	r := th.m_dbc.QueryRow("SELECT Count(*) FROM SysMailsMaxId WHERE PlaceHolder=0")
	if r != nil {
		var count int32
		err = r.Scan(&count)
		if err != nil {
			log.Error("scan count failed")
			return
		}
		if count == 0 {
		_, err = th.m_dbc.Exec("INSERT INTO SysMailsMaxId (PlaceHolder,MaxId) VALUES (0,0)")
			if err != nil {
				log.Error("INSERTSysMailsMaxId failed")
				return
			}
		}
	}
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS SysMails(Id int(11),PRIMARY KEY (Id))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS SysMails failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='SysMails'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasTableId := columns["TableId"]
	if !hasTableId {
		_, err = th.m_dbc.Exec("ALTER TABLE SysMails ADD COLUMN TableId int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN TableId failed")
			return
		}
	}
	_, hasAttachedItems := columns["AttachedItems"]
	if !hasAttachedItems {
		_, err = th.m_dbc.Exec("ALTER TABLE SysMails ADD COLUMN AttachedItems LONGBLOB")
		if err != nil {
			log.Error("ADD COLUMN AttachedItems failed")
			return
		}
	}
	_, hasSendTime := columns["SendTime"]
	if !hasSendTime {
		_, err = th.m_dbc.Exec("ALTER TABLE SysMails ADD COLUMN SendTime int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN SendTime failed")
			return
		}
	}
	return
}
func (th *dbSysMailTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT Id,TableId,AttachedItems,SendTime FROM SysMails")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbSysMailTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO SysMails (Id,TableId,AttachedItems,SendTime) VALUES (?,?,?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbSysMailTable) prepare_delete_stmt() (err error) {
	th.m_delete_stmt,err=th.m_dbc.StmtPrepare("DELETE FROM SysMails WHERE Id=?")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbSysMailTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	err=th.prepare_delete_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbSysMailTable) Preload() (err error) {
	r_max_id := th.m_dbc.QueryRow("SELECT MaxId FROM SysMailsMaxId WHERE PLACEHOLDER=0")
	if r_max_id != nil {
		err = r_max_id.Scan(&th.m_max_id)
		if err != nil {
			log.Error("scan max id failed")
			return
		}
	}
	r, err := th.m_dbc.StmtQuery(th.m_preload_select_stmt)
	if err != nil {
		log.Error("SELECT")
		return
	}
	var Id int32
	var dTableId int32
	var dAttachedItems []byte
	var dSendTime int32
	for r.Next() {
		err = r.Scan(&Id,&dTableId,&dAttachedItems,&dSendTime)
		if err != nil {
			log.Error("Scan err[%v]", err.Error())
			return
		}
		if Id>th.m_max_id{
			log.Error("max id ext")
			th.m_max_id = Id
			th.m_max_id_changed = true
		}
		row := new_dbSysMailRow(th,Id)
		row.m_TableId=dTableId
		err = row.AttachedItems.load(dAttachedItems)
		if err != nil {
			log.Error("AttachedItems %v", Id)
			return
		}
		row.m_SendTime=dSendTime
		row.m_TableId_changed=false
		row.m_SendTime_changed=false
		row.m_valid = true
		th.m_rows[Id]=row
	}
	return
}
func (th *dbSysMailTable) GetPreloadedMaxId() (max_id int32) {
	return th.m_preload_max_id
}
func (th *dbSysMailTable) fetch_rows(rows map[int32]*dbSysMailRow) (r map[int32]*dbSysMailRow) {
	th.m_lock.UnSafeLock("dbSysMailTable.fetch_rows")
	defer th.m_lock.UnSafeUnlock()
	r = make(map[int32]*dbSysMailRow)
	for i, v := range rows {
		r[i] = v
	}
	return r
}
func (th *dbSysMailTable) fetch_new_rows() (new_rows map[int32]*dbSysMailRow) {
	th.m_lock.UnSafeLock("dbSysMailTable.fetch_new_rows")
	defer th.m_lock.UnSafeUnlock()
	new_rows = make(map[int32]*dbSysMailRow)
	for i, v := range th.m_new_rows {
		_, has := th.m_rows[i]
		if has {
			log.Error("rows already has new rows %v", i)
			continue
		}
		th.m_rows[i] = v
		new_rows[i] = v
	}
	for i, _ := range new_rows {
		delete(th.m_new_rows, i)
	}
	return
}
func (th *dbSysMailTable) save_rows(rows map[int32]*dbSysMailRow, quick bool) {
	for _, v := range rows {
		if th.m_dbc.m_quit && !quick {
			return
		}
		err, delay, _ := v.Save(false)
		if err != nil {
			log.Error("save failed %v", err)
		}
		if th.m_dbc.m_quit && !quick {
			return
		}
		if delay&&!quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
}
func (th *dbSysMailTable) Save(quick bool) (err error){
	if th.m_max_id_changed {
		max_id := atomic.LoadInt32(&th.m_max_id)
		_, err := th.m_dbc.Exec("UPDATE SysMailsMaxId SET MaxId=?", max_id)
		if err != nil {
			log.Error("save max id failed %v", err)
		}
	}
	removed_rows := th.fetch_rows(th.m_removed_rows)
	for _, v := range removed_rows {
		_, err := th.m_dbc.StmtExec(th.m_delete_stmt, v.GetId())
		if err != nil {
			log.Error("exec delete stmt failed %v", err)
		}
		v.m_valid = false
		if !quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
	th.m_removed_rows = make(map[int32]*dbSysMailRow)
	rows := th.fetch_rows(th.m_rows)
	th.save_rows(rows, quick)
	new_rows := th.fetch_new_rows()
	th.save_rows(new_rows, quick)
	return
}
func (th *dbSysMailTable) AddRow() (row *dbSysMailRow) {
	th.m_lock.UnSafeLock("dbSysMailTable.AddRow")
	defer th.m_lock.UnSafeUnlock()
	Id := atomic.AddInt32(&th.m_max_id, 1)
	th.m_max_id_changed = true
	row = new_dbSysMailRow(th,Id)
	row.m_new = true
	row.m_loaded = true
	row.m_valid = true
	th.m_new_rows[Id] = row
	atomic.AddInt32(&th.m_gc_n,1)
	return row
}
func (th *dbSysMailTable) RemoveRow(Id int32) {
	th.m_lock.UnSafeLock("dbSysMailTable.RemoveRow")
	defer th.m_lock.UnSafeUnlock()
	row := th.m_rows[Id]
	if row != nil {
		row.m_remove = true
		delete(th.m_rows, Id)
		rm_row := th.m_removed_rows[Id]
		if rm_row != nil {
			log.Error("rows and removed rows both has %v", Id)
		}
		th.m_removed_rows[Id] = row
		_, has_new := th.m_new_rows[Id]
		if has_new {
			delete(th.m_new_rows, Id)
			log.Error("rows and new_rows both has %v", Id)
		}
	} else {
		row = th.m_removed_rows[Id]
		if row == nil {
			_, has_new := th.m_new_rows[Id]
			if has_new {
				delete(th.m_new_rows, Id)
			} else {
				log.Error("row not exist %v", Id)
			}
		} else {
			log.Error("already removed %v", Id)
			_, has_new := th.m_new_rows[Id]
			if has_new {
				delete(th.m_new_rows, Id)
				log.Error("removed rows and new_rows both has %v", Id)
			}
		}
	}
}
func (th *dbSysMailTable) GetRow(Id int32) (row *dbSysMailRow) {
	th.m_lock.UnSafeRLock("dbSysMailTable.GetRow")
	defer th.m_lock.UnSafeRUnlock()
	row = th.m_rows[Id]
	if row == nil {
		row = th.m_new_rows[Id]
	}
	return row
}
func (th *dbBanPlayerRow)GetStartTime( )(r int32 ){
	th.m_lock.UnSafeRLock("dbBanPlayerRow.GetdbBanPlayerStartTimeColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_StartTime)
}
func (th *dbBanPlayerRow)SetStartTime(v int32){
	th.m_lock.UnSafeLock("dbBanPlayerRow.SetdbBanPlayerStartTimeColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_StartTime=int32(v)
	th.m_StartTime_changed=true
	return
}
func (th *dbBanPlayerRow)GetStartTimeStr( )(r string ){
	th.m_lock.UnSafeRLock("dbBanPlayerRow.GetdbBanPlayerStartTimeStrColumn")
	defer th.m_lock.UnSafeRUnlock()
	return string(th.m_StartTimeStr)
}
func (th *dbBanPlayerRow)SetStartTimeStr(v string){
	th.m_lock.UnSafeLock("dbBanPlayerRow.SetdbBanPlayerStartTimeStrColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_StartTimeStr=string(v)
	th.m_StartTimeStr_changed=true
	return
}
func (th *dbBanPlayerRow)GetDuration( )(r int32 ){
	th.m_lock.UnSafeRLock("dbBanPlayerRow.GetdbBanPlayerDurationColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_Duration)
}
func (th *dbBanPlayerRow)SetDuration(v int32){
	th.m_lock.UnSafeLock("dbBanPlayerRow.SetdbBanPlayerDurationColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Duration=int32(v)
	th.m_Duration_changed=true
	return
}
func (th *dbBanPlayerRow)GetPlayerId( )(r int32 ){
	th.m_lock.UnSafeRLock("dbBanPlayerRow.GetdbBanPlayerPlayerIdColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_PlayerId)
}
func (th *dbBanPlayerRow)SetPlayerId(v int32){
	th.m_lock.UnSafeLock("dbBanPlayerRow.SetdbBanPlayerPlayerIdColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_PlayerId=int32(v)
	th.m_PlayerId_changed=true
	return
}
func (th *dbBanPlayerRow)GetAccount( )(r string ){
	th.m_lock.UnSafeRLock("dbBanPlayerRow.GetdbBanPlayerAccountColumn")
	defer th.m_lock.UnSafeRUnlock()
	return string(th.m_Account)
}
func (th *dbBanPlayerRow)SetAccount(v string){
	th.m_lock.UnSafeLock("dbBanPlayerRow.SetdbBanPlayerAccountColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Account=string(v)
	th.m_Account_changed=true
	return
}
type dbBanPlayerRow struct {
	m_table *dbBanPlayerTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_UniqueId        string
	m_StartTime_changed bool
	m_StartTime int32
	m_StartTimeStr_changed bool
	m_StartTimeStr string
	m_Duration_changed bool
	m_Duration int32
	m_PlayerId_changed bool
	m_PlayerId int32
	m_Account_changed bool
	m_Account string
}
func new_dbBanPlayerRow(table *dbBanPlayerTable, UniqueId string) (r *dbBanPlayerRow) {
	th := &dbBanPlayerRow{}
	th.m_table = table
	th.m_UniqueId = UniqueId
	th.m_lock = NewRWMutex()
	th.m_StartTime_changed=true
	th.m_StartTimeStr_changed=true
	th.m_Duration_changed=true
	th.m_PlayerId_changed=true
	th.m_Account_changed=true
	return th
}
func (th *dbBanPlayerRow) GetUniqueId() (r string) {
	return th.m_UniqueId
}
func (th *dbBanPlayerRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbBanPlayerRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(6)
		db_args.Push(th.m_UniqueId)
		db_args.Push(th.m_StartTime)
		db_args.Push(th.m_StartTimeStr)
		db_args.Push(th.m_Duration)
		db_args.Push(th.m_PlayerId)
		db_args.Push(th.m_Account)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.m_StartTime_changed||th.m_StartTimeStr_changed||th.m_Duration_changed||th.m_PlayerId_changed||th.m_Account_changed{
			update_string = "UPDATE BanPlayers SET "
			db_args:=new_db_args(6)
			if th.m_StartTime_changed{
				update_string+="StartTime=?,"
				db_args.Push(th.m_StartTime)
			}
			if th.m_StartTimeStr_changed{
				update_string+="StartTimeStr=?,"
				db_args.Push(th.m_StartTimeStr)
			}
			if th.m_Duration_changed{
				update_string+="Duration=?,"
				db_args.Push(th.m_Duration)
			}
			if th.m_PlayerId_changed{
				update_string+="PlayerId=?,"
				db_args.Push(th.m_PlayerId)
			}
			if th.m_Account_changed{
				update_string+="Account=?,"
				db_args.Push(th.m_Account)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE UniqueId=?"
			db_args.Push(th.m_UniqueId)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.m_StartTime_changed = false
	th.m_StartTimeStr_changed = false
	th.m_Duration_changed = false
	th.m_PlayerId_changed = false
	th.m_Account_changed = false
	if release && th.m_loaded {
		atomic.AddInt32(&th.m_table.m_gc_n, -1)
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbBanPlayerRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT BanPlayers exec failed %v ", th.m_UniqueId)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE BanPlayers exec failed %v", th.m_UniqueId)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
func (th *dbBanPlayerRow) Touch(releasable bool) {
	th.m_touch = int32(time.Now().Unix())
	th.m_releasable = releasable
}
type dbBanPlayerRowSort struct {
	rows []*dbBanPlayerRow
}
func (th *dbBanPlayerRowSort) Len() (length int) {
	return len(th.rows)
}
func (th *dbBanPlayerRowSort) Less(i int, j int) (less bool) {
	return th.rows[i].m_touch < th.rows[j].m_touch
}
func (th *dbBanPlayerRowSort) Swap(i int, j int) {
	temp := th.rows[i]
	th.rows[i] = th.rows[j]
	th.rows[j] = temp
}
type dbBanPlayerTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_rows map[string]*dbBanPlayerRow
	m_new_rows map[string]*dbBanPlayerRow
	m_removed_rows map[string]*dbBanPlayerRow
	m_gc_n int32
	m_gcing int32
	m_pool_size int32
	m_preload_select_stmt *sql.Stmt
	m_preload_max_id int32
	m_save_insert_stmt *sql.Stmt
	m_delete_stmt *sql.Stmt
}
func new_dbBanPlayerTable(dbc *DBC) (th *dbBanPlayerTable) {
	th = &dbBanPlayerTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	th.m_rows = make(map[string]*dbBanPlayerRow)
	th.m_new_rows = make(map[string]*dbBanPlayerRow)
	th.m_removed_rows = make(map[string]*dbBanPlayerRow)
	return th
}
func (th *dbBanPlayerTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS BanPlayers(UniqueId varchar(64),PRIMARY KEY (UniqueId))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS BanPlayers failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='BanPlayers'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasStartTime := columns["StartTime"]
	if !hasStartTime {
		_, err = th.m_dbc.Exec("ALTER TABLE BanPlayers ADD COLUMN StartTime int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN StartTime failed")
			return
		}
	}
	_, hasStartTimeStr := columns["StartTimeStr"]
	if !hasStartTimeStr {
		_, err = th.m_dbc.Exec("ALTER TABLE BanPlayers ADD COLUMN StartTimeStr varchar(256) DEFAULT ''")
		if err != nil {
			log.Error("ADD COLUMN StartTimeStr failed")
			return
		}
	}
	_, hasDuration := columns["Duration"]
	if !hasDuration {
		_, err = th.m_dbc.Exec("ALTER TABLE BanPlayers ADD COLUMN Duration int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN Duration failed")
			return
		}
	}
	_, hasPlayerId := columns["PlayerId"]
	if !hasPlayerId {
		_, err = th.m_dbc.Exec("ALTER TABLE BanPlayers ADD COLUMN PlayerId int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN PlayerId failed")
			return
		}
	}
	_, hasAccount := columns["Account"]
	if !hasAccount {
		_, err = th.m_dbc.Exec("ALTER TABLE BanPlayers ADD COLUMN Account varchar(256) DEFAULT ''")
		if err != nil {
			log.Error("ADD COLUMN Account failed")
			return
		}
	}
	return
}
func (th *dbBanPlayerTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT UniqueId,StartTime,StartTimeStr,Duration,PlayerId,Account FROM BanPlayers")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbBanPlayerTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO BanPlayers (UniqueId,StartTime,StartTimeStr,Duration,PlayerId,Account) VALUES (?,?,?,?,?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbBanPlayerTable) prepare_delete_stmt() (err error) {
	th.m_delete_stmt,err=th.m_dbc.StmtPrepare("DELETE FROM BanPlayers WHERE UniqueId=?")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbBanPlayerTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	err=th.prepare_delete_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbBanPlayerTable) Preload() (err error) {
	r, err := th.m_dbc.StmtQuery(th.m_preload_select_stmt)
	if err != nil {
		log.Error("SELECT")
		return
	}
	var UniqueId string
	var dStartTime int32
	var dStartTimeStr string
	var dDuration int32
	var dPlayerId int32
	var dAccount string
	for r.Next() {
		err = r.Scan(&UniqueId,&dStartTime,&dStartTimeStr,&dDuration,&dPlayerId,&dAccount)
		if err != nil {
			log.Error("Scan err[%v]", err.Error())
			return
		}
		row := new_dbBanPlayerRow(th,UniqueId)
		row.m_StartTime=dStartTime
		row.m_StartTimeStr=dStartTimeStr
		row.m_Duration=dDuration
		row.m_PlayerId=dPlayerId
		row.m_Account=dAccount
		row.m_StartTime_changed=false
		row.m_StartTimeStr_changed=false
		row.m_Duration_changed=false
		row.m_PlayerId_changed=false
		row.m_Account_changed=false
		row.m_valid = true
		th.m_rows[UniqueId]=row
	}
	return
}
func (th *dbBanPlayerTable) GetPreloadedMaxId() (max_id int32) {
	return th.m_preload_max_id
}
func (th *dbBanPlayerTable) fetch_rows(rows map[string]*dbBanPlayerRow) (r map[string]*dbBanPlayerRow) {
	th.m_lock.UnSafeLock("dbBanPlayerTable.fetch_rows")
	defer th.m_lock.UnSafeUnlock()
	r = make(map[string]*dbBanPlayerRow)
	for i, v := range rows {
		r[i] = v
	}
	return r
}
func (th *dbBanPlayerTable) fetch_new_rows() (new_rows map[string]*dbBanPlayerRow) {
	th.m_lock.UnSafeLock("dbBanPlayerTable.fetch_new_rows")
	defer th.m_lock.UnSafeUnlock()
	new_rows = make(map[string]*dbBanPlayerRow)
	for i, v := range th.m_new_rows {
		_, has := th.m_rows[i]
		if has {
			log.Error("rows already has new rows %v", i)
			continue
		}
		th.m_rows[i] = v
		new_rows[i] = v
	}
	for i, _ := range new_rows {
		delete(th.m_new_rows, i)
	}
	return
}
func (th *dbBanPlayerTable) save_rows(rows map[string]*dbBanPlayerRow, quick bool) {
	for _, v := range rows {
		if th.m_dbc.m_quit && !quick {
			return
		}
		err, delay, _ := v.Save(false)
		if err != nil {
			log.Error("save failed %v", err)
		}
		if th.m_dbc.m_quit && !quick {
			return
		}
		if delay&&!quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
}
func (th *dbBanPlayerTable) Save(quick bool) (err error){
	removed_rows := th.fetch_rows(th.m_removed_rows)
	for _, v := range removed_rows {
		_, err := th.m_dbc.StmtExec(th.m_delete_stmt, v.GetUniqueId())
		if err != nil {
			log.Error("exec delete stmt failed %v", err)
		}
		v.m_valid = false
		if !quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
	th.m_removed_rows = make(map[string]*dbBanPlayerRow)
	rows := th.fetch_rows(th.m_rows)
	th.save_rows(rows, quick)
	new_rows := th.fetch_new_rows()
	th.save_rows(new_rows, quick)
	return
}
func (th *dbBanPlayerTable) AddRow(UniqueId string) (row *dbBanPlayerRow) {
	th.m_lock.UnSafeLock("dbBanPlayerTable.AddRow")
	defer th.m_lock.UnSafeUnlock()
	row = new_dbBanPlayerRow(th,UniqueId)
	row.m_new = true
	row.m_loaded = true
	row.m_valid = true
	_, has := th.m_new_rows[UniqueId]
	if has{
		log.Error("已经存在 %v", UniqueId)
		return nil
	}
	th.m_new_rows[UniqueId] = row
	atomic.AddInt32(&th.m_gc_n,1)
	return row
}
func (th *dbBanPlayerTable) RemoveRow(UniqueId string) {
	th.m_lock.UnSafeLock("dbBanPlayerTable.RemoveRow")
	defer th.m_lock.UnSafeUnlock()
	row := th.m_rows[UniqueId]
	if row != nil {
		row.m_remove = true
		delete(th.m_rows, UniqueId)
		rm_row := th.m_removed_rows[UniqueId]
		if rm_row != nil {
			log.Error("rows and removed rows both has %v", UniqueId)
		}
		th.m_removed_rows[UniqueId] = row
		_, has_new := th.m_new_rows[UniqueId]
		if has_new {
			delete(th.m_new_rows, UniqueId)
			log.Error("rows and new_rows both has %v", UniqueId)
		}
	} else {
		row = th.m_removed_rows[UniqueId]
		if row == nil {
			_, has_new := th.m_new_rows[UniqueId]
			if has_new {
				delete(th.m_new_rows, UniqueId)
			} else {
				log.Error("row not exist %v", UniqueId)
			}
		} else {
			log.Error("already removed %v", UniqueId)
			_, has_new := th.m_new_rows[UniqueId]
			if has_new {
				delete(th.m_new_rows, UniqueId)
				log.Error("removed rows and new_rows both has %v", UniqueId)
			}
		}
	}
}
func (th *dbBanPlayerTable) GetRow(UniqueId string) (row *dbBanPlayerRow) {
	th.m_lock.UnSafeRLock("dbBanPlayerTable.GetRow")
	defer th.m_lock.UnSafeRUnlock()
	row = th.m_rows[UniqueId]
	if row == nil {
		row = th.m_new_rows[UniqueId]
	}
	return row
}
func (th *dbCarnivalRow)GetRound( )(r int32 ){
	th.m_lock.UnSafeRLock("dbCarnivalRow.GetdbCarnivalRoundColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_Round)
}
func (th *dbCarnivalRow)SetRound(v int32){
	th.m_lock.UnSafeLock("dbCarnivalRow.SetdbCarnivalRoundColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Round=int32(v)
	th.m_Round_changed=true
	return
}
type dbCarnivalRow struct {
	m_table *dbCarnivalTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_Id        int32
	m_Round_changed bool
	m_Round int32
}
func new_dbCarnivalRow(table *dbCarnivalTable, Id int32) (r *dbCarnivalRow) {
	th := &dbCarnivalRow{}
	th.m_table = table
	th.m_Id = Id
	th.m_lock = NewRWMutex()
	th.m_Round_changed=true
	return th
}
func (th *dbCarnivalRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbCarnivalRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(2)
		db_args.Push(th.m_Id)
		db_args.Push(th.m_Round)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.m_Round_changed{
			update_string = "UPDATE Carnival SET "
			db_args:=new_db_args(2)
			if th.m_Round_changed{
				update_string+="Round=?,"
				db_args.Push(th.m_Round)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE Id=?"
			db_args.Push(th.m_Id)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.m_Round_changed = false
	if release && th.m_loaded {
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbCarnivalRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT Carnival exec failed %v ", th.m_Id)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE Carnival exec failed %v", th.m_Id)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
type dbCarnivalTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_row *dbCarnivalRow
	m_preload_select_stmt *sql.Stmt
	m_save_insert_stmt *sql.Stmt
}
func new_dbCarnivalTable(dbc *DBC) (th *dbCarnivalTable) {
	th = &dbCarnivalTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	return th
}
func (th *dbCarnivalTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS Carnival(Id int(11),PRIMARY KEY (Id))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS Carnival failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='Carnival'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasRound := columns["Round"]
	if !hasRound {
		_, err = th.m_dbc.Exec("ALTER TABLE Carnival ADD COLUMN Round int(11) DEFAULT 0")
		if err != nil {
			log.Error("ADD COLUMN Round failed")
			return
		}
	}
	return
}
func (th *dbCarnivalTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT Round FROM Carnival WHERE Id=0")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbCarnivalTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO Carnival (Id,Round) VALUES (?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbCarnivalTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbCarnivalTable) Preload() (err error) {
	r := th.m_dbc.StmtQueryRow(th.m_preload_select_stmt)
	var dRound int32
	err = r.Scan(&dRound)
	if err!=nil{
		if err!=sql.ErrNoRows{
			log.Error("Scan failed")
			return
		}
	}else{
		row := new_dbCarnivalRow(th,0)
		row.m_Round=dRound
		row.m_Round_changed=false
		row.m_valid = true
		row.m_loaded=true
		th.m_row=row
	}
	if th.m_row == nil {
		th.m_row = new_dbCarnivalRow(th, 0)
		th.m_row.m_new = true
		th.m_row.m_valid = true
		err = th.Save(false)
		if err != nil {
			log.Error("save failed")
			return
		}
		th.m_row.m_loaded = true
	}
	return
}
func (th *dbCarnivalTable) Save(quick bool) (err error) {
	if th.m_row==nil{
		return errors.New("row nil")
	}
	err, _, _ = th.m_row.Save(false)
	return err
}
func (th *dbCarnivalTable) GetRow( ) (row *dbCarnivalRow) {
	return th.m_row
}
func (th *dbOtherServerPlayerRow)GetAccount( )(r string ){
	th.m_lock.UnSafeRLock("dbOtherServerPlayerRow.GetdbOtherServerPlayerAccountColumn")
	defer th.m_lock.UnSafeRUnlock()
	return string(th.m_Account)
}
func (th *dbOtherServerPlayerRow)SetAccount(v string){
	th.m_lock.UnSafeLock("dbOtherServerPlayerRow.SetdbOtherServerPlayerAccountColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Account=string(v)
	th.m_Account_changed=true
	return
}
func (th *dbOtherServerPlayerRow)GetName( )(r string ){
	th.m_lock.UnSafeRLock("dbOtherServerPlayerRow.GetdbOtherServerPlayerNameColumn")
	defer th.m_lock.UnSafeRUnlock()
	return string(th.m_Name)
}
func (th *dbOtherServerPlayerRow)SetName(v string){
	th.m_lock.UnSafeLock("dbOtherServerPlayerRow.SetdbOtherServerPlayerNameColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Name=string(v)
	th.m_Name_changed=true
	return
}
func (th *dbOtherServerPlayerRow)GetLevel( )(r int32 ){
	th.m_lock.UnSafeRLock("dbOtherServerPlayerRow.GetdbOtherServerPlayerLevelColumn")
	defer th.m_lock.UnSafeRUnlock()
	return int32(th.m_Level)
}
func (th *dbOtherServerPlayerRow)SetLevel(v int32){
	th.m_lock.UnSafeLock("dbOtherServerPlayerRow.SetdbOtherServerPlayerLevelColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Level=int32(v)
	th.m_Level_changed=true
	return
}
func (th *dbOtherServerPlayerRow)GetHead( )(r string ){
	th.m_lock.UnSafeRLock("dbOtherServerPlayerRow.GetdbOtherServerPlayerHeadColumn")
	defer th.m_lock.UnSafeRUnlock()
	return string(th.m_Head)
}
func (th *dbOtherServerPlayerRow)SetHead(v string){
	th.m_lock.UnSafeLock("dbOtherServerPlayerRow.SetdbOtherServerPlayerHeadColumn")
	defer th.m_lock.UnSafeUnlock()
	th.m_Head=string(v)
	th.m_Head_changed=true
	return
}
type dbOtherServerPlayerRow struct {
	m_table *dbOtherServerPlayerTable
	m_lock       *RWMutex
	m_loaded  bool
	m_new     bool
	m_remove  bool
	m_touch      int32
	m_releasable bool
	m_valid   bool
	m_PlayerId        int32
	m_Account_changed bool
	m_Account string
	m_Name_changed bool
	m_Name string
	m_Level_changed bool
	m_Level int32
	m_Head_changed bool
	m_Head string
}
func new_dbOtherServerPlayerRow(table *dbOtherServerPlayerTable, PlayerId int32) (r *dbOtherServerPlayerRow) {
	th := &dbOtherServerPlayerRow{}
	th.m_table = table
	th.m_PlayerId = PlayerId
	th.m_lock = NewRWMutex()
	th.m_Account_changed=true
	th.m_Name_changed=true
	th.m_Level_changed=true
	th.m_Head_changed=true
	return th
}
func (th *dbOtherServerPlayerRow) GetPlayerId() (r int32) {
	return th.m_PlayerId
}
func (th *dbOtherServerPlayerRow) save_data(release bool) (err error, released bool, state int32, update_string string, args []interface{}) {
	th.m_lock.UnSafeLock("dbOtherServerPlayerRow.save_data")
	defer th.m_lock.UnSafeUnlock()
	if th.m_new {
		db_args:=new_db_args(5)
		db_args.Push(th.m_PlayerId)
		db_args.Push(th.m_Account)
		db_args.Push(th.m_Name)
		db_args.Push(th.m_Level)
		db_args.Push(th.m_Head)
		args=db_args.GetArgs()
		state = 1
	} else {
		if th.m_Account_changed||th.m_Name_changed||th.m_Level_changed||th.m_Head_changed{
			update_string = "UPDATE OtherServerPlayers SET "
			db_args:=new_db_args(5)
			if th.m_Account_changed{
				update_string+="Account=?,"
				db_args.Push(th.m_Account)
			}
			if th.m_Name_changed{
				update_string+="Name=?,"
				db_args.Push(th.m_Name)
			}
			if th.m_Level_changed{
				update_string+="Level=?,"
				db_args.Push(th.m_Level)
			}
			if th.m_Head_changed{
				update_string+="Head=?,"
				db_args.Push(th.m_Head)
			}
			update_string = strings.TrimRight(update_string, ", ")
			update_string+=" WHERE PlayerId=?"
			db_args.Push(th.m_PlayerId)
			args=db_args.GetArgs()
			state = 2
		}
	}
	th.m_new = false
	th.m_Account_changed = false
	th.m_Name_changed = false
	th.m_Level_changed = false
	th.m_Head_changed = false
	if release && th.m_loaded {
		atomic.AddInt32(&th.m_table.m_gc_n, -1)
		th.m_loaded = false
		released = true
	}
	return nil,released,state,update_string,args
}
func (th *dbOtherServerPlayerRow) Save(release bool) (err error, d bool, released bool) {
	err,released, state, update_string, args := th.save_data(release)
	if err != nil {
		log.Error("save data failed")
		return err, false, false
	}
	if state == 0 {
		d = false
	} else if state == 1 {
		_, err = th.m_table.m_dbc.StmtExec(th.m_table.m_save_insert_stmt, args...)
		if err != nil {
			log.Error("INSERT OtherServerPlayers exec failed %v ", th.m_PlayerId)
			return err, false, released
		}
		d = true
	} else if state == 2 {
		_, err = th.m_table.m_dbc.Exec(update_string, args...)
		if err != nil {
			log.Error("UPDATE OtherServerPlayers exec failed %v", th.m_PlayerId)
			return err, false, released
		}
		d = true
	}
	return nil, d, released
}
func (th *dbOtherServerPlayerRow) Touch(releasable bool) {
	th.m_touch = int32(time.Now().Unix())
	th.m_releasable = releasable
}
type dbOtherServerPlayerRowSort struct {
	rows []*dbOtherServerPlayerRow
}
func (th *dbOtherServerPlayerRowSort) Len() (length int) {
	return len(th.rows)
}
func (th *dbOtherServerPlayerRowSort) Less(i int, j int) (less bool) {
	return th.rows[i].m_touch < th.rows[j].m_touch
}
func (th *dbOtherServerPlayerRowSort) Swap(i int, j int) {
	temp := th.rows[i]
	th.rows[i] = th.rows[j]
	th.rows[j] = temp
}
type dbOtherServerPlayerTable struct{
	m_dbc *DBC
	m_lock *RWMutex
	m_rows map[int32]*dbOtherServerPlayerRow
	m_new_rows map[int32]*dbOtherServerPlayerRow
	m_removed_rows map[int32]*dbOtherServerPlayerRow
	m_gc_n int32
	m_gcing int32
	m_pool_size int32
	m_preload_select_stmt *sql.Stmt
	m_preload_max_id int32
	m_save_insert_stmt *sql.Stmt
	m_delete_stmt *sql.Stmt
}
func new_dbOtherServerPlayerTable(dbc *DBC) (th *dbOtherServerPlayerTable) {
	th = &dbOtherServerPlayerTable{}
	th.m_dbc = dbc
	th.m_lock = NewRWMutex()
	th.m_rows = make(map[int32]*dbOtherServerPlayerRow)
	th.m_new_rows = make(map[int32]*dbOtherServerPlayerRow)
	th.m_removed_rows = make(map[int32]*dbOtherServerPlayerRow)
	return th
}
func (th *dbOtherServerPlayerTable) check_create_table() (err error) {
	_, err = th.m_dbc.Exec("CREATE TABLE IF NOT EXISTS OtherServerPlayers(PlayerId int(11),PRIMARY KEY (PlayerId))ENGINE=InnoDB ROW_FORMAT=DYNAMIC")
	if err != nil {
		log.Error("CREATE TABLE IF NOT EXISTS OtherServerPlayers failed")
		return
	}
	rows, err := th.m_dbc.Query("SELECT COLUMN_NAME,ORDINAL_POSITION FROM information_schema.`COLUMNS` WHERE TABLE_SCHEMA=? AND TABLE_NAME='OtherServerPlayers'", th.m_dbc.m_db_name)
	if err != nil {
		log.Error("SELECT information_schema failed")
		return
	}
	columns := make(map[string]int32)
	for rows.Next() {
		var column_name string
		var ordinal_position int32
		err = rows.Scan(&column_name, &ordinal_position)
		if err != nil {
			log.Error("scan information_schema row failed")
			return
		}
		if ordinal_position < 1 {
			log.Error("col ordinal out of range")
			continue
		}
		columns[column_name] = ordinal_position
	}
	_, hasAccount := columns["Account"]
	if !hasAccount {
		_, err = th.m_dbc.Exec("ALTER TABLE OtherServerPlayers ADD COLUMN Account varchar(256)")
		if err != nil {
			log.Error("ADD COLUMN Account failed")
			return
		}
	}
	_, hasName := columns["Name"]
	if !hasName {
		_, err = th.m_dbc.Exec("ALTER TABLE OtherServerPlayers ADD COLUMN Name varchar(256)")
		if err != nil {
			log.Error("ADD COLUMN Name failed")
			return
		}
	}
	_, hasLevel := columns["Level"]
	if !hasLevel {
		_, err = th.m_dbc.Exec("ALTER TABLE OtherServerPlayers ADD COLUMN Level int(11)")
		if err != nil {
			log.Error("ADD COLUMN Level failed")
			return
		}
	}
	_, hasHead := columns["Head"]
	if !hasHead {
		_, err = th.m_dbc.Exec("ALTER TABLE OtherServerPlayers ADD COLUMN Head varchar(256)")
		if err != nil {
			log.Error("ADD COLUMN Head failed")
			return
		}
	}
	return
}
func (th *dbOtherServerPlayerTable) prepare_preload_select_stmt() (err error) {
	th.m_preload_select_stmt,err=th.m_dbc.StmtPrepare("SELECT PlayerId,Account,Name,Level,Head FROM OtherServerPlayers")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbOtherServerPlayerTable) prepare_save_insert_stmt()(err error){
	th.m_save_insert_stmt,err=th.m_dbc.StmtPrepare("INSERT INTO OtherServerPlayers (PlayerId,Account,Name,Level,Head) VALUES (?,?,?,?,?)")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbOtherServerPlayerTable) prepare_delete_stmt() (err error) {
	th.m_delete_stmt,err=th.m_dbc.StmtPrepare("DELETE FROM OtherServerPlayers WHERE PlayerId=?")
	if err!=nil{
		log.Error("prepare failed")
		return
	}
	return
}
func (th *dbOtherServerPlayerTable) Init() (err error) {
	err=th.check_create_table()
	if err!=nil{
		log.Error("check_create_table failed")
		return
	}
	err=th.prepare_preload_select_stmt()
	if err!=nil{
		log.Error("prepare_preload_select_stmt failed")
		return
	}
	err=th.prepare_save_insert_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	err=th.prepare_delete_stmt()
	if err!=nil{
		log.Error("prepare_save_insert_stmt failed")
		return
	}
	return
}
func (th *dbOtherServerPlayerTable) Preload() (err error) {
	r, err := th.m_dbc.StmtQuery(th.m_preload_select_stmt)
	if err != nil {
		log.Error("SELECT")
		return
	}
	var PlayerId int32
	var dAccount string
	var dName string
	var dLevel int32
	var dHead string
		th.m_preload_max_id = 0
	for r.Next() {
		err = r.Scan(&PlayerId,&dAccount,&dName,&dLevel,&dHead)
		if err != nil {
			log.Error("Scan err[%v]", err.Error())
			return
		}
		if PlayerId>th.m_preload_max_id{
			th.m_preload_max_id =PlayerId
		}
		row := new_dbOtherServerPlayerRow(th,PlayerId)
		row.m_Account=dAccount
		row.m_Name=dName
		row.m_Level=dLevel
		row.m_Head=dHead
		row.m_Account_changed=false
		row.m_Name_changed=false
		row.m_Level_changed=false
		row.m_Head_changed=false
		row.m_valid = true
		th.m_rows[PlayerId]=row
	}
	return
}
func (th *dbOtherServerPlayerTable) GetPreloadedMaxId() (max_id int32) {
	return th.m_preload_max_id
}
func (th *dbOtherServerPlayerTable) fetch_rows(rows map[int32]*dbOtherServerPlayerRow) (r map[int32]*dbOtherServerPlayerRow) {
	th.m_lock.UnSafeLock("dbOtherServerPlayerTable.fetch_rows")
	defer th.m_lock.UnSafeUnlock()
	r = make(map[int32]*dbOtherServerPlayerRow)
	for i, v := range rows {
		r[i] = v
	}
	return r
}
func (th *dbOtherServerPlayerTable) fetch_new_rows() (new_rows map[int32]*dbOtherServerPlayerRow) {
	th.m_lock.UnSafeLock("dbOtherServerPlayerTable.fetch_new_rows")
	defer th.m_lock.UnSafeUnlock()
	new_rows = make(map[int32]*dbOtherServerPlayerRow)
	for i, v := range th.m_new_rows {
		_, has := th.m_rows[i]
		if has {
			log.Error("rows already has new rows %v", i)
			continue
		}
		th.m_rows[i] = v
		new_rows[i] = v
	}
	for i, _ := range new_rows {
		delete(th.m_new_rows, i)
	}
	return
}
func (th *dbOtherServerPlayerTable) save_rows(rows map[int32]*dbOtherServerPlayerRow, quick bool) {
	for _, v := range rows {
		if th.m_dbc.m_quit && !quick {
			return
		}
		err, delay, _ := v.Save(false)
		if err != nil {
			log.Error("save failed %v", err)
		}
		if th.m_dbc.m_quit && !quick {
			return
		}
		if delay&&!quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
}
func (th *dbOtherServerPlayerTable) Save(quick bool) (err error){
	removed_rows := th.fetch_rows(th.m_removed_rows)
	for _, v := range removed_rows {
		_, err := th.m_dbc.StmtExec(th.m_delete_stmt, v.GetPlayerId())
		if err != nil {
			log.Error("exec delete stmt failed %v", err)
		}
		v.m_valid = false
		if !quick {
			time.Sleep(time.Millisecond * 5)
		}
	}
	th.m_removed_rows = make(map[int32]*dbOtherServerPlayerRow)
	rows := th.fetch_rows(th.m_rows)
	th.save_rows(rows, quick)
	new_rows := th.fetch_new_rows()
	th.save_rows(new_rows, quick)
	return
}
func (th *dbOtherServerPlayerTable) AddRow(PlayerId int32) (row *dbOtherServerPlayerRow) {
	th.m_lock.UnSafeLock("dbOtherServerPlayerTable.AddRow")
	defer th.m_lock.UnSafeUnlock()
	row = new_dbOtherServerPlayerRow(th,PlayerId)
	row.m_new = true
	row.m_loaded = true
	row.m_valid = true
	_, has := th.m_new_rows[PlayerId]
	if has{
		log.Error("已经存在 %v", PlayerId)
		return nil
	}
	th.m_new_rows[PlayerId] = row
	atomic.AddInt32(&th.m_gc_n,1)
	return row
}
func (th *dbOtherServerPlayerTable) RemoveRow(PlayerId int32) {
	th.m_lock.UnSafeLock("dbOtherServerPlayerTable.RemoveRow")
	defer th.m_lock.UnSafeUnlock()
	row := th.m_rows[PlayerId]
	if row != nil {
		row.m_remove = true
		delete(th.m_rows, PlayerId)
		rm_row := th.m_removed_rows[PlayerId]
		if rm_row != nil {
			log.Error("rows and removed rows both has %v", PlayerId)
		}
		th.m_removed_rows[PlayerId] = row
		_, has_new := th.m_new_rows[PlayerId]
		if has_new {
			delete(th.m_new_rows, PlayerId)
			log.Error("rows and new_rows both has %v", PlayerId)
		}
	} else {
		row = th.m_removed_rows[PlayerId]
		if row == nil {
			_, has_new := th.m_new_rows[PlayerId]
			if has_new {
				delete(th.m_new_rows, PlayerId)
			} else {
				log.Error("row not exist %v", PlayerId)
			}
		} else {
			log.Error("already removed %v", PlayerId)
			_, has_new := th.m_new_rows[PlayerId]
			if has_new {
				delete(th.m_new_rows, PlayerId)
				log.Error("removed rows and new_rows both has %v", PlayerId)
			}
		}
	}
}
func (th *dbOtherServerPlayerTable) GetRow(PlayerId int32) (row *dbOtherServerPlayerRow) {
	th.m_lock.UnSafeRLock("dbOtherServerPlayerTable.GetRow")
	defer th.m_lock.UnSafeRUnlock()
	row = th.m_rows[PlayerId]
	if row == nil {
		row = th.m_new_rows[PlayerId]
	}
	return row
}

type DBC struct {
	m_db_name            string
	m_db                 *sql.DB
	m_db_lock            *Mutex
	m_initialized        bool
	m_quit               bool
	m_shutdown_completed bool
	m_shutdown_lock      *Mutex
	m_db_last_copy_time	int32
	m_db_copy_path		string
	m_db_addr			string
	m_db_account			string
	m_db_password		string
	Global *dbGlobalTable
	Players *dbPlayerTable
	BattleSaves *dbBattleSaveTable
	TowerFightSaves *dbTowerFightSaveTable
	ArenaSeason *dbArenaSeasonTable
	Guilds *dbGuildTable
	GuildStages *dbGuildStageTable
	ActivitysToDeletes *dbActivitysToDeleteTable
	SysMailCommon *dbSysMailCommonTable
	SysMails *dbSysMailTable
	BanPlayers *dbBanPlayerTable
	Carnival *dbCarnivalTable
	OtherServerPlayers *dbOtherServerPlayerTable
}
func (th *DBC)init_tables()(err error){
	th.Global = new_dbGlobalTable(th)
	err = th.Global.Init()
	if err != nil {
		log.Error("init Global table failed")
		return
	}
	th.Players = new_dbPlayerTable(th)
	err = th.Players.Init()
	if err != nil {
		log.Error("init Players table failed")
		return
	}
	th.BattleSaves = new_dbBattleSaveTable(th)
	err = th.BattleSaves.Init()
	if err != nil {
		log.Error("init BattleSaves table failed")
		return
	}
	th.TowerFightSaves = new_dbTowerFightSaveTable(th)
	err = th.TowerFightSaves.Init()
	if err != nil {
		log.Error("init TowerFightSaves table failed")
		return
	}
	th.ArenaSeason = new_dbArenaSeasonTable(th)
	err = th.ArenaSeason.Init()
	if err != nil {
		log.Error("init ArenaSeason table failed")
		return
	}
	th.Guilds = new_dbGuildTable(th)
	err = th.Guilds.Init()
	if err != nil {
		log.Error("init Guilds table failed")
		return
	}
	th.GuildStages = new_dbGuildStageTable(th)
	err = th.GuildStages.Init()
	if err != nil {
		log.Error("init GuildStages table failed")
		return
	}
	th.ActivitysToDeletes = new_dbActivitysToDeleteTable(th)
	err = th.ActivitysToDeletes.Init()
	if err != nil {
		log.Error("init ActivitysToDeletes table failed")
		return
	}
	th.SysMailCommon = new_dbSysMailCommonTable(th)
	err = th.SysMailCommon.Init()
	if err != nil {
		log.Error("init SysMailCommon table failed")
		return
	}
	th.SysMails = new_dbSysMailTable(th)
	err = th.SysMails.Init()
	if err != nil {
		log.Error("init SysMails table failed")
		return
	}
	th.BanPlayers = new_dbBanPlayerTable(th)
	err = th.BanPlayers.Init()
	if err != nil {
		log.Error("init BanPlayers table failed")
		return
	}
	th.Carnival = new_dbCarnivalTable(th)
	err = th.Carnival.Init()
	if err != nil {
		log.Error("init Carnival table failed")
		return
	}
	th.OtherServerPlayers = new_dbOtherServerPlayerTable(th)
	err = th.OtherServerPlayers.Init()
	if err != nil {
		log.Error("init OtherServerPlayers table failed")
		return
	}
	return
}
func (th *DBC)Preload()(err error){
	err = th.Global.Preload()
	if err != nil {
		log.Error("preload Global table failed")
		return
	}else{
		log.Info("preload Global table succeed !")
	}
	err = th.Players.Preload()
	if err != nil {
		log.Error("preload Players table failed")
		return
	}else{
		log.Info("preload Players table succeed !")
	}
	err = th.BattleSaves.Preload()
	if err != nil {
		log.Error("preload BattleSaves table failed")
		return
	}else{
		log.Info("preload BattleSaves table succeed !")
	}
	err = th.TowerFightSaves.Preload()
	if err != nil {
		log.Error("preload TowerFightSaves table failed")
		return
	}else{
		log.Info("preload TowerFightSaves table succeed !")
	}
	err = th.ArenaSeason.Preload()
	if err != nil {
		log.Error("preload ArenaSeason table failed")
		return
	}else{
		log.Info("preload ArenaSeason table succeed !")
	}
	err = th.Guilds.Preload()
	if err != nil {
		log.Error("preload Guilds table failed")
		return
	}else{
		log.Info("preload Guilds table succeed !")
	}
	err = th.GuildStages.Preload()
	if err != nil {
		log.Error("preload GuildStages table failed")
		return
	}else{
		log.Info("preload GuildStages table succeed !")
	}
	err = th.ActivitysToDeletes.Preload()
	if err != nil {
		log.Error("preload ActivitysToDeletes table failed")
		return
	}else{
		log.Info("preload ActivitysToDeletes table succeed !")
	}
	err = th.SysMailCommon.Preload()
	if err != nil {
		log.Error("preload SysMailCommon table failed")
		return
	}else{
		log.Info("preload SysMailCommon table succeed !")
	}
	err = th.SysMails.Preload()
	if err != nil {
		log.Error("preload SysMails table failed")
		return
	}else{
		log.Info("preload SysMails table succeed !")
	}
	err = th.BanPlayers.Preload()
	if err != nil {
		log.Error("preload BanPlayers table failed")
		return
	}else{
		log.Info("preload BanPlayers table succeed !")
	}
	err = th.Carnival.Preload()
	if err != nil {
		log.Error("preload Carnival table failed")
		return
	}else{
		log.Info("preload Carnival table succeed !")
	}
	err = th.OtherServerPlayers.Preload()
	if err != nil {
		log.Error("preload OtherServerPlayers table failed")
		return
	}else{
		log.Info("preload OtherServerPlayers table succeed !")
	}
	err = th.on_preload()
	if err != nil {
		log.Error("on_preload failed")
		return
	}
	err = th.Save(true)
	if err != nil {
		log.Error("save on preload failed")
		return
	}
	return
}
func (th *DBC)Save(quick bool)(err error){
	err = th.Global.Save(quick)
	if err != nil {
		log.Error("save Global table failed")
		return
	}
	err = th.Players.Save(quick)
	if err != nil {
		log.Error("save Players table failed")
		return
	}
	err = th.BattleSaves.Save(quick)
	if err != nil {
		log.Error("save BattleSaves table failed")
		return
	}
	err = th.TowerFightSaves.Save(quick)
	if err != nil {
		log.Error("save TowerFightSaves table failed")
		return
	}
	err = th.ArenaSeason.Save(quick)
	if err != nil {
		log.Error("save ArenaSeason table failed")
		return
	}
	err = th.Guilds.Save(quick)
	if err != nil {
		log.Error("save Guilds table failed")
		return
	}
	err = th.GuildStages.Save(quick)
	if err != nil {
		log.Error("save GuildStages table failed")
		return
	}
	err = th.ActivitysToDeletes.Save(quick)
	if err != nil {
		log.Error("save ActivitysToDeletes table failed")
		return
	}
	err = th.SysMailCommon.Save(quick)
	if err != nil {
		log.Error("save SysMailCommon table failed")
		return
	}
	err = th.SysMails.Save(quick)
	if err != nil {
		log.Error("save SysMails table failed")
		return
	}
	err = th.BanPlayers.Save(quick)
	if err != nil {
		log.Error("save BanPlayers table failed")
		return
	}
	err = th.Carnival.Save(quick)
	if err != nil {
		log.Error("save Carnival table failed")
		return
	}
	err = th.OtherServerPlayers.Save(quick)
	if err != nil {
		log.Error("save OtherServerPlayers table failed")
		return
	}
	return
}
