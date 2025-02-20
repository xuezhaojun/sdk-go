package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/keepalive"
	"gopkg.in/yaml.v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protocol"
)

// GRPCOptions holds the options that are used to build gRPC client.
type GRPCOptions struct {
	URL              string
	CAFile           string
	ClientCertFile   string
	ClientKeyFile    string
	TokenFile        string
	KeepAliveOptions KeepAliveOptions
}

// KeepAliveOptions holds the keepalive options for the gRPC client.
type KeepAliveOptions struct {
	Enable              bool
	Time                time.Duration
	Timeout             time.Duration
	PermitWithoutStream bool
}

// GRPCConfig holds the information needed to build connect to gRPC server as a given user.
type GRPCConfig struct {
	// URL is the address of the gRPC server (host:port).
	URL string `json:"url" yaml:"url"`
	// CAFile is the file path to a cert file for the gRPC server certificate authority.
	CAFile string `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	// ClientCertFile is the file path to a client cert file for TLS.
	ClientCertFile string `json:"clientCertFile,omitempty" yaml:"clientCertFile,omitempty"`
	// ClientKeyFile is the file path to a client key file for TLS.
	ClientKeyFile string `json:"clientKeyFile,omitempty" yaml:"clientKeyFile,omitempty"`
	// TokenFile is the file path to a token file for authentication.
	TokenFile string `json:"tokenFile,omitempty" yaml:"tokenFile,omitempty"`
	// keepalive options
	KeepAliveConfig KeepAliveConfig `json:"keepAliveConfig,omitempty" yaml:"keepAliveConfig,omitempty"`
}

// KeepAliveConfig holds the keepalive options for the gRPC client.
type KeepAliveConfig struct {
	// Enable specifies whether the keepalive option is enabled.
	// When disabled, other keepalive configurations are ignored. Default is false.
	Enable bool `json:"enable,omitempty" yaml:"enable,omitempty"`
	// Time sets the duration after which the client pings the server if no activity is seen.
	// A minimum value of 10s is enforced if set below that. Default is 30s.
	Time *time.Duration `json:"time,omitempty" yaml:"time,omitempty"`

	// Timeout sets the duration the client waits for a response after a keepalive ping.
	// If no response is received, the connection is closed. Default is 10s.
	Timeout *time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// PermitWithoutStream determines if keepalive pings are sent when there are no active RPCs.
	// If false, pings are not sent and Time and Timeout are ignored. Default is false.
	PermitWithoutStream bool `json:"permitWithoutStream,omitempty" yaml:"permitWithoutStream,omitempty"`
}

// BuildGRPCOptionsFromFlags builds configs from a config filepath.
func BuildGRPCOptionsFromFlags(configPath string) (*GRPCOptions, error) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	config := &GRPCConfig{}
	if err := yaml.Unmarshal(configData, config); err != nil {
		return nil, err
	}

	if config.URL == "" {
		return nil, fmt.Errorf("url is required")
	}

	if (config.ClientCertFile == "" && config.ClientKeyFile != "") ||
		(config.ClientCertFile != "" && config.ClientKeyFile == "") {
		return nil, fmt.Errorf("either both or none of clientCertFile and clientKeyFile must be set")
	}
	if config.ClientCertFile != "" && config.ClientKeyFile != "" && config.CAFile == "" {
		return nil, fmt.Errorf("setting clientCertFile and clientKeyFile requires caFile")
	}
	if config.TokenFile != "" && config.CAFile == "" {
		return nil, fmt.Errorf("setting tokenFile requires caFile")
	}

	options := &GRPCOptions{
		URL:            config.URL,
		CAFile:         config.CAFile,
		ClientCertFile: config.ClientCertFile,
		ClientKeyFile:  config.ClientKeyFile,
		TokenFile:      config.TokenFile,
		KeepAliveOptions: KeepAliveOptions{
			Enable:              false,
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: false,
		},
	}

	options.KeepAliveOptions.Enable = config.KeepAliveConfig.Enable

	if config.KeepAliveConfig.Time != nil {
		options.KeepAliveOptions.Time = *config.KeepAliveConfig.Time
	}

	if config.KeepAliveConfig.Timeout != nil {
		options.KeepAliveOptions.Timeout = *config.KeepAliveConfig.Timeout
	}

	options.KeepAliveOptions.PermitWithoutStream = config.KeepAliveConfig.PermitWithoutStream

	return options, nil
}

func NewGRPCOptions() *GRPCOptions {
	return &GRPCOptions{}
}

func (o *GRPCOptions) GetGRPCClientConn() (*grpc.ClientConn, error) {
	var kacp = keepalive.ClientParameters{
		Time:                o.KeepAliveOptions.Time,
		Timeout:             o.KeepAliveOptions.Timeout,
		PermitWithoutStream: o.KeepAliveOptions.PermitWithoutStream,
	}

	// Prepare gRPC dial options.
	dialOpts := []grpc.DialOption{}
	if o.KeepAliveOptions.Enable {
		dialOpts = append(dialOpts, grpc.WithKeepaliveParams(kacp))
	}

	if len(o.CAFile) != 0 {
		certPool, err := x509.SystemCertPool()
		if err != nil {
			return nil, err
		}

		caPEM, err := os.ReadFile(o.CAFile)
		if err != nil {
			return nil, err
		}

		if ok := certPool.AppendCertsFromPEM(caPEM); !ok {
			return nil, fmt.Errorf("invalid CA %s", o.CAFile)
		}

		// Create a TLS configuration with CA pool and TLS 1.3.
		tlsConfig := &tls.Config{
			RootCAs:    certPool,
			MinVersion: tls.VersionTLS13,
			MaxVersion: tls.VersionTLS13,
		}

		// Check if client certificate and key files are provided for mutual TLS.
		if len(o.ClientCertFile) != 0 && len(o.ClientKeyFile) != 0 {
			// Load client certificate and key pair.
			clientCerts, err := tls.LoadX509KeyPair(o.ClientCertFile, o.ClientKeyFile)
			if err != nil {
				return nil, err
			}
			// Add client certificates to the TLS configuration.
			tlsConfig.Certificates = []tls.Certificate{clientCerts}
			dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
		} else {
			// token based authentication requires the configuration of transport credentials.
			dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
			if len(o.TokenFile) != 0 {
				// Use token-based authentication if token file is provided.
				token, err := os.ReadFile(o.TokenFile)
				if err != nil {
					return nil, err
				}
				perRPCCred := oauth.TokenSource{
					TokenSource: oauth2.StaticTokenSource(&oauth2.Token{
						AccessToken: string(token),
					})}
				// Add per-RPC credentials to the dial options.
				dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(perRPCCred))
			}
		}

		// Establish a connection to the gRPC server.
		conn, err := grpc.Dial(o.URL, dialOpts...)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to grpc server %s, %v", o.URL, err)
		}

		return conn, nil
	}

	// Insecure connection option; should not be used in production.
	dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial(o.URL, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to grpc server %s, %v", o.URL, err)
	}

	return conn, nil
}

func (o *GRPCOptions) GetCloudEventsProtocol(ctx context.Context, errorHandler func(error), clientOpts ...protocol.Option) (options.CloudEventsProtocol, error) {
	conn, err := o.GetGRPCClientConn()
	if err != nil {
		return nil, err
	}

	// Periodically (every 100ms) check the connection status and reconnect if necessary.
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				conn.Close()
			case <-ticker.C:
				connState := conn.GetState()
				// If any failure in any of the steps needed to establish connection, or any failure encountered while
				// expecting successful communication on established channel, the grpc client connection state will be
				// TransientFailure.
				// For a connected grpc client, if the connections is down, the grpc client connection state will be
				// changed from Ready to Idle.
				if connState == connectivity.TransientFailure || connState == connectivity.Idle {
					errorHandler(fmt.Errorf("grpc connection is disconnected (state=%s)", connState))
					ticker.Stop()
					conn.Close()
					return // exit the goroutine as the error handler function will handle the reconnection.
				}
			}
		}
	}()

	opts := []protocol.Option{}
	opts = append(opts, clientOpts...)
	return protocol.NewProtocol(conn, opts...)
}
