package schema

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// DecodeOpts controls payload decoding for peek.
type DecodeOpts struct {
	// PayloadJSON: the payload is protojson-encoded JSON instead of binary proto.
	PayloadJSON bool
	// UseProtoNames emits snake_case names in JSON output.
	UseProtoNames bool
	// EmitUnpopulated includes fields with default values.
	EmitUnpopulated bool
}

// DecodeResult is the decoded payload.
type DecodeResult struct {
	// Decoded is the pretty-printed JSON representation of the message.
	Decoded []byte
	// DecodedType is the full name of the decoded message
	// (the inner type when an Any payload was unwrapped).
	DecodedType string
	// Unwrapped reports that the payload was google.protobuf.Any and the
	// inner message was decoded instead.
	Unwrapped bool
}

// Decode converts a queue message payload into JSON according to the schema.
// When the payload parses as google.protobuf.Any whose type_url matches a
// message in the schema, the inner message is decoded; otherwise the payload
// is decoded as msgType directly.
func (c *Compiled) Decode(msgType string, payload []byte, opts DecodeOpts) (*DecodeResult, error) {
	if res, err := c.decodeAny(payload, opts); err == nil {
		res.Unwrapped = true
		return res, nil
	}
	return c.decodeAs(msgType, payload, opts)
}

func (c *Compiled) decodeAs(msgType string, payload []byte, opts DecodeOpts) (*DecodeResult, error) {
	msg, err := c.newMessage(msgType)
	if err != nil {
		return nil, err
	}
	if err := c.unmarshalInto(msg, payload, opts); err != nil {
		return nil, fmt.Errorf("decode %s: %w", msgType, err)
	}
	out, err := c.marshalJSON(msg, opts)
	if err != nil {
		return nil, err
	}
	return &DecodeResult{Decoded: out, DecodedType: msgType}, nil
}

// decodeAny treats the payload as google.protobuf.Any and decodes the inner
// message resolved from type_url within this schema.
func (c *Compiled) decodeAny(payload []byte, opts DecodeOpts) (*DecodeResult, error) {
	anyMsg := &anypb.Any{}
	if err := c.unmarshalInto(anyMsg, payload, opts); err != nil {
		return nil, fmt.Errorf("decode Any: %w", err)
	}
	if anyMsg.TypeUrl == "" {
		return nil, errors.New("payload is not google.protobuf.Any: empty type_url")
	}
	innerType := anyMsg.TypeUrl
	if i := strings.LastIndex(innerType, "/"); i >= 0 {
		innerType = innerType[i+1:]
	}
	inner, err := c.newMessage(innerType)
	if err != nil {
		return nil, fmt.Errorf("decode Any: %w", err)
	}
	if err := proto.Unmarshal(anyMsg.Value, inner); err != nil {
		return nil, fmt.Errorf("decode Any inner %s: %w", innerType, err)
	}
	out, err := c.marshalJSON(inner, opts)
	if err != nil {
		return nil, err
	}
	return &DecodeResult{Decoded: out, DecodedType: innerType}, nil
}

func (c *Compiled) unmarshalInto(msg proto.Message, payload []byte, opts DecodeOpts) error {
	if opts.PayloadJSON {
		umo := protojson.UnmarshalOptions{Resolver: c.jsonResolver()}
		if err := umo.Unmarshal(payload, msg); err != nil {
			return err
		}
		return nil
	}
	return proto.Unmarshal(payload, msg)
}

func (c *Compiled) marshalJSON(msg proto.Message, opts DecodeOpts) ([]byte, error) {
	mo := protojson.MarshalOptions{
		UseProtoNames:   opts.UseProtoNames,
		EmitUnpopulated: opts.EmitUnpopulated,
		Resolver:        c.jsonResolver(),
	}
	out, err := mo.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("render JSON: %w", err)
	}
	return marshalIndentJSON(out)
}

func marshalIndentJSON(compact []byte) ([]byte, error) {
	var buf any
	if err := jsonUnmarshal(compact, &buf); err != nil {
		return nil, err
	}
	return marshalIndent(buf)
}
