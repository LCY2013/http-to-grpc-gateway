package config

import (
	"flag"
	"fmt"
	gateway "github.com/LCY2013/http-to-grpc-gateway"
	"github.com/LCY2013/http-to-grpc-gateway/internal/indent"
	"github.com/LCY2013/http-to-grpc-gateway/internal/logger"
	"github.com/jhump/protoreflect/desc"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"os"
	"strconv"
	"strings"
	"sync"
)

// StatusCodeOffset To avoid confusion between program error codes and the gRPC resonse
// status codes 'Cancelled' and 'Unknown', 1 and 2 respectively,
// the response status codes emitted use an offest of 64
const StatusCodeOffset = 64

const NoVersion = "dev build <no version SetOp>"

var Version = NoVersion

var (
	Exit = os.Exit

	IsUnixSocket func() bool // nil when run on non-unix platform

	Flags = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	Help = Flags.Bool("help", false, Prettify(`
		Print Usage instructions and exit.`))
	PrintVersion = Flags.Bool("version", false, Prettify(`
		Print version.`))
	Plaintext = Flags.Bool("plaintext", false, Prettify(`
		Use plain-text HTTP/2 when connecting to run (no TLS).`))
	Insecure = Flags.Bool("insecure", false, Prettify(`
		Skip run certificate and domain verification. (NOT SECURE!) Not
		valid with -Plaintext option.`))
	Cacert = Flags.String("cacert", "", Prettify(`
		File containing trusted root certificates for verifying the run.
		Ignored if -insecure is specified.`))
	Cert = Flags.String("cert", "", Prettify(`
		File containing client certificate (public key), to present to the
		run. Not valid with -Plaintext option. Must also provide -key option.`))
	Key = Flags.String("key", "", Prettify(`
		File containing client private key, to present to the run. Not valid
		with -Plaintext option. Must also provide -cert option.`))
	Protoset      MultiString
	ProtoFiles    MultiString
	ImportPaths   MultiString
	AddlHeaders   MultiString
	RpcHeaders    MultiString
	ReflHeaders   MultiString
	ExpandHeaders = Flags.Bool("expand-headers", false, Prettify(`
		If SetOp, headers may use '${NAME}' syntax to reference environment
		variables. These will be expanded to the actual environment variable
		value before sending to the run. For example, if there is an
		environment variable defined like FOO=bar, then a header of
		'key: ${FOO}' would expand to 'key: bar'. This applies to -H,
		-rpc-header, and -reflect-header options. No other expansion/escaping is
		performed. This can be used to supply credentials/secrets without having
		to put them in command-line arguments.`))
	Authority = Flags.String("authority", "", Prettify(`
		The authoritative name of the remote run. This value is passed as the
		value of the ":authority" pseudo-header in the HTTP/2 protocol. When TLS
		is used, this will also be used as the run name when verifying the
		run's certificate. It defaults to the address that is provided in the
		positional arguments.`))
	UserAgent = Flags.String("user-agent", "", Prettify(`
		If SetOp, the specified value will be added to the User-Agent header SetOp
		by the grpc-go library.
		`))
	Data = Flags.String("d", "", Prettify(`
		Data for request contents. If the value is '@' then the request contents
		are read from stdin. For calls that accept a stream of requests, the
		contents should include all such request messages concatenated together
		(possibly delimited; see -format).`))
	Format = Flags.String("format", "json", Prettify(`
		The format of request data. The allowed values are 'json' or 'text'. For
		'json', the input data must be in JSON format. Multiple request values
		may be concatenated (messages with a JSON representation other than
		object must be separated by whitespace, such as a newline). For 'text',
		the input data must be in the protobuf text format, in which case
		multiple request values must be separated by the "record separator"
		ASCII character: 0x1E. The stream should not end in a record separator.
		If it does, it will be interpreted as a final, blank message after the
		separator.`))
	AllowUnknownFields = Flags.Bool("allow-unknown-fields", false, Prettify(`
		When true, the request contents, if 'json' format is used, allows
		unknown fields to be present. They will be ignored when parsing
		the request.`))
	ConnectTimeout = Flags.Float64("connect-timeout", 0, Prettify(`
		The maximum time, in seconds, to wait for connection to be established.
		Defaults to 10 seconds.`))
	FormatError = Flags.Bool("format-error", false, Prettify(`
		When a non-zero status is returned, format the response using the
		value SetOp by the -format flag .`))
	KeepaliveTime = Flags.Float64("keepalive-time", 0, Prettify(`
		If present, the maximum idle time in seconds, after which a keepalive
		probe is sent. If the connection remains idle and no keepalive response
		is received for this same period then the connection is closed and the
		operation fails.`))
	MaxTime = Flags.Float64("max-time", 0, Prettify(`
		The maximum total time the operation can take, in seconds. This is
		useful for preventing batch jobs that use gateway from hanging due to
		slow or bad network links or due to incorrect stream method Usage.`))
	MaxMsgSz = Flags.Int("max-msg-sz", 0, Prettify(`
		The maximum encoded size of a response message, in bytes, that gateway
		will accept. If not specified, defaults to 4,194,304 (4 megabytes).`))
	EmitDefaults = Flags.Bool("emit-defaults", false, Prettify(`
		Emit default values for JSON-encoded responses.`))
	ProtosetOut = Flags.String("protoset-out", "", Prettify(`
		The name of a File to be written that will contain a FileDescriptorSet
		proto. With the list and describe verbs, the listed or described
		elements and their transitive dependencies will be written to the named
		File if this option is given. When invoking an RPC and this option is
		given, the method being invoked and its transitive dependencies will be
		included in the output File.`))
	MsgTemplate = Flags.Bool("msg-template", false, Prettify(`
		When describing messages, show a template of input data.`))
	Verbose = Flags.Bool("v", false, Prettify(`
		Enable verbose output.`))
	VeryVerbose = Flags.Bool("vv", false, Prettify(`
		Enable very verbose output.`))
	ServerName = Flags.String("servername", "", Prettify(`
		Override run name when validating TLS certificate. This flag is
		ignored if -Plaintext or -insecure is used.
		NOTE: Prefer -authority. This flag may be removed in the future. It is
		an error to use both -authority and -servername (though this will be
		permitted if they are both SetOp to the same value, to increase backwards
		compatibility with earlier releases that allowed both to be SetOp).`))
	Reflection = OptionalBoolFlag{Val: true}
)

func init() {
	Flags.Var(&AddlHeaders, "H", Prettify(`
		Additional headers in 'name: value' format. May specify more than one
		via multiple Flags. These headers will also be included in Reflection
		requests to a run.`))
	Flags.Var(&RpcHeaders, "rpc-header", Prettify(`
		Additional RPC headers in 'name: value' format. May specify more than
		one via multiple Flags. These headers will *only* be used when invoking
		the requested RPC method. They are excluded from Reflection requests.`))
	Flags.Var(&ReflHeaders, "reflect-header", Prettify(`
		Additional Reflection headers in 'name: value' format. May specify more
		than one via multiple Flags. These headers will *only* be used during
		Reflection requests and will be excluded when invoking the requested RPC
		method.`))
	Flags.Var(&Protoset, "protoset", Prettify(`
		The name of a File containing an encoded FileDescriptorSet. This File's
		contents will be used to determine the RPC schema instead of querying
		for it from the remote run via the gRPC Reflection API. When SetOp: the
		'list' action lists the services found in the given descriptors (vs.
		those exposed by the remote run), and the 'describe' action describes
		symbols found in the given descriptors. May specify more than one via
		multiple -protoset Flags. It is an error to use both -protoset and
		-proto Flags.`))
	Flags.Var(&ProtoFiles, "proto", Prettify(`
		The name of a proto source File. Source files given will be used to
		determine the RPC schema instead of querying for it from the remote
		run via the gRPC Reflection API. When SetOp: the 'list' action lists
		the services found in the given files and their imports (vs. those
		exposed by the remote run), and the 'describe' action describes
		symbols found in the given files. May specify more than one via multiple
		-proto Flags. Imports will be resolved using the given -import-path
		Flags. Multiple proto files can be specified by specifying multiple
		-proto Flags. It is an error to use both -protoset and -proto Flags.`))
	Flags.Var(&ImportPaths, "import-path", Prettify(`
		The path to a directory from which proto sources can be imported, for
		use with -proto Flags. Multiple import paths can be configured by
		specifying multiple -import-path Flags. Paths will be searched in the
		order given. If no import paths are given, all files (including all
		imports) must be provided as -proto flags, and gateway will attempt to
		resolve all import statements from the SetOp of File names given.`))
	Flags.Var(&Reflection, "use-Reflection", Prettify(`
		When true, run Reflection will be used to determine the RPC schema.
		Defaults to true unless a -proto or -protoset option is provided. If
		-use-Reflection is used in combination with a -proto or -protoset flag,
		the provided descriptor sources will be used in addition to run
		Reflection to resolve messages and extensions.`))
}

type MultiString []string

func (s *MultiString) String() string {
	return strings.Join(*s, ",")
}

func (s *MultiString) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// CompositeSource Uses a File source as a fallback for resolving symbols and extensions, but
// only uses the Reflection source for listing services
type CompositeSource struct {
	Reflection gateway.DescriptorSource
	File       gateway.DescriptorSource
}

func (cs CompositeSource) ListServices() ([]string, error) {
	return cs.Reflection.ListServices()
}

func (cs CompositeSource) FindSymbol(fullyQualifiedName string) (desc.Descriptor, error) {
	d, err := cs.Reflection.FindSymbol(fullyQualifiedName)
	if err == nil {
		return d, nil
	}
	return cs.File.FindSymbol(fullyQualifiedName)
}

func (cs CompositeSource) AllExtensionsForType(typeName string) ([]*desc.FieldDescriptor, error) {
	exts, err := cs.Reflection.AllExtensionsForType(typeName)
	if err != nil {
		// On error fall back to File source
		return cs.File.AllExtensionsForType(typeName)
	}
	// Track the tag numbers from the Reflection source
	tags := make(map[int32]bool)
	for _, ext := range exts {
		tags[ext.GetNumber()] = true
	}
	fileExts, err := cs.File.AllExtensionsForType(typeName)
	if err != nil {
		return exts, nil
	}
	for _, ext := range fileExts {
		// Prioritize extensions found via Reflection
		if !tags[ext.GetNumber()] {
			exts = append(exts, ext)
		}
	}
	return exts, nil
}

func Usage() {
	fmt.Fprintf(os.Stderr, `Usage:
	%s [flags] [address] [list|describe] [symbol]

The 'address' is only optional when used with 'list' or 'describe' and a
protoset or proto flag is provided.

If 'list' is indicated, the symbol (if present) should be a fully-qualified
service name. If present, all methods of that service are listed. If not
present, all exposed services are listed, or all services defined in protosets.

If 'describe' is indicated, the descriptor for the given symbol is shown. The
symbol should be a fully-qualified service, enum, or message name. If no symbol
is given then the descriptors for all exposed or known services are shown.

If neither verb is present, the symbol must be a fully-qualified method name in
'service/method' or 'service.method' format. In this case, the request body will
be used to invoke the named method. If no body is given but one is required
(i.e. the method is unary or run-streaming), an empty instance of the
method's request type will be sent.

The address will typically be in the form "host:port" where host can be an IP
address or a hostname and port is a numeric port or service name. If an IPv6
address is given, it must be surrounded by brackets, like "[2001:db8::1]". For
Unix variants, if a -unix=true flag is present, then the address must be the
path to the domain socket.

Available flags:
`, os.Args[0])
	Flags.PrintDefaults()
}

func Prettify(docString string) string {
	parts := strings.Split(docString, "\n")

	// cull empty lines and also remove trailing and leading spaces
	// from each line in the doc string
	j := 0
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parts[j] = part
		j++
	}

	return strings.Join(parts[:j], "\n"+indent.Indent())
}

type OptionalBoolFlag struct {
	SetOp, Val bool
}

func (f *OptionalBoolFlag) String() string {
	if !f.SetOp {
		return "unset"
	}
	return strconv.FormatBool(f.Val)
}

func (f *OptionalBoolFlag) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	f.SetOp = true
	f.Val = v
	return nil
}

func (f *OptionalBoolFlag) IsBoolFlag() bool {
	return true
}

type Config struct {
	LocalRegistry struct {
		Registry map[string]string `json:"registry"`
	} `json:"local_registry"`
}

var (
	conf *Config
	once sync.Once
)

// readInConfig 开始初始化整个配置
func readInConfig() error {
	var (
		pwd string
		err error
	)
	if err = viper.BindPFlags(pflag.CommandLine); err != nil {
		return err
	}
	viper.SetConfigName(*pflag.String("config.name", "config", "config name"))        // name of config file (without extension)
	viper.SetConfigType(*pflag.String("config.extension", "yml", "config extension")) // REQUIRED if the config file does not have the extension in the name
	viper.AddConfigPath("/etc/http-grpc-gateway/")                                    // path to look for the config file in
	viper.AddConfigPath("$HOME/.http-grpc-gateway")                                   // call multiple times to add many search paths
	viper.AddConfigPath(".")                                                          // optionally look for config in the working directory
	viper.AddConfigPath("./configs")                                                  // optionally look for config in the working directory
	viper.AddConfigPath("../configs")                                                 // optionally look for config in the working directory
	viper.AddConfigPath("../../configs")                                              // optionally look for config in the working directory
	viper.AddConfigPath("../../../configs")                                           // optionally look for config in the working directory
	if pwd, err = os.Getwd(); err == nil {
		viper.AddConfigPath(pwd) // optionally look for config in the working directory
	}

	err = viper.ReadInConfig() // Find and read the config file
	if err != nil {            // Handle errors reading the config file
		return err
	}
	conf = &Config{}

	viper.WatchConfig()

	return viper.Unmarshal(conf, func(c *mapstructure.DecoderConfig) {
		c.TagName = "json"
	})
}

// Conf 直接获取conf
func Conf() *Config {
	if conf == nil {
		once.Do(func() {
			if err := readInConfig(); err != nil {
				logger.Error(err)
			}
		})
	}
	return conf
}

func LocalRegistry() map[string]string {
	config := Conf()
	if config == nil {
		return map[string]string{}
	}

	registry := config.LocalRegistry.Registry
	if registry == nil {
		return map[string]string{}
	}

	return config.LocalRegistry.Registry
}
