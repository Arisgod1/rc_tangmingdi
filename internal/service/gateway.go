package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/protobuf/proto"
)

// LenientJSONPb lets the HTTP gateway accept payload both as base64 (standard
// proto3 JSON) and as raw JSON bytes, which is friendlier for manual demos.
// gRPC calls are unaffected.
type LenientJSONPb struct {
	runtime.JSONPb
}

func (m *LenientJSONPb) ContentType(_ interface{}) string {
	return "application/json"
}

func (m *LenientJSONPb) Marshal(v interface{}) ([]byte, error) {
	return m.JSONPb.Marshal(v)
}

func (m *LenientJSONPb) Unmarshal(data []byte, v interface{}) error {
	message, ok := v.(proto.Message)
	if !ok {
		return m.JSONPb.Unmarshal(data, v)
	}
	normalized, err := normalizePayload(data)
	if err != nil {
		return err
	}
	return m.JSONPb.Unmarshal(normalized, message)
}

func (m *LenientJSONPb) NewDecoder(r io.Reader) runtime.Decoder {
	return lenientDecoder{r: r, marshaler: m}
}

type lenientDecoder struct {
	r         io.Reader
	marshaler *LenientJSONPb
}

func (d lenientDecoder) Decode(v interface{}) error {
	body, err := io.ReadAll(d.r)
	if err != nil {
		return err
	}
	return d.marshaler.Unmarshal(body, v)
}

func normalizePayload(body []byte) ([]byte, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return body, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	raw, ok := envelope["payload"]
	if !ok {
		return body, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return body, nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return body, nil
	}
	if _, err := base64.StdEncoding.DecodeString(value); err == nil {
		return body, nil
	}
	envelope["payload"] = json.RawMessage(fmt.Sprintf("%q", base64.StdEncoding.EncodeToString([]byte(value))))
	return json.Marshal(envelope)
}
