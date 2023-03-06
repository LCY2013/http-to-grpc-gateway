package main

import (
	"context"
	"fmt"
	"github.com/LCY2013/http-to-grpc-gateway/proto/helloworld"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// 连接服务器
	//conn, err := grpc.Dial(":8080", grpc.WithInsecure())
	conn, err := grpc.Dial(":8080", grpc.WithTransportCredentials(insecure.NewCredentials()))

	if err != nil {
		fmt.Printf("连接服务端失败: %s", err)
		return
	}
	defer conn.Close()
	// 新建一个客户端
	c := helloworld.NewGreeterClient(conn)
	// 调用服务端函数
	r, err := c.SayHello(context.Background(), &helloworld.HelloRequest{Name: "hello"})
	if err != nil {
		fmt.Printf("调用服务端代码失败: %s", err)
		return
	}

	fmt.Printf("调用成功: %s", r.Message)
}
