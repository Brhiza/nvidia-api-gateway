package gateway

import (
	"log"

	"github.com/pkoukk/tiktoken-go"
)

var tk *tiktoken.Tiktoken

func init() {
	var err error
	// use cl100k_base for general GPT models which is a good approximation
	tk, err = tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		log.Fatalf("Failed to load tiktoken encoding: %v", err)
	}
}

// EstimateTokens calculates an approximate token count for the request messages
// and adds a 20% safety buffer.
func EstimateTokens(text string) int {
	if tk == nil {
		return 0
	}
	tokens := tk.Encode(text, nil, nil)
	count := len(tokens)
	// Add 20% safety buffer
	return int(float64(count) * 1.2)
}
