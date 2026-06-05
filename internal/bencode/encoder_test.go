package bencode

import "testing"

func TestMarshalString(t *testing.T) {
	got, err := Marshal("hello")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "5:hello" {
		t.Fatalf("expected %q, got %q", "5:hello", string(got))
	}
}

func TestMarshalEmptyString(t *testing.T) {
	got, err := Marshal("")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "0:" {
		t.Fatalf("expected %q, got %q", "0:", string(got))
	}
}

func TestMarshalIntegerTypes(t *testing.T) {
	cases := []struct {
		name string
		value any
		want  string
	}{
		{name: "int", value: int(7), want: "i7e"},
		{name: "int64", value: int64(7), want: "i7e"},
		{name: "negative int", value: int(-5), want: "i-5e"},
		{name: "negative int64", value: int64(-5), want: "i-5e"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, string(got))
			}
		})
	}
}

func TestMarshalEmptyListAndDict(t *testing.T) {
	list, err := Marshal([]any{})
	if err != nil {
		t.Fatal(err)
	}
	if string(list) != "le" {
		t.Fatalf("expected %q, got %q", "le", string(list))
	}

	dict, err := Marshal(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if string(dict) != "de" {
		t.Fatalf("expected %q, got %q", "de", string(dict))
	}
}

func TestMarshalMapIntValues(t *testing.T) {
	got, err := Marshal(map[string]any{
		"count": int(3),
		"size":  int64(9),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Map iteration order is non-deterministic in Go; accept both orderings.
	want1 := "d5:counti3e4:sizei9ee"
	want2 := "d4:sizei9e5:counti3ee"
	if string(got) != want1 && string(got) != want2 {
		t.Fatalf("unexpected output: %q (expected either %q or %q)", string(got), want1, want2)
	}
}
