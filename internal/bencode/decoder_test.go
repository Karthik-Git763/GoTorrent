package bencode

import "testing"

func TestDecodeInteger(t *testing.T) {
	result, _, err := Decode([]byte("i42e"))
	if err != nil {
		t.Fatal(err)
	}
	if result != int64(42) {
		t.Fatalf("expected 42, got %v", result)
	}
}

func TestDecodeString(t *testing.T) {
	result, _, err := Decode([]byte("5:hello"))
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello" {
		t.Fatalf("expected \"hello\", got %v", result)
	}
}

func TestDecodeEmptyString(t *testing.T) {
	result, _, err := Decode([]byte("0:"))
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatalf("expected empty string, got %v", result)
	}
}

func TestDecodeNegativeInteger(t *testing.T) {
	result, _, err := Decode([]byte("i-42e"))
	if err != nil {
		t.Fatal(err)
	}
	if result != int64(-42) {
		t.Fatalf("expected -42, got %v", result)
	}
}

func TestDecodeZeroInteger(t *testing.T) {
	result, _, err := Decode([]byte("i0e"))
	if err != nil {
		t.Fatal(err)
	}
	if result != int64(0) {
		t.Fatalf("expected 0, got %v", result)
	}
}

func TestDecodeInvalidInteger(t *testing.T) {
	_, _, err := Decode([]byte("i42"))
	if err == nil {
		t.Fatal("expected error for missing 'e'")
	}
}

func TestDecodeInvalidString(t *testing.T) {
	_, _, err := Decode([]byte("5:hi"))
	if err == nil {
		t.Fatal("expected error for short string")
	}
}

func TestDecodeEmptyInput(t *testing.T) {
	_, _, err := Decode([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestDecodeUnknownType(t *testing.T) {
	_, _, err := Decode([]byte("x"))
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestDecodeRemainingBytes(t *testing.T) {
	val, rest, err := Decode([]byte("i42e5:hello"))
	if err != nil {
		t.Fatal(err)
	}
	if val != int64(42) {
		t.Fatalf("expected 42, got %v", val)
	}
	if string(rest) != "5:hello" {
		t.Fatalf("expected remaining \"5:hello\", got %q", string(rest))
	}
}