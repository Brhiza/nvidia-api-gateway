package gateway

import "testing"

func TestExtractGeminiModelDecodesEscapedSlash(t *testing.T) {
	got := extractGeminiModel("deepseek-ai%2Fdeepseek-v4-flash:generateContent")
	want := "deepseek-ai/deepseek-v4-flash"
	if got != want {
		t.Fatalf("extractGeminiModel() = %q, want %q", got, want)
	}
}

func TestExtractGeminiModelDecodesEscapedSlashStream(t *testing.T) {
	got := extractGeminiModel("deepseek-ai%2Fdeepseek-v4-flash:streamGenerateContent")
	want := "deepseek-ai/deepseek-v4-flash"
	if got != want {
		t.Fatalf("extractGeminiModel() = %q, want %q", got, want)
	}
}
