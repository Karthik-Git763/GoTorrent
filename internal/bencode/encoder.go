package bencode

import (
	"bytes"
	"fmt"
	"io"
)

func MarshalValue(w io.Writer, v any) error {
	switch v := v.(type) {
	case int:
		_, err := fmt.Fprintf(w, "i%de", v)
		return err
	case int64:
		_, err := fmt.Fprintf(w, "i%de", v)
		return err
	case string:
		_, err := fmt.Fprintf(w, "%d:%s", len(v), v)
		return err
	case []byte:
		_, err := fmt.Fprintf(w, "%d:", len(v))
		if err != nil {
			return err
		}
		_, err = w.Write(v)
		return err
	case []any:
		if _, err := fmt.Fprint(w, "l"); err != nil {
			return err
		}
		for _, item := range v {
			if err := MarshalValue(w, item); err != nil {
				return err
			}
		}
		_, err := fmt.Fprint(w, "e")
		return err
	case map[string]any:
		if _, err := fmt.Fprint(w, "d"); err != nil {
			return err
		}
		for k, item := range v {
			if err := MarshalValue(w, k); err != nil {
				return err
			}
			if err := MarshalValue(w, item); err != nil {
				return err
			}
		}
		_, err := fmt.Fprint(w, "e")
		return err
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
}

func Marshal(v any) ([]byte, error) {
	buf := bytes.Buffer{}
	if err := MarshalValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
