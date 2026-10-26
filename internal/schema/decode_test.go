package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeRoundTrip(t *testing.T) {
	c := loadTestCompiled(t)
	orig := `{"stringField":"hello","int32Field":5,"tags":["a","b"],"address":{"city":"Kazan"}}`
	res, err := c.Encode("clue.test.v1.Item", []byte(orig), EncodeOpts{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := c.Decode("clue.test.v1.Item", res.Body, DecodeOpts{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Unwrapped {
		t.Error("did not expect Unwrapped")
	}
	if decoded.DecodedType != "clue.test.v1.Item" {
		t.Errorf("DecodedType = %q", decoded.DecodedType)
	}
	var got map[string]any
	if err := json.Unmarshal(decoded.Decoded, &got); err != nil {
		t.Fatalf("decoded json: %v", err)
	}
	if got["stringField"] != "hello" {
		t.Errorf("stringField = %v", got["stringField"])
	}
	if got["int32Field"] != float64(5) {
		t.Errorf("int32Field = %v", got["int32Field"])
	}
	if !strings.Contains(string(decoded.Decoded), `"city": "Kazan"`) {
		t.Errorf("decoded = %s", decoded.Decoded)
	}
	if !strings.Contains(string(decoded.Decoded), `"a"`) {
		t.Errorf("decoded = %s", decoded.Decoded)
	}
}

func TestDecodeAnyBinaryUnwrap(t *testing.T) {
	c := loadTestCompiled(t)
	res, err := c.Encode("clue.test.v1.Address", []byte(`{"city":"SPb"}`), EncodeOpts{WrapAny: true})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := c.Decode("clue.test.v1.Address", res.Body, DecodeOpts{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !decoded.Unwrapped || decoded.DecodedType != "clue.test.v1.Address" {
		t.Fatalf("unwrap result: %+v", decoded)
	}
	if !strings.Contains(string(decoded.Decoded), `"city": "SPb"`) {
		t.Errorf("decoded = %s", decoded.Decoded)
	}
}

func TestDecodeAnyJSONUnwrap(t *testing.T) {
	c := loadTestCompiled(t)
	payload := []byte(`{"@type":"type.googleapis.com/clue.test.v1.Address","city":"Ufa"}`)
	decoded, err := c.Decode("clue.test.v1.Address", payload, DecodeOpts{PayloadJSON: true})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !decoded.Unwrapped || decoded.DecodedType != "clue.test.v1.Address" {
		t.Fatalf("unwrap result: %+v", decoded)
	}
	if !strings.Contains(string(decoded.Decoded), `"city": "Ufa"`) {
		t.Errorf("decoded = %s", decoded.Decoded)
	}
}

func TestDecodeAnyEnvelopeJSON(t *testing.T) {
	c := loadTestCompiled(t)
	envJSON := `{"payload":{"@type":"type.googleapis.com/clue.test.v1.Address","city":"Sochi"}}`
	res, err := c.Encode("clue.test.v1.AnyEnvelope", []byte(envJSON), EncodeOpts{Encoding: EncodingJSON})
	if err != nil {
		t.Fatalf("Encode envelope: %v", err)
	}
	if !strings.Contains(string(res.Body), `"city"`) {
		t.Errorf("envelope JSON = %s", res.Body)
	}
	decoded, err := c.Decode("clue.test.v1.AnyEnvelope", res.Body, DecodeOpts{PayloadJSON: true})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Unwrapped {
		t.Error("envelope decode should not mark Unwrapped")
	}
	if !strings.Contains(string(decoded.Decoded), `"city": "Sochi"`) {
		t.Errorf("decoded = %s", decoded.Decoded)
	}
}

func TestDecodeProtoNamesAndEmitUnpopulated(t *testing.T) {
	c := loadTestCompiled(t)
	res, err := c.Encode("clue.test.v1.Address", []byte(`{"city":"Tver"}`), EncodeOpts{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := c.Decode("clue.test.v1.Address", res.Body, DecodeOpts{UseProtoNames: true, EmitUnpopulated: true})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !strings.Contains(string(decoded.Decoded), `"street": ""`) {
		t.Errorf("expected emitted street field, got %s", decoded.Decoded)
	}
}

func TestDecodeGarbage(t *testing.T) {
	c := loadTestCompiled(t)
	garbage := []byte{0xFF, 0xFF, 0xFF}
	if _, err := c.Decode("clue.test.v1.Address", garbage, DecodeOpts{}); err == nil {
		t.Fatal("expected error for garbage payload")
	}
}

func TestDecodeJSONUnknownFieldRejected(t *testing.T) {
	c := loadTestCompiled(t)
	if _, err := c.Decode("clue.test.v1.Address", []byte(`{"nope":1}`), DecodeOpts{PayloadJSON: true}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeTruncated(t *testing.T) {
	c := loadTestCompiled(t)
	res, err := c.Encode("clue.test.v1.Item", []byte(`{"stringField":"hello"}`), EncodeOpts{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := c.Decode("clue.test.v1.Item", res.Body[:len(res.Body)-2], DecodeOpts{}); err == nil {
		t.Fatal("expected error for truncated payload")
	}
}

func TestDecodeJSONUsesKnownTypes(t *testing.T) {
	c := loadTestCompiled(t)
	payload := `{"createdAt":"2024-05-05T10:00:00Z","ttl":"5s"}`
	res, err := c.Encode("clue.test.v1.Item", []byte(payload), EncodeOpts{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := c.Decode("clue.test.v1.Item", res.Body, DecodeOpts{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !strings.Contains(string(decoded.Decoded), `"createdAt": "2024-05-05T10:00:00Z"`) {
		t.Errorf("decoded = %s", decoded.Decoded)
	}
}

func TestDecodeLargeUint64(t *testing.T) {
	c := loadTestCompiled(t)
	payload := []byte(`{"uint64Field":"18446744073709551615"}`)
	res, err := c.Encode("clue.test.v1.Item", payload, EncodeOpts{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := c.Decode("clue.test.v1.Item", res.Body, DecodeOpts{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !strings.Contains(string(decoded.Decoded), `"uint64Field": "18446744073709551615"`) {
		t.Errorf("decoded = %s", decoded.Decoded)
	}
}
