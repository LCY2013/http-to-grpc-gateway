// Command gateway makes gRPC requests (a la cURL, but HTTP/2). It can use a supplied descriptor
// file, protobuf sources, or service config.Reflection to translate JSON or text request data into the
// appropriate protobuf messages and vice versa for presenting the response contents.
package main

import (
	"context"
	"fmt"
	gateway "github.com/LCY2013/http-to-grpc-gateway"
	"github.com/LCY2013/http-to-grpc-gateway/internal/config"
	"github.com/LCY2013/http-to-grpc-gateway/internal/server"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/descriptorpb"

	// Register gzip compressor so compressed responses will work
	_ "google.golang.org/grpc/encoding/gzip"
	// Register xds so xds and xds-experimental resolver schemes work
	_ "google.golang.org/grpc/xds"
)

func main() {
	config.Flags.Usage = config.Usage
	config.Flags.Parse(os.Args[1:])
	if *config.Help {
		config.Usage()
		os.Exit(0)
	}
	if *config.PrintVersion {
		fmt.Fprintf(os.Stderr, "%s %s\n", filepath.Base(os.Args[0]), config.Version)
		os.Exit(0)
	}

	// Do extra validation on arguments and figure out what user asked us to do.
	if *config.ConnectTimeout < 0 {
		fail(nil, "The -connect-timeout argument must not be negative.")
	}
	if *config.KeepaliveTime < 0 {
		fail(nil, "The -keepalive-time argument must not be negative.")
	}
	if *config.MaxTime < 0 {
		fail(nil, "The -max-time argument must not be negative.")
	}
	if *config.MaxMsgSz < 0 {
		fail(nil, "The -max-msg-sz argument must not be negative.")
	}
	if *config.Plaintext && *config.Insecure {
		fail(nil, "The -config.Plaintext and -insecure arguments are mutually exclusive.")
	}
	if *config.Plaintext && *config.Cert != "" {
		fail(nil, "The -config.Plaintext and -cert arguments are mutually exclusive.")
	}
	if *config.Plaintext && *config.Key != "" {
		fail(nil, "The -config.Plaintext and -key arguments are mutually exclusive.")
	}
	if (*config.Key == "") != (*config.Cert == "") {
		fail(nil, "The -cert and -key arguments must be used together and both be present.")
	}
	if *config.Format != "json" && *config.Format != "text" {
		fail(nil, "The -format option must be 'json' or 'text'.")
	}
	if *config.EmitDefaults && *config.Format != "json" {
		warn("The -emit-defaults is only used when using json format.")
	}

	args := config.Flags.Args()

	if len(args) == 0 {
		fail(nil, "Too few arguments.")
	}
	var target string
	if args[0] != "list" && args[0] != "describe" && args[0] != "server" {
		target = args[0]
		args = args[1:]
	}

	if len(args) == 0 {
		fail(nil, "Too few arguments.")
	}

	verbosityLevel := 0
	if *config.Verbose {
		verbosityLevel = 1
	}
	if *config.VeryVerbose {
		verbosityLevel = 2
	}

	// Protoset or protofiles provided and -use-config.Reflection unset
	if !config.Reflection.SetOp && (len(config.Protoset) > 0 || len(config.ProtoFiles) > 0) {
		config.Reflection.Val = false
	}

	var list, describe, invokeCmd bool
	if args[0] == "list" {
		list = true
		args = args[1:]
	} else if args[0] == "describe" {
		describe = true
		args = args[1:]
	} else if args[0] == "server" {
		args = args[1:]
		server.Run(args)
	} else {
		invokeCmd = true
	}

	cmd(args, list, describe, invokeCmd, target, verbosityLevel)
}

func cmd(args []string, list, describe, invoke bool, target string, verbosityLevel int) {
	var symbol string
	if invoke {
		if len(args) == 0 {
			fail(nil, "Too few arguments.")
		}
		symbol = args[0]
		args = args[1:]
	} else {
		if *config.Data != "" {
			warn("The -d argument is not used with 'list' or 'describe' verb.")
		}
		if len(config.RpcHeaders) > 0 {
			warn("The -rpc-header argument is not used with 'list' or 'describe' verb.")
		}
		if len(args) > 0 {
			symbol = args[0]
			args = args[1:]
		}
	}

	if len(args) > 0 {
		fail(nil, "Too many arguments.")
	}
	if invoke && target == "" {
		fail(nil, "No host:port specified.")
	}
	if len(config.Protoset) == 0 && len(config.ProtoFiles) == 0 && target == "" {
		fail(nil, "No host:port specified, no config.Protoset specified, and no proto sources specified.")
	}
	if len(config.Protoset) > 0 && len(config.ReflHeaders) > 0 {
		warn("The -reflect-header argument is not used when -config.Protoset files are used.")
	}
	if len(config.Protoset) > 0 && len(config.ProtoFiles) > 0 {
		fail(nil, "Use either -config.Protoset files or -proto files, but not both.")
	}
	if len(config.ImportPaths) > 0 && len(config.ProtoFiles) == 0 {
		warn("The -import-path argument is not used unless -proto files are used.")
	}
	if !config.Reflection.Val && len(config.Protoset) == 0 && len(config.ProtoFiles) == 0 {
		fail(nil, "No config.Protoset files or proto files specified and -use-config.Reflection set to false.")
	}

	ctx := context.Background()
	if *config.MaxTime > 0 {
		timeout := time.Duration(*config.MaxTime * float64(time.Second))
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	dial := func() *grpc.ClientConn {
		dialTime := 10 * time.Second
		if *config.ConnectTimeout > 0 {
			dialTime = time.Duration(*config.ConnectTimeout * float64(time.Second))
		}
		ctx, cancel := context.WithTimeout(ctx, dialTime)
		defer cancel()
		var opts []grpc.DialOption
		if *config.KeepaliveTime > 0 {
			timeout := time.Duration(*config.KeepaliveTime * float64(time.Second))
			opts = append(opts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:    timeout,
				Timeout: timeout,
			}))
		}
		if *config.MaxMsgSz > 0 {
			opts = append(opts, grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(*config.MaxMsgSz)))
		}
		var creds credentials.TransportCredentials
		if !*config.Plaintext {
			tlsConf, err := gateway.ClientTLSConfig(*config.Insecure, *config.Cacert, *config.Cert, *config.Key)
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

			// can use either -servername or -config.Authority; but not both
			if *config.ServerName != "" && *config.Authority != "" {
				if *config.ServerName == *config.Authority {
					warn("Both -servername and -config.Authority are present; prefer only -config.Authority.")
				} else {
					fail(nil, "Cannot specify different values for -servername and -config.Authority.")
				}
			}
			overrideName := *config.ServerName
			if overrideName == "" {
				overrideName = *config.Authority
			}

			if overrideName != "" {
				opts = append(opts, grpc.WithAuthority(overrideName))
			}
		} else if *config.Authority != "" {
			opts = append(opts, grpc.WithAuthority(*config.Authority))
		}

		grpcurlUA := "http-to-grpc-gateway/" + config.Version
		if config.Version == config.NoVersion {
			grpcurlUA = "http-to-grpc-gateway/dev-build (no version set)"
		}
		if *config.UserAgent != "" {
			grpcurlUA = *config.UserAgent + " " + grpcurlUA
		}
		opts = append(opts, grpc.WithUserAgent(grpcurlUA))

		network := "tcp"
		if config.IsUnixSocket != nil && config.IsUnixSocket() {
			network = "unix"
		}
		cc, err := gateway.BlockingDial(ctx, network, target, creds, opts...)
		if err != nil {
			fail(err, "Failed to dial target host %q", target)
		}
		return cc
	}
	printFormattedStatus := func(w io.Writer, stat *status.Status, formatter gateway.Formatter) {
		formattedStatus, err := formatter(stat.Proto())
		if err != nil {
			fmt.Fprintf(w, "ERROR: %v", err.Error())
		}
		fmt.Fprint(w, formattedStatus)
	}

	if *config.ExpandHeaders {
		var err error
		config.AddlHeaders, err = gateway.ExpandHeaders(config.AddlHeaders)
		if err != nil {
			fail(err, "Failed to expand additional headers")
		}
		config.RpcHeaders, err = gateway.ExpandHeaders(config.RpcHeaders)
		if err != nil {
			fail(err, "Failed to expand rpc headers")
		}
		config.ReflHeaders, err = gateway.ExpandHeaders(config.ReflHeaders)
		if err != nil {
			fail(err, "Failed to expand config.Reflection headers")
		}
	}

	var cc *grpc.ClientConn
	var descSource gateway.DescriptorSource
	var refClient *grpcreflect.Client
	var fileSource gateway.DescriptorSource
	if len(config.Protoset) > 0 {
		var err error
		fileSource, err = gateway.DescriptorSourceFromProtoSets(config.Protoset...)
		if err != nil {
			fail(err, "Failed to process proto descriptor sets.")
		}
	} else if len(config.ProtoFiles) > 0 {
		var err error
		fileSource, err = gateway.DescriptorSourceFromProtoFiles(config.ImportPaths, config.ProtoFiles...)
		if err != nil {
			fail(err, "Failed to process proto source files.")
		}
	}
	if config.Reflection.Val {
		md := gateway.MetadataFromHeaders(append(config.AddlHeaders, config.ReflHeaders...))
		refCtx := metadata.NewOutgoingContext(ctx, md)
		cc = dial()
		refClient = grpcreflect.NewClientV1Alpha(refCtx, reflectpb.NewServerReflectionClient(cc))
		reflSource := gateway.DescriptorSourceFromServer(ctx, refClient)
		if fileSource != nil {
			descSource = config.CompositeSource{Reflection: reflSource, File: fileSource}
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
	config.Exit = func(code int) {
		// since defers aren't run by os.Exit...
		reset()
		os.Exit(code)
	}

	if list {
		if symbol == "" {
			svcs, err := gateway.ListServices(descSource)
			if err != nil {
				fail(err, "Failed to list services")
			}
			if len(svcs) == 0 {
				fmt.Println("(No services)")
			} else {
				for _, svc := range svcs {
					fmt.Printf("%s\n", svc)
				}
			}
			if err := writeProtoset(descSource, svcs...); err != nil {
				fail(err, "Failed to write config.Protoset to %s", *config.ProtosetOut)
			}
		} else {
			methods, err := gateway.ListMethods(descSource, symbol)
			if err != nil {
				fail(err, "Failed to list methods for service %q", symbol)
			}
			if len(methods) == 0 {
				fmt.Println("(No methods)") // probably unlikely
			} else {
				for _, m := range methods {
					fmt.Printf("%s\n", m)
				}
			}
			if err := writeProtoset(descSource, symbol); err != nil {
				fail(err, "Failed to write config.Protoset to %s", *config.ProtosetOut)
			}
		}

	} else if describe {
		var symbols []string
		if symbol != "" {
			symbols = []string{symbol}
		} else {
			// if no symbol given, describe all exposed services
			svcs, err := descSource.ListServices()
			if err != nil {
				fail(err, "Failed to list services")
			}
			if len(svcs) == 0 {
				fmt.Println("Server returned an empty list of exposed services")
			}
			symbols = svcs
		}
		for _, s := range symbols {
			if s[0] == '.' {
				s = s[1:]
			}

			dsc, err := descSource.FindSymbol(s)
			if err != nil {
				fail(err, "Failed to resolve symbol %q", s)
			}

			fqn := dsc.GetFullyQualifiedName()
			var elementType string
			switch d := dsc.(type) {
			case *desc.MessageDescriptor:
				elementType = "a message"
				parent, ok := d.GetParent().(*desc.MessageDescriptor)
				if ok {
					if d.IsMapEntry() {
						for _, f := range parent.GetFields() {
							if f.IsMap() && f.GetMessageType() == d {
								// found it: describe the map field instead
								elementType = "the entry type for a map field"
								dsc = f
								break
							}
						}
					} else {
						// see if it's a group
						for _, f := range parent.GetFields() {
							if f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_GROUP && f.GetMessageType() == d {
								// found it: describe the map field instead
								elementType = "the type of a group field"
								dsc = f
								break
							}
						}
					}
				}
			case *desc.FieldDescriptor:
				elementType = "a field"
				if d.GetType() == descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					elementType = "a group field"
				} else if d.IsExtension() {
					elementType = "an extension"
				}
			case *desc.OneOfDescriptor:
				elementType = "a one-of"
			case *desc.EnumDescriptor:
				elementType = "an enum"
			case *desc.EnumValueDescriptor:
				elementType = "an enum value"
			case *desc.ServiceDescriptor:
				elementType = "a service"
			case *desc.MethodDescriptor:
				elementType = "a method"
			default:
				err = fmt.Errorf("descriptor has unrecognized type %T", dsc)
				fail(err, "Failed to describe symbol %q", s)
			}

			txt, err := gateway.GetDescriptorText(dsc, descSource)
			if err != nil {
				fail(err, "Failed to describe symbol %q", s)
			}
			fmt.Printf("%s is %s:\n", fqn, elementType)
			fmt.Println(txt)

			if dsc, ok := dsc.(*desc.MessageDescriptor); ok && *config.MsgTemplate {
				// for messages, also show a template in JSON, to make it easier to
				// create a request to invoke an RPC
				tmpl := gateway.MakeTemplate(dsc)
				options := gateway.FormatOptions{EmitJSONDefaultFields: true}
				_, formatter, err := gateway.RequestParserAndFormatter(gateway.Format(*config.Format), descSource, nil, options)
				if err != nil {
					fail(err, "Failed to construct formatter for %q", *config.Format)
				}
				str, err := formatter(tmpl)
				if err != nil {
					fail(err, "Failed to print template for message %s", s)
				}
				fmt.Println("\nMessage template:")
				fmt.Println(str)
			}
		}
		if err := writeProtoset(descSource, symbols...); err != nil {
			fail(err, "Failed to write config.Protoset to %s", *config.ProtosetOut)
		}

	} else {
		// Invoke an RPC
		if cc == nil {
			cc = dial()
		}
		var in io.Reader
		if *config.Data == "@" {
			in = os.Stdin
		} else {
			in = strings.NewReader(*config.Data)
		}

		// if not verbose output, then also include record delimiters
		// between each message, so output could potentially be piped
		// to another gateway process
		includeSeparators := verbosityLevel == 0
		options := gateway.FormatOptions{
			EmitJSONDefaultFields: *config.EmitDefaults,
			IncludeTextSeparator:  includeSeparators,
			AllowUnknownFields:    *config.AllowUnknownFields,
		}
		rf, formatter, err := gateway.RequestParserAndFormatter(gateway.Format(*config.Format), descSource, in, options)
		if err != nil {
			fail(err, "Failed to construct request parser and formatter for %q", *config.Format)
		}
		h := &gateway.DefaultEventHandler{
			Out:            os.Stdout,
			Formatter:      formatter,
			VerbosityLevel: verbosityLevel,
		}

		err = gateway.InvokeRPC(ctx, descSource, cc, symbol, append(config.AddlHeaders, config.RpcHeaders...), h, rf.Next)
		if err != nil {
			if errStatus, ok := status.FromError(err); ok && *config.FormatError {
				h.Status = errStatus
			} else {
				fail(err, "Error invoking method %q", symbol)
			}
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
			if *config.FormatError {
				printFormattedStatus(os.Stderr, h.Status, formatter)
			} else {
				gateway.PrintStatus(os.Stderr, h.Status, formatter)
			}
			config.Exit(config.StatusCodeOffset + int(h.Status.Code()))
		}
	}
}

func info(msg string, args ...interface{}) {
	msg = fmt.Sprintf("Info: %s\n", msg)
	fmt.Fprintf(os.Stdout, msg, args...)
}

func warn(msg string, args ...interface{}) {
	msg = fmt.Sprintf("Warning: %s\n", msg)
	fmt.Fprintf(os.Stderr, msg, args...)
}

func fail(err error, msg string, args ...interface{}) {
	if err != nil {
		msg += ": %v"
		args = append(args, err)
	}
	fmt.Fprintf(os.Stderr, msg, args...)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		config.Exit(1)
	} else {
		// nil error means it was CLI usage issue
		fmt.Fprintf(os.Stderr, "Try '%s -help' for more details.\n", os.Args[0])
		config.Exit(2)
	}
}

func writeProtoset(descSource gateway.DescriptorSource, symbols ...string) error {
	if *config.ProtosetOut == "" {
		return nil
	}
	f, err := os.Create(*config.ProtosetOut)
	if err != nil {
		return err
	}
	defer f.Close()
	return gateway.WriteProtoset(f, descSource, symbols...)
}
