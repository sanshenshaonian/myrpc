package main

import (
	"encoding/json"
	"fmt"
	"geerpc"
	"geerpc/codec"
	"log"
	"net"
	"time"
)

//开始服务端
func StartServer(addr chan string) {
	l, err := net.Listen("tcp", "127.0.0.1:8000") //服务端开始监听
	if err != nil {
		log.Fatal("network error", err)
	}

	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String() //通过channel限制 main函数中client 必须在监听程序启动之后才能开始
	geerpc.Accept(l)          //服务端接受连接 开始rpc 解码
}

func main() {
	var addr = make(chan string)

	go StartServer(addr) //给服务端单开一个go程

	conn, _ := net.Dial("tcp", <-addr) //客户端连接 tcp
	defer func() { conn.Close() }()

	time.Sleep(time.Second)

	_ = json.NewEncoder(conn).Encode(geerpc.DefaultOptions) //给option消息编码 使用json编码规则
	cc := codec.NewGobCodec(conn)                           //初始化一个gob编解码器

	for i := 0; i < 5; i++ {
		h := &codec.Header{
			ServiceMethod: "foo.sum",
			Seq:           uint64(i),
		}
		//发送编码后文件
		_ = cc.Write(h, fmt.Sprintf("geerpc req %d", h.Seq))

		var reply string
		_ = cc.ReadHeader(h)
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
}
