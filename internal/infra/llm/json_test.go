package llm

import (
	"strings"
	"testing"
)

func TestModelJSONObjectPreservesTagsInStrings(t *testing.T) {
	for _, text := range []string{"literal <think>reason</think> text", "literal <think> without close"} {
		object := `{"title":"` + text + `"}`
		for _, wrapped := range []string{object, "```json\n" + object + "\n```", "说明<think>{reason}</think>\n" + object + "\n完成。"} {
			got, err := modelJSONObject(wrapped)
			if err != nil || string(got) != object {
				t.Fatalf("got %s, %v; want %s", got, err, object)
			}
		}
	}
	for _, invalid := range []string{"<think>unfinished", `{"broken":`, "none"} {
		if _, err := modelJSONObject(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	got, err := modelJSONObject("<THINK>reason</THINK>" + `{"title":"ok"}`)
	if err != nil || !strings.Contains(string(got), "ok") {
		t.Fatalf("uppercase wrapper: %s %v", got, err)
	}
}
