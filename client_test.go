package cookielessaudiences

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSegmentAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("api_key") != "good" {
			_, _ = w.Write([]byte(`{"status":401}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":200,"interests":{"tier1":["INT.a"]},"labels":{"INT.a":"A"}}`))
	}))
	defer srv.Close()

	c := New("good")
	c.Base = srv.URL
	res, err := c.Segment(context.Background(), "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := Labels(res); !reflect.DeepEqual(got, []string{"A"}) {
		t.Fatalf("labels %v", got)
	}
	bad := New("bad")
	bad.Base = srv.URL
	if _, err := bad.Segment(context.Background(), "https://example.com"); err == nil {
		t.Fatal("expected APIError")
	} else if ae, ok := err.(*APIError); !ok || ae.Status != 401 {
		t.Fatalf("wrong error %v", err)
	}
}
