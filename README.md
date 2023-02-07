
* [关于RPC实现](#关于RPC实现)
* [关于缓存实现](#关于缓存实现)

# 关于RPC实现

## 消息的序列化与反序列化

一个典型的 RPC 调用如下：

```go
err = client.Call("Arith.Multiply", args, &reply)
```

客户端发送的请求包括服务名 `Arith`，方法名 `Multiply`，参数 `args` 三个，服务端的响应包括错误 `error`，返回值 `reply` 2 个。我们将请求和响应中的参数和返回值抽象为 body，剩余的信息放在 header 中，那么就可以抽象出数据结构 Header：

```go
package codec

import "io"

type Header struct {
	ServiceMethod string // format "Service.Method"
	Seq           uint64 // sequence number chosen by client
	Error         string
}
```

- ServiceMethod 是服务名和方法名，通常与 Go 语言中的结构体和方法相映射。
- Seq 是请求的序号，也可以认为是某个请求的 ID，用来区分不同的请求。
- Error 是错误信息，客户端置为空，服务端如果如果发生错误，将错误信息置于 Error 中。


我们将和消息编解码相关的代码都放到 codec 子目录中，在此之前，还需要在根目录下使用 `go mod init geerpc` 初始化项目，方便后续子 package 之间的引用。

进一步，抽象出对消息体进行编解码的接口 Codec，抽象出接口是为了实现不同的 Codec 实例：

```go
type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}
```

## 通信过程

客户端与服务端的通信需要协商一些内容，例如 HTTP 报文，分为 header 和 body 2 部分，body 的格式和长度通过 header 中的 `Content-Type` 和 `Content-Length` 指定，服务端通过解析 header 就能够知道如何从 body 中读取需要的信息。对于 RPC 协议来说，这部分协商是需要自主设计的。为了提升性能，一般在报文的最开始会规划固定的字节，来协商相关的信息。比如第1个字节用来表示序列化方式，第2个字节表示压缩方式，第3-6字节表示 header 的长度，7-10 字节表示 body 的长度。

目前需要协商的唯一一项内容是消息的编解码方式。我们将这部分信息，放到结构体 `Option` 中承载。目前，已经进入到服务端的实现阶段了。

```go
package geerpc

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber int        // MagicNumber marks this's a geerpc request
	CodecType   codec.Type // client may choose different Codec to encode body
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}
```

一般来说，涉及协议协商的这部分信息，需要设计固定的字节来传输的。但是为了实现上更简单，GeeRPC 客户端固定采用 JSON 编码 Option，后续的 header 和 body 的编码方式由 Option 中的 CodeType 指定，服务端首先使用 JSON 解码 Option，然后通过 Option 的 CodeType 解码剩余的内容。即报文将以这样的形式发送：

```bash
| Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
| <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定   ------->|
```

在一次连接中，Option 固定在报文的最开始，Header 和 Body 可以有多个，即报文可能是这样的。

```go
| Option | Header1 | Body1 | Header2 | Body2 | ...
```

## Call 的设计

对 `net/rpc` 而言，一个函数需要能够被远程调用，需要满足如下五个条件：

- the method's type is exported. -- 方法所属类型是导出的。
- the method is exported. -- 方式是导出的。
- the method has two arguments, both exported (or builtin) types. -- 两个入参，均为导出或内置类型。
- the method's second argument is a pointer. -- 第二个入参必须是一个指针。
- the method has return type error. -- 返回值为 error 类型。

更直观一些：

```go
func (t *T) MethodName(argType T1, replyType *T2) error
```

根据上述要求，首先我们封装了结构体 Call 来承载一次 RPC 调用所需要的信息。

```go
// Call represents an active RPC.
type Call struct {
	Seq           uint64
	ServiceMethod string      // format "<service>.<method>"
	Args          interface{} // arguments to the function
	Reply         interface{} // reply from the function
	Error         error       // if error occurs, it will be set
	Done          chan *Call  // Strobes when call is complete.
}

func (call *Call) done() {
	call.Done <- call
}
```
## 为什么需要超时处理机制

超时处理是 RPC 框架一个比较基本的能力，如果缺少超时处理机制，无论是服务端还是客户端都容易因为网络或其他错误导致挂死，资源耗尽，这些问题的出现大大地降低了服务的可用性。因此，我们需要在 RPC 框架中加入超时处理的能力。

纵观整个远程调用的过程，需要客户端处理超时的地方有：

- 与服务端建立连接，导致的超时
- 发送请求到服务端，写报文导致的超时
- 等待服务端处理时，等待处理导致的超时（比如服务端已挂死，迟迟不响应）
- 从服务端接收响应时，读报文导致的超时

需要服务端处理超时的地方有：

- 读取客户端请求报文时，读报文导致的超时
- 发送响应报文时，写报文导致的超时
- 调用映射服务的方法时，处理报文导致的超时


GeeRPC 在 3 个地方添加了超时处理机制。分别是：

1）客户端创建连接时
2）客户端 `Client.Call()` 整个过程导致的超时（包含发送报文，等待处理，接收报文所有阶段）
3）服务端处理报文，即 `Server.handleRequest` 超时。

## 创建连接超时

为了实现上的简单，将超时设定放在了 Option 中。ConnectTimeout 默认值为 10s，HandleTimeout 默认值为 0，即不设限。

```go
type Option struct {
	MagicNumber    int           // MagicNumber marks this's a geerpc request
	CodecType      codec.Type    // client may choose different Codec to encode body
	ConnectTimeout time.Duration // 0 means no limit
	HandleTimeout  time.Duration
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType,
	ConnectTimeout: time.Second * 10,
}
```

客户端连接超时，只需要为 Dial 添加一层超时处理的外壳即可。


## 负载均衡策略

假设有多个服务实例，每个实例提供相同的功能，为了提高整个系统的吞吐量，每个实例部署在不同的机器上。客户端可以选择任意一个实例进行调用，获取想要的结果。那如何选择呢？取决了负载均衡的策略。对于 RPC 框架来说，我们可以很容易地想到这么几种策略：

- 随机选择策略 - 从服务列表中随机选择一个。
- 轮询算法(Round Robin) - 依次调度不同的服务器，每次调度执行 i = (i + 1) mode n。
- 加权轮询(Weight Round Robin) - 在轮询算法的基础上，为每个服务实例设置一个权重，高性能的机器赋予更高的权重，也可以根据服务实例的当前的负载情况做动态的调整，例如考虑最近5分钟部署服务器的 CPU、内存消耗情况。
- 哈希/一致性哈希策略 - 依据请求的某些特征，计算一个 hash 值，根据 hash 值将请求发送到对应的机器。一致性 hash 还可以解决服务实例动态添加情况下，调度抖动的问题。一致性哈希的一个典型应用场景是分布式缓存服务。感兴趣可以阅读[动手写分布式缓存 - GeeCache第四天 一致性哈希(hash)](https://geektutu.com/post/geecache-day4.html)
- ...

## 服务发现

负载均衡的前提是有多个服务实例，那我们首先实现一个最基础的服务发现模块 Discovery。为了与通信部分解耦，这部分的代码统一放置在 xclient 子目录下。


## 注册中心

注册中心的好处在于，客户端和服务端都只需要感知注册中心的存在，而无需感知对方的存在。更具体一些：

1) 服务端启动后，向注册中心发送注册消息，注册中心得知该服务已经启动，处于可用状态。一般来说，服务端还需要定期向注册中心发送心跳，证明自己还活着。
2) 客户端向注册中心询问，当前哪天服务是可用的，注册中心将可用的服务列表返回客户端。
3) 客户端根据注册中心得到的服务列表，选择其中一个发起调用。


# 关于缓存实现

## 1 FIFO/LFU/LRU 算法简介

GeeCache 的缓存全部存储在内存中，内存是有限的，因此不可能无限制地添加数据。假定我们设置缓存能够使用的内存大小为 N，那么在某一个时间点，添加了某一条缓存记录之后，占用内存超过了 N，这个时候就需要从缓存中移除一条或多条数据了。那移除谁呢？我们肯定希望尽可能移除“没用”的数据，那如何判定数据“有用”还是“没用”呢？

### 1.1 FIFO(First In First Out)

先进先出，也就是淘汰缓存中最老(最早添加)的记录。FIFO 认为，最早添加的记录，其不再被使用的可能性比刚添加的可能性大。这种算法的实现也非常简单，创建一个队列，新增记录添加到队尾，每次内存不够时，淘汰队首。但是很多场景下，部分记录虽然是最早添加但也最常被访问，而不得不因为呆的时间太长而被淘汰。这类数据会被频繁地添加进缓存，又被淘汰出去，导致缓存命中率降低。

### 1.2 LFU(Least Frequently Used)

最少使用，也就是淘汰缓存中访问频率最低的记录。LFU 认为，如果数据过去被访问多次，那么将来被访问的频率也更高。LFU 的实现需要维护一个按照访问次数排序的队列，每次访问，访问次数加1，队列重新排序，淘汰时选择访问次数最少的即可。LFU 算法的命中率是比较高的，但缺点也非常明显，维护每个记录的访问次数，对内存的消耗是很高的；另外，如果数据的访问模式发生变化，LFU 需要较长的时间去适应，也就是说 LFU 算法受历史数据的影响比较大。例如某个数据历史上访问次数奇高，但在某个时间点之后几乎不再被访问，但因为历史访问次数过高，而迟迟不能被淘汰。

### 1.3 LRU(Least Recently Used)

最近最少使用，相对于仅考虑时间因素的 FIFO 和仅考虑访问频率的 LFU，LRU 算法可以认为是相对平衡的一种淘汰算法。LRU 认为，如果数据最近被访问过，那么将来被访问的概率也会更高。LRU 算法的实现非常简单，维护一个队列，如果某条记录被访问了，则移动到队尾，那么队首则是最近最少访问的数据，淘汰该条记录即可。

## 2 LRU 算法实现

### 2.1 核心数据结构
LRU 算法最核心的 2 个数据结构

- 字典(map)，存储键和值的映射关系。这样根据某个键(key)查找对应的值(value)的复杂是`O(1)`，在字典中插入一条记录的复杂度也是`O(1)`。
- 双向链表(double linked list)实现的队列。将所有的值放到双向链表中，这样，当访问到某个值时，将其移动到队尾的复杂度是`O(1)`，在队尾新增一条记录以及删除一条记录的复杂度均为`O(1)`。


```go
package lru

import "container/list"

// Cache is a LRU cache. It is not safe for concurrent access.
type Cache struct {
	maxBytes int64
	nbytes   int64
	ll       *list.List
	cache    map[string]*list.Element
	// optional and executed when an entry is purged.
	OnEvicted func(key string, value Value)
}

type entry struct {
	key   string
	value Value
}

// Value use Len to count how many bytes it takes
type Value interface {
	Len() int
}
```

- 在这里我们直接使用 Go 语言标准库实现的双向链表`list.List`。
- 字典的定义是 `map[string]*list.Element`，键是字符串，值是双向链表中对应节点的指针。
- `maxBytes` 是允许使用的最大内存，`nbytes` 是当前已使用的内存，`OnEvicted` 是某条记录被移除时的回调函数，可以为 nil。
- 键值对 `entry` 是双向链表节点的数据类型，在链表中仍保存每个值对应的 key 的好处在于，淘汰队首节点时，需要用 key 从字典中删除对应的映射。
- 为了通用性，我们允许值是实现了 `Value` 接口的任意类型，该接口只包含了一个方法 `Len() int`，用于返回值所占用的内存大小。

- 一致性哈希(consistent hashing)的原理以及为什么要使用一致性哈希。
- 实现一致性哈希代码，添加相应的测试用例，**代码约60行**

## 一致性哈希

对于分布式缓存来说，当一个节点接收到请求，如果该节点并没有存储缓存值，那么它面临的难题是，从谁那获取数据？自己，还是节点1, 2, 3, 4... 。假设包括自己在内一共有 10 个节点，当一个节点接收到请求时，随机选择一个节点，由该节点从数据源获取数据。

假设第一次随机选取了节点 1 ，节点 1 从数据源获取到数据的同时缓存该数据；那第二次，只有 1/10 的可能性再次选择节点 1, 有 9/10 的概率选择了其他节点，如果选择了其他节点，就意味着需要再一次从数据源获取数据，一般来说，这个操作是很耗时的。这样做，一是缓存效率低，二是各个节点上存储着相同的数据，浪费了大量的存储空间。

那有什么办法，对于给定的 key，每一次都选择同一个节点呢？使用 hash 算法也能够做到这一点。那把 key 的每一个字符的 ASCII 码加起来，再除以 10 取余数可以吗？当然可以，这可以认为是自定义的 hash 算法。

### 1.2 节点数量变化了怎么办？

简单求取 Hash 值解决了缓存性能的问题，但是没有考虑节点数量变化的场景。假设，移除了其中一台节点，只剩下 9 个，那么之前 `hash(key) % 10` 变成了 `hash(key) % 9`，也就意味着几乎缓存值对应的节点都发生了改变。即几乎所有的缓存值都失效了。节点在接收到对应的请求时，均需要重新去数据源获取数据，容易引起 `缓存雪崩`。

> 缓存雪崩：缓存在同一时刻全部失效，造成瞬时DB请求量大、压力骤增，引起雪崩。常因为缓存服务器宕机，或缓存设置了相同的过期时间引起。

那如何解决这个问题呢？一致性哈希算法可以。

## 2 算法原理

### 2.1 步骤

一致性哈希算法将 key 映射到 2^32 的空间中，将这个数字首尾相连，形成一个环。

- 计算节点/机器(通常使用节点的名称、编号和 IP 地址)的哈希值，放置在环上。
- 计算 key 的哈希值，放置在环上，顺时针寻找到的第一个节点，就是应选取的节点/机器。


也就是说，一致性哈希算法，在新增/删除节点时，只需要重新定位该节点附近的一小部分数据，而不需要重新定位所有的节点，这就解决了上述的问题。

### 2.2 数据倾斜问题

如果服务器的节点过少，容易引起 key 的倾斜。例如上面例子中的 peer2，peer4，peer6 分布在环的上半部分，下半部分是空的。那么映射到环下半部分的 key 都会被分配给 peer2，key 过度向 peer2 倾斜，缓存节点间负载不均。

为了解决这个问题，引入了虚拟节点的概念，一个真实节点对应多个虚拟节点。

假设 1 个真实节点对应 3 个虚拟节点，那么 peer1 对应的虚拟节点是  peer1-1、 peer1-2、 peer1-3（通常以添加编号的方式实现），其余节点也以相同的方式操作。

- 第一步，计算虚拟节点的 Hash 值，放置在环上。
- 第二步，计算 key 的 Hash 值，在环上顺时针寻找到应选取的虚拟节点，例如是 peer2-1，那么就对应真实节点 peer2。

虚拟节点扩充了节点的数量，解决了节点较少的情况下数据容易倾斜的问题。而且代价非常小，只需要增加一个字典(map)维护真实节点与虚拟节点的映射关系即可。


## 1 缓存雪崩、缓存击穿与缓存穿透

[GeeCache 第五天](https://geektutu.com/post/geecache-day5.html) 提到了缓存雪崩和缓存击穿，在这里做下总结：

> **缓存雪崩**：缓存在同一时刻全部失效，造成瞬时DB请求量大、压力骤增，引起雪崩。缓存雪崩通常因为缓存服务器宕机、缓存的 key 设置了相同的过期时间等引起。

> **缓存击穿**：一个存在的key，在缓存过期的一刻，同时有大量的请求，这些请求都会击穿到 DB ，造成瞬时DB请求量大、压力骤增。

> **缓存穿透**：查询一个不存在的数据，因为不存在则不会写到缓存中，所以每次都会去请求 DB，如果瞬间流量过大，穿透到 DB，导致宕机。

## Singleflight
通俗的来说就是 singleflight将相同的并发请求合并成一个请求，进而减少对下层服务的压力，通常用于解决缓存击穿的问题。

- Do 方法，接收 2 个参数，第一个参数是 `key`，第二个参数是一个函数 `fn`。Do 的作用就是，针对相同的 key，无论 Do 被调用多少次，函数 `fn` 都只会被调用一次，等待 fn 调用结束了，返回返回值或错误。

`g.mu` 是保护 Group 的成员变量 `m` 不被并发读写而加上的锁。为了便于理解 `Do` 函数，我们将 `g.mu` 暂时去掉。并且把 `g.m` 延迟初始化的部分去掉，延迟初始化的目的很简单，提高内存使用效率。


```go
func (g *Group) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
	if c, ok := g.m[key]; ok {
		c.wg.Wait()   // 如果请求正在进行中，则等待
		return c.val, c.err  // 请求结束，返回结果
	}
	c := new(call)
	c.wg.Add(1)       // 发起请求前加锁
	g.m[key] = c      // 添加到 g.m，表明 key 已经有对应的请求在处理

	c.val, c.err = fn() // 调用 fn，发起请求
	c.wg.Done()         // 请求结束

    delete(g.m, key)    // 更新 g.m
    
	return c.val, c.err // 返回结果
}
```

