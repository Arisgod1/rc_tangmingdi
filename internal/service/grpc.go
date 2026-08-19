package service

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/arisone/redcapital/api/pb"
	"github.com/arisone/redcapital/internal/adapter"
	domain "github.com/arisone/redcapital/internal/domain/notification"
)

type NotificationService struct {
	pb.UnimplementedNotificationServiceServer
	Repository domain.Repository
	Adapters   *adapter.Registry
}

func (s *NotificationService) SubmitNotification(ctx context.Context, req *pb.SubmitNotificationRequest) (*pb.SubmitNotificationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if _, ok := s.Adapters.Get(req.Provider); !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "provider %q is not configured", req.Provider)
	}
	n, err := domain.New(req.Provider, req.EventType, req.IdempotencyKey, req.Payload, time.Now())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid notification: %v", err)
	}
	stored, reused, err := s.Repository.CreateOrGet(ctx, n)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			return nil, status.Error(codes.AlreadyExists, "idempotency key conflicts with an existing notification")
		}
		return nil, status.Errorf(codes.Internal, "persist notification: %v", err)
	}
	if reused {
		return &pb.SubmitNotificationResponse{NotificationId: stored.ID, Status: mapStatus(stored.Status)}, nil
	}
	return &pb.SubmitNotificationResponse{NotificationId: stored.ID, Status: pb.NotificationStatus_NOTIFICATION_STATUS_ACCEPTED}, nil
}

func (s *NotificationService) GetNotification(ctx context.Context, req *pb.GetNotificationRequest) (*pb.GetNotificationResponse, error) {
	if req == nil || req.NotificationId == "" {
		return nil, status.Error(codes.InvalidArgument, "notification_id is required")
	}
	n, err := s.Repository.Get(ctx, req.NotificationId)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "notification not found")
		}
		return nil, status.Errorf(codes.Internal, "query notification: %v", err)
	}
	return toResponse(n), nil
}

func toResponse(n domain.Notification) *pb.GetNotificationResponse {
	resp := &pb.GetNotificationResponse{
		NotificationId: n.ID,
		Provider:       n.Provider,
		EventType:      n.EventType,
		IdempotencyKey: n.IdempotencyKey,
		Status:         mapStatus(n.Status),
		Attempts:       int32(n.Attempts),
		LastError:      n.LastError,
		CreatedAt:      timestamppb.New(n.CreatedAt),
		UpdatedAt:      timestamppb.New(n.UpdatedAt),
	}
	if n.NextAttemptAt != nil {
		resp.NextAttemptAt = timestamppb.New(*n.NextAttemptAt)
	}
	if n.DeliveredAt != nil {
		resp.DeliveredAt = timestamppb.New(*n.DeliveredAt)
	}
	return resp
}

func mapStatus(s domain.Status) pb.NotificationStatus {
	switch s {
	case domain.StatusPending:
		return pb.NotificationStatus_NOTIFICATION_STATUS_PENDING
	case domain.StatusDelivering:
		return pb.NotificationStatus_NOTIFICATION_STATUS_DELIVERING
	case domain.StatusRetryWait:
		return pb.NotificationStatus_NOTIFICATION_STATUS_RETRY_WAIT
	case domain.StatusSucceeded:
		return pb.NotificationStatus_NOTIFICATION_STATUS_SUCCEEDED
	case domain.StatusDead:
		return pb.NotificationStatus_NOTIFICATION_STATUS_DEAD
	default:
		return pb.NotificationStatus_NOTIFICATION_STATUS_UNSPECIFIED
	}
}
