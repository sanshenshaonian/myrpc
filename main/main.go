package main

import (
	"geerpc"
	"log"
	"net"
	"sync"
	"time"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

//开始服务端
func StartServer(addr chan string) {
	var foo Foo
	if err := geerpc.Register(&foo); err != nil {
		log.Fatal("register error:", err)
	}

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
			args := &Args{Num1: i, Num2: i * i}
			var reply int
			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("call foo.sum error", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}
