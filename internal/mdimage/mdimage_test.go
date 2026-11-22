package mdimage

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		in   string
		text string
		urls []string
	}{
		{"plain text", "plain text", nil},
		{"![image](https://cdn.example.com/a.png)", "", []string{"https://cdn.example.com/a.png"}},
		{"look:\n\n![cat](https://x.io/c.png)\n\nnice", "look:\n\nnice", []string{"https://x.io/c.png"}},
		{"a ![x](/uploads/k.png \"t\") b", "a  b", []string{"/uploads/k.png"}},
		{"![a](https://x/1.png)![b](https://x/1.png)![c](https://x/2.png)", "", []string{"https://x/1.png", "https://x/2.png"}},
		{"![bad](data:image/png;base64,AAAA) ok", "ok", nil},
		{"![](/uploads/../etc) ok", "ok", nil},
		{"not an image [link](https://x)", "not an image [link](https://x)", nil},
	}
	for _, c := range cases {
		text, urls := Split(c.in)
		if text != c.text || !reflect.DeepEqual(urls, c.urls) {
			t.Errorf("Split(%q) = %q, %v; want %q, %v", c.in, text, urls, c.text, c.urls)
		}
	}
}

func TestAbsolute(t *testing.T) {
	if got := Absolute("/uploads/a.png", "https://o.example"); got != "https://o.example/uploads/a.png" {
		t.Fatal(got)
	}
	if got := Absolute("/uploads/a.png", ""); got != "" {
		t.Fatal(got)
	}
	if got := Absolute("https://cdn/a.png", ""); got != "https://cdn/a.png" {
		t.Fatal(got)
	}
}
