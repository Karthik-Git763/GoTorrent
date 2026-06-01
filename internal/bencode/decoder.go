package bencode

import (
	"fmt"
	"strconv"
)

// Decode parses a single bencoded value from data and returns:
//   - the decoded value (int64 for integers, string for strings, []interface{} for lists, map[string]interface{} for dictionaries)
//   - the remaining unconsumed bytes
//   - any error encountered
func Decode(data []byte) (interface{}, []byte, error) {
	if len(data) == 0 {
		return nil, data, fmt.Errorf("empty input")
	}
	switch {
	case data[0] == 'i':
		return decodeInteger(data)
	case data[0] >= '0' && data[0] <= '9':
		return decodeString(data)
	case data[0] == 'l':
		return decodeList(data)
	case data[0] == 'd':
		return decodeDict(data)
	default:
		return nil, data, fmt.Errorf("unknown type: %c", data[0])
	}
}

// decodeInteger decodes a bencode integer in the form i<number>e.
func decodeInteger(data []byte) (interface{}, []byte, error) {
	// data[0] is 'i', find the closing 'e'
	end := 1
	for end < len(data) && data[end] != 'e' {
		end++
	}
	if end >= len(data) {
		return nil, data, fmt.Errorf("invalid integer: missing 'e'")
	}

	// Parse the number portion (data[1:end])
	n, err := strconv.ParseInt(string(data[1:end]), 10, 64)
	if err != nil {
		return nil, data, fmt.Errorf("invalid integer: %v", err)
	}

	return n, data[end+1:], nil
}

// decodeString decodes a bencode string in the form <length>:<content>.
func decodeString(data []byte) (interface{}, []byte, error) {
	// Find the ':'
	colon := 0
	for colon < len(data) && data[colon] != ':' {
		colon++
	}
	if colon >= len(data) {
		return nil, data, fmt.Errorf("invalid string: missing ':'")
	}

	// Parse the length
	length, err := strconv.Atoi(string(data[:colon]))
	if err != nil {
		return nil, data, fmt.Errorf("invalid string length: %v", err)
	}
	if length < 0 {
		return nil, data, fmt.Errorf("invalid string length: %d (negative)", length)
	}

	// Check we have enough data
	start := colon + 1
	if start+length > len(data) {
		return nil, data, fmt.Errorf("invalid string: expected %d bytes, got %d", length, len(data)-start)
	}

	return string(data[start : start+length]), data[start+length:], nil
}

// decodeList decodes a bencode list in the form l<items>e.
func decodeList(data []byte) (interface{}, []byte, error) {
	if len(data) == 0 || data[0] != 'l' {
		return nil, data, fmt.Errorf("invalid list: missing 'l'")
	}

	list := make([]interface{}, 0)
	idx := 1
	for {
		if idx >= len(data) {
			return nil, data, fmt.Errorf("invalid list: missing 'e'")
		}
		if data[idx] == 'e' {
			return list, data[idx+1:], nil
		}

		item, rest, err := Decode(data[idx:])
		if err != nil {
			return nil, data, err
		}
		list = append(list, item)

		consumed := len(data[idx:]) - len(rest)
		idx += consumed
	}
}

// decodeDict decodes a bencode dictionary in the form d<items>e.
func decodeDict(data []byte) (map[string]interface{}, []byte, error) {
	if len(data) == 0 || data[0] != 'd' {
		return nil, data, fmt.Errorf("invalid dict: missing 'd'")
	}

	dict := make(map[string]interface{})
	idx := 1
	for {
		if idx >= len(data) {
			return nil, data, fmt.Errorf("invalid dict: missing 'e'")
		}
		if data[idx] == 'e' {
			return dict, data[idx+1:], nil
		}

		keyVal, rest, err := Decode(data[idx:])
		if err != nil {
			return nil, data, err
		}
		key, ok := keyVal.(string)
		if !ok {
			return nil, data, fmt.Errorf("invalid dict key: expected string, got %T", keyVal)
		}
		consumed := len(data[idx:]) - len(rest)
		idx += consumed

		if idx >= len(data) {
			return nil, data, fmt.Errorf("invalid dict: missing value for key %q", key)
		}

		value, rest, err := Decode(data[idx:])
		if err != nil {
			return nil, data, err
		}
		dict[key] = value
		consumed = len(data[idx:]) - len(rest)
		idx += consumed
	}
}
