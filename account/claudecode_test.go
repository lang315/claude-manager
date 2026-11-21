package account

import "testing"

func TestDropKey(t *testing.T) {
	for _, c := range []struct{ in, key, want string }{
		{`{"a":1,"primaryApiKey":"k","b":2}`, "primaryApiKey", `{"a":1,"b":2}`},
		{"{\n  \"primaryApiKey\": \"k\",\n  \"b\": 2\n}", "primaryApiKey", "{\n  \"b\": 2\n}"},
		{`{"a":1,"primaryApiKey":"k"}`, "primaryApiKey", `{"a":1}`},
		{`{"primaryApiKey":"k"}`, "primaryApiKey", `{}`},
		{`{"a":{"primaryApiKey":1}}`, "primaryApiKey", `{"a":{"primaryApiKey":1}}`},
	} {
		got, err := dropKey([]byte(c.in), c.key)
		if err != nil || string(got) != c.want {
			t.Errorf("dropKey(%s) = %s, %v; want %s", c.in, got, err, c.want)
		}
	}
	if _, err := dropKey([]byte(`[]`), "a"); err == nil {
		t.Error("not an object")
	}
}
