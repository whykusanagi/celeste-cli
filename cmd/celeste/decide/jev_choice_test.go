package decide

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/jev"
)

// A choice outside the question's options is no answer: the caller falls
// back as for an empty one (Guarded: the heuristic), instead of acting on
// a value it does not know (an unknown orchestrator lane).
func TestJevChoiceOutsideTheOptionsIsDropped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{
			"lane":{"type":"choice","choice":"rust","probabilities":{"rust":0.9},"confidence":0.9}}}`))
	}))
	defer srv.Close()
	got, err := Jev{Client: &jev.Client{Key: "k", URL: srv.URL}}.Ask(context.Background(), "write code", typed[2:])
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := got["lane"]; ok {
		t.Errorf("unknown choice kept: %+v", a)
	}
}
