package engine

import "bytes"

// bytesReader is a tiny helper so rules.go can use json.Decoder with
// DisallowUnknownFields over a RawMessage.
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
