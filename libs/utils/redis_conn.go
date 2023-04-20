package utils

import (
	"context"
	"fmt"
	"ih_server/libs/log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	REDIS_CLIENT_RECONNECT_INTERVAL = 10
)

type RedisConn struct {
	cluster   *redis.ClusterClient
	ctx       context.Context
	addrs     []string
	disc      int32
	disc_chan chan bool
	mtx       sync.Mutex
}

func (r *RedisConn) Connect(addrs []string) bool {
	if r.cluster != nil {
		fmt.Printf("redis cluster 已经建立\n")
		return true
	}

	conn := redis.NewClusterClient(&redis.ClusterOptions{Addrs: addrs})
	r.ctx = context.Background()
	r.cluster = conn
	r.addrs = addrs
	r.disc_chan = make(chan bool)
	fmt.Printf("redis cluster [%v] 創建成功\n", addrs)
	return true
}

func (r *RedisConn) Clear() {
	if r.cluster != nil {
		r.cluster.Close()
		r.cluster = nil
	}
}

func (r *RedisConn) Close() {
	if r.cluster == nil {
		return
	}
	if r.disc == 1 {
		return
	}
	atomic.StoreInt32(&r.disc, 1)
	r.cluster.Close()
	r.cluster = nil
}

func (r *RedisConn) Do(cmd string, args ...interface{}) *redis.Cmd {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if r.cluster == nil {
		return nil
	}
	args = append([]interface{}{cmd}, args...)
	return r.cluster.Do(r.ctx, args...)
}

func (r *RedisConn) HGetAll(key string) *redis.MapStringStringCmd {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if r.cluster == nil {
		return nil
	}
	return r.cluster.HGetAll(r.ctx, key)
}

func (r *RedisConn) HExists(key, field string) *redis.BoolCmd {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if r.cluster == nil {
		return nil
	}
	return r.cluster.HExists(r.ctx, key, field)
}

/*func (this *RedisConn) Post(cmd string, args ...interface{}) error {
	this.mtx.Lock()
	defer this.mtx.Unlock()
	if this.conn == nil {
		return errors.New("未建立redis连接")
	}
	err := this.conn.Send(cmd, args...)
	if err != nil {
		return err
	}
	return this.conn.Flush()
}

func (this *RedisConn) Flush() error {
	this.mtx.Lock()
	defer this.mtx.Unlock()
	if this.conn == nil {
		return errors.New("未建立redis连接")
	}
	return this.conn.Flush()
}

func (this *RedisConn) Receive() (interface{}, error) {
	this.mtx.Lock()
	defer this.mtx.Unlock()
	if this.conn == nil {
		return nil, errors.New("未建立redis连接")
	}
	return this.conn.Receive()
}

func (this *RedisConn) Send(cmd string, args ...interface{}) error {
	this.mtx.Lock()
	defer this.mtx.Unlock()
	if this.conn == nil {
		return errors.New("未建立redis连接")
	}
	return this.conn.Send(cmd, args...)
}*/

func (r *RedisConn) ping() error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	cmd := r.cluster.Ping(r.ctx)
	if cmd.Err() != nil {
		return cmd.Err()
	}
	return nil
}

func (r *RedisConn) Run(interval_ms int) {
	defer func() {
		if err := recover(); err != nil {
			log.Stack(err)
		}
	}()

	if r.cluster == nil {
		return
	}

	for {
		if atomic.LoadInt32(&r.disc) == 1 {
			r.disc_chan <- true
			break
		}

		err := r.ping()
		if err != nil {
			log.Info("redis cluster 斷連了")
			time.Sleep(time.Second * 5)
		} else {
			time.Sleep(time.Millisecond * time.Duration(interval_ms))
		}
	}
}
