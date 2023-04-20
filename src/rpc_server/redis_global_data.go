package main

import (
	"ih_server/libs/log"
	"ih_server/libs/utils"
)

type RedisGlobalData struct {
	inited     bool
	redis_conn *utils.RedisConn // redis连接
}

var global_data RedisGlobalData

func (d *RedisGlobalData) Init() bool {
	d.redis_conn = &utils.RedisConn{}
	if d.redis_conn == nil {
		log.Error("redis客户端未初始化")
		return false
	}

	if !d.redis_conn.Connect(config.RedisIPs) {
		return false
	}

	d.inited = true
	log.Info("全局数据GlobalData载入完成")
	return true
}

func (d *RedisGlobalData) Close() {
	d.redis_conn.Close()
}

func (d *RedisGlobalData) RunRedis() {
	d.redis_conn.Run(1000)
}
