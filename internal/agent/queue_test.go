package agent

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestQueueOfflineThenFlush(t *testing.T) {
	key := make([]byte, 32)
	q, err := OpenQueue(filepath.Join(t.TempDir(), "fila.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	for i := 0; i < 3; i++ {
		if err := q.Push(&Batch{BatchID: string(rune('a' + i)), AgentVersion: "t"}); err != nil {
			t.Fatal(err)
		}
	}

	var fail atomic.Bool
	fail.Store(true)
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "fora do ar", 503)
			return
		}
		if r.Header.Get("authorization") != "Bearer tok" || r.Header.Get("content-encoding") != "gzip" {
			http.Error(w, "sem auth", 400)
			return
		}
		zr, _ := gzip.NewReader(r.Body)
		var b Batch
		_ = json.NewDecoder(zr).Decode(&b)
		got = append(got, b.BatchID)
		_, _ = w.Write([]byte(`{"idleMinutes":5}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok")

	if err := q.Flush(c, nil); err == nil || q.Len() != 3 {
		t.Fatalf("offline: deveria manter os 3 lotes (err=%v, len=%d)", err, q.Len())
	}
	fail.Store(false)
	if err := q.Flush(c, nil); err != nil || q.Len() != 0 {
		t.Fatalf("online: deveria esvaziar (err=%v, len=%d)", err, q.Len())
	}
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("ordem errada: %v", got)
	}
}

func TestQueueIsEncrypted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fila.db")
	q, _ := OpenQueue(path, []byte("0123456789abcdef0123456789abcdef"))
	_ = q.Push(&Batch{BatchID: "segredo-visivel", AgentVersion: "t"})
	var data []byte
	_ = q.db.QueryRow(`select data from batches`).Scan(&data)
	q.Close()
	if json.Valid(data) || containsBytes(data, []byte("segredo-visivel")) {
		t.Fatal("lote gravado sem criptografia")
	}
	other, _ := OpenQueue(path, make([]byte, 32))
	defer other.Close()
	if ids, _, _ := other.Peek(10); len(ids) != 0 {
		t.Fatal("chave errada não deveria abrir o lote")
	}
}

func containsBytes(h, n []byte) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if string(h[i:i+len(n)]) == string(n) {
			return true
		}
	}
	return false
}
