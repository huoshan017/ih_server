package csv_readers

import (
	"encoding/csv"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
)

type Carnival struct {
	Round int32
	StartTime string
	EndTime string
}

type CarnivalMgr struct {
	id2items map[int32]*Carnival
	items_array []*Carnival
}

func (this *CarnivalMgr) Read(file_path_name string) bool {
	if file_path_name == "" {
		file_path_name = "../game_csv/carnival.csv"
	}
	cs, err := ioutil.ReadFile(file_path_name)
	if err != nil {
		log.Printf("CarnivalMgr.Read err: %v", err.Error())
		return false
	}

 	r := csv.NewReader(strings.NewReader(string(cs)))
	ss, _ := r.ReadAll()
   	sz := len(ss)
	this.id2items = make(map[int32]*Carnival)
    for i := int32(1); i < int32(sz); i++ {
		//if i < 5 {
		//	continue
		//}
		var v Carnival
		var intv, id int
		// Round
		intv, err = strconv.Atoi(ss[i][0])
		if err != nil {
			log.Printf("table Carnival convert column Round value %v with row %v err %v", ss[i][0], 0, err.Error())
			return false
		}
		v.Round = int32(intv)
		id = intv
		// StartTime
		v.StartTime = ss[i][1]
		// EndTime
		v.EndTime = ss[i][2]
		if id <= 0 {
			continue
		}
		this.id2items[int32(id)] = &v
		this.items_array = append(this.items_array, &v)
   	}
	return true
}

func (this *CarnivalMgr) Get(id int32) *Carnival {
	return this.id2items[id]
}

func (this *CarnivalMgr) GetByIndex(idx int32) *Carnival {
	if int(idx) >= len(this.items_array) {
		return nil
	}
	return this.items_array[idx]
}

func (this *CarnivalMgr) GetNum() int32 {
	return int32(len(this.items_array))
}

