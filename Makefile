PROTO_DIR := api/proto
PB_DIR := api/pb
GOOGLEAPIS_DIR := third_party/googleapis

.PHONY: generate test race run

generate:
	PATH="$$(go env GOPATH)/bin:$$PATH" protoc -I $(PROTO_DIR) -I $(GOOGLEAPIS_DIR) \
		--go_out=$(PB_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(PB_DIR) --go-grpc_opt=paths=source_relative \
		--grpc-gateway_out=$(PB_DIR) --grpc-gateway_opt=paths=source_relative \
		$(PROTO_DIR)/notification.proto

test:
	go test ./...

race:
	go test -race ./...

run:
	go run ./cmd/notification
