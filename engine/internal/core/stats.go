package core

// Traffic is what has been sent (Up) and received (Down) through the node,
// in bytes.
type Traffic struct {
	Up, Down int64
}
