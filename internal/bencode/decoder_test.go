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