package main

import (
	"fmt"
	"geerpc"
	"log"
	"net"
	"sync"
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
	log.SetFlags(0)
	var addr = make(chan string)
	go StartServer(addr) //给服务端单开一个go程

	client, _ := geerpc.Dial("tcp", <-addr) //客户端连接 tcp
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("geerpc req %d", i)
			var reply string
			if err := client.Call("foo.sum", args, &reply); err != nil {
				log.Fatal("call foo.sum error", err)
			}
			log.Println("reply", reply)
		}(i)
	}
	wg.Wait()
}
