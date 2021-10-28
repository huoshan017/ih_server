package csv_readers

import (
	"encoding/csv"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
)

type Carnivalsub struct {
	ID int32
	ResetTimeType int32
	Reward string
	ActiveType int32
	DescriptionId int32
	Param1 int32
	Param2 int32
	Param3 int32
	Param4 int32
	EventCount int32
	RewardMailID int32
}

type CarnivalsubMgr struct {
	id2items map[int32]*Carnivalsub
	items_array []*Carnivalsub
}

func (this *CarnivalsubMgr) Read(file_path_name string) bool {
	if file_path_name == "" {
		file_path_name = "../game_csv/carnivalsub.csv"
	}
	cs, err := ioutil.ReadFile(file_path_name)
	if err != nil {
		log.Printf("CarnivalsubMgr.Read err: %v", err.Error())
		return false
	}

 	r := csv.NewReader(strings.NewReader(string(cs)))
	ss, _ := r.ReadAll()
   	sz := len(ss)
	this.id2items = make(map[int32]*Carnivalsub)
    for i := int32(1); i < int32(sz); i++ {
		//if i < 5 {
		//	continue
		//}
		var v Carnivalsub
		var intv, id int
		// ID
		intv, err = strconv.Atoi(ss[i][0])
		if err != nil {
			log.Printf("table Carnivalsub convert column ID value %v with row %v err %v", ss[i][0], 0, err.Error())
			return false
		}
		v.ID = int32(intv)
		id = intv
		// ResetTimeType
		intv, err = strconv.Atoi(ss[i][1])
		if err != nil {
			log.Printf("table Carnivalsub convert column ResetTimeType value %v with row %v err %v", ss[i][1], 1, err.Error())
			return false
		}
		v.ResetTimeType = int32(intv)
		// Reward
		v.Reward = ss[i][2]
		// ActiveType
		intv, err = strconv.Atoi(ss[i][3])
		if err != nil {
			log.Printf("table Carnivalsub convert column ActiveType value %v with row %v err %v", ss[i][3], 3, err.Error())
			return false
		}
		v.ActiveType = int32(intv)
		// DescriptionId
		intv, err = strconv.Atoi(ss[i][4])
		if err != nil {
			log.Printf("table Carnivalsub convert column DescriptionId value %v with row %v err %v", ss[i][4], 4, err.Error())
			return false
		}
		v.DescriptionId = int32(intv)
		// Param1
		intv, err = strconv.Atoi(ss[i][5])
		if err != nil {
			log.Printf("table Carnivalsub convert column Param1 value %v with row %v err %v", ss[i][5], 5, err.Error())
			return false
		}
		v.Param1 = int32(intv)
		// Param2
		intv, err = strconv.Atoi(ss[i][6])
		if err != nil {
			log.Printf("table Carnivalsub convert column Param2 value %v with row %v err %v", ss[i][6], 6, err.Error())
			return false
		}
		v.Param2 = int32(intv)
		// Param3
		intv, err = strconv.Atoi(ss[i][7])
		if err != nil {
			log.Printf("table Carnivalsub convert column Param3 value %v with row %v err %v", ss[i][7], 7, err.Error())
			return false
		}
		v.Param3 = int32(intv)
		// Param4
		intv, err = strconv.Atoi(ss[i][8])
		if err != nil {
			log.Printf("table Carnivalsub convert column Param4 value %v with row %v err %v", ss[i][8], 8, err.Error())
			return false
		}
		v.Param4 = int32(intv)
		// EventCount
		intv, err = strconv.Atoi(ss[i][9])
		if err != nil {
			log.Printf("table Carnivalsub convert column EventCount value %v with row %v err %v", ss[i][9], 9, err.Error())
			return false
		}
		v.EventCount = int32(intv)
		// RewardMailID
		intv, err = strconv.Atoi(ss[i][10])
		if err != nil {
			log.Printf("table Carnivalsub convert column RewardMailID value %v with row %v err %v", ss[i][10], 10, err.Error())
			return false
		}
		v.RewardMailID = int32(intv)
		if id <= 0 {
			continue
		}
		this.id2items[int32(id)] = &v
		this.items_array = append(this.items_array, &v)
   	}
	return true
}

func (this *CarnivalsubMgr) Get(id int32) *Carnivalsub {
	return this.id2items[id]
}

func (this *CarnivalsubMgr) GetByIndex(idx int32) *Carnivalsub {
	if int(idx) >= len(this.items_array) {
		return nil
	}
	return this.items_array[idx]
}

func (this *CarnivalsubMgr) GetNum() int32 {
	return int32(len(this.items_array))
}

