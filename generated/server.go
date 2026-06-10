// Package transactionv2server provides a gRPC server implementation for the TransactionService v2.
// It handles transaction processing, retrieval, and status streaming according to the BRD requirements.
package transactionv2server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	// Assuming the generated proto package is at this path, relative to the module root.
	// In a real project, this would be specified by the module name configured in go.mod.
	transactionv2 "github.com/fintech/transaction/v2" 
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TransactionServiceServer implements the gRPC TransactionServiceServer interface.
type TransactionServiceServer struct {
	// UnimplementedTransactionServiceServer must be embedded to have forward compatible implementations
	// of the TransactionServiceServer.
	transactionv2.UnimplementedTransactionServiceServer

	mu               sync.RWMutex                     // Protects 'transactions' map
	transactions     map[string]*transactionv2.TransactionResponse // Stores processed transactions
	idempotencyCache sync.Map                         // map[string]idempotencyEntry: Stores processed requests for idempotency
	logger           *slog.Logger                     // Structured logger
}

// idempotencyEntry stores information about a processed transaction for idempotency checks.
type idempotencyEntry struct {
	OriginalRequest *transactionv2.TransactionRequest // The full original request for deep comparison
	Response        *transactionv2.TransactionResponse  // The successful response to return for idempotent requests
}

// NewTransactionServiceServer creates and returns a new TransactionServiceServer instance.
// It initializes the internal state and sets up the logger.
func NewTransactionServiceServer(logger *slog.Logger) *TransactionServiceServer {
	if logger == nil {
		logger = slog.Default() // Use default logger if none is provided
	}
	return &TransactionServiceServer{
		transactions:     make(map[string]*transactionv2.TransactionResponse),
		idempotencyCache: sync.Map{},
		logger:           logger,
	}
}

// validateProcessTransactionRequest performs validation checks on the TransactionRequest
// based on BRD requirements. It returns a gRPC status error if validation fails.
func (s *TransactionServiceServer) validateProcessTransactionRequest(req *transactionv2.TransactionRequest) error {
	if req.GetTransactionId() == "" {
		return status.Errorf(codes.INVALID_ARGUMENT, "transaction_id cannot be empty")
	}
	if req.GetAmount() <= 0 {
		return status.Errorf(codes.INVALID_ARGUMENT, "amount must be positive")
	}
	if len(req.GetCurrencyCode()) != 3 {
		return status.Errorf(codes.INVALID_ARGUMENT, "currency_code must be a 3-character ISO 4217 code")
	}
	if req.GetSourceAccountId() == "" {
		return status.Errorf(codes.INVALID_ARGUMENT, "source_account_id cannot be empty")
	}
	if req.GetDestAccountId() == "" {
		return status.Errorf(codes.INVALID_ARGUMENT, "dest_account_id cannot be empty")
	}
	return nil
}

// ProcessTransaction handles the unary RPC for processing a financial transaction.
// It validates the request, performs an idempotency check, and simulates transaction processing.
// Context deadlines are respected during processing.
func (s *TransactionServiceServer) ProcessTransaction(ctx context.Context, req *transactionv2.TransactionRequest) (*transactionv2.TransactionResponse, error) {
	txID := req.GetTransactionId()
	s.logger.Info("Received ProcessTransaction request", slog.String("transaction_id", txID))

	// 1. Input Validation
	if err := s.validateProcessTransactionRequest(req); err != nil {
		s.logger.Warn("ProcessTransaction validation failed", slog.String("transaction_id", txID), slog.Any("error", err))
		return nil, err // Returns gRPC status as defined by the validation helper
	}

	// 2. Idempotency Check using sync.Map
	if entry, ok := s.idempotencyCache.Load(txID); ok {
		cachedEntry := entry.(idempotencyEntry)
		// Compare the original request with the current request payload
		// proto.Equal performs a deep comparison of protobuf messages.
		if proto.Equal(cachedEntry.OriginalRequest, req) {
			s.logger.Info("ProcessTransaction: Idempotent request detected, returning cached response", slog.String("transaction_id", txID))
			return cachedEntry.Response, nil
		} else {
			// If transaction_id exists but the request payload differs, it's a conflict.
			s.logger.Warn("ProcessTransaction: Transaction ID already exists with a different payload", slog.String("transaction_id", txID))
			return nil, status.Errorf(codes.ALREADY_EXISTS, "transaction_id '%s' already exists with a different payload", txID)
		}
	}

	// 3. Simulate Transaction Processing
	// In a production system, this would involve complex business logic,
	// database operations, and potentially calls to other services.
	s.logger.Debug("ProcessTransaction: Simulating processing delay", slog.String("transaction_id", txID))
	select {
	case <-ctx.Done():
		// Context was cancelled or deadline exceeded before processing completed.
		s.logger.Warn("ProcessTransaction context cancelled or deadline exceeded during simulation", slog.String("transaction_id", txID), slog.Any("error", ctx.Err()))
		return nil, status.Errorf(codes.Canceled, "processing cancelled: %v", ctx.Err())
	case <-time.After(50 * time.Millisecond): // Simulate work (e.g., database write, external API call)
		// Processing simulated successfully.
	}

	// Determine transaction creation time. Prioritize request's created_at, fallback to current time.
	createdAt := req.GetCreatedAt()
	if createdAt == nil {
		createdAt = timestamppb.Now()
	}

	// For this example, we'll initially set the status to PENDING.
	// A real system would move it through PROCESSING, SUCCESS/FAILED via background workers.
	resp := &transactionv2.TransactionResponse{
		TransactionId: txID,
		Status:        transactionv2.TxStatus_PENDING,
		CreatedAt:     createdAt,
	}

	// Store the transaction in our in-memory map for GetTransaction and StreamStatus.
	s.mu.Lock()
	s.transactions[txID] = resp
	s.mu.Unlock()

	// Update the idempotency cache with the request and the generated response.
	// In a production system, this would typically happen *after* successful persistence
	// of the transaction to durable storage.
	s.idempotencyCache.Store(txID, idempotencyEntry{
		OriginalRequest: req,
		Response:        resp,
	})

	s.logger.Info("ProcessTransaction completed successfully", slog.String("transaction_id", txID), slog.String("status", resp.GetStatus().String()))
	return resp, nil
}

// GetTransaction retrieves a transaction by its ID.
// It returns NOT_FOUND if the transaction with the given ID does not exist.
// Context deadlines are respected.
func (s *TransactionServiceServer) GetTransaction(ctx context.Context, req *transactionv2.GetTransactionRequest) (*transactionv2.TransactionResponse, error) {
	txID := req.GetTransactionId()
	s.logger.Info("Received GetTransaction request", slog.String("transaction_id", txID))

	if txID == "" {
		return nil, status.Errorf(codes.INVALID_ARGUMENT, "transaction_id cannot be empty")
	}

	// Check if context has been cancelled or deadline exceeded.
	select {
	case <-ctx.Done():
		s.logger.Warn("GetTransaction context cancelled or deadline exceeded", slog.String("transaction_id", txID), slog.Any("error", ctx.Err()))
		return nil, status.Errorf(codes.Canceled, "retrieval cancelled: %v", ctx.Err())
	default:
		// Continue
	}

	s.mu.RLock()
	resp, ok := s.transactions[txID]
	s.mu.RUnlock()

	if !ok {
		s.logger.Warn("GetTransaction: Transaction not found", slog.String("transaction_id", txID))
		return nil, status.Errorf(codes.NOT_FOUND, "transaction_id '%s' not found", txID)
	}

	s.logger.Info("GetTransaction completed successfully", slog.String("transaction_id", txID), slog.String("status", resp.GetStatus().String()))
	return resp, nil
}

// StreamStatus streams real-time status updates for a given transaction ID.
// It simulates status changes over time (PENDING -> PROCESSING -> SUCCESS/FAILED).
// If the transaction is not found, it returns NOT_FOUND.
func (s *TransactionServiceServer) StreamStatus(req *transactionv2.StreamStatusRequest, stream transactionv2.TransactionService_StreamStatusServer) error {
	txID := req.GetTransactionId()
	s.logger.Info("Received StreamStatus request", slog.String("transaction_id", txID))

	if txID == "" {
		return status.Errorf(codes.INVALID_ARGUMENT, "transaction_id cannot be empty")
	}

	// Check if the transaction exists before attempting to stream status updates.
	s.mu.RLock()
	initialTx, ok := s.transactions[txID]
	s.mu.RUnlock()

	if !ok {
		s.logger.Warn("StreamStatus: Transaction not found, cannot stream status", slog.String("transaction_id", txID))
		return status.Errorf(codes.NOT_FOUND, "transaction_id '%s' not found", txID)
	}

	// Simulate a progression of transaction statuses.
	// In a real system, these updates would be driven by events from a message queue
	// or background workers processing the transaction.
	simulatedStatuses := []transactionv2.TxStatus{
		transactionv2.TxStatus_PENDING,
		transactionv2.TxStatus_PROCESSING,
		transactionv2.TxStatus_SUCCESS, // Could be TX_STATUS_FAILED based on internal logic
	}

	// Determine where to start in the simulated status progression, based on the current status
	currentStatusIndex := 0
	for i, stat := range simulatedStatuses {
		if stat == initialTx.GetStatus() {
			currentStatusIndex = i
			break
		}
	}

	for i := currentStatusIndex; i < len(simulatedStatuses); i++ {
		select {
		case <-stream.Context().Done():
			// Client cancelled the stream or context deadline exceeded.
			s.logger.Warn("StreamStatus context cancelled or deadline exceeded during streaming", slog.String("transaction_id", txID), slog.Any("error", stream.Context().Err()))
			return status.Errorf