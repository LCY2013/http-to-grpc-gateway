## 生成 proto 文件信息

```shell
protoc -I ./ --go_out=./ ./proto/helloworld/helloworld.proto

protoc -I ./ --go_out=plugins=grpc:proto/helloworld ./proto/helloworld/helloworld.proto 

protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    ./proto/helloworld/helloworld.proto
```

## [项目来源grpcurl](https://github.com/fullstorydev/grpcurl)
