package txnclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"time"

	pb "github.com/fintech/transaction/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// TransactionClient wraps the generated proto client with Dial options and retry policies.
type TransactionClient struct {
	conn   *grpc.ClientConn
	client pb.TransactionServiceClient
}

// NewTransactionClient creates a v2 client with configurable mTLS options.
func NewTransactionClient(addr string, certFile, keyFile, caFile string) (*TransactionClient, error) {
	var opts []grpc.DialOption

	if certFile != "" && keyFile != "" && caFile != "" {
		creds, err := loadTLSCredentials(certFile, keyFile, caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client TLS credentials: %w", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial gRPC server: %w", err)
	}

	return &TransactionClient{
		conn:   conn,
		client: pb.NewTransactionServiceClient(conn),
	}, nil
}

func loadTLSCredentials(certFile, keyFile, caFile string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to append CA cert")
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      certPool,
	}

	return credentials.NewTLS(config), nil
}

// ProcessTransaction processes a transaction with a 3-retry backoff policy on UNAVAILABLE status.
func (c *TransactionClient) ProcessTransaction(ctx context.Context, req *pb.TransactionRequest) (*pb.TransactionResponse, error) {
	var resp *pb.TransactionResponse
	var err error

	backoff := 100 * time.Millisecond
	for i := 0; i < 3; i++ {
		resp, err = c.client.ProcessTransaction(ctx, req)
		if err == nil {
			return resp, nil
		}

		st, _ := status.FromError(err)
		if st.Code() != codes.UNAVAILABLE {
			return nil, fmt.Errorf("transaction ID %s: %w", req.GetTransactionId(), err)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
		}
	}

	return nil, fmt.Errorf("transaction ID %s failed after 3 retries: %w", req.GetTransactionId(), err)
}

// GetTransaction retrieves a transaction with a 3-retry backoff policy on UNAVAILABLE status.
func (c *TransactionClient) GetTransaction(ctx context.Context, transactionID string) (*pb.TransactionResponse, error) {
	var resp *pb.TransactionResponse
	var err error

	req := &pb.GetTransactionRequest{TransactionId: transactionID}

	backoff := 100 * time.Millisecond
	for i := 0; i < 3; i++ {
		resp, err = c.client.GetTransaction(ctx, req)
		if err == nil {
			return resp, nil
		}

		st, _ := status.FromError(err)
		if st.Code() != codes.UNAVAILABLE {
			return nil, fmt.Errorf("transaction ID %s: %w", transactionID, err)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
		}
	}

	return nil, fmt.Errorf("transaction ID %s failed after 3 retries: %w", transactionID, err)
}

// StreamStatus retrieves real-time status updates and invokes the handler function for updates.
func (c *TransactionClient) StreamStatus(ctx context.Context, transactionID string, handler func(*pb.StatusUpdate)) error {
	req := &pb.StreamStatusRequest{}

	stream, err := c.client.StreamStatus(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}

	for {
		update, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("error reading stream for ID %s: %w", transactionID, err)
		}

		if update.GetTransactionId() == transactionID {
			handler(update)
		}
	}
}

// Close closes the underlying gRPC client connection.
func (c *TransactionClient) Close() error {
	return c.conn.Close()
}
