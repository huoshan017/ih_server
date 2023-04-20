package server_config

import (
	"encoding/json"
	"flag"
	"fmt"
	"ih_server/libs/log"
	"os"
)

const (
	SERVER_TYPE_CENTER      = 1
	SERVER_TYPE_LOGIN       = 2
	SERVER_TYPE_GAME        = 3
	SERVER_TYPE_RPC         = 4
	SERVER_TYPE_TEST_CLIENT = 100
	SERVER_TYPE_TEST_GM     = 200
)

const (
	RuntimeRootDir = "../"
	ConfigDir      = "conf/"
	LogConfigDir   = "conf/log/"
	GameDataDir    = "game_data/"
	LogDir         = "log/"
	DBBackUpDir    = "db_backup/"
)

type ServerConfig interface {
	GetType() int32
	GetLogConfigFile() string
	GetDBBackupPath() string
}

// 中心服务器配置
type CenterServerConfig struct {
	LogConfigFile             string // 日志配置文件地址
	ListenLoginIP             string // 监听LoginServer
	MaxLoginConnections       int32  // 最大Login连接数
	ListenGameIP              string // 监听game_server的IP
	MaxGameConnections        int32  // 最大game_server连接数
	GmIP                      string // GM命令的地址
	HallServerGroupConfigFile string // 大厅配置文件地址
	MYSQL_NAME                string
	MYSQL_IP                  string
	MYSQL_ACCOUNT             string
	MYSQL_PWD                 string
	DBCST_MIN                 int
	DBCST_MAX                 int
	MYSQL_COPY_PATH           string
}

func (sc *CenterServerConfig) GetType() int32 {
	return int32(SERVER_TYPE_CENTER)
}

func (sc *CenterServerConfig) GetLogConfigFile() string {
	return sc.LogConfigFile
}

func (sc *CenterServerConfig) GetDBBackupPath() string {
	return ""
}

type FacebookConfig struct {
	FacebookAppID     string
	FacebookAppSecret string
}

// 登陆服务器配置
type LoginServerConfig struct {
	ServerId           int32
	InnerVersion       string
	ServerName         string
	ListenClientIP     string
	ListenGameIP       string   // 监听game_server连接
	MaxGameConnections int32    // game_server最大连接数
	LogConfigFile      string   // 日志配置文件
	CenterServerIP     string   // 连接CenterServer
	RedisIPs           []string // 连接redis-cluster
	VerifyAccount      bool     // 验证账号

	Facebook []*FacebookConfig // facebook

	UseHttps bool

	MYSQL_IP        string
	MYSQL_ACCOUNT   string
	MYSQL_PWD       string
	MYSQL_NAME      string
	MYSQL_COPY_PATH string
	DBCST_MIN       int
	DBCST_MAX       int
}

func (sc *LoginServerConfig) GetType() int32 {
	return int32(SERVER_TYPE_LOGIN)
}

func (sc *LoginServerConfig) GetLogConfigFile() string {
	return sc.LogConfigFile
}

func (sc *LoginServerConfig) GetDBBackupPath() string {
	return DBBackUpDir + sc.MYSQL_NAME
}

type PayChannel struct {
	KeyFile string
	Channel string
}

// 游戏服务器配置
type GameServerConfig struct {
	ServerId             uint32
	InnerVersion         string
	ServerName           string
	ListenRoomServerIP   string
	ListenClientInIP     string
	ListenClientOutIP    string
	MaxClientConnections int32
	RpcServerIP          string
	ListenRpcServerIP    string
	LogConfigFile        string // 日志配置文件
	CenterServerIP       string // 中心服务器IP
	MatchServerIP        string // 匹配服务器IP
	RecvMaxMSec          int64  // 接收超时毫秒数
	SendMaxMSec          int64  // 发送超时毫秒数
	RedisIPs             []string
	MYSQL_NAME           string
	MYSQL_IP             string
	MYSQL_ACCOUNT        string
	MYSQL_PWD            string
	DBCST_MIN            int
	DBCST_MAX            int
	MYSQL_COPY_PATH      string
	DisableTestCommand   bool
	UseHttps             bool
	PayChannelList       []*PayChannel // 支付渠道
}

func (sc *GameServerConfig) GetType() int32 {
	return int32(SERVER_TYPE_GAME)
}

func (sc *GameServerConfig) GetLogConfigFile() string {
	return sc.LogConfigFile
}

func (sc *GameServerConfig) GetDBBackupPath() string {
	return DBBackUpDir + sc.MYSQL_NAME
}

// RPC服务器配置
type RpcServerConfig struct {
	LogConfigFile    string
	ListenIP         string
	MaxConnections   int
	RedisIPs         []string
	GmIP             string
	GmServerUseHttps bool
	MYSQL_NAME       string
	MYSQL_IP         string
	MYSQL_ACCOUNT    string
	MYSQL_PWD        string
	MYSQL_COPY_PATH  string
	DBCST_MIN        int
	DBCST_MAX        int
}

func (sc *RpcServerConfig) GetType() int32 {
	return int32(SERVER_TYPE_RPC)
}

func (sc *RpcServerConfig) GetLogConfigFile() string {
	return sc.LogConfigFile
}

func (sc *RpcServerConfig) GetDBBackupPath() string {
	return DBBackUpDir + sc.MYSQL_NAME
}

// 测试客户端配置
type TestClientConfig struct {
	LoginServerIP     string
	LogConfigFile     string
	RegisterUrl       string
	BindNewAccountUrl string
	LoginUrl          string
	SelectServerUrl   string
	SetPasswordUrl    string
	AccountPrefix     string
	AccountStartIndex int32
	AccountNum        int32
	UseHttps          bool
}

func (sc *TestClientConfig) GetType() int32 {
	return int32(SERVER_TYPE_TEST_CLIENT)
}

func (sc *TestClientConfig) GetLogConfigFile() string {
	return sc.LogConfigFile
}

func (sc *TestClientConfig) GetDBBackupPath() string {
	return ""
}

// GM测试配置
type GmTestConfig struct {
	GmServerIP    string
	LogConfigFile string
}

func (sc *GmTestConfig) GetType() int32 {
	return int32(SERVER_TYPE_TEST_GM)
}

func (sc *GmTestConfig) GetLogConfigFile() string {
	return sc.LogConfigFile
}

func (sc *GmTestConfig) GetDBBackupPath() string {
	return ""
}

func _get_config_path(config_file string) (config_path string) {
	if len(os.Args) > 1 {
		arg_config_file := flag.String("f", "", "config file path")
		fmt.Printf("os.Args %v", os.Args)
		if nil != arg_config_file {
			flag.Parse()
			fmt.Printf("配置参数 %v", *arg_config_file)
			config_path = *arg_config_file
		}
	} else {
		config_path = RuntimeRootDir + ConfigDir + config_file
	}
	return
}

func ServerConfigLoad(config_file string, config ServerConfig) bool {
	config_path := _get_config_path(config_file)
	data, err := os.ReadFile(config_path)
	if err != nil {
		fmt.Printf("读取配置文件[%v]失败 %v", config_path, err)
		return false
	}
	err = json.Unmarshal(data, config)
	if err != nil {
		fmt.Printf("解析配置文件[%v]失败 %v", config_path, err)
		return false
	}

	// 加载日志配置
	log_config_path := RuntimeRootDir + LogConfigDir + config.GetLogConfigFile()

	log.Init("", log_config_path, true)
	//defer log.Close()

	return true
}

func ServerConfigClose() {
	log.Close()
}

func GetGameDataPathFile(data_file string) string {
	return RuntimeRootDir + GameDataDir + data_file
}

func GetConfPathFile(config_file string) string {
	return RuntimeRootDir + ConfigDir + config_file
}
