package bencode

import (
	"fmt"
	"strconv"
)

// Decode parses a single bencoded value from data and returns:
//   - the decoded value (int64 for integers, string for strings)
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