package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthConnected(t *testing.T) {
	r := New()
	srv := httptest.NewServer(r.Handler(func() bool { return true }))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHealthDisconnected(t *testing.T) {
	r := New()
	srv := httptest.NewServer(r.Handler(func() bool { return false }))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestMetricsExposition(t *testing.T) {
	r := New()
	r.RecordArchived()
	r.RecordArchived()
	r.RecordError()

	srv := httptest.NewServer(r.Handler(func() bool { return true }))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	wantLines := []string{
		"statussaver_archived_total 2",
		"statussaver_errors_total 1",
		"statussaver_connected 1",
		"# TYPE statussaver_archived_total counter",
		"# TYPE statussaver_connected gauge",
	}
	for _, want := range wantLines {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q\n--- body ---\n%s", want, body)
		}
	}
}

func TestMetricsDisconnectedGauge(t *testing.T) {
	r := New()
	srv := httptest.NewServer(r.Handler(func() bool { return false }))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	if !strings.Contains(string(buf[:n]), "statussaver_connected 0") {
		t.Errorf("disconnected state not reflected:\n%s", string(buf[:n]))
	}
}
