package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type GobCodec struct {
	conn io.ReadWriteCloser //tcp链接实例
	buf  *bufio.Writer      //防止阻塞影响效率构建的缓冲带buffer
	dec  *gob.Decoder       //对应gob的decoder 解码器
	enc  *gob.Encoder       //对应gob的encoder 编码器
}

var _Codec = (*GobCodec)(nil)

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)

	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		_ = c.buf.Flush() //bufio 通过flush（）将缓冲写入真实的文件
		if err != nil {
			c.Close()
		}
	}()
	//将head和body编码，最后通过defer闭包flush流将编码信息发送
	//在encodec中传入buf ，编码后的文件流存入buf中， 通过flush（）发送
	if err := c.enc.Encode(h); err != nil {
		log.Println("rpc codec: gob error encoding header:", err)
		return err
	}

	if err := c.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body:", err)
		return err
	}
	return nil
}

func (c *GobCodec) Close() error {
	return c.conn.Close()
}
