package codec

import (
	"io"
)

//消息头结构体
type Header struct {
	ServiceMethod string // 格式“Service.Method”
	Seq           uint64 // 消息序列号
	Error         string
}

//编码接口类
//实现了readheader 对消息头解码 readbody对消息体解码 和写消息
type Codec interface {
	io.Closer
	ReadHeader(*Header) error         //消息头解码
	ReadBody(interface{}) error       //消息体解码
	Write(*Header, interface{}) error //写消息
}

type NewCodecFunc func(io.ReadWriteCloser) Codec

type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json" // not implemented
)

//新建一个解码方法map  key为编解码方法，value为创建一个编解码方法类型
var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}
