//go:build embed_model

package detector

import _ "embed"

// When built with -tags embed_model, the model and tokenizer are
// embedded directly in the binary. No download step needed.
//
// Build:
//   1. Place model files in internal/detector/models/
//   2. go build -tags embed_model -o aigw ./cmd/aigw
//
// Expected files:
//   internal/detector/models/model.onnx      (~90MB INT8 quantized)
//   internal/detector/models/tokenizer.json   (~2MB)

//go:embed models/model.onnx
var embeddedModelData []byte

//go:embed models/tokenizer.json
var embeddedTokenizerData []byte

func hasEmbeddedModel() bool        { return len(embeddedModelData) > 0 }
func getEmbeddedModel() []byte      { return embeddedModelData }
func getEmbeddedTokenizer() []byte  { return embeddedTokenizerData }
