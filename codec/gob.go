package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

//gob类型解码方法类
type GobCodec struct {
	conn io.ReadWriteCloser //conn连接， 通过tcp链接传输编码和解码消息
	buf  *bufio.Writer      //消息缓冲区， 通过buf.flush（）将缓冲区中消息发出
	dec  *gob.Decoder       //god消息解码类型
	enc  *gob.Encoder       //god消息编码类型
}

var _ Codec = (*GobCodec)(nil)

//gob编解码类初始化函数
func NewGobCodec(conn io.ReadWriteCloser) Codec {
	//通过conn传输消息
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

//消息头解码
func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

//消息体解码
func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

//写消息到conn中
func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		_ = c.buf.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
	if err = c.enc.Encode(h); err != nil {
		log.Println("rpc: gob error encoding header:", err)
		return
	}
	if err = c.enc.Encode(body); err != nil {
		log.Println("rpc: gob error encoding body:", err)
		return
	}
	return
}

//关闭conn连接 conn.close（）
func (c *GobCodec) Close() error {
	return c.conn.Close()
}
