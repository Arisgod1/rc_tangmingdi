package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/arisone/redcapital/api/pb"
	"github.com/arisone/redcapital/internal/adapter"
	"github.com/arisone/redcapital/internal/adapter/mock"
	sqliterepo "github.com/arisone/redcapital/internal/datasource/notification"
	domain "github.com/arisone/redcapital/internal/domain/notification"
	"github.com/arisone/redcapital/internal/registry"
	"github.com/arisone/redcapital/internal/service"
	"github.com/arisone/redcapital/internal/worker"
)

func TestSubmitGetAndHTTPGateway(t *testing.T) {
	repo, err := sqliterepo.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	reg := registry.NewFromDeps(repo, map[string]adapter.Adapter{
		"mock": &mock.Adapter{Responses: []domain.DeliveryResult{{Outcome: domain.DeliverySucceeded, StatusCode: 200}}},
	})
	grpcServer := grpc.NewServer()
	pb.RegisterNotificationServiceServer(grpcServer, service.New(reg))

	grpcLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcAddr := grpcLis.Addr().String()
	go grpcServer.Serve(grpcLis)
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	gatewayMux := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, &service.LenientJSONPb{}))
	if err := pb.RegisterNotificationServiceHandler(context.Background(), gatewayMux, conn); err != nil {
		t.Fatal(err)
	}
	httpLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := &http.Server{Handler: gatewayMux}
	go httpServer.Serve(httpLis)
	t.Cleanup(func() { httpServer.Close() })

	worker := worker.New(reg, 10*time.Millisecond, 30*time.Second, nil)
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		worker.Run(workerCtx)
		close(workerDone)
	}()
	t.Cleanup(func() {
		cancelWorker()
		<-workerDone
	})

	client := pb.NewNotificationServiceClient(conn)
	ctx := context.Background()
	submit, err := client.SubmitNotification(ctx, &pb.SubmitNotificationRequest{
		Provider:       "mock",
		EventType:      "user.registered",
		IdempotencyKey: "order-42",
		Payload:        []byte(`{"user_id":42}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if submit.NotificationId == "" || submit.Status != pb.NotificationStatus_NOTIFICATION_STATUS_ACCEPTED {
		t.Fatalf("unexpected submit response: %+v", submit)
	}

	duplicate, err := client.SubmitNotification(ctx, &pb.SubmitNotificationRequest{
		Provider:       "mock",
		EventType:      "user.registered",
		IdempotencyKey: "order-42",
		Payload:        []byte(`{"user_id":42}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.NotificationId != submit.NotificationId {
		t.Fatalf("expected same notification id, got %s vs %s", duplicate.NotificationId, submit.NotificationId)
	}

	_, err = client.SubmitNotification(ctx, &pb.SubmitNotificationRequest{
		Provider:       "mock",
		EventType:      "user.registered",
		IdempotencyKey: "order-42",
		Payload:        []byte(`{"user_id":43}`),
	})
	if err == nil || status.Code(err).String() != "AlreadyExists" {
		t.Fatalf("expected AlreadyExists conflict, got %v", err)
	}

	var got *pb.GetNotificationResponse
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err = client.GetNotification(ctx, &pb.GetNotificationRequest{NotificationId: submit.NotificationId})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == pb.NotificationStatus_NOTIFICATION_STATUS_SUCCEEDED {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got == nil || got.Status != pb.NotificationStatus_NOTIFICATION_STATUS_SUCCEEDED {
		t.Fatalf("notification did not reach SUCCEEDED: %+v", got)
	}
	if got.Attempts != 1 || got.LastError != "" {
		t.Fatalf("unexpected delivery metadata: %+v", got)
	}

	resp, err := http.Get("http://" + httpLis.Addr().String() + "/v1/notifications/" + submit.NotificationId)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", resp.StatusCode, body)
	}
	var gatewayResult struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &gatewayResult); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gatewayResult.Status, "SUCCEEDED") {
		t.Fatalf("expected SUCCEEDED via gateway, got %s", gatewayResult.Status)
	}

	postResp, err := http.Post(
		"http://"+httpLis.Addr().String()+"/v1/notifications",
		"application/json",
		strings.NewReader(`{"provider":"mock","eventType":"order.paid","idempotencyKey":"http-1","payload":"{\"order_id\":7}"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer postResp.Body.Close()
	postBody, err := io.ReadAll(postResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 for POST, got %d: %s", postResp.StatusCode, postBody)
	}
	var postResult struct {
		NotificationId string `json:"notificationId"`
		Status         string `json:"status"`
	}
	if err := json.Unmarshal(postBody, &postResult); err != nil {
		t.Fatal(err)
	}
	if postResult.NotificationId == "" || !strings.HasSuffix(postResult.Status, "ACCEPTED") {
		t.Fatalf("unexpected POST result: %s", postBody)
	}
}
