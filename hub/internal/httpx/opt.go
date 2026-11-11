package httpx

import "encoding/json"

// Opt is a JSON field that tells apart "absent", "null" and "has a value",
// which PATCH bodies need.
type Opt[T any] struct {
	Set  bool
	Null bool
	Val  T
}

func (o *Opt[T]) UnmarshalJSON(raw []byte) error {
	o.Set = true
	if string(raw) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(raw, &o.Val)
}

// HasValue reports whether the field was sent with a non-null value.
func (o Opt[T]) HasValue() bool {
	return o.Set && !o.Null
}
