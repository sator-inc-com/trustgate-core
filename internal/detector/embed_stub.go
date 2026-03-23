//go:build !embed_model

package detector

// Stub functions when model is NOT embedded in the binary.
// Model must be loaded from disk via 'aigw model download'.

func hasEmbeddedModel() bool   { return false }
func getEmbeddedModel() []byte { return nil }
func getEmbeddedTokenizer() []byte { return nil }
