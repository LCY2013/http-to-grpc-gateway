package main

import (
	"context"
	"fmt"
	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	grpcurl "http-to-grpc-gateway"
	"http-to-grpc-gateway/internal/registry"
	httpReg "http-to-grpc-gateway/internal/registry/http"
	"http-to-grpc-gateway/internal/util/async"
	"log"
	"net/http"
	"os"
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
			_, err := writer.Write([]byte("request timeout"))
			if err != nil {
				return
			}
		case res := <-ctx.Done():
			// 如果处理完成前取消了，在STDERR中记录请求被取消的消息
			_, err := fmt.Fprintf(os.Stderr, "request cancelled: %s\n", res)
			if err != nil {
				return
			}
		case err := <-done:
			if err != nil {
				_, err = writer.Write([]byte(err.Error()))
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

	var creds credentials.TransportCredentials
	if !*plaintext {
		tlsConf, err := grpcurl.ClientTLSConfig(*insecure, *cacert, *cert, *key)
		if err != nil {
			fail(err, "Failed to create TLS config")
		}

		sslKeylogFile := os.Getenv("SSLKEYLOGFILE")
		if sslKeylogFile != "" {
			w, err := os.OpenFile(sslKeylogFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
			if err != nil {
				fail(err, "Could not open SSLKEYLOGFILE %s", sslKeylogFile)
			}
			tlsConf.KeyLogWriter = w
		}

		creds = credentials.NewTLS(tlsConf)

		// can use either -servername or -authority; but not both
		if *serverName != "" && *authority != "" {
			if *serverName == *authority {
				warn("Both -servername and -authority are present; prefer only -authority.")
			} else {
				fail(nil, "Cannot specify different values for -servername and -authority.")
			}
		}
		overrideName := *serverName
		if overrideName == "" {
			overrideName = *authority
		}

		if overrideName != "" {
			opts = append(opts, grpc.WithAuthority(overrideName))
		}
	} else if *authority != "" {
		opts = append(opts, grpc.WithAuthority(*authority))
	}

	UA := "http-to-grpc-gateway/" + version
	if version == noVersion {
		UA = "http-to-grpc-gateway/dev-build (no version set)"
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
		fmt.Fprintf(os.Stderr, "Failed to get register %+v: %+v", register, err)
		return nil, err
	}

	creds = nil

	cc, err := grpcurl.BlockingDial(ctx, network, registry.Addr, creds, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to dial target host %q: %+v", registry.Addr, err)
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
			fmt.Fprintf(os.Stderr, "%+v Failed to process proto descriptor sets.", err)
			return err
		}
	} else if len(protoFiles) > 0 {
		var err error
		fileSource, err = grpcurl.DescriptorSourceFromProtoFiles(importPaths, protoFiles...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%+v Failed to process proto source files.", err)
			return err
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
		fmt.Fprintf(os.Stderr, "%+v Failed to construct request parser and formatter for %q", err, *format)
		return err
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
		fmt.Fprintf(os.Stderr, "%+v Error invoking method %q", err, registry.Method)
		fmt.Fprintf(writer, "")
		return err
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
		fmt.Printf("Sent %d request%s and received %d response%s\n", reqCount, reqSuffix, h.NumResponses, respSuffix)
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
