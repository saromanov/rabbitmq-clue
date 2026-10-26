package schema

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	EncodingBinary = "binary"
	EncodingJSON   = "json"

	ContentTypeProtobuf = "application/x-protobuf"
	ContentTypeJSON     = "application/json"
)

// EncodeOpts controls how a JSON payload is turned into publishable bytes.
type EncodeOpts struct {
	// Encoding: "binary" (default) or "json".
	Encoding string
	// WrapAny wraps the message into google.protobuf.Any.
	WrapAny bool
	// JSONUseProtoNames emits snake_case names in JSON output.
	JSONUseProtoNames bool
}

// EncodeResult is the encoded payload with the default content type.
type EncodeResult struct {
	Body        []byte
	ContentType string
}

// Encode builds the message from a JSON payload (protojson syntax) and
// serializes it for publishing.
func (c *Compiled) Encode(msgType string, payload []byte, opts EncodeOpts) (*EncodeResult, error) {
	msg, err := c.newMessage(msgType)
	if err != nil {
		return nil, err
	}
	umo := protojson.UnmarshalOptions{Resolver: c.jsonResolver()}
	if err := umo.Unmarshal(payload, msg); err != nil {
		return nil, fmt.Errorf("invalid payload for %s: %w", msgType, err)
	}

	target := msg
	if opts.WrapAny {
		anyMsg, err := anypb.New(msg)
		if err != nil {
			return nil, fmt.Errorf("wrap in Any: %w", err)
		}
		target = anyMsg
	}

	switch opts.Encoding {
	case EncodingBinary, "":
		body, err := proto.Marshal(target)
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", msgType, err)
		}
		return &EncodeResult{Body: body, ContentType: ContentTypeProtobuf}, nil
	case EncodingJSON:
		mo := protojson.MarshalOptions{
			UseProtoNames: opts.JSONUseProtoNames,
			Resolver:      c.jsonResolver(),
		}
		body, err := mo.Marshal(target)
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", msgType, err)
		}
		return &EncodeResult{Body: body, ContentType: ContentTypeJSON}, nil
	default:
		return nil, fmt.Errorf("unknown encoding %q", opts.Encoding)
	}
}
