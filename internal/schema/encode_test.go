package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeBinary(t *testing.T) {
	c := loadTestCompiled(t)
	res, err := c.Encode("clue.test.v1.Item", []byte(`{"stringField":"hi","int32Field":7,"status":"STATUS_ACTIVE"}`), EncodeOpts{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if res.ContentType != ContentTypeProtobuf {
		t.Errorf("content type = %q", res.ContentType)
	}
	decoded, err := c.Decode("clue.test.v1.Item", res.Body, DecodeOpts{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(decoded.Decoded, &obj); err != nil {
		t.Fatalf("decoded is not JSON: %v", err)
	}
	if obj["stringField"] != "hi" || obj["int32Field"] != float64(7) {
		t.Errorf("decoded = %v %v", obj["stringField"], obj["int32Field"])
	}
}

func TestEncodeBinaryInt64IsString(t *testing.T) {
	c := loadTestCompiled(t)
	res, err := c.Encode("clue.test.v1.Item", []byte(`{"int64Field":"123"}`), EncodeOpts{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := c.Decode("clue.test.v1.Item", res.Body, DecodeOpts{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(decoded.Decoded, &obj); err != nil {
		t.Fatalf("json: %v", err)
	}
	if obj["int64Field"] != "123" {
		t.Errorf("int64Field = %v, want string 123", obj["int64Field"])
	}
}

func TestEncodeJSON(t *testing.T) {
	c := loadTestCompiled(t)
	res, err := c.Encode("clue.test.v1.Address", []byte(`{"city":"Moscow"}`), EncodeOpts{Encoding: EncodingJSON})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if res.ContentType != ContentTypeJSON {
		t.Errorf("content type = %q", res.ContentType)
	}
	var obj map[string]any
	if err := json.Unmarshal(res.Body, &obj); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if obj["city"] != "Moscow" {
		t.Errorf("city = %v", obj["city"])
	}
}

func TestEncodeJSONProtoNames(t *testing.T) {
	c := loadTestCompiled(t)
	res, err := c.Encode("clue.test.v1.Item", []byte(`{"int32Field":1}`), EncodeOpts{Encoding: EncodingJSON, JSONUseProtoNames: true})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.Contains(string(res.Body), `"int32_field"`) {
		t.Errorf("expected proto names in %s", res.Body)
	}
}

func TestEncodeWrapAny(t *testing.T) {
	c := loadTestCompiled(t)
	res, err := c.Encode("clue.test.v1.Address", []byte(`{"city":"SPb"}`), EncodeOpts{WrapAny: true})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := c.Decode("clue.test.v1.AnyEnvelope", res.Body, DecodeOpts{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !decoded.Unwrapped {
		t.Error("expected Unwrapped = true")
	}
	if decoded.DecodedType != "clue.test.v1.Address" {
		t.Errorf("DecodedType = %q", decoded.DecodedType)
	}
	var obj map[string]any
	if err := json.Unmarshal(decoded.Decoded, &obj); err != nil {
		t.Fatalf("json: %v", err)
	}
	if obj["city"] != "SPb" {
		t.Errorf("city = %v", obj["city"])
	}
}

func TestEncodeUnknownEncoding(t *testing.T) {
	c := loadTestCompiled(t)
	if _, err := c.Encode("clue.test.v1.Address", []byte(`{}`), EncodeOpts{Encoding: "yaml"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestEncodeUnknownType(t *testing.T) {
	c := loadTestCompiled(t)
	if _, err := c.Encode("no.such.Msg", []byte(`{}`), EncodeOpts{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestEncodeInvalidPayload(t *testing.T) {
	c := loadTestCompiled(t)
	if _, err := c.Encode("clue.test.v1.Address", []byte(`{not json`), EncodeOpts{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestEncodeUnknownFieldRejected(t *testing.T) {
	c := loadTestCompiled(t)
	if _, err := c.Encode("clue.test.v1.Address", []byte(`{"nope":1}`), EncodeOpts{}); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestEncodeBadEnumValue(t *testing.T) {
	c := loadTestCompiled(t)
	if _, err := c.Encode("clue.test.v1.Item", []byte(`{"status":"NOPE"}`), EncodeOpts{}); err == nil {
		t.Fatal("expected error for bad enum")
	}
}

func TestEncodeBadBytes(t *testing.T) {
	c := loadTestCompiled(t)
	if _, err := c.Encode("clue.test.v1.Item", []byte(`{"bytesField":"!!!"}`), EncodeOpts{}); err == nil {
		t.Fatal("expected error for bad base64")
	}
}

func TestEncodeBadTimestamp(t *testing.T) {
	c := loadTestCompiled(t)
	if _, err := c.Encode("clue.test.v1.Item", []byte(`{"createdAt":"not-a-time"}`), EncodeOpts{}); err == nil {
		t.Fatal("expected error for bad timestamp")
	}
}
