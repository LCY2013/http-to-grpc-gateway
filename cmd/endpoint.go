package main

import (
	"context"
	"fmt"
	grpcurl "github.com/LCY2013/http-to-grpc-gateway"
	"github.com/LCY2013/http-to-grpc-gateway/internal/ack"
	"github.com/LCY2013/http-to-grpc-gateway/internal/logger"
	"github.com/LCY2013/http-to-grpc-gateway/internal/registry"
	httpReg "github.com/LCY2013/http-to-grpc-gateway/internal/registry/http"
	"github.com/LCY2013/http-to-grpc-gateway/internal/util/async"
	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"log"
	"net/http"
	"strings"
	"time"
)

func Server(args []string) {
	// 创建一个监听8080端口的服务器
	err := http.ListenAndServe(":8080", registerWithServe(args[0]))
	if err != nil {
		log.Fatal(err)
		return
	}
}

func registerWithServe(registryType string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		var register registry.Register
		switch registryType {
		case "http":
			register = httpReg.NewRegisterHttp(request)
		}

		done := make(chan error)

		// do business
		async.GO(func() {
			conn, dialErr := dial(ctx, register)
			if dialErr != nil {
				err := dialErr
				_, _ = writer.Write([]byte(err.Error()))
				return
			}

			r, dialErr := register.Register()
			if dialErr != nil {
				err := dialErr
				_, _ = writer.Write([]byte(err.Error()))
				return
			}

			done <- invoke(ctx, request, writer, conn, r)
		})

		// 通过select监听多个channel
		select {
		case <-time.After(30 * time.Second):
			// 如果两秒后接受到了一个消息后，意味请求已经处理完成
			// 我们写入"request processed"作为响应
			_, err := writer.Write([]byte(ack.ToFailResponse("request timeout")))
			if err != nil {
				return
			}
		case res := <-ctx.Done():
			// 如果处理完成前取消了，在STDERR中记录请求被取消的消息
			logger.Errorf("request cancelled: %s\n", res)
			_, err := writer.Write([]byte(ack.ToFailResponse("request cancelled")))
			if err != nil {
				return
			}
		case err := <-done:
			if err != nil {
				_, err = writer.Write([]byte(ack.ToFailResponse(err.Error())))
			}
		}
	}
}

func dial(ctx context.Context, register registry.Register) (*grpc.ClientConn, error) {
	dialTime := 10 * time.Second
	if *connectTimeout > 0 {
		dialTime = time.Duration(*connectTimeout * float64(time.Second))
	}

	ctx, cancel := context.WithTimeout(ctx, dialTime)
	defer cancel()

	var opts []grpc.DialOption
	if *keepaliveTime > 0 {
		timeout := time.Duration(*keepaliveTime * float64(time.Second))
		opts = append(opts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    timeout,
			Timeout: timeout,
		}))
	}

	if *maxMsgSz > 0 {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(*maxMsgSz)))
	}

	UA := "github.com/LCY2013/http-to-grpc-gateway/" + version
	if version == noVersion {
		UA = "github.com/LCY2013/http-to-grpc-gateway/dev-build (no version set)"
	}
	if *userAgent != "" {
		UA = *userAgent + " " + UA
	}
	opts = append(opts, grpc.WithUserAgent(UA))

	network := "tcp"
	if isUnixSocket != nil && isUnixSocket() {
		network = "unix"
	}

	registry, err := register.Register()
	if err != nil {
		logger.Errorf("Failed to get register %+v: %+v", register, err)
		return nil, err
	}

	cc, err := grpcurl.BlockingDial(ctx, network, registry.Addr, nil, opts...)
	if err != nil {
		logger.Errorf("Failed to dial target host %q: %+v", registry.Addr, err)
		return nil, err
	}
	return cc, nil
}

func invoke(ctx context.Context, req *http.Request, writer http.ResponseWriter, cc *grpc.ClientConn, registry *registry.Registry) error {
	// Invoke an RPC
	if cc == nil {
		return nil
	}

	verbosityLevel := 0
	if *verbose {
		verbosityLevel = 1
	}
	if *veryVerbose {
		verbosityLevel = 2
	}

	var descSource grpcurl.DescriptorSource
	var refClient *grpcreflect.Client
	var fileSource grpcurl.DescriptorSource
	if len(protoset) > 0 {
		var err error
		fileSource, err = grpcurl.DescriptorSourceFromProtoSets(protoset...)
		if err != nil {
			logger.Errorf("%+v Failed to process proto descriptor sets.", err)
			fmt.Fprintf(writer, ack.ToFailResponse(err.Error()))
			return nil
		}
	} else if len(protoFiles) > 0 {
		var err error
		fileSource, err = grpcurl.DescriptorSourceFromProtoFiles(importPaths, protoFiles...)
		if err != nil {
			logger.Errorf("%+v Failed to process proto source files.", err)
			fmt.Fprintf(writer, ack.ToFailResponse(err.Error()))
			return nil
		}
	}
	if reflection.val {
		md := grpcurl.MetadataFromHeaders(append(addlHeaders, reflHeaders...))
		refCtx := metadata.NewOutgoingContext(ctx, md)
		refClient = grpcreflect.NewClientV1Alpha(refCtx, reflectpb.NewServerReflectionClient(cc))
		reflSource := grpcurl.DescriptorSourceFromServer(ctx, refClient)
		if fileSource != nil {
			descSource = compositeSource{reflSource, fileSource}
		} else {
			descSource = reflSource
		}
	} else {
		descSource = fileSource
	}

	// arrange for the RPCs to be cleanly shutdown
	reset := func() {
		if refClient != nil {
			refClient.Reset()
			refClient = nil
		}
		if cc != nil {
			cc.Close()
			cc = nil
		}
	}
	defer reset()

	// if not verbose output, then also include record delimiters
	// between each message, so output could potentially be piped
	// to another grpcurl process
	includeSeparators := verbosityLevel == 0
	options := grpcurl.FormatOptions{
		EmitJSONDefaultFields: *emitDefaults,
		IncludeTextSeparator:  includeSeparators,
		AllowUnknownFields:    *allowUnknownFields,
	}

	rf, formatter, err := grpcurl.RequestParserAndFormatter(grpcurl.Format(*format), descSource, req.Body, options)
	if err != nil {
		logger.Errorf("%+v Failed to construct request parser and formatter for %q", err, *format)
		fmt.Fprintf(writer, ack.ToFailResponse(err.Error()))
		return nil
	}
	h := &grpcurl.DefaultEventHandler{
		Out:            writer,
		Formatter:      formatter,
		VerbosityLevel: verbosityLevel,
	}

	switch grpcurl.Format(*format) {
	case grpcurl.FormatJSON:
		writer.Header().Add("Content-Type", "application/json; charset=utf-8")
	case grpcurl.FormatText:
		writer.Header().Add("Content-Type", "text/plain; charset=utf-8")
	default:
		for k, v := range req.Header {
			if v != nil && len(v) > 0 {
				writer.Header().Add(k, v[0])
			}
		}
	}

	rpcHeader := append(addlHeaders, rpcHeaders...)
	for k, v := range req.Header {
		if strings.ToTitle(k) != "X-" {
			continue
		}
		if v != nil && len(v) > 0 {
			rpcHeader = append(rpcHeader, k+":"+v[0])
		}
	}

	err = grpcurl.InvokeRPC(ctx, descSource, cc, registry.Method, rpcHeader, h, rf.Next)
	if err != nil {
		logger.Errorf("%+v Error invoking method %q", err, registry.Method)
		fmt.Fprintf(writer, ack.ToFailResponse(err.Error()))
		return nil
	}
	reqSuffix := ""
	respSuffix := ""
	reqCount := rf.NumRequests()
	if reqCount != 1 {
		reqSuffix = "s"
	}
	if h.NumResponses != 1 {
		respSuffix = "s"
	}
	if verbosityLevel > 0 {
		logger.Infof("Sent %d request%s and received %d response%s\n", reqCount, reqSuffix, h.NumResponses, respSuffix)
	}
	if h.Status.Code() != codes.OK {
		if *formatError {
			grpcurl.PrintStatus(writer, h.Status, formatter)
		} else {
			grpcurl.PrintStatus(writer, h.Status, formatter)
		}
	}

	return nil
}
