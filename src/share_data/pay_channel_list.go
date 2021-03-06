package share_data

import (
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"ih_server/src/server_config"
	"io/ioutil"
)

type PayChannel struct {
	KeyFile string
	Partner string
	*rsa.PublicKey
}

type PayChannelConfig struct {
	PayChannels []*PayChannel
	ConfigPath  string
}

func (config *PayChannelConfig) _read_key(key_file string) *rsa.PublicKey {
	path := server_config.GetGameDataPathFile(key_file)
	content, err := ioutil.ReadFile(path)
	if nil != err {
		fmt.Printf("read key failed (%s)!\n", err.Error())
		return nil
	}

	block, err := base64.StdEncoding.DecodeString(string(content)) //pem.Decode([]byte(content))
	if err != nil {
		fmt.Printf("failed to parse base64 data (%v) the public key, err: %v\n", content, err.Error())
		return nil
	}

	pub, err := x509.ParsePKIXPublicKey(block)
	if nil != err {
		fmt.Printf("read key failed to ParsePkXIPublicKey err: %v\n", err.Error())
		return nil
	}

	return pub.(*rsa.PublicKey)
}

func (config *PayChannelConfig) _read_config(data []byte) bool {
	err := json.Unmarshal(data, config)
	if err != nil {
		fmt.Printf("解析配置文件失败 %v\n", err.Error())
		return false
	}

	for i := 0; i < len(config.PayChannels); i++ {
		pc := config.PayChannels[i]
		if pc == nil {
			continue
		}
		pub_key := config._read_key(server_config.GetGameDataPathFile(pc.KeyFile))
		if pub_key == nil {
			return false
		}
		pc.PublicKey = pub_key
	}

	return true
}

func (config *PayChannelConfig) LoadConfig(filepath string) bool {
	data, err := ioutil.ReadFile(filepath)
	if err != nil {
		fmt.Printf("读取配置文件失败 %v\n", err)
		return false
	}

	if !config._read_config(data) {
		return false
	}

	config.ConfigPath = filepath

	return true
}

func (config *PayChannelConfig) Verify(hashedReceipt, decodedSignature []byte) *PayChannel {
	var pay_channel *PayChannel
	for i := 0; i < len(config.PayChannels); i++ {
		pay_channel = config.PayChannels[i]
		err := rsa.VerifyPKCS1v15(pay_channel.PublicKey, crypto.SHA1, hashedReceipt, decodedSignature)
		if err == nil {
			return pay_channel
		}
	}
	return nil
}
