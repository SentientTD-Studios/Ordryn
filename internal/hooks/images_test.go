package hooks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCommentTextAndImages(t *testing.T) {
	text, imgs := commentTextAndImages("see this\n\n![shot](https://cdn.example.com/a.png)")
	if text != "see this" || len(imgs) != 1 || imgs[0] != "https://cdn.example.com/a.png" {
		t.Fatalf("got %q %v", text, imgs)
	}
	text, imgs = commentTextAndImages("![image](https://cdn.example.com/a.png)")
	if text != "📷 Image" || len(imgs) != 1 {
		t.Fatalf("image-only comment: got %q %v", text, imgs)
	}
}

func TestPayloadsCarryCommentImages(t *testing.T) {
	vars := map[string]string{
		"name":   "Task",
		"url":    "https://o.example/tasks/1",
		"images": "https://cdn.example.com/a.png\nhttps://cdn.example.com/b.png",
	}
	for _, typ := range []string{"discord.webhook", "slack.webhook", "teams.webhook", "googlechat.webhook"} {
		raw, err := marshalWebhookPayload(typ, "", "hi", "task.commented", vars)
		if err != nil {
			t.Fatal(err)
		}
		s := string(raw)
		if !strings.Contains(s, "cdn.example.com/a.png") || !strings.Contains(s, "cdn.example.com/b.png") {
			t.Errorf("%s payload missing images: %s", typ, s)
		}
	}
	raw, _ := marshalWebhookPayload("discord.webhook", "", "hi", "task.commented", vars)
	var d struct {
		Embeds []struct {
			URL   string `json:"url"`
			Image struct {
				URL string `json:"url"`
			} `json:"image"`
		} `json:"embeds"`
	}
	if err := json.Unmarshal(raw, &d); err != nil || len(d.Embeds) != 2 || d.Embeds[1].URL != vars["url"] {
		t.Fatalf("discord gallery: %s", raw)
	}
	raw, _ = marshalWebhookPayload("http.webhook", "json", "hi", "task.commented", vars)
	if !strings.Contains(string(raw), `"images":["https://cdn.example.com/a.png","https://cdn.example.com/b.png"]`) {
		t.Errorf("json payload: %s", raw)
	}
}
