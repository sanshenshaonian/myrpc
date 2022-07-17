package codec

import (
	"io"
)

type Header struct {
	ServiceMethod string // 服务方法名称
	Seq           uint64 // 请求ID
	Error         string //错误信息 服务端返回错误信息用
}

type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

type NewCodecFunc func(io.ReadWriteCloser) Codec

const (
	GobType  string = "application/gob"
	JsonType string = "application/json" // not implemented
)

var NewCodecFunMap map[string]NewCodecFunc

func init() {
	NewCodecFunMap = make(map[string]NewCodecFunc)
	NewCodecFunMap[GobType] = NewGobCodec
}
