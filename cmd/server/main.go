package main

import (
	"fmt"
	"github.com/LCY2013/http-to-grpc-gateway/proto/helloworld"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"net"
)

func main() {
	// 监听本地端口
	lis, err := net.Listen("tcp", ":8081")
	if err != nil {
		fmt.Printf("监听端口失败: %s", err)
		return
	}

	// 创建gRPC服务器
	srv := grpc.NewServer()
	// 注册服务
	helloworld.RegisterGreeterServer(srv, &helloworld.HelloworldService{})

	reflection.Register(srv)

	err = srv.Serve(lis)
	if err != nil {
		fmt.Printf("开启服务失败: %s", err)
		return
	}
}
